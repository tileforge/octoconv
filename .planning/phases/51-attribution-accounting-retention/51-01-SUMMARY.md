---
phase: 51-attribution-accounting-retention
plan: 01
subsystem: database
tags: [postgres, migration, prometheus, metrics, cardinality, accounting]

# Dependency graph
requires: []
provides:
  - "client_usage_daily Postgres table (client_id, engine, day, status) PK, day index, set_updated_at trigger"
  - "octoconv_client_job_outcomes_total bounded per-client Prometheus counter"
  - "RecordClientJobOutcome(clientID, engine, status) and the clientLabel ceiling allocator"
affects: [51-02, 51-03, 51-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deliberate absence of a CHECK(engine ...) allow-list on an accounting table written inside the guarded terminal-transition transaction, so a stale allow-list can never abort a job's path to done"
    - "First-seen-first-served, never-evicted, in-process cardinality allocator for a Prometheus label (clientLabelAllocator), with an isolated test-local instance so ceiling tests are not order-dependent on shared package state"

key-files:
  created:
    - internal/db/migrations/0011_client_usage_daily.sql
    - internal/db/migrate_test.go
  modified:
    - internal/metrics/metrics.go
    - internal/metrics/metrics_test.go

key-decisions:
  - "PK is (client_id, engine, day, status), diverging from D-01's (client_id, engine, day) wording because D-04 stores status as a column rather than pre-filtering -- documented in the migration comment as an intentional divergence, not an oversight"
  - "No CHECK(engine ...) allow-list on client_usage_daily, unlike jobs.engine: the rollup INSERT (Plan 02) runs inside the guarded terminal-transition transaction, and a stale allow-list would abort that transaction instead of merely dropping accounting"
  - "clientLabelAllocator is a struct with a test-local constructor (newClientLabelAllocator) rather than bare package-level state with a reset helper, so the ceiling test is not order-dependent on ids admitted by sibling tests or concurrent production calls"
  - "METRICS_CLIENT_ID_CEILING read via a package-local env-int helper inside internal/metrics rather than threaded through cmd/*/main.go, keeping the blast radius to one package"

patterns-established:
  - "Package-local env-int config helper duplicated per consumer, matching the codebase's existing no-shared-config-package convention"

requirements-completed: [ACCT-01, ACCT-02, ACCT-04, RETN-01]

# Metrics
duration: 25min
completed: 2026-08-02
---

# Phase 51 Plan 01: Attribution surfaces (rollup table + bounded counter) Summary

**Additive, zero-behavior-change scaffolding: `client_usage_daily` Postgres rollup table plus a cardinality-bounded `octoconv_client_job_outcomes_total` Prometheus counter with a first-seen-first-served overflow allocator — no production code path reads or writes either surface yet.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-08-02T23:06:00+03:00 (approx.)
- **Completed:** 2026-08-02T23:11:36+03:00
- **Tasks:** 3 completed (Task 3 is TDD: RED + GREEN)
- **Files modified:** 4

## Accomplishments

- `client_usage_daily` exists with the exact `(client_id, engine, day, status)` PK the rollup UPSERT (Plan 02) and the purge (Plan 04) depend on, a `day` index for the purge, and the reused `set_updated_at` trigger — proven live against Postgres, not just applied.
- A live-Postgres schema-contract test (`TestMigrationsApplyClientUsageDaily`) pins the table shape via catalog introspection (`pg_constraint`/`information_schema`), verified to fail loudly on a wrong PK column set before being restored to the correct assertion.
- `octoconv_client_job_outcomes_total` exists with a proven numeric ceiling (`METRICS_CLIENT_ID_CEILING`, default 200) and a single stable `overflow` collapse for anything beyond it or malformed/empty/nil-UUID input; already-admitted ids are never evicted.
- `octoconv_job_duration_seconds` `{engine,status}` and `octoconv_queue_depth` `{queue,state}` label sets are unchanged and now pinned by regression tests (`TestJobDurationLabelsPinned`, `TestQueueDepthLabelsPinned`).

## Task Commits

Each task was committed atomically:

1. **Task 1: Create the client_usage_daily migration** - `25008db` (feat)
2. **Task 2: Pin the migrated schema with a live-Postgres test** - `3688011` (test)
3. **Task 3: Add the bounded per-client outcome counter** - `464223c` (test, RED) + `cb24f84` (feat, GREEN)

**Plan metadata:** committed separately by the orchestrator (STATE.md/ROADMAP.md not owned by this plan).

_Task 3 is a TDD task: `464223c` adds tests that fail to compile against the not-yet-existing `clientJobOutcomes`/`RecordClientJobOutcome`/`newClientLabelAllocator`/`overflowClientLabel`/`clientLabel` symbols (RED); `cb24f84` implements them and all tests pass (GREEN). No REFACTOR commit was needed — the GREEN implementation required no follow-up cleanup._

## Files Created/Modified

- `internal/db/migrations/0011_client_usage_daily.sql` - New `client_usage_daily` table (PK, day index, reused `set_updated_at` trigger); header comment records the D-01/D-04 PK-wording divergence, the deliberate absence of an `engine` allow-list, and the RETN-01 retention-is-enforced-by-Plan-04 note.
- `internal/db/migrate_test.go` - `TestMigrationsApplyClientUsageDaily`: live-catalog schema contract (table existence, exact PK column set, column types/nullability, day index, FK CASCADE delete rule, migration idempotency).
- `internal/metrics/metrics.go` - Added `clientJobOutcomes` CounterVec, `overflowClientLabel`, `ClientLabelCeiling`/`envClientLabelCeiling`, `clientLabelAllocator` (struct + `resolve`), `defaultClientAllocator`, `clientLabel`, `RecordClientJobOutcome`. `jobOutcomes`/`jobDuration`/`webhookDeliveries`/`reconcilerActions` declarations untouched.
- `internal/metrics/metrics_test.go` - `TestRecordClientJobOutcome`, `TestClientLabelCeilingCollapsesToOverflow` (test-local allocator instance), `TestClientLabelRejectsMalformedID`, `TestJobDurationLabelsPinned`, `TestQueueDepthLabelsPinned`.

## Decisions Made

- Kept the `status` column in the PK (`client_id, engine, day, status`) per D-04's design even though D-01's prose named a narrower key — recorded as an explicit reconciliation in the migration comment rather than silently changed.
- Made `clientLabelAllocator` a struct type with an exported test-local constructor instead of exposing package-level map/mutex vars directly, so `TestClientLabelCeilingCollapsesToOverflow` can prove ceiling behavior deterministically without resetting or depending on shared production state.
- `ClientLabelCeiling` is a package-level `var` (not `const`) resolved once at init from `METRICS_CLIENT_ID_CEILING`, matching the codebase's existing per-consumer env-int helper convention (no shared config package).

## Deviations from Plan

None - plan executed exactly as written. The threat register's two `mitigate` items (T-51-01 cardinality ceiling, T-51-02 no engine CHECK) are both implemented as specified, not as auto-fixes.

## Issues Encountered

None. `docker compose up -d postgres` reported a container-name conflict because `octoconv-db` was already running and healthy on port 5434 (per the environment note); used the already-running container directly instead of restarting it.

## Known Stubs

None. No UI or data-consuming code was touched by this plan — the table and counter exist but are deliberately not yet wired into any write/read path (that is Plans 02/03's job).

## Next Phase Readiness

- Plan 02 can now write the rollup inside `Repo.transition`'s `apply` closure against the proven `(client_id, engine, day, status)` conflict target.
- Plan 03 can call `metrics.RecordClientJobOutcome` from the worker/reconciler call sites; the ceiling and overflow behavior are already proven independently of any call site.
- Plan 04 can purge `client_usage_daily` using the `client_usage_daily_day_idx` index-driven `DELETE ... WHERE day < cutoff`.

---
*Phase: 51-attribution-accounting-retention*
*Completed: 2026-08-02*

## Self-Check: PASSED

- FOUND: internal/db/migrations/0011_client_usage_daily.sql
- FOUND: internal/db/migrate_test.go
- FOUND: internal/metrics/metrics.go
- FOUND: internal/metrics/metrics_test.go
- FOUND commit: 25008db (feat(51-01): add client_usage_daily rollup migration)
- FOUND commit: 3688011 (test(51-01): pin client_usage_daily schema with live-Postgres test)
- FOUND commit: 464223c (test(51-01): add failing tests for bounded per-client outcome counter)
- FOUND commit: cb24f84 (feat(51-01): implement bounded per-client outcome counter (GREEN))
