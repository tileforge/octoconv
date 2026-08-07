---
phase: 55-live-gate-multi-replica-quota-correctness
plan: 03
subsystem: api
tags: [go, postgres, pgx, errors, quota, retry-after]

# Dependency graph
requires:
  - phase: 52-quota-enforcement-intra-class-fairness
    provides: "internal/jobs/admission.go's evaluate()/admitLocked/PreAdmit quota gate, the three quota sentinels, and quotaRejection's Retry-After derivation (D-11)"
provides:
  - "internal/jobs.DailyQuotaError{Err, EvaluatedAt}: a typed error wrapping ErrDailyBytesLimit/ErrDailyJobsLimit with the Postgres now() evaluate's daily branch read, via Unwrap so errors.Is is unaffected"
  - "internal/api quotaRejection deriving a daily rejection's Retry-After from DailyQuotaError.EvaluatedAt via errors.As, with a time.Now() fallback for any unwrapped sentinel"
  - "A straddle regression test (EvaluatedAt one second before UTC midnight) proving Retry-After is small even though the test process's own wall clock is far from midnight"
  - "Closed .planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md, moved to .planning/todos/completed/"
affects: ["55-05 (touches internal/api/handlers.go comment-only in a later wave)", "any future consumer of jobs.ErrDailyBytesLimit/ErrDailyJobsLimit"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Typed wrapper error carrying a DB-evaluated instant, exposed via Unwrap so errors.Is against the underlying sentinel is unaffected by the wrap (mirrors http.MaxBytesError's errors.As usage already in handlers.go)"
    - "Extending an existing single QueryRow with one extra scanned column to carry a clock value out of a transaction, rather than adding a second round trip"

key-files:
  created: []
  modified:
    - internal/jobs/admission.go
    - internal/jobs/admission_test.go
    - internal/api/handlers.go
    - internal/api/handlers_test.go
    - .planning/todos/completed/2026-08-04-fix-retry-after-midnight-race.md (moved from pending/)

key-decisions:
  - "DailyQuotaError.Err is the underlying sentinel (not embedded) with Unwrap returning it, so errors.Is/QuotaReason need zero changes and only the two call sites that need EvaluatedAt (quotaRejection) use errors.As"
  - "The extra now() column is selected as bare now() (transaction-start, timestamptz), not (now() AT TIME ZONE 'utc') -- keeps it an unambiguous instant; secondsUntilUTCMidnight already normalizes via .UTC()"
  - "The concurrency sentinel stays bare/unwrapped -- its Retry-After is the fixed quotaConcurrencyRetryAfterSeconds constant with no clock dependency, so wrapping it would add a field nothing reads"
  - "quotaRejection computes dailyNow := time.Now() exactly once, outside the switch, overridden only if a non-zero EvaluatedAt is present -- keeps the acceptance criterion 'no new time.Now() call inside the daily branches' literally true while still providing a safe fallback for any bare-sentinel path (production: none; the fake repos in handlers_test.go construct bare sentinels directly)"

requirements-completed: [TODO-2026-08-04-RETRY-AFTER-MIDNIGHT]

# Metrics
duration: ~20min
completed: 2026-08-06
---

# Phase 55 Plan 03: Retry-After Midnight Race Fix Summary

**Daily quota rejections now derive `Retry-After` from the Postgres transaction's own evaluation instant (carried through a new `*jobs.DailyQuotaError` wrapper via `errors.As`), instead of the API process's wall clock at response-write time.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-06T17:03:00Z (approx, first file read)
- **Completed:** 2026-08-06T17:22:54Z
- **Tasks:** 2
- **Files modified:** 5 (4 Go files + 1 todo file moved)

## Accomplishments
- Added `internal/jobs.DailyQuotaError{Err, EvaluatedAt}`, returned by `evaluate`'s daily branch in place of the two bare sentinels, carrying the Postgres `now()` the daily-window filter used — obtained from a third scanned column on the *existing* `QueryRow` (zero new round trips).
- `quotaRejection` (`internal/api/handlers.go`) now unwraps via `errors.As(err, &dqe)` and passes `dqe.EvaluatedAt` to `secondsUntilUTCMidnight` for the two daily branches, falling back to `time.Now()` only when no wrapper is present (today: only the fake repos in `handlers_test.go` construct a bare sentinel).
- Added an end-to-end straddle regression test (`TestCreateJob_QuotaDailyBytes_RetryAfterStraddlesMidnight`) that a `*DailyQuotaError` with `EvaluatedAt` one second before UTC midnight produces `Retry-After < 60`, even though the test process's own clock is nowhere near midnight — the exact case the original todo said `TestSecondsUntilUTCMidnight`'s pure-function table cannot reach.
- Added DB-backed and pure-unit tests proving the wrap preserves `errors.Is` for both daily sentinels, is absent for the concurrency sentinel, and that `EvaluatedAt` matches a same-instant `SELECT now()`/`SELECT (now() AT TIME ZONE 'utc')::date` read from the test's own pool.
- Closed and moved `.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md` to `.planning/todos/completed/`, with the resolution and its regression tests recorded in the file's frontmatter.

## Task Commits

Each task was committed atomically:

1. **Task 1: Carry the Postgres evaluation instant out of evaluate() in a typed error** - `fde52bc` (feat)
2. **Task 2: Derive Retry-After from the carried instant, with a straddle regression test** - `f90b8bf` (fix)
3. **Todo closure: move folded todo to completed/** - `e9c2597` (docs)

_No TDD RED/GREEN split was used — this plan's `tdd="true"` tasks specified test additions inline within the same action as the implementation change (a small typed-error addition plus a `switch`-branch input change), and the plan's own `<action>` text interleaves test and implementation instructions rather than gating them into separate commits. Each task's test(s) and implementation landed together in one commit, matching this plan's own task boundaries._

## Files Created/Modified
- `internal/jobs/admission.go` - Added `DailyQuotaError` type (`Err error`, `EvaluatedAt time.Time`, `Error()`, `Unwrap()`); extended `evaluate`'s daily-branch `QueryRow` with a third `now()` column and a new `evaluatedAt time.Time` scan target; both daily-verdict returns now construct `&DailyQuotaError{...}`; extended the sentinel block's and concurrency branch's doc comments.
- `internal/jobs/admission_test.go` - `TestDailyQuotaErrorCarriesPostgresEvaluationClock` (errors.As yields non-zero, near-`SELECT now()`, same-UTC-date `EvaluatedAt`; errors.Is against `ErrDailyJobsLimit` still holds), `TestConcurrencyRejectionIsNotADailyQuotaError` (errors.As is false for a concurrency verdict).
- `internal/api/handlers.go` - `quotaRejection` now does `var dqe *jobs.DailyQuotaError; errors.As(err, &dqe)`, uses `dqe.EvaluatedAt` when present and non-zero as the `now` argument to `secondsUntilUTCMidnight` for both daily branches, else falls back to a single `time.Now()` read; doc comment extended to name the fix and the fallback's justification.
- `internal/api/handlers_test.go` - `TestQuotaRejection_RetryAfterUsesDBEvaluationInstantNotProcessClock` (pure unit test on `quotaRejection`: exact `1`/`43200` for wrapped daily sentinels, bare-sentinel fallback still `ok==true`/`>0`, concurrency constant unaffected, non-quota `ok==false`), `TestCreateJob_QuotaDailyBytes_RetryAfterStraddlesMidnight` (end-to-end straddle case through the full HTTP handler, `fakeRepo.preAdmitErr` set to a `*jobs.DailyQuotaError` with `EvaluatedAt` one second before midnight).
- `.planning/todos/completed/2026-08-04-fix-retry-after-midnight-race.md` - moved from `.planning/todos/pending/`, frontmatter extended with `completed`, `completed_by`, and a `resolution` field naming the exact type/tests that closed it.

## New error type — exact shape (for 55-05, which touches this same file comment-only in a later wave)

```go
// internal/jobs/admission.go
type DailyQuotaError struct {
	// Err is always ErrDailyBytesLimit or ErrDailyJobsLimit.
	Err error
	// EvaluatedAt is the Postgres now() the daily-window filter used.
	EvaluatedAt time.Time
}

func (e *DailyQuotaError) Error() string { return e.Err.Error() }
func (e *DailyQuotaError) Unwrap() error { return e.Err }
```

- Constructed only inside `evaluate`'s daily branch (`internal/jobs/admission.go`), in place of the two bare sentinel returns: `&DailyQuotaError{Err: ErrDailyBytesLimit, EvaluatedAt: evaluatedAt}` / `&DailyQuotaError{Err: ErrDailyJobsLimit, EvaluatedAt: evaluatedAt}`.
- `EvaluatedAt` is scanned from a third column (`now()`, bare — a `timestamptz`, not zone-adjusted) added to the daily branch's single existing `QueryRow` in `evaluate`. No new query, no new round trip.
- The concurrency sentinel (`ErrConcurrencyLimit`) is **never** wrapped — it is still returned bare, by deliberate design (its `Retry-After` is a fixed constant with no clock dependency).
- `errors.Is(err, ErrDailyBytesLimit)` / `errors.Is(err, ErrDailyJobsLimit)` and `QuotaReason(err)` are **unaffected** by the wrap — `Unwrap` makes `errors.Is` transparent, so every pre-existing call site (including `admitLocked`'s `QuotaReason(verdict)` call and every existing test) needed zero changes.
- **`Retry-After` now derives from:** `internal/api/handlers.go`'s `quotaRejection(err error)`. It does `var dqe *jobs.DailyQuotaError; errors.As(err, &dqe)`, then for the two daily `switch` branches passes `secondsUntilUTCMidnight(dailyNow)` where `dailyNow` is `dqe.EvaluatedAt` if `dqe != nil && !dqe.EvaluatedAt.IsZero()`, else the single `time.Now()` read computed once before the `switch` (the documented fallback for a bare, unwrapped sentinel — no production path exercises this fallback today; only the fake repos in `handlers_test.go` construct one directly).

## Decisions Made
- `DailyQuotaError.Err` is a plain field (not an embedded interface) with an explicit `Unwrap()` returning it — keeps the wrapper's shape obvious at the call site and matches this codebase's existing `errors.As`-consuming pattern for `*http.MaxBytesError` in the same `handlers.go` file.
- The third `now()` column reuses the existing `QueryRow` rather than a second query, per the plan's explicit "no new round trip" constraint and to avoid a second read that could observe a different transaction snapshot than the two existing subquery pairs.
- `quotaRejection`'s single `time.Now()` call was kept outside the `switch`, computed unconditionally, then simply not used when `dqe` is present — this satisfies the acceptance criterion "no new `time.Now()` call inside `quotaRejection`'s daily branches" literally (there is exactly one `time.Now()` call in the whole function, and it is not textually inside either daily `case`).

## Deviations from Plan

None — plan executed exactly as written. Both tasks' `<action>` and `<behavior>` specifications were implemented as specified; all acceptance criteria (see Self-Check below) were verified to hold, including the deliberately scoped `grep -c '\.QueryRow(ctx,'` criterion, which was satisfied without any wording compromise to the new explanatory comments.

## Issues Encountered

One tooling snag, not a plan issue: an early draft of the `now()`-scan comment inside `evaluate`'s SQL raw-string literal used backticks around the phrase `timestamp without time zone`, which prematurely terminated the Go raw string (raw strings are themselves backtick-delimited) and produced a syntax error. Fixed by rephrasing the comment to avoid backticks (`go build` confirmed clean immediately after).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `internal/jobs/admission.go` and `internal/api/handlers.go` are both clean (`go build ./...`, `go vet ./...`, `gofmt -l` empty) and `go test ./... -timeout 5m` passes fully (all packages, DB-backed included, against the local docker-compose Postgres).
- 55-05 (a later wave) touches `internal/api/handlers.go` comment-only — the exact call-site shape (`errors.As(err, &dqe)`, `dqe.EvaluatedAt`, the single pre-switch `time.Now()` fallback) is recorded above for that plan to reference without re-reading this diff.
- No signature in `internal/api/api.go` changed (`PreAdmit`'s contract is untouched); no new query round trip was added — both explicit success criteria from the plan hold.
- The daily-quota sentinel-error contract change was scoped exactly to `internal/jobs/admission.go` and `internal/api/handlers.go` — the concurrency-cap branch of `evaluate`/`admitLocked` (the mechanism phase 55's live gate proves) was not touched, per this plan's critical constraint.

---
*Phase: 55-live-gate-multi-replica-quota-correctness*
*Completed: 2026-08-06*

## Self-Check: PASSED

- FOUND: internal/jobs/admission.go
- FOUND: internal/jobs/admission_test.go
- FOUND: internal/api/handlers.go
- FOUND: internal/api/handlers_test.go
- FOUND: .planning/todos/completed/2026-08-04-fix-retry-after-midnight-race.md
- FOUND: .planning/phases/55-live-gate-multi-replica-quota-correctness/55-03-SUMMARY.md
- FOUND commit: fde52bc (Task 1)
- FOUND commit: f90b8bf (Task 2)
- FOUND commit: e9c2597 (todo closure)
