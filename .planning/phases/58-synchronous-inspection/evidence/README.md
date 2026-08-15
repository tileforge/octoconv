# Phase 58 (INSP-06/SC6) evidence

**Status: PASSED.** Run `20260815T235101Z`, 230 assertions, 200 rounds ×
9 concurrent `POST /v1/inspect` against a live OrbStack Kubernetes api
Deployment.

The passing run is the last of ten. The nine before it are the reason this
directory is worth reading: four of them were harness defects wearing a
verdict, and one of those was a PASS.

## What run 20260815T235101Z proves

| Claim | Observed |
|---|---|
| api container never restarted | `restart_count_before=2 restart_count_after=2` |
| `/healthz` stayed inside kubelet's liveness contract | 0 failures across 86 polls; longest consecutive run of failures 0, against `failureThreshold:6` |
| the observation window could have caught a restart | 86,238 ms against the contract's own 60,000 ms (`periodSeconds:10 × failureThreshold:6`, read from the live Deployment, not from a literal) |
| the burst reached the code under test | 1,566 of 1,800 requests genuinely inspected; all 234 rejects carried `inspect_busy`, none `rate_limited` — the binding constraint was the concurrency semaphore this gate is named after |
| all three pathological fixtures were actually parsed | ordinals 1/2/3 → 514 / 528 / 524 responses with status 200 |
| no request ever failed abnormally | 0 responses with 5xx, 0 with a status outside {200,429}, 0 with an empty status cell (a hung connection) |
| the ceilings under test were the ones deployed | `INSPECT_MAX_WORK_BYTES=1048576`, `INSPECT_MAX_UPLOAD_BYTES=104857600`, `INSPECT_MAX_CONCURRENT=4`, `resources.limits` cpu=2 memory=1Gi, all read back off the live pod |

## What it does NOT prove

- **Nothing about report correctness.** The fixtures stress ceilings, not
  parsers; `internal/inspect`'s own tests cover per-format correctness.
- **Nothing at `api.replicas > 1`.** The rate budget and the concurrency
  semaphore are per-process, in-memory state. This gate pins one replica
  deliberately (see `values-inspect-gate.yaml`), so it says nothing about
  a multi-replica fleet's aggregate behavior.
- **Nothing about the rate limiters.** All three (`ByIP`, `PerClient`,
  inspect's own) were lifted for this run precisely so they would stop
  shedding the burst before it reached the inspect path. Their own unit
  tests cover them.
- **Nothing about the engine worker classes.** They are pinned to zero
  replicas here; the full production topology does not fit one OrbStack
  node, and leaving them in made the measurement about node CPU rather
  than about inspection.

## The nine runs before it

Recorded because a gate's credibility is its failure history, and because
four separate defects here were in the *harness*, not the service.

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

- `inspect-burst-20260815T235101Z-{burst,poll}.tsv` + `-transcript.log` —
  the passing run. Every assertion in the transcript is re-derivable from
  the two TSVs by a reader who never watched the run.
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
ROUNDS=200 ./scripts/inspect-burst-liveproof.sh
```

The script tears the k8s install down on exit, pass or fail. `ROUNDS` must
stay high enough that the run outlasts the liveness window — the gate
refuses to pass otherwise, and says by how much it fell short.
