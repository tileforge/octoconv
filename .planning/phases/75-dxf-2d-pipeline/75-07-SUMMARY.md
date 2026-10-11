---
phase: 75-dxf-2d-pipeline
plan: 07
subsystem: cad-worker DXF converters and worker mapping
tags: [dxf, svg, pdf, png, rsvg-convert, cross-check, line-endings, measurements, error-codes, tdd]
status: complete

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-02 cad-dxf outputs and dxf-out fixtures; 75-04 ValidateCADSVG, CADDrawingLetterbox, JudgeCADDrawingPNG; 75-05 JudgeCADDXFPDF (R-10); 75-06 constants, stage classes, cadDrawStage, DXF option contract
provides:
  - "internal/convert/caddxf.go: CADDXFConverter{CADConverter} (dxf>svg, dxf>pdf), CADDXFRenderConverter{CADConverter} (render, dxf>png), dxfDrawing, dxfStage, rsvgStage, cadDXFCrossCheck, cadXMLLineEnds, cadDXFWriteNew; both unregistered"
  - "MIMEType(svg) = image/svg+xml"
  - "internal/worker: isCADTerminal and cadFailureCode for ErrCADNothingDrawable, ErrCADDXFTimeout, ErrCADRsvgTimeout, ErrCADSVGRejected, ErrCADPDFRejected (terminal) and ErrCADDrawingFailed (retryable)"
  - "docs/ERROR-CODES.md DXF cases; code count unchanged"
  - "scripts/cad-dxf-cr-fixture.sh and internal/convert/testdata/cad/dxf-cr (line-ending fixtures from the engine image)"
affects: [75-08, 75-09, 75-10, 76]

tech-stack:
  added: []
  patterns:
    - "Helper outputs are untrusted files: Lstat must show a regular file, SVGs are opened O_NOFOLLOW, and every file the worker writes is removed and then created O_EXCL"
    - "Texts cross-check: texts.json against the client SVG, the render-only SVG against texts.json with U+FFFD outside covered, the verdict counts and page against the SVG; XML line-ending normalization on both sides and nothing else"
    - "Stub helpers replay the real 75-02 outputs and record argv; rsvg argv asserted against frames.tsv"

key-files:
  created:
    - internal/convert/caddxf.go
    - internal/convert/caddxf_test.go
    - internal/convert/testdata/cad/dxf-cr/ (9 files)
    - scripts/cad-dxf-cr-fixture.sh
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt
    - .planning/phases/75-dxf-2d-pipeline/deferred-items.md
  modified:
    - internal/convert/sniff.go
    - internal/convert/sniff_test.go
    - internal/convert/sandboxcoverage_test.go
    - internal/convert/testdata/cad/PROVENANCE.md
    - internal/worker/descriptors.go
    - internal/worker/pinning_test.go
    - docs/ERROR-CODES.md

key-decisions:
  - "75-07: both helper SVGs are validated on every operation (R-9); svg returns Go's client SVG, rsvg-convert reads only Go's render-only SVG (clean.svg)"
  - "75-07: the texts comparison normalizes CRLF and CR to LF on both sides, as an XML parser does; no other leniency"
  - "75-07: an ok verdict with a missing output is ErrCADDrawingFailed (retryable); an output that is not a regular file is ErrCADSVGRejected (terminal) for the SVGs and texts.json and ErrCADMeasurementsInvalid for the draft"
  - "75-07: texts.json rejections, which 75-04 wraps in ErrCADPDFRejected, are re-wrapped as ErrCADSVGRejected with %v so the PDF sentence never reaches an svg job"

requirements-completed: [CCONV-02, CCONV-03, RNDR-06]

coverage:
  - id: D1
    description: "dxf>svg and dxf>pdf return only Go's SVG or a PDF that JudgeCADDXFPDF passed; every rejection returns nothing with the D-13 class"
    requirement: CCONV-02
    verification:
      - kind: unit
        ref: "internal/convert/caddxf_test.go TestCADDXFConvert* (7 tests, 77 subtests)"
        status: pass
    human_judgment: false
  - id: D2
    description: "dxf>png is letterboxed with the frames.tsv argv, judged in drawing mode, and paired with a valid 0600 sidecar, or nothing"
    requirement: RNDR-06
    verification:
      - kind: unit
        ref: "internal/convert/caddxf_test.go TestCADDXFRender* (6 tests)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Carriage returns: no real DXF carrier reaches texts.json; an injected CR, CRLF or CR CR passes only because of the normalization, and a real difference still fails"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-cr-fixture.sh in the engine image (evidence/75-07-cr.txt)"
        status: pass
      - kind: unit
        ref: "TestCADDXFConvertNormalizesLineEndingsOnBothSides, plus a mutation run that made the normalization a no-op and failed the test"
        status: pass
    human_judgment: false
  - id: D4
    description: "Worker maps the six DXF sentinels to existing codes with fixed sentences and the D-13 terminality; the code count is unchanged"
    requirement: CCONV-03
    verification:
      - kind: unit
        ref: "internal/worker/pinning_test.go TestPinnedCADFailureVerdicts (25 subtests, 17 before)"
        status: pass
    human_judgment: false

actuals:
  tokens: 17366
  tasks: 3
  commits: 3
plan_head_before: 3bc583affbb1a2e28eaf53eb90209c64b2bef6ca
plan_head_after: f7bf04364454c6312e87cf19ff5d46eea6dfc0c6

duration: "about 2 h of wall time across a usage-limit stop and a signing stop (2026-10-11, around 09:55Z to 19:40Z)"
completed: 2026-10-11
---

# Phase 75 Plan 07: DXF converters and worker mapping Summary

**This plan builds the two DXF converters from the 75-06 plumbing and the 75-04/75-05 judges:**
- `CADDXFConverter` handles dxf to svg and dxf to pdf.
- `CADDXFRenderConverter` renders dxf to png, with a measurements sidecar.

Both return only bytes Go wrote or judged, and return nothing on any rejection. The worker now maps every DXF failure to an existing code with a fixed sentence. Neither converter is registered; plan 75-09 exposes them.

## Task Commits

All three are SSH-signed (G).

1. **Task 1:** `d090495`. DXF to SVG and PDF through the guard, cad-dxf, the Go validator and the PDF judge. Also adds the CR fixtures, MIMEType svg and the coverage registration.
2. **Task 2:** `26c3dbd`. DXF render to a letterboxed, judged PNG with the measurements sidecar.
3. **Task 3:** `f7bf043`. The worker maps the DXF and rsvg failure classes to codes and terminality, and ERROR-CODES.md describes them. My own commit attempt failed on 1Password signing (`failed to fill whole buffer`), so I halted. The orchestrator committed the staged files after the user unlocked 1Password.

## Accomplishments

### The pipeline (D-01, D-05, D-06, R-9, R-10)

Every operation runs the same steps in `dxfDrawing`:

1. **Guard.** Runs with the 75-06 DXF argv.
2. **Clear stale files.** draw.svg, draw.render.svg, texts.json, clean.svg and measure.json are removed if present.
3. **Run cad-dxf** under `cadDrawStage` with the job-local HOME:
   `--max-drawn-entities 850155 --texts JOB/texts.json --render-svg JOB/draw.render.svg [--measure JOB/measure.json] IN JOB/draw.svg`.
4. **Check the outputs.**
   - Each must be a regular file.
   - An SVG above `CADMaxDXFSVGBytes` is `ErrCADTooComplex`.
5. **Validate both SVGs** with `ValidateCADSVG`.
   - The client SVG goes into OUT for svg, or is discarded.
   - The render-only SVG goes into JOB/clean.svg for pdf and png, or is discarded.
6. **Cross-check.** Any disagreement is `ErrCADSVGRejected`:
   - texts.json, the client SVG, the render-only SVG and the verdict all hold the same number of texts;
   - each texts.json text equals the client SVG text;
   - each render-only text equals the texts.json text with U+FFFD at every character outside `covered`;
   - the verdict's `uncovered` and `render_substituted` equal the counts texts.json implies;
   - the two SVGs have the same page size and viewBox;
   - the verdict's page_w_mm and page_h_mm equal the SVG root within 1e-6.

Then, per target:

- **svg:** returns Go's client SVG. rsvg-convert never runs.
- **pdf:**
  - runs `rsvg-convert -f pdf -b white -o OUT JOB/clean.svg`, with no other flag;
  - judges the result with `JudgeCADDXFPDF(OUT, CADMaxDXFPDFBytes, texts, render-only facts)` (R-10);
  - removes OUT on rejection.
- **png:**
  - **Options:** view and quality are refused before any engine runs; size, background and assume_unit are honoured.
  - **Draft first:** the draft is parsed and checked before rendering. Its format must be dxf; a zero extent is `ErrCADEmptyResult`; `buildCADMeasurements` must succeed.
  - **Render:** the argv is `[-b white] -w sw -h sh --page-width S --page-height S --left L --top T -o OUT JOB/clean.svg`, from `CADDrawingLetterbox` of the validated SVG.
  - **Judge:** `JudgeCADDrawingPNG` decides the frame.
  - **Sidecar:** written last, as a new 0600 file. A deferred removal deletes OUT, clean.svg and the sidecar on any error.

### Carriage return (the 75-04 risk)

- **Generator.** `scripts/cad-dxf-cr-fixture.sh` runs in the engine image (`sha256:f5de09d5…`, the 75-05 image) and wrote `testdata/cad/dxf-cr`. Two runs gave identical bytes.
- **Real carriers.** No way of spelling a CR in a readable DXF reaches texts.json:
  - `^M` is dropped in TEXT and drawn as U+25AF in MTEXT;
  - `\U+000D` stays literal;
  - a raw CR byte in a value makes the file `unreadable/read_error`.
- **Injected fixture.** Python's ElementTree (the serializer cad-dxf uses) writes raw CR, CRLF and CR CR into three texts. Go's parser reads `E\nF`, `J\nK` and `G\n\nH`, while texts.json keeps the CRs.
- **Result.** The comparison normalizes CRLF and CR to LF on both sides and does nothing else:
  - the injected drawing passes for svg; for pdf it reaches the PDF judge;
  - a real text difference next to a CR still fails;
  - a CR on only one side still fails.
- **Mutation.** Making the normalization a no-op failed the test with `text 0 of texts.json differs from the client SVG`. Nothing produces a CR today, but the normalization keeps a legitimate drawing from being rejected if one ever does.

### Worker mapping (D-13)

| Sentinel | Terminal | Code | Message |
|---|---|---|---|
| ErrCADNothingDrawable | yes | cad_empty_result | the drawing contains nothing that can be drawn: model space is empty, hidden or holds only 3D solids |
| ErrCADDXFTimeout | yes | cad_too_complex | the drawing took too long to draw; simplify it or export fewer details |
| ErrCADRsvgTimeout | yes | cad_too_complex | the drawing took too long to convert to PDF or PNG; simplify it or export fewer details |
| ErrCADSVGRejected | yes | engine_error | the drawing could not be converted into a safe vector file |
| ErrCADPDFRejected | yes | cad_empty_result | the PDF of this drawing shows no recognisable content |
| ErrCADDrawingFailed | no (retried) | engine_error | the drawing could not be produced |

- ErrCADNothingDrawable is checked before the mesh empty-result sentence.
- A sandbox fault that wraps a DXF sentinel stays retryable.
- ERROR-CODES.md now covers:
  - **cad_too_complex:** DXF expansion, cyclic and over-deep blocks, and the drawing and conversion time limits;
  - **cad_unreadable:** DXF structure the strict reader refuses, and non-numeric or out-of-range coordinates;
  - **cad_empty_result:** nothing drawable, and a PDF or preview with no recognisable content;
  - **engine_error:** the sentence now lists all four fixed cad sentences.

"Nineteen codes" is unchanged, and `internal/errcode` has no diff.

## Deviations from Plan

### R-9 adaptation (the plan text predates R-9 and R-10)

**1. Two SVGs and the R-10 judge.**
- The helper argv carries `--render-svg JOB/draw.render.svg`, and both SVGs are validated on every operation.
- The cross-check covers the render-only SVG's U+FFFD substitution and the verdict's `uncovered` and `render_substituted`.
- The PDF is judged by `JudgeCADDXFPDF` with the render-only facts, not by `JudgeCADPDF` with `cadDXFExpectedText`.

### Auto-fixed Issues

**2. [Rule 2 - Security] Exclusive writes and regular-file checks.**
- **Issue:** cad-dxf and rsvg-convert both run with write access to the job dir. Either could leave a symlink where the unconfined worker later reads or writes.
- **Fix:**
  - Helper outputs must Lstat as regular files.
  - The SVGs are opened with O_NOFOLLOW.
  - clean.svg, the svg output and the sidecar are removed, then created with O_EXCL (`cadDXFWriteNew`).
  - OUT and the sidecar are cleared before rsvg-convert runs.
- **Test:** `TestCADDXFRenderWritesTheSidecarAsANewFile`, with the symlink planted by cad-dxf and by rsvg-convert. Swapping in `os.WriteFile` failed the rsvg-convert case: the document was written through the symlink. My first version of the test planted the symlink only from cad-dxf, and the earlier clear removed it, so that version could not fail. I added the rsvg-convert planter.
- **Commit:** 26c3dbd (render), d090495 (convert).

**3. [Rule 1] A test case that passed for the wrong reason.** "The render-only SVG has another page size" was first caught by the text count. I replaced it with a width-only mutation and a viewBox-only mutation, and logged each rejection reason to confirm it. The viewBox rejection message now names the viewBox.

### Additions within the plan's intent

**4. The CR fixtures live in a new `testdata/cad/dxf-cr/` directory.** The probe gates count the files in `dxf/` and `dxf-out/`, and the 75-04 fixture test globs every `dxf-out/*.svg`. The generator script, its PROVENANCE section and `evidence/75-07-cr.txt` come with the fixtures.

**5. Extra refusals and checks:**
- an output path that collides with a job-dir helper name is refused, like the input;
- an ok verdict with a missing output is `ErrCADDrawingFailed`;
- the draft must be a regular file.

### Deferred (logged in deferred-items.md, not fixed here)

- **The 3D render sidecar.** `internal/convert/cadrender.go:126` writes it with `os.WriteFile` after the confined cad-render stage. This is the same class as item 2, in phase 74 code.
- **The cad-mesh test and the host Python.** `TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL` needs a host python3 of 3.10 or later; see the Verification section.

## Verification

- **Task 1 gate:** vet clean; 7 TestCADDXFConvert* pass (the gate needs 6); TestConfinedClassesExecOnlyThroughTheSandbox and TestDefaultRegistersOnlyAllowlistedCADPairs pass. Acceptance:
  - `--unlimited` appears 0 times;
  - `CADDXF` appears 0 times in converters.go and cmd/cad-worker/main.go;
  - no `//` line was added.
- **Task 2 gate:** vet clean; 6 TestCADDXFRender* pass (the gate needs 5); the whole convert package passes (224 s); `o.View` appears 0 times.
- **Task 3 gate:** vet clean; the worker and errcode packages pass; 25 CAD pin subtests pass (the gate needs 23); `git diff main -- internal/errcode` is empty; no added comment.
- **Plan verification:**
  - `GOTOOLCHAIN=go1.26.4 go vet ./...` is clean.
  - `git diff --exit-code main -- internal/convert/exec.go internal/convert/converters.go internal/api` is empty.
  - gofmt is clean.
- **`go test ./... -count=1`, first run: FAIL.** One test failed with this shell's `python3`, `/usr/bin/python3` 3.9.6: `TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL`, `cad-mesh printed no verdict`. The helper raises `TypeError: 'staticmethod' object is not callable` under the fake OCP harness, because staticmethod objects are callable only from 3.10.
  - The plan did not cause it. None of the test, the harness or the helper changed in 75-07, and at the plan base 3bc583a the test fails the same way with 3.9.6.
  - **Second run: rc 0, 31 packages ok** (internal/convert 300.7 s). It ran at f7bf043 with python 3.14 first on PATH, which is closer to the production image's 3.11. I am recording that run as the plan's green, under that stated condition.
- **State:**
  - STATE.md, ROADMAP.md and config.json were not staged.
  - The compose stack was never started.
  - My image tag `engine75-cr7` is removed, and no `engine75` image remains.
  - The VM has 10.0 GiB free.
- **Ledger:** I did not append to the broken-windows ledger. No `gsd_run` resolver was in this shell, and the ledger is optional.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register:
- **T-75-24:** only Go-validated SVG is returned or rasterized; tested with a script element in each SVG.
- **T-75-25:** cross-checks on texts, counts, substitution and page.
- **T-75-26:** fixed rsvg argv, with `--unlimited` absent.
- **T-75-27:** output and sidecar removal on every error path.
- **T-75-SC:** no package was added.

No endpoint or registration exists yet; 75-09 exposes the converters.

## Self-Check: PASSED

- Every created and modified file listed above exists.
- d090495, 26c3dbd and f7bf043 are ancestors of HEAD and show signature G.
- The measured count since 3bc583a is 3.
