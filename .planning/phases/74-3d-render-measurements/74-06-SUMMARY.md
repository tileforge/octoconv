---
phase: 74-3d-render-measurements
plan: 06
subsystem: cad-engine
tags: [landlock, render, pixel-judge, preflight, sandbox-paths]
status: complete
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
  - "D-06, decided by the orchestrator under the user's delegation after the plan stopped on a W15 thin frame at 256 px: the span check of J1 applies to blob-mode frames only; a frame meeting the thin criteria (aspect < 0.15, dark_count >= 0.25 x long side, J2, J3, J0) is judged by the thin rule; CADMinRenderSize is not raised"
  - "amd64 render Landlock is deferred and stated; the measurement script no longer rebuilds the emulated amd64 image unless --amd64 is passed"
commits: 9
plan_head_before: 9ce961742ae865897cf6ca53464fc24bad334cb6
plan_head_after: 242684479fb0a3f78dfdbac3d84b81ce14108fd2
actuals:
  tokens: 32600
  tasks: 3
  commits: 9
requirements-completed: []
---

# Phase 74 Plan 06: Render Landlock measurement, judge calibration and preflight Summary

The render stage's ruleset is measured and recorded, the mesh and guard sets are re-measured unchanged, the Go pixel judge is calibrated against pixelcheck (J1's span check now blob-only, D-06), and the preflight proves render both ways on the real engine.

## Status: complete, with one check NOT RUN by authorisation

The plan first stopped at its own stop condition (below, kept as the record), the orchestrator settled D-06, and the plan was finished.

### The stop and the D-06 decision

First judge run: 49 frames, 47 agree, 2 thin-only divergences, 0 unexplained, but the W15 frame (first 4396 triangles of `nasa-sls-block1.stl` as ASCII, sha256 `77492adb...`) at 256 px was rejected by Go with `J1 186 part pixels cannot span a 206 px extent` (box 20x206, dark_count 65, aspect 0.097, margin 25; pixelcheck also rejects it). The thin criteria held, but J1 fired before the thin rule could apply, so a real thin part at the minimum size would have been refused although the helper's own verdict was ok.

Decision (orchestrator, under the user's delegation): relax J1 where the thin rule admits the frame; do NOT raise `CADMinRenderSize`. Implemented in commit 3c2c1ee: J2 and J3 now run first, then the blob case (which alone keeps the `count < long` span check), then the thin case, then J4. Empty and blank frames still fail on J1 (no part pixels) and J4. Tests added: `TestJudgeCADPNGAcceptsW15ThinFrameAt256WithFewerPixelsThanItsExtent` (186 part pixels against a 206 px extent, thin, margin 25) and `TestJudgeCADPNGRejectsSparseNoise` (scattered dots and a dotted line, both J4); both fail when the old order is restored (mutation-proved), and the existing judge tests pass.

Second judge run (`evidence/74-06-judge.txt`): `JUDGE_SUMMARY,frames=49,agreements=46,thin_only_divergences=3,unexplained_divergence=0,render_failed=0,real_frames=46,real_go_ok=46,w15_frames=3,w15_go_ok=3,blank_rejected_by_both=1,result=ok`. Every real-part frame is ok in Go, W15 is ok at 256, 1280 and 2048 (three thin-rule divergences from pixelcheck, all allowed), both blank frames are rejected by both judges.

### Live proof (`evidence/74-06-sandbox-liveproof.txt`)

`scripts/sandbox-liveproof.sh cad` on the committed tree (commit c40b54f, tracked dirt 1 = the pre-existing STATE.md modification): confined-startup, refuses-without-landlock, escape-hatch and preflight-catches-restrict-self PASS; the proven line reads `cad sandbox proven: cad-guard, cad-mesh and cad-render ran inside the job directory and were denied a file outside it`. The fifth check, emulated-amd64-refuses, is NOT RUN: a disk watchdog stopped the emulated amd64 build at 5.77 GiB free (floor 6 GiB; the orchestrator allowed skipping it), so the transcript has no ALL PASS line and nothing about amd64 is claimed. The image built by the run was removed.

Preflight wall from the worker log: landlock line 16:27:27, proven line 16:27:30, about 3 s (1 s resolution), far under 30 s, so `cadPreflightTimeout` stays 60 s.

## What was built

- **Measurement (commit 552d85d).** `scripts/cad-worker-sandbox-measure.sh`: stages `guard mesh render`, per-stage input lists, mesh argv `--format F in out --quality standard --measure /job/measure.json`, `cad-mesh stats` for the three STL inputs, render inputs derived from the mesh REF STLs plus `stl_ascii_box`, `stl_binary_solid_header`, `nasa-hubble` and the W15 slice (14 inputs x `iso 1280 opaque`, `top 2048 opaque`, `iso 1280 transparent` = 42 render cases), signature = verdict + STL and measure.json sha256 (mesh) or PNG sha256 (render), every render REF must be an ok frame, the render effect row places the STL outside every grant, `--recheck` reads `74-06-landlock.txt`. Result: `MEASURED,stage=render,ro=/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11,engines=cad-render,multiarch=/usr/lib/aarch64-linux-gnu`; guard and mesh MEASURED lines equal the Go vars (nothing to update); `SUMMARY,stages=3,inputs=65,sufficient=130,different=0,leaked=0,effect_denied=7,negative_controls=3,removal_rows=32,proc_sys_tmp=none`. Seven render entries removable (`/etc/ld.so.cache`, `/etc/nsswitch.conf`, `/etc/passwd`, `/proc/cpuinfo`, `/sys/devices/system/cpu`, `/usr/lib/locale`, `/usr/share/zoneinfo`), five load-bearing. Render negative control drops `/opt/venv` and fails on the first input. `--recheck` ends `RECHECK-OK,cases=14`. Image `sha256:d1ee5a9ef804a300a8ba2344561a439ae6e99a641ef3cb561e3142ee107ac685`, arm64 native, kernel 7.0.14-orbstack.
- **Judge mode (commit a9563a0).** `scripts/cad-render-probe.sh judge`, rules as fixed in the plan, evidence as above (re-run in c40b54f after the J1 reorder).
- **Record and crosscheck (commit 1a4bdb7).** SANDBOX-PATHS.md render subsection and blocks; `conf05-crosscheck.sh cadrender`, `cad` expands to three classes and prints `MATCH: cadrender readonly (3) engines (1)`; mutating a `cadrender:readonly` line makes `cad` exit 1 and the document is restored byte for byte (cmp); the other seven classes still exit 0. Go pins for `cadRenderReadOnly` and `cadRenderEngines`, the banned-path list now covers render, and `TestCADRenderHelperImportsNoOpenCascade` (mutation-proved: an `import OCP` line makes it fail naming the line).
- **Preflight (commit 5142db6).** `CADPreflight`: `requireOutsideGrants` includes `cadRenderReadOnly`; mesh runs with `--measure` and the draft must give inch, declared, sorted extents 1 2 3 (1e-4), volume 6 and area 22 (1e-6); the fixture STL is rendered iso 256 opaque and transparent and judged by `JudgeCADPNG`; a blank 256 frame must be rejected by the same judge; a copy outside the job directory must be denied by the render stage (`requireCADDenied` plus `requireDenied`). Tests: blank frame names the pixel judge, outside-render leak names confinement, inch measurement mismatches (unit, volume, area, extents, undeclared) name units, a sandbox-unavailable outside run is refused as a denial naming the render stage, `cadRenderReadOnly` is checked against the outside directory. `cmd/cad-worker/main.go` logs `cad-guard, cad-mesh and cad-render ran inside the job directory ...`.
- **Extra item 1 (commit fd329bd).** The `SideOutputs` doc comment in `internal/worker/runner.go` now reads that probe populates it with the sanitize report and cad with the render measurements as role=measurements; three existing lines edited, no line added.

## Extra items recorded

1. runner.go doc comment: done as above. Against the phase-73 merge-base runner.go adds four `//` lines (the one edited in 74-05 plus these three edited lines); all are edits of pre-existing lines, none is a new comment line.
2. Orchestrator decision on the one-operation-per-engine invariant: 74-05 relaxed `TestOneOperationPerEngineClass` so that cad is pinned to exactly convert and render. The orchestrator ACCEPTS this under the user's delegation: the v1.16 roadmap decision is one `cad:job` task type for both operations. No code change here.

## Verification

- `GOTOOLCHAIN=go1.26.4`: `gofmt -l` clean, `go build ./...`, `go vet ./...`, `go test ./... -count=1` all pass on the final tree; `git diff --exit-code main -- internal/convert/exec.go` empty; `cad-guard` untouched against the phase-73 merge-base.
- After the J1 reorder: `go build`, `go vet` and `go test ./... -count=1` pass again; the plan's added-comment acceptance command for Task 3 (`... | grep -E '^\+\s*//' | grep -qv 'go:embed'`) returns non-zero on macOS for an empty diff because BSD `grep -qv` exits 0 on empty input; the direct check (no `+//` line in `cadsandbox.go`, `cadsandbox_test.go`, `main.go` against the merge-base) is empty.
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

None beyond the register. T-74-12 is mitigated by the removal table, the denied outside-file row, the failed negative control and the preflight denial (unit-proven and live-proven on the built image); T-74-13 by the `cadrender` crosscheck with its mutation proof.

## Notes

- A leftover stopped container named `octoconv-cad-worker` (exited before this plan ran) is not mine and was left alone. The partial emulated amd64 build left BuildKit cache behind (free disk 6.3 GiB afterwards); no image of mine remains.
- Re-running the live proof with the emulated amd64 check needs about 3 GiB more free disk than was available.

## Self-Check: PASSED

The evidence files, the SANDBOX-PATHS blocks and commits 552d85d, a9563a0, 1a4bdb7, fd329bd, 5142db6, 3c2c1ee, c40b54f and 2426844 exist and are ancestors of HEAD; the amd64 check is recorded as NOT RUN, not claimed.
