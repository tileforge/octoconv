---
phase: 52-quota-enforcement-intra-class-fairness
plan: 02
subsystem: observability
tags: [prometheus, http-429, mcp, error-handling]

# Dependency graph
requires:
  - phase: 51-attribution-accounting-retention
    provides: "octoconv_client_job_outcomes_total + the bounded clientLabel/defaultClientAllocator this plan reuses"
provides:
  - "RecordQuotaWouldReject(clientID, engine, reason) -- observe-mode metric plans 52-03/52-04 call for both dry-run and live rejections"
  - "code=\"rate_limited\" on the pre-existing rate-limit 429, establishing the {error, code} body contract plan 52-04's quota_exceeded 429 must match"
  - "extractErrorBody + widened apiError in internal/mcpserver/client.go, carrying code and a parsed Retry-After into MCP tool error text"
affects: [52-03, 52-04, 52-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "One CounterVec, many reasons: octoconv_quota_would_reject_total is pre-declared to be reused for both observe-mode and enforcement counting (D-14) -- no second metric appears when 52-04 lands"
    - "Three explicit apiError branches (neither / code-only / code+retry_after), each independently redacting the API key, rather than one combined branch -- makes D-12's redaction guarantee grep-verifiable per branch"

key-files:
  created: []
  modified:
    - internal/metrics/metrics.go
    - internal/metrics/metrics_test.go
    - internal/ratelimit/ratelimit.go
    - internal/ratelimit/ratelimit_test.go
    - internal/mcpserver/client.go
    - internal/mcpserver/client_test.go

key-decisions:
  - "quotaWouldReject.WithLabelValues always resolves clientID through the existing clientLabel/defaultClientAllocator -- never a second allocator instance -- so QUOT-04's observe-mode series and ACCT-04's cardinality bound share one admission map"
  - "apiError's Retry-After parsing only accepts the delta-seconds integer form, never HTTP-date -- matches every 429 producer in this codebase (internal/ratelimit today, quota rejections in 52-04)"
  - "extractErrorText removed outright rather than kept as a wrapper -- it had exactly one caller (apiError itself), so extractErrorBody replaces it cleanly with no duplicated decode logic"

requirements-completed: [QUOT-03, QUOT-04]

duration: ~15min
completed: 2026-08-03
---

# Phase 52 Plan 02: Quota Observability Surfaces Summary

**New `octoconv_quota_would_reject_total` counter (bounded by the existing client-label allocator), `code="rate_limited"` on the pre-existing rate-limit 429, and an MCP proxy hop that carries a 429's code + Retry-After into agent-visible tool error text — no enforcement logic anywhere in this plan.**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-08-03
- **Tasks:** 3/3
- **Files modified:** 6

## Accomplishments
- `RecordQuotaWouldReject(clientID, engine, reason)` in `internal/metrics/metrics.go`: a three-value `reason` taxonomy (`concurrency`/`daily_bytes`/`daily_jobs`) on a CounterVec that plan 52-04 will reuse verbatim for actual rejections — the observe-vs-enforce distinction lives in `clients.enforcement_mode` in Postgres, not in a metric label, so cardinality doesn't double when enforcement goes live.
- The only pre-existing 429 in the codebase (`internal/ratelimit`) now emits `code: "rate_limited"` alongside its byte-identical `error` message, establishing the `{error, code}` body contract plan 52-04's `quota_exceeded` 429 must match.
- `internal/mcpserver/client.go`'s `apiError` now surfaces `(code=<code>, retry_after=<n>s)` when both are present on a non-2xx response, `(code=<code>)` when only the code is present, and the message unchanged when neither is — so an MCP agent can tell "wait N seconds, same request will succeed" from "not allowed" (D-12), with the API key redacted on every one of the three branches.

## Task Commits

Each task was committed atomically:

1. **Task 1: octoconv_quota_would_reject_total, through the existing bounded allocator** - `4e9202b` (feat)
2. **Task 2: code="rate_limited" on the existing rate-limit 429** - `da1dc74` (feat)
3. **Task 3: MCP proxy carries code and Retry-After into the tool error** - `a33e108` (feat)

_Note: worktree mode — the plan-metadata commit (SUMMARY.md) is committed separately per the worktree execution protocol; STATE.md/ROADMAP.md are updated centrally by the orchestrator after merge, not by this agent._

## Files Created/Modified
- `internal/metrics/metrics.go` - `quotaWouldReject` CounterVec + `RecordQuotaWouldReject`, reusing `clientLabel`/`defaultClientAllocator`
- `internal/metrics/metrics_test.go` - `TestRecordQuotaWouldReject`, `TestQuotaWouldRejectUsesBoundedClientLabel`, `TestQuotaWouldRejectReasonLabels`
- `internal/ratelimit/ratelimit.go` - `limitHandler`'s JSON body gained `"code": "rate_limited"`; doc comment extended per D-10
- `internal/ratelimit/ratelimit_test.go` - asserts both `error` and `code` on a real 429, plus a positive-integer `Retry-After`
- `internal/mcpserver/client.go` - `extractErrorText` replaced by `extractErrorBody(body) (msg, code string)`; `apiError` rewritten into three explicit, independently-redacting branches; new `parseRetryAfterSeconds` helper
- `internal/mcpserver/client_test.go` - quota-vs-rate-limit distinguishability tests, a no-code-field regression test, and a key-leak test on the new branches

## Decisions Made
None beyond what's captured in the frontmatter `key-decisions` above — plan executed as specified, including its explicit interface contract (`RecordQuotaWouldReject` signature, the `{error, code}` 429 body shape).

## Deviations from Plan

None - plan executed exactly as written.

One clarification worth recording: the plan's acceptance criterion for Task 1 (`grep -c "newClientLabelAllocator(" internal/metrics/metrics.go` outputs 1) is imprecise — the pre-existing baseline already has 2 matches (the constructor's own definition plus `defaultClientAllocator`'s single call site), unchanged by this plan. Verified via `git show HEAD:internal/metrics/metrics.go` before this plan's commits: the count was already 2. The substantive requirement — no *second* allocator instance introduced by this plan — holds; `RecordQuotaWouldReject` calls `clientLabel`, never `newClientLabelAllocator`, directly.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Plans 52-03 and 52-04 can now call `RecordQuotaWouldReject` and rely on the `{error, code}` 429 body contract (both `rate_limited` and the forthcoming `quota_exceeded`) being correctly surfaced end-to-end through the MCP proxy. No enforcement decision, handler, repo, or migration was touched by this plan — verified by `git diff --stat` against `deploy/`, `internal/queue/`, and `internal/metrics/queue_collector.go` (all empty).

---
*Phase: 52-quota-enforcement-intra-class-fairness*
*Completed: 2026-08-03*

## Self-Check: PASSED

- FOUND: .planning/phases/52-quota-enforcement-intra-class-fairness/52-02-SUMMARY.md
- FOUND: 4e9202b (Task 1 commit)
- FOUND: da1dc74 (Task 2 commit)
- FOUND: a33e108 (Task 3 commit)
