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
- ✅ **v1.9 Job Progress & Control** — Phases 38-42 (shipped 2026-07-25) — see `.planning/milestones/v1.9-ROADMAP.md`
- ✅ **v1.10 OCR Engine (text extraction)** — Phases 43-46 (shipped 2026-07-27) — see `.planning/milestones/v1.10-ROADMAP.md`
- ✅ **v1.11 Pandoc Engine** — Phases 47-50 (shipped 2026-08-02) — see `.planning/milestones/v1.11-ROADMAP.md`
- 🚧 **v1.12 Consumer Accounting & Fairness** — Phases 51-55 (in progress)

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

<details>
<summary>✅ v1.9 Job Progress & Control (Phases 38-42) — SHIPPED 2026-07-25</summary>

**Milestone Goal:** Сделать длинные конвертации прозрачными и управляемыми — клиент видит прогресс/ETA в реальном времени, может листать свои джобы и отменять их.

- [x] Phase 38: Schema Foundation (1/1 plans) — completed 2026-07-24
- [x] Phase 39: Cancellation (end-to-end) (4/4 plans) — completed 2026-07-24
- [x] Phase 40: Job Listing (2/2 plans) — completed 2026-07-25
- [x] Phase 41: Progress Core (4/4 plans) — completed 2026-07-25
- [x] Phase 42: Live Push (4/4 plans) — completed 2026-07-25

Full details: `.planning/milestones/v1.9-ROADMAP.md`

</details>

<details>
<summary>✅ v1.10 OCR Engine (text extraction) (Phases 43-46) — SHIPPED 2026-07-27</summary>

**Milestone Goal:** Клиент может извлечь машиночитаемый текст из изображений и сканированных PDF через тот же async-контур, что и остальные движки — offline, детерминированно, с онлайн-прогрессом. Шестой engine-класс.

- [x] Phase 43: OCR Engine Foundation - Standalone `OCRConverter` extracts txt/hOCR from images and scanned PDFs, streaming page-by-page — memory-bomb-safe and born-digital-PDF-safe from the first line, before any queue/worker plumbing (completed 2026-07-26)
- [x] Phase 44: Async Pipeline Wiring - OCR jobs flow end-to-end through the async pipeline (queue, `cmd/ocr-worker`, retry, reconciler, API routing) over the completely unmodified v1.9 progress/SSE/webhook machinery (completed 2026-07-26)
- [x] Phase 45: Containerization + Measured Timeout Gate - A running `ocr-worker` container passes a live E2E, with `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` sized from a measured seconds-per-page gate rather than a copied constant (completed 2026-07-27)
- [x] Phase 46: KEDA/Helm Chart Integration - The OCR class autoscales in the cluster with production parity to the other five engine classes, scale-from-zero live-proven (completed 2026-07-27)

Full details: `.planning/milestones/v1.10-ROADMAP.md`

</details>

<details>
<summary>✅ v1.11 Pandoc Engine (Phases 47-50) — SHIPPED 2026-08-02</summary>

**Milestone Goal:** Седьмой engine-класс — pandoc markup/document interconvert (md ↔ rst ↔ org ↔ latex ↔ epub ↔ html ↔ docx), только markup↔markup (без →pdf), через тот же async-контур, что и остальные шесть движков.

- [x] Phase 47: Pandoc Engine Foundation (3/3 plans) — completed 2026-07-30
- [x] Phase 48: Async Pipeline Wiring (3/3 plans) — completed 2026-07-30
- [x] Phase 49: Containerization + Measured Timeout Gate (4/4 plans) — completed 2026-08-01
- [x] Phase 50: KEDA/Helm Chart Integration (4/4 plans) — completed 2026-08-01

Full details: `.planning/milestones/v1.11-ROADMAP.md`

</details>

### 🚧 v1.12 Consumer Accounting & Fairness (In Progress)

**Milestone Goal:** превратить OctoConv из «сервиса, который умеет конвертировать» в общую платформу, которая знает своих потребителей — кто сколько потребляет, в каких границах, и как не дать одному потребителю выжрать класс движка целиком.

- [x] **Phase 51: Attribution, Accounting & Retention** - Per-client job outcomes and consumption are tracked in Postgres and Prometheus without multiplying existing metric cardinality or repeating the platform's unbounded-table-growth debt (completed 2026-08-02)
- [x] **Phase 52: Quota Enforcement & Intra-Class Fairness** - Operator-settable per-client limits are enforced fail-closed on every job-creation path (REST + both MCP entry points), rolled out via an observe-only period, and structurally prevent one client from starving others within an engine class (completed 2026-08-03)
- [x] **Phase 53: Operator Usage Visibility** - Operator can answer "who consumed what, and what are this client's limits/remaining budget" through a dedicated endpoint, without reading raw Prometheus (7/7 plans; initial verification 9/10 `gaps_found` on SC2, closed by 53-05..53-07, re-verified 10/10 `passed`) (completed 2026-08-05)
- [ ] **Phase 54: Per-Class Capacity Floor (SEED-011)** - `minReplicaCount` becomes operator-selectable per engine class instead of the hardcoded zero
- [ ] **Phase 55: Live Gate — Multi-Replica Quota Correctness** - Quota admission is proven correct under 2+ concurrent API replicas via a live run, not merely assumed from the atomic-Postgres design

## Phase Details

### Phase 51: Attribution, Accounting & Retention

**Goal**: Every job's client and consumption (bytes, count, engine class) is tracked in Postgres and exposed in Prometheus with a purpose-built, cardinality-bounded shape — and the new accounting data does not repeat OctoConv's existing unbounded-Postgres-growth gap (`jobs`/`job_events` rows that never age out).
**Depends on**: Nothing (first phase of v1.12 — builds directly on the stable v1.0–v1.11 platform; everything downstream in this milestone reads the data this phase produces)
**Requirements**: ACCT-01, ACCT-02, ACCT-03, ACCT-04, RETN-01
**Success Criteria** (what must be TRUE):

  1. A new, purpose-built Prometheus counter carries a `client_id` label on job outcomes, while `octoconv_job_duration_seconds` stays engine/status-only and `octoconv_queue_depth`'s label set remains exactly `{queue, state}` — proven by a pinning test that fails loudly on any future diff.
  2. Every job's terminal transition (`done`/`failed`, including jobs recovered by the reconciler) increments a Postgres daily usage rollup (jobs count, bytes in/out, per client/engine/day) exactly once, and the rollup write happens **inside the same transaction as the guarded status transition** — not as an adjacent call after it. A worker that dies between a committed `MarkDone` and a separate accounting write would leave the job terminal and the usage permanently unrecorded, since neither an asynq retry nor the reconciler revisits a terminal job. Proven by a soak test that deliberately forces the reconciler's documented stale-recovery race and asserts the usage total, not just the recovery count. *(Tightened 2026-08-02 after PR #3 review.)*
  3. `client_id` label cardinality is bounded by an explicit **configured numeric ceiling** with defined overflow behavior (clients beyond the ceiling collapse into a single stable `overflow` label value), not merely restricted to IDs present in `clients` — `cmd/manage-clients` can add valid clients indefinitely, so "a real client id" is not a bound. An unrecognized or malformed ID never mints a new series. *(Tightened 2026-08-02 after PR #3 review.)*
  4. Retention is **executable, not documentary**: a concrete purge / partition-drop / archival mechanism runs and is proven to actually remove rows past the window (a test that ages data and asserts it is gone), covering both the new usage table (which grows with `clients × engines × days`, not raw job volume) and the pre-existing `jobs`/`job_events` gap. A retention policy described only in a migration comment does not satisfy RETN-01. *(Tightened 2026-08-02 after PR #3 review.)*
  5. Re-running the existing KEDA scale-from-zero live gate after the new metrics/table land shows no regression — accounting is additive to the platform's operational surface, not a new blast radius for autoscaling.
  6. Byte accounting sums over ALL of a job's inputs and outputs (`Repo.Inputs`/`Repo.Outputs`, `ORDER BY ordinal`) rather than reading ordinal 0 — so a job that ever produces or consumes more than one file is counted correctly without an accounting rewrite. See the constraint note below.

**Constraint — do not hardcode one-input/one-output (added 2026-08-02, SEED-017):**
Today every job is exactly one input, one engine invocation, one output, so
reading `job_inputs`/`job_outputs` at ordinal 0 would work and would be the
natural thing to write. It must not be written that way. The schema has carried
an `ordinal` column on both tables since `0001_init.sql`, `internal/jobs/repo.go`
already exposes `Repo.Inputs`/`Repo.Outputs` ordered by it, and SEED-017 (PDF
split/merge) is the first concrete demand for N — split alone produces N outputs
from one input. Baking the 1:1 assumption into the accounting layer would make
byte totals silently wrong the moment that lands, and retrofitting it is exactly
the multi-site value drift that bit this project twice during v1.11. Summing over
the lists costs nothing now.

**Plans**: 5 plans

Plans:
**Wave 1**

- [x] 51-01-PLAN.md — client_usage_daily migration + cardinality-bounded per-client counter (additive, zero behavior change)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 51-02-PLAN.md — rollup UPSERT inside the guarded terminal-transition transaction; MarkDone/MarkFailed widened

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 51-03-PLAN.md — call-site propagation across 7 engine handlers + reconciler; forced stale-recovery race proof

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 51-04-PLAN.md — executable batched retention purge on the reconciler tick + env plumbing

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 51-05-PLAN.md — autoscaling-contract zero-diff proof + KEDA scale-from-zero live gate (operator-gated)

**Autonomous**: false (Plan 05 is an operator-run live gate)

### Phase 52: Quota Enforcement & Intra-Class Fairness

**Goal**: An operator can cap what a client may consume (concurrent jobs, daily volume, allowed engines), enforcement is fail-closed on every job-creation path and rolled out safely to already-integrated consumers, and the same admission mechanism gives a structural guarantee that one client cannot starve others within an engine class — all without touching queue topology or the KEDA contract.
**Depends on**: Phase 51 (tiers 2/3 of the admission gate read/write the `client_usage_daily` rollup; the concurrency cap reads `jobs.status`, already ground truth, but the byte/volume checks need Phase 51's table to exist and be populated)
**Requirements**: QUOT-01, QUOT-02, QUOT-03, QUOT-04, FAIR-01, FAIR-02
**Success Criteria** (what must be TRUE):

  1. An operator can set per-client limits (concurrent jobs per engine, daily byte/job volume, allowed engine set) where the absence of a limit is a literal `NULL` meaning unlimited — never a large sentinel number — and no existing client's behavior changes until an operator explicitly sets one.
  2. Every job-creation entry point (`POST /v1/jobs`, `cmd/mcp-server`, `cmd/mcp-http`) enforces the identical fail-closed admission check before the S3 write and before enqueue — proven by an integration test that exercises both REST and at least one MCP path, not assumed from proxying. Two properties are load-bearing and must be designed in, not discovered: **(a)** the check-and-admit is serialized per (client, engine) — a bare `SELECT count(*)` followed by an insert lets two API replicas both observe "below cap" and both admit, so the count and the create must share one transaction under a per-client/per-engine advisory lock or an equivalent atomic counter; **(b)** the authoritative reservation happens **before** the upload body is written to S3, with reservation release if a later step fails — otherwise an over-quota client repeatedly burns S3 bandwidth and storage before receiving its 429. *(Added 2026-08-02 after PR #3 review.)*
  2b. Admission reservations and completed usage are **distinguishable**, so a job admitted against the daily budget and later completed is counted once, not twice. The rollup cannot be a single pair of counters that both the admission reserve and the terminal write increment. *(Added 2026-08-02 after PR #3 review.)*

  3. A limit-exceeding request is rejected with `429` + `Retry-After` and a `quota_exceeded` error code distinguishable from the existing `rate_limited` code — never `403` — and an in-flight concurrency slot is released on every terminal state (`done`, `failed`, `reconciler_exhausted`), not only on success.
  4. An observe-only mode computes and exposes what would have been rejected (metric/log, no actual rejection), able to run for at least one full quota window before an operator enables real enforcement for any specific client.
  5. The per-client, per-engine concurrency cap gives a structural guarantee — at cap `K` and `maxReplicaCount` `M`, one client can occupy at most `K` of `M` slots — and this ships with zero diff lines in any `deploy/chart/octoconv/templates/scaledobject-*.yaml` and zero signature changes to the reconciler's `enqueuer` interface, proving `octoconv_queue_depth` semantics and all 7 ScaledObjects' PromQL are unchanged.

**Plans**: 9 plans (5 shipped + 4 gap-closure, planned 2026-08-03 after verification scored 8/9)

Plans:
**Wave 1**

- [x] 52-01-PLAN.md — `client_limits` migration (`(client_id, engine)`, NULL = unlimited) + `clients.enforcement_mode` + limits repo + `manage-clients` subcommands
- [x] 52-02-PLAN.md — observe-only counter through the Phase 51 bounded allocator + `code: "rate_limited"` on the existing 429 + MCP proxy carries code/Retry-After

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 52-03-PLAN.md — admission gate inside `Repo.Create`'s transaction under a per-`(client, engine)` `pg_advisory_xact_lock`; queued+active slot predicate; no release code

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 52-04-PLAN.md — pre-upload gate before the S3 write, 429 + `Retry-After` + `quota_exceeded` (never 403), kill-switch and metrics wiring

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 52-05-PLAN.md — REST + real MCP tool admission proof against one live API, FAIR-02 zero-diff contract gate, OpenAPI/README contract docs

**Wave 5** *(gap closure — 52-VERIFICATION.md status `gaps_found`, 8/9 must-haves)*

- [x] 52-06-PLAN.md — daily quota charges in-flight (queued+active) admitted work inside the advisory-locked transaction; false `K x MAX_INPUT_BYTES` bound deleted; daily-only (`max_concurrent = nil`) test coverage
- [x] 52-07-PLAN.md — `convert.AllEngines()`/`IsKnownEngine()` as the guarded source of truth; `manage-clients set-limit` rejects an unknown engine before any DB write (WR-08)

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 52-08-PLAN.md — migration 0014 widens the usage rollup's status domain; `Cancel` records usage so `create -> cancel -> create` no longer resets the daily cap (WR-01); `Repo.Create`/`transition` pin `pgx.ReadCommitted` (WR-02)

**Wave 7** *(blocked on Wave 6 completion)*

- [x] 52-09-PLAN.md — REQUIREMENTS.md QUOT-01/QUOT-04 sync, README daily-cap semantics corrected, full regression suite + FAIR-02 zero-diff re-proof from `d7ac603`

**Autonomous**: true (Phase 55 is where the operator-gated multi-replica live proof happens)

### Phase 53: Operator Usage Visibility

**Goal**: An operator can answer "who consumed what, by engine class, over what period" and "what are this client's limits and remaining budget" through a dedicated, already-established access-control pattern — not by hand-querying raw Prometheus or the `jobs` table.
**Depends on**: Phase 51 (reads `client_usage_daily`; does not require Phase 52 to be complete, but is sequenced after quota work has started so a "usage vs. configured limit" view is meaningful rather than empty)
**Requirements**: OPSV-01, OPSV-02
**Success Criteria** (what must be TRUE):

  1. An operator, authenticated through the existing `OPERATOR_CLIENT_IDS`-gated mechanism (the Phase 26 precedent), can retrieve per-client, per-engine usage for a given period from a dedicated endpoint, backed by the Phase 51 rollup rather than a raw `jobs` scan or Prometheus query.
  2. Per client, the operator can see the currently configured limits and the remaining budget/quota state, so limits can be set from observed data rather than guessed.
  3. A non-operator caller receives the identical no-leak response the project's existing operator-only routes already guarantee (byte-identical 404, not a distinguishable 403) — no new information-disclosure surface introduced by this endpoint.

**Plans**: 7 plans (4 original + 3 gap closure)

Plans:
**Wave 1**

- [x] 53-01-PLAN.md — jobs.UsageSummary: status-unfiltered client_usage_daily aggregate plus the literal-status in-flight term, pinned to evaluate()'s formula (D-05)
- [x] 53-02-PLAN.md — clients.ListAllLimits: batch limits + enforcement_mode read with an optional single-client narrowing bind (OPSV-02)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 53-03-PLAN.md — api.UsageRepo/LimitsRepo interfaces, widened NewServer, and all three call sites updated with hand-written fakes

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 53-04-PLAN.md — GET /v1/system/usage behind requireOperator: combined usage+limits+remaining response, byte-identical no-leak 404, D-09 caveat fields, OpenAPI spec

**Wave 4** *(gap closure — blocked on Wave 3 completion; closes 53-VERIFICATION.md Gap #1 / SC2 / OPSV-02)*

- [x] 53-05-PLAN.md — Seed the merge key set from every client in meta so an idle unlimited client is visible and distinguishable from a nonexistent client_id, without weakening SC3's no-leak 404
- [x] 53-06-PLAN.md — Exclude ON DELETE SET NULL orphans from both in-flight aggregates (WR-01) and add jobs.Repo.UTCToday so the endpoint and evaluate() read one clock (WR-03)

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 53-07-PLAN.md — Enforcement-aware truth claims: remaining_matches_admission_gate honors the QUOTA_ENFORCEMENT_DISABLED kill-switch and observe mode (WR-02), day range derived from the Postgres clock (WR-03)

### Phase 54: Per-Class Capacity Floor (SEED-011)

**Goal**: An operator can choose a non-zero `minReplicaCount` per engine class to eliminate cold-start latency under sustained load, instead of the hardcoded zero every class ships with today.
**Depends on**: Nothing (fully independent of Phases 51-53 — zero data or code coupling, pure Helm values/`ScaledObject` change; sequenced here as a low-risk hygiene tail so it doesn't add unrelated chart diff noise to the accounting/quota review, per research's explicit "can land at any point" finding)
**Requirements**: CAPA-01
**Success Criteria** (what must be TRUE):

  1. Each of the seven engine classes' `minReplicaCount` is set via a Helm values field instead of a hardcoded `0`, defaulting to today's `0` (zero behavior change for the current deployment).
  2. Setting a non-zero `minReplicaCount` for one engine class does not alter another class's `ScaledObject`/PromQL or its scale-from-zero behavior — confirmed by a template diff touching only the intended class.
  3. `helm lint`/`helm template` render cleanly for both the default (all-zero) values and an example non-zero override, with no live cluster required — this is chart-only, no architectural novelty (research: skip live-gate for this phase).

**Plans**: 2 plans

Plans:
- [ ] 54-01-PLAN.md — Values-driven minReplicaCount + floor-derived fallback.replicas via a validating `_helpers.tpl` define, wired into all 7 ScaledObjects (SC1)
- [ ] 54-02-PLAN.md — `values-capacity-floor.yaml` example override + a `helm` CI job proving per-class isolation, clean render, and that the D-02 validation fires (SC2/SC3)

### Phase 55: Live Gate — Multi-Replica Quota Correctness

**Goal**: Prove, on a live multi-replica setup, that the quota admission counter built in Phase 52 is correct when 2+ API replicas concurrently serve the same client — the one claim in this milestone that cannot be proven statically, matching this project's own precedent (`internal/ratelimit`'s documented `api.replicas: 1`-only caveat) for exactly this class of correctness claim.
**Depends on**: Phase 52 (needs the atomic Postgres admission mechanism built and enforcing before its multi-replica correctness can be exercised)
**Requirements**: QUOT-05
**Success Criteria** (what must be TRUE):

  1. With `api.replicas` set to 2 or more, N concurrent job-creation requests from one client whose concurrency cap is `K` result in at most `K` admitted jobs — observed directly on a live run, never more, regardless of which replica handled which request.
  2. The live run's evidence (timestamps, replica identifiers, admitted/rejected counts) is captured and committed, mirroring this project's existing live-gate discipline (KEDA scale-from-zero, downscale-survival), and the phase records explicitly that the quota mechanism — unlike `internal/ratelimit` — does not carry a replicas-count caveat.

**Plans**: TBD
**Autonomous**: false (operator-gated live proof — requires a real 2+-replica run, cannot be closed by static verification alone)

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
| 42. Live Push | v1.9 | 4/4 | Complete   | 2026-07-25 |
| 43. OCR Engine Foundation | v1.10 | 4/4 | Complete   | 2026-07-26 |
| 44. Async Pipeline Wiring | v1.10 | 4/4 | Complete   | 2026-07-26 |
| 45. Containerization + Measured Timeout Gate | v1.10 | 4/4 | Complete | 2026-07-27 |
| 46. KEDA/Helm Chart Integration | v1.10 | 3/3 | Complete | 2026-07-27 |
| 47. Pandoc Engine Foundation | v1.11 | 3/3 | Complete   | 2026-07-30 |
| 48. Async Pipeline Wiring | v1.11 | 3/3 | Complete   | 2026-07-30 |
| 49. Containerization + Measured Timeout Gate | v1.11 | 4/4 | Complete   | 2026-08-01 |
| 50. KEDA/Helm Chart Integration | v1.11 | 4/4 | Complete    | 2026-08-01 |
| 51. Attribution, Accounting & Retention | v1.12 | 5/5 | Complete    | 2026-08-02 |
| 52. Quota Enforcement & Intra-Class Fairness | v1.12 | 9/9 | Complete   | 2026-08-03 |
| 53. Operator Usage Visibility | v1.12 | 7/7 | Complete    | 2026-08-05 |
| 54. Per-Class Capacity Floor (SEED-011) | v1.12 | 0/TBD | Not started | - |
| 55. Live Gate — Multi-Replica Quota Correctness | v1.12 | 0/TBD | Not started | - |

---

*v1.11 (Pandoc Engine) shipped 2026-08-02, Phases 47-50 — 9/9 requirements, milestone audit passed (9/9 requirements, 6/6 cross-phase seams), 3 live gates (compose E2E, KEDA scale-from-zero, downscale-survival). v1.12 (Consumer Accounting & Fairness) roadmap created 2026-08-02, Phases 51-55 — 15/15 v1 requirements mapped, 11/15 complete (ACCT-01..04, RETN-01, QUOT-01..04, FAIR-01, FAIR-02, per REQUIREMENTS.md's traceability table), with QUOT-05 (Phase 55), OPSV-01/02 (Phase 53) and CAPA-01 (Phase 54) pending. Next: `/gsd:plan-phase 53`.*
</content>
