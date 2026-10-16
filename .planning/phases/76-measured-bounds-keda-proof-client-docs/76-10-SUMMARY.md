---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 10
subsystem: cad-scaling
tags: [cad, keda, hpa, downscale-survival, pod-deletion-cost, grace, orbstack, d-05, d-08, d-18, ar-18, ar-19]
status: complete

requires:
  - phase: 76-03
    provides: evidence/76-03-cells.txt CELLSPEC, evidence/76-03-inworker.txt INWORKER_ROW, evidence/76-03-derived.txt (engine 886, grace 906, format ceilings, PATHB fired=0)
  - phase: 76-09
    provides: scripts/keda-cad-loadproof.sh (preflight, snapshot, INSTALL_ASSIST, restore), SNAPSHOT_BEFORE, the four :dev tags to remove
provides:
  - deploy/chart/octoconv/values-loadproof.yaml keda.cad.scaleDownStabilizationSeconds 15
  - scripts/keda-cad-downscale-survival.sh [--calibrate-only] (LONG by a pre-registered band rule, busy pod by engine census, the 2->1 survival checks, restore with a local-port check)
  - evidence/76-10-keda-downscale-calibration.txt (PASS), evidence/76-10-keda-downscale.txt (official, PASS), evidence/76-10-keda-downscale-attempt1-fail.txt, evidence/76-10-ci-local.txt
affects: [76-11]

actuals:
  tokens: 19050
  tasks: 2
  commits: 6
plan_head_before: 9f4080385b65512f7bea67de70f15ae13c4c7ca7
plan_head_after: 1468d8e4852be53231a56e6ff821b74d1d9ad5bc

tech-stack:
  added: []
  patterns:
    - "The busy pod is the pod whose /proc holds an engine process (comm, or a python cmdline naming cad-guard/cad-mesh/cad-render/cad-dxf, or rsvg-convert), and it must also be the earliest pod and the pod LONG went active on"
    - "A Killing event has second resolution: SIGTERM-before-finish uses the second + 1 s, annotation-before-SIGTERM uses the truncated second"
    - "A backgrounded timeout-wrapped kubectl is stopped with TERM to the wrapper's children, and RESTORE fails while a local port stays bound"

key-files:
  created:
    - scripts/keda-cad-downscale-survival.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-10-keda-downscale-calibration.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-10-keda-downscale.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-10-keda-downscale-attempt1-fail.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-10-ci-local.txt
  modified:
    - deploy/chart/octoconv/values-loadproof.yaml
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/deferred-items.md

key-decisions:
  - "LONG is render_step M2 (render to png, size 2048, draft, iso, opaque): the smaller of the two cells in the 180-590 s band (194.90 s; dxf_png L170030 is 355.95 s)"
  - "A cad render in flight survives the KEDA/HPA 2->1 scale-down: SIGTERM at least 161.7 s before the job finished, one attempt, Completed exit 0 after 162.0 s against the 906 s grace"
  - "The deletion-cost annotation races the HPA when SHORT finishes before the second pod is Ready (0.26-1.26 s margin in the official run); hardening is deferred (deferred-items.md)"
  - "CADW-05 stays open: 76-11 still carries it"

requirements-completed: []

duration: 2026-10-16T15:55Z to 2026-10-16T16:45Z
completed: 2026-10-16
---

# Phase 76 Plan 10: a long cad render survives the KEDA scale-down that picks its own pod Summary

**On OrbStack k8s, from a fresh clone at 86a4d05, a 197 s render of M2.step went active on the only cad pod. A second job took the Deployment to 2. The pod holding `cad-mesh` was annotated with deletion cost -1000, and the HPA's 2->1 step sent it SIGTERM at 16:22:29Z. The render finished at 16:25:11.714Z with one attempt, and the pod exited Completed with exit 0, 162.0 s after SIGTERM and inside the 906 s grace. k8s ended equal to 76-09's SNAPSHOT_BEFORE (43 lines), and the four phase-76 `:dev` tags are gone.**

## Task 1 (tracer): the overlay key and a calibrate-only k8s cycle

- **Overlay (d862c79).** values-loadproof.yaml gains `keda.cad.scaleDownStabilizationSeconds: 15`, with no comment. The rendered cad ScaledObject window is 15 with the overlay and 900 with values-local.yaml alone. The cad Deployment grace is 906 = DERIVED 886 + 20 in both renders. Rendered with and without the overlay, the chart differs only in that window line.
- **Script.** `scripts/keda-cad-downscale-survival.sh` is a new file with no comments, built on the shape of keda-cad-loadproof.sh, which is unchanged since 76-09:
  - preflight, snapshot, image check, INSTALL_ASSIST and restore;
  - ports 18099 and 15443;
  - the compose refusal names `docker compose -p octoconv stop`.
- **Extra checks before any job.**
  - Configmap: CAD_ENGINE_TIMEOUT equals DERIVED, CAD_WORKER_CONCURRENCY is 1, and MAX_UPLOAD_BYTES is 104857600.
  - RENDER_CHECK runs helm template with and without the overlay.
  - After install, LIVE_CHECK reads the live ScaledObject window (15) and the Deployment grace (906).

### The pre-registered LONG rule

RULE `long_select`, `band` and `survival` are written before any job. The rule takes:

- the CELLSPEC lines of the last `derive76_select` block with result=ok;
- render cells only, at most 104857600 bytes and at most their format ceiling;
- PATHB fired=0 for every format, so the render ceiling is the format ceiling;
- the largest in-worker attempt wall of each cell (INWORKER_ROW status=done);
- the band [180, floor(886/1.5) = 590], keeping the smallest wall inside it.

```
LONG_CANDIDATES,list=render_step:M2:194.90:eligible+render_iges:IR16MiB:64.27:below_band+render_stl:S1_k256:39.50:below_band+dxf_png:L170030:355.95:eligible
LONG_SELECT,route=render_step,cell=M2,file=/Users/apaderin/.cache/octoconv/cad-corpus/scaled/b76/M2.step,sha256=4e1361e2…abe91,bytes=1474086,target=png,operation=render,opts=size:2048;quality:draft;view:iso;background:opaque,format_ceiling_bytes=66060288,inworker_rows=3,expected_wall_s=194.90,band_low=180,band_high=590,result=ok
```

- **sha256.** The selected sha256 equals both CELLSPEC lines for render_step M2 (awk: `cellspec_matches=2 sha_equal=2`).
- **Dry tests.** Before the live run, the selection was tested against the real files:
  - band 300-590 picks dxf_png L170030;
  - band 400-590 writes STOP,reason=no_long_candidate_in_band;
  - band 200-300 writes the same STOP.
- **The full run** re-applies the rule and checks that it picks the calibration's choice (LONG_SELECT_MATCH).

### Calibration (evidence/76-10-keda-downscale-calibration.txt)

- Fresh clone at d862c79, tracked_dirt=0. `docker compose -p octoconv stop` ran first; the four `:dev` images were rebuilt in the clone, and their ids equal 76-09's.
- The run went from 16:04:57Z to 16:09:50Z.
- /healthz returned 200 through the port-forward.
- The units_inch.step render scaled cad 0->1. The pod was Ready 15.0 s after creation (submit to Ready 17.7 s) and logged `cad sandbox proven`.
- The pod imageID equals the local cad-worker:dev id `sha256:4c541fc15ece…`. Its grace is 906, equal to the expected 906.
- The job ended done, and cad drained to 0 in 85.0 s.
- QUIET: load1 9.36 at the start, 8.38 at the end, contaminated=0.
- RESTORE `snapshot_lines=43,diff_lines=0`; SUMMARY `checks=28,result=PASS`.
- **Tracer gate (auto chain).** The Task 1 verify was re-run end to end and printed TASK1_VERIFY_PASS, so the plan expanded to Task 2.

## Task 2: the survival run

### Attempts

| # | Commit | Result | Cause |
|---|---|---|---|
| 1 | c160bef | FAIL at preflight, `local port 18099 is in use` | the calibration's teardown had left both kubectl port-forwards running; no k8s step ran |
| 2 | 86a4d05 | **PASS, official** | |

Both attempts ran from fresh clones with the four images rebuilt (same ids), under `caffeinate -dimsu`, with a 60 s VM-disk watchdog. Attempt 1's file is kept as evidence/76-10-keda-downscale-attempt1-fail.txt.

### Official run (evidence/76-10-keda-downscale.txt)

- **HDR.** commit 86a4d05f…, tracked_dirt=0, 16:17:07Z, load1 18.37 during the install, VM free 11.84 GiB.
- **Calibration again.** units_inch.step 0->1, Ready 10.0 s after creation, sandbox proven, imageID match, grace 906, drained in 88.5 s, QUIET clean.
- **LONG.** M2.step submitted at 16:21:31.555Z (load1 7.22), with opts `{"size":2048,"quality":"draft","view":"iso","background":"opaque"}`. It went active at 16:21:54.8Z on `cad-worker-b4bb77ddb-bnqg2`, the only cad pod.
- **SHORT.** units_inch.step render submitted at 16:21:55.778Z. cad went 1->2 and reached 2 Ready at 16:22:27.457Z.
- **BUSY.** The first census, 0.9 s after 2 Ready, found:
  - `bnqg2=cad-mesh:201` and `62rqh=none`;
  - the engine-holding pod is the earliest pod and the pod LONG went active on;
  - the annotation was read back as -1000;
  - spec.replicas was 2 when the annotation landed.

  ```
  BUSY,pod=cad-worker-b4bb77ddb-bnqg2,engine_procs=cad-mesh:201,earliest=1,deletion_cost=-1000,grace_s=906,expected_grace_s=906,result=ok
  ```

- **Grace.** The busy pod's spec grace is 906, against DERIVED engine 886 + 20 = 906.
- **SHORT done** at 16:22:24.796Z. That is before the sampler saw the second pod Ready, because asynq takes the task before the readiness probe passes.
- **DOWNSCALE.** spec and status reached 1 at 16:22:31.983Z, 7.2 s after SHORT finished, inside the 240 s window.
- **SIGTERM and ORDER.**

  ```
  SIGTERM,pod=cad-worker-b4bb77ddb-bnqg2,killing_at=2026-10-16T16:22:29Z
  ORDER_DETAIL,annotated_utc=2026-10-16T16:22:28.740Z,killing_at=2026-10-16T16:22:29Z,long_finished_at=2026-10-16T16:25:11.714Z,sigterm_second_upper_bound=2026-10-16T16:22:30.000Z,sigterm_to_finish_s=161.7,annotation_before_sigterm=1
  ORDER,sigterm_before_finish=1,result=ok
  ```

- **LONG result.** `attempts=1,queued_to_active=1,status=done,started_at=16:21:54.707Z,finished_at=16:25:11.714Z,attempt_wall_s=197.01,in_band=1`. The k8s wall is 2.1 s above the largest compose wall (194.90 s).
- **POD_TERMINATION.** `reason=Completed,exit=0,finished_at=2026-10-16T16:25:11Z,since_sigterm_s=162.0,grace_s=906,source=watch`. The watch saw the pod go Running -> Succeeded (Completed, 0) -> DELETED. There was no exit 137, SIGKILL or OOMKilled.
- **TRANSITION lines, all of them:**

  ```
  TRANSITION,from=0,to=1,ready=0,spec=1,at_utc=2026-10-16T16:21:46.825Z,since_submit_s=15.3
  TRANSITION,from=1,to=1,ready=1,spec=1,at_utc=2026-10-16T16:21:58.270Z,since_submit_s=26.7
  TRANSITION,from=1,to=2,ready=1,spec=2,at_utc=2026-10-16T16:22:16.174Z,since_submit_s=44.6
  TRANSITION,from=2,to=2,ready=2,spec=2,at_utc=2026-10-16T16:22:27.457Z,since_submit_s=55.9
  TRANSITION,from=2,to=1,ready=1,spec=1,at_utc=2026-10-16T16:22:31.983Z,since_submit_s=60.4
  TRANSITION,from=1,to=0,ready=0,spec=0,at_utc=2026-10-16T16:26:46.401Z,since_submit_s=314.8
  ```

- **DRAIN** `drain_s=94.7,bound_s=405`. SAMPLER: 144 samples, 0 errors, max 2 replicas.
- **QUIET (LONG row).** load1 7.22 at the start and 7.23 at the end, contaminated=0. load1 peaked at 11.27 between samples. That is allowed by the start/end rule and is stated so it is not hidden.
- **RESTORE** `snapshot_lines=43,diff_lines=0,octoconv_pods=0,k8s_enabled=true,local_ports_free=1,result=ok`. **SUMMARY** `checks=40,result=PASS` at 16:27:28Z, load1 6.94.
- The Task 2 verify printed TASK2_VERIFY_PASS.

### Timings at a glance

| Event | UTC |
|---|---|
| LONG started (jobs.started_at) | 16:21:54.707 |
| SHORT finished | 16:22:24.796 |
| busy pod annotated | 16:22:28.740 |
| Killing (SIGTERM) on the busy pod | 16:22:29 (second resolution) |
| status.replicas 1 | 16:22:31.983 |
| LONG finished | 16:25:11.714 |
| busy pod container finished | 16:25:11 (Completed, 0) |
| cad at 0 | 16:26:46.401 |

### Restore to the pre-phase state

- **Compared with 76-09.** After the run, the snapshot was recomputed by hand in the same format: helm releases, namespaces, keda.sh CRDs, the external.metrics APIService, and every pod outside octoconv and keda with its restart count. It matches 76-09's SNAPSHOT_BEFORE line for line, 43 lines each (`diff` empty).
- **Namespaces.** No octoconv or keda namespace exists.
- **k8s.** `orbctl config get k8s.enable` prints true.
- **`:dev` tags.** I removed the four tags with `docker rmi octoconv-<x>:dev`, by tag name and without `-f`: api, cad-worker, webhook-worker and mcp-http. No container used them. Before and after the removal, `docker images` differs by exactly those four lines. `octoconv-api:latest`, `octoconv-cad-worker:latest` and `octoconv-av-worker:latest` are untouched.
- **Clones.** All three clones (d862c79, c160bef, 86a4d05) and the gate clone (b94fda8) are removed, and `~/.cache/octoconv/clones` is empty.

## The gate

`GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate` ran from a fresh clone at b94fda8 (evidence/76-10-ci-local.txt):

- **ALL REQUESTED TIERS PASSED.**
- internal/convert took 395.799 s.
- load1 was 5.03 at 16:29:59Z and 5.88 at 16:37:35Z.

`scripts/ci-helm-checks.sh` also exited 0 on the main tree after Task 1.

## Deviations from Plan

1. **[Rule 1] Port-forward leak (86a4d05).**
   - The script started each port-forward as `kt … kubectl port-forward &`. `$!` named the backgrounded subshell, not the perl wrapper. The teardown's `pkill -KILL -P` then killed the wrapper, and kubectl was orphaned.
   - The calibration at d862c79 left both forwards listening on 18099 and 15443. Their target namespace was already deleted, so k8s was not affected and the calibration's RESTORE diff of 0 is still true. They did keep the ports bound.
   - Full-run attempt 1 stopped on this in preflight. I killed the two leaked processes (pids 77416 and 77419, both mine) by pid.
   - The fix sends TERM to the wrapper, whose new TERM handler kills kubectl. As a fallback it TERMs this script's own exact `kubectl port-forward -n octoconv svc/… <port>` pattern, and only when the script started that forward. RESTORE now carries `local_ports_free` and fails while either port is bound.
   - A local test of the TERM path left 0 processes behind. In the official run RESTORE has local_ports_free=1, and `ps` showed no port-forward afterwards.
   - The calibration file was not re-run. Its checks do not depend on the leak, and the official run also passed through the same calibration steps.
2. **LONG_SELECT runs before the KEDA install, not after the calibration.** It is a file-only step. Running it first means a STOP costs no k8s cycle. The RULE lines still come before it and before every job, and the Task 1 verify's order check holds.
3. **Extra lines, all stricter than the plan:** RENDER_CHECK, LIVE_CHECK, LONG_CANDIDATES, LONG_SELECT_MATCH, LONG_SUBMIT, LONG_ACTIVE, SHORT_SUBMIT, BUSY_CENSUS, DOWNSCALE, ORDER_DETAIL, BUSY_WATCH, SAMPLER and QUIET.
4. **ORDER uses the Killing second conservatively.** sigterm_before_finish needs the Killing second + 1 s to fall before finished_at. annotation_before_sigterm needs the annotation to fall before the Killing second itself.
5. **quiet_host in the survival run.** The LONG row fails the run if load1 is above 11 at LONG's submit or at its finish. The plan does not ask for this; the user rule requires it.
6. **Calibration readiness** is measured from pod creation to Ready (15.0 s and 10.0 s), not from submit, so the 120 s bound applies to the pod.
7. **Observation, not a deviation: the annotation margin was thin.**
   - The HPA acted 0.26-1.26 s after the annotation.
   - The run meets every pre-registered check: spec was 2 at the annotation, the annotation came before the Killing second, and the victim was the busy pod.
   - An earlier HPA decision would have chosen the idle pod and failed the run.
   - Logged in deferred-items.md with a hardening proposal (annotate the only pod when LONG goes active). It is not applied, because a changed script would need a new run.

## NOT RUN

- The race, docker-build and e2e tiers. This plan asks only for the helm checks, and the helm and gate tiers ran. 76-11 runs the full set.
- A registry-backed cold pull inside k8s. The OrbStack image store is shared, so this was not possible (as in 76-09).

## Known Stubs

None.

## Threat Flags

None new. Each threat was handled as follows:

- **T-76-24.** The window lives only in values-loadproof.yaml. RENDER_CHECK and the verify render the chart with and without the overlay (15 and 900), and both renders keep the 906 grace. No commit of this plan touches values.yaml or values-local.yaml.
- **T-76-25.** ORDER, attempts 1 with one queued->active, Completed exit 0, and the wall inside the pre-registered band. No input was swapped.
- **T-76-26.** The busy pod is named by its own engine process and must also be the earliest pod and LONG's pod. Exactly one of two pods qualified.
- **T-76-27.** RESTORE diff 0, and the snapshot equals 76-09's. Only the four phase-76 `:dev` tags were removed, and only by name.

## Disk

- **During the runs.** VM free never went below 11.84 GiB (official HDR); the watchdog minimum was 11.84 GiB (12413688 KiB).
- **At the end.** VM free is 14.23 GiB (14917436 KiB) and the host has 19 GiB. At the start the VM had 14.9 GiB and the host 19 GiB.
- **Left in place.** Build cache from the rebuilds was not touched, and nothing was pruned.

## Self-Check: PASSED

- The overlay, the script and the four evidence files exist, and so do the three transcripts.
- The six commits from 9f40803 to 1468d8e are ancestors of HEAD. All are signed G, and none has a Co-Authored-By trailer.
- `git diff --exit-code main -- internal/convert/exec.go scripts/keda-ocr-downscale-survival.sh scripts/keda-pandoc-downscale-survival.sh scripts/keda-av-downscale-survival.sh scripts/keda-load-proof.sh scripts/keda-ocr-loadproof.sh scripts/keda-pandoc-loadproof.sh scripts/cad-image-size-measure.sh` exits 0. scripts/keda-cad-loadproof.sh is unchanged since 76-09 (8682a5f).
- Comments:
  - the new script has no comment line besides the shebang;
  - values-loadproof.yaml gains no `#` line;
  - no commit lists values.yaml, values-local.yaml or keda-cad-loadproof.sh.
