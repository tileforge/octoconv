---
phase: 62-operational-debt-tail
plan: 01
status: complete
completed: 2026-09-02
requirements: [DEBT-12, DEBT-13, DEBT-14]
---

# 62-01 — Three debts, one of which turned out not to exist

## DEBT-14: verified, not fixed

The requirement came from a todo dated 2026-08-18 listing three open items.
Before writing any code I checked each against the source and ran the tests.
There was no work in it:

* **Item 1** — "a report can say `coverage.complete: true` while every check
  says `unknown/read_failed`" — was closed by Phase 59. `ReasonReadFailed` sits
  in `makesReportIncomplete`, and `TestBuildReportAllChecksReadFailedIsNotComplete`
  runs precisely the scenario the todo describes.
* **Item 2** — "four detectors return a bare bool, so a read failure is
  indistinguishable from *not this format*" — was closed by the
  `readSniffWindow` refactor: the window is read once outside the matchers and a
  genuine failure sets `sniffReadFailed`. Widening those four signatures is
  recorded at the call site as considered and **rejected** (D-12), because they
  are shared with the conversion path, which wants only a bool.
* **Item 3** — GitHub Actions billing. Not code.

Marked Complete rather than deleted: the requirement is correct, it was simply
satisfied before it was written. Inventing work to justify it would have been
the worse error, and the check cost ten minutes.

## DEBT-13: the test was asserting about the database, not about its job

`Sweeper.sweep` reaches stale jobs through `FindStale`, which takes no client
scope and sweeps the **whole** jobs table. That is correct — the reconciler
recovers every stranded job, not one client's. So the soak test's
`len(imageCalls) == 1` was asserting a property of the shared database, and
`go test ./...` runs packages in parallel against one. It failed intermittently
depending on which packages overlapped: once in phase 61 with one foreign job,
once in phase 60 with two.

The assertion now counts enqueues **for the job the test created**. A permanent
regression test (`TestSweepCountsAreNotAboutOneJob`) puts a foreign stale job in
the sweep on purpose and pins both halves: the own-job count is one, and a
total-count assertion demonstrably cannot hold.

## DEBT-12: an initContainer, applied to all ten pods rather than the one reported

The todo left the decision open with three options. Taken: **initContainer**,
for the reason the todo itself gives — every Go entrypoint here treats a startup
dependency failure as fatal, and "fatal means fatal" is what makes a non-zero
`restartCount` a signal. A bounded retry inside `main()` would have weakened
that in ten binaries to solve an ordering problem that belongs to the
orchestrator.

`octoconv.waitForPostgres` uses the pinned postgres image the chart already
deploys, so `pg_isready` costs no new supply-chain surface.

**Applied to every deployment that receives the app secret, not just api.** All
ten entrypoints have the same `log.Fatalf` shape; fixing the reported instance
and leaving nine identical ones is the pattern the previous milestone spent
eight review rounds learning to stop repeating. `mcp-http` and `asynqmon` are
correctly skipped — neither takes `commonEnv`, and `mcp-http`'s template says so
in its own header.

## The guard was silently green, and inverting it is the only reason that is known

`scripts/ci-helm-checks.sh` now asserts the property for every Deployment at
once, so a future one cannot be added without it.

Its first version ran a `yq` invocation per deployment inside a shell loop. The
per-name expressions errored, the errors went to `/dev/null`, the counts came
back empty, `${HAS_DB:-0}` defaulted to zero, and **the guard passed on a
deployment with the initContainer removed**. It was rewritten as a single `yq`
pass emitting `<name> <hasSecretEnv> <hasInitContainer>` triples, with an
explicit failure when zero deployments parse — a guard that can pass vacuously
is worse than none.

It then also had to move: it read `$DEFAULT_RENDER`, which the script deletes
before the negative cases run, so it failed on a missing file rather than on the
chart.

Inverted: removing the include from `deployment-worker.yaml` now fails the gate
with the deployment named.

## Gates

`gofmt -l .` clean · `go vet ./...` clean · `go build ./...` clean ·
`go test ./... -count=1` exit 0 against live Postgres + Redis + MinIO
(25 packages) · `scripts/ci-helm-checks.sh` exit 0 · `yq -e '.'` parses
`docs/openapi.yaml` and `docker-compose.yml`.

Local only — no GitHub Actions run.
