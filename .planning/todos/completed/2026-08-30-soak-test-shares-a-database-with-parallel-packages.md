---
created: 2026-08-30
area: testing
severity: low
resolves_phase: 62
resolved: 2026-09-02
---

> **Resolved by phase 62 (DEBT-13).** The reconciler soak test now asserts about
> the job it created, keyed by its own `jobID`, rather than about the contents of
> a table other packages write to in parallel.

# The reconciler soak test can be broken by another package's in-flight job

`TestSoakStaleRecoveryCountsUsageExactlyOnce` asserts **exactly one** enqueue for
the job it created. `Sweeper.sweep()` reaches that job through
`jobs.Repo.FindStale`, which takes no client scope — it sweeps the **whole
`jobs` table**. So any in-flight job present in the shared database while that
test runs is swept too, and the assertion fails on a job the test never created.

Observed once during phase 61, in a full `go test ./...` run:

```
imageCalls = [fe04c736-… ace0b5c1-…], want exactly one enqueue for ace0b5c1-…
```

`fe04c736` belonged to another test. It passes in isolation, and two subsequent
full-suite runs were green, so it is intermittent and depends on which packages
happen to overlap — `go test ./...` runs packages in parallel against one
database.

**Not a production defect.** The table-wide sweep is correct: the reconciler is
supposed to recover every stranded job, not one client's. The weakness is in the
test's assumption that it is alone in the database.

**Why it was not fixed in phase 61.** It touches test isolation across packages
— scoping the soak test's assertion to its own job ids, or serialising the
DB-backed packages — and phase 61's file set was deliberately disjoint from the
rest of the milestone. Folding an unrelated test-infrastructure change into it
would have added exactly the review noise that phase's separate existence was
meant to avoid.

**Shape of the fix when it is taken up:** assert on the enqueues for the ids the
test created rather than on the total call count. That keeps the test honest
about the sweeper's real, table-wide contract instead of pretending the database
is private to it.

## Second observation, 2026-09-01 (phase 60, PR #12 review round 2)

Recurred in a full `go test ./...` run against live Postgres:

```
imageCalls = [a46c70ce-… 1f0a3e42-… e39c6ff4-…], want exactly one enqueue for e39c6ff4-…
```

**Two** foreign jobs this time, not one. The package passed in isolation
immediately afterwards, and the next full run was green — same intermittent
shape, same cause.

Worth recording rather than re-diagnosing: the failure is now confirmed across
two separate phases and two different working sets, so it is a property of the
test's assumption rather than of anything either phase changed. It also costs
real time during review rounds, because a red full-suite run has to be
investigated before it can be dismissed.
