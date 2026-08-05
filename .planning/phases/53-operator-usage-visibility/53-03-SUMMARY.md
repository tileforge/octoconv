---
phase: 53-operator-usage-visibility
plan: 03
subsystem: api
tags: [go, chi, interface-segregation, dependency-injection]

# Dependency graph
requires:
  - phase: 53-01
    provides: internal/jobs.UsageRow and (*jobs.Repo).UsageSummary
  - phase: 53-02
    provides: internal/clients.LimitWithMeta and (*clients.Repo).ListAllLimits
provides:
  - "api.UsageRepo and api.LimitsRepo — narrow interfaces on Server, wired through NewServer as required positional params"
  - "Server.usageRepo / Server.limitsRepo — never nil after construction, ready for plan 53-04's handler"
  - "fakeUsageRepo / fakeLimitsRepo in internal/api/handlers_test.go — capture-args-and-configurable-returns test doubles with call counters, reusable by every existing and future *_test.go in the package"
affects: [53-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-narrow-interfaces-one-concrete-repo (PresetRepo/PresetAdmin precedent) extended to UsageRepo/LimitsRepo: neither is folded into the existing jobs-scoped Repo interface"
    - "Required positional constructor params with zero defaulting for load-bearing dependencies, contrasted explicitly against the nil-safe SetCancelPublisher post-construction seam"

key-files:
  created: []
  modified:
    - internal/api/api.go
    - cmd/api/main.go
    - internal/api/handlers_test.go
    - internal/api/presets_handlers_test.go
    - internal/api/routes_test.go
    - internal/api/sse_test.go
    - internal/api/system_presets_handlers_test.go
    - internal/mcpserver/quota_admission_test.go

key-decisions:
  - "usageRepo/limitsRepo are required positional NewServer params with NO defaulting — a nil is a compile-time-visible call-site error, not a configurable state, matching the plan's explicit instruction to not model this on SetCancelPublisher's nil-safety"
  - "cmd/api/main.go passes the already-constructed jobsRepo (doubling as UsageRepo) and clientRepo (doubling as LimitsRepo) — no new repo constructed, no new env var, mirroring how presetRepo already appears twice for PresetRepo/PresetAdmin"

patterns-established:
  - "Pattern: when a plan's own grep undercounts NewServer-style call sites (a single-package fan-out that outgrew the file list originally scoped), the fix is mechanical positional-arg addition using the SAME already-established fakes/constructors, not a re-scope — verified by re-running the plan's own grep after the fix and confirming go build/go vet/go test are all clean"

requirements-completed: [OPSV-01, OPSV-02]

# Metrics
duration: ~30min
completed: 2026-08-05
---

# Phase 53 Plan 03: Wire UsageRepo/LimitsRepo into api.Server Summary

**`api.UsageRepo`/`api.LimitsRepo` narrow interfaces added to `internal/api/api.go`, wired as required positional `NewServer` params, and threaded through every call site in the codebase (8 files, not the 3 the plan anticipated) — zero routes, zero handlers, contract-and-wiring only.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-08-05T13:16:00Z (approx, per session start)
- **Completed:** 2026-08-05T13:41:19Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments
- `internal/api/api.go`: `UsageRepo` (`UsageSummary(ctx, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]jobs.UsageRow, error)`) and `LimitsRepo` (`ListAllLimits(ctx, clientID *uuid.UUID) ([]clients.LimitWithMeta, error)`) interfaces declared, matching plans 53-01/53-02's real method signatures exactly; existing `Repo` interface unchanged (0 grep hits for `UsageSummary`/`ListAllLimits` inside it)
- `Server` gained `usageRepo`/`limitsRepo` fields and `NewServer` gained two new required positional params (`usage UsageRepo, limits LimitsRepo`) inserted between `presetAdmin` and `resolver`, with zero defaulting logic
- `cmd/api/main.go` wires the already-constructed `jobsRepo` (as `UsageRepo`) and `clientRepo` (as `LimitsRepo`) — no new repo, no new env var
- `internal/api/handlers_test.go`: `fakeUsageRepo`/`fakeLimitsRepo` added, copying `fakeRepo`'s capture-args-and-configurable-returns shape including a `calls int` counter (load-bearing for plan 53-04's pre-DB-work validation assertions)
- Every `NewServer` call site in the repo updated (`cmd/api/main.go`, and 7 test files: `handlers_test.go`, `presets_handlers_test.go`, `routes_test.go`, `sse_test.go`, `system_presets_handlers_test.go`, plus `internal/mcpserver/quota_admission_test.go` which passes the real `*jobs.Repo`/`*clients.Repo`) — `go build ./...`, `go vet ./...` and `go test ./internal/api/ -count=1` all green

## Task Commits

Each task was committed atomically:

1. **Task 1: Declare UsageRepo / LimitsRepo and widen NewServer** - `22588c4` (feat)
2. **Task 2: Update all NewServer call sites and add the two fakes** - `780b294` (feat)

**Plan metadata:** (this commit, following)

## Files Created/Modified
- `internal/api/api.go` - `UsageRepo`/`LimitsRepo` interfaces, `Server.usageRepo`/`limitsRepo` fields, widened `NewServer` signature and struct literal, `clients` import added
- `cmd/api/main.go` - `api.NewServer(...)` call passes `jobsRepo` (usage slot) and `clientRepo` (limits slot); comment explaining the deliberate double-use
- `internal/api/handlers_test.go` - `fakeUsageRepo`/`fakeLimitsRepo` types added near `fakeRepo`; every `NewServer(...)` call in this file (there are ~30, not the 1 `newTestServer` helper the plan's read_first anticipated) updated with the new positional args
- `internal/api/presets_handlers_test.go` - `NewServer(...)` call updated (discovered call site, not in plan's `files_modified`)
- `internal/api/routes_test.go` - `NewServer(...)` call updated (discovered call site)
- `internal/api/sse_test.go` - `NewServer(...)` call updated (discovered call site)
- `internal/api/system_presets_handlers_test.go` - `newOperatorGateTestServer` and two other `NewServer(...)` calls in this file updated
- `internal/mcpserver/quota_admission_test.go` - `api.NewServer(...)` call updated to pass the real `jobsRepo`/`clientsRepo` it already constructs (discovered call site)

## Decisions Made
- Kept `usageRepo`/`limitsRepo` as required positional params with zero defaulting, per the plan's explicit instruction — a nil here is a call-site programming error, never a runtime-configurable state.
- `cmd/api/main.go` reuses `jobsRepo`/`clientRepo` rather than constructing anything new, keeping the change additive and env-var-free.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated 5 additional `NewServer` call sites beyond the plan's anticipated 3**
- **Found during:** Task 2, after the plan's own three-call-site edits, `go vet ./...` still failed
- **Issue:** The plan's `<read_first>` and `<acceptance_criteria>` assumed exactly three `NewServer` call sites (`cmd/api/main.go`, `handlers_test.go:newTestServer`, `system_presets_handlers_test.go:newOperatorGateTestServer`). Re-running the plan's own verification grep (`grep -rn "api.NewServer(\|= NewServer(\|return NewServer(" cmd/ internal/ --include='*.go'`) after the edits surfaced 8 files and roughly 38 individual call expressions — `internal/api/handlers_test.go` alone contains ~30 separate `NewServer(...)` calls beyond the one `newTestServer` helper, plus `presets_handlers_test.go`, `routes_test.go`, `sse_test.go`, two more calls in `system_presets_handlers_test.go`, and a DB-backed integration test in `internal/mcpserver/quota_admission_test.go`.
- **Fix:** Mechanically inserted `&fakeUsageRepo{}, &fakeLimitsRepo{}` (or, for `quota_admission_test.go`, the real `jobsRepo, clientsRepo` it already constructs) into the same positional slot at every remaining call site, using targeted `sed` substitutions on the handful of distinct argument-list shapes present, then hand-editing the one multi-line call in `quota_admission_test.go`. No new fakes, no new behavior, no architectural change — purely propagating the same two dependencies the plan already designed.
- **Files modified:** `internal/api/handlers_test.go`, `internal/api/presets_handlers_test.go`, `internal/api/routes_test.go`, `internal/api/sse_test.go`, `internal/api/system_presets_handlers_test.go`, `internal/mcpserver/quota_admission_test.go`
- **Verification:** `go build ./...`, `go vet ./...`, `go build ./cmd/...` all exit 0; `go test ./internal/api/ -count=1` and `go test ./internal/api/ -run TestRequireOperator -count=1 -v` both pass; re-running the plan's own call-site grep after the fix shows every listed call site now passes ten arguments; `gofmt -l internal/api/ cmd/api/ internal/mcpserver/` prints nothing
- **Committed in:** `780b294` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking — Rule 3)
**Impact on plan:** Pure mechanical propagation of the plan's own two new dependencies to call sites the plan's grep missed. No new fakes, no behavior change, no architectural decision required. `internal/api/routes.go` remains untouched and `go.mod`/`go.sum` remain unchanged, so the plan's own threat-model dispositions (T-53-03-04, T-53-03-SC) still hold.

## Issues Encountered
None beyond the call-site count deviation documented above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `api.UsageRepo`/`api.LimitsRepo` and `Server.usageRepo`/`limitsRepo` are in place; plan 53-04's handler can consume them directly.
- `fakeUsageRepo`/`fakeLimitsRepo` in `internal/api/handlers_test.go` are ready for plan 53-04's handler tests: both carry `calls int` counters so validation-failure tests can assert `calls == 0` (no DB work before validation, D-03).
- No route exists yet — `internal/api/routes.go` was deliberately not touched (verified: absent from `git diff --name-only`).
- `go.mod`/`go.sum` unchanged — zero new dependencies.
- No blockers for plan 53-04.

---
*Phase: 53-operator-usage-visibility*
*Completed: 2026-08-05*

## Self-Check: PASSED

- FOUND: internal/api/api.go
- FOUND: cmd/api/main.go
- FOUND: internal/api/handlers_test.go
- FOUND: internal/api/system_presets_handlers_test.go
- FOUND: .planning/phases/53-operator-usage-visibility/53-03-SUMMARY.md
- FOUND commit: 22588c4
- FOUND commit: 780b294
