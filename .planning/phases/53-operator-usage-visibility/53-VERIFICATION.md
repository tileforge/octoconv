---
phase: 53-operator-usage-visibility
verified: 2026-08-05T23:30:00Z
status: passed
score: 10/10 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 9/10
  gaps_closed:
    - "SC2 / OPSV-02 — Per client, the operator can see the currently configured limits and the remaining budget/quota state (idle unlimited clients were invisible; now emit a synthetic all-unlimited row and are provably distinguishable from a nonexistent client_id)"
  gaps_remaining: []
  regressions: []
---

# Phase 53: Operator Usage Visibility — Verification Report

**Phase Goal:** An operator can answer "who consumed what, by engine class, over what period" and "what are this client's limits and remaining budget" through a dedicated, already-established access-control pattern — not by hand-querying raw Prometheus or the `jobs` table.

**Verified:** 2026-08-05T23:30:00Z
**Status:** passed
**Re-verification:** Yes — after gap closure (plans 53-05, 53-06, 53-07)

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1 — Operator retrieves per-client, per-engine usage for a period via a dedicated `OPERATOR_CLIENT_IDS`-gated endpoint, backed by the Phase 51 rollup (`client_usage_daily`), not a raw `jobs` scan or Prometheus | ✓ VERIFIED (unchanged from initial verification) | `internal/jobs/usage_query.go:75-83` (`usageTerminalSQL`) reads `client_usage_daily` with no status filter; route registered inside `requireOperator` (`routes.go:77-80`). Full suite still green. |
| 2 | **SC2 — Per client, operator can see currently configured limits and remaining budget/quota state, so limits can be set from observed data** (previously FAILED) | ✓ **VERIFIED** | `handleSystemUsage`'s merge (`internal/api/system_usage_handlers.go:248-263`) now unions THREE key sets: usage keys, non-nil-Engine limit keys, and every remaining `ClientID` in `meta` (seeded from `LimitWithMeta`, one row per client regardless of whether it has usage/limits). A client with no `client_limits` row, no usage, and no in-flight jobs now emits exactly one synthetic row (`engine:""`, real `client_name`/`enforcement_mode`, all-null limits/remaining) instead of being silently dropped. **Independently reproduced against a live Postgres instance** (not unit fakes): inserted a real idle/unlimited client + a real operator client with genuine hashed API keys, drove the request through `srv.Routes()` end-to-end (auth → requireOperator → handleSystemUsage → real `jobs.Repo`/`clients.Repo`), and confirmed the idle client's narrowed response (`{"usage":[{...one row, engine:"", client_name:"verify-probe-idle-...", enforcement_mode:"observe", limits: all null...}]}`) is NOT byte-identical to a genuinely nonexistent `client_id`'s response (`{"usage":[]}`) — see Behavioral Spot-Checks. `go test ./internal/api/ -run TestSystemUsage -v` also passes 40/40, including the 5 new regression tests from 53-05 (`TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow`, `TestSystemUsage_NonexistentClientIDReturnsEmptyUsage`, `TestSystemUsage_IdleClientDistinguishableFromNonexistentClient`, `TestSystemUsage_AllClientsIncludesIdleUnlimitedClients`, `TestSystemUsage_NonOperatorNoLeak404EvenWithClientsPresent`). |
| 3 | SC3 — Non-operator caller gets a byte-identical no-leak 404 (never 403), no new information-disclosure surface, **and this must not have regressed after the SC2 fix made client existence more observable** | ✓ VERIFIED — no regression | Structurally guaranteed: `requireOperator` (`system_presets_handlers.go:54-67`) runs as chi middleware BEFORE `handleSystemUsage` and returns 404 (`noSuchPreset`) without ever calling `next.ServeHTTP`, so the handler — including the new third seeding pass and the `UTCToday` call — is never reached for a non-operator. Confirmed three ways: (a) `TestSystemUsage_NonOperator404MatchesPresets404Exactly` compares live bytes against a real `/v1/system/presets` 404; (b) `TestSystemUsage_NonOperatorNoLeak404EvenWithClientsPresent` (new in 53-05) asserts the same 404 even when the limits fake is populated with real client rows — the exact scenario the SC2 fix touches; (c) **independently reproduced live**: a real non-operator client with a genuine API key gets `404 {"error":"preset not found"}`, byte-identical to `/v1/system/presets`'s 404 for the same caller. Zero repo calls asserted via `usage.calls==0 && limits.calls==0 && usage.todayCalls==0` (`system_usage_handlers_test.go:104-105, 1107-1108`) — explicitly including the NEW `UTCToday` read added in 53-07, so the day-resolution fix did not reopen a repo-call leak on the denial path. |
| 4 | D-01 — Same handler/response shape for "all clients" and "?client_id=one" | ✓ VERIFIED (unchanged) | `TestSystemUsage_ClientIDNarrowingPassedToBothRepos` passes. |
| 5 | D-02 — All-clients list sorted by consumption descending | ✓ VERIFIED (unchanged) | `sortSystemUsageRows` untouched byte-for-byte by 53-05 (confirmed by SUMMARY's own diff-grep evidence); `TestSystemUsage_SortedByConsumptionDescending` and the new `TestSystemUsage_AllClientsIncludesIdleUnlimitedClients` (asserting a synthetic row sorts last) both pass. |
| 6 | D-03/D-04 — `?period` fail-closed allow-set, 422 before any repo call including the new `UTCToday` read, default `today` | ✓ VERIFIED | `resolveUsagePeriod`/`validUsagePeriods` unchanged in shape; the new `s.usageRepo.UTCToday(ctx)` call is placed AFTER both the `?period` and `?client_id` validations (`system_usage_handlers.go:183-199`). `TestSystemUsage_InvalidPeriodRejectedBeforeAnyRepoCall` extended with `usage.todayCalls == 0` assertions (line 192-193) and passes live. |
| 7 | D-05 — `remaining` arithmetic mirrors `evaluate()`'s formula, AND the previously-flagged trust gap (WR-02: `remaining_matches_admission_gate` overstating enforcement) is closed | ✓ VERIFIED — **fully addresses the reviewer's own proposed remedy** | `RemainingMatchesAdmissionGate` is now `period == periodToday && !s.quotaEnforcementDisabled` (`system_usage_handlers.go:353`), closing the kill-switch half of WR-02 with a new always-emitted `quota_enforcement_disabled` field, wired end-to-end from `cmd/api/main.go:214` (`QuotaEnforcementDisabled: quotaDisabled`) — the SAME variable passed to `jobsRepo.SetQuotaEnforcementDisabled` two lines below, so the two can never diverge. Per-client `observe`-mode is addressed exactly as 53-REVIEW.md's own "Fix" code sample specified: NOT as a second boolean, but via the pre-existing per-row `enforcement_mode` field plus an explicit prose caveat in `remaining_note` ("A row whose own enforcement_mode is 'observe' has its verdict computed and counted here too, but that row is never actually enforced"), pinned by `TestSystemUsage_TodayWithLiveEnforcementIsAuthoritative` asserting the note text actually contains "observe". This is not a partial fix relative to the review's literal recommendation — the review's own suggested code left observe-mode as a note-level caveat, and the implementation matches it exactly. A third precedence branch (multi-day wins over kill-switch) is also tested (`TestSystemUsage_MultiDayCaveatWinsOverKillSwitch`). |
| 8 | D-06 — `used_terminal`/`used_in_flight` reported separately | ✓ VERIFIED (unchanged) | `TestSystemUsage_UsedTerminalAndInFlightReportedSeparately` passes. |
| 9 | D-09 — Multi-day `remaining` carries a caveat; `today` affirms liveness | ✓ VERIFIED (unchanged, extended) | `TestSystemUsage_MultiDayCarriesCaveat` passes; new `remaining_semantics: computed_but_not_enforced` branch added and tested for the kill-switch-on case. |
| 10 | Unlimited dimension marshals JSON `null`, explicit `0` stays `0` | ✓ VERIFIED (unchanged) | `TestSystemUsage_UnlimitedMarshalsNullAndExplicitZeroMarshalsZero` passes; the new synthetic row also marshals all-null limits/remaining (confirmed in the live probe JSON). |
| 11 | WR-03 — "today" resolved from the API host clock, not Postgres's, causing possible day-boundary drift with `evaluate()` | ✓ VERIFIED CLOSED | `s.usageRepo.UTCToday(ctx)` (`system_usage_handlers.go:195`) replaces `time.Now()`; `grep -c "time.Now()" internal/api/system_usage_handlers.go` = 0 (confirmed). `jobs.Repo.UTCToday` reads `SELECT (now() AT TIME ZONE 'utc')::date` verbatim with zero Go-side re-derivation. `TestUTCTodayMatchesPostgresDay` (live DB) confirms equality with a direct Postgres query; ran live during this verification and passed. |
| 12 | WR-01 — Orphaned `ON DELETE SET NULL` jobs collapsing into a phantom `uuid.Nil` client row in the unnarrowed shape | ✓ VERIFIED CLOSED | Both in-flight SQL statements gained `client_id IS NOT NULL` / `j.client_id IS NOT NULL` guards (`usage_query.go:106, 126`), placed to preserve the partial index's applicability. Ran `TestUsageSummaryExcludesOrphanedNullClientJobs` live against Postgres during this verification (deletes a real client with `ON DELETE SET NULL` in effect) — **PASSED**. |

**Score:** 12/12 truths verified (10 must-have + 2 warning-level fixes explicitly requested for re-check, all closed)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/api/system_usage_handlers.go` | Three-way key-set merge closing SC2 | ✓ VERIFIED | 406 lines, `usageEngineNone` constant + third seeding pass present and correct; compiles, vets, gofmt clean. |
| `internal/jobs/usage_query.go` | `client_id IS NOT NULL` guards + `UTCToday` | ✓ VERIFIED | Both guards present with explanatory comments; `UTCToday` reads Postgres verbatim, no Go-side time manipulation. |
| `internal/clients/limits.go` | `LimitWithMeta` nil-Engine contract (read side, unchanged since initial verification) | ✓ VERIFIED | Doc comment and LEFT JOIN unchanged; correctly the read-side contract the HTTP layer now honors. |
| `internal/api/api.go` | `UsageRepo.UTCToday`, `Config.QuotaEnforcementDisabled`, `Server.quotaEnforcementDisabled` | ✓ VERIFIED | All three present; `false` zero-value is documented as the fail-safe default. |
| `cmd/api/main.go` | Wires `QuotaEnforcementDisabled` from the SAME variable as the kill-switch | ✓ VERIFIED | `api.Config{..., QuotaEnforcementDisabled: quotaDisabled}` (main.go:214), `jobsRepo.SetQuotaEnforcementDisabled(quotaDisabled)` two lines below — same source variable, cannot diverge. |
| `docs/openapi.yaml` | `quota_enforcement_disabled` field, widened `remaining_semantics` enum, engine label documented | ✓ VERIFIED | All present; `required` list for the top-level envelope includes `quota_enforcement_disabled`; per-row `required` list unchanged (no breaking change). |
| `internal/api/system_usage_handlers_test.go` | Regression coverage for SC2 merge cell + SC3 non-regression + WR-02/WR-03 fixes | ✓ VERIFIED | 40 `--- PASS`, 0 `--- FAIL`, 0 `--- SKIP` (live run during this verification). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `internal/api/routes.go` | `s.requireOperator` | `r.Use(s.requireOperator)` inside `/system/usage` sub-router | ✓ WIRED | Unchanged; independently confirmed live — non-operator gets 404 before the handler runs, even with the new merge logic and new `UTCToday` call in place. |
| `handleSystemUsage` merge | `clients.LimitWithMeta` nil-Engine rows | three-way key-set union (`meta` seeding pass) | ✓ **NOW WIRED** (was NOT WIRED in initial verification) | The nil-Engine metadata-only rows now flow all the way to the emitted `keySet`, not just to the `meta` lookup map — confirmed by live probe and by `TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow`. |
| `handleSystemUsage` | `s.usageRepo.UTCToday` | day resolution, placed after validation, before `UsageSummary` | ✓ WIRED | Confirmed via source read and via `TestSystemUsage_DayRangeDerivedFromRepoClockNotProcessClock` / `TestSystemUsage_UTCTodayErrorReturns500BeforeAnyRepoRead` (live pass). |
| `cmd/api/main.go` | `api.Config.QuotaEnforcementDisabled` | same `quotaDisabled` variable as `jobsRepo.SetQuotaEnforcementDisabled` | ✓ WIRED | Single source of truth confirmed by direct code read; no divergence possible. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `handleSystemUsage` | `rows` (final emitted set, incl. synthetic rows) | Three-way union of `usageByKey`, `limitsByKey`, and every `ClientID` in `meta` (from `s.limitsRepo.ListAllLimits` → live Postgres `clients LEFT JOIN client_limits`) | Yes — proven live via an independent probe against a real Postgres instance with a genuinely inserted idle client (not a fake/mock) | ✓ FLOWING (previously ⚠️ HOLLOW for the idle-client subset — now closed) |
| `handleSystemUsage` | `today` / `dayFrom`/`dayTo` | `s.usageRepo.UTCToday` → live Postgres `now()` | Yes — proven live, matches a direct concurrent `SELECT` | ✓ FLOWING |
| `handleSystemUsage` | `s.quotaEnforcementDisabled` | `cmd/api/main.go`'s `quotaDisabled` env-derived variable, same source as the actual admission-gate kill-switch | Yes — single source, cannot diverge | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full package build/vet/fmt clean | `go build ./... && go vet ./... && gofmt -l .` | exit 0, no output | ✓ PASS |
| Unit test suite for the endpoint | `go test ./internal/api/ -run TestSystemUsage -count=1 -v` | 40/40 `--- PASS`, 0 FAIL, 0 SKIP | ✓ PASS |
| DB-gated WR-01 orphan-exclusion proof | `go test ./internal/jobs/ -run TestUsageSummaryExcludesOrphanedNullClientJobs -count=1 -v` (live Postgres, deletes a real client under `ON DELETE SET NULL`) | `--- PASS` | ✓ PASS |
| DB-gated WR-03 clock-parity proof | `go test ./internal/jobs/ -run TestUTCTodayMatchesPostgresDay -count=1 -v` | `--- PASS` | ✓ PASS |
| Full repo test suite (all packages, DB-gated, live containers) | `go test ./... -count=1` | every package `ok`, 0 FAIL | ✓ PASS |
| **Independent live probe (this verification's own code, not SUMMARY-reported): SC2 idle-client distinguishability** | Custom Go test inserting a real idle client (no `client_limits` row) + a real operator client with genuine hashed API keys, driving `srv.Routes()` end-to-end against the real Postgres, HTTP layer included | Idle client: `200 {"usage":[{"client_id":"...","client_name":"verify-probe-idle-...","engine":"","enforcement_mode":"observe","limits":{all null}...}]}` — NOT byte-identical to nonexistent-client response `200 {"usage":[]}` | ✓ PASS — SC2 gap closed |
| **Independent live probe: SC3 non-regression** | Same probe, non-operator caller against `/v1/system/usage` vs `/v1/system/presets` | Both `404 {"error":"preset not found"}`, byte-identical | ✓ PASS — no regression |
| **Independent live probe: fail-closed 422** | Same probe, `?period=bogus` | `422 {"error":"invalid period filter"}` | ✓ PASS |

*(Probe artifacts were created in a temporary, uncommitted directory for this verification only, run against the live dev database, and fully cleaned up afterward — the 3 temporary probe client rows were deleted from Postgres and no files were left in the git working tree. `git status --porcelain` confirmed clean of any probe residue.)*

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|-----------------|--------------|--------|----------|
| OPSV-01 | 53-01, 53-03, 53-04 | Operator retrieves "who consumed what, by engine class, over period" via `OPERATOR_CLIENT_IDS`-gated endpoint | ✓ SATISFIED (unchanged) | SC1 verified above; endpoint live, tested, DB-backed. |
| OPSV-02 | 53-02, 53-03, 53-04, **53-05, 53-06, 53-07** | Per-client configured limits + remaining budget visible, so limits can be set from observed data | ✓ **SATISFIED** (previously BLOCKED — the gap is now closed) | SC2 now verified above, both via the full test suite and an independent live probe against a real Postgres instance. **`.planning/REQUIREMENTS.md`'s traceability table currently still reads `OPSV-02 \| Phase 53 \| Gap` — this line should be updated to `Complete` since the underlying failure it referenced (53-VERIFICATION.md's SC2 gap) no longer exists.** Flagging explicitly per the requesting teammate's instruction so the orchestrator can make this edit. |

No orphaned requirements: REQUIREMENTS.md maps only OPSV-01/OPSV-02 to Phase 53, both declared in plan frontmatter.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | Previous 🛑 Blocker (union key set omitting nil-Engine rows) | — | **RESOLVED** — third seeding pass now present and correct. |
| `internal/api/system_usage_handlers.go` | 285 (formerly) | `remaining_matches_admission_gate` ignoring the kill-switch | — | **RESOLVED** — now `period == periodToday && !s.quotaEnforcementDisabled`. Per-client observe-mode remains a note-level caveat by deliberate design, matching the reviewer's own proposed fix (see Truth #7 above) — not re-flagged as a gap. |
| `internal/api/system_usage_handlers.go` | 161 (formerly) | `time.Now()` vs Postgres `now()` clock drift | — | **RESOLVED** — `grep -c "time.Now()" internal/api/system_usage_handlers.go` = 0; `UTCToday` used exclusively. |
| `internal/jobs/usage_query.go` | 95-119 (formerly) | Missing `client_id IS NOT NULL` guard on in-flight queries | — | **RESOLVED** — both statements guarded, proven live against a real `ON DELETE SET NULL` deletion. |
| `internal/api/system_usage_handlers.go` | 163-173 | Unbounded all-clients response with no pagination/ceiling (WR-05) | ℹ️ Info | Not addressed by this gap-closure wave; still an accepted risk per D-02 (small internal client population) from the initial review. Not a must-have for SC1/SC2/SC3 — no regression, informational only. |
| `internal/api/system_usage_handlers.go` | 235-261 | Mistyped `client_limits.engine` reported as an enforceable budget the gate never applies (WR-06) | ℹ️ Info | Not addressed by this gap-closure wave. Not a must-have for SC1/SC2/SC3 — informational only, not re-litigated here since the requesting teammate's scope was CR-01/WR-01/WR-02/WR-03. |
| `internal/api/api.go` | 152-157 | "Never nil after NewServer" contract not enforced by a panic (WR-08) | ℹ️ Info | Not addressed; not in scope for this re-verification's requested checks. |
| — | — | No `TBD`/`FIXME`/`XXX`/`TODO`/`HACK`/`PLACEHOLDER` markers found in any phase-modified file (checked again this pass) | — | Clean. |

## Human Verification Required

None. This remains a fully server-side, non-visual, deterministically-testable API surface. All must-haves and the specifically-requested re-checks (SC2, SC3 non-regression, WR-02 scope, fail-closed ordering) were resolved via direct code reading, the full automated test suite (unit + live-DB integration), and an independent live probe built for this verification pass that reproduces the exact end-to-end HTTP path (auth → requireOperator → handler → real repos → real Postgres) rather than relying on unit-test fakes or SUMMARY.md's self-reported evidence.

## Gaps Summary

None remaining. The single gap from the initial verification (SC2 / OPSV-02: idle unlimited clients silently dropped from the response, making them indistinguishable from nonexistent clients) is closed by plan 53-05's three-way key-set union, independently reproduced against a live Postgres instance during this re-verification rather than accepted on the SUMMARY's word.

The two additional read-layer/reporting-truth gaps the code review flagged (WR-01: phantom `uuid.Nil` client rows from `ON DELETE SET NULL` orphans; WR-03: API-process-clock vs. Postgres-clock day drift) are also closed by plans 53-06/53-07, both verified live during this pass.

WR-02 (`remaining_matches_admission_gate` overstating enforcement) is closed for the service-wide kill-switch (a new boolean field, wired from the same source variable as the actual kill-switch, immune to divergence) and closed for per-client observe-mode via the exact remedy the code review itself proposed — a note-level caveat plus the pre-existing per-row `enforcement_mode` field, not a second boolean. This was verified to be a deliberate, complete implementation of the review's own suggested fix, not a partial fix left half-done.

SC3 (non-operator no-leak 404) was specifically re-checked for regression risk introduced by the SC2 fix (since the fix makes real client existence more observable to the narrowed query) and confirmed unweakened: `requireOperator` structurally prevents the handler — and therefore the new merge logic and the new `UTCToday` call — from ever running for a non-operator, proven both by the existing test suite and by an independent live probe.

Remaining warning-level findings from the original code review (WR-05 unbounded pagination, WR-06 unrecognized engine strings, WR-08 unenforced non-nil contract) were outside the explicit scope of this re-verification request (which named CR-01, WR-01, WR-02, WR-03) and were not re-litigated; they are listed as informational anti-patterns above for completeness but do not block phase completion.

**Action item for the orchestrator:** `.planning/REQUIREMENTS.md`'s traceability table still lists `OPSV-02 | Phase 53 | Gap` (with an inline note pointing at the now-superseded initial verification). This should be updated to `Complete` now that the underlying gap is closed, per the requesting teammate's explicit instruction to flag this for restoration.

---

_Verified: 2026-08-05T23:30:00Z_
_Verifier: Claude (gsd-verifier)_
