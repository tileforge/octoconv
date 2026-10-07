---
phase: 75-dxf-2d-pipeline
plan: 02
subsystem: cad-worker engine (DXF)
tags: [ezdxf, cad-dxf, texts-json, landlock, fidelity, fixtures, measurements]

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-01 cad-dxf core, engine image, probe tracer/fixtures modes, 41 DXF fixtures
provides:
  - cmd/cad-worker/helper/cad-dxf final CLI (--max-drawn-entities N --texts PATH [--measure PATH] SRC SVG)
  - texts.json contract {"texts":[{"text","covered"}]} in SVG document order
  - dxf measurements draft (eleven keys, format dxf, $INSUNITS vocabulary)
  - scripts/cad-dxf-probe.sh helper mode (semantics, safety, corpus, --write-out)
  - internal/convert/testdata/cad/dxf-out (25 real outputs) and frames.tsv
affects: [75-03, 75-04, 75-07, 76]

actuals:
  tokens: 27200
  tasks: 3
  commits: 4
plan_head_before: 31150fab68d701c947b289866853325d2d16500e
plan_head_after: c4b46f8080a6d881913834fe911d3ad82c09647a

tech-stack:
  added: []
  patterns:
    - "Frontend.draw_entities override applies the keep filter to top-level and block (virtual) entities alike; skips counted in the verdict"
    - "texts.json written from the same emitted list as the SVG text elements; covered = characters in the resolved font's best cmap"
    - "Module-level constants HATCH_TIMEOUT_S and TEXT_AS_OUTLINE as test seams, set only by the probe harness that loads the helper as a module"
    - "Probe judge: one host-side judge.py with pre-registered per-case expectations; every row carries input sha256, verdict, facts and result"

key-files:
  created:
    - internal/convert/testdata/cad/dxf-out/ (25 files)
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt
  modified:
    - cmd/cad-worker/helper/cad-dxf
    - scripts/cad-dxf-probe.sh
    - internal/convert/testdata/cad/PROVENANCE.md

key-decisions:
  - "75-02: the drawn-entity backstop counts every Frontend.draw_entity call (virtual entities included, MINSERT cells drawn through draw_insert count their contents, not the cell); --max-drawn-entities is required and at least 1"
  - "75-02: hatch_truncated counts ezdxf's own 'hatching timeout of' print per draw_hatch_pattern call, the only signal ezdxf 1.4.4 gives (GAP line in the evidence)"
  - "75-02: uncovered characters stay in the SVG text and in texts.json; covered lists the subsequence the resolved DejaVu face maps"
  - "75-02: an empty frame is empty/nothing_drawable, a frame with zero extent on both axes is empty/zero_extent; a zero extent on one axis is still drawn (aspect clamp 1/1000)"

requirements-completed: []

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
    description: "texts.json equals the SVG text nodes in order; XML-illegal characters become U+FFFD; special characters round-trip; font failure is loud on fresh and warm caches; drawn-entity cap, block cycle and recursion end too_complex"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "helper --group safety (13 rows, nonnumeric_inf_unguarded as a fact row)"
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
    description: "Text rendering agrees with ezdxf's outline rendering at 1280 px to at least 0.99 in both directions on every text-bearing corpus file and on text_cases"
    requirement: CCONV-03
    verification:
      - kind: integration
        ref: "helper --group corpus (FIDELITY rows): 7 corpus files pass, text_cases 0.9606/0.9490"
        status: fail
    human_judgment: true
  - id: D7
    description: "Real outputs frozen as dxf-out fixtures with frames.tsv and a PROVENANCE section"
    verification:
      - kind: integration
        ref: "helper --write-out (25 OUT rows, 0 fail)"
        status: pass
    human_judgment: false

duration: 37min
completed: 2026-10-07
status: halted
---

# Phase 75 Plan 02: cad-dxf helper semantics, safety and corpus evidence Summary

**cad-dxf draws model space with counted skips and a drawn-primitive frame, writes texts.json and the dxf draft, and fails loudly on missing fonts and bombs. The corpus passes, and so does text fidelity on all 7 corpus files. One pre-registered gate fails: FIDELITY on text_cases is 0.9606/0.9490 against 0.99. The cause is a librsvg 2.54.7 defect: it moves any text line that holds characters outside DejaVu. The plan is halted on that gate, and a decision is needed.**

## Gate result

Final run: `scripts/cad-dxf-probe.sh helper --group all --write-out` at a695dd0, with tracked dirt 1 (the orchestrator's `config.json`). Image id `sha256:d22ff36d…0eef06`.

| Group | Rows | Fail | Note |
|---|---|---|---|
| semantics | 22 | 0 | Task 1 gate: 22 ok rows (it needs at least 20) and fail=0. **PASS** |
| safety | 13 | 0 | 1 fact row. Task 2 gate: font_warm_cache and text_round_trip ok. **PASS** |
| corpus | 25 | **1** | 17 CORPUS rows and 8 FIDELITY rows. `FIDELITY text_cases` failed. **Task 3 gate FAIL** |
| writeout | 25 | 0 | 25 fixtures |
| all | 85 | **1** | `SUMMARY,mode=helper,group=all,…,fail=1` |

The Task 3 automated gate fails on two of its parts: the all-groups SUMMARY has `fail=1`, and one agreement value is below 0.99. Its other parts pass: 8 FIDELITY rows, 17 CORPUS rows, the KNOWN_LIMIT line, a non-empty frames.tsv and 25 fixtures.

## The failing row and why

`FIDELITY,group=corpus,file=text_cases,…,text_in_outline=0.9606,outline_in_text=0.9490,threshold=0.99,result=fail`. Two causes were separated by masking rows of the same two 1280 px renders (an exploratory dev-image measurement, not in the evidence):

| Masked | text_in_outline / outline_in_text |
|---|---|
| nothing | 0.9606 / 0.9491 |
| the `Lat中文Ω😀end` line | 0.9898 / 0.9864 |
| that line and the `Fit text` line | 1.0000 / 1.0000 |

1. **The line holding characters outside DejaVu is drawn in the wrong place. This finding is new.** DejaVu has no glyph for 中 or 文, so pango draws them as hex boxes. With hex boxes in a line, librsvg 2.54.7 moves the whole line off its baseline, and the size of the shift depends on the device scale. The evidence line `FINDING,key=rsvg_hexbox_baseline` reproduces this with a stand-alone SVG whose baseline sits at y = 500 px:
   - `Latend` at font-size 600 has its ink box at y 454 to 500, which is correct.
   - `Lat中文end` at font-size 600 has its ink box at y 172 to 221, which is wrong.
   - The same text at font-size 60 with a ×10 matrix is correct, because the device scale is 1 there.

   At other scales the line leaves the canvas entirely. In a dev probe, a PNG at a 0.025 scale had no ink at all, and the PDF at that scale lost the end of the line. No SVG attribute changed this: text-rendering, dominant-baseline, a font-family list and tspans were all tried. The SVG text and texts.json stay exact. Only the rasterized PNG and PDF are affected.
2. **FIT-text kerning drift.** This is R-1's accepted pango kerning limit. ezdxf stretches the unkerned width, so the drift is magnified on FIT text. It alone holds text_cases at 0.9898/0.9864, under 0.99 at 1280 px. So the 0.99 threshold on text_cases conflicts with R-1 even after cause 1 is fixed.

Exploratory check: if the uncovered characters are replaced with U+FFFD in the SVG that rsvg renders, the line returns to its baseline and text_cases measures 0.9873/0.9789. It is still below 0.99, because of cause 2.

## Decision needed (Rule 4)

- **A. Accept both as known limits.**
  - Treat FIT and uncovered-character lines as fidelity limits, and re-state the gate for text_cases as either excluding those lines or using a lower threshold.
  - 75-04's PDF judge keeps R-3. A line that is moved off the page loses covered characters, so that job fails terminally rather than passing silently.
  - Phase 76 documents that CJK and other non-DejaVu text is misplaced in PNG and PDF.
- **B. Use a render-only SVG.** cad-dxf also writes an SVG for rsvg in which uncovered characters are U+FFFD (they are already counted in `uncovered`). The client SVG stays exact.
  - This changes the 75-04 and 75-07 contracts, because Go must pass the render SVG to rsvg.
  - It fixes the misplacement, but the gate still needs restating for the FIT kerning.
- **C. Add a font that covers the missing scripts.** This contradicts the locked apt decision (librsvg2-bin and fonts-dejavu-core only).

Until this is answered, the plan is `status: halted`. The fixtures are usable as they are. Option B would change only the render inputs, not dxf-out's SVG or texts.json.

## Accomplishments

- **cad-dxf (Task 1, 9c2a939):**
  - A `keep` filter is applied to top-level and block entities through a `draw_entities` override. It counts and drops these types:
    - IMAGE, IMAGEDEF, the three UNDERLAYs, OLE2FRAME and OLEFRAME
    - 3DSOLID, BODY, REGION, `*SURFACE` and ACAD_PROXY_ENTITY
    - INSERTs of XREF, overlay or missing blocks, counted as `XREF`
    - non-table DXFTagStorage, counted as `PROXY:<type>`
  - Image policy stays IGNORE and proxy graphics stay SHOW. Reading stays strict, and the code contains no `recover`, `EXTMIN` or `EXTMAX`.
  - The frame is the drawn-primitive boxes plus the glyph boxes. XLINE and RAY are stashed and then drawn clipped to the frame.
  - Outcomes:
    - nothing drawn → `empty/nothing_drawable`
    - zero extent on both axes → `empty/zero_extent`
    - an audit exception → `unreadable/audit_error`
    - MemoryError → `too_complex/out_of_memory`
  - `--measure` writes the eleven-key draft with `allow_nan=False`.
- **cad-dxf (Task 2, 296e0a1):**
  - `--texts` and `--max-drawn-entities` are both required, and a value below 1 exits 64.
  - XML-illegal characters and lone surrogates become U+FFFD before ezdxf's own control-character replacement, and again after it. `AB\x01CD\xffEF` becomes `AB�CD�EF`.
  - texts.json is ASCII-only JSON, built from the same list as the SVG. The verdict gains `uncovered` and `hatch_truncated`.
  - `FontNotFoundError` and a missing TrueType glyph cache both give `failed/font_unavailable`.
  - The 75-01 check `getattr(pipeline, "default_font_face", None)` was always true, because the attribute also exists on the class. It is now `"default_font_face" not in vars(pipeline)`.
  - `DrawnOverCap` gives `too_complex/drawn_entities_over_cap`.
- **Probe helper mode (Tasks 1–3, a695dd0):**
  - Groups semantics, safety, corpus and writeout. Each run gets a group-tagged HDR (commit, dirt, image id), its rows and its own SUMMARY, and a group re-run replaces only its own lines.
  - Every judged run checks the following:
    - the last stdout line is the verdict
    - the exit code matches the verdict
    - the SVG passes a strict parse
    - texts.json equals the SVG text in order, and its count equals the verdict's
    - `covered` is a subsequence of the text, and `uncovered` is the sum
    - the page equals the SVG root size
    - the draft keeps its contract
    - no outputs exist on a non-ok run
- **Corpus:**
  - All 17 files have 0 auditor fixes, 3dsolids is `empty/nothing_drawable` and all the others are ok.
  - The legacy files read `Деталь` and `Bügel Ø5`.
  - The replicate sha256 values match PROVENANCE.
  - The 50 MiB replicate took 22.78 s at 423 MiB RSS and a 430 MiB cgroup peak, with no OOM.
  - Fidelity at 1280 px: legacy_cp1251 and legacy_latin1 1.0000/1.0000; columns_R2007 0.9988/0.9990; dimension_in_nested_blocks 1.0000/0.9999; hatches_2 0.9994/0.9993; uncommon 0.9986/0.9981; acad_table_with_blk_ref 1.0000/1.0000.
- **Fixtures (c4b46f8):**
  - 25 files in `dxf-out/`.
  - `nofont.pdf` and `outline.pdf` both give 0 pdftotext characters. They are negative fixtures for the 75-04 PDF judge.
  - The text PDFs contain the covered text as a subsequence: text_cases 145 characters, legacy 6, uncommon 299, hatches_2 40.
  - frames.tsv has a header row and the three PNG rows.
  - `grep -c '<text' legacy_cp1251.svg` prints 1.

## Deviations from Plan

### Plan premise corrected

**1. [Rule 1 - plan expectation] 😀 is covered by DejaVu Sans**
- **Found during:** Task 2
- **Issue:** The pre-registered check expected `covered` to lack 中, 文 and 😀, with uncovered ≥ 3. But fontTools shows that `DejaVuSans.ttf` maps U+1F600, so a correct helper reports uncovered = 2.
- **Fix:** The `text_coverage` row now reads the cmap of DejaVuSans.ttf inside the image and requires `covered` to equal the characters that font maps. The plan's expectation and the observed mapping are recorded in a `DEVIATION,key=text_coverage_emoji` line, and the row carries `plan_premise_1f600_uncovered=false`. The helper itself is unchanged.

### Auto-fixed Issues

**2. [Rule 1 - Bug] The 75-01 default-font check never fired**
- **Found during:** Task 2
- **Issue:** `default_font_face` is a class attribute of the pipeline, so `getattr(...) is None` was never true.
- **Fix:** The check is now `"default_font_face" not in vars(pipeline)`. `font_fresh_cache` proves it.
- **Commit:** 296e0a1

**3. [Rule 2 - Missing signal] Hatch timeout detection**
- **Issue:** ezdxf 1.4.4 signals a hatch timeout only by printing a message.
- **Fix:** The helper captures stdout during `draw_hatch_pattern` and counts `hatching timeout of`. The evidence carries `GAP,key=hatch_timeout_partial`. It is proven with a generated ANSI31 pattern hatch: 0 at the default timeout, 1 with `HATCH_TIMEOUT_S=0` through the harness. All corpus hatches are solid.
- **Commit:** 296e0a1

**4. [Design choice kept from 75-01] XLINE/RAY interception point**
- **Issue:** The plan says to track the top-level entity type in the backend's `enter_entity`.
- **Fix:** The helper instead intercepts XLINE/RAY in `draw_line_entity`. This also catches XLINE/RAY inside blocks, which top-level tracking would let inflate the frame. The result is the same as the plan intends: xline_ray has bbox 0,0 to 100,50 and an inner page aspect of 0.5.

**5. [Probe bug, own code] Usage rows**
- **Issue:** The judge first required a verdict line on usage errors (exit 64), but the helpers by design print none, as cad-render, cad-mesh and cad-guard do.
- **Fix:** Usage rows are exempt from that check. The fix was found and made before any commit of those rows.

**6. [Process] Accidental second probe run**
- **Issue:** After the final run, a mistyped command started a second `helper --group all --write-out` in the background. It was killed during its semantics group.
- **Effect:** It had rewritten only the `LOCK` line, with identical content, now at the end of the file. The dxf-out sha256 values all match the evidence `OUT` lines (`shasum -c` reports 0 mismatches). Its image tag `engine75-8d48826a49aa` was removed by hand.

### Test seams added to the production helper

`HATCH_TIMEOUT_S` and `TEXT_AS_OUTLINE` are module constants. Production never changes them, and they are not CLI options. Only `harness.py`, which loads `/usr/local/bin/cad-dxf` as a module, sets them. They provide the hatch signal proof and the D-04 reference rendering. When `TEXT_AS_OUTLINE` is set, the stock `SVGRenderBackend` is used and text is drawn by `RenderPipeline2d.draw_text`, with the same frame and page.

## Known Stubs

None.

## Threat Flags

None. No new endpoint or trust boundary. `never_resolved` gives the same SVG sha256 confined and unconfined (`9f977d8d…`), with skipped `IMAGE-2+OLE2FRAME-1+PDFUNDERLAY-1+XREF-1`. `internal/convert/exec.go`, `internal/api`, `internal/worker`, `internal/queue`, `docker-compose.yml` and `deploy/` have no diff against main. No dxf pair is registered.

## Verification

- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` is green. `go test ./... -count=1` is green, including `TestDefaultRegistersOnlyAllowlistedCADPairs` and `TestDXFFixturesSniffAsDXF`.
- Acceptance checks:
  - No comment lines besides the shebang in cad-dxf or the probe.
  - `grep -cE 'recover\.readfile|EXTMIN|EXTMAX'` prints 0.
  - The never_resolved row records the skipped object and the equal confined and unconfined sha256.
  - nonnumeric_inf without the guard is a `result=fact` row, with `failed/draw_exception` and matches_expected=1.
  - Every safety row carries sha256.
  - The HDR names the commit, the dirt and the image id.
  - No `engine75` tag remains, including the dev tag `engine75-dev02` used for exploration, which was removed.
- The VM has 8.1 GiB free.

The broken-windows ledger was not appended: no `gsd_run` resolver was in this shell, and the ledger is optional.

## Self-Check: PASSED

- All key files exist: cad-dxf, the probe, the evidence, PROVENANCE and the 25 dxf-out files.
- Commits 9c2a939, 296e0a1, a695dd0 and c4b46f8 are ancestors of HEAD and SSH-signed (G).
- STATE.md, ROADMAP.md and config.json were not staged.
- The plan gate itself is not met (D6 fail). That is recorded as `status: halted`, not as a pass.
