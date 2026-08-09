---
created: 2026-08-04T00:57:36.622Z
title: Fix Retry-After midnight race in daily quota rejection
area: api
files:
  - internal/api/handlers.go:1017-1026 (quotaRejection)
  - internal/api/handlers.go:986 (secondsUntilUTCMidnight)
  - internal/jobs/jobs.go (ErrDailyBytesLimit, ErrDailyJobsLimit sentinels)
completed: 2026-08-06
completed_by: phase 55 plan 03 (folded todo, CONTEXT D-07)
resolution: >-
  Added internal/jobs.DailyQuotaError{Err, EvaluatedAt}, wrapping the two
  daily sentinels with the Postgres now() evaluate's daily branch read (one
  extra scanned column on the existing QueryRow, no new round trip).
  quotaRejection (internal/api/handlers.go) now derives
  secondsUntilUTCMidnight's input from that EvaluatedAt via errors.As,
  falling back to time.Now() only for an unwrapped sentinel. Regression
  test: TestCreateJob_QuotaDailyBytes_RetryAfterStraddlesMidnight
  (internal/api/handlers_test.go), plus
  TestQuotaRejection_RetryAfterUsesDBEvaluationInstantNotProcessClock and
  TestDailyQuotaErrorCarriesPostgresEvaluationClock
  (internal/jobs/admission_test.go).
---

## Problem

`quotaRejection` computes `secondsUntilUTCMidnight(time.Now())` using the
API's own wall clock, independent of the moment the admission transaction in
Postgres actually evaluated `(now() AT TIME ZONE 'utc')::date` to reject the
request. In the narrow window where that DB evaluation happens just before
UTC midnight but the API formats the HTTP response just after, the function
targets the *next* midnight instead of the one that already passed — it can
advertise `Retry-After: ~86400` even though the quota the client was
rejected against has already reset.

Found by Codex review on PR #5 (phase 52 quota enforcement). Verified against
the code before capturing: `quotaRejection` (handlers.go:1017-1026) calls
`secondsUntilUTCMidnight(time.Now())` fresh, with no dependency on when the DB
transaction ran.

**Not a safety violation.** The daily cap itself is not bypassed — a client
that retries immediately in this window would very likely succeed, because
the new day's budget is genuinely fresh. The defect is an overly conservative
`Retry-After` hint, not an under-enforcement. It self-corrects the moment the
client retries and gets admitted, but a strictly compliant client that
actually waits the advertised ~86400s before retrying would wait far longer
than necessary.

Deliberately deferred rather than fixed as part of PR #5 — see below.

## Solution

Not a point fix. `jobs.ErrDailyBytesLimit` / `jobs.ErrDailyJobsLimit` are
today plain sentinel errors with no payload, per this codebase's error
convention (`errors: package-level var Err<Reason>`, CLAUDE.md). Fixing this
correctly means carrying the DB-evaluated reset deadline (the `day` value the
transaction actually used) through the error path from `internal/jobs` up to
`internal/api`, which is an error-contract change touching the admission
package's public sentinel design — not something to do casually inside an
unrelated PR.

Options worth weighing when this is picked up:
- Extend the sentinel errors to carry a reset-deadline payload (e.g. wrap
  with `fmt.Errorf("%w: reset=%s", ErrDailyBytesLimit, deadline)` and parse
  it back out, or a small typed error struct implementing `Unwrap`).
- Recheck/derive the deadline defensively in the API layer in a way that
  doesn't require a DB round-trip.

Whichever approach is chosen, add a regression test that exercises the exact
straddle case (DB eval just before midnight, response formatted just after) —
the existing `TestSecondsUntilUTCMidnight` table only varies the input to
that pure function, not the DB-vs-API timing gap that causes the bug.
