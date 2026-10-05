---
phase: 71-evaluation-gate
plan: 06
subsystem: infra
tags: [cad, rss, ladder, ocp, memory-limit, ceilings, caps, llvmpipe, occt, go-worker, tessellation]
status: complete

requires:
  - phase: 71-03
    provides: committed and fetched corpus, 33-rung size ladder, D-10 counters, private-corpus status line
  - phase: 71-05
    provides: user acceptance of VTK 9.7.1 + OSMesa; f3d as yardstick only
provides:
  - scripts/cad-rss-time-measure.sh with --ladder, --include-private, --resume, --max-wall-s, --floor-from, LADDER_STOP, DIST, NOISE, PIN_COMPARE, --threads-decision, --quality-summary
  - scripts/cad-go-worker-rss.sh (VmHWM of a live Go worker after one done job)
  - scripts/cad-derive-limits.py (floor, k_worst, size_max, cliffs, FIT, CEILING, CAP, MEMORY_LIMIT_DECISION, PIN_DECISION with PIN_ROBUSTNESS, --pin override)
  - scripts/cad-render-stacking-probe.py
  - evidence 71-06-ladder-4g-ocp7.9.3.1.1, 71-06-ladder-4g-ocp8.0.1.0.0, 71-06-ladder-2g, 71-06-ladder-1g, 71-06-threads, 71-06-quality, 71-06-go-worker-rss, 71-06-render-stacking, 71-06-limits
affects: [71-07, 71-08, 73, 75, 76]

actuals:
  tokens: 179737
  tasks: 3
  commits: 3
plan_head_before: c9715f049826a7487dc8ac1f8248568c78f4e35a
plan_head_after: ad3e7a0371d3f15cbdaa1d2458f77ae6c854be17

tech-stack:
  added: []
  patterns: [one container per cell with memory equal to memory-swap, series-wise ladder with stop-at-first-oom-or-timeout, measured pre-steps inside the cell budget, derived evidence blocks appended by post-processing modes, formula printed with substituted numbers]

key-files:
  created:
    - scripts/cad-go-worker-rss.sh
    - scripts/cad-derive-limits.py
    - scripts/cad-render-stacking-probe.py
    - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp8.0.1.0.0.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-2g.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-1g.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-threads.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-quality.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-go-worker-rss.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-render-stacking.txt
    - .planning/phases/71-evaluation-gate/evidence/71-06-limits.txt
  modified:
    - scripts/cad-rss-time-measure.sh
    - scripts/cad-measure/cad_mesh.py
    - scripts/cad-corpus-gen.py
    - internal/convert/testdata/cad/PROVENANCE.md

key-decisions:
  - "Pod memory limit for cad: 4096 MiB. The smallest L of 1/2/4 GiB at which the STEP ceiling reaches the 40 MiB yardstick: size_max is 6.6 / 28.9 / 73.6 MiB at 1 / 2 / 4 GiB"
  - "Ceilings at 4 GiB (D-07 formula with the worst file): STEP 73 MiB, IGES 137 MiB, STL binary 278 MiB, STL ASCII 1363 MiB (separate: k differs by 80%), DXF 217 MiB; every one is flagged where it lies above the largest measured rung"
  - "Cap candidates: render triangles 4266184, STEP entities 759890, NAUO 2042, DXF expanded inserts 172677 (D-07 worst multiplier; measured last fit and first OOM are printed next to each value)"
  - "Threads at --cpus 2: LP_NUM_THREADS=2, OCCT inParallel=off, by the planner's 10 percent rule"
  - "Tessellation constants stay at the STACK.md start values (0.004/0.5, 0.001/0.35, 0.0003/0.2)"
  - "OCP pin: 7.9.3.1.1 by user decision (PIN_DECISION); the formula result 8.0.1.0.0 (threshold b only) is kept as PIN_FORMULA"

requirements-completed: [EVAL-03]
---

# Phase 71 Plan 06: RSS and time to the break, limits by formula Summary

Peak RSS and wall time were measured per (file, operation, memory limit) in one container each at `--cpus 2` up to the first OOM or timeout on both OCP pins; from them the pod memory limit (4 GiB), the per-format input ceilings, four cap candidates, the thread settings and the tessellation constants were derived with the formula printed, and the Go worker RSS was measured on a live process (24.9 MiB, not the assumed 60). The OCP pin is 7.9.3.1.1 by user decision (the formula printed 8.0.1.0.0).

## Decision: OCP pin 7.9.3.1.1 (user)

The user chose **7.9.3.1.1** over the formula result **8.0.1.0.0**. `71-06-limits.txt` now has `PIN_DECISION,choice=7.9.3.1.1` (user decision) and `PIN_FORMULA,choice=8.0.1.0.0`; it was regenerated with `python3 scripts/cad-derive-limits.py EVIDENCE_DIR --pin 7.9.3.1.1` and `LIMITS-OK` still prints. Nothing else in the file changed.

User's reasoning: the 8.0 gain is not robust, 8.0 adds libjasper, libbz2 and Imath to the licence set, and it needs the `cad_mesh.py` compat shim.

| Threshold (planner's) | Value | Met |
|---|---|---|
| (a) 8.0 reads a corpus file 7.9 fails | 0 files, reverse 0 | no |
| (b) k_worst STEP mesh at least 15% lower | 7.9: 27.30, 8.0: 22.14, change 18.9% with rss_eff = max(child, cg_peak) | yes |
| (c) mesh wall sum at least 20% lower over 53 common files | 2449.7 s against 2304.3 s, 5.9% (median per-file ratio 0.931 over 24 files of 2 s or more) | no |

Why (b) was not a clean switch (`PIN_ROBUSTNESS` line):

- (b) flips with the RSS definition: 4.8% with `ru_maxrss` only, 8.8% with `memory.peak` only, 18.9% with the plan's max of the two. The difference is a baseline about 16 MiB lower in 8.0 (floor 262 against 276 MiB by `ru_maxrss`), not a lower slope.
- The slope at 40 to 200 MiB is the same: 14.81 MiB per MiB on 7.9 and 14.88 on 8.0.
- Both pins read every corpus file with the same triangle counts (a handful differ by a few triangles). 8.0 is faster on mesh by 5 to 13% on mid-size STEP and slower by 14% on one 115 MB IGES.

## Accomplishments

### Task 1: ladders (`98d444e`, gate `LADDER-OK`)

- **Script.** `--ladder` goes by ascending size inside a series (a scaled generator family, or `real`, or `private`) for each (format, operation, limit) and ends the series at the first `oom_kill` above 0, rc 137 or timeout with a `LADDER_STOP` line (`reason=oom|timeout|top`, plus `series=`, `at_bytes=`). An OOM-killed container whose wrapper dies is recognised by `docker inspect` (`OOMKilled`) and recorded as an OOM row; a missing WRAP line otherwise is retried three times and then a `FLAKE` line, never a stop. `--resume`, `--max-wall-s` (chunks of at most 6600 s, exit 75), `--floor-from` (skip rungs whose 4g RSS is under 40% of the limit being run), `DIST` (nearest rank, same as `probe-timeout-measure.sh`), `NOISE` before every file, `PRIVATE_CORPUS`, `PIN_COMPARE`.
- **Evidence size.** 4g on 7.9.3.1.1: 450 ROW, 117 DIST; 8.0.1.0.0: 117 ROW, 31 DIST, 55 PIN_COMPARE lines; 2g: 38 ROW; 1g: 57 ROW. 0 FLAKE, 0 lost wrappers, 0 unattributed rc 137 across all files. Real files ran 3 repeats at 4g, scaled files 1.
- **Reach.** STEP mesh to 209.7 MiB and render to 8.8 M triangles pass at 4g. Breaks were found at 1g and 2g for STEP, binary STL, DXF (MINSERT and replicate) and IGES; ASCII STL (200 MiB, 859 MiB RSS) does not break at 1g, so its top rung is 200 MiB (at least 100 MiB, D-05).
- **Private stream.** `OCTOCONV_CAD_PRIVATE_CORPUS` is unset: `PRIVATE_CORPUS,files=0,status=реальный поток не замерен`. Tested with two files named `SECRET-client-x.step` and `SECRET-client-y.dxf`: rows are `src=private,name=-`, no file name and no text appears in the output, log or stderr.
- **Measured ladder results at 4g (rss_eff = max(ru_maxrss, memory.peak)).**

| Series | Last passing rung | RSS | First break | Break |
|---|---|---|---|---|
| step-copies LinearSlide (mesh) | 200 MiB | 3263 MiB | none | top |
| step-copies LinearSlide (render) | 40 MiB | 633 MiB | 80 MiB | timeout, 900 s |
| step-copies freecad (mesh) | 200 MiB | 3523 MiB | none | top |
| step-copies freecad (render) | 150 MiB | 2663 MiB | 200 MiB | timeout, 900 s |
| step-nauo (render) | n=2500 | 2782 MiB | n=5000 (16.1 M triangles) | OOM |
| step-nauo (mesh) | n=10000 | 2459 MiB, 518.6 s | n=100000 | timeout |
| iges-from-step (mesh) | 274 MiB | 3131 MiB | 493 MiB | OOM |
| stl-tile (binary) | 196 MiB | 2148 MiB | none | top |
| stl-ascii | 200 MiB | 859 MiB | none | top |
| dxf-replicate | 198 MiB | 2497 MiB | none | top |
| dxf-minsert | n=100000 cells, 116.8 s | 1694 MiB | n=1000000 | timeout and OOM |

- **PIN_COMPARE** (appended to the 8.0 file) lists 55 STEP/IGES files with RSS, median wall and failure of both pins; no file fails on either pin except the 100000-instance file (timeout on both) and the 493 MiB IGES (OOM on both).

### Task 2 (`93df429`, gates `MEASURES-OK`)

- **Threads** (`--cpus 2 --memory 4g`, 3 repeats, `71-06-threads.txt`): mesh on the 9 MB real STEP and the 46 MB step-copies rung with `--parallel` on / off / affinity: wall sums 123.7 / 126.2 / 119.0 s, user+sys 146.5 / 126.1 / 139.0 s, threads 11 / 1 / 11. Everything is within 10% of the fastest, so the rule picks the lowest user+sys: `occt_parallel=off`. Render iso 1280 on a 1.04 M and a 2.2 M triangle STL with `--lp-threads` 1 / 2 / default: wall sums 9.77 / 8.63 / 8.87 s; 1 is out (13% slower), 2 beats default on user+sys (10.80 against 11.48 s) and threads (15 against 33): `lp_num_threads=2`. `THREADS_DECISION,lp_num_threads=2,occt_parallel=off`.
- **Quality** (`71-06-quality.txt`, 126 ROW): three real parts, three degenerate fixtures and the heaviest real STEP at draft / standard / fine. All 63 renders pass the pixel judge. The heaviest real STEP (9 MB, `freecad-mit-e-vent-t-slot`) gives 273208 / 436842 / 820472 triangles, mesh 18.1 / 20.0 / 28.2 s, pipeline peak 669 / 756 / 896 MiB. Fine is 19% of the render-triangle cap and the pipeline peak is 31% of the 4g budget, the 71-05 checkpoint had no remark, so the start values stay. The QUALITY lines were composed by hand from the QUALITY_ROW lines and the cap; `HDR,quality_lines=` says so.
- **Go worker** (`71-06-go-worker-rss.txt`): compose worker, PID 1 `worker`, image created 2026-10-01, concurrency 4. VmRSS idle 24.2 MiB; VmHWM after one `quota-burst-heavy.png` to webp job (status done, 8 s) 24.9 MiB. It is the image-class worker, an analogue of the future cad-worker, and the line says so. No key in the file; the probe client was revoked; services stopped, volumes `octoconv_postgres_data` and `octoconv_minio_data` kept (they did not exist before this run).

### Task 3 (`ad3e7a0`, gate `LIMITS-OK`)

`python3 scripts/cad-derive-limits.py EVIDENCE_DIR` is reproducible (a second run is byte-identical). Substituted numbers, 4 GiB:

| Format | floor MiB | Go MiB | k_worst MiB/MiB | worst file (sha prefix) | size_max 1 / 2 / 4 GiB | CEILING at 4 GiB |
|---|---|---|---|---|---|---|
| STEP | 479.0 | 24.9 | 32.115 | real 9 MB `freecad-mit-e-vent-t-slot` | 6.6 / 28.9 / 73.6 | 73 MiB |
| IGES | 272.0 | 24.9 | 18.709 | real 11 MB `freecad-festo-valve` | 22.4 / 60.8 / 137.4 | 137 MiB |
| STL binary | 481.0 | 24.9 | 8.493 | derived 196 MiB tile | 24.8 / 109.2 / 278.0 | 278 MiB |
| STL ASCII | 518.0 | 24.9 | 1.705 | derived 200 MiB | 102.0 / 522.4 / 1363.2 | 1363 MiB |
| DXF | 114.0 | 24.9 | 12.568 | derived 100 MiB replicate | 46.0 / 103.0 / 217.1 | 217 MiB |

Formula: `size_max = (0.70 * L - floor - go) / k_worst`, floor = largest pipeline peak over files under 1 MiB, k_worst = largest `(peak - floor) / MiB` over files of 1 MiB or more, counter-driven series (`step-nauo`, `dxf-minsert`) excluded and handled by the caps. STL binary and ASCII keep separate ceilings (k differs by 79.9%, threshold 25%). Each CEILING line carries the formula, the sha of the worst file, the OOM and timeout cliffs, the margin check against 0.8 and `private=реальный поток не замерен`.

- **Where a ceiling is only partly measured** (the CEILING line says `beyond_top_rung=1,measured_ceiling_mib=`): STL binary 196 MiB measured, ASCII 199, DXF 197. Their formula numbers extrapolate above the top rung at 4g without a break. STEP's top rung is 200 MiB, its ceiling 73 MiB is inside the measured range. IGES has an OOM cliff at 493 MiB (ceiling over cliff 0.28).
- **Cap candidates** (D-10, with derivation, last measured fit and first OOM on each line):

| Counter | Value | Last fit measured | First OOM | Basis |
|---|---|---|---|---|
| render triangles | 4266184 | 8.8 M | 16.1 M (n=5000 NAUO file) | worst multiplier 544.8 MiB per M triangles from the 437 k-triangle real part, cold page cache included |
| STEP entities | 759890 | 3.92 M (stacked copies) | none | worst multiplier 3110 MiB per M entities from the real 9 MB part |
| NAUO instances | 2042 | 2500 | 5000 (render) | 1.157 MiB per instance at n=1000; mesh wall about 0.05 s per instance (53 s at 1000, 121 s at 2500, 519 s at 10000) |
| DXF expanded inserts (MINSERT columns x rows) | 172677 | 100000 (116.8 s, 1694 MiB) | 1000000 | the corpus has no MINSERT, so this comes from the generated `dxf-minsert` series |

- **Memory limit decision:** `MEMORY_LIMIT_DECISION,limit_mib=4096`: at 1 GiB the STEP ceiling is 6.6 MiB (and binary STL 24.8 against a real 49.6 MiB file), at 2 GiB it is 28.9 MiB, under the 40 MiB comparison point (PreviewGenerator, printed as a yardstick only). The fleet default is 1Gi (one service 2Gi), so this is a recorded deviation from it (D-06).

## Findings the next phases need

- **Render time cliff is a stacking effect, and a real hazard.** The step-copies series replicates the DATA section at the same coordinates. The same 214128 triangles render in 61.6 s stacked and 1.2 s with each of the 36 copies shifted apart (`71-06-render-stacking.txt`), and the cost sits before the VTK render (`t_total_s` 61.2 against `t_render_s` 1.0). Non-stacked geometry is cheap: a 4.1 M-triangle tile renders in 9.4 s, a 1.04 M-triangle real STL in 4 s. The timeout cliff at 80 MiB therefore says nothing about size, but a real STEP with many coincident duplicates can reach it, and no size or triangle cap prevents it: the render step needs its own time guard (phase 76). The STEP CEILING line flags `ceiling_over_timeout_cliff=0.91` and gives `recommended_ceiling_mib_if_the_timeout_cliff_applies=64`.
- **memory.peak includes page cache.** The first repeat of a cell often shows a much larger `memory.peak` than `ru_maxrss` (for example 342 against 117 MiB on the same file). `rss_eff` takes the larger as planned, so floors and small-file multipliers are inflated and every ceiling and cap is on the conservative side. Where measured fits exceed the formula (render triangles 8.8 M against 4.27 M) both are printed.
- **IGES is about six times the size of the STEP it was written from** (115 to 977 MB from 20 to 150 MiB STEP), and reading it costs 2.1 GB at 115 MB and 3.1 GB at 287 MB.
- **Binary STL: `stlstats` is the heavy step** (32.3 s and 2.1 GB on a 196 MiB file, render 9.4 s).
- **DXF with a big MINSERT is the cheapest bomb**: 16 KB, 100000 cells, 117 s and 1.7 GB (the PDF path); 1000000 cells time out at 900 s with 3.7 GB in use.
- **NAUO** is a time problem first: mesh wall is linear in instances; memory breaks at render between 2500 and 5000 instances because the render input grows with the triangles.
- **Go worker 24.9 MiB** replaces the assumed 60 MiB; its effect on the formula is small (about 35 MiB, half a MiB on the STEP ceiling).
- **No real file is near any ceiling.** The largest real STEP is 17.4 MiB, IGES 10.8 MiB, binary STL 49.6 MiB, DXF 0.3 MiB; there is no real ASCII STL above 3 KB. Everything above those sizes is derived (stacked copies, tiles, replicated modelspace, one prototype many times) and the private stream was not measured.

## Time and noise

- **Runs (UTC, 2026-10-04, wall clock):** 4g real files 00:01 to 00:19; 4g scaled 00:20 to 02:34 (two chunks, the first ended by `--max-wall-s` at 6950 s); 8.0.1.0.0 ladder 02:46 to 03:30; DXF MINSERT at 4g 03:34 to 04:10; first pass 2g and 1g 04:15 to 07:16 (2g chunk ended at 6600 s and finished in the extension); generation of added rungs 07:17 to 07:32; extension at 4g / 2g / 1g 07:33 to 08:56; 8.0.1.0.0 on the added rungs 08:57 to 09:08; threads 09:11 to 09:32; quality 09:33 to 09:43; Go worker 09:44. About 10 h of wall clock, about 8.5 h of it measurement.
- **Host noise (`host_noise`, `NOISE`):** 12 containers of other projects ran throughout; 129 file starts in the 7.9 4g file show a neighbour at a mean of 15% of a core, 28 above 25%, 10 above 50%, peak 98.7%. For cells with 3 repeats and a median of 2 s or more the median (max - min) / median is 7.9% (worst 51.1%). Wall times of single-run rungs and the pin comparison (c) carry that noise; the 8.0 against 7.9 wall difference of 5.9% is inside it. Nothing was stopped on the host; no timing was discarded as unusable, none was fabricated.
- **Break versus flake:** no WRAP line was lost, no row has rc 137 without `oom_kill` or timeout, so no flake was recorded as a break and no break as a flake. Every OOM row has `oom_kill` of 1 (14 in 1g, 10 in 2g, 3 in 7.9 4g, 1 in 8.0 4g).
- **Disk:** free space 13 GiB at the end (14 GiB at the start); the cache grew to 4.2 GB (the IGES rungs are 1.9 GB of it). Two docker images were added (`octoconv-cad-measure:ocp7.9.3.1.1-full-32f07542eda8` and `...ocp8.0.1.0.0-full-32f07542eda8`, plus the 8.0 image at the old tag); none was deleted.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Ladders to the break at 4g, 2g, 1g on both pins | 98d444e | cad-rss-time-measure.sh, cad_mesh.py, cad-corpus-gen.py, PROVENANCE.md, four ladder evidence files |
| 2 | Threads, tessellation constants, Go worker RSS | 93df429 | cad-go-worker-rss.sh, cad-render-stacking-probe.py, cad-rss-time-measure.sh, four evidence files |
| 3 | Pod limit, ceilings, caps and pin by formula | ad3e7a0 | cad-derive-limits.py, 71-06-limits.txt |

`commits: 3` is measured with `git rev-list --count c9715f0..ad3e7a0` from the plan ledger and excludes the SUMMARY commit. All three commits are SSH-signed (`G`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `cad_mesh.py` did not run on cadquery-ocp 8.0.1.0.0**
- **Found during:** Task 1, first 8.0 run (ImportError on every STEP row; the file was discarded and rewritten).
- **Issue:** `OCP.TColStd.TColStd_SequenceOfAsciiString` does not exist in 8.0; the sequence class is `OCP.collections.Sequence_TCollection_AsciiString`.
- **Fix:** import fallback in `file_units_step` (one comment, an external API constraint). The measurement image tag changed `48d9162df359` to `32f07542eda8` and both images were rebuilt from the approved lock files. Regression: 7.9.3.1.1 on the new tag, mesh on 31 real STEP/IGES files, outputs (triangles, volume, area, units, solids) identical to the earlier rows, diffs 0 (a NOTE line heads the 8.0 evidence). The 7.9 4g file therefore holds rows from both tags (first part `48d9162df359`, DXF MINSERT and the extensions `32f07542eda8`).
- **Files modified:** scripts/cad-measure/cad_mesh.py. **Commit:** 98d444e

**2. [Rule 1 - Bug] The rsvg pre-step ran without a budget**
- **Found during:** Task 1, DXF MINSERT ladder (a 1000000-cell file would have held the host for hours).
- **Fix:** the SVG pre-step and the STL pre-step run through `measure_wrap.py` inside the cell budget at `--premesh-mem` 4g, once per file and cached; a failed pre-step is a SKIP and, for oom or timeout, a ladder stop with `via=prestep`. Render and rsvg rows therefore measure the second process only, as the formula assumes (separate processes).
- **Commit:** 98d444e

**3. [Rule 2 - Missing critical] Ladder semantics for mixed series and resumes**
- **Issue:** one ladder over all files of a format would let a pathological series (NAUO, stacked copies) end the rungs of unrelated series, and a resumed chunk treated a `top` line as a stop.
- **Fix:** stops are per (format, operation, limit, series), carry `series=`, apply only to rungs at or above the stop's size, and only `oom` and `timeout` lines count as stops on resume.
- **Commit:** 98d444e, 93df429

**4. [Rule 2 - Missing critical] Corpus rungs added so that D-05 and D-10 can be answered**
- **Issue:** the 71-03 ladder had no rung of 100 MiB or more for DXF, no IGES rung at all, no MINSERT in any file, and NAUO went from 1000 (fit) to 10000 (OOM) in one step.
- **Fix:** `cad-corpus-gen.py` kinds `dxf-minsert` (needs `PYTHONHASHSEED=0`, ezdxf writes OBJECTS in set order; checked byte-identical over 6 runs) and `iges-from-step` (timestamps normalised; two runs identical on a small file), rungs `dxf-replicate` 100 and 200 MiB, `step-nauo` 2500 and 5000. `PROVENANCE.md` has the shas, bytes and commands. Generated files are not committed.
- **Commit:** 98d444e

**5. [Rule 3 - Blocking] `.env` was not sourced in `cad-go-worker-rss.sh`**
- **Issue:** the plan sources `./.env`; the secret-read guard blocks reading it, and it is not needed: the key pepper must equal the one the API container uses.
- **Fix:** the script reads `API_KEY_SALT` from the running api container (`printenv`, never printed) and passes the dev `DATABASE_URL` of `docker-compose.yml` explicitly. The one-shot `createbucket` is run with `docker compose run --rm` instead of `up --wait`, and the script refuses to run when the stack already has containers.
- **Commit:** 93df429

### Plan items done differently or not done

- **`--repeats 3` at the rungs near the ceiling at 2g and 1g was not run.** Both runs used one repeat everywhere. OOM and timeout rows are deterministic evidence (`oom_kill`), but a rung that passes at 90% of the limit is a single sample.
- **Real files in the 2g and 1g runs** were skipped by `--floor-from` where the 4g RSS is under 40% of the limit (the LADDER_STOP lines show `floor_skipped=`); those rungs are inferred as passing, not measured at that limit.
- **Post-processing modes in the measure script** (`--pin-compare`, `--threads-decision`, `--quality-summary`) append the PIN_COMPARE, THREADS_DECISION and QUALITY_ROW blocks; the three QUALITY lines are written by hand.
- **PIN_COMPARE was regenerated once** after the 8.0 run was extended to the added STEP/IGES rungs (the earlier derived block was replaced; ROW lines are untouched).
- **Per-series `LADDER_STOP`** instead of one line per (format, operation, limit); `reason=top` lines for the `real` series are the end of the real corpus (largest real file), not a claim about size.

## Auth gates

None. All commits were signed on the first attempt.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register. T-71-15: `--include-private` rows carry `name=-`, sha and numbers; tested with named files, the name and content appear nowhere. T-71-16: the key is held in a shell variable only, passed to curl through a process-substitution header file, revoked in an EXIT trap (`key revoked` printed), the dev stack is bound to localhost and was stopped, not removed. T-71-17: every cell had `--memory` equal to `--memory-swap`, 900 s budget, chunks by `--max-wall-s`, `NOISE` in the evidence. The dev Postgres volume now holds the revoked `cad-rss-probe` client and one job row (volumes were created by this run).

## Self-Check: PASSED

- scripts/cad-rss-time-measure.sh, scripts/cad-go-worker-rss.sh, scripts/cad-derive-limits.py, scripts/cad-render-stacking-probe.py: present
- evidence 71-06-ladder-4g-ocp7.9.3.1.1, 71-06-ladder-4g-ocp8.0.1.0.0, 71-06-ladder-2g, 71-06-ladder-1g, 71-06-threads, 71-06-quality, 71-06-go-worker-rss, 71-06-render-stacking, 71-06-limits: present
- commits 98d444e, 93df429, ad3e7a0 on gsd/phase-71-evaluation-gate; ledger count 3
- gates printed `LADDER-OK`, `MEASURES-OK`, `LIMITS-OK`; `git diff --exit-code 4e595c0 -- deploy` clean; `git diff --name-only 4e595c0 -- internal cmd deploy docker-compose.yml | grep -v '^internal/convert/testdata/cad/'` empty; `internal/convert/exec.go` untouched; `grep -E '^ROW,src=private,name=[^-]'` over the evidence is empty

## Addendum: Codex review of PR #32 (P1 and P2), 2026-10-05

Two findings on the limit derivation, both closed after phase verification.

**P2: an incomplete pipeline counted as ok** (`scripts/cad-derive-limits.py`, `3a8a2d1`). A ladder point whose other operation had already stopped (the STEP render timed out at 80 MiB while the mesh went on to 150 and 200 MiB) was `ok` on its mesh cell alone and fed the FIT rung, the cliff top rung, the top-rung-from-`LADDER_STOP` calculation and the entity-cap fit. A point is now `ok` only when every operation of its pipeline was measured, otherwise `partial`. A partial point stays in `k_worst` as a lower bound (it can only raise k) and is excluded from FIT, the cliff top rung, top-rung stops (every pipeline op needs a `reason=top` line in the series) and the cap fits. `python3 scripts/cad-derive-limits.py --self-check` rebuilds the 80/150 MiB case on synthetic rows and prints `SELF-CHECK-OK`; restoring the old status rule makes it fail on three assertions. On the old evidence the fix moved only: FIT step at 4 GiB 150.2 to 79.2 MiB, CEILING step `top_rung_mib` 200.0 to 150.9, `step_entities` `last_fit_n` 3922062 to 1514122; no ceiling or cap value.

**P1: limits derived from rows of another thread configuration** (`cdca1e5`, `6107a69`). Every row of the 4g, 2g, 1g and 8.0 ladders was tagged `par=on,lp=default` while the decision selects `occt_parallel=off`, `LP_NUM_THREADS=2`; render RSS is higher at `lp=2` (STL 100 MiB 873 to 1113 MiB in `71-06-threads.txt`), so the render-driven limits were optimistic. User decision (relayed): re-run only the 4 GiB ladder for pin 7.9.3.1.1 at the selected settings; not the 2g/1g ladders and not the 8.0 pin.
- **Re-measure.** `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt`, same corpus and rungs, `--ladder --repeats 3` on committed and fetched files, `--repeats 1` on the scaled series, `--parallel off --lp-threads 2`, `--cpus 2`, 900 s cell budget, image `full` `cce9074fe380`. 448 ROW lines (the first file has 450), 0 FLAKE, every rc 137 is an `oom_kill` or a timeout, the same cells fail as before. The Mac slept during the first resume chunk and the OrbStack engine hung; the cell it hung on wrote no row and was re-run after the engine recovered, so no row spans the sleep. Host noise: 12 containers throughout, neighbour CPU at the 137 file starts mean 14.4%, peak 143.6%.
- **Derivation.** `cad-derive-limits.py` reads the selected `par`/`lp` from `THREADS_DECISION`, uses only rows (and `SKIP`/`LADDER_STOP` lines, through the file's `HDR,ladder` line) with those tags and refuses when no 4g row has them (`CONFIG` and `CONFIG_ROWS` lines show used and excluded rows). The pin formula is computed on `par=on,lp=default` rows of both pins (`PIN_CONFIG`) because 8.0.1.0.0 exists only there; `PIN_FORMULA` and `PIN_ROBUSTNESS` are byte-identical to before and `PIN_DECISION` stays 7.9.3.1.1 by the user.
- **Effect of the configuration** on the same files (`rss_eff`): mesh with `par=off` is lower (freecad copies 150.9 MiB 2908 to 2378 MiB, IGES 274 MiB 3131 to 2569) and slower on large inputs (381 s against 332 s, 157 s against 136 s; `step-nauo` mesh at 10000 instances now times out at 900 s against 518.6 s); render with `lp=2` is higher (STL tile 100 MiB 837 to 1114, 196 MiB 1527 to 1692, freecad copies 150.9 MiB 2663 to 3131, IGES 274 MiB 1370 to 1737). The 9 MB real STEP is flat (render 756 to 757 MiB).

### Before and after (4 GiB, `cpus=2`; before = `par=on,lp=default`, after = `par=off,lp=2`)

| Item | Before | After |
|---|---|---|
| Memory limit | 4096 | 4096 (unchanged) |
| STEP floor MiB / k_worst | 479.0 / 32.115 | 478.0 / 37.362 |
| STEP ceiling | 73 | 63 |
| STEP size_max 1 / 2 / 4 GiB | 6.6 / 28.9 / 73.6 | 5.7 / 24.9 / 63.3 |
| STEP ceiling over timeout cliff | 0.91, FLAG | 0.79, ok |
| STEP `top_rung_mib` / FIT at 4 GiB | 200.0 / 150.2 (with the P2 bug) | 150.9 / 79.2 |
| IGES floor / k_worst | 272.0 / 18.709 | 272.0 / 16.486 |
| IGES ceiling | 137 | 155 |
| IGES ceiling over OOM cliff | 0.28 | 0.31 |
| STL binary floor / k_worst | 481.0 / 8.493 | 480.0 / 8.498 |
| STL binary ceiling | 278 | 277 |
| STL ASCII floor / k_worst | 518.0 / 1.705 | 514.0 / 1.720 |
| STL ASCII ceiling | 1363 | 1353 |
| DXF floor / k_worst | 114.0 / 12.568 | 115.0 / 23.017 |
| DXF ceiling | 217 | 118 |
| Cap render triangles | 4266184 (544.82 MiB per M) | 4014975 (579.16 MiB per M) |
| Cap STEP entities | 759890 (last fit 3.92 M) | 754762 (last fit 1.51 M) |
| Cap NAUO instances | 2042 (1.16 MiB each) | 1518 (1.56 MiB each) |
| Cap DXF expanded inserts | 172677 | 170031 |
| STL k divergence | 79.9% | 79.8% (separate ceilings kept) |

Last fit and first OOM for triangles (8.8 M, 16.1 M), NAUO (2500, 5000) and DXF inserts (100000, 1000000) are unchanged.

### Honest flags

- **The STEP (73 to 63) and DXF (217 to 118) shifts are not caused by the thread configuration.** The worst-file rule now picks a 1.04 MiB STEP file (peak 517 against floor 478 MiB, `worst_point_governs=yes`) and a 2 MiB DXF file (peak 160 against floor 115 MiB). ezdxf and `rsvg-convert` do not depend on `par` or `lp`, so the DXF change is `memory.peak` noise (page cache) at a floor-adjacent point. The real 9 MB STEP gives k 32.3 and a ceiling near 73, the 100 MiB DXF rung gives k 12.5 and a ceiling near 218. The D-07 rule was not changed, so the more conservative 63 and 118 stand; measured fits are 150.9 MiB (STEP full pipeline) and 197.6 MiB (DXF).
- **The STEP timeout margin (0.79) passes only because the ceiling dropped**; the time guard in phase 76 is still needed (`recommended_ceiling_mib_if_the_timeout_cliff_applies=64`).
- **L stays 4096 by `L_SENSITIVITY`.** The 2 GiB limit would need STEP k_worst at most 23.267 against the measured 37.362 (1.61 times higher; even the real file's 32.3 fails); 1 GiB would need at most 5.347. The 1g and 2g ladders (`par=on,lp=default`) and the 8.0 pin ladder were not re-run; they are history and take no part in any number.
- **Not re-measured at the selected configuration:** the 71-06 threads/quality pipeline peaks (896 and 669 MiB), the 8.0.1.0.0 comparison (the pin formula is evaluated on `par=on,lp=default` rows of both pins).

### Addendum commits

`3a8a2d1` fix(71-06) P2; `cdca1e5` feat(71-06) config-filtered derivation; `6107a69` feat(71-06) re-measure evidence and regenerated limits; the `71-DECISION.md` and this addendum follow in a `docs(71)` commit.

## Addendum 2: Codex review of PR #32, P1 "Reject rows whose pixel validation failed", 2026-10-05

**Finding.** `Cell.status()` treated every `rc=0` row as ok, including a render row that records `pix=fail`. In the selected 4 GiB view `stl-ascii-nasa-sls-block1-1MiB.stl` (render, `rc=0`, `pix=fail`, peak 514 MiB) counted as an ok pipeline, became the STL ASCII floor and moved the STL ASCII ceiling to 1353 MiB. The finding is correct: with that point excluded the ceiling is 877 MiB. Fixed in `scripts/cad-derive-limits.py` (the `fix(71-06)` commit of this addendum); the class was swept, not only the named row.

### Class sweep: every ROW field that records an output verdict

Fields come from `scripts/cad-rss-time-measure.sh` (`pix`, `cstate`, `out=`) and the helpers' `out=` keys (`cad_mesh.py`, `cad_dxf.py`, `cad_render.py`). Verdict failures are counted over all five ladder files (`rc=0` rows: mesh 364, render 332, stlstats 89, dxf 91, rsvg-png 93, rsvg-pdf 91); informational rows are counted in the selected 4g `par=off,lp=2` file (the `par=on` 4g file differs by a few rows of the same files).

| ROW field | Meaning | Decision | `rc=0` rows failing it in the evidence |
|---|---|---|---|
| `pix=` (render, rsvg-png) | `pixelcheck.py single` verdict on the PNG; `fail` when the PNG is missing or `ok=0` | **required `ok`** | 3, all the same file (1g, 4g `par=on`, 4g `par=off,lp=2`) |
| `pix=` (rsvg-pdf) | `PDFCHK`: non-empty file with magic `%PDF-` | **required `ok`** | 0 |
| `cstate=` | `ok` or `wraplost-*` (WRAP line lost, row synthesised as rc 137) | **required `ok`** | 0 (1110 of 1110 are `ok`) |
| `triangles` (mesh, stlstats, render) | triangle count of the produced or read mesh | **required present and above 0** (empty output); a missing field fails, it does not pass | 0 |
| `bbox2d` (dxf) | extents of the drawn model space; `null` means nothing drawn | **required not null** | 0 |
| `svg_counts` (dxf) | written only when `out.svg` was produced | **required present** | 0 |
| `volume`, `area`, `volume_null_reason=no_solids`, `solids:0`, `closed_solids:0` (mesh) | volume of closed solids; null for surface-only and open-shell geometry | informational | 20 mesh rows per ladder file, all with triangles above 0 (`surfaces_only.iges`, `iges_no_newlines.iges`, `open_shell.step`, `single_face.step`, `freecad-mk8-hotend.iges`, `freecad-festo-valve-5-2.igs`, two derived IGES copies) |
| `unit_agree=0` (mesh) | OCP `FileUnits` differs from the unit read from the file text | informational | 3 rows, `nist_ctc_05_asme1_ap242-e1.stp` (878030 bytes) |
| `watertight`, `volume_null_reason=stl_not_watertight` (stlstats) | property of the input, which is what stlstats reports | informational | 27 of 42 stlstats rows per ladder file |
| `auditor_errors`, `skipped_types`, `svg_counts` values, `text0_decoding=latin-1-fallback`, `bbox_source=finite_faces` | diagnostics of the input, or an observation for phase 75 | informational | `auditor_errors` and `svg_counts` values are 0 on every row; `skipped_types` non-empty (3DSOLID) on 10 dxf rows; fallback decoding 6 rows; `finite_faces` 3 rows |

Why the informational ones are not verdicts: `71-04-SUMMARY.md` defines `unit_agree` as a flag "never trusted alone" (the unit is taken from the file text) and `no_solids` as the expected reason for surface-only inputs, whose meshes are valid; `71-DECISION.md` records `svg_counts` as an observation for phase 75. A `no_solids` mesh has triangles above 0 and is a real RSS measurement. Sensitivity, not applied: if `unit_agree=0` also failed a mesh cell, only `nist_ctc_05` would leave the STEP floor points (19 to 18) and the pin comparison (53 to 52 common files); no floor, k_worst, ceiling, cap or pin result changes.

**Required verdicts per operation** (a cell is `ok` only if every row has `rc=0`, `cstate=ok` and these hold):

| Operation | Required verdicts |
|---|---|
| mesh | `triangles` above 0 |
| stlstats | `triangles` above 0 |
| render | `pix=ok` and `triangles` above 0 |
| dxf | `bbox2d` not null and `svg_counts` present |
| rsvg-png | `pix=ok` |
| rsvg-pdf | `pix=ok` (PDF magic) |

A cell with `rc=0` and a failed verdict is `fail`: excluded from floors, FIT, cliffs, the top-rung calculation and the caps, and its `rss()` and `wall()` are empty. `rc=137` and timeout keep their precedence (`oom`, `timeout`). The rc=0 verdict failures are printed as `INVALID_OUTPUT` lines in `71-06-limits.txt`, so an excluded cell is visible. The selected view has exactly one.

### Why the render failed

`stl-ascii-nasa-sls-block1-1MiB.stl` fails `pix` identically in all three ladders that contain it: 1g (`par=on,lp=default`), 4g `par=on,lp=default` and 4g `par=off,lp=2`. The PIX line is the same in all three: `frac=0.0022,margin_px=124,dark=0.0005,bbox=594:124:686:1153,w=1280,h=1280,mode=part,ok=0`. It is not an empty frame and not a crash: `rc=0`, 4396 triangles, a non-empty render.

Cause: this file is the 1 MiB rung of the `stl-ascii` series built from `nasa-sls-block1.stl` (`cad-corpus-gen.py`, `scale_stl_ascii`), which writes the first 4396 facets of the base mesh and stops at the byte budget. A prefix of the facets is a thin, non-watertight strip of the rocket: the frame holds a vertical band 93 x 1030 pixels, 0.22% of the frame against `FRAC_MIN` 2%. The sliver rule did not fire (`mode=part`; it needs a projected aspect below 0.02 and `proj_aspect` is not in the evidence), so the part rule rejected a thin but real shape. `frac` grows with the prefix: 5 MiB 0.0261, 10 MiB 0.0322, 25 MiB 0.0569, 50 MiB 0.0712, 100 MiB 0.1105, 200 MiB 0.1535; the 5 MiB rung passes by 0.006. The unscaled `nasa-sls-block1.stl` has `frac=0.0791`.

This is not a defect in the renderer, but it is a finding for phase 74: the part check `FRAC_MIN=0.02` with a sliver exception only below aspect 0.02 rejects genuine thin parts (image aspect here about 0.09). A production quality gate must not reuse a frac-only criterion without an aspect or projected-extent rule, or it will reject real thin STL files (rails, plates seen edge-on). The 1 MiB slice is also a poor derived corpus point (a truncated part), another reason to keep it out of the floor.

The same row is in the old `par=on,lp=default` evidence, so the history numbers moved too (below).

### Self-check and mutation tests

`python3 scripts/cad-derive-limits.py --self-check` now builds 16 synthetic points (step, stl_ascii, dxf) and asserts: `pix=fail` (render, rsvg-png, rsvg-pdf) and `pix=-` on a pix operation, `triangles:0` and a missing `triangles` field (mesh, stlstats, render), `bbox2d:null`, a missing `svg_counts` and `cstate=wraplost-*` are each `fail`; the step (130.0), stl_ascii (80.0) and dxf (60.0) floors come from the ok points only (the failed points carry a peak of 900); `rss()` and `wall()` of a failed cell are empty; and the informational verdicts (`no_solids`, `unit_agree:0`, `stl_not_watertight`) stay `ok`.

Mutation results (scratch copies of the script, one change each):

| Mutation | Result |
|---|---|
| the old `Cell.status` (`rc == 0` only) with the new self-check | 16 failures |
| `pix` verdict removed | 9 failures (4 pix points, 3 floors, rss/wall) |
| `triangles` verdict removed | 6 failures |
| `bbox2d` verdict removed | 2 failures |
| `svg_counts` verdict removed | 2 failures |
| `cstate` verdict removed | 2 failures |
| `rss()` and `wall()` back to `rc == 0` | 1 failure |

### Before and after (4 GiB, `par=off,lp=2`, `cpus=2`)

Only the STL ASCII lines moved. `MEMORY_LIMIT_DECISION` (4096), `L_SENSITIVITY`, every FIT line, the STEP, IGES, STL binary and DXF `LIMIT` and `CEILING` lines, all four `CAP` values, `PIN_FORMULA`, `PIN_ROBUSTNESS` and `PIN_DECISION` are byte-identical.

| Item | Before | After |
|---|---|---|
| STL ASCII floor MiB (floor points) | 514.0 (2) | 272.0 (1: `stl_ascii_box.stl`) |
| STL ASCII k_worst | 1.720 | 2.930 |
| STL ASCII k_median / k_min | -5.520 / -47.601 | 1.740 / 0.800 |
| STL ASCII size_max 1 / 2 / 4 GiB | 103.4 / 520.2 / 1353.7 | 143.3 / 388.0 / 877.2 |
| **STL ASCII ceiling** | **1353** | **877** |
| STL k divergence | 79.8% | 65.5% (separate ceilings kept, threshold 25%) |
| Cap render triangles | 4014975; point 4396 `ok:514.0` | 4014975 (unchanged, `floor_render` 517.0 comes from another point); point 4396 `fail:-` |
| L, STEP / IGES / STL binary / DXF ceilings, other caps, FIT, L_SENSITIVITY | unchanged | unchanged |

The old `k_median` and `k_min` were negative because the 514 floor sat above the peaks of 272 MiB points. The 877 MiB ceiling is still above the highest measured rung (199 MiB), so it stays an extrapolation flagged in `71-DECISION.md`.

**Points that flipped:** one. `stl-ascii-nasa-sls-block1-1MiB.stl` at 4g, render: `ok` to `fail` (point status `fail`, `INVALID_OUTPUT`). Its 1g and 4g `par=on` rows are outside the selected view, so they affect only the history numbers. No other point changed status.

**History numbers at `par=on,lp=default`** (first addendum, recomputed with the same rule on `71-06-ladder-4g-ocp7.9.3.1.1.txt`): STL ASCII floor 518.0 to 272.0, k_worst 1.705 to 2.935, ceiling 1363 to 875; STEP 73, IGES 137, STL binary 278, DXF 217 unchanged. `71-DECISION.md` carries the recomputed history; the tables above in this file keep the pre-fix values as written then.

### Gates

`--self-check` prints `SELF-CHECK-OK`; `LIMITS-OK`, `DECISION-OK` and `NO-WIRING` print; the regenerated `71-06-limits.txt` is byte-identical on a rerun; `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test ./...` pass in the worktree (run before the docs commit).
