---
id: SEED-011
status: implemented
implemented_in: "v1.12 Phase 54 (CAPA-01) — `minReplicaCount` templatized out of all 7 `scaledobject-*.yaml` into per-class Helm values via the validating `octoconv.kedaMinReplicaCount` helper, default `0` (zero behavior change); `fallback.replicas` derived as `max(1, minReplicaCount)` through `octoconv.kedaFallbackReplicas` so validation cannot be bypassed; negative/over-max/non-integer values abort the render; `values-capacity-floor.yaml` ships as the example override and a CI `helm` job proves per-class isolation. The `av: { minReplicaCount: 1 }` warm floor this seed suggested was NOT applied — the mechanism ships, choosing the floor is left to the operator. Verified 2026-08-06."
planted: 2026-07-26
planted_during: post-v1.9 (design discussion after milestone close)
trigger_when: when an engine has steady enough load to justify a warm floor (avoid cold-start), OR when tuning per-engine autoscaling
scope: Small
---

# SEED-011: Per-engine KEDA warm floor (configurable minReplicaCount)

Let an operator keep a **minimum number of workers always active per engine class** (a "warm floor"), instead of every class being hardwired to scale-from-zero. Example: always keep ≥1 av (video) worker running; when the video queue backs up, scale up to `maxReplicaCount`; when it drains, shrink back down to the floor (1), not to 0. The floor is set manually per engine.

## Why This Matters

Every engine's `ScaledObject` currently hardcodes `minReplicaCount: 0` (scale-from-zero). That's ideal for bursty/rare engines (zero idle cost), but for a **steadily-loaded** engine it means every lull pays a cold-start on the next job: KEDA activation + a heavy image pull (av's ffmpeg-from-source image is large) + container boot before the first video even starts decoding. A warm floor of 1 removes that latency for the busy engine, at the cost of always paying for 1 replica — a per-engine policy the operator should be able to choose.

## What ships (Small — /gsd:quick-sized)

This is NOT a milestone — the KEDA mechanism already exists; only one value is missing.

1. **Templatize the floor.** In the ScaledObject templates (`deploy/chart/octoconv/templates/scaledobject-image.yaml:27` and the per-class siblings -document/-html/-audio/-av), `minReplicaCount: 0` is hardcoded while `maxReplicaCount`/`pollingInterval`/`cooldownPeriod` are already `.Values.keda.<class>.*`. Change to `minReplicaCount: {{ .Values.keda.<class>.minReplicaCount | default 0 }}`.
2. **Add the value.** In `deploy/chart/octoconv/values.yaml`, add `minReplicaCount` to each class block, default `0` (preserves today's scale-from-zero behavior for everyone), and set `av: { minReplicaCount: 1 }` for the warm video floor.
3. **Verify** with a `helm template` render assertion (floor renders per value) — no new live proof needed; the scale-up/drain behavior is unchanged.

Result: `av` worker stays ≥1 always, scales up to `av.maxReplicaCount` under backlog, and after `cooldownPeriod` shrinks back to 1. Tunable by editing `values.yaml` + `helm upgrade`.

## Nuances / invariants to respect

- **Tradeoff is per engine:** warm floor kills cold-start but always costs the floor's replicas. Keep `0` for bursty/rare engines; use `1+` only where load is steady (av is the obvious candidate).
- **Existing invariants still hold:** `cooldownPeriod > engine timeout` (WR-06, values.yaml) and the downscale-survival guarantee (Phase 37 live-proof — a scale-down never kills an in-flight job, via `stop_grace_period`/graceful shutdown). With `min≥1` the "cool down to 0 mid-job" risk disappears entirely; the N→N-1 survival case still applies.
- **Static floor vs scheduled floor:** a plain `minReplicaCount` is a *static* floor (always N). If a *time-of-day* floor is ever wanted (e.g. 2 during business hours, 0 at night), that's a separate **KEDA Cron scaler** trigger, not this — note it but don't build it unless asked.

## Relation to SEED-010

Composes with the dynamic capability registry (SEED-010): a capability row could carry its own `min_replicas`, making the warm floor **data-driven** per capability instead of a chart value — so a self-registered worker declares its own floor without a chart edit. If SEED-010 is built, fold this into it; if not, this stands alone as a small chart change.

## Breadcrumbs

- `deploy/chart/octoconv/templates/scaledobject-image.yaml` (+ -document/-html/-audio/-av siblings) — `minReplicaCount: 0` hardcode to templatize.
- `deploy/chart/octoconv/values.yaml` (~:171-226) — per-class `keda.<class>` blocks (max/polling/cooldown already values; add min).
- Phase 27 (KEDA autoscaling) + Phase 37 (av KEDA) — the existing scale-from-zero + downscale-survival proofs.
