package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/storage"
	"github.com/tileforge/octoconv/internal/webhook"
)

func TestIsTerminalStorageNoSuchKey(t *testing.T) {
	// internal/storage wraps every minio error via fmt.Errorf("...: %w", err);
	// isTerminal must unwrap through that chain to find the NoSuchKey code.
	raw := minio.ErrorResponse{Code: minio.NoSuchKey, Message: "The specified key does not exist."}
	wrapped := fmt.Errorf("download %q: %w", "uploads/x/0-in.png", raw)
	if !isTerminal(wrapped) {
		t.Fatal("expected isTerminal(NoSuchKey) = true")
	}
}

func TestIsTerminalNoConverter(t *testing.T) {
	err := fmt.Errorf("no converter for %s -> %s", "bmp", "webp")
	if !isTerminal(err) {
		t.Fatal("expected isTerminal(\"no converter for ...\") = true")
	}
}

func TestIsTerminalVipsSignatures(t *testing.T) {
	cases := []string{
		"convert: exit status 1: vips__file_read_signature: is not a known file format",
		"convert: exit status 1: Premature end of JPEG file",
		"convert: exit status 1: JPEG datastream contains no image",
	}
	for _, msg := range cases {
		if !isTerminal(errors.New(msg)) {
			t.Fatalf("expected isTerminal(%q) = true", msg)
		}
	}
}

func TestIsTerminalTransientDefault(t *testing.T) {
	cases := []error{
		errors.New("dial tcp: connection refused"),
		errors.New("context deadline exceeded"),
		fmt.Errorf("upload %q: %w", "results/x/0-out.webp", errors.New("connection reset by peer")),
		nil,
	}
	for _, err := range cases {
		if isTerminal(err) {
			t.Fatalf("expected isTerminal(%v) = false (broad-retry default, D-01)", err)
		}
	}
}

func TestIsTerminalLibreOfficeSignatures(t *testing.T) {
	cases := []string{
		"convert: libreoffice: output missing %PDF- magic bytes",
		"convert: libreoffice: output is empty",
		"convert: libreoffice: no export filter for docx -> mp3",
		"convert: libreoffice: output does not match expected container format odt",
		"convert: libreoffice: produced no output file for \"odt\": stat /work/in.odt: no such file or directory",
		"convert: libreoffice: pdf_profile requested for non-pdf target \"odt\"",
	}
	for _, msg := range cases {
		if !isTerminal(errors.New(msg)) {
			t.Fatalf("expected isTerminal(%q) = true", msg)
		}
	}
}

// TestIsTerminalTimeoutUnchanged asserts that a wrapped context.DeadlineExceeded
// (the shape a document/image engine timeout actually surfaces as) is STILL
// transient under the shared isTerminal — the image path (HandleImageConvert)
// must keep retrying its timeouts. Only the engine-scoped isDocumentTerminal
// diverges from this.
func TestIsTerminalTimeoutUnchanged(t *testing.T) {
	wrapped := fmt.Errorf("convert: %w", fmt.Errorf("soffice killed: %w", context.DeadlineExceeded))
	if isTerminal(wrapped) {
		t.Fatal("expected isTerminal(wrapped context.DeadlineExceeded) = false — image path must keep retrying timeouts")
	}
}

// TestIsCanceled pins the classifier (D-07) that keeps a job:cancel-killed
// attempt out of both MarkFailed and asynq retry: a context.Canceled error
// (bare or wrapped, mirroring how attemptCtx.Err() surfaces through
// exec.go's "%s killed: %w" and process()'s "convert: %w" wrapping) must
// classify true, while a context.DeadlineExceeded (the DISTINCT engine-
// timeout arm, timeoutIsTerminal) and a plain engine error must classify
// false.
func TestIsCanceled(t *testing.T) {
	canceledCases := []error{
		context.Canceled,
		fmt.Errorf("%w: killed", context.Canceled),
		fmt.Errorf("convert: %w", fmt.Errorf("vips killed: %w", context.Canceled)),
	}
	for _, err := range canceledCases {
		if !isCanceled(err) {
			t.Fatalf("expected isCanceled(%v) = true", err)
		}
	}

	notCanceledCases := []error{
		context.DeadlineExceeded,
		fmt.Errorf("convert: %w", fmt.Errorf("vips killed: %w", context.DeadlineExceeded)),
		errors.New("convert: exit status 1: some engine failure"),
		nil,
	}
	for _, err := range notCanceledCases {
		if isCanceled(err) {
			t.Fatalf("expected isCanceled(%v) = false", err)
		}
	}
}

func TestIsDocumentTerminal(t *testing.T) {
	// A DOCUMENT_ENGINE_TIMEOUT expiry (exec.go's process-group-kill shape,
	// preserved through libreoffice.go and process()'s %w wrapping) IS
	// terminal for the document engine — DOC-08's deliberate divergence.
	timeoutErr := fmt.Errorf("convert: %w", fmt.Errorf("soffice killed: %w", context.DeadlineExceeded))
	if !isDocumentTerminal(timeoutErr) {
		t.Fatal("expected isDocumentTerminal(wrapped context.DeadlineExceeded) = true (DOC-08)")
	}

	// Delegates to isTerminal for every non-timeout signature.
	terminalCases := []error{
		fmt.Errorf("no converter for %s -> %s", "docx", "png"),
		errors.New("convert: libreoffice: output is empty"),
		errors.New("convert: libreoffice: output missing %PDF- magic bytes"),
		errors.New("convert: libreoffice: no export filter for docx -> mp3"),
		errors.New("convert: libreoffice: output does not match expected container format odt"),
		fmt.Errorf("download %q: %w", "uploads/x/0-in.docx", minio.ErrorResponse{Code: minio.NoSuchKey}),
	}
	for _, err := range terminalCases {
		if !isDocumentTerminal(err) {
			t.Fatalf("expected isDocumentTerminal(%v) = true", err)
		}
	}

	transientCases := []error{
		errors.New("dial tcp: connection refused"),
		nil,
	}
	for _, err := range transientCases {
		if isDocumentTerminal(err) {
			t.Fatalf("expected isDocumentTerminal(%v) = false", err)
		}
	}
}

func TestIsTerminalChromiumSignatures(t *testing.T) {
	cases := []string{
		"convert: chromium: output missing %PDF- magic bytes",
		"convert: chromium: output is empty",
		// Live-observed (Plan 04): chromium-headless-shell can exit 0 while
		// writing no output file at all; validatePDF's os.Stat failure
		// surfaces as "chromium: stat output: stat <path>: no such file or
		// directory".
		"convert: chromium: stat output: stat /work/out.pdf: no such file or directory",
	}
	for _, msg := range cases {
		if !isTerminal(errors.New(msg)) {
			t.Fatalf("expected isTerminal(%q) = true", msg)
		}
	}
}

// TestIsTerminalVeraPDFSignatures proves D-06 (phase 23): both terminal
// veraPDF error strings ("pdf/a non-compliant" for a clean non-compliant
// report, "pdf/a validation error" for any invocation/parse failure --
// fail-closed) classify terminal via the shared isTerminal, regardless of
// how they arrive wrapped (validateDocumentOutput wraps with "libreoffice:
// %w", process() wraps with "convert: %w").
func TestIsTerminalVeraPDFSignatures(t *testing.T) {
	cases := []string{
		"convert: libreoffice: verapdf: pdf/a non-compliant: 6.6.2.1: the document catalog dictionary doesn't contain metadata key",
		"convert: libreoffice: verapdf: pdf/a validation error: parse machine-readable report: EOF",
	}
	for _, msg := range cases {
		if !isTerminal(errors.New(msg)) {
			t.Fatalf("expected isTerminal(%q) = true", msg)
		}
	}
}

// TestIsDocumentTerminalVeraPDFSignatures proves both veraPDF terminal
// signatures also classify terminal through isDocumentTerminal (the
// document engine's engine-scoped classifier that HandleDocumentConvert
// actually calls) -- fail-closed reaches MarkFailed+SkipRetry, not an asynq
// retry loop, and (per D-07) rides the existing
// {"engine_stderr": err.Error()} MarkFailed detail path with no new logging.
func TestIsDocumentTerminalVeraPDFSignatures(t *testing.T) {
	cases := []error{
		errors.New("convert: libreoffice: verapdf: pdf/a non-compliant: 6.2.4.3: DeviceRGB colour space is used without RGB output intent profile"),
		errors.New("convert: libreoffice: verapdf: pdf/a validation error: start verapdf: exec: \"verapdf\": executable file not found in $PATH"),
	}
	for _, err := range cases {
		if !isDocumentTerminal(err) {
			t.Fatalf("expected isDocumentTerminal(%v) = true", err)
		}
	}
}

func TestIsHTMLTerminal(t *testing.T) {
	// An HTML_ENGINE_TIMEOUT expiry (exec.go's process-group-kill shape,
	// preserved through chromium.go and process()'s %w wrapping) IS terminal
	// for the html engine — HTML-01's deliberate divergence, mirroring
	// isDocumentTerminal's DOC-08 shape.
	timeoutErr := fmt.Errorf("convert: %w", fmt.Errorf("chromium-headless-shell killed: %w", context.DeadlineExceeded))
	if !isHTMLTerminal(timeoutErr) {
		t.Fatal("expected isHTMLTerminal(wrapped context.DeadlineExceeded) = true (HTML-01)")
	}

	// Delegates to isTerminal for every non-timeout signature.
	terminalCases := []error{
		fmt.Errorf("no converter for %s -> %s", "html", "png"),
		errors.New("convert: chromium: output is empty"),
		errors.New("convert: chromium: output missing %PDF- magic bytes"),
		fmt.Errorf("download %q: %w", "uploads/x/0-in.html", minio.ErrorResponse{Code: minio.NoSuchKey}),
	}
	for _, err := range terminalCases {
		if !isHTMLTerminal(err) {
			t.Fatalf("expected isHTMLTerminal(%v) = true", err)
		}
	}

	transientCases := []error{
		errors.New("dial tcp: connection refused"),
		nil,
	}
	for _, err := range transientCases {
		if isHTMLTerminal(err) {
			t.Fatalf("expected isHTMLTerminal(%v) = false", err)
		}
	}
}

// TestIsAudioTerminal proves Key Decision 1's stage-aware split (SC2,
// BINDING per STATE.md — the superseded blanket-timeoutIsTerminal shape must
// NOT be re-litigated): a ffmpeg-stage failure/timeout is terminal (a
// malformed/adversarial input signal); a whisper-stage timeout on audio that
// already passed the ffprobe duration/format check is transient, bounded by
// AUDIO_MAX_RETRY; a duration-guard rejection (ErrAudioDurationExceeded) is
// always terminal; every other error falls through to the shared isTerminal
// classifier (minio.NoSuchKey / "no converter for" / nil).
func TestIsAudioTerminal(t *testing.T) {
	if isAudioTerminal(nil) {
		t.Fatal("expected isAudioTerminal(nil) = false")
	}

	// ffmpeg-stage timeout -> terminal (malformed/adversarial input signal).
	ffmpegTimeout := fmt.Errorf("convert: %w", fmt.Errorf("audio: ffmpeg: %w", fmt.Errorf("ffmpeg killed: %w", context.DeadlineExceeded)))
	if !isAudioTerminal(ffmpegTimeout) {
		t.Fatal("expected isAudioTerminal(ffmpeg-stage timeout) = true (Key Decision 1)")
	}

	// ffmpeg-stage non-timeout failure -> terminal.
	ffmpegFailure := fmt.Errorf("convert: %w", fmt.Errorf("audio: ffmpeg: %w", errors.New("exit status 1: Invalid data found when processing input")))
	if !isAudioTerminal(ffmpegFailure) {
		t.Fatal("expected isAudioTerminal(ffmpeg-stage failure) = true (Key Decision 1)")
	}

	// whisper-stage timeout on already-duration-validated audio -> transient
	// (the distinguishing SC2 case: this must NOT match the ffmpeg-stage
	// terminal signature, and must NOT delegate to timeoutIsTerminal's
	// blanket DeadlineExceeded-is-terminal shape).
	whisperTimeout := fmt.Errorf("convert: %w", fmt.Errorf("audio: whisper-cli: %w", fmt.Errorf("whisper-cli killed: %w", context.DeadlineExceeded)))
	if isAudioTerminal(whisperTimeout) {
		t.Fatal("expected isAudioTerminal(whisper-stage timeout) = false (Key Decision 1 — bounded by AUDIO_MAX_RETRY)")
	}

	// ErrAudioDurationExceeded -> always terminal.
	durationErr := fmt.Errorf("convert: %w", convert.ErrAudioDurationExceeded)
	if !isAudioTerminal(durationErr) {
		t.Fatal("expected isAudioTerminal(ErrAudioDurationExceeded) = true")
	}

	// Deterministic ffprobe-stage failures -> terminal (WR-01): the duration
	// guard's probe runs BEFORE ffmpeg on the same untrusted input, so a
	// corrupt-but-sniffable file (valid magic bytes, broken container
	// metadata) must fail terminal at the very first probe, not burn the
	// AUDIO_MAX_RETRY + reconciler budget. Shapes mirror audioduration.go's
	// actual wrapping: ProbeDuration wraps runCommand errors with
	// "ffprobe: %w" (yielding exec.go's "ffprobe failed:" on a non-zero
	// exit) and parseProbedDuration emits the unparseable/implausible
	// rejections directly.
	ffprobeTerminalCases := []error{
		errors.New("ffprobe: ffprobe failed: exit status 1: moov atom not found"),
		errors.New(`ffprobe: unparseable duration "N/A": strconv.ParseFloat: parsing "N/A": invalid syntax`),
		errors.New("ffprobe: implausible duration -1"),
	}
	for _, err := range ffprobeTerminalCases {
		if !isAudioTerminal(err) {
			t.Fatalf("expected isAudioTerminal(%v) = true (deterministic ffprobe-stage failure, WR-01)", err)
		}
	}

	// ffprobe environment/timeout shapes -> transient (WR-01's documented
	// split): a missing binary or a probe-ctx expiry is a deployment/load
	// problem, not an input problem — retry may succeed.
	ffprobeTransientCases := []error{
		fmt.Errorf("ffprobe: %w", errors.New(`start ffprobe: exec: "ffprobe": executable file not found in $PATH`)),
		fmt.Errorf("ffprobe: %w", fmt.Errorf("ffprobe killed: %w", context.DeadlineExceeded)),
	}
	for _, err := range ffprobeTransientCases {
		if isAudioTerminal(err) {
			t.Fatalf("expected isAudioTerminal(%v) = false (ffprobe environment/timeout shape stays transient, WR-01)", err)
		}
	}

	// Shared base classifier fallthrough.
	sharedCases := []error{
		fmt.Errorf("no converter for %s -> %s", "mp3", "txt"),
		fmt.Errorf("download %q: %w", "uploads/x/0-in.mp3", minio.ErrorResponse{Code: minio.NoSuchKey}),
	}
	for _, err := range sharedCases {
		if !isAudioTerminal(err) {
			t.Fatalf("expected isAudioTerminal(%v) = true (shared isTerminal fallthrough)", err)
		}
	}

	// Non-timeout whisper-cli failure -> transient by default (31-RESEARCH.md
	// A2, adopted: input to whisper-cli is a server-produced normalized WAV,
	// so a non-timeout failure is most plausibly environment/config).
	whisperFailure := fmt.Errorf("convert: %w", fmt.Errorf("audio: whisper-cli: %w", errors.New("exit status 1: failed to load model")))
	if isAudioTerminal(whisperFailure) {
		t.Fatal("expected isAudioTerminal(non-timeout whisper-cli failure) = false (transient default)")
	}

	if isAudioTerminal(errors.New("dial tcp: connection refused")) {
		t.Fatal("expected isAudioTerminal(transient network error) = false")
	}

	// WR-04: whisper.go's pre-ffmpeg minimum-budget guard fires when an
	// upstream stage (stalled S3 download) pre-consumed the whole-attempt
	// deadline. Its distinct error shape deliberately carries NO
	// "audio: ffmpeg:" prefix, so it must classify transient (asynq retries
	// the network-transient attempt) instead of the terminal
	// corrupt-input misclassification the guard exists to prevent.
	budgetErr := fmt.Errorf("convert: %w", fmt.Errorf("audio: insufficient attempt budget remaining: %w", context.DeadlineExceeded))
	if isAudioTerminal(budgetErr) {
		t.Fatal("expected isAudioTerminal(insufficient-budget error) = false (upstream budget exhaustion is transient, WR-04)")
	}
}

// TestIsAudioTerminalOutputSignatures pins WR-03: validateAudioOutput's
// exit-0-but-no-output shapes ("audio: output is empty", "audio: stat
// output", whisper.go) classify terminal via audio's OWN
// terminalAudioSignatures list — NOT by accidentally substring-matching the
// LibreOffice/Chromium lists' "output is empty"/"stat output" entries inside
// the shared isTerminal loop, which is how they classified before WR-03. The
// second half proves the independence directly: with the foreign lists
// emptied, the audio messages must STILL classify terminal, so rewording a
// LibreOffice/Chromium signature can never silently flip audio retry
// behavior.
func TestIsAudioTerminalOutputSignatures(t *testing.T) {
	cases := []error{
		errors.New("convert: audio: output is empty"),
		errors.New("convert: audio: stat output: stat /work/out.txt: no such file or directory"),
	}
	for _, err := range cases {
		if !isAudioTerminal(err) {
			t.Fatalf("expected isAudioTerminal(%v) = true (validateAudioOutput no-output shape, WR-03)", err)
		}
	}

	// Independence proof: the classification must not depend on another
	// engine's signature list. Empty the foreign lists and re-check.
	savedLO, savedCh := terminalLibreOfficeSignatures, terminalChromiumSignatures
	terminalLibreOfficeSignatures, terminalChromiumSignatures = nil, nil
	defer func() {
		terminalLibreOfficeSignatures, terminalChromiumSignatures = savedLO, savedCh
	}()
	for _, err := range cases {
		if !isAudioTerminal(err) {
			t.Fatalf("expected isAudioTerminal(%v) = true even with the LibreOffice/Chromium signature lists emptied — audio must classify via terminalAudioSignatures, not a foreign list (WR-03)", err)
		}
	}
}

// TestIsAVTerminal pins D-02 (35-CONTEXT.md, BINDING): a stage-aware
// transient/terminal split derived FRESH for video, NOT a copy of
// isAudioTerminal's blanket "any ffmpeg-stage failure/timeout is terminal"
// rule. The distinguishing case is the transcode-timeout row: audio's rule
// would classify it terminal (ffmpeg is audio's cheap stage), but av's
// transcode is the expensive stage, so a timeout there must stay transient.
func TestIsAVTerminal(t *testing.T) {
	if isAVTerminal(nil) {
		t.Fatal("expected isAVTerminal(nil) = false")
	}

	// D-02's single most important row: ErrAVTranscodeFailed wrapping
	// context.DeadlineExceeded -> transient. This is the one assertion that
	// distinguishes isAVTerminal from isAudioTerminal.
	transcodeTimeout := fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVTranscodeFailed, fmt.Errorf("ffmpeg killed: %w", context.DeadlineExceeded)))
	if isAVTerminal(transcodeTimeout) {
		t.Fatal("D-02: expected isAVTerminal(transcode timeout) = false (transient — transcode is the expensive stage)")
	}

	// ErrAVTranscodeFailed wrapping a non-timeout ffmpeg exit error ->
	// terminal.
	transcodeFailure := fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVTranscodeFailed, errors.New("exit status 1: Invalid data found when processing input")))
	if !isAVTerminal(transcodeFailure) {
		t.Fatal("D-02: expected isAVTerminal(transcode non-timeout failure) = true")
	}

	// ErrAVAudioExtractFailed and ErrAVThumbnailFailed -> terminal, both
	// with and without a wrapped context.DeadlineExceeded (cheap stages,
	// ANY failure is terminal).
	cheapStageCases := []error{
		fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVAudioExtractFailed, fmt.Errorf("ffmpeg killed: %w", context.DeadlineExceeded))),
		fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVAudioExtractFailed, errors.New("exit status 1"))),
		fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVThumbnailFailed, fmt.Errorf("ffmpeg killed: %w", context.DeadlineExceeded))),
		fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVThumbnailFailed, errors.New("exit status 1"))),
	}
	for _, err := range cheapStageCases {
		if !isAVTerminal(err) {
			t.Fatalf("D-02: expected isAVTerminal(%v) = true (cheap stage, timeout or not)", err)
		}
	}

	// Deterministic guard/output-validation rejections -> terminal
	// regardless of stage or timeout.
	deterministicCases := []error{
		fmt.Errorf("convert: %w", convert.ErrAVOutputMissingOrEmpty),
		fmt.Errorf("convert: %w", convert.ErrAVTimecodeOutOfRange),
		fmt.Errorf("convert: %w", convert.ErrAVResolutionExceeded),
		// 36-05 gap-closure (CR-01/HI-01): the generalized re-encode
		// source-resolution bound must be terminal like ErrAVResolutionExceeded
		// -- a rejected declared source resolution can never succeed on retry.
		fmt.Errorf("convert: %w", convert.ErrAVReencodeResolutionExceeded),
		fmt.Errorf("convert: %w", convert.ErrAudioDurationExceeded),
		fmt.Errorf("convert: %w", convert.ErrAVNoVideoStream),
	}
	for _, err := range deterministicCases {
		if !isAVTerminal(err) {
			t.Fatalf("D-02: expected isAVTerminal(%v) = true (deterministic rejection, always terminal)", err)
		}
	}

	// Unrecognized S3/Postgres blip errors fall through to isTerminal(err)
	// and match its verdict.
	sharedCases := []error{
		fmt.Errorf("no converter for %s -> %s", "mkv", "mp4"),
		fmt.Errorf("download %q: %w", "uploads/x/0-in.mp4", minio.ErrorResponse{Code: minio.NoSuchKey}),
	}
	for _, err := range sharedCases {
		if !isAVTerminal(err) {
			t.Fatalf("expected isAVTerminal(%v) = true (shared isTerminal fallthrough)", err)
		}
	}
	if isAVTerminal(errors.New("dial tcp: connection refused")) {
		t.Fatal("expected isAVTerminal(transient network error) = false")
	}

	// Contrast row: isAudioTerminal and isAVTerminal must DISAGREE on a
	// transcode-timeout-shaped error, so a future refactor that collapses
	// the two classifiers fails loudly. isAudioTerminal keys on the literal
	// "audio: ffmpeg:" prefix, so this row is constructed with that exact
	// shape wrapping the same underlying context.DeadlineExceeded, to prove
	// the two rules are genuinely independent, not just differently-typed
	// spellings of the same behavior.
	audioShapedTimeout := fmt.Errorf("convert: %w", fmt.Errorf("audio: ffmpeg: %w", fmt.Errorf("ffmpeg killed: %w", context.DeadlineExceeded)))
	if !isAudioTerminal(audioShapedTimeout) {
		t.Fatal("contrast setup: expected isAudioTerminal(audio-shaped ffmpeg timeout) = true")
	}
	if isAVTerminal(transcodeTimeout) == isAudioTerminal(audioShapedTimeout) {
		t.Fatal("D-02 contrast: isAVTerminal(transcode timeout) and isAudioTerminal(ffmpeg-stage timeout) must DISAGREE — isAVTerminal must not become a copy of isAudioTerminal")
	}
}

// TestEnforceAudioGuardBeforeConvert_IN02 pins T-30-08/IN-02: an audio job
// whose downloaded file exceeds the configured ceiling is rejected BEFORE
// Convert runs (the guard runs strictly before conv.Convert, not merely
// documented as running before it), and the resulting error classifies
// terminal via isAudioTerminal. process()'s h.repo/h.store are concrete
// types (not interfaces, per ARCHITECTURE.md's "Key Abstractions" note), so
// a full Handler cannot be constructed without live Postgres/S3 — this test
// drives enforceAudioGuardBeforeConvert directly, the smallest testable
// seam process() delegates to. Gated on a real ffprobe binary (mirrors
// internal/convert's requireLiveAudioBinaries skip-gate convention) since
// EnforceMaxDuration shells out to ffprobe — there is no fake seam for it.
func TestEnforceAudioGuardBeforeConvert_IN02(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH; see Plan 01's \"Local Development Setup\"")
	}

	fixture := filepath.Join("..", "convert", "testdata", "audio", "jfk.wav") // ~11s WAV
	convertCalled := false
	convertFn := func() error {
		convertCalled = true
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Ceiling of 1s is well below jfk.wav's ~11s declared duration.
	err := enforceAudioGuardBeforeConvert(ctx, convert.EngineAudio, fixture, 1*time.Second, nil, convertFn)
	if err == nil {
		t.Fatal("enforceAudioGuardBeforeConvert(over-ceiling audio) = nil, want ErrAudioDurationExceeded")
	}
	if !errors.Is(err, convert.ErrAudioDurationExceeded) {
		t.Fatalf("enforceAudioGuardBeforeConvert error = %v, want errors.Is ErrAudioDurationExceeded", err)
	}
	if convertCalled {
		t.Fatal("convertFn was called despite the duration guard rejecting the input — guard did not run before Convert (IN-02)")
	}
	if !isAudioTerminal(err) {
		t.Fatalf("isAudioTerminal(%v) = false, want true (duration-guard rejection always classifies terminal)", err)
	}

	// Non-audio engines never invoke the guard: a generously-small ceiling
	// (0, the zero value every non-audio worker cmd passes) must NOT reject
	// an image/document/html job — convertFn always runs.
	convertCalled = false
	if err := enforceAudioGuardBeforeConvert(ctx, convert.EngineImage, fixture, 0, nil, convertFn); err != nil {
		t.Fatalf("enforceAudioGuardBeforeConvert(non-audio engine) = %v, want nil (guard must not run)", err)
	}
	if !convertCalled {
		t.Fatal("convertFn was not called for a non-audio engine — gate must be job.Engine == convert.EngineAudio only")
	}
}

// TestEnforceAudioGuardProbeExpiryTransient_WR02 pins the WR-01/WR-02
// interaction: when the guard's probe ctx expires (here forced via an
// already-expired parent — audioProbeTimeout derives from the parent, so the
// child deadline is expired too), the resulting "ffprobe: ffprobe killed:"
// shape must classify TRANSIENT via isAudioTerminal (a hung probe is a
// load/environment problem, not an input problem — retry may succeed), and
// convertFn must never run. Gated on a real ffprobe binary since
// EnforceMaxDuration shells out to it — there is no fake seam.
func TestEnforceAudioGuardProbeExpiryTransient_WR02(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH; see Plan 01's \"Local Development Setup\"")
	}

	fixture := filepath.Join("..", "convert", "testdata", "audio", "jfk.wav")
	convertCalled := false

	expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := enforceAudioGuardBeforeConvert(expiredCtx, convert.EngineAudio, fixture, 4*time.Hour, nil, func() error {
		convertCalled = true
		return nil
	})
	if err == nil {
		t.Fatal("enforceAudioGuardBeforeConvert(expired probe ctx) = nil, want ffprobe-killed error")
	}
	if !strings.Contains(err.Error(), "ffprobe killed:") {
		t.Fatalf("enforceAudioGuardBeforeConvert(expired probe ctx) error = %v, want the \"ffprobe killed:\" ctx-expiry shape", err)
	}
	if convertCalled {
		t.Fatal("convertFn ran despite the probe failing — guard must run strictly before Convert")
	}
	if isAudioTerminal(err) {
		t.Fatalf("isAudioTerminal(%v) = true, want false (probe-ctx expiry stays transient, WR-01/WR-02)", err)
	}
}

// TestAVFailureCode pins HandleAVConvert's terminal-path error_code mapping
// (D-09/IN-01) without requiring a live Postgres/S3 Handler: avFailureCode
// is the pure function HandleAVConvert delegates to, so this test exercises
// the mapping directly.
func TestAVFailureCode(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "timecode out of range",
			err:      fmt.Errorf("convert: %w", convert.ErrAVTimecodeOutOfRange),
			wantCode: "timecode_out_of_range",
		},
		{
			name:     "duration exceeded",
			err:      fmt.Errorf("convert: %w", convert.ErrAudioDurationExceeded),
			wantCode: "duration_exceeded",
		},
		{
			name:     "resolution exceeded",
			err:      fmt.Errorf("convert: %w", convert.ErrAVResolutionExceeded),
			wantCode: "resolution_exceeded",
		},
		{
			name:     "no video stream",
			err:      fmt.Errorf("convert: %w", convert.ErrAVNoVideoStream),
			wantCode: "no_video_stream",
		},
		{
			name:     "generic engine failure",
			err:      fmt.Errorf("convert: %w", convert.ErrAVTranscodeFailed),
			wantCode: "engine_error",
		},
	}
	for _, tc := range cases {
		code, message := avFailureCode(tc.err)
		if code != tc.wantCode {
			t.Errorf("%s: avFailureCode(%v) code = %q, want %q", tc.name, tc.err, code, tc.wantCode)
		}
		if message == "" {
			t.Errorf("%s: avFailureCode(%v) message is empty, want a fixed client-facing message", tc.name, tc.err)
		}
	}
}

// TestIsOCRTerminal pins D-04 (44-CONTEXT.md): every deterministic
// input/client-fault guard rejection is terminal, an OCR_ENGINE_TIMEOUT
// expiry is TRANSIENT (the single divergence from isDocumentTerminal/
// isHTMLTerminal, mirroring isAVTerminal's transcode-timeout treatment), and
// a genuine non-timeout tesseract/pdftoppm/pdfinfo crash is terminal (unlike
// isAudioTerminal's/isAVTerminal's default-to-shared-isTerminal treatment of
// an unrecognized non-timeout failure).
func TestIsOCRTerminal(t *testing.T) {
	if isOCRTerminal(nil) {
		t.Fatal("expected isOCRTerminal(nil) = false")
	}

	// Deterministic input/client-fault guard rejections -> terminal,
	// regardless of stage.
	deterministicCases := []error{
		fmt.Errorf("%w: declared page count 50 exceeds bound 20", convert.ErrOCRTooManyPages),
		fmt.Errorf("%w: declared page box 1000x1000 pts at 300 DPI rasterizes to 1e9 pixels, exceeds bound 1e8", convert.ErrOCRPageBoxTooLarge),
		fmt.Errorf("ocr: %w", convert.ErrOCRImageTooLarge),
		fmt.Errorf("ocr: %w", fmt.Errorf("unsupported language %q", "fra")),
		fmt.Errorf("ocr: unsupported target format %q", "bmp"),
	}
	for _, err := range deterministicCases {
		if !isOCRTerminal(err) {
			t.Fatalf("D-04: expected isOCRTerminal(%v) = true (deterministic input/client fault)", err)
		}
	}

	// D-04's single most important row: an OCR_ENGINE_TIMEOUT expiry (the
	// wrapped context.DeadlineExceeded shape exec.go's process-group kill
	// produces) is TRANSIENT -- this is the one assertion that distinguishes
	// isOCRTerminal from isDocumentTerminal/isHTMLTerminal.
	timeout := fmt.Errorf("convert: %w", fmt.Errorf("ocr: tesseract: %w", fmt.Errorf("tesseract killed: %w", context.DeadlineExceeded)))
	if isOCRTerminal(timeout) {
		t.Fatal("D-04: expected isOCRTerminal(engine timeout) = false (transient — retry may succeed when worker load drops)")
	}

	// Other non-timeout engine crashes -> terminal (D-04: OCR is
	// deterministic on its input, so it will recur).
	crashCases := []error{
		fmt.Errorf("convert: %w", fmt.Errorf("ocr: tesseract: %w", errors.New("exit status 1: Error in pixReadStream: Unknown format: extension not recognized"))),
		fmt.Errorf("convert: %w", fmt.Errorf("ocr: pdftoppm: %w", errors.New("exit status 1: Syntax Error"))),
		fmt.Errorf("convert: %w", fmt.Errorf("ocr: %w", fmt.Errorf("pdfinfo: %w", errors.New("exit status 1: Syntax Warning")))),
	}
	for _, err := range crashCases {
		if !isOCRTerminal(err) {
			t.Fatalf("D-04: expected isOCRTerminal(%v) = true (non-timeout engine crash is deterministic, will recur)", err)
		}
	}

	// Unrecognized S3/no-converter errors still fall through to the shared
	// isTerminal classifier and match its verdict.
	sharedCases := []error{
		fmt.Errorf("no converter for %s -> %s", "gif", "txt"),
		fmt.Errorf("download %q: %w", "uploads/x/0-in.pdf", minio.ErrorResponse{Code: minio.NoSuchKey}),
	}
	for _, err := range sharedCases {
		if !isOCRTerminal(err) {
			t.Fatalf("expected isOCRTerminal(%v) = true (shared isTerminal fallthrough)", err)
		}
	}
	if isOCRTerminal(errors.New("dial tcp: connection refused")) {
		t.Fatal("expected isOCRTerminal(transient network error) = false")
	}
}

// TestOCRFailureCode pins HandleOCRConvert's terminal-path error_code
// mapping (mirrors TestAVFailureCode's precedent): ocrFailureCode is the pure
// function HandleOCRConvert delegates to, so this test exercises the mapping
// directly without a live Postgres/S3 Handler.
func TestOCRFailureCode(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "too many pages",
			err:      fmt.Errorf("%w: declared page count 50 exceeds bound 20", convert.ErrOCRTooManyPages),
			wantCode: "too_many_pages",
		},
		{
			name:     "page box too large",
			err:      convert.ErrOCRPageBoxTooLarge,
			wantCode: "page_too_large",
		},
		{
			name:     "image too large",
			err:      fmt.Errorf("ocr: %w", convert.ErrOCRImageTooLarge),
			wantCode: "image_too_large",
		},
		{
			name:     "unsupported language",
			err:      fmt.Errorf("ocr: %w", fmt.Errorf("unsupported language %q", "fra")),
			wantCode: "unsupported_language",
		},
		{
			name:     "unsupported target",
			err:      fmt.Errorf("ocr: unsupported target format %q", "bmp"),
			wantCode: "unsupported_target",
		},
		{
			name:     "generic engine failure",
			err:      fmt.Errorf("ocr: tesseract: %w", errors.New("exit status 1")),
			wantCode: "engine_error",
		},
	}
	for _, tc := range cases {
		code, message := ocrFailureCode(tc.err)
		if code != tc.wantCode {
			t.Errorf("%s: ocrFailureCode(%v) code = %q, want %q", tc.name, tc.err, code, tc.wantCode)
		}
		if message == "" {
			t.Errorf("%s: ocrFailureCode(%v) message is empty, want a fixed client-facing message", tc.name, tc.err)
		}
	}
}

// --- Progress throttle / terminal-bypass / freeze-on-abort (PROG-03, D-01/D-03/D-03a/D-06) ---

// spyProgressCall records one UpdateProgress invocation for assertion.
type spyProgressCall struct {
	pct int
	eta *int
}

// spyProgressRepo is a fake progressRepo recording every call, so the
// throttle/bypass/freeze contract is testable without a live Postgres
// *jobs.Repo.
type spyProgressRepo struct {
	updates   []spyProgressCall
	durations []float64
	stages    []convert.StageUpdate
}

func (s *spyProgressRepo) UpdateStage(_ context.Context, _ uuid.UUID, stage string, index, count int) (bool, error) {
	s.stages = append(s.stages, convert.StageUpdate{Stage: stage, Index: index, Count: count})
	return true, nil
}

func (s *spyProgressRepo) UpdateProgress(_ context.Context, _ uuid.UUID, pct int, eta *int) error {
	s.updates = append(s.updates, spyProgressCall{pct: pct, eta: eta})
	return nil
}

func (s *spyProgressRepo) SetSourceDuration(_ context.Context, _ uuid.UUID, seconds float64) error {
	s.durations = append(s.durations, seconds)
	return nil
}

// fakeProgressConverter implements BOTH convert.Converter and
// convert.ProgressConverter so runConvertWithOptionalProgress's type-assert
// branch is exercisable without a live ffmpeg binary. ConvertWithProgress
// replays the configured updates through sink then returns err (nil for a
// success run, non-nil to simulate a mid-stream failure).
type fakeProgressConverter struct {
	updates     []convert.ProgressUpdate
	sourceSecs  float64
	err         error
	plainCalled bool
}

func (f *fakeProgressConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "mov", To: "mp4"}}
}
func (f *fakeProgressConverter) Engine() string { return convert.EngineAV }
func (f *fakeProgressConverter) Convert(_ context.Context, _, _ string, _ map[string]any) error {
	f.plainCalled = true
	return nil
}
func (f *fakeProgressConverter) ConvertWithProgress(_ context.Context, _, _ string, _ map[string]any, sink convert.ProgressSink) error {
	sink.SourceDuration(f.sourceSecs)
	for _, u := range f.updates {
		sink.Update(u)
	}
	return f.err
}

// fakeNonProgressConverter implements ONLY convert.Converter — proves the
// non-progress engines' plain conv.Convert path is untouched (byte-for-byte
// the same call) even when throttling IS configured on the Handler; the
// type-assert for convert.ProgressConverter simply fails for them (D-01).
type fakeNonProgressConverter struct {
	plainCalled bool
}

func (f *fakeNonProgressConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "png", To: "webp"}}
}
func (f *fakeNonProgressConverter) Engine() string { return convert.EngineImage }
func (f *fakeNonProgressConverter) Convert(_ context.Context, _, _ string, _ map[string]any) error {
	f.plainCalled = true
	return nil
}

func TestProgressThrottle(t *testing.T) {
	jobID := uuid.New()

	t.Run("rapid ticks within the window produce exactly one throttled write", func(t *testing.T) {
		repo := &spyProgressRepo{}
		conv := &fakeProgressConverter{
			sourceSecs: 120,
			updates: []convert.ProgressUpdate{
				{Pct: 10}, {Pct: 11}, {Pct: 12}, {Pct: 100},
			},
		}
		if _, err := runConvertWithOptionalProgress(context.Background(), conv, "in", "out", nil, repo, jobID, "", 2*time.Second, 1, false, "", nil, nil, 0); err != nil {
			t.Fatalf("runConvertWithOptionalProgress: %v", err)
		}
		if conv.plainCalled {
			t.Fatal("plain Convert was called; ConvertWithProgress should have been used")
		}
		if len(repo.durations) != 1 || repo.durations[0] != 120 {
			t.Fatalf("SourceDuration calls = %v, want exactly one call with 120", repo.durations)
		}
		// Ticks 11/12/100 all arrive within the same instant (no simulated
		// clock advance), so both the 2s-interval AND the 1%-delta gates
		// suppress all three after the first tick lands — until the
		// unconditional terminal-100 bypass write below.
		if len(repo.updates) != 2 {
			t.Fatalf("UpdateProgress calls = %+v, want exactly 2 (first throttled tick + terminal 100 bypass)", repo.updates)
		}
		if repo.updates[0].pct != 10 {
			t.Fatalf("first write pct = %d, want 10", repo.updates[0].pct)
		}
		if repo.updates[len(repo.updates)-1].pct != 100 {
			t.Fatalf("last write pct = %d, want 100 (terminal bypass)", repo.updates[len(repo.updates)-1].pct)
		}
	})

	t.Run("non-progress converter falls back to plain Convert unchanged", func(t *testing.T) {
		repo := &spyProgressRepo{}
		conv := &fakeNonProgressConverter{}
		if _, err := runConvertWithOptionalProgress(context.Background(), conv, "in", "out", nil, repo, jobID, "", 2*time.Second, 1, false, "", nil, nil, 0); err != nil {
			t.Fatalf("runConvertWithOptionalProgress: %v", err)
		}
		if !conv.plainCalled {
			t.Fatal("plain Convert was NOT called for a non-ProgressConverter engine")
		}
		if len(repo.updates) != 0 || len(repo.durations) != 0 {
			t.Fatalf("no progress writes expected for a non-progress converter, got updates=%v durations=%v", repo.updates, repo.durations)
		}
	})

	t.Run("progress converter with throttling disabled (minInterval<=0) uses plain Convert", func(t *testing.T) {
		repo := &spyProgressRepo{}
		conv := &fakeProgressConverter{sourceSecs: 60, updates: []convert.ProgressUpdate{{Pct: 50}}}
		if _, err := runConvertWithOptionalProgress(context.Background(), conv, "in", "out", nil, repo, jobID, "", 0, 0, false, "", nil, nil, 0); err != nil {
			t.Fatalf("runConvertWithOptionalProgress: %v", err)
		}
		if !conv.plainCalled {
			t.Fatal("plain Convert was NOT called when progress throttling is disabled (minInterval<=0)")
		}
		if len(repo.updates) != 0 || len(repo.durations) != 0 {
			t.Fatalf("no progress writes expected when throttling is disabled, got updates=%v durations=%v", repo.updates, repo.durations)
		}
	})
}

// TestProgressFreezeOnAbort pins D-06: on an error return from
// ConvertWithProgress mid-stream, the last throttled value stays frozen — no
// further UpdateProgress call of ANY kind (no terminal-100, no null-reset)
// happens after the error. This is proved with a spy repo at the unit level,
// not only asserted by construction.
func TestProgressFreezeOnAbort(t *testing.T) {
	repo := &spyProgressRepo{}
	conv := &fakeProgressConverter{
		sourceSecs: 90,
		updates:    []convert.ProgressUpdate{{Pct: 30}},
		err:        errors.New("simulated mid-stream ffmpeg failure"),
	}
	_, err := runConvertWithOptionalProgress(context.Background(), conv, "in", "out", nil, repo, uuid.New(), "", 2*time.Second, 1, false, "", nil, nil, 0)
	if err == nil {
		t.Fatal("runConvertWithOptionalProgress = nil error, want the simulated failure propagated")
	}
	if len(repo.updates) != 1 || repo.updates[0].pct != 30 {
		t.Fatalf("UpdateProgress calls = %+v, want exactly one (the throttled 30%% tick, no write after error — D-06 freeze)", repo.updates)
	}
}

// fakeBlankOCRConverter implements convert.Converter with a Convert that
// writes an EMPTY output file and returns nil -- mirrors OCRConverter's own
// OCRE-05 contract (validateOCROutput, internal/convert/ocr.go, deliberately
// does NOT check size>0: an empty txt/hocr/alto file is a VALID, successful
// result for an image/page with no recognizable text). This proves the seam
// HandleOCRConvert composes (runConvertWithOptionalProgress -> process()'s
// nil-error success path) treats a blank/text-free result as success
// regardless of output size, so it flows to h.repo.MarkDone, never
// MarkFailed -- process()'s h.repo/h.store are concrete types (not
// interfaces, ARCHITECTURE.md's "Key Abstractions" note), so a full Handler
// cannot be constructed in a pure unit test; this drives the exact seam
// process() delegates to, the smallest testable proof of the OCRE-05
// contract without a live Postgres/S3 Handler.
type fakeBlankOCRConverter struct{}

func (fakeBlankOCRConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "png", To: "txt"}}
}
func (fakeBlankOCRConverter) Engine() string { return convert.EngineOCR }
func (fakeBlankOCRConverter) Convert(_ context.Context, _, outPath string, _ map[string]any) error {
	return os.WriteFile(outPath, []byte(""), 0o644)
}

// TestOCRBlankPageConvertSucceeds_OCRE05 pins OCRE-05: a blank/text-free
// page must complete as done with empty output, never failed. Runs the
// fake blank converter through runConvertWithOptionalProgress -- the exact
// seam HandleOCRConvert's process() call uses -- and asserts a nil error
// (the branch HandleOCRConvert takes straight to h.repo.MarkDone, never
// h.repo.MarkFailed) alongside a genuinely empty output file.
func TestOCRBlankPageConvertSucceeds_OCRE05(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	repo := &spyProgressRepo{}

	_, err := runConvertWithOptionalProgress(context.Background(), fakeBlankOCRConverter{}, "in.png", outPath, nil, repo, uuid.New(), "", 0, 0, false, "", nil, nil, 0)
	if err != nil {
		t.Fatalf("runConvertWithOptionalProgress(blank-output OCR result) = %v, want nil (OCRE-05: blank page is a VALID success, not a failure)", err)
	}

	data, rerr := os.ReadFile(outPath)
	if rerr != nil {
		t.Fatalf("read output: %v", rerr)
	}
	if len(data) != 0 {
		t.Fatalf("output = %q, want empty (blank/text-free page)", data)
	}
}

// --- Detached best-effort progress-webhook fire (D-04, PUSH-02, T-42-07/T-42-08) ---

// spyProgressWebhookDeliverer is a fake progressWebhookDeliverer recording
// every call, so the opt-in gating and fire-count contracts are testable
// without a live HTTP round-trip.
type spyProgressWebhookDeliverer struct {
	calls int32
}

func (s *spyProgressWebhookDeliverer) Deliver(_ context.Context, _ string, _ []byte, _ int64, _ string) (int, error) {
	atomic.AddInt32(&s.calls, 1)
	return 200, nil
}

// TestProgressWebhookFire_NonBlocking pins Pitfall 5 / T-42-07: even when
// the callback_url endpoint hangs far longer than the configured
// webhookTimeout, progressSink.Update returns almost immediately — the
// detached goroutine's own context.WithTimeout(context.Background(), ...)
// bounds the HTTP attempt, and the caller (the ffmpeg -progress reader
// goroutine, in production) never waits on it.
func TestProgressWebhookFire_NonBlocking(t *testing.T) {
	var reqCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	repo := &spyProgressRepo{}
	sink := &progressSink{
		ctx: context.Background(), repo: repo, jobID: uuid.New(),
		webhookEnabled: true, callbackURL: srv.URL, webhookDelivr: webhook.NewDeliverer(),
		webhookSecret: []byte("s3cr3t"), webhookTimeout: 200 * time.Millisecond,
	}

	start := time.Now()
	sink.Update(convert.ProgressUpdate{Pct: 42})
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("Update took %v, want <50ms (the detached fire must never block on a slow callback endpoint)", elapsed)
	}

	// Give the detached goroutine time to actually reach the endpoint (its
	// own 200ms timeout fires well before the server's 2s sleep completes),
	// so the request-count assertion below isn't a false negative from the
	// goroutine simply not having started yet.
	time.Sleep(400 * time.Millisecond)
	if atomic.LoadInt32(&reqCount) == 0 {
		t.Fatal("detached fire never reached the callback endpoint")
	}
}

// TestProgressWebhookGating pins D-04's opt-in gate: the deliverer is
// invoked ONLY when webhookEnabled && callbackURL != "" && a non-nil
// deliverer is set. Any one of those three being absent must fire nothing.
func TestProgressWebhookGating(t *testing.T) {
	cases := []struct {
		name        string
		enabled     bool
		callbackURL string
		delivr      *spyProgressWebhookDeliverer
		nilDelivr   bool
	}{
		{name: "opted out (webhookEnabled=false)", enabled: false, callbackURL: "http://example.invalid", delivr: &spyProgressWebhookDeliverer{}},
		{name: "empty callback_url", enabled: true, callbackURL: "", delivr: &spyProgressWebhookDeliverer{}},
		{name: "nil deliverer", enabled: true, callbackURL: "http://example.invalid", nilDelivr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d progressWebhookDeliverer
			if !tc.nilDelivr {
				d = tc.delivr
			}
			sink := &progressSink{
				ctx: context.Background(), repo: &spyProgressRepo{}, jobID: uuid.New(),
				webhookEnabled: tc.enabled, callbackURL: tc.callbackURL, webhookDelivr: d,
				webhookSecret: []byte("s3cr3t"), webhookTimeout: 50 * time.Millisecond,
			}
			sink.Update(convert.ProgressUpdate{Pct: 10})
			time.Sleep(20 * time.Millisecond)
			if tc.delivr != nil && atomic.LoadInt32(&tc.delivr.calls) != 0 {
				t.Fatalf("%s: deliverer.Deliver called %d times, want 0 (not opted-in)", tc.name, tc.delivr.calls)
			}
		})
	}
}

// poisonedProgressRepo implements progressRepo AND additionally exposes
// EnqueueWebhookDeliver/RecordAttempt-shaped methods the progress path must
// NEVER call (T-42-08/PUSH-02 separation) — proved here at the call level
// (not just by grep), so a future refactor that widened progressRepo's
// contract to also reach an enqueuer would be caught by a failing test.
type poisonedProgressRepo struct {
	spyProgressRepo
	enqueueCalls int32
	recordCalls  int32
}

func (p *poisonedProgressRepo) EnqueueWebhookDeliver(_ context.Context, _ uuid.UUID) error {
	atomic.AddInt32(&p.enqueueCalls, 1)
	return nil
}

func (p *poisonedProgressRepo) RecordAttempt(_ context.Context, _ uuid.UUID, _ string, _ int, _ *int, _ bool) (int64, error) {
	atomic.AddInt32(&p.recordCalls, 1)
	return 0, nil
}

// TestProgressWebhookNeverEnqueues pins D-04/T-42-08: a full opted-in
// progress run (via runConvertWithOptionalProgress, the real production
// call path) never calls EnqueueWebhookDeliver/RecordAttempt — the progress
// path is structurally incapable of touching the terminal
// bounded-retry/dead-letter budget.
func TestProgressWebhookNeverEnqueues(t *testing.T) {
	repo := &poisonedProgressRepo{}
	delivr := &spyProgressWebhookDeliverer{}
	conv := &fakeProgressConverter{
		sourceSecs: 100,
		updates:    []convert.ProgressUpdate{{Pct: 10}, {Pct: 50}, {Pct: 90}},
	}
	if _, err := runConvertWithOptionalProgress(context.Background(), conv, "in", "out", nil, repo, uuid.New(), "", 2*time.Second, 1, true, "http://example.invalid", delivr, []byte("s3cr3t"), 50*time.Millisecond); err != nil {
		t.Fatalf("runConvertWithOptionalProgress: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&repo.enqueueCalls) != 0 {
		t.Fatalf("progress path called EnqueueWebhookDeliver %d times, want 0 (T-42-08 separation)", repo.enqueueCalls)
	}
	if atomic.LoadInt32(&repo.recordCalls) != 0 {
		t.Fatalf("progress path called RecordAttempt %d times, want 0 (T-42-08 separation)", repo.recordCalls)
	}
	if atomic.LoadInt32(&delivr.calls) == 0 {
		t.Fatal("expected the progress-webhook deliverer to have fired at least once")
	}
}

// --- Terminal webhook body event discriminator (D-03) ---

// TestBuildTerminalWebhookBody_EventDiscriminator pins D-03: the terminal
// webhook body carries an additive "event":"completed" field alongside the
// existing job_id/status, and the marshaled bytes (what SignPayload's HMAC
// actually covers) include it.
func TestBuildTerminalWebhookBody_EventDiscriminator(t *testing.T) {
	job := &jobs.Job{ID: uuid.New(), Status: jobs.StatusDone}
	body := buildTerminalWebhookBody(job)

	if body["event"] != "completed" {
		t.Fatalf("body[\"event\"] = %v, want \"completed\"", body["event"])
	}
	if body["job_id"] != job.ID {
		t.Fatalf("body[\"job_id\"] = %v, want %v", body["job_id"], job.ID)
	}
	if body["status"] != job.Status {
		t.Fatalf("body[\"status\"] = %v, want %v", body["status"], job.Status)
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"event":"completed"`) {
		t.Fatalf("marshaled body = %s, want it to contain \"event\":\"completed\" (the bytes SignPayload's HMAC covers)", raw)
	}
}

// TestIsPandocTerminal pins D-01 (48-CONTEXT.md): a PANDOC_ENGINE_TIMEOUT
// expiry AND a "pandoc:"-prefixed engine/parse error are BOTH terminal --
// mirroring isDocumentTerminal's timeout-is-terminal shape, the OPPOSITE of
// isOCRTerminal's transient-engine-timeout treatment (the divergence is
// asserted directly below via TestIsPandocTerminalDivergesFromOCR). Genuine
// infra transients with no "pandoc:" text stay retryable via the shared
// isTerminal fallthrough.
func TestIsPandocTerminal(t *testing.T) {
	if isPandocTerminal(nil) {
		t.Fatal("expected isPandocTerminal(nil) = false")
	}

	// D-01: a PANDOC_ENGINE_TIMEOUT expiry (exec.go's process-group-kill
	// shape, preserved through pandoc.go's "pandoc: pandoc: %w" wrapping and
	// process()'s "convert: %w" wrapping) IS terminal for the pandoc engine.
	timeoutErr := fmt.Errorf("convert: %w", fmt.Errorf("pandoc: pandoc: %w", fmt.Errorf("pandoc killed: %w", context.DeadlineExceeded)))
	if !isPandocTerminal(timeoutErr) {
		t.Fatal("D-01: expected isPandocTerminal(wrapped context.DeadlineExceeded) = true (mirrors isDocumentTerminal)")
	}

	// A bare pandoc engine/parse error (the "pandoc: pandoc:" prefix
	// pandoc.go's runCommand-failure branch emits) is terminal -- isTerminal
	// alone has no pandoc-specific signature and would return false here.
	parseErr := fmt.Errorf("convert: %w", errors.New("pandoc: pandoc: exit status 1: [ERROR] Could not parse markdown"))
	if !isPandocTerminal(parseErr) {
		t.Fatal("D-01: expected isPandocTerminal(\"pandoc:\"-prefixed parse error) = true")
	}

	// ErrPandocInputTooLarge (T-48-10) is always terminal.
	oversizeErr := fmt.Errorf("convert: %w", fmt.Errorf("pandoc: %w", convert.ErrPandocInputTooLarge))
	if !isPandocTerminal(oversizeErr) {
		t.Fatal("expected isPandocTerminal(ErrPandocInputTooLarge) = true")
	}

	// A generic transient error with NO "pandoc:" text and not a timeout
	// stays TRANSIENT -- a genuine infra blip (e.g. a transient S3 download
	// failure) must still be retried.
	transientErr := errors.New("dial tcp: connection refused")
	if isPandocTerminal(transientErr) {
		t.Fatal("expected isPandocTerminal(generic transient error) = false (genuine infra transient stays retryable, D-01)")
	}

	// Shared isTerminal fallthrough still applies (minio.NoSuchKey /
	// "no converter for").
	sharedCases := []error{
		fmt.Errorf("no converter for %s -> %s", "md", "pdf"),
		fmt.Errorf("download %q: %w", "uploads/x/0-in.md", minio.ErrorResponse{Code: minio.NoSuchKey}),
	}
	for _, err := range sharedCases {
		if !isPandocTerminal(err) {
			t.Fatalf("expected isPandocTerminal(%v) = true (shared isTerminal fallthrough)", err)
		}
	}
}

// TestIsPandocTerminalDivergesFromOCR documents and pins the D-01/D-04
// divergence side by side: the SAME wrapped context.DeadlineExceeded shape
// classifies terminal for pandoc but transient for OCR -- proving pandoc
// mirrors isDocumentTerminal, NOT isOCRTerminal, exactly as 48-CONTEXT.md's
// D-01 requires.
func TestIsPandocTerminalDivergesFromOCR(t *testing.T) {
	timeoutErr := fmt.Errorf("convert: %w", fmt.Errorf("pandoc: pandoc: %w", fmt.Errorf("pandoc killed: %w", context.DeadlineExceeded)))
	if !isPandocTerminal(timeoutErr) {
		t.Fatal("expected isPandocTerminal(timeout) = true")
	}
	ocrTimeoutErr := fmt.Errorf("convert: %w", fmt.Errorf("ocr: tesseract: %w", fmt.Errorf("tesseract killed: %w", context.DeadlineExceeded)))
	if isOCRTerminal(ocrTimeoutErr) {
		t.Fatal("expected isOCRTerminal(timeout) = false -- pandoc and OCR must diverge on timeout classification (D-01/D-04)")
	}
}

// TestPandocFailureCode pins HandlePandocConvert's terminal-path error_code
// mapping (mirrors TestOCRFailureCode's precedent): pandocFailureCode is the
// pure function HandlePandocConvert delegates to, so this test exercises the
// mapping directly without a live Postgres/S3 Handler.
func TestPandocFailureCode(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "input too large",
			err:      fmt.Errorf("pandoc: %w", convert.ErrPandocInputTooLarge),
			wantCode: "input_too_large",
		},
		{
			name:     "unsupported conversion",
			err:      fmt.Errorf("pandoc: unsupported conversion %s -> %s", "epub", "latex"),
			wantCode: "unsupported_target",
		},
		{
			name:     "unsupported wrap opt",
			err:      fmt.Errorf("pandoc: unsupported wrap %q", "bogus"),
			wantCode: "invalid_options",
		},
		{
			name:     "opts parse failure",
			err:      fmt.Errorf("pandoc: parse opts: %w", errors.New("unexpected EOF")),
			wantCode: "invalid_options",
		},
		{
			name:     "generic engine failure",
			err:      errors.New("pandoc: pandoc: exit status 1"),
			wantCode: "engine_error",
		},
	}
	for _, tc := range cases {
		code, message := pandocFailureCode(tc.err)
		if code != tc.wantCode {
			t.Errorf("%s: pandocFailureCode(%v) code = %q, want %q", tc.name, tc.err, code, tc.wantCode)
		}
		if message == "" {
			t.Errorf("%s: pandocFailureCode(%v) message is empty, want a fixed client-facing message", tc.name, tc.err)
		}
	}
}

// fakeBlankPandocConverter implements convert.Converter with a Convert that
// writes an EMPTY output file and returns nil -- mirrors
// fakeBlankOCRConverter's own OCRE-05 precedent, applied to pandoc's SC4
// contract (pandocValidateOutput, internal/convert/pandoc.go, deliberately
// skips its content-sniff check on a zero-size output and returns nil: an
// empty-body markdown source producing an empty plaintext/fragment result
// is a VALID, successful conversion, not a failure). This proves the seam
// HandlePandocConvert composes (runConvertWithOptionalProgress ->
// process()'s nil-error success path) treats a blank result as success
// regardless of output size, so it flows to h.repo.MarkDone, never
// MarkFailed -- process()'s h.repo/h.store are concrete types (not
// interfaces, ARCHITECTURE.md's "Key Abstractions" note), so a full Handler
// cannot be constructed in a pure unit test; this drives the exact seam
// process() delegates to, the smallest testable proof of the SC4 contract
// without a live Postgres/S3 Handler.
type fakeBlankPandocConverter struct{}

func (fakeBlankPandocConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "md", To: "html"}}
}
func (fakeBlankPandocConverter) Engine() string { return convert.EnginePandoc }
func (fakeBlankPandocConverter) Convert(_ context.Context, _, outPath string, _ map[string]any) error {
	return os.WriteFile(outPath, []byte(""), 0o644)
}

// TestPandocEmptyOutputSucceeds_SC4 pins SC4: an empty-but-valid pandoc
// output must complete as done, never failed. Runs the fake blank converter
// through runConvertWithOptionalProgress -- the exact seam HandlePandocConvert's
// process() call uses -- and asserts a nil error (the branch HandlePandocConvert
// takes straight to h.repo.MarkDone, never h.repo.MarkFailed) alongside a
// genuinely empty output file.
func TestPandocEmptyOutputSucceeds_SC4(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.html")
	repo := &spyProgressRepo{}

	_, err := runConvertWithOptionalProgress(context.Background(), fakeBlankPandocConverter{}, "in.md", outPath, nil, repo, uuid.New(), "", 0, 0, false, "", nil, nil, 0)
	if err != nil {
		t.Fatalf("runConvertWithOptionalProgress(blank-output pandoc result) = %v, want nil (SC4: empty-but-valid output is done, not failed)", err)
	}

	data, rerr := os.ReadFile(outPath)
	if rerr != nil {
		t.Fatalf("read output: %v", rerr)
	}
	if len(data) != 0 {
		t.Fatalf("output = %q, want empty (empty-body source producing an empty result)", data)
	}
}

// --- 57-04 Task 3: process() routes by (job.Operation, source, target) ---

// opGateRepo is a minimal convertRepo fake purpose-built for exercising
// process() up to and past h.registry.LookupOp WITHOUT a live database or
// storage: inputs is returned verbatim from Inputs() (inputKey runs BEFORE
// the registry gate, so it must succeed for the gate itself to be reached),
// and markDoneCalls/addOutputCalls count invocations so a test can assert a
// routing failure never reaches either.
type opGateRepo struct {
	inputs []jobs.Input

	markDoneCalls  int
	addOutputCalls int
}

func (r *opGateRepo) Get(_ context.Context, _ uuid.UUID) (*jobs.Job, error) { return nil, nil }
func (r *opGateRepo) MarkActive(_ context.Context, _ uuid.UUID) error       { return nil }
func (r *opGateRepo) MarkFailed(_ context.Context, _ uuid.UUID, _, _ string, _ map[string]any, _ time.Duration) error {
	return nil
}
func (r *opGateRepo) MarkDone(_ context.Context, _ uuid.UUID, _ time.Duration) error {
	r.markDoneCalls++
	return nil
}
func (r *opGateRepo) Inputs(_ context.Context, _ uuid.UUID) ([]jobs.Input, error) {
	return r.inputs, nil
}
func (r *opGateRepo) AddOutputWithRole(_ context.Context, _ uuid.UUID, _ jobs.Output) error {
	return nil
}

func (r *opGateRepo) AddOutputs(_ context.Context, _ uuid.UUID, _ []jobs.Output) error {
	return nil
}

func (r *opGateRepo) AddOutput(_ context.Context, _ uuid.UUID, _ jobs.Output) error {
	r.addOutputCalls++
	return nil
}
func (r *opGateRepo) Outputs(_ context.Context, _ uuid.UUID) ([]jobs.Output, error) { return nil, nil }
func (r *opGateRepo) UpdateProgress(_ context.Context, _ uuid.UUID, _ int, _ *int) error {
	return nil
}
func (r *opGateRepo) SetSourceDuration(_ context.Context, _ uuid.UUID, _ float64) error {
	return nil
}
func (r *opGateRepo) UpdateStage(_ context.Context, _ uuid.UUID, _ string, _, _ int) (bool, error) {
	return true, nil
}

// identityTestConverter is a synthetic Converter registered ONLY under
// convert.OperationConvert (never "sanitize") for a single txt->txt pair --
// it exercises a REAL, freshly-built *convert.Registry (never convert.Default,
// so these tests never mutate global registration state) without needing any
// real engine binary. Convert is a plain byte copy.
type identityTestConverter struct{}

func (identityTestConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "txt", To: "txt"}}
}
func (identityTestConverter) Engine() string { return "test" }
func (identityTestConverter) Convert(_ context.Context, inPath, outPath string, _ map[string]any) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, data, 0o600)
}

// TestProcess_UnregisteredOperationFailsWithoutMarkDone proves process()
// routes by the FULL (operation, source, target) triple, not just
// (source, target): a job carrying Operation "sanitize" for a pair that is
// registered ONLY under "convert" must fail -- naming "sanitize" in the
// error -- and must never reach h.repo.MarkDone. This is the direct proof
// that a garbage/unrouted operation is not silently processed as convert.
func TestProcess_UnregisteredOperationFailsWithoutMarkDone(t *testing.T) {
	registry := convert.NewRegistry()
	registry.Register(identityTestConverter{})

	repo := &opGateRepo{inputs: []jobs.Input{{
		Ordinal:   0,
		ObjectKey: "uploads/57-04-task3/0-in.txt",
		Filename:  "in.txt",
		Format:    "txt",
	}}}
	h := &Handler{repo: repo, registry: registry, engineTimout: 5 * time.Second}

	job := &jobs.Job{
		ID:           uuid.New(),
		Operation:    "sanitize",
		SourceFormat: "txt",
		TargetFormat: "txt",
	}

	err := h.process(context.Background(), job)
	if err == nil {
		t.Fatal("process() = nil, want an error -- \"sanitize\" has no registered txt->txt triple")
	}
	if !strings.Contains(err.Error(), "sanitize") {
		t.Errorf("process() error = %q, want it to name the rejected operation %q", err.Error(), "sanitize")
	}
	if repo.markDoneCalls != 0 {
		t.Errorf("MarkDone called %d times, want 0 (a routing failure must never reach MarkDone)", repo.markDoneCalls)
	}
	if repo.addOutputCalls != 0 {
		t.Errorf("AddOutput called %d times, want 0 (a routing failure must never reach AddOutput)", repo.addOutputCalls)
	}
}

// requireS3IntegrationForOperationTest mirrors internal/storage/storage_test.go's
// TestRoundTrip skip precedent: this package has no existing fake for h.store
// (a concrete *storage.Client, D-10 step 1's convertRepo seam deliberately
// stops at the repo/enqueuer boundary), so a genuine end-to-end success run
// needs a real S3-compatible endpoint.
func requireS3IntegrationForOperationTest(t *testing.T) {
	t.Helper()
	if os.Getenv("S3_ENDPOINT") == "" {
		t.Skip("S3_ENDPOINT not set; skipping live end-to-end operation-routing success test")
	}
}

// TestProcess_KnownOperationConvertSucceeds is the success-side complement
// of TestProcess_UnregisteredOperationFailsWithoutMarkDone: a job carrying
// Operation "convert" for a pair registered under "convert" must run
// process() to completion -- through the SAME h.registry.LookupOp call --
// ending in exactly one AddOutput and one MarkDone. Gated on a real
// S3-compatible endpoint (see requireS3IntegrationForOperationTest) because
// process() downloads/uploads through h.store, a concrete *storage.Client
// with no in-package fake.
func TestProcess_KnownOperationConvertSucceeds(t *testing.T) {
	requireS3IntegrationForOperationTest(t)

	ctx := context.Background()
	store, err := storage.New(ctx)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	jobID := uuid.New()
	inKey := "uploads/57-04-task3-success/0-in.txt"
	if uerr := store.Upload(ctx, inKey, strings.NewReader("hello operation routing"), -1, "text/plain"); uerr != nil {
		t.Fatalf("seed upload: %v", uerr)
	}

	registry := convert.NewRegistry()
	registry.Register(identityTestConverter{})

	repo := &opGateRepo{inputs: []jobs.Input{{
		Ordinal:   0,
		ObjectKey: inKey,
		Filename:  "in.txt",
		Format:    "txt",
	}}}
	h := &Handler{repo: repo, store: store, registry: registry, engineTimout: 30 * time.Second}

	job := &jobs.Job{
		ID:           jobID,
		Operation:    "convert",
		SourceFormat: "txt",
		TargetFormat: "txt",
	}

	if perr := h.process(ctx, job); perr != nil {
		t.Fatalf("process() = %v, want nil (convert is registered for txt->txt)", perr)
	}
	if repo.addOutputCalls != 1 {
		t.Errorf("AddOutput called %d times, want 1", repo.addOutputCalls)
	}
	if repo.markDoneCalls != 1 {
		t.Errorf("MarkDone called %d times, want 1", repo.markDoneCalls)
	}
}
