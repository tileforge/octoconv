package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestByIP_NotEvadedByForwardedForSpoofing reproduces Gap 2 (RATE-03 /
// ROADMAP SC5 / 01-REVIEW.md CR-01) through the real router: 5 sequential
// requests share one real RemoteAddr (the default httptest peer) but each
// carries a distinct spoofed X-Forwarded-For header. Against a coarse ByIP
// limit of 2/min, the first two requests must pass the limiter (falling
// through to auth, where they get 401 since they carry no API key — that's
// expected and irrelevant to this test) and the remaining three must be
// rejected with 429 by ratelimit.ByIP. If all 5 pass, the coarse pre-auth
// flood guard is being evaded by header variation.
func TestByIP_NotEvadedByForwardedForSpoofing(t *testing.T) {
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{IPRateLimitRPM: 2, MaxUploadBytes: 1 << 20})
	h := srv.Routes()

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+uuid.New().String(), nil)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.9.8.%d", i+1))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if i < 2 {
			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("request %d: got 429, want it to pass ByIP (401 from auth is fine); spoofed X-Forwarded-For=10.9.8.%d", i+1, i+1)
			}
		} else {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: got status %d, want 429 (coarse IP limit evaded by spoofed X-Forwarded-For=10.9.8.%d)", i+1, rec.Code, i+1)
			}
		}
	}
}

// TestHealthRoutes_ServesHealthz proves srv.HealthRoutes() answers
// GET /healthz with 200 when every dependency ping is healthy (Phase 58
// Task 2's <behavior> block, first bullet).
func TestHealthRoutes_ServesHealthz(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.HealthRoutes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHealthRoutes_CarriesNoV1Routes proves HealthRoutes() carries NOT ONE
// /v1 route -- it is a router for /healthz alone, never a second copy of
// the main API surface. A 404 here is the correct, intended answer.
func TestHealthRoutes_CarriesNoV1Routes(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))
	rec := httptest.NewRecorder()
	srv.HealthRoutes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (HealthRoutes() must carry no /v1 routes); body=%s", rec.Code, rec.Body.String())
	}
}

// TestHealthRoutes_NoAuthRequired proves HealthRoutes() never requires an
// Authorization header -- the liveness probe carries no API key and never
// will.
func TestHealthRoutes_NoAuthRequired(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil) // deliberately no Authorization header
	rec := httptest.NewRecorder()
	srv.HealthRoutes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (HealthRoutes() must not require auth); body=%s", rec.Code, rec.Body.String())
	}
}

// TestHealthRoutes_StillServedByMainRouter proves Routes() -- the /v1
// router -- ALSO still answers GET /healthz (D-08): three live consumers
// (deploy/chart/octoconv/templates/job-e2e.yaml, scripts/cancel-liveproof.sh,
// scripts/quota-burst-driver.sh) probe /healthz at API_ADDR, not HEALTH_ADDR,
// and must keep working unmodified.
func TestHealthRoutes_StillServedByMainRouter(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (Routes() must still serve /healthz); body=%s", rec.Code, rec.Body.String())
	}
}

// TestHealthRoutesBodyIdenticalToMainRouter proves the two routers'
// GET /healthz bodies are byte-for-byte identical -- the whole premise of
// this task is that adding a second listener is a MOVE (same handler,
// second address), never a rewrite. If this test ever fails, the fix is to
// make HealthRoutes()'s output match Routes()'s output again, never to
// update an expectation.
func TestHealthRoutesBodyIdenticalToMainRouter(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req1 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec1 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec2 := httptest.NewRecorder()
	srv.HealthRoutes().ServeHTTP(rec2, req2)

	if rec1.Code != rec2.Code {
		t.Fatalf("status mismatch: Routes()=%d, HealthRoutes()=%d", rec1.Code, rec2.Code)
	}
	if !bytes.Equal(rec1.Body.Bytes(), rec2.Body.Bytes()) {
		t.Fatalf("body mismatch: Routes()=%s, HealthRoutes()=%s", rec1.Body.String(), rec2.Body.String())
	}
}
