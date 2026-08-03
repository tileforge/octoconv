# Phase 52: Quota Enforcement & Intra-Class Fairness - Context

**Gathered:** 2026-08-03
**Status:** Ready for planning

<domain>
## Phase Boundary

An operator can cap what a client may consume (concurrent jobs per engine class,
daily bytes, daily job count), enforcement is fail-closed before the S3 write and
before enqueue, it is rolled out observe-only first and enabled per client, and
the same admission mechanism gives a structural guarantee that one client cannot
starve others inside an engine class — all without touching queue topology or the
KEDA contract.

**Not in this phase:** per-client sub-queues or a weighted scheduler
(FAIR-V2-01), priority tiers between clients (FAIR-V2-02), an operator REST
surface for limits, wall-time as an *enforced* cap, and a general sweep of
structured error codes across the API.

</domain>

<decisions>
## Implementation Decisions

### Limit storage and granularity (QUOT-01)

- **D-01:** A single table **`client_limits`**, primary key **`(client_id, engine)`**
  with `engine NOT NULL`. No client-wide level, no inheritance resolution, and no
  sentinel row (`engine = '*'` is explicitly rejected — this project already
  treats sentinel values as a defect class, which is why QUOT-01 mandates `NULL`
  for "unlimited"). The key deliberately mirrors `client_usage_daily`'s
  `(client_id, engine, day, status)`, so the daily check is a primary-key read
  joined one-to-one rather than an aggregation.
  - Accepted cost, recorded so it is not rediscovered as a surprise: expressing
    "1 GB/day for this client across everything" requires seven rows. With a
    handful of internal consumers this is cheaper than an inheritance rule that
    must be applied correctly at every read site.
  - A client-wide tier can be added later without redesigning this table.

- **D-02:** **Absence means unlimited, at two levels:** a `NULL` column is
  unlimited, and no row at all for a `(client_id, engine)` pair is unlimited.
  Both must be true for the ROADMAP's SC1 ("no existing client's behavior changes
  until an operator explicitly sets one") to hold on a live database where no
  `client_limits` rows exist yet.

- **D-03:** Enforced units in this phase: **concurrent jobs (per engine)**,
  **daily bytes**, **daily job count**. Wall-time is collected and surfaced but
  **not enforced** — it is only known after a job finishes, so a client would
  blow through a wall-time cap mid-job and the phase would have to invent a
  policy for what to do about the running job. Observe-only (D-11) is what
  produces the data to decide whether a wall-time cap is needed at all.

- **D-04:** Limits are set through **`cmd/manage-clients`**, the existing
  operator CLI that already creates clients and API keys. No new REST surface in
  this phase: it adds an authorization question to a phase whose subject is
  enforcement, and Phase 53 (operator usage visibility) is the natural place for
  a read/write HTTP surface if one is wanted.

- **D-05:** The daily window is the **UTC calendar day**, matching
  `client_usage_daily.day`, which is already written as
  `(now() AT TIME ZONE 'utc')::date`. A rolling 24-hour window was rejected: the
  daily rollup cannot serve it, so it would force aggregation over `jobs` and
  therefore lose accuracy the moment the retention purge removes rows.

### Concurrency slot semantics (FAIR-01)

- **D-06:** A slot is occupied by **`queued` + `active`**, not `active` alone.
  This is the decision that makes FAIR-01 true in spirit rather than merely on
  paper: asynq is strictly FIFO within a queue and has no intra-queue fairness
  (verified against the vendored source — `Config.Queues`/`StrictPriority` weight
  *named queues*, and `Group`/`GroupAggregator` is batching). If only `active`
  counted, a client could enqueue 10 000 jobs, sit at the head of the FIFO, and
  occupy every slot as it freed — satisfying an `active`-only cap while starving
  everyone. The predicate also matches the existing partial index
  `jobs_inflight_idx ON jobs (created_at) WHERE status IN ('queued', 'active')`.

- **D-07:** Occupancy is a **`COUNT` over `jobs` inside the same transaction as
  the insert**, serialized by a Postgres advisory lock keyed on
  `(client_id, engine)`. `jobs.status` is already ground truth; count-and-insert
  in one transaction is what satisfies QUOT-05 across API replicas, where a bare
  `SELECT count(*)` followed by an insert lets two replicas both observe "below
  cap" and both admit.

- **D-08:** **There is no slot to release, and no release code to write.** Slot
  occupancy is derived from `jobs.status`, so every terminal transition —
  including `failed`, `canceled` and the reconciler's exhaustion path — frees the
  slot as a consequence of the status change that already happens. A dedicated
  counter column was rejected specifically because it creates a second source of
  truth that can drift from `jobs`, and the drift failure modes are permanent:
  leaked slots that never free, or negative counts.
  - Consequence worth stating for the planner: **this phase has no worker-side
    component at all.** Nothing in `internal/worker/worker.go` changes.

### Rejection shape (QUOT-02, QUOT-03)

- **D-09:** There is exactly **one enforcement point** — the REST job-creation
  handler. Both MCP servers proxy over HTTP through
  `internal/mcpserver/client.go` and never touch the database, so QUOT-02's
  "every creation path" is satisfied by one check plus a guarantee that the
  rejection survives the proxy hop. The plan must prove this by exercising a
  real MCP path, not by asserting that proxying makes it true.

- **D-10:** A **`code` field is added additively** to the JSON error body
  alongside the existing `error` text, and populated **only for the two 429
  responses**: `quota_exceeded` and `rate_limited`. Every other error body is
  left byte-for-byte unchanged.
  - Rationale for the narrow scope: QUOT-03 requires a machine-readable
    discriminator between those two specifically, and `rate_limited` does not
    exist today — `internal/ratelimit`'s 429 body is the prose string
    `{"error": "rate limit exceeded"}`. A full sweep of API error codes is real
    work with real value, and it is already recorded in **SEED-020** alongside
    the JobRunner consolidation, where retry classification and terminal error
    handling get centralised. Doing it here would be scope creep; doing it
    quota-only with a bespoke field shape would have to be undone there.
  - The addition is backward compatible: existing consumers read `error` and
    ignore unknown fields.

- **D-11:** **`Retry-After` is derived from the limit type that fired**, not a
  single constant. A daily-volume rejection returns the seconds remaining until
  UTC midnight, which is exact. A concurrency rejection returns a short constant,
  because a slot frees unpredictably. A single constant was rejected: on a daily
  cap it would be wrong by hours, and the client would return immediately and get
  the same rejection.

- **D-12:** `internal/mcpserver/client.go`'s `apiError` must **carry the code and
  the retry hint into the MCP tool error**. Today it reads only the `error` field
  (`client.go:436`) and drops both the status code and the `Retry-After` header,
  so an MCP agent cannot tell "wait 60 seconds" from "you are not allowed" and
  will retry into a wall. Its existing behaviour of redacting the API key from
  the surfaced text must be preserved.

### Rollout (QUOT-04)

- **D-13:** Two independent switches: an **`enforcement_mode` per client** (the
  ROADMAP criterion says "before an operator enables real enforcement **for any
  specific client**", which a global flag cannot express) **plus a global env
  kill-switch** that forces observe-only service-wide. The global switch is cheap
  insurance: during an incident, reverting enforcement should not require editing
  database rows.

- **D-14:** The observe-only signal is a **`would-be-rejected` counter labelled
  `{client_id, engine, reason}`**, where `reason` distinguishes concurrency from
  daily bytes from daily job count — an operator needs to know *which* limit
  would have fired, not just that one would have.
  - `client_id` **must** go through the same bounded allocator introduced in
    Phase 51 (`clientLabelAllocator`, ceiling 200, overflow collapsing to the
    literal `overflow`). A new counter carrying raw client ids would reintroduce
    exactly the cardinality problem ACCT-04 closed.
  - Note carried forward from the PR #4 review: that allocator bounds what **one
    process emits over one lifetime**, not global TSDB cardinality. That is
    accepted and documented at the call site; do not re-litigate it here.

### Accepted imprecision (SC2b)

- **D-15:** Daily byte and job-count checks read `client_usage_daily`, which is
  written at the **terminal** transition. Jobs that are admitted but not yet
  finished are therefore invisible to the check, so a burst can overshoot the
  daily cap. **This overshoot is accepted, and it is bounded by the concurrency
  cap: at most `K × MAX_INPUT_BYTES` for that engine.**
  - Full reserve/consume accounting was rejected for the reason recorded during
    the architecture review: it reintroduces a second source of truth plus
    reservation-leak failure modes, on a path where the concurrency cap already
    bounds the damage to a computable, small number.
  - This is a decision, not an oversight. The plan must **state the bound
    explicitly and cover it with a test** that admits a burst up to the
    concurrency cap and asserts the overshoot stays within `K × MAX_INPUT_BYTES`
    — so the property is pinned rather than assumed.
  - This also resolves SC2b's "reservations and completed usage are
    distinguishable, counted once not twice": with no reservation written, there
    is nothing to double-count. `client_usage_daily` keeps its exactly-once
    write inside the terminal transition (Phase 51 D-05).

  - **Amendment (2026-08-03, plan 52-06, source: `52-VERIFICATION.md` truth #5 /
    `52-REVIEW.md` CR-01):** D-15's bound above was **false** whenever
    `max_concurrent` is `NULL` — a configuration the schema (`client_limits`'s
    columns are independently nullable), `internal/clients/limits.go`'s
    cross-field validation, and `cmd/manage-clients set-limit <id> <engine> -
    <bytes> <jobs>` all accept and none of them reject. With no concurrency cap
    at all, nothing bounded how much admitted-but-unfinished work could pile up,
    and the verifier reproduced this live: `max_daily_jobs = 2`,
    `max_concurrent = nil`, 50 sequential `Repo.Create` calls, 50/50 admitted.
    D-15's original text above is left unedited so the record shows what was
    decided and why it changed, not a silently rewritten history.
    - **Superseding rule:** the daily check now charges today's committed
      `client_usage_daily` rollup **plus** currently in-flight (`queued` +
      `active`) admitted work for the same `(client_id, engine)`, evaluated
      under the same `pg_advisory_xact_lock`-guarded transaction `admitLocked`
      already holds — the in-flight read is serialized against every other
      admission attempt for that exact pair, so the SC2(a) property is not
      weakened by the new term.
    - **Date-agnostic in-flight rule:** the in-flight term carries no
      `created_at`/date predicate — a job that is `queued` or `active` right
      now consumes capacity right now, regardless of when it was created.
      Conservative consequence: a job in flight across a UTC midnight is
      charged against each day it is in flight and again against the day it
      terminates (via the rollup) — a deliberately fail-closed double charge at
      the day boundary, not a free window.
    - **Rejected alternative:** forbidding daily-limits-without-a-concurrency-cap
      at `SetLimit` (i.e. requiring `max_concurrent` whenever a daily limit is
      set). Rejected because the fix above makes that configuration safe on its
      own; forbidding it would remove operator expressiveness for no remaining
      benefit.
    - Residual unchanged from D-15's original text: `PreAdmit`'s unlocked
      pre-upload gate can still let a concurrent burst upload before
      `admitLocked` rejects it — wasted uploads, never over-admission — bounded
      and measured by plan 52-05's
      `TestQuotaAdmission/ConcurrentBurstBoundedUploadWaste`.

### Claude's Discretion

Column types and index choices on `client_limits`; the migration ordinal; the
advisory-lock key derivation for `(client_id, engine)` (it must not collide with
the reconciler's existing key — see `internal/reconciler/reconciler.go:27`); the
exact metric and env-var names; the concurrency `Retry-After` constant; where the
admission check lives structurally; the `manage-clients` subcommand and flag
shape — all planner/executor's call, provided that: limits key on
`(client_id, engine)` with NULL/absent meaning unlimited, the slot predicate is
`queued + active` counted in-transaction under an advisory lock, no slot-release
code is written, `code` is populated only on the two 429s, `Retry-After` varies
by limit type, enforcement is per-client with a global kill-switch, the
would-be-rejected counter goes through the Phase 51 allocator, the daily
overshoot bound is tested, and `octoconv_queue_depth` plus all seven
`ScaledObject` PromQL strings are untouched.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements and prior decisions
- `.planning/REQUIREMENTS.md` — QUOT-01..05, FAIR-01/02, and the evidence gates
  on FAIR-V2-01/02 (why per-client sub-queues are off the table).
- `.planning/ROADMAP.md` §"Phase 52" — success criteria, including SC2's two
  load-bearing properties (serialized check-and-admit, reservation before the S3
  write) and SC2b, both added after the PR #3 review.
- `.planning/phases/51-attribution-accounting-retention/51-CONTEXT.md` — D-01
  (`client_usage_daily` shape and why it exists), D-05 (the rollup write lives
  inside the transition transaction), D-07/D-08/D-09/D-10 (the metric cardinality
  ceiling and the overflow collapse this phase must reuse).
- `.planning/research/SUMMARY.md` §"Corrections after PR #3 review" — authoritative.

### Deferred work this phase deliberately does not do
- `.planning/seeds/SEED-020.md` — JobRunner consolidation. Owns the general
  structured-error-code sweep (D-10 above populates `code` for two responses
  only) and records why quota work does **not** pay the seven-handler tax.
- `.planning/seeds/SEED-016.md` — client-assigned priority tiers (FAIR-V2-02).
- `.planning/seeds/SEED-010.md` — dynamic capability registry. Relevant because
  quota rows key on `engine`; if engine classes ever become catalog data, a
  capability with no quota row must resolve to an explicit default rather than
  accidental-unlimited.

### Code the plan must read before designing
- `internal/jobs/repo.go` — `Repo.transition` and its `apply func(ctx, tx) error`
  closure; `Repo.Create` and the insert path admission must join.
- `internal/api/handlers.go:691` — the single job-creation path and its seven-way
  enqueue switch; `writeError` at :928 is the error-body shape `code` extends.
- `internal/mcpserver/client.go:195-215, 436-455` — the proxy hop, `apiError`,
  and the API-key redaction that must survive D-12.
- `internal/ratelimit/ratelimit.go:96-115` — the existing 429 handler, its
  `Retry-After`, and its locally duplicated error-body shape; it gains
  `code: "rate_limited"`.
- `internal/reconciler/reconciler.go:17-30` — the advisory-lock convention and
  the existing key that D-07's key must not collide with.
- `internal/metrics/metrics.go` — `clientLabelAllocator`, `RecordClientJobOutcome`,
  and the two label-pinning tests that guard the KEDA contract.
- `internal/db/migrations/0001_init.sql:10-14, 67-71` — the three-column `clients`
  table and the existing indexes on `jobs`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`client_usage_daily`** (migration 0011) already records per-client/engine/day
  bytes-in, bytes-out, job count and wall-seconds, written exactly-once inside the
  terminal transition. The daily checks read it directly; no new accounting is needed.
- **`Repo.transition`'s `apply` closure** is the established seam for anything that
  must be atomic with a status change. Admission is not a transition, but the same
  discipline applies to the count-and-insert.
- **Advisory locks** are already used by the reconciler with a documented
  never-collide convention.
- **`clientLabelAllocator`** in `internal/metrics` gives a bounded `client_id`
  label with an `overflow` collapse — the new counter reuses it rather than
  inventing a second policy.
- **`cmd/manage-clients`** already owns operator-side client mutation.
- **`jobs_inflight_idx`** is a partial index over exactly the `queued`/`active`
  predicate D-06 selects on.

### Established Patterns
- **`NULL` means absent, never a sentinel number** — QUOT-01 states this as a
  requirement because the project has lost data to drifting sentinels twice.
- **Fail-closed at the edge** — validation happens before the storage write
  (`internal/api/handlers.go:67` documents this ordering deliberately).
- **Env-derived config with a code default**, `firstField`-tolerant of trailing
  comments. Note the trap found in the PR #4 review: an empty env value falls back
  to the code default, so "empty disables it" is false — a kill-switch must have an
  explicit off value.
- **Measured, not copied** — any new constant that bounds work needs a stated
  derivation.

### Integration Points
- `internal/api/handlers.go` job-creation handler — the single admission point,
  ahead of the S3 write and the enqueue.
- `internal/ratelimit` — gains `code: "rate_limited"` on its existing 429.
- `internal/mcpserver/client.go` — carries code and retry hint through the proxy.
- A new migration for `client_limits` plus the `enforcement_mode` column.
- `internal/metrics` — the would-be-rejected counter, reusing the allocator.

### Explicitly NOT an integration point
- `internal/worker/worker.go` — unchanged. D-08 removes any worker-side work.
- `deploy/chart/octoconv/templates/scaledobject-*.yaml` and the queue-depth
  collector — untouched, and FAIR-02 requires this be proven, not assumed.

</code_context>

<specifics>
## Specific Ideas

- The `429`-never-`403` rule is not stylistic: a quota is a consumption limit,
  not an authorization boundary, and conflating them makes clients treat a
  temporary condition as permanent.
- FAIR-01's guarantee should be stated in the concrete form the ROADMAP uses —
  at cap `K` and `maxReplicaCount` `M`, one client occupies at most `K` of `M`
  slots — and demonstrated, since "structural guarantee" is otherwise unfalsifiable.
- The overshoot bound in D-15 should appear in a code comment with its derivation
  (`K × MAX_INPUT_BYTES`), not only in this document.

</specifics>

<deferred>
## Deferred Ideas

- **Operator REST surface for limits** — deferred to Phase 53 (operator usage
  visibility), which already needs an HTTP read surface; adding write there keeps
  the authorization question in one phase.
- **Wall-time as an enforced cap** — deferred pending observe-only data (D-03).
- **General structured error codes across all API responses** — SEED-020.
- **Client-wide (non-per-engine) limit tier** — deferred; D-01 leaves room for it.
- **Rolling 24-hour quota window** — rejected for this phase (D-05); would need a
  data source that survives retention.

</deferred>

---

*Phase: 52-quota-enforcement-intra-class-fairness*
*Context gathered: 2026-08-03*
