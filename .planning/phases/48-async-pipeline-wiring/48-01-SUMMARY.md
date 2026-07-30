---
phase: 48-async-pipeline-wiring
plan: 01
subsystem: infra
tags: [asynq, redis, queue, pandoc, go]

# Dependency graph
requires:
  - phase: 44-async-pipeline-wiring
    provides: OCR queue/task/client wiring pattern (TypeOCRConvert/QueueOCR/NewOCRConvertTask/OCRUniqueTTL/EnqueueOCRConvert) cloned verbatim in shape
  - phase: 47-pandoc-engine-foundation
    provides: convert.EnginePandoc const (already registered, not re-added this plan)
provides:
  - TypePandocConvert task type + QueuePandoc (tied to convert.EnginePandoc, DEBT-02) asynq routing target
  - NewPandocConvertTask (reuses ConvertPayload/ParseConvertPayload, per-job asynq.Unique lock)
  - pandocRetrySchedule (2-entry, no jitter, pandoc's own values) + PandocRetryDelay + RetryDelayFunc TypePandocConvert case
  - pandocBackoffSum + PandocUniqueTTL derived FRESH from pandoc's own PANDOC_MAX_RETRY/PANDOC_ENGINE_TIMEOUT budget (D-03), never copied from OCR/AV/audio
  - AllConvertQueues() includes QueuePandoc; TestAllConvertQueuesCoversEveryEngine guard requires convert.EnginePandoc
  - Client.EnqueuePandocConvert + pandocMaxRetry/pandocUniqueTTL fields, wired in NewClient from PANDOC_MAX_RETRY (default 2) / PANDOC_ENGINE_TIMEOUT (default 90s, clearly-marked placeholder superseded by Phase 49)
affects: [48-02-api-routing, 48-03-worker-reconciler, 49-pandoc-containerization]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fresh-derivation proof pattern: each engine's UniqueTTL test asserts inequality against neighboring engines' TTLs under identical (maxRetry, engineTimeout) inputs, catching an accidental copy-paste of another engine's schedule/margin"
    - "Placeholder-timeout precedent: PANDOC_ENGINE_TIMEOUT default (90s) explicitly commented as superseded by a future measured-timeout phase, mirroring OCR's 300s->744s (Phase 44->45) precedent"

key-files:
  created: []
  modified:
    - internal/queue/queue.go
    - internal/queue/client.go
    - internal/queue/queue_test.go

key-decisions:
  - "PandocUniqueTTL derived fresh from pandoc's own PANDOC_MAX_RETRY/PANDOC_ENGINE_TIMEOUT budget (D-03) -- test proves it differs from OCRUniqueTTL/AVUniqueTTL/AudioUniqueTTL under identical inputs"
  - "pandocRetrySchedule = 5s, 20s (2-entry, no jitter, PROVISIONAL pending Phase 49's measured PANDOC_ENGINE_TIMEOUT) -- shorter than OCR's 15s/1m since pandoc is a lighter CPU-bound text transform"
  - "PANDOC_ENGINE_TIMEOUT default = 90s, PANDOC_MAX_RETRY default = 2 -- both clearly flagged as placeholders in client.go doc comments"

patterns-established:
  - "N/A -- this plan clones the established per-engine queue/client pattern; no new patterns introduced"

requirements-completed: [PDCP-01]

# Metrics
duration: 20min
completed: 2026-07-30
---

# Phase 48 Plan 01: Pandoc Asynq Plumbing Summary

**Dedicated `pandoc` asynq queue + task type with a fresh-derived (never-copied) `PandocUniqueTTL`, a 2-entry own-schedule retry, and `EnqueuePandocConvert` -- a mechanical clone of the OCR Phase-44 queue/client machinery, standalone-compiling with zero api/worker/migration changes.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-30T18:58:00Z (approx.)
- **Completed:** 2026-07-30T19:18:29Z
- **Tasks:** 2
- **Files modified:** 3 (internal/queue/queue.go, internal/queue/client.go, internal/queue/queue_test.go)

## Accomplishments
- `TypePandocConvert`/`QueuePandoc` (tied to `convert.EnginePandoc`, DEBT-02 discipline), `NewPandocConvertTask`, a 2-entry `pandocRetrySchedule` (5s, 20s, own values, no jitter) + `PandocRetryDelay`, and `RetryDelayFunc`'s `TypePandocConvert` case
- `PandocUniqueTTL` derived FRESH from pandoc's own `PANDOC_MAX_RETRY`/`PANDOC_ENGINE_TIMEOUT` budget (D-03) -- proven by tests to scale with `engineTimeout` and to differ from `OCRUniqueTTL`/`AVUniqueTTL`/`AudioUniqueTTL` under identical inputs
- `AllConvertQueues()` now includes `QueuePandoc`; `TestAllConvertQueuesCoversEveryEngine` completeness guard requires `convert.EnginePandoc`
- `Client.EnqueuePandocConvert` + `pandocMaxRetry`/`pandocUniqueTTL` fields, wired in `NewClient` from `PANDOC_MAX_RETRY` (default 2) and `PANDOC_ENGINE_TIMEOUT` (default 90s -- a CLEARLY-MARKED PLACEHOLDER, doc-commented as superseded by Phase 49's measured value, mirroring the OCR 300s->744s precedent)

## Task Commits

Each task was committed as a test -> feat pair:

1. **Task 1: Pandoc task type, queue, 2-entry retry schedule + PandocUniqueTTL + AllConvertQueues**
   - `a9a7140` (test) - failing tests for TypePandocConvert/NewPandocConvertTask/pandocRetrySchedule/PandocRetryDelay/PandocUniqueTTL + AllConvertQueues completeness guard
   - `c6e7e8d` (feat) - TypePandocConvert/QueuePandoc/NewPandocConvertTask/pandocRetrySchedule/PandocRetryDelay/pandocBackoffSum/PandocUniqueTTL + AllConvertQueues update
2. **Task 2: Client pandoc fields + NewClient wiring + EnqueuePandocConvert**
   - `c180e18` (test) - failing tests for pandocMaxRetry/pandocUniqueTTL fresh-derivation + EnqueuePandocConvert
   - `ce6e2f4` (feat) - Client pandoc fields, NewClient env wiring (PANDOC_MAX_RETRY/PANDOC_ENGINE_TIMEOUT), EnqueuePandocConvert

**Plan metadata:** (this commit) - docs: complete 48-01 plan

_Note: test commits stage only queue_test.go; the corresponding feat commit that follows stages only the implementation file, so each test commit's tree snapshot fails to compile on its own (RED), and the paired feat commit restores a compiling, green state (GREEN)._

## Files Created/Modified
- `internal/queue/queue.go` - TypePandocConvert/QueuePandoc consts, NewPandocConvertTask, pandocRetrySchedule/PandocRetryDelay, RetryDelayFunc case, pandocBackoffSum/PandocUniqueTTL, AllConvertQueues update
- `internal/queue/client.go` - pandocMaxRetry/pandocUniqueTTL Client fields, NewClient env reads + wiring, EnqueuePandocConvert
- `internal/queue/queue_test.go` - TestPandocConvertTaskRoundTrip, TestPandocRetrySchedule, TestRetryDelayFuncRoutesPandocConvert, TestPandocUniqueTTL, TestClientPandocUniqueTTLDiffersFromOCRAndAV, TestEnqueuePandocConvert, wantEngines completeness-guard update

## Decisions Made
- `pandocRetrySchedule` values chosen as 5s/20s (own, not copied) -- shorter than OCR's 15s/1m since pandoc conversion is a lighter, deterministic CPU-bound transform and D-01 (48-CONTEXT.md) classifies most pandoc failures as terminal anyway, so this schedule mostly covers genuine infra transients.
- `PANDOC_ENGINE_TIMEOUT` default set to 90s (within the plan's suggested 60-120s placeholder range) and `PANDOC_MAX_RETRY` default set to 2 (matching the other fast engines) -- both explicitly flagged in doc comments as placeholders superseded by Phase 49.
- Added an explicit not-copied assertion (`PandocUniqueTTL(...) != OCRUniqueTTL(...)`, `!= AVUniqueTTL(...)`, `!= AudioUniqueTTL(...)` under identical inputs) beyond the plan's minimum ask (`PandocUniqueTTL(2,60s) != PandocUniqueTTL(2,120s)`), to directly prove the D-03 anti-pattern was avoided rather than only proving the TTL scales with its own input.

## Deviations from Plan

None - plan executed exactly as written. Both tasks' acceptance criteria were met without needing any Rule 1-4 auto-fixes.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required. No new environment variables are mandatory (PANDOC_MAX_RETRY/PANDOC_ENGINE_TIMEOUT both have code defaults); operators may optionally set them once Phase 49 supplies a measured PANDOC_ENGINE_TIMEOUT.

## Next Phase Readiness

- Plan 02 (API routing: opts-dispatch + enqueue-switch cases, PandocConverter registration, content-sniff wiring) can now call `queue.Client.EnqueuePandocConvert` and depend on `internal/api/api.go`'s `Enqueuer` interface gaining an `EnqueuePandocConvert` method.
- Plan 03 (worker handler + reconciler route + cmd/pandoc-worker) can now consume `queue.TypePandocConvert`/`queue.QueuePandoc`/`queue.ParseConvertPayload` and register `queue.RetryDelayFunc`'s pandoc routing on the asynq server.
- `go build ./...`, `go vet ./...`, and `go test ./internal/queue/... -count=1` all green; package compiles standalone with zero api/worker/migration changes, as required by the plan's scope fence.

---
*Phase: 48-async-pipeline-wiring*
*Completed: 2026-07-30*
