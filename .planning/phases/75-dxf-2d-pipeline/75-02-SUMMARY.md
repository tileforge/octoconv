---
phase: 75-dxf-2d-pipeline
plan: 02
subsystem: cad-worker engine (DXF)
tags: [ezdxf, cad-dxf, texts-json, render-svg, landlock, fidelity, fixtures, measurements]

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-01 cad-dxf core, engine image, probe tracer/fixtures modes, 41 DXF fixtures
provides:
  - cmd/cad-worker/helper/cad-dxf final CLI (--max-drawn-entities N --texts PATH --render-svg PATH [--measure PATH] SRC SVG)
  - two SVGs per drawing (R-9): exact client SVG and render-only SVG with U+FFFD for characters outside the resolved font cmap; verdict key render_substituted
  - texts.json contract {"texts":[{"text","covered"}]} in SVG document order
  - dxf measurements draft (eleven keys, format dxf, $INSUNITS vocabulary)
  - scripts/cad-dxf-probe.sh helper mode (semantics, safety, corpus with the R-9 per-line gate, --write-out); tracer rasterizes the render-only SVG
  - internal/convert/testdata/cad/dxf-out (31 real outputs) and frames.tsv
affects: [75-03, 75-04, 75-07, 76]

actuals:
  tokens: 36100
  tasks: 3
  commits: 8
plan_head_before: 31150fab68d701c947b289866853325d2d16500e
plan_head_after: 65924acb14eb5a7d63496d2b18b500deaa1936e0

tech-stack:
  added: []
  patterns:
    - "Frontend.draw_entities override applies the keep filter to top-level and block (virtual) entities alike; skips counted in the verdict"
    - "texts.json, the client SVG and the render-only SVG are written from one emitted list; covered and the U+FFFD substitution use the same resolved-font cmap"
    - "Module-level constants HATCH_TIMEOUT_S and TEXT_AS_OUTLINE as test seams, set only by the probe harness that loads the helper as a module"
    - "Per-line fidelity: the harness draws ezdxf's own outline for one draw_text call at a time (same frame and page); the line is measured along its own direction from the text matrix"
    - "Probe judge: one host-side judge.py with pre-registered per-case expectations; every row carries input sha256, verdict, facts and result"

key-files:
  created:
    - internal/convert/testdata/cad/dxf-out/ (31 files)
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt
  modified:
    - cmd/cad-worker/helper/cad-dxf
    - scripts/cad-dxf-probe.sh
    - internal/convert/testdata/cad/PROVENANCE.md
    - .planning/phases/75-dxf-2d-pipeline/75-CONTEXT.md (R-9 addendum)

key-decisions:
  - "75-02: the drawn-entity backstop counts every Frontend.draw_entity call (virtual entities included); --max-drawn-entities is required and at least 1"
  - "75-02: hatch_truncated counts ezdxf's own 'hatching timeout of' print per draw_hatch_pattern call, the only signal ezdxf 1.4.4 gives (GAP line in the evidence)"
  - "75-02: uncovered characters stay exact in the client SVG and texts.json; the render-only SVG, the only rsvg input, carries U+FFFD in their place (R-9)"
  - "75-02: an empty frame is empty/nothing_drawable, a frame with zero extent on both axes is empty/zero_extent; a zero extent on one axis is still drawn"
  - "75-02 (R-9 addendum): line width bound max(3.5% of outline ink width, 1.0 px); both-empty lines agree with a both_empty fact; substituted lines gate start, covered-prefix baseline and inside-page"

requirements-completed: [CCONV-02, CCONV-03, RNDR-06]

coverage:
  - id: D1
    description: "Model space and visible layers only; IMAGE/UNDERLAY/OLE/XREF never resolved and counted; 3DSOLID and proxies skipped and counted; nothing drawable is empty"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-probe.sh helper --group semantics (layers_off_frozen, all_hidden, never_resolved, 3dsolids, uncommon)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Frame and bbox from drawn primitives plus glyph boxes, never header extents or defpoints; XLINE/RAY clipped; far POINT kept; zero extent empty"
    requirement: RNDR-06
    verification:
      - kind: integration
        ref: "helper --group semantics (wrong_extents, header_far_extents, xline_ray, far_point, single_point, wide, tall)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Draft: format dxf, 2D bbox, null volume/area with not_applicable_2d, $INSUNITS 1/2/4/5/6/13 mapped, 0/missing empty, other codes raw"
    requirement: RNDR-06
    verification:
      - kind: integration
        ref: "helper --group semantics (units_0..units_21, units_missing; draft_contract on every ok run)"
        status: pass
    human_judgment: false
  - id: D4
    description: "texts.json equals the client SVG text in order; the render-only SVG equals it with U+FFFD at uncovered positions; XML-illegal characters become U+FFFD; special characters round-trip; font failure is loud on fresh and warm caches; drawn-entity cap, block cycle and recursion end too_complex"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "helper --group safety (13 rows, nonnumeric_inf_unguarded as a fact row); render_svg_texts_ok and render_svg_structure_equal on every ok run"
        status: pass
    human_judgment: false
  - id: D5
    description: "17 corpus DXF read strict with zero fixes; 3dsolids empty, all others ok; cp1251 and latin1 texts exact"
    requirement: CCONV-03
    verification:
      - kind: integration
        ref: "helper --group corpus (17 CORPUS rows)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Text fidelity: seven corpus files at least 0.99 ink coverage both ways at 1280 px; text_cases passes the R-9 per-line geometric gate on the render-only SVG, and the client SVG fails it"
    requirement: CCONV-03
    verification:
      - kind: integration
        ref: "helper --group corpus (7 FIDELITY coverage rows, FIDELITY text_cases mode=per_line 18/18, LINE client_before check=fail)"
        status: pass
    human_judgment: false
  - id: D7
    description: "Real outputs frozen as dxf-out fixtures (client and render-only SVGs) with frames.tsv and a PROVENANCE section"
    verification:
      - kind: integration
        ref: "helper --write-out (31 OUT rows, 0 fail)"
        status: pass
    human_judgment: false

duration: 63min
completed: 2026-10-07
status: complete
---

# Phase 75 Plan 02: cad-dxf helper semantics, safety and corpus evidence Summary

**cad-dxf now writes six outputs for each drawing:**
- the exact client SVG
- a render-only SVG, in which characters outside DejaVu become U+FFFD; it is the only input rsvg gets
- texts.json with font coverage
- the dxf measurements draft
- a verdict listing every skip
- a loud failure on missing fonts and on bombs

**Every gate passes at a1cb958:** 110 rows, 0 failures. The R-9 per-line gate passes all 18 text_cases lines on the render-only SVG. The same check fails on the client SVG, where the line is 60 px off.

## Halt resolution (R-9)

The plan halted at d0cd6dd on `FIDELITY text_cases`, which scored 0.9606 / 0.9490 against the 0.99 whole-image coverage gate. There were two causes:
- librsvg 2.54.7 moves a whole line off its baseline when it holds characters that DejaVu does not cover. The `FINDING,key=rsvg_hexbox_baseline` line reproduces this.
- R-1's FIT-text kerning drift.

The orchestrator decided R-9 (6124d7e): option B plus a per-line text gate. Three questions about how to measure per line came back after my first prototype numbers; the answers are recorded in the R-9 addendum of 75-CONTEXT.md:
- the width bound is max(3.5%, 1.0 px);
- lines with no ink on either side agree, recorded with a `both_empty` fact;
- substituted lines are judged by start point, covered-prefix baseline and inside-page.

Before and after for text_cases at 1280 px:

| Measure | Before (client SVG, d0cd6dd) | After (render-only SVG, a1cb958) |
|---|---|---|
| Whole-image coverage, text_in_outline / outline_in_text | 0.9606 / 0.9490 (gate, **fail**) | 0.9873 / 0.9789 (info only; the gate is now per line) |
| Line 17 `Lat中文Ω😀end`, covered-prefix baseline | **−60.00 px** (the `client_before` row, check=fail, expected=fail) | **0.00 px**, start 0.00 px, inside page |
| Per-line gate | — | **18/18 lines pass**, outline lines 18 = SVG text lines 18 |
| Worst non-substituted line | — | start ±1.00 px, baseline −1.00 px, height −1.00 px, width −7 px on 270 px (`Fit text`, −2.59%); `red` −1 px on 23 px (bound 1.00 px) |
| `6000` (dimension text at about 1.3 px) | — | both_empty=1, in client SVG and texts.json, in_pdf_text=1 (R-3 requires it as a covered string) |

The other seven corpus files kept the 0.99 coverage gate, rasterized now from the render-only SVG. Their numbers did not change: 0.9981 to 1.0000.

## Gate result

Final run: `scripts/cad-dxf-probe.sh helper --group all --write-out` at a1cb958, with tracked dirt 1 (the orchestrator's `config.json`). Image id `sha256:f6bb26df…3b432f`.

| Group | Rows | Fail |
|---|---|---|
| semantics | 22 | 0 |
| safety | 13 | 0 (plus 1 fact row) |
| corpus | 44 | 0 |
| writeout | 31 | 0 |
| all | 110 | **0** |

Plan gates, evaluated on the committed evidence:
- **Task 1:** 22 semantics rows are ok (the gate needs at least 20), and the semantics SUMMARY has fail=0.
- **Task 2:** the font_warm_cache row is `failed/font_unavailable … result=ok`, and text_round_trip is ok.
- **Task 3:** the Task 3 awk check passes.
  - There are 8 FIDELITY rows and 17 CORPUS rows.
  - No `text_in_outline` or `outline_in_text` value is below 0.99. The text_cases row holds its coverage numbers under `info_coverage_*` names.
  - The kerning KNOWN_LIMIT line is present.
  - frames.tsv is not empty, and dxf-out has 31 files.
  - `grep -c '<text' legacy_cp1251.svg` prints 1.

The tracer was re-run at a1cb958 into this evidence file. It passed 6 runs and denied 2, with rsvg reading the render-only SVG. Its HDR shows tracked dirt 8, because it ran after the write-out had already regenerated the six PDFs.

## Accomplishments

### cad-dxf

**Drawing semantics (9c2a939):**
- The `keep` filter is applied to top-level and block entities through a `draw_entities` override. It counts and drops:
  - IMAGE and IMAGEDEF
  - UNDERLAY and OLE
  - 3DSOLID, BODY, REGION, `*SURFACE` and ACAD_PROXY_ENTITY
  - INSERTs of XREF, overlay or missing blocks, counted as `XREF`
  - non-table DXFTagStorage, counted as `PROXY:<type>`
- Reading is strict; the helper never uses ezdxf's recover reader. It never reads `$EXTMIN` or `$EXTMAX`.
- The frame is the box of the drawn primitives plus the text glyph boxes. XLINE and RAY are clipped to the frame.
- Empty results are `empty/nothing_drawable` or `empty/zero_extent`.
- The eleven-key draft is written by `--measure`.

**Text and backstops (296e0a1):**
- `--texts` and `--max-drawn-entities` are required.
- XML-illegal characters become U+FFFD.
- `covered` and `uncovered` are reported.
- Font failure is `failed/font_unavailable` on a fresh and on a warm font cache.
- Exceeding the drawn-entity cap is `too_complex/drawn_entities_over_cap`.
- `hatch_truncated` counts truncated hatch patterns.
- MemoryError is `out_of_memory`.

**Render-only SVG (a1cb958):**
- `--render-svg` is required, and the file is written on every ok run.
- Its texts equal the client texts, with U+FFFD at every position outside the resolved face's cmap.
- `render_substituted` equals `uncovered` by construction.

### Probe

- The judge checks every ok run:
  - the render SVG's texts against what texts.json implies;
  - the render SVG's element and attribute structure against the client SVG;
  - that `render_substituted` equals the substitution count it derives itself.
- Per-line gate:
  - The harness draws ezdxf's outline one line at a time, with the same frame and page. It reports 18 draw_text calls for 18 SVG texts.
  - The client and render SVGs are split into one SVG per line.
  - rsvg rasterizes each line under the confined ruleset.
  - Ink is measured along each line's own direction.
  - The extra evidence lines are `RULE,key=line_width_bound`, `RULE,key=substituted_line_gate` and `KNOWN_LIMIT,key=uncovered_as_fffd`.

### Fixtures (65924ac)

- 31 files in dxf-out, including the six new `<name>.render.svg` files.
- text_cases.render.svg has `substituted=2`.
- All PDFs are now drawn from the render-only SVGs:
  - text_cases.pdf extracts 147 characters, which are the 145 covered characters plus 2 U+FFFD.
  - nofont.pdf and outline.pdf extract 0 characters.
- The PNGs and client SVGs are byte-identical to the previous fixtures.

## Deviations from Plan

### Plan premise corrected

**1. [Rule 1 - plan expectation] 😀 is covered by DejaVu Sans.** The plan expected 😀 to be uncovered. DejaVuSans.ttf maps U+1F600, so the `text_coverage` row now compares against the font's cmap, read inside the image. The `DEVIATION,key=text_coverage_emoji` line records the plan's original expectation next to the observed mapping.

### Plan changed by decision

**2. [Rule 4, resolved] Text fidelity gate replaced (R-9 and addendum).**
- The plan required 0.99 whole-image coverage on text_cases. That was replaced by:
  - a render-only SVG;
  - the per-line geometric gate;
  - the client-SVG check, which must fail.
- See "Halt resolution" above.
- Exposing the render-only SVG changes the contracts of 75-04 and 75-07: Go must pass `out.render.svg` to rsvg and validate both SVGs. Those plans need to be updated to match.

### Auto-fixed Issues

**3. [Rule 1 - Bug] The 75-01 default-font check never fired.** The attribute also exists on the class, so the old check was always satisfied. It now looks only at the instance (296e0a1).

**4. [Rule 2] Hatch timeout detection.** The helper captures stdout and counts ezdxf's `hatching timeout of` message. The evidence has a GAP line for this. A generated ANSI31 pattern hatch shows 0 at the default timeout and 1 with `HATCH_TIMEOUT_S=0` (296e0a1).

**5. [Design kept from 75-01] XLINE/RAY interception.** XLINE and RAY are intercepted in `draw_line_entity`, not tracked through `enter_entity`. This also covers XLINE and RAY inside blocks.

**6. [Probe, own code] Fixes made during development:**
- usage rows are exempt from the verdict-line check;
- the per-line summary counters needed a `line=` field in their match pattern.

Both were fixed before any committed evidence.

**7. [Process] Accidental background run (before the halt).** A mistyped command started a background run, which I killed. It had rewritten only the LOCK line. Its image tag was removed. The final evidence comes from the later complete runs.

### Test seams in the production helper

`HATCH_TIMEOUT_S` and `TEXT_AS_OUTLINE` are module constants. Production never changes them. They are set only by the probe's `harness.py`, which loads `/usr/local/bin/cad-dxf` as a module. Its per-line outline mode wraps ezdxf's `RenderPipeline2d.draw_text` and `Recorder.store` inside the harness process; the helper itself is unchanged by it.

## Known Stubs

None.

## Threat Flags

None. No new endpoint or trust boundary. never_resolved gives the same SVG confined and unconfined, and every skipped reference is counted. These paths have no diff against main:
- `internal/convert/exec.go`
- `internal/api`
- `internal/worker`
- `internal/queue`
- `docker-compose.yml`
- `deploy/`

No dxf pair is registered.

## Verification

- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` is green.
- `go test ./... -count=1` is green, including `TestDefaultRegistersOnlyAllowlistedCADPairs` and `TestDXFFixturesSniffAsDXF`.
- cad-dxf and the probe have no comment lines besides the shebang.
- `grep -cE 'recover\.readfile|EXTMIN|EXTMAX'` on the helper prints 0.
- Every dxf-out sha256 matches its OUT line.
- No `engine75` tag remains.
- The VM has 8.1 GiB free.

The broken-windows ledger was not appended: no `gsd_run` resolver was in this shell, and the ledger is optional.

## Self-Check: PASSED

- All key files exist.
- Commits 9c2a939, 296e0a1, a695dd0, c4b46f8, d0cd6dd, a1cb958 and 65924ac are ancestors of HEAD and SSH-signed (G). The measured count of 8 since 31150fa includes the orchestrator's R-9 commit 6124d7e.
- STATE.md, ROADMAP.md and config.json are not staged.
