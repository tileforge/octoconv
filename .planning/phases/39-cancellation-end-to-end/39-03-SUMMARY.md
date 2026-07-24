---
phase: 39-cancellation-end-to-end
plan: 03
subsystem: worker
tags: [go, asynq, redis, pubsub, cancellation, worker]

# Dependency graph
requires:
  - phase: 39-cancellation-end-to-end
    provides: "Repo.Cancel + POST /v1/jobs/{id}/cancel + nil-safe CancelPublisher seam (Plan 01)"
  - phase: 39-cancellation-end-to-end
    provides: "internal/cancel Registry + Publisher/Subscriber over job:cancel (Plan 02)"
provides:
  - "process() registers/deregisters the per-attempt CancelFunc in cancel.Default keyed by job.ID around the whole download->convert->upload window"
  - "isCanceled(err) classifier + a context.Canceled arm in all five Handle*Convert handlers: SkipRetry, no MarkFailed"
  - "cmd/api publishes job:cancel via the real cancel.Publisher (SetCancelPublisher)"
  - "every engine worker (image/document/html/audio/av) runs a cancel.Subscriber against cancel.Default; webhook-worker intentionally unwired"
affects: [39-04-live-verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Register-before-Convert, Deregister-on-return via defer -- mirrors the existing attemptCtx/defer cancelFn() pairing already in process()"
    - "isCanceled(err) checked as the FIRST branch in every Handle*Convert, before the engine-scoped terminal classifier (isTerminal/isDocumentTerminal/isHTMLTerminal/isAudioTerminal/isAVTerminal)"
    - "Every engine worker cmd builds its own dedicated go-redis client for the Subscriber (mirrors cmd/api's existing /healthz-pinger client pattern), run in a goroutine bound to the process signal ctx"

key-files:
  created: []
  modified:
    - internal/worker/worker.go
    - internal/worker/worker_test.go
    - cmd/api/main.go
    - cmd/worker/main.go
    - cmd/audio-worker/main.go
    - cmd/av-worker/main.go
    - cmd/chromium-worker/main.go
    - cmd/document-worker/main.go

key-decisions:
  - "Renamed process()'s context.WithTimeout CancelFunc from `cancel` to `cancelFn` to avoid shadowing the new internal/cancel package import -- a pure rename, no behavior change to the existing defer cancelFn() timeout-cancellation."
  - "isCanceled is a package-level helper (errors.Is(err, context.Canceled)), checked identically and independently in all five handlers rather than folded into isTerminal/isDocumentTerminal/etc, so the cancel-vs-timeout distinction (context.Canceled vs context.DeadlineExceeded) stays a single, obviously-correct assertion instead of being threaded through five already-divergent terminal classifiers."
  - "Reused the existing MarkActive-rejection SkipRetry path unchanged (D-07): a queued-then-canceled job is dropped there, before any download/convert work, with no code change needed this plan."
  - "cmd/api reuses its existing rdb (already built for the /healthz pinger) rather than opening a second Redis connection for the publisher -- one client, two responsibilities, same REDIS_ADDR."
  - "Each of the five engine worker cmds opens ITS OWN dedicated go-redis client for the Subscriber (not shared with anything else in that process, since none of them already had one) -- mirrors cmd/api's client-construction shape exactly for consistency, deferred Close() alongside the other worker-lifecycle resources."

patterns-established:
  - "context.Canceled vs context.DeadlineExceeded as two structurally distinct, never-conflated classification arms checked in a fixed order (canceled first, terminal second, transient fallthrough last) across every engine handler."

requirements-completed: []

# Metrics
duration: 15min
completed: 2026-07-24
---

# Phase 39 Plan 03: Worker Cancellation Wiring Summary

**Wires the internal/cancel registry and Redis pub/sub (built in isolation by Plan 02) into the live worker/API processes: process() registers a per-attempt CancelFunc before every Convert call, all five engine handlers SkipRetry (never MarkFailed) on a context.Canceled kill, cmd/api publishes job:cancel, and every engine worker subscribes -- activating the low-latency in-flight kill mechanism end to end.**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-07-24T19:00:30+03:00
- **Tasks:** 3 completed
- **Files modified:** 8

## Accomplishments
- `internal/worker/worker.go`'s `process()` now registers the `context.WithTimeout` `CancelFunc` (renamed `cancel` -> `cancelFn` to avoid shadowing the new `cancel` package import) in `cancel.Default` keyed by `job.ID` immediately after the timeout is derived and BEFORE any download/convert/upload work, with a paired `defer cancel.Default.Deregister(job.ID)`. Calling that `CancelFunc` early (from the Subscriber, on a received `job:cancel`) cancels `attemptCtx` -> `runCommand`'s existing `ctx.Done()` SIGKILL-on-process-group path fires completely unchanged (`internal/convert/exec.go` untouched, verified via an empty `git diff --stat`) -> `attemptCtx.Err()` surfaces as `context.Canceled`, distinct from the timeout's `context.DeadlineExceeded`.
- New package-level `isCanceled(err error) bool` (`errors.Is(err, context.Canceled)`) is checked as the FIRST branch -- before the engine-scoped terminal classifier -- inside all five `Handle*Convert` functions (`HandleImageConvert`, `HandleDocumentConvert`, `HandleHTMLConvert`, `HandleAudioConvert`, `HandleAVConvert`). On a cancel-killed attempt each handler returns `fmt.Errorf("%w: canceled", asynq.SkipRetry)` WITHOUT calling `MarkFailed` (the job is already `canceled` in Postgres via the API's guarded transition; `MarkFailed` would be rejected by the allow-list anyway, D-05) and records no job-outcome metric (mirrors the existing transient branch's no-double-count comment). The pre-existing `MarkActive`-rejection SkipRetry path (queued-then-canceled dequeue-and-drop, D-07) was left untouched -- it already classifies correctly.
- `cmd/api/main.go` now injects a real `*cancel.Publisher` via `srv.SetCancelPublisher(cancel.NewPublisher(rdb))`, reusing the SAME `rdb` client already constructed for the `/healthz` pinger (no second Redis connection). Plan 01's `handleCancelJob` already called the (previously nil) publisher best-effort, so this single line activates the speed layer with zero handler changes.
- All five engine-worker binaries (`cmd/worker` = image, `cmd/document-worker`, `cmd/chromium-worker` = html, `cmd/audio-worker`, `cmd/av-worker`) now build a dedicated go-redis client from `redisOpt.Addr` and run `cancel.NewSubscriber(rdb, cancel.Default).Run(ctx)` in a goroutine bound to the process's `signal.NotifyContext` ctx, with `defer rdb.Close()`. `cmd/webhook-worker` runs no engine process and was intentionally left unwired (D-04's explicit scope boundary) -- confirmed via `grep -rL 'cancel.NewSubscriber' cmd/webhook-worker`.
- `internal/worker/worker_test.go` gained `TestIsCanceled`, pinning the classifier against a bare `context.Canceled`, a doubly-wrapped `context.Canceled` (mirroring the real `exec.go`/`process()` wrapping shape), `context.DeadlineExceeded` (must be false -- the distinct timeout arm), a plain engine error, and `nil`.

## Task Commits

Each task was committed atomically:

1. **Task 1: worker registration + context.Canceled SkipRetry arm** - `6d3ecaa` (feat)
2. **Task 2: API publisher wiring (cmd/api)** - `db4d600` (feat)
3. **Task 3: cancel subscriber in every engine worker** - `d03726e` (feat)

**Plan metadata:** (this commit) `docs(39-03): complete worker-cancel-wiring plan`

## Files Created/Modified
- `internal/worker/worker.go` - `process()` registers/deregisters the per-attempt CancelFunc; new `isCanceled` helper; a `context.Canceled` SkipRetry arm in all five `Handle*Convert` handlers
- `internal/worker/worker_test.go` - `TestIsCanceled`
- `cmd/api/main.go` - `srv.SetCancelPublisher(cancel.NewPublisher(rdb))`
- `cmd/worker/main.go`, `cmd/audio-worker/main.go`, `cmd/av-worker/main.go`, `cmd/chromium-worker/main.go`, `cmd/document-worker/main.go` - each runs `cancel.NewSubscriber(rdb, cancel.Default).Run(ctx)` in a goroutine against a dedicated go-redis client

## Decisions Made
- Followed the plan and 39-CONTEXT.md's locked decisions (D-04, D-05, D-06, D-07) exactly: register/deregister around the whole attempt, reuse `runCommand` unchanged, `context.Canceled` -> SkipRetry with no MarkFailed, dequeue-and-drop only (no `DeleteTask`, confirmed via `grep -n 'DeleteTask' internal/worker/worker.go` returning nothing).
- `isCanceled` is intentionally NOT folded into the existing `isTerminal`/`isDocumentTerminal`/`isHTMLTerminal`/`isAudioTerminal`/`isAVTerminal` classifiers -- it is a structurally distinct condition (cancel vs. terminal-vs-transient) checked as its own first branch in every handler, keeping the two concerns from ever being conflated inside an already-divergent per-engine classifier.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None. `go build ./...`, `go vet ./internal/worker/ ./cmd/...`, and the full `go test ./...` suite are all green; `git diff --stat internal/convert/exec.go` is empty (the reuse invariant holds); `grep -c 'isCanceled(err)' internal/worker/worker.go` is exactly 5.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- The mechanism is now fully wired: a `POST /v1/jobs/{id}/cancel` on an `active` job publishes over Redis, the owning worker's Subscriber cancels the exact registered `CancelFunc`, `runCommand` SIGKILLs the real engine process group, and the handler returns SkipRetry without resurrecting the job.
- CNCL-03 (this plan's assigned requirement) is intentionally left incomplete in `REQUIREMENTS.md`/not marked complete here -- Plan 04 owns the live proof (a real long-running engine job canceled mid-flight, verified via the process table that the child is actually gone) and the non-resurrection regression tests. No blockers for Plan 04.

---
*Phase: 39-cancellation-end-to-end*
*Completed: 2026-07-24*

## Self-Check: PASSED

All modified files found on disk; all three task commits (`6d3ecaa`, `db4d600`, `d03726e`) verified present in `git log`.
