---
phase: 56-jobrunner-consolidation-structured-error-codes
plan: 02
subsystem: api
tags: [error-codes, auth, ratelimit, go]

# Dependency graph
requires:
  - phase: 56-01
    provides: JobRunner consolidation groundwork (parallel wave, no file overlap with this plan)
provides:
  - "internal/errcode: closed 14-code sync (4xx/429) error-code vocabulary with All()/IsKnown()"
  - "internal/auth's 401/500 bodies now carry a machine-readable code from internal/errcode"
  - "internal/ratelimit's 429 body sources its code from errcode.RateLimited instead of a local literal"
affects: [56-03, mcpserver, frontend]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Closed vocabulary package (internal/errcode) mirroring internal/jobs's untyped-string-const enum idiom, with All()/IsKnown() built from one package-level var"
    - "internal/auth's locally duplicated writeError widened to accept a code parameter, keeping the flat {error,code} envelope (D-01) without importing internal/api"

key-files:
  created:
    - internal/errcode/errcode.go
    - internal/errcode/errcode_test.go
  modified:
    - internal/auth/middleware.go
    - internal/auth/middleware_test.go
    - internal/ratelimit/ratelimit.go
    - internal/ratelimit/ratelimit_test.go

key-decisions:
  - "internal/errcode declares exactly the fourteen codes named in the plan, one per client-actionable failure class (D-02); no additions or omissions were needed"
  - "QuotaExceeded and RateLimited constants pin the byte-identical wire values already shipping (quota_exceeded, rate_limited); TestWireValuesFrozen and TestRateLimitBodyCodeUnchanged assert against the LITERAL strings, not the constants, per T-56-06"
  - "internal/api, internal/worker, internal/metrics were left untouched -- writeError's signature change in internal/api is plan 56-03's scope, verified via git diff --name-only"

patterns-established:
  - "Sync error-code vocabulary lives in internal/errcode, imported by any package emitting a {error,code} body; async job-failure codes stay a separate vocabulary beside EngineDescriptor in internal/worker (D-04/D-06)"

requirements-completed: [ERRC-01]

# Metrics
duration: 6min
completed: 2026-08-08
---

# Phase 56 Plan 02: internal/errcode vocabulary + auth/ratelimit adoption Summary

**Closed 14-code sync error-code package (`internal/errcode`) adopted by `internal/auth` and `internal/ratelimit`, freezing the two wire values (`quota_exceeded`, `rate_limited`) that already ship.**

## Performance

- **Duration:** 6 min
- **Started:** 2026-08-08T05:55:30+03:00
- **Completed:** 2026-08-08T06:01:47+03:00
- **Tasks:** 2 completed (each run as RED/GREEN TDD pairs)
- **Files modified:** 6 (2 created, 4 modified)

## Accomplishments
- `internal/errcode` package: fourteen untyped string constants (`InvalidRequest`, `UnsupportedConversion`, `InvalidOptions`, `OptsNotApplicable`, `MissingFile`, `UnknownPreset`, `PayloadTooLarge`, `NotFound`, `InvalidCursor`, `AlreadyExists`, `Unauthorized`, `QuotaExceeded`, `RateLimited`, `InternalError`), `All()` (defensive-copy enumeration), `IsKnown()` (membership test); zero dependency on any other internal package, verified via `go list -deps`.
- `internal/auth/middleware.go`'s locally duplicated `writeError` widened to `writeError(w, status, code, msg)`; all three call sites (missing header, invalid key, resolver error) now emit `errcode.Unauthorized` / `errcode.InternalError`. Message strings byte-unchanged.
- `internal/ratelimit/ratelimit.go`'s 429 body sources `errcode.RateLimited` instead of the local `"rate_limited"` literal; the encoded body (error text + Retry-After) is byte-identical to before.
- Whole-repo `go build ./... && go vet ./... && go test ./... -count=1` green (all 20 tested packages pass).

## Task Commits

Each task was executed as a RED/GREEN TDD pair, committed atomically:

1. **Task 1: Create the internal/errcode package**
   - `e816f67` (test) - failing tests for the closed vocabulary (uniqueness, pattern, wire-value freeze, IsKnown, All()-copy)
   - `f794ce5` (feat) - errcode.go implementation; all five tests pass

2. **Task 2: Adopt errcode in internal/auth and internal/ratelimit**
   - `ddbc2bf` (test) - failing tests asserting auth error bodies carry a known code, and pinning ratelimit's 429 code literal
   - `8735c68` (feat) - widened auth's writeError signature and wired the three call sites; ratelimit's 429 now sources errcode.RateLimited

**Plan metadata:** this commit (docs: complete plan) — created after this SUMMARY.

_Note: both tasks had `tdd="true"`; each produced a test commit (RED, confirmed failing/non-compiling before implementation) followed by a feat commit (GREEN, confirmed passing)._

## Files Created/Modified
- `internal/errcode/errcode.go` - closed 14-code sync error-code vocabulary, `All()`, `IsKnown()`
- `internal/errcode/errcode_test.go` - uniqueness, machine-safe-pattern, wire-value-freeze, IsKnown, All()-copy tests
- `internal/auth/middleware.go` - `writeError` gained a `code` parameter; three call sites wired to `errcode.Unauthorized`/`errcode.InternalError`
- `internal/auth/middleware_test.go` - added `TestUnauthorizedBodyCarriesCode`, `TestAuthErrorCodesAreKnown`
- `internal/ratelimit/ratelimit.go` - 429 body's `code` field now sources `errcode.RateLimited`
- `internal/ratelimit/ratelimit_test.go` - added `TestRateLimitBodyCodeUnchanged` (asserts the literal `"rate_limited"`, not the constant)

## Decisions Made
- Followed the plan's exact fourteen-constant list, values, and doc-comment framing (D-01/D-02/D-03/D-06) with no additions.
- Kept `internal/auth`'s `writeError` duplicated rather than importing `internal/api`, per the plan and the existing package doc comment's own stated rationale (avoiding a dependency inversion).
- Did not touch `internal/api`, `internal/worker`, or `internal/metrics` — confirmed zero diff via `git diff --name-only -- internal/api internal/metrics internal/worker`, matching the plan's explicit scope boundary (56-03's job).

## Deviations from Plan

None - plan executed exactly as written. All acceptance criteria greps and test filters from the plan were re-run and passed:
- `grep -c '= "' internal/errcode/errcode.go` → 14
- `go doc ./internal/errcode` lists `All`, `IsKnown`, and all fourteen constants
- `grep -n 'QuotaExceeded *= *"quota_exceeded"'` / `'RateLimited *= *"rate_limited"'` both match
- `go list -deps ./internal/errcode | grep -c 'octoconv/internal'` → 1
- `grep -c 'errcode\.' internal/auth/middleware.go` → 3
- `grep -n 'errcode.RateLimited' internal/ratelimit/ratelimit.go` matches; `grep -c '"rate_limited"' internal/ratelimit/ratelimit.go` → 0
- `grep -c '"rate_limited"' internal/ratelimit/ratelimit_test.go` → 5 (≥1)
- `go test ./internal/auth/... -run 'TestUnauthorizedBodyCarriesCode|TestAuthErrorCodesAreKnown' -count=1 -v` → both pass
- `git diff --name-only -- internal/api internal/metrics internal/worker` → empty

## Issues Encountered

Worktree base drift: the worktree's HEAD was initially on an older commit (`175a738`, Phase 53 merge) that predated the phase-56 planning commit, so `.planning/phases/56-jobrunner-consolidation-structured-error-codes/` was absent from the worktree filesystem even though the `Read` tool (unsandboxed) could see it in the main checkout. Corrected via the sanctioned `worktree_branch_check` reset (`git reset --hard a9c897810d5b0f64ea67b7733526b1598ff798a5`) after confirming the worktree had zero uncommitted changes. No other issues.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `internal/errcode` is a compiled, tested, leaf-level vocabulary ready for plan 56-03 to sweep `internal/api`'s ~112 `writeError` call sites against.
- `internal/api`, `internal/worker`, `internal/metrics` remain completely untouched by this plan (verified zero diff), so 56-03 starts from a clean baseline.
- No blockers or concerns for downstream plans.

---
*Phase: 56-jobrunner-consolidation-structured-error-codes*
*Completed: 2026-08-08*

## Self-Check: PASSED

- FOUND: internal/errcode/errcode.go
- FOUND: internal/errcode/errcode_test.go
- FOUND: .planning/phases/56-jobrunner-consolidation-structured-error-codes/56-02-SUMMARY.md
- FOUND commit: e816f67 (test: failing errcode vocabulary tests)
- FOUND commit: f794ce5 (feat: errcode.go implementation)
- FOUND commit: ddbc2bf (test: failing auth/ratelimit adoption tests)
- FOUND commit: 8735c68 (feat: auth/ratelimit adoption)
