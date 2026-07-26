---
phase: 39-cancellation-end-to-end
plan: 02
subsystem: infra
tags: [go, redis, pubsub, cancellation, go-redis, worker]

# Dependency graph
requires:
  - phase: 39-cancellation-end-to-end
    provides: "Repo.Cancel + POST /v1/jobs/{id}/cancel + nil-safe CancelPublisher seam (Plan 01)"
  - phase: 39-cancellation-end-to-end
    provides: "locked D-03/D-04/D-12 decisions (39-CONTEXT.md)"
provides:
  - "internal/cancel package: mutex-guarded Registry (map[uuid.UUID]context.CancelFunc), Default singleton, Channel const"
  - "Publisher.Publish(ctx, id) satisfying api.CancelPublisher's signature"
  - "Subscriber.Run(ctx) — clean-shutdown Redis pub/sub receive loop that calls Registry.Cancel per received job id"
affects: [39-03-cancel-pubsub-wiring, 39-04-live-verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Registry mirrors convert.Default's process-wide singleton style (var Default = NewRegistry())"
    - "Channel name defined once (const Channel) in cancel.go; Publisher/Subscriber reference it, never a hardcoded literal"
    - "Subscriber.Run selects on ctx.Done() alongside the pubsub Channel() so shutdown is prompt and the subscription is always Close()d"
    - "Unparseable pub/sub payloads are silently skipped (uuid.Parse), never crash the receive loop"

key-files:
  created:
    - internal/cancel/cancel.go
    - internal/cancel/pubsub.go
    - internal/cancel/cancel_test.go
  modified: []

key-decisions:
  - "Reused the existing go-redis client (github.com/redis/go-redis/v9, already a direct dependency via asynq) — zero new Go dependency, go.mod/go.sum untouched"
  - "Registry.Cancel is a lock-then-call pattern (lookup under mutex, invoke CancelFunc outside the lock) so a slow/blocking CancelFunc can never hold the registry mutex"
  - "Round-trip pub/sub test is REDIS_ADDR-gated (skips cleanly without a live Redis), mirroring internal/queue/queue_test.go's existing integration-test skip pattern; verified green against a live Redis instance during execution"

patterns-established:
  - "Best-effort speed layer over an authoritative DB backstop: this package documents in its own package doc comment that it is never load-bearing for correctness (D-05/D-12) — the guarded-transition allow-list (Plan 01) is the sole correctness mechanism"

requirements-completed: []

# Metrics
duration: 20min
completed: 2026-07-24
---

# Phase 39 Plan 02: Cancel Registry + Redis Pub/Sub Summary

**New `internal/cancel` package: a mutex-guarded in-memory cancel-func registry plus a Redis pub/sub `job:cancel` Publisher/Subscriber, reusing the existing go-redis client with zero new dependencies.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-24T15:51:46Z
- **Tasks:** 2 completed
- **Files modified:** 3 (all new)

## Accomplishments
- `internal/cancel/cancel.go`: a mutex-guarded `Registry` wrapping `map[uuid.UUID]context.CancelFunc`, with `NewRegistry()`, a process-wide `Default` singleton (mirroring `convert.Default`'s style), and `Register`/`Deregister`/`Cancel` methods. `Cancel` on an id this process is not running (never registered, deregistered, or owned by a different worker process) is a documented safe no-op (D-04). `const Channel = "job:cancel"` lives here as the single source of truth for the pub/sub channel name.
- `internal/cancel/pubsub.go`: `Publisher` wraps a `*redis.Client` and exposes `Publish(ctx, id uuid.UUID) error` (`rdb.Publish(ctx, Channel, id.String()).Err()`) — its signature satisfies `internal/api`'s `CancelPublisher` interface from Plan 01, so Plan 03 can inject `*cancel.Publisher` directly with no adapter. `Subscriber` wraps a `*redis.Client` + `*Registry` and exposes `Run(ctx context.Context)`: it subscribes to `Channel`, loops selecting on `ctx.Done()` and the subscription's message channel, parses each payload with `uuid.Parse` (silently skipping unparseable payloads per T-39-05), calls `reg.Cancel(id)`, and closes the pub/sub subscription on return — no leaked goroutine or connection (T-39-04).
- `internal/cancel/cancel_test.go`: unit tests for all four Registry behaviors (register-then-cancel fires, unregistered-cancel no-ops, deregister-then-cancel no-ops, `Default` singleton usability) plus a `-race`-clean concurrency test spinning 100 goroutines across distinct and shared ids. A `REDIS_ADDR`-gated integration test (`TestPublishSubscribeRoundTrip`) publishes a real job id over a live Redis `job:cancel` channel and asserts the Subscriber's registered `CancelFunc` fires within 3s, then asserts `Run` returns within 3s of its ctx being canceled.
- Verified `git diff go.mod` shows no changes — `github.com/redis/go-redis/v9` was already a direct dependency in `go.mod` (via asynq's transitive requirement, already promoted to direct by an earlier phase), so this plan adds zero new Go dependencies (the zero-new-deps invariant from research/STACK.md holds).

## Task Commits

Each task was committed atomically:

1. **Task 1: cancel Registry (in-memory cancel-func map)** - `ca834c6` (feat)
2. **Task 2: Publisher + Subscriber over the job:cancel channel** - `b098d9b` (feat)

**Plan metadata:** (this commit) `docs(39-02): complete cancel-registry-and-pubsub plan`

## Files Created/Modified
- `internal/cancel/cancel.go` - `Registry`, `Default` singleton, `Channel` const, `Register`/`Deregister`/`Cancel`
- `internal/cancel/pubsub.go` - `Publisher`/`Subscriber` over the `job:cancel` Redis channel
- `internal/cancel/cancel_test.go` - Registry unit tests (incl. `-race`), `Default` singleton test, `REDIS_ADDR`-gated pub/sub round-trip test

## Decisions Made
- Followed the plan and 39-CONTEXT.md's locked decisions (D-03, D-04) exactly: custom Redis pub/sub (not asynq's `Inspector.CancelProcessing`), reusing the existing go-redis client, in-memory mutex-guarded map, `Publisher.Publish`'s signature matching `api.CancelPublisher` verbatim so Plan 03 needs no adapter.
- `Registry.Cancel` releases the mutex before invoking the stored `CancelFunc` (lock only to read the map) so a `context.CancelFunc` can never be called while holding the registry lock — avoids a theoretical deadlock if a future `CancelFunc` implementation ever touched the registry itself.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None. `go build ./...`, `go vet ./...`, and `go test ./internal/cancel/ -race -count=1` are all green; the `REDIS_ADDR`-gated round-trip test was additionally run live against a reachable local Redis instance and passed (confirming the skip-by-default test actually exercises the real path, not just compiles).

## User Setup Required

None - no external service configuration required. (This plan's own tests need `REDIS_ADDR` to exercise the live round-trip; they skip cleanly without it, matching the existing `internal/queue` integration-test convention.)

## Next Phase Readiness
- `internal/cancel`'s `Registry`, `Default`, `Publisher`, and `Subscriber` are the exact exports Plan 03 wires in: the worker's shared handler path registers/deregisters `CancelFunc`s around `Convert` calls against `cancel.Default`, every worker binary runs a `Subscriber.Run` goroutine against the same `Default` registry, and `cmd/api/main.go` constructs a `*cancel.Publisher` from its existing Redis client and calls `Server.SetCancelPublisher` (the seam Plan 01 left) to plug it in.
- CNCL-03 (this plan's assigned requirement) is intentionally left incomplete in `REQUIREMENTS.md` — this plan only builds the mechanism in isolation; CNCL-03 ("a real in-flight engine process is actually killed") is not satisfiable until Plan 03 wires the registry into the worker's `Convert` call and Plan 04 proves it live against a real engine process. No blockers for Plan 03.

---
*Phase: 39-cancellation-end-to-end*
*Completed: 2026-07-24*

## Self-Check: PASSED

All created files found on disk; both task commits (`ca834c6`, `b098d9b`) verified present in `git log`.
