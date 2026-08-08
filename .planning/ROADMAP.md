# Roadmap: OctoConv

## Milestones

- ✅ **v1.0 Hardening MVP** — Phases 1-4 (shipped 2026-07-08) — see `.planning/milestones/v1.0-ROADMAP.md`
- ✅ **v1.1 Tech Debt Cleanup** — Phases 5-7 (shipped 2026-07-08) — see `.planning/milestones/v1.1-ROADMAP.md`
- ✅ **v1.2 Document Engine Class** — Phases 8-11 (shipped 2026-07-10) — see `.planning/milestones/v1.2-ROADMAP.md`
- ✅ **v1.3 Document Class v2** — Phases 12-16 (shipped 2026-07-12) — see `.planning/milestones/v1.3-ROADMAP.md`
- ✅ **v1.4 CI, Presets & Debt Cleanup** — Phases 17-19 (shipped 2026-07-13) — see `.planning/milestones/v1.4-ROADMAP.md`
- ✅ **v1.5 MCP Access & Document Fidelity** — Phases 20-23 (shipped 2026-07-13) — see `.planning/milestones/v1.5-ROADMAP.md`
- ✅ **v1.6 Kubernetes & KEDA** — Phases 24-28 (shipped 2026-07-17) — see `.planning/milestones/v1.6-ROADMAP.md`
- ✅ **v1.7 Audio Engine & Hardening** — Phases 29-33 (shipped 2026-07-18) — see `.planning/milestones/v1.7-ROADMAP.md`
- ✅ **v1.8 AV Engine (video/ffmpeg)** — Phases 34-37 (shipped 2026-07-23) — see `.planning/milestones/v1.8-ROADMAP.md`
- ✅ **v1.9 Job Progress & Control** — Phases 38-42 (shipped 2026-07-25) — see `.planning/milestones/v1.9-ROADMAP.md`
- ✅ **v1.10 OCR Engine (text extraction)** — Phases 43-46 (shipped 2026-07-27) — see `.planning/milestones/v1.10-ROADMAP.md`
- ✅ **v1.11 Pandoc Engine** — Phases 47-50 (shipped 2026-08-02) — see `.planning/milestones/v1.11-ROADMAP.md`
- ✅ **v1.12 Consumer Accounting & Fairness** — Phases 51-55 (shipped 2026-08-07) — see `.planning/milestones/v1.12-ROADMAP.md`
- 🚧 **v1.13 Inspect & Operations** — Phases 56-61 (in progress)

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

<details>
<summary>✅ v1.0 Hardening MVP (Phases 1-4) — SHIPPED 2026-07-08</summary>

- [x] Phase 1: Merge, Auth & Rate Limiting (4/4 plans) — completed 2026-07-04
- [x] Phase 2: Webhook Delivery (3/3 plans) — completed 2026-07-04
- [x] Phase 3: Retry-Safety & Reconciler (3/3 plans) — completed 2026-07-06
- [x] Phase 4: Content Validation, Storage Lifecycle & Observability (5/5 plans) — completed 2026-07-07

Full details: `.planning/milestones/v1.0-ROADMAP.md`

</details>

<details>
<summary>✅ v1.1 Tech Debt Cleanup (Phases 5-7) — SHIPPED 2026-07-08</summary>

- [x] Phase 5: Webhook SSRF Private-IP Opt-Out (1/1 plans) — completed 2026-07-08
- [x] Phase 6: Reconciler Webhook-Gap Sweep & Staleness Soak Test (4/4 plans) — completed 2026-07-08
- [x] Phase 7: Image Dimension Limit (Decompression-Bomb Protection) (2/2 plans) — completed 2026-07-08

Full details: `.planning/milestones/v1.1-ROADMAP.md`

</details>

<details>
<summary>✅ v1.2 Document Engine Class (Phases 8-11) — SHIPPED 2026-07-10</summary>

- [x] Phase 8: Document Content Safety & Format Detection (2/2 plans) — completed 2026-07-09
- [x] Phase 9: LibreOffice Converter Engine (2/2 plans) — completed 2026-07-09
- [x] Phase 10: Document Worker & Reconciler Integration (4/4 plans) — completed 2026-07-09
- [x] Phase 11: API Routing & End-to-End Document Conversion (4/4 plans, incl. gap closure 11-04) — completed 2026-07-10

Full details: `.planning/milestones/v1.2-ROADMAP.md`

</details>

<details>
<summary>✅ v1.3 Document Class v2 (Phases 12-16) — SHIPPED 2026-07-12</summary>

- [x] Phase 12: Tech Debt Cleanup (1/1 plans) — completed 2026-07-10
- [x] Phase 13: Cross-Format Conversion & Input Safety (3/3 plans) — completed 2026-07-10
- [x] Phase 14: Validated Conversion Options & PDF/A Export (3/3 plans) — completed 2026-07-10
- [x] Phase 15: HTML→PDF Chromium Engine (5/5 plans) — completed 2026-07-11
- [x] Phase 16: Webhook Delivery Decoupling (5/5 plans, incl. gap closure 16-05) — completed 2026-07-12

Full details: `.planning/milestones/v1.3-ROADMAP.md`

</details>

<details>
<summary>✅ v1.4 CI, Presets & Debt Cleanup (Phases 17-19) — SHIPPED 2026-07-13</summary>

- [x] Phase 17: Tech Debt Cleanup (2/2 plans) — completed 2026-07-12
- [x] Phase 18: Presets (4/4 plans) — completed 2026-07-12
- [x] Phase 19: CI Pipeline (2/2 plans) — completed 2026-07-13

Full details: `.planning/milestones/v1.4-ROADMAP.md`

</details>

<details>
<summary>✅ v1.5 MCP Access & Document Fidelity (Phases 20-23) — SHIPPED 2026-07-13</summary>

- [x] Phase 20: Presets REST CRUD & Format Discovery (2/2 plans) — completed 2026-07-13
- [x] Phase 21: MCP Server (3/3 plans) — completed 2026-07-13
- [x] Phase 22: CFB Encrypted-vs-Legacy Classification (2/2 plans) — completed 2026-07-13
- [x] Phase 23: veraPDF ISO 19005 Validation (3/3 plans) — completed 2026-07-13

Full details: `.planning/milestones/v1.5-ROADMAP.md`

</details>

<details>
<summary>✅ v1.6 Kubernetes & KEDA (Phases 24-28) — SHIPPED 2026-07-17</summary>

- [x] Phase 24: Helm Chart Core & Landmine Closure (3/3 plans) — completed 2026-07-14
- [x] Phase 25: MCP Streamable HTTP (3/3 plans) — completed 2026-07-14
- [x] Phase 26: Operator System-Presets REST (2/2 plans) — completed 2026-07-14
- [x] Phase 27: KEDA Autoscaling (3/3 plans) — completed 2026-07-16
- [x] Phase 28: Autoscale Load-Proof (3/3 plans) — completed 2026-07-17

Full details: `.planning/milestones/v1.6-ROADMAP.md`

</details>

<details>
<summary>✅ v1.7 Audio Engine & Hardening (Phases 29-33) — SHIPPED 2026-07-18</summary>

- [x] Phase 29: v1.6 Hardening Tail (3/3 plans) — completed 2026-07-17
- [x] Phase 30: Audio Engine Foundation (3/3 plans) — completed 2026-07-18
- [x] Phase 31: Queue, Worker & Routing Integration (4/4 plans) — completed 2026-07-18
- [x] Phase 32: Containerization & Local E2E + RTF Gate (5/5 plans) — completed 2026-07-18
- [x] Phase 33: KEDA/Helm Chart Integration (3/3 plans) — completed 2026-07-18

Full details: `.planning/milestones/v1.7-ROADMAP.md`

</details>

<details>
<summary>✅ v1.8 AV Engine (video/ffmpeg) (Phases 34-37) — SHIPPED 2026-07-23</summary>

- [x] Phase 34: AV Engine Foundation (3/3 plans) — completed 2026-07-20
- [x] Phase 35: Queue, Worker & Routing Integration (7/7 plans) — completed 2026-07-22
- [x] Phase 36: Containerization & RTF-Measured Timeout (5/5 plans) — completed 2026-07-23
- [x] Phase 37: KEDA/Helm Chart Integration (3/3 plans) — completed 2026-07-23

Full details: `.planning/milestones/v1.8-ROADMAP.md`

</details>

<details>
<summary>✅ v1.9 Job Progress & Control (Phases 38-42) — SHIPPED 2026-07-25</summary>

**Milestone Goal:** Сделать длинные конвертации прозрачными и управляемыми — клиент видит прогресс/ETA в реальном времени, может листать свои джобы и отменять их.

- [x] Phase 38: Schema Foundation (1/1 plans) — completed 2026-07-24
- [x] Phase 39: Cancellation (end-to-end) (4/4 plans) — completed 2026-07-24
- [x] Phase 40: Job Listing (2/2 plans) — completed 2026-07-25
- [x] Phase 41: Progress Core (4/4 plans) — completed 2026-07-25
- [x] Phase 42: Live Push (4/4 plans) — completed 2026-07-25

Full details: `.planning/milestones/v1.9-ROADMAP.md`

</details>

<details>
<summary>✅ v1.10 OCR Engine (text extraction) (Phases 43-46) — SHIPPED 2026-07-27</summary>

**Milestone Goal:** Клиент может извлечь машиночитаемый текст из изображений и сканированных PDF через тот же async-контур, что и остальные движки — offline, детерминированно, с онлайн-прогрессом. Шестой engine-класс.

- [x] Phase 43: OCR Engine Foundation - Standalone `OCRConverter` extracts txt/hOCR from images and scanned PDFs, streaming page-by-page — memory-bomb-safe and born-digital-PDF-safe from the first line, before any queue/worker plumbing (completed 2026-07-26)
- [x] Phase 44: Async Pipeline Wiring - OCR jobs flow end-to-end through the async pipeline (queue, `cmd/ocr-worker`, retry, reconciler, API routing) over the completely unmodified v1.9 progress/SSE/webhook machinery (completed 2026-07-26)
- [x] Phase 45: Containerization + Measured Timeout Gate - A running `ocr-worker` container passes a live E2E, with `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` sized from a measured seconds-per-page gate rather than a copied constant (completed 2026-07-27)
- [x] Phase 46: KEDA/Helm Chart Integration - The OCR class autoscales in the cluster with production parity to the other five engine classes, scale-from-zero live-proven (completed 2026-07-27)

Full details: `.planning/milestones/v1.10-ROADMAP.md`

</details>

<details>
<summary>✅ v1.11 Pandoc Engine (Phases 47-50) — SHIPPED 2026-08-02</summary>

**Milestone Goal:** Седьмой engine-класс — pandoc markup/document interconvert (md ↔ rst ↔ org ↔ latex ↔ epub ↔ html ↔ docx), только markup↔markup (без →pdf), через тот же async-контур, что и остальные шесть движков.

- [x] Phase 47: Pandoc Engine Foundation (3/3 plans) — completed 2026-07-30
- [x] Phase 48: Async Pipeline Wiring (3/3 plans) — completed 2026-07-30
- [x] Phase 49: Containerization + Measured Timeout Gate (4/4 plans) — completed 2026-08-01
- [x] Phase 50: KEDA/Helm Chart Integration (4/4 plans) — completed 2026-08-01

Full details: `.planning/milestones/v1.11-ROADMAP.md`

</details>

<details>
<summary>✅ v1.12 Consumer Accounting & Fairness (Phases 51-55) — SHIPPED 2026-08-07</summary>

**Milestone Goal:** превратить OctoConv из «сервиса, который умеет конвертировать» в общую платформу, которая знает своих потребителей — кто сколько потребляет, в каких границах, и как не дать одному потребителю выжрать класс движка целиком.

- [x] Phase 51: Attribution, Accounting & Retention (5/5 plans) — completed 2026-08-02
- [x] Phase 52: Quota Enforcement & Intra-Class Fairness (9/9 plans) — completed 2026-08-03
- [x] Phase 53: Operator Usage Visibility (7/7 plans) — completed 2026-08-05
- [x] Phase 54: Per-Class Capacity Floor (SEED-011) (2/2 plans) — completed 2026-08-06
- [x] Phase 55: Live Gate — Multi-Replica Quota Correctness (7/7 plans) — completed 2026-08-07

Full details: `.planning/milestones/v1.12-ROADMAP.md`

</details>

## 🚧 v1.13 Inspect & Operations (Phases 56-61)

**Milestone Goal:** превратить OctoConv из конвертера форматов в processing API — ввести измерение «операция» с двумя первыми не-конвертерными возможностями (`inspect`, `sanitize`) и убрать ×7 тариф, который сегодня платит каждая сквозная забота, проходящая через семь копий одного хендлера.

**Coverage:** 28/28 v1 requirements mapped, 0 orphans.

### Phase 56: JobRunner Consolidation & Structured Error Codes

**Goal**: Все семь конверсионных хендлеров исполняются одним дескриптор-управляемым code-path, терминальный исход эмитится ровно одним вызовом в ровно одном месте и структурно не может выстрелить до коммита транзакции, а отказы становятся машиночитаемым контрактом, выпущенным один раз и потреблённым REST'ом, обоими MCP и фронтендом.
**Depends on**: Nothing (первая фаза v1.13; строится на стабильной платформе v1.0–v1.12)
**Requirements**: RUNR-01, RUNR-02, RUNR-03, ERRC-01, ERRC-02, ERRC-03
**Success Criteria** (what must be TRUE):

  1. Ни один из семи конверсионных классов больше не имеет собственного хендлера: все проходят через `Runner` поверх `EngineDescriptor`, где таймаут, лимит входа, opts-схема, retry-классификация, unique-lock TTL и progress-репортинг — поля данных, а не ветвления. `HandleWebhookDeliver` байт-неизменен (иная retry/dead-letter семантика, сознательно вне скоупа).
  2. Каждый из семи классов ведёт себя в точности как до рефакторинга. В частности сохранены **противоположные по полярности** стадийные асимметрии `isAudioTerminal` и `isAVTerminal` — дескриптор моделирует `(стадия, таймаут)`, а не `(движок, таймаут)`; закреплено пиннинг-тестом на каждый класс, падающим громко на любой будущий дифф.
  3. Набор меток `octoconv_job_duration_seconds` остался ровно `{engine, status}`, а `octoconv_queue_depth` — ровно `{queue, state}`; доказано существующими пиннинг-тестами плюс диффом, поэтому KEDA scale-from-zero всех семи классов заведомо не тронут. Соблазн «раз уж мы здесь, добавим метку `operation`» — главный риск взрывного радиуса в этой фазе.
  4. Терминальный исход задачи эмитится ровно одним вызовом в ровно одном месте, и выстрелить им до коммита охраняемого перехода структурно невозможно — доказано инверсионным прогоном (перенос эмиссии раньше коммита ломает тест). Той же дисциплиной закрыт живой дефект `internal/reconciler/reconciler.go:311-312`, где `RecordReconcilerAction("exhausted")` инкрементится безусловно — тот же класс, который ревью PR #4 нашло в семи местах и здесь не заметило.
  5. Каждый терминальный отказ джобы и каждый клиентский 4xx несёт стабильный код из закрытого версионированного перечня; референсный фронтенд и оба MCP-сервера выбирают поведение по коду, и тест доказывает, что ни один потребитель не матчит текст сообщения. Терминальные отказы `invalid_options` появились в обоих счётчиках исходов.

**Note on scope**: весь контракт кодов ошибок вводится ЗДЕСЬ и один раз — включая 4xx-половину в `internal/api/handlers.go`. Дробление его между фазами заставило бы двух MCP-потребителей и фронтенд мигрировать дважды внутри одного милстоуна (PITFALLS.md Pitfall 11).

**Note on cost (research correction)**: SEED-020 переоценил собственную цену. Тела хендлеров — 79–110 строк (86–127 с доккоментами), а не 94–163, и `process()` (`worker.go:1847-1942`) **уже** является общей функцией оркестрации, поэтому ×7 тариф живёт целиком в обёрточном бойлерплейте. «Disk guards у av / page caps у ocr», описанные как различия уровня хендлера, оказались внутренностями конвертеров. Счёт «14 симметричных call sites» подтверждён точно.

**Plans:** 7/7 plans complete

Plans:
**Wave 1**

- [x] 56-01-PLAN.md — Worker test seams + seven-handler pinning suite (D-10 step 1, frozen)
- [x] 56-02-PLAN.md — internal/errcode closed sync vocabulary + auth/ratelimit adoption

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 56-03-PLAN.md — internal/api 4xx code sweep (112 call sites) + envelope proofs
- [x] 56-04-PLAN.md — Runner + EngineDescriptor consolidation, seven handlers deleted, ERRC-02 closed

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 56-05-PLAN.md — Inversion run + metric-label zero-diff proof + reconciler RUNR-02 guard fix
- [x] 56-06-PLAN.md — docs/ERROR-CODES.md freeze point + bidirectional Go-to-doc sync test

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 56-07-PLAN.md — Frontend + both MCP servers migrate to codes; ERRC-03 no-text-matching proof

**Autonomous**: true

### Phase 57: Operation Dimension & Output Roles

**Goal**: У джобы появляется измерение «операция» (`convert` по умолчанию, абсолютная обратная совместимость), реестр разрешает движок по тройке `(operation, from, to)`, а у выходов появляется роль — так что отчёт больше никогда не может быть отдан вместо основного результата.
**Depends on**: Phase 56 (`process()` начинает читать `job.Operation` — делать это до консолидации значило бы править семь хендлеров, а потом их схлопывать)
**Requirements**: OPRN-01, OPRN-02, OPRN-03, ROLE-01, ROLE-02
**Success Criteria** (what must be TRUE):

  1. Клиент, не приславший `operation`, не наблюдает НИ ОДНОГО изменения: та же форма запроса, тот же 202, то же тело `GET /v1/jobs/{id}` и байт-идентичный ключ `engines` в `GET /v1/formats` — доказано пиннинг-тестом против до-фазового вывода, а не рассуждением.
  2. Клиент может прислать `operation` явно; реестр разрешает тройку `(operation, from, to)`, и пара с совпадающими форматами (`pdf→pdf`), зарегистрированная для не-convert операции, не может быть выбрана для `convert` — доказано тестом на дизъюнктность в духе pandoc/chromium-гарда v1.11.
  3. Неподдерживаемая тройка `(operation, from, to)` отклоняется 422 с машиночитаемым кодом до того, как хоть один байт попал в S3 и до создания строки джобы — fail-closed, как сегодняшняя проверка пары.
  4. `job_outputs` несёт роль, существующие строки корректны без backfill, и `GET /v1/jobs/{id}` возвращает КАЖДЫЙ выход с его ролью — при этом ответ одновыходной convert-джобы остаётся байт-идентичным. Закрывает живой дефект `internal/api/handlers.go:857-870`, где `outs[0]` презайнится без учёта роли.

**Constraint — ROLE-02 обязан закрыться ЗДЕСЬ, а не параллельно с Phase 60.** Сегодня `handleGetJob` берёт `outs[0]` безусловно. В момент, когда sanitize отдаст второй выход с `role=report`, отчёт может быть выдан как основной результат или основной результат может быть скрыт. Это не гипотетика — дефект уже в коде, просто пока недостижим.

**Decision locked (2026-08-07)**: `sanitize` вводится как НОВОЕ значение `jobs.operation` (миграция расширяет CHECK, который сегодня разрешает `convert|extract|archive|inspect|render`), движок — `probe`, зарезервированный в схеме с `0001_init.sql`. Переиспользование `extract` отвергнуто: sanitize ничего не извлекает, и `extract` понадобится по назначению в следующем милстоуне. Решение расходится с формулировкой SEED-013 (`engine=probe`, `operation=inspect|convert`) — семя фиксирует старую модель, эта запись её замещает.

**Autonomous**: true

### Phase 58: Synchronous Inspection

**Goal**: Клиент может узнать правду о файле до того, как поставит дорогую джобу — реальный формат, характеристики, риски и доступные операции с проверкой квоты — одним синхронным запросом, чьи границы доказаны живым гейтом, а не допущением.
**Depends on**: Phase 57 (перечисление доступных `(operation, target)` в отчёте осмысленно только после введения измерения операции)
**Requirements**: INSP-01, INSP-02, INSP-03, INSP-04, INSP-05, INSP-06
**Success Criteria** (what must be TRUE):

  1. Клиент может отправить файл в `POST /v1/inspect` и синхронно получить определённый по magic bytes формат (никогда по расширению) и только те характеристики, которые применимы именно к этому формату — а не одну плоскую схему с нулями во всех неподходящих полях.
  2. Находки возвращаются типизированным списком из закрытого перечня — код, уровень серьёзности и, где осмысленно, местоположение: макросы OOXML, PDF JavaScript и action-словари, вложенные файлы, внешние ссылки, шифрование, форма decompression bomb, класс OLE-CFB.
  3. Отчёт никогда не переоценивает свою полноту: каждая названная проверка несёт clean / not_applicable / unknown, а отчёт под потолком работы явно сообщает охват — усечённый анализ не может выглядеть как полноуверенное «чисто». Сегодняшний код бинарный (reject/pass), трёхзначного состояния в нём нет нигде.
  4. Ответ перечисляет доступные для этого файла `(operation, target)` с обслуживающим движком, оценкой длительности и живой проверкой квоты — клиент узнаёт «пройдёт ли и сколько займёт» за один round-trip.
  5. Путь ограничен независимо от v1.12 admission-гейта, который он минует по построению: потолок работы, потолок тела, собственный rate-бюджет, отказ машиночитаемым кодом. Результат inspect не даёт права пропустить fail-closed валидацию при создании джобы — доказано тестом, который инспектирует один файл и создаёт джобу с другими байтами (TOCTOU).
  6. **Живой гейт**: операторский залп патологических inspect-запросов против живого API оставляет `/healthz` отвечающим в пределах liveness-порога весь прогон, evidence закоммичен и перепроверяем. Статическим рассуждением этот критерий не закрывается.

**Why the gate lives inside this phase, not as its own** (в отличие от QUOT-05/Phase 55): гейт проверяет свойство кода, который пишется в этой же фазе, и между ними нет фазы, способной это свойство изменить. Прецедент — Phase 51 Plan 05 (операторский KEDA-гейт внутри фазы, которая построила метрики). Если гейт упадёт, починка — вероятно перенос глубокого анализа в воркер, то есть изменение дизайна: фаза и не должна закрываться до того, как дизайн доказан.

**Risk note**: это первый в проекте request-path, разбирающий недоверенные байты **инлайн**, в том же процессе, что обслуживает `/healthz`, в сервисе, вся модель безопасности которого построена на воркер-изоляции с измеренными таймаутами. Патологический залп валит liveness-пробу и каскадит рестарт всего флота, а не тормозит одного клиента.

**Autonomous**: false (SC6 — операторский живой гейт)

### Phase 59: Born-Digital PDF Routing

**Goal**: Born-digital PDF перестаёт быть тупиком: inspect становится маршрутизатором вместо слепого прогона через Tesseract на 744-секундном таймауте или безусловного отказа.
**Depends on**: Phase 58 (маршрутизация читает отчёт inspect — без него решать нечем)
**Requirements**: BDPD-01, BDPD-02
**Success Criteria** (what must be TRUE):

  1. Отчёт inspect сообщает, содержит ли PDF пригодный текстовый слой, с той же честностью о выборочности, что и любое другое утверждение отчёта.
  2. Клиент с born-digital PDF получает пригодный маршрут вместо слепого прогона или отказа, и комментарий `internal/convert/ocrguards.go:227-228` больше не советует document-движок, который pdf на вход не принимает вовсе (проверено: `pdf` — source-формат ровно в одном месте, `ocr.go:34`).

**Scope fence**: берётся только SEED-021 Opportunity 1 (маршрутизация). Opportunity 2 — постраничная классификация Mixed-PDF — отложена: семя само фиксирует нерешённый merging-контракт (порядок страниц, недублирование, асимметрия Markdown vs hOCR/ALTO). Выбор инструмента извлечения текста НЕ предрешён этой фазой; `firecrawl/pdf-inspector` (0.1.x, без готовых dual-arch сборок, ноль пересечения с risk-сигналами) остаётся спайком, не фазой.

**Autonomous**: true

### Phase 60: Sanitize — Eighth Engine Class

**Goal**: Клиент может получить безопасную копию документа — без макросов, JavaScript, вложений, внешних ссылок и метаданных — с отчётом о том, что было найдено и вырезано, и с доказанной, а не заявленной чистотой результата.
**Depends on**: Phase 56 (дескриптор-паттерн — probe становится ПЕРВЫМ классом, добавленным правильно, а не восьмой ручной копией) и Phase 57 (измерение операции + роль выхода для отчёта)
**Requirements**: SANI-01, SANI-02, SANI-03, SANI-04, SANI-05, SANI-06
**Success Criteria** (what must be TRUE):

  1. Клиент может санировать PDF и получить тот же документ без JavaScript, action-словарей (`/OpenAction`, `/AA`), launch-действий, вложенных файлов и внешних URI-ссылок; и санировать OOXML — без VBA-макросов **включая p-код**, а не только исходник (защита от VBA stomping — это базовая ставка, а не продвинутая), и без встроенных OLE-объектов, с консистентными `[Content_Types].xml` и relationship-ссылками (файл открывается чисто).
  2. EXIF/GPS/IPTC/XMP отсутствуют в изображениях, `/Info` и XMP — в PDF, подтверждено проверкой присутствия.
  3. Чистота **доказана, а не заявлена**: выход перепроверяется реализацией разбора, независимой от той, что удаляла (прецедент проекта — veraPDF, проверяющий вывод LibreOffice); удалённое физически отсутствует в выходных байтах — полная перезапись, а не incremental update, оставляющий объект восстановимым в предыдущей ревизии; любой остаток валит джобу closed до того, как что-либо попало в S3.
  4. Клиент получает вторым выходом с `role=report` перечень найденного и удалённого, сформулированный так, что заявляет ТОЛЬКО реально проверенные классы угроз — никогда общую свободу от вредоносного содержимого. Это ложная гарантия, а не маркетинг: индустрия прямо фиксирует, что CDR не определяет функциональность вредоносного кода.
  5. `probe` — полноценный восьмой engine-класс по отлаженному шаблону: своя очередь, свой бинарник и контейнер, fail-closed валидация входа, transient/terminal классификация, **ИЗМЕРЕННЫЙ** на собственном корпусе таймаут (отличный от 65s/742s/744s/753s — копирование чужого таймаута зафиксировано как антипаттерн проекта), и восьмой ScaledObject, оставляющий существующие семь байт-неизменными.

**Design decisions deferred to phase planning** (research не смог их закрыть, и это честно): blacklist-удаление против reconstruct-from-trusted-content; выбор независимого парсера-верификатора. Оба должны быть записаны как решения ДО того, как ляжет код.

**Blast-radius note (research correction)**: добавление восьмой очереди — НЕ высокий риск. PromQL каждого `scaledobject-*.yaml` замкнут на собственный литерал имени очереди, а `AllConvertQueues()` существует именно для того, чтобы коллектор глубины не правился второй раз; доказано дважды (v1.10 OCR, v1.11 pandoc). Дорого — **переименование** очереди (7 PromQL + `AllConvertQueues()` + маршрутизация реконсилятора), и v1.13 его не требует.

**Autonomous**: false (SC5 — операторский замер таймаута на собственном корпусе)

### Phase 61: v1.12 Debt Tail

**Goal**: Закрыть самый весомый перенос v1.12 — застрявшая джоба, навсегда съедающая суточный бюджет клиента без операторского средства это снять — плюс два мелких пункта с нулевым пересечением по файлам с остальным милстоуном.
**Depends on**: Nothing (полностью независима; файловый набор не пересекается с фазами 56-60)
**Requirements**: DEBT-09, DEBT-10, DEBT-11
**Success Criteria** (what must be TRUE):

  1. Оператор может снять застрявшую in-flight джобу штатным средством, а не сырым SQL, поэтому навсегда застрявшая джоба больше не блокирует суточный бюджет клиента каждый следующий UTC-день; связка «доступность квот ↔ здоровье `cmd/webhook-worker`» задокументирована.
  2. Секция квот в README внутренне непротиворечива (`README.md:242` против `README.md:294`), и список engine-классов выводится из `convert.AllEngines()`, а не хардкодится числом.
  3. `PurgeUsage` (`internal/jobs/retention.go:126`) берёт cutoff из часов Postgres, как `evaluate()`, `recordUsage` и `UTCToday`, а не из `time.Now()` хоста.

**Placement**: отдельной фазой-хвостом по прецеденту Phase 54 (CAPA-01) — сворачивание в 56-60 добавило бы несвязанный шум в дифф ревью основной работы. Может исполняться в любом слоте.

**Autonomous**: true

### Phase Ordering Rationale

- **Жёсткие зависимости, проверенные по коду**: 56 → 57 (после консолидации `process()` читает `job.Operation` из одного места, а не из семи); 57 → 58 (перечисление доступных операций осмысленно только после измерения); 58 → 59 (маршрутизация читает отчёт); 56+57 → 60.
- **Sanitize последним, несмотря на сильнейшую продуктовую историю** — ему нужны дескриптор-паттерн, измерение операции и роль выхода одновременно, и он несёт самую высокую планку безопасности в милстоуне.
- **56 не имеет жёстких зависимостей вниз, но окупается в 60**: без него `probe` был бы восьмой ручной копией, а каждая будущая post-commit забота платила бы ×8.
- **Хвост долга (61) изолирован по файлам** и может уехать в любой слот — вынесен отдельно ровно для чистоты ревью, как Phase 54 в v1.12.

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|-----------------|--------|-----------|
| 1. Merge, Auth & Rate Limiting | v1.0 | 4/4 | Complete | 2026-07-04 |
| 2. Webhook Delivery | v1.0 | 3/3 | Complete | 2026-07-04 |
| 3. Retry-Safety & Reconciler | v1.0 | 3/3 | Complete | 2026-07-06 |
| 4. Content Validation, Storage Lifecycle & Observability | v1.0 | 5/5 | Complete | 2026-07-07 |
| 5. Webhook SSRF Private-IP Opt-Out | v1.1 | 1/1 | Complete | 2026-07-08 |
| 6. Reconciler Webhook-Gap Sweep & Staleness Soak Test | v1.1 | 4/4 | Complete | 2026-07-08 |
| 7. Image Dimension Limit (Decompression-Bomb Protection) | v1.1 | 2/2 | Complete | 2026-07-08 |
| 8. Document Content Safety & Format Detection | v1.2 | 2/2 | Complete | 2026-07-09 |
| 9. LibreOffice Converter Engine | v1.2 | 2/2 | Complete | 2026-07-09 |
| 10. Document Worker & Reconciler Integration | v1.2 | 4/4 | Complete | 2026-07-09 |
| 11. API Routing & End-to-End Document Conversion | v1.2 | 4/4 | Complete | 2026-07-10 |
| 12. Tech Debt Cleanup | v1.3 | 1/1 | Complete    | 2026-07-10 |
| 13. Cross-Format Conversion & Input Safety | v1.3 | 3/3 | Complete    | 2026-07-10 |
| 14. Validated Conversion Options & PDF/A Export | v1.3 | 3/3 | Complete    | 2026-07-10 |
| 15. HTML→PDF Chromium Engine | v1.3 | 5/5 | Complete    | 2026-07-11 |
| 16. Webhook Delivery Decoupling | v1.3 | 5/5 | Complete | 2026-07-12 |
| 17. Tech Debt Cleanup | v1.4 | 2/2 | Complete | 2026-07-12 |
| 18. Presets | v1.4 | 4/4 | Complete | 2026-07-12 |
| 19. CI Pipeline | v1.4 | 2/2 | Complete | 2026-07-13 |
| 20. Presets REST CRUD & Format Discovery | v1.5 | 2/2 | Complete | 2026-07-13 |
| 21. MCP Server | v1.5 | 3/3 | Complete | 2026-07-13 |
| 22. CFB Classification | v1.5 | 2/2 | Complete | 2026-07-13 |
| 23. veraPDF ISO 19005 Validation | v1.5 | 3/3 | Complete | 2026-07-13 |
| 24. Helm Chart Core & Landmine Closure | v1.6 | 3/3 | Complete | 2026-07-14 |
| 25. MCP Streamable HTTP | v1.6 | 3/3 | Complete | 2026-07-14 |
| 26. Operator System-Presets REST | v1.6 | 2/2 | Complete    | 2026-07-14 |
| 27. KEDA Autoscaling | v1.6 | 3/3 | Complete    | 2026-07-16 |
| 28. Autoscale Load-Proof | v1.6 | 3/3 | Complete    | 2026-07-17 |
| 29. v1.6 Hardening Tail | v1.7 | 3/3 | Complete    | 2026-07-17 |
| 30. Audio Engine Foundation | v1.7 | 3/3 | Complete    | 2026-07-18 |
| 31. Queue, Worker & Routing Integration | v1.7 | 4/4 | Complete    | 2026-07-18 |
| 32. Containerization & Local E2E + RTF Gate | v1.7 | 5/5 | Complete    | 2026-07-18 |
| 33. KEDA/Helm Chart Integration | v1.7 | 3/3 | Complete    | 2026-07-18 |
| 34. AV Engine Foundation | v1.8 | 3/3 | Complete    | 2026-07-20 |
| 35. Queue, Worker & Routing Integration | v1.8 | 7/7 | Complete    | 2026-07-22 |
| 36. Containerization & RTF-Measured Timeout | v1.8 | 5/5 | Complete    | 2026-07-23 |
| 37. KEDA/Helm Chart Integration | v1.8 | 3/3 | Complete    | 2026-07-23 |
| 38. Schema Foundation | v1.9 | 1/1 | Complete    | 2026-07-24 |
| 39. Cancellation (end-to-end) | v1.9 | 4/4 | Complete    | 2026-07-24 |
| 40. Job Listing | v1.9 | 2/2 | Complete   | 2026-07-25 |
| 41. Progress Core | v1.9 | 4/4 | Complete | 2026-07-25 |
| 42. Live Push | v1.9 | 4/4 | Complete   | 2026-07-25 |
| 43. OCR Engine Foundation | v1.10 | 4/4 | Complete   | 2026-07-26 |
| 44. Async Pipeline Wiring | v1.10 | 4/4 | Complete   | 2026-07-26 |
| 45. Containerization + Measured Timeout Gate | v1.10 | 4/4 | Complete | 2026-07-27 |
| 46. KEDA/Helm Chart Integration | v1.10 | 3/3 | Complete | 2026-07-27 |
| 47. Pandoc Engine Foundation | v1.11 | 3/3 | Complete   | 2026-07-30 |
| 48. Async Pipeline Wiring | v1.11 | 3/3 | Complete   | 2026-07-30 |
| 49. Containerization + Measured Timeout Gate | v1.11 | 4/4 | Complete   | 2026-08-01 |
| 50. KEDA/Helm Chart Integration | v1.11 | 4/4 | Complete    | 2026-08-01 |
| 51. Attribution, Accounting & Retention | v1.12 | 5/5 | Complete    | 2026-08-02 |
| 52. Quota Enforcement & Intra-Class Fairness | v1.12 | 9/9 | Complete   | 2026-08-03 |
| 53. Operator Usage Visibility | v1.12 | 7/7 | Complete    | 2026-08-05 |
| 54. Per-Class Capacity Floor (SEED-011) | v1.12 | 2/2 | Complete    | 2026-08-06 |
| 55. Live Gate — Multi-Replica Quota Correctness | v1.12 | 7/7 | Complete    | 2026-08-07 |
| 56. JobRunner Consolidation & Structured Error Codes | v1.13 | 7/7 | Complete   | 2026-08-08 |
| 57. Operation Dimension & Output Roles | v1.13 | 0/TBD | Not started | — |
| 58. Synchronous Inspection | v1.13 | 0/TBD | Not started | — |
| 59. Born-Digital PDF Routing | v1.13 | 0/TBD | Not started | — |
| 60. Sanitize — Eighth Engine Class | v1.13 | 0/TBD | Not started | — |
| 61. v1.12 Debt Tail | v1.13 | 0/TBD | Not started | — |

---

*v1.13 (Inspect & Operations) started 2026-08-07, Phases 56-61 — 28/28 v1 requirements mapped, 0 orphans. Two operator-gated phases: 58 (live process-isolation proof for the synchronous inspect path) and 60 (measured `probe` engine timeout). Next: `/gsd:discuss-phase 56`.*

*v1.12 (Consumer Accounting & Fairness) shipped 2026-08-07, Phases 51-55 — 15/15 v1 requirements, 30 plans, milestone audit `tech_debt` (15/15 requirements, 5/5 phases, 9/9 cross-phase seams, 1/1 E2E flow, 0 blockers, 16 non-blocking debt items recorded). One live gate: multi-replica quota correctness at `api.replicas: 2` (10/10 rounds, 167 assertions, evidence committed). Phases 52 and 53 each went through a real gaps_found → closure → re-verification cycle. Next: `/gsd:new-milestone`.*
</content>
