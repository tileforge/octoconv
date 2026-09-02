---
phase: 61-v1-12-debt-tail
plan: 02
subsystem: jobs
tags: [clock-source, retention, postgres, live-db-verified]
requires:
  - phase: 51-usage-rollup
    provides: "client_usage_daily.day written by Postgres, and UTCToday's rule that Go never touches the value"
provides:
  - "PurgeJobs and PurgeUsage compute their cutoff in SQL"
affects: []
status: complete
---

# Plan 61-02 — Summary

Commit `9e36384`. Closes DEBT-11.

## Both, not one

DEBT-11 names `PurgeUsage`. `PurgeJobs` had the identical shape and is fixed in
the same commit — phase 59 ended with a reviewer finding exactly that pattern: a
fix applied to the named instance with its twin left standing beside it.

Both derived their cutoff from the process clock and compared it against columns
Postgres writes. `evaluate()`, `recordUsage` and `UTCToday` already read their
clock from the database; these two were the last that did not.

**`PurgeUsage` is the sharper case.** `client_usage_daily.day` is a `DATE`, so
skew is not a proportional error: a container a few seconds off across midnight
moves a **whole day** of a client's usage rollup — the rows the daily quota is
computed from — into or out of the purge. For `PurgeJobs` the boundary shifts by
exactly the skew.

## The fix had a way to be right and wrong at once

Writing the boundary as `::date - interval` reads better than keeping the
time-of-day component, and silently moves the cutoff by up to a day, because a
`DATE` promotes to midnight when compared against a timestamp. **A correctness
fix that also changes what gets deleted is two changes, and only one was asked
for.** Both queries keep the time-of-day component, and a new boundary test pins
it: a job one minute inside the window survives, one minute outside does not.

## Verified against a real database

Postgres 18 started for this, because an `$1::interval` binding and a boundary
comparison are not things to reason about. All ten pre-existing retention tests
pass **unchanged**, plus the two new ones.

## The guard reads the source

On any host whose clock agrees with the database the two forms are behaviourally
identical, so no behavioural test would ever fail. The defect appears only where
it cannot be reproduced on demand. Inversion: restoring the host clock reddens
`TestRetentionTakesItsClockFromPostgres`.

## A comment that was half right

The old doc comment warned that a non-UTC boundary would drift the day, and
concluded that computing it in Go was fine as long as it was UTC. **The zone was
never the whole problem** — two correctly-zoned clocks that disagree by seconds
still disagree across midnight. Corrected rather than deleted, so the next reader
sees why the earlier reasoning fell short.

## Recorded, not fixed

`FindStale` (`repo.go`) has the same shape: a Go-computed cutoff against
Postgres-written timestamps. Left alone deliberately — it compares against
`timestamptz`, so skew shifts its threshold proportionally rather than flipping a
day, and changing it alters reconciler recovery timing, which is outside DEBT-11
and carries real risk. Named here so it is a decision rather than an oversight.
