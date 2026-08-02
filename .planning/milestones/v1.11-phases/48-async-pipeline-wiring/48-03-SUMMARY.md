---
phase: 48-async-pipeline-wiring
plan: 03
subsystem: worker
tags: [asynq, pandoc, worker, reconciler, terminal-classification, go]

# Dependency graph
requires:
  - phase: 48-01
    provides: TypePandocConvert/QueuePandoc/NewPandocConvertTask/PandocUniqueTTL/EnqueuePandocConvert (queue producer contract)
  - phase: 48-02
    provides: migration 0010_pandoc_engine.sql, PandocConverter registered in convert.Default, API opts/enqueue/content-gate wiring for EnginePandoc
provides:
  - HandlePandocConvert (worker task handler, mirrors HandleOCRConvert's lifecycle shape)
  - isPandocTerminal (D-01 stage-aware classifier: timeout AND pandoc engine/parse errors terminal, mirrors isDocumentTerminal not isOCRTerminal)
  - pandocFailureCode (client-facing error_code mapping)
  - cmd/pandoc-worker (asynq server consuming the pandoc queue)
  - reconciler routing for engine='pandoc' (EnqueuePandocConvert)
  - empty-but-valid pandoc output completes done, never failed (SC4), proven at the worker level
affects: [49-pandoc-containerization, 50-pandoc-keda-helm]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Engine-scoped terminal classifier mirroring an existing precedent (isDocumentTerminal) rather than a blanket copy of the shared isTerminal — pandoc's timeout+engine-error-terminal split is a conscious divergence from OCR's transient-timeout treatment, documented inline in both directions"
    - "cmd/*-worker entrypoints clone the closest analog verbatim and delete engine-irrelevant env/config blocks (OMP/cgroup thread tuning, page/pixel guards) rather than parameterizing them away"

key-files:
  created:
    - cmd/pandoc-worker/main.go
    - internal/worker/pandoc_integration_test.go
  modified:
    - internal/worker/worker.go
    - internal/worker/worker_test.go
    - internal/reconciler/reconciler.go
    - internal/reconciler/reconciler_test.go
    - .planning/REQUIREMENTS.md

key-decisions:
  - "isPandocTerminal classifies a wrapped context.DeadlineExceeded (PANDOC_ENGINE_TIMEOUT expiry) AND any error carrying a \"pandoc:\" substring (every deterministic failure PandocConverter.Convert can produce) as TERMINAL — mirrors isDocumentTerminal's timeout-is-terminal shape (D-01, 48-CONTEXT.md), the deliberate opposite of isOCRTerminal's transient-engine-timeout treatment. A genuine infra transient with no \"pandoc:\" text still falls through to the shared isTerminal's broad-retry default."
  - "HandlePandocConvert mirrors HandleOCRConvert's shape exactly (strict PandocOptsFromMap re-parse guard for defense-in-depth, D-10) rather than HandleDocumentConvert's guard-free shape — both were sanctioned by the plan; the OCR shape was chosen since pandoc, like OCR, has its own opts type worth re-validating on the read path."
  - "cmd/pandoc-worker's PANDOC_ENGINE_TIMEOUT default (90s) matches internal/queue/client.go's own NewClient default exactly, since both independently derive/consume the same budget and must not silently drift apart."
  - "PANDOC_MAX_INPUT_BYTES default of 50 MiB (Claude's Discretion, 48-CONTEXT.md) — pandoc reads linearly with no exotic bomb shape to guard beyond a size cap plus the engine timeout."
  - "PANDOC_WORKER_CONCURRENCY defaults to 2 (not 1 like OCR/AV/audio) — pandoc is a fast, lightweight, deterministic single subprocess, not a heavy per-page/per-frame engine, so a second concurrent job does not risk CPU oversubscription the way tesseract/ffmpeg does under a cgroup CPU limit."

patterns-established:
  - "Pattern: an engine-scoped terminal classifier's doc comment explicitly cross-references the ONE assertion that diverges from its nearest sibling classifier (isPandocTerminal vs isOCRTerminal on timeout), pinned by a dedicated side-by-side test (TestIsPandocTerminalDivergesFromOCR) rather than relying on the doc comment alone."

requirements-completed: [PDCP-01]

# Metrics
duration: ~30min
completed: 2026-07-30
---

# Phase 48 Plan 03: Pandoc Worker Wiring + Reconciler Routing Summary

**HandlePandocConvert + isPandocTerminal (timeout+engine-error terminal, mirroring isDocumentTerminal) + cmd/pandoc-worker consuming the pandoc queue + reconciler recovery for stranded pandoc jobs — closes Phase 48 (PDCP-01).**

## Performance

- **Duration:** ~30 min
- **Completed:** 2026-07-30
- **Tasks:** 3
- **Files modified:** 7 (2 created, 5 modified)

## Accomplishments
- `isPandocTerminal` + `pandocFailureCode` (internal/worker/worker.go): D-01's stage-aware split — a `PANDOC_ENGINE_TIMEOUT` expiry AND any pandoc engine/parse error are TERMINAL (mirrors `isDocumentTerminal`, the opposite of `isOCRTerminal`'s transient-timeout treatment); genuine infra transients (no `"pandoc:"` text, not a timeout) stay retryable via the shared `isTerminal` fallthrough.
- `HandlePandocConvert` (internal/worker/worker.go): the full queued→active→done lifecycle for a pandoc job, mirroring `HandleOCRConvert`'s shape (strict opts re-parse guard, `isCanceled`/`isPandocTerminal`/transient branches, `queue.QueuePandoc`-tagged metrics, best-effort Postgres-first webhook enqueue).
- `cmd/pandoc-worker/main.go`: a new worker binary cloning `cmd/ocr-worker`'s wiring (db/storage/redis, cancel subscriber, progress throttle + progress-webhook, asynq server on `QueuePandoc`, metrics listener, graceful shutdown) with every OCR-specific OMP/cgroup/page/pixel-guard env read removed — pandoc needs none of it.
- Reconciler routing (`internal/reconciler/reconciler.go`): `EnqueuePandocConvert` added to the `enqueuer` interface and a `case convert.EnginePandoc` route in the recovery switch, so a stranded `engine='pandoc'` job recovers onto the pandoc queue instead of the fail-closed `unroutable_engine` default.
- SC4 (empty-but-valid output completes done, never failed) proven at the worker level via `fakeBlankPandocConverter` + `TestPandocEmptyOutputSucceeds_SC4`, exercising the exact `runConvertWithOptionalProgress` seam `HandlePandocConvert`'s `process()` call uses.
- Real-pandoc (skip-guarded) integration tests: a genuine md→html conversion succeeds through the worker seam; a genuine engine failure (corrupt docx source) classifies terminal via `isPandocTerminal`.
- `internal/convert/exec.go`, `internal/convert/progress.go`, `internal/api/sse.go` verified byte-for-byte unchanged across the whole phase (`git diff --exit-code` fence, green). No `go.mod` changes.
- PDCP-01 marked complete in REQUIREMENTS.md — Phase 48 (Async Pipeline Wiring) closed.

## Task Commits

Each task was committed atomically:

1. **Task 1: isPandocTerminal + pandocFailureCode (D-01)** - `4c27969` (feat)
2. **Task 2: HandlePandocConvert + reconciler pandoc routing** - `23bd622` (feat)
3. **Task 3: cmd/pandoc-worker + empty-output SC4 proof + invariant fence** - `65babc9` (feat)

**Plan metadata:** (this commit) - `docs: complete 48-03 plan`

## Files Created/Modified
- `internal/worker/worker.go` - `isPandocTerminal`, `pandocFailureCode`, `HandlePandocConvert`
- `internal/worker/worker_test.go` - table tests for the classifier/failure-code mapping, `fakeBlankPandocConverter` + SC4 empty-output test
- `internal/worker/pandoc_integration_test.go` (new) - skip-guarded real-pandoc integration tests (md→html success, corrupt-input terminal classification)
- `cmd/pandoc-worker/main.go` (new) - the pandoc-queue-consuming worker binary
- `internal/reconciler/reconciler.go` - `EnqueuePandocConvert` interface method + `case convert.EnginePandoc` recovery route
- `internal/reconciler/reconciler_test.go` - `fakeEnqueuer.EnqueuePandocConvert`, `TestSweepRoutesPandocJobsToPandocQueue`, pandoc row in `TestSweepRoutesEveryEngineConstant`
- `.planning/REQUIREMENTS.md` - PDCP-01 marked complete (checkbox + traceability table)

## Decisions Made
- `isPandocTerminal` mirrors `isDocumentTerminal`'s timeout-is-terminal shape per D-01 (48-CONTEXT.md, BINDING) rather than `isOCRTerminal`'s transient-timeout shape — pinned by a dedicated side-by-side divergence test (`TestIsPandocTerminalDivergesFromOCR`), not just a doc comment.
- `HandlePandocConvert` mirrors `HandleOCRConvert`'s strict-opts-reparse shape (defense-in-depth, D-10) rather than `HandleDocumentConvert`'s guard-free shape — both were sanctioned by the plan as executor's discretion; chosen because pandoc, like OCR, carries its own validated opts type worth re-checking on the read path.
- `PANDOC_ENGINE_TIMEOUT` default (90s) in `cmd/pandoc-worker/main.go` intentionally matches `internal/queue/client.go`'s own default exactly, since both independently read the same env var for related-but-separate purposes (worker attempt deadline vs. `PandocUniqueTTL` derivation) and must never silently drift apart.
- `PANDOC_MAX_INPUT_BYTES` default of 50 MiB and `PANDOC_WORKER_CONCURRENCY` default of 2 are Claude's Discretion per 48-CONTEXT.md — pandoc is fast/lightweight relative to OCR/AV/audio, justifying a higher default concurrency than those engines' `1`.

## Deviations from Plan

None - plan executed as written, with the two executor-discretion choices called out above (both explicitly sanctioned by the plan's `<action>` text as "acceptable and preferred").

## Issues Encountered
- Initial `TestPandocConvertIntegration` draft incorrectly asserted a terminal-100% `UpdateProgress` bypass write for a non-`ProgressConverter` engine. `runConvertWithOptionalProgress`'s type-assert only reaches the bypass write on a successful `ConvertWithProgress` call; `PandocConverter` doesn't implement that interface at all, so it takes the early-return plain `conv.Convert(...)` path, which never touches `repo`. Fixed the assertion to expect zero `UpdateProgress` calls — the correct, proven contract for pandoc jobs (no fabricated progress signal).
- A stray `pandoc-worker` build artifact (from an explicit `go build ./cmd/pandoc-worker/...` invocation) landed untracked in the repo root; deleted before staging so it was never committed.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Phase 48 (Async Pipeline Wiring) is complete: all 3 plans (queue plumbing, migration/API/registry wiring, worker/reconciler wiring) landed. PDCP-01 is fully satisfied — a pandoc job flows queued→active→done through `cmd/pandoc-worker` with correct stage-aware retry classification and reconciler recovery, over the byte-for-byte-unchanged v1.9 progress/SSE/webhook machinery.
- Phase 49 (containerization + measured `PANDOC_ENGINE_TIMEOUT`) can now proceed: it inherits a working, fully-wired `cmd/pandoc-worker` to containerize and a real end-to-end pipeline to time — no placeholder timeout logic remains to be threaded through, only measured.
- Phase 50 (KEDA/Helm) remains blocked on Phase 49's measured numbers, per the roadmap's hard-ordered spine (unchanged from the audio/av/ocr 4-phase precedent).

---
*Phase: 48-async-pipeline-wiring*
*Completed: 2026-07-30*

## Self-Check: PASSED

All created/modified files verified present on disk; all 3 task commit hashes (4c27969, 23bd622, 65babc9) verified present in `git log --oneline --all`.
