# Architecture Research — v1.9 Job Progress & Control

**Domain:** Integration of progress/ETA/live-push/listing/cancellation into OctoConv's existing async conversion pipeline
**Researched:** 2026-07-24
**Confidence:** HIGH (derived directly from reading the current codebase — this is an integration design, not greenfield ecosystem research)

## Standard Architecture

### System Overview (current, with v1.9 additions marked `[NEW]`/`[MOD]`)

```
┌───────────────────────────────────────────────────────────────────────────┐
│                              cmd/api (stateless, N replicas)                │
│  ┌────────────┐ ┌────────────┐ ┌───────────────┐ ┌──────────────────────┐  │
│  │POST /v1/jobs│ │GET /v1/jobs│ │GET /v1/jobs/{id}│ │POST /v1/jobs/{id}/cancel│ │
│  │ (existing) │ │/{id}[MOD]  │ │/{id}[MOD progress] │ │[NEW]                │  │
│  └────────────┘ └────────────┘ └───────────────┘ └──────────────────────┘  │
│  ┌────────────────────┐  ┌───────────────────────────────────────────┐    │
│  │GET /v1/jobs [NEW]   │  │GET /v1/jobs/{id}/events (SSE) [NEW]        │    │
│  │listing+pagination   │  │DB-poll ticker per open connection          │    │
│  └────────────────────┘  └───────────────────────────────────────────┘    │
├───────────────────────────────────────────────────────────────────────────┤
│                    Redis (asynq queues — unchanged — PLUS:)                │
│         [NEW] pub/sub channel `job:cancel` (fire-and-forget, fan-out)      │
├───────────────────────────────────────────────────────────────────────────┤
│  engine workers (image/document/html/audio/av-worker, KEDA-scaled)          │
│  ┌───────────────────────────────────────────────────────────────────┐    │
│  │ Handler.process() [MOD]                                            │    │
│  │  attemptCtx := context.WithCancel(context.WithTimeout(...))  [MOD]  │    │
│  │  register(jobID, cancelFunc) in in-memory map            [NEW]     │    │
│  │  if conv implements ProgressReporter [NEW optional iface]:          │    │
│  │      conv.ConvertWithProgress(ctx, in, out, opts, onProgress)       │    │
│  │  else: conv.Convert(ctx, in, out, opts)   (unchanged path)          │    │
│  │  onProgress → throttle (>=1s) → repo.UpdateProgress() [NEW,         │    │
│  │      non-locking, no job_events row]                                │    │
│  │  subscribe once at startup to `job:cancel`; on match, call          │    │
│  │      cancelFunc() → ctx.Done() → existing runCommand SIGKILL path   │    │
│  └───────────────────────────────────────────────────────────────────┘    │
├───────────────────────────────────────────────────────────────────────────┤
│                              Postgres (system of record)                    │
│  jobs [MOD: + progress_pct, + progress_updated_at, + eta_seconds]           │
│  jobs.status already includes 'canceled' in the original CHECK constraint   │
│  job_events (unchanged shape; NO row per progress tick — only real          │
│              transitions, incl. the new active/queued→canceled transition) │
│  webhook_deliveries (unchanged shape; FindWebhookGaps + delivery payload    │
│              switch widened to cover status='canceled' [MOD])              │
├───────────────────────────────────────────────────────────────────────────┤
│  reconciler (unchanged FindStale query — 'canceled' already excluded        │
│              because it only selects status IN ('queued','active'))        │
│  webhook-worker (delivers `progress` event [NEW, optional] the same way     │
│              it delivers `done`/`failed` today — reuses HMAC signing,       │
│              retry/backoff, dead-letter machinery unchanged)                │
└───────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | New/Modified |
|-----------|-----------------|--------------|
| `internal/jobs.Repo.UpdateProgress` | Lightweight, non-transactional `UPDATE jobs SET progress_pct=..., eta_seconds=..., progress_updated_at=now() WHERE id=$1 AND status='active'` — no `FOR UPDATE`, no `job_events` insert | **NEW** |
| `internal/jobs.Repo.Cancel` | Guarded transition `queued/active → canceled` through the existing `transition()` helper (row lock + `job_events` insert) | **NEW** (thin wrapper, reuses existing machinery) |
| `internal/jobs.Repo.List` | Paginated, client-scoped read query (status/engine filters, keyset pagination) | **NEW** |
| `internal/convert.ProgressReporter` | Optional interface a `Converter` may additionally implement to report fractional progress via a callback | **NEW** |
| `internal/convert` (exec.go) | Add a streaming-stdout variant of `runCommand` that lets a caller consume ffmpeg's `-progress pipe:1` lines while still preserving Setpgid+SIGKILL | **MOD** (additive — existing `runCommand` callers unaffected) |
| `internal/convert.AVConverter` / `AudioConverter` | Implement `ProgressReporter` using ffmpeg `-progress pipe:1` (`out_time_us`, `speed`) against the already-probed `source_duration` | **MOD** |
| `internal/worker.Handler.process` | Type-assert `ProgressReporter`, wire throttled `onProgress` → `repo.UpdateProgress`; wrap `attemptCtx` with `context.WithCancel`; register/unregister the cancel func in an in-memory map; subscribe to Redis `job:cancel` | **MOD** |
| `internal/worker` shared `isTerminal` | New top-level check: `errors.Is(err, context.Canceled)` → terminal, `SkipRetry`, but do **NOT** call `MarkFailed` (status is already `canceled`, and the guarded transition's allow-list rejects it anyway as a safety net) | **MOD** |
| `internal/api` new `handleCancelJob` | `POST /v1/jobs/{id}/cancel`: ownership check (mirrors `handleGetJob`'s 404-not-403 discipline) → `repo.Cancel` → publish job id to `job:cancel` | **NEW** |
| `internal/api` new `handleListJobs` | `GET /v1/jobs`: client-scoped list + filters + pagination | **NEW** |
| `internal/api` new `handleJobEvents` (SSE) | `GET /v1/jobs/{id}/events`: DB-poll ticker (1-2s) streaming `progress`/terminal events, closes on terminal status | **NEW** |
| `internal/webhook` payload builder (`HandleWebhookDeliver`) | New `status == StatusCanceled` branch (no `download_url`); new (separate, non-terminal, best-effort) `progress` event type triggered from the worker's throttle point, reusing `Deliverer`/HMAC/retry unchanged | **MOD** |
| `internal/reconciler` `FindWebhookGaps` | Status filter widened from `('done','failed')` to `('done','failed','canceled')` | **MOD** |
| `internal/db/migrations` | New migration: `jobs.progress_pct smallint`, `jobs.eta_seconds int`, `jobs.progress_updated_at timestamptz` (all nullable) | **NEW** |

## Recommended Project Structure (new/changed files only)

```
internal/
├── convert/
│   ├── convert.go          # [MOD] add ProgressReporter interface (optional, additive)
│   ├── exec.go              # [MOD] add runCommandStreaming (parallel to runCommand)
│   ├── av.go                # [MOD] AVConverter implements ProgressReporter
│   └── whisper.go           # [MOD] AudioConverter implements ProgressReporter (ffmpeg normalize stage only — whisper-cli stage stays indeterminate)
├── jobs/
│   ├── repo.go               # [MOD] + UpdateProgress, + Cancel, + List
│   └── jobs.go               # [MOD] + StatusCanceled Go-side const (DB value already existed)
├── cancel/                   # [NEW package] tiny in-memory registry + Redis pub/sub wiring
│   ├── registry.go           # map[uuid.UUID]context.CancelFunc, mutex-guarded
│   └── pubsub.go             # Subscribe(ctx, redisClient, onCancel func(uuid.UUID)), Publish(ctx, redisClient, jobID)
├── worker/
│   └── worker.go             # [MOD] process() wiring, isTerminal context.Canceled branch
├── api/
│   ├── handlers.go           # [MOD] handleGetJob progress fields; [NEW] handleCancelJob, handleListJobs, handleJobEvents
│   └── routes.go             # [MOD] register 3 new routes
├── webhook/
│   └── (worker.go's HandleWebhookDeliver, not a separate package file) # [MOD] canceled + progress payload branches
├── reconciler/
│   └── reconciler.go         # [MOD] FindWebhookGaps status list
└── db/migrations/
    └── 0007_job_progress.sql # [NEW]
```

### Structure Rationale

- **`internal/cancel/` as its own small package** rather than folding pub/sub logic into `internal/worker` or `internal/queue`: it has a single, narrow responsibility (map a job id to a live cancel func, and bridge that to Redis pub/sub) that both the worker binaries (5 engine classes) and the api process need to import from a neutral location — mirrors the existing `internal/metrics` pattern (a small cross-cutting package every binary imports) rather than adding a new axis of coupling between `internal/worker` and `internal/api`.
- **`ProgressReporter` as an *additional*, optional interface**, not a change to `Converter` itself: `Converter`'s 3-method shape (`Pairs`/`Convert`/`Engine`) is depended on by `internal/api` (format-pair validation) and every existing engine (libvips, LibreOffice, Chromium, whisper's transcribe stage). Changing its signature would force every converter to grow a no-op progress callback parameter it can never call — a needless, risky, wide-blast-radius edit for a feature only two of five engine classes can currently support. This mirrors the project's own precedent (Key Decision, v1.2): "document-engine extends the existing Converter/Registry rather than introducing a Handler/Capability/Input/Output/Progress contract" — the same reasoning applies here: extend by addition, not by breaking the existing contract.
- **Migration file numbered `0007_job_progress.sql`**, following the existing `NNNN_description.sql` convention (`0005_html_engine.sql`, `0006_audio_engine.sql`).

## Architectural Patterns

### Pattern 1: Non-locking, self-conditioning progress write (avoids contending on the guarded-transition lock)

**What:** Progress is written via a plain `UPDATE ... WHERE id = $1 AND status = 'active'` with no `SELECT ... FOR UPDATE`, no transaction, and no `job_events` row — deliberately bypassing `Repo.transition()`.

**Why this is safe (and why it must NOT go through `transition()`):**
- `transition()` exists to guard *state-machine* changes (`queued→active`, `active→done`, etc.) and pairs every change with an audit-log (`job_events`) row. Progress is **not** a state change — status stays `active` throughout — so there is nothing for the allow-list guard to validate, and appending a `job_events` row on every throttled tick (every 1-3s for a multi-minute video transcode) would make the append-only audit log balloon by orders of magnitude and pollute `RecoveryCount`'s tag-based counting logic if the detail schema were ever conflated.
- Progress writes are inherently **high-frequency** and **best-effort** (losing one tick has zero correctness impact — the next tick a few seconds later supersedes it). Routing them through `SELECT ... FOR UPDATE` would serialize every progress tick against `MarkActive`/`MarkDone`/`MarkFailed`/the new `Cancel` for the *same row*, and — because Postgres row locks are per-row, not per-column — against each other if a retry ever produced two attempts in flight. That contention buys nothing: correctness for progress does not require it.
- The `WHERE status = 'active'` clause makes the write **self-conditioning**: a late progress tick that arrives after `MarkDone`/`MarkFailed`/`Cancel` has already flipped the row away from `active` silently affects zero rows — no error, no stale resurrection of "50%" on a row that is actually `done`. This is the same "let the WHERE clause do the guarding" idea the codebase already uses elsewhere (e.g., `MarkActive`'s `COALESCE(started_at, now())` for idempotent re-entry), just applied to progress instead of transition validation.
- **Recommended additional guard:** add `AND (progress_pct IS NULL OR progress_pct <= $2)` (monotonic non-regression) to protect against a stale, out-of-order write from an asynq internal retry that started a fresh attempt at 0% racing a previous attempt's in-flight higher-percentage tick. Given the existing image/document/html/audio/av engines all run at `WORKER_CONCURRENCY` per-process concurrency with `asynq.Unique` locks preventing two concurrent tasks for the same job, this race is narrow but not impossible during the brief internal-retry window; the extra clause costs nothing and closes it.

**Trade-offs:** Progress can very rarely be silently dropped (e.g., a Postgres blip mid-tick) with no retry — acceptable, since the next tick a few seconds later corrects it. This must **not** be used for anything that needs durability (that's what `job_events`/`transition()` remain for).

### Pattern 2: Optional-capability interface for progress reporting (`ProgressReporter`)

**What:**
```go
// ProgressUpdate is throttled by the caller (worker), not the engine — an
// engine implementing ProgressReporter should call the callback as often as
// its own progress source allows (e.g. ffmpeg's ~10Hz -progress cadence);
// throttling to a Postgres-write-friendly cadence is process()'s job, not
// the Converter's.
type ProgressUpdate struct {
    Pct        float64        // 0-100
    ETA        *time.Duration // nil when not yet computable (e.g. speed unknown)
}

// ProgressReporter is an OPTIONAL capability a Converter may additionally
// implement. Converters that cannot report progress (libvips, LibreOffice,
// Chromium, whisper-cli's transcribe stage) simply do not implement it —
// process() falls back to the existing conv.Convert(...) call unchanged.
type ProgressReporter interface {
    ConvertWithProgress(ctx context.Context, inPath, outPath string, opts map[string]any, onProgress func(ProgressUpdate)) error
}
```

**When to use:** Any converter whose underlying engine emits a native, parseable progress stream (ffmpeg's `-progress pipe:1`, verified pattern used by third-party tools such as `better-ffmpeg-progress` and documented on ffmpeg-kit's wiki — MEDIUM confidence, no single canonical ffmpeg.org page enumerates every key, but `out_time_us`/`speed`/`progress=continue|end` are consistently reported by every independent implementation surveyed). LibreOffice, Chromium (`--print-to-pdf`), and whisper-cli's transcription stage expose no equivalent machine-readable progress stream — those stay indeterminate for v1.9 (client sees status only, no `progress_pct`, matching the milestone's own stated "indeterminate-фолбэк для остальных движков").

**Worker integration (`process()`):**
```go
if pr, ok := conv.(convert.ProgressReporter); ok {
    lastWrite := time.Now()
    err = pr.ConvertWithProgress(attemptCtx, inPath, outPath, job.Opts, func(u convert.ProgressUpdate) {
        if time.Since(lastWrite) < progressWriteInterval { // e.g. 2s
            return
        }
        lastWrite = time.Now()
        var eta *int
        if u.ETA != nil {
            s := int(u.ETA.Seconds())
            eta = &s
        }
        _ = h.repo.UpdateProgress(attemptCtx, job.ID, int(u.Pct), eta) // best-effort, error discarded — same discipline as webhook enqueue failures elsewhere in this handler
    })
} else {
    err = conv.Convert(attemptCtx, inPath, outPath, job.Opts)
}
```

**Trade-offs:** Two call shapes exist on the hot path (`Convert` vs `ConvertWithProgress`) instead of one — acceptable, mirrors the existing precedent of `enforceAudioGuardBeforeConvert` already splicing engine-specific behavior into `process()` via a closure rather than forcing every engine through one rigid shape.

### Pattern 3: `runCommand` streaming variant (progress source without breaking the hardened Setpgid+SIGKILL contract)

**What:** `internal/convert/exec.go`'s `runCommand` currently wires `cmd.Stdout = &stdout` (a `bytes.Buffer`) and returns the fully-buffered output only after the process exits or is killed. A new `runCommandStreaming` variant keeps every existing safety property (`Setpgid: true`, ctx-driven `syscall.Kill(-pid, SIGKILL)`, stderr captured for diagnostics) but replaces the stdout wiring with an `io.Pipe` (or `cmd.StdoutPipe()`) read line-by-line by a `bufio.Scanner` in a dedicated goroutine, forwarding each parsed `key=value` line to the caller's callback as it arrives — not after the process exits.

**Why not just parse the buffered output after exit:** ffmpeg's `-progress pipe:1` is specifically designed to be consumed *while the process runs* — buffering it defeats the entire purpose (the caller would only learn "0% → 100%" at the moment the process already finished, which is a strictly worse experience than the current indeterminate baseline).

**Constraints preserved:**
- The ctx-cancellation kill path (`select { case <-ctx.Done(): ...SIGKILL... }`) is unchanged — cancellation (both timeout-driven and, new in v1.9, user-cancel-driven) still kills the whole process group exactly as today.
- stderr capture for diagnostics (`terminalVipsSignatures`-style substring classification) is unaffected — only stdout's wiring changes for engines that opt into streaming.

**Trade-offs:** A second, parallel exec path in `exec.go` (`runCommand` and `runCommandStreaming`) rather than one unified function — acceptable and consistent with keeping non-progress-reporting engines' code paths completely untouched (zero regression risk to libvips/LibreOffice/Chromium/whisper's transcribe stage, which are already production-hardened and audited).

### Pattern 4: Cancel signal — fast pub/sub path with a guaranteed correctness backstop

**What:** Cancellation needs to reach *one specific, currently-running* worker process/goroutine out of a horizontally-scaled, KEDA-autoscaled fleet — the API process has no direct handle on which replica (or whether any replica) is running a given job.

**Design (two layers, deliberately redundant):**

1. **Fast path (best-effort, low-latency):** `POST /v1/jobs/{id}/cancel` first performs the guarded DB transition (`queued/active → canceled`, via `Repo.Cancel` → existing `transition()`), then publishes the job id (fire-and-forget) to a single shared Redis pub/sub channel, e.g. `job:cancel`, on the *same* Redis instance asynq already uses. Every engine worker process subscribes to this channel once at startup and keeps an in-memory `map[uuid.UUID]context.CancelFunc` populated for whatever job(s) it is currently `process()`-ing (`WORKER_CONCURRENCY`-bounded, so the map is always small). On a matching message, the worker calls the stored `CancelFunc()`, which cancels `attemptCtx` — flowing, completely unchanged, into `runCommand`'s existing `ctx.Done()` → `SIGKILL(-pid)` branch. A message for a job this particular worker replica is not running is a harmless no-op map-miss (every replica subscribes to the same channel; there is no need to route the message to "the right" replica — the map lookup does that for free).
2. **Correctness backstop (guaranteed, not just best-effort):** the Redis publish can be silently lost (no subscriber connected at that instant, a Redis blip, a message published a heartbeat before the worker even registered the job in its map). This must never allow a canceled job to be reported back to the client as `done`. It doesn't, because of the **existing guarded-transition allow-list**: once the DB row is `canceled`, the row is no longer in `{queued, active}` — `MarkDone`'s and `MarkFailed`'s `transition()` calls (`allowedFrom = []string{StatusActive}` / `{StatusQueued, StatusActive}`) will both **reject** any attempt by a worker that never received the cancel signal and runs the job to completion. The worker's existing `asynq.SkipRetry` idiom (`"already active/done/canceled — let asynq drop it rather than loop"`, already present verbatim in every `HandleXConvert`'s `MarkActive` error path) already anticipates exactly this outcome. **No new guard code is needed for this half of the backstop — it falls out of the transition allow-list that already exists.**

**Worst case if the fast path is entirely missed:** a canceled job's engine process (ffmpeg, LibreOffice, etc.) runs to completion wastefully, consuming CPU/timeout budget that would otherwise have been reclaimed instantly — but it can **never** flip the row back to `done`/`active`, never re-fires a `done` webhook, and never corrupts client-visible state. This is an accepted, bounded, and precedented shape of residual risk in this codebase (mirrors the accepted `file://` residual and the PDF/A veraPDF-cost precedent already logged in Key Decisions) — worth flagging explicitly to the roadmap as a deliberate trade-off, not an oversight.

**New terminal classification:** `context.Canceled` (distinct from `context.DeadlineExceeded`, which every existing `isXTerminal` already handles or deliberately doesn't) must be added as a top-level check in the shared `isTerminal()` — classified terminal (`SkipRetry`), but the handler must **skip the `MarkFailed` call** entirely on this path (status is already `canceled`; calling `MarkFailed` would only generate a harmless-but-noisy illegal-transition error that the reconciler-style code elsewhere in this codebase already treats as an expected, non-alerting outcome — but it's cleaner to just not call it).

**Trade-offs:** Introduces the first cross-worker Redis pub/sub usage in a codebase whose only prior Redis usage was asynq's own queue mechanics — a genuinely new operational primitive (subscription lifecycle per worker binary, at-least-once-is-not-guaranteed semantics). This is justified specifically because cancellation's whole value proposition is *speed* (a user pressing cancel expects the process-group kill within roughly a second, not "whenever the next poll interval fires") — a DB-poll-only fallback (each worker periodically re-checking `jobs.status` for its own in-flight job id) was considered and rejected as the *primary* mechanism because it reintroduces exactly the poll-latency trade-off pub/sub exists to avoid, though it remains available as a defense-in-depth secondary check (e.g., worker re-checks `jobs.status` every N seconds for its own in-flight job as an extra safety net independent of the pub/sub message, cheap given `WORKER_CONCURRENCY` is small).

### Pattern 5: SSE endpoint stays stateless by polling Postgres, not subscribing to worker-internal state

**What:** `GET /v1/jobs/{id}/events` lives in the `api` process (the only process a client talks to directly) and must remain horizontally scalable and stateless — any api replica must be able to serve any client's SSE connection, with no session affinity and no dependency on which worker replica (if any) is currently processing the job.

**Recommended approach — DB polling, not a new worker→api channel:** the SSE handler runs a short ticker (1-2s) against `jobs.Get(ctx, id)` (already used by `handleGetJob`) for the SAME columns the throttled `UpdateProgress` writes populate, and emits an `event: progress` / `data: {...}` frame only when `progress_pct` or `status` actually changed since the last tick, closing the stream once `status` reaches a terminal value (`done`/`failed`/`canceled`).

**Why this over a worker→api Redis pub/sub progress channel:** the project's own established architectural principle (documented in `.planning/PROJECT.md`'s Data Flow section) is that *"Redis/asynq holds only transient queue state... it is not consulted for job status"* — Postgres alone is the cross-process source of truth for job state. A worker-published `progress:{job_id}` pub/sub channel would be a second, competing source of live state that the api would need to reconcile with Postgres anyway (a client that connects to SSE a few seconds after a progress tick was published, with no message replay, would otherwise see nothing until the next tick — requiring a "poll once on connect, then subscribe" hybrid to avoid an initial blank frame). Given clients are trusted internal services (not thousands of concurrently-open public SSE connections), N api replicas each running a lightweight 1-2s poll per open connection is a negligible Postgres load and reuses 100% of the already-audited `Repo.Get` read path — zero new infrastructure, zero new consistency-reconciliation logic, and it directly satisfies the "stateless, horizontally-scalable api" constraint by construction (no per-connection worker affinity of any kind).

**Trade-off flagged for the roadmap:** this gives a 1-2s progress latency ceiling on the SSE stream (in addition to the `progress` webhook event, which can be pushed the instant the throttled DB write happens — see the webhook path above, which has genuinely lower latency than SSE-via-poll since it's write-triggered, not poll-triggered). If sub-second SSE latency is later required, promoting to a Redis pub/sub-fed SSE handler is the natural follow-up — deliberately deferred, not because it's infeasible, but because polling is the simpler, already-precedented choice that satisfies the milestone's stated goal ("прозрачность и управляемость", not "sub-second telemetry").

## Data Flow

### Progress write/read flow (new)

```
ffmpeg (child process, -progress pipe:1)
    ↓ (streamed, line-by-line, NOT buffered)
runCommandStreaming (exec.go) → parses out_time_us/speed
    ↓ (per-line callback)
AVConverter.ConvertWithProgress's onProgress(ProgressUpdate)
    ↓ (engine-agnostic, via ProgressReporter type-assertion)
worker.process()'s throttled wrapper (>=1 tick per ~2s)
    ↓
jobs.Repo.UpdateProgress  →  UPDATE jobs SET progress_pct=..., eta_seconds=...
                              WHERE id=$1 AND status='active'  (no lock, no job_events)
    ↓ (Postgres is now the single source of truth for progress, same as status)
   ┌─────────────────────────────┬───────────────────────────────────┐
   │                             │                                     │
GET /v1/jobs/{id} (poll)   GET /v1/jobs/{id}/events (SSE, DB-poll)   webhook `progress` event
(reads jobs.progress_pct    (reads jobs.progress_pct on a 1-2s      (enqueued directly from the
 directly, existing         ticker, streams on change)               throttle point above — lowest
 Repo.Get path, extended)                                            latency of the three, reuses
                                                                       existing HMAC/retry/dead-letter
                                                                       webhook-delivery machinery)
```

### Cancellation flow (new)

```
client → POST /v1/jobs/{id}/cancel
    ↓
handleCancelJob: ownership check (mirrors handleGetJob's 404-not-403 discipline)
    ↓
jobs.Repo.Cancel(ctx, id)  →  transition(queued|active → canceled)   [DURABLE, guarded, job_events row written]
    ↓ (only on transition SUCCESS)
publish jobID → Redis `job:cancel` channel   [BEST-EFFORT, fire-and-forget]
    ↓                                                    ↓ (if some worker IS running this job)
API responds 200/202                          worker's in-memory map hit → cancelFunc()
                                                          ↓
                                               attemptCtx.Done() → runCommand's EXISTING
                                               ctx.Done() branch → SIGKILL(-pid)  [UNCHANGED exec.go]
                                                          ↓
                                               process() returns wrapped context.Canceled
                                                          ↓
                                               isTerminal(): context.Canceled → terminal,
                                               SkipRetry, do NOT call MarkFailed
                                                          ↓ (independently, even if pub/sub was missed)
                                               any later MarkDone/MarkFailed attempt is REJECTED
                                               by transition()'s allow-list (row is no longer
                                               queued/active) — CORRECTNESS BACKSTOP, no new code
```

### Key Data Flows

1. **Progress durability boundary:** progress lives ONLY in Postgres columns, never in `job_events` (too high-frequency for an append-only audit log) and never solely in Redis (Redis stays transient-queue-state-only, per the existing architectural principle).
2. **Cancellation durability boundary:** the *decision* to cancel is durable the instant `Repo.Cancel`'s transition commits (before any signal is even published) — the pub/sub layer is purely a latency optimization for *executing* an already-durable decision, never the source of truth for whether a job is canceled.

## Scaling Considerations

| Concern | Current (single api, low job volume) | Growth (many concurrent SSE clients) | Growth (many workers, high cancel rate) |
|---------|----------------------------------|--------------------------------------|------------------------------------------|
| SSE polling load on Postgres | Negligible (one 1-2s ticker per open connection) | Bounded by client count, not job count — internal clients only, so this stays low; if it ever becomes a concern, switch the SSE source to Redis pub/sub fed by the same throttled write | N/A |
| Progress write volume | One throttled UPDATE per job per ~2s, no lock contention | N/A | N/A |
| `job:cancel` pub/sub fan-out | Every worker process receives every cancel message (map-miss is O(1) for non-matching jobs) | N/A | Scales fine — Redis pub/sub fan-out cost is proportional to subscriber count (bounded by worker replica count, which KEDA already caps), not job count |
| Webhook `progress` event volume | One enqueue per throttled tick, reusing existing webhook-delivery infra (HMAC, retry, dead-letter) | N/A | If throttling is too aggressive relative to webhook queue capacity, widen the throttle interval (a config knob, not an architecture change) |

## Anti-Patterns

### Anti-Pattern 1: Routing progress writes through `Repo.transition()`

**What people might do:** Reuse the existing guarded-transition helper for progress "to stay consistent with the rest of the codebase."
**Why it's wrong:** `transition()`'s `SELECT ... FOR UPDATE` + `job_events` insert exists to make *state-machine* changes safe and auditable. Progress isn't a state change (status stays `active`) and is far too high-frequency to log as an event — doing so would serialize every progress tick against real status transitions on the same row and bloat the append-only audit table by orders of magnitude for zero correctness benefit.
**Do this instead:** A dedicated, non-locking, self-conditioning `UPDATE ... WHERE status='active'` (Pattern 1 above).

### Anti-Pattern 2: Making `Converter.Convert` itself take a progress callback parameter

**What people might do:** Add a `progress func(float64)` parameter directly to the existing `Convert(ctx, inPath, outPath, opts) error` method so "every converter has the same shape."
**Why it's wrong:** Forces libvips/LibreOffice/Chromium/whisper's transcribe stage — none of which can report meaningful progress — to accept and ignore a parameter, widening a stable, audited interface for no functional gain on 3 of 5 engine classes, and every existing call site (including tests) would need updating.
**Do this instead:** An additional, optional `ProgressReporter` interface (Pattern 2) that only the engines that can support it implement; `process()` type-asserts and falls back cleanly.

### Anti-Pattern 3: Treating a lost cancel pub/sub message as a bug to "fix" with delivery guarantees

**What people might do:** Reach for a durable queue (a new asynq task type, or Redis Streams with consumer groups) for cancel signaling "so no cancel is ever missed."
**Why it's wrong:** Over-engineers a problem the guarded-transition allow-list already solves for free — a missed fast-path signal only wastes compute on a job whose result is discarded anyway (rejected on the eventual `MarkDone`/`MarkFailed` attempt); it can never produce an incorrect client-visible outcome. Adding delivery guarantees here spends real complexity (new task type, retry/backoff tuning, another queue to monitor) buying nothing beyond "the wasted compute window is shorter," which a periodic worker-side self-check (defense-in-depth re-poll of `jobs.status` for its own in-flight job) achieves far more simply.
**Do this instead:** Fire-and-forget pub/sub for speed + the existing transition allow-list as the correctness backstop (Pattern 4).

### Anti-Pattern 4: SSE endpoint holding per-connection worker state or requiring sticky sessions

**What people might do:** Have the SSE handler somehow "attach" to the specific worker instance processing the job (e.g., via a worker-assigned connection token) to get push-based updates without polling.
**Why it's wrong:** Breaks the api's stateless/horizontally-scalable constraint — a client's SSE connection could land on any api replica via a load balancer, and workers are independently KEDA-scaled with no stable identity a client (or the api) can address.
**Do this instead:** Any api replica polls the same Postgres row (Pattern 5) — trivially compatible with any load-balancing strategy, no affinity required.

## Integration Points

### New Components (not modified — net-new)

| Component | Purpose | Depends on |
|-----------|---------|------------|
| `internal/cancel` package | In-memory job→cancelFunc registry + Redis pub/sub subscribe/publish helpers | Redis (existing instance), `context` |
| `db/migrations/0007_job_progress.sql` | Adds `jobs.progress_pct`, `jobs.eta_seconds`, `jobs.progress_updated_at` (all nullable) | none |
| `jobs.Repo.UpdateProgress` | Non-locking progress write | new columns above |
| `jobs.Repo.Cancel` | Guarded transition to `canceled` | existing `transition()` helper (zero new locking logic) |
| `jobs.Repo.List` | Paginated, client-scoped job listing | existing `jobs_client_created_idx (client_id, created_at DESC)` — no new index needed |
| `convert.ProgressReporter` | Optional capability interface | none (additive to `convert` package) |
| `exec.runCommandStreaming` | Streaming-stdout variant of `runCommand` | `os/exec`, `syscall` (same as existing `runCommand`) |
| `api.handleCancelJob`, `handleListJobs`, `handleJobEvents` | 3 new HTTP handlers/routes | `jobs.Repo` (Cancel/List/Get), `internal/cancel` (publish) |

### Modified Components (existing — integration touches)

| Component | Change | Why |
|-----------|--------|-----|
| `internal/worker.Handler.process` | Wrap `attemptCtx` in `context.WithCancel`; register/deregister in `internal/cancel`'s map; type-assert `ProgressReporter` | Needed for both cancel-signal delivery and progress plumbing |
| `internal/worker` shared `isTerminal` | Add `context.Canceled` → terminal branch, skip `MarkFailed` on that path | Cancel must resolve to `SkipRetry` without clobbering the already-`canceled` row |
| `internal/reconciler.FindWebhookGaps` | Widen `status IN (...)` to include `'canceled'` | So a canceled job's completion webhook (if any) isn't silently dropped by the gap-sweep the same way done/failed jobs already are covered |
| `internal/worker.HandleWebhookDeliver` | New `job.Status == jobs.StatusCanceled` payload branch | Client-facing webhook contract needs a defined shape for canceled jobs (currently undefined — `done`/`failed` are the only two handled branches) |
| `internal/api.handleGetJob` | Response gains `progress_pct` / `eta_seconds` fields (nullable — absent means indeterminate) | Core SEED-008 requirement |
| `internal/api/routes.go` | Register the 3 new routes under existing `/v1/*` auth middleware | Consistency with every other `/v1/*` route (no new auth pattern) |
| `convert.AVConverter`, `convert.AudioConverter` | Implement `ProgressReporter` (av: transcode/audio-extract/thumbnail all have known source durations already probed via `ProbeDuration`; audio: only the ffmpeg-normalize stage, not whisper-cli's transcribe stage) | These are the only two engine classes with a native, parseable progress stream |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| worker ↔ Postgres (progress) | Direct SQL, non-locking `UPDATE` | New write path, deliberately bypasses `transition()` |
| api ↔ Postgres (progress read, list, cancel-transition) | Direct SQL via `jobs.Repo`, all existing connection pool | No new infrastructure |
| api ↔ worker (cancel signal) | Redis pub/sub (`job:cancel`), fire-and-forget | First cross-worker pub/sub usage in the codebase; correctness does NOT depend on delivery (backstop via transition allow-list) |
| worker ↔ Redis (progress) | **None** — progress never touches Redis; only cancel signaling does | Keeps the existing "Redis is transient queue state only" principle intact for progress, deliberately scoped the new pub/sub usage to cancel-only |
| api ↔ client (SSE) | Long-lived HTTP response, DB-poll sourced | Stateless — no worker affinity, no session stickiness required |
| worker ↔ webhook-worker (progress event) | Existing asynq `webhook` queue, unchanged delivery machinery | `progress` is just a new payload shape on the same delivery path as `done`/`failed`/`canceled` |

## Suggested Build Order

1. **Schema migration** (`0007_job_progress.sql`: `progress_pct`, `eta_seconds`, `progress_updated_at`). Foundational — every other piece reads or writes these columns. Zero risk (additive, nullable columns).
2. **Cancellation, end to end** (`Repo.Cancel` via existing `transition()`; `POST /v1/jobs/{id}/cancel`; `internal/cancel` package; worker's `context.WithCancel` wiring + `job:cancel` subscribe; `context.Canceled` branch in shared `isTerminal`; reconciler/webhook `canceled`-awareness). **Build this before progress** — it is self-contained, reuses the hardened `runCommand` SIGKILL path completely unchanged, has the clearest client-visible value, and its correctness backstop (the transition allow-list) is already fully present in the codebase today, so the riskiest new piece here is genuinely just the pub/sub plumbing, not a change to any converter or the exec contract.
3. **`GET /v1/jobs` listing** (`Repo.List`, `handleListJobs`, route). Fully independent of both cancel and progress — no schema dependency, no Converter-contract change, reuses the existing `jobs_client_created_idx`. Can be built in parallel with step 2 by a different work-stream if desired, but is sequenced after schema purely for planning tidiness (both are "control-plane API surface" work).
4. **Progress core** (`convert.ProgressReporter` interface; `exec.runCommandStreaming`; `AVConverter`/`AudioConverter` implementations parsing ffmpeg's `-progress pipe:1`; `worker.process()` throttled `UpdateProgress` wiring; `GET /v1/jobs/{id}` response gains `progress_pct`/`eta_seconds`). Sequenced after cancellation because it is the riskiest change (new exec-contract variant, new Converter capability, touches the hot conversion path of two production-hardened engine classes) — better to land the lower-risk, high-value cancel feature first and validate the `internal/cancel` package's Redis usage pattern before adding a second new capability on top of the same worker loop.
5. **Live push** (SSE `GET /v1/jobs/{id}/events` DB-poll endpoint; webhook `progress` event type reusing the existing delivery pipeline). Built last since both are purely additive consumers of the progress data step 4 produces — there is nothing to push until `progress_pct` is actually being populated.

## Open Questions / Roadmap Flags

- **ETA formula precision:** recommend `ETA = (100 - pct) / speed_normalized_rate` derived from ffmpeg's own `speed=` field (e.g., `speed=2.5x` means 2.5s of source processed per wall-clock second) rather than a naive `elapsed * (100-pct)/pct` — the former reacts faster to speed changes mid-transcode. This needs to be pinned down in requirements/roadmap, not assumed here.
- **`progress` webhook event opt-in:** should clients that already registered a `callback_url` automatically start receiving `progress` events, or does this need a new opt-in field (e.g., `progress_webhook: true`) to avoid silently changing existing integrations' webhook volume? Flag for requirements — this research treats it as reusing the existing delivery pipeline mechanically, but the *policy* (default-on vs. opt-in) is a product decision, not an architecture one.
- **SSE latency ceiling (1-2s via DB poll)** — flagged above as an explicit, deliberate trade-off; revisit only if a future milestone's requirements demand sub-second push.
- **Whisper-cli transcribe-stage progress**: whisper.cpp's CLI does not expose an equivalent `-progress pipe:1`-style machine-readable stream (not verified against the specific pinned v1.9.1 build in this research pass — flag for phase-specific research if audio progress reporting for the transcribe stage specifically becomes in-scope; the ffmpeg-normalize stage that precedes it does support it).

## Sources

- Codebase (HIGH confidence — direct reads): `internal/jobs/repo.go`, `internal/worker/worker.go`, `internal/convert/convert.go`, `internal/convert/exec.go`, `internal/convert/av.go`, `internal/convert/audioduration.go`, `internal/reconciler/reconciler.go`, `internal/webhook/{webhook,deliver}.go`, `internal/queue/queue.go`, `internal/db/migrations/{0001_init,0005_html_engine,0006_audio_engine}.sql`, `internal/api/handlers.go`, `cmd/api/main.go`, `.planning/PROJECT.md`.
- [ffmpeg-kit Wiki: Pipe Support](https://github.com/arthenica/ffmpeg-kit/wiki/Pipe-Support) — MEDIUM confidence corroboration of `-progress pipe:1` streaming pattern.
- [better-ffmpeg-progress (PyPI)](https://pypi.org/project/better-ffmpeg-progress/1.2.3/) — MEDIUM confidence corroboration of `out_time_us`/`speed` key names as consumed by an independent third-party implementation.

---
*Architecture research for: OctoConv v1.9 Job Progress & Control (integration milestone)*
*Researched: 2026-07-24*
