---
phase: 46-keda-helm-chart-integration
plan: 03
subsystem: infra
tags: [keda, helm, kubernetes, autoscaling, scale-from-zero, ocr, live-proof]

requires:
  - phase: 46-keda-helm-chart-integration
    provides: ocr-worker Deployment + KEDA ScaledObject + keda.ocr values (46-01), the two gate scripts + values-loadproof keda.ocr=15 (46-02)
  - phase: 45-containerization-measured-timeout-gate
    provides: the octoconv-ocr-worker:dev image + measured OCR_ENGINE_TIMEOUT=744s the grace 774 derives from
provides:
  - live-proven OCR scale-from-zero (SC3) + downscale-survival (SC4) on OrbStack k8s
  - committed timestamped evidence closing OCRP-02
affects: [milestone-v1.10-complete]

tech-stack:
  added: []
  patterns:
    - "OCR class autoscales with production parity to the other five engine classes (per-engine ScaledObject + WR-01 triad + measured grace)"

key-files:
  created:
    - .planning/phases/46-keda-helm-chart-integration/evidence/sc3-ocr-scale-from-zero-20260726T221417Z.txt
    - .planning/phases/46-keda-helm-chart-integration/evidence/sc4-ocr-downscale-survival-20260726T222250Z.txt
    - .planning/phases/46-keda-helm-chart-integration/evidence/gate-transcript-loadproof-20260726T221417Z.log
    - .planning/phases/46-keda-helm-chart-integration/evidence/gate-transcript-downscale-20260726T222250Z.log
  modified: []

key-decisions:
  - "Live proof run on OrbStack k8s v1.34.8 (compose down first); both gate scripts self-contained (KEDA v2.20.1 install + chart + teardown trap)"

requirements-completed: [OCRP-02]

duration: ~90min (incl. OrbStack k8s enablement + full source rebuild of ocr-worker after an aggressive disk prune)
completed: 2026-07-26
---

# Phase 46 Plan 03: Live KEDA Scale Proof Summary

**OCR class autoscaling live-proven on OrbStack k8s — scale-from-zero 0→1→0 (SC3, 11 assertions) and a 60-page OCR job surviving a genuine 2→1 downscale with graceful SIGTERM-before-completion / exit-0 / grace=774 (SC4, 22 assertions).**

## Performance
- **Duration:** ~90 min (operator live gate; included enabling OrbStack Kubernetes + a full tesseract-from-source rebuild of the ocr-worker image after disk reclamation)
- **Completed:** 2026-07-26
- **Tasks:** 1/1 (blocking human-verify checkpoint — operator-run, approved)

## Accomplishments
- **SC3 (scale-from-zero):** `=== ALL 11 ASSERTIONS PASSED ===`. ocr-worker at a genuine 0 replicas → trigger job `4c68a9f8` (text_sample.png→txt) → scaled 0→1 → job `done` → drained back to 0. Event-timeline captured (Started `2026-07-26T22:19:02Z`). Queue registry seeded (incl. `ocr`) so the WR-01 `ignoreNullValues:"false"` fallback resolves to a real zero.
- **SC4 (downscale-survival):** `=== ALL 22 ASSERTIONS PASSED ===`. 60-page pdfunite fixture (<64 cap) long job `389c6f97` → active; short job `bbf147d6` scaled 1→2; busy pod annotated `pod-deletion-cost=-1000`; live `spec.terminationGracePeriodSeconds=774` confirmed under the loadproof overlay (production grace preserved). Forced 2→1 downscale: SIGTERM `22:25:35Z` occurred BEFORE job completion `22:25:49Z`; pod terminated `reason=Completed exit=0` (never 137/SIGKILL); exactly one queued→active (no false retry); long job reached `done`.
- OrbStack torn down clean (octoconv + keda namespaces empty after both runs; EXIT-trap teardown).

## Task Commits
1. **Task 1: Live SC3 + SC4 proof** — evidence committed with this summary (docs commit).

## Files Created/Modified
- `.planning/phases/46-keda-helm-chart-integration/evidence/sc3-ocr-scale-from-zero-*.txt` — SC3 0→1→0 timeline
- `.planning/phases/46-keda-helm-chart-integration/evidence/sc4-ocr-downscale-survival-*.txt` — SC4 raw survival block
- `.planning/phases/46-keda-helm-chart-integration/evidence/gate-transcript-*.log` — full gate transcripts (API key `[REDACTED, 43 chars]`)

## Decisions Made
- Ran on OrbStack Kubernetes (v1.34.8), which had to be enabled (`k8s.enable=true` + engine restart) for this proof; restored to `k8s.enable=false` at close to match the pre-proof baseline.

## Deviations from Plan

### Auto-fixed Issues
**1. [Blocking — missing image] webhook-worker Deployment never became Available on the first SC3 run**
- **Found during:** first `keda-ocr-loadproof.sh` run (readiness gate timed out on deployments/webhook-worker)
- **Issue:** the chart's always-on `webhook-worker` (replicas 2) needs `octoconv-webhook-worker:dev`, but plan step 3 only built api + ocr-worker; imagePullPolicy IfNotPresent → the pod could not start.
- **Fix:** `docker build -f Dockerfile.webhook-worker -t octoconv-webhook-worker:dev .` before re-running. (The engine workers are scale-from-zero so only the always-on webhook-worker blocked readiness.)
- **Verification:** SC3 re-run reached all 11 assertions; webhook-worker Available.

**Total deviations:** 1 (missing always-on image — environment/build gap, not a plan-logic defect).
**Impact:** none on the proof's validity; the second run is the authoritative evidence.

## Issues Encountered
- OrbStack Kubernetes was disabled; enabling it required an engine restart (`orbctl config set k8s.enable true` + `orbctl stop`). Disk was at ~98% — reclaimed via docker image/build-cache prune + removing the unused 10.7GB `vscode` volume (other projects' data volumes were deliberately NOT touched). The aggressive prune wiped the tesseract build cache, forcing a full ocr-worker source rebuild.

## User Setup Required
None.

## Next Phase Readiness
- OCRP-02 fully closed (SC1-SC4 all live/verified). Phase 46 complete → v1.10 milestone ready for audit → complete-milestone.

---
*Phase: 46-keda-helm-chart-integration*
*Completed: 2026-07-26*
