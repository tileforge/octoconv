---
phase: 52-quota-enforcement-intra-class-fairness
reviewed: 2026-08-03T00:00:00Z
depth: standard
scope: gap-closure (plans 52-06..52-09), diff base 77acc12b80c25c77ecd32b156c85b8fac0035ad4
files_reviewed: 11
files_reviewed_list:
  - internal/jobs/admission.go
  - internal/jobs/admission_test.go
  - internal/jobs/repo.go
  - internal/jobs/repo_test.go
  - internal/jobs/usage.go
  - internal/db/migrations/0014_client_usage_canceled.sql
  - internal/convert/convert.go
  - internal/convert/convert_test.go
  - cmd/manage-clients/main.go
  - cmd/manage-clients/main_test.go
  - README.md
findings:
  critical: 0
  warning: 9
  info: 0
  total: 9
status: issues_found
---

# Phase 52 (gap closure, plans 52-06..52-09): Code Review Report

**Reviewed:** 2026-08-03
**Depth:** standard (with live-database verification against `octoconv-db`)
**Files Reviewed:** 11
**Status:** issues_found
**Diff base:** `77acc12b80c25c77ecd32b156c85b8fac0035ad4..HEAD`

> This report covers the **gap-closure** work only. The waves 1–4 review is preserved in
> git history at commit `cf25a12`.

## Summary

The four claimed fixes were verified, not taken on trust. Each was traced through the code
and, where possible, checked against the live database.

**Verified sound (adversarially, not by inspection alone):**

- **CR-01 (in-flight charged into the daily cap).** `evaluate` runs on `tx`, after
  `pg_advisory_xact_lock` (`admission.go:325-333`), so the new in-flight term is genuinely
  inside the locked transaction — the ROADMAP SC2(a) race is not reintroduced.
- **No double count.** Every path that sets a terminal `jobs.status` was enumerated
  (`grep "UPDATE jobs"` — exactly 7 sites, all in `repo.go`). `done`/`failed`/`canceled`
  each write `recordUsage` exactly once inside `transition`'s apply closure, guarded by the
  `allowedFrom` allow-list; `RequeueStale` (active→queued) and `MarkActive` (active→active)
  never write it and never leave the in-flight set. Crossover is atomic because the status
  UPDATE and the ledger UPSERT commit in one transaction, and the admission read is a
  **single statement** (one snapshot), so no interleaving can observe a job in both terms or
  in neither.
- **No under-count / evasion path.** There is exactly one `INSERT INTO jobs` in the whole
  codebase (`repo.go:152`), reached only through `Repo.Create` → `admitLocked`; no batch,
  preset, or MCP path bypasses admission. `PurgeJobs` explicitly refuses to delete
  `queued`/`active` rows, so in-flight cost cannot vanish through retention.
  `Input.SizeBytes` is `multipart.FileHeader.Size`, computed by the parser and also used as
  the upload bound — not a client-declarable number.
- **Index applicability.** Confirmed live with `EXPLAIN`. On a synthetic 10 000-row
  in-flight table both new subqueries drive off `jobs_client_engine_inflight_idx`
  (`Bitmap Index Scan` / `Index Only Scan`), with `job_inputs_job_idx` on the inner side.
  The comment's claim is correct. (On the *current* dev database both partial indexes hold
  zero rows and the planner picks `jobs_inflight_idx` arbitrarily; that is an empty-table
  artifact, not a defect.)
- **Day-boundary semantics.** Traced for a fail-open case and found none: within one UTC day
  a job's cost can never leave both terms, because `recordUsage` stamps
  `day = (now() AT TIME ZONE 'utc')::date` at terminal time. The cross-midnight behaviour is
  double-charging, i.e. conservative in the safe direction.
- **Migration 0014.** Verified live: `\d client_usage_daily` shows exactly one check
  constraint, `client_usage_daily_status_check CHECK (status = ANY (ARRAY['done','failed','canceled']))`.
  No backfill is correct — historical cancels have no ledger rows and inventing them would
  meter consumption an operator never intended.
- **`IsKnownEngine` exactness.** No trimming, no `NormalizeFormat`, case-sensitive.
  `TestValidateEngine` pins `"Image"`, `"IMAGE"`, `" image"`, `"image "`, `""` as rejected;
  `TestAllEnginesCoversEveryEngineClass` additionally cross-checks `Default.Classes()`.
- **Test strength.** `TestOvershootBoundedByConcurrencyCap` (which asserted only
  `admitted <= K` and `totalBytes <= K*inputSize`) was replaced by a **strictly stronger**
  test asserting exactly one admission, the exact sentinel, and `totalBytes == inputSize`.
  `TestAdmissionHoldsUnderRepeatableReadDefault` really does induce the hostile condition
  (`RuntimeParams`, then asserts `SHOW default_transaction_isolation == "repeatable read"`
  before proceeding), and would genuinely fail without the pin: under REPEATABLE READ the
  snapshot is taken at `loadQuotaState`, before the lock, and the 12 non-conflicting inserts
  would all commit.
- `go vet ./...` clean; `go test ./internal/jobs/ ./internal/convert/ ./cmd/manage-clients/`
  green against the live database.

**What is wrong.** No blocker-grade correctness or security defect was found in the changed
lines. Nine warnings follow, in descending order of consequence. The most important are
WR-01 (a new, unbounded-in-time availability failure mode that the README understates),
WR-02 (a completeness argument in the code that the status enum does not actually
guarantee), and WR-03 (a migration that can silently no-op and take `DELETE /v1/jobs/{id}`
down with it, with zero test coverage of the resulting schema).

## Warnings

### WR-01: The in-flight daily charge is unbounded in time — a stranded job can lock a client out of an engine for days, with no operator remedy

**File:** `internal/jobs/admission.go:241-246`, `internal/jobs/admission.go:258-271`
**Severity:** WARNING (highest priority of the nine)

**Issue.** The in-flight term deliberately carries **no `created_at` predicate**, so a job
sitting in `queued`/`active` is charged against `max_daily_jobs`/`max_daily_bytes` on every
UTC day it remains there, forever. Before plan 52-06 an unfinished job consumed only a
concurrency slot — an *opt-in* limit. Now it consumes daily budget for a client that set no
concurrency cap at all, which is precisely the configuration the CR-01 verifier used
(`manage-clients set-limit <id> image - - 2`).

Reachability is not hypothetical: `handlers.go:766` documents that "the row stays in
`queued` on any enqueue failure; a reconciler will recover it". If N jobs strand and
`max_daily_jobs = N`, the client is admitted zero further jobs on that engine — not for
today, but for every subsequent day — until something drives those jobs terminal. The only
in-band escape is `DELETE /v1/jobs/{id}`, which **only the owning client can call**
(`handlers.go:926-930` ownership guard); `cmd/manage-clients` has no cancel/unstick command,
so an operator's only remedy is raw SQL.

This self-heals *if and only if* `cmd/webhook-worker` is deployed and healthy — it is the
sole host of the reconciler sweeper (`.env.example:186-192`), lock-gated to one instance
fleet-wide. Quota **availability** now hard-depends on a separate optional binary, and
nothing in the code, README, or `.env.example` records that new coupling.

The README's disclosure ("charged against each day it is in flight and again against the day
it terminates, which is deliberately conservative") describes the arithmetic but not the
failure mode: it reads as a one-day rounding artifact, not as "an engine can become
unusable for a client until an out-of-band actor intervenes".

**Fix.** Pick one:

```go
// Option A -- bound the in-flight term by the reconciler's own recovery horizon, so a
// job that outlives every recovery attempt stops charging a cap it can no longer leave.
+ COALESCE((SELECT count(*) FROM jobs j
    WHERE j.client_id = $1 AND j.engine = $2
      AND j.status IN ('queued', 'active')
      AND j.created_at >= $3), 0)   -- $3: timestamptz cutoff bound from Go
```

Note the trade-off the current comment correctly identifies: a `created_at` predicate is
fine as long as it compares `timestamptz` to `timestamptz` — bind the cutoff from Go exactly
as `FindStale`/`FindWebhookGaps` already do — so no session-`TimeZone` dependency is
introduced.

Option B — keep the semantics, but (i) state in the README that a stranded job charges the
cap indefinitely and that recovery depends on `cmd/webhook-worker` running, and (ii) add an
operator escape hatch (`manage-clients cancel-job <job-id>`) that routes through
`Repo.Cancel`, never an ad-hoc UPDATE (the guarded-transition anti-pattern in CLAUDE.md).

---

### WR-02: The no-double-count completeness argument silently assumes `jobs.status` has exactly five values — `awaiting_upload` breaks it

**File:** `internal/jobs/admission.go:213-220`, `internal/jobs/admission.go:242`, `internal/jobs/admission.go:246`
**Severity:** WARNING

**Issue.** The load-bearing comment states: *"a job has exactly one status at any instant;
`client_usage_daily` rows exist only for the terminal statuses `recordUsage` writes;
queued/active jobs are never in the rollup."* That is a **partition** argument, and it is
sound only if `{queued, active} ∪ {done, failed, canceled}` covers the whole status domain.
It does not:

```sql
-- internal/db/migrations/0001_init.sql:49-50
status text NOT NULL DEFAULT 'queued'
  CHECK (status IN ('awaiting_upload', 'queued', 'active', 'done', 'failed', 'canceled')),
```

`jobs.StatusAwaitingUpload` (`internal/jobs/jobs.go:14`) is in the enum, is accepted by the
DB CHECK, and is already whitelisted as a filterable value in
`internal/api/jobs_list_handler.go:16` — but is in neither the in-flight predicate, nor the
`jobs_client_engine_inflight_idx` partial predicate, nor any `recordUsage` call site. Today
nothing writes it (verified: two references in the whole tree, both declarations), so this
is latent rather than live. The moment a presigned/two-phase upload flow lands, those jobs
become invisible to quota accounting **and** the partial index must change in lockstep in
three places, with nothing failing to signal it.

The same silence applies to the index: `0013_client_limits.sql:76` says *"If the D-06
predicate ever changes, this index must change with it"*, but the new query in
`admission.go` is now a **second** consumer of that predicate and is not named there.

**Fix.** Either delete the now-dead `StatusAwaitingUpload` constant and the DB CHECK arm, or
make the assumption explicit and enforced:

```go
// admission.go, next to the in-flight subqueries:
// The in-flight predicate must stay the exact complement of the statuses recordUsage
// writes. jobs.status domain (0001_init.sql:50) is
// {awaiting_upload, queued, active, done, failed, canceled}; awaiting_upload is
// currently unreachable (no writer). If it ever becomes reachable it MUST be added
// here AND to jobs_client_engine_inflight_idx, or such jobs consume quota invisibly.
```

and add a guard in `internal/db/migrate_test.go` asserting the `jobs` status CHECK arms equal
`inFlightStatuses ∪ terminalStatuses`, so the two cannot drift silently.

---

### WR-03: Migration 0014 uses `DROP CONSTRAINT IF EXISTS` where every prior constraint-swap migration in this repo uses a bare `DROP CONSTRAINT` — and no test asserts the result

**File:** `internal/db/migrations/0014_client_usage_canceled.sql:43-48`
**Severity:** WARNING

**Issue.** Every other constraint swap in this codebase drops **loudly**:

```sql
-- 0005_html_engine.sql:10, 0006:9, 0009:13, 0010:13 -- all identical shape
ALTER TABLE jobs DROP CONSTRAINT jobs_engine_check;
```

0014 breaks that convention:

```sql
ALTER TABLE client_usage_daily DROP CONSTRAINT IF EXISTS client_usage_daily_status_check;
ALTER TABLE client_usage_daily ADD  CONSTRAINT client_usage_daily_status_check CHECK (...);
```

If the pre-existing constraint carries any other name on some target database (a restore, a
schema created outside this migration chain, a manual repair), the `DROP` silently does
nothing, the `ADD` succeeds under the new name, **both constraints coexist**, and the older
`CHECK (status IN ('done','failed'))` still rejects `'canceled'`. `db.Migrate` reports
success and records the version. From that point every `DELETE /v1/jobs/{id}` returns
`500 "failed to cancel job"` (`handlers.go:939-955`) for the life of that deployment, with
the job left in `queued`/`active` and nothing pointing at the migration.

Compounding it: `internal/db/migrate_test.go` asserts the `client_usage_daily` primary key,
its columns, its `day` index, and its FK — but **nothing** about the status CHECK, and 52-08
did not add such an assertion. The only coverage of 0014's effect is indirect, via
`DATABASE_URL`-gated integration tests.

The migration's own justification (*"the actual name was confirmed live via `\d`"*) argues
**for** the bare `DROP`, not against it: if the name was confirmed, `IF EXISTS` buys nothing
and costs the loud failure.

**Fix.**

```sql
-- match 0005/0006/0009/0010: fail loudly if the schema is not what we verified
ALTER TABLE client_usage_daily DROP CONSTRAINT client_usage_daily_status_check;
ALTER TABLE client_usage_daily
    ADD CONSTRAINT client_usage_daily_status_check
        CHECK (status IN ('done', 'failed', 'canceled'));
```

and add to `internal/db/migrate_test.go`:

```go
// 0014: exactly ONE status CHECK may exist, and it must admit 'canceled'.
var n int
if err := pool.QueryRow(ctx, `
    SELECT count(*) FROM pg_constraint
    WHERE conrelid = 'client_usage_daily'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%status%'`).Scan(&n); err != nil {
    t.Fatalf("count client_usage_daily status checks: %v", err)
}
if n != 1 {
    t.Fatalf("client_usage_daily status CHECK count = %d, want exactly 1", n)
}
```

---

### WR-04: Accounting can now break cancellation — the exact coupling migrations 0011 and 0013 both explicitly forbade

**File:** `internal/jobs/repo.go:261-269`, `internal/db/migrations/0014_client_usage_canceled.sql:46-48`
**Severity:** WARNING

**Issue.** Two migrations in this codebase state the principle verbatim:

> *"Accounting must never be able to break conversion."* — `0011_client_usage_daily.sql:59`,
> restated and sharpened in `0013_client_limits.sql:49-50`.

Both used it to justify **not** putting an `engine` allow-list CHECK on the accounting
tables, because a stale allow-list "would abort that transaction and leave every job of the
new engine unable to reach `done`".

Plan 52-06 wires `recordUsage` into `Repo.Cancel`, making a user-facing endpoint depend on a
successful accounting write — while `client_usage_daily.status` **retains** exactly the kind
of closed-value CHECK the principle warns about. The argument that made an engine allow-list
unsafe applies identically here: the `status` value is a Go constant (`StatusCanceled`), not
client input, so the CHECK buys no injection protection the write site does not already
have, but it can abort the transition. A future sixth terminal status, or a partially
applied 0014 (WR-03), turns cancel into a hard 500 with the job left uncancelled.

This is not merely an aesthetic asymmetry: `MarkDone`/`MarkFailed` carried the same coupling
before, but cancel is the *only* lever a client has to release quota it is being charged for
(see WR-01), so breaking cancel is strictly worse than breaking `done`.

**Fix.** Drop the CHECK, matching the reasoning 0011/0013 already committed to for `engine`:

```sql
ALTER TABLE client_usage_daily DROP CONSTRAINT client_usage_daily_status_check;
-- No replacement. status is always a jobs.Status* Go constant at the single write site
-- (internal/jobs/usage.go), never client input -- the same argument this table already
-- accepts for `engine`. A CHECK here can only abort a terminal transition it cannot
-- protect anything from.
```

If the CHECK is kept, add a note at `usage.go`'s write site stating that any new terminal
status requires a migration *before* the Go constant is used, plus the schema assertion from
WR-03 so drift is caught in CI rather than in production.

---

### WR-05: The WR-02 isolation pin was applied only inside `internal/jobs` — the same unstated dependency remains in three other repositories

**File:** `internal/jobs/repo.go:137`, `internal/jobs/repo.go:652` (fixed); `internal/presets/repo.go:148`, `internal/presets/repo.go:194`, `internal/clients/repo.go:65`, `internal/clients/repo.go:106` (not fixed)
**Severity:** WARNING

**Issue.** The fix's own reasoning — *"correctness silently depended on READ COMMITTED"* —
is not specific to `internal/jobs`. `internal/presets/repo.go:148` runs the identical
shape: `SELECT ... FOR UPDATE`, compute `newVersion = currentVersion + 1`, `UPDATE`,
`INSERT`, all on a bare `pgx.BeginFunc` with no `TxOptions`, i.e. still inheriting
`default_transaction_isolation`. Under the very configuration
`TestAdmissionHoldsUnderRepeatableReadDefault` now deliberately induces, those transactions
abort with a `40001` serialization failure that no caller retries — a different failure mode
than the jobs one (loud rather than silent), but still an unhandled dependency the phase
demonstrated a deployment can have.

The resulting state is worse than either extreme: half the codebase now pins isolation
explicitly and the other half does not, with no comment recording which half is which or
why. A reader of `presets/repo.go` will reasonably infer the omission is deliberate.

**Fix.** Pin the four remaining read-modify-write transactions the same way, or add a
one-line comment at each stating that a `40001` there is acceptable and why:

```go
return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
```

---

### WR-06: `TestAdmissionHoldsUnderRepeatableReadDefault` never migrates its own pool and cannot be run in isolation

**File:** `internal/jobs/admission_test.go:728-814`
**Severity:** WARNING

**Issue.** The test builds its own `*pgxpool.Pool` (line 749) and calls `NewRepo(pool)`
directly (line 769), bypassing `newTestRepo`, which is the only place `db.Migrate` runs
(`repo_test.go:23`). It therefore works only because an earlier test in the package already
migrated the shared database. Running

```
go test ./internal/jobs -run TestAdmissionHoldsUnderRepeatableReadDefault
```

against a fresh database fails with a raw `relation "clients" does not exist` rather than
skipping or migrating — the opposite of the careful skip discipline the rest of the test
takes (it explicitly prefers "an honest skip" to "a false failure" at line 766).

Secondary: lines 774-813 duplicate ~40 lines of `TestConcurrentBurstAdmitsExactlyK`
(lines 200-239) verbatim. A change to the burst assertions must now be made twice, and a
future reader cannot tell which copy is authoritative.

**Fix.**

```go
if err := db.Migrate(ctx, pool); err != nil {
    t.Fatalf("db.Migrate (repeatable-read pool): %v", err)
}
```

and extract the burst body into a shared helper:

```go
func assertConcurrentBurstAdmitsExactly(t *testing.T, r *Repo, clientID uuid.UUID, n, want int, keyPrefix string)
```

---

### WR-07: `TestCancelRecordsUsageExactlyOnce` asserts a weaker property than its name claims, and pins an invariant the code does not guarantee

**File:** `internal/jobs/repo_test.go:250-253`, `internal/jobs/repo_test.go:204`, `internal/jobs/repo_test.go:246`
**Severity:** WARNING

**Issue, part 1 — weak assertion.** The exactly-once half rests on:

```go
if err := r.Cancel(ctx, id); err == nil {
    t.Fatalf("expected illegal-transition error on a repeated Cancel, got nil")
}
```

Any error keeps this green. If a regression made `Cancel` fail for an unrelated reason —
notably a broken `recordUsage` write, which is the very thing this test exists to cover
(WR-03/WR-04) — the assertion still passes, and the following `job_count == 1` check passes
*because* the write failed. The test is therefore satisfied by the exact defect it is meant
to catch.

**Issue, part 2 — false invariant.** Lines 204 and 246 assert and document
*"bytes_out stays 0 (a canceled job has no outputs)"*. The code does not guarantee this:
`Repo.AddOutput` (`repo.go:409`) commits on its own pool connection **before** `MarkDone`
(`worker.go:1941`), and `Cancel`'s allow-list still contains `active`. A cancel landing in
that window charges a real, non-zero `bytes_out` on a `canceled` row via `usage.go:60`. The
test passes only because its fixture never uploads an output.

**Fix.**

```go
// part 1: pin the illegal-transition error specifically, not "any error"
err = r.Cancel(ctx, id)
if err == nil || !strings.Contains(err.Error(), "illegal transition") {
    t.Fatalf("repeated Cancel error = %v, want an illegal-transition error", err)
}

// part 2: soften the claim to what the code actually guarantees --
// "bytes_out is 0 for a job canceled before any output was recorded; a cancel racing
//  AddOutput legitimately records the bytes that were produced"
```

---

### WR-08: Two assertions in the new admission tests are dead or narrower than the production query

**File:** `internal/jobs/admission_test.go:492-494`, `internal/jobs/admission_test.go:497-502`
**Severity:** WARNING

**Issue.** Lines 492-494:

```go
if !errors.Is(err, ErrDailyBytesLimit) { t.Fatalf(...) }
if errors.Is(err, ErrConcurrencyLimit) { t.Fatalf("... must NOT be ErrConcurrencyLimit ...") }
```

`ErrConcurrencyLimit` and `ErrDailyBytesLimit` are distinct `errors.New` sentinels returned
unwrapped (`admission.go:51-55`, `:201`, `:280`), so the second branch is **unreachable** —
passing `errors.Is` against one already excludes the other. It reads as a load-bearing guard
and is not one, which is worse than no assertion at all.

Lines 497-502: the verification query filters on `j.client_id` and `j.status` but **not on
`j.engine`**, while the production in-flight term does filter on engine
(`admission.go:242`). The test therefore does not verify the shape it claims to mirror; it
agrees only because the fixture uses a single engine.

**Fix.** Delete the dead branch; add `AND j.engine = 'image'` to the verification query so it
mirrors `evaluate`'s predicate exactly.

---

### WR-09: README overstates the pre-upload gate and hardcodes facts that `AllEngines()` owns

**File:** `README.md:241-242`, `README.md:251`, `README.md:257`
**Severity:** WARNING

**Issue.** Three drifts between prose and code:

1. Lines 241-242 still read *"sit in front of `POST /v1/jobs` — **enforced fail-closed
   before the upload touches storage**"*, which describes `PreAdmit`. `PreAdmit` is
   explicitly non-authoritative and unlocked (`admission.go:355-360`), and the same README
   contradicts itself 35 lines later ("because the pre-upload gate is unlocked by design").
   The authoritative gate is `admitLocked`, which runs *after* the storage write. A reader
   sizing a quota off line 242 will conclude uploads cannot outrun the cap.
2. Line 257 hardcodes *"the seven known engine classes"* in prose. `convert.AllEngines()` is
   the source of truth and `TestAllEnginesCoversEveryEngineClass` will happily be updated to
   eight while this sentence rots.
3. Line 251's usage sample still shows `set-limit <client-id> <engine> ...`, whereas
   `usage()` (`cmd/manage-clients/main.go:229-232`) now renders the concrete
   `image|document|html|audio|av|ocr|pandoc` list. The doc and the tool's own `--help` no
   longer agree.

Related and worth recording, though not a doc bug: `AllEngines()` and the DB's
`jobs_engine_check` (`0010_pandoc_engine.sql:15`) are now **two divergent allow-lists** —
the DB additionally permits `cad`, `archive`, `probe`. `set-limit` will refuse to write a
limit for any of those. That is harmless today (no converter registers them, so no job can
carry them), and `TestAllEnginesCoversEveryEngineClass` covers the dangerous direction
(`Default.Classes() ⊆ AllEngines()`), but nothing covers the reverse drift.

**Fix.**

```markdown
Per-client, per-engine limits (QUOT-01..04, FAIR-01) sit in front of `POST /v1/jobs`.
An advisory pre-upload gate rejects a client already at its cap before the upload reaches
storage; the authoritative, race-proof check runs inside the job-insert transaction.
`NULL`/absent means unlimited; an explicit `max-concurrent 0` denies the engine entirely.
```

plus: replace "the seven known engine classes" with "one of the known engine classes (the
command prints the current set on error)", and update line 251's placeholder to match
`usage()`.

---

_Reviewed: 2026-08-03_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard (+ live `EXPLAIN` and full `go test` verification)_
