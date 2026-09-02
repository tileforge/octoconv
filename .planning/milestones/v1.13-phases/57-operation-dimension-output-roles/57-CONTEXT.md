# Phase 57: Operation Dimension & Output Roles - Context

**Gathered:** 2026-08-08
**Status:** Ready for planning

<domain>
## Phase Boundary

У джобы появляется измерение «операция»: API принимает `operation` (по умолчанию `convert`), реестр разрешает движок по тройке `(operation, from, to)` вместо пары, неподдерживаемая тройка отклоняется 422 fail-closed до записи в S3 и до создания строки джобы. Параллельно `job_outputs` получает роль (`primary`/`report`/`preview`/`thumbnail`) аддитивной миграцией с DB-дефолтом, и `GET /v1/jobs/{id}` перестаёт слепо презайнить `outs[0]` — выбор становится role-driven, а все выходы возвращаются с их ролями. Существующий клиент, не приславший `operation`, не наблюдает ни одного изменения — доказано golden-пиннингом против до-фазового вывода.

**Не в скоупе:** сам движок sanitize и очередь `probe` (Phase 60), синхронный `POST /v1/inspect` (Phase 58), born-digital PDF routing (Phase 59), v1.12 debt tail (Phase 61). Реконсилятор, unique-лок TTL, очереди, KEDA и квоты в этой фазе НЕ меняются — см. D-14.

</domain>

<decisions>
## Implementation Decisions

### Приём `operation` в API

- **D-01:** Клиент передаёт операцию **multipart-полем `operation`** рядом с существующими `preset`/`target`/`opts` (`handlers.go:133-135`), через `r.FormValue(formFieldOperation)`. Один endpoint `POST /v1/jobs`, один admission-гейт, один fail-closed путь. Отдельный endpoint на операцию отвергнут: дублировал бы весь путь создания (валидация → квоты → S3) — ровно тот класс дублирования, который Phase 56 схлопнула в воркере. `operation` внутри `opts` отвергнут: `opts` — per-engine типизированная схема со strict-parse, а движок ещё не известен в момент, когда нужно резолвить операцию.

- **D-02:** **Источник истины перечня операций — реестр `convert.Default`, а не DB CHECK и не отдельный Go-allowlist.** Операция валидна тогда и только тогда, когда у неё есть хотя бы одна зарегистрированная тройка. Сегодня это ровно `convert`; `sanitize` станет принимаемым в момент, когда Phase 60 добавит `Default.Register(SanitizeConverter{})` в `converters.go`. Зарезервированные-но-пустые значения CHECK (`extract`/`archive`/`inspect`/`render`) отклоняются как любой мусор. Тот же принцип, что уже у `/v1/formats`: возможности выводятся из реестра, никогда не хардкодятся.

  **Явно унаследовано (не пересматривается):** реестр — **compile-time процесс-локальный синглтон** (`convert.Default`, `convert.go:172`, заполняется `init()` в `converters.go`). Воркеры НЕ регистрируют возможности в API — это другой процесс; три воркера (`cmd/av-worker`, `cmd/ocr-worker`, `cmd/pandoc-worker`) лишь перерегистрируют сконфигурированный экземпляр в свою копию. Динамический реестр возможностей — SEED-010, отвергнут на уровне проекта (PROJECT.md, ретроспектива v1.12: ослабляет свойство безопасности, а стоимость добавления движка и так низка). Следствие: «операция доступна» = «скомпилирована в API-бинарь», а не «развёрнута в этой инсталляции» — см. отложенные идеи.

- **D-03:** **Два кода отказа, оба 422**, оба из `internal/errcode` (Phase 56):
  - новый `unsupported_operation` — такой операции нет в реестре вообще (опечатка клиента, либо движок ещё не задеплоен в этой сборке);
  - существующий `unsupported_conversion` — операция есть, но пара `(from, to)` под неё не зарегистрирована.

  Это разные классы отказа с разными действиями клиента, поэтому дробление согласуется с D-02 Phase 56 («гранулярность по классу отказа»), а не нарушает её. Добавление кода аддитивно — ничего не переименовывается, Pitfall 11 не задет. Требует записи в `docs/ERROR-CODES.md` и соответствующего теста Go↔doc (D-06 Phase 56).

- **D-04:** **Нормализация до любого ветвления.** Отсутствующее или пустое поле → `convert` одной строкой сразу после `FormValue`; дальше в коде ветвления «прислали ли поле» не существует нигде. Явная `operation=convert` и отсутствие поля физически неразличимы ниже этой точки, поэтому расхождению негде завестись (снимает Pitfall 13 на API-стороне). Трёхзначная семантика («поля нет» vs «поле пустое» → 422) отвергнута как источник ровно того ветвления, которое Pitfall 13 называет причиной расхождения API↔DB.

- **D-05:** **Все три входа получают параметр в этой фазе:** REST, оба MCP-сервера (они проксируют в тот же `POST /v1/jobs`) и референсный фронтенд. Параметр опциональный. Цель — чтобы Phase 60 трогала только движок и очередь, а не публичные поверхности.

### Форма operation-aware реестра

- **D-06:** Конвертер объявляет операцию через **optional-интерфейс `OperationalConverter { Operation() string }`**, по точному прецеденту `ProgressConverter` (`internal/convert/progress.go:39-45`). `Register` делает type-assert и при отсутствии считает операцию `convert`. Все семь существующих конвертеров и `converters.go` — ноль изменений. Расширение `Pair` до троек в `Pairs()` отвергнуто: тронуло бы семь реализаций и десятки тестов в фазе, обещающей нулевой дифф. Явный `RegisterOp(op, c)` отвергнут: конвертер перестал бы отвечать за то, что он умеет.

- **D-07:** `Registry.m` ключуется расширенной тройкой; `Lookup`/`Supports`/`EngineFor` сохраняют двухаргументные сигнатуры и делегируют в `*Op`-варианты с `"convert"` — ~30+ существующих вызовов не трогаются. На `*Op` переезжают ровно два: `internal/worker` (чтение `job.Operation` в консолидированном `Runner` Phase 56) и `handlers.go:397` (`EngineFor` в `handleCreateJob`).

- **D-08:** **`Register` перестаёт быть молчаливым last-write-wins.** Сегодня это голое присваивание в map (`convert.go:122-127`), и единственная защита от коллизии — четыре теста в `pairs_disjoint_test.go` плюс комментарии в `converters.go`. Занятая тройка становится **panic на `init()`** — сервис не стартует. Плюс тест дизъюнктности по тройкам, включая identity-случай `pdf→pdf`, которого в проекте никогда не было.

- **D-09:** **Законная перерегистрация переезжает на явный `Replace`.** `cmd/av-worker`, `cmd/ocr-worker` и `cmd/pandoc-worker` намеренно перетирают zero-value плейсхолдеры сконфигурированными экземплярами (несущими лимиты безопасности: `MaxSourceDuration`, `MaxImagePixels`/`MaxPages`, `MaxInputBytes`) — под D-08 это упало бы паникой на старте. Они переезжают на `Replace` (по одной строке на воркер). Побочная выгода: «я знаю, что перезаписываю» становится читаемым в коде, а не в 25-строчном комментарии, и четвёртый воркер больше не может молча перехватить чужие пары.

- **D-10:** **`GET /v1/formats` получает новый ключ сразу, включая `convert`** — полное дерево `{operation → engine → pairs}`. Ключ `engines` остаётся байт-идентичным (группировка по парам с `Operation == "convert"`). Клиент и оба MCP-входа, которых D-05 учит про `operation`, могут открыть список операций уже сейчас, а не угадывать его. Guarded-на-непустоту вариант отвергнут: тогда параметр из D-05 некуда документировать.

### Роли выходов

- **D-11:** Отчёт попадает в ответ **массивом `outputs[]`** — все выходы с `{role, ordinal, format, size_bytes, url}`. `download_url` сохраняется и **всегда** указывает на выход с `role == "primary"`. Одна форма покрывает любое число ролей: `preview`/`thumbnail` в будущем не потребуют новых ключей. Плоский `report_url` отвергнут: каждая новая роль означала бы новый ключ, а ROLE-02 требует возвращать все выходы. Комбинация обоих отвергнута: два представления одних данных — два места, где они могут разойтись.

  **Следствие, не решение:** `outputs[]` появляется только при `len(outs) > 1` — иначе ломается SC4 («ответ одновыходной convert-джобы байт-идентичен»). Идиом уже установлен (`handlers.go:802-804`).

- **D-12:** **Eager-презайн всех выходов** тем же `presignTTL`. Один round-trip, клиенту нечего доспрашивать. Удвоение вызовов `PresignGet` касается только терминального состояния — презайн уже сегодня живёт внутри `case jobs.StatusDone`, опрос активной джобы не делает ни одного. Вариант «метаданные без url» отвергнут: потребовал бы строить новый endpoint за отчётом в этой же фазе.

- **D-13:** **Отсутствие primary-выхода у `done`-джобы становится недостижимым состоянием — инвариант на записи, а не проверка на чтении.** `MarkDone` отказывается коммитить переход в `done`, если у джобы нет ровно одного выхода с `role='primary'`, в той же транзакции, что и guarded-переход. API тогда не ветвится вообще.

  **Планировщику — осторожно:** проверка вписывается в **замыкание `MarkDone`**, НЕ в `Repo.transition` (PROJECT.md фиксирует транзиционную машинерию как неприкасаемый инвариант; 56-CONTEXT.md подтверждает: «transition machinery itself is untouched»). Пиннинг-тесты Phase 56 (`internal/worker/pinning_test.go`) должны пройти без изменений. Выполнимость проверена: `AddOutput` (`worker.go:1317`) уже предшествует `MarkDone` (`:1328`) во всех семи движках, каждый пишет ровно один выход, роль которого берётся из DB-дефолта.

- **D-14:** **Зафиксирован инвариант «один engine-класс обслуживает ровно одну операцию»**, закреплённый тестом по реестру. Благодаря ему остаются корректными по построению и НЕ меняются в этой фазе: роутинг реконсилятора по `job.Engine`, вывод unique-лок TTL, имена очередей, PromQL всех семи ScaledObject и квоты `client_limits(client_id, engine)` / `client_usage_daily(client_id, engine, day, status)`. Это закрывает все три подсистемы Pitfall 10 нулевым диффом вместо operation-aware переработки.

  **Проверено, а не предположено:** `default`-ветка реконсилятора (`reconciler.go:377-386`) уже fail-closed — незнакомый engine даёт метрику `unroutable_engine` и пропуск, никогда не догадку. Раз `probe` — свой engine-класс, misroute sanitize-джобы на convert-хендлер структурно невозможен уже сегодня.

  **Цена, принятая осознанно:** вторая probe-операция (SEED-013 «читающая» половина, `inspect`-операция на `probe`-движке) потребует либо своего engine-класса, либо явного снятия этого инварианта. Совпадает с уже задокументированным ограничением research §B: грань `client_usage_daily` не различает две операции одного движка.

### Миграция

- **D-15:** **Одна миграция фазы делает оба аддитивных шага:** `job_outputs.role text NOT NULL DEFAULT 'primary' CHECK (role IN ('primary','report','preview','thumbnail'))` и расширение `jobs_operation_check` до `sanitize` по отлаженному паттерну `jobs_engine_check` (`DROP CONSTRAINT` / `ADD CONSTRAINT`, как в `0005`/`0006`/`0009`/`0010`). Phase 60 тогда — чисто движок, очередь и контейнер, без возврата к схеме.

  `presets.operation` (идентичный CHECK, `0001_init.sql:22-23`) в этой фазе НЕ трогается — пресеты для sanitize никто не заказывал. Расхождение двух перечней зафиксировано здесь как известное и намеренное.

  Осознанное следствие: в базе появляется разрешённое значение, которое API отклоняет (реестр под него пуст, D-02). Расхождение без последствий, но видимое в схеме — планировщику стоит закрыть его комментарием в миграции.

- **D-16:** **DB-дефолты — источник истины, не Go-слой** (прямое следствие Pitfall 13). Тест обязан доказать, что строка, вставленная БЕЗ значения роли в Go-структуре, читается обратно как `primary`. Поведение `ADD COLUMN ... DEFAULT` для существующих строк должно быть **проверено на PostgreSQL 18**, а не выведено из общего знания о Postgres.

### Доказательная дисциплина

- **D-17:** **Golden-файлы коммитятся ПЕРВЫМ коммитом фазы** — точный шаг 1 протокола D-10 Phase 56. Отдельный коммит снимает сырые тела `GET /v1/formats` и `GET /v1/jobs/{id}` (одновыходная convert-джоба) с СЕГОДНЯШНЕГО кода в testdata, до того как ляжет хоть строка нового. Дальше тест сравнивает байты с файлом, а не с ожиданием, вписанным рукой уже после рефакторинга. Инлайн-ожидание в теле теста отвергнуто: ожидание писал бы тот же человек в тот же момент, что и изменение — ровно тот разрыв, который шаг 1 и закрывает.

- **D-18:** **Role-driven выбор доказывается обратным порядком плюс инверсией.** Тест вставляет выходы в «неправильном» порядке (`report` на ordinal 0, `primary` на 1) и требует, чтобы `download_url` разрешился в primary. Плюс инверсионный прогон по шагу 3 D-10: временно вернуть `outs[0]` — тест ОБЯЗАН упасть. Доказывает, что тест ловит именно этот класс дефекта, а не проходит по совпадению.

### Claude's Discretion

- Точное имя multipart-поля и константы (`formFieldOperation` — рабочее имя по образцу `formFieldTarget`/`formFieldPreset`).
- Точное имя нового ключа в `/v1/formats` (`operations` — рабочее имя из research §B, не финальное).
- Точное имя метода `Replace` (`Replace`/`MustReplace`/`Reregister`) и форма panic-сообщения `Register`.
- Форма и расположение golden-файлов в testdata.
- Нужен ли `role` в `ORDER BY` у `Repo.Outputs` как детерминирующий tiebreaker в дополнение к role-driven выбору в Go.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Roadmap & requirements
- `.planning/ROADMAP.md` §"Phase 57: Operation Dimension & Output Roles" — goal, 4 success criteria, constraint «ROLE-02 обязан закрыться ЗДЕСЬ», Decision locked 2026-08-07 (`sanitize` как новое значение `jobs.operation`, движок `probe`, отказ от переиспользования `extract`, расхождение с формулировкой SEED-013)
- `.planning/REQUIREMENTS.md` §OPRN (OPRN-01..03), §ROLE (ROLE-01..02) — полный текст требований
- `.planning/PROJECT.md` — рамка v1.13, архитектурные инварианты (engine-class queue routing, single-input/output, неприкасаемость guarded transitions), ретроспектива v1.12 с отказом от SEED-010

### Prior phase context (locked, не пересматривать)
- `.planning/phases/56-jobrunner-consolidation-structured-error-codes/56-CONTEXT.md` — D-01/D-02 (конверт и гранулярность кодов ошибок), D-03 (единое пространство sync/async кодов), D-06 (`internal/errcode` как источник истины + тест Go↔doc), D-10 (трёхшаговый протокол доказательства нулевого изменения поведения)
- `docs/ERROR-CODES.md` — замороженный перечень кодов; `unsupported_operation` добавляется сюда

### Research
- `.planning/research/ARCHITECTURE.md` §B "The `operation` dimension" — подтверждённые значения CHECK, доказательство что `operation` уже сквозной на уровне персистентности (единственное место записи `handlers.go:731`), шаблон `OperationalConverter`, обратная совместимость `/v1/formats`, доказательство operation-агностичности admission-гейта и `client_usage_daily`
- `.planning/research/ARCHITECTURE.md` §C "Output roles" — текущая схема `job_outputs`, минимальная аддитивная миграция, обратная совместимость `handleGetJob`, верификация байт-учёта
- `.planning/research/PITFALLS.md` Pitfall 3 (`outs[0]` не role-aware — живой дефект), Pitfall 9 (`sanitize` не в CHECK, конфликт словаря с SEED-013), Pitfall 10 (реестр/реконсилятор/TTL не operation-aware), Pitfall 13 (расхождение API-дефолта и DB-дефолта)

### Live code touchpoints (проверено в ходе обсуждения)
- `internal/convert/convert.go:110-172` — `Registry`, `Register` (голое присваивание, last-write-wins), `Lookup`/`Supports`/`EngineFor`/`Classes`, `Default`
- `internal/convert/converters.go` — семь регистраций и три задокументированных перерегистрации-плейсхолдера
- `internal/convert/progress.go:39-45` — `ProgressConverter`, прецедент optional-интерфейса
- `internal/convert/pairs_disjoint_test.go` — четыре существующих теста дизъюнктности пар
- `internal/api/handlers.go:33` (`operationConv`), `:133-135` (form-поля), `:397` (`EngineFor` перед `Upload`), `:840-878` (`handleGetJob`, безусловный `outs[0]`)
- `internal/api/formats_handlers.go` — рендер `{"engines": ...}`
- `internal/jobs/repo.go:534-537` (`Outputs`, `ORDER BY ordinal`, роль не селектится), `:410-419` (`AddOutput`)
- `internal/worker/worker.go:1317` (`AddOutput`) / `:1328` (`MarkDone`) — порядок, на котором держится D-13
- `internal/worker/pinning_test.go` — пиннинг Phase 56, должен пройти без изменений
- `internal/reconciler/reconciler.go:359-386` — engine-switch с fail-closed `default`
- `internal/db/migrations/0001_init.sql:22-23,45-48` — оба CHECK; `0009_ocr_engine.sql`/`0010_pandoc_engine.sql` — паттерн расширения CHECK; `0012_output_identity_and_retention_index.sql` — `UNIQUE (job_id, ordinal)`

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/convert/progress.go:39-45` `ProgressConverter` — точный шаблон для `OperationalConverter`: второй optional-интерфейс, type-assert на стороне потребителя, fallback при отсутствии. Реализуют двое из семи, остальные не знают о его существовании.
- `internal/worker/runner.go` + `EngineDescriptor` (Phase 56) — `process()` уже единственная точка, читающая джобу; чтение `job.Operation` добавляется в одном месте, а не в семи.
- `handlers.go:802-804` (`resp["opts"]` при непустоте) и `:872-877` (`error_code`/`error_message` при непустоте) — установленный идиом «аддитивный ключ, guarded на непустоту», под который ложится `outputs[]`.
- `internal/errcode` (Phase 56) — типизированные константы sync-кодов; `unsupported_operation` добавляется туда одной строкой.
- `internal/jobs/usage.go:59-60` — байт-учёт суммирует по всем выходам без предикатов по `ordinal`; роль тоже не потребует предиката (research верифицировал прямым чтением).

### Established Patterns
- **Compile-time closed allowlist вместо рантайм-открытия.** `AllEngines()`/`IsKnownEngine` с явным запретом нормализации (`convert.go:48-56`) — та же дисциплина применяется к операциям.
- **Расширение CHECK отдельной миграцией** `DROP CONSTRAINT` / `ADD CONSTRAINT` — доказано четырежды на `jobs_engine_check`.
- **Guarded status transitions** (`Repo.transition`) — неприкасаемы; D-13 добавляет предусловие в замыкание `MarkDone`, не в саму машинерию.
- **Дизъюнктность пар доказывается тестом, а не рантаймом** — четыре теста в `pairs_disjoint_test.go`. D-08 усиливает это до рантайм-отказа, что является намеренным отходом от текущей практики.
- **Fail-closed по умолчанию в роутинге** — `default`-ветка реконсилятора даёт метрику и пропуск, никогда догадку.

### Integration Points
- `internal/api/handlers.go` — приём поля, нормализация, `EngineFor` → `EngineForOp`, `handleGetJob` role-driven выбор + `outputs[]`
- `internal/api/formats_handlers.go` + `Registry.Classes` — новый ключ дерева операций при байт-идентичном `engines`
- `internal/convert/convert.go` + `converters.go` — `OperationalConverter`, тройки в `Registry.m`, `Register`/`Replace`
- `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go` — по одной строке `Register` → `Replace`
- `internal/jobs/repo.go` — `Output.Role`, `role` в SELECT `Outputs`, `role` в `AddOutput` и его `ON CONFLICT ... DO UPDATE`, предусловие в `MarkDone`
- `internal/worker` — `Runner` читает `job.Operation` и передаёт в `LookupOp`
- `internal/mcpserver` (оба сервера) и `frontend/src/api.ts` — проброс опционального `operation`
- Новая миграция `00NN_operation_and_output_roles.sql`
- `docs/ERROR-CODES.md` — `unsupported_operation`

</code_context>

<specifics>
## Specific Ideas

- Ответ `GET /v1/formats` для существующего клиента должен совпадать с сегодняшним **побайтово**, и это проверяется файлом, снятым до изменений, а не ожиданием, написанным после.
- Отчёт sanitize никогда не должен иметь возможности оказаться в `download_url` — не «не оказывается сегодня», а «не может по построению»: сначала role-driven выбор, потом инвариант на записи, потом инверсионный тест, доказывающий, что защита живая.
- `Register` должен ронять сервис на старте при коллизии троек. Молчаливое перетирание чужих пар — то, что сегодня возможно и держится только на комментариях.

</specifics>

<deferred>
## Deferred Ideas

- **Deployment-profile awareness** — реестр отвечает на вопрос «что скомпилировано», а не «что развёрнуто». Инсталляция с частичным флотом (например, только AV-воркер) принимает джобу для неразвёрнутого движка: клиент получает 202, байты уходят в S3, квота списывается, задача лежит в очереди ~3 цикла unique-лока и валится непрозрачным `reconciler_exhausted` («recovery attempts exhausted»), не сообщающим настоящую причину. Разрыв существует с v1.2 и до сих пор не проявлялся, потому что все инсталляции разворачивают полный флот. Кандидаты: явный список включённых engine-классов в конфиге API с fail-closed 422 на этапе `EngineFor` (до S3) + фильтрация `/v1/formats` по нему. Проверить пересечение с `AllConvertQueues()` и `minReplicaCount`-хелпером чарта. — собственная фаза, не Phase 57.

- **`presets.operation` симметрично `jobs.operation`** — идентичный CHECK (`0001_init.sql:22-23`) в этой фазе не расширяется; пресеты для sanitize не заказаны. Расхождение двух перечней намеренное и здесь зафиксировано.

- **Вторая операция на одном engine-классе** (SEED-013 «читающая» половина: `inspect`-операция на `probe`-движке) — потребует либо своего engine-класса, либо явного снятия инварианта D-14, и упрётся в грань `client_usage_daily(client_id, engine, day, status)`, не различающую две операции одного движка.

- **`docs/openapi.yaml` enum + codegen (ERRC-V2-02)** — унаследовано из Phase 56, по-прежнему отложено; `unsupported_operation` добавляется только в `docs/ERROR-CODES.md`.

</deferred>

---

*Phase: 57-Operation Dimension & Output Roles*
*Context gathered: 2026-08-08*
