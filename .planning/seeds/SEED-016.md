---
id: SEED-016
status: dormant
planted: 2026-08-02
planted_during: post-v1.11 (asked while v1.12 "Consumer Accounting & Fairness" was being planned)
trigger_when: when usage data (v1.12 OPSV-01) shows urgent jobs actually suffering behind queued bulk work, OR when a consumer asks to mark specific conversions as urgent
scope: Medium
---

# SEED-016: Client-assigned job priority (queue tiers)

A client marks an individual conversion as urgent, and the worker picks it up
before jobs that were queued earlier. Concretely: video #1 is queued `low`,
video #2 arrives `high` — the worker takes #2 first.

**Operator's decisions, recorded at planting time (2026-08-02):**

1. **Priority applies only while jobs are QUEUED.** A job already in flight is
   left alone — no preemption, no cancel-and-requeue. This is a scope decision,
   not a limitation to work around.
2. **The client assigns the priority**, not the operator. It is a property of
   the request ("this conversion is urgent"), not a per-client tier the operator
   hands out.

## Why This Matters

Today every engine class has exactly one queue and asynq serves it strict FIFO.
A large bulk upload therefore delays anything submitted after it, no matter how
urgent, until the backlog drains. With `maxReplicaCount: 2` and 1–2 concurrent
jobs per pod, the queue in front of an urgent job can be long.

## The mechanism (verified, not assumed)

asynq v0.26.0 has **no per-task priority inside a queue**. Verified by reading
the vendored source during v1.12 research: `Config.Queues map[string]int` weights
*named queues*, and `StrictPriority` switches those weights from weighted
round-robin to strict ordering. `Group`/`GroupAggregator` batches tasks into one
handler call — the opposite of what is wanted here. Inside a single queue,
ordering is FIFO with no priority field to set.

So priority must be expressed as **separate queues per tier**, and the switch is
already in the code:

```go
// cmd/av-worker/main.go:158 — today
Queues: map[string]int{queue.QueueAV: 1},
// with tiers
Queues: map[string]int{queue.QueueAVHigh: 3, queue.QueueAV: 1},
```

**No new worker binary is needed** — one worker process consumes both tiers.

Prefer the default weighted round-robin over `StrictPriority: true`. Strict means
a steady stream of `high` starves `low` indefinitely; weighted guarantees the low
tier a share. "Low priority" must not come to mean "never".

## What ships (Medium)

1. **Queue constants + enqueue routing** — a tier suffix on the engine queue name;
   `EnqueueXConvert` selects the queue by the job's tier.
2. **Persist the tier on the job row.** Load-bearing: the reconciler re-enqueues
   stranded jobs by `jobs.engine`. Without a stored tier, recovery silently
   drops an urgent job back into the default queue.
3. **Opts surface** — a closed enum (`priority: high|normal`), parsed through the
   existing strict-parse opts discipline, never a free-form string.
4. **Worker `Queues` map** — weights per tier, per engine class.
5. **Queue-depth collector + KEDA PromQL** — see the invariant below.

## Nuances / invariants to respect

- **Do NOT double the ScaledObjects.** Two ScaledObjects targeting one Deployment
  means two HPAs fighting over one scale target. The correct shape is a single
  ScaledObject per engine class whose PromQL sums the tiers
  (`queue=~"av|av_high"`), with the prioritization living inside the worker's
  weights. Autoscaling then keeps working untouched. (This corrects a pessimistic
  note in the v1.12 STACK research, which assumed tiers would double the
  ScaledObject count.)
- **`AllConvertQueues()` gains entries**, so `octoconv_queue_depth` gains a series
  per tier. That is fine — but the PromQL in every `scaledobject-*.yaml` selects
  `queue="<class>"` by exact match today and would silently stop seeing the high
  tier's backlog. Scale-from-zero would then ignore urgent work. This is the one
  change that can break autoscaling; it must be verified, not assumed.
- **The reconciler must preserve the tier** (see item 2 above).
- **No preemption** — per the operator's decision. Worth stating in the API docs
  so callers do not expect an urgent job to displace a running one. Under
  `AV_ENGINE_TIMEOUT=753s` an in-flight video can hold a slot for minutes, and a
  `high` job submitted behind it still waits.

## The open question this design must answer

If the client assigns priority, what stops every client from marking everything
`high`? Once all jobs are `high`, the tiers collapse back into one queue and the
feature is inert.

This is not an argument against the decision — it is the design question the
implementing phase has to answer. Plausible answers, in rough order of
intrusiveness:

- **Default to `normal`, make `high` opt-in per request.** Costs nothing, relies
  on callers being reasonable — plausible for internal services, worthless if
  one integration sets it blanket.
- **Cap the high tier per client** — an operator-set allowance on how many
  concurrent `high` jobs a client may hold. This composes directly with v1.12's
  per-client admission control (FAIR-01), which is the same mechanism.
- **Make `high` cost budget** — a high-priority job consumes more of the client's
  quota. Requires v1.12's accounting to exist first.

The second and third both depend on v1.12 shipping, which is the main reason this
seed is dormant rather than scheduled.

## Relation to v1.12 and to SEED-011

- **v1.12 `FAIR-V2-01`** (per-client sub-queues) is a *different* idea that was
  rejected for breaking the KEDA contract by renaming queues per client. This seed
  adds a bounded number of tier queues per engine class (2, not N-per-client), and
  keeps one ScaledObject per class — which is why it is tractable where
  FAIR-V2-01 is not.
- **v1.12 `FAIR-V2-02`** is priority *between clients* as operator policy. This
  seed is priority *per job* as a client-supplied request property. Same queue
  mechanism, different contract and different failure modes.
- **SEED-011** (per-class warm floor) composes: a warm floor on a class with a
  high tier removes cold-start from exactly the jobs that were marked urgent.

## Breadcrumbs

- Verified asynq semantics: `.planning/research/STACK.md` (v1.12), section on
  asynq capabilities — read from `$GOMODCACHE/github.com/hibiken/asynq@v0.26.0/server.go:152-159`.
- KEDA-contract risk in detail: `.planning/research/PITFALLS.md` (v1.12), the
  fairness-vs-KEDA pitfall.
- Current worker queue config: `cmd/*-worker/main.go`, the `Queues:` line in each.
- Reconciler engine→queue routing: `internal/reconciler/reconciler.go`.
