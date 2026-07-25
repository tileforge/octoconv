# Phase 42: Live Push - Context

**Gathered:** 2026-07-25
**Status:** Ready for planning

<domain>
## Phase Boundary

A client can watch a job's progress in real time — via **SSE** (`GET /v1/jobs/{id}/events`) or an **opt-in progress webhook event** — without polling, and a stale progress delivery NEVER competes with the terminal `done`/`failed` event's durable bounded-retry/dead-letter budget. Pure consumer of the `progress_pct`/`eta_seconds`/`progress_updated_at` columns Phase 41 writes. This is PUSH-01 (SSE) and PUSH-02 (opt-in progress webhook).

**In scope:** the SSE endpoint (auth + 404-ownership, DB-poll-sourced so any api replica serves it, heartbeat, clean close/no goroutine leak); the per-job progress-webhook opt-in flag; the `event` discriminator field on webhook bodies; and a best-effort single-attempt inline progress-webhook delivery path that is structurally separate from the terminal retry queue.

**Out of scope:** WebSocket/long-poll transports (SSE only); progress for engines other than av transcode (Phase 41 already scopes the ramp there); changing the terminal webhook's retry/dead-letter contract; a new OCR/preview/CAD engine (deferred seeds).
</domain>

<decisions>
## Implementation Decisions

### SSE payload + cadence (PUSH-01)
- **D-01:** The SSE stream is **DB-poll-sourced** (server polls the `jobs` row; NO in-memory pub/sub, so any stateless `api` replica can serve any client — a hard constraint from the stateless-API architecture). Poll interval **~1s, env-tunable** (`SSE_POLL_INTERVAL`-style knob). **Emit-on-change only**: an SSE event is sent ONLY when `progress_pct`, `eta_seconds`, or `status` changed since the last emit — no duplicate events. Event payload mirrors `handleGetJob`'s null-aware fields: `{"status":…, "progress_pct":…|null, "eta_seconds":…|null}`. Use a **named SSE event** (`event: progress`). A **heartbeat comment** (`: keepalive\n\n`) every ~15s (env-tunable) to survive proxy idle-timeouts. Rejected: emit-every-poll (chatty duplicates, needless bandwidth).

### SSE lifecycle / termination (PUSH-01)
- **D-02:** On a **terminal status** (`done`/`failed`/`canceled`), emit ONE final event carrying the terminal `{status}` (named e.g. `event: done`/a terminal event), then **close the stream**. The final event does **NOT** include a presigned `download_url` — the client does a normal `GET /v1/jobs/{id}` for it. Rationale: presigning N objects per stream is expensive and couples the (otherwise storage-free) SSE handler to the storage client; SSE stays cheap and stateless. On **client disconnect** (`r.Context().Done()`), the handler returns cleanly with zero leaked goroutines (the load-bearing PUSH-01 SC — mirrors the Phase 41 leak discipline). A **max-stream-duration safety cap** (~30m, env-tunable) closes a stream whose job never terminates, so a wedged/abandoned stream can't hold a connection+goroutine forever. Rejected: including download_url in the final event (presign cost + storage coupling on the stream); no max-cap (unbounded stream lifetime risk).

### Opt-in + event discriminator (PUSH-02)
- **D-03:** Progress webhooks are **opt-in per job** via a NEW top-level form field alongside `callback_url` (e.g. `progress_webhook=true`) — NOT buried in `opts` (opts is per-engine conversion parameters; delivery configuration is orthogonal). An **`event` discriminator field** is added to **BOTH** webhook bodies — the new progress body (`"event":"progress"`) AND the existing terminal body (`"event":"completed"` or similar). Both events POST to the SAME `callback_url`, so the client MUST be able to tell them apart — the discriminator on both is required, not optional. The field is **additive** (existing clients that ignore unknown JSON keys are unaffected — backward-compat-safe). A job with no `callback_url`, or with `progress_webhook` absent/false, sends NO progress webhooks. Rejected: opt-in inside opts (conflates delivery with engine params); event only on the progress body (a client on a shared callback_url can't discriminate the terminal event); a separate `progress_callback_url` (a second URL to SSRF-validate + more client config for no real gain).

### Progress-webhook delivery path (PUSH-02 — the load-bearing separation)
- **D-04:** The worker fires progress webhooks **INLINE, best-effort, single HTTP attempt, drop-on-failure**, from the SAME progress sink that writes progress to the DB. It reuses the **same PROG-03 throttle gate** (≥2s AND ≥1% delta) that already gates the DB progress write — one throttle decision drives both the DB write and the webhook fire. It is HMAC-signed with the existing `sign` scheme. It fires ONLY when the job (a) opted in (D-03) AND (b) is on the ramping path (av transcode, per Phase 41 D-02). It **NEVER** enqueues to `webhook_deliveries` / the asynq webhook retry queue — the separation from the terminal `done`/`failed` bounded-retry/dead-letter budget is guaranteed **by construction** (there is simply no enqueue path). A progress-webhook write, like the DB progress write, must NEVER fail or block the conversion (best-effort, short timeout, error ignored — mirrors the existing best-effort-enqueue discipline). Rejected: a separate no-retry asynq queue (new queue/consumer for a fire-and-forget signal, and risks accidentally wiring dead-letter); reusing the webhook worker with MaxRetry=0 (shares the dead-letter code path — risk of accidental budget mixing, the exact thing PUSH-02 forbids).

### Claude's Discretion
- Exact SSE event names (`progress`/`done` vs a single `event` field carrying the discriminator), the exact `event` string values (`"progress"`/`"completed"`/`"done"`), the concrete env-var names (`SSE_POLL_INTERVAL`/`SSE_HEARTBEAT_INTERVAL`/`SSE_MAX_STREAM_DURATION`), the per-attempt HTTP timeout for the inline progress webhook, and whether the terminal SSE event reuses the `progress` event name or a distinct one — all executor/planner's call, provided: DB-poll-sourced + emit-on-change (D-01), zero goroutine leak on disconnect/terminal/cap (D-02), the discriminator appears on BOTH bodies (D-03), and the progress-webhook path never touches the retry/dead-letter queue (D-04).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The progress source (Phase 41) — what SSE/webhook read
- `internal/jobs/repo.go` — `Repo.Get` / the `jobs` row carrying `progress_pct`/`eta_seconds`/`progress_updated_at`/`source_duration_seconds` (Phase 41 Plan 03); the non-locking read the SSE poll loop reuses.
- `internal/jobs/jobs.go` — `Job.ProgressPct`/`EtaSeconds` (`*int`, null-aware) + `Status*` consts (terminal set for SSE close).
- `internal/worker/worker.go` — the Phase 41 `progressSink` / `runConvertWithOptionalProgress` / throttle gate (`SetProgressThrottle`) that D-04's inline progress webhook hooks into; the type-assert seam and terminal-100/freeze branches.
- `internal/api/handlers.go` — `handleGetJob` (:664, the null-aware `progress_pct`/`eta_seconds` rendering + the 404-not-403 ownership guard the SSE endpoint mirrors exactly).

### The webhook infrastructure (reused + extended)
- `internal/webhook/webhook.go` (package doc, `Delivery` row shape), `internal/webhook/deliver.go` (`Deliverer.Deliver` — single signed HTTP attempt, 2xx=success), `internal/webhook/sign.go` (HMAC `X-OctoConv-Signature`/`X-OctoConv-Timestamp` scheme the progress webhook reuses), `internal/webhook/repo.go` (the `webhook_deliveries` retry/dead-letter table D-04 must NOT touch for progress).
- `internal/queue/queue.go` — `TypeWebhookDeliver`/`QueueWebhook`, `NewWebhookDeliverTask` (`MaxRetry=6`, backoff 30s→15m, `asynq.Unique`), `WebhookPayload` — the TERMINAL durable path; the terminal body is where the `event:"completed"` discriminator (D-03) is added.
- The worker/handler that builds the TERMINAL webhook JSON body (grep for the marshalled `{job_id,status,…}` payload posted to `callback_url`) — where the `event` discriminator field is added additively.
- The `callback_url` SSRF validation + `formFieldCallbackURL`/`formFieldOpts` handling in `internal/api/handlers.go` (create path) — where the new `progress_webhook` opt-in form flag is parsed/validated; the same SSRF guard covers the progress delivery (same URL).

### Routing + config
- `internal/api/routes.go` — the auth'd `/v1` group where `GET /v1/jobs/{id}/events` registers (alongside `GET /jobs/{id}` and `POST /jobs/{id}/cancel`).
- `cmd/api/main.go` — `os.Getenv`/`envInt`/`envDurationSeconds` wiring for the `SSE_*` knobs; `.env.example` documents them.

### Requirements + prior context
- `.planning/REQUIREMENTS.md` — PUSH-01, PUSH-02.
- `.planning/phases/41-progress-core/41-CONTEXT.md` — the progress columns + throttle gate this phase consumes; D-02 (ramp only on av transcode) scopes which jobs get progress webhooks.
- `.planning/phases/39-cancellation-end-to-end/` — the SSE close-on-`canceled` case reuses the Phase 39 cancel path; the goroutine-leak live discipline (cancel-liveproof / 41-04) is the template for any SSE leak proof.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- Phase 41's `progress_pct`/`eta_seconds` columns + `Repo.Get` — the SSE poll and the progress-webhook body both read them directly; no new read method likely needed.
- Phase 41's throttle gate (≥2s AND ≥1%) — D-04 reuses the SAME decision to gate the inline progress webhook (no second throttle).
- `internal/webhook/sign.go` HMAC signing + `deliver.go` single-attempt POST — the progress webhook signs/POSTs with the identical scheme (just never enqueues to the retry table).
- `handleGetJob`'s 404-not-403 ownership guard + `auth.ClientFromContext` — the SSE endpoint mirrors it verbatim (a cross-client `/events` request is 404).

### Established Patterns
- `os.Getenv`-only config → the `SSE_*` knobs.
- Best-effort telemetry writes never fail the conversion (Phase 41 progress-write discipline) → the inline progress webhook is best-effort/error-ignored the same way.
- Guarded-transition is the ONLY lifecycle-status path — SSE and progress webhooks are pure READS/telemetry, never a status write.
- `chi` handler + `http.Flusher` for SSE streaming; `r.Context().Done()` for disconnect detection (stdlib, no new dep).

### Integration Points
- `internal/api` (new SSE handler + `progress_webhook` opt-in parse) ← reads `internal/jobs` progress columns; `internal/worker` (inline progress webhook in the Phase 41 sink) ← `internal/webhook` sign/deliver, NEVER `internal/queue` webhook-enqueue for progress. The terminal webhook body (existing) gains an additive `event` field.
</code_context>

<specifics>
## Specific Ideas

- SSE framing: `event: <name>\ndata: <json>\n\n` per emit; `: keepalive\n\n` heartbeat; set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, and flush after every write (`http.Flusher`).
- The zero-goroutine-leak-on-disconnect guarantee is the SSE analog of Phase 41 PROG-05 — worth an explicit test (and the existing goroutine-count discipline from 41-04 is the template if a live proof is wanted).
- The `event` discriminator on the terminal body is additive and backward-compat-safe, but the terminal webhook's signed-body change means the HMAC covers the new field — existing consumers that verify the signature over the raw body are unaffected (they verify whatever bytes arrive).
</specifics>

<deferred>
## Deferred Ideas

- WebSocket / long-poll transports — SSE only for v1.9.
- Progress for non-av engines / a generalized progress bus — out of scope (Phase 41 scopes the ramp to av transcode).
- OCR / archive / CAD / preview engine classes — deferred seeds (SEED-005/006/007 + the OCR idea from Phase 41's discussion), captured after v1.9 closes.
</deferred>

---

*Phase: 42-live-push*
*Context gathered: 2026-07-25*
