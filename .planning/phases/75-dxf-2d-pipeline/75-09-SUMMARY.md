---
phase: 75-dxf-2d-pipeline
plan: 09
subsystem: DXF exposure (registry, cad-worker, API ceiling, formats and inspect) and its create-time and output tests
tags: [dxf, exposure, registry, ceiling, opts, 422, 413, inspect, measurements, add-outputs]
status: complete

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-06 CADMaxDXFBytes, ConfinedCAD with the DXF and rsvg timeouts and the D-07 option rules; 75-07 CADDXFConverter and CADDXFRenderConverter, unregistered; 75-08 measured rulesets, calibrated drawing judge and the preflight on every start
provides:
  - "Exposure commit bd90a59: convert dxf>svg, convert dxf>pdf and render dxf>png registered under cad, replaced in the cad-worker by the confined converter, gated at the API by CADMaxDXFBytes, and advertised by /v1/formats and /v1/inspect"
  - "The CAD pair allowlists become the exact eight-pair set: convert step>stl, iges>stl, dxf>svg, dxf>pdf; render step>png, iges>png, stl>png, dxf>png"
  - "HTTP tests for the D-07 contract on dxf, the dxf 413 and a padded-dxf builder; worker tests for the dxf render's two outputs and the dxf convert's single output"
affects: [75-10, 76]

tech-stack:
  added: []
  patterns:
    - "Exposure in one commit: registry, worker Replace, API ceiling and every allowlist pin together; earlier commits inert"
    - "New tests are shown to bite by mutation of the code they pin (evidence/75-09-mutations.txt)"

key-files:
  created:
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-09-ci-local.txt
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-09-mutations.txt
  modified:
    - internal/convert/converters.go
    - internal/convert/convert_test.go
    - cmd/cad-worker/main.go
    - internal/api/api.go
    - internal/api/cad_ceiling_test.go
    - internal/api/formats_handlers_test.go
    - internal/api/cad_opts_test.go
    - internal/worker/cad_outputs_test.go

key-decisions:
  - "75-09: the inspect offer table is keyed by operation and target, because a dxf has two convert targets (svg, pdf) where step and iges had one"
  - "75-09: pre58_pinning_test.go and handlers_test.go name no CAD pair set, so the exposure left them untouched; the full suite proved no other pin names the old set"

requirements-completed: [RNDR-06, CCONV-02, CCONV-03]

coverage:
  - id: D1
    description: "One commit exposes the three DXF pairs everywhere at once, and the allowlists are the exact eight-pair set"
    requirement: CCONV-02
    verification:
      - kind: unit
        ref: "TestDefaultRegistersOnlyAllowlistedCADPairs (eight pairs, vacuity guard kept), TestListFormats_AdvertisesOnlyAllowlistedCADPairs, TestCADFormatCeilingDefaultsPinnedToConvertConstants, TestCADInspectReportsPerFormatCeilingAsMaxBytes/dxf"
        status: pass
      - kind: integration
        ref: "git log --oneline main..HEAD -- internal/convert/converters.go internal/api/api.go lists only bd90a59; scripts/ci-local.sh helm gate on bd90a59 ALL REQUESTED TIERS PASSED (evidence/75-09-ci-local.txt)"
        status: pass
    human_judgment: false
  - id: D2
    description: "dxf create-time contract: 422 opts_not_applicable for view/quality on render and any option on convert, 422 invalid_options for an invalid view, 413 naming dxf above the ceiling, none with a side effect; 202 on the cad queue with normalized opts for the valid cases"
    requirement: RNDR-06
    verification:
      - kind: unit
        ref: "internal/api TestCreateJobDXF* (5 tests, 14 subtests), TestCADDXFCeilingAtTheDefaultRefusesBeforeUpload (svg, png), TestCADDXFPaddedInputIsAcceptedBelowTheCeiling"
        status: pass
      - kind: unit
        ref: "mutations M1 (dxf rules removed) and M2 (dxf ceiling removed) fail the tests that pin them (evidence/75-09-mutations.txt)"
        status: pass
    human_judgment: false
  - id: D3
    description: "A dxf render records the PNG and the 2D measurements in one AddOutputs call before MarkDone; a dxf convert records one primary output with its content type"
    requirement: CCONV-03
    verification:
      - kind: unit
        ref: "internal/worker TestCADOutputsDXF* (3 tests), with mutation M3 failing all three"
        status: pass
    human_judgment: false

actuals:
  tokens: 5200
  tasks: 2
  commits: 2
plan_head_before: 7780cc2618f1f11c03ac699ccf3c3507229446ab
plan_head_after: 1e1a0fba6180ec9d8c45d813405f3e0ecde186c2

duration: "about 28 min of wall time (2026-10-12 03:03Z to 03:31Z)"
completed: 2026-10-12
---

# Phase 75 Plan 09: DXF exposure and its create-time and output tests Summary

**DXF is visible to clients in one signed commit (bd90a59).** The registry, the cad-worker's confined replacements, the 118 MiB dxf ceiling, /v1/formats, /v1/inspect and every CAD allowlist changed together. The tests that followed pin what a client now meets for dxf: the 422s, the 413, the 202s and the outputs.

## Task Commits

Both are SSH-signed (G).

1. **Task 1, exposure:** `bd90a59` feat. Contains converters.go, cmd/cad-worker/main.go and internal/api/api.go, plus the three flipped pins.
2. **Task 2, tests:** `1e1a0fb` test. cad_opts_test.go, cad_ceiling_test.go and cad_outputs_test.go.

`git log --oneline main..HEAD -- internal/convert/converters.go internal/api/api.go` lists only bd90a59. Every earlier commit of the phase leaves the client-visible surface unchanged.

## What the exposure commit changes

- **Registry.** `converters.go` registers `CADDXFConverter{}` and `CADDXFRenderConverter{}` after `CADRenderConverter{}`.
- **cad-worker.** `cmd/cad-worker/main.go` replaces both with `CADConverter: conv`, the confined converter that carries the DXF and rsvg timeouts. The landlock startup line now reads `(convert, render and dxf)`.
- **API.** `defaultMaxFormatBytes[cad]["dxf"] = convert.CADMaxDXFBytes`, which is 118 MiB. The engine-level cad gate stays at CADMaxIGESBytes (135 MiB), so the dxf gate is the per-format one and the 413 message names dxf.
- **No queue, engine class or operation value was added.** dxf uses the existing cad class and the cad:job task.

## Flipped pins (every one listed)

- **`internal/convert/convert_test.go` TestDefaultRegistersOnlyAllowlistedCADPairs.** The allowlist is exactly eight pairs: convert step>stl, iges>stl, dxf>svg, dxf>pdf; render step>png, iges>png, stl>png, dxf>png. The vacuity guard and the all-present check are kept, and the failure message names the eight.
- **`internal/api/formats_handlers_test.go` TestListFormats_AdvertisesOnlyAllowlistedCADPairs.** `engines` and `operations.convert` gain dxf>svg and dxf>pdf, and `operations.render` gains dxf>png.
- **`internal/api/cad_ceiling_test.go`:**
  - TestCADFormatCeilingDefaultsPinnedToConvertConstants: the cad map is step, iges, stl and dxf, and dxf equals CADMaxDXFBytes.
  - TestCADInspectReportsPerFormatCeilingAsMaxBytes: re-keyed by operation and target, with a dxf row (legacy_cp1251.dxf) offering convert svg, convert pdf and render png with max_bytes CADMaxDXFBytes.
- **No other pin needed flipping.** The full suite with only the code change applied failed in exactly these three tests. `pre58_pinning_test.go` and `handlers_test.go` name no CAD pair set; they passed unchanged.

## Tests added in Task 2

**internal/api/cad_opts_test.go** uses the `postDXFJob` builder with `synthetic/legacy_cp1251.dxf`. Every refusal asserts zero uploads, no created job and zero enqueues.

| Test | Cases | Result |
|---|---|---|
| TestCreateJobDXFRenderViewOrQualityIsOptsNotApplicable | `view top`, `quality fine`, `size 512 + view iso`, `background transparent + quality draft` | 422 opts_not_applicable |
| TestCreateJobDXFRenderInvalidViewIsInvalidOptions | `view sideways` | 422 invalid_options |
| TestCreateJobDXFConvertWithAnyOptionIsOptsNotApplicable | svg: size 1024, quality, assume_unit; pdf: background, view, quality | 422 opts_not_applicable |
| TestCreateJobDXFConvertWithoutOptionsIsAcceptedOnTheCADQueue | pdf, svg with no opts | 202, cad convert job, no stored opts, one cad enqueue of the created id, one upload |
| TestCreateJobDXFRenderWithSizeBackgroundAndUnitIsAcceptedNormalized | size 2048, background transparent, assume_unit mm, with whitespace | 202, stored opts exactly `{size: 2048, background: transparent, assume_unit: mm}`, one cad enqueue |

**internal/api/cad_ceiling_test.go:**
- `cadDXFPaddedTo` pads legacy_cp1251.dxf to an exact size with 999 comment pairs inside the HEADER section. The head pair and the `0/EOF` tail the sniffer reads are left intact.
- TestCADDXFCeilingAtTheDefaultRefusesBeforeUpload: a file of CADMaxDXFBytes + 1 bytes, for convert svg and render png, gets 413 payload_too_large with `dxf file exceeds the 118 MiB limit for this conversion`, and nothing is uploaded, created or enqueued. It is skipped under `-short`, like the STEP/IGES ceiling test.
- TestCADDXFPaddedInputIsAcceptedBelowTheCeiling: the same builder at 1 MiB gets 202 as a dxf job on the cad queue. This proves the padding still sniffs as dxf.

**internal/worker/cad_outputs_test.go** uses the newCADHarness with `sidecarConverter{from: "dxf"}`. The 2D sidecar is a dxf document with unit mm (declared), a two-coordinate bbox, and null volume and area with not_applicable_2d. The fixture is checked with `measurements.Validate`.
- TestCADOutputsDXFRenderRecordsPNGAndMeasurementsInOneCallThenMarksDone:
  - the repo calls are exactly `AddOutputs,MarkDone`;
  - primary is at ordinal 0 as out.png with image/png;
  - measurements is at ordinal 1 as measurements.json with application/json;
  - uploads go png first, then json.
- TestCADOutputsDXFRenderWithA3DBBoxUploadsNothing: a 3-coordinate bbox on a dxf document is a terminal ErrCADMeasurementsInvalid, with no upload and no repo write.
- TestCADOutputsDXFConvertRecordsOnePrimaryAndNoExtras (svg, pdf): a sidecar is present, but the convert job records exactly one primary row (out.svg as image/svg+xml, out.pdf as application/pdf) and uploads one object.

## Verification

All of these ran with GOTOOLCHAIN=go1.26.4 and python 3.14 first on PATH.

- **Task 1:**
  - gofmt is clean, and `go build ./...` and `go vet ./...` pass.
  - `go test ./... -count=1` gives 31 packages ok.
  - TestJobDurationLabelsPinned and TestQueueDepthLabelsPinned pass.
  - `git diff --exit-code main -- internal/api/testdata '**/*.golden.json' internal/metrics` is empty.
  - `scripts/ci-local.sh helm gate` on bd90a59 ends `ALL REQUESTED TIERS PASSED` (evidence/75-09-ci-local.txt). The only tracked dirt is .planning/config.json. The transcript itself warns that local helm is v4.3.0 while ci.yml pins v4.2.3.
  - `go test ./internal/convert/ -run TestDefaultRegistersOnlyAllowlistedCADPairs -v` passes with eight allowlisted pairs.
- **Task 2:**
  - `go vet` on api and worker is clean, and `go test ./internal/api/ ./internal/worker/` is ok.
  - The DXF pattern gate counts 10 top-level PASS and no FAIL; at least 6 were required.
  - No `//` line was added in the three test files.
- **Before the Task 2 commit:** the whole suite again gave 31 packages ok and 0 FAIL.
- **Phase rule:** `git diff --exit-code main -- internal/convert/exec.go` is empty.
- **Mutations (evidence/75-09-mutations.txt).** Each was restored with `git checkout -- <file>`, and the tree was checked clean before committing.
  - M1 disables the dxf branch of ValidateCADApplicability: both opts_not_applicable dxf tests fail. The invalid_options test still passes, which is correct, because that refusal comes from parsing.
  - M2 removes the dxf ceiling: the defaults pin, the inspect dxf row and the dxf 413 test fail.
  - M3 inverts cadSideOutputs' render check: all three worker dxf tests fail.

The tests in Task 2 (tdd="true") pin behaviour that the exposure commit had already made reachable, so they could not start RED against the committed code. The mutations above stand in for the RED step: each one removes the behaviour a test pins, and that test fails.

## Deviations from Plan

1. **[Plan interpretation] The inspect dxf row landed in the exposure commit.** Task 1's action puts it there ("the inspect offer table gains a dxf row"). Task 2 lists inspect in its behaviour, but it is already covered by `TestCADInspectReportsPerFormatCeilingAsMaxBytes/dxf`, so Task 2 did not add a duplicate.
2. **[Test strength] Extra cases beyond the plan's list.** The plan's list is covered in full. The extras are:
   - two combined-option refusals on render;
   - five more single-option refusals on convert;
   - the svg target in the 202 convert case;
   - a below-ceiling acceptance of the padded dxf, so the 413 cannot pass on a file that fails to sniff;
   - a 3D-bbox rejection on a dxf render;
   - the pdf content type on convert.
3. **[Process] One commit attempt hit a transient `.git/index.lock`.** No git process held it and the file was gone at the next check, probably the editor's git polling. The retry committed the same staged set. Nothing was removed by hand.
4. **[Scope] STATE.md, ROADMAP.md and REQUIREMENTS.md were not updated.** The orchestrator handles phase tracking, as instructed.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register:
- **T-75-32:** exposure in one commit (bd90a59) after 75-08's measured stages.
- **T-75-33:** the dxf ceiling from CADMaxDXFBytes, with 413 and no side effect, tested and mutation-proven.
- **T-75-34:** the D-07 422 contract over HTTP with no side effect, tested and mutation-proven.
- **T-75-SC:** no package was added.

## State

- The compose stack was never started.
- No image was built, and no container or volume was touched.
- The VM disk was not used beyond the ci-local helm and gate tiers.

## Self-Check: PASSED

- **Files:** converters.go, main.go, api.go, the five test files and both evidence files exist with the changes described.
- **Commits:** bd90a59 and 1e1a0fb are ancestors of HEAD and show signature G.
