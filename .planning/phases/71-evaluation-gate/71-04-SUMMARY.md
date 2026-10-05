---
phase: 71-evaluation-gate
plan: 04
subsystem: infra
tags: [cad, measurement, ocp, vtk, osmesa, ezdxf, rsvg, pixel-judge, units, rendering]
status: complete

requires:
  - phase: 71-02
    provides: measurement image, tracer helpers, synthetic corpus
  - phase: 71-03
    provides: committed public corpus, manifest, counters
provides:
  - cad_mesh.py with unit origin, null-volume reason codes, IGES and STL paths, quality and parallel knobs, --dump-maps
  - cad_dxf.py (ezdxf drawing to a square SVG) and the rsvg-png / rsvg-pdf operations
  - cad_render.py with seven orthographic Z-up views, transparent background, LP_NUM_THREADS, --dump-maps
  - pixelcheck.py single (part, drawing, sliver), views, same, transparent
  - contact_sheet.py and scripts/cad-render-measure.sh
  - evidence 71-04-helpers.txt (UNITS, FINDINGS), 71-04-render.txt, four contact sheets
affects: [71-05, 71-06, 71-07, 72, 73, 74, 75]

actuals:
  tokens: 13400
  tasks: 2
  commits: 2
plan_head_before: e28611e5d7657126162f7f7919ca466e47fcf436
plan_head_after: 1a81673cb6c3cf2747d02f82ff759ff4a513ee46

tech-stack:
  added: []
  patterns: [container per cell with retry lines instead of silent retries, pixel judge over exit codes, geometry-derived sliver mode for edge-on degenerate projections]

key-files:
  created:
    - scripts/cad-measure/cad_dxf.py
    - scripts/cad-measure/contact_sheet.py
    - scripts/cad-render-measure.sh
    - .planning/phases/71-evaluation-gate/evidence/71-04-helpers.txt
    - .planning/phases/71-evaluation-gate/evidence/71-04-render.txt
    - .planning/phases/71-evaluation-gate/evidence/71-04-contact-views.png
    - .planning/phases/71-evaluation-gate/evidence/71-04-contact-robustness.png
    - .planning/phases/71-evaluation-gate/evidence/71-04-contact-quality.png
    - .planning/phases/71-evaluation-gate/evidence/71-04-contact-dxf.png
  modified:
    - scripts/cad-measure/cad_mesh.py
    - scripts/cad-measure/cad_render.py
    - scripts/cad-measure/pixelcheck.py
    - scripts/cad-measure/measure_wrap.py
    - scripts/cad-rss-time-measure.sh

key-decisions:
  - "Units come from the STEP/IGES text; OCP FileUnits is recorded beside it and a mismatch is flagged (unit_agree), never trusted alone"
  - "volume and area only over closed valid solids; otherwise null with a reason code, never 0"
  - "A sliver pixel mode applies when the renderer reports a projected aspect below 0.02; it asks for a full-length stroke, not fill or dark pixels"
  - "Failed cells are retried up to three times and every retry is a RETRY line in the evidence"

patterns-established:
  - "Pixel thresholds are named constants at the top of pixelcheck.py and are printed as CAL lines in the render evidence"
  - "The geometry fact (projected aspect) comes from the renderer sidecar, the verdict from the pixels"

requirements-completed: [EVAL-01]
---

# Phase 71 Plan 04: Helpers, 2D path and pixel-judged render Summary

The CAD helpers now report units with their origin, honest null volumes with reason codes, an ezdxf SVG to rsvg-convert drawing path, and a seven-view orthographic render that passes pixel criteria on real parts and is byte-identical across runs on one architecture.

## Accomplishments

- **Task 1 (`f2015fe`)**: `cad_mesh.py mesh|stlstats`, `cad_dxf.py`, operations `stlstats`, `dxf`, `rsvg-png`, `rsvg-pdf` in `cad-rss-time-measure.sh`, `--quality` and `--parallel` pass-through. Gate `HELPERS-OK` over the whole committed corpus (59 ROW lines, one rc not 0, explained in FINDINGS).
  - `units_inch.step`: `unit_text:inch`, agrees with OCP; `assembly_nauo4.step`: 4 solids, volume 4000 mm^3.
  - `surfaces_only.iges`, `iges_no_newlines.iges`, `open_shell.step`, `single_face.step`: `volume:null`, reason `no_solids`; no row has `volume:0` or `closed_solids:0` with a volume.
  - `legacy_cp1251.step` and `legacy_latin1.step` read with rc=0; DXF `text0` decodes to `Деталь` and `Bügel Ø5`.
  - `stl_ascii_box.stl` volume 6000 mm^3; every `rsvg-png` row has `pix=ok` at 1280x1280, every PDF starts with `%PDF-`.
- **Task 2 (`1a81673`)**: `cad-render-measure.sh` gate `RENDER-OK`: `RENDER_SUMMARY,parts=5,pngs=108,fail=0,views_differ=4,determinism=identical`, `retries=0`.
  - 5 real parts (LinearSlide, corner bracket, LM8UU, SC8UU as the multi-solid assembly, NASA Hubble STL), 7 views each with `ok=1`; 6 robustness fixtures pass in all 7 views; 8 DXF drawings pass `--mode drawing`.
  - The four designated asymmetric parts (slide, corner, hubble, lbracket) differ in iso/front/top: `min_pair_iou` 0.11 to 0.50 against a limit of 0.97.
  - Transparent render passes. Two iso renders of every real part are byte-identical; `LP_NUM_THREADS=1` equals the default (informative).
  - Size ladder on the slide, iso: 512 px 0.74 s wall, 1280 px 0.92 s, 2048 px 1.21 s (child RSS 207, 273, 393 MiB).
  - Calibrated pixel constants over the real parts (part mode): fill fraction 0.116 to 0.654, dark fraction at least 0.0046, margin at least 76 px; constants are printed as CAL lines.
  - Camera constants for RNDR-02 (VIEWDEF lines): orthographic, Z up, camera = bbox centre + direction x 2 x diagonal; iso (1,-1,1), front (0,-1,0), back (0,1,0), left (-1,0,0), right (1,0,0) with up (0,0,1); top (0,0,1) up (0,1,0); bottom (0,0,-1) up (0,-1,0).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Up vector not orthogonalised in the camera fit (tracer bug)**
- **Found during:** Task 2 first runs; the degenerate plate iso was judged as a sliver
- **Issue:** the projected half-height was measured on the raw up hint (Z) instead of the true camera up, which gave a wrong fit and a wrong projected aspect for iso
- **Fix:** orthogonalise the up vector against the view direction
- **Files modified:** scripts/cad-measure/cad_render.py
- **Commit:** 1a81673

**2. [Rule 1 - Bug] Unbounded whole-shape bounding box**
- **Found during:** Task 1 corpus run on `V-Slot-20x20-100mm.step`
- **Issue:** `BRepBndLib.Add_s` returned +-1e100 (diagonal 3.46e100); the tracer derived the linear deflection from it and produced a coarse 672-triangle mesh
- **Fix:** above a diagonal of 1e12 use the union of per-face `AddOptimal_s` boxes (`bbox_source:finite_faces`, 1056 triangles, diagonal 103.92)
- **Files modified:** scripts/cad-measure/cad_mesh.py
- **Commit:** f2015fe

**3. [Rule 2 - Missing critical functionality] `measure_wrap.py` could not carry null values or the new keys**
- **Issue:** `None` values were dropped from `out=`, so `volume:null` could not appear; new keys (area, unit_agree, insunits, skipped_types, svg_counts, bbox_source) had no slot
- **Fix:** print `null` for null values, flatten lists and dicts, extend the key list. The file is not in the plan's `files_modified`
- **Commit:** f2015fe

### Plan wording versus observed behaviour (documented, not forced)

- **Negative volume never occurs:** OCP's STEP reader repairs reversed orientation (`reversed_solid.step` reads +6 mm^3).
- **Open shell and closed shell with a removed face read as 0 solids,** so they give `no_solids`. The reason codes `open_shell`, `invalid_solid` and `negative_volume` exist in `cad_mesh.py` but were not observed on this corpus.
- **Inch STEP scales to mm** (6 in^3 reads 98322.384 mm^3).
- **`text_fonts.dxf` is not in the committed corpus** (71-03 did not take it). Text is covered by `columns_R2007.dxf` (MTEXT) and the two legacy DXFs; DIMENSION by `uncommon.dxf` and `dimension_in_nested_blocks.dxf`.
- **The real assembly is `SC8UU.step` (3 solids);** no heavy file was downloaded.
- **SVG path:** `/job/out.svg` instead of the plan's `/job/out/out.svg`, to match the existing job-directory layout.
- **Pixel criteria calibration (A6):** part mode keeps the 71-02 values. New sliver mode (projected aspect below 0.02, reported by the renderer sidecar) requires a stroke spanning at least 80 percent of the frame, margin and non-empty pixels, but no fill or dark-pixel minimum: a smooth rod seen edge-on has no feature edges. Drawing mode: fill at least 0.0003, dark at least 0.0002.
- **Evidence image tags differ between the two evidence files** because the tag hashes every helper `.py`; the Task 1 evidence was produced before the Task 2 files were final.

## Findings for phases 72-75

- `3dsolids.dxf` (four ACIS 3DSOLID, nothing else): `cad_dxf` exits 3, nothing drawable; the `rsvg-png` and `rsvg-pdf` cells print `SKIP prestep-failed-3-attempts` (a deterministic failure, not a flake). Phase 75 needs a typed error, not an empty page.
- `uncommon.dxf` renders without its 3DSOLID and without a failure (`skipped_types:3DSOLID-1`): surface skipped types to the caller.
- `nist_ctc_05_asme1_ap242-e1.stp` declares lowercase `'inch'`; OCP `FileUnits` returns an empty name (`unit_agree:0`). Read the unit from the file text. Whether OCP scaled that file's geometry was not proven (proven only for `units_inch.step`).
- Real NASA STLs are not watertight under exact-coordinate vertex deduplication: volume null with `stl_not_watertight`.
- A STEP has no code page: `legacy_cp1251.step` text falls back to latin-1 (`Äåòàëü`) while the geometry reads fine; the DXF twins decode through `$DWGCODEPAGE`.
- DXF SVG output had zero `script`, `foreignObject` and external `href` elements on every drawable file (observation for phase 75, no filtering here).

## Auth gates

Task commits were blocked once by the 1Password SSH signing agent (`failed to fill whole buffer`). The work was held staged, the user unlocked 1Password, and the commits were made signed. No unsigned commit and no hook bypass was used.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register. T-71-10: every cell runs in its own container with `--cpus 2`, equal `--memory` and `--memory-swap`, `--network none`, `--user nobody` and the `measure_wrap` budget. T-71-12: `cad_dxf.py` sets `HOME` into the job directory before importing ezdxf. T-71-11 is accepted; the SVG counters are recorded.

## Self-Check: PASSED

- scripts/cad-measure/cad_dxf.py, contact_sheet.py, scripts/cad-render-measure.sh: present
- evidence 71-04-helpers.txt, 71-04-render.txt and four contact sheets: present
- commits f2015fe and 1a81673 on gsd/phase-71-evaluation-gate; `git rev-list --count` from the ledger gives 2
- gates printed `HELPERS-OK` and `RENDER-OK`; `internal/convert/cgroup.go` and everything outside `internal/convert/testdata/cad` unchanged against 4e595c0
