---
phase: 53-operator-usage-visibility
plan: 02
subsystem: database
tags: [postgres, pgx, LEFT JOIN, operator-visibility, quotas]

# Dependency graph
requires:
  - phase: 52-quota-enforcement-intra-class-fairness
    provides: client_limits table (0013_client_limits.sql), clients.enforcement_mode column, loadQuotaState LEFT JOIN precedent
provides:
  - "internal/clients.LimitWithMeta type: operator-facing read shape joining client identity, enforcement_mode and per-engine limits"
  - "internal/clients.(*Repo).ListAllLimits(ctx, clientID *uuid.UUID): batch or single-client read, one query, one optional bind"
affects: [53-03-api-limits-repo-interface, 53-04-operator-limits-endpoint]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "LEFT JOIN clients -> client_limits, mirroring loadQuotaState (internal/jobs/admission.go), so a client with no limits row still appears with a nil Engine rather than being dropped"
    - "Optional narrowing via a single bound predicate ($1::uuid IS NULL OR c.id = $1), never a branching two-code-path implementation (D-01)"

key-files:
  created: []
  modified:
    - internal/clients/limits.go
    - internal/clients/limits_test.go

key-decisions:
  - "Return a non-nil empty slice ([]LimitWithMeta{}) from ListAllLimits rather than a nil slice, matching the plan's explicit instruction (differs from the existing ListLimits, which returns nil on empty)."

patterns-established:
  - "Batch operator-facing reads over per-client tables reuse the LEFT JOIN clients -> <per-client-table> shape from loadQuotaState, with an optional single-bind narrowing predicate instead of two code paths."

requirements-completed: [OPSV-02]

# Metrics
duration: 6min
completed: 2026-08-05
---

# Phase 53 Plan 02: Operator-Facing Batch Limits Read Summary

**Added `internal/clients.ListAllLimits`, a single LEFT-JOIN query returning every client's identity, enforcement mode and per-engine limits (or one client's, via an optional bound narrowing predicate), proven against a real Postgres with five integration tests including a manual mutation check.**

## Performance

- **Duration:** 6 min
- **Started:** 2026-08-05T13:17:00Z
- **Completed:** 2026-08-05T13:23:53Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- `LimitWithMeta` type and `(*Repo).ListAllLimits` added to `internal/clients/limits.go`, purely additive (zero lines removed, confirmed by `git diff -U0 | grep -c '^-[^-]'` = 0)
- Single statement `clients c LEFT JOIN client_limits l` with `($1::uuid IS NULL OR c.id = $1)` narrowing — one code path for both "all clients" and "one client" (D-01)
- Five DATABASE_URL-gated tests pass against a live Postgres, proving: absent-row clients still appear with `Engine == nil`, one row per configured engine in engine order, `nil` vs. explicit `0` stay distinguishable, `enforcement_mode` travels with the limits, and the narrowed result is a field-identical strict subset of the unnarrowed result
- Manually ran the plan's prescribed mutation check (`LEFT JOIN` → `JOIN`) and confirmed `TestListAllLimitsIncludesClientWithNoLimitsRow` fails as expected, then reverted

## Task Commits

Each task was committed atomically:

1. **Task 1: Add LimitWithMeta and (*Repo).ListAllLimits** - `e1817f4` (feat)
2. **Task 2: Prove the LEFT JOIN semantics, the narrowing bind, and NULL-vs-zero fidelity** - `5e9dc1f` (test)

**Plan metadata:** (this commit, docs)

## Files Created/Modified
- `internal/clients/limits.go` - Added `LimitWithMeta` struct and `(*Repo).ListAllLimits` method (additive only; `Limit`, `SetLimit`, `ClearLimit`, `GetLimit`, `ListLimits`, `SetEnforcementMode` untouched)
- `internal/clients/limits_test.go` - Appended 5 new tests plus a `filterByClient` test helper for narrowing shared-database assertions down to fixture clients

## Decisions Made
- `ListAllLimits` returns `[]LimitWithMeta{}` (non-nil empty slice) rather than `nil` on zero rows — an explicit instruction in the plan's `<action>` block, kept even though it diverges from `ListLimits`'s existing nil-on-empty convention, because the operator-facing batch endpoint (53-04) should be able to JSON-serialize an empty result as `[]` rather than `null` without an extra guard.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `internal/clients.LimitWithMeta` and `.ListAllLimits` are ready for plan 53-03 to wrap in an `api.LimitsRepo` interface, and for 53-04's operator handler to consume directly — field names and the method signature match the `<interfaces>` contract this plan declared, unchanged.
- No SELECT touches `api_key_hash` or any other credential column (verified: SELECT list is exactly `c.id, c.name, c.enforcement_mode, l.engine, l.max_concurrent, l.max_daily_bytes, l.max_daily_jobs`).
- No blockers.

---
*Phase: 53-operator-usage-visibility*
*Completed: 2026-08-05*
