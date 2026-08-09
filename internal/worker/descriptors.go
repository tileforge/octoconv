package worker

import (
	"errors"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/queue"
)

// Frozen async job-failure error codes (jobs.error_code) — D-04/D-05
// (56-CONTEXT.md): this phase closes ERRC-02's metrics gap and
// consolidates seven handlers into one Runner, but it does NOT rename or
// rationalize any of these fourteen values; every one carries the exact
// string it already had before this refactor. CodeEngineError is a
// deliberate catch-all SHARED by all seven engines' default fallback
// (D-05 explicitly freezes it against renaming). CodeReconcilerExhausted
// is the one code emitted OUTSIDE this package — its sole producer is
// internal/reconciler/reconciler.go, which plan 56-05 (not this plan) is
// responsible for updating to reference this constant; this plan does not
// touch internal/reconciler.
const (
	CodeEngineError         = "engine_error"
	CodeInvalidOptions      = "invalid_options"
	CodeDurationExceeded    = "duration_exceeded"
	CodeTimecodeOutOfRange  = "timecode_out_of_range"
	CodeResolutionExceeded  = "resolution_exceeded"
	CodeNoVideoStream       = "no_video_stream"
	CodeBornDigital         = "born_digital"
	CodeTooManyPages        = "too_many_pages"
	CodePageTooLarge        = "page_too_large"
	CodeImageTooLarge       = "image_too_large"
	CodeUnsupportedLanguage = "unsupported_language"
	CodeUnsupportedTarget   = "unsupported_target"
	CodeInputTooLarge       = "input_too_large"
	CodeReconcilerExhausted = "reconciler_exhausted"
)

// allFailureCodes is the single place this frozen fourteen-code vocabulary
// is enumerated — AllFailureCodes() is built from it, for plan 56-06's
// Go-to-doc sync test.
var allFailureCodes = []string{
	CodeEngineError,
	CodeInvalidOptions,
	CodeDurationExceeded,
	CodeTimecodeOutOfRange,
	CodeResolutionExceeded,
	CodeNoVideoStream,
	CodeBornDigital,
	CodeTooManyPages,
	CodePageTooLarge,
	CodeImageTooLarge,
	CodeUnsupportedLanguage,
	CodeUnsupportedTarget,
	CodeInputTooLarge,
	CodeReconcilerExhausted,
}

// AllFailureCodes returns every declared async job-failure code exactly
// once, in declaration order. The returned slice is a fresh copy on every
// call, so callers cannot mutate the package's closed vocabulary through
// it.
func AllFailureCodes() []string {
	out := make([]string, len(allFailureCodes))
	copy(out, allFailureCodes)
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
