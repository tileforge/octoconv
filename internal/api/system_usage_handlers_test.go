package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// newUsageTestServer builds a Server wired with the supplied usage/limits
// fakes in the usage/limits slots and the given operator allowlist,
// mirroring newOperatorGateTestServer's shape (system_presets_handlers_test.go)
// but returning the fakes' server AND the resolver, so a caller can build
// operatorIDs from resolver.client.ID (the AuthN-resolved caller for every
// request in this file, since fakeResolver always resolves testClientKey to
// the same fixed client).
func newUsageTestServer(operatorIDs map[uuid.UUID]struct{}, usage *fakeUsageRepo, limits *fakeLimitsRepo) (*Server, *fakeResolver) {
	resolver := newFakeResolver()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), usage, limits, resolver, healthyDeps(), Config{
		MaxUploadBytes:    1 << 20,
		OperatorClientIDs: operatorIDs,
	})
	return srv, resolver
}

// newOperatorUsageTestServer is newUsageTestServer's operator-included
// shorthand: build the resolver first, then seed the operator allowlist
// from its fixed client id — the pattern every full-router test in this
// file needs, since operatorIDs must be known before NewServer is called.
func newOperatorUsageTestServer(usage *fakeUsageRepo, limits *fakeLimitsRepo) *Server {
	resolver := newFakeResolver()
	return NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), usage, limits, resolver, healthyDeps(), Config{
		MaxUploadBytes:    1 << 20,
		OperatorClientIDs: map[uuid.UUID]struct{}{resolver.client.ID: {}},
	})
}

// usageRequest builds an authenticated GET /v1/system/usage request.
func usageRequest(rawQuery string) *http.Request {
	target := "/v1/system/usage"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	return authed(httptest.NewRequest(http.MethodGet, target, nil))
}

// TestSystemUsage_NonOperatorNoLeak404 (ROADMAP SC3): a resolved caller NOT
// in the allowlist requests GET /v1/system/usage and gets the exact same
// no-leak 404 body as an unknown preset, never 403, with zero repo calls.
func TestSystemUsage_NonOperatorNoLeak404(t *testing.T) {
	otherOperator := uuid.New() // deliberately NOT the resolver's fixed client id
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv, _ := newUsageTestServer(map[uuid.UUID]struct{}{otherOperator: {}}, usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("ROADMAP SC3: status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusForbidden {
		t.Fatalf("ROADMAP SC3: status must never be 403")
	}

	wantRec := httptest.NewRecorder()
	writeError(wantRec, http.StatusNotFound, noSuchPreset)
	if !bytes.Equal(rec.Body.Bytes(), wantRec.Body.Bytes()) {
		t.Errorf("ROADMAP SC3: body = %q, want byte-identical to noSuchPreset 404 %q", rec.Body.String(), wantRec.Body.String())
	}

	if usage.calls != 0 || limits.calls != 0 {
		t.Errorf("ROADMAP SC3: a non-operator must not cause any DB work; usage.calls=%d limits.calls=%d", usage.calls, limits.calls)
	}
}

// TestSystemUsage_NonOperator404MatchesPresets404Exactly proves the
// byte-identity claim against a LIVE /v1/system/presets response, not just
// the noSuchPreset constant.
func TestSystemUsage_NonOperator404MatchesPresets404Exactly(t *testing.T) {
	otherOperator := uuid.New()
	srv, _ := newUsageTestServer(map[uuid.UUID]struct{}{otherOperator: {}}, &fakeUsageRepo{}, &fakeLimitsRepo{})

	usageRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(usageRec, usageRequest(""))

	presetsRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(presetsRec, authed(httptest.NewRequest(http.MethodGet, "/v1/system/presets", nil)))

	if usageRec.Code != presetsRec.Code {
		t.Fatalf("status codes differ: usage=%d presets=%d", usageRec.Code, presetsRec.Code)
	}
	if usageRec.Header().Get("Content-Type") != presetsRec.Header().Get("Content-Type") {
		t.Fatalf("Content-Type differs: usage=%q presets=%q", usageRec.Header().Get("Content-Type"), presetsRec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(usageRec.Body.Bytes(), presetsRec.Body.Bytes()) {
		t.Errorf("body differs: usage=%q presets=%q", usageRec.Body.String(), presetsRec.Body.String())
	}
}

// TestSystemUsage_EmptyAllowlistDeniesEveryone: an empty/unset
// OPERATOR_CLIENT_IDS denies even a resolved, otherwise-valid caller
// (fail-closed, matching ParseOperatorClientIDs's contract).
func TestSystemUsage_EmptyAllowlistDeniesEveryone(t *testing.T) {
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv, _ := newUsageTestServer(nil, usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	wantRec := httptest.NewRecorder()
	writeError(wantRec, http.StatusNotFound, noSuchPreset)
	if !bytes.Equal(rec.Body.Bytes(), wantRec.Body.Bytes()) {
		t.Errorf("body = %q, want byte-identical to noSuchPreset 404 %q", rec.Body.String(), wantRec.Body.String())
	}
	if usage.calls != 0 || limits.calls != 0 {
		t.Errorf("fail-closed: expected zero repo calls, got usage.calls=%d limits.calls=%d", usage.calls, limits.calls)
	}
}

// TestSystemUsage_InvalidPeriodRejectedBeforeAnyRepoCall (D-03/D-04): an
// unknown ?period is rejected 422 before any repo call; the empty-but-present
// form ?period= takes the "today" default and returns 200.
func TestSystemUsage_InvalidPeriodRejectedBeforeAnyRepoCall(t *testing.T) {
	tests := []struct {
		name       string
		rawQuery   string
		wantStatus int
	}{
		{name: "yesterday", rawQuery: "period=yesterday", wantStatus: http.StatusUnprocessableEntity},
		{name: "1d", rawQuery: "period=1d", wantStatus: http.StatusUnprocessableEntity},
		{name: "uppercase 7D", rawQuery: "period=7D", wantStatus: http.StatusUnprocessableEntity},
		{name: "trailing space", rawQuery: "period=today%20", wantStatus: http.StatusUnprocessableEntity},
		{name: "present but empty defaults to today", rawQuery: "period=", wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage := &fakeUsageRepo{}
			limits := &fakeLimitsRepo{}
			srv := newOperatorUsageTestServer(usage, limits)

			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, usageRequest(tc.rawQuery))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusUnprocessableEntity {
				if strings.TrimSpace(rec.Body.String()) != `{"error":"invalid period filter"}` {
					t.Errorf("body = %q, want {\"error\":\"invalid period filter\"}", rec.Body.String())
				}
				if usage.calls != 0 || limits.calls != 0 {
					t.Errorf("D-03: invalid period must cost zero repo calls; usage.calls=%d limits.calls=%d", usage.calls, limits.calls)
				}
			}
		})
	}
}

// TestSystemUsage_InvalidClientIDRejectedBeforeAnyRepoCall: a malformed
// ?client_id is rejected 400 before any repo call.
func TestSystemUsage_InvalidClientIDRejectedBeforeAnyRepoCall(t *testing.T) {
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest("client_id=not-a-uuid"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != `{"error":"invalid client_id"}` {
		t.Errorf("body = %q, want {\"error\":\"invalid client_id\"}", rec.Body.String())
	}
	if usage.calls != 0 || limits.calls != 0 {
		t.Errorf("expected zero repo calls before validation completes; usage.calls=%d limits.calls=%d", usage.calls, limits.calls)
	}
}

// TestSystemUsage_RepoErrorDoesNotLeakInternals: a distinctive underlying
// repo error never reaches the response body.
func TestSystemUsage_RepoErrorDoesNotLeakInternals(t *testing.T) {
	t.Run("usage repo error", func(t *testing.T) {
		usage := &fakeUsageRepo{err: errors.New(`pq: relation "client_usage_daily" does not exist`)}
		limits := &fakeLimitsRepo{}
		srv := newOperatorUsageTestServer(usage, limits)

		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, usageRequest(""))

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "client_usage_daily") {
			t.Errorf("body leaked internal error text: %q", rec.Body.String())
		}
	})

	t.Run("limits repo error", func(t *testing.T) {
		usage := &fakeUsageRepo{}
		limits := &fakeLimitsRepo{err: errors.New(`pq: relation "client_limits" does not exist`)}
		srv := newOperatorUsageTestServer(usage, limits)

		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, usageRequest(""))

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "client_limits") {
			t.Errorf("body leaked internal error text: %q", rec.Body.String())
		}
	})
}
