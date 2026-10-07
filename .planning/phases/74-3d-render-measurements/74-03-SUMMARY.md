---
phase: 74-3d-render-measurements
plan: 03
subsystem: cad-engine
tags: [options, validation, pixel-judge, png, render]
status: complete
requires:
  - phase: 74-02
    provides: cad-mesh quality and measurements, the measurements document builder
  - phase: 73-cad-class-convert-slice-confined
    provides: CADConverter, the opts switch in the API, the worker descriptor
provides:
  - convert.CADOpts, ParseCADOpts, CADOptsFromMap, ValidateCADApplicability, Resolved and the render size constants
  - convert.OperationRender (not registered)
  - convert.ErrCADRenderRejected and convert.JudgeCADPNG with CADPixelFacts
  - scripts/cmd/cad-pixel-judge for the live differential in plan 74-06
affects: [74-04, 74-05, 74-06]
key-files:
  created:
    - internal/convert/cadopts.go
    - internal/convert/cadopts_test.go
    - internal/convert/cadpixel.go
    - internal/convert/cadpixel_test.go
    - internal/api/cad_opts_test.go
    - scripts/cmd/cad-pixel-judge/main.go
  modified:
    - internal/convert/cad.go
    - internal/convert/cad_test.go
    - internal/convert/convert.go
    - internal/api/handlers.go
key-decisions:
  - "Size is parsed from the raw token with strconv.Atoi, so 1280.0, 1e3, a string and null are all invalid_options and an explicit 0 is out of range"
  - "String options are checked from the raw JSON value (must be a string in the closed set), so null, a number or an empty string cannot pass as an absent key"
  - "CADOptsFromMap returns CADOpts, not any; the worker descriptor closure compiles unchanged because CADOpts is assignable to any"
requirements-completed: [RNDR-02, RNDR-03, RNDR-04, RNDR-05, MEAS-04]
commits: 2
plan_head_before: d4afc414b879b80754cc06f9efb17faebbc7904d
plan_head_after: 914c9f63035409ec4848875b0d97384efb0fe266
actuals:
  tokens: 21000
  tasks: 2
  commits: 2
---

# Phase 74 Plan 03: Closed render options and the Go pixel judge Summary

CAD options are one closed union validated at create and re-parsed identically in the worker, and a pixel judge accepts every frame the 71 rule accepted, admits the W15 thin part, and rejects empty, dot, edge-to-edge and mis-sized frames.

## What was built

- **cadopts.go (D-04, R-03).** `CADOpts` with view (iso, front, back, left, right, top, bottom), size (*int, 256..2048), quality (draft, standard, fine), background (opaque, transparent) and assume_unit (mm, cm, m, inch, foot, micrometre). `ParseCADOpts` runs `checkStrictObject` (duplicate keys, trailing bytes, non-object) and then validates each key from its raw value. `Resolved()` fills iso, 1280, standard, opaque and leaves assume_unit empty.
- **Applicability.** `ValidateCADApplicability(engine, operation, source, target, o)`: zero opts always apply; operation convert accepts only quality; render accepts all five keys except quality on an stl source (alias-normalized); any other engine or operation is refused.
- **API.** `case convert.EngineCAD` in the opts switch: parse failure is 422 invalid_options, applicability failure is 422 opts_not_applicable, and the stored opts are `json.Marshal` of the parsed struct (not Resolved), so whitespace and key order from the client never reach storage.
- **Worker.** `CADConverter.Convert` still calls `CADOptsFromMap` and now also refuses a non-quality key; the descriptor closure is unchanged. The old "takes no options" tests were replaced by the union's behaviour.
- **cadpixel.go (D-06).** `JudgeCADPNG(r, size, background)`: reads at most 96 MiB, `DecodeConfig` must report size x size before any pixel decode (a 20000 x 20000 header is refused with no IDAT present), then J1 count >= long side, J2 long side >= 0.50 x size, J3 margin >= int(0.03 x size), J4 blob (frac 0.02..0.90 and dark >= 0.001) or thin (aspect < 0.15 and dark count >= 0.25 x long side). Opaque mask is non-zero alpha with a channel below 250; transparent mask is non-zero alpha. Transparent needs alpha in the PNG (colour type 4 or 6, or tRNS), a transparent fraction within 0.05..0.95 and some alpha 255; opaque needs no alpha-0 pixel. Every rejection wraps `ErrCADRenderRejected` and names the rule.
- **cad-pixel-judge.** `--size N --background opaque|transparent PNG...`, one `GOJUDGE,png=..,ok=0|1,mode=..` line per file with all facts, exit 1 if any is rejected, 64 on usage.

## Verification

- `go build ./...`, `go vet ./...` clean; `go test ./internal/convert/ ./internal/api/ ./internal/worker/ -count=1` green.
- 18 top-level judge tests pass: hexagon blob (mode blob), W15 band 93 x 1030 at x=594,y=124 (mode thin, frac and dark below the 71 blob thresholds, so only the thin rule admits it), 1126 x 2 sliver (thin), and rejections for empty (J1), dot (J2), edge-to-edge (J3), 0.95-frac dark frame (J3), hollow outline (J4), wrong size (J0), huge header (J0), non-PNG, and the transparent and opaque background cases.
- `TestDefaultRegistersOnlyAllowlistedCADPairs` passes unchanged; `OperationRender` is referenced only by the constant and the validator.
- `git diff main -- internal/convert/exec.go` is empty; no added `//` line in the plan's files.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Render-path API cases are covered at the validator, not over HTTP**
- **Found during:** Task 1
- **Issue:** The plan lists API cases for `operation=render` (invalid view, STL render with quality). Create gate 1 rejects `render` as an unknown operation until plan 74-05 registers it, and the plan forbids registering a render converter here, so those requests can never reach the opts switch.
- **Fix:** `TestValidateCADApplicability` covers every render and convert applicability row; the API tests cover the convert path (invalid_options and opts_not_applicable cases, normalized storage) and `TestCreateJobRenderStaysUnregistered` pins that render is still 422 unsupported_operation with no upload, create or enqueue. The handler passes `operation` straight to the validator, so 74-05 inherits the render behaviour; it should add the two HTTP render cases when it registers the converter.
- **Files modified:** internal/api/cad_opts_test.go, internal/convert/cadopts_test.go

**2. [Rule 3 - Blocking] ErrCADRenderRejected landed in the Task 1 commit**
- **Issue:** The sentinel is declared in cad.go, which Task 1 already edited.
- **Fix:** Added with the Task 1 cad.go change; Task 2 uses it.

## Known Stubs

None.

## Threat Flags

None. T-74-05 (closed enums, *int range, applicability, normalized storage) and T-74-06 (header size must equal the request, at most 2048, before decode) are mitigated and tested.

## Notes for later plans

- The judge does not check the helper verdict's view or size; the caller (74-04) must compare them to the request (J0 helper facts).
- Small sizes are only synthetically tested (a 256 frame with a blob). The research caveat stands: thin parts at 256 px are unmeasured and need the live calibration in 74-06.
- A thin rod seen end-on has a dot silhouette and fails J2 by design.
- `quality` is accepted on a convert job but `CADConverter.Convert` does not yet pass it to cad-mesh; wiring is outside this plan.

## Self-Check: PASSED

All six created files exist; commits 47b4951 and 914c9f6 are ancestors of HEAD.
