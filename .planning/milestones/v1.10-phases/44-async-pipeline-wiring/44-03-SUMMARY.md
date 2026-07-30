---
phase: 44-async-pipeline-wiring
plan: 03
subsystem: worker
tags: [asynq, ocr, tesseract, pdftoppm, reconciler, retry-classification, progress]

# Dependency graph
requires:
  - phase: 44-01
    provides: matchPDF sniffer + OCRConverter registration + jobs.engine CHECK migration (0009)
  - phase: 44-02
    provides: TypeOCRConvert/QueueOCR/OCRUniqueTTL + Client.EnqueueOCRConvert
provides:
  - HandleOCRConvert (worker.go) — the ocr queue's asynq task handler, cloning HandleAVConvert's simple shape
  - isOCRTerminal + ocrFailureCode — D-04 stage-aware terminal/transient classifier + client-facing error_code mapping
  - cmd/ocr-worker/main.go — the ocr-class worker process (asynq server bound to queue.QueueOCR, cancel subscriber, progress throttle, progress webhook)
  - reconciler EngineOCR routing — a stranded engine='ocr' job now recovers onto the ocr queue via EnqueueOCRConvert
affects: [44-04-integration-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Stage-aware terminal classifier with an OWN dedicated non-timeout-crash signature list (terminalOCRSignatures), diverging from isAudioTerminal's/isAVTerminal's default-to-transient-on-unrecognized-non-timeout-error shape"
    - "cmd/<engine>-worker main.go clone-minus-engine-specific-env, mirroring the audio/av/document/html worker precedent exactly"

key-files:
  created:
    - cmd/ocr-worker/main.go
  modified:
    - internal/worker/worker.go
    - internal/worker/worker_test.go
    - internal/reconciler/reconciler.go
    - internal/reconciler/reconciler_test.go

key-decisions:
  - "isOCRTerminal treats a non-timeout tesseract/pdftoppm/pdfinfo crash as TERMINAL via a dedicated terminalOCRSignatures prefix list (\"ocr: tesseract:\", \"ocr: pdftoppm:\", \"pdfinfo:\"), checked before the shared isTerminal fallthrough — this diverges deliberately from isAudioTerminal's/isAVTerminal's own non-timeout-default-to-shared-isTerminal (transient) treatment, because D-04 (44-CONTEXT.md) explicitly classifies OCR as deterministic on its input at every stage, unlike audio/av's server-produced-intermediate-input assumption."
  - "cmd/ocr-worker does NOT re-register a configured OCRConverter (unlike av-worker's AVConverter re-Register) — converters.go's init() already registered the zero-value OCRConverter{} (uncapped MaxImagePixels/MaxPages this phase); OCR_MAX_PAGES/pixel-cap env wiring is explicitly Phase 45's job."
  - "OCRE-05 blank->done is asserted at the runConvertWithOptionalProgress seam (the exact call HandleOCRConvert's process() makes), not via a full live-Postgres/S3 Handler — mirrors the existing codebase precedent (no Handle*Convert has a full-Handler unit test anywhere; process()'s h.repo/h.store are concrete types per ARCHITECTURE.md's own note). Live E2E queued->active->done proof is 44-04's job."

patterns-established:
  - "isOCRTerminal: input-fault sentinels always terminal -> timeout explicitly transient -> dedicated engine-crash signature list terminal -> shared isTerminal fallthrough for genuinely unrecognized errors"

requirements-completed: [OCRE-05]

# Metrics
duration: 12min
completed: 2026-07-26
---

# Phase 44 Plan 03: OCR Worker Wiring Summary

**HandleOCRConvert + isOCRTerminal/ocrFailureCode + cmd/ocr-worker + reconciler ocr routing — OCR jobs now flow queued -> active -> done through a dedicated asynq worker with stage-aware retry classification.**

## Performance

- **Duration:** ~12 min (3 commits between 18:38 and 18:43 UTC+3, plus investigation/reading time)
- **Started:** 2026-07-26T18:30:00+03:00 (approx.)
- **Completed:** 2026-07-26T18:43:00+03:00
- **Tasks:** 3/3 completed
- **Files modified:** 5 (1 created, 4 modified)

## Accomplishments

- `isOCRTerminal` + `ocrFailureCode` classify every OCR outcome: the four Phase-43 guard sentinels (`ErrOCRBornDigital`/`ErrOCRTooManyPages`/`ErrOCRPageBoxTooLarge`/`ErrOCRImageTooLarge`) plus unknown-language/unsupported-target rejections are always terminal; an `OCR_ENGINE_TIMEOUT` expiry is explicitly transient (mirrors `isAVTerminal`'s transcode-timeout treatment, the single row that distinguishes this classifier from `isDocumentTerminal`/`isHTMLTerminal`); any other non-timeout tesseract/pdftoppm/pdfinfo crash is terminal via a dedicated signature list (D-04: OCR is deterministic on its input at every stage).
- `HandleOCRConvert` clones `HandleAVConvert`'s simple shape (no guard splice — `OCRConverter` self-contains its own guards): strict `OCROptsFromMap` re-parse before `MarkActive`, `isOCRTerminal`/`ocrFailureCode` on failure, both `metrics.RecordJobOutcome` calls tagged `queue.QueueOCR`, Postgres-first webhook enqueue.
- `cmd/ocr-worker/main.go` consumes `queue.QueueOCR`: cancel subscriber wired verbatim, `SetProgressThrottle` called **before** `srv.Start` (SC4 — without this the `ProgressConverter` seam never fires and OCR progress silently never ramps), `SetProgressWebhook` gated on `WEBHOOK_SIGNING_SECRET`, no `AV_*` env copied.
- Reconciler recovers a stranded `engine='ocr'` job onto the ocr queue via a new `case convert.EngineOCR: EnqueueOCRConvert` arm, proven both by a dedicated routing test and the existing every-engine-constant completeness guard.
- OCRE-05 (blank/text-free page completes `done`, not `failed`) is proven at the exact seam `HandleOCRConvert`'s `process()` call uses (`runConvertWithOptionalProgress`): a fake converter writing an empty output file returns `nil`, the branch that leads straight to `MarkDone`.

## Task Commits

Each task was committed atomically:

1. **Task 1: isOCRTerminal + ocrFailureCode** - `e48783a` (feat)
2. **Task 2: HandleOCRConvert + reconciler ocr routing** - `d49a270` (feat)
3. **Task 3: cmd/ocr-worker/main.go + OCRE-05 blank->done assertion** - `2c93b06` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/worker/worker.go` - added `terminalOCRSignatures`, `isOCRTerminal`, `ocrFailureCode`, `HandleOCRConvert`
- `internal/worker/worker_test.go` - `TestIsOCRTerminal`, `TestOCRFailureCode`, `fakeBlankOCRConverter` + `TestOCRBlankPageConvertSucceeds_OCRE05`
- `internal/reconciler/reconciler.go` - `EnqueueOCRConvert` added to the `enqueuer` interface + `case convert.EngineOCR` in the recover switch
- `internal/reconciler/reconciler_test.go` - `fakeEnqueuer.EnqueueOCRConvert`/`ocrCallIDs`, `TestSweepRoutesOCRJobsToOCRQueue`, `convert.EngineOCR` row added to `TestSweepRoutesEveryEngineConstant`
- `cmd/ocr-worker/main.go` (new) - the ocr-class worker process, cloned from `cmd/av-worker/main.go` minus every `AV_*` env/re-register block

## Decisions Made

- `isOCRTerminal`'s non-timeout-crash treatment (terminal via a dedicated signature list) deliberately diverges from `isAudioTerminal`/`isAVTerminal` (which default an unrecognized non-timeout error to the shared `isTerminal`'s transient verdict) — see `key-decisions` above for the full rationale (D-04, 44-CONTEXT.md).
- `cmd/ocr-worker` does not re-register a configured `OCRConverter`; the zero-value registration from `converters.go`'s `init()` is used as-is this phase (Phase 45 wires the real caps).
- The `OCRE-05` blank->done contract is proven at the `runConvertWithOptionalProgress` seam rather than via a full live-Handler test, following the codebase's own precedent (no `Handle*Convert` handler has ever had a full-Handler unit test — `process()`'s `h.repo`/`h.store` are concrete, not interface, types). The live queued->active->done proof belongs to 44-04.

## Deviations from Plan

None — plan executed exactly as written. One clarification worth noting: `requirements: [OCRP-01, OCRE-05]` is the plan's frontmatter, but per the plan's own `<critical_reminders>` ("Do NOT mark OCRP-01 fully complete — 44-04 integration closes it"), only `OCRE-05` is marked complete in `REQUIREMENTS.md`'s traceability table; `OCRP-01` stays `Partial (44-01, 44-02, 44-03)` until 44-04's live E2E closes it.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required. `OCR_ENGINE_TIMEOUT`/`OCR_WORKER_CONCURRENCY`/`PROGRESS_MIN_INTERVAL_SECONDS`/`PROGRESS_MIN_DELTA_PCT`/`PROGRESS_WEBHOOK_TIMEOUT`/`WEBHOOK_SIGNING_SECRET`/`METRICS_ADDR` all have code defaults; docker-compose/env wiring for the new `ocr-worker` service is out of this plan's scope (Phase 45 containerizes it).

## Next Phase Readiness

- `cmd/ocr-worker` builds and is ready to be wired into `docker-compose.yml`/deployed alongside the other worker processes (Phase 45's job).
- 44-04 can now drive a live OCR job queued -> active -> done end-to-end (API create -> `ocr` queue -> `cmd/ocr-worker` -> S3 result -> done) and verify SC4's progress ramp against the reused, unmodified progress/SSE machinery.
- Verified: `go build ./...`, `go vet ./...` clean; `go test ./internal/worker/... ./internal/reconciler/... -race -count=1` green; `git diff --exit-code internal/convert/progress.go internal/api/sse.go internal/convert/exec.go` empty (progress/SSE/exec untouched).

---
*Phase: 44-async-pipeline-wiring*
*Completed: 2026-07-26*

## Self-Check: PASSED

All created/modified files verified present on disk; all three task commits (`e48783a`, `d49a270`, `2c93b06`) verified present in git log.
