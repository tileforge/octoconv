# Phase 52: Quota Enforcement & Intra-Class Fairness - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-03
**Phase:** 52-quota-enforcement-intra-class-fairness
**Areas discussed:** Limit shape and granularity, Concurrency slot semantics, Rejection shape, Rollout

---

## Two findings from the codebase scout that reframed the phase

Recorded here because they changed what the questions were, not just their answers.

1. **There is exactly one job-creation path.** Both MCP servers proxy over HTTP
   through `internal/mcpserver/client.go` and never touch the database. QUOT-02's
   "fail-closed on every creation path" is therefore one enforcement point plus a
   requirement that the rejection survives the proxy hop intelligibly — not three
   checks.

2. **The rejection cannot currently survive that hop.** `apiError`
   (`client.go:436`) flattens a non-2xx response to a string, reading only the
   `error` field and discarding the status code and `Retry-After`. And the
   `rate_limited` code that QUOT-03 asks `quota_exceeded` to be distinguishable
   from **does not exist** — `internal/ratelimit` returns the prose
   `{"error": "rate limit exceeded"}`. An MCP agent today could not tell "wait 60
   seconds" from "you are not allowed".

---

## Limit shape and granularity

| Option | Description | Selected |
|--------|-------------|----------|
| One table, key `(client_id, engine)` | `client_limits` with `engine NOT NULL` — all limits per-engine, no inheritance levels, no `engine='*'` sentinel. Mirrors `client_usage_daily`'s key so the check is a one-to-one PK read. Cost: a client-wide daily cap takes seven rows. | ✓ |
| Two levels with inheritance | `client_limits` (PK `client_id`) for client-wide plus `client_engine_limits` for per-engine, the latter overriding. Friendlier to operators, but adds a resolution rule that must be applied at every read site. | |
| Columns on `clients` | Limits as nullable columns directly on `clients`. Fewest entities, but the per-engine concurrency cap FAIR-01 requires cannot be expressed without seven columns or a jsonb blob. | |

**User's choice:** One table, key `(client_id, engine)`
**Notes:** The sentinel-row variant (`engine = '*'`) was ruled out on the same
grounds QUOT-01 already states for numeric sentinels — this project has lost data
to drifting sentinel values twice.

| Option | Description | Selected |
|--------|-------------|----------|
| Concurrency + daily bytes and jobs | Concurrency delivers FAIR-01; bytes and job count read exactly from the existing rollup. Wall-time collected and shown but not enforced — observe-only produces the data to decide later. | ✓ |
| Add wall-time | A daily machine-time cap too. More honest about "who ate the workers", but known only after the fact: a client breaches it mid-job and the phase must invent a policy for the running job. | |
| Concurrency only | Anti-starvation alone. Narrowest phase, but QUOT-01's "consumption limits" would be only partly satisfied. | |

**User's choice:** Concurrency + daily bytes and jobs

| Option | Description | Selected |
|--------|-------------|----------|
| CLI `cmd/manage-clients` | Extend the existing operator tool that already creates clients and keys. No new API surface, no authorization question. Cost: changing limits needs host/pod access. | ✓ |
| REST like system-presets | Operator endpoint gated by `OPERATOR_CLIENT_IDS`. Convenient and sets up Phase 53, but widens the API surface in a phase whose subject is enforcement. | |
| Both | CLI and REST in this phase. Larger scope and two write paths to keep consistent. | |

**User's choice:** CLI `cmd/manage-clients`

| Option | Description | Selected |
|--------|-------------|----------|
| UTC calendar day | Matches `client_usage_daily.day`, already written as `(now() AT TIME ZONE 'utc')::date`, making the check a PK read. Cost: the quota resets abruptly at UTC midnight. | ✓ |
| Rolling 24 hours | Fairer, no midnight cliff, but the daily rollup cannot serve it — would force aggregation over `jobs` and lose accuracy once retention purges rows. | |

**User's choice:** UTC calendar day

---

## Concurrency slot semantics

| Option | Description | Selected |
|--------|-------------|----------|
| `queued` + `active` | A slot is taken from admission. Only this makes the cap actually prevent starvation: asynq is strictly FIFO within a queue, so a client with 10 000 queued jobs sits at the head and takes every slot as it frees. Matches `jobs_inflight_idx`'s predicate. | ✓ |
| `active` only | Caps real CPU consumption. But with FIFO ordering the noisy client still queues ahead of everyone and occupies slots as they free — FAIR-01 satisfied literally, not in spirit. | |

**User's choice:** `queued` + `active`

| Option | Description | Selected |
|--------|-------------|----------|
| `COUNT` over `jobs` in the same transaction | `jobs.status` is already ground truth. Count and insert in one transaction under an advisory lock on `(client, engine)` — nothing can diverge, and no slot-release code needs to exist at all. | ✓ |
| A dedicated counter column | Increment on admit, decrement on terminal transition. Cheaper to read, but creates a second source of truth that can drift from `jobs` — and both drift modes are permanent: leaked slots that never free, or negative counts. | |

**User's choice:** `COUNT` over `jobs` in the same transaction
**Notes:** This choice has a consequence worth recording — **the phase has no
worker-side component at all.** Slot release falls out of the status transition,
so nothing in `internal/worker/worker.go` changes. That retroactively confirms
the correction made during the architecture review: quota work does not pay the
seven-handler duplication tax, because its only worker-adjacent concern is
transactional and transactional concerns already live in `Repo.transition`.

---

## Rejection shape

| Option | Description | Selected |
|--------|-------------|----------|
| `code` field additive, populated only for the two 429s | Add `code` beside `error`, set `quota_exceeded` and `rate_limited`, leave every other body untouched. Closes QUOT-03, sets the shape SEED-020 will extend, needs no sweep of `writeError`. Fully backward compatible. | ✓ |
| Code every API error now | Give every `writeError` a code. Valuable to the frontend and MCP, but that is separate work already recorded in SEED-020 alongside JobRunner, where error classification centralises. | |
| No new field | Discriminate by error text or status header. Changes no contract, but QUOT-03 asks specifically for machine-readable distinction, which text matching does not provide. | |

**User's choice:** `code` field additive, populated only for the two 429s

| Option | Description | Selected |
|--------|-------------|----------|
| Propagate code and `Retry-After` | Extend `apiError` so a 429 carries the code and wait time into the MCP tool error. Without it an MCP agent cannot distinguish "wait a minute" from "not allowed" and retries into a wall. | ✓ |
| Leave as is | MCP keeps returning the `error` string. Zero work, but QUOT-02's fail-closed guarantee on MCP paths stays a formality. | |

**User's choice:** Propagate code and `Retry-After`

---

## Rollout

| Option | Description | Selected |
|--------|-------------|----------|
| Per-client mode column + global kill-switch | `enforcement_mode` in `client_limits` so enforcement turns on one consumer at a time, as the success criterion requires, plus one env var that forces observe-only service-wide as incident insurance. | ✓ |
| Global env flag only | `QUOTA_MODE=observe\|enforce` for the whole service. Simplest, but enabling means enabling for everyone — precisely the blind switch QUOT-04 exists to prevent. | |
| Per-client column only | No global switch. Fewer entities, but an incident rollback means editing database rows. | |

**User's choice:** Per-client mode column + global kill-switch

| Option | Description | Selected |
|--------|-------------|----------|
| `would-be-rejected` counter with reason | `{client_id, engine, reason}` where reason separates concurrency from bytes from job count — an operator needs to know which limit would have fired. `client_id` through the Phase 51 bounded allocator, or the new counter reintroduces the cardinality problem ACCT-04 closed. | ✓ |
| Counter plus a `job_events` row | Adds a durable trace. But `job_events` is keyed on `job_id` and a rejected request never creates a job, so this would need a separate table. | |

**User's choice:** `would-be-rejected` counter with reason

| Option | Description | Selected |
|--------|-------------|----------|
| Derived from the limit type | Daily volume returns seconds until UTC midnight (exact); concurrency returns a short constant, since a slot frees unpredictably. | ✓ |
| One constant for all cases | Simpler, but on a daily cap it is wrong by hours — the client returns too early and gets the identical rejection. | |

**User's choice:** Derived from the limit type

---

## Accepted imprecision (raised during the wrap-up, not a pre-selected area)

Daily byte and job-count checks read a rollup written only at the terminal
transition, so a burst of admitted-but-unfinished jobs is invisible to the check
and can overshoot the daily cap.

| Option | Description | Selected |
|--------|-------------|----------|
| Accept a bounded overshoot | The concurrency cap already bounds the burst: overshoot is at most `K × MAX_INPUT_BYTES`. Record it as a deliberate allowance and pin it with a test rather than building reservations. | ✓ |
| Also count unfinished jobs | Add `sum(size_bytes)` over the client's `queued`/`active` inputs to the rollup figure. More accurate without reservations, but turns a PK read into two queries. | |
| Full reserve/consume accounting | Separate reserved and consumed counters with release. Exact, but it is precisely the scheme rejected during the architecture review — a second source of truth plus reservation leaks. | |

**User's choice:** Accept a bounded overshoot
**Notes:** This also resolves SC2b ("reservations and completed usage
distinguishable, counted once not twice") — with no reservation written there is
nothing to double-count, and `client_usage_daily` keeps its exactly-once write
inside the terminal transition.

---

## Claude's Discretion

Column types and indexes on `client_limits`; migration ordinal; the advisory-lock
key derivation for `(client_id, engine)` and its non-collision with the
reconciler's existing key; metric and env-var names; the concurrency
`Retry-After` constant; the structural placement of the admission check; the
`manage-clients` subcommand and flag shape.

## Deferred Ideas

- Operator REST surface for limits — Phase 53, which already needs an HTTP read surface.
- Wall-time as an enforced cap — pending observe-only data.
- General structured error codes across all API responses — SEED-020.
- Client-wide (non-per-engine) limit tier — the chosen table leaves room for it.
- Rolling 24-hour quota window — needs a data source that survives retention.
