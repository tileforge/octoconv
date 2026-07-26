---
phase: 39-cancellation-end-to-end
plan: 01
subsystem: api
tags: [go, chi, pgx, cancellation, guarded-transition, rest-api]

# Dependency graph
requires:
  - phase: 39-cancellation-end-to-end
    provides: locked D-01/D-02/D-09/D-08 decisions (39-CONTEXT.md)
provides:
  - "Repo.Cancel: guarded {queued,active}->canceled transition, stamps finished_at, reuses transition()"
  - "POST /v1/jobs/{id}/cancel: 404-not-403 ownership, idempotent terminal short-circuit, terminal-race handling, always 200"
  - "Nil-safe CancelPublisher seam (api.CancelPublisher, Server.SetCancelPublisher) for Plan 03 to wire the real Redis pub/sub publisher"
affects: [39-02-worker-cancel-wiring, 39-03-cancel-pubsub]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cancel as a thin wrapper on the existing guarded transition() helper -- zero new transition machinery"
    - "404-not-403 ownership check mirrored verbatim from handleGetJob"
    - "Idempotent terminal short-circuit before calling a mutating repo method (check job.Status before Cancel, not after)"
    - "Nil-safe optional-dependency seam: unexported field + exported setter, NewServer signature untouched"

key-files:
  created: []
  modified:
    - internal/jobs/repo.go
    - internal/jobs/repo_test.go
    - internal/api/handlers.go
    - internal/api/routes.go
    - internal/api/api.go
    - internal/api/handlers_test.go

key-decisions:
  - "Cancel reuses transition() exactly like MarkDone/MarkFailed -- no ad-hoc UPDATE, no new transition machinery (D-01)"
  - "Terminal-race classification is by re-read + isTerminalStatus, never by string-matching the illegal-transition error text"
  - "CancelPublisher is wired via a post-construction setter (SetCancelPublisher), not a NewServer parameter, so existing callers (handlers_test.go, cmd/api/main.go) stay unchanged until Plan 03"

patterns-established:
  - "Idempotent mutation endpoints: check terminal/already-applied state BEFORE calling the mutating repo method, and treat the mutating method's rejection as a race to re-read, not a client-facing error"

requirements-completed: [CNCL-01, CNCL-02]

# Metrics
duration: 25min
completed: 2026-07-24
---

# Phase 39 Plan 01: Cancel Transition + API Summary

**DB-authoritative `POST /v1/jobs/{id}/cancel` backed by a guarded `{queued,active}->canceled` Postgres transition, with 404-not-403 ownership and always-200 idempotent/race-safe responses.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-07-24T15:44:14Z
- **Tasks:** 2 completed
- **Files modified:** 6

## Accomplishments
- `Repo.Cancel(ctx, id)` added to `internal/jobs/repo.go` as a thin call into the existing guarded `transition()` helper (`{queued,active} -> canceled`), stamping `finished_at` exactly like `MarkDone`/`MarkFailed`. Rejects done/failed/canceled jobs via the allow-list (illegal-transition error) and returns `jobs.ErrNotFound` for a nonexistent job.
- `POST /v1/jobs/{id}/cancel` wired end-to-end: route registered in `routes.go`, `handleCancelJob` in `handlers.go` mirrors `handleGetJob`'s 404-not-403 ownership check verbatim, is idempotent on an already-terminal job (200 + current status, no `Cancel` call), and handles the terminal-race (re-read + 200 + now-current status, never 409/500).
- Nil-safe `CancelPublisher` seam added to `api.go` (`CancelPublisher` interface + `Server.cancelPublisher` field + `SetCancelPublisher` setter) so Plan 03 can inject the real Redis pub/sub publisher without touching `handleCancelJob` again. `NewServer`'s signature is unchanged.
- Full test coverage for both layers: DB-integration tests for `Repo.Cancel` (queued, active, done-rejected, not-found) and HTTP-level tests for `handleCancelJob` (queued/active success, cross-client 404, unknown-id 404, invalid-id 400, already-terminal idempotency, terminal-race, no-auth 401, publisher-seam firing).

## Task Commits

Each task was committed atomically:

1. **Task 1: Repo.Cancel via the guarded transition** - `0f2dfe4` (feat)
2. **Task 2: handleCancelJob + route + nil-safe publisher seam** - `29800c6` (feat)

**Plan metadata:** (this commit) `docs(39-01): complete cancel-transition-and-api plan`

## Files Created/Modified
- `internal/jobs/repo.go` - Added `Repo.Cancel`, reusing `transition()`
- `internal/jobs/repo_test.go` - Added `TestCancelQueued`, `TestCancelActive`, `TestCancelDoneRejected`, `TestCancelNotFound`
- `internal/api/handlers.go` - Added `handleCancelJob` and `isTerminalStatus` helper
- `internal/api/routes.go` - Registered `POST /jobs/{id}/cancel`
- `internal/api/api.go` - Added `Cancel` to the `Repo` interface, `CancelPublisher` interface, `Server.cancelPublisher` field + `SetCancelPublisher` setter
- `internal/api/handlers_test.go` - Extended `fakeRepo` with `Cancel`/`cancelErr`/`canceled`/`getJobAfterCancel`; added `fakeCancelPublisher`; added 9 cancel-path tests

## Decisions Made
- Followed the plan and 39-CONTEXT.md's locked decisions (D-01, D-02, D-09, D-08) exactly: no new transition machinery, 404-not-403 ownership, idempotent terminal short-circuit, terminal-race re-read, always-200 response, no webhook on cancel.
- Terminal-race detection re-reads via `s.repo.Get` and checks `isTerminalStatus` on the result, rather than string-matching the "illegal transition" error text — more robust and matches the plan's explicit instruction to never string-match.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `Repo.Cancel` and the cancel route are the correctness layer this milestone's Plans 02/03 build on: Plan 02 wires the worker's `context.WithCancel` + cancel-classification around `Convert`, Plan 03 implements the real `internal/cancel` Redis pub/sub package and calls `Server.SetCancelPublisher` to plug it into the seam left here.
- No blockers. `go build ./...`, `go vet ./internal/jobs/ ./internal/api/`, and `go test ./... ` (full suite, DB-backed) are all green.

---
*Phase: 39-cancellation-end-to-end*
*Completed: 2026-07-24*

## Self-Check: PASSED

All created/modified files found on disk; both task commits (`0f2dfe4`, `29800c6`) verified present in `git log`.
