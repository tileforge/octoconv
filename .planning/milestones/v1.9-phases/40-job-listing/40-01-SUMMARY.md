---
phase: 40-job-listing
plan: 01
subsystem: database
tags: [postgres, pgx, keyset-pagination, cursor, jobs]

requires:
  - phase: 38-schema-foundation
    provides: "progress_pct/eta_seconds/source_duration_seconds/progress_updated_at columns on jobs (nullable, no-default)"
provides:
  - "jobs.JobSummary + jobs.ListFilter types"
  - "opaque base64 cursor codec (EncodeCursor/DecodeCursor)"
  - "Repo.List(ctx, clientID, filter, cursor, limit) — client-scoped, keyset-paginated, N+1-free read"
affects: [40-02-job-listing-endpoint]

tech-stack:
  added: []
  patterns:
    - "Opaque keyset cursor: base64(RFC3339Nano-timestamp|uuid), decoupling the API contract from internal ordering columns"
    - "limit+1 probe row to detect next-page existence without a separate COUNT query"

key-files:
  created:
    - internal/jobs/cursor.go
    - internal/jobs/cursor_test.go
    - internal/jobs/list_test.go
  modified:
    - internal/jobs/jobs.go
    - internal/jobs/repo.go

key-decisions:
  - "Cursor encoding: base64(RawURLEncoding) over `RFC3339Nano-timestamp|uuid`, split on the single '|' — RFC3339Nano never contains '|' so the split is unambiguous"
  - "client_id=$1 is unconditional in Repo.List's SQL WHERE (never a Go-side filter); proven, not inferred, by a dedicated two-client leak test"
  - "Reused jobs_client_created_idx (client_id, created_at DESC); no new migration/index added, per D-09's default (no proven EXPLAIN problem)"

patterns-established:
  - "Keyset pagination via row-value comparison `(created_at, id) < ($N, $N)`, ORDER BY created_at DESC, id DESC — the only stable ordering under continuous-insert load"

requirements-completed: [JLIST-01, JLIST-02]

duration: 20min
completed: 2026-07-25
---

# Phase 40 Plan 01: Job Listing Read Layer Summary

**Client-scoped keyset-paginated `Repo.List` for job listing: one bound-parameter SQL query per page, an opaque base64 cursor codec, and a `JobSummary` row type — with a dedicated two-client leak test proving cross-client isolation at the SQL layer.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-25T06:13:00+03:00
- **Completed:** 2026-07-25T06:16:00+03:00
- **Tasks:** 2 (both TDD)
- **Files modified:** 5 (2 modified, 3 created)

## Accomplishments
- `jobs.JobSummary` (per-row listing shape, `ProgressPct *int` nullable) and `jobs.ListFilter` (Status/Engine, empty-means-unset) types
- Opaque cursor codec (`Cursor`, `EncodeCursor`, `DecodeCursor`) that round-trips losslessly at RFC3339Nano precision and rejects empty/non-base64/structurally-wrong/tampered input with a wrapped error, never a panic
- `Repo.List` — exactly one `r.pool.Query` per page: `WHERE client_id=$1 [AND status=$][AND engine=$][AND (created_at,id)<($,$)] ORDER BY created_at DESC, id DESC LIMIT $limit+1`, all values bound `$N` params
- limit+1 probe row correctly detected and dropped; next cursor built from the last returned row only when a further page exists
- DB test suite incl. the REQUIRED `TestListClientScopeLeak` (two-client cross-client isolation), `TestListKeysetStableUnderInsert` (no duplicate/gap across a mid-paging insert), `TestListFilters`, `TestListLimitProbe`, `TestListProgressNull` — all green against live Postgres

## Task Commits

Each task followed RED → GREEN (TDD):

1. **Task 1: JobSummary + ListFilter types and the opaque cursor codec**
   - `6a8e0b4` test: add failing test for cursor codec (RED)
   - `acd2037` feat: implement JobSummary/ListFilter types and opaque cursor codec (GREEN)
2. **Task 2: Repo.List keyset query + client-scope + N+1-free + DB tests**
   - `e7cf567` test: add failing DB tests for Repo.List, incl. two-client leak test (RED)
   - `9b251da` feat: implement Repo.List keyset query (GREEN)

**Plan metadata:** (this commit)

## Files Created/Modified
- `internal/jobs/jobs.go` - added `JobSummary` and `ListFilter` types
- `internal/jobs/cursor.go` - `Cursor` type + `EncodeCursor`/`DecodeCursor` opaque codec
- `internal/jobs/cursor_test.go` - round-trip, empty/non-base64/structurally-wrong/tampered rejection tests
- `internal/jobs/repo.go` - added `Repo.List` (keyset query, client-scoped, filtered, single round-trip)
- `internal/jobs/list_test.go` - DB integration tests incl. `TestListClientScopeLeak`

## Decisions Made
- Cursor payload separator is a literal `|`; safe because `time.RFC3339Nano` output never contains `|` and `uuid.String()` is a fixed hex+hyphen format — a single `strings.SplitN(..., 2)` is unambiguous.
- `ListFilter` performs zero validation itself (empty-means-unset only) — Plan 02's handler owns fail-closed validation against the known status/engine sets, keeping this layer purely data-carrying per the plan's scope boundary.
- No new index added: `jobs_client_created_idx (client_id, created_at DESC)` is reused as-is; `id DESC` tie-break is a cheap residual filter on the same index scan, not extended into the index itself (D-09 default, no proven EXPLAIN problem).

## Deviations from Plan

None - plan executed exactly as written. Both tasks followed strict RED→GREEN TDD; no auto-fixes, architectural changes, or scope additions were needed.

## TDD Gate Compliance

Both tasks show a `test(...)` commit (RED) followed by a `feat(...)` commit (GREEN) in git log, confirmed above. No REFACTOR commits were needed (implementations were minimal and clean on first pass).

## Issues Encountered

None. `octoconv-db` (compose postgres, host port 5434) was not running at session start; brought up with `docker compose up -d postgres` only (no other services) and left running for Plan 02, per this plan's sequential-execution instructions.

## User Setup Required

None - no external service configuration required. (Postgres container `octoconv-db` is running locally on :5434 for subsequent DB-backed test runs in Plan 02.)

## Next Phase Readiness

- `Repo.List` is ready for Plan 02's `handleListJobs` HTTP handler (auth, opaque `?cursor=`, `?status=`/`?engine=` validation, `?limit=` clamp, response envelope) and the `Repo` interface addition in `internal/api/api.go`.
- No blockers. Scope was intentionally held to `internal/jobs` only — no handler/route/env code, no write path, no new index, no `download_url`/`opts` in the summary type (all deferred to Plan 02 or v2 per 40-CONTEXT.md).

---
*Phase: 40-job-listing*
*Completed: 2026-07-25*

## Self-Check: PASSED

All created/modified files found on disk; all 4 task commit hashes (6a8e0b4, acd2037, e7cf567, 9b251da) found in git log.
