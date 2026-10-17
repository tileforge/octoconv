---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 09
subsystem: cad-scaling
tags: [cad, keda, scale-from-zero, minReplicaCount, cold-start, image-size, orbstack, d-08, d-09, ar-4, ar-10, ar-17]
status: complete

requires:
  - phase: 76-03
    provides: evidence/76-03-derived.txt (DERIVED cad_engine_timeout_s=886)
  - phase: 76-08
    provides: the cad liveproof, ALL PASS; the quiet_host rule
provides:
  - scripts/cad-image-measure.sh (size, cold pull, network table, preflight and the D-09 DECISION line for one production image)
  - scripts/keda-cad-loadproof.sh (calibration, then a timed 0->2->0 with TRANSITION lines and a restore check)
  - docs/CAD.md ## Scaling, held to values.yaml, the configmap and both evidence files by TestCADDocMatchesTheCode/scaling
  - evidence/76-09-image-size.txt, evidence/76-09-keda-loadproof.txt (official), evidence/76-09-ci-local.txt
affects: [76-10, 76-11]

actuals:
  tokens: 47875
  tasks: 3
  commits: 14
plan_head_before: 323a85701d947cf16a373d67ea62299b9cc0f991
plan_head_after: 8682a5fdbad85615b7e0121109a038d00616dd74

tech-stack:
  added: []
  patterns:
    - "A doc row's source is `values.yaml keda.cad.<key>`, `configmap <VAR>` or `evidence/<file> <LINE>[<key>=<value>].<field>`; without a selector the line must occur once, with one the first match counts"
    - "Timed rows follow quiet_host: load1 <= 11 at the start and the end, otherwise CONTAMINATED and repeated (image measure) or QUIET contaminated=1 (loadproof)"
    - "Every docker, kubectl and helm call in the new scripts runs under a perl fork/alarm wrapper that SIGKILLs the client"

key-files:
  created:
    - scripts/cad-image-measure.sh
    - scripts/keda-cad-loadproof.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-image-size.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-ci-local.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/deferred-items.md
  modified:
    - docs/CAD.md
    - internal/convert/cad_doc_test.go

key-decisions:
  - "keda.cad.minReplicaCount stays 0 (D-09). The pre-registered premise holds: cold start 41.28 s <= 88.6 s (0.1 x the 886 s engine timeout)"
  - "The measured image and the loadproof's cad-worker:dev are byte-identical (both sha256:4c541fc15ece...), because the Go binary does not change when only scripts, docs and tests change"
  - "The loadproof scales the nine non-cad ScaledObject targets to 0 while helm install runs (INSTALL_ASSIST), after two attempts lost an install-time scheduling race on the 11-CPU node; cad-worker is untouched and the chart is unchanged"
  - "KEDA drains cad 2->0 directly one cooldown after the last job; the 900 s HPA window does not apply to the deactivation (the plan's open assumption, now resolved)"
  - "CADW-05 stays open: 76-10 and 76-11 still carry it"

requirements-completed: []

duration: 2026-10-16T13:45Z to 2026-10-16T15:50Z
completed: 2026-10-16
---

# Phase 76 Plan 09: cad scales from zero on OrbStack k8s, and minReplicaCount 0 stands on a measured cold start Summary

**The production cad-worker image is 315.5 MiB compressed and starts cold in 41.28 s at 100 Mbit/s, under the 88.6 s premise limit, so `minReplicaCount: 0` stands. On OrbStack k8s, from a fresh clone at 3af7906, one render scaled cad 0->1, three converts reached exactly 2 Ready replicas, every job ran once, and the worker drained 2->0 in 98.6 s. k8s was restored with a zero-line diff. docs/CAD.md `## Scaling` states all of it, and a subtest compares every row with the chart and the evidence.**

## Task 1 (tracer): the image measured, the decision, and ## Scaling

`scripts/cad-image-measure.sh` (c180f2e) copies the 71-08 method for one image:

- registry:2 and docker:dind by digest;
- one push, then a fresh dind daemon for each cold pull;
- a pull fails if any layer already exists, or if the fetched count differs from the manifest's layer count;
- three preflights under `--cpus 2 --memory 4g`, timed from the container's StartedAt to the `cad sandbox proven` line;
- the RULE lines are written before any timed row.

The 71 script is byte-identical to main.

The official run used a fresh clone at c180f2e and the image `octoconv-cad-worker:measure76-c180f2e` (id sha256:4c541fc15ece…, arm64). It ran from 13:59:55Z to 14:14:03Z. HDR has `tracked_dirt=0`. The measure76 tag and the clone were removed afterwards; `docker images` lists 0 `measure76-` tags.

| Row | Value |
|---|---|
| uncompressed / compressed | 1118.1 MiB / 315.5 MiB, 12 layers |
| COLD_PULL (clean) | 7.11 s, 6.56 s, 6.43 s; every pull fetched=12, already_exists=0 |
| PULL_CALC | 26.5 s at 100 Mbit/s, 2.6 s at 1000, 0.3 s at 10000 |
| PREFLIGHT (clean) | 7.67 s, 5.47 s, 5.02 s; each logged the landlock line, the proven line and the postgres attempt |
| CONTAMINATED rows, repeated | pull rep 1 twice (load1 end 18.97, then 15.59, with a 16.26 s wall), pull rep 3 once (end 11.60), preflight rep 2 once (end 11.78) |

The DECISION line:

```
DECISION,key=keda_cad_min_replica_count,value=0,source=D-09,cold_start_s=41.28,engine_timeout_s=886,premise_limit_s=88.6,premise_holds=1
```

**premise_holds=1.** cold_start_s = 7.11 + 26.5 + 7.67 = 41.28, and the limit is 0.1 x 886 = 88.6. values.yaml `keda.cad.minReplicaCount` is still 0, and values.yaml gained no line.

### RED, GREEN and the mutation

- RED at 64da821: `cad_doc_test.go:688: ../../docs/CAD.md has no "## Scaling" section`, giving `--- FAIL: TestCADDocMatchesTheCode/scaling`.
- GREEN at 0ca1aec: all 9 subtests pass.
- Mutation: the doc's minReplicaCount row set to 1 fails with `docs/CAD.md Scaling `minReplicaCount` = "1", values.yaml keda.cad.minReplicaCount gives "0"`. Reverted.

The subtest:

- reads values.yaml with a line parser over `keda:` -> `cad:`, and the configmap for CAD_WORKER_CONCURRENCY;
- resolves each evidence source;
- requires a row for every keda.cad key, the configmap row and every listed evidence source;
- requires the decision row to equal values.yaml minReplicaCount;
- requires the doc to say "the premise holds" exactly when premise_holds=1.

Tracer gate (auto chain): the Task 1 verify was re-run end to end and passed, so the plan expanded to Task 2.

## Task 2: the loadproof on OrbStack k8s

`scripts/keda-cad-loadproof.sh` is a clone of the pandoc loadproof with no comments.

- Ports 18099 and 15443.
- KEDA 2.20.1 and `values-local.yaml` only.
- Preflight: context orbstack, `orbctl config get k8s.enable` true, no running octoconv compose container, VM free disk at least 6 GiB.
- Snapshots of helm releases, namespaces, keda.sh CRDs, the external.metrics APIService, and every pod outside octoconv and keda with its restart count.
- It refuses to start if an octoconv or keda release or namespace already exists.
- The four frozen KEDA scripts are byte-identical to main.

No octoconv `:dev` tag existed before this plan (only `octoconv-api:latest`, `octoconv-cad-worker:latest` and `octoconv-av-worker:latest`). This plan built `octoconv-api:dev`, `octoconv-cad-worker:dev`, `octoconv-webhook-worker:dev` and `octoconv-mcp-http:dev`. **76-10 removes all four.** `docker compose -p octoconv stop` ran first; it stopped containers that were already stopped.

### Attempts

Every attempt ran from a fresh clone, built the four images in it with tracked_dirt=0, and ran under `caffeinate -dimsu` with a 60 s disk watchdog.

| # | Commit | Result | Cause | Restore |
|---|---|---|---|---|
| 1 | 767a82e | FAIL at node_fit: requested_cpu 8.86 of 11 | three non-cad worker pods from the first install still held 2 CPU each; the fit check ran before KEDA took them to 0 | ok, diff 0 |
| 2 | fc1e79f | every check passed | the MAX_REPLICAS line carried `samples` and `sample_errors` before `result=ok`, which the plan's verify pattern rejects | ok, diff 0 |
| 3 | 33b7713 | FAIL: deployment/api not Available in 300 s | api FailedScheduling, Insufficient cpu (14:47:04Z) | ok, diff 0 |
| 4 | bc1b6ed | FAIL: the createbucket hook hit its 120 s deadline | minio FailedScheduling, Insufficient cpu | FAIL at 15:11:46Z, then ok |
| 5 | 3af7906 | **PASS, official** | | ok, diff 0 |

- **Attempt 4's restore.** An av-worker pod stuck in its `wait-for-bucket` init container (grace 783 s) held the namespace past the 400 s wait. I recomputed the snapshot by hand at 15:13:53Z, after the pod's grace ended, and it matched SNAPSHOT_BEFORE line for line.
- **Root cause of attempts 3 and 4.** Each KEDA-owned Deployment starts at 1 replica at install. Ten workers at 2 CPU plus api at 2 CPU ask for about 22 CPU on an 11-CPU node. If api or minio loses the race, the queue metric never appears and the fallback keeps the workers at 1. The cause is recorded in evidence/76-09-keda-loadproof-attempt3-4-cause.txt and deferred-items.md.
- **Each re-run had a recorded cause.** Attempt 2's result was not re-counted: its file is kept whole, and the official run is attempt 5.

### Official run (attempt 5, evidence/76-09-keda-loadproof.txt)

- **HDR.** `commit=3af7906e…`, `tracked_dirt=0`, 15:18:14Z, load1 28.59 during the install, VM free 14.89 GiB.
- **IMAGE.** cad-worker:dev is sha256:4c541fc15ece…, the same id as the measured image.
- **INSTALL_ASSIST.** The nine non-cad targets were scaled to 0 in 27.4 s; cad-worker was untouched. SETTLE took 60 s.
- **ZERO, calibration.** replicas=0, fallback=False, metric s0-prometheus present at 0.
- **Node fit.** free_cpu 8.14 against 4 needed, free_mem 13526 MiB against 8192 needed.
- **CALIBRATION.**
  - Submitted at 15:21:44.437Z, Ready at 15:22:16Z: submit_to_ready_s=31.6, scheduled_to_started_s=1.0.
  - pull_events=absent: the Pulled event says "already present".
  - `pod_image_id=docker://sha256:4c541fc15ece7b2d884335429619058940f261ae69f01b5a6c99f724c7e334ca`. The awk check against the IMAGE line printed `IMAGE_MATCH`.
  - sandbox_proven=1, done, drained_s=94.0.
  - QUIET: load1 10.46 at the start, 8.71 at the end, contaminated=0.
- **ZERO, timed.** replicas=0, fallback=False, metric value 0.
- **TIMED.** Three NAUO1000 converts to stl, submitted at 15:23:56.559Z.

  | Job | attempts | queued->active | wall | finished |
  |---|---|---|---|---|
  | 99c76612 | 1 | 1 | 39.3 s | 15:24:35.901Z |
  | a4a913b6 | 1 | 1 | 43.0 s | 15:24:39.772Z |
  | 101ad988 | 1 | 1 | 46.5 s | 15:24:43.260Z |

- **TRANSITION lines, all of them:**

  ```
  TRANSITION,from=0,to=1,ready=0,spec=1,at_utc=2026-10-16T15:24:21.986Z,since_submit_s=25.4
  TRANSITION,from=1,to=2,ready=1,spec=2,at_utc=2026-10-16T15:24:32.667Z,since_submit_s=36.1
  TRANSITION,from=2,to=2,ready=2,spec=2,at_utc=2026-10-16T15:24:43.487Z,since_submit_s=46.9
  TRANSITION,from=2,to=0,ready=0,spec=0,at_utc=2026-10-16T15:26:21.855Z,since_submit_s=145.3
  ```

- **Drain path: 2->0 directly**, with no 2->1 step. `DRAIN,last_done_utc=15:24:43.260Z,zero_utc=15:26:21.855Z,drain_s=98.6,bound_s=1290,path=2->0,result=ok`.
- **MAX_REPLICAS.** observed=2 (`SAMPLER,interval_s=2,samples=59,sample_errors=0`; each sample also takes about 0.5 s of kubectl time). spec.replicas never exceeded 2.
- **POD_EVENTS.** Each of the three cad pods has Scheduled, Pulled ("already present"), Started and Ready timestamps, and no Pulling event.
- **QUIET, timed.** load1 8.82 at the start and 8.49 at the end, contaminated=0. load1 peaked at 11.58 between samples. That is within the pre-registered start/end rule, and stated here so it is not hidden.
- **RESTORE.** `snapshot_lines=43,diff_lines=0,octoconv_pods=0,k8s_enabled=true,result=ok`.
- **SUMMARY.** `checks=29,result=PASS`, 15:27:05Z, load1 8.37.

The Task 2 verify printed TASK2_VERIFY_PASS. After the run:

- no pod in octoconv or keda;
- `helm list -A` and `kubectl get ns` equal SNAPSHOT_BEFORE (9 lines);
- no keda.sh CRD and no external.metrics APIService, as before;
- k8s is still enabled;
- the clone was removed.

## Task 3: the OrbStack rows and the gate

- RED at b3c3c9b: four `docs/CAD.md Scaling has no row for evidence/76-09-keda-loadproof.txt …` failures, for CALIBRATION.submit_to_ready_s, TRANSITION[ready=2].since_submit_s, DRAIN.drain_s and CALIBRATION.pull_events.
- The subtest also requires the loadproof SUMMARY to be PASS and both QUIET steps to be contaminated=0.
- GREEN at b22ba24. The rows are submit to Ready 31.6 s, three jobs to 2 Ready in 46.9 s, drain 98.6 s, and pull events absent. The section adds the sentence that a registry-backed cluster adds the cold pull above.
- Mutation: submit-to-ready set to 32.6 fails with `… = "32.6", evidence/76-09-keda-loadproof.txt CALIBRATION.submit_to_ready_s gives "31.6"`. Reverted.
- The Task 3 verify printed TASK3_VERIFY_PASS.

**Gate.** `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate` from a fresh clone at b22ba24 (evidence/76-09-ci-local.txt):

- **ALL REQUESTED TIERS PASSED.**
- **internal/convert 406.505 s**, under the 540 s stop line.
- load1 was 7.52 at 15:35:51Z and 10.32 at 15:43:45Z, peaking near 15 in between.
- The launcher waited 345 s for load1 to fall to 8.

## Deviations from Plan

1. **[Rule 1] Node-fit race (fc1e79f).** Attempt 1 checked the fit before KEDA had taken the first install's worker pods to 0. `settle_all` now waits until no ScaledObject target holds a pod.
2. **[Rule 1] MAX_REPLICAS format (33b7713).** The sampler counts moved to a SAMPLER line, so the line has exactly the plan's fields.
3. **[Rule 2] Diagnostics (bc1b6ed).** Attempt 3 failed and printed nothing. `diag_ns` now dumps the namespace on every wait timeout.
4. **[Rule 3] Install assist (3af7906).** The chart's install-time starvation on an 11-CPU node blocked the proof in attempts 3 and 4. The script scales the nine non-cad targets to 0 during the install and records it. The chart and values are unchanged, so the cad tuning under test is still production. The chart issue is a Rule 4 decision and is logged in deferred-items.md.
5. **[Rule 1] Teardown namespace wait.** Raised from 400 s to 960 s, to outlast the 906 s cad grace.
6. **[Rule 2] quiet_host in the loadproof (767a82e).** Each trigger waits for load1 <= 11, and each step writes a QUIET line. The plan did not ask for this, but the user rule requires it for timed observations.
7. **quiet_host in the image measure.** A row above 11 at its start or its end is written as CONTAMINATED and repeated, up to 6 attempts. The extra fields (attempt, load1_start, load1_end) come after the plan's fields.
8. **Fixture order.** NAUO1000 is generated before the KEDA install rather than after the client mint, so no docker container runs while k8s holds the workload.
9. **Extra lines.** ZERO also checks that the external metric exists with value 0, and CALIBRATION_STEP lines record the healthz check and the node fit. Both are stricter than the plan.
10. **Smoke run.** One smoke run of the measure script on the main tree, against `octoconv-cad-worker:latest`, wrote only to the scratchpad. It is not evidence.

## NOT RUN

- The race, docker-build and e2e tiers: the plan asks only for helm and gate. 76-11 runs them.
- A registry-backed cold pull inside k8s: not possible on the OrbStack shared image store. The registry pull is measured separately with dind.

## Known Stubs

None.

## Threat Flags

None new. Each threat was mitigated as follows:

- T-76-19: snapshot and restore, diff 0.
- T-76-20: node fit, and a sampler maximum of exactly 2.
- T-76-21: every queue seeded, and ZERO with fallback=False and a metric of 0.
- T-76-22: the pod imageID equals the local id.
- T-76-23: the key is never printed, and no transcript contains it.

## Disk and leftovers

- **Disk at the end.** VM free 14.98 GiB, host 19 GiB. At the start they were 23.5 GiB and 28 GiB.
- **Images pulled by this plan and left in place, as the plan says:** `registry@sha256:a3d8aaa6…` (25 MB) and `docker@sha256:0b6d18a4…` (325 MB).
- **Build cache** from the four rebuilds is still in place. Nothing was pruned.

## Self-Check: PASSED

- All six key files exist, and so do the evidence attempt files and the transcripts.
- The 14 commits from 323a857 to 8682a5f are in HEAD. All are signed G, and none has a Co-Authored-By trailer.
- `git diff --exit-code main -- internal/convert/exec.go scripts/cad-image-size-measure.sh scripts/keda-pandoc-loadproof.sh scripts/keda-ocr-loadproof.sh` exits 0.
- No comment line in either new script; no new `#` line in values.yaml; no `//` line added to cad_doc_test.go.

## Follow-up

2026-10-17, quick 261017-doa: on Codex round 2 of PR #37, scripts/keda-cad-loadproof.sh now fails the proof when its calibration or timed step is contaminated (before, it recorded contaminated=1 and could still end PASS); the official PASS stands, because both QUIET lines record contaminated=0 (evidence/76-09-keda-loadproof.txt lines 63 and 78) and SUMMARY result=PASS is line 81.
