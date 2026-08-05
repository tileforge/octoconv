# Phase 53: Operator Usage Visibility - Research

**Researched:** 2026-08-05
**Domain:** Go/chi HTTP handler + pgx SQL over an existing schema (no new external dependencies, no schema changes)
**Confidence:** HIGH

## Summary

This phase is almost entirely a codebase-grounding exercise: every mechanism the endpoint needs
already exists in the repo and only needs to be composed, not invented. `requireOperator`
(`internal/api/system_presets_handlers.go:54-67`) is reusable as-is for a new `/v1/system/usage`
route — it is generic over the wrapped handler and keyed only on `auth.ClientFromContext`, with no
preset-specific logic inside it. The load-bearing piece is `evaluate()` in
`internal/jobs/admission.go:236-364`, which is the literal SQL formula D-05 says the endpoint must
mirror exactly: terminal usage from `client_usage_daily` (no status filter — summed across
`done`/`failed`/`canceled`) plus in-flight `queued`/`active` jobs, using bound-literal status
values so `jobs_client_engine_inflight_idx` stays applicable.

One factual correction to CONTEXT.md's canonical_refs is required before planning: migration
`0014_client_usage_canceled.sql` (dated after `0011_client_usage_daily.sql`, part of the merged
Phase 52 PR) **widened** `client_usage_daily.status`'s CHECK constraint from `('done','failed')` to
`('done','failed','canceled')`, and `internal/jobs/repo.go:267` now calls
`recordUsage(ctx, tx, id, StatusCanceled, 0)` from `Repo.Cancel`. CONTEXT.md's canonical_refs
section (line 59) states "`canceled` deliberately excluded" — that was true of the original 0011
migration but has since been **superseded**. The `evaluate()` daily-usage query
(`admission.go:284-295`) already reflects this: it sums `client_usage_daily` with no status
predicate at all. The planner must build the OPSV-01/D-05 usage query the same way — summing
across all three terminal statuses, not filtering to `('done','failed')` — or the endpoint's
numbers will silently disagree with what the admission gate actually enforces, defeating D-05's
entire purpose. See "Conflicts with locked decisions / stale context" below.

No new external packages, no schema migration, and no new access-control mechanism are needed.
The remaining design work is: (1) a new read-only repo method (likely on `internal/jobs.Repo`,
since it already queries `clients`/`client_limits`/`jobs`/`client_usage_daily` together in
`admission.go`) that returns the combined per-(client, engine) usage+limits+remaining rows; (2) a
new handler + route mirroring `handleListSystemPresets`/`jobs_list_handler.go`'s validation idiom;
(3) wiring `clientRepo` (already constructed in `cmd/api/main.go:107` but not currently passed to
`api.NewServer`) or an equivalent narrow interface into `Server`.

**Primary recommendation:** Add one new method to `internal/jobs.Repo` (e.g.
`UsageSummary(ctx, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]UsageRow, error)`) that
reuses `evaluate()`'s exact SQL shape for the in-flight/terminal split, joins `client_limits` for
configured limits, and computes `remaining` the same way `evaluate()` compares against the limit.
Expose it through a new narrow interface (or widen `api.Repo` — see Open Questions) and a new
`handleSystemUsage` gated by the existing `requireOperator` under a new `r.Route("/system/usage",
...)` sibling to `/system/presets` in `routes.go`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Operator authentication/authorization | API / Backend | — | Existing `auth.Middleware` + `requireOperator`, both already in `internal/api` |
| Period → date-range resolution (`today`/`7d`/`30d`) | API / Backend | — | Pure Go logic in the handler, mirroring `secondsUntilUTCMidnight`'s UTC-day convention |
| Terminal usage aggregation | Database / Storage | API / Backend | `client_usage_daily` rollup is the system of record (Phase 51); Postgres does the SUM, Go only shapes the response |
| In-flight (queued/active) accounting | Database / Storage | API / Backend | Must read `jobs` live, using the identical literal-status SQL form `evaluate()` uses for index applicability |
| Limits + remaining computation | API / Backend | Database / Storage | The `remaining` arithmetic (NULL-safe, mirroring `evaluate()`'s comparison) is Go/SQL composition, not a stored value |
| No-leak 404 for non-operators | API / Backend | — | `requireOperator` middleware, unchanged, reused as-is |

## User Constraints (from CONTEXT.md)

<user_constraints>

### Locked Decisions

- **D-01:** One endpoint, `GET /v1/system/usage`. Without `?client_id=`, returns a list across ALL
  clients (primary shape). With `?client_id=`, narrows to that one client. Same handler, same
  response shape (list of one row vs. list of N) — do not build two code paths.
- **D-02:** The all-clients list is sorted by consumption descending for the current period.
  No pagination — bounded by Phase 51's 200-distinct-client Prometheus label ceiling (note: that
  ceiling is a Prometheus-metric-cardinality guard, `internal/metrics/metrics.go:113`, NOT a
  `client_usage_daily` row-count guard — see "Conflicts / clarifications" below for why this still
  supports D-02's "small by construction" reasoning).
- **D-03:** `?period=today|7d|30d` — fixed, fail-closed allow-set validated before any DB work
  (422 on unrecognized value), mirroring `internal/api/jobs_list_handler.go:16-23,36-42`. Do NOT
  build arbitrary `?from=&to=` date-range parsing in this phase.
- **D-04:** Default period when `?period` omitted: `today`. Matches the UTC-day boundary the
  admission gate uses (`(now() AT TIME ZONE 'utc')::date`, `admission.go:288,292`).
- **D-05 (load-bearing):** The remaining-budget figure mirrors Phase 52's admission-gate
  accounting EXACTLY: terminal usage (`client_usage_daily`) PLUS in-flight (`queued`/`active`)
  work, the identical formula `evaluate()` computes.
- **D-06:** Response breaks "used" into `used_terminal` and `used_in_flight` separately, not a
  combined `used` + `remaining`.
- **D-07:** `remaining` is computed against the SAME period the request resolved (`?period`,
  defaulting to `today`) — no separate hardcoded "always today" remaining calculation.
- **D-08:** OPSV-01 (period usage) and OPSV-02 (limits + remaining) are ONE combined response from
  the single `GET /v1/system/usage` endpoint, not two separate endpoints.

### Claude's Discretion

- Exact JSON field names and response envelope shape (list wrapper vs. bare array; whether
  `client_id` is included per-row when `?client_id=` narrows to one).
- How NULL/unlimited limits are represented in JSON — likely `null`, mirroring the CLI's
  `formatOptionalInt`/`formatOptionalInt64` convention (never confuse "unlimited" with a number).
- Whether the response includes a machine-readable `enforcement_mode` (observe/enforce) per
  client — low-cost to include if the query already touches `clients`.
- Internal query shape for the cross-client "today" sort (single query vs. per-client loop) —
  purely a performance/implementation choice.
- Exact route placement under `/v1/system/` and whether `requireOperator` needs any adjustment —
  confirmed by this research: it needs ZERO adjustment, reusable as-is.

### Deferred Ideas (OUT OF SCOPE)

None — discussion stayed within phase scope. The one reviewed-and-explicitly-NOT-folded item is
the Retry-After midnight-race todo (`.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md`),
a distinct bug in `POST /v1/jobs`'s 429 path, unrelated to this read-only endpoint.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OPSV-01 | Operator can get "who consumed what, by engine class, over a period" via an `OPERATOR_CLIENT_IDS`-gated endpoint | `client_usage_daily` schema (0011/0014), `requireOperator` reuse, `?period` validation pattern from `jobs_list_handler.go`, engine enumeration via `convert.Default.Classes()` |
| OPSV-02 | Per-client: currently configured limits + remaining budget, so limits can be set from data | `internal/clients/limits.go` (`ListLimits`/`GetLimit`), `client_limits` schema (0013), `evaluate()`'s exact remaining-budget formula (`admission.go:236-364`) to mirror per D-05 |

</phase_requirements>

## Conflicts with locked decisions / stale context

**Not a locked-decision conflict, but a factual staleness in CONTEXT.md's canonical_refs that
would silently violate D-05 if followed literally.**

CONTEXT.md line 59 states: `client_usage_daily` "`status IN ('done','failed')`, `canceled`
deliberately excluded." This describes migration `0011_client_usage_daily.sql`'s ORIGINAL CHECK
constraint. Migration `0014_client_usage_canceled.sql` (merged as part of Phase 52, PR #5,
`2179b67`) **superseded** that constraint:

```sql
-- 0014_client_usage_canceled.sql
ALTER TABLE client_usage_daily DROP CONSTRAINT client_usage_daily_status_check;
ALTER TABLE client_usage_daily
    ADD CONSTRAINT client_usage_daily_status_check
        CHECK (status IN ('done', 'failed', 'canceled'));
```

`internal/jobs/repo.go:267` (`Repo.Cancel`) now calls `recordUsage(ctx, tx, id, StatusCanceled,
0)`, so canceled jobs DO write rollup rows today. `evaluate()`'s own daily-usage query
(`admission.go:284-295`) reflects this correctly — it sums `client_usage_daily` with **no status
predicate at all**:

```sql
COALESCE((SELECT sum(bytes_in) FROM client_usage_daily
    WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
```

**Why this matters for planning:** D-05 requires this endpoint's `used_terminal` figure to mirror
`evaluate()` exactly. If the planner builds the OPSV-01 usage query using CONTEXT.md's stated
`status IN ('done','failed')` filter, the endpoint's "usage" number will disagree with what the
admission gate actually enforces (which includes canceled) — reintroducing the exact category of
drift D-05 exists to prevent. **Recommendation:** the terminal-usage query must sum
`client_usage_daily` with no status filter (matching `evaluate()`), while still exposing `status`
as an optional breakdown dimension in the response if OPSV-01's "by engine class" framing is read
to also want a done/failed/canceled split (Claude's Discretion — not locked either way; the
`status` column exists precisely so a consumer CAN slice by it, per 0014's own comment: "every
existing consumer that slices by status keeps working"). At minimum, the aggregate `used_terminal`
total used for the D-05/D-07 remaining-budget calculation MUST be status-unfiltered.

This is not a request to relitigate a locked decision — no D-0x decision claims `canceled` is
excluded; only the canonical_refs background note does, and it is now stale relative to the merged
Phase 52 code it was written against.

## Access Control (SC3 — no-leak 404)

**`requireOperator` is reusable AS-IS.** Source: `internal/api/system_presets_handlers.go:54-67`.

```go
func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, ok := auth.ClientFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusNotFound, noSuchPreset)
			return
		}
		if _, isOperator := s.operators[client.ID]; !isOperator {
			writeError(w, http.StatusNotFound, noSuchPreset)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

It has zero dependency on presets — it only reads `auth.ClientFromContext` and `s.operators`
(the `OPERATOR_CLIENT_IDS` set parsed once at startup by `ParseOperatorClientIDs`,
`system_presets_handlers.go:26-43`). Wiring is a new sibling route:

```go
// routes.go, alongside the existing /v1/system/presets route
r.Route("/system/usage", func(r chi.Router) {
	r.Use(s.requireOperator)
	r.Get("/", s.handleSystemUsage)
})
```

**Exact no-leak 404 body**, verbatim from `presets_handlers.go:26` and `writeError`
(`handlers.go:1031-1033`):

```go
const noSuchPreset = "preset not found"
// writeError(w, http.StatusNotFound, noSuchPreset) produces:
// HTTP 404, Content-Type: application/json
// {"error":"preset not found"}
```

**This exact string (`"preset not found"`) is what the new endpoint's non-operator 404 must also
emit**, byte-identical, reusing the same `noSuchPreset` constant — not a new phase-specific string
like `"usage not found"`. The precedent (`system_presets_handlers_test.go:98-128`) proves
byte-identity by encoding the SAME `writeError(wantRec, http.StatusNotFound, noSuchPreset)` into a
separate recorder and comparing `.Bytes()`. The planner's Success Criterion 3 test should follow
this exact idiom — do not invent a new expected-body literal.

**No changes needed to `requireOperator`, `ParseOperatorClientIDs`, or `Server.operators`.**

## Standard Stack

No new external packages. This phase is 100% composition of already-present dependencies:

### Core (existing, reused)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/go-chi/chi/v5` | v5.3.0 | New `/v1/system/usage` route | Already the project's router |
| `github.com/jackc/pgx/v5` | v5.10.0 | New aggregate SQL query | Already the project's DB driver |
| `github.com/google/uuid` | v1.6.0 | `?client_id=` parsing | Already used throughout |

**No `npm install` / `pip install` / `cargo add` needed. Package Legitimacy Audit is N/A — see
below.**

## Package Legitimacy Audit

**Not applicable.** This phase introduces zero new external packages (Go module or otherwise). All
work is new Go source files (`internal/jobs/usage_query.go` or similar, `internal/api/system_usage_handlers.go`)
and no `go.mod`/`go.sum` changes are anticipated. If the planner's task breakdown surfaces an actual
new dependency during execution, the Package Legitimacy Gate protocol must be run at that time —
none is currently foreseeable given the phase boundary (read-only against existing tables).

## Data Sources

### `client_usage_daily` (migrations 0011 + 0014)

```sql
CREATE TABLE client_usage_daily (
    client_id     uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    engine        text NOT NULL,
    day           date NOT NULL,
    status        text NOT NULL CHECK (status IN ('done', 'failed', 'canceled')),  -- widened by 0014
    job_count     bigint NOT NULL DEFAULT 0,
    bytes_in      bigint NOT NULL DEFAULT 0,
    bytes_out     bigint NOT NULL DEFAULT 0,
    wall_seconds  double precision NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (client_id, engine, day, status)
);
CREATE INDEX client_usage_daily_day_idx ON client_usage_daily (day);
```

- No CHECK on `engine` (deliberate — a stale allowlist here must never abort a real conversion's
  terminal transition; the same reasoning applies to `client_limits`).
- PK includes `status`, so a `(client_id, engine, day)` triple can have up to 3 rows (one per
  terminal status) — any read must `SUM(...)` / `GROUP BY (client_id, engine)` across `status`,
  never assume one row per day.
- 400-day retention (9600h, `cmd/webhook-worker/main.go:104`, `USAGE_RETENTION` env var) enforced
  by the reconciler's `PurgeUsage` (`internal/jobs/retention.go:122`). All three `?period` values
  (`today`/`7d`/`30d`) are trivially within this window — no boundary interaction to worry about.
- The Phase 51 "200 distinct clients → overflow" ceiling (`internal/metrics/metrics.go:101-126`,
  `ClientLabelCeiling`) is a **Prometheus label-cardinality guard only** — it bounds
  `octoconv_client_job_outcomes_total`'s `client_id` label, NOT `client_usage_daily` row count or
  distinct-client count in Postgres. `client_usage_daily` has no analogous cap; it is exact and
  unbounded by client count (bounded only by actual client population × engine × day × status).
  D-02's "bounds the response size" reasoning is really an operational argument (OctoConv's
  clients are internal company services, actual population is far below 200 — same reasoning
  `metrics.go:216-219`'s comment gives) rather than a schema-enforced ceiling. **Flag for planner:**
  if real usage grows past a few hundred distinct clients, the no-pagination `GET
  /v1/system/usage` (all clients) response could grow unboundedly; this is accepted per D-02, not
  a defect to fix in this phase.

### `client_limits` (migration 0013)

```sql
CREATE TABLE client_limits (
    client_id       uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    engine          text NOT NULL,
    max_concurrent  int,      -- NULL = unlimited; 0 is a legal explicit deny
    max_daily_bytes bigint,   -- NULL = unlimited
    max_daily_jobs  bigint,   -- NULL = unlimited
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (max_concurrent IS NULL OR max_concurrent >= 0),
    CHECK (max_daily_bytes IS NULL OR max_daily_bytes >= 0),
    CHECK (max_daily_jobs IS NULL OR max_daily_jobs >= 0),
    PRIMARY KEY (client_id, engine)
);
CREATE INDEX jobs_client_engine_inflight_idx ON jobs (client_id, engine)
    WHERE status IN ('queued', 'active');
```

- Also added by 0013: `clients.enforcement_mode text NOT NULL DEFAULT 'observe' CHECK
  (enforcement_mode IN ('observe', 'enforce'))` — this is the field Claude's Discretion suggests
  surfacing per-client in the response.
- **An absent `client_limits` row means unlimited on every dimension** — identical to a row with
  all three columns NULL (`quotaState.unlimited()`, `admission.go:201-203`). The endpoint's
  "configured limits" section must represent both cases identically (e.g., all three limit fields
  `null` in JSON), never synthesizing a fake row.
- `jobs_client_engine_inflight_idx` is the index the in-flight COUNT query must match — this
  requires writing the status predicate as SQL literals (`'queued', 'active'`), not bound
  parameters, exactly as `evaluate()` does (see below).

## The remaining-budget formula (D-05) — verbatim extraction

Source: `internal/jobs/admission.go:236-364` (`evaluate`). This is the EXACT formula the endpoint
must mirror.

**Concurrency dimension** (`max_concurrent`):
```sql
SELECT count(*) FROM jobs
WHERE client_id = $1 AND engine = $2 AND status IN ($3, $4)  -- bound params here, NOT literals
-- $3, $4 = StatusQueued ("queued"), StatusActive ("active")
```
`remaining_concurrent = max_concurrent - count` (never negative in the admission gate's own use,
since it's a boolean `count >= max_concurrent` check; for display purposes the endpoint should
clamp remaining to `max(0, limit - used)` if it wants a non-negative "remaining", or show a
negative to make an over-limit condition visible — Claude's Discretion, but note D-06 already
handles the "why is remaining low" diagnosis via `used_in_flight`, so a raw signed
`limit - used` is arguably MORE informative for OPSV-02's stated goal ("set limits from observed
data") than a clamped-at-zero value. Recommend: **do not clamp** — show the true signed value so
an operator can see how far over/under a client is).

**Daily bytes / daily jobs dimension** (`max_daily_bytes`, `max_daily_jobs`):
```sql
SELECT
    COALESCE((SELECT sum(bytes_in) FROM client_usage_daily
        WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
    + COALESCE((SELECT sum(ji.size_bytes) FROM jobs j JOIN job_inputs ji ON ji.job_id = j.id
        WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0),
    COALESCE((SELECT sum(job_count) FROM client_usage_daily
        WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
    + COALESCE((SELECT count(*) FROM jobs j
        WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0)
```

Critical details to replicate exactly:
1. **No status filter on `client_usage_daily`** in the terminal-sum subqueries — sums across
   `done`/`failed`/`canceled` (see "Conflicts" section above).
2. **The in-flight subqueries use literal `'queued', 'active'` strings, not bound `$n` params** —
   this is deliberate so the query planner can match `jobs_client_engine_inflight_idx`'s partial
   predicate. `evaluate()`'s own comment (`admission.go:295-304`) explains this is NOT unified
   with the concurrency-count query above (which DOES bind `$3`/`$4`) for exactly this reason.
   **The planner must replicate this same literal-vs-bound-param split**, or the visibility
   endpoint's in-flight-bytes/in-flight-jobs query will not use the index.
3. **No `created_at` predicate on the in-flight term** — a queued/active job counts toward
   "in-flight" regardless of when it was created, only `client_usage_daily.day` is date-scoped.
   This has a documented semantic wrinkle for D-07 (see below).
4. `max_daily_bytes`/`max_daily_jobs` comparisons in `evaluate()` are `>` (strictly exceeds,
   because the admission check adds the INCOMING request's own contribution before comparing) —
   the visibility endpoint has no incoming request, so `remaining = limit - (terminal + in_flight)`
   with no `+1`/`+inputBytes` adjustment.

**Limit dimensions and NULL=unlimited handling** (`quotaState`, `admission.go:190-203`):
```go
type quotaState struct {
	enforcementMode string
	maxConcurrent   *int
	maxDailyBytes   *int64
	maxDailyJobs    *int64
}
func (st quotaState) unlimited() bool {
	return st.maxConcurrent == nil && st.maxDailyBytes == nil && st.maxDailyJobs == nil
}
```
Three independent dimensions, each independently nullable. `remaining` per dimension is undefined
(`null` in JSON) when that dimension's limit is `nil` — do not compute `0 - used` or any sentinel
number for an unlimited dimension; that would misrepresent unlimited as a hard-zero cap.

## D-07 + multi-day periods — the semantic wrinkle the planner must resolve

**Surfaced explicitly, as instructed.** D-07 says `remaining` is computed against the SAME
resolved period (`today`/`7d`/`30d`). But `evaluate()`'s live formula — the thing D-05 says must be
mirrored EXACTLY — is inherently a **single-day** (`today`-only) comparison: it always filters
`client_usage_daily` to `day = (now() AT TIME ZONE 'utc')::date`, because `max_daily_bytes` /
`max_daily_jobs` are, by name and by the admission gate's actual enforcement, DAILY caps. There is
no such thing as a "7-day remaining" or "30-day remaining" in the admission gate's own semantics —
the gate resets every UTC midnight regardless of what multi-day window an operator might query.

Two readings are possible for `?period=7d`/`?period=30d` combined with D-07:

1. **`used_terminal`/`used_in_flight` for the period, but `remaining` is ALWAYS computed against
   `today` alone** (ignoring `?period` for the remaining calculation specifically) — this
   contradicts D-07's literal text ("no separate, hardcoded always-today remaining calculation").
2. **`remaining` for `?period=7d` sums `client_usage_daily` over the FULL 7-day window and
   compares that sum against `max_daily_bytes`/`max_daily_jobs`** — this is D-05-compliant in
   spirit (same limit columns, same in-flight term) but produces a number with NO admission-gate
   analogue: `evaluate()` never compares a 7-day sum against a daily limit, so a 7-day "remaining"
   computed this way could show deeply negative values for any client with normal daily usage
   under a tight daily cap, even when the CLIENT'S NEXT REQUEST would be admitted fine (today's
   bucket reset). This is not "wrong" data, but it is easy to misread as "this client is way over
   budget" when in fact only TODAY'S bucket matters for admission.

**Recommendation for the planner (not a locked decision — Claude's Discretion territory, but
worth flagging loudly since D-05+D-07 create tension that isn't self-resolving):** implement
reading (2) — sum the terminal term across the full resolved `day` range for `remaining`, keeping
the in-flight term unchanged (it has no `day` predicate in `evaluate()` either way) — because this
is what "computed against the SAME period" (D-07) literally requires, and it is still a faithful
generalization of `evaluate()`'s formula (same columns, same in-flight predicate, just a wider
`day` range). But the response MUST make the semantic difference from live enforcement visible:
document in the JSON response (a field like `remaining_is_daily_equivalent: false` when
`period != "today"`, or simply document it in the API's doc comment / OpenAPI description) that
`remaining` for `7d`/`30d` describes a hypothetical "if this whole window were one day" comparison,
NOT what `evaluate()` would decide right now. The `today` case has no such caveat — it IS exactly
what `evaluate()` computes, byte-for-byte. This distinction directly serves OPSV-02's actual intent
("set limits from observed data") — an operator sizing `max_daily_bytes` from a 30-day trend
legitimately wants to know "if my daily cap were applied to this month's typical day, would today's
worst day have blown it," which is what reading (2) answers.

## Cross-client "sort by consumption desc" (D-02)

**Sort key:** total bytes (`bytes_in + bytes_out`?) or `job_count`? CONTEXT.md does not specify
which dimension "consumption" means when a client has multiple engines. Two reasonable choices:

- Sort by the SUM of `used_terminal` bytes (`bytes_in + bytes_out`) across ALL engines for that
  client in the resolved period — matches "who consumed the most [data]".
- Sort by `job_count` — matches "who ran the most jobs".

Given OPSV-01's own framing ("кто сколько потребил" — "who consumed how much") and that
`client_usage_daily` tracks both, **recommend sorting by total bytes (`bytes_in + bytes_out`)
summed across engines**, since bytes is the closer proxy for "consumption" (job count treats a
1 KB job and a 2 GB video job identically). This is Claude's Discretion per CONTEXT.md — no locked
decision — but the planner needs SOME concrete choice; document whichever is chosen in the
endpoint's response/doc comment since it is not self-evident from the requirement text alone.

**Query shape for the cross-client case:** a single query using window functions or a two-step
Go-side aggregation is both viable. Recommend a single SQL query per period, e.g.:

```sql
-- Terminal usage, grouped by client, summed across engine AND status, for the resolved day range
SELECT client_id, engine,
       SUM(job_count) AS job_count,
       SUM(bytes_in)  AS bytes_in,
       SUM(bytes_out) AS bytes_out
FROM client_usage_daily
WHERE day BETWEEN $1 AND $2  -- resolved period's [dayFrom, dayTo] inclusive
  AND ($3::uuid IS NULL OR client_id = $3)  -- optional ?client_id= narrowing, same query both ways (D-01)
GROUP BY client_id, engine
```
then a second query for in-flight per (client, engine), then a third for `client_limits` +
`clients.enforcement_mode`, joined/merged in Go. Three simple queries composed in Go is more
in-keeping with this codebase's style (no CTEs/window functions observed anywhere in
`internal/jobs` or `internal/clients` — every existing query is a flat `SELECT`/`JOIN` at most 2
tables deep) than one large 3-way-joined aggregate query. **This satisfies D-01's "same handler,
same response shape" requirement** naturally: the `?client_id=` narrowing is a single optional
`WHERE` clause parameter (`$3::uuid IS NULL OR client_id = $3` — the exact idiom pgx supports via a
`*uuid.UUID` bind), not two separate code paths.

## Repo-layer reuse

| Method | Reusable as-is? | Notes |
|--------|-----------------|-------|
| `clients.Repo.ListLimits(ctx, clientID)` (`internal/clients/limits.go:87-110`) | Partially — it is single-client only, ordered by engine. Reusable for the `?client_id=` narrowed case; the ALL-clients case (D-01 primary shape) needs a NEW method with no `WHERE client_id = $1`, or N+1 calls to `ListLimits` (avoid — the whole codebase's query style favors one query over a loop; see `handleListJobs`'s single-query pattern). | Recommend a new `clients.Repo.ListAllLimits(ctx) ([]LimitWithClient, error)` OR fold limits into the same package doing the usage query (see below). |
| `clients.Repo.GetLimit` | Not needed — superseded by the batch read above. | — |
| `jobs.Repo` — **no existing read method for `client_usage_daily`** beyond `PurgeUsage` (delete-only) and the private `recordUsage` (write-only, inside `internal/jobs/usage.go`). | New method required. | `internal/jobs/admission.go` already has ALL the SQL patterns to copy (same package, same file conventions) — the natural home for the new read is `internal/jobs`, NOT `internal/clients`, because `internal/jobs` is the package that already owns `evaluate()`'s exact formula and the in-flight-index-matching SQL literal convention. |

**Cross-package design question (Claude's Discretion / planner decision):** should the new method
live in `internal/jobs` (which already queries `clients`/`client_limits` via raw SQL inside
`admission.go`, same precedent `loadQuotaState` sets — joining `clients c LEFT JOIN client_limits
l`) or should it be split — `jobs.Repo` returns usage+in-flight, `clients.Repo` returns limits, and
the HANDLER composes both? **Recommend the split**, for interface-segregation reasons this
codebase already follows (`internal/api/api.go`'s `Repo`/`Storage`/`Enqueuer`/`PresetRepo`/
`PresetAdmin` are all narrow, single-purpose interfaces — see "Key Abstraction" in
CLAUDE.md/ARCHITECTURE): `jobs.Repo` exposes a `UsageSummary` method returning
usage+in-flight+client_id+engine rows; `clients.Repo` exposes a new `ListAllLimits` method
returning limit+enforcement_mode rows keyed by client+engine; the HTTP handler joins the two
slices in Go by `(client_id, engine)` key. This avoids `internal/jobs` needing to know about
`clients.Limit`'s Go type, and avoids `internal/clients` needing to know about `jobs.Status*`
constants — each repo stays scoped to its own table family, same as today.

**New `api.Server` dependency:** `Server` currently has NO `clients` repo field at all (confirmed —
`internal/api/api.go`'s `Server` struct has `repo Repo` (jobs only), no clients dependency).
`cmd/api/main.go:107` already constructs `clientRepo := clients.NewRepo(pool)` but never passes it
to `api.NewServer` (`cmd/api/main.go:170`). The planner must either:
1. Add a new narrow interface (e.g. `LimitsRepo` with just the one new method) as a new
   positional/config parameter to `NewServer`, wiring `clientRepo` from `cmd/api/main.go`, or
2. Widen the existing `Repo` interface (jobs) — NOT recommended, breaks interface segregation
   since `Repo` is currently jobs-only and every existing consumer (`fakeRepo` in
   `handlers_test.go`) would need a new method regardless of whether they exercise it.

Recommend (1): a new interface `UsageRepo` (or similar) in `api.go`, e.g.:
```go
type UsageRepo interface {
	// New jobs.Repo method — usage + in-flight, no limits
	UsageSummary(ctx context.Context, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]jobs.UsageRow, error)
}
type LimitsRepo interface {
	// New clients.Repo method — limits + enforcement_mode, all clients or one
	ListAllLimits(ctx context.Context, clientID *uuid.UUID) ([]clients.LimitWithMeta, error)
}
```
Both added as new `Server` fields + `NewServer` parameters, following the exact pattern
`presets`/`presetAdmin` already establish (two narrow interfaces backed by potentially the same
or different concrete repos).

## Handler + validation patterns to mirror

Source: `internal/api/jobs_list_handler.go:12-42` (allow-set validation), `internal/api/handlers.go`
(`writeJSON`/`writeError`).

```go
// Fail-closed period validation, mirroring listJobsValidStatuses (jobs_list_handler.go:12-22)
var validUsagePeriods = map[string]struct{}{
	"today": {}, "7d": {}, "30d": {},
}

func (s *Server) handleSystemUsage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	period := q.Get("period")
	if period == "" {
		period = "today"
	}
	if _, ok := validUsagePeriods[period]; !ok {
		writeError(w, http.StatusUnprocessableEntity, "invalid period filter")
		return
	}

	var clientID *uuid.UUID
	if v := q.Get("client_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid client_id")
			return
		}
		clientID = &id
	}

	// ... resolve period to [dayFrom, dayTo], call the two repo methods, join, sort, writeJSON
}
```

`writeJSON`/`writeError` signatures (`handlers.go:968-975,1031-1033`):
```go
func writeJSON(w http.ResponseWriter, status int, v any)
func writeError(w http.ResponseWriter, status int, msg string)  // {"error": msg}
```
No new response-writing helper needed. Non-nil empty slices for JSON marshaling (`[]` not `null`)
is an established convention (`jobs_list_handler.go:81`, D-02 there) — apply the same to this
endpoint's list.

**Engine enumeration source of truth:** `convert.Default.Classes() map[string][]Pair`
(`internal/convert/convert.go:155`), keys are `convert.EngineImage`/`EngineDocument`/`EngineHTML`/
`EngineAudio`/`EngineAV`/`EngineOCR`/`EnginePandoc` (7 engines, `convert.go:20-26`). This is used
by `jobs_list_handler.go:45` to validate `?engine=`. This phase's response does NOT need an
`?engine=` query param (not in D-01..D-08), but if the response enumerates "which engines does
this client have activity/limits on," the enumeration should come from the QUERY RESULTS
(distinct `engine` values actually present in `client_usage_daily`/`client_limits` for that
client), not from `convert.Default.Classes()` — a client's historical usage rows may reference an
engine no longer compiled in (schema deliberately has no CHECK on `engine`, per both migrations'
comments), and the visibility endpoint should show what actually happened, not filter it through
today's compiled engine set.

## Test patterns

Confirmed via `internal/api/system_presets_handlers_test.go` and `internal/jobs/admission_test.go`
— stdlib `testing` only, no assertion library (CLAUDE.md conformant).

**HTTP-layer no-leak-404 test idiom** (`system_presets_handlers_test.go:98-128`,
`228-263`) — the exact pattern the planner's SC3 test must follow:
```go
func TestSystemUsage_NonOperatorNoLeak404(t *testing.T) {
	operator := &clients.Client{ID: uuid.New(), Name: "operator"}
	other := &clients.Client{ID: uuid.New(), Name: "other"}
	srv := newOperatorGateTestServer(map[uuid.UUID]struct{}{operator.ID: {}}) // or a usage-specific equivalent

	req := httptest.NewRequest(http.MethodGet, "/v1/system/usage", nil)
	req = req.WithContext(auth.WithClient(req.Context(), other))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)  // or exercise requireOperator directly, per the two-tier
	                                   // pattern the presets tests use (isolation test + full-route test)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	wantRec := httptest.NewRecorder()
	writeError(wantRec, http.StatusNotFound, noSuchPreset)
	if !bytes.Equal(rec.Body.Bytes(), wantRec.Body.Bytes()) {
		t.Errorf("body = %q, want byte-identical to noSuchPreset 404 %q", rec.Body.String(), wantRec.Body.String())
	}
}
```

**Fake dependency idiom** (`handlers_test.go:34-100`) — every `api.Repo`-family interface has a
hand-written fake struct capturing call args + configurable return values (no mocking library).
Any new interface (`UsageRepo`/`LimitsRepo` per the recommendation above) needs an equivalent
`fakeUsageRepo`/`fakeLimitsRepo` added to `handlers_test.go` (or a new `_test.go` file) with the
same capture-args-and-configurable-returns shape.

**DB-backed integration test harness:** confirmed present, `DATABASE_URL`-gated,
`t.Skip("DATABASE_URL not set; skipping integration test")` (`internal/jobs/admission_test.go:941-944`,
`internal/clients/limits_test.go:174-175`, `internal/jobs/repo_test.go:15-18`). No testcontainers,
no sqlmock — tests run against a real Postgres instance the developer/CI must provision via
`DATABASE_URL`, and call `db.Migrate` if building their own pool (`admission_test.go:993`) or use
a shared `newTestRepo(t)` helper (`internal/jobs/repo_test.go:15`) that presumably does the same.
**The new `jobs.Repo.UsageSummary`/`clients.Repo.ListAllLimits` methods should get their own
`DATABASE_URL`-gated integration test** proving the exact SQL produces the expected sums, mirroring
`admission_test.go`'s fixture style (`createFixture`, `createTestClient`, `setEnforcementMode`
helpers already exist in that package and are reusable for building usage-visibility fixtures too,
since they are unexported but same-package).

## Common Pitfalls

### Pitfall 1: Filtering `client_usage_daily` by `status IN ('done','failed')`
**What goes wrong:** The endpoint's usage numbers under-report relative to what `evaluate()`
enforces, silently violating D-05.
**Why it happens:** CONTEXT.md's canonical_refs (written against the pre-0014 schema) says
`canceled` is excluded; that statement is now stale (see "Conflicts" section).
**How to avoid:** Sum `client_usage_daily` with NO status filter for the D-05 aggregate figure,
exactly as `evaluate()` does. Optionally expose `status` as a breakdown dimension separately.
**Warning signs:** A canceled-job-heavy client shows `remaining` higher on the visibility endpoint
than what a real `POST /v1/jobs` from that client would actually be admitted at.

### Pitfall 2: Binding in-flight status literals as query parameters
**What goes wrong:** `SELECT ... WHERE status IN ($1, $2)` with bound params instead of literal
`'queued', 'active'` strings can defeat `jobs_client_engine_inflight_idx`'s partial-index
predicate match, turning a fast partial-index scan into a full/sequential scan of `jobs` — exactly
the failure mode `admission.go:295-304`'s comment documents and deliberately avoids.
**Why it happens:** Binding is the "normal" idiom everywhere else in this codebase (SQL-injection
hygiene habit); it's easy to reflexively bind here too.
**How to avoid:** Copy `evaluate()`'s exact literal-string form for the in-flight subquery, only
binding `client_id`/`engine`/date-range as real parameters.
**Warning signs:** `EXPLAIN ANALYZE` on the in-flight query shows a Seq Scan on `jobs` instead of
an Index Scan using `jobs_client_engine_inflight_idx`.

### Pitfall 3: Unbounded cross-client query on a growing `jobs` table for in-flight counts
**What goes wrong:** D-02's no-pagination all-clients response computes an in-flight COUNT/SUM
per client — if done as N+1 queries (one per client) rather than one grouped query, this scales
linearly with distinct client count and could be slow as the client population grows toward (or
past) the informal ~200 assumption.
**Why it happens:** The per-client `evaluate()` formula is naturally single-client; naively
looping it for the all-clients case is the obvious-but-wrong translation.
**How to avoid:** Use `GROUP BY client_id, engine` on the in-flight query (bounded by
`jobs_client_engine_inflight_idx`'s partial predicate, which already limits the scan to
queued/active rows only — small relative to the full `jobs` table regardless of client count) so
it is one query regardless of N clients.
**Warning signs:** Response latency for the all-clients endpoint scales with distinct client count
rather than staying roughly constant.

### Pitfall 4: Timezone drift between the rollup, the admission gate, and this endpoint
**What goes wrong:** If the endpoint resolves `today`/`7d`/`30d` using the Go process's local
timezone or `time.Now()` instead of UTC, its day boundaries will disagree with
`client_usage_daily.day` (written as `(now() AT TIME ZONE 'utc')::date`, Postgres transaction-start
time) and with `evaluate()`'s own UTC-day filter.
**Why it happens:** Go's `time.Now()` defaults to whatever the process's configured location is;
easy to forget `.UTC()`.
**How to avoid:** Always call `.UTC()` before deriving day boundaries in Go, mirroring
`secondsUntilUTCMidnight`'s existing convention (`handlers.go:986-987`, `now = now.UTC()`).
**Warning signs:** An operator sees `today`'s numbers change at a time that doesn't match UTC
midnight (e.g., shifts at local midnight if the deploy host isn't UTC).

### Pitfall 5: Confusing "no `client_limits` row" with "explicit deny" for `max_concurrent = 0`
**What goes wrong:** Treating a `nil` `max_concurrent` and an explicit `0` the same way in the
response — both are legal, but nil means unlimited and 0 means "run nothing on this engine."
**Why it happens:** Both look like "falsy" values if scanned into a non-pointer Go int.
**How to avoid:** Scan into `*int`/`*int64` exactly as `clients.Limit` already does
(`internal/clients/limits.go:23-28`) — never coerce to a bare `int` with a 0 default.
**Warning signs:** An operator sees `max_concurrent: 0` and cannot tell if that means "unlimited"
(bug) or "denied" (correct) from the JSON alone — the type must make this unambiguous (`null` vs
`0`).

## Code Examples

### `evaluate()`'s formula, condensed for the endpoint's read-only equivalent
```go
// Source: internal/jobs/admission.go:236-364 (evaluate) — the endpoint's
// UsageSummary/remaining computation must reproduce this SQL shape exactly
// for the `today` case, and generalize only the day-range for 7d/30d (see
// "D-07 + multi-day periods" section above).
const usageSummarySQL = `
	SELECT
		client_id, engine,
		COALESCE(SUM(job_count), 0)  AS job_count,
		COALESCE(SUM(bytes_in), 0)   AS bytes_in,
		COALESCE(SUM(bytes_out), 0)  AS bytes_out
	FROM client_usage_daily
	WHERE day BETWEEN $1 AND $2
	  AND ($3::uuid IS NULL OR client_id = $3)
	GROUP BY client_id, engine`

const inFlightSQL = `
	SELECT client_id, engine,
		count(*) AS jobs_in_flight,
		COALESCE((SELECT sum(ji.size_bytes) FROM job_inputs ji WHERE ji.job_id = j.id), 0) AS bytes_in_flight
	FROM jobs j
	WHERE j.status IN ('queued', 'active')  -- literal, matches jobs_client_engine_inflight_idx
	  AND ($1::uuid IS NULL OR j.client_id = $1)
	GROUP BY client_id, engine`
```
Note: the `inFlightSQL` above has a subquery-per-row cost for `bytes_in_flight` — acceptable given
the partial index already bounds the outer scan to a small queued/active set, but worth an
`EXPLAIN ANALYZE` sanity check during implementation given it wasn't in `evaluate()`'s own
per-client form (which sums via a JOIN, not a correlated subquery — `admission.go:289`). Prefer
mirroring `evaluate()`'s JOIN form instead of a correlated subquery:
```sql
SELECT j.client_id, j.engine, count(DISTINCT j.id), COALESCE(sum(ji.size_bytes), 0)
FROM jobs j JOIN job_inputs ji ON ji.job_id = j.id
WHERE j.status IN ('queued', 'active')
GROUP BY j.client_id, j.engine
```
(watch for double-counting `count(*)` if a job has multiple `job_inputs` rows — use
`count(DISTINCT j.id)` as shown, since `evaluate()`'s own per-client form avoids this by running
the job-count subquery separately from the byte-sum subquery, at `admission.go:291-294`. Two
separate GROUP BY queries — one for job count, one for bytes — is actually the safer generalization
of `evaluate()`'s existing two-separate-subqueries structure than one JOINed query.)

## State of the Art

Not applicable — this phase mirrors an internal formula already implemented one migration ago
(Phase 52), not an external ecosystem convention. No external "state of the art" comparison is
relevant.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | "Consumption" for D-02's sort should mean total bytes (`bytes_in + bytes_out`), not `job_count` | Cross-client sort | Low — either is defensible; if wrong, only the default sort order differs, no correctness impact. Flagged as Claude's Discretion in CONTEXT.md, not a hard requirement. |
| A2 | The new usage-read method belongs on `internal/jobs.Repo`, limits-read on `internal/clients.Repo`, joined in the HTTP handler | Repo-layer reuse | Medium — a different split (e.g., one combined method in a new package) would still satisfy requirements but changes which files the planner's tasks touch. Not locked by CONTEXT.md ("Claude's Discretion" implicitly, since CONTEXT.md doesn't name the method). |
| A3 | For `?period=7d`/`30d`, `remaining` should sum the terminal term across the full day range (reading (2) in the D-07 analysis) rather than always comparing against `today` alone | D-07 + multi-day periods | Medium-High — this is the most consequential open interpretive question in this research; getting it wrong produces a `remaining` figure that either contradicts D-07's text or produces a number with no admission-gate analogue. Recommend surfacing to the user/planner explicitly if the planner is not comfortable proceeding on this research's recommendation alone. |

**None of these block planning** — all three are Claude's-Discretion-shaped, not hard blockers,
but A3 is significant enough that the planner should state its chosen resolution explicitly in
PLAN.md rather than let it fall out of task-writing incidentally.

## Open Questions

1. **(RESOLVED 2026-08-05 — see `53-CONTEXT.md` D-09)** The user chose reading (2) during
   plan-phase: `remaining` for `7d`/`30d` sums terminal usage across the full resolved day range
   against the daily limit, with the in-flight term unchanged, plus a mandatory machine-readable
   caveat distinguishing `today` (byte-for-byte `evaluate()`) from multi-day (hypothetical, no
   admission-gate analogue). Implemented and tested in plan `53-04`
   (`remaining_matches_admission_gate` / `remaining_semantics` / `remaining_note`; both states
   pinned by tests). Original question text retained below for traceability.

   **Should `remaining` for `?period=7d`/`30d` be "as if this whole window were one day" (reading
   2, recommended) or "always compare against today's bucket regardless of `?period`" (reading 1,
   literal-but-arguably-contradicts-D-07)?**
   - What we know: `evaluate()` itself has NO multi-day concept — `max_daily_bytes`/
     `max_daily_jobs` are inherently daily caps enforced against `day = today` only.
   - What's unclear: which reading D-07's author intended when locking that decision — the
     CONTEXT.md discussion transcript (`53-CONTEXT.md`) doesn't show the multi-day case being
     explicitly worked through, only that "remaining" must track whatever period was requested.
   - Recommendation: implement reading (2), document the caveat in the response/API doc, as
     detailed above. If the planner or user disagrees on execution, this is cheap to change later
     since it's isolated to one aggregation query, not a schema or interface change.

2. **Exact JSON field names / response envelope** — explicitly left to Claude's Discretion by
   CONTEXT.md. No further research needed; this is a planning-time decision, not a research gap.

3. **Whether `enforcement_mode` is included per client-engine row** — also explicitly Claude's
   Discretion. Low-cost to include (the query already touches `clients` for the limits join);
   recommend including it, since OPSV-02's "what are this client's limits" question is
   incompletely answered without knowing whether those limits are actually being enforced or only
   observed.

## Environment Availability

Skipped — this phase has no new external tool/service/runtime dependencies. All work is Go source
against the existing Postgres schema, using already-present, already-verified-available
dependencies (`go`, `pgx`, `chi` — all confirmed present and in use throughout the existing
codebase; no new probe needed).

## Security Domain

`security_enforcement` is absent from `.planning/config.json` → treated as enabled per policy.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No (unchanged) | Existing `auth.Middleware` (API-key based), not touched by this phase |
| V3 Session Management | No | Stateless API-key auth, no sessions in this project |
| V4 Access Control | Yes | Reused `requireOperator` (`internal/api/system_presets_handlers.go:54-67`) — narrower-than-authentication authorization check, fail-closed on empty allowlist, uniform 404 for both "not authenticated resolvable" and "authenticated but not an operator" |
| V5 Input Validation | Yes | Fail-closed allow-set validation for `?period` (422 before DB work, mirroring `jobs_list_handler.go`); `uuid.Parse` for `?client_id` (400 on malformed UUID, mirroring existing `chi.URLParam`+`uuid.Parse` patterns elsewhere in the codebase) |
| V6 Cryptography | No | No crypto surface introduced |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| SQL injection via `?client_id=`/`?period=` | Tampering | pgx parameterized queries throughout (`$1`, `$2`, ...) — never string-concatenate query params into SQL, consistent with every existing query in this codebase |
| Information disclosure — a non-operator learning ANYTHING about `/v1/system/usage`'s existence or behavior (timing, distinguishable error) | Information Disclosure | `requireOperator`'s byte-identical 404 (reused as-is) is the existing, tested mitigation; this phase's only NEW disclosure risk is if the new route accidentally skips `requireOperator` in `routes.go` — verify the route is registered inside the `r.Use(s.requireOperator)` sub-router, not as a top-level `/v1` route |
| Cross-client data disclosure via `?client_id=` to a NON-operator | Information Disclosure / Elevation of Privilege | Not reachable — `requireOperator` runs before the handler ever inspects `?client_id`; a non-operator never reaches the code that would honor it. No additional check needed inside the handler itself, since by the time the handler runs, the caller is already a confirmed operator (same precedent as `handleShowSystemPreset`, which trusts `requireOperator` completely and does zero re-checking) |
| Denial of service via unbounded all-clients response (D-02, no pagination) | Denial of Service | Accepted risk per D-02 (see Pitfall 3 / Data Sources note above) — bounded in practice by OctoConv's internal-only client population; if this becomes a real concern, it is future work, not this phase's scope |

## Sources

### Primary (HIGH confidence — direct repo file reads, this session)
- `internal/api/system_presets_handlers.go` — `ParseOperatorClientIDs`, `requireOperator`, `noSuchPreset` usage
- `internal/api/routes.go` — `/v1/system/presets` route wiring precedent
- `internal/api/presets_handlers.go:26` — `noSuchPreset` constant definition
- `internal/jobs/admission.go` — `evaluate()`, `loadQuotaState`, `quotaState`, `admitLocked`, `PreAdmit` (full file read)
- `internal/db/migrations/0011_client_usage_daily.sql`, `0013_client_limits.sql`, `0014_client_usage_canceled.sql` — full schema + the canceled-status supersession
- `internal/jobs/usage.go` — `recordUsage` (write path, confirms canceled is written)
- `internal/jobs/repo.go:261-268` — `Repo.Cancel` calling `recordUsage(..., StatusCanceled, 0)`
- `internal/clients/limits.go` — `Limit`, `SetLimit`, `ClearLimit`, `GetLimit`, `ListLimits`, `SetEnforcementMode` (full file read)
- `internal/clients/clients.go`, `internal/db/migrations/0001_init.sql` (clients table) — `Client{ID, Name}`, table shape
- `internal/api/jobs_list_handler.go` — fail-closed `?status`/`?engine` validation idiom, non-nil-empty-slice JSON convention
- `internal/api/api.go` — `Repo`/`Storage`/`Enqueuer`/`PresetRepo`/`PresetAdmin` interface-segregation precedent, `Server` struct (confirms no existing `clients` dependency)
- `internal/api/handlers.go:968-1033` — `writeJSON`, `writeError`, `secondsUntilUTCMidnight` (UTC convention precedent)
- `internal/metrics/metrics.go` — `ClientLabelCeiling`/`overflowClientLabel` (confirms the 200-client ceiling is Prometheus-only, not a DB constraint)
- `internal/api/system_presets_handlers_test.go` — no-leak-404 byte-identity test idiom, `newOperatorGateTestServer` fake-server pattern
- `internal/api/handlers_test.go:34-100` — `fakeRepo` hand-written-fake idiom
- `internal/jobs/admission_test.go:900-999` — `DATABASE_URL`-gated integration test idiom, `db.Migrate` usage
- `cmd/manage-clients/main.go:178-202` — `show-limits` CLI precedent, `formatOptionalInt`/`formatOptionalInt64` NULL-vs-value convention
- `cmd/api/main.go:107,170` — confirms `clientRepo` is constructed but not yet passed to `api.NewServer`
- `cmd/webhook-worker/main.go:99-105` — `USAGE_RETENTION` = 9600h (400 days), confirms all `?period` values are well within retention
- `internal/jobs/jobs.go` — status constants, `Job`/`Input` struct shapes
- `internal/convert/convert.go:20-26,155` — engine constants, `Registry.Classes()`
- `.planning/phases/53-operator-usage-visibility/53-CONTEXT.md` — full locked decisions, discretion areas, canonical refs (read in full)
- `.planning/REQUIREMENTS.md` §"Operator Visibility" — OPSV-01/OPSV-02 literal text
- `.planning/ROADMAP.md` §"Phase 53" — goal, success criteria, dependency framing
- `.planning/STATE.md` — Phase 51→52→53 sequencing rationale, pending-todo note for Phase 53
- `.planning/config.json` — confirms `nyquist_validation: false` (Validation Architecture section skipped), `security_enforcement` absent (Security Domain section included)

### Secondary / Tertiary
None used — this is a codebase-grounded phase and every claim above traces to a direct file read
in this session; no WebSearch/Context7/external lookups were needed or performed.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new dependencies, 100% reuse of already-verified-in-production code
- Architecture: HIGH — every pattern (route wiring, interface segregation, fake-repo testing,
  fail-closed validation) is a direct, recent (Phase 26/40/51/52) precedent in this exact codebase
- Pitfalls: HIGH — all five pitfalls are derived from explicit, dated comments in the source code
  itself (`admission.go`'s extensive why-not-what comments), not speculation
- The D-07/multi-day semantic question: MEDIUM — this is a genuine interpretive gap in the locked
  decisions, not a research confidence gap; the recommendation is well-reasoned but is a judgment
  call, flagged accordingly in Assumptions Log A3 and Open Questions #1

**Research date:** 2026-08-05
**Valid until:** No expiry driver — this is a closed, internal, schema-stable codebase snapshot;
valid until the next migration touching `client_usage_daily`/`client_limits`/`jobs.status`, or the
next change to `evaluate()`'s formula (Phase 55's live-gate phase reads but does not change this
formula, so no near-term change is expected before this phase executes).
