---
phase: quick-261007-1sx
plan: 01
subsystem: cad-measurements
status: complete
tags: [cad, stl, step, iges, measurements, codex-p2, pr-35]
requires: [ac81afe]
provides:
  - stl_ambiguous_shells null reason (stl only), documented and test-reconciled
  - per-shell signed STL volume (shells = triangles joined through shared edges)
  - per-solid negative_volume for STEP/IGES solid_summary
affects: [internal/measurements, cmd/cad-worker/helper/cad-mesh, docs/MEASUREMENTS.md, scripts/cad-stl-case.sh, scripts/cad-liveproof.sh]
tech-stack:
  added: []
  patterns: [vectorized min-hook union-find with pointer jumping in numpy, twin-edge pairing by argsort of directed edge keys]
key-files:
  created: []
  modified:
    - internal/measurements/measurements.go
    - internal/measurements/measurements_test.go
    - internal/measurements/consistency_test.go
    - internal/convert/cadmeasure_test.go
    - docs/MEASUREMENTS.md
    - cmd/cad-worker/helper/cad-mesh
    - scripts/cad-stl-case.sh
    - scripts/cad-liveproof.sh
    - .planning/phases/74-3d-render-measurements/evidence/74-08-stl-case.txt
    - .planning/phases/74-3d-render-measurements/evidence/74-08-liveproof.txt
decisions:
  - STL shells are edge-connected components (approved refinement of locked rule 1); vertex-touching shells stay separate
  - stats_edges keeps only pairs with head < tail, one pair per undirected edge
  - per-triangle values are kept as determinants and divided by six once per shell, so integer meshes stay exact
completed: 2026-10-07
duration: ~75 min
commits: 6
plan_head_before: ac81afe19ff57103ca070c011950c3a0856b4f06
plan_head_after: 8dc219e5076bc115566d95d5d8e5a10f886c19aa
actuals:
  tokens: 22185
  tasks: 3
  commits: 6
---

# Quick 261007-1sx: per-shell signed volume for STL, per-solid sign for STEP/IGES Summary

A watertight STL's volume is now summed per edge-connected shell. Mixed-sign shells give the new stl-only null reason `stl_ambiguous_shells`, and every shell negative gives `negative_volume`. Any shell of exactly 0 volume gives `invalid_solid`. For STEP/IGES, one closed solid with negative mass makes the document `negative_volume`. This closes Codex P2 round 2 on PR #35 for the whole class: no path publishes a sum across volumes of opposite sign.

## Commits

| # | Hash | Subject |
|---|---|---|
| 1 | a4b877c | feat(quick-261007-1sx): stl_ambiguous_shells null reason |
| 2 | 334316b | fix(quick-261007-1sx): per-shell signed volume for STL, per-solid sign for STEP and IGES |
| 3 | 084e7ff | fix(quick-261007-1sx): sum shell determinants before dividing by six |
| 4 | 95d9750 | test(quick-261007-1sx): shells differential, step sign probe, one-shell stats rss, hollow stl liveproof row |
| 5 | 836acbc | test(quick-261007-1sx): stl case differential ALL PASS on the fixed commit |
| 6 | 8dc219e | test(quick-261007-1sx): cad liveproof ALL PASS on the fixed commit |

Every commit is signed and has no attribution trailer. Image `octoconv-cad-worker:latest` = `sha256:99952ab3c0ad6f337465b99b1b9adbde891e9a9ac555e7fcb08e613edf172462`, built by cad-stl-case.sh from 95d9750. cad-mesh in the image has sha256 3b8d1929..., which matches the tree (`image_equals_tree=1`).

## Evidence

### 74-08-stl-case.txt (commit 95d9750, tracked_dirt 0)
`SUMMARY,result=ALL PASS,sniff=10,baseline=9,case=9,nonfinite=1,shells=9,union=1,step_sign=4,stats_rss=3`

The SHELLS rows run the real helpers inside the image. The "old" column is the ac81afe helper, and it is the teeth: on three fixtures it published numbers.

| fixture | tri | new (fixed) | old (ac81afe) | shells pos/neg/zero | rounds |
|---|---|---|---|---|---|
| cube_out | 12 | 1000.0/600.0 | 1000.0/600.0 | 1: 1/0/0 | 2 |
| two_out | 24 | 2000.0/1200.0 | 2000.0/1200.0 | 2: 2/0/0 | 2 |
| cube_in | 12 | negative_volume | negative_volume | 1: 0/1/0 | 2 |
| two_in | 24 | negative_volume | negative_volume | 2: 0/2/0 | 2 |
| out_in_disjoint | 24 | stl_ambiguous_shells | **875.0/750.0** | 2: 1/1/0 | 2 |
| hollow | 24 | stl_ambiguous_shells | **936.0/696.0** | 2: 1/1/0 | 2 |
| corner_touch_mixed | 24 | stl_ambiguous_shells | **875.0/750.0** | 2: 1/1/0 | 2 |
| corner_touch_out | 24 | 1125.0/750.0 | 1125.0/750.0 | 2: 2/0/0 | 2 |
| zero_shell | 14 | invalid_solid | **1000.0/700.0** | 2: 1/0/1 | 2 |

UNION: on a randomly labelled 3,816,804-node cycle (seed 74), roots=1 after **14 rounds**, within the bound of 46 (2*ceil(log2 n)+2). It took 2.08 s.

STEP_SIGN, compound of box 10 plus box 5 at x=20:
- memory, reversed_solid: new `2:2:None:None:negative_volume`; old `2:2:874.9999999999998:749.9999999999999` published. branch_reached=1.
- memory, inward_shell: new `2:2:None:None:negative_volume`; old published 874.99... branch_reached=1.
- file, reversed_solid: `repaired`. OCP's STEP reader fixes the orientation, so both helpers give 2:2:1124.9999999999998:749.99...
- file, inward_shell: `repaired`, with the same numbers. OCP repairs both file variants on read, so the negative-solid branch can only be reached in memory (or from an IGES/STEP file that OCP does not repair).

STATS_RSS, next to t51 (b14becc):

| kind | triangles | shells exp/counted | union rounds | wall s | child RSS MiB | cgroup peak MiB | t51 wall / child / cgroup |
|---|---|---|---|---|---|---|---|
| binary | 3,816,804 | 318,067 / 318,067 | 2 | 5.71 | 612 | 789 | 3.37 s / 582 / 759 |
| ascii_upper | 1,804,224 | 150,352 / 150,352 | 2 | 7.04 | 525 | 685 | 5.57 s / 525 / 681 |
| binary_one_shell (N=563, seed 74, 190,181,484 bytes, sha256 7cb162bb...) | 3,803,628 | 1 / 1 | 8 | 11.26 | 610 | 786 | n/a (new) |

All three rows are well inside the share gates of 2007.04 MiB (70% of 2867.2) and 221.2 s (70% of the 316 s mesh timeout). oom_kill is 0 everywhere. Draft volume and area equal the expectations exactly: 1,908,402,000 / 699,747,400; 902,112,000 / 330,774,400; 178,453,547 / 1,901,814.

### 74-08-liveproof.txt (commit 836acbc, tracked_dirt 0)
`SUMMARY,result=ALL PASS,formats=1,inspect=5,convert=5,quality=1,render=5,views=6,meas=10,opts_reject=9,reject=2,bomb=12,render_retry=1,mesh_signal=1,empty=1,cancel_render=1,cancel=1,worker_kill=1,image=1`

- `MEAS,name=stl_hollow_ambiguous`: extents 10:10:10, volume null, area null, reason stl_ambiguous_shells, valid=1, result=ok. This runs under the confined production worker.
- The three stl_case rows still give 6000/2200.

Both transcripts come from commits whose cmd/internal/scripts/docs/Dockerfile.cad-worker/docker-compose.yml tree equals HEAD.

### Gates on HEAD 95d9750 (before the evidence commits)
- `GOTOOLCHAIN=go1.26.4` go build, go vet ./... and gofmt -l . are clean. `go test ./... -count=1` passes in every package.
- `scripts/conf05-crosscheck.sh cad`: rc 0. MATCH on cadguard, cadmesh, cadrender and cad multiarch.
- `git diff --exit-code ac81afe -- internal/convert/exec.go internal/convert/cadsandbox.go internal/convert/sandboxwrap.go Dockerfile.cad-worker` is clean, and 74-06-sandbox-liveproof.txt is untouched.
- `scripts/ci-local.sh gate race`: PASS gate, PASS race. Transcript at `.planning/ci-local/ci-local-20261006T225301Z.log`.

### VM free disk
| point | free |
|---|---|
| start of Task 3 | 5.0 GiB (planning: 5.1) |
| after `docker builder prune -f` | 11.7 GiB |
| stl-case run | start 9.6, minimum 7.9 GiB |
| cad-liveproof run | minimum 6.9 GiB (at startup) |
| end, after removing the untagged prior cad-worker image fd614b7c | 7.0 GiB |

Port 5434: I stopped `log-format-postgres-1` right before cad-liveproof.sh and started it again right after. It is back to `Up (healthy)`. octoconv-db did not need a reattach.

## Design notes

- **Edge-adjacency refinement of locked rule 1 (approved):** shells are the components of the triangle graph, with two triangles adjacent when they share a directed edge and its reverse (stats_index dedup ids). The `corner_touch_mixed` row is the evidence: an outward 10-cube and an inverted 5-cube that share only the vertex (10,10,10) count as 2 shells, 1/1 → `stl_ambiguous_shells`. Under vertex adjacency they would merge into one shell of +875, and ac81afe publishes 875.0 for them.
- **Half pairs (Task 1 step 9):** stats_edges keeps only the pairs whose sorted forward key has head < tail. That is exactly one (a, b) pair per undirected edge, which halves the union-find arrays and the gather work.
- Union-find is vectorized: gather parent[a] and parent[b], hook with np.minimum.at, then pointer-jump to stars. The only Python loops run over chunks and union rounds. No scipy or VTK, and nothing was installed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Per-triangle det/6 lost exactness**
- **Found during:** Task 2 fixture check, after commit 334316b
- **Issue:** plan step 8 stored each triangle's volume as det/6. Summing those values per shell gave 999.9999999999999 for a 10-cube, 64.00000000000001 for the 4-cube and 125.00000000000001 for the 5-cube. The pre-fix helper summed integer determinants first and gave 1000.0 exactly.
- **Fix:** stats_mass stores determinants, and stats_shells divides each shell sum by 6 once. Signs and classification are unchanged.
- **Files modified:** cmd/cad-worker/helper/cad-mesh
- **Commit:** 084e7ff

**2. [Rule 3 - Blocking] TestVocabularyCounts asserted 6 null reasons**
- **Found during:** Task 1 commit A green step
- **Fix:** the count changed to 7 in the same vocabulary commit. The plan listed only TestVocabularyExactMembers.
- **Commit:** a4b877c

**3. [Rule 3 - Blocking] Evidence committed in two commits, not one**
- **Found during:** Task 3 step 5
- **Issue:** cad-liveproof.sh counts every modified tracked file except its own --out as tracked dirt. An uncommitted 74-08-stl-case.txt would have made its header `tracked_dirt=1`, and the plan requires 0.
- **Fix:** I committed 74-08-stl-case.txt first (836acbc), then ran the liveproof on 836acbc and committed its transcript (8dc219e). Both commits have the same product tree as HEAD.

**4. Cleanup:** I removed the untagged, unreferenced octoconv-labelled image fd614b7c (the previous cad-worker:latest, which my rebuild untagged). It was not another project's image, and no volumes were pruned. Other dangling images, including older octoconv ones, were left alone.

No stubs, no TODOs, and no code comments were added. The helper's shebang is still its only `#` line, and neither script has comment lines.

## Threat Flags

None. No new network, auth or file-access surface. The sandbox sets are unchanged, and conf05 MATCHes.

## Self-Check: PASSED

- All 10 key files exist. All 6 commits are ancestors of HEAD: a4b877c, 334316b, 084e7ff, 95d9750, 836acbc, 8dc219e.
- The Task 1, Task 2 and Task 3 automated verify blocks all pass (Task 3 without the SUMMARY-exists clause, which this file satisfies).
