package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/apaderin/octoconv/internal/jobs"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/measurements"
	"github.com/apaderin/octoconv/internal/queue"
)

// Async job-failure error codes (jobs.error_code).
//
// Phase 56 (D-04/D-05) froze these strings against renaming: every one carries
// the exact value it had before that refactor, and CodeEngineError is a
// deliberate catch-all shared by every engine's default fallback.
// CodeReconcilerExhausted is the one code emitted OUTSIDE this package -- its
// sole producer is internal/reconciler/reconciler.go.
//
// Phase 59 introduced a distinction this vocabulary did not previously need:
// a code can stop being PRODUCIBLE without ceasing to EXIST. jobs.error_code is
// a persisted column (internal/db/migrations/0001_init.sql) served back on
// GET /v1/jobs/{id}, so a code no current code path can emit is still returned
// to clients reading old rows. Deleting such a code would make the contract
// wrong about the past.
//
// Hence two lists below. allFailureCodes is what this build can still produce.
// historicalFailureCodes is what it cannot, but clients may still receive.
// Both are documented; only the first is a list of possible outcomes.
const (
	CodeEngineError        = "engine_error"
	CodeInvalidOptions     = "invalid_options"
	CodeDurationExceeded   = "duration_exceeded"
	CodeTimecodeOutOfRange = "timecode_out_of_range"
	CodeResolutionExceeded = "resolution_exceeded"
	CodeNoVideoStream      = "no_video_stream"
	CodeTooManyPages       = "too_many_pages"
	// CodeUnsupportedPackage: the document loads a \usepackage this
	// deployment's bundle cannot resolve. Shares its string with
	// errcode.UnsupportedPackage -- ONE code space (D-03), the same as
	// CodeInvalidOptions above -- because the API's bounded preamble scan is
	// allowed to ABSTAIN, so the identical refusal arrives here for any
	// document whose preamble ran past that window. Two codes for one failure
	// would make where it was caught visible to the client, which is not
	// something they can act on.
	CodeUnsupportedPackage  = "unsupported_package"
	CodePageTooLarge        = "page_too_large"
	CodeImageTooLarge       = "image_too_large"
	CodeUnsupportedLanguage = "unsupported_language"
	CodeUnsupportedTarget   = "unsupported_target"
	CodeInputTooLarge       = "input_too_large"
	// CodeOutputTooLarge: the engine succeeded and the RESULT is too big. Only
	// the tex engine can reach it -- it is the only one whose input is a
	// program, so the only one where output size is not bounded by input size.
	CodeOutputTooLarge      = "output_too_large"
	CodeReconcilerExhausted = "reconciler_exhausted"
	CodeCADTooComplex       = "cad_too_complex"
	CodeCADUnreadable       = "cad_unreadable"
	CodeCADEmptyResult      = "cad_empty_result"
	CodeCADMeshTimeout      = "cad_mesh_timeout"
)

// allFailureCodes enumerates every code this build can still produce.
// AllFailureCodes() is built from it, and the doc-sync test
// (internal/errcode/doc_sync_test.go) holds it equal to the asynchronous table
// in docs/ERROR-CODES.md.
//
// A code removed from here MUST be added to historicalFailureCodes below --
// removing it from both would delete from the contract a value clients can
// still be served from the database. TestActiveAndHistoricalFailureCodesAreDisjoint
// and the historical-section doc test enforce both halves.
var allFailureCodes = []string{
	CodeEngineError,
	CodeInvalidOptions,
	CodeDurationExceeded,
	CodeTimecodeOutOfRange,
	CodeResolutionExceeded,
	CodeNoVideoStream,
	CodeTooManyPages,
	CodePageTooLarge,
	CodeImageTooLarge,
	CodeUnsupportedLanguage,
	CodeUnsupportedTarget,
	CodeInputTooLarge,
	CodeOutputTooLarge,
	CodeReconcilerExhausted,
	CodeUnsupportedPackage,
	CodeCADTooComplex,
	CodeCADUnreadable,
	CodeCADEmptyResult,
	CodeCADMeshTimeout,
}

// CodeBornDigital is HISTORICAL as of Phase 59 and is not in allFailureCodes.
//
// It meant "this PDF already has an extractable text layer, so the OCR job was
// refused". Phase 59 (D-05/D-06) replaced that refusal with a route: a txt
// target now receives the extracted text layer, and hocr/alto go on to a real
// render because pdftotext produces no word or line boxes. No code path can
// produce this value any more -- proven, not asserted, by
// convert.TestBornDigitalHasNoRefusalPath, which runs a born-digital PDF through
// all three targets -- and, more strongly, by the sentinel no longer existing
// at all: Phase 59 deleted it, so the compiler now enforces what that test
// used to check.
//
// It is kept declared, and kept in docs/ERROR-CODES.md's historical section,
// because jobs created before Phase 59 still carry it in the database and
// GET /v1/jobs/{id} still serves it. A client that received it yesterday must
// still be able to look it up today.
const CodeBornDigital = "born_digital"

// historicalFailureCodes enumerates codes this build can no longer produce but
// which clients may still receive for jobs that failed before the change.
//
// The distinction exists because deleting a retired code from the documentation
// would create a NEW inaccuracy while removing an old one: the contract would
// stop describing values the API demonstrably still returns. Retiring a code is
// therefore a move between two lists, never a deletion -- and the doc-sync test
// checks both lists and their disjointness, so neither half can be skipped.
var historicalFailureCodes = []string{
	CodeBornDigital,
}

// AllFailureCodes returns every async job-failure code this build can still
// PRODUCE, exactly once, in declaration order. The returned slice is a fresh
// copy on every call, so callers cannot mutate the package's closed vocabulary
// through it.
//
// Deliberately excludes historical codes: this is the list of possible
// outcomes, and a consumer enumerating what a job might fail with should not be
// shown values nothing can emit. Consumers that must INTERPRET a code received
// from the API need HistoricalFailureCodes() as well.
func AllFailureCodes() []string {
	out := make([]string, len(allFailureCodes))
	copy(out, allFailureCodes)
	return out
}

// HistoricalFailureCodes returns codes no longer producible by this build but
// still reachable by a client reading an old job. Same fresh-copy contract as
// AllFailureCodes.
func HistoricalFailureCodes() []string {
	out := make([]string, len(historicalFailureCodes))
	copy(out, historicalFailureCodes)
	return out
}

// audioFailureCode maps a terminal process() error for an audio job to the
// distinguishable client-facing (error_code, message) pair MarkFailed
// records — lifted verbatim (Task 2, 56-04-PLAN.md) from HandleAudioConvert's
// own inline two-way branch into a named, unit-testable function, mirroring
// avFailureCode/ocrFailureCode/pandocFailureCode's precedent (worker.go):
// a duration-guard rejection gets its own distinguishable, actionable
// client-facing code (31-RESEARCH.md A3) rather than the generic
// "engine_error" — the client can react to it differently than a generic
// corrupted-input failure.
func audioFailureCode(err error) (code, message string) {
	if errors.Is(err, convert.ErrAudioDurationExceeded) {
		return CodeDurationExceeded, "declared audio duration exceeds the configured maximum"
	}
	return CodeEngineError, "unsupported or corrupted input format"
}

// The eight package-level engine descriptors (D-09: never a
// map[string]EngineDescriptor registry — each worker binary imports its own
// directly, matching the one-engine-per-binary deployment model every
// cmd/*-worker/main.go already uses). OptsFromMap adapters preserve each
// convert.*OptsFromMap wrapper's exact "parse strictly, use nothing"
// semantics: the parsed value is discarded, only the error is kept.

// ImageDescriptor is the one deliberate divergence from the other six: no
// OptsFromMap (image never re-parses its persisted Opts,
// TestPinnedImageHasNoOptsGuard) and no FailureCode override (every
// terminal image failure uses the shared default "engine_error" pair).
var ImageDescriptor = EngineDescriptor{
	Engine:     convert.EngineImage,
	Type:       queue.TypeImageConvert,
	IsTerminal: isTerminal,
}

// DocumentDescriptor mirrors ImageDescriptor plus the strict opts re-parse
// guard; isDocumentTerminal is DOC-08's deliberate timeout-is-terminal
// divergence from the image engine's isTerminal.
var DocumentDescriptor = EngineDescriptor{
	Engine: convert.EngineDocument,
	Type:   queue.TypeDocumentConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.DocOptsFromMap(m)
	},
	IsTerminal: isDocumentTerminal,
}

// HTMLDescriptor mirrors DocumentDescriptor's timeout-is-terminal shape
// (isHTMLTerminal delegates to the same timeoutIsTerminal helper).
var HTMLDescriptor = EngineDescriptor{
	Engine: convert.EngineHTML,
	Type:   queue.TypeHTMLConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.HTMLOptsFromMap(m)
	},
	IsTerminal: isHTMLTerminal,
}

// AudioDescriptor is the one engine with a non-nil PreConvertGuard need —
// deliberately left nil HERE (see NewRunner's doc comment in runner.go for
// the full rationale: audioMaxDuration is per-Handler, env-configured
// config unavailable to a package-level var built at init time). NewRunner
// special-cases convert.EngineAudio and installs the guard itself.
// isAudioTerminal is Key Decision 1's stage-aware split (ffmpeg-stage
// terminal, whisper-stage transient) — NOT a copy of the
// timeout-is-terminal shape above.
var AudioDescriptor = EngineDescriptor{
	Engine: convert.EngineAudio,
	Type:   queue.TypeAudioConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.AudioOptsFromMap(m)
	},
	IsTerminal:  isAudioTerminal,
	FailureCode: audioFailureCode,
}

// AVDescriptor's isAVTerminal is D-02's stage-aware split, derived fresh
// for video with the OPPOSITE polarity from isAudioTerminal's: a transcode
// (expensive) stage timeout stays transient, but any audio-extract/
// thumbnail (cheap) stage failure is terminal regardless of timeout.
var AVDescriptor = EngineDescriptor{
	Engine: convert.EngineAV,
	Type:   queue.TypeAVConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.AVOptsFromMap(m)
	},
	IsTerminal:  isAVTerminal,
	FailureCode: avFailureCode,
}

// OCRDescriptor's isOCRTerminal is D-04's stage-aware split: deterministic
// guard rejections are always terminal, but an OCR_ENGINE_TIMEOUT expiry
// stays transient (mirroring isAVTerminal's transcode-timeout treatment,
// the opposite of isDocumentTerminal's/isHTMLTerminal's shape).
var OCRDescriptor = EngineDescriptor{
	Engine: convert.EngineOCR,
	Type:   queue.TypeOCRConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.OCROptsFromMap(m)
	},
	IsTerminal:  isOCRTerminal,
	FailureCode: ocrFailureCode,
}

// PandocDescriptor's isPandocTerminal is D-01's timeout-is-terminal shape
// (mirroring isDocumentTerminal/isHTMLTerminal), the OPPOSITE of
// isOCRTerminal's transient-engine-timeout treatment — pandoc is
// deterministic and CPU-bound with no external services/network.
var PandocDescriptor = EngineDescriptor{
	Engine: convert.EnginePandoc,
	Type:   queue.TypePandocConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.PandocOptsFromMap(m)
	},
	IsTerminal:  isPandocTerminal,
	FailureCode: pandocFailureCode,
}

// ProbeDescriptor is the eighth, and the first whose task type is not a
// convert (queue.TypeProbeSanitize).
//
// TERMINAL/TRANSIENT, DERIVED FOR SANITIZE RATHER THAN COPIED
//
// Phase 56's descriptors show that polarity is per-engine and genuinely
// disagrees between neighbours: isAudioTerminal treats an ffmpeg-stage
// timeout as terminal and a whisper-stage timeout as transient, and
// isAVTerminal has the opposite shape. Copying a neighbour's classification
// is the same antipattern as copying its timeout, so isProbeTerminal is
// argued branch by branch:
//
//   - Verification failure -- TERMINAL. The verifier is deterministic on the
//     output bytes, and the output is deterministic on the input. A retry
//     re-runs the same sanitize on the same file and reaches the same
//     verdict. Retrying would burn the budget to be told the same thing
//     three times.
//   - Ghostscript TIMEOUT -- TRANSIENT. This is the one branch that depends
//     on something other than the input: how loaded the box is. It is the
//     OPPOSITE of isPandocTerminal's shape, and for the stated reason --
//     pandoc is a CPU-bound text transform, while a ghostscript rasterizing
//     pass competes for a shared CPU budget under KEDA scale-out. Same
//     polarity as isOCRTerminal, arrived at independently: both wrap an
//     external renderer whose worst case is load, not content.
//   - Ghostscript PARSE failure -- TERMINAL. A file gs cannot read now is a
//     file gs cannot read in twenty seconds.
//   - Options present -- TERMINAL. Sanitize takes none; a persisted Opts map
//     with content is a corrupt column value, and D-10 already classifies
//     that as terminal for every engine.
//   - Page count over PROBE_MAX_PAGES -- TERMINAL, and it is the branch worth
//     reading next to the timeout above rather than on its own. Both refuse an
//     over-long document; they are opposites about WHY. A timeout says the box
//     was busy, which the next attempt may not be. A page count says the file
//     has more pages than this service sanitizes, which is a property of the
//     file: a retry reproduces it exactly, three times, at ten minutes each.
//     That is the whole reason the ceiling was added (63-CONTEXT.md D-04) and
//     the reason a test pins the two verdicts together rather than separately.
//
// Everything else falls through to isTerminal's shared storage/input
// classification.
var ProbeDescriptor = EngineDescriptor{
	Engine: convert.EngineProbe,
	Type:   queue.TypeProbeSanitize,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.SanitizeOptsFromMap(m)
	},
	IsTerminal:  isProbeTerminal,
	FailureCode: probeFailureCode,
	SideOutputs: probeSideOutputs,
}

// isProbeTerminal classifies a sanitize failure. See ProbeDescriptor's doc
// comment for the argument behind each branch; this is only the mechanism.
func isProbeTerminal(err error) bool {
	if err == nil {
		return false
	}
	// A node that cannot confine the engine is retried elsewhere, never
	// blamed on the document: this MUST precede every check below, because
	// classifyEngineFailure/unverifiableEngineFailure preserve the sentinel
	// unwrapped, but the wrapped text can still carry this class's own
	// terminal shape (e.g. ErrSanitizeMalformedInput) alongside it.
	if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
		return false
	}
	// Verification failure: deterministic on the output, which is
	// deterministic on the input. Both outcomes -- residue found, and could
	// not be verified -- are terminal, and for the same reason.
	if errors.Is(err, convert.ErrSanitizeResidue) || errors.Is(err, convert.ErrSanitizeUnverifiable) {
		return true
	}
	// Malformed input: the engine refused to parse it, or a part this path
	// must rewrite is not well-formed. Deterministic on the input, so a retry
	// reproduces it exactly.
	//
	// This branch was CLAIMED by the doc comment above from the day this
	// descriptor was written and was not implemented until PR #12 round 4:
	// these errors fell through to isTerminal, which knows nothing about
	// ghostscript or the sanitizer and returned false. Asynq then exhausted
	// its retries without MarkFailed ever running, leaving the job ACTIVE for
	// the reconciler to exhaust over repeated 15-minute cycles -- a job that
	// looks in-flight for an hour and never reports why it died.
	if errors.Is(err, convert.ErrSanitizeMalformedInput) {
		return true
	}
	// Page count over the ceiling: deterministic on the input, so a retry
	// spends PROBE_ENGINE_TIMEOUT reproducing it. Deliberately the OPPOSITE
	// verdict from the deadline that falls through below, on the same
	// engine, and the difference is what the failure depends on.
	if errors.Is(err, convert.ErrProbeTooManyPages) {
		return true
	}
	// A ghostscript timeout is the one failure here that depends on load
	// rather than on the file, so it stays TRANSIENT -- falling through to
	// isTerminal, which returns false for a DeadlineExceeded. This is the
	// explicit opposite of isPandocTerminal/isDocumentTerminal, which route
	// through timeoutIsTerminal. classifyEngineFailure (sanitizepdf.go) is what
	// keeps a timeout out of the branch above.
	return isTerminal(err)
}

// probeFailureCode maps a terminal sanitize error to the client-facing pair.
//
// The two verification outcomes get DIFFERENT messages even though both are
// terminal, because they call for different actions: residue means the file
// still carries something and the service is at fault, while unverifiable
// means the service could not check and is refusing to say the file is
// anything. Collapsing them into one message would hide that difference at
// the only place a client ever sees it.
func probeFailureCode(err error) (code, message string) {
	if errors.Is(err, convert.ErrSanitizeResidue) {
		return CodeEngineError, "sanitized output still contained a construct that had to be removed"
	}
	if errors.Is(err, convert.ErrSanitizeUnverifiable) {
		return CodeEngineError, "sanitized output could not be independently verified and was discarded"
	}
	if errors.Is(err, convert.ErrSanitizeMalformedInput) {
		return CodeEngineError, "the uploaded document could not be parsed for sanitization"
	}
	// The SAME code the OCR path already returns for the identical condition
	// (ocrFailureCode, worker.go). Two codes for one condition is a vocabulary
	// defect -- a client should not have to learn that "too many pages" is
	// spelled one way when it was an OCR job and another way when it was a
	// sanitize job (63-CONTEXT.md D-04).
	if errors.Is(err, convert.ErrProbeTooManyPages) {
		return CodeTooManyPages, "the document has more pages than this service will sanitize"
	}
	if strings.Contains(strings.ToLower(err.Error()), "takes no options") {
		return CodeInvalidOptions, "stored conversion options are invalid"
	}
	return CodeEngineError, "unsupported or corrupted input format"
}

// probeSideOutputs reads back the report the sanitize converter wrote beside
// the primary output and hands it to process() as the job's role=report
// output.
//
// A missing sidecar is an ERROR, not a job without a report. The converter
// writes it on every successful path, so its absence means either the
// converter changed without this hook changing with it, or something removed
// the file -- and in both cases a silent success would deliver a sanitized
// document with no statement of what was removed from it.
func probeSideOutputs(_ context.Context, _ *jobs.Job, outPath string) ([]SideOutput, error) {
	blob, err := os.ReadFile(convert.SanitizeReportPath(outPath))
	if err != nil {
		return nil, fmt.Errorf("sanitize report missing beside the output: %w", err)
	}
	return []SideOutput{{
		Role:        jobs.OutputRoleReport,
		Filename:    "report.json",
		Format:      "json",
		ContentType: "application/json",
		Body:        blob,
	}}, nil
}

// TectonicDescriptor is the ninth engine class.
//
// TERMINAL/TRANSIENT, ARGUED RATHER THAN COPIED
//
// Phase 56's descriptors show polarity genuinely disagrees between neighbours
// -- isAudioTerminal treats an ffmpeg-stage timeout as terminal and a
// whisper-stage one as transient, isAVTerminal has the opposite shape -- so
// copying a neighbour's classification is the same antipattern as copying its
// timeout:
//
//   - LaTeX compilation error (undefined control sequence, unbalanced braces,
//     a missing package) -- TERMINAL. Deterministic on the document. A retry
//     re-runs the same engine over the same bytes and reaches the same verdict;
//     spending the budget on it buys three identical failures.
//
//   - Engine TIMEOUT -- TERMINAL, and this is the branch that CHANGED on
//     evidence. It shipped transient, reasoned by analogy with OCR and probe:
//     an external engine whose worst case is load rather than content. The
//     comment carried its own falsifier -- "only true while the timeout is
//     generous relative to a document's real pass count; if 64-05's measurement
//     shows otherwise, the honest response is a bound on the input, not
//     flipping this branch." Both halves of that have now happened.
//
//     64-05 measured the largest ADMISSIBLE document at 180.3s against a 271s
//     bound: a 1.5x margin. A timeout therefore no longer means "the box was
//     busy"; it means the document is doing something the measurement did not
//     see. And the input bound the comment prescribed was added (2 MiB) and
//     does NOT cover this case, because the cost driver is macro expansion, not
//     source size:
//
//     \count255=0 \loop\advance\count255 by 1 \ifnum\count255<100000000 \repeat
//
//     is a few hundred bytes and runs forever -- measured, still running when
//     killed at 60s in the worker image (PR #16 review, codex P1). Left
//     transient, asynq spends TECTONIC_MAX_RETRY+1 = 3 attempts x 271s = 813
//     seconds of a concurrency-1 worker on one such upload, and the class
//     scales to two replicas.
//
//     So the retry budget was being spent on the one failure here that can
//     never succeed. A legitimate document at the ceiling that times out is
//     possible but requires the box to be 50% slower than the measurement, on
//     a document already at the largest admissible size; that client resubmits.
//     A document that cannot finish is refused on the first attempt instead of
//     occupying the class for a quarter of an hour.
//
//   - No bundle configured -- TERMINAL, and NOT a document failure. The
//     converter refuses rather than falling back to a network fetch, so this
//     means the process is misconfigured. Retrying a misconfiguration is
//     pointless, and the message says which knob.
//
// Everything else falls through to isTerminal's shared storage/input
// classification.
var TectonicDescriptor = EngineDescriptor{
	Engine: convert.EngineTectonic,
	Type:   queue.TypeTectonicConvert,
	// This engine takes no options, so a populated map is a corrupt column
	// value -- D-10's terminal case, and the same strict refusal probe uses.
	// Without it, Runner.Handle skips the read-side check and the job is marked
	// DONE with its persisted semantics silently discarded (PR #16, codex P2).
	// image's nil OptsFromMap is the older precedent and is pinned as an
	// exception; probe is the one worth copying here.
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.TeXOptsFromMap(m)
	},
	IsTerminal:  isTectonicTerminal,
	FailureCode: tectonicFailureCode,
}

// isTectonicTerminal classifies a typesetting failure. See TectonicDescriptor's
// doc comment for the argument behind each branch; this is only the mechanism.
func isTectonicTerminal(err error) bool {
	if err == nil {
		return false
	}
	// NOT terminal, and stated before everything below so no later branch can
	// claim it: the sandbox could not be established, which says nothing about
	// the document. Every other tectonic failure here is a property of the
	// source and therefore reproduces; this one is a property of the NODE, and
	// left terminal a single bad node would fail every tex job routed to it
	// and tell each client their file is broken.
	//
	// The literal `return false` rather than a fall-through: the signature
	// scan at the bottom matches on substrings of the error text, and the text
	// here carries the helper's stderr, which this code does not author.
	if errors.Is(err, convert.ErrTeXSandboxUnavailable) {
		return false
	}
	// NOT terminal, and stated beside the sandbox fault because it is the same
	// kind of claim: the wall-clock deadline fired while the engine had barely
	// run, which is a fact about the NODE. Measured in the shipped image at a
	// 2-CPU quota, the same document took 0.96s wall for 0.95s of CPU idle and
	// 3.67s wall for 1.08s of CPU under 4x oversubscription -- wall inflated
	// 4x, consumption did not move. Terminal, a busy node would tell every
	// client their document failed to typeset and retry none of them.
	if errors.Is(err, convert.ErrTeXEngineStarved) {
		return false
	}
	// Terminal, and on stronger evidence than the deadline: this is what the
	// document COST, measured by the kernel and independent of what else the
	// node was doing. It costs the same on the next attempt.
	if errors.Is(err, convert.ErrTeXCPUExhausted) {
		return true
	}
	// A misconfigured process, not a bad document. Deliberately checked BEFORE
	// the shared classifier, which knows nothing about bundles and would let
	// this retry forever.
	if errors.Is(err, convert.ErrTeXBundleUnusable) {
		return true
	}
	// A package the bundle does not have (TEX-03). Terminal for the same
	// reason a compilation error is -- it is missing on every attempt -- but
	// checked explicitly rather than left to the signature list below, because
	// this is the one failure here that names something the client can change.
	if errors.Is(err, convert.ErrTeXUnsupportedPackage) {
		return true
	}
	// Both are properties of the DOCUMENT, so both reproduce exactly: a
	// bibliography that needs biber needs it on every attempt, and a reference
	// that does not resolve will not resolve on a retry either.
	if errors.Is(err, convert.ErrTeXBiberUnavailable) || errors.Is(err, convert.ErrTeXUndefinedReferences) {
		return true
	}
	// Also a property of the document: the same source ships the same pages
	// out on every attempt, so a retry re-does 271 seconds of work to produce
	// the same oversized PDF and refuse it again.
	if errors.Is(err, convert.ErrTeXOutputTooLarge) {
		return true
	}
	// The ENGINE's deadline is terminal on this engine (see the descriptor's
	// doc comment for the measurement that changed it): the document is a
	// program, the timeout is 1.5x the largest admissible document's measured
	// cost, and a non-terminating document reproduces exactly on every retry.
	//
	// Keyed on convert's sentinel and NOT on context.DeadlineExceeded, which
	// this would otherwise catch for the wrong reason: process() bounds the
	// whole attempt -- download, convert, upload -- with one deadline, so a
	// stalled S3 or Postgres call raises the identical error. Treating that as
	// terminal reported an infrastructure stall to the client as a document
	// that failed to typeset, and skipped the retry that would have fixed it
	// (PR #16, codex round 2).
	//
	// Cancellation is NOT the same thing and keeps falling through: a canceled
	// job was canceled by a client or a shutdown, and isTerminal already knows
	// what that means.
	if errors.Is(err, convert.ErrTeXEngineTimeout) {
		return true
	}
	// Killed by the cgroup's OOM killer with time still on the clock. Same
	// argument as the deadline: concurrency is 1 and the container is fixed, so
	// a document that exhausts it exhausts it again -- and MEASURED, because
	// without this the retry loop is real: a 235-byte \message loop drove the
	// worker's memory 315 -> 1006 MiB, the child was killed, and the attempt
	// came back as a plain non-zero exit that asynq retried (PR #16, round 2).
	if errors.Is(err, convert.ErrTeXEngineKilled) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return isTerminal(err)
	}
	// The engine ran and refused the document. XeTeX's own wording; captured
	// from real output rather than predicted (64-02 evidence).
	msg := strings.ToLower(err.Error())
	for _, sig := range []string{
		"unrecoverable error",
		"latex error",
		"emergency stop",
		"produced an empty pdf",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return isTerminal(err)
}

// tectonicFailureCode maps a terminal typesetting error to the client-facing
// pair. 64-03 adds the unsupported-package branch, which is the one that names
// what the client must change.
func tectonicFailureCode(err error) (code, message string) {
	// Named first, because it is the only branch here whose message tells the
	// client what to DO. The name comes from the engine's own output
	// (convert.tectonicMissingPackage, written against a captured run), never
	// from a guess -- and when the output does not permit naming one reliably,
	// the parser returns "" and this branch is not reached at all.
	if pkg := convert.TeXUnsupportedPackageName(err); pkg != "" {
		// The SAME code cmd/api returns when its bounded preamble scan catches
		// this before the upload (errcode.UnsupportedPackage). One code space,
		// D-03: which side saw it depends only on where \begin{document} fell
		// in the client's file, which is not something they can act on.
		return CodeUnsupportedPackage, convert.TeXUnsupportedPackageMessage(pkg)
	}
	if errors.Is(err, convert.ErrTeXBundleUnusable) {
		// Deliberately NOT phrased as a fault in the document: it is not one,
		// and a client told "your file could not be typeset" would go looking
		// in the wrong place.
		//
		// Keyed on the sentinel, not on the env var's NAME appearing in the
		// error text. That substring matched only the "unset" wording, so a
		// bundle that was a directory, an unreadable mount or an empty file
		// fell through to the generic message -- a deployment fault reported as
		// a bad document, which is the exact thing this branch exists to
		// prevent. Found in this PR's own sweep after round 3 widened the ways
		// a bundle can be unusable.
		return CodeEngineError, "the typesetting engine is not configured on this deployment"
	}
	if errors.Is(err, convert.ErrTeXCPUExhausted) {
		// Says what ran out, and does NOT say "within the time limit": the
		// document was refused on what it consumed, which is a different
		// remedy -- a cheaper document rather than a faster one.
		return CodeEngineError, "the document used more processing time than this deployment allows"
	}
	if errors.Is(err, convert.ErrTeXEngineTimeout) {
		// Named separately from the generic typesetting failure because the
		// client's remedy is different: this document did not fail to compile,
		// it failed to FINISH, and the usual cause is a construct that expands
		// without bound rather than anything the engine could report.
		//
		// The sentinel, not context.DeadlineExceeded: only a deadline that
		// fired while the engine was running says anything about the document.
		return CodeEngineError, "the document did not finish typesetting within the time limit"
	}
	if errors.Is(err, convert.ErrTeXBiberUnavailable) {
		// The one failure here whose remedy is a single package option, so the
		// message says the option rather than describing the situation.
		return CodeEngineError, "biblatex requires the option backend=bibtex on this deployment: no biber is available"
	}
	if errors.Is(err, convert.ErrTeXEngineKilled) {
		// Says WHAT ran out, because "could not be typeset" would send the
		// client looking for a syntax error in a document that has none. The
		// remedy is a smaller document, not a correction.
		return CodeEngineError, "the document exhausted the memory available to the typesetting engine"
	}
	if errors.Is(err, convert.ErrTeXOutputTooLarge) {
		// Its own code rather than engine_error: nothing about the document is
		// malformed, and "could not be typeset" would send the client looking
		// for a mistake that is not there. The remedy is fewer pages.
		return CodeOutputTooLarge, "the typeset PDF is larger than this deployment accepts"
	}
	if errors.Is(err, convert.ErrTeXUndefinedReferences) {
		return CodeEngineError, "the document has undefined references or citations; the PDF would show ?? in their place"
	}
	return CodeEngineError, "the document could not be typeset"
}

var CADDescriptor = EngineDescriptor{
	Engine: convert.EngineCAD,
	Type:   queue.TypeCADJob,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.CADOptsFromMap(m)
	},
	IsTerminal:  isCADTerminal,
	FailureCode: cadFailureCode,
	SideOutputs: cadSideOutputs,
}

func cadSideOutputs(_ context.Context, job *jobs.Job, outPath string) ([]SideOutput, error) {
	if job.Operation != convert.OperationRender {
		return nil, nil
	}
	blob, err := os.ReadFile(convert.CADMeasurementsPath(outPath))
	if err != nil {
		return nil, fmt.Errorf("%w: measurements missing beside the render: %v", convert.ErrCADMeasurementsInvalid, err)
	}
	if _, err := measurements.Validate(blob); err != nil {
		return nil, fmt.Errorf("%w: %v", convert.ErrCADMeasurementsInvalid, err)
	}
	return []SideOutput{{
		Role:        jobs.OutputRoleMeasurements,
		Filename:    "measurements.json",
		Format:      "json",
		ContentType: "application/json",
		Body:        blob,
	}}, nil
}

func isCADTerminal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
		return false
	}
	for _, sentinel := range []error{
		convert.ErrCADInputTooLarge,
		convert.ErrCADTooComplex,
		convert.ErrCADUnreadable,
		convert.ErrCADEmptyResult,
		convert.ErrCADMeshTimeout,
		convert.ErrCADEngineKilled,
		convert.ErrCADRenderTimeout,
		convert.ErrCADRenderRejected,
		convert.ErrCADMeasurementsInvalid,
		convert.ErrCADNothingDrawable,
		convert.ErrCADDXFTimeout,
		convert.ErrCADRsvgTimeout,
		convert.ErrCADSVGRejected,
		convert.ErrCADPDFRejected,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return isTerminal(err)
}

func cadFailureCode(err error) (code, message string) {
	switch {
	case errors.Is(err, convert.ErrCADInputTooLarge):
		return CodeInputTooLarge, "the CAD file exceeds the maximum size this deployment accepts for its format"
	case errors.Is(err, convert.ErrCADTooComplex), errors.Is(err, convert.ErrCADEngineKilled):
		return CodeCADTooComplex, "the CAD model is too complex to convert; simplify it or split it into smaller parts"
	case errors.Is(err, convert.ErrCADUnreadable):
		return CodeCADUnreadable, "the CAD file could not be read; re-export it from the source application"
	case errors.Is(err, convert.ErrCADNothingDrawable):
		return CodeCADEmptyResult, "the drawing contains nothing that can be drawn: model space is empty, hidden or holds only 3D solids"
	case errors.Is(err, convert.ErrCADEmptyResult):
		return CodeCADEmptyResult, "the CAD file contains no geometry that can be converted to a mesh"
	case errors.Is(err, convert.ErrCADMeshTimeout):
		return CodeCADMeshTimeout, "the model took too long to convert to a mesh; simplify it or export fewer details"
	case errors.Is(err, convert.ErrCADRenderTimeout):
		return CodeCADTooComplex, "the model took too long to render; simplify it or export fewer details"
	case errors.Is(err, convert.ErrCADRenderRejected):
		return CodeCADEmptyResult, "the rendered preview shows no recognisable part"
	case errors.Is(err, convert.ErrCADMeasurementsInvalid):
		return CodeEngineError, "the measurements of this model could not be produced"
	case errors.Is(err, convert.ErrCADRenderFailed):
		return CodeEngineError, "the preview could not be rendered"
	case errors.Is(err, convert.ErrCADDXFTimeout):
		return CodeCADTooComplex, "the drawing took too long to draw; simplify it or export fewer details"
	case errors.Is(err, convert.ErrCADRsvgTimeout):
		return CodeCADTooComplex, "the drawing took too long to convert to PDF or PNG; simplify it or export fewer details"
	case errors.Is(err, convert.ErrCADSVGRejected):
		return CodeEngineError, "the drawing could not be converted into a safe vector file"
	case errors.Is(err, convert.ErrCADPDFRejected):
		return CodeCADEmptyResult, "the PDF of this drawing shows no recognisable content"
	case errors.Is(err, convert.ErrCADDrawingFailed):
		return CodeEngineError, "the drawing could not be produced"
	}
	return CodeEngineError, "unsupported or corrupted input format"
}
