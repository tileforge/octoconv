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
	"github.com/apaderin/octoconv/internal/errcode"
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

// newOperatorUsageTestServerQuotaDisabled is newOperatorUsageTestServer's
// kill-switch-on variant (plan 53-07): identical except Config also sets
// QuotaEnforcementDisabled: true, so tests can exercise the
// "computed_but_not_enforced" branch of the response's truth claim.
func newOperatorUsageTestServerQuotaDisabled(usage *fakeUsageRepo, limits *fakeLimitsRepo) *Server {
	resolver := newFakeResolver()
	return NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), usage, limits, resolver, healthyDeps(), Config{
		MaxUploadBytes:           1 << 20,
		OperatorClientIDs:        map[uuid.UUID]struct{}{resolver.client.ID: {}},
		QuotaEnforcementDisabled: true,
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
	writeError(wantRec, http.StatusNotFound, errcode.UnknownPreset, noSuchPreset)
	if !bytes.Equal(rec.Body.Bytes(), wantRec.Body.Bytes()) {
		t.Errorf("ROADMAP SC3: body = %q, want byte-identical to noSuchPreset 404 %q", rec.Body.String(), wantRec.Body.String())
	}

	if usage.calls != 0 || limits.calls != 0 {
		t.Errorf("ROADMAP SC3: a non-operator must not cause any DB work; usage.calls=%d limits.calls=%d", usage.calls, limits.calls)
	}
	if usage.todayCalls != 0 {
		t.Errorf("ROADMAP SC3: a non-operator must not cause any DB work, including UTCToday; usage.todayCalls=%d", usage.todayCalls)
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
	writeError(wantRec, http.StatusNotFound, errcode.UnknownPreset, noSuchPreset)
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
				if strings.TrimSpace(rec.Body.String()) != `{"code":"invalid_request","error":"invalid period filter"}` {
					t.Errorf("body = %q, want {\"code\":\"invalid_request\",\"error\":\"invalid period filter\"}", rec.Body.String())
				}
				if usage.calls != 0 || limits.calls != 0 {
					t.Errorf("D-03: invalid period must cost zero repo calls; usage.calls=%d limits.calls=%d", usage.calls, limits.calls)
				}
				if usage.todayCalls != 0 {
					t.Errorf("D-03: invalid period must cost zero repo calls, including UTCToday; usage.todayCalls=%d", usage.todayCalls)
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
	if strings.TrimSpace(rec.Body.String()) != `{"code":"invalid_request","error":"invalid client_id"}` {
		t.Errorf("body = %q, want {\"code\":\"invalid_request\",\"error\":\"invalid client_id\"}", rec.Body.String())
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
// "today", both day bounds equal the fake repo's frozen day, and the
// response affirms remaining is the live admission-gate figure.
//
// This is the ONE pre-existing test this plan (53-07) edits: it previously
// computed its expected day via time.Now().UTC().Format(...) AFTER issuing
// the request, which raced a real UTC-midnight crossing between fixture
// setup and assertion (WR-07 #1, an intermittent CI flake). Freezing
// usage.today removes the process clock from this test entirely — its
// original intent (an omitted ?period resolves to "today") is unchanged.
func TestSystemUsage_DefaultPeriodIsToday(t *testing.T) {
	usage := &fakeUsageRepo{today: time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)}
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
	const wantDay = "2020-03-01"
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

// TestSystemUsage_DayRangeDerivedFromRepoClockNotProcessClock (plan 53-07,
// WR-03): a fake returning a fixed day far from the real one still produces
// that exact day in the response — proving the API process's own clock is
// never consulted. If this test ever passes with a date matching the real
// "today", the handler has regressed to reading the process clock again.
func TestSystemUsage_DayRangeDerivedFromRepoClockNotProcessClock(t *testing.T) {
	usage := &fakeUsageRepo{today: time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("WR-03: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DayFrom != "2020-03-01" {
		t.Errorf("WR-03: day_from = %q, want \"2020-03-01\" — a value matching the real today means the handler is still reading the API process clock, not the repo's UTCToday", resp.DayFrom)
	}
	if resp.DayTo != "2020-03-01" {
		t.Errorf("WR-03: day_to = %q, want \"2020-03-01\" — a value matching the real today means the handler is still reading the API process clock, not the repo's UTCToday", resp.DayTo)
	}
	if usage.todayCalls != 1 {
		t.Errorf("WR-03: usage.todayCalls = %d, want 1 (UTCToday must be called exactly once)", usage.todayCalls)
	}
}

// TestSystemUsage_SevenDayRangeAnchoredOnRepoClock: the multi-day window is
// anchored on the same frozen repo-clock day, not the real today.
func TestSystemUsage_SevenDayRangeAnchoredOnRepoClock(t *testing.T) {
	usage := &fakeUsageRepo{today: time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest("period=7d"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DayFrom != "2020-02-24" {
		t.Errorf("day_from = %q, want \"2020-02-24\" (inclusive 7-day window ending 2020-03-01, 2020 being a leap year)", resp.DayFrom)
	}
	if resp.DayTo != "2020-03-01" {
		t.Errorf("day_to = %q, want \"2020-03-01\"", resp.DayTo)
	}
}

// TestSystemUsage_UTCTodayErrorReturns500BeforeAnyRepoRead (plan 53-07,
// T-53-07-04/T-53-07-05): a UTCToday failure returns 500 with zero
// UsageSummary/ListAllLimits calls (no partial DB work), and the underlying
// error text never reaches the client.
func TestSystemUsage_UTCTodayErrorReturns500BeforeAnyRepoRead(t *testing.T) {
	usage := &fakeUsageRepo{todayErr: errors.New("boom")}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if usage.calls != 0 {
		t.Errorf("UTCToday failure must abort before UsageSummary; usage.calls=%d", usage.calls)
	}
	if limits.calls != 0 {
		t.Errorf("UTCToday failure must abort before ListAllLimits; limits.calls=%d", limits.calls)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("body leaked internal error text: %q", rec.Body.String())
	}
}

// TestSystemUsage_KillSwitchMakesRemainingNotAuthoritative (plan 53-07,
// WR-02): with the service-wide kill-switch on, period=today's remaining
// figures are no longer claimed as authoritative, even though the arithmetic
// still mirrors evaluate().
func TestSystemUsage_KillSwitchMakesRemainingNotAuthoritative(t *testing.T) {
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServerQuotaDisabled(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.QuotaEnforcementDisabled {
		t.Errorf("WR-02: quota_enforcement_disabled = false, want true")
	}
	if resp.RemainingMatchesAdmissionGate {
		t.Errorf("WR-02: remaining_matches_admission_gate = true, want false when the kill-switch is on")
	}
	if resp.RemainingSemantics != "computed_but_not_enforced" {
		t.Errorf("WR-02: remaining_semantics = %q, want \"computed_but_not_enforced\"", resp.RemainingSemantics)
	}
	if resp.RemainingNote == "" || !strings.Contains(resp.RemainingNote, "QUOTA_ENFORCEMENT_DISABLED") {
		t.Errorf("WR-02: remaining_note = %q, want a non-empty note mentioning QUOTA_ENFORCEMENT_DISABLED", resp.RemainingNote)
	}
}

// TestSystemUsage_TodayWithLiveEnforcementIsAuthoritative (plan 53-07,
// WR-02; note text updated by plan 55-04 Task 2/WR-01): the live case still
// asserts remaining is authoritative, AND the note points the reader at the
// per-row enforced field rather than restating the observe-mode caveat as
// prose (the per-row enforced boolean, added by WR-01, now carries that
// fact directly).
func TestSystemUsage_TodayWithLiveEnforcementIsAuthoritative(t *testing.T) {
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
	if resp.QuotaEnforcementDisabled {
		t.Errorf("WR-02: quota_enforcement_disabled = true, want false")
	}
	if !resp.RemainingMatchesAdmissionGate {
		t.Errorf("WR-02: remaining_matches_admission_gate = false, want true")
	}
	if resp.RemainingSemantics != "live_admission_gate" {
		t.Errorf("WR-02: remaining_semantics = %q, want \"live_admission_gate\"", resp.RemainingSemantics)
	}
	if resp.RemainingNote == "" || !strings.Contains(resp.RemainingNote, "enforced") {
		t.Errorf("WR-01: remaining_note = %q, want a non-empty note pointing at the per-row enforced field", resp.RemainingNote)
	}
}

// TestSystemUsage_MultiDayCaveatWinsOverKillSwitch (plan 53-07,
// <design_resolution> locked precedence): a multi-day period's
// hypothetical-figure caveat wins over the kill-switch state, pinning the
// precedence order so a future reorder fails loudly.
func TestSystemUsage_MultiDayCaveatWinsOverKillSwitch(t *testing.T) {
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServerQuotaDisabled(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest("period=7d"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RemainingSemantics != "hypothetical_single_day_equivalent" {
		t.Errorf("precedence: remaining_semantics = %q, want \"hypothetical_single_day_equivalent\" (multi-day wins over the kill-switch)", resp.RemainingSemantics)
	}
	if resp.RemainingMatchesAdmissionGate {
		t.Errorf("precedence: remaining_matches_admission_gate = true, want false")
	}
	if !resp.QuotaEnforcementDisabled {
		t.Errorf("precedence: quota_enforcement_disabled = false, want true (still reported, independent of remaining_semantics)")
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
	// A real ListAllLimits always carries a metadata entry for any client
	// with usage (WR-02 fix, Task 1) — an all-empty limits fake would now
	// make this usage row's metadata lookup miss and skip the row entirely.
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: clientID, ClientName: "usage-only-co", EnforcementMode: "observe", Engine: nil},
	}}
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
	// A real ListAllLimits always carries a metadata entry for any client
	// with usage (WR-02 fix, Task 1) — an all-empty limits fake would now
	// make every usage row's metadata lookup miss and skip all six rows.
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: low, ClientName: "low-co", EnforcementMode: "observe", Engine: nil},
		{ClientID: mid, ClientName: "mid-co", EnforcementMode: "observe", Engine: nil},
		{ClientID: high, ClientName: "high-co", EnforcementMode: "observe", Engine: nil},
	}}
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

// --- Gap closure (53-05): ROADMAP SC2 / OPSV-02 — idle unlimited clients
// must be visible and distinguishable from a nonexistent client_id
// (53-VERIFICATION.md Gap #1 / 53-REVIEW.md CR-01) ---

// TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow (ROADMAP SC2 / OPSV-02):
// the previously-untested "zero usage AND zero limits" merge cell. A client
// with a nil-Engine (metadata-only) LimitWithMeta row and no usage at all
// must still emit exactly one row, not zero.
func TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow(t *testing.T) {
	id := uuid.New()
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: id, ClientName: "idle-co", EnforcementMode: "observe", Engine: nil},
	}}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest("client_id="+id.String()))

	if rec.Code != http.StatusOK {
		t.Fatalf("ROADMAP SC2/OPSV-02: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 1 {
		t.Fatalf("ROADMAP SC2/OPSV-02: usage rows = %d, want exactly 1 for an idle unlimited client (Gap #1 / CR-01); body=%s", len(resp.Usage), rec.Body.String())
	}
	row := resp.Usage[0]
	if row.ClientID != id {
		t.Errorf("ROADMAP SC2/OPSV-02: client_id = %s, want %s", row.ClientID, id)
	}
	if row.Engine != "" {
		t.Errorf("ROADMAP SC2/OPSV-02: engine = %q, want \"\" (usageEngineNone)", row.Engine)
	}
	if row.ClientName != "idle-co" {
		t.Errorf("ROADMAP SC2/OPSV-02: client_name = %q, want \"idle-co\"", row.ClientName)
	}
	if row.EnforcementMode != "observe" {
		t.Errorf("ROADMAP SC2/OPSV-02: enforcement_mode = %q, want \"observe\"", row.EnforcementMode)
	}
	if row.Limits.MaxConcurrent != nil || row.Limits.MaxDailyBytes != nil || row.Limits.MaxDailyJobs != nil {
		t.Errorf("ROADMAP SC2/OPSV-02: expected all-null limits, got %+v", row.Limits)
	}
	if row.Remaining.Concurrent != nil || row.Remaining.DailyBytes != nil || row.Remaining.DailyJobs != nil {
		t.Errorf("ROADMAP SC2/OPSV-02: expected all-null remaining, got %+v", row.Remaining)
	}
	if row.UsedTerminal.JobCount != 0 || row.UsedTerminal.BytesIn != 0 || row.UsedTerminal.BytesOut != 0 {
		t.Errorf("ROADMAP SC2/OPSV-02: expected all-zero used_terminal, got %+v", row.UsedTerminal)
	}
	if row.UsedInFlight.JobCount != 0 || row.UsedInFlight.BytesIn != 0 {
		t.Errorf("ROADMAP SC2/OPSV-02: expected all-zero used_in_flight, got %+v", row.UsedInFlight)
	}
}

// TestSystemUsage_NonexistentClientIDReturnsEmptyUsage: a client_id matching
// no client (both fakes empty) still returns "usage":[] — proving the fix
// did not change the nonexistent-client response shape (ROADMAP SC2/OPSV-02).
func TestSystemUsage_NonexistentClientIDReturnsEmptyUsage(t *testing.T) {
	usage := &fakeUsageRepo{}
	limits := &fakeLimitsRepo{}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest("client_id="+uuid.New().String()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 0 {
		t.Errorf("nonexistent client_id: usage rows = %d, want 0; body=%s", len(resp.Usage), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"usage":[]`) {
		t.Errorf("nonexistent client_id: body must contain \"usage\":[], got body=%s", rec.Body.String())
	}
}

// TestSystemUsage_IdleClientDistinguishableFromNonexistentClient
// (ROADMAP SC2 / OPSV-02, the exact CR-01 / Gap #1 regression): a real idle
// unlimited client and a nonexistent client_id must produce provably
// different response bodies.
func TestSystemUsage_IdleClientDistinguishableFromNonexistentClient(t *testing.T) {
	id := uuid.New()
	idleSrv := newOperatorUsageTestServer(&fakeUsageRepo{}, &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: id, ClientName: "idle-co", EnforcementMode: "observe", Engine: nil},
	}})
	nonexistentSrv := newOperatorUsageTestServer(&fakeUsageRepo{}, &fakeLimitsRepo{})

	idleRec := httptest.NewRecorder()
	idleSrv.Routes().ServeHTTP(idleRec, usageRequest("client_id="+id.String()))

	nonexistentRec := httptest.NewRecorder()
	nonexistentSrv.Routes().ServeHTTP(nonexistentRec, usageRequest("client_id="+id.String()))

	if bytes.Equal(idleRec.Body.Bytes(), nonexistentRec.Body.Bytes()) {
		t.Fatalf("CR-01/Gap #1 regression: idle unlimited client and nonexistent client_id must NOT produce byte-identical bodies; both = %q", idleRec.Body.String())
	}

	var idleResp, nonexistentResp systemUsageResponse
	if err := json.Unmarshal(idleRec.Body.Bytes(), &idleResp); err != nil {
		t.Fatalf("decode idle response: %v", err)
	}
	if err := json.Unmarshal(nonexistentRec.Body.Bytes(), &nonexistentResp); err != nil {
		t.Fatalf("decode nonexistent response: %v", err)
	}
	if len(idleResp.Usage) != 1 {
		t.Errorf("CR-01/Gap #1: idle client usage rows = %d, want 1", len(idleResp.Usage))
	}
	if len(nonexistentResp.Usage) != 0 {
		t.Errorf("CR-01/Gap #1: nonexistent client usage rows = %d, want 0", len(nonexistentResp.Usage))
	}
}

// TestSystemUsage_AllClientsIncludesIdleUnlimitedClients (ROADMAP SC2 /
// OPSV-02, un-narrowed shape): the all-clients response emits at least one
// row per client ListAllLimits returns, including idle unlimited ones, not
// just clients with usage or a configured limit. D-02 (consumption-
// descending sort) is preserved: the busy client sorts first.
func TestSystemUsage_AllClientsIncludesIdleUnlimitedClients(t *testing.T) {
	busy := uuid.New()
	idle1 := uuid.New()
	idle2 := uuid.New()

	usage := &fakeUsageRepo{rows: []jobs.UsageRow{
		{ClientID: busy, Engine: "image", TerminalJobCount: 3, TerminalBytesIn: 3000},
	}}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: busy, ClientName: "busy-co", EnforcementMode: "enforce", Engine: nil},
		{ClientID: idle1, ClientName: "idle-one", EnforcementMode: "observe", Engine: nil},
		{ClientID: idle2, ClientName: "idle-two", EnforcementMode: "observe", Engine: nil},
	}}
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
	if len(resp.Usage) != 3 {
		t.Fatalf("ROADMAP SC2/OPSV-02: usage rows = %d, want 3 (busy + 2 idle unlimited clients); body=%s", len(resp.Usage), rec.Body.String())
	}
	if resp.Usage[0].ClientID != busy {
		t.Errorf("D-02: first row client_id = %s, want %s (consumption-descending)", resp.Usage[0].ClientID, busy)
	}

	seen := map[uuid.UUID]systemUsageRow{}
	for _, row := range resp.Usage {
		seen[row.ClientID] = row
	}
	for _, want := range []struct {
		id   uuid.UUID
		name string
	}{{idle1, "idle-one"}, {idle2, "idle-two"}} {
		row, ok := seen[want.id]
		if !ok {
			t.Fatalf("ROADMAP SC2/OPSV-02: idle client %s (%s) missing from all-clients response", want.id, want.name)
		}
		if row.Engine != "" {
			t.Errorf("ROADMAP SC2/OPSV-02: idle client %s engine = %q, want \"\"", want.name, row.Engine)
		}
		if row.ClientName != want.name {
			t.Errorf("ROADMAP SC2/OPSV-02: idle client %s client_name = %q, want %q", want.id, row.ClientName, want.name)
		}
	}
}

// TestSystemUsage_NonOperatorNoLeak404EvenWithClientsPresent (ROADMAP SC3
// regression on the fix's new data path): the merge fix must not turn the
// endpoint into a client-existence oracle for non-operators. A non-operator
// caller must still receive the byte-identical noSuchPreset 404 with zero
// repo calls, even when the limits fake is populated with nil-Engine rows.
func TestSystemUsage_NonOperatorNoLeak404EvenWithClientsPresent(t *testing.T) {
	otherOperator := uuid.New() // deliberately NOT the resolver's fixed client id
	usage := &fakeUsageRepo{rows: []jobs.UsageRow{
		{ClientID: uuid.New(), Engine: "image", TerminalJobCount: 1, TerminalBytesIn: 100},
	}}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: uuid.New(), ClientName: "co-a", EnforcementMode: "observe", Engine: nil},
		{ClientID: uuid.New(), ClientName: "co-b", EnforcementMode: "observe", Engine: nil},
	}}
	srv, _ := newUsageTestServer(map[uuid.UUID]struct{}{otherOperator: {}}, usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SC3 regression: merge fix must not turn this endpoint into a client-existence oracle for non-operators; status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusForbidden {
		t.Fatalf("SC3 regression: status must never be 403")
	}

	wantRec := httptest.NewRecorder()
	writeError(wantRec, http.StatusNotFound, errcode.UnknownPreset, noSuchPreset)
	if !bytes.Equal(rec.Body.Bytes(), wantRec.Body.Bytes()) {
		t.Errorf("SC3 regression: body = %q, want byte-identical to noSuchPreset 404 %q", rec.Body.String(), wantRec.Body.String())
	}

	if usage.calls != 0 || limits.calls != 0 {
		t.Errorf("SC3 regression: a non-operator must not cause any DB work even when the limits fake is populated; usage.calls=%d limits.calls=%d", usage.calls, limits.calls)
	}
	if usage.todayCalls != 0 {
		t.Errorf("SC3 regression: a non-operator must not cause any DB work, including UTCToday; usage.todayCalls=%d", usage.todayCalls)
	}
}

// TestSystemUsage_UsageRowWithoutClientMetadataIsSkipped (53-REVIEW-GAPS.md
// WR-02): UsageSummary and ListAllLimits run as two unsynchronized reads
// (system_usage_handlers.go:202,208) — a client deleted between them yields
// a usage key with no meta entry. The handler must skip that row rather
// than emit enforcement_mode:"", which would violate the OpenAPI
// enum:[observe, enforce]+required contract. Client A has usage AND
// metadata; client B has usage but no metadata (simulating the
// vanished-between-reads case) — only A's row must appear.
func TestSystemUsage_UsageRowWithoutClientMetadataIsSkipped(t *testing.T) {
	clientA := uuid.New()
	clientB := uuid.New()
	usage := &fakeUsageRepo{rows: []jobs.UsageRow{
		{ClientID: clientA, Engine: "image", TerminalJobCount: 1, TerminalBytesIn: 100},
		{ClientID: clientB, Engine: "image", TerminalJobCount: 2, TerminalBytesIn: 200},
	}}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: clientA, ClientName: "acme", EnforcementMode: "enforce", Engine: strPtr("image")},
	}}
	srv := newOperatorUsageTestServer(usage, limits)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, usageRequest(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("WR-02: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp systemUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Usage) != 1 {
		t.Fatalf("WR-02: usage rows = %d, want exactly 1 (B's metadata-less row must be skipped); body=%s", len(resp.Usage), rec.Body.String())
	}
	if resp.Usage[0].ClientID != clientA {
		t.Errorf("WR-02: surviving row client_id = %s, want %s (A, the one with metadata)", resp.Usage[0].ClientID, clientA)
	}
	for _, row := range resp.Usage {
		if row.EnforcementMode == "" {
			t.Errorf("WR-02: no row may have an empty enforcement_mode, got row for client %s", row.ClientID)
		}
	}
}

// TestSystemUsage_EnforcementModeNeverOutsideEnum sweeps every row of a
// richer fixture (idle-unlimited, limits-but-no-usage, usage-but-no-limits,
// both) and fails if any enforcement_mode falls outside {observe, enforce}
// — the exact enum docs/openapi.yaml declares required.
func TestSystemUsage_EnforcementModeNeverOutsideEnum(t *testing.T) {
	busy := uuid.New()
	idle := uuid.New()
	limitedOnly := uuid.New()
	usage := &fakeUsageRepo{rows: []jobs.UsageRow{
		{ClientID: busy, Engine: "image", TerminalJobCount: 5, TerminalBytesIn: 500},
	}}
	limits := &fakeLimitsRepo{rows: []clients.LimitWithMeta{
		{ClientID: busy, ClientName: "busy-co", EnforcementMode: "enforce", Engine: strPtr("image")},
		{ClientID: idle, ClientName: "idle-co", EnforcementMode: "observe", Engine: nil},
		{ClientID: limitedOnly, ClientName: "limited-co", EnforcementMode: "enforce", Engine: strPtr("document"), MaxDailyJobs: int64Ptr(10)},
	}}
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
	if len(resp.Usage) == 0 {
		t.Fatalf("expected at least one row; body=%s", rec.Body.String())
	}
	for _, row := range resp.Usage {
		if row.EnforcementMode != "observe" && row.EnforcementMode != "enforce" {
			t.Errorf("row for client %s has enforcement_mode=%q, want one of {observe, enforce}", row.ClientID, row.EnforcementMode)
		}
	}
}

// TestSystemUsage_EnforcedIsPerRowNotResponseLevel (53-REVIEW-GAPS.md
// WR-01): the response-level remaining_matches_admission_gate states only
// that the response-level preconditions hold; each row's own `enforced`
// carries whether THAT row is actually enforced. An observe-mode row must
// have enforced:false even while remaining_matches_admission_gate is true.
func TestSystemUsage_EnforcedIsPerRowNotResponseLevel(t *testing.T) {
	observeClient := uuid.New()
	enforceClient := uuid.New()
	newFixture := func() (*fakeUsageRepo, *fakeLimitsRepo) {
		return &fakeUsageRepo{}, &fakeLimitsRepo{rows: []clients.LimitWithMeta{
			{ClientID: observeClient, ClientName: "observe-co", EnforcementMode: "observe", Engine: nil},
			{ClientID: enforceClient, ClientName: "enforce-co", EnforcementMode: "enforce", Engine: nil},
		}}
	}

	// rowEnforced decodes the raw JSON into a map so a field that is
	// OMITTED (rather than present-and-false) fails the assertion — a
	// *bool would silently default to nil/false either way.
	rowEnforced := func(t *testing.T, body []byte, clientID uuid.UUID) any {
		t.Helper()
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("decode raw response: %v", err)
		}
		for _, r := range raw["usage"].([]any) {
			rowMap := r.(map[string]any)
			if rowMap["client_id"] == clientID.String() {
				v, present := rowMap["enforced"]
				if !present {
					t.Fatalf("row for client %s has no \"enforced\" key at all (must never be omitted); row=%v", clientID, rowMap)
				}
				return v
			}
		}
		t.Fatalf("no row found for client %s; body=%s", clientID, body)
		return nil
	}

	t.Run("default server: kill-switch off, period=today", func(t *testing.T) {
		usage, limits := newFixture()
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
		if !resp.RemainingMatchesAdmissionGate {
			t.Fatalf("WR-01: remaining_matches_admission_gate = false, want true")
		}
		if v := rowEnforced(t, rec.Body.Bytes(), observeClient); v != false {
			t.Errorf("WR-01: observe-mode row's enforced = %v, want false even while remaining_matches_admission_gate is true", v)
		}
		if v := rowEnforced(t, rec.Body.Bytes(), enforceClient); v != true {
			t.Errorf("WR-01: enforce-mode row's enforced = %v, want true", v)
		}
	})

	t.Run("kill-switch on: every row enforced false", func(t *testing.T) {
		usage, limits := newFixture()
		srv := newOperatorUsageTestServerQuotaDisabled(usage, limits)

		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, usageRequest(""))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if v := rowEnforced(t, rec.Body.Bytes(), observeClient); v != false {
			t.Errorf("WR-01: kill-switch on, observe row enforced = %v, want false", v)
		}
		if v := rowEnforced(t, rec.Body.Bytes(), enforceClient); v != false {
			t.Errorf("WR-01: kill-switch on, enforce row enforced = %v, want false", v)
		}
	})

	t.Run("period=7d: every row enforced false", func(t *testing.T) {
		usage, limits := newFixture()
		srv := newOperatorUsageTestServer(usage, limits)

		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, usageRequest("period=7d"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if v := rowEnforced(t, rec.Body.Bytes(), observeClient); v != false {
			t.Errorf("WR-01: period=7d, observe row enforced = %v, want false", v)
		}
		if v := rowEnforced(t, rec.Body.Bytes(), enforceClient); v != false {
			t.Errorf("WR-01: period=7d, enforce row enforced = %v, want false", v)
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
