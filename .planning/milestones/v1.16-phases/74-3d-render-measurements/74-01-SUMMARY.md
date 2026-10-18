---
phase: 74-3d-render-measurements
plan: 01
subsystem: cad-engine
tags: [vtk, osmesa, landlock, render, docker, wave0]
status: complete
requires:
  - phase: 71-evaluation-gate
    provides: approved vtk 9.7.1 hash lock, keep-list prune, render path, pixelcheck
  - phase: 73-cad-class-convert-slice-confined
    provides: engine image, cad-mesh, sandbox-probe pattern, Landlock candidate set
provides:
  - pruned vtk 9.7.1 venv and libosmesa6 in the Dockerfile.cad-worker engine stage
  - cmd/cad-worker/helper/cad-render, a verbatim port of the 71 render path with a CADVERDICT last line
  - DERIVED cad_render_triangle_cap, cad_stl_ceiling_bytes, cad_frame_cost_2048_mib, cad_unit_factor_source, nist_ctc_05_factor
  - scripts/cad-render-probe.sh (tracer, identity, cost, units) and scripts/cad-unit-probe.py
affects: [74-02, 74-03, 74-04, 74-05, 74-07]
key-files:
  created:
    - cmd/cad-worker/requirements-vtk.txt
    - cmd/cad-worker/vtk_keeplist.py
    - cmd/cad-worker/vtk-keeplist.txt
    - cmd/cad-worker/helper/cad-render
    - scripts/cad-render-probe.sh
    - scripts/cad-unit-probe.py
    - .planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt
  modified:
    - Dockerfile.cad-worker
key-decisions:
  - "The triangle cap is 3,835,382 and the STL byte ceiling 190,840,832 (182 MiB), derived from a measured 2048 px frame cost of 104 MiB; the numbers in the evidence win over 3,806,000"
  - "No source for the unit factor qualified under the pre-registered rule: cad_unit_factor_source is none; the OCP scale on nist_ctc_05 is 25.4"
metrics:
  duration: single session
  completed: 2026-10-05
commits: 2
plan_head_before: 55c5e521fb60123e1502e32cf99d7ae6a8f20da8
plan_head_after: 1a17c18fe66e0f98bd22f0bbc05f52653d8f4ff3
actuals:
  tokens: 17500
  tasks: 3
  commits: 2
---

# Phase 74 Plan 01: Render engine in the production image and the two Wave 0 probes Summary

The pruned production engine image renders real STEP parts to pixel-accepted PNGs as nobody under a real Landlock ruleset, reproduces the 71 renders byte for byte on five parts, and the 2048 px frame cost and the inch-STEP scale are now measured evidence lines.

## Task 1 checkpoint (R-10, vtk legitimacy, blocking-human)

Answered by the orchestrator under the user's delegation, recorded verbatim: "approved: 71 renderer_user_approval approved_2026-10-04, hashes match". The 71 approval is `DECISION,key=renderer_user_approval,value=approved_2026-10-04` (71-DECISION line 45).

Checks run by the executor before the resume signal was treated as given:

- `scripts/cad-measure/requirements-vtk.txt`: sha256 8c3f2ef4f069d6e05d35d03a425ba53d03833885a18da4f99bf9cd3cf1185452, one package line `vtk==9.7.1`, two `--hash=sha256:` entries.
- The copied `cmd/cad-worker/requirements-vtk.txt` is byte-identical (`cmp`), and so are `vtk_keeplist.py` and `vtk-keeplist.txt`.
- The PyPI JSON for vtk 9.7.1 gives cp311 manylinux_2_28_aarch64 = c760e6f7a2cff2236fe4b4b58ce0c6433a7421c0e3443bd30586fd58a27a0268 and cp311 manylinux2014_x86_64 = d5d642e5f0cdb213e2eff5d6de93d0b783a0a05ecd968c15805fde65f6df9926. These are exactly the two hashes in the lock.

## What was built

- **Dockerfile.cad-worker.** The venv stage gets a second `--require-hashes --no-deps --only-binary` install of the vtk lock and the keep-list prune, with `/tmp/keeplist` removed in the same RUN. The engine stage adds `libosmesa6` to the apt line (nothing else), `/usr/local/bin/cad-render` at 0755, and six `vtkmodules` imports in the smoke. The Go build and final stages are untouched.
- **cad-render.** The 71 `render()` body with the identical vtkmodules imports, constants and call sequence; camera fit by bbox corners with a module-constant margin 0.06. Dropped: docstring, dump_maps, `--margin`, `--lp-threads`, the LP_NUM_THREADS write, timing fields and the json sidecar. fd 1 is duplicated and pointed at fd 2 before the imports, so VTK output cannot forge a verdict. Exits 0, 1, 2, 3, 64 as specified. The verdict line is `CADVERDICT` plus compact sorted JSON.
- **scripts/cad-render-probe.sh.** Modes tracer, identity, cost, units. It builds `--target engine` from a staged copy of only the needed files under `octoconv-cad-worker:engine74-<12 hex>`, refuses an emulated image, and removes every tag it built (none remain). Each mode rewrites only its own lines in the evidence file.
- **scripts/cad-unit-probe.py.** stdlib + OCP, one `UNIT_ROW` per file.

## Results (evidence/74-01-wave0.txt)

- **LOCK:** one line, `cmp=identical`, `result=ok`.
- **TRACER:** six confined renders of two real parts (units_inch.step, 12 triangles; LinearSlide-MGN15x-Block.step, 5944 triangles) at iso 1280 opaque, iso 1280 transparent, top 2048 opaque. All six are `result=ok`: pixelcheck single (and transparent) `ok=1`, verdict view/size/background equal the request, PNG width and height equal size. The negative control (the same STL at `/outside/in.stl`) gives `png=absent`, `verdict_ok_lines=0`, `control=unconfined_ok`, `result=denied`. `TRACER-OK`.
- **IDENTITY:** `parts=5,identical=5,different=0,failed=0`. libosmesa6 is 22.3.6-1+deb12u2 in both images.
- **COST:** eight rows (two files, 1280 and 2048, two reps), all `rc=0`, `pix=ok`, `oom_kill=0`. The heavy part (step-nauo n1000, 3,816,000 VTK triangles) peaks at 1752 MiB at 2048 px, well under 2867.2 MiB, so the stop condition did not trigger.
  - `cad_frame_cost_2048_mib` = 104 (heavy part: 1752 minus 1648; the light part's 1280 peak of 513 is a page-cache outlier of rep 1 and gives a negative difference, which the rule takes as the maximum over files).
  - `cad_render_triangle_cap` = floor((2867.2 - 517.0 - 24.9 - 104) / 579.16 x 1e6) = 3,835,382, under the 4,014,975 clamp.
  - `cad_stl_ceiling_bytes` = floor((3,835,382 x 50 + 84) / 1048576) x 1048576. By hand: 191,769,184 / 1,048,576 = 182.88, floor 182, 182 x 1,048,576 = 190,840,832.
- **UNITS:** control units_inch.step ratio 25.399999999999995 (25.4 within 1e-6, result=ok); nist_ctc_05 ratio 25.4 exactly, so `nist_ctc_05_factor` = 25.4; LM8UU and legacy_cp1251 ratio 1.0; nist-d2mi ratio 25.4707. Under the pre-registered rule no source qualifies, so `cad_unit_factor_source` = none.
  - `model_length_unit` reports 1.0 on every row, because cad-mesh pins `xstep.cascade.unit` to MM before the read, so it can never be the source.
  - `fileunits` gives 25.4 on three inch files but `unknown` on nist_ctc_05.
  - `text_unit` gives the right factor on every row; it fails only on d2mi, where the vertex-bbox ratio 25.4707 is a geometry effect (OCP vertex set against 130 VERTEX_POINTs), not a unit one.
  - The rule was not loosened after the fact. Plan 74-02 must read this as `none` per R-04, with the evidence above as the input for the orchestrator if a different rule should be adopted.
- **GAP:** `unit_ratio_mismatch` (d2mi), `unit_factor_source=none`, `iges_inch_scaling=unproven` (no inch IGES in the corpus, the 73 GAP).

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 3 - Blocking] The regenerated heavy STEP does not reproduce the 71 sha256**
- **Found during:** Task 3, cost mode
- **Issue:** `scripts/cad-corpus-gen.py --kind step-nauo --targets 1000` yields sha d5584bc5... (1,154,063 bytes) in both the 71 pruned image and the engine image, while the 71 ROW and the cached 71 file carry fc05817f... (1,154,281 bytes). The generator drifted since the 71 file was made; it is not an engine difference.
- **Fix:** the plan requires the 71 sha when the ROW records one, so the cost input is the cached 71 file (`$OCTOCONV_CAD_CACHE/scaled/...n1000.step`) whose sha equals the 71 ROW. The regenerated sha is kept in the cost HDR line as `regenerated_sha256`, with `regenerated_equals_71_row=0`.
- **Files modified:** scripts/cad-render-probe.sh
- **Commit:** 1a17c18

**2. [Rule 1 - Bug] The 5944 check compared STL records, not the VTK cell count**
- **Found during:** Task 3, cost mode
- **Issue:** the production STL of the light part has 5948 records; the 71 figure 5944 is the vtkSTLReader cell count, which is also what cad-render reports.
- **Fix:** the COST_ROW triangles field is the render verdict's VTK count (5944, asserted); `stl_records` is a separate field.
- **Commit:** 1a17c18

**3. [Rule 1 - Bug] cad-unit-probe set the unit pin before constructing the reader**
- **Found during:** Task 3, units mode (the control failed with `cannot pin xstep.cascade.unit`, which shows it can fail)
- **Fix:** construct the reader first, as cad-mesh does.
- **Commit:** 1a17c18

### Plan interpretation

- The acceptance line says four TRACER lines with `result=ok`; the action text says render each of the two STLs for three settings, which is six. Six were run and all six are ok, a superset of the four. The evidence SUMMARY records `runs_ok=6,runs_total=6`.
- cad-render wraps the vtkmodules imports in its try block, so an import failure also yields `failed/render_exception`; the plan said "after the imports". A non-finite diagonal is also reported as `zero_extent`. Neither changes the render path.
- The units probe adds `fileunits_factor`, `text_factor`, `vertices` and error fields to `UNIT_ROW`, and a `UNITS_CONTROL` line.

## Auth gates

None. The Task 1 checkpoint was answered under the user's delegation (above).

## Known Stubs

None.

## Threat Flags

None. cad-render opens one input file and writes one output file inside `/job`; the Landlock set is the candidate render set (ro /opt/venv, /usr/bin/python3.11, /usr/lib/python3.11, /usr/lib/aarch64-linux-gnu, exec /usr/local/bin/cad-render; rw /job), and the negative control shows a file outside every grant yields no PNG.

## Verification

- `go build ./...` and `go vet ./...` green with GOTOOLCHAIN=go1.26.4.
- `git diff main -- internal/convert/exec.go` is empty; no diff against the phase-73 merge-base in scripts/cad-measure, Dockerfile.cad-measure, internal/api, internal/worker, internal/queue, docker-compose.yml, deploy.
- `cmd/cad-worker/helper/cad-render` is mode 100755, line 1 `#!/opt/venv/bin/python -IB`; the vtkmodules import set equals that of cad_render.py; no OCP import; no comment lines beyond shebangs; `grep -c require-hashes Dockerfile.cad-worker` = 2; the engine apt line is `python3 libgl1 libx11-6 libosmesa6 ca-certificates`.
- STATE.md and ROADMAP.md untouched; render is not registered.

## Self-Check: PASSED

All seven created files exist; commits 44cdf78 and 1a17c18 are ancestors of HEAD and SSH-signed.
