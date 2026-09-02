---
phase: 61-v1-12-debt-tail
status: passed
verified: 2026-08-30
requirements: [DEBT-09, DEBT-10, DEBT-11]
plans_executed: 3
---

# Phase 61 — Verification

Goal-backward, against the ROADMAP's three Success Criteria.

## SC1 — an operator can clear a stuck job with a supported tool, and the coupling is documented

**✓ SATISFIED.** `manage-clients cancel-job <job-id>` delegates to
`jobs.Repo.Cancel`, which frees the concurrency slot and records the job's daily
contribution **in one transaction**. `list-inflight` shows what is holding a
client's budget and for how long, because a remedy you cannot aim is half a
remedy.

Three tests keep it honest, two of them reading the source — the behaviour that
must not exist (a hand-written status update) cannot be observed by calling
anything.

The coupling is in `README.md`, in the section an operator reads during the
incident: the daily cap counts in-flight jobs, a job leaves that count only on a
terminal transition, and stranded jobs are recovered by the reconciler's sweeper
inside `cmd/webhook-worker`. With every webhook-worker down, "clients are running
out of quota" is a symptom of a stopped worker.

## SC2 — the README quota section is self-consistent and the engine list is derived

**✓ SATISFIED.** The claim that enforcement happens "before the upload touches
storage" was rewritten to match the passage twelve lines below that contradicted
it — the guarantee is no over-admission, not "nothing is uploaded".

`grep -c 'seven known engine classes' README.md` → **0**. The classes are
enumerated and held equal to `convert.AllEngines()` in **both directions**, plus
a narrow guard against any restated count. All three inversions redden with a
message naming the specific cause.

## SC3 — `PurgeUsage` takes its cutoff from Postgres

**✓ SATISFIED, and extended to its twin.** `grep -c 'time.Now()'
internal/jobs/retention.go` → **0**. `PurgeJobs` had the identical shape and was
fixed in the same commit; fixing the named instance and leaving the twin beside
it is the pattern phase 59 ended on.

Verified against a live PostgreSQL 18: all ten pre-existing retention tests pass
unchanged, plus a boundary test pinning that the fix did not move the cutoff.

## Requirements

| ID | Status | Evidence |
|---|---|---|
| DEBT-09 | ✓ SATISFIED | `cancel-job` / `list-inflight`; coupling documented in README |
| DEBT-10 | ✓ SATISFIED | Contradiction removed; engine list under a two-way doc-sync test |
| DEBT-11 | ✓ SATISFIED | Both purges compute their cutoff in SQL; live-DB verified |

## Gate

Local — CI auto-triggers are off (`a13e67b`).

```
go build ./...                       OK
go vet ./...                         OK
gofmt -l internal/ cmd/              empty
go test ./... (with live Postgres)   green
```

Postgres 18 was started for this phase specifically: an `$1::interval` binding
and a date/timestamp boundary comparison are not things to reason about, and
DEBT-11's whole subject is a mismatch that a correctly-synchronised machine
cannot reveal.

## Judgement calls, recorded rather than buried

**`FindStale` has the same clock defect and was left alone.** It compares against
`timestamptz`, so skew shifts its threshold proportionally rather than flipping a
whole day, and changing it alters reconciler recovery timing — outside DEBT-11
and carrying real risk. Named here so it reads as a decision.

**Phase 60 was skipped over, not forgotten.** It is `Autonomous: false`: SC5
requires an operator to measure a timeout on their own corpus, and two design
decisions are explicitly deferred to phase planning. Phase 61 declares no
dependencies, a disjoint file set, and "may run in any slot" — so it is the one
that can move while the operator is away.

## What each fix would have looked like if it were wrong

Worth recording, because two of the three had a plausible wrong version:

- The retention fix invited `::date - interval`, which reads better and moves
  the boundary a full day. A correctness fix that also changes what gets deleted
  is two changes.
- The README fix invited replacing "seven" with "eight", which is correct today
  and wrong at the ninth engine.
- The CLI comment quoting the SQL it forbids would have tripped its own grep
  gate and pushed the next person to weaken the test instead of the code.
