# Phase 56: JobRunner Consolidation & Structured Error Codes - Context

**Gathered:** 2026-08-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Все семь конверсионных хендлеров (`HandleImageConvert`, `HandleDocumentConvert`, `HandleHTMLConvert`, `HandleAudioConvert`, `HandleAVConvert`, `HandleOCRConvert`, `HandlePandocConvert`) исполняются одним дескриптор-управляемым `Runner` поверх `EngineDescriptor`, где per-engine различия (opts-парсер, terminal-классификатор, failure-code маппер, progress-конфиг, pre-convert guard) — данные, а не ветвления. Терминальный исход джобы эмитится ровно одним вызовом в ровно одном месте, структурно не может выстрелить до коммита guarded-перехода. Структурированные коды ошибок вводятся ОДИН РАЗ и полностью в этой фазе — обе половины: async job-failure taxonomy (уже существует, только консолидируется) и sync 4xx API-ответы (новый gap, ~60 `writeError` сайтов). `HandleWebhookDeliver` — вне скоупа, байт-неизменен.

**Не в скоупе:** operation dimension (Phase 57), output roles (Phase 57), inspect (Phase 58), sanitize (Phase 60), v1.12 debt tail — README/`PurgeUsage`/stuck-job (Phase 61).

</domain>

<decisions>
## Implementation Decisions

### Sync error envelope (API 4xx/429)
- **D-01:** Плоский конверт `{"error": msg, "code": "..."}` — тот же стиль, что уже используют `writeQuotaError` (`quota_exceeded`) и `internal/ratelimit` (`rate_limited`). Не вложенный `{error:{code,...}}` — фронт (`frontend/src/api.ts:54`) читает `body.error` как строку и не должен ломаться. Расширяемость (ERRC-01) — через дополнительные top-level поля позже (`retry_after`, `details`), не через смену формы.
- **D-02:** Гранулярность — по классу отказа, ~10-14 кодов на все ~60 `writeError`-сайтов в `internal/api/handlers.go` (и `jobs_list_handler.go`). Примерная таксономия: `invalid_request` (общая валидация формы: missing target, preset+target конфликт, unrecognized format и т.п. — НЕ дробится тоньше), `unsupported_conversion`, `invalid_options`, `opts_not_applicable`, `missing_file`, `unknown_preset`, `payload_too_large`, `not_found`, `invalid_cursor`, `quota_exceeded` (существует), `rate_limited` (существует), `internal_error`. Детали конкретного отказа остаются в `message`, не кодируются в отдельный код.
- **D-03:** Async `invalid_options` (код в `MarkFailed`) и sync `invalid_options` (422 при создании job с некорректными opts) — **одно и то же имя кода**, единое пространство кодов на весь сервис. Консьюмер видит один и тот же `invalid_options` независимо от того, поймал ли он его синхронно на 422 или асинхронно в `error_code` после отказа джобы.

### Async taxonomy (14 существующих кодов)
- **D-04:** Строки-значения существующих 14 async-кодов (`engine_error`, `invalid_options`, `duration_exceeded`, `timecode_out_of_range`, `resolution_exceeded`, `no_video_stream`, `born_digital`, `too_many_pages`, `page_too_large`, `image_too_large`, `unsupported_language`, `unsupported_target`, `input_too_large`, `reconciler_exhausted`) переносятся в `EngineDescriptor.FailureCode` **без изменений** при консолидации. Никакого rename/rationalization в этой фазе — MCP (`internal/mcpserver/client.go`) уже потребляет их структурно, лишний диф в потребителях не нужен (Pitfall 11 — vocabulary churn запрещён внутри милстоуна).
- **D-05:** Catch-all `engine_error`, используемый идентично семью движками с разными подразумеваемыми смыслами, остаётся как есть — переименование того же класса запрещено этой же заморозкой.

### Source of truth & enforcement
- **D-06:** Источник истины перечня кодов — Go-константы (новый пакет, например `internal/errcode`, с типизированными константами для sync-кодов; async-коды остаются строковыми константами рядом с `EngineDescriptor`, компилятор ловит опечатки). `docs/ERROR-CODES.md` генерируется/ведётся вручную из кода как человекочитаемый список для фронта и обеих MCP-команд; тест проверяет соответствие Go↔doc.
- **D-07:** `docs/openapi.yaml` как enum и codegen — явно ОТЛОЖЕНО, это ERRC-V2-02 (следующий шаг, не эта фаза).
- **D-08:** ERRC-03 (ни один потребитель не матчит текстом) и Pitfall-12 unknown-code fallback доказываются: (a) статический анализ/grep-проверка фронта и обоих MCP-серверов на отсутствие ветвления по тексту `error.message`/`error_message`; (b) отдельный тест-фикстур, посылающий заведомо неизвестный код и проверяющий, что консьюмер уходит в generic fallback, а не падает/бросает.

### EngineDescriptor form & pinning
- **D-09:** Набор `EngineDescriptor` — package-level переменные в `internal/worker` (`ImageDescriptor`, `DocumentDescriptor`, ... как `var` в `worker.go` или новом `descriptors.go` того же пакета). Каждый `cmd/*-worker/main.go` импортирует свой и собирает `worker.NewRunner(h, worker.ImageDescriptor)`. Согласуется с текущей моделью "один бинарь — один движок" (уже верифицировано: один `mux.HandleFunc` на бинарь). НЕ реестр `map[string]EngineDescriptor` — не нужна косвенность там, где сегодня прямые импорты.
- **D-10:** Доказательство zero-behavior-change (RUNR-03) и "структурно невозможно выстрелить до коммита" (SC4) — трёхшаговый протокол:
  1. Пиннинг-тесты пишутся/расширяются на СЕГОДНЯШНЕЙ семи-хендлерной сети ДО рефакторинга — покрывают противоположные полярности `isAudioTerminal`/`isAVTerminal`, все failure-code лейблы, метки `octoconv_job_duration_seconds{engine,status}`/`octoconv_queue_depth{queue,state}`.
  2. Рефакторинг в `Runner`/`EngineDescriptor` — те же тесты должны пройти без изменений (не переписываются под новую форму).
  3. Инверсионный прогон — временно переносится вызов metrics/webhook-эмиссии ДО коммита guarded-перехода, тест обязан упасть. Доказывает, что тест действительно ловит этот класс дефекта, а не проходит по случайности.

### Reconciler fix (RUNR-02)
- Фикс `internal/reconciler/reconciler.go:311-312` — `RecordReconcilerAction("exhausted")` сегодня инкрементится безусловно, не guarded на `ferr` от `MarkFailed`. Тот же класс дефекта, что и в семи хендлерах, чинится той же дисциплиной (одна метрика, один guarded call site) в этой фазе.

### Claude's Discretion
- Точное имя и путь нового пакета для sync error-code констант (`internal/errcode` — рабочее название, не финальное решение).
- Точный список ~10-14 sync-кодов — стартовый список в D-02 ориентировочный; финализация при маппинге всех ~60 `writeError`-сайтов допустима в рамках заданной гранулярности "по классу отказа".
- Форма `docs/ERROR-CODES.md` (таблица vs список) — Claude решает формат при написании.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Roadmap & requirements
- `.planning/ROADMAP.md` §"Phase 56: JobRunner Consolidation & Structured Error Codes" — goal, success criteria (5 SC), scope note, cost-correction note
- `.planning/REQUIREMENTS.md` §RUNR (RUNR-01..03), §ERRC (ERRC-01..03) — full requirement text
- `.planning/PROJECT.md` — v1.13 milestone framing, architectural constraints (single-input/output, engine-class queue routing)

### Research (already synthesized for this phase)
- `.planning/research/ARCHITECTURE.md` §A "JobRunner consolidation (SEED-020)" — verified handler line-counts, per-engine difference table, proposed `EngineDescriptor`/`Runner` signature, post-commit-hook pattern, tests-that-must-not-change list
- `.planning/research/ARCHITECTURE.md` §F "Structured error codes" — where fixed strings live today (sync vs async), correction that async is already structured/MCP-consumed, the narrower sync gap, `invalid_options` metrics gap verification
- `.planning/research/ARCHITECTURE.md` §G "Suggested build order" — Phase 56 scope boundary vs Phase 57+
- `.planning/research/PITFALLS.md` Pitfall 2 (JobRunner collapses stage-based asymmetries), Pitfall 4 (metric-label drift breaks KEDA), Pitfall 11 (error-code vocabulary churn within milestone), Pitfall 12 (exhaustive-match consumer breakage), Pitfall 14 (test suite proves runner shape not pre-existing behavior — CI vs live-gate gap)
- `.planning/research/SUMMARY.md` — cross-cutting synthesis (if referenced further during planning)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/worker/worker.go:1847-1942` `process()` — already the single shared orchestration function every handler calls; the runner wraps this unchanged.
- `internal/api/handlers.go:1006-1010` `writeQuotaError` — existing precedent for the `{"error":msg,"code":...}` shape; new sync error-code work should follow this exact pattern, not invent a new one.
- `internal/mcpserver/client.go:100-101` — `ErrorCode`/`ErrorMessage` typed DTO fields already consume the async path structurally; no MCP-side async migration needed, only verification.

### Established Patterns
- Guarded status transitions (`internal/jobs/repo.go` `Repo.transition`) — the runner only reorders/consolidates calls into this; transition machinery itself is untouched (explicit PROJECT.md invariant).
- One `Handler` / one `Handle*Convert` mux registration per `cmd/*-worker/main.go` binary — the deployment model the package-level-descriptor decision (D-09) relies on.
- `job.Engine` is byte-identical to `queue.QueueX` by construction (`internal/queue/queue.go:38-45`) — the runner body can drop `queue.QueueX` literals entirely.

### Integration Points
- `internal/reconciler/reconciler.go:311-312` — the unguarded `RecordReconcilerAction("exhausted")` fix (RUNR-02) touches this file, not `internal/worker`.
- `internal/api/handlers.go` (~60 `writeError` call sites) and `internal/api/jobs_list_handler.go` (4 call sites) — the sync-half error-code sweep.
- `cmd/worker/main.go`, `cmd/document-worker/main.go`, `cmd/chromium-worker/main.go`, `cmd/audio-worker/main.go`, `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go` — each swaps its `mux.HandleFunc(queue.TypeXConvert, h.HandleXConvert)` registration for `worker.NewRunner(h, worker.XDescriptor).Handle`.

</code_context>

<specifics>
## Specific Ideas

No specific UI/UX references — this is an internal refactor + API contract phase. Key concrete asks from discussion:
- Error envelope must look exactly like the existing `writeQuotaError` shape (`{"error": ..., "code": "quota_exceeded"}`), just generalized to every 4xx.
- `docs/ERROR-CODES.md` must exist as a deliverable of this phase (freeze point per Pitfall 11), even though publishing into `docs/openapi.yaml` (ERRC-V2-02) is explicitly deferred.

</specifics>

<deferred>
## Deferred Ideas

- **`docs/openapi.yaml` error-code enum + codegen (ERRC-V2-02)** — explicit next-step, not this phase.
- **Existing 429 envelope migration to a new wrapper form (ERRC-V2-01)** — not blocking; `quota_exceeded`/`rate_limited` already machine-readable in the chosen flat shape, so no rework needed even under D-01.
- **v1.12 debt tail (stuck in-flight job relief, README quota section fix, `PurgeUsage` Postgres-clock fix)** — belongs to Phase 61, zero file overlap by design, not folded here despite research noting it could ride along with any lightest-diff phase.
- **Operation dimension / output roles / registry `OperationalConverter`** — Phase 57, hard dependency direction confirmed (`process()` needs to read `job.Operation` from one consolidated place first).

### Reviewed Todos (not folded)
None — `gsd-sdk query todo.match-phase 56` returned zero matches.

</deferred>

---

*Phase: 56-JobRunner Consolidation & Structured Error Codes*
*Context gathered: 2026-08-08*
