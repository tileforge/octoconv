package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/ratelimit"
)

// authedClientRequest injects an authenticated client directly into the
// request context via auth.WithClient, bypassing auth.Middleware entirely
// -- these tests only exercise the gate/rate-limit middleware in
// isolation, mirroring the minimal-router shape 58-06-PLAN.md's Task 2
// action calls for, not the full Routes() chain.
func authedClientRequest(method, path string, client *clients.Client) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(auth.WithClient(req.Context(), client))
}

// TestInspectGateRejectsImmediatelyWhenFull: with maxConcurrent=1 and one
// slot already held by an in-flight request, a second concurrent request
// must be rejected within tens of milliseconds, not after the first
// request's slot frees.
func TestInspectGateRejectsImmediatelyWhenFull(t *testing.T) {
	gate := newInspectSemaphore(1)

	started := make(chan struct{})
	release := make(chan struct{})
	blocking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})

	r := chi.NewRouter()
	r.Use(gate.middleware)
	r.Get("/inspect", blocking)

	firstDone := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inspect", nil))
		firstDone <- rec.Code
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never reached the handler")
	}

	start := time.Now()
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/inspect", nil))
	elapsed := time.Since(start)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec2.Code, rec2.Body.String())
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("second request took %v to reject, want near-immediate (<200ms) -- it must not queue behind the held slot", elapsed)
	}
	if got := rec2.Header().Get("Retry-After"); got == "" {
		t.Error("missing Retry-After header on the 429")
	}

	close(release)
	select {
	case code := <-firstDone:
		if code != http.StatusOK {
			t.Fatalf("first request status = %d, want 200", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first request never completed")
	}
}

// TestInspectGateReleasesSlotAfterPanic: the slot must be released even
// when the wrapped handler panics -- mirroring production ordering, where
// middleware.Recoverer runs OUTSIDE (ahead of) this gate in Routes().
func TestInspectGateReleasesSlotAfterPanic(t *testing.T) {
	gate := newInspectSemaphore(1)

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(gate.middleware)
	r.Get("/inspect", panicking)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inspect", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (recovered panic)", rec.Code)
	}

	// The slot must have been released despite the panic -- a second
	// request against the SAME gate must pass, not be rejected.
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r2 := chi.NewRouter()
	r2.Use(middleware.Recoverer)
	r2.Use(gate.middleware)
	r2.Get("/inspect", ok)

	rec2 := httptest.NewRecorder()
	r2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/inspect", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after slot release", rec2.Code)
	}
}

// TestInspectGateReleasesSlotOnClientDisconnect: the slot must be released
// when the request's context is canceled (simulating a client disconnect)
// and the wrapped handler observes it and returns.
func TestInspectGateReleasesSlotOnClientDisconnect(t *testing.T) {
	gate := newInspectSemaphore(1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // honors cancellation, like a well-behaved handler would
	})

	r := chi.NewRouter()
	r.Use(gate.middleware)
	r.Get("/inspect", handler)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/inspect", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never returned after context cancellation")
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r2 := chi.NewRouter()
	r2.Use(gate.middleware)
	r2.Get("/inspect", ok)

	rec2 := httptest.NewRecorder()
	r2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/inspect", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after disconnect-triggered release", rec2.Code)
	}
}

// decodeErrorBody decodes the flat {"error", "code"} shape writeError
// produces.
func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, rec.Body.String())
	}
	return body
}

// TestInspectGateCodeDiffersFromRateLimited: the semaphore's 429 and the
// rate limiter's 429 must carry different machine-readable codes.
func TestInspectGateCodeDiffersFromRateLimited(t *testing.T) {
	gate := newInspectSemaphore(1)
	started := make(chan struct{})
	release := make(chan struct{})
	blocking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})

	r := chi.NewRouter()
	r.Use(gate.middleware)
	r.Get("/inspect", blocking)

	go func() {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/inspect", nil))
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never reached the handler")
	}

	recSem := httptest.NewRecorder()
	r.ServeHTTP(recSem, httptest.NewRequest(http.MethodGet, "/inspect", nil))
	close(release)
	if recSem.Code != http.StatusTooManyRequests {
		t.Fatalf("semaphore status = %d, want 429", recSem.Code)
	}
	semBody := decodeErrorBody(t, recSem)
	if semBody["code"] != errcode.InspectBusy {
		t.Fatalf("semaphore code = %q, want %q", semBody["code"], errcode.InspectBusy)
	}

	client := &clients.Client{ID: uuid.New(), Name: "test-client"}
	rl := chi.NewRouter()
	rl.Use(ratelimit.PerClient(1))
	rl.Get("/rl", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	rl.ServeHTTP(httptest.NewRecorder(), authedClientRequest(http.MethodGet, "/rl", client))
	recRL := httptest.NewRecorder()
	rl.ServeHTTP(recRL, authedClientRequest(http.MethodGet, "/rl", client))
	if recRL.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limit status = %d, want 429", recRL.Code)
	}
	rlBody := decodeErrorBody(t, recRL)
	if rlBody["code"] != errcode.RateLimited {
		t.Fatalf("rate-limit code = %q, want %q", rlBody["code"], errcode.RateLimited)
	}

	if semBody["code"] == rlBody["code"] {
		t.Fatalf("semaphore and rate-limit 429s share code %q, want distinct codes", semBody["code"])
	}
}

// TestInspectRateBudgetIndependentOfPerClientLimit: exhausting the
// inspect-only rate budget for a client must NOT affect that same
// client's requests against the general per-client limiter, and vice
// versa -- the two must carry independent state.
func TestInspectRateBudgetIndependentOfPerClientLimit(t *testing.T) {
	client := &clients.Client{ID: uuid.New(), Name: "test-client"}

	inspectRouter := chi.NewRouter()
	inspectRouter.Use(newInspectRateLimit(1))
	inspectRouter.Get("/inspect", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	generalRouter := chi.NewRouter()
	generalRouter.Use(ratelimit.PerClient(120))
	generalRouter.Get("/jobs", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	// Exhaust the inspect-only budget for this client (rpm=1: first request
	// consumes the only slot in the window, second is rejected).
	inspectRouter.ServeHTTP(httptest.NewRecorder(), authedClientRequest(http.MethodGet, "/inspect", client))
	recInspect := httptest.NewRecorder()
	inspectRouter.ServeHTTP(recInspect, authedClientRequest(http.MethodGet, "/inspect", client))
	if recInspect.Code != http.StatusTooManyRequests {
		t.Fatalf("inspect status = %d, want 429 once its own budget is exhausted", recInspect.Code)
	}

	// The SAME client hitting the general per-client limiter -- an entirely
	// separate limiter instance with its own untouched state -- must still
	// pass.
	recGeneral := httptest.NewRecorder()
	generalRouter.ServeHTTP(recGeneral, authedClientRequest(http.MethodGet, "/jobs", client))
	if recGeneral.Code != http.StatusOK {
		t.Fatalf("general per-client status = %d, want 200 -- exhausting the inspect budget must not affect the general limiter", recGeneral.Code)
	}

	// And the reverse must also hold: POST /v1/jobs traffic from the SAME
	// client passing through the general limiter must not touch inspect's
	// own budget either (this router instance is a fresh one keyed only by
	// its own state, so this is really just confirming the general
	// limiter's own independent counter -- included for symmetry with the
	// requirement's own wording).
	recInspectAgain := httptest.NewRecorder()
	inspectRouter.ServeHTTP(recInspectAgain, authedClientRequest(http.MethodGet, "/inspect", client))
	if recInspectAgain.Code != http.StatusTooManyRequests {
		t.Fatalf("inspect status after unrelated general traffic = %d, want 429 (still exhausted, unaffected by general router activity)", recInspectAgain.Code)
	}
}

// TestInspectGateDefaultsWhenMaxConcurrentNonPositive: maxConcurrent <= 0
// must apply the documented default rather than admitting zero requests.
func TestInspectGateDefaultsWhenMaxConcurrentNonPositive(t *testing.T) {
	gate := newInspectSemaphore(0)
	r := chi.NewRouter()
	r.Use(gate.middleware)
	r.Get("/inspect", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inspect", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- maxConcurrent<=0 must default, not block everything", rec.Code)
	}
}
