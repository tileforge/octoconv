---
phase: 42-live-push
plan: 03
subsystem: worker
tags: [webhook, hmac, progress, av-transcode, asynq, detached-goroutine, best-effort-delivery]

requires:
  - phase: 42-live-push (plan 01)
    provides: "jobs.ProgressWebhook per-job opt-in flag + jobs.CallbackURL"
  - phase: 41-progress-core
    provides: "progressSink / runConvertWithOptionalProgress / SetProgressThrottle — the PROG-03 throttle gate this plan's webhook fire reuses"
provides:
  - "event discriminator on BOTH webhook bodies: progress body carries event=progress, terminal body carries event=completed (buildTerminalWebhookBody)"
  - "progressSink.fireDetached: a detached, single-attempt, HMAC-signed inline progress-webhook POST fired from the same PROG-03 throttle decision that drives the DB write, structurally incapable of enqueuing to webhook_deliveries/the asynq retry queue"
  - "Handler.SetProgressWebhook post-construction setter + cmd/av-worker wiring (WEBHOOK_SIGNING_SECRET + PROGRESS_WEBHOOK_TIMEOUT env, guarded, before srv.Start)"
affects: [42-04-live-gate]

tech-stack:
  added: []
  patterns:
    - "progressWebhookDeliverer: a narrow interface-segregated seam (single Deliver method) mirroring progressRepo's own segregation — spy-testable without a live HTTP webhook.Deliverer"
    - "Detached fire-and-forget goroutine bound to its OWN context.WithTimeout(context.Background(), ...) rather than the caller's ctx — the load-bearing non-blocking guarantee for a hostile/slow callback endpoint (Pitfall 5)"
    - "buildTerminalWebhookBody: a small pure function factored out of HandleWebhookDeliver (mirrors avFailureCode's precedent) purely so the additive event discriminator is unit-testable without a live Postgres/S3/webhook Handler"

key-files:
  created: []
  modified:
    - internal/worker/worker.go
    - internal/worker/worker_test.go
    - cmd/av-worker/main.go
    - .env.example

key-decisions:
  - "PROGRESS_WEBHOOK_TIMEOUT default 1500ms, deliberately shorter than PROGRESS_MIN_INTERVAL_SECONDS's 2s default (plan-checker W1) — bounds in-flight detached goroutines to at most one per job at any instant"
  - "The D-03a terminal-100 write stays a direct repo.UpdateProgress call, bypassing the sink entirely — it fires NO progress webhook by design (A2): the terminal `completed` webhook already covers job completion, so a duplicate progress=100 fire would be redundant"
  - "WEBHOOK_SIGNING_SECRET is reused verbatim by cmd/av-worker (no new secret, no rotation) — av-worker fails OPEN (progress webhooks silently disabled) rather than closed when unset, since it must remain runnable for deployments that never opt into progress_webhook=true"

requirements-addressed: [PUSH-02]
requirements-completed: [PUSH-02]

duration: ~40min
completed: 2026-07-25
---

# Phase 42 Plan 03: Progress-Webhook Delivery Summary

**Adds an opt-in, HMAC-signed `progress` webhook event fired from a detached, single-attempt, own-timeout goroutine inside the Phase 41 progressSink — reusing the existing PROG-03 throttle decision and structurally incapable of touching the terminal `done`/`failed` bounded-retry/dead-letter queue — plus the additive `event` discriminator on both webhook bodies and cmd/av-worker's real `webhook.Deliverer` wiring.**

## Performance

- **Duration:** ~40 min
- **Tasks:** 3/3 completed
- **Files modified:** 4 (0 created, 4 modified)

## Accomplishments

- `progressWebhookDeliverer` (interface-segregated, single `Deliver` method) + `progressSink` webhook fields (`webhookEnabled`/`callbackURL`/`webhookSecret`/`webhookDelivr`/`webhookTimeout`) + `fireDetached` — fired from `Update` immediately after the existing throttled `repo.UpdateProgress` write, gated by `webhookEnabled && callbackURL != "" && webhookDelivr != nil`. `fireDetached` marshals `{"job_id","event":"progress","progress_pct","eta_seconds"}`, then spawns exactly one `go func()` bound to its own `context.WithTimeout(context.Background(), webhookTimeout)` — never the caller's `ctx` — signs with the existing `webhook.SignPayload`, and calls `Deliver` once with the error discarded.
- `runConvertWithOptionalProgress`'s signature widened to thread the webhook config onto the constructed `progressSink`; `process()` passes `job.ProgressWebhook`/`job.CallbackURL`/`h.progressWebhookDelivr`/`h.progressWebhookSecret`/`h.progressWebhookTimeout` at the call site. Every non-opted-in job and every non-av-worker cmd passes `webhookEnabled=false`/`webhookDelivr=nil`, keeping the DB-write path byte-identical to before this plan.
- `buildTerminalWebhookBody` factors the terminal body's `job_id`/`status` construction out of `HandleWebhookDeliver` and additively sets `body["event"] = "completed"` — the SOLE terminal-body site. `download_url`/`error_code`/`error_message` branches, `SignPayload`, `RecordAttempt`/`MarkDeadLetter`, and `MaxRetry` are all unchanged (verified: 3 occurrences before and after).
- `Handler.SetProgressWebhook(d, secret, timeout)` — a post-construction setter mirroring `SetProgressThrottle`'s precedent. `cmd/av-worker/main.go` reads `WEBHOOK_SIGNING_SECRET` + `PROGRESS_WEBHOOK_TIMEOUT` (default 1500ms) and calls it, guarded by `len(secret) > 0`, before `srv.Start(mux)`. When the secret is unset, progress webhooks are silently disabled and av-worker still starts.
- `.env.example`: corrected the `WEBHOOK_SIGNING_SECRET` comment (no longer webhook-worker-only) and documented `PROGRESS_WEBHOOK_TIMEOUT`.
- 5 new tests in `internal/worker/worker_test.go`: `TestProgressWebhookFire_NonBlocking` (a 2s-sleeping httptest server + 200ms webhookTimeout proves `Update` returns in <50ms), `TestProgressWebhookGating` (opted-out / empty callback_url / nil deliverer all fire zero times), `TestProgressWebhookNeverEnqueues` (a "poisoned" `progressRepo` exposing `EnqueueWebhookDeliver`/`RecordAttempt`-shaped methods proves the progress path calls neither, at the call level not just by grep), and `TestBuildTerminalWebhookBody_EventDiscriminator` (asserts the marshaled terminal body contains `"event":"completed"`).

## Task Commits

Each task followed strict RED→GREEN TDD (Tasks 1 and 2; Task 3 is non-TDD wiring):

1. **Task 1: Detached best-effort progress-webhook fire** — RED `43a30f9` (test), GREEN `9224c4a` (feat)
2. **Task 2: Terminal webhook body event discriminator** — RED `c526222` (test), GREEN `4a07903` (feat)
3. **Task 3: Wire av-worker to sign + deliver inline progress webhooks** — `eeca328` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/worker/worker.go` — `progressWebhookDeliverer` interface, `progressSink` webhook fields + `fireDetached`, widened `runConvertWithOptionalProgress` signature, `Handler.progressWebhookDelivr/Secret/Timeout` fields + `SetProgressWebhook` setter, `buildTerminalWebhookBody` + `body["event"] = "completed"` in `HandleWebhookDeliver`
- `internal/worker/worker_test.go` — updated 4 existing `runConvertWithOptionalProgress` call sites for the widened signature + 5 new tests (non-blocking proof, opt-in gating, never-enqueue spy, terminal-event assertion)
- `cmd/av-worker/main.go` — reads `WEBHOOK_SIGNING_SECRET`/`PROGRESS_WEBHOOK_TIMEOUT`, calls `h.SetProgressWebhook(webhook.NewDeliverer(), secret, timeout)` guarded by `len(secret) > 0`, before `srv.Start(mux)`; startup log line
- `.env.example` — `WEBHOOK_SIGNING_SECRET` comment corrected + `PROGRESS_WEBHOOK_TIMEOUT` documented

## Decisions Made

- `PROGRESS_WEBHOOK_TIMEOUT` default 1500ms — deliberately shorter than `PROGRESS_MIN_INTERVAL_SECONDS`'s 2s default (plan-checker W1) so at most one detached fire is ever in flight per job.
- The D-03a terminal-100 write stays a direct `repo.UpdateProgress` call (unchanged from Phase 41), bypassing the sink and firing no progress webhook — the terminal `completed` webhook already covers completion (A2, accepted per 42-RESEARCH.md).
- `av-worker` reuses the SAME `WEBHOOK_SIGNING_SECRET` as `cmd/webhook-worker` — no new secret, no key rotation — and fails OPEN (silently disabled) rather than closed when the secret is unset, since av-worker must remain runnable for deployments that never set `progress_webhook=true`.

## Deviations from Plan

None — plan executed exactly as written. The only implementation-detail choice left to discretion was factoring `buildTerminalWebhookBody` out as a small pure function (mirroring the existing `avFailureCode` precedent in the same file) purely to make the additive `event` discriminator unit-testable without a live Postgres/S3/webhook `Handler` — no behavior change, the terminal body's shape/ordering/values are identical to the inline map literal the plan described.

## Issues Encountered

None.

## User Setup Required

None — no new external service configuration required. `PROGRESS_WEBHOOK_TIMEOUT` and `WEBHOOK_SIGNING_SECRET` are already-documented env vars (the latter pre-existing, reused verbatim).

## Verification

- `go build ./...` and `go vet ./...` clean (whole repo).
- `gofmt -l` clean on all touched files.
- `go test ./internal/worker/... -race -v` green, including all 5 new tests plus the full pre-existing suite (no regressions); `DATABASE_URL=… go test ./internal/... -race -count=1` green across every package (api/jobs/webhook/worker/etc).
- Acceptance-criteria greps: `fireDetached`/`progressWebhookDeliverer` present (11 occurrences); `context.WithTimeout(context.Background()` used for the detached fire (not `s.ctx`); `sed`-scoped grep on `fireDetached`'s body for `enqueuer|EnqueueWebhookDeliver|NewWebhookDeliverTask|webhookRepo|.Enqueue` == 0; progress body carries `"event": "progress"`; terminal body carries `body["event"] = "completed"`; `MaxRetry|MarkDeadLetter|RecordAttempt` occurrence count unchanged (3 before, 3 after); `SetProgressWebhook` defined in worker.go and called in cmd/av-worker/main.go before `srv.Start(mux)` (line 142 vs 177); `WEBHOOK_SIGNING_SECRET`/`PROGRESS_WEBHOOK_TIMEOUT` both read in cmd/av-worker/main.go; `.env.example` documents `PROGRESS_WEBHOOK_TIMEOUT` and no longer says "webhook-worker-only".

## Known Stubs

None. Every path is wired end-to-end with a real HMAC signer, a real (or spy, in tests) HTTP deliverer, and real env-driven config — no mocked/hardcoded data path exists in shipped code (only in test doubles, as expected).

## Threat Flags

None beyond what `42-03-PLAN.md`'s own `<threat_model>` (T-42-07/T-42-08/T-42-09/T-42-10) already anticipated and this plan closes:

- T-42-07 (DoS via slow/hanging callback_url): closed by `fireDetached`'s own short-lived detached goroutine + independent `context.WithTimeout`.
- T-42-08 (budget mixing / progress leaking into the terminal retry queue): closed by construction — no enqueue call exists in the progress path, proved both by grep and by `TestProgressWebhookNeverEnqueues`'s poisoned-repo spy.
- T-42-09 (forged progress webhook body): closed by reusing the existing `webhook.SignPayload` HMAC scheme unchanged.
- T-42-10 (SSRF via a second/re-validated URL): accepted as documented — the inline delivery reuses the SAME already-validated `callback_url`, no new validation surface introduced.

## Next Phase Readiness

- PUSH-02 is now fully implemented, unit-tested, and marked **complete** in REQUIREMENTS.md.
- 42-04 (the SSE live-proof operator gate, PUSH-01) is unblocked — it depends only on 42-02's SSE endpoint, already complete.
- No blockers.

---
*Phase: 42-live-push*
*Completed: 2026-07-25*

## Self-Check: PASSED

- `internal/worker/worker.go` — FOUND
- `internal/worker/worker_test.go` — FOUND
- `cmd/av-worker/main.go` — FOUND
- `.env.example` — FOUND
- `.planning/phases/42-live-push/42-03-SUMMARY.md` — FOUND
- `43a30f9` (test: Task 1 RED) — FOUND
- `9224c4a` (feat: Task 1 GREEN) — FOUND
- `c526222` (test: Task 2 RED) — FOUND
- `4a07903` (feat: Task 2 GREEN) — FOUND
- `eeca328` (feat: Task 3) — FOUND
