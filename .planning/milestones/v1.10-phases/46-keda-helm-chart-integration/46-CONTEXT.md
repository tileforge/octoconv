# Phase 46: KEDA/Helm Chart Integration - Context

**Gathered:** 2026-07-26
**Status:** Ready for planning

<domain>
## Phase Boundary

The OCR engine class autoscales in the Kubernetes cluster with production parity to the other five engine classes, and scale-from-zero is LIVE-PROVEN. Concretely: an `ocr-worker` Deployment + a KEDA `ScaledObject` ship in the Helm chart (`deploy/chart/octoconv`), mirroring the av/audio templates, with grace/stabilization derived from the measured `OCR_ENGINE_TIMEOUT=744s`; the always-on api queue-depth collector already resolves the OCR backlog at zero replicas; and a timestamped live proof captures 0→1 on real queue depth, scale-back-to-0 after drain, and an N→N-1 downscale that a long multi-page OCR job survives without a premature SIGTERM. Covers OCRP-02 (the Helm/KEDA half).

**In scope:** `deploy/chart/octoconv/templates/deployment-ocr-worker.yaml` + `scaledobject-ocr.yaml` (clone av); `values.yaml` `keda.ocr` block + the ocr-worker deployment/env/resources values; the WR-01 fail-safe triad verbatim; env-parity (IN-02) for the `OCR_*` vars across all queue-client services in the chart (configmap/secret/values); the live scale-from-zero + downscale-survival proof (Phase-33/37 harness on local OrbStack k8s).
**Out of scope:** any converter/worker-logic change (43/44/45 done); the compose-side wiring (done Phase 45); a per-engine warm floor / `minReplicaCount>0` (SEED-011, separate); a time-of-day/Cron floor (SEED-011 nuance, not built); SEED-010 dynamic registry.
</domain>

<decisions>
## Implementation Decisions

### KEDA scale-down tuning (SC1)
- **D-01:** The `keda.ocr` values MIRROR av/audio 1:1, because the measured `OCR_ENGINE_TIMEOUT=744s` sits between audio's 742s and av's 753s: `scaleDownStabilizationSeconds: 900` (744s worst-case job + ~21% margin, aligned with the 900s/15m `RECONCILER_ACTIVE_STALE_AFTER` cap — a genuine downscale never races a live job; NON-NULL and load-bearing in production, same rationale as audio/av, not just a load-proof-overlay knob), `cooldownPeriod: 180`, `pollingInterval: 15`, `threshold: "1"`. The `scaledobject-ocr.yaml` gates the stabilization block on BOTH `hasKey` AND `ne … nil` (the falsy-0 guard, HARD-03/D-06) exactly like `scaledobject-av.yaml`.
- **D-02:** `maxReplicaCount: 2` (av/audio parity), NOT image's 4 — OCR is a long, CPU-bound job (compose limits cpus:2/memory:1g, `OCR_WORKER_CONCURRENCY=1`), so a conservative ceiling matches the other heavy engines and avoids node CPU oversubscription. A higher ceiling is a measured-evidence future tweak.
- **D-03:** The WR-01 fail-safe triad is applied VERBATIM from the first commit: `ignoreNullValues: "false"` (a sustained absent PromQL result reads as scaler ERROR, not queue-empty), `fallback: {failureThreshold: 3, replicas: 1}`, and the retry-inclusive PromQL `sum(octoconv_queue_depth{queue="ocr", state=~"pending|active|retry"})` (retry-state tasks are imminent work — never scheduled/archived).

### Warm floor (SC1 — scale-from-zero)
- **D-04:** `scaledobject-ocr.yaml` hardcodes `minReplicaCount: 0` (pure scale-from-zero), exactly like every existing sibling. The per-engine warm floor (templatizing `minReplicaCount` across ALL six ScaledObjects) stays **SEED-011** — a separate `/gsd:quick`-sized chart change — and is NOT folded into this phase (anti-scope-creep). This keeps OCR consistent with siblings and defers the cross-cutting templatization to one deliberate pass.

### Live proof (SC3 + SC4)
- **D-05:** Reuse the Phase-33/37 load-proof harness on a LOCAL OrbStack Kubernetes cluster (compose brought DOWN first — the OrbStack compose/k8s mutual-exclusion discipline). Capture TIMESTAMPED evidence for BOTH: (a) scale-from-zero — 0→1 on real OCR queue depth, then scale back to 0 after drain; (b) downscale-survival — a long multi-page OCR job survives a genuine N→N-1 HPA downscale WITHOUT a premature SIGTERM (protected by `terminationGracePeriodSeconds` derived from the 744s timeout + the graceful-shutdown path, mirroring Phase-37). This is an operator-gated live checkpoint (orchestrator runs it, brings evidence for approval).

### Claude's Discretion
- The exact `deployment-ocr-worker.yaml` resource requests/limits (mirror av's cpus:2/1g and the OMP_THREAD_LIMIT/cgroup discipline), the `terminationGracePeriodSeconds` value (derive from the 744s timeout + margin, mirroring av's `stop_grace_period=764s`/773s precedent), the values.yaml key ordering, and the exact load-proof harness invocation — all executor/planner's call, provided: the ScaledObject mirrors av's WR-01 triad, the tuning values are D-01/D-02, minReplicaCount stays 0, and both live proofs are captured with timestamped evidence.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The av/audio KEDA+Helm templates to CLONE
- `deploy/chart/octoconv/templates/scaledobject-av.yaml` — the ScaledObject to clone as `scaledobject-ocr.yaml` (WR-01 triad, falsy-0 guard, retry-inclusive PromQL, non-null scaleDownStabilization block).
- `deploy/chart/octoconv/templates/scaledobject-audio.yaml` — the sibling with the identical 900s-stabilization rationale (742s job).
- `deploy/chart/octoconv/templates/deployment-av-worker.yaml` — the Deployment to clone as `deployment-ocr-worker.yaml` (env, resources, terminationGracePeriodSeconds, graceful shutdown).
- `deploy/chart/octoconv/values.yaml` — the `keda:` block (image/document/html/audio/av) to extend with a `keda.ocr` block (D-01/D-02); the per-service env where OCR_* must reach IN-02 parity.

### Env parity + collector (SC2 — already satisfied)
- `internal/queue/queue.go:704` — the always-on collector queue list already returns `QueueOCR` (added Phase 44), so KEDA resolves the OCR backlog at zero replicas. SC2 is DONE — this phase confirms it, not builds it.
- `internal/queue/queue.go:42` — `QueueOCR = convert.EngineOCR` (the `queue="ocr"` label the PromQL selects on).

### Measured inputs (from Phase 45)
- `docker-compose.yml` ocr-worker service — `OCR_ENGINE_TIMEOUT=744s`, `stop_grace_period=764s`, `OCR_MAX_PAGES=64`, `OCR_WORKER_CONCURRENCY=1` (the measured values the chart's grace/stabilization derive from).
- `.planning/phases/45-containerization-measured-timeout-gate/45-03-SUMMARY.md` + the PROJECT.md Key Decision — the measured-timeout derivation (worst-cell 5.81 s/page, Path-B 200→64, 17.3% under the 900s CAP).

### Requirements + prior live-proof precedent
- `.planning/REQUIREMENTS.md` — OCRP-02.
- `.planning/ROADMAP.md` §"Phase 46" — the four Success Criteria (the non-negotiable contract).
- Phase 33 / Phase 37 live scale-from-zero + downscale-survival proofs — the harness + evidence format to reuse (D-05); `deploy/chart/octoconv/values-loadproof.yaml` is the load-proof overlay.
- `.planning/seeds/SEED-011.md` — the deferred warm-floor change (D-04).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The full `deploy/chart/octoconv` chart with per-engine `deployment-*-worker.yaml` + `scaledobject-*.yaml` pairs — OCR is a near-verbatim clone of the av pair.
- `internal/queue/queue.go`'s collector (already emits `octoconv_queue_depth{queue="ocr",…}`) — the metric KEDA's PromQL scaler reads; no code change needed for SC2.
- The Phase-33/37 load-proof harness + `values-loadproof.yaml` overlay — the live-proof tooling.

### Established Patterns
- Per-engine ScaledObject with the WR-01 fail-safe triad, retry-inclusive PromQL, falsy-0 guard, and a NON-NULL `scaleDownStabilizationSeconds` for engines whose worst-case job exceeds the k8s 300s HPA default (audio 742s, av 753s → both 900s; OCR 744s → 900s).
- Grace-period derived from the measured engine timeout (av `stop_grace_period` 764s/773s), never guessed.
- IN-02 env parity: every queue.NewClient-constructing service sets the engine's env identically.

### Integration Points
- `deploy/chart/octoconv/templates/` (new deployment + scaledobject), `values.yaml` (`keda.ocr` + ocr-worker block), configmap/secret (OCR_* env), and the live cluster (KEDA + Prometheus) for the scale proof.
</code_context>

<specifics>
## Specific Ideas

- The OCR ScaledObject is a mechanical clone of `scaledobject-av.yaml` with `queue="ocr"`, `scaleTargetRef: ocr-worker`, and the `keda.ocr` values from D-01/D-02 — the WR-01 triad and falsy-0 guard copied verbatim.
- The live proof mirrors av/audio's own Phase live gates: orchestrator brings OrbStack k8s up, drives the OCR queue, captures the 0→1→0 + N→N-1 timestamps, brings evidence for approval.
</specifics>

<deferred>
## Deferred Ideas

- **SEED-011 — per-engine warm floor:** templatize `minReplicaCount` across all six ScaledObjects (default 0, set `av: 1` etc.) — a separate `/gsd:quick`-sized chart change, not this phase.
- **Time-of-day / scheduled floor:** a KEDA Cron scaler trigger (SEED-011 nuance) — note only, not built.
- **maxReplicaCount > 2 for OCR:** a measured-evidence future tweak if OCR load proves burstier than av/audio.
- **SEED-010 — dynamic capability registry:** a self-registering worker declaring its own min_replicas/capabilities — separate, dormant.

</deferred>

---

*Phase: 46-keda-helm-chart-integration*
*Context gathered: 2026-07-26*
