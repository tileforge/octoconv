---
created: 2026-08-05T23:45:00.000Z
title: Add per-row `enforced` flag to GET /v1/system/usage
area: api
source: .planning/phases/53-operator-usage-visibility/53-REVIEW-GAPS.md (WR-01)
files:
  - internal/api/system_usage_handlers.go:353 (RemainingMatchesAdmissionGate)
  - internal/api/system_usage_handlers.go:363-365 (remaining_note)
  - docs/openapi.yaml:1010-1017, :1108
  - internal/db/migrations/0013_client_limits.sql:66 (enforcement_mode NOT NULL DEFAULT 'observe')
---

## Problem

Phase 53's WR-02 named two configurations where
`remaining_matches_admission_gate: true` is a false claim: per-client `observe`
mode, and the service-wide `QUOTA_ENFORCEMENT_DISABLED` kill-switch. Plan 53-07
closed only the kill-switch half:

```go
RemainingMatchesAdmissionGate: period == periodToday && !s.quotaEnforcementDisabled,
```

The observe-mode half got prose only — a caveat sentence in `remaining_note`.

That matters more than it looks because `clients.enforcement_mode` is
`NOT NULL DEFAULT 'observe'`. So the *typical* deployment — kill-switch off,
every client at the column default — emits the strongest possible claim
(`remaining_matches_admission_gate: true`, `remaining_semantics:
"live_admission_gate"`) for a fleet where the admission gate rejects exactly
zero requests. An operator dashboard keyed on that boolean reports "budgets
enforced" for a service enforcing nothing.

This is recoverable by the consumer — per-row `enforcement_mode` is present, so
the truth can be re-derived — which is why it is a warning and not a blocker.
But shipping a field whose name asserts the opposite of reality is the wrong
default for an operator-truth endpoint.

## Note on the disagreement

Phase 53's re-verification judged this "not a partial fix", on the grounds that
the original WR-02's *code sample* contained only the top-level flag and its
follow-up sentence explicitly routed observe-mode to the note text. That
reading is accurate as textual exegesis — the original review was internally
inconsistent, promising "per row plus a top-level flag" in prose while its
sample delivered only the flag. But judged against behavior rather than against
the review's own wording, the gap is real.

## Fix

One boolean on `systemUsageRow`, computed from data already in hand:

```go
// systemUsageRow gains:
//   Enforced bool `json:"enforced"`
row.Enforced = period == periodToday &&
    !s.quotaEnforcementDisabled &&
    cm.enforcementMode == "enforce"
```

Add `enforced` to the row `properties`/`required` in `docs/openapi.yaml`, and
narrow `remaining_matches_admission_gate`'s description to "the response-level
preconditions hold; see each row's `enforced` for whether that row is actually
enforced."

Test: assert `enforced == false` for an `observe` row while
`remaining_matches_admission_gate == true`.
