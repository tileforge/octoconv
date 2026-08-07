# Phase 54: Per-Class Capacity Floor (SEED-011) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-06
**Phase:** 54-per-class-capacity-floor-seed-011
**Areas discussed:** Форма ключа в values, Ловушка falsy-0, Взаимодействие с fallback, Как доказываем SC3

---

## Ловушка falsy-0 → values shape and rendering (D-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Всегда явный 0 | `minReplicaCount: 0` written explicitly in values.yaml for all 7 classes; template renders it directly with no conditional. No conditional means falsy-0 is unreachable. Default visible in values rather than hidden in the template. | ✓ |
| Явный 0 + default-страховка | Same, but template uses `\| default 0`. Guards against a deleted key — but Helm's `default` fires on any falsy value including an explicit `0`, so it is harmless here yet misleading as a pattern. | |
| Опциональный + hasKey guard | Key absent from values.yaml; template gated on `and (hasKey …) (ne … nil)`, mirroring `scaleDownStabilizationSeconds`. Maximum symmetry with the existing pattern. | |

**User's choice:** Всегда явный 0
**Notes:** The deciding argument was that the analogy to `scaleDownStabilizationSeconds` is
incomplete: that guard protects an *optional* `advanced:` block, whereas `minReplicaCount` is a
required scalar on every `ScaledObject`. Removing the conditional removes the trap outright
rather than defending against it.

---

## Ловушка falsy-0 → invalid values (D-02)

| Option | Description | Selected |
|--------|-------------|----------|
| fail в шаблоне | `{{ fail }}` on negative `minReplicaCount` and on `min > max`, caught by `helm template` / `helm lint` — the same run SC3 already requires, no cluster needed. Cost: ~6 lines × 7 files, or one helper. | ✓ |
| Полагаемся на KEDA | No template validation; KEDA's admission webhook rejects at install time. Zero boilerplate, but the error surfaces outside the gate this phase is graded on. | |
| Только min <= max | Validate only the relation to `maxReplicaCount` — the mistake operators actually make. Negative values left to KEDA. Half the boilerplate. | |

**User's choice:** fail в шаблоне
**Notes:** SC3's verification surface is `helm template` with no live cluster, so anything only
KEDA catches falls outside the phase gate entirely.

---

## Форма ключа в values → helper vs inline (D-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Хелпер | One `define` in `_helpers.tpl`, 7 one-line call sites. Matches the chart's existing pattern (`octoconv.commonEnv`, `octoconv.prometheusScrapeConfig`). Logic lives in one place — cannot fix 6 of 7 classes. | ✓ |
| Инлайн в 7 файлах | ~6 lines of validation in each `scaledobject-*.yaml`. Each file reads standalone without jumping to `_helpers.tpl`, at the cost of 42 duplicated lines and a real desync risk. | |

**User's choice:** Хелпер
**Notes:** Confirmed during scouting that `_helpers.tpl` already carries six `define`s, so this
follows an established convention rather than introducing one. Side benefit: a one-line-per-class
diff makes SC2 ("diff touches only the intended class") trivially readable.

---

## Форма ключа в values → overlays and the SC3 example (D-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Оверлеи не трогаем + отдельный пример | `values-local` / `values-e2e` / `values-loadproof` inherit `0` from values.yaml and stay untouched; a new `values-capacity-floor.yaml` demonstrates a non-zero override for SC3. | ✓ |
| Прописать 0 во всех оверлеях | Add explicit `minReplicaCount: 0` to all three overlays for completeness. 21 lines that change nothing plus three new desync sites. | |
| Пример внутрь loadproof | Show the non-zero value directly in `values-loadproof.yaml`, where sustained load already exists. | |

**User's choice:** Оверлеи не трогаем + отдельный пример
**Notes:** The third option was specifically rejected because the load-proof gate exists to prove
scale-from-zero — a non-zero floor removes the very zero it tests, so the gate would stop proving
what it was built for.

---

## Взаимодействие с fallback (D-05)

Framing question: an operator sets `minReplicaCount: 3`, then Prometheus becomes unreachable and
the scaler errors. What should happen?

| Option | Description | Selected |
|--------|-------------|----------|
| fallback следует за полом | `replicas` becomes `max(1, minReplicaCount)` via the same helper. The floor the operator paid for holds during a scaler failure — precisely when cold start hurts most. At default `0` the render is byte-identical to today. | ✓ |
| Оставить 1 | `fallback.replicas` stays hardcoded at 1 across all 7 classes. Minimal diff, strictly inside CAPA-01's letter — but a class with floor 3 drops to 1 on scaler error. | |
| Отдельный knob | New `keda.<class>.fallbackReplicas` field defaulting to 1. Maximum operator control, but 7 more fields beyond CAPA-01, and it still permits `fallback=1` with `min=3` — the same bug, now opt-in. | |

**User's choice:** fallback следует за полом
**Notes:** Judged in scope rather than creep: introducing an operator-settable floor while
leaving `fallback.replicas` hardcoded creates a configuration where the two directly contradict
each other. At the `0` default the rendered output is unchanged, so SC1's "zero behavior change"
still holds.

---

## Как доказываем SC3 (D-06)

| Option | Description | Selected |
|--------|-------------|----------|
| CI-джоб helm | New job in `ci.yml`: `helm lint`, `helm template` for default and for the override, a diff asserting exactly one ScaledObject changed, plus a negative case proving the D-02 validation fires. No cluster, runs in seconds. | ✓ |
| Ручные команды в плане | Same commands as `acceptance_criteria` in PLAN.md — executor runs them once and records output. Nothing added to CI, no regression protection. | |
| Golden-output файлы | Commit reference `helm template` output and diff against it. Maximum diff precision, but large YAML files needing an update on every chart change. | |

**User's choice:** CI-джоб helm
**Notes:** Surfaced during scouting that the chart currently has **no** CI coverage of any kind —
no `helm lint`, no `helm template` anywhere in `.github/workflows/`. So this job protects future
chart edits generally, not just this phase. The negative case was called out as load-bearing:
without it, a validation block that never fires would pass every positive check.

---

## Claude's Discretion

- Exact helper naming; whether D-02's validation and D-05's `max(1, …)` live in one `define` or two.
- Exact `fail` message wording, beyond naming the class and both values.
- Which class `values-capacity-floor.yaml` demonstrates, and the non-zero value used.
- CI job placement/naming in `ci.yml`, and whether it runs on all pushes or only on `deploy/chart/**` changes.
- Comment style and depth in the modified templates.

## Deferred Ideas

- Guidance on *when* an operator should raise a floor — an operational judgment informed by
  Phase 53's `GET /v1/system/usage`, and a docs concern rather than a chart change.
- Interaction between a non-zero floor and `cooldownPeriod` / `scaleDownStabilizationSeconds` —
  speculative until a floor is actually in production use.
- Extending the new CI helm job to render all three overlays — clear value, but widens the phase
  from CAPA-01 into general chart-CI work.

## Todos reviewed and not folded

The todo matcher scored two Phase 53 items at 0.6 against this phase:
`2026-08-05-per-row-enforced-flag.md` and `2026-08-05-usage-row-empty-enforcement-mode.md`.
Both matched purely on keyword overlap ("add", "per", "operator", "review"). Both are Go changes
to `internal/api/system_usage_handlers.go`; this phase touches no Go code. Not folded.
