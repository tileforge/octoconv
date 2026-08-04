---
phase: 52-quota-enforcement-intra-class-fairness
plan: 07
subsystem: api
tags: [go, cli, quota, engine-validation, operator-tooling]

# Dependency graph
requires:
  - phase: 52-01
    provides: "client_limits table, clients.enforcement_mode, QUOT-01 storage contract (NULL/absent row = unlimited)"
provides:
  - "convert.AllEngines()/convert.IsKnownEngine() -- the single compile-time source of truth for the seven engine-class strings, with a registry-derived completeness guard"
  - "cmd/manage-clients set-limit fails closed on an unknown engine token before any database write"
affects: [operator-tooling, quota-enforcement]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Completeness guard mirroring queue.TestAllConvertQueuesCoversEveryEngine: assert exact membership count + registry cross-check, so a new engine class can't silently become un-validatable"
    - "Fail-closed CLI validation immediately after reading the raw os.Args token, before any parsing/DB call"

key-files:
  created:
    - cmd/manage-clients/main_test.go
  modified:
    - internal/convert/convert.go
    - internal/convert/convert_test.go
    - cmd/manage-clients/main.go

key-decisions:
  - "IsKnownEngine is exact and case-sensitive with no NormalizeFormat call -- engine-class strings are stored verbatim in jobs.engine/client_limits.engine, and normalization would silently accept a typo variant like \"Image\" and reproduce the exact defect this plan closes"
  - "Validation is added ONLY to set-limit, not clear-limit or show-limits -- a row with a typo'd engine can only exist if written before this check existed, and validating the delete path would make such a row permanently unremovable through the CLI; this is a deliberate divergence from 52-REVIEW.md WR-08's suggestion, recorded in a comment at the clear-limit case"
  - "No DB-level engine CHECK/allow-list was added (nor considered) -- 0013_client_limits.sql already documents why: a stale allow-list there would abort INSERT INTO jobs for a legitimate new engine class, breaking conversion rather than merely failing to enforce a limit"

requirements-completed: [QUOT-01]

# Metrics
duration: ~25min
completed: 2026-08-03
---

# Phase 52 Plan 07: Fail-closed engine validation in manage-clients set-limit Summary

**`manage-clients set-limit` now rejects an unknown engine token (e.g. the WR-08 `images` typo) before touching the database, backed by a single compile-time source of truth (`convert.AllEngines()`/`IsKnownEngine()`) with a registry-derived completeness guard; `clear-limit`/`show-limits` deliberately stay unvalidated so a pre-existing bad row can still be found and removed.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-08-03
- **Tasks:** 2/2
- **Files modified:** 4 (3 modified, 1 created)

## Accomplishments

- `internal/convert/convert.go` gained `AllEngines() []string` (fixed, fresh-constructed order) and `IsKnownEngine(engine string) bool` (exact, case-sensitive membership), sitting right after the existing engine-class const block they expose.
- `TestAllEnginesCoversEveryEngineClass` in `internal/convert/convert_test.go` mirrors `queue.TestAllConvertQueuesCoversEveryEngine`: asserts `len(AllEngines()) == 7` exactly, no duplicates/no extras, `IsKnownEngine` agrees with membership, rejects `"images"`/`"Image"`/`""`/`"*"`/`"webhook"`, and cross-checks every `Default.Classes()` key is present in `AllEngines()` -- so a future engine class that registers a converter but isn't added to the list fails this test instead of silently becoming un-validatable at the CLI.
- `cmd/manage-clients/main.go` gained `validateEngine(engine string) error`, called in the `set-limit` case immediately after `engine := os.Args[3]` and before all three `parseOptional*` calls and `repo.SetLimit` -- earliest possible, fail-closed at the edge. On rejection: `log.Fatalf("invalid engine: %v", err)`, matching the surrounding error style.
- `usage()`'s `set-limit` synopsis now renders the closed engine set from `convert.AllEngines()` instead of a bare `<engine>` placeholder, with zero duplicated engine-name literals.
- `cmd/manage-clients/main_test.go` (new, first test file for this command) adds `TestValidateEngine` and `TestValidateEngineAcceptsExactlyKnownSet` -- both run with no `DATABASE_URL`/`API_KEY_SALT` set, since `validateEngine` is pure.
- Live behavioral verification against the running `octoconv-db` Postgres container (see below): a typo'd engine is rejected with zero rows written; a real engine reaches `repo.SetLimit` and fails only on the expected FK violation (nonexistent client id), proving the check does not block valid input.

## Task Commits

1. **Task 1: A single, guarded source of truth for the engine-class list** - `3980c4d` (feat)
2. **Task 2: Fail closed on an unknown engine in set-limit, before any database write** - `16485f1` (fix)

**Plan metadata:** committed separately after this SUMMARY (worktree mode -- orchestrator merges).

## Files Created/Modified

- `internal/convert/convert.go` - added `AllEngines()` and `IsKnownEngine()`, doc-commented against ever adding normalization
- `internal/convert/convert_test.go` - added `TestAllEnginesCoversEveryEngineClass` completeness guard
- `cmd/manage-clients/main.go` - added `validateEngine`, wired into `set-limit`, documented the deliberate non-validation of `clear-limit`/`show-limits`, made `usage()`'s engine placeholder derive from `convert.AllEngines()`
- `cmd/manage-clients/main_test.go` (new) - `TestValidateEngine`, `TestValidateEngineAcceptsExactlyKnownSet`

## Decisions Made

- `IsKnownEngine` is exact/case-sensitive, deliberately never calling `NormalizeFormat` (which exists for file formats, not engine classes) -- doing so would silently accept `"Image"` and reproduce the exact WR-08 defect.
- Validation lives only in `set-limit`; `clear-limit`/`show-limits` are intentionally left unvalidated (comment at the `clear-limit` case explains why), diverging from 52-REVIEW.md WR-08's literal suggestion to apply the same check everywhere.
- No DB-level engine allow-list was added -- `0013_client_limits.sql`'s existing comment already forecloses that option (would break `INSERT INTO jobs` for a new engine, not just fail to enforce a limit).

## Deviations from Plan

None - plan executed exactly as written, including the deliberate `clear-limit`/`show-limits` divergence from 52-REVIEW.md WR-08, which the plan itself calls for and required recording in a comment (done at the `clear-limit` case in `cmd/manage-clients/main.go`).

One incidental fix during Task 2 verification: the first `usage()` edit used `log.Fatalf` with a non-constant format string (the dynamically built engine list), which `go vet` correctly flagged (`non-constant format string in call to log.Fatalf`). Fixed by switching to `log.Fatal` (no formatting needed, since the message has no verbs). This is a Rule 1 (bug) fix, folded into Task 2's single commit since it was found and fixed before that task's commit, not a separate deviation needing its own entry.

## Issues Encountered

None beyond the `go vet` non-constant-format-string catch above, resolved before committing.

## Live Verification (recorded verbatim)

Run against the pre-existing, already-healthy `octoconv-db` container (`postgres://octo:octo-pass@localhost:5434/octo_db`, shared infra, not started or torn down by this plan):

```
$ DATABASE_URL="postgres://octo:octo-pass@localhost:5434/octo_db" API_KEY_SALT=x \
  go run ./cmd/manage-clients set-limit 00000000-0000-0000-0000-000000000000 images 5 - -
2026/08/03 12:45:48 invalid engine: "images" is not a known engine class (must be one of: image, document, html, audio, av, ocr, pandoc)
exit status 1

$ psql "$DATABASE_URL" -c "SELECT count(*) FROM client_limits WHERE engine = 'images';"
 count
-------
     0
(1 row)

$ DATABASE_URL="postgres://octo:octo-pass@localhost:5434/octo_db" API_KEY_SALT=x \
  go run ./cmd/manage-clients set-limit 00000000-0000-0000-0000-000000000000 image 5 - -
2026/08/03 12:45:59 set limit: set client limit: ERROR: insert or update on table "client_limits" violates foreign key constraint "client_limits_client_id_fkey" (SQLSTATE 23503)
exit status 1
```

The second run reaches `repo.SetLimit` and fails only on the expected FK violation for a nonexistent client id -- confirming `validateEngine` does not block a real engine class.

## Verification Summary

- `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` outputs nothing.
- `go test ./internal/convert/ ./cmd/manage-clients/ -count=1` exits 0 (no database configured for these two packages' tests).
- `git diff --name-only -- internal/queue/ deploy/chart/octoconv/templates/ | wc -l` -> `0` (FAIR-02 zero-diff fence respected).
- `grep -c '"image"' cmd/manage-clients/main.go` -> `0` (no duplicated engine-class literal).
- `git diff internal/convert/convert.go | grep '^+' | grep 'NormalizeFormat' | grep -v '^+[[:space:]]*//' | wc -l` -> `0` (only doc-comment mentions).

## Known Stubs

None.

## Threat Flags

None - all new surface (the `validateEngine` check itself) was anticipated and dispositioned in the plan's own `<threat_model>` (T-52-39..T-52-42); no additional surface introduced.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

WR-08 (52-VERIFICATION.md anti-pattern) is closed: `manage-clients set-limit` can no longer create a silently-inert `client_limits` row from an operator typo. `clear-limit`/`show-limits` remain intentionally permissive as an escape hatch for any row written before this validation existed. No further follow-up required from this plan; ready for the orchestrator to merge alongside sibling plan 52-06.

---
*Phase: 52-quota-enforcement-intra-class-fairness*
*Completed: 2026-08-03*
