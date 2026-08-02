---
phase: 44-async-pipeline-wiring
plan: 04
subsystem: testing
tags: [ocr, tesseract, poppler, progress, sse, worker, integration-test]

# Dependency graph
requires:
  - phase: 44-async-pipeline-wiring (44-03)
    provides: HandleOCRConvert, isOCRTerminal, cmd/ocr-worker, the reused progressSink/runConvertWithOptionalProgress seam
provides:
  - Integration-level proof that OCR jobs drive the v1.9 progress machinery (progressSink -> repo.UpdateProgress) completely unmodified
  - Single-output invariant proxy assertion for the OCR path
  - Explicit git-diff fence confirming internal/convert/progress.go and internal/api/sse.go are byte-for-byte unchanged across the whole phase
affects: [45-ocr-containerization]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Integration test drives the worker's own seam (runConvertWithOptionalProgress) with a real converter + committed fixtures + a spy repo, rather than re-testing the converter's ConvertWithProgress in isolation (that unit-level proof already exists in internal/convert/ocrpdf_test.go)."

key-files:
  created:
    - internal/worker/ocr_progress_integration_test.go
  modified: []

key-decisions:
  - "Used the 3-page multipage_image_only.pdf fixture (not the 2-page multipage_text.pdf already used by internal/convert/ocrpdf_test.go) so the ramp assertion can require >=2 DISTINCT pre-100 pct values (33, 66) plus the terminal 100 -- a 2-page fixture only yields one pre-100 tick (50), which would not distinguish a genuine ramp from a single jump."
  - "minInterval set to 1 nanosecond (not 0) in the test's runConvertWithOptionalProgress calls -- 0 falls back to the plain conv.Convert() path (D-01's own throttle-configured gate) and would silently emit zero UpdateProgress calls, defeating the test's purpose. 1ns disables the interval/delta throttle gates without disabling the ProgressConverter path."
  - "Single-output invariant is proven via a filesystem proxy (exactly one file in the temp output dir after one converter invocation through the worker seam) rather than via a full HandleOCRConvert/Postgres round-trip, since h.repo/h.store are concrete (non-interface) types on Handler and a full Handler cannot be constructed in a pure unit test (per ARCHITECTURE.md's Key Abstractions note)."

patterns-established: []

requirements-completed: [OCRP-01]

# Metrics
duration: 20min
completed: 2026-07-26
---

# Phase 44 Plan 04: Integration Proof (progress ramp + reuse-unchanged fence) Summary

**Integration test proves OCR drives the unmodified v1.9 progressSink: single image jobs record null->100 (zero intermediate ticks), a 3-page scanned PDF records a genuine 33/66/100 page-by-page ramp, and `git diff --exit-code` confirms progress.go/sse.go carry zero edits across the whole phase.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-26
- **Tasks:** 2 (both completed; Task 2 is a verification-only gate, no production code)
- **Files modified:** 1 created

## Accomplishments
- `TestOCRProgressIntegration` drives `runConvertWithOptionalProgress` — the EXACT seam `process()` uses in production — against a real `convert.OCRConverter{}` and the committed Phase 43 fixtures (`internal/convert/testdata/ocr/multipage_image_only.pdf`, `text_sample.png`), recording every `UpdateProgress` call through a spy `progressRepo`.
  - Multi-page case: 3-page scanned PDF -> 4 total `UpdateProgress` calls (33, 66, 100 from the converter's own page loop + a redundant terminal-100 bypass write), with >=2 distinct pre-100 values (33, 66) proving a genuine page-by-page ramp, not a single jump.
  - Single-image case: exactly 1 `UpdateProgress` call, pct=100 — the image path emits zero `sink.Update` calls (OCR-04: `ConvertWithProgress` delegates straight to `Convert` for a non-PDF source), so the ONLY recorded write is the unconditional terminal-100 bypass — the null->100 contract, with no fabricated intermediate value.
- `TestOCRProgressIntegrationSingleOutput` proves one converter invocation through the same seam writes exactly one output file (single-output invariant proxy, W2 plan-checker requirement).
- Reuse-unchanged fence (Task 2) verified and recorded: `git diff --exit-code -- internal/convert/progress.go internal/api/sse.go` exits 0 (FENCE_CLEAN); `grep -rc "progressBlockParser\|buildProgressTick" internal/convert/ocr*.go` == 0 across all 8 `ocr*.go`/`ocr*_test.go` files — OCR's page estimator (`ocrprogress.go`) never touches the ffmpeg-specific progress parser.
- Full suite green: `go build ./...`, `go vet ./...` clean; `DATABASE_URL=... go test ./internal/... -race -count=1` all packages pass (17 packages, `internal/db` has no test files).

## Task Commits

Each task was committed atomically:

1. **Task 1 + Task 2 (single commit — Task 2 adds no code, only a verification gate)** - `a4b4f1a` (test)

**Plan metadata:** (this commit, docs: complete 44-04 plan)

## Files Created/Modified
- `internal/worker/ocr_progress_integration_test.go` - Integration test: `TestOCRProgressIntegration` (multi-page ramp + single-image null->100 via `runConvertWithOptionalProgress` + real `convert.OCRConverter{}` + committed fixtures), `TestOCRProgressIntegrationSingleOutput` (single-output invariant proxy), `requireOCRIntegrationBinaries` (pdfinfo/pdftoppm/pdftotext/tesseract skip guard, mirrors `internal/convert/ocrpdf_test.go`'s `requireOCRPDFBinaries`).

## Decisions Made
See `key-decisions` in frontmatter: 3-page fixture choice for a genuine 2-distinct-value ramp; `minInterval=1ns` (not 0) to keep the `ProgressConverter` path active while defeating the interval/delta throttle; filesystem-proxy single-output proof (Handler's concrete `h.repo`/`h.store` fields make a full `HandleOCRConvert` unit test impossible without a live Postgres/S3 Handler).

## Deviations from Plan

None — plan executed exactly as written. Task 2 required no code changes (as specified); its verification commands were run and their results recorded here rather than in a separate artifact.

## Issues Encountered

- First test iteration used `minInterval=0, minDeltaPct=0` intending "no throttling," but `runConvertWithOptionalProgress`'s own `!ok || minInterval <= 0` gate falls back to the plain `conv.Convert()` path at `minInterval=0`, producing zero `UpdateProgress` calls. Fixed by setting `minInterval = time.Nanosecond` (still keeps the ramp effectively un-throttled given real per-page tesseract/pdftoppm processing time, while keeping the `ProgressConverter` path active).
- First ramp assertion excluded the last pre-bypass update from the "must be <100" check, but the 3-page fixture's OWN last page naturally computes `pctForPage(3,3)=100` before the worker's separate terminal-100 bypass write also fires — so the true sequence is `[33, 66, 100, 100]`, not `[33, 66, <100], [100]`. Adjusted the assertion to check monotonic non-decreasing order across ALL recorded updates, collect only the DISTINCT values strictly below 100 (>= 2 required), and separately assert the FINAL recorded value is 100 — this correctly distinguishes "genuine ramp" from "single jump" without assuming which specific update carries the worker's own bypass write.
- Cleaned up an untracked `ocr-worker` build binary accidentally produced in the repo root by an earlier `go build ./...` verification run (not committed; removed before final commit).

## Known Stubs

None.

## Threat Flags

None — this plan adds test code only, no new production surface (per the plan's own threat model: T-44-11's mitigation, the git-diff fence, is exactly what Task 2 proves held).

## User Setup Required

None - no external service configuration required.

## Live E2E Deferral Note (W1)

**The LIVE compose/API/SSE end-to-end progress-ramp test (ROADMAP's literal Phase 44 SC4 wording — "a single image job's progress goes null->100; a multi-page scanned PDF ramps page-by-page over GET/SSE") is explicitly DEFERRED to Phase 45's container E2E gate**, per 44-CONTEXT.md's `<specifics>` section: "a live-stack check (compose)... a natural operator/self gate like the av/SSE proofs (Phase 45/46 have the container... planner's call)." This phase (44-04) proves the SAME ramp/null->100 contract at the converter/handler INTEGRATION level — through the real `OCRConverter` + the real worker `progressSink`/`runConvertWithOptionalProgress` seam + committed fixtures — which is the strongest proof achievable without a running `ocr-worker` container and a live Postgres-backed job via the HTTP API. Phase 45 (which adds the Dockerfile, measured `OCR_ENGINE_TIMEOUT`, and `cmd/ocr-worker` containerization) is the natural place to also add the live compose upload -> poll/SSE -> ramp E2E check against a running container, mirroring the av/audio precedent (Phases 33/37).

## Next Phase Readiness

Phase 44 (Async Pipeline Wiring) is now COMPLETE — all 4 plans (44-01 migration/registry, 44-02 queue/task/client, 44-03 HandleOCRConvert/cmd/ocr-worker/reconciler, 44-04 this integration proof) landed. OCRP-01 is closed. OCR jobs flow end-to-end through the async pipeline (API create -> `ocr` queue -> `cmd/ocr-worker` -> S3 result -> done) with correct retry/dedup/engine-routing, over the byte-for-byte-unchanged v1.9 progress/SSE/webhook machinery (confirmed by this plan's fence). Phase 45 (containerization + measured `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES`) can now proceed — it depends on this phase's stable queue/worker contract to produce real timing data, and should add the live compose E2E ramp check deferred above.

---
*Phase: 44-async-pipeline-wiring*
*Completed: 2026-07-26*

## Self-Check: PASSED
- FOUND: internal/worker/ocr_progress_integration_test.go
- FOUND: a4b4f1a
