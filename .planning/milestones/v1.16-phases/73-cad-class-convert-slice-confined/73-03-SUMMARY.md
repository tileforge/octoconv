---
phase: 73-cad-class-convert-slice-confined
plan: 03
subsystem: cad-engine
tags: [landlock, sandbox-paths, cad-guard, cad-mesh, crosscheck, multiarch]
requires:
  - phase: 73-01
    provides: Dockerfile.cad-worker engine stage, cad-mesh, the CADVERDICT contract
  - phase: 73-02
    provides: cad-guard, CADConverter, the seeded set vars and cadMultiarchLib
provides:
  - scripts/cad-worker-sandbox-measure.sh, the production-image Landlock measurement with --recheck
  - evidence/73-03-landlock.txt, one MEASURED line per stage backed by removal rows, effectiveness and negative controls
  - SANDBOX-PATHS.md section cad with the five conf05 blocks
  - scripts/conf05-crosscheck.sh classes cadguard, cadmesh and the cad alias with the multiarch comparison
affects: [73-04, 73-05, 73-06]
actuals:
  tokens: 14291
  tasks: 2
  commits: 2
plan_head_before: 517d4d9353bc20a56b167d97c27d491d152be293
plan_head_after: aa511380fb1406639433ed621f3aa555624ce7c6
tech-stack:
  added: []
  patterns:
    - content-judged sufficiency (verdict line plus STL hash), never the exit code
    - one-at-a-time removal table with a per-stage negative control
    - document blocks diffed against Go literals, with a map form for the multiarch table
key-files:
  created:
    - scripts/cad-worker-sandbox-measure.sh
    - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-03-landlock.txt
  modified:
    - Dockerfile.cad-worker
    - .planning/research/SANDBOX-PATHS.md
    - internal/convert/cadsandbox.go
    - internal/convert/cadsandbox_test.go
    - scripts/conf05-crosscheck.sh
key-decisions:
  - "The measured sets equal the 73-02 seeds: guard is python3.11 plus its stdlib, mesh adds /opt/venv; the engine path and the multiarch directory are load-bearing but kept outside the flat sets"
  - "amd64 is a build proof, recorded as landlock=not_measured; the amd64 triplet is observed in the emulated image and recorded as derived"
requirements-completed: [CADW-02]
duration: 105min
completed: 2026-10-05
status: complete
---

# Phase 73 Plan 03: Landlock sets measured on the production engine image Summary

**Both cad stages now run under Landlock sets measured on the engine image that ships, each entry justified by a one-at-a-time removal row, effectiveness and a negative control per stage, recorded once in SANDBOX-PATHS.md and held equal to the Go vars by a crosscheck that fails on drift in either direction.**

## Accomplishments

- **Measurement script.** `scripts/cad-worker-sandbox-measure.sh` builds `--target engine` from a staged context, refuses an emulated image, cross-compiles `sandbox-probe` with go1.26.4, and runs ten inputs (seven STEP, three IGES) through both stages with the production argv and environment. Steps: REF, TRACE, CANDIDATE, SUFFICIENT, EFFECT, REMOVAL, NEGATIVE_CONTROL, MEASURED, AMD64, MULTIARCH, SUMMARY. `--recheck` reads the MEASURED lines back and re-runs one STEP and one IGES per stage. It removes every tag it created.
- **Result.** `SUMMARY,stages=2,inputs=10,sufficient=40,different=0,leaked=0,effect_denied=5,negative_controls=2,removal_rows=20,proc_sys_tmp=none`. A second full run reproduced every MEASURED, REMOVAL, NEGATIVE_CONTROL and MULTIARCH row.
- **Measured sets.**

| Stage | ro | engine | multiarch |
|---|---|---|---|
| guard | `/usr/bin/python3.11`, `/usr/lib/python3.11` | `cad-guard` | `/usr/lib/<triplet>` |
| mesh | `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/python3.11` | `cad-mesh` | `/usr/lib/<triplet>` |

- **Removal tables.** Guard candidate of nine entries: five removable (`/etc/ld.so.cache`, `/usr/lib/locale`, `/usr/lib/python3`, `/usr/local/lib/python3.11`, `/usr/share/zoneinfo`), four load-bearing (interpreter, stdlib, engine, multiarch). Mesh candidate of eleven: six removable (`/etc/ld.so.cache`, `/etc/nsswitch.conf`, `/etc/passwd`, `/sys/devices/system/cpu`, `/usr/lib/locale`, `/usr/share/zoneinfo`), five load-bearing (`/opt/venv`, interpreter, stdlib, engine, multiarch). The load-bearing entries alone are sufficient on all ten inputs, so there is no interaction. Neither set contains `/tmp`, `/proc` or `/sys`.
- **Effectiveness.** The same `units_inch.step` outside every grant gives no ok verdict from the guard (`cannot_count`) and no STL from the mesh (`read_failed`), on the candidate and on the measured set. Under the guard's measured set `/opt/venv/pyvenv.cfg` is unreadable while it reads without a ruleset. Negative controls: the guard without `/usr/lib/python3.11` and the mesh without `/opt/venv` both fail on the first input (`failed_as_expected`).
- **Architecture.** The multiarch directory is outside both flat sets. `/usr/lib/aarch64-linux-gnu` is load-bearing in both tables on the native image. The emulated `linux/amd64` build of the same Dockerfile has `/usr/lib/x86_64-linux-gnu`, all five set entries, a passing import smoke, guard ok and a 12-triangle mesh on `units_inch.step`; the line says `kind=build_proof,landlock=not_measured`. `MULTIARCH` records `go_map=match` against `cadMultiarchLib`.
- **Document and crosscheck.** The `cad` section of SANDBOX-PATHS.md carries exactly one pair each of `cadguard:readonly`, `cadguard:engines`, `cadmesh:readonly`, `cadmesh:engines` and `cad:multiarch`. `scripts/conf05-crosscheck.sh cad` exits 0; the other seven classes still exit 0. Mutation checks, each restored and `cmp`-verified: a changed mesh entry gives 1, a deleted multiarch block gives 2, a changed multiarch doc line gives 1, a changed Go engine gives 1, a changed Go multiarch value gives 1, a renamed Go var gives 3.
- **Go pins.** The four set vars are one entry per line with unchanged values. `TestCADSetsAreTheMeasuredOnes` names the script and the evidence file and tells the reader to re-measure; two new tests hold that the guard set has no path to `/opt/venv` and that neither set carries `/tmp`, `/proc` or `/sys`.

## Task Commits

1. **Task 1: Measurement on the production image** - `9321ca4` (feat)
2. **Task 2: Record the sets once, crosscheck, pins** - `aa51138` (feat)

Both are SSH-signed (`Good "git" signature`).

## Crosscheck diff (added lines)

Code: the `cad` alias and `CAD_MULTIARCH` flag, the `cadguard` and `cadmesh` cases in `class_go_file`, `class_go_readonly_var` and `class_go_engines_var`, `doc_multiarch_lines`, `extract_go_map`, and the multiarch comparison block before the final `exit`. Updated usage header text only: the class count and list, the `cad` alias paragraph, the "other six classes" count, and the multiarch note. The `unknown class` message lists the new names. No other comment lines.

## Deviations from Plan

**1. [Rule 3 - Blocking] Stale invariance base.** The plan names `520ecd4`; `scripts/cad-sandbox-measure.sh` differs between `520ecd4` and `e219f8c` (the 73-01 deviation 1 base problem). `git diff --exit-code e219f8c -- internal/convert/exec.go scripts/cad-sandbox-measure.sh internal/api internal/queue docker-compose.yml deploy go.mod go.sum internal/convert/converters.go` prints nothing.

**2. [Rule 1 - Operand] Comment check.** The plan's acceptance check `grep -qE '^\+\s*//'` also matches the `//go:embed` directive that 73-02 added to `cadsandbox.go`. It was run with that single directive excluded: no other comment line was added to either Go file.

**3. Script shape.** The script has no usage comment block (the project rule is no comments by default); `usage()` prints the synopsis and exit codes. `git add -f` of the evidence file was done at commit time, not inside the script.

**4. TDD.** The measured sets equal the 73-02 seeds, so the set-pin tests had no red phase. The crosscheck's drift detection was proven by the mutation checks above instead.

**5. Removal rows name only the first changed input.** Each load-bearing row stops at the first input that changed, which is `units_inch` (the first in the matrix) every time. A `removable` verdict needs all ten inputs identical.

**Total deviations:** 5, none outside the plan's files.

## Issues Encountered

- The image id in the evidence HDR (`sha256:566f27bb...`) differs from the id of the earlier development run: rebuilds are not byte-identical. The sets and every verdict row were identical across both runs.
- Free disk was 9.9 GiB at the start and 8.2 GiB at the end (build cache); no image created by this plan remains.

## Known Stubs

None.

## Known Gaps

- amd64 Landlock is not measured, only the build and the unconfined smoke under emulation. The `amd64` row of the multiarch map is observed in the build, not measured.
- The IGES and STEP inputs are the plan's ten; sets were not re-measured against bomb-class inputs (73-01 spikes cover the reader, not the filesystem set).

## Threat Flags

None. T-73-08 is mitigated by the removal rows, the outside-input effectiveness runs, the guard-without-venv check and the job-directory-only writable set. T-73-09 is mitigated by the crosscheck extension and its mutation proof.

## Self-Check: PASSED

- Created files exist: `scripts/cad-worker-sandbox-measure.sh`, `evidence/73-03-landlock.txt`.
- Commits `9321ca4` and `aa51138` are ancestors of HEAD.
- Task 1 and Task 2 automated verifies pass; `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -count=1`, `go build ./...`, `go vet ./...` and `gofmt -l internal cmd` are clean.
- STATE.md and ROADMAP.md untouched by this plan; cad is not registered.
