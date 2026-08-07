---
phase: 53-operator-usage-visibility
plan: 06
subsystem: database
tags: [postgres, pgx, quota, usage-rollup, gap-closure]

# Dependency graph
requires:
  - phase: 53-operator-usage-visibility
    provides: "plan 53-01's UsageRow/UsageSummary read-side (this plan fixes two defects in that same file)"
provides:
  - "internal/jobs.UsageSummary never emits a phantom uuid.Nil client row for jobs orphaned by ON DELETE SET NULL"
  - "internal/jobs.(*Repo).UTCToday(ctx) — a Postgres-derived 'today' read, for plan 53-07's handler to resolve the same clock evaluate() uses"
affects: [53-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Server-clock read pattern: a dedicated Repo method (UTCToday) that returns a Postgres-computed value completely untouched by Go, specifically to prevent a caller from re-deriving 'today' from a different clock than the one the write path stamped rows with"

key-files:
  created: []
  modified: [internal/jobs/usage_query.go, internal/jobs/usage_query_test.go]

key-decisions:
  - "Both in-flight statements' new IS NOT NULL guard is placed AFTER the literal status predicate and BEFORE the ($1::uuid IS NULL OR ...) bind, preserving jobs_client_engine_inflight_idx's partial-index applicability (index predicate is on status only, verified unaffected)"
  - "UTCToday returns the Postgres value completely unmodified — no Go-side Truncate/UTC() call — so the method's only job is delegating the clock read, with zero chance of a Go-side re-derivation reintroducing the WR-03 drift it exists to close"
  - "utcToday test helper migrated from time.Now().UTC().Truncate(24*time.Hour) to r.UTCToday(ctx), fixing the WR-07 UTC-midnight test flake at its root rather than tolerating it"

patterns-established:
  - "Pattern: revert-and-observe deviation proof — before committing a defect fix pinned by a new test, temporarily strip the fix, run the new test, capture the exact failure output, then restore from a pre-edit backup (never git checkout/reset) and re-run green; the captured failure output goes in the SUMMARY as evidence the test is not vacuous"

requirements-completed: [OPSV-01, OPSV-02]

# Metrics
duration: ~40min
completed: 2026-08-05
---

# Phase 53 Plan 06: Usage Query Read-Layer Gap Closure Summary

**Guarded both in-flight usage aggregates against ON DELETE SET NULL orphan jobs (WR-01) and added `Repo.UTCToday` so the operator endpoint can share Postgres's clock with `evaluate()` (WR-03), closing the last two read-layer trust gaps from PR #6 review.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-08-05T22:30:00Z (approx)
- **Completed:** 2026-08-05T23:10:00Z (approx)
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- `usageInFlightJobsSQL` and `usageInFlightBytesSQL` now carry a `client_id IS NOT NULL` / `j.client_id IS NOT NULL` guard, placed after the literal-status predicate and before the optional-clientID bind — a deleted client's still-queued/active jobs no longer collapse into a phantom `client_id: uuid.Nil` row with `enforcement_mode: ""` in the operator report
- `(*Repo).UTCToday(ctx) (time.Time, error)` added: reads `SELECT (now() AT TIME ZONE 'utc')::date` verbatim, error-wrapped as `"read utc today: %w"`, so plan 53-07's handler can resolve "today" from the same clock `evaluate()` uses instead of the API host's wall clock
- All 7 `utcToday()` call sites in `usage_query_test.go` migrated to `utcToday(t, r)`, reading Postgres directly — removes the WR-07 UTC-midnight test-flake source at its root
- Fixed a latent CodeRabbit-flagged flake: `TestUsageSummaryMirrorsEvaluateDailyBytesBoundary`'s `int64Ptr(want-1)` write is now guarded by a `want <= 0` positivity check, so a zero-byte fixture fails with a diagnostic instead of violating migration 0013's `CHECK (max_daily_bytes IS NULL OR max_daily_bytes >= 0)`
- Manually verified (temporary revert against a file backup, then restored — never via git checkout/reset) that stripping the two `IS NOT NULL` guards reproduces the exact predicted phantom row and fails the new integration test

## Task Commits

Each task was committed atomically:

1. **Task 1: Exclude orphaned NULL-client jobs from both in-flight aggregates** - `143996a` (fix)
2. **Task 2: Add Repo.UTCToday and make the DB-gated day helper read the Postgres clock** - `b5939f4` (feat)

**Plan metadata:** (this commit, following)

## Files Created/Modified
- `internal/jobs/usage_query.go` - `client_id IS NOT NULL` guards on both in-flight SQL consts; new `(*Repo).UTCToday` method; package doc comment updated to point callers at `UTCToday`
- `internal/jobs/usage_query_test.go` - `TestUsageInFlightSQLExcludesNullClients` (source-level shape guard), `TestUsageSummaryExcludesOrphanedNullClientJobs` (DB-gated orphan integration test), `TestUTCTodayMatchesPostgresDay`, `utcToday` helper migrated to `(t, r)` signature reading `r.UTCToday`, positivity guard before `int64Ptr(want-1)`

## Decisions Made
- Placed the new `IS NOT NULL` predicates immediately after the literal `status IN (...)` term and before the `$1::uuid IS NULL OR ...` bind in both statements — confirmed via the acceptance-criteria greps that the literal-status form (`jobs_client_engine_inflight_idx` match) is undisturbed, and via the doc comment update that the index's predicate is on `status` only so the new term cannot affect its applicability.
- `UTCToday` deliberately does zero Go-side time manipulation on the scanned value — this is the whole point of the method (WR-03's fix is "read the DB's own value", not "read the DB's value and then process it in Go").
- Left `UTCToday` out of any interface (`api.UsageRepo` widening is plan 53-07's Task 1, per the plan's explicit instruction) — `internal/api` is untouched by this plan, confirmed by `git diff --stat internal/api/` (empty) after every commit.

## Deviations from Plan

**Minor plan-wording discrepancy (informational only, no code impact):**
Task 1's acceptance criterion states `go test ./internal/jobs/ -run 'TestUsageInFlightSQL' -count=1 -v` should report 3 `--- PASS` ("the two pre-existing shape tests plus the new one"). In the actual pre-existing test file there was only ONE test whose name contains the substring `TestUsageInFlightSQL` (`TestUsageInFlightSQLUsesLiteralStatuses`) — the other in-flight-related test, `TestUsageInFlightJobCountDoesNotJoinInputs`, does not match that substring pattern. With the new `TestUsageInFlightSQLExcludesNullClients` added, the `-run 'TestUsageInFlightSQL'` filter reports exactly 2 `--- PASS`, not 3. This is not a defect in the implementation — both tests exist, both pass, the SQL is correctly guarded. It is a miscount in the plan's own acceptance-criteria text, not a Rule 1-4 deviation requiring a fix, since the underlying behavior (2 shape guards, both DB-independent, both passing) is exactly what the plan's `<behavior>` section asked for. Documented here rather than silently "fixed" by renaming a test to force a 3rd substring match, which would have been cosmetic churn with no correctness value.

None - all functional code changes executed exactly as written.

## Issues Encountered
- `internal/storage`'s `TestRoundTrip` fails under `go test ./...` due to the shared local MinIO container's host volume being at 100% disk usage (`docker exec octoconv-minio df -h /data` → 49G used / 256M avail) — a pre-existing environment condition, not caused by this plan. `internal/storage/` was not touched by either commit in this plan (`git diff --stat internal/storage/` is empty across both task commits). Logged to `.planning/phases/53-operator-usage-visibility/deferred-items.md` per the executor's scope-boundary rule rather than fixed, since fixing host disk space is infra housekeeping outside this plan's `files_modified`.
- This worktree did not have a `.env` file (it is git-ignored and not checked out per-worktree); copied it from the main repo checkout (`/Users/apaderin/dev/octoconv/.env`) to run the DB-gated tests against the already-running shared `octoconv-db` container on `localhost:5434`. The copy is itself git-ignored and was not committed.

## Recorded Evidence (per plan's `<output>` instructions)

**Final text of both guarded in-flight statements** (`internal/jobs/usage_query.go`):
```sql
-- usageInFlightJobsSQL
SELECT client_id, engine, count(*)
FROM jobs
WHERE status IN ('queued', 'active')
    AND client_id IS NOT NULL -- jobs.client_id is nullable with ON DELETE SET NULL (0001_init.sql:43); ...
    AND ($1::uuid IS NULL OR client_id = $1)
GROUP BY client_id, engine

-- usageInFlightBytesSQL
SELECT j.client_id, j.engine, COALESCE(sum(ji.size_bytes), 0)
FROM jobs j JOIN job_inputs ji ON ji.job_id = j.id
WHERE j.status IN ('queued', 'active')
    AND j.client_id IS NOT NULL -- jobs.client_id is nullable with ON DELETE SET NULL (0001_init.sql:43); ...
    AND ($1::uuid IS NULL OR j.client_id = $1)
GROUP BY j.client_id, j.engine
```

**Observed failure output with the `IS NOT NULL` predicates temporarily reverted** (`TestUsageSummaryExcludesOrphanedNullClientJobs`, then restored from a pre-edit backup, never via `git checkout`/`reset`):
```
=== RUN   TestUsageSummaryExcludesOrphanedNullClientJobs
    usage_query_test.go:352: UsageSummary(nil) returned a phantom row with ClientID == uuid.Nil
    (engine=image, InFlightJobCount=1) — an orphaned NULL-client job must be excluded, not
    attributed to a fabricated client identity (WR-01): {ClientID:00000000-0000-0000-0000-
    000000000000 Engine:image TerminalJobCount:0 TerminalBytesIn:0 TerminalBytesOut:0
    InFlightJobCount:1 InFlightBytesIn:111}
--- FAIL: TestUsageSummaryExcludesOrphanedNullClientJobs (0.09s)
FAIL
```

**`UTCToday`'s returned value alongside the direct SELECT it was compared against** (`TestUTCTodayMatchesPostgresDay`, run 2026-08-05):
- `r.UTCToday(ctx)` and a direct `r.pool.QueryRow(ctx, "SELECT (now() AT TIME ZONE 'utc')::date")` both returned `2026-08-05 00:00:00 +0000 UTC` (verified equal via `time.Time.Equal`, and independently confirmed via `docker exec octoconv-db psql -U octo -d octo_db -tAc "SELECT (now() AT TIME ZONE 'utc')::date"` → `2026-08-05`); `Hour()`, `Minute()`, `Second()`, `Nanosecond()` all 0, `Location() == time.UTC`.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `jobs.Repo.UTCToday` is ready for plan 53-07 to wire into `api.UsageRepo`'s interface and fakes (explicitly deferred to that plan, per this plan's instructions).
- Both `usageInFlightJobsSQL`/`usageInFlightBytesSQL` guards are in place; plan 53-07's handler can rely on `UsageSummary` never emitting a `uuid.Nil` client row.
- `go build ./... && go vet ./... && gofmt -l .` all clean; `go test ./internal/jobs/ -count=1` reports `ok` with 0 SKIP, 0 FAIL across the full package (100+ subtests).
- `internal/api` is byte-for-byte untouched by this plan (`git diff --stat internal/api/` empty after both commits) — no contention with plan 53-05's sibling wave-4 worktree.
- One unrelated, pre-existing environment issue logged to `deferred-items.md`: `internal/storage`'s `TestRoundTrip` fails due to host disk space on the shared MinIO container, outside this plan's scope.

---
*Phase: 53-operator-usage-visibility*
*Completed: 2026-08-05*

## Self-Check: PASSED

- FOUND: internal/jobs/usage_query.go
- FOUND: internal/jobs/usage_query_test.go
- FOUND: .planning/phases/53-operator-usage-visibility/53-06-SUMMARY.md
- FOUND commit: 143996a
- FOUND commit: b5939f4
