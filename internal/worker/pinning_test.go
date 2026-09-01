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
//
// --- Phase 59: one row removed, with justification ------------------------
//
// Removed from pinFailureCodeCases:
//
//	{"ocr", "born digital", fmt.Errorf("ocr: %w", convert.ErrOCRBornDigital), "born_digital"},
//
// The protocol above is scoped to plan 56-04's refactor, so this edit is not
// literally a violation of it. That scoping is exactly why the justification is
// written here rather than treated as permission: "out of scope by design" is
// itself a claim, and a suite from which rows can be removed on the strength of
// an unstated claim has stopped being a defence.
//
// Why the row could not stay. It pinned a mapping from a sentinel to an error
// code, and Phase 59 (D-05/D-06) removed the sentinel's producer:
// convert.ocrPDFGuardStage no longer refuses a PDF for having a text layer --
// a txt target receives the extracted layer, and hocr/alto go on to a real
// render because pdftotext produces no word or line boxes. Keeping the row
// would have meant keeping an unreachable sentinel alive solely so a test could
// hand it to a mapping function that no production path can ever call: a pin on
// behavior that no longer exists, which is a different thing from a pin on
// behavior that must not change.
//
// What makes this a fact and not an assertion:
// convert.TestBornDigitalHasNoRefusalPath (internal/convert/ocrpdf_test.go)
// runs a born-digital PDF through ALL THREE targets -- txt, hocr, alto -- and
// requires that none of them refuses the file for having a text layer. All
// three, not only the two whose behavior changed: a proof of unreachability
// that skips a route proves nothing about that route.
//
// Stronger still, the sentinel itself is gone: this phase deleted
// convert.ErrOCRBornDigital, so its unreachability is now enforced by the
// compiler rather than by a passing assertion. The test remains as the
// behavioral half, catching a refusal reintroduced under some new name.
//
// What did NOT happen: the code was not deleted from the contract.
// worker.CodeBornDigital survives as a historical code (descriptors.go) and
// keeps its row in docs/ERROR-CODES.md, because jobs.error_code is persisted
// and GET /v1/jobs/{id} still serves it for jobs that failed before this phase.
//
// No other row in this file was touched.

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
func (r *pinRepo) AddOutputWithRole(_ context.Context, _ uuid.UUID, _ jobs.Output) error {
	return nil
}

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

// pinSuccessProcessFn builds a processFn stub that mirrors process()'s own
// success contract: process() itself calls h.repo.MarkDone before
// returning nil (worker.go's process(), the final line), so every
// Handle*Convert success branch is documented as running AFTER MarkDone
// already committed ("Postgres-first: MarkDone already committed inside
// process()"). A processFn stub that simply returns nil would silently
// drop that MarkDone call from the shared ordered log, making an ordering
// assertion (TestPinnedEmissionNeverPrecedesCommit) vacuously pass for the
// wrong reason. This stub calls repo.MarkDone itself, exactly as
// process() does, before returning nil.
func pinSuccessProcessFn(repo *pinRepo) func(context.Context, *jobs.Job) error {
	return func(ctx context.Context, job *jobs.Job) error {
		return repo.MarkDone(ctx, job.ID, 0)
	}
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
			h.processFn = pinSuccessProcessFn(repo)

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

	h.processFn = pinSuccessProcessFn(repo)
	_ = tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

	assertLabelNames(t, "octoconv_job_outcomes_total", []string{"engine", "status"})
	assertLabelNames(t, "octoconv_job_duration_seconds", []string{"engine", "status"})
	assertLabelNames(t, "octoconv_client_job_outcomes_total", []string{"client_id", "engine", "status"})
}

// --- TestPinnedEmissionNeverPrecedesCommit --------------------------------

// assertWebhookNeverPrecedesCommit walks the shared ordered call log and
// fails if any "EnqueueWebhookDeliver" token appears before a preceding
// "MarkFailed"/"MarkDone" commit token has been observed. Because calls is
// already in real execution order, a simple forward scan with a
// once-set "committed" flag IS the positional assertion the plan requires
// -- it can detect a future diff that moves the webhook enqueue before the
// guarded transition commits (the exact defect class plan 56-05's
// inversion run must be able to break), unlike a count-only assertion.
func assertWebhookNeverPrecedesCommit(t *testing.T, calls []string) {
	t.Helper()
	committed := false
	for _, c := range calls {
		switch c {
		case "MarkFailed", "MarkDone":
			committed = true
		case "EnqueueWebhookDeliver":
			if !committed {
				t.Fatalf("EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in %v", calls)
			}
		}
	}
}

// TestPinnedEmissionNeverPrecedesCommit pins, for each of the seven
// engines, that a terminal failure and a success both preserve
// "commit strictly before webhook enqueue" ordering on the real call log.
func TestPinnedEmissionNeverPrecedesCommit(t *testing.T) {
	for _, tc := range pinEngines {
		t.Run(tc.name+"/terminal", func(t *testing.T) {
			job, jobID := newPinJob("https://example.test/hook")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return tc.terminalErr }

			_ = tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))
			assertWebhookNeverPrecedesCommit(t, calls)
		})
		t.Run(tc.name+"/success", func(t *testing.T) {
			job, jobID := newPinJob("https://example.test/hook")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = pinSuccessProcessFn(repo)

			_ = tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))
			assertWebhookNeverPrecedesCommit(t, calls)
		})
	}
}

// --- TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook ----------

// TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook pins, for each
// of the seven engines, that a MarkFailed call which itself returns an
// error (a Postgres blip, or a concurrent transition winning the guarded
// allow-list) suppresses BOTH the per-client counter and the webhook
// enqueue -- while octoconv_job_outcomes_total (the duration-histogram
// counterpart) is DELIBERATELY NOT guarded the same way and still moves.
// This asymmetry is a pre-existing, deliberate choice documented at every
// terminal branch in worker.go's own comments ("Guarded on ferr: ...
// counting here too would double-count this client, or attribute an
// outcome that never happened" for the client counter and webhook; no
// equivalent guard exists on the RecordJobOutcome call immediately above
// it) -- it is NOT the RUNR-02 defect this phase's reconciler fix
// addresses (that defect is an entirely unguarded reconciler-side
// increment, internal/reconciler/reconciler.go:311-312, fixed separately
// in this phase). Pinned here as-is; plan 56-04's consolidation must
// preserve this exact asymmetry verbatim rather than "fix" it into a
// symmetric guard.
func TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook(t *testing.T) {
	for _, tc := range pinEngines {
		t.Run(tc.name, func(t *testing.T) {
			job, jobID := newPinJob("https://example.test/hook")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls, markFailedErr: errors.New("postgres blip")}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return tc.terminalErr }

			labels := map[string]string{"engine": tc.engine, "status": jobs.StatusFailed}
			clientLabels := map[string]string{"client_id": job.ClientID.String(), "engine": tc.engine, "status": jobs.StatusFailed}
			beforeJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			beforeClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

			_ = tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

			afterJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			afterClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

			if afterJob != beforeJob+1 {
				t.Fatalf("%s: octoconv_job_outcomes_total delta = %v, want 1 (unguarded on MarkFailed's own error -- a deliberate pre-existing asymmetry, not a bug)", tc.name, afterJob-beforeJob)
			}
			if afterClient != beforeClient {
				t.Fatalf("%s: octoconv_client_job_outcomes_total delta = %v, want 0 (guarded on ferr==nil)", tc.name, afterClient-beforeClient)
			}
			if n := countToken(calls, "EnqueueWebhookDeliver"); n != 0 {
				t.Fatalf("%s: EnqueueWebhookDeliver calls = %d, want 0 (guarded on ferr==nil)", tc.name, n)
			}
		})
	}
}

// --- TestPinnedOptsGuardPrecedesMarkActive / TestPinnedImageHasNoOptsGuard -

// pinInvalidOptsCases holds one engine-specific map[string]any that
// provably fails that engine's own convert.*OptsFromMap strict re-parse --
// derived by reading each of the six OptsFromMap functions (opts.go,
// htmlopts.go, audioopts.go, avopts.go, ocropts.go, pandocopts.go), never
// guessed: an out-of-allowlist enum value for each engine's own closed
// field.
var pinInvalidOptsCases = []struct {
	engine string
	opts   map[string]any
}{
	{"document", map[string]any{"pdf_profile": "bogus"}},
	{"html", map[string]any{"page_size": "bogus"}},
	{"audio", map[string]any{"language": "xx"}},
	{"av", map[string]any{"codec": "bogus"}},
	{"ocr", map[string]any{"language": "fra"}},
	{"pandoc", map[string]any{"wrap": "bogus"}},
}

// TestPinnedOptsGuardPrecedesMarkActive pins, for each of the six engines
// with a strict opts re-parse guard, that a job whose stored Opts fail that
// parse produces exactly one MarkFailed(code="invalid_options",
// message="stored conversion options are invalid", attempt=0), with
// "MarkActive" entirely absent from the call log, a SkipRetry-wrapped
// return, and the webhook enqueued exactly once when CallbackURL is set
// and zero times when it is empty.
//
// CRITICAL (D-10/ERRC-02): this test deliberately makes NO assertion about
// octoconv_job_outcomes_total or octoconv_client_job_outcomes_total. Today
// neither counter fires on this branch -- that gap is exactly what ERRC-02
// (this milestone's structured-error-code work) closes, in plan 56-04.
// Pinning today's absent-counter behavior here would make this frozen
// suite CONTRADICT a phase requirement the moment 56-04 lands; a future
// reader must not "complete" these assertions.
func TestPinnedOptsGuardPrecedesMarkActive(t *testing.T) {
	for _, oc := range pinInvalidOptsCases {
		pe := pinEngineByName(t, oc.engine)
		for _, callbackURL := range []string{"https://example.test/hook", ""} {
			name := oc.engine + "/with_callback"
			if callbackURL == "" {
				name = oc.engine + "/no_callback"
			}
			t.Run(name, func(t *testing.T) {
				clientID := uuid.New()
				jobID := uuid.New()
				job := &jobs.Job{ID: jobID, ClientID: clientID, CallbackURL: callbackURL, Opts: oc.opts}
				calls := []string{}
				repo := &pinRepo{job: job, calls: &calls}
				enq := &pinEnqueuer{calls: &calls}
				h := newPinHandler(t, repo, enq)
				h.processFn = func(context.Context, *jobs.Job) error {
					t.Fatal("processFn must not be called when the opts guard rejects the job")
					return nil
				}

				err := pe.wrapper(h, context.Background(), newTask(t, pe.taskType, jobID))

				if !errors.Is(err, asynq.SkipRetry) {
					t.Fatalf("%s: err = %v, want errors.Is(err, asynq.SkipRetry)", oc.engine, err)
				}
				if len(repo.markFailed) != 1 {
					t.Fatalf("%s: MarkFailed calls = %d, want 1", oc.engine, len(repo.markFailed))
				}
				if got := repo.markFailed[0]; got.code != "invalid_options" || got.message != "stored conversion options are invalid" || got.attempt != 0 {
					t.Fatalf("%s: MarkFailed(code=%q, message=%q, attempt=%v), want (code=invalid_options, message=%q, attempt=0)", oc.engine, got.code, got.message, got.attempt, "stored conversion options are invalid")
				}
				if n := countToken(calls, "MarkActive"); n != 0 {
					t.Fatalf("%s: MarkActive appears %d time(s) in %v, want 0 (opts guard runs BEFORE MarkActive)", oc.engine, n, calls)
				}
				wantWebhook := 0
				if callbackURL != "" {
					wantWebhook = 1
				}
				if n := countToken(calls, "EnqueueWebhookDeliver"); n != wantWebhook {
					t.Fatalf("%s: EnqueueWebhookDeliver calls = %d, want %d", oc.engine, n, wantWebhook)
				}
			})
		}
	}
}

// TestPinnedImageHasNoOptsGuard pins the one deliberate divergence from
// the other six engines: HandleImageConvert performs no opts re-parse at
// all, so an arbitrary Opts map (even one that would fail every other
// engine's own strict parse) reaches MarkActive unchanged.
func TestPinnedImageHasNoOptsGuard(t *testing.T) {
	job, jobID := newPinJob("")
	job.Opts = map[string]any{"pdf_profile": "bogus"} // arbitrary; image never re-parses Opts.
	calls := []string{}
	repo := &pinRepo{job: job, calls: &calls}
	enq := &pinEnqueuer{calls: &calls}
	h := newPinHandler(t, repo, enq)
	h.processFn = pinSuccessProcessFn(repo)

	tc := pinEngineByName(t, "image")
	if err := tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID)); err != nil {
		t.Fatalf("image: err = %v, want nil", err)
	}
	if n := countToken(calls, "MarkActive"); n != 1 {
		t.Fatalf("image: MarkActive calls = %d, want 1 (image performs no opts re-parse guard)", n)
	}
}

// --- TestPinnedAudioStageAsymmetry / TestPinnedAVStageAsymmetry ----------

// stageAsymmetryCase is one row of a stage-tagged terminal/transient
// assertion, driven through the real handler wrapper.
type stageAsymmetryCase struct {
	name     string
	err      error
	terminal bool
}

// TestPinnedAudioStageAsymmetry and TestPinnedAVStageAsymmetry are the two
// engines PITFALLS.md Pitfall 2 names by name: isAudioTerminal and
// isAVTerminal encode DELIBERATELY OPPOSITE stage-level polarities for the
// "same shaped" wrapped-context.DeadlineExceeded timeout error --
// isAudioTerminal's ffmpeg (normalize) stage timeout is TERMINAL while its
// whisper (transcribe) stage timeout is TRANSIENT; isAVTerminal's
// transcode stage timeout is TRANSIENT while its audio-extract/thumbnail
// stage failure (timeout or not) is TERMINAL -- the exact opposite
// polarity from audio's cheap/expensive-stage split. Any future descriptor
// that models terminal/transient as a function of (engine, isTimeout)
// instead of (stage, isTimeout) cannot satisfy both classifiers
// simultaneously; these two tests are therefore the acceptance gate for
// plan 56-04's per-engine descriptor design, driven through the real
// wrapper (not the pure isAudioTerminal/isAVTerminal functions directly)
// so a descriptor refactor that silently drops the stage tag fails here
// even if the pure classifier were somehow preserved unchanged elsewhere.
func TestPinnedAudioStageAsymmetry(t *testing.T) {
	tc := pinEngineByName(t, "audio")
	cases := []stageAsymmetryCase{
		{
			name:     "ffmpeg-stage timeout is terminal",
			err:      fmt.Errorf("convert: %w", fmt.Errorf("audio: ffmpeg: %w", fmt.Errorf("ffmpeg killed: %w", context.DeadlineExceeded))),
			terminal: true,
		},
		{
			name:     "whisper-stage timeout is transient",
			err:      fmt.Errorf("convert: %w", fmt.Errorf("audio: whisper-cli: %w", fmt.Errorf("whisper-cli killed: %w", context.DeadlineExceeded))),
			terminal: false,
		},
	}
	runStageAsymmetryCases(t, tc, cases)
}

func TestPinnedAVStageAsymmetry(t *testing.T) {
	tc := pinEngineByName(t, "av")
	cases := []stageAsymmetryCase{
		{
			name:     "transcode-stage timeout is transient",
			err:      fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVTranscodeFailed, fmt.Errorf("ffmpeg killed: %w", context.DeadlineExceeded))),
			terminal: false,
		},
		{
			name:     "audio-extract-stage failure is terminal",
			err:      fmt.Errorf("convert: %w", fmt.Errorf("%w: %w", convert.ErrAVAudioExtractFailed, errors.New("exit status 1"))),
			terminal: true,
		},
	}
	runStageAsymmetryCases(t, tc, cases)
}

// runStageAsymmetryCases drives every stageAsymmetryCase through the real
// wrapper and asserts the terminal-vs-transient verdict through observable
// effects (MarkFailed count, SkipRetry wrapping, counter movement) --
// never by calling the pure classifier function directly.
func runStageAsymmetryCases(t *testing.T, tc pinEngine, cases []stageAsymmetryCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			job, jobID := newPinJob("")
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error { return c.err }

			labels := map[string]string{"engine": tc.engine, "status": jobs.StatusFailed}
			before := readCounter(t, "octoconv_job_outcomes_total", labels)

			err := tc.wrapper(h, context.Background(), newTask(t, tc.taskType, jobID))

			after := readCounter(t, "octoconv_job_outcomes_total", labels)

			if c.terminal {
				if !errors.Is(err, asynq.SkipRetry) {
					t.Fatalf("%s: err = %v, want errors.Is(err, asynq.SkipRetry) (terminal)", c.name, err)
				}
				if len(repo.markFailed) != 1 {
					t.Fatalf("%s: MarkFailed calls = %d, want 1 (terminal)", c.name, len(repo.markFailed))
				}
				if after != before+1 {
					t.Fatalf("%s: octoconv_job_outcomes_total delta = %v, want 1 (terminal)", c.name, after-before)
				}
				return
			}
			if errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("%s: err = %v, want NOT errors.Is(err, asynq.SkipRetry) (transient)", c.name, err)
			}
			if len(repo.markFailed) != 0 {
				t.Fatalf("%s: MarkFailed calls = %d, want 0 (transient)", c.name, len(repo.markFailed))
			}
			if after != before {
				t.Fatalf("%s: octoconv_job_outcomes_total moved on a transient error", c.name)
			}
		})
	}
}
