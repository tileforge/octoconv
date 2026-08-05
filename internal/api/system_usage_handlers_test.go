package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/jobs"
)

// int64Ptr mirrors intPtr (sse_test.go) for the *int64 limit/remaining
// fields.
func int64Ptr(v int64) *int64 { return &v }

// strPtr returns a pointer to v, for LimitWithMeta.Engine fixtures.
func strPtr(v string) *string { return &v }

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

// --- Task 3: response semantics (D-02/D-05/D-06/D-07/D-09) ---

// TestResolveUsagePeriod is a pure table test on resolveUsagePeriod with a
// FIXED now, pinning the .UTC() normalization (RESEARCH Pitfall 4): a
// non-UTC instant equal to the same point in time must produce the
// identical result.
func TestResolveUsagePeriod(t *testing.T) {
	nowUTC := time.Date(2026, 8, 5, 23, 59, 0, 0, time.UTC)
	loc := time.FixedZone("+05:00", 5*60*60)
	nowNonUTC := nowUTC.In(loc) // same instant, non-UTC representation

	tests := []struct {
		name        string
		period      string
		wantDayFrom string
		wantDayTo   string
	}{
		{name: "today", period: "today", wantDayFrom: "2026-08-05", wantDayTo: "2026-08-05"},
		{name: "7d", period: "7d", wantDayFrom: "2026-07-30", wantDayTo: "2026-08-05"},
		{name: "30d", period: "30d", wantDayFrom: "2026-07-07", wantDayTo: "2026-08-05"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, now := range []time.Time{nowUTC, nowNonUTC} {
				dayFrom, dayTo := resolveUsagePeriod(tc.period, now)
				gotFrom, gotTo := dayFrom.Format("2006-01-02"), dayTo.Format("2006-01-02")
				if gotFrom != tc.wantDayFrom || gotTo != tc.wantDayTo {
					t.Errorf("resolveUsagePeriod(%q, %v) = (%s, %s), want (%s, %s)", tc.period, now, gotFrom, gotTo, tc.wantDayFrom, tc.wantDayTo)
				}
			}
		})
	}
}

// TestSystemUsage_DefaultPeriodIsToday (D-04): no ?period defaults to
// "today", both day bounds equal today's UTC date, and the response affirms
// remaining is the live admission-gate figure.
func TestSystemUsage_DefaultPeriodIsToday(t *testing.T) {
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Period != "today" {
		t.Errorf("period = %q, want \"today\"", resp.Period)
	}
	wantDay := time.Now().UTC().Format("2006-01-02")
	if resp.DayFrom != wantDay || resp.DayTo != wantDay {
		t.Errorf("day_from=%q day_to=%q, want both %q", resp.DayFrom, resp.DayTo, wantDay)
	}
	if !resp.RemainingMatchesAdmissionGate {
		t.Errorf("remaining_matches_admission_gate = false, want true for period=today")
	}
	if resp.RemainingSemantics != "live_admission_gate" {
		t.Errorf("remaining_semantics = %q, want \"live_admission_gate\"", resp.RemainingSemantics)
	}
	if !usage.gotDayFrom.Equal(usage.gotDayTo) {
		t.Errorf("captured gotDayFrom=%v gotDayTo=%v, want equal for period=today", usage.gotDayFrom, usage.gotDayTo)
	}
}

// TestSystemUsage_MultiDayCarriesCaveat (D-09): a multi-day period marks
// remaining as a non-authoritative hypothetical. Without this caveat an
// operator reads a legitimately-negative multi-day figure as "this client
// is over budget right now", which is false.
func TestSystemUsage_MultiDayCarriesCaveat(t *testing.T) {
	for _, tc := range []struct {
		period    string
		wantSpanD int
	}{
		{period: "7d", wantSpanD: 6},
		{period: "30d", wantSpanD: 29},
	} {
		t.Run(tc.period, func(t *testing.T) {
			usage := &fakeUsageRepo{}
			limits := &fakeLimitsRepo{}
			srv := newOperatorUsageTestServer(usage, limits)

			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, usageRequest("period="+tc.period))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var resp systemUsageResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.RemainingMatchesAdmissionGate {
				t.Errorf("D-09: remaining_matches_admission_gate = true for period=%s, want false — without this caveat an operator reads a legitimately-negative multi-day figure as a live over-budget verdict", tc.period)
			}
			if resp.RemainingSemantics != "hypothetical_single_day_equivalent" {
				t.Errorf("D-09: remaining_semantics = %q, want \"hypothetical_single_day_equivalent\"", resp.RemainingSemantics)
			}
			if resp.RemainingNote == "" {
				t.Errorf("D-09: remaining_note must be non-empty for period=%s", tc.period)
			}
			gotSpan := int(usage.gotDayTo.Sub(usage.gotDayFrom).Hours() / 24)
			if gotSpan != tc.wantSpanD {
				t.Errorf("captured day span = %d days, want %d", gotSpan, tc.wantSpanD)
			}
		})
	}
}

// TestSystemUsage_RemainingMirrorsEvaluateFormula (D-05): the three
// per-dimension remaining formulas must reproduce evaluate()'s exactly. The
// deliberately huge TerminalBytesOut is the trap: if daily_bytes leaks
// bytes_out into the budget arithmetic, it comes out hugely negative
// instead of the expected 500.
func TestSystemUsage_RemainingMirrorsEvaluateFormula(t *testing.T) {
	clientID := uuid.New()
	usage := &fakeUsageRepo{rows: []jobs.UsageRow{{
		ClientID: clientID, Engine: "image",
		TerminalJobCount: 10, TerminalBytesIn: 1000, TerminalBytesOut: 9_000_000,
		InFlightJobCount: 3, InFlightBytesIn: 500,
	}}}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{{
		ClientID: clientID, ClientName: "acme", EnforcementMode: "enforce", Engine: strPtr("image"),
		MaxConcurrent: intPtr(5), MaxDailyBytes: int64Ptr(2000), MaxDailyJobs: int64Ptr(20),
	}}}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 1 {
		t.Fatalf("usage rows = %d, want 1; body=%s", len(resp.Usage), rec.Body.String())
	}
	row := resp.Usage[0]

	if row.Remaining.Concurrent == nil || *row.Remaining.Concurrent != 2 {
		t.Errorf("D-05: remaining.concurrent = %v, want 2 (5 - 3 in-flight only, NOT 5 - 13)", row.Remaining.Concurrent)
	}
	if row.Remaining.DailyBytes == nil || *row.Remaining.DailyBytes != 500 {
		t.Errorf("D-05: remaining.daily_bytes = %v, want 500 (2000 - (1000 + 500)) — a huge negative here means bytes_out leaked into the budget arithmetic", row.Remaining.DailyBytes)
	}
	if row.Remaining.DailyJobs == nil || *row.Remaining.DailyJobs != 7 {
		t.Errorf("D-05: remaining.daily_jobs = %v, want 7 (20 - (10 + 3))", row.Remaining.DailyJobs)
	}
}

// TestSystemUsage_UsedTerminalAndInFlightReportedSeparately (D-06): terminal
// and in-flight figures are separate fields, not combined into one "used".
func TestSystemUsage_UsedTerminalAndInFlightReportedSeparately(t *testing.T) {
	clientID := uuid.New()
	usage := &fakeUsageRepo{rows: []jobs.UsageRow{{
		ClientID: clientID, Engine: "image",
		TerminalJobCount: 10, TerminalBytesIn: 1000, TerminalBytesOut: 9000000,
		InFlightJobCount: 3, InFlightBytesIn: 500,
	}}}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	usageArr := raw["usage"].([]any)
	if len(usageArr) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(usageArr))
	}
	rowMap := usageArr[0].(map[string]any)
	if _, present := rowMap["used"]; present {
		t.Errorf("D-06: response row must not have a combined \"used\" field: %v", rowMap)
	}

	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	row := resp.Usage[0]
	if row.UsedTerminal.JobCount != 10 || row.UsedTerminal.BytesIn != 1000 || row.UsedTerminal.BytesOut != 9000000 {
		t.Errorf("D-06: used_terminal = %+v, want {10, 1000, 9000000}", row.UsedTerminal)
	}
	if row.UsedInFlight.JobCount != 3 || row.UsedInFlight.BytesIn != 500 {
		t.Errorf("D-06: used_in_flight = %+v, want {3, 500}", row.UsedInFlight)
	}
}

// TestSystemUsage_NegativeRemainingIsNotClamped: usage exceeding the limits
// yields a negative remaining, not 0. Clamping would hide how far over a
// client is, which is exactly what OPSV-02 needs to size a limit.
func TestSystemUsage_NegativeRemainingIsNotClamped(t *testing.T) {
	clientID := uuid.New()
	usage := &fakeUsageRepo{rows: []jobs.UsageRow{{
		ClientID: clientID, Engine: "image",
		TerminalJobCount: 100, TerminalBytesIn: 100_000,
		InFlightJobCount: 5, InFlightBytesIn: 0,
	}}}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{{
		ClientID: clientID, ClientName: "acme", EnforcementMode: "enforce", Engine: strPtr("image"),
		MaxConcurrent: intPtr(1), MaxDailyBytes: int64Ptr(1000), MaxDailyJobs: int64Ptr(10),
	}}}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	row := resp.Usage[0]
	if row.Remaining.Concurrent == nil || *row.Remaining.Concurrent >= 0 {
		t.Errorf("expected a negative remaining.concurrent, clamping hides how far over a client is (OPSV-02); got %v", row.Remaining.Concurrent)
	}
	if row.Remaining.DailyBytes == nil || *row.Remaining.DailyBytes >= 0 {
		t.Errorf("expected a negative remaining.daily_bytes; got %v", row.Remaining.DailyBytes)
	}
	if row.Remaining.DailyJobs == nil || *row.Remaining.DailyJobs >= 0 {
		t.Errorf("expected a negative remaining.daily_jobs; got %v", row.Remaining.DailyJobs)
	}
}

// TestSystemUsage_UnlimitedMarshalsNullAndExplicitZeroMarshalsZero
// (RESEARCH Pitfall 5): a nil limit pointer marshals as JSON null (never
// 0, never omitted); an explicit MaxConcurrent=0 marshals as a numeric
// zero, staying distinguishable from unlimited.
func TestSystemUsage_UnlimitedMarshalsNullAndExplicitZeroMarshalsZero(t *testing.T) {
	unlimitedClient := uuid.New()
	deniedClient := uuid.New()
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: unlimitedClient, ClientName: "unlimited-co", EnforcementMode: "enforce", Engine: strPtr("image")},
		{ClientID: deniedClient, ClientName: "denied-co", EnforcementMode: "enforce", Engine: strPtr("image"), MaxConcurrent: intPtr(0)},
	}}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	body := rec.Body.String()

	if !strings.Contains(body, `"max_concurrent":null`) {
		t.Errorf("expected an unlimited row's max_concurrent to marshal as null; body=%s", body)
	}
	if !strings.Contains(body, `"concurrent":null`) {
		t.Errorf("expected an unlimited row's remaining.concurrent to marshal as null; body=%s", body)
	}
	if !strings.Contains(body, `"max_concurrent":0`) {
		t.Errorf("expected the explicit-deny row's max_concurrent to marshal as a numeric 0; body=%s", body)
	}

	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 2 {
		t.Fatalf("usage rows = %d, want 2; body=%s", len(resp.Usage), body)
	}
	for _, row := range resp.Usage {
		if row.ClientID == unlimitedClient && row.Limits.MaxConcurrent != nil {
			t.Errorf("unlimited row's max_concurrent key must be present but decode as nil")
		}
		if row.ClientID == deniedClient && (row.Limits.MaxConcurrent == nil || *row.Limits.MaxConcurrent != 0) {
			t.Errorf("explicit-deny row's max_concurrent must decode as a non-nil pointer to 0, got %v", row.Limits.MaxConcurrent)
		}
	}
}

// TestSystemUsage_ClientWithLimitsButNoUsageAppears (OPSV-02): a client
// with a configured limit but zero usage still appears, with remaining
// equal to the full limit — a configured-but-unconsumed limit is precisely
// what an operator asks about.
func TestSystemUsage_ClientWithLimitsButNoUsageAppears(t *testing.T) {
	clientID := uuid.New()
	usage := &fakeUsageRepo{} // no rows at all
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{{
		ClientID: clientID, ClientName: "fresh-co", EnforcementMode: "enforce", Engine: strPtr("document"),
		MaxDailyJobs: int64Ptr(50),
	}}}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 1 {
		t.Fatalf("OPSV-02: usage rows = %d, want exactly 1 (configured-but-unconsumed limit); body=%s", len(resp.Usage), rec.Body.String())
	}
	row := resp.Usage[0]
	if row.ClientID != clientID || row.Engine != "document" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if row.UsedTerminal.JobCount != 0 || row.UsedTerminal.BytesIn != 0 || row.UsedTerminal.BytesOut != 0 {
		t.Errorf("used_terminal should be all-zero for an unconsumed limit, got %+v", row.UsedTerminal)
	}
	if row.UsedInFlight.JobCount != 0 || row.UsedInFlight.BytesIn != 0 {
		t.Errorf("used_in_flight should be all-zero for an unconsumed limit, got %+v", row.UsedInFlight)
	}
	if row.Remaining.DailyJobs == nil || *row.Remaining.DailyJobs != 50 {
		t.Errorf("OPSV-02: remaining.daily_jobs = %v, want 50 (the full, unconsumed limit)", row.Remaining.DailyJobs)
	}
}

// TestSystemUsage_ClientWithUsageButNoLimitsAppears: the inverse — a client
// with usage but only a nil-Engine (metadata-only) limits row appears with
// all three limits and all three remaining as null, and its client_name/
// enforcement_mode carried over from the metadata row.
func TestSystemUsage_ClientWithUsageButNoLimitsAppears(t *testing.T) {
	clientID := uuid.New()
	usage := &fakeUsageRepo{rows: []jobs.UsageRow{{
		ClientID: clientID, Engine: "image",
		TerminalJobCount: 4, TerminalBytesIn: 400,
	}}}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{{
		ClientID: clientID, ClientName: "no-limits-co", EnforcementMode: "observe", Engine: nil,
	}}}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 1 {
		t.Fatalf("usage rows = %d, want 1; body=%s", len(resp.Usage), rec.Body.String())
	}
	row := resp.Usage[0]
	if row.Limits.MaxConcurrent != nil || row.Limits.MaxDailyBytes != nil || row.Limits.MaxDailyJobs != nil {
		t.Errorf("expected all-null limits for a client with no client_limits row, got %+v", row.Limits)
	}
	if row.Remaining.Concurrent != nil || row.Remaining.DailyBytes != nil || row.Remaining.DailyJobs != nil {
		t.Errorf("expected all-null remaining for a client with no client_limits row, got %+v", row.Remaining)
	}
	if row.ClientName != "no-limits-co" || row.EnforcementMode != "observe" {
		t.Errorf("client_name/enforcement_mode not carried over from the metadata row: got name=%q mode=%q", row.ClientName, row.EnforcementMode)
	}
}

// TestSystemUsage_SortedByConsumptionDescending (D-02): clients ordered by
// total TerminalBytesIn+TerminalBytesOut descending, ties broken by
// ClientID ascending; each client's engine rows stay contiguous and
// engine-ascending.
func TestSystemUsage_SortedByConsumptionDescending(t *testing.T) {
	low := uuid.New()
	mid := uuid.New()
	high := uuid.New()

	usage := &fakeUsageRepo{rows: []jobs.UsageRow{
		{ClientID: low, Engine: "video", TerminalBytesIn: 10},
		{ClientID: low, Engine: "audio", TerminalBytesIn: 10},
		{ClientID: mid, Engine: "video", TerminalBytesIn: 500},
		{ClientID: mid, Engine: "audio", TerminalBytesIn: 500},
		{ClientID: high, Engine: "video", TerminalBytesIn: 5000},
		{ClientID: high, Engine: "audio", TerminalBytesIn: 5000},
	}}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 6 {
		t.Fatalf("usage rows = %d, want 6; body=%s", len(resp.Usage), rec.Body.String())
	}

	wantOrder := []uuid.UUID{high, high, mid, mid, low, low}
	for i, row := range resp.Usage {
		if row.ClientID != wantOrder[i] {
			t.Fatalf("row %d: client_id = %s, want %s (descending consumption order)", i, row.ClientID, wantOrder[i])
		}
	}
	// Within each client, engine ascending: audio before video.
	if resp.Usage[0].Engine != "audio" || resp.Usage[1].Engine != "video" {
		t.Errorf("high-consumption client's engines not engine-ascending: got %s, %s", resp.Usage[0].Engine, resp.Usage[1].Engine)
	}
}

// TestSystemUsage_ClientIDNarrowingPassedToBothRepos (D-01): ?client_id
// reaches both repos as a parsed *uuid.UUID; omitting it passes nil to
// both. The envelope shape (still a `usage` array) is identical either way.
func TestSystemUsage_ClientIDNarrowingPassedToBothRepos(t *testing.T) {
	target := uuid.New()

	t.Run("with client_id", func(t *testing.T) {
		usage := &fakeUsageRepo{}
		limits := &fakeLimitsRepo{}
		srv := newOperatorUsageTestServer(usage, limits)

		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, usageRequest("client_id="+target.String()))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if usage.gotClientID == nil || *usage.gotClientID != target {
			t.Errorf("D-01: usage.gotClientID = %v, want %s", usage.gotClientID, target)
		}
		if limits.gotClientID == nil || *limits.gotClientID != target {
			t.Errorf("D-01: limits.gotClientID = %v, want %s", limits.gotClientID, target)
		}
		var resp systemUsageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Usage == nil {
			t.Errorf("envelope shape must still carry a usage array (even if empty), not omit it")
		}
	})

	t.Run("without client_id", func(t *testing.T) {
		usage := &fakeUsageRepo{}
		limits := &fakeLimitsRepo{}
		srv := newOperatorUsageTestServer(usage, limits)

		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, usageRequest(""))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if usage.gotClientID != nil {
			t.Errorf("D-01: usage.gotClientID = %v, want nil", usage.gotClientID)
		}
		if limits.gotClientID != nil {
			t.Errorf("D-01: limits.gotClientID = %v, want nil", limits.gotClientID)
		}
		var resp systemUsageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Usage == nil {
			t.Errorf("envelope shape must still carry a usage array (even if empty), not omit it")
		}
	})
}

// TestSystemUsage_EmptyResultMarshalsAsEmptyArray: both fakes return no
// rows; the raw body must contain "usage":[] and never "usage":null.
func TestSystemUsage_EmptyResultMarshalsAsEmptyArray(t *testing.T) {
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))
	body := rec.Body.String()

	if !strings.Contains(body, `"usage":[]`) {
		t.Errorf("expected \"usage\":[] in an empty result, got body=%s", body)
	}
	if strings.Contains(body, `"usage":null`) {
		t.Errorf("\"usage\":null must never appear, got body=%s", body)
	}
}
