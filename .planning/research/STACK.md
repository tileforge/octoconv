# Stack Research

**Domain:** Per-consumer accounting, quotas, and intra-class fairness for an existing Go/asynq/Postgres/Redis async conversion service
**Researched:** 2026-08-02
**Confidence:** HIGH (asynq claims verified against vendored v0.26.0 source; httprate claims verified against vendored v0.15.0 source; Prometheus cardinality guidance is MEDIUM — WebSearch-derived, cross-checked against multiple sources, no single canonical Prometheus doc page fetched)

## Recommended Stack

### Core Technologies — additions

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| PostgreSQL (existing) | 18 | Durable per-client counters: concurrency-in-flight cap, daily volume quota | Already the system of record; already in the hot path of every job-creation request (`internal/api/handlers.go`'s "Postgres-first double write"); a quota check that lives in the *same transaction* as the job INSERT gets atomicity for free — no dual-write hazard. Verified: no new infra call needed. |
| `pg_advisory_xact_lock` (Postgres built-in, existing pattern) | n/a | Serializes the "count in-flight jobs for (client_id, engine) then insert" check-then-act sequence across concurrent API replicas | Already used in this codebase for reconciler-sweeper leader election (`internal/reconciler/reconciler.go`, `advisorylock_test.go`) — reusing a proven idiom rather than introducing a new one. Closes the classic COUNT-then-INSERT race that a bare `SELECT COUNT(*)` under READ COMMITTED does not. |
| asynq `Queues` priority map (existing dependency, v0.26.0) | — | Coarse, *queue-level* prioritization only — NOT per-tenant fairness | See "asynq capabilities" section below. Verified in vendored source (`server.go:131-160`). Recommended only as an optional future SEED (priority *tiers*, e.g. `image` vs `image-low`), not as this milestone's fairness mechanism. |
| Prometheus `client_golang` (existing, v1.23.2) | — | `client_id` label on a **new low-cardinality-safe counter**, NOT retrofitted onto the existing histogram | See cardinality section. Existing `octoconv_job_duration_seconds` (Histogram) must NOT gain a `client_id` label — bucket multiplication makes it the wrong metric to extend. Add a separate `Counter` instead. |

### Supporting Libraries — evaluated, none recommended as new dependencies

| Library | Version | Purpose | Verdict |
|---------|---------|---------|---------|
| `github.com/redis/go-redis/v9` (already a **direct** dependency, v9.14.1) | already present | Could theoretically host quota counters via `INCR`/`EXPIRE` | **Not recommended for durable quotas** — see "Quota counting" section. Already used directly for `job:cancel` pub/sub (`internal/cancel/pubsub.go`) so it costs nothing to *keep using* for what it's already good at, but extending it to quota/accounting state would violate the explicitly documented "Redis is transient" constraint (CLAUDE.md Constraints; PROJECT.md v1.12 context: "Postgres остаётся системой записи — новых хранилищ не заводим"). |
| `github.com/go-redis/redis_rate/v10` | not installed | Distributed sliding-window rate limiter on Redis | Not needed. This library solves *rate* (requests/sec), not *quota* (durable daily volume / concurrency caps) or *fairness*. The existing `internal/ratelimit` package's doc comment already names this as the documented upgrade path *if* the API is horizontally scaled and RPM limits need to stay in sync across replicas — that is an orthogonal, already-anticipated future need, not something v1.12 must solve. Do not pull it in now; today's `httprate` in-process counter is correct for `replicas: 1` (confirmed in `deploy/chart/octoconv/values.yaml:33`). |
| `golang.org/x/time/rate` | v0.14.0 (already resolved transitively, in `go.sum`, zero new dependency to add) | In-process token-bucket smoothing | Not needed for quotas/fairness (same single-replica-only limitation as `httprate`'s local counter — doesn't solve cross-replica races). Could be reused later for local burst-smoothing, but out of scope here. |
| Any dedicated "multi-tenant fair scheduler" library (e.g. generic Go weighted-fair-queuing packages) | — | Per-key fair dequeue ordering | **Investigated and rejected.** No mature, actively maintained Go library implements weighted-fair-queuing *on top of asynq specifically* — asynq's task model (opaque payload, Redis sorted-set-backed queue) does not expose a pluggable dequeue-ordering hook a third-party scheduler could attach to. Building this yourself would mean either forking asynq's processor loop or building an entirely separate admission-side scheduler — see "Fairness" recommendation below for the cheaper alternative that avoids this altogether. |

## Verified: asynq v0.26.0 capabilities (read from vendored source, not assumed)

Checked against `/go/pkg/mod/github.com/hibiken/asynq@v0.26.0` (the exact module version pinned in this repo's `go.mod`).

### 1. Queue-level priority (`Config.Queues`, `Config.StrictPriority`) — `server.go:131-160`

```go
// List of queues to process with given priority value. Keys are the names of the
// queues and values are associated priority value.
//
// Example:
//     Queues: map[string]int{
//         "critical": 6,
//         "default":  3,
//         "low":      1,
//     }
//
// With the above config and given that all queues are not empty, the tasks
// in "critical", "default", "low" should be processed 60%, 30%, 10% of
// the time respectively.
Queues map[string]int
```

**This is priority between *named Redis queues* processed by the same worker process** (weighted round-robin by default; strict cutoff if `StrictPriority: true`). It is **not** per-task, per-client, or per-tenant. OctoConv already uses one queue per *engine class* (`image`, `document`, `av`, …) — this mechanism has no notion of "client" at all. The only way to use it for fairness would be to create additional queues per priority *tier* (e.g. `image` + `image-low`) and statically route clients to a tier — a coarse, few-buckets mechanism, not per-client fairness. **Does not solve** "one client cannot starve the others inside a single engine's queue."

### 2. Task Groups / `GroupAggregator` — `server.go:219-273`, `aggregator.go`, `client.go:212`

```go
// GroupGracePeriod specifies the amount of time the server will wait for an incoming task before aggregating
// the tasks into one task... GroupAggregator aggregates a group of tasks into one before the tasks are passed to the Handler.
```

`asynq.Group(name)` on `Client.Enqueue` puts a task into a named group; `GroupAggregator` is a user function that combines **multiple tasks sharing a group into a single task** delivered to the `Handler` once (batching/debouncing use case — e.g. "collapse 10 near-simultaneous notification tasks for the same user into 1 digest"). **This is task aggregation, not fairness or per-tenant isolation.** Using it here would be actively wrong: it would merge multiple *different clients'* conversion jobs into one `Handler` invocation, breaking the "one job = one task, all detail re-read from Postgres by job_id" invariant this codebase relies on everywhere (`ConvertPayload{JobID}` minimal-payload discipline). **Verdict: does not apply to this problem; do not use it.**

### 3. No per-task priority within a queue

Searched `client.go` and `internal/base/base.go` for any per-`Task` priority/weight option analogous to `Config.Queues`' weighting — none exists. Within a single named queue, asynq's pending set is strictly FIFO by enqueue order (confirmed: no priority score field on individual task messages, only on queues as a whole). **There is no built-in notion of "this client's task should be dequeued before/after that one" inside one queue.**

### Conclusion — what must be built on top

asynq v0.26.0 (as used here, one queue per engine class) provides **zero per-tenant fairness primitives**. To satisfy "one consumer cannot starve an engine class" you cannot lean on asynq's priority/group features — both were checked and both solve a different problem. The two realistic options, cheapest first:

1. **Recommended: admission-side concurrency cap per (client_id, engine).** Before enqueueing, check (inside the same Postgres transaction as the job INSERT, guarded by `pg_advisory_xact_lock(hashtext(client_id::text || engine))`) how many of that client's jobs are currently `queued` or `active` for that engine (`jobs` table already has both columns; add a partial composite index `(client_id, engine) WHERE status IN ('queued','active')`). If the client is at/above a configurable per-client concurrency cap for that engine (e.g. `IMAGE_MAX_CONCURRENT_PER_CLIENT`), reject with `429` + `Retry-After` — same response shape `internal/ratelimit` already uses. This bounds the worst case exactly: if the cap is `K` and KEDA's `maxReplicaCount` for that engine is `M` (`K < M`), a single client can occupy at most `K` of the `M` available worker slots, guaranteeing `M-K` slots stay available to everyone else — a provable, deterministic guarantee, not a heuristic. Zero new dependencies; reuses the existing guarded-transaction and advisory-lock idioms verbatim.
2. **Optional future SEED, not needed for this milestone:** priority-tier queues (e.g. `image` + `image-low`), using asynq's *actually-supported* `Config.Queues` weighting, with clients statically assigned a tier. Doubles the number of queues and KEDA `ScaledObject`s per engine class (7 classes × 2 = 14) — real operational cost. Flag as a SEED if the concurrency cap alone proves insufficient in practice (e.g. operator wants "gold clients always jump the line", not just "no one is starved").

Do not attempt to build true weighted-fair-queuing (virtual-finish-time scheduling) on top of asynq's Redis lists — it would require intercepting/reordering asynq's own dequeue loop, which is internal and not designed for external hooking. The admission-cap approach sidesteps this entirely by controlling *supply* into the queue rather than *order* within it.

## Quota counting: Postgres vs Redis — recommendation and trade-off

**Recommendation: Postgres, for both counters this milestone needs.**

| Counter | Mechanism | Why Postgres wins here |
|---|---|---|
| Concurrent jobs in flight per (client, engine) | Derived directly from `jobs.status` (no new counter table) via `SELECT count(*) ... WHERE client_id=$1 AND engine=$2 AND status IN ('queued','active')`, serialized by `pg_advisory_xact_lock` in the same transaction as the job INSERT | The value being counted (`jobs.status`) is *already* durably in Postgres — there is nothing to keep in sync with a second store. A Redis counter here would be a second source of truth that can drift from `jobs.status` on any partial failure (worker crash mid-update, reconciler recovery, cancellation) — exactly the dual-write hazard the existing "Postgres-first double write" pattern was designed to avoid (`internal/api/handlers.go:83-109` doc comment). |
| Daily/period volume quota (job count and/or bytes per client per day) | New small table, e.g. `client_usage_daily(client_id, usage_date, engine, jobs_count, bytes_total)`, atomic `INSERT ... ON CONFLICT (client_id, usage_date, engine) DO UPDATE SET jobs_count = client_usage_daily.jobs_count + 1 RETURNING jobs_count`, checked against the client's quota inside the same transaction as the job INSERT — over-quota rolls the whole transaction back (job never gets created, quota never gets consumed) | Atomicity: "quota consumed if and only if job admitted" falls out for free from one Postgres transaction. A Redis `INCR`+`EXPIRE` equivalent requires either (a) a second round trip with no shared transaction with the Postgres INSERT — a job could get created after the Redis INCR fails, or the reverse — or (b) Lua-scripted atomicity inside Redis alone, which still doesn't couple to the Postgres commit. Given Redis is explicitly documented as a transient broker in this project (CLAUDE.md Constraints, PROJECT.md v1.12 context), reusing it as a durable ledger contradicts an already-locked architectural decision. |

**Trade-off spelled out (durability/consistency vs latency):**
- Postgres: fully durable (survives Redis flush/restart — note Redis here has *no* persistence guarantee configured; it is treated as ephemeral broker state), transactionally consistent with job admission, adds ~0 extra network hops (already paying a Postgres round trip per job creation) — at the cost of being the *slower* of the two options and adding write load (one extra small UPSERT + one advisory lock) to Postgres per job creation. At this service's internal, non-billing scale (7 engine classes, KEDA-scaled to low replica counts, RPM-gated at the edge already) this write volume is not a concern — it's one more statement in a transaction that already writes the `jobs` row.
- Redis: lower latency, trivially atomic per-key (`INCR`), horizontally consistent across replicas natively — but no durability guarantee in this deployment, and no natural way to make the counter change atomic *with* the Postgres job-admission decision without reintroducing a dual-write race. Appropriate for *approximate*, best-effort request-rate throttling (what `internal/ratelimit` already does, and could later move to Redis via `httprate.WithLimitCounter` if the API is horizontally scaled — see Alternatives) but wrong for *exact*, durable business quotas that an operator will treat as a hard guarantee ("this client gets 500 conversions/day, not 501").

**Do not build a new Redis-backed quota system for this milestone.** Keep Redis doing exactly what it already does (asynq broker, `job:cancel` pub/sub) and put all new durable counters in Postgres, reusing the transaction that already exists around job creation.

## Prometheus cardinality: adding `client_id`

**Accepted practice (MEDIUM confidence, WebSearch-derived, cross-checked across multiple 2025/2026 sources — see Sources):**
- Every unique label-value combination is a new stored time series; cost is roughly linear in (label A cardinality) × (label B cardinality) × ... for all labels on one metric.
- A commonly cited rule of thumb: keep any single label's cardinality comfortably under ~1,000 distinct values; treat 10,000+ as high risk. This service's `clients` table is a small, operator-managed internal-services list (currently a handful of rows, PROJECT.md explicitly frames this as "internal-only" ecosystem) — comfortably inside the safe range today and for the foreseeable future of an internal-services-only client base.
- **Histograms multiply cost far faster than counters.** `octoconv_job_duration_seconds` is a `HistogramVec` with `prometheus.DefBuckets` (11 default buckets) already labeled `{engine, status}` (7 engines × ~2-3 statuses ≈ ~15-20 combos × ~13 series each [11 buckets + `_sum` + `_count`] ≈ ~250 series today). Adding `client_id` directly to this histogram would multiply that by the client count — the wrong place to add the label even at a "safe" client count, because histogram cost compounds with every other label already present.
- **Recommendation:** do NOT add `client_id` to `octoconv_job_duration_seconds` (histogram). DO add it to a **new, separate `CounterVec`** purpose-built for accounting (e.g. `octoconv_client_job_outcomes_total{client_id, engine, status}` mirroring the shape of the existing `octoconv_job_outcomes_total` but scoped only to the accounting use case) — counters cost exactly 1 series per label combination, no bucket multiplication. This keeps the existing latency/SLO metrics cheap and keeps the new per-client accounting metric's cost bounded and predictable: (client count) × (engine count) × (status count), which at internal-service scale (dozens of clients at most, 7 engines, 2-3 statuses) is a few hundred series — trivial for a single-node Prometheus instance.
- Do not add `client_id` to `octoconv_queue_depth`, `octoconv_reconciler_actions_total`, or `octoconv_webhook_deliveries_total` — none of these are naturally scoped to a single client (queue depth spans all clients; reconciler/webhook actions are operational, not per-tenant), and none need it for this milestone's stated goal.

**Explicit bound this project should fix in code/docs:** given the client base is internal-services-only and PROJECT.md's own Key Decisions table already calls this "осознанный рост кардинальности... лимит фиксируется явно" (a deliberate cardinality increase with the limit stated explicitly) — recommend hard-coding a sanity ceiling (e.g. reject client creation past N=500 active clients, or alert if `count(distinct client_id)` in the new metric exceeds a threshold) rather than leaving it unbounded. This is a cheap guard, not a new dependency.

**Escape hatch if client count grows unexpectedly beyond the safe range:**
1. **First, and cheapest:** stop relying on Prometheus for the accounting *ledger* — it already isn't the source of truth here (Postgres is; the accounting metric is a convenience view for dashboards/alerting, not the audit trail). The durable per-client usage data lives in Postgres (`client_usage_daily` / `jobs` table) regardless of what happens to the Prometheus metric; the operator-facing "who and how much" answer (this milestone's explicit requirement) can be served from a Postgres-backed API/dashboard query instead of PromQL if cardinality ever becomes a problem — this removes the pressure to keep scaling the Prometheus label.
2. If per-client Prometheus visibility is still wanted at higher client counts: drop the raw `client_id` label and replace it with a small number of cohort/tier buckets (e.g. a `tier` label with ≤10 values, set by the operator) — this is the standard "bucket instead of raw ID" mitigation cited across the cardinality guidance above.
3. If truly high-cardinality per-client metrics become a hard requirement later (unlikely for an internal-services-only product per PROJECT.md's own framing), that is a genuinely different infrastructure decision (e.g. a cardinality-tolerant backend such as Mimir/VictoriaMetrics/Thanos) — explicitly out of scope for this milestone and not recommended pre-emptively; the internal client scale does not justify it today.

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| asynq `Config.Queues` per-client priority weighting | Verified: queue-level only, not per-task/per-tenant; would require one Redis queue *per client* to actually use it for per-client fairness — does not scale and breaks the "one queue per engine class" design this codebase is built around | Admission-side per-client concurrency cap in Postgres (see above) |
| asynq `Group`/`GroupAggregator` | Verified: batches multiple tasks into one before the `Handler` runs — the opposite of isolating tenants; would violate the one-job-one-task invariant | Leave tasks as-is; solve fairness at admission, not at dequeue |
| Redis (`INCR`/`EXPIRE` or a rate-limiting library) as the durable quota ledger | Redis is explicitly a transient broker in this project's constraints; a Redis-only counter can drift from Postgres `jobs.status` on partial failures, recreating the dual-write hazard the existing "Postgres-first double write" pattern was built to avoid | Postgres, inside the existing job-creation transaction |
| Adding `client_id` to `octoconv_job_duration_seconds` (Histogram) | Histogram cost multiplies by bucket count; adding a label here multiplies an already-labeled `{engine,status}` histogram by client count for no benefit (per-client latency SLOs are not this milestone's goal) | A new, separate `CounterVec` scoped to accounting only |
| A third-party generic "fair queue"/"weighted round robin" Go library bolted onto asynq | Investigated: no library plugs into asynq's internal dequeue loop; would require forking asynq or building a parallel scheduler — high cost for a problem the admission-cap approach solves more cheaply | Postgres-backed admission-side concurrency cap |
| `github.com/go-redis/redis_rate` (or similar) right now | Solves a different, currently-unneeded problem (cross-replica RPM rate limiting) — the API runs at `replicas: 1` today (`deploy/chart/octoconv/values.yaml:33`), so the existing in-process `httprate` counter is correct as-is | Keep `httprate`'s local counter; revisit only if/when the API is horizontally scaled (the upgrade path is already documented in `internal/ratelimit/ratelimit.go`'s package doc comment) |

## Stack Patterns by Variant

**If the "cannot starve" requirement is judged to need strict prioritization (not just a starvation-proof cap) later:**
- Use asynq's real `Config.Queues` weighting with a small number of statically-assigned client tiers (2-3 extra queues per engine class), not per-client queues.
- Because that is the only per-queue mechanism asynq actually offers verified in source; per-client queues would not scale (7 engines × N clients queues, N KEDA ScaledObjects) and asynq has no per-task priority to fall back on within one queue.

**If client count is expected to grow well past "internal services only" (dozens) toward hundreds+:**
- Revisit the cardinality ceiling on the new `client_id`-labeled counter before it happens, not after — add the sanity guard mentioned above now, since it's cheap, rather than reactively once a metric collapses.
- Because Prometheus cardinality problems are cheap to prevent and expensive to firefight in production (per WebSearch guidance across multiple sources).

## Version Compatibility

| Package | Version (pinned in `go.mod`) | Notes |
|---------|-------------------------------|-------|
| `github.com/hibiken/asynq` | v0.26.0 | Claims above verified against this exact vendored version's source in `$GOMODCACHE`. `Config.Queues`, `StrictPriority`, `Group`/`GroupAggregator` all confirmed present and behave as described; no per-task priority field found anywhere in `client.go` or `internal/base/base.go`. |
| `github.com/go-chi/httprate` | v0.15.0 | `LimitCounter` interface and `WithLimitCounter` option confirmed present in vendored source (`limiter.go`, `httprate.go`) — the Redis-upgrade escape hatch documented in `internal/ratelimit/ratelimit.go`'s package comment is real and already wired for a future swap, not aspirational. |
| `github.com/redis/go-redis/v9` | v9.14.1 | Already a **direct** dependency (not just via asynq) — used today for `job:cancel` pub/sub (`internal/cancel/pubsub.go`). No version bump needed for anything recommended here, since nothing new is proposed for Redis. |
| `github.com/prometheus/client_golang` | v1.23.2 | No version change needed — `CounterVec`/`HistogramVec` APIs used here are unchanged; only a new metric is added, following the exact pattern already in `internal/metrics/metrics.go`. |
| PostgreSQL | 18 (existing) | No version change needed. `INSERT ... ON CONFLICT ... DO UPDATE ... RETURNING` and `pg_advisory_xact_lock` are both long-standing, stable Postgres features already used elsewhere in this codebase or well within reach of pgx v5.10.0. |

## Sources

- `asynq@v0.26.0` vendored source, read directly: `server.go` (Config.Queues/StrictPriority/GroupGracePeriod/GroupAggregator doc comments and struct fields), `client.go` (Group option, no per-task priority found), `aggregator.go`, `inspector.go` (Groups/GroupInfo) — HIGH confidence, primary source.
- `httprate@v0.15.0` vendored source, read directly: `limiter.go`, `local_counter.go`, `httprate.go` — HIGH confidence, primary source.
- This repository: `internal/metrics/metrics.go`, `internal/queue/queue.go`, `internal/ratelimit/ratelimit.go`, `internal/db/migrations/0001_init.sql`, `internal/reconciler/reconciler.go` (advisory lock precedent), `deploy/chart/octoconv/values.yaml` (API replica count), `.planning/PROJECT.md` (Current Milestone, Constraints, Key Decisions) — HIGH confidence, read directly.
- WebSearch: "Prometheus label cardinality best practice tenant_id per-user label" — MEDIUM confidence (aggregated blog-post guidance, not a single official Prometheus doc page; numbers cross-checked across multiple independent sources returned in the same search and consistent with each other): [The Prometheus Cardinality Bomb](https://openobserve.ai/blog/prometheus-data-cardinality/), [Prometheus Label Best Practices](https://oneuptime.com/blog/post/2026-01-30-prometheus-label-best-practices/view), [Prometheus Labels: Understanding and Best Practices](https://middleware.io/blog/prometheus-labels/), [What are the Limitations of Prometheus Labels?](https://signoz.io/guides/what-are-the-limitations-of-prometheus-labels/), [How to Manage Metric Cardinality in Prometheus](https://oneuptime.com/blog/post/2026-01-25-prometheus-metric-cardinality/view).

---
*Stack research for: Consumer Accounting & Fairness (v1.12) in OctoConv*
*Researched: 2026-08-02*
