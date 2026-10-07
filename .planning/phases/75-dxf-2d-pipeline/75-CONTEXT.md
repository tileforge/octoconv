# Phase 75: DXF 2D Pipeline - Context

**Gathered:** 2026-10-07
**Status:** Ready for planning
**Mode:** `--auto`. Every choice below is the orchestrator's recommended option, per the user's v1.16 delegation (memory `v116-delegated-decisions`).

<domain>
## Phase Boundary

The `cad` class gains its DXF half through a second toolchain (ezdxf 1.4.4 → SVG → `rsvg-convert` 2.54.7), under the same confinement as phases 73 and 74:

- `convert` dxf → svg (CCONV-02)
- `convert` dxf → pdf (CCONV-03)
- `render` dxf → png, top-down only (RNDR-06), with `measurements` as the second output: 2D bbox, no volume, units from `$INSUNITS` (extends MEAS-01 and MEAS-03)
- DXF bombs end in a terminal error (extends CADIN-04)
- Each new stage gets its own measured Landlock ruleset (extends CADW-02)

Out of scope:
- measured timeout, KEDA and client docs (phase 76)
- paper-space layouts, page/layout options
- 3DSOLID/ACIS rendering

</domain>

<decisions>
## Implementation Decisions

### Carried forward (binding, not re-decided)
These come from phase 71 (`71-DECISION.md`) and phases 72–74:
- **Toolchain:** `dxf_path = ezdxf-svg+rsvg-convert`, with ezdxf 1.4.4 and rsvg-convert 2.54.7.
- **Python dependencies:** ezdxf and its deps come from the approved phase-71 lock (`scripts/cad-measure/requirements-common.txt`), hash-locked and installed with `--require-hashes --no-deps --only-binary`, the same way as the vtk wheel in 74.
- **Apt packages:** `librsvg2-bin` and `fonts-dejavu-core`, and nothing else.
- **Limits:** the DXF ceiling is 118 MiB (`ceiling_dxf_mib`) and the expanded MINSERT/INSERT cap is 170031 (`cap_dxf_expanded_inserts`).
- **Text encoding:** legacy 8-bit DXF is decoded through `$DWGCODEPAGE`. `legacy_cp1251.dxf` must read as `Деталь`.
- **Landlock sets:** the 71 candidate sets are `cad_dxf` (python/venv set plus `/usr/share/fonts`) and `rsvg-convert` (`/usr/bin/rsvg-convert` plus the multiarch lib dir). Phase 75 re-measures both on the production image. 71 is only the starting point.
- **From phase 74:** one stage per confined helper with its own ruleset, the D-03 failure-class pattern, D-07 atomic two outputs, the D-08 validator rules, and the D-09 measurement method (removal table, negative control, judgment by content). `exec.go` stays unchanged.
- **Measurements vocabulary:** `dxf` already exists, with `ReasonNotApplicable2D` and a 2D bbox only (phase 72 `internal/measurements`). It is unchanged unless a new reason is unavoidable.

### Pipeline and stages
- **D-01: Stages.** The order is guard → `cad-dxf` → `rsvg-convert`. Each stage is a separately confined helper with its own ruleset. — **Reversibility:** costly — every stage's Landlock set, conf05 block and preflight depend on it.
  - **guard:** the existing stdlib `cad-guard` gains a DXF mode. It is a pair-parser pre-pass that counts entities, counts expanded INSERT/MINSERT instances (rows × cols × nesting) against the 170031 cap, detects cyclic block references and over-deep block nesting, and finds non-numeric coordinate values. It reuses the guard's existing ruleset.
  - **`cad-dxf`:** a new ezdxf helper that draws model space to SVG and writes the measurements draft.
  - **`rsvg-convert`:** used for pdf and png only. svg output is the `cad-dxf` result after Go validation.
- **D-02: Read mode.** `cad-dxf` uses ezdxf's strict reader where possible. If the researcher shows strict reading rejects real corpus files that `recover` reads, use `recover` with an auditor gate: any error the auditor could not fix makes the file terminal (`cad_unreadable`), never a silent repair. The researcher measures this against the 17-DXF phase-71 corpus.
- **D-03: Drawn content.** Draw model space only, visible layers only (layers that are off or frozen are absent), with linetypes as ezdxf draws them.
  - **Never resolved:** XREF, IMAGE/IMAGEDEF, UNDERLAY and OLE are never resolved or loaded. The helper skips them explicitly and counts them in its verdict; Landlock is the second line.
  - **Not drawn:** 3DSOLID, ACIS and proxy entities are skipped and counted.
  - **Empty result:** a drawing with nothing drawable (71 `3dsolids.dxf`) is terminal `cad_empty_result`, not a blank page.

### Text and output safety (SC1, SC2)
- **D-04: Text is real text, not outlines.** ezdxf's SVG backend turns text into outline paths, which loses "the text is not lost" and makes PDF text extraction (SC2) impossible. So `cad-dxf` emits DXF TEXT/MTEXT/ATTRIB (and dimension text) as SVG `<text>` elements. The font family is fixed to the bundled DejaVu, and placement, height and rotation are computed from the entity. The content goes through a real XML serializer (never string formatting), so `&<>"'` round-trip exactly. The researcher decides the mechanism, for example a Frontend/Backend subclass that intercepts text drawing. If no faithful mechanism exists, stop and report; do not fall back to outlines. — **Reversibility:** costly — the SVG validator allowlist and the PDF text check depend on it.
- **D-05: Safe by construction, then validated in Go.**
  - The worker parses every produced SVG with `encoding/xml` before any upload or `rsvg-convert` call.
  - It accepts only a closed allowlist of elements and attributes: svg/g/path/polyline/polygon/line/circle/ellipse/rect/text/tspan/defs/clipPath plus geometry and presentation attributes.
  - It rejects: DOCTYPE or entity declarations, processing instructions other than the XML declaration, `script`, `foreignObject`, `style` elements and `style` attributes containing `url(`/`@import`, any `on*` attribute, and any `href`/`xlink:href` (including `use`).
  - On failure the job is terminal and nothing is uploaded.
  - The round-trip test feeds DXF text with `&<>"'` and checks that the SVG text node content equals the source exactly.
  - `rsvg-convert` runs with default libxml2 limits; no `--unlimited` flag.
- **D-06: PDF.** `rsvg-convert -f pdf` of the validated SVG, with a content-fit page (the drawing's aspect plus a margin). The worker judges the PDF before upload: it starts with `%PDF-`, has exactly one page, the page is non-empty, and text is extractable where the DXF had text. The research or the plan picks the judge tool from what the image already has, or a stdlib-level Go check; no new heavy dependency.

### Render dxf → png (RNDR-06, SC3)
- **D-07: Options.**
  - `render` on dxf accepts `size`, `background` and `assume_unit`, with the same ranges and defaults as 3D (1280, opaque).
  - `view` on dxf → 422 `opts_not_applicable`. The value itself is valid; it does not apply to DXF.
  - `quality` on dxf → 422 `opts_not_applicable`.
  - `convert` dxf → svg/pdf accepts no options: any key → `opts_not_applicable`.

  These are enforced in `ValidateCADApplicability` at create time and re-parsed in the worker. — **Reversibility:** costly — a published 422 contract.
- **D-08: Framing.** The PNG is `rsvg-convert` of the same validated SVG into a size × size frame. The drawing is scaled to fit with a margin and centred. A non-square drawing is letterboxed on the background (opaque white, or transparent), never cropped or stretched.
- **D-09: Pixel judge for drawings.** Line drawings are not 3D blobs; the 74 J1–J4 rules would misjudge thin strokes. Add a drawing mode to the Go judge, calibrated against 71's pixelcheck `drawing` mode on the 8 phase-71 drawings by the D-06/74 differential method (frames, agreements, explained divergences, zero unexplained).
  - Reject: an empty frame, a frame touching the edge, or a wrong size.
  - The bbox-span check is relative to the letterbox extent.

### Measurements (SC3, extends MEAS-01 and MEAS-03)
- **D-10: bbox.** The bbox is computed from the drawn entity set: the same visible model-space entities as the picture, via ezdxf bbox. It is never taken from `$EXTMIN`/`$EXTMAX`. Volume and area are null with `not_applicable_2d`.
- **D-11: Units.** Units come from `$INSUNITS` in the closed vocabulary with provenance. `0`, or missing, is `unspecified`; `assume_unit` fills only that case (`client_assumed`). Any `$INSUNITS` code outside the vocabulary is `unspecified` with the raw code recorded in the draft. The measurements file is the second output of every render job and is written atomically through `AddOutputs`, as in 74.

### Bombs and failure classes (SC4, extends CADIN-04)
- **D-12: Error codes.** Expanded inserts over the cap, cyclic block references and block nesting deeper than a derived bound → terminal `cad_too_complex`. Non-numeric coordinates and malformed structure → terminal `cad_unreadable`. Both are decided by the guard before ezdxf loads the file wherever the guard can see it, with ezdxf-side backstops.
  - The bound is derived from the corpus maximum with headroom, and the researcher records the derivation.
- **D-13: Failure classes mirror 74 D-03.**
  - Content failures (guard verdicts, empty result, SVG validation failure, PDF or PNG judge rejects) are terminal.
  - A stage timeout is terminal, and its message names the stage.
  - An `rsvg-convert` or `cad-dxf` crash on input that passed validation is retryable as an engine fault.
  - A sandbox fault keeps its 68-01 sentinel classification.

  Stage timeouts are derived from 71 evidence by the 73/74 method. The DXF engine total must stay under the 886 s engine budget and the 900 s reconciler window.

### Confinement (SC5, extends CADW-02)
- **D-14: Rulesets.** Measure the `cad-dxf` and `rsvg-convert` sets on the production image by the D-09/74 method, judging by content: the SVG text is present, and the PDF and PNG are non-empty with text. Fonts must be readable under the ruleset, and the negative control removes `/usr/share/fonts` and must show the text drop. HOME and the fontconfig/XDG cache live in a job-dir subpath. Record the sets in SANDBOX-PATHS `## cad`, held by the Go pinning test and `conf05-crosscheck.sh cad`.
- **D-15: Preflight.** It proves each new stage both ways at startup, as 73/74 do: success inside the job dir (judged by content), and denial of a file outside it.

### Orchestrator resolutions after research (2026-10-07, `--auto`, recommended options)
- **R-1: Kerning.** Accept the pango-vs-ezdxf kerning drift: up to 3.3% narrower, with identical start and height. Record it in the docs as a known limit. Do not patch ezdxf.
- **R-2: bbox source.** The D-10 bbox is the box of the drawn primitives (what the picture shows), not `bbox.extents`, which counts undrawn dimension defpoints.
- **R-3: PDF text rule.** The text must appear in the PDF as a subsequence of the characters DejaVu covers. Characters outside the font are excluded from the comparison and counted in the verdict.
- **R-4: Stray far POINT.** Accept it. A stray far-away POINT is drawn and is part of the frame and the bbox. The frame shows exactly what the DXF contains, and isolated points are not filtered.
- **R-5: Timeout env names.** `CAD_DXF_TIMEOUT` and `CAD_RSVG_TIMEOUT`, following `CAD_GUARD_TIMEOUT`, `CAD_MESH_TIMEOUT` and `CAD_RENDER_TIMEOUT`. The values are re-measured in Wave 0 by the 73 method. The DXF engine total must stay under 886 s.
- **R-6: Package legitimacy.** The ezdxf set is a byte-for-byte subset of the phase-71 user-approved lock (`renderer_user_approval` / `requirements-common.txt`). The planner adds a checkpoint that verifies the hashes against the lock with `cmp`. Under the delegation the orchestrator answers that checkpoint, the same way as the 74-01 R-10 vtk checkpoint.
- **R-7: Memory budget (corrected 2026-10-07).** The budget is 2842.3 MiB: 0.70 × the 4096 MiB pod, minus the Go worker. That is already the project's 70% rule, so the rule is that the worst measured stage peak (cad-dxf or rsvg, for pdf or png) must be **at most the budget itself**, as in 71 and 73. It is not 70% of the budget; the 1989.61 MiB threshold was a double-counting misreading. The research's 2510 MiB (measured at the insert cap) fits. Wave 0 still re-measures the ceiling, the caps and the stage peaks on the production helper, and derives the constants from those measurements. If any row exceeds 2842.3 MiB, the ceiling or caps are re-derived down, as for IGES in 73. Every row records `pct_of_budget`. Pod memory is not raised here; phase 76 owns resources.
- **R-8: Guard extensions found by research are binding.**
  - An expanded-entity cap (850155) in addition to the 170031 expanded-insert cap.
  - Block names compared after `name.lower()`, matching ezdxf.
  - `nan`, `inf`, out-of-range magnitudes and `1_0`-style literals rejected inside ENTITIES/BLOCKS only.
  - A nesting bound of 16.
  - Every deterministic ezdxf crash class (`RecursionError`, `ValueError` on `inf`) is pre-empted by the guard, so none of them can reach the retryable path of D-13.

- **R-9: Text outside DejaVu (decided 2026-10-07 after the 75-02 halt; option B plus a per-line gate).**
  - **The problem.** librsvg 2.54.7 shifts a whole `<text>` line off its baseline when the line contains glyphs that DejaVu does not cover; pango draws them as hex boxes. At small scales the line can leave the page, which loses text the font does cover. That breaks the goal that no text is lost.
  - **The fix.** `cad-dxf` writes two SVGs:
    - The client SVG keeps the exact text and is the only SVG returned for `dxf → svg`.
    - A render-only SVG replaces every character outside the font's actual cmap with U+FFFD. It is the only input `rsvg-convert` gets, for both pdf and png.
  - **What validates which file.** The Go allowlist validates both files. The PDF judge's R-3 subsequence rule compares only covered characters, as before.
  - **The fidelity gate.** For `text_cases`, the 0.99 whole-image coverage gate is replaced by a per-line geometric check on the render-only SVG. Each line must keep its start point and baseline within 1 px at 1280, and its height within 1 px. Its width must be within 3.5% of ezdxf's outline width; that bound is R-1's measured kerning drift (3.3%) plus margin. A line with substituted characters must stay on its baseline and inside the page. The other seven text-bearing corpus files keep the 0.99 coverage gate.
  - **Docs.** A known limit for phase 76's docs: characters outside DejaVu appear as U+FFFD in PNG and PDF, while the client SVG keeps them exact.
- **R-9 addendum: per-line gate rules (decided 2026-10-07 after the 75-02 per-line numbers; orchestrator, under the user's delegation).**
  - **Short words.** A line passes width when |text width − outline width| ≤ max(3.5% of the outline ink width, 1.0 px). Both widths are integer-pixel ink-box spans, so a bound under 1 px would test the instrument's resolution, not kerning. The measurement stays the ink box; the sub-pixel estimate is not used. Start and baseline stay within 1 px, and height within 1 px.
  - **Lines with no ink.** A line with no ink in either the render-only rsvg render or ezdxf's own outline at 1280 counts as agreeing, recorded as `both_empty=1`. Its text must still be present in the client SVG, in texts.json and in the PDF text. In 75-02 the dimension text `6000` is a covered string, so R-3 requires it in pdftotext output, and it is there (`in_pdf_text=1`).
  - **Substituted lines.** These gates apply: start point within 1 px; the baseline of the covered prefix (the text before the first U+FFFD, measured as the ink bottom within the prefix's own extent along the line) within 1 px; and the line fully inside the page. Width and height are recorded but not gated. The same check on the client SVG must fail; it does, at −60 px.

### Claude's Discretion
- How the guard's DXF pair parser is structured, and its exact verdict fields.
- The SVG allowlist's exact attribute list, provided it satisfies D-05.
- The exact page margin values.
- Whether the PDF judge is a small Go parser or a tool already in the image.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope and requirements
- `.planning/ROADMAP.md` §Phase 75: goal and SC1–SC5. SC3, SC4 and SC5 are "extends" criteria the verifier must check.
- `.planning/REQUIREMENTS.md`: RNDR-06, CCONV-02, CCONV-03, MEAS-01, MEAS-03, CADIN-04, CADW-02.

### Evaluation evidence (phase 71)
- `.planning/phases/71-evaluation-gate/71-DECISION.md`:
  - §2D-путь DXF
  - the DXF ceiling and cap rows
  - the `cad_dxf` and `rsvg-convert` Landlock rows
  - units provenance
- `scripts/cad-measure/cad_dxf.py`: the prototype helper (HOME redirect, skip recording, bbox).
- `scripts/cad-measure/requirements-common.txt`: the approved ezdxf lock.
- `.planning/phases/71-evaluation-gate/evidence/71-04-helpers.txt`, `71-04-render.txt`, `71-06-limits.txt`.

### Patterns to mirror (phases 72–74)
- `.planning/phases/74-3d-render-measurements/74-CONTEXT.md`: D-01..D-09.
- `74-VERIFICATION.md`.
- `internal/convert/cad.go`, `cadrender.go`, `cadsandbox.go`, `cadopts.go`, `cadpixel.go`, `cadmeasure.go`.
- `cmd/cad-worker/helper/cad-guard`, `cad-mesh`, `cad-render`.
- `internal/measurements/measurements.go` and `docs/MEASUREMENTS.md`: the dxf rows already exist.
- `.planning/research/SANDBOX-PATHS.md` §cad, and `scripts/conf05-crosscheck.sh`.
- `.planning/research/SUMMARY.md` and `PITFALLS.md` item 19: DXF output is untrusted.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The `CADRenderConverter` flow (stages, verdict parsing, `requireSTLMatchesVerdict`-style cross-checks, sidecar measurements, `cadSideOutputs`, `AddOutputs`) is the template for the DXF render path.
- `cad-guard` is already a stdlib pre-pass with a measured ruleset.
- The `cadsniff` DXF sniffer (phase 72) already exists, including the binary-DXF and DWG rejections.
- `classifyCADStage` / `cadFailureCode` and the sentinels in `cad.go`.
- The scripts `scripts/cad-liveproof.sh`, `scripts/sandbox-liveproof.sh cad` and `scripts/cad-stl-case.sh` follow the differential pattern.

### Established Patterns
- Exposure happens in one commit: register the pairs, update the formats/inspect pins and the allowlist tests together, and keep every earlier commit inert.
- Evidence transcripts carry their commit and tracked-dirt headers, and a FAIL or NOT RUN is never recorded as a pass.
- No code comments (user rule).

### Integration Points
- `converters.go` registration
- `cmd/cad-worker/main.go`
- `defaultMaxFormatBytes[cad]["dxf"]`
- `/v1/formats` and `/v1/inspect`
- `Dockerfile.cad-worker` (apt line, pip lock)
- SANDBOX-PATHS and conf05
- `docs/MEASUREMENTS.md` and ERROR-CODES, if a code is added

</code_context>

<specifics>
## Specific Ideas
- Corpus: the phase-71 DXF set (17 files), including `legacy_cp1251.dxf`, `legacy_latin1.dxf`, `uncommon.dxf` and `3dsolids.dxf`, plus generated `dxf-minsert` and replicate series.
- A DXF whose text contains `&<>"'`, and one with an off layer and a frozen layer, for the SC1 round trip.
- The SC3 hard cases: a non-square drawing (wide and tall), and a drawing with `$EXTMIN`/`$EXTMAX` deliberately wrong to prove the bbox comes from entities.

</specifics>

<deferred>
## Deferred Ideas
- Paper-space layout and page selection options (`layout`/`page`): v1.x.
- SHX font fidelity beyond DejaVu substitution: v1.x.
- Rendering 3DSOLID/ACIS from DXF: out of scope.
- Measured DXF timeout under load, KEDA and the preview contract: phase 76.

</deferred>

---

*Phase: 75-dxf-2d-pipeline*
