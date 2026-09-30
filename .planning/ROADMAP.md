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
| 67 | `image` — образец и общая часть | CONF-01..05, DEBT-15 | ✓ влита 2026-09-23, PR #28 `eb28f67` |
| 68 | `pandoc`, `ocr`, `av`, `audio` | CONF-01, 02, 04, 05 | ○ следующая |
| 69 | `html`, `probe` — и решение по `/tmp` | CONF-01, 02, 04, 05, 06 | ○ |
| 70 | `document` — на amd64-узле | CONF-01, 02, 04, 05, 07 | ○ |

- [x] Phase 67: Image Confinement & Shared Sandbox — 2026-09-23, PR #28
- [x] Phase 68: Four Straightforward Engine Classes (completed 2026-09-24)
- [x] Phase 69: HTML, Probe & the /tmp Decision (completed 2026-09-29)
- [ ] Phase 70: Document on amd64

### Phase 67: Image Confinement & Shared Sandbox

**Goal**: `image` — образец и общая часть. Начата как PR #28 до объявления
милстоуна и записана сюда как его первая фаза, а не как сирота. Выносит
`sandbox.ResolveHelper` и `engineSandbox.wrapEngine` в общее место, подключает
libvips по замеренному набору, пробрасывает аварийный выход в compose и чарт.
Влита 2026-09-23 (`eb28f67`); планировочных артефактов в `.planning/phases/`
у неё нет — работа шла по ветке PR #28.
**Depends on**: ничего
**Requirements**: CONF-01, CONF-02, CONF-03, CONF-04, CONF-05, DEBT-15

**Success Criteria** (what must be TRUE):

1. Боевой бинарник в режиме хелпера конвертирует внутри каталога задачи и
   получает отказ на файле снаружи — проверено в собранном образе
2. Воркер отказывается стартовать без Landlock и называет переменную
3. Переменная задаётся из `.env` и из `worker.extraEnv`
4. Общая часть не скопирована в tectonic, а вынесена из него

### Phase 68: Four Straightforward Engine Classes

**Goal**: Четыре прямолинейных класса под Landlock: `pandoc` (нужен только свой
статический бинарник), `ocr` (два бинарника + `tessdata`), `av` (ffmpeg +
ffprobe), `audio` (whisper + ffmpeg по ДРУГОМУ пути, чем в av-образе). Наборы
замерены, новых архитектурных решений нет.
**Depends on**: Phase 67 (общая часть `sandbox.ResolveHelper` / `wrapEngine`)
**Requirements**: CONF-01, CONF-02, CONF-04, CONF-05

**Success Criteria** (what must be TRUE):

1. Каждый из четырёх конвертирует под ruleset и получает отказ на файле извне
2. Наборы путей совпадают с `SANDBOX-PATHS.md` и удерживаются тестом
3. Каждый воркер отказывается стартовать без Landlock

**Plans:** 6/6 plans complete

Plans:

- [x] 68-01-PLAN.md — tracer: `pandoc` под ruleset целиком + общая часть (engineExec, runCommandAs, ErrEngineSandboxUnavailable, preflight-инструменты, `scripts/sandbox-liveproof.sh`)
- [x] 68-02-PLAN.md — перезамер всех четырёх классов по реальным argv задачи (pdfinfo/pdftotext, ffprobe в audio, шрифты), доказательства по содержимому, conf05-блоки и `scripts/conf05-crosscheck.sh`
- [x] 68-03-PLAN.md — `ocr`: шесть вызовов через engineExec, страницы внутри каталога задачи, preflight трёх маршрутов, воркер, extraEnv в чарте
- [x] 68-04-PLAN.md — `av`: семь вызовов, FIFO прогресса внутри каталога задачи, preflight на потоковом перекодировании, воркер, extraEnv
- [x] 68-05-PLAN.md — `audio`: ffmpeg + whisper-cli и ffprobe-гард из воркера через NewHandler, файл модели, preflight, воркер, extraEnv
- [x] 68-06-PLAN.md — класс целиком: AST-тест покрытия exec, сверка наборов, compose для ocr/av/audio, живые доказательства на пересобранных образах

### Phase 69: HTML, Probe & the /tmp Decision

**Goal**: `html` и `probe` под Landlock, и решение по `/tmp`. Chromium кладёт
разделяемую память в `/tmp`, и `TMPDIR` её не уводит — замерено; ghostscript
пишет туда свои временные. Общий `/tmp` означает, что параллельные задачи видят
файлы друг друга. Фаза начинается с этого решения, а не с кода.
**Depends on**: Phase 67
**Requirements**: CONF-01, CONF-02, CONF-04, CONF-05, CONF-06

**Success Criteria** (what must be TRUE):

1. Решение по `/tmp` принято и записано с причиной
2. Оба класса конвертируют под ruleset; действенность проверена **по
   содержимому** результата, а не по факту создания файла
3. Chromium получает и обёртку, и интерпретатор, и настоящий бинарник

**Plans:** 5/5 plans complete

Plans:
**Wave 1**

- [x] 69-01-PLAN.md — сначала решение по `/tmp` в SANDBOX-PATHS.md (D-01…D-05 с замерами), затем tracer `html`: ruleset с общими путями записи (forJobRW), окружение движка по списку, проверка параллельности и очистка `/tmp` перед задачей, preflight по `/Title`, воркер, compose/чарт, живой гейт

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 69-02-PLAN.md — `probe`: шесть вызовов через один engineExec, без `/tmp`, TMPDIR в окружении самого движка, scratch внутри каталога задачи, сохранение sandbox-сбоя до классификатора, preflight четырёх движков по содержимому, раздел probe исправлен по коду (D-10)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 69-03-PLAN.md — класс целиком: e2e для OOXML/изображений через конфайненный воркер, allowlist только фазы 70, сверка шести классов, инверсии, живой гейт 31 проверки на пересобранных образах, статус в SANDBOX-PATHS.md

**Gap closure, wave 1** *(UAT 2026-09-27)*

- [x] 69-04-PLAN.md — G-69-2: CSP вклеивается в начало документа (после ведущего doctype, иначе после BOM или в байт 0), без поиска `<head`/`<html`; CSP запрещает фреймы (сырой `in.html` во фрейме исполнял скрипт); 18 форм — юнит-тестом и живой дифференциальной проверкой в `sandbox-liveproof.sh html`, обе защиты инвертированы

**Gap closure, wave 2** *(blocked on 69-04)*

- [x] 69-05-PLAN.md — G-69-3: большой документ под ruleset при лимитах развёртывания без флага `/dev/shm` (с негативным контролем) в живом гейте; затем из чарта, values, compose и гейта убраны монтирование и размер `/dev/shm`, комментарии ссылаются на SANDBOX-PATHS.md

### Phase 70: Document on amd64

**Goal**: `document` под Landlock — оба движка, LibreOffice и veraPDF. Фаза
начиналась с того, что это единственный класс, чей образ закреплён на
`linux/amd64` (ради `verapdf/cli`, публикуемого amd64-only), а эмулируемому amd64
Landlock недоступен — замерено 2026-09-19 на реальных образах, и эта причина
остаётся в силе. Фаза выяснила, что закреплена была только build-стадия
veraPDF (в `/opt/verapdf` jar-файлы и скрипт, ELF нет), сделала образ нативным и
сняла закрепление лишь после того, как дифференциальный прогон veraPDF
(native против emulated, `scripts/verapdf-differential.sh`) не нашёл расхождений.
Замер путей и живой гейт выполнены на этом arm64-хосте; эмулируемый amd64-образ
отказывается стартовать, а первый amd64-деплой — отдельный todo (D-05).
**Depends on**: Phase 67
**Requirements**: CONF-01, CONF-02, CONF-04, CONF-05, CONF-07

**Success Criteria** (what must be TRUE):

1. Наборы путей для LibreOffice и veraPDF замерены тем же инструментом на нативном образе — после того, как дифференциальный прогон veraPDF снял закрепление amd64
2. Конверсия проходит под ruleset, файл извне недоступен
3. Воркер отказывается стартовать без Landlock — включая случай, когда причина
   в эмуляции, а не в ядре

**Plans:** 9 plans (5 + 4 gap closure)

Plans:
**Wave 1**

- [x] 70-01-PLAN.md — tracer: нативный образ (на amd64 закреплена только стадия veraPDF), повторяемый дифференциальный прогон veraPDF natively vs emulated с реальным PDF/A из soffice, снятие pin в compose только при `DIVERGENCES: 0` (иначе pin остаётся и допустим только маршрут amd64-host), PDFA e2e на нативном образе; спайк по bind'у сокета soffice.bin; checkpoint: как soffice получает право на сокет (или стоп ради amd64-узла, D-03)

**Wave 2** *(blocked on Wave 1 completion; runs only on `DIVERGENCES: 0`)*

- [x] 70-02-PLAN.md — право на сокет по выбранному маршруту в `internal/sandbox`, helper-режим document-worker, драйвер `scripts/document-sandbox-measure.sh` (содержимое, не код возврата), наборы soffice и veraPDF замерены и сужены удалением, маршрут `/tmp` по замеру (D-09), раздел document в SANDBOX-PATHS.md

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 70-03-PLAN.md — tracer `document` в коде: два ruleset на задачу, очищенное окружение движков (TMPDIR, JAVA_OPTS), preflight обоих движков по содержимому, отказ до Postgres, `isDocumentTerminal` с sentinel, строка класса в живом гейте (четыре общие проверки)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 70-04-PLAN.md — tracer: эмулируемый amd64-образ отказывается стартовать до Postgres (D-08), проверки маршрута `/tmp` на живом образе; `DOCUMENT_SANDBOX_DISABLED` в compose/.env/чарте, concurrency по маршруту, бюджет startupProbe

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 70-05-PLAN.md — класс целиком: e2e через конфайненный воркер (шим внутри ruleset), инверсии, сверка восьми классов, живой гейт восьми классов на пересобранных образах; CONF-07 и формулировка фазы (D-04), один todo про amd64-прод (D-05), закрытие todo

**Gap closure — Wave 1** *(70-VERIFICATION.md: gaps 1 и 2; 70-06 и 70-07 не делят файлов)*

- [x] 70-06-PLAN.md — html: замер того, что Chromium, который ставит образ сегодня, делает с разделяемой памятью (`/dev/shm` запрещён, доступен, 64 МиБ), и негативный контроль large-document, который fallback не обходит (наблюдение inotify при ДОСТУПНОМ `/dev/shm`, только в тесте); `sandbox-liveproof.sh html` ALL PASS (9 checks); Chromium не закрепляется
- [x] 70-07-PLAN.md — WR-01: общий `sharedTmpGuard` отказывает при concurrency ниже 1 (document и html, тесты 0 и -1, отказ на собранных образах); IN-01 preflight от signal-контекста во всех воркерах; IN-02 TMPDIR с пробелом на пути PDF/A; IN-03 гигиена differential-скрипта

**Gap closure — Wave 2** *(blocked on Wave 1)*

- [ ] 70-08-PLAN.md — WR-02: сужение `ro:/proc` veraPDF по замеру (проверки N3 и V0L в драйвере, JVM через боевой launcher), либо записанный остаточный риск; честные комментарии про очистку окружения; html и tectonic — запись и один todo

**Gap closure — Wave 3** *(blocked on Wave 2)*

- [ ] 70-09-PLAN.md — e2e через конфайненный воркер и живой гейт восьми классов `ALL PASS (40 checks)` на образах из фиксирующего коммита, сверка conf05, окно в WINDOWS.md закрыто; todo про `sock:`-альтернативу (MAKE_SOCK+REMOVE_FILE) и про образы MinIO

Phase detail for shipped milestones lives in the collapsed sections below and in
`.planning/milestones/`.

## Progress

**v1.15 Engine Confinement** открыт 2026-09-23. Фаза 67 влита 2026-09-23
(PR #28, `eb28f67`); фаза 68 — следующая, 69–70 не начаты.

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
