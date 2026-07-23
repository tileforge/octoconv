# Feature Research

**Domain:** Job progress, observability, and control for an async file-conversion API (v1.9 "Job Progress & Control")
**Researched:** 2026-07-24
**Confidence:** MEDIUM (patterns verified against AWS MediaConvert docs, asynq's own API/wiki, ffmpeg `-progress` docs, and established REST/SSE practice; no single vendor runs an identical "presigned-download-or-webhook conversion service" so specifics are synthesized, not copy-pasted from one source)

This file scopes the five target features from `.planning/PROJECT.md` v1.9: progress %, ETA, live-push (webhook `progress` event and/or SSE), `GET /v1/jobs` listing, and `POST /v1/jobs/{id}/cancel`. It assumes the existing v1.0-v1.8 contour (Postgres-first double write, guarded status transitions with append-only `job_events`, engine-class asynq queues, hardened `runCommand` process-group kill, HMAC-signed webhook delivery with bounded retry) as fixed and asks only how the new capabilities should sit on top of it.

**Note:** this replaces the FEATURES.md previously written for the v1.8 (AV engine) milestone research — that content is preserved in git history (`git log -p -- .planning/research/FEATURES.md`) but no longer reflects the current milestone's scope.

## Feature Landscape

### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| `progress_pct` field on `GET /v1/jobs/{id}` | Any async job API with jobs running longer than a few seconds (AWS MediaConvert, GitHub Actions runs, CI systems) reports *some* percent-complete once a job is `active`; a bare `queued/active/done` tri-state feels broken for a 12-minute video transcode | MEDIUM | 0-100 integer, `null` when indeterminate (see below) — never a fabricated number |
| Indeterminate/`null` progress for engines that can't report it | libvips/LibreOffice/Chromium conversions don't expose a native progress stream; MediaConvert itself only reports percent for phases it can actually measure (probing/transcoding/uploading), not for every job type | LOW | Table stakes is *honesty*, not universal coverage — `null` is a valid, expected value, not a bug |
| `GET /v1/jobs` listing scoped to caller's own jobs | Every multi-tenant job API (Stripe, GitHub Actions, MediaConvert `ListJobs`) needs a way to see "what did I submit" beyond single-job polling by ID, once a client has more than a handful of in-flight jobs | MEDIUM | Must reuse the existing 404-not-403 ownership discipline — hard `WHERE client_id = $1`, never a client-suppliable override |
| Status/engine filter + pagination on the listing | Any list endpoint over an unbounded, ever-growing table needs filters and a page-size cap or it becomes a full-table-scan DoS vector the moment client job volume grows | MEDIUM | Bounded default + max page size enforced server-side regardless of client-requested `limit` |
| `POST /v1/jobs/{id}/cancel` | Long video/audio jobs are exactly the case where "I submitted this by mistake / conditions changed" matters; MediaConvert, ComfyUI, and virtually every long-running-job API expose a cancel endpoint | MEDIUM-HIGH | The hard part isn't the endpoint, it's the state-machine race (queued/active/already-terminal) and actually killing the child process — both addressed below |
| Idempotent cancel | Calling cancel twice, or cancelling a job that finished microseconds earlier, must not surface as a client-visible error | LOW-MEDIUM | Precedent: Omni's cancel-AI-job endpoint returns success + current state on an already-terminal job rather than erroring; ArcGIS's stricter "error if already terminal" model exists but is worse UX for exactly this race |
| ETA field, omitted/`null` when not derivable | Once percent is known, "how much longer" is the very next question every user asks; MediaConvert's own job-progress docs pair phase percent with elapsed/remaining framing | MEDIUM | Must be an *honest* estimate or `null` — never a guess dressed up as precision (see Anti-Features) |

### Differentiators (Competitive Advantage)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Live progress push — webhook `progress` event | Removes polling entirely for callers already using `callback_url`; matches the "push over poll" philosophy the project already committed to for terminal results (v1.0 webhook delivery) | MEDIUM-HIGH | Needs an `event` discriminator added to the webhook envelope (today's payload has no `event` field — it's implicitly always terminal) so existing integrations aren't silently broken by an unexpected mid-job POST |
| Live progress push — SSE stream (`GET /v1/jobs/{id}/events` or similar) | For a caller not using webhooks (no public callback endpoint, e.g. a CLI/MCP client polling interactively), SSE gives sub-second updates without hammering the REST endpoint | MEDIUM-HIGH | Internal-service clients aren't limited by the browser `EventSource` API's no-custom-headers restriction — plain `Authorization: ApiKey ...` works fine over a raw HTTP SSE connection. Flag as a gap only for the bundled reference React frontend (`frontend/`), which *would* hit that browser limitation if it consumed SSE directly |
| Stage-based sub-progress (`downloading` / `converting` / `uploading` / `probing`) alongside the percent | MediaConvert's own model is phase + percent-within-phase, not a single flat number — useful for document/av pipelines with a fixed-cost probe stage before the variable-cost transcode | MEDIUM | Optional enrichment; percent-of-total remains the primary field, stage is a string annotation, not a replacement |
| ffmpeg-driven speed-aware ETA (audio/av engines) | ffmpeg's own `-progress` output already emits a `speed` field (encode rate vs. realtime); combined with the already-probed `source_duration`, ETA becomes `(source_duration - out_time) / speed` instead of a naive linear elapsed/percent extrapolation | MEDIUM | Only available for audio/av; image/document/html get elapsed-based ETA at best, or `null` |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|------------------|-------------|
| Per-frame or sub-second progress webhook delivery | "More updates = more real-time" | ffmpeg's `-progress` pipe emits a block roughly every frame/second by default — forwarding every tick to a webhook (through the existing HMAC-sign + bounded-retry + `webhook_deliveries` row-per-attempt machinery) would multiply write/HTTP load by 100-1000x per job and defeat the whole point of the retry/dead-letter design, which is built for a handful of terminal attempts, not a firehose | Throttle: emit at most every N seconds (e.g. 5s) *and* only on a meaningful percent delta (e.g. ≥5%); for webhook delivery specifically, use a **separate, best-effort, non-retried, non-dead-lettered** dispatch path — a stale/late progress event is not worth retrying, unlike a terminal one |
| Progress webhook events reusing the exact terminal-webhook retry/dead-letter contract | Reuse existing, already-hardened code | Retrying a `progress=42%` delivery from 3 minutes ago after the job has already moved past that point is actively misleading, and burning the same `MaxRetry=6` bounded-backoff budget on stale telemetry starves/delays the one delivery that actually matters (the terminal `done`/`failed` event) | Fire-and-forget single attempt for `progress`; only `done`/`failed`/`canceled` keep the durable retry+dead-letter contract |
| Fabricated/simulated progress for engines that can't report it (fake linear ramp based on elapsed time) | A "spinning" percent looks more polished than `null` | Actively dishonest — a libvips image conversion that takes 200ms either finishes before anyone reads the percent, or a fake ramp lies about being "60% done" with no basis; violates the project's fail-closed / no-fabrication posture used everywhere else in the codebase (magic-bytes over extension, measured RTF over guessed timeouts) | Return `progress_pct: null` (indeterminate) for engines without a native signal; the client already handles `active` with no percent today |
| Unauthenticated SSE endpoint (common shortcut because browsers' native `EventSource` can't send an `Authorization` header) | "Just let the browser connect directly with a token in the query string" | Query-string tokens land in access logs, proxy logs, and browser history — a real credential leak vector, and this is an *internal* service where every other `/v1/*` route already requires the API-key header; carving out one unauthenticated exception undermines the whole auth model | Require the same `Authorization: ApiKey` header as every other route for SSE too; if a browser client needs it, that's a frontend-side problem (short-lived signed stream token, or a polyfill/backend proxy that attaches the header) — not a reason to weaken the API |
| Unbounded `GET /v1/jobs` listing (no `limit` cap, "just give me everything") | Convenience for a client that "just wants all its jobs" | A client with years of job history requesting the full unfiltered set turns a paginated list endpoint into an unbounded table scan — the same DoS shape the project already guards against elsewhere (`MAX_UPLOAD_BYTES`, `MAX_IMAGE_PIXELS`, opts-size caps) | Server-enforced max page size regardless of client-requested `limit`; cursor-based pagination so deep pages stay index-friendly instead of degrading like offset pagination does |
| Auto-resuming or auto-retrying a canceled job | "The user might have cancelled by mistake, let them un-cancel" | `canceled` must be a true terminal state (already reserved in the `jobs.status` CHECK constraint) — an "un-cancel" path reopens exactly the kind of ambiguous state-machine edge case the guarded-transition design exists to prevent, and risks resurrecting a job whose partial output was never meant to be trusted | Terminal means terminal: a client that wants the conversion redone submits a brand-new job |
| Silently overwriting a completed job's terminal state when a stale cancel request arrives | Simpler code: "just set status=canceled" unconditionally | If cancel races with completion (job finishes microseconds before the cancel request is processed), blindly overwriting `done`→`canceled` destroys a legitimately-produced, storable result and lies to the client about what happened | Guarded transition: cancel only succeeds as a state change from `{queued, active}`; if the current status is already terminal (`done`/`failed`/`canceled`), respond `200` with the **current** terminal status unchanged (idempotent no-op), never overwrite it |

## Feature Dependencies

```
Persisted source_duration (ffprobe, per-job, stored not transient)
    └──requires-for──> Progress % (av/audio engines only — numerator/denominator)
                           └──enables──> ETA (elapsed + percent, or ffmpeg `speed` + remaining duration)
                                            └──feeds──> Live push: webhook `progress` event
                                            └──feeds──> Live push: SSE stream
                                            └──feeds──> GET /v1/jobs listing (progress_pct column)

Existing hardened runCommand (Setpgid + SIGKILL on timeout)
    └──reused-by──> Cancellation in-flight kill (same kill path, triggered by
                     context cancellation instead of a timeout)

Existing guarded-transition machinery (Repo.transition, job_events append log)
    └──extended-by──> Cancellation state transition ({queued,active} -> canceled)
                          └──must-not-conflict-with──> Worker's terminal MarkDone/MarkFailed
                                                        (loses the race harmlessly: an illegal-
                                                        transition error on an already-canceled
                                                        row is treated as terminal/non-retryable,
                                                        same as any other guarded-transition failure)

Existing webhook envelope (single implicit terminal payload, no `event` field)
    └──must-add──> `event` discriminator field BEFORE `progress` events can be introduced
                     (existing integrations assume one POST per job = "it's done")

GET /v1/jobs listing ──independent-of── Progress %, ETA, Cancellation
    (can ship standalone; progress_pct is just an extra column once it exists)
```

### Dependency Notes

- **Progress % requires persisted `source_duration`:** today `ProbeDuration`/ffprobe already runs transiently inside `Convert()` for audio/av (`internal/convert/audioduration.go`, `internal/convert/av.go`) but the value isn't stored on the `jobs` row. Percent-of-total for those two engine classes needs a durable `jobs.source_duration_seconds` (or equivalent) populated once probing happens, before percent can be computed against elapsed `out_time`.
- **ETA enhances Progress %, doesn't replace it:** a client can use percent alone; ETA is strictly additional and must degrade to `null` independently of percent being present (e.g., percent known but speed not yet stabilized in the first few seconds of a transcode).
- **Cancellation reuses, not duplicates, the process-kill contract:** the existing `runCommand` wrapper already kills the whole process group on `ctx` cancellation/timeout (`internal/convert/exec.go`). Cancellation's job is to get a cancellation signal into that same `ctx` — via asynq's `Inspector.CancelProcessing(taskID)`, which is documented as best-effort (it signals; it doesn't guarantee the task dies), so the job row's guarded transition to `canceled` is the authoritative outcome regardless of whether the kill signal actually lands before the process finishes on its own.
- **The webhook envelope needs an `event` field before `progress` can be added:** conflicts with silent reuse. Today's webhook payload (`internal/worker/worker.go`) is `{"job_id":..., "status":...}` with no discriminator, sent exactly once per job on terminal completion. Adding a `progress` event either (a) requires every existing webhook consumer to start switching on a new `event` field it's never seen before, or (b) should be opt-in per job (e.g., only sent if the client explicitly requests progress events at job-creation time), so existing integrations see zero behavior change unless they ask for it.
- **Job listing conflicts with nothing:** it's the one feature in this milestone with no hard dependency on the others — it can ship first or in parallel, and simply gains a `progress_pct`/`progress_stage` column once those exist.

## Concrete Contracts

### `GET /v1/jobs/{id}` — additions

```json
{
  "job_id": "5b1e...",
  "status": "active",
  "engine": "av",
  "progress_pct": 42,
  "progress_stage": "converting",
  "eta_seconds": 87,
  "source_format": "mov",
  "target_format": "mp4"
}
```

- `progress_pct`: integer 0-100, or `null` when the engine can't report (image/document/html today) or the job hasn't started (`queued`).
- `progress_stage`: optional short string (`probing`/`downloading`/`converting`/`uploading`); omit the key entirely rather than send `null` if unused.
- `eta_seconds`: integer, or `null` when not yet derivable (job just started, no stable `speed` signal yet) or not supported for this engine. Round to a coarse unit (nearest 5-10s) — false precision ("87.342s remaining") is worse than an honest coarse estimate.
- Terminal jobs (`done`/`failed`/`canceled`) omit `progress_stage`/`eta_seconds`; `progress_pct` may be `100` for `done`, otherwise omit or leave at last-known value.

### Progress source of truth: DB field, not `job_events`

`job_events` is (and should stay) an append-only audit log of discrete **state transitions** — one row per `queued→active`, `active→done`, reconciler recovery, etc. Progress is high-frequency telemetry (potentially one update per second for a 750s video job), not a state transition. Writing every tick to `job_events` would:
- Blow up the event log's size and defeat its purpose as a readable audit trail (it becomes 99% progress noise, 1% signal)
- Require rewriting the "one row per meaningful change" assumption baked into `RecoveryCount`'s `job_events`-scan pattern

Recommendation: add `jobs.progress_pct SMALLINT NULL`, `jobs.progress_stage TEXT NULL`, and `jobs.source_duration_seconds NUMERIC NULL` columns, updated via a **plain `UPDATE`** (not the guarded-transition machinery — this is not a status change and must not lock/serialize against it), **throttled** at the write call site: only write when the percent has moved by a minimum delta (e.g. ≥1%) or a minimum interval has elapsed (e.g. every 2-5s), whichever comes first. This keeps write volume bounded to ~1 UPDATE per throttle-interval per active job, independent of how chatty the underlying ffmpeg `-progress` pipe is.

### ffmpeg `-progress` — the actual signal for audio/av engines

`ffmpeg -progress pipe:1` (or a file) emits newline-delimited `key=value` blocks, each terminated by `progress=continue` or `progress=end`:

```
frame=120
fps=30.00
bitrate=1024.5kbits/s
total_size=524288
out_time_us=4000000
out_time=00:00:04.000000
dup_frames=0
drop_frames=0
speed=1.2x
progress=continue
```

Relevant fields for this milestone: `out_time_us` (position into the output so far) divided by the persisted `source_duration` gives percent; `speed` (encode rate vs. realtime) combined with remaining duration gives ETA (`(source_duration - out_time) / speed`). This requires piping/parsing this stream from the already-spawned ffmpeg child inside the worker **without disturbing the existing process-group-kill contract** — the progress reader must be a side channel (extra pipe/fd or a `-progress <path>` temp file polled by a ticker), not something that changes how the child is spawned, waited on, or killed.

### Live push — webhook `progress` event

- New envelope shape (breaking-compatible addition): `{"event": "progress", "job_id": ..., "status": "active", "progress_pct": 42, "eta_seconds": 87, "timestamp": "..."}`. The existing terminal payload should also gain the same `event` field (`"event": "done"` / `"event": "failed"` / `"event": "canceled"`) for consistency, once any consumer is expected to branch on it.
- Cadence: throttled identically to the DB write (percent-delta and/or time-interval gated) — never per-tick.
- Delivery contract: **separate from** the existing `webhook_deliveries` bounded-retry/dead-letter table. Single best-effort attempt, no retry, no dead-letter row, same HMAC signing. A dropped progress webhook is not worth chasing; the client can always fall back to polling `GET /v1/jobs/{id}`.
- Opt-in: only sent for jobs that request it (e.g., a `progress_webhooks=true` flag alongside `callback_url` at job creation), so existing `callback_url`-only integrations never see an unexpected extra POST shape.

### Live push — SSE stream

- Endpoint shape: `GET /v1/jobs/{id}/events`, `Content-Type: text/event-stream`, same `Authorization: ApiKey` auth as every other route, same 404-not-403 ownership check before the stream opens.
- Payload per event: same JSON shape as the webhook `progress` event, framed as an SSE `data:` line; a final event with `status: done|failed|canceled` (and the download URL when applicable) closes the stream server-side.
- Cadence: identical throttle as the DB write — the API server polls the DB row on a ticker (e.g. every 1-2s) and relays a change, rather than the worker holding an HTTP connection open (workers don't currently serve HTTP; keep that boundary intact). This also naturally caps SSE update frequency to the DB write throttle.
- Backpressure/lifetime: bound the connection (e.g., close and require reconnect after N minutes or on job completion) and cap concurrent open streams per client to prevent a client holding hundreds of long-lived connections open as a resource-exhaustion vector — mirrors the project's existing per-client rate-limiting posture.
- Heartbeat: send periodic SSE comment lines (`:\n\n`) to keep intermediate proxies/load balancers from timing out an idle-looking-but-alive connection.

### `GET /v1/jobs` listing

- Fields per row: `job_id`, `status`, `engine`, `source_format`, `target_format`, `progress_pct` (nullable), `created_at`, `finished_at` (nullable). Omit `download_url`/opts/error detail from the list view — those stay on the single-job `GET /v1/jobs/{id}` to keep list rows small.
- Filters: `?status=active` (repeatable or comma-separated for OR), `?engine=av`. Both are indexed-column filters, whitelisted (never pass raw client strings into a dynamic WHERE clause — reuses the same "closed allowlist, never client-controlled SQL shape" discipline already used for opts validation).
- Ordering: default `created_at DESC` (most recent first); no client-specified arbitrary sort column (keeps the query plan predictable and avoids a sortable-column injection surface).
- Pagination: cursor/keyset-based on `(created_at, id)`, not offset — offset pagination degrades under concurrent inserts (a job created between two page requests shifts every subsequent offset, and this table gets new rows constantly). Opaque cursor token, default page size (e.g. 50), server-enforced max (e.g. 200) regardless of requested `limit`.
- Scoping: hard `WHERE client_id = $1` bound to the authenticated caller — never accept a client-suppliable `client_id` query param. Consistent with the existing `GET /v1/jobs/{id}` ownership model: a job that isn't yours simply never appears, with zero distinguishable behavior from "you have no jobs matching this filter."

### `POST /v1/jobs/{id}/cancel`

- Guarded transition: succeeds (`{queued, active} → canceled`) exactly like every other status change, through the same row-locking `Repo.transition` helper, with a `job_events` row appended (cancellation *is* a meaningful state transition, unlike routine progress ticks — it belongs in the audit log).
- Idempotency: if the job is already `canceled`, respond `200` with the current status unchanged. If the job is already `done` or `failed` (raced past cancellation), respond `200` with the current (unmodified) terminal status — never overwrite a completed/failed result, never surface this race as a client-visible error.
- Ownership: cancelling a job that doesn't belong to the caller (or doesn't exist) → `404`, identical to `GET /v1/jobs/{id}`'s existing not-found/not-yours collapse.
- In-flight kill: for an `active` job, the API's transition to `canceled` also signals the worker to actually stop the child process — via asynq's `Inspector.CancelProcessing(taskID)` (best-effort; requires the job's asynq task ID to be resolvable from the job row) cancelling the handler's `context.Context`, which the existing hardened `runCommand` wrapper already turns into a process-group SIGKILL on cancellation (no new kill logic needed — reuse the timeout path's exact mechanism, just triggered by a different signal).
- Race with natural completion: if the worker's `MarkDone`/`MarkFailed` call loses the race (job already flipped to `canceled` by the time it runs), the guarded-transition allow-list rejects it as an illegal transition — treated exactly like any other illegal-transition error today (terminal, non-retryable, logged, no retry loop).
- Partial-output cleanup: a canceled job must never expose a `download_url`. The worker checks the job's status before its final S3 upload/`MarkDone` and skips the upload entirely if the job has already moved to `canceled` (defense in depth beyond the transition guard); any object that *did* get written before the check fires is cleaned up by the existing TTL-based lifecycle rule, not a bespoke delete-on-cancel path — no new storage-cleanup mechanism required.
- Client-visible result of a canceled job: `GET /v1/jobs/{id}` returns `status: "canceled"`, no `download_url`, no `error_code`/`error_message` (those fields are reserved for engine failures, not user-initiated stops) — a distinct, honest third terminal outcome alongside `done`/`failed`.

## MVP Definition

### Launch With (v1)

- [ ] `progress_pct` (nullable) + `eta_seconds` (nullable) on `GET /v1/jobs/{id}`, DB-field source of truth, throttled writes — the core deliverable (SEED-008); everything else in this milestone is secondary to making long-running jobs visible at all
- [ ] Persisted `source_duration` for audio/av engines (dependency for percent/ETA on those two classes; other engines stay indeterminate)
- [ ] `GET /v1/jobs` listing: own-jobs-only, status/engine filters, cursor pagination with a server-enforced max page size — independent value, ships regardless of progress-push complexity
- [ ] `POST /v1/jobs/{id}/cancel`: idempotent, guarded transition to `canceled`, in-flight kill via existing `runCommand` process-group kill triggered through asynq `CancelProcessing`, no partial-output leakage

### Add After Validation (v1.x)

- [ ] Webhook `progress` event (opt-in, throttled, best-effort non-retried delivery, new `event` discriminator) — once the polling-based percent/ETA contract is proven correct, push it to clients already using `callback_url`
- [ ] SSE streaming endpoint — once the DB-field progress model is stable, expose it as a live stream for clients not using webhooks (CLI/MCP-style interactive polling replacement)

### Future Consideration (v2+)

- [ ] Stage-based sub-progress breakdown surfaced in a UI (the reference `frontend/`) — defer until the raw percent/stage fields are validated against real jobs
- [ ] Batch cancel (`POST /v1/jobs/cancel` with a filter, mirroring the batch-cancel pattern some job APIs offer) — defer until single-job cancel semantics are proven correct under the race conditions above
- [ ] `event`-discriminated webhook payloads for *all* event types (not just adding `progress`) — a broader webhook-contract-versioning conversation, bigger than this milestone

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Progress % (DB field, poll-only) | HIGH | MEDIUM | P1 |
| ETA | MEDIUM | MEDIUM | P1 |
| `GET /v1/jobs` listing | HIGH | MEDIUM | P1 |
| `POST /v1/jobs/{id}/cancel` | HIGH | MEDIUM-HIGH | P1 |
| Webhook `progress` event | MEDIUM | MEDIUM-HIGH | P2 |
| SSE streaming endpoint | MEDIUM | MEDIUM-HIGH | P2 |
| Stage-based sub-progress in UI | LOW-MEDIUM | LOW | P3 |
| Batch cancel | LOW | MEDIUM | P3 |

**Priority key:**
- P1: Must have for this milestone (v1.9)
- P2: Should have, add once P1 is validated against real jobs
- P3: Nice to have, future consideration

## Competitor / Precedent Feature Analysis

| Feature | AWS MediaConvert | asynq (queue layer OctoConv already runs on) | Our Approach |
|---------|-------------------|-----------------------------------------------|--------------|
| Progress reporting | Phase + percent-within-phase via polling or EventBridge `STATUS_UPDATE` events (only emitted if a job runs >1 minute) | No native job-level percent concept — asynq tracks task state (pending/active/retry/archived), not user-defined progress | Flat 0-100 + optional `progress_stage`, `null` when the engine can't measure it — simpler than MediaConvert's per-phase breakdown, matches OctoConv's engine-class model |
| Cancellation | Job-level cancel API on `PROGRESSING`/`SUBMITTED` jobs | `Inspector.CancelProcessing(taskID)` — best-effort signal via `ctx` cancellation, `DeleteTask` for not-yet-active tasks (errors if task is already active) | Combine both asynq primitives: `DeleteTask` for queued-not-dequeued, `CancelProcessing` for active — but the *authoritative* outcome is always the Postgres guarded transition, since asynq's own cancellation is explicitly best-effort |
| Push vs poll | EventBridge push (opt-in) alongside polling `GetJob` | N/A (task queue has no external notification concept) | Both, mirroring MediaConvert's opt-in-push-alongside-poll model: DB-field percent for polling (table stakes), webhook/SSE push as an addition (differentiator) |
| List/filter jobs | `ListJobs` with status filter | `ListTasks`/`Inspector` queue introspection (operator-facing, not client-facing) | `GET /v1/jobs`, client-facing, scoped to caller — closer to MediaConvert's `ListJobs` shape than to asynq's operator-facing inspector |

## Sources

- [FFmpeg `-progress` output format and key/value meaning (ffmpeg-user mailing list)](https://ffmpeg.org/pipermail/ffmpeg-user/2016-July/032897.html)
- [ffmpeg Documentation (`-progress` option)](https://www.ffmpeg.org/ffmpeg.html)
- [Monitoring MediaConvert job progress (AWS docs)](https://docs.aws.amazon.com/mediaconvert/latest/ug/how-mediaconvert-jobs-progress.html)
- [Monitoring AWS Elemental MediaConvert overview](https://docs.aws.amazon.com/mediaconvert/latest/ug/monitoring-overview.html)
- [asynq `inspector.go` — `CancelProcessing`/`DeleteTask` semantics](https://github.com/hibiken/asynq/blob/master/inspector.go)
- [asynq Wiki: Task Timeout and Cancelation](https://github.com/hibiken/asynq/wiki/Task-Timeout-and-Cancelation)
- [Cancel AI job (Omni Docs) — idempotent cancel-on-terminal-job precedent](https://docs.omni.co/api/ai/cancel-ai-job)
- [Cancel a job (ArcGIS REST) — stricter error-on-terminal-job precedent](https://gis.charlottenc.gov/arcgis/help/en/rest/services-reference/enterprise/cancel-gp-job)
- [EventSource / SSE custom-header limitation and workarounds](https://www.tutorialpedia.org/blog/http-authorization-header-in-eventsource-server-sent-events/)
- [REST API Pagination: cursor vs. offset best practices](https://www.speakeasy.com/api-design/pagination)
- [Broken Object Level Authorization — always scope list queries to the authenticated caller](https://how2.sh/posts/how-to-fix-broken-object-level-authorization-in-apis/)
- Project files read directly: `.planning/PROJECT.md`, `README.md`, `internal/api/handlers.go`, `internal/jobs/jobs.go`, `internal/jobs/repo.go`, `internal/db/migrations/0001_init.sql`, `internal/convert/audioduration.go`, `internal/convert/av.go`, `internal/worker/worker.go`

---
*Feature research for: OctoConv v1.9 Job Progress & Control*
*Researched: 2026-07-24*
