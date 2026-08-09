// Package worker contains the asynq task handlers that run conversion engines.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/minio/minio-go/v7"

	"github.com/apaderin/octoconv/internal/cancel"
	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/metrics"
	"github.com/apaderin/octoconv/internal/queue"
	"github.com/apaderin/octoconv/internal/storage"
	"github.com/apaderin/octoconv/internal/webhook"
)

// terminalVipsSignatures are stderr substrings (lowercased) that vips emits
// for genuinely corrupted/unknown input formats. Verified live-tested
// (debian:bookworm-slim + libvips-tools, vips-8.14.1): exit code is 1 for
// EVERY failure mode (transient or terminal), so classification must be on
// stderr content, not exit code.
var terminalVipsSignatures = []string{
	"is not a known file format",
	"premature end of jpeg file",
	"jpeg datastream contains no image",
}

// terminalLibreOfficeSignatures are lowercased error-message substrings that
// indicate a deterministically-unrecoverable document conversion:
// validateDocumentOutput's guard against LibreOffice's documented "exit 0 but
// empty/corrupt/wrong-container output" failure mode ("output is empty",
// "output missing %pdf- magic bytes" from validatePDF, and "output does not
// match expected container format" from validateDocumentOutput's non-pdf
// SniffContainer check, internal/convert/libreoffice.go), plus filterFor's
// unsupported-(source,target)-pair error ("no export filter for"). No retry
// can fix any of these — a corrupt or filter-confused document always fails
// validateDocumentOutput/filterFor again (D-04): coupling the validator's
// error string into this slice in the same commit that introduces it is what
// keeps a mismatched cross-format output from being silently retried up to
// DOCUMENT_MAX_RETRY times before finally failing (T-13-02).
var terminalLibreOfficeSignatures = []string{
	"output missing %pdf- magic bytes",
	"output is empty",
	"no export filter for",
	"output does not match expected container format",
	"produced no output file",
	// PDF/A OutputIntent marker missing on a requested pdf_profile export
	// (D-05/D-06, phase 14): a mis-tagged output always fails the same
	// check again, so it must never retry into a false "done".
	"output missing pdf/a outputintent marker",
	// pdf_profile persisted on a non-pdf target (Convert's argv-side
	// invariant guard, internal/convert/libreoffice.go, WR-03): a corrupt
	// or hand-inserted jobs.options row deterministically fails the same
	// check on every delivery.
	"pdf_profile requested for non-pdf target",
}

// terminalChromiumSignatures are lowercased error-message substrings that
// indicate a deterministically-unrecoverable html->pdf conversion. Verified
// live-tested (Plan 04 smoke checklist, debian:bookworm-slim +
// chromium-headless-shell 150.0.7871.100):
//   - "output is empty" / "output missing %pdf- magic bytes" -- carried
//     over unchanged; internal/convert/chromium.go reuses validatePDF
//     verbatim from libreoffice.go, so these two error strings apply
//     identically to a corrupt/empty chromium-produced PDF.
//   - "stat output" -- validatePDF's os.Stat failure branch
//     (fmt.Errorf("libreoffice: stat output: %w", err)) fires when
//     --print-to-pdf writes NO file at all. Live-observed directly during
//     the smoke checklist's item 2 investigation: chromium-headless-shell
//     can exit 0 while silently producing zero output for a render that
//     never completes the print-to-pdf command handler (originally
//     triggered by the since-removed --blink-settings=scriptEnabled=false
//     flag, but the underlying "exit 0, no output file" failure mode is a
//     property of the one-shot command handler itself, not exclusive to
//     that one flag) -- classifying it terminal prevents burning the full
//     HTML_MAX_RETRY budget on a render that will deterministically produce
//     no output again on retry.
var terminalChromiumSignatures = []string{
	"output is empty",
	"output missing %pdf- magic bytes",
	"stat output",
}

// terminalAudioSignatures are lowercased error-message substrings that
// classify validateAudioOutput's "exit 0 but empty/missing output" failure
// mode (internal/convert/whisper.go) as deterministically unrecoverable:
// whisper-cli can exit 0 while writing an empty file ("audio: output is
// empty") or no file at all ("audio: stat output", validateAudioOutput's
// os.Stat failure branch) — a deterministic no-output render produces no
// output again on retry, so retrying only burns AUDIO_MAX_RETRY budget.
// Before WR-03 these two errors classified terminal only by ACCIDENT —
// substring-matching the foreign "output is empty"/"stat output" entries in
// terminalLibreOfficeSignatures/terminalChromiumSignatures inside the shared
// isTerminal loop — so rewording another engine's signature (or
// validateAudioOutput's message) would have silently flipped audio retry
// behavior with no failing test. This dedicated list, checked explicitly in
// isAudioTerminal BEFORE the shared-isTerminal fallthrough, makes the audio
// outcome self-contained; coupling validateAudioOutput's exact error strings
// into this slice follows the same commit-coupling discipline the
// LibreOffice/veraPDF lists document (D-04/D-06 precedent). The "audio: "
// prefix keeps these entries from ever matching another engine's stderr.
var terminalAudioSignatures = []string{
	"audio: output is empty",
	"audio: stat output",
}

// terminalVeraPDFSignatures are lowercased error-message substrings that
// classify a real ISO 19005-2b PDF/A validation failure as deterministically
// unrecoverable (D-06, phase 23): "pdf/a non-compliant" is emitted by
// ValidatePDFA (internal/convert/verapdf.go) when veraPDF's machine-readable
// report says isCompliant=false; "pdf/a validation error" is emitted for ANY
// veraPDF invocation/parse failure (unparseable report, batchSummary failure
// counters, process start failure) -- fail-closed per D-06: an unverifiable
// archival claim is a failed archival claim, never silently retried into a
// false "done". A VERAPDF_TIMEOUT expiry needs no signature here --
// isDocumentTerminal already classifies a wrapped context.DeadlineExceeded as
// terminal before this substring loop is ever reached. Coupling both
// substrings into this slice in the SAME commit that introduces them
// (verapdf.go) is what keeps a non-compliant/unverifiable PDF/A output from
// being silently retried up to DOCUMENT_MAX_RETRY times before finally
// failing.
var terminalVeraPDFSignatures = []string{
	"pdf/a non-compliant",
	"pdf/a validation error",
}

// isTerminal classifies a process() error as terminal (no retry can help:
// bad input, unsupported pair, missing storage object) vs. transient
// (network/S3/engine-timeout/Postgres blip — broad-retry default, D-01).
// This is the SHARED classifier used by both HandleImageConvert and (via
// isDocumentTerminal) HandleDocumentConvert. It deliberately has NO
// context.DeadlineExceeded arm — a timeout must keep classifying as
// transient here so the image path (HandleImageConvert) continues to retry
// engine timeouts; the document engine's divergent timeout-is-terminal
// behavior (DOC-08) lives in the engine-scoped isDocumentTerminal below, not
// here.
func isTerminal(err error) bool {
	if err == nil {
		return false
	}
	// internal/storage wraps every minio error via fmt.Errorf("...: %w", err),
	// and minio.ToErrorResponse itself is a bare type switch (no unwrapping) —
	// so errors.As must walk the %w chain first to surface the underlying
	// minio.ErrorResponse before classifying its Code.
	var mErr minio.ErrorResponse
	if errors.As(err, &mErr) && minio.ToErrorResponse(mErr).Code == minio.NoSuchKey {
		// D-02: storage input genuinely missing.
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no converter for") {
		// D-01: registry.Lookup miss — no engine supports this format pair.
		return true
	}
	for _, sig := range terminalVipsSignatures {
		if strings.Contains(msg, sig) {
			// D-01: engine reports a corrupted/unknown input format.
			return true
		}
	}
	for _, sig := range terminalLibreOfficeSignatures {
		if strings.Contains(msg, sig) {
			// D-01 (document analog): LibreOffice reports a deterministically
			// bad/unsupported document.
			return true
		}
	}
	for _, sig := range terminalChromiumSignatures {
		if strings.Contains(msg, sig) {
			// D-01 (html analog): chromium-headless-shell reports a
			// deterministically bad/unrenderable input.
			return true
		}
	}
	for _, sig := range terminalVeraPDFSignatures {
		if strings.Contains(msg, sig) {
			// D-06 (phase 23): veraPDF reports a non-compliant or
			// unverifiable PDF/A archival claim -- fail-closed, no retry.
			return true
		}
	}
	return false
}

// timeoutIsTerminal is the shared body behind the timeout-aware engine
// classifiers (isDocumentTerminal, isHTMLTerminal). It encodes the single
// divergence those engines share with the image path: nil is never terminal;
// a wrapped context.DeadlineExceeded (an engine-timeout expiry) IS terminal —
// a stuck soffice/chromium render is no more likely to succeed on retry, so
// the only reliable backstop is an immediate terminal failure (MarkFailed +
// SkipRetry, no asynq retry) rather than burning the whole retry budget;
// every non-timeout error falls through to the shared isTerminal classifier.
// Extracting this keeps the two engine-scoped entrypoints from drifting apart
// on a future edit (IN-01) while preserving their distinct doc comments.
func timeoutIsTerminal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return isTerminal(err)
}

// isDocumentTerminal is the document engine's engine-scoped terminal
// classifier — DOC-08's deliberate divergence from the image engine. Unlike
// isTerminal (which keeps treating a timeout as transient so the image path
// retries it), a DOCUMENT_ENGINE_TIMEOUT expiry IS classified terminal here:
// LibreOffice's documented hang-on-bad-input behavior means a retry cannot
// help, so an immediate terminal failure (MarkFailed + SkipRetry, no asynq
// retry) is the only reliable backstop. Without this, a stuck soffice would
// be retried up to DOCUMENT_MAX_RETRY times, each burning up to
// DOCUMENT_ENGINE_TIMEOUT (300s default) before finally being dropped.
//
// A DOCUMENT_ENGINE_TIMEOUT expiry surfaces as a wrapped context.
// DeadlineExceeded: exec.go's process-group kill produces
// fmt.Errorf("%s killed: %w", name, ctx.Err()), preserved through
// libreoffice.go's fmt.Errorf("libreoffice: %w", err) and process()'s
// fmt.Errorf("convert: %w", err) — so errors.Is(err, context.DeadlineExceeded)
// stays true all the way up to this handler.
//
// Every non-timeout error is classified identically to the image path by
// delegating (via timeoutIsTerminal) to isTerminal — no document-specific
// duplication of the no-converter/minio.NoSuchKey/LibreOffice-signature checks.
func isDocumentTerminal(err error) bool {
	return timeoutIsTerminal(err)
}

// isHTMLTerminal is the html engine's engine-scoped terminal classifier —
// HTML-01's deliberate divergence from the image engine, mirroring
// isDocumentTerminal exactly (DOC-08's timeout-is-terminal pattern applied
// to the chromium engine). An HTML_ENGINE_TIMEOUT expiry surfaces as a
// wrapped context.DeadlineExceeded (same exec.go process-group-kill shape,
// preserved through chromium.go's fmt.Errorf("chromium: %w", err) and
// process()'s fmt.Errorf("convert: %w", err)) and is classified terminal
// here rather than retried: a stuck chromium render is no more likely to
// succeed on retry than a stuck soffice render. Every non-timeout error is
// classified identically to the image/document paths by delegating (via
// timeoutIsTerminal) to the shared isTerminal (which now also checks
// terminalChromiumSignatures).
func isHTMLTerminal(err error) bool {
	return timeoutIsTerminal(err)
}

// isAudioTerminal is the audio engine's engine-scoped terminal classifier —
// Key Decision 1 (STATE.md, BINDING): a stage-aware split, deliberately NOT
// a copy of timeoutIsTerminal's blanket "any wrapped context.DeadlineExceeded
// is terminal" shape (the superseded interpretation FEATURES.md/
// ARCHITECTURE.md recommended). A ffmpeg-stage failure OR timeout (the
// "audio: ffmpeg:" prefix emitted by whisper.go's stage 1, normalize) is
// classified terminal — a malformed/adversarial input signal, mirroring the
// image engine's dimension-bomb terminal philosophy (T-31-08); a
// whisper-stage timeout on audio that already passed the ffprobe
// duration/format check (the "audio: whisper-cli:" prefix, stage 2) stays
// transient, bounded by AUDIO_MAX_RETRY (T-31-07) — mirroring the image
// engine's own timeout-stays-transient behavior, NOT
// isDocumentTerminal's/isHTMLTerminal's timeoutIsTerminal delegation.
// ErrAudioDurationExceeded (the EnforceMaxDuration guard spliced into
// process(), T-30-08/IN-02) always classifies terminal — a declared-duration
// rejection can never succeed on retry. Deterministic ffprobe-stage failures
// (the duration guard's own probe: a non-zero ffprobe exit on broken
// container metadata, or an unparseable/implausible reported duration) are
// likewise terminal (WR-01) — the probe runs BEFORE ffmpeg on the same
// untrusted input, so a parse-level probe failure is the same
// malformed/adversarial-input signal Key Decision 1 already classifies
// terminal at the ffmpeg stage; ffprobe environment/timeout shapes ("start
// ffprobe:", "ffprobe killed:") deliberately stay transient. Every other
// error (S3/Postgres blips, no-converter, non-timeout whisper-cli failures,
// ffprobe environment/timeout shapes) falls through to
// the shared isTerminal classifier — per 31-RESEARCH.md A2 (Claude's
// Discretion, adopted): the input to whisper-cli is a server-produced
// normalized WAV, so a non-timeout whisper-cli failure is most plausibly
// environment/config, not malformed client input. One deliberate exception
// to that transient default (WR-03): validateAudioOutput's exit-0-but-no-
// output shapes ("audio: output is empty", "audio: stat output") ARE
// terminal, via the dedicated terminalAudioSignatures list checked here —
// previously they classified terminal only by accidentally matching the
// LibreOffice/Chromium signature lists inside isTerminal; the explicit list
// makes that outcome self-contained and pinned.
//
// Deliberately does NOT call timeoutIsTerminal.
func isAudioTerminal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, convert.ErrAudioDurationExceeded) {
		// T-30-08/IN-02: a rejected declared duration can never succeed on
		// retry.
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "audio: ffmpeg:") {
		// Key Decision 1: ffmpeg-stage failure OR timeout is a
		// malformed/adversarial input signal — terminal, no retry.
		return true
	}
	if strings.Contains(msg, "ffprobe: unparseable duration") ||
		strings.Contains(msg, "ffprobe: implausible duration") ||
		strings.Contains(msg, "ffprobe failed:") {
		// WR-01: duration-guard (ffprobe) stage. The probe runs BEFORE ffmpeg
		// on the same untrusted input, so a deterministic probe-level failure
		// (non-zero exit on malformed container metadata — exec.go's
		// "ffprobe failed:" shape — or audioduration.go's
		// unparseable/implausible duration rejections) is the same
		// malformed/adversarial-input signal Key Decision 1 already
		// classifies terminal at the ffmpeg stage: a corrupt-but-sniffable
		// file must fail terminal at the very first probe instead of burning
		// the AUDIO_MAX_RETRY + reconciler budget. Environment/timeout shapes
		// are deliberately NOT matched here and stay transient: "start
		// ffprobe:" (binary missing/unrunnable — a deployment problem, not an
		// input problem) and "ffprobe killed:" (probe-ctx expiry under
		// enforceAudioGuardBeforeConvert's short probe bound — a hung probe
		// on a loaded host may succeed on retry).
		return true
	}
	for _, sig := range terminalAudioSignatures {
		if strings.Contains(msg, sig) {
			// WR-03: whisper-cli's exit-0-but-empty/missing-output failure
			// mode (validateAudioOutput) is deterministic — terminal via
			// audio's OWN signature list, independent of the
			// LibreOffice/Chromium lists these strings previously (and
			// accidentally) matched inside the shared isTerminal loop.
			return true
		}
	}
	return isTerminal(err)
}

// isAVTerminal is the av engine's engine-scoped terminal classifier — D-02
// (35-CONTEXT.md, BINDING): a stage-aware split derived FRESH for video, not
// a copy of isAudioTerminal's blanket "any 'audio: ffmpeg:' failure or
// timeout is terminal" rule. That rule is correct for audio because ffmpeg
// is audio's CHEAP normalize stage — a timeout there is a strong
// malformed-input signal. It would be WRONG for av: av's transcode stage
// (ErrAVTranscodeFailed) is the EXPENSIVE operation this whole class
// exists to run, so a timeout there may simply mean the retry budget ran
// out under load, not that the input is malformed — porting audio's rule
// verbatim would make every transcode timeout terminal and silently
// destroy the transient-retry behavior this classifier exists to provide.
// ErrAudioDurationExceeded is reused here on purpose (not a bug): AV's
// duration guard calls the same enforceMaxDurationOf helper audio uses
// (av.go:395), so the same sentinel — and the same always-terminal
// treatment — keeps the two engines' client-facing contracts symmetric.
func isAVTerminal(err error) bool {
	if err == nil {
		return false
	}
	// Deterministic guard/output-validation rejections: always terminal,
	// regardless of which stage produced them — a rejected declared
	// resolution, an out-of-range timecode, a missing/empty output, or a
	// missing video stream can never succeed on retry.
	if errors.Is(err, convert.ErrAVOutputMissingOrEmpty) ||
		errors.Is(err, convert.ErrAVTimecodeOutOfRange) ||
		errors.Is(err, convert.ErrAVResolutionExceeded) ||
		errors.Is(err, convert.ErrAVReencodeResolutionExceeded) ||
		errors.Is(err, convert.ErrAudioDurationExceeded) ||
		errors.Is(err, convert.ErrAVNoVideoStream) {
		return true
	}
	isTimeout := errors.Is(err, context.DeadlineExceeded)
	switch {
	case errors.Is(err, convert.ErrAVTranscodeFailed):
		// D-02: transcode is the expensive stage — a timeout stays
		// TRANSIENT (this is the single assertion that distinguishes this
		// classifier from isAudioTerminal); a non-timeout ffmpeg failure
		// (malformed/adversarial input) stays terminal.
		return !isTimeout
	case errors.Is(err, convert.ErrAVAudioExtractFailed), errors.Is(err, convert.ErrAVThumbnailFailed):
		// D-02: audio-extract and thumbnail are the cheap stages — ANY
		// failure (timeout or not) is TERMINAL, mirroring audio's own
		// cheap-stage rule.
		return true
	}
	// No substring-based fallback here: after Plan 01's D-01 sentinel
	// refactor, every av ffmpeg stage has a typed sentinel, so matching on
	// error text would only reintroduce the exact fragility D-01 removed.
	return isTerminal(err) // no-converter / minio.NoSuchKey / shared fallthrough
}

// terminalOCRSignatures are lowercased error-message prefixes that classify a
// genuine (non-timeout) tesseract/pdftoppm/pdfinfo engine crash as
// deterministically unrecoverable (D-04, 44-CONTEXT.md): OCR is
// deterministic on its input, so a corrupt/adversarial file that crashes the
// engine once will crash it again on retry. "ocr: tesseract:" wraps a
// tesseract exec failure (internal/convert/ocr.go, ocrpdf.go); "ocr:
// pdftoppm:" wraps a per-page rasterize failure (ocrpdf.go); "pdfinfo:" wraps
// a pdfinfo probe failure (ocrguards.go, ocrpdf.go) -- the probe runs BEFORE
// rasterization on the same untrusted PDF, so a probe crash is the same
// malformed-input signal as a rasterize/OCR crash. Checked explicitly here,
// BEFORE the shared isTerminal fallthrough, mirroring terminalAudioSignatures'
// self-contained-list precedent (worker.go) rather than relying on an
// accidental substring match in another engine's list.
var terminalOCRSignatures = []string{
	"ocr: tesseract:",
	"ocr: pdftoppm:",
	"pdfinfo:",
}

// isOCRTerminal is the OCR engine's engine-scoped terminal classifier — D-04
// (44-CONTEXT.md, BINDING): a stage-aware split derived for OCR, deliberately
// NOT a copy of isDocumentTerminal's/isHTMLTerminal's blanket
// timeout-is-terminal shape, and NOT a copy of isAudioTerminal's/
// isAVTerminal's non-timeout-default-to-shared-isTerminal shape either.
// Deterministic input/client-fault guard rejections (ErrOCRBornDigital,
// ErrOCRTooManyPages, ErrOCRPageBoxTooLarge, ErrOCRImageTooLarge, an unknown-
// language rejection, or an unsupported-target rejection) are ALWAYS
// terminal — a retry can never help a rejection that is deterministic on the
// input. A wrapped context.DeadlineExceeded (an OCR_ENGINE_TIMEOUT expiry) is
// the single divergence from isDocumentTerminal/isHTMLTerminal: it classifies
// TRANSIENT here, mirroring isAVTerminal's transcode-timeout treatment --
// retry may succeed when worker load drops. Every OTHER non-timeout error
// (a genuine tesseract/pdftoppm/pdfinfo crash) is terminal via the dedicated
// terminalOCRSignatures list above, checked BEFORE the shared isTerminal
// fallthrough -- OCR is deterministic on its input, so a crash will recur
// (unlike isAudioTerminal's/isAVTerminal's default-to-transient-via-
// isTerminal treatment of an unrecognized non-timeout failure, which assumes
// their inputs are server-produced/pre-validated by an earlier stage). Only
// a genuinely unrecognized error (no-converter, minio.NoSuchKey, or some
// other unrecognized shape entirely) falls through to the shared isTerminal.
func isOCRTerminal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, convert.ErrOCRBornDigital) ||
		errors.Is(err, convert.ErrOCRTooManyPages) ||
		errors.Is(err, convert.ErrOCRPageBoxTooLarge) ||
		errors.Is(err, convert.ErrOCRImageTooLarge) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unsupported language") || strings.Contains(msg, "unsupported target format") {
		// D-04: a rejected client-requested language (OCROptsFromMap) or an
		// unrecognized target-format extension (ocrPrepareConversion) is a
		// deterministic client-input fault -- retry can never help, same as
		// the guard sentinels above.
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// D-04: the single assertion that distinguishes this classifier from
		// isDocumentTerminal/isHTMLTerminal -- an OCR_ENGINE_TIMEOUT expiry
		// stays TRANSIENT, mirroring isAVTerminal's transcode-timeout
		// treatment.
		return false
	}
	for _, sig := range terminalOCRSignatures {
		if strings.Contains(msg, sig) {
			// D-04: a genuine non-timeout tesseract/pdftoppm/pdfinfo crash is
			// deterministic on this input -- it will recur, so it is
			// terminal, not a broad-retry default.
			return true
		}
	}
	return isTerminal(err) // no-converter / minio.NoSuchKey / shared fallthrough
}

// ocrFailureCode maps a terminal process() error for an OCR job to the
// distinguishable client-facing (error_code, message) pair MarkFailed
// records — mirrors avFailureCode's precedent (worker.go): a small pure
// function so the mapping itself is unit-testable without a live Postgres/S3
// Handler. Order matters: the more specific sentinels are checked before the
// generic default.
//   - ErrOCRBornDigital -> "born_digital": the PDF already has an extractable
//     text layer -- OCR was never needed and will never succeed as an OCR
//     job.
//   - ErrOCRTooManyPages -> "too_many_pages".
//   - ErrOCRPageBoxTooLarge -> "page_too_large".
//   - ErrOCRImageTooLarge -> "image_too_large".
//   - an unknown-language rejection -> "unsupported_language".
//   - an unsupported-target rejection -> "unsupported_target".
//   - default -> "engine_error", mirroring every other engine's fallback.
func ocrFailureCode(err error) (code, message string) {
	switch {
	case errors.Is(err, convert.ErrOCRBornDigital):
		return CodeBornDigital, "pdf already has an extractable text layer"
	case errors.Is(err, convert.ErrOCRTooManyPages):
		return CodeTooManyPages, "pdf page count exceeds the configured maximum"
	case errors.Is(err, convert.ErrOCRPageBoxTooLarge):
		return CodePageTooLarge, "pdf page would rasterize beyond the configured pixel bound"
	case errors.Is(err, convert.ErrOCRImageTooLarge):
		return CodeImageTooLarge, "image dimensions exceed the configured maximum"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unsupported language") {
		return CodeUnsupportedLanguage, "requested ocr language is not supported"
	}
	if strings.Contains(msg, "unsupported target format") {
		return CodeUnsupportedTarget, "requested target format is not supported for ocr"
	}
	return CodeEngineError, "unsupported or corrupted input format"
}

// isPandocTerminal is the pandoc engine's engine-scoped terminal classifier
// -- D-01 (48-CONTEXT.md, BINDING): pandoc conversion is deterministic and
// CPU-bound with no external services/network, so BOTH a PANDOC_ENGINE_TIMEOUT
// expiry AND a pandoc engine/parse error are classified TERMINAL. This
// mirrors isDocumentTerminal's/isHTMLTerminal's timeout-is-terminal shape
// (DOC-08's precedent), the OPPOSITE of isOCRTerminal's
// transient-engine-timeout treatment -- a stuck/pathological pandoc
// invocation is no more likely to succeed on retry than a stuck soffice or
// chromium render.
//
// A PANDOC_ENGINE_TIMEOUT expiry surfaces as a wrapped context.
// DeadlineExceeded (exec.go's process-group-kill shape, preserved through
// pandoc.go's fmt.Errorf("pandoc: pandoc: %w", err) and process()'s
// fmt.Errorf("convert: %w", err)) -- errors.Is(err, context.DeadlineExceeded)
// stays true all the way up to this handler, exactly like
// isDocumentTerminal's own documented chain.
//
// Every deterministic failure PandocConverter.Convert can produce --
// unsupported-pair rejection, a corrupt persisted-opts parse failure,
// ErrPandocInputTooLarge, the pandoc subprocess itself failing, or
// pandocValidateOutput rejecting a bare/corrupt output -- is wrapped with a
// "pandoc:" prefix SOMEWHERE in the chain (pandoc.go: every one of Convert's
// error returns carries it, verified against pandoc.go's own error-wrapping
// discipline). Checking for that substring here, explicitly and BEFORE the
// shared isTerminal fallthrough, is what makes a bare pandoc parse/engine
// error terminal -- isTerminal alone has no pandoc-specific signature list
// and would otherwise return false (transient) for it, which would be wrong:
// pandoc is deterministic on its input, so an engine/parse failure will recur
// identically on retry. Only pandoc.Convert ever emits this prefix, so a
// genuine infra transient (a plain S3/Postgres blip with no "pandoc:" text)
// is NOT captured here and correctly falls through isTerminal to its
// broad-retry default (D-01's "genuine infra transients stay retryable"
// half).
//
// Engine-scoped like isDocumentTerminal/isHTMLTerminal/isOCRTerminal/
// isAudioTerminal/isAVTerminal -- never reword another engine's signature.
func isPandocTerminal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// D-01: the deliberate divergence from isOCRTerminal -- a pandoc
		// timeout means pathological/oversize input a retry cannot fix.
		return true
	}
	if errors.Is(err, convert.ErrPandocInputTooLarge) {
		// T-48-10: oversize input rejected fail-closed before the subprocess
		// ever runs -- deterministic on the input, never fixed by retry.
		return true
	}
	if strings.Contains(strings.ToLower(err.Error()), "pandoc:") {
		// D-01: every deterministic failure PandocConverter.Convert can
		// produce (unsupported pair, corrupt opts, subprocess failure, bad
		// output) carries this prefix -- terminal, not the shared isTerminal's
		// broad-retry default.
		return true
	}
	return isTerminal(err) // no-converter / minio.NoSuchKey / shared fallthrough
}

// pandocFailureCode maps a terminal process() error for a pandoc job to the
// distinguishable client-facing (error_code, message) pair MarkFailed
// records -- mirrors ocrFailureCode's precedent (worker.go): a small pure
// function so the mapping itself is unit-testable without a live Postgres/S3
// Handler. Order matters: the more specific substrings are checked before
// the generic default.
//   - ErrPandocInputTooLarge -> "input_too_large": the input exceeds the
//     configured PANDOC_MAX_INPUT_BYTES cap.
//   - an unsupported-pair rejection (pandocPairSupported's own
//     "unsupported conversion %s -> %s" message, pandoc.go) ->
//     "unsupported_target".
//   - a persisted-opts parse/validation failure (PandocOptsFromMap's
//     "unsupported wrap %q" or "parse opts:" messages, pandocopts.go) ->
//     "invalid_options".
//   - default -> "engine_error", mirroring every other engine's fallback.
func pandocFailureCode(err error) (code, message string) {
	if errors.Is(err, convert.ErrPandocInputTooLarge) {
		return CodeInputTooLarge, "pandoc input exceeds the configured maximum size"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unsupported conversion") {
		return CodeUnsupportedTarget, "requested conversion is not supported by the pandoc engine"
	}
	if strings.Contains(msg, "unsupported wrap") || strings.Contains(msg, "parse opts") {
		return CodeInvalidOptions, "stored conversion options are invalid"
	}
	return CodeEngineError, "unsupported or corrupted input format"
}

// isCanceled reports whether err is (or wraps) context.Canceled -- the shape
// process()'s attemptCtx.Err() takes when cancel.Default.Cancel(job.ID) fires
// the registered CancelFunc early, distinct from a timeoutIsTerminal-style
// context.DeadlineExceeded (the engine timeout firing on its own). Every
// Handle*Convert checks isCanceled BEFORE its engine-scoped terminal
// classifier (isTerminal/isDocumentTerminal/isHTMLTerminal/isAudioTerminal/
// isAVTerminal): a canceled job must SkipRetry WITHOUT MarkFailed (D-07) --
// the job is already `canceled` in Postgres via the API's guarded
// transition, and re-failing it would be wrong (MarkFailed would also be
// rejected by the allow-list, since `canceled` is not an allowed source for
// any Mark* transition, D-05).
func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

// convertRepo is the narrow, interface-segregated subset of *jobs.Repo the
// seven Handle*Convert wrappers and process() depend on (mirrors
// internal/api/api.go's Repo/Storage/Enqueuer interface-segregation
// precedent, and this package's own pre-existing progressRepo). It is a
// STRICT SUPERSET of progressRepo's two methods (UpdateProgress,
// SetSourceDuration) — process() passes h.repo, typed convertRepo, straight
// into runConvertWithOptionalProgress's progressRepo parameter, and Go only
// permits that interface-to-interface assignment while convertRepo retains
// both. Introduced as a behavior-preserving test seam (56-CONTEXT.md D-10
// step 1, RUNR-03): before this, Handler.repo was a concrete *jobs.Repo, so
// constructing a Handler for a handler-level test needed a live Postgres
// (internal/worker/pandoc_integration_test.go documented this exact
// limitation). Unexported: satisfiable only from within package worker, so
// no external package gains a way to substitute a repo (T-56-01).
type convertRepo interface {
	Get(ctx context.Context, id uuid.UUID) (*jobs.Job, error)
	MarkActive(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, code, message string, detail map[string]any, attempt time.Duration) error
	MarkDone(ctx context.Context, id uuid.UUID, attempt time.Duration) error
	Inputs(ctx context.Context, jobID uuid.UUID) ([]jobs.Input, error)
	AddOutput(ctx context.Context, jobID uuid.UUID, o jobs.Output) error
	Outputs(ctx context.Context, jobID uuid.UUID) ([]jobs.Output, error)
	UpdateProgress(ctx context.Context, id uuid.UUID, pct int, eta *int) error
	SetSourceDuration(ctx context.Context, id uuid.UUID, seconds float64) error
}

// webhookEnqueuer is the narrow, interface-segregated subset of
// *queue.Client the seven Handle*Convert wrappers depend on — the single
// EnqueueWebhookDeliver call fired after a terminal or successful outcome
// commits. Introduced alongside convertRepo as a behavior-preserving test
// seam (56-CONTEXT.md D-10 step 1, RUNR-03); unexported for the same reason
// (T-56-01).
type webhookEnqueuer interface {
	EnqueueWebhookDeliver(ctx context.Context, jobID uuid.UUID) error
}

// Handler processes image conversion tasks end to end.
type Handler struct {
	repo          convertRepo
	store         *storage.Client
	registry      *convert.Registry
	engineTimout  time.Duration
	webhookRepo   *webhook.Repo
	deliverer     *webhook.Deliverer
	enqueuer      webhookEnqueuer
	signingSecret []byte
	presignTTL    time.Duration
	// audioMaxDuration is the AUDIO_MAX_DURATION_SECONDS ceiling passed to
	// convert.EnforceMaxDuration for EngineAudio jobs inside process()
	// (T-30-08/IN-02). Zero (the value every non-audio worker cmd passes) is
	// inert — process() only invokes the guard when job.Engine ==
	// convert.EngineAudio, so image/document/html/webhook workers never read
	// this field. The real value is wired by the audio worker cmd (Plan 04).
	audioMaxDuration time.Duration
	// progressMinInterval / progressMinDeltaPct are the PROG-03 dual-gate
	// throttle config (D-03), installed via SetProgressThrottle after
	// construction. progressMinInterval <= 0 (the zero value every worker cmd
	// but av-worker leaves it at) disables progress reporting entirely:
	// process() only takes the convert.ProgressConverter seam when this is
	// > 0, so image/document/html/audio jobs are byte-unaffected even when
	// their converter happened to implement the optional interface.
	progressMinInterval time.Duration
	progressMinDeltaPct int
	// progressWebhookDelivr / progressWebhookSecret / progressWebhookTimeout
	// are the D-04 opt-in inline progress-webhook delivery config, installed
	// via SetProgressWebhook after construction (mirrors SetProgressThrottle's
	// post-construction-setter precedent). progressWebhookDelivr nil (every
	// worker cmd but av-worker, and av-worker itself when
	// WEBHOOK_SIGNING_SECRET is unset) disables progress-webhook delivery
	// entirely — progressSink.Update's own webhookDelivr != nil gate skips
	// the fire, so this is inert rather than a startup failure.
	progressWebhookDelivr  progressWebhookDeliverer
	progressWebhookSecret  []byte
	progressWebhookTimeout time.Duration
	// processFn is the D-10 processFn indirection: assigned to the method
	// value h.process in NewHandler (after the struct literal is built), and
	// called by all seven Handle*Convert wrappers in place of a direct
	// h.process(...) call. h.process itself is unchanged — this field exists
	// solely so a pinning test can substitute a stub processFn on a Handler
	// built from fakes, without a live engine/converter/storage stack.
	processFn func(ctx context.Context, job *jobs.Job) error
	// preConvertGuard, when non-nil, splices additional validation
	// immediately before process()'s call to the engine's Convert (via
	// runConvertWithOptionalProgress) — installed by NewRunner from the
	// bound EngineDescriptor's PreConvertGuard field (runner.go). nil for
	// six of the seven engines; only the audio Runner installs one,
	// wrapping the unmodified enforceAudioGuardBeforeConvert
	// (T-30-08/IN-02). A nil guard is functionally identical to the
	// pre-Task-1 code path: enforceAudioGuardBeforeConvert's own
	// engine-gate was already a pass-through for every non-audio engine.
	preConvertGuard func(ctx context.Context, job *jobs.Job, inPath string, convertFn func() error) error
}

// SetProgressWebhook installs the D-04 opt-in inline progress-webhook
// delivery config (mirrors SetProgressThrottle's post-construction-setter
// precedent — NewHandler's already-long signature is not widened for a knob
// only av-worker uses). Never calling this (every non-av worker cmd, and
// av-worker itself when WEBHOOK_SIGNING_SECRET is unset) leaves
// progressWebhookDelivr nil, which progressSink.Update's own gate treats as
// "progress webhooks disabled" — the throttled DB write still lands
// unchanged.
func (h *Handler) SetProgressWebhook(d progressWebhookDeliverer, secret []byte, timeout time.Duration) {
	h.progressWebhookDelivr = d
	h.progressWebhookSecret = secret
	h.progressWebhookTimeout = timeout
}

// SetProgressThrottle installs the PROG-03 dual-gate progress-write throttle
// (mirrors Phase 39's SetCancelPublisher post-construction-setter precedent
// — NewHandler's already-long signature is not widened for a knob only
// av-worker uses). Never calling this (every non-av worker cmd) leaves
// progressMinInterval at its zero value, which process()'s type-assert seam
// treats as "progress reporting disabled" — the plain conv.Convert path
// runs unchanged.
func (h *Handler) SetProgressThrottle(minInterval time.Duration, minDeltaPct int) {
	h.progressMinInterval = minInterval
	h.progressMinDeltaPct = minDeltaPct
}

// NewHandler builds a worker handler. repo and enqueuer keep their concrete
// *jobs.Repo / *queue.Client parameter types — only the Handler struct
// fields they populate are typed as the narrower convertRepo/webhookEnqueuer
// interfaces (D-10 step 1): both concrete types satisfy those interfaces
// structurally, so no production wiring changes, and no external package
// gains a way to inject an alternative store (T-56-01).
func NewHandler(repo *jobs.Repo, store *storage.Client, registry *convert.Registry, engineTimeout time.Duration, webhookRepo *webhook.Repo, deliverer *webhook.Deliverer, enqueuer *queue.Client, signingSecret []byte, presignTTL time.Duration, audioMaxDuration time.Duration) *Handler {
	if engineTimeout == 0 {
		engineTimeout = 120 * time.Second
	}
	if presignTTL == 0 {
		// D-09: comfortably exceed the ~30 min retry window so a recovering
		// or dead-lettered client can still fetch the result.
		presignTTL = 6 * time.Hour
	}
	h := &Handler{
		repo:             repo,
		store:            store,
		registry:         registry,
		engineTimout:     engineTimeout,
		webhookRepo:      webhookRepo,
		deliverer:        deliverer,
		enqueuer:         enqueuer,
		signingSecret:    signingSecret,
		presignTTL:       presignTTL,
		audioMaxDuration: audioMaxDuration,
	}
	// processFn defaults to h.process (D-10): the struct literal above cannot
	// reference h while h is still being constructed, so the method value is
	// assigned here instead. A pinning test overrides this field directly on
	// a Handler built from fakes.
	h.processFn = h.process
	return h
}

// HandleImageConvert runs one image conversion end to end (load job -> mark
// active -> download input -> run engine -> upload output -> record output
// -> mark done), entirely through Runner.Handle over ImageDescriptor
// (runner.go/descriptors.go, D-09): image is the one engine with no opts
// re-parse guard (ImageDescriptor.OptsFromMap is nil,
// TestPinnedImageHasNoOptsGuard) and no FailureCode override -- a terminal
// image failure always uses isTerminal + the shared default "engine_error"
// pair, mirroring HandleWebhookDeliver's own separate classification
// pattern (untouched by this refactor).
//
// This method itself carries no conversion logic any more (D-09's "no
// per-engine handler" truth): it exists solely so
// internal/worker/pinning_test.go's frozen dispatch table
// ((*Handler).HandleImageConvert, a Go method expression) keeps compiling
// and keeps exercising the real Runner path unchanged -- every worker
// binary's own mux registration (cmd/worker/main.go) calls
// worker.NewRunner(h, worker.ImageDescriptor).Handle directly instead, never
// through this method.
func (h *Handler) HandleImageConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, ImageDescriptor).Handle(ctx, t)
}

// HandleDocumentConvert runs one document conversion end to end, entirely
// through Runner.Handle over DocumentDescriptor (D-09): DOC-07/DOC-08's two
// divergences from ImageDescriptor now live as DATA on the descriptor --
// OptsFromMap (the strict opts re-parse guard image lacks) and
// isDocumentTerminal (a DOCUMENT_ENGINE_TIMEOUT expiry takes the terminal
// path, unlike the image engine's own isTerminal, which treats the same
// timeout as transient). See HandleImageConvert's doc comment for why this
// method still exists as a thin delegator (frozen pinning_test.go
// compatibility, D-10 step 2) rather than being deleted outright.
func (h *Handler) HandleDocumentConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, DocumentDescriptor).Handle(ctx, t)
}

// HandleHTMLConvert runs one html->pdf conversion end to end, entirely
// through Runner.Handle over HTMLDescriptor (D-09). HTML-01's two
// divergences from ImageDescriptor now live as DATA on the descriptor,
// mirroring DocumentDescriptor's shape exactly: OptsFromMap and
// isHTMLTerminal (an HTML_ENGINE_TIMEOUT expiry takes the terminal path).
// See HandleImageConvert's doc comment for why this method still exists as
// a thin delegator.
func (h *Handler) HandleHTMLConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, HTMLDescriptor).Handle(ctx, t)
}

// HandleAudioConvert runs one audio transcription end to end, entirely
// through Runner.Handle over AudioDescriptor (D-09). AUD-05's three
// divergences from ImageDescriptor now live as DATA on the descriptor:
// OptsFromMap; isAudioTerminal (Key Decision 1's stage-aware classifier,
// NOT a blanket timeout-is-terminal copy); and FailureCode = audioFailureCode
// (descriptors.go), which gives a duration-guard rejection its own distinct
// "duration_exceeded" client-facing code (31-RESEARCH.md A3) instead of the
// generic "engine_error". process()'s own EnforceMaxDuration splice
// (T-30-08/IN-02) is installed via NewRunner's audio-specific
// preConvertGuard wiring, not through anything in this method. See
// HandleImageConvert's doc comment for why this method still exists as a
// thin delegator.
func (h *Handler) HandleAudioConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, AudioDescriptor).Handle(ctx, t)
}

// avFailureCode maps a terminal process() error for an av job to the
// distinguishable client-facing (error_code, message) pair MarkFailed
// records. Factored out as a small pure function so the mapping itself is
// unit-testable without a live Postgres/S3 Handler (HandleAVConvert's own
// db plumbing cannot run without them). Order matters: the more specific
// sentinels are checked before the generic default.
//   - ErrAVTimecodeOutOfRange -> "timecode_out_of_range" (D-09): an explicit
//     out-of-range thumbnail timecode is a CLIENT fault, deliberately not
//     clamped, and must not be indistinguishable from a corrupted-input
//     engine failure.
//   - ErrAudioDurationExceeded -> "duration_exceeded", the same code
//     HandleAudioConvert already emits for its own duration guard (AV's
//     guard reuses the identical sentinel, av.go:395) — keeps the two
//     engines' client-facing contracts symmetric.
//   - ErrAVResolutionExceeded -> "resolution_exceeded".
//   - ErrAVNoVideoStream -> "no_video_stream" (IN-01): a generic-brand
//     ISOBMFF audio-only file misrouted to the av engine must say so rather
//     than reporting a generic engine error.
//   - default -> "engine_error", mirroring every other engine's fallback.
func avFailureCode(err error) (code, message string) {
	switch {
	case errors.Is(err, convert.ErrAVTimecodeOutOfRange):
		return CodeTimecodeOutOfRange, "requested thumbnail timecode exceeds source duration"
	case errors.Is(err, convert.ErrAudioDurationExceeded):
		return CodeDurationExceeded, "declared video duration exceeds the configured maximum"
	case errors.Is(err, convert.ErrAVResolutionExceeded):
		return CodeResolutionExceeded, "declared video resolution exceeds the configured maximum"
	case errors.Is(err, convert.ErrAVNoVideoStream):
		return CodeNoVideoStream, "no video stream found in source"
	default:
		return CodeEngineError, "unsupported or corrupted input format"
	}
}

// HandleAVConvert runs one video conversion (transcode / audio-extract /
// thumbnail) end to end, entirely through Runner.Handle over AVDescriptor
// (D-09). AVE-03's two divergences from DocumentDescriptor's SIMPLE shape
// (no PreConvertGuard: AVConverter.Convert already self-contains its own
// duration/resolution guard, av.go:388-400) now live as DATA: isAVTerminal
// (D-02's stage-aware split, the OPPOSITE polarity from isAudioTerminal)
// and FailureCode = avFailureCode, so a client can distinguish a
// timecode/duration/resolution/no-video-stream rejection from a generic
// corrupted-input failure. See HandleImageConvert's doc comment for why
// this method still exists as a thin delegator.
func (h *Handler) HandleAVConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, AVDescriptor).Handle(ctx, t)
}

// HandleOCRConvert runs one OCR extraction (image or scanned/image-only PDF
// -> txt/hOCR/ALTO) end to end, entirely through Runner.Handle over
// OCRDescriptor (D-09). OCRConverter.Convert already self-contains every
// guard (page-count/page-box/born-digital/image-pixel, ocrguards.go/
// ocrpdf.go), mirroring AVDescriptor's SIMPLE shape (no PreConvertGuard).
// isOCRTerminal (D-04's stage-aware split, the OPPOSITE of isAVTerminal's
// own shape) and FailureCode = ocrFailureCode live as DATA on the
// descriptor, so a client can distinguish a born-digital/too-many-pages/
// page-too-large/image-too-large/unsupported-language/unsupported-target
// rejection from a generic corrupted-input failure. See
// HandleImageConvert's doc comment for why this method still exists as a
// thin delegator.
func (h *Handler) HandleOCRConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, OCRDescriptor).Handle(ctx, t)
}

// HandlePandocConvert runs one pandoc markup-to-markup conversion end to
// end, entirely through Runner.Handle over PandocDescriptor (D-01,
// 48-CONTEXT.md, D-09). PandocConverter.Convert already self-contains every
// guard (unsupported-pair/opts/input-size, pandocValidateOutput's
// target-appropriate content check, pandoc.go/markupsniff.go), mirroring
// OCRDescriptor's strict-opts-reparse shape. isPandocTerminal (pandoc's
// timeout+engine-error terminal classification mirrors isDocumentTerminal,
// the OPPOSITE of isOCRTerminal's transient-timeout treatment) and
// FailureCode = pandocFailureCode live as DATA on the descriptor.
//
// An empty-but-valid pandoc output (e.g. an empty-body markdown source
// producing an empty plaintext/fragment result) completes as done, never
// failed (SC4): pandocValidateOutput (pandoc.go) already skips its
// content-sniff check on a zero-size output and returns nil, so process()
// returns nil and Runner.Handle's success path runs unchanged. See
// HandleImageConvert's doc comment for why this method still exists as a
// thin delegator.
func (h *Handler) HandlePandocConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, PandocDescriptor).Handle(ctx, t)
}

// HandleWebhookDeliver delivers one webhook attempt for a completed job:
// re-read the job from Postgres, regenerate a fresh presigned download_url
// per attempt (done only, D-09), sign, deliver, record the attempt, and
// dead-letter on final-attempt exhaustion (D-10). Delivery failures are
// returned unwrapped so asynq applies its own retry/backoff (D-05); only
// unparseable payloads and jobs with no callback_url are terminal.
// buildTerminalWebhookBody constructs the base terminal-webhook JSON body
// carried by EVERY delivery of the durable done/failed event — the SOLE
// terminal-body construction site (HandleWebhookDeliver below adds
// download_url/error_code/error_message on top of it for the done/failed
// branches respectively). The additive "event":"completed" discriminator
// (D-03) lets a client on a shared callback_url tell this event apart from
// the new opt-in `progress` event (progressSink.fireDetached); the
// signature (SignPayload, computed after json.Marshal) covers it since it
// is set on the map BEFORE marshaling, same as job_id/status. Factored out
// as a small pure function (mirrors avFailureCode's precedent) so the
// discriminator is unit-testable without a live Postgres/S3/webhook
// Handler.
func buildTerminalWebhookBody(job *jobs.Job) map[string]any {
	body := map[string]any{
		"job_id": job.ID,
		"status": job.Status,
	}
	body["event"] = "completed"
	return body
}

// setWebhookDownloadURL sets body["download_url"] to the presigned URL of
// the job's PRIMARY output, selected by ROLE (jobs.OutputRolePrimary), never
// by ordinal or slice position -- a report or preview output must never
// reach download_url just because it sorts first (codex PR #7 finding #1;
// same defect class ROLE-02/T-57-24 closed for handleGetJob at
// handlers.go:944-951). With no primary present in outs, it sets nothing and
// returns nil: an asynq retry storm over a state D-13 makes unreachable
// (a done job always has exactly one primary output) would be worse than an
// omitted key, and substituting another output is the very bug being fixed
// here -- this mirrors handleGetJob's hasPrimary=false path exactly. A
// presign error is returned unwrapped; the caller applies its own
// "presign output for job %s: %w" wrapping. Factored out as a pure function
// (same rationale as buildTerminalWebhookBody above) because
// HandleWebhookDeliver itself cannot be unit-tested: h.store is a concrete
// *storage.Client whose constructor does a live BucketExists round trip.
func setWebhookDownloadURL(ctx context.Context, body map[string]any, outs []jobs.Output, presign func(context.Context, string) (string, error)) error {
	for _, o := range outs {
		if o.Role == jobs.OutputRolePrimary {
			url, err := presign(ctx, o.ObjectKey)
			if err != nil {
				return err
			}
			body["download_url"] = url
			return nil
		}
	}
	return nil
}

func (h *Handler) HandleWebhookDeliver(ctx context.Context, t *asynq.Task) error {
	payload, err := queue.ParseWebhookPayload(t.Payload())
	if err != nil {
		// Unparseable payload: nothing we can retry into success.
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	jobID := payload.JobID

	job, err := h.repo.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", jobID, err)
	}

	if job.CallbackURL == "" {
		// Nothing to deliver to — terminal, not a transient failure.
		return fmt.Errorf("%w: no callback_url", asynq.SkipRetry)
	}

	body := buildTerminalWebhookBody(job)
	if job.Status == jobs.StatusDone {
		outs, err := h.repo.Outputs(ctx, job.ID)
		if err != nil {
			return fmt.Errorf("load outputs for job %s: %w", jobID, err)
		}
		if len(outs) == 0 {
			return fmt.Errorf("job %s has no outputs", jobID)
		}
		// D-09: regenerate a fresh presigned URL on every attempt, never
		// reuse a stale one across retries.
		if err := setWebhookDownloadURL(ctx, body, outs, func(c context.Context, key string) (string, error) {
			return h.store.PresignGet(c, key, h.presignTTL)
		}); err != nil {
			return fmt.Errorf("presign output for job %s: %w", jobID, err)
		}
	}
	if job.Status == jobs.StatusFailed {
		if job.ErrorCode != "" {
			body["error_code"] = job.ErrorCode
		}
		if job.ErrorMessage != "" {
			body["error_message"] = job.ErrorMessage
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal webhook body for job %s: %w", jobID, err)
	}

	ts := time.Now().Unix()
	sig := webhook.SignPayload(h.signingSecret, ts, bodyBytes)

	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	attempt := retryCount + 1

	code, derr := h.deliverer.Deliver(ctx, job.CallbackURL, bodyBytes, ts, sig)
	metrics.RecordWebhookDelivery(derr == nil)

	var statusCodePtr *int
	if code > 0 {
		statusCodePtr = &code
	}
	deliveryID, recErr := h.webhookRepo.RecordAttempt(ctx, jobID, job.CallbackURL, attempt, statusCodePtr, derr == nil)

	if derr != nil {
		if recErr == nil && retryCount >= maxRetry {
			// Final attempt exhausted: flag for investigation (D-10).
			_ = h.webhookRepo.MarkDeadLetter(ctx, deliveryID)
		}
		// Unwrapped: let asynq's own retry policy + backoff (D-05) apply.
		return derr
	}
	return nil
}

// audioProbeTimeout is the SHORT bound the duration guard's ffprobe
// invocation runs under (WR-02/T-30-03): ProbeDuration's contract is explicit
// that its ctx must never carry the full AUDIO_ENGINE_TIMEOUT budget —
// ffprobe reads container metadata and is near-instant even for large files,
// while a hung/adversarial probe (the guard's own threat model: it runs on
// untrusted input BEFORE any decode) would otherwise consume the entire
// whole-attempt deadline on every retry. A probe-ctx expiry surfaces as
// "ffprobe: ffprobe killed: context deadline exceeded", which isAudioTerminal
// deliberately classifies TRANSIENT (WR-01's documented split — a hung probe
// on a loaded host may succeed on retry), so this bound converts a
// full-budget hang into a fast, retryable failure.
const audioProbeTimeout = 15 * time.Second

// enforceAudioGuardBeforeConvert splices the T-30-08/IN-02 declared-duration
// guard in front of the engine's Convert call, gated on engine ==
// convert.EngineAudio (image/document/html jobs never pay the ffprobe cost
// and audioMaxDuration, 0 for every non-audio worker cmd, is never
// consulted for them). Factored out of process() as its own package-level
// function so the guard-runs-strictly-before-Convert ordering is
// unit-testable (the IN-02 pinning test) without a live-Postgres/S3
// Handler — process()'s h.repo/h.store are concrete types, not interfaces
// (ARCHITECTURE.md's "Key Abstractions" note), so a full Handler cannot be
// constructed in a pure unit test. A rejection here surfaces as
// ErrAudioDurationExceeded, unwrapped, which isAudioTerminal classifies
// terminal.
func enforceAudioGuardBeforeConvert(ctx context.Context, engine, inPath string, audioMaxDuration time.Duration, convertFn func() error) error {
	if engine == convert.EngineAudio {
		// WR-02/T-30-03: derive a short probe-only deadline from the
		// whole-attempt ctx rather than passing it through — ProbeDuration
		// must never run under the full AUDIO_ENGINE_TIMEOUT budget.
		probeCtx, cancel := context.WithTimeout(ctx, audioProbeTimeout)
		err := convert.EnforceMaxDuration(probeCtx, inPath, audioMaxDuration)
		cancel()
		if err != nil {
			return err
		}
	}
	return convertFn()
}

// progressRepo is the narrow subset of *jobs.Repo the progress sink writes
// through — interface-segregated (mirrors internal/api/api.go's Repo/
// Storage/Enqueuer pattern) so the throttle/terminal-bypass/freeze-on-abort
// logic below is unit-testable with a spy, without a live Postgres-backed
// *jobs.Repo. *jobs.Repo satisfies this structurally; no wiring is needed at
// the call site beyond passing h.repo.
type progressRepo interface {
	UpdateProgress(ctx context.Context, id uuid.UUID, pct int, eta *int) error
	SetSourceDuration(ctx context.Context, id uuid.UUID, seconds float64) error
}

// progressWebhookDeliverer is the narrow subset of *webhook.Deliverer the
// progress sink fires through — interface-segregated (mirrors progressRepo's
// own interface-segregation, internal/api/api.go's Repo/Storage/Enqueuer
// pattern) so the D-04 detached best-effort fire is spy-testable without a
// live HTTP deliverer. *webhook.Deliverer satisfies this structurally; no
// wiring is needed at the call site beyond passing a *webhook.Deliverer.
type progressWebhookDeliverer interface {
	Deliver(ctx context.Context, url string, body []byte, timestamp int64, signature string) (int, error)
}

// progressSink implements convert.ProgressSink for the worker: it applies
// the PROG-03 dual-gate throttle (D-03) to every Update — a write lands only
// when BOTH at least minInterval has elapsed since the last write AND the
// value moved by at least minDeltaPct — and persists through the
// non-locking progressRepo. SourceDuration persists the probed duration once
// (D-05). Every write is best-effort: a progress/telemetry write must never
// fail the conversion, so the error progressRepo returns is deliberately
// ignored (mirrors the best-effort webhook-enqueue discipline used
// elsewhere in this file); library code never logs (CLAUDE.md). Zero value
// is unusable (minInterval/minDeltaPct must be set) — callers always
// construct this via runConvertWithOptionalProgress, never directly.
//
// webhookEnabled/callbackURL/webhookSecret/webhookDelivr/webhookTimeout are
// the D-04 opt-in inline progress-webhook fire config: all zero/nil for
// every non-opted-in job (job.ProgressWebhook false) and for every
// non-av-worker cmd (webhookDelivr stays nil — only cmd/av-worker calls
// Handler.SetProgressWebhook) — fireDetached's own gate makes a missing
// deliverer/unset callback byte-identical to "opted out," no separate code
// path.
type progressSink struct {
	ctx         context.Context
	repo        progressRepo
	jobID       uuid.UUID
	minInterval time.Duration
	minDeltaPct int

	hasLast   bool
	lastWrite time.Time
	lastPct   int

	webhookEnabled bool
	callbackURL    string
	webhookSecret  []byte
	webhookDelivr  progressWebhookDeliverer
	webhookTimeout time.Duration
}

// SourceDuration reports the probed source duration once, best-effort.
func (s *progressSink) SourceDuration(seconds float64) {
	_ = s.repo.SetSourceDuration(s.ctx, s.jobID, seconds)
}

// Update applies the dual-gate throttle and, if it passes, persists the
// tick best-effort, then (D-04) fires a best-effort progress webhook from
// the SAME throttle decision — one PROG-03 decision drives both the DB
// write and the webhook fire.
func (s *progressSink) Update(u convert.ProgressUpdate) {
	now := time.Now()
	if s.hasLast {
		if now.Sub(s.lastWrite) < s.minInterval {
			return
		}
		if u.Pct-s.lastPct < s.minDeltaPct {
			return
		}
	}
	_ = s.repo.UpdateProgress(s.ctx, s.jobID, u.Pct, u.ETA)
	s.hasLast = true
	s.lastWrite = now
	s.lastPct = u.Pct

	if s.webhookEnabled && s.callbackURL != "" && s.webhookDelivr != nil {
		s.fireDetached(u)
	}
}

// fireDetached fires one best-effort, single-attempt, HMAC-signed progress
// webhook POST from a SHORT-LIVED DETACHED goroutine bound to its OWN
// context.WithTimeout(context.Background(), s.webhookTimeout) — NOT s.ctx
// (the whole-attempt ctx the ffmpeg -progress reader goroutine itself runs
// under) — so a slow/adversarial callback_url endpoint can NEVER stall the
// reader (Pitfall 5, T-42-07). Single attempt, error discarded
// (drop-on-failure, D-04): a progress-webhook write must never fail or
// block the conversion, mirroring the best-effort DB-write discipline this
// method already applies above. This function structurally NEVER
// references h.enqueuer, h.webhookRepo, or queue.NewWebhookDeliverTask —
// there is no enqueue call anywhere in it, which is what makes the
// separation from the terminal retry/dead-letter budget (T-42-08) a
// construction guarantee, not a runtime check.
func (s *progressSink) fireDetached(u convert.ProgressUpdate) {
	body, err := json.Marshal(map[string]any{
		"job_id":       s.jobID,
		"event":        "progress",
		"progress_pct": u.Pct,
		"eta_seconds":  u.ETA,
	})
	if err != nil {
		// Best-effort: a marshal failure for this fixed, non-adversarial
		// shape should never happen; silently drop the fire rather than
		// fail the conversion. Library code never logs (CLAUDE.md).
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.webhookTimeout)
		defer cancel()
		ts := time.Now().Unix()
		sig := webhook.SignPayload(s.webhookSecret, ts, body)
		_, _ = s.webhookDelivr.Deliver(ctx, s.callbackURL, body, ts, sig)
	}()
}

// runConvertWithOptionalProgress is the D-01 seam: it type-asserts conv for
// the optional convert.ProgressConverter capability and, ONLY when both that
// capability is present AND progress throttling is configured
// (minInterval > 0, i.e. SetProgressThrottle was called — currently
// av-worker only), drives a throttled progressSink through
// ConvertWithProgress. Otherwise it calls the plain conv.Convert path,
// byte-unchanged for every other engine (D-01) even if that converter
// happens to also implement ProgressConverter. On a SUCCESSFUL
// ConvertWithProgress return, the terminal 100% write BYPASSES the throttle
// entirely and always lands (D-03a) — issued directly against repo, not
// through the sink's gated Update, and fires NO progress webhook (A2: the
// terminal `completed` webhook, HandleWebhookDeliver, already covers
// completion — a duplicate progress=100 fire here would be redundant with
// that durable event). On an ERROR return, nothing further is written: the
// last throttled value stays frozen (D-06) — this is simply the
// terminal-100 call on the line below never executing on that branch, not
// separate "reset" logic. Factored out of process() as a package-level
// function (mirrors enforceAudioGuardBeforeConvert) so this whole contract
// is unit-testable with a fake ProgressConverter and a spy progressRepo,
// without a live Postgres/S3/live-ffmpeg Handler.
//
// webhookEnabled/callbackURL/webhookDelivr/webhookSecret/webhookTimeout are
// the D-04 opt-in inline progress-webhook config, threaded straight onto
// the constructed progressSink; every non-opted-in job and every
// non-av-worker cmd passes webhookEnabled=false / webhookDelivr=nil, which
// keeps the DB-write path byte-identical to before this plan.
func runConvertWithOptionalProgress(ctx context.Context, conv convert.Converter, inPath, outPath string, opts map[string]any, repo progressRepo, jobID uuid.UUID, minInterval time.Duration, minDeltaPct int, webhookEnabled bool, callbackURL string, webhookDelivr progressWebhookDeliverer, webhookSecret []byte, webhookTimeout time.Duration) error {
	pconv, ok := conv.(convert.ProgressConverter)
	if !ok || minInterval <= 0 {
		return conv.Convert(ctx, inPath, outPath, opts)
	}

	sink := &progressSink{
		ctx: ctx, repo: repo, jobID: jobID, minInterval: minInterval, minDeltaPct: minDeltaPct,
		webhookEnabled: webhookEnabled, callbackURL: callbackURL, webhookDelivr: webhookDelivr,
		webhookSecret: webhookSecret, webhookTimeout: webhookTimeout,
	}
	if err := pconv.ConvertWithProgress(ctx, inPath, outPath, opts, sink); err != nil {
		return err
	}
	// D-03a/A2: terminal 100% write bypasses the throttle entirely, always
	// lands on a successful convert, and fires no progress webhook.
	_ = repo.UpdateProgress(ctx, jobID, 100, nil)
	return nil
}

func (h *Handler) process(ctx context.Context, job *jobs.Job) error {
	// The ENTIRE attempt (input lookup, download, convert, upload, record) is
	// bounded by a single whole-attempt deadline, not just conv.Convert(): the
	// minio client sets no transport read-deadline, so a stalled (hung, not
	// refused) S3 transfer would otherwise have NO upper bound. Plan 01's
	// derived ImageUniqueTTL assumes every attempt is <= ENGINE_TIMEOUT and
	// asynq never refreshes the per-job unique lock on internal same-task
	// retries — a single attempt that outlives that TTL would let the lock
	// lapse in Redis while this handler is still running, letting the
	// reconciler's next EnqueueImageConvert create a second concurrent task
	// for the same job (the T-03-10 double-processing race). Threading one
	// attemptCtx through every step closes that gap (T-03-11).
	attemptCtx, cancelFn := context.WithTimeout(ctx, h.engineTimout)
	defer cancelFn()

	// attemptStart is a SEPARATE clock from each Handle*Convert caller's own
	// `start` local: it measures only the engine attempt this transaction is
	// accounting for (the client_usage_daily wall_seconds column threaded
	// into MarkDone below), while the caller's `start` continues to feed
	// metrics.RecordJobOutcome's duration histogram unchanged. Captured here
	// (immediately after the attempt context, before cancel registration and
	// any download/convert/upload work) so it covers the FULL attempt this
	// call to MarkDone is committing, not just the engine invocation.
	attemptStart := time.Now()

	// D-04: register the per-attempt CancelFunc BEFORE any download/convert/
	// upload work so the job:cancel Subscriber (internal/cancel) can find and
	// invoke it for the whole attempt window, and deregister on return
	// (whichever way process() exits) so a later attempt for a different job
	// id is never affected by a stale entry. Calling cancelFn early cancels
	// attemptCtx -> runCommand's existing ctx.Done() SIGKILL path fires
	// UNCHANGED (internal/convert/exec.go) -> attemptCtx.Err() surfaces as
	// context.Canceled, distinct from the timeout's context.DeadlineExceeded.
	cancel.Default.Register(job.ID, cancelFn)
	defer cancel.Default.Deregister(job.ID)

	inputs, err := h.inputKey(attemptCtx, job.ID)
	if err != nil {
		return err
	}

	// job.Operation is the value handleCreateJob recorded and validated
	// fail-closed (57-04): this is the ONLY point in the worker that reads
	// the registry, and it trusts job.Operation completely -- it does NOT
	// substitute a default. If job.Operation is ever empty (a row created
	// outside the API, bypassing its validation), LookupOp simply finds no
	// converter and the job fails honestly instead of being silently
	// processed as convert.
	conv, ok := h.registry.LookupOp(job.Operation, job.SourceFormat, job.TargetFormat)
	if !ok {
		return fmt.Errorf("no converter for operation %s: %s -> %s", job.Operation, job.SourceFormat, job.TargetFormat)
	}

	workDir, err := os.MkdirTemp("", "octoconv-"+job.ID.String()+"-")
	if err != nil {
		return fmt.Errorf("mkdtemp: %w", err)
	}
	defer os.RemoveAll(workDir)

	inPath := filepath.Join(workDir, "in."+job.SourceFormat)
	outName := "out." + job.TargetFormat
	outPath := filepath.Join(workDir, outName)

	if err := h.downloadTo(attemptCtx, inputs.ObjectKey, inPath); err != nil {
		return err
	}

	// T-30-08/IN-02: the declared-duration guard MUST actually run, not just
	// be documented — closes the gap where EnforceMaxDuration existed
	// (Phase 30) but no caller ever invoked it. Runs AFTER download (there
	// is nothing to probe before the file exists locally) and strictly
	// BEFORE conv.Convert (the most expensive step).
	//
	// D-09 (56-CONTEXT.md): the splice itself is now generic — h.preConvertGuard,
	// installed by NewRunner from the bound EngineDescriptor's
	// PreConvertGuard (runner.go), runs in front of convertFn when non-nil;
	// every other engine (h.preConvertGuard == nil) calls convertFn()
	// directly. This is functionally identical to the pre-refactor
	// unconditional call to enforceAudioGuardBeforeConvert: that function's
	// own engine-gate (`if engine == convert.EngineAudio`) was already a
	// pass-through for every non-audio engine, so removing the
	// always-audio-shaped wrapper here changes no externally observable
	// behavior.
	convertFn := func() error {
		if err := runConvertWithOptionalProgress(attemptCtx, conv, inPath, outPath, job.Opts, h.repo, job.ID, h.progressMinInterval, h.progressMinDeltaPct, job.ProgressWebhook, job.CallbackURL, h.progressWebhookDelivr, h.progressWebhookSecret, h.progressWebhookTimeout); err != nil {
			return fmt.Errorf("convert: %w", err)
		}
		return nil
	}
	var convertErr error
	if h.preConvertGuard != nil {
		convertErr = h.preConvertGuard(attemptCtx, job, inPath, convertFn)
	} else {
		convertErr = convertFn()
	}
	if convertErr != nil {
		return convertErr
	}

	outKey := storage.OutputKey(job.ID, 0, outName)
	size, err := h.uploadFrom(attemptCtx, outKey, outPath, convert.MIMEType(job.TargetFormat))
	if err != nil {
		return err
	}

	if err := h.repo.AddOutput(attemptCtx, job.ID, jobs.Output{
		Ordinal:     0,
		ObjectKey:   outKey,
		Filename:    outName,
		Format:      job.TargetFormat,
		SizeBytes:   size,
		ContentType: convert.MIMEType(job.TargetFormat),
	}); err != nil {
		return err
	}

	return h.repo.MarkDone(attemptCtx, job.ID, time.Since(attemptStart))
}

func (h *Handler) inputKey(ctx context.Context, jobID uuid.UUID) (jobs.Input, error) {
	ins, err := h.repo.Inputs(ctx, jobID)
	if err != nil {
		return jobs.Input{}, err
	}
	if len(ins) == 0 {
		return jobs.Input{}, fmt.Errorf("job %s has no inputs", jobID)
	}
	return ins[0], nil
}

func (h *Handler) downloadTo(ctx context.Context, key, path string) error {
	rc, err := h.store.Download(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, rc); err != nil {
		return fmt.Errorf("write input: %w", err)
	}
	return nil
}

func (h *Handler) uploadFrom(ctx context.Context, key, path, contentType string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open output: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat output: %w", err)
	}
	if err := h.store.Upload(ctx, key, f, fi.Size(), contentType); err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
