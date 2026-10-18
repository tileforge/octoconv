---
phase: 71-evaluation-gate
verified: 2026-10-18T01:05:40Z
status: passed
score: 8/8 must-haves verified
covered_files:
  - .planning/phases/71-evaluation-gate/71-01-PLAN.md
  - .planning/phases/71-evaluation-gate/71-01-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-02-PLAN.md
  - .planning/phases/71-evaluation-gate/71-02-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-03-PLAN.md
  - .planning/phases/71-evaluation-gate/71-03-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-04-PLAN.md
  - .planning/phases/71-evaluation-gate/71-04-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-05-PLAN.md
  - .planning/phases/71-evaluation-gate/71-05-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-06-PLAN.md
  - .planning/phases/71-evaluation-gate/71-06-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-07-PLAN.md
  - .planning/phases/71-evaluation-gate/71-07-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-08-PLAN.md
  - .planning/phases/71-evaluation-gate/71-08-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-DECISION.md
  - .planning/phases/71-evaluation-gate/71-PATTERNS.md
  - .planning/phases/71-evaluation-gate/71-RESEARCH.md
  - .planning/phases/71-evaluation-gate/evidence/71-01-decisions.txt
  - .planning/phases/71-evaluation-gate/evidence/71-01-lock.txt
  - .planning/phases/71-evaluation-gate/evidence/71-02-tracer.txt
  - .planning/phases/71-evaluation-gate/evidence/71-03-corpus-inventory.txt
  - .planning/phases/71-evaluation-gate/evidence/71-04-contact-dxf.png
  - .planning/phases/71-evaluation-gate/evidence/71-04-contact-quality.png
  - .planning/phases/71-evaluation-gate/evidence/71-04-contact-robustness.png
  - .planning/phases/71-evaluation-gate/evidence/71-04-contact-views.png
  - .planning/phases/71-evaluation-gate/evidence/71-04-helpers.txt
  - .planning/phases/71-evaluation-gate/evidence/71-04-render.txt
  - .planning/phases/71-evaluation-gate/evidence/71-05-contact-f3d.png
  - .planning/phases/71-evaluation-gate/evidence/71-05-f3d.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-go-worker-rss.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-1g.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-2g.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp8.0.1.0.0.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-limits.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-quality.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-render-stacking.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-threads.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-landlock.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-libtrace.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-licences.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-prune-amd64.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-prune-arch.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-prune.txt
  - .planning/phases/71-evaluation-gate/evidence/71-08-image-size.txt
  - .planning/phases/71-evaluation-gate/evidence/71-08-no-wiring.txt
  - Dockerfile.cad-measure
  - Dockerfile.cad-worker
  - cmd/cad-worker/requirements-vtk.txt
  - cmd/cad-worker/vtk-keeplist.txt
  - cmd/cad-worker/vtk_keeplist.py
  - internal/convert/cad.go
  - internal/convert/cad_provenance_test.go
  - internal/convert/cadpixel.go
  - internal/convert/testdata/cad/PROVENANCE.md
  - internal/convert/testdata/cad/dxf-cr/cr_carriers.dxf
  - internal/convert/testdata/cad/dxf-cr/cr_carriers.render.svg
  - internal/convert/testdata/cad/dxf-cr/cr_carriers.svg
  - internal/convert/testdata/cad/dxf-cr/cr_carriers.texts.json
  - internal/convert/testdata/cad/dxf-cr/cr_carriers.verdict
  - internal/convert/testdata/cad/dxf-cr/cr_injected.render.svg
  - internal/convert/testdata/cad/dxf-cr/cr_injected.svg
  - internal/convert/testdata/cad/dxf-cr/cr_injected.texts.json
  - internal/convert/testdata/cad/dxf-cr/cr_injected.verdict
  - internal/convert/testdata/cad/dxf-out/alpha_one_visible.pdf
  - internal/convert/testdata/cad/dxf-out/alpha_one_visible.render.svg
  - internal/convert/testdata/cad/dxf-out/alpha_one_visible.svg
  - internal/convert/testdata/cad/dxf-out/alpha_one_visible.texts.json
  - internal/convert/testdata/cad/dxf-out/frames.tsv
  - internal/convert/testdata/cad/dxf-out/hatches_2.pdf
  - internal/convert/testdata/cad/dxf-out/hatches_2.render.svg
  - internal/convert/testdata/cad/dxf-out/hatches_2.svg
  - internal/convert/testdata/cad/dxf-out/hatches_2.texts.json
  - internal/convert/testdata/cad/dxf-out/legacy_cp1251.pdf
  - internal/convert/testdata/cad/dxf-out/legacy_cp1251.render.svg
  - internal/convert/testdata/cad/dxf-out/legacy_cp1251.svg
  - internal/convert/testdata/cad/dxf-out/legacy_cp1251.texts.json
  - internal/convert/testdata/cad/dxf-out/nofont.pdf
  - internal/convert/testdata/cad/dxf-out/outline.pdf
  - internal/convert/testdata/cad/dxf-out/tall.render.svg
  - internal/convert/testdata/cad/dxf-out/tall.svg
  - internal/convert/testdata/cad/dxf-out/tall.texts.json
  - internal/convert/testdata/cad/dxf-out/tall_256_opaque.png
  - internal/convert/testdata/cad/dxf-out/text_alpha.pdf
  - internal/convert/testdata/cad/dxf-out/text_alpha.render.svg
  - internal/convert/testdata/cad/dxf-out/text_alpha.svg
  - internal/convert/testdata/cad/dxf-out/text_alpha.texts.json
  - internal/convert/testdata/cad/dxf-out/text_alpha_faint.pdf
  - internal/convert/testdata/cad/dxf-out/text_alpha_faint.render.svg
  - internal/convert/testdata/cad/dxf-out/text_alpha_faint.svg
  - internal/convert/testdata/cad/dxf-out/text_alpha_faint.texts.json
  - internal/convert/testdata/cad/dxf-out/text_alpha_faint_1280_opaque.png
  - internal/convert/testdata/cad/dxf-out/text_alpha_faint_1280_transparent.png
  - internal/convert/testdata/cad/dxf-out/text_cases.pdf
  - internal/convert/testdata/cad/dxf-out/text_cases.render.svg
  - internal/convert/testdata/cad/dxf-out/text_cases.svg
  - internal/convert/testdata/cad/dxf-out/text_cases.texts.json
  - internal/convert/testdata/cad/dxf-out/text_fonts.pdf
  - internal/convert/testdata/cad/dxf-out/text_fonts.render.svg
  - internal/convert/testdata/cad/dxf-out/text_fonts.svg
  - internal/convert/testdata/cad/dxf-out/text_fonts.texts.json
  - internal/convert/testdata/cad/dxf-out/uncommon.pdf
  - internal/convert/testdata/cad/dxf-out/uncommon.render.svg
  - internal/convert/testdata/cad/dxf-out/uncommon.svg
  - internal/convert/testdata/cad/dxf-out/uncommon.texts.json
  - internal/convert/testdata/cad/dxf-out/units_3.measure.json
  - internal/convert/testdata/cad/dxf-out/white_one_black.pdf
  - internal/convert/testdata/cad/dxf-out/white_one_black.render.svg
  - internal/convert/testdata/cad/dxf-out/white_one_black.svg
  - internal/convert/testdata/cad/dxf-out/white_one_black.texts.json
  - internal/convert/testdata/cad/dxf-out/wide.measure.json
  - internal/convert/testdata/cad/dxf-out/wide.render.svg
  - internal/convert/testdata/cad/dxf-out/wide.svg
  - internal/convert/testdata/cad/dxf-out/wide.texts.json
  - internal/convert/testdata/cad/dxf-out/wide_1280_opaque.png
  - internal/convert/testdata/cad/dxf-out/wide_1280_transparent.png
  - internal/convert/testdata/cad/dxf-out/wrong_extents.measure.json
  - internal/convert/testdata/cad/dxf/all_hidden.dxf
  - internal/convert/testdata/cad/dxf/alpha_clear_all.dxf
  - internal/convert/testdata/cad/dxf/alpha_clear_geometry.dxf
  - internal/convert/testdata/cad/dxf/alpha_one_fill.dxf
  - internal/convert/testdata/cad/dxf/alpha_one_visible.dxf
  - internal/convert/testdata/cad/dxf/coord_1e300.dxf
  - internal/convert/testdata/cad/dxf/coord_1e400.dxf
  - internal/convert/testdata/cad/dxf/cyclic_ab.dxf
  - internal/convert/testdata/cad/dxf/cyclic_case.dxf
  - internal/convert/testdata/cad/dxf/cyclic_self.dxf
  - internal/convert/testdata/cad/dxf/cyclic_unreferenced.dxf
  - internal/convert/testdata/cad/dxf/degenerate_fill.dxf
  - internal/convert/testdata/cad/dxf/dimension_minsert.dxf
  - internal/convert/testdata/cad/dxf/entities_over.dxf
  - internal/convert/testdata/cad/dxf/faint_only.dxf
  - internal/convert/testdata/cad/dxf/far_point.dxf
  - internal/convert/testdata/cad/dxf/header_far_extents.dxf
  - internal/convert/testdata/cad/dxf/layers_off_frozen.dxf
  - internal/convert/testdata/cad/dxf/minsert_over_inserts.dxf
  - internal/convert/testdata/cad/dxf/nest_d16.dxf
  - internal/convert/testdata/cad/dxf/nest_d17.dxf
  - internal/convert/testdata/cad/dxf/never_resolved.dxf
  - internal/convert/testdata/cad/dxf/nonfinite_header.dxf
  - internal/convert/testdata/cad/dxf/nonfinite_objects.dxf
  - internal/convert/testdata/cad/dxf/nonfinite_tables.dxf
  - internal/convert/testdata/cad/dxf/nonnumeric_abc.dxf
  - internal/convert/testdata/cad/dxf/nonnumeric_comma.dxf
  - internal/convert/testdata/cad/dxf/nonnumeric_inf.dxf
  - internal/convert/testdata/cad/dxf/nonnumeric_nan.dxf
  - internal/convert/testdata/cad/dxf/nonnumeric_underscore.dxf
  - internal/convert/testdata/cad/dxf/single_point.dxf
  - internal/convert/testdata/cad/dxf/tall.dxf
  - internal/convert/testdata/cad/dxf/text_alpha.dxf
  - internal/convert/testdata/cad/dxf/text_alpha_faint.dxf
  - internal/convert/testdata/cad/dxf/text_cases.dxf
  - internal/convert/testdata/cad/dxf/text_fonts.dxf
  - internal/convert/testdata/cad/dxf/text_illegal_chars.dxf
  - internal/convert/testdata/cad/dxf/units_0.dxf
  - internal/convert/testdata/cad/dxf/units_1.dxf
  - internal/convert/testdata/cad/dxf/units_13.dxf
  - internal/convert/testdata/cad/dxf/units_2.dxf
  - internal/convert/testdata/cad/dxf/units_21.dxf
  - internal/convert/testdata/cad/dxf/units_3.dxf
  - internal/convert/testdata/cad/dxf/units_4.dxf
  - internal/convert/testdata/cad/dxf/units_5.dxf
  - internal/convert/testdata/cad/dxf/units_6.dxf
  - internal/convert/testdata/cad/dxf/units_missing.dxf
  - internal/convert/testdata/cad/dxf/white_geometry.dxf
  - internal/convert/testdata/cad/dxf/white_one_black.dxf
  - internal/convert/testdata/cad/dxf/white_text.dxf
  - internal/convert/testdata/cad/dxf/wide.dxf
  - internal/convert/testdata/cad/dxf/wrong_extents.dxf
  - internal/convert/testdata/cad/dxf/xline_ray.dxf
  - internal/convert/testdata/cad/dxf/zwsp_text.dxf
  - internal/convert/testdata/cad/public/2020_corner_bracket-Corner.step
  - internal/convert/testdata/cad/public/3dsolids.dxf
  - internal/convert/testdata/cad/public/LM8UU.step
  - internal/convert/testdata/cad/public/LinearSlide-MGN15x-Block.step
  - internal/convert/testdata/cad/public/SC8UU.step
  - internal/convert/testdata/cad/public/V-Slot-20x20-100mm.step
  - internal/convert/testdata/cad/public/acad_table_with_blk_ref.dxf
  - internal/convert/testdata/cad/public/columns_R2007.dxf
  - internal/convert/testdata/cad/public/dimension_in_nested_blocks.dxf
  - internal/convert/testdata/cad/public/hatches_1.dxf
  - internal/convert/testdata/cad/public/hatches_2.dxf
  - internal/convert/testdata/cad/public/houses_of_parliament_georeferenced.dxf
  - internal/convert/testdata/cad/public/nasa-hubble-all-pieces.stl
  - internal/convert/testdata/cad/public/nasa-jwst-all-pieces-without-supports.stl
  - internal/convert/testdata/cad/public/nist-d2mi-827-9999-906.stp
  - internal/convert/testdata/cad/public/nist_ctc_01_asme1_ap242-e1.stp
  - internal/convert/testdata/cad/public/nist_ctc_05_asme1_ap242-e1.stp
  - internal/convert/testdata/cad/public/nist_stc_06_asme1_ap242-e3.stp
  - internal/convert/testdata/cad/public/uncommon.dxf
  - internal/convert/testdata/cad/reject/dwg_ac1027.dwg
  - internal/convert/testdata/cad/reject/dxf_binary.dxf
  - internal/convert/testdata/cad/reject/dxf_bom.dxf
  - internal/convert/testdata/cad/reject/dxf_truncated.dxf
  - internal/convert/testdata/cad/reject/iges_truncated.iges
  - internal/convert/testdata/cad/reject/step_bom.step
  - internal/convert/testdata/cad/reject/step_missing_header.step
  - internal/convert/testdata/cad/reject/step_trailing_nul.step
  - internal/convert/testdata/cad/reject/step_truncated.step
  - internal/convert/testdata/cad/reject/stl_ambiguous.stl
  - internal/convert/testdata/cad/reject/stl_ascii_truncated.stl
  - internal/convert/testdata/cad/reject/stl_binary_ac10_header.stl
  - internal/convert/testdata/cad/reject/stl_binary_empty.stl
  - internal/convert/testdata/cad/reject/stl_binary_trailing_byte.stl
  - internal/convert/testdata/cad/reject/stl_binary_truncated.stl
  - internal/convert/testdata/cad/render-bg/degenerate_plate_front_1280_opaque.png
  - internal/convert/testdata/cad/render-bg/degenerate_plate_front_1280_transparent.png
  - internal/convert/testdata/cad/render-bg/degenerate_rod_iso_256_opaque.png
  - internal/convert/testdata/cad/render-bg/degenerate_rod_iso_256_transparent.png
  - internal/convert/testdata/cad/render-bg/linearslide_iso_256_opaque.png
  - internal/convert/testdata/cad/render-bg/linearslide_iso_256_transparent.png
  - internal/convert/testdata/cad/render-bg/nauo4_iso_1280_opaque.png
  - internal/convert/testdata/cad/render-bg/nauo4_iso_1280_transparent.png
  - internal/convert/testdata/cad/render-bg/w15_iso_256_opaque.png
  - internal/convert/testdata/cad/render-bg/w15_iso_256_transparent.png
  - internal/convert/testdata/cad/synthetic/assembly_nauo4.step
  - internal/convert/testdata/cad/synthetic/asymmetric_l_bracket.step
  - internal/convert/testdata/cad/synthetic/blocks_minsert.dxf
  - internal/convert/testdata/cad/synthetic/comments_999.dxf
  - internal/convert/testdata/cad/synthetic/degenerate_plate.step
  - internal/convert/testdata/cad/synthetic/degenerate_rod.step
  - internal/convert/testdata/cad/synthetic/iges_no_newlines.iges
  - internal/convert/testdata/cad/synthetic/legacy_cp1251.dxf
  - internal/convert/testdata/cad/synthetic/legacy_cp1251.step
  - internal/convert/testdata/cad/synthetic/legacy_latin1.dxf
  - internal/convert/testdata/cad/synthetic/legacy_latin1.step
  - internal/convert/testdata/cad/synthetic/open_shell.step
  - internal/convert/testdata/cad/synthetic/reversed_solid.step
  - internal/convert/testdata/cad/synthetic/single_face.step
  - internal/convert/testdata/cad/synthetic/stl_ascii_box.stl
  - internal/convert/testdata/cad/synthetic/stl_binary_solid_header.stl
  - internal/convert/testdata/cad/synthetic/surfaces_only.iges
  - internal/convert/testdata/cad/synthetic/units_inch.step
  - scripts/cad-corpus-fetch.sh
  - scripts/cad-corpus-gen.py
  - scripts/cad-corpus-inventory.sh
  - scripts/cad-corpus-manifest.tsv
  - scripts/cad-derive-limits.py
  - scripts/cad-f3d-yardstick.sh
  - scripts/cad-go-worker-rss.sh
  - scripts/cad-image-size-measure.sh
  - scripts/cad-image-trace.sh
  - scripts/cad-measure/cad_dxf.py
  - scripts/cad-measure/cad_mesh.py
  - scripts/cad-measure/cad_render.py
  - scripts/cad-measure/common.sh
  - scripts/cad-measure/contact_sheet.py
  - scripts/cad-measure/counters.py
  - scripts/cad-measure/lock.sh
  - scripts/cad-measure/measure_wrap.py
  - scripts/cad-measure/pixelcheck.py
  - scripts/cad-measure/requirements-common.txt
  - scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt
  - scripts/cad-measure/requirements-ocp-8.0.1.0.0.txt
  - scripts/cad-measure/requirements-vtk.txt
  - scripts/cad-measure/verdicts.sh
  - scripts/cad-measure/vtk-keeplist.txt
  - scripts/cad-measure/vtk_keeplist.py
  - scripts/cad-prune-amd64-check.sh
  - scripts/cad-prune-identity.sh
  - scripts/cad-render-measure.sh
  - scripts/cad-render-probe.sh
  - scripts/cad-render-stacking-probe.py
  - scripts/cad-rss-time-measure.sh
  - scripts/cad-sandbox-measure.sh
  - scripts/cad-vtk-keeplist-test.py
  - scripts/cmd/cad-pixel-judge/main.go
covered_digest: "v3:sha256:2a4cdd97b7f87e4dca2af8b6953e42fc51efa364b0e7437c883a4d51bbc3d8b2"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 7/8
  previous_head: "c38cd9c (main, PR #37 merged; the 2026-10-17 pass)"
  this_head: "ee30f2e (branch gsd/v1.16-phase71-reverify-provenance: c38cd9c plus 325b303, c40f7c9, 6d1ce13 and ee30f2e of quick 261018-44q)"
  trigger: "quick 261018-44q, gap closure by option (a): per-file PROVENANCE sections for the 126 later files, the phase-71 sweeps scoped to the phase-71 corpus (W24), the render probe on the Go judge (the 2026-10-17 advisory)"
  gaps_closed:
    - "Every committed file of the corpus in internal/convert/testdata/cad has a PROVENANCE.md section with source, author, licence from the D-04 list and a sha256 equal to its content (71-03 must-have; enforced by the 71-03 gates CORPUS-OK and PROVENANCE-COMPLETE)"
  gaps_remaining: []
  regressions: []
gaps: []
human_verification: []
---

# Phase 71: Evaluation Gate Verification Report

**Phase Goal:** Выбор ядра геометрии, рендерера и 2D-пути DXF сделан по замеру на настоящем образе `debian:bookworm-slim` на нативной архитектуре, а не по прототипу; числа, от которых зависит всё остальное (RSS, время, размер образа, лимиты входа, лицензии), известны до того, как написана строка продакшен-кода.
**Verified:** 2026-10-18T01:05:40Z
**Status:** passed (8/8; every docker-only gate stays NOT RUN and is not counted)
**Re-verification:** Yes, after quick 261018-44q. The 2026-10-17 pass found truth 8 failed on main at c38cd9c (gaps_found, 7/8). This pass re-checks the fixed branch at ee30f2e, which is c38cd9c plus four quick-task commits: 325b303 (RED Go test), c40f7c9 (PROVENANCE), 6d1ce13 (W24 sweeps), ee30f2e (render probe on the Go judge). The pass before that was `passed` 7/7 at b6269c6 on 2026-10-05 and is in git history.

## What changed since the 2026-10-17 pass

- `git diff --stat c38cd9c ee30f2e` names seven files: `internal/convert/cad_provenance_test.go` (new), `internal/convert/testdata/cad/PROVENANCE.md`, `scripts/cad-measure/common.sh`, `scripts/cad-corpus-inventory.sh`, `scripts/cad-rss-time-measure.sh`, `scripts/cad-image-trace.sh` and `scripts/cad-render-probe.sh`. Nothing under `.planning/phases/71-evaluation-gate/` except this report, and no file under `testdata/cad` other than PROVENANCE.md, changed.
- PROVENANCE.md: one per-file section for each of the 126 later files, each sha256 the re-hash of the committed file; for the 116 phase-75 files it equals an evidence line with the same name and bytes, and the 10 render-bg sections say their sha256 was computed from the committed blob because 056a8d2 recorded none. The three `- Licence: generated.` lines are split into `- Licence: generated` and `- Origin: ...` with the wording kept. `git diff c38cd9c -- PROVENANCE.md` has 4 removed lines: the three licence lines and the one dxf-out sentence that named 75-02-helper.txt as the source of every sha256.
- `internal/convert/cad_provenance_test.go` makes the 71-03 contract part of `go test ./internal/convert/`.
- `pixelcheck.py`, `Dockerfile.cad-measure`, every `scripts/cad-measure/*.py` and `internal/convert/exec.go` are unchanged, so the measurement image tag of the phase-71 evidence still hashes the same inputs.

## Scope and method

- Tree identity: `git rev-parse 40a4016^{tree} c38cd9c^{tree}` gives `14238c6...` for both, and `git diff --name-only 40a4016 c38cd9c` is empty. That holds for every file, `.planning` included.
- Since the phase-71 merge 9117f60, nothing under `.planning/phases/71-evaluation-gate/` has changed: `71-DECISION.md`, all evidence files, the PLANs and the SUMMARYs are byte-identical. Five phase-71 artifacts were changed by later phases: `internal/convert/testdata/cad/PROVENANCE.md` (append only, no `-` lines), `scripts/cad-corpus-gen.py`, `scripts/cad-corpus-inventory.sh`, `scripts/cad-image-trace.sh` and `scripts/cad-rss-time-measure.sh`. In the last four the change is a `! -path '*/reject/*'` exclusion, plus new generator kinds in the corpus generator. The other phase-71 scripts, `pixelcheck.py` and `Dockerfile.cad-measure` are unchanged.
- No docker, kubectl, helm or live stack was used. Commands that need docker are marked NOT RUN below and are not counted as passes.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1. The core, renderer and DXF path were decided by measurement on the real bookworm-slim image, on the native arch, under Landlock, with pixel judgement on real parts | VERIFIED | `71-DECISION.md` and the evidence it cites have not changed since 9117f60 (the previous pass checked the numbers). Goal-backward on main: the production image `Dockerfile.cad-worker:1,21` uses the same base digest `debian:bookworm-slim@sha256:3783cc...`, VTK + `libosmesa6`, OCP with `cadquery-ocp-novtk==7.9.3.1.1` (the recorded user pin), and ezdxf + `rsvg-convert` for DXF. `cmd/cad-worker/{requirements-vtk.txt,vtk_keeplist.py,vtk-keeplist.txt}` are byte-identical to the phase-71 sources in `scripts/cad-measure/`. The decisions held. The later replacement of the pixel judge does not falsify SC1, see "Pixel judge follow-up" below. W3 (no non-OCP core was measured) is carried. |
| 2 | SC2. The repo has a corpus of real CAD files, or a fetch script with checksums, with a licence recorded for each file and every EVAL-02 category covered | VERIFIED | The 37 phase-71 corpus files are byte-unchanged since 9117f60. I re-hashed all 37: each sha256 and basename is in `PROVENANCE.md` (0 missing). `scripts/cad-corpus-fetch.sh --check` gives `corpus fetch ok: 17 files`. `python3 scripts/cad-corpus-gen.py --check-only` gives `self-check passed`. TAMPER-DETECTED and SELF-CHECK-FIRES print their markers. The manifest has 0 licences outside D-04. Each later fixture group has a licence statement (`generated`, with sources named). Per-file checksums are the separate plan must-have in truth 8. |
| 3 | SC3. At `--cpus=2`, RSS, time, image size and cold pull are recorded, and the input limits are derived from measurement with the derivation shown | VERIFIED | `cad-derive-limits.py --self-check` gives `SELF-CHECK-OK,...` with all 11 flags. `cad-derive-limits.py <scratch copy> --pin 7.9.3.1.1 --out` regenerates `71-06-limits.txt` byte for byte (`cmp` is silent). The plan's LIMITS-OK command, run on a scratch copy, prints `LIMITS-OK`. Without a pin the deriver exits 2 (`refusing to derive without an explicit pin decision`). LADDER-OK and MEASURES-OK print their markers. Goal-backward on main: production `internal/convert/cad.go:24-41` carries the phase-71 numbers STEP 63 MiB, DXF 118 MiB and caps 754762 / 1518 / 170031. IGES 120 MiB, STL 190840832 B and the timeouts were set later from later measurements. That fits a phase whose job was to know the numbers first. |
| 4 | SC4. The licences of libfreeimage, libraw and libjasper were checked for internal use and the decision recorded | VERIFIED as an engineering record, NOT legal clearance | Evidence unchanged. D18-OK prints `D18-OK`. The judgement of the previous pass stands: `LAWYER_OK=needed,blocking=no`. |
| 5 | SC5. No production wiring by phase 71 | VERIFIED (historical) | Checked on the phase-71 branch head b6269c6 against base 4e595c0. The only `M` entries are `.planning/{REQUIREMENTS,ROADMAP,STATE}.md`. 0 added files lie outside `.planning/`, `scripts/`, `Dockerfile.cad-measure` and `internal/convert/testdata/cad/`. 0 `.go` files were added under `internal/`. The trees of 9117f60 and b6269c6 are identical. Production wiring on main (AllEngines, `/v1/formats`, chart) belongs to phases 72-76 and does not regress this criterion. NO-WIRING itself is NOT RUN (it needs docker and is defined against HEAD). |
| 6 | Every number in `71-DECISION.md` traces to an evidence line | VERIFIED (regression check) | DECISION and evidence are byte-unchanged and the limits regenerate identically. DECISION-OK prints `DECISION-OK`. |
| 7 | User decisions are recorded consistently (lock approval, D-18, libssl3 for f3d only, renderer approval, pin 7.9.3.1.1) | VERIFIED (regression check) | Records unchanged. On main the pin is still 7.9.3.1.1 in both `Dockerfile.cad-measure` and the production requirements. |
| 8 | 71-03 must-have. Every committed corpus file has a PROVENANCE.md section with source, author, a D-04 licence and a matching sha256 | VERIFIED (re-verified after quick 261018-44q; FAILED on c38cd9c) | Both 71-03 gates, extracted verbatim from `71-03-PLAN.md:162,164` and run as written with bash on ee30f2e: CORPUS-OK `rc=0`, last line `CORPUS-OK`; PROVENANCE-COMPLETE `rc=0`, last line `PROVENANCE-COMPLETE` (both were `rc=1` on c38cd9c, quick evidence `red-gates.txt`). `TestCADProvenanceCoversEveryCommittedFile` passes: it walks all 178 files and requires for each exactly one section `` ## `<rel>` — <bytes> bytes `` with the true size, exactly one sha256 line equal to the content and exactly one licence line in the D-04 allow-list, plus the CORPUS-OK licence and size stages; on c38cd9c it failed with 129 problems (126 files without a section, 3 licence lines). Mutations caught by the test and the matching gate: a changed sha256 digit, a period after a licence token, a removed render-bg section, an extra file. The reject marked region still equals `cad-corpus-gen.py --reject --provenance`, and the `dxf-replicate` Derived table that `cad-dxf-probe.sh prov_sha` reads gives the same 7 values. |

**Score:** 8/8 verified, 0 present but behavior-unverified. None of these truths is a state-transition or cleanup invariant: they are records checked against evidence and code.

### Pixel judge follow-up (asked explicitly)

The question: phase 71's `scripts/cad-measure/pixelcheck.py` was not updated when later phases replaced the pixel judge (`internal/convert/cadpixel.go`, `cadMeasure`, the all-channel `cadBackgroundCutoff` rule, and 056a8d2 removing the 5%..95% transparent band).

Correction of premise: `scripts/cad-render-probe.sh` is not phase-71 tooling. Its first commit is 44cdf78 `feat(74-01)` and its judge mode is a9563a0 `feat(74-06)`. It is not in any phase-71 PLAN, SUMMARY or the DECISION artifact index.

What differs, from reading both judges:
- Ink rule: same in both. pixelcheck counts a pixel as ink when `(img < 250).any(axis=2)`; Go skips a pixel only when all three channels are >= `cadBackgroundCutoff` (250). Dark is `< 80` on all channels in both.
- Alpha: pixelcheck does `convert("RGB")` without compositing, while Go composites straight alpha on white and skips alpha-0 pixels. This makes no difference on cad-render output: the transparent and opaque twin of each committed render-bg pair give pixelcheck the same frac (for example nauo4 0.0429 on both).
- Part rule: pixelcheck uses frac only (0.02..0.90), plus a sliver mode that needs a `proj_aspect` sidecar. Go adds J2 (long side at least 0.5 of the frame) and the thin rule. This is the W15 difference already recorded in phase 71.
- Transparent: pixelcheck `transparent` still requires `frac_transparent` in 0.05..0.95. Go now requires only alpha present and a fully transparent edge ring.

Non-live run (pinned numpy 2.4.6 / pillow 12.3.0 in a scratch venv; Go judge built with go1.26.4) on the 10 committed `render-bg/` PNGs:

| Frame | Go | pixelcheck single | pixelcheck transparent |
|---|---|---|---|
| nauo4_iso_1280 opaque / transparent | ok=1 blob / ok=1 blob | 1 / 1 | - / 0 (ft 0.9577) |
| linearslide_iso_256 opaque / transparent | 1 blob / 1 blob | 1 / 1 | - / 1 (ft 0.7218) |
| w15_iso_256 opaque / transparent | 1 thin / 1 thin | 0 / 0 | - / 0 (ft 0.9976) |
| degenerate_plate_front_1280 | 1 thin / 1 thin | 0 / 0 | - / 0 |
| degenerate_rod_iso_256 | 0 / 0 | 0 / 0 | - / 0 |

Judgement: **fine for phase 71, with one warning outside phase 71.**
- No phase-71 claim becomes stale or false. `pixelcheck.py` is phase 71's measurement instrument. DECISION records its thresholds as the calibrated D-11 step-1 criteria and never hands them to production. It also says outright that `pixelcheck.py` rejects a thin but real part (DECISION, Codex P1 paragraph; W15). Every phase-71 verdict it produced (RENDER_SUMMARY 108 PNGs; `TRANSPARENT,item=slide,frac_transparent=0.7240,ok=1`; 20 Landlock render rows; the ladder `pix=` cells) is still true of that instrument, and the instrument is unchanged. Its one effect on a limit makes the limit more conservative, not less: rejecting the W15 point moved the STL ASCII ceiling 1353 -> 877. Production does not use that ceiling (`CADMaxSTLBytes` comes from phase 74's frame-cost bound).
- `pixelcheck.py` still runs as documented. It parses, its CLI is unchanged, and it ran on the host with the locked numpy/pillow versions. Inside the measurement image it runs from `/opt/cad-measure` (not re-run, needs docker).
- `cad-render-probe.sh judge` was out of date relative to 056a8d2 (the 2026-10-17 advisory). Resolved by quick 261018-44q (ee30f2e): every frame verdict of the probe (tracer, cost, judge, blank frames) now comes from the Go judge built from `scripts/cmd/cad-pixel-judge`, and `pixelcheck.py` remains in the probe only for the identity `same` comparison. A judge that does not run is `ok=error` and fails the run, never a refusal. The new `judge-png` mode needs no docker: on the 10 committed render-bg PNGs it returns the verdicts of `cadRealPairs` (`internal/convert/cadpixelbg_test.go:310-316`), including `nauo4_iso_1280_transparent` accepted as blob, refuses that frame with `rule=background` when judged as opaque, reports a missing path as `ok=error` with exit 1, refuses both blank frames by J1, and logs no docker call with a stub docker first on PATH. The tracer, cost and judge modes themselves need docker and were not run (W26).
- The pixelcheck/Go table above was re-run on ee30f2e with the same pinned venv and the Go judge built with go1.26.4, and is equal (quick evidence `pixel-table.txt`). `pixelcheck.py` is byte-unchanged.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `71-DECISION.md` | Decision record with evidence refs | VERIFIED | unchanged since 9117f60; DECISION-OK |
| `evidence/71-0{1..8}-*` (30 files) | Primary evidence | VERIFIED | unchanged since 9117f60 |
| `Dockerfile.cad-measure` | Measurement image | VERIFIED | unchanged; still not referenced by compose, chart or CI |
| `scripts/cad-*`, `scripts/cad-measure/*` (27 scripts incl. cad-render-probe.sh) | Measurement scripts | VERIFIED (W24 resolved) | `bash -n` / `ast.parse` clean on all 27; self-tests green (table below); `shellcheck -S warning` exits 0 on the five scripts quick 261018-44q touched |
| `internal/convert/testdata/cad/` + `PROVENANCE.md` | Corpus with licences | VERIFIED | phase-71 corpus intact; the directory-wide per-file contract holds for all 178 files and is enforced by `TestCADProvenanceCoversEveryCommittedFile` (truth 8) |
| `scripts/cad-corpus-manifest.tsv` + `cad-corpus-fetch.sh` | Checksummed fetch | VERIFIED | `--check` 17 files; tamper detected |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| DECISION summary lines | `71-06-limits.txt` etc. | `evidence=file#LABEL` | WIRED | unchanged |
| `71-06-limits.txt` | raw ladders | `cad-derive-limits.py --pin 7.9.3.1.1` | WIRED | byte-identical regeneration on main |
| phase-71 choices | production CAD image | `Dockerfile.cad-worker`, `cmd/cad-worker/*` lock copies | WIRED | same base digest, OCP pin, VTK lock and keep-list (cmp equal) |
| phase-71 limits | production bounds | `internal/convert/cad.go` | WIRED | STEP 63, DXF 118 and three caps taken over; the rest superseded by later measurements |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Formatting | `gofmt -l .` | empty | PASS |
| Vet | `GOTOOLCHAIN=go1.26.4 go vet ./...` | rc=0, no output | PASS |
| Phase-71-relevant Go tests | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/... ./internal/api/... ./internal/queue/... ./scripts/cmd/cad-png-compare/... ./scripts/cmd/cad-pixel-judge/...` (python 3.14 first on PATH) at ee30f2e | `ok` x4 (convert 357.4 s, api 8.5 s, queue 0.7 s, cad-png-compare 1.7 s), cad-pixel-judge has no test files, rc=0 | PASS |
| Provenance contract test | `go test -count=1 -run '^TestCADProvenanceCoversEveryCommittedFile$' ./internal/convert/` | `ok` (also part of the convert run above) | PASS |
| Full suite | `go test ./... -count=1` | not re-run on ee30f2e; the last full run is the one cited by the 2026-10-17 pass for 40a4016, a tree that no longer equals this one | NOT RUN |
| Script syntax | `bash -n` / `ast.parse` on the 27 scripts of the covered list (cad-render-probe.sh included) | `files=27,failures=0` | PASS |
| Deriver self-check / regeneration / pin guard | see truth 3, on a scratch copy of the evidence directory | SELF-CHECK-OK; `cmp` silent; exit 2 without pin | PASS |
| Verdict / resume / trace / keep-list self-tests | `verdicts.sh --self-test`; `cad-rss-time-measure.sh --self-test`; `cad-image-trace.sh self-test`; `cad-vtk-keeplist-test.py` | `cases=44,failed=0`; `cases=89,failed=0` (78 before, plus 11 input-selection cases); `cases=51,failed=0` (41 before, plus 10); `cases=10,failed=0`; all rc=0 | PASS |
| Corpus generator / fetch / tamper | `--check-only`; `--check`; TAMPER-DETECTED | `self-check passed`; `17 files`; `TAMPER-DETECTED` | PASS |
| Plan gates, grep-only (run as written, TMPDIR in scratch) | LOCKS-OK, SELF-CHECK-FIRES, TAMPER-DETECTED, LADDER-OK, MEASURES-OK, LIMITS-OK (scratch copy), PRUNE-OK, D18-OK, LANDLOCK-OK, DECISION-OK | each prints its marker, rc=0 | PASS |
| Plan gate CORPUS-OK (71-03) | as written | rc=0, `CORPUS-OK` (rc=1 on c38cd9c) | PASS |
| Plan gate PROVENANCE-COMPLETE (71-03) | as written | rc=0, `PROVENANCE-COMPLETE`, files=178 (rc=1 and 126 missing on c38cd9c) | PASS |
| W24 sweep selection | quick evidence `w24-proof.sh`, against 71-03-corpus-inventory.txt, 71-04-helpers.txt and 71-07-prune.txt | 37 inventory pairs equal, 37 ladder names equal, 25 prune 3D inputs equal, 49 PRUNE_OTHER pairs, no corpus walk outside common.sh; `W24-OK` | PASS |
| Render probe judge-png | quick evidence `judge-png-proof.sh` | 10 of 10 equal to `cadRealPairs`; background, missing path, blank frames and docker-stub controls; `JUDGE-PNG-OK` | PASS |
| Pixel judges differential | pixelcheck.py vs cad-pixel-judge on 10 committed render-bg PNGs | equal to the table above; `PIXEL-TABLE-EQUAL` | INFO |
| Phase-71 evidence untouched | `git status --porcelain .planning/phases/71-evaluation-gate/evidence` after every run | empty | PASS |
| Plan gates that need docker | HASHES-OK, TRACER-OK, GEN-DETERMINISTIC, INVENTORY-OK, PRIVACY-OK, HELPERS-OK, RENDER-OK, F3D-OK (re-render), STALE-REFUSED, RECHECK-OK, SIZE-OK, NO-WIRING | - | NOT RUN |

### Probe Execution

No `probe-*.sh` declared by the phase. SKIPPED.

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| EVAL-01 | 71-01, 02, 04, 05, 06, 07, 08 | core / renderer / DXF path by measurement, native, Landlock, pixel content | SATISFIED | truth 1; choices carried into production |
| EVAL-02 | 71-02, 03, 08 | corpus with licences | SATISFIED; the plan contract regressed on c38cd9c and is restored on ee30f2e | truths 2 and 8 |
| EVAL-03 | 71-03, 06, 08 | RSS/time at cpus=2, image size, cold pull, derived limits | SATISFIED | truth 3 |
| EVAL-04 | 71-01, 07, 08 | licences checked for internal use, decision recorded | SATISFIED as an engineering record; legal OK open | truth 4 |

REQUIREMENTS.md marks EVAL-01..04 `[x]` / Complete and maps them to Phase 71 only. No orphaned IDs.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| PROVENANCE.md, cad_provenance_test.go, common.sh and the 4 modified phase-71 scripts, pixelcheck.py, cad-render-probe.sh, cadpixel.go | - | `TBD`/`FIXME`/`XXX`/`TODO` (word match, `mktemp XXXXXX` excluded), re-run on ee30f2e | none found | - |

### Warnings (non-blocking)

- W23 (updated). `pixelcheck.py` is not aligned with the production judge. This is fine for phase 71 (reasons above), and the instrument is unchanged. The stale `cad-render-probe.sh` judge rule of the 2026-10-17 advisory is gone: since ee30f2e the probe takes every frame verdict from the Go judge and calls `pixelcheck.py` only for the identity `same` comparison, which the Go judge does not provide.
- W24 (resolved by 6d1ce13). The three phase-71 sweeps now select their inputs through `scripts/cad-measure/common.sh` (`cad_corpus_files` for the inventory filter, `cad_corpus_cad_files` for the ladder filter). Given the corpus root they walk only `public/` and `synthetic/`; any other directory is walked as before; a missing directory, or a root without `public/` or `synthetic/`, is a non-zero status, never an empty success. The inventory container reads the host list on stdin and runs no find of its own. `corpus_other_expected` and both prune input loops of `cad-image-trace.sh` call the same function as `list_cad_files` of `cad-rss-time-measure.sh`, so prune-check expects what the ladder selects. Proven without docker against the phase-71 evidence (quick evidence `w24-red.txt`, `w24-green.txt`, `mutations-w24.txt`):
  - inventory committed group: 163 files on c38cd9c, now 37 (public 19, synthetic 18), whose basename and sha256 pairs equal the 37 committed CNT lines of `71-03-corpus-inventory.txt`; a stub docker run of the inventory (`inventory-stub.txt`) shows 37 `/corpus/<rel>` lines on the container's stdin, in public/ then synthetic/ order, `INV,group=committed,files=37`, and no private name in the output;
  - ladder `list_cad_files`: 92 on c38cd9c, now 37, whose names equal the 37 names of `71-04-helpers.txt`;
  - prune-check: 176 expected pairs on c38cd9c, now 49 (25 3D + 2 x 12 DXF); the 3D inputs in prune naming equal the 25 corpus PRUNE_PNG inputs of `71-07-prune.txt`. The phase-71 PRUNE_OTHER line itself reads `rows=48,same=48,diff=0`, written before the `expected=` field existed; this pass does not re-run prune-check (docker);
  - mutations: dropping `synthetic` from the groups fails proof checks (a)-(d) and both self-tests; a root comparison that never matches shows 163 and 92 again; removing the reject exclusion fails the self-test reject cases.
- W25 (new, recorded, not fixed). The local cache `$HOME/.cache/octoconv/cad-corpus` is outside the repository. It holds the 17 fetched files, but of the 33 scaled files the phase-71 inventory counted only 5 are present (the five `dxf-replicate-uncommon-*` targets), beside 77 later entries (`dxf-farpoint` 61, `dxf-minsert` 12, `dxf-replicate` 2, `dxf-combo` 1, `b76/` 1). A re-run of the scaled inventory group or of the ladder on the cache would not reproduce the phase-71 scaled input set until the generator rebuilds it. Read with `ls` only (quick evidence `w25-cache.txt`).
- W26 (new). The 74-06 Task 1 verify greps `^JUDGE_SUMMARY,.*unexplained_divergence=0` (`74-06-PLAN.md:122`). That field described the pixelcheck/Go differential, which ee30f2e retired; a re-run of the judge mode prints `JUDGE_SUMMARY` with `judge_errors=` and `blank_refused=` instead and will not match that grep. Recorded in the Follow-up line of `74-06-SUMMARY.md`; `evidence/74-06-judge.txt` stays the record of the differential, not re-run and not edited. The tracer, cost and judge modes of the probe need docker and were not re-run after the change.
- Sweep note. `scripts/cad-dxf-probe.sh` (phase 75) keeps its own pixelcheck/Go drawing differential; it is out of the scope of quick 261018-44q and unchanged. No other script walks the corpus root: the sweep in quick evidence `w24-sweep.txt` finds only explicit file or subdirectory names outside `common.sh`.
- W1-W22 of the 2026-10-05 pass are carried as recorded there (git b6269c6). They were not re-judged, except that later phases now own W1 (RSS on pruned), W7 (render time guard: `CADDefaultRenderTimeout` exists in `cad.go`) and W17 (per-arch Landlock sets).
- Criterion 4: legal OK for libfreeimage / libraw / jbigkit / libgomp is still open (`LAWYER_OK=needed,blocking=no`).

### Human Verification Required

None. The fix option was decided before this pass: option (a) of the 2026-10-17 report, taken by quick 261018-44q.

### Gaps Summary

No gap remains. The 2026-10-17 pass found one gap with one root cause: phases after 71 put their fixtures and helper outputs into the phase-71 corpus directory `internal/convert/testdata/cad` (phase 75 `dxf/`, `dxf-out/` and `dxf-cr/`; quick 261014-0lo `render-bg/`) without the phase-71 per-file PROVENANCE contract, so CORPUS-OK and PROVENANCE-COMPLETE were red on main, 10 committed PNGs had no checksum anywhere, and the phase-71 sweeps no longer reproduced the phase-71 input sets (W24).

Option (a) was taken (quick 261018-44q):
- each of the 126 files has its own section with a verified sha256 and `- Licence: generated`, and the three group licence lines are split so the allow-list accepts them (c40f7c9);
- the 10 render-bg PNGs now carry the sha256 of their committed blob, unchanged since 056a8d2, with that origin stated;
- `TestCADProvenanceCoversEveryCommittedFile` makes the contract part of `go test`, so the next fixture directory without per-file sections fails the package tests, not only a phase-71 gate (325b303);
- the three sweeps select the phase-71 corpus from one shared function (6d1ce13, W24 resolved);
- the render probe judges with the Go judge the product uses (ee30f2e, the advisory resolved).

Both gates now exit 0 as written, and every non-docker check of this report passes. The docker-only gates stay NOT RUN and are not counted: HASHES-OK, TRACER-OK, GEN-DETERMINISTIC, INVENTORY-OK, PRIVACY-OK, HELPERS-OK, RENDER-OK, F3D-OK, STALE-REFUSED, RECHECK-OK, SIZE-OK, NO-WIRING. The open warnings are W25 (local scaled cache) and W26 (the superseded 74-06 grep), neither of which touches a phase-71 claim.

---

_Verified: 2026-10-18T01:05:40Z (re-verification after quick 261018-44q, branch head ee30f2e; previous pass 2026-10-17T20:22:23Z on c38cd9c, gaps_found 7/8)_
_Verifier: Claude (gsd-verifier)_
