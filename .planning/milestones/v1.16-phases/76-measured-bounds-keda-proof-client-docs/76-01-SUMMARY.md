---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 01
subsystem: measurement
tags: [cad, stacking-probe, render-cost, vtk, ocp, evidence, path-b]

requires:
  - phase: 75-dxf-2d-pipeline
    provides: scripts/cad-dxf-bounds.sh harness (build_engine, wrapped, measure_wrap, disk_guard, RULE-before-row protocol)
  - phase: 74-3d-render-measurements
    provides: production cad-render/cad-mesh stats stages, CADMaxRenderTriangles cap
provides:
  - scripts/cad-corpus-gen.py kind stl-stack (--part cube | --scaled-from STL) and step-nauo --nauo-pitch
  - scripts/cad-dxf-bounds.sh mode stack [--smoke] [--out FILE] on the production engine image
  - evidence/76-01-stack.txt with three STACK_DECISION lines and STACK_GLOBAL render_deadline_rule=remainder
affects: [76-02, 76-03, 76-05, 76-06, 76-08]

actuals:
  tokens: 19290
  tasks: 2
  commits: 2
plan_head_before: 3894ece5fe81c41d88564b399fbf1bd4d025baf1
plan_head_after: 1af1995529f0848f90cca32d7df6a5a70bc20481

tech-stack:
  added: []
  patterns:
    - "Premise probe before the N=5 run: pre-registered RULE lines, one rung per ladder step, mechanical per-format decision lines that later plans parse"
    - "A guard refusal (cad-guard rc 4 too_complex) is band=refused_by_cap, the production outcome, never a STOP and never a cost row"

key-files:
  created:
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt
  modified:
    - scripts/cad-corpus-gen.py
    - scripts/cad-dxf-bounds.sh

key-decisions:
  - "Bytes do not bound 3D render cost: step, iges and stl are all not_byte_bound; the AR-1 remainder branch is live (render deadline R = 886 - guard - mesh - io), no format qualifies for a Path-B byte render ceiling"
  - "S6 (coincident NAUO, --nauo-pitch 0) was added before any row because the S3 step-nauo grid places instances 200 mm apart and does not stack them; S6 is the shape that broke STEP"
  - "STEP MAPPED_ITEM instancing is counted as probed by S6 (same 1518 expanded-instance guard cap, same coincident geometry): an assumption written as a RULE line before any row"

patterns-established:
  - "stack mode: STACK_ROW per stage, NOT_RUN reason=over_budget_below for every rung above the first over rung, RUNG_SKIP for rungs outside a ceiling, RUNG_PICK for generator-chosen rungs"

requirements-completed: []

coverage:
  - id: D1
    description: "stl-stack generator kind and step-nauo pitch option, existing kinds byte-identical"
    requirement: CADW-04
    verification:
      - kind: automated_ui
        ref: "scripts/cad-dxf-bounds.sh stack --smoke (GEN_IDENTITY,kind=stl-tile,...,equal=1)"
        status: pass
    human_judgment: false
  - id: D2
    description: "stack mode of scripts/cad-dxf-bounds.sh on the production engine image with confined stages and pre-registered rules"
    requirement: CADW-04
    verification:
      - kind: other
        ref: "Task 1 <verify> command (smoke file: SUMMARY result=ok, S1 render rc=0, 11 RULE lines before the first STACK_ROW)"
        status: pass
    human_judgment: false
  - id: D3
    description: "pre-registered stacking probe from a fresh clone, per-format decisions and the global render-deadline rule"
    requirement: CADW-04
    verification:
      - kind: other
        ref: "Task 2 <verify> command on evidence/76-01-stack.txt"
        status: pass
    human_judgment: false

duration: 94min
completed: 2026-10-13
status: complete
---

# Phase 76 Plan 01: Stacking Probe Summary

**Bytes do not bound 3D render cost. A 614,484-byte STL (1,024 stacked cubes, 12,288 triangles) and a 598,732-byte STEP (100 coincident NAUO instances) each hit the 900 s wrap budget on the production engine image, so all three 3D formats are `not_byte_bound` and the render deadline is the window remainder.**

## Performance

- **Duration:** about 94 min. The probe itself ran 57 min, 14:13:30 to 15:10:25 UTC.
- **Started:** 2026-10-13T13:40:49Z
- **Completed:** 2026-10-13T15:15Z
- **Tasks:** 2 of 2
- **Files modified:** 3 (2 scripts, 1 evidence file)

## Accomplishments

- `scripts/cad-corpus-gen.py` has two additions:
  - kind `stl-stack`: K coincident copies of the 12-triangle unit cube (`--part cube`) or of a binary STL (`--scaled-from`). A `SCALED_SKIP ... reason=over_stl_ceiling` line is printed when `84 + 50 x T > 190840832`.
  - option `step-nauo --nauo-pitch`: the default of 200 leaves every existing output unchanged. `stl-tile` output was checked against the main-branch generator inside the engine image (`GEN_IDENTITY ... equal=1`).
- `scripts/cad-dxf-bounds.sh stack [--smoke] [--out FILE]` runs every rung on the production `engine` stage, image tag `engine76-31b85807c654`, id `sha256:10d5b44c…`.
  - Each stage runs in its own container under `--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody` with a 900 s `measure_wrap.py` budget.
  - `cad-mesh` and `cad-render` are confined by `sandbox-probe` using the production read-only sets plus `/usr/lib/aarch64-linux-gnu` and the helper. `LP_NUM_THREADS=2` is set on every stage, as `cadStage` does.
  - Before the engine build, a preflight refuses to start if an octoconv compose container, an octoconv k8s pod, a `go test` or a docker build is running.
- `evidence/76-01-stack.txt` was produced from a fresh clone at `7570956` with tracked_dirt=0. It holds:
  - 11 RULE lines, written before any row;
  - 3 FLOOR lines;
  - 48 STACK_ROW lines over 20 rungs;
  - 12 NOT_RUN lines;
  - 3 STACK_DECISION lines, 1 STACK_GLOBAL line and `SUMMARY,mode=stack,...,result=ok`.

## Decision table (evidence/76-01-stack.txt)

| format | decision | break shape | break bytes | floor bytes (file) | over shapes | reason |
|---|---|---|---|---|---|---|
| step | not_byte_bound | S6 (coincident NAUO) | 598,732 (n=100) | 878,030 (nist_ctc_05_asme1_ap242-e1.stp) | S6 | another mechanism than copies is over; S4 copies all within or guard-refused |
| iges | not_byte_bound | S5 (IGES of stacked copies) | 32,905,602 | 11,321,208 (freecad-festo-valve-5-2.igs) | S5 | 308/408 subfigure instancing unprobed, never byte_bound by rule |
| stl | not_byte_bound | S1 (stacked cube) | 614,484 (K=1024) | 52,041,984 (nasa-pillars-of-creation-full.stl) | S1 | break is 85x below the floor |

`STACK_GLOBAL,render_deadline_rule=remainder,not_byte_bound_with_over=step+iges+stl,byte_bound=none,within_budget=none`

**The remainder branch of AR-1 is now in force:**
- 76-02 and 76-03 compute the render deadline as `R = 886 - guard - mesh - io` from measured guard, mesh and I/O.
- Every non-stacked render cell must fit `ceil(1.5W) <= R`. Stacked rows are classified margin, no_margin or refused against R.
- 76-03 writes `KNOWN_LIMIT stacked_render_refused` for step, iges and stl.
- 76-05 adds no operation-aware byte render ceiling for any format, because none is `byte_bound`.
- 76-08 proves the STACKED_REFUSAL row live.

## Every row over budget (no row dropped, none excused)

| shape | rung | bytes | triangles | stage | wall_s | result |
|---|---|---|---|---|---|---|
| S1 | k1024 | 614,484 | 12,288 | render | 900.01 | wrap timeout (rc 137, timeout=1), band=over |
| S5 | 32MiB | 32,905,602 | 544,308 | render | 387.56 | rc 0, over B_render 320 s |
| S6 | n100 | 598,732 | 594,800 | render | 900.03 | wrap timeout (rc 137, timeout=1), band=over |

The rungs above each of these are NOT_RUN `reason=over_budget_below` and were never counted:
- S1: k4096 … k318067;
- S5: 64MiB, 96MiB, max;
- S6: n300 … n1518.

## Walls against the 71 figures

| what | 71 (evaluation image) | 76-01 (production engine image) |
|---|---|---|
| 36 coincident LinearSlide copies, 214,128 triangles, render | 61.57 s stacked, 1.18 s spread (71-06-render-stacking) | 70.40 s (S6 n36, NAUO); 68.22 s (S4 20MiB, step-copies) |
| step-copies 10 MiB render | 9.40 s | 11.03 s |
| step-copies 20 MiB render | 61.26 s | 68.22 s |
| step-copies 40 MiB render | 437.36 s | not run: the production guard refuses the file (`entities_over_cap`, 809,136 > 754,762) |
| step-copies 32 MiB render (58 copies) | — | 261.26 s (within 320, but ceil(1.5 x 261.26) = 392 > 320) |
| 100 coincident NAUO instances render | — | 900.03 s timeout |
| 1,024 stacked cubes (12,288 triangles) render | — | 900.01 s timeout (256 cubes: 41.71 s; 64 cubes: 7.62 s) |

The render cost grows with the square of the stack depth, not with the bytes:
- cube stack: 7.62 s at K=64, 41.71 s at K=256, over 900 s at K=1024;
- spread NAUO (S3): 2,545,088 triangles render in 9.97 s;
- coincident NAUO (S6): 594,800 triangles do not finish in 900 s.

Load average during the rows was 6.25 to 22.66 on the shared host. It is recorded on every row and does not change any decision.

## Other facts the run established

- **The STEP entity cap bounds step-copies.** 40, 48, 56 and 62 MiB of LinearSlide copies are refused by `cad-guard` (`too_complex/entities_over_cap`, rc 4). The largest admitted copies stack is about 67 copies (about 38.9 MiB). 74's 437.36 s cell at 40 MiB is therefore not admissible in production.
- **IGES from LinearSlide copies is about 0.88 of the STEP size.** The calibration `RUNG_PICK` measured 4,449,797 STEP bytes against 3,928,014 IGES bytes. This is not 71's "about 6x", which was measured on a different base.
- **S3 mesh at the NAUO cap (n=1518, grid) took 193.11 s** against B_mesh 210.67 s. It is within, but by the smallest margin of any row; its render is refused by the triangle cap (5,592,312).
- **The S2 rung K=2** (104,083,884 bytes, 2,081,676 triangles) renders in 14.23 s. K=4 and above are over the STL ceiling and are written as RUNG_SKIP, not as rows.

## Task Commits

1. **Task 1: Tracer, one stacked STL through generator, engine image, confined stats and render, evidence protocol.** Commit `7570956` (feat).
2. **Task 2: The pre-registered stacking probe from a fresh clone, with per-format decisions.** Commit `1af1995` (docs, evidence).

**Plan metadata:** recorded by the final docs commit of this plan.

## Files Created/Modified

- `scripts/cad-corpus-gen.py`:
  - `stl-stack` kind, `--part cube`, `--nauo-pitch`;
  - the default outputs of every existing kind are unchanged.
- `scripts/cad-dxf-bounds.sh`:
  - `stack` mode with RULE lines, preflight, `stack_rung`/`stack_stage`/`stack_emit`, and per-shape ladders S1 to S6;
  - an embedded `stack.py` that does row classification, floors and the decision;
  - `ENGINE_TAG_PREFIX`, whose default `engine75-` keeps the 75 modes unchanged.
- `.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt`: the probe evidence.

## Decisions Made

- **AR-1, recommended option, already resolved in the plan.** The probe ran, all three formats are `not_byte_bound`, and the remainder branch applies.
- **S5 rungs are IGES byte targets.** "iges-from-step of the S4 files at 32, 64, 96 MiB" was read as IGES files of at most 32, 64 and 96 MiB, plus the largest at or below 141,557,760 bytes.
  - Each rung is the largest whole-MiB step-copies target whose IGES fits. It was found by measured `RUNG_PICK` lines from a 4 MiB calibration file.
  - IGES files written from the S4 STEP files themselves would be about 30 to 58 MB, a different set from the targets the plan names.
- **S4 ceiling rung.** The 63 MiB step-copies target writes 66,277,712 bytes, which is over the 66,060,288 ceiling and would be a 413. The ceiling rung is therefore the largest target at or below 63 whose file fits: 62 MiB, 65,062,164 bytes (`RUNG_PICK` lines). The RULE line says so before any row.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - missing coverage] Added shape S6, coincident NAUO, and `step-nauo --nauo-pitch`.**
- **Found during:** Task 1, while reading `scale_step_nauo`.
- **Issue:** S3 uses the existing `step-nauo` kind, which translates every instance onto a 200 mm grid. The instances do not overlap, so S3 probes instancing but not stacking. Yet the plan's own rationale is that "a NAUO assembly can stack a part deeply inside a few MiB". Running S3 alone would have left STEP's coincident-instance mechanism unprobed, and STEP would have come out `within_budget`.
- **Fix:**
  - Added `--nauo-pitch` to the generator. The default of 200 is byte-identical to the old output, and a non-default pitch gets a `-pitchN` file name.
  - Added S6, `step-nauo --nauo-pitch 0` at the S3 counts, to the pre-registered `shapes` and `decision` RULE lines before any row: step's other mechanisms are S3 and S6.
  - S3 runs exactly as the plan specified.
- **Effect:** S6 is the shape that made STEP `not_byte_bound` (n=100, 598,732 bytes, 900.03 s).
- **Committed in:** `7570956`.

**2. [Rule 2 - correctness] A guard refusal is `band=refused_by_cap`.**
- **Issue:** the plan names `refused_by_cap` only for the triangle cap. Under the `stop` rule as written, a `cad-guard` rc 4 `too_complex` (the production refusal of the S4 40 to 62 MiB files, `entities_over_cap`) would have been a STOP.
- **Fix:** rc 4 with verdict `too_complex` from the guard is `refused_by_cap` and ends the rung, but not the ladder. Any other non-zero rc that is not a timeout or OOM is still a STOP. This was written in the `bands` and `stop` RULE lines before any row.

**3. [Rule 3 - blocking] `iges-from-step` generation runs with 8g.**
- **Issue:** the `max` IGES rung needs OCP to read a step-copies file of about 150 MiB. That can exceed 4g in the generator container, and a generator OOM would STOP the probe for a reason unrelated to render cost.
- **Fix:** generation only (`GEN_MEM=8g`), never a measured stage. This is in the `rows` RULE line. It was not exercised in the end, because the ladder stopped at the 32 MiB rung.

**4. [Rule 2] Extra evidence fields and lines.**
- **What:** STACK_ROW lines carry `sha256, verdict, reason, class, threads_max` after the fields the plan lists. `GEN`, `RUNG_SKIP` and `RUNG_PICK` lines record generator provenance and rung selection.
- **Effect:** the regexes of plans 76-02 and 76-03 (`^STACK_DECISION,format=...,decision=...,` and `^STACK_GLOBAL,render_deadline_rule=...,`) are unaffected.

**5. The pre-registered assumption on STEP MAPPED_ITEM instancing.**
- **The assumption:** MAPPED_ITEM is bounded by the same 1518 expanded-instance guard cap as NAUO and meshes to the same coincident instances, so S6 is counted as its probe. This is written as RULE `assumption_step_instancing`.
- **Effect on the result:** none, because STEP is `not_byte_bound` through S6 itself.

**Total deviations:** 5, all recorded in RULE lines before the first row.

**Impact on plan:** one shape added, one band widened to the production refusal and one generator memory knob; no RULE was edited after a row.

## Issues Encountered

- **Two dev-loop wait shells never returned.** They polled with `pgrep -f` on a pattern that appeared in their own command line, so they matched themselves. They were killed. The measured run was not affected: it was watched by PID.
- **The smoke SUMMARY did not match the plan's verify.** It first printed `rows=1,stack_rows=2,...,result=ok`, which the plan's `grep '^SUMMARY,mode=stack_smoke,rows=1,result=ok'` does not match. The fields were reordered before the Task 1 commit.

## Requirements

`CADW-04` is not marked complete. This plan only answers the premise question. The measured timeout, the paired ceilings and the provenance flip land in 76-02 to 76-04, which carry the same requirement.

## Verification

- Task 1 verify passed (exit 0) on the final smoke file:
  - `SUMMARY,mode=stack_smoke,rows=1,result=ok`;
  - S1 render rc=0;
  - `GEN_IDENTITY ... equal=1`;
  - 11 stack RULE lines;
  - no STACK_DECISION;
  - no comment line after the shebang.
- Task 2 verify passed (exit 0) on `evidence/76-01-stack.txt`:
  - no STOP, `result=ok`, `tracked_dirt=0`;
  - exactly one STACK_DECISION per format and a STACK_GLOBAL line;
  - iges is not byte_bound;
  - 3 FLOOR lines;
  - the first STACK_ROW comes at a later line than the last RULE;
  - 48 STACK_ROW lines (at least 12 required).
- NOT_RUN cross-check gave `NOT_RUN_CHECK=ok`. The awk check was:
  `awk -F, '/^STACK_ROW,/{split($2,a,"="); for(i=1;i<=NF;i++){if($i~/^band=over/||$i=="timeout=1")over[a[2]]=1}} /^NOT_RUN,/{split($2,a,"="); if(!(a[2] in over)){bad=1}} END{exit bad}'`.
  Every NOT_RUN names S1, S5 or S6, and each of those has an earlier `band=over`/`timeout=1` rung.
- `git merge-base --is-ancestor 757095647c83a46ba7ae450cfe10ec665bda2bf0 HEAD` exits 0.
- `docker images ... | grep -c '^engine76-'` gives 0.
- `git diff --exit-code main -- internal/convert/exec.go` exits 0, and `git diff --name-only main -- cmd internal` is empty.
- `git status --porcelain -- .planning/config.json` gives ` M`, the same as before the plan.
- No new comment line: `git diff main -- scripts/cad-corpus-gen.py | grep -cE '^\+[[:space:]]*#'` gives 0, and there is no `#` line after the shebang in `cad-dxf-bounds.sh`.
- `grep -nwE 'prune|image rm|volume rm|down -v|down --volumes' scripts/cad-dxf-bounds.sh` is empty.

## Hygiene and cleanup

- **Fresh clone.** `~/.cache/octoconv/clones/7570956/octoconv` was created with `octoconv-bundle.ttb` copied in and tracked_dirt=0. It was removed after the run.
- **Images.** The `engine76-31b85807c654` tag was built from fully cached layers and removed on exit by the script's cleanup. No other image, container, volume or k8s object was touched.
- **Nothing was pruned.** The run added 18 build-cache records of type `source.local` (build context and Dockerfile snapshots, under 1 MB together). They are left in place, because the plan forbids any prune.
- **VM disk.** It was 19 GiB free at the start of Task 1. The 60 s watchdog saw 17 GiB at the start of the probe and 14 GiB at its end; 14.6 GiB was free at the close. It never went near the 6 GiB floor. No container, image or file of this run remains, so the drop is not attributable to leftovers of this run.

## Next Phase Readiness

76-02 can run `--init`: STACK_DECISION exists for step, iges and stl, and STACK_GLOBAL is `remainder`. The bracketing stacked rungs per format, the largest within and the smallest over, are in the STACK_ROW lines:
- stl: S1 k256 / k1024;
- step: S6 n36 / n100;
- iges: no within rung of S5 below 32MiB in this probe. The only S5 row is the over rung, so the bracket's lower side is absent.

## Self-Check: PASSED

- FOUND: scripts/cad-corpus-gen.py, scripts/cad-dxf-bounds.sh, .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt
- FOUND: commit 7570956 (ancestor of HEAD), commit 1af1995 (ancestor of HEAD)
