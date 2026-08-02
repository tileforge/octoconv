---
phase: 51-attribution-accounting-retention
plan: 04
subsystem: database
tags: [postgres, reconciler, retention, prometheus, helm, docker-compose]

# Dependency graph
requires:
  - phase: 51-02
    provides: "client_usage_daily table (0011_client_usage_daily.sql), day-indexed for the purge target"
  - phase: 51-03
    provides: "client_usage_daily populated by every real terminal transition across all 7 engines and the reconciler's exhaustion path -- non-empty, complete data for this plan to age and delete against"
provides:
  - "internal/jobs.Repo.PurgeJobs/PurgeUsage: batched, bounded, Go-computed-cutoff DELETE queries with a hard olderThan<=0/batch<=0 disables-purging guard"
  - "Sweeper.purge(ctx), called from Run/RunWithLock immediately after sweep() (inside the same advisory-lock gate), bounded by maxPurgeBatchesPerSweep per kind, reporting through metrics.RecordReconcilerPurge"
  - "JOBS_RETENTION/USAGE_RETENTION/RETENTION_BATCH_SIZE and METRICS_CLIENT_ID_CEILING documented and propagated byte-identically across cmd/webhook-worker/main.go, .env.example, docker-compose.yml and the Helm ConfigMap"
affects: [52]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Retention purge hosted on an existing reconciler tick (no new deploy unit): purge() is called from Run/RunWithLock AFTER sweep(), never from inside sweep() itself -- sweep's early returns on a finder error would otherwise silently disable retention. RunWithLock's call site stays inside the TryAcquire advisory-lock gate, so exactly one fleet-wide replica ever purges."
    - "Cutoffs are always computed in Go and bound as parameters, never SQL-side arithmetic (now() - $N) -- binding a bare Go time.Duration into `now() - $2` leaves Postgres to infer $2's type from context, which resolves the whole expression to `interval` and fails against a timestamptz column. Compute the target instant/date in Go and bind it directly instead."
    - "olderThan<=0 (or batch<=0) returns (0, nil) WITHOUT executing a DELETE, at BOTH the jobs.PurgeJobs/PurgeUsage layer and implicitly at the reconciler.Config zero-value layer (every pre-existing Config literal in the codebase omits the three new fields) -- an unset retention window must mean 'off', never 'delete everything older than now'."

key-files:
  created:
    - internal/jobs/retention.go
    - internal/jobs/retention_test.go
    - internal/reconciler/retention_test.go
  modified:
    - internal/reconciler/reconciler.go
    - internal/reconciler/reconciler_test.go
    - internal/metrics/metrics.go
    - cmd/webhook-worker/main.go
    - .env.example
    - docker-compose.yml
    - deploy/chart/octoconv/templates/configmap.yaml

key-decisions:
  - "PurgeJobs/PurgeUsage operate table-wide, not scoped by client_id -- matching the plan's SQL shape exactly (DELETE ... WHERE id IN (SELECT id ... LIMIT $2)). This is a deliberate, load-bearing property (a single purge pass covers every client in one bounded pass) but it means retention_test.go's own aged/undeleted fixtures (the disabled-window and batch-limit tests) would otherwise leak into and corrupt later, independent `go test` invocations' exact rows-affected assertions -- both test files register `t.Cleanup` deletes for every row they create/insert, unconditionally, regardless of whether the purge under test actually removed it."
  - "Backdating created_at in tests binds a Go-computed target timestamp (`time.Now().Add(-age)`) directly, never `now() - $2` in raw SQL -- discovered live: binding a bare time.Duration into that SQL expression makes Postgres infer $2 as timestamptz (not interval), resolving the whole expression to `interval` and failing against the timestamptz column with a real Postgres error, not a test assumption."
  - "PurgeUsage's cutoff is computed with time.Now().UTC(), not local time -- client_usage_daily.day is always written as `(now() AT TIME ZONE 'utc')::date` (recordUsage, Plan 02), so a non-UTC cutoff would drift the purge/write day boundary by the host's local offset."
  - "RecordReconcilerPurge reuses the existing reconcilerActions CounterVec (adding purged_jobs/purged_usage to its Help text) rather than introducing a new metric -- RETN-01 only requires the purge to report what it deleted, and this vec is already the reconciler's reporting surface."
  - "METRICS_CLIENT_ID_CEILING is documented in .env.example and the Helm ConfigMap only, NOT fanned out per docker-compose service like RECONCILER_*/JOBS_RETENTION -- the admitted-label map is per-process by design (D-10), so a per-process mismatch cannot corrupt anything and the compiled Go default (200) already applies everywhere it is left unset."

patterns-established:
  - "Package-level doc comment on retention.go records BOTH halves of the design rationale up front: why jobs deletion needs no explicit job_events/job_inputs/job_outputs/webhook_deliveries DELETE (ON DELETE CASCADE, 0001_init.sql), and why client_usage_daily is purged independently on its own longer window (the entire reason D-01 chose a dedicated rollup table over aggregating jobs)."

requirements-completed: [RETN-01]

# Metrics
duration: ~65min
completed: 2026-08-03
---

# Phase 51 Plan 04: Executable Retention Summary

**A batched, advisory-lock-gated purge hosted on the reconciler's existing tick deletes aged `jobs` rows (cascading to `job_events`/`job_inputs`/`job_outputs`/`webhook_deliveries`) and aged `client_usage_daily` rows on its own longer window, proven against live Postgres by tests that age data with SQL-backdated timestamps and assert the rows are actually gone — not described in a migration comment.**

## Performance

- **Duration:** ~65 min
- **Tasks:** 3 completed (all `type="auto"`, Task 1 `tdd="true"`)
- **Files modified:** 10 (3 created, 7 modified)
- **Completed:** 2026-08-03

## Accomplishments

- `internal/jobs/retention.go`: `PurgeJobs(ctx, olderThan, batch)` deletes `done`/`failed`/`canceled` jobs older than a Go-computed cutoff (never `queued`/`active`, regardless of age), batch-bounded via a `LIMIT` inside the delete's subquery (Postgres does not accept `LIMIT` directly on `DELETE`); `PurgeUsage(ctx, olderThan, batch)` deletes `client_usage_daily` rows by `ctid` (the table has no surrogate id) on its own window, with the cutoff computed in UTC to match `recordUsage`'s UTC-day write convention. Both return `(0, nil)` without executing a `DELETE` when `olderThan<=0` or `batch<=0` — the single highest-consequence guard in the file.
- `internal/jobs/retention_test.go`: 7 live-Postgres tests prove every `<behavior>` bullet — the primary aged-done-job-with-cascade case, `queued`/`active` survival regardless of age, fresh-job survival, `batch=1` against 3 eligible rows returning 1 and leaving 2, the usage 400-day window, and the disabled-window guard for both purges. Since neither purge is client-scoped (a deliberate, load-bearing property — one purge pass covers every client), every test registers `t.Cleanup` deletes for its own rows so debris a test deliberately leaves behind (the disabled-window and batch-limit fixtures) cannot corrupt a later, independent `go test` invocation's exact rows-affected assertions — verified by running the suite twice back-to-back.
- `internal/reconciler/reconciler.go`: `Config` gains `JobsRetention`/`UsageRetention`/`PurgeBatchSize` (zero-value by default — every pre-existing `Config` literal in the codebase omits them, so retention stays off until explicitly configured); `jobStore` interface grows `PurgeJobs`/`PurgeUsage`; new `Sweeper.purge(ctx)` runs up to `maxPurgeBatchesPerSweep` (10) batches per kind, stopping early on a partial batch, reporting each non-zero batch via `metrics.RecordReconcilerPurge`. `purge(ctx)` is called from both `Run` and `RunWithLock` **immediately after** `s.sweep(ctx)` — never from inside `sweep()`, whose early returns on a finder error would otherwise silently disable retention — and `RunWithLock`'s call site stays inside the `TryAcquire` advisory-lock gate, so exactly one fleet-wide replica ever purges.
- `internal/metrics/metrics.go`: `RecordReconcilerPurge(kind string, n int)` adds to the existing `reconcilerActions` CounterVec (`purged_jobs`/`purged_usage` added to its Help text) — no new metric, reusing the reconciler's existing reporting surface (T-51-15).
- `internal/reconciler/retention_test.go` (new): `TestPurgeDeletesAgedRows` (live Postgres — aged done job gone with its `job_events` cascade, fresh job spared, aged usage row gone), `TestPurgeDisabledWhenWindowsUnset` (asserted twice — against the fake, checking `olderThan=0`/default-batch call arguments, and against live Postgres, checking a real aged job survives — both using `testConfig()`'s exact literal shape, the same one every pre-existing test in the package uses), `TestPurgeIsBoundedPerTick` (fake store scripted to always report a full batch, proving the `maxPurgeBatchesPerSweep` ceiling — not a partial-batch early exit — is what stops the loop). Every pre-existing `internal/reconciler` test still passes unchanged.
- `cmd/webhook-worker/main.go`, `.env.example`, `docker-compose.yml` (both `webhook-worker-1`/`webhook-worker-2` blocks), `deploy/chart/octoconv/templates/configmap.yaml`: `JOBS_RETENTION=2160h` (90d), `USAGE_RETENTION=9600h` (400d), `RETENTION_BATCH_SIZE=1000` — byte-identical default values and a why-comment on the hour-not-day-unit choice (Go durations have no day unit; writing `90d` would silently fall back to the default) across every file, following the `RECONCILER_*`/`STORAGE_TTL` documented-default precedent. `METRICS_CLIENT_ID_CEILING=200` documented in `.env.example` and the Helm ConfigMap only (not fanned out per compose service, unlike the retention/`RECONCILER_*` keys — the admitted-label map is per-process by design, D-10).

## Task Commits

Each task was committed atomically:

1. **Task 1: Batched, bounded purge queries** — `22e8a5a` (feat)
2. **Task 2: Host the purge on the reconciler tick** — `37aeef5` (feat)
3. **Task 3: Propagate the retention knobs through every deployment surface** — `ffe439c` (feat)

**Plan metadata:** committed separately by the orchestrator (STATE.md/ROADMAP.md not owned by this plan).

## Files Created/Modified

- `internal/jobs/retention.go` — `PurgeJobs`/`PurgeUsage`, the package doc comment recording the cascade fact and the independent-usage-window rationale.
- `internal/jobs/retention_test.go` — 7 live-Postgres tests, plus `createAgedJob`/`insertUsageRow` helpers with `t.Cleanup`-based self-containment.
- `internal/reconciler/reconciler.go` — `Config` fields, `jobStore` interface methods, `defaultPurgeBatchSize`/`maxPurgeBatchesPerSweep` constants, `Sweeper.purge`, `Run`/`RunWithLock` call sites.
- `internal/reconciler/reconciler_test.go` — `fakeStore.PurgeJobs`/`PurgeUsage` with scriptable per-call `(rows, err)` results and call-argument recording.
- `internal/reconciler/retention_test.go` — the three plan-specified tests.
- `internal/metrics/metrics.go` — `RecordReconcilerPurge`, `reconcilerActions` Help text extended.
- `cmd/webhook-worker/main.go` — the three new `envDuration`/`envInt` reads wired into the `reconciler.Config` literal.
- `.env.example` — `JOBS_RETENTION`/`USAGE_RETENTION`/`RETENTION_BATCH_SIZE`/`METRICS_CLIENT_ID_CEILING` documented.
- `docker-compose.yml` — same three retention keys added to both `webhook-worker-1`/`webhook-worker-2` service blocks.
- `deploy/chart/octoconv/templates/configmap.yaml` — same three retention keys plus `METRICS_CLIENT_ID_CEILING`.

## Decisions Made

- PurgeJobs/PurgeUsage are deliberately table-wide (not `client_id`-scoped) — matching the plan's exact SQL shape. This surfaced a real test-fragility risk: rows a test intentionally leaves behind (the disabled-window and batch-limit fixtures) would otherwise accumulate as debris and corrupt a later, independent `go test` invocation's exact rows-affected assertions. Fixed with unconditional `t.Cleanup` deletes registered at row-creation time in both `internal/jobs/retention_test.go` and `internal/reconciler/retention_test.go`, verified by running the jobs-package suite twice back-to-back.
- Backdating test fixtures binds a Go-computed timestamp directly (`created_at = $2`) rather than issuing `now() - $2` in SQL — the latter fails against Postgres with `column "created_at" is of type timestamp with time zone but expression is of type interval`, because Postgres infers the bare Go `time.Duration` parameter as `timestamptz` (not `interval`) from the ambiguous expression context. Discovered live while writing Task 1's tests, not merely anticipated.
- `PurgeUsage`'s cutoff uses `time.Now().UTC()`, not local time — `client_usage_daily.day` is always written in UTC (Plan 02's `recordUsage`), so a non-UTC purge cutoff would drift the day boundary by the host's local offset.

## Deviations from Plan

None — plan executed exactly as written. The two live-Postgres discoveries above (the `now() - $2` type-inference failure, and the table-wide-purge test-debris fragility) were resolved entirely within Task 1's own `<verify>`/`<acceptance_criteria>` loop before committing; neither required deviating from the plan's specified SQL shape, `<interfaces>`, or file list.

## Issues Encountered

- The worktree had no `.env` (git-ignored, not present in a fresh worktree — same friction Plans 02/03 recorded). Copied `.env` from the main repo checkout so `DATABASE_URL` was available for the live-Postgres tests this plan's acceptance criteria require; no `.env` content was committed.
- The sandbox's command-complexity guard rejected `set -a && . ./.env && set +a`-style compound Bash blocks (same friction Plans 02/03 recorded) — worked around by passing `DATABASE_URL=... go test ...` as a single simple command instead.
- The worktree's HEAD was initially one commit behind the expected base commit (`a972366`, the `chore: merge executor worktree` landing from a sibling worktree after this one was spawned) — corrected via `git merge --ff-only` to the expected base before any work began (working tree was clean, no risk of lost work).
- Leftover debris from the mid-development `now() - $2` failure (jobs/usage rows partially created before the bug was caught) required one manual `psql` cleanup pass before the test suite could be verified clean and repeat-run-stable; this was purely local-dev-database hygiene, not a change to any committed file.

## Known Stubs

None. No UI or data-consuming code was touched by this plan.

## Next Phase Readiness

- Phase 52 (quota enforcement) reads `client_usage_daily`, which now has both a populated write path (Plan 03) and a proven, bounded retention purge (this plan) — consumption history is guaranteed to survive the `jobs`/`job_events` purge for the full 400-day window regardless of the 90-day operational retention on `jobs` itself.
- `go build ./...`/`go vet ./...`/`gofmt -l .` are all clean for the whole module; the full test suite (`go test ./... -count=1`) is green across all 20 test-bearing packages, no `SKIP` in any test this plan added or touched (the 20 pre-existing `SKIP`s are unrelated live-E2E/LibreOffice-binary/MCP-HTTP-live gates, unchanged by this plan).
- `helm template`/`helm lint` (with `values-local.yaml`, per this plan's own notes on the pre-existing unrelated `secret.yaml` nil-pointer against bare `helm lint`) and `docker compose config -q` all succeed; `JOBS_RETENTION`/`USAGE_RETENTION`/`RETENTION_BATCH_SIZE` render into the Helm output.
- `git diff go.mod go.sum` is empty and no `scaledobject-*.yaml` was touched — no new dependency, no KEDA-scaling change.

---
*Phase: 51-attribution-accounting-retention*
*Completed: 2026-08-03*

## Self-Check: PASSED

- FOUND: internal/jobs/retention.go
- FOUND: internal/jobs/retention_test.go
- FOUND: internal/reconciler/reconciler.go
- FOUND: internal/reconciler/reconciler_test.go
- FOUND: internal/reconciler/retention_test.go
- FOUND: internal/metrics/metrics.go
- FOUND: cmd/webhook-worker/main.go
- FOUND: .env.example
- FOUND: docker-compose.yml
- FOUND: deploy/chart/octoconv/templates/configmap.yaml
- FOUND commit: 22e8a5a (feat(51-04): add batched, bounded PurgeJobs/PurgeUsage retention queries)
- FOUND commit: 37aeef5 (feat(51-04): host the retention purge on the reconciler's advisory-lock tick)
- FOUND commit: ffe439c (feat(51-04): propagate retention knobs across every deployment surface)
