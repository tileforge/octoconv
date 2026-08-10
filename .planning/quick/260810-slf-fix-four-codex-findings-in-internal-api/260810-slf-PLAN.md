---
phase: quick-260810-slf
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/api/inspect_handlers.go
  - internal/api/inspect_handlers_test.go
  - internal/api/inspectgate.go
  - internal/api/inspectgate_test.go
  - internal/api/estimate.go
  - internal/api/estimate_test.go
  - internal/api/api.go
  - cmd/api/main.go
autonomous: true
requirements: [PR9-F7-ZIP-DIRECTORY-UNBOUNDED, PR9-F8-SEMAPHORE-BEFORE-BODY, PR9-F9-PREADMIT-CONTEXT, PR9-F10-PROVENANCE-OVERRIDE]

must_haves:
  truths:
    - "Разбор ZIP central directory ограничен структурно, а не только последующие циклы по записям — и не выполняется дважды на один файл"
    - "Слот семафора не удерживается клиентом, который медленно льёт тело: слот берётся после чтения тела, либо тело имеет дедлайн, гарантирующий освобождение слота"
    - "Проверка квоты не приходит с уже истёкшим контекстом после медленной, но легитимной загрузки"
    - "Переопределённый оператором таймаут движка не может быть помечен как measured"
  artifacts:
    - path: "internal/api/inspect_handlers.go"
      provides: "Ограниченный и однократный разбор ZIP-каталога; свежий дедлайн для проверки квоты"
    - path: "internal/api/inspectgate.go"
      provides: "Захват слота, не удерживаемый медленной передачей тела"
    - path: "internal/api/estimate.go"
      provides: "Провенанс, привязанный к фактическому значению, а не к фиксированной таблице"
---

<objective>
Закрыть четыре замечания внешнего ревью (codex, PR #9) в пакете `internal/api`. Два из них — обходы границ, которые фаза 58 бралась поставить: заявленный потолок не ограничивает разбор ZIP-каталога, а семафор удерживается клиентом, не делающим никакой работы. Третье ломает INSP-04 на практике. Четвёртое — нечестность метки, идентичная той, что уже исправлялась в D-11.

Purpose: живой гейт SC6 нельзя гонять до этих правок — он нагружает ровно те пути, которые здесь меняются, и evidence против текущего кода пришлось бы выбросить.
Output: четыре починенных места, каждое с тестом, доказывающим свойство, а не намерение.
</objective>

<context>
@.planning/phases/58-synchronous-inspection/58-CONTEXT.md
@.planning/phases/58-synchronous-inspection/58-RESEARCH.md
@CLAUDE.md

Замечания приведены дословно, с file:line от ревьюера. Каждое проверено оркестратором против кода.
</context>

<tasks>

<task type="auto">
  <name>Task 1: ZIP central directory — структурная граница и один разбор вместо двух</name>
  <files>internal/api/inspect_handlers.go, internal/api/inspect_handlers_test.go</files>
  <read_first>
    - internal/api/inspect_handlers.go строки 160-230 — создание `Tracker` (около 167), детект формата и вызов `convert.SniffContainer` (около 215), затем диспетчер семейств
    - internal/inspect/container.go около строки 103 — ВТОРОЙ вызов `convert.SniffContainer` на тот же файл
    - internal/convert/docsniff.go — что именно делает `SniffContainer`: `archive/zip.NewReader` парсит central directory ЦЕЛИКОМ до того, как сработает любой per-entry cap
    - internal/inspect/budget.go — `Tracker.Consume`, чтобы понять, как учитывается работа
  </read_first>
  <action>
**Замечание (`inspect_handlers.go:216`), дословно:** «For a ZIP-shaped upload, format detection calls `convert.SniffContainer` before the `Tracker` exists. That function invokes `archive/zip.NewReader` and walks the entire attacker-controlled central directory; dispatch later repeats `SniffContainer` and constructs another `zip.Reader`, while `maxZipEntriesScanned` only limits subsequent loops after the full directory has already been parsed and allocated. A near-100 MiB archive containing a very large number of tiny entries can therefore consume CPU and heap far beyond `INSPECT_MAX_WORK_BYTES` despite every advertised ceiling, so directory parsing itself needs a structural entry/read bound or isolation.»

Уточнение оркестратора: одна деталь формулировки неверна — `Tracker` создаётся на строке 167, ДО вызова на 215. Существо замечания это не меняет: работа разбора каталога не оплачивается трекером ни в одном из двух вызовов, поэтому заявленный потолок её не ограничивает.

Две вещи к исправлению:
  1. **Двойной разбор.** `SniffContainer` вызывается для детекта формата и повторно внутри `AnalyzeContainer`. Результат первого вызова должен передаваться дальше, а не вычисляться заново. Это ровно вдвое сокращает атакуемую работу и убирает расхождение, при котором два разбора одного файла могли бы дать разные ответы.
  2. **Структурная граница на сам разбор.** `zip.NewReader` парсит каталог целиком до того, как код увидит хоть одну запись. Ограничить нужно ДО вызова: по размеру области каталога и/или по числу заявленных записей, читая end-of-central-directory до полного разбора. Если граница превышена — не разбирать, вернуть результат, ведущий к `unknown` по контейнерным проверкам с причиной потолка, НЕ к `clean` и НЕ к 500.

Записать комментарием, почему граница стоит именно перед `zip.NewReader`: после него работа уже сделана, и любая проверка постфактум ограничивает лишь то, что и так дёшево.
  </action>
  <verify>
    <automated>go test ./internal/api/ -count=1</automated>
  </verify>
  <acceptance_criteria>
    - `grep -c 'SniffContainer' internal/api/inspect_handlers.go` печатает 1, и результат передаётся в анализатор вместо повторного вызова (проверить чтением, что `internal/inspect/container.go` принимает готовый результат либо вызывается ровно один раз на запрос)
    - ZIP с числом заявленных записей свыше границы: контейнерные проверки получают `unknown` с причиной потолка; ответ НЕ 500 и НЕ `clean` — доказано новым тестом
    - ZIP нормального размера обрабатывается как раньше — существующие тесты зелёные без правок
    - Тест, доказывающий, что `zip.NewReader` НЕ вызывается для превышающего границу архива (например, через счётчик или через фикстуру, чей полный разбор был бы заметно дорог)
    - `go test ./internal/api/ -count=1 -race` завершается кодом 0
    - Инверсия: убрать границу перед разбором → новый тест падает. Дословный вывод в SUMMARY
  </acceptance_criteria>
  <done>Каталог разбирается один раз и только в пределах структурной границы; превышение даёт честный unknown.</done>
</task>

<task type="auto">
  <name>Task 2: Семафор не удерживается медленной передачей тела; квота получает живой контекст</name>
  <files>internal/api/inspectgate.go, internal/api/inspectgate_test.go, internal/api/inspect_handlers.go, internal/api/inspect_handlers_test.go, internal/api/api.go, cmd/api/main.go</files>
  <read_first>
    - internal/api/inspectgate.go около строки 120 — где берётся слот семафора относительно чтения тела
    - internal/api/inspect_handlers.go строки 108-135 — `context.WithTimeout` (около 113), `MaxBytesReader`, `ParseMultipartForm` (около 126)
    - cmd/api/main.go — конфигурация `http.Server`: какие таймауты заданы (`ReadHeaderTimeout`) и какие нет
    - internal/api/estimate.go — где используется контекст для `PreAdmit`
  </read_first>
  <action>
**Замечание A (`inspectgate.go:120`), дословно:** «The semaphore is acquired before `handleInspect` calls `ParseMultipartForm`, and the API server configures only `ReadHeaderTimeout`, not a request-body read timeout. Consequently, four authenticated clients can send multipart bodies indefinitely slowly and hold every default inspect slot without doing any parsing, causing all legitimate inspect requests to receive `inspect_busy`. Read the size-bounded body before acquiring the parsing slot, or enforce a body deadline that guarantees slot release.»

Это классический slowloris против семафора: защита от залпа сама становится вектором отказа. Выбрать ОДНО и обосновать:
  (а) брать слот ПОСЛЕ чтения тела — слот тогда охраняет ровно разбор, для чего и заведён;
  (б) дедлайн на чтение тела, гарантирующий освобождение слота.
Вариант (а) прямее и не добавляет ещё одного таймаута; при нём стоит проверить, что чтение тела само по себе ограничено размером (`MaxBytesReader`) — иначе граница просто переезжает.

Если выбран (б) или если чтение тела остаётся под слотом по иным причинам — задать серверу таймаут чтения тела явно, а не полагаться на `ReadHeaderTimeout`, который к телу не относится.

**Замечание B (`inspect_handlers.go:114`), дословно:** «This two-second context starts before `ParseMultipartForm` reads as much as 100 MiB from the client. Any legitimate upload taking longer than `INSPECT_MAX_WORK_DURATION` arrives at `availableOperations` with an already-expired context, so every context-aware `PreAdmit` call becomes a non-quota error and all operations report quota as unknown regardless of actual quota. Create the PreAdmit timeout after body intake and parsing, or give quota checking its own fresh deadline.»

Потолок работы предназначен ограничивать РАЗБОР, а не сетевую передачу. Сейчас он тратится на приём байт, и INSP-04 деградирует до постоянного `unknown` для любой медленной, но совершенно легитимной загрузки. Создавать дедлайн работы после приёма тела, либо выдать проверке квоты собственный свежий дедлайн.
  </action>
  <verify>
    <automated>go test ./internal/api/ -count=1 -race</automated>
  </verify>
  <acceptance_criteria>
    - Тест: N+1 клиентов, где N — размер семафора, льют тело медленно; легитимный запрос, пришедший следом, НЕ получает `inspect_busy` из-за них — доказано тестом, использующим медленный `io.Reader` в теле
    - Тест: загрузка, чей приём занял дольше `INSPECT_MAX_WORK_DURATION`, всё равно получает осмысленный вердикт квоты (не `unknown` по причине истёкшего контекста) — доказано тестом, где приём искусственно замедлен
    - `grep -n 'ReadTimeout\|ReadHeaderTimeout' cmd/api/main.go` — если выбран вариант (б), присутствует явный таймаут тела; если вариант (а), в SUMMARY записано, почему он не нужен
    - Существующее поведение при переполнении семафора настоящим разбором сохранено: 429 с `errcode.InspectBusy` — прежний тест зелёный без правок
    - `go test ./... -count=1 -race` завершается кодом 0
    - Две инверсии: (а) вернуть захват слота перед чтением тела → тест медленного клиента падает; (б) вернуть создание контекста до приёма → тест квоты падает. Дословные выводы в SUMMARY
  </acceptance_criteria>
  <done>Слот охраняет разбор, а не передачу; потолок работы тратится на работу, а не на сеть.</done>
</task>

<task type="auto">
  <name>Task 3: Провенанс оценки привязан к фактическому значению</name>
  <files>internal/api/estimate.go, internal/api/estimate_test.go</files>
  <read_first>
    - internal/api/estimate.go около строки 241 — откуда берётся число (`EngineTimeouts`) и откуда метка провенанса (фиксированная таблица)
    - .planning/phases/58-synchronous-inspection/58-CONTEXT.md, запись D-11 вместе с поправкой от 2026-08-10 — источником истины для провенанса является `.env.example`, а не дефолт в коде
    - .env.example, блоки таймаутов движков — как записан провенанс для audio/av/ocr/pandoc
  </read_first>
  <action>
**Замечание (`estimate.go:241`), дословно:** «The reported number comes from the runtime-configurable `EngineTimeouts` map, while its provenance is selected from a fixed per-engine table. If an operator overrides a historically measured engine timeout, the arbitrary replacement is still labeled `measured`, even though that exact upper bound did not come from the documented run. Configuration must carry provenance alongside each timeout, or any overridden value should conservatively be labeled unmeasured.»

Метка описывает происхождение КОНКРЕТНОГО числа, а не движка вообще. Если оператор переопределил значение, документированный прогон его не порождал, и звать такое число `measured` — ровно та нечестность, которую фаза 58 берётся устранять в отчёте.

Выбрать ОДНО и обосновать комментарием:
  (а) провенанс едет вместе со значением через конфиг — точнее, но расширяет конфиг;
  (б) значение, отличающееся от известного измеренного дефолта, консервативно помечается `unmeasured` — дешевле и ошибается в безопасную сторону.

Записать рассуждение рядом с кодом: поле описывает свойство оценки, а не текущее состояние калибровки движка (это уже зафиксировано поправкой к D-11).
  </action>
  <verify>
    <automated>go test ./internal/api/ -run Estimate -count=1</automated>
  </verify>
  <acceptance_criteria>
    - Тест: движок с известным измеренным дефолтом, значение НЕ переопределено → провенанс `measured`
    - Тест: тот же движок, значение переопределено на другое → провенанс НЕ `measured` — доказано тестом, чьё имя содержит `Override`
    - Тест: движок без измеренного провенанса → `unmeasured` независимо от значения
    - `go test ./internal/api/ -count=1` завершается кодом 0
    - Инверсия: вернуть выбор провенанса по фиксированной таблице → тест переопределения падает. Дословный вывод в SUMMARY
  </acceptance_criteria>
  <done>Переопределённое значение не может выдавать себя за измеренное.</done>
</task>

</tasks>

<threat_model>
## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation |
|-----------|----------|-----------|-------------|------------|
| T-SLF-01 | Denial of Service | Разбор ZIP central directory вне заявленного потолка | mitigate | Структурная граница ПЕРЕД `zip.NewReader` плюс устранение двойного разбора; превышение даёт `unknown`, не 500 |
| T-SLF-02 | Denial of Service | Удержание слотов семафора медленной передачей тела | mitigate | Слот охраняет разбор, а не приём; при удержании под слотом — явный дедлайн тела. Защита от залпа перестаёт быть вектором залпа |
| T-SLF-03 | Repudiation | Квота всегда `unknown` после медленной загрузки | mitigate | Дедлайн работы создаётся после приёма тела либо у проверки квоты свой свежий |
| T-SLF-04 | Repudiation | Переопределённый таймаут помечен `measured` | mitigate | Провенанс привязан к фактическому значению; при расхождении — консервативный `unmeasured` |
| T-SLF-SC | Tampering | npm/pip/cargo installs | accept | Ноль новых зависимостей — только Go stdlib |
</threat_model>

<verification>
- `go build ./... && go vet ./...` — чисто
- `gofmt -l internal/api cmd/api` — пустой вывод
- `go test ./... -count=1 -race` — зелёный по всему репозиторию
- `git diff --stat -- go.mod go.sum` — пусто
- `git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json` печатает `1`, то же для двух других golden
- `npx @redocly/cli lint docs/openapi.yaml` — 0 ошибок (если контракт затронут)
- Каждая инверсия выполнена, дословный вывод падения в SUMMARY
</verification>

<success_criteria>
- Четыре замечания закрыты, каждое с тестом, доказывающим свойство
- Оба обхода границ закрыты структурно, а не документированы как известные
- Ноль новых зависимостей
</success_criteria>
