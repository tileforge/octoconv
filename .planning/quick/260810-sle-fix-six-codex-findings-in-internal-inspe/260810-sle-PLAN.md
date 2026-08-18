---
phase: quick-260810-sle
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/inspect/pdf.go
  - internal/inspect/pdf_test.go
  - internal/inspect/image.go
  - internal/inspect/image_test.go
  - internal/inspect/container.go
  - internal/inspect/container_test.go
  - internal/inspect/report.go
  - internal/inspect/report_test.go
autonomous: true
requirements: [PR9-F1-PDF-HEX-ESCAPES, PR9-F2-PDF-STREAM-FALSE-POSITIVE, PR9-F3-IMAGE-WINDOW, PR9-F4-RELS-SKIPPED, PR9-F5-COVERAGE-CHARACTERISTICS, PR9-F6-UNKNOWN-FORMAT]

must_haves:
  truths:
    - "PDF, в котором имя записано hex-эскейпом (/Java#53cript, /Obj#53tm), не может дать clean: либо эскейп декодирован и находка/downgrade срабатывают, либо весь PDF-блок опускается до unknown"
    - "Изображение, чьи метаданные лежат за пределами байтового окна, даёт unknown/work_ceiling_bytes, а не clean"
    - "Часть контейнера, пропущенная из-за ЗАЯВЛЕННОГО размера свыше потолка, приводит к unknown-вердикту, а не к clean"
    - "coverage.complete=false, если ЛЮБАЯ характеристика признала усечение по собственному потолку сервиса — не только checks"
    - "Нераспознанный формат даёт unknown по всему словарю, а не not_applicable"
    - "Литерал /JS или /AA внутри тела потока не даёт critical-находку pdf_javascript"
  artifacts:
    - path: "internal/inspect/pdf.go"
      provides: "Обработка hex-эскейпов имён и лексическая фильтрация тел потоков"
    - path: "internal/inspect/image.go"
      provides: "Признак усечения окна, поднимаемый в статус проверки"
    - path: "internal/inspect/container.go"
      provides: "Пропуск части по заявленному размеру возвращает ceiling-вердикт"
    - path: "internal/inspect/report.go"
      provides: "Coverage учитывает причины усечения в characteristics; unknown-формат не маскируется под not_applicable"
---

<objective>
Закрыть шесть замечаний внешнего ревью (codex, PR #9) в пакете `internal/inspect`. Пять из них — один класс: отчёт говорит `clean` там, где честный ответ `unknown`. Ровно то, ради предотвращения чего фаза 58 существует, и ровно то, что её собственная инверсионная дисциплина не поймала, потому что тесты писали те же агенты, что и код.

Purpose: без этих правок эндпоинт врёт в пяти сценариях, из которых один (hex-эскейпы в именах PDF) прямо эксплуатируем — маленький вредоносный PDF получает `clean`.
Output: шесть починенных мест, каждое с тестом, который падает на до-фиксовом поведении.
</objective>

<context>
@.planning/phases/58-synchronous-inspection/58-CONTEXT.md
@.planning/phases/58-synchronous-inspection/58-RESEARCH.md
@CLAUDE.md

Замечания приведены дословно ниже, с file:line от ревьюера. Каждое проверено оркестратором против кода перед постановкой задачи — это не гипотезы.
</context>

<tasks>

<task type="auto">
  <name>Task 1: PDF — hex-эскейпы имён и ложные срабатывания на телах потоков</name>
  <files>internal/inspect/pdf.go, internal/inspect/pdf_test.go</files>
  <read_first>
    - internal/inspect/pdf.go — весь файл: таблицы литералов (около строки 80), `containsCompressionSignal`, `firstTokenLocation` (около строки 199), и header-комментарий, объясняющий приоритет детектора сжатия
    - internal/inspect/pdf_test.go — существующие тесты, особенно `TestAnalyzePDFObjStmNeverReportsClean` и `TestAnalyzePDFFindingSurvivesObjStm`, чью семантику нельзя ослабить
    - internal/inspect/status.go — три значения статуса и перечень причин; новая причина, если понадобится, объявляется там, а не локально
  </read_first>
  <action>
**Замечание 1 (`pdf.go:112`), дословно:** «PDF names permit hexadecimal escapes, so a valid dictionary can spell `/JavaScript` as `/Java#53cript` (and `/ObjStm` as `/Obj#53tm`). Because these tables recognize only literal byte spellings, a small malicious PDF using escaped names is reported clean without triggering either the positive finding or the compressed-object-stream downgrade. Decode PDF name escapes, or conservatively return unknown when the bounded scanner encounters them.»

Выбрать ОДНО из двух и обосновать выбор комментарием:
  (а) декодировать `#XX` в именах перед сравнением — точнее, но добавляет парсер;
  (б) при встрече `#` в позиции, синтаксически возможной внутри имени, опускать весь PDF-блок до `unknown` с причиной — консервативно и дёшево.

Вариант (б) предпочтителен по духу D-01/D-02 (bounded, минимум нового парсинга) и по тому же принципу, что уже применён к `/ObjStm`: непонятое не выдаётся за чистое. Если выбран (б), причина должна быть ОТЛИЧНОЙ от `compressed_object_streams` — это другая слепота, и слипание двух причин лишит будущего читателя возможности понять, какая сработала.

**Замечание 2 (`pdf.go:199`), дословно:** «When a literal such as `/JS` occurs inside an image, font, or other stream payload, `firstTokenLocation` still sets `found`, and this branch emits a critical `pdf_javascript` finding even though the bytes are not a PDF name in an action dictionary. Three-byte markers such as `/JS` and `/AA` are especially prone to appearing in arbitrary compressed data, so benign PDFs can receive false critical findings. Positive findings need at least lexical filtering that excludes stream bodies and comments.»

Добавить лексическую фильтрацию: литерал считается PDF-именем только если он не находится внутри `stream`…`endstream` и не после `%` до конца строки. Минимальный корректный подход — один проход, отмечающий диапазоны тел потоков и комментариев, и проверка, что позиция найденного токена вне них.

ВАЖНО, чтобы не сломать уже доказанное: фильтрация уменьшает ложные ПОЛОЖИТЕЛЬНЫЕ находки. Она НЕ должна превращать «токен найден только внутри тела потока» в `clean` — тело потока может быть сжатым, то есть это ровно случай слепоты. Такой файл должен получить `unknown`, а не `clean`. Записать это комментарием: фильтрация меняет уверенность в находке, а не право сказать «чисто».
  </action>
  <verify>
    <automated>go test ./internal/inspect/ -count=1</automated>
  </verify>
  <acceptance_criteria>
    - `AnalyzePDF` на PDF, где `/JavaScript` записан как `/Java#53cript`: результат НЕ содержит `StatusClean` ни по одному PDF-коду — доказано новым тестом, чьё имя содержит `HexEscape`
    - `AnalyzePDF` на PDF, где `/ObjStm` записан как `/Obj#53tm`: PDF-блок опущен до `unknown` — доказано новым тестом
    - `AnalyzePDF` на PDF, где `/JS` встречается ТОЛЬКО внутри `stream`…`endstream`: результат НЕ содержит `Severity` critical по коду `pdf_javascript`, и при этом НЕ содержит `StatusClean` по нему же
    - `AnalyzePDF` на PDF, где `/JavaScript` стоит в настоящем словаре (вне потоков): находка сохраняется как и раньше — существующий `TestAnalyzePDFFindingSurvivesObjStm` остаётся зелёным без правок
    - `go test ./internal/inspect/ -count=1` завершается кодом 0
    - Инверсия: убрать обработку эскейпов → новый hex-тест падает; вернуть → зелёный. Дословный вывод падения в SUMMARY
  </acceptance_criteria>
  <done>Ни hex-эскейп, ни литерал внутри тела потока не могут дать ни ложный critical, ни ложное clean.</done>
</task>

<task type="auto">
  <name>Task 2: Изображения и контейнеры — усечение обязано давать unknown</name>
  <files>internal/inspect/image.go, internal/inspect/image_test.go, internal/inspect/container.go, internal/inspect/container_test.go</files>
  <read_first>
    - internal/inspect/image.go — `scanImageMetadata` (около строки 85) и то, как её промах превращается в статус
    - internal/inspect/container.go строки 275-300 — цикл по частям, ветка `if f.UncompressedSize64 > uint64(maxRelsPartBytes) { continue }`, и как `scanExternalReferences` возвращает `ceilingHit`
    - internal/inspect/container_test.go около строки 372 — тест, который сейчас утверждает отсутствие находки для пропущенной части; его надо ДОПОЛНИТЬ утверждением о вердикте, а не удалить
    - internal/inspect/status.go — причины усечения
  </read_first>
  <action>
**Замечание 3 (`image.go:85`), дословно:** «For an image larger than `maxBytes`, `scanImageMetadata` only examines the bounded head window, but any miss is converted directly to `StatusClean`. Valid PNG, WebP, HEIC, and JPEG files can place metadata after that window, so an upload with metadata beyond the default 8 MiB limit is confidently reported clean and coverage remains complete. The scanner must return whether its window truncated the file and use `unknown/work_ceiling_bytes` in that case.»

`scanImageMetadata` обязана возвращать признак «окно не покрыло файл целиком». Промах при усечённом окне → `unknown` с `work_ceiling_bytes`. Промах при окне, покрывшем файл полностью → `clean` (это по-прежнему законно).

**Замечание 4 (`container.go:287`), дословно:** «When a candidate `.rels`, `content.xml`, or `styles.xml` part declares more than `maxRelsPartBytes`, this branch skips it without setting `truncatedAnyPart`. If no later candidate contains a marker, `scanExternalReferences` returns `ceilingHit=false` and the caller reports the file clean even though the only relevant part was never inspected; the committed adversarial test currently codifies that false absence claim. Skipping a candidate because of the service ceiling must return an unknown ceiling verdict.»

Пропуск части по заявленному размеру — это отказ от работы по причине собственного потолка сервиса, а значит `ceilingHit` обязан стать true. Существующая логика «не доверять заявленному маленькому размеру, но доверять заявленному большому ровно настолько, чтобы пропустить работу» остаётся — меняется только то, что пропуск больше не выдаётся за проверку.

Тест около строки 372 не удалять: его утверждение «не открывать часть, заявившую огромный размер» верно и ценно. Дополнить его утверждением, что вердикт при этом — ceiling/unknown, а не clean. Ровно этого утверждения в нём и не хватало, из-за чего он заморозил ложное «чисто».
  </action>
  <verify>
    <automated>go test ./internal/inspect/ -count=1</automated>
  </verify>
  <acceptance_criteria>
    - Изображение размером больше окна, чьи метаданные лежат ЗА окном: статус проверки `unknown`, причина `work_ceiling_bytes` — доказано новым тестом
    - Изображение, целиком уместившееся в окно и не имеющее метаданных: статус `clean` — прежнее поведение сохранено, доказано тестом
    - Контейнер, чья единственная релевантная часть заявляет размер свыше `maxRelsPartBytes`: `scanExternalReferences` возвращает `ceilingHit=true`, и итоговый статус проверки внешних ссылок — `unknown`, не `clean`
    - Существующий тест около `container_test.go:372` по-прежнему утверждает, что часть НЕ была открыта, И дополнительно утверждает ceiling-вердикт
    - `go test ./internal/inspect/ -count=1` завершается кодом 0
    - Две инверсии: (а) вернуть `continue` без флага → новый тест контейнера падает; (б) вернуть прямое `StatusClean` в image → новый тест изображения падает. Дословные выводы в SUMMARY
  </acceptance_criteria>
  <done>Ни пропуск части, ни неполное окно не могут выглядеть как проверка, давшая «чисто».</done>
</task>

<task type="auto">
  <name>Task 3: Отчёт — coverage от характеристик и нераспознанный формат</name>
  <files>internal/inspect/report.go, internal/inspect/report_test.go</files>
  <read_first>
    - internal/inspect/report.go — `BuildReport`, `buildChecks` (около строки 151) и вычисление coverage (около строки 190)
    - internal/inspect/status.go — разделение статуса проверки и полноты отчёта; какие причины делают отчёт выборочным
    - internal/inspect/budget.go — `Tracker.Consume` и `Remaining`: обратить внимание, что `Consume` отвергает только значения БОЛЬШЕ лимита, поэтому списание ровно `Remaining()` не исчерпывает трекер
  </read_first>
  <action>
**Замечание 5 (`report.go:190`), дословно:** «Coverage examines ceiling reasons only in `checks`, not in characteristics. Media analyzers can return fields such as `duration_seconds_reason: work_ceiling_bytes` without producing any checks, and charging exactly `tr.Remaining()` does not exhaust the tracker because `Consume` rejects only values greater than the limit. Such a response therefore says `coverage.complete=true` while explicitly admitting that a characteristic was truncated by the service ceiling. Include characteristic ceiling reasons in report-wide coverage or explicitly mark the tracker exhausted when an analyzer reports truncation.»

Coverage обязана учитывать причины усечения, признанные характеристиками, а не только проверками. Отчёт, который в одном поле пишет `duration_seconds_reason: work_ceiling_bytes`, а в другом `coverage.complete: true`, противоречит сам себе — и противоречит INSP-03 буквально.

**Замечание 6 (`report.go:151`), дословно:** «When detection returns an empty format, no family claims any code and `buildChecks` falls back to `not_applicable/format_not_applicable` for the entire vocabulary. Applicability cannot be established when the format itself is unknown, and this contradicts the endpoint contract describing unrecognized content as carrying honest unknown statuses; clients currently receive a complete report implying that every risk check is irrelevant. Supply unknown checks for the unrecognized-format case rather than using the known-format fallback.»

Нераспознанный формат — это не «проверка неприменима», а «неизвестно, применима ли». `not_applicable` означает «нечего было искать», и утверждать это про файл, чей формат не определён, нельзя. Весь словарь получает `unknown` с причиной, называющей нераспознанный формат.
  </action>
  <verify>
    <automated>go test ./internal/inspect/ -count=1</automated>
  </verify>
  <acceptance_criteria>
    - Отчёт, где анализатор вернул характеристику с причиной `work_ceiling_bytes` и НИ ОДНОЙ проверки: `coverage.complete == false` — доказано новым тестом
    - Отчёт по файлу с пустым определённым форматом: КАЖДЫЙ код словаря имеет статус `unknown`, и НИ ОДИН — `not_applicable` — доказано новым тестом, перебирающим весь словарь
    - Существующее поведение для распознанного формата не изменилось: коды чужих семейств по-прежнему `not_applicable` — прежние тесты зелёные без правок
    - `go test ./internal/inspect/ -count=1` завершается кодом 0
    - Инверсия: вернуть coverage к учёту только checks → новый тест характеристик падает. Дословный вывод в SUMMARY
  </acceptance_criteria>
  <done>Отчёт не может признать усечение в одном поле и заявить полноту в другом; нераспознанный формат не выдаётся за неприменимость.</done>
</task>

</tasks>

<threat_model>
## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation |
|-----------|----------|-----------|-------------|------------|
| T-SLE-01 | Spoofing | PDF с hex-эскейпом имени | mitigate | Эскейп либо декодируется, либо переводит блок в `unknown`; вредоносный PDF не может получить `clean`, назвав свои действия иначе |
| T-SLE-02 | Repudiation | Ложная critical-находка на безобидном PDF | mitigate | Лексическая фильтрация тел потоков и комментариев; при этом право сказать `clean` не расширяется — только уверенность в находке |
| T-SLE-03 | Information Disclosure | Метаданные за окном отчитаны как отсутствующие | mitigate | Признак усечения окна поднимается в статус `unknown/work_ceiling_bytes` |
| T-SLE-04 | Repudiation | Пропущенная часть контейнера отчитана как проверенная | mitigate | Пропуск по потолку выставляет `ceilingHit`; тест дополнен утверждением о вердикте, а не только об отсутствии находки |
| T-SLE-05 | Repudiation | Самопротиворечивый отчёт (усечение + complete) | mitigate | Coverage считает причины усечения и в характеристиках |
| T-SLE-06 | Repudiation | Нераспознанный формат как «всё неприменимо» | mitigate | Весь словарь получает `unknown` |
| T-SLE-SC | Tampering | npm/pip/cargo installs | accept | Ноль новых зависимостей — только Go stdlib. Package Legitimacy Gate неприменим |
</threat_model>

<verification>
- `go build ./... && go vet ./...` — чисто
- `gofmt -l internal/inspect` — пустой вывод
- `go test ./... -count=1 -race` — зелёный по всему репозиторию
- `git diff --stat -- go.mod go.sum` — пусто (ноль новых зависимостей)
- `git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json` печатает `1`, то же для двух других golden — байты пиннинга фазы не тронуты
- Каждая инверсия выполнена, дословный вывод падения записан в SUMMARY
</verification>

<success_criteria>
- Шесть замечаний закрыты, каждое с тестом, падающим на до-фиксовом поведении
- Ни одна правка не ослабила уже доказанные свойства (`TestAnalyzePDFObjStmNeverReportsClean`, `TestAnalyzePDFFindingSurvivesObjStm` зелёные без изменений)
- Ноль новых зависимостей
</success_criteria>
