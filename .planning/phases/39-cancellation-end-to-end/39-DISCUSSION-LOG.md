# Phase 39 Discussion Log

**Date:** 2026-07-24
**Mode:** discuss (default)

## Framing

Cancellation, grounded in a codebase scout that confirmed the research design: `transition()` helper + Mark* allow-lists already provide the non-resurrection backstop; `runCommand` already SIGKILLs the process group on `ctx.Done()`; `StatusCanceled` + status CHECK already exist; `FindStale` already excludes `canceled`. Carried forward without re-asking: guarded `{queued,active}→canceled` transition, 404-not-403 ownership, idempotency, live process-death verification.

## Gray areas presented (all 4 selected for discussion)

**A. Cancel-signal transport** — custom Redis pub/sub vs asynq `CancelProcessing`.
- User asked whether Redis is already in the project. Answered: yes — asynq runs entirely on Redis, and `github.com/redis/go-redis/v9` is already a transitive dependency; a custom `job:cancel` pub/sub reuses both (0 new infra/deps) and avoids asynq#987.
- **Decision:** custom Redis pub/sub `job:cancel` + in-memory `map[jobID]context.CancelFunc` registry (`internal/cancel`).

**B. Queued-job cancel** — dequeue-and-drop vs asynq DeleteTask.
- **Decision:** dequeue-and-drop (rejected `MarkActive` + SkipRetry, before any download/convert). No DeleteTask.

**C. Partial output on cancel** — lifecycle-TTL vs proactive cleanup.
- **Decision:** lifecycle-TTL only. Output is uploaded only after full conversion, so a mid-convert cancel leaves no output; input is TTL-cleaned; canceled exposes no result.

**D. Missed-signal resilience** — pub/sub + allow-list vs periodic DB re-poll.
- **Decision:** pub/sub + allow-list. Missed signal = bounded wasted compute (≤ engine timeout), status stays canceled (Mark* rejected). No periodic DB poll.

## Decisions captured (see 39-CONTEXT.md D-01..D-12)

- Transition/API: D-01 (Repo.Cancel via existing transition), D-02 (404-not-403 + idempotent 200), D-09 (200 not 202, async kill).
- Transport: D-03 (custom pub/sub `internal/cancel`), D-04 (worker ctx.WithCancel registry, reuse runCommand kill).
- Backstop: D-05 (allow-list is correctness), D-07 (context.Canceled → SkipRetry, no MarkFailed), D-06 (dequeue-and-drop), D-10 (FindStale excludes canceled + regression test).
- Residuals: D-11 (lifecycle-TTL), D-12 (accept missed-signal).
- Discretion: D-08 (no webhook on cancel unless a need surfaces).

## Deferred

- Webhook on cancel (D-08), batch cancel (v2), DeleteTask (rejected), periodic DB poll (rejected), proactive S3 cleanup (rejected).
