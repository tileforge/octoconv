---
quick_id: 260803-obt
title: Close Codex review findings on PR #5 (phase 52 quota admission)
date: 2026-08-03
status: complete
branch: phase-52-quota-enforcement
tasks: 3/3
commits:
  - b0ab4c3
  - 24416ba
  - ab52e0c
reconstructed: true
---

> **Note on provenance.** The executor wrote its SUMMARY.md inside its git
> worktree but — per the quick-workflow constraint forbidding docs commits —
> never committed it, and the worktree's post-merge rescue did not fire before
> removal. The original file is unrecoverable (absent from disk and from every
> branch's history). **This file is reconstructed from the executor's own
> return report, not read from the original.** Commit hashes, test names and
> the measured failure numbers below are quoted from that report; everything
> stated here is independently confirmable from the commits themselves.

# Quick Task 260803-obt — Close Codex PR #5 findings

Four findings from Codex's review of open PR #5, grouped into three tasks. Every
premise was verified empirically before planning (see PLAN.md), so no task
re-investigated its own justification.

## Task 1 — P1 (migration drop) + P2a (false midnight claim) — `b0ab4c3`

`internal/db/migrations/0014_client_usage_canceled.sql:44` now uses a bare
`DROP CONSTRAINT` instead of `DROP CONSTRAINT IF EXISTS`, matching the
convention of migrations 0005/0006/0009/0010. With `IF EXISTS`, a database
whose original status CHECK carried a different name would keep that
constraint, the following `ADD CONSTRAINT` would succeed alongside it, and the
surviving old `CHECK (status IN ('done','failed'))` would reject every
canceled-usage row — leaving `Repo.Cancel` returning 500 indefinitely while the
migration was recorded as successful. The migration header now records why the
drop is bare and why the fix was made in place rather than as a new 0015.

The false across-midnight claim was removed from `internal/jobs/admission.go`
and `README.md` and replaced with a description of what actually happens: the
ledger row lands on the day the terminal transaction **started**, not the day it
committed, because `now()` is frozen at transaction start.

`internal/jobs/usage.go` was deliberately not touched (empty diff) — switching
`now()` to `clock_timestamp()` would have made `day` non-deterministic within a
transaction.

## Task 2 — P2b (lock-scope regression test) — `24416ba`

Added `TestConcurrentBurstDailyJobsCapAdmitsExactlyOne`: 12 concurrent creates
against `max_daily_jobs = 1` with no concurrency cap, so the **daily** term —
not the concurrency term — is what must be serialized.

**Load-bearing proof (the point of the test):** with `pg_advisory_xact_lock`
temporarily disabled, 4 of 5 runs failed with overshoot — `okCount` of 3, 7, 8
and 11 instead of 1. Restored, the test passes stably. The prior test
(`TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher`) does two sequential
creates and would have kept passing through that same mutation.

## Task 3 — P3 (`awaiting_upload` classification) — `ab52e0c`

Introduced three named sets in `admission.go` — `quotaInFlightStatuses`,
`quotaLedgerStatuses`, `quotaUnaccountedStatuses` — declaring how every job
status participates in quota accounting. **The SQL queries themselves were not
changed**; this is a declaration plus a guard, not a behavior change.

Added `TestEveryJobStatusIsClassifiedForQuotaAccounting`, which reads the live
schema (`pg_get_constraintdef` for `jobs_status_check`, `pg_get_indexdef` for
`jobs_client_engine_inflight_idx`) and fails if the status domain gains an
unclassified value or the index predicate drifts from the declaration.

**Load-bearing proof:** removing `StatusAwaitingUpload` from the classified set
made the test fail naming exactly the missing status; restored, it passes.

Rationale for classifying rather than including `awaiting_upload` in the
in-flight predicate: the byte term sums `job_inputs.size_bytes`, and for a job
in `awaiting_upload` that row either does not exist yet (so including the status
buys the byte term nothing) or carries a client-declared size not reconciled
against any stored object. That choice belongs to whichever phase actually
introduces a two-phase upload flow, since only it can say whether a trusted size
exists at that point. Pre-deciding it now would pin semantics no test can
exercise — the status is unreachable through every repository API today.

## Verification

- `gofmt -l .` clean; `go build ./...` and `go vet ./...` without error
- `go test ./... -count=1` — all packages `ok`, including `internal/jobs`
- Full migration chain proved end-to-end on a fresh database (`octo_migtest`,
  created and dropped); the already-migrated dev database `octo_db` confirmed
  at target state with no action required

## Deviation recorded by the executor

Task 3's acceptance criterion expected
`grep -c "status IN ('queued', 'active')" internal/jobs/admission.go` to output
2, but the actual count is 3. This was **already true at the base commit
`2e78279`**, before any change in this task — the third match is a pre-existing
comment, not a rewritten query. The criterion's intent (queries not rewritten)
was confirmed instead via `git diff`. Not a defect; the criterion was written
against a miscount.
