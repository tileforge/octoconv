---
phase: 45-containerization-measured-timeout-gate
plan: 03
subsystem: infra
tags: [docker-compose, ocr, tesseract, timeout-derivation, reconciler-cap, env-config]

# Dependency graph
requires:
  - phase: 45-01
    provides: Dockerfile.ocr-worker (source-built Tesseract/Leptonica, tessdata_fast) + cmd/ocr-worker env resolver
  - phase: 45-02
    provides: scripts/ocr-timing-measure.sh (supervised p95 sec/page gate) + compose ocr-worker service placeholder + IN-02 OCR_* propagation
provides:
  - Measured, CAP-verified OCR_ENGINE_TIMEOUT=744s (was 300s placeholder)
  - Finalized OCR_MAX_PAGES=64 (Path-B NO-GO lever fired, lowered from D-03's provisional 200)
  - Recomputed ocr-worker stop_grace_period=764s (was 320s placeholder)
  - Key Decision recorded in PROJECT.md + STATE.md with full measurement conditions
affects: [46-keda-helm-integration]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "seconds-per-page timeout derivation (ceil(OCR_MAX_PAGES x worst-cell-p95 x 2.0), CAP-checked, Path-B lever) mirrors the RTF-based derivation used for AV/AUDIO engine classes"

key-files:
  created:
    - .planning/phases/45-containerization-measured-timeout-gate/45-03-SUMMARY.md
  modified:
    - docker-compose.yml
    - .env.example
    - .planning/PROJECT.md
    - .planning/STATE.md
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md

key-decisions:
  - "OCR_ENGINE_TIMEOUT=744s / OCR_MAX_PAGES=64 measured via scripts/ocr-timing-measure.sh (ALL GREEN), never copied from AV(753s)/AUDIO(742s); Path-B lever fired (200->64) because the D-03 provisional ceiling breached the 900s reconciler CAP at the measured worst-cell p95."

patterns-established: []

requirements-completed: [OCRE-04]

# Metrics
duration: 15min
completed: 2026-07-26
---

# Phase 45 Plan 03: Measured OCR Timeout Gate Summary

**OCR_ENGINE_TIMEOUT derived from a real seconds-per-page measurement (worst cell dense_eng p95=5.81 s/page) at 744s, with the Path-B NO-GO lever lowering OCR_MAX_PAGES from 200 to 64 to clear the 900s reconciler CAP — propagated byte-identically across all 9 queue-client services.**

## Performance

- **Duration:** ~15 min (Task 2 only; Task 1's supervised measurement checkpoint was run and approved by the operator in a prior session)
- **Tasks:** 2 (Task 1: operator-run measurement checkpoint, already complete; Task 2: derive/plug-in/record, this session)
- **Files modified:** 6 (docker-compose.yml, .env.example, PROJECT.md, STATE.md, REQUIREMENTS.md, ROADMAP.md) + 1 created (this SUMMARY)

## Accomplishments

- Derived `OCR_ENGINE_TIMEOUT` from the operator-approved measurement: worst-cell p95 = 5.81 s/page (`dense_eng`), measured on the real `ocr-worker` image at compose limits (arch=arm64, cpus=2.0, memory=1g, N=10, omp_thread_limit=2).
- Confirmed the D-03 provisional `OCR_MAX_PAGES=200` ceiling would have produced `ceil(200 x 5.81 x 2.0)=2324s`, breaching the 900s `RECONCILER_ACTIVE_STALE_AFTER` CAP — fired the documented Path-B NO-GO lever, lowering `OCR_MAX_PAGES` to 64.
- Final derivation: `OCR_ENGINE_TIMEOUT = ceil(64 x 5.81 x 2.0) = 744s`, a 17.3% margin under the 900s CAP. 744s is distinct from both `AV_ENGINE_TIMEOUT` (753s) and `AUDIO_ENGINE_TIMEOUT` (742s) — not a copied constant.
- Propagated `OCR_ENGINE_TIMEOUT: "744s"` byte-identically across all 9 queue-client services in `docker-compose.yml` (api, worker, webhook-worker-1, webhook-worker-2, document-worker, chromium-worker, audio-worker, av-worker, ocr-worker) — IN-02 parity preserved.
- Updated `OCR_MAX_PAGES` to `"64"` on the `ocr-worker` service only (not read by `queue.NewClient()`, so no IN-02 requirement).
- Recomputed `ocr-worker`'s `stop_grace_period` from the placeholder `320s` to `764s` (744s ShutdownTimeout + 10s margin + 10s Docker SIGKILL margin), mirroring av-worker's own 773s derivation.
- Replaced every placeholder-derivation comment (compose ocr-worker block, IN-02 comment blocks on all 9 services, `.env.example`) with the real derivation, worst-cell breakdown, and CAP margin.
- Recorded the full Key Decision (numbers + derivation + measurement conditions + Path-B firing + corpus-representativeness caveat) in `.planning/PROJECT.md`'s Key Decisions table and `.planning/STATE.md`'s Decisions section.
- Marked `OCRE-04` complete in `.planning/REQUIREMENTS.md` (traceability table + checkbox) via `gsd-sdk query requirements.mark-complete`.

## Task Commits

Each task was committed atomically:

1. **Task 1: Supervised measurement run** — no code commit (operator/orchestrator checkpoint; measurement numbers captured and approved in a prior session, consumed here as pre-derived input).
2. **Task 2 (part a — compose+env): Derive, CAP-check, plug in measured values** - `64e638a` (feat)
3. **Task 2 (part b — Key Decision): Record in PROJECT.md + STATE.md** - `6f8a08e` (docs)

**Plan metadata:** commit created below (docs: complete plan) — see final commit hash reported to orchestrator.

## Files Created/Modified

- `docker-compose.yml` - OCR_ENGINE_TIMEOUT 300s->744s on all 9 services; OCR_MAX_PAGES 200->64 on ocr-worker; stop_grace_period 320s->764s on ocr-worker; all placeholder-derivation comments replaced with the real measured derivation
- `.env.example` - OCR_ENGINE_TIMEOUT=744s, OCR_MAX_PAGES=64, full derivation comment mirroring the AV_*/AUDIO_* comment style
- `.planning/PROJECT.md` - new Key Decisions table row recording the measured numbers, derivation, CAP margin, Path-B firing, and measurement conditions
- `.planning/STATE.md` - Decisions section entry mirroring the same Key Decision with the full per-cell p95 breakdown and the fixture-representativeness caveat
- `.planning/REQUIREMENTS.md` - OCRE-04 flipped to complete (checkbox + traceability table)
- `.planning/ROADMAP.md` - Phase 45 progress table updated to 3/4 plans (via `roadmap.update-plan-progress`) after this SUMMARY landed

## Decisions Made

- **`OCR_ENGINE_TIMEOUT`=744s / `OCR_MAX_PAGES`=64, measured not copied.** Worst-cell p95 (dense_eng)=5.81 s/page on the real ocr-worker image at compose limits (arch=arm64, cpus=2.0, memory=1g, N=10, omp_thread_limit=2). D-03's provisional `OCR_MAX_PAGES=200` would have derived a 2324s timeout, breaching the 900s reconciler CAP — the Path-B NO-GO lever (established precedent from AV/AUDIO engine classes) was fired, lowering `OCR_MAX_PAGES` to 64 so the derived timeout (744s) clears the CAP with a 17.3% margin. 744s is deliberately distinct from AV(753s)/AUDIO(742s) — confirming it was derived, not copied (PITFALLS.md Pitfall 7).
- **Corpus representativeness (recorded honestly, not glossed over).** The committed unit-test fixtures under `internal/convert/testdata/ocr/*` (2 KB one-line synthetic PNGs) under-stated real per-page OCR cost at 0.148 s/page — using them directly would have yielded a dangerously-low ~60s timeout that could truncate real multi-page OCR jobs mid-run. The operator therefore synthesized a representative 300-DPI full-page corpus (ImageMagick/Arial, mixed EN+RU, ~10K chars/page at normal 12pt clean / 10pt dense body with margins) and re-measured against it — the 5.81 s/page worst cell above is derived from that representative corpus, not the committed fixtures. An intermediate adversarial fine-print run (full A4 page at ~5pt) produced 17.9 s/page; this was recorded as a reference upper bound but deliberately NOT used as the committed derivation basis, since it does not represent expected real-world document density.

## Deviations from Plan

None - plan executed exactly as written. Task 1 (the supervised measurement checkpoint) was already run and approved by the operator prior to this execution session; this session executed only Task 2 (derive/plug-in/record), using the operator-approved numbers as-is per the executor's explicit instructions.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required. This plan only edits `docker-compose.yml`, `.env.example`, and planning documentation.

## Next Phase Readiness

- Phase 45's measured `OCR_ENGINE_TIMEOUT=744s` / `OCR_MAX_PAGES=64` / `stop_grace_period=764s` are now live in `docker-compose.yml` and `.env.example`, unblocking Phase 46 (KEDA/Helm integration), which consumes these measured numbers for `terminationGracePeriodSeconds` and KEDA cooldown/stabilization tuning (mirrors the audio/av precedent).
- Plan 45-04 (the live E2E checkpoint: image->txt + multi-page PDF->hocr + signed webhook + progress ramp) remains the next plan in this phase and can now run against the finalized, non-placeholder timeout configuration.
- `docker compose config` validated clean; `go build ./...` clean (no Go source was touched by this plan).

---
*Phase: 45-containerization-measured-timeout-gate*
*Completed: 2026-07-26*
