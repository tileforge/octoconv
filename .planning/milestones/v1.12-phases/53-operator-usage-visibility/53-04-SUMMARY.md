---
phase: 53-operator-usage-visibility
plan: 04
subsystem: api
tags: [go, chi, http, openapi, quota, operator]

# Dependency graph
requires:
  - phase: 53-01
    provides: internal/jobs.UsageRow and (*jobs.Repo).UsageSummary
  - phase: 53-02
    provides: internal/clients.LimitWithMeta and (*clients.Repo).ListAllLimits
  - phase: 53-03
    provides: api.UsageRepo/api.LimitsRepo interfaces, Server.usageRepo/limitsRepo, fakeUsageRepo/fakeLimitsRepo test doubles
provides:
  - "GET /v1/system/usage: operator-gated combined usage + limits + remaining-budget endpoint (OPSV-01/OPSV-02)"
  - "resolveUsagePeriod(period, now) — UTC-normalized inclusive day-range resolver for today/7d/30d"
  - "systemUsageResponse/systemUsageRow wire types documented in docs/openapi.yaml"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "remaining arithmetic mirrors an authoritative write-side formula (evaluate()) field-by-field, pinned by a test that seeds a deliberately-wrong decoy value (huge TerminalBytesOut) and asserts it does NOT leak into the computed figure"
    - "merge-over-union-of-keys: two independent repo reads (usage, limits) merged by (client_id, engine) key, emitting the union of both key sets rather than an inner join, so a client with only usage or only limits is never silently dropped"

key-files:
  created:
    - internal/api/system_usage_handlers.go
    - internal/api/system_usage_handlers_test.go
  modified:
    - internal/api/routes.go
    - docs/openapi.yaml

key-decisions:
  - "Test-server construction always builds the fakeResolver FIRST and derives the operator allowlist from resolver.client.ID, rather than injecting auth.WithClient context directly — full-router tests (srv.Routes().ServeHTTP) must pass through the real auth.Middleware, which always re-resolves the caller from the Authorization header and would overwrite any manually-injected context"
  - "Sort implemented with the stdlib sort.Slice (not a hand-rolled algorithm): the comparator is a total order (consumption desc, then ClientID string asc, then Engine asc), so sort.Slice's non-stable guarantee never matters"

patterns-established:
  - "Pattern: full-router operator-gate tests build the fakeResolver externally (not via a helper that hides it), seed OperatorClientIDs from resolver.client.ID, then drive srv.Routes().ServeHTTP with an authed() request — the only way to exercise requireOperator's real allow/deny path end-to-end since auth.Middleware always overwrites request context from the header"

requirements-completed: [OPSV-01, OPSV-02]

# Metrics
duration: ~50min
completed: 2026-08-05
---

# Phase 53 Plan 04: Operator Usage Visibility Endpoint Summary

**`GET /v1/system/usage` — operator-gated, single combined response merging per-(client, engine) usage from the Phase 51 rollup with Phase 52's configured limits and a `remaining` figure that mirrors `evaluate()`'s admission-gate arithmetic field-by-field, with a byte-identical no-leak 404 proven against a live `/v1/system/presets` request.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-08-05T16:53:00Z (approx, per session start)
- **Completed:** 2026-08-05T14:07:48Z
- **Tasks:** 3
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments
- `internal/api/system_usage_handlers.go`: `validUsagePeriods` fail-closed allow-set, `resolveUsagePeriod(period, now)` (UTC-normalized inclusive day range for `today`/`7d`/`30d`), pointer-typed response structs (`null`-vs-`0` fidelity), and `handleSystemUsage` merging `UsageSummary` + `ListAllLimits` over the union of their (client, engine) keys, computing `remaining` per dimension exactly as `evaluate()` does (concurrency in-flight-only, daily-bytes excluding `bytes_out`, daily-jobs terminal+in-flight)
- `internal/api/routes.go`: `GET /v1/system/usage` registered inside its own `requireOperator` sub-router, sibling of `/system/presets`
- `internal/api/system_usage_handlers_test.go`: 17 top-level test functions (several table-driven/subtest) proving ROADMAP SC3 (byte-identical 404, both against the derived `noSuchPreset` constant and a live `/v1/system/presets` response), fail-closed validation ordering (D-03/D-04), the D-09 multi-day caveat, the three `remaining` formulas including a deliberate `bytes_out` decoy (D-05), separated terminal/in-flight usage (D-06), unclamped negative remaining, `null`-vs-explicit-`0` fidelity (RESEARCH Pitfall 5), both halves of the union key set (OPSV-02), the consumption sort (D-02), `?client_id` narrowing reaching both repos (D-01), and the empty-array marshal
- `docs/openapi.yaml`: `GET /v1/system/usage` documented with `period`/`client_id` query params, the full envelope + row schema, and all five response codes (200/400/401/404/422) — the 404 documents the existing `"preset not found"` body rather than inventing a new literal
- Manually ran all four prescribed mutation checks (route hoisted out of the operator sub-router; `noSuchPreset` replaced with a new literal; `bytes_out` leaked into daily-bytes remaining; concurrency remaining also subtracting terminal jobs) and confirmed each fails the intended test(s); all reverted before their respective commits

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement handleSystemUsage, period resolution, and the merge + remaining arithmetic** - `290e8b5` (feat)
2. **Task 2: Prove SC3's byte-identical 404 and the fail-closed validation ordering** - `633378f` (test)
3. **Task 3: Pin the response semantics (D-02/D-05/D-06/D-07/D-09) and document the endpoint in OpenAPI** - `32ee90a` (test)

**Plan metadata:** (this commit, following)

## Files Created/Modified
- `internal/api/system_usage_handlers.go` - `validUsagePeriods`, `resolveUsagePeriod`, response types, `handleSystemUsage` (merge + remaining arithmetic + consumption sort)
- `internal/api/routes.go` - `GET /v1/system/usage` registered inside a `requireOperator` sub-router, sibling of `/system/presets`
- `internal/api/system_usage_handlers_test.go` - 17 test functions (some table-driven with subtests) covering the operator gate, validation ordering, and full response-semantics contract
- `docs/openapi.yaml` - `/v1/system/usage` path block with `get` operation, query params, envelope schema, and 200/400/401/404/422 responses

## Decisions Made
- Every full-router operator-gate test builds `newFakeResolver()` explicitly (via the local `newOperatorUsageTestServer` helper) and seeds `OperatorClientIDs` from `resolver.client.ID`, mirroring `system_presets_handlers_test.go`'s own `TestSystemPresets_OperatorSucceedsForAllVerbs` pattern — `auth.Middleware` always re-resolves the caller from the `Authorization` header when driving `srv.Routes().ServeHTTP`, so a manually-injected `auth.WithClient` context (the pattern `newOperatorGateTestServer` uses for isolated middleware tests) would be overwritten and silently produce a 401 instead of exercising the intended allow/deny path. The plan's literal `newUsageTestServer(operatorIDs, usage, limits) *Server` helper signature is retained for the non-operator cases (where the exact caller identity doesn't need to match the allowlist), with a second small helper (`newOperatorUsageTestServer`) added for the operator-required cases — a minor, necessary addition to make the plan's "drive the full router" instruction actually work; it introduces no new production behavior.
- `sortSystemUsageRows` uses `sort.Slice` from the stdlib rather than a hand-rolled sort, since the comparator (consumption desc → ClientID string asc → Engine asc) is already a total order, matching the project's existing `sort.Slice` usage in `internal/jobs/usage_query.go`.

## Deviations from Plan

None - plan executed as written. The only implementation detail beyond the plan's literal text is the additional `newOperatorUsageTestServer` test helper described above under Decisions Made, which exists solely to satisfy the plan's own instruction that tests "drive the FULL router via `srv.Routes().ServeHTTP`" — without it, operator-required tests (period/client_id validation, repo-error, and all Task 3 semantics tests) cannot pass real `auth.Middleware` resolution. This is not a Rule 1-4 deviation: it changes no production code and no test assertion, only how the fixture server is constructed.

## Issues Encountered
- Initial worktree HEAD was stale (pointed at a pre-wave-1 commit `3f94254`, six commits behind the expected wave-2 base `6ec9bb2`). Corrected per the `<worktree_branch_check>` protocol (`git reset --hard 6ec9bb2b4b3fc32d845e55acc3ed1dfe8a4bb41d`) before any file edits — no production code was affected, this was purely a worktree-state correction step required before Task 1 could begin (the plan file itself did not exist on disk until after the reset).
- Two of the plan's literal `<acceptance_criteria>` shell commands (`yq -e '.paths."/v1/system/usage".get.responses | has("200") and has("400") ...'`) fail due to a `yq`/`jq`-style operator-precedence quirk in the ungrouped `has(...) and has(...)` chain, not a spec defect — verified all five response codes (`200`/`400`/`401`/`404`/`422`) are present individually and with correctly parenthesized `and` groupings; both return `true`.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Phase 53 (Operator Usage Visibility) is now feature-complete: `GET /v1/system/usage` is live, tested, and documented. Requirements OPSV-01/OPSV-02 marked complete in `.planning/REQUIREMENTS.md`.
- `go.mod`/`go.sum` unchanged — zero new dependencies (the only external tool invoked, `@redocly/cli`, was run ad hoc via `npx` for linting only, per the plan's threat model T-53-04-SC).
- `DATABASE_URL=postgres://octo:octo-pass@localhost:5434/octo_db go test ./... -count=1` passes cleanly across the whole repo (verified against the shared `octoconv-db` container already running for this environment).
- Live compose smoke test (operator vs non-operator curl against a running stack) was not run in this worktree — no `octoconv-api` container was running and starting a worktree-local compose stack risks colliding with the shared Postgres container per prior-plan precedent (53-01-SUMMARY.md's own "Issues Encountered"). All other verification (build/vet/test/lint/mutation checks) is green; this is the one item deferred to a live-environment check if the orchestrator or a follow-up wants it.
- No blockers for phase completion / milestone audit.

---
*Phase: 53-operator-usage-visibility*
*Completed: 2026-08-05*

## Self-Check: PASSED

- FOUND: internal/api/system_usage_handlers.go
- FOUND: internal/api/system_usage_handlers_test.go
- FOUND: internal/api/routes.go
- FOUND: docs/openapi.yaml
- FOUND: .planning/phases/53-operator-usage-visibility/53-04-SUMMARY.md
- FOUND commit: 290e8b5
- FOUND commit: 633378f
- FOUND commit: 32ee90a
