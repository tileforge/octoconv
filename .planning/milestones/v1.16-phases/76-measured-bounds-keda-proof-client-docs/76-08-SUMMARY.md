---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 08
subsystem: cad-liveproof
tags: [cad, liveproof, determinism, worker-kill, guard-edge, stacked-refusal, quiet-host, d-11, d-12, d-02, d-04, ar-1]
status: complete

requires:
  - phase: 76-03
    provides: evidence/76-03-derived.txt (DERIVED, PATHB, KNOWN_LIMIT lines)
  - phase: 76-07
    provides: the cad client surfaces at 3010f3e
provides:
  - scripts/cmd/cad-png-compare (canonical JSON and the D-12 PNG tolerance, judged by the worker's own judge)
  - scripts/cad-liveproof.sh rows TIMEOUTS, RULE, DETERMINISM (3D and DXF), GUARD_EDGE, STACKED_REFUSAL, RENDER_CEILING, WORKER_KILL_RENDER, WORKER_KILL_RSVG; measured INSPECT, REJECT and DXF_CEILING; STOP_AFTER=determinism3d
  - liveproof options CAD_LIVEPROOF_IMAGE_SOURCE (proven image reuse) and CAD_LIVEPROOF_QUIET_LOAD (quiet gate), plus a per-row load sidecar
  - evidence/76-08-liveproof.txt (official, ALL PASS) and evidence/76-08-quiet.txt (quiet_host classification and coverage)
affects: [76-09, 76-10, 76-11]

actuals:
  tokens: 17636
  tasks: 3
  commits: 13
plan_head_before: 3010f3eaf1025d5fb9df56ade7439b05202be994
plan_head_after: 967d897f5fd23191b03eedc3991036a62cd0ea4c

tech-stack:
  added: []
  patterns:
    - "A PNG pair is compared by tolerance, never by hash: dims, alpha mode, the worker's JudgeCADPNG / JudgeCADDrawingPNG verdicts, bbox edges within 1 px, mean NRGBA channel difference at most 1/255, at most 0.1% of pixels over 16/255"
    - "Live expectations are read from evidence lines (DERIVED, PATHB, KNOWN_LIMIT, CELL, BOUND76_ROW); a missing or repeated key fails the run"
    - "A process census that sees nothing is not a zero: vm_census first finds the restarted container's init by its cgroup"
    - "quiet_host per row: load1 at the row's start (previous line, after the quiet gate) and end (its own line); a row above 11 is contaminated and needs a clean ALL PASS rerun"

key-files:
  created:
    - scripts/cmd/cad-png-compare/main.go
    - scripts/cmd/cad-png-compare/compare.go
    - scripts/cmd/cad-png-compare/compare_test.go
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-08-liveproof.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-08-quiet.txt
  modified:
    - scripts/cad-liveproof.sh

key-decisions:
  - "STACKED_REFUSAL follows widened_to=render_cost_not_bounded_by_input_bytes, because the stacked_render_refused line names no format, and takes each cell's options from its single refused BOUND76_ROW (quality=fine for step and iges)"
  - "The signal watcher skips zombies and processes with an empty cmdline; a just-exited ink check is a zombie whose comm still reads rsvg-convert (attempt 3)"
  - "The official evidence is attempt 5 at f44489e, the run whose HDR commit has no non-planning diff to HEAD; attempt 6 reruns its one contaminated row clean under the pre-registered RULE_AMEND rerun_contaminated_rows"
  - "Images were reused with --no-build after proving them equal to the fa12a13 clean-clone build, because the approved builder prune emptied the build cache"
  - "CADW-04 is complete. CADW-05 is not marked here: 76-09, 76-10 and 76-11 still carry it (KEDA scale-from-zero, minReplicaCount, the final-commit all-class gate)"

requirements-completed: [CADW-04]

duration: 2026-10-15T09:25Z to 2026-10-16T06:35Z, wall clock including two disk-floor stops, a docker wedge and quiet-window waits
completed: 2026-10-16
---

# Phase 76 Plan 08: the CAD live proof gets kill, determinism, cap-edge and stacked-refusal rows Summary

**The CAD live proof checks the measured bounds, judges re-runs by a pre-registered tolerance, and kills the worker mid cad-render and mid PNG raster. Official run (attempt 5): fresh clone at f44489e, production deadlines, `SUMMARY,result=ALL PASS`. Under quiet_host, all 164 rows have a clean ALL PASS run: 163 in attempt 5, and the remaining one in attempt 6.**

## Task 1: comparator and the 3D determinism tracer

RED at b30794c (evidence/76-08-red.txt). The stubs let the tests compile, so they fail on their assertions:

```
compare_test.go:78: every channel off by 1: mad 0 frac 0, want 1 0
compare_test.go:105: 10 of 10000 pixels off by 17: frac 0, want 0.001
compare_test.go:135: boxDelta({X:10 Y:20 W:100 H:50}, {X:11 Y:20 W:99 H:50}) = 0, want 1
compare_test.go:193: a frame with a part: verdict "", want ok
compare_test.go:233: canonicalJSON("") = "", want an error
compare_test.go:278: zero-byte png: exit 0, want 2
```

8 of 9 tests failed. The one that passed tests identical frames, where the stub's zeros happen to be correct.

GREEN at 8cfbe49, 9 of 9 passing. Six mutations each made the suite fail:

- `<=` changed to `<` in the mean check;
- `<=` changed to `<` in the fraction check;
- the over-16 threshold changed to `>= 16`;
- the bbox tolerance widened by 1 px;
- `UseNumber` dropped;
- the alpha-mode check disabled.

Tolerance constants: mean 1.0 in 0..255 units, fraction 0.001, per-channel threshold 16 (strictly greater counts), bbox 1 px. All comparisons are `<=`. mad_255 is the mean over all four NRGBA channels, taken literally from the plan (AR-9 forbids tightening).

Liveproof changes (f61e574):

- **Bounds from evidence.** The script reads the DERIVED values. TIMEOUTS compares six cad-worker deadlines and the api's CAD_ENGINE_TIMEOUT with them.
- **INSPECT.** Expects estimate_source=measured and estimated_max_seconds=886.
- **Ceilings.** INSPECT, REJECT and DXF_CEILING take their ceilings and limit messages from the evidence. `grep -cE '66060288|141557760|190840832|12373196[89]'` and `grep -c unmeasured` both print 0.
- **RULE line.** Written in the HDR block, before any row.

Tracer: `SUMMARY,result=PARTIAL,stopped_after=determinism3d` (evidence/76-08-tracer.txt). All four 3D rows had mad_255=0, frac_over_16=0, bbox delta 0. The Task 1 verify passed.

## Task 2: new rows (fa12a13, then e07d657, fbf007c, f44489e)

- **Watcher.** It takes an exclusion token, and both rsvg rows pass `ink.png`. Since fbf007c it skips zombies and empty cmdlines. DXF_RETRY rsvg requires the hit to be the `-f pdf` raster and records its cmdline.
- **Census.**
  - `engine_census` counts cad-guard, cad-mesh, cad-render, cad-dxf and rsvg-convert against PID 1's start time.
  - `vm_census` runs `docker run --rm --pid host --cgroupns host --network none alpine`. It returns ERR unless the restarted container's init cgroup names the container id.
- **Reconciler check.** Matches `^webhook-worker(-[0-9]+)?$`.
- **Image reuse (e07d657).** `CAD_LIVEPROOF_IMAGE_SOURCE` with two expected image ids. The script refuses unless `git diff <src> HEAD -- cmd internal Dockerfile.api Dockerfile.cad-worker go.mod go.sum deploy docker-compose.yml` is empty and the running images match. It then uses `--no-build`.
- **Quiet gate (f44489e).** `CAD_LIVEPROOF_QUIET_LOAD` waits for load1 at or below the bound before the next row, and records the wait.

## Task 3: the official live proof

| Attempt | Commit | Outcome | Evidence |
|---|---|---|---|
| 1 | fa12a13 | stopped by the 6 GiB disk watchdog at the iges stacked row | 76-08-liveproof-attempt1-killed.txt |
| 2 | e07d657 | stopped by the disk watchdog at GUARD_EDGE C | 76-08-liveproof-attempt2-killed.txt |
| 3 | 24f30a8 | **FAIL** at DXF_RETRY rsvg-convert: the job finished done with no retry (the zombie race) | 76-08-liveproof-attempt3-fail.txt |
| 4 | f4fe442 | ALL PASS; 40 rows contaminated (lines 141–180, load1 12–25) | 76-08-liveproof-attempt4.txt |
| 5 | f44489e | **ALL PASS, official**; 163 of 164 rows clean | 76-08-liveproof.txt |
| 6 | f44489e | ALL PASS; 161 of 164 clean, including DXF_EMPTY#2 | 76-08-liveproof-attempt6.txt |

Every attempt ran from a fresh clone with tracked_dirt=0, production deadlines, the 6 GiB watchdog and `caffeinate -dimsu`. Attempt 1 built its images. Attempts 2–6 reused them. Every transcript is kept whole. Attempt 4 was renamed with its content unedited; 967d897 records that each byte matches f44489e.

HDR of the official run:

- `commit=f44489ea30a172bc43af8898736989d70c932dbb`, `tracked_dirt=0`;
- api `sha256:f073c7e160f5…`, cad-worker `sha256:f50534c1fcc8…`;
- `build=no_build_reused_images,image_source=fa12a13…,image_input_diff_files=0,running_images_match=1`;
- `quiet_gate_load1=11`;
- engine 886, render 521, dxf 186, rsvg 217;
- start 03:21:21Z (load1 9.18), end 04:11:45Z (load1 6.47).

Task 3 verify on evidence/76-08-liveproof.txt: **TASK3_VERIFY_PASS**. Acceptance checks:

- the HDR commit is an ancestor of HEAD, with no non-planning diff;
- the file has no `FAIL` lines;
- the override is not tracked;
- all 13 plan commits are signed G.

### New rows (official run)

- TIMEOUTS: `guard=21s,mesh=324s,render=521s,dxf=186s,rsvg=217s,engine=886s,api_engine=886s,result=ok`.
- **DETERMINISM, 6 rows.** Every row has json_equal=1, bbox_max_delta_px=0, mad_255=0.000000, frac_over_16=0.000000 and within=1. The re-runs are pixel-identical.

  | Class | View | Background | bbox |
  |---|---|---|---|
  | 3d | iso | opaque | 134:250:1068:764 |
  | 3d | top | opaque | 357:76:566:1127 |
  | 3d | iso | transparent | 134:250:1068:764 |
  | 3d | top | transparent | 357:76:566:1127 |
  | dxf (uncommon.dxf) | none | opaque | 78:158:1123:963 |
  | dxf (uncommon.dxf) | none | transparent | 78:158:1123:963 |

- **GUARD_EDGE.**
  - C: 850155 entities, done in 122.7 s against the 186 s dxf deadline, one svg output of 87015742 bytes, svg_check ok.
  - L170031: 850156 entities, failed cad_too_complex, retried=0, outputs=0.
- **STACKED_REFUSAL.** Selected by `KNOWN_LIMIT,key=stacked_render_refused,widened_to=render_cost_not_bounded_by_input_bytes` and its three per-format lines. Each row ended failed cad_too_complex with attempts=1, retried=0, outputs=0 and census 0/0. Each attempt wall is at least 521 s and below 886 s:

  | Format | Cell | Options | Attempt wall |
  |---|---|---|---|
  | step | S6_n100 | fine, iso, opaque, 2048 | 533.644 s |
  | iges | S5_32MiB | fine, iso, opaque, 2048 | 546.251 s |
  | stl | S1_k1024 | iso, opaque, 2048 | 521.223 s |

- **RENDER_CEILING.** `NOT_APPLICABLE,row=RENDER_CEILING,reason=no_pathb_fired`. It quotes the four PATHB lines, all fired=0. It counts 0 and is not a pass.
- **DXF_RETRY rsvg.** `signalled_cmdline=/usr/bin/rsvg-convert -f pdf -b white -o …/out.pdf …/clean.svg`, retried=1, done.
- **Worker kills.**

  | Row | Result |
  |---|---|
  | WORKER_KILL | orphans=0, kill_to_done 117.7 s |
  | WORKER_KILL_RENDER | killed cad-render pid 413, compose_up, orphans=0, vm_orphans=0 (195 VM procs seen), kill_to_done 121.9 s, done with 2 outputs |
  | WORKER_KILL_RSVG | killed the out.png raster pid 1899, ink_check=0, orphans=0, vm_orphans=0, kill_to_done 144.6 s, done with 2 outputs, drawing judge ok |

### quiet_host (evidence/76-08-quiet.txt)

`RULE_AMEND,key=rerun_contaminated_rows` was written before attempt 5. A row is contaminated when load1 is above 11 at its start or at its end. A contaminated row is never counted clean. It can be cleared only by a later full ALL PASS run from a fresh clone on the same images.

`QUIET_COVERAGE,rows=164,uncovered=0,clean_attempt5=163,clean_attempt6=161,clean_in_attempt5_or_attempt6=164`.

- In attempt 5 the only contaminated row is DXF_EMPTY#2 (alpha_clear_geometry.dxf to pdf): load1 10.14 at start, 13.01 at end. It is clean in attempt 6 (6.27 and 6.09).
- Attempt 6's three contaminated rows (DXF_MEAS#1, DXF_RETRY#2, WORKER_KILL#1) are clean in attempt 5.

### Watcher fix proof (evidence/76-08-watcher-fixproof.txt)

- In the cad-worker image, with `sleep 0.2 & exec sleep 6`, the old watcher hits the zombie (pid 8, empty cmdline). The new one does not hit it, and it still hits the live `sleep 6`.
- Live on the reused images, five TERM hits by the new watcher were all the `-f pdf` raster, and every job retried to done.
- A signal-0 hit on a 2048 PNG render was the `-o out.png` raster.

## Gate

`scripts/ci-local.sh helm gate` from a fresh clone at fa12a13 (evidence/76-08-ci-local.txt): ALL REQUESTED TIERS PASSED. internal/convert took 402.288 s, under 540. Load1 was 7.21 at start and 4.85 at the end.

Every commit after fa12a13 touches only scripts/cad-liveproof.sh or .planning. The script passes `bash -n` and shellcheck shows no new warnings; the six warnings it prints are also on main.

## Deviations from Plan

1. **[Rule 1] STACKED_REFUSAL source lines and options.** The stacked_render_refused line names no format, so the row follows `widened_to`. The refused measurements used quality=fine for step and iges, so the options come from the cell's refused BOUND76_ROW. Cell files are read from `$SCALED/b76`, the CELL `path`, with size and sha256 checked.
2. **[Rule 1] Watcher zombie race (fbf007c).** Found by attempt 3's FAIL and fixed before any pass was counted.
3. **[Rule 2] GUARD_EDGE assertions.** The row asserts the CELL guard verdicts and that L170031 has one entity more than C. It does not hard-code 850155.
4. **[Rule 2] REJECT padded STEP.** It also uses the derived ceiling. Limit messages are compared exactly against the formatLimitMessage rendering.
5. **[Rule 3] Image reuse and quiet gate.** Added after the approved builder prune emptied the build cache, and to meet quiet_host. Both are proven inside each run's HDR.
6. **Official file name.** The official file is attempt 5, not attempt 4: attempt 4 predates the quiet-gate commit, and its HDR commit would fail the plan's `C..HEAD` acceptance check.
7. **RENDER_CEILING fired branch.** For stl there is no convert pair, so the branch records `none_offered`. The branch is unreachable while every PATHB line is fired=0.
8. **TDD.** The RED commit uses stubs so the tests fail on assertions, not on missing symbols.
9. **Smoke runs.** Smoke runs on the main tree (extracted helpers, the 18090 override) were added. Their MinIO objects were deleted.

## Open items

- **RESOLVED 2026-10-16T13:46Z: MinIO cleanup for attempts 5 and 6, and `docker compose -p octoconv stop`.**
  - Docker recovered by itself.
  - The interrupted cleanup completed at 08:42:44Z. It deleted 604 objects (1829130978 bytes) and took the bucket from 4.8 GiB / 1918 objects to the 3.1 GiB / 1314-object baseline.
  - A scoped verification at 13:45Z covered the two clients created after the last cleanup (attempts 5 and 6, 153 jobs each, no other clients). It found 0 objects left, and a direct probe of an attempt-5 results prefix returned 0 objects.
  - The per-object rows of the 08:42 run were lost: the verification run wrote the same scratch output file. Only that run's header line survives.
  - `docker compose -p octoconv stop` ran with rc 0 and left nothing running. VM free 23.56 GiB, host 28 GiB.
  - All of this is recorded in evidence/76-08-minio-cleanup.txt.
- **Originally:** these waited for Docker to recover. The docker daemon stopped responding at about 05:27Z on 2026-10-16, during the cleanup after attempt 6: `docker compose exec … mc du`, `docker compose stop`, `docker ps` and `docker version` all hung. The hung clients have since exited. No OrbStack restart was done; it is not approved.
  - After recovery, run `scratchpad/76-08-minio-cleanup.sh` (it deletes only the uploads and results of the 76-08 clients), then `docker compose -p octoconv stop`, and append the result to evidence/76-08-minio-cleanup.txt.
  - Earlier cleanups returned the bucket to its 3.1 GiB / 1314-object baseline each time.
- **CADW-05 stays open.** 76-09 (KEDA 0->2->0 and minReplicaCount), 76-10 and 76-11 (the final-commit all-class gate) still carry it. This plan delivered its cad-liveproof share.

## NOT RUN

- The RENDER_CEILING fired branch: no PATHB line has fired=1.
- The race tier and the docker-build tier were not requested. The gate ran helm and gate only.

## Known Stubs

None.

## Threat Flags

None. T-76-15 is mitigated by engine_census and the VM-wide cgroup-checked census. T-76-16 by the PARTIAL and NOT_APPLICABLE lines and the quiet_host coverage. T-76-17 by TIMEOUTS and the ports-only, untracked override. T-76-18 by the CELL size and sha256 checks.

## Self-Check: PASSED

- Files exist: scripts/cmd/cad-png-compare/{main,compare,compare_test}.go, evidence/76-08-liveproof.txt, 76-08-quiet.txt, 76-08-tracer.txt, 76-08-red.txt, 76-08-ci-local.txt.
- 13 commits from 3010f3e to 967d897 are in HEAD. All are signed G, and none has a Co-Authored-By trailer.
- `git diff --exit-code main -- internal/convert/exec.go` exits 0. No commit of this plan touches cmd/ or internal/.
