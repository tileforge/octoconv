# Phase 75: DXF 2D Pipeline - Research

**Researched:** 2026-10-07
**Domain:** DXF → SVG/PDF/PNG through ezdxf 1.4.4 + rsvg-convert 2.54.7 under Landlock; untrusted-text-safe SVG; DXF bomb guard
**Confidence:** HIGH for the text mechanism, Landlock sets, read mode, guard rules and bomb behaviour (all run against the real library and binaries in this session); MEDIUM for timeouts (measured on a prototype helper, must be re-measured with the production helper in Wave 0)

All measurements were taken in this session on a research image built `FROM octoconv-cad-worker:latest` (image id `99952ab3c0ad`, arm64, OrbStack kernel 7.0.14) plus exactly the phase-75 delta (apt `librsvg2-bin fonts-dejavu-core`, pip `ezdxf fonttools pillow pyparsing typing-extensions` from the 71 lock, `--require-hashes --no-deps --only-binary`). Landlock was applied with the production helper (`/usr/local/bin/cad-worker __sandbox-exec`). Prototype scripts lived in the session scratchpad and were not committed; the code that matters is reproduced below. The research images were removed afterwards.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

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

### Claude's Discretion
- How the guard's DXF pair parser is structured, and its exact verdict fields.
- The SVG allowlist's exact attribute list, provided it satisfies D-05.
- The exact page margin values.
- Whether the PDF judge is a small Go parser or a tool already in the image.

### Deferred Ideas (OUT OF SCOPE)
- Paper-space layout and page selection options (`layout`/`page`): v1.x.
- SHX font fidelity beyond DejaVu substitution: v1.x.
- Rendering 3DSOLID/ACIS from DXF: out of scope.
- Measured DXF timeout under load, KEDA and the preview contract: phase 76.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| RNDR-06 | `render` dxf → png draws the drawing top-down; a view option for dxf is rejected with 422 | Letterbox flags for rsvg-convert (§Pattern 5), drawing-mode pixel judge calibrated on 132 frames (§Pattern 7), `ValidateCADApplicability` changes (§Pattern 9) |
| CCONV-02 | dxf → svg: vector, model space, visible layers, text not lost; no scripts, `foreignObject` or external refs | Text-as-`<text>` pipeline proven against ezdxf's own outline rendering (coverage ≥ 0.993 at 2048 px on the corpus), Go validate-and-reserialize (§Pattern 3, §Pattern 4), off/frozen layers absent (measured), `&<>"'` round trip measured |
| CCONV-03 | dxf → pdf | rsvg-convert PDF with real text (pdftotext extracts it), stdlib Go PDF judge prototype that decodes the text exactly (§Pattern 6) |
| MEAS-01 (extends) | render gives a second `measurements` output, atomically | Same sidecar path as 74 (`CADMeasurementsPath`, `cadSideOutputs`); DXF draft shape (§Pattern 8) |
| MEAS-03 (extends) | DXF units from `$INSUNITS` or `unspecified`; 2D bbox, no volume | `$INSUNITS` code table measured from ezdxf; bbox source decision with evidence (§Pattern 8, Open Question 2) |
| CADIN-04 (extends) | DXF bombs end terminal within measured bounds, no OOM; XREF/IMAGE never resolved | Guard prototype on 47 inputs, bombs without the guard (crash/timeout table), a guard bypass found and its fix (§Pattern 2, Pitfall 3), IMAGE path-open code path in ezdxf identified and disabled |
| CADW-02 (extends) | each stage under its own measured ruleset; font available under ruleset; text not lost | Removal tables for `cad-dxf` and `rsvg-convert`, final sets, negative controls, effectiveness (§Landlock evidence) |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- Go 1.26 (`go 1.26.4`, use `GOTOOLCHAIN=go1.26.4`); chi, asynq, pgx, minio-go; no new Go module dependencies for this phase (D-06 says "no new heavy dependency").
- No code comments by default; a comment only for an external constraint the code cannot express. Rationale goes in the PR body. Python helpers follow the same rule (the existing `cad-guard`, `cad-mesh`, `cad-render` carry none).
- Library code in `internal/*` never logs; only `cmd/*/main.go` logs.
- Errors wrapped with `fmt.Errorf("...: %w", err)`; sentinels checked with `errors.Is`; HTTP handlers never echo internal error text.
- `gofmt` clean and `go vet ./...` clean is the minimum bar.
- Exposure in one commit: register pairs, update formats/inspect pins and allowlist tests together; earlier commits inert.
- Evidence transcripts carry commit and tracked-dirt headers; a FAIL or NOT RUN is never recorded as a pass.
- Local gates, not GitHub CI (Actions minutes are scarce).
- Shared OrbStack VM: keep ≥ 6 GiB free, `docker builder prune -f` is allowed, never prune volumes or other projects' images, remove images you build.
- Commits and PR bodies carry no agent attribution (user rule overrides harness defaults).

## Summary

D-04 has a faithful mechanism, and it is small. Every text path in ezdxf's drawing add-on (TEXT, ATTRIB, ATTDEF, simple MTEXT, complex MTEXT with inline codes, dimension text, table cell text) ends in a single call: `RenderPipeline2d.draw_text(text, transform, properties, cap_height, dxftype)`, where ezdxf has already done the layout (alignment, FIT/ALIGNED stretching, width factor, rotation, MTEXT line wrapping, word placement). Overriding that one method to record the string and its `Matrix44` instead of converting it to glyph outlines, then emitting `<text>` elements with `transform="matrix(...)"` after ezdxf's page transform, reproduces ezdxf's own outline rendering to a pixel-coverage agreement of 0.998–1.000 at 1280 px on every text-bearing corpus file. Two settings are load-bearing and were found by testing, not predicted: the font size must be folded out of the transform, and the SVG coordinate space must be 10 000 rather than ezdxf's default 1 000 000. With the default, rsvg-convert fails with `FreetypeError` on one file and, worse, silently drops all text (exit 0) on `legacy_cp1251.dxf` and on a large-glyph file.

The output pipeline is proven end to end: rsvg-convert draws the `<text>` with DejaVu, its cairo PDF carries a `ToUnicode` map, `pdftotext` extracts exactly the helper's text list (whitespace-insensitive) on every text file, including `Деталь` and `A&B <x> "q" 'a' & <script>…`, and a stdlib-only Go prototype (regexp, compress/zlib, hex, utf16) decodes the same text with exact equality. The negative control matters: without `/usr/share/fonts` rsvg-convert still exits 0 and writes a PDF with no text, so the PDF judge is the only thing standing between a font-less sandbox and a silently text-less output.

Read mode resolves to strict: `ezdxf.readfile` plus `doc.audit()` reads all 17 corpus files with zero audit errors and zero fixes, while `recover.readfile` additionally accepts a truncated file. The guard needs more than D-12's literal list. A cap on expanded inserts alone is not a cap on work, because the 71 cap was derived with a 5-entity block. Block-name matching must follow ezdxf (`name.lower()`), or a cycle written as `A` → `a` passes the guard; I found this bypass on the first differential probe. And ezdxf accepts `nan`, `inf`, `1e400` and `1_2345.5` as coordinates, where `inf` crashes the drawing (`ValueError`, which would be classified as retryable). Without the guard, cyclic and over-deep blocks crash ezdxf with `RecursionError` (a deterministic crash that D-13 would retry), and MINSERT and nested-grid bombs hit any timeout.

**Primary recommendation:** build `cad-dxf` as an ezdxf `Frontend` + `RenderPipeline2d.draw_text` override + `SVGRenderBackend` subclass that writes presentation attributes instead of CSS classes, with `output_coordinate_space=10000`, normalized font size, strict read + audit gate, XLINE/RAY clipped to the finite frame, and IMAGE/UNDERLAY/OLE/XREF/proxy filtered before drawing. In Go, re-serialize the SVG from a validated token stream (do not pass the helper's bytes through), judge the PDF by decoding its text, and add a drawing mode to the pixel judge that is the 71 rule minus `dark`, plus a span check against the letterbox.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| DXF option applicability (view/quality → 422) | API (create-time `ValidateCADApplicability`) | Worker (re-parse) | Published 422 contract (D-07); the worker re-validates queued opts |
| Size ceiling 118 MiB | API (`defaultMaxFormatBytes[cad]["dxf"]`) | Worker (`cadMaxBytes`) | Same two-layer shape as step/iges/stl |
| Bomb detection (expansion, cycles, depth, numerics) | Guard stage (stdlib Python, guard ruleset) | `cad-dxf` backstops (audit 104, draw counter, RecursionError) | Guard reads untrusted bytes without ezdxf on its path (D-01) |
| Drawing + text layout + measurements draft | `cad-dxf` helper (ezdxf, own ruleset) | — | Only ezdxf knows layout; fonts readable only here and in rsvg |
| SVG safety (allowlist, single root, no DTD/PI) | Go worker (validate + re-serialize) | Helper (ElementTree escaping) | Safe by construction in the helper, enforced by the trusted tier (D-05) |
| PDF/PNG rasterization | `rsvg-convert` (own ruleset) | — | D-01 |
| PDF judge (one page, non-empty, text decodes) | Go worker | — | Trusted tier judges output before upload (D-06) |
| PNG drawing judge (frame, span, margin) | Go worker | Preflight | D-09 |
| Measurements document (units, 2D bbox) | Go worker (`buildCADMeasurements` DXF branch) | Helper draft | Same split as 74 |
| Two outputs atomically | Worker (`cadSideOutputs`, `AddOutputs`) | — | Unchanged from 74 |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| ezdxf | 1.4.4 | DXF read, audit, drawing add-on (Frontend, RenderPipeline2d, SVGBackend), font metrics | Locked by 71 (`dxf_path`); hash-locked in `scripts/cad-measure/requirements-common.txt` [VERIFIED: requirements-common.txt, installed and run this session] |
| fonttools | 4.66.1 | TTF parsing for ezdxf font metrics | ezdxf dependency, 71 lock [VERIFIED: installed] |
| pillow | 12.3.0 | Imported unconditionally by `ezdxf.addons.drawing.frontend` | ezdxf dependency, 71 lock [VERIFIED: frontend.py imports PIL] |
| pyparsing | 3.3.3 | ezdxf query parser | ezdxf dependency, 71 lock |
| typing-extensions | 4.16.0 | ezdxf typing | ezdxf dependency, 71 lock |
| numpy | 2.4.6 | ezdxf numeric core | Already in `cmd/cad-worker/requirements-mesh.txt` with the same hashes as the 71 common lock; do not install twice [VERIFIED: both files] |
| librsvg2-bin (rsvg-convert) | 2.54.7+dfsg-1~deb12u1 | SVG → PNG/PDF | Locked by 71 [VERIFIED: dpkg in research image] |
| fonts-dejavu-core | 2.37-6 | DejaVu Sans/Serif/Mono (6 files) for ezdxf metrics and pango rendering | Locked by 71 [VERIFIED: dpkg, fc-list] |

Transitive apt packages pulled by `librsvg2-bin` (bookworm): libcairo2 1.16.0-7, libpango-1.0-0 1.50.12, libharfbuzz0b 6.0.0, fontconfig 2.14.1-4, libfreetype6 2.12.1, libxml2 2.9.14 [VERIFIED: dpkg -l in research image].

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Overriding `RenderPipeline2d.draw_text` | Second pass over model space collecting TEXT/MTEXT | Would re-implement ezdxf's alignment, FIT/ALIGNED, MTEXT wrapping, block transforms and dimension text — the exact layout the override gets for free. Rejected. |
| Go stdlib PDF judge | `pdftotext` (poppler-utils) in the image | A new apt package outside the 71 list (D-02/74 supply-chain rule) and a new confined stage. The stdlib prototype already decodes cairo's output exactly. |
| Per-glyph `x` lists to defeat pango kerning | — | Tested: no change in placement, and it breaks PDF text extraction (`pdftotext` returns `T`, `RO`). Rejected. |
| `crop_at_margins=True` to bound XLINE/RAY | Clip XLINE/RAY to the finite frame in the helper | Cropping drops a POINT lying exactly on the frame edge (measured on `far_point.dxf`). Rejected. |

**Installation (production Dockerfile delta):**
```dockerfile
COPY cmd/cad-worker/requirements-dxf.txt /req/requirements-dxf.txt
RUN /opt/venv/bin/pip install --no-cache-dir --only-binary=:all: --no-deps --require-hashes \
      -r /req/requirements-dxf.txt
# engine stage apt line gains: librsvg2-bin fonts-dejavu-core
```
`requirements-dxf.txt` = the five non-numpy blocks of `scripts/cad-measure/requirements-common.txt`, copied verbatim (183 hash lines). Built and imported in this session on arm64; `pip download --platform manylinux…x86_64 --require-hashes` resolves all five amd64 wheels (`ezdxf-1.4.4-cp311-cp311-manylinux_2_17_x86_64`, `fonttools-4.66.1-…manylinux2014_x86_64`, `pillow-12.3.0-…manylinux_2_28_x86_64`, two `py3-none-any`) [VERIFIED: pip download this session].

**Image size:** `octoconv-cad-worker:latest` 1 044 299 781 B → 1 173 815 290 B with the delta (+129.5 MB; apt layer 41.1 MB, pip layer 88.4 MB, of which `ezdxf/acc` compiled extensions are 20 MB) [VERIFIED: docker image inspect / history]. amd64 size not measured (only wheel resolution).

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| ezdxf 1.4.4 | PyPI | latest release 2026-05-14 | unknown (PyPI gives none) | none reported by seam (upstream github.com/mozman/ezdxf) | SUS (`unknown-downloads`, `no-repository`) | Approved in 71: lock + user approval (71-DECISION "Lock-файлы Python — допущены", A9 closed by hash cross-check) |
| fonttools 4.66.1 | PyPI | latest release 2026-09-29 | unknown | github.com/fonttools/fonttools | SUS (`too-new`, `unknown-downloads`) | Approved in 71 lock (pinned version predates the "too-new" release) |
| pillow 12.3.0 | PyPI | latest release 2026-07-01 | unknown | github.com/python-pillow/Pillow | SUS (`unknown-downloads`) | Approved in 71 lock |
| pyparsing 3.3.3 | PyPI | — | unknown | github.com/pyparsing/pyparsing | SUS (`unknown-downloads`) | Approved in 71 lock |
| typing-extensions 4.16.0 | PyPI | — | unknown | — | SUS (`unknown-downloads`) | Approved in 71 lock |

**Packages removed due to [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** all five, every flag driven by PyPI not publishing download counts. They are installed by hash from the lock the user approved in phase 71, so no new package enters the supply chain. The planner may add one `checkpoint:human-verify` that re-confirms the lock is copied byte-for-byte (diff of the five blocks), rather than one checkpoint per package.

## Architecture Patterns

### System Architecture Diagram

```
client upload (dxf) ──► API create
                         ├─ sniff (72: rejects binary/BOM/truncated DXF)
                         ├─ size ≤ 118 MiB (defaultMaxFormatBytes[cad]["dxf"])
                         └─ ValidateCADApplicability(op, "dxf", target, opts) ──422 opts_not_applicable
                               │ enqueue cad job
                               ▼
worker (cad-worker) ── job dir /job/<id>/in.dxf ──────────────────────────────────────────────
  ① guard  [ruleset: guard set]  cad-guard --format dxf …
        ├─ unreadable (bad group code, non-numeric/non-finite real, |coord|>1e12 in ENTITIES/BLOCKS) ─► cad_unreadable
        ├─ too_complex (cycle, nesting>16, expanded inserts>170031, expanded entities>850155) ─► cad_too_complex
        ▼ ok
  ② cad-dxf [ruleset: dxf set, HOME=<job>/.home]
        strict readfile + audit ─► unreadable / too_complex(audit 104)
        filter: IMAGE/UNDERLAY/OLE/XREF/proxy/3DSOLID… (counted)
        Frontend draws model space → Recorder;  draw_text override records (text, Matrix44)
        XLINE/RAY clipped to finite frame; frame = finite primitives ∪ text glyph boxes
        nothing drawn ─► cad_empty_result
        writes out.svg (ET serializer, presentation attrs, <text>), measure.json, verdict line
        ▼
  ③ Go: validate + re-serialize SVG (allowlist, single root, no DTD/PI/comments) ─fail─► terminal
        ├─ convert dxf→svg: upload re-serialized SVG ─► done
        ├─ convert dxf→pdf: ④ rsvg-convert -f pdf -b white → Go PDF judge (1 page, paint ops, text decodes = SVG text) ─► done
        └─ render dxf→png:  build measurements (dxf branch) → ④ rsvg-convert letterboxed S×S
                             → Go drawing judge → write sidecar → AddOutputs(png + measurements) ─► done
```

### Recommended Project Structure
```
cmd/cad-worker/
├── helper/cad-dxf               # new ezdxf helper (#!/opt/venv/bin/python -IB)
├── helper/cad-guard             # gains --format dxf
└── requirements-dxf.txt         # 5 blocks from the 71 common lock
internal/convert/
├── caddxf.go                    # CADDXFConverter (convert dxf→svg/pdf) + dxf render path
├── caddxfsvg.go                 # SVG validate + re-serialize
├── caddxfpdf.go                 # PDF judge
├── cadpixel.go                  # + drawing mode
├── cadsandbox.go                # + cadDXFReadOnly/cadDXFEngines, cadRsvgReadOnly/cadRsvgEngines, stage env, preflight
└── testdata/cad/synthetic/      # + text_cases.dxf, wide/tall/wrong_extents, bombs (generated by cad-corpus-gen.py)
```

### Pattern 1: Text as `<text>` via `RenderPipeline2d.draw_text` (D-04)

**What:** subclass `RenderPipeline2d`, override `draw_text` to record the string and its glyph-space→WCS matrix; build the `Frontend` on that pipeline (`UniversalFrontend.__init__(ctx, pipeline, cfg)`); after `get_xml_root_element`, append `<text>` elements using `backend.transformation_matrix`.

**Evidence that it is the single choke point** [VERIFIED: ezdxf 1.4.4 source read this session]: `frontend.py` `draw_text_entity_2d` and `draw_simple_mtext` call `self.pipeline.draw_text(line, transform, properties, cap_height, dxftype)`; `mtext_complex.py` `TextRenderer.render` calls `self.backend.draw_text(self.text, t, self.properties, self.cap_height)` with the pipeline as backend; DIMENSION and ACAD_TABLE reach these through virtual MTEXT entities. `RenderPipeline2d.draw_text` is where ezdxf turns text into `get_text_glyph_paths` outlines.

**Geometry of the transform** [VERIFIED: `fonts/ttfonts.py` `get_text_glyph_paths`]: ezdxf scales glyphs by `f = cap_height / capH_units` (`capH` = top of glyph `A` minus baseline = bottom of glyph `x`) and shifts y by `-baseline·f`. An SVG `<text>` draws font unit `(u, v)` at `(u·s, −v·s)` with `s = font-size / unitsPerEm`. So with `em = unitsPerEm·f`, local SVG → WCS is `scale(1,−1) · translate(0, −baseline·f) · transform`, then `· T` (ezdxf page placement, which includes the y flip).

**Fidelity measured** (ezdxf outline rendering vs `<text>` rendering of the same drawing, rsvg-convert, ink mask < 160, agreement within 2 px; first number = outline ink covered by text ink, second = reverse) [VERIFIED: run this session]:

| File | 1280 px (1e6 space, early prototype) | 256 px (final config) | 2048 px (final config) |
|---|---|---|---|
| legacy_cp1251 | 1.000 / 1.000 | 1.000 / 1.000 | 1.000 / 1.000 |
| legacy_latin1 | 1.000 / 1.000 | — | — |
| columns_R2007 (complex MTEXT, columns) | 0.999 / 0.999 | 0.981 / 0.994 | 0.997 / 0.997 |
| dimension_in_nested_blocks | 1.000 / 1.000 | — | — |
| hatches_2 | 1.000 / 0.999 | — | — |
| uncommon (dims, leaders, multileaders) | 0.999 / 0.998 | 0.971 / 0.997 | 0.993 / 0.993 |
| acad_table_with_blk_ref | 1.000 / 1.000 | — | — |
| text_cases (synthetic: `&<>"'`, rotated, centre/right, FIT, ALIGNED, width 0.5, `%%c %%d %%p`, MTEXT `\P \H \C \f \S`, ATTRIB, DIMENSION) | 0.999 / 1.000 | 1.000 / 1.000 | 0.997 / 0.996 |
| giant (one glyph the height of the drawing) | — | 1.000 / 1.000 | 1.000 / 1.000 |
| tiny / huge (same drawing at 1e-4 and 1e6 scale, large kerned text) | — | 0.928 / 0.969 | 0.767 / 0.769 |

The tiny/huge row is pango kerning: the word `TinyText 123` keeps its start x (62 px) and height (127 px) exactly but is 820 px wide instead of 848 (−3.3%), because ezdxf lays text out without kerning (`TTFontRenderer(font)`, `kerning=False`) and pango kerns `Te`/`Ty`. `font-kerning`, `font-feature-settings` (attribute and `style`), `text-rendering`, `textLength` and per-glyph `x` lists were all tested and none changed librsvg 2.54.7's output [VERIFIED]. Consequence: left-aligned text is exact; centre/right-aligned and FIT/ALIGNED text can be off by up to the kerning delta. This is fidelity, not text loss; record it as a known limit.

**Example:**
```python
from ezdxf.addons.drawing import Frontend, RenderContext, config, layout, svg
from ezdxf.addons.drawing.pipeline import RenderPipeline2d, prepare_string_for_rendering
from ezdxf.fonts.ttfonts import get_fontname
from ezdxf.math import BoundingBox2d, Matrix44

class TextPipeline(RenderPipeline2d):
    def __init__(self, backend, sink):
        super().__init__(backend)
        self.sink = sink

    def draw_text(self, text, transform, properties, cap_height, dxftype="TEXT"):
        if not text.strip():
            return
        text = xml_safe(prepare_string_for_rendering(text, dxftype))
        face = properties.font or self.default_font_face
        gc = getattr(self.text_engine.get_font(face), "glyph_cache", None)
        if gc is None or not hasattr(gc, "font"):
            raise FontUnavailable(dxftype)
        f = gc.get_scaling_factor(cap_height)
        box = BoundingBox2d()
        for p in self.text_engine.get_text_glyph_paths(text, face, cap_height):
            p.transform_inplace(transform)
            if len(p):
                box.extend(p.extents())
        self.sink.append(dict(
            text=text, m=Matrix44(transform), em=gc.font["head"].unitsPerEm * f,
            shift=-gc.font_measurements.baseline * f, family=get_fontname(gc.font),
            bold="Bold" in (gc.font["name"].getDebugName(2) or ""),
            color=self.get_backend_properties(properties).color, box=box))

class DXFFrontend(Frontend):
    def __init__(self, ctx, backend, cfg, sink):
        super(Frontend, self).__init__(ctx, TextPipeline(backend, sink), cfg)

def emit_text(group, t, T):
    m = Matrix44.scale(1, -1, 1) @ Matrix44.translate(0, t["shift"], 0) @ t["m"] @ T
    k = math.hypot(m[1, 0], m[1, 1]) or 1.0
    el = ET.SubElement(group, "text")
    el.set("transform", "matrix(%s)" % " ".join("%.6g" % v for v in (
        m[0, 0] / k, m[0, 1] / k, m[1, 0] / k, m[1, 1] / k, m[3, 0], m[3, 1])))
    el.set("font-family", t["family"])
    el.set("font-size", "%.6g" % (t["em"] * k))
    if t["bold"]:
        el.set("font-weight", "bold")
    el.set("fill", t["color"][:7])
    el.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    el.text = t["text"]
```
`xml_safe` must replace code points outside XML 1.0 `Char` (`\t \n \r`, U+0020–D7FF, E000–FFFD, 10000–10FFFF) with U+FFFD: `prepare_string_for_rendering` only replaces C0 controls, and ezdxf's `readfile` decodes with `errors="surrogateescape"`, so lone surrogates reach the string. ElementTree escapes `& < >` but does not reject illegal characters [VERIFIED: probe; Go `encoding/xml` rejects `U+0001` with "illegal character code"].

### Pattern 2: Guard DXF mode (D-12)

Stdlib-only pair parser, run by `/usr/bin/python3 -IB` under the existing guard ruleset (no ezdxf, no `/opt/venv`). It reads the file as latin-1 with universal newlines (never fails to decode) and processes `code`/`value` line pairs.

Rules, each tested against the real ezdxf on the inputs listed under Evidence:
1. **Group code line** must be an integer (`[+-]?\d+` after strip), else `unreadable/bad_group_code`. Catches binary DXF and BOM fixtures.
2. **Real-valued codes** (ezdxf `DOUBLE`: 10–59, 110–149, 210–239, 460–469, 1010–1059 [VERIFIED: `lldxf/types.py` read this session]) must match `[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?`, else `unreadable/non_numeric_value`; a value that matches but overflows to ±inf (`1e400`) → `unreadable/coordinate_out_of_range`. Python `float()`, and therefore ezdxf, accepts `nan`, `inf`, `1e400` (→ inf) and `1_2345.5` (underscore); the guard must reject these. Integer codes (`INT16`, `INT32`: 60–99, 160–179, 270–299, 370–389, 400–409, 420–459, 1060–1071) must be integer or real syntax (ezdxf tolerates ProE's float-ints).
3. **Coordinate magnitude** for codes 10–39 > 1e12 (= `measurements.MaxAbsCoordinate`) → `unreadable/coordinate_out_of_range`, **only inside ENTITIES and BLOCKS**. The header carries `$EXTMIN = 1e20` and objects carry `2.0E+50` (`AcDbOsnapPointRef` in `uncommon.dxf`) on real files, so a file-wide check rejects 15 of 17 corpus files [VERIFIED: first guard run rejected them; section-scoped run passes all 17].
4. **Block graph**: nodes are BLOCK names, keyed exactly as ezdxf keys them (`name.lower()` after the same decoding ezdxf uses, see Pitfall 3). Edges are code-2 references from `INSERT` (multiplicity `max(70,1)·max(71,1)`), `DIMENSION`, `ARC_DIMENSION`, `LARGE_RADIAL_DIMENSION`, `ACAD_TABLE` (multiplicity 1), whether they sit in ENTITIES (model space) or inside a BLOCK. `DIMENSION` matters: a 1000×1000 MINSERT inside a dimension's geometry block runs ezdxf past 120 s [VERIFIED].
5. **Cycle** anywhere in the block graph (referenced or not) → `too_complex/cyclic_block_reference`. Iterative DFS, not recursive.
6. **Nesting** = longest reference chain from model space; > **16** → `too_complex/nesting_too_deep`. Derivation: corpus maximum is 4 (`dimension_in_nested_blocks.dxf`, msp depth 4, block depth 3); 4× headroom = 16; ezdxf's own drawing recursion fails between 196 and 200 levels at the default recursion limit, so 16 is a 12× margin below the crash [VERIFIED: depth probe 16…195 ok, 200 RecursionError].
7. **Expanded inserts** = Σ over model-space references `mult × (1 + E(child))`, `E(B) = Σ refs mult × (1 + E(child))`, saturating at cap+1; > **170031** → `too_complex/expanded_inserts_over_cap`.
8. **Expanded entities** (new, see Pitfall 2) = model-space entity count + Σ refs `mult × N(child)`, `N(B)` = direct entities of B + Σ refs `mult × N(child)`; > **850155** → `too_complex/expanded_entities_over_cap`. Derivation from the same 71 evidence row as the 170031 cap: the `dxf-minsert` series is "one MINSERT, columns x rows cells of a 5-entity block" [VERIFIED: 71-06-limits.txt `CAP,counter=dxf_expanded_inserts … basis=dxf-minsert series (one MINSERT, columns x rows cells of a 5-entity block)`], so the measured quantity is 5 entities per instance: 170031 × 5 = 850155.

Verdict fields: `entities`, `expanded_inserts`, `expanded_entities`, `nesting`, `blocks`, plus `line`/`code` on numeric failures. Exit codes as the STEP mode (0 ok, 2 unreadable, 4 too_complex).

Guard prototype results (wall time with `/usr/bin/python3`, rss 9–15 MiB) [VERIFIED: run this session]:

| Input | Verdict | Counters / reason |
|---|---|---|
| 17 corpus files (incl. 2–50 MiB replicate) | ok | max nesting 4, max expanded inserts 11764 (50 MiB replicate), 74286 entities |
| dxf-replicate-uncommon-100MiB (103.5 MB) | ok in 4.52 s | 148651 entities, 23579 expanded inserts |
| dxf-replicate-uncommon-200MiB (207.2 MB) | ok in 7.98 s | 297488 entities |
| minsert_cap_170000 | ok | 170000 expanded inserts (under cap) |
| dxf-minsert n1000000, minsert_250k, minsert_nested_4m (20×20 of 100×100), nest_d30_f2 (2^30), dimension_minsert_1m | too_complex | expanded_inserts_over_cap |
| cyclic_self, cyclic_ab, cyclic_unreferenced | too_complex | cyclic_block_reference |
| nonnumeric abc, comma, nan, inf, underscore | unreadable | non_numeric_value |
| nonnumeric 1e300, 1e400 | unreadable | coordinate_out_of_range |
| reject/dxf_binary, reject/dxf_bom | unreadable | bad_group_code |
| reject/dxf_truncated | ok (ezdxf strict then rejects it; 72's sniffer rejects it at create) | — |
| case.dxf (block `A` containing `INSERT a`) | **ok — bypass** with case-sensitive keys; ezdxf resolves `a` to `A` and its audit reports code 104 | fixed by ezdxf-equivalent keys |

Guard time at the 118 MiB ceiling ≈ 5 s, so the existing 30 s `CAD_GUARD_TIMEOUT` holds (×1.5 of the 200 MiB time is 12 s).

### Pattern 3: `cad-dxf` helper skeleton (D-01, D-02, D-03, D-10, D-11)

```python
os.environ.update(HOME=home, XDG_CACHE_HOME=home + "/.cache", XDG_CONFIG_HOME=home + "/.config")
import ezdxf
...
doc = ezdxf.readfile(src)
auditor = doc.audit()
if any(e.code == AuditError.INVALID_BLOCK_REFERENCE_CYCLE for e in auditor.errors): verdict too_complex
elif auditor.errors: verdict unreadable
cfg = config.Configuration(
    background_policy=config.BackgroundPolicy.OFF,
    image_policy=config.ImagePolicy.IGNORE,
    proxy_graphic_policy=config.ProxyGraphicPolicy.SHOW,
    color_policy=config.ColorPolicy.COLOR)
fe.draw_layout(msp, filter_func=keep, finalize=False)
# draw stashed XLINE/RAY clipped to the finite box, then fe.pipeline.finalize()
page = layout.Page(w_mm, h_mm, layout.Units.mm, margins=layout.Margins.all(10))
settings = layout.Settings(fit_page=True, output_coordinate_space=10000)
root = backend.get_xml_root_element(page, settings=settings, render_box=frame)
```

- **Environment before `import ezdxf`**: ezdxf loads its font-manager cache from `$XDG_CACHE_HOME/ezdxf/font_manager_cache.json` at import and rebuilds it by scanning font dirs when absent [VERIFIED: `fonts/fonts.py` `load()`/`_load_font_manager`]. HOME in a job-dir subpath keeps that per job.
- **Strict read** (D-02 resolved, see Evidence): `ezdxf.readfile` + `doc.audit()`. Audit code 104 (`INVALID_BLOCK_REFERENCE_CYCLE`) → `too_complex`; any other unfixed `auditor.errors` → `unreadable`; `auditor.fixes` reported in the verdict (corpus: 0).
- **`keep` filter** counts and drops: `IMAGE`, `IMAGEDEF`, `PDFUNDERLAY`, `DWFUNDERLAY`, `DGNUNDERLAY`, `OLE2FRAME`, `OLEFRAME`; `3DSOLID`, `BODY`, `REGION`, the `*SURFACE` family, `ACAD_PROXY_ENTITY`; `INSERT` whose block is an XREF or XREF overlay (`block.block.is_xref`/`is_xref_overlay`) or missing; any `DXFTagStorage` that is not `AcadTableBlockContent` (unknown entity whose only geometry is proxy graphic) as `PROXY:<type>`.
- **`ImagePolicy.IGNORE` is mandatory, not cosmetic**: the default `DISPLAY` calls `_find_image_path(document_dir, image_def.dxf.filename)`, which tries the DXF's path as an absolute path first and then `PIL.Image.open`s it [VERIFIED: `frontend.py` `draw_image_entity`, `_find_image_path`]. A DXF naming `/etc/passwd` or `../../x.png` would make the helper open it. The filter keeps it out of the frontend entirely; Landlock is the second line.
- **`ProxyGraphicPolicy.SHOW`, not `IGNORE`**: `ACAD_TABLE` is loaded as `AcadTableBlockContent(DXFTagStorage)` and is drawn only through the `DXFGraphicProxy` wrapper, which `IGNORE` skips. With `IGNORE`, `acad_table_with_blk_ref.dxf` became `nothing_drawable` and `uncommon.dxf` lost its MULTILEADER text (49 → 47 texts) [VERIFIED]. `ACAD_TABLE` draws its anonymous block, not binary proxy data; MULTILEADER parses its proxy graphic first and falls back to its own data [VERIFIED: `entities/acad_table.py`, `entities/mleader.py`]. D-03's "proxy entities" are handled by the `keep` filter above.
- **XLINE/RAY**: ezdxf draws them `infinite_line_length × unit_vector` long, and the vector is not normalized. The 71 replicate files carry `(999, 0, 0)` and inflated the frame from 23 091 to 690 000 units [VERIFIED]. Exclude them from the frame and draw them clipped to it with `ezdxf.math.clipping.ClippingRect2d(...).clip_line(a, b)` (returns a tuple of segments).
- **Frame and measurements bbox** = ezdxf `BoundingBox2d` of every recorded primitive except XLINE/RAY, extended by the text glyph boxes. Track the current top-level entity in `Recorder.enter_entity` and union `record.bbox()` in `store`.
- **Page**: long side 297 mm, 10 mm margins, short side `277 × aspect` + margins, with aspect clamped to ≤ 1000 (a single line has zero height). rsvg-convert renders the PDF page at the SVG `width`/`height` (297 mm → 841.89 pt) [VERIFIED: pdfinfo].
- **No CSS**: `SVGRenderBackend` writes `<style>` elements in `<defs>` and `class` attributes (`Styles.get_class`). Subclass it (`make_backend` static method) so `add_strokes`/`add_filling` set `fill`, `stroke`, `stroke-width`, `stroke-opacity`, `fill-opacity` attributes directly.
- **Font failure must not be silent**: when the font manager finds no TTF, ezdxf falls back to `MonospaceFont` (no `glyph_cache.font`) or raises `FontNotFoundError`. The prototype dropped text in the first case (`TEXT_NO_TTF: 18`) and crashed in the second. The helper must emit a `failed`/`font_unavailable` verdict, which is classified as an engine fault and caught by preflight.
- **Keep stdout clean**: ezdxf `print()`s font-scan errors to stdout (`[Errno 13] Permission denied: '/usr/local/share/fonts'` under Landlock) and complex-MTEXT layout errors. Redirect `sys.stdout` to stderr for the work, and write the `CADVERDICT` line to the original stdout at the end.
- **Verdict JSON with `ensure_ascii=True`**: surrogate-escaped text cannot be UTF-8 encoded.
- **Backstops inside the helper**: count `draw_entity` calls for virtual entities and abort with `too_complex` above 850155. Catch `RecursionError` and `MemoryError` → `too_complex`; read errors (`DXFStructureError`, `UnicodeDecodeError`, `OSError`) → `unreadable`; anything else during drawing → `failed` (engine fault).
- **`$INSUNITS` → vocabulary** [VERIFIED: `ezdxf.units.decode`]: 1 `in`→`inch`, 2 `ft`→`foot`, 4 `mm`, 5 `cm`, 6 `m`, 13 `µm`→`micrometre`; 0 or missing → `unspecified`/`none`; any other code (3 mi, 7 km, 8 µin, 9 mil, 10 yd, 11 Å, 12 nm, 14 dm, …, 21–24 US survey) → `unspecified` with the raw code in `declared_unit_raw`. DXF values are not scaled (`applied_factor` 1), so `cadUnitFor` needs a DXF branch: the current STEP/IGES branch requires `applied_factor` to equal the mm-per-unit table value and would turn every declared DXF unit into `unspecified` [VERIFIED: `internal/convert/cadmeasure.go` `cadUnitFor`].

### Pattern 4: Go SVG validate-and-re-serialize (D-05)

Do not pass the helper's bytes on. Decode with `encoding/xml` (strict, no `Entity` map, nil `CharsetReader`), check every token, and write a new document with Go-controlled escaping. rsvg-convert and the client then see exactly what Go validated, which removes the Go-vs-libxml2 differential class.

What Go's decoder actually reports (probe this session) [VERIFIED]:
- XML declaration → `ProcInst{Target:"xml"}`; allow only as the first token.
- `<!DOCTYPE …>` → `Directive`; reject. An undefined entity reference is a decode error.
- `<?xml-stylesheet …?>` → `ProcInst{xml-stylesheet}`; reject. Comments → `Comment`; reject (the helper emits none).
- Root `xmlns` → `Attr{Space:"", Local:"xmlns"}`; prefixed declarations → `Attr{Space:"xmlns", Local:"h"}`; reject all prefixed declarations.
- `xml:space` → `Attr{Space:"http://www.w3.org/XML/1998/namespace", Local:"space"}`.
- Elements carry the resolved namespace (`<h:script>` → `Space:"http://www.w3.org/1999/xhtml"`); require `Space == "http://www.w3.org/2000/svg"` on every element.
- **A second root after the first closes is accepted** (`<svg>…</svg><svg/>` decodes with no error, the second in namespace ""). Enforce a single root and nothing after it.
- `encoding="ISO-8859-1"` → error with nil `CharsetReader`; invalid UTF-8 and U+0001 → errors.

Element/attribute inventory of everything the helper produced across 22 SVGs, 221 061 paths and 58 749 texts [VERIFIED]:

| Element | Attributes | Value grammar for the allowlist |
|---|---|---|
| `svg` (root, exactly one) | `xmlns`, `width`, `height`, `viewBox` | xmlns = SVG ns; `^\d+(\.\d+)?mm$`; `^0 0 \d+ \d+$` |
| `defs` | none (empty) | no children |
| `rect` | `x`, `y`, `width`, `height`, `fill`, `fill-opacity` | integers; `#[0-9a-f]{6}`; decimal |
| `g` | `stroke-linecap`, `stroke-linejoin`, `fill-rule` | `round`, `round`, `evenodd` |
| `path` | `d`, `fill`, `stroke`, `stroke-width`, `stroke-opacity`, `fill-opacity` | `d` ⊂ `[MmLlQqCcZz0-9 .\-]`; colours `#rrggbb` or `none`; numbers |
| `text` | `transform`, `font-family`, `font-size`, `font-weight`, `fill`, `xml:space` | `matrix(6 numbers)`; family ∈ {`DejaVu Sans`, `DejaVu Sans Mono`, `DejaVu Serif`}; number; `bold`; colour; `preserve`; children: character data only |

This is a subset of D-05's element list (no polyline/polygon/line/circle/ellipse/tspan/clipPath appear) and contains no `style`, `class`, `href`, `on*`, `id` or `url(`. A closed allowlist this tight makes the reject list hold by construction. Character data is allowed only inside `text` (whitespace elsewhere is dropped). Stream it: the SVG at the expansion cap is 105 MB.

### Pattern 5: Letterboxed PNG with rsvg-convert (D-08)

`-w/-h -a` alone yields a non-square image (1280 × 99 for `wide.dxf`), and `--page-width/--page-height` without offsets pins the drawing top-left [VERIFIED]. Go computes the fit from the validated root `width`/`height` (mm):
```
inner = S × (1 − 2 × 0.03);  k = inner / max(w_mm, h_mm)
sw, sh = round(w_mm·k), round(h_mm·k);  left, top = (S − sw)/2, (S − sh)/2
rsvg-convert [-b white] -w sw -h sh --page-width S --page-height S --left left --top top -o out.png in.svg
```
`-b white` → RGB PNG with no alpha (opaque). Without `-b` the SVG's background rect (`fill-opacity="0.0"`, from `BackgroundPolicy.OFF`) leaves an RGBA PNG transparent outside the ink [VERIFIED]. PDF: `rsvg-convert -f pdf -b white -o out.pdf in.svg` (page = SVG size).

### Pattern 6: PDF judge, stdlib Go (D-06)

cairo 1.16's PDF (rsvg-convert 2.54.7) is classic: plain `N 0 obj … endobj`, Flate content streams, `/Type /Pages … /Count 1`, one `/Type /Page`, fonts as TrueType/WinAnsi (literal strings with `\(`, `\\`, octal escapes) or Type0/Identity-H (hex strings), each with a `/ToUnicode` CMap using `bfchar` (and possibly `bfrange`) [VERIFIED: decoded this session]. Judge:
1. starts with `%PDF-`;
2. exactly one `/Type /Page` object and `/Count 1`;
3. page `/Contents` inflated (cap the inflated size), at least one stroke/fill/text-show beyond the `-b white` background fill;
4. text: map font resource names → ToUnicode CMaps, tokenize the content stream (literal and hex strings, `Tf`), decode, strip whitespace; compare with the concatenated `<text>` contents of the validated SVG, whitespace stripped.

Prototype result (main.go, ~250 lines, regexp/zlib/hex/utf16 only) [VERIFIED]: `text_equal=true` on text_cases (151 runes), legacy_cp1251 (6), legacy_latin1 (7), columns_R2007 (677), uncommon (299), acad_table (74), dimension_in_nested_blocks (68); the outline-mode PDF decodes 0 runes and the no-font negative control decodes 0 of 151. Independent cross-check: `pdftotext -raw` output with whitespace removed hashes identically to the helper's text list on all four files tested.

Glyphs DejaVu lacks: `中文` renders as pango hex boxes, and the PDF decodes them as hex digits (`Lat4E2D6587Ω😀end` vs `Lat中文Ω😀end`). Exact equality would fail such a drawing. Use an ordered subsequence check over the characters the resolved font covers. The helper reports per-text coverage from the TTF cmap (`gc.cmap` is fontTools' best cmap), Go cross-checks that the reported texts equal the SVG text nodes, and the uncovered count goes in the verdict (Open Question 3).

### Pattern 7: Pixel judge, drawing mode (D-09)

Calibration: 22 SVGs (corpus + synthetic) × {256, 1280, 2048} × {opaque, transparent} = 132 frames through Pattern 5, judged by the 71 `pixelcheck.py single --mode drawing` and by the proposed rule [VERIFIED]:

| Fact | Observed |
|---|---|
| span of ink-box long side / letterbox long side | 0.933–0.938 on every frame (= 277/297 page inner ratio) except `far_point` |
| margin | 15 px @256 (0.059·S), 78 @1280, 125 @2048 — ≥ `cadMarginMin` 0.03 |
| opaque frac (min) | 0.00086 (2048 replicate/tall) — ≥ 71 `DRAW_FRAC_MIN` 0.0003 |
| opaque dark | 0.0000 on 13 opaque frames (12 at 256 px: antialiased hairlines never reach < 80 in all channels; `far_point`) |
| transparent `FracTransparent` | 0.92–0.9991 — **above** the 3D `cadTransparentMax` 0.95 |
| transparent fully-opaque pixels | 0 on several frames (e.g. comments_999 @1280) — the 3D rule "no opaque pixel → reject" would reject them |
| 71 pixelcheck on transparent frames | rejects all 66 (it converts RGBA → RGB, turning transparent black into ink) |

Agreement with 71 on opaque frames: 53 of 66 accept in both. Explained divergences: 12 frames at 256 px fail only on 71 `dark` (antialiasing), plus `far_point` ×3 (see Pitfall 6). A drawing in a single non-black colour (ACI 1 red, 2 yellow) also fails 71 `dark` by definition. Zero unexplained.

Proposed Go rule (`JudgeCADDrawingPNG(r, size, background, letterbox)`):
- J0 size S×S (existing);
- opaque: no transparency (existing); transparent: alpha present and `FracTransparent ≥ 0.05`, no upper bound, no opaque-pixel requirement;
- ink = existing mask (opaque: any channel < 250; transparent: alpha > 0); J1 ink count ≥ 1 and `frac ≥ 0.0003` (71 `DRAW_FRAC_MIN`, which every real frame passes);
- J2 ink-box long side ≥ 0.5 × letterbox long side (observed 0.933);
- J3 margin ≥ 0.03·S and the ink box inside the letterbox rectangle ± 2 px;
- no `dark` criterion.

Judge transparent frames against their opaque twin in tests (same ink box: observed identical, e.g. columns @1280 `[78,299,1200,979]` both).

### Pattern 8: Measurements draft for DXF

Same draft keys as 74 so `parseCADMeasureDraft` keeps working: `format:"dxf"`, `declared_unit` (vocabulary name or ""), `declared_unit_raw` (raw `$INSUNITS` code as a string or ""), `applied_factor: 1`, `bbox` 2 coordinates per corner, `solids/closed_solids: null`, `volume/area: null`, `null_reason:"not_applicable_2d"`, `triangles: 0`. Validation: `measurements.Validate` already enforces 2D bbox, no `solids`, volume/area null with `not_applicable_2d` for `dxf` [VERIFIED: measurements.go:160-163, :275-276]. `bbox` must be ≤ 1e12 in absolute value; the guard's coordinate rule keeps drawn geometry inside that.

### Pattern 9: Option applicability (D-07)

`ValidateCADApplicability(engine, operation, source, target, o)` currently returns nil for zero opts, allows only `quality` for convert, and for render rejects only `quality` on stl [VERIFIED: cadopts.go:144-165]. Add: source `dxf` + convert → any non-zero opts → error; source `dxf` + render → `view != ""` or `quality != ""` → error. The handler already maps the error to 422 `opts_not_applicable` (handlers.go:1002-1006). Note `Resolved()` defaults `View` to `iso`; the DXF render path must not send it anywhere.

### Anti-Patterns to Avoid
- **ezdxf's default `output_coordinate_space` (1 000 000) with `<text>`**: silently drops all text (rc 0) on `legacy_cp1251.dxf` and `giant.dxf`, and fails `FreetypeError` on `text_cases.dxf`; 100 000 still drops `giant.dxf` silently [VERIFIED].
- **Un-normalized `font-size`** (tiny font, huge matrix): `FreetypeError`, rc 1, no output [VERIFIED].
- **`crop_at_margins=True`**: drops a point on the frame edge.
- **Passing helper SVG bytes through after validation**: leaves a parser differential; re-serialize.
- **Using ezdxf `bbox.extents()` of drawn entities as the measurement**: includes DIMENSION defpoints at (0,0) that are never drawn (`uncommon.dxf` min x 0.0 vs drawn 21.89) [VERIFIED].
- **`recover.readfile`**: accepts a truncated DXF that strict rejects; no corpus file needs it.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Text layout (alignment, FIT, MTEXT wrap/codes, dimension text) | Own MTEXT parser | ezdxf `draw_text` override | ezdxf already computes the matrix; reproduction measured ≥ 0.993 |
| Font metrics | Own TTF reader in the helper | ezdxf font manager (`glyph_cache`) | The SVG must use the same metrics ezdxf used for layout |
| XML escaping in the helper | String formatting | `xml.etree.ElementTree` | `&<>"'` round trip measured exact |
| XML escaping in Go | `fmt.Sprintf` | `xml.EscapeText` + attribute escaper on re-serialization | Same reason; Go output is what ships |
| Line clipping for XLINE/RAY | Own Liang-Barsky | `ezdxf.math.clipping.ClippingRect2d.clip_line` | Present in 1.4.4 |
| SVG rasterization/PDF | Own renderer | rsvg-convert | Locked |

## Runtime State Inventory

Not a rename/refactor phase — section omitted.

## Common Pitfalls

### Pitfall 1: rsvg-convert succeeds and the text is gone
**What goes wrong:** rsvg-convert exits 0 with no text in the PNG/PDF when the font is unreadable (no `/usr/share/fonts` under Landlock) or when pango/FreeType reject a font scale.
**Why:** pango warns on stderr and draws nothing; the exit code does not change.
**How to avoid:** judge the PDF by decoded text; keep `output_coordinate_space=10000` and normalized font size; preflight renders the cp1251 fixture and requires `Деталь` back from the PDF.
**Warning signs:** `Pango-WARNING … failed to create cairo scaled font` on stderr.

### Pitfall 2: the expanded-insert cap is not a work cap
**What goes wrong:** 170000 instances of a 1000-entity block pass a 170031-instance cap and mean 170 M drawn entities.
**Why:** the 71 cap was measured with a 5-entity block.
**How to avoid:** also cap expanded entities at 850155 (guard) and count drawn virtual entities in the helper (backstop).

### Pitfall 3: the guard and ezdxf disagree on block identity
**What goes wrong:** the guard keys `A` and `a` as different blocks and misses the cycle; ezdxf resolves `INSERT a` to block `A` [VERIFIED: probe, ezdxf audit code 104].
**Why:** ezdxf keys blocks by `name.lower()` (sections/blocks.py:116-119) on text decoded with the document encoding (cp-page before R2007, UTF-8 from R2007), and does not strip spaces.
**How to avoid:** decode names the way ezdxf does (`$ACADVER`, `$DWGCODEPAGE`) before `.lower()`. Prove it with a differential test (guard verdict vs ezdxf audit 104) over case, non-ASCII case and space variants. Keep the helper's audit-104 → `too_complex` backstop.

### Pitfall 4: deterministic crashes classified as retryable
**What goes wrong:** without the guard, `cyclic_self`, `cyclic_ab` and `nest_d200` crash with `RecursionError`, and `inf`/`1e400` crash with `ValueError: cannot convert float NaN to integer` (rc 1). D-13 makes a crash on validated input retryable, so each burns 3 attempts.
**How to avoid:** guard rules 2, 5 and 6, plus the helper catching `RecursionError`/`MemoryError` → `too_complex`.

### Pitfall 5: NaN is accepted silently
**What goes wrong:** `nan` coordinates read and draw (the line disappears) with verdict ok; `1e300` reads and draws a degenerate frame.
**How to avoid:** guard real syntax + finiteness + the section-scoped magnitude rule.

### Pitfall 6: a frame defined by ink that does not rasterize
**What goes wrong:** a stray POINT far from the drawing sets the frame; the drawing shrinks into a corner and 71 `frac` rejects it.
**How to avoid:** with crop off, the point is drawn and the span rule passes; record `far_point` as an explained divergence. Whether a stray-point-dominated drawing should succeed is a product question (Open Question 4).

### Pitfall 7: HOME is `/nonexistent` in production
**What goes wrong:** the 73/74 Landlock measurements used `HOME=/job`, but the cad-worker container runs with `HOME=/nonexistent` [VERIFIED: `echo $HOME` in the image], and `engineEnv` copies HOME from the worker.
**How to avoid:** the DXF stages get `HOME=<job>/.home`, `XDG_CACHE_HOME=<job>/.home/.cache`, `XDG_CONFIG_HOME=<job>/.home/.config` from Go (not only from the helper), because rsvg-convert's fontconfig writes `$XDG_CACHE_HOME/fontconfig` (4 files, ~16 KB) when `/var/cache/fontconfig` is not granted.

### Pitfall 8: hatch timeouts are partial, not failures
**What goes wrong:** ezdxf `Configuration.hatching_timeout` (30 s) stops drawing a hatch pattern and moves on; the result is a partially hatched drawing with no signal.
**How to avoid:** not measured here; log/count it in the verdict if the frontend exposes it, or note it as a known limit (low priority).

### Pitfall 9: the SVG at the cap is large
**What goes wrong:** at 170000 cells the SVG is 105 MB (prototype, 1e6 space); the client downloads it for dxf→svg and Go parses it twice if not streamed.
**How to avoid:** stream validate→re-serialize; consider an SVG byte ceiling as a backstop; re-measure size with the 10 000 space.

## Code Examples

### Inline presentation attributes instead of CSS classes
```python
class SvgNoCss(svg.SVGRenderBackend):
    def add_strokes(self, d, properties):
        if not d:
            return
        el = ET.SubElement(self.entities, "path", d=d)
        color, opacity = self.resolve_color(properties.color)
        el.set("fill", "none")
        el.set("stroke", color)
        el.set("stroke-width", str(self.resolve_stroke_width(properties.lineweight)))
        if opacity < 1.0:
            el.set("stroke-opacity", "%.3f" % opacity)

    def add_filling(self, d, properties):
        if not d:
            return
        el = ET.SubElement(self.entities, "path", d=d)
        color, opacity = self.resolve_color(properties.color)
        el.set("fill", color)
        if opacity < 1.0:
            el.set("fill-opacity", "%.3f" % opacity)

class Backend(svg.SVGBackend):
    @staticmethod
    def make_backend(page, settings):
        return SvgNoCss(page, settings)
```

### Finite frame excluding XLINE/RAY
```python
finite = BoundingBox2d()
current = {"kind": None}

class Backend(svg.SVGBackend):
    def enter_entity(self, entity, properties):
        if not entity.is_virtual:
            current["kind"] = entity.dxftype()

    def store(self, record, properties):
        if current["kind"] not in ("XLINE", "RAY"):
            b = record.bbox()
            if b.has_data:
                finite.extend(b)
        svg.SVGBackend.store(self, record, properties)
```

### XLINE/RAY clipped after the main pass
```python
rect = ClippingRect2d(Vec2(finite.extmin), Vec2(finite.extmax))
reach = (finite.size.x + finite.size.y) * 2 + 1
current["kind"] = "XLINE"
for entity, props in stashed:
    d = Vec2(entity.dxf.unit_vector).normalize()
    p0 = Vec2(entity.dxf.start)
    a = p0 - d * reach if entity.dxftype() == "XLINE" else p0
    for s, e in rect.clip_line(a, p0 + d * reach):
        fe.pipeline.draw_line(s, e, props)
fe.pipeline.finalize()
```

## Evidence

### D-02: strict vs recover on the 17-file corpus [VERIFIED: run this session]

| File | `readfile` + `audit()` | `recover.readfile` | model-space entities (both) |
|---|---|---|---|
| all 17 (3dsolids, acad_table, blocks_minsert, columns_R2007, comments_999, dimension_in_nested_blocks, hatches_1, hatches_2, houses_of_parliament, legacy_cp1251, legacy_latin1, uncommon, replicate 2/5/10/20/50 MiB) | ok, 0 errors, 0 fixes | ok, 0 errors, 0 fixes | identical |
| reject/dxf_binary | `DXFStructureError` | `DXFStructureError` | — |
| reject/dxf_bom | `OSError: not a DXF file` | ok (1 entity) | — |
| reject/dxf_truncated | `DXFStructureError: missing EOF tag` | ok (1 entity) | — |

Decision: strict. No corpus file needs `recover`, and `recover` would read two of the three reject fixtures.

### SC1 checks on the prototype [VERIFIED]
- `legacy_cp1251.dxf` → `<text … font-family="DejaVu Sans">Деталь</text>`; PDF decodes `Деталь`.
- `A&B <x> "q" 'a' & <script>alert(1)</script>` → text node `A&amp;B &lt;x&gt; "q" 'a' &amp; &lt;script&gt;alert(1)&lt;/script&gt;`; PDF/pdftotext return the source string exactly.
- Off layer `OFFL` and frozen layer `FROZ`: `HIDDEN_OFF`, `HIDDEN_FROZEN` and the frozen LINE absent (`skipped {"LINE":1,"TEXT":2}`).
- `never_resolved.dxf` (IMAGE `/etc/passwd`, IMAGE `../../outside/secret.png`, PDFUNDERLAY `/etc/hostname`, XREF `/etc/passwd`) → `skipped {"IMAGE":2,"PDFUNDERLAY":1,"XREF":1}`; identical output under a ruleset that grants no `/etc`.
- `3dsolids.dxf` → `empty/nothing_drawable` (`skipped {"3DSOLID":4}`); `all_hidden.dxf` → empty.
- `wrong_extents.dxf` (`$EXTMIN/$EXTMAX` ±1e9) → bbox `[10,20,40,30]` from entities.
- `wide.dxf`/`tall.dxf` → pages 297 × 22.8 mm and 22.8 × 297 mm; letterboxed at 1280 with ink box centred.

### Landlock (D-14) — removal tables on the research image, production helper [VERIFIED]

Matrix: text_cases, legacy_cp1251, columns_R2007, uncommon, never_resolved. Environment `env -i PATH LANG=C.UTF-8 HOME=/job/home XDG_CACHE_HOME=/job/home/.cache TMPDIR=/job`, `--network none --user nobody --cpus 2 --memory 4g`, `rw:/job`. Judged by content: SVG sha256 + `<text>` count for cad-dxf; PNG sha256 and PDF sha256 with `/CreationDate` stripped for rsvg-convert.

| Stage | Entry | Result |
|---|---|---|
| cad-dxf | `/opt/venv` | **load-bearing** (rc 1) |
| cad-dxf | `/usr/bin/python3.11` | **load-bearing** (rc 78, sandbox setup) |
| cad-dxf | `/usr/lib/python3.11` | **load-bearing** (rc 1) |
| cad-dxf | `/usr/lib/aarch64-linux-gnu` | **load-bearing** (rc 78) |
| cad-dxf | `/usr/share/fonts` | **load-bearing** (fresh HOME: rc 1, `FontNotFoundError`; cached font list: text silently dropped) |
| cad-dxf | `/etc/ld.so.cache`, `/usr/lib/locale`, `/usr/local/share`, `/usr/share/zoneinfo`, `/etc/localtime`, `/sys/devices/system/cpu` | removable (SVG identical; `/usr/local/share` changes only a stdout noise line) |
| rsvg-convert | `/usr/bin/rsvg-convert` | **load-bearing** (rc 78) |
| rsvg-convert | `/usr/lib/aarch64-linux-gnu` | **load-bearing** (rc 78) |
| rsvg-convert | `/usr/share/fonts` | **load-bearing** (rc 0, PNG and PDF differ; no-font PDF has no font objects and pdftotext returns nothing) |
| rsvg-convert | `/etc/fonts`, `/usr/share/fontconfig`, `/var/cache/fontconfig`, `/etc/ld.so.cache`, `/usr/local/share`, `/etc/localtime`, `/usr/share/zoneinfo` | removable (outputs identical; fontconfig falls back to its compiled-in config and prints `Fontconfig error: Cannot load default config file` to stderr) |

Final sets, all five inputs identical to the unconfined run [VERIFIED]:
- `cad-dxf`: `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/python3.11`, `/usr/share/fonts` + multiarch (`cadMultiarchLib`) + engine. The same as the 71 candidate minus `/opt/cad-measure` (the helper moves to `/usr/local/bin/cad-dxf`).
- `rsvg-convert`: `/usr/share/fonts` + multiarch + engine `/usr/bin/rsvg-convert`. The 71 candidate plus `/usr/share/fonts`, which became load-bearing because the SVG now carries `<text>`.
- Effectiveness: cad-dxf on `/tmp/outside/in.dxf` → `unreadable/PermissionError`; rsvg-convert → rc 1, `Error opening file … Permission denied`, no output.
- No `/proc`, `/sys`, `/tmp` in either set; writable set is the job dir only.

These sets were measured with a prototype helper at `/proto/cad_dxf_proto.py`; the plan re-measures with the production helper, its argv and the Go-provided env (D-14).

### Timing and memory (D-13), `--cpus 2 --memory 4g`, prototype helper [VERIFIED]

| Input | cad-dxf wall / RSS | rsvg PNG (S=2048) wall / RSS | rsvg PDF wall / RSS | SVG size |
|---|---|---|---|---|
| corpus files ≤ 343 KB | 0.36–0.55 s / 56–73 MiB | ~0.05 s | ~0.05 s | ≤ 30 KB |
| replicate 50 MiB | 30.39 s / 494 MiB | 2.90 s / 351 MiB | 3.35 s / 466 MiB | 16.7 MB |
| replicate 100 MiB (103.5 MB, below the 118 MiB ceiling) | 58.07 s / 884 MiB | 6.48 s / 669 MiB | 7.95 s / 902 MiB | 33.7 MB |
| minsert_cap_170000 (5-entity block, at the cap) | **147.83 s / 1274 MiB** | 85.81 s / 1768 MiB | **92.74 s / 2510 MiB** | 104.8 MB |
| no guard: dxf-minsert n1000000, minsert_nested_4m, nest_d30_f2, dimension_minsert_1m | killed at 120 s | — | — | — |

Derivation by the 73 method (worst measured at or bracketing the cap/ceiling × 1.5, ceil):
- `cad-dxf`: cap governs (147.83 s; ceiling extrapolates to 58.07 × 118/98.7 ≈ 69 s). ⌈147.83 × 1.5⌉ = **222 s**.
- `rsvg-convert`: ⌈92.74 × 1.5⌉ = **140 s**.
- guard: reuse 30 s.
- DXF engine sum: 30 + 222 + 140 + 60 (IO) = **452 s** ≤ 886 s engine, ≤ 900 s reconciler.
- Unmeasured combination: a ceiling-size file that is also at the expansion cap ≈ 69 + 148 ≈ 217 s, a 1.02× margin under 222 s. Flag it, as 73 flagged its double-cap case.
- Memory: rsvg PDF at the cap is the new high-water mark, 2510 MiB against the 0.70 × 4096 − 24.9 = 2842 MiB budget (88%).

All of this was measured with the 1e6 coordinate space and un-normalized font sizes; Wave 0 re-measures the cap and ceiling rows with the production helper.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| ezdxf SVG text as outline paths (71 `cad_dxf.py`) | `<text>` via pipeline override | this phase | Text extractable; rsvg needs fonts |
| 71 `cad_dxf.py` used `recover.readfile` | strict `readfile` + audit gate | this phase (D-02) | Truncated files no longer silently accepted |
| 71 `rsvg-convert` set: binary + multiarch | + `/usr/share/fonts` | this phase | Font is load-bearing for rsvg |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The 850155 expanded-entity cap (170031 × 5) is the right second cap; 71 never measured block sizes other than 5 | Pattern 2 | A larger-block grid under the cap could exceed the 222 s stage timeout (terminal timeout, not OOM) |
| A2 | Block-name decoding in the guard can match ezdxf for every codepage in practice | Pitfall 3 | Residual cycles reach ezdxf; backstop (audit 104, RecursionError) still makes them terminal |
| A3 | Wave-0 re-measure with `output_coordinate_space=10000` will not raise times above the derived timeouts | Timing | Timeouts would need re-derivation |
| A4 | The removable fontconfig entries stay removable on amd64 | Landlock | amd64 is not measured (no Landlock under emulation), as in 73/74 |
| A5 | No production DXF depends on `recover` | D-02 | The private flow is unmeasured (71 `PRIVATE_CORPUS files=0`) |

## Open Questions

1. **Kerning delta.** Centre/right/FIT text can drift up to ~3% of its width. Accept and document (recommended), or patch ezdxf to kern (`TTFontRenderer(font, kerning=True)`), which changes ezdxf's own layout and was not tested.
2. **D-10 bbox source.** "Via ezdxf bbox" taken literally (`bbox.extents` of drawn entities) includes undrawn DIMENSION defpoints (`uncommon`: 0.0 vs 21.89). Recommended: the frame box (ezdxf `BoundingBox2d` of the recorded primitives + text glyph boxes), which is exactly what the picture shows. Needs a one-line confirmation.
3. **PDF text rule for glyphs outside DejaVu.** Recommended: ordered subsequence over font-covered characters, with the uncovered count recorded. Exact equality would fail every CJK drawing.
4. **Stray far-away entities.** `far_point` passes the proposed rule (the dot is drawn), so the drawing is tiny in the frame. Accept (recommended, honest framing), or exclude isolated POINTs from the frame.
5. **Timeout env names** (`CAD_DXF_TIMEOUT`, `CAD_RSVG_TIMEOUT`) and `ValidateCADTimeouts` gaining the DXF sum — naming is the planner's.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker (OrbStack, arm64) | image build, Landlock measurements | ✓ | — | — |
| Free VM disk | builds | ✓ | 11 GiB after `docker builder prune` (was 6.6 GiB) | prune builder cache only |
| Go | worker code | ✓ | `GOTOOLCHAIN=go1.26.4` | — |
| `octoconv-cad-worker:latest` | base for measurement builds | ✓ | id 99952ab3c0ad, 1 044 299 781 B | rebuild from Dockerfile.cad-worker |
| DXF corpus 12 committed + 5 replicate in `~/.cache/octoconv/cad-corpus/scaled/` | evidence | ✓ | plus 100/200 MiB replicate and minsert n100…n10000000 | `scripts/cad-corpus-gen.py` |
| pdftotext (host, Homebrew) | independent PDF cross-check in tests/evidence only | ✓ | — | Go judge |
| amd64 Landlock | amd64 set | ✗ | — | build proof only (as 73/74) |

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib); Python helpers exercised through Go tests and evidence scripts |
| Config file | none |
| Quick run command | `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'CAD|DXF' -count=1` |
| Full suite command | `GOTOOLCHAIN=go1.26.4 go test ./... -count=1` plus `scripts/conf05-crosscheck.sh cad` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CCONV-02 | SVG allowlist accepts helper output, rejects DOCTYPE/PI/comment/second root/foreign ns/`style`/`href`/`on*` | unit (Go) | `go test ./internal/convert -run TestDXFSVG` | ❌ Wave 0 |
| CCONV-02 | `&<>"'` round trip: SVG text node == source | integration (image) | `scripts/cad-liveproof.sh`-style DXF case | ❌ |
| CCONV-02 | cp1251 `Деталь`, off/frozen layers absent | integration (image) | same | ❌ |
| CCONV-03 | PDF judge: one page, paint ops, text decodes; rejects no-font PDF and outline PDF | unit (Go, fixture PDFs from rsvg) + differential vs pdftotext | `go test -run TestDXFPDFJudge` | ❌ |
| RNDR-06 | view/quality on dxf → 422 opts_not_applicable; dxf convert with any opt → 422 | unit (Go) | `go test ./internal/convert -run TestValidateCADApplicability` and API handler test | partial (cadopts_test.go exists) |
| RNDR-06 | letterbox: wide/tall centred, S×S, not cropped | integration | drawing-judge test on rendered frames | ❌ |
| MEAS-01/03 | dxf measurements: 2D bbox from entities (wrong_extents), `$INSUNITS` table, assume_unit only when unspecified | unit (Go) | `go test -run TestCADMeasurementsDXF` | ❌ |
| CADIN-04 | guard verdicts on bombs; differential guard vs ezdxf audit on name variants | unit (Python via Go exec) + differential script | `go test -run TestCADGuardDXF` | ❌ (cadguard_test.go exists for STEP) |
| CADW-02 | rulesets pinned to SANDBOX-PATHS; preflight both ways; negative control removes fonts | unit + image | `go test -run TestCADSandbox`, `scripts/conf05-crosscheck.sh cad`, measurement script | partial |

### Sampling Rate
- **Per task commit:** quick run command.
- **Per wave merge:** full suite + `conf05-crosscheck.sh cad`.
- **Phase gate:** full suite green and the image-level evidence scripts (Landlock removal tables, timing at cap/ceiling, pixel-judge differential, PDF differential) recorded with commit headers before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] Synthetic fixtures via `scripts/cad-corpus-gen.py`: text_cases, wide, tall, wrong_extents, never_resolved, far_point, cyclic_*, nest_d*, minsert_*, dimension_minsert, nonnumeric_*, block-name case/space/non-ASCII variants.
- [ ] Re-measure cad-dxf/rsvg at the cap and near the ceiling with the production helper (coordinate space 10 000) and confirm 222 s/140 s or re-derive.
- [ ] Re-measure both Landlock sets with the production helper, its argv and the Go env.
- [ ] Differential harness: guard vs ezdxf audit (cycles, names), Go PDF judge vs pdftotext, Go drawing judge vs 71 pixelcheck (opaque frames).

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (unchanged API-key auth) | — |
| V3 Session Management | no | — |
| V4 Access Control | no (unchanged) | — |
| V5 Input Validation | yes | Closed opts parser + `ValidateCADApplicability`; guard numeric/structure rules; SVG allowlist with value grammars; PDF judge |
| V6 Cryptography | no | — |
| V12 Files and Resources | yes | Never resolve IMAGE/UNDERLAY/OLE/XREF paths; Landlock rw only the job dir |
| V14 Configuration | yes | Hash-locked wheels, apt subset of the 71 list, measured rulesets pinned by test |

### Known Threat Patterns for DXF → SVG/PDF

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Script/handler/external-ref injection via DXF text into SVG | Tampering / XSS on the client | ElementTree escaping in the helper; Go allowlist + re-serialization; no `style`/`href`/`on*` possible |
| DTD/entity expansion in SVG | DoS | Helper emits no DTD; Go rejects `Directive`; rsvg default libxml2 limits (no `--unlimited`) |
| Local file read via IMAGE/UNDERLAY/XREF path | Information disclosure | `ImagePolicy.IGNORE` + entity filter; Landlock grants no `/etc` |
| MINSERT/nested-block/cycle bombs | DoS | Guard caps (inserts, entities, depth, cycles); helper backstops; stage timeouts; cgroup OOM → terminal |
| NaN/inf/huge coordinates | DoS / bad output | Guard numeric rules; measurements validator `MaxAbsCoordinate` |
| Output parser differential (Go vs libxml2) | Tampering | Re-serialize so both see Go's bytes |
| Silent text loss (font missing) | Integrity | PDF text judge; preflight with the cp1251 fixture; font negative control in measurements |

## In-repo values cited (read this session)

- `internal/convert/cad.go:28-32`: `CADDefaultGuardTimeout  = 30 * time.Second`, `CADDefaultMeshTimeout   = 316 * time.Second`, `CADDefaultRenderTimeout = 480 * time.Second`, `CADIOAllowance          = 60 * time.Second`, `CADDefaultEngineTimeout = CADDefaultGuardTimeout + CADDefaultMeshTimeout + CADDefaultRenderTimeout + CADIOAllowance` [VERIFIED: cad.go:28-32].
- `internal/convert/cadsandbox.go:42-45`: `var cadGuardReadOnly = []string{ "/usr/bin/python3.11", "/usr/lib/python3.11", }`; `:59` `var cadMultiarchLib = map[string]string{"arm64": "aarch64-linux-gnu", "amd64": "x86_64-linux-gnu"}`; `:70` `env := append(engineEnv(jobDir), "LP_NUM_THREADS=2")` [VERIFIED].
- `internal/convert/cadpixel.go:22-25`: `cadExtentMin = 0.50`, `cadMarginMin = 0.03`, `cadTransparentMin = 0.05`, `cadTransparentMax = 0.95` [VERIFIED].
- `internal/measurements/measurements.go:15` `MaxAbsCoordinate = 1e12`; `:32` `ReasonNotApplicable2D = "not_applicable_2d"` [VERIFIED].
- `internal/worker/descriptors.go:62-64`: `CodeCADTooComplex = "cad_too_complex"`, `CodeCADUnreadable = "cad_unreadable"`, `CodeCADEmptyResult = "cad_empty_result"` [VERIFIED].
- `internal/api/api.go:513-519`: `defaultMaxFormatBytes` holds `"step"`, `"iges"`, `"stl"` for `convert.EngineCAD`; `"dxf"` is to be added [VERIFIED].
- `cmd/cad-worker/requirements-mesh.txt`: `numpy==2.4.6` with the same first hash `sha256:001fbb8e…` as the 71 common lock [VERIFIED].

## Sources

### Primary (HIGH confidence)
- ezdxf 1.4.4 installed source, read in the research image: `addons/drawing/{frontend,pipeline,svg,layout,config,recorder,text,mtext_complex,unified_text_renderer,gfxproxy}.py`, `fonts/{fonts,ttfonts,font_manager}.py`, `lldxf/{types,tagger}.py`, `entities/{acad_table,mleader,block}.py`, `sections/blocks.py`, `audit.py`, `math/clipping.py`, `_options.py`.
- Runs this session: prototype helper over 17 corpus + 9 synthetic + 21 bomb + 3 reject + 3 big files; Landlock removal tables with the production helper; rsvg-convert PNG/PDF; pdftotext/pdffonts/pdfinfo; Go `encoding/xml` probe; Go PDF judge prototype; guard prototype.
- `.planning/phases/71-evaluation-gate/71-DECISION.md`, `evidence/71-04-helpers.txt`, `71-04-render.txt`, `71-06-limits.txt`.

### Secondary (MEDIUM confidence)
- 73/74 RESEARCH timeout method (`73-RESEARCH.md` §4.3, `74-RESEARCH.md` render timeout section).

### Tertiary (LOW confidence)
- None used for decisions.

## Metadata

**Confidence breakdown:**
- Text mechanism (D-04): HIGH — measured against ezdxf's own rendering on corpus and synthetic files, at three sizes.
- Landlock sets: HIGH for arm64 on the research image; re-measure with the production helper is required by D-14.
- Guard rules: HIGH for the tested shapes; MEDIUM for name-decoding parity (one bypass found, fix designed, not yet implemented).
- Timeouts: MEDIUM — measured on the prototype with a different coordinate space.
- Pixel/PDF judges: HIGH — calibrated on 132 frames; PDF decoder cross-checked against pdftotext.

**Research date:** 2026-10-07
**Valid until:** 2026-11-06 (pinned versions; re-check if the base image digest or the lock changes)
