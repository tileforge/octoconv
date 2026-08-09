# Pitfalls Research

**Domain:** Adding per-tenant accounting, quotas, and fairness to a live, already-integrated multi-consumer async conversion platform (OctoConv v1.12)
**Researched:** 2026-08-02
**Confidence:** HIGH (grounded directly in this codebase's existing metrics/queue/reconciler/rate-limit implementation) / MEDIUM (external Prometheus-cardinality and KEDA-fairness claims, verified against multiple sources but not against OctoConv's own live Prometheus at scale)

This is not generic "quotas are hard" advice. Every pitfall below is anchored to a specific file, decision, or documented residual risk already in this repository: `internal/metrics/metrics.go`, `internal/metrics/queue_collector.go`, `internal/reconciler/reconciler.go`, `internal/ratelimit/ratelimit.go`, `deploy/chart/octoconv/templates/scaledobject-*.yaml`, and the project's own recorded anti-patterns (constant-copying — "PITFALLS.md Pitfall 7" referenced repeatedly in `.planning/STATE.md`/`PROJECT.md` across the AV/OCR/Pandoc milestones — and env-value drift between Go defaults, docker-compose, `.env.example`, and the Helm ConfigMap, which recurred twice in the v1.11 milestone).

## Critical Pitfalls

### Pitfall 1: `client_id` multiplies existing cardinality, not just adds to it

**What goes wrong:**
Today `octoconv_job_outcomes_total` and `octoconv_job_duration_seconds` are labeled `{engine, status}` — 7 engines × ~3 statuses = ~21 series for the counter, and 21 × 15 histogram buckets for the duration histogram (`internal/metrics/metrics.go:13-22`). Adding `client_id` does not add N series, it **multiplies** the existing label space by N: 21×N for the counter, 21×15×N for the histogram. With ~20-30 internal clients and 7 (soon 8+) engines, this is a low-hundreds-to-low-thousands series count today — survivable — but every new engine class the project has shipped roughly once a month makes the multiplier permanent and growing, and nothing in the current `promauto.NewCounterVec`/`NewHistogramVec` declarations enforces a ceiling on distinct `client_id` values actually seen.

**Why it happens:**
Teams reason about cardinality additively ("one more label, N more series") instead of multiplicatively against every *already-existing* label on the same metric. OctoConv already has 4 metrics; adding `client_id` to all of them "for consistency" (job outcomes, job duration, webhook deliveries, reconciler actions) compounds the problem across every one rather than just the one that actually needs per-client attribution.

**How to avoid:**
- Add `client_id` to exactly one metric family purpose-built for accounting (e.g. a new `octoconv_client_job_total{client_id, engine, status}` counter), not by editing the existing 4 metrics in place. Keep `octoconv_job_outcomes_total`/`octoconv_job_duration_seconds` engine/status-only for operational dashboards and alerting that must stay cheap and stable.
- Do not add `client_id` to a histogram at all — histograms already multiply by bucket count; a per-client duration histogram is the single most expensive shape available. Use a counter (or, if duration matters, per-client only a `_sum`/`_count` pair, not full buckets) for the accounting-purpose metric.
- `clients` is a bounded, operator-managed Postgres table (internal services only) — cap cardinality explicitly by deriving the label set from `SELECT id FROM clients`, not by trusting whatever string arrives at label-time. Never label with anything derived from request input (job id, filename, preset name) even by accident.
- Enforce the cap in code, not by convention: register the accounting metric behind a small allowlist check (reject/collapse to `"unknown"` any `client_id` not present in a periodically-refreshed in-memory set from the `clients` table) so a bug elsewhere (e.g. a malformed UUID leaking through) cannot mint unbounded series.

**Warning signs:**
`/metrics` scrape body size or scrape duration growing measurably after the accounting feature ships; `prometheus_tsdb_symbol_table_size_bytes` or head series count climbing in step with new client onboarding rather than staying flat; Grafana panels querying `octoconv_job_duration_seconds` becoming slow.

**Phase to address:**
The phase that introduces the accounting metric itself — cardinality bounding must be a stated success criterion of that phase, not a follow-up hardening task.

---

### Pitfall 2: Adding `client_id` to `octoconv_queue_depth` breaks the one metric KEDA actually reads

**What goes wrong:**
`octoconv_queue_depth{queue, state}` is collected via `queueDepthCollector.Collect` (`internal/metrics/queue_collector.go:32-45`), which calls `asynq.Inspector.GetQueueInfo(q)` once per queue — a cheap, O(1)-ish aggregate Redis call. KEDA's PromQL is `sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})` (`deploy/chart/octoconv/templates/scaledobject-image.yaml:38`, mirrored per engine). If `client_id` is added to this metric "for consistency" with the accounting work, two things break at once:
1. **Cardinality**: `GetQueueInfo` has no per-client breakdown — producing per-client queue depth would require enumerating individual tasks (`ListPendingTasks`/`ListActiveTasks`/`ListRetryTasks`) per queue per scrape, turning a cheap aggregate call into an O(queue depth) Redis scan on every Prometheus scrape interval, for every one of 8 engine queues.
2. **Query semantics**: KEDA's `sum(...)` already aggregates away extra labels, so functionally nothing changes for KEDA *if done correctly* — but the risk is a copy-paste mistake where someone updates the ScaledObject template's PromQL to filter or group by `client_id` "to be more precise," which then undercounts total backlog (a `sum without(client_id)` bug, or a stray `client_id="x"` filter left in a template) and KEDA scales on a fraction of the real backlog.

**Why it happens:**
"We're adding `client_id` everywhere for attribution" is a natural but wrong generalization — `octoconv_queue_depth` was deliberately relocated from per-worker to the always-on `api` process specifically to make it a single scrape-cheap source of truth for KEDA (documented in Phase 27's KEDA-01 decision and re-verified by `TestQueueDepthMetricRelocationE2E`). It is infrastructure-critical, not an accounting metric, and should be governed by a different, stricter change-control bar.

**How to avoid:**
- Explicit rule for this milestone: `octoconv_queue_depth` gets zero new labels and zero new label values. Per-client backlog visibility (if wanted) is a **separate** metric computed a **separate** way (e.g. periodic Postgres aggregation of `jobs` rows in `queued`/`active` grouped by `client_id`, not derived from the asynq Inspector).
- Treat any diff touching `internal/metrics/queue_collector.go` or `deploy/chart/octoconv/templates/scaledobject-*.yaml` during this milestone as requiring the same live KEDA gate discipline the project already uses (`scripts/keda-gate.sh`) before merge — this metric has a proven blast radius (WR-01: a scrape gap here already once caused KEDA to read an empty queue as "nothing pending").
- Add a pinning test (mirroring the project's existing `TestNewQueueDepthCollectorDescribeAllFourQueues` style) that asserts the `octoconv_queue_depth` label set is exactly `{queue, state}` and fails loudly if a future PR adds a label to it.

**Warning signs:**
Any new label appearing in a diff to `queue_collector.go` or the ScaledObject PromQL strings; scrape latency on `/metrics` (api process) increasing after the accounting feature ships; KEDA's `external.metrics.k8s.io` values disagreeing with `asynqmon`'s reported queue depth for the same queue.

**Phase to address:**
The phase that touches Prometheus metrics at all — make "queue_depth is frozen" an explicit, tested constraint stated in that phase's plan, not an assumption.

---

### Pitfall 3: A cardinality-degraded Prometheus silently degrades autoscaling for the whole platform, not just accounting

**What goes wrong:**
If the accounting metrics (Pitfall 1) do explode cardinality despite precautions, the blast radius is not contained to accounting dashboards. The in-chart Prometheus (`deploy/chart/octoconv/templates/prometheus.yaml`) is the same instance KEDA's Prometheus scaler queries for `octoconv_queue_depth`. High cardinality does not typically crash Prometheus outright, but it does increase scrape duration, WAL/head-block memory, and query latency across *every* series it holds — including KEDA's own polling query. Given the chart's already-adopted defensive posture (`ignoreNullValues: "false"` + `fallback.replicas: 1` from WR-01, precisely because a *different* Prometheus availability gap once caused KEDA to misread an empty scrape as zero backlog), a slow-but-not-down Prometheus under cardinality pressure risks the same failure mode returning through a new door: KEDA's query times out or returns stale data, and every engine class silently falls back to its `fallback.replicas` value instead of scaling to real backlog.

**Why it happens:**
"Accounting" and "autoscaling" feel like separate concerns because they're separate *features*, but in this architecture they share one Prometheus instance and one scrape budget. A change that is purely additive from the accounting feature's point of view is not additive from KEDA's point of view.

**How to avoid:**
- Load-test the `/metrics` scrape (api process) with a synthetic client-population-sized cardinality *before* shipping, not after — the project already has a `scripts/keda-load-proof.sh`-style discipline for measuring real behavior instead of assuming it; reuse that instinct here.
- Consider a scrape-time or exposition-time budget check: alert if total exposed series count crosses a defined ceiling, independent of Prometheus's own storage limits.
- Keep KEDA's scaler query as cheap as it is today (`sum(octoconv_queue_depth{...})` over a 2-label metric) — do not let it become a query that also has to filter/aggregate over a high-cardinality label, even transitively via recording rules that might later get reused carelessly.

**Warning signs:**
KEDA scale-from-zero live-gate timing regressing (compare against the project's own recorded baselines, e.g. Phase 28's 11s/60s SC1 proof) after the accounting feature ships; Prometheus scrape duration or evaluation duration metrics trending up.

**Phase to address:**
Whichever phase ships the client-labeled accounting metrics should include a live regression check against the existing KEDA scale-from-zero gate as an explicit success criterion — not a new gate, the *existing* one, re-run to prove non-regression.

---

### Pitfall 4: In-process quota counters race across API replicas exactly like the existing rate limiter already flags as a future problem

**What goes wrong:**
`internal/ratelimit/ratelimit.go` is explicit in its own package doc: it uses `httprate`'s **in-process, in-memory** counter, which is correct only because today's deployment runs a single `api` replica (`deploy/chart/octoconv/values.yaml`, `api.replicas: 1`). The doc comment already names the upgrade path (`httprate.WithLimitCounter` + a Redis-backed counter) as a *known, deferred* gap. If quota/fairness counters (parallel-jobs-in-flight, daily-volume-consumed) are implemented the same way — an in-process map or counter inside the `api` binary — the moment an operator sets `api.replicas: 2` (a one-line Helm values change, not a code change, and nothing today prevents it), each replica enforces the limit independently: a client with a limit of 10 concurrent jobs can actually run up to 10×N concurrent jobs across N replicas, silently. This is a classic TOCTOU race even at replicas=1 if two requests from the same client land concurrently on the same process and both read-then-write the counter non-atomically.

**Why it happens:**
The rate limiter precedent looks like a template to copy; its known limitation is well-documented but easy to miss if the new code is written by pattern-matching on existing code rather than reading its doc comments. Quotas feel like "the same shape as rate limiting" but have a materially different consequence for correctness — an over-limit *burst* from rate limiting self-heals next window; an over-limit *quota breach* from a race can mean a client silently exceeded its daily volume cap for the rest of the day with no automatic correction.

**How to avoid:**
- Do not implement quota/parallel-jobs counting as in-process state. Use Postgres — the system of record — as the single source of truth, inside the same transaction family as the existing "Postgres-first double write" (`internal/api/handlers.go`, `pgx.BeginFunc`).
- Concretely: the admission check ("is this client under its parallel-jobs limit / daily-volume limit?") must be evaluated with a `SELECT ... FOR UPDATE` (or an equivalent atomic constraint, e.g. a partial unique index / `INSERT ... WHERE (subquery) < limit`) inside the **same transaction** that inserts the `jobs` row — mirroring the guarded-transition pattern (`Repo.transition`, row lock + allow-list check + write, all atomic) already used for job status changes. This closes the race at the database level regardless of how many `api` replicas exist.
- If a fast in-process check is wanted as a pre-filter for cheap rejection (avoid a DB round-trip for obviously-over-limit clients), it must be advisory-only and re-verified atomically in Postgres before the row is actually written — never authoritative on its own.

**Warning signs:**
Load-testing a single client's concurrent job submissions against 2+ `api` replicas and observing more in-flight jobs than the configured limit; any quota-check code path that does not touch Postgres inside the same transaction as job creation.

**Phase to address:**
The phase that implements the fail-closed admission check — this must be a stated design constraint of that phase's plan (Postgres-atomic, replica-count-independent), verified with a live test that runs 2+ API replicas concurrently, not just 1.

---

### Pitfall 5: Reconciler re-enqueue can double-fire usage accounting through the exact race the project already documented and accepted for `RecoveryCount`

**What goes wrong:**
The reconciler's own code comment (`internal/reconciler/reconciler.go:358-376`, T-03-12) documents an **accepted residual race**: if the first `RequeueStale` call actually commits server-side but the client (the reconciler process) observes a transport error before acknowledging it, the reconciler's bounded single retry runs a **second** genuinely-successful transition and records a second recovery event — over-counting `RecoveryCount` by one. This is accepted today because the only consequence is one job reaching `reconciler_exhausted` slightly earlier than intended. If a new usage/accounting write is hooked onto the same code path (e.g., "record machine-time consumed" at the point a job transitions to `done`, or "decrement in-flight quota" at the point a job is marked failed/done), the same race window means a job that was merely *slow, not actually stranded* — and completed normally between the reconciler's `FindStale` read and its `RequeueStale` write — could, in the rare double-fault case, have its terminal transition recorded twice if the accounting write is not independently idempotent.

More generally: a job re-enqueued by the reconciler (whether via the queued/active stale-recovery path or the webhook-gap path) must be counted **once** toward any usage/quota metric, regardless of how many times it is physically re-enqueued. The reconciler's enqueue-first + `asynq.ErrDuplicateTask` guard already solves this for *task execution* (a duplicate enqueue is rejected, so the job doesn't run twice) — but usage accounting is a *separate write path* and does not automatically inherit that guarantee unless it is deliberately built on the same guard or an equivalent one.

**Why it happens:**
It's tempting to add an accounting increment at "the point where I already have the job object and know its terminal status" — e.g., inside the worker's existing `metrics.RecordJobOutcome(...)` call sites (`internal/worker/worker.go`, one per engine, at `MarkDone`/`MarkFailed`). Those call sites already run once per *successful terminal transition* today (because `Repo.transition`'s guarded allow-list makes a second `MarkDone` on an already-`done` job fail), so job-outcome counting is naturally exactly-once. But if a *new* usage/quota write is added as a **separate** statement rather than inside the same guarded transaction as `MarkDone`/`MarkFailed`, it loses that exactly-once property the moment any retry or reconnect logic surrounds it.

**How to avoid:**
- Write usage accounting (bytes processed, machine-time, parallel-slot release) inside the **same Postgres transaction** as the guarded `MarkDone`/`MarkFailed` transition, not as a separate statement after it. `Repo.transition`'s existing pattern (row lock, allow-list check, update, event-log insert, all-or-nothing) is exactly the primitive to extend — add the usage write as one more statement inside that same closure, so it inherits the same "exactly-once because the source status can only be consumed once" guarantee.
- If a separate usage table is used, give it a `UNIQUE(job_id)` constraint (or `UNIQUE(job_id, event_type)` for a ledger) so even an accidental double-write becomes a no-op conflict rather than a silent double-count — cheap, durable insurance independent of getting the transaction boundary exactly right everywhere.
- Never accrue usage at *enqueue* time for anything the reconciler can re-enqueue (queued/active recovery). Accrue it at the terminal transition (done/failed/reconciler_exhausted), which the guarded-transition allow-list already protects from being reached twice for the same job.

**Warning signs:**
A usage/quota decrement or increment that lives in a different function/transaction than `MarkDone`/`MarkFailed`; any accounting table without a uniqueness constraint on job id; a soak test (mirroring the project's existing reconciler soak test discipline, `internal/reconciler/reconciler_soak_test.go`) that intentionally forces the stale-recovery race and checks the usage total, not just the recovery count.

**Phase to address:**
The phase implementing Postgres usage accounting — pair it with a soak/race test analogous to the existing reconciler soak test, specifically exercising a job that completes normally right as the reconciler's stale-sweep would have recovered it.

---

### Pitfall 6: Checking quota after the S3 upload (or not decrementing "in-flight" on failure) creates a cheap DoS and self-inflicted starvation

**What goes wrong:**
The milestone context is explicit: limits must be checked "fail-closed on input — before the S3 write and before enqueue." `handleCreateJob` today already has a precedent for get-this-order-right: content-magic-byte validation runs *before* the S3 write specifically so a malformed/oversized upload never costs a storage write. Two related mistakes are easy to make when adding quota:
1. **Ordering mistake**: implementing the quota/admission check *after* `ParseMultipartForm`/S3 upload (e.g., because the client_id is only convenient to check once other fields are parsed) means an over-quota client's *rejected* request still costs a full multipart parse and S3 PUT — a client already blocked by quota can still cheaply flood S3 storage (and later TTL cleanup load) with uploads that are rejected only at the very end.
2. **Semantics mistake**: if a "max parallel jobs" quota is incremented at admission and only decremented on `done`, a client whose jobs frequently `fail` (e.g., due to a format they don't realize is unsupported) permanently loses in-flight slots — starving *itself* out of its own quota with no automatic recovery, which looks to the client like a platform bug rather than a quota bug.

**Why it happens:**
Quota checks are naturally written where `client_id` is already resolved and convenient (deep in the handler, after auth but often after other parsing), not where the fail-closed contract actually requires them (as early as physically possible). And "decrement on completion" is usually written to mean "decrement on success" by default, because that's the intuitive happy-path case to write first.

**How to avoid:**
- Place the quota admission check immediately after `auth.Middleware` resolves the client and before `ParseMultipartForm` is even called — the same position magic-byte validation earns for content correctness, but one step earlier, since quota doesn't need file bytes at all (only `client_id`, and for daily-volume limits, `Content-Length`/the pre-parse size ceiling already available via `http.MaxBytesReader`).
- Decrement/release any "parallel jobs in flight" counter on **every** terminal transition — `done`, `failed`, and `reconciler_exhausted` alike — not just `done`. This is a direct consequence of Pitfall 5's guidance: hook the release into the same guarded transition that already fires for all three outcomes.
- For daily-volume-style quotas (bytes/day), decide explicitly whether admission is metered on *accepted bytes* (before conversion, matches the existing pre-parse size ceiling) or *consumed machine-time* (after conversion) — and document the choice, since it changes whether a rejected/failed job still "costs" quota. The safer default for an anti-noisy-neighbor goal (not billing) is to meter on admission (bytes/jobs accepted), since the resource cost to the platform (S3 write, queue slot, worker CPU) is incurred regardless of the job's eventual success.

**Warning signs:**
Any quota-check call site that appears after `r.ParseMultipartForm` or after the S3 `Put` call in `handleCreateJob`; an in-flight counter that only has a decrement in the success path; client complaints of quota exhaustion that don't correlate with successful job volume.

**Phase to address:**
The phase implementing the admission gate — the plan for that phase should explicitly state the check's position in the handler (before parse, before S3 write) as a testable success criterion, and the release-on-all-terminal-states requirement as a second one.

---

### Pitfall 7: Shipping enforced limits with no rollout period silently breaks already-integrated consumers

**What goes wrong:**
Every current client of OctoConv is an internal service that has been integrated and running against an *unlimited* API for up to several months (the project's own milestone history runs v1.0 → v1.11 without any concept of per-client caps). Introducing a hard 429/403-style quota rejection with no transition period means the first time any existing integration hits a limit, it fails in production with no warning, no prior signal, and — worse — no guarantee the calling service even has retry/backoff logic for a response shape it has never seen from OctoConv before (existing consumers may treat any non-2xx from `POST /v1/jobs` as a hard failure rather than a "come back later" signal, since today the only non-2xx codes they've ever observed are 401/404/413/422/429-from-rate-limiting).

**Why it happens:**
Quota features are usually designed and tested against the *new* behavior (does the limit actually block a client at N+1?) without equally designing the *transition* from "everyone unlimited" to "some clients limited." The milestone's own locked decision ("existing clients notice nothing until an operator sets a quota") already points at the right default, but that default has to be enforced structurally, not just intended.

**How to avoid:**
- Make "no limit" the literal absence of a row/value, not a large numeric default. E.g., `clients.max_parallel_jobs NULLABLE` with `NULL` meaning unlimited, checked as `limit IS NULL OR count < limit` — never a large hardcoded number like `1000000` that could later be mistaken for a real, low, intentional default (this is the same class of mistake the project has already been bitten by twice this milestone cycle with env-value drift between Go defaults/compose/`.env.example`/Helm ConfigMap — a numeric default that quietly diverges from "actually unlimited" is exactly that pattern applied to a new value).
- Ship an **observe-only mode** first: compute and log/metric what *would* have been rejected (e.g. `metrics.RecordReconcilerAction`-style counter for "would_reject_quota") without actually rejecting, for at least one full operational cycle (the project's own daily/weekly quota window) before flipping enforcement on for any specific client.
- Expose usage-so-far to clients before enforcement exists for them — e.g., response headers (`X-OctoConv-Usage-Today`, `X-OctoConv-Limit`) present even when the limit is `NULL`/unenforced, so integrators can build retry/backoff logic and see their own numbers *before* a limit can ever bite them, not after.
- Roll out enforcement per-client (operator sets a limit on one client at a time, per the milestone's own "quota per client" design), not as a global cutover flag — this bounds the blast radius of the first real limit hit to one integration at a time, mirroring the same "hard cutover with observation" discipline the project used successfully for auth (Phase 1) and content validation (Phase 4), both of which shipped with fail-closed defaults it had already load-tested, not with a silent global flip.

**Warning signs:**
Any migration that sets a non-NULL default limit on existing rows; enforcement code with no corresponding "would-have-blocked" counter; no per-client staged rollout mechanism in the plan.

**Phase to address:**
The phase that ships enforcement (not the phase that ships the accounting data) — the roadmap should structurally separate "collect usage data" (safe, additive, phase N) from "enforce limits based on usage data" (risky, phase N+1 or later), exactly as the milestone's own feature list already implies ("Атрибуция" before "Границы").

---

### Pitfall 8: A fairness mechanism built as per-client queues breaks KEDA's static per-engine PromQL and the reconciler's hardcoded one-queue-per-engine routing

**What goes wrong:**
The most intuitive way to implement "one client can't starve others within an engine class" is per-client sub-queues with weighted dequeue (asynq does support named queues with relative weights, e.g. `critical:6,default:3,low:1`). Applied naively — one asynq queue per `(engine, client)` pair — this breaks multiple load-bearing assumptions simultaneously:
1. **KEDA**: each ScaledObject's PromQL hardcodes a single queue name per engine (`queue="image"`, `queue="audio"`, etc. — `deploy/chart/octoconv/templates/scaledobject-*.yaml`). A dynamic, client-derived set of queue names has no static Helm template to match against; either the ScaledObject query has to `sum` over a wildcard/regex queue-name match (weakening the "exactly this engine's backlog" precision KEDA currently has) or a new ScaledObject would be needed per client — which does not scale operationally and reintroduces the same cardinality problem as Pitfall 1, this time in Kubernetes objects rather than Prometheus series.
2. **Reconciler routing**: `sweep()`'s recovery switch is hardcoded one `Enqueue<Engine>Convert` method per engine (`internal/reconciler/reconciler.go:287-314`), and the `enqueuer` interface has exactly one such method per engine, not per `(engine, client)` pair. Per-client queues would require either N methods per engine (does not scale) or a dynamic queue-name parameter threaded through — a structural change to a component the project has deliberately kept simple and hardcoded through 7 engine additions specifically to keep the recovery path auditable.
3. **`octoconv_queue_depth` collector**: `NewQueueDepthCollector` takes a fixed `queues ...string` list wired at startup (`cmd/api/main.go`) — per-client queues would need this list to grow dynamically as clients are added/removed, which the collector was not built for and which reintroduces Pitfall 1/2's cardinality risk on the metric KEDA depends on.

**Why it happens:**
"Fairness" is conceptually a scheduling/queueing problem, and per-tenant queues are the textbook answer in systems built for exactly that. But OctoConv's queue architecture was deliberately designed around "one queue per **engine class**, routing decided at compile time" (documented pattern: "Engine-class queue routing") specifically to keep KEDA and the reconciler simple — extending the *same axis* (queue identity) to also encode client identity conflates two orthogonal concerns that the architecture currently keeps cleanly separated.

**How to avoid:**
- Implement fairness as **admission control**, not queue restructuring: cap how many of a given client's jobs may be concurrently `active` within one engine class (enforced the same atomic-Postgres way as Pitfall 4/6), so no client can occupy more than its fair share of the worker pool's concurrency, without touching queue topology at all. This keeps exactly one asynq queue per engine, keeps KEDA's PromQL untouched, keeps the reconciler's routing untouched.
- If richer fairness (weighted scheduling, not just a hard per-client concurrency cap) is genuinely required, treat it as a much larger, separately-researched architectural change explicitly out of scope for a milestone whose own locked decision says "queue architecture and KEDA are not rewritten this milestone" — do not let fairness quietly reopen that constraint through the back door of "just add client to the queue name."
- If asynq's native weighted-queue feature is used at all, keep the weight *dimension* aligned with what already exists (engine class), not client — e.g. it is legitimate to weight `image` vs `document` differently, that is orthogonal to per-client fairness and does not touch KEDA's per-engine queries.

**Warning signs:**
Any design doc or PR that proposes a queue name containing `client_id`; any change to the `enqueuer` interface signature to accept a queue-name parameter; any ScaledObject template change from an exact `queue="x"` match to a regex/wildcard match.

**Phase to address:**
Whichever phase designs the fairness mechanism — the plan should state up front "fairness is admission control, not queue topology" as a design constraint, verified by a diff review that touches zero lines in `deploy/chart/octoconv/templates/scaledobject-*.yaml` and zero lines in the reconciler's `enqueuer` interface.

---

### Pitfall 9: Accounting-data growth repeats the platform's existing unbounded-Postgres-growth gap, now on a per-job table

**What goes wrong:**
OctoConv already has an unaddressed retention gap: `jobs`, `job_events`, and `webhook_deliveries` grow forever in Postgres — MinIO's ILM lifecycle rule (`uploads/`/`results/` TTL) only cleans up **S3 objects**, not the Postgres rows describing them (`internal/db/migrations/0001_init.sql`). This has been survivable so far because the service is young and internal-scale, but it is a documented architectural gap, not a solved problem. Adding a new fine-grained per-job usage/accounting table on top — especially if designed as a per-job or (worse) per-event ledger rather than a rollup — creates a second unbounded-growth table with no retention story from day one, at exactly the kind of "many jobs run" service this platform already is (the project's own throughput has been proven at 20 jobs/burst → 4 replicas in 11s in the KEDA load-proof; multiply that by months of steady internal traffic across 8 engine classes).

**Why it happens:**
Accounting features are usually designed around "what data do we need to answer today's question" (who/how much) without designing for "how does this table look after a year of production traffic," and retention/partitioning is easy to defer as a later hardening task — exactly the kind of deferred-but-never-scheduled work this project's own `Out of Scope`/advisory-debt sections already show accumulates (e.g. veraPDF full validation, CFB directory parsing were explicitly deferred with a tracked ID; an accounting table without a retention plan risks becoming an *untracked* version of the same pattern).

**How to avoid:**
- Given the milestone's own explicit "no billing" decision, prefer an **aggregated rollup** (e.g. daily per-`client_id`-per-`engine` counters: jobs count, bytes, machine-time) over a per-job or per-event ledger. A rollup answers "who and how much" without ever growing proportionally to job volume — it grows proportionally to `clients × engines × days`, which is bounded and small.
- If a per-job record is kept anyway (e.g. because the existing `jobs` table with its `jobs_client_created_idx (client_id, created_at DESC)` index is reused directly for reporting, rather than a new table), that index already exists and answers most "recent usage by client" queries without inventing new storage — evaluate whether a new table is needed at all before building one.
- If a new ledger-style table is built regardless, design retention/partitioning (e.g. monthly partitions dropped after N months, mirroring the S3 TTL's intent for Postgres) as part of the same migration that creates the table, not as a follow-up. State the retention window as an explicit, operator-visible constant from day one.
- Treat "the accounting table has no retention policy" as the same class of finding the project's own audits already catch for other subsystems (advisory tech debt items are tracked with an ID and revisited, e.g. AVX-02, K8SV2-03) — give it one, don't let it be silently absent.

**Warning signs:**
A migration adding an accounting/usage table with no corresponding cleanup job, partition scheme, or documented retention window; a design that stores one row per job rather than one row per `(client, engine, day)`.

**Phase to address:**
The phase that creates the accounting schema — retention/growth bound should be a stated design decision in that phase's plan (rollup vs ledger, explicit retention window), not deferred.

---

### Pitfall 10: Treating an advisory/client-side limit signal as an authorization boundary

**What goes wrong:**
Two distinct concerns get conflated under "limits": (a) *authentication/authorization* — is this caller who it claims to be, and is it allowed to act at all (the existing `auth.Middleware`, 401/404 semantics, a real security boundary) — and (b) *quota/fairness* — is this caller currently within its operational budget (a soft, revisable, 429-style operational control, explicitly **not** a security boundary per the milestone's own framing: "это учёт и защита от соседа, а не монетизация"). Two concrete ways this conflation causes real problems in OctoConv's specific architecture:
1. **Bypassable enforcement point**: job creation has more than one entry path today — the direct REST `POST /v1/jobs`, the MCP stdio server (`cmd/mcp-server`), and the in-cluster MCP HTTP server (`cmd/mcp-http`, which does per-request caller-key pass-through per Phase 25). If the quota/admission check is implemented as HTTP middleware on the REST router only, and the MCP servers proxy to the REST endpoint (as they appear to today), enforcement is naturally uniform — but if a future optimization has an MCP path call an internal Go function directly instead of going through the HTTP handler (a plausible "avoid the network hop" refactor), quota silently stops applying to that path. The check belongs in the shared code path both entry points ultimately funnel through (the guarded-transition/Postgres-atomic layer from Pitfall 4), not solely in HTTP middleware, so it cannot be bypassed by a future internal-call shortcut.
2. **Trusting client-supplied priority/quota hints**: if a "fairness" or "priority" opt is ever exposed as a client-settable field (e.g. an `opts.priority` or a header), it must go through the same closed-allowlist, server-validated discipline the project already enforces for `DocOpts`/`AudioOpts`/`OCROpts` (client bytes never reach engine argv unchecked, closed allowlist, strict-parse) — a client-claimed priority/quota-tier value must be **resolved server-side from the `clients` table**, never accepted as authoritative input. The operator-only system routes (`OPERATOR_CLIENT_IDS` allowlist, Phase 26) are the right precedent: operator identity is checked against a server-side allowlist, never trusted from client-supplied data.

**Why it happens:**
"Limits" and "authorization" both produce 4xx responses and both gate access to a resource, so it's easy to reuse the same code path, response semantics, or even the same status code (403) for both — which then makes a legitimate quota-exceeded response indistinguishable from an actual authorization failure to both operators (misdiagnosing incidents) and to client integrations (which may special-case 403 as "never retry," the correct behavior for real authz failures but the wrong behavior for a transient quota condition).

**How to avoid:**
- Keep quota-exceeded on 429 (with `Retry-After`, mirroring the existing rate-limit response shape in `internal/ratelimit/ratelimit.go`'s `limitHandler`), never 403 — 403 stays reserved for the project's existing real authorization failures (cross-client 404-not-403 policy already established for auth; operator-route 403 for non-operator callers).
- Enforce quota checks in the shared, non-bypassable layer identified in Pitfall 4/6 (Postgres-atomic, inside the job-creation transaction), and add an explicit test that exercises quota enforcement through **every** job-creation entry point the project has (REST directly, and at least one MCP path), not just the primary handler — this is the concrete, actionable equivalent of "don't assume all paths funnel through the same gate," it's a test that proves it.
- Any client-supplied field that could influence admission (priority, requested quota tier, "urgent" flags) must be validated against server-side state (the `clients` table), never trusted verbatim — apply the exact discipline already proven for `DocOpts`/`AudioOpts`/`OCROpts` typed-opts-with-closed-allowlist to this new surface.

**Warning signs:**
Any quota-exceeded response using status 403; any admission-check code reachable only from `internal/api/handlers.go` and not from a shared lower layer; any client-settable field named anything like `priority`/`tier`/`quota` that is read and used without a corresponding server-side lookup.

**Phase to address:**
The phase implementing enforcement — include an integration test asserting quota is enforced identically via REST and via at least one MCP entry point as an explicit success criterion.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|-----------------|------------------|
| Reuse `httprate`'s in-process counter for quota (copy the rate-limit pattern) | Fast to write, matches existing code style | Silently wrong the moment `api.replicas > 1`; TOCTOU race even at replicas=1 | Never for quota/fairness — acceptable only for the coarse pre-auth `ByIP` rate limiter it already exists for |
| Add `client_id` to all 4 existing Prometheus metrics "for consistency" | Uniform-looking metrics surface | Multiplicative cardinality across engine × status × client on every metric, including ones that don't need it | Never — add a purpose-built accounting metric instead |
| Per-job usage ledger table instead of a daily rollup | Maximum reporting flexibility, feels more "correct" | Unbounded growth with the exact retention gap `jobs`/`job_events` already have; slow aggregation queries at scale | Only if a genuine per-job audit trail is required by a stated requirement — not by default given the "no billing" decision |
| Hardcode a large numeric default limit (e.g. 1000) instead of `NULL`-means-unlimited | Simple schema, no null-handling logic | Silently becomes a real, low, enforced limit the moment traffic grows past the "large" number — exactly the numeric-default-drift pattern already seen twice this milestone cycle with env values | Never |
| Skip the observe-only rollout period, enable enforcement globally on ship day | Faster to ship the milestone | Un-forewarned production incident for whichever existing consumer happens to be the first over any limit | Never for a live platform with already-integrated consumers |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| Prometheus / KEDA scaler | Adding `client_id` to `octoconv_queue_depth` or any metric KEDA's PromQL touches | Freeze `octoconv_queue_depth`'s label set; put accounting data in separate, KEDA-unrelated metrics/tables |
| Reconciler recovery path | Hooking usage accounting onto a code path that can retry/re-fire (the reconciler's documented T-03-12 residual race) | Write usage inside the same guarded transaction as the terminal status transition, with a `UNIQUE(job_id)` constraint as backstop |
| Helm chart / ScaledObject templates | Making queue names or ScaledObject count depend on `client_id` for fairness | Keep fairness as admission control (Postgres-side concurrency cap per client), zero queue-topology changes |
| MCP server(s) (`cmd/mcp-server`, `cmd/mcp-http`) | Assuming quota enforcement "just works" because MCP proxies to REST today, without a test proving it | Explicit integration test enforcing quota identically via REST and MCP entry points |
| `clients` table / operator tooling | Treating a client-supplied opts field (priority, tier) as authoritative | Resolve any priority/tier/quota-affecting value server-side from `clients`, same discipline as `DocOpts`/`AudioOpts` |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| Per-client label on `octoconv_job_duration_seconds` (histogram) | `/metrics` scrape body grows, Grafana panels querying it slow down | Never label a histogram by client; use a counter, or `_sum`/`_count` only, for the accounting-purpose metric | Roughly linear in `clients × engines × 15 buckets` — noticeable well before "many" clients given 7-8 engines already |
| Deriving per-client queue depth from `asynq.Inspector.ListPendingTasks`-style enumeration | Scrape duration on `/metrics` spikes proportional to actual queue length | Compute per-client backlog from Postgres (`jobs` rows in `queued`/`active` grouped by client), never from asynq's per-task list APIs on the scrape hot path | Breaks as soon as any queue has more than a trivial backlog — this is an O(n) Redis scan per scrape, not the O(1) `GetQueueInfo` call used today |
| Per-job usage ledger with no partitioning | `SELECT`/aggregation queries for "who and how much" get slower month over month | Prefer daily rollups; if a ledger is kept, partition by month/retention window from the first migration | Breaks gradually as job volume accumulates — no sharp threshold, but the project's own KEDA load-proof already demonstrates burst throughput (20 jobs in seconds) that compounds fast over months |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Quota-exceeded returns 403 instead of 429 | Operators/clients misdiagnose a soft operational limit as an authorization failure; clients may hard-fail instead of retrying | Use 429 + `Retry-After`, reserve 403 for actual authorization failures per existing convention |
| Client-supplied priority/tier field trusted without server-side validation | A client could self-declare a higher tier and defeat fairness/quota entirely | Resolve priority/tier from `clients` table server-side, never from request input, mirroring `OPERATOR_CLIENT_IDS` allowlist discipline |
| Quota check implemented only as HTTP middleware on the REST router | A future internal-call shortcut (MCP calling Go functions directly instead of over HTTP) silently bypasses quota | Enforce in the shared Postgres-atomic layer both entry points funnel through, and test both entry points |
| S3 upload occurs before the quota admission check | Over-quota client can still cheaply flood storage before being rejected | Move quota check before `ParseMultipartForm`/S3 write, same ordering discipline already used for magic-byte validation |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|------------------|
| No warning before enforcement begins | Existing integration fails in production with no prior signal | Observe-only period + usage-exposing response headers before any client gets a real enforced limit |
| "In-flight" quota only released on success | Client that has occasional legitimate failures slowly loses all its own concurrency headroom, looks like a platform bug | Release the in-flight slot on every terminal state (`done`, `failed`, `reconciler_exhausted`) |
| Quota-exceeded response indistinguishable from a generic error | Client can't tell "retry later" from "this will never work" | Distinct, documented 429 response shape + `Retry-After`, consistent with the existing rate-limiter's response shape |
| Operator "who and how much" answer requires reading raw Prometheus | Operators fall back to `promql` archaeology for a routine question | Ship the explicit reporting endpoint/dashboard the milestone already scopes, backed by the rollup table (Pitfall 9), not raw metrics |

## "Looks Done But Isn't" Checklist

- [ ] **Client accounting metric added:** Often missing a cardinality ceiling — verify the metric's label values are derived from the bounded `clients` table, not from unchecked request input, and that a pinning test asserts the label set of `octoconv_queue_depth` is unchanged.
- [ ] **Quota admission check added:** Often missing atomicity across replicas — verify the check-and-write happens inside a single Postgres transaction (`SELECT ... FOR UPDATE` or equivalent), not as a separate read-then-write, and verify it under 2+ concurrent `api` replicas, not just 1.
- [ ] **Reconciler-touched jobs and usage accounting:** Often missing idempotency — verify a job that is stale-recovered by the reconciler and then completes normally is counted exactly once in usage accounting, with a test that deliberately forces the recovery race.
- [ ] **Fairness mechanism:** Often missing a KEDA/reconciler regression check — verify zero diff lines in `deploy/chart/octoconv/templates/scaledobject-*.yaml` and zero signature changes to the reconciler's `enqueuer` interface.
- [ ] **Rollout of enforcement:** Often missing an observe-only period — verify there is a "would have rejected" counter/log distinct from actual rejection, and that it ran for at least one full quota window before any client had enforcement enabled.
- [ ] **Accounting table:** Often missing a retention/partitioning story — verify the migration that creates it also states (in code or a comment, matching the project's own documentation discipline) how old data is bounded or rolled up.
- [ ] **Every job-creation entry point:** Often missing MCP-path coverage — verify quota enforcement is tested through both `POST /v1/jobs` directly and at least one MCP entry point (`cmd/mcp-server` or `cmd/mcp-http`).
- [ ] **New limit constants (parallel jobs, daily volume, MB/day):** Often missing cross-service parity — verify the value is defined once and propagated byte-identically to Go default, `.env.example`, docker-compose, and the Helm ConfigMap, following the same `IN-02`-style pinning-test discipline already used for every `*_ENGINE_TIMEOUT` this project has shipped, since env-value drift between these four surfaces has already recurred twice in the prior milestone.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|-----------------|------------------|
| `client_id` cardinality explosion already shipped | MEDIUM | Stop labeling the offending metric by client (roll back the metric definition, not necessarily the whole feature), let old high-cardinality series age out of Prometheus retention, replace with a bounded rollup-table-based reporting path |
| `client_id` accidentally added to `octoconv_queue_depth` | HIGH (touches KEDA) | Revert the collector/label change immediately, re-run the live KEDA scale-from-zero gate to confirm scaling behavior is restored before considering the incident closed |
| Quota race caused an over-limit client to run unbounded jobs | LOW | The Postgres-atomic fix (Pitfall 4) is additive — no data corruption, just tighten the admission check; no rollback of already-completed jobs needed |
| Existing consumer broken by unannounced enforcement | MEDIUM (trust cost is the real cost) | Immediately set the affected client's limit back to `NULL` (unlimited), communicate directly with the owning team, then re-roll-out with the observe-only period this pitfall doc recommends |
| Accounting table grew unbounded before retention was added | MEDIUM | Backfill a rollup table from the raw data, then partition/prune the raw table going forward; do not attempt to retrofit partitioning onto a live unpartitioned table without a maintenance window |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|-------------------|----------------|
| 1. Metric cardinality multiplication | Phase introducing accounting metrics | Pinning test on label sets; scrape-size/duration check before/after |
| 2. `client_id` on `octoconv_queue_depth` | Same phase (metrics) | Pinning test asserting `octoconv_queue_depth` label set is exactly `{queue, state}` |
| 3. Cardinality degrading KEDA transitively | Same phase (metrics) | Re-run existing `scripts/keda-gate.sh`/load-proof gate as a non-regression check |
| 4. In-process/racy quota counters | Phase implementing admission/quota enforcement | Concurrency test with 2+ `api` replicas hammering one client's limit |
| 5. Reconciler double-count race | Phase implementing usage accounting writes | Soak test forcing the stale-recovery race, asserting usage counted once |
| 6. Quota check ordering / release-on-failure | Phase implementing admission gate | Handler-order test (quota check before parse/S3 write); release-on-all-terminal-states test |
| 7. Silent breakage of existing consumers | Phase implementing enforcement (separate from data-collection phase) | Migration review confirming `NULL`-means-unlimited default; observe-only counter present before first client enforcement |
| 8. Fairness breaking KEDA/reconciler | Phase designing fairness mechanism | Zero-diff check on ScaledObject templates and `enqueuer` interface |
| 9. Unbounded accounting-table growth | Phase creating accounting schema | Migration includes stated retention/rollup design |
| 10. Advisory limit treated as authz boundary | Phase implementing enforcement | Integration test enforcing quota identically via REST and MCP paths; 429-not-403 response check |

## Sources

- `internal/metrics/metrics.go`, `internal/metrics/queue_collector.go`, `internal/metrics/metrics_test.go` — current metric shapes and the queue-depth collector's design intent (HIGH confidence, direct code read)
- `internal/reconciler/reconciler.go` — enqueue-first + `asynq.ErrDuplicateTask` guard, the documented T-03-12 accepted residual race, and the hardcoded per-engine routing switch (HIGH confidence, direct code read)
- `internal/ratelimit/ratelimit.go` — documented in-process-counter limitation and its own stated upgrade path (HIGH confidence, direct code read)
- `internal/api/handlers.go`, `internal/jobs/repo.go` — Postgres-first double write and guarded-transition pattern (HIGH confidence, direct code read)
- `deploy/chart/octoconv/templates/scaledobject-*.yaml`, `deploy/chart/octoconv/values.yaml` — KEDA PromQL shape and `api.replicas` configurability (HIGH confidence, direct code read)
- `internal/db/migrations/0001_init.sql` — existing table/index shapes, absence of Postgres-side retention vs. MinIO's S3-only ILM TTL (HIGH confidence, direct code read)
- `.planning/PROJECT.md`, `.planning/STATE.md` — the project's own recorded anti-patterns: constant-copying ("PITFALLS.md Pitfall 7" recurrence across AV/OCR/Pandoc milestones) and env-value drift between Go defaults/compose/`.env.example`/Helm ConfigMap, recurring twice in the v1.11 milestone (HIGH confidence, direct project-history read)
- [How to Create Prometheus Label Best Practices](https://oneuptime.com/blog/post/2026-01-30-prometheus-label-best-practices/view) — multiplicative cardinality effect of adding a label to an existing metric (MEDIUM confidence, WebSearch, cross-checked against multiple similar sources)
- [What are the Limitations of Prometheus Labels? | SigNoz](https://signoz.io/guides/what-are-the-limitations-of-prometheus-labels/) — cardinality/memory relationship (MEDIUM confidence)
- [Introduce a mechanism to guard against unbounded cardinality · prometheus/client_golang Discussion #970](https://github.com/prometheus/client_golang/discussions/970) — client-side cardinality guarding pattern referenced in Pitfall 1's prevention strategy (MEDIUM confidence)
- [High Cardinality in Prometheus: How to Find and Fix It | Last9](https://last9.io/blog/how-to-manage-high-cardinality-metrics-in-prometheus/) — bucketing/rollup recommendations (MEDIUM confidence)
- KEDA fairness/priority-queue blind-spot claims (Pitfall 8) are derived primarily from this codebase's own architecture (queue-per-engine routing, static ScaledObject PromQL) rather than external sources — general web search did not surface KEDA-specific fairness literature, so this pitfall is LOW-external-confidence but HIGH-internal-confidence (grounded directly in the reconciler/chart code cited above)

---
*Pitfalls research for: OctoConv v1.12 Consumer Accounting & Fairness milestone*
*Researched: 2026-08-02*
