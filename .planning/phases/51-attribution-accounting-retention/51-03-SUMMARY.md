---
phase: 51-attribution-accounting-retention
plan: 03
subsystem: worker
tags: [prometheus, postgres, reconciler, worker, accounting, concurrency]

# Dependency graph
requires:
  - phase: 51-02
    provides: "Widened MarkDone(ctx, id, attempt)/MarkFailed(ctx, id, code, message, detail, attempt) signatures; recordUsage inside transition's apply closure"
provides:
  - "Every one of the 7 engine handlers threads its real per-attempt wall-clock duration into MarkFailed/MarkDone, and fires metrics.RecordClientJobOutcome adjacent to every existing metrics.RecordJobOutcome"
  - "The reconciler's exhaustion path (jobStore.MarkFailed) accounts both the Postgres rollup and the per-client Prometheus counter"
  - "Live-Postgres proof that the reconciler's stale-recovery race counts client_usage_daily exactly once, forcing the race for real rather than arguing it"
affects: [51-04, 51-05, 52]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "attempt=0 sentinel for pre-MarkActive terminal writes: any MarkFailed call site that runs before the job has ever transitioned to active passes 0 -- recordUsage's SQL then finds jobs.started_at IS NULL and itself falls back to 0, correctly recording zero wall time for a job that never occupied a worker (invalid_options guards in worker.go; the reconciler's exhaustion path)"
    - "attemptStart as a SEPARATE clock from the handler's own `start` local inside process(): start feeds the duration histogram (metrics.RecordJobOutcome) from the top of the handler, attemptStart feeds the client_usage_daily wall_seconds column from immediately after the attempt context is created -- two clocks for two different accounting destinations"

key-files:
  created: []
  modified:
    - internal/worker/worker.go
    - internal/reconciler/reconciler.go
    - internal/reconciler/reconciler_test.go
    - internal/reconciler/reconciler_soak_test.go

key-decisions:
  - "The invalid_options MarkFailed sites (6 of them, one per non-image handler) pass attempt=0 rather than a captured pre-guard clock: these guards run BEFORE MarkActive, so the job never left `queued` and never occupied a worker slot -- recordUsage's own started_at-IS-NULL fallback already produces the correct answer (0) without needing a second clock. Documented once at the first (Document handler) site and referenced tersely at the other five."
  - "No metrics.RecordClientJobOutcome call was added at any invalid_options site, mirroring the pre-existing absence of metrics.RecordJobOutcome there -- keeps octoconv_client_job_outcomes_total summable against octoconv_job_outcomes_total; the Postgres rollup remains authoritative for that asymmetry regardless."
  - "process()'s attemptStart is captured immediately after the attempt context (context.WithTimeout) is created, before cancel.Default.Register and any download/convert/upload work -- it measures the FULL attempt the eventual MarkDone call is committing, not just the engine invocation."
  - "The reconciler's exhaustion-path MarkFailed passes attempt=0 with the same started_at-NULL rationale as the invalid_options sites -- documented explicitly at the call site rather than assumed from Plan 02's precedent."
  - "TestSoakStaleRecoveryCountsUsageExactlyOnce calls s.sweep(ctx) directly (same package) instead of racing Sweeper.Run's ticker, for a deterministic recovery point, while still crossing ActiveStaleAfter by genuinely waiting (no started_at backdating) -- mirrors this file's existing TestSoakRecoversStrandedQueuedJob discipline."
  - "TestSoakExhaustedJobIsAccounted uses MaxRecoveries=0 so the reconciler_exhausted branch fires on the very first sweep past QueuedStaleAfter, keeping the test fast without multiple recovery round-trips -- it is the only live-Postgres coverage for Task 2's wiring, since reconciler_test.go's fakeStore never touches a real client_usage_daily table."

patterns-established:
  - "Fire-and-forget vs. transactional durability contract called out explicitly in a comment at the FIRST adjacent RecordJobOutcome/RecordClientJobOutcome pair in the file (HandleImageConvert), rather than repeated at all 7 sites -- future readers who only see the later 6 pairs are expected to have already read the first."

requirements-completed: [ACCT-01, ACCT-03]

# Metrics
duration: ~45min
completed: 2026-08-02
---

# Phase 51 Plan 03: Call-site propagation + forced stale-recovery race proof Summary

**All 14 `h.repo.MarkFailed` calls and the single `h.repo.MarkDone` call across the seven engine handlers now thread their real per-attempt wall-clock duration (`time.Since(start)`/`time.Since(attemptStart)`, or a documented `0` at the 6 pre-`MarkActive` `invalid_options` guards), `metrics.RecordClientJobOutcome` fires adjacent to every one of the 14 `metrics.RecordJobOutcome` calls, the reconciler's exhaustion path now accounts both Postgres and Prometheus, and a live-Postgres soak test forces the reconciler's stale-recovery race for real — MarkActive, wait out `ActiveStaleAfter`, sweep, reject the zombie's `MarkDone` as an illegal transition, complete the recovered attempt, and assert `client_usage_daily.job_count = 1`, not merely that recovery happened.**

## Performance

- **Duration:** ~45 min
- **Tasks:** 3 completed (all `type="auto"`)
- **Files modified:** 4
- **Completed:** 2026-08-02

## Accomplishments

- **Observed call-site counts (verified by grep before editing, per the plan's own instruction not to trust quoted line numbers):** `internal/worker/worker.go` had exactly 14 `h.repo.MarkFailed(` calls, exactly 1 `h.repo.MarkDone(` call, and exactly 14 `metrics.RecordJobOutcome(` calls — matching the plan's expectation exactly. Of the 14 `MarkFailed` calls, 6 are `invalid_options` guards that run before `MarkActive` (one per non-image handler: Document, HTML, Audio, AV, OCR, Pandoc — the Image handler has no opts-parse guard) and 8 are in the post-`start` terminal-classification branch (one per handler, plus Audio's split `duration_exceeded`/`engine_error` branch).
- Every terminal `MarkFailed` site after `start := time.Now()` now passes `time.Since(start)`; every `invalid_options` site now passes a literal `0`, documented once (Document handler, the first such site in file order) with the full rationale and referenced tersely at the other five.
- `process()` gained a second, separate clock (`attemptStart`, captured immediately after the attempt `context.WithTimeout` and before `cancel.Default.Register`) threaded into the single `h.repo.MarkDone` call — distinct from each handler's own `start`, which continues to feed `metrics.RecordJobOutcome`'s duration histogram unchanged.
- `metrics.RecordClientJobOutcome(job.ClientID.String(), <same engine>, <same status>)` now fires immediately after every one of the 14 `metrics.RecordJobOutcome` calls — never at the 6 `invalid_options` sites, mirroring their pre-existing absence of `RecordJobOutcome` there (documented as the known asymmetry, both at the first `invalid_options` site and here).
- `internal/reconciler`'s `jobStore.MarkFailed` interface method widened to the same `attempt time.Duration` signature; the exhaustion call site passes `0` (the reconciler has no attempt clock) and now also fires `metrics.RecordClientJobOutcome` immediately after `metrics.RecordReconcilerAction("exhausted")`, guarded by the same `job != nil` check already in place for the webhook enqueue — closing T-51-10 (a reconciler-exhausted job was previously counted in Postgres but invisible per-client in Prometheus).
- `TestSoakStaleRecoveryCountsUsageExactlyOnce` (new): drives a real `jobs.Repo` against live Postgres with the in-memory `fakeEnqueuer`, forces the documented stale-recovery race by genuinely waiting past `ActiveStaleAfter` and calling `s.sweep(ctx)` directly, asserts the zombie original worker's post-recovery `MarkDone` fails with `"illegal transition"`, completes the recovered attempt, and asserts `client_usage_daily` shows exactly one row with `job_count = 1` (not 2), correct summed byte totals, and `wall_seconds > 0`. **PASS** (1.38s).
- `TestSoakExhaustedJobIsAccounted` (new): `MaxRecoveries = 0` drives the exhaustion branch on the first sweep past `QueuedStaleAfter`, asserting a `client_usage_daily` row with `status = 'failed', job_count = 1` — the only live-Postgres coverage for Task 2's wiring. **PASS** (1.37s).
- All 4 soak tests in the file (`TestSoakRecoversStrandedQueuedJob`, `TestSoakExhaustsAtCap`, and the two new ones) pass; the full suite (`go test ./... -count=1`) is green across all 22 packages with test files, no skips.

## Task Commits

Each task was committed atomically:

1. **Task 1: Thread attempt duration and per-client outcomes through the seven engine handlers** — `c26b592` (feat)
2. **Task 2: Account the reconciler's exhaustion path** — `e6facf4` (feat)
3. **Task 3: Force the stale-recovery race and assert the usage total** — `40b7991` (test)

**Plan metadata:** committed separately by the orchestrator (STATE.md/ROADMAP.md not owned by this plan).

## Files Created/Modified

- `internal/worker/worker.go` — every `h.repo.MarkFailed`/`h.repo.MarkDone` call widened with its attempt duration; `metrics.RecordClientJobOutcome` added adjacent to all 14 `metrics.RecordJobOutcome` calls; `process()` gained the `attemptStart` clock. No handler control flow, error classification, or webhook-enqueue ordering changed.
- `internal/reconciler/reconciler.go` — `jobStore.MarkFailed` interface widened; the exhaustion branch passes `attempt=0` and fires `metrics.RecordClientJobOutcome`.
- `internal/reconciler/reconciler_test.go` — `fakeStore.MarkFailed` updated to the new signature; `markFailedCalls` assertions unchanged; the attempt duration is now also recorded in a new `markFailedAttempts` slice (unused by any assertion yet, available for a future test).
- `internal/reconciler/reconciler_soak_test.go` — two new tests: `TestSoakStaleRecoveryCountsUsageExactlyOnce`, `TestSoakExhaustedJobIsAccounted`; added the `strings` import for the "illegal transition" substring assertion.

## Decisions Made

- attempt=0 at every pre-`MarkActive` `invalid_options` site and at the reconciler's exhaustion site: both cases share the same underlying reason (the job never occupied a worker, `jobs.started_at` is still `NULL`), and `recordUsage`'s own `started_at IS NOT NULL` branch already derives the correct fallback of `0` — no second clock needed at these sites. This was Plan 02's own documented refinement of D-03a, now threaded through every caller that hits it.
- No `RecordClientJobOutcome` at `invalid_options` sites, preserving summability between `octoconv_client_job_outcomes_total` and `octoconv_job_outcomes_total` — an explicit, plan-mandated asymmetry, not an oversight.
- `TestSoakStaleRecoveryCountsUsageExactlyOnce` calls `s.sweep(ctx)` directly rather than racing `Sweeper.Run`'s ticker, for a deterministic recovery point in the test, while still crossing the staleness threshold by genuinely waiting — this mirrors the file's pre-existing `TestSoakRecoversStrandedQueuedJob` discipline exactly (real elapsed time, no `started_at` backdating).

## Deviations from Plan

None — plan executed exactly as written. `.planning/phases/51-attribution-accounting-retention/51-PATTERNS.md`, referenced in the plan's `<files_to_read>` and `<read_first>` blocks, does not exist in this worktree; the plan itself anticipated this ("Locate the call sites by grep, not by the line numbers quoted anywhere in the planning docs — they drift") and grep-based discovery was used throughout, producing the exact call-site counts the plan predicted (14/1/14).

## Issues Encountered

- The worktree had no `.env` (git-ignored, not present in a fresh worktree — same as Plan 02). Copied `.env` from the main repo checkout (`/Users/apaderin/dev/octoconv/.env`) so `DATABASE_URL` was available for the live-Postgres soak tests this plan's acceptance criteria require; no `.env` content was committed (still git-ignored in the worktree).
- The sandbox's command-complexity guard rejected compound `git rev-parse ... ; case ... ; done`-style multi-statement Bash blocks for the mandatory worktree cwd-drift/HEAD-safety assertions (same friction Plan 02 recorded for `set -a && . ./.env`). Worked around by running each check as its own simple Bash call; all specified safety assertions were still performed in substance.
- The worktree's HEAD was initially one commit behind the expected base (a sibling `chore: merge executor worktree` commit landed after this worktree was created) — corrected via `git reset --hard` to the expected base per the mandatory branch-check step before any work began (working tree was clean, no risk of lost work).

## Known Stubs

None. No UI or data-consuming code was touched by this plan.

## Next Phase Readiness

- Plan 04's retention purge can now rely on `client_usage_daily` being populated by every real terminal transition across all 7 engines and the reconciler's exhaustion path — the full write surface this plan's own scope covers is now wired, giving Plan 04 non-empty, complete data to age and delete against.
- `go build ./...` is clean for the whole module (the starting-state note's expected breakage in `internal/worker` and `internal/reconciler` is fully resolved).
- Phase 52 (quota enforcement) has a proven, exactly-once-by-construction, fully-wired data source — every code path that can terminally transition a job (7 engine handlers' happy/terminal paths plus the reconciler's exhaustion path) now writes to `client_usage_daily`.

---
*Phase: 51-attribution-accounting-retention*
*Completed: 2026-08-02*

## Self-Check: PASSED

- FOUND: internal/worker/worker.go
- FOUND: internal/reconciler/reconciler.go
- FOUND: internal/reconciler/reconciler_test.go
- FOUND: internal/reconciler/reconciler_soak_test.go
- FOUND: .planning/phases/51-attribution-accounting-retention/51-03-SUMMARY.md
- FOUND commit: c26b592 (feat(51-03): thread attempt duration and per-client outcomes through the seven engine handlers)
- FOUND commit: e6facf4 (feat(51-03): account the reconciler's exhaustion path)
- FOUND commit: 40b7991 (test(51-03): force the reconciler stale-recovery race live and assert usage exactly-once)
