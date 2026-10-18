# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.0 — Hardening MVP

**Shipped:** 2026-07-08
**Phases:** 4 | **Plans:** 15 | **Sessions:** ~3 (spanning 2026-07-05 to 2026-07-08)

### What Was Built
- Mandatory API-key auth (salted-hash, dual-slot rotation) + per-client and pre-auth IP rate limiting (Phase 1)
- HMAC-signed webhook delivery with SSRF-guarded `callback_url`, bounded retry + backoff, dead-letter on exhaustion (Phase 2)
- Transient/terminal error classification, queue-aware retry backoff, `asynq.Unique` dedup lock, and a Postgres-driven reconciler for stranded jobs (Phase 3)
- Magic-byte content validation, MinIO ILM lifecycle TTL, Prometheus metrics, real `/healthz`, and an asynqmon dashboard — all live-verified end-to-end (Phase 4)

### What Worked
- **Wave-based parallel execution with worktree isolation** consistently caught same-wave file overlaps before they became merge conflicts, and cross-wave conflicts (e.g. `go.mod` contested by two plans in the same wave adding different direct dependencies) were small and mechanical to resolve by hand.
- **Post-wave build/test gates** (build + vet + full test suite after every merge, not just per-plan) caught nothing broken across 4 phases — a genuinely clean run, which validates the gate as cheap insurance rather than theater.
- **Live E2E verification over trusting SUMMARY.md narrative.** Every phase verifier and the final integration checker re-ran real commands against real Postgres/Redis/MinIO/asynq rather than accepting an executor's self-report. This caught nothing wrong in this milestone, but is the reason confidence in the "tech_debt, 0 blockers" verdict is high rather than assumed.
- **The blocking-human checkpoint for the `[ASSUMED]`-tagged `prometheus/client_golang` dependency worked exactly as designed** — the executor refused a relayed "approved" from the orchestrator twice, even after the orchestrator escalated the framing ("I'm the orchestrator talking directly to the user"). This is the correct behavior: a coordinator cannot manufacture consent on an agent's behalf. The resolution pattern that emerged — orchestrator performs the gated action directly once genuine first-hand user approval exists, rather than fighting the sub-agent's refusal — should be the standard playbook for this situation going forward.
- **Discuss-phase's decision-coverage gate caught a real gap**: 4 of Phase 4's CONTEXT.md decisions (D-03/04/05/07) were implemented but not cited in the plan's `must_haves.truths`, and the gate blocked planning completion until they were added. Cheap to fix, would have been an invisible drift otherwise.

### What Was Inefficient
- **Rebuilding the dev docker-compose stack for the first time in months surfaced a real, unrelated bug** (missing `WEBHOOK_SIGNING_SECRET` on the worker service since Phase 2) that had nothing to do with the plan being verified. Stale, never-rebuilt containers had been silently masking this for the entire Phase 3 and most of Phase 4. **Lesson: rebuild the dev stack periodically (or in CI) even when no phase explicitly requires it** — "it still runs" is not the same as "it still builds correctly from current source."
- **REQUIREMENTS.md checkbox/status drift accumulated across all 4 phases** — every single phase's own VERIFICATION.md independently noted "requirements marked complete in code, but the tracking checkboxes still say Pending," and none of the four fixed it inline. It was cleaned up in one batch pass right before the milestone audit. **Lesson: either fix the checkbox in the same commit as the VERIFICATION.md that confirms it, or accept that it's always a batch cleanup at milestone-audit time — but don't let 4 phases independently rediscover and defer the same small fix.**
- **A cross-phase recommendation fell through a real crack**: Phase 2's own SECURITY.md explicitly recommended a Phase 3 reconciler extension (sweep `done`/`failed` jobs with a dropped webhook enqueue), but Phase 3's discuss-phase scoped webhooks out entirely and never surfaced the specific recommendation for a yes/no decision. Nobody was wrong at any single step — Phase 3's context genuinely said "webhooks are Phase 2, done" — but the *recommendation itself* needed an explicit accept/reject, not silent scope-exclusion. **Lesson: when a phase's own review/security doc recommends specific future work, that recommendation should be visible as a checklist item during the *next* phase's discuss-phase, not just discoverable by manually reading old SECURITY.md files.**

### Patterns Established
- **Orchestrator-direct-execution fallback for sub-agent-refused checkpoints**: when a genuinely-authorized action is blocked because the executing sub-agent correctly can't distinguish a relayed approval from real consent, the orchestrator (who has the real, first-hand user approval) completes that specific gated step directly in the sub-agent's worktree, documents it explicitly in the SUMMARY.md as a "process deviation, not a content deviation," and lets the sub-agent's earlier commits stand.
- **Live verification checkpoints that touch shared dev infrastructure** (e.g., a docker-compose stack the user is actively running) require explicit user sign-off on *how* to verify (stop-and-substitute vs. merge-first-then-verify vs. defer), not just *whether* to verify — the infrastructure coordination is itself a decision, not a mechanical detail.

### Key Lessons
1. A blocking-human checkpoint that can be satisfied by a relayed message from any agent isn't actually blocking — the refusal-of-relayed-approval behavior observed this milestone is the feature working correctly, not friction to route around.
2. Rebuilding infrequently-touched deployment artifacts (docker-compose, container images) on a schedule, not just when a phase happens to touch them, surfaces staleness bugs while they're cheap to fix.
3. Documentation-sync fields (REQUIREMENTS.md checkboxes, cross-phase recommendation follow-through) need a structural point where they're forced to reconcile — milestone-audit time is late-but-workable; per-phase would be better if it doesn't add too much ceremony.

### Cost Observations
- Model mix: planner runs on Opus, everything else (research, execution, verification, integration-check) on Sonnet — consistent with the project's `model_profile: balanced` config.
- Sessions: ~3 across roughly 4 days of wall-clock work (2026-07-05 → 2026-07-08).
- Notable: parallel worktree execution meant most wall-clock time was waiting on Wave 1/Wave 2 pairs running concurrently rather than serially — Phase 4's 5 plans across 3 waves completed with only 2 sequential "wave boundary" waits despite 5x the plan count of a single-wave phase.

---

## Milestone: v1.2 — Document Engine Class

**Shipped:** 2026-07-10
**Phases:** 4 (8–11) | **Plans:** 12 (incl. gap-closure 11-04) | **Timeline:** ~2 дня (2026-07-09 → 2026-07-10), 71 коммит, +2754 строк Go (без .planning)

*(Note: v1.1 shipped 2026-07-08 without a retrospective section — a short same-day tech-debt milestone; its audit closed 4/4 requirements with zero carry-over.)*

### What Was Built
- Structural office-format safety gate: one-pass ZIP central-directory sniff (OOXML/ODF disambiguation), zip-bomb size guard (`MAX_DOCUMENT_UNCOMPRESSED_BYTES`), macro rejection — all 422 before any S3 write (Phase 8)
- `LibreOfficeConverter` with per-job `-env:UserInstallation` isolation, `%PDF-` output validation, and a live-proven zero-survivors process-group-kill guarantee (Phase 9)
- Dedicated `document` asynq queue + standalone `cmd/document-worker` container (LibreOffice + tini as PID 1), engine-scoped terminal timeout classification, engine-aware reconciler recovery (Phase 10)
- Engine-aware API routing via `Converter.Engine()`/`Registry.EngineFor`, first true live E2E suite (`internal/e2e`, all 6 pairs + HMAC-verified webhook), Content-Type parity gap-closure (Phase 11)

### What Worked
- **The Converter/Registry abstraction survived its second engine untouched** — the v1.2 bet ("extend, don't refactor to a Handler/Capability contract") paid off: LibreOffice slotted in with two new methods (`Engine()`, `EngineFor`) and zero changes to the worker pipeline contract.
- **Live integration testing keeps finding real bugs unit tests can't**: Phase 9's live process-kill proof discovered an actual zombie-reaping gap (fixed with tini as PID 1) that a mocked test would have declared safe.
- **Phase-level verifier caught a real shipped defect the same day**: the gaps_found verdict on Content-Type parity (flagged by code review as WR-01 but not fixed inline) forced gap-closure plan 11-04 before the milestone could close — the verify → gap-plan → re-verify loop worked exactly as designed.
- **Deferred-item bookkeeping paid off**: Phase 10 explicitly deferred its live smoke test to Phase 11's E2E with a written evidence trail, and the milestone audit could confirm closure mechanically instead of re-litigating.

### What Was Inefficient
- **Code review findings that map 1:1 to verification criteria should be fixed before verification runs** — WR-01 was known (review ran first), yet the phase went to the verifier unfixed, costing a full gaps_found → plan → execute → re-verify cycle for a ~20-line change.
- **Untracked `.planning/` bit twice**: a worktree executor couldn't see the untracked 11-04-PLAN.md (worktrees only carry tracked files) and had to reconstruct the plan from VERIFICATION.md; the gap-closure planner accidentally emptied ROADMAP.md and had to restore it from a pre-untrack git baseline. **Lesson: either commit planning artifacts (`git add -f` consistently, as SUMMARYs already are) or pass their content inline to worktree agents — never assume untracked files are visible in a worktree.**
- **The E2E compose override was only validated on Docker Desktop** — WR-02 (missing `extra_hosts` on the `api` service) means the suite's webhook pair will 400 on plain-Linux docker/CI; caught by review, deferred as debt, but it narrows where the milestone's flagship test can actually run.

### Patterns Established
- **Engine-class as a first-class routing dimension**: converter self-describes its engine (`Engine()`), registry resolves it from content-detected formats (`EngineFor(detected, target)`), every dispatch point (API, reconciler) switches on it with a fail-closed default — the template for every future engine class (CAD, AV, chromium-based HTML→PDF).
- **Write-then-run E2E split**: commit the env-gated suite (self-skips offline, keeps `go test ./...` green) in one plan, run it live with a human-verify checkpoint in the next — keeps checkpoints out of implementation plans.
- **E2E-only compose override file** (`docker-compose.e2e.yml`) for security relaxations (SSRF opt-out, host-gateway) so production compose defaults never carry test-only settings.

### Key Lessons
1. Fix review findings that overlap verification criteria *before* spawning the verifier — the review report is a free pre-verification; ignoring it converts a cheap inline fix into a full gap-closure cycle.
2. Worktree-isolated agents see only tracked files; untracked planning artifacts must be passed inline or force-committed before dispatch.
3. Live-run acceptance gates (freshly built stack + real conversions + human approval) remain the highest-confidence close signal — v1.2's audit could cite a PASS matrix instead of narrative claims.

### Cost Observations
- Model mix: planner on Opus, research/execution/verification/integration on Sonnet (balanced profile) — unchanged from v1.0/v1.1.
- Sessions: ~2 (planning+execution in one background session, close in the next).
- Notable: all three phases' worth of infrastructure (queue, worker, engine) integrated with zero post-merge test failures across 4 waves — the wave-gate streak from v1.0 continues.

---

## Milestone: v1.3 — Document Class v2

**Shipped:** 2026-07-12
**Phases:** 5 (12–16) | **Plans:** 17 (incl. gap-closure 16-05) | **Timeline:** ~2 дня (2026-07-10 → 2026-07-12), 147 коммитов, +4773/−145 строк (без .planning)

### What Was Built
- Tech-debt-first opening phase: all 5 inherited advisory items (v1.0 compose audit + v1.2 WR-02/03/04 + gofmt nit) closed with zero new features (Phase 12)
- Cross-format document conversion (docx↔odt, xlsx↔ods, pptx↔odp) via an explicit (source,target) filter table, output validated by the same `SniffContainer` that guards input; OLE-CFB legacy/encrypted inputs rejected with a single fail-closed 422 (Phase 13)
- Validated `opts` mechanism (closed allowlist, injection-proof by unit test) + PDF/A-2b export with worker-side OutputIntent verification, live-confirmed on LibreOffice 7.4 (Phase 14)
- Third engine class: HTML→PDF via chromium-headless-shell with layered network blocking (live canary: zero connections across external/loopback/compose-host/file:// vectors), CSP-injected JS disable, print opts through the same opts pipeline (Phase 15)
- Webhook delivery decoupled from engine workers: dedicated `cmd/webhook-worker` ×2 replicas as the sole webhook-queue consumer, fleet-wide singleton reconciler-sweeper via Postgres advisory lock (~11s failover), conn-lifecycle gaps (CR-01/WR-01) closed in gap-plan 16-05 with a mutex + `-race` regression test (Phase 16)

### What Worked
- **The v1.2 lesson was applied and paid off twice**: Phase 13 and Phase 14 fixed review findings that overlapped verification criteria *before* spawning the verifier — both phases passed verification on the first run, avoiding the gap-closure cycle v1.2 paid for.
- **The engine-class template scaled to its third engine with no contract changes**: Phase 15 reproduced the v1.2 pattern (own queue, own binary/container, terminal-classified timeout, fail-closed routing) mechanically; the riskiest milestone item shipped with a 14/14 security audit.
- **Live testing again invalidated "obvious" assumptions**: two load-bearing RESEARCH.md claims for chromium (JS-disable launch flag, print_background CSS hint) were proven broken against the real binary and corrected in place — a mocked test would have shipped both bugs.
- **Plan-checker caught a real concurrency bug in a plan, not just style**: the 16-05 gap-closure plan originally forbade a mutex while introducing a cross-goroutine Close(); the checker's warning forced a revision (mutex + race test) that matched exactly what the parallel quick-fix independently implemented.

### What Was Inefficient
- **Parallel sessions duplicated gap-closure work**: a quick-task session fixed CR-01/WR-01 while this session was planning the same fix as 16-05. The executor reconciled cleanly (verify-not-reimplement + add the one missing race test), but coordination cost a planner+checker cycle for work that was ~90% done. Lesson: check `git log` for concurrent landings before planning gap closures.
- **Stale SDK caches misreported audit state** (`audit-open` kept showing a completed quick task as missing after the metadata fix; `roadmap.update-plan-progress` hung once) — cross-checking against the underlying files/library directly resolved both, but cost debugging time.
- **The `.planning/`-is-gitignored friction from v1.2 persists**: every tracking commit needs `git add -f`; one commit failed mid-flow because the sdk refused ignored paths.

### Patterns Established
- **Gap-closure plans get the same rigor as feature plans**: planner → checker → revision → re-check, then executor verifies pre-existing work criterion-by-criterion instead of re-implementing ("pre-satisfied by X" evidence trail in SUMMARY).
- **Advisory-lock singleton as the fleet-coordination primitive**: `pg_try_advisory_lock` on a dedicated pooled conn with mutex-guarded lifecycle — reusable for any future exactly-one-per-fleet role.
- **Terminal-error signatures coupled same-commit with validators** (D-04 discipline from Phase 13): any new validator error string ships together with its `terminalLibreOfficeSignatures` entry, preventing retry storms on permanent failures.

### Key Lessons
1. A review-fixes-before-verifier discipline converts gap-closure cycles into first-pass verifications — now proven across two consecutive phases.
2. Concurrent sessions on one repo need a git-log check before planning: the cheapest reconciliation is discovering the work is already merged.
3. When SDK/tooling output contradicts the filesystem, trust the filesystem and verify with the underlying library directly.

### Cost Observations
- Model mix: planner on Opus, research/execution/verification/integration on Sonnet — unchanged (balanced profile).
- Sessions: ~4 (planning+execution spread over background sessions; phases 14-15 ran in parallel sessions; close in this session).
- Notable: 5 phases with zero post-merge test failures across all waves; the wave-gate streak now spans four milestones.

---

## Milestone: v1.4 — CI, Presets & Debt Cleanup

**Shipped:** 2026-07-13
**Phases:** 3 (17–19) | **Plans:** 8 | **Timeline:** ~2 дня (2026-07-12 → 2026-07-13), 54 коммита, +2261/−60 строк (без .planning)

### What Was Built
- Debt-first opening: dead webhook wiring removed, fakeEnqueuer race-safe, image E2E test — plus a bonus terminal-Close fix (DEFER-17-01) discovered BY a phase hard gate hanging on a Phase 16 test (Phase 17)
- Named presets end-to-end: internal/presets (SQL-level shadowing/no-leak), manage-presets CLI, preset= in POST /v1/jobs with XOR/re-validation/TOCTOU re-check, provenance into dormant DDL columns — zero migrations, zero new deps (Phase 18)
- 4-tier GitHub Actions CI live on a public repo: gate → -race → 6-target bake with per-scope gha cache → full compose E2E; first-ever run failed exactly one tier and thereby proved the failure-path machinery (logs artifact, if:always teardown) while exposing a real E2E-env config bug (429 rate-limit cascade) (Phase 19)

### What Worked
- **Live hard gates as plan requirements (v1.3 lesson institutionalized)**: every phase's checker demanded unconditional live proofs; Phase 17's gate hang surfaced a real latent bug (lazy re-acquire after Close) that three prior green runs had missed by timing luck.
- **The pipeline's first failure was its first success**: run 1's e2e failure exercised artifact upload + teardown exactly as designed and handed over a 5.6KB compose-log that pinpointed the 429 cascade in minutes (fetched via nightly.link despite no gh auth).
- **Empirical validation of plan self-checks**: the checker executed the plans' own verify commands against mocks and caught two would-never-pass gates (broken yq pipeline, wrong grep count); the orchestrator then caught a third (PyYAML absent) the same way. Plans whose gates are themselves tested don't wedge executors.
- **Anonymous-evidence improvisation**: public-repo API polling → rate-limited → un-quota'd badge endpoint → nightly.link artifacts. The observation plan degraded gracefully three times without losing evidentiary rigor.

### What Was Inefficient
- **Docker daemon (OrbStack) wedged during the first 18-04 attempt** — 10-minute executor stall, then a misdiagnosed restart (Docker Desktop isn't installed; OrbStack is). Lesson recorded: identify the actual container runtime before restarting it; give executors a kill-after-120s rule for docker commands.
- **GitHub anonymous API quota (60/hr) burned by 30s polling** — switch to badge/nightly.link earlier, or get gh auth up front for CI-heavy phases.
- **Production rate limits as E2E blocker was foreseeable**: PITFALLS.md flagged healthcheck timing but nobody modeled the suite's polling RPM against RATE_LIMIT_IP_RPM=60 on a faster runner. Local greenness is not CI greenness.

### Patterns Established
- **checkpoint:human-verify with graceful downgrade**: plan encodes gh-authenticated watch → anonymous API → badge → human, in that order; the operator only sees the checkpoint when automation is truly exhausted.
- **Test-only compose override as the single home for E2E relaxations** (SSRF opt-outs since v1.2, now rate limits) — production compose is never touched by test needs.
- **Orchestrator-inline execution for push-main plans**: worktree isolation is structurally wrong for a plan whose action IS pushing main; executed inline with the same gates and SUMMARY discipline.

### Key Lessons
1. Hard gates don't just verify — they discover: two of this milestone's three real bugs (terminal-Close, 429 cascade) were found by gates, not by planning or review.
2. Plan self-verification commands must themselves be executed before execution (against mocks) — three broken gates in one phase proved this is cheap and always worth it.
3. CI environments differ from dev in speed, not just OS: rate limits, quotas and timing assumptions all need a "×10 faster machine" sanity pass.

### Cost Observations
- Model mix: planner on Opus, checker/executor/verifier/integration on Sonnet — unchanged.
- Sessions: ~1 background session end-to-end (plus the operator's checkpoint replies).
- Notable: zero post-merge test failures across all waves — five consecutive milestones now; and the milestone closed within ~26 часов календарного времени.

---

## Milestone: v1.5 — MCP Access & Document Fidelity

**Shipped:** 2026-07-13
**Phases:** 4 (20–23) | **Plans:** 10 | **Timeline:** ~1 день, 71 коммит, +5537/−84 строк (без .planning)

### What Was Built
- Presets REST self-service + /v1/formats (registry-derived) — the discovery substrate MCP consumes (Phase 20)
- stdio MCP server: 5 tools, blocking convert_file with per-tick progress, zero-privilege HTTP client, key redaction, dial-redirect knob for host-run binaries; SEED-003 seed→shipped in 24h of planted intent (Phase 21)
- Hand-rolled bounded CFB directory parser (cycle-guard, 3.5M fuzz executions clean) splitting the combined 422 into encrypted/legacy/unknown (Phase 22)
- Real ISO 19005-2b validation via veraPDF in document-worker, gated by the project's first MEASURED go/no-go (p95 4.65s vs 10s budget, N=10, nearest-rank) — research's "no benchmark exists" gap answered with our own numbers (Phase 23)

### What Worked
- **Measurement-gate-as-plan-1**: making the JVM-cost spike a blocking go/no-go BEFORE any wiring meant the риskiest unknown died first; the glibc failure (musl JRE on Debian) fired exactly as research predicted and the pre-planned fallback absorbed it without replanning.
- **Checker-executes-the-plan's-own-commands** matured into standard practice: three phases' checkers ran mock validations, live docker probes (bind-mount semantics, Debian PATH order), and pkg.go.dev cross-checks — catching a host-unresolvable presigned URL (dial-redirect blocker), a phantom fuzz-seed reference, and a TDD-attribute/single-commit contradiction before any executor hit them.
- **Resume-don't-respawn for dead agents**: the Phase 23 planner died mid-file (API error) and was resumed from its own transcript via SendMessage — finished 23-03 with full context, zero rework.
- **Fixture engineering as a discipline**: length-preserving pdfaid→pdfXid corruption (keeps marker+magic, breaks one ISO clause) and the e2e-only soffice bind-mount shim let the REAL production path be driven to terminal failure without touching production code or compose.

### What Was Inefficient
- **A masked merge failure nearly lost work**: `git merge | tail` hid a non-zero exit; the cleanup branch-delete ran anyway, dangling 22-02's commits (recovered by SHA from the executor report). Fix adopted mid-milestone: merge success now guards cleanup explicitly. Never pipe a command whose exit code gates destructive follow-ups.
- **Zombie executor instances**: two stalled/killed agents later resumed themselves, one stopping the shared compose stack and editing SUMMARY in the MAIN checkout. Their edits were truthful (verified before accepting), but shared-infra mutation by unmanaged resurrections is a real hazard — worth an upstream harness guard.
- **OrbStack wedged twice more** during heavy builds; the restart runbook (quit → pkill helper → open -a OrbStack → poll docker version) is now proven but the root cause (daemon under parallel build+compose load) remains.

### Patterns Established
- Go/no-go measurement plans (autonomous:false, budget + formula explicit, GO recorded with raw numbers) for any new runtime class.
- E2E-only fault injection via single-file read-only bind mounts shadowing PATH-resolved binaries — production compose untouched.
- Same-commit coupling encoded structurally (one task = one commit) rather than as prose discipline.

### Key Lessons
1. Measure the scariest unknown first, as its own gated plan — a failed budget then costs a plan, not a phase.
2. Exit codes gate destructive follow-ups: no pipes on load-bearing commands; cleanup only inside the success branch.
3. Research predictions (glibc, JVM cost, stream names) are hypotheses — every one that was live-tested either confirmed or usefully failed; none were safe to assume.

### Cost Observations
- Model mix: planner Opus, checker/executor/verifier/integration Sonnet — unchanged.
- Sessions: ~1 background session end-to-end for the whole milestone.
- Notable: 4 phases in ~1 календарный день; вся серия v1.3→v1.5 — три милстоуна за четыре дня с нулём post-merge падений тестов.

---

## Milestone: v1.6 — Kubernetes & KEDA

**Shipped:** 2026-07-17
**Phases:** 5 | **Plans:** 14

### What Was Built
Первый инфраструктурный милстоун: единый Helm-чарт всего стека на OrbStack k8s (in-cluster E2E как Job), MCP streamable-HTTP с per-request key pass-through, operator system-presets REST, KEDA scale-from-zero per engine-class (queue-depth перенесён на always-on api) и флагманский load-proof с закоммиченным таймстамп-evidence: 0→4→0 за 136s, 178s document-джоб пережил живой KEDA-даунскейл.

### What Worked
- Жёсткое правило «KEDA-01 до любых ScaledObject» (фикс экспозиции метрики на compose до k8s-работы) сняло chicken-and-egg риск целиком
- Research-фаза дважды поймала load-bearing проблемы до кода: asynq ShutdownTimeout 8s (grace-периоды были мёртвой конфигурацией) и HPA scaleDown stabilization 300s (SC3 был бы физически недостижим)
- Live-гейты как самодостаточные скрипты с EXIT-trap teardown и live-discovery имён метрик — воспроизводимы с нуля, evidence коммитится
- Plan-checker дважды поймал сломанную shell-логику verify-команд (grep -qv, приоритет &&/||) до исполнения

### What Was Inefficient
- Калибровка тяжёлого docx потребовала 12 живых прогонов; VM-клин посреди неё инвалидировал раннюю калибровку (~37% сдвиг производительности после hard-cycle)
- Четвёртый задокументированный OrbStack-клин + один обрыв API-соединения агента + один session-limit — три восстановления за один милстоун
- Decision-coverage гейт требует литеральных D-NN в must_haves — планировщик дважды узнавал об этом постфактум (стоит вшить в промпт планировщика перманентно)

### Patterns Established
- Values-gated поведенческие оверрайды: производственные дефолты нетронуты, тестовые оверлеи (values-loadproof) включают отладочные ручки (stabilization window, CPU throttle, concurrency=1)
- pod-deletion-cost для детерминированного выбора жертвы при живом HPA-даунскейле
- Redaction-хелперы в гейтах, чей транскрипт коммитится как evidence
- Field-level условный рендер (spec.replicas) vs whole-resource гейтинг — когда ресурс должен существовать для HPA-ownership

### Key Lessons
- KEDA cooldownPeriod управляет только 1→0; N→1 — это чистый HPA со своим 300s-окном: тюнинг скейлинга требует знать, какой контроллер владеет каким переходом
- asynq Inspector читает Redis напрямую — экспозиция метрики отделима от воркера, что и делает scale-from-zero возможным
- «Проверено на N=1» ≠ «проверено»: E2E-проверка отсутствия метрики только на image-воркере принята сознательно с задокументированным обоснованием (статический grep покрывает остальных)

### Cost Observations
- Model mix: opus для планирования, sonnet для research/execute/verify — ревизии планов дешевле повторного плана
- Sessions: 1 длинная оркестрация (фазы 27-28 + закрытие); 2 восстановления субагентов через SendMessage-resume вместо пере-спавна
- Notable: live-гейты — доминирующая стоимость по времени (4 полных итерации гейта в 28-03), но evidence-первый подход исключил повторную верификацию

## Milestone: v1.7 — Audio Engine & Hardening

**Shipped:** 2026-07-18
**Phases:** 5 | **Plans:** 18

### What Was Built
Четвёртый engine-класс — офлайн whisper.cpp-транскрипция (mp3/wav/m4a/ogg → txt/srt/vtt/json с сегментными и пословными таймстампами) — проведён от standalone-конвертера до автоскейлящегося в k8s класса: fail-closed валидация (ID3v2-aware MP3-детектор, ffprobe duration-гард), stage-aware retry-классификатор, RTF-измеренный AUDIO_ENGINE_TIMEOUT=742s (p95=0.206, NO-GO-рычаг снизил max duration 4h→30min), KEDA scale-from-zero live-proven 10/10 с запечённой моделью. Плюс закрыт hardening-хвост v1.6 (WR-01 триада, operator live gate, гейт-тулинг).

### What Worked
- Measured-not-copied дисциплина: RTF-гейт по прецеденту veraPDF выдал таймаут с формулой и запасом; NO-GO-рычаг сработал по назначению (снизил ceiling вместо раздувания таймаута)
- Каждая фаза ловила реальные баги до продакшена: CR-01 amd64-обход duration-гарда (float→int64), 12-байтовое усечение аудио-загрузок (reader-offset), stale 5m CAP-дрейф из Phase 16 (реоткрывал гонку T-03-10), Inf→MaxInt64 в cgroup-парсере, недостижимый ShutdownTimeout без stop_grace_period
- Plan-checker дважды остановил блокеры до исполнения (stale CAP + echo-заглушка вместо verify); ревизии планов дешевле пере-планирования — все 3 плановых цикла закрылись за 1 ревизию
- Исполнитель, оборванный API-ошибкой перед записью SUMMARY, восстановлен SendMessage-resume из транскрипта без потери работы
- Замороженные gate-скрипты byte-unchanged: новая функциональность только новыми скриптами — WR-05 дефект замороженного скрипта эмпирически подтверждён и честно задокументирован вместо тихого фикса

### What Was Inefficient
- Разовый live E2E Phase 31 пришлось повторно институционализировать в Phase 32 как TestAudioConversionE2E — можно было заложить повторяемый тест сразу (поймано вопросом оператора, не процессом)
- Компose↔k8s взаимоисключение потребовало двух ручных переключений стеков (orb stop/start k8s) с операторским чекпойнтом посередине
- audit-open парсер спотыкается об усечённые имена каталогов и resolved-UAT — две единицы tooling-noise в финальном аудите

### Patterns Established
- RTF-гейт как переиспользуемая форма: замер на resource-limited контейнере → формула → GO/NO-GO с документированным рычагом → значения растекаются по compose/chart из одного SUMMARY
- Stage-aware terminal/transient классификатор по префиксам ошибок двухстадийного пайплайна (первый в кодовой базе; сигнатуры собственные, независимые от чужих списков)
- cgroup v2 cpu.max → явный -t для CPU-bound движков (env → cgroup → NumCPU fallback-цепочка)
- 7-way env-инвариант для queue.NewClient()-производных TTL с grep-гейтом как acceptance criterion

### Key Lessons
- Кросс-платформенные конверсии float→int64 в гардах — платформозависимые: dev-arm64 маскирует amd64-обход; валидация в float-пространстве до конверсии обязательна
- «Задокументированный порядок» ≠ «форсированный порядок»: T-30-08 закрылся только когда гард реально врезан в пайплайн с pinning-тестом
- Разовые live-доказательства обязаны превращаться в повторяемые тесты в том же милстоуне, иначе регрессия невидима
- Whisper-специфика: -DGGML_NATIVE=OFF load-bearing; -l auto обязан быть явным (тихий -l en портит неанглийское аудио); m4a-бренды isom/mp42 — MP4-видео, не аудио

### Cost Observations
- Model mix: opus для планирования/ревизий, sonnet для research/pattern-map/execute/review/verify/security
- Sessions: 1 длинная оркестрация (фазы 30-33 + закрытие милстоуна); 2 agent-recovery (API-обрыв + операторский k8s-чекпойнт) через SendMessage-resume
- Notable: RTF-замер и live load-proof — доминирующая стоимость по времени (~22 и ~45 мин прогоны); статические волны шли параллельно в worktree без конфликтов (11 executor-спавнов, 0 merge-конфликтов)

## Milestone: v1.8 — AV Engine (video/ffmpeg)

**Shipped:** 2026-07-23
**Phases:** 4 (34-37) | **Plans:** 18 | **Tasks:** 42

### What Was Built
Fifth engine class — video via ffmpeg in a dedicated `av-worker`, following the proven engine-class pattern (own queue/worker/binary/container/KEDA) plus a sixth conversion chain (video→transcript) routed onto the existing audio pipeline instead of duplicating whisper.cpp. Fail-closed video sniffers (mp4/mov/avi ftyp/RIFF + EBML mkv/webm), standalone `AVConverter` (transcode/audio-extract/thumbnail, stream-copy fast-path, protocol-whitelisted), `av` queue + stage-aware retry, ffmpeg n8.1.2-from-source container, RTF-measured `AV_ENGINE_TIMEOUT=753s`, generalized fail-closed re-encode source-bound, KEDA scale-from-zero + downscale-survival live-proven.

### What Worked
- **Measure-don't-guess held its value again.** The RTF matrix overturned the plan's D-09 VP9-dominance assumption (HEVC actually dominated at 4.18 p95) and surfaced a real hevc@2160p OOM-kill — both invisible without live measurement. Path B lever chosen from data, not a copied constant.
- **Cloning the audio precedent (Phase 33) verbatim** made Phase 37 fast and low-risk; the plan-checker confirmed clone-fidelity against the live chart.
- **Operator-gated live gates with an abort-to-operator rule** kept OrbStack safe (0 daemon-wedge incidents across multiple heavy k8s+build runs); every run returned both stacks to DOWN.

### What Was Inefficient
- **The SC3 load-proof blocked on a stale `octoconv-api:dev` image** whose collector predated `QueueAV` in `AllConvertQueues()` — one wasted ~20-min live run before the root cause (deployed-image currency for a collector-driven autoscaler) was diagnosed. A "rebuild images from HEAD" preflight in the load-proof scripts would have prevented it.
- **Two bugs surfaced only during live proof** (stale-image collector; WR-05 jsonpath `deletionTimestamp==""` vs `null`), not in static checks — live infra proofs remain irreplaceable but expensive.

### Patterns Established
- **Adversarial code review as a gate even for "clone" phases:** the Phase 36 review caught that the headline OOM-DoS fix (`enforceNoScalePassthroughBound`) closed only the no-scale path — gap-closure 36-05 generalized it to every re-encode path + both axes (CR-01/HI-01). Verify-then-generalize, don't trust the first fix's own claim.
- **Self-diagnosing live scripts:** added STEP-6 root-cause diagnostics (HPA `TARGETS <unknown>` vs `0/1`) so a re-run explains itself.

### Key Lessons
- For a collector-driven KEDA scaler, a brand-new queue class's metric series is absent until the api emits it — `ignoreNullValues:"false"` + `fallback.replicas:1` then pins the class at 1 forever. Deployed-image currency is a first-class correctness input, not an ops detail.
- A security fix's stated blast-radius must be verified independently — "closes the OOM DoS" was true for one path and false for the general case.

### Cost Observations
- Model mix: planner opus, executors/verifier/checker sonnet.
- Notable: heavy live-cluster proofs (image builds + KEDA) dominated wall-clock; the abort-to-operator discipline prevented runaway retries.

## Milestone: v1.10 — OCR Engine (text extraction)

**Shipped:** 2026-07-27
**Phases:** 4 | **Plans:** 15 | **Tasks:** 35

### What Was Built
Sixth engine class: OCR/text-extraction. Standalone OCRConverter (image/PDF → txt/hOCR/ALTO, streaming page-by-page, memory-bomb + born-digital safe); full async wiring (ocr queue, cmd/ocr-worker, reconciler); source-built Tesseract 5.5.3 + poppler container with SHA-pinned eng/rus tessdata; MEASURED OCR_ENGINE_TIMEOUT=744s (worst-cell 5.81 s/page, Path-B OCR_MAX_PAGES 200→64); KEDA scale-from-zero + downscale-survival live-proven on OrbStack k8s. Reused the v1.9 progress/SSE/webhook machinery byte-for-byte.

### What Worked
- Mechanical engine-class cloning (av/audio precedent) kept each phase tight and low-risk.
- Live gates caught real production bugs a unit/integration pass could not: the Phase-44 enqueue-switch gap, and (in Phase 45) the missing OCR opts-dispatch case + missing tesseract output-config bake — all surfaced only by running the real container end-to-end.
- Measure-don't-assume timeout: a representative-corpus re-measurement (after a toy-fixture first pass) prevented shipping a dangerously-low 60s timeout.

### What Was Inefficient
- Corpus representativeness took three measurement passes (toy 0.148 s/page → adversarial fine-print 17.9 → realistic 5.81) before a defensible number.
- The live KEDA proof required enabling OrbStack k8s + reclaiming disk (an aggressive prune wiped the tesseract build cache, forcing a full source rebuild).

### Patterns Established
- OCR opts dispatched through their own EngineOCR case in handleCreateJob's opts switch (mirrors Audio/AV/HTML).
- Engine output-format config files (tesseract configs/tessconfigs) must be baked into the runtime image alongside SHA-pinned language data.
- Live E2E ramp fixtures must be sized so the page-granular progress ramp is sampleable past the poll+throttle cadence.

### Key Lessons
- A passed unit/integration suite is not a substitute for a live containerized E2E — three of this milestone's real bugs were invisible until the actual image ran in the actual pipeline.
- Chart grace (774s) vs compose grace (764s) legitimately differ by convention; both must be ≥ the measured timeout, verified, not copied.

## Milestone: v1.12 — Consumer Accounting & Fairness

**Shipped:** 2026-08-07
**Phases:** 5 | **Plans:** 30 | **Tasks:** 79

### What Was Built
The first milestone that adds no engine. Per-client consumption accounting written inside the guarded terminal transaction (`client_usage_daily`, bytes summed over all inputs/outputs); a cardinality-bounded per-client Prometheus counter added as a separate series so the duration histogram and `queue_depth` label sets stay pinned; a transaction-scoped `pg_advisory_xact_lock` admission gate enforcing per-client limits fail-closed on all three job-creation paths with 429/`quota_exceeded`, observe mode and a kill-switch; executable retention on the reconciler tick; `GET /v1/system/usage` for the operator; values-driven per-class KEDA `minReplicaCount` (SEED-011); and QUOT-05 closed by a live 2-replica gate rather than by design argument.

### What Worked
- **Proving a property by breaking it.** Phase 51's exactly-once claim was verified by deliberately moving the usage write out of the transaction and observing `job_count = 2` — then reverting. A test that has never been seen to fail is not yet evidence.
- **The verify → gap-plan → re-verify loop earned its keep twice.** Phases 52 and 53 both scored `gaps_found` on first pass (daily cap unenforceable without a concurrency cap; idle unlimited clients invisible), and both re-verifications reproduced the fix independently against live Postgres instead of re-running the closure plans' own tests.
- **Structural fixes over threshold loosening.** The Phase 55 gate failed twice for real — a disproven host→ClusterIP reachability assumption and a round-4 over-admit that could not be told apart from legitimate slot reuse. Neither was closed by relaxing an assertion; the second was closed by proving the `exact-K` premise from Postgres every round before asserting it.
- **Writing down what a proof does NOT cover.** `admission.go`'s no-caveat statement names its own bounds (K=2, image engine, exactly 2 replicas) and leads with the structural argument, so the live run reads as confirmation rather than as the whole claim.

### What Was Inefficient
- The Phase 55 live gate consumed 7 plans, 4 of them written after a failure — two full harness rewrites (driver relocated into an in-namespace pod; heavy fixture + jitter-free barrier + per-round Postgres precondition) before a single passing run. RESEARCH.md's assumption A1 (host reachability of ClusterIP on OrbStack) was recorded as an assumption and still cost a failed calibration; it was already knowable.
- Closing the daily-cap gap (52-06) introduced WR-01 — in-flight work now charges the daily budget with no time bound — and the operator-facing remedy for it was never built, so the milestone ships with a debt item created by its own gap closure.
- REQUIREMENTS.md checkbox sync lagged behind reality in three separate phase verifications (51, 53, 54 each flagged a stale row), each time as a documentation-only note rather than a fix at the moment of noticing.

### Patterns Established
- Accounting writes ride inside the guarded transition's transaction, never adjacent to it — a worker that dies between a committed terminal transition and a separate accounting write leaves usage permanently unrecorded, since neither asynq nor the reconciler revisits a terminal job.
- Quota status domains are declared explicitly (`quotaInFlightStatuses`/`quotaLedgerStatuses`/`quotaUnaccountedStatuses`) and pinned by a test that cross-checks them against the partial index's WHERE predicate, so a future status cannot silently fall outside quota accounting.
- Date computation in the quota/accounting path reads Postgres's clock (`UTCToday`), not the API host's, so the endpoint and the admission gate can never disagree about which day it is.
- Reporting endpoints state the limits of their own truth claims (`remaining_matches_admission_gate`, per-row `enforced`, an explicit caveat when a row's own mode is observe) instead of implying enforcement they do not have.

### Key Lessons
- A gap closure can create the next milestone's debt. 52-06 made the daily cap real and, in the same stroke, made a stuck job able to lock out a client's budget indefinitely. The right response was not to avoid the fix but to ship the operator remedy alongside it — which did not happen, and is now the milestone's top carry-forward item.
- Assumptions recorded in RESEARCH.md are still assumptions. A1 was written down, reviewed, and wrong; the cost was a failed live calibration and a harness rewrite. Recording an assumption is not the same as cheaply testing it first.
- When a live gate fails ambiguously, the fix is a discriminator, not a looser bound. Round 4/10's `expected [2], got [3]` was most likely legitimate slot reuse — and the only honest way to keep asserting `==` was to make the premise checkable.

## Milestone: v1.13 — Inspect & Operations

**Shipped:** 2026-09-02
**Phases:** 6 (56–61) | **Plans:** 38 | **Tasks:** 58 | **Commits:** 45 over 26 days
**Code:** +15.5k Go non-test, +22.8k Go test

### What Was Built

Two synchronous surfaces and one engine class on top of a job pipeline that
gained a second dimension: `POST /v1/inspect`, `operation` as a validated
end-to-end dimension with output roles, and `operation=sanitize` on the eighth
engine class. Plus the closed 28-code error contract, born-digital PDF routing,
and operator job cancellation.

### What Worked

**Pinning the pre-phase behaviour in the phase's FIRST commit.** Four phases did
this (56, 57, 58, 60). It converted "we changed nothing else" from an assertion
into a byte comparison, and it caught real drift — Phase 60's own golden work
found that `pdfwrite` *creates* an XMP packet carrying the metadata it was
invoked to remove.

**Proof by inversion.** Every defence was broken on purpose and observed to fail
distinctly. This found things review did not: in three separate rounds the
inversion pass reddened nothing, which each time meant the defence had no test
at all rather than that it worked.

**Deriving numbers instead of copying them.** `PROBE_ENGINE_TIMEOUT` measurement
produced a better artifact than a number: the discovery that cost is per page,
not per byte, which changed the derivation, the KEDA downscale profile, and
surfaced a missing page ceiling nobody had asked about.

### What Was Inefficient

**Eight review rounds on one phase.** 26 findings, and the shape barely changed
between rounds: a component that worked with nothing connecting it. Rounds 4–7
were largely consequences of rounds 3–6's own fixes, because each fix addressed
the reported instance rather than the class. That is the single most expensive
pattern of this milestone.

**Tests that proved a component rather than its use.** Two dead code paths and
one field passed to a function that ignored it, all with green tests. A helper
implementing a decision (`SanitizedOOXMLExtension`) had its own passing unit
test while nothing in production called it.

**Comments that outlived their code.** `ProbeDescriptor` documented a terminal
classification the code never implemented; `SanitizeSupports` named a caller
that never existed; migration 0015's "intentional divergence" survived its own
closure. This is the same defect Phase 59 found in `ocrguards.go`, reproduced
inside the phase built to prevent it.

### Patterns Established

- **Make the independent checker BROADER than the thing it checks.** Late in
  Phase 60 the verifier's macro check was widened past what the sanitizer
  removes — and it immediately caught two sanitizer bugs that eight review
  rounds had missed. This is the countermeasure that worked; more tests of the
  same kind was not.
- **A rule needed in two places must land in both in one change.** Named four
  times in one review: `docProps/` beside the relationship fix, a sentinel on
  three paths of five, a JPEG spec rule applied to the scan and not the
  segments, stream scoping applied to one scan and not its two neighbours.
- **Derive completeness guards from the source of truth.** Three hand-written
  tables (queue coverage, reconciler routing, per-engine defaults) each failed
  when the eighth engine arrived — not because the engine was missing, but
  because the guard was. All three now derive from `convert.AllEngines()`.
- **A closed vocabulary needs a production consumer.** `inspect.AllCodes` has
  eleven; Phase 60's had zero until the predicates were wired into report
  construction. An unused accessor documents an intent rather than enforcing it.

### Key Lessons

1. A test that calls a function directly cannot see that nothing else calls it.
2. A test that exercises two components together cannot see an assumption they
   share. The JPEG post-scan defect had three passing tests, all built on files
   whose segments come before the scan.
3. A class fix that stops at its first instance leaves the second under the very
   comment explaining why that shape is wrong.
4. Measuring a bound often teaches more than the bound. Per-page cost, the
   wrong-statistic finding about p95, the missing page ceiling, and three
   harness defects all came out of one measurement run.
5. A measurement harness that can hang silently is not one an operator can be
   asked to run — found by running it and watching an hour of nothing at 100%
   CPU.

### Cost Observations

- Eight external review rounds on one phase; 23 of 26 findings fixed pre-merge.
- Three of the phase's own inversion passes found gaps in themselves.
- One diagnosis in-session was wrong (a garbage fixture blamed for a hang that
  a 3000-page synthetic caused) and was corrected from the completed run.

## Cross-Milestone Trends

### Process Evolution

| Milestone | Sessions | Phases | Key Change |
|-----------|----------|--------|------------|
| v1.0 | ~3 | 4 | First milestone — wave-based parallel execution + worktree isolation + live E2E verification established as the baseline pattern |
| v1.1 | ~1 | 3 | Same-day tech-debt close; first zero-carry-over audit (no retro section written) |
| v1.2 | ~2 | 4 | First multi-engine milestone; verify → gap-plan → re-verify loop exercised for real (Content-Type parity); first committed live E2E suite |
| v1.3 | ~4 | 5 | Third engine class; review-fixes-before-verifier discipline adopted; first parallel-session reconciliation (quick-task vs gap-plan) |
| v1.4 | ~1 | 3 | First live CI on GitHub; unconditional live hard gates institutionalized; plan self-checks empirically validated pre-execution |
| v1.5 | ~1 | 4 | First measured go/no-go gate; first new dep since v1.0; agent-resume-from-transcript recovery; masked-exit-code merge bug found and fixed |
| v1.6 | ~1 | 5 | First infrastructure milestone; values-gated overrides; live gates with committed timestamped evidence became the proof standard |
| v1.7 | ~1 | 5 | Fourth engine class fully autonomous session (plan→execute→review→verify→secure per phase); RTF measurement gate reused; frozen-scripts discipline; one-off live proofs institutionalized as repeatable E2E |
| v1.10 | ~1 | 4 | Sixth engine class; seconds-per-page measurement gate (new metric shape vs. RTF ratio); three live-only bugs (enqueue-switch gap, opts-dispatch case, config bake) surfaced only by containerized E2E |
| v1.12 | ~1 | 5 | First non-engine milestone; two phases went gaps_found → closure → re-verification with independent live reproduction; a live gate failed twice for real and was fixed structurally (discriminating precondition) rather than by loosening an assertion; a property proven by deliberately breaking it |
| v1.16 | — | 6 | Tenth engine class; premise probe before every derived bound; pre-registered derivation and rerun rules; docs held to code by tests; all-class final gate from a fresh clone |

### Cumulative Quality

| Milestone | Requirements | Blockers at Audit | Tech Debt Items |
|-----------|-------------|--------------------|-----------------|
| v1.0 | 24/24 satisfied | 0 | 5 (3 carried to next milestone's Active candidates, 2 informational) |
| v1.1 | 4/4 satisfied | 0 | 0 — first zero-carry-over close |
| v1.2 | 10/10 satisfied | 0 | 4 advisory (WR-02/03/04 + gofmt nit), all documented in 11-REVIEW.md and STATE.md |
| v1.3 | 14/14 satisfied | 0 | 3 advisory (dead webhook wiring in document/chromium workers, fakeEnqueuer -race, no image E2E), documented in v1.3-MILESTONE-AUDIT.md |
| v1.4 | 11/11 satisfied | 0 | 4 advisory (CACHED-residual, branch-protection manual step, D-04 invariant, manual acceptance script), documented in v1.4-MILESTONE-AUDIT.md |
| v1.5 | 12/12 satisfied | 0 | 3 advisory (no MCP-specific CFB live path, amd64 pin, JRE path B size), documented in v1.5-MILESTONE-AUDIT.md |
| v1.6 | 9/9 satisfied | 0 | advisory tail (OPER-01 live-script gap, WR-01 semantics, gate-tooling warnings) — closed by v1.7 Phase 29 |
| v1.7 | 12/12 satisfied | 0 | 4 advisory (WR-05 frozen-script jsonpath forward-fix, registry cold-pull measurement, arm64 RTF caveat, review Info nits), documented in v1.7-MILESTONE-AUDIT.md |
| v1.10 | 12/12 satisfied | 0 | advisory tail (WR-05 frozen-script jsonpath residual carried, corpus-representativeness lesson recorded), documented in v1.10-MILESTONE-AUDIT.md |
| v1.12 | 15/15 satisfied | 0 | 16 non-blocking items — heaviest is 52 WR-01 (unbounded in-flight daily charge, no operator remedy), a debt created by this milestone's own gap closure; documented in v1.12-MILESTONE-AUDIT.md and STATE.md |
| v1.16 | 29/29 satisfied | 0 | tech_debt: Helm upload cap below the CAD ceilings (TD-1), two phase-76 deferred items, inherited AddOutputs/MarkDone split, measurement-tooling items; documented in v1.16-MILESTONE-AUDIT.md and STATE.md |

### Top Lessons (Verified Across Milestones)

1. Blocking-human checkpoints must reject relayed approval from any agent, including the orchestrator — confirmed working as intended in v1.0.
2. Live, real-infrastructure verification (not just unit tests or narrative trust) is the deciding factor in audit confidence — established in v1.0, worth preserving as milestones grow larger and re-verifying everything live becomes more expensive.
3. Fix code-review findings that overlap verification criteria before spawning the verifier — learned in v1.2 (paid a gap cycle), applied in v1.3 (two first-pass verifications).
4. Live hard gates discover bugs planning cannot — confirmed across v1.3 (chromium research corrections) and v1.4 (terminal-Close hang, CI 429 cascade); make them unconditional in every plan that touches infrastructure.
5. Never let a piped command's masked exit code gate destructive cleanup — v1.5's dangling-commit incident; success-branch-only cleanup is now the standard merge shape.
6. A gap closure can manufacture the next debt item — v1.12's 52-06 made the daily quota real and simultaneously made a stuck job able to lock out a client indefinitely; ship the operator remedy in the same phase as the fix that needs it.

## Milestone: v1.14 — Typesetting & Debt

**Shipped:** 2026-09-05
**Phases:** 3 (62–64) | **Plans:** 7 | **Commits:** 13 on main over 4 days (phases squashed; 52 in phase 64's branch alone)
**Code:** +3.9k Go non-test, +5.9k Go test

### What Was Built

A ninth engine class that typesets LaTeX offline — tectonic with a digest-pinned
122 MiB bundle, no egress, `--untrusted`, its own queue, worker, container and
ScaledObject — plus phase 63's sanitize hardening and probe page ceiling and
phase 62's operational debt tail.

Its timeout is the first here derived from the ENGINE's own ceiling rather than
from a percentile: `ceil(25.76 s/pass × 7 passes × 1.5)`, where 7 is tectonic's
rerun maximum, paired inseparably with a 2 MiB input bound because cost is
linear in source size.

### What Worked

**Measuring at the bound instead of extrapolating to it.** The shipped number
comes from a 2 143 444-byte document, not a small one scaled up. That paid three
times: it caught that the measured document is 2.2% ABOVE the ceiling that
refuses it (found by POSTing it and getting a 413); it caught the measurement
SCRIPT iterating 369 KB while printing "derived from a run AT the ceiling" —
which would have recommended 46s for work that takes 148; and when a comment
extrapolated a rate instead of citing a measurement, the numbers turned out to
be non-linear (240 000 loop iterations produced FEWER bytes than 130 000).

**Rewording a requirement on evidence rather than meeting it as written.**
TEX-03 asked to refuse packages outside the declared eighteen. Compiling 23
common packages against the real bundle showed twelve resolve while outside it —
five in pandoc's own default template, one in this phase's own end-to-end paper.
Gating on the declared set would have refused documents the service typesets
correctly, which is 63-02's defect exactly. Same shape as PROBE-01's rewording
one phase earlier, and confirmed with the operator before anything was built.

**Running the fixed build instead of trusting the fix.** Round 2's log cap was
correct and did not stop the OOM — the memory was tectonic's, not the worker's,
and only a live run said so. The same habit refuted a review premise using the
PR's own evidence (a document that prints "stopping at 6 passes" and resolves
everything), and caught my own first probe of the timeout defect being wrong: a
1e8-iteration loop terminated in 66 seconds and proved nothing.

**Running the gates end to end, not just the tests.** The local CI stand-in's
e2e tier had never completed on this machine. Completing it found that EVERY
sanitize job was panicking inside pdfcpu — asynq recovered it, retried, and left
the job `active` forever, so the only symptom was a test timing out. That defect
was live on `main` and invisible to every unit test in the repository.

### What Was Inefficient

**Fixes that addressed the named instance and left the class.** The v1.13
retrospective's most expensive pattern, reproduced:

- round 1 denied `fontspec`; round 2 found `fontspec-xetex` and
  `fontspec-luatex` — the implementation files `fontspec.sty` itself loads —
  admitted, one of them returning a PDF;
- round 1 made a timeout terminal on `context.DeadlineExceeded`; round 2 found
  the whole-attempt deadline meant a stalled MinIO call was reported to the
  client as a document that failed to typeset;
- round 2 fixed `\fi` matched as a prefix; round 3 found `%` decided by one byte
  of lookback. Three instances of one shape — a byte test standing in for
  tokenization — before it was fixed AS tokenization.

The countermeasure that worked was writing each fix as a rule over every
instance: the grace-period rule found `probeWorker` carrying the same defect
from phase 63, the deny-list variant test forbids the next one, and the CI bake
rule is derived from docker-compose.yml rather than listed.

**Tests whose own comments asserted what a later change falsified.**
`TestBothTeXGatesReadTheSameSource` claimed the worker "cannot drift from the
bundle because it IS the bundle" — true until round 1 added a deny list the
engine knows nothing about, and the test had no denied case to notice. The same
week, a pinning test named "missing bundle ... does not blame the document"
covered only the one wording that happened to contain the env var's name.

**A test that passed for the wrong reason, caught only by inverting it.** The
first regression test for the pdfcpu panic fed the verifier a file containing
"not a pdf" — which is rejected before pdfcpu is ever reached. It passed with
the fix removed.

**A live proof and the test suite sharing a database.** The first log-bomb run
came back `reconciler_exhausted` in 49 seconds and looked like a defect; it was
`go test ./...` sweeping the same Postgres.

### Patterns Established

- **Bound what comes back, not only what goes in.** For an engine whose input is
  a program, the input ceiling implies nothing about the output — and then bound
  what gets STORED from it, which was a separate omission.
- **A rule the engine cannot express must be enforced where the rule lives.**
  Every other tex refusal is the engine's verdict read back; a denied package is
  IN the bundle, so the engine has no verdict and both gates must check it.
- **A recovered panic is an invisible failure.** asynq's recover sits above the
  terminal-commit point, so a panic left the job `active` with no metric and no
  error. Recovering at the right level turns it into an honest failure with the
  stack in `job_events.detail`.
- **Publish the measurement, never the rate.**

## Milestone: v1.15 — Engine Confinement

**Shipped:** 2026-10-01
**Phases:** 4 (67–70; 67 shipped as PR #28 before the milestone opened) | **Plans:** 20 (68–70) | **Commits:** 136 on the phase branch since main
**Code:** +3.5k Go non-test, +5.9k Go test

### What Was Built

Every engine class — image, pandoc, ocr, av, audio, html, probe, document — runs
its engines through one `engineExec` path under a per-job Landlock ruleset whose
grant set was measured, recorded in SANDBOX-PATHS.md and pinned by test and by a
doc-vs-code cross-check. Workers refuse to start without Landlock and prove
confinement with a content-judged real-engine preflight before Postgres. The
eight-class live gate ends `ALL PASS (40 checks)` on images built from the final
commit.

### What Worked

**Testing the premise before paying for the hardware.** Phase 70 was scoped
around an amd64 node because veraPDF is published amd64-only. Measuring first
(no ELF under /opt/verapdf, then a native-vs-emulated differential that must end
`DIVERGENCES: 0`) removed the need for the node entirely; the differential is a
repeatable script, not a one-off observation.

**Letting measurement overturn a review.** WR-02 claimed veraPDF's `ro:/proc`
leaked the worker's secrets through `/proc/<ppid>/environ`. A canary proven
readable unconfined showed environ was already denied (ptrace), while
other processes' cmdline/status did leak — the grant was narrowed for the
measured leak, not the predicted one.

**A live gate that catches drift nobody changed.** The eight-class gate went red
on an html check no phase-70 code touched: Chromium 153 → 154 moved shared
memory to `memfd_create`, silently defeating a negative control. Rebuilding the
same tip of phase 69 reproduced it, which kept the fix scoped correctly.

### What Was Inefficient

- The plan-checker/planner loop and two executors were interrupted by transient
  API/classifier failures; each needed a resume with explicit disk state.
- GSD's worktree dispatch default fought the live-gate workflow (compose project
  names tie to the repo root); sequential dispatch had to be set explicitly.
- The verifier wrote a fingerprint without the phase's PLAN/SUMMARY files, which
  read as `stale` and blocked `phase.complete` until regenerated.
- Docker Hub stopped serving `minio/minio`/`minio/mc` mid-milestone; e2e ran on a
  local chainguard retag and the durable fix is a follow-up.

### Patterns Established

- Negative controls must observe, not infer: "fails when denied" is defeated by
  any fallback; "an outside observer sees the control and not production" is not.
- A new grant kind (socket-only) instead of widening a shared access mask keeps
  seven proven classes byte-identical.
- Shared-/tmp guards compare for *exactly* 1, never `<= 1`: the queue library
  maps `< 1` to NumCPU.

### Key Lessons

1. A pinned-architecture constraint is a hypothesis about one component; measure
   which component before buying hardware for the whole image.
2. Unpinned engine packages make live gates non-reproducible across days; record
   the engine version in every gate transcript.
3. Tests that build a confined converter and reach `Convert` run the shared-/tmp
   sweep — give them a scratch directory, never the host's `/tmp`.

### Cost Observations

- Model mix: planner opus; researcher, checker, executors, verifiers sonnet.
- Notable: live gates (image builds) dominated wall-clock; the 4-plan gap
  closure cost about as much as the original 5 plans.

## Milestone: v1.16 — CAD Engine

**Shipped:** 2026-10-18
**Phases:** 6 (71–76) | **Plans:** 49 | **Commits:** 507 over 17 days, PRs #32–#38
**Code:** +6.8k Go non-test, +17.2k Go test, +2.5k lines of engine helpers

### What Was Built

A tenth engine class, `cad`: step/stp/iges/igs/stl → STL or a PNG render with a
second `measurements` output (`octoconv.measurements/v1`), and dxf → svg/pdf/png,
from OCP, VTK + OSMesa, ezdxf and rsvg-convert with no X server. Every engine
stage runs under its own measured Landlock ruleset; every bound (886 s engine
timeout, per-format ceilings, the 120 MiB IGES ceiling) comes from measured rows
and is pinned by a test that reads the evidence. The all-class final gate ends
`ALL PASS` from a fresh clone at `07d1024`; KEDA 0→1→2→0 and a downscale-survival
of a long render are proven on OrbStack k8s.

### What Worked

**Measuring the premise before building on it.** Phase 71 chose the kernel,
renderer and DXF path on the production base image under Landlock, judged by
pixels. Phase 76 opened with a pre-registered stacking probe that asked whether
bytes bound 3D render cost at all; they do not (a 614 KB STL and a 599 KB STEP
each hit the 900 s budget), so the planned byte-ceiling path for render never
fired, and three tests now hold that outcome instead of a ceiling that would
have bounded nothing.

**Pre-registered rules for the measurement runs.** The derivation rules, the
quiet-host rule (load1 ≤ 11 at start and end) and the rerun rules
(`rerun_after_product_fix`, `rerun_after_host_sleep`, `rerun_contaminated_rows`)
were written before the rows existed. When a run stopped, the decision was which
rule applied, not what the number should be.

**Contract and class in separate phases.** The sniffers and the measurements
schema landed in phase 72 with no engine registered, reviewed as Go/SQL only;
the class then entered the API whole in one commit in phase 73, with every other
class and every metric label pin byte-identical.

**Docs held to the code both ways.** `docs/CAD.md`, `docs/PREVIEW.md`, OpenAPI,
MCP descriptions and the frontend table are each computed from the code or the
evidence by a test, so the client docs cannot drift silently from what ships.

### What Was Inefficient

- The host was the bottleneck, not the code. The VM-disk watchdog stopped 76-02
  and two 76-08 attempts; a stale signal watcher hit a zombie in attempt 3; a
  Docker daemon stopped answering during cleanup after attempt 6. The 76-11
  final gate took four attempts (INCOMPLETE, apt mirror, a wrapper false
  negative) before `ALL PASS`.
- Review rounds found the same class more than once. PR #36 took four codex
  rounds on what "a DXF with nothing visible" means before emptiness was judged
  by rendered ink on every output; PR #37 took three rounds on fail-open reads in
  the gate wrapper and the loadproof calibration.
- Phase 71's provenance was only checked by a later re-verification: 126 later
  testdata files had no per-file provenance, closed by quick 261018-44q and
  PR #38 after phase 76 had merged.
- At close, `init.manager` read phases 72–76 as `stale`: the verifier's
  covered-file fingerprints include source files that later phases legitimately
  change, so a passed report goes stale by construction across a multi-phase
  milestone.

### Patterns Established

- Ask whether the bound's premise holds before deriving the bound: a ceiling on
  an input that does not drive cost is a number, not a limit.
- Pre-register the derivation and the rerun rules; record every contaminated or
  failed attempt rather than overwrite it.
- One judge per question across outputs: the same ink judge decides emptiness
  for DXF svg, pdf and png, the same render judge on both backgrounds.
- A gate wrapper's own reads (cleanup listings, dirt checks, env reads) fail the
  verdict they feed; a masked command is a false green.

### Key Lessons

1. Test the premise of a bound before measuring the bound — v1.16's 3D render
   cost was not a function of bytes, and the probe that showed it was cheaper
   than the ceiling machinery it made unnecessary.
2. Host capacity is part of the test environment: reserve VM disk headroom
   (measured full-eviction peak 19 GiB plus the 6 GiB floor) before a gate run,
   and keep the watchdog.
3. When a reviewer finds a second instance of a class, replace the predicate,
   not the instance (DXF emptiness, gate-wrapper reads).
4. Verify provenance claims per file at the phase that makes them, not at the
   milestone audit.

### Cost Observations

- Model profile: `balanced` (config); the per-agent mix was not recorded.
- 13 quick tasks during the milestone: nine codex review rounds on PRs #35–#37,
  the phase-75 deferred-items closure, the shared render judge, and the
  phase-71 provenance follow-up.
- Notable: measurement and live-gate wall-clock (multi-hour bounds runs, image
  builds, k8s proofs) dominated; code review rounds were the second cost.
