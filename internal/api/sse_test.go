package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/jobs"
)

// sequencedFakeRepo returns a SCRIPTED sequence of *jobs.Job/error values
// from Get, advancing one step per call (pinned at the last entry once
// exhausted). Unlike fakeRepo (handlers_test.go), which always returns the
// SAME static job, this fake exists specifically to drive
// handleJobEvents' poll loop across multiple ticks so emit-on-change /
// terminal-during-poll can be exercised.
type sequencedFakeRepo struct {
	mu    sync.Mutex
	jobs  []*jobs.Job
	errs  []error
	calls int
}

func (r *sequencedFakeRepo) Get(_ context.Context, _ uuid.UUID) (*jobs.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.calls
	if i >= len(r.jobs) {
		i = len(r.jobs) - 1
	}
	r.calls++
	var err error
	if i < len(r.errs) {
		err = r.errs[i]
	}
	var job *jobs.Job
	if i >= 0 && i < len(r.jobs) {
		job = r.jobs[i]
	}
	return job, err
}

func (r *sequencedFakeRepo) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *sequencedFakeRepo) Create(context.Context, jobs.CreateParams) (uuid.UUID, error) {
	return uuid.Nil, errors.New("sequencedFakeRepo: Create not implemented")
}
func (r *sequencedFakeRepo) PreAdmit(context.Context, uuid.UUID, string, int64) error {
	return errors.New("sequencedFakeRepo: PreAdmit not implemented")
}
func (r *sequencedFakeRepo) Outputs(context.Context, uuid.UUID) ([]jobs.Output, error) {
	return nil, errors.New("sequencedFakeRepo: Outputs not implemented")
}
func (r *sequencedFakeRepo) Cancel(context.Context, uuid.UUID) error {
	return errors.New("sequencedFakeRepo: Cancel not implemented")
}
func (r *sequencedFakeRepo) List(context.Context, uuid.UUID, jobs.ListFilter, *jobs.Cursor, int) ([]jobs.JobSummary, *jobs.Cursor, error) {
	return nil, nil, errors.New("sequencedFakeRepo: List not implemented")
}

// newSSETestServer builds a Server around repo with fast-but-inert SSE
// tunables (long poll/heartbeat/cap); individual tests override whichever
// cadence they need to exercise via newSSETestServerWithConfig.
func newSSETestServer(repo Repo) *Server {
	return newSSETestServerWithConfig(repo, Config{
		SSEPollInterval:      5 * time.Millisecond,
		SSEHeartbeatInterval: time.Hour,
		SSEMaxStreamDuration: time.Hour,
	})
}

func newSSETestServerWithConfig(repo Repo, cfg Config) *Server {
	cfg.MaxUploadBytes = 1 << 20
	return NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), cfg)
}

// requestForSSE builds a request whose chi route context already carries the
// {id} URL param (so chi.URLParam works without going through a live
// router -- routes.go's registration is Task 2, not yet present when this
// handler is unit-tested) and whose context carries client via
// auth.WithClient (mirroring what auth.Middleware would install). It returns
// a CancelFunc so disconnect-path tests can simulate the client going away.
func requestForSSE(id uuid.UUID, client *clients.Client) (*http.Request, context.CancelFunc) {
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String()+"/events", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithClient(ctx, client)
	ctx, cancel := context.WithCancel(ctx)
	return req.WithContext(ctx), cancel
}

func intPtr(v int) *int { return &v }

func TestSSE_InitialSnapshotAndEmitOnChange(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	id := uuid.New()
	job1 := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusActive, ProgressPct: intPtr(10), EtaSeconds: intPtr(5)}
	job2 := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusActive, ProgressPct: intPtr(20), EtaSeconds: intPtr(3)}
	repo := &sequencedFakeRepo{jobs: []*jobs.Job{job1, job1, job1, job2, job2}}
	srv := newSSETestServer(repo)

	req, cancel := requestForSSE(id, client)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.handleJobEvents(rec, req)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for repo.callCount() < 4 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for enough polls to observe the change to job2")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancel")
	}

	body := rec.Body.String()
	if n := strings.Count(body, "event: progress"); n != 2 {
		t.Fatalf("progress events = %d, want 2 (initial snapshot + one change); body=%q", n, body)
	}
	if !strings.Contains(body, `"progress_pct":10`) {
		t.Fatalf("body missing initial pct=10 event: %q", body)
	}
	if !strings.Contains(body, `"progress_pct":20`) {
		t.Fatalf("body missing changed pct=20 event: %q", body)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}
}

func TestSSE_Heartbeat(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	id := uuid.New()
	job := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusActive}
	repo := &sequencedFakeRepo{jobs: []*jobs.Job{job}}
	srv := newSSETestServerWithConfig(repo, Config{
		SSEPollInterval:      time.Hour,
		SSEHeartbeatInterval: 5 * time.Millisecond,
		SSEMaxStreamDuration: time.Hour,
	})

	req, cancel := requestForSSE(id, client)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.handleJobEvents(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond) // several heartbeat cycles at 5ms
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after cancel")
	}

	body := rec.Body.String()
	if !strings.Contains(body, ": keepalive\n\n") {
		t.Fatalf("expected at least one heartbeat comment, got %q", body)
	}
}

func TestSSE_TerminalCloseOnConnect(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	id := uuid.New()
	job := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusDone}
	repo := &sequencedFakeRepo{jobs: []*jobs.Job{job}}
	srv := newSSETestServer(repo)

	req, cancel := requestForSSE(id, client)
	defer cancel()
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.handleJobEvents(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return promptly for an already-terminal job")
	}

	if got := repo.callCount(); got != 1 {
		t.Fatalf("Get calls = %d, want 1 (no poll loop entered for an already-terminal job)", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data: {"status":"done"}`) {
		t.Fatalf("missing terminal event: %q", body)
	}
	if strings.Contains(body, "download_url") {
		t.Fatalf("terminal event must never carry download_url (D-02): %q", body)
	}
}

func TestSSE_TerminalDuringPoll(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	id := uuid.New()
	active := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusActive, ProgressPct: intPtr(50)}
	done := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusDone, ProgressPct: intPtr(100)}
	repo := &sequencedFakeRepo{jobs: []*jobs.Job{active, done}}
	srv := newSSETestServer(repo)

	req, cancel := requestForSSE(id, client)
	defer cancel()
	rec := httptest.NewRecorder()

	doneCh := make(chan struct{})
	go func() {
		srv.handleJobEvents(rec, req)
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not close after the job reached a terminal status")
	}

	// Three events land: the initial active snapshot, the change-emit
	// carrying the full done/100 payload (snapshot changed), and the
	// dedicated terminal event carrying {"status":"done"} only (D-02).
	body := rec.Body.String()
	if n := strings.Count(body, "event: progress"); n != 3 {
		t.Fatalf("progress events = %d, want 3 (initial + change + terminal); body=%q", n, body)
	}
	if !strings.Contains(body, `data: {"status":"done"}`) {
		t.Fatalf("missing terminal event: %q", body)
	}
	if !strings.HasSuffix(strings.TrimRight(body, "\n"), `data: {"status":"done"}`) {
		t.Fatalf("terminal event must be the LAST thing written: %q", body)
	}
}

func TestSSE_DisconnectNoGoroutineLeak(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	id := uuid.New()
	job := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusActive}

	runtime.GC()
	before := runtime.NumGoroutine()

	const cycles = 20
	for i := 0; i < cycles; i++ {
		repo := &sequencedFakeRepo{jobs: []*jobs.Job{job}}
		srv := newSSETestServer(repo)
		req, cancel := requestForSSE(id, client)
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			srv.handleJobEvents(rec, req)
			close(done)
		}()

		time.Sleep(2 * time.Millisecond) // let the handler enter its select loop
		cancel()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("cycle %d: handler did not return after disconnect", i)
		}
	}

	runtime.GC()
	after := runtime.NumGoroutine()
	if delta := after - before; delta > 5 {
		t.Fatalf("goroutine count grew by %d after %d connect/disconnect cycles (before=%d after=%d) -- possible leak", delta, cycles, before, after)
	}
}

func TestSSE_MaxStreamDurationCap(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	id := uuid.New()
	job := &jobs.Job{ID: id, ClientID: client.ID, Status: jobs.StatusActive} // never terminal
	repo := &sequencedFakeRepo{jobs: []*jobs.Job{job}}
	srv := newSSETestServerWithConfig(repo, Config{
		SSEPollInterval:      5 * time.Millisecond,
		SSEHeartbeatInterval: time.Hour,
		SSEMaxStreamDuration: 20 * time.Millisecond,
	})

	req, cancel := requestForSSE(id, client)
	defer cancel()
	rec := httptest.NewRecorder()

	start := time.Now()
	done := make(chan struct{})
	go func() {
		srv.handleJobEvents(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return on the max-stream-duration cap")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("handler took %v to return on a 20ms cap -- too slow", elapsed)
	}
}

func TestSSE_CrossClientNotFound(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	otherClientID := uuid.New()
	id := uuid.New()
	job := &jobs.Job{ID: id, ClientID: otherClientID, Status: jobs.StatusQueued}
	repo := &sequencedFakeRepo{jobs: []*jobs.Job{job}}
	srv := newSSETestServer(repo)

	req, cancel := requestForSSE(id, client)
	defer cancel()
	rec := httptest.NewRecorder()
	srv.handleJobEvents(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); strings.Contains(got, "event-stream") {
		t.Fatalf("expected no SSE headers on a 404, got Content-Type=%q", got)
	}
}

func TestSSE_UnknownJobNotFound(t *testing.T) {
	client := &clients.Client{ID: uuid.New()}
	id := uuid.New()
	repo := &sequencedFakeRepo{jobs: []*jobs.Job{nil}, errs: []error{jobs.ErrNotFound}}
	srv := newSSETestServer(repo)

	req, cancel := requestForSSE(id, client)
	defer cancel()
	rec := httptest.NewRecorder()
	srv.handleJobEvents(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
