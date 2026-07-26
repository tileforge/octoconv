---
phase: 46-keda-helm-chart-integration
plan: 02
subsystem: infra
tags: [keda, helm, ocr, tesseract, autoscaling, load-proof, kubernetes]

# Dependency graph
requires:
  - phase: 46-01
    provides: ocr-worker Deployment + KEDA ScaledObject templates, keda.ocr + ocrWorker values, OCR_* configmap env
provides:
  - scripts/keda-ocr-loadproof.sh (SC3 scale-from-zero live-proof gate, authored not yet run)
  - scripts/keda-ocr-downscale-survival.sh (SC4 N->N-1 downscale-survival live-proof gate, authored not yet run)
  - deploy/chart/octoconv/values-loadproof.yaml keda.ocr.scaleDownStabilizationSeconds:15 overlay
affects: [46-03 (operator-gated live run of these two scripts)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "OCR live-proof harness mirrors av/audio Phase-33/37 shape but uses committed testdata fixtures (text_sample.png, multipage_image_only.pdf) instead of in-container ffmpeg-lavfi synthesis -- only the LONG SC4 fixture needs in-container synthesis (pdfunite tripling), the SHORT/SC3 fixture is a direct POST of a committed file"

key-files:
  created:
    - scripts/keda-ocr-loadproof.sh
    - scripts/keda-ocr-downscale-survival.sh
  modified:
    - deploy/chart/octoconv/values-loadproof.yaml

key-decisions:
  - "SC3 trigger job reuses the audio-style committed-fixture postJob shape (text_sample.png -> txt), not av's ffmpeg-lavfi in-container synthesis -- ocr already has a real, small, fast-processing fixture on disk"
  - "SC4 LONG fixture is synthesized in-container via pdfunite (tripling the committed 20-page multipage_image_only.pdf to ~60 pages), targeting hocr -- stays under OCR_MAX_PAGES=64 while outliving the 15s-stabilized downscale window at the measured ~1.5-2s/page rate"
  - "keda.ocr.scaleDownStabilizationSeconds:15 added to values-loadproof.yaml mirroring keda.av's block exactly; production 900s (values.yaml) and 774s pod grace (ocrWorker.terminationGracePeriodSeconds) are untouched by the overlay"
  - "Both scripts are AUTHORED and statically verified only in this plan (bash -n, shellcheck -S error, helm template dual-overlay render) -- no live cluster run happens here; Plan 03 is the operator-gated live execution"

requirements-completed: [OCRP-02]

# Metrics
duration: 25min
completed: 2026-07-27
---

# Phase 46 Plan 02: OCR KEDA Live-Proof Harness Authoring Summary

**Authored the SC3 scale-from-zero and SC4 downscale-survival live-proof gate scripts for the OCR engine class as structural clones of the av Phase-37 harness, plus the keda.ocr load-proof overlay knob -- statically verified, ready for Plan 03's live OrbStack run.**

## Performance

- **Duration:** 25 min
- **Started:** 2026-07-27T00:00:00Z (approx)
- **Completed:** 2026-07-27
- **Tasks:** 2/2 completed
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments
- `scripts/keda-ocr-loadproof.sh` (SC3): self-contained, teardown-safe 0->1->0 scale-from-zero gate for ocr-worker, ports 18095/15439, seeds the `ocr` queue into asynq's registry, drives the committed `text_sample.png -> txt` job, captures a timestamped pod event-timeline (Scheduled->Pulling->Pulled->Created->Started), confirms drain back to 0.
- `scripts/keda-ocr-downscale-survival.sh` (SC4): self-contained gate proving a long OCR job survives a genuine 2->1 KEDA/HPA downscale, ports 18096/15440, synthesizes a ~60-page PDF via in-container `pdfunite` (tripling the committed 20-page fixture) targeting `hocr`, uses the WR-05-fixed `jq select(.metadata.deletionTimestamp == null)` victim-pod selection, asserts `spec.terminationGracePeriodSeconds == 774` survives the loadproof overlay, and runs the SC4(1-3) triple-check (job reaches done, exactly one queued->active transition, SIGTERM-before-completion with no exit-137).
- `deploy/chart/octoconv/values-loadproof.yaml` extended with a `keda.ocr.scaleDownStabilizationSeconds: 15` block mirroring `keda.av`'s, leaving production's 900s stabilization and 774s pod grace untouched.
- Both plan `<verify>` blocks pass verbatim, printing `SC3_SCRIPT_OK` and `SC4_SCRIPT_OK`; `go build ./...` clean.

## Task Commits

Each task was committed atomically:

1. **Task 1: values-loadproof.yaml keda.ocr override + author scripts/keda-ocr-loadproof.sh (SC3)** - `f0f7a05` (feat)
2. **Task 2: author scripts/keda-ocr-downscale-survival.sh (SC4) + overlay render assertion** - `7dd0c4f` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified
- `scripts/keda-ocr-loadproof.sh` - SC3 scale-from-zero live-proof gate for ocr-worker (structural clone of `keda-av-loadproof.sh`, committed-fixture postJob shape from `keda-audio-loadproof.sh`)
- `scripts/keda-ocr-downscale-survival.sh` - SC4 downscale-survival live-proof gate for ocr-worker (structural clone of `keda-av-downscale-survival.sh`, pdfunite-synthesized LONG fixture)
- `deploy/chart/octoconv/values-loadproof.yaml` - added `keda.ocr.scaleDownStabilizationSeconds: 15` load-proof overlay block

## Decisions Made
- Reused the audio-style committed-fixture `postJob` shape for the SC3 trigger and SC4 SHORT job (text_sample.png is already a small, fast, committed fixture -- no ffmpeg-style in-container synthesis needed for those two jobs).
- The SC4 LONG fixture is the only one requiring in-container synthesis, via `pdfunite` (poppler-utils, already in the `octoconv-ocr-worker:dev` image) tripling the committed 20-page `multipage_image_only.pdf` to ~60 pages -- verified locally with `pdfinfo` before submission to guarantee it stays under `OCR_MAX_PAGES=64`.
- `opts={"language":"eng"}` is passed on all three job submissions (SC3 trigger, SC4 LONG, SC4 SHORT), matching the e2e test suite's convention for explicit language selection rather than relying on tesseract's server-side default.

## Deviations from Plan

None - plan executed exactly as written. Both scripts were authored as structural clones per the plan's explicit `<action>` instructions, and every acceptance criterion (ports, fixture sources, grace assertion, victim-selection shape, exit-137 guard, teardown discipline, no frozen-script edits) was met on the first pass.

## Known Stubs

None - both scripts are complete, executable harnesses with no placeholder logic. They are intentionally NOT run against a live cluster in this plan (Plan 03's scope, per the plan's explicit boundary), which is a scope decision documented in the plan itself, not a stub.

## Verification Results
- `bash -n` + `shellcheck -S error`: clean on both new scripts.
- `grep` assertions (queue seed incl. `ocr`, `ocr-worker` targeting, ports 18095/15439 and 18096/15440, `774` grace, `pod-deletion-cost=-1000`, exit-137 guard, `deletionTimestamp == null`, `pdfunite`, EXIT-trap teardown): all present.
- `awk`-scoped `values-loadproof.yaml` keda.ocr block check: `scaleDownStabilizationSeconds: 15` present, no `terminationGracePeriodSeconds` override.
- `helm template` with both `values-local.yaml` + `values-loadproof.yaml` overlays: `worker-ocr-scaledobject` renders `stabilizationWindowSeconds: 15`; the rendered manifest still contains `terminationGracePeriodSeconds: 774` for the ocr-worker Deployment.
- `go build ./...`: clean.
- No frozen sibling script (`keda-gate.sh`, `keda-load-proof.sh`, `keda-audio-loadproof.sh`, `keda-av-loadproof.sh`, `keda-av-downscale-survival.sh`) was modified.

## Next Steps
- Plan 03 (operator-gated live checkpoint): build the `octoconv-ocr-worker:dev` image, bring up OrbStack Kubernetes (compose down first), and run both `scripts/keda-ocr-loadproof.sh` and `scripts/keda-ocr-downscale-survival.sh` live, capturing timestamped evidence under `.planning/phases/46-keda-helm-chart-integration/evidence/` for operator approval.

## Self-Check: PASSED

All created files confirmed present on disk; both task commits (`f0f7a05`, `7dd0c4f`) confirmed present in git log.
