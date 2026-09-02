---
phase: 57-operation-dimension-output-roles
plan: 05
subsystem: database
tags: [go, postgres, pgx, jobs, invariant]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    plan: 03
    provides: "job_outputs.role column (closed CHECK, DB DEFAULT 'primary') that this plan's MarkDone precondition counts against"
provides:
  - "D-13: a done job without exactly one job_outputs row with role='primary' is an unreachable state — MarkDone's apply closure refuses to commit the transition otherwise, and the refusal rolls back the whole apply closure (status UPDATE + recordUsage + job_events insert)"
  - "internal/jobs/output_role_test.go addPrimaryOutput test helper — the canonical way every DB-integration test in this package now satisfies MarkDone's precondition without duplicating AddOutput boilerplate"
  - "5 new tests proving the invariant's 3 rejection shapes (no outputs, report-only, two primary) and 2 acceptance shapes (single primary, primary+report)"
affects: [57-06, 60]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "MarkDone's apply closure, not Repo.transition, is where a done-specific precondition belongs — transition() stays a shared, untouched invariant across MarkActive/MarkDone/MarkFailed/Cancel/RequeueStale; a precondition specific to one target status lives in that status's own apply closure, still inside the same tx transition() already row-locked"

key-files:
  created: []
  modified:
    - internal/jobs/repo.go
    - internal/jobs/output_role_test.go
    - internal/jobs/repo_test.go
    - internal/jobs/usage_test.go
    - internal/jobs/retention_test.go
    - internal/jobs/admission_test.go
    - internal/reconciler/retention_test.go

key-decisions:
  - "The precondition query and its error return are NOT wrapped in asynq.SkipRetry (unlike the illegal-transition case in internal/worker/worker.go) because the failure mode self-heals: asynq redelivers the task, AddOutput upserts on (job_id, ordinal), and the retried MarkDone succeeds. T-57-22 in the plan's threat register accepts the residual retry cost as the price of self-healing over a hard-fail."
  - "TestRecordUsageSumsAllOrdinals's second output row is demoted from the AddOutput-defaulted role='primary' to role='report' via a direct SQL UPDATE (AddOutput has no write path for role, D-16) rather than removed — this keeps proving recordUsage's real claim (it sums size_bytes over EVERY ordinal regardless of role) while satisfying D-13's new precondition."
  - "internal/reconciler/reconciler_soak_test.go required NO changes despite being listed in files_modified: both its MarkDone calls already satisfy the invariant on inspection — one already has AddOutput immediately before it, and the other (the 'zombie MarkDone' case) targets a job that is 'queued', not 'active', so transition()'s allow-list rejects it before the D-13 check inside MarkDone's apply closure is ever reached."

requirements-completed: [ROLE-01, ROLE-02]

# Metrics
duration: ~45min
completed: 2026-08-09
---

# Phase 57 Plan 05: MarkDone D-13 Invariant Summary

**MarkDone's apply closure now counts `job_outputs` rows with `role='primary'` on the same locked transaction and refuses to commit `done` unless the count is exactly one, making "done job with no primary result" an unreachable database state rather than an API-layer guess — `Repo.transition` itself is untouched.**

## Performance

- **Duration:** ~45 min
- **Completed:** 2026-08-09T17:57Z
- **Tasks:** 2/2 completed
- **Files modified:** 7 (1 production file, 6 test files)

## Accomplishments

- Added a 4-line precondition (plus a comment explaining the "why" three times over: why not in `transition()`, why no race, why the caller treats the error as retryable) to `MarkDone`'s apply closure in `internal/jobs/repo.go`, gated on `SELECT count(*) FROM job_outputs WHERE job_id = $1 AND role = 'primary'` run on the same `tx` that `transition()` already holds the job row locked on.
- Confirmed via `git diff` that every changed line lives strictly inside `MarkDone` — `Repo.transition`'s signature, body, and every other caller (`MarkActive`, `MarkFailed`, `Cancel`, `RequeueStale`) are byte-for-byte unchanged.
- Wrote `addPrimaryOutput`, a package-private test helper in `internal/jobs/output_role_test.go`, and 5 new DB-gated tests proving the invariant's shape: 3 rejections (zero outputs, report-only output, two primary outputs) and 2 acceptances (single primary, primary+report) — each rejection test also confirms the whole transaction rolled back (`jobs.status` still `active`, no `job_events` row with `to_status='done'`, no `client_usage_daily` row).
- Walked every pre-existing `MarkDone` call across `internal/jobs/{repo,usage,retention,admission}_test.go` and `internal/reconciler/retention_test.go` (27 call sites found by direct inspection) and brought each to the new precondition individually — 15 needed a new `addPrimaryOutput`/`AddOutput` call, 8 already satisfied it (an `AddOutput` already ran immediately before), 4 correctly need no change because they assert `MarkDone` fails for a reason unrelated to D-13 (canceled job, already-done job, queued-not-active job).
- One fixture (`TestRecordUsageSumsAllOrdinals`) needed a genuine adjustment rather than a bare `addPrimaryOutput` insertion: its second output row defaulted to `role='primary'` via `AddOutput` (which never writes `role`, D-16), giving it two primary outputs. Demoted that row to `role='report'` via direct SQL so the test keeps proving what it claims (recordUsage sums bytes across every ordinal, regardless of role) without violating D-13.

## Task Commits

1. **Task 1: Предусловие «ровно один primary-выход» в замыкании MarkDone** - `2043f32` (feat) — `internal/jobs/repo.go`
2. **Task 2: Тесты инварианта D-13 и приведение существующих DB-тестов к новому предусловию** - `15bffb3` (test) — `internal/jobs/output_role_test.go`, `internal/jobs/repo_test.go`, `internal/jobs/usage_test.go`, `internal/jobs/retention_test.go`, `internal/jobs/admission_test.go`, `internal/reconciler/retention_test.go`

## Files Created/Modified

- `internal/jobs/repo.go` - `MarkDone`'s apply closure gains a 29-line block (precondition query + explanatory comment) before its two pre-existing steps (`UPDATE jobs`, `recordUsage`); `transition` and every other caller are untouched
- `internal/jobs/output_role_test.go` - `addPrimaryOutput` helper (writes ordinal-0 output via `AddOutput`, relying on the DB default for role) + 5 new tests: `TestMarkDoneRejectsJobWithNoOutputs`, `TestMarkDoneRejectsJobWithOnlyReportOutput`, `TestMarkDoneRejectsJobWithTwoPrimaryOutputs`, `TestMarkDoneAcceptsPrimaryPlusReport`, `TestMarkDoneAcceptsSinglePrimaryOutput`
- `internal/jobs/repo_test.go` - 8 pre-existing `MarkDone` calls (`TestCancelDoneRejected`, `TestRequeueStale`, 5 calls inside `TestFindWebhookGaps`, `TestUpdateProgressInactiveJobIsNoop`) each gain an `addPrimaryOutput` call immediately before `MarkDone`
- `internal/jobs/usage_test.go` - 5 `MarkDone` call sites adapted: 4 gain `addPrimaryOutput` (`TestRecordUsageFailedPathAndStatusSeparation`, `TestRecordUsageSkipsOrphanedClient`, `TestRecordUsageWallSeconds`'s "explicit attempt" subtest, both subtests of `TestRecordUsageSecondTerminalTransitionIsRejected` on their first `MarkDone`); `TestRecordUsageSumsAllOrdinals` instead demotes its second output's role to `report` via direct SQL
- `internal/jobs/retention_test.go` - `createAgedJob`'s `StatusDone` case gains one `addPrimaryOutput` call, fixing all 7 dependent test failures (`TestPurgeJobs*`, `TestPurgeUsage*`) in one place
- `internal/jobs/admission_test.go` - 2 call sites (`TestTerminalStatusFreesSlotWithNoReleaseCode`'s `MarkDone` case, `TestInFlightAndTerminalUsageCountedExactlyOnce`'s job A) gain `addPrimaryOutput`
- `internal/reconciler/retention_test.go` - 2 call sites gain a direct `repo.AddOutput` call (this package cannot see `jobs.addPrimaryOutput`, which is unexported)

## Decisions Made

- The precondition lives inside `MarkDone`'s apply closure, not `transition()`, per the plan's explicit directive and the project's documented "guarded transition helper" anti-pattern — confirmed via `git diff` that zero lines of `transition()` changed.
- The rejection error is a plain, unwrapped `fmt.Errorf` (not a sentinel, not `asynq.SkipRetry`) — matching the "illegal transition" convention at `repo.go:685` and, per the plan, deliberately NOT the `asynq.SkipRetry` convention used for illegal-transition errors in `internal/worker/worker.go`, because a retried task's `AddOutput` upsert self-heals the precondition on the next attempt.
- `TestRecordUsageSumsAllOrdinals`'s fixture was corrected rather than left alone or weakened — see key-decisions in frontmatter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `TestRecordUsageSumsAllOrdinals` fixture produced two primary outputs**

- **Found during:** Task 2, while running the full `internal/jobs` suite after Task 1 landed
- **Issue:** The test calls `AddOutput` twice (ordinals 0 and 1) to prove `recordUsage` sums `size_bytes` across every ordinal. `AddOutput` never writes `role` (D-16 — the DB default is the sole source of truth), so both rows defaulted to `role='primary'`, giving the job two primary outputs and tripping the new "exactly one" precondition with `job ... has 2 primary outputs, exactly 1 required`.
- **Fix:** Added a direct SQL `UPDATE job_outputs SET role = 'report' WHERE job_id = $1 AND ordinal = 1` immediately after the second `AddOutput` call, with a comment explaining why `AddOutput` itself cannot express this. `recordUsage`'s SQL sums `size_bytes` unconditionally on `job_id`, without filtering by role, so the byte-summing assertion this test exists to prove is unaffected.
- **Files modified:** `internal/jobs/usage_test.go`
- **Verification:** `DATABASE_URL=... go test ./internal/jobs/ -run TestRecordUsageSumsAllOrdinals -count=1 -v` → `PASS`; full package suite green afterward.
- **Committed in:** `15bffb3` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug in a test fixture, surfaced by the new invariant working as intended)
**Impact on plan:** None on the invariant itself — this was the exact kind of "test was proving something else" case Task 2's own instructions anticipated and told the executor to diagnose rather than paper over. `internal/reconciler/reconciler_soak_test.go` — listed in the plan's `files` for Task 2 — needed zero changes; both its `MarkDone` calls were already compliant on inspection (documented in key-decisions above), so it was not staged or committed.

## Issues Encountered

None beyond the fixture bug documented above.

## User Setup Required

None - no external service configuration required. Verification ran against the already-running local `octoconv-db` (PostgreSQL 18, `postgres://octo:octo-pass@localhost:5434/octo_db`), which was already up as a Docker container shared across worktrees; `go run ./cmd/migrate` was re-run to confirm the schema was current before testing.

## Threat-model self-review (per plan_specific_warnings)

The warning asked whether this invariant could abort a transition that must not be abortable, echoing 52-REVIEW.md's WR-03/WR-04 finding that `recordUsage` can abort `Repo.Cancel` via the `engine` status CHECK. **It cannot**, by construction: the new `SELECT count(*) ... role = 'primary'` query and its `n != 1` check exist ONLY inside `MarkDone`'s apply closure — a closure `MarkFailed`, `Cancel`, `MarkActive`, and `RequeueStale` never call (each passes its own distinct closure to `transition()`, per `repo.go:188-284`). There is no shared code path through which this check could reach `Cancel` or any other transition, so the WR-03/WR-04 class of defect (a write on one transition unexpectedly failing another transition sharing the same underlying table/constraint) does not recur here. Confirmed live: `TestCanceledAllowListBackstop` (unmodified by this plan) still passes — a canceled job's `MarkDone` attempt is rejected by `transition()`'s allow-list (`{active}` does not include `canceled`) before the apply closure, and therefore this new check, ever runs.

The downstream consequence flagged in the warning — that plan 57-06's `handleGetJob` will cite this invariant as the reason it does NOT branch on "no primary output" — now rests on solid ground: `TestMarkDoneRejectsJobWithNoOutputs` and `TestMarkDoneRejectsJobWithOnlyReportOutput` prove the DB-level unreachability directly (not merely "not observed today"), and `TestMarkDoneAcceptsPrimaryPlusReport` proves the invariant does not block the sanitize-job shape that motivated it.

## Next Phase Readiness

- `job_outputs.role='primary'` exactly-one is now enforced at the point of writing `done`, not merely at read time — plan 57-06's `handleGetJob` can select the primary output unconditionally once a job is `done`, with no defensive "what if there is no primary" branch, because that state cannot exist.
- The self-healing retry story (T-57-22, accept disposition) depends on `AddOutput`'s upsert semantics staying exactly what they are today (`ON CONFLICT (job_id, ordinal) DO UPDATE`) — any future change to that upsert's identity key would need to re-examine whether a retried `MarkDone` still converges.
- No blockers for 57-06 or Phase 60 (the `role='report'` write path Phase 60 needs is unaffected — `AddOutput` still never writes `role`, per D-16, and this plan's own precondition is agnostic to how a `report` row gets its role, only that a `primary` one exists exactly once).

## Acceptance Criteria — literal command output

Task 1:

```
$ grep -c "AND role = 'primary'" internal/jobs/repo.go
1
$ grep -c 'count primary outputs for job' internal/jobs/repo.go
1
$ git diff -U0 HEAD~2 -- internal/jobs/repo.go | grep '^[+-]' | grep -v '^[+-][+-]' | grep -c 'func (r \*Repo) transition'
0
$ grep -v '^\s*//' internal/jobs/repo.go | grep -c 'asynq'
0
$ go build ./...
(exit 0)
$ gofmt -l internal/jobs
(empty)
```

`git diff HEAD~2 -- internal/jobs/repo.go` (full diff, confirming every changed line is inside `MarkDone`):

```diff
@@ -203,6 +203,35 @@ func (r *Repo) MarkActive(ctx context.Context, id uuid.UUID) error {
 // clock).
 func (r *Repo) MarkDone(ctx context.Context, id uuid.UUID, attempt time.Duration) error {
 	return r.transition(ctx, id, StatusDone, []string{StatusActive}, nil, func(ctx context.Context, tx pgx.Tx) error {
+		// D-13: a done job without exactly one primary output is an
+		// unreachable state, enforced here rather than in transition()
+		// ... (full explanatory comment, 17 lines) ...
+		var n int
+		if err := tx.QueryRow(ctx,
+			`SELECT count(*) FROM job_outputs WHERE job_id = $1 AND role = 'primary'`, id,
+		).Scan(&n); err != nil {
+			return fmt.Errorf("count primary outputs for job %s: %w", id, err)
+		}
+		if n != 1 {
+			return fmt.Errorf("job %s has %d primary outputs, exactly 1 required to mark done", id, n)
+		}
 		if _, err := tx.Exec(ctx,
 			`UPDATE jobs SET status = 'done', finished_at = now() WHERE id = $1`, id); err != nil {
 			return err
```

Task 2:

```
$ go test ./internal/jobs/ -count=1
ok  	github.com/apaderin/octoconv/internal/jobs	3.774s
$ go test ./internal/reconciler/ -count=1
ok  	github.com/apaderin/octoconv/internal/reconciler	6.037s
$ go test ./internal/jobs/ -run 'TestMarkDoneRejects|TestMarkDoneAccepts' -count=1 -v
=== RUN   TestMarkDoneRejectsJobWithNoOutputs
--- PASS: TestMarkDoneRejectsJobWithNoOutputs (0.04s)
=== RUN   TestMarkDoneRejectsJobWithOnlyReportOutput
--- PASS: TestMarkDoneRejectsJobWithOnlyReportOutput (0.05s)
=== RUN   TestMarkDoneRejectsJobWithTwoPrimaryOutputs
--- PASS: TestMarkDoneRejectsJobWithTwoPrimaryOutputs (0.03s)
=== RUN   TestMarkDoneAcceptsPrimaryPlusReport
--- PASS: TestMarkDoneAcceptsPrimaryPlusReport (0.03s)
=== RUN   TestMarkDoneAcceptsSinglePrimaryOutput
--- PASS: TestMarkDoneAcceptsSinglePrimaryOutput (0.03s)
PASS
ok  	github.com/apaderin/octoconv/internal/jobs	0.590s
$ grep -c 'func addPrimaryOutput' internal/jobs/output_role_test.go
1
$ grep -rc 'MarkDone(' internal/jobs/*_test.go internal/reconciler/*_test.go | awk -F: '{s+=$2} END {print s}'
34
$ gofmt -l internal/jobs internal/reconciler
(empty)
$ go test ./internal/worker/ -count=1
ok  	github.com/apaderin/octoconv/internal/worker	4.646s
```

Zero `SKIP` lines confirmed via `go test ./internal/jobs/ ./internal/reconciler/ -count=1 -v | grep -c SKIP` → `0`.

Overall verification:

```
$ go build ./... && go vet ./...
(clean, exit 0)
$ gofmt -l .
(empty)
$ go test ./internal/jobs/ ./internal/reconciler/ ./internal/worker/ -count=1
ok  	github.com/apaderin/octoconv/internal/jobs	3.256s
ok  	github.com/apaderin/octoconv/internal/reconciler	6.232s
ok  	github.com/apaderin/octoconv/internal/worker	4.646s
```

**Note on the plan's live compose-stack end-to-end check** (`png→webp` conversion through the running API/worker, then `psql ... SELECT count(*) FROM job_outputs WHERE job_id='<id>' AND role='primary'` = 1): this plan's files (`internal/jobs/*`) do not include `internal/api` or `internal/worker`, which the sibling parallel plan 57-04 owns and was concurrently modifying in a separate worktree at execution time. Building and running `cmd/api`/`cmd/worker` from this worktree's checkout would not reflect 57-04's in-flight changes and risks a false read either way, so this exact live-HTTP path is deferred to post-merge integration testing. In its place, the equivalent proof was run directly against the same live PostgreSQL 18 instance (`octoconv-db`, already running as a shared Docker container) through the same `Repo.AddOutput` → `Repo.MarkDone` code path a real worker uses:

```
$ psql "postgres://octo:octo-pass@localhost:5434/octo_db" -tAc \
    "SELECT job_id FROM job_outputs WHERE object_key LIKE 'results/d13/%-0-out.webp' ORDER BY job_id DESC LIMIT 1"
feaf9b7b-b2f1-48ef-a7d0-41b8613f159f
$ psql "postgres://octo:octo-pass@localhost:5434/octo_db" -tAc \
    "SELECT status FROM jobs WHERE id='feaf9b7b-b2f1-48ef-a7d0-41b8613f159f'"
done
$ psql "postgres://octo:octo-pass@localhost:5434/octo_db" -tAc \
    "SELECT count(*) FROM job_outputs WHERE job_id='feaf9b7b-b2f1-48ef-a7d0-41b8613f159f' AND role='primary'"
1
```

This job/output pair was written by `TestMarkDoneAcceptsSinglePrimaryOutput`'s `addPrimaryOutput` call and left in the database (that test has no `t.Cleanup`), giving a literal, live-Postgres proof of the exact query the plan's `<verification>` section specifies.

## Self-Check: PASSED

All modified files confirmed present on disk (`internal/jobs/repo.go`, `internal/jobs/output_role_test.go`, SUMMARY.md itself); all three commit hashes (`2043f32`, `15bffb3`, `428d59d`) confirmed present in `git log --oneline --all`.
