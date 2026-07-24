# Phase 39: Cancellation (end-to-end) - Context

**Gathered:** 2026-07-24
**Status:** Ready for planning

<domain>
## Phase Boundary

A client can cancel a `queued` or `active` conversion job via `POST /v1/jobs/{id}/cancel`, and can trust that (a) the job's terminal state becomes `canceled` authoritatively, (b) a real in-flight engine process is actually killed, and (c) nothing (reconciler, asynq retry) ever resurrects it. This is the first behavioral layer of v1.9 and the first cross-worker signalling primitive (`internal/cancel` Redis pub/sub) — deliberately built before Progress (Phase 41) so that Redis-pub/sub pattern is validated in isolation.

**In scope:** `Repo.Cancel`, the `POST /v1/jobs/{id}/cancel` handler, a new `internal/cancel` package (in-memory cancel-func registry + Redis pub/sub `job:cancel`), worker `context.WithCancel` wiring + cancel-classification, reconciler/asynq `canceled`-terminal awareness with a regression test, and live verification that the real engine process dies.
**Out of scope:** progress/ETA (Phases 40/41), the listing endpoint (Phase 40), a webhook on cancel (deferred — see D-08), batch cancel (v2), any change to the hardened `exec.go` kill path (it already does the kill).
</domain>

<decisions>
## Implementation Decisions

### Cancel transition + API (CNCL-01, CNCL-02)
- **D-01:** `Repo.Cancel(ctx, id)` reuses the EXISTING guarded `transition()` helper (`internal/jobs/repo.go:415`): `transition(ctx, id, StatusCanceled, []string{StatusQueued, StatusActive}, detail, updateFn)` — SELECT…FOR UPDATE the row, reject if not in `{queued,active}`, UPDATE to `canceled`, append a `job_events` row (from_status→canceled). Stamp `finished_at` in the update closure (mirrors `MarkDone`/`MarkFailed`). No new transition machinery.
- **D-02:** `POST /v1/jobs/{id}/cancel` — ownership check identical to `handleGetJob` (a job owned by another client or not found → `404`, never `403`). Idempotent: if the job is already terminal (`done`/`failed`/`canceled`), return `200` with the job's current status and do NOT re-transition. On a successful cancel, return `200` with `status: canceled`. If `Repo.Cancel` returns the "illegal transition" error due to a race (the job reached a terminal state between the read and the transition), re-read and return `200` with the now-current status — never a 409/500 for that race.
- **D-09:** Response is `200` (not 202) with the current status; the process kill is asynchronous/best-effort and the handler does NOT block waiting for process death. CNCL-03's "the process is actually dead" is proven by tests/live verification, not inside the request path.

### Cancel-signal transport (the phase's central design)
- **D-03:** Custom Redis pub/sub — NOT asynq's `Inspector.CancelProcessing` (which needs the asynq-assigned task ID our tasks don't set to job_id, and carries the unfixed asynq#987 retry-on-cancel race). New `internal/cancel` package:
  - a per-process in-memory registry `map[uuid.UUID]context.CancelFunc` (mutex-guarded) — records the CancelFunc for each job THIS worker is actively processing;
  - a publisher (api side) `PUBLISH job:cancel <job_id>` on cancel;
  - a subscriber (every worker binary) that, on a received job_id, looks up the registry and calls the CancelFunc if present (this worker is running that job) — a no-op on workers not running it.
  - Reuses the EXISTING Redis instance and the already-present `github.com/redis/go-redis/v9` client (asynq transitive dep; `queue.RedisOpt()`). ZERO new infra, ZERO new Go dependency (preserves research's "0 new deps").
- **D-04:** Worker wiring — the shared handler path wraps the per-attempt ctx in `context.WithCancel`, registers `(job_id → cancelFunc)` in the `internal/cancel` registry BEFORE calling `Convert`, and deregisters on return (`defer`). No `exec.go` change: `runCommand` already SIGKILLs the whole process group on `ctx.Done()` (`internal/convert/exec.go:45-49`), so cancelling this ctx reuses the load-bearing hardened kill path unchanged. Wired into EVERY worker binary (image/document/html/audio/av) since they share `runCommand`; av/audio are where it matters most.

### Correctness backstop + non-resurrection (CNCL-04)
- **D-05:** The guarded-transition allow-list IS the authoritative correctness backstop (verified already in code): after a job is `canceled`, `MarkActive` (`{queued,active}→active`), `MarkDone` (`{active}→done`), and `MarkFailed` (`{queued,active}→failed`) all reject it because `canceled` is not an allowed source. A worker that MISSES the pub/sub signal and runs to completion will have its terminal `Mark*` rejected → the job stays `canceled`. This is the sole guaranteed mechanism; the pub/sub is only a latency optimization.
- **D-07:** Cancel-classification in the worker — when the process is killed by cancel, `runCommand` returns `ctx.Err()` = `context.Canceled` (distinct from `context.DeadlineExceeded`, the existing timeout arm). The worker must, on a `context.Canceled`-wrapped error: NOT call `MarkFailed` (the job is already `canceled`), and return an `asynq.SkipRetry`-wrapped error so asynq does not retry. Similarly, when a queued-then-canceled task is dequeued, `MarkActive`'s rejection must be classified as SkipRetry (mirrors the existing "already active/done" handling in the worker's comments) — no download/convert happens (rejected at `MarkActive`, before any work). This is the **dequeue-and-drop** model.
- **D-06 (queued cancel):** dequeue-and-drop only — do NOT call asynq `DeleteTask`. The rejected `MarkActive` + SkipRetry already drops the task cheaply (before download/convert); `DeleteTask` would add Inspector wiring and a worker-grabs-it-first race for marginal queue-cleanliness benefit.
- **D-10 (reconciler):** `FindStale` already filters `status IN ('queued','active')`, so `canceled` jobs are already excluded from stale-recovery — confirm with an explicit regression test (a `canceled` stale job is NOT requeued). The reconciler's webhook-gap sweep must likewise not attempt delivery for `canceled` jobs.

### Residuals / partial output (locked)
- **D-11 (partial output = lifecycle-TTL):** No proactive S3/temp cleanup on cancel. Output is uploaded only AFTER a full conversion (download→convert→upload), so a mid-convert cancel leaves no output object; the input object is cleaned by the existing MinIO ILM TTL; the worker temp dir is cleaned on process exit. A `canceled` job exposes no result (`status != done` ⇒ no `download_url`).
- **D-12 (missed-signal resilience = accept):** Trust pub/sub + allow-list. On a missed signal the job runs to completion/timeout (bounded by the engine timeout) and its terminal `Mark*` is rejected — wasted compute only, never state corruption. NO periodic worker DB-status re-poll (avoids constant DB load for a rare Redis-disconnect window). Documented as an accepted, bounded residual.

### Claude's Discretion / confirm in planning
- **D-08 (webhook on cancel):** default is NO terminal webhook on cancel (the client that issued the cancel already knows the outcome; the terminal webhook contract is for `done`/`failed`). If planning surfaces a real need (e.g. operator-initiated cancel that a different client should learn about), revisit — but do not add it speculatively in this phase.
- Live-verification shape (CNCL-03): an integration/live test that submits a long-running real engine job (e.g. an av transcode), cancels it mid-flight, and asserts via the process table that the real ffmpeg child is gone (not just that the DB row reads `canceled`) — mirrors the Phase 9 zombie-reaping precedent. `go test -race` alone is insufficient.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The mechanisms cancel reuses (do not duplicate)
- `internal/jobs/repo.go` — the `transition()` helper (:415, SELECT…FOR UPDATE + allow-list + job_events) that `Repo.Cancel` reuses; and the `MarkActive`/`MarkDone`/`MarkFailed` allow-lists (:155-178) that ARE the non-resurrection backstop.
- `internal/jobs/jobs.go` — `StatusCanceled` const (:19; already exists).
- `internal/convert/exec.go` — `runCommand` (:28) with the `Setpgid`+`SIGKILL`-on-`ctx.Done()` contract (:40-49); the cancel ctx plugs into this unchanged.
- `internal/worker/worker.go` — the shared handler loop, `MarkActive` call sites, `isTerminal`/`SkipRetry` classification (the `context.DeadlineExceeded` timeout arm is the template for the new `context.Canceled` arm).
- `internal/reconciler/reconciler.go` — `FindStale` (`status IN ('queued','active')` already excludes `canceled`); the webhook-gap sweep.
- `internal/api/handlers.go` — `handleGetJob` (the 404-not-403 ownership pattern to mirror for `handleCancelJob`).
- `internal/queue/queue.go` — `RedisOpt()` and the go-redis client the pub/sub reuses.

### Requirements + research
- `.planning/REQUIREMENTS.md` — CNCL-01..04.
- `.planning/research/ARCHITECTURE.md` (§cancellation two-layer design), `.planning/research/PITFALLS.md` (cancellation-races pitfall #4), `.planning/research/STACK.md` (asynq#987 + go-redis already present) — the research consensus this phase implements.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `transition()` guarded helper — `Repo.Cancel` is a thin call, not new machinery.
- `runCommand`'s `ctx.Done()`→SIGKILL — the entire kill mechanism already exists; cancel just needs to cancel the ctx.
- go-redis client via asynq — pub/sub reuses it; no new dependency.
- `FindStale`'s `status IN ('queued','active')` filter — already excludes `canceled`.

### Established Patterns
- Guarded transitions with allow-lists (the correctness backstop) + append-only `job_events`.
- The worker's `isTerminal` + `asynq.SkipRetry` classification (the `context.DeadlineExceeded` arm is the exact template for the new `context.Canceled` arm).
- 404-not-403 ownership (`handleGetJob`) for the new cancel route.

### Integration Points
- `internal/cancel` (new) is imported by BOTH the api binary (publisher) and every worker binary (subscriber + registry). The worker registers/deregisters CancelFuncs around `Convert`; the api publishes on cancel.
</code_context>

<specifics>
## Specific Ideas

The phase's value is a bounded, correct cancel: DB-authoritative `canceled` via the existing guarded transition (correctness), a best-effort Redis pub/sub for low-latency in-flight kill via the existing SIGKILL path (speed), and a live proof the real process actually dies. Keep the pub/sub strictly a speed optimization — never let correctness depend on it. Reuse everything (transition, runCommand kill, go-redis, FindStale filter); the only genuinely new code is the small `internal/cancel` package and the worker registry wiring.
</specifics>

<deferred>
## Deferred Ideas

- **Webhook on cancel** (D-08) — not this phase unless a concrete need surfaces in planning.
- **Batch cancel** — v2 (CNCL-V2-01).
- **asynq DeleteTask for queued cancels** — rejected (D-06); dequeue-and-drop is sufficient.
- **Periodic worker DB-status re-poll** — rejected (D-12); allow-list is the completion-time recheck.
- **Proactive partial-output cleanup** — rejected (D-11); lifecycle TTL handles it.
</deferred>

---

*Phase: 39-cancellation-end-to-end*
*Context gathered: 2026-07-24*
