---
phase: 74-3d-render-measurements
plan: 02
subsystem: cad-engine
tags: [measurements, ocp, numpy, units, stl-stats, quality, docker]
status: complete
requires:
  - phase: 74-01
    provides: engine image with render, DERIVED cad_stl_ceiling_bytes, UNIT_ROW measurements, the unit probe
  - phase: 73-cad-class-convert-slice-confined
    provides: cad-mesh, the 63 MiB STEP ceiling, the 316 s mesh timeout
provides:
  - cad-mesh --quality draft|standard|fine and --measure PATH, and the stats subcommand for STL (numpy only)
  - internal/convert/cadmeasure.go (cadMeasureDraft, parseCADMeasureDraft, buildCADMeasurements, cadMMPerUnit, ErrCADMeasure)
  - invalid_solid as an allowed STL null reason (validator, docs, doc-sync tables, consistency test)
  - scripts/cad-render-probe.sh mesh mode and evidence/74-02-mesh.txt (18 rows, all ok)
affects: [74-03, 74-04, 74-05, 74-07]
key-files:
  created:
    - internal/convert/cadmeasure.go
    - internal/convert/cadmeasure_test.go
    - .planning/phases/74-3d-render-measurements/evidence/74-02-mesh.txt
  modified:
    - cmd/cad-worker/helper/cad-mesh
    - scripts/cad-render-probe.sh
    - internal/measurements/measurements.go
    - internal/measurements/consistency_test.go
    - docs/MEASUREMENTS.md
    - .planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt
key-decisions:
  - "cad_unit_factor_source is text_unit (orchestrator decision under the user's delegation): the factor is the R-04 table value for the unit the file text declares; the earlier DERIVED none line is untouched and a DECISION line follows it"
  - "IGES that declares anything but mm is reported unspecified/none (applied_factor null), because the IGES inch GAP stands"
  - "A STEP with no unit text gets applied_factor 1 and may take assume_unit; unit text that is present but outside the vocabulary, mixed or unreadable gets declared_unit empty, a non-empty raw text and no assume_unit"
requirements-completed: [RNDR-04, MEAS-02, MEAS-03, MEAS-04]
commits: 2
plan_head_before: 02c028fc6254218b27a9c0d309d0e8f781dc62ea
plan_head_after: abb023aff4db8b855db42e2ddea1311085dfdfee
actuals:
  tokens: 16800
  tasks: 2
  commits: 2
---

# Phase 74 Plan 02: Measurements at the source Summary

cad-mesh now measures with OCP (numpy for STL) and tessellates by quality, Go turns the draft into a validated `octoconv.measurements/v1` document in the declared unit, and every row of the live evidence on the production image is ok.

## What was built

- **cad-mesh quality (RNDR-04).** `--quality draft|standard|fine` maps to linear 0.004/0.001/0.0003 x diagonal and angular 0.5/0.35/0.2. Standard keeps the existing finite_box diagonal and the old constants, so its STL is unchanged.
- **cad-mesh --measure (MEAS-02).** After the STL is written: solids, closed solids (every shell closed and BRepCheck valid), volume and area summed over closed solids only. A closed total of exactly 0 or non-finite demotes the closed solids to invalid (never writes 0). The document bbox is the optimal box over the whole shape, with the finite-face fallback. A failure after the STL exists is `unreadable/measure_failed`, exit 2. The draft is compact sorted JSON with `allow_nan=False`.
- **Declared unit.** STEP text (GLOBAL_UNIT_ASSIGNED_CONTEXT) and the IGES global section are parsed as in the 71 helper; the IGES parse streams only the G lines (the 71 version read the whole file). Text outside the vocabulary goes to stderr as `cad-mesh: unit text not in vocabulary: ...` and into `declared_unit_raw`, never into `declared_unit`.
- **cad-mesh stats (D-08, R-08).** Binary (size = 84 + 50n) or ASCII STL, numpy and stdlib only, no OCP in the function (the awk acceptance check prints 0). Bbox in float64, volume and area in 1,000,000-triangle chunks, vertex dedupe by lexsort, the vertex array released before the edge sort, watertight by sorted forward and reverse edge keys. Reasons: `stl_not_watertight`, `negative_volume`, `invalid_solid` (watertight with zero or non-finite volume); a malformed file is `unreadable/stl_malformed`, exit 2; MemoryError is `too_complex/out_of_memory`.
- **cadmeasure.go (MEAS-03, MEAS-04).** `parseCADMeasureDraft` (at most 64 KiB, required keys, DisallowUnknownFields, one object, finite numbers), `buildCADMeasurements` (lengths /f, area /f^2, volume /f^3; declared only when applied_factor equals the table within 1e-9 relative; STL never declared; assume_unit only fills a missing unit; every document passes `Document.Validate`; failures wrap `ErrCADMeasure`, and a validator failure also wraps `measurements.ErrInvalid`).
- **invalid_solid for STL** added to `reasonsByFormat`, the Null reasons row, the Validation rules sentence and the consistency test in one commit (abb023a). `docs/MEASUREMENTS.md` no longer says "No engine emits this document yet".
- **scripts/cad-render-probe.sh mesh.** Builds the engine image, runs every row in one container each under the production limits, and removes its image tag. The pre-change helper for IDENTITY_STL is read from commit e3cab94 (the last commit that touched cad-mesh) and run on the same image.

## Results (evidence/74-02-mesh.txt, SUMMARY result=ok, 18 rows, 0 bad)

- **DRAFT (9 rows).** units_inch.step: volume 98322.384 and area 14193.52 mm, declared inch, applied_factor 25.4 (the plan's 98322.38 and 14193.51 within 0.01). legacy_cp1251 and LM8UU: mm, factor 1. nist_ctc_05: inch, 25.4, 1 solid. degenerate_plate: closed, volume 1000. open_shell.step and single_face.step: `no_solids`. surfaces_only.iges: `no_solids`, mm. nist-d2mi (extra row): inch, 25.4. The Go side builds a valid document from real helper drafts of units_inch (3 x 2 x 1 in, 6 in^3), an IGES, an ASCII STL and the not-watertight STL (checked once with a throwaway test, deleted).
- **STATS (3 rows).** stl_ascii_box volume 6000, area 2200; stl_binary_solid_header the same; nasa-hubble `stl_not_watertight`.
- **QUALITY (2 rows).** LinearSlide 3988 <= 5948 <= 14140; degenerate_plate 12, 12, 12.
- **IDENTITY_STL (2 rows).** units_inch.step (sha 07ccabf8...) and LM8UU.step (sha c4e58715...): new `--quality standard --measure` equals the e3cab94 helper byte for byte. Only these two files are checked, as the plan specified; the standard path is the old constants and the old mesher call.
- **WALL.** The 61.6 MiB step-copies file (sha equals the 73 S5 row d5dc17ee...) meshed with `--measure` at --cpus 2 --memory 4g: 173.24 s (an earlier run of the same code 171.22 s), limit 210 s, child rss 944 MiB, cgroup peak 1126 MiB, 3,057,894 triangles, 308-byte draft. The stop condition (> 210 s) did not trigger. The margin is 37 s, so any further work added to measure on this path must be re-measured against 210 s.
- **STATS_RSS.** Binary STL tile of 190,840,284 bytes (ceiling 190,840,832; 3,816,804 triangles), a watertight tile of units_inch so the full path (mass, dedupe, edge sort) runs: 2.94 s, child rss 582 MiB, cgroup peak 786 MiB against 2867.2 MiB.
- **Go.** `go test ./internal/convert/ ./internal/measurements/ -count=1` green (convert 109 s); 10 new top-level tests in cadmeasure_test.go; `go build ./...` and `go vet` clean.

## Unit-factor decision and the d2mi finding

Decision (orchestrator, under the user's delegation, recommended option): `cad_unit_factor_source=text_unit`. Appended to `evidence/74-01-wave0.txt` as a `DECISION` line after the untouched `DERIVED ... value=none` line, together with the GAP line below.

- text_unit equals the measured ratio within 1e-6 on units_inch (25.4), nist_ctc_05 (25.4 exactly, where OCP's own FileUnits reports unknown), LM8UU and legacy_cp1251 (1.0).
- **GAP `nist_d2mi_ratio_25.4707`, explained in one probe run of the 71 pruned image (no build).** OCP reports 139 unique vertices on nist-d2mi-827-9999-906.stp against 130 `VERTEX_POINT` entities. The 9 extra are OCP-created edge-split vertices on the top face at z = 0.6 in, where the text vertices reach 0.50625 in. Restricted to the 130 vertices that match a VERTEX_POINT, the bbox diagonal is 4.924289396704869 in on both sides, so the ratio is exactly 25.4. The 25.4707 is a vertex-set artifact of the comparison, not a unit effect.
- What this does and does not prove: the applied factor in the draft is the table value for the declared text unit; the helper does not measure the OCP ratio at run time. The ratio was measured offline on five STEP files in 74-01 plus the d2mi probe. `docs/MEASUREMENTS.md` states this.
- Fallback per R-05: text unit absent gives `unspecified/none` (or `client_assumed` with assume_unit); present but outside the vocabulary gives `unspecified/none` and assume_unit is not applied.
- The IGES inch-scaling GAP stands: no inch IGES in the corpus, so an IGES that declares anything but mm has `applied_factor` null and becomes `unspecified/none`. If a later plan proves inch IGES scaling, the one-line rule in `applied_factor()` in cad-mesh is where to extend it.

## Inputs from 74-01 used

Triangle cap 3,835,382 and the STL ceiling 190,840,832 bytes (the STATS_RSS tile is sized from the DERIVED line in the evidence file). The cap itself belongs to the render path (plan 74-03 onward).

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 1 - Bug] open_shell.step expectation in the probe**
- **Found during:** Task 1, first probe run
- **Issue:** the DRAFT expectation for open_shell.step was `open_shell`, but the corpus row, and docs/MEASUREMENTS.md line 75, record that OCP reads it as 0 solids, so `no_solids` is the correct and already documented reason. The helper was right; my expectation was wrong.
- **Fix:** expectation changed to `no_solids` in `scripts/cad-render-probe.sh`; the probe was re-run in full (run 2 is the evidence).
- **Commit:** e873193

### Plan interpretation

- Evidence has 18 rows with result=ok (the plan asks for at least 17): the extra is a DRAFT row for nist-d2mi, used for the unit finding.
- The d2mi probe script was run from the scratchpad and is not committed; its output is recorded in the GAP line. This is the only non-reproducible step in this plan.
- The evidence HDR line records `commit=02c028f` with `tracked_dirt=3`: the probe ran on the working tree before the task commit. The files that run used (cad-mesh and the probe) are byte-identical to e873193.
- The STATS_RSS tile is a watertight tile built by the probe itself from the units_inch STL, not a 71 `stl-tile` of a NASA part, so that the whole stats pipeline runs rather than an early not-watertight exit.
- Task 1's commit also carries the DECISION and GAP lines in `evidence/74-01-wave0.txt`, per the orchestrator's instruction, although that file is not in this plan's file list.

## Auth gates

None.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register. T-74-03 (draft tampering) is mitigated by the strict 64 KiB decode, the table check and `Document.Validate` on every document; T-74-04 (STL stats memory) by chunked mass accumulation and the measured 786 MiB peak at the ceiling.

## Verification

- Task 1 gate: ast parse, `bash -n`, the awk row count (n[0]=0, n[1]=18) and `SUMMARY,result=ok` all pass; no comment lines in cad-mesh or the probe beyond the shebang; `awk '/^def stats/,/^def [^s]/' cad-mesh | grep -c OCP` prints 0.
- `git diff --exit-code <phase-73 merge-base> -- cmd/cad-worker/helper/cad-guard internal/convert/exec.go` and `git diff --exit-code main -- internal/convert/exec.go` both exit 0.
- Task 2 gate: vet clean; measurements tests green including doc-sync; 10 passing CAD measure tests; no added comment lines in cadmeasure.go or measurements.go.
- The invalid_solid validator change and the docs table row are in one commit (abb023a).
- Both commits are SSH-signed (`git log` shows G). No engine74 image tag remains; free disk 15 GiB. STATE.md and ROADMAP.md untouched; render is not registered.

## Self-Check: PASSED

cadmeasure.go, cadmeasure_test.go and 74-02-mesh.txt exist; commits e873193 and abb023a are ancestors of HEAD.
