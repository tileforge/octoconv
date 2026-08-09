---
phase: 51-attribution-accounting-retention
plan: 05
status: complete
completed: 2026-08-03
requirements: [ACCT-01, ACCT-04]
---

# Plan 51-05 Summary: autoscaling-contract gate

Both tasks pass. The accounting work did not touch the autoscaling contract, and the new surfaces behave in-cluster, not just in tests.

## Task 1 — static proof

```
$ git diff --name-only 50442cc..HEAD -- . ':!.planning' | grep -E "scaledobject-.*\.yaml|queue_collector\.go|^internal/queue/" | wc -l
0
```

The only file under `deploy/` in the phase diff is `configmap.yaml` (the retention env keys). No `scaledobject-*.yaml`, no `queue_collector.go`, nothing under `internal/queue/`.

```
=== RUN   TestJobDurationLabelsPinned
--- PASS: TestJobDurationLabelsPinned (0.00s)
=== RUN   TestQueueDepthLabelsPinned
--- PASS: TestQueueDepthLabelsPinned (0.00s)
ok  	github.com/apaderin/octoconv/internal/metrics	0.450s
```

Note for the record: `internal/convert/avdiskguard_test.go` also appears in the phase diff range. It is **not** phase work — it is a separate commit de-flaking a pre-existing test that raced against host free-disk state (it sampled `free`, then asked the guard for `free+1`, which the host could satisfy by the time the call ran; two of three consecutive runs passed). Committed separately so the PR's CI would not be randomly red.

## Task 2 — live gate on OrbStack k8s

Compose brought fully down first (four daemon wedges are on record from running both stacks hot). Images rebuilt from this tree and tagged `:dev`.

**`scripts/keda-gate.sh` — 21/21, matching the recorded baseline exactly:**

```
=== ALL 21 ASSERTIONS PASSED ===
SC1 (D-12a): octoconv_queue_depth resolved at genuinely 0 replicas -- PASS
SC2 (D-12b): image/document/html all scaled 0->1 from their own job type -- PASS
SC2 (D-12c): image class cycled back to 0 after cooldown -- PASS
SC3 (D-12d/D-09): webhook-worker held replicas:2, no ScaledObject, START/MID/END -- PASS
```

**Metric label sets, scraped from live pods after a real md→html conversion:**

```
# api pod
octoconv_queue_depth{queue="pandoc",state="active"} 0
octoconv_queue_depth{queue="pandoc",state="archived"} 0
octoconv_queue_depth{queue="pandoc",state="pending"} 0

# pandoc-worker pod
octoconv_client_job_outcomes_total{client_id="99a3d176-5f81-4da8-91ce-4301b97d6928",engine="pandoc",status="done"} 1
octoconv_job_outcomes_total{engine="pandoc",status="done"} 1
octoconv_job_duration_seconds_count{engine="pandoc",status="done"} 1
```

`queue_depth` still carries exactly `{queue,state}`; the new counter carries a real client UUID (not the `overflow` sentinel); the duration histogram did **not** gain `client_id`.

**The rollup, read straight from Postgres after that conversion:**

```
engine|status|job_count|bytes_in|bytes_out
pandoc|done  |1        |32      |3935
```

Bytes are summed over the job's inputs and outputs, so both are non-zero.

**Retention proven live, not merely unit-tested.** A terminal job was backdated 100 days and one sweep interval was allowed to pass:

```
aged BEFORE sweep: 1
aged AFTER  sweep: 0
```

and the counter materialised on the replica holding the advisory lock:

```
octoconv_reconciler_actions_total{action="purged_jobs"} 2
```

**No purge crash loop.** The webhook-worker pod shows `restartCount=2`, both from before the app came up: the container exited while Postgres was still refusing connections (`21:18:34`), and the current container has run since `21:18:55` without restarting. That is the project's existing no-initContainer startup race, unrelated to this phase. No purge or panic lines appear in the logs.

## Two operational notes worth keeping

**The metric checks initially came back empty, and the reason was mine.** Only `api`, `worker` and `webhook-worker` images were rebuilt; the job was served by `pandoc-worker`, whose `:dev` image was two days old and contained zero occurrences of `client_job_outcomes_total`. The absent counter was a stale image, not a code defect — confirmed by grepping the binary inside the image before and after a rebuild. Anyone re-running this gate should rebuild **every** engine worker image they intend to exercise.

**Prometheus counters emit nothing until first observation.** On a freshly installed stack with no jobs, `/metrics` returns 200 with zero `octoconv_*` series. That is expected, and it means this gate is only meaningful after a real conversion has run.

## Teardown

`helm uninstall` completed, `octoconv` namespace deployments back to 0, port-forwards killed, `k8s.enable=false`, Docker healthy. Baseline restored.
