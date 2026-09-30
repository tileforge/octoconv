---
gsd_state_version: "1.0"
milestone: v1.15
milestone_name: Engine Confinement (67–70)
current_phase: 70
current_phase_name: Document on amd64
status: executing
stopped_at: Phase 70 context gathered
last_updated: "2026-09-30T15:58:57.729Z"
last_activity: 2026-09-30
last_activity_desc: Phase 70 execution started
state_head: 0ceec2f4923cc4858f8f348df769015f97c9cfc0
progress:
  total_phases: 4
  completed_phases: 3
  total_plans: 16
  completed_plans: 15
  percent: 75
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-09-29 after Phase 69)

**Core value:** Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла (изображения, офисные документы, HTML, аудио, видео, OCR, markup) и получить результат — без риска для стабильности или безопасности продакшена.
**Current focus:** Phase 70 — Document on amd64

## Current Position

Phase: 70 (Document on amd64) — EXECUTING
Plan: 4 of 5
Status: Ready to execute
Last activity: 2026-09-30 — Phase 70 execution started

## Performance Metrics

**Velocity:**

- Total plans completed: 196 (all v1.0–v1.11)
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
| 68 | 6 | - | - |
| 69 | 5 | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
| Phase 55 P05 | 15min | 2 tasks | 2 files |
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 68 P01 | 75min | 3 tasks | 16 files |
| Phase 68 P02 | 31min | 3 tasks | 3 files |
| Phase 68 P03 | 25min | 3 tasks | 9 files |
| Phase 68 P04 | 60min | 3 tasks | 12 files |
| Phase 68 P05 | ~55min | 3 tasks | 20 files |
| Phase 68 P06 | 28min | 3 tasks | 4 files |
| Phase 69 P01 | 95min | 3 tasks | 20 files |
| Phase 69 P02 | 51min | 3 tasks | 32 files |
| Phase 69 P03 | 55min | 2 tasks | 3 files |
| Phase 69 P04 | 50min | 3 tasks | 5 files |
| Phase 69 P05 | 40 min | 2 tasks | 7 files |

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
- [Phase 68]: 68-01: forJob never grants an input directory separately -- every confined class must materialise its input inside the job directory (mirrors pandoc; binding for plans 03-06)
- [Phase 68]: 68-01: sandbox-fault sentinel (ErrEngineSandboxUnavailable) checked before class-prefix text in all four engine-scoped terminal classifiers, so a node fault is never misclassified as a client file defect
- [Phase 68]: 68-02: OCR's proven set keeps /usr/share/fonts (necessary -- without it pdftoppm+tesseract silently degrade to empty OCR text at exit 0) but drops /etc/fonts, /usr/share/fontconfig/conf.avail and the empty /usr/local/share/fonts — necessity-by-removal, judged by content not exit code
- [Phase 68]: 68-02: audio's model grant moves from the /models directory to the resolved /models/ggml-base.bin file, granted at run time; ffprobe added to the engine set to cover the pre-Convert duration guard — AUDIO_MODEL_PATH is configurable and not known at Go-var-declaration time
- [Phase 68]: OCR's six engine call sites (spanning ocr.go/ocrpdf.go/ocrguards.go) share one per-job engineExec via an explicit x engineExec parameter, not a Sandbox-field lookup per file — keeps every free function testable with a bare engineExec{} (unconfined), byte-identical to the pre-Phase-68 runCommand calls, and is the shape plan 04 (av, five call sites across two files) reuses directly
- [Phase 68]: AV: engineExec built once per job in avPrepareConversion and threaded through every ffprobe/ffmpeg call site (probe, transcode, audio-extract, thumbnail)
- [Phase 68]: AV: progress FIFO directory moved inside the job directory; -progress option folded into the engine's own argv BEFORE sandbox wrapping (engineExec.stream), never after
- [Phase 68]: AV: EnforceMaxDuration keeps its exported signature this plan (calls probeDuration with engineExec{}); plan 05 threads the audio worker's own sandbox handle into it
- [Phase 68]: The pre-Convert audio duration guard's ffprobe is confined by threading a sandbox helper through Handler.audioSandboxHelper the same way audioMaxDuration already travels (NewHandler -> NewRunner -> audioPreConvertGuard -> enforceAudioGuardBeforeConvert -> convert.EnforceMaxDuration); cmd/audio-worker hands the converter and NewHandler the SAME helper variable so they cannot disagree.
- [Phase 68]: Coverage-as-a-class AST test holds all of internal/convert to engineExec routing; conf05-crosscheck all four classes MATCH; docker compose down -v replaced with down (no -v) for the mid-sequence teardown per sandbox/environment constraints — Prevents future unclassified/unconfined engine execs by construction rather than by per-class review; -v teardown avoided to preserve volumes and respect sandbox denial of irreversible destruction
- [Phase 69]: 69-01: html keeps /tmp read-write (chromium's shared memory, TMPDIR does not move it) -- concurrency capped to 1 while confined, held by a startup guard (D-03) and a pre-task sweep (D-04), not a comment
- [Phase 69]: 69-01: forJob generalised into forJobRW (a class-shared writable path beyond the job directory); engineExec gained a replaceable, allowlisted environment (withEnv/engineEnv) so a class whose grant includes /proc never leaks secrets via /proc/self/environ
- [Phase 69]: 69-01: HTMLPreflight judges confinement by the produced PDF's /Title field, never by chromium's exit status (it exits 0 and writes a blank PDF when denied, D-06/D-07); the negative run points the engine itself outside every grant since Convert copies its input into the job directory first
- [Phase 69]: [Phase 69]: 69-03: three of four required class-closure inversions performed and reverted (isProbeTerminal sentinel removal, /tmp added to probeReadWrite); the fourth (literal exec restored in chromium.go) was blocked by the harness's own security-weakening tool classifier and is tracked as an open item in WINDOWS.md
- [Phase 69]: 69-04: injectCSPFirst performs no head/html tag search at all -- document-start splice (BOM, HTML whitespace, past a leading doctype's first '>') closes G-69-2
- [Phase 69]: 69-04: frame-src 'none' and child-src 'none' added to cspNoScriptMeta -- refuses a framed/embedded copy of the raw upload (in.html), a live bypass measured independent of the splice fix
- [Phase 69]: 69-04: both G-69-2 guards proven load-bearing by live inversion -- pre-fix splice fails exactly 12 of 18 rows, frame-src/child-src removal fails exactly f1/f2
- [Phase 69]: 69-05: the large-document check's transcript states the /dev/shm size actually in effect (~7.8 GiB, OrbStack default), never the diagnosis session's assumed ~64Mi -- validity rests on the negative control (denied on /dev/shm, not on an exhausted tmpfs), not on that size
- [Phase 69]: 69-05: G-69-3 closed -- chromium-worker chart/compose/live-gate no longer mount or size /dev/shm; each now cross-references SANDBOX-PATHS.md instead of restating a pre-Landlock rationale

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
| 260830-dmg | Восемь находок ревью PR #10 за три круга (3 codex + 4 codex + 1 своя), все — ложный вердикт, все три воспроизведены до принятия: октальные escape'ы считались как литеральные цифры (18 против истинных 6 при пороге 16 — вердикт не преувеличен, а сфабрикован; тот же класс, что уже был опровергнут замером для hex и оставлен в соседней функции), голое слово `stream` внутри литеральной строки открывало диапазон и давало ложный `present` (проверка синтаксиса добавлена в детектор, а не в общий `scanPDFStreamRanges`, чья широта корректна для двух других вызывающих), и неоднозначный маркер `/FontFile*` пропускал текстонесущий Form XObject, сохраняя покрытие — ложный `absent`, направление, запрещённое D-09. Второй круг codex обошёл первое исправление (`>>` можно положить внутрь литерала — два обхода одной границы), поэтому слоёный предикат заменён однопроходным сканером, отслеживающим лексический контекст. Третью находку нашёл сам, атакуя этот сканер: незакрытый литерал проглатывал остаток окна и давал `absent` о неосмотренном. Ни один вердикт корпуса не сдвинулся ни разу, DIVERGENCE.md побайтово идентичен | 2026-08-30 | 06a6bd4 | [260830-dmg-close-three-codex-findings-from-pr-10-re](./quick/260830-dmg-close-three-codex-findings-from-pr-10-re/) |
| 260918-rdm | README.md переписан по скиллу `create-readme`: 566 строк → 505, секции Contributing/License убраны (для обеих есть файлы), операционные подробности убраны в `<details>`, предупреждения — в GitHub-admonitions. Документ отставал от кода на милстоун, а не на правку: секция движков знала пять классов из девяти (`ocr`/`pandoc`/`probe`/`tectonic` отсутствовали, хотя машинно-проверяемый список для `set-limit` в том же файле их содержал — документ противоречил сам себе), операция `sanitize` не упоминалась вообще, пять маршрутов (`/v1/inspect`, `GET /v1/jobs`, `/events`, `/cancel`, `/v1/system/usage`) не были описаны, Roadmap заканчивался на v1.8 при шипнутом v1.14, а `docs/*` не были залинкованы ни разу. Граница проведена по `main` на тот день: Landlock (#21) описан, CPU-бюджет (#22) и `Correlation-Id` (#23) — нет, они были ещё в ревью. Оба влиты 2026-09-19 (`5579e59`, `b95b38d`), и документацию за ними догнала задача 260919-q7k. Обобщая таблицу конфигурации до `<CLASS>_*`, сам внёс неточность (image-класс шаблону не подчиняется) и поймал её сверкой с `.env.example`, а не рассуждением о том, как именование «должно быть» устроено | 2026-09-18 | ddc150c | [260918-rdm-readme-refresh](./quick/260918-rdm-readme-refresh/) |
| 260919-q7k | Документация и планировочные артефакты догнаны за мержами #22/#23. README не знал ни `TECTONIC_ENGINE_CPU_SECONDS`, ни `Correlation-Id` — я сам провёл его границу по `main` того дня и через несколько часов влил обе исключённые ветки; `docs/TYPESETTING.md` не знал про CPU-бюджет, хотя это документ про TeX-движок, и расходился с `.env.example`, который знал. Добавлены: correlation-id с ограничениями и причиной (`422`, а не усечение, потому что обрезанное значение больше не совпадает с искомым), CPU-бюджет с замерами (0.96s wall/0.95s CPU на простое против 3.67s/1.08s на вчетверо перегруженном узле — часы раздулись, потребление нет), две строки отказов в TYPESETTING. Заголовок `## Two failures…` там считал два при трёх строках и стал беззначным — тот же дефект, ради которого написан `readme_sync_test.go`. STATE: «Pending Todos» перечисляли 4 пункта при одном живом (три в `completed/`, Phase 53 шипнута в v1.13) — снятые названы вместе с тем, куда делись; строка 260918-rdm утверждала «#22 и #23 ещё в ревью». ROADMAP: фазы 65 и 66 записаны как шипнутые ВНЕ милстоуна, и записано, что у фазы 65 нет планировочных артефактов вовсе. Тест «README равен .env.example» намеренно не написан: точный двусторонний падал бы постоянно (README сокращает до `<CLASS>_*`), а односторонний не поймал бы этот случай — он обратного направления | 2026-09-19 | 5dd7b57 | [260919-q7k-docs-and-state-after-merges](./quick/260919-q7k-docs-and-state-after-merges/) |
| 260919-b4n | `.gitignore` не знал бинарников `cmd/`: в корне лежали `api` (39 МБ) и `manage-clients` (14 МБ), один `git add -A` от попадания в историю навсегда. Строка `/tectonic-worker` показывала, что это уже случалось и было починено узко, для одного имени — класс сработал ещё дважды молча. Блок между маркерами покрывает все шестнадцать `cmd/`, а `internal/repohygiene` держит список равным `cmd/` в обе стороны: команда без строки — бинарник, который можно закоммитить; строка без команды — шаблон, который однажды скроет файл, взявший это имя. Третья проверка ловит имя без ведущего `/`, которое заглушило бы `cmd/api` и `internal/api` целиком (проверено `git ls-files`/`check-ignore`, а не рассуждением). Пакет из одних тест-файлов — что Go такой принимает, измерено до того, как на этом строить. Инверсия в три стороны. Бинарники не удалены, `.vscode/` не тронут: это файлы владельца | 2026-09-19 | 5979dfc | [260919-b4n-gitignore-cmd-binaries](./quick/260919-b4n-gitignore-cmd-binaries/) |
| 260919-fast | Перепроверено утверждение «document-worker не сконфайнить на arm64-хосте» — верно по исходу, неверно по причине, и разница меняет план фазы. Замер пробником (`sandbox.Available()` + настоящий `Apply()` + чтение `/etc/hostname`) в четырёх средах: нативный arm64 конфайнит по-настоящему (DENIED), эмулируемый amd64 — `available=false`, и то же самое в РЕАЛЬНЫХ образах `octoconv-tectonic-worker` и `octoconv-document-worker`. Ядро одно, разница в эмуляции: `landlock_create_ruleset` из amd64-процесса не проходит. Причина не в архитектуре хоста, а в `platform: linux/amd64` у одного из двенадцати сервисов — ради `verapdf/cli`, который публикуется amd64-only. Следствие: семь из восьми классов, включая chromium, разрабатываются здесь; заблокирован один, и снять блокировку можно не трогая Landlock. Записано в todo. Происхождение неверной версии: её нет ни в `MEMORY.md`, ни в todo — она жила только в моём пересказе прошлой сессии и дважды процитирована мной как факт | 2026-09-19 | e08a627 | — |
| 260920-s3p | Инструмент замера путей для песочницы и первый замеренный набор. `scripts/sandbox-paths-measure.sh` (две фазы) + `scripts/cmd/sandbox-probe`, который ставит кандидатный ruleset и `exec`'ает под ним движок. Ловушка, ради которой нужны обе фазы: трасса `openat` **не видит исполняемый файл движка** — его не открывают, его `execve`'ят, поэтому первый набор для libvips выглядел полным и все пять пар упали с `exec: permission denied` под настоящим ruleset. Результат по `image`: rw — каталог задачи; ro — `/usr/bin/vips`, `/lib`, `/usr/lib`, `/etc/ld.so.cache`, две записи `/proc/self`, `/proc/{filesystems,cpuinfo}`, две записи `/sys/devices/system/cpu`. Из `/etc` больше ничего, `/tmp` не нужен. Достаточность — пять пар форматов `rc=0` с непустым выходом; действенность — тот же движок с входом вне каталога задачи читает файл без ruleset и получает `Permission denied` под ним. Записано в `.planning/research/SANDBOX-PATHS.md`. Взято именно это, потому что лимит Codex исчерпан, а замер ничего не меняет в поведении | 2026-09-20 | PENDING | [260920-s3p-sandbox-path-measure](./quick/260920-s3p-sandbox-path-measure/) |

### Pending Todos

Ровно один, и он совпадает с тем, что лежит в `.planning/todos/pending/`:

- **Первый деплой на amd64 проверяет все восемь конфайненных классов.** Замер и живой
  гейт шли только на arm64; гранты директорийные, так что multiarch-каталоги покрыты
  конструкцией — это рассуждение, а не замер (D-05, фаза 70). См.
  `.planning/todos/pending/2026-09-29-amd64-first-deployment-preflight-check.md`.

Пункт, стоявший здесь до 2026-09-30, снят потому, что он сделан, а не потому, что
о нём передумали (сделано, а не передумано): «остальные классы движков запускаются
неконфайнено» закрыт фазами 67-70 →
`todos/completed/2026-09-06-other-engines-still-unconfined.md`.

Четыре пункта, стоявшие здесь до 2026-09-19, сняты потому, что они сделаны, а не
потому, что о них передумали — след сохранён нарочно, чтобы «исчезло из списка»
не читалось как «отменено»:

- api crash-loop до готовности postgres → `todos/completed/2026-08-18-api-startup-crashloop-until-postgres.md`
- Inspect honest-reporting, три пункта из ревью PR #9 → `todos/completed/2026-08-18-inspect-honest-reporting-open-items.md`
- Retry-After midnight race → `todos/completed/2026-08-04-fix-retry-after-midnight-race.md`
- Phase 53 (Operator Usage Visibility, OPSV-01/02) — шипнута в составе v1.13;
  `GET /v1/system/usage` в `internal/api/routes.go`

### Blockers/Concerns

currently blocking. **v1.13 sequencing and risks carried into the roadmap:**

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

Acknowledged and deferred at the v1.13 milestone close on 2026-09-02: 47 items.

These accumulated across thirteen milestones. The seeds are dormant by
design — a seed exists to be dated and left until its trigger fires. The
quick tasks are execution history whose directories were never archived.
The todos are the live ones and are the only part of this table worth
reading as work: the probe page ceiling, the three round-8 review
findings, the API startup crashloop, the inspect honest-reporting items,
and the soak test that shares a database with parallel packages.

| Category | Item | Status |
|----------|------|--------|
| quick_tasks | 260712-cqg-fix-phase-16-verification-gaps-cr-01-wr- | missing |
| quick_tasks | 260719-2f5-openapi-3-1-api-octoconv-docs-yaak | missing |
| quick_tasks | 260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8 | missing |
| quick_tasks | 260728-0ay-modernize-frontend-spa-to-api-v1-10-with | missing |
| quick_tasks | 260802-9de-close-remaining-pr-2-review-findings-enf | missing |
| quick_tasks | 260802-9zc-fix-the-same-mid-rune-utf-8-truncation-i | missing |
| quick_tasks | 260802-asr-fix-three-frontend-bugs-found-in-pr-2-re | missing |
| quick_tasks | 260803-obt-close-codex-pr-5-findings-on-phase-52-qu | missing |
| quick_tasks | 260803-q7u-fix-test-integrity-defects-found-by-code | missing |
| quick_tasks | 260803-tm4-strict-boolean-env-parsing-plus-openapi- | missing |
| quick_tasks | 260804-44i-round-retry-after-up-to-whole-seconds-fo | missing |
| quick_tasks | 260804-47r-fix-flaky-testenforceminfreedisk-exact-b | missing |
| quick_tasks | 260809-ul4-4-codex-pr-7-role-driven-download-url-op | missing |
| quick_tasks | 260810-r2a-round2-eocd-bound | missing |
| quick_tasks | 260810-r2i-round2-inspect-exit-paths | missing |
| quick_tasks | 260810-sle-fix-six-codex-findings-in-internal-inspe | missing |
| quick_tasks | 260810-slf-fix-four-codex-findings-in-internal-api | missing |
| quick_tasks | 260811-r3s-exhaustion-sink | missing |
| quick_tasks | 260811-r4b-bounded-record-count | missing |
| quick_tasks | 260811-r5p-parser-divergence | missing |
| quick_tasks | 260812-r6c-criterion-collapse | missing |
| quick_tasks | 260816-r8m-intake-and-placement | missing |
| quick_tasks | 260816-r9d-double-charge | missing |
| quick_tasks | 260817-j58-close-codex-pr9-finding-xlsxsheetcount-a | missing |
| quick_tasks | 260830-6zr-ci-workflow-dispatch | missing |
| quick_tasks | 260830-dmg-close-three-codex-findings-from-pr-10-re | missing |
| seeds | 004 | dormant |
| seeds | 005 | dormant |
| seeds | 006 | dormant |
| seeds | 007 | dormant |
| seeds | 010 | dormant |
| seeds | 013 | dormant |
| seeds | 014 | dormant |
| seeds | 015 | dormant |
| seeds | 016 | dormant |
| seeds | 017 | dormant |
| seeds | 018 | dormant |
| seeds | 019 | dormant |
| seeds | 020 | dormant |
| seeds | 021 | dormant |
| seeds | 022 | dormant |
| todos |  | open |
| todos |  | open |
| todos |  | open |
| todos |  | open |
| todos |  | open |
| todos |  | open |

## Session Continuity

Last session: 2026-09-29T19:40:04.078Z
Stopped at: Phase 70 context gathered
Resume file: .planning/phases/70-document-on-amd64/70-CONTEXT.md

## Operator Next Steps

- Plan and execute Phase 68 (/gsd-plan-phase 68, then /gsd-execute-phase 68)
