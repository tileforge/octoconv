---
phase: 41-progress-core
plan: 03
subsystem: worker
tags: [postgres, asynq, progress-throttle, ffmpeg, api]

# Dependency graph
requires:
  - phase: 41-progress-core
    provides: "ProgressUpdate/ProgressSink/ProgressConverter seam contracts + AVConverter.ConvertWithProgress (41-01/41-02)"
provides:
  - "Repo.UpdateProgress / Repo.SetSourceDuration: non-locking, monotonic, self-conditioning progress writes that never touch the guarded transition lock or job_events (D-03b)"
  - "Job.ProgressPct/EtaSeconds/SourceDurationSeconds fields + Get scanning the three progress columns"
  - "Worker type-assert seam (runConvertWithOptionalProgress) driving a dual-gate throttled progressSink through AVConverter.ConvertWithProgress, with a terminal-100 throttle-bypass on success and freeze-on-abort on error"
  - "PROGRESS_MIN_INTERVAL_SECONDS / PROGRESS_MIN_DELTA_PCT env knobs wired onto av-worker's Handler via SetProgressThrottle"
  - "GET /v1/jobs/{id} surfaces progress_pct/eta_seconds null-aware for every engine"
affects: [41-04-live-gate, 42-push-progress]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Interface-segregated progressRepo (UpdateProgress/SetSourceDuration subset) lets the worker's throttle/bypass/freeze logic be unit-tested with a spy, without a live Postgres-backed *jobs.Repo"
    - "Package-level seam function (runConvertWithOptionalProgress) mirrors enforceAudioGuardBeforeConvert's factoring: type-assert + branch logic lives outside the Handler method so it is testable with fakes"
    - "Post-construction Set*Throttle setter mirrors Phase 39's SetCancelPublisher — NewHandler's signature stays stable for every non-av worker cmd"

key-files:
  created: []
  modified:
    - internal/jobs/repo.go
    - internal/jobs/jobs.go
    - internal/jobs/repo_test.go
    - internal/worker/worker.go
    - internal/worker/worker_test.go
    - internal/api/handlers.go
    - internal/api/handlers_test.go
    - cmd/av-worker/main.go
    - .env.example

key-decisions:
  - "UpdateProgress/SetSourceDuration are plain r.pool.Exec UPDATEs, never routed through transition() — no SELECT...FOR UPDATE, no job_events insert (D-03b). Self-conditioning via WHERE status='active' AND (progress_pct IS NULL OR progress_pct <= $new): 0 rows affected is the expected outcome for a late/out-of-order tick or a terminal job, not an error."
  - "The terminal 100% write is issued directly against repo (bypassing the sink's gated Update) from runConvertWithOptionalProgress, immediately after a successful ConvertWithProgress return and strictly before MarkDone — this is what makes D-03a's 'always lands' guarantee true by construction rather than by timing luck."
  - "Freeze-on-abort (D-06) needed no explicit reset logic: it is simply the terminal-100 call never executing on the error branch. Proven at the unit level with a spy progressRepo (TestProgressFreezeOnAbort), not only asserted by code inspection."
  - "progressSink depends on a narrow progressRepo interface, not the concrete *jobs.Repo the rest of Handler uses — *jobs.Repo satisfies it structurally with zero wiring changes, and it is what makes TestProgressThrottle/TestProgressFreezeOnAbort possible without a live Postgres pool."
  - "handleGetJob surfaces resp[\"progress_pct\"] = job.ProgressPct (a *int) directly rather than an if/else — encoding/json already renders a nil pointer as JSON null and a non-nil one as the plain integer, which is exactly the 'present but null vs a real number' contract PROG-01 requires."

patterns-established:
  - "Pattern: narrow interface segregation for a single write-path dependency (progressRepo) purely to unlock spy-based unit testing of throttle/timing logic that would otherwise require a live DB."

requirements-completed: [PROG-01, PROG-02, PROG-03, PROG-04]

# Metrics
duration: ~15min
completed: 2026-07-25
---

# Phase 41 Plan 03: Non-Locking Progress Persistence + Worker Throttle + API Surfacing Summary

**A non-locking, monotonic `Repo.UpdateProgress`/`SetSourceDuration` write path that never contends the guarded status-transition lock or `job_events`, driven by a worker-side dual-gate throttle (>=2s AND >=1%) with a terminal-100 bypass and freeze-on-abort, surfaced null-aware on `GET /v1/jobs/{id}` — closing PROG-01 through PROG-04 end-to-end.**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-07-25
- **Tasks:** 3/3 completed
- **Files modified:** 9 (0 new, 9 modified)

## Accomplishments
- `Repo.UpdateProgress`/`Repo.SetSourceDuration`: plain, non-locking `UPDATE`s that never call `transition()`, never `SELECT ... FOR UPDATE`, and never append to `job_events` (D-03b) — proven by a DB test asserting the `job_events` row count is unchanged across both writes.
- The monotonic + active-only guard is enforced entirely in SQL (`WHERE status='active' AND (progress_pct IS NULL OR progress_pct <= $new)`); a stale/late tick or a write against a non-active job is a self-conditioned no-op (0 rows affected, `nil` error), never an error path.
- `Job` struct gained `ProgressPct`/`EtaSeconds`/`SourceDurationSeconds`, and `Repo.Get` now scans all three columns.
- The worker's `runConvertWithOptionalProgress` type-asserts `conv.(convert.ProgressConverter)` and, only when `SetProgressThrottle` has configured throttling, drives a throttled `progressSink` through `ConvertWithProgress`; every other engine (and av itself when throttling is unconfigured) falls through to the exact same `conv.Convert` call as before D-01.
- `progressSink` applies the dual gate (`>= minInterval` AND `>= minDeltaPct`) to every `Update`, and its constructor/collaborators depend on a narrow `progressRepo` interface rather than the concrete `*jobs.Repo`, making the whole throttle/bypass/freeze contract unit-testable with a spy.
- A successful `ConvertWithProgress` return triggers an unconditional terminal 100% write bypassing the throttle (D-03a); an error return writes nothing further, leaving the last throttled value frozen (D-06) — proven explicitly with `TestProgressFreezeOnAbort`'s spy repo, not only asserted by code inspection.
- `PROGRESS_MIN_INTERVAL_SECONDS` (default 2s) / `PROGRESS_MIN_DELTA_PCT` (default 1) are read in `cmd/av-worker/main.go` and wired via `Handler.SetProgressThrottle` before `srv.Start(mux)` — the same happens-before convention as `AV_DISK_SAFETY_FACTOR`. Every other worker cmd never calls this setter, so `progressMinInterval` stays at its zero value and their engines are byte-unaffected.
- `GET /v1/jobs/{id}` now surfaces `progress_pct`/`eta_seconds` null-aware for every job, regardless of status: real integers for an in-flight/finished av job, a frozen last value for a canceled/failed-mid-stream job, and a genuine JSON `null` (never a fabricated number) for every non-progress engine.

## Task Commits

Each task followed RED -> GREEN:

1. **Task 1: Non-locking UpdateProgress + SetSourceDuration + Job progress fields + Get scan**
   - `1a0fe3d` test(41-03): add failing tests for non-locking UpdateProgress/SetSourceDuration
   - `780c59d` feat(41-03): implement non-locking UpdateProgress + SetSourceDuration (PROG-02/03)
2. **Task 2: Worker type-assert seam + throttle sink + terminal-100 bypass + freeze + env knobs**
   - `a06d439` test(41-03): add failing tests for progress throttle/terminal-bypass/freeze-on-abort
   - `0916510` feat(41-03): implement worker progress seam + dual-gate throttle (PROG-03)
3. **Task 3: Surface progress_pct/eta_seconds null-aware on GET /v1/jobs/{id}**
   - `27161ff` test(41-03): add failing tests for null-aware progress_pct/eta_seconds surfacing
   - `b6ca803` feat(41-03): surface progress_pct/eta_seconds null-aware on GET /v1/jobs/{id}

_No REFACTOR commit needed — all three GREEN implementations landed clean on first pass._

**Plan metadata:** committed separately below (docs commit)

## Files Created/Modified
- `internal/jobs/repo.go` - `UpdateProgress`/`SetSourceDuration` (non-locking, monotonic, write-once); `Get`'s SELECT/Scan extended with the three progress columns
- `internal/jobs/jobs.go` - `Job.ProgressPct`/`EtaSeconds`/`SourceDurationSeconds` pointer fields
- `internal/jobs/repo_test.go` - `TestUpdateProgress`, `TestUpdateProgressInactiveJobIsNoop`, `TestSetSourceDuration` (+ `countJobEvents`/`intPtr` helpers)
- `internal/worker/worker.go` - `progressRepo` interface, `progressSink`, `runConvertWithOptionalProgress` seam function, `Handler.progressMinInterval`/`progressMinDeltaPct` fields + `SetProgressThrottle` setter, `process()`'s convert closure rewired through the seam
- `internal/worker/worker_test.go` - `spyProgressRepo`, `fakeProgressConverter`, `fakeNonProgressConverter`, `TestProgressThrottle` (3 subtests), `TestProgressFreezeOnAbort`
- `internal/api/handlers.go` - `handleGetJob` surfaces `progress_pct`/`eta_seconds` from the loaded job
- `internal/api/handlers_test.go` - `TestGetJob_ProgressSurfaced`, `TestGetJob_ProgressNullForNonProgressEngine`, `TestGetJob_ProgressFrozenOnCancel`
- `cmd/av-worker/main.go` - reads `PROGRESS_MIN_INTERVAL_SECONDS`/`PROGRESS_MIN_DELTA_PCT`, calls `h.SetProgressThrottle(...)` before `srv.Start(mux)`
- `.env.example` - documents both new knobs alongside the `AV_*` block

## Decisions Made
- Progress writes bypass `transition()` entirely (D-03b): denormalized telemetry, not an audited lifecycle change — accepted per the plan's threat register (T-41-10, "accept" disposition).
- Terminal-100 write issued directly against `repo` from the seam function (not through the throttled sink), immediately after a successful `ConvertWithProgress` and strictly before `MarkDone` — makes D-03a's guarantee structural rather than timing-dependent.
- `progressRepo` narrow interface (rather than depending on concrete `*jobs.Repo`) purely to unlock spy-based unit tests of the throttle/bypass/freeze contract without a live Postgres pool — mirrors the interface-segregation pattern already used in `internal/api/api.go`.
- `handleGetJob` sets `resp["progress_pct"] = job.ProgressPct` (a `*int`) directly, relying on `encoding/json`'s standard nil-pointer-to-null behavior, instead of an explicit if/else branch — simpler and equally correct.

## Deviations from Plan

None — plan executed exactly as written. All acceptance criteria (grep-verified: no `FOR UPDATE`/`job_events` tokens in the new repo methods, `SetProgressThrottle` wired before `srv.Start`, `NewHandler`'s signature unchanged, both env knobs documented in both files) were met on the first implementation pass.

## Issues Encountered
None.

## Requirements Status
`requirements-completed: [PROG-01, PROG-02, PROG-03, PROG-04]` — all four are now genuinely closed end-to-end: the ramp computed in Plans 01/02 is durable (PROG-02's `source_duration_seconds`), non-contending (PROG-03's throttled non-locking write), and client-visible (PROG-01/04's null-aware `GET /v1/jobs/{id}` surfacing). PROG-05 (the live SIGKILL-mid-stream process-table + goroutine gate) remains open for Plan 04, as scoped.

## Known Stubs
None. Every method/field touched in this plan is fully wired end-to-end (repo write path -> worker seam -> API surfacing); nothing here is a placeholder. The worker's progress seam only fires for jobs whose converter implements `convert.ProgressConverter` (currently `AVConverter` alone) AND whose `Handler` has `SetProgressThrottle` configured (currently `av-worker` alone) — this is the documented, intentional scope boundary from D-01, not an oversight.

## Threat Flags
None beyond what the plan's own `<threat_model>` already covered (T-41-07/08/09/10) — no new network endpoints, auth paths, or schema changes were introduced by this plan; the progress fields were added strictly inside `handleGetJob`'s existing ownership guard.

## Next Phase Readiness
- PROG-01 through PROG-04 are closed; the durable progress/ETA data Phase 42's SSE/webhook push (PUSH-01/02) will consume is now genuinely written and readable.
- Plan 04's live gate (process-table + goroutine inspection against the real docker-compose stack, PROG-05) is the only remaining item in this phase.

## Self-Check: PASSED

- FOUND: internal/jobs/repo.go
- FOUND: internal/jobs/jobs.go
- FOUND: internal/jobs/repo_test.go
- FOUND: internal/worker/worker.go
- FOUND: internal/worker/worker_test.go
- FOUND: internal/api/handlers.go
- FOUND: internal/api/handlers_test.go
- FOUND: cmd/av-worker/main.go
- FOUND: .env.example
- FOUND: commit 1a0fe3d
- FOUND: commit 780c59d
- FOUND: commit a06d439
- FOUND: commit 0916510
- FOUND: commit 27161ff
- FOUND: commit b6ca803

---
*Phase: 41-progress-core*
*Completed: 2026-07-25*
