---
gsd_state_version: 1.0
milestone: v1.12
milestone_name: Consumer Accounting & Fairness
status: planning
stopped_at: Phase 54 context gathered
last_updated: "2026-08-06T00:22:19.672Z"
last_activity: 2026-08-05
progress:
  total_phases: 5
  completed_phases: 3
  total_plans: 21
  completed_plans: 21
  percent: 60
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-02 after v1.12 roadmap creation)

**Core value:** Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла (изображения, офисные документы, HTML, аудио, видео, OCR, markup) и получить результат — без риска для стабильности или безопасности продакшена.
**Current focus:** Phase 54 — per class capacity floor (seed 011)

## Current Position

Phase: 54
Plan: Not started
Status: Ready to plan
Last activity: 2026-08-05

## Performance Metrics

**Velocity:**

- Total plans completed: 137 (all v1.0–v1.11)
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
| 54 | TBD | - | - |
| 55 | TBD | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. v1.12-specific roadmap decisions (recorded at roadmap creation, 2026-08-02):

- **Phase numbering continues from v1.11's Phase 50 — this milestone is Phases 51-55**, not a restart at 1.
- **Attribution before enforcement before visibility (Phase 51 → 52 → 53), mirroring the milestone's own framing** ("Атрибуция" before "Границы"): quotas can't be enforced against numbers that aren't tracked yet, and the operator endpoint has nothing to show without the rollup existing first.
- **Quota enforcement and baseline (admission-control) fairness are ONE phase (52), not two**: FAIR-01's per-client concurrency cap is the identical mechanism QUOT-01..04 already need (Q1/Q3 = Q4 option A in ARCHITECTURE.md) — building them separately would duplicate the admission-cap machinery for no benefit. FAIR-02 (queue_depth/PromQL invariance) is the phase's own tripwire, not a separate feature.
- **RETN-01 folded into Phase 51 (Attribution), not its own phase**: the requirement text itself frames retention as a precondition ("строить учёт поверх неограниченно растущих таблиц значит повторить уже имеющийся долг") — the new `client_usage_daily` table must state its retention/rollup design in the same migration that creates it, and the pre-existing `jobs`/`job_events` unbounded-growth gap gets an explicit policy in the same phase rather than being silently repeated a third time.
- **QUOT-05 (multi-replica correctness) is its own live-gate phase (55), separate from the phase that builds the mechanism (52)**: the atomic Postgres admission design is a Phase 52 implementation constraint (must be correct under N replicas by construction), but QUOT-05 as a requirement is specifically the *proof* — `api.replicas: 2+`, live-observed — matching this project's own precedent for claims that can't be proven statically (KEDA scale-from-zero/downscale-survival gates) and directly answering `internal/ratelimit`'s own documented `replicas: 1`-only caveat, which the milestone explicitly must not repeat for quotas.
- **CAPA-01 (SEED-011, minReplicaCount) is its own small phase (54), not folded into 51-53**: zero data/code coupling with the accounting/quota work (pure Helm values/`ScaledObject` change) — keeping it separate avoids unrelated chart-diff noise in the accounting/quota review and lets it ship/rollback independently; sequenced as a low-risk hygiene tail per research, not blocking or blocked by anything.
- **Per-client sub-queue fairness (the design that renames queues and touches all 7 ScaledObject PromQL strings) is explicitly NOT in this roadmap** — deferred (FAIR-V2-01 in REQUIREMENTS.md v2), evidence-gated on Phase 53's operator visibility showing the admission-cap baseline is actually insufficient in practice. Building it speculatively would repeat the SEED-010 rejection pattern already recorded in PROJECT.md, this time risking the KEDA scale-from-zero contract instead of the compile-time engine allowlist.
- **Coverage: 15/15 v1 requirements mapped, 0 orphans** — see REQUIREMENTS.md Traceability table.

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

### Pending Todos

- Plan and execute Phase 53 (Operator Usage Visibility: OPSV-01/02) — reads Phase 51's rollup table, sequenced after Phase 52's quota mechanism so "usage vs. limit" is meaningful.
- Fix Retry-After midnight race in daily quota rejection — `quotaRejection` can advertise ~86400s when the daily quota already reset; needs a sentinel-error contract change, deliberately deferred out of PR #5. See `.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md`.

### Blockers/Concerns

None currently blocking. Sequencing carried into the roadmap:

- Hard-ordered spine for the accounting/quota work: 51 (attribution + retention — new rollup table + client-labeled counter, additive/zero-behavior-change) → 52 (quota enforcement + baseline fairness, same mechanism — needs Phase 51's table) → 53 (operator visibility — reads Phase 51's table, sequenced after 52 starts so "usage vs. limit" is meaningful).
- Phase 54 (CAPA-01/SEED-011) is fully independent — chart-only, can execute at any point; placed as a hygiene tail so it doesn't add unrelated diff noise to 51-53's review.
- Phase 55 is an operator-gated live proof (`autonomous: false`) — depends on Phase 52's atomic mechanism existing; needs a real `api.replicas: 2+` run, cannot be closed by static verification.
- Highest-risk phase is 52: three-tier admission gate (concurrency/byte-budget/atomic-reserve) touching `handleCreateJob`, both MCP entry points, and the reconciler's release-on-every-terminal-state discipline — PITFALLS.md documents 10 pitfalls anchored to this exact area (cardinality, replica-races, reconciler double-count, rollout safety, KEDA/fairness conflation, 429-vs-403).
- Explicit non-goals for this milestone (do not resurface): per-client sub-queues/dynamic virtual queues (breaks all 7 ScaledObject PromQL strings + `AllConvertQueues()` + reconciler routing — FAIR-V2-01), billing/invoicing, self-service quota portal, ML/anomaly throttling, formal SLA tiers.
- Operational discipline (OrbStack) carried forward: pre-build all images sequentially with non-`latest` tags; never run compose and k8s stacks hot simultaneously (four confirmed daemon wedges on record, per v1.6/v1.7/v1.8 history) — relevant again for Phase 55's live multi-replica run.

## Deferred Items

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

Last session: 2026-08-06T00:22:19.654Z
Stopped at: Phase 54 context gathered
Resume file: .planning/phases/54-per-class-capacity-floor-seed-011/54-CONTEXT.md

## Operator Next Steps

- Phase 52 is complete and verified (9/9 plans, shipped as PR #5).
- Start phase planning with `/gsd:plan-phase 53`.

</content>
