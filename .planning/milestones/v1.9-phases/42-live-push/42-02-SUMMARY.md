---
phase: 42-live-push
plan: 02
subsystem: api
tags: [sse, server-sent-events, go-chi, http-flusher, streaming, progress]

requires:
  - phase: 41-progress-core
    provides: "jobs.progress_pct/eta_seconds columns + Repo.Get, the non-locking progress read this stream polls"
provides:
  - "GET /v1/jobs/{id}/events: DB-poll-sourced SSE progress stream, emit-on-change, heartbeat, terminal-close, disconnect-close, max-duration cap"
  - "api.Config SSEPollInterval/SSEHeartbeatInterval/SSEMaxStreamDuration knobs (env: SSE_POLL_INTERVAL/SSE_HEARTBEAT_INTERVAL/SSE_MAX_STREAM_DURATION)"
affects: [42-04-live-gate]

tech-stack:
  added: []
  patterns:
    - "Single-request-goroutine SSE loop (no `go func()` in the handler) -- zero-goroutine-leak by construction, not by join/drain discipline"
    - "http.NewResponseController(w).Flush() for post-write flushing (Go 1.20+ idiom, survives chi middleware's ResponseWriter wrap)"
    - "sseSnapshot: a comparable (==-able) struct with a -1 sentinel for nil *int fields, used to gate emit-on-change without per-field nil-checks at every comparison site"

key-files:
  created:
    - internal/api/sse.go
    - internal/api/sse_test.go
  modified:
    - internal/api/api.go
    - internal/api/routes.go
    - cmd/api/main.go
    - .env.example

key-decisions:
  - "Terminal event reuses the `progress` SSE event name (not a distinct `done` event) -- one event type for clients to listen to; the terminal event's data body is `{\"status\":...}` only, distinguishable by its smaller shape and by isTerminalStatus being true"
  - "On a poll where the snapshot changed AND the new status is terminal, BOTH the change-emit (full payload) and the dedicated terminal-emit ({status} only) are written -- matches 42-RESEARCH.md's Pattern 1 reference implementation exactly; a client sees the final progress_pct/eta_seconds value AND an unambiguous close signal"
  - "SSE unit tests unit-test handleJobEvents directly (not via srv.Routes().ServeHTTP) using a manually-constructed chi RouteContext + auth.WithClient, since routes.go registration is Task 2 -- Task 1's tests must pass before the route exists"

requirements-addressed: [PUSH-01 (SSE implemented + unit-tested; full close deferred to 42-04's live gate)]
requirements-completed: []

duration: ~45min
completed: 2026-07-25
---

# Phase 42 Plan 02: SSE Progress Stream Summary

**Added `GET /v1/jobs/{id}/events`: a DB-poll-sourced, single-goroutine SSE handler that mirrors `handleGetJob`'s auth + 404-ownership guard exactly, emits a named `progress` event only on change, heartbeats every 15s, and closes cleanly on terminal status, client disconnect, or a 30m safety cap -- zero goroutine leak by construction (no `go func()` anywhere in the handler).**

## Performance

- **Duration:** ~45 min
- **Tasks:** 2/2 completed
- **Files modified:** 6 (2 created, 4 modified)

## Accomplishments

- `internal/api/sse.go`: `handleJobEvents` -- parses the id, reuses `s.repo.Get` + the IDENTICAL `job.ClientID != client.ID` → 404 ownership guard as `handleGetJob` (T-39-01/T-42-04 precedent), sets SSE headers, emits the current snapshot immediately on connect, then runs a single `select` loop over a poll ticker (`s.repo.Get` re-read), a heartbeat ticker (`: keepalive\n\n`), `r.Context().Done()` (disconnect), and a max-duration timer (safety cap) -- every branch runs in the request's own goroutine.
- Emit-on-change diffing via `sseSnapshot` (a comparable struct with a `-1` sentinel for nil `*int` progress_pct/eta_seconds) -- exactly one event per genuine change, no duplicate emits when the polled row is unchanged.
- Route `GET /jobs/{id}/events` registered inside the `/v1` group, right after `GET /jobs/{id}`, inheriting the identical `ratelimit.ByIP → auth.Middleware → ratelimit.PerClient` chain as every other route (T-42-05: no unauthenticated path).
- `SSE_POLL_INTERVAL` / `SSE_HEARTBEAT_INTERVAL` / `SSE_MAX_STREAM_DURATION` env knobs (default 1s/15s/30m) wired via `cmd/api/main.go`'s existing `envDuration` helper onto `api.Config`, documented in `.env.example`.
- 8 unit tests in `internal/api/sse_test.go` covering: initial snapshot + emit-on-change, heartbeat, terminal-close-on-connect, terminal-during-poll, disconnect with a 20-cycle goroutine-count-stability proof, max-duration cap, and 404 ownership (cross-client + unknown id).

## Task Commits

Each task was committed atomically:

1. **Task 1: handleJobEvents SSE handler + snapshot diffing + unit tests** - `0339c45` (feat, tdd)
2. **Task 2: Register the /events route + wire SSE env knobs** - `d553704` (feat)

_Note: Task 1 was executed as a single build-then-test-then-commit unit rather than separate RED/GREEN commits -- the handler and its 8-test suite were developed together and both compiled/passed before the first commit; there was no intermediate "tests exist but fail to compile against an unimplemented handler" state worth committing separately, since sse.go and sse_test.go are new files with no prior green baseline to regress from._

## Files Created/Modified

- `internal/api/sse.go` - `handleJobEvents` + `sseSnapshot`/`snapshotOf`/`sseProgressPayload`/`emit`/`emitTerminal` helpers; reuses the existing `isTerminalStatus` (no new terminal-status function)
- `internal/api/sse_test.go` - `sequencedFakeRepo` (a scripted-sequence fake distinct from `handlers_test.go`'s static `fakeRepo`, needed to drive the poll loop across multiple ticks) + 8 `TestSSE_*` tests
- `internal/api/api.go` - `Server`/`Config` gain `ssePollInterval`/`sseHeartbeatInterval`/`sseMaxStreamDuration` (zero-value-triggers-default: 1s/15s/30m, mirrors `PresignTTL`'s existing treatment)
- `internal/api/routes.go` - `r.Get("/jobs/{id}/events", s.handleJobEvents)` inside the `/v1` group
- `cmd/api/main.go` - reads `SSE_POLL_INTERVAL`/`SSE_HEARTBEAT_INTERVAL`/`SSE_MAX_STREAM_DURATION` via `envDuration`, wires onto `api.Config`
- `.env.example` - documents the three new `SSE_*` knobs

## Decisions Made

- Terminal event reuses the `progress` SSE event name (single event type; the smaller `{"status":...}` body plus the caller's own `isTerminalStatus`-style knowledge distinguishes it from a mid-stream progress update). Discretion explicitly left to the executor by 42-CONTEXT.md.
- When a poll's changed snapshot lands on a terminal status in the same tick, both the change-emit (full payload) and the terminal-emit (`{status}` only) are written, in that order, before the handler returns -- this is what 42-RESEARCH.md's Pattern 1 reference code does, and it means a client never misses the final `progress_pct`/`eta_seconds` value just because the job also happened to finish on that same poll.
- Task 1's tests call `s.handleJobEvents` directly (via a manually-built chi `RouteContext` + `auth.WithClient`) rather than through `srv.Routes().ServeHTTP`, because `routes.go` registration is Task 2's job -- the route does not exist yet when Task 1's tests must already be green.

## Deviations from Plan

None - plan executed exactly as written. No architectural changes, no blocking issues requiring Rule 3/4 handling.

## Verification

- `go build ./...` and `go vet ./...` clean (whole repo, not just `internal/api`).
- `go test ./internal/api/... -race` green, including all 8 `TestSSE_*` cases and the full pre-existing handler/route/preset test suite (no regressions).
- `go test ./...` (whole repo) green.
- `gofmt -l` clean on all touched files.
- Connection-pool note (RESEARCH Pitfall 4, non-gating): N concurrent SSE streams × 1 poll/s is architecturally identical to N clients directly polling `GET /v1/jobs/{id}` today -- each poll borrows a `pgxpool` connection only for the duration of one indexed PK `SELECT`, not for the stream's lifetime. No new pool sizing or index work was required or performed.
- Acceptance-criteria greps: `grep -c "go func" internal/api/sse.go` == 0; `grep -n "download_url\|PresignGet" internal/api/sse.go` returns nothing; `grep -c "defer .*\.Stop()" internal/api/sse.go` == 3; `grep -n "isTerminalStatus" internal/api/sse.go` shows reuse (no new helper defined); `grep -c "SSE_" .env.example` == 3.

## Known Stubs

None. The endpoint is fully wired end-to-end: real DB-poll reads, real SSE framing, real route registration, real env-config wiring. No mocked/hardcoded data path exists in the shipped code (only in test fakes, as expected).

## Threat Flags

None beyond what `42-02-PLAN.md`'s own `<threat_model>` (T-42-04/T-42-05/T-42-06) already anticipated and this plan closes:

- T-42-04 (cross-client progress disclosure): closed by the byte-identical `handleGetJob` ownership guard.
- T-42-05 (auth bypass): closed by registering the route inside the `/v1` group, after `auth.Middleware`.
- T-42-06 (DoS via long-lived streams): closed by the existing `ratelimit.ByIP`/`ratelimit.PerClient` budgets (a stream is one request against those) plus the `SSE_MAX_STREAM_DURATION` cap.

## Scope boundary

- PUSH-01 is implemented and unit-tested here (httptest-based), but NOT marked complete in REQUIREMENTS.md — per 42-02-PLAN.md's success criteria, 42-04's live gate (real-socket/proxy-topology proof) closes it.
- NO webhook delivery / `event` discriminator / av-worker wiring in this plan (that's 42-03, PUSH-02).

## Self-Check: PASSED

- `internal/api/sse.go` — FOUND
- `internal/api/sse_test.go` — FOUND
- `0339c45` — FOUND (`git log --oneline --all | grep 0339c45`)
- `d553704` — FOUND (`git log --oneline --all | grep d553704`)
- `grep -n "jobs/{id}/events" internal/api/routes.go` — FOUND
- `grep -n "SSE_POLL_INTERVAL" cmd/api/main.go` — FOUND
- `grep -c "SSE_" .env.example` — 3 (>= 3, PASSED)
