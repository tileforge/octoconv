# Phase 53: Operator Usage Visibility - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-05
**Phase:** 53-operator-usage-visibility
**Areas discussed:** Endpoint shape, Period granularity, Remaining-budget semantics, One endpoint or two

---

## Todo cross-reference

| Todo | Score | Reasons | Decision |
|------|-------|---------|----------|
| Fix Retry-After midnight race in daily quota rejection | 0.6 | keywords: after, daily, quota, jobs | Not folded — distinct bug in POST /v1/jobs's 429 path, unrelated to this read-only operator endpoint |

---

## Endpoint shape

| Option | Description | Selected |
|--------|-------------|----------|
| Both (recommended) | `GET /v1/system/usage` — no `?client_id=` lists across all clients; with `?client_id=` narrows to one | ✓ |
| Single-client only | `GET /v1/system/clients/{id}/usage` — operator must already know the ID | |
| List-all only | `GET /v1/system/usage` with no per-client lookup path | |

**User's choice:** Both, via one endpoint with an optional `?client_id=` filter.

**Follow-up — sort/pagination for the all-clients list:**

| Option | Description | Selected |
|--------|-------------|----------|
| Sort by consumption, no pagination (recommended) | Descending by period spend — answers "who consumed most" directly. Small internal client count (Phase 51 D-08's 200-client ceiling) makes pagination unnecessary | ✓ |
| Sort by client name, no pagination | Predictable but doesn't surface top consumers | |
| Keyset pagination like /v1/jobs | Mirrors handleListJobs; over-engineered for the expected small client count | |

**User's choice:** Sort by consumption, descending, no pagination.

---

## Period granularity (OPSV-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Fixed presets (recommended) | `?period=today\|7d\|30d`, fail-closed allow-set validation mirroring existing `?status`/`?engine` idiom | ✓ |
| Arbitrary date range | `?from=&to=` — more flexible but needs bounds/ordering validation and interaction with the 400-day retention window | |
| Both | Presets plus `?from=/?to=` fallback | |

**User's choice:** Fixed presets only.

**Follow-up — default period when `?period` is omitted:**

| Option | Description | Selected |
|--------|-------------|----------|
| today (recommended) | Matches the UTC-day boundary the admission gate itself uses — answers "what's happening right now" | ✓ |
| 7d | Smooths daily spikes, closer to a "weekly report" default | |
| Required, no default | Missing `?period` rejected 422 like an invalid filter | |

**User's choice:** `today`.

---

## "Remaining" semantics (OPSV-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror admission gate exactly (recommended) | Terminal usage (client_usage_daily) + in-flight (queued/active) — identical formula to evaluate() in admission.go post-gap-closure | ✓ |
| Terminal-only | Simpler, but can show remaining > 0 when the admission gate would already reject — the exact drift Phase 52's gap closure fixed internally | |

**User's choice:** Mirror the admission gate exactly.

**Notes:** This is the load-bearing decision of the phase — an operator-visible number that doesn't match enforcement reality invites limit decisions based on stale data. Directly connects to the still-open in-flight-time-bound gap noted in the code review of Phase 52's gap closure (a stuck job holds the daily quota indefinitely) — the response breakdown decision below makes that condition diagnosable rather than opaque.

**Follow-up — breakdown of "used" into terminal vs. in-flight:**

| Option | Description | Selected |
|--------|-------------|----------|
| Break down (recommended) | Response carries `used_terminal`, `used_in_flight`, and `remaining` separately — a stuck job showing `remaining: 0` is diagnosable at a glance | ✓ |
| Sum only | Response carries only `used` and `remaining` — simpler, but `remaining: 0` gives no clue whether the client is legitimately over-limit or stuck on a phantom job | |

**User's choice:** Break down into `used_terminal` / `used_in_flight`.

---

## One endpoint or two

| Option | Description | Selected |
|--------|-------------|----------|
| One combined (recommended) | GET /v1/system/usage returns both period-usage and current-limits+remaining in one response. Operator's question is always "what's going on with this client" — never two separate concerns. Avoids two responses disagreeing about which period they cover | ✓ |
| Two separate | /v1/system/usage + /v1/system/limits. Cleaner REST resource separation, but requires reconciling `?period` between two independent requests | |

**User's choice:** One combined endpoint.

---

## Claude's Discretion

- Exact JSON field names and response envelope shape (list wrapper vs. bare array)
- How NULL/unlimited limits are represented in JSON (likely `null`)
- Whether to include a machine-readable `enforcement_mode` per client
- Internal query shape for the cross-client sort (single query vs. per-client loop)
- Exact route placement under `/v1/system/` and any `requireOperator` adjustment needed (expected: none)

## Deferred Ideas

None raised outside phase scope during discussion. The Retry-After midnight-race todo was reviewed via the cross-reference step and deliberately not folded (see table above) — it remains its own pending todo, not a deferred idea from this discussion.
