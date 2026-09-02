# Roadmap: OctoConv

## Milestones

- ✅ **v1.0–v1.12** — Phases 1–55 (archived; see `.planning/milestones/`)
- ✅ **v1.13 Inspect & Operations** — Phases 56–61 (shipped 2026-09-02)
- 🚧 **v1.14 Typesetting & Debt** — Phases 62–64 (in progress)

## Phases

### 🚧 v1.14 Typesetting & Debt (Phases 62–64)

- [x] Phase 62: Operational Debt Tail — 1 plan, merged as PR #13
- [ ] Phase 63: Sanitize Hardening & Probe Page Ceiling (3 plans est.)
- [ ] Phase 64: Tectonic — Ninth Engine Class (5 plans est.)

---

### Phase 62: Operational Debt Tail

**Goal**: Убрать три вещи, которые v1.13 назвал и не починил, и каждая из
которых видна не в коде, а при эксплуатации: под, крашлупящий при каждом
холодном старте; красный полный прогон тестов, который надо расследовать, чтобы
отбросить; и отчёт, способный сказать «я прочитал файл полностью» и «я не смог
его прочитать» в одном теле.

**Depends on**: ничего. Файловый набор не пересекается с 63 и 64.
**Requirements**: DEBT-12, DEBT-13, DEBT-14

**Success Criteria** (what must be TRUE):

1. `helm upgrade --install` на чистом кластере поднимает api с `restartCount: 0` — не «обычно ноль», а доказано прогоном, потому что дефект наблюдался живьём, а не выведен.
2. `go test ./...` на живой базе даёт один и тот же результат при параллельном прогоне пакетов: soak-тест реконсилятора утверждает про свою джобу, а не про содержимое таблицы.
3. Ответ `POST /v1/inspect`, у которого все проверки `unknown/read_failed`, не может нести `coverage.complete: true`; словарь `Coverage.Reason`, `MarkTruncated` и описание в `docs/openapi.yaml` двигаются вместе, а не по отдельности.

**Autonomous**: true

---

### Phase 63: Sanitize Hardening & Probe Page Ceiling

**Goal**: Закрыть три находки восьмого раунда ревью PR #12, влитые
неисправленными по решению оператора, и дать probe границу по входу — потому
что замер таймаута показал, что таймаут сегодня единственная граница, а
стоимость постраничная.

**Depends on**: ничего (Phase 62 файлово не пересекается)
**Requirements**: SANI-07, SANI-08, SANI-09, PROBE-01, PROBE-02

**Success Criteria** (what must be TRUE):

1. ICC-профиль из нескольких APP2-чанков переживает санитацию целиком, и продолжающий чанк не отбрасывается из-за того, что его байты на позиции заголовка случайно читаются как маленький размер.
2. PDF, чей бинарный поток содержит литерал `endstream`, обрабатывается корректно: редакция не переписывает байты внутри потока и подсчёт ревизий не считает то, что лежит в полезной нагрузке — обе стороны выводят границу из `/Length`, а не из подстроки.
3. OOXML-пакет, у которого relationship нацелен на его собственную главную часть, не отдаётся клиенту пустым: верификатор требует наличия главной части и её объявления типа.
4. Документ, превышающий `PROBE_MAX_PAGES`, получает `too_many_pages` **до** записи в S3, и этот отказ терминален — не тратит бюджет ретраев, в отличие от таймаута, который остаётся транзиентным.
5. Существующие проверки не ослаблены: инверсия каждой новой границы валит именно свой тест.

**Autonomous**: true

---

### Phase 64: Tectonic — Ninth Engine Class

**Goal**: Дать клиенту настоящую вёрстку `latex → pdf` девятым engine-классом
— так, чтобы документ клиента не решал, что скачает воркер, и не мог выполнить
команду.

**Depends on**: ничего технически; ставится последней, потому что самая
крупная, и потому что 62/63 дешевле проверять на зелёном прогоне.
**Requirements**: TEX-01, TEX-02, TEX-03, TEX-04, TEX-05

**Success Criteria** (what must be TRUE):

1. Клиент отправляет `.tex` с `target=pdf` и получает свёрстанный PDF с формулами, перекрёстными ссылками и библиографией — сборка многопроходная и сходится.
2. Воркер не имеет сетевого доступа, и это свойство проверяется, а не декларируется: сборка проходит при полностью отрезанной сети, потому что бандл вшит в образ и запинен по версии и содержимому.
3. Набор пакетов закрыт и объявлен: документ с `\usepackage` вне набора получает отказ, называющий пакет, а список поддерживаемого держится равным документу механическим тестом — как `docs/SANITIZE.md` и `docs/ERROR-CODES.md`.
4. Враждебный документ с `\write18{...}` и `\input{|"cmd"}` не выполняет команду, и это доказано прогоном плюс инверсией: снятие `--untrusted` меняет исход.
5. `tex` — девятый engine-класс в полном контуре, и восемь существующих ScaledObject рендерятся байт-неизменными (`helm template` до и после, а не утверждение).
6. `TECTONIC_ENGINE_TIMEOUT` **измерен** на корпусе, включающем документ с библиографией и перекрёстными ссылками, а не только односпроходные — и отличается от 65s/600s/742s/744s/753s по замеру, а не чтобы отличаться.

**Design decision locked before planning** (`.planning/research/TECTONIC.md`):
бандл **сокращённый и собранный самостоятельно**, не полный TeXLive. Полный —
~2.69 GiB на слой, на порядок больше любого воркера во флоте; сокращённый
превращает «какие LaTeX-пакеты мы поддерживаем» в закрытый объявленный набор,
что совпадает с идиомой всего остального кода. Сетевой доступ вместо бандла
отвергнут явно, а не не рассмотрен.

**Autonomous**: false — таймаут требует измерения на корпусе, и корпус
многопроходных документов должен быть решён до замера, а не после.

## Progress

| Phase | Milestone | Plans | Status | Completed |
|-------|-----------|-------|--------|-----------|
| 62. Operational Debt Tail | v1.14 | 0/TBD | Not started | — |
| 63. Sanitize Hardening & Probe Page Ceiling | v1.14 | 0/TBD | Not started | — |
| 64. Tectonic — Ninth Engine Class | v1.14 | 0/TBD | Not started | — |

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
