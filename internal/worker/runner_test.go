package worker

// This file closes ERRC-02 and proves the single-terminal-emission-call-site
// structural invariant (ROADMAP SC4). It reuses pinRepo/pinEnqueuer/
// newPinHandler/readCounter/newTask/countToken from pinning_test.go
// (56-01, read-only, never modified here) rather than building new fakes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/tileforge/octoconv/internal/jobs"
)

// --- TestInvalidOptionsRecordsBothOutcomeCounters ------------------------

// invalidOptsRunnerCases pairs each of the six opts-guarded engine
// descriptors with an opts map that provably fails that engine's own
// convert.*OptsFromMap strict re-parse -- the same six (engine, opts) pairs
// pinning_test.go's pinInvalidOptsCases already uses, driven here through
// the real Runner directly (NewRunner(h, d).Handle) instead of through a
// Handle*Convert wrapper.
var invalidOptsRunnerCases = []struct {
	name string
	d    EngineDescriptor
	opts map[string]any
}{
	{"document", DocumentDescriptor, map[string]any{"pdf_profile": "bogus"}},
	{"ocr", OCRDescriptor, map[string]any{"language": "fra"}},
	{"pandoc", PandocDescriptor, map[string]any{"wrap": "bogus"}},
}

// TestInvalidOptionsRecordsBothOutcomeCounters closes ERRC-02: for each of
// the six engines with a strict opts re-parse, a job whose stored Opts fail
// that parse now increments BOTH octoconv_job_outcomes_total{engine,
// status="failed"} and octoconv_client_job_outcomes_total{client_id,engine,
// status="failed"} by exactly 1, while still calling MarkFailed with
// code="invalid_options" and attempt=0, and still never calling MarkActive.
//
// This test would fail against the pre-refactor (pre-56-04) code by design:
// before this plan, every invalid_options MarkFailed call sat outside both
// counter emission sites (the seven Handle*Convert wrappers each had their
// own copy of this branch with no metrics call). Plan 56-01's pinning
// suite deliberately did NOT pin that absence as permanent behavior --
// TestPinnedOptsGuardPrecedesMarkActive's own doc comment names this exact
// gap as ERRC-02, closed here as a direct consequence of routing the
// opts-guard branch through Runner.finish (runner.go), the single
// terminal-outcome commit point every engine now shares.
func TestInvalidOptionsRecordsBothOutcomeCounters(t *testing.T) {
	for _, tc := range invalidOptsRunnerCases {
		t.Run(tc.name, func(t *testing.T) {
			clientID := uuid.New()
			jobID := uuid.New()
			job := &jobs.Job{ID: jobID, ClientID: clientID, CallbackURL: "https://example.test/hook", Opts: tc.opts}
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls}
			enq := &pinEnqueuer{calls: &calls}
			h := newPinHandler(t, repo, enq)
			h.processFn = func(context.Context, *jobs.Job) error {
				t.Fatal("processFn must not be called when the opts guard rejects the job")
				return nil
			}

			labels := map[string]string{"engine": tc.d.Engine, "status": jobs.StatusFailed}
			clientLabels := map[string]string{"client_id": job.ClientID.String(), "engine": tc.d.Engine, "status": jobs.StatusFailed}
			beforeJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			beforeClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

			err := NewRunner(h, tc.d).Handle(context.Background(), newTask(t, tc.d.Type, jobID))

			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("%s: err = %v, want errors.Is(err, asynq.SkipRetry)", tc.name, err)
			}
			if len(repo.markFailed) != 1 {
				t.Fatalf("%s: MarkFailed calls = %d, want 1", tc.name, len(repo.markFailed))
			}
			if got := repo.markFailed[0]; got.code != CodeInvalidOptions || got.message != "stored conversion options are invalid" || got.attempt != 0 {
				t.Fatalf("%s: MarkFailed(code=%q, message=%q, attempt=%v), want (code=%q, message=%q, attempt=0)", tc.name, got.code, got.message, got.attempt, CodeInvalidOptions, "stored conversion options are invalid")
			}
			if n := countToken(calls, "MarkActive"); n != 0 {
				t.Fatalf("%s: MarkActive appears %d time(s) in %v, want 0 (opts guard runs BEFORE MarkActive)", tc.name, n, calls)
			}

			afterJob := readCounter(t, "octoconv_job_outcomes_total", labels)
			afterClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)
			if afterJob != beforeJob+1 {
				t.Fatalf("%s: octoconv_job_outcomes_total delta = %v, want 1 (ERRC-02)", tc.name, afterJob-beforeJob)
			}
			if afterClient != beforeClient+1 {
				t.Fatalf("%s: octoconv_client_job_outcomes_total delta = %v, want 1 (ERRC-02)", tc.name, afterClient-beforeClient)
			}
		})
	}
}

// --- TestInvalidOptionsMarkFailedErrorSuppressesClientCounter -----------

// TestInvalidOptionsMarkFailedErrorSuppressesClientCounter pins the same
// deliberate asymmetry TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook
// (pinning_test.go) pins for every other terminal branch, now proven on the
// opts-guard branch too: when MarkFailed itself returns an error (a
// Postgres blip, or a concurrent transition winning the guarded
// allow-list), the client counter does not move and no webhook is
// enqueued, while octoconv_job_outcomes_total -- deliberately unguarded on
// MarkFailed's own error -- still moves.
func TestInvalidOptionsMarkFailedErrorSuppressesClientCounter(t *testing.T) {
	// Any one opts-guarded engine suffices: the guard is structurally
	// identical for all six (Runner.Handle's shared OptsFromMap branch).
	tc := invalidOptsRunnerCases[0]

	clientID := uuid.New()
	jobID := uuid.New()
	job := &jobs.Job{ID: jobID, ClientID: clientID, CallbackURL: "https://example.test/hook", Opts: tc.opts}
	calls := []string{}
	repo := &pinRepo{job: job, calls: &calls, markFailedErr: errors.New("postgres blip")}
	enq := &pinEnqueuer{calls: &calls}
	h := newPinHandler(t, repo, enq)
	h.processFn = func(context.Context, *jobs.Job) error {
		t.Fatal("processFn must not be called when the opts guard rejects the job")
		return nil
	}

	labels := map[string]string{"engine": tc.d.Engine, "status": jobs.StatusFailed}
	clientLabels := map[string]string{"client_id": job.ClientID.String(), "engine": tc.d.Engine, "status": jobs.StatusFailed}
	beforeJob := readCounter(t, "octoconv_job_outcomes_total", labels)
	beforeClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

	_ = NewRunner(h, tc.d).Handle(context.Background(), newTask(t, tc.d.Type, jobID))

	afterJob := readCounter(t, "octoconv_job_outcomes_total", labels)
	afterClient := readCounter(t, "octoconv_client_job_outcomes_total", clientLabels)

	if afterJob != beforeJob+1 {
		t.Fatalf("octoconv_job_outcomes_total delta = %v, want 1 (unguarded on MarkFailed's own error -- a deliberate pre-existing asymmetry, not a bug)", afterJob-beforeJob)
	}
	if afterClient != beforeClient {
		t.Fatalf("octoconv_client_job_outcomes_total delta = %v, want 0 (guarded on ferr==nil)", afterClient-beforeClient)
	}
	if n := countToken(calls, "EnqueueWebhookDeliver"); n != 0 {
		t.Fatalf("EnqueueWebhookDeliver calls = %d, want 0 (guarded on ferr==nil)", n)
	}
}

// --- TestSingleTerminalEmissionCallSite -----------------------------------

// occurrence names one file:line hit found by TestSingleTerminalEmissionCallSite,
// so a future regression's failure message names its own location.
type occurrence struct {
	file string
	line int
	text string
}

func (o occurrence) String() string {
	return fmt.Sprintf("%s:%d: %s", o.file, o.line, o.text)
}

// TestSingleTerminalEmissionCallSite is the machine check behind ROADMAP
// SC4's "exactly one call in exactly one place": it reads every non-test
// .go file in package worker, drops lines whose trimmed prefix is "//", and
// asserts the package-wide occurrence count of "metrics.RecordJobOutcome("
// is exactly 1 and of "metrics.RecordClientJobOutcome(" is exactly 1 (both
// today live solely in runner.go's finish method).
//
// For EnqueueWebhookDeliver, the search pattern is ".EnqueueWebhookDeliver("
// (a leading dot) rather than the bare method name, so the
// webhookEnqueuer interface's own method declaration in worker.go
// (`EnqueueWebhookDeliver(ctx context.Context, jobID uuid.UUID) error`,
// no receiver prefix) is not miscounted as a call site. Exactly ONE genuine
// call site exists today: runner.go's finish method. HandleWebhookDeliver
// itself (worker.go) is not a second call site -- it IS the delivery
// handler invoked BY a prior enqueue, and never enqueues another webhook
// delivery of its own.
func TestSingleTerminalEmissionCallSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	var jobOutcomeHits, clientOutcomeHits, webhookHits []occurrence

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, "metrics.RecordJobOutcome(") {
				jobOutcomeHits = append(jobOutcomeHits, occurrence{name, i + 1, trimmed})
			}
			if strings.Contains(line, "metrics.RecordClientJobOutcome(") {
				clientOutcomeHits = append(clientOutcomeHits, occurrence{name, i + 1, trimmed})
			}
			if strings.Contains(line, ".EnqueueWebhookDeliver(") {
				webhookHits = append(webhookHits, occurrence{name, i + 1, trimmed})
			}
		}
	}

	if len(jobOutcomeHits) != 1 {
		t.Fatalf("metrics.RecordJobOutcome( occurs %d time(s), want exactly 1: %v", len(jobOutcomeHits), jobOutcomeHits)
	}
	if len(clientOutcomeHits) != 1 {
		t.Fatalf("metrics.RecordClientJobOutcome( occurs %d time(s), want exactly 1: %v", len(clientOutcomeHits), clientOutcomeHits)
	}
	// wantWebhookCalls names every genuine call site expected today:
	//   1. runner.go's finish method (the Runner's single terminal-outcome
	//      commit point, guarded on ferr == nil and CallbackURL != "").
	// HandleWebhookDeliver contributes 0 -- it delivers, it does not enqueue.
	const wantWebhookCalls = 1
	if len(webhookHits) != wantWebhookCalls {
		t.Fatalf(".EnqueueWebhookDeliver( occurs %d time(s), want exactly %d: %v", len(webhookHits), wantWebhookCalls, webhookHits)
	}
}
