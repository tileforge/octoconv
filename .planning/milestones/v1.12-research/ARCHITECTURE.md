# Architecture Research: Consumer Accounting & Fairness Integration

**Domain:** Integrating per-client accounting, quota enforcement, and intra-class fairness into an existing Go async file-conversion service (OctoConv v1.12)
**Researched:** 2026-08-02
**Confidence:** HIGH (all findings grounded in the actual repo files read below; no external ecosystem claims required — this is an internal-integration question, not a "what does the ecosystem look like" question)

**Files read for this research:** `.planning/PROJECT.md`, `internal/api/handlers.go`, `internal/api/routes.go`, `internal/queue/queue.go`, `internal/jobs/repo.go`, `internal/metrics/metrics.go`, `internal/metrics/queue_collector.go`, `internal/ratelimit/ratelimit.go` (+ its doc comment), `internal/clients/clients.go`, `internal/worker/worker.go`, `internal/reconciler/reconciler.go`, `internal/db/migrations/0001_init.sql`, `deploy/chart/octoconv/templates/scaledobject-*.yaml`.

`.planning/codebase/ARCHITECTURE.md` does not exist as a standalone file at that path — the equivalent codebase architecture map is already embedded in this project's `CLAUDE.md` (component responsibilities, layers, patterns) and was used as background; nothing in it contradicts what direct file reading confirmed below.

---

## System Overview (current, unmodified)

```
POST /v1/jobs (multipart)
   │
   ▼
chi middleware: RequestID → ClientIPFromRemoteAddr → Logger → Recoverer
   │  (/v1 subrouter)
   ▼
ratelimit.ByIP (in-process, pre-auth, coarse flood guard)
   │
   ▼
auth.Middleware (resolves client_id from API key → context)
   │
   ▼
ratelimit.PerClient (in-process, per-client_id RPM, httprate in-memory counter)
   │
   ▼
handleCreateJob:
   parse multipart → preset/target XOR gate → read file →
   client := auth.ClientFromContext(ctx) →
   preset resolve (if used) →
   Sniff/SniffContainer/LooksLikeHTML/.../IsOLECFB/SniffVideo/SniffAudio
     (fail-closed content detection, several hundred lines, all BEFORE storage) →
   engine, ok := convert.Default.EngineFor(detected, target) →
   maxEngineBytes[engine] size-tier check →
   dimension/decompression-bomb check →
   callback_url SSRF validate →
   opts parse+validate (engine-keyed) →
   ── s.storage.Upload(ctx, key, rest, ...) ──  (S3 write — the "gate" line)
   preset TOCTOU re-check (Pitfall 8 pattern) →
   s.repo.Create(...)   (Postgres-first double write, one tx: jobs+job_inputs+job_events) →
   s.queue.EnqueueXConvert(ctx, createdID)   (asynq, engine-class queue, job_id-only payload)
```

Downstream: one asynq queue per engine class (`image`/`document`/`html`/`audio`/`av`/`ocr`/`pandoc`), one worker binary per class, each `Handle*Convert` in `internal/worker/worker.go` follows the identical shape: `repo.Get(jobID)` → `MarkActive` → `start := time.Now()` → `process(ctx, job)` → `MarkDone`/`MarkFailed` → `metrics.RecordJobOutcome(engine, status, time.Since(start))`. `octoconv_queue_depth{queue,state}` is collected pull-based from `asynq.Inspector.GetQueueInfo` in the always-on `api` process (`internal/metrics/queue_collector.go`) and is the sole signal all 7 KEDA `ScaledObject`s query via `sum(octoconv_queue_depth{queue="X", state=~"pending|active|retry"})`.

---

## Key Pre-Existing Facts That Shape This Milestone

These are not proposals — they are things already true in the codebase that materially constrain the design:

1. **Per-job usage data already fully exists.** `jobs.client_id`, `jobs.started_at`, `jobs.finished_at` (→ duration), `job_inputs.size_bytes` (→ bytes in), `job_outputs.size_bytes` (→ bytes out) are all already columns/rows written today by `Repo.Create`/`AddOutput`/`transition`. **Nothing needs to be captured that isn't already captured.** The gap is purely: (a) no Prometheus label carries `client_id`, and (b) no aggregate/rollup exists for O(1) quota checks or "who/how much" reporting without scanning `jobs`.

2. **`jobs.priority int NOT NULL DEFAULT 0`** already exists in the schema (`0001_init.sql`) and is **completely unused** by any Go code today (`grep` confirms no reader). It is a pre-existing hook, not wired to asynq (asynq has no concept of intra-queue task priority by score — only inter-queue weighting). Do not assume it "just works" for fairness; asynq would need this value read at enqueue time and mapped to something asynq actually understands (a queue name), which it is not currently.

3. **Redis is explicitly transient (broker only)** — this is a locked architectural line, restated in the milestone context and consistent with `internal/ratelimit`'s own doc comment, which already anticipates this exact problem and pre-writes the answer: *"If the API is later horizontally scaled behind a load balancer, this package's state stops being shared across replicas; the upgrade path at that point is httprate's WithLimitCounter option backed by a Redis-based counter... not a rewrite of the ByIP/PerClient call sites."* That comment is about **rate limiting** (a soft, resettable-every-minute signal), not **quota** (a hard, auditable, must-not-silently-lose-count signal tied to money-adjacent trust between internal teams). The two must not be conflated: a lost Redis counter increment is an acceptable failure mode for RPM shaping; it is **not** acceptable for a quota that gates whether a client is allowed to keep working today. **Conclusion: the quota counter of record must live in Postgres, not Redis**, precisely because Postgres — not Redis — is this system's system of record, and the milestone context explicitly locks "no new storages."

4. **`octoconv_queue_depth` is labeled `{queue, state}` only** — it is a per-queue-wide `asynq.Inspector.GetQueueInfo` call, with **no per-client breakdown possible**: asynq's task payload (`ConvertPayload{JobID}`) carries no client_id, and asynq's Redis queue is a flat structure per queue name, not partitioned by client. Any fairness design that requires per-client visibility *inside* the queue (sub-queues, weighted dispatch) necessarily changes queue *names*, which is exactly what all 7 `ScaledObject`s' `query: sum(octoconv_queue_depth{queue="X", ...})` PromQL match on literally. **This is the tripwire the quality gate asks to flag — see Q4 below.**

5. **asynq has no built-in intra-queue fairness primitive.** Its only native "fairness"-adjacent feature is *inter-queue weighting* (`asynq.Config.Queues map[string]int`, a static, process-startup-time config), not per-key round robin within one queue. This rules out a "peek ahead and skip" dispatcher without leaving asynq's supported feature surface — which the milestone context forbids ("архитектура очередей и KEDA не переписывается" — queue/KEDA architecture is not rewritten).

6. **The codebase already has a precedent for the exact TOCTOU shape this milestone needs**: the preset-resolution re-check immediately before `repo.Create` (`internal/api/handlers.go:673-687`, "Pitfall 8"). It resolves once early (informational), then **re-resolves atomically right before the Postgres-first write**, and on mismatch aborts job creation *without deleting the already-uploaded S3 object* ("left in place for TTL cleanup, never deleted here"). Quota's TOCTOU window (two concurrent uploads from the same client both passing an early soft check) is structurally identical and should reuse this exact shape rather than invent a new one.

---

## Q1 — Where does quota enforcement belong, and what is the ordering?

**Quota is not one gate — it is three gates of different cost and different authority, layered exactly the way `maxUploadByte` → `maxEngineBytes[engine]` are already layered today.**

| Tier | What it checks | Where (line reference is current `handlers.go`) | Cost | Authority |
|---|---|---|---|---|
| 1. Concurrency admission | "does this client already have ≥N jobs in `queued`/`active`?" | Immediately after `client, _ := auth.ClientFromContext(ctx)` (~line 164) — **before** preset resolution, **before** any content sniffing | One indexed `SELECT count(*) FROM jobs WHERE client_id=$1 AND status IN ('queued','active')` (needs a new partial index, see Q2) | Soft fail-fast (best-effort; a low-cost early exit that saves S3 bandwidth for the common "client is way over cap" case) |
| 2. Class + byte-size soft check | "is `engine` in this client's allowed set? would `header.Size` alone blow the daily byte budget?" | Extend the **existing** `if limit, ok := s.maxEngineBytes[engine]; ok && header.Size > limit` block (~line 402) — same slot, same "checked immediately after the engine class is known and strictly before `s.storage.Upload`" discipline that D-07 already documents | One read of the client's usage-aggregate row (see Q2) | Soft fail-fast — still before the S3 write, so a client obviously over quota never touches storage |
| 3. Atomic authoritative reserve | The real enforcement: an atomic conditional increment against the usage aggregate | Immediately before `s.repo.Create(...)` (~line 691), in the **same slot** as the existing preset TOCTOU re-check (Pitfall 8) | One `UPDATE ... WHERE usage + delta <= limit RETURNING ...` (or `INSERT ... ON CONFLICT DO UPDATE ... RETURNING`) | Authoritative — this is what actually prevents overshoot under concurrent requests from the same client |

**Why three tiers and not one:** tiers 1–2 exist purely so a client who is obviously over quota never causes an S3 write (matches this codebase's "validate before storage write" discipline verbatim — quota is a validation gate like magic-byte sniffing or dimension checks, not a traffic-shaping concern like RPM). Tier 3 exists because tiers 1–2, evaluated pre-upload, have the same TOCTOU window the preset code already solved: two concurrent uploads from the same client can both pass a soft check. Tier 3 must be atomic and must be the last thing that can abort job creation — and, matching existing behavior for every other late-stage failure in this handler (preset re-check failure, enqueue failure), **a tier-3 rejection must leave the already-uploaded S3 object in place for TTL cleanup, never delete it inline.**

**Relative to auth / rate limiting / content sniffing / S3 write:**
- Quota tiers 1–2 sit strictly **after** `auth.Middleware` (client identity is a hard prerequisite) and **after** `ratelimit.ByIP`/`ratelimit.PerClient` (those are unrelated, request-pacing concerns that should reject first and cheaply — no reason to spend a Postgres round-trip on a request that RPM would reject anyway).
- Quota tier 1 sits **before** content sniffing (cheapest possible rejection point, no dependency on detected format).
- Quota tier 2 sits **after** `EngineFor` resolves the engine class (class-scoped quotas are meaningless before the engine is known) but **before** `s.storage.Upload`.
- Quota tier 3 sits **after** `s.storage.Upload` (matching the preset TOCTOU precedent exactly — that recheck also runs post-upload) but **before** `s.repo.Create`.
- **Do not implement tier 1/2 as new chi middleware** the way `ratelimit.PerClient` is implemented. `ratelimit.PerClient` is stateless and in-process by design (a resettable RPM signal); quota tiers need a Postgres round-trip and engine-scoped logic that is only knowable deep inside the handler (`engine` isn't resolved until after content sniffing). Keeping quota checks inline in `handleCreateJob`, as sequential fail-closed gates using the existing `writeError(w, status, msg)` convention, is consistent with every other validation gate in this file (opts, dimensions, SSRF, content sniffing) and avoids splitting quota logic across two different failure-reporting idioms (chi middleware vs. handler-local `writeError`).
- **Open question to flag for roadmap, not resolved here:** what HTTP status/semantics quota rejections use. Daily-volume overage plausibly wants `429 + Retry-After` (bounded, resets on a known schedule, mirrors the existing rate-limit contract clients already parse). Concurrent-jobs-cap rejection has no fixed retry time (depends on when an in-flight job finishes) — `429` without `Retry-After`, or a distinct `403`, are both defensible; this is a product decision, not an architecture one, and should be a roadmap decision point, not assumed.

---

## Q2 — Where does usage get recorded? New table, new columns, or an aggregate?

**Answer: an aggregate/rollup table — new, additive, zero-behavior-change to existing tables.** Per-job raw usage does **not** need new capture, per fact #1 above. What's missing is a query-cheap rollup.

**Recommended new table** (name illustrative, e.g. `client_usage_daily`):

```sql
CREATE TABLE client_usage_daily (
    client_id      uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    usage_date     date NOT NULL,           -- UTC day bucket
    engine         text NOT NULL,           -- one row per (client, day, engine) — matches jobs.engine's CHECK domain
    jobs_count     bigint NOT NULL DEFAULT 0,
    bytes_in       bigint NOT NULL DEFAULT 0,
    bytes_out      bigint NOT NULL DEFAULT 0,
    engine_seconds double precision NOT NULL DEFAULT 0,
    PRIMARY KEY (client_id, usage_date, engine)
);
```

**Where it gets written — the least invasive point:** every `Handle*Convert` in `internal/worker/worker.go` already computes `time.Since(start)` and calls `metrics.RecordJobOutcome(queue.QueueX, status, time.Since(start))` at exactly one point per handler, immediately after `MarkDone`/`MarkFailed`. `job.ClientID` is already in scope there (`job, err := h.repo.Get(ctx, jobID)` at the top of every handler already populates `ClientID`). **This is the single natural choke point for both new responsibilities**, added as one extra call alongside the existing metrics call:

```go
metrics.RecordJobOutcome(queue.QueueImage, jobs.StatusDone, time.Since(start))  // existing
metrics.RecordJobOutcome(...) // -> also pass job.ClientID as a new label (see below)
repo.RecordUsage(ctx, job.ClientID, job.Engine, bytesIn, bytesOut, time.Since(start))  // new — atomic UPSERT
```

`RecordUsage` should be a single `INSERT ... ON CONFLICT (client_id, usage_date, engine) DO UPDATE SET jobs_count = client_usage_daily.jobs_count + 1, bytes_in = ... + $n, ... RETURNING jobs_count, bytes_in+bytes_out` — this single statement is **also** the atomic primitive Q3's tier-3 reserve check reads/writes against (see Q3) — one table serves both accounting-for-reporting and quota-enforcement, avoiding duplicate bookkeeping.

**Why not new `jobs` columns:** bytes/duration are already derivable from existing columns (`job_inputs.size_bytes`, `job_outputs.size_bytes`, `started_at`/`finished_at`) — adding duplicate columns to `jobs` would be redundant storage with a consistency-drift risk (two places that can disagree). The only thing worth adding to `jobs`/Prometheus is the **label**, not new raw data.

**Prometheus:** add `client_id` as a label on `octoconv_job_outcomes_total`/`octoconv_job_duration_seconds` (`internal/metrics/metrics.go`), changing `RecordJobOutcome(engine, status, d)` → `RecordJobOutcome(engine, status, clientID, d)`. This is the explicitly pre-accepted cardinality growth per `PROJECT.md`: *"client_id как метка Prometheus — осознанный рост кардинальности... но лимит фиксируется явно"* — the number of internal clients is small and bounded (this is not a public multi-tenant SaaS), so unlike `octoconv_queue_depth` (which must stay unlabeled-by-client to keep KEDA's PromQL untouched, see Q4), the job-outcome/duration metrics are **not** read by KEDA and are safe to relabel.

**Operator visibility (Q4 of the milestone's target features, "кто и сколько")** reads `client_usage_daily` directly (cheap, pre-aggregated, no full-table `jobs` scan) rather than Prometheus — Prometheus retention/cardinality is not the right tool for "give me this client's usage for the last 30 days," and `client_usage_daily` is already the right shape for that query.

---

## Q3 — Multi-replica correctness

**The counter of record must be Postgres, using atomic conditional UPDATE, not Redis, not in-process state.** Rationale is fact #3 above: Redis here is contractually transient (asynq broker only); a quota that "must be globally correct" cannot be sourced from a store the architecture explicitly does not treat as durable, and the milestone explicitly forbids new storages — Postgres is the only store both durable and already-approved.

**Mechanism, concretely:**
- **Concurrency cap (tier 1 in Q1):** does **not** need a separate counter table at all — it is a live, derivable fact of `jobs.status`. A `SELECT count(*) FROM jobs WHERE client_id=$1 AND status IN ('queued','active')` against a new partial index (`CREATE INDEX jobs_client_inflight_idx ON jobs (client_id) WHERE status IN ('queued','active')`, mirroring the existing `jobs_inflight_idx` pattern in `0001_init.sql`) is correct across any number of API replicas because it reads the same Postgres rows every replica writes through the same guarded `Repo.transition` machinery. No new bookkeeping, no double-counting risk, because it's not a counter — it's a query over ground truth.
- **Daily volume/bytes cap (tier 2/3 in Q1):** the atomic `INSERT ... ON CONFLICT DO UPDATE ... WHERE (existing usage + delta) <= limit RETURNING` against `client_usage_daily` (Q2's table) is safe under N concurrent API replicas because Postgres's row-level lock semantics on the conflicting row serialize concurrent writers — this is standard "atomic reserve" pattern, no distributed lock needed, no Redis needed. This is the exact same "guarded, single-statement, correctness-first" idiom `Repo.transition`'s `SELECT ... FOR UPDATE` already uses elsewhere in this codebase, just via `ON CONFLICT` instead of an explicit row lock (both are valid Postgres idioms for the same problem; `ON CONFLICT DO UPDATE` is preferable here because it also *creates* the row on first use in one round trip, matching the "additive, zero-migration-for-existing-clients" requirement in `PROJECT.md`).

**Failure modes and how this shape handles them:**

| Failure mode | Cause | Outcome under the Postgres-atomic design |
|---|---|---|
| Double-count | Two API replicas process the same job's usage-record call concurrently | Cannot happen for the *reserve* (tier 3, at job-creation time) — it's one row, one atomic statement, Postgres serializes it. For the *worker-side* `RecordUsage` call (Q2) — a job is only ever processed by one worker due to asynq's `asynq.Unique` lock (already relied on elsewhere in this codebase, e.g. `ImageUniqueTTL`), so double-recording usage for the same job_id is already structurally prevented by machinery that exists today. |
| Lost count | API replica crashes between `s.storage.Upload` and `repo.Create` | Matches existing behavior: the job row simply never gets created (as today, with no usage side-effect either) — no partial usage is recorded because the reserve statement and `repo.Create` should be adjacent/co-transactional; a crash there is equivalent to today's "row stays in `queued`... reconciler recovers it" story just failing one step earlier, before any row exists at all. Recommend wrapping the tier-3 reserve and `repo.Create` in the **same** `pgx.BeginFunc` transaction so they commit-or-rollback together — this is a small, natural extension of `Repo.Create`'s existing single transaction (it already inserts `jobs`+`job_inputs`+`job_events` together), not a new pattern. |
| Quota bypass during a partition | A replica cannot reach Postgres | **Fail-closed**, not fail-open: if the tier-3 reserve query errors (Postgres unreachable), `handleCreateJob` must reject the request (mirrors the existing `handleHealth` posture — Postgres unreachable is already treated as `503`/degraded elsewhere in this service, and `s.repo.Create`'s own failure path already returns `500` rather than silently proceeding). A Postgres partition already means the API cannot durably record the job at all (Postgres-first double write requires Postgres), so quota fail-closed adds no new availability risk — the request was already going to fail at `repo.Create` moments later. |
| Race between tier-2 soft check and tier-3 reserve | Two concurrent requests from the same client both pass the cheap tier-2 read | Expected and fine — tier 2 is explicitly documented above as a soft optimization, not authority. Tier 3 is where correctness is actually enforced; one of the two racing requests will lose at tier 3 and abort (leaving its S3 object for TTL cleanup, per the preset-TOCTOU precedent). |

---

## Q4 — Fairness inside an engine class

Evaluated against this codebase's actual structure and the explicit "don't rewrite queue/KEDA architecture" constraint, and against fact #4/#5 above (asynq has no intra-queue fairness primitive; `octoconv_queue_depth` is not client-aware).

| Option | Where it would live | Intrusiveness | Touches KEDA queue-depth contract? |
|---|---|---|---|
| **A. Admission control at the API** (concurrency cap per client — literally Q1's tier 1) | `internal/api/handlers.go`, before `EnqueueXConvert` | **Low.** Reuses the exact same gate already being built for quota (Q1/Q3) — this is not new machinery, it's the same mechanism read for a fairness purpose. | **No.** Queue names, labels, and `AllConvertQueues()` are completely untouched — a client that's capped simply never gets a job enqueued, so `octoconv_queue_depth` semantics don't change at all. |
| **B. A dispatcher process between queue and worker** | Would be a new component sitting between Redis and `cmd/*-worker` | **High**, and arguably pointless: asynq's `Processor` dequeues one task at a time via its internal loop with no "peek ahead and pick" API exposed to callers — building this would mean either forking asynq's dequeue loop (a de facto rewrite of the "asynq architecture", explicitly out of scope) or building it as option C below (per-queue weighting) which asynq already supports natively. | **Yes, unavoidably** — any true reordering-before-dequeue design needs the queue itself to be partitioned (see C), since asynq gives no hook to reorder within one queue. |
| **C. Per-client (bounded-cardinality) sub-queues** | `internal/queue/queue.go` (new queue-name scheme, e.g. `image-0`..`image-N` hashed by `client_id`), wired into `asynq.Config.Queues` weights in `cmd/*-worker/main.go` | **High.** This is asynq's actual supported primitive for "fairness between named groups" (weighted multi-queue config) — so if true fairness beyond admission caps is ever needed, this is the mechanism to reach for, not a bespoke dispatcher (B). But it requires: new queue-name scheme in `internal/queue/queue.go`; `AllConvertQueues()` (currently the single source of truth for the metrics collector arg list, with its own completeness guard test `TestAllConvertQueuesCoversEveryEngine`) must enumerate every shard, not every engine; `internal/metrics/queue_collector.go`'s `Describe`/`Collect` must aggregate across shards; **every one of the 7 `deploy/chart/octoconv/templates/scaledobject-*.yaml` PromQL strings** (`sum(octoconv_queue_depth{queue="image", ...})`) must change from an exact-match label filter to a shard-aggregating query (e.g. `sum(octoconv_queue_depth{queue=~"image(-[0-9]+)?", ...})`); and `internal/reconciler/reconciler.go`'s engine→queue routing switch (currently one fixed queue name per `case convert.EngineX`) must compute the same shard a stranded job's original enqueue would have used, or a recovered job could land on a different shard's weight allocation than intended. | **YES — explicitly flag this.** This is the option the quality gate's KEDA warning is about. Every ScaledObject's trigger query is a hard-coded exact-match on `queue="<engine>"`; introducing shard suffixes breaks scale-from-zero unless all 7 PromQL strings, the collector, `AllConvertQueues()`, and the reconciler's routing are updated together, in the same change. |
| **D. Worker-side selection** (worker pulls ahead / reorders in-process) | Inside `cmd/*-worker` | **High**, and not really compatible with asynq's model — `asynq.ServeMux`/`Processor` invoke one handler per dequeued task with zero visibility into what else is queued; there is no supported "peek N, choose 1" hook. Building this means replacing asynq's own dequeue loop, which is the queue-architecture rewrite the milestone explicitly rules out. | Not directly (queue names unchanged) but achieving it requires bypassing asynq's own dispatch, which is a different and larger kind of architecture change than the milestone scopes. |

**Recommendation:** start with **A** (admission control / concurrency caps) — it directly reuses Q1/Q3's mechanism, costs nothing extra to build, and is fully sufficient for the stated problem shape ("Сейчас внутри очереди чистый FIFO при `maxReplicaCount: 2`" — small worker pools, a handful of known internal clients, not a public many-tenant system where true weighted-fair-queuing matters). **Defer C** (sub-queue sharding) as a distinct, separately-scoped, KEDA-contract-touching piece of work, only if operator visibility (built in this milestone) later shows admission caps aren't enough in practice. **Reject B and D** outright — B duplicates what C already gives you natively through asynq, and D requires leaving asynq's supported feature surface entirely.

---

## Q5 — Suggested build order

Ordered by actual data/mechanism dependency, not by feature-list order in `PROJECT.md`:

1. **Attribution: `client_id` Prometheus label + `client_usage_daily` migration + wiring into the existing `MarkDone`/`MarkFailed` → `metrics.RecordJobOutcome` choke point (Q2).**
   Must come first because everything else *reads* this data: you cannot enforce a quota against numbers you aren't tracking, and you cannot show an operator "who and how much" without the aggregate existing and being populated for at least a few real jobs first. This is purely additive (new table, new label argument, one new call site per `Handle*Convert`) — zero behavior change for existing clients, matching this project's own stated additive-first discipline (cf. v1.9's "аддитивная схема... zero behavior change").

2. **Quota enforcement (Q1/Q3): the three-tier gate in `handleCreateJob` + the `jobs_client_inflight_idx` partial index + the atomic `client_usage_daily` reserve.**
   Depends on #1's table existing (tier 2/3 read/write it) and on `jobs.status` already being ground truth for concurrency (tier 1 needs no new table, just a new index). This step **also delivers Q4's option A (admission-control fairness)** for free — it is the same mechanism read for a different purpose, so there is no separate "fairness phase" needed for the baseline (non-sub-queue) fairness requirement.

3. **Operator visibility endpoint/dashboard.**
   Depends only on #1 (reads `client_usage_daily`); does not depend on #2 being complete, and could ship in parallel with #2's later plans once the aggregate table exists and has real data — but sequenced after #1 so it has something real to show, and after #2 has started so the "quota vs. actual usage" comparison the operator surface will want to show is meaningful (an operator view of usage with no quotas configured yet is a weaker deliverable, even if technically buildable earlier).

4. **SEED-011 (`minReplicaCount` per class, chart-only).**
   Fully independent of 1–3 — zero data or code coupling, pure Helm values/`ScaledObject` change. Can land at any point; recommend doing it early as a low-risk, high-confidence "quick win" phase, or as a hygiene tail at the end — either is defensible, since it blocks nothing and is blocked by nothing.

5. **(Deferred, only if evidence from #3 shows it's needed) Sub-queue fairness (Q4 option C).**
   Must come last, after #1–#3 are live and generating real operational signal, because: (a) it is the only piece that touches the KEDA queue-depth contract across 7 chart files + the metrics collector + `AllConvertQueues()` + the reconciler's routing switch, all of which must change together; (b) it is meaningfully higher-risk and higher-effort than 1–4 combined; (c) #2 already covers the common "one client floods the class" failure mode for this system's actual scale (small internal client count, `maxReplicaCount: 2`-sized worker pools). Building this speculatively before evidence from #3 shows admission caps are insufficient would repeat the exact pattern this project's own `PROJECT.md` explicitly rejected once already for SEED-010 ("экономит... стоимость, но ослабляет свойство безопасности" / cost-saving speculative infrastructure rejected in favor of the option that doesn't compromise an existing guarantee) — here, the guarantee at risk is the KEDA scale-from-zero contract, not the compile-time allowlist, but the reasoning shape is the same: don't take on contract risk before evidence justifies it.

---

## New vs. Modified Components (summary)

| Component | New or Modified | What changes |
|---|---|---|
| `internal/db/migrations/00XX_client_usage.sql` | **New** | `client_usage_daily` table + `jobs_client_inflight_idx` partial index |
| `internal/jobs/repo.go` | **Modified** | New `RecordUsage`/reserve methods; possibly extend `Create`'s existing transaction to include the tier-3 reserve |
| `internal/api/handlers.go` (`handleCreateJob`) | **Modified** | Three new inline gates (Q1) at three specific existing slots — no new middleware |
| `internal/worker/worker.go` (all 7 `Handle*Convert`) | **Modified** | One new call alongside the existing `metrics.RecordJobOutcome` call in each handler |
| `internal/metrics/metrics.go` | **Modified** | `client_id` label added to `octoconv_job_outcomes_total`/`octoconv_job_duration_seconds` |
| `internal/metrics/queue_collector.go` | **Untouched** for the recommended build order (only touched if/when sub-queue sharding, item 5, is later pursued) |
| `deploy/chart/octoconv/templates/scaledobject-*.yaml` (×7) | **Untouched** for the recommended build order (KEDA contract preserved) — only touched by deferred item 5 |
| Operator visibility endpoint (`internal/api` new route, or a small new package) | **New** | Reads `client_usage_daily`, exposes "who and how much" |
| `deploy/chart/octoconv` `minReplicaCount` values (SEED-011) | **Modified** | Chart-only, independent of everything else |

---

## Sources

All findings sourced directly from repository files (no external ecosystem research applicable — this is an internal-integration architecture question):
- `internal/api/handlers.go` — gate ordering, TOCTOU precedent (preset re-check)
- `internal/jobs/repo.go` — guarded-transition pattern, existing columns (`started_at`/`finished_at`), `jobs_inflight_idx` precedent
- `internal/queue/queue.go` — `AllConvertQueues()`, per-engine queue/task/retry/unique-TTL wiring
- `internal/metrics/metrics.go`, `internal/metrics/queue_collector.go` — current label shape of `octoconv_job_outcomes_total`/`octoconv_queue_depth`
- `internal/ratelimit/ratelimit.go` — in-process vs. shared-counter reasoning already documented by this codebase for a related (not identical) problem
- `internal/worker/worker.go` — the `MarkActive`/`process`/`MarkDone`/`MarkFailed`/`RecordJobOutcome` choke point, repeated identically across all 7 engine handlers
- `internal/reconciler/reconciler.go` — engine→queue routing switch (relevant to Q4's sub-queue intrusiveness assessment)
- `internal/db/migrations/0001_init.sql` — `jobs`/`job_inputs`/`job_outputs` schema, confirming per-job usage data already exists
- `deploy/chart/octoconv/templates/scaledobject-*.yaml` — exact PromQL strings that constitute the KEDA contract
- `.planning/PROJECT.md` — milestone goal, locked decisions, "no new storages," client_id-as-Prometheus-label pre-acceptance, SEED-010 rejection precedent
