---
phase: 75-dxf-2d-pipeline
plan: 05
subsystem: cad-worker bounds (DXF)
tags: [dxf, bounds, wave-0, r-7, r-10, rsvg-convert, pdf-judge, measurements]

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-01 engine image and probe pattern; 75-02 cad-dxf with --render-svg; 75-03 cad-guard DXF mode and the stress shapes; 75-04 cad-svg-check, cad-pdf-check and cad-pixel-judge
provides:
  - "scripts/cad-dxf-bounds.sh smoke|floor|run [--out FILE]"
  - "scripts/cad-corpus-gen.py: dxf-minsert --block-entities N, dxf-combo --cells N, dxf-far-point --targets D,..."
  - "evidence/75-05-bounds.txt DERIVED keys: cad_dxf_peak_limit_mib 2842.3, cad_dxf_ceiling_bytes 123731968, cad_dxf_expanded_inserts_cap 170031, cad_dxf_expanded_entities_cap 850155, cad_dxf_timeout_s 162, cad_rsvg_timeout_s 141, cad_dxf_svg_max_bytes 135266304, cad_dxf_pdf_max_bytes 19922944, cad_dxf_pdf_text_floor 0.00469174"
  - "internal/convert: CADDXFPDFTextFloorPt (pinned by a test to the DERIVED line), JudgeCADDXFPDF, JudgeCADDXFPDFFiles, CADDXFPDFReport; CADSVGFacts gains ViewBoxW, ViewBoxH and TextSizes; CADPDFFacts gains PageW, PageH, BelowRenderFloor, BelowFloorRunes and FloorPt"
  - "cad-pdf-check --svg FILE (judges with the R-10 floor) and --report (per-text rendered size and presence)"
affects: [75-06, 75-07, 75-09, 76]

actuals:
  tokens: 61352
  tasks: 2
  commits: 8
plan_head_before: 7ae2205a5ec0daf2bf79116d6cd26f4df17333f4
plan_head_after: 5d89d17dc1952f4f3cbc283a25791af3161ac407

tech-stack:
  added: []
  patterns:
    - "Pre-registered RULE lines written before any row; rows buffered and BOUND_ROW lines emitted after the derivation, each with admitted=0|1"
    - "Rep 2 only for the rows that set a DERIVED value, looped until the setters are stable"
    - "Rendered text size on the PDF page = SVG font-size x matrix scale x min(MediaBox/viewBox); the PDF-side CTM x Tm x Tf size cross-checks present texts"
    - "Evidence file shared by modes: floor replaces only its own lines, run replaces only the others"

key-files:
  created:
    - scripts/cad-dxf-bounds.sh
    - internal/convert/caddxfpdffloor_test.go
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-05-bounds.txt
  modified:
    - scripts/cad-corpus-gen.py
    - internal/convert/caddxfpdf.go
    - internal/convert/caddxfsvg.go
    - scripts/cmd/cad-pdf-check/main.go

key-decisions:
  - "75-05: rule e subtracts the MINSERT entity itself (floor((cap_entities-1)/50) for B, floor((cap_entities-E-1)/5) for C); accepted by the orchestrator"
  - "75-05 (R-10): the rendered size of each text comes from the validated render-only SVG mapped through the PDF MediaBox, because rsvg-convert drops sub-floor texts from the content stream entirely and the PDF holds no size for them"
  - "75-05: the R-10 floor F = 2 x L = 0.00469174 pt, L = 0.00234587 pt the largest lost size over 66 sweep rows"
  - "75-05: cad_dxf_expanded_entities_cap stays 5 x cap_inserts = 850155; the 170031 rung itself counts 850156 and is one entity over it (FACT, conservative)"

requirements-completed: [CCONV-02, CCONV-03, RNDR-06]

coverage:
  - id: D1
    description: "Ceiling, both expansion caps, both stage timeouts and both byte ceilings are DERIVED by the pre-registered rule from rows on the production image; every admitted stage peak is at most 2842.3 MiB and carries pct_of_budget"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-bounds.sh run at 9babbae; Task 2 gate (no STOP, ok SUMMARY, 7 integer DERIVED keys, admitted peaks, 2 CHECK_ROW ok)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The two shapes the caps admit beyond the ladder (50-entity block at the entity cap; ceiling replicate plus the remaining expansion) pass the guard with the derived caps and stay under the limit"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "evidence/75-05-bounds.txt CHECK_ROW B (2565 MiB, 90.2%) and C (2414 MiB, 84.9%)"
        status: pass
    human_judgment: false
  - id: D3
    description: "R-10 floor measured and applied: F = 2 x L from a 66-row sweep, pinned in Go, texts below F excluded and counted, texts above F still required"
    requirement: CCONV-03
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-bounds.sh floor (DERIVED cad_dxf_pdf_text_floor); TEETH above_the_floor and below_the_floor rows of the run"
        status: pass
      - kind: unit
        ref: "internal/convert/caddxfpdffloor_test.go (floor pinned to the evidence, exclusion, teeth, mismatched inputs, sizes, report)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Engine sum 30 + 162 + 141 + 60 = 393 s at most 886 s; with the slowest Go side 416.98 s"
    requirement: RNDR-06
    verification:
      - kind: integration
        ref: "evidence/75-05-bounds.txt CHECK engine_sum and go_side"
        status: pass
    human_judgment: false

duration: about 6 h of wall time over two sessions (2026-10-11 03:50Z to 06:40Z for the floor, the judge and the run; the earlier session built Task 1 and hit the STOP)
completed: 2026-10-11
status: complete
---

# Phase 75 Plan 05: DXF bounds Summary

**Every DXF bound that 75-06 pins is now a measured, pre-registered derivation, run on the production image with the production helper, guard, Go validator and rsvg-convert. The values are a 118 MiB ceiling, 170031 expanded inserts, 850155 expanded entities, CAD_DXF_TIMEOUT 162 s, CAD_RSVG_TIMEOUT 141 s, a 129 MiB SVG ceiling and a 19 MiB PDF ceiling. The worst stage peak is 2565 MiB, 90.2% of the 2842.3 MiB budget. The first full run stopped on the PDF text judge, and the R-10 rendered-size floor (F = 0.00469174 pt) was measured and applied in the Go judge before the run was repeated.**

## Performance

- **Duration:** two sessions. Task 1 and the first run (STOP) came first. After R-10, the floor sweep, the judge change and the full run took 03:50Z to 06:40Z on 2026-10-11.
- **Tasks:** 2 of 2, plus the R-10 work the orchestrator ordered after the STOP.
- **Commits:** 8 since 7ae2205. One of them, d13d1ff (R-10 in 75-CONTEXT.md), is the orchestrator's.

## Derived values, rechecked by hand

Run at 9babbae, tracked dirt 1 (the orchestrator's config.json), image `sha256:f5de09d5…f07448`, arm64.

| Key | Value | Recomputed |
|---|---|---|
| cad_dxf_peak_limit_mib | 2842.3 | 0.70 × 4096 − 24.9 = 2867.2 − 24.9 = 2842.3 |
| cad_dxf_ceiling_bytes | 123731968 | The R118 row peak is 1098 MiB, at most 2842.3, so 118 × 1048576 = 123731968 |
| cad_dxf_expanded_inserts_cap | 170031 | Every rung is under the limit, the largest being L170031 at 2565 MiB, so the cap is 170031 (the clamp is not reached) |
| cad_dxf_expanded_entities_cap | 850155 | 5 × 170031 = 850155 |
| cad_dxf_timeout_s | 162 | The slowest cad-dxf wall over admitted rows is 107.35 s (C): 1.5 × 107.35 = 161.025, rounded up to 162 |
| cad_rsvg_timeout_s | 141 | The slowest rsvg wall is 93.46 s (L170031, PDF): 1.5 × 93.46 = 140.19, rounded up to 141 |
| cad_dxf_svg_max_bytes | 135266304 | The largest helper SVG is 90133506 bytes (L170031): 1.5 × that = 135200259 = 128.94 MiB, rounded up to 129 MiB = 135266304 |
| cad_dxf_pdf_max_bytes | 19922944 | The largest PDF is 12675864 bytes (L170031): 1.5 × that = 19013796 = 18.13 MiB, rounded up to 19 MiB = 19922944 |
| cad_dxf_pdf_text_floor | 0.00469174 pt | 2 × 0.00234587 = 0.00469174 |

The remaining rule checks:
- **Guard:** its slowest wall is 12.25 s (the 75-03 `wide` stress shape at 118 MiB), and 1.5 × 12.25 = 18.4 s ≤ 30, so CAD_GUARD_TIMEOUT stays 30 s.
- **Engine sum:** 30 + 162 + 141 + 60 = 393 s ≤ 886.
- **Go side:** the slowest Go sum is 23.98 s (L170031), and 393 + 23.98 = 416.98 s ≤ 886.
- **PNG:** the largest is 2803264 bytes (L60000 opaque), well under 96 MiB.

## Rows (maximum over repetitions; R118, L170031 and C have two repetitions)

| Row | Input | cad-dxf s / MiB | rsvg PDF s / MiB | rsvg PNG opaque / transparent s | Row peak | % of budget |
|---|---|---|---|---|---|---|
| R100 | 103540757 B replicate | 43.30 / 868 | 5.36 / 938 | 3.87 / 3.92 | 938 | 33.0 |
| R118 | 122178757 B replicate | 51.59 / 1059 | 6.32 / 1098 | 4.65 / 4.65 | 1098 | 38.6 |
| L60000 | MINSERT, 5-entity block | 29.00 / 505 | 12.82 / 945 | 10.07 / 10.45 | 945 | 33.2 |
| L100000 | | 47.93 / 833 | 33.23 / 1555 | 27.16 / 27.80 | 1555 | 54.7 |
| L120000 | | 57.82 / 980 | 48.95 / 1859 | 44.28 / 37.63 | 1859 | 65.4 |
| L135000 | | 66.14 / 1030 | 61.54 / 2089 | 50.16 / 47.88 | 2089 | 73.5 |
| L150000 | | 70.54 / 1159 | 74.49 / 2290 | 64.79 / 64.57 | 2290 | 80.6 |
| L170031 | | 81.93 / 1306 | 93.46 / 2565 | 88.58 / 88.35 | 2565 | 90.2 |
| B | 17003 cells × 50-entity block | 82.56 / 1299 | 33.65 / 2565 | 25.55 / 27.91 | 2565 | 90.2 |
| C | R118 + 89163 cells | 107.35 / 1604 | 18.42 / 2414 | 12.90 / 14.04 | 2414 | 84.9 |

The guard's walls and peaks on these rows are at most 3.17 s and 201 MiB.

**Research figures next to the measurements.** The research used a prototype helper with a 1e6 coordinate space.

| Measure | Research | Measured here |
|---|---|---|
| cad-dxf at the cap | 147.83 s / 1274 MiB | 81.93 s / 1306 MiB |
| rsvg PNG 2048 at the cap | 85.81 s / 1768 MiB | 88.58 s / 1821 MiB |
| rsvg PDF at the cap | 92.74 s / 2510 MiB | 93.46 s / 2565 MiB |
| SVG size at the cap | 104.8 MB | 90.1 MB |
| 100 MiB replicate, cad-dxf | 58.07 s / 884 MiB | 43.30 s / 868 MiB (72.04 s in the stopped first run, which shows the spread across runs) |
| 100 MiB replicate, rsvg PDF | 7.95 s / 902 MiB | 5.36 s / 938 MiB |
| Research-derived timeouts | 222 s and 140 s | 162 s and 141 s; cad-dxf is now governed by the combination row C, not by the cap rung |

**Check rows:**
- **B:** 17003 cells, 850151 expanded entities, guard ok under the derived caps. The PDF text check still covers every text: 170030 runes expected and found.
- **C:** the 118 MiB replicate (27829 inserts, 404339 entities) plus 89163 cells is exactly 850155 entities, guard ok. It is 122179816 bytes, at most the ceiling.

## R-10: the STOP and the floor

**The STOP (e3aa13e).** The first run halted on R100: `cad-pdf-check` found 223307 of 414713 expected runes. rsvg-convert exits 0 and silently drops some microscopic texts (90°, 126°, 36°, the rotated ordinate values) from the PDF content stream. The orchestrator decided R-10 (d13d1ff).

**Sweep (`floor` mode, 5c992a7, run at c00b31f):**
- **Rows:** 66 in all. 61 are uncommon.dxf with one POINT at (D, D), D = round(2000 × 1.12^k) for k = 0..60 (this is the `dxf-far-point` kind); the other 5 are the 2, 5, 10, 20 and 50 MiB replicates.
- **Alignment:** every row has residue 0.
- **Result:** the largest lost size is L = 0.00234587 pt (D367332). Every size from 0.00235626 pt upward is present in all rows, so F = 0.00469174 pt.
- **Reproducible:** two runs gave identical values.

**Where each text's size comes from (deviation from the orchestrator's mechanism, Rule 1).** The orchestrator asked for each text's page size to come from the PDF, along the text-matrix path the decoder walks. The dropped texts are absent from the content stream entirely: they have no Tj, no Tm and no Tf, so the PDF holds no size for them.
- **What the judge uses:** each text's size from the validated render-only SVG (font-size × √|det matrix|, the larger of text and tspan), mapped to points through the PDF's own MediaBox (min of MediaBox/viewBox on each axis).
- **Cross-check:** the decoder now tracks CTM × Tm × Tf for every show. The size of each present text agrees with the SVG-derived size within 0.44% at or above 0.1 pt (12.3075 vs 12.3076 pt on uncommon). Overall the gap is within 0.00051 pt; cairo writes text matrices in steps of 1/1024 pt.

**Teeth (run rows):**
- `above_the_floor`: uncommon.dxf, with text 0 (12.3075 pt) altered to ☃☃ in texts.json. The unaltered input is ok; the altered one is rejected with `missing text`.
- `below_the_floor`: the D367332 file, with a 0.00234587 pt text altered. Both the unaltered and the altered input are ok, with 49 texts counted below the floor.

The Go tests prove the same both ways, plus the mismatched-count and missing-MediaBox rejections.

**What R-10 means for these rows:** on R100, R118 and C every text is below F (67963, 80213 and 169376 texts), so their PDF text check is vacuous, and the judge says so in `below_render_floor`. The ladder rows and B still check every text: 170031 and 170030 runes.

## Deviations from Plan

### Accepted by the orchestrator

**1. [Rule 1] Rule e counts the MINSERT entity itself.** Smoke showed expanded_entities = 5n + 1 for a rung, and +1 + 5 × cells for a combo. The rule therefore uses floor((cap_entities − 1)/50) and floor((cap_entities − E − 1)/5). C lands exactly on 850155.
- FACT: rule c is kept as written. The 170031 rung itself counts 850156 expanded entities, one more than the derived entity cap, which is the conservative direction.

**2. [Rule 4, decided as R-10] PDF text floor.** Added after the STOP; see above. The run gained exactly one RULE line (`key=r10`), written before any row. No other rule changed.

### Deviation against 75-04 (recorded as the orchestrator required)

**3. The PDF judge and its CLI changed.**
- `JudgeCADPDF` keeps its signature and behaviour; its body now calls the shared `cadPDFDecode`, and all 75-04 tests pass unchanged.
- New entry points:
  - `JudgeCADDXFPDF(path, maxBytes, texts, svgFacts)`, which applies the floor;
  - `JudgeCADDXFPDFFiles`;
  - `CADDXFPDFReport`.
- New facts fields:
  - `CADSVGFacts` gains `ViewBoxW`, `ViewBoxH` and `TextSizes`;
  - `CADPDFFacts` gains `PageW`, `PageH`, `BelowRenderFloor`, `BelowFloorRunes` and `FloorPt`.
- `cad-pdf-check` changes:
  - `--svg` appends `below_render_floor`, `below_floor_runes` and `floor_pt` to the GOPDF line;
  - `--svg` without `--texts` is a usage error (64);
  - `--report` prints REPORT and GOPDFREPORT lines.
- 75-07 must call `JudgeCADDXFPDF` with the facts of the validated render-only SVG. texts.json and the SVG must hold the same number of texts.

### Plan scope

**4. Product code changed.** The plan's verification expected only scripts and evidence to change. R-10 put the floor in `internal/convert` (caddxfpdf.go, caddxfsvg.go and a new test file) and in `scripts/cmd/cad-pdf-check`. `internal/convert/exec.go` has no diff against main, and go.mod and go.sum are unchanged.

### Auto-fixed

**5. [Rule 1] The row status now names the first failing step in pipeline order (e0e0611).** The first STOP line had blamed rsvg_png_opaque, a stage that never ran. I re-ran the evidence at the fix.

**6. [Rule 3] Detached run.** A run under the harness's 2-hour background limit could have been killed. The run went through a nohup wrapper with a 60 s VM-disk watchdog at the 6 GiB floor. The lowest free space seen was 10 GiB.

### Additions within the plan's intent

- The four 75-03 stress shapes at 118 MiB (2 repetitions each) are part of rule f's guard check, as the orchestrator directed. Their walls ranged from 6.42 to 12.25 s.
- Smoke also exercises both check-shape generators (SB, SC).

## Facts and gaps for later plans

- **Ceiling coverage:** the 118 MiB ceiling is measured on a 122178757-byte replicate and on C (122179816 bytes). Admitted inputs between 122179816 and 123731968 bytes (1.26%) are not measured. Their peak at that size is 38.6% of the budget.
- **GAP go_worker_rss_dxf (51 lines, for phase 76):**
  - the Go-side RSS reaches 61 MiB in the PDF judge (C) and about 45 MiB in the pixel judge, against the 24.9 MiB Go-worker share;
  - it is recorded, not a stop.
- **Pixel judge:** cad-pixel-judge `--drawing` re-validates the SVG to get the letterbox. Its walls are therefore an upper bound of production's.
- **Byte identity:** a fresh `dxf-minsert` n=1000 with the default block size is byte-identical across the unchanged HEAD generator and the new one in the same image: sha256 `29a84ae6fecdd0cf70d68a8380b519766c162792a0110ef96fae67ada61ea6f1`.
- **For 75-06:** pin the integer DERIVED keys above. `CADDXFPDFTextFloorPt` is already pinned by `TestCADDXFPDFTextFloorComesFromTheEvidence`.
- **Phase 76 docs (R-10 known limit):** text rendered below 0.00469174 pt on the page may be missing from the PDF text layer, while the SVG keeps it.

## Known Stubs

None.

## Threat Flags

None. These are measurement tools and a stricter judge input contract. There is no endpoint, no wiring and no registration. T-75-18 and T-75-19 are mitigated by the derived caps, ceiling and timeouts. T-75-SC: no package was added.

## Verification

- **Task 1 gate:**
  - `bash -n` and `ast.parse` pass;
  - the smoke run (last at 9babbae) gives `SUMMARY,mode=smoke,…,result=ok`, 13 RULE lines, 25 BOUND_ROW lines and no DERIVED line;
  - there are no comment lines besides the shebang.
- **Task 2 gate:** the plan's automated command printed `TASK2-GATE-OK`.
  - 65 BOUND_ROW lines, all `admitted=1`, all with pct_of_budget, and every peak ≤ 2842.3;
  - 2 CHECK_ROW lines, both ok;
  - no STOP line in the evidence.
- **Acceptance:**
  - the HDR names commit 9babbae, tracked dirt 1 and the image id;
  - the engine sum, recomputed, is 393 ≤ 886;
  - `docker images | grep -c engine75-` prints 0.
- **Go:** `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` is clean, and `go test ./... -count=1` passes (31 packages ok; internal/convert took 176 s). No `//` line was added.
- **State:**
  - the octoconv compose stack is still stopped, and `log-format-postgres-1` was not touched;
  - STATE.md, ROADMAP.md and config.json are not staged;
  - the VM has 10 GiB free.
- **Ledger:** I did not append to the broken-windows ledger. No `gsd_run` resolver was in this shell, and the ledger is optional.

## Self-Check: PASSED

- All key files exist.
- Commits 62f7cc7, e0e0611, e3aa13e, c00b31f, 5c992a7, 9babbae and 5d89d17 are ancestors of HEAD and SSH-signed (G).
- The measured count since 7ae2205 is 8, including the orchestrator's d13d1ff.
