---
gsd_state_version: 1.0
milestone: v1.9
milestone_name: Job Progress & Control
status: executing
stopped_at: Completed 41-01-PLAN.md
last_updated: "2026-07-25T05:00:11.324Z"
last_activity: 2026-07-25
progress:
  total_phases: 5
  completed_phases: 3
  total_plans: 11
  completed_plans: 8
  percent: 60
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-19 after v1.8 milestone start)

**Core value:** Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла (изображения, офисные документы, HTML, аудио, видео) и получить результат — без риска для стабильности или безопасности продакшена.
**Current focus:** Phase 41 — Progress Core

## Current Position

Phase: 41 (Progress Core) — EXECUTING
Plan: 2 of 4
Status: Ready to execute
Last activity: 2026-07-25

## Performance Metrics

**Velocity:**

- Total plans completed: 107 (all v1.0–v1.8)
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
| 40 | TBD | - | - |
| 41 | TBD | - | - |
| 42 | TBD | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
| Phase 36 P01 | 15min | 3 tasks | 6 files |
| Phase 36 P02 | 30min | 2 tasks | 2 files |
| Phase 36 P03 | 20min | 3 tasks | 3 files |
| Phase 36 P04 | 25min | 1 tasks | 4 files |
| Phase 36 P05 | 20min | 2 tasks | 9 files |
| Phase 37 P01 | 25min | 3 tasks | 4 files |
| Phase 37 P02 | 30min | 2 tasks | 3 files |
| Phase 37 P03 | 40min | 2 tasks | 6 files |
| Phase 38 P01 | 20min | 2 tasks | 1 files |
| Phase 39 P01 | 25min | 2 tasks | 6 files |
| Phase 39 P02 | 20min | 2 tasks | 3 files |
| Phase 39 P03 | 15min | 3 tasks | 8 files |
| Phase 40 P01 | 20min | 2 tasks | 5 files |
| Phase 40 P02 | 25min | 2 tasks | 6 files |
| Phase 41 P01 | 20min | 2 tasks | 3 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. v1.9-specific decisions surfaced by research, to be recorded as Key Decisions before/at implementation:

- **Progress writes bypass the guarded-transition lock by design (Phase 41):** `Repo.UpdateProgress` is a non-locking `UPDATE ... WHERE status='active'` with no `job_events` row — progress is high-frequency telemetry, not a state change; routing it through `transition()` would serialize it against real status changes and bloat the append-only audit log for zero benefit. Do not "simplify" this later by merging it into `transition()`.
- **ffmpeg progress reporting is an optional `ProgressReporter` capability, not a widened `Converter` contract (Phase 41):** only `AVConverter`/`AudioConverter`'s ffmpeg-normalize stage implement it; libvips/LibreOffice/Chromium/whisper-cli's transcribe stage are untouched. `exec.runCommandWithProgress` is additive (fd-3 via `cmd.ExtraFiles`, parent write-end closed immediately after `Start()`), never a replacement for `runCommand`.
- **Cancellation correctness lives entirely in Postgres, not asynq (Phase 39):** `Inspector.CancelProcessing` is a best-effort speed optimization only; the guarded `{queued,active}→canceled` transition allow-list is the authoritative backstop against the confirmed, unfixed asynq#987 retry-on-cancel race in the pinned v0.26.0. Do not rely on the handler's own `context.Canceled` check to stop retries.
- **`internal/cancel` package (Phase 39) is new shared infrastructure:** in-memory `map[uuid.UUID]context.CancelFunc` registry + Redis pub/sub (`job:cancel`), imported by both `api` and every worker binary — purely a low-latency signal delivery mechanism, never the source of truth for whether a job is actually canceled.
- **`GET /v1/jobs` pagination must be keyset, not offset (Phase 40):** offset pagination drifts under this table's continuous-insert load; keyset on `(created_at, id)` is a hard requirement, not a style preference.
- **ffmpeg `out_time_ms` is microseconds, not milliseconds (Phase 41):** a well-known unfixed ffmpeg naming wart — must be verified against a real fixture captured from the pinned n8.1.2 binary before trusting the parser, not assumed from docs.
- **Progress-pipe-to-exec wiring is its own live-gated verification track, separate from parsing logic (Phase 41, PROG-05):** a careless pipe-reader goroutine can leak forever if the parent's write-end fd copy isn't explicitly closed after `cmd.Start()`. Requires a live SIGKILL-mid-stream test asserting zero orphaned processes AND zero leaked goroutines — `go test -race` is insufficient (mirrors the Phase 9 zombie-reaping precedent).
- **Webhook `progress` event is opt-in and best-effort single-attempt, never the durable retry/dead-letter contract (Phase 42, PUSH-02):** a stale progress delivery isn't worth chasing and would compete for retry budget with the terminal `done`/`failed` event.
- **Zero new Go dependencies for v1.9:** SSE via stdlib `http.Flusher`; ffmpeg progress parsing via stdlib `os.Pipe`+`bufio.Scanner`; cancellation via already-pinned `asynq` v0.26.0 (`Inspector.CancelProcessing`/`TaskID`); pagination via plain parameterized `pgx` SQL.
- [Phase 38]: 0007_job_progress.sql adds progress_pct/eta_seconds/source_duration_seconds/progress_updated_at to jobs, nullable no-default with fail-closed CHECKs — Foundational additive schema unblocking Phase 40 read (progress_pct) and Phase 41 writes; verified on fresh and populated Postgres with zero application behavior change
- [Phase 39]: Cancel reuses transition() exactly like MarkDone/MarkFailed (no new transition machinery, D-01)
- [Phase 39]: Terminal-race classification is by re-read + isTerminalStatus, never by string-matching the illegal-transition error text
- [Phase 39]: CancelPublisher is wired via a post-construction setter (SetCancelPublisher), not a NewServer parameter
- [Phase 39]: Reused existing go-redis client (already direct dep via asynq) for job:cancel pub/sub — zero new Go dependency
- [Phase 39]: Registry.Cancel releases the mutex before invoking the stored CancelFunc to avoid any reentrancy deadlock risk
- [Phase 39]: isCanceled checked as first branch in every Handle*Convert (D-07), independent of engine-scoped terminal classifiers
- [Phase 40]: Repo.List: client_id=$1 unconditional in SQL WHERE; opaque base64(RFC3339Nano|uuid) cursor; jobs_client_created_idx reused, no new index
- [Phase 40]: handleListJobs scopes exclusively to auth.ClientFromContext's client.ID -- never a client-supplied id -- passed as the sole clientID to Repo.List
- [Phase 41]: Plan 01: -progress fixture captured via -progress <file> (not pipe:N) -- the pinned av-worker docker image's ffmpeg has no pipe protocol registered ('Protocol not found'); block content is transport-agnostic, but Plan 02's streaming exec must pick a different transport than pipe:3
- [Phase 41]: Plan 01: progressEMAAlpha=0.3 / progressWarmupSamples=3 shipped as plain package constants, not env vars (Open Question 2 resolved) -- only Plan 03's throttle gates get env-tunable ceremony

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260712-cqg | Fix Phase 16 verification gaps CR-01/WR-01: advisory-lock connection lifecycle (Release pool slot on TryAcquire error; add PGAdvisoryLock.Close and call at webhook-worker shutdown) | 2026-07-12 | 1f8b22b | [260712-cqg-fix-phase-16-verification-gaps-cr-01-wr-](./quick/260712-cqg-fix-phase-16-verification-gaps-cr-01-wr-/) |
| 260719-2f5 | OpenAPI 3.1 спека API (docs/openapi.yaml, 14 операций, ApiKey auth, multipart POST /v1/jobs с аудио-примерами) для импорта в Yaak + docs/README.md; redocly lint 0 errors | 2026-07-19 | 43799af | [260719-2f5-openapi-3-1-api-octoconv-docs-yaak](./quick/260719-2f5-openapi-3-1-api-octoconv-docs-yaak/) |
| 260724-26g | Актуализация README под shipped v1.0–v1.8 (5 движков image/document/html/audio/video, presets + /v1/formats, PDF/A+veraPDF, CFB, k8s/Helm/KEDA, MCP; cmd/ дерево 12 бинарников, config-таблица, Roadmap, Contributing) | 2026-07-23 | 50bade0 | [260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8](./quick/260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8/) |

### Pending Todos

- Plan Phase 38 (Schema Foundation): additive nullable migration `0007_job_progress.sql` (`progress_pct`, `eta_seconds`, `progress_updated_at`, `source_duration_seconds`). Zero-risk, no research needed per SUMMARY.md — cheap, mechanical opener that unblocks Phase 40 (denormalized read) and Phase 41 (progress/ETA writes).

### Blockers/Concerns

None currently blocking the overall roadmap. Sequencing carried into the roadmap:

- Hard-ordered spine: 38 (schema, independent, first) → 39 (cancellation, validates the new `internal/cancel` Redis pub/sub pattern) → 40 (job listing, parallelizable with 39 in principle, but reads Phase 38's denormalized column) → 41 (progress core, riskiest, sequenced after 39 so the Redis cancel-signal pattern is proven) → 42 (live push, pure consumer of Phase 41's data).
- Phase 41 must be split into two verification tracks per research: (a) ffmpeg `-progress` parsing logic vs a REAL captured n8.1.2 fixture (normal / unknown-duration / non-monotonic / >100% cases), and (b) the progress-pipe-to-exec wiring as its OWN live-gated plan with a SIGKILL-mid-stream test — do not conflate the two.
- Phase 39's cancellation races span three existing subsystems simultaneously (guarded transitions, reconciler stale-sweep, asynq retry classification) — solve with mechanisms that already exist (transition allow-list, the "already active/done/canceled" `SkipRetry` idiom), not new ad-hoc code.
- Phase 40's `GET /v1/jobs` needs a dedicated cross-client leak test (two real API keys), not inferred from the existing single-job 404 test — offset-drift, N+1, and SQL-layer `client_id` enforcement are all named pitfalls.
- Re-check asynq `Inspector.CancelProcessing` behavior vs the open asynq#987 issue at the version actually pinned at Phase 39 implementation time (still v0.26.0 as of roadmapping; no upgrade expected to fix it).
- Live-verify SSE behavior against the real reverse-proxy/ingress topology in Phase 42, not just localhost — proxy buffering is a named pitfall for long-lived streaming responses.
- Operational discipline (OrbStack): pre-build all images sequentially with non-`latest` tags; never run compose and k8s stacks hot simultaneously (four confirmed daemon wedges on record, per v1.6/v1.7/v1.8 history).
- Phase 39 Plan 04 Task 3 (checkpoint:human-verify, gate=blocking): scripts/cancel-liveproof.sh authored and its PID-capture logic live-verified in isolation, but the script has NOT been run end-to-end against a live docker-compose stack. CNCL-03 stays Pending until an operator runs it and confirms the PASS summary (see 39-04-SUMMARY.md Operator Runbook).

## Deferred Items

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

Last session: 2026-07-25T05:00:11.312Z
Stopped at: Completed 41-01-PLAN.md
Resume file: None

## Operator Next Steps

- Start Phase 38 with `/gsd:plan-phase 38`
