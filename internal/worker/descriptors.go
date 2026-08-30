package worker

import (
	"errors"

	"github.com/apaderin/octoconv/internal/convert"
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
	CodeEngineError         = "engine_error"
	CodeInvalidOptions      = "invalid_options"
	CodeDurationExceeded    = "duration_exceeded"
	CodeTimecodeOutOfRange  = "timecode_out_of_range"
	CodeResolutionExceeded  = "resolution_exceeded"
	CodeNoVideoStream       = "no_video_stream"
	CodeTooManyPages        = "too_many_pages"
	CodePageTooLarge        = "page_too_large"
	CodeImageTooLarge       = "image_too_large"
	CodeUnsupportedLanguage = "unsupported_language"
	CodeUnsupportedTarget   = "unsupported_target"
	CodeInputTooLarge       = "input_too_large"
	CodeReconcilerExhausted = "reconciler_exhausted"
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
	CodeReconcilerExhausted,
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

// The seven package-level engine descriptors (D-09: never a
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
