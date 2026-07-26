---
phase: 45-containerization-measured-timeout-gate
plan: 01
subsystem: infra
tags: [docker, tesseract, leptonica, poppler, ocr, cgroup, asynq]

# Dependency graph
requires:
  - phase: 44-async-pipeline-wiring
    provides: cmd/ocr-worker skeleton (unregistered zero-value OCRConverter, OCR_ENGINE_TIMEOUT/OCR_WORKER_CONCURRENCY reads), internal/convert/ocr.go's OCRConverter{MaxImagePixels,MaxPages} fields, internal/convert/cgroup.go's CgroupCPULimit
provides:
  - "Dockerfile.ocr-worker: source-built tag-pinned Tesseract 5.5.3 + Leptonica 1.87.0, poppler-utils from apt, SHA-256-pinned tessdata_fast eng+rus ONLY, USER nobody, dual version/lang-set runtime assertions"
  - "cmd/ocr-worker: buildOCRConverter(OCR_MAX_PAGES, OCR_MAX_IMAGE_PIXELS) re-registered before srv.Start (overrides converters.go's zero-value registration), resolveOCRThreadLimit -> OMP_THREAD_LIMIT export, OCR_WORKER_CONCURRENCY default 1"
affects: [45-02-scripts-ocr-timing-measure, 45-03-measured-timeout-checkpoint, 45-04-live-e2e, 46-keda-helm]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Source-built engine + throwaway build stage, mirrored from Dockerfile.av-worker/Dockerfile.audio-worker: pin by tag AND assert git rev-parse HEAD == the (possibly peeled) commit"
    - "SHA-256 fail-closed data bake (tessdata_fast eng+rus), mirrors Dockerfile.audio-worker's ggml-base.bin bake"
    - "Env-only-in-main + post-construction setter for process-wide config (buildOCRConverter re-register, OMP_THREAD_LIMIT), mirrors cmd/av-worker's AVConverter re-register / AV_DISK_SAFETY_FACTOR setter"

key-files:
  created:
    - Dockerfile.ocr-worker
    - cmd/ocr-worker/main_test.go
  modified:
    - cmd/ocr-worker/main.go

key-decisions:
  - "Tesseract 5.5.3 pinned by tag + peeled commit db0ec62f81b0737fbbe184d8fea40af5738f8eef (annotated tag, resolved live via git ls-remote --tags 2026-07-26)"
  - "Leptonica 1.87.0 pinned by tag + commit 13275a278eb55b5746e33f95fbf5a2c8f604b3ab (lightweight tag, tag hash IS the commit)"
  - "tessdata_fast pinned at commit 87416418657359cb625c412a48b6e1d6d41c29bd; eng.traineddata sha256=7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2, rus.traineddata sha256=e16e5e036cce1d9ec2b00063cf8b54472625b9e14d893a169e2b0dedeb4df225 (both resolved live by downloading and sha256sum-ing against the exact pinned commit)"
  - "Leptonica 1.87.0 installs its shared library as libleptonica.so.6 (not the historical liblept.so name) -- discovered live via readelf -d, corrected the runtime COPY glob accordingly"
  - "libarchive-dev/libarchive13 added to the build/runtime dep lists after readelf -d showed tesseract genuinely links libarchive.so.13 (combined traineddata support) -- not in STACK.md's drafted apt list since that draft assumed apt-installed leptonica-dev pulling transitive deps automatically"
  - "libwebpmux3 added to the runtime apt list after readelf -d on the built libleptonica.so.6 showed it needs libwebpmux.so.3 in addition to libwebp.so.7"
  - "OCR_MAX_IMAGE_PIXELS default set to 100_000_000, reusing cmd/api/main.go's MAX_IMAGE_PIXELS magnitude/envInt64 pattern (same decompression-bomb guard class)"

patterns-established:
  - "OCR engine build stage builds Leptonica first, then Tesseract against it in the SAME tess-build stage (not two separate stages) -- Tesseract's own build depends on Leptonica being installed and ldconfig'd first"

requirements-completed: [OCRE-04]

# Metrics
duration: 35min
completed: 2026-07-26
---

# Phase 45 Plan 01: Dockerfile.ocr-worker + cmd/ocr-worker env resolver Summary

**Source-built tag-pinned Tesseract 5.5.3 + Leptonica 1.87.0 Docker image (eng+rus tessdata_fast only, SHA-256 verified) plus cmd/ocr-worker re-registering a configured OCRConverter with cgroup-derived OMP_THREAD_LIMIT and concurrency=1**

## Performance

- **Duration:** 35 min
- **Tasks:** 2 completed
- **Files modified:** 3 (1 created Dockerfile, 1 created test, 1 modified main.go)

## Accomplishments
- `Dockerfile.ocr-worker` builds green end-to-end: Go binary stage → `tess-build` stage (Leptonica 1.87.0 from source, then Tesseract 5.5.3 from source against it, both tag+commit pinned and rev-parse-asserted) → SHA-256-verified tessdata_fast eng+rus bake → slim runtime stage (poppler-utils from apt, `USER nobody`, dual runtime assertions: `tesseract --version` == 5.5.3 and `TESSDATA_PREFIX` contains exactly `eng.traineddata rus.traineddata`)
- Live-verified: `tesseract --version` reports `tesseract 5.5.3` / `leptonica-1.87.0`; `tesseract --list-langs` returns exactly `eng` and `rus` (no `osd`, since no `osd.traineddata` was baked); image size 440MB
- `cmd/ocr-worker/main.go` now reads `OCR_MAX_PAGES` (default 200, provisional) and `OCR_MAX_IMAGE_PIXELS` (default 100,000,000) and re-registers a CONFIGURED `OCRConverter` via `buildOCRConverter` before `srv.Start`, overriding converters.go's zero-value registration (mirrors av-worker's D-09 precedent)
- `resolveOCRThreadLimit()` (CgroupCPULimit → NumCPU fallback, mirrors `resolveAVThreads` exactly) exports `OMP_THREAD_LIMIT` before `srv.Start` so tesseract's OpenMP threads honor the cgroup CPU quota
- `OCR_WORKER_CONCURRENCY` default flipped from 2 to 1 (D-03: one OCR job at a time, mirrors av/audio)

## Task Commits

Each task was committed atomically:

1. **Task 1: Dockerfile.ocr-worker** - `4ef417c` (feat)
2. **Task 2 RED: failing test for OCR env resolver** - `c40c868` (test)
2. **Task 2 GREEN: wire OCR env resolver** - `384f344` (feat)

_TDD task 2 produced two commits (RED test → GREEN implementation); no REFACTOR commit was needed._

## Files Created/Modified
- `Dockerfile.ocr-worker` - source-built tag-pinned Tesseract 5.5.3 + Leptonica 1.87.0, poppler-utils, SHA-pinned tessdata_fast eng+rus, USER nobody
- `cmd/ocr-worker/main_test.go` - TestBuildOCRConverter + TestResolveOCRThreadLimit
- `cmd/ocr-worker/main.go` - buildOCRConverter re-register, resolveOCRThreadLimit + OMP_THREAD_LIMIT export, OCR_WORKER_CONCURRENCY default 1, envInt64 helper

## Decisions Made
- Both source builds (Leptonica, Tesseract) pinned by tag AND asserted `git rev-parse HEAD` == a specific commit, documenting the annotated-vs-lightweight-tag distinction inline (Tesseract 5.5.3 is annotated — peeled commit `db0ec62f`; Leptonica 1.87.0 is lightweight — tag hash IS the commit `13275a27`), resolved live via `git ls-remote --tags` on 2026-07-26.
- tessdata_fast pinned to a fixed commit (`87416418`, resolved via `git ls-remote HEAD` against `tesseract-ocr/tessdata_fast`), never the mutable `main` branch pointer; both `.traineddata` SHA-256 hashes resolved live by downloading and hashing against that exact commit before writing the Dockerfile's `sha256sum -c -` lines.
- OCR_MAX_IMAGE_PIXELS's default (100,000,000) intentionally reuses `cmd/api/main.go`'s `MAX_IMAGE_PIXELS` magnitude and `envInt64` pattern — same decompression-bomb guard class, just enforced by the OCR converter instead of the API layer.
- No changes needed to `internal/convert/ocr.go`: `OCRConverter.MaxPages`/`MaxImagePixels` fields already existed from Phase 43, so the "add config fields if needed" contingency in the plan's critical reminders was not exercised.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected the Leptonica shared-library COPY glob from `liblept.so*` to `libleptonica.so*`**
- **Found during:** Task 1 (Dockerfile.ocr-worker build/smoke verification)
- **Issue:** STACK.md's drafted Dockerfile skeleton and Leptonica's historical naming both assume the shared library installs as `liblept.so`; Leptonica 1.87.0's actual autotools install produces `libleptonica.so.6` (confirmed live via `readelf -d` against the compiled `libtesseract.so.5`'s own `NEEDED` entries). The original COPY glob silently copied nothing, and the runtime image failed its own version assertion with `tesseract: error while loading shared libraries: libleptonica.so.6: cannot open shared object file`.
- **Fix:** Changed the runtime-stage COPY glob to `libleptonica.so*` and documented the naming discrepancy inline.
- **Files modified:** Dockerfile.ocr-worker
- **Verification:** `docker build` green; `tesseract --version`/`--list-langs` both pass live inside the built image.
- **Committed in:** 4ef417c (Task 1 commit — fixed before the task was committed, so no separate deviation commit)

**2. [Rule 2 - Missing Critical] Added `libarchive-dev`/`libarchive13` and `libwebpmux3` to the build/runtime dependency lists**
- **Found during:** Task 1 (Dockerfile.ocr-worker build)
- **Issue:** `readelf -d` against the compiled `tesseract`/`libtesseract.so.5`/`libleptonica.so.6` binaries showed real `NEEDED` entries for `libarchive.so.13` (Tesseract's combined-traineddata support) and `libwebpmux.so.3` (Leptonica's webp support, in addition to `libwebp.so.7`) that STACK.md's drafted apt list did not anticipate (that draft assumed an apt-installed `libleptonica-dev` package pulling every transitive dependency automatically — not applicable here since both engines are built from source).
- **Fix:** Added `libarchive-dev` to the `tess-build` stage's apt-install line and `libarchive13`/`libwebpmux3` to the runtime stage's apt-install line.
- **Files modified:** Dockerfile.ocr-worker
- **Verification:** Runtime `tesseract --version`/`--list-langs` both execute successfully with no missing-shared-library errors.
- **Committed in:** 4ef417c (Task 1 commit — fixed before the task was committed, so no separate deviation commit)

---

**Total deviations:** 2 auto-fixed (1 bug, 1 missing-critical-dependency) — both discovered live during the Task 1 Docker build and folded into the Task 1 commit before it was made (no plan-execution rollback needed).
**Impact on plan:** Both fixes were necessary for the image to build/run correctly at all; no scope creep — no files outside `Dockerfile.ocr-worker` were touched for either fix.

## Issues Encountered
None beyond the two auto-fixed Docker build issues documented above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `Dockerfile.ocr-worker` builds a real, runnable ocr-worker image with the pinned tesseract 5.5.3 + eng/rus-only tessdata — ready for Plan 02's `scripts/ocr-timing-measure.sh` and compose service wiring to build against it.
- `cmd/ocr-worker` now exposes `OCR_MAX_PAGES`/`OCR_MAX_IMAGE_PIXELS`/`OMP_THREAD_LIMIT`/`OCR_WORKER_CONCURRENCY` env knobs — Plan 03's measured `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` values plug straight into the same `envInt`/`envDuration` reads with no further code changes.
- No `OCR_ENGINE_TIMEOUT` value was finalized or committed to compose/`.env.example` in this plan (correctly deferred to Plan 03's supervised measurement).
- OCRE-04 is NOT marked complete by this plan alone — it closes only once the measurement (Plan 03) and live E2E (Plan 04) land; this plan produces the measurable artifact + env plumbing.

---
*Phase: 45-containerization-measured-timeout-gate*
*Completed: 2026-07-26*
