---
phase: 52-quota-enforcement-intra-class-fairness
plan: 08
subsystem: database
tags: [postgres, pgx, quota, admission-control, gap-closure, isolation-level]

# Dependency graph
requires:
  - phase: 52-quota-enforcement-intra-class-fairness (plans 01-07, especially 52-06)
    provides: pg_advisory_xact_lock-guarded admitLocked/evaluate admission gate with the in-flight (queued+active) daily charge plan 52-06 added, recordUsage terminal-transition rollup writer, client_usage_daily/client_limits schema
provides:
  - "Cancel records usage inside its guarded transition (recordUsage(ctx, tx, id, StatusCanceled, 0)), closing the create->cancel->create daily-cap evasion path (52-REVIEW.md WR-01)"
  - "migration 0014 widens client_usage_daily's status CHECK to admit 'canceled', dropped/re-added under its confirmed live name client_usage_daily_status_check, no backfill"
  - "Repo.Create and Repo.transition pin pgx.TxOptions{IsoLevel: pgx.ReadCommitted} explicitly instead of inheriting default_transaction_isolation (52-REVIEW.md WR-02)"
  - "TestCanceledJobStillCountsAgainstDailyQuota / TestCancelDoesNotDoubleCountAcrossInFlightAndRollup / TestCancelRecordsUsageExactlyOnce: permanent regression coverage for the cancel-evasion and no-double-count properties"
  - "TestAdmissionHoldsUnderRepeatableReadDefault: live proof the concurrency cap holds even when the deployment's default isolation is repeatable read"
affects: [53-operator-usage-visibility, 55-multi-replica-quota-live-proof]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "isolation level stated explicitly per-transaction (pgx.TxOptions{IsoLevel: pgx.ReadCommitted}) at every call site whose correctness depends on per-statement snapshots, rather than trusted as an inherited connection/DSN default"
    - "a second pgxpool.Pool with an overridden RuntimeParams entry, built via pgxpool.ParseConfig + pgxpool.NewWithConfig, used to induce a hostile server-side default in a live-DB test without touching the DSN's options GUC string"

key-files:
  created:
    - internal/db/migrations/0014_client_usage_canceled.sql
  modified:
    - internal/jobs/repo.go
    - internal/jobs/usage.go
    - internal/jobs/admission.go
    - internal/jobs/admission_test.go
    - internal/jobs/repo_test.go

key-decisions:
  - "Cancel charges usage via the exact same recordUsage(ctx, tx, id, status, attempt) call MarkDone/MarkFailed already use, with status=StatusCanceled and attempt=0 -- no new write path, no new SQL, relying entirely on the {queued,active} allow-list transition() already enforces for the exactly-once property"
  - "Migration 0014 does NOT backfill historical canceled jobs -- the ledger becomes complete going forward only, since backfilling would require re-deriving byte totals from jobs/job_inputs rows RETN-01's purge may have already removed"
  - "TestAdmissionHoldsUnderRepeatableReadDefault sets the hostile isolation level via pgxpool.Config.ConnConfig.RuntimeParams, not a DSN `options=-c ...` string -- the options GUC splits its value on unescaped whitespace, and a live attempt to URL-encode the space in \"repeatable read\" was silently truncated to \"repeatable\" by the server and rejected outright; RuntimeParams sends it as its own startup-packet parameter with no escaping ambiguity"

requirements-completed: [QUOT-02]

# Metrics
duration: ~7min (task commits)
completed: 2026-08-03
---

# Phase 52 Plan 08: Cancel usage ledger + pinned isolation level (gap closure) Summary

**A canceled job now charges its consumption to the daily ledger exactly once (closing the create-cancel-create evasion path WR-01 found), and the admission transaction states the READ COMMITTED isolation level its post-lock re-check depends on instead of inheriting it (WR-02) -- both proven against a live Postgres, including under a repeatable-read server default.**

## Performance

- **Duration:** ~7 min (task commits span 13:01:03 → 13:08:05 UTC+3, 2026-08-03)
- **Tasks:** 3 completed
- **Files modified:** 6 (1 new migration, 5 modified Go files)

## Accomplishments

- **Migration 0014** (`internal/db/migrations/0014_client_usage_canceled.sql`) widens `client_usage_daily`'s status CHECK from `('done','failed')` to `('done','failed','canceled')`. The constraint's live name (`client_usage_daily_status_check`) was confirmed via `\d client_usage_daily` against the running database before writing the DDL, then dropped and re-added under that same explicit name. Header comment cites 52-REVIEW.md WR-01 and states explicitly that there is no backfill. Verified: applying via `db.Migrate` is clean (`TestJobLifecycle` green), the widened constraint accepts `'canceled'` and rejects `'bogus'`, and `git diff --name-only d7ac603..HEAD -- internal/db/migrations/` shows exactly 2 files (0013 from earlier phase-52 waves + 0014).
- **`Repo.Cancel`** (`internal/jobs/repo.go`) now calls `r.recordUsage(ctx, tx, id, StatusCanceled, 0)` inside its existing guarded `transition()` closure, after the status UPDATE — mirroring `MarkFailed` exactly, no new transaction, no new code path. The doc comment states the superseding rule and cites WR-01/migration 0014; `usage.go`'s `recordUsage` doc comment is corrected to name all three terminal statuses (`StatusDone`/`StatusFailed`/`StatusCanceled`) with the SQL statement itself completely untouched.
- **`Repo.Create` and `Repo.transition`** (`internal/jobs/repo.go`) now use `pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, fn)` instead of `pgx.BeginFunc(ctx, r.pool, fn)` — exactly the two call sites named in the plan, nothing else changed about either function. `admitLocked`'s doc comment (`internal/jobs/admission.go`) documents the precise failure mode the pin prevents: under REPEATABLE READ, the post-lock re-`evaluate` would read the pre-lock snapshot instead of a fresh per-statement one, silently over-admitting with no serialization error. `internal/webhook/repo.go`, `internal/clients/repo.go` and `internal/presets/repo.go`'s `pgx.BeginFunc` call sites were deliberately left untouched — outside this phase's admission-correctness surface (confirmed via `git diff --name-only -- internal/webhook/ internal/clients/ internal/presets/` returning 0 files).
- Five new tests, all green against a live Postgres instance:
  - `TestCanceledJobStillCountsAgainstDailyQuota` — the WR-01 evasion path inverted: create A, cancel it, create B (admitted), create C rejected with `ErrDailyJobsLimit`; asserts the canceled row's `job_count = 1`.
  - `TestCancelDoesNotDoubleCountAcrossInFlightAndRollup` — a canceled job's bytes are charged exactly once; a double count would reject one create earlier than asserted.
  - `TestCancelRecordsUsageExactlyOnce` (repo_test.go) — asserts the exact row shape (`status`, `job_count`, `bytes_in`, `bytes_out`) after a cancel, then re-asserts `job_count` is still exactly 1 after a rejected repeated Cancel attempt.
  - `TestAdmissionHoldsUnderRepeatableReadDefault` — builds a second `pgxpool.Pool` with `default_transaction_isolation=repeatable read` forced via `RuntimeParams`, confirms the induced condition via `SHOW default_transaction_isolation` (skipping honestly if not induced, never on any other condition), then runs the same 12-goroutine burst `TestConcurrentBurstAdmitsExactlyK` uses against `max_concurrent=3` and asserts exactly 3 admitted / 9 rejected / 3 rows in `jobs`.
  - `TestTerminalStatusFreesSlotWithNoReleaseCode`'s pre-existing Cancel subtest re-confirmed green (D-08's zero-release-code property unaffected by the new recordUsage call).
- Full `internal/jobs` suite green, both `-count=1` and `-race`. Sibling package `internal/mcpserver` re-run (not modified) and confirmed unperturbed.

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0014 — admit 'canceled' into the usage rollup's status domain** - `d2cc2af` (feat)
2. **Task 2: Cancel charges what it consumed, exactly once** - `225eb51` (fix)
3. **Task 3: Pin READ COMMITTED where the admission re-check depends on it** - `48e35a7` (fix)

_No separate plan-metadata commit was made for STATE.md/ROADMAP.md — this is a worktree-isolated executor; the orchestrator owns those writes after the wave completes._

## Files Created/Modified

- `internal/db/migrations/0014_client_usage_canceled.sql` (new) — widens `client_usage_daily`'s status CHECK to `('done','failed','canceled')`, dropped/re-added under its confirmed live name, no backfill, additive-only blast radius.
- `internal/jobs/repo.go` — `Cancel` now calls `recordUsage(ctx, tx, id, StatusCanceled, 0)` inside its guarded transition; `Repo.Create` and `Repo.transition` now use `pgx.BeginTxFunc` with `pgx.TxOptions{IsoLevel: pgx.ReadCommitted}` instead of `pgx.BeginFunc`; doc comments on both updated in place.
- `internal/jobs/usage.go` — `recordUsage`'s doc comment corrected: status is one of `StatusDone`/`StatusFailed`/`StatusCanceled`; the SQL statement itself is byte-for-byte unchanged.
- `internal/jobs/admission.go` — `admitLocked`'s doc comment gains the isolation-dependency paragraph documenting the WR-02 failure mode and why `Repo.Create` pins the level explicitly.
- `internal/jobs/admission_test.go` — three new tests: `TestCanceledJobStillCountsAgainstDailyQuota`, `TestCancelDoesNotDoubleCountAcrossInFlightAndRollup`, `TestAdmissionHoldsUnderRepeatableReadDefault`.
- `internal/jobs/repo_test.go` — one new test: `TestCancelRecordsUsageExactlyOnce`.

## Decisions Made

- Cancel's usage write reuses `recordUsage` verbatim (same SQL, same signature) rather than a variant — the exactly-once property is a direct consequence of `transition`'s `{queued,active}` allow-list rejecting a second cancel before `apply` ever runs, identical to how it already holds for `MarkDone`/`MarkFailed`.
- No backfill for migration 0014 — historical canceled jobs are not retro-inserted; recorded explicitly in the migration's header comment as a deliberate scope boundary, not an oversight.
- `TestAdmissionHoldsUnderRepeatableReadDefault` sets the hostile isolation level via `pgxpool.Config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"` (set after `pgxpool.ParseConfig`, applied via `pgxpool.NewWithConfig`) rather than a DSN `options=-c ...` query parameter — see Deviations below.
- `internal/webhook/repo.go`, `internal/clients/repo.go`, `internal/presets/repo.go`'s pre-existing `pgx.BeginFunc` call sites are explicitly left untouched, per the plan's own instruction — confirmed via a zero-diff check on those three directories, so the omission reads as a decision, not an oversight.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Replaced the plan's suggested DSN `options=-c ...` approach for inducing repeatable-read with pgx's `RuntimeParams` map**
- **Found during:** Task 3, first run of `TestAdmissionHoldsUnderRepeatableReadDefault`
- **Issue:** The plan's `<action>` suggested appending a URL-encoded `options=-c%20default_transaction_isolation%3Drepeatable%20read` query parameter to the DSN. Live against `octoconv-db`, this failed: `FATAL: invalid value for parameter "default_transaction_isolation": "repeatable"` — the space between "repeatable" and "read" was decoded to a literal space in the `options` GUC value, and Postgres's own whitespace-splitting of that value (the `-c` options string splits on unescaped whitespace, per PostgreSQL's own `options` connection-parameter semantics) truncated the two-word value to just `"repeatable"`, an invalid enum member.
- **Fix:** Built the second pool via `pgxpool.ParseConfig(dsn)` followed by setting `cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"` and `pgxpool.NewWithConfig(ctx, cfg)`. `RuntimeParams` sends each entry as its own discrete startup-packet parameter (not through the `options` GUC's shell-like value splitting), so the two-word value round-trips with zero escaping needed. This is a documented, first-class `pgconn.Config` field (`pgconn/config.go:50`), not a workaround — it connects to the exact same `DATABASE_URL` target with one additional startup parameter.
- **Files modified:** `internal/jobs/admission_test.go` (test body and imports only — `os` retained, `strings` added then removed once the DSN-string approach was abandoned, `github.com/jackc/pgx/v5/pgxpool` added)
- **Verification:** `TestAdmissionHoldsUnderRepeatableReadDefault` PASSED live: `SHOW default_transaction_isolation` on the second pool reports exactly `repeatable read`, and the burst asserts exactly 3 admitted / 9 `ErrConcurrencyLimit` / 3 rows in `jobs`.
- **Committed in:** `48e35a7` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — a live-verified DSN-escaping defect in the plan's suggested test-harness approach, not in the admission logic being tested; the property being proven — the cap holds under a repeatable-read default — is unaffected and is exactly what the test proves).
**Impact on plan:** The fix changes only how the hostile isolation level is induced in the test harness; the assertions, the fixture shape (12-goroutine burst, `max_concurrent=3`, exact 3/9 split), and the production code under test are all as specified in the plan.

## Issues Encountered

- **DATABASE_URL not set in the shell environment**, but a live Postgres instance was already reachable at `localhost:5434` (`octoconv-db`, the project's standard docker-compose service, already running and healthy) — confirmed via `docker ps` and `psql`. All live-DB verification in this plan (migration application, all six new/modified tests, the full `-count=1`/`-race` suite runs, and the cross-package `internal/mcpserver` confirmation) was run with `DATABASE_URL="postgres://octo:octo-pass@localhost:5434/octo_db"` set inline per command, matching `.env.example`'s documented default.
- **`\d client_usage_daily` output, pasted verbatim as the plan's acceptance criteria require:**
  ```
                      Table "public.client_usage_daily"
      Column    |           Type           | Collation | Nullable | Default
  --------------+--------------------------+-----------+----------+---------
   client_id    | uuid                     |           | not null |
   engine       | text                     |           | not null |
   day          | date                     |           | not null |
   status       | text                     |           | not null |
   job_count    | bigint                   |           | not null | 0
   bytes_in     | bigint                   |           | not null | 0
   bytes_out    | bigint                   |           | not null | 0
   wall_seconds | double precision         |           | not null | 0
   created_at   | timestamp with time zone |           | not null | now()
   updated_at   | timestamp with time zone |           | not null | now()
  Indexes:
      "client_usage_daily_pkey" PRIMARY KEY, btree (client_id, engine, day, status)
      "client_usage_daily_day_idx" btree (day)
  Check constraints:
      "client_usage_daily_status_check" CHECK (status = ANY (ARRAY['done'::text, 'failed'::text, 'canceled'::text]))
  Foreign-key constraints:
      "client_usage_daily_client_id_fkey" FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
  Triggers:
      client_usage_daily_set_updated BEFORE UPDATE ON client_usage_daily FOR EACH ROW EXECUTE FUNCTION set_updated_at()
  ```
- **Direct insert commands and their observed outcomes**, recorded verbatim as the plan's acceptance criteria require: `BEGIN; INSERT INTO client_usage_daily (client_id, engine, day, status) SELECT id, 'image', now()::date, 'canceled' FROM clients LIMIT 1; ROLLBACK;` → `INSERT 0 1` (succeeded), then rolled back to leave no residue. `BEGIN; INSERT INTO client_usage_daily (client_id, engine, day, status) SELECT id, 'image', now()::date, 'bogus' FROM clients LIMIT 1; ROLLBACK;` → `ERROR: new row for relation "client_usage_daily" violates check constraint "client_usage_daily_status_check"` (rejected as expected).

## Next Phase Readiness

- The two review findings this gap-closure plan targeted (52-REVIEW.md WR-01, WR-02) are closed: the daily ledger is now complete across all three terminal outcomes (done/failed/canceled), and the isolation level the admission mechanism's correctness silently depended on is now a stated, tested requirement rather than an inherited default.
- No other files outside `internal/jobs/` and one new migration were touched; `internal/queue/`, `internal/worker/`, `deploy/chart/octoconv/templates/` are untouched (confirmed via the plan's own zero-diff check), so this plan does not perturb Phase 55's upcoming multi-replica live-proof surface.
- Ready for the phase's re-verification pass; no remaining code path writes to `client_usage_daily` outside the guarded `transition()`-backed terminal writers this plan extended.

## Self-Check: PASSED

- FOUND: `.planning/phases/52-quota-enforcement-intra-class-fairness/52-08-SUMMARY.md`
- FOUND: `internal/db/migrations/0014_client_usage_canceled.sql`
- FOUND: `d2cc2af` (Task 1 commit)
- FOUND: `225eb51` (Task 2 commit)
- FOUND: `48e35a7` (Task 3 commit)
- FOUND: `71d4474` (metadata commit)

---
*Phase: 52-quota-enforcement-intra-class-fairness*
*Completed: 2026-08-03*
