package worker

// This suite pins the pre-refactor seven-handler network per
// 56-CONTEXT.md D-10 step 1: it freezes TODAY's exact terminal/transient/
// canceled/success behavior for every one of the seven Handle*Convert
// wrappers (image/document/html/audio/av/ocr/pandoc), driven through the
// real wrapper methods on a Handler built from fakes (the convertRepo/
// webhookEnqueuer/processFn seams introduced in Task 1), BEFORE plan
// 56-04's Runner/EngineDescriptor consolidation. Plan 56-04's refactor MUST
// make this suite pass UNCHANGED -- any modification to an assertion in
// this file during that refactor is a protocol violation requiring
// explicit justification (PITFALLS.md Pitfall 14: "the test suite proves
// the runner's shape, not the seven engines' pre-existing behavior").

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/queue"
)

// --- Harness -----------------------------------------------------------

// markFailedCall records one h.repo.MarkFailed invocation's full argument
// set, so a pinning test can assert the exact (code, message, attempt)
// triple a terminal outcome produced, not merely that MarkFailed was
// called.
type markFailedCall struct {
	code    string
	message string
	detail  map[string]any
	attempt time.Duration
}

// pinRepo implements convertRepo, recording every call into a SHARED
// ordered log (calls) using stable tokens -- "Get", "MarkActive",
// "MarkFailed", "MarkDone", "EnqueueWebhookDeliver" (the last one is
// appended by pinEnqueuer into the SAME slice, so ordering between a
// commit and the webhook enqueue is directly observable, Task 3's
// TestPinnedEmissionNeverPrecedesCommit). getErr/markActiveErr/
// markFailedErr inject outcomes for the guarded-transition error-path
// tests (Task 3's TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook).
// Inputs/AddOutput/Outputs/UpdateProgress/SetSourceDuration exist only to
// satisfy convertRepo -- process() itself is never reached because every
// pinning test stubs h.processFn directly.
type pinRepo struct {
	job *jobs.Job

	getErr        error
	markActiveErr error
	markFailedErr error

	calls      *[]string
	markFailed []markFailedCall
}

func (r *pinRepo) Get(_ context.Context, _ uuid.UUID) (*jobs.Job, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	*r.calls = append(*r.calls, "Get")
	return r.job, nil
}

func (r *pinRepo) MarkActive(_ context.Context, _ uuid.UUID) error {
	if r.markActiveErr != nil {
		return r.markActiveErr
	}
	*r.calls = append(*r.calls, "MarkActive")
	return nil
}

func (r *pinRepo) MarkFailed(_ context.Context, _ uuid.UUID, code, message string, detail map[string]any, attempt time.Duration) error {
	*r.calls = append(*r.calls, "MarkFailed")
	r.markFailed = append(r.markFailed, markFailedCall{code: code, message: message, detail: detail, attempt: attempt})
	return r.markFailedErr
}

func (r *pinRepo) MarkDone(_ context.Context, _ uuid.UUID, _ time.Duration) error {
	*r.calls = append(*r.calls, "MarkDone")
	return nil
}

func (r *pinRepo) Inputs(_ context.Context, _ uuid.UUID) ([]jobs.Input, error) { return nil, nil }
func (r *pinRepo) AddOutput(_ context.Context, _ uuid.UUID, _ jobs.Output) error {
	return nil
}
func (r *pinRepo) Outputs(_ context.Context, _ uuid.UUID) ([]jobs.Output, error) { return nil, nil }
func (r *pinRepo) UpdateProgress(_ context.Context, _ uuid.UUID, _ int, _ *int) error {
	return nil
}
func (r *pinRepo) SetSourceDuration(_ context.Context, _ uuid.UUID, _ float64) error {
	return nil
}

// pinEnqueuer implements webhookEnqueuer, appending into the SAME shared
// ordered log pinRepo writes to (see pinRepo's doc comment).
type pinEnqueuer struct {
	calls *[]string
	err   error
}

func (e *pinEnqueuer) EnqueueWebhookDeliver(_ context.Context, _ uuid.UUID) error {
	*e.calls = append(*e.calls, "EnqueueWebhookDeliver")
	return e.err
}

// newPinHandler builds a Handler directly from fakes (same package,
// unexported fields reachable) -- store/registry/webhookRepo/deliverer
// stay nil because no pinning test reaches process() itself (h.processFn
// is always stubbed per test case).
func newPinHandler(_ *testing.T, r *pinRepo, e *pinEnqueuer) *Handler {
	return &Handler{repo: r, enqueuer: e, engineTimout: time.Second}
}

// countToken counts how many times token appears in the shared ordered
// call log.
func countToken(calls []string, token string) int {
	n := 0
	for _, c := range calls {
		if c == token {
			n++
		}
	}
	return n
}

// readCounter gathers family from prometheus.DefaultGatherer (the SAME
// registerer promauto.NewCounterVec/NewHistogramVec register into inside
// internal/metrics, so worker-emitted metrics are visible from this
// package without exporting anything from internal/metrics) and returns
// the counter value whose label set matches labels EXACTLY (0 when
// absent). Every caller MUST treat this as a before/after DELTA read: the
// default registerer is process-wide and other tests in this package also
// emit into the same series.
func readCounter(t *testing.T, family string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != family {
			continue
		}
		for _, m := range mf.GetMetric() {
			got := make(map[string]string, len(labels))
			for _, l := range m.GetLabel() {
				got[l.GetName()] = l.GetValue()
			}
			if len(got) != len(labels) {
				continue
			}
			match := true
			for k, v := range labels {
				if got[k] != v {
					match = false
					break
				}
			}
			if match && m.GetCounter() != nil {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// assertLabelNames gathers family from prometheus.DefaultGatherer and
// asserts the sorted label-name set of EVERY series in that family equals
// want -- mirrors internal/metrics/metrics_test.go's
// TestJobDurationLabelsPinned idiom, applied here to prove the assertion
// holds from the worker's own emission call sites, not only from
// internal/metrics's unit tests.
func assertLabelNames(t *testing.T, family string, want []string) {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	found := false
	for _, mf := range mfs {
		if mf.GetName() != family {
			continue
		}
		for _, m := range mf.GetMetric() {
			found = true
			var names []string
			for _, l := range m.GetLabel() {
				names = append(names, l.GetName())
			}
			sort.Strings(names)
			if len(names) != len(wantSorted) {
				t.Fatalf("%s labels: want %v, got %v", family, wantSorted, names)
			}
			for i := range wantSorted {
				if names[i] != wantSorted[i] {
					t.Fatalf("%s labels: want %v, got %v", family, wantSorted, names)
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s family not found in default gatherer", family)
	}
}

// newTask builds a real asynq task carrying a queue.ConvertPayload, so
// queue.ParseConvertPayload runs for real inside every Handle*Convert
// wrapper under test -- the payload-parse step is exercised genuinely, not
// bypassed.
func newTask(t *testing.T, taskType string, jobID uuid.UUID) *asynq.Task {
	t.Helper()
	b, err := json.Marshal(queue.ConvertPayload{JobID: jobID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return asynq.NewTask(taskType, b)
}

// pinEngine holds one row of the seven-engine network under pin: display
// name, asynq task type, the expected engine label, the wrapper method
// (dispatched via a Go METHOD EXPRESSION -- (*Handler).HandleXConvert --
// never a bound method value obtained from a live Handler variable's own
// Handle-prefixed field access, so the seven wrappers are proven callable
// purely from data), and the expected default terminal (code, message)
// pair plus the terminal error each engine's own classifier already
// treats as terminal (reused verbatim from worker_test.go's existing
// tables -- never an invented error string).
type pinEngine struct {
	name        string
	taskType    string
	engine      string
	wrapper     func(*Handler, context.Context, *asynq.Task) error
	wantCode    string
	wantMessage string
	terminalErr error
}

// pinEngines is the seven-row table every per-engine pinning test in this
// suite iterates. terminalErr values are taken directly from
// worker_test.go's existing terminal-classification tables (TestIsTerminal
// VipsSignatures / TestIsTerminalLibreOfficeSignatures / TestIsTerminal
// ChromiumSignatures / TestIsAudioTerminal / TestAVFailureCode / TestIsOCR
// Terminal / TestIsPandocTerminal) -- never an invented error string, per
// the plan's "never invent an error string not already present in a
// passing test" instruction. Every one of these seven default cases maps
// to the shared "engine_error" / "unsupported or corrupted input format"
// fallback; the non-default failure-code branches (av/ocr/pandoc/audio)
// are pinned separately by TestPinnedFailureCodePerEngine.
var pinEngines = []pinEngine{
	{
		name:        "image",
		taskType:    queue.TypeImageConvert,
		engine:      convert.EngineImage,
		wrapper:     (*Handler).HandleImageConvert,
		wantCode:    "engine_error",
		wantMessage: "unsupported or corrupted input format",
		terminalErr: errors.New("convert: exit status 1: vips__file_read_signature: is not a known file format"),
	},
	{
		name:        "document",
		taskType:    queue.TypeDocumentConvert,
		engine:      convert.EngineDocument,
		wrapper:     (*Handler).HandleDocumentConvert,
		wantCode:    "engine_error",
		wantMessage: "unsupported or corrupted input format",
		terminalErr: errors.New("convert: libreoffice: output missing %PDF- magic bytes"),
	},
	{
		name:        "html",
		taskType:    queue.TypeHTMLConvert,
		engine:      convert.EngineHTML,
		wrapper:     (*Handler).HandleHTMLConvert,
		wantCode:    "engine_error",
		wantMessage: "unsupported or corrupted input format",
		terminalErr: errors.New("convert: chromium: output is empty"),
	},
	{
		name:        "audio",
		taskType:    queue.TypeAudioConvert,
		engine:      convert.EngineAudio,
		wrapper:     (*Handler).HandleAudioConvert,
		wantCode:    "engine_error",
		wantMessage: "unsupported or corrupted input format",
		terminalErr: fmt.Errorf("convert: %w", fmt.Errorf("audio: ffmpeg: %w", errors.New("exit status 1: Invalid data found when processing input"))),
	},
	{
		name:        "av",
		taskType:    queue.TypeAVConvert,
		engine:      convert.EngineAV,
		wrapper:     (*Handler).HandleAVConvert,
		wantCode:    "engine_error",
		wantMessage: "unsupported or corrupted input format",
		terminalErr: fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVTranscodeFailed, errors.New("exit status 1: Invalid data found when processing input"))),
	},
	{
		name:        "ocr",
		taskType:    queue.TypeOCRConvert,
		engine:      convert.EngineOCR,
		wrapper:     (*Handler).HandleOCRConvert,
		wantCode:    "engine_error",
		wantMessage: "unsupported or corrupted input format",
		terminalErr: fmt.Errorf("convert: %w", fmt.Errorf("ocr: tesseract: %w", errors.New("exit status 1: Error in pixReadStream: Unknown format: extension not recognized"))),
	},
	{
		name:        "pandoc",
		taskType:    queue.TypePandocConvert,
		engine:      convert.EnginePandoc,
		wrapper:     (*Handler).HandlePandocConvert,
		wantCode:    "engine_error",
		wantMessage: "unsupported or corrupted input format",
		terminalErr: fmt.Errorf("convert: %w", errors.New("pandoc: pandoc: exit status 1: [ERROR] Could not parse markdown")),
	},
}

// pinEngineByName looks up one pinEngines row by its display name -- used
// by tests that key off an engine name string (TestPinnedFailureCodePerEngine).
func pinEngineByName(t *testing.T, name string) pinEngine {
	t.Helper()
	for _, e := range pinEngines {
		if e.name == name {
			return e
		}
	}
	t.Fatalf("no pinEngine named %q", name)
	return pinEngine{}
}

// newPinJob builds a minimal *jobs.Job for a pinning test: a fresh client
// id (so counter-delta assertions can never collide across sub-tests, per
// the plan's "fresh uuid.New() client id per sub-test" instruction) and
// the given callback URL.
func newPinJob(callbackURL string) (*jobs.Job, uuid.UUID) {
	clientID := uuid.New()
	jobID := uuid.New()
	return &jobs.Job{ID: jobID, ClientID: clientID, CallbackURL: callbackURL}, jobID
}

// --- TestPinnedTerminalOutcomePerEngine ---------------------------------

// TestPinnedTerminalOutcomePerEngine pins, for each of the seven engines,
// that a terminal processFn error produces exactly one MarkFailed call
// carrying that engine's expected default (code, message) pair, a
// SkipRetry-wrapped return, both job-outcome counters moving by exactly 1
// with status=failed, and exactly one webhook enqueue when CallbackURL is
// set.
func TestPinnedTerminalOutcomePerEngine(t *testing.T) {
	for _, tc := range pinEngines {
		t.Run(tc.name, func(t *testing.T) {
			job, jobID := newPinJob("https://example.test/hook")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return tc.terminalErr }

			labels := map[string]string{"engine": tc.engine, "status": jobs.StatusFailed}
			clientLabels := map[string]string{"client_id": job.ClientID.String(), "engine": tc.engine, "status": jobs.StatusFailed}
			beforeJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			beforeClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

			err := tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("%s: err = %v, want errors.Is(err, asynq.SkipRetry)", tc.name, err)
			}
			if len(repo.markFailed) != 1 {
				t.Fatalf("%s: MarkFailed calls = %d, want 1", tc.name, len(repo.markFailed))
			}
			if got := repo.markFailed[0]; got.code != tc.wantCode || got.message != tc.wantMessage {
				t.Fatalf("%s: MarkFailed(code=%q, message=%q), want (code=%q, message=%q)", tc.name, got.code, got.message, tc.wantCode, tc.wantMessage)
			}

			afterJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			afterClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)
			if afterJob != beforeJob+1 {
				t.Fatalf("%s: octoconv_job_outcomes_total delta = %v, want 1", tc.name, afterJob-beforeJob)
			}
			if afterClient != beforeClient+1 {
				t.Fatalf("%s: octoconv_client_job_outcomes_total delta = %v, want 1", tc.name, afterClient-beforeClient)
			}
			if n := countToken(calls, "EnqueueWebhookDeliver"); n != 1 {
				t.Fatalf("%s: EnqueueWebhookDeliver calls = %d, want 1 (CallbackURL set)", tc.name, n)
			}
		})
	}
}

// --- TestPinnedFailureCodePerEngine -------------------------------------

// pinFailureCodeCase pins one non-default terminal error_code branch of
// avFailureCode / ocrFailureCode / pandocFailureCode, or audio's inline
// errors.Is(convert.ErrAudioDurationExceeded) special-case -- exercised
// through the real handler wrapper, not by calling the pure mapping
// function directly. Every err value here is copied verbatim from
// TestAVFailureCode / TestOCRFailureCode / TestPandocFailureCode /
// TestIsAudioTerminal (worker_test.go).
type pinFailureCodeCase struct {
	engine   string
	name     string
	err      error
	wantCode string
}

var pinFailureCodeCases = []pinFailureCodeCase{
	// av: TestAVFailureCode.
	{"av", "timecode out of range", fmt.Errorf("convert: %w", convert.ErrAVTimecodeOutOfRange), "timecode_out_of_range"},
	{"av", "duration exceeded", fmt.Errorf("convert: %w", convert.ErrAudioDurationExceeded), "duration_exceeded"},
	{"av", "resolution exceeded", fmt.Errorf("convert: %w", convert.ErrAVResolutionExceeded), "resolution_exceeded"},
	{"av", "no video stream", fmt.Errorf("convert: %w", convert.ErrAVNoVideoStream), "no_video_stream"},
	{"av", "generic engine failure", fmt.Errorf("convert: %w", convert.ErrAVTranscodeFailed), "engine_error"},

	// ocr: TestOCRFailureCode.
	{"ocr", "born digital", fmt.Errorf("ocr: %w", convert.ErrOCRBornDigital), "born_digital"},
	{"ocr", "too many pages", fmt.Errorf("%w: declared page count 50 exceeds bound 20", convert.ErrOCRTooManyPages), "too_many_pages"},
	{"ocr", "page box too large", convert.ErrOCRPageBoxTooLarge, "page_too_large"},
	{"ocr", "image too large", fmt.Errorf("ocr: %w", convert.ErrOCRImageTooLarge), "image_too_large"},
	{"ocr", "unsupported language", fmt.Errorf("ocr: %w", fmt.Errorf("unsupported language %q", "fra")), "unsupported_language"},
	{"ocr", "unsupported target", fmt.Errorf("ocr: unsupported target format %q", "bmp"), "unsupported_target"},
	{"ocr", "generic engine failure", fmt.Errorf("ocr: tesseract: %w", errors.New("exit status 1")), "engine_error"},

	// pandoc: TestPandocFailureCode.
	{"pandoc", "input too large", fmt.Errorf("pandoc: %w", convert.ErrPandocInputTooLarge), "input_too_large"},
	{"pandoc", "unsupported conversion", fmt.Errorf("pandoc: unsupported conversion %s -> %s", "epub", "latex"), "unsupported_target"},
	{"pandoc", "unsupported wrap opt", fmt.Errorf("pandoc: unsupported wrap %q", "bogus"), "invalid_options"},
	{"pandoc", "opts parse failure", fmt.Errorf("pandoc: parse opts: %w", errors.New("unexpected EOF")), "invalid_options"},
	{"pandoc", "generic engine failure", errors.New("pandoc: pandoc: exit status 1"), "engine_error"},

	// audio: TestIsAudioTerminal's duration-guard and ffmpeg-stage rows.
	{"audio", "duration exceeded", fmt.Errorf("convert: %w", convert.ErrAudioDurationExceeded), "duration_exceeded"},
	{"audio", "generic engine failure (ffmpeg stage)", fmt.Errorf("convert: %w", fmt.Errorf("audio: ffmpeg: %w", errors.New("exit status 1: Invalid data found when processing input"))), "engine_error"},
}

// TestPinnedFailureCodePerEngine drives each pinFailureCodeCases row
// through the real handler wrapper (via pinEngineByName) and asserts the
// resulting MarkFailed code -- not by calling avFailureCode/ocrFailureCode/
// pandocFailureCode directly, so a future refactor that stops threading
// the mapped code into MarkFailed's argument fails loudly even if the pure
// mapping function itself still returns the right value.
func TestPinnedFailureCodePerEngine(t *testing.T) {
	for _, tc := range pinFailureCodeCases {
		t.Run(tc.engine+"/"+tc.name, func(t *testing.T) {
			pe := pinEngineByName(t, tc.engine)
			job, jobID := newPinJob("")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return tc.err }

			_ = pe.wrapper(h, context.Background(), newTask(t, pe.taskType, jobID))

			if len(repo.markFailed) != 1 {
				t.Fatalf("%s/%s: MarkFailed calls = %d, want 1", tc.engine, tc.name, len(repo.markFailed))
			}
			if got := repo.markFailed[0].code; got != tc.wantCode {
				t.Fatalf("%s/%s: MarkFailed code = %q, want %q", tc.engine, tc.name, got, tc.wantCode)
			}
		})
	}
}

// --- TestPinnedTransientOutcomePerEngine --------------------------------

// TestPinnedTransientOutcomePerEngine pins, for each of the seven engines,
// that a genuine transient error (a plain network blip matching none of
// any engine's terminal signatures) produces zero MarkFailed calls, zero
// counter movement on either family, zero webhook enqueues, and a returned
// error for which errors.Is(err, asynq.SkipRetry) is false -- so asynq's
// own retry/backoff applies.
func TestPinnedTransientOutcomePerEngine(t *testing.T) {
	transientErr := errors.New("dial tcp: connection refused")
	for _, tc := range pinEngines {
		t.Run(tc.name, func(t *testing.T) {
			job, jobID := newPinJob("https://example.test/hook")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return transientErr }

			labels := map[string]string{"engine": tc.engine, "status": jobs.StatusFailed}
			clientLabels := map[string]string{"client_id": job.ClientID.String(), "engine": tc.engine, "status": jobs.StatusFailed}
			beforeJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			beforeClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

			err := tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

			if errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("%s: err = %v, want NOT errors.Is(err, asynq.SkipRetry) (transient)", tc.name, err)
			}
			if len(repo.markFailed) != 0 {
				t.Fatalf("%s: MarkFailed calls = %d, want 0 (transient)", tc.name, len(repo.markFailed))
			}
			afterJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			afterClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)
			if afterJob != beforeJob {
				t.Fatalf("%s: octoconv_job_outcomes_total moved on a transient error", tc.name)
			}
			if afterClient != beforeClient {
				t.Fatalf("%s: octoconv_client_job_outcomes_total moved on a transient error", tc.name)
			}
			if n := countToken(calls, "EnqueueWebhookDeliver"); n != 0 {
				t.Fatalf("%s: EnqueueWebhookDeliver calls = %d, want 0 (transient)", tc.name, n)
			}
		})
	}
}

// --- TestPinnedCanceledOutcomePerEngine ----------------------------------

// TestPinnedCanceledOutcomePerEngine pins, for each of the seven engines,
// D-07: a context.Canceled processFn error (the shape a job:cancel signal
// produces) yields zero MarkFailed calls, zero counter movement, zero
// webhook enqueues, and a SkipRetry-wrapped return -- checked BEFORE any
// engine-scoped terminal classifier runs.
func TestPinnedCanceledOutcomePerEngine(t *testing.T) {
	canceledErr := fmt.Errorf("%w: killed", context.Canceled)
	for _, tc := range pinEngines {
		t.Run(tc.name, func(t *testing.T) {
			job, jobID := newPinJob("https://example.test/hook")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return canceledErr }

			labels := map[string]string{"engine": tc.engine, "status": jobs.StatusFailed}
			clientLabels := map[string]string{"client_id": job.ClientID.String(), "engine": tc.engine, "status": jobs.StatusFailed}
			beforeJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			beforeClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

			err := tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("%s: err = %v, want errors.Is(err, asynq.SkipRetry) (canceled, D-07)", tc.name, err)
			}
			if len(repo.markFailed) != 0 {
				t.Fatalf("%s: MarkFailed calls = %d, want 0 (canceled, D-07)", tc.name, len(repo.markFailed))
			}
			afterJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			afterClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)
			if afterJob != beforeJob {
				t.Fatalf("%s: octoconv_job_outcomes_total moved on a canceled attempt", tc.name)
			}
			if afterClient != beforeClient {
				t.Fatalf("%s: octoconv_client_job_outcomes_total moved on a canceled attempt", tc.name)
			}
			if n := countToken(calls, "EnqueueWebhookDeliver"); n != 0 {
				t.Fatalf("%s: EnqueueWebhookDeliver calls = %d, want 0 (canceled)", tc.name, n)
			}
		})
	}
}

// --- TestPinnedSuccessOutcomePerEngine -----------------------------------

// TestPinnedSuccessOutcomePerEngine pins, for each of the seven engines,
// that a nil processFn error increments both job-outcome counters by
// exactly 1 with status=done and enqueues exactly one webhook when
// CallbackURL is set.
func TestPinnedSuccessOutcomePerEngine(t *testing.T) {
	for _, tc := range pinEngines {
		t.Run(tc.name, func(t *testing.T) {
			job, jobID := newPinJob("https://example.test/hook")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return nil }

			labels := map[string]string{"engine": tc.engine, "status": jobs.StatusDone}
			clientLabels := map[string]string{"client_id": job.ClientID.String(), "engine": tc.engine, "status": jobs.StatusDone}
			beforeJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			beforeClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

			err := tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

			if err != nil {
				t.Fatalf("%s: err = %v, want nil (success)", tc.name, err)
			}
			if len(repo.markFailed) != 0 {
				t.Fatalf("%s: MarkFailed calls = %d, want 0 (success)", tc.name, len(repo.markFailed))
			}
			afterJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			afterClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)
			if afterJob != beforeJob+1 {
				t.Fatalf("%s: octoconv_job_outcomes_total delta = %v, want 1", tc.name, afterJob-beforeJob)
			}
			if afterClient != beforeClient+1 {
				t.Fatalf("%s: octoconv_client_job_outcomes_total delta = %v, want 1", tc.name, afterClient-beforeClient)
			}
			if n := countToken(calls, "EnqueueWebhookDeliver"); n != 1 {
				t.Fatalf("%s: EnqueueWebhookDeliver calls = %d, want 1 (CallbackURL set)", tc.name, n)
			}
		})
	}
}

// --- TestPinnedUnparseablePayload -----------------------------------------

// TestPinnedUnparseablePayload pins, for each of the seven engines, that a
// task whose payload is not a valid queue.ConvertPayload returns a
// SkipRetry-wrapped error with an EMPTY call log -- processFn, and every
// repo/enqueuer method, are never reached.
func TestPinnedUnparseablePayload(t *testing.T) {
	for _, tc := range pinEngines {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			repo := &pinRepo{calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error {
				t.Fatal("processFn must not be called for an unparseable payload")
				return nil
			}

			task := asynq.NewTask(tc.taskType, []byte("not-json"))
			err := tc.wrapper(h, context.Background(), task)

			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("%s: err = %v, want errors.Is(err, asynq.SkipRetry)", tc.name, err)
			}
			if len(calls) != 0 {
				t.Fatalf("%s: calls = %v, want empty (unparseable payload, zero repo calls)", tc.name, calls)
			}
		})
	}
}

// --- TestPinnedEmissionLabelSets ------------------------------------------

// TestPinnedEmissionLabelSets drives one terminal and one success case
// (image is representative -- every engine emits through the identical
// metrics.RecordJobOutcome/metrics.RecordClientJobOutcome call sites) and
// asserts octoconv_job_outcomes_total / octoconv_job_duration_seconds
// carry exactly {engine,status} and octoconv_client_job_outcomes_total
// exactly {client_id,engine,status} -- the worker-side half of the
// Pitfall 4 metric-label-drift guard, asserted from the worker's own
// emission, not only from internal/metrics's unit tests.
func TestPinnedEmissionLabelSets(t *testing.T) {
	tc := pinEngineByName(t, "image")

	job, jobID := newPinJob("https://example.test/hook")
	calls := []string{}
	repo := &pinRepo{job: job, calls: &calls}
	enq := &pinEnqueuer{calls: &calls}
	h := newPinHandler(t, repo, enq)

	h.processFn = func(context.Context, *jobs.Job) error { return tc.terminalErr }
	_ = tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

	h.processFn = func(context.Context, *jobs.Job) error { return nil }
	_ = tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

	assertLabelNames(t, "octoconv_job_outcomes_total", []string{"engine", "status"})
	assertLabelNames(t, "octoconv_job_duration_seconds", []string{"engine", "status"})
	assertLabelNames(t, "octoconv_client_job_outcomes_total", []string{"client_id", "engine", "status"})
}
