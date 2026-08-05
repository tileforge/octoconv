---
phase: 53-operator-usage-visibility
plan: 01
subsystem: database
tags: [postgres, pgx, quota, admission, usage-rollup]

# Dependency graph
requires:
  - phase: 51-consumer-attribution
    provides: client_usage_daily rollup table (migration 0011, widened by 0014 for canceled)
  - phase: 52-quota-enforcement-fairness
    provides: admission.go's evaluate() formula and client_limits schema (migration 0013) this plan mirrors
provides:
  - internal/jobs.UsageRow and (*Repo).UsageSummary — the read side of client_usage_daily plus the live in-flight (queued/active) term, for one client or all clients over an inclusive UTC day range
affects: [53-02, 53-03, 53-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Read-side query mirrors an authoritative write-side formula byte-for-byte via shared SQL shape (status-unfiltered terminal sum, literal in-flight status predicates), verified by a boundary cross-check test that feeds the read side's own number back into the write side and asserts the admit/reject flip happens at exactly that number"
    - "Source-level SQL-shape assertions (string.Contains on package-level SQL consts) as an always-on, DB-independent regression gate for SQL structure invariants that a DB round-trip test cannot cheaply pin"

key-files:
  created: [internal/jobs/usage_query.go, internal/jobs/usage_query_test.go]
  modified: []

key-decisions:
  - "UsageSummary takes NO advisory lock (unlike admitLocked) — a read-only visibility query tolerates a benign, bounded race with a concurrent admission; only the admission gate itself needs to be exact under concurrent replicas"
  - "TerminalBytesOut is reported for operator visibility only and is explicitly documented as NOT part of the daily-bytes budget arithmetic, since evaluate() never compares against bytes_out"
  - "In-flight job count and in-flight byte sum stay two separate grouped queries (not one JOINed query) so a job with N job_inputs rows contributes 1 to the count and N sizes to the byte sum — mirrors evaluate()'s own two-separate-subqueries structure"

patterns-established:
  - "Pattern: mirror-and-cross-check — when a read-side query must agree byte-for-byte with an authoritative write-side formula (D-05), pin the agreement with a test that computes the read side's own figure and feeds it back into the write side's limit, asserting the exact boundary flip, not just equal numbers computed independently"

requirements-completed: [OPSV-01, OPSV-02]

# Metrics
duration: ~35min
completed: 2026-08-05
---

# Phase 53 Plan 01: Usage Query Read-Side Summary

**`internal/jobs.UsageSummary` — per-(client, engine) terminal + in-flight usage over an inclusive UTC day range, using the exact SQL shape `evaluate()`'s admission gate uses, boundary-pinned against `evaluate()` itself.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-08-05T13:00:00Z (approx)
- **Completed:** 2026-08-05T13:27:00Z
- **Tasks:** 2
- **Files modified:** 2 (both new)

## Accomplishments
- `internal/jobs/usage_query.go`: `UsageRow` type (7 fields) and `(*Repo).UsageSummary(ctx, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]UsageRow, error)` — reads `client_usage_daily` (status-unfiltered, mirroring `evaluate()`'s post-0014 shape) plus the live `queued`/`active` in-flight term (literal-status predicates matching `jobs_client_engine_inflight_idx`), for one client or all clients through a single optional bind
- `internal/jobs/usage_query_test.go`: 3 DB-independent source-level SQL-shape tests (always-on regression gate) + 6 `DATABASE_URL`-gated integration tests, including two boundary cross-checks that feed `UsageSummary`'s own reported figure back into `max_daily_bytes`/`max_daily_jobs` and assert `evaluate()` (via `Repo.Create`) flips from admit to reject at exactly that number
- Manually verified (mutation check, reverted) that both the source-level and integration guard tests fail loudly if a `status IN ('done','failed')` filter is reintroduced into the terminal aggregate

## Task Commits

Each task was committed atomically:

1. **Task 1: Add UsageRow and (*Repo).UsageSummary with evaluate()-mirroring SQL** - `0e18b87` (feat)
2. **Task 2: Pin the SQL shape and cross-check UsageSummary against evaluate()** - `17042ad` (test)

**Plan metadata:** (this commit, following)

## Files Created/Modified
- `internal/jobs/usage_query.go` - `UsageRow` type + `(*Repo).UsageSummary`; three package-level SQL consts (`usageTerminalSQL`, `usageInFlightJobsSQL`, `usageInFlightBytesSQL`) mirroring `evaluate()`'s formula
- `internal/jobs/usage_query_test.go` - Group A (3 source-level SQL-shape tests, no DB) + Group B (6 `DATABASE_URL`-gated integration tests)

## Decisions Made
- No advisory lock on the read path — see `key-decisions` above; this matches 53-CONTEXT.md's stated Integration Points and T-53-01-05's accepted-risk disposition in the plan's threat model.
- `TerminalBytesOut` documented explicitly as visibility-only, not budget arithmetic, to prevent a future caller from assuming it participates in `max_daily_bytes`.
- Job-count and byte-sum kept as two separate grouped SQL statements for the in-flight term rather than one JOINed query, to avoid multiplying a job by its `job_inputs` row count — pinned by `TestUsageSummaryMultiInputJobCountedOnce` and the source-level `TestUsageInFlightJobCountDoesNotJoinInputs`.

## Deviations from Plan

None - plan executed exactly as written. One presentational adjustment during Task 1: reworded an inline comment on `usageInFlightJobsSQL` (which originally repeated the literal `status IN ('queued', 'active')` string a third time inside a comment) to avoid tripping the plan's own strict `grep -c` acceptance check (expects the literal to appear exactly twice, once per in-flight SQL statement). This is a documentation-only wording change with zero behavioral impact — not a Rule 1-4 deviation, just conforming exactly to the plan's acceptance criteria as written.

## Issues Encountered
- The project's shared `octoconv-db` Postgres container (used by multiple parallel worktree agents) was already running healthy on `localhost:5434`; `docker compose up -d postgres` in this worktree failed with a container-name conflict, so verification connected directly to the existing shared container instead of starting a worktree-local one. No schema or data conflict — `db.Migrate` is idempotent and this plan's tests only touch freshly-created test clients.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `UsageRow`/`UsageSummary` are ready to be wrapped by plan 53-03's `api.UsageRepo` interface and consumed by plan 53-04's operator-facing handler — the field names and method signature are frozen per this plan's `<interfaces>` contract and must not change without updating those downstream plans.
- No blockers. `go.mod`/`go.sum` unchanged — zero new dependencies, as the plan's threat model required.

---
*Phase: 53-operator-usage-visibility*
*Completed: 2026-08-05*

## Self-Check: PASSED

- FOUND: internal/jobs/usage_query.go
- FOUND: internal/jobs/usage_query_test.go
- FOUND: .planning/phases/53-operator-usage-visibility/53-01-SUMMARY.md
- FOUND commit: 0e18b87
- FOUND commit: 17042ad
