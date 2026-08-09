# Feature Research

**Domain:** Consumer accounting, quotas and fairness for an internal multi-tenant async file-conversion platform (7 engine classes, small worker pools, Postgres system of record, asynq/Redis queue, Kubernetes+KEDA autoscaling)
**Researched:** 2026-08-02
**Confidence:** MEDIUM-HIGH (grounded in current OctoConv schema/code + verified ecosystem patterns; LOW on a few forward-looking specifics flagged inline)

## Scope Fence (repeated from milestone context — drives every anti-feature below)

OctoConv is **internal-only**: no billing, no public multi-tenancy, no self-service signup, no SLA contracts. "Accounting" here means "who is using what, and can we stop one consumer from starving the others" — an *operations* concern, not a *monetization* concern. Any feature whose only justification is "SaaS platforms do this for their paying customers" is out of scope and called out explicitly as an anti-feature.

## Grounding: what already exists (changes the cost model for "table stakes")

This is the single most important finding of this research: **most of the raw data for usage accounting already exists in the schema**, unused.

- `jobs` table (`internal/db/migrations/0001_init.sql`) already has `client_id`, `engine`, `status`, `created_at`, `started_at`, `finished_at`, `error_code`. `jobs_client_created_idx (client_id, created_at DESC)` already exists.
- `job_inputs`/`job_outputs` already carry `size_bytes` per job (ordinal 0 today, schema supports more).
- `internal/jobs/repo.go` already scopes every list/get query by `client_id` (`ListByClient`, keyset-paginated, from v1.9 JLIST work) — the query pattern for "give me this client's jobs" is already a solved, tested problem in this codebase.
- `internal/metrics/metrics.go` already has `octoconv_job_outcomes_total{engine,status}` and `octoconv_job_duration_seconds{engine,status}` — the *shape* for per-engine accounting exists; it is only missing the `client_id` label the milestone calls out.
- `internal/ratelimit/ratelimit.go` already establishes the enforcement contract this milestone should extend, not replace: 429 + `Retry-After` header + `{"error": "..."}` JSON body, keyed off `auth.ClientFromContext`, running in `internal/api` middleware chain. Its own doc comment already anticipates a future Redis-backed counter if the API is horizontally scaled — quotas should follow the same "Postgres/in-process now, note the scaling seam, don't build it yet" discipline.
- Phase 26 already established the **operator-only endpoint** pattern: `/v1/system/presets` gated by an `OPERATOR_CLIENT_IDS` env allowlist, fail-closed/fail-loud, byte-identical 404 on unauthorized access (no information leak). This is the template for any new `/v1/system/usage` style endpoint — do not invent a second auth model.
- One negative finding worth flagging: **no code path ever deletes rows from `jobs`/`job_events`.** MinIO ILM only deletes S3 objects after 7 days; Postgres rows are permanent today. This matters directly for the "what retention is sane" question below.

## Feature Landscape

### Table Stakes (Operators Expect These)

Features an internal platform operator assumes exist once you claim "we track usage." Missing these = the milestone's own goal ("кто и сколько") is unmet.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| `client_id` label on job outcome/duration metrics | Milestone explicitly states this is the current gap ("вопрос «чей это трафик» неотвечаем") | LOW | Add label to `jobOutcomes`/`jobDuration` in `internal/metrics/metrics.go`. Cardinality is bounded by the number of internal clients (tens, not millions) — PROJECT.md already accepts this tradeoff explicitly; still, document a hard cap (e.g. reject/alert past N distinct clients) so a future bug (e.g. per-request UUID mistakenly used as a label) can't blow up Prometheus. |
| Per-job accounting record queryable by client | "Кто, что, когда, объём, машинное время" — this is the literal milestone requirement | LOW | Already exists as data (see Grounding above). The work is a read path: an aggregation query (`GROUP BY client_id, engine`) or view, not a new write path. Wall time = `finished_at - started_at`; verify whether these timestamps reset across asynq retries before treating the delta as "machine time" (open question, see Gaps). |
| Concurrent-jobs-per-client admission cap | Directly protects the shared worker pool — the one thing that actually matters when a pod runs 1-2 concurrent jobs | LOW-MED | `SELECT COUNT(*) FROM jobs WHERE client_id=$1 AND engine=$2 AND status IN ('queued','active')` before enqueue. Needs a per-(client,engine) or per-client-global default limit, configurable, defaulting to "unlimited" so existing clients see zero behavior change until an operator sets one (matches PROJECT.md's "аддитивно и обратно совместимо" locked decision). |
| Fail-closed quota check before S3 write + enqueue | Explicitly locked in PROJECT.md: "проверка fail-closed на входе — до записи в S3 и постановки в очередь" | LOW | Insert the check in `handleCreateJob` before the multipart body is streamed to storage — mirrors how format-pair/size validation already gates on request path (413/422 pattern) before any I/O. |
| 429 + `Retry-After` on quota rejection, reusing the existing error envelope | Least-surprising contract: OctoConv's own rate limiter already trained every existing client on this exact response shape; a second, differently-shaped rejection for quotas would be a needless surprise | LOW | Extend `internal/ratelimit`'s `limitHandler` pattern (or a sibling in a new `internal/quota` package) rather than inventing a new status code. See Enforcement section below for the one real nuance (daily/volume quotas vs RPM). |
| Per-engine allowance per client (which engine classes a client may use) | Operational governance need called out in milestone ("доступные классы") — a real internal-platform need: e.g. a low-trust internal caller should not get access to the expensive `av`/`document` engines by default | LOW | A simple allowlist/denylist column or join table keyed by `(client_id, engine)`; check alongside format-pair validation, same fail-closed 422/403-shaped rejection point. |
| Operator-facing usage query (endpoint and/or dashboard) | Milestone requirement #4 verbatim: "оператор может ответить «кто и сколько» без чтения сырых метрик" | LOW-MED | Extends the Phase 26 `OPERATOR_CLIENT_IDS`-gated REST pattern. See Operator Visibility section for the endpoint-vs-dashboard tradeoff — recommendation is both, cheaply. |
| Byte-volume quota (per-window, e.g. daily bytes-in) | Protects against a different failure mode than concurrency: one client uploading a steady stream of huge-but-serialized files that never trips a concurrency cap but saturates S3/network/engine time | MED | Requires summing `job_inputs.size_bytes` over a rolling or calendar window per client — a real query, not free like the concurrency cap. Table stakes because the milestone explicitly calls out "суточный объём" as a target quota dimension. |

### Differentiators (Worth Doing, Not Required for the Goal)

Features that make the accounting/fairness story materially better but aren't the bar for "milestone complete."

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Two-tier weighted priority queue per engine class (`image` + `image-deprioritized`) | Graceful degradation instead of a binary hard-reject once a client crosses a *soft* threshold — asynq supports this natively via weighted queue priority (verified: `hibiken/asynq` processes named queues in a weighted-by-configured-ratio manner, e.g. `{"image":4, "image-deprioritized":1}`), so this is a config change plus one routing decision at enqueue time, not a new scheduler | MED | Bounded queue-count growth: one extra queue *per engine class* (7 extra queues total, not per-client), so it doesn't blow up `AllConvertQueues()`'s compile-time list or force per-client KEDA ScaledObjects — it composes with the existing "engine-class queue routing" pattern in `internal/queue/queue.go` instead of replacing it. KEDA config grows by one extra PromQL series per class (bounded, known upfront). |
| Soft-quota response headers (`X-Quota-*`) so a well-behaved internal caller can self-throttle before hitting a hard reject | Matches the ecosystem convention (`X-RateLimit-Limit/Remaining/Reset`) already adjacent to the existing `Retry-After` contract; internal service authors appreciate not having to hit the wall to discover the ceiling | LOW-MED | Purely additive response headers on success responses; no new enforcement logic. |
| Lightweight usage rollup/materialized view (daily per-client-per-engine aggregates) | Cheap long-horizon reporting without re-scanning a `jobs` table that (per Grounding) never gets pruned and will keep growing | MED | Not required at today's likely volume (internal service, bounded client count) but is the natural next step once the raw-row query starts showing up in `EXPLAIN ANALYZE` as expensive. Recommend as differentiator now, revisit as table-stakes once volume data exists. |
| Small Grafana/dashboard-as-code panel for per-client usage, layered on the Prometheus stack the KEDA milestone already ships | Turns "read raw PromQL" into an actual answer to "кто и сколько" without operators writing queries by hand | LOW-MED | Prometheus is already in the Helm chart (Phase 27); this is a dashboard JSON artifact, not new infrastructure. |
| Configurable `minReplicaCount` per engine class (SEED-011) | Explicitly bundled into this milestone's target features; while not strictly a fairness/accounting feature, it's an adjacent operator-control lever that belongs in the same "operator can shape the platform" theme | LOW | Chart value already close to this shape (currently hardcoded to 0 per class per PROJECT.md); becomes a `values.yaml` field, no new mechanism. |

### Anti-Features (Wrong for This Context — With Reasons)

Every one of these is a pattern that shows up prominently in "quota/fairness/accounting" research because that literature is dominated by commercial multi-tenant SaaS. They are explicitly wrong here.

| Feature | Why Requested (surface appeal, from research) | Why Problematic Here | Alternative |
|---------|------------------------------------------------|----------------------|-------------|
| Billing/invoicing, cost allocation, usage-based pricing UI | This is *the* dominant pattern in "usage accounting" literature (Stripe-style metering, Google Cloud Quotas monetization hooks) | PROJECT.md locks this out explicitly: internal service, no billing, no public multi-tenancy. Building any pricing/cost layer is pure scope creep with zero users who need it | Usage accounting for ops/fairness only — a count and a byte-sum, not a cost |
| Public self-service quota signup/portal, tiered plans (free/premium/enterprise) | Standard SaaS multi-tenant onboarding pattern (seen repeatedly in the noisy-neighbor research: "Enterprise 50 / Premium 20 / Basic 5") | There is no self-service signup for API keys today (`cmd/manage-clients` CLI, operator-run) and no external tenants — a portal implies external trust boundaries this service explicitly doesn't have | Quotas configured the same way API keys already are: an operator-run CLI/admin path, not client self-service |
| Per-request granular metering / cost-per-MB pricing model | Natural extension once you're recording bytes in/out per job | No monetization exists to feed; building a pricing engine with nothing to price is speculative infrastructure | Record bytes for *ops visibility* (byte-volume quota, dashboard) only — never attach a price |
| Dynamic per-tenant virtual queues / full weighted-fair-queueing scheduler (Inngest-style: one logical queue per tenant key, fair round-robin scheduling across all of them) | This is the textbook "correct" fairness answer in the noisy-neighbor literature, and genuinely is the right tool at high concurrency with many competing tenants | With **1-2 concurrent jobs per pod and max 2 pods**, at most 2 jobs run at once per engine class regardless of how many clients are queued behind them — the marginal fairness benefit of a scheduler beyond "cap concurrent jobs per client + optionally deprioritize" is close to zero, while the implementation cost (dynamic queue creation, asynq has no native support for this, would require either N asynq queues or a custom in-process scheduler layered on top of a single Redis queue) is high and touches the compile-time `AllConvertQueues()`/KEDA-per-queue pattern this codebase deliberately keeps small and static | Per-client concurrency cap (admission control) + the bounded two-tier priority queue differentiator above — both fit inside the existing static queue-per-engine-class architecture |
| ML/anomaly-detection-based abuse throttling | Appears in "protecting shared platforms" research for public-facing APIs with unknown/adversarial traffic | Consumers are a small, known, internal set of company services — not adversarial, not anonymous. A threshold quota an operator can read and reason about is strictly better here than an opaque model an operator can't explain to another engineer at 2am | Simple, explainable numeric quotas (concurrency, RPM — already exists, daily bytes/jobs) |
| Formal SLA tiers / contractual quota tiers per client | Natural pairing with "quotas," seen in every commercial quota system reviewed | No SLA concept exists anywhere in OctoConv (no client-facing docs promise response times); inventing tiers formalizes a relationship that doesn't exist and adds a config dimension (tier → quota mapping) for zero present benefit | Flat default quota + optional per-client override, exactly like the existing per-client RPM override shape already established by `ratelimit.PerClient` |
| A new distributed/external rate-limiting service (Envoy global ratelimit, standalone Redis token-bucket cluster) for quota enforcement | Shows up in "scalable quota management" research aimed at horizontally-scaled, high-QPS public APIs | OctoConv's existing rate limiter is deliberately in-process/single-instance today, with its own doc comment noting the Redis-backed upgrade path is future work *if* the API is horizontally scaled — it isn't yet. Quota state (concurrency counts, daily byte sums) needs to be durable and query-able by operators anyway, which points at Postgres, not a new ephemeral counter service | Postgres-backed counts/sums (same system of record everything else already uses), matching the existing "Postgres remains system of record — no new stores" locked constraint |
| Enforcing quotas retroactively by inspecting completed-job outcomes and penalizing after the fact | Superficially simpler than admission-time checks (no extra query on the hot path) | Directly contradicts the milestone's own locked decision: "проверка fail-closed на входе — до записи в S3 и постановки в очередь." Retroactive enforcement lets the damage (S3 write, worker slot occupied) happen before anyone finds out | Admission-time check, synchronous, before the multipart body is streamed to storage |

## Concrete Answers to the Five Questions

### 1. Usage accounting — what to record, at what granularity, retention

**Record per job** (all already representable in the existing schema, per Grounding): `client_id`, `engine`, `source_format`/`target_format`, `bytes_in` (sum `job_inputs.size_bytes`), `bytes_out` (sum `job_outputs.size_bytes`), outcome (`status`, `error_code`), `created_at`, `started_at`, `finished_at`. Wall time = `finished_at - started_at`.

**Granularity: per-job row, not pre-aggregated, as the source of truth.** A per-job row is what lets an operator debug "why is client X's quota tripping" (they can see the actual jobs), which a rollup can't answer. Pre-aggregate only as a *read-side cache* (the Differentiator rollup/materialized view above), computed from the per-job rows, never as the only record.

**How operators query it:** two shapes, both worth having — (a) a REST endpoint under the existing `OPERATOR_CLIENT_IDS` gate returning per-client summaries (jobs count, bytes, engine-seconds, by engine and by day) for scripting/runbooks, and (b) a Prometheus-labeled counter for real-time dashboarding. Don't build only one — an endpoint answers "give me last month's numbers for client X" cheaply; Prometheus answers "is client X spiking right now."

**Retention:** MEDIUM confidence, flagged as an open question rather than a hard recommendation, because it depends on actual traffic volume this research can't observe. What's verifiable: Postgres rows are never deleted today (S3 objects are TTL'd via MinIO ILM but `jobs`/`job_events` are not), and `job_events` is the fastest-growing table (one row per status transition, appended for every job across 7 engine classes). Recommendation: don't build a retention/purge mechanism as part of *this* milestone (it's genuinely orthogonal to accounting/quotas/fairness), but explicitly flag it as a near-future gap in the roadmap output rather than silently accepting unbounded Postgres growth — a sane starting point if it becomes necessary is bounded raw-row retention (e.g. 90-180 days) plus an indefinite rollup, mirroring the pattern already used for S3 (bounded raw objects, want-to-keep aggregate elsewhere).

### 2. Quota models — which protect the shared pool, which look useful but don't

| Model | Actually protects the worker pool? | Verdict |
|-------|--------------------------------------|---------|
| Concurrent-jobs cap (per client, per engine or global) | **Yes, directly.** With 1-2 concurrent slots per pod, this is the only lever that bounds how many of those slots one client can occupy at once | Table stakes |
| Jobs-per-window (e.g. 500/day) | Indirectly — bounds total load over time, but a client can still burst all 500 jobs into the pool in the first minute of the day and starve others within that window unless combined with a concurrency cap | Useful *in combination with* concurrency cap, not alone |
| Bytes-per-window (e.g. 5GB/day) | Indirectly — protects against the "few huge jobs" failure mode that a jobs-per-window count misses entirely (10 files of 500MB each looks fine by job count, terrible by bytes) | Table stakes per milestone's own "суточный объём" requirement; complements jobs-per-window, doesn't replace it |
| Per-engine allowance (which engines a client may call) | Yes, but for a different threat: an untrusted/low-priority internal caller shouldn't be able to hit the expensive `av`/`document`/`ocr` engines at all, regardless of how well-behaved its request rate is | Table stakes (cheap, high governance value) |
| RPM (existing) | Protects the API/DB layer from request-storm, but is a poor proxy for *worker pool* load — a client can stay under RPM while still keeping the worker pool saturated with a few long-running jobs | Already exists; keep, but don't treat it as sufficient for pool protection — it answers a different question than the concurrency cap does |
| Combination: concurrency cap + daily byte/job budget + per-engine allowlist | This combination is the one that actually covers all three realistic abuse shapes (burst-of-many-small, few-huge, wrong-engine-class) | **Recommended model** |

The one that "looks useful but doesn't protect anything" on its own: **jobs-per-window without a concurrency cap.** A window-only limiter caps total throughput over a day but does nothing to stop a client from front-loading its entire daily allowance into the worker pool simultaneously, which is exactly the starvation scenario the milestone is trying to prevent. It has to be paired with a concurrency cap to actually do its job.

### 3. Enforcement behavior — what happens on quota exceeded

**Reject at upload, synchronously, before S3 write and before enqueue** — this matches PROJECT.md's own locked decision and is the least-surprising choice for an internal caller: a caller that gets a clear rejection at request time can retry/backoff/alert immediately, versus discovering minutes later via webhook that a job it thought was queued got silently deprioritized or dropped.

**Status code: 429 + `Retry-After`, reusing the exact contract `internal/ratelimit` already established** for RPM limiting — same status code, same header, same JSON error shape. Internal consumers of this API have already been trained on this exact response by the existing rate limiter; introducing a different code (403? a bespoke 4xx?) for a conceptually similar "you're over a threshold" condition would be needless inconsistency for zero benefit.

**One real nuance the existing RPM limiter doesn't have to handle:** `Retry-After` for an RPM limit is always ~60s (the window), which is a reasonable value to literally sleep-and-retry on. `Retry-After` for a *daily* byte/job quota could be many hours — technically valid per RFC 6585, but a client that naively sleeps for `Retry-After` seconds before retrying a daily quota rejection is a bad experience. Recommendation: distinguish the two cases in the JSON error body (e.g. an `error_code` field like `"rate_limited"` vs `"quota_exceeded"`) so callers/operators can tell "retry shortly" from "come back tomorrow, or ask an operator to raise the limit" apart, even though both use the same HTTP status and header. This is a small addition, not a new contract.

**Queue-but-deprioritize is a *complement* to rejection, not a replacement for it**, and only makes sense for the concurrency dimension: once a client is safely under its hard concurrency cap, a *soft* threshold before the hard cap could route to the two-tier priority queue (Differentiator above) instead of hard-rejecting — giving well-behaved-but-heavy clients graceful degradation rather than a wall. Don't build deprioritization for the byte/job-window quotas — those are naturally "come back later," not "queue with lower priority," since the constraint is total volume, not instantaneous pool occupancy.

**Throttling (delaying the HTTP response instead of rejecting) is not recommended** — it ties up an API request-handler goroutine and a client connection for an indeterminate wait, which is worse for an internal caller than a fast, clear rejection it can handle with its own retry policy.

### 4. Fairness — what's practical with a FIFO-per-engine-class queue and 1-2 workers

Confirmed via `hibiken/asynq`'s own documentation: **priority in asynq is a property of named queues (weighted or strict), not of individual tasks within a queue** — there is no per-task priority field asynq will honor within a single FIFO queue. This directly shapes what's achievable without a rearchitecture:

- **Not practical here: true fair queueing / weighted-fair-queueing / per-tenant dynamic queues.** These are the textbook answer for noisy-neighbor problems at scale (e.g. Inngest's per-tenant concurrency-key virtual queues), but they assume enough concurrent workers that scheduling *order* among many simultaneously-runnable tasks matters. With 1-2 concurrent job slots per engine class, order barely matters compared to *how many slots one client can occupy at all* — see anti-feature table above for the full argument.
- **Practical and recommended: admission control via per-client concurrency cap.** This is the cheapest, and it's the one mechanism that directly bounds worst-case starvation: if a client can hold at most 1 (of 1-2) concurrent slots in an engine class, it structurally cannot fully block other clients even under a burst, because the queue keeps draining as soon as that client's one active job finishes, and other clients' jobs get picked up FIFO from that point. It changes zero queue/worker code — only the enqueue-time admission check in `internal/api`.
- **Practical and recommended as the second layer: bounded two-tier weighted priority queue per engine class** (Differentiator above), using asynq's native weighted-queue feature. This gives graceful degradation for clients that are heavy but under their hard cap, without introducing dynamic per-client queues or touching the KEDA/`AllConvertQueues()` static-list architecture beyond one predictable extra queue per class.
- **FIFO within a queue stays FIFO — don't try to reorder it.** Combined with a concurrency cap, plain FIFO is fine: the cap is what prevents starvation, not queue ordering.

### 5. Operator visibility — what form

Both an endpoint and dashboard surfacing, cheaply, layered on infrastructure that already exists:

- **REST endpoint**, extending the Phase 26 `OPERATOR_CLIENT_IDS`-gated pattern (`/v1/system/presets` precedent) — e.g. `/v1/system/usage?client_id=...&engine=...&from=...&to=...` returning job counts, byte sums, engine-seconds. This is what an operator scripts against for ad hoc questions and incident response ("is client X the reason the `document` queue is backed up right now").
- **Prometheus + a small dashboard**, extending the metrics/Grafana surface the KEDA milestone already ships (Prometheus is already in the Helm chart per Phase 27) — `client_id`-labeled counters/histograms feeding a dashboard panel per engine class, for at-a-glance/real-time visibility without anyone writing PromQL by hand.
- Do **not** build only the dashboard: a dashboard alone doesn't let an operator answer "give me client X's exact usage for the invoice-adjacent-but-not-a-bill monthly review" or feed a script; a raw-metrics-only answer also doesn't satisfy the milestone's own requirement that operators shouldn't have to read raw metrics to get an answer.

## Feature Dependencies

```
client_id label on metrics
    └──requires──> (nothing new; client_id already flows through auth.Middleware into request context)

Per-job accounting query / operator usage endpoint
    └──requires──> client_id on jobs (already exists)
    └──enhances──> Operator visibility (endpoint half)

Concurrent-jobs-per-client admission cap
    └──requires──> per-(client,engine) active/queued job count query (cheap, existing indexes)
    └──enables──> Fairness (structural anti-starvation guarantee)

Byte-volume / jobs-per-window quota
    └──requires──> per-job accounting data (bytes_in/bytes_out already recorded)
    └──complements──> Concurrent-jobs-per-client cap (different abuse shape, see quota model table)

Two-tier weighted priority queue per engine class
    └──requires──> Concurrent-jobs-per-client admission cap (soft threshold below the hard cap)
    └──enhances──> Fairness (graceful degradation instead of binary reject)

Per-engine allowance per client
    └──independent of the above (governance dimension, not a load-protection dimension)

Usage rollup / materialized view
    └──requires──> Per-job accounting data as source of truth
    └──enhances──> Operator visibility at scale (defer until raw-row query cost is measured)

Dashboard
    └──requires──> client_id-labeled Prometheus metrics
```

### Dependency Notes

- **Two-tier priority queue requires the concurrency cap first:** deprioritizing only makes sense as a *softer* response below a hard cap that already exists; building the priority tier before the cap leaves no floor under worst-case starvation.
- **Byte/window quotas complement, not replace, the concurrency cap:** see the quota-model table — each protects against a distinct abuse shape (burst-of-many vs few-huge vs sustained-daily-volume), and neither alone covers the starvation scenario the milestone is aimed at.
- **Rollup conflicts with nothing** but should be sequenced after the raw per-job accounting path ships and only if/when query cost on the (unpruned, ever-growing per the Grounding finding) `jobs` table becomes measurably expensive — building it pre-emptively risks maintaining a cache with no evidence it's needed yet.

## MVP Definition

### Launch With (v1.12, this milestone)

- [ ] `client_id` label on `octoconv_job_outcomes_total`/`octoconv_job_duration_seconds` — closes the stated "чей это трафик" gap directly, near-zero cost
- [ ] Per-client, per-engine (and/or global) concurrent-jobs admission cap, fail-closed at upload, 429 + `Retry-After`, config additive/opt-in so existing clients see zero change until an operator sets one — the one mechanism that structurally prevents starvation given a 1-2-slot worker pool
- [ ] Per-client daily byte-volume and/or jobs-per-window quota, same enforcement contract — covers the abuse shape the concurrency cap alone misses (few-huge-jobs / sustained volume)
- [ ] Per-engine allowance per client — cheap, high governance value, explicitly named in milestone target features ("доступные классы")
- [ ] Operator-facing usage endpoint under the existing `OPERATOR_CLIENT_IDS` gate — directly answers milestone requirement #4
- [ ] `minReplicaCount` per engine class made configurable (SEED-011) — small, already scoped, no new mechanism

### Add After Validation (v1.12.x or next milestone)

- [ ] Two-tier weighted priority queue per engine class — build once the hard concurrency cap is live and an operator has real evidence of clients that would benefit from graceful degradation instead of hard rejection
- [ ] Soft-quota response headers (`X-Quota-*`) — nice DX polish, not required for the milestone goal
- [ ] Dashboard panel(s) on top of the client_id-labeled Prometheus metrics — trigger: once the endpoint has been used a few times and a recurring "I keep querying the same thing" pattern shows up

### Future Consideration (defer, revisit only with evidence)

- [ ] Usage rollup/materialized view for long-horizon reporting — defer until raw `jobs`-table query cost is actually measured as a problem
- [ ] Any retention/purge policy for `jobs`/`job_events` — genuinely orthogonal to this milestone; flag as a gap, don't build blind

## Feature Prioritization Matrix

| Feature | Operator Value | Implementation Cost | Priority |
|---------|-----------------|----------------------|----------|
| `client_id` metric label | HIGH | LOW | P1 |
| Concurrent-jobs-per-client cap | HIGH | LOW-MED | P1 |
| Byte/window quota | HIGH | MED | P1 |
| Per-engine allowance | MED | LOW | P1 |
| Operator usage endpoint | HIGH | LOW-MED | P1 |
| `minReplicaCount` configurable (SEED-011) | LOW-MED | LOW | P1 (already scoped) |
| Two-tier priority queue | MED | MED | P2 |
| Soft-quota headers | LOW-MED | LOW-MED | P2 |
| Dashboard | MED | LOW-MED | P2 |
| Usage rollup table | LOW (today) | MED | P3 |
| Retention/purge policy | LOW (today) | MED | P3 (flag as gap) |

**Priority key:** P1 = this milestone. P2 = valuable, sequence after P1 lands and an operator has real usage evidence. P3 = defer until volume data justifies it.

## Sources

- OctoConv codebase (authoritative, HIGH confidence): `internal/db/migrations/0001_init.sql`, `internal/metrics/metrics.go`, `internal/ratelimit/ratelimit.go`, `internal/queue/queue.go`, `internal/jobs/repo.go`, `.planning/PROJECT.md` (Phase 26 operator-endpoint precedent, KEDA/Prometheus chart precedent, locked decisions on fail-closed/additive/no-new-stores)
- [Queue Priority · hibiken/asynq Wiki](https://github.com/hibiken/asynq/wiki/Queue-Priority) — verified weighted-vs-strict priority is a named-queue property, not per-task (HIGH confidence, official project wiki)
- [Fixing noisy neighbor problems in multi-tenant queueing systems — Inngest Blog](https://www.inngest.com/blog/fixing-multi-tenant-queueing-concurrency-problems) — per-tenant concurrency-key pattern as the SaaS-scale answer, used here to argue *against* porting it at this system's concurrency scale (MEDIUM confidence, vendor blog but technically substantive)
- [Admission Control in Kubernetes](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) and [ResourceQuota admission-control design doc](https://github.com/kubernetes/design-proposals-archive/blob/main/resource-management/admission_control_resource_quota.md) — precedent for "reject at admission time, not retroactively" in a non-billing internal-platform quota system (HIGH confidence, official k8s docs)
- [429 Too Many Requests — MDN](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Status/429) and general rate-limiting best-practice sources (multiple, converging on 429 + `Retry-After`, never 403, for throttle/quota conditions) — MEDIUM-HIGH confidence, converged across sources and consistent with OctoConv's own existing implementation
- The Noisy Neighbor Problem in Multitenant Architectures (Neon), Weighted Fair Queueing (Wikipedia), and related noisy-neighbor pattern surveys — used for the general taxonomy of fairness mechanisms (fair queueing, concurrency caps, WRR, admission control), cross-checked against what asynq's actual API supports (MEDIUM confidence, general pattern literature)

---
*Feature research for: internal multi-consumer file-conversion platform — accounting, quotas, fairness*
*Researched: 2026-08-02*
