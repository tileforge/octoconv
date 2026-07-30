# Phase 45 Discussion Log

**Date:** 2026-07-26
**Mode:** discuss (default)

## Framing

Containerization + measured timeout gate — clone of audio-32/av-36. Carried forward: timeout DERIVED from measured sec/page (never copied), under reconciler CAP (900s/15m); tessdata_fast SHA-pin eng/rus (OSD deferred); poppler apt; IN-02 env propagation; CI bake; live E2E closes the deferred Phase-44 SC4; recorded as Key Decision. Orchestrator runs the supervised measurement (like av RTF).

## Gray areas presented (all 3 selected)

**A. Tesseract source vs apt.**
- **Decision (D-01):** Source-built Tesseract 5.5.x tag/commit-pinned (+ Leptonica) in a throwaway build stage — matches ffmpeg/whisper source-pin discipline + newer engine than apt 5.3.0. poppler-utils from apt. (User chose source over the apt-pinned recommendation — keeps SC1 wording, no amendment.)

**B. Corpus + safety.**
- **Decision (D-02):** 3-doc corpus (clean scan / dense multi-column / noisy-skewed), eng+rus, ~300 DPI; p95 sec/page; OCR_ENGINE_TIMEOUT = ceil(MAX_PAGES × p95 × 2.0), under CAP; Path-B lever (lower MAX_PAGES if over CAP). Supervised run.

**C. MAX_PAGES + concurrency.**
- **Decision (D-03):** OCR_MAX_PAGES ≈ 200 (finalized so MAX_PAGES×p95×2.0 < 900s CAP); OCR_WORKER_CONCURRENCY = 1 (av precedent, avoid CPU oversubscription); OMP_THREAD_LIMIT pinned to cgroup CPU quota (CgroupCPULimit). Concurrency>1 = measured-evidence future tweak.

## Deferred

- KEDA/Helm → Phase 46; concurrency>1 → future; tessdata_best → evidence-gated; deskew/OSD/passthrough/searchable-PDF → v2.
