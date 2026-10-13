---
phase: 75-dxf-2d-pipeline
plan: 04
subsystem: cad-worker judges (DXF)
tags: [svg-allowlist, encoding-xml, pdf-judge, tounicode, pixel-judge, letterbox, r-9, tdd]

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-02 dxf-out fixtures (client and render-only SVGs, texts.json, PDFs, letterboxed PNGs, frames.tsv)
provides:
  - "internal/convert/caddxfsvg.go: ErrCADSVGRejected, CADSVGFacts{WidthMM, HeightMM, Texts}, ValidateCADSVG(r, w, maxBytes)"
  - "internal/convert/caddxfpdf.go: ErrCADPDFRejected, CADPDFFacts{Pages, PaintOps, TextRunes, ExpectedRunes, Text}, JudgeCADPDF(path, maxBytes, want), cadDXFText, parseCADDXFTexts(path, maxBytes), cadDXFExpectedText, exported CADDXFExpectedTextFile(path, maxBytes)"
  - "internal/convert/cadpixel.go: CADLetterbox, CADDrawingLetterbox(wMM, hMM, size), JudgeCADDrawingPNG(r, size, background, box); CADPixelFacts gains BoxX, BoxY"
  - "scripts/cmd/cad-svg-check [--max-bytes N] [--out FILE] SVG -> GOSVG line, exit 1 rejected, 64 usage"
  - "scripts/cmd/cad-pdf-check [--max-bytes N] [--texts FILE] [--print-text] PDF -> GOPDF line or decoded text, exit 1 rejected, 64 usage"
  - "scripts/cmd/cad-pixel-judge --drawing --letterbox-svg FILE and --print-letterbox --svg FILE --size N (LETTERBOX line)"
affects: [75-05, 75-07, 75-08, 75-10]

actuals:
  tokens: 24321
  tasks: 3
  commits: 6
plan_head_before: 885ac89cf4a9587f13883fe357e79ebe6ee1c6b2
plan_head_after: 1c1a1bd117152abfdd76c9af87acd1da431afad1

tech-stack:
  added: []
  patterns:
    - "Validate-and-re-serialize: encoding/xml Token() walk, per-element attribute allowlist with anchored grammars, output written token by token with Go escaping; the input bytes never pass through"
    - "Sequential PDF object scan over bufio with re-seekable SectionReaders; an indirect /Length is checked by locating endstream and requiring the resolved length to end there"
    - "Content streams tokenized incrementally from a zlib reader under a shared inflated-byte budget of 64 x the PDF limit"
    - "Drawing judge shares the frame read (cadReadFrame) and the ink mask (cadMeasure) with the 3D judge"

key-files:
  created:
    - internal/convert/caddxfsvg.go
    - internal/convert/caddxfsvg_test.go
    - internal/convert/caddxfpdf.go
    - internal/convert/caddxfpdf_test.go
    - scripts/cmd/cad-svg-check/main.go
    - scripts/cmd/cad-pdf-check/main.go
  modified:
    - internal/convert/cadpixel.go
    - internal/convert/cadpixel_test.go
    - scripts/cmd/cad-pixel-judge/main.go

key-decisions:
  - "75-04: ValidateCADSVG validates both helper SVGs (R-9); the render-only SVG's texts equal texts.json with U+FFFD at every character outside the entry's covered set"
  - "75-04: the SVG allowlist admits presentation attributes (fill, stroke, stroke-width, both opacities, fill-rule, stroke-linecap, stroke-linejoin) on g and every shape, transform only on text as matrix of six numbers, and requires xmlns, width, height and viewBox on the root"
  - "75-04: the PDF judge fails closed on Do, BI, sh and any operator outside the PDF 1.7 path, text, colour, state and marked-content sets, and on a font used for text without a ToUnicode CMap"
  - "75-04: texts.json rejections wrap ErrCADPDFRejected; a read or open failure is a plain error"
  - "75-04: JudgeCADDrawingPNG reports Mode drawing on every outcome; a rejection names J0-J3 or the background rule"

requirements-completed: [CCONV-02, CCONV-03, RNDR-06]

coverage:
  - id: D1
    description: "ValidateCADSVG accepts all 12 helper SVGs (client and render-only), re-validates its output to identical bytes with equal facts, and rejects 61 mutations naming the rule"
    requirement: CCONV-02
    verification:
      - kind: unit
        ref: "internal/convert/caddxfsvg_test.go TestValidateCADSVG* (9 tests)"
        status: pass
      - kind: integration
        ref: "go run ./scripts/cmd/cad-svg-check on every dxf-out/*.svg -> ok=1"
        status: pass
    human_judgment: false
  - id: D2
    description: "text_cases output decodes to A&B <x> \"q\" 'a' & <script>alert(1)</script> with no unescaped <script"
    requirement: CCONV-02
    verification:
      - kind: unit
        ref: "TestValidateCADSVGRoundTripsSpecialCharacters"
        status: pass
    human_judgment: false
  - id: D3
    description: "JudgeCADPDF accepts the four helper PDFs with their texts.json, rejects nofont and outline with text_cases' expectation, rejects 25 structure cases; decoded text equals pdftotext -raw on text_cases, legacy_cp1251 and uncommon"
    requirement: CCONV-03
    verification:
      - kind: unit
        ref: "internal/convert/caddxfpdf_test.go TestJudgeCADPDF* and TestCADDXFTexts* (7 tests)"
        status: pass
      - kind: integration
        ref: "Task 2 verify: cad-pdf-check --print-text vs pdftotext -raw (poppler 26.10.0), whitespace removed"
        status: pass
    human_judgment: false
  - id: D4
    description: "CADDrawingLetterbox equals all frames.tsv rows; JudgeCADDrawingPNG accepts the three helper frames and rejects blank, wrong size, edge, outside letterbox, dot, opacity and fraction cases; every JudgeCADPNG test unchanged and passing"
    requirement: RNDR-06
    verification:
      - kind: unit
        ref: "internal/convert/cadpixel_test.go TestCADDrawingLetterbox*, TestJudgeCADDrawingPNG* (6 tests), TestJudgeCADPNG*"
        status: pass
    human_judgment: false

duration: 23min
completed: 2026-10-07
status: complete
---

# Phase 75 Plan 04: SVG allowlist, PDF judge and drawing pixel judge Summary

**Three judges the DXF path needs, each built test-first on the 75-02 helper outputs:**
- a streaming `encoding/xml` allowlist that writes its own bytes;
- a stdlib PDF judge whose decoded text equals pdftotext's on the fixtures;
- a drawing mode of the pixel judge, with a Go letterbox that matches the probe's frames exactly.

## Performance

- **Duration:** about 23 min (18:39Z to 19:02Z)
- **Tasks:** 3 of 3, each as a RED test commit and then a GREEN commit
- **Files:** 9 (6 created, 3 modified)

## Accomplishments

### Task 1: ValidateCADSVG (e1b38ce)

- **Streaming:** the input goes through `io.LimitReader(maxBytes+1)` and a counting reader. Exceeding the limit is a rejection. A read error from the source comes back as a plain error, not a rejection.
- **Decoder:** strict, no Entity map, nil CharsetReader, `Token()` only.
  - An XML declaration is accepted only as the first token, with version 1.0 and encoding absent or UTF-8 in any case. The helper writes `encoding='utf-8'`.
  - Every Directive, Comment and other ProcInst is rejected.
- **Structure:**
  - every element must be in the SVG namespace;
  - there is exactly one root `svg`, and its only namespace declaration is the default `xmlns`;
  - nothing but whitespace may follow the root;
  - character data is allowed only in `text` and `tspan`;
  - depth is at most 16;
  - a containment table fixes which elements may appear inside which (for example, `clipPath` only inside `defs`).
- **Grammars:**
  - numbers are finite;
  - colours are `#rrggbb` or `none`;
  - opacities are in 0..1;
  - path `d` uses the research character set;
  - `matrix(...)` takes six numbers;
  - fonts are the three DejaVu families;
  - the root takes `width` and `height` in mm (positive, bounded digits) and `viewBox` `0 0 W H` (positive integers);
  - attributes may not repeat.
- **Output:** the output is the Go declaration, then the tags in input order with Go escaping of attributes (`& < > " '` tab LF CR) and text (`xml.EscapeText`). Every element gets an explicit end tag.
- **Facts:** `WidthMM` and `HeightMM` come from the root; `Texts` holds one entry per text element, with tspan content joined.
- **Speed:** a 104.5 MB SVG with 1.1 M paths validates in 5.1 s at 24 MB RSS on the host.
- **Tests:** all 12 dxf-out SVGs, the special-character round trip, a control-whitespace fixed point, the D-05 shapes with tspan and an attribute-less clipPath inside defs, depth 16 at the exact byte limit, 61 mutations, read and usage errors, and the 40-character quote bound.

### Task 2: JudgeCADPDF and texts.json (f510f60)

- **Size limits:** a file over maxBytes is rejected before it is parsed.
- **Header:** the file must start with `%PDF-`.
- **Object scan:** objects are read in one sequential pass. An object number defined twice, more than one xref section, trailing tokens after `%%EOF`, or a missing trailer is a rejection.
- **Stream lengths:** a direct `/Length` must land on `endstream`. cairo writes the length as an indirect object after the stream, so the scanner finds `endstream` first and then requires the resolved length to end exactly there, allowing at most one EOL in between.
- **Rejected structures:**
  - `/Type /ObjStm` and `/Type /XRef`
  - `/Encrypt` or `/XRefStm` in the trailer
  - any `/Pages` with `/Count` other than 1
  - a page count other than 1
  - a filter other than FlateDecode, and `/DecodeParms`, on any stream the judge reads
- **Content:**
  - Content is tokenized incrementally, including literal strings with every escape, octal and line continuation, plus hex strings, names, numbers, arrays and dictionaries.
  - The Tf font is decoded through its ToUnicode CMap (bfchar and bfrange, both string and array forms, UTF-16 surrogates). Simple fonts use 1-byte codes; Type0 Identity-H fonts use 2-byte codes.
- **Rules:**
  - The page must have at least one paint operation beyond the background fill. Without one it is an `empty page`.
  - For R-3, the whitespace-stripped decoded text must contain the expected runes as an ordered subsequence; otherwise it is rejected as `missing text: K of N`.
- **Fixture results:**

  | PDF | ok | paint_ops | text_runes | expected_runes |
  |---|---|---|---|---|
  | text_cases | 1 | 29 | 147 | 145 (2 U+FFFD from the render-only SVG) |
  | legacy_cp1251 | 1 | 2 | 6 (`Деталь`) | 6 |
  | uncommon | 1 | 232 | 299 | 299 |
  | hatches_2 | 1 | 13 | 40 | 40 |
  | nofont, text_cases expectation | 0 | 169 | 0 | 145, `missing text: 0 of 145` |
  | outline, text_cases expectation | 0 | 29 | 0 | 145, `missing text: 0 of 145` |

- **Parity with pdftotext:**
  - Go's decoded text equals `pdftotext -raw` (poppler 26.10.0, host), with whitespace removed, on text_cases (147), legacy_cp1251 (6) and uncommon (299).
  - It also matches on hatches_2 (40), nofont (0) and outline (0).
- **Speed:** a synthetic PDF with 112 MB of inflated content and 2 M paint operations is judged in 3.3 s at 24 MB RSS.
- **parseCADDXFTexts:**
  - the read is bounded;
  - it uses DisallowUnknownFields;
  - the file must hold exactly one value;
  - `texts`, `text` and `covered` must all be present;
  - `covered` must be an ordered subsequence of `text`.

  `cadDXFExpectedText` concatenates the covered strings with whitespace removed.

### Task 3: drawing judge and letterbox (1c1a1bd)

- **CADDrawingLetterbox:** reproduces every frames.tsv row:
  - wide at 1280 is 1203 x 92 at 38,594;
  - tall at 256 is 18 x 241 at 119,7;
  - a square at 1280 is 1203 x 1203 at 38,38.

  A non-positive or infinite dimension, or a size outside 256..2048, is an error.
- **JudgeCADDrawingPNG:**
  - **J0:** the frame size.
  - **Opaque background:** no transparent pixel.
  - **Transparent background:** alpha must be present and the transparent fraction at least 0.05. There is no upper bound and no opaque-pixel requirement.
  - **J1:** at least one ink pixel and an ink fraction of at least 0.0003.
  - **J2:** the ink box's long side is at least half the letterbox's long side.
  - **J3:** the margin is at least `int(0.03 x S)`, and the ink box stays inside the letterbox within 2 px.
  - There is no dark rule.
- **Helper frames:**

  | Frame | Ink box | Margin | Fraction | Notes |
  |---|---|---|---|---|
  | wide_1280_opaque | 1123 x 13 at 78,633 | 78 | 0.002095 | |
  | wide_1280_transparent | same box | 78 | 0.002095 | frac_transparent 0.9979 |
  | tall_256_opaque | 4 x 225 at 126,15 | 15 | 0.006927 | dark_count 0, so the 71 dark rule would have rejected it |

- **JudgeCADPNG:** behaviour is unchanged. Its frame read now lives in `cadReadFrame`, and `cadMeasure` also records BoxX and BoxY. No existing cadpixel test line was removed or changed.

## Deviations from Plan

### R-9 adaptation (plan text predates R-9)

**1. Both SVGs validated, with render-only texts derived from texts.json.**
- The fixture test runs over all 12 SVGs: the client SVG and the `.render.svg` of each drawing.
- For a render-only SVG, the expected texts are the texts.json texts with U+FFFD at every character that is not in that entry's `covered` set. This follows the helper's substitution rule, which tests membership in the font cmap.
- The PDF expectation stays the covered characters (R-3). The PDFs are drawn from the render-only SVG, so text_cases decodes to 147 runes against 145 expected; the two extra runes are U+FFFD.

### Auto-fixed Issues

**2. [Rule 3 - Blocking] Exported `CADDXFExpectedTextFile`.** `cad-pdf-check` is package main and cannot call the unexported `parseCADDXFTexts`. The wrapper is a thin, tested call of parse plus expected text (f510f60).

**3. [Rule 2] Grammar decisions within D-05 that the plan left open:**
- presentation attributes are allowed on `g` and on every shape, not only on the shapes the helper writes them on;
- the full keyword sets are allowed for linecap, linejoin and fill-rule;
- `points` must hold an even count of finite numbers;
- `xml:space` takes `preserve` or `default`;
- `matrix` accepts comma or space separators;
- the root requires `xmlns`, `width`, `height` and `viewBox`, and its mm values must be positive with at most 6+6 digits;
- duplicate attributes are rejected.

**4. [Rule 2] Fail-closed PDF additions:**
- `Do`, `BI` and `sh` are rejected, along with every operator outside the known sets;
- a font used for text without a ToUnicode CMap is rejected;
- a Type0 font must use Identity-H;
- an odd-length string for a two-byte font is rejected;
- `/DecodeParms` is rejected;
- object numbers must be positive;
- caps apply to object count (65536), values outside streams, nesting (32), operands per operator (4096), string length (1 MiB), CMap entries and decoded text (16 MiB).

**5. [Rule 3] CADPixelFacts gains BoxX and BoxY.** J3's letterbox containment needs the ink box position, and `cadMeasure` already computed it.

**6. GOJUDGE drawing fields.** With `--drawing`, the GOJUDGE line appends `box_x, box_y, frac6, lb_left, lb_top, lb_w, lb_h` after the existing fields, so the 3D line is unchanged. `mode=drawing` is printed for accepted and rejected frames alike.

## Notes for later plans

- **75-07 (texts cross-check, not probed here):**
  - An XML parser normalizes a raw CR in character data to LF; Python's ElementTree writes CR unescaped.
  - If any DXF text reached the helper's emitted list with a CR, `facts.Texts` would hold LF where texts.json holds CR, and an exact-equality cross-check would reject a legitimate drawing.
  - Not probed: ezdxf is not on the host, and `prepare_string_for_rendering` may already strip CR.
  - Worth one fixture before the cross-check is made terminal.
- **75-05:** all three CLIs default to a 1 GiB input limit, so measurement rows are not cut short. Production limits are the plan's `CADMaxDXFSVGBytes` and `CADMaxDXFPDFBytes`.
- **cad-pdf-check:** a malformed texts.json exits 64, the same as a usage error. Only a PDF rejection exits 1.

## Known Stubs

None.

## Threat Flags

None. These are pure functions and local CLIs: no endpoint, no wiring and no registration (`TestDefaultRegistersOnlyAllowlistedCADPairs` passes). T-75-13 to T-75-17 are mitigated as planned:
- 61 SVG mutation rejections;
- re-serialization;
- no DTD;
- streaming with byte and depth bounds;
- the decoded-text rule with pdftotext parity;
- the PDF byte and inflate caps.

## Verification

- **Task 1 gate:**
  - vet passes;
  - 9 PASS and 0 FAIL among the SVG tests;
  - every dxf-out SVG prints `ok=1`;
  - `grep -nE 'io\.ReadAll|os\.ReadFile' caddxfsvg.go` prints nothing;
  - there are 61 mutation rows, each asserting `errors.Is(err, ErrCADSVGRejected)`.
- **Task 2 gate:**
  - vet passes;
  - 7 PASS and 0 FAIL;
  - the three real PDFs print `ok=1`;
  - their Go text equals pdftotext's and is non-empty;
  - the nofont PDF is rejected;
  - `diff <(git show main:go.mod) go.mod` is empty.
- **Task 3 gate:**
  - vet passes;
  - 6 drawing-judge PASS, 1 letterbox PASS and 0 FAIL, with all JudgeCADPNG tests passing;
  - the CLI prints `LETTERBOX,sw=1203,sh=92,left=38,top=594` for wide.svg at 1280;
  - the cadpixel_test.go diff against main has no removed lines.
- **No comments:** no added `//` line in any file of this plan.
- **Plan verification:**
  - `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` are clean;
  - `go test ./... -count=1` is green, with internal/convert at 170.6 s and internal/repohygiene passing;
  - `git diff --exit-code main -- internal/convert/exec.go go.mod go.sum` is empty.
- **Untouched files:** STATE.md, ROADMAP.md and config.json were not modified by this plan. config.json carries the orchestrator's pre-existing edit, which was never staged.
- **Ledger:** I did not append to the broken-windows ledger. No `gsd_run` resolver was in this shell, and the ledger is optional.

## Self-Check: PASSED

- All nine key files exist.
- Commits 12e156e, e1b38ce, bc11149, f510f60, 7726468 and 1c1a1bd are ancestors of HEAD and SSH-signed (G).
- The measured count is 6 since 885ac89.
