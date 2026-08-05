---
phase: 53-operator-usage-visibility
reviewed: 2026-08-05T20:26:46Z
depth: standard
diff_base: 31a9457ffc5a444d340cae2eacce8a47c04719ed
scope: gap-closure work only (plans 53-05, 53-06, 53-07)
files_reviewed: 8
files_reviewed_list:
  - internal/api/system_usage_handlers.go
  - internal/api/system_usage_handlers_test.go
  - internal/api/api.go
  - internal/jobs/usage_query.go
  - internal/jobs/usage_query_test.go
  - cmd/api/main.go
  - docs/openapi.yaml
  - internal/api/handlers_test.go
findings:
  critical: 0
  warning: 4
  info: 7
  total: 11
status: issues_found
---

# Phase 53 (gap closure): Code Review Report

**Reviewed:** 2026-08-05T20:26:46Z
**Depth:** standard
**Diff base:** `31a9457` (the state already reviewed in `53-REVIEW.md`)
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Three fixes were reviewed adversarially: the CR-01 merge fix (53-05), the `client_id IS NOT NULL`
guards + `Repo.UTCToday` (53-06), and the enforcement-aware truth claim (53-07).

**The three fixes do what they claim.** I attempted to break each one and failed on the load-bearing
properties:

- **53-05 synthetic row.** The third seeding pass can never collide with or shadow a real
  `(client, engine)` row: `clientsWithRows` is derived from the *already-populated* `keySet`
  (`system_usage_handlers.go:255-263`), so a client that produced any usage or limits key is
  skipped entirely. A synthetic row's `hasLimit` is false, so `Limits`/`Remaining` stay at their
  all-nil zero values and no arithmetic runs on it; it contributes `0` to `consumption`, and the
  comparator `(consumption, ClientID string, Engine)` remains a total order, so `sort.Slice`'s
  non-stability still cannot matter. `engine: ""` marshals as a present JSON string, satisfying
  the OpenAPI `required` list.
- **53-06 guards.** Both in-flight statements carry the guard (`usage_query.go:106` and `:126`),
  and neither can change results on the narrowed path, because `client_id = $1` with a non-NULL
  bind already excludes NULLs. The terminal statement correctly needs no guard
  (`client_usage_daily.client_id` is `NOT NULL` + `ON DELETE CASCADE`). I empirically verified the
  documented failure mechanism rather than trusting the comment: with pgx v5.10.0, scanning a
  `NULL::uuid` into a `uuid.UUID` yields `err=<nil>, id=00000000-...`, so the pre-fix behaviour
  really was a silent phantom row, not a query error. `UTCToday`'s SQL is byte-identical to
  `evaluate()`'s day expression (`admission.go:288,292`) and its error wrapping matches the
  package convention (`fmt.Errorf("read utc today: %w", err)`).
- **53-07 ordering.** The critical property holds in the actual control flow, not only in a test:
  `?period` validation returns at `system_usage_handlers.go:166-169`, and the `UTCToday` read is at
  `:195`, strictly after. `?client_id` parsing (`:174-181`) is also before it. The operator gate is
  chi middleware (`routes.go:77-80`), so the non-operator path still short-circuits in
  `requireOperator` before the handler body executes at all — zero `UsageSummary`, zero
  `ListAllLimits`, zero `UTCToday`.
- **Query/enforcement divergence.** No drift: the terminal aggregate is still status-unfiltered,
  the in-flight predicates are still SQL literals, and adding `client_id IS NOT NULL` does not
  affect `jobs_client_engine_inflight_idx` (its partial predicate is on `status` only).
- **Arithmetic.** All types are signed (`int`/`int64`); no unsigned underflow is possible.
  nil-means-unlimited vs explicit-zero still round-trips correctly.
- **Tests.** The new tests are load-bearing, not tautological. Reverting the 53-05 pass fails
  `TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow`,
  `TestSystemUsage_IdleClientDistinguishableFromNonexistentClient` and
  `TestSystemUsage_AllClientsIncludesIdleUnlimitedClients`; reverting the 53-06 guards fails
  `TestUsageInFlightSQLExcludesNullClients` (source-shape, always-on) *and*
  `TestUsageSummaryExcludesOrphanedNullClientJobs` (real `ON DELETE SET NULL` path); reverting
  53-07's `&& !s.quotaEnforcementDisabled` fails
  `TestSystemUsage_KillSwitchMakesRemainingNotAuthoritative`, and reordering the semantics switch
  fails `TestSystemUsage_MultiDayCaveatWinsOverKillSwitch`. `go vet` is clean and the full jobs
  suite passes against live Postgres (`DATABASE_URL=...:5434`, 12/12 including the DB-gated ones).

**What is still wrong.** No blocker survived scrutiny, but four defects did:

1. 53-07 implemented only *half* of the prior review's own prescribed WR-02 fix. `53-REVIEW.md:210`
   asked for the claim to be conditional on live enforcement **"per row plus a top-level flag"**;
   only the top-level flag shipped. Because `enforcement_mode` defaults to `'observe'` for every
   provisioned client (`0013_client_limits.sql:66`), the default fleet posture now receives the
   *strongest* possible claim — `remaining_matches_admission_gate: true`,
   `remaining_semantics: "live_admission_gate"` — while the gate rejects nothing. (WR-01)
2. The exact contract violation 53-06's SQL guard was added to prevent — a row carrying
   `enforcement_mode: ""` against a `required` `enum: [observe, enforce]` — is still reachable
   through a second, unguarded path the fix did not consider. (WR-02)
3. 53-07 spent an extra round trip to make the *clock* exact while the five reads behind the
   response still run in five different snapshots, so `used_in_flight` can be internally
   inconsistent and the strengthened "exactly what the gate computes right now" claim outruns the
   read pattern. (WR-03)
4. The new 500 failure mode is absent from the OpenAPI contract that the same plan edited. (WR-04)

## Narrative Findings (AI reviewer)

_No `<structural_findings>` block was supplied for this run, so all findings below are narrative._

## Critical Issues

None. Every blocker hypothesis I formed (synthetic-row collision/shadowing, arithmetic
contamination from the synthetic row, guard asymmetry between the two in-flight statements, a repo
call preceding the 422, an authorization regression from the new data path, index-plan regression
from the added predicate) was traced and disproved — see Summary for the evidence.

## Warnings

### WR-01: `remaining_matches_admission_gate` still claims live enforcement for observe-mode rows — the prior WR-02 fix shipped only half of its own prescription

**Severity: WARNING**
**File:** `internal/api/system_usage_handlers.go:353`, `:363-365`
(contract: `docs/openapi.yaml:1010-1017`; DB default: `internal/db/migrations/0013_client_limits.sql:66`)

**Issue:**
The 53-05/06/07 wave was supposed to close `53-REVIEW.md`'s WR-02, which named **two** reachable
configurations where the claim is false: (1) per-client `observe` mode, and (2) the service-wide
kill-switch. Its prescribed fix (`53-REVIEW.md:210-211`) was explicit:

> **Fix:** make the claim conditional on enforcement actually being live, **per row plus a
> top-level flag**

Only the top-level flag was implemented:

```go
RemainingMatchesAdmissionGate: period == periodToday && !s.quotaEnforcementDisabled,
```

Case (1) got prose only. That matters more than it looks, because `enforcement_mode` is
`NOT NULL DEFAULT 'observe'` (`0013_client_limits.sql:66`) — the OpenAPI text itself calls
`observe` "the default for every provisioned client". So the *typical* deployment — kill-switch
off, every client at the default mode — emits the strongest possible claim
(`remaining_matches_admission_gate: true`, `remaining_semantics: "live_admission_gate"`) for a
fleet in which the admission gate rejects exactly zero requests. An operator dashboard keyed on
that boolean will report "budgets enforced" for a service that is enforcing nothing. The failure
mode is precisely the one D-05 and this whole endpoint exist to prevent, only inverted: the
numbers are right and the *claim about them* is wrong.

The response is not unrecoverable — a consumer can compute the truth from the per-row
`enforcement_mode` — which is why this is a WARNING and not a BLOCKER. But requiring every
consumer to re-derive it, while shipping a field whose name asserts the opposite, is the wrong
default for an operator-truth endpoint.

**Fix:** add the per-row half that was prescribed. One boolean on `systemUsageRow`, computed from
data already in hand:

```go
// systemUsageRow gains:
//   Enforced bool `json:"enforced"`
// ...and in the row-building loop:
row.Enforced = period == periodToday &&
    !s.quotaEnforcementDisabled &&
    cm.enforcementMode == "enforce" // clients.enforcement_mode, 0013_client_limits.sql:66-67
```

Add `enforced` to the row `properties`/`required` list in `docs/openapi.yaml:1108`, and narrow
`remaining_matches_admission_gate`'s description to "the response-level preconditions hold; see
each row's `enforced` for whether that row is actually enforced." A test asserting
`enforced == false` for an `observe` row while `remaining_matches_admission_gate == true` pins it.

---

### WR-02: a row with `enforcement_mode: ""` is still reachable — the exact contract violation 53-06's guard was added to prevent

**Severity: WARNING**
**File:** `internal/api/system_usage_handlers.go:270-282`
(contract: `docs/openapi.yaml:1062-1075` — `enum: [observe, enforce]`, and `:1108` — `required`)

**Issue:**
53-06's guard is justified, verbatim in both SQL constants (`usage_query.go:106`, `:126`), by:

> ...fabricating a phantom client row carrying `enforcement_mode: ""` — a value
> `docs/openapi.yaml` declares as `enum: [observe, enforce]` and required.

That is the right thing to prevent. But the handler retains a second, unguarded path to the same
output, and comments it as acceptable:

```go
// ...If it is somehow absent, still emit the row with empty
// name/enforcement_mode rather than dropping it, because dropping would hide
// real usage from the operator.
cm := meta[k.clientID]
```

The comment's stated justification — "every `client_usage_daily` row's `client_id` has a FK to
`clients`, `ON DELETE CASCADE`" — only establishes that the two reads are consistent *at a single
instant*. They are not taken at a single instant: `UsageSummary` runs at T1 and `ListAllLimits`
runs at T2 with no shared snapshot (`:202`, `:208`). A client deleted between T1 and T2 yields a
usage key with no `meta` entry, and the handler emits `client_name: ""` and `enforcement_mode: ""`
— a response that violates its own `required` + `enum` schema and would fail a contract test or a
strict client-side deserializer. The window is narrow, but 53-06 treated the identical output as
worth a schema-justified SQL guard; leaving the other door open is inconsistent, and the zero-value
map lookup makes it silent.

**Fix:** do not synthesize an invalid enum value. Either skip the metadata-less row (it can only
describe a client that no longer exists) or emit an explicitly out-of-band marker that the schema
declares:

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

Whichever branch is chosen, add a handler test with a `fakeUsageRepo` row whose `ClientID` is
absent from `fakeLimitsRepo` — that case has no coverage today.

---

### WR-03: the response is assembled from five unsynchronized snapshots, so `used_in_flight` can be internally inconsistent while the envelope claims exactness

**Severity: WARNING**
**File:** `internal/jobs/usage_query.go:169-227`; caller `internal/api/system_usage_handlers.go:195-212`

**Issue:**
`UsageSummary` issues three independent `pool.Query` calls, each in its own implicit transaction and
therefore its own snapshot; the handler adds `UTCToday` before them and `ListAllLimits` after —
five snapshots in total. `evaluate()`, by contrast, computes its terminal + in-flight terms in
**one** statement (`admission.go:288-303`), under an advisory lock.

Two observable consequences, both of which contradict the envelope's strengthened claim
(`"remaining is exactly what the admission gate (evaluate()) computes right now"`,
`system_usage_handlers.go:365`):

1. A job that reaches a terminal status between the terminal query (T1) and the in-flight count
   (T2) is counted in **neither** term — it was still `active` at T1 (so no `client_usage_daily`
   row) and no longer `active` at T2. The reported `used` is strictly lower than anything
   `evaluate()` would ever compute, and `remaining` is correspondingly too optimistic.
2. A job terminating between the in-flight *count* (T2) and the in-flight *bytes* (T3) produces a
   row with `used_in_flight.job_count: 1` and `used_in_flight.bytes_in: 0` — a self-inconsistent
   pair no single snapshot could produce.

Similarly, `UTCToday` (T0) and the terminal query (T1) can straddle UTC midnight, which reduces —
but does not eliminate — the WR-03 skew 53-07 set out to close. The package header does disclose
"a benign race with a concurrent admission (a figure that is a few milliseconds stale)", but 53-07
raised the response's own truth claim past the level the read pattern supports, and paid a whole
extra round trip for clock exactness while leaving snapshot exactness on the table.

**Fix:** put `UsageSummary`'s three statements (ideally `UTCToday` too) in one repeatable-read
transaction, which costs nothing extra on the wire versus the round trips already being made:

```go
return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
    func(tx pgx.Tx) error {
        // move the three Query calls onto tx — one snapshot, so the terminal and
        // in-flight terms can no longer disagree about a job that transitions
        // mid-request (evaluate() reads both terms in a single statement).
        ...
    })
```

If the transaction is judged too costly for an operator route, then soften the claim text instead —
the two must not disagree.

---

### WR-04: the new 500 failure mode is undocumented in the OpenAPI contract the same plan edited

**Severity: WARNING**
**File:** `docs/openapi.yaml:1110-1136` (handler paths: `internal/api/system_usage_handlers.go:197`, `:204`, `:210`)

**Issue:**
53-07 added a third 500 path — `writeError(w, http.StatusInternalServerError, "failed to resolve
current day")` — and a dedicated test for it
(`TestSystemUsage_UTCTodayErrorReturns500BeforeAnyRepoRead`), while editing this exact response
block for the new `quota_enforcement_disabled` field. The `responses:` map still documents only
`400/401/404/422/429`. A `500` is now reachable from a dependency that no other documented status
covers (the DB clock read), and the repo already has the reusable component for it
(`docs/openapi.yaml:287` `InternalError`, used at `:428`), so this is an omission rather than an
absent convention. Generated clients will treat a 500 here as an undeclared response.

**Fix:**

```yaml
        "422":
          description: Invalid period filter — not one of today/7d/30d.
          ...
        "500":
          $ref: "#/components/responses/InternalError"
        "429":
          $ref: "#/components/responses/TooManyRequests"
```

## Info

### IN-01: `engine: ""` is not provably unique to the synthetic row — no DB constraint and no defensive check back the new OpenAPI claim

**Severity: INFO**
**File:** `internal/api/system_usage_handlers.go:222-224`, `:386`; `docs/openapi.yaml:1054-1059`

**Issue:** The spec now states absolutely that an empty `engine` "means the client had no usage and
no configured limits". `client_limits.engine` is `text NOT NULL` with a *deliberately* absent
`CHECK (engine IN (...))` allow-list (`0013_client_limits.sql:18,41-51`), so an empty-string engine
is insertable at the DB level. Such a row would enter `limitsByKey` and be emitted as
`engine: ""` **with non-null limits** — a row that contradicts the documented meaning of its own
`engine` value, inside the same JSON object. Reachability today is low (`cmd/manage-clients`
validates via `convert.IsKnownEngine`, `main.go:123,241-245`), so this is defence-in-depth rather
than a live bug, but the endpoint has no guard of its own.

**Fix:** treat an empty engine as not-a-real-limit at the merge site, so the label stays unique to
the synthetic row: `if lm.Engine != nil && *lm.Engine != usageEngineNone { ... }`. Alternatively add
an explicit boolean row field rather than overloading a value in `engine`'s domain.

### IN-02: OpenAPI describes `enforcement_mode` as per-engine, but it is per-client

**Severity: INFO**
**File:** `docs/openapi.yaml:1064-1067`

**Issue:** "This client's configured enforcement mode **for this engine**" — the column is
`clients.enforcement_mode` (`0013_client_limits.sql:65-67`), a single per-client switch with no
per-engine dimension, and the handler copies the same `clientMeta.enforcementMode` onto every row
of that client (`system_usage_handlers.go:221,282`). An operator reading this could believe they
can set `observe` for one engine and `enforce` for another; no such capability exists.

**Fix:** "This client's configured enforcement mode (a single per-client switch; every engine row
for a client repeats the same value)."

### IN-03: the all-clients shape is now unbounded in the number of clients, with no pagination or cap

**Severity: INFO**
**File:** `internal/api/system_usage_handlers.go:259-263`; contract `docs/openapi.yaml:1039-1045`

**Issue:** Before 53-05 the un-narrowed response held only clients with usage or a configured
limit; it now emits at least one row for **every** row in `clients`, including long-idle or
key-revoked ones, with no `limit`/cursor (unlike `GET /v1/jobs`, which has D-06 page-size knobs).
This is the intended OPSV-02 semantics, and the route is operator-only and rate-limited, so it is
recorded rather than objected to — but the response size is now a function of the clients table,
and the `?client_id` narrowing is the only lever.

**Fix:** none required now; consider documenting the growth characteristic next to the `usage`
array description, or adding the same `?limit` knob `handleListJobs` already has if the client
count grows.

### IN-04: the ~340-character justification comment is duplicated verbatim inside both SQL constants and is transmitted to the server on every query

**Severity: INFO**
**File:** `internal/jobs/usage_query.go:106`, `:126`

**Issue:** The identical multi-hundred-character `--` comment lives inside two separate query
strings, so it is copy-paste duplicated (drift risk: an edit to one will not be applied to the
other) and is sent over the wire and parsed by Postgres on every operator request. The file's
existing convention places this kind of rationale in the Go doc comment above the constant, where
it costs nothing — and in fact this same rationale is *already* summarized there
(`usage_query.go:92-95`).

**Fix:** keep a one-line SQL comment (`-- ON DELETE SET NULL orphans; see doc comment above`) and
move the full paragraph into the Go doc comment that precedes each constant.

### IN-05: `newUsageTestServer`'s second return value is never used at any call site

**Severity: INFO**
**File:** `internal/api/system_usage_handlers_test.go:33-40` (call sites `:83`, `:114`, `:139`, `:1086`)

**Issue:** Every one of the four callers writes `srv, _ := newUsageTestServer(...)`. The
`*fakeResolver` return, and the doc comment explaining why it is returned, are dead.

**Fix:** drop the second return value and the paragraph justifying it, or use it (the
`newOperatorUsageTestServer` helper below already covers the "need the resolver's id" case).

### IN-06: no test pins the ordering guarantee on the 400 path, and none covers the metadata-less row

**Severity: INFO**
**File:** `internal/api/system_usage_handlers_test.go:202-219`

**Issue:** `TestSystemUsage_InvalidPeriodRejectedBeforeAnyRepoCall` asserts `usage.todayCalls == 0`,
but `TestSystemUsage_InvalidClientIDRejectedBeforeAnyRepoCall` — which exercises the *other*
pre-repo validation, and sits between the period check and the new `UTCToday` read — still asserts
only `usage.calls`/`limits.calls`. Moving the `UTCToday` read above the `client_id` parse would not
fail any test. Separately, the metadata-less row path (WR-02) has no coverage at all.

**Fix:** add `if usage.todayCalls != 0 { ... }` to the invalid-`client_id` test, mirroring the
invalid-`period` one, and add a `fakeUsageRepo` row whose `ClientID` is absent from
`fakeLimitsRepo`.

### IN-07: `handleSystemUsage` has grown to ~215 lines across 11 numbered steps

**Severity: INFO**
**File:** `internal/api/system_usage_handlers.go:156-370`

**Issue:** 53-05 and 53-07 both added logic to a handler that already carried validation, a
two-source merge, per-dimension arithmetic, a sort and envelope construction. The three-way merge
in particular (`:216-325`) is pure, deterministic and independently testable, but is currently only
reachable through an HTTP round trip in tests.

**Fix:** extract `mergeUsageRows(usageRows []jobs.UsageRow, limitRows []clients.LimitWithMeta)
([]systemUsageRow, map[uuid.UUID]int64)` alongside the already-extracted `resolveUsagePeriod` and
`sortSystemUsageRows`, and unit-test the merge cells (usage-only, limits-only, both, neither,
metadata-less) directly.

---

_Reviewed: 2026-08-05T20:26:46Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
