---
phase: 53-operator-usage-visibility
plan: 07
subsystem: api
tags: [go, chi, http, openapi, quota, operator, gap-closure]

# Dependency graph
requires:
  - phase: 53-05
    provides: "handleSystemUsage with the three-way key-set merge (idle-client visibility fix)"
  - phase: 53-06
    provides: "jobs.Repo.UTCToday (Postgres-derived day read) and the in-flight IS NOT NULL guards"
provides:
  - "api.UsageRepo.UTCToday, threading the Postgres clock into GET /v1/system/usage's day resolution"
  - "api.Config.QuotaEnforcementDisabled / Server.quotaEnforcementDisabled, the reporting-only mirror of jobs.Repo's kill-switch"
  - "quota_enforcement_disabled top-level response field and the computed_but_not_enforced remaining_semantics value"
  - "remaining_matches_admission_gate now requires BOTH period=today AND service-wide enforcement live"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Reporting-only config mirror: Config.QuotaEnforcementDisabled/Server.quotaEnforcementDisabled duplicate jobs.Repo's kill-switch value at the API layer purely for display -- never consulted by any admission decision -- fed from the exact same quotaDisabled variable at cmd/api/main.go's single call site so the two can never diverge"
    - "Three-branch truth-claim precedence, encoded as a switch in source order matching the plan's locked <design_resolution>: multi-day period wins over the kill-switch (no admission-gate analogue exists regardless), then the kill-switch, then the live case -- each branch pinned by its own dedicated test so a future reorder fails loudly"
    - "Frozen fake-clock testing: fakeUsageRepo.today/todayErr/todayCalls let tests assert an exact day far from the real one, eliminating the WR-07 UTC-midnight flake at its root rather than tolerating it"

key-files:
  created: []
  modified:
    - internal/api/api.go
    - internal/api/handlers_test.go
    - internal/api/system_usage_handlers.go
    - internal/api/system_usage_handlers_test.go
    - cmd/api/main.go
    - docs/openapi.yaml

key-decisions:
  - "quotaEnforcementDisabled is a plain bool field with no defaulting branch in NewServer -- false is the correct, fail-safe zero value (an unwired call site reports \"enforcement live\", matching pre-existing behaviour before this field existed), so no code path could accidentally leave it in an ambiguous state"
  - "The day resolution (s.usageRepo.UTCToday) is placed strictly after both the ?period 422 check and the ?client_id 400 check, and strictly before UsageSummary -- preserving D-03's fail-closed guarantee for the new repo read, pinned by three separate tests (invalid period, both non-operator 404 paths) all asserting todayCalls == 0"
  - "Per-row enforcement_mode is left completely untouched when the kill-switch is on -- the kill-switch is a separate, service-wide fact and gets its own top-level field (quota_enforcement_disabled), never overwriting the per-client column's own reported value"

patterns-established:
  - "Pattern: when a boolean claim in a response depends on more than one upstream condition, encode the full precedence as an explicit ordered switch (not nested if/else), and pin every branch AND the precedence order itself with one test per branch plus one test that exercises two conditions simultaneously (TestSystemUsage_MultiDayCaveatWinsOverKillSwitch) -- this is what makes a future accidental reorder fail loudly instead of silently"

requirements-completed: [OPSV-01, OPSV-02]

# Metrics
duration: ~35min
completed: 2026-08-05
---

# Phase 53 Plan 07: Close WR-02/WR-03 Enforcement-Truth Gaps Summary

**`remaining_matches_admission_gate` now requires BOTH `period=today` AND the service-wide `QUOTA_ENFORCEMENT_DISABLED` kill-switch being off (previously it only checked period), a new always-present `quota_enforcement_disabled` field surfaces the kill-switch state directly, and `day_from`/`day_to` now resolve from `jobs.Repo.UTCToday` (Postgres's own clock) instead of the API host's `time.Now()`.**

## Performance

- **Duration:** ~35 min
- **Tasks:** 3
- **Files modified:** 6 (0 created, 6 modified)

## Accomplishments

- `internal/api/api.go`: widened `UsageRepo` with `UTCToday(ctx) (time.Time, error)`; added `Config.QuotaEnforcementDisabled` (doc comment states it mirrors `jobsRepo.SetQuotaEnforcementDisabled` and is reporting-only, never an admission input) and `Server.quotaEnforcementDisabled`, wired in `NewServer` with no defaulting branch (`false` zero value is correct)
- `internal/api/handlers_test.go`: `fakeUsageRepo` gained `today`/`todayErr`/`todayCalls` and an `UTCToday` implementation — `todayCalls` deliberately a separate counter from `calls`, so every pre-existing `calls == 0` assertion keeps its original meaning
- `cmd/api/main.go`: `api.Config{QuotaEnforcementDisabled: quotaDisabled}` added to the `NewServer` call, fed from the SAME `quotaDisabled` variable already passed to `jobsRepo.SetQuotaEnforcementDisabled`, so the two can never diverge
- `internal/api/system_usage_handlers.go`: added `const periodToday = "today"` (collapses three bare-string literals into one, IN-07b); day resolution now calls `s.usageRepo.UTCToday(ctx)` — placed after the `?period`/`?client_id` validations and before `UsageSummary`, so an invalid request still costs zero DB work including the new read; on `UTCToday` failure, `writeError(w, 500, "failed to resolve current day")` (a fixed, distinct string — the underlying error text never reaches the client); envelope gained `QuotaEnforcementDisabled bool` (always emitted, no `omitempty`); `RemainingMatchesAdmissionGate` is now `period == periodToday && !s.quotaEnforcementDisabled`; the semantics/note selection became a three-branch `switch` in the plan's locked precedence order (multi-day wins over the kill-switch, then the kill-switch, then live)
- `docs/openapi.yaml`: added `quota_enforcement_disabled` property + description, added it to the top-level `required` list, widened the `remaining_semantics` enum with `computed_but_not_enforced`, rewrote `remaining_matches_admission_gate`'s description for the new two-condition truth, extended the per-row `enforcement_mode` description with the observe-mode caveat; per-row `required` list untouched
- `internal/api/system_usage_handlers_test.go`: added `newOperatorUsageTestServerQuotaDisabled` helper plus six new tests (day range from the repo clock not the process clock, 7-day window anchored on the frozen day, `UTCToday` failure → 500 with zero downstream repo calls and no leaked error text, kill-switch makes remaining not authoritative, live enforcement stays authoritative with the observe-mode caveat actually present in the note, multi-day caveat wins over the kill-switch); rewrote `TestSystemUsage_DefaultPeriodIsToday` to use a frozen fake day instead of `time.Now()` (the WR-07 #1 flake, and the one pre-existing test this plan edits); extended the invalid-period test and both non-operator 404 tests with `usage.todayCalls == 0`

## Task Commits

Each task was committed atomically:

1. **Task 1: Widen UsageRepo with UTCToday and thread the kill-switch into Config** — `21e98ba` (feat)
2. **Task 2: Resolve the day from the server clock and make the truth claim enforcement-aware** — `cfaff69` (fix)
3. **Task 3: Pin the enforcement-aware claim and the frozen server clock** — `d835ac2` (test)

## Files Modified

- `internal/api/api.go` — `UsageRepo.UTCToday`, `Config.QuotaEnforcementDisabled`, `Server.quotaEnforcementDisabled`
- `internal/api/handlers_test.go` — `fakeUsageRepo.today/todayErr/todayCalls` + `UTCToday` implementation
- `cmd/api/main.go` — `QuotaEnforcementDisabled: quotaDisabled` wired into the `api.Config` literal
- `internal/api/system_usage_handlers.go` — `periodToday` constant, server-clock day resolution, `QuotaEnforcementDisabled` field, three-branch semantics precedence, updated package/header doc comment
- `internal/api/system_usage_handlers_test.go` — quota-disabled test-server helper, six new tests, one rewritten pre-existing test, three extended existing tests
- `docs/openapi.yaml` — `quota_enforcement_disabled` property/required entry, widened `remaining_semantics` enum, rewritten descriptions

## Decisions Made

- `Config.QuotaEnforcementDisabled` defaults to `false` with no explicit defaulting branch in `NewServer` — this is deliberately the fail-safe direction: any call site that forgets to wire it reports "enforcement is live", which was the endpoint's exact pre-existing behavior before this field existed, never a silent downgrade to "enforcement is off".
- The three-branch `remaining_semantics` selection is written as an explicit `switch` (not nested `if`), with the multi-day check first — this makes the locked precedence order (`<design_resolution>` in the plan) visible directly in source order, not just documented in a comment.
- Per-row `enforcement_mode` is never rewritten based on the kill-switch — the kill-switch is a single, service-wide fact belonging in its own top-level field; conflating it into the per-row column would make a client's own configured mode unreadable while the kill-switch happens to be on.

## Deviations from Plan

**Minor plan-wording discrepancy (informational only, no code impact):**
Task 3's acceptance criterion states `go test ./internal/api/ -run TestSystemUsage -count=1 -v` should report 36 `--- PASS`. The actual observed count is 39 `--- PASS` (all 6 new top-level tests present, 0 `--- FAIL`, 0 `--- SKIP`, stable across `-count=5`). This is not a defect — every named test and every behavior the plan's `<behavior>` section and acceptance criteria specify is present and passing; it mirrors the same class of pre-existing-count miscount plan 53-06's SUMMARY documented for its own acceptance criteria (a planner-side estimate that undercounted subtests/table-driven cases), not a Rule 1-4 deviation requiring a fix.

**TDD gate sequence note (informational):** This plan's frontmatter marks Task 3 `tdd="true"`, but the plan's own design places the implementation in Tasks 1-2 (`feat`/`fix`, `21e98ba`/`cfaff69`) BEFORE the pinning tests in Task 3 (`test`, `d835ac2`) — the opposite order from a literal RED-then-GREEN cycle. This was the plan's explicit, deliberate design (mirroring 53-05/53-06's own gap-closure pattern in this same phase: fix first, then pin with tests plus a mutation-check proof), not an executor deviation. The mutation-check the plan's own acceptance criteria required was performed in place of a literal RED phase: `RemainingMatchesAdmissionGate` was temporarily reverted to `period == periodToday` (dropping the kill-switch term), confirmed `TestSystemUsage_KillSwitchMakesRemainingNotAuthoritative` fails with the exact predicted message, then restored via `cp` backup/restore (never `git stash`/`checkout`), confirming `git diff --stat` was empty afterward. See `## TDD Gate Compliance` below.

## TDD Gate Compliance

No `test(...)` commit precedes the `feat(...)`/`fix(...)` commits for this plan's behavior — see the "TDD gate sequence note" above for why this is the plan's own intentional design rather than a skipped RED phase. The mutation-check acceptance criterion (proving the new test is not vacuous) was satisfied via a temporary, uncommitted revert-and-observe-and-restore cycle instead of a literal pre-implementation failing test. All three task commits, all acceptance criteria, and the full plan's `<verification>` block passed.

## Issues Encountered

- One arithmetic bug in a NEW test authored during this plan (not a plan defect): `TestSystemUsage_SevenDayRangeAnchoredOnRepoClock` initially asserted `day_from == "2020-02-25"` for a 7-day window ending `2020-03-01`; 2020 is a leap year, so `resolveUsagePeriod`'s `AddDate(0, 0, -6)` correctly lands on `2020-02-24`, not `2020-02-25`. Caught immediately by the test itself failing on first run; fixed before committing Task 3 — no implementation code was ever wrong, only the test's initial expected value.
- `internal/storage`'s previously-flaky `TestRoundTrip` (MinIO host disk-space threshold, logged in prior plans' SUMMARYs as a pre-existing environment condition) did NOT reproduce during this plan's full-suite run — the shared container evidently has headroom again. No action needed; `internal/storage` was never touched by this plan (`git diff --stat internal/storage/` empty across all three commits).
- This worktree had no `.env` (git-ignored, not checked out per-worktree); copied from `/Users/apaderin/dev/octoconv/.env` to run the DB-gated full-suite verification against the already-running shared `octoconv-db`/`octoconv-redis`/`octoconv-minio` containers, mirroring plan 53-06's documented approach. The copy remains git-ignored and was never staged or committed.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- This was the final gap-closure plan (wave 5) of Phase 53. All three code-review findings targeted at this plan (WR-02, WR-03, WR-07 #1) are closed: `remaining_matches_admission_gate` no longer overstates enforcement when the kill-switch is on or when a row is in observe mode; `day_from`/`day_to` derive from Postgres, never the API process clock; both midnight-flake sources (this plan's API-layer test and plan 53-06's `internal/jobs` test) are eliminated at the root via frozen fake/repo clocks.
- `go build ./... && go vet ./... && gofmt -l .` all clean.
- Full repo suite (`set -a && . .env && set +a && go test ./... -count=1`) reports `ok` for every package with no test files skipped, including `internal/storage`.
- `git diff --exit-code go.mod go.sum` clean (no dependency change) and `git diff --exit-code internal/convert/exec.go` clean (untouched) — confirmed per the plan's `<verification>` block.
- No blockers for phase completion.

## Verification Evidence

### The three exact `remaining_semantics`/`remaining_note` pairs

1. **`live_admission_gate`** (period=today, kill-switch off):
   > "period=today: remaining is exactly what the admission gate (evaluate()) computes right now. A row whose own enforcement_mode is \"observe\" has its verdict computed and counted here too, but that row is never actually enforced — a negative remaining on such a row does not mean requests are being rejected for it."

2. **`computed_but_not_enforced`** (period=today, kill-switch on):
   > "period=today: the arithmetic is exactly what the admission gate (evaluate()) computes, but QUOTA_ENFORCEMENT_DISABLED is set service-wide right now — no request is being rejected. Verdicts are still computed and counted, never enforced, until the kill-switch is turned off."

3. **`hypothetical_single_day_equivalent`** (period=7d/30d, either kill-switch state):
   > "max_daily_bytes/max_daily_jobs are DAILY caps that reset at UTC midnight. For a multi-day period, remaining compares a multi-day sum against a one-day cap — it has no admission-gate analogue and will legitimately go deeply negative for healthy clients."

### Full envelope JSON for the kill-switch-on case

Captured via a throwaway, uncommitted debug test (`newOperatorUsageTestServerQuotaDisabled`, empty usage/limits fakes, `?period` omitted):

```json
{
  "period": "today",
  "day_from": "2026-08-05",
  "day_to": "2026-08-05",
  "quota_enforcement_disabled": true,
  "remaining_matches_admission_gate": false,
  "remaining_semantics": "computed_but_not_enforced",
  "remaining_note": "period=today: the arithmetic is exactly what the admission gate (evaluate()) computes, but QUOTA_ENFORCEMENT_DISABLED is set service-wide right now — no request is being rejected. Verdicts are still computed and counted, never enforced, until the kill-switch is turned off.",
  "usage": []
}
```

### Observed mutation-check failure (temporary, uncommitted revert)

With `RemainingMatchesAdmissionGate` temporarily reverted from `period == periodToday && !s.quotaEnforcementDisabled` to `period == periodToday` (dropping the kill-switch term):

```
=== RUN   TestSystemUsage_KillSwitchMakesRemainingNotAuthoritative
    system_usage_handlers_test.go:440: WR-02: remaining_matches_admission_gate = true, want false when the kill-switch is on
--- FAIL: TestSystemUsage_KillSwitchMakesRemainingNotAuthoritative (0.00s)
FAIL
```

Restored via `cp` from a pre-edit backup (never `git checkout`/`reset`/`stash`); `git diff --stat internal/api/system_usage_handlers.go` was empty afterward (byte-identical to the committed state).

### Clean-diff invariants

- `git diff --exit-code go.mod go.sum` — clean (exit 0), no dependency change
- `git diff --exit-code internal/convert/exec.go` — clean (exit 0), untouched
- `grep -rc "usage not found" internal/api/` — 0 everywhere, no new 404 literal introduced
- `grep -c "time.Now()" internal/api/system_usage_handlers.go` — 0

### Test suite results

- `go build ./... && go vet ./... && gofmt -l .` — all clean, no output
- `go test ./internal/api/ -run TestSystemUsage -count=1 -v` — 39 `--- PASS` (see Deviations for the plan-text count discrepancy), 0 `--- FAIL`, 0 `--- SKIP`
- `go test ./internal/api/ -run TestSystemUsage -count=5` — stable, no flake
- Full repo suite against the live shared `octoconv-db`/`octoconv-redis`/`octoconv-minio` containers: `go test ./... -count=1` — every package reports `ok`, including `internal/storage` (the MinIO disk-space issue prior plans logged did not reproduce this run)

---
*Phase: 53-operator-usage-visibility*
*Completed: 2026-08-05*

## Self-Check: PASSED

- FOUND: internal/api/api.go
- FOUND: internal/api/system_usage_handlers.go
- FOUND: internal/api/system_usage_handlers_test.go
- FOUND: cmd/api/main.go
- FOUND: docs/openapi.yaml
- FOUND: .planning/phases/53-operator-usage-visibility/53-07-SUMMARY.md
- FOUND commit: 21e98ba
- FOUND commit: cfaff69
- FOUND commit: d835ac2
