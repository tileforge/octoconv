---
phase: 46-keda-helm-chart-integration
plan: 01
subsystem: infra
tags: [helm, keda, kubernetes, ocr, autoscaling, chart]

# Dependency graph
requires:
  - phase: 45-containerization-measured-timeout-gate
    provides: "measured OCR_ENGINE_TIMEOUT=744s, OCR_MAX_PAGES=64, OCR_WORKER_CONCURRENCY=1, OCR_MAX_IMAGE_PIXELS=100000000 (docker-compose.yml + .env.example, IN-02 parity)"
provides:
  - "ocr-worker Deployment template (deploy/chart/octoconv/templates/deployment-ocr-worker.yaml)"
  - "worker-ocr-scaledobject KEDA ScaledObject template (deploy/chart/octoconv/templates/scaledobject-ocr.yaml)"
  - "keda.ocr + ocrWorker values.yaml blocks (D-01/D-02 tuning)"
  - "OCR_* env group in the shared octoconv-config ConfigMap (IN-02 parity)"
  - "SC2 confirmation: QueueOCR already in AllConvertQueues()/the always-on collector"
affects: [46-02-keda-loadproof-scripts, 46-03-live-scale-proof]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "6th engine class (OCR) mirrors the av/audio Deployment+ScaledObject pair mechanically — no new chart pattern introduced"

key-files:
  created:
    - deploy/chart/octoconv/templates/deployment-ocr-worker.yaml
    - deploy/chart/octoconv/templates/scaledobject-ocr.yaml
  modified:
    - deploy/chart/octoconv/values.yaml
    - deploy/chart/octoconv/templates/configmap.yaml

key-decisions:
  - "keda.ocr mirrors keda.av 1:1: threshold \"1\", maxReplicaCount 2, pollingInterval 15, cooldownPeriod 180, non-null scaleDownStabilizationSeconds 900 (D-01/D-02)"
  - "ocrWorker.terminationGracePeriodSeconds = 774 (744s OCR_ENGINE_TIMEOUT + 30s chart convention), distinct from compose's stop_grace_period=764 (744+10+10 compose convention)"
  - "minReplicaCount hardcoded 0 in scaledobject-ocr.yaml, not templatized — SEED-011 stays out of scope (D-04)"
  - "WR-01 fail-safe triad (ignoreNullValues:\"false\", fallback{failureThreshold:3,replicas:1}, retry-inclusive PromQL) copied verbatim from scaledobject-av.yaml"
  - "OCR_* env placed in the shared octoconv-config ConfigMap (not a per-service env block) so every queue.NewClient()-constructing Deployment inherits it via octoconv.commonEnv (IN-02/DEBT-05)"
  - "SC2 confirmed, not rebuilt: QueueOCR was already registered in AllConvertQueues() by Phase 44 — queue.go left untouched"

patterns-established: []

requirements-completed: [OCRP-02]

# Metrics
duration: 20min
completed: 2026-07-26
---

# Phase 46 Plan 01: Clone av KEDA/Helm pair into OCR Summary

**ocr-worker Deployment + worker-ocr-scaledobject ScaledObject chart templates, keda.ocr/ocrWorker values, and OCR_* ConfigMap env — mechanical clone of the av pair with the measured 744s timeout wired through**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-26T21:06:00Z
- **Completed:** 2026-07-26T21:26:36Z
- **Tasks:** 2
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments
- Added `keda.ocr` values block (threshold "1", maxReplicaCount 2, pollingInterval 15, cooldownPeriod 180, scaleDownStabilizationSeconds 900) and `ocrWorker` values block (image octoconv-ocr-worker, terminationGracePeriodSeconds 774, cpu "2"/memory "1Gi" limits) mirroring the av/audio precedent
- Extended the shared `octoconv-config` ConfigMap with the full OCR_* env surface (OCR_WORKER_CONCURRENCY, OCR_ENGINE_TIMEOUT="744s", OCR_MAX_RETRY, OCR_MAX_PAGES="64", OCR_MAX_IMAGE_PIXELS) for IN-02 env parity
- Created `deployment-ocr-worker.yaml` — a verbatim av-worker clone (ocr-worker name/image, terminationGracePeriodSeconds 774, tier:app label, commonEnv envFrom, metrics probes)
- Created `scaledobject-ocr.yaml` — a verbatim scaledobject-av.yaml clone (worker-ocr-scaledobject, scaleTargetRef ocr-worker, keda.ocr.* values, WR-01 triad, retry-inclusive PromQL on queue="ocr", minReplicaCount 0 hardcoded, falsy-0 guard, keda+prometheus co-dependency gate)
- Confirmed SC2 (did not rebuild): `TestAllConvertQueuesCoversEveryEngine` passes and `QueueOCR` is present in `internal/queue/queue.go`'s `AllConvertQueues()` — the always-on api queue-depth collector already resolves the OCR backlog at zero replicas

## Task Commits

Each task was committed atomically:

1. **Task 1: values.yaml keda.ocr + ocrWorker blocks + configmap OCR_* env** - `ac7eb87` (feat)
2. **Task 2: deployment-ocr-worker.yaml + scaledobject-ocr.yaml templates + SC2 confirm + full render assertion** - `6685790` (feat)

_No TDD tasks in this plan (pure Helm/YAML chart authoring, no Go behavior change)._

## Files Created/Modified
- `deploy/chart/octoconv/templates/deployment-ocr-worker.yaml` - new ocr-worker Deployment (clone of deployment-av-worker.yaml)
- `deploy/chart/octoconv/templates/scaledobject-ocr.yaml` - new KEDA ScaledObject for the ocr queue (clone of scaledobject-av.yaml)
- `deploy/chart/octoconv/values.yaml` - added `ocrWorker` block (after audioWorker/avWorker, before chromiumWorker) and `keda.ocr` block (after keda.av)
- `deploy/chart/octoconv/templates/configmap.yaml` - added OCR_* env group after the AV_* group

## Decisions Made
- Mirrored keda.av 1:1 rather than deriving fresh tuning numbers — OCR's measured 744s sits between audio's 742s and av's 753s, so the same 900s stabilization/180s cooldown/15s polling/2 max-replica shape applies without new evidence-gathering (D-01/D-02, per 46-CONTEXT.md).
- Kept terminationGracePeriodSeconds derivation distinct per environment: chart convention is ENGINE_TIMEOUT+30 (774), while compose convention is ENGINE_TIMEOUT+10+10 (764) — documented inline in both values.yaml and the new Deployment template so a future reader doesn't "fix" one to match the other.
- Left `internal/queue/queue.go` completely untouched — SC2 was a confirm-not-build step; the test-and-grep check ran clean on the first try, no deviation needed.

## Deviations from Plan

None - plan executed exactly as written. Both tasks' `<verify>` automated commands passed on the first run (VALUES_ENV_OK, then TestAllConvertQueuesCoversEveryEngine + helm lint + RENDER_OK), no Rule 1-4 fixes were required.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required. This plan is pure Helm template/values authoring, verified entirely via `helm lint`/`helm template` render assertions; no live cluster interaction (the live proof is Plan 03).

## Next Phase Readiness
- The OCR value/env contract (`keda.ocr`, `ocrWorker`, `OCR_*` ConfigMap entries) and both new chart templates (`deployment-ocr-worker.yaml`, `scaledobject-ocr.yaml`) are ready for Plan 02 to consume when authoring `keda-ocr-loadproof.sh` / `keda-ocr-downscale-survival.sh` and the `values-loadproof.yaml` `keda.ocr.scaleDownStabilizationSeconds: 15` override.
- SC1 (chart half) and the env-parity half of SC4 are satisfied by this plan; SC2 is confirmed; SC3 (live scale-from-zero) and the survival half of SC4 remain for Plans 02/03.
- No blockers identified.

---
*Phase: 46-keda-helm-chart-integration*
*Completed: 2026-07-26*

## Self-Check: PASSED

All created/modified files verified present on disk; both task commits (`ac7eb87`, `6685790`) verified present in `git log`.
