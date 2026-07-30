# Phase 46: KEDA/Helm Chart Integration - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-26
**Phase:** 46-keda-helm-chart-integration
**Areas discussed:** KEDA scale-down tuning, Warm-floor (SEED-011), Live-proof scope/environment

---

## KEDA scale-down tuning (maxReplicaCount)

| Option | Description | Selected |
|--------|-------------|----------|
| 2 (av/audio parity) | maxReplicaCount=2, rest mirrors av/audio; conservative, CPU-bound engine | ✓ |
| 4 (like image) | maxReplicaCount=4 for burstier traffic; risks node CPU oversubscription | |

**User's choice:** 2 (av/audio parity)
**Notes:** Base values mirror av/audio 1:1 (scaleDownStabilizationSeconds=900, cooldownPeriod=180, pollingInterval=15, threshold="1") — the measured OCR_ENGINE_TIMEOUT=744s sits between audio 742s and av 753s.

---

## Warm-floor (SEED-011)

| Option | Description | Selected |
|--------|-------------|----------|
| OCR = scale-from-zero, SEED-011 separate | scaledobject-ocr.yaml hardcodes minReplicaCount:0 like siblings; warm-floor stays a separate /gsd:quick change | ✓ |
| Templatize minReplicaCount now | Templatize across all 6 ScaledObjects this phase (closes SEED-011); expands phase scope | |

**User's choice:** OCR = scale-from-zero, SEED-011 separate
**Notes:** Anti-scope-creep — roadmap SC1 mandates minReplicaCount:0; the cross-cutting templatization is a deliberate separate pass.

---

## Live-proof scope/environment

| Option | Description | Selected |
|--------|-------------|----------|
| Phase-33/37 harness, OrbStack k8s, full evidence | Reuse the load-proof harness; capture 0→1→0 + N→N-1 downscale-survival, timestamped | ✓ |
| Only scale-from-zero (0→1→0) | Minimal proof; rely on stop_grace_period + Phase-37 precedent for survival | |

**User's choice:** Phase-33/37 harness, OrbStack k8s, full evidence
**Notes:** Operator-gated live checkpoint; compose down first (OrbStack mutual-exclusion). Both SC3 (scale-from-zero) and SC4 (downscale-survival) proven live.

## Claude's Discretion

- Exact deployment-ocr-worker.yaml resource requests/limits, terminationGracePeriodSeconds value, values.yaml key ordering, and load-proof harness invocation.

## Deferred Ideas

- SEED-011 per-engine warm floor (templatize minReplicaCount across all six ScaledObjects).
- Time-of-day / scheduled floor (KEDA Cron scaler) — SEED-011 nuance, not built.
- maxReplicaCount > 2 for OCR — measured-evidence future tweak.
- SEED-010 dynamic capability registry.
