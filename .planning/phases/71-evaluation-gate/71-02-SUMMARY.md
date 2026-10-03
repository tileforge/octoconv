---
phase: 71-evaluation-gate
plan: 02
subsystem: infra
tags: [cad, measurement, cadquery-ocp, vtk, osmesa, landlock, ezdxf, corpus, generator]
status: complete

requires:
  - phase: 71-01
    provides: user-approved hash-pinned lock files, digest-pinned base image
provides:
  - Dockerfile.cad-measure (bookworm-slim by digest, hash-locked venv, USER nobody)
  - measurement helpers: measure_wrap, cad_mesh, cad_render, pixelcheck, common.sh
  - scripts/cad-rss-time-measure.sh (container per cell, ROW/HDR/SUMMARY lines, --confine-ro)
  - one real CC-BY-3.0 STEP with provenance
  - scripts/cad-corpus-gen.py and 18 deterministic synthetic fixtures with provenance
  - tracer evidence with the candidate LANDLOCK_RO set
affects: [71-03, 71-04, 71-05, 71-06, 71-07]

actuals:
  tokens: 197000
  tasks: 2
  commits: 2
plan_head_before: 39988a1f55b4cd96c5350c064fdd705a3bbe69ae
plan_head_after: d1e48976ca1a8fe6582b19b773350c65a077cb03

tech-stack:
  added: [cadquery-ocp-novtk 7.9.3.1.1, vtk 9.7.1, ezdxf 1.4.4, numpy 2.4.6, pillow 12.3.0 (all from the approved lock files, measurement image only)]
  patterns: [container per measurement cell, pixel judge instead of exit code, generator committed before the code that judges its output]

key-files:
  created:
    - Dockerfile.cad-measure
    - scripts/cad-measure/common.sh
    - scripts/cad-measure/measure_wrap.py
    - scripts/cad-measure/cad_mesh.py
    - scripts/cad-measure/cad_render.py
    - scripts/cad-measure/pixelcheck.py
    - scripts/cad-rss-time-measure.sh
    - scripts/cad-corpus-gen.py
    - internal/convert/testdata/cad/PROVENANCE.md
    - internal/convert/testdata/cad/public/LinearSlide-MGN15x-Block.step
    - internal/convert/testdata/cad/synthetic/ (18 files)
    - .planning/phases/71-evaluation-gate/evidence/71-02-tracer.txt
  modified: []

key-decisions:
  - "Tracer part is FreeCAD-library LinearSlide-MGN15x-Block.step (CC-BY-3.0, commit-pinned URL); every candidate checked was FreeCAD-exported, so the not-FreeCAD preference could not be met"
  - "Image build uses a staged context (Dockerfile, requirements, helper scripts) instead of the repository root, so the build context stays small"
  - "closed_solids means every shell of the solid passes BRep_Tool.IsClosed_s; IsClosed_s on the solid itself returned False for a valid closed solid"
  - "reversed_solid.step documents that OCP's STEP reader repairs inverted orientation (volume +6), so the helper must not expect a negative volume from it"

patterns-established:
  - "ROW lines print name only for files inside the repository or the corpus cache; private sources print name=- and never emit product text"
  - "A Landlock result counts only with the probe canary denied before the helper runs and a negative-control read denied"

requirements-completed: [EVAL-01, EVAL-02]
---

# Phase 71 Plan 02: Tracer slice and synthetic corpus generator Summary

One real STEP part runs through a native arm64 `debian:bookworm-slim` image as `nobody` (OCP read, volume, bbox, STL, VTK/OSMesa PNG, pixel judge, peak RSS and time at `--cpus=2 --memory=2g`) both unconfined and under a real Landlock ruleset, and 18 deterministic synthetic fixtures were generated and committed before any code that judges them.

## Accomplishments

- **Measurement image** `octoconv-cad-measure:ocp7.9.3.1.1-full-0e0672165074`: Config.User `nobody`, Architecture `arm64`, base by digest in both stages, three `--require-hashes` installs (OCP and vtk with `--no-deps`). Image size 1.42 GB uncompressed.
- **Tracer result** (`evidence/71-02-tracer.txt`): 1 solid, volume 17567.98 mm^3, bbox diagonal 72.69 mm, 5948 triangles; mesh 0.8-0.9 s wall, child RSS 271 MiB, cgroup peak 127-196 MiB; render 1.5-2.0 s wall, child RSS 313 MiB, cgroup peak 273-341 MiB; `oom_kill=0`; PIX `frac=0.2768 margin_px=78 dark=0.0046 ok=1`. The same numbers hold unconfined and confined.
- **Landlock**: `LANDLOCK_RO=/opt/venv:/opt/cad-measure:/usr/lib/python3.11:/lib:/usr/lib:/etc/ld.so.cache:/proc/self:/proc/cpuinfo:/proc/sys/vm/overcommit_memory:/sys/devices/system/cpu:/usr/bin/python3.11` with `PROBE_RW=/job`. A read of `/etc/passwd` under the same ruleset fails with `PermissionError`. This is a tracer candidate, not a grant; the full matrix is plan 71-07.
- **Generator**: two in-image runs are byte-identical and equal to the committed `synthetic/` directory (`GEN-DETERMINISTIC`); `--check-only` on the host (python3 stdlib, no OCP) passes on the committed directory and exits non-zero on a copy with `units_inch.step` replaced by `x` (`SELF-CHECK-FIRES`). Eleven further mutations (wrong inch factor, UTF-8 instead of cp1251, a `\X2\` escape, wrong `$DWGCODEPAGE`, a newline in the flat IGES, STL size and header, three NAUO instead of four, MINSERT counts, flipped orientation flags, a 998 group) were each rejected by a specific check.
- **Read-back at generation** (OCP and ezdxf) is recorded per file in `PROVENANCE.md`: inch STEP reads as INCH and scales to 50.8 x 76.2 x 25.4 mm; the assembly reads as 4 solids; ezdxf decodes both legacy DXF texts through `$DWGCODEPAGE`; MINSERT `(3, 3)` and one DIMENSION are seen.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Tracer: image, helpers, per-cell script, real part, Landlock evidence | bed3951 | Dockerfile.cad-measure, scripts/cad-measure/*, scripts/cad-rss-time-measure.sh, testdata/cad/public, PROVENANCE.md, evidence/71-02-tracer.txt |
| 2 | Deterministic synthetic fixture generator and output | d1e4897 | scripts/cad-corpus-gen.py, testdata/cad/synthetic/*, PROVENANCE.md |

`commits: 2` is measured with `git rev-list --count 39988a1..d1e4897` and excludes the SUMMARY commit. The generator commit precedes any 71-04 change to `scripts/cad-measure/cad_mesh.py`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Feature edges rendered green and surfaces dark**
- **Found during:** Task 1 (first render and PIX `ok=0`, dark 0.0006)
- **Issue:** `vtkFeatureEdges` colours edges by scalar by default, so the dark edge colour was ignored; surfaces without normals shaded to about half the intended grey.
- **Fix:** `ColoringOff()`, `ScalarVisibilityOff()`, `vtkPolyDataNormals` with a 30 degree split, ambient 0.45 / diffuse 0.55, line width 2.0. PIX then passed (dark 0.0046).
- **Files modified:** scripts/cad-measure/cad_render.py
- **Commit:** bed3951

**2. [Rule 1 - Bug] OCP `FileUnits` and solid closedness**
- **Found during:** Task 1
- **Issue:** `reader.FileUnits()` takes three `TColStd_SequenceOfAsciiString` arguments in this OCP build (the no-argument call raised `TypeError`); `BRep_Tool.IsClosed_s(solid)` is False for a valid closed solid, giving `closed_solids:0`.
- **Fix:** pass the sequences; `closed_solids` counts solids whose shells all pass `IsClosed_s`.
- **Files modified:** scripts/cad-measure/cad_mesh.py
- **Commit:** bed3951

**3. [Rule 3 - Blocking] Staged image build context**
- **Issue:** the repository root as build context sends frontend and binaries to the daemon for an image that needs six files.
- **Fix:** `cad_build_image` stages `Dockerfile.cad-measure`, the requirement files and `scripts/cad-measure/*.py` in a temporary directory. `--only-binary=:all:` was added to the pip installs.
- **Files modified:** scripts/cad-measure/common.sh, Dockerfile.cad-measure
- **Commit:** bed3951

**4. [Rule 3 - Blocking] Bind-mount flake guard in the generator**
- **Issue:** OrbStack bind mounts of host directories created moments earlier intermittently failed (about 4 of 14 runs): the directory was missing or `os.makedirs(..., exist_ok=True)` raised `FileExistsError`, and once `cad_mesh.py` reported "STEP read failed" on a fresh job directory.
- **Fix:** the generator tolerates that `FileExistsError` (one comment, an external-environment constraint). The measurement script is not guarded: a failed cell shows as a row with `rc!=0` or as a missing WRAP line (exit 1), never as a silent success.
- **Files modified:** scripts/cad-corpus-gen.py
- **Commit:** d1e4897

### Other notes

- **Tracer part exporter.** All FreeCAD-library candidates checked (about ten) are FreeCAD-exported (`Open CASCADE STEP processor 6.7-7.6`, system `FreeCAD`); none is from another CAD. `LinearSlide-MGN15x-Block.step` was chosen because its header carries no personal path. Author is the git author `k2-mirror`; per-part authorship is not verified by the repository (A8).
- **Plan wording on null-volume fixtures did not hold.** OCP's STEP reader repairs inverted orientation (flipping ADVANCED_FACE flags, FACE_BOUND flags or both on a plain box all read back as volume +6), and a MANIFOLD_SOLID_BREP over a CLOSED_SHELL with a face removed reads as 0 solids, the same as an OPEN_SHELL. `open_shell.step` is therefore an OPEN_SHELL (0 solids) and `reversed_solid.step` records that no negative volume appears; `PROVENANCE.md` says so for plan 71-04.
- **Extra output lines.** `PIX,...` lines (the pixel judge's raw measurement) and `HDR,confine_ro=...` are emitted in addition to the specified HDR/ROW/SUMMARY lines.
- **Memory instruments disagree on small inputs.** `cg_peak_mib` (cgroup `memory.peak`) is below `child_rss_mib` (`ru_maxrss`) for mesh (127 vs 271 MiB) because mapped library pages are charged to the cgroup that first loaded them; both are recorded and the larger is the one to use for headroom (noted in the evidence header).
- **Trace over-reports.** The openat trace listed `/etc/passwd`, `/etc/nsswitch.conf`, `/etc/localtime`, `/usr/share/zoneinfo`, `/usr/lib/locale` and `/usr/lib/aarch64-linux-gnu/gconv`; the confined run succeeded without granting them. Plan 71-07 should narrow rather than copy the trace.
- **Probe toolchain.** The probe is built with the host Go (go1.27.1 darwin/arm64 cross-compiled, recorded as `HDR,go=`); `go.mod` pins 1.26.4. The probe is a measuring tool, not a shipped binary, and its behaviour (canary denied, ruleset applied) is checked at every run.
- **Stale image tags.** Four `octoconv-cad-measure` tags exist locally from helper-script edits (the current one is `...-0e0672165074`); they share layers and were left in place per the no-deletion instruction. Free disk went from 20 to 18 GiB.
- **Private-source control.** A copy of the part outside the repository and the cache printed `src=private,name=-`, and neither the copy's file name nor the product text appeared in the output file.

## Known Stubs

None. `cad_mesh.py` handles STEP only and `cad_render.py` only the `iso` view by design of this tracer plan; the rss script prints `SKIP` lines for IGES/DXF and mesh-on-STL rather than a result. Plan 71-04 owns the remaining formats and views.

## Threat Flags

None. The surfaces added (a rw job directory mounted into the container, a mounted Landlock probe binary) are the ones in the plan's threat model (T-71-03, T-71-04, T-71-05, T-71-SC): `--network none`, `--cpus`, `--memory` equal to `--memory-swap`, a budget with SIGKILL to the process group, input mounted read-only, installs only through the approved hash locks.

## Self-Check: PASSED

- Dockerfile.cad-measure, scripts/cad-measure/{common.sh,measure_wrap.py,cad_mesh.py,cad_render.py,pixelcheck.py}, scripts/cad-rss-time-measure.sh, scripts/cad-corpus-gen.py: present
- internal/convert/testdata/cad/PROVENANCE.md, public/ (1 file), synthetic/ (18 files), evidence/71-02-tracer.txt: present
- commits bed3951 and d1e4897: present on gsd/phase-71-evaluation-gate
- tracer gate printed `TRACER-OK`; generator gate printed `GEN-DETERMINISTIC`; mutation gate printed `SELF-CHECK-FIRES`
- `git diff --name-only 4e595c0 -- internal cmd deploy docker-compose.yml | grep -v '^internal/convert/testdata/cad/'` is empty
- `grep -nE 'ResetCamera\(\)' scripts/cad-measure/cad_render.py` finds nothing
