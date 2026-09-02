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

## SC1: the live gate caught the fix being half a fix

The success criterion asked for a proven cold install, not a reasoned one. The
first run of it, on a namespace that had never existed, came back:

```
api ready after 31s (readyReplicas=1)
api restartCount=2
```

**With the initContainer in place and working.** Its own log reads
`waiting for postgres...` ×3 then `postgres is accepting connections`, and
`pg_isready` exited 0. The postgres race was closed. What the api died of was
the other one:

```
storage: check bucket "octoconv": Get "http://minio...:9000/octoconv/?location=":
dial tcp 192.168.194.228:9000: connect: connection refused
```

MinIO is a **second** fatal startup dependency of every binary here —
`storage.New` fails closed on an unreachable MinIO *and* on an absent bucket —
and the bucket is created by `octoconv-createbucket`, a **post-install hook**,
which by construction cannot have run when the main-release pods first start.
So on a cold install the api was guaranteed to crash at least once no matter how
healthy postgres was. The todo named postgres because postgres is what the
operator noticed; the class is "startup dependency not yet there".

The helper is now `octoconv.waitForDeps` with two initContainers: `pg_isready`,
and `mc ls local/$S3_BUCKET` against the same `S3_ENDPOINT`/`S3_BUCKET` the app
container reads — one probe covering both halves of the `storage.New`
precondition. It uses the pinned `mc` image the createbucket Job already uses,
so still no new supply-chain surface. Redis is deliberately **not** waited on:
neither `asynq.NewClient` nor `redis.NewClient` dials at construction, so an
unready Redis is not a startup fatal for any entrypoint here.

Re-run, same procedure:

```
api ready after 22s (readyReplicas=1)     # was 40s without the waits
api restartCount=0
api lastState={}
```

and with `worker` scaled up alongside it, both at `restartCount 0`, 21s to
Ready, the worker's own init log showing five `waiting for postgres...` lines.
Both runs recorded `Readiness probe failed` events for postgres *and* minio, so
the race was genuinely live in the run that produced the zero — not absorbed by
a warm cluster.

Evidence: `evidence/sc1-without-initcontainer.txt` (restartCount 3, the
reported defect reproduced), `evidence/sc1-halffix-postgres-only.txt`
(restartCount 2, the fix that looked right), `evidence/sc1-with-both-waits.txt`
and `evidence/sc1-with-both-waits-api-and-worker.txt` (restartCount 0).

**`helm install --wait` would now deadlock** — helm blocks on api Ready, api
blocks on the bucket, the hook that creates the bucket never runs. Every
octoconv install in `scripts/` is already deliberately async and says so in its
own log line; that is now a mechanical property of the repo rather than a
convention, because the deadlock costs a five-minute timeout to discover.

## The guard was silently green, and inverting it is the only reason that is known

`scripts/ci-helm-checks.sh` now asserts the property for every Deployment at
once — both waits, counted separately — so a future deployment cannot be added
without them, and the half-fix above cannot come back silently.

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

Inverted three ways, each failing distinctly:

* include removed from `deployment-worker.yaml` → `worker (postgres=0 bucket=0)`
* the bucket wait removed from the helper → all ten named as
  `(postgres=1 bucket=0)` — the exact half-fix the live gate caught
* `--wait` added to `keda-gate.sh`'s install → the deadlock guard fires, naming
  file and line. It also had to learn to ignore comment/log lines: several of
  these scripts announce "WITHOUT --wait" in their own output, and its first
  version fired on the sentence describing the rule.

## Review round 1 — one P2, and it was right

`postgres.user` was interpolated straight into the `sh -c` program. It is a
legal PostgreSQL role name and legal URI userinfo, so `app;false` renders as:

```
until pg_isready -h postgres.octoconv.svc.cluster.local -p 5432 -U app;false >/dev/null 2>&1; do
```

— an `until false` loop that never exits, leaving **every** application pod in
`Init` forever while Postgres sits there perfectly healthy. Operator-supplied
values, so this is a contract narrowing rather than an attack path: the chart
would have silently acquired a "no shell metacharacters" rule on a value that
never had one.

Host and user now travel as `env` (`PGHOST`, `PGUSER`) and are referenced
quoted. `scripts/ci-helm-checks.sh` renders the chart with
`--set 'postgres.user=app;false'` and asserts the string does **not** appear in
any initContainer command — plus that it *does* appear in the env, so the guard
cannot pass by rendering something that never used the value. Inverted: putting
the interpolation back names all four occurrences with their line numbers.

Cold install re-run after the change: api and worker both `restartCount 0`
(`evidence/sc1-with-both-waits-after-review-fix.txt`).

## Review round 2 — one P1, and it was right too

The `wait-for-bucket` initContainer dialled `http://` unconditionally while
`s3.useSSL` — an existing public chart value — says otherwise. Against a
TLS-only endpoint that probe can never succeed, and a probe that can never
succeed in an initContainer is strictly worse than no probe: all ten
Deployments would sit in `Init` forever instead of crash-looping once. The
fix's own failure mode was worse than the bug it fixed.

`octoconv-createbucket` had the same literal (`http://minio:9000`, which also
ignored `s3.endpoint` entirely). That one is pre-existing, but it is the same
class **and** the thing every waiting pod is waiting for — fixing only the
initContainer would have left the deadlock exactly where it was. Both now derive
the scheme at runtime from `S3_USE_SSL` and dial `$S3_ENDPOINT`.

### A second parser, so a differential test rather than a careful reading

Deriving the scheme in shell means a second implementation of
`internal/envcfg.ParseBool` — which strips a trailing `#` comment, trims, and
accepts exactly `true`/`false`, rejecting `TRUE`, `1`, `yes`, `on` and
`true anything`. This project has already paid for the "reason about the parser"
version of that mistake four times over a single bound, so:
`TestChartShellMirrorsParseBool` extracts the snippet **from the chart file**,
refuses to run if the extraction is empty or still contains a Helm action, and
runs it under `/bin/sh` for every row of `ParseBool`'s own documented table plus
two hostile values. Inverted twice — accepting `TRUE`, and dropping the comment
strip — it names the exact diverging row in each case.

`ci-helm-checks.sh` additionally asserts no `mc alias set` line in the whole
render hardcodes a scheme (12 invocations). Inverted, it names all three.

### The guard I added in round 1 was itself broken by `pipefail`

`if ! echo "$RENDER" | grep -q ...` — `grep -q` exits on its first match, which
SIGPIPEs the `echo` feeding it, and under `set -o pipefail` the pipeline exits
141. Read as "no match". It failed on a render containing **eleven** matches.
Both greps in that guard now count with `grep -c` and compare, which reads the
whole stream. The failure was loud, which is the only reason it cost minutes
rather than shipping.

## Gates

No Go file changed after the test run below (`git status` verified), so it
still stands for this tree.

`gofmt -l .` clean · `go vet ./...` clean · `go build ./...` clean ·
`go test ./... -count=1` exit 0 against live Postgres + Redis + MinIO
(25 packages) · `scripts/ci-helm-checks.sh` exit 0 (three new guards, each inverted) ·
`yq -e '.'` parses
`docs/openapi.yaml` and `docker-compose.yml`.

Live cold-install gate on OrbStack Kubernetes: five runs, evidence above --
the last one (`evidence/sc1-final-after-round2.txt`) taken after both review
rounds, api and worker at `restartCount 0`, 25s to Ready, with a
`Readiness probe failed` event for postgres in the same run so the race was
live rather than absorbed.

One earlier attempt at these two gates was invalidated by the environment, not
by the code: running the k8s cold install and the compose-backed live suite
concurrently wedged the OrbStack daemon, and six packages "failed" at exactly
480s because Postgres and Redis had become unreachable mid-run. `orbctl stop` +
`start` reported success without restoring it. Both gates were re-run serially
once the daemon came back. A wall of identical-duration timeouts is a fact about
the machine, not about the code, and is worth checking `docker ps` before
believing.

Local only — no GitHub Actions run.
