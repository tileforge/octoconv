---
phase: 61-v1-12-debt-tail
plan: 01
subsystem: cmd, jobs
tags: [operator-tooling, quota, guarded-transition, source-reading-tests]
requires:
  - phase: 52-quota-enforcement
    provides: "The daily cap that counts in-flight jobs, and jobs.Repo.Cancel's rollup-in-transaction contract"
provides:
  - "manage-clients list-inflight / cancel-job"
  - "jobs.Repo.FindInFlight, sharing quotaInFlightStatuses with the admission gate"
affects: [61-03]
status: complete
---

# Plan 61-01 — Summary

Commit `c519254`. Closes the code half of DEBT-09 (52 WR-01, the heaviest v1.12
carry-forward).

## The mechanism existed; the entrance did not

`jobs.Repo.Cancel` already ran the guarded transition — row lock, `{queued,
active}` allow-list, `job_events`, and the usage rollup **in the same
transaction**. Nothing needed writing except a way for an operator to reach it.

**That absence was not a convenience gap.** The hand-written status update an
operator resorts to skips `recordUsage`: the job stops being in-flight so the
slot frees, while its daily contribution is never booked. It repairs the quota
and breaks its accounting in one stroke — worse than the stuck job, because
nothing looks wrong afterwards.

## Two commands, because a remedy you cannot aim is half a remedy

`list-inflight` shows what holds a client's budget and for how long;
`cancel-job` releases one through the repo.

`FindInFlight` reads its status set from **`quotaInFlightStatuses`** — the same
slice `evaluate()` counts against `max_concurrent`. A second literal list would
drift, and a listing that disagreed with the gate would send an operator to
cancel jobs that were not the ones holding the budget: worse than no listing,
because it looks like it worked.

Age is computed by **Postgres**, not derived in the CLI from `CreatedAt`. The
quota gate reads its own clock from the database, and an operator shown a
host-clock age would be reasoning about a different timeline than the one
deciding whether the job still consumes budget. Adopted while writing rather
than left for DEBT-11 to find.

## Tests that read the source, and why

Two of the three read `main.go` rather than calling anything, because **the
behaviour that must not exist cannot be observed by calling**. The plausible
regression is someone dropping the `internal/jobs` dependency "to keep the CLI
light" — which reads as a simplification and is a correctness regression.

The third pins that both commands appear in the usage string. A remedy the
operator is never told about is, from their side, a remedy that does not exist —
which is what sent them to `psql` in the first place.

## One wording decision

The forbidden SQL is **never spelled out in `main.go`**, not even in prose,
because a comment quoting it trips its own gate and pushes the next person to
weaken the test instead of the code. This project has paid for that collision
once already (v1.9 milestone record). The comment says so, so the omission reads
as deliberate.

| Inversion | Red |
|---|---|
| direct statement instead of `Repo.Cancel` | `TestCLIIssuesNoDirectJobMutation`, `TestCancelGoesThroughTheRepo` |
| commands dropped from usage | `TestUsageListsOperatorJobCommands` |

## Deviation from the plan

The plan listed only `cmd/manage-clients` in `files_modified`. `FindInFlight`
went into `internal/jobs/repo.go` instead of the CLI, because the plan's own
acceptance criterion forbids SQL in the CLI and because the in-flight definition
had to be shared with the gate rather than restated. No wave conflict: 61-02
touches `retention.go`, a different file.
