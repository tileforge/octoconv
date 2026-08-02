---
phase: 44-async-pipeline-wiring
plan: 02
subsystem: queue
tags: [asynq, go, ocr, retry-schedule, unique-ttl]

# Dependency graph
requires:
  - phase: 44-async-pipeline-wiring
    provides: "Plan 01 — OCRConverter registered, migration 0009 adds 'ocr' to jobs.engine CHECK, EngineOCR upload ceiling"
provides:
  - "TypeOCRConvert/QueueOCR asynq task type + queue tied to convert.EngineOCR"
  - "NewOCRConvertTask cloning NewAVConvertTask (reuses ConvertPayload)"
  - "ocrRetrySchedule (15s/1m, provisional) + OCRRetryDelay + ocrBackoffSum"
  - "OCRUniqueTTL derived FRESH from OCR's own maxRetry/engineTimeout budget (D-04/IN-02)"
  - "RetryDelayFunc routes TypeOCRConvert to OCRRetryDelay, not the webhook schedule"
  - "AllConvertQueues() + its completeness guard now cover EngineOCR"
  - "Client.EnqueueOCRConvert + ocrMaxRetry/ocrUniqueTTL fields, wired via OCR_MAX_RETRY/OCR_ENGINE_TIMEOUT env"
affects: ["44-03-worker-and-reconciler", "44-04-api-routing", "45-ocr-containerization"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-engine-class asynq task/queue/retry-schedule/unique-TTL clone (6th application: image/document/html/audio/av/ocr)"
    - "XUniqueTTL derived from the engine's OWN retry/timeout budget, never copied from another engine (IN-02)"

key-files:
  created: []
  modified:
    - internal/queue/queue.go
    - internal/queue/client.go
    - internal/queue/queue_test.go

key-decisions:
  - "ocrRetrySchedule set to 15s/1m (provisional, 2 entries mirroring avRetrySchedule's shape since OCR_MAX_RETRY defaults to 2 like AV_MAX_RETRY) — shorter than av's 30s/2m since a tesseract/pdftoppm attempt is expected to be materially cheaper per-page than an ffmpeg transcode. Documented as provisional pending Phase 45's measured OCR_ENGINE_TIMEOUT."
  - "OCR_ENGINE_TIMEOUT default 300s (mirrors DOCUMENT_ENGINE_TIMEOUT's 300s default and the AV_ENGINE_TIMEOUT [ASSUMED]-placeholder precedent) — a Go-code placeholder this phase, superseded by Phase 45's measured value."
  - "OCRUniqueTTL reuses uniqueTTLSafetyMargin verbatim (no ocr-specific margin const) — same discipline as every other engine class's UniqueTTL."

requirements-completed: []  # OCRP-01 is a multi-plan end-to-end requirement; NOT marked complete here per plan instructions — 44-03/44-04 close it

# Metrics
duration: 4min
completed: 2026-07-26
---

# Phase 44 Plan 02: OCR Queue/Task Wiring Summary

**Dedicated `ocr` asynq queue/task type with a fresh-derived OCRUniqueTTL (never copied from AV/Audio) and a client-level EnqueueOCRConvert, mechanically cloning the av engine-class wiring.**

## Performance

- **Duration:** 4 min (commit-to-commit span)
- **Started:** 2026-07-26T18:22:39+03:00
- **Completed:** 2026-07-26T18:26:16+03:00
- **Tasks:** 2 completed
- **Files modified:** 3 (internal/queue/queue.go, internal/queue/client.go, internal/queue/queue_test.go)

## Accomplishments
- Added `TypeOCRConvert`/`QueueOCR` (tied to `convert.EngineOCR`, DEBT-02 discipline) and `NewOCRConvertTask` cloning `NewAVConvertTask` exactly, reusing `ConvertPayload`.
- Added a provisional `ocrRetrySchedule` (15s, 1m) + `OCRRetryDelay` + `ocrBackoffSum`, and wired `TypeOCRConvert` into `RetryDelayFunc`'s switch so OCR tasks no longer silently fall through to the webhook/default schedule.
- Derived `OCRUniqueTTL(maxRetry, engineTimeout)` from OCR's OWN budget — `(maxRetry+1)*engineTimeout + ocrBackoffSum(maxRetry) + uniqueTTLSafetyMargin` — and proved by test that it scales with its own `engineTimeout` argument (not a copy of `AVUniqueTTL`/`AudioUniqueTTL`).
- Extended `AllConvertQueues()` and its completeness guard (`TestAllConvertQueuesCoversEveryEngine`) to require `convert.EngineOCR`.
- Added `Client.ocrMaxRetry`/`ocrUniqueTTL` fields, wired `NewClient` to read `OCR_MAX_RETRY` (default 2) and `OCR_ENGINE_TIMEOUT` (default 300s, provisional), and added `EnqueueOCRConvert` mirroring `EnqueueAVConvert` exactly (errors wrapped with `%w`).
- Added a client-level fresh-derivation proof (`TestClientOCRUniqueTTLDiffersFromAVAndAudio`) confirming `ocrUniqueTTL` is not byte-equal to `avUniqueTTL`/`audioUniqueTTL` under default env.

## Task Commits

Each task followed strict RED→GREEN TDD, committed atomically:

1. **Task 1: OCR task type, queue, retry schedule + OCRUniqueTTL + AllConvertQueues**
   - `5796dda` test(44-02): add failing test for ocr task/queue/retry/TTL (RED)
   - `29a0af0` feat(44-02): add ocr task type, queue, retry schedule + OCRUniqueTTL + AllConvertQueues (GREEN)
2. **Task 2: Client OCR fields + NewClient wiring + EnqueueOCRConvert**
   - `b33554d` test(44-02): add failing test for client EnqueueOCRConvert + fresh-TTL proof (RED)
   - `d4c167f` feat(44-02): add client ocrMaxRetry/ocrUniqueTTL fields + EnqueueOCRConvert (GREEN)

## TDD Gate Compliance

Both tasks show a `test(...)` commit immediately followed by a `feat(...)` commit in `git log`, confirming the RED→GREEN gate sequence was honored for both. No REFACTOR commit was needed (no cleanup required after either GREEN step).

## Files Created/Modified
- `internal/queue/queue.go` — `TypeOCRConvert`, `QueueOCR`, `NewOCRConvertTask`, `ocrRetrySchedule`, `OCRRetryDelay`, `ocrBackoffSum`, `OCRUniqueTTL`, `RetryDelayFunc` TypeOCRConvert case, `AllConvertQueues()` update.
- `internal/queue/client.go` — `Client.ocrMaxRetry`/`ocrUniqueTTL` fields, `NewClient` env wiring (`OCR_MAX_RETRY`/`OCR_ENGINE_TIMEOUT`), `EnqueueOCRConvert`.
- `internal/queue/queue_test.go` — `TestOCRConvertTaskRoundTrip`, `TestOCRRetryDelaySchedule`, `TestRetryDelayFuncRoutesOCRConvert`, `TestOCRUniqueTTL` (incl. the fresh-derivation and zero-margin proofs), `TestClientOCRUniqueTTLDiffersFromAVAndAudio`, `TestEnqueueOCRConvert` (REDIS_ADDR-gated), and the extended `TestAllConvertQueuesCoversEveryEngine`.

## Decisions Made
- `ocrRetrySchedule` = 15s/1m (provisional, 2 entries, no jitter) — mirrors `avRetrySchedule`'s shape since `OCR_MAX_RETRY` defaults to 2 like `AV_MAX_RETRY`, but shorter given OCR's expected lower per-attempt cost than av's ffmpeg transcode. Documented in-code as provisional pending Phase 45's measured `OCR_ENGINE_TIMEOUT`.
- `OCR_ENGINE_TIMEOUT` default = 300s (mirrors `DOCUMENT_ENGINE_TIMEOUT`'s 300s default and the `AV_ENGINE_TIMEOUT` `[ASSUMED]`-placeholder precedent) — a Go-code default this phase, to be superseded entirely by Phase 45's measured value.
- `OCRUniqueTTL` reuses the shared `uniqueTTLSafetyMargin` constant verbatim (no ocr-specific margin) — consistent with every other engine class's `XUniqueTTL`.

## Deviations from Plan

None — plan executed exactly as written. Both tasks matched the plan's `<action>`/`<behavior>` specs precisely; no Rule 1/2/3/4 deviations were needed.

## Issues Encountered

`gofmt` flagged a spacing inconsistency in a newly-added test block (backoffSum/comment alignment) after the first RED-phase edit; ran `gofmt -w` before running tests, which is standard formatting hygiene, not a deviation from plan scope.

## User Setup Required

None — no external service configuration required. `OCR_MAX_RETRY`/`OCR_ENGINE_TIMEOUT` are optional env overrides with safe code defaults; Phase 45 will supply the measured `OCR_ENGINE_TIMEOUT` for production.

## Next Phase Readiness

- Plan 03 (worker + reconciler) can now call `queue.NewOCRConvertTask`/`OCRUniqueTTL`/`Client.EnqueueOCRConvert` to route OCR jobs through `HandleOCRConvert` on the `ocr` queue.
- Plan 04 (API routing / `cmd/api/main.go` queue-depth collector) picks up `AllConvertQueues()` automatically now that it includes `QueueOCR` — no separate wiring needed there.
- `OCRP-01` is NOT marked fully complete by this plan alone (per plan instructions) — end-to-end closure happens in 44-03/44-04.
- No blockers for Plan 03/04.

---
*Phase: 44-async-pipeline-wiring*
*Completed: 2026-07-26*

## Self-Check: PASSED

All created/modified files found on disk; all 4 task commit hashes (5796dda, 29a0af0, b33554d, d4c167f) verified present in git log.
