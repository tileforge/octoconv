---
phase: 74-3d-render-measurements
plan: 04
subsystem: cad-engine
tags: [render, timeouts, classification, measurements, quality]
status: complete
requires:
  - phase: 74-02
    provides: cad-mesh quality, --measure and stats, buildCADMeasurements
  - phase: 74-03
    provides: CADOpts, JudgeCADPNG, ErrCADRenderRejected, OperationRender
provides:
  - CADRenderConverter (step, iges, stl to png) with CADMeasurementsPath, unregistered
  - one 886 s timeout budget across code, compose, chart and .env.example
  - quality wired from the convert job opts into cad-mesh --quality
  - render sentinels mapped in the worker with no new wire code
affects: [74-05, 74-06, 74-07]
key-files:
  created:
    - internal/convert/cadrender.go
    - internal/convert/cadrender_test.go
  modified:
    - internal/convert/cad.go
    - internal/convert/cadsandbox.go
    - internal/convert/cadsandbox_test.go
    - internal/convert/cad_test.go
    - internal/convert/sandboxcoverage_test.go
    - cmd/cad-worker/main.go
    - docker-compose.yml
    - deploy/chart/octoconv/values.yaml
    - deploy/chart/octoconv/templates/configmap.yaml
    - .env.example
    - internal/worker/descriptors.go
    - internal/worker/pinning_test.go
    - docs/ERROR-CODES.md
key-decisions:
  - "Render deadline 480 s is chosen under the 900 s reconciler window (1.10x the worst in-cap measured render, 437.36 s), not measured and not a 1.5x derivation; stacked inputs above about 40 MiB stay bounded only by it until phase 76"
  - "Option flags are appended after the positionals (cad-mesh --format F in out --quality Q --measure P) so the 73 stub helpers that read $4 as the output path keep working; the render helper keeps its flags first and positionals last"
  - "The measurements document is built from the draft before the render runs, so an invalid draft costs no render; the sidecar is written only after the PNG has passed the pixel judge"
commits: 3
plan_head_before: 28f5a77e9125a468a297caf721a206459b1cc46d
plan_head_after: 99c29eeaaff89e85fd63c7c3cfece7129062eedc
actuals:
  tokens: 16750
  tasks: 3
  commits: 3
requirements-completed: [RNDR-01, RNDR-04, RNDR-05]
---

# Phase 74 Plan 04: Render converter, timeout budget and worker classification Summary

CADRenderConverter exists behind an unregistered type with every judgment proven against stub helpers, the engine timeout is one 886 s budget everywhere, and quality now reaches cad-mesh on convert jobs.

## What was built

- **Timeouts (R-01).** `CADDefaultRenderTimeout` 480 s; `CADDefaultEngineTimeout` = 30 + 316 + 480 + 60 = 886 s. `ValidateCADTimeouts(guard, mesh, render, engine)` and `ConfinedCAD(..., render, engine)` refuse a configuration whose stages plus the I/O allowance exceed the engine timeout and name `CAD_RENDER_TIMEOUT`. `cmd/cad-worker` reads `CAD_RENDER_TIMEOUT`. Compose: every `CAD_ENGINE_TIMEOUT` is 886s, the cad-worker gains `CAD_RENDER_TIMEOUT: "480s"` and grace 906s. Chart: cad worker grace 906, configmap 886s plus the render line. `.env.example` states 886 as a four-term sum and labels 480 as chosen, not measured, with the stacked-input GAP. A test pins 886 < 900 and grace 906. `RECONCILER_ACTIVE_STALE_AFTER` is untouched; the one `.env.example` line that named it was reworded so no added line mentions it.
- **Caps and sentinels.** `CADMaxRenderTriangles` 3,835,382 and `CADMaxSTLBytes` 190,840,832 (test parses the 74-01 DERIVED lines); `cadMaxBytes("stl")`; `ErrCADRenderTimeout`, `ErrCADRenderFailed`, `ErrCADMeasurementsInvalid`; `cadVerdict` gains view, size, background, proj_aspect.
- **Classification.** For stage render: verdict empty is `ErrCADEmptyResult`; any other verdict, a non-SIGKILL signal, a plain error or exit 0 without a verdict is `ErrCADRenderFailed` (retryable, D-03); the stage deadline is `ErrCADRenderTimeout` with the engine CPU in the text; SIGKILL stays `ErrCADEngineKilled`. Guard and mesh behaviour is unchanged.
- **CADRenderConverter.** STEP/IGES: guard, mesh with `--quality` and `--measure`, STL re-judge and verdict match. STL: size ceiling then `cad-mesh stats --measure` (mesh set and mesh deadline), no guard. Then the draft is parsed (invalid is `ErrCADMeasurementsInvalid`), the triangle cap (cap passes, cap + 1 is `ErrCADTooComplex`) and zero extent (`ErrCADEmptyResult`) are checked before any render, the document is built, the render runs under its own deadline and `cadRenderReadOnly` (own slice seeded equal to the mesh paths, plan 74-06 measures it) with exec grant `cad-render`, the verdict's view, size and background must equal the request (`ErrCADRenderRejected`), the PNG goes through `JudgeCADPNG`, and the sidecar is written at `CADMeasurementsPath(out)` with mode 0600. Any failure removes the sidecar. Not registered in `convert.Default` or the worker.
- **Worker.** `isCADTerminal` lists render timeout, render rejected and measurements invalid, not render failed. `cadFailureCode`: render timeout is `cad_too_complex` "the model took too long to render; simplify it or export fewer details"; rejected is `cad_empty_result` "the rendered preview shows no recognisable part"; measurements invalid is `engine_error` "the measurements of this model could not be produced"; render failed is `engine_error` "the preview could not be rendered". No new code constant. `docs/ERROR-CODES.md` rows updated, code count unchanged.

## Mandatory additions from 74-03's notes

1. **Quality wired into convert (RNDR-04).** `CADConverter.Convert` resolves its options and passes `Resolved().Quality` (standard by default) to `meshStage`, which appends `--quality Q` to the cad-mesh argv. `TestCADConvertPassesQualityToTheMesher` checks the argv for default, draft and fine, and that a fine run yields 48 triangles against 12 for standard through a stub whose output depends on the quality argument. That a real fine tessellation is at least as dense as standard is the 74-02 live QUALITY evidence (LinearSlide 3988 <= 5948 <= 14140 triangles). Deviation: `internal/convert/sandboxcoverage_test.go` and the existing mesh-argv assertion in `cad_test.go` needed edits that are not on the plan's file list (see Deviations).
2. **Helper facts compared in the converter.** `renderStage` rejects a verdict whose view, size or background differs from the request with `ErrCADRenderRejected`; one subtest per field. The judge still does not do this.
3. **For 74-05.** Add the two HTTP render cases once render is registered: an invalid `view` (422 invalid_options) and an STL render carrying `quality` (422 opts_not_applicable). `TestCADRenderRefusesQualityOnAnStlSource` covers the second at the converter.

## Verification

- `GOTOOLCHAIN=go1.26.4`: `go build ./...`, `go vet ./...` and `go test ./... -count=1` all pass (no FAIL); `scripts/ci-helm-checks.sh` exits 0.
- 23 tests match `TestCADRender|TestCADPinned|TestCADSets` plus the quality and allowlist tests, all passing (the plan asked for at least 12); `TestPinnedCADFailureVerdicts` has 17 passing subtests (plan: at least 16); `TestDefaultRegistersOnlyAllowlistedCADPairs` passes.
- `git diff --exit-code main -- internal/convert/exec.go` is empty. No remaining `CAD_ENGINE_TIMEOUT` 406s in compose, chart or `.env.example`. No added `//` line in cad.go, cadrender.go or cadsandbox.go against the phase-73 merge-base; no added `Code...=` constant.
- All three commits are SSH-signed. STATE.md and ROADMAP.md were not touched by this plan (STATE.md was already modified in the working tree before it started and was not staged).

## Deviations from Plan

**1. [Rule 3 - Blocking] sandboxcoverage_test.go registers cadrender.go**
- **Found during:** Task 2 full package run
- **Issue:** `TestConfinedClassesExecOnlyThroughTheSandbox` refuses any class file that calls `runBounded` and is not classified, and then refuses `cad-render` as an ungranted engine.
- **Fix:** `cadrender.go` joins the cad class files and `cadRenderEngines` joins the cad engine union in the three places that list them. File not on the plan's list.
- **Commit:** e9e9984

**2. [Rule 1 - Bug] the 73 mesh argv assertion**
- **Issue:** `TestCADConvertPassesTheCapsAndPathsToTheStages` asserted the exact mesh argv without quality; convert now always passes `--quality standard`.
- **Fix:** the expected argv gains `--quality standard`. Same commit.

**3. [Rule 3] `.env.example` wording**
- The plan's own gate refuses any added line that mentions `RECONCILER_ACTIVE_STALE_AFTER`, and the existing derivation line did. The sentence now says "the stale-after window of the active-job reconciler". Content unchanged. Commit bc32c3c.

**4. [Plan interpretation] estimate_test.go and queue_test.go unchanged**
- Both read `CADDefaultEngineTimeout` by name, so no 406 or 426 literal remained to update; they pass against 886 s.

**5. [Plan interpretation] draft and caps ordering**
- The measurements document is built before the render so an invalid draft is terminal without paying for a render, and the sidecar is written after the pixel judge. The plan listed the build after the judge; the observable outcomes in the behaviour list are the same.

## Known Stubs

None.

## Threat Flags

None beyond the register. T-74-07 (stacked inputs) is accepted and bounded only by the 480 s render deadline until phase 76 (GAP in `.env.example`); T-74-08 by the view/size/background comparison and the independent pixel judge; T-74-09 by the 886 s < 900 s test and `ValidateCADTimeouts`.

## Notes for later plans

- 74-05 registers `CADRenderConverter` (operation render, three pairs) and adds the two HTTP render cases above.
- 74-06 measures `cadRenderReadOnly` (seeded equal to the mesh paths) and the real render wall at the 480 s deadline.
- The render helper's args are `--view V --size N --background B SRC DST`; the mesh helper's are `--format F SRC OUT [--quality Q] [--measure P]` and `stats --measure P SRC`.

## Self-Check: PASSED

cadrender.go and cadrender_test.go exist; commits bc32c3c, e9e9984 and 99c29ee are ancestors of HEAD.
