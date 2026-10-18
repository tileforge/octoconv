---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 02
subsystem: measurement
tags: [cad, bounds, stage-walls, render, mesh, dxf, rsvg, evidence, rule-amend]
status: complete

requires:
  - phase: 76-01
    provides: STACK_DECISION (step, iges, stl all not_byte_bound) and STACK_GLOBAL render_deadline_rule=remainder
  - phase: 75-dxf-2d-pipeline
    provides: scripts/cad-dxf-bounds.sh harness and the 75-05 BOUND_ROW on-record walls
  - phase: quick-261014-0lo
    provides: the fixed render judge (one verdict on both backgrounds), judge_commit 056a8d2
provides:
  - scripts/cad-dxf-bounds.sh mode bounds76 --init|--smoke|--cells-only|--group G [--rerun]|--amend-rerun [--row-log LOG]|--finish
  - scripts/cad-corpus-gen.py --exact-bytes N for step-copies, iges-from-step, stl-tile and dxf-replicate
  - evidence/76-02-bounds.txt with HDR, 26 RULE lines, 3 RULE_AMEND lines, CELL, BOUND76_ROW, REFUSAL_ROW, NOT_RUN, NOTE and SUMMARY,mode=bounds76,...,result=ok
  - evidence/76-02-iges-attempt1-rows.log and evidence/76-02-pmset-sleep-wake.txt (the host-sleep exclusion's sources)
affects: [76-03]

actuals:
  tokens: 97400
  tasks: 3
  commits: 19
plan_head_before: cc1c7a62033ea30b00d569aab5e6a712cd3d5008
plan_head_after: 26f14d6b96f488928d2da5d914b14e739f7d31c7

tech-stack:
  added: []
  patterns:
    - "A group stopped by the environment or by a product defect is re-run only under a RULE_AMEND written and committed before the re-run; the stopped rows and STOP lines stay in the file"
    - "A wall that overlaps host sleep is excluded only through a NOTE,key=contaminated_row line derived from pmset Sleep..Wake intervals and the row log, never from the wall value"

key-files:
  created:
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-02-bounds.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-02-iges-attempt1-rows.log
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-02-pmset-sleep-wake.txt
  modified:
    - scripts/cad-dxf-bounds.sh
    - scripts/cad-corpus-gen.py

key-decisions:
  - "rerun_after_product_fix: the step group, stopped at stage pixel by the render judge on M2 transparent, re-ran once at 4568488 after quick 261014-0lo; the commit differs from init 9eb0c8f only in .planning, the harness and the judge fix paths (diff_class on the HDR), same engine image id"
  - "rerun_after_host_sleep: the iges group, stopped because the Mac slept from 2026-10-14T02:37:59Z, re-ran once under caffeinate -dimsu at b242ebd with inputs identical to the stopped HDR (harness and .planning only) and the same image id; one row (M3 render standard iso opaque rep 1, 16554.18 s) is contaminated=1 and excluded from W"
  - "The host-sleep re-run had to run at a new commit, not the literal stopped-HDR commit, because the harness itself had to carry the amendment; the rule records inputs_identical_to_stopped_hdr=1 by git diff, like the watchdog amendment"
  - "--finish writes SUMMARY only; DERIVED lines (stage deadlines, io, engine, pod, bytes) are 76-03's derive76, which needs the in-worker rows"

patterns-established:
  - "NOTE,key=contaminated_row,line=N: maxwall and the 76-03 derivation skip exactly those evidence lines"

requirements-completed: []

duration: about 20 h wall (2026-10-13 15:46Z to 2026-10-14 11:40Z), including two STOPs, a quick task and a host sleep
completed: 2026-10-14
---

# Phase 76 Plan 02: Measured stage bounds Summary

**Every stage of the D-02 cells measured on the production engine image after a pre-registered rule: 416 rows, 401 setter rows, `SUMMARY,mode=bounds76,groups=refusal+dxf+stl+step+iges,...,contaminated_rows=1,result=ok`.
The 3D render walls at the STEP and IGES ceilings are far beyond 886 s: M1 renders 951-1224 s at fine and M3 hits the 1800 s wrap budget at standard in every variant.
76-03 has to resolve this under its rules, not 76-02.**

## What ran

| Group | Attempts | Result | Duration |
|---|---|---|---|
| refusal | 1 at 9eb0c8f | ok, L170031 `too_complex`, stress shapes as expected | 144 s |
| dxf | 1 at 9eb0c8f | ok, 220 rows | 6251 s |
| stl | 1 at 9eb0c8f | ok, 26 rows | 1172 s |
| step | 1: 9eb0c8f, disk watchdog STOP at 18:32Z (VM 5 GiB) | STOP | about 2185 s |
| step | 2: c17c779 (`rerun_after_watchdog`), STOP `row_failed` stage pixel on M2 transparent | STOP | 9082 s |
| step | 3: 4568488 (`rerun_after_product_fix`, judge 056a8d2) | ok | 10826 s |
| iges | 1: 4568488, host slept from 02:37:59Z, stopped by me at 08:45:58Z | STOP `host_sleep` | 24217 s (sleep included) |
| iges | 2: b242ebd (`rerun_after_host_sleep`, caffeinate pid 11766 for harness 11763, PreventSystemSleep 1) | ok, pmset re-check 0 sleeps | 9629 s |
| pathb | - | NOT_RUN `no_byte_bound_format_in_76-01` | - |

Every group HDR has `tracked_dirt=0` and `image_id=sha256:10d5b44c…3965` (`image_id_match=1` on every re-run). The engine tag inputs hash `31b85807c654` was the same in every clone.

## Per-cell maxima per stage (setter rows, contaminated row excluded)

3D (wall_s, peak MiB):

| Cell | guard | mesh fine | mesh standard | mesh draft | render (quality, slowest variant) | stats |
|---|---|---|---|---|---|---|
| M1 step-copies 66060288 B (k=67, stacked) | 0.31 | 28.99 | 39.17 | 30.95 | fine: 1223.74 (top transparent), iso opaque 1042.92 | - |
| M2 step-nauo n=1518 | 0.10 | 242.58 | 193.63 | 180.12 | draft: 9.62 (iso opaque, 2705076 triangles) | - |
| M3 iges 141557706 B (k=288, stacked) | 1.39 | 308.15 (peak 3083) | 145.76 | 117.24 | standard: timeout at the 1800 s budget in all four variants (band refused) | - |
| M4 stl 190840784 B (3816814 tri) | - | - | - | - | 21.40 (iso opaque) | 7.25 |

DXF (wall_s):

| Cell | guard | cad-dxf | rsvg_ink | rsvg_pdf | rsvg_png_transparent |
|---|---|---|---|---|---|
| C (850155 entities) | 4.19 | **170.84** | 20.32 | 27.75 | 26.17 |
| L170030 | 0.06 | 112.69 | 125.79 | 137.72 | **180.29** |
| I170031x4 | 0.05 | 79.95 | 77.34 | 92.71 | 80.91 |
| R123731968 | 3.57 | 67.06 | 6.51 | 7.82 | 6.79 |

Next to earlier figures:

| Stage | 76-02 max (setter, every band) | Earlier |
|---|---|---|
| guard | 13.43 (stress_wide, refused_by_cap); 7.11 admitted | 75-05 3.17; 73 S5 0.32 |
| mesh | 308.15 (M3 fine, load1 157.88) | 73 316 = ceil(1.5 x 210.63, 71 ladder); 73 S5 84.62 |
| render | 1800.06 budget (M3, refused); 1223.74 admitted (M1) | 74: 480 chosen, not measured; 71 ladder 40 MiB 437.36 s |
| cad-dxf | 170.84 (C rep 2, load1 27.48) | 75-05 107.35 (C); 0og 123.61 |
| rsvg | 180.29 (L170030 png transparent) | 75-05 93.46 (L170031 pdf); 0og 96.60 |

Raw `ceil(1.5 x W)` for orientation only (not DERIVED; 76-03 applies onrecord, in-worker walls, io and the render remainder rule): guard 21, mesh 463, dxf 257, rsvg 271, render 2701 over every band or 1836 over admitted rows.

## Slowest loaded row per stage

The highest load1 among setter rows: mesh M3 rep 2 at load1 157.88 (308.15 s, the mesh W), guard M3 at 48.01, render M1 rep 4 at 29.86 (948.96 s), dxf C at 27.48 (170.84 s, the dxf W), rsvg_png_transparent L170030 at 18.33 (180.29 s, the rsvg W). Every loaded row counts in W (RULE stat).

## Classification rows (setter=0)

| Cell | mesh | render iso opaque | band |
|---|---|---|---|
| S1_k256 (stl cube) | stats 0.29 | 40.85 | within |
| S1_k1024 | stats 0.26 | 900.02 | refused (wrap timeout) |
| S6_n36 (coincident NAUO) | 5.60 | 155.44 | within |
| S6_n100 | 13.73 | 900.02 | refused |
| S5_32MiB (iges copies) | 28.59 | 900.02 | refused |

`NOT_RUN,format=iges,shape=S5,role=classification_within,reason=absent_in_76-01`: 76-01 has no S5 rung within budget.

## Peaks and bytes

- Largest admitted setter stage peaks: mesh 3083 MiB (M3 fine), rsvg_pdf 2556 (L170030), render 1835 (M4), rsvg_ink 1816, dxf 1591. No OOM kill.
- **For 76-03's pod rule:** stage_peak 3083 MiB alone already exceeds `0.70 x 4096 = 2867.2` before any go_peak is added.
- DXF output bytes, admitted rows: SVG 93525876 B (L170030, so ceil(1.5x) is 134 MiB), PDF 12642299 B (I170031x4, 19 MiB). Every PNG is at most 254641 B.

## RULE_AMEND lines (verbatim keys)

1. `rerun_after_watchdog` (13 Oct, earlier session).
2. `rerun_after_product_fix` at 4568488 with `judge_commit=056a8d2…`. Diff classes against init: `planning:19+harness:1+judge_go:1+judge_test:4+judge_testdata:11+judge_cli:1+liveproof:1+docs:2`. `go_src_changed=internal/convert/cadpixel.go+scripts/cmd/cad-pixel-judge/main.go`. Committed in 69551f4 before the re-run.
3. `rerun_after_host_sleep` at 8965b93: one re-run with the same measured inputs and image as the stopped HDR, under caffeinate.
   - Rows overlapping a pmset Sleep..Wake interval are marked contaminated=1 and excluded from W; every other row counts.
   - A sleep during the re-run is STOP, with no third attempt.
   - Committed in 035e318 before the re-run.

NOTE lines:
- `prefix_judge_dxf`: 60 judge rows, all ok under the pre-fix judge.
- `prefix_judge_stl`: 9 judge rows, all ok.
- In both groups the PNGs were not retained (`retained_pngs=0`), so no offline re-judge was run.
- `fix_row_compare` (M2 transparent):
  - Same input sha256, triangles and render out_bytes (154650).
  - `old_pixel_class=fail` changed to `new_pixel_class=ok`; mode `none` became `blob`.
  - count, dark_count, box and margin are unchanged.
  - PNG bytes were not hashed in either attempt, so byte identity cannot be established. The NOTE's generic text says the count may differ under the new definition; here it did not.
- `contaminated_row line=495`:
  - M3 render standard iso opaque rep 1, row interval 02:17:41Z..08:32:36Z, overlap 5947 s, wall 16554.18 s.
  - `host_sleep_attempt`: 13 rows, 1 contaminated, 12 clean.
- `host_sleep_recheck`: 0 sleeps from 08:56:01Z to 11:36:28Z.

Host-sleep check of the other groups: pmset shows one Sleep (2026-10-13T21:34:42Z..21:36:14Z) and the 14 Oct cycle from 02:37:59Z. No step attempt overlaps either:
- step attempt 2 ended at 21:24:07Z;
- the product-fix re-run ran 23:01:35Z..02:02:00Z;
- dxf, stl, refusal and the first step attempt ran 15:48Z..18:32Z.

## Deviations from Plan

1. **[Rule 4, decided by the orchestrator] Two more RULE_AMENDs.**
   - The step group was stopped by a product defect in the render judge, fixed by quick 261014-0lo.
   - The iges group was stopped by host sleep.
   - Both re-runs were pre-registered and committed before any row. All earlier rows and the three STOP lines stay.
   - So the plan's `! grep -q '^STOP,'` check fails by construction (3 STOP lines). Every other Task 3 verify clause passes.
2. **[Rule 3] Host-sleep re-run commit.** The orchestrator asked for the same commit as the stopped HDR, but carrying the amendment needs a harness change. The re-run ran at b242ebd, whose diff against 4568488 is only `scripts/cad-dxf-bounds.sh` and `.planning`. This is recorded as `inputs_identical_to_stopped_hdr=1`.
3. **[Rule 1] Two harness bugs before the iges re-run, neither wrote an evidence line.**
   - caffeinate does not stay the parent of the program it runs, so the ancestor walk failed (fixed in fb3e5f4 by reading "asserting on behalf of").
   - awk exited early, and pmset's SIGPIPE ended the harness under pipefail with rc 141 (b242ebd).
   - A third launch was refused by the preflight because another project's `go test` was running. It waited for quiet and wrote nothing.
4. **Acceptance "every group HDR carries the init commit"** holds only through the amendments: the re-run HDRs carry c17c779, 4568488 and b242ebd with the recorded diff classes.
5. **`git diff --name-only main -- cmd internal`** lists the quick 261014-0lo judge files (internal/convert/cadpixel*.go, its tests and testdata). They are not from this plan. `internal/convert/exec.go` and 75-05-bounds.txt are byte-identical to main.
6. **GROUP_END rows=** counts every row of the group across attempts (existing harness behaviour), so step's final 129 and iges's final 32 include the earlier attempts.

## Known Stubs

None.

## Threat Flags

None. The harness gained pmset reads and a caffeinate check; no network or product surface.

## Cleanup

- Clones 4568488, 8965b93, fb3e5f4 and b242ebd removed; no worktree.
- The engine76 tag was removed by the harness on exit.
- 15 build-cache records created by the five engine builds were pruned by ID, leaving the build cache at its baseline record set (0 baseline records removed).
- No compose stack was started (bounds76 uses plain docker run), no volume was touched, OrbStack was not restarted, `go clean -cache` was not run.
- The VM had 20 GiB free and the host 26 GiB at the end.

## Self-Check: PASSED

- Files exist: `evidence/76-02-bounds.txt`, `evidence/76-02-iges-attempt1-rows.log`, `evidence/76-02-pmset-sleep-wake.txt`, `scripts/cad-dxf-bounds.sh`.
- Commits 4568488, 69551f4, 8965b93, 035e318, fb3e5f4, b242ebd and 26f14d6 are ancestors of HEAD and SSH-signed (`G`), with no Co-Authored-By trailer.
- `SUMMARY,mode=bounds76,...,result=ok` is in the evidence; no comment line in the harness after the shebang.
