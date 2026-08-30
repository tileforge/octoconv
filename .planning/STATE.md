---
gsd_state_version: 1.0
milestone: v1.13
milestone_name: Inspect & Operations
status: completed
stopped_at: Phase 59 complete; nothing pushed yet
last_updated: "2026-08-30T05:21:45.770Z"
last_activity: 2026-08-30 -- Phase 59 executed (7 plans) and verified (6055a56)
progress:
  total_phases: 6
  completed_phases: 4
  total_plans: 29
  completed_plans: 29
  percent: 67
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-07 after v1.13 milestone start)

**Core value:** Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла (изображения, офисные документы, HTML, аудио, видео, OCR, markup) и получить результат — без риска для стабильности или безопасности продакшена.
**Current focus:** Phase 59 — born-digital-pdf-routing

## Current Position

Phase: 59 (born-digital-pdf-routing) — COMPLETE
Plan: 7 of 7 — all executed and verified
Status: Phase 59 complete: BDPD-01 and BDPD-02 both satisfied with live evidence, 7 plans executed. Next in v1.13: Phase 60 (Sanitize) and Phase 61 (v1.12 debt tail).
Last activity: 2026-08-30 -- Phase 59 executed (7 plans) and verified (6055a56)

## Performance Metrics

**Velocity:**

- Total plans completed: 153 (all v1.0–v1.11)
- Average duration: - min
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 4 | - | - |
| 02 | 3 | - | - |
| 03 | 3 | - | - |
| 04 | 5 | - | - |
| 05 | 1 | - | - |
| 06 | 4 | - | - |
| 07 | 2 | - | - |
| 08 | 2 | - | - |
| 09 | 2 | - | - |
| 10 | 4 | - | - |
| 11 | 4 | - | - |
| 12 | 1 | - | - |
| 13 | 3 | - | - |
| 14 | 3 | - | - |
| 15 | 5 | - | - |
| 16 | 5 | - | - |
| 17 | 2 | - | - |
| 18 | 4 | - | - |
| 19 | 2 | - | - |
| 20 | 2 | - | - |
| 21 | 3 | - | - |
| 22 | 2 | - | - |
| 23 | 3 | - | - |
| 26 | 2 | - | - |
| 27 | 3 | - | - |
| 28 | 3 | - | - |
| 29 | 3 | - | - |
| 30 | 3 | - | - |
| 31 | 4 | - | - |
| 32 | 5 | - | - |
| 33 | 3 | - | - |
| 34 | 3 | - | - |
| 35 | 7 | - | - |
| 36 | 5 | - | - |
| 37 | 3 | - | - |
| 38 | 1 | - | - |
| 39 | 4 | - | - |
| 40 | 2 | - | - |
| 41 | 4 | - | - |
| 42 | 4 | - | - |
| 43 | 4 | - | - |
| 44 | 4 | - | - |
| 45 | 4 | - | - |
| 46 | 3 | - | - |
| 47 | 3 | - | - |
| 48 | 3 | - | - |
| 49 | 4 | - | - |
| 50 | 4 | - | - |
| 51 | 5 | - | - |
| 52 | TBD | - | - |
| 53 | 7 | - | - |
| 54 | 2 | - | - |
| 55 | 7 | - | - |
| 56 | 7 | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
| Phase 55 P05 | 15min | 2 tasks | 2 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

**v1.13-specific roadmap decisions (recorded at roadmap creation, 2026-08-07):**

- **Phase numbering continues from v1.12's Phase 55 — this milestone is Phases 56-61**, not a restart at 1.
- **`inspect` is a SYNCHRONOUS `POST /v1/inspect`, not a job.** The whole point is deciding *before* queuing an expensive job. The cost was accepted knowingly: the path bypasses quotas, accounting, the reconciler, webhooks and SSE, so it must carry its own bounds (INSP-05) and prove process isolation with a live gate (INSP-06) rather than assume it.
- **`sanitize` is a NEW `jobs.operation` value, engine `probe`** — not a reuse of `extract`. The verb names what it does; `extract` would lie (sanitize extracts nothing) and would consume a value needed on its merits in the Document wave. This **supersedes SEED-013's framing** (`engine=probe`, `operation=inspect|convert`), which two independent researchers flagged as inconsistent with the milestone description.
- **The whole error-code contract lands in ONE phase (56), including the 4xx half in `internal/api/handlers.go`** — splitting it would make two MCP servers and the frontend migrate twice inside one milestone (PITFALLS.md Pitfall 11).
- **ROLE-02 must close in Phase 57, before sanitize ships in 60.** `handleGetJob` takes `outs[0]` with no role awareness (`internal/api/handlers.go:857-870`, verified live) — the defect is already in the code, merely unreachable until a second output exists.
- **INSP-06's live gate lives INSIDE Phase 58, unlike QUOT-05 which was its own phase (55).** The gate proves a property of code written in the same phase, with no intervening phase able to change it — matching Phase 51 Plan 05's precedent (operator-run KEDA gate inside the phase that built the metrics). If the gate fails, the fix is likely a design change (moving deep analysis to a worker), so the phase should not close until the design is proven.
- **The three DEBT items are their own phase (61), not folded into 56-60** — zero file overlap, and folding them would add unrelated diff noise to the main work's review. Phase 54 (CAPA-01) precedent.
- **Research corrections carried into the roadmap:** SEED-020 overstated its own cost (handler bodies are 79–110 lines, not 94–163; `process()` at worker.go:1847-1942 is already the shared orchestrator; av's disk guards and ocr's page caps are converter-internal, not handler-level). Its "14 symmetric call sites" claim is exactly correct. Adding an eighth queue is **low** blast radius (each ScaledObject's PromQL is self-contained; proven twice in v1.10/v1.11) — *renaming* one is the expensive operation, and v1.13 requires none. The real KEDA risk here is metric-label drift during the refactor.
- **Coverage: 28/28 v1 requirements mapped, 0 orphans** — see REQUIREMENTS.md Traceability table.

**v1.12-specific roadmap decisions (recorded at roadmap creation, 2026-08-02):**

- **Phase numbering continues from v1.11's Phase 50 — this milestone is Phases 51-55**, not a restart at 1.
- **Attribution before enforcement before visibility (Phase 51 → 52 → 53), mirroring the milestone's own framing** ("Атрибуция" before "Границы"): quotas can't be enforced against numbers that aren't tracked yet, and the operator endpoint has nothing to show without the rollup existing first.
- **Quota enforcement and baseline (admission-control) fairness are ONE phase (52), not two**: FAIR-01's per-client concurrency cap is the identical mechanism QUOT-01..04 already need (Q1/Q3 = Q4 option A in ARCHITECTURE.md) — building them separately would duplicate the admission-cap machinery for no benefit. FAIR-02 (queue_depth/PromQL invariance) is the phase's own tripwire, not a separate feature.
- **RETN-01 folded into Phase 51 (Attribution), not its own phase**: the requirement text itself frames retention as a precondition ("строить учёт поверх неограниченно растущих таблиц значит повторить уже имеющийся долг") — the new `client_usage_daily` table must state its retention/rollup design in the same migration that creates it, and the pre-existing `jobs`/`job_events` unbounded-growth gap gets an explicit policy in the same phase rather than being silently repeated a third time.
- **QUOT-05 (multi-replica correctness) is its own live-gate phase (55), separate from the phase that builds the mechanism (52)**: the atomic Postgres admission design is a Phase 52 implementation constraint (must be correct under N replicas by construction), but QUOT-05 as a requirement is specifically the *proof* — `api.replicas: 2+`, live-observed — matching this project's own precedent for claims that can't be proven statically (KEDA scale-from-zero/downscale-survival gates) and directly answering `internal/ratelimit`'s own documented `replicas: 1`-only caveat, which the milestone explicitly must not repeat for quotas.
- **CAPA-01 (SEED-011, minReplicaCount) is its own small phase (54), not folded into 51-53**: zero data/code coupling with the accounting/quota work (pure Helm values/`ScaledObject` change) — keeping it separate avoids unrelated chart-diff noise in the accounting/quota review and lets it ship/rollback independently; sequenced as a low-risk hygiene tail per research, not blocking or blocked by anything.
- **Per-client sub-queue fairness (the design that renames queues and touches all 7 ScaledObject PromQL strings) is explicitly NOT in this roadmap** — deferred (FAIR-V2-01 in REQUIREMENTS.md v2), evidence-gated on Phase 53's operator visibility showing the admission-cap baseline is actually insufficient in practice. Building it speculatively would repeat the SEED-010 rejection pattern already recorded in PROJECT.md, this time risking the KEDA scale-from-zero contract instead of the compile-time engine allowlist.
- **Coverage: 15/15 v1 requirements mapped, 0 orphans** — see REQUIREMENTS.md Traceability table.
- [Phase 55]: internal/jobs/admission.go records that quota admission carries no api.replicas:1-only caveat, citing live gate run 20260807T153358Z (10/10 rounds, 167 assertions) by path — Closes ROADMAP Phase 55 SC2 and QUOT-05; mirrors internal/ratelimit's own caveat register, inverted

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260712-cqg | Fix Phase 16 verification gaps CR-01/WR-01: advisory-lock connection lifecycle (Release pool slot on TryAcquire error; add PGAdvisoryLock.Close and call at webhook-worker shutdown) | 2026-07-12 | 1f8b22b | [260712-cqg-fix-phase-16-verification-gaps-cr-01-wr-](./quick/260712-cqg-fix-phase-16-verification-gaps-cr-01-wr-/) |
| 260719-2f5 | OpenAPI 3.1 спека API (docs/openapi.yaml, 14 операций, ApiKey auth, multipart POST /v1/jobs с аудио-примерами) для импорта в Yaak + docs/README.md; redocly lint 0 errors | 2026-07-19 | 43799af | [260719-2f5-openapi-3-1-api-octoconv-docs-yaak](./quick/260719-2f5-openapi-3-1-api-octoconv-docs-yaak/) |
| 260724-26g | Актуализация README под shipped v1.0–v1.8 (5 движков image/document/html/audio/video, presets + /v1/formats, PDF/A+veraPDF, CFB, k8s/Helm/KEDA, MCP; cmd/ дерево 12 бинарников, config-таблица, Roadmap, Contributing) | 2026-07-23 | 50bade0 | [260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8](./quick/260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8/) |
| 260728-0ay | Модернизация frontend SPA под API v1.10: динамические форматы из /v1/formats (6 движков), живой SSE-прогресс видео (fetch+ReadableStream, progress bar + ETA) с polling-фолбэком, статусы active/canceled, кнопка Cancel, hint про localhost:9100 для download. `npm run build` зелёный. | 2026-07-28 | b3351d0 | [260728-0ay-modernize-frontend-spa-to-api-v1-10-with](./quick/260728-0ay-modernize-frontend-spa-to-api-v1-10-with/) |
| 260802-9de | Закрытие оставшихся замечаний code review PR#2: потолок pandoc-аплоада в API теперь берётся из PANDOC_MAX_INPUT_BYTES (оверсайз отклоняется на входе, а не падает асинхронно в воркере), и обрезка неполной UTF-8-руны перед валидацией sniff-префикса (файлы >1 MiB больше не отклоняются ложно) | 2026-08-02 | fbc51ea | [260802-9de-close-remaining-pr-2-review-findings-enf](./quick/260802-9de-close-remaining-pr-2-review-findings-enf/) |
| 260802-9zc | Устранён последний экземпляр PR#2 finding-2: LooksLikeHTML держал собственную копию логики sniff-префикса и потому не получил фикс обрезки неполной UTF-8-руны — HTML-файлы >1 MiB с руной на границе кэпа отклонялись ложно. Копия заменена вызовом общего readSniffPrefix | 2026-08-02 | fff79da | [260802-9zc-fix-the-same-mid-rune-utf-8-truncation-i](./quick/260802-9zc-fix-the-same-mid-rune-utf-8-truncation-i/) |
| 260802-asr | Три фронтенд-бага из ревью PR#2: терминальный SSE-кадр отбрасывал финальный GET и готовая конверсия оставалась без ссылки Download (job?.status убран из deps эффекта, запись гейтится по identity джобы); finalize получил bounded retry (3 попытки, backoff 400/1200мс, только 5xx/429/408/сеть); селектор таргетов фильтруется по формату выбранного файла с зеркалом NormalizeFormat (jpeg/tif/htm/tex) вместо глобального объединения, дававшего избегаемые 422. Добавлен vitest, 23 теста | 2026-08-02 | d41593d | [260802-asr-fix-three-frontend-bugs-found-in-pr-2-re](./quick/260802-asr-fix-three-frontend-bugs-found-in-pr-2-re/) |
| 260809-ul4 | Четыре замечания codex из ревью PR#7. Функциональные: (1) `HandleWebhookDeliver` презайнил `outs[0]` — тот же дефект, что ROLE-02 закрыл для `GET /v1/jobs/{id}`, но callback-путь не входил в букву требования, поэтому верификатор фазы 57 его пропустил; литерал `"primary"` поднят в `jobs.OutputRolePrimary` как единственное объявление на `api` и `worker`; (2) `mcpserver.FormatsResponse` терял ключ `operations` при декодировании, хотя `tools.go` в jsonschema отсылал агентов именно к нему. Контрактные: (3) `docs/openapi.yaml` не объявлял `operation` при `additionalProperties: false` (SDK не мог отправить поле), `operations` и `outputs`; (4) спека утверждала «code is never added to any non-429 response», что стало ложью после фазы 56 — `Error.code` теперь required с enum по закрытому словарю, `QuotaError` удалён как избыточный, а `internal/errcode/openapi_sync_test.go` механически стережёт спеку от дрейфа (раньше `TestErrorCodeDocSync` стерёг только ERROR-CODES.md). Инверсия вебхук-фикса воспроизведена независимо | 2026-08-09 | 032cc78 | [260809-ul4-4-codex-pr-7-role-driven-download-url-op](./quick/260809-ul4-4-codex-pr-7-role-driven-download-url-op/) |
| 260817-j58 | Закрыта находка codex из ревью PR#9 (`container.go:894`): `countSheetElements` принимал XML с несколькими top-level элементами — `encoding/xml` не считает второй корень ошибкой и отдаёт его как обычный элемент, поэтому `<workbook>…</workbook><ext><sheets><sheet/></sheets></ext>` публиковал `sheet_count: 2` с полным покрытием для не-well-formed XML. Поведение stdlib сначала **измерено**, а не предположено: `xml.Unmarshal` принимает то же самое (оракула для делегирования нет), а текст ДО корня не отвергает вообще ничто в stdlib — поэтому чинится класс целиком: ровно один корень + запрет непробельных CharData по обе стороны от него. Три теста: 7 случаев класса с проверкой причины `malformed_input` (не потолка), гард от чрезмерного отклонения (пролог/DOCTYPE/комментарии/PI/whitespace вокруг единственного корня), и тест-пин измеренной снисходительности `encoding/xml` | 2026-08-17 | aeb0768 | [260817-j58-close-codex-pr9-finding-xlsxsheetcount-a](./quick/260817-j58-close-codex-pr9-finding-xlsxsheetcount-a/) |

| 260809-fast | codex, второй круг ревью PR#7: `mcpserver.JobStatus` не объявлял `Outputs`, поэтому массив `outputs` из `GET /v1/jobs/{id}` молча терялся у обоих MCP-серверов. `JobStatus` служит типом вывода `get_job_status`, а `download_result` отдаёт только primary-URL — значит вторичный артефакт (отчёт sanitize) был недостижим через MCP вообще. Тот же класс, что находка №2 про `FormatsResponse`: соседний DTO в том же файле, который узкая правка не подмела. Латентно до фазы 60. Тег-инверсия (`outputs`→`output`) доказывает, что тест ловит потерю данных, а не только отсутствие поля | 2026-08-09 | 23bf611 | — |
| 260830-6zr | Автотриггеры `on: [push, pull_request]` в `.github/workflows/ci.yml` закомментированы, заменены на `on: workflow_dispatch` — лимиты GitHub Actions на исходе, а воркфлоу срабатывал дважды за цикл (пуш ветки + открытие PR). Ключ `on:` не удалён намеренно: он обязателен, и файл без него GitHub считает невалидным воркфлоу, что вдобавок лишает ручного запуска. Все пять job'ов на месте, откат — одна строка. Заодно найдено и починено: планы 59-02/59-05 в шагах verify звали `python3 -c "import yaml"`, а pyyaml на этой машине нет — переведены на `yq` v4 — он стоит в проекте и им же пользуются рабочие процессы GSD; `yq '.on'` вдобавок не спотыкается о YAML 1.1, где голое `on:` разбирается как булев true, а поскольку он умеет запрашивать, критерии приёмки усилены с «файл парсится» до проверки значений | 2026-08-30 | a13e67b | [260830-6zr-ci-workflow-dispatch](./quick/260830-6zr-ci-workflow-dispatch/) |

### Pending Todos

- api container crash-loops at every fresh k8s install until postgres accepts connections (`log.Fatalf` as the dependency-wait mechanism; restartCount reaches 2-3 before Ready). Observed while running the INSP-06 gate. See `.planning/todos/pending/2026-08-18-api-startup-crashloop-until-postgres.md`.
- Inspect honest-reporting: three open items from the PR #9 review loop (coverage-vs-read_failed contract; bool-returning detectors that cannot report a read failure; CI never run since 2026-08-16 due to GitHub Actions billing). See `.planning/todos/pending/2026-08-18-inspect-honest-reporting-open-items.md`. Rounds 20-27 are all closed in code; these three are contract/API/account decisions, not review-round patches.
- Plan and execute Phase 53 (Operator Usage Visibility: OPSV-01/02) — reads Phase 51's rollup table, sequenced after Phase 52's quota mechanism so "usage vs. limit" is meaningful.
- Fix Retry-After midnight race in daily quota rejection — `quotaRejection` can advertise ~86400s when the daily quota already reset; needs a sentinel-error contract change, deliberately deferred out of PR #5. See `.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md`.

### Blockers/Concerns

None currently blocking. **v1.13 sequencing and risks carried into the roadmap:**

- Hard-ordered spine: **56 → 57 → 58 → 59**, and **56 + 57 → 60**. Phase 61 (debt tail) is fully independent and can execute in any slot.
- **Highest-blast-radius risk is Phase 56, and it is not the refactor itself — it is metric-label drift.** The phase touches all 14 metric call sites in the same milestone that adds an `operation` dimension; the natural "let's add an `operation` label while we're here" impulse would break KEDA scale-from-zero for all seven existing engine classes. `TestJobDurationLabelsPinned`/`TestQueueDepthLabelsPinned` must stay green, verified by diff, mirroring FAIR-02's v1.12 method.
- **Second-highest risk in Phase 56:** `isAudioTerminal` and `isAVTerminal` encode stage-level asymmetries with **opposite polarity from each other**, deliberately. The descriptor must model `(stage, isTimeout)`, not `(engine, isTimeout)`, or the refactor silently regresses the two costliest engine classes. A test suite that proves the runner's shape rather than the seven engines' pre-existing behavior would pass while regressing.
- **Highest operational risk is Phase 58:** the synchronous inspect handler shares a process with `/healthz`. A pathological burst can fail the k8s liveness probe and cascade a fleet-wide restart, not merely slow one client. This is why INSP-06 is an operator-gated live proof.
- **Highest security bar is Phase 60.** Verified against real sources: PDF incremental updates leave "removed" objects byte-recoverable in an earlier revision; naive OOXML zip-entry deletion leaves dangling `[Content_Types].xml`/`.rels` references or leaves payload bytes physically present; self-verification with the removing parser is structurally blind to polyglot bypasses (CVE-2015-5092 precedent). Apply the project's own veraPDF precedent — verify with an independent implementation.
- **Two design decisions deliberately left open for Phase 60 planning** (research could not close them honestly): blacklist-removal vs reconstruct-from-trusted-content, and the independent verification parser choice. Both must be written decisions before code lands.
- **Operator-gated phases:** 58 (live process-isolation proof) and 60 (measured `probe` engine timeout on its own corpus — copying a sibling's timeout is a documented project anti-pattern; every class has a distinct value: 65s pandoc, 742s audio, 744s ocr, 753s av).
- **Explicit non-goals for v1.13 (do not resurface):** SEED-010 dynamic capability registry (third rejection), SEED-021 Opportunity 2 (per-page Mixed-PDF routing — unresolved merging contract), rasterize-and-rebuild sanitize, poppler/qpdf/mutool/ffprobe/exiftool in `Dockerfile.api`, renaming any existing queue, SEED-017/SEED-018.
- **Вынос `inspect` в отдельный воркер — закрытый вопрос, не поднимать заново (решено 2026-08-10).** Вариант рассматривался явно на обсуждении фазы 58 и отвергнут: он снимает риск для `/healthz` полностью, но ломает то единственное свойство, ради которого фаза существует — ответ за один синхронный round-trip ДО постановки дорогой джобы. Inspect, ушедший в очередь, — это уже джоба. Соблазн будет возвращаться каждый раз, когда синхронный путь окажется чем-то ограничен (потолком работы, семафором, честным `unknown` вместо ответа): правильная реакция — уточнять границы, а не переносить путь. Отдельный воркер `cmd/probe-worker` в планах есть (SANI-06, фаза 60), но он про sanitize — ПИШУЩУЮ половину; читающая половина на том же движке отложена в v2 как SANI-V2-02 с записанной причиной (гранулярность `client_usage_daily` — `(client_id, engine, day, status)` без операции, две probe-операции стали бы неразличимы в учёте).
- Operational discipline (OrbStack) carried forward: pre-build all images sequentially with non-`latest` tags; never run compose and k8s stacks hot simultaneously (four confirmed daemon wedges on record) — relevant again for Phase 58's and Phase 60's live runs.

Carried from v1.12:

- Hard-ordered spine for the accounting/quota work: 51 (attribution + retention — new rollup table + client-labeled counter, additive/zero-behavior-change) → 52 (quota enforcement + baseline fairness, same mechanism — needs Phase 51's table) → 53 (operator visibility — reads Phase 51's table, sequenced after 52 starts so "usage vs. limit" is meaningful).
- Phase 54 (CAPA-01/SEED-011) is fully independent — chart-only, can execute at any point; placed as a hygiene tail so it doesn't add unrelated diff noise to 51-53's review.
- Phase 55 is an operator-gated live proof (`autonomous: false`) — depends on Phase 52's atomic mechanism existing; needs a real `api.replicas: 2+` run, cannot be closed by static verification.
- Highest-risk phase is 52: three-tier admission gate (concurrency/byte-budget/atomic-reserve) touching `handleCreateJob`, both MCP entry points, and the reconciler's release-on-every-terminal-state discipline — PITFALLS.md documents 10 pitfalls anchored to this exact area (cardinality, replica-races, reconciler double-count, rollout safety, KEDA/fairness conflation, 429-vs-403).
- Explicit non-goals for this milestone (do not resurface): per-client sub-queues/dynamic virtual queues (breaks all 7 ScaledObject PromQL strings + `AllConvertQueues()` + reconciler routing — FAIR-V2-01), billing/invoicing, self-service quota portal, ML/anomaly throttling, formal SLA tiers.
- Operational discipline (OrbStack) carried forward: pre-build all images sequentially with non-`latest` tags; never run compose and k8s stacks hot simultaneously (four confirmed daemon wedges on record, per v1.6/v1.7/v1.8 history) — relevant again for Phase 55's live multi-replica run.

## Deferred Items

Items acknowledged and deferred at v1.12 milestone close on 2026-08-07. Milestone audit status
`tech_debt` — 15/15 requirements, 5/5 phases, 9/9 cross-phase seams, 1/1 E2E flow, 0 blockers
(`.planning/milestones/v1.12-MILESTONE-AUDIT.md` has the itemized list):

| Category | Item | Status |
|----------|------|--------|
| tech_debt | **52 WR-01** — an in-flight (`queued`/`active`) job charges the daily cap with no time bound, so a permanently stuck job locks out a client's daily budget every subsequent UTC day; no operator remedy exists in `cmd/manage-clients` (raw SQL only), and the new coupling between quota availability and `cmd/webhook-worker`'s health is undocumented | Accepted, non-blocking — the unavoidable price of making the daily cap real; self-heals via the reconciler in a correctly operated deployment. **Highest-value follow-up in the milestone** |
| tech_debt | 52 WR-09 — `README.md:242` claims enforcement happens "before the upload touches storage", contradicted by `README.md:294`; `README.md:257` hardcodes "the seven known engine classes" instead of deriving from `convert.AllEngines()` | Open — documentation accuracy only, shipped behavior is correct |
| tech_debt | 52 WR-03/WR-04 — migration 0014 uses `DROP CONSTRAINT IF EXISTS` against repo convention with no exactly-once CHECK assertion; `recordUsage` can now abort `Repo.Cancel` via the status CHECK, which migrations 0011/0013 explicitly argued against for `engine` on the same table | Latent risks, verified non-defective at close |
| tech_debt | 52 WR-05 — `internal/presets/repo.go` and `internal/clients/repo.go` still use bare `pgx.BeginFunc` without the `pgx.ReadCommitted` pin adopted in `internal/jobs` for the identical read-modify-write shape | Out of phase scope, pre-existing inconsistency |
| accepted_risk | 52 — `admitLocked` runs after `s.storage.Upload`, so a concurrent burst wastes a bounded N−K uploads before the 429 | Disclosed and measured (`internal/jobs/admission.go:531-596`, `TestQuotaAdmission/ConcurrentBurstBoundedUploadWaste`) — accepted deviation from ROADMAP SC2b's literal wording, not debt to close |
| tech_debt | 53 WR-05/WR-06/WR-08 — `GET /v1/system/usage` all-clients response is unpaginated; a mistyped `client_limits.engine` is reported as an enforceable budget the gate never applies; `api.go`'s "never nil after NewServer" contract is not panic-enforced | Informational; unpaginated response accepted per D-02 (small internal client population) |
| tech_debt | 54 IN-04/IN-06 — the new `ci.yml` `helm` job has no `timeout-minutes`; `values.yaml:189-190` hardcodes `minioadmin` defaults (pre-existing, untouched) | CI hygiene |
| tech_debt | 54 WR-03/WR-04 — `azure/setup-helm@v4` tag-pinned not SHA-pinned; the four pre-existing CI jobs carry no workflow-level `permissions:` | Accepted overrides with recorded rationale in 54-VERIFICATION.md frontmatter (threat model T-54-03; purely-additive `ci.yml` invariant) |
| tech_debt | 55 Gap Note A — the live-gate harness proves the cross-replica claim with aggregate per-pod admit/reject counts, where 55-05-PLAN.md's acceptance criteria asked for a transcript line naming a specific admitted and a specific rejected request-id on different pods | Unflagged deviation, not a functional gap — the underlying claim is independently verified; recorded so a future reader does not hunt for a line that isn't there |
| tech_debt | Cross-phase — `internal/jobs/retention.go:126` `PurgeUsage` derives its cutoff from `time.Now().UTC()` rather than Postgres's `now()`, unlike `evaluate()`/`recordUsage`/`UTCToday` | Inconsequential at a 400-day window; inconsistent with the clock discipline this milestone established |
| infra | `internal/storage` `TestRoundTrip` fails on the shared dev host — the `octoconv-minio` volume is at 100% disk (`49G used / 256M avail`) | Host housekeeping, not application code (53-06 deferred-items.md) |
| tooling_noise | `audit-open` reports 12 quick tasks as `missing` | False positives — all 12 have both PLAN.md and SUMMARY.md; the SDK parser expects a `status:` frontmatter field these files do not carry |
| seeds | SEED-004..007, 010, 013..021 (14 dormant) | Dormant by design — forward-looking parking lot. SEED-011 flipped to `implemented` at this close (delivered by Phase 54 / CAPA-01); SEED-017 (PDF split/merge) is the concrete future demand the ACCT-02 multi-ordinal byte accounting was built for |

Items acknowledged and deferred at v1.11 milestone close on 2026-08-02:

| Category | Item | Status |
|----------|------|--------|
| quick_task | 4 pre-v1.11 quick tasks without SUMMARY (260712-cqg, 260719-2f5, 260724-26g, 260728-0ay) | missing/abandoned — all predate v1.11, carried forward from the v1.10 close below |
| seeds | SEED-004..015 (12 dormant) | dormant by design; SEED-011 now in active scope as v1.12 Phase 54 |
| tech_debt | 12 open WARNING findings in 50-REVIEW.md — live-gate harness robustness (missing jq/psql preflights, fixed /tmp fixture names, `--calibrate-only` matched only by exact `$1`, victim/survivor can collapse to one pod, `$WORKDIR` leak on error paths, attribution poller kubectl-execs into the very cgroup whose memory it measures) | Accepted — harnesses are internal gate tooling, not shipped product. Recommended: one `/gsd:quick` hardening pass |
| tech_debt | Stale "4 MiB" prose in ~12 comment sites after the Path-B cap change | Comments only; every actual value is 3 MiB (3145728) and verified consistent across compose, chart and Go |
| tech_debt | WR-11: `metadata.namespace` on ScaledObject | Reproduced from all six sibling ScaledObjects — fixing it is a whole-chart change, not a pandoc one |

Items acknowledged and deferred at v1.10 milestone close on 2026-07-27:

| Category | Item | Status |
|----------|------|--------|
| quick_task | 260712-cqg-fix-phase-16-verification-gaps-cr-01-wr- | missing/abandoned (v1.3-era, out of scope) |
| quick_task | 260719-2f5-openapi-3-1-api-octoconv-docs-yaak | missing/abandoned (v1.5-era, out of scope) |
| quick_task | 260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8 | missing/abandoned (out of scope) |
| seeds | SEED-001..015 (12 dormant) | dormant by design — forward-looking parking lot |

Acknowledged at v1.8 close (2026-07-23):

| Category | Item | Status |
|----------|------|--------|
| seed | SEED-001: Lesson-recording analysis (tutors/language schools) | RESOLVED 2026-07-23 — the actual need (audio/video → transcript) shipped in v1.7 + v1.8; the vertical analysis-product (top-mistakes + spaced-repetition deck) was deliberately NOT pursued. Do not resurface as a backlog candidate. |
| seed | SEED-004: Local k8s + KEDA full-stack validation | Deferred — superseded in practice by per-phase live gates (keda-gate 21/21, load-proofs Phases 28/33); revisit as CI k8s validation (K8SV2-01) |
| tech_debt | WR-05 keda-load-proof.sh BUSY_POD jsonpath defect (kubectl v1.36.2, `deletionTimestamp==""` vs absent key) | Accepted residual (29-REVIEW), empirically confirmed live in Phase 33; forward-fix of the frozen script recommended in a future phase |
| tech_debt | Registry cold-pull time for 682MB audio image | Unmeasurable on OrbStack shared store (Pulling→Pulled ≈0 recorded); measure in a real-registry environment before production KEDA tuning is trusted; same open question now applies to the av-worker image (AVX-02, v2 requirement) |
| tooling_noise | audit-open flags 29-HUMAN-UAT.md (status resolved) and quick task 260712-cqg (complete, SUMMARY present) | Both artifacts closed; audit parser counts them regardless |
| v2_scope | AVX-01: Trim/crop as validated closed-opts (start/end timecodes) | Deferred to v2 — only on confirmed demand | v1.8 requirements definition (2026-07-19) |
| v2_scope | AVX-02: Registry cold-pull measurement for heavy images (av-worker) | Deferred to v2 — carries forward the same unmeasured-on-OrbStack gap as the audio image | v1.8 requirements definition (2026-07-19) |
| v2_scope | PROG-V2-01: Stage-based sub-progress in the reference frontend UI | Deferred to v2 | v1.9 requirements definition (2026-07-23) |
| v2_scope | PROG-V2-02: Coarse elapsed-vs-timeout progress heuristic for non-ffmpeg engines | Rejected for v1.9 — `null` is the honest answer; revisit only on confirmed demand | v1.9 requirements definition (2026-07-23) |
| v2_scope | CNCL-V2-01: Batch cancel (cancel many jobs in one call) | Deferred to v2 | v1.9 requirements definition (2026-07-23) |
| v2_scope | PUSH-V2-01: `event`-discriminated payloads for ALL webhook types | Deferred to v2 — broader webhook contract-versioning conversation, out of scope for the single new `progress` event | v1.9 requirements definition (2026-07-23) |
| v2_scope | PDOC-V2-01: →pdf output via weasyprint/tectonic | Deferred to v2 — docx→pdf/html→pdf already covered by LibreOffice/chromium | v1.11 requirements definition (2026-07-30) |
| v2_scope | PDOC-V2-02: Extended format allowlist (textile, mediawiki, asciidoc, ipynb, typst) | Deferred to v2 — on demand | v1.11 requirements definition (2026-07-30) |
| v2_scope | PDOC-V2-03: Custom --lua-filter/templates/reference-doc | Deferred to v2 — needs its own sandbox/upload contract for untrusted code | v1.11 requirements definition (2026-07-30) |

v1.12 requirements definition (2026-08-02) deferred the following to v2, with reasons (see REQUIREMENTS.md):

| Category | Item | Status |
|----------|------|--------|
| v2_scope | FAIR-V2-01: Sub-queue/weighted-scheduler fairness | Evidence-gated — renames queues, breaks all 7 ScaledObject PromQL strings + `AllConvertQueues()` + reconciler routing; revisit only if Phase 53's operator visibility shows Phase 52's admission caps are insufficient |
| v2_scope | FAIR-V2-02: Priority tiers between clients | The only real asynq path is additional named queues per engine class, doubling ScaledObject count |
| v2_scope | ACCT-V2-01: CPU-second (machine-time) accounting beyond wall-time | Requires worker instrumentation; only justified if inter-team capacity allocation becomes a real question |

Items acknowledged and carried forward at milestone closes (see `.planning/milestones/*-MILESTONE-AUDIT.md` for full detail):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| tech_debt | docker-compose.yml audit for other stale gaps vs .env.example | Closed as DEBT-05, Phase 12 | v1.0 close (2026-07-08) |
| tech_debt | WR-02: docker-compose.e2e.yml lacks `extra_hosts` on `api` — E2E webhook pair fails on plain-Linux docker | Closed as DEBT-01, Phase 12 | v1.2 close (2026-07-10) |
| tech_debt | WR-03: engine-class string literals duplicated in 4 places — extract exported constants | Closed as DEBT-02, Phase 12 | v1.2 close (2026-07-10) |
| tech_debt | WR-04: E2E HTTP clients lack per-request timeouts | Closed as DEBT-03, Phase 12 | v1.2 close (2026-07-10) |
| tech_debt | gofmt nit in internal/queue/queue_test.go (pre-existing since Phase 9/10) | Closed as DEBT-04, Phase 12 | v1.2 close (2026-07-10) |
| v2_scope | Full ISO 19005 (veraPDF) validation of PDF/A outputs | Closed as PDFA-01/02, Phase 23 | v1.3 requirements definition (2026-07-10) |
| v2_scope | Legacy vs encrypted CFB distinction (directory-stream parsing) | Closed as CFB-01/02, Phase 22 | v1.3 requirements definition (2026-07-10) |
| v2_scope | Custom fonts / extended CJK-RTL coverage for HTML→PDF | Deferred to v2 (DOCV3-03, carried) | v1.3 requirements definition (2026-07-10) |
| accepted_risk | Active anti-DoS by document complexity (sheets/cells/unzipped size) | Accepted residual risk (DOC-V2-05, carried) | v1.2 requirements definition (2026-07-09) |
| accepted_risk | `file://` passive subresource read inside chromium-worker (shared UID nobody) | Accepted residual risk (v1.3 Phase 15) | v1.3 close (2026-07-12) |
| accepted_risk | Hallucination on silence/music (whisper exits 0 with structurally-valid garbage) | Accepted residual risk (Phase 30, carried); applies equally to video→transcript pairs added in v1.8 | v1.7 close (2026-07-18) |
| seed | SEED-001: Lesson-recording analysis for tutors and language schools | Foundation in v1.7 (transcription + JSON timestamp contract); mistake-analysis/deck remains a future milestone, not v1.8 | v1.2 close (2026-07-10) |
| seed | SEED-003: MCP-сервер для OctoConv | ✓ Implemented (v1.5 Phase 21, MCP-01..05) | v1.4 planning (2026-07-12) |
| seed | SEED-004: OctoConv on Kubernetes + KEDA autoscaling | ✓ Delivered (v1.6 Phases 24-28) | v1.6 requirements definition (2026-07-14) |
| infra | k8s-валидация в CI (kind/k3d) | Deferred to v2 (K8SV2-01) | v1.6 requirements definition (2026-07-14) |
| infra | `is_operator` column vs env-allowlist for operators | Deferred to v2 (K8SV2-03) | v1.6 requirements definition (2026-07-14) |
| tech_debt | CACHED-hit log confirmation for CI docker-build (needs gh auth) | Operator-accepted residual | v1.4 close (2026-07-13) |
| ops | Branch-protection required-checks (gate/race/docker-build) — manual GitHub UI step | Open operational follow-up | v1.4 close (2026-07-13) |
| tech_debt | presets D-04 single-active-version: application-transactional only, no DB backstop | Accepted residual | v1.4 close (2026-07-13) |
| seed | SEED-002: Decouple webhook delivery from any specific engine worker binary | ✓ Implemented (v1.3 Phase 16, WEBH-01) | v1.2 close (2026-07-10) |

## Session Continuity

Last session: 2026-08-19T08:32:01.504Z
Stopped at: Phase 59 complete; nothing pushed yet
Resume file: .planning/phases/59-born-digital-pdf-routing/59-CONTEXT.md

## Operator Next Steps

- **Milestone v1.13 (Inspect & Operations) started 2026-08-07** — 6 phases (56-61), 28/28 v1 requirements mapped, 0 orphans. Research complete (`.planning/research/`, 5 files). Next: `/gsd:discuss-phase 56` then `/gsd:plan-phase 56`.
- **Four live defects found by v1.13 research, actionable regardless of phase order:** `internal/reconciler/reconciler.go:311-312` (unguarded `RecordReconcilerAction("exhausted")` — the exact PR #4 defect class, uncaught; closed by RUNR-02), `internal/api/handlers.go:857-870` (`handleGetJob` presigns `outs[0]` with no role awareness; closed by ROLE-02), `internal/jobs/retention.go:126` (`PurgeUsage` on the Go clock; closed by DEBT-11), `internal/convert/ocrguards.go:227-228` (comment steers clients to an engine that does not accept pdf as source; closed by BDPD-02).
- **One new Go dependency planned:** `github.com/pdfcpu/pdfcpu` v0.14.0 (Apache-2.0, pure Go, `CGO_ENABLED=0`). Largest new dependency since the MCP `go-sdk` (Phase 21) — justified specifically on `/JS`/`/OpenAction`/`/EmbeddedFile` detection inside compressed object streams, where a byte scan has a real false-negative rate. It also doubles as the sanitize verifier.
- **Milestone v1.12 (Consumer Accounting & Fairness) closed and archived 2026-08-07** — 5 phases, 30 plans, 79 tasks, 15/15 requirements. Audit status `tech_debt`: 0 blockers, 9/9 cross-phase seams, 16 non-blocking debt items recorded (`.planning/milestones/v1.12-MILESTONE-AUDIT.md`). `.planning/REQUIREMENTS.md` is intentionally absent — `/gsd:new-milestone` creates a fresh one.
- **Highest-value carry-forward: 52 WR-01.** An in-flight job charges the daily cap with no time bound, so a permanently stuck job locks out a client's daily budget every subsequent UTC day, with no operator remedy in `cmd/manage-clients` and no documentation of the new quota ↔ `cmd/webhook-worker` health coupling. Natural shape for a first phase of the next milestone, together with the README self-contradiction (52 WR-09).
- Phase numbering continues from 55 — v1.13 is Phases 56-61, never restart at 1.
- Phase directories for 51-55 were archived into `.planning/milestones/v1.12-phases/` at v1.13 milestone start (commit a2b8d5b); v1.12 research moved to `.planning/milestones/v1.12-research/`.
