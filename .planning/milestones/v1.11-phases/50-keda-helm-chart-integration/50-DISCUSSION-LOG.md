# Phase 50: KEDA/Helm Chart Integration - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-01
**Phase:** 50-keda-helm-chart-integration
**Areas discussed:** Профиль KEDA-тюнинга, Потолок и ресурсы, Механика downscale-proof, Live-proof логистика

---

## Профиль KEDA-тюнинга

| Option | Description | Selected |
|--------|-------------|----------|
| null, как document | 65s покрыт дефолтными 300s HPA; falsy-0 гейт ставим, values-loadproof переопределяет для гейта | ✓ |
| Ненулевое, вывести из 65s | ~120s явный контракт — расходится с document-прецедентом | |
| 900, как тяжёлые | Копия соседа — антипаттерн проекта | |

**User's choice:** scaleDownStabilizationSeconds: null (fast-engine профиль)

| Option | Description | Selected |
|--------|-------------|----------|
| 2 / 10 / 90 — из concurrency | threshold = PANDOC_WORKER_CONCURRENCY=2; polling/cooldown из скорости pandoc; совпадает с html численно, но выведено из бюджета pandoc | ✓ |
| 1 / 15 / 120 — document-профиль | Игнорирует concurrency=2 и скорость pandoc | |
| На усмотрение планировщика | Только принцип, числа планировщику | |

**User's choice:** threshold "2", pollingInterval 10, cooldownPeriod 90
**Notes:** WR-01 триада + retry-inclusive PromQL — carried forward verbatim, не обсуждались.

---

## Потолок и ресурсы

| Option | Description | Selected |
|--------|-------------|----------|
| 2 — паритет большинства | 2 пода × concurrency 2 = 4 параллельных конверсии; повышение — measured-evidence твик | ✓ |
| 4 — как image | 8 CPU резерва на класс без данных о нагрузке | |

**User's choice:** maxReplicaCount: 2
**Notes:** Ресурсы 2 CPU/1Gi не обсуждались — зафиксированы условиями измерения 49-03 (Path-B 4 MiB под 1g; не понижать без перезамера). Grace 85s — вывод планировщика из 65s (75+10), зеркалит compose.

---

## Механика downscale-proof

| Option | Description | Selected |
|--------|-------------|----------|
| Loadproof-overlay + max-фикстура | values-loadproof занижает стабилизацию (прецедент document); html→md @≈4MiB (~32s окно); реальный HPA N→N-1 | ✓ |
| Ручной kubectl scale | Не «genuine HPA downscale» из SC4 | |
| На усмотрение планировщика | Только требование, механика исследователю | |

**User's choice:** Loadproof-overlay + max-size фикстура

---

## Live-proof логистика

| Option | Description | Selected |
|--------|-------------|----------|
| Одна k8s-сессия, оба гейта | Один цикл OrbStack up/down: loadproof → downscale-survival → teardown; меньше тогглинга; два evidence-набора | ✓ |
| Два раздельных гейта | Гранулярнее, но дольше и больше рестартов OrbStack | |

**User's choice:** Одна OrbStack k8s-сессия, оба гейта подряд, operator-gated.

## Claude's Discretion

- Точная форма deployment-pandoc-worker.yaml (клон fast-engine соседа, без cgroup/OMP);
- порядок ключей values.yaml; значение gate-override стабилизации в values-loadproof.yaml;
- параметры прогона gate-скриптов (число джобов, каденс поллинга).

## Deferred Ideas

- SEED-011 warm floor (теперь 7 ScaledObject'ов); maxReplicaCount>2 для pandoc (measured-evidence); SEED-010 dynamic registry.
- После фазы: закрытие v1.11 → PR; тегнуть @codex на ревью PR (запрос пользователя mid-session).
