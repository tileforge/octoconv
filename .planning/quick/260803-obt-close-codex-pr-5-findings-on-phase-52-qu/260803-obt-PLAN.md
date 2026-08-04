---
phase: quick-260803-obt
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/db/migrations/0014_client_usage_canceled.sql
  - internal/jobs/admission.go
  - internal/jobs/admission_test.go
  - README.md
autonomous: true
requirements: [PR5-P1, PR5-P2A, PR5-P2B, PR5-P3]

must_haves:
  truths:
    - "Migration 0014 drops the pre-existing client_usage_daily status CHECK with a bare DROP CONSTRAINT, so a database whose constraint carries a different name aborts the migration instead of recording 0014 as applied while the old CHECK survives"
    - "The full migration chain still applies cleanly to a brand-new empty database, ending with exactly one status CHECK on client_usage_daily admitting done/failed/canceled"
    - "The live dev database, which already ran 0014, needs no further migration: a pg_constraint read shows exactly one status CHECK and it is already the target definition"
    - "admission.go no longer claims a job in flight across UTC midnight is charged against each day it is in flight AND again against the day it terminates; it instead describes the verified behavior, including the narrow terminal-transaction-spanning-midnight window in which the ledger row lands on the previous day"
    - "README.md's Quotas and fairness section carries the same corrected statement, with no surviving copy of the old claim"
    - "recordUsage still uses now(), not clock_timestamp() — the timestamp semantics are documented, never changed"
    - "A CONCURRENT burst of creates against max_daily_jobs = 1 with NO concurrency cap admits exactly one job, so the daily term's dependence on the advisory lock is pinned by a test that fails if the daily subqueries leave admitLocked's transaction"
    - "That new test is proven load-bearing: with the pg_advisory_xact_lock call temporarily disabled it FAILS, and it passes again once the call is restored"
    - "awaiting_upload's exclusion from both quota terms is a named, declared classification in admission.go rather than an omission implicit in a SQL literal, with the rejected alternative and its correctness (not cost) reasoning recorded at the declaration"
    - "A live-DB invariant test fails if the jobs.status CHECK domain gains a status that is not classified, or if jobs_client_engine_inflight_idx's partial predicate stops matching the declared in-flight status set"
    - "go build ./..., go vet ./..., gofmt -l . and go test ./... -count=1 are all green, and no pre-existing assertion was weakened"
  artifacts:
    - path: "internal/db/migrations/0014_client_usage_canceled.sql"
      provides: "bare DROP CONSTRAINT matching the 0005/0006/0009/0010 convention, plus a header note recording why the fix is an in-place edit rather than a new 0015"
      contains: "DROP CONSTRAINT client_usage_daily_status_check"
    - path: "internal/jobs/admission.go"
      provides: "corrected midnight-crossing comment + the quota status classification declarations"
      contains: "quotaUnaccountedStatuses"
    - path: "internal/jobs/admission_test.go"
      provides: "concurrent daily-cap lock-coverage test + jobs-status classification invariant test"
      contains: "TestConcurrentBurstDailyJobsCapAdmitsExactlyOne"
    - path: "README.md"
      provides: "corrected midnight-crossing statement in the Quotas and fairness section"
  key_links:
    - from: "internal/jobs/admission.go quotaInFlightStatuses"
      to: "jobs_client_engine_inflight_idx partial predicate (0013_client_limits.sql)"
      via: "invariant test reading pg_get_indexdef and comparing status literals"
      pattern: "pg_get_indexdef"
    - from: "internal/jobs/admission.go quota status classification"
      to: "jobs_status_check (0001_init.sql)"
      via: "invariant test reading pg_get_constraintdef and partitioning the status domain"
      pattern: "pg_get_constraintdef"
    - from: "TestConcurrentBurstDailyJobsCapAdmitsExactlyOne"
      to: "admitLocked's pg_advisory_xact_lock"
      via: "concurrent burst with max_concurrent nil and max_daily_jobs 1"
      pattern: "int64Ptr\\(1\\)"
---

<objective>
Close all four Codex review findings on PR #5 (Phase 52 quota admission): the silently-passing
DROP in migration 0014 (P1), the false across-midnight accounting claim in admission.go and
README.md (P2a), the missing test pinning the daily terms to the advisory lock (P2b), and
awaiting_upload sitting outside both quota accounting sets (P3).

Purpose: PR #5 ships the quota admission gate. Two of these findings are correctness-of-claim
defects (a migration that can pass while leaving the database broken, and a documented guarantee
that does not hold), one is a missing regression gate on the phase's own load-bearing mechanism,
and one is a classification gap that would silently un-meter a future upload flow.

Output: one corrected migration, one corrected comment + README statement, two new tests, and
one explicit status classification — no behavior change to the admission verdict itself.
</objective>

<execution_context>
@/Users/apaderin/dev/octoconv/.claude/get-shit-done/workflows/execute-plan.md
@/Users/apaderin/dev/octoconv/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@.planning/STATE.md

Project conventions that bind this work: error wrapping with `fmt.Errorf("...: %w", err)`;
`internal/*` never logs; every package/exported identifier carries a doc comment; non-obvious
"why" decisions get inline comments; `gofmt` + `go vet ./...` are the enforced minimum bar.

## Verified premises (do NOT re-investigate)

All four findings were verified empirically before this plan was written. Treat these as given:

1. `now()` is frozen at transaction start in PostgreSQL. Confirmed live: inside one transaction,
   two `SELECT now()` calls separated by `pg_sleep(1.2)` both returned `14:30:16.16974` while
   `clock_timestamp()` advanced to `14:30:17.400399`. Both `recordUsage` (usage.go) and the
   admission daily query (admission.go) use `now()`.
2. `awaiting_upload` is in the live `jobs` status CHECK, is declared as `StatusAwaitingUpload` in
   `internal/jobs/jobs.go:14`, and its ONLY other reference in the whole repo is a filter
   allow-list at `internal/api/jobs_list_handler.go:16`. No code path writes it.
3. Sibling migrations `0005_html_engine.sql:10`, `0006_audio_engine.sql:9`,
   `0009_ocr_engine.sql:13` and `0010_pandoc_engine.sql:13` all use a bare
   `ALTER TABLE ... DROP CONSTRAINT ...` with no `IF EXISTS`.
4. The live dev database currently carries exactly ONE check constraint on `client_usage_daily`:
   `client_usage_daily_status_check | CHECK ((status = ANY (ARRAY['done'::text, 'failed'::text, 'canceled'::text])))`.
   That is already the post-0014 target state.
5. The live `jobs` status CHECK is
   `jobs_status_check | CHECK ((status = ANY (ARRAY['awaiting_upload'::text, 'queued'::text, 'active'::text, 'done'::text, 'failed'::text, 'canceled'::text])))`.
6. The live in-flight index is
   `CREATE INDEX jobs_client_engine_inflight_idx ON public.jobs USING btree (client_id, engine) WHERE (status = ANY (ARRAY['queued'::text, 'active'::text]))`.
7. The fresh-database migration proof below was executed end-to-end before this plan was written
   and works: a scratch database can be created through the `octoconv-db` container as user
   `octo`, `go run ./cmd/migrate` applies the full chain against it, and the resulting
   `client_usage_daily` status CHECK is the expected one.

## Decision: migration 0014 is edited IN PLACE, no 0015 is added

This is settled — do not re-open it, do not add a 0015.

- Migration 0014 is already recorded in `schema_migrations` on the live dev database, so editing
  its text will not re-run it there. That is fine: premise 4 above shows the dev database's
  post-0014 state is ALREADY exactly what the corrected migration produces (the drop matched by
  name, the add succeeded, and no second status CHECK survives). Re-running would be a no-op.
- 0014 is unreleased — PR #5 is not merged, so the local dev instance is the only database that
  has ever run it. There is no drifted, already-migrated database to repair.
- A new 0015 could not fix the reported defect anyway. On a fresh or not-yet-migrated database
  0014 still runs first, and with `IF EXISTS` it would still pass silently on a drifted schema.
  Only editing 0014 itself makes that path loud. A 0015 that merely re-asserts the constraint
  would add permanent noise to every database while closing nothing.

## Scope fence — DO NOT TOUCH

- `deploy/chart/`, `internal/queue/`, `internal/worker/`, and the reconciler's `enqueuer`
  interface — all under a verified FAIR-02 zero-diff contract.
- `internal/clients/limits.go` cross-field validation.
- The pre-existing concurrency-count query's `$3`/`$4` status binding in `evaluate`
  (52-REVIEW.md WR-03 — deliberately out of scope).
- `recordUsage`'s `now()` — it must NOT become `clock_timestamp()`. The timestamp semantics are
  documented here, never changed.
- No existing assertion may be weakened to make anything green.

## Live-DB test harness (reuse, do not reinvent)

`internal/jobs` tests run against the live Postgres instance (`octoconv-db`, localhost:5434).
`newTestRepo(t)` (`internal/jobs/repo_test.go:14`) skips when `DATABASE_URL` is unset, connects
via `db.Connect`, runs `db.Migrate`, and returns a `*Repo` whose `r.pool` the tests use directly.
`createTestClient`, `setClientLimits`, `setEnforcementMode`, `seedDailyUsage`, `int64Ptr`,
`intPtr` and `createFixture` already exist — use them. No mocks, no new harness.

Load the environment with `set -a && . ./.env && set +a` before running any `internal/jobs` test.
</context>

<tasks>

<task type="auto">
  <name>Task 1: Correct migration 0014's DROP and the false across-midnight accounting claim</name>
  <files>internal/db/migrations/0014_client_usage_canceled.sql, internal/jobs/admission.go, README.md</files>
  <read_first>
internal/db/migrations/0014_client_usage_canceled.sql (whole file — the header comment is long and must be preserved, only extended)
internal/db/migrations/0010_pandoc_engine.sql (the bare-DROP convention this must match)
internal/jobs/admission.go lines 236-288 (the daily-usage query and its trailing comment block)
internal/jobs/usage.go lines 45-60 (recordUsage's `day` computation — the other half of the semantics being documented)
README.md lines 279-292 (the "Daily cap accounting, exact at admission" bullet)
  </read_first>
  <action>
Three edits, no behavior change to any query or Go expression.

(1) In `internal/db/migrations/0014_client_usage_canceled.sql`, change the DROP statement from
`DROP CONSTRAINT IF EXISTS client_usage_daily_status_check;` to
`DROP CONSTRAINT client_usage_daily_status_check;` — a bare drop, matching the convention of
0005/0006/0009/0010. Leave the ADD CONSTRAINT statement and the entire existing header comment
intact; append a new paragraph to that header recording, in the file's existing explanatory
register: that the DROP is deliberately bare because `IF EXISTS` turns schema drift into a silent
pass — if the original 0011 column CHECK lives under a different auto-generated name, the drop
becomes a no-op, the ADD succeeds alongside the survivor, the surviving
`CHECK (status IN ('done','failed'))` then rejects every canceled-usage row, `Repo.Cancel` rolls
back on every call, and the API returns 500 indefinitely while `schema_migrations` records this
migration as successfully applied; and that this migration was corrected in place rather than
superseded by a 0015 because it is unreleased (the only database that has run it is the local dev
instance, where a live `pg_constraint` read confirms exactly one status CHECK already matching
this file's ADD), and because a 0015 could not close the defect anyway — on a fresh database 0014
still runs first, so only the bare DROP here makes that path abort loudly.

(2) In `internal/jobs/admission.go`, rewrite consequence (a) of the comment that currently reads
"a job in flight across a UTC midnight is charged against each day it is in flight and again
against the day it terminates -- deliberately conservative and fail-closed rather than a free
window at the day boundary". That claim is false in a narrow window and must be replaced with a
description of the verified behavior. The replacement text must state all of the following, and
nothing beyond it:
  - While a job is `queued` or `active` it is counted by the in-flight term in every admission for
    its (client, engine) pair, on whatever UTC day that admission runs — the in-flight term has no
    date predicate.
  - When the job terminates, `recordUsage` (usage.go) writes its ledger row with
    `day = (now() AT TIME ZONE 'utc')::date`, and PostgreSQL's `now()` is transaction-START time,
    not wall-clock time — the same `now()` this query uses for its own `day` filter.
  - Therefore the ledger row lands on the UTC date at which the terminal transition's transaction
    BEGAN, not the date at which it committed. In the narrow window where a terminal transaction
    begins before UTC midnight and commits after it — for example while blocked on the jobs-row
    `SELECT ... FOR UPDATE` — the row is written to the PREVIOUS day, and from the instant that
    transaction commits the job appears in neither the new day's in-flight term nor the new day's
    ledger term: its cost is absent from the new day's total.
  - This is accepted, not fixed. `clock_timestamp()` would place the row on the commit-side day
    but would make `day` non-deterministic within a transaction, which is explicitly not wanted;
    and the outcome is defensible — the job is charged in full to the day its terminal transition
    began, and on the new day it existed only for the tail of a transaction that was already
    committing.
Do NOT re-assert the old "charged against each day ... and again against the day it terminates",
do NOT call this fail-closed or conservative, and do NOT introduce any new bound (no claim about
the window's maximum width, no claim about how often it can occur). Leave consequence (b) — the
"no timestamptz-versus-date comparison, so this check never acquires a dependency on the session
TimeZone setting" paragraph — exactly as it stands. Leave every SQL string and every Go
expression in this file untouched in this task.

(3) In `README.md`, replace the final sentence of the "Daily cap accounting, exact at admission"
bullet ("The in-flight term is not scoped to a creation date: a job still in flight across a UTC
midnight is charged against each day it is in flight and again against the day it terminates,
which is deliberately conservative.") with a one-to-three-sentence README-register version of the
same corrected statement: the in-flight term is not scoped to a creation date, so a job still in
flight across a UTC midnight keeps counting on the new day; its ledger row is written for the UTC
day on which its terminal transaction STARTED, because PostgreSQL's `now()` is transaction-start
time; so a terminal transition that begins just before midnight and commits just after it books
the job to the previous day and stops contributing to the new day's total — a known, accepted
narrow window, not a refund path. Change nothing else in that bullet or in the rest of the
section.
  </action>
  <verify>
    <automated>grep -v '^--' internal/db/migrations/0014_client_usage_canceled.sql | grep -c 'IF EXISTS' | grep -qx 0 &amp;&amp; grep -q 'DROP CONSTRAINT client_usage_daily_status_check' internal/db/migrations/0014_client_usage_canceled.sql &amp;&amp; ! grep -rq 'charged against each day it is in flight' internal/jobs/admission.go README.md &amp;&amp; gofmt -l . | grep -qx '' ; go build ./... &amp;&amp; go vet ./...</automated>
  </verify>
  <acceptance_criteria>
- `grep -v '^--' internal/db/migrations/0014_client_usage_canceled.sql | grep -c 'IF EXISTS'` prints `0`
- `grep -c 'DROP CONSTRAINT client_usage_daily_status_check' internal/db/migrations/0014_client_usage_canceled.sql` prints `1`
- `grep -rn 'charged against each day it is in flight' internal/jobs/admission.go README.md` prints nothing and exits non-zero
- `grep -c 'clock_timestamp' internal/jobs/usage.go` prints `0` (the `now()` semantics were documented, not changed)
- `git diff --stat internal/jobs/usage.go` prints nothing (usage.go is untouched)
- Fresh-database chain proof, run verbatim (validated before this plan was written):
  `docker exec octoconv-db psql -U octo -d postgres -c "DROP DATABASE IF EXISTS octo_migtest;" -c "CREATE DATABASE octo_migtest;"`
  then `set -a && . ./.env && set +a; DATABASE_URL="${DATABASE_URL%/octo_db}/octo_migtest" go run ./cmd/migrate` prints `migrations applied`
  then `docker exec octoconv-db psql -U octo -d octo_migtest -tAc "SELECT conname||' | '||pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='client_usage_daily'::regclass AND contype='c';"`
  prints exactly one line: `client_usage_daily_status_check | CHECK ((status = ANY (ARRAY['done'::text, 'failed'::text, 'canceled'::text])))`
  then `docker exec octoconv-db psql -U octo -d postgres -c "DROP DATABASE octo_migtest;"` prints `DROP DATABASE`
- Already-migrated dev database needs nothing further:
  `docker exec octoconv-db psql -U octo -d octo_db -tAc "SELECT count(*) FROM pg_constraint WHERE conrelid='client_usage_daily'::regclass AND contype='c';"` prints `1`
  and `docker exec octoconv-db psql -U octo -d octo_db -tAc "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='client_usage_daily'::regclass AND contype='c';"`
  prints `CHECK ((status = ANY (ARRAY['done'::text, 'failed'::text, 'canceled'::text])))`
- `go build ./...` and `go vet ./...` exit 0; `gofmt -l .` prints nothing
  </acceptance_criteria>
  <done>Migration 0014 uses a bare DROP and explains both that choice and the in-place-edit decision; the false across-midnight claim exists nowhere in admission.go or README.md, replaced by the verified description; the migration chain applies cleanly to a brand-new database and the already-migrated dev database is confirmed to be in the target state with no new migration.</done>
</task>

<task type="auto">
  <name>Task 2: Pin the daily quota terms to the advisory lock with a concurrent-burst test</name>
  <files>internal/jobs/admission_test.go</files>
  <read_first>
internal/jobs/admission_test.go lines 1-75 (the helpers: setClientLimits, setEnforcementMode, int64Ptr, createFixture)
internal/jobs/admission_test.go lines 190-241 (TestConcurrentBurstAdmitsExactlyK — the concurrency-cap burst shape to mirror)
internal/jobs/admission_test.go lines 461-553 (TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher and TestDailyJobsLimitWithoutConcurrencyCap — the two sequential tests this one supplements)
internal/jobs/admission.go lines 205-288 (the daily query inside evaluate) and 312-353 (admitLocked's advisory lock)
  </read_first>
  <action>
Add ONE new test, `TestConcurrentBurstDailyJobsCapAdmitsExactlyOne`, to
`internal/jobs/admission_test.go`. Modify no existing test.

Shape it on `TestConcurrentBurstAdmitsExactlyK`: `newTestRepo(t)`, `createTestClient(t, r)`,
`setEnforcementMode(t, r, clientID, enforcementModeEnforce)`, then
`setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(1))` — max_concurrent deliberately
nil, max_daily_bytes deliberately nil, max_daily_jobs = 1. Launch 12 goroutines that all block on
a shared start channel, each calling `createFixture` with a distinct object key under a fresh
`uploads/adm16/` prefix and a small size, collect their errors into a slice indexed by goroutine,
close the channel, and wait.

Assert: exactly 1 error is nil; exactly 11 satisfy `errors.Is(err, ErrDailyJobsLimit)`; any other
error is a `t.Fatalf`; and `SELECT count(*) FROM jobs WHERE client_id = $1` on `r.pool` returns
exactly 1, so the rejected transactions left nothing behind.

Give it a doc comment recording exactly why it exists and what breaks it: it is the lock-coverage
pin that `TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher` cannot be, because that test
performs only two SEQUENTIAL creates and therefore still passes if the daily in-flight subqueries
are moved out of `admitLocked`'s transaction onto `r.pool`; and
`TestConcurrentBurstAdmitsExactlyK` does not cover it either, because it configures only
`max_concurrent`, so the concurrency term — not the daily term — is what gets serialized there.
Setting max_concurrent to nil is what makes the DAILY term the thing being serialized here.
Record the failure mode explicitly: if the daily subqueries ever leave the advisory-locked
transaction (or the `pg_advisory_xact_lock` call is removed), all 12 goroutines read an in-flight
count of 0 before any insert commits, all 12 are admitted, and this test fails. Note as well that
the byte and job-count terms are computed by the SAME single SQL statement under the SAME lock,
so one test pins the lock coverage of both.
  </action>
  <verify>
    <automated>set -a &amp;&amp; . ./.env &amp;&amp; set +a &amp;&amp; go test ./internal/jobs/ -run 'TestConcurrentBurstDailyJobsCapAdmitsExactlyOne' -count=1 -v</automated>
  </verify>
  <acceptance_criteria>
- `set -a && . ./.env && set +a && go test ./internal/jobs/ -run 'TestConcurrentBurstDailyJobsCapAdmitsExactlyOne' -count=1 -v` prints `--- PASS: TestConcurrentBurstDailyJobsCapAdmitsExactlyOne` and `ok`
- Load-bearing proof (mutation must be reverted): temporarily comment out the
  `SELECT pg_advisory_xact_lock($1, $2)` `tx.Exec` block in `admitLocked`, re-run the same
  `go test` command, and observe `--- FAIL: TestConcurrentBurstDailyJobsCapAdmitsExactlyOne`;
  then restore the block
- After restoring: `grep -v '^\s*//' internal/jobs/admission.go | grep -c 'pg_advisory_xact_lock'` prints `1`
- `set -a && . ./.env && set +a && go test ./internal/jobs/ -count=1` exits 0 with `ok` (every pre-existing test in the package still passes, unmodified)
- `git diff internal/jobs/admission_test.go | grep -c '^-'` prints a number equal to the count of `^--- a/` header lines only (no existing test line was deleted or weakened)
- `gofmt -l .` prints nothing; `go vet ./...` exits 0
  </acceptance_criteria>
  <done>A concurrent burst against `max_daily_jobs = 1` with no concurrency cap admits exactly one job, the test is empirically proven to fail when the advisory lock is removed, the lock is restored, and the whole `internal/jobs` suite is green with no existing test touched.</done>
</task>

<task type="auto">
  <name>Task 3: Classify awaiting_upload explicitly and guard the quota status partition</name>
  <files>internal/jobs/admission.go, internal/jobs/admission_test.go</files>
  <read_first>
internal/jobs/jobs.go lines 12-20 (the status constants)
internal/jobs/admission.go lines 236-288 (the in-flight SQL literals and the comment explaining why they are literals)
internal/jobs/usage.go lines 27-50 (which statuses recordUsage is called with)
internal/db/migrations/0013_client_limits.sql lines 70-79 (jobs_client_engine_inflight_idx and its "if the D-06 predicate ever changes, this index must change with it" note)
internal/api/jobs_list_handler.go lines 10-25 (the only other awaiting_upload reference)
  </read_first>
  <action>
Two edits.

(1) Add three package-level `var` declarations to `internal/jobs/admission.go`, near the quota
reason constants:
  - `quotaInFlightStatuses` = StatusQueued, StatusActive — the status set the in-flight term's SQL
    literals and `jobs_client_engine_inflight_idx`'s partial predicate both use.
  - `quotaLedgerStatuses` = StatusDone, StatusFailed, StatusCanceled — the terminal statuses
    `recordUsage` writes to `client_usage_daily` (migration 0014).
  - `quotaUnaccountedStatuses` = StatusAwaitingUpload — statuses visible to NEITHER quota term.
Give them a shared doc comment that records the decision and its reasoning, in this package's
existing explanatory register:
  - Every value in the `jobs.status` CHECK domain must appear in exactly one of these three sets;
    the invariant test in admission_test.go enforces that partition against the live schema.
  - `awaiting_upload` is declared in jobs.go and admitted by the schema, but NO code path writes
    it — its only other reference in the repo is a filter allow-list in
    `internal/api/jobs_list_handler.go`. It is therefore invisible to both quota terms today with
    zero observable effect.
  - The rejected alternative, rejected on correctness rather than cost: moving `awaiting_upload`
    into the in-flight predicate (which would also require changing
    `jobs_client_engine_inflight_idx`'s predicate in a new migration) would commit to quota
    semantics for a status whose meaning does not exist yet. The byte term sums
    `job_inputs.size_bytes`; for a job still awaiting its upload that row is either absent — in
    which case moving the status buys the byte term nothing — or carries a client-declared size
    that has not been verified against any stored object. Deciding between those belongs to
    whichever phase actually introduces the two-phase upload flow, because that phase is the one
    that determines whether a trusted size exists at `awaiting_upload` time; pre-deciding it here
    would encode a semantic no test can exercise, since the status is unreachable through every
    repo API.
  - What the declaration buys instead: the omission stops being an accident of a SQL literal and
    becomes a named classification, so the day a status is added to the schema, or the in-flight
    index predicate changes, or someone moves `awaiting_upload` out of the unaccounted set, the
    invariant test forces the quota question to be answered rather than skipped.
Change no SQL string and no query in this file — the in-flight literals stay literals for the
index-matching reason already documented there.

(2) Add `TestEveryJobStatusIsClassifiedForQuotaAccounting` to `internal/jobs/admission_test.go`
(live DB, `newTestRepo(t)`), asserting three things:
  - Read the jobs status CHECK definition from the live schema:
    `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'jobs'::regclass AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%(status = ANY%'`.
    Extract every `'<value>'::text` literal with a regexp and assert the resulting set equals the
    union of the three declared slices EXACTLY, in both directions. The failure message must name
    the offending statuses and instruct the reader to decide whether the new status consumes
    quota — moving it into `quotaInFlightStatuses` (which also requires changing
    `jobs_client_engine_inflight_idx`'s predicate in a new migration and the in-flight SQL
    literals in `evaluate`), into `quotaLedgerStatuses`, or into `quotaUnaccountedStatuses` with a
    recorded reason.
  - Read `SELECT pg_get_indexdef('jobs_client_engine_inflight_idx'::regclass)`, extract the status
    literals from its `WHERE` predicate the same way, and assert that set equals
    `quotaInFlightStatuses` exactly — so the index predicate and the Go declaration cannot drift
    apart in either direction.
  - Assert `quotaUnaccountedStatuses` contains exactly `StatusAwaitingUpload`, with a failure
    message stating that removing an entry is only correct together with moving that status into
    one of the other two sets, and that for the in-flight set that also means changing the index
    predicate in a new migration.
Compare sets order-independently (sort or use a map), never by comparing raw SQL strings.
  </action>
  <verify>
    <automated>set -a &amp;&amp; . ./.env &amp;&amp; set +a &amp;&amp; go test ./internal/jobs/ -run 'TestEveryJobStatusIsClassifiedForQuotaAccounting' -count=1 -v</automated>
  </verify>
  <acceptance_criteria>
- `set -a && . ./.env && set +a && go test ./internal/jobs/ -run 'TestEveryJobStatusIsClassifiedForQuotaAccounting' -count=1 -v` prints `--- PASS: TestEveryJobStatusIsClassifiedForQuotaAccounting` and `ok`
- Load-bearing proof (mutation must be reverted): temporarily remove `StatusAwaitingUpload` from `quotaUnaccountedStatuses`, re-run the same command, observe `--- FAIL: TestEveryJobStatusIsClassifiedForQuotaAccounting`, then restore it
- After restoring: `grep -c 'StatusAwaitingUpload' internal/jobs/admission.go` prints `1` or more, and the single-test run passes again
- `grep -c "status IN ('queued', 'active')" internal/jobs/admission.go` prints `2` (both in-flight subqueries still use SQL literals — no query was rewritten)
- `git diff internal/db/migrations/ | wc -l` prints only the Task 1 migration diff — no new migration file exists: `ls internal/db/migrations/ | grep -c '^0015'` prints `0`
- `set -a && . ./.env && set +a && go test ./... -count=1` exits 0
- `go build ./...` and `go vet ./...` exit 0; `gofmt -l .` prints nothing
  </acceptance_criteria>
  <done>awaiting_upload's exclusion from quota accounting is a declared, reasoned classification instead of an implicit omission; a live-schema invariant test fails if the status domain gains an unclassified value or the in-flight index predicate drifts from the declaration; no migration was added and no admission query was changed.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| deployment → database schema | A migration runs with DDL privileges; a migration that passes while leaving the schema wrong is an availability defect that outlives the deploy |
| client → POST /v1/jobs admission gate | Untrusted request volume crosses here; the quota terms are what bound it |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-OBT-01 | Denial of Service | `internal/db/migrations/0014_client_usage_canceled.sql` | mitigate | Bare `DROP CONSTRAINT` (Task 1): a drifted schema aborts the migration instead of recording it applied while a surviving `CHECK (status IN ('done','failed'))` makes every `Repo.Cancel` return 500 |
| T-OBT-02 | Elevation of Privilege | `evaluate`'s daily terms in `internal/jobs/admission.go` | mitigate | Concurrent-burst test with `max_daily_jobs = 1` and no concurrency cap (Task 2) fails if the daily subqueries ever leave the advisory-locked transaction, which is what would let a burst exceed a client's daily cap |
| T-OBT-03 | Repudiation | quota accounting coverage of `jobs.status` | mitigate | Declared three-way status partition plus a live-schema invariant test (Task 3): a future status that consumes capacity cannot be added without the quota question being answered |
| T-OBT-04 | Information Disclosure | the corrected comment / README text | accept | Documentation of an accounting window; no secret, no client data, and no new attack surface — the wording is corrected precisely so the residual is disclosed rather than overclaimed |
| T-OBT-SC | Tampering | npm/pip/cargo installs | accept | No package-manager installs in this plan — no new dependency is added to `go.mod` |
</threat_model>

<verification>
Run from the repository root with the live database up (`docker ps` shows `octoconv-db`):

```bash
set -a && . ./.env && set +a
go build ./...
go vet ./...
gofmt -l .
go test ./... -count=1
```

Plus the fresh-database migration chain proof from Task 1's acceptance criteria (create
`octo_migtest`, run `go run ./cmd/migrate` against it, read the `client_usage_daily` status CHECK,
drop the database).

The two mutation proofs (advisory lock removed in Task 2, `StatusAwaitingUpload` removed in
Task 3) must each have been observed FAILING and then reverted; the final `go test ./... -count=1`
above runs against the fully restored tree.
</verification>

<success_criteria>
- Migration 0014 uses a bare `DROP CONSTRAINT` and its header records both why the drop is bare
  and why the fix is an in-place edit rather than a new 0015; no `0015_*.sql` exists.
- The full migration chain applies cleanly to a brand-new empty database, producing exactly one
  `client_usage_daily` status CHECK admitting `done`/`failed`/`canceled`; the already-migrated dev
  database is confirmed to already be in that state.
- The string "charged against each day it is in flight" appears nowhere in `internal/jobs/admission.go`
  or `README.md`; both carry the verified description of the midnight-spanning terminal-transaction
  window instead, with no new unproven bound asserted.
- `internal/jobs/usage.go` is unmodified — `now()` was documented, never replaced.
- `TestConcurrentBurstDailyJobsCapAdmitsExactlyOne` passes, and was observed to FAIL with the
  advisory lock disabled.
- `TestEveryJobStatusIsClassifiedForQuotaAccounting` passes, and was observed to FAIL with
  `StatusAwaitingUpload` removed from `quotaUnaccountedStatuses`.
- No file under `deploy/chart/`, `internal/queue/`, `internal/worker/` or `internal/clients/` is
  modified; the `$3`/`$4` concurrency-count binding is untouched.
- `go build ./...`, `go vet ./...`, `gofmt -l .` and `go test ./... -count=1` are all green, and
  no pre-existing assertion was weakened.
</success_criteria>

<output>
Create `.planning/quick/260803-obt-close-codex-pr-5-findings-on-phase-52-qu/260803-obt-SUMMARY.md` when done
</output>
