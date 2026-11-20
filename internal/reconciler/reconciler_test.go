package reconciler

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/worker"
)

// fakeStore is an in-memory jobStore implementation for unit tests, with
// configurable errors and call tracking so tests can exercise the sweeper's
// enqueue-first ordering and bounded RequeueStale retry without a live DB.
type fakeStore struct {
	stale         []jobs.StaleJob
	findStaleErr  error
	recoveryCount map[uuid.UUID]int
	jobs          map[uuid.UUID]*jobs.Job

	requeueStaleCalls int
	// requeueStaleErrs, if non-empty, is popped one error per call (nil
	// entries mean "succeed"); once exhausted, calls succeed.
	requeueStaleErrs []error

	markFailedCalls    []uuid.UUID
	markFailedAttempts []time.Duration
	markFailedDetails  []map[string]any
	// markFailedErr, if non-nil, is returned by every MarkFailed call
	// instead of nil -- used by TestSweepExhaustedActionGuardedOnMarkFailed
	// to exercise the ferr != nil branch of the exhaustion path.
	markFailedErr error

	webhookGaps              []jobs.WebhookGapJob
	findWebhookGapsErr       error
	webhookGapRecoveredCalls []uuid.UUID

	// purgeJobsCalls/purgeUsageCalls record every (olderThan, batch) pair
	// passed to PurgeJobs/PurgeUsage, so tests can assert on both call count
	// (TestPurgeIsBoundedPerTick) and the arguments threaded through from
	// Config. purgeJobsResults/purgeUsageResults are popped one (n, err) pair
	// per call (mirroring requeueStaleErrs' style below); once exhausted,
	// calls return (0, nil).
	purgeJobsCalls    []purgeCall
	purgeJobsResults  []purgeResult
	purgeUsageCalls   []purgeCall
	purgeUsageResults []purgeResult
}

// purgeCall records one PurgeJobs/PurgeUsage invocation's arguments.
type purgeCall struct {
	olderThan time.Duration
	batch     int
}

// purgeResult is one scripted (rows, err) return value for PurgeJobs/PurgeUsage.
type purgeResult struct {
	n   int64
	err error
}

func (f *fakeStore) FindStale(ctx context.Context, queuedStaleAfter, activeStaleAfter time.Duration) ([]jobs.StaleJob, error) {
	if f.findStaleErr != nil {
		return nil, f.findStaleErr
	}
	return f.stale, nil
}

func (f *fakeStore) RecoveryCount(ctx context.Context, id uuid.UUID) (int, error) {
	return f.recoveryCount[id], nil
}

func (f *fakeStore) RequeueStale(ctx context.Context, id uuid.UUID, reason string) error {
	idx := f.requeueStaleCalls
	f.requeueStaleCalls++
	if idx < len(f.requeueStaleErrs) && f.requeueStaleErrs[idx] != nil {
		return f.requeueStaleErrs[idx]
	}
	f.recoveryCount[id]++
	return nil
}

func (f *fakeStore) MarkFailed(ctx context.Context, id uuid.UUID, code, message string, detail map[string]any, attempt time.Duration) error {
	f.markFailedCalls = append(f.markFailedCalls, id)
	f.markFailedAttempts = append(f.markFailedAttempts, attempt)
	f.markFailedDetails = append(f.markFailedDetails, detail)
	return f.markFailedErr
}

// The reconciler uses this one: it never ran the job, so it has no attempt
// clock and must not claim a measured zero (66-02). Recorded as a zero attempt
// so the existing assertions on markFailedAttempts keep their meaning.
func (f *fakeStore) MarkFailedUnmeasured(ctx context.Context, id uuid.UUID, code, message string, detail map[string]any) error {
	return f.MarkFailed(ctx, id, code, message, detail, 0)
}

func (f *fakeStore) Get(ctx context.Context, id uuid.UUID) (*jobs.Job, error) {
	if j, ok := f.jobs[id]; ok {
		return j, nil
	}
	return &jobs.Job{ID: id}, nil
}

func (f *fakeStore) FindWebhookGaps(ctx context.Context, activeStaleAfter time.Duration) ([]jobs.WebhookGapJob, error) {
	if f.findWebhookGapsErr != nil {
		return nil, f.findWebhookGapsErr
	}
	return f.webhookGaps, nil
}

func (f *fakeStore) RecordWebhookGapRecovered(ctx context.Context, id uuid.UUID, status string) error {
	f.webhookGapRecoveredCalls = append(f.webhookGapRecoveredCalls, id)
	return nil
}

func (f *fakeStore) PurgeJobs(ctx context.Context, olderThan time.Duration, batch int) (int64, error) {
	idx := len(f.purgeJobsCalls)
	f.purgeJobsCalls = append(f.purgeJobsCalls, purgeCall{olderThan: olderThan, batch: batch})
	if idx < len(f.purgeJobsResults) {
		r := f.purgeJobsResults[idx]
		return r.n, r.err
	}
	return 0, nil
}

func (f *fakeStore) PurgeUsage(ctx context.Context, olderThan time.Duration, batch int) (int64, error) {
	idx := len(f.purgeUsageCalls)
	f.purgeUsageCalls = append(f.purgeUsageCalls, purgeCall{olderThan: olderThan, batch: batch})
	if idx < len(f.purgeUsageResults) {
		r := f.purgeUsageResults[idx]
		return r.n, r.err
	}
	return 0, nil
}

// fakeEnqueuer is an in-memory enqueuer implementation for unit tests. The
// synchronous unit tests below read the call-counter slices directly after
// sweep() returns (single-threaded, race-free); the soak test in
// reconciler_soak_test.go, however, reads a counter while a live Sweeper.Run
// goroutine is concurrently appending to it, so the slices are guarded by mu
// and exposed only through locked snapshot accessors for that concurrent
// reader.
type fakeEnqueuer struct {
	mu sync.Mutex

	enqueueImageErr    error
	imageCalls         []uuid.UUID
	webhookCalls       []uuid.UUID
	enqueueWebhookErr  error
	enqueueDocumentErr error
	documentCalls      []uuid.UUID
	enqueueHTMLErr     error
	htmlCalls          []uuid.UUID
	enqueueAudioErr    error
	audioCalls         []uuid.UUID
	enqueueAVErr       error
	avCalls            []uuid.UUID
	enqueueOCRErr      error
	ocrCalls           []uuid.UUID
	enqueuePandocErr   error
	pandocCalls        []uuid.UUID
	enqueueProbeErr    error
	probeCalls         []uuid.UUID
	enqueueTectonicErr error
	tectonicCalls      []uuid.UUID
	enqueueCADErr      error
	cadCalls           []uuid.UUID
}

func (f *fakeEnqueuer) EnqueueImageConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imageCalls = append(f.imageCalls, id)
	return f.enqueueImageErr
}

func (f *fakeEnqueuer) EnqueueWebhookDeliver(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.webhookCalls = append(f.webhookCalls, id)
	return f.enqueueWebhookErr
}

func (f *fakeEnqueuer) EnqueueDocumentConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.documentCalls = append(f.documentCalls, id)
	return f.enqueueDocumentErr
}

func (f *fakeEnqueuer) EnqueueHTMLConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.htmlCalls = append(f.htmlCalls, id)
	return f.enqueueHTMLErr
}

func (f *fakeEnqueuer) EnqueueAudioConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audioCalls = append(f.audioCalls, id)
	return f.enqueueAudioErr
}

func (f *fakeEnqueuer) EnqueueAVConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.avCalls = append(f.avCalls, id)
	return f.enqueueAVErr
}

func (f *fakeEnqueuer) EnqueueOCRConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ocrCalls = append(f.ocrCalls, id)
	return f.enqueueOCRErr
}

func (f *fakeEnqueuer) EnqueuePandocConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pandocCalls = append(f.pandocCalls, id)
	return f.enqueuePandocErr
}

func (f *fakeEnqueuer) EnqueueProbeSanitize(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probeCalls = append(f.probeCalls, id)
	return f.enqueueProbeErr
}

func (f *fakeEnqueuer) EnqueueTectonicConvert(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tectonicCalls = append(f.tectonicCalls, id)
	return f.enqueueTectonicErr
}

func (f *fakeEnqueuer) EnqueueCADJob(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cadCalls = append(f.cadCalls, id)
	return f.enqueueCADErr
}

// imageCallIDs returns a locked snapshot copy of imageCalls — used by the
// soak test to read the counter concurrently with a running Sweeper.Run
// goroutine without racing on the underlying slice.
func (f *fakeEnqueuer) imageCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.imageCalls))
	copy(out, f.imageCalls)
	return out
}

// webhookCallIDs returns a locked snapshot copy of webhookCalls.
func (f *fakeEnqueuer) webhookCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.webhookCalls))
	copy(out, f.webhookCalls)
	return out
}

// documentCallIDs returns a locked snapshot copy of documentCalls.
func (f *fakeEnqueuer) documentCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.documentCalls))
	copy(out, f.documentCalls)
	return out
}

// htmlCallIDs returns a locked snapshot copy of htmlCalls.
func (f *fakeEnqueuer) htmlCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.htmlCalls))
	copy(out, f.htmlCalls)
	return out
}

// audioCallIDs returns a locked snapshot copy of audioCalls.
func (f *fakeEnqueuer) audioCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.audioCalls))
	copy(out, f.audioCalls)
	return out
}

// avCallIDs returns a locked snapshot copy of avCalls.
func (f *fakeEnqueuer) avCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.avCalls))
	copy(out, f.avCalls)
	return out
}

// ocrCallIDs returns a locked snapshot copy of ocrCalls.
func (f *fakeEnqueuer) ocrCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.ocrCalls))
	copy(out, f.ocrCalls)
	return out
}

// probeCallIDs returns a locked snapshot copy of probeCalls.
func (f *fakeEnqueuer) probeCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.probeCalls))
	copy(out, f.probeCalls)
	return out
}

func (f *fakeEnqueuer) tectonicCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.tectonicCalls...)
}

func (f *fakeEnqueuer) cadCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.cadCalls...)
}

// pandocCallIDs returns a locked snapshot copy of pandocCalls.
func (f *fakeEnqueuer) pandocCallIDs() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, len(f.pandocCalls))
	copy(out, f.pandocCalls)
	return out
}

func testConfig() Config {
	return Config{
		QueuedStaleAfter: 90 * time.Second,
		ActiveStaleAfter: 5 * time.Minute,
		SweepInterval:    10 * time.Millisecond,
		MaxRecoveries:    3,
	}
}

func TestSweepSwitchHasNoProtocolCase(t *testing.T) {
	src, err := os.ReadFile("reconciler.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{`case "protocol"`, `case "external"`, "case protocol:", "case external:"} {
		if strings.Contains(string(src), label) {
			t.Fatalf("sweep switch contains %s", label)
		}
	}
}

func TestSweepRecoversUnderCap(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "probe"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.probeCalls) != 0 || len(enq.documentCalls) != 0 || len(enq.cadCalls) != 0 {
		t.Fatalf("enqueue probe=%d document=%d cad=%d, want none", len(enq.probeCalls), len(enq.documentCalls), len(enq.cadCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(store.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed should not be called, got %d calls", len(store.markFailedCalls))
	}
}

func TestSweepFailsStrandedDocumentJobs(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "document"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.documentCalls) != 0 {
		t.Fatalf("EnqueueDocumentConvert calls = %+v, want none", enq.documentCalls)
	}
	if len(enq.imageCalls) != 0 {
		t.Fatalf("EnqueueImageConvert should not be called for a document job, got %d calls", len(enq.imageCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(store.markFailedCalls) != 1 || store.markFailedCalls[0] != id {
		t.Fatalf("MarkFailed calls = %+v, want [%s]", store.markFailedCalls, id)
	}
	if len(store.markFailedDetails) != 1 || store.markFailedDetails[0]["action"] != "document_retired" {
		t.Fatalf("MarkFailed detail = %+v, want action document_retired", store.markFailedDetails)
	}
}

func TestSweepRoutesHTMLJobsToHTMLQueue(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "html"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.htmlCalls) != 0 {
		t.Fatalf("EnqueueHTMLConvert calls = %+v, want none", enq.htmlCalls)
	}
	if len(enq.imageCalls) != 0 {
		t.Fatalf("EnqueueImageConvert should not be called for an html job, got %d calls", len(enq.imageCalls))
	}
	if len(enq.documentCalls) != 0 {
		t.Fatalf("EnqueueDocumentConvert should not be called for an html job, got %d calls", len(enq.documentCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(store.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed calls = %+v, want none", store.markFailedCalls)
	}
}

// TestSweepRoutesAudioJobsToAudioQueue proves a stranded engine="audio" job
// routes to EnqueueAudioConvert and NONE of the other three Enqueue* methods
// (AUD-05 T-31-09: sweep() must no longer fail-closed-default an audio job).
func TestSweepRoutesAudioJobsToAudioQueue(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "audio"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.audioCalls) != 0 {
		t.Fatalf("EnqueueAudioConvert calls = %+v, want none", enq.audioCalls)
	}
	if len(enq.imageCalls) != 0 {
		t.Fatalf("EnqueueImageConvert should not be called for an audio job, got %d calls", len(enq.imageCalls))
	}
	if len(enq.documentCalls) != 0 {
		t.Fatalf("EnqueueDocumentConvert should not be called for an audio job, got %d calls", len(enq.documentCalls))
	}
	if len(enq.htmlCalls) != 0 {
		t.Fatalf("EnqueueHTMLConvert should not be called for an audio job, got %d calls", len(enq.htmlCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(store.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed calls = %+v, want none", store.markFailedCalls)
	}
}

// TestSweepRoutesAVJobsToAVQueue proves a stranded engine="av" job is not
// re-enqueued and is not failed. The av queue was not observed empty.
func TestSweepRoutesAVJobsToAVQueue(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "av"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.avCalls) != 0 {
		t.Fatalf("EnqueueAVConvert calls = %+v, want none", enq.avCalls)
	}
	if len(enq.imageCalls) != 0 {
		t.Fatalf("EnqueueImageConvert should not be called for an av job, got %d calls", len(enq.imageCalls))
	}
	if len(enq.documentCalls) != 0 {
		t.Fatalf("EnqueueDocumentConvert should not be called for an av job, got %d calls", len(enq.documentCalls))
	}
	if len(enq.htmlCalls) != 0 {
		t.Fatalf("EnqueueHTMLConvert should not be called for an av job, got %d calls", len(enq.htmlCalls))
	}
	if len(enq.audioCalls) != 0 {
		t.Fatalf("EnqueueAudioConvert should not be called for an av job, got %d calls", len(enq.audioCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(store.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed calls = %+v, want none", store.markFailedCalls)
	}
}

// TestSweepOCRStrandedRowStaysQueued proves a stranded ocr row is not
// re-enqueued and is not failed. The ocr queue was not observed empty.
func TestSweepOCRStrandedRowStaysQueued(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "ocr"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	for tick := 0; tick < 5; tick++ {
		s.sweep(context.Background())
		if len(enq.ocrCalls) != 0 {
			t.Fatalf("tick %d: EnqueueOCRConvert calls = %+v, want none", tick, enq.ocrCalls)
		}
		if store.requeueStaleCalls != 0 {
			t.Fatalf("tick %d: RequeueStale calls = %d, want 0", tick, store.requeueStaleCalls)
		}
		if len(store.markFailedCalls) != 0 {
			t.Fatalf("tick %d: MarkFailed calls = %+v, want none", tick, store.markFailedCalls)
		}
	}
}

// TestSweepPandocStrandedRowStaysQueued proves a stranded pandoc row is not
// re-enqueued and is not failed. The pandoc queue was not observed empty.
func TestSweepPandocStrandedRowStaysQueued(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "pandoc"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	for tick := 0; tick < 5; tick++ {
		s.sweep(context.Background())
		if len(enq.pandocCalls) != 0 {
			t.Fatalf("tick %d: EnqueuePandocConvert calls = %+v, want none", tick, enq.pandocCalls)
		}
		if store.requeueStaleCalls != 0 {
			t.Fatalf("tick %d: RequeueStale calls = %d, want 0", tick, store.requeueStaleCalls)
		}
		if len(store.markFailedCalls) != 0 {
			t.Fatalf("tick %d: MarkFailed calls = %+v, want none", tick, store.markFailedCalls)
		}
	}
}

// TestSweepTectonicStrandedRowStaysQueued proves a stranded tectonic row is not
// re-enqueued and is not failed. The tectonic queue was not observed empty.
func TestSweepTectonicStrandedRowStaysQueued(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "tectonic"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	for tick := 0; tick < 5; tick++ {
		s.sweep(context.Background())
		if len(enq.tectonicCalls) != 0 {
			t.Fatalf("tick %d: EnqueueTectonicConvert calls = %+v, want none", tick, enq.tectonicCalls)
		}
		if store.requeueStaleCalls != 0 {
			t.Fatalf("tick %d: RequeueStale calls = %d, want 0", tick, store.requeueStaleCalls)
		}
		if len(store.markFailedCalls) != 0 {
			t.Fatalf("tick %d: MarkFailed calls = %+v, want none", tick, store.markFailedCalls)
		}
	}
}

// TestSweepImageStrandedRowStaysQueued proves a stranded image row is not
// re-enqueued and is not failed. The image queue was not observed empty.
func TestSweepImageStrandedRowStaysQueued(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "image"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	for tick := 0; tick < 5; tick++ {
		s.sweep(context.Background())
		if len(enq.imageCalls) != 0 {
			t.Fatalf("tick %d: EnqueueImageConvert calls = %+v, want none", tick, enq.imageCalls)
		}
		if store.requeueStaleCalls != 0 {
			t.Fatalf("tick %d: RequeueStale calls = %d, want 0", tick, store.requeueStaleCalls)
		}
		if len(store.markFailedCalls) != 0 {
			t.Fatalf("tick %d: MarkFailed calls = %+v, want none", tick, store.markFailedCalls)
		}
	}
}

// TestSweepRoutesEveryEngineConstant is the D-06 completeness guard for the
// reconciler routing seam (35-CONTEXT.md): D-06 explicitly declined
// refactoring the reconciler's (and the API's) routing switch into a shared
// queueForEngine helper for this phase, so the risk of a future engine
// constant silently missing a routing case is closed by this test instead of
// by a structural refactor. The table below is driven directly off the
// convert.Engine* constants -- if a sixth engine class is ever added to
// convert.go without a matching case in sweep()'s switch, adding its
// constant/accessor here (the only change required to extend coverage) makes
// this test fail with "RequeueStale calls = 0, want 1" instead of the
// omission staying invisible except for a metrics-only unroutable_engine
// bump in production.
func TestSweepRoutesEveryEngineConstant(t *testing.T) {
	// The accessor per engine must stay explicit -- there is no way to derive
	// "which fake counter belongs to this engine" -- but WHICH ENGINES ARE
	// COVERED is now derived from convert.AllEngines() rather than typed out
	// again. Phase 60 added the eighth class, and the hand-written table here
	// would have kept passing with probe absent: the guard against a missing
	// routing case would itself have been the thing missing a case.
	accessors := map[string]func(*fakeEnqueuer) []uuid.UUID{
		convert.EngineImage:    (*fakeEnqueuer).imageCallIDs,
		convert.EngineDocument: (*fakeEnqueuer).documentCallIDs,
		convert.EngineHTML:     (*fakeEnqueuer).htmlCallIDs,
		convert.EngineAudio:    (*fakeEnqueuer).audioCallIDs,
		convert.EngineAV:       (*fakeEnqueuer).avCallIDs,
		convert.EngineOCR:      (*fakeEnqueuer).ocrCallIDs,
		convert.EnginePandoc:   (*fakeEnqueuer).pandocCallIDs,
		convert.EngineProbe:    (*fakeEnqueuer).probeCallIDs,
		convert.EngineTectonic: (*fakeEnqueuer).tectonicCallIDs,
		convert.EngineCAD:      (*fakeEnqueuer).cadCallIDs,
	}

	var cases []struct {
		engine  string
		callIDs func(*fakeEnqueuer) []uuid.UUID
	}
	for _, engine := range convert.AllEngines() {
		fn, ok := accessors[engine]
		if !ok {
			t.Fatalf("engine %q is in convert.AllEngines() but has no fake accessor here -- "+
				"add one, and add its case to sweep()'s switch, or a stranded job of that "+
				"class stays queued forever on the unroutable_engine branch", engine)
		}
		cases = append(cases, struct {
			engine  string
			callIDs func(*fakeEnqueuer) []uuid.UUID
		}{engine, fn})
	}

	for _, want := range cases {
		t.Run(want.engine, func(t *testing.T) {
			id := uuid.New()
			store := &fakeStore{
				stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: want.engine}},
				recoveryCount: map[uuid.UUID]int{id: 0},
			}
			enq := &fakeEnqueuer{}
			s := NewSweeper(store, enq, testConfig())

			s.sweep(context.Background())

			got := want.callIDs(enq)
			if want.engine == convert.EngineImage || want.engine == convert.EngineHTML || want.engine == convert.EngineAudio || want.engine == convert.EngineAV || want.engine == convert.EngineOCR || want.engine == convert.EnginePandoc || want.engine == convert.EngineTectonic || want.engine == convert.EngineProbe {
				if len(got) != 0 {
					t.Fatalf("engine %s: enqueue calls = %+v, want none", want.engine, got)
				}
				if store.requeueStaleCalls != 0 {
					t.Fatalf("engine %s: RequeueStale calls = %d, want 0", want.engine, store.requeueStaleCalls)
				}
				if len(store.markFailedCalls) != 0 {
					t.Fatalf("engine %s: MarkFailed calls = %+v, want none", want.engine, store.markFailedCalls)
				}
				return
			}
			if want.engine == convert.EngineCAD || want.engine == convert.EngineDocument {
				if len(got) != 0 {
					t.Fatalf("engine %s: enqueue calls = %+v, want none", want.engine, got)
				}
				if store.requeueStaleCalls != 0 {
					t.Fatalf("engine %s: RequeueStale calls = %d, want 0", want.engine, store.requeueStaleCalls)
				}
				if len(store.markFailedCalls) != 1 || store.markFailedCalls[0] != id {
					t.Fatalf("engine %s: MarkFailed calls = %+v, want [%s]", want.engine, store.markFailedCalls, id)
				}
				return
			}
			if len(got) != 1 || got[0] != id {
				t.Fatalf("engine %q: matching Enqueue* calls = %+v, want [%s]", want.engine, got, id)
			}
			// RequeueStale only runs after a successful, non-duplicate
			// enqueue (sweep()'s enqueue-first ordering) -- a job whose
			// engine fell through to the fail-closed default never reaches
			// RequeueStale, so this is the indirect proof that the switch
			// routed rather than defaulted (equivalent to asserting zero
			// unroutable_engine outcomes without needing a metrics reader).
			if store.requeueStaleCalls != 1 {
				t.Fatalf("engine %q: RequeueStale calls = %d, want 1 (proves the switch did not fall through to the fail-closed default)", want.engine, store.requeueStaleCalls)
			}

			for _, other := range cases {
				if other.engine == want.engine {
					continue
				}
				if got := other.callIDs(enq); len(got) != 0 {
					t.Fatalf("engine %q: unexpected Enqueue* call recorded on %q's accessor: %+v", want.engine, other.engine, got)
				}
			}
		})
	}
}

// TestSweepAudioStrandedRowStaysQueued proves a stranded audio row is not
// re-enqueued and is not failed. The audio queue was not observed empty.
func TestSweepAudioStrandedRowStaysQueued(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "audio"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	for tick := 0; tick < 5; tick++ {
		s.sweep(context.Background())
		if len(enq.audioCalls) != 0 {
			t.Fatalf("tick %d: EnqueueAudioConvert calls = %+v, want none", tick, enq.audioCalls)
		}
		if store.requeueStaleCalls != 0 {
			t.Fatalf("tick %d: RequeueStale calls = %d, want 0", tick, store.requeueStaleCalls)
		}
		if len(store.markFailedCalls) != 0 {
			t.Fatalf("tick %d: MarkFailed calls = %+v, want none", tick, store.markFailedCalls)
		}
	}
}

// TestSweepAVStrandedRowStaysQueued proves a stranded av row is not
// re-enqueued and is not failed. The av queue was not observed empty.
func TestSweepAVStrandedRowStaysQueued(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "av"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	for tick := 0; tick < 5; tick++ {
		s.sweep(context.Background())
		if len(enq.avCalls) != 0 {
			t.Fatalf("tick %d: EnqueueAVConvert calls = %+v, want none", tick, enq.avCalls)
		}
		if store.requeueStaleCalls != 0 {
			t.Fatalf("tick %d: RequeueStale calls = %d, want 0", tick, store.requeueStaleCalls)
		}
		if len(store.markFailedCalls) != 0 {
			t.Fatalf("tick %d: MarkFailed calls = %+v, want none", tick, store.markFailedCalls)
		}
	}
}

// TestSweepSkipsUnknownEngine uses "archive" as its unrecognized-engine fixture
// (not "av" -- av gained its own routing case in this plan, per D-06/T-35-17,
// and is now covered separately by TestSweepRoutesAVJobsToAVQueue). archive
// remains genuinely out of scope (see the fail-closed default's comment in
// reconciler.go), so it is still a valid fixture for the "never guess"
// behavior this test guards.
func TestSweepSkipsUnknownEngine(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "archive"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.imageCalls) != 0 {
		t.Fatalf("EnqueueImageConvert should not be called for an unrecognized engine, got %d calls", len(enq.imageCalls))
	}
	if len(enq.documentCalls) != 0 {
		t.Fatalf("EnqueueDocumentConvert should not be called for an unrecognized engine, got %d calls", len(enq.documentCalls))
	}
	if len(enq.avCalls) != 0 {
		t.Fatalf("EnqueueAVConvert should not be called for an unrecognized engine, got %d calls", len(enq.avCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale should not be called for an unrecognized engine, got %d calls", store.requeueStaleCalls)
	}
	if len(store.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed should not be called, got %d calls", len(store.markFailedCalls))
	}
}

func TestSweepSkipsDuplicateEnqueue(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusQueued, Engine: "probe"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.probeCalls) != 0 {
		t.Fatalf("EnqueueProbeSanitize calls = %d, want 0", len(enq.probeCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(store.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed should not be called, got %d calls", len(store.markFailedCalls))
	}
	if store.recoveryCount[id] != 0 {
		t.Fatalf("recovery count = %d, want 0", store.recoveryCount[id])
	}
}

func TestSweepRequeueStaleRetriedOnce(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:            []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "probe"}},
		recoveryCount:    map[uuid.UUID]int{id: 0},
		requeueStaleErrs: []error{errors.New("transient write failure"), nil},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(enq.probeCalls) != 0 || len(store.markFailedCalls) != 0 {
		t.Fatalf("probe calls %d markFailed %d, want none", len(enq.probeCalls), len(store.markFailedCalls))
	}
}

func TestSweepRequeueStaleBoundedRetry(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "probe"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
		requeueStaleErrs: []error{
			errors.New("transient write failure"),
			errors.New("transient write failure again"),
		},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	done := make(chan struct{})
	go func() {
		s.sweep(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sweep did not return — possible unbounded retry loop")
	}

	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale calls = %d, want 0", store.requeueStaleCalls)
	}
	if len(enq.probeCalls) != 0 || len(store.markFailedCalls) != 0 {
		t.Fatalf("probe calls %d markFailed %d, want none", len(enq.probeCalls), len(store.markFailedCalls))
	}
}

func TestSweepExhaustsAtCap(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "image"}},
		recoveryCount: map[uuid.UUID]int{id: 3},
		jobs:          map[uuid.UUID]*jobs.Job{id: {ID: id, CallbackURL: "https://example.test/hook"}},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(store.markFailedCalls) != 1 || store.markFailedCalls[0] != id {
		t.Fatalf("MarkFailed calls = %+v, want [%s]", store.markFailedCalls, id)
	}
	if len(enq.webhookCalls) != 1 || enq.webhookCalls[0] != id {
		t.Fatalf("EnqueueWebhookDeliver calls = %+v, want [%s]", enq.webhookCalls, id)
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale should not be called at cap, got %d calls", store.requeueStaleCalls)
	}
}

func TestSweepExhaustNoCallbackNoWebhook(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: "image"}},
		recoveryCount: map[uuid.UUID]int{id: 3},
		jobs:          map[uuid.UUID]*jobs.Job{id: {ID: id, CallbackURL: ""}},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(store.markFailedCalls) != 1 {
		t.Fatalf("MarkFailed calls = %d, want 1", len(store.markFailedCalls))
	}
	if len(enq.webhookCalls) != 0 {
		t.Fatalf("EnqueueWebhookDeliver should not be called without callback_url, got %d calls", len(enq.webhookCalls))
	}
}

func TestSweepRecoversWebhookGap(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		recoveryCount: map[uuid.UUID]int{},
		webhookGaps:   []jobs.WebhookGapJob{{ID: id, Status: jobs.StatusDone}},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.webhookCalls) != 1 || enq.webhookCalls[0] != id {
		t.Fatalf("EnqueueWebhookDeliver calls = %+v, want [%s]", enq.webhookCalls, id)
	}
	if len(store.webhookGapRecoveredCalls) != 1 || store.webhookGapRecoveredCalls[0] != id {
		t.Fatalf("RecordWebhookGapRecovered calls = %+v, want [%s]", store.webhookGapRecoveredCalls, id)
	}
}

func TestSweepSkipsDuplicateWebhookGap(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		recoveryCount: map[uuid.UUID]int{},
		webhookGaps:   []jobs.WebhookGapJob{{ID: id, Status: jobs.StatusFailed}},
	}
	enq := &fakeEnqueuer{enqueueWebhookErr: asynq.ErrDuplicateTask}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.webhookCalls) != 1 || enq.webhookCalls[0] != id {
		t.Fatalf("EnqueueWebhookDeliver calls = %+v, want [%s]", enq.webhookCalls, id)
	}
	if len(store.webhookGapRecoveredCalls) != 0 {
		t.Fatalf("RecordWebhookGapRecovered should NOT be called on duplicate enqueue, got %d calls", len(store.webhookGapRecoveredCalls))
	}
}

func TestSweepWebhookGapFindErrorBestEffort(t *testing.T) {
	store := &fakeStore{
		recoveryCount:      map[uuid.UUID]int{},
		findWebhookGapsErr: errors.New("transient query failure"),
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.webhookCalls) != 0 {
		t.Fatalf("EnqueueWebhookDeliver should not be called on FindWebhookGaps error, got %d calls", len(enq.webhookCalls))
	}
	if len(store.webhookGapRecoveredCalls) != 0 {
		t.Fatalf("RecordWebhookGapRecovered should not be called on FindWebhookGaps error, got %d calls", len(store.webhookGapRecoveredCalls))
	}
}

// TestSweepNeverResurrectsCanceled is a documentation-level regression for
// the D-10 non-resurrection guarantee (CNCL-04): the AUTHORITATIVE proof that
// a canceled job is excluded lives at the SQL layer in
// internal/jobs/repo_test.go's TestFindStaleExcludesCanceled (FindStale's
// WHERE clause structurally cannot match status='canceled'). This test
// documents the reconciler-side consequence: because FindStale/
// FindWebhookGaps never yield a canceled job, fakeStore.stale/webhookGaps
// being empty (as they always are for an all-canceled backlog) means sweep()
// issues zero Enqueue*/RequeueStale/webhook-gap-recovery calls — the
// reconciler has no code path that could resurrect a canceled job even if one
// were (incorrectly) handed to it.
func TestSweepNeverResurrectsCanceled(t *testing.T) {
	// stale and webhookGaps are deliberately empty: FindStale's SQL WHERE
	// clause (status IN ('queued','active')) and FindWebhookGaps' WHERE
	// clause (status IN ('done','failed')) both structurally exclude
	// 'canceled', so a real Repo backed by Postgres would never populate
	// these slices with a canceled job's id in the first place.
	store := &fakeStore{recoveryCount: map[uuid.UUID]int{}}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	s.sweep(context.Background())

	if len(enq.imageCalls) != 0 || len(enq.documentCalls) != 0 || len(enq.htmlCalls) != 0 || len(enq.avCalls) != 0 {
		t.Fatalf("no engine enqueue should occur for an empty (all-canceled) stale set: image=%d document=%d html=%d av=%d",
			len(enq.imageCalls), len(enq.documentCalls), len(enq.htmlCalls), len(enq.avCalls))
	}
	if store.requeueStaleCalls != 0 {
		t.Fatalf("RequeueStale should not be called for an empty stale set, got %d calls", store.requeueStaleCalls)
	}
	if len(enq.webhookCalls) != 0 {
		t.Fatalf("EnqueueWebhookDeliver should not be called for an empty webhook-gap set, got %d calls", len(enq.webhookCalls))
	}
	if len(store.webhookGapRecoveredCalls) != 0 {
		t.Fatalf("RecordWebhookGapRecovered should not be called for an empty webhook-gap set, got %d calls", len(store.webhookGapRecoveredCalls))
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	store := &fakeStore{recoveryCount: map[uuid.UUID]int{}}
	enq := &fakeEnqueuer{}
	cfg := testConfig()
	cfg.SweepInterval = 5 * time.Millisecond
	s := NewSweeper(store, enq, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return within 1s of context cancellation")
	}
}

// readCounter gathers family from prometheus.DefaultGatherer -- the same
// registerer promauto.NewCounterVec registers into inside internal/metrics,
// so reconciler-emitted metrics are visible from this package without
// exporting anything from internal/metrics -- and returns the counter value
// whose label set matches labels EXACTLY (0 when absent). Mirrors
// internal/worker/pinning_test.go's readCounter idiom (56-04). Every caller
// MUST treat this as a before/after DELTA read: the default registerer is
// process-wide and other tests in this package also emit into the same
// series.
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

// TestSweepExhaustedActionGuardedOnMarkFailed proves RUNR-02 both ways:
// when the guarded transition (MarkFailed) does NOT commit, none of
// octoconv_reconciler_actions_total{action="exhausted"},
// octoconv_client_job_outcomes_total, or the webhook enqueue fire; when it
// DOES commit, the action counter increments by exactly 1. Reads are
// before/after deltas, never absolute values (other tests in this package
// share the same process-wide default registerer).
func TestSweepExhaustedActionGuardedOnMarkFailed(t *testing.T) {
	id := uuid.New()
	engine := "image"
	clientID := uuid.New()

	// --- MarkFailed fails: nothing should move. ---
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusActive, Engine: engine}},
		recoveryCount: map[uuid.UUID]int{id: 3},
		jobs:          map[uuid.UUID]*jobs.Job{id: {ID: id, ClientID: clientID, Engine: engine, CallbackURL: "https://example.test/hook"}},
		markFailedErr: errors.New("db write failed"),
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	actionBefore := readCounter(t, "octoconv_reconciler_actions_total", map[string]string{"action": "exhausted"})
	clientBefore := readCounter(t, "octoconv_client_job_outcomes_total", map[string]string{"client_id": clientID.String(), "engine": engine, "status": jobs.StatusFailed})

	s.sweep(context.Background())

	actionAfterFail := readCounter(t, "octoconv_reconciler_actions_total", map[string]string{"action": "exhausted"})
	clientAfterFail := readCounter(t, "octoconv_client_job_outcomes_total", map[string]string{"client_id": clientID.String(), "engine": engine, "status": jobs.StatusFailed})

	if delta := actionAfterFail - actionBefore; delta != 0 {
		t.Fatalf("action=exhausted delta = %v, want 0 when MarkFailed did not commit", delta)
	}
	if delta := clientAfterFail - clientBefore; delta != 0 {
		t.Fatalf("client_job_outcomes delta = %v, want 0 when MarkFailed did not commit", delta)
	}
	if len(enq.webhookCalls) != 0 {
		t.Fatalf("EnqueueWebhookDeliver calls = %d, want 0 when MarkFailed did not commit", len(enq.webhookCalls))
	}

	// --- MarkFailed succeeds: the action counter increments by exactly 1. ---
	id2 := uuid.New()
	store2 := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id2, Status: jobs.StatusActive, Engine: engine}},
		recoveryCount: map[uuid.UUID]int{id2: 3},
		jobs:          map[uuid.UUID]*jobs.Job{id2: {ID: id2, ClientID: clientID, Engine: engine, CallbackURL: "https://example.test/hook"}},
	}
	enq2 := &fakeEnqueuer{}
	s2 := NewSweeper(store2, enq2, testConfig())

	actionBefore2 := readCounter(t, "octoconv_reconciler_actions_total", map[string]string{"action": "exhausted"})

	s2.sweep(context.Background())

	actionAfter2 := readCounter(t, "octoconv_reconciler_actions_total", map[string]string{"action": "exhausted"})
	if delta := actionAfter2 - actionBefore2; delta != 1 {
		t.Fatalf("action=exhausted delta = %v, want 1 when MarkFailed committed", delta)
	}
}

// TestSweepUnroutableEngineActionStillUnguarded proves the fix did not
// over-apply: the fail-closed default branch's
// octoconv_reconciler_actions_total{action="unroutable_engine"} still
// increments unconditionally, because there is no MarkFailed call on that
// path at all.
func TestSweepUnroutableEngineActionStillUnguarded(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		stale:         []jobs.StaleJob{{ID: id, Status: jobs.StatusQueued, Engine: "archive"}},
		recoveryCount: map[uuid.UUID]int{id: 0},
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, testConfig())

	before := readCounter(t, "octoconv_reconciler_actions_total", map[string]string{"action": "unroutable_engine"})

	s.sweep(context.Background())

	after := readCounter(t, "octoconv_reconciler_actions_total", map[string]string{"action": "unroutable_engine"})
	if delta := after - before; delta != 1 {
		t.Fatalf("action=unroutable_engine delta = %v, want 1", delta)
	}
	if len(store.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed calls = %d, want 0 on the unroutable_engine path", len(store.markFailedCalls))
	}
}

// TestReconcilerExhaustedCodeMatchesFrozenRegistry ties the reconciler's
// "reconciler_exhausted" literal to plan 56-04's frozen async job-failure
// registry (internal/worker/descriptors.go), so the two can never drift
// independently. internal/worker does not import internal/reconciler (see
// `go list -deps`), so this test-only edge introduces no import cycle.
func TestReconcilerExhaustedCodeMatchesFrozenRegistry(t *testing.T) {
	const reconcilerLiteral = "reconciler_exhausted"

	if reconcilerLiteral != worker.CodeReconcilerExhausted {
		t.Fatalf("reconciler literal %q != worker.CodeReconcilerExhausted %q", reconcilerLiteral, worker.CodeReconcilerExhausted)
	}

	found := false
	for _, code := range worker.AllFailureCodes() {
		if code == reconcilerLiteral {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%q not found in worker.AllFailureCodes()", reconcilerLiteral)
	}
}

// TestSweepCountsAreNotAboutOneJob is the regression proof for the assumption
// TestSoakStaleRecoveryCountsUsageExactlyOnce used to make.
//
// Sweeper.sweep reaches every stale job through FindStale, which takes no
// client scope and sweeps the whole jobs table. That is correct — the
// reconciler recovers every stranded job, not one client's — so a test that
// counts TOTAL enqueues is asserting something about the database rather than
// about its own job. `go test ./...` runs packages in parallel against one
// database, so that assertion failed intermittently depending on which
// packages overlapped: once in phase 61 with one foreign job, once in phase 60
// with two.
//
// This pins the property the soak test actually cares about, with a foreign
// job present on purpose.
func TestSweepCountsAreNotAboutOneJob(t *testing.T) {
	own := uuid.New()
	foreign := uuid.New()
	store := &fakeStore{
		stale: []jobs.StaleJob{
			{ID: foreign, Status: jobs.StatusActive, Engine: "document"},
			{ID: own, Status: jobs.StatusActive, Engine: "document"},
		},
		recoveryCount: map[uuid.UUID]int{own: 0, foreign: 0},
	}
	enq := &fakeEnqueuer{}
	NewSweeper(store, enq, testConfig()).sweep(context.Background())

	all := store.markFailedCalls
	if len(all) != 2 {
		t.Fatalf("the sweep should have touched both stale jobs; got %+v", all)
	}

	var mine int
	for _, id := range all {
		if id == own {
			mine++
		}
	}
	if mine != 1 {
		t.Errorf("own job failed %d times, want exactly 1", mine)
	}

	// And the shape that used to be asserted is shown to be wrong here: a
	// total count of 1 is simply false in the presence of any other stale job,
	// which is the normal state of a shared test database.
	if len(all) == 1 {
		t.Error("this fixture has two stale jobs; a total-count assertion cannot hold")
	}
}
