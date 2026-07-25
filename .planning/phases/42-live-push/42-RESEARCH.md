# Phase 42: Live Push - Research

**Researched:** 2026-07-25
**Domain:** Go stdlib SSE streaming (chi/net/http, `http.Flusher`/`http.ResponseController`) + a second, structurally-separate best-effort webhook delivery path reusing the existing `internal/webhook` HMAC/deliver primitives
**Confidence:** HIGH

## Summary

This phase adds two pure-consumer surfaces on top of Phase 41's `progress_pct`/`eta_seconds` columns and throttle gate: a DB-poll-sourced SSE stream (`GET /v1/jobs/{id}/events`, PUSH-01) and an opt-in, best-effort, single-attempt progress webhook fired inline from the worker's existing `progressSink` (PUSH-02). Both are additive to code that already exists and was live-verified in Phase 41 — no new external dependency, no new engine, no change to the guarded-transition lifecycle.

The SSE side is materially simpler than it might look: because the poll loop lives entirely in the request's own goroutine (chi/net/http already gives every HTTP request its own goroutine, and this handler spawns **no additional goroutine** — no reader thread, no worker pool), "zero leaked goroutines" is satisfied **by construction**, not by careful draining the way Phase 41's ffmpeg-reader-goroutine leak had to be. The only resources that need explicit cleanup are `time.Ticker`/`time.Timer` runtime timers (`defer .Stop()`), and the only correctness property that needs a live/unit test is that the handler's `select` loop actually **returns** on every exit path (`r.Context().Done()`, terminal status, max-duration cap) — verified in this session by reading chi v5.3.0's `middleware.WrapResponseWriter` source directly (confirms `http.Flusher` survives the existing `middleware.Logger` wrap) and by tracing `net/http`'s per-connection goroutine lifecycle (it always exits when the handler returns, regardless of streaming).

The webhook side has one non-obvious, load-bearing risk this research surfaces that CONTEXT.md does not spell out: `progressSink.Update` is called **synchronously, in-line, from the same goroutine that is draining ffmpeg's `-progress` pipe** (Phase 41's `runCommandStreaming` reader goroutine, via `newTranscodeProgressReporter`'s closure). If the inline webhook POST is fired *synchronously* from inside `Update` with no independent short timeout, a slow/hanging client `callback_url` endpoint stalls the pipe reader — which can back up ffmpeg's own write to fd 3 and, in the worst case, stall the transcode itself. This directly contradicts D-04's "must never fail or block the conversion." The fix (detailed in Common Pitfalls/Code Examples below) is to fire the HTTP POST from a short-lived, **detached** goroutine bound to its own short `context.WithTimeout` (NOT `attemptCtx`), so the reader goroutine's `Update` call returns immediately regardless of the remote endpoint's latency. Because ticks are already throttled to >= the PROG-03 interval (default 2s) and the detached goroutine's own timeout is shorter than that interval, at most one or two such goroutines are ever in flight — self-bounding, not a leak.

A genuine schema gap exists: **no column persists the `progress_webhook` opt-in today.** CONTEXT.md D-03 is explicit that this must be a new top-level field, NOT stored in `opts` (which is engine-conversion-parameter space). The `jobs` table has no such column (confirmed by reading `0001_init.sql` and every migration through `0007_job_progress.sql`), so this phase needs a **new, purely-additive migration** (`0008_progress_webhook.sql`), mirroring `0007`'s zero-risk nullable/defaulted-column shape exactly.

A second cross-cutting wiring gap: `cmd/av-worker/main.go` currently passes `nil` for `deliverer` and `signingSecret` when constructing `worker.NewHandler` (both are commented "webhook-worker-only; HandleAVConvert never reads it"). Firing an inline progress webhook from inside av-worker means av-worker must now construct a real `*webhook.Deliverer` and read `WEBHOOK_SIGNING_SECRET` — a wiring change to `cmd/av-worker/main.go` and a `.env.example` comment correction (that var is no longer webhook-worker-exclusive).

**Primary recommendation:** Add `GET /v1/jobs/{id}/events` as a new handler in `internal/api` that reuses `s.repo.Get` (already in the `Repo` interface — no new repo method needed for SSE reads) inside a single-goroutine `select` loop over a poll ticker, a heartbeat ticker, `r.Context().Done()`, and a max-duration timer, flushing via `http.NewResponseController(w).Flush()` after every write. Add a new nullable `jobs.progress_webhook` boolean column (migration `0008`) and thread it through `CreateParams`/`Job`/`Repo.Create`/`Repo.Get`. Extend `progressSink.Update` (worker.go) to fire a detached, short-timeout, best-effort HMAC-signed POST — built from a NEW narrow `progressWebhookDeliverer` interface (mirrors the existing `progressRepo` interface-segregation pattern) — that NEVER touches `enqueuer`/`webhookRepo`/the asynq webhook queue. Add the additive `event` field to both webhook body-construction sites: the new progress POST body, and `HandleWebhookDeliver`'s existing `body := map[string]any{"job_id":…, "status":…}` (worker.go, the SINGLE call site for the terminal webhook JSON).

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PUSH-01 | `GET /v1/jobs/{id}/events` streams live progress via SSE, ApiKey-authed + 404-ownership, DB-poll sourced (stateless/any-replica), heartbeat, clean close on terminal/disconnect, no goroutine leak | Architecture Patterns Pattern 1 (SSE handler skeleton); Common Pitfalls 1-3; Code Examples "SSE handler"; Don't Hand-Roll row 1 (reuse `s.repo.Get` + existing `isTerminalStatus`) |
| PUSH-02 | Opt-in `progress` webhook event, new `event` discriminator on both progress and terminal bodies, best-effort single-attempt, structurally separate from the terminal retry/dead-letter budget | Architecture Patterns Patterns 2-4; Common Pitfalls 4-6; Code Examples "inline progress webhook fire" + "terminal body discriminator"; Runtime State Inventory (migration gap) |

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 (SSE payload + cadence):** DB-poll-sourced (NO in-memory pub/sub — stateless api, any replica serves). Poll interval ~1s, env-tunable (`SSE_POLL_INTERVAL`-style knob). Emit-on-change only: send ONLY when `progress_pct`, `eta_seconds`, or `status` changed since the last emit. Payload mirrors `handleGetJob`'s null-aware fields: `{"status":…, "progress_pct":…|null, "eta_seconds":…|null}`. Named SSE event (`event: progress`). Heartbeat comment (`: keepalive\n\n`) every ~15s (env-tunable). Rejected: emit-every-poll.
- **D-02 (SSE lifecycle/termination):** On terminal status (`done`/`failed`/`canceled`), emit ONE final event carrying `{status}`, then close. NO `download_url` in the final event (client does a normal `GET /v1/jobs/{id}`). On client disconnect (`r.Context().Done()`), return cleanly with zero leaked goroutines (load-bearing SC, mirrors Phase 41 leak discipline). Max-stream-duration safety cap (~30m, env-tunable) closes a stream whose job never terminates. Rejected: download_url in final event; no max-cap.
- **D-03 (opt-in + event discriminator):** Opt-in per job via a NEW top-level form field alongside `callback_url` (e.g. `progress_webhook=true`) — NOT in `opts`. `event` discriminator field on BOTH bodies — new progress body (`"event":"progress"`) AND existing terminal body (`"event":"completed"` or similar). Both hit the SAME `callback_url`, so discrimination is required. Additive = backward-compat-safe. No `callback_url`, or `progress_webhook` absent/false → no progress webhooks. Rejected: opt-in inside `opts`; discriminator only on the progress body; a separate `progress_callback_url`.
- **D-04 (progress-webhook delivery path — load-bearing separation):** Worker fires progress webhooks INLINE, best-effort, single HTTP attempt, drop-on-failure, from the SAME progress sink that writes the DB. Reuses the SAME PROG-03 throttle gate (one decision drives both the DB write and the webhook fire). HMAC-signed with the existing `sign` scheme. Fires ONLY when the job (a) opted in AND (b) is on the ramping path (av transcode, Phase 41 D-02). NEVER enqueues to `webhook_deliveries`/the asynq webhook retry queue — separation guaranteed by construction (no enqueue path exists). Must NEVER fail or block the conversion (best-effort, short timeout, error ignored). Rejected: a separate no-retry asynq queue; reusing the webhook worker with `MaxRetry=0`.

### Claude's Discretion

Exact SSE event names (`progress`/`done` vs a single `event` field), exact `event` string values (`"progress"`/`"completed"`/`"done"`), concrete env-var names (`SSE_POLL_INTERVAL`/`SSE_HEARTBEAT_INTERVAL`/`SSE_MAX_STREAM_DURATION`), the per-attempt HTTP timeout for the inline progress webhook, and whether the terminal SSE event reuses the `progress` event name or a distinct one — provided: DB-poll-sourced + emit-on-change (D-01), zero goroutine leak on disconnect/terminal/cap (D-02), the discriminator appears on BOTH bodies (D-03), and the progress-webhook path never touches the retry/dead-letter queue (D-04).

### Deferred Ideas (OUT OF SCOPE)

- WebSocket / long-poll transports — SSE only for v1.9.
- Progress for non-av engines / a generalized progress bus — out of scope (Phase 41 scopes the ramp to av transcode).
- OCR / archive / CAD / preview engine classes — deferred seeds (SEED-005/006/007 + the OCR idea from Phase 41's discussion).
</user_constraints>

## Project Constraints (from CLAUDE.md)

- Errors wrapped with `fmt.Errorf("<action>: %w", err)`; sentinel errors via `errors.Is`/`errors.As` — the SSE handler has almost no error paths (it's a read-only poll loop), but the `progressWebhookDeliverer` fire path must follow the existing best-effort discard convention (`_ = deliverer.Deliver(...)`), never wrap-and-return.
- `panic` is never used for control flow in `internal/`; chi's `middleware.Recoverer` is the only recovery mechanism — the SSE handler's `select` loop must not rely on panic/recover for cleanup; all cleanup is `defer`.
- Guarded-transition (`Repo.transition`) is the ONLY path for lifecycle status changes — SSE and progress webhooks are pure reads/telemetry; neither writes `jobs.status` (mirrors Phase 41's progress-write discipline exactly, `internal/jobs/repo.go` `UpdateProgress`/`SetSourceDuration`).
- Env-var-only configuration (`os.Getenv`, no config file) — every new SSE_*/progress-webhook tunable follows the existing `envInt`/`envDuration`/`envDurationSeconds`/`firstField` helper pattern already duplicated in `cmd/api/main.go`/`cmd/av-worker/main.go`.
- Library code (`internal/*`) never logs — `internal/api`, `internal/worker`, `internal/webhook` must not `log.Printf` on a failed progress-webhook delivery; only `cmd/*/main.go` may log (and only at startup, per the existing convention).
- Best-effort telemetry writes must never fail the conversion — the existing precedent (`_ = h.enqueuer.EnqueueWebhookDeliver(...)`, `_ = s.repo.UpdateProgress(...)`) is the exact shape the inline progress-webhook fire must follow: error return value discarded, no propagation into `process()`'s error chain.
- Naming: lowercase-no-separator filenames (`sse.go` not `s-s-e.go`), `handle<Noun>` for handler methods (`handleJobEvents`), `New<Type>` constructors, short 1-2 letter receivers, `ctx` always first param.

## Summary (Architectural Responsibility Map)

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SSE stream lifecycle (auth, ownership, poll loop, heartbeat, close) | API / Backend | — | `internal/api`; a pure HTTP-layer concern, no browser/frontend tier exists in this project |
| Progress read for each poll tick | Database / Storage (read) | API / Backend | `jobs.Repo.Get`, already-indexed PK lookup; API layer owns the polling cadence, DB just serves the read |
| Progress-webhook opt-in flag (parse + persist) | API / Backend | Database / Storage | Parsed in `handleCreateJob` (mirrors `callback_url`), persisted via a new `jobs.progress_webhook` column |
| Inline progress-webhook delivery (fire) | Worker (API/Backend) | — | `internal/worker`'s `progressSink`, the same process/goroutine that already writes the DB progress tick (Phase 41) |
| Terminal webhook `event` discriminator | Worker (API/Backend) | — | `HandleWebhookDeliver`'s existing body-construction site, worker.go — additive field only |
| HMAC signing + HTTP delivery (reused) | Worker (API/Backend) | — | `internal/webhook` package, already used by the terminal path; the progress path reuses it as a library, not a new tier |

No browser/CDN tier exists in OctoConv (internal API service, no shipped frontend) — every Phase 42 capability lives entirely in the API + Worker + Database tiers already mapped in `CLAUDE.md`'s Layers section, identical in shape to Phase 41's map.

## Standard Stack

### Core

Zero new dependencies. Everything is stdlib or already-vendored `[VERIFIED: go.mod, go.sum — read directly; go version go1.26.5 darwin/arm64 local toolchain]`:

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| stdlib `net/http` (`http.Flusher`, `http.NewResponseController`) | Go 1.26 | SSE framing + per-write flush | `http.ResponseController` (Go 1.20+) is the modern, wrapper-aware idiom — it walks a chain of `Unwrap() http.ResponseWriter` implementations, so it works correctly even after chi's `middleware.Logger`/`Recoverer` wrap the writer, without needing a brittle direct type-assertion `[VERIFIED: go doc net/http ResponseController, this session]` |
| `github.com/go-chi/chi/v5` v5.3.0 (existing) | already pinned | Routing for the new `/jobs/{id}/events` route | Same router already used for every other `/v1` route; no new registration pattern needed |
| stdlib `time.Ticker`/`time.Timer` | Go 1.26 | Poll cadence, heartbeat cadence, max-stream-duration cap | Simplest correct primitive for three independent cadences inside one `select` |
| `internal/webhook` (existing, this repo) | — | HMAC signing (`SignPayload`) + single-attempt HTTP delivery (`Deliverer.Deliver`) | Already built, already ASVS-reviewed for the terminal path (Phase 2/16) — the progress path reuses it as a library call, introducing zero new crypto/HTTP-client code |
| `github.com/jackc/pgx/v5` v5.10.0 (existing) | already pinned | New `progress_webhook` column read/write via existing `Repo.Create`/`Repo.Get` | Same pool, same query shape as every other job-row field |

### Supporting

None. Stdlib + existing internal packages only.

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `http.NewResponseController(w).Flush()` | Direct `w.(http.Flusher)` type-assertion | Both work here (chi v5.3.0's wrap chain preserves `http.Flusher` — verified by reading `middleware/wrap_writer.go`), but `ResponseController` is the documented Go 1.20+ idiom specifically designed to survive arbitrary `ResponseWriter` wrapping via `Unwrap()`, so it is more robust to a FUTURE middleware addition that a raw type-assertion would silently break |
| A single-goroutine `select` loop inside the handler | A separate goroutine per stream that writes to the `ResponseWriter` while the handler goroutine blocks on a signal channel | The separate-goroutine shape reintroduces exactly the join/leak-safety complexity Phase 41's `runCommandStreaming` had to solve for ffmpeg — completely unnecessary here since there is no external process to drain; a single goroutine handling both the poll/heartbeat ticks AND the response write is simpler and structurally leak-free |
| Firing the inline progress webhook detached (its own goroutine + short timeout) | Firing it synchronously inline inside `progressSink.Update` (same call stack as the DB write) | Synchronous-inline blocks the ffmpeg `-progress` pipe reader goroutine for up to the HTTP timeout on every throttled tick — a slow/hanging callback endpoint risks stalling the transcode itself (see Common Pitfalls). Detached-with-short-timeout avoids this entirely while staying "best-effort" |
| A new `jobs.progress_webhook boolean` column | Overloading an existing free-form field, or bit-packing into `callback_url` | CONTEXT.md D-03 explicitly locks "NOT in opts" and "not a second URL field" — a dedicated boolean column is the only shape consistent with the locked decision, and No existing column can safely carry this (there is no spare boolean/jsonb slot on `jobs` reserved for delivery config) |

**Installation:**
```bash
# No new dependencies — nothing to install.
```

**Version verification:** No third-party package versions to verify — this phase touches only stdlib (`net/http`, `time`), the already-pinned `go-chi/chi/v5@v5.3.0`/`jackc/pgx/v5@v5.10.0`/`google/uuid@v1.6.0`, and this repository's own `internal/webhook`/`internal/jobs`/`internal/worker` packages.

## Package Legitimacy Audit

Not applicable — zero new Go dependencies, zero new external packages. `slopcheck`/registry verification was not run because there is nothing to install (mirrors Phase 41's "Not applicable" precedent exactly).

## Architecture Patterns

### System Architecture Diagram

```text
PUSH-01: SSE stream (DB-poll, single goroutine, no leak by construction)
─────────────────────────────────────────────────────────────────────────
  GET /v1/jobs/{id}/events  (auth.Middleware -> ratelimit -> handleJobEvents)
            │
            ▼
  s.repo.Get(ctx, id)  -- SAME method handleGetJob already calls
            │
    ┌───────┴────────┐
   404 (not found OR      job found, ownership OK
   cross-client, per            │
   handleGetJob's exact         ▼
   ownership guard)      set SSE headers (text/event-stream, no-cache,
                          keep-alive); http.NewResponseController(w)
                                │
                          emit initial snapshot event (first "change"
                          from an unset baseline) -- client gets state
                          immediately, not after the first poll tick
                                │
                          ┌─────▼──────────────────────────────────┐
                          │ for { select {                          │
                          │   case <-r.Context().Done():            │  <-- client disconnect;
                          │     return                               │      handler goroutine exits,
                          │   case <-maxDurationTimer.C:             │      NO extra goroutine ever
                          │     return                               │      spawned -- "zero leak"
                          │   case <-heartbeatTicker.C:              │      is structural, not proven
                          │     w.Write(": keepalive\n\n"); Flush()  │      by a join
                          │   case <-pollTicker.C:                   │
                          │     job := s.repo.Get(ctx, id)           │
                          │     if changed(job, lastSnapshot):       │
                          │       write "event: progress\ndata:…";  │
                          │       Flush(); lastSnapshot = job        │
                          │     if isTerminalStatus(job.Status):     │  <-- REUSES handlers.go's
                          │       write terminal event; Flush()      │      existing isTerminalStatus
                          │       return                             │      (done/failed/canceled)
                          │ } }                                      │
                          └──────────────────────────────────────────┘

PUSH-02: opt-in progress webhook (structurally separate from the
terminal retry/dead-letter path -- no enqueue path exists for progress)
─────────────────────────────────────────────────────────────────────────
  POST /v1/jobs (multipart)
            │
            ▼
  parse new top-level form field `progress_webhook` (alongside
  callback_url, SAME SSRF validation already run once for callback_url --
  NO second validation path, D-03/D-04)
            │
            ▼
  jobs.Repo.Create -- INSERT ... progress_webhook  (NEW column, migration 0008)
            │
            ▼
  av-worker: process() -> runConvertWithOptionalProgress
            │
            ▼
  progressSink{repo, jobID, minInterval, minDeltaPct,
               webhookEnabled: job.ProgressWebhook,       <-- NEW fields
               callbackURL:    job.CallbackURL,
               deliverer:      h.progressWebhookDeliverer, (NEW Handler field,
               signingSecret:  h.progressWebhookSecret}    nil for every
                                                            non-av-worker cmd)
            │
            ▼
  sink.Update(u ProgressUpdate):
    if !throttleGatePasses(u): return              <-- SAME PROG-03 gate,
    _ = repo.UpdateProgress(ctx, jobID, u.Pct, u.ETA)   ONE decision drives
    if webhookEnabled && callbackURL != "":            BOTH writes (D-04)
      go fireProgressWebhook(detached, short timeout,   <-- DETACHED, own
                              callbackURL, jobID, u)         short ctx --
                                                              see Pitfall 5
            │
            ▼
  fireProgressWebhook: body{job_id, event:"progress",
    progress_pct, eta_seconds} -> json.Marshal -> SignPayload
    -> deliverer.Deliver(shortCtx, callbackURL, body, ts, sig)
    -- error IGNORED, NEVER touches h.enqueuer/h.webhookRepo/
       queue.NewWebhookDeliverTask -- no asynq task type exists for this,
       so "never enqueues" is true by construction, not by a runtime check

  (unrelated, unchanged) terminal completion path:
  HandleAVConvert -> MarkDone/MarkFailed -> h.enqueuer.EnqueueWebhookDeliver
    -> HandleWebhookDeliver: body := map[string]any{"job_id":…, "status":…}
       + NEW: body["event"] = "completed"              <-- additive field,
       -> json.Marshal -> SignPayload -> Deliver           SAME call site,
       -> RecordAttempt -> (retry via asynq / dead-letter   SAME HMAC scheme,
          on exhaustion, UNCHANGED, D-05/D-10)              backward-compat
```

### Recommended Project Structure

```
internal/api/
├── sse.go                 # NEW: handleJobEvents, snapshot diffing, SSE framing helpers
├── sse_test.go             # NEW: unit tests (httptest.NewRecorder + a canceled request ctx;
│                           #      no live server needed for the disconnect/terminal/max-cap paths)
├── routes.go               # MOD: + r.Get("/jobs/{id}/events", s.handleJobEvents)
├── api.go                  # MOD: + SSEPollInterval/SSEHeartbeatInterval/SSEMaxStreamDuration
│                           #      Config fields + Server fields (zero-value-triggers-default,
│                           #      mirrors PresignTTL's existing treatment)
├── handlers.go             # MOD: handleCreateJob parses `progress_webhook` form field
│                           #      (mirrors callback_url's own parse site exactly)
└── callbackurl.go          # UNCHANGED: reused verbatim, no second validation path

internal/jobs/
├── jobs.go                 # MOD: + Job.ProgressWebhook bool; + CreateParams.ProgressWebhook bool
└── repo.go                 # MOD: Create's INSERT + Get's SELECT gain progress_webhook

internal/db/migrations/
└── 0008_progress_webhook.sql  # NEW: ALTER TABLE jobs ADD COLUMN progress_webhook boolean
                                #      NOT NULL DEFAULT false -- purely additive, mirrors 0007

internal/worker/
├── worker.go                # MOD: progressSink gains webhookEnabled/callbackURL/deliverer/
│                             #      signingSecret/timeout fields + fireProgressWebhook;
│                             #      Handler gains progressWebhookDeliverer/progressWebhookSecret/
│                             #      progressWebhookTimeout fields + a SetProgressWebhook setter
│                             #      (mirrors SetProgressThrottle's post-construction pattern);
│                             #      HandleWebhookDeliver's body map gains body["event"]="completed"
└── worker_test.go            # MOD: TestProgressThrottle-adjacent test extended/added for the
                              #      webhook-fire gating (spy progressWebhookDeliverer, no real HTTP)

cmd/av-worker/main.go        # MOD: construct webhook.NewDeliverer() (or a new short-timeout
                              #      variant) + read WEBHOOK_SIGNING_SECRET; call
                              #      h.SetProgressWebhook(...) before srv.Start(mux) -- SAME
                              #      happens-before convention as SetProgressThrottle above it

.env.example                 # MOD: correct the WEBHOOK_SIGNING_SECRET "webhook-worker-only"
                              #      comment (no longer true -- av-worker now reads it too);
                              #      + document SSE_POLL_INTERVAL/SSE_HEARTBEAT_INTERVAL/
                              #      SSE_MAX_STREAM_DURATION/PROGRESS_WEBHOOK_TIMEOUT
```

### Pattern 1: SSE handler — single-goroutine poll loop, no extra goroutine spawned

**What:** `handleJobEvents` mirrors `handleGetJob`'s auth/ownership/404 shape exactly (same `s.repo.Get` + `client.ID` comparison), then enters a `select` loop entirely within the request's own goroutine.

**When to use:** The one new route, `GET /v1/jobs/{id}/events`.

```go
// Source: pattern synthesized from internal/api/handlers.go's handleGetJob
// (ownership guard, reused verbatim) + Go 1.20+ http.ResponseController
// (verified via `go doc net/http ResponseController`, this session) +
// chi v5.3.0's middleware/wrap_writer.go (verified: http.Flusher survives
// middleware.Logger's wrap via the Unwrap-aware fancyWriter, this session).
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	job, err := s.repo.Get(ctx, id)
	if errors.Is(err, jobs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	client, _ := auth.ClientFromContext(ctx)
	if job.ClientID != client.ID {
		// IDENTICAL to handleGetJob's ownership guard (T-39-01 precedent):
		// cross-client is indistinguishable from not-found.
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// X-Accel-Buffering: no -- defensive, in case this service is ever put
	// behind an nginx reverse proxy later; harmless no-op otherwise.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	emit(rc, w, "progress", job) // initial snapshot -- see discretion note below
	last := snapshotOf(job)
	if isTerminalStatus(job.Status) {
		emitTerminal(rc, w, job.Status) // job was already terminal when the
		return                          // stream opened -- one event, close.
	}

	pollTicker := time.NewTicker(s.ssePollInterval)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(s.sseHeartbeatInterval)
	defer heartbeatTicker.Stop()
	maxDuration := time.NewTimer(s.sseMaxStreamDuration)
	defer maxDuration.Stop()

	for {
		select {
		case <-ctx.Done():
			// D-02: client disconnect. No cleanup beyond the three defers
			// above -- this goroutine IS the request goroutine; returning
			// here ends it. No separate goroutine was ever spawned, so
			// there is nothing else to join or leak.
			return
		case <-maxDuration.C:
			// D-02: wedged/abandoned stream safety cap.
			return
		case <-heartbeatTicker.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return // write failed -- treat as disconnect
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-pollTicker.C:
			job, err := s.repo.Get(ctx, id)
			if err != nil {
				return // transient DB error -- close rather than spin;
				       // the client's own reconnect logic recovers
			}
			if snap := snapshotOf(job); snap != last {
				emit(rc, w, "progress", job)
				last = snap
			}
			if isTerminalStatus(job.Status) {
				emitTerminal(rc, w, job.Status)
				return
			}
		}
	}
}
```

**Why "zero leaked goroutines" needs no join/drain discipline here (unlike Phase 41):** every branch of the `select` runs in the SAME goroutine `net/http` already allocated for this request. There is no `go func(){...}()` anywhere in this handler. When any branch returns, that goroutine — and this handler's entire execution — ends; `net/http`'s own per-connection machinery (unrelated to application code) tears down the underlying connection goroutine exactly as it does for every other request. The only thing a leak-discipline TEST needs to prove is that the `select` loop's four exit conditions are actually reachable (a disconnected `r.Context()` unblocks promptly, the max-duration timer fires, a terminal status is detected on the very next poll) — provable with `httptest` + a manually-canceled request context, no live process/SIGKILL harness needed (unlike PROG-05's ffmpeg case).

**Claude's Discretion, flagged for the planner:** whether to always emit an initial snapshot event immediately on connect (recommended above — a client should not wait up to `SSE_POLL_INTERVAL` to learn the job's current state) versus waiting for the first poll tick. Both satisfy "emit-on-change" literally (the initial emit is a change from an "unset" baseline); recommend the immediate-emit shape for better UX with no correctness cost.

### Pattern 2: `progress_webhook` opt-in — new column, parsed like `callback_url`

**What:** A new nullable-turned-`NOT NULL DEFAULT false` boolean column, parsed at job-create time exactly where `callback_url` is parsed today.

```sql
-- internal/db/migrations/0008_progress_webhook.sql
-- Additive, zero-behavior-change (mirrors 0007_job_progress.sql's shape):
-- every existing INSERT that omits this column gets false, every existing
-- row reads false. NOT stored in the options jsonb column (D-03: delivery
-- config is orthogonal to engine conversion parameters).
ALTER TABLE jobs
    ADD COLUMN progress_webhook boolean NOT NULL DEFAULT false;
```

```go
// internal/api/handlers.go, immediately after the existing callback_url
// block (D-03/D-04: same SSRF validation already run for callback_url
// covers this flag too -- it is the SAME URL, not a second one).
progressWebhook := r.FormValue("progress_webhook") == "true"
if progressWebhook && callbackURL == "" {
	// Opt-in with no delivery target is a no-op, not an error (D-03: "no
	// callback_url ... sends NO progress webhooks") -- silently normalize
	// rather than 400, mirroring opts' "empty/absent means off" tolerance.
	progressWebhook = false
}
```

**Why NOT a second SSRF validation path:** `progress_webhook` never carries a URL — it's a boolean gate over the SAME `callback_url` already validated by `validateCallbackURL` earlier in `handleCreateJob`. There is exactly one URL to validate, one validation call site, unchanged.

### Pattern 3: Inline progress-webhook fire — detached, short-timeout, throttle-gated

**What:** Extends `progressSink.Update` (worker.go) with an optional, best-effort webhook fire that piggybacks on the SAME throttle decision already gating the DB write (D-04).

```go
// Source: extends internal/worker/worker.go's existing progressSink
// (verified: Update already gates minInterval/minDeltaPct before calling
// repo.UpdateProgress -- this is the ONE call site D-04 requires the
// webhook fire to share).

// progressWebhookDeliverer is the narrow subset of *webhook.Deliverer the
// sink needs -- interface-segregated exactly like progressRepo above it,
// so the fire path is unit-testable with a spy, no real HTTP/Handler.
type progressWebhookDeliverer interface {
	Deliver(ctx context.Context, url string, body []byte, timestamp int64, signature string) (int, error)
}

type progressSink struct {
	ctx         context.Context
	repo        progressRepo
	jobID       uuid.UUID
	minInterval time.Duration
	minDeltaPct int

	hasLast   bool
	lastWrite time.Time
	lastPct   int

	// NEW (D-04/PUSH-02): nil-safe -- every non-opted-in job, and every
	// non-av-worker cmd (webhookDeliverer stays nil there), takes the
	// existing byte-identical path with no behavior change.
	webhookEnabled bool
	callbackURL    string
	webhookSecret  []byte
	webhookDelivr  progressWebhookDeliverer
	webhookTimeout time.Duration
}

func (s *progressSink) Update(u convert.ProgressUpdate) {
	now := time.Now()
	if s.hasLast {
		if now.Sub(s.lastWrite) < s.minInterval || u.Pct-s.lastPct < s.minDeltaPct {
			return // SAME PROG-03 gate as before -- unchanged
		}
	}
	_ = s.repo.UpdateProgress(s.ctx, s.jobID, u.Pct, u.ETA)
	s.hasLast, s.lastWrite, s.lastPct = true, now, u.Pct

	// D-04: the SAME throttle decision above ALSO drives the webhook fire.
	// Never touches h.enqueuer / h.webhookRepo / any asynq task type --
	// there is no NewProgressWebhookDeliverTask, so "never enqueues" is a
	// structural fact, not a runtime branch that could be wrong.
	if s.webhookEnabled && s.callbackURL != "" && s.webhookDelivr != nil {
		s.fireDetached(u)
	}
}

// fireDetached fires the progress webhook from a SHORT-LIVED, DETACHED
// goroutine bound to its own short timeout -- deliberately NOT s.ctx
// (attemptCtx) and deliberately NOT synchronous with the caller. This is
// the single most important correctness property in this pattern: Update
// is invoked from the SAME goroutine that drains ffmpeg's -progress pipe
// (Phase 41's runCommandStreaming reader). A synchronous HTTP call here
// would block that reader for up to the timeout on every throttled tick;
// if the remote endpoint hangs, ffmpeg's own write to fd 3 can back up and
// stall the transcode -- turning a best-effort telemetry feature into a
// conversion-correctness bug (see Common Pitfalls, Pitfall 5). Because
// ticks are already throttled to >= minInterval apart, and webhookTimeout
// is chosen shorter than minInterval, at most 1-2 of these goroutines are
// ever in flight for a single job -- self-bounding, not a leak.
func (s *progressSink) fireDetached(u convert.ProgressUpdate) {
	body, err := json.Marshal(map[string]any{
		"job_id":       s.jobID,
		"event":        "progress",
		"progress_pct": u.Pct,
		"eta_seconds":  u.ETA,
	})
	if err != nil {
		return // best-effort: a marshal failure is dropped, never logged
		       // (internal/* never logs) and never surfaced to process().
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.webhookTimeout)
		defer cancel()
		ts := time.Now().Unix()
		sig := webhook.SignPayload(s.webhookSecret, ts, body)
		_, _ = s.webhookDelivr.Deliver(ctx, s.callbackURL, body, ts, sig) // error ignored: drop-on-failure (D-04)
	}()
}
```

```go
// cmd/av-worker/main.go: wiring, mirrors SetProgressThrottle's existing
// post-construction-setter convention exactly -- must complete BEFORE
// srv.Start(mux), the same happens-before boundary every other
// process-wide config write in this file already uses.
webhookSigningSecret := []byte(os.Getenv("WEBHOOK_SIGNING_SECRET"))
progressWebhookTimeout := envDuration("PROGRESS_WEBHOOK_TIMEOUT", 3*time.Second)
if len(webhookSigningSecret) > 0 {
	h.SetProgressWebhook(webhook.NewDeliverer(), webhookSigningSecret, progressWebhookTimeout)
}
// If WEBHOOK_SIGNING_SECRET is unset, progress webhooks are silently
// disabled (nil progressWebhookDeliverer -> Update's nil-check skips the
// fire) rather than failing startup -- av-worker must remain runnable in
// deployments that never use progress_webhook=true.
```

**Why the per-attempt timeout should be SHORTER than the throttle interval:** if `webhookTimeout >= minInterval` (default 2s), a consistently slow endpoint could have multiple in-flight detached goroutines accumulate faster than they complete under sustained load — still bounded (each carries its own timeout, so they all eventually terminate), but unnecessarily so. A `PROGRESS_WEBHOOK_TIMEOUT` default around 3s against a `PROGRESS_MIN_INTERVAL_SECONDS` default of 2s is a reasonable starting point (Claude's Discretion per CONTEXT.md) but is itself an assumption pending the same "measure, don't assume" discipline PROG-03's throttle defaults are already flagged with.

### Pattern 4: Terminal webhook `event` discriminator — one call site, additive field

**What:** `HandleWebhookDeliver` in `internal/worker/worker.go` (the SOLE site that marshals the terminal webhook JSON body) gains one additive map key.

```go
// internal/worker/worker.go, HandleWebhookDeliver — the EXACT existing
// body-construction site (worker.go, current line ~1026-1029):
body := map[string]any{
	"job_id": job.ID,
	"status": job.Status,
	"event":  "completed", // NEW (D-03): additive discriminator, present
	                       // regardless of done/failed -- the existing
	                       // "status" field already distinguishes those;
	                       // "event" only needs to distinguish "this is a
	                       // terminal delivery" from "event":"progress".
}
```

**Why this is safe for the existing HMAC contract:** `SignPayload` signs whatever bytes `json.Marshal(body)` produces, computed AFTER this field is added — there is no separate "canonical body" the signature was computed against and that this field would now diverge from. Any consumer that already ignores unknown JSON keys (D-03's stated backward-compat assumption) verifies the signature over the actual bytes received, which include the new field; nothing to reconcile.

### Anti-Patterns to Avoid

- **Spawning a dedicated goroutine to write the SSE response while the handler goroutine blocks elsewhere:** unnecessary complexity that reintroduces a join/leak-safety problem this domain doesn't have. Keep everything in the one request goroutine (Pattern 1).
- **Firing the inline progress webhook synchronously inside `progressSink.Update`:** blocks the ffmpeg `-progress` pipe reader goroutine on a slow endpoint — the single most important pitfall in this phase (Pattern 3 / Pitfall 5).
- **Adding a second SSRF-validation path for `progress_webhook`:** there is only one URL (`callback_url`); a second validation call would be pure duplication with no additional value.
- **Routing the progress webhook through `queue.NewWebhookDeliverTask`/`h.enqueuer` "just with `MaxRetry(0)`":** CONTEXT.md explicitly rejects this — it shares code paths with the dead-letter/retry machinery and risks accidentally mixing budgets. The correct shape has NO enqueue call for progress at all.
- **Storing `progress_webhook` inside the `options` jsonb column:** explicitly rejected by D-03 — delivery configuration is orthogonal to engine conversion parameters, and conflating them would also make the flag readable/writable through the existing `opts` validation pipeline, which has no business gating delivery behavior.
- **Detecting SSE client disconnect only by checking `Write`'s return error:** unreliable timing-wise (a write can appear to succeed briefly after a client has gone away, depending on OS socket buffering) — `r.Context().Done()` is the documented, immediate signal `net/http` provides specifically for this, and Pattern 1's `select` checks it as a first-class case, not as a side effect of a failed write.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SSE client disconnect detection | A custom heartbeat-ack protocol or TCP-keepalive probing | `r.Context().Done()` (stdlib) | `net/http` already closes the request context the moment the underlying connection is gone (client close, proxy timeout, etc.) — this is the documented, zero-cost mechanism, not something to reimplement |
| Response flushing through middleware-wrapped writers | A direct `w.(http.Flusher)` type assertion, hoping no future middleware breaks it | `http.NewResponseController(w).Flush()` | Purpose-built (Go 1.20+) to survive an arbitrary chain of `Unwrap()`-implementing wrappers; verified against this project's actual chi v5.3.0 wrap chain in this session |
| Ownership / 404-not-403 guard for the new SSE route | A new authorization check | The IDENTICAL `job.ClientID != client.ID` comparison `handleGetJob` already uses | One canonical ownership check in the codebase (T-39-01 precedent already establishes this pattern for `handleCancelJob` too) — a third near-duplicate implementation is a drift risk, not a feature |
| Terminal-status detection for SSE close | A new `isTerminal`-style helper | The EXISTING `isTerminalStatus` function already in `internal/api/handlers.go` (used by `handleCancelJob`) | Already correct (done/failed/canceled), already tested, already in the same package |
| HMAC signing for the progress webhook | A second signing scheme/secret for "the fast path" | `webhook.SignPayload` (existing, unchanged) | One signing scheme, one secret, one verification story for API consumers regardless of which process (av-worker inline vs webhook-worker queued) delivered the event |
| Progress-webhook HTTP delivery | A bespoke `http.Client` call in `internal/worker` | `webhook.Deliverer.Deliver` (existing) with a possibly-different `*http.Client` timeout | Reuses the exact request-construction/2xx-success/redirect-refusal logic already ASVS-reviewed for the terminal path; only the per-attempt timeout may differ, not the delivery mechanics |
| SSE progress read | A new denormalized read path / cache | `jobs.Repo.Get` (already used by `handleGetJob`, already in the `Repo` interface) | The exact same row, the exact same null-aware fields — no second source of truth to keep in sync |

**Key insight:** every piece of this phase is composition of already-built, already-verified primitives (Phase 41's progress columns/throttle, the existing webhook HMAC/deliver pair, the existing ownership guard, stdlib SSE mechanics) onto two new call sites — there is no new infrastructure class here, mirroring Phase 41's own "Key insight" almost exactly.

## Runtime State Inventory

**Trigger check:** this phase adds a new column and a new inline delivery path, but does not rename/refactor/migrate any EXISTING data or identifier — so the full 5-category audit (stored data / live service config / OS-registered state / secrets / build artifacts) from the standard rename-phase checklist is not the primary lens here. However, one item from that checklist genuinely applies and must be called out explicitly, since it is easy to miss:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Schema / stored data | `jobs` table has NO column today for the `progress_webhook` opt-in (confirmed by reading `0001_init.sql` through `0007_job_progress.sql` — no such column exists anywhere in the migration history) | **New migration required**: `0008_progress_webhook.sql`, `ALTER TABLE jobs ADD COLUMN progress_webhook boolean NOT NULL DEFAULT false` — purely additive, zero-behavior-change for every existing row/INSERT, mirrors `0007`'s shape exactly |
| Secrets / env vars | `WEBHOOK_SIGNING_SECRET` is documented in `.env.example` as read ONLY by `cmd/webhook-worker` today; this phase makes `cmd/av-worker` a SECOND reader of the same secret (needed to sign inline progress webhooks with the identical HMAC scheme clients already verify against) | Code edit only (no key rotation, no new secret) — update `cmd/av-worker/main.go` to read it, and correct the `.env.example` comment that currently claims "webhook-worker-only" |
| Build artifacts | None — no compiled binaries or package installs reference the old/new state | None — verified by inspection; no egg-info/binary-cache equivalent exists in this Go codebase |
| Live service config | None — no external service (n8n, Datadog, etc.) is involved in this phase; all new state is internal to Postgres/the two Go processes | None — verified; this project has no such external dependency for this feature |
| OS-registered state | None — no new systemd/launchd/pm2/Task-Scheduler registration is introduced | None — verified; SSE and the inline webhook are ordinary HTTP behavior inside the existing `api`/`av-worker` processes |

## Common Pitfalls

### Pitfall 1: Detecting SSE disconnect via write errors instead of `r.Context().Done()`
**What goes wrong:** A handler that only checks `Write`'s return error to decide "client gone" can keep polling/writing for one or more extra cycles after a real disconnect, because OS socket buffering can let a write "succeed" briefly even after the peer is gone.
**Why it happens:** It feels natural to treat "the write failed" as the disconnect signal, mirroring how one might notice a broken pipe in a simpler synchronous protocol.
**How to avoid:** Make `r.Context().Done()` a first-class `select` case (Pattern 1) — `net/http` closes it promptly and is the documented mechanism for exactly this.
**Warning signs:** In a manual test, killing the client connection and watching the server keep polling/writing for more than one tick before noticing.

### Pitfall 2: `time.Ticker`/`time.Timer` resource leak if `Stop()` is forgotten on every exit path
**What goes wrong:** Returning from the handler on any branch without having `defer`-registered `Stop()` for every ticker/timer leaves the underlying runtime timer alive until the next GC cycle touches it — not a goroutine leak (Go's timer implementation as of Go 1.23+ does not spawn a dedicated goroutine per timer the way older runtimes' timer-heap-goroutine model did), but still a real, measurable per-stream resource cost at high stream-churn volume.
**Why it happens:** Easy to add a `time.NewTicker` at the top of the function and forget the `defer .Stop()` right next to it, especially when there are three of them (poll/heartbeat/max-duration) and multiple early-return branches.
**How to avoid:** `defer pollTicker.Stop()` / `defer heartbeatTicker.Stop()` / `defer maxDuration.Stop()` immediately after each `time.New*` call, before any branch that could return.
**Warning signs:** A load test that opens and closes many SSE streams in quick succession showing a slowly climbing (not spiking) heap/timer count that never plateaus.

### Pitfall 3: Assuming chi middleware breaks `http.Flusher`/streaming
**What goes wrong:** A common (but, for this specific stack, WRONG) assumption is that `middleware.Logger`/`Recoverer` wrapping the `ResponseWriter` silently breaks `http.Flusher`, leading to defensive but unnecessary workarounds (e.g., bypassing the `/v1` middleware chain entirely for the SSE route, which would also drop rate-limiting/auth).
**Why it happens:** Some HTTP middleware libraries DO wrap `ResponseWriter` in ways that lose optional interfaces like `http.Flusher`/`http.Hijacker`.
**How to avoid:** This project's chi v5.3.0 `middleware.WrapResponseWriter` specifically detects and preserves `http.Flusher` (and `http.Hijacker`) via its `fancyWriter`/`flushWriter` variants, chosen based on what the underlying writer actually implements — verified by reading `middleware/wrap_writer.go` directly in this session. `http.NewResponseController(w).Flush()` is doubly safe here since `ResponseController` itself walks `Unwrap()` chains. No workaround is needed; keep the SSE route inside the normal `/v1` middleware stack (auth + rate-limit apply exactly as PUSH-01 requires).
**Warning signs:** N/A for this stack (verified false assumption) — would show up as `Flush` silently failing/no-op'ing if it were true.

### Pitfall 4: DB-poll load at scale — reasoning, not measurement, is sufficient here
**What goes wrong (hypothetical, not observed):** A naive worry that N concurrent SSE streams × 1 poll/sec each will overwhelm Postgres or exhaust the connection pool.
**Why it doesn't actually go wrong:** Each poll is a single indexed primary-key `SELECT ... WHERE id = $1` (`jobs_pkey`), sub-millisecond, and `pgxpool` only holds a connection for the duration of that one query — not for the lifetime of the SSE stream. This is architecturally identical to N clients directly polling `GET /v1/jobs/{id}` every second today (already the case before this phase), just moved server-side. No new connection-pool sizing or index work is required.
**How to avoid a REAL problem:** Ensure the Postgres pool's max-connections setting comfortably exceeds expected peak CONCURRENT query execution (not peak concurrent stream count — those are very different numbers, since each stream only borrows a connection for a sub-millisecond query once per poll interval). This is worth a one-line note in the plan's verification section, not a load-test gate.
**Warning signs:** `pg_stat_activity` showing sustained (not transient) connection-pool saturation under realistic concurrent-stream counts — not expected, but the cheap diagnostic if a false alarm needs settling.

### Pitfall 5: Synchronous inline webhook fire can stall the ffmpeg `-progress` pipe reader
**What goes wrong:** If `progressSink.Update`'s webhook POST is fired synchronously (no detached goroutine, no independent short timeout) and the client's `callback_url` endpoint is slow or hanging, the SAME goroutine that is draining ffmpeg's `-progress` pipe (Phase 41's `runCommandStreaming` reader) blocks on the HTTP call. ffmpeg's own write to fd 3 can then back up once the pipe's kernel buffer fills, and in the worst case this stalls or times out the transcode itself — the exact opposite of D-04's "must never fail or block the conversion."
**Why it happens:** `progressSink.Update` is deliberately the SAME call site as the DB write (by design, D-04, "one throttle decision drives both") — it's easy to assume "best-effort" only means "ignore the error," not "must also be non-blocking with respect to the caller."
**How to avoid:** Fire the HTTP POST from a short-lived, DETACHED goroutine bound to its OWN `context.WithTimeout(context.Background(), shortTimeout)` — NOT `attemptCtx` — so `Update` returns to its caller (the pipe reader) immediately regardless of remote endpoint latency (Pattern 3). Choose `shortTimeout` comfortably shorter than the PROG-03 throttle interval so at most a small, bounded number of these goroutines are ever concurrently in flight for one job.
**Warning signs:** A unit/integration test using a deliberately slow HTTP test server (e.g., `httptest.NewServer` with a handler that sleeps) as the `callback_url`, asserting that `progressSink.Update` returns promptly (within, say, 50ms) regardless of the test server's artificial delay — this is the load-bearing test for this pitfall, analogous to PROG-05's live SIGKILL proof but for the webhook path instead of the exec path.

### Pitfall 6: Forgetting the terminal-100% write bypass never fires a progress webhook
**What goes wrong (design gap, not a bug per se):** `runConvertWithOptionalProgress`'s post-success terminal write (`_ = repo.UpdateProgress(ctx, jobID, 100, nil)`, D-03a) calls `repo.UpdateProgress` DIRECTLY, bypassing `progressSink.Update` entirely — so if the webhook-fire logic lives only inside `Update` (Pattern 3, as recommended), the terminal 100% tick NEVER triggers a `"event":"progress"` webhook. A client relying solely on progress webhooks (no SSE, no polling) would see their last progress webhook at some value < 100%, followed by the SEPARATE terminal `"event":"completed"` webhook moments later — never an explicit `"progress_pct":100` webhook.
**Why it happens:** The terminal-write bypass exists specifically to guarantee the 100% DB write always lands even under throttle pressure (D-03a, Phase 41) — it was never designed with a webhook side-effect in mind, since Phase 41 predates PUSH-02.
**How to avoid / decide:** This is a genuine open design question for the planner (see Open Questions) — the two options are (a) accept this as intentional (the terminal `"completed"` webhook already conveys "it's done," so a redundant `progress:100` webhook adds no information) or (b) also fire a best-effort progress webhook from the terminal-write bypass site. CONTEXT.md does not address this explicitly. Recommend (a) — simpler, and avoids a near-simultaneous double-delivery race between the progress-100 and completed webhooks hitting the same endpoint within milliseconds of each other.
**Warning signs:** A plan-review or QA pass noticing "the client says they never saw progress_pct:100 via webhook" — the answer is "correct, by design (a)," not a bug, provided the planner documents the decision.

## Code Examples

### SSE payload shape (D-01, mirrors handleGetJob's null-aware fields)

```go
// Source: mirrors internal/api/handlers.go's handleGetJob response shape
// (job.ProgressPct/EtaSeconds are already *int, nil renders as JSON null).
type sseProgressPayload struct {
	Status      string `json:"status"`
	ProgressPct *int   `json:"progress_pct"`
	EtaSeconds  *int   `json:"eta_seconds"`
}

func emit(rc *http.ResponseController, w io.Writer, event string, job *jobs.Job) {
	payload := sseProgressPayload{Status: job.Status, ProgressPct: job.ProgressPct, EtaSeconds: job.EtaSeconds}
	b, err := json.Marshal(payload)
	if err != nil {
		return // best-effort telemetry -- never propagate a marshal error
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	_ = rc.Flush()
}

func emitTerminal(rc *http.ResponseController, w io.Writer, status string) {
	// D-02: final event carries ONLY {status} -- no download_url.
	fmt.Fprintf(w, "event: %s\ndata: {\"status\":%q}\n\n", "progress", status)
	_ = rc.Flush()
}

// snapshotOf produces a comparable value for D-01's emit-on-change gate.
// A plain struct (not a pointer) so `!=` works directly for the caller.
type sseSnapshot struct {
	Status string
	Pct    int // sentinel -1 for nil
	ETA    int // sentinel -1 for nil
}

func snapshotOf(job *jobs.Job) sseSnapshot {
	s := sseSnapshot{Status: job.Status, Pct: -1, ETA: -1}
	if job.ProgressPct != nil {
		s.Pct = *job.ProgressPct
	}
	if job.EtaSeconds != nil {
		s.ETA = *job.EtaSeconds
	}
	return s
}
```

### Test shape for Pitfall 5 (the load-bearing non-blocking proof)

```go
// Source: pattern for internal/worker/worker_test.go -- proves Update
// returns promptly even when the configured callback_url is slow,
// WITHOUT spinning up a live ffmpeg/Postgres/Handler.
func TestProgressSinkUpdateNonBlockingOnSlowWebhook(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // deliberately slower than webhookTimeout
	}))
	defer slow.Close()

	sink := &progressSink{
		ctx: context.Background(), repo: &spyProgressRepo{}, jobID: uuid.New(),
		minInterval: 0, minDeltaPct: 0,
		webhookEnabled: true, callbackURL: slow.URL,
		webhookSecret: []byte("test"), webhookDelivr: webhook.NewDeliverer(),
		webhookTimeout: 200 * time.Millisecond,
	}
	start := time.Now()
	sink.Update(convert.ProgressUpdate{Pct: 42})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("Update blocked for %v -- must return immediately regardless of a slow callback_url", elapsed)
	}
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| Client polls `GET /v1/jobs/{id}` on its own cadence, no server push | Server-side SSE (DB-poll-sourced) + opt-in webhook push | This phase (v1.9) | Client no longer needs its own polling loop for real-time progress; the polling burden simply moves server-side (Pitfall 4: architecturally equivalent load, not new) |
| Every webhook body has an implicit, single meaning (always "job finished") | Webhook bodies carry an explicit `event` discriminator | This phase (v1.9), additive | A client subscribed to `progress_webhook=true` can safely multiplex both event kinds on one `callback_url` without a second endpoint |

**Deprecated/outdated:** None — additive only, no prior mechanism is replaced (SSE/webhook progress push did not exist before this phase, and terminal webhook delivery is unchanged in structure).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `PROGRESS_WEBHOOK_TIMEOUT` default of ~3s (shorter than the 2s `PROGRESS_MIN_INTERVAL_SECONDS` default) is a reasonable starting point | Architecture Patterns Pattern 3 | No measured data on typical client callback endpoint latency exists yet — mirrors PROG-03's own "measure, don't assume" flagged defaults; if too short, legitimate-but-slightly-slow endpoints never get a successful progress webhook (silently, since best-effort); if too long, more detached goroutines can be briefly in flight under sustained slowness (still bounded, just wasteful) |
| A2 | The terminal-100% write (D-03a's throttle bypass) should NOT also fire a progress webhook — the terminal `"completed"` webhook already conveys completion | Common Pitfalls, Pitfall 6 | If wrong (i.e., the intended contract actually wants a progress:100 tick), a client integration test could reasonably expect one and not receive it; low risk since CONTEXT.md never specifies this case either way — flagged explicitly as a planner decision point, not asserted as fact |
| A3 | `SSE_POLL_INTERVAL`/`SSE_HEARTBEAT_INTERVAL`/`SSE_MAX_STREAM_DURATION` defaults of ~1s/~15s/~30m (taken directly from CONTEXT.md's own D-01/D-02 prose) are adequate without a load-test measurement pass | Architecture Patterns Pattern 1 | CONTEXT.md itself only says "~1s"/"~15s"/"~30m" (approximate, not measured) — REQUIREMENTS.md's "measure, don't assume" note applies to PROG's throttle gates explicitly but does not explicitly extend to these SSE cadences; low risk either way since they are trivially env-tunable post-deploy |
| A4 | `progress_webhook` should be silently normalized to `false` when `callback_url` is empty (rather than rejected with a 400) | Architecture Patterns Pattern 2 | If the intended contract actually wants a 400 for "opted in with no delivery target," this is a wrong-shaped error response; low risk — D-03's own prose ("A job with no callback_url... sends NO progress webhooks") reads as tolerant-by-design, matching this choice |

**None of the core mechanics (SSE headers/framing, `http.ResponseController` behavior through chi's wrap chain, the existing HMAC/deliver reuse, the exact migration-gap finding, the synchronous-blocking risk in Pitfall 5) are assumptions — all were verified by reading this repository's actual code or Go's documented stdlib behavior in this session.**

## Open Questions

1. **Should the terminal 100% progress write also fire a best-effort progress webhook?**
   - What we know: `runConvertWithOptionalProgress`'s terminal bypass calls `repo.UpdateProgress` directly, not through `progressSink.Update` — so as designed, it structurally CANNOT also fire a webhook without an explicit additional call site.
   - What's unclear: CONTEXT.md is silent on whether a client should ever see an explicit `progress:100` webhook, or whether the terminal `completed` webhook alone is sufficient.
   - Recommendation: Treat as NOT required (Assumption A2) — document the choice in the plan so a future bug report ("no progress:100 webhook") is recognized as by-design, not a regression.

2. **Exact env var names and defaults for the three SSE cadences and the progress-webhook timeout.**
   - What we know: CONTEXT.md explicitly delegates exact names to Claude's Discretion; this document recommends `SSE_POLL_INTERVAL` (default 1s), `SSE_HEARTBEAT_INTERVAL` (default 15s), `SSE_MAX_STREAM_DURATION` (default 30m), `PROGRESS_WEBHOOK_TIMEOUT` (default 3s) — following the existing `_SECONDS`-suffix-tolerant `envDurationSeconds`-style parsing precedent where a bare-integer-seconds value should also be accepted.
   - What's unclear: Nothing blocking — purely a naming/default choice, easily changed later since every value is env-tunable.
   - Recommendation: Adopt the names above unless the planner has a stronger preference; consistency with `PROGRESS_MIN_INTERVAL_SECONDS`'s existing naming style is the main constraint.

3. **Does `WEBHOOK_ALLOW_INSECURE_HTTP`/`WEBHOOK_ALLOW_PRIVATE_IPS`'s existing SSRF relaxation apply identically to progress-webhook delivery?**
   - What we know: Both flags are read by `validateCallbackURL` at job-CREATE time (API process) — the URL is validated ONCE, before storage, regardless of how many times or by which process it is later POSTed to (webhook-worker for terminal, av-worker inline for progress). Both delivery paths use the SAME already-validated `callback_url`.
   - What's unclear: Nothing — this is confirmed correct by construction (one validation call site, one stored URL, two delivery consumers of that one validated value). Called out here only because CONTEXT.md's "additional context" section explicitly asked this to be verified.
   - Recommendation: No new validation code needed; the plan should simply note in a comment (mirroring this research) that av-worker's inline delivery relies on the SAME upstream validation, never re-validating.

## Environment Availability

No new external tool/service/runtime dependency is introduced by this phase — SSE is stdlib `net/http`, and the progress webhook reuses the already-running `internal/webhook` package and the already-provisioned `WEBHOOK_SIGNING_SECRET`. The one environment change worth tracking is which PROCESS reads that secret:

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `WEBHOOK_SIGNING_SECRET` env var, readable by `cmd/av-worker` | Inline progress-webhook HMAC signing (D-04) | Partial — currently set in every deployment for `cmd/webhook-worker` only; `cmd/av-worker`'s process environment must ALSO have it set (docker-compose service env / k8s manifest) | — | If unset in av-worker's environment: progress webhooks are silently disabled for that replica (Pattern 3's `len(webhookSigningSecret) > 0` gate) — NOT a startup failure, since av-worker must remain runnable for deployments that never use `progress_webhook=true` |
| Go 1.26 toolchain | `http.ResponseController` (Go 1.20+, comfortably covered) | ✓ | go1.26.5 darwin/arm64 (local); go.mod pins `go 1.26.4` | — |
| chi v5.3.0 | `http.Flusher` survival through `middleware.Logger`/`Recoverer` wrap | ✓ | v5.3.0 (already pinned in go.mod, verified via source read) | — |

**Missing dependencies with no fallback:** None — `WEBHOOK_SIGNING_SECRET` has an explicit, documented, non-fatal fallback (feature silently off) rather than blocking av-worker startup.

**Missing dependencies with fallback:** `WEBHOOK_SIGNING_SECRET` in av-worker's environment, as above — the planner should include a docker-compose.yml / deployment-manifest task to actually SET this for the av-worker service, since "silently disabled" could otherwise go unnoticed in a real deployment.

## Validation Architecture

> `.planning/config.json` has `workflow.nyquist_validation: false` — included per the instruction's default-inclusion rule for completeness; not enforced as a gate per project config.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (project convention) |
| Config file | none — plain `go test ./...` |
| Quick run command | `go test ./internal/api/... ./internal/jobs/... ./internal/worker/...` |
| Full suite command | `go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PUSH-01 | SSE emits on change only, heartbeats, closes on terminal, closes on disconnect with no leaked goroutine, respects max-duration cap, mirrors ownership 404 | unit (httptest + canceled ctx; no live server needed) | `go test ./internal/api/... -run TestSSE -v` | ❌ Wave 0 (`internal/api/sse_test.go`) |
| PUSH-02 | Opt-in flag parsed/persisted; progress webhook fires only when opted-in + av-transcode; never enqueues to webhook_deliveries; terminal + progress bodies both carry `event`; inline fire never blocks the caller on a slow endpoint | unit (spy `progressWebhookDeliverer`/`progressRepo`; `httptest.NewServer` slow-handler for the non-blocking proof) | `go test ./internal/worker/... -run TestProgress -v` + `go test ./internal/jobs/... -run TestCreate -v` | ❌ Wave 0 (`internal/worker/worker_test.go` additions, `internal/jobs/repo_test.go` addition) |

### Sampling Rate
- **Per task commit:** `go test ./internal/api/... ./internal/jobs/... ./internal/worker/...`
- **Per wave merge:** `go test ./...`
- **Phase gate:** Full suite green before `/gsd:verify-work`; no live-process/SIGKILL-style gate is needed for this phase (unlike Phase 41's PROG-05) since neither PUSH-01 nor PUSH-02 spawns an external OS process — the non-blocking proof (Pitfall 5) is a plain `httptest`-based unit test, not an operator-run live script.

### Wave 0 Gaps
- [ ] `internal/db/migrations/0008_progress_webhook.sql` — the new column, plus `cmd/migrate` re-run in dev
- [ ] `internal/api/sse_test.go` — emit-on-change, heartbeat cadence, terminal close, disconnect close (canceled `httptest` request context), max-duration cap, 404-ownership parity with `handleGetJob`
- [ ] `internal/jobs/repo_test.go` addition — `progress_webhook` round-trips through `Create`/`Get`
- [ ] `internal/worker/worker_test.go` additions — `progressSink` webhook-fire gating (opted-in + av-only), the non-blocking-on-slow-endpoint proof (Pitfall 5's test), and confirmation that `h.enqueuer`/`h.webhookRepo` are never touched by the progress path (a spy asserting zero calls)
- [ ] `internal/worker/worker_test.go` addition — `HandleWebhookDeliver`'s body now includes `event:"completed"` (a simple body-shape assertion)
- [ ] Framework install: none — stdlib `testing` already covers everything

## Security Domain

`security_enforcement` is not present in `.planning/config.json` — treated as enabled per the instruction's default (mirrors Phase 41's treatment).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (unchanged) | `GET /v1/jobs/{id}/events` sits inside the existing `/v1` route group — same `auth.Middleware` ApiKey requirement as every other route (PUSH-01's explicit requirement); no new auth mechanism |
| V3 Session Management | no | N/A — stateless API-key auth, unchanged; an SSE stream carries no session state beyond the initial auth check |
| V4 Access Control | yes | SSE reuses `handleGetJob`'s EXACT ownership guard (`job.ClientID != client.ID` → 404, never 403) — no new access-control surface, same T-39-01 precedent |
| V5 Input Validation | yes | `progress_webhook` is a boolean parsed from a form value (`== "true"`, fail-safe default false) — no injection surface; `callback_url` reuses the existing, already-audited `validateCallbackURL` SSRF guard with NO second validation path introduced |
| V6 Cryptography | yes (reused, unchanged) | The progress webhook reuses `webhook.SignPayload`'s existing HMAC-SHA256 scheme verbatim — no new crypto code, no new secret-handling logic beyond a second process (av-worker) reading the SAME existing `WEBHOOK_SIGNING_SECRET` |
| V7 Error Handling / Logging | yes | Best-effort delivery/DB-write failures are dropped silently per the existing convention (`internal/*` never logs); no raw error text or stack trace is ever surfaced to the SSE stream or the webhook body — both carry only integers/enums/status strings, matching Phase 41's own V7 posture |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Cross-client progress leakage via `GET /v1/jobs/{id}/events` | Information Disclosure | Identical ownership guard to `handleGetJob`/`handleCancelJob` — a cross-client or nonexistent job id is 404, indistinguishable, on the SSE route too |
| SSRF via a second, unvalidated delivery-configuration URL | Tampering / Info Disclosure (SSRF) | Avoided by construction — `progress_webhook` carries NO URL of its own; it only gates delivery to the ALREADY-validated `callback_url` (no second `validateCallbackURL` call site to accidentally get wrong or skip) |
| Resource/connection exhaustion via many long-lived SSE streams | Denial of Service | Bounded by the existing per-client/per-IP rate limits (a new SSE connection is one request against the existing `ratelimit.PerClient`/`ratelimit.ByIP` budgets) plus the ~30m max-stream-duration cap (D-02) that guarantees no single stream holds a connection/goroutine forever |
| Stalling the transcode via a slow/malicious `callback_url` responder (self-inflicted, since the URL was chosen by the same client) | Denial of Service | The detached-goroutine-with-short-timeout design (Pattern 3/Pitfall 5) guarantees the ffmpeg progress-reader goroutine is never blocked by callback_url latency, regardless of how slow or adversarial the endpoint is |

## Sources

### Primary (HIGH confidence)
- Live command execution and source reads in this research session: `go doc net/http ResponseController` (confirms Go 1.20+ `ResponseController`/`Flush` API), direct read of the vendored `github.com/go-chi/chi/v5@v5.3.0/middleware/wrap_writer.go` (confirms `http.Flusher` survives `middleware.Logger`'s wrap via the `fancyWriter`/`flushWriter` variants), `go version` (go1.26.5 darwin/arm64, matches `go.mod`'s `go 1.26.4` pin)
- Direct reads of this repository's own code: `internal/api/handlers.go` (`handleGetJob`, `handleCreateJob`'s `callback_url`/opts parsing, `isTerminalStatus`), `internal/api/routes.go`, `internal/api/api.go`, `internal/jobs/repo.go` (`Get`, `Create`, `UpdateProgress`, `SetSourceDuration`, `transition`), `internal/jobs/jobs.go`, `internal/worker/worker.go` (full file, both halves — `progressSink`/`runConvertWithOptionalProgress`/`process`/`HandleWebhookDeliver`'s exact body-construction site), `internal/webhook/webhook.go`/`deliver.go`/`sign.go`, `internal/queue/queue.go` (`TypeWebhookDeliver`, `NewWebhookDeliverTask`, `MaxRetry=6`, backoff schedule), `internal/convert/progress.go`/`av.go` (`ProgressSink`/`ProgressConverter`/`ProgressUpdate` shapes), `internal/db/migrations/0001_init.sql` through `0007_job_progress.sql` (confirms no existing `progress_webhook` column), `cmd/av-worker/main.go` (confirms `deliverer`/`signingSecret` currently nil there), `cmd/api/main.go` (existing `envInt64`/`envDuration`/`firstField` helpers), `.env.example` (confirms `WEBHOOK_SIGNING_SECRET`'s current "webhook-worker-only" documentation)
- `.planning/phases/42-live-push/42-CONTEXT.md` — locked decisions D-01 through D-04
- `.planning/phases/41-progress-core/41-CONTEXT.md` and `41-RESEARCH.md` — the progress source/throttle gate this phase consumes, and the goroutine-leak discipline this phase's SSE side deliberately avoids needing (by having no extra goroutine at all)
- `.planning/REQUIREMENTS.md` — PUSH-01, PUSH-02, and the "measure, don't assume" note that motivates flagging A1/A3 as assumptions rather than settled defaults

### Secondary (MEDIUM confidence)
- None distinct from Primary — every claim in this document traces to either a direct source read/tool execution in this session or an existing, already-verified Phase 41 finding.

### Tertiary (LOW confidence)
- None — every technical claim was either verified by reading this repository's actual code, verified via `go doc`/source-read of the pinned dependency versions, or explicitly logged in the Assumptions table above.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies; every mechanism (SSE framing, `ResponseController`, HMAC reuse) verified against stdlib docs or this repo's actual pinned dependency source
- Architecture: HIGH — the SSE handler shape and the progress-webhook fire path were both derived directly from reading the exact existing call sites they extend (`handleGetJob`, `progressSink.Update`, `HandleWebhookDeliver`'s body map), not from a generic template
- Pitfalls: HIGH for the SSE-side pitfalls (verified via source read, not assumption) and for the migration-gap/wiring-gap findings (verified by reading every migration file and `cmd/av-worker/main.go` directly); MEDIUM for the exact timeout/env-default numbers (explicitly flagged as assumptions, A1/A3) and for the terminal-100%-webhook design question (Pitfall 6/A2, explicitly surfaced as an open decision rather than asserted)

**Research date:** 2026-07-25
**Valid until:** 30 days (stable domain — stdlib SSE mechanics and this project's own existing webhook/progress infrastructure are not fast-moving; re-verify only if chi is upgraded past v5.3.0 or Go is upgraded past the current 1.26 pin)
