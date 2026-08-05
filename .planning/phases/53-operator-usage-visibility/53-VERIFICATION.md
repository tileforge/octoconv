---
phase: 53-operator-usage-visibility
verified: 2026-08-05T17:30:00Z
status: gaps_found
score: 9/10 must-haves verified
overrides_applied: 0
gaps:
  - truth: "Per client, the operator can see the currently configured limits and the remaining budget/quota state (ROADMAP SC2 / OPSV-02)"
    status: failed
    reason: "handleSystemUsage's merge step (internal/api/system_usage_handlers.go:193-204) builds the emitted row key set as `usageByKey ∪ limitsByKey`, where `limitsByKey` only indexes LimitWithMeta rows whose Engine is non-nil. A client with NO client_limits row at all (Engine == nil — the documented, load-bearing signal for 'this client has no limits configured', and per ROADMAP SC1 the DEFAULT state of every freshly-provisioned client in the fleet) and no usage in the queried period contributes to neither map, so it produces ZERO output rows. Confirmed empirically with a live probe test: GET /v1/system/usage?client_id=<a real, idle, unlimited client> returns `{\"usage\":[]}` — byte-identical to the response for a client_id that matches no client at all. An operator cannot tell 'this client exists and is unlimited' from 'no such client', which is exactly the question SC2/OPSV-02 poses. This matches code review finding CR-01 (53-REVIEW.md), independently reproduced against the actual handler rather than accepted on the review's word."
    artifacts:
      - path: "internal/api/system_usage_handlers.go"
        issue: "keySet (lines 193-204) unions only usageByKey and limitsByKey; limitsByKey (line 180-186) is populated only from LimitWithMeta rows with non-nil Engine, so nil-Engine metadata-only rows never enter the emitted key set and the client is silently omitted from the response."
    missing:
      - "Seed the emitted key set from every client present in `meta` (i.e. every ClientID ListAllLimits returns, including nil-Engine rows) as well as usage/limits keys, emitting a synthetic all-unlimited / no-activity row for any client that produced neither a usage key nor a configured-limit key."
      - "Add a test asserting `?client_id=<a real, idle, unlimited client>` returns exactly one row (not an empty array) carrying that client's client_name/enforcement_mode with all-null limits, and a companion test asserting `?client_id=<a UUID matching no client>` still returns `{\"usage\":[]}`, so the two cases become provably distinguishable."
      - "Re-run TestSystemUsage_* suite plus a regression test for the previously-untested 'zero usage AND zero limits' cell of the merge truth table (system_usage_handlers_test.go currently only covers 'usage but no limits' and 'limits but no usage', never 'neither')."
---

# Phase 53: Operator Usage Visibility — Verification Report

**Phase Goal:** An operator can answer "who consumed what, by engine class, over what period" and "what are this client's limits and remaining budget" through a dedicated, already-established access-control pattern — not by hand-querying raw Prometheus or the `jobs` table.

**Verified:** 2026-08-05T17:30:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1 — Operator retrieves per-client, per-engine usage for a period via a dedicated `OPERATOR_CLIENT_IDS`-gated endpoint, backed by the Phase 51 rollup (`client_usage_daily`), not a raw `jobs` scan or Prometheus | ✓ VERIFIED | `internal/jobs/usage_query.go:72-80` (`usageTerminalSQL`) reads `client_usage_daily` with no status filter (mirrors `evaluate()`, admission.go:287-292); the in-flight term reads `jobs`/`job_inputs` live status by design (D-06), not as a substitute for the rollup. Route `GET /v1/system/usage` registered inside `requireOperator` (`routes.go:77-80`). `go build`/`go vet`/`go test ./...` all pass; DB-gated `TestUsageSummary*` integration tests pass live against Postgres (6/6 PASS). |
| 2 | SC2 — Per client, operator can see currently configured limits and remaining budget/quota state, so limits can be set from observed data | ✗ **FAILED** | See Gap #1 below. `handleSystemUsage`'s merge (system_usage_handlers.go:193-204) drops any client whose `LimitWithMeta.Engine == nil` (no limits row) AND has no usage in the window — the documented default state of the whole fleet (ROADMAP SC1). Empirically reproduced with a live probe: `?client_id=<idle, unlimited, real client>` → `{"usage":[]}`, indistinguishable from a nonexistent client_id. |
| 3 | SC3 — Non-operator caller gets a byte-identical no-leak 404 (never 403), no new information-disclosure surface | ✓ VERIFIED | Route sits inside `r.Route("/system/usage", func(r chi.Router){ r.Use(s.requireOperator); ... })` (routes.go:77-80), reusing `requireOperator` verbatim and the `noSuchPreset` constant — no new literal (`grep -c "usage not found" internal/api/` = 0). `TestSystemUsage_NonOperatorNoLeak404` and `TestSystemUsage_NonOperator404MatchesPresets404Exactly` both PASS, the latter comparing status/Content-Type/body bytes against a *live* `/v1/system/presets` 404 response, not a hardcoded literal. Zero repo calls on denial (`usage.calls==0 && limits.calls==0`), asserted and passing. |
| 4 | D-01 — Same handler/response shape for "all clients" and "?client_id=one", one optional bind, not two code paths | ✓ VERIFIED | `usage_query.go` and `limits.go` both use `($n::uuid IS NULL OR ... = $n)` single-predicate binds. `TestSystemUsage_ClientIDNarrowingPassedToBothRepos` and `TestUsageSummaryAllClientsAndNarrowedAgree`/`TestListAllLimitsNarrowingMatchesUnnarrowed` PASS. |
| 5 | D-02 — All-clients list sorted by consumption descending | ✓ VERIFIED | `sortSystemUsageRows` (system_usage_handlers.go:312-324); `TestSystemUsage_SortedByConsumptionDescending` PASSES. |
| 6 | D-03/D-04 — `?period` fail-closed allow-set (today/7d/30d), 422 before any repo call, default `today` | ✓ VERIFIED | `validUsagePeriods` map + early-return `writeError` before either repo call (system_usage_handlers.go:141-156). `TestSystemUsage_InvalidPeriodRejectedBeforeAnyRepoCall` table (5 invalid + 1 valid-empty case) PASSES, asserting `calls==0` on rejection. |
| 7 | D-05 — `remaining` arithmetic mirrors `evaluate()`'s per-dimension formula (concurrency = in-flight only; daily-bytes excludes `bytes_out`; daily-jobs = terminal+in-flight) | ✓ VERIFIED | system_usage_handlers.go:243-260, matches `evaluate()` (admission.go:236-364). `TestSystemUsage_RemainingMirrorsEvaluateFormula` (with a deliberately huge `TerminalBytesOut` decoy) PASSES. *Caveat (see Anti-Patterns/Warnings): the response's `remaining_matches_admission_gate` flag is unconditionally `true` for `period=today` even when the row's own `enforcement_mode` is `observe` or the service-wide kill-switch is active — WR-02, not part of this must-have's literal wording but a real trust gap.* |
| 8 | D-06 — `used_terminal` and `used_in_flight` reported as separate fields | ✓ VERIFIED | `TestSystemUsage_UsedTerminalAndInFlightReportedSeparately` PASSES. |
| 9 | D-09 — Multi-day `remaining` carries an explicit hypothetical-caveat; `today` affirms it is live | ✓ VERIFIED | `remaining_matches_admission_gate`/`remaining_semantics`/`remaining_note` fields (system_usage_handlers.go:285-294). `TestSystemUsage_MultiDayCarriesCaveat` (7d and 30d) PASSES. |
| 10 | Unlimited dimension marshals JSON `null`, never `0`; explicit `max_concurrent=0` stays `0` | ✓ VERIFIED | Every limit/remaining field is a pointer, no `omitempty` (system_usage_handlers.go:79-93). `TestSystemUsage_UnlimitedMarshalsNullAndExplicitZeroMarshalsZero` and `TestListAllLimitsPreservesNullVersusExplicitZero` both PASS. |

**Score:** 9/10 truths verified (SC2 FAILED)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/jobs/usage_query.go` | `UsageRow` + `(*Repo).UsageSummary` mirroring `evaluate()` | ✓ VERIFIED | Exists, 232 lines, status-unfiltered terminal SQL, literal in-flight statuses, two separate in-flight queries. `go vet` clean, gofmt clean. |
| `internal/jobs/usage_query_test.go` | SQL-shape + DB-gated cross-check tests | ✓ VERIFIED | Source-level gate + 6 DB-gated integration tests, all PASS live. |
| `internal/clients/limits.go` | `LimitWithMeta` + `(*Repo).ListAllLimits` | ✓ VERIFIED | LEFT JOIN `clients → client_limits`, one optional bind, pointer fidelity preserved. 5 DB-gated tests PASS. |
| `internal/clients/limits_test.go` | Proof of LEFT JOIN semantics, narrowing, null/zero fidelity | ✓ VERIFIED | All 5 tests PASS live. |
| `internal/api/api.go` | `UsageRepo`/`LimitsRepo` interfaces, widened `NewServer` | ✓ VERIFIED | Both interfaces present, `Server` holds both fields, all 3 call sites updated and compile. |
| `cmd/api/main.go` | Wires `jobsRepo`/`clientRepo` into the two new slots | ✓ VERIFIED | `api.NewServer(jobsRepo, ..., jobsRepo, clientRepo, ...)` (main.go:175); no new repo constructed, no new env var. |
| `internal/api/system_usage_handlers.go` | `handleSystemUsage`, period resolution, merge + remaining arithmetic | ⚠️ **WIRED BUT INCOMPLETE** | Exists (325 lines), compiles, is exercised end-to-end by 24 passing tests — but the merge step has the CR-01 gap (Truth #2 above): correctly wired to real data, but silently drops a real category of rows. |
| `internal/api/routes.go` | `GET /v1/system/usage` inside `requireOperator` sub-router | ✓ VERIFIED | routes.go:77-80. |
| `internal/api/system_usage_handlers_test.go` | SC3 byte-identity + fail-closed + semantics tests | ✓ VERIFIED | 18 tests in this file, all PASS; no test covers the "zero usage AND zero limits" merge cell (the CR-01 gap), confirming the review's untested-path claim. |
| `docs/openapi.yaml` | `GET /v1/system/usage` documented, all response codes, byte-identical 404 example | ✓ VERIFIED | Path present, `operationId: getSystemUsage`, response codes 200/400/401/404/422/429 all present, `404` example body is `preset not found` (matches the byte-identical claim). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `internal/api/routes.go` | `s.requireOperator` | `r.Use(s.requireOperator)` inside `/system/usage` sub-router | ✓ WIRED | Confirmed by direct code read and by live-router tests (not just middleware-in-isolation). |
| `internal/api/system_usage_handlers.go` | `s.usageRepo.UsageSummary` | resolved day range + optional client UUID | ✓ WIRED | Called at line 163; confirmed data-flowing via live DB integration tests on the underlying repo and via handler tests asserting `calls`/captured args. |
| `internal/api/system_usage_handlers.go` | `s.limitsRepo.ListAllLimits` | same optional client UUID | ✓ WIRED | Called at line 169; same evidence as above. |
| `internal/api/system_usage_handlers_test.go` | `noSuchPreset` | byte-identity comparison | ✓ WIRED | `writeError(wantRec, 404, noSuchPreset)` compared via `bytes.Equal`; test passes. |
| `internal/api/system_usage_handlers.go` merge | `clients.LimitWithMeta` nil-Engine rows | union key set (intended, per interface doc) | ✗ **NOT WIRED** | `LimitWithMeta`'s doc comment (limits.go:116-119) explicitly states nil-Engine rows "MUST NOT be discarded by the caller" — the handler discards them from the key set anyway (only uses them for `meta`, never for `keySet`). This is the CR-01 root cause. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `handleSystemUsage` | `usageRows` | `s.usageRepo.UsageSummary` → `jobs.Repo.UsageSummary` → live Postgres `client_usage_daily`/`jobs`/`job_inputs` | Yes (proven live) | ✓ FLOWING |
| `handleSystemUsage` | `limitRows` | `s.limitsRepo.ListAllLimits` → `clients.Repo.ListAllLimits` → live Postgres `clients LEFT JOIN client_limits` | Yes (proven live) | ✓ FLOWING |
| `handleSystemUsage` | `rows` (final emitted set) | Union of the above two maps' keys | Partially — real DB data flows through, but the union logic silently excludes a real, observable data category (idle unlimited clients) | ⚠️ HOLLOW for that subset — see Gap #1 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full package build/vet/fmt clean | `go build ./... && go vet ./... && gofmt -l .` | exit 0, no output | ✓ PASS |
| Unit test suite for the endpoint | `go test ./internal/api/ -run TestSystemUsage -count=1 -v` | 24/24 `--- PASS`, 0 SKIP | ✓ PASS |
| DB-gated `UsageSummary` integration proof vs `evaluate()` | `DATABASE_URL=... go test ./internal/jobs/ -run TestUsageSummary -count=1 -v` | 6/6 `--- PASS` | ✓ PASS |
| DB-gated `ListAllLimits` integration proof | `DATABASE_URL=... go test ./internal/clients/ -run TestListAllLimits -count=1 -v` | 5/5 `--- PASS` | ✓ PASS |
| Full repo test suite (all packages, DB-gated) | `DATABASE_URL=... go test ./... -count=1` | all packages `ok` | ✓ PASS |
| **Independent probe: idle unlimited client visibility (CR-01)** | Custom Go test hitting the live handler with `usage.rows=[]`, `limits.rows=[{Engine:nil,...}]`, `?client_id=<that client>` | `{"usage":[]}` returned — same shape as a nonexistent client | ✗ **FAIL** — confirms CR-01/Gap #1 |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|-----------------|--------------|--------|----------|
| OPSV-01 | 53-01, 53-03, 53-04 | Operator retrieves "who consumed what, by engine class, over period" via `OPERATOR_CLIENT_IDS`-gated endpoint | ✓ SATISFIED | SC1 verified above; endpoint live, tested, DB-backed. |
| OPSV-02 | 53-02, 53-03, 53-04 | Per-client configured limits + remaining budget visible, so limits can be set from data | ✗ **BLOCKED** | SC2 FAILED — see Gap #1. The read-side repo (`ListAllLimits`) correctly preserves the nil-Engine signal; the HTTP-layer merge in `handleSystemUsage` discards it before it reaches the operator. |

No orphaned requirements: REQUIREMENTS.md maps only OPSV-01/OPSV-02 to Phase 53, and both are declared in plan frontmatter (53-01/53-04 for OPSV-01; 53-02/53-03/53-04 for OPSV-02).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/api/system_usage_handlers.go` | 193-204 | Union key set omits nil-Engine metadata-only rows | 🛑 Blocker | Drives Gap #1 / SC2 failure — the default-state fleet majority is invisible. |
| `internal/api/system_usage_handlers.go` | 285 | `remaining_matches_admission_gate` set solely from `period == "today"`, ignoring per-client `observe` mode and the `QUOTA_ENFORCEMENT_DISABLED` kill-switch | ⚠️ Warning | Not one of the phase's literal must-haves, but directly undermines the D-05 "trustworthy reporting" intent this endpoint exists to satisfy (review WR-02). Recommend a follow-up plan/gap-closure item. |
| `internal/api/system_usage_handlers.go` | 161 | `time.Now()` (API host clock) vs. `evaluate()`'s Postgres `now()` for "today" | ⚠️ Warning | Possible day-boundary drift between reported and enforced figures around UTC midnight or NTP skew (review WR-03). |
| `internal/jobs/usage_query.go` | 95-119 | In-flight queries `GROUP BY client_id` with no `client_id IS NOT NULL` guard against `ON DELETE SET NULL` orphaned jobs | ⚠️ Warning | Could surface a phantom nil-UUID "client" row on the unnarrowed shape (review WR-01); not exercised by any existing test. |
| `internal/api/system_usage_handlers.go` | 163-173 | Unbounded all-clients response, no pagination/ceiling | ℹ️ Info | Accepted risk per D-02 (small internal client population); documented in the plan's threat model as `accept`. |
| — | — | No `TBD`/`FIXME`/`XXX` markers found in any phase-modified file | — | Clean. |

## Human Verification Required

None. This is a fully server-side, non-visual, deterministically-testable API surface; all must-haves were resolvable via direct code reading, automated tests (unit + live-DB integration), and an independent live-handler probe reproducing the reviewer's claim.

## Gaps Summary

The phase delivers a real, working, correctly-gated, correctly-sourced (Phase 51 rollup, not a raw `jobs` scan or Prometheus) endpoint that satisfies SC1 and SC3 convincingly — both are backed by passing tests that exercise the live router and a live Postgres instance, not just unit-level mocks.

SC2 fails for a specific, well-defined, and previously-flagged (code review CR-01) reason: `handleSystemUsage`'s merge step unions `usageByKey` and `limitsByKey`, but `limitsByKey` is deliberately populated only from `LimitWithMeta` rows with a non-nil `Engine` — exactly the rows that represent "this client HAS a configured limit". The nil-Engine rows that represent "this client exists, has an enforcement mode, but no limits at all" are read correctly by `internal/clients.ListAllLimits` (its own doc comment and its own test suite prove the LEFT JOIN preserves them) but are then discarded one layer up, in the HTTP handler's merge, before they ever reach the operator. This was independently reproduced with a live probe against the actual `handleSystemUsage` code path (not merely re-stated from the review): `GET /v1/system/usage?client_id=<a real, idle, unlimited client>` returns `{"usage":[]}`, byte-for-byte identical to the response for a UUID that matches no client at all.

Because ROADMAP SC1 establishes that "no `client_limits` row + `enforcement_mode='observe'`" is the default state of every freshly-provisioned client, this is not an edge case — it is the majority case in a fresh or lightly-configured deployment, and it is precisely the "what are this client's limits" question OPSV-02 was written to answer. The fix is scoped and known (seed the key set from every client in `meta`, not just clients with a usage or limits key) and was already specified in the code review's fix suggestion; it was not applied before this phase was submitted for verification.

This is a single, well-isolated gap concentrated entirely in the merge step of one file. Recommend routing back through `/gsd:plan-phase --gaps` for a focused closure plan rather than reopening 53-01/53-02/53-03, which verified clean.

---

_Verified: 2026-08-05T17:30:00Z_
_Verifier: Claude (gsd-verifier)_
