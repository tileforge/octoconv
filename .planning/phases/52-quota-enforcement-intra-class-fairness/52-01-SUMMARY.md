---
phase: 52-quota-enforcement-intra-class-fairness
plan: 01
subsystem: database
tags: [postgres, pgx, quota, migration, cli, manage-clients]

# Dependency graph
requires:
  - phase: 51-attribution-accounting-retention
    provides: client_usage_daily rollup table and its (client_id, engine, day) key shape, which client_limits mirrors one level shallower
provides:
  - client_limits table (client_id, engine) primary key, three nullable NULL-means-unlimited limit columns (max_concurrent, max_daily_bytes, max_daily_jobs)
  - clients.enforcement_mode column, default 'observe', two-value CHECK
  - jobs_client_engine_inflight_idx, the index the D-06 occupancy COUNT will read
  - internal/clients/limits.go — SetLimit/ClearLimit/GetLimit/ListLimits/SetEnforcementMode on *clients.Repo
  - manage-clients set-limit/clear-limit/show-limits/set-mode operator subcommands
affects: [52-02, 52-03, 52-04, 53-operator-usage-visibility]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "NULL/absent-row means unlimited, never a sentinel number (D-02) — enforced at every layer: SQL nullable columns with no default, Go *int/*int64 pointer fields, and CLI '-' token parsing"
    - "Full-row upsert (INSERT ... ON CONFLICT DO UPDATE SET every column) for a limits row — a nil pointer clears a previously set limit, documented at the call site to avoid a partial-update misreading"

key-files:
  created:
    - internal/db/migrations/0013_client_limits.sql
    - internal/clients/limits.go
    - internal/clients/limits_test.go
  modified:
    - cmd/manage-clients/main.go

key-decisions:
  - "client_limits carries no CHECK (engine IN (...)) allow-list, mirroring 0011's precedent, sharpened because this table is read inside Repo.Create's transaction ahead of the jobs insert — a stale allow-list would abort job creation for a new engine, not just leave it unmetered"
  - "max_concurrent = 0 is a distinct, legal value (explicit deny) from NULL (unlimited) — round-tripped as a non-nil pointer to 0 vs. a nil pointer at every layer"
  - "SetLimit is full-row replace semantics, not merge — a second SetLimit call with a nil field clears a previously set limit on that column"

requirements-completed: [QUOT-01]

# Metrics
duration: ~50min
completed: 2026-08-03
---

# Phase 52 Plan 01: Client Limits Storage & Operator CLI Summary

**`client_limits` table (client_id, engine) with NULL-means-unlimited limit columns, `clients.enforcement_mode` default 'observe', and four new `manage-clients` subcommands (set-limit/clear-limit/show-limits/set-mode) to write and read them — no enforcement logic yet.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-08-03T05:12:00Z (approx, worktree base reset)
- **Completed:** 2026-08-03T05:19:43Z
- **Tasks:** 3
- **Files modified:** 4 (3 created, 1 modified)

## Accomplishments

- Migration 0013 creates `client_limits` (client_id, engine) with three nullable NULL-means-unlimited limit columns, no CHECK allow-list on `engine`, and adds `clients.enforcement_mode` defaulting to `'observe'` — every existing client's behavior is unchanged
- `jobs_client_engine_inflight_idx` added for the D-06 concurrency-occupancy COUNT the admission gate (a later plan) will run under an advisory lock
- `internal/clients/limits.go`: a small repository (SetLimit/ClearLimit/GetLimit/ListLimits/SetEnforcementMode) on `*clients.Repo`, following the package's existing `r.pool.QueryRow`/`Exec` + `fmt.Errorf` idiom, with `*int`/`*int64` pointer fields so NULL survives the round trip as `nil`
- `manage-clients` gained four subcommands so an operator can set, clear, inspect, and mode-switch quota limits entirely through the existing CLI, with `-` producing SQL NULL and `0` producing an explicit deny — both live-verified against the local compose stack

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0013 — client_limits table, clients.enforcement_mode, in-flight count index** - `8defee0` (feat)
2. **Task 2: internal/clients/limits.go — limits repository with NULL-preserving pointer types** - `9baf027` (feat)
3. **Task 3: manage-clients set-limit / clear-limit / show-limits / set-mode subcommands** - `d4db9eb` (feat)

_No TDD tasks in this plan — all `type="auto"` per PLAN.md frontmatter._

## Files Created/Modified

- `internal/db/migrations/0013_client_limits.sql` - `client_limits` table, `clients.enforcement_mode` column, `jobs_client_engine_inflight_idx`
- `internal/clients/limits.go` - `Limit` struct + `SetLimit`/`ClearLimit`/`GetLimit`/`ListLimits`/`SetEnforcementMode` on `*clients.Repo`
- `internal/clients/limits_test.go` - DATABASE_URL-gated round-trip proof that NULL survives write/read as nil, 0 stays distinguishable from NULL, SetLimit replaces (not merges), ClearLimit/ListLimits, SetEnforcementMode Go-side validation + ErrNotFound mapping
- `cmd/manage-clients/main.go` - four new subcommands (`set-limit`, `clear-limit`, `show-limits`, `set-mode`), widened `usage()` and package doc comment

## Decisions Made

None beyond what CONTEXT.md's decisions (D-01, D-02, D-04, D-13) already fixed — this plan implemented them as specified. Column types, the migration ordinal (0013, next after 0012), and the exact `manage-clients` subcommand/flag shape were Claude's Discretion per CONTEXT.md and were resolved per the PLAN.md's own interface spec.

## Deviations from Plan

None - plan executed exactly as written. All acceptance criteria in PLAN.md Tasks 1-3 were verified directly (grep counts, `go build`/`go vet`/`gofmt`, live migration apply + idempotent re-run, live `manage-clients` round trips against the local `octoconv-db` compose container on port 5434).

## Issues Encountered

None. One incidental stray build artifact (`./manage-clients` binary from a local `go build ./cmd/manage-clients/` verification run) was created outside the repo's tracked paths and removed before staging — not committed.

## User Setup Required

None - no external service configuration required. Verification used the pre-existing local `octoconv-db`/`octoconv-redis`/`octoconv-minio` compose containers already running on this machine (ports 5434/6379/9100).

## Next Phase Readiness

- `client_limits` and `clients.enforcement_mode` are live on the local dev database and ready for Plan 03's admission gate (`internal/jobs/admission.go`) to read inside `Repo.Create`'s transaction.
- `internal/clients/limits.go`'s exported signatures match the PLAN.md `<interfaces>` contract exactly (`Limit`, `SetLimit`, `ClearLimit`, `GetLimit`, `ListLimits`, `SetEnforcementMode`) — downstream plans (52-03, 52-04) can compile against them without renaming.
- No enforcement, no worker-side change, no chart file touched — confirmed by `git diff --stat` against `deploy/chart/octoconv/templates/` returning empty for this plan's commits, matching PLAN.md's stated success criteria.

---
*Phase: 52-quota-enforcement-intra-class-fairness*
*Completed: 2026-08-03*

## Self-Check: PASSED

All created files verified present (internal/db/migrations/0013_client_limits.sql, internal/clients/limits.go, internal/clients/limits_test.go); all three task commits verified present in git log (8defee0, 9baf027, d4db9eb).
