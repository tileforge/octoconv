---
phase: 40-job-listing
plan: 02
subsystem: api
tags: [chi, http, pagination, keyset-cursor, jobs]

requires:
  - phase: 40-job-listing
    provides: "Repo.List (keyset SQL, client-scoped), jobs.JobSummary/ListFilter/Cursor, EncodeCursor/DecodeCursor"
provides:
  - "GET /v1/jobs handler (handleListJobs), scoped to the caller's client.ID"
  - "api.Repo interface widened with List"
  - "JobsListDefaultPageSize/JobsListMaxPageSize config knobs (env JOBS_LIST_DEFAULT_PAGE_SIZE/JOBS_LIST_MAX_PAGE_SIZE)"
affects: []

tech-stack:
  added: []
  patterns:
    - "Fail-closed 422-before-work query-param validation (status/engine) against registry-derived/const allow-sets, mirroring the upload path's idiom"
    - "Non-nil empty-slice initialization for JSON envelopes so an empty result marshals to [] not null"

key-files:
  created:
    - internal/api/jobs_list_handler.go
  modified:
    - internal/api/api.go
    - internal/api/routes.go
    - internal/api/handlers_test.go
    - cmd/api/main.go
    - .env.example

key-decisions:
  - "client.ID from auth.ClientFromContext is the SOLE client scope passed to Repo.List — no query param ever supplies a client id (JLIST-01, T-40-04)"
  - "?limit above JobsListMaxPageSize clamps to MAX rather than 422'ing (D-06); a value < 1 clamps to 1; absent/unparseable uses JobsListDefaultPageSize"
  - "progress_pct/started_at/finished_at keys are always present in each item (nullable) so the dashboard can rely on a stable shape; error_code is added only when status=failed and non-empty; download_url/opts are never included in the listing (kept on the per-job GET)"

patterns-established:
  - "Collection route (GET /jobs) registered alongside the existing /jobs/{id} route in the same /v1 group — no chi routing conflict"

requirements-completed: [JLIST-01, JLIST-02]

duration: 25min
completed: 2026-07-25
---

# Phase 40 Plan 02: Job Listing HTTP Endpoint Summary

**`GET /v1/jobs` — an auth-scoped, fail-closed-validated, keyset-paginated listing endpoint that exposes Plan 01's `Repo.List` read layer with the exact `{jobs, next_cursor}` envelope (no `download_url`/`opts`, `error_code` only on failed).**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-25T06:10:00Z
- **Completed:** 2026-07-25T06:35:00Z
- **Tasks:** 2
- **Files modified:** 6 (1 created, 5 modified)

## Accomplishments
- `api.Repo` interface widened with `List(ctx, clientID, ListFilter, *Cursor, limit)`; `Config`/`Server` gained `JobsListDefaultPageSize`/`JobsListMaxPageSize` (default 20/100, D-06), wired through `cmd/api/main.go` via `envInt64("JOBS_LIST_DEFAULT_PAGE_SIZE", 20)` / `envInt64("JOBS_LIST_MAX_PAGE_SIZE", 100)`, documented in `.env.example`
- `handleListJobs` (`internal/api/jobs_list_handler.go`): resolves the caller via `auth.ClientFromContext` and passes ONLY `client.ID` to `Repo.List`; validates `?status` against the `jobs.Status*` const set and `?engine` against `convert.Default.Classes()` — both fail-closed 422 BEFORE any DB work; clamps `?limit` to `[1, MAX]` (above-MAX clamps, not 422); decodes `?cursor` via `jobs.DecodeCursor` (400 on malformed); builds the `{jobs, next_cursor}` envelope with the exact D-02/D-04 item shape
- Route `GET /jobs` registered in the auth'd `/v1` group alongside the existing job routes (`internal/api/routes.go`)
- `fakeRepo.List` added to `handlers_test.go` with arg-capture fields (`listClientID`, `listFilter`, `listCursor`, `listLimit`) enabling scope/clamp assertions
- 9 new handler tests, all green: envelope shape (incl. `error_code` present-only-on-failed, no `download_url`/`opts`, `progress_pct` always-present), scope isolation, unknown-status/engine 422, limit clamp-to-max, limit default, malformed-cursor 400, next-cursor round-trip, exact empty-result body

## Task Commits

1. **Task 1: Extend Repo interface + Config page-size knobs + env wiring + fakeRepo.List** - `05ddd13` (feat)
2. **Task 2: handleListJobs — validation, clamp, envelope + item shape, route, tests** - `27fc794` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified
- `internal/api/jobs_list_handler.go` - `handleListJobs`: fail-closed validation, limit clamp, cursor decode, envelope + item builder
- `internal/api/api.go` - `Repo.List` added to the interface; `JobsListDefaultPageSize`/`JobsListMaxPageSize` on `Config`+`Server`, defaulted in `NewServer`
- `internal/api/routes.go` - `r.Get("/jobs", s.handleListJobs)` registered in the `/v1` group
- `internal/api/handlers_test.go` - `fakeRepo.List` + capture fields; 9 new `TestListJobs_*` tests
- `cmd/api/main.go` - `JobsListDefaultPageSize`/`JobsListMaxPageSize` wired via `envInt64`
- `.env.example` - `JOBS_LIST_DEFAULT_PAGE_SIZE`/`JOBS_LIST_MAX_PAGE_SIZE` documented

## Decisions Made
- `client.ID` from `auth.ClientFromContext` is the only client scope ever passed to `Repo.List` — validated by `TestListJobs_ScopedToCaller` at the handler seam (the SQL-layer leak proof lives in Plan 01's `TestListClientScopeLeak`).
- Empty `jobs` slice is explicitly initialized non-nil (`make([]map[string]any, 0, len(summaries))`) so an empty result marshals to `[]`, never `null`, per D-02.
- `?status`/`?engine` validation order runs BEFORE the limit/cursor parse and BEFORE any DB call, matching the project's fail-closed 422-before-work idiom used by the upload path.

## Deviations from Plan

None - plan executed exactly as written. Both tasks compiled clean on first pass; no auto-fixes, architectural changes, or scope additions were needed.

## Issues Encountered

None. `octoconv-db` (compose postgres, host port 5434) was left running from Plan 01; this plan's tests are all in-process `fakeRepo`-backed handler tests and did not require a live Postgres connection.

## User Setup Required

None - no external service configuration required. Two new env vars (`JOBS_LIST_DEFAULT_PAGE_SIZE`, `JOBS_LIST_MAX_PAGE_SIZE`) have safe defaults (20/100) baked into `NewServer`, so an unset `.env` in any existing deployment continues to work unchanged.

## Next Phase Readiness

- `GET /v1/jobs` is fully wired end-to-end: `Repo.List` (Plan 01) → `handleListJobs` (this plan) → route. JLIST-01/JLIST-02 are complete.
- No blockers. `progress_pct` rides along null until Phase 41 populates it, per 40-CONTEXT.md's scope boundary — this plan does not need to revisit that.

---
*Phase: 40-job-listing*
*Completed: 2026-07-25*

## Self-Check: PASSED

All created/modified files found on disk; both task commit hashes (05ddd13, 27fc794) found in git log.
