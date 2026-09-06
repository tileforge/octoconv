---
gsd_state_version: 1.0
milestone: v1.14
milestone_name: Typesetting & Debt
status: complete
last_updated: "2026-09-05T02:15:00.000Z"
last_activity: 2026-09-05
progress:
  total_phases: 3
  completed_phases: 3
  total_plans: 5
  completed_plans: 5
  percent: 100
  note: "v1.14 SHIPPED 2026-09-05: phase 64 merged as e443c23 (PR #16), all three phases on main"
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-07 after v1.13 milestone start)

**Core value:** Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла (изображения, офисные документы, HTML, аудио, видео, OCR, markup) и получить результат — без риска для стабильности или безопасности продакшена.
**Current focus:** none. v1.14 SHIPPED (2026-09-05) and its debt tail is worked down (PRs #17-#20). Two todos remain, both tex: confining the engine's file reads, and the page-count heuristic. Next milestone not yet chosen.

## Current Position

Milestone: v1.14 — Typesetting & Debt, SHIPPED 2026-09-05. No milestone open.

DEBT WORKED DOWN AFTER THE MILESTONE (PRs #17-#20, all merged):

- #17 fuzzed the PDF tokenizer. Its first finding was in the fuzz property
  itself, and it answered the residual PR #14 left open: the resynchronisation
  really does record decoy object headers out of a payload (10 of them on the
  input the fuzzer produced), and the xref-anchored retry is what makes them
  unreachable in the answer that is used.
- #18 moved inspect's page_count onto the tokenizer and fixed the trailer's
  /Prev, then found the cost nobody had measured: scanPDF's integer window
  trimmed by re-slicing the same backing array, 48 MB on a 21 MB file, in the
  API process and the worker both.
- #19 is the one that matters: a .tex was a CREDENTIAL READ. --untrusted stops
  \write18 and does NOT confine file reads, so \openin on /proc/self/environ
  returned DATABASE_URL, S3 keys and API_KEY_SALT. Found while testing whether
  the engine can read a sibling file -- the premise multi-file projects depend
  on. TEX-04 had proved the mechanism it named and never asked this one. The
  engine now gets an allowlist environment; CONFINING the reads is #21
  (todos/completed/2026-09-05-tex-file-reads-not-confined.md, open PR).
- #21 closed that with Landlock, and took EIGHT review rounds (17 findings) plus
  ten own sweeps (6 more) to do it. Everything after round 4 was one thing:
  TECTONIC_CACHE_DIR is operator-supplied, and every way of turning it into a
  Landlock grant was wider than it looked -- the root, the format's parent, the
  file's NAME, then a relative path that compared as "tmp" and opened as
  "/tmp". Each fix was right and each produced the next finding. What held were
  invariants over the WHOLE spec, not over the setting that had just failed.
  Six of my own greens were not evidence, and three of those were the same
  mistake as four of the findings: the check and the checked thing were not the
  same object.
- #21 closed that with Landlock, which the todo never costed: it listed three
  PRIVILEGED mechanisms and called a mount namespace "the only one that makes
  the guarantee structural", when an unprivileged process can restrict ITSELF
  and have it inherited across execve. Measured before writing code (ABI 8 as
  nobody in the image), and running it found two defects reasoning had not:
  the ruleset would not build at all (the bundle is a FILE, and Landlock
  refuses directory rights on one) and nothing could open /dev/null. The
  option list was the defect, not the implementation.
- #20 shipped multi-file projects (operator chose repeated "file" parts over an
  archive). Five review rounds, 16 findings, plus eight own sweeps finding seven
  more. "What counts as TeX source" was answered four times -- by extension, by
  text-shape, by loader list, per-directive -- because every fix closed a bypass
  and closing a bypass means looking at more files, which is how a data file
  gets read as a preamble. What held is the test shape: every table carries both
  directions.

REVIEW AVAILABILITY is now a real constraint: codex hit its account
code-review usage limit on #17, #19 and twice on #20. Each merge went ahead on
local gates plus an own adversarial sweep, and each PR says so rather than
reading silence as approval.

CODEX ROUND 3 ADDRESSED (2026-09-05): 3 findings, all P2, no P1. Each
reproduced before being fixed.

- P2 the comment scanner asked whether the PREVIOUS BYTE was a backslash, so
  \let\foo\\% \usepackage{pgfplots} reported pgfplots with complete=true and the
  API refused a document that compiles. Third instance in this PR of a
  byte/prefix test standing in for tokenization, so the fix is the tokenization:
  a control SYMBOL is consumed as one token, which also stops \{ and \} moving
  braceDepth.
- P2 os.Stat is not "the bundle is usable". Reproduced live: with
  TECTONIC_BUNDLE_PATH pointing at the DIRECTORY /opt/tectonic the worker
  started, logged its bundle and consumed the queue. ValidateTectonicBundle
  opens it, requires a regular non-empty file, and is used by BOTH the startup
  check and Convert.
- P2 the timeout measurement claimed the ceiling and measured 368 999 bytes by
  default, because the 2 143 444-byte document is generated on demand. It now
  stages the generated corpus -- which doubles as a check that every committed
  file comes back byte-identical from the generator.

Two defects in scripts/ci-local.sh found by running it: a RETURN-trap teardown
referencing a function-local array (transcript ended in "compose[@]: unbound
variable"), and no guard against a developer API already holding :8090 -- which
made the whole e2e tier fail with "401 invalid api key" against the wrong
process.

CODEX ROUND 2 ADDRESSED (2026-09-05): 10 findings, 2xP1. All ten confirmed by
running before any fix, and three of them turned out to be wider than the
review said.

- P1 unbounded engine log. MEASURED: 235 bytes of .tex -> 323 565 198 bytes of
  stdout in 30s (10.8 MB/s), so the 271s timeout would hold ~2.9 GB in a 1 GiB
  container. Capped at 8 MiB of TAIL, which is 5.9x the 1 416 019 bytes the
  largest ADMISSIBLE document produces. runCommand itself unchanged -- veraPDF's
  whole XML report is stdout and is parsed.
- P1 no output ceiling. MEASURED: 199 bytes -> 19 545 890 bytes of PDF in 93s
  through the live API, against 1 914 057 bytes for the largest real document.
  TECTONIC_MAX_OUTPUT_BYTES=32 MiB, terminal, own code (output_too_large).
- P2 deny list enforced in the API only. Independently found and proven live
  BEFORE the review arrived, and wider: fontspec past the sniff window returned
  a finished PDF, and fontspec-xetex/fontspec-luatex -- the implementation files
  fontspec.sty itself loads -- were admitted outright. Worker now scans the
  whole input; all three names denied; the class test forbids any denied package
  having an unlisted bundle variant.
- P2 whole-attempt deadline made infra stalls terminal. process() bounds
  download+convert+upload with one context (T-03-11), so a stalled MinIO call
  raised the engine's error. Only convert.ErrTeXEngineTimeout is terminal now.
- P2 \fi matched as a prefix: \finalfoo closed a conditional and the API refused
  a document that compiles. Whole-control-word matching; \usepackagefoo{bar} was
  the same defect on the directive.
- P2 undefined-references check forgeable by \typeout. Keyed on LaTeX's own
  warning line; both forms captured from the image as fixtures. This check has
  now been wrong in both directions inside one PR.
- P2 E2E CI job bakes without the bundle -- the round-1 fix went into one of the
  two jobs that bake. Test derives the rule from compose + ci.yml.
- P2 unpinned upstream builder clone. Pinned; PROVENANCE records that the
  SHIPPED asset cannot be attributed to a revision, since that tree is gone.
- P2 TYPESETTING.md still called a timeout transient.
- P2 pod grace == asynq ShutdownTimeout. Written as a rule over all nine
  workers, which found probeWorker (phase 63) with the identical slip.

CODEX ROUND 1 ADDRESSED (2026-09-04): 7 findings, 2xP1. Six confirmed by
running before any fix; one premise refuted with a real gap behind it.

- P1 the tex image could not be built from a fresh checkout at all (the bundle
  is gitignored; the fetch was prose in a Dockerfile comment). Now a script CI
  runs before bake.
- P1 a tex timeout was retried 3x271s of a concurrency-1 worker. Now terminal:
  the input is a program, and 64-05 measured the largest admissible document at
  180.3s against a 271s bound. My own first probe was WRONG (a 1e8-iteration
  loop completed in 66s and proved nothing).
- P2 the scanner refused a document that compiles (\usepackage inside a false
  conditional). Now abstains; the regression test then found a bug in the fix.
- P2 fontspec was admitted though the docs promise it is unavailable.
- P2 a bare \usepackage{biblatex} failed on the missing biber with no
  actionable message.
- P2 persisted opts were silently ignored on this engine alone.
- P2 non-convergence: REFUTED. 64-04's paper prints "stopping at 6 passes" and
  resolves everything. The real gap next to it -- undefined references
  published silently -- is fixed, keyed on LaTeX's own warning and scoped to
  the final pass.

TEX-01..TEX-05 are all closed. Two of them were reworded or qualified on
evidence rather than ticked:

- TEX-03's gate refuses what the BUNDLE cannot resolve, not what the declared
  set omits. Gating on the declared eighteen was measured to refuse twelve
  packages that compile — five in pandoc's default template, one in the paper
  64-04 itself ran. Same shape as PROBE-01's rewording in 63-02, and confirmed
  with the operator before it was built.
- TEX-01 is met, with a named limitation: the API is single-input, so a
  multi-file project cannot be submitted. Carried by
  todos/pending/2026-09-04-tex-multi-file-projects.md.

64-05's measurement produced a finding rather than only a number:
TECTONIC_ENGINE_TIMEOUT=271s means nothing without an input ceiling, because
cost is linear in source size — at the inherited 100 MiB a dense .tex needs
~9000s, ten times the reconciler cap. The pair (2 MiB / 271s) is now pinned by
test, the way PROBE_MAX_PAGES and PROBE_ENGINE_TIMEOUT are.

Three defects this phase found that only a mechanism could find, all now
covered by tests that walk a list rather than restate one: the missing tectonic
case in handleCreateJob's enqueue switch (every .tex 500'd), cmd/api not reading
TECTONIC_ENGINE_TIMEOUT, and both webhook-workers missing four engine vars since
Phase 32 — harmless only by coincidence, in the service that runs the
reconciler.

Next: work the PR #16 review rounds, merge, then close milestone v1.14.

Open findings carried out of phase 64 (both in todos/pending/):

- multi-file LaTeX projects cannot be submitted (single-input API);
- the tex input ceiling is a heuristic — real cost is pages x passes, neither
  knowable before the engine runs, so the timeout is the actual backstop.

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
| 260830-dmg | Восемь находок ревью PR #10 за три круга (3 codex + 4 codex + 1 своя), все — ложный вердикт, все три воспроизведены до принятия: октальные escape'ы считались как литеральные цифры (18 против истинных 6 при пороге 16 — вердикт не преувеличен, а сфабрикован; тот же класс, что уже был опровергнут замером для hex и оставлен в соседней функции), голое слово `stream` внутри литеральной строки открывало диапазон и давало ложный `present` (проверка синтаксиса добавлена в детектор, а не в общий `scanPDFStreamRanges`, чья широта корректна для двух других вызывающих), и неоднозначный маркер `/FontFile*` пропускал текстонесущий Form XObject, сохраняя покрытие — ложный `absent`, направление, запрещённое D-09. Второй круг codex обошёл первое исправление (`>>` можно положить внутрь литерала — два обхода одной границы), поэтому слоёный предикат заменён однопроходным сканером, отслеживающим лексический контекст. Третью находку нашёл сам, атакуя этот сканер: незакрытый литерал проглатывал остаток окна и давал `absent` о неосмотренном. Ни один вердикт корпуса не сдвинулся ни разу, DIVERGENCE.md побайтово идентичен | 2026-08-30 | 06a6bd4 | [260830-dmg-close-three-codex-findings-from-pr-10-re](./quick/260830-dmg-close-three-codex-findings-from-pr-10-re/) |

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

Last session: 2026-08-19T08:32:01.504Z
Stopped at: Phase 60 context gathered; ready to plan
Resume file: .planning/phases/59-born-digital-pdf-routing/59-CONTEXT.md

## Operator Next Steps

- Start the next milestone with /gsd-new-milestone
