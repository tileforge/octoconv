# Phase 53: Operator Usage Visibility - Context

**Gathered:** 2026-08-05
**Status:** Ready for planning

<domain>
## Phase Boundary

An operator (a caller whose `client.ID` is in the `OPERATOR_CLIENT_IDS` allowlist) can, through one dedicated HTTP endpoint, answer two questions per client: "how much did they consume, by engine class, over a period" (OPSV-01) and "what are their current limits and how much budget is actually left" (OPSV-02) — backed by the Phase 51 rollup and Phase 52 admission-gate semantics, not a raw `jobs` scan or a Prometheus query. A non-operator caller gets the byte-identical no-leak 404 the project's other operator-only routes already guarantee (ROADMAP Success Criterion 3 — locked, not discussed here).

</domain>

<decisions>
## Implementation Decisions

### Endpoint shape
- **D-01:** One endpoint, `GET /v1/system/usage`. Without `?client_id=`, it returns a list across ALL clients — this is the primary shape, since OPSV-01's own wording ("кто сколько потребил") implies a cross-client view, not a lookup the operator can only perform if they already know the ID. With `?client_id=`, it narrows to that one client. Same handler, same response shape (a list of one row vs. a list of N rows) — do not build two separate code paths for "one" vs "all".
- **D-02:** The all-clients list is sorted by consumption, descending, for the current period — this is what makes "who consumed the most" answerable at a glance. No pagination: the client-label ceiling from Phase 51 (D-08, 200 distinct clients before `overflow` collapse) bounds the response size, so a keyset-paginated shape (mirroring `handleListJobs`) would add complexity for a list that is already small by construction.

### Period selection (OPSV-01)
- **D-03:** `?period=today|7d|30d` — a fixed, fail-closed allow-set validated before any DB work, mirroring `handleListJobs`'s `?status`/`?engine` validation idiom (`internal/api/jobs_list_handler.go:16-23,36-42`): an unrecognized value is rejected 422, never silently coerced. Do NOT build arbitrary `?from=&to=` date-range parsing for this phase — bounds validation, empty-range handling, and interaction with the 400-day retention window (Phase 51 D-12) are real design surface with no requirement pulling for it yet. If a future need for exact ranges appears, it is its own gray area for that phase.
- **D-04:** Default period when `?period` is omitted: `today`. This deliberately matches the UTC-day boundary the admission gate itself uses (`(now() AT TIME ZONE 'utc')::date`) — the default answers "what's happening right now", the most common operator question, using the same day boundary the enforcement path uses so the number an operator sees lines up with what's actually being enforced.

### "Remaining" semantics (OPSV-02) — the load-bearing decision of this phase
- **D-05:** The remaining-budget figure mirrors Phase 52's admission-gate accounting EXACTLY: terminal usage (`client_usage_daily`, the day's rollup) PLUS in-flight work (`queued`/`active` jobs), the identical formula `evaluate()` in `internal/jobs/admission.go` computes post-gap-closure. This is deliberate, not incidental: showing only terminal usage would let "remaining" claim budget is available when the admission gate would actually reject the next request — the exact category of drift that Phase 52's gap closure (52-06) fixed *inside* the admission path. An operator-visible number that doesn't match enforcement reality is worse than no number, because it invites a limit decision based on stale data.
- **D-06:** The response breaks "used" into `used_terminal` and `used_in_flight` separately, not just a combined `used` + `remaining`. Consequence this makes visible on purpose: if a stuck job (the still-open Retry-After/in-flight-time-bound gap noted below) holds a client's `remaining` at 0 indefinitely, the operator sees `used_in_flight > 0` with a small or zero `used_terminal` and can diagnose it directly — rather than only seeing `remaining: 0` and having to guess whether the client is legitimately over-limit or stuck on a phantom job.
- **D-07:** `remaining` is computed against the SAME period the request resolved (D-03/D-04's `?period`, defaulting to `today`) — there is no separate, hardcoded "always today" remaining calculation independent of what `?period` the caller asked for. This keeps the single combined response (D-08) internally consistent: usage-for-period and remaining-for-that-same-period never disagree about what window they describe.

### One endpoint, not two
- **D-08:** OPSV-01 (period usage) and OPSV-02 (limits + remaining) are ONE combined response from the single `GET /v1/system/usage` endpoint (per D-01), not two separate endpoints (`/usage` + `/limits`). The operator's actual question is always "what's going on with this client", not "give me usage" and separately "give me limits" — a combined response avoids two round-trips and, per D-07, avoids the two responses disagreeing about which period they cover.

### Claude's Discretion
- Exact JSON field names and response envelope shape (list wrapper vs bare array, whether `client_id` is included per-row when `?client_id=` narrows to one).
- How NULL/unlimited limits are represented in JSON (planner's call — likely `null`, mirroring the CLI's `formatOptionalInt`/`formatOptionalInt64` convention of never confusing "unlimited" with a number).
- Whether the response includes a machine-readable `enforcement_mode` (observe/enforce) per client — useful context, not explicitly asked for, low-cost to include if the query already touches `clients`.
- Internal query shape for computing the cross-client "today" sort (a single query vs. per-client loop) — purely a performance/implementation choice.
- Exact route placement under `/v1/system/` and whether `requireOperator` needs any adjustment — the existing middleware (`internal/api/system_presets_handlers.go:47-67`) should be reusable as-is; confirm during planning.

### Reviewed Todos (not folded)
- **"Fix Retry-After midnight race in daily quota rejection"** (`.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md`) — matched this phase by keyword (after/daily/quota/jobs, score 0.6) but is a distinct bug in `POST /v1/jobs`'s 429 response path (`quotaRejection`/`secondsUntilUTCMidnight` in `internal/api/handlers.go`), unrelated to this phase's read-only operator endpoint. Deliberately NOT folded — stays its own pending todo, to be picked up separately when its sentinel-error-contract redesign is scoped.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements and roadmap
- `.planning/REQUIREMENTS.md` §"Operator Visibility" (lines 31-34) — OPSV-01/OPSV-02 literal text
- `.planning/ROADMAP.md` §"Phase 53: Operator Usage Visibility" — goal, 3 success criteria (including the locked no-leak-404 requirement), dependency on Phase 51

### Access control precedent (reuse, don't reinvent)
- `internal/api/system_presets_handlers.go:16-42` — `ParseOperatorClientIDs`, the `OPERATOR_CLIENT_IDS` env parser
- `internal/api/system_presets_handlers.go:47-67` — `requireOperator` middleware: fail-closed empty allowlist, byte-identical no-leak 404 for non-operators
- `internal/api/routes.go:58-64` — how `/v1/system/...` routes are gated by `requireOperator` today (the `/v1/system/presets` precedent)

### Data sources this phase reads (does not write)
- `internal/db/migrations/0011_client_usage_daily.sql` — the terminal-usage rollup table OPSV-01 and D-05's terminal half read: `(client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)`, `status IN ('done','failed')`, `canceled` deliberately excluded
- `internal/db/migrations/0013_client_limits.sql` — the per-`(client_id, engine)` limits table OPSV-02 reads: `max_concurrent`, `max_daily_bytes`, `max_daily_jobs`, all nullable meaning unlimited (never a sentinel)
- `internal/jobs/admission.go` `evaluate()` — the exact terminal+in-flight formula D-05 mirrors; the in-flight predicate (`status IN ('queued','active')`) and its literal-status SQL form for index applicability (`jobs_client_engine_inflight_idx`)
- `internal/clients/limits.go` — `ListLimits` and related repo methods `cmd/manage-clients show-limits` already uses; likely reusable as-is for the limits half of the combined response

### Existing patterns to mirror
- `internal/api/jobs_list_handler.go:16-23,36-42` — the fail-closed query-param validation idiom (`?status`/`?engine` against an allow-set, 422 before any DB work) that D-03's `?period` validation should follow
- `cmd/manage-clients/main.go:178-197` (`show-limits` case) — the CLI precedent for displaying limits with NULL=unlimited; note it currently shows ONLY configured limits, no usage/remaining — this phase's HTTP endpoint is new work, not a port of that command

### Phase 52 decisions this phase depends on (read for full context, not re-decided here)
- `.planning/phases/52-quota-enforcement-intra-class-fairness/52-CONTEXT.md` — D-06 (queued+active slot semantics), D-15 as amended (daily cap in-flight accounting)
- `.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md` — the deferred, NOT-folded todo noted above; read only to understand why it's excluded, not as scope for this phase

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `requireOperator` middleware (`system_presets_handlers.go:47-67`) — apply directly to the new route; no changes needed
- `internal/clients` repo's `ListLimits`-style methods — the limits half of the response can likely call these largely as-is
- `writeJSON`/`writeError` helpers already used throughout `internal/api` — follow existing response-writing conventions, don't invent new ones

### Established Patterns
- Fail-closed query-param validation against an allow-set, 422 before DB work (`handleListJobs`) — apply to `?period`
- NULL-means-unlimited display convention, never a sentinel number (`formatOptionalInt`/`formatOptionalInt64` in `cmd/manage-clients/main.go`) — the JSON equivalent should preserve this distinction
- Operator-gated `/v1/system/...` subtree with byte-identical no-leak 404 (`system_presets_handlers.go`, `routes.go:58-64`)

### Integration Points
- New route added under the existing `/v1/system/...` chi sub-router, gated by the existing `requireOperator` middleware — no new auth mechanism
- Reads `client_usage_daily` (Phase 51) and `client_limits` (Phase 52) — no schema changes anticipated; this phase is read-only against existing tables
- The in-flight (`queued`/`active`) count needs its own query against `jobs`, following the same literal-status SQL form `internal/jobs/admission.go`'s `evaluate()` uses so `jobs_client_engine_inflight_idx` stays applicable — but this read does NOT need to run under `admitLocked`'s advisory lock (that lock exists to serialize admission decisions; a read-only visibility query tolerates a benign race with an in-flight admission, unlike the admission gate itself)

</code_context>

<specifics>
## Specific Ideas

No specific UI/format examples given — this is a pure JSON API phase (no frontend work implied by OPSV-01/02's text or ROADMAP).

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

### Reviewed Todos (not folded)
See `<decisions>` § "Reviewed Todos (not folded)" above — the Retry-After midnight-race todo was reviewed and deliberately excluded.

</deferred>

---

*Phase: 53-operator-usage-visibility*
*Context gathered: 2026-08-05*
