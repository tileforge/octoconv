---
phase: 73-cad-class-convert-slice-confined
plan: 01
subsystem: cad-engine
tags: [ocp, landlock, step, iges, stl, docker, guard-rules]
requires:
  - phase: 71-evaluation-gate
    provides: approved hash locks, Landlock candidate set, ceilings and caps, measurement helper
  - phase: 72-engine-free-contract-foundations
    provides: the CAD sniffer that fronts the engine
provides:
  - Dockerfile.cad-worker (venv and engine stages) built from a hash-identical subset of the approved locks
  - cmd/cad-worker/helper/cad-mesh, the production mesh helper with a CADVERDICT last line
  - scripts/cad-stl-judge.py, a content judge for binary STL
  - scripts/cad-spike.sh and scripts/cad-spike.py, the tracer, the bomb spikes and the rule derivation
  - evidence/73-01-spikes.txt with the measured rows, five RULE lines and the DERIVED stage timeouts and ceilings
affects: [73-02, 73-03, 73-04, 73-05, 73-06]
actuals:
  tokens: 32900
  tasks: 2
  commits: 3
plan_head_before: e219f8c66b6331945de02fdc468830f947ef4cef
plan_head_after: 1ac2ad7d6e7f6ad5964b74c96f59f83372d35ec2
tech-stack:
  added: []
  patterns:
    - helper verdict as a last stdout line, stdout re-pointed so engine noise goes to stderr
    - rules written as functions of evidence rows and recomputed by a check mode
key-files:
  created:
    - Dockerfile.cad-worker
    - cmd/cad-worker/requirements-mesh.txt
    - cmd/cad-worker/helper/cad-mesh
    - scripts/cad-stl-judge.py
    - scripts/cad-spike.sh
    - scripts/cad-spike.py
    - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-01-spikes.txt
  modified:
    - .gitignore
key-decisions:
  - "load_check is numeric_strict: a load-check fail of the not-a-Real class is a terminal cad_unreadable, checked after ReadFile and before TransferRoots (auto-selected under user delegation)"
  - "1..2 is recorded as a reader-accepted GAP, no text scan, exempt from the stop condition (auto-selected under user delegation)"
  - "Production IGES ceiling is 135 MiB, superseding the 71 value of 155 MiB; the STEP ceiling stays 63 MiB (auto-selected under user delegation)"
requirements-completed: [CCONV-01, CADIN-04]
duration: 60min
completed: 2026-10-05
status: complete
---

# Phase 73 Plan 01: Tracer and OCP bomb spikes Summary

**A production engine image from the approved hash locks turns inch STEP into a millimetre binary STL under a real Landlock ruleset, and every STEP bomb shape now has an observed outcome on the real OCP reader, from which the guard rules, stage timeouts and the production IGES ceiling are derived.**

## Performance

- **Duration:** about 60 min of wall time, most of it spike cells
- **Started:** 2026-10-05T07:23Z
- **Completed:** 2026-10-05T08:23Z
- **Tasks:** 2
- **Files modified:** 8 (7 created, 1 modified)

## Accomplishments

- Tracer: `units_inch.step` gives exactly 12 triangles with sorted extents 25.4, 50.8, 76.2. The two NIST inch files give an STL diagonal inside the vertex box times 25.4 and 1.10 times it (125.43 mm against 125.08, 907.16 mm against 841.59). The same bytes outside the job directory give no STL while the unconfined control succeeds (`result=denied`). The helper contains no scaling code.
- Spikes: S0 load check over 31 corpus files, S1 six coordinate bombs, S2 two cycle shapes, S3 doubling hierarchies (NAUO, writer-made, and text-level MAPPED_ITEM), S4 three evasion forms, S5 three ceiling files plus an IGES ladder. 61 ROW lines, all naming the image id, all at server_arch arm64 and `--cpus 2 --memory 4g`.
- Rules and derivations are recomputed by `scripts/cad-spike.sh rules --check`, which finds no difference.

## Task Commits

1. **Task 1: Tracer** - `2a8c040` (feat)
2. **Task 2a: Spike harness and observed rows, committed before any rule existed** - `e37f8ce` (feat)
3. **Task 2b: Rules and derivations from the rows** - `1ac2ad7` (feat)

**Plan metadata:** the docs commit that carries this file.

## Results

### RULE lines

| key | value | basis |
|---|---|---|
| load_check | numeric_strict | 0 of 31 valid files have a numeric-class fail; 5 of 5 numeric bombs have one; 7 NIST PMI files carry 9 to 934 non-numeric fails and stay diagnostic |
| assembly_cycle | reader_handles | self-reference and two-node cycle both end rc=2 with a null shape in under 1 s |
| expansion | guard_bounds_expanded_instances | NAUO doubling hierarchy |
| mapped_item | guard_bounds | text-level MAPPED_ITEM nesting doubles triangles each level |
| entity_counter | semicolons_minus_3, evasion_ok=1 | semicolons minus 3 is at least the line regex on all three evasion forms |

### DERIVED lines

| key | value | arithmetic |
|---|---|---|
| cad_guard_timeout_s | 30 | 3 x 0.32 s (largest count wall) is at most 30 |
| cad_mesh_timeout_s | 316 | max(316, ceil(1.5 x 84.62)); 316 is ceil(210.63 x 1.5) from the 71 ladder |
| cad_io_allowance_s | 60 | assumption A7, not measured |
| cad_engine_timeout_s | 406 | 30 + 316 + 60, under 900 |
| cad_pod_grace_s | 426 | 406 + 20 |
| cad_iges_ceiling_bytes | 141557760 (135 MiB) | largest measured clean IGES mesh row with cgroup peak at most 2867 MiB: 135.4 MiB peaked at 2703 MiB; 148.0 MiB peaked at 2925 MiB. Never extrapolated upward |
| cad_step_ceiling_bytes | 66060288 (63 MiB) | the 61.6 MiB point peaked at 1150 MiB, far under 2867 MiB |

Plan 73-05 takes the IGES ceiling as 135 MiB. The IGES peaks are measured on LinearSlide-based copies and are content dependent.

### Stop conditions

- **Strictness ambiguity on partial STL:** it tripped. With the pre-registered rule `load_check` computed `diagnostic`, and `abc`, `--1` and `1..2` still produced a 12-triangle STL. I returned a checkpoint. The orchestrator chose option A (numeric-class strict) and exempted `1..2` as reader-accepted. Both were auto-selected under user delegation (2026-10-04). After the change the stop evaluates clean: 5 numeric hits, 1 reader-accepted, 0 unexplained.
- **Derived timeout at or above 900 s:** not tripped, the engine timeout is 406 s.
- **Package outside the approved locks:** not tripped. The import smoke passed with only numpy and the OCP pair.

### Bomb observations the guard must act on (input to 73-02)

- **Doubling hierarchy and MAPPED_ITEM are expansion bombs.** A 570 KB STEP with 28 NAUO tokens at K=14 meshes to 28.0 M triangles and a 1.4 GB STL. K=15 peaks at 4096 MiB and K=16 is OOM-killed (rc 137). A 20 KB text-level MAPPED_ITEM file at K=16 gives 786 432 triangles. Neither the NAUO edge cap nor a bare token count bounds them, so the guard must compute and bound expanded instances for both representations.
- **Writer-made "mapped" rows carry no MAPPED_ITEM.** `STEPControl_Writer` emits NAUO, so those rows have `mapped_tokens=0`. The `mapped_item` rule therefore counts only rows with `mapped_tokens>0`, which the text-level series supplies.
- **NaN coordinate hangs in TransferRoots** until the 400 s budget. The numeric-class check has to run before TransferRoots.
- **`1e300` and `1e999`** end as a null shape at transfer.
- **Evasion:** `/* x */` comments and CR-only line endings take the line-anchored regex to 0 while the `;` count stays 356. The `;` counter is the one to use.
- **Mesh walls:** 84.62 s at 61.6 MiB STEP, 2.99 s for flat NAUO 1518, 55.96 s at 148 MiB IGES. The production helper does no volume or validity analysis, so it is much faster than the 71 helper.

## Decisions Made

- The three decisions above, all "auto-selected under user delegation".
- The tracer helper re-points stdout to stderr and writes the verdict to the saved descriptor, so OCCT noise never follows the verdict line.

## Deviations from Plan

**1. [Rule 3 - Blocking] Stale invariance baseline.** The plan names `520ecd4`, which predates `b4eba28` (it added `scripts/cad-measure/verdicts.sh`), so `git diff 520ecd4 -- scripts/cad-measure` cannot be empty. Invariance was verified against `e219f8c`: exec.go, scripts/cad-measure, Dockerfile.cad-measure, go.mod, go.sum, internal/, cmd/api, deploy and docker-compose.yml are unchanged. Use `e219f8c` in any re-run of the plan's verify.

**2. [Rule 3 - Blocking] `.gitignore`.** Added `/cad-worker`. `internal/repohygiene` fails as soon as `cmd/cad-worker/` exists, and it passes now. Committed in `2a8c040`.

**3. [Rule 1 - Bug in plan operand] IGES base.** The IGES ceiling files use the LinearSlide base, not freecad: freecad's 9 MB copy unit cannot get near 155 MiB.

**4. Harness additions.** `scripts/cad-spike.sh` gained `finish` and `spikes --only`. `scripts/cad-spike.py` gained `gen-mapped-text`, `copies-target`, `rowline`, `loadcheck`, `binding`, `rules` and `stops`.

**5. [Rule 2 - Missing functionality] Rule operands that did not match the object.**
- The `mapped_item` rule counts rows with `mapped_tokens>0`, because the writer-made rows had none.
- `load_check` gained the `numeric_strict` value and a numeric-class counter, per the orchestrator's decision.
- Extra DERIVED lines (`cad_iges_ceiling_bytes`, `cad_step_ceiling_bytes`) and two extra GAP lines make the SUMMARY line `derived=7,gaps=3` instead of the planned `derived=5,gaps=1`.
- The S5I ladder (five IGES mesh rows) was added to derive the IGES ceiling.

**Total deviations:** 5 (2 blocking fixes, 1 operand correction, 1 additive harness change, 1 rule-operand and scope extension). No scope creep outside the plan's files.

## Issues Encountered

- A first `abc` exploration looked like a hang because output was block-buffered; the hang was the NaN file, confirmed with `-u`.
- OCCT prints load-check messages to stdout. The helper keeps its stdout clean by writing the verdict to a saved descriptor.

## Known Stubs

None.

## Threat Flags

None. The spikes ran only in containers with `--network none`, capped at `--cpus 2 --memory 4g`.

## Known Gaps (recorded in the evidence)

- `iges_inch_scaling`: unproven, no inch IGES in the corpus (R-01).
- `lexer_accepts_double_dot_real`: OCCT reads `1..2` with no load-check entry; bbox_diag 95.0381 equals the baseline.
- `iges_ceiling_supersedes_71`: 135 MiB supersedes the 71 value of 155 MiB for the production engine.
- The IGES ceiling peaks come from one content family (LinearSlide copies); 71's freecad IGES points were measured on the heavier 71 helper.
- amd64 runtime is unmeasured; only the arm64 native image was run.

## Next Phase Readiness

73-02 gets the guard rules and the verdict line contract. 73-03 re-measures Landlock on the production image, so the tracer's 71 candidate set is a starting point only. 73-05 takes 63 MiB (STEP), 135 MiB (IGES), 316 s (mesh), 406 s (engine) and 426 s (pod grace). The 1e12 finite-box logic and the numeric-class check run before transfer.

## Self-Check: PASSED

- All created files exist, and commits `2a8c040`, `e37f8ce` and `1ac2ad7` are ancestors of HEAD.
- `scripts/cad-spike.sh rules --check` reports no difference.
- The plan's Task 2 automated verify passes.
- `TestAllEnginesCoversEveryEngineClass`, `TestDefaultRegistersNoCADPair` and `TestGitignoreCoversEveryCommandBinary` pass.
- No generated input is tracked.
