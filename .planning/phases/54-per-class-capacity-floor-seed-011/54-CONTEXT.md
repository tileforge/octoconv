# Phase 54: Per-Class Capacity Floor (SEED-011) - Context

**Gathered:** 2026-08-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Make `minReplicaCount` an operator-settable Helm values field for each of the seven engine
classes, replacing the `0` currently hardcoded in every `scaledobject-*.yaml`. The default stays
`0`, so the current deployment's behavior does not change at all.

This is a chart-only phase — `deploy/chart/octoconv/` plus one CI job. No Go code, no schema, no
queue topology, no PromQL trigger changes. It is fully independent of Phases 51-53.

**Out of scope:** any change to `threshold`, `maxReplicaCount`, `pollingInterval`,
`cooldownPeriod`, `scaleDownStabilizationSeconds`, or the PromQL queries. Deciding *when* an
operator should actually raise a floor is an operational judgment, not a chart change.

</domain>

<decisions>
## Implementation Decisions

### Values shape and rendering

- **D-01:** `minReplicaCount` lives in the existing per-class `keda.<class>` block in
  `values.yaml`, alongside `threshold` / `maxReplicaCount` / `pollingInterval` /
  `cooldownPeriod`. It is written **explicitly as `0`** for all seven classes, and the template
  renders it directly with no conditional wrapper.

  **Why this shape specifically:** the chart already carries an explicit falsy-0 guard
  (`and (hasKey ...) (ne ... nil)`, labelled `HARD-03/D-06`) around
  `scaleDownStabilizationSeconds`, because Helm treats `0` as falsy and a bare `if` would
  silently drop it. `minReplicaCount`'s default *is* `0`, so it sits squarely in that trap.
  Rather than repeat the guard, this phase removes the possibility: with no conditional in the
  template, there is nothing for a falsy `0` to be swallowed by. The guard pattern is the right
  answer for a genuinely optional block (`advanced:`); `minReplicaCount` is a required scalar
  field on every `ScaledObject`, so the analogy does not carry over.

  Secondary benefit: an operator reading `values.yaml` sees the default without having to open
  the template.

- **D-02:** The template validates and fails fast via `{{ fail }}` on two conditions:
  a negative `minReplicaCount`, and `minReplicaCount > maxReplicaCount`. Error messages name the
  class and both values.

  **Why in the template rather than leaving it to KEDA:** SC3 requires `helm template` / `helm
  lint` to be the verification surface, with no live cluster. A KEDA admission webhook rejection
  only surfaces at `helm install` time, i.e. outside the gate this phase is graded on. Six of the
  seven classes have `maxReplicaCount: 2`, so `min > max` is an easy mistake to make.

- **D-03:** Both the validation and the value render go through a single `define` in
  `templates/_helpers.tpl`; each of the seven `scaledobject-*.yaml` files gets a one-line
  `include` call. This matches the chart's established pattern (`octoconv.commonEnv`,
  `octoconv.prometheusScrapeConfig`).

  **Why:** inline would mean ~42 lines duplicated across seven files, and the realistic failure
  is fixing six of seven. A one-line-per-class diff also makes SC2 ("a template diff touching
  only the intended class") trivially readable.

- **D-04:** The three existing overlays (`values-local.yaml`, `values-e2e.yaml`,
  `values-loadproof.yaml`) are **not** modified — they inherit `0` from `values.yaml`, so no
  existing gate changes behavior. SC3's "example non-zero override" is a **new**
  `values-capacity-floor.yaml` that sets a non-zero floor for exactly one class.

  **Why not put the example in `values-loadproof.yaml`:** that gate exists to prove
  scale-from-zero. A non-zero floor removes the very zero it is testing, so the gate would stop
  proving what it was built to prove.

### Failure behavior

- **D-05:** `fallback.replicas` becomes `max(1, minReplicaCount)` rather than the currently
  hardcoded `1`, computed by the same helper family.

  **Why this is in scope rather than creep:** all seven classes hardcode
  `fallback: {failureThreshold: 3, replicas: 1}`. Introducing an operator-settable floor without
  touching this creates a configuration where the two contradict each other — an operator sets
  `minReplicaCount: 3` to eliminate cold start, and then a Prometheus outage drops the class to
  1, i.e. the floor silently fails exactly when cold-start latency hurts most. At the default
  `minReplicaCount: 0` the expression yields `1`, so the rendered output is byte-identical to
  today and SC1's "zero behavior change" holds.

### Verification

- **D-06:** A new `helm` job in `.github/workflows/ci.yml` covers SC2 and SC3:
  `helm lint`; `helm template` with default values; `helm template` with
  `values-capacity-floor.yaml`; a `diff` of the two renders asserting exactly one `ScaledObject`
  changed; and a **negative** case asserting the D-02 validation actually fires (e.g.
  `--set keda.image.minReplicaCount=99` against `maxReplicaCount: 4` must exit non-zero).

  **Why CI rather than one-shot commands in the plan:** the chart currently has **no** CI
  coverage of any kind — no `helm lint`, no `helm template`. A one-shot check satisfies SC3 on
  the day it runs and protects nothing afterward. The job needs no cluster and runs in seconds.

  The negative case is load-bearing: without it, a validation block that never fires would pass
  every positive check.

### Claude's Discretion

- Exact helper naming and whether D-02's validation and D-05's `max(1, …)` live in one `define`
  or two.
- Exact wording of the `fail` messages, beyond naming the class and both values.
- Which class `values-capacity-floor.yaml` demonstrates, and the non-zero value it uses.
- Placement and naming of the CI job within `ci.yml`, and whether it runs on all pushes or only
  when `deploy/chart/**` changes.
- Comment style and depth in the modified templates (the chart's existing templates carry long
  rationale comments; matching that register is expected).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Chart — files this phase modifies
- `deploy/chart/octoconv/values.yaml` — the `keda.<class>` blocks that gain `minReplicaCount`
  (see the existing `scaleDownStabilizationSeconds` entries for the comment register this chart
  uses to justify per-class divergence)
- `deploy/chart/octoconv/templates/_helpers.tpl` — six existing `define`s; the pattern D-03 follows
- `deploy/chart/octoconv/templates/scaledobject-image.yaml` — hardcoded `minReplicaCount: 0` at line 27
- `deploy/chart/octoconv/templates/scaledobject-document.yaml` — line 45
- `deploy/chart/octoconv/templates/scaledobject-html.yaml` — line 24
- `deploy/chart/octoconv/templates/scaledobject-audio.yaml` — line 36
- `deploy/chart/octoconv/templates/scaledobject-av.yaml` — line 35
- `deploy/chart/octoconv/templates/scaledobject-ocr.yaml` — line 35
- `deploy/chart/octoconv/templates/scaledobject-pandoc.yaml` — line 44

### The falsy-0 precedent D-01 reasons about
- `deploy/chart/octoconv/templates/scaledobject-av.yaml` §11-22, §39 — the `HARD-03/D-06` falsy-0
  guard on `scaleDownStabilizationSeconds`, gated on **both** `hasKey` and `ne … nil`. Read the
  header comment: it states explicitly why `hasKey` alone was insufficient.
- `deploy/chart/octoconv/templates/scaledobject-ocr.yaml`, `scaledobject-audio.yaml` — the same
  guard, for cross-checking that D-03's helper does not disturb it.

### Overlays (D-04 — read to confirm they are left alone)
- `deploy/chart/octoconv/values-local.yaml`
- `deploy/chart/octoconv/values-e2e.yaml`
- `deploy/chart/octoconv/values-loadproof.yaml` — the load-proof gate that D-04 deliberately
  does not touch, because it exists to prove scale-from-zero

### Verification
- `.github/workflows/ci.yml` — currently contains `gate`, `race`, `docker-build`, `e2e`; no helm
  step of any kind. D-06 adds one. Note `e2e` uses `continue-on-error` on `pull_request`.

### Requirements
- `.planning/ROADMAP.md` §"Phase 54: Per-Class Capacity Floor (SEED-011)" — goal and SC1/SC2/SC3
- `.planning/REQUIREMENTS.md` — CAPA-01

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `templates/_helpers.tpl`: already defines `octoconv.name`, `octoconv.fullname`,
  `octoconv.labels`, `octoconv.selectorLabels`, `octoconv.commonEnv`,
  `octoconv.prometheusScrapeConfig`. D-03's new `define` joins an established pattern rather than
  introducing one.
- The `keda.<class>` values blocks already exist for all seven classes, each with a consistent
  key set. `minReplicaCount` slots in without restructuring anything.

### Established Patterns
- **Per-class divergence is documented inline, at length.** `values.yaml`'s
  `scaleDownStabilizationSeconds` entries carry multi-line comments explaining why audio/av/ocr
  are non-null (job durations of 742s/753s/744s exceed the k8s 300s HPA default) while
  document/html are null. New per-class knobs are expected to explain themselves in the same
  register — a bare `minReplicaCount: 0` with no comment would be out of place.
- **Falsy-0 is a known, already-bitten hazard in this chart** (`HARD-03/D-06`). D-01 addresses it
  structurally rather than by repeating the guard.
- **ScaledObjects are gated on `and .Values.keda.enabled .Values.prometheus.enabled`** — a
  documented co-dependency guard preventing a ScaledObject from dangling against a Service that
  was never created. Any `helm template` invocation in D-06 must set **both** flags or it renders
  zero ScaledObjects and silently proves nothing.
- All seven ScaledObjects use `ignoreNullValues: "false"` plus `fallback`, deliberately failing
  toward availability on scaler error. D-05 extends that same philosophy to the floor.

### Integration Points
- `_helpers.tpl` define → seven `scaledobject-*.yaml` include sites (D-03).
- `values.yaml` `keda.<class>.minReplicaCount` → the helper (D-01).
- `values-capacity-floor.yaml` → `helm template -f` in the new CI job (D-04, D-06).
- `.github/workflows/ci.yml` → new `helm` job (D-06).

</code_context>

<specifics>
## Specific Ideas

- The `helm template` commands in D-06 must pass `--set keda.enabled=true --set
  prometheus.enabled=true`. Without both, the co-dependency guard renders no ScaledObject at all
  and every assertion would pass vacuously.
- D-06's diff assertion should be specific about *which* object changed, not merely that the diff
  is small — SC2 is about isolation between classes.
- The negative validation case should target a class where the violation is unambiguous; six of
  seven classes have `maxReplicaCount: 2`, `image` has `4`.

</specifics>

<deferred>
## Deferred Ideas

- **Guidance on when to raise a floor.** Choosing a non-zero `minReplicaCount` is an operational
  decision driven by observed traffic and cold-start cost. Phase 53's `GET /v1/system/usage` is
  the data source that would inform it. Documenting that judgment is a docs concern, not a chart
  change, and is out of scope here.
- **Interaction between a non-zero floor and `cooldownPeriod` / `scaleDownStabilizationSeconds`.**
  A non-zero floor makes some of the downscale tuning partly moot for that class. Worth revisiting
  once a floor is actually in production use — speculative until then.
- **Extending the new CI helm job to render all overlays** (`values-local`, `values-e2e`,
  `values-loadproof`). Clear value in catching chart regressions broadly, but it widens this
  phase from CAPA-01 into general chart-CI work. The job D-06 adds is the natural place to grow
  that later.

### Reviewed Todos (not folded)
- `2026-08-05-per-row-enforced-flag.md` — matched at 0.6 by the todo matcher, but purely on
  keyword overlap ("add", "per", "operator", "review"). It is a Go/API change to
  `internal/api/system_usage_handlers.go` from Phase 53. This phase touches no Go code.
- `2026-08-05-usage-row-empty-enforcement-mode.md` — same false-positive match, same reason.

</deferred>

---

*Phase: 54-per-class-capacity-floor-seed-011*
*Context gathered: 2026-08-06*
