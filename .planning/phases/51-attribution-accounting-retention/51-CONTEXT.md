# Phase 51: Attribution, Accounting & Retention - Context

**Gathered:** 2026-08-02
**Status:** Ready for planning

<domain>
## Phase Boundary

Per-client consumption becomes observable and durable: a `client_id`-labeled Prometheus counter, a Postgres daily rollup written inside the same transaction as the guarded terminal transition, an explicit numeric cardinality ceiling, and an **executable** retention mechanism that also closes the platform's pre-existing unbounded-growth debt. First phase of v1.12 — everything downstream (quotas, fairness, operator visibility) reads what this phase produces. Covers ACCT-01..04 and RETN-01.

**In scope:** the new `client_usage_daily` table + its migration; the rollup write wired into `Repo.transition`'s existing `apply` seam; a new client-labeled counter in `internal/metrics`; the cardinality ceiling + overflow behavior; a batched purge running on the reconciler's existing ticker, covering `jobs`/`job_events` and the new table; the tests that prove exactly-once accounting and actual row removal.

**Out of scope:** quota configuration and enforcement (Phase 52 — this phase only produces the data quotas will read); the operator-facing usage endpoint (Phase 53); `minReplicaCount` (Phase 54); the multi-replica live gate (Phase 55). No change to queue topology, `octoconv_queue_depth`, or any `ScaledObject`.
</domain>

<decisions>
## Implementation Decisions

### Accounting table shape (ACCT-02)
- **D-01:** A dedicated **`client_usage_daily`** rollup table, keyed `(client_id, engine, day)` — NOT on-the-fly aggregation over `jobs`. This resolves Open Decision #1 in REQUIREMENTS.md. Three reasons, in order of weight: (a) usage history must **survive the `jobs` retention purge** this same phase introduces — aggregating over `jobs` would silently truncate consumption history to the retention window; (b) it grows as `clients × engines × days`, which is predictable and small, rather than with raw job volume; (c) Phase 52 needs somewhere to record reservations distinct from consumption, and a derived query has nowhere to put them.

### What is recorded (ACCT-02)
- **D-02:** Jobs count, bytes in, bytes out, **and wall-time**. Wall-time is load-bearing, not decorative: a 10 MB video and a 10 MB PDF consume wildly different worker capacity, and byte counts cannot show that. The milestone's motivating question is "who ate the workers", which bytes alone cannot answer.
- **D-03:** Bytes are summed over **all** of a job's inputs and outputs, never read at ordinal 0 — the 1:1 constraint already recorded in ROADMAP Phase 51 and ACCT-02 (SEED-017).
  - **Correction (2026-08-02, from pattern mapping):** the methods are `Repo.Inputs`/`Repo.Outputs` (`internal/jobs/repo.go:421,448`) — an earlier draft of this file named them `ListInputs`/`ListOutputs`, which do not exist. More importantly, **both are scoped to `r.pool`, not to a `tx`**. Calling them as-is from inside `transition`'s `apply` closure would read outside the transaction, which defeats D-05's whole point. Either give them `tx`-scoped variants or inline a `SELECT sum(size_bytes)` against `tx`. This is a real design decision the plan must make explicitly, not a rename.
- **D-03a (from pattern mapping):** `MarkDone(ctx, id)` and `MarkFailed(ctx, id, code, message, detail)` carry neither `client_id`, nor `engine`, nor duration. The rollup needs all three, so **their signatures change** — this is not a copy-paste of an existing call shape. The values already exist at the call sites in `internal/worker/worker.go` (the `job` and `start` locals); they are simply not threaded into the repo layer today.
- **D-04:** **Failed jobs are counted**, with `status` as a column so any consumer can slice. A failed conversion burned CPU and occupied a worker slot exactly like a successful one; excluding failures would make a client with broken inputs look free and would leave a hole Phase 52's quota could not close. Storing status rather than pre-filtering keeps the choice with the reader.

### Write site and exactly-once (ACCT-03)
- **D-05:** The rollup write happens **inside the same transaction as the guarded status transition**, not as an adjacent call. `Repo.transition` already takes an `apply func(ctx, tx) error` closure — the seam exists; use it rather than inventing one. Rationale is the PR #3 review finding: a worker dying between a committed `MarkDone` and a separate accounting write leaves the job terminal, and neither an asynq retry nor the reconciler revisits a terminal job, so the usage is lost permanently and silently.
- **D-06:** Idempotency must hold against the reconciler's documented stale-recovery race — a job recovered and re-processed must not be counted twice. Since D-05 puts the write in the transition transaction and the transition is already guarded by an allow-list on the source status, a second terminal transition for the same job cannot commit; that property is what makes exactly-once true, and the test must prove it by forcing the race rather than asserting it.

### Prometheus shape and cardinality (ACCT-01, ACCT-04)
- **D-07:** `client_id` goes on a **new, purpose-built CounterVec**. It is NOT added to `octoconv_job_duration_seconds` (a histogram already at ~250 series — the label would multiply it) and nothing in this phase touches `octoconv_queue_depth`, the only metric KEDA reads.
- **D-08:** Ceiling is **200 distinct client label values**. At 7 engines × ~5 statuses that bounds the new counter at roughly 7 000 series — safe for Prometheus and far above the plausible number of internal consumers.
- **D-09:** Overflow collapses into a single stable **`overflow`** label value. Data is not lost, only its per-client resolution — an operator still sees that the traffic exists, and the Postgres rollup remains exact regardless of the metric ceiling.
- **D-10:** Slots are allocated **first-seen-first-served** after process start. Chosen for being deterministic and state-free. The consequence must be documented in-code, not discovered: **the labeled set can change across an API restart**, so a dashboard may attribute differently after a redeploy. Acceptable because the Postgres rollup is the authoritative record and the metric is for alerting/observation.

### Retention (RETN-01)
- **D-11:** A **batched purge running on the reconciler's existing ticker** — not partitioning, not a new k8s CronJob. The reconciler already has a periodic sweep, an advisory lock against concurrent runs, and an actions metric; reusing it means zero new deploy units and retention that works identically in compose and k8s. Partitioning `jobs`/`job_events` is genuinely cheaper at large volume and is the right answer later, but migrating live tables to partitioned ones carries risk disproportionate to a phase whose subject is accounting.
- **D-12:** Windows: **`jobs`/`job_events` 90 days**, **`client_usage_daily` 400 days**. The job rows are an operational record and S3 artifacts already expire at 168h, so a quarter is generous; the rollup is tiny and >1 year enables year-over-year comparison. Both windows are **operator-configurable via env with these defaults**, following the existing `STORAGE_TTL` convention — not hardcoded constants.
- **D-13:** Retention must be **executable and proven**: a test ages data and asserts the rows are actually gone. A policy stated in a migration comment does not satisfy RETN-01 (PR #3 review finding).

### Claude's Discretion
- Exact column types and index choices on `client_usage_daily`; the migration ordinal; purge batch size and how it bounds a single sweep's work; the counter's exact metric name and help text; whether the ceiling bookkeeping lives in `internal/metrics` or beside it; how the purge reports what it deleted through the existing reconciler actions metric — all planner/executor's call, provided: the write is inside the transition transaction, bytes sum over the ordinal lists, the ceiling is a configured number with an `overflow` collapse, retention is proven by a test that ages data, and `octoconv_queue_depth` plus all seven `ScaledObject` PromQL strings are untouched.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Corrected design guidance (authoritative)
- `.planning/research/SUMMARY.md` §"Corrections after PR #3 review (2026-08-02)" — **this section overrides `ARCHITECTURE.md` wherever they disagree.** It carries the four defects found in review, two of which bind this phase directly (usage write inside the terminal transaction; reservations distinguishable from consumption).
- `.planning/research/ARCHITECTURE.md` — integration shape and build order. Useful, but read the corrections above FIRST; its proposed `RecordUsage`-after-transition ordering is wrong.
- `.planning/research/PITFALLS.md` — cardinality multiplication, the reconciler double-count race, and the unbounded-growth pattern this phase must not repeat.

### The seams this phase touches
- `internal/jobs/repo.go` — `Repo.transition`'s `apply func(ctx, tx) error` closure is the write site for D-05; `Repo.Inputs`/`Repo.Outputs` (already `ORDER BY ordinal`) are the byte source for D-03.
- `internal/metrics/metrics.go` — the `promauto.NewCounterVec` registration idiom to clone for D-07; `octoconv_job_duration_seconds` is the histogram that must NOT gain the label.
- `internal/worker/worker.go` — the seven `metrics.RecordJobOutcome(...)` call sites, currently invoked AFTER the transition (outside the transaction). D-05 changes where accounting happens relative to these.
- `internal/reconciler/reconciler.go` — the existing ticker, advisory lock and actions metric that D-11's purge reuses; also the source of the stale-recovery race D-06 must be tested against.
- `internal/db/migrations/0001_init.sql` — `jobs`, `job_events`, `job_inputs`/`job_outputs` (note the `ordinal` columns), and the `clients` table the ceiling is measured against.

### Requirements and milestone contract
- `.planning/REQUIREMENTS.md` — ACCT-01..04, RETN-01, plus the 1:1 constraint recorded under ACCT-02.
- `.planning/ROADMAP.md` §"Phase 51" — the six success criteria, four of which were tightened after the PR #3 review; the "do not hardcode one-input/one-output" constraint block.
- `.planning/seeds/SEED-017.md` — why the 1:1 constraint exists and what will exercise it.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `Repo.transition`'s `apply` closure — the transactional seam for the rollup write already exists; no new transaction plumbing needed.
- `Repo.Inputs`/`Repo.Outputs` already read `ORDER BY ordinal`, so multi-artifact byte summing needs no repository change.
- The reconciler's periodic sweep + advisory lock + actions metric — the purge's host.
- `promauto.NewCounterVec` with an explicit label list — the metric idiom, cloned rather than invented.
- `STORAGE_TTL` — the precedent for an operator-configurable retention window read from env.

### Established Patterns
- Guarded status transitions with an allow-list on the source status; illegal transitions error rather than silently overwrite.
- Postgres is the system of record; Redis is a transient broker.
- Env-configured operational knobs with documented defaults, propagated identically across every service (IN-02 discipline).

### Integration Points
- New migration for `client_usage_daily`; `internal/jobs/repo.go` (write inside `transition`); `internal/metrics` (new counter + ceiling bookkeeping); `internal/reconciler` (purge sweep); worker call sites for the outcome recording that moves. Nothing in `internal/queue`, and no chart change.
</code_context>

<specifics>
## Specific Ideas
- Reuse `Repo.transition`'s `apply` closure rather than adding a second write path — the exactly-once property falls out of the guarded transition instead of being enforced separately.
- The idempotency test should force the reconciler's stale-recovery race deliberately and assert the usage total, not just that recovery happened.
- The retention test should age rows and assert they are gone — the criterion is deliberately unsatisfiable by documentation.
</specifics>

<deferred>
## Deferred Ideas
- Quota configuration, reservations and enforcement → Phase 52 (this phase only produces the data).
- Operator usage endpoint → Phase 53.
- Partitioning `jobs`/`job_events` instead of a purge → revisit when volume makes `DELETE` expensive; recorded here as the known better answer at scale (D-11).
- CPU-time accounting beyond wall-time → `ACCT-V2-01`.
- Per-client resolution above 200 clients → would need a different metric strategy (Postgres-backed dashboards rather than PromQL), noted with D-08/D-09.
</deferred>

---

*Phase: 51-attribution-accounting-retention*
*Context gathered: 2026-08-02*
