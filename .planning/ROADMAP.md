# Roadmap: OctoConv

## Milestones

- ✅ **v1.0–v1.12** — Phases 1–55 (archived; see `.planning/milestones/`)
- ✅ **v1.13 Inspect & Operations** — Phases 56–61 (shipped 2026-09-02)
- ✅ **v1.14 Typesetting & Debt** — Phases 62–64 (shipped 2026-09-05)
- ✅ **v1.15 Engine Confinement** — Phases 67–70 (shipped 2026-10-01)

## Phases

Следующий милстоун не открыт — `/gsd-new-milestone`.

Phase detail for shipped milestones lives in the collapsed sections below and in
`.planning/milestones/`.

## Progress

**v1.15 Engine Confinement** закрыт 2026-10-01: все восемь классов движков
работают под замеренным Landlock-ruleset; живой гейт восьми классов —
`ALL PASS (40 checks)`. Открытые follow-up — в `.planning/todos/pending/`.

<details>
<summary>✅ v1.15 Engine Confinement (Phases 67–70) — SHIPPED 2026-10-01</summary>

- [x] Phase 67: Image Confinement & Shared Sandbox — 2026-09-23, PR #28
- [x] Phase 68: Four Straightforward Engine Classes (6/6 plans) — 2026-09-24
- [x] Phase 69: HTML, Probe & the /tmp Decision (5/5 plans) — 2026-09-29
- [x] Phase 70: Document on amd64 (9/9 plans, incl. 4 gap closure) — 2026-10-01

Full detail: `.planning/milestones/v1.15-ROADMAP.md` · phases archived to
`.planning/milestones/v1.15-phases/`

</details>

### Шипнуто вне милстоуна (2026-09-19)

Две фазы выполнены и влиты, когда ни один милстоун не был открыт. Записаны
здесь как есть: придумывать им милстоун задним числом значило бы подогнать
историю под форму документа.

| Фаза | Что | PR | Влито |
|---|---|---|---|
| 65 | CPU-бюджет движка TeX (`TECTONIC_ENGINE_CPU_SECONDS`, `RLIMIT_CPU`) — граница на то, чего документ СТОИТ, а не сколько он занимает часы | [#22](https://github.com/paderinandrey/octoconv/pull/22) `5579e59` | 2026-09-19 |
| 66 | Observability: `job_id` сквозь логи, `Correlation-Id` от вызывающего, запись, соединяющая request id с job_id (OBS-01..04) | [#23](https://github.com/paderinandrey/octoconv/pull/23) `b95b38d` | 2026-09-19 |

У фазы 66 планировочные артефакты лежат в
`.planning/phases/66-observability-correlation/`. **У фазы 65 их нет вовсе** —
ни CONTEXT, ни PLAN, ни SUMMARY: работа шла по ветке `phase-65-cpu-budget` без
них. Это записано как факт, а не замолчано: единственный след решений фазы —
комментарии в коде, замеры в `.env.example` и история ревью PR #22.

Тем же днём влит [#24](https://github.com/paderinandrey/octoconv/pull/24)
(`ddc150c`) — переписанный README и контрактные тесты, держащие
`docs/openapi.yaml` равным маршрутам живого роутера.

<details>
<summary>✅ v1.14 Typesetting &amp; Debt (Phases 62–64) — SHIPPED 2026-09-05</summary>

- [x] Phase 62: Operational Debt Tail (1/1 plans) — 2026-09-02, PR #13
- [x] Phase 63: Sanitize Hardening & Probe Page Ceiling (2/2 plans) — 2026-09-03, PRs #14, #15
- [x] Phase 64: Tectonic — Ninth Engine Class (5/5 plans) — 2026-09-05, PR #16

Full detail: `.planning/milestones/v1.14-ROADMAP.md` · phases archived to
`.planning/milestones/v1.14-phases/`

</details>

<details>
<summary>✅ v1.13 Inspect &amp; Operations (Phases 56–61) — SHIPPED 2026-09-02</summary>

- [x] Phase 56: JobRunner Consolidation & Structured Error Codes (7/7 plans) — 2026-08-08
- [x] Phase 57: Operation Dimension & Output Roles (7/7 plans) — 2026-08-09
- [x] Phase 58: Synchronous Inspection (8/8 plans) — 2026-08-18
- [x] Phase 59: Born-Digital PDF Routing (7/7 plans) — 2026-08-30
- [x] Phase 60: Sanitize — Eighth Engine Class (6/6 plans) — 2026-09-02
- [x] Phase 61: v1.12 Debt Tail (3/3 plans) — 2026-08-30

Full detail: `.planning/milestones/v1.13-ROADMAP.md` · phases archived to
`.planning/milestones/v1.13-phases/`

</details>

<details>
<summary>✅ v1.0 – v1.12 (Phases 1–55) — archived</summary>

Each milestone's full phase detail and requirements live in
`.planning/milestones/v{X.Y}-ROADMAP.md` and `-REQUIREMENTS.md`.

</details>

## Carried forward, not in this milestone

- **SEED-018 job pipelines** — `md → pdf` через pandoc→tectonic одной джобой.
  Первая джоба, пересекающая границу движка.
- 15 спящих семян в `.planning/seeds/`, включая другие engine-классы
  (CAD, archive, preview, tabular), чьи триггеры формально сработали на
  «милстоун добавляет новый класс» и которые сознательно не взяты.

## Backlog

### Phase 999.1: Per-Deployment Worker Selection (BACKLOG)

**Goal:** Каждая установка поднимает только нужный ей набор engine-классов, а API
честно отказывает в классах, которых в установке нет. Сейчас в чарте ни у
одного воркера нет `enabled` — любая установка поднимает все девять.

Решено вместо выноса воркеров в отдельные репозитории (обсуждено 2026-09-23):
воркеры и так собираются раздельно (свой `cmd/*-worker`, Dockerfile,
deployment), а общий контракт (`jobs.Repo` transitions, миграции, asynq payload,
S3-ключи, коды ошибок, `Converter`, `sandbox`) живёт в `internal/` и при
раздельных репо потребовал бы версионируемого SDK и дал бы version skew в
рантайме. Пересмотреть, если движками начнут владеть разные команды или
разойдутся релизные циклы.

Состав:

1. `<name>Worker.enabled` в `deploy/chart/octoconv/values.yaml` для каждого
   воркера (включая KEDA `scaledobject-*`) + готовые наборы values; `profiles:`
   в `docker-compose.yml`
2. **Главное:** API отказывает (явный 4xx с кодом ошибки) на пару форматов,
   чей engine-класс не развёрнут, вместо вечного `queued`. Сейчас валидация
   идёт по `convert.Default`, где зарегистрированы все классы. Источник правды —
   один список включённых классов (напр. `ENABLED_ENGINES`), выводимый из тех же
   values, чтобы чарт и API не могли разойтись. `/formats`, inspect и MCP
   отдают только включённые классы
3. Опционально: CI собирает/тестирует только затронутые воркеры по путям

**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)
