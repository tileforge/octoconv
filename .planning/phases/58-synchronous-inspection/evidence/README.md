# Phase 58 (INSP-06/SC6) evidence

**Status: PASSED.** Run `20260816T021758Z`, 230 assertions, 200 rounds ×
9 concurrent `POST /v1/inspect` against a live OrbStack Kubernetes api
Deployment.

This run supersedes `20260815T235101Z`, which passed identically but against
an earlier binary. Round 8 of review changed the request path itself (a
bounded multipart intake stage, `internal/api/inspect_handlers.go`), so the
older evidence described code that no longer exists. The gate now refuses to
run at all when the image predates any Go source or the tree carries
uncommitted Go changes — the re-run was not a courtesy, it was the new
preflight's requirement:

```
PASS: octoconv-api:dev is at least as new as every Go source file
PASS: no uncommitted Go changes -- the image can correspond to the committed tree
PASS: deployed api image matches the validated tag == octoconv-api:dev
```

That check exists because `20260815T235101Z`'s evidence was sound only by
luck: its image happened to be built six minutes after the last Go commit,
and nobody verified it until a review finding asked.

The passing run is the last of eleven. The ten before it are the reason this
directory is worth reading: five of them were harness defects wearing a
verdict, and one of those was a PASS.

## What run 20260816T021758Z proves

| Claim | Observed |
|---|---|
| api container never restarted | `restart_count_before=2 restart_count_after=2` |
| `/healthz` stayed inside kubelet's liveness contract | 0 failures across 87 polls; longest consecutive run of failures 0, against `failureThreshold:6` |
| the observation window could have caught a restart | 87,369 ms against the contract's own 60,000 ms (`periodSeconds:10 × failureThreshold:6`, read from the live Deployment, not from a literal) |
| the binary matched the code under review | image newer than every `.go` file; no uncommitted Go changes; deployed image equals the validated tag |
| the burst reached the code under test | 1,585 of 1,800 requests genuinely inspected; every reject carried `inspect_busy`, none `rate_limited` — the binding constraint was the concurrency semaphore this gate is named after |
| all three pathological fixtures were actually parsed | ordinals 1/2/3 → 522 / 536 / 527 responses with status 200 |
| no request ever failed abnormally | 0 responses with 5xx, 0 with a status outside {200,429}, 0 with an empty status cell (a hung connection) |
| the ceilings under test were the ones deployed | `INSPECT_MAX_WORK_BYTES=1048576`, `INSPECT_MAX_UPLOAD_BYTES=104857600`, `INSPECT_MAX_CONCURRENT=4`, `resources.limits` cpu=2 memory=1Gi, all read back off the live pod |

## What it does NOT prove

- **Nothing about report correctness.** The fixtures stress ceilings, not
  parsers; `internal/inspect`'s own tests cover per-format correctness.
- **Nothing at `api.replicas > 1`.** The rate budget and the concurrency
  semaphore are per-process, in-memory state. This gate pins one replica
  deliberately (see `values-inspect-gate.yaml`), so it says nothing about
  a multi-replica fleet's aggregate behavior.
- **Nothing about the bounded intake stage added in round 8.** This gate
  drives 9 concurrent requests; the OOM scenario that stage closes needs
  roughly 30. A green run here is not evidence about it.
- **Nothing about the rate limiters.** All three (`ByIP`, `PerClient`,
  inspect's own) were lifted for this run precisely so they would stop
  shedding the burst before it reached the inspect path. Their own unit
  tests cover them.
- **Nothing about the engine worker classes.** They are pinned to zero
  replicas here; the full production topology does not fit one OrbStack
  node, and leaving them in made the measurement about node CPU rather
  than about inspection.

## The ten runs before it

Recorded because a gate's credibility is its failure history, and because
five separate defects here were in the *harness*, not the service.

| Run | Outcome | What it actually was |
|---|---|---|
| `20260812T221844Z` | FAIL at helm | KEDA CRDs absent |
| — | FAIL at `deployment/worker` | full topology does not fit one node (`Insufficient cpu`) |
| `20260812T223622Z` | **PASS, 36 assertions — and rejected** | see below |
| `20260812T224026Z` | FAIL | window 35.7 s < 60 s — the new assertion working |
| `20260812T224322Z` | aborted | killed mid-run at round 37 |
| `20260812T234109Z` | FAIL at round 6 | `exit 137`, cause unnamed |
| `20260815T230358Z` | hung at round 32 | see below |
| `20260815T234459Z` | FAIL at round 21 | watchdog fired; diagnostics ruled out OOM and eviction |
| `20260815T234900Z` | FAIL | 75 rounds clean, window 36.1 s < 60 s |
| `20260815T235101Z` | PASS, superseded | sound, but against the pre-round-8 binary; provenance unverified at the time |

### The PASS that proved nothing

`20260812T223622Z` reported GATE PASS on 36 assertions, every one of them
true. It still demonstrated almost nothing:

- The run lasted **10.3 seconds**. A kubelet restart needs six consecutive
  failures at `periodSeconds:10` — about a minute. The observation window
  could not contain the failure it claimed the api had survived, so
  "0 consecutive failures < 6" passed *vacuously*.
- `RATE_LIMIT_IP_RPM=60` shed 60 of 90 requests at the door. Only 27 were
  inspected; **3** reached the concurrency semaphore.
- Only the docx fixture was ever parsed. The pdf and mp4 fixtures were
  transferred, checksummed, reported present — and never inspected.

Two assertions now make the scenario prove it *could* have failed: the
observation window must outlast the liveness contract, and every fixture
ordinal must reach status 200 at least once. Both are in
`scripts/inspect-burst-liveproof.sh`, with this run named in their comments.

### The three hangs, and the two wrong fixes

Runs stalled at rounds 6, 21 and 32 with a shape that made no sense: driver
container `running` with `restartCount=0`, node reporting no pressure, api
pod ready with an unchanged restartCount, `/healthz` answering 200 in 1.4 ms
through a fresh exec into the very same poller pod.

The first two fixes read that evidence wrongly — memory headroom
(256Mi → 512Mi) and a per-round watchdog. Neither was the fault.

The fault was in the burst driver's start barrier: a `mkfifo` that the
parent fed one line per worker while N workers blocked on `read`. A FIFO
does not distribute lines one-per-reader, so one worker could buffer more
than its share and leave another blocked forever; `wait` never returned and
the exec stalled. Intermittent by construction — which is why it surfaced at
a different round each time and looked like a resource problem. The barrier
is now a file the workers spin on.

The watchdog earns its keep anyway: it converted an unbounded hang into a
bounded failure carrying the three cluster reads that ruled out OOM and
eviction, which is how the race became findable at all.

## Files

- `inspect-burst-20260816T021758Z-{burst,poll}.tsv` + `-transcript.log` —
  the passing run. Every assertion in the transcript is re-derivable from
  the two TSVs by a reader who never watched the run.
- `inspect-burst-20260815T235101Z-{burst,poll}.tsv` + `-transcript.log` —
  the superseded pass, against the pre-round-8 binary.
- `inspect-burst-20260815T234900Z-{burst,poll}.tsv` — 75 rounds clean,
  failed only the window assertion. Kept because it is the clearest record
  of that assertion doing its job.
- `inspect-burst-20260815T234459Z-FAILED-hung-round21-transcript.log` —
  the watchdog firing with its diagnostics, the run that made the race
  findable.
- `inspect-burst-20260812T22{3622,4026}Z-{burst,poll}.tsv` — the vacuous
  PASS and the first window failure.

## Reproducing

```bash
docker compose down                       # compose and k8s must never be hot together
docker build -f Dockerfile.api    -t octoconv-api:dev .
docker build -f Dockerfile.worker -t octoconv-worker:dev .
ROUNDS=200 ./scripts/inspect-burst-liveproof.sh   # refuses if the image predates any .go file
```

The script tears the k8s install down on exit, pass or fail. `ROUNDS` must
stay high enough that the run outlasts the liveness window — the gate
refuses to pass otherwise, and says by how much it fell short.

---

## Run 20260818T013247Z — SC6 PASSED

The gate's first passing run. 230 checks, exit 0, on `octoconv-api:dev` built
from `337add1` (STEP 1 refuses to run an image older than any `.go` file, and
separately refuses when the Go tree has uncommitted changes — both passed).

| assertion | observed |
|---|---|
| api `restartCount` before → after | **3 → 3, unchanged** (the gate's main ground truth) |
| max consecutive `/healthz` poll failures | **0**, against `failureThreshold: 6` |
| observation window | **89473 ms ≥ 60000 ms** required by `periodSeconds:10 × failureThreshold:6` |
| burst responses | 1800 rows (200 rounds × 9), **0 × 5xx**, 0 with a status outside 200/429, 0 hung |
| genuine accepts | **1593 × 200** |
| rejects | every 429 carried `inspect_busy` or `rate_limited` |

Poll evidence: `inspect-burst-20260818T013247Z-poll.tsv`.
Burst evidence: `inspect-burst-20260818T013247Z-burst.tsv`.

### What this run proves, stated narrowly

A pathological burst against a live api pod, driven from an in-cluster pod
through the Service ClusterIP, left `/healthz` answering for a window that
outlasts the restart contract by 50%, and the container never restarted.

### What it does NOT prove, and the numbers that say so

- **The load is bursty, not continuous.** Mean request duration was 37.8 ms
  (max 77 ms) while a round took ~486 ms, so the round pacing is dominated by
  `kubectl exec` overhead, not by the api. The api gets idle gaps between
  rounds. "Answered throughout a 90-second burst" is true; "answered under 90
  seconds of continuous saturation" is not what was measured.
- **Requests are cheap by construction.** `values-inspect-gate.yaml` sets
  `INSPECT_MAX_WORK_BYTES: 1048576`, so every analyzer stops at 1 MiB. The
  37.8 ms mean is a consequence of that ceiling, not evidence that inspection
  is cheap on arbitrary input.
- **The concurrency semaphore did engage**: 207 of 1800 responses were 429,
  all `inspect_busy` (`INSPECT_MAX_CONCURRENT: 4` against 9 concurrent
  requests per round). Unlike the 2026-08-15 run, nothing was shed by the rate
  limiter — this overlay sets every `RATE_LIMIT_*_RPM` to 1000000 precisely so
  the burst reaches the inspect path.

### Why `restartCount` starts at 2 or 3, not 0

Recorded because a non-zero baseline in the numbers above would otherwise look
like unexplained damage. It is not burst-related: **the api container
crash-loops at every fresh install until postgres accepts connections**,
reproduced deliberately on 2026-08-18 —

```
api-788b6f9d8d-7qpht  0/1  CrashLoopBackOff  1 (10s ago)
api-788b6f9d8d-7qpht  1/1  Running           2 (13s ago)
```

with the previous container's own log:

```
postgres: ping postgres: failed to connect to `user=octo database=octo_db`:
  dial tcp 192.168.194.210:5432: connect: connection refused
```

`cmd/api/main.go` uses `log.Fatalf` for startup dependency failures, so the
process exits(1) and kubelet restarts it until the dependency is up. The gate
is unaffected — it asserts the DELTA across the burst, not the absolute count
— but the behavior is real and is recorded as its own todo
(`.planning/todos/pending/2026-08-18-api-startup-crashloop-until-postgres.md`).

### Two refused runs preceded this one, and both refusals were correct

| run | ROUNDS | window | verdict |
|---|---|---|---|
| 20260818T012818Z | 10 | 10234 ms | refused: window < 60000 ms |
| 20260818T013007Z | 90 | 43738 ms | refused: window < 60000 ms |
| 20260818T013247Z | 200 | 89473 ms | **passed** |

Both refusals came from the anti-vacuity check added after the 2026-08-15 run
passed vacuously over a 10-second window. In the first of them, 36 assertions
were already green — including `restartCount` unchanged and zero `/healthz`
failures — and the gate still refused, correctly: a clean record over a window
shorter than the contract cannot distinguish "the api survived" from "the burst
ended first". The script's default `ROUNDS` was 10 at the time, i.e. a default
that could never pass; it is now 200, with the measured pacing recorded beside
it.
