---
phase: 57-operation-dimension-output-roles
plan: 03
subsystem: database
tags: [go, postgres, migrations, pgx, jobs]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    plan: 01
    provides: "errcode.UnsupportedOperation sync error code that 57-04 will use to fail-closed on operation='sanitize' at the API layer"
provides:
  - "internal/db/migrations/0015_operation_and_output_roles.sql — job_outputs.role (closed CHECK, DB DEFAULT 'primary') + jobs_operation_check widened to admit 'sanitize'"
  - "jobs.Output.Role field, populated from job_outputs.role via Repo.Outputs"
  - "internal/jobs/output_role_test.go — DB-gated proof suite: default-without-backfill on live PostgreSQL 18, closed role allowlist, widened operation CHECK, multi-output role round-trip"
affects: [57-05, 57-06, 60]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "D-16: DB column DEFAULT is the sole source of truth for a role/status-like column; the Go INSERT path (AddOutput) never names the column, closing the class of defect where an API-side default drifts from the DB default"
    - "DROP CONSTRAINT / ADD CONSTRAINT (no IF EXISTS) applied to jobs_operation_check for the first time, mirroring the jobs_engine_check precedent (0005/0006/0009/0010) rather than 0014's IF EXISTS deviation, which 52-REVIEW.md flagged as tech debt"

key-files:
  created:
    - internal/db/migrations/0015_operation_and_output_roles.sql
    - internal/jobs/output_role_test.go
  modified:
    - internal/jobs/jobs.go
    - internal/jobs/repo.go

key-decisions:
  - "AddOutput intentionally does NOT write role, even on the ON CONFLICT DO UPDATE arm — the DB default is the only writer of this column until Phase 60 adds a dedicated role-write path, and the doc comment on AddOutput explicitly flags the guarantee that future path must carry (no existing caller silently zero-values a row's role on retry)"
  - "Outputs' ORDER BY ordinal is left unchanged (not extended to role) — ordinal is already a stable sort key via job_outputs_job_ordinal_key (0012), and primary-output selection is role-driven at the handler layer, not positional"
  - "presets.operation is deliberately NOT widened to 'sanitize' in this migration — no preset for it has been requested; the migration's header comment records this as an acknowledged divergence between the two operation enums, not an oversight"

patterns-established:
  - "TestOutputRoleDefaultAppliesToPreExistingRows: a live PostgreSQL-version-gated proof (server_version_num >= 180000, t.Skipf otherwise) that an additive NOT NULL DEFAULT column needs no backfill — evidence over restated Postgres folklore"

requirements-completed: [ROLE-01]

# Metrics
duration: ~35min
completed: 2026-08-09
---

# Phase 57 Plan 03: job_outputs.role and jobs.operation='sanitize' Summary

**Additive migration 0015 gives every `job_outputs` row a DB-defaulted, closed-enum `role` column and widens `jobs_operation_check` to admit `sanitize`, with `Repo.Outputs` now surfacing the role and a live-PostgreSQL-18 test suite proving the DB default needs no backfill.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-08-09T17:34:58Z
- **Tasks:** 3/3 completed
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- Wrote and applied `internal/db/migrations/0015_operation_and_output_roles.sql` against the live Postgres instance (already holding a job_outputs row seeded before the migration ran) — `job_outputs.role text NOT NULL DEFAULT 'primary' CHECK (role IN ('primary','report','preview','thumbnail'))`, plus `jobs_operation_check` widened from five to six legal `operation` values via the same bare `DROP CONSTRAINT`/`ADD CONSTRAINT` pattern already proven live on `jobs_engine_check`.
- Confirmed live, not asserted from general Postgres knowledge, that the pre-existing row (and every row inserted with the pre-migration column list) reads back as `role='primary'` with zero backfill statements run.
- Added `Output.Role string` to `internal/jobs/jobs.go` and wired it into `Repo.Outputs`' SELECT/Scan; `AddOutput` was deliberately left untouched (D-16) with its doc comment now explaining why and what Phase 60 will need before it can start writing `role`.
- Added `internal/jobs/output_role_test.go` (5 new DB-gated tests, all green against the live PostgreSQL 18 instance, zero skips): default-without-Go-layer-help, default-on-pre-existing-row-shape (version-gated), closed-CHECK rejection + all four legal values, widened `jobs_operation_check` accepting `sanitize` / rejecting garbage, and multi-output role round-trip in ordinal order.

## Task Commits

1. **Task 1: Migration 0015 — job_outputs.role и расширение jobs_operation_check** - `f328857` (feat) — `internal/db/migrations/0015_operation_and_output_roles.sql`
2. **Task 2: Output.Role в модели и role в SELECT Repo.Outputs** - `2efc7d7` (feat) — `internal/jobs/jobs.go`, `internal/jobs/repo.go`
3. **Task 3: DB-тесты дефолта роли, закрытого CHECK и расширенного jobs_operation_check** - `20f45b6` (test) — `internal/jobs/output_role_test.go`

## Files Created/Modified

- `internal/db/migrations/0015_operation_and_output_roles.sql` - additive migration: `job_outputs.role` column + widened `jobs_operation_check`; header comment documents both intentional divergences (DB accepts `sanitize` before the registry does; `presets.operation` not widened)
- `internal/jobs/jobs.go` - `Output.Role string` field with a doc-comment note that the DB, not this struct, owns the default
- `internal/jobs/repo.go` - `Outputs` SELECT/Scan gains `role` (scanned directly into a plain `string`, not `*string`+`deref()`, since the column is `NOT NULL`); `AddOutput`'s doc comment extended to explain why `role` is absent from both the INSERT column list and the `ON CONFLICT` SET clause
- `internal/jobs/output_role_test.go` - 5 new DB-gated tests (213 lines) proving the default, the closed allowlist, the widened operation CHECK, and multi-row role round-trip; reuses the existing `createTestJob`/`createTestClient`/`newTestRepo` helpers from `list_test.go`/`repo_test.go` rather than introducing new ones

## Decisions Made

- **`AddOutput` stays role-blind by design (D-16).** The plan's own framing was followed literally: the DB default is the single source of truth, so no Go-side write path for `role` exists yet. The doc comment added to `AddOutput` explicitly calls out the guarantee a future Phase-60 write path must carry — don't let the upsert's `ON CONFLICT DO UPDATE` arm start silently zeroing an existing row's role on a retried conversion.
- **Reused the existing `createTestJob` helper from `list_test.go`** instead of adding a duplicate — the plan's `<read_first>` pointed at `repo_test.go`'s helpers but the actual name collision was with `list_test.go`, caught immediately by the Go compiler (`createTestJob redeclared in this block`) and fixed by adopting the existing four-argument signature (`t, r, clientID, engine`) everywhere in the new file.
- **`server_version_num` gate uses `t.Skipf`, not a hard failure**, per the plan's explicit instruction — the proof is scoped to PostgreSQL 18 and honestly says so if run against an older major version (not exercised in this run: the live instance is 180004).

## Deviations from Plan

**1. [Rule 3 - Blocking] Fixed a Bash-tool worktree cwd-drift mid-execution**

- **Found during:** Task 1, before writing the migration file
- **Issue:** Early in this session, several `Bash` calls used an explicit `cd /Users/apaderin/dev/octoconv && ...` which moved the shell into the shared main checkout instead of this agent's isolated worktree (`.../.claude/worktrees/agent-a9c07606fb31ca657`). A subsequent `git rev-parse --show-toplevel`/`git rev-parse HEAD` check (run without any `cd`, since the tool resets cwd between calls) revealed the worktree's own HEAD was still at a stale pre-Phase-57 commit (`175a738`, "Phase 53"), not the expected wave-1 tracking commit (`a37b7567`) — the mandatory `<worktree_branch_check>` merge-base check had already caught this and required the one sanctioned `git reset --hard a37b7567485ac981d9ae0a64928ea7b1210972e1`, run only after confirming `git status --short` was clean.
- **Fix:** Re-verified the worktree root via `git rev-parse --show-toplevel` (with no `cd`) before every subsequent file operation, and re-read every file needed for this plan directly under the worktree-prefixed absolute path (`/Users/apaderin/dev/octoconv/.claude/worktrees/agent-a9c07606fb31ca657/...`) rather than the shared-checkout path. All migration/repo/jobs files were confirmed byte-identical between the two checkouts for the historical files this plan depends on (0001–0014) before proceeding, and the `Write`/`Edit` tools themselves refused two attempts to touch the shared-checkout path, which is what surfaced the drift in the first place.
- **Files modified:** None beyond the plan's own files — this was a tooling-path correction, not a code change.
- **Verification:** `git rev-parse --show-toplevel` and `git rev-parse HEAD` (no `cd`) confirmed the worktree root and that HEAD sat on `a37b7567` before Task 1 began; all three task commits below were made from that corrected state.
- **Committed in:** N/A (no code changed; caught before Task 1's commit).

---

**Total deviations:** 1 auto-fixed (1 blocking, tooling-only)
**Impact on plan:** None on shipped code — the drift was caught and corrected before any file was written to the wrong location, and no wrong-location writes ever succeeded (the `Write` tool's own worktree-path guard blocked both attempts).

## Issues Encountered

None beyond the cwd-drift above.

## User Setup Required

None - no external service configuration required. The live PostgreSQL/Redis/MinIO compose stack used for verification (`docker compose up -d postgres redis minio createbucket`) is local dev infrastructure, already documented in `README.md`.

## Next Phase Readiness

- `job_outputs.role` exists on the live schema with a closed four-value CHECK and a proven DB default — 57-05's `MarkDone` invariant (a `done` job has exactly one `primary` output) and 57-06's role-driven `handleGetJob` selection now have a column to enforce/select against.
- `jobs.operation` accepts `sanitize` at the DB layer; 57-04's API-side rejection (422 `unsupported_operation`, using the `errcode.UnsupportedOperation` constant already landed by plan 01) is the other half of the intentional divergence this migration documents.
- `AddOutput` remains role-blind; Phase 60 (or an earlier plan needing to write `role='report'`) must add a dedicated write path and explicitly guard against the existing upsert silently overwriting an already-set role — this migration's Task 2 doc comment on `AddOutput` states that requirement so it is not rediscovered from scratch.
- No blockers for the rest of wave 2 or for plan 57-04/57-05/57-06.

## Acceptance Criteria — literal command output

Task 1:

```
$ grep -c "ADD COLUMN role text NOT NULL DEFAULT 'primary'" internal/db/migrations/0015_operation_and_output_roles.sql
1
$ grep -c "role IN ('primary', 'report', 'preview', 'thumbnail')" internal/db/migrations/0015_operation_and_output_roles.sql
1
$ grep -c "DROP CONSTRAINT jobs_operation_check" internal/db/migrations/0015_operation_and_output_roles.sql
1
$ grep -c "'convert', 'extract', 'archive', 'inspect', 'render', 'sanitize'" internal/db/migrations/0015_operation_and_output_roles.sql
1
$ grep -c 'presets' internal/db/migrations/0015_operation_and_output_roles.sql
2
$ grep -c 'ALTER TABLE presets' internal/db/migrations/0015_operation_and_output_roles.sql
0
$ DATABASE_URL=... go run ./cmd/migrate
2026/08/09 20:29:18 migrations applied   # exit 0
$ psql "$DATABASE_URL" -tAc "SELECT column_name, is_nullable, column_default FROM information_schema.columns WHERE table_name='job_outputs' AND column_name='role'"
role|NO|'primary'::text
$ psql "$DATABASE_URL" -tAc "SELECT column_default FROM information_schema.columns WHERE table_name='job_outputs' AND column_name='role'"
'primary'::text
$ psql "$DATABASE_URL" -tAc "SELECT count(*) FROM job_outputs WHERE role IS NULL"
0
$ psql "$DATABASE_URL" -tAc "SELECT role FROM job_outputs WHERE job_id='22222222-2222-2222-2222-222222222222'"   # row seeded BEFORE the migration ran
primary
```

Task 2:

```
$ grep -A 8 'type Output struct' internal/jobs/jobs.go | grep -c 'Role *string'
1
$ grep -c 'SELECT ordinal, object_key, filename, format, size_bytes, content_type, role' internal/jobs/repo.go
1
$ grep -c '&o.Role' internal/jobs/repo.go
1
$ grep -v '^\s*//' internal/jobs/repo.go | grep -c 'INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)'
1
$ grep -v '^\s*//' internal/jobs/repo.go | grep -c 'ORDER BY ordinal`'
2
$ go build ./...
(clean, exit 0)
$ go vet ./internal/jobs/
(clean, exit 0)
$ gofmt -l internal/jobs
(empty)
```

Task 3:

```
$ DATABASE_URL=... go test ./internal/jobs/ -run 'TestAddOutputWithoutRole|TestOutputRoleDefault|TestOutputRoleRejects|TestJobsOperationCheckAcceptsSanitize|TestOutputsReturnsRole' -count=1 -v
=== RUN   TestAddOutputWithoutRoleReadsBackAsPrimary
--- PASS: TestAddOutputWithoutRoleReadsBackAsPrimary (0.08s)
=== RUN   TestOutputRoleDefaultAppliesToPreExistingRows
--- PASS: TestOutputRoleDefaultAppliesToPreExistingRows (0.03s)
=== RUN   TestOutputRoleRejectsValueOutsideAllowlist
--- PASS: TestOutputRoleRejectsValueOutsideAllowlist (0.04s)
=== RUN   TestJobsOperationCheckAcceptsSanitize
--- PASS: TestJobsOperationCheckAcceptsSanitize (0.02s)
=== RUN   TestOutputsReturnsRoleForEveryOutput
--- PASS: TestOutputsReturnsRoleForEveryOutput (0.02s)
PASS
ok  	github.com/apaderin/octoconv/internal/jobs	1.250s
$ DATABASE_URL=... go test ./internal/jobs/ -count=1
ok  	github.com/apaderin/octoconv/internal/jobs	3.873s
$ grep -c 'server_version_num' internal/jobs/output_role_test.go
3
$ grep -c 'jobs_operation_check' internal/jobs/output_role_test.go
3
$ grep -c '"sanitize"' internal/jobs/output_role_test.go
1
$ grep -c 'INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)' internal/jobs/output_role_test.go
1
$ gofmt -l internal/jobs
(empty)
```

Overall verification:

```
$ go build ./...                                              -> clean
$ go vet ./...                                                 -> clean
$ gofmt -l .                                                    -> (empty)
$ DATABASE_URL=... go test ./internal/jobs/ -count=1            -> ok, 3.752s
$ git diff --name-only internal/jobs/ | grep -c 'repo_test.go\|usage_test.go\|retention_test.go\|admission_test.go'
0
$ psql "$DATABASE_URL" -tAc "SELECT role, count(*) FROM job_outputs GROUP BY role ORDER BY role"
preview|2
primary|16
report|4
thumbnail|2
```

Note on the last query: the plan's literal verification command (`SELECT count(*) FROM job_outputs WHERE role <> 'primary'` immediately after migration) was satisfied at the moment it was checked — the pre-migration-seeded row and every row present right after `go run ./cmd/migrate` completed were all `'primary'` (see Task 1's `role IS NULL` = `0` and the seeded row's explicit `primary` readback above). The non-`primary` rows shown in this final query are Task 3's own test fixtures (`TestOutputRoleRejectsValueOutsideAllowlist`'s four legal-value inserts and `TestOutputsReturnsRoleForEveryOutput`'s `report` row), inserted deliberately by the test suite itself, not evidence of any backfill or migration defect.

## Self-Check: PASSED

All created/modified files confirmed present on disk; all three task commit hashes (`f328857`, `2efc7d7`, `20f45b6`) confirmed present in `git log --oneline`.
