---
phase: 50-keda-helm-chart-integration
plan: 01
subsystem: infra
tags: [helm, kubernetes, keda, prometheus, pandoc, autoscaling]

# Dependency graph
requires:
  - phase: 49-containerization-measured-timeout-gate
    provides: "MEASURED PANDOC_ENGINE_TIMEOUT=65s, PANDOC_WORKER_CONCURRENCY=2, PANDOC_MAX_INPUT_BYTES=4194304, PANDOC_MAX_RETRY=2 (docker-compose.yml pandoc-worker service, 49-03-SUMMARY.md)"
  - phase: 48-async-pipeline-wiring
    provides: "QueuePandoc already registered in AllConvertQueues() (internal/queue/queue.go), pandocRetrySchedule (5s/20s)"
provides:
  - "pandocWorker + keda.pandoc blocks in deploy/chart/octoconv/values.yaml"
  - "PANDOC_* env group in the shared octoconv-config ConfigMap (IN-02 parity)"
  - "deployment-pandoc-worker.yaml — pandoc-worker Deployment template (grace 85s, cpu 2/mem 1Gi, extraEnv hook)"
  - "scaledobject-pandoc.yaml — KEDA ScaledObject template (null-stabilization, WR-01 triad, falsy-0 guard)"
affects: ["50-02 (live loadproof gate)", "50-03/50-04 (downscale-survival gate, live evidence)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fast-engine KEDA tuning profile (scaleDownStabilizationSeconds: null) — pandoc joins document as the second class to take the FAST-engine shape instead of the audio/av/ocr 900s heavy-engine shape"
    - "Grace-period derived from pandoc's own compose stop_grace_period arithmetic (65+10+10=85), not the ENGINE_TIMEOUT+30s chart convention used by the other five worker classes"

key-files:
  created:
    - deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml
    - deploy/chart/octoconv/templates/scaledobject-pandoc.yaml
  modified:
    - deploy/chart/octoconv/values.yaml
    - deploy/chart/octoconv/templates/configmap.yaml

key-decisions:
  - "keda.pandoc.scaleDownStabilizationSeconds stays null in production (D-01) — measured 65s worst-case job is comfortably covered by the k8s 300s HPA default; the falsy-0 guard (hasKey AND ne nil) is retained verbatim so values-loadproof.yaml can still override it for the live gate"
  - "keda.pandoc trigger values (threshold 2, pollingInterval 10, cooldownPeriod 90) are numerically identical to keda.html but independently derived from PANDOC_WORKER_CONCURRENCY and the measured p95, not copied (D-02)"
  - "pandocWorker.terminationGracePeriodSeconds=85 mirrors compose's own 65+10+10 derivation exactly, diverging from the ENGINE_TIMEOUT+30s convention used by worker/documentWorker/audioWorker/avWorker/ocrWorker (D-04b)"
  - "SC2 (QueuePandoc in the always-on api queue-depth collector) confirmed present, not re-built — TestAllConvertQueuesCoversEveryEngine passes unchanged"

patterns-established:
  - "pandocWorker.extraEnv override hook added to deployment-pandoc-worker.yaml ahead of need, mirroring documentWorker.extraEnv, for Plan 02's downscale-survival gate to force PANDOC_WORKER_CONCURRENCY=1 without a ConfigMap edit"

requirements-completed: [PDCP-02]

# Metrics
duration: ~20min
completed: 2026-08-01
---

# Phase 50 Plan 01: Pandoc Chart Templates + Values/Env Contract Summary

**pandoc-worker Deployment + KEDA ScaledObject shipped into the Helm chart with FAST-engine tuning (null stabilization) derived from the measured 65s PANDOC_ENGINE_TIMEOUT, plus IN-02 ConfigMap env parity — helm template/lint green, SC2 confirmed unmodified.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-01T19:18:00Z (approx.)
- **Completed:** 2026-08-01T19:38:45Z
- **Tasks:** 2
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments
- `values.yaml` carries the complete pandoc value/env contract: `pandocWorker` deployment/resources block (grace 85s, cpu "2"/memory "1Gi") and `keda.pandoc` tuning block (threshold "2", maxReplicaCount 2, pollingInterval 10, cooldownPeriod 90, scaleDownStabilizationSeconds null), both independently derived and documented (not copied from siblings)
- `configmap.yaml` carries the `PANDOC_*` env group byte-identical to `docker-compose.yml`'s pandoc-worker service, closing the IN-02 parity gap that was previously zero PANDOC hits across the chart
- `deployment-pandoc-worker.yaml` renders a production-parity Deployment: WR-02 replicas-omission, commonEnv envFrom, extraEnv override hook, metrics probes on 9090, cpu "2"/memory "1Gi" limits, terminationGracePeriodSeconds 85
- `scaledobject-pandoc.yaml` renders the WR-01 fail-safe triad verbatim (`ignoreNullValues: "false"`, `fallback: {failureThreshold: 3, replicas: 1}`, retry-inclusive PromQL) with the falsy-0 guard retained even though production stabilization is null, gated on both `keda.enabled` and `prometheus.enabled`
- SC2 confirmed (not rebuilt): `QueuePandoc` already present in `AllConvertQueues()`, `TestAllConvertQueuesCoversEveryEngine` passes

## Task Commits

Each task was committed atomically:

1. **Task 1: values.yaml keda.pandoc + pandocWorker blocks + configmap PANDOC_* env** - `7123114` (feat)
2. **Task 2: deployment-pandoc-worker.yaml + scaledobject-pandoc.yaml templates + SC2 confirm + full render assertion** - `0ed6892` (feat)

_Note: no plan-metadata commit yet — this SUMMARY commit itself serves as the plan-completion record (worktree mode; orchestrator handles STATE.md/ROADMAP.md centrally after merge)._

## Files Created/Modified
- `deploy/chart/octoconv/values.yaml` - added `pandocWorker` deployment/resources block and `keda.pandoc` tuning block; extended the WR-06 invariant comment to cover pandocRetrySchedule
- `deploy/chart/octoconv/templates/configmap.yaml` - added `PANDOC_WORKER_CONCURRENCY`/`PANDOC_MAX_INPUT_BYTES`/`PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY` env group after the OCR block
- `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml` (new) - pandoc-worker Deployment, name-substitution clone of deployment-ocr-worker.yaml + extraEnv hook from deployment-document-worker.yaml
- `deploy/chart/octoconv/templates/scaledobject-pandoc.yaml` (new) - KEDA ScaledObject, null-stabilization skeleton cloned from scaledobject-document.yaml + WR-01 triad from scaledobject-ocr.yaml

## Decisions Made
- Followed CONTEXT.md D-01/D-02/D-03/D-04a/D-04b exactly as specified — no new decisions required during execution; all derivations (grace 85s, tuning 2/10/90, resources 2/1Gi) were pre-computed in planning and verified against `internal/queue/queue.go` (pandocRetrySchedule max step 20s < cooldownPeriod 90s) and `docker-compose.yml` (byte-for-byte env values) during implementation.

## Deviations from Plan

None - plan executed exactly as written. All acceptance criteria in both tasks were met on the first implementation pass; no auto-fixes were required.

## Issues Encountered

None. One process note: the worktree branch (`worktree-agent-a2060d0f5ece32ddc`) was initially stale relative to the expected base commit (still at the v1.10 milestone-archive commit, missing Phase 49's pandoc containerization work) — corrected via `git reset --hard` to the plan's expected base commit `1fe609dd7e0a6ae74ea6bda1100b6ebb522dd64d` per the mandatory worktree-branch-check step before any file edits began.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Chart half of PDCP-02 (SC1 + SC2 + the env-parity half of SC4) is closed; `helm template`/`helm lint` render assertions all pass against production values with keda/prometheus enabled.
- Plan 02 (live loadproof gate) and Plan 03/04 (downscale-survival gate + live evidence) can proceed directly against these templates — the `pandocWorker.extraEnv` hook is already in place for the downscale-survival gate's concurrency override, and `values-loadproof.yaml`'s pandoc overlay block (not yet added — deferred to Plan 02 per the phase's plan boundaries) can rely on the falsy-0 guard in `scaledobject-pandoc.yaml` being verbatim-correct.
- No blockers.

---
*Phase: 50-keda-helm-chart-integration*
*Completed: 2026-08-01*

## Self-Check: PASSED

- FOUND: deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml
- FOUND: deploy/chart/octoconv/templates/scaledobject-pandoc.yaml
- FOUND: .planning/phases/50-keda-helm-chart-integration/50-01-SUMMARY.md
- FOUND commit: 7123114 (Task 1)
- FOUND commit: 0ed6892 (Task 2)
- FOUND commit: 918aaca (SUMMARY.md)
