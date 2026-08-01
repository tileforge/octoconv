# Phase 50: KEDA/Helm Chart Integration - Context

**Gathered:** 2026-08-01
**Status:** Ready for planning

<domain>
## Phase Boundary

The pandoc engine class autoscales in the Kubernetes cluster with production parity to the other six engine classes, and scale-from-zero is LIVE-PROVEN. Concretely: a `pandoc-worker` Deployment + a KEDA `ScaledObject` ship in the Helm chart (`deploy/chart/octoconv`), mirroring the ocr/av/document templates, with grace/tuning derived from the MEASURED `PANDOC_ENGINE_TIMEOUT=65s` (a FAST engine — the first KEDA integration since document whose worst-case job sits far under the k8s 300s HPA default); the always-on api queue-depth collector already resolves the pandoc backlog at zero replicas (SC2 — confirm, not build); and a timestamped live proof captures 0→1 on real queue depth, scale-back-to-0 after drain, and an N→N-1 HPA downscale that an in-flight pandoc job survives without a premature SIGTERM. Covers PDCP-02 (the Helm/KEDA half — the compose/container half completed in Phase 49). Near-verbatim clone of OCR Phase 46 (av 37 / audio 33), with the tuning profile diverging to the fast-engine shape.

**In scope:** `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml` + `scaledobject-pandoc.yaml` (clone ocr/av pair); `values.yaml` `keda.pandoc` block + the pandocWorker deployment/env/resources values; the WR-01 fail-safe triad verbatim; IN-02 `PANDOC_*` env parity across all queue-client services in the chart (configmap/values); `values-loadproof.yaml` pandoc overlay (gate-only stabilization override); `scripts/keda-pandoc-loadproof.sh` + `scripts/keda-pandoc-downscale-survival.sh` (clone the ocr pair); the live scale-from-zero + downscale-survival proof on local OrbStack k8s.
**Out of scope:** any converter/worker/queue-logic change (47/48/49 done); compose-side wiring (done Phase 49); per-engine warm floor / `minReplicaCount>0` (SEED-011, separate); time-of-day/Cron floor (SEED-011 nuance); SEED-010 dynamic registry; →pdf / broad formats (v2).
</domain>

<decisions>
## Implementation Decisions

### KEDA tuning — FAST-engine profile, derived from pandoc's own budget (SC1)
- **D-01:** `keda.pandoc.scaleDownStabilizationSeconds: null` — the document-precedent FAST-engine profile, NOT the audio/av/OCR 900s. Rationale: the measured worst-case job is 65s (worst cell html_large→md p95=32.318s × 2.0), which the k8s 300s HPA default downscale stabilization already covers with >4× margin — a non-null knob would be cargo-culting the heavy engines whose ~742–753s jobs genuinely exceed the 300s default. `scaledobject-pandoc.yaml` still carries the falsy-0 guard (`hasKey` AND `ne … nil`, HARD-03/D-06) exactly like scaledobject-ocr/av — so `values-loadproof.yaml` can override stabilization for the gate (D-04).
- **D-02:** Trigger values DERIVED from pandoc's own budget (never copied): `threshold: "2"` = `PANDOC_WORKER_CONCURRENCY` (one pod digests 2 concurrent jobs — a second replica is justified only at depth >2); `pollingInterval: 10`, `cooldownPeriod: 90` (seconds-scale jobs → fast reaction, early return to 0). Numerically equal to html's profile, but the derivation is pandoc's own (concurrency + measured job cost) — record it that way in comments. The WR-01 fail-safe triad applies VERBATIM: `ignoreNullValues: "false"`, `fallback: {failureThreshold: 3, replicas: 1}`, retry-inclusive PromQL `sum(octoconv_queue_depth{queue="pandoc", state=~"pending|active|retry"})`.
- **D-03:** `maxReplicaCount: 2` (parity with document/html/audio/av/ocr, NOT image's 4). With per-pod concurrency 2 that is already 4 parallel conversions; each pod still reserves 2 CPU/1Gi, so a higher ceiling without load evidence risks node oversubscription — a measured-evidence future tweak (same call as OCR's D-02). `minReplicaCount: 0` hardcoded (warm floor stays SEED-011).

### Resources + grace (SC1 — bound by measurement conditions, not preference)
- **D-04a:** Deployment resources MIRROR the measurement container EXACTLY: `cpu: "2"`, `memory: "1Gi"` — the 49-03 measurement ran at cpus=2.0/memory=1g AND Path-B lowered `PANDOC_MAX_INPUT_BYTES` to 4 MiB specifically for 1g headroom (compose comment: do NOT lower memory below 1g without re-measuring). Not a free knob.
- **D-04b:** `terminationGracePeriodSeconds` derived from the measured 65s exactly like compose: asynq ShutdownTimeout 65+10=75s → grace 75+10=**85s** (mirrors compose `stop_grace_period: 85s`; ocr's 744→764 precedent). Planner records the derivation in the values comment.

### Downscale-survival proof mechanics (SC4)
- **D-05:** The N→N-1 gate uses the **loadproof-overlay + max-size-fixture** shape (document precedent): `values-loadproof.yaml` overrides `keda.pandoc.scaleDownStabilizationSeconds` to a small gate value (production stays null per D-01), and the queue is fed reader-heavy **html→md fixtures @≈4 MiB** (`PANDOC_MAX_INPUT_BYTES` ceiling, the measured worst cell ≈32s) so a GENUINE HPA 2→1 downscale lands inside a live job window. Evidence: the in-flight job completes `done` with NO requeue/retry, pod terminates gracefully. Manual `kubectl scale` explicitly rejected — SC4 says genuine HPA downscale.
  - **D-05a (user decision, 2026-08-01, post-plan-check):** signed-webhook delivery is NOT re-proven in-cluster for the downscale gate. Rationale: an in-cluster receiver would require enabling the webhook SSRF private-IP opt-out just for the gate; the signed-webhook path is engine-agnostic, independent of the downscale path, and already live-proven for pandoc in Phase 49 Plan 04 (`TestPandocConversionE2E`, HMAC-verified against the live compose stack). The plan-checker flagged this as a D-05 scope reduction; the user explicitly accepted it. The gate's evidence contract is: `done` status + zero requeue/retry + graceful pod termination.

### Live-proof logistics (SC3 + SC4)
- **D-06:** ONE OrbStack k8s session runs BOTH gates back-to-back: bring k8s up (compose down first — mutual-exclusion discipline) → `scripts/keda-pandoc-loadproof.sh` (0→1 on real queue depth, back to 0 after drain) → `scripts/keda-pandoc-downscale-survival.sh` (D-05) → EXIT-trap teardown → restore `k8s.enable false`. Fewer OrbStack toggles (each restart is a daemon-wedge risk), two separate timestamped evidence sets under the phase `evidence/` dir. Operator-gated: orchestrator runs it and brings PASS for approval. `:dev` image tags required for api + pandoc-worker + the always-on webhook-worker (replicas 2), per the OrbStack live-proof runbook.

### Claude's Discretion
- **Values-driven Prometheus `scrape_interval`** (planner decision, checker-warned, accepted): `_helpers.tpl`'s scrape config gains a `prometheus.scrapeInterval` values key (production default 15s unchanged — render-verified) so the SC4 gate's ~15–50s job window is observable at gate cadence (5s in values-loadproof.yaml). Record in the 50-02 SUMMARY.
- Exact `deployment-pandoc-worker.yaml` shape (clone the closest fast-engine sibling — pandoc needs no cgroup/OMP bits); values.yaml key ordering; the gate-override stabilization value in values-loadproof.yaml; the loadproof scripts' exact drive parameters (job counts, poll cadence) — all planner/executor's call, provided: the ScaledObject carries the WR-01 triad + falsy-0 guard verbatim, tuning is D-01/D-02/D-03, resources/grace are D-04, minReplicaCount stays 0, and both live proofs land with timestamped evidence.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The OCR Phase 46 KEDA+Helm integration to CLONE (closest, most recent analog)
- `.planning/milestones/v1.10-phases/46-keda-helm-chart-integration/` (46-01..03 PLAN/SUMMARY, 46-VERIFICATION.md, evidence/) — the plan shape: chart templates+values → live loadproof gate → downscale-survival gate (operator-gated checkpoints).
- `deploy/chart/octoconv/templates/scaledobject-ocr.yaml` — the ScaledObject to clone as `scaledobject-pandoc.yaml` (WR-01 triad, falsy-0 guard, retry-inclusive PromQL, co-dependency guard on keda.enabled AND prometheus.enabled).
- `deploy/chart/octoconv/templates/deployment-ocr-worker.yaml` — the Deployment to clone as `deployment-pandoc-worker.yaml` (minus OCR's cgroup/OMP bits pandoc doesn't need).
- `scripts/keda-ocr-loadproof.sh` + `scripts/keda-ocr-downscale-survival.sh` — the live-proof scripts to clone as the pandoc pair (preflight compose-down guard, EXIT-trap teardown, timestamped evidence format).

### The FAST-engine tuning precedent (D-01 diverges from the OCR clone here)
- `deploy/chart/octoconv/values.yaml` `keda:` block — document's `scaleDownStabilizationSeconds: null` comment ("production default OFF, K8s 300s HPA default preserved; load-proof gate overrides via values-loadproof.yaml") is D-01's template; audio/av/ocr's 900s comments show the heavy-engine contrast to cite.
- `deploy/chart/octoconv/values-loadproof.yaml` — the gate-only override overlay (add the pandoc block).

### Measured inputs (from Phase 49 — the numbers every derivation consumes)
- `docker-compose.yml` pandoc-worker service — `PANDOC_ENGINE_TIMEOUT=65s` (measured: worst cell html_large→md p95=32.318s × 2.0, arm64, cpus=2.0/1g, N=10), `stop_grace_period: 85s` (75s asynq ShutdownTimeout + 10s), `PANDOC_WORKER_CONCURRENCY=2`, `PANDOC_MAX_INPUT_BYTES=4MiB` (Path-B, memory headroom at 1g — do NOT lower the 1Gi limit without re-measuring), resource limits cpus 2.0/1g.
- `.planning/phases/49-containerization-measured-timeout-gate/49-03-SUMMARY.md` + the PROJECT.md Key Decision — the full measurement derivation.

### Env parity + collector (SC2 — already satisfied, confirm only)
- `internal/queue/queue.go:814` — `AllConvertQueues()` already returns `QueuePandoc` (Phase 48), so the always-on api collector emits `octoconv_queue_depth{queue="pandoc",…}` at zero replicas. SC2 is DONE — this phase confirms it, not builds it.
- `deploy/chart/octoconv/templates/configmap.yaml` + `values.yaml` per-service env — where `PANDOC_*` must reach IN-02 parity across every queue-client service in the chart.

### Requirements + live-proof runbook
- `.planning/REQUIREMENTS.md` — PDCP-02. `.planning/ROADMAP.md` §"Phase 50" — the four Success Criteria (non-negotiable contract).
- OrbStack live-proof runbook (memory: orbstack-k8s-live-proof): k8s.enable toggle + orbctl stop discipline, compose/k8s mutual exclusion, `:dev` image tags incl. webhook-worker, volume-prune safety, evidence/teardown conventions.
- `.planning/seeds/SEED-011.md` — the deferred warm-floor change.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The full `deploy/chart/octoconv` chart with six per-engine `deployment-*-worker.yaml` + `scaledobject-*.yaml` pairs — pandoc is a near-verbatim clone of the ocr pair with fast-engine tuning.
- `internal/queue/queue.go` collector already emits the pandoc queue-depth metric (Phase 48) — no code change for SC2.
- `scripts/keda-ocr-loadproof.sh` / `keda-ocr-downscale-survival.sh` + `values-loadproof.yaml` — the live-proof tooling to clone.

### Established Patterns
- Per-engine ScaledObject: WR-01 triad, retry-inclusive PromQL, falsy-0 guard; stabilization NON-NULL only when worst-case job exceeds the 300s HPA default (audio/av/ocr) — pandoc (65s) takes the document-style null.
- Grace-period derived from the measured engine timeout (65→75→85s), never guessed.
- IN-02 env parity: every queue.NewClient-constructing service sets PANDOC_* identically (compose already does — mirror in chart).
- Operator-gated live checkpoints with timestamped evidence + EXIT-trap teardown.

### Integration Points
- `deploy/chart/octoconv/templates/` (new deployment + scaledobject), `values.yaml` (`keda.pandoc` + `pandocWorker` block), configmap (PANDOC_* env), `values-loadproof.yaml` (pandoc gate override), `scripts/` (two new gate scripts), and the live OrbStack k8s cluster (KEDA + Prometheus) for the proofs.
</code_context>

<specifics>
## Specific Ideas
- The pandoc ScaledObject is a mechanical clone of `scaledobject-ocr.yaml` with `queue="pandoc"`, `scaleTargetRef: pandoc-worker`, and the fast-engine values from D-01/D-02/D-03 — triad + falsy-0 guard copied verbatim; the stabilization block simply stays inactive in production because the value is null.
- Downscale gate: feed ≈4 MiB reader-heavy html→md jobs (the measured worst cell, ~32s each) so the HPA 2→1 lands mid-job; overlay-lowered stabilization makes the window catchable — production tuning untouched.
- One k8s session for both gates; orchestrator runs everything and brings evidence for approval (gsd-driving-style).
</specifics>

<deferred>
## Deferred Ideas
- **SEED-011 — per-engine warm floor** (templatized `minReplicaCount`) — separate `/gsd:quick`-sized change, now spanning seven ScaledObjects.
- **maxReplicaCount > 2 for pandoc** — measured-evidence future tweak if pandoc load proves burstier (its light jobs would make it the first candidate for a higher ceiling).
- **SEED-010 — dynamic capability registry** — dormant.
- After the phase: v1.11 milestone close-out → PR; tag @codex for review on the PR (user request, 2026-08-01).
</deferred>

---

*Phase: 50-keda-helm-chart-integration*
*Context gathered: 2026-08-01*
