# CAD Preview Dimensions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add default-on, optional overall dimension annotations to 3D CAD PNG previews.

**Architecture:** Keep the existing VTK render and pixel judge. Compose the approved PNG in Go with
projected bounding-box dimension lines and labels derived from the measurements document.

**Tech Stack:** Go, image/png, existing golang.org/x/image drawing and font packages, existing VTK helper.

**Spec:** docs/superpowers/specs/2026-10-31-cad-preview-dimensions-design.md

## Global Constraints

- `dimensions` is a strict boolean, defaults to true, and applies only to STEP/IGES/STL render jobs.
- False preserves the original PNG bytes. No measurements schema change or implicit STL unit.
- Preserve 256..2048 square frames, background alpha, and validation before annotation.
- Work on `codex/cad-preview-dimensions`; preserve unrelated untracked user files.
- At most five review/fix iterations; one independent final review is planned.

## Review Focus

- Explicit false must survive API normalization and persisted options.
- Thin and planar models must have readable labels and no invented visible third axis.
- Unknown or assumed units must not be mislabeled as declared millimetres.
- Off-origin models and differing measurement/mesh units must project without a scale mismatch.
- Blank PNGs and irregular output files must still fail before labels are added.

### Task 1: Option, annotations and pipeline

**Files:** internal/convert/cadopts.go, cadrender.go, new caddimensions.go and corresponding tests;
internal/api/cad_opts_test.go; go.mod.

**Interfaces:** `CADOpts.Dimensions *bool`; `cadDimensionImage(image.Image, stlBounds,
measurements.Document, CADOpts) (image.Image, error)` composes an already-validated frame.

- [x] Add failing tests for strict booleans, applicability, API round-trip and default/false render behavior.
- [x] Run focused Go tests and verify missing-option/default-annotation failures.
- [x] Add option handling and the compositor, retaining raw mesh bounds separately from display measurements.
- [x] Add tests for projection, numerical labels, all views, degenerate axes, small frames and alpha.
- [x] Run package checks; convert and presets pass in the full Go 1.26.4 run, API passes across the initial run and its remaining-test continuation.

### Task 2: Contract, real-render verification and review

**Files:** docs/CAD.md, PREVIEW.md, MEASUREMENTS.md, ERROR-CODES.md, openapi.yaml;
internal/mcpserver/tools.go; existing doc contract tests.

**Interfaces:** Updated documented CAD options and annotation semantics, no new outputs or stages.

- [x] Update docs and doc contract tests for the boolean option, default change and composition framing.
- [x] Generate and visually inspect a real CAD preview, including a transparent and small-size case.
- [x] Run `go test ./... -timeout 10m`, `go vet ./...`, `go build ./...` and focused race tests; complete the API tests separately after the aggregate timeout (below).
- [x] Obtain one independent review, address material findings and rerun relevant checks.

## Execution record

- Decision: interpret “yes to both” as default true plus an explicit false opt-out; stated to the user.
- Decision: use a separate branch in the supplied checkout so changes remain directly reviewable there.
- Focused dimensions tests first failed on the unknown option and unchanged default PNG, then passed.
- Real local CAD-container proof: generated a 15 × 60 × 125 mm STEP bracket; inspected the 1280 px opaque
  and 256 px transparent annotated PNGs, with values matching the measurements document.
- Independent review (iteration 1) found one material issue: short-axis labels overlapped on a long rod.
  The pixel-overwrite regression failed before the fix and passed after adding collision-aware placement.
  The 100000 × 100000 × 10000000 mm real-render proof also shows separated labels at 256 px.
- The first broader run found the preview-document contract test needed the new dimensions property;
  updated it and verified the CAD, preview and OpenAPI document checks pass.
- Updated the MCP opts description after its registry-consistency test found the missing dimensions option;
  the MCP description test now passes.
- Focused race tests passed for dimensions, API option persistence and rejection; `go vet ./...` and
  `go build ./...` passed after the layout fix.
- Global Go 1.27.3 fails existing API stdlib-layout and stalled-body tests. Reproduced
  `TestZipSniffHeapTermIsDerived` and `TestHealthzMassWithAStalledBodyIsReleasedAtTheGrace` using original
  HEAD production files via a read-only Go overlay. Both pass with the already-installed Go 1.26.4
  specified by go.mod. Final full-suite verification uses `GOTOOLCHAIN=go1.26.4`.

- Final Go 1.26.4 run: every non-API package passed, including the complete convert suite (324.6 s).
  The API package reached the aggregate 10-minute timeout during
  `TestKeyedSmallBodyMassIsReleasedAtTheShortWindow`, without earlier assertion failures.
  The 79 remaining regular API tests passed in a separate 75.3 s run. The API package has no
  parallel tests; the continuation covers the running test and every subsequent listed regular test.
- The API fuzz seed suite `FuzzZipDirectoryGateNeverUnderestimates` also passed separately on Go 1.26.4.
- Completed in one independent review/fix iteration; no deferred findings.
