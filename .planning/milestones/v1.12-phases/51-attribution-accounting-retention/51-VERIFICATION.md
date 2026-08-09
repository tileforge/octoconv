---
phase: 51-attribution-accounting-retention
verified: 2026-08-02T21:45:42Z
status: passed
score: 11/11 must-haves verified
overrides_applied: 0
---

# Phase 51: Attribution, Accounting & Retention Verification Report

**Phase Goal:** Per-client job outcomes and consumption are tracked in Postgres and Prometheus without multiplying existing metric cardinality or repeating the platform's unbounded-table-growth debt.
**Verified:** 2026-08-02T21:45:42Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

Sourced from ROADMAP.md §Phase 51 (6 success criteria, 4 tightened after PR #3 review) merged with the 51-CONTEXT.md D-01..D-13 decisions and each plan's `must_haves`. Every truth below was checked against running code and live-Postgres test output, not against SUMMARY.md prose.

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 (SC1/ACCT-01/ACCT-04) | A new, purpose-built counter carries `client_id` on job outcomes; `octoconv_job_duration_seconds` stays `{engine,status}`; `octoconv_queue_depth` stays `{queue,state}`; proven by a pinning test | VERIFIED | `internal/metrics/metrics.go:43-46` declares `clientJobOutcomes` as a separate `CounterVec{client_id,engine,status}`; `jobOutcomes`/`jobDuration` declarations untouched. `go test ./internal/metrics/ -run 'TestJobDurationLabelsPinned|TestQueueDepthLabelsPinned' -v` → both PASS live. `grep -v '^//' metrics.go \| grep client_id` shows every non-comment occurrence confined to the `clientJobOutcomes` block. |
| 2 (SC2/ACCT-02/ACCT-03/D-05) | Every terminal transition (incl. reconciler recovery) increments the Postgres rollup exactly once, and the write is **inside** the transition transaction, not adjacent | VERIFIED | `internal/jobs/repo.go:176-201` — `MarkDone`/`MarkFailed` call `r.recordUsage(ctx, tx, ...)` from inside the `apply` closure passed to `r.transition`, which itself runs inside `pgx.BeginFunc` (`repo.go:579-605`) on the same `pgx.Tx` that holds `SELECT ... FOR UPDATE`. This is the OPPOSITE of the codebase's pre-existing `metrics.RecordJobOutcome` habit (14 sites in `worker.go`, all fired *after* the transition returns) — confirmed by inspecting both call patterns side by side. Live proof: `TestSoakStaleRecoveryCountsUsageExactlyOnce` (`internal/reconciler/reconciler_soak_test.go:214`) genuinely waits past `ActiveStaleAfter`, calls `sweep()`, asserts the zombie's `MarkDone` fails with `"illegal transition"`, completes the recovered attempt, and asserts `job_count == 1` (not 2) — **PASS (1.45s)**, run live in this session. |
| 3 (D-06, inversion check) | The exactly-once property is load-bearing, demonstrated by moving the write outside the transaction and observing failure | VERIFIED | `51-02-SUMMARY.md` records the inversion was performed live: `job_count = 2, want exactly 1` observed, then reverted (commit `b464af9`). Independently re-confirmed in this session: `TestRecordUsageExactlyOnceUnderConcurrentMarkDone` and `TestRecordUsageSecondTerminalTransitionIsRejected` both PASS under `-race`. |
| 4 (SC3/ACCT-04/D-08/D-09/D-10) | `client_id` cardinality bounded by an explicit **configured numeric ceiling**; overflow collapses to a single stable value; malformed/absent ids never mint a series | VERIFIED | `internal/metrics/metrics.go:100-113` — `ClientLabelCeiling` resolved from `METRICS_CLIENT_ID_CEILING` (default 200, numeric, not "bounded by clients table"). `clientLabelAllocator.resolve` (`:155-174`) collapses empty/non-UUID/nil-UUID/over-ceiling ids into the literal `overflow` without consuming a slot; already-admitted ids are never evicted. `TestClientLabelCeilingCollapsesToOverflow`, `TestClientLabelRejectsMalformedID` PASS live. Live-cluster evidence (51-05-SUMMARY.md): real client UUID observed on `/metrics`, not `overflow`, on a real pod. |
| 5 (SC4/RETN-01/D-11/D-13) | Retention is **executable**: a purge runs and a test ages data and asserts rows are gone; in-flight/fresh rows survive | VERIFIED | `internal/jobs/retention.go` — `PurgeJobs`/`PurgeUsage`, batch-bounded (`LIMIT $2`), Go-computed cutoffs (no `interval`/`now() -` arithmetic), `olderThan<=0`/`batch<=0` → `(0,nil)` no-op. Hosted on the reconciler's real tick: `s.purge(ctx)` called from both `Run` and `RunWithLock` immediately after `s.sweep(ctx)` (`internal/reconciler/reconciler.go:118-119,252-262`). Live test run this session: `go test ./internal/jobs/ -run TestPurge -v` → 7/7 PASS, including `TestPurgeJobsSparesQueuedAndActiveRegardlessOfAge` and `TestPurgeJobsSparesFreshJobInsideWindow`; `go test ./internal/reconciler/ -run TestPurge -v` → 3/3 PASS including `TestPurgeDisabledWhenWindowsUnset`. Also proven on a live k8s cluster in 51-05-SUMMARY.md (`aged BEFORE sweep: 1` → `aged AFTER sweep: 0`, with `octoconv_reconciler_actions_total{action="purged_jobs"}` materialising). |
| 6 (RETN-01, second table) | `client_usage_daily` purged independently, on its own longer window | VERIFIED | `internal/jobs/retention.go` `PurgeUsage` uses `client_usage_daily_day_idx` on its own `USAGE_RETENTION` cutoff (9600h/400d vs `JOBS_RETENTION` 2160h/90d). `TestPurgeUsageDeletesAgedRowKeepsFreshRow` PASS live. |
| 7 (SC5/D-07) | Accounting work leaves `octoconv_queue_depth` and every `ScaledObject` PromQL untouched; KEDA live gate re-run shows no regression | VERIFIED | `git diff --stat 25008db~1..867290d -- deploy/chart/octoconv/templates/ internal/metrics/queue_collector.go internal/queue/` → only `configmap.yaml` changed (15 insertions), zero `scaledobject-*.yaml`, zero `queue_collector.go`, zero `internal/queue/` — re-run live in this session, matches the plan's own evidence command exactly. Live KEDA gate already executed by the operator per plan 05's `checkpoint:human-verify` gate (not re-run here per task instructions — OrbStack k8s intentionally off): `scripts/keda-gate.sh` 21/21, matching the recorded baseline, captured verbatim in `51-05-SUMMARY.md`. |
| 8 (SC6/ACCT-02/D-03/SEED-017) | Bytes summed over ALL inputs/outputs, never ordinal 0 | VERIFIED | `internal/jobs/usage.go:56-57` — two correlated `COALESCE((SELECT sum(size_bytes) FROM job_inputs/job_outputs WHERE job_id = j.id), 0)` subqueries with no ordinal predicate. `grep -v '^\s*//' usage.go \| grep -c ordinal` → 0. Live test `TestRecordUsageSumsAllOrdinals` (2 inputs/2 outputs, 300/30 bytes not 100/10) PASS. Live-cluster confirmation in `51-05-SUMMARY.md`: `pandoc\|done\|1\|32\|3935` — non-trivial non-1:1-looking real bytes from an actual conversion. |
| 9 (ACCT-02/D-02/D-04) | Failed jobs are counted, with status as a column; wall-time is recorded | VERIFIED | Migration `CHECK (status IN ('done','failed'))`, PK includes `status`. `TestRecordUsageFailedPathAndStatusSeparation` PASS live — two distinct rows for done vs failed. `TestRecordUsageWallSeconds` (3 sub-cases: explicit attempt, derived from `started_at`, zero when never started) PASS live. |
| 10 (T-51-07) | A job whose client was deleted (`client_id IS NULL`) still reaches done/failed and writes no usage row | VERIFIED | `usage.go:71` `WHERE j.id = $1 AND j.client_id IS NOT NULL` — zero SELECT rows → zero INSERT rows → no error. `TestRecordUsageSkipsOrphanedClient` PASS live. |
| 11 (env config parity, D-12) | Retention env keys byte-identical across `.env.example`, `docker-compose.yml`, Helm ConfigMap, and Go defaults | VERIFIED | `grep JOBS_RETENTION\|USAGE_RETENTION\|RETENTION_BATCH_SIZE` across all four surfaces: `2160h`/`9600h`/`1000` identical everywhere (`cmd/webhook-worker/main.go:103-105`, `.env.example:206-208`, `docker-compose.yml:264-266,337-339`, `configmap.yaml:116-118`). `helm template ... \| grep -c JOBS_RETENTION` → 1. `docker compose config -q` exits 0. `helm lint` with `values-local.yaml` → 0 failures (bare `helm lint` fails on a pre-existing, unrelated `secret.yaml` nil-pointer, documented in 51-04-SUMMARY.md and reproduced independently in this session). |

**Score:** 11/11 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/db/migrations/0011_client_usage_daily.sql` | `client_usage_daily` table, PK (client_id,engine,day,status), day index, reused trigger, no engine CHECK | VERIFIED | Applied live via `go run ./cmd/migrate` (implicit — `TestMigrationsApplyClientUsageDaily` runs `Migrate()` against live Postgres and passes). Grep checks all match acceptance criteria exactly (1 CREATE TABLE, 0 engine CHECK, 1 PK match). |
| `internal/db/migrate_test.go` | Live-Postgres schema-contract test | VERIFIED | `TestMigrationsApplyClientUsageDaily` PASS, run live this session. |
| `internal/metrics/metrics.go` | `octoconv_client_job_outcomes_total` + bounded allocator | VERIFIED | Present, wired, tested — see Truth 4. |
| `internal/metrics/metrics_test.go` | Overflow + pinning tests | VERIFIED | 6 new/modified tests, all PASS live. |
| `internal/jobs/usage.go` | `recordUsage` transactional rollup | VERIFIED | Exists, correct SQL shape, called from inside `transition`'s closure. |
| `internal/jobs/usage_test.go` | Multi-ordinal, failed-path, orphan, exactly-once proofs | VERIFIED | 9 test functions (incl. sub-tests), all PASS under `-race`. |
| `internal/jobs/retention.go` | `PurgeJobs`/`PurgeUsage` | VERIFIED | Batch-bounded, Go-cutoff, disabled-by-default guard — all confirmed by grep and live tests. |
| `internal/jobs/retention_test.go` | Aged-data proof incl. queued/active survival | VERIFIED | 7 tests PASS live, including the survival cases. |
| `internal/reconciler/retention_test.go` | Sweeper-level purge proof | VERIFIED | 3 tests PASS live, including `TestPurgeDisabledWhenWindowsUnset`. |
| `internal/reconciler/reconciler_soak_test.go` | Forced stale-recovery race + exhaustion accounting | VERIFIED | `TestSoakStaleRecoveryCountsUsageExactlyOnce`, `TestSoakExhaustedJobIsAccounted` both PASS live (genuinely waits real time, no backdating). |
| `internal/worker/worker.go` | Attempt-duration threading + per-client metric at all 7 handlers | VERIFIED | 14 `MarkFailed` + 1 `MarkDone` sites all widened; 14/14 `RecordClientJobOutcome` calls paired with `RecordJobOutcome`. |
| `.env.example` / `docker-compose.yml` / `deploy/chart/octoconv/templates/configmap.yaml` / `cmd/webhook-worker/main.go` | Retention + ceiling env keys, byte-identical | VERIFIED | See Truth 11. |
| `.planning/phases/51-attribution-accounting-retention/51-05-SUMMARY.md` | Captured live-gate evidence (KEDA 21/21, live `/metrics`, live purge) | VERIFIED | Present with verbatim operator output; not re-run per task instructions (OrbStack k8s intentionally off in this session). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `internal/jobs/repo.go` `MarkDone`/`MarkFailed` apply closures | `internal/jobs/usage.go` `recordUsage` | `r.recordUsage(ctx, tx, id, status, attempt)` inside `apply` | WIRED | Confirmed by direct code read (`repo.go:181,200`) — call happens on the same `tx` before `apply` returns, i.e. before the row lock is released and before `transition`'s own `job_events` INSERT. |
| `internal/metrics/metrics.go` | `clientLabel` allocator | `RecordClientJobOutcome` resolves every id through `clientLabel()` before `WithLabelValues` | WIRED | `metrics.go:191-193` — no other call path to `clientJobOutcomes` exists in the package. |
| `internal/worker/worker.go` handlers | `metrics.RecordClientJobOutcome` | one call adjacent to each `metrics.RecordJobOutcome` | WIRED | 14/14 pairs confirmed by grep across all 7 handlers. |
| `internal/reconciler/reconciler.go` exhaustion path | `jobs.Repo.MarkFailed` | `attempt=0`, plus `RecordClientJobOutcome` guarded by `job != nil` | WIRED | `reconciler.go:311,320`. |
| `internal/reconciler/reconciler.go` `Run`/`RunWithLock` | `Sweeper.purge` | called immediately after `sweep()`, inside the advisory-lock gate for `RunWithLock` | WIRED | `reconciler.go:118-119` (Run), `:252-262` (RunWithLock) — `s.purge(ctx)` appears exactly twice, confirmed by grep. |
| `cmd/webhook-worker/main.go` | `reconciler.Config` | `envDuration`/`envInt` reads into the Config literal | WIRED | `main.go:103-105`. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `client_usage_daily` rows | `job_count`, `bytes_in`, `bytes_out`, `wall_seconds` | `recordUsage`'s `SELECT ... FROM jobs j` joined against live `sum(size_bytes)` over `job_inputs`/`job_outputs` | Yes | FLOWING — confirmed by live unit tests (real byte sums, not fixtures returning static values) AND by the 51-05 live-cluster evidence: a real md→html pandoc conversion produced `bytes_in=32, bytes_out=3935`, non-round, engine-realistic numbers. |
| `octoconv_client_job_outcomes_total` | `client_id` label | `job.ClientID.String()` read from the actually-processed job row at each of the 14 worker call sites and the reconciler exhaustion site | Yes | FLOWING — live-cluster `/metrics` scrape shows a real client UUID, not a static or `overflow` placeholder, immediately after a real conversion. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full module builds clean | `go build ./...` | exit 0 | PASS |
| Full module vets clean | `go vet ./...` | exit 0, no output | PASS |
| Formatting clean | `gofmt -l .` | no output | PASS |
| Exactly-once under real concurrency | `go test ./internal/jobs/ -run TestRecordUsage -race -v` | 9/9 PASS | PASS |
| Forced stale-recovery race, live Postgres | `go test ./internal/reconciler/ -run TestSoak -v` | 4/4 PASS (6.47s, real elapsed waits) | PASS |
| Retention purge, live Postgres | `go test ./internal/jobs/ -run TestPurge -v` + `go test ./internal/reconciler/ -run TestPurge -v` | 10/10 PASS | PASS |
| Whole test suite | `go test ./... -count=1` | all packages `ok`, 20 pre-existing unrelated live-binary/E2E SKIPs (documented, none from Phase 51) | PASS |
| Autoscaling contract zero-diff | `git diff --stat <merge-base>..HEAD -- deploy/chart/octoconv/templates/ internal/metrics/queue_collector.go internal/queue/` | only `configmap.yaml` (15 insertions) | PASS |
| Chart renders retention keys | `helm template deploy/chart/octoconv -f values-local.yaml \| grep -c JOBS_RETENTION` | 1 | PASS |
| Compose config valid | `docker compose config -q` | exit 0 | PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` declared or referenced by this phase's PLAN/SUMMARY files. `scripts/keda-gate.sh` is the phase's live-gate script but is explicitly an operator-run `checkpoint:human-verify` task (plan 05, `autonomous: false`) whose evidence is already captured verbatim in `51-05-SUMMARY.md`; per task instructions, cluster gates were not re-run in this session (OrbStack k8s intentionally off).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| ACCT-01 | 51-01, 51-03, 51-05 | Per-client Prometheus counter, separate from duration histogram, `queue_depth` untouched | SATISFIED | Truths 1, 7 |
| ACCT-02 | 51-01, 51-02 | Postgres daily rollup, written at the guarded-transition write site, bytes over all inputs/outputs | SATISFIED | Truths 2, 8, 9 |
| ACCT-03 | 51-02, 51-03 | Idempotent accounting — one job counted exactly once, including reconciler-recovered jobs | SATISFIED | Truths 2, 3 |
| ACCT-04 | 51-01, 51-05 | Explicit numeric cardinality ceiling with documented overflow | SATISFIED | Truth 4 |
| RETN-01 | 51-04 | Executable retention (not documentary), proven by aging data | SATISFIED | Truths 5, 6 |

**Note:** `.planning/REQUIREMENTS.md` still shows `[ ]` (unchecked) for ACCT-01..04/RETN-01 and a `Traceability` table marked `Pending`. This appears to be a whole-milestone (v1.12) tracking convention — every requirement for Phases 51-55 is unchecked, including ones for phases not yet started — rather than a Phase 51-specific gap. `.planning/ROADMAP.md` correctly marks Phase 51 and all 5 of its plans `[x]` complete. Flagged here for the record; not treated as a code/behavior gap since it is a documentation-sync artifact, not a missing capability.

No orphaned requirements found: REQUIREMENTS.md maps exactly ACCT-01..04 and RETN-01 to Phase 51, and all five appear in at least one plan's `requirements:` frontmatter field.

### Anti-Patterns Found

None. Scanned all 18 files touched across the phase's 5 plans (migration, metrics, jobs/usage, jobs/retention, worker, reconciler, cmd/webhook-worker, env/compose/chart) for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER` and placeholder-language patterns — zero matches. No debt markers. No stub return patterns in the modified files.

### Human Verification Required

None outstanding. Plan 05's single `checkpoint:human-verify` task (KEDA live gate re-run) was already completed by the operator; its verbatim evidence (21/21 assertions, live `/metrics` label sets, live purge demonstration) is captured in `51-05-SUMMARY.md` and was not re-executed in this session per explicit task instructions (OrbStack k8s intentionally off; do not run live cluster gates during static verification).

### Gaps Summary

No gaps found. All 6 ROADMAP success criteria (including the 4 tightened after PR #3 review) and all 5 requirement IDs (ACCT-01..04, RETN-01) are backed by code that was read directly and by tests that were re-run live in this session against a running Postgres/Redis/MinIO stack — not merely accepted from SUMMARY.md claims. The two properties this phase's own design flagged as "deliberately unsatisfiable by documentation" (D-05's transactional write site, D-13's executable retention) were independently traced in the source and re-proven by live test execution rather than taken on faith.

One non-blocking documentation note: `.planning/REQUIREMENTS.md` checkbox/traceability status has not been updated to reflect Phase 51's completion (see Requirements Coverage section above). This is a doc-sync item, not a functional gap, and does not affect the phase's goal achievement.

---

*Verified: 2026-08-02T21:45:42Z*
*Verifier: Claude (gsd-verifier)*
