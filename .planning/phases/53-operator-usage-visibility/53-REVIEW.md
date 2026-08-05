---
phase: 53-operator-usage-visibility
reviewed: 2026-08-05T14:22:24Z
depth: standard
files_reviewed: 16
files_reviewed_list:
  - internal/jobs/usage_query.go
  - internal/jobs/usage_query_test.go
  - internal/clients/limits.go
  - internal/clients/limits_test.go
  - internal/api/api.go
  - internal/api/system_usage_handlers.go
  - internal/api/system_usage_handlers_test.go
  - internal/api/routes.go
  - cmd/api/main.go
  - docs/openapi.yaml
  - internal/api/handlers_test.go
  - internal/api/presets_handlers_test.go
  - internal/api/routes_test.go
  - internal/api/sse_test.go
  - internal/api/system_presets_handlers_test.go
  - internal/mcpserver/quota_admission_test.go
findings:
  critical: 1
  warning: 8
  info: 7
  total: 16
status: issues_found
---

# Phase 53: Code Review Report

**Reviewed:** 2026-08-05T14:22:24Z
**Depth:** standard
**Files Reviewed:** 16
**Status:** issues_found

## Summary

`GET /v1/system/usage` (OPSV-01/OPSV-02) was reviewed adversarially across the HTTP layer
(`system_usage_handlers.go`, `routes.go`, `api.go`), the two new repo reads
(`jobs.UsageSummary`, `clients.ListAllLimits`), the OpenAPI contract, and the mechanical
`NewServer` signature widening.

**What holds up under attack:**

- **Authorization is correctly mounted.** `r.Route("/system/usage", …)` installs
  `s.requireOperator` on a dedicated chi sub-router (`routes.go:77-80`), so the gate runs
  before routing inside that subtree — including for non-GET methods, which therefore get the
  no-leak 404 rather than a 405 that would confirm the path exists. Non-operator responses are
  byte-identical to `/v1/system/presets` and cost zero repo calls (proven by the tests, and
  re-verified by reading the chain).
- **No SQL injection surface.** Every value in both new query files is a bound `$n` parameter;
  the only string literals in the SQL are the two hard-coded statuses. `SetEnforcementMode`
  validates in Go before binding.
- **No timezone bug in the day-range bind.** I empirically verified against the live Postgres
  that `$1::date` resolves the parameter type to `date` (not `timestamptz`), so
  `WHERE day BETWEEN $1::date AND $2::date` is *not* sensitive to the session `TimeZone`
  (unlike `$1::timestamptz::date`, which I confirmed shifts by a day under
  `America/New_York`). The RESEARCH Pitfall 4 concern is genuinely closed at the bind layer.
- **The `remaining` arithmetic matches `evaluate()`'s inequalities.** `>=` on concurrency,
  `bytesUsed + inputBytes > max`, and `jobsUsed + 1 > max` all map correctly onto the
  reported `max - used` figures, including the `max_concurrent = 0` deny case. `bytes_out` is
  correctly excluded from the byte budget.
- `go build ./...`, `go vet ./...`, `gofmt -l .` are clean; the unit suites and the
  `DATABASE_URL`-gated `TestUsageSummary*` integration suite pass against the live dev DB.

**Where it fails:** the *merge* step in the handler, not the SQL. The endpoint silently drops
every client that has neither a `client_limits` row nor usage in the window — which, given
that the entire fleet defaults to unlimited/observe (ROADMAP SC1), is the majority of clients
in a fresh deployment (CR-01). Beyond that, three of the endpoint's own truth claims are
overstated: `remaining_matches_admission_gate` ignores both `observe` mode and the
service-wide `QUOTA_ENFORCEMENT_DISABLED` kill-switch (WR-02); "today" is resolved from the
API process clock while `evaluate()` resolves it from Postgres `now()` (WR-03); and a
mistyped `client_limits.engine` is reported as an enforceable budget that the gate will never
apply (WR-06). Each of these is precisely the "operator-reported numbers disagree with
enforcement" failure D-05 exists to prevent.

## Critical Issues

### CR-01: Clients with no limits row and no usage in the window are silently dropped from the response

**File:** `internal/api/system_usage_handlers.go:177-204`
(supporting contract: `internal/clients/limits.go:112-123`)

**Issue:**
`ListAllLimits` deliberately `LEFT JOIN`s `clients -> client_limits` so that a client with no
limits row still surfaces as a metadata-only row with `Engine == nil`. Its doc comment states
this explicitly and in capitals:

> `Engine` is a `*string`, nil when the client has no client_limits row at all — that row still
> carries valid ClientID, ClientName and EnforcementMode and **MUST NOT be discarded by the
> caller**, because "this client has no limits configured" is exactly the answer OPSV-02 asks
> for.

The handler discards it. A nil-`Engine` row contributes only to the `meta` map (line 182); it
never enters `limitsByKey` (line 183), and `keySet` is built solely from `usageByKey ∪
limitsByKey` (lines 198-204). Therefore a client that has (a) no `client_limits` row and
(b) no `client_usage_daily` rows in the period and (c) no in-flight jobs produces **no output
row at all** — its name, its `enforcement_mode`, and the fact that it is unlimited are all
invisible.

Why this is a blocker rather than a cosmetic gap:

1. **It is the default state of the whole fleet.** ROADMAP SC1 pins that an already-provisioned
   client has no `client_limits` row and `enforcement_mode = 'observe'`. On a fresh deployment
   *every* client is in this state, so `GET /v1/system/usage` reports only whichever clients
   happened to run a job today and is silent about the rest.
2. **It creates a false-negative for the narrowed shape.** `?client_id=<a real, idle,
   unlimited client>` returns `{"usage":[]}` — byte-identical to `?client_id=<a UUID that
   matches no client at all>`. An operator cannot distinguish "this client exists and is
   unlimited" from "no such client", which is literally the question OPSV-02 poses.
3. **It is untested.** `TestSystemUsage_ClientWithUsageButNoLimitsAppears`
   (`system_usage_handlers_test.go:560`) covers the nil-`Engine` row *only* when the client
   also has usage, so the merge's zero-usage path is unexercised. There is no test for a valid
   `?client_id` that yields an empty result.

**Fix:** seed the key set from every client the metadata carries, emitting a synthetic
all-unlimited row when a client contributes no (client, engine) key at all. Keep the engine
label explicit rather than fabricating one:

```go
// keySet as today: usage keys ∪ configured-limit keys.
// PLUS: any client that produced neither must still surface as an
// "exists, unlimited, idle" row -- LimitWithMeta's contract (D-02/OPSV-02).
clientsWithRows := make(map[uuid.UUID]struct{}, len(keySet))
for k := range keySet {
    clientsWithRows[k.clientID] = struct{}{}
}
for id := range meta {
    if _, ok := clientsWithRows[id]; !ok {
        keySet[usageMergeKey{clientID: id, engine: ""}] = struct{}{}
    }
}
```

with the response documenting `engine: ""` as "no engine activity or configuration for this
client". Whatever shape is chosen, add two tests: (a) `?client_id=<idle unlimited client>`
returns exactly one row carrying that client's `client_name`/`enforcement_mode` and all-null
limits; (b) `?client_id=<nonexistent uuid>` returns `usage: []`, so the two cases are provably
distinguishable.

## Warnings

### WR-01: Orphaned in-flight jobs collapse into a phantom nil-UUID "client" row

**File:** `internal/jobs/usage_query.go:95-119, 186-194, 205-213`

**Issue:** `jobs.client_id` is **nullable** with `ON DELETE SET NULL`
(`internal/db/migrations/0001_init.sql:43`; confirmed live via `\d jobs`). Both in-flight
statements `GROUP BY client_id` with no `client_id IS NOT NULL` guard, so every job whose
client row was deleted while the job was still queued/active forms a single `client_id = NULL`
group. pgx does **not** error on this — I verified against the live database that scanning
`NULL::uuid` into a non-pointer `uuid.UUID` succeeds and silently yields the nil UUID:

```
scan NULL::uuid -> value=00000000-0000-0000-0000-000000000000 err=<nil>
  row: client_id=00000000-0000-0000-0000-000000000000 engine=image count=1
```

The result is an output row attributed to a client that does not exist, aggregating orphans
from *all* deleted clients into one bucket, and carrying `client_name: ""` and
`enforcement_mode: ""`. The latter violates the schema this phase just published:
`docs/openapi.yaml` declares `enforcement_mode` as `enum: [observe, enforce]` and lists it in
`required`. The handler's defensive comment at
`system_usage_handlers.go:211-216` anticipates a missing metadata entry but reasons about it
as "still emit the row" — it does not anticipate that the *identity* itself is fabricated.

Note the asymmetry: `($1::uuid IS NULL OR client_id = $1)` excludes NULLs when a `?client_id`
is supplied, so this only manifests on the default all-clients shape (D-01's primary shape).
`client_usage_daily.client_id` is `NOT NULL` + `ON DELETE CASCADE`, so the terminal term is
unaffected.

**Fix:** exclude orphans at the source, and say why:

```sql
-- usageInFlightJobsSQL / usageInFlightBytesSQL
WHERE status IN ('queued', 'active')
    AND client_id IS NOT NULL   -- jobs.client_id is ON DELETE SET NULL; a NULL
                                -- group would scan as the nil UUID and surface
                                -- as a phantom client in the operator report
    AND ($1::uuid IS NULL OR client_id = $1)
```

Add a `DATABASE_URL`-gated test that inserts a `client_id = NULL` queued job inside a
rolled-back transaction and asserts `UsageSummary(nil, …)` returns no `uuid.Nil` row.

### WR-02: `remaining_matches_admission_gate` ignores observe mode and the service-wide kill-switch

**File:** `internal/api/system_usage_handlers.go:285-294`

**Issue:** `RemainingMatchesAdmissionGate` is set solely from `period == "today"`, and the
accompanying note asserts *"remaining is exactly what the admission gate (evaluate()) computes
right now."* That is false in two reachable configurations:

1. **Per-client observe mode.** `admitLocked` (`admission.go:421-427`) computes the verdict and
   then **admits anyway** when `st.enforcementMode == enforcementModeObserve` — which is the
   column DEFAULT for every provisioned client. A row can report `remaining.daily_jobs: -40`
   while the gate is rejecting nothing.
2. **The global kill-switch.** `QUOTA_ENFORCEMENT_DISABLED` (`cmd/api/main.go:131,233,235` →
   `jobsRepo.SetQuotaEnforcementDisabled`) disables enforcement service-wide, overriding every
   per-client mode. This state is **completely invisible** in the response: there is no
   response field for it, and `enforcement_mode` still reports `enforce` per row.

The per-row `enforcement_mode` field lets a careful operator infer case (1); nothing lets them
infer case (2). Given that the whole purpose of this endpoint is to stop operator-reported
numbers from disagreeing with enforcement, an unconditional "this is what the gate will do"
claim is the wrong default.

**Fix:** make the claim conditional on enforcement actually being live, per row plus a
top-level flag:

```go
// Server gains: quotaEnforcementDisabled bool (wired from cmd/api/main.go
// alongside jobsRepo.SetQuotaEnforcementDisabled).
resp.QuotaEnforcementDisabled = s.quotaEnforcementDisabled
resp.RemainingMatchesAdmissionGate = period == "today" && !s.quotaEnforcementDisabled
```

and narrow the note text to say that a row whose `enforcement_mode` is `observe` has its
verdict computed and reported but never enforced. Update `docs/openapi.yaml` accordingly.

### WR-03: "today" is resolved from the API process clock, `evaluate()` resolves it from Postgres

**File:** `internal/api/system_usage_handlers.go:161` (`time.Now()`), compared with
`internal/jobs/admission.go:288,292` (`(now() AT TIME ZONE 'utc')::date`)

**Issue:** `resolveUsagePeriod(period, time.Now())` derives `dayFrom`/`dayTo` from the **API
host's** wall clock; `evaluate()`'s terminal term derives its day from the **database server's**
`now()`. These are two independent clocks. Around a UTC-midnight crossing — or under ordinary
NTP skew between the two hosts — the endpoint queries `day BETWEEN D AND D` while the gate
filters `day = D+1` (or vice versa), so the reported `used_terminal` and every `remaining`
figure describe a different day than the one the gate will enforce, while the response still
asserts `remaining_matches_admission_gate: true`. This is exactly the D-05 drift the file's own
header comment claims to have closed.

It also propagates into the tests: `TestUsageSummaryMirrorsEvaluateDailyBytesBoundary`
(`usage_query_test.go:97`) compares a Go-derived `utcToday()` against a Postgres-derived day
and would flake across the same boundary.

**Fix:** derive the day server-side so both sides read one clock. Either resolve the period
inside the SQL, or add a trivial repo read used by the handler:

```go
// jobs.Repo
func (r *Repo) UTCToday(ctx context.Context) (time.Time, error) {
    var d time.Time
    err := r.pool.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'utc')::date`).Scan(&d)
    return d, err
}
```

and pass that instant into `resolveUsagePeriod` instead of `time.Now()`. If the extra round
trip is unwanted, at minimum stop asserting `remaining_matches_admission_gate: true`
unconditionally and document the skew bound in `remaining_note`.

### WR-04: `validUsagePeriods` and `resolveUsagePeriod`'s switch can drift silently

**File:** `internal/api/system_usage_handlers.go:32-56`

**Issue:** The fail-closed allow-set (`validUsagePeriods`) and the period→range mapping
(`resolveUsagePeriod`'s `switch`) are two independent lists with no structural link, and the
`switch` has **no explicit `"today"` case** — it falls through to `default`. Adding a period to
the map without adding a `case` produces a silent wrong answer rather than a failure: the
handler validates the value, `resolveUsagePeriod` quietly returns today's single-day range, and
the envelope echoes `"period": "<the new value>"` with `day_from == day_to == today`. An
operator asking for a 90-day window would get today's numbers labelled `90d`.

**Fix:** collapse the two into one table so they cannot drift:

```go
// validUsagePeriods maps each accepted ?period value to its inclusive
// day-span offset (days back from today). Adding an entry here is the ONLY
// place a new period is defined -- no second switch to keep in sync.
var validUsagePeriods = map[string]int{"today": 0, "7d": 6, "30d": 29}

func resolveUsagePeriod(period string, now time.Time) (time.Time, time.Time) {
    now = now.UTC()
    today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
    back, ok := validUsagePeriods[period]
    if !ok {
        // unreachable: the handler rejects 422 first. Fail closed anyway.
        back = 0
    }
    return today.AddDate(0, 0, -back), today
}
```

### WR-05: Unbounded all-clients response with no pagination or ceiling

**File:** `internal/api/system_usage_handlers.go:163-173`;
`internal/clients/limits.go:143-151`; `internal/jobs/usage_query.go:147`

**Issue:** With `?client_id` omitted (D-01's primary shape), all three `UsageSummary`
statements and `ListAllLimits` are unbounded — no `LIMIT`, no cursor, no cap on the emitted
`usage` array. The handler materialises four maps plus a slice sized by *(clients × engines)*
and marshals the whole thing into one response. This is inconsistent with the deliberate
posture taken for `GET /v1/jobs`, where `jobsListDefaultPageSize`/`jobsListMaxPageSize`
(`api.go:190-195`) exist specifically as a DoS guard (D-06, T-40-06) and `?limit` is clamped
rather than rejected. An operator-only route is lower risk, but it sits behind the same
per-client rate limiter that a compromised or buggy operator key could saturate, and the
response size is bounded only by how many clients exist.

**Fix:** either add the same `?limit` clamp + keyset cursor used by `handleListJobs`, or —
if a single-shot report is intentional for this phase — push a hard server-side ceiling into
both repo reads (`LIMIT $n` on the ordered results) and document the truncation explicitly in
the envelope (e.g. `"truncated": true`) so a silently short report is impossible.

### WR-06: A mistyped `client_limits.engine` is reported as an enforceable budget the gate never applies

**File:** `internal/api/system_usage_handlers.go:235-261`

**Issue:** `client_limits.engine` is unvalidated free text — `0013_client_limits.sql:41-50`
deliberately omits a `CHECK (engine IN (…))` allow-list (a defensible storage-layer decision,
since a stale allow-list there would abort `INSERT INTO jobs`). The consequence at *this* layer
is that an operator who sets a limit on `engine = 'video'` (instead of `'av'`) or `'jpeg'`
(instead of `'image'`) gets a row in this report showing `max_daily_bytes`, `max_daily_jobs`
and a full untouched `remaining` — for an engine class the admission gate will never match,
because `evaluate()` looks up `client_limits` by the job's actual `jobs.engine` value. The
report says "this client is capped and has budget left"; the gate says "this client is
unlimited". That is the exact visibility/enforcement lie D-05 and this phase exist to prevent,
and it is precisely the misconfiguration an operator would use this endpoint to *find*.

There is no test and no signal field for it.

**Fix:** the endpoint already imports `internal/convert`-adjacent knowledge elsewhere in the
package; surface unknown engine strings rather than laundering them:

```go
// convert.Default already knows every registered engine class.
row.EngineRecognized = convert.IsKnownEngine(k.engine) // new field, default true
```

Emit `engine_recognized: false` (documented in `docs/openapi.yaml`) for any `client_limits`
engine that is not a live engine class, and add a test asserting a limits row on engine
`"video"` is reported with `engine_recognized: false`. Alternatively, validate the engine
string at the write site (`cmd/manage-clients`) — but the read side should still report,
because rows written before that validation existed will persist.

### WR-07: Two midnight-boundary test flakes

**File:** `internal/api/system_usage_handlers_test.go:294`;
`internal/jobs/usage_query_test.go:87-89`

**Issue:**
1. `TestSystemUsage_DefaultPeriodIsToday` issues the request, then computes
   `wantDay := time.Now().UTC().Format("2006-01-02")` *after* the response was produced. If
   the request resolves on day `D` and the assertion runs on `D+1`, the test fails
   spuriously.
2. `utcToday()` returns `time.Now().UTC().Truncate(24 * time.Hour)`. This happens to yield UTC
   midnight (the zero time is UTC-aligned), but it is a non-obvious property to depend on, and
   the value is compared against rows Postgres stamped with its own `(now() AT TIME ZONE
   'utc')::date` — so every `TestUsageSummary*` test inherits WR-03's two-clock race at the day
   boundary.

**Fix:** capture the expected day *before* the request in (1):

```go
wantDay := time.Now().UTC().Format("2006-01-02")
rec := httptest.NewRecorder()
srv.Routes().ServeHTTP(rec, usageRequest(""))
// ... and accept either wantDay or the day after, or freeze the clock.
```

and in (2) replace `Truncate` with the explicit, self-documenting form used by
`resolveUsagePeriod`:

```go
n := time.Now().UTC()
return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
```

Fixing WR-03 (server-derived day) removes the underlying race for both.

### WR-08: The "never nil after NewServer" contract for `usageRepo`/`limitsRepo` is documented but not enforced

**File:** `internal/api/api.go:144-149, 279-282`; counter-example at
`internal/mcpserver/quota_admission_test.go:372-374`

**Issue:** `Server`'s field comment claims *"Never nil after NewServer because they are
required positional params"*, and `NewServer`'s doc says *"a nil here is a programming error at
a call site, not a configurable state."* Being a positional parameter of an interface type
guarantees nothing — `nil` is a perfectly valid value for an interface. The codebase already
demonstrates this: `internal/mcpserver/quota_admission_test.go:373` passes `nil` for
`presetAdmin`, whose comment makes the identical claim. A `nil` `usageRepo` produces a nil
interface call inside `handleSystemUsage`, panicking on the first operator request and
surfacing as a `middleware.Recoverer` 500 with no indication of the cause.

**Fix:** enforce the invariant where it is asserted:

```go
func NewServer(..., usage UsageRepo, limits LimitsRepo, ...) *Server {
    if usage == nil || limits == nil {
        // Startup-time programming error -- fail loudly at construction,
        // not with a recovered panic on the first operator request.
        panic("api.NewServer: usage and limits repos are required")
    }
    ...
}
```

(Startup-time `panic` in a constructor is consistent with the codebase's `log.Fatalf`-at-init
posture and does not violate the "no panic for control flow" rule, which governs
request/task handling paths.)

## Info

### IN-01: `newUsageTestServer` returns a `*fakeResolver` no caller can use

**File:** `internal/api/system_usage_handlers_test.go:26-40`
**Issue:** The doc comment says the resolver is returned "so a caller can build `operatorIDs`
from `resolver.client.ID`" — impossible, because `operatorIDs` is a *parameter* consumed before
the resolver is constructed. All three call sites (lines 70, 98, 123) discard it as `srv, _`.
Dead return value plus a self-contradicting comment.
**Fix:** drop the second return value and the comment clause.

### IN-02: `newOperatorUsageTestServer` duplicates `newUsageTestServer`'s body

**File:** `internal/api/system_usage_handlers_test.go:46-52`
**Issue:** The two helpers construct the same server with the same 10 arguments; the second
inlines the whole `NewServer` call instead of delegating. Any future signature change must be
applied twice in one file.
**Fix:** `newOperatorUsageTestServer` should build the resolver, then call
`newUsageTestServer(map[uuid.UUID]struct{}{resolver.client.ID: {}}, usage, limits)` — which
becomes trivial once the resolver is threaded through properly (see IN-01).

### IN-03: Step-number comments jump from 3 to 7

**File:** `internal/api/system_usage_handlers.go:158, 175`
**Issue:** Numbered comments run `// 1.`, `// 2.`, `// 3.`, then `// 7. Merge.` — steps 4-6
are dangling references to a plan document, not to anything in this file. A reader looking for
the missing steps finds nothing.
**Fix:** renumber contiguously or drop the numbering and keep the prose.

### IN-04: `int64` → `int` narrowing in the concurrency remaining

**File:** `internal/api/system_usage_handlers.go:246`
**Issue:** `*lm.MaxConcurrent - int(ur.InFlightJobCount)` narrows an `int64` count to `int`.
Harmless on the 64-bit targets this service builds for, but it is the one place in the
`remaining` block where the two operand widths differ, and the surrounding code otherwise
keeps everything in `int64`.
**Fix:** compute in `int64` and expose `Remaining.Concurrent` as `*int64`, or add a short
comment pinning the 64-bit assumption.

### IN-05: Several tests index `resp.Usage[0]` without a length or status check

**File:** `internal/api/system_usage_handlers_test.go:462, 541, 580`
**Issue:** `TestSystemUsage_NegativeRemainingIsNotClamped` (line 462) decodes and immediately
indexes `resp.Usage[0]` without asserting `rec.Code == 200` or `len(resp.Usage) == 1`. A
regression that 500s or returns an empty array produces an index-out-of-range panic instead of
a readable failure message. Lines 541 and 580 do check `len`, so the pattern is inconsistent
within one file.
**Fix:** add the `rec.Code` and `len(resp.Usage)` guards uniformly, matching the style already
used at line 538.

### IN-06: `NewServer` now takes 10 positional parameters

**File:** `internal/api/api.go:282`; ~25 mechanical call-site edits across the test suite
**Issue:** The signature is now `(repo, storage, queue, presets, presetAdmin, usage, limits,
resolver, health, cfg)`, with three interfaces in a row (`presets`, `presetAdmin`, `usage`,
`limits`) that are easy to transpose at a call site and that the compiler cannot always catch —
`&fakeUsageRepo{}` and `&fakeLimitsRepo{}` are distinguishable, but a future pair of
same-shaped interfaces would not be. The Phase 53 diff is itself the evidence: widening by two
parameters required touching six unrelated test files.
**Fix:** consider a `Deps` struct alongside the existing `Config` for the next widening, so
adding a dependency is an additive field rather than a positional churn across every call
site.

### IN-07: Redundant `DATABASE_URL` skip and duplicated magic string

**File:** `internal/clients/limits_test.go:174-176`;
`internal/api/system_usage_handlers.go:32-33, 139, 285`
**Issue:** (a) `TestSetEnforcementMode` re-checks `os.Getenv("DATABASE_URL")` even though
`newTestRepo` (`internal/clients/repo_test.go:21-24`) already skips on it — every other test in
the file relies on the helper. (b) The literal `"today"` appears three times in the handler
(map key, default assignment, `RemainingMatchesAdmissionGate` comparison); the WR-04 fix would
naturally reduce this to one.
**Fix:** delete the redundant skip; hoist `"today"` into a `const periodToday = "today"`.

---

_Reviewed: 2026-08-05T14:22:24Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
