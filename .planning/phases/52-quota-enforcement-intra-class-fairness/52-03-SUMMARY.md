---
phase: 52-quota-enforcement-intra-class-fairness
plan: 03
subsystem: database
tags: [postgres, pgx, advisory-lock, quota, concurrency, fairness]

# Dependency graph
requires:
  - phase: 52-quota-enforcement-intra-class-fairness (plan 01)
    provides: client_limits table (client_id, engine) with NULL-means-unlimited limit columns, clients.enforcement_mode, jobs_client_engine_inflight_idx
  - phase: 52-quota-enforcement-intra-class-fairness (plan 02)
    provides: RecordQuotaWouldReject(clientID, engine, reason) and the rate_limited/quota_exceeded {error, code} 429 body contract
  - phase: 51-attribution-accounting-retention
    provides: client_usage_daily rollup table, written exactly-once inside the terminal transition
provides:
  - internal/jobs/admission.go — the QUOT/FAIR admission gate mechanism (ErrConcurrencyLimit/ErrDailyBytesLimit/ErrDailyJobsLimit sentinels, QuotaReason, PreAdmit, SetQuotaObserver, SetQuotaEnforcementDisabled, admitLocked)
  - admitLocked joined as the first statement inside Repo.Create's existing pgx.BeginFunc closure, under a per-(client, engine) transaction-scoped advisory lock
  - a serialized, race-proof concurrency cap (queued+active occupancy) proven under a 12-goroutine burst with -race
  - the D-15 daily-overshoot bound (K x MAX_INPUT_BYTES) and the PreAdmit concurrent-burst residual bound ((N - K) x MAX_INPUT_BYTES), both stated in code with derivation and pinned by tests
affects: [52-04, 52-05, 53-operator-usage-visibility]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Transaction-scoped, per-entity advisory lock: pg_advisory_xact_lock(int, int) with FNV-1a-derived keys, structurally distinct from internal/reconciler's single-bigint session-scoped leader lock — no shared key space, no reuse"
    - "Occupancy derived live from jobs.status (queued+active), never a counter column — zero slot-release code anywhere, terminal transitions free a slot as a side effect of the status change that already happens"
    - "Three distinct sentinel errors (not one generic ErrQuotaExceeded) so callers can compute a per-limit-type Retry-After via errors.Is"
    - "Two-tier admission: a cheap non-locking PreAdmit pre-upload check (optimization, not correctness) plus the authoritative admitLocked inside the same transaction as the insert (correctness mechanism) — both documented with their accepted, bounded, measured residuals rather than assumed away"

key-files:
  created:
    - internal/jobs/admission.go
    - internal/jobs/admission_test.go
  modified:
    - internal/jobs/repo.go

key-decisions:
  - "admitLocked is a free function (not a *Repo method) taking report/disabled as explicit parameters, so its exact signature satisfies the grep-verifiable acceptance criterion (\"func admitLocked(\" with no receiver) while still being callable from inside Create's closure with r's quotaObserver/quotaDisabled already in scope"
  - "quotaObserver/quotaDisabled fields were added to the Repo struct in Task 1 (not deferred to Task 2) because admission.go's SetQuotaObserver/SetQuotaEnforcementDisabled methods needed them to exist for Task 1's own `go build ./...` verification to pass standalone"
  - "The daily-overshoot test (TestOvershootBoundedByConcurrencyCap) sets max_daily_bytes exactly equal to one job's input size, not an arbitrary small number — a smaller cap would make the DAILY check the blocker on the very first create (since bytesUsed stays 0 for the whole test), defeating the point of proving concurrency is what actually bounds the overshoot"

requirements-completed: [QUOT-01, QUOT-02, QUOT-04, FAIR-01]

# Metrics
duration: ~55min
completed: 2026-08-03
---

# Phase 52 Plan 03: Admission Gate — Concurrency, Daily Quota & Intra-Class Fairness Summary

**A transaction-scoped Postgres admission gate (`internal/jobs/admission.go`) that serializes a per-(client, engine) occupancy count with `INSERT INTO jobs` under an advisory lock, proven to admit exactly K of a 12-goroutine concurrent burst with `-race`, plus a non-locking `PreAdmit` pre-upload optimization — both documented residuals (`K x MAX_INPUT_BYTES` daily overshoot, `(N - K) x MAX_INPUT_BYTES` burst waste) stated in code with their derivation and pinned by tests.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-08-03T02:08:32Z (approx, worktree base reset after wave 1 merge)
- **Completed:** 2026-08-03T02:41:15Z
- **Tasks:** 3/3
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments

- `internal/jobs/admission.go`: three quota sentinels (`ErrConcurrencyLimit`/`ErrDailyBytesLimit`/`ErrDailyJobsLimit`), a shared reason taxonomy (`QuotaReason`), FNV-1a-derived per-(client, engine) advisory-lock keys proven structurally incapable of colliding with the reconciler's leader lock (different key-space arity, different scope), `loadQuotaState` (LEFT JOIN makes "no row" and "all-NULL row" indistinguishable — both mean unlimited, D-02), `evaluate` (concurrency-then-daily, first violation wins), `admitLocked` (the authoritative, transaction-scoped path) and `PreAdmit` (the non-locking pre-upload optimization) — nil-safe `SetQuotaObserver`/`SetQuotaEnforcementDisabled` wiring seams mirroring `api.Server.SetCancelPublisher`'s startup-only contract.
- `internal/jobs/repo.go`: `admitLocked` joined as the FIRST statement inside `Repo.Create`'s existing `pgx.BeginFunc` closure, before `INSERT INTO jobs` — no second transaction, `Create`'s signature and `internal/worker` both unchanged (verified via `git diff --stat`).
- `internal/jobs/admission_test.go`: a `DATABASE_URL`-gated suite proving the cap holds under a real 12-goroutine concurrent burst (`go test -race`, exactly 3 admitted / 9 rejected / 3 rows), the ROADMAP SC1 unlimited-by-default fast path (no row and all-NULL row), table-driven zero-release-code slot-freeing across `MarkDone`/`MarkFailed`/`Cancel`, daily bytes/jobs exact-boundary semantics ("would exceed" not "already exceeded"), day/engine scoping, observe mode and the global kill-switch both admitting while firing the observer exactly once, `PreAdmit` parity with `admitLocked` including the no-double-count-in-observe-mode guard, and the D-15 overshoot bound pinned against real `job_inputs` rows — plus a DB-free `TestQuotaLockKeysDeterministicAndDistinct` with hard-coded FNV-1a values that would catch a silent hash-function swap.
- The D-15 daily-overshoot bound (`K x MAX_INPUT_BYTES`) and `PreAdmit`'s concurrent-burst residual bound (`(N - K) x MAX_INPUT_BYTES`) are both stated in code with their full derivation, not just in planning docs — grep-verifiable per the plan's own acceptance criteria.

## Task Commits

Each task was committed atomically:

1. **Task 1: internal/jobs/admission.go — sentinels, lock keys, admitLocked, PreAdmit, wiring seams** - `2660359` (feat)
2. **Task 2: Join admitLocked to Repo.Create's existing transaction** - `0f23854` (feat)
3. **Task 3: admission_test.go — cap, burst, slot-release, observe mode, overshoot bound** - `0d56bc5` (test)

_Note: worktree mode — the plan-metadata commit (SUMMARY.md) is committed separately per the worktree execution protocol; STATE.md/ROADMAP.md are updated centrally by the orchestrator after merge, not by this agent._

## Files Created/Modified

- `internal/jobs/admission.go` - the admission gate: sentinels, reason taxonomy, advisory-lock key derivation, `loadQuotaState`/`evaluate`, `admitLocked`, `PreAdmit`, observer/kill-switch setters
- `internal/jobs/repo.go` - `Repo` struct gains `quotaObserver`/`quotaDisabled` fields; `Create`'s `pgx.BeginFunc` closure calls `admitLocked` as its first statement; `Create`'s doc comment names the three quota sentinels
- `internal/jobs/admission_test.go` - 13 tests (2 table-driven, 1 DB-free) covering the unlimited fast path, the concurrency cap (sequential and 12-goroutine concurrent), zero-release-code slot-freeing, daily bytes/jobs boundaries and scoping, observe mode, the kill-switch, `PreAdmit` parity, the D-15 overshoot bound, and lock-key determinism/distinctness

## Decisions Made

See `key-decisions` in the frontmatter above. In short: `admitLocked` had to be a free function (not a method) to satisfy the plan's own grep-verifiable acceptance criterion for its exact declaration shape, so `Repo`'s `quotaObserver`/`quotaDisabled` are threaded through as explicit parameters from `Create`'s closure (where `r` is already in scope) rather than via a receiver.

## Deviations from Plan

None — plan executed exactly as written. All acceptance criteria in PLAN.md Tasks 1-3 were verified directly:
- `gofmt -l internal/jobs`, `go vet ./internal/jobs/`, `go build ./...` — all clean
- Every grep-based acceptance criterion (function signature count, `pg_advisory_xact_lock` count=1, no `pg_try_advisory_lock`/`PGAdvisoryLock` reuse, no `internal/metrics` import, the UTC-date expression appearing exactly once outside comments, the D-15 and PreAdmit-burst bounds present in code, `Create`'s signature unchanged, `admitLocked(ctx, tx` appearing once and before `INSERT INTO jobs`, `pgx.BeginFunc` count unchanged, `internal/worker` untouched) — verified with the exact commands from the plan
- `DATABASE_URL=... go test ./internal/jobs/ -count=1 -v` — all 13 admission tests + the full pre-existing suite PASS, zero skips
- `DATABASE_URL=... go test -race ./internal/jobs/ -run TestConcurrentBurstAdmitsExactlyK -count=1` — PASS, exactly 3/9 split, asserted as exact integers
- `go test ./internal/api/ ./internal/reconciler/` — unaffected, still PASS

## Issues Encountered

One incidental fixture collision, fixed inline before any commit: this plan's test file initially redeclared `intPtr`, which already exists in `internal/jobs/repo_test.go`. Removed the duplicate and reused the existing helper; only `int64Ptr` (genuinely new — no prior `*int64` test helper in this package) was added.

## User Setup Required

None — no external service configuration required. Verification used the pre-existing local `octoconv-db` compose container already running on this machine (port 5434).

## Next Phase Readiness

- `internal/jobs.Repo.Create` now returns `jobs.ErrConcurrencyLimit`/`ErrDailyBytesLimit`/`ErrDailyJobsLimit` for a quota-rejected job, and `Repo.PreAdmit` exposes the same verdicts as a non-locking pre-upload check — both exported exactly per the PLAN.md `<interfaces>` contract (`ErrConcurrencyLimit`, `ErrDailyBytesLimit`, `ErrDailyJobsLimit`, `QuotaReason`, `PreAdmit`, `SetQuotaObserver`, `SetQuotaEnforcementDisabled`), so Plan 52-04 can wire the REST handler's `errors.Is` dispatch, the `quota_exceeded` 429 body, and `cmd/api/main.go`'s `metrics.RecordQuotaWouldReject`/kill-switch env wiring without renaming anything here.
- No worker-side change, no chart file touched — confirmed by `git diff --stat` against `internal/worker/`, `internal/queue/`, `deploy/` returning empty for this plan's commits, matching PLAN.md's stated non-goals (D-08).
- The D-15 and PreAdmit-burst residuals are now both measured quantities (test-pinned), not assumptions — Plan 52-05's `TestQuotaAdmission/ConcurrentBurstBoundedUploadWaste` (named explicitly in this plan's `PreAdmit` doc comment) has a concrete mechanism to measure against once the handler wires `PreAdmit` ahead of the S3 write.

---
*Phase: 52-quota-enforcement-intra-class-fairness*
*Completed: 2026-08-03*

## Self-Check: PASSED

- FOUND: internal/jobs/admission.go
- FOUND: internal/jobs/admission_test.go
- FOUND: .planning/phases/52-quota-enforcement-intra-class-fairness/52-03-SUMMARY.md
- FOUND: 2660359 (Task 1 commit)
- FOUND: 0f23854 (Task 2 commit)
- FOUND: 0d56bc5 (Task 3 commit)
