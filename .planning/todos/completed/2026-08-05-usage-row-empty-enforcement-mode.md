---
created: 2026-08-05T23:45:00.000Z
title: GET /v1/system/usage can emit enforcement_mode:"" violating its own enum
area: api
source: .planning/phases/53-operator-usage-visibility/53-REVIEW-GAPS.md (WR-02)
files:
  - internal/api/system_usage_handlers.go:268-282 (cm := meta[k.clientID])
  - internal/api/system_usage_handlers.go:202, :208 (the two unsynchronized reads)
  - docs/openapi.yaml:1062-1075 (enum [observe, enforce]), :1108 (required)
  - internal/jobs/usage_query.go:106, :126 (the guards added for the same output shape)
---

## Problem

The handler builds each row's metadata with a zero-value map lookup:

```go
cm := meta[k.clientID]
```

commented as defensive-only, on the grounds that every `client_usage_daily`
row's `client_id` has an FK to `clients` with `ON DELETE CASCADE`. That
establishes the two reads are consistent *at a single instant* — but they are
not taken at a single instant. `UsageSummary` runs at T1
(`system_usage_handlers.go:202`) and `ListAllLimits` at T2 (`:208`), with no
shared transaction or snapshot.

A client deleted between T1 and T2 yields a usage key with no `meta` entry, and
the handler emits `client_name: ""` and `enforcement_mode: ""` — violating the
endpoint's own OpenAPI contract, which declares `enforcement_mode` as
`enum: [observe, enforce]` and `required`. A strict client-side deserializer or
a contract test would reject it.

The window is narrow. What makes it worth fixing is the inconsistency: plan
53-06 added `client_id IS NOT NULL` guards to two SQL statements specifically to
prevent this exact output shape, justified verbatim by the same OpenAPI enum.
Leaving the other door open — and silently, via a zero-value map lookup —
contradicts that reasoning.

Verified against the source: the two reads are indeed separate calls with no
shared snapshot.

## Fix

Do not synthesize an invalid enum value. Either skip the metadata-less row (it
can only describe a client that no longer exists) or emit an explicitly
out-of-band marker the schema declares:

```go
cm, haveMeta := meta[k.clientID]
if !haveMeta {
    // Client vanished between the UsageSummary and ListAllLimits reads
    // (they share no snapshot). Emitting enforcement_mode:"" here would
    // violate docs/openapi.yaml's own required enum — the same defect the
    // client_id IS NOT NULL guards (usage_query.go:106,126) exist to prevent.
    continue
}
```

Test: a handler test with a `fakeUsageRepo` row whose `ClientID` is absent from
`fakeLimitsRepo`. That case has no coverage today.
