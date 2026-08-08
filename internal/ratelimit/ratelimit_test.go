package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/clients"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// withClientHeader injects a resolved client into the request context, standing
// in for auth.Middleware (which normally runs before PerClient in the real chain).
func withClientHeader(id uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithClient(r.Context(), &clients.Client{ID: id, Name: "test"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func TestPerClient_OverLimitReturns429WithRetryAfter(t *testing.T) {
	clientID := uuid.New()
	h := withClientHeader(clientID)(PerClient(2)(okHandler()))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: got status %d, want 429", rec.Code)
	}
	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("3rd request: Retry-After header is empty")
	}
	retryAfterInt, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After %q is not an integer seconds value: %v", retryAfter, err)
	}
	if retryAfterInt <= 0 {
		t.Fatalf("Retry-After = %d, want a positive integer", retryAfterInt)
	}

	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}
	if body.Error != "rate limit exceeded" {
		t.Fatalf("429 body error = %q, want %q", body.Error, "rate limit exceeded")
	}
	if body.Code != "rate_limited" {
		t.Fatalf("429 body code = %q, want %q", body.Code, "rate_limited")
	}
}

func TestPerClient_DifferentClientsAreIsolated(t *testing.T) {
	clientA := uuid.New()
	clientB := uuid.New()
	limiter := PerClient(2)

	hA := withClientHeader(clientA)(limiter(okHandler()))
	hB := withClientHeader(clientB)(limiter(okHandler()))

	// Exhaust client A's limit.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
		rec := httptest.NewRecorder()
		hA.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("client A request %d: got status %d, want 200", i+1, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	rec := httptest.NewRecorder()
	hA.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("client A 3rd request: got status %d, want 429", rec.Code)
	}

	// Client B is unaffected by A's burst.
	req = httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	rec = httptest.NewRecorder()
	hB.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("client B 1st request: got status %d, want 200 (should be isolated from client A)", rec.Code)
	}
}

// TestRateLimitBodyCodeUnchanged asserts the 429 body's code field is the
// LITERAL string "rate_limited", not the errcode.RateLimited constant --
// this catches a future rename of the constant's value before it ships,
// since internal/mcpserver/client.go and the frontend match on the literal
// wire value (T-56-06).
func TestRateLimitBodyCodeUnchanged(t *testing.T) {
	h := ByIP(2)(okHandler())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
		req.RemoteAddr = "203.0.113.9:12345"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	req.RemoteAddr = "203.0.113.9:12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: got status %d, want 429", rec.Code)
	}

	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}
	if body.Code != "rate_limited" {
		t.Fatalf("429 body code = %q, want literal %q", body.Code, "rate_limited")
	}
}

func TestByIP_OverLimitReturns429(t *testing.T) {
	h := ByIP(2)(okHandler())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
		req.RemoteAddr = "203.0.113.5:12345"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: got status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("3rd request: Retry-After header is empty")
	}
}
