---
phase: 45-containerization-measured-timeout-gate
plan: 04
subsystem: testing
tags: [ocr, tesseract, e2e, hocr, progress, webhook, docker]

requires:
  - phase: 45-containerization-measured-timeout-gate
    provides: Dockerfile.ocr-worker (Plan 01), compose ocr-worker service + measured OCR_ENGINE_TIMEOUT=744s (Plan 03)
  - phase: 44-async-pipeline-wiring
    provides: the OCR queue/worker contract + the deferred SC4 live progress-ramp proof
provides:
  - TestOCRConversionE2E — live image->txt + multi-page PDF->hocr E2E, signed-webhook + mid-ramp progress assertion
  - closed the deferred Phase-44 SC4 (live progress ramp over GET /v1/jobs/{id} against a real ocr-worker container)
  - three production bug fixes surfaced only by the live containerized run
affects: [phase-46-keda-helm, ocr]

tech-stack:
  added: []
  patterns:
    - "OCR opts dispatched through a dedicated EngineOCR case in handleCreateJob's opts switch (mirrors Audio/AV/HTML), never the DocOpts default"
    - "tesseract output-format configs (configs/ + tessconfigs/) baked into the runtime image alongside the SHA-pinned language packs"
    - "OCR e2e ramp fixture sized (page count) so the page-granular progress ramp is sampleable against the live poll+throttle cadence"

key-files:
  created:
    - .planning/phases/45-containerization-measured-timeout-gate/45-04-SUMMARY.md
  modified:
    - internal/e2e/e2e_test.go
    - internal/e2e/testdata/multipage_image_only.pdf
    - internal/api/handlers.go
    - internal/convert/ocropts.go
    - Dockerfile.ocr-worker

key-decisions:
  - "The 20-page e2e fixture (~30s job) is deliberately larger than the 3-page unit fixture so a live 0<progress_pct<100 sample is observable past the 2s poll + 2s progress throttle"
  - "tesseract configs/ + tessconfigs/ are engine files, not language packs — Pitfall-4 bloat scope is *.traineddata only, so the no-stray-language assertion was rescoped to *.traineddata"

patterns-established:
  - "Live containerized E2E is the gate that surfaces packaging/wiring gaps a unit/integration test cannot (missing opts case, missing engine configs, too-fast fixture)"

requirements-completed: [OCRE-04]

duration: ~70min
completed: 2026-07-26
---

# Phase 45 Plan 04: Live OCR E2E Summary

**TestOCRConversionE2E proves image->txt + 20-page PDF->hocr end-to-end against the real ocr-worker container (presigned download + HMAC-signed webhook + live 0<progress_pct<100 ramp), closing the deferred Phase-44 SC4 — and surfaced three production bugs fixed inline.**

## Performance

- **Duration:** ~70 min (incl. live debugging of 3 surfaced bugs)
- **Completed:** 2026-07-26
- **Tasks:** 2/2 (Task 1 test authoring — autonomous; Task 2 live run — operator-approved)
- **Files modified:** 5

## Accomplishments
- `TestOCRConversionE2E` authored (Task 1, commit `537868c`): two subtests (image->txt, multi-page PDF->hocr), self-skips offline, signed-webhook HMAC assertion, mandatory mid-ramp `0<progress_pct<100` assertion.
- Live run GREEN against the real compose `ocr-worker` (tesseract 5.5.3, `OCR_ENGINE_TIMEOUT=744s`, `OCR_MAX_PAGES=64`): `PASS TestOCRConversionE2E (34.22s)` — Image_ToTxt 2.09s, MultiPagePDF_ToHOCR 32.09s.
- **SC1 + SC4 proven**: image OCR + multi-page scanned-PDF OCR end-to-end via presigned txt/hocr download, an HMAC-verified signed webhook, and a live page-granular progress ramp — the piece Phase 44 proved only at the converter/handler level.

## Task Commits

1. **Task 1: Author TestOCRConversionE2E** — `537868c` (test)
2. **Fix (surfaced live): OCR opts dispatch** — `a6dcbd1` (fix)
3. **Fix (surfaced live): bake tesseract output configs** — `c1c39fc` (fix)
4. **Test (surfaced live): 20-page ramp fixture** — `32ec390` (test)

## Files Created/Modified
- `internal/e2e/e2e_test.go` — TestOCRConversionE2E (+ pollUntilDoneCapturingProgress helper, assertDownloadIsWellFormedHOCR)
- `internal/e2e/testdata/multipage_image_only.pdf` — replaced 3-page fixture with a 20-page image-only PDF (normal 537x393pt page geometry, ~1.5s/page)
- `internal/api/handlers.go` — EngineOCR case in the opts-validation switch
- `internal/convert/ocropts.go` — ValidateOCRApplicability + isZeroOCROpts
- `Dockerfile.ocr-worker` — COPY tesseract configs/ + tessconfigs/ into TESSDATA_PREFIX; rescope the no-stray-language assertion to *.traineddata

## Decisions Made
- Kept the small 3-page fixture only for unit tests; the e2e ramp proof needs a job long enough (20 pages, ~30s) to sample the page-granular ramp past the live poll (2s) + progress throttle (2s).

## Deviations from Plan

### Auto-fixed Issues (all surfaced by the Task 2 live run, none reproducible offline)

**1. [Missing Critical — Phase-44 wiring gap] OCR jobs with opts returned 422 "invalid opts"**
- **Found during:** Task 2 (live run — both subtests 422 on POST /v1/jobs)
- **Issue:** `handleCreateJob`'s opts-validation switch had no `EngineOCR` case, so an OCR job's `language` opt fell through to the `default` branch's `ParseDocOpts`, which rejected `language` as an unknown field.
- **Fix:** Added an `EngineOCR` case (mirrors Audio/AV/HTML) calling `convert.ParseOCROpts` + new `convert.ValidateOCRApplicability`, persisting the normalized struct.
- **Verification:** unit tests (api+convert) green; live run advanced past 422.
- **Committed in:** `a6dcbd1`

**2. [Missing Critical — containerization gap] ->hocr / ->alto produced no output ("Can't open hocr")**
- **Found during:** Task 2 (PDF job stuck active, retry-exhausted; reproduced in-container: `read_params_file: Can't open hocr`)
- **Issue:** `Dockerfile.ocr-worker` baked only eng+rus `.traineddata` and asserted the tessdata dir held EXACTLY those two files, dropping tesseract's own `configs/`+`tessconfigs/` output-format parameter files (`make install` output). txt (the built-in default) worked, so only non-txt targets failed.
- **Fix:** COPY `configs/`+`tessconfigs/` into `TESSDATA_PREFIX`; rescope the no-stray-language assertion to `*.traineddata` (Pitfall-4 bloat is about language packs, not these tiny text configs).
- **Verification:** in-image `ls configs/{hocr,alto,txt}` present, langs still exactly eng+rus; live PDF->hocr produced well-formed hOCR.
- **Committed in:** `c1c39fc`

**3. [Blocking — test fixture too small] SC4 ramp never sampled**
- **Found during:** Task 2 (PDF job done in ~2s; `max mid-flight sample=-1`)
- **Issue:** the 3-page fixture OCR'd faster than the 2s poll + 2s progress throttle could observe an intermediate `0<progress_pct<100`.
- **Fix:** replaced with a 20-page image-only PDF (~30s job).
- **Verification:** live `MultiPagePDF_ToHOCR PASS (32.09s)` — mid-ramp sample observed.
- **Committed in:** `32ec390`

---

**Total deviations:** 3 auto-fixed (2 missing-critical production bugs, 1 blocking test fixture).
**Impact on plan:** All three were real defects invisible to offline/unit tests and only exposed by running the actual container — exactly the class of gap this live E2E gate exists to catch. No scope creep; the measurement/derivation (Plan 03) was untouched.

## Issues Encountered
- Docker VM disk pressure (host at ~99%) during the live build; reclaimed ~12 GB via image + build-cache prune. Brought up only the OCR-relevant services (postgres/redis/minio/api/ocr-worker/webhook-worker×2) rather than the full stack to avoid rebuilding the heavy unrelated source-built worker images.

## User Setup Required
None.

## Next Phase Readiness
- OCRE-04 fully closed: measured timeout live-validated end-to-end against the real container.
- Ready for Phase 46 (KEDA/Helm): the `ocr-worker` image, measured `OCR_ENGINE_TIMEOUT=744s`, and `stop_grace_period=764s` are the inputs the ScaledObject/`terminationGracePeriodSeconds` derive from.

---
*Phase: 45-containerization-measured-timeout-gate*
*Completed: 2026-07-26*
