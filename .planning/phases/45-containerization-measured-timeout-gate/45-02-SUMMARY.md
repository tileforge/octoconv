---
phase: 45-containerization-measured-timeout-gate
plan: 02
subsystem: infra
tags: [docker-compose, tesseract, ocr, ci, github-actions, measurement-gate, asynq, ocrworker]

# Dependency graph
requires:
  - phase: 45-containerization-measured-timeout-gate
    plan: 01
    provides: "Dockerfile.ocr-worker (source-built tesseract 5.5.3 + leptonica 1.87.0 + SHA-pinned tessdata_fast eng/rus), cmd/ocr-worker/main.go reading OCR_MAX_PAGES/OCR_MAX_IMAGE_PIXELS/OCR_ENGINE_TIMEOUT/OCR_WORKER_CONCURRENCY, internal/queue OCRUniqueTTL"
provides:
  - "scripts/ocr-timing-measure.sh: supervised p95 seconds-per-page measurement gate (3-doc x 2-lang matrix at compose ocr-worker resource limits)"
  - "docker-compose.yml ocr-worker service with PLACEHOLDER OCR_ENGINE_TIMEOUT clearly flagged for Plan 03"
  - "IN-02 OCR_ENGINE_TIMEOUT + OCR_MAX_RETRY byte-identical across all 9 queue.NewClient()-constructing services"
  - "CI bake matrix ocr-worker target (docker-build + e2e tiers, gha cache scope=ocr-worker)"
  - ".env.example OCR_* documentation in the AV_*/AUDIO_* derivation-comment style"
affects: [45-containerization-measured-timeout-gate/45-03, 46-keda-scaling]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Measurement-gate-then-scaffold: author the supervised p95 measurement script and the compose/CI placeholder scaffold in one plan, defer the actual measured-number plug-in to a separate supervised operator plan (mirrors AV Phase-36 Plan 03/04 split)"
    - "IN-02 propagation: a new engine class's *_ENGINE_TIMEOUT + *_MAX_RETRY are added byte-identically to every queue.NewClient()-constructing service in the same change that introduces the engine's own worker service"

key-files:
  created:
    - scripts/ocr-timing-measure.sh
  modified:
    - docker-compose.yml
    - .env.example
    - .github/workflows/ci.yml

key-decisions:
  - "OCR_ENGINE_TIMEOUT is committed as a clearly-marked PLACEHOLDER (300s) in docker-compose.yml/.env.example/queue.NewClient()'s own default -- NOT a measured value. Plan 03 must replace it with the real p95-derived number before OCRE-04 can be considered closed."
  - "scripts/ocr-timing-measure.sh's 3-document corpus reuses already-committed internal/convert/testdata/ocr/ unit-test fixtures (text_sample.png=clean, adversarial_text.png=noisy, multipage_image_only.pdf=dense/PDF-sourced) rather than committing new large binaries; all three are small synthetic ImageMagick-generated fixtures, not real-world scans -- the script exposes OCR_TIMING_{CLEAN,NOISY,DENSE}_SRC env overrides so an operator can substitute a more representative corpus at Plan-03 run time without touching the matrix/derivation logic."
  - "The dense/PDF cell times pdftoppm (-r 300 -f 1 -l 1 -singlefile -png) THEN tesseract together per run, matching convertPDF's real per-page production shape (ocrpdf.go); the clean/noisy image cells time tesseract alone, matching Convert()'s image-source branch (ocr.go) which never rasterizes an already-image input."
  - "stop_grace_period for ocr-worker is also a PLACEHOLDER (320s = 300s placeholder timeout + 10s ShutdownTimeout + 10s Docker SIGKILL margin), explicitly flagged for Plan-03 recomputation once the real OCR_ENGINE_TIMEOUT lands."

requirements-completed: []  # OCRE-04 NOT closed by this plan -- the measured OCR_ENGINE_TIMEOUT/OCR_MAX_PAGES plug-in is Plan 03's job; this plan only builds the gate + scaffold.

# Metrics
duration: 35min
completed: 2026-07-26
---

# Phase 45 Plan 02: OCR timing measurement gate + compose scaffold + IN-02 propagation + CI bake Summary

**Supervised p95-seconds-per-page measurement script cloning av-rtf-measure.sh, an ocr-worker compose service with a clearly-flagged PLACEHOLDER timeout, IN-02 OCR_ENGINE_TIMEOUT/OCR_MAX_RETRY parity across all 9 queue.NewClient() services, and an ocr-worker CI bake target -- no measured timeout number is committed.**

## Performance

- **Duration:** 35 min
- **Started:** 2026-07-26T18:34:00Z (approx.)
- **Completed:** 2026-07-26T19:10:00Z
- **Tasks:** 3/3 completed
- **Files modified:** 4 (1 created, 3 modified)

## Accomplishments
- `scripts/ocr-timing-measure.sh`: a runnable, shellcheck-clean supervised gate that builds `Dockerfile.ocr-worker`, runs a 3-doc (clean/dense/noisy) x 2-lang (eng/rus) = 6-cell matrix at `--cpus=2.0 --memory=1g` (env-overridable), times the FULL production argv shape per cell (pdftoppm render for the PDF-sourced dense cell, tesseract directly for the two image cells), prints nearest-rank p95 seconds-per-page per cell, and closes with a DERIVATION HELP block that prints (never commits) `OCR_ENGINE_TIMEOUT = ceil(OCR_MAX_PAGES x worst-cell-p95 x 2.0)` plus the 900s CAP and the Path-B (lower OCR_MAX_PAGES) lever.
- `docker-compose.yml` gained a 9th `ocr-worker` service, structurally cloning `av-worker` (build, `container_name`, `restart: always`, `depends_on` healthy, `cpus:"2.0"/memory:1g`, `WEBHOOK_SIGNING_SECRET`), with `OCR_ENGINE_TIMEOUT`/`stop_grace_period` explicitly commented as PLACEHOLDERs pending Plan 03.
- IN-02: `OCR_ENGINE_TIMEOUT="300s"` + `OCR_MAX_RETRY="2"` added byte-identically to all 9 queue.NewClient()-constructing services (api, worker, webhook-worker-1, webhook-worker-2, document-worker, chromium-worker, audio-worker, av-worker, ocr-worker) -- verified via a grep-based script producing "IN-02 OK".
- `OCR_MAX_PAGES`/`OCR_WORKER_CONCURRENCY`/`OCR_MAX_IMAGE_PIXELS` confirmed present on exactly 1 service (ocr-worker), never leaked onto the other 8.
- `.github/workflows/ci.yml`: `ocr-worker` added to the docker-build tier (`cache-to`+`cache-from`, `scope=ocr-worker`) and the e2e tier (`cache-from`, same scope, so the e2e job reuses the docker-build-warmed GHA cache for tesseract's slow from-source build) -- comment updated to "9 compose bake targets".
- `.env.example` documents all 4 new `OCR_*` vars in the `AV_*`/`AUDIO_*` derivation-comment style, marking `OCR_ENGINE_TIMEOUT` as a PLACEHOLDER.

## Task Commits

Each task was committed atomically:

1. **Task 1: scripts/ocr-timing-measure.sh** - `a5bf1a7` (feat)
2. **Task 2: docker-compose.yml ocr-worker service + IN-02 + .env.example** - `03149b9` (feat)
3. **Task 3: CI bake matrix** - `4ab4b18` (chore)

**Plan metadata:** (this commit, following SUMMARY.md)

## Files Created/Modified
- `scripts/ocr-timing-measure.sh` - supervised p95 sec/page measurement gate (new)
- `docker-compose.yml` - ocr-worker service (placeholder timeout) + IN-02 OCR_* on 8 existing services
- `.env.example` - OCR_ENGINE_TIMEOUT/OCR_MAX_RETRY/OCR_WORKER_CONCURRENCY/OCR_MAX_PAGES/OCR_MAX_IMAGE_PIXELS documentation
- `.github/workflows/ci.yml` - ocr-worker bake target in docker-build + e2e tiers

## Decisions Made
- Corpus sourcing for the measurement script reuses committed `internal/convert/testdata/ocr/` fixtures (Claude's Discretion per 45-CONTEXT.md D-02) rather than synthesizing new text-rendering tooling in-container (no font-rendering/PIL-equivalent tool is present in `Dockerfile.ocr-worker`'s runtime image) or committing new large binaries. This keeps the script runnable as-is while remaining honest in its own header comment that these are small synthetic ImageMagick fixtures, not real-world scans, and exposing `OCR_TIMING_*_SRC` overrides for an operator to substitute a more representative corpus at Plan-03 run time.
- `stop_grace_period: 320s` for ocr-worker is a PLACEHOLDER paired with the PLACEHOLDER `OCR_ENGINE_TIMEOUT`, both explicitly commented for Plan-03 recomputation, mirroring av-worker's 773s derivation shape (753s + 10s + 10s) but with OCR's own (currently placeholder) numbers.
- `OCR_MAX_IMAGE_PIXELS` was placed only on the ocr-worker service (not propagated via IN-02) since it is read by the OCR converter's own guard (`enforceImagePixelBound`), not by `queue.NewClient()` -- mirrors `AV_MAX_DURATION_SECONDS`'s own ocr-worker-analog scoping.

## Deviations from Plan

None - plan executed exactly as written. All three tasks matched their `<action>` specifications; no Rule 1-4 auto-fixes were needed.

## Issues Encountered

None.

## Known Placeholders (NOT stubs -- intentional, scope-fenced)

This plan deliberately commits **no measured OCR_ENGINE_TIMEOUT value** — that is out of scope by design (Plan 03's job). The following values are explicitly marked PLACEHOLDER in both `docker-compose.yml` and `.env.example`, and must be replaced before OCRE-04 is considered closed:

| Placeholder | Current value | File | Replaced by |
|---|---|---|---|
| `OCR_ENGINE_TIMEOUT` (all 9 services) | `300s` | `docker-compose.yml`, `.env.example` | Plan 03's supervised `scripts/ocr-timing-measure.sh` run: `ceil(OCR_MAX_PAGES x worst-cell-p95-sec/page x 2.0)`, verified < 900s |
| `stop_grace_period` (ocr-worker) | `320s` | `docker-compose.yml` | Plan 03: measured `OCR_ENGINE_TIMEOUT` + 10s ShutdownTimeout + 10s SIGKILL margin |
| `OCR_MAX_PAGES` (ocr-worker) | `200` (provisional) | `docker-compose.yml`, `.env.example` | Plan 03: finalized so the derived timeout stays under the 900s CAP (Path-B lever if not) |

## User Setup Required

None - no external service configuration required. Plan 03 will require an operator to actually run `scripts/ocr-timing-measure.sh` against a live Docker daemon (with k8s/OrbStack confirmed down) and review the printed p95 numbers before committing the real `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` values.

## Next Phase Readiness

Plan 03 can now: (1) run `scripts/ocr-timing-measure.sh` against the built `Dockerfile.ocr-worker` image at the exact `--cpus=2.0 --memory=1g` compose limits, (2) read the per-cell p95 seconds-per-page and the printed derivation, (3) replace the 3 PLACEHOLDER values above in `docker-compose.yml`/`.env.example` with the measured numbers (all 9 `OCR_ENGINE_TIMEOUT`/`OCR_MAX_RETRY` occurrences must be updated in lockstep, IN-02), and (4) run the live E2E proving both OCRE-04's timeout gate and the deferred Phase-44 SC4 progress-ramp E2E against the real containerized `ocr-worker`. No blockers.

---
*Phase: 45-containerization-measured-timeout-gate*
*Completed: 2026-07-26*

## Self-Check: PASSED

All created/modified files verified present on disk; all 3 task commit hashes (a5bf1a7, 03149b9, 4ab4b18) verified present in git log.
