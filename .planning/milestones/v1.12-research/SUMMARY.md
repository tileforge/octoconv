# Project Research Summary

**Project:** OctoConv — v1.12 "Consumer Accounting & Fairness"
**Domain:** Adding per-consumer attribution, quotas, and intra-class fairness to an already-shipped internal Go async file-conversion platform (7 engine classes, asynq/Redis queues, Postgres system of record, Kubernetes + KEDA scale-from-zero)
**Researched:** 2026-08-02
**Confidence:** HIGH

## Executive Summary

This is not a greenfield "add multi-tenancy" problem — it is an integration problem into a live, already-hardened platform. All four researchers converged independently on the same conclusion from four different angles (library internals, feature landscape, code architecture, failure-mode analysis): **fairness and quotas must be built as Postgres-backed admission control at the API layer, not as queue-topology changes.** asynq v0.26.0, verified against vendored source, has zero intra-queue fairness primitive — `Config.Queues`/`StrictPriority` weight named *queues*, not clients or tasks, and `Group`/`GroupAggregator` batches tasks together (the opposite of isolation). The one design that looks like the "textbook" answer — per-client sub-queues — is explicitly the one design that must **not** be built this milestone: it renames queues, which breaks the PromQL in all 7 `scaledobject-*.yaml` templates, `AllConvertQueues()`, the queue-depth collector, and the reconciler's engine→queue routing switch — the entire scale-from-zero contract this project has spent five milestones proving live. A per-client concurrency cap (admission-time, `K` of `M` = `maxReplicaCount`) gives a *provable, structural* anti-starvation guarantee using the exact mechanism quotas already need, at zero cost to queue topology.

The recommended approach: capture nothing new (all raw usage data — `client_id`, `started_at`/`finished_at`, `job_inputs/outputs.size_bytes` — already exists in the schema), add one new Postgres rollup table (`client_usage_daily`) written atomically from the same choke point every worker already hits (`MarkDone`/`MarkFailed` → `metrics.RecordJobOutcome`), and enforce three admission gates in `handleCreateJob` before the S3 write: a cheap concurrency-cap read, a cheap byte-budget read, and an atomic Postgres reserve immediately before `repo.Create` (reusing the exact TOCTOU-recheck shape the preset-resolution code already established). Postgres, not Redis, is the counter of record — Redis is contractually transient in this architecture, and the existing `internal/ratelimit` package's own doc comment already documents (and warns against reusing, for this purpose) an in-process counter that is correct only at `api.replicas: 1`.

The key risks are not implementation-technique risks but *blast-radius* risks: (1) `client_id` multiplies, not adds to, existing Prometheus label cardinality — it must land on a new purpose-built counter, never on the existing histogram, and never, under any circumstance, on `octoconv_queue_depth`, the one metric KEDA reads; and (2) every current OctoConv consumer has run against an unlimited API for up to a year — shipping hard enforcement with no rollout period risks breaking already-integrated production consumers with zero warning. Both risks are mitigated by discipline already established elsewhere in this codebase (pinning tests, `NULL`-means-unlimited instead of sentinel values, observe-only-before-enforce), not by new tooling.

## Key Findings

### Recommended Stack

No new infrastructure or third-party dependencies are recommended. The entire stack addition is: one new Postgres table + index, `pg_advisory_xact_lock` (already used for reconciler leader election), and one new Prometheus `CounterVec`. asynq's priority/group features, a Redis-backed quota counter, `golang.org/x/time/rate`, `github.com/go-redis/redis_rate`, and any third-party fair-queueing library were all evaluated and explicitly rejected — see STACK.md's "What NOT to Use" table for the full rationale per item.

**Core additions:**
- **PostgreSQL 18 (existing)** — durable counters for concurrency-in-flight and daily volume, written inside the same transaction as job creation/completion — gets atomicity for free from a pattern already in production use.
- **`pg_advisory_xact_lock` (existing pattern)** — serializes check-then-act quota sequences across concurrent API replicas; already proven in `internal/reconciler/reconciler.go`.
- **`prometheus/client_golang` (existing)** — one new, separate `CounterVec` (e.g. `octoconv_client_job_outcomes_total{client_id,engine,status}`) for accounting; never retrofit `client_id` onto the existing histogram.
- **asynq `Config.Queues` (existing, unused for this purpose)** — verified in vendored source (`server.go:131-160`) to be queue-level weighting only, with zero per-task/per-client notion; noted only as a possible future SEED for coarse priority *tiers*, not this milestone's mechanism.

### Expected Features

**Must have (table stakes, P1 — this milestone):**
- `client_id` label on job outcome/duration metrics — closes the milestone's stated "чей это трафик" gap, near-zero cost
- Per-client, per-engine concurrent-jobs admission cap, fail-closed before S3 write, additive/opt-in (existing clients see zero change until an operator sets a limit)
- Per-client daily byte-volume and/or jobs-per-window quota — covers the abuse shape (few-huge-jobs / sustained volume) the concurrency cap alone misses
- Per-engine allowance per client (governance: which engine classes a client may use)
- Operator-facing usage endpoint under the existing `OPERATOR_CLIENT_IDS` gate (Phase 26 precedent)
- `minReplicaCount` per engine class configurable (SEED-011) — small, independent, chart-only

**Should have (differentiators, P2 — sequence after P1 with evidence):**
- Two-tier weighted priority queue per engine class (`image` + `image-deprioritized`), using asynq's *real* weighted-queue feature — bounded to one extra queue per engine class, not per-client
- Soft-quota response headers (`X-Quota-*`) for self-throttling DX
- Dashboard panel(s) on top of the new client-labeled metric

**Defer (v2+ / evidence-gated):**
- Usage rollup/materialized view beyond the daily table — defer until raw-row query cost is actually measured
- Any retention/purge policy for `jobs`/`job_events` — genuinely orthogonal to this milestone, but flag as a tracked gap, not silence
- Per-client sub-queues / dynamic virtual queues (Q4 option C in ARCHITECTURE.md) — explicitly deferred, evidence-gated on admission caps proving insufficient in practice

**Explicit anti-features** (wrong for this context, and why):
- **Billing/invoicing, usage-based pricing** — no monetization exists in this internal service; PROJECT.md locks this out explicitly
- **Self-service quota portal, tiered plans (free/premium/enterprise)** — no self-service signup exists today (`cmd/manage-clients` is operator-run); a portal implies an external trust boundary this service doesn't have
- **Per-request/per-MB pricing model** — nothing to price; recording bytes is for ops visibility only, never cost
- **Full weighted-fair-queueing scheduler (Inngest-style per-tenant virtual queues)** — the textbook answer at high concurrency, but with 1-2 concurrent slots per pod and max 2 pods, ordering barely matters compared to occupancy; the implementation cost (dynamic queues, KEDA-per-queue breakage) is high for near-zero marginal benefit at this scale
- **ML/anomaly-detection abuse throttling** — consumers are a small, known, internal, non-adversarial set; an explainable numeric quota an operator can reason about beats an opaque model
- **Formal SLA tiers** — no SLA concept exists anywhere in OctoConv; would formalize a relationship that doesn't exist
- **A new distributed/external rate-limiting service (Envoy global ratelimit, standalone Redis cluster)** — solves horizontal-scale problems this single-replica-today API doesn't have, and quota state needs to be durable/query-able anyway, which points at Postgres

### Architecture Approach

Quota enforcement is not one gate but three tiers of increasing cost and authority, layered exactly the way `maxUploadByte` → `maxEngineBytes[engine]` are already layered in `handleCreateJob` today: (1) a cheap concurrency-admission read immediately after auth, before content sniffing; (2) a cheap byte-budget soft-check once the engine is known, before the S3 write; (3) an atomic Postgres reserve immediately before `repo.Create`, reusing the exact TOCTOU-recheck shape the existing preset-resolution code already established ("Pitfall 8" precedent) — a tier-3 rejection leaves the already-uploaded S3 object in place for TTL cleanup, never deletes it inline. Usage is recorded at the single natural choke point every `Handle*Convert` worker handler already hits (`MarkDone`/`MarkFailed` → `metrics.RecordJobOutcome`), as one additional atomic `INSERT ... ON CONFLICT DO UPDATE` against a new `client_usage_daily` rollup table — the same statement that serves both accounting-for-reporting and quota-enforcement, avoiding duplicate bookkeeping.

**Major components:**
1. **`client_usage_daily` (new table)** — `(client_id, usage_date, engine)` primary key, `jobs_count`/`bytes_in`/`bytes_out`/`engine_seconds` — additive, zero-migration-impact rollup, source of truth for both reporting and the quota reserve.
2. **`jobs_client_inflight_idx` (new partial index)** — `(client_id) WHERE status IN ('queued','active')`, mirroring the existing `jobs_inflight_idx` pattern — makes the concurrency cap a live query over ground truth, not a separate counter.
3. **Three inline gates in `handleCreateJob`** — no new chi middleware (quota logic needs the engine, resolved deep in the handler, and a Postgres round trip httprate-style in-process middleware can't provide); reuses the existing `writeError` convention.
4. **`RecordUsage` in `internal/jobs/repo.go`** — the atomic UPSERT called from the worker-side terminal-transition choke point, extending the guarded-transaction pattern `Repo.transition` already uses.
5. **Operator usage endpoint** — extends the Phase 26 `OPERATOR_CLIENT_IDS`-gated `/v1/system/*` pattern, reads `client_usage_daily` directly (cheap, pre-aggregated — not raw Prometheus, not a full `jobs` scan).
6. **Untouched, deliberately**: `internal/metrics/queue_collector.go`, all 7 `deploy/chart/octoconv/templates/scaledobject-*.yaml`, and the reconciler's `enqueuer` interface — none of these should show a diff line under the recommended build order.

### Critical Pitfalls

1. **`client_id` multiplies existing Prometheus cardinality, not adds to it** (compounds with every other label already on a metric) — add it to exactly one new, purpose-built `CounterVec`, never to the existing `{engine,status}` histogram, and enforce a cap derived from the bounded `clients` table rather than trusting request input.
2. **Adding `client_id` to `octoconv_queue_depth` breaks the one metric KEDA reads** — freeze that metric's label set explicitly (`{queue,state}` only), add a pinning test that fails loudly on any future diff, and treat any PR touching `queue_collector.go` or the ScaledObject PromQL with the same live-gate discipline as `scripts/keda-gate.sh`.
3. **In-process quota counters race across API replicas** exactly like `internal/ratelimit`'s own doc comment already warns — quota state must be Postgres-atomic (`SELECT...FOR UPDATE` or `ON CONFLICT DO UPDATE`) inside the same transaction as job creation/completion, verified under 2+ concurrent replicas, not assumed correct at `replicas: 1`.
4. **Per-client sub-queues break the KEDA + reconciler contract** — any design that puts `client_id` into a queue *name* requires rewriting all 7 ScaledObject PromQL strings, the queue-depth collector, `AllConvertQueues()`, and the reconciler's hardcoded engine→queue routing switch simultaneously; this is explicitly the design to avoid, deferred behind evidence.
5. **Shipping enforcement with no rollout period silently breaks already-integrated consumers** — every existing client has run against an unlimited API for up to a year and has never seen a quota-shaped rejection; `NULL` (not a large sentinel number) must mean unlimited, and an observe-only period ("would have rejected" counter, no actual rejection) should run before real enforcement per client.

## Implications for Roadmap

Based on research, suggested phase structure (ordered by actual data/mechanism dependency, not by PROJECT.md's feature-list order):

### Phase 1: Attribution (metrics label + usage rollup table + wiring)
**Rationale:** Everything downstream *reads* this data — quotas can't be enforced against numbers that aren't tracked, and the operator endpoint has nothing to show without it. Purely additive: new table, new label argument, one new call site per `Handle*Convert`. Zero behavior change for existing clients.
**Delivers:** `client_usage_daily` migration + partial index groundwork; `client_id` label on a new, separate accounting `CounterVec`; `RecordUsage` wired into all 7 `MarkDone`/`MarkFailed` choke points.
**Addresses:** "Атрибуция" (FEATURES.md table stakes: client_id label, per-job accounting record).
**Avoids:** Pitfall 1 (cardinality multiplication) and Pitfall 2 (`octoconv_queue_depth` contamination) — both must be closed as this phase's own success criteria, with a pinning test proving `octoconv_queue_depth`'s label set is unchanged.

### Phase 2: Quota enforcement (the three-tier gate + admission-control fairness)
**Rationale:** Depends on Phase 1's table existing (tiers 2/3 read/write it) and on `jobs.status` already being ground truth (tier 1 needs only a new index). This phase also delivers the baseline fairness requirement for free — the concurrency cap read for a fairness purpose is the identical mechanism read for a quota purpose, so no separate "fairness phase" is needed for the non-sub-queue baseline.
**Delivers:** `jobs_client_inflight_idx`; three inline gates in `handleCreateJob` (concurrency, byte-budget soft-check, atomic reserve); per-engine allowance check; 429+`Retry-After` response contract, `rate_limited` vs `quota_exceeded` distinguished in the error body.
**Uses:** Postgres `pg_advisory_xact_lock` / `ON CONFLICT DO UPDATE` idioms from STACK.md; the existing TOCTOU-recheck shape from the preset-resolution precedent.
**Implements:** ARCHITECTURE.md's Q1/Q3 three-tier gate and Q4 option A (admission control).
**Avoids:** Pitfall 4 (in-process/racy counters — test with 2+ API replicas), Pitfall 5 (reconciler double-count race — pair with a soak test forcing the stale-recovery race), Pitfall 6 (check-before-S3-write ordering; release-on-every-terminal-state, not just success), Pitfall 7 (rollout safety — `NULL`-means-unlimited, observe-only period, per-client staged rollout), Pitfall 10 (enforce in the shared layer, tested through both REST and at least one MCP entry point, never 403).

### Phase 3: Operator visibility (endpoint and/or dashboard)
**Rationale:** Depends only on Phase 1 (reads `client_usage_daily`); sequenced after quota work has started so "usage vs. configured limit" is a meaningful comparison, though it does not technically require Phase 2 to be complete.
**Delivers:** `/v1/system/usage`-style endpoint under `OPERATOR_CLIENT_IDS`; optionally a Grafana panel over the new accounting counter.
**Addresses:** "Видимость для оператора" (milestone requirement #4).

### Phase 4: SEED-011 (`minReplicaCount` per engine class)
**Rationale:** Fully independent of Phases 1-3 — zero data/code coupling, pure Helm values/`ScaledObject` change. Can land at any point; a defensible low-risk "quick win" or hygiene tail.
**Delivers:** Operator-selectable `minReplicaCount` per class, replacing the hardcoded zero.

### Phase Ordering Rationale

- Attribution before enforcement before visibility mirrors the milestone's own stated ordering ("Атрибуция" before "Границы") and is the only order in which each phase has real data to build against.
- Quota enforcement and baseline fairness are the same phase because they are the same mechanism (Q1/Q3 = Q4 option A) — building them separately would duplicate the admission-cap machinery for no benefit.
- Per-client sub-queue fairness (the one design that touches KEDA) is deliberately **not** in this roadmap — it is evidence-gated behind Phase 3's operator visibility showing admission caps are insufficient, consistent with this project's own precedent of rejecting speculative infrastructure that weakens an existing guarantee (SEED-010 rejection cited in PROJECT.md).
- SEED-011 is unordered relative to the rest because it shares no code or data with the accounting/quota/fairness work.

### Research Flags

Needs research during phase planning:
- **Phase 2 (quota enforcement):** the two open product decisions research explicitly left unresolved — (a) HTTP semantics for concurrency-cap rejection specifically (daily-volume overage has a clean 429+Retry-After answer; concurrency-cap rejection has no fixed retry time and could defensibly be 429-without-Retry-After or a distinct code — this is a product decision, not an architecture one); (b) whether `client_usage_daily` needs to be a daily rollup as designed, or whether the existing `jobs_client_created_idx` already covers "basic reporting" cheaply enough to defer the new table's write path. Both should be resolved as explicit roadmap/plan decisions, not assumed during implementation.
- **Phase 2:** multi-replica correctness needs a live test with 2+ API replicas hammering one client's limit — this is a new class of test this project hasn't needed before (existing `api.replicas: 1` in every prior milestone).

Phases with standard, well-documented patterns (skip research-phase):
- **Phase 1:** pure extension of existing, already-proven patterns (`RecordJobOutcome` call sites, `promauto.NewCounterVec`, guarded-transaction UPSERT) — no open questions.
- **Phase 3:** direct extension of the Phase 26 `OPERATOR_CLIENT_IDS` REST precedent — a solved problem in this codebase already.
- **Phase 4 (SEED-011):** chart-only Helm values change, no architectural novelty.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | asynq v0.26.0 and httprate v0.15.0 claims verified directly against vendored source, not assumed from docs. Prometheus cardinality guidance is MEDIUM (WebSearch-derived, cross-checked across multiple sources, no single canonical doc page) but the specific application to this codebase's metric shapes is HIGH (direct code read). |
| Features | MEDIUM-HIGH | Grounded in the actual current schema/code (HIGH) plus verified ecosystem patterns (asynq wiki, k8s admission-control docs, MDN on 429) — a few forward-looking specifics (exact quota numbers, retention window) are explicitly flagged LOW and left as open questions for planning, not guessed at. |
| Architecture | HIGH | No external ecosystem claims required — this is an internal-integration question, and every finding is sourced directly from repo files (`handlers.go`, `repo.go`, `queue.go`, `metrics.go`, `queue_collector.go`, `reconciler.go`, `0001_init.sql`, all 7 `scaledobject-*.yaml`, `PROJECT.md`). |
| Pitfalls | HIGH (internal) / MEDIUM (external) | Every pitfall is anchored to a specific file/line/decision already in this repository (reconciler's documented T-03-12 residual race, ratelimit's own doc comment, the preset TOCTOU precedent). External cardinality-guidance sources are MEDIUM confidence (blog posts, cross-checked); the KEDA-specific fairness-breakage claim (Pitfall 8) is explicitly noted as LOW-external/HIGH-internal since no KEDA-specific fairness literature surfaced in search — the finding rests entirely on this codebase's own static PromQL/routing code. |

**Overall confidence:** HIGH

### Gaps to Address

- **HTTP semantics for concurrency-cap quota rejection** (429 without `Retry-After` vs. a distinct status) is unresolved by design — flag as a roadmap/plan-time decision point in Phase 2, not something to assume during research synthesis.
- **`client_usage_daily` vs. reusing `jobs_client_created_idx`** — research recommends the new rollup table but flags that the existing per-job index may already answer "basic reporting" cheaply enough that the new table's write path could be deferred; resolve this trade-off explicitly when planning Phase 1/3, ideally validated against a realistic row-count estimate.
- **Retention/purge policy for `jobs`/`job_events`** (and, by extension, `client_usage_daily`) — genuinely orthogonal to this milestone per all four researchers, but is a pre-existing, unaddressed gap this milestone's own new table would otherwise silently repeat. Recommend the new table state its retention/rollup design explicitly in its creation migration even if a full purge mechanism is out of scope, and track the pre-existing `jobs`/`job_events` gap as a named, ID'd tech-debt item (matching this project's existing AVX-02/K8SV2-03-style tracking discipline) rather than leaving it silent.
- **Exact quota default values** (concurrency cap per engine, daily byte/job budget) — not resolved by research; these are operator-tunable, default-`NULL` (unlimited) values to be set during rollout, not fixed architectural constants.
- **Enforcement coverage across every job-creation entry point**, including `cmd/mcp-server` and `cmd/mcp-http`, not just the REST handler — flagged repeatedly (PITFALLS.md Pitfall 10) as a place bypass risk hides if enforcement is later refactored into HTTP-middleware-only; must be an explicit success criterion (integration test through both REST and at least one MCP path) in whichever phase ships enforcement, not assumed to "just work" because MCP currently proxies to REST.

## Sources

### Primary (HIGH confidence)
- `asynq@v0.26.0` vendored source (`server.go`, `client.go`, `aggregator.go`, `inspector.go`) — read directly, not assumed
- `httprate@v0.15.0` vendored source (`limiter.go`, `local_counter.go`, `httprate.go`) — read directly
- OctoConv repository (authoritative for all architecture/pitfall findings): `internal/api/handlers.go`, `internal/jobs/repo.go`, `internal/queue/queue.go`, `internal/metrics/metrics.go`, `internal/metrics/queue_collector.go`, `internal/ratelimit/ratelimit.go`, `internal/reconciler/reconciler.go`, `internal/worker/worker.go`, `internal/clients/clients.go`, `internal/db/migrations/0001_init.sql`, `deploy/chart/octoconv/templates/scaledobject-*.yaml`, `deploy/chart/octoconv/values.yaml`, `.planning/PROJECT.md`
- [Queue Priority · hibiken/asynq Wiki](https://github.com/hibiken/asynq/wiki/Queue-Priority) — official project wiki, confirms weighted/strict priority is a named-queue property, not per-task
- [Admission Control in Kubernetes](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) and [ResourceQuota admission-control design doc](https://github.com/kubernetes/design-proposals-archive/blob/main/resource-management/admission_control_resource_quota.md) — official k8s docs, precedent for reject-at-admission
- [429 Too Many Requests — MDN](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Status/429)

### Secondary (MEDIUM confidence)
- Prometheus label cardinality best practices — multiple independent WebSearch sources, cross-checked and consistent: [The Prometheus Cardinality Bomb](https://openobserve.ai/blog/prometheus-data-cardinality/), [Prometheus Label Best Practices](https://oneuptime.com/blog/post/2026-01-30-prometheus-label-best-practices/view), [Prometheus Labels: Understanding and Best Practices](https://middleware.io/blog/prometheus-labels/), [SigNoz: Limitations of Prometheus Labels](https://signoz.io/guides/what-are-the-limitations-of-prometheus-labels/), [Last9: High Cardinality in Prometheus](https://last9.io/blog/how-to-manage-high-cardinality-metrics-in-prometheus/)
- [Fixing noisy neighbor problems in multi-tenant queueing systems — Inngest Blog](https://www.inngest.com/blog/fixing-multi-tenant-queueing-concurrency-problems) — used to argue against porting per-tenant virtual queues at this system's scale
- [prometheus/client_golang Discussion #970](https://github.com/prometheus/client_golang/discussions/970) — client-side cardinality guarding pattern

### Tertiary (LOW confidence)
- KEDA-specific fairness/priority-queue claims (PITFALLS.md Pitfall 8) — no KEDA-specific fairness literature surfaced via search; conclusion rests entirely on direct reading of this codebase's own static PromQL/routing code (HIGH internal confidence, LOW external corroboration)

---
*Research completed: 2026-08-02*
*Ready for roadmap: yes*

---

## Corrections after PR #3 review (2026-08-02)

An external review of the milestone plan found four design defects in
`ARCHITECTURE.md`'s proposed integration shape. **Where this section and
`ARCHITECTURE.md` disagree, this section wins** — the phase planner should treat
the items below as binding.

1. **The admission check must be serialized per (client, engine).**
   `ARCHITECTURE.md` describes a `SELECT count(*)` of the client's queued/active
   jobs followed by `Repo.Create`. Reading shared Postgres rows does not make
   check-and-insert atomic: two API replicas can both observe a count below the
   cap and both admit, so a cap of K admits more than K. The count and the create
   must share one transaction under a per-(client, engine) advisory lock or an
   equivalent atomic counter. `STACK.md` did specify `pg_advisory_xact_lock`;
   `ARCHITECTURE.md` dropped it.

2. **The authoritative reservation must precede the S3 write.**
   `ARCHITECTURE.md`'s gate ordering uploads the request body before the tier-3
   reserve resolves, so a client that loses the reserve has already consumed S3
   bandwidth and storage — repeatedly, at will. This contradicts QUOT-02 as
   written ("до записи в S3"). Reserve first; release the reservation if a later
   step fails.

3. **Reservations and completed usage must be distinguishable.**
   If admission increments the daily rollup to reserve capacity and the terminal
   transition increments the same counters again, every completed job is counted
   twice and clients exhaust their daily budget early. The table needs a
   reserved-versus-consumed distinction (or an identifiable reservation that
   completion finalizes), not one pair of counters written by both paths.

4. **The usage write belongs inside the terminal-transition transaction.**
   `ARCHITECTURE.md` proposes an adjacent `RecordUsage` call after
   `MarkDone`/`MarkFailed`. If the worker dies in between, the job is already
   terminal — neither an asynq retry nor the reconciler will revisit it — and the
   usage is lost permanently. ACCT-03's exactly-once guarantee requires the rollup
   update to be part of the same guarded transaction as the status transition.

Two further findings were applied directly to the ROADMAP's Phase 51 success
criteria and need no separate design note: cardinality must be bounded by a
configured numeric ceiling with a stable overflow label (restricting labels to
IDs present in `clients` is not a bound, since clients can be added
indefinitely), and retention must be an executable mechanism proven to remove
aged rows, not a policy described in a migration comment.
