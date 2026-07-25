# Phase 42 Discussion Log

**Date:** 2026-07-25
**Mode:** discuss (default)

## Framing

Live Push (PUSH-01 SSE, PUSH-02 opt-in progress webhook) — pure consumer of the Phase 41 progress columns. Carried forward without re-asking (code scout + prior phases): ApiKey auth + 404-ownership on every route (SSE too); stateless API → SSE MUST be DB-poll-sourced, no in-memory pub/sub across replicas; existing webhook infra (HMAC-signed body, MaxRetry=6 + backoff 30s→15m + dead-letter, minimal job-id payload, best-effort enqueue) is the TERMINAL durable path; Phase 41 throttle knobs; "progress-write never fails the conversion" discipline.

## Gray areas presented (all 4 selected)

**A. SSE payload + cadence** — poll interval, event shape, emit policy, heartbeat.
- **Decision (D-01):** DB-poll-sourced, ~1s poll (env-tunable), emit-on-change only, payload {status, progress_pct, eta_seconds} null-aware, named `progress` event, ~15s heartbeat comment.

**B. SSE lifecycle / close** — terminal close, disconnect, download_url, max-cap.
- **Decision (D-02):** final event with {status} then close; NO download_url in stream (client does GET); clean disconnect (zero goroutine leak); ~30m env max-stream-duration safety cap.

**C. Opt-in + event discriminator** — how opt-in is expressed, discriminator scope.
- **Decision (D-03):** new top-level form flag `progress_webhook=true` alongside callback_url (not in opts); additive `event` field on BOTH progress and terminal bodies (both hit the same callback_url → client must discriminate); additive = backward-compat-safe.

**D. Progress-webhook delivery path** — where it fires, throttle, retry separation.
- **Decision (D-04):** worker inline best-effort single-attempt, HMAC-signed, reuse the PROG-03 throttle gate, only opted-in av-transcode; NEVER enqueues to webhook_deliveries/asynq retry — PUSH-02 separation guaranteed by construction; best-effort/error-ignored so it never fails the conversion.

## Deferred

- WebSocket/long-poll → v1.9 SSE-only.
- Progress for non-av engines / generalized progress bus → out of scope.
- OCR/archive/CAD/preview engine classes → deferred seeds, captured after v1.9 closes.
