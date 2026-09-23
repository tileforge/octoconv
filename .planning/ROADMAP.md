# Roadmap: OctoConv

## Milestones

- ◆ **v1.15 Engine Confinement** — Phases 67–70 (в работе, открыт 2026-09-23)
- ✅ **v1.0–v1.12** — Phases 1–55 (archived; see `.planning/milestones/`)
- ✅ **v1.13 Inspect & Operations** — Phases 56–61 (shipped 2026-09-02)
- ✅ **v1.14 Typesetting & Debt** — Phases 62–64 (shipped 2026-09-05)

## Phases

### v1.15 Engine Confinement (67–70)

Цель милстоуна: «движок читает только то, что ему дали» перестаёт быть
свойством одного класса из девяти.

Фазы сгруппированы **по решениям, которые они разделяют**, а не по движкам:
четыре прямолинейных класса идут одной фазой, потому что новых решений в них
нет; `html` и `probe` — вместе, потому что обоим нужен `/tmp` на запись и это
одно решение; `document` — отдельно, потому что требует другого железа.

| # | Фаза | Требования | Состояние |
|---|---|---|---|
| 67 | `image` — образец и общая часть | CONF-01..05, DEBT-15 | ◆ в работе, PR #28 |
| 68 | `pandoc`, `ocr`, `av`, `audio` | CONF-01, 02, 04, 05 | ○ |
| 69 | `html`, `probe` — и решение по `/tmp` | CONF-01, 02, 04, 05, 06 | ○ |
| 70 | `document` — на amd64-узле | CONF-01, 02, 04, 05, 07 | ○ |

**Phase 67 — `image`: образец и общая часть.** Уже в работе как PR #28, начата
до объявления милстоуна и записана сюда как его первая фаза, а не как сирота.
Выносит `sandbox.ResolveHelper` и `engineSandbox.wrapEngine` в общее место,
подключает libvips по замеренному набору, пробрасывает аварийный выход в compose
и чарт.

Success criteria:
1. Боевой бинарник в режиме хелпера конвертирует внутри каталога задачи и
   получает отказ на файле снаружи — проверено в собранном образе
2. Воркер отказывается стартовать без Landlock и называет переменную
3. Переменная задаётся из `.env` и из `worker.extraEnv`
4. Общая часть не скопирована в tectonic, а вынесена из него

**Phase 68 — четыре прямолинейных класса.** `pandoc` (нужен только свой
статический бинарник), `ocr` (два бинарника + `tessdata`), `av` (ffmpeg +
ffprobe), `audio` (whisper + ffmpeg по ДРУГОМУ пути, чем в av-образе). Наборы
замерены, новых архитектурных решений нет.

Success criteria:
1. Каждый из четырёх конвертирует под ruleset и получает отказ на файле извне
2. Наборы путей совпадают с `SANDBOX-PATHS.md` и удерживаются тестом
3. Каждый воркер отказывается стартовать без Landlock

**Phase 69 — `html` и `probe`, и решение по `/tmp`.** Chromium кладёт
разделяемую память в `/tmp`, и `TMPDIR` её не уводит — замерено; ghostscript
пишет туда свои временные. Общий `/tmp` означает, что параллельные задачи видят
файлы друг друга. Фаза начинается с этого решения, а не с кода.

Success criteria:
1. Решение по `/tmp` принято и записано с причиной
2. Оба класса конвертируют под ruleset; действенность проверена **по
   содержимому** результата, а не по факту создания файла
3. Chromium получает и обёртку, и интерпретатор, и настоящий бинарник

**Phase 70 — `document`.** Единственный, чей образ закреплён на `linux/amd64`
(ради veraPDF, публикуемого amd64-only). Эмулируемому amd64 Landlock недоступен
— замерено 2026-09-19 на реальных образах. Замер путей и живой гейт выполняются
на amd64-узле.

Success criteria:
1. Набор путей для LibreOffice замерен на amd64 тем же инструментом
2. Конверсия проходит под ruleset, файл извне недоступен
3. Воркер отказывается стартовать без Landlock — включая случай, когда причина
   в эмуляции, а не в ядре

Phase detail for shipped milestones lives in the collapsed sections below and in
`.planning/milestones/`.

## Progress

**v1.15 Engine Confinement** открыт 2026-09-23. Фаза 67 в работе (PR #28),
фазы 68–70 не начаты.

Замеры, на которых стоит весь милстоун, уже сделаны:
`.planning/research/SANDBOX-PATHS.md` содержит наборы путей для всех семи
нативных классов с доказательствами достаточности и действенности.

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
