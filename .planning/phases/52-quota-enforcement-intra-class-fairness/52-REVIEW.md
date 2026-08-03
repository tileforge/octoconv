---
phase: 52-quota-enforcement-intra-class-fairness
reviewed: 2026-08-03T03:35:51Z
depth: standard
files_reviewed: 22
files_reviewed_list:
  - cmd/api/main.go
  - cmd/manage-clients/main.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/values.yaml
  - docs/openapi.yaml
  - internal/api/api.go
  - internal/api/handlers.go
  - internal/api/handlers_test.go
  - internal/api/sse_test.go
  - internal/clients/limits.go
  - internal/clients/limits_test.go
  - internal/db/migrations/0013_client_limits.sql
  - internal/jobs/admission.go
  - internal/jobs/admission_test.go
  - internal/jobs/repo.go
  - internal/mcpserver/client.go
  - internal/mcpserver/client_test.go
  - internal/mcpserver/quota_admission_test.go
  - internal/metrics/metrics.go
  - internal/metrics/metrics_test.go
  - internal/ratelimit/ratelimit.go
  - internal/ratelimit/ratelimit_test.go
findings:
  critical: 1
  warning: 12
  info: 4
  total: 17
status: issues_found
---

# Phase 52: Code Review Report

**Reviewed:** 2026-08-03T03:35:51Z
**Depth:** standard
**Files Reviewed:** 22
**Status:** issues_found

## Summary

The concurrency half of this phase is sound. `admitLocked` really does run inside
`Repo.Create`'s transaction, the advisory-lock key derivation is deterministic and
structurally incapable of colliding with the reconciler's one-bigint session lock
(PostgreSQL keys the one-arg and two-arg advisory forms in separate lock tags), the
observe/enforce split fires the observer exactly once on every rejected-or-would-be-
rejected path, and the kill-switch defaults fail-closed (`quotaDisabled` zero value is
"enforce per client"). `cmd/manage-clients` has no injection surface — every value is a
bound `$N` parameter. Quota sentinels map to 429 with `code=quota_exceeded`, never 403,
and never echo internal error text. `go build`, `go vet`, `gofmt -l` and the non-DB test
suites are clean.

The **daily** half is not sound. The daily byte/job caps read a rollup that is only
written at terminal transitions, so admitted-but-unfinished work is invisible to them.
`evaluate`'s own comment claims that overshoot "is bounded by the concurrency cap" — a
claim that is only true when `max_concurrent` is set, and `max_concurrent` is explicitly
independently nullable (`set-limit <id> image - 1000000000 100` is a documented, supported
configuration). In that configuration the daily caps are effectively unenforceable
(CR-01), and they can additionally be evaded by cancelling jobs (WR-01).

Secondary themes: the load-bearing post-lock re-check silently depends on READ COMMITTED
and never asserts it (WR-02); the occupancy `COUNT` parameterizes the two statuses in a
way that can defeat the partial index this very migration adds for it (WR-03); three
comments in the diff make claims the adjacent code contradicts (WR-04, WR-05, WR-10); and
the operator surface has two silent-failure modes (kill-switch spelling WR-07, unvalidated
engine name WR-08).

## Critical Issues

### CR-01: Daily byte/job quotas are effectively unenforceable when no concurrency cap is set

**File:** `internal/jobs/admission.go:205-240` (comment at `:206-221`), `internal/jobs/usage.go:43` (write site), `internal/db/migrations/0013_client_limits.sql:31-33`

**Issue:**
`evaluate` compares the request against `client_usage_daily`, which `recordUsage` writes
**only** at the terminal transition (`MarkDone`/`MarkFailed`). Every job that has been
admitted but has not yet finished contributes 0 bytes and 0 job-count to the check. The
in-code justification for accepting this states:

> This overshoot is accepted, and it is bounded by the concurrency cap: at most K
> in-flight jobs for this client+engine (K = max_concurrent) …

That bound does not exist when `max_concurrent` is NULL. The three limit columns are
independently nullable by design (`0013_client_limits.sql:20-33`, D-02) and the CLI
exposes exactly that shape:

```
manage-clients set-limit <id> image - 1000000000 100    # daily caps only, no K
```

With that configuration K is unbounded, so the "bound" degenerates to "however many jobs
the client can submit before any of them completes". This is not merely a concurrency
race — it is a sequential property too: a client with `max_daily_jobs = 100` can submit
1000 jobs back-to-back and every one of them reads `jobsUsed = 0`, because none of the
previous 999 has reached a terminal state yet. The advisory lock does not help: it
serializes the reads, but each serialized read still sees the same unchanged rollup.

Net effect: a supported, operator-reachable quota configuration silently enforces
essentially nothing, while `show-limits` reports the limit as in force and the 429
contract advertises that it is enforced. The unit test that "pins" this
(`TestOvershootBoundedByConcurrencyCap`, `admission_test.go:456`) only exercises the case
where `max_concurrent` **is** set, so it cannot catch this.

**Fix:** count in-flight work into the daily evaluation — it is safe to do so under the
advisory lock that is already held, and it needs no new table:

```go
// admission.go, inside evaluate, replace the daily read with one that also
// charges admitted-but-unfinished work for today.
if err := q.QueryRow(ctx, `
    SELECT
      COALESCE((SELECT sum(bytes_in)  FROM client_usage_daily
                WHERE client_id = $1 AND engine = $2
                  AND day = (now() AT TIME ZONE 'utc')::date), 0)
    + COALESCE((SELECT sum(ji.size_bytes) FROM jobs j
                JOIN job_inputs ji ON ji.job_id = j.id
                WHERE j.client_id = $1 AND j.engine = $2
                  AND j.status IN ('queued','active')
                  AND j.created_at >= (now() AT TIME ZONE 'utc')::date), 0),
      COALESCE((SELECT sum(job_count) FROM client_usage_daily
                WHERE client_id = $1 AND engine = $2
                  AND day = (now() AT TIME ZONE 'utc')::date), 0)
    + COALESCE((SELECT count(*) FROM jobs j
                WHERE j.client_id = $1 AND j.engine = $2
                  AND j.status IN ('queued','active')
                  AND j.created_at >= (now() AT TIME ZONE 'utc')::date), 0)`,
    clientID, engine,
).Scan(&bytesUsed, &jobsUsed); err != nil { ... }
```

(Note this also removes the double-count hazard only if the in-flight predicate and the
rollup predicate stay disjoint — `queued`/`active` are never in `client_usage_daily`, so
they are.) If that is rejected, the alternative minimum is to make the unbounded case
impossible instead of undocumented: reject `SetLimit` when a daily limit is set with
`MaxConcurrent == nil` (`internal/clients/limits.go:35`), and correct the comment at
`admission.go:206-221` to state the bound only holds under a concurrency cap.

## Warnings

### WR-01: Daily quotas can be reset to zero cost by cancelling jobs

**File:** `internal/jobs/repo.go:226-243`, `internal/jobs/admission.go:222-239`

**Issue:** `Cancel` deliberately does not call `recordUsage`, so a canceled job contributes
nothing to `client_usage_daily` — even though it already consumed the upload, an S3 object,
a queue slot, and (if a worker picked it up before the cancel landed) real engine CPU. That
was a defensible accounting decision in Phase 51 when the rollup was reporting-only; Phase
52 makes the same rollup the enforcement input, which turns it into a quota-evasion path:
`create → cancel → create → cancel …` consumes bandwidth, storage and worker time
indefinitely against a `max_daily_bytes`/`max_daily_jobs` cap that never advances. Clients
are internal and trusted, which is why this is a WARNING and not a BLOCKER, but the daily
cap does not mean what the 429 contract says it means.

**Fix:** either roll canceled jobs into `client_usage_daily` under a `'canceled'` status
row (the table is already keyed by status, `0011_client_usage_daily.sql`) and let
`evaluate` sum it like the others, or state explicitly in `admission.go` that a canceled
job is free and that daily caps therefore bound only completed work.

### WR-02: The load-bearing post-lock re-check silently depends on READ COMMITTED

**File:** `internal/jobs/repo.go:130`, `internal/jobs/admission.go:266-278`

**Issue:** `admitLocked` reads state, takes `pg_advisory_xact_lock`, then re-runs the
occupancy count *on the same transaction*. That re-count only observes commits made by
transactions that finished while this one waited on the lock **because the transaction is
READ COMMITTED and therefore takes a fresh snapshot per statement**. `pgx.BeginFunc`
(repo.go:130) inherits the server/DSN default isolation level; nothing in this repo pins
it. If `default_transaction_isolation` is ever set to `repeatable read` (a one-line DSN or
`postgresql.conf` change, e.g. `options=-c default_transaction_isolation=repeatable%20read`),
the post-lock count is served from the snapshot taken by `loadQuotaState` *before* the
lock, the cap is silently over-admitted, and nothing errors — no serialization failure,
because the concurrent inserts do not conflict. The single most important property of this
phase would fail silently on a config change made by someone who never reads this file.

**Fix:** pin it explicitly rather than inherit it:

```go
err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted},
    func(tx pgx.Tx) error { ... })
```

and add one line to `admitLocked`'s doc comment stating that the re-check requires a
per-statement snapshot.

### WR-03: The occupancy COUNT parameterizes the statuses, defeating the partial index the migration adds for it

**File:** `internal/jobs/admission.go:191-195`, `internal/db/migrations/0013_client_limits.sql:69-78`

**Issue:** The migration creates `jobs_client_engine_inflight_idx ON jobs (client_id, engine)
WHERE status IN ('queued','active')` and explains that without it the query would "hold the
lock for the duration of a scan of all in-flight rows across every client and engine". The
query it serves binds the statuses as parameters:

```sql
SELECT count(*) FROM jobs WHERE client_id = $1 AND engine = $2 AND status IN ($3, $4)
```

PostgreSQL can only use a partial index when it can prove the query predicate implies the
index predicate. That proof succeeds in a *custom* plan (parameters folded to constants) but
fails in a *generic* plan, and pgx v5's default `QueryExecModeCacheStatement` prepares and
caches this statement, so after ~5 executions the planner may switch to the generic plan
permanently and fall back to `jobs_client_created_idx` (or a seq scan) — scanning the
client's entire job history under the advisory lock, which is exactly the outcome the
migration comment says must not happen. `StatusQueued`/`StatusActive` are Go constants, not
client input, so there is no injection reason to bind them.

**Fix:**

```go
if err := q.QueryRow(ctx, `
    SELECT count(*) FROM jobs
    WHERE client_id = $1 AND engine = $2 AND status IN ('queued', 'active')`,
    clientID, engine,
).Scan(&count); err != nil { ... }
```

with a comment tying the literals to the index predicate in `0013_client_limits.sql:77-78`
(the migration already says "If the D-06 predicate ever changes, this index must change
with it" — the literals make that couple visible).

### WR-04: `admitLocked`'s comment contradicts what the code actually does with the pre-lock read

**File:** `internal/jobs/admission.go:271-275`

**Issue:** The comment states the pre-lock state read "is only used to decide whether locking
was worth it, never as the basis for the actual verdict." That is false: `st` is passed
straight into `evaluate` (`:275`) and supplies **the limit values themselves**, and `st.enforcementMode`
(`:287`) decides enforce-vs-observe. Only the counts are re-read post-lock. The consequence
is benign today (an operator changing a limit concurrently races), but the comment is
actively misleading in the one function whose concurrency reasoning a future maintainer will
trust verbatim.

**Fix:** reword to what is true — "the counts are re-read post-lock; the limits and
enforcement mode are the pre-lock values, which is acceptable because an operator limit
change racing an in-flight admission has no correctness consequence" — or re-run
`loadQuotaState` on `tx` after the lock if the stronger property is wanted.

### WR-05: Migration's "blast radius: additive only … jobs untouched" claim is false, and the index build blocks job creation at startup

**File:** `internal/db/migrations/0013_client_limits.sql:14-16` vs `:65-67` and `:77-78`

**Issue:** The header states "No existing table, column, constraint or row is modified; jobs,
job_events and client_usage_daily are untouched by this migration." Eight and sixty lines
later the same file does `ALTER TABLE clients ADD COLUMN …` (brief ACCESS EXCLUSIVE) and
`CREATE INDEX … ON jobs` (SHARE lock for the whole build, blocking every `INSERT INTO jobs`).
Because `db.Migrate` runs each migration inside one transaction (`internal/db/db.go:72-86`),
`CREATE INDEX CONCURRENTLY` is not available, and because every API replica runs `Migrate`
at startup (`cmd/api/main.go:54`), a rollout against a large `jobs` table stalls all job
creation for the duration of the index build. Related, pre-existing but newly exercised: two
replicas starting simultaneously both see the migration as unapplied and both attempt it;
the loser fails with `relation already exists` and `log.Fatalf`s into a crash-loop.

**Fix:** correct the blast-radius paragraph to name both DDL statements and their lock
levels; add an operator note (README/runbook) that this migration must be applied ahead of
the rollout on a non-trivial `jobs` table, or split the index into a separately-applied
`CREATE INDEX CONCURRENTLY` step outside the transactional runner.

### WR-06: The non-quota `PreAdmit` failure is logged without the error

**File:** `internal/api/handlers.go:436-443`

**Issue:**

```go
log.Printf("quota pre-admit check failed (continuing to authoritative check): client_id=%s engine=%s", client.ID, engine)
```

`err` is in scope and is discarded. The whole point of this branch is that the failure is
tolerated silently at the request level, which makes the log line the *only* signal that the
pre-upload gate is broken (DB down, statement error, context deadline). With no error text an
operator sees an unattributable line and cannot distinguish "Postgres unreachable" from "the
query is malformed". This also contradicts the project convention that errors are wrapped and
surfaced for diagnosability (CLAUDE.md, Error Handling).

**Fix:**

```go
log.Printf("quota pre-admit check failed (continuing to authoritative check): client_id=%s engine=%s err=%v", client.ID, engine, err)
```

### WR-07: The kill-switch silently ignores every spelling but exact `true`

**File:** `cmd/api/main.go:185-188`, `deploy/chart/octoconv/values.yaml:38-40`

**Issue:** `firstField(os.Getenv("QUOTA_ENFORCEMENT_DISABLED")) == "true"` accepts only the
exact lowercase literal. `True`, `TRUE`, `1`, `yes`, `on` all leave enforcement **on** with no
error and no log line — the warning at `:187` only prints when the switch actually engaged, so
absence of the warning is the only feedback, and nobody reads for an absent line. This is the
emergency lever an operator reaches for during an incident; failing closed is correct, failing
closed *silently on a plausible spelling* is not.

**Fix:** normalize and reject unknown values loudly:

```go
switch v := strings.ToLower(strings.TrimSpace(firstField(os.Getenv("QUOTA_ENFORCEMENT_DISABLED")))); v {
case "", "false":
    quotaDisabled = false
case "true":
    quotaDisabled = true
default:
    log.Fatalf("QUOTA_ENFORCEMENT_DISABLED=%q: must be \"true\" or \"false\"", v)
}
```

### WR-08: `set-limit` accepts any engine string, producing a limit that is silently never enforced

**File:** `cmd/manage-clients/main.go:120`, `internal/clients/limits.go:35-49`

**Issue:** `engine := os.Args[3]` is written straight through to `client_limits.engine` with no
validation. `manage-clients set-limit <id> images 5 - -` (note the typo) succeeds, prints "set
limit for client … engine images", and `show-limits` echoes it back — but `loadQuotaState`'s
`LEFT JOIN … ON l.engine = $2` never matches `image`, so the client is unlimited while the
operator believes a cap is in force. The migration deliberately (and correctly) refuses a DB
CHECK allow-list because a stale allow-list would break `INSERT INTO jobs`; that reasoning does
not apply to the operator CLI, which is the right place to catch a typo.

**Fix:** validate in the CLI against the registry that already knows the engine classes, e.g.

```go
if !convert.Default.KnownEngine(engine) { // or: slices.Contains(convert.AllEngines(), engine)
    log.Fatalf("unknown engine %q (known: %v)", engine, convert.AllEngines())
}
```

At minimum, print a loud warning instead of a success line, and apply the same check in
`clear-limit`.

### WR-09: `internal/reconciler`'s advisory-lock comment is now false

**File:** `internal/reconciler/reconciler.go:27-33` (made stale by `internal/jobs/admission.go:106-130`)

**Issue:** The reconciler still documents "there are none today (verified: zero other
`pg_try_advisory_lock` callers in this codebase). If a second advisory-lock use is ever added,
it MUST use a different key." This phase adds a second advisory-lock user. The two are in fact
safe (one-bigint vs two-int lock tags are distinct key spaces), but the invariant a future
reader will act on is now stated in two places with only one of them correct — and the
reconciler's version tells them to pick a different *key*, which is the wrong mental model
(the *form* is what separates them).

**Fix:** update the reconciler comment to point at `quotaLockKeys` and record the one-arg vs
two-arg separation, mirroring the (correct) explanation in `admission.go:116-123`.

### WR-10: The MCP client echoes the upstream-supplied `code` verbatim while claiming it cannot

**File:** `internal/mcpserver/client.go:443-471`

**Issue:** The doc comment asserts: "A malformed or missing Retry-After is simply omitted --
never injected verbatim -- so a hostile or buggy upstream can't smuggle arbitrary text into the
agent-visible error." `Retry-After` is indeed validated (`parseRetryAfterSeconds`), but `code`
is taken straight from the response body and interpolated unvalidated:

```go
msg = fmt.Sprintf("%s (code=%s, retry_after=%ds)", msg, code, retryAfter)
```

`code` is bounded only by `maxErrorBodyBytes` (4 KiB), and the resulting string is returned to
an LLM agent as tool-error text — a prompt-injection channel the comment explicitly claims is
closed. (The `error` field was already passed through verbatim before this change, so this is
not a new *class* of exposure; the stated guarantee is nonetheless wrong, and `code` is a
closed two-value enum in `docs/openapi.yaml:87-88`, so there is no reason to pass it through
unconstrained.)

**Fix:**

```go
switch code {
case "quota_exceeded", "rate_limited": // the closed set docs/openapi.yaml defines
default:
    code = "" // unknown code: fall back to the message-only branch
}
```

and correct the comment to say what is actually guaranteed.

### WR-11: `TestQuotaAdmission` subtests are order-coupled through shared mutable state

**File:** `internal/mcpserver/quota_admission_test.go:55-60, 103-143, 169-190`

**Issue:** `sharedClientID`/`sharedRawKey` are assigned in `RESTRejectsOverCap` and consumed by
`MCPToolRejectsOverSameCap` and `FairnessGuaranteeIsPerClientNotGlobal`. Running
`go test -run 'TestQuotaAdmission/MCPToolRejectsOverSameCap'` sends `Authorization: ApiKey `
(empty key) and fails with a 401-shaped error that looks nothing like the real defect;
`FairnessGuaranteeIsPerClientNotGlobal` silently degenerates into "a fresh client can create a
job" because no other client is holding a slot. A load-bearing proof test should not be
un-runnable in isolation.

**Fix:** provision the shared client in the harness (`newQuotaAdmissionHarness`) or in a
`t.Run`-independent helper invoked by each subtest that needs it, and have the fairness subtest
assert its precondition (`sharedClientID != uuid.Nil` and a queued job exists for it) rather
than assume it.

### WR-12: The advisory lock is acquired with no `lock_timeout`, so one stuck admission pins pool connections

**File:** `internal/jobs/admission.go:267`, `cmd/api/main.go:201-205`

**Issue:** `pg_advisory_xact_lock` blocks indefinitely, and the lock is held for the remainder
of `Create`'s transaction (three INSERTs). Every concurrent admission for the same
(client, engine) queues behind it, each holding a pgxpool connection and an API goroutine.
`http.Server` here sets only `ReadHeaderTimeout` — there is no `WriteTimeout` and no
per-request context deadline — so a single pathologically slow or hung transaction for one hot
client can drain the shared pool and take down admissions for *all* clients. That is the exact
opposite of the intra-class fairness this phase exists to provide.

**Fix:** bound the wait, and treat the timeout as a retryable rejection:

```go
if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil { ... }
if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, k1, k2); err != nil {
    return fmt.Errorf("quota admission: acquire advisory lock: %w", err)
}
```

(and/or give `handleCreateJob` a `context.WithTimeout` around the `repo.Create` call).

## Info

### IN-01: `evaluate` returns `(error, error)`

**File:** `internal/jobs/admission.go:188`

**Issue:** `func evaluate(...) (error, error)` — first result is the quota verdict, second is a
real failure. It is documented, but the signature is trivially mis-consumed (`if err := evaluate(...)`
compiles and inverts the meaning), and `go vet` will not catch it.

**Fix:** introduce a tiny named type (`type verdict = error` is not enough; a `struct{ Reason string; Err error }`
or a distinct `quotaVerdict` type) or return `(verdict error, err error)` with named results so
call sites read unambiguously.

### IN-02: `metrics.QuotaWouldRejectValue` is a test-only export with production side effects

**File:** `internal/metrics/metrics.go:244-262`

**Issue:** Exported from a production package solely for `internal/mcpserver`'s test. It also has
side effects: `GetMetricWithLabelValues` materializes the series if absent, and `clientLabel`
consumes a slot from the bounded first-seen allocator — so merely *reading* a counter can
displace a real client from per-client resolution.

**Fix:** move the read seam behind an `export_test.go`-style accessor, or have it look the series
up without allocating (`clientLabel` bypass + `prometheus.Gatherer` scan).

### IN-03: `SetQuotaObserver`/`SetQuotaEnforcementDisabled` mutate fields read by request goroutines with no synchronization

**File:** `internal/jobs/repo.go:56-62`, `internal/jobs/admission.go:93-104`

**Issue:** Both setters write plain struct fields that `Create` reads from concurrent HTTP
goroutines. Correct today only because both are startup-only by convention (and tests are
sequential). Any future runtime reconfiguration — a `/admin` toggle, a config-reload signal — is
an immediate data race with no compile-time or vet-time warning.

**Fix:** store them in `atomic.Bool` / `atomic.Pointer[func...]`, or add a `// NOT SAFE after the
repo begins serving` note next to the fields themselves (not only on the setters).

### IN-04: The `observe`/`enforce` literals are duplicated across three packages

**File:** `internal/jobs/admission.go:40-43`, `internal/clients/limits.go:117`, `internal/mcpserver/quota_admission_test.go:322-325`

**Issue:** `internal/jobs` keeps them unexported, so `internal/clients.SetEnforcementMode` hard-codes
`"observe"`/`"enforce"` (including in its error string) and the cross-entry-point test re-declares
them again. Three copies of a CHECK-constrained enum drift silently if a third mode is ever added.

**Fix:** export `jobs.EnforcementModeObserve` / `jobs.EnforcementModeEnforce` (or move them to a
tiny shared package) and have `clients.SetEnforcementMode` validate against that single set.

---

_Reviewed: 2026-08-03T03:35:51Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
