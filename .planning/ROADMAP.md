# Roadmap: OctoConv

## Milestones

- ✅ **v1.0 Hardening MVP** — Phases 1-4 (shipped 2026-07-08) — see `.planning/milestones/v1.0-ROADMAP.md`
- ✅ **v1.1 Tech Debt Cleanup** — Phases 5-7 (shipped 2026-07-08) — see `.planning/milestones/v1.1-ROADMAP.md`
- ✅ **v1.2 Document Engine Class** — Phases 8-11 (shipped 2026-07-10) — see `.planning/milestones/v1.2-ROADMAP.md`
- ✅ **v1.3 Document Class v2** — Phases 12-16 (shipped 2026-07-12) — see `.planning/milestones/v1.3-ROADMAP.md`
- ✅ **v1.4 CI, Presets & Debt Cleanup** — Phases 17-19 (shipped 2026-07-13) — see `.planning/milestones/v1.4-ROADMAP.md`
- ✅ **v1.5 MCP Access & Document Fidelity** — Phases 20-23 (shipped 2026-07-13) — see `.planning/milestones/v1.5-ROADMAP.md`
- ✅ **v1.6 Kubernetes & KEDA** — Phases 24-28 (shipped 2026-07-17) — see `.planning/milestones/v1.6-ROADMAP.md`
- ✅ **v1.7 Audio Engine & Hardening** — Phases 29-33 (shipped 2026-07-18) — see `.planning/milestones/v1.7-ROADMAP.md`
- ✅ **v1.8 AV Engine (video/ffmpeg)** — Phases 34-37 (shipped 2026-07-23) — see `.planning/milestones/v1.8-ROADMAP.md`
- 🚧 **v1.9 Job Progress & Control** — Phases 38-42 (in progress, started 2026-07-24)

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

<details>
<summary>✅ v1.0 Hardening MVP (Phases 1-4) — SHIPPED 2026-07-08</summary>

- [x] Phase 1: Merge, Auth & Rate Limiting (4/4 plans) — completed 2026-07-04
- [x] Phase 2: Webhook Delivery (3/3 plans) — completed 2026-07-04
- [x] Phase 3: Retry-Safety & Reconciler (3/3 plans) — completed 2026-07-06
- [x] Phase 4: Content Validation, Storage Lifecycle & Observability (5/5 plans) — completed 2026-07-07

Full details: `.planning/milestones/v1.0-ROADMAP.md`

</details>

<details>
<summary>✅ v1.1 Tech Debt Cleanup (Phases 5-7) — SHIPPED 2026-07-08</summary>

- [x] Phase 5: Webhook SSRF Private-IP Opt-Out (1/1 plans) — completed 2026-07-08
- [x] Phase 6: Reconciler Webhook-Gap Sweep & Staleness Soak Test (4/4 plans) — completed 2026-07-08
- [x] Phase 7: Image Dimension Limit (Decompression-Bomb Protection) (2/2 plans) — completed 2026-07-08

Full details: `.planning/milestones/v1.1-ROADMAP.md`

</details>

<details>
<summary>✅ v1.2 Document Engine Class (Phases 8-11) — SHIPPED 2026-07-10</summary>

- [x] Phase 8: Document Content Safety & Format Detection (2/2 plans) — completed 2026-07-09
- [x] Phase 9: LibreOffice Converter Engine (2/2 plans) — completed 2026-07-09
- [x] Phase 10: Document Worker & Reconciler Integration (4/4 plans) — completed 2026-07-09
- [x] Phase 11: API Routing & End-to-End Document Conversion (4/4 plans, incl. gap closure 11-04) — completed 2026-07-10

Full details: `.planning/milestones/v1.2-ROADMAP.md`

</details>

<details>
<summary>✅ v1.3 Document Class v2 (Phases 12-16) — SHIPPED 2026-07-12</summary>

- [x] Phase 12: Tech Debt Cleanup (1/1 plans) — completed 2026-07-10
- [x] Phase 13: Cross-Format Conversion & Input Safety (3/3 plans) — completed 2026-07-10
- [x] Phase 14: Validated Conversion Options & PDF/A Export (3/3 plans) — completed 2026-07-10
- [x] Phase 15: HTML→PDF Chromium Engine (5/5 plans) — completed 2026-07-11
- [x] Phase 16: Webhook Delivery Decoupling (5/5 plans, incl. gap closure 16-05) — completed 2026-07-12

Full details: `.planning/milestones/v1.3-ROADMAP.md`

</details>

<details>
<summary>✅ v1.4 CI, Presets & Debt Cleanup (Phases 17-19) — SHIPPED 2026-07-13</summary>

- [x] Phase 17: Tech Debt Cleanup (2/2 plans) — completed 2026-07-12
- [x] Phase 18: Presets (4/4 plans) — completed 2026-07-12
- [x] Phase 19: CI Pipeline (2/2 plans) — completed 2026-07-13

Full details: `.planning/milestones/v1.4-ROADMAP.md`

</details>

<details>
<summary>✅ v1.5 MCP Access & Document Fidelity (Phases 20-23) — SHIPPED 2026-07-13</summary>

- [x] Phase 20: Presets REST CRUD & Format Discovery (2/2 plans) — completed 2026-07-13
- [x] Phase 21: MCP Server (3/3 plans) — completed 2026-07-13
- [x] Phase 22: CFB Encrypted-vs-Legacy Classification (2/2 plans) — completed 2026-07-13
- [x] Phase 23: veraPDF ISO 19005 Validation (3/3 plans) — completed 2026-07-13

Full details: `.planning/milestones/v1.5-ROADMAP.md`

</details>

<details>
<summary>✅ v1.6 Kubernetes & KEDA (Phases 24-28) — SHIPPED 2026-07-17</summary>

- [x] Phase 24: Helm Chart Core & Landmine Closure (3/3 plans) — completed 2026-07-14
- [x] Phase 25: MCP Streamable HTTP (3/3 plans) — completed 2026-07-14
- [x] Phase 26: Operator System-Presets REST (2/2 plans) — completed 2026-07-14
- [x] Phase 27: KEDA Autoscaling (3/3 plans) — completed 2026-07-16
- [x] Phase 28: Autoscale Load-Proof (3/3 plans) — completed 2026-07-17

Full details: `.planning/milestones/v1.6-ROADMAP.md`

</details>

<details>
<summary>✅ v1.7 Audio Engine & Hardening (Phases 29-33) — SHIPPED 2026-07-18</summary>

- [x] Phase 29: v1.6 Hardening Tail (3/3 plans) — completed 2026-07-17
- [x] Phase 30: Audio Engine Foundation (3/3 plans) — completed 2026-07-18
- [x] Phase 31: Queue, Worker & Routing Integration (4/4 plans) — completed 2026-07-18
- [x] Phase 32: Containerization & Local E2E + RTF Gate (5/5 plans) — completed 2026-07-18
- [x] Phase 33: KEDA/Helm Chart Integration (3/3 plans) — completed 2026-07-18

Full details: `.planning/milestones/v1.7-ROADMAP.md`

</details>

<details>
<summary>✅ v1.8 AV Engine (video/ffmpeg) (Phases 34-37) — SHIPPED 2026-07-23</summary>

- [x] Phase 34: AV Engine Foundation (3/3 plans) — completed 2026-07-20
- [x] Phase 35: Queue, Worker & Routing Integration (7/7 plans) — completed 2026-07-22
- [x] Phase 36: Containerization & RTF-Measured Timeout (5/5 plans) — completed 2026-07-23
- [x] Phase 37: KEDA/Helm Chart Integration (3/3 plans) — completed 2026-07-23

Full details: `.planning/milestones/v1.8-ROADMAP.md`

</details>

### 🚧 v1.9 Job Progress & Control (In Progress)

**Milestone Goal:** Сделать длинные конвертации прозрачными и управляемыми — клиент видит прогресс/ETA в реальном времени, может листать свои джобы и отменять их.

- [x] **Phase 38: Schema Foundation** - Job rows gain the nullable progress/ETA columns every later phase depends on, with zero behavior change (completed 2026-07-24)
- [x] **Phase 39: Cancellation (end-to-end)** - Client can cancel a queued/active job and trust the real in-flight engine process is dead — never resurrected (completed 2026-07-24)
- [x] **Phase 40: Job Listing** - Client can list and page through their own jobs with no cross-client leakage and no pagination drift (completed 2026-07-25)
- [x] **Phase 41: Progress Core** - Client sees an honest, real-time `progress_pct`/`eta_seconds` for in-flight av/audio jobs — never fabricated (completed 2026-07-25)
- [ ] **Phase 42: Live Push** - Client can watch progress via SSE or an opt-in webhook event instead of polling

## Phase Details

### Phase 38: Schema Foundation
**Goal**: Job rows can store progress/ETA state (percent, ETA, last-progress-write timestamp, source duration) so every later v1.9 phase has a stable, additive schema to build on — with zero behavior change for existing clients.
**Depends on**: Nothing (first phase of v1.9; continues from Phase 37, v1.8)
**Requirements**: None directly (foundational; unblocks PROG-01..05 in Phase 41 and the denormalized read in JLIST-02, Phase 40)
**Success Criteria** (what must be TRUE):
  1. Migration `0007_job_progress.sql` applies cleanly via `cmd/migrate` against both a fresh database and the existing populated `jobs` table.
  2. `jobs` gains four nullable columns — `progress_pct`, `eta_seconds`, `progress_updated_at`, `source_duration_seconds` — with no `NOT NULL`/default that could break existing inserts from any current engine worker.
  3. `GET /v1/jobs/{id}` responses for existing jobs are unchanged (new columns not yet surfaced in the API layer) — confirms zero regression to shipped behavior.
  4. `canceled` requires no migration of its own: a direct DB insert/update proves the existing `jobs.status` CHECK constraint and Go `StatusCanceled` constant (both present since `0001_init.sql`) already accept it.
**Plans**: 1 plan
- [x] 38-01-PLAN.md — Additive migration 0007_job_progress.sql: four nullable progress/ETA/duration/throttle columns + fail-closed CHECKs, zero behavior change

### Phase 39: Cancellation (end-to-end)
**Goal**: A client can cancel a `queued` or `active` job and trust that the real underlying engine process is dead — not just that the database row says `canceled` — with no resurrection by the reconciler or asynq's own retry mechanics.
**Depends on**: Phase 38 (schema; `canceled` itself needs no new columns, but sequenced per the research-recommended build order to validate the new Redis pub/sub pattern before Phase 41 reuses it)
**Requirements**: CNCL-01, CNCL-02, CNCL-03, CNCL-04
**Success Criteria** (what must be TRUE):
  1. `POST /v1/jobs/{id}/cancel` transitions a `queued` or `active` job owned by the caller to `canceled` and returns 200; a job owned by another client or not found returns 404 (never 403) — mirroring the existing ownership model.
  2. Calling cancel again on an already-terminal job (`done`/`failed`/`canceled`) returns 200 with the job's current status and never overwrites a terminal outcome.
  3. Cancelling an `active` job actually kills the real in-flight engine process group (ffmpeg/whisper-cli/soffice/…) via the existing hardened `runCommand` SIGKILL path — verified by a live process-table check that the child is genuinely dead, not just that the DB row reads `canceled`.
  4. A `canceled` job is never resurrected: a dedicated regression test proves the reconciler's stale-sweep excludes `canceled` jobs, and `errors.Is`/`SkipRetry` classification stops asynq from retrying a canceled task — with the Postgres guarded-transition allow-list as the authoritative backstop, independent of the handler's own `context.Canceled` check (which the confirmed asynq#987 race can bypass).
**Plans**: 4 plans
- [x] 39-01-PLAN.md — Repo.Cancel (guarded transition) + POST /v1/jobs/{id}/cancel handler + route (CNCL-01, CNCL-02)
- [x] 39-02-PLAN.md — internal/cancel package: in-memory cancel-func registry + job:cancel Redis pub/sub, zero new deps (CNCL-03)
- [x] 39-03-PLAN.md — Worker register/deregister + context.Canceled SkipRetry arm; api publisher + every engine worker subscriber wiring (CNCL-03)
- [x] 39-04-PLAN.md — Non-resurrection regression (FindStale/allow-list/webhook-gap) + live ffmpeg process-death proof (CNCL-04, CNCL-03) — Tasks 1-2 done (regression tests + scripts/cancel-liveproof.sh authored); Task 3 (operator live-run checkpoint) PENDING, see 39-04-SUMMARY.md

### Phase 40: Job Listing
**Goal**: A client can list and page through their own jobs — status, progress, filters — without ever seeing another client's data and without results drifting under this table's continuous inserts.
**Depends on**: Phase 38 (reads the denormalized `progress_pct` column added there)
**Requirements**: JLIST-01, JLIST-02
**Success Criteria** (what must be TRUE):
  1. `GET /v1/jobs` returns only jobs belonging to the authenticated caller, enforced at the SQL layer (`WHERE client_id = $1`, never Go-side filtering) — a live two-API-key test proves zero cross-client leakage.
  2. Optional `status` and `engine` query filters narrow the result set correctly and can be combined.
  3. Pagination is keyset-based (stable cursor on `created_at, id`) and remains correct even while new jobs are inserted concurrently during a live paging test; the server enforces a max page size regardless of client-requested size.
  4. Each returned job includes `status` and `progress_pct` read directly from the `jobs` row (no N+1 query per listed job).
**Plans**: 2 plans
- [x] 40-01-PLAN.md — internal/jobs: JobSummary + opaque cursor codec + client-scoped keyset Repo.List (+ two-client leak & keyset-stability DB tests)
- [x] 40-02-PLAN.md — internal/api: GET /v1/jobs handler (fail-closed filters, page-size clamp, envelope) + env wiring

### Phase 41: Progress Core
**Goal**: A client polling `GET /v1/jobs/{id}` for an in-flight av/audio job sees an honest, real-time `progress_pct` and `eta_seconds` — genuinely `null` (never fabricated) for engines with no machine-readable signal — and the ffmpeg progress pipe never regresses the hardened process-group-kill contract.
**Depends on**: Phase 38 (schema), Phase 39 (validates the `internal/cancel` Redis pub/sub pattern this phase's cancel-mid-progress path reuses)
**Requirements**: PROG-01, PROG-02, PROG-03, PROG-04, PROG-05
**Success Criteria** (what must be TRUE):
  1. `GET /v1/jobs/{id}` on an in-flight av/audio job returns a monotonically increasing `progress_pct` (0-100) driven by ffmpeg `-progress` output, verified against a real captured fixture from the pinned n8.1.2 binary (`out_time_ms` correctly interpreted as microseconds, not milliseconds); image/document/html jobs and the whisper transcribe stage report `null`.
  2. `source_duration_seconds` is persisted on the job row from the existing ffprobe probe as the percent denominator for av/audio jobs, and stays `null` for non-ffmpeg engines.
  3. A live multi-job test confirms progress writes never contend with the guarded status-transition lock and never append to `job_events`; writes are throttled by BOTH a time interval AND a ≥1% delta, and the terminal 100% write always lands even under throttling.
  4. `eta_seconds` reflects ffmpeg's `speed=` field through a warm-up/smoothing window, returning `null` before it is derivable.
  5. A live SIGKILL-mid-stream test (timeout or cancel) on a job using the progress pipe leaves zero orphaned engine processes AND zero leaked reader goroutines — verified by live process-table/goroutine inspection, not just `go test -race`.
**Plans**: 4 plans
- [x] 41-01-PLAN.md — Progress -progress block parser + percent/EMA-ETA math + seam contracts (TDD, fixture-tested)
- [x] 41-02-PLAN.md — runCommandStreaming fd-3 leak-safe exec + AVConverter.ConvertWithProgress ramp (exec.go unchanged)
- [x] 41-03-PLAN.md — Non-locking UpdateProgress + throttle sink + terminal-100/freeze + null-aware GET surfacing + env knobs
- [x] 41-04-PLAN.md — Live SIGKILL-mid-stream operator gate (zero orphaned procs + zero leaked goroutines + frozen pct) — operator live-run PASS 2026-07-25 (40/40, scripts/av-progress-liveproof.sh)

### Phase 42: Live Push
**Goal**: A client can watch a job's progress in real time — via SSE or an opt-in webhook event — without polling, and without a stale progress delivery ever competing with the terminal event's durable-retry budget.
**Depends on**: Phase 41 (pure consumer of progress/ETA data)
**Requirements**: PUSH-01, PUSH-02
**Success Criteria** (what must be TRUE):
  1. `GET /v1/jobs/{id}/events` streams live progress via SSE under the same ApiKey auth + 404-ownership check as every other route; a cross-client request gets 404.
  2. The SSE stream is DB-poll sourced (so any `api` replica can serve it, keeping the API stateless), sends heartbeat comments to survive proxy idle-timeouts, and closes cleanly with no leaked goroutines on terminal job state or client disconnect.
  3. A job created with the opt-in progress-webhook flag (alongside `callback_url`) receives throttled progress webhook events carrying a new `event` discriminator field.
  4. A failed progress-webhook delivery is dropped as best-effort/single-attempt and never enters the bounded-retry/dead-letter queue reserved for the terminal `done`/`failed` event.
**Plans**: 4 plans (2 waves)
- [x] 42-01-PLAN.md — Schema + opt-in foundation: 0008 migration + progress_webhook threaded through jobs/repo + parsed at job create (PUSH-02)
- [x] 42-02-PLAN.md — SSE endpoint: DB-poll, emit-on-change GET /v1/jobs/{id}/events + route + env knobs + httptest leak/framing tests (PUSH-01)
- [ ] 42-03-PLAN.md — Progress-webhook delivery: event discriminator on both bodies + detached best-effort inline fire + av-worker deliverer/secret wiring (PUSH-02)
- [ ] 42-04-PLAN.md — SSE live-proof: operator gate streaming a real av job through the full stack (PUSH-01, checkpoint)

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|-----------------|--------|-----------|
| 1. Merge, Auth & Rate Limiting | v1.0 | 4/4 | Complete | 2026-07-04 |
| 2. Webhook Delivery | v1.0 | 3/3 | Complete | 2026-07-04 |
| 3. Retry-Safety & Reconciler | v1.0 | 3/3 | Complete | 2026-07-06 |
| 4. Content Validation, Storage Lifecycle & Observability | v1.0 | 5/5 | Complete | 2026-07-07 |
| 5. Webhook SSRF Private-IP Opt-Out | v1.1 | 1/1 | Complete | 2026-07-08 |
| 6. Reconciler Webhook-Gap Sweep & Staleness Soak Test | v1.1 | 4/4 | Complete | 2026-07-08 |
| 7. Image Dimension Limit (Decompression-Bomb Protection) | v1.1 | 2/2 | Complete | 2026-07-08 |
| 8. Document Content Safety & Format Detection | v1.2 | 2/2 | Complete | 2026-07-09 |
| 9. LibreOffice Converter Engine | v1.2 | 2/2 | Complete | 2026-07-09 |
| 10. Document Worker & Reconciler Integration | v1.2 | 4/4 | Complete | 2026-07-09 |
| 11. API Routing & End-to-End Document Conversion | v1.2 | 4/4 | Complete | 2026-07-10 |
| 12. Tech Debt Cleanup | v1.3 | 1/1 | Complete    | 2026-07-10 |
| 13. Cross-Format Conversion & Input Safety | v1.3 | 3/3 | Complete    | 2026-07-10 |
| 14. Validated Conversion Options & PDF/A Export | v1.3 | 3/3 | Complete    | 2026-07-10 |
| 15. HTML→PDF Chromium Engine | v1.3 | 5/5 | Complete    | 2026-07-11 |
| 16. Webhook Delivery Decoupling | v1.3 | 5/5 | Complete | 2026-07-12 |
| 17. Tech Debt Cleanup | v1.4 | 2/2 | Complete | 2026-07-12 |
| 18. Presets | v1.4 | 4/4 | Complete | 2026-07-12 |
| 19. CI Pipeline | v1.4 | 2/2 | Complete | 2026-07-13 |
| 20. Presets REST CRUD & Format Discovery | v1.5 | 2/2 | Complete | 2026-07-13 |
| 21. MCP Server | v1.5 | 3/3 | Complete | 2026-07-13 |
| 22. CFB Classification | v1.5 | 2/2 | Complete | 2026-07-13 |
| 23. veraPDF ISO 19005 Validation | v1.5 | 3/3 | Complete | 2026-07-13 |
| 24. Helm Chart Core & Landmine Closure | v1.6 | 3/3 | Complete | 2026-07-14 |
| 25. MCP Streamable HTTP | v1.6 | 3/3 | Complete | 2026-07-14 |
| 26. Operator System-Presets REST | v1.6 | 2/2 | Complete    | 2026-07-14 |
| 27. KEDA Autoscaling | v1.6 | 3/3 | Complete    | 2026-07-16 |
| 28. Autoscale Load-Proof | v1.6 | 3/3 | Complete    | 2026-07-17 |
| 29. v1.6 Hardening Tail | v1.7 | 3/3 | Complete    | 2026-07-17 |
| 30. Audio Engine Foundation | v1.7 | 3/3 | Complete    | 2026-07-18 |
| 31. Queue, Worker & Routing Integration | v1.7 | 4/4 | Complete    | 2026-07-18 |
| 32. Containerization & Local E2E + RTF Gate | v1.7 | 5/5 | Complete    | 2026-07-18 |
| 33. KEDA/Helm Chart Integration | v1.7 | 3/3 | Complete    | 2026-07-18 |
| 34. AV Engine Foundation | v1.8 | 3/3 | Complete    | 2026-07-20 |
| 35. Queue, Worker & Routing Integration | v1.8 | 7/7 | Complete    | 2026-07-22 |
| 36. Containerization & RTF-Measured Timeout | v1.8 | 5/5 | Complete    | 2026-07-23 |
| 37. KEDA/Helm Chart Integration | v1.8 | 3/3 | Complete    | 2026-07-23 |
| 38. Schema Foundation | v1.9 | 1/1 | Complete    | 2026-07-24 |
| 39. Cancellation (end-to-end) | v1.9 | 4/4 | Complete    | 2026-07-24 |
| 40. Job Listing | v1.9 | 2/2 | Complete   | 2026-07-25 |
| 41. Progress Core | v1.9 | 4/4 | Complete | 2026-07-25 |
| 42. Live Push | v1.9 | 2/4 | In Progress|  |

---

*v1.8 shipped 2026-07-23. v1.9 (Job Progress & Control) roadmapped 2026-07-24, Phases 38-42. Next: `/gsd:plan-phase 38`.*
