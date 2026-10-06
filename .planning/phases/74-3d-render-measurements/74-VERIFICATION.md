---
phase: 74-3d-render-measurements
verified: 2026-10-06T17:45:35Z
status: passed
score: 9/9 must-haves verified
covered_files: [".env.example",".planning/ci-local/ci-local-20261005T214321Z.log",".planning/phases/74-3d-render-measurements/74-01-PLAN.md",".planning/phases/74-3d-render-measurements/74-01-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-02-PLAN.md",".planning/phases/74-3d-render-measurements/74-02-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-03-PLAN.md",".planning/phases/74-3d-render-measurements/74-03-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-04-PLAN.md",".planning/phases/74-3d-render-measurements/74-04-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-05-PLAN.md",".planning/phases/74-3d-render-measurements/74-05-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-06-PLAN.md",".planning/phases/74-3d-render-measurements/74-06-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-07-PLAN.md",".planning/phases/74-3d-render-measurements/74-07-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-CONTEXT.md",".planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt",".planning/phases/74-3d-render-measurements/evidence/74-02-mesh.txt",".planning/phases/74-3d-render-measurements/evidence/74-05-addoutputs-live.txt",".planning/phases/74-3d-render-measurements/evidence/74-06-judge.txt",".planning/phases/74-3d-render-measurements/evidence/74-06-landlock.txt",".planning/phases/74-3d-render-measurements/evidence/74-06-sandbox-liveproof.txt",".planning/phases/74-3d-render-measurements/evidence/74-07-liveproof.txt",".planning/research/SANDBOX-PATHS.md","Dockerfile.cad-worker","cmd/cad-worker/helper/cad-mesh","cmd/cad-worker/helper/cad-render","cmd/cad-worker/main.go","cmd/cad-worker/requirements-vtk.txt","cmd/cad-worker/vtk-keeplist.txt","cmd/cad-worker/vtk_keeplist.py","deploy/chart/octoconv/templates/configmap.yaml","deploy/chart/octoconv/values.yaml","docker-compose.yml","docs/ERROR-CODES.md","docs/MEASUREMENTS.md","internal/api/api.go","internal/api/cad_ceiling_test.go","internal/api/cad_opts_test.go","internal/api/formats_handlers_test.go","internal/api/handlers.go","internal/api/handlers_test.go","internal/api/pre58_pinning_test.go","internal/convert/cad.go","internal/convert/cad_test.go","internal/convert/cadmeasure.go","internal/convert/cadmeasure_test.go","internal/convert/cadopts.go","internal/convert/cadopts_test.go","internal/convert/cadpixel.go","internal/convert/cadpixel_test.go","internal/convert/cadrender.go","internal/convert/cadrender_test.go","internal/convert/cadsandbox.go","internal/convert/cadsandbox_test.go","internal/convert/convert.go","internal/convert/convert_test.go","internal/convert/converters.go","internal/convert/operations_test.go","internal/convert/sandboxcoverage_test.go","internal/jobs/add_outputs_test.go","internal/jobs/repo.go","internal/measurements/consistency_test.go","internal/measurements/measurements.go","internal/worker/cad_outputs_test.go","internal/worker/descriptors.go","internal/worker/pinning_test.go","internal/worker/probe_sideoutput_test.go","internal/worker/runner.go","internal/worker/webhook_role_test.go","internal/worker/worker.go","internal/worker/worker_test.go","scripts/cad-liveproof.sh","scripts/cad-render-probe.sh","scripts/cad-unit-probe.py","scripts/cad-worker-sandbox-measure.sh","scripts/cmd/cad-measurements-check/main.go","scripts/cmd/cad-pixel-judge/main.go","scripts/conf05-crosscheck.sh"]
covered_digest: "v3:sha256:098057508d15647783cbe776da9016deb31ad401325990fd089891d7470006aa"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 8/9
  previous_verified: 2026-10-05T22:05:09Z
  gaps_closed:
    - "The final cad-worker image (now carrying vtk 9.7.1 and libosmesa6) builds for linux/amd64 and, run emulated, refuses to start without Landlock (the emulated-amd64-refuses check of scripts/sandbox-liveproof.sh cad, which phase 73 ran and passed on its image)"
  gaps_remaining: []
  regressions: []
  advisory_closed:
    - "Live-DB run of TestAddOutputs* is now a committed transcript (evidence/74-05-addoutputs-live.txt, 3 PASS, no SKIP)"
    - ".env.example rationale block removed (77fa7cf)"
    - "Stale test name TestCreateJobCADTakesNoOptions renamed (e60f49c)"
gaps: []
deferred: []
coincidental_reliance_items:
  - truth: "SC4 units: STEP/IGES report the declared unit, numbers converted back from mm"
    reason: undeclared-precondition
    harden: "The factor used to convert back is the table value of the unit the helper's own regex reads from the STEP text (cad-mesh applied_factor), not a factor measured at run time; cadFactorMatches in cadmeasure.go therefore compares the table with itself for STEP. The result is correct only while OCP resolves the same unit from the same file. The Wave-0 probe and the live ctc_05 extents (19 x 20.75 x 22 inch, round numbers) show agreement on five STEP files. To harden it, cross-check the text unit against OCP's own unit reading where OCP reports one, and report unspecified/none on disagreement. A file that defines a CONVERSION_BASED_UNIT named INCH with a non-25.4 conversion is the case the text reading would get wrong."
---

# Phase 74: 3D Render & Measurements Verification Report

**Phase goal:** Клиент получает PNG-превью детали и вместе с ним честные размеры; два выхода одной джобы видны оба или не виден ни один.
**Verified:** 2026-10-06T17:45:35Z on branch `gsd/phase-74-3d-render-measurements`, HEAD `720cb93` (base `main`)
**Status:** passed
**Re-verification:** Yes. The previous run (2026-10-05T22:05:09Z, HEAD `01be894`) was gaps_found 8/9. This run follows the gap closure.

## Goal Achievement

The client-facing goal is achieved. The render and measurements path is proven live on `d93f6db`. The only product-tree changes since then are a 4-line comment removal in `.env.example` and a test rename in `internal/api/handlers_test.go`. The one open gap is now closed: the final `Dockerfile.cad-worker` builds for linux/amd64, including the hash-locked vtk wheel, the keep-list prune, libosmesa6 and the `vtkmodules` import smoke. Run emulated, that image refuses to start without Landlock.

### Re-verification of the gap (truth 9)

`evidence/74-06-sandbox-liveproof.txt` (committed in `720cb93`) is the raw output of `scripts/sandbox-liveproof.sh cad`:

- `commit: 8c6a6c41d346…`, `tracked dirt: 1 file(s)`. Native image `sha256:91dfe431…`.
- Results: PASS confined-startup (the worker logs `cad sandbox proven: cad-guard, cad-mesh and cad-render ran inside the job directory and were denied a file outside it`), PASS refuses-without-landlock, PASS escape-hatch, PASS preflight-catches-restrict-self.
- `emulated image id: sha256:70c52290… (amd64)`. `PASS: cad emulated-amd64-refuses` with the refusal line `landlock is unavailable on this kernel …`.
- The run ends with `ALL PASS (5 checks)`. There is no NOT RUN line.

**Is the transcript from the current code?**
- `git diff --stat 8c6a6c4 720cb93` changes only the evidence file, so the product tree is identical to HEAD.
- `8c6a6c4` itself adds only `.planning/HANDOFF.json` and `.continue-here.md`.
- `git diff --stat d93f6db 8c6a6c4 -- . ':!.planning'` shows only `.env.example` (4 comment lines removed) and `internal/api/handlers_test.go` (one test renamed). Neither can change the cad image. The final stage copies only `/out/cad-worker`, which `go build ./cmd/cad-worker` produces, and `_test.go` files are never compiled into it. `.env.example` is not read by the build. The only non-test `go:embed` directives are the migrations, `testdata/cad/synthetic/units_inch.step` and `texresolvable.txt`, and none of them changed.
- The one dirty tracked file is `.planning/STATE.md`, which the commit message names. It is still the only modification in the working tree. It sits under `.planning/`, and no build input reads it.

**Does the check prove the build, not just the refusal?** Yes:
- `check_emulated_amd64_refuses` runs `docker build --platform linux/amd64 -f Dockerfile.cad-worker` on the same Dockerfile and calls `die` if that build fails.
- The engine stage ends with a `RUN … import … vtkmodules.vtkRenderingOpenGL2` smoke. An amd64 build therefore cannot complete unless the x86_64 vtk wheel hash installs and the pruned VTK still imports.
- The script fails the check if the emulated output contains 🔒.

**Verdict:** ✓ VERIFIED.

One limit stays in scope as before: every Landlock measurement is native arm64. The amd64 evidence covers build, import and refusal. It does not cover a confined amd64 render, and CONTEXT defers that, as recorded in SANDBOX-PATHS "Ещё не замерено".

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1: `render` step/stp/iges/stl → png gives a non-empty, pixel-judged part image. The default is iso, 1280, opaque. Different views give different images, with Z up. | ✓ VERIFIED (regression ok) | 74-07-liveproof `SUMMARY,result=ALL PASS … render=5,views=6`, taken on d93f6db. The product code has not changed since, except for comments and a test name. The full unit suite passes on go1.26.4. Initial evidence: judge ok, 1280x1280, default sha equals the iso sha, front h/w 0.502 and top h/w 1.500 (Z up). |
| 2 | SC2: closed options give 422. `quality` changes tessellation for convert and render. `transparent` gives a fully transparent background. | ✓ VERIFIED (regression ok) | Live `opts_reject=9`, `quality=1` (draft 3988 ≤ standard 5948 ≤ fine 14140), transparent alpha=1 with background fraction + part fraction = 1.0. The cadopts and cadrender unit tests pass. |
| 3 | SC3/MEAS-01 (3D half): two outputs, written atomically. The inversion test can fail against the naive sequence. `download_url` points to the PNG. | ✓ VERIFIED | `Repo.AddOutputs` runs in one `pgx.BeginFunc`. `process()` validates, uploads, calls `AddOutputs` once, then `MarkDone`. **New:** evidence/74-05-addoutputs-live.txt (commit 1950f68, tracked dirt 0, PostgreSQL 18.4) records PASS for `TestAddOutputsWritesBothRowsInOneTransaction`, `TestAddOutputsRollsBackWhenTheSecondRowFails` (which holds the naive two-call control) and `TestAddOutputsRejectsEmptyInputWithoutWriting`, with go test exit 0 and no SKIP. `git diff 1950f68 HEAD -- internal/ cmd/` shows only the handlers_test rename, so repo.go and add_outputs_test.go are unchanged since that run. Live: every render reports `roles=primary@0;measurements@1`. |
| 4 | SC4/MEAS-02..04: null with a reason, never 0. Unit by source. `assume_unit` never overrides a declared unit. IGES inch is a recorded GAP. | ✓ VERIFIED (coincidental-reliance) | Unchanged from the initial run. Live `meas=6`: units_inch stays inch even with `assume_unit=mm`, STL gives unspecified/none or client_assumed, surfaces-only IGES gives `no_solids`, open STL gives `stl_not_watertight`. The advisory on the STEP text-unit factor still stands; see coincidental_reliance_items. |
| 5 | SC5 (extends CADW-02 and CADW-03 for the render stage): the render stage has no OCP and runs under its own measured ruleset, matched across SANDBOX-PATHS, Go and conf05. Preflight judges by pixels. Render failures are retryable, a render timeout is terminal, mesh failures are terminal. A kill leaves no live process. | ✓ VERIFIED | Re-run now: `scripts/conf05-crosscheck.sh cad` gives MATCH for cadguard (2/1), cadmesh (3/1), cadrender (3/1) and multiarch (2), exit 0. The sandbox liveproof on 8c6a6c4 shows the preflight proven line covering cad-render, plus refuse/escape-hatch/restrict-self on the final image, now also on amd64. Live `render_retry=1`, `mesh_signal=1`, `cancel_render=1` (pid gone in 0.3 s). The pinning_test and cadsandbox_test packages pass. |
| 6 | Limits: engine timeout 886 s, under the 900 s reconciler window. Triangle cap 3,835,382, derived from Wave-0. | ✓ VERIFIED (regression ok) | The commit 77fa7cf removed only rationale comments. `CAD_RENDER_TIMEOUT` and the 886 values did not change. The cad_test, cadsandbox_test and cadrender_test pins pass. |
| 7 | Supply chain: the only additions are vtk from the 71 lock and libosmesa6. | ✓ VERIFIED | Re-run: `cmp` says `requirements-vtk.txt`, `vtk_keeplist.py` and `vtk-keeplist.txt` are byte-identical to `scripts/cad-measure/`. The Dockerfile is unchanged since the initial run. |
| 8 | Invariants: `exec.go` unchanged, no new code comments, goldens and metric labels unchanged. | ✓ VERIFIED | Re-run: `git diff main...HEAD` is empty for exec.go, scripts/cad-measure, Dockerfile.cad-measure and cad-guard. The gap-closure commits add no comments; 77fa7cf removes 4. |
| 9 | The final engine image builds for amd64 and refuses to run unconfined there (emulated-amd64-refuses). | ✓ VERIFIED (gap closed) | See "Re-verification of the gap" above: `ALL PASS (5 checks)`, taken on 8c6a6c4, whose product tree equals HEAD's. |

**Score:** 9/9 truths verified (0 present-but-behavior-unverified)

### Decisions recorded

These are unchanged from the initial verification and still hold:
- **D-06, the J1 thin-frame relaxation.** It is bounded and evidenced in 74-06-judge.txt: 49 frames, 0 unexplained divergences.
- **The cad class serves both convert and render.** The exception is enforced by name and by count in operations_test and convert_test.
- **`cad_unit_factor_source = text_unit`.** The override is honest and evidenced, with a residual precondition recorded under coincidental_reliance_items.

### Per-stage "extends …" criteria

- **CADW-02 and CADW-03 (render stage) are extended by this phase (SC5).** Truth 5 verifies them: the render ruleset is measured and crosschecked, the worker refuses to start without Landlock (now on arm64 and on emulated amd64), the preflight runs cad-render inside and outside the job directory, render-stage errors are classified, and a cancel during render leaves no live process.
- **MEAS-01 and MEAS-03 (DXF half) are extended by Phase 75 SC2,** which says "вторым выходом идёт `measurements` с 2D bbox … единицы по `$INSUNITS` или `unspecified` (расширяет MEAS-01, MEAS-03)". The REQUIREMENTS.md note says that these extended criteria "проверяются верификатором расширяющей фазы". For Phase 74 the 3D halves are satisfied: two outputs for STEP/STP/IGES/STL render, and units from the declared STEP/IGES unit or `unspecified` for STL. The DXF half is not a gap here.

### Deferred Items

None. The DXF parts of MEAS-01 and MEAS-03 are owned by Phase 75 by the requirements mapping. They are not gaps of this phase.

### Advisory (New Scope, Unevidenced)

| # | Finding | Category | Why Advisory |
|---|---------|----------|--------------|
| 1 | `applied_factor` for STEP is the table value of the text unit, so `cadFactorMatches` compares the table with itself for STEP (carried forward from the initial run). | other | It is documented truthfully in docs/MEASUREMENTS.md. No failing file or test demonstrates a wrong unit. See coincidental_reliance_items. |

The previous advisory on the live AddOutputs DB run is closed by evidence/74-05-addoutputs-live.txt.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/cad-worker/helper/cad-render` | confined VTK render, CADVERDICT | ✓ VERIFIED | Unchanged since the initial run |
| `cmd/cad-worker/helper/cad-mesh` | `--quality`, `--measure`, `stats` | ✓ VERIFIED | Unchanged |
| `Dockerfile.cad-worker` | hash-locked vtk, prune, libosmesa6 | ✓ VERIFIED (arm64 and amd64 build) | The amd64 image `sha256:70c52290…` was built from this file in the 8c6a6c4 run |
| `internal/convert/cadopts.go`, `cadpixel.go`, `cadrender.go`, `cadmeasure.go` | options, judge, converter, measurements | ✓ VERIFIED | Unchanged; tests pass |
| `internal/jobs/repo.go` `AddOutputs` | one transaction | ✓ VERIFIED | The live-DB transcript is now committed |
| `internal/worker/descriptors.go` | `cadSideOutputs`, render sentinels | ✓ VERIFIED | Unchanged |
| `.planning/research/SANDBOX-PATHS.md` | `conf05:cadrender` blocks | ✓ VERIFIED | conf05 MATCH, re-run, exit 0 |
| evidence/74-0* | wave0, mesh, addoutputs-live, judge, landlock, sandbox, liveproof | ✓ VERIFIED | The sandbox-liveproof is now ALL PASS 5/5 |

### Key Link Verification

All links from the initial verification are unchanged in code, because `git diff 1950f68 HEAD -- internal/ cmd/` contains only the test rename. They remain WIRED: handlers → ParseCADOpts/ValidateCADApplicability, worker → CADOptsFromMap, cadrender → JudgeCADPNG/buildCADMeasurements, process() → AddOutputs, cad-worker main → CADRenderConverter, SANDBOX-PATHS ↔ cadsandbox.go (conf05 re-run), wave0 DERIVED → cad.go constants.

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| measurements output | bbox, solids, volume, area, unit | cad-mesh → `buildCADMeasurements` → sidecar → `cadSideOutputs` → S3 + `AddOutputs` | yes (live MEAS rows) | ✓ FLOWING |
| PNG primary | rendered frame | cad-render under Landlock → `JudgeCADPNG` → upload | yes (live judge on downloaded bytes) | ✓ FLOWING |

### Behavioral Spot-Checks (this run, GOTOOLCHAIN=go1.26.4)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build | `go build ./...` | ok (go1.26.4 darwin/arm64) | ✓ PASS |
| Vet | `go vet ./...` | ok | ✓ PASS |
| Formatting | `gofmt -l .` | empty | ✓ PASS |
| Unit tests | `go test -count=1 ./...` (one run; the six packages cut off from the captured tail were re-run on their own) | every package ok, internal/convert 162.5 s, no FAIL | ✓ PASS |
| Renamed test | `go test -run TestCreateJobCADRejectsOtherEnginesOptions -v ./internal/api/` | PASS | ✓ PASS |
| Landlock sets match the doc | `scripts/conf05-crosscheck.sh cad` | 4 MATCH, exit 0 | ✓ PASS |
| vtk lock and keep-list identity | `cmp scripts/cad-measure/* cmd/cad-worker/*` | 3/3 identical | ✓ PASS |
| Invariant files | `git diff main...HEAD -- internal/convert/exec.go scripts/cad-measure Dockerfile.cad-measure cmd/cad-worker/helper/cad-guard` | empty | ✓ PASS |
| Liveproof on the current product tree | `git diff --stat d93f6db HEAD -- . ':!.planning'` and `git diff --stat 8c6a6c4 HEAD -- . ':!.planning'` | comment and test-name changes only; empty | ✓ PASS |

The compose-backed live suites were not re-run. Their committed transcripts were taken on product trees that equal HEAD, except for comments and a test name.

### Probe Execution

| Probe | Result | Taken on | Status |
|-------|--------|----------|--------|
| `scripts/sandbox-liveproof.sh cad` → 74-06-sandbox-liveproof.txt | `ALL PASS (5 checks)` incl. emulated-amd64-refuses | 8c6a6c4 (product tree = HEAD), dirt = `.planning/STATE.md` | PASS |
| `scripts/cad-liveproof.sh` → 74-07-liveproof.txt | `SUMMARY,result=ALL PASS` | d93f6db, tracked dirt 0 | PASS |
| `go test -run TestAddOutputs ./internal/jobs/` against live PG → 74-05-addoutputs-live.txt | 3 PASS, exit 0, no SKIP | 1950f68, tracked dirt 0 | PASS |
| `scripts/cad-worker-sandbox-measure.sh` → 74-06-landlock.txt | `MEASURED,stage=render…`, leaked 0 | unchanged since the initial run | PASS |
| `scripts/cad-render-probe.sh judge` → 74-06-judge.txt | 0 unexplained | unchanged | PASS |

No docker probe was re-run by this verifier. Each transcript was checked against git history to confirm it reflects the current product tree.

### Requirements Coverage

| Requirement | Source plan | Status | Evidence |
|-------------|-------------|--------|----------|
| RNDR-01 | 74-01, 04, 05, 06, 07 | ✓ SATISFIED | Truth 1; the image now also builds for amd64 (truth 9) |
| RNDR-02 | 74-03, 07 | ✓ SATISFIED | Truth 1, truth 2 |
| RNDR-03 | 74-01, 03, 07 | ✓ SATISFIED | Truth 2 |
| RNDR-04 | 74-02, 03, 04, 07 | ✓ SATISFIED | Truth 2 |
| RNDR-05 | 74-03, 04, 06, 07 | ✓ SATISFIED | Truth 2 |
| MEAS-01 | 74-05, 07 | ✓ SATISFIED for 3D | Truth 3; the DXF half is extended by Phase 75 SC2 |
| MEAS-02 | 74-02, 07 | ✓ SATISFIED | Truth 4 |
| MEAS-03 | 74-01, 02, 07 | ✓ SATISFIED for STEP/IGES/STL | Truth 4; the DXF half is extended by Phase 75 SC2; IGES non-mm gives unspecified (recorded GAP) |
| MEAS-04 | 74-02, 03, 07 | ✓ SATISFIED | Truth 4 |

There are no orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (changed files) | | TBD, FIXME or XXX | | 0 real markers. The only matches are `mktemp …XXXXXX` templates in scripts/cad-render-probe.sh and scripts/cad-worker-sandbox-measure.sh. |
| `.planning/phases/74-3d-render-measurements/.continue-here.md`, `.planning/HANDOFF.json` | | pause-work handoff files from 8c6a6c4 still tracked | ℹ️ Info | Planning hygiene only. The resume flow or the orchestrator should remove them before merge. |

The previous Info items are resolved: the `.env.example` rationale was removed in 77fa7cf, and the stale test name was fixed in e60f49c.

### Human Verification Required

None.

### Gaps Summary

There are no gaps. The one gap from the initial verification, the emulated-amd64 build and refusal check, is closed by an ALL PASS 5/5 transcript taken on a commit whose product tree equals HEAD. The regression checks on the eight previously passing truths are clean: build, vet, gofmt, the full unit suite on go1.26.4, the conf05 crosscheck, lock identity and the invariant diffs. The former advisory on the live AddOutputs DB run is closed by a committed transcript. One advisory remains, the STEP text-unit factor precondition. It is non-blocking.

---

_Verified: 2026-10-06T17:45:35Z_
_Verifier: Claude (gsd-verifier)_
