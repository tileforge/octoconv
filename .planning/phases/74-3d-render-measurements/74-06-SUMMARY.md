---
phase: 74-3d-render-measurements
plan: 06
subsystem: cad-engine
tags: [landlock, render, pixel-judge, preflight, sandbox-paths]
status: partial
requires:
  - phase: 74-05
    provides: render registered and exposed, provisional cadRenderReadOnly
provides:
  - the render read-only set measured on the production image, and guard and mesh re-measured
  - SANDBOX-PATHS.md render subsection with the conf05:cadrender blocks, held by conf05-crosscheck.sh
  - a Go test that fails when the cad-render helper imports OpenCascade
  - CADPreflight proving render both ways, the pixel judge and the inch measurements
  - scripts/cad-render-probe.sh judge, the Go judge against pixelcheck.py on real renders
affects: [74-07]
key-files:
  created:
    - .planning/phases/74-3d-render-measurements/evidence/74-06-landlock.txt
    - .planning/phases/74-3d-render-measurements/evidence/74-06-judge.txt
  modified:
    - scripts/cad-worker-sandbox-measure.sh
    - scripts/cad-render-probe.sh
    - scripts/conf05-crosscheck.sh
    - .planning/research/SANDBOX-PATHS.md
    - internal/convert/cadsandbox.go
    - internal/convert/cadsandbox_test.go
    - cmd/cad-worker/main.go
    - internal/worker/runner.go
key-decisions:
  - "cadRenderReadOnly stays equal to the mesh paths: the measured set is /opt/venv, /usr/bin/python3.11, /usr/lib/python3.11 plus multiarch; /proc/cpuinfo and /sys/devices/system/cpu appear in the trace and are removable"
  - "The plan stopped at its own stop condition: a W15 thin frame at 256 px fails the Go judge on J1; thresholds were not retuned"
  - "amd64 render Landlock is deferred and stated; the measurement script no longer rebuilds the emulated amd64 image unless --amd64 is passed"
commits: 5
plan_head_before: 9ce961742ae865897cf6ca53464fc24bad334cb6
plan_head_after: 5142db690aae2a455e934f81dffed7b15c6b3c33
actuals:
  tokens: 32600
  tasks: 2
  commits: 5
requirements-completed: []
---

# Phase 74 Plan 06: Render Landlock measurement, judge calibration and preflight Summary

The render stage's ruleset is measured and recorded, the mesh and guard sets are re-measured unchanged, and the preflight code is written and unit-proven; the plan stopped before its live proof because a thin W15 frame at 256 px fails the Go pixel judge, which the plan reserves for a D-06 decision.

## Status: PARTIAL, stopped at the plan's stop condition

Task 1 (measurement) is complete. Task 1's judge half stopped where the plan says to stop. Task 2 is complete. Task 3's code and unit tests are complete; its live transcript (`74-06-sandbox-liveproof.txt`) was NOT produced.

### The stop: W15 at 256 px fails the Go judge

`evidence/74-06-judge.txt`, `JUDGE_SUMMARY,frames=49,agreements=47,thin_only_divergences=2,unexplained_divergence=0,render_failed=0,real_frames=46,real_go_ok=46,w15_frames=3,w15_go_ok=2,blank_rejected_by_both=1,result=fail`, then `STOP`.

- W15 (first 4396 triangles of `nasa-sls-block1.stl` as ASCII, 71 ladder `stl-ascii-nasa-sls-block1-1MiB.stl`, sha256 `77492adb...`) at iso, opaque:
  - 1280 px: Go ok, mode thin, box 93x1030, margin 124; pixelcheck ok=0 (frac 0.0022, dark 0.0005). Listed as a thin-rule divergence.
  - 2048 px: Go ok, mode thin, box 149x1648, margin 198; pixelcheck ok=0. Listed as a thin-rule divergence.
  - 256 px: Go REJECTS: `J1 186 part pixels cannot span a 206 px extent` (reproduced by running `cad-pixel-judge` on the re-rendered frame). Facts: box 20x206, count 186, dark_count 65, frac 0.0028, dark 0.0010, margin 25, aspect 0.097. pixelcheck also rejects it (ok=0). The thin rule never gets to apply: J1 (`count < long side`) fires first, although the thin conditions themselves would hold (aspect 0.097 < 0.15 and dark_count 65 >= 0.25 x 206 = 51.5).
- Consequence: a real thin part rendered at the minimum size 256 would be rejected as `ErrCADRenderRejected` (`cad_empty_result` to the client). The helper's own verdict was `ok` with `triangles=4396` and `verdict_match=1`, so the render itself is fine; only the judge refuses it.
- Not done on purpose: no threshold or rule was changed. Settling it is D-06's: relax J1 for frames the thin rule would admit (for example require J1 only outside thin mode), raise `CADMinRenderSize`, or accept that thin parts at 256 are refused.
- Every other required outcome held: all 46 real-part frames (five parts, seven views at 1280, 256/512/2048 for LM8UU and V-Slot, transparent iso for each) are ok in Go and agree with pixelcheck; both blank frames (opaque and transparent) are rejected by both judges.

### Not done because of the stop

- `evidence/74-06-sandbox-liveproof.txt` (`scripts/sandbox-liveproof.sh cad` on the final image, and the preflight wall time read from the worker log). A change to `cadpixel.go` rebuilds the cad-worker binary, so a transcript taken now would be redone; the emulated amd64 build inside that script would also take the free disk (9.2 GiB free after this plan, floor 6) to its edge. `cadPreflightTimeout` is therefore still 60 s and unmeasured.
- The Task 1 verify line as written does not hold (the judge exits 1), and the Task 3 verify line stops at its first half.

## What was built

- **Measurement (commit 552d85d).** `scripts/cad-worker-sandbox-measure.sh`: stages `guard mesh render`, per-stage input lists, mesh argv `--format F in out --quality standard --measure /job/measure.json`, `cad-mesh stats` for the three STL inputs, render inputs derived from the mesh REF STLs plus `stl_ascii_box`, `stl_binary_solid_header`, `nasa-hubble` and the W15 slice (14 inputs x `iso 1280 opaque`, `top 2048 opaque`, `iso 1280 transparent` = 42 render cases), signature = verdict + STL and measure.json sha256 (mesh) or PNG sha256 (render), every render REF must be an ok frame, the render effect row places the STL outside every grant, `--recheck` reads `74-06-landlock.txt`. Result: `MEASURED,stage=render,ro=/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11,engines=cad-render,multiarch=/usr/lib/aarch64-linux-gnu`; guard and mesh MEASURED lines equal the Go vars (nothing to update); `SUMMARY,stages=3,inputs=65,sufficient=130,different=0,leaked=0,effect_denied=7,negative_controls=3,removal_rows=32,proc_sys_tmp=none`. Seven render entries removable (`/etc/ld.so.cache`, `/etc/nsswitch.conf`, `/etc/passwd`, `/proc/cpuinfo`, `/sys/devices/system/cpu`, `/usr/lib/locale`, `/usr/share/zoneinfo`), five load-bearing. Render negative control drops `/opt/venv` and fails on the first input. `--recheck` ends `RECHECK-OK,cases=14`. Image `sha256:d1ee5a9ef804a300a8ba2344561a439ae6e99a641ef3cb561e3142ee107ac685`, arm64 native, kernel 7.0.14-orbstack.
- **Judge mode (commit a9563a0).** `scripts/cad-render-probe.sh judge`, rules as fixed in the plan, evidence as above.
- **Record and crosscheck (commit 1a4bdb7).** SANDBOX-PATHS.md render subsection and blocks; `conf05-crosscheck.sh cadrender`, `cad` expands to three classes and prints `MATCH: cadrender readonly (3) engines (1)`; mutating a `cadrender:readonly` line makes `cad` exit 1 and the document is restored byte for byte (cmp); the other seven classes still exit 0. Go pins for `cadRenderReadOnly` and `cadRenderEngines`, the banned-path list now covers render, and `TestCADRenderHelperImportsNoOpenCascade` (mutation-proved: an `import OCP` line makes it fail naming the line).
- **Preflight (commit 5142db6).** `CADPreflight`: `requireOutsideGrants` includes `cadRenderReadOnly`; mesh runs with `--measure` and the draft must give inch, declared, sorted extents 1 2 3 (1e-4), volume 6 and area 22 (1e-6); the fixture STL is rendered iso 256 opaque and transparent and judged by `JudgeCADPNG`; a blank 256 frame must be rejected by the same judge; a copy outside the job directory must be denied by the render stage (`requireCADDenied` plus `requireDenied`). Tests: blank frame names the pixel judge, outside-render leak names confinement, inch measurement mismatches (unit, volume, area, extents, undeclared) name units, a sandbox-unavailable outside run is refused as a denial naming the render stage, `cadRenderReadOnly` is checked against the outside directory. `cmd/cad-worker/main.go` logs `cad-guard, cad-mesh and cad-render ran inside the job directory ...`.
- **Extra item 1 (commit fd329bd).** The `SideOutputs` doc comment in `internal/worker/runner.go` now reads that probe populates it with the sanitize report and cad with the render measurements as role=measurements; three existing lines edited, no line added.

## Extra items recorded

1. runner.go doc comment: done as above. Against the phase-73 merge-base runner.go adds four `//` lines (the one edited in 74-05 plus these three edited lines); all are edits of pre-existing lines, none is a new comment line.
2. Orchestrator decision on the one-operation-per-engine invariant: 74-05 relaxed `TestOneOperationPerEngineClass` so that cad is pinned to exactly convert and render. The orchestrator ACCEPTS this under the user's delegation: the v1.16 roadmap decision is one `cad:job` task type for both operations. No code change here.

## Verification

- `GOTOOLCHAIN=go1.26.4`: `gofmt -l` clean, `go build ./...`, `go vet ./...`, `go test ./... -count=1` all pass on the final tree; `git diff --exit-code main -- internal/convert/exec.go` empty; `cad-guard` untouched against the phase-73 merge-base.
- The plan's added-comment acceptance command for Task 3 (`... | grep -E '^\+\s*//' | grep -qv 'go:embed'`) returns non-zero on macOS for an empty diff because BSD `grep -qv` exits 0 on empty input; the direct check (no `+//` line in `cadsandbox.go`, `cadsandbox_test.go`, `main.go` against the merge-base) is empty.
- All five code commits are SSH-signed.

## Deviations from Plan

**1. [Plan interpretation] amd64 build in the measurement script**
- `amd64_proof` now runs only with `--amd64`; the default prints `AMD64,...,status=deferred,landlock=not_measured,render=not_measured`. Reason: the engine image now carries VTK, the emulated build would take the free disk below the floor, and the plan requires amd64 render to stay deferred and stated. The `MULTIARCH` row still compares the Go map with `x86_64-linux-gnu` and prints `amd64_status=derived`.

**2. [Plan interpretation] trace breadth for render**
- The render trace covers every input at `iso 1280 opaque` and the other two variants for `units_inch` and `nasa-hubble` (18 traces, not 42); sufficiency and the removal table run the full 42-case matrix.

**3. [Rule 1 - Bug] stray file from a test stub**
- The first version of the preflight stub wrote the measurements draft even for the outside mesh run, which on macOS `cp ... ""` left `internal/convert/draft.json` in the working tree; the stub now writes only when its eighth argument is non-empty.

**4. Commit ledger started late**
- The plan commit ledger file was created after the first commit, from the recorded pre-plan HEAD (`9ce9617`), so `commits: 5` is measured from it.

## Known Stubs

None.

## Threat Flags

None beyond the register. T-74-12 is mitigated by the removal table, the denied outside-file row, the failed negative control and the preflight denial (unit-proven, live proof pending); T-74-13 by the `cadrender` crosscheck with its mutation proof.

## Notes for the next step

- D-06 decision needed: what the judge does with a thin frame at 256 px (see "The stop"). After it, re-run `scripts/cad-render-probe.sh judge`, then rebuild the image and run `scripts/sandbox-liveproof.sh cad` teeing to `evidence/74-06-sandbox-liveproof.txt`, read the preflight wall from the worker log, and raise `cadPreflightTimeout` only if it exceeds 30 s.
- A leftover stopped container named `octoconv-cad-worker` (exited 4 hours before this plan ran) is not mine and was left alone.

## Self-Check: PASSED

The evidence files, the SANDBOX-PATHS blocks and commits 552d85d, a9563a0, 1a4bdb7, fd329bd, 5142db6 exist and are ancestors of HEAD; the missing live transcript is listed above, not claimed.
