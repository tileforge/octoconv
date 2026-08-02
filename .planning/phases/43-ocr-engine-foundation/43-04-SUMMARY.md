---
phase: 43-ocr-engine-foundation
plan: 04
subsystem: convert
tags: [pdftoppm, pdfinfo, tesseract, ocr, streaming, memory-safety, progress, encoding-xml, go]

# Dependency graph
requires:
  - phase: 43-ocr-engine-foundation
    provides: "OCROpts closed language allowlist (43-01); PDF page-count/page-box/image-pixel/born-digital guards (43-02); OCRConverter Pairs/Engine/Convert (image path) + the named ocrPageMerger encoding/xml merge helper (43-03)"
provides:
  - "Streaming per-page PDF OCR path (OCR-02, SC2): pdfinfo page-count cap + born-digital check BEFORE any render, then per page: page-box bound -> pdftoppm -f N -l N -r 300 -> tesseract -> delete page image + raw output before the next iteration -- never a whole-PDF rasterize"
  - "Multi-page hOCR/ALTO merge via the Plan 03 ocrPageMerger (encoding/xml, never string concat), proven on adversarial &/</> content by a hermetic round-trip test across BOTH targets"
  - "OCRConverter.ConvertWithProgress (OCR-04): the 2nd ProgressConverter implementor -- page-granular sink.Update ramp for a PDF source, ZERO Update calls for a single-image source"
  - "ocrPageProgress: a new, page-shaped percent+ETA estimator with zero coupling to progress.go's ffmpeg tick parser"
affects: [44-ocr-worker-integration]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared prepare-step factoring (ocrPrepareConversion) so Convert and ConvertWithProgress can never diverge on the same accept/reject decision -- mirrors av.go's avPrepareConversion"
    - "Per-page pdfinfo probe (-f N -l N) parses a DIFFERENT pdfinfo output shape (\"Page    N size:\") than the whole-document probe (\"Page size:\") -- a new, separate parser (parsePdfinfoPerPagePageSize) rather than reusing Plan 02's whole-document parser"
    - "-singlefile on pdftoppm's per-page render -> deterministic \"<outBase>.png\" naming, no page-number-suffix guessing"
    - "defer os.Remove inside a per-page helper function -- called synchronously once per loop iteration -- guarantees the rendered page image and tesseract's raw output are deleted on EVERY return path (success or error) before the next page starts"
    - "PATH-restriction test technique (restrictedPATH helper): symlink only the binaries a guard stage is allowed to invoke into a temp dir, t.Setenv PATH to it, so a test can PROVE (not just assert) that a later stage's binary was never invoked -- any attempt would fail with a distinct 'executable not found' error instead of the expected sentinel"

key-files:
  created:
    - internal/convert/ocrprogress.go
    - internal/convert/ocrprogress_test.go
    - internal/convert/ocrpdf.go
    - internal/convert/ocrpdf_test.go
    - internal/convert/testdata/ocr/multipage_text.pdf
  modified:
    - internal/convert/ocr.go

key-decisions:
  - "ocr.go's Convert() PDF branch and its opts/target preamble were both edited (beyond this plan's own files_modified frontmatter list, which named only ocrprogress.go/ocrpdf.go) because Task 2's <action> explicitly instructs 'wire the dispatch in ocr.go if not already present' and Task 3 explicitly instructs factoring a shared prepare step Convert must also use -- both edits are surgical (the PDF dispatch one-liner, and swapping inline OCROptsFromMap+target-check for a call to the new shared ocrPrepareConversion), the image path body is untouched, and every scope-fence grep (progress.go/converters.go/exec.go unchanged, no worker/api/queue/db/cmd edits) still holds"
  - "-singlefile added to pdftoppm's per-page render argv (not explicitly named in the plan) so the rendered page's output filename is deterministic (\"<outBase>.png\") rather than requiring the caller to parse pdftoppm's own zero-padded page-number-suffix naming convention -- live-verified against the real binary"
  - "A NEW per-page pdfinfo output parser (parsePdfinfoPerPagePageSize) was required: pdfinfo -f N -l N changes the 'Page size:' line's shape to 'Page    N size:  W x H pts' (page number embedded), which Plan 02's parsePdfinfoPageSize (built for the whole-document 'Page size:' line, no embedded number) does not match -- live-verified against the real binary before writing the parser, not assumed"
  - "TestOCRConvertPDF/TestOCRConvertPDFHOCRMerged needed a NEW fixture (testdata/ocr/multipage_text.pdf, 2 real distinguishable text pages 'PAGE ONE'/'PAGE TWO', 0 pdftotext-extractable chars) because the existing 43-02 multipage_image_only.pdf fixture is blank/text-free (live-verified: tesseract recognizes zero characters on any of its 3 pages) -- reusing it would have left 'concatenated txt spans all pages' unfalsifiable"
  - "PATH-restriction (restrictedPATH) rather than a mock/stub is how TestOCRPDFPageCap/PageBox/BornDigital PROVE pdftoppm/tesseract were never invoked before the fail-closed guards fire -- a plain 'assert the right error' test would pass even if the code accidentally rendered first, since the guard's own sentinel would still surface eventually; restricting PATH makes any accidental pre-guard exec fail loudly with a DIFFERENT, distinguishable error"
  - "ocrPageProgress's ETA uses a plain running average (elapsedTotal/pagesDone), deliberately NOT an EMA like progress.go's ffmpeg etaEstimator -- a page completion is a much lower-frequency, much less noisy signal than an ffmpeg -progress tick, so a simple average is the right-sized estimator and avoids any risk of the two estimators' code converging by coincidence"

patterns-established:
  - "ocrPrepareConversion is now the FIXED shared preamble both OCRConverter.Convert and OCRConverter.ConvertWithProgress call -- any future OCR entry point must call it too, never reimplement OCROptsFromMap+target-check inline"
  - "convertPDF's onPage callback signature (func(page, total int, elapsed time.Duration)) is the fixed seam between the streaming per-page loop and any progress-reporting caller -- Phase 44 (if it ever needs a second progress consumer) should reuse this callback, not add a second loop variant"

requirements-completed: [OCR-02, OCR-04]

# Metrics
duration: 55min
completed: 2026-07-26
---

# Phase 43 Plan 04: Streaming PDF Path + Page-Granular Progress Summary

**A scanned/image-only PDF is OCR'd exactly one page at a time (pdfinfo page-count cap + born-digital check before any render, then per-page page-box bound -> pdftoppm -> tesseract -> delete), multi-page hOCR/ALTO merges into one valid-XML document via the Plan 03 encoding/xml merger, and OCRConverter becomes the second `ProgressConverter` implementor with a page-granular percent+ETA ramp -- closing Phase 43's OCR-02 and OCR-04.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-07-26T~12:05:00Z (approx, from prior commit timestamps)
- **Completed:** 2026-07-26T13:01:00Z
- **Tasks:** 3 completed (Task 1 strict TDD RED->GREEN; Tasks 2-3 `auto`)
- **Files modified:** 6 (5 created, 1 modified)

## Accomplishments

- **Streaming per-page PDF path (OCR-02, SC2, the memory-bomb-safety load-bearing criterion):** `ocrPDFGuardStage` runs the pdfinfo page-count cap (`enforceMaxPages`) and the pdftotext born-digital detector (`detectBornDigitalPDF`, both from Plan 02) BEFORE any render. The per-page loop (`ocrPDFPage`, called once per page 1..N by `convertPDF`) then, for EACH page: probes that page's own declared physical page-box via a new per-page `pdfinfo -f N -l N` probe and bounds it with Plan 02's `enforcePageBoxBound` (defense-in-depth over the page-count cap alone, T-43-01b) BEFORE invoking `pdftoppm -r 300 -f N -l N -singlefile -png`, OCRs the single rendered page via Plan 03's tesseract argv builder, and deletes BOTH the rendered page image and tesseract's own raw output file (via `defer os.Remove`, guaranteed on every return path) before the loop's next iteration. The whole PDF is never rasterized up front; at most one page's image is ever on disk.
- **A new per-page pdfinfo parser was required and live-verified:** `pdfinfo -f N -l N` changes the "Page size:" line's shape to `Page    N size:  W x H pts` (page number embedded) -- distinct from Plan 02's whole-document `Page size:  W x H pts` line. `parsePdfinfoPerPagePageSize` (ocrpdf.go) is a new, separate, fail-closed parser for this shape; Plan 02's `parsePdfinfoPageSize` is untouched.
- **Multi-page hOCR/ALTO merge (BLOCKER-2-class fix):** for hocr/alto targets, `convertPDF` feeds each page's raw tesseract fragment into Plan 03's named `ocrPageMerger` (`AddPage` per page, `Finalize` once at the end) -- never string concatenation. A hermetic `TestOCRMergePagesRoundTrip` merges >=2 synthetic per-page fragments (adversarial `&`/`<`/`>` content) for BOTH `hocr` and `alto` targets and asserts the merged document parses via a full `xml.Decoder` token walk. Real-binary `TestOCRConvertPDFHOCRMerged` OCRs a real 2-page PDF to hocr and confirms exactly 2 merged `ocr_page` elements.
- **Page-granular progress (OCR-04):** `ocrprogress.go`'s `ocrPageProgress` (`pctForPage` percent helper + `recordPage`/`etaSeconds` running-average ETA) is a NEW, page-shaped estimator with zero coupling to progress.go's ffmpeg tick-based parser/EMA (verified by grep: 0 matches for `progressBlockParser|etaEstimator|computeProgressPct|out_time|speed`). `OCRConverter.ConvertWithProgress` (the 2nd `ProgressConverter` implementor after `AVConverter`) drives this estimator via `convertPDF`'s `onPage` callback for a PDF source, and delegates straight to `Convert` (zero `sink.Update`/`SourceDuration` calls) for a single-image source.
- **Convert and ConvertWithProgress share ONE preamble** (`ocrPrepareConversion`, mirroring `av.go`'s `avPrepareConversion`) so the two entry points can never drift into a different accept/reject decision on the same bytes -- proven by `TestOCRPDFPageCapWithProgress` asserting the identical `ErrOCRTooManyPages` rejection (with zero `Update` calls) that plain `Convert` already produces (`TestOCRPDFPageCap`).
- **A new real fixture, `testdata/ocr/multipage_text.pdf`** (2 pages, "PAGE ONE"/"PAGE TWO", built via ImageMagick + verified 0 pdftotext-extractable characters), was needed because the existing `multipage_image_only.pdf` fixture (43-02) is blank/text-free on every page (live-verified: tesseract recognizes 0 characters on any of its 3 pages) -- reusing it would have made "concatenated txt spans all pages" an unfalsifiable assertion.
- **PATH-restriction proves guard ordering, not just asserts it:** `restrictedPATH` symlinks ONLY the binaries a guard stage is allowed to invoke (e.g. `pdfinfo`+`pdftotext`, deliberately excluding `pdftoppm`/`tesseract`) into a temp dir and points `PATH` at it for the test -- if the code under test ever tried to exec a binary outside that restricted set, it would fail with a DISTINCT "executable not found" error rather than the guard's own expected sentinel, so passing assertions on the expected sentinel are real proof of invocation order (`TestOCRPDFPageCap`, `TestOCRPDFPageBox`, `TestOCRPDFBornDigital`, `TestOCRPDFPageCapWithProgress`).
- All real-binary tests actually ran (poppler 26.07.0 + tesseract 5.5.2 installed locally), not just skip-stubbed; every one is still gated behind `exec.LookPath`+`t.Skip` so `go test -short`/any CI without the binaries stays hermetic.

## Task Commits

1. **Task 1: Page-based progress estimator** (`tdd="true"`, strict RED->GREEN)
   - `a9ce28b` (test) - RED: `pctForPage`/`ocrPageProgress` undefined, compile failure
   - `bbcc821` (feat) - GREEN: `ocrprogress.go` implementation, all cases pass, 0 forbidden-token grep matches
2. **Task 2: Streaming PDF Convert path** - `e922483` (feat): `ocrpdf.go` (guard stage, per-page page-box bound, pdftoppm/tesseract/delete loop, XML merge), `ocr.go` PDF-dispatch wiring, new `multipage_text.pdf` fixture, hermetic + PATH-restricted + real-binary tests
3. **Task 3: ConvertWithProgress** - `ec84100` (feat): `var _ ProgressConverter = OCRConverter{}`, shared `ocrPrepareConversion` preamble (also adopted by `Convert`), page-granular ramp via `convertPDF`'s `onPage` callback, zero-updates single-image path, tests

**Plan metadata:** (this commit) `docs(43-04): complete plan`

## TDD Gate Compliance

Task 1's RED (`a9ce28b`) -> GREEN (`bbcc821`) gate sequence is present and verified in `git log` order. No fail-fast violation: RED failed at compile time (undefined `pctForPage`/`newOCRPageProgress`), a genuine RED, not a passing-test-in-disguise.

## Files Created/Modified

- `internal/convert/ocrprogress.go` (90 lines) - `pctForPage`, `ocrPageProgress`/`newOCRPageProgress`/`recordPage`/`etaSeconds` -- new, page-shaped percent+ETA estimator
- `internal/convert/ocrprogress_test.go` (97 lines) - percent/clamp/null-denominator/monotonic/ETA-nil-then-derived/final-page-zero-ETA cases
- `internal/convert/ocrpdf.go` (315 lines after Task 3) - `ocrRasterizeDPI`, `ocrRasterizePageArgs`, `ocrPDFPageSizeArgs`/`parsePdfinfoPerPagePageSize`/`ocrPDFPageSizeForPage` (new per-page pdfinfo parser), `ocrPDFGuardStage`, `ocrPDFPage`, `convertPDF`, `var _ ProgressConverter`, `ocrPrepareConversion`, `ConvertWithProgress`
- `internal/convert/ocrpdf_test.go` (~360 lines) - `requireOCRPDFBinaries`/`restrictedPATH` helpers, `TestOCRRasterizePageArgs`, `TestOCRMergePagesRoundTrip` (hocr+alto subtests), `TestOCRPDFPageCap`/`PageBox`/`BornDigital` (PATH-restricted ordering proof), `TestOCRConvertPDF`, `TestOCRConvertPDFHOCRMerged`, `fakeOCRProgressSink`, `TestOCRConvertWithProgress`, `TestOCRPDFPageCapWithProgress`, `TestOCRSingleImageNullProgress`
- `internal/convert/testdata/ocr/multipage_text.pdf` - new 2-page image-only, text-bearing ("PAGE ONE"/"PAGE TWO") PDF fixture
- `internal/convert/ocr.go` - PDF-source dispatch now calls `convertPDF` (was a stub error); `Convert`'s preamble now calls the new shared `ocrPrepareConversion` instead of inlining `OCROptsFromMap`+target-check

## Decisions Made

See `key-decisions` in frontmatter above (ocr.go edit scope, `-singlefile`, new per-page pdfinfo parser, new fixture, PATH-restriction test technique, running-average ETA).

## Deviations from Plan

**1. [Plan-gap clarification] `ocr.go` edited despite not being in this plan's `files_modified` frontmatter list**
- **Found during:** Task 2 (PDF dispatch wiring) and Task 3 (shared preamble)
- **Issue:** The plan's frontmatter `files_modified` names only `ocrprogress.go`/`ocrprogress_test.go`/`ocrpdf.go`/`ocrpdf_test.go`, but Task 2's `<action>` explicitly says "wire the dispatch in ocr.go if not already present, keeping ocr.go's image path untouched," and Task 3's `<action>` explicitly says to factor a shared prepare step "mirroring av's avPrepareConversion" that both `Convert` and `ConvertWithProgress` call.
- **Fix:** Edited exactly two spots in `ocr.go`: the PDF-source branch (one line, from a stub error to `c.convertPDF(...)`) and `Convert`'s own preamble (swapped inline `OCROptsFromMap`+target-check for a call to the new `ocrPrepareConversion`, defined in `ocrpdf.go`). The image-path body (tesseract invocation, rename, `validateOCROutput`) is byte-for-byte untouched.
- **Files modified:** `internal/convert/ocr.go`
- **Commits:** `e922483`, `ec84100`

## Issues Encountered

- `pdfinfo -f N -l N`'s "Page size:" line grows a page-number field (`Page    N size:`) compared to the whole-document form Plan 02's parser expects (`Page size:`) -- discovered by running the real binary before writing code, not assumed from documentation. Required a new, separate parser (`parsePdfinfoPerPagePageSize`) rather than reusing Plan 02's `parsePdfinfoPageSize`.
- The existing `multipage_image_only.pdf` fixture (43-02) turned out to be blank/text-free on every page when actually OCR'd (live-verified via direct `pdftoppm`+`tesseract` before writing tests) -- unsuitable for proving "concatenated txt spans all pages." A new fixture with real distinguishable per-page text was built instead.

## Known Stubs

None -- every function in `ocrprogress.go`/`ocrpdf.go` is a complete, real implementation validated against the actual tesseract/poppler binaries, not a placeholder. `OCRConverter` remains intentionally NOT registered into `convert.Default` (Phase 44's job, per the phase's own scope fence) -- documented scope boundary, not a stub.

## Threat Flags

None beyond the plan's own `<threat_model>` (T-43-01, T-43-01b, T-43-05m, T-43-06, T-43-04c, T-43-07) -- no new trust-boundary surface: `pdftoppm`/`tesseract` are invoked exclusively through the existing hardened `runCommand`, the per-page page-box guard runs before every render, and the merged multi-page hOCR/ALTO document is proven valid XML on adversarial content by the hermetic round-trip test.

## User Setup Required

None - no external service configuration required. (poppler `pdftoppm`/`pdfinfo`/`pdftotext` and `tesseract` are local dev-machine binary dependencies already documented as required in 43-CONTEXT.md/STACK.md; container packaging is Phase 44/45's concern.)

## Next Phase Readiness

- Phase 43 (OCR Engine Foundation) is now COMPLETE: all four plans (43-01 OCROpts, 43-02 guards, 43-03 image converter core, 43-04 this plan's streaming PDF path + progress) landed, all four requirements (OCR-01, OCR-02, OCR-03, OCR-04) are marked complete in REQUIREMENTS.md.
- `OCRConverter` (Pairs/Engine/Convert/ConvertWithProgress) is a complete, standalone, unregistered converter ready for Phase 44 to wire into `convert.Default`, the worker, and the queue -- exactly mirroring the Audio/AV phase-boundary precedent.
- Scope fence held: `internal/convert/converters.go`, `internal/convert/exec.go`, and `internal/convert/progress.go` are all byte-for-byte unchanged (`git diff --exit-code` clean); no edits under `internal/worker`, `internal/api`, `internal/queue`, `internal/db`, or `cmd/*`.
- No blockers.

---
*Phase: 43-ocr-engine-foundation*
*Completed: 2026-07-26*

## Self-Check: PASSED

- FOUND: internal/convert/ocrprogress.go
- FOUND: internal/convert/ocrprogress_test.go
- FOUND: internal/convert/ocrpdf.go
- FOUND: internal/convert/ocrpdf_test.go
- FOUND: internal/convert/testdata/ocr/multipage_text.pdf
- FOUND: .planning/phases/43-ocr-engine-foundation/43-04-SUMMARY.md
- FOUND commit: a9ce28b (test, Task 1 RED)
- FOUND commit: bbcc821 (feat, Task 1 GREEN)
- FOUND commit: e922483 (feat, Task 2)
- FOUND commit: ec84100 (feat, Task 3)
