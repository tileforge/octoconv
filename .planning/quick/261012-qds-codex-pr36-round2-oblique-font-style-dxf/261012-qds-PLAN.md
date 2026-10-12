---
phase: quick-261012-qds
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - cmd/cad-worker/helper/cad-dxf
  - internal/convert/caddxfsvg.go
  - internal/convert/caddxf.go
  - internal/convert/caddxfsvg_test.go
  - internal/convert/caddxf_test.go
  - scripts/cad-corpus-gen.py
  - scripts/cad-dxf-probe.sh
  - scripts/cad-liveproof.sh
  - internal/convert/testdata/cad/dxf/text_fonts.dxf
  - internal/convert/testdata/cad/dxf-out/text_fonts.*
  - internal/convert/testdata/cad/PROVENANCE.md
  - README.md
  - docs/ERROR-CODES.md
  - docs/MEASUREMENTS.md
  - docs/openapi.yaml
autonomous: true
requirements: [quick-261012-qds]
---

# Quick 261012-qds: codex PR #36 round 2 (font style, DXF docs, liveproof counters)

## Objective

Three findings on PR #36 at 2d8d250, each fixed as a class:

1. `cad-dxf` measures text with the font file ezdxf resolved but tells the SVG only the family and
   a `"Bold" in subfamily` flag, so any face whose slant, weight or width is not Book/Bold renders
   from a different file than the one that sized the frame.
2. README and docs/ERROR-CODES.md still say the `cad` engine answers every DXF with
   `unsupported_conversion`; phase 75 made DXF -> SVG, PDF and the PNG render route real.
3. `scripts/cad-liveproof.sh` records `pdftotext_clear_lines` and `pdftotext_gone_lines` without
   asserting them.

## Premise check (done before planning)

The cad-worker image installs `fonts-dejavu-core` only: six faces (Sans, Sans Mono, Serif, each
Book and Bold). No oblique, italic, condensed or light face exists there. A style that names
`DejaVuSansMono-Oblique.ttf` falls back to `DejaVuSans.ttf` and is measured and emitted with that
face, so the shipped image cannot show the codex defect today. With `fonts-dejavu-extra` added
(scratch image) the defect is real: the oblique Mono files are measured oblique and emitted
upright, and condensed and light faces get families (`DejaVu Sans Condensed`, `DejaVu Sans Light`)
that the Go allowlist rejects.

Decision: do not ship more fonts. ezdxf's fallback list prefers `DejaVuSansCondensed.ttf` to
`DejaVuSans.ttf`, so adding the extra package would change the default face of every DXF text,
every committed fixture and the PDF text floor. Fix the class in the helper so that any face
round-trips, and prove the oblique, italic, light and condensed paths with a probe variant of the
engine image that adds `fonts-dejavu-extra`.

## Rules fixed before any code

- **Font attributes come from the measured font's tables, never from a name substring.**
  - family: name ID 16 (typographic family), else name ID 1;
  - weight: OS/2 `usWeightClass` rounded to a CSS weight and clamped to 100..900: 400 is omitted,
    700 is written `bold` (so existing outputs keep their bytes), anything else is the number;
  - style: italic when OS/2 `fsSelection` bit 0 or `head.macStyle` bit 1 is set; written
    `oblique` when `fsSelection` bit 9 is set or the subfamily (name ID 17, else 2) says Oblique,
    `italic` otherwise; upright faces get no attribute;
  - stretch: OS/2 `usWidthClass` other than 5 is written as its CSS keyword.
- **Go allowlist (text and tspan):** `font-style` in {italic, oblique}; `font-weight` in {bold,
  100, 200, 300, 500, 600, 800, 900}; `font-stretch` in the eight non-normal CSS keywords;
  `font-family` stays the three DejaVu families. Anything else is rejected.
- **Client and render-only SVG agree on every text's font:** the validator records each text's
  font descriptor (family, size, weight, style, stretch of the text and its tspans) and the
  cross-check rejects any difference.
- **Round trip:** for every font file the helper can resolve, the PDF that rsvg-convert makes from
  the render-only SVG embeds the font whose PostScript name equals the measured file's. Proven in
  the shipped engine (6 faces) and in the probe variant with `fonts-dejavu-extra` (22 faces).
- **Geometric oblique** (DXF TEXT oblique angle, style oblique angle, width factor) is already a
  shear and scale in the text transform; it is kept there and covered by the fixture.

## Tasks

### Task 1: font round trip (RED first)

- RED: Go tests for `font-style` and `font-stretch` on text and tspan, numeric weights, the
  cross-check font agreement, and the real `text_fonts` outputs fail on HEAD; the probe `fonts`
  group fails on HEAD in the extra-fonts variant.
- `cad-dxf`: font attributes from the tables (rules above).
- `caddxfsvg.go`: allowlist, `CADSVGFacts.TextFonts`; `caddxf.go`: cross-check.
- `scripts/cad-corpus-gen.py --dxf-fixtures`: `text_fonts.dxf` with styles naming each shipped
  face, both Sans Mono oblique files, extended font data (Sans Mono italic, italic bold), and a
  TEXT with oblique angle 15 and width factor 0.7; fixture count 44; `fixtures --write` keeps every
  committed fixture byte-identical.
- `scripts/cad-dxf-probe.sh helper --group fonts`: per resolvable font file, one-TEXT DXF, helper
  confined, rsvg-convert to PDF confined, `pdffonts` PostScript name equals the file's;
  `OCTOCONV_PROBE_FONT_PACKAGES` builds the probe variant. `text_fonts` joins the write-out set.

### Task 2: liveproof counters

- Assert `pdftotext_clear_lines=0` and `pdftotext_gone_lines=0`; sweep every recorded counter in
  the script and assert or justify each.
- Prove the assertion fires on a crafted PDF that carries the invisible text, then revert.
- New rows: DXF_SVG text_fonts (font attributes per text), DXF_PDF text_fonts (`pdffonts` lists
  exactly the expected faces), DXF_RENDER text_fonts 1280 (drawing judge keeps the ink inside the
  frame).

### Task 3: docs

- Sweep README.md, docs/*.md, docs/openapi.yaml and chart values for statements made false by
  phase 75; every number grepped from code.

## Verification

- `go test ./...` green (GOTOOLCHAIN=go1.26.4, py314 on PATH, 15m); `go vet`, gofmt clean.
- Probe fixtures, helper fonts (shipped and extra variant) and write-out rows ok.
- Liveproof ALL PASS from a fresh clone at the final commit, bundle copied, tracked dirt 0;
  `scripts/ci-local.sh helm gate race` from the clone.
- `internal/convert/exec.go` byte-identical to main.
