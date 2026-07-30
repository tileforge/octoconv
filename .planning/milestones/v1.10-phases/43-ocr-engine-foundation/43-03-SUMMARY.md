---
phase: 43-ocr-engine-foundation
plan: 03
subsystem: convert
tags: [tesseract, ocr, hocr, alto, encoding-xml, psm-oem, exec-hardening, go]

# Dependency graph
requires:
  - phase: 43-ocr-engine-foundation
    provides: "OCROpts closed language allowlist + injection-safety pattern (43-01); PDF page-count/page-box/image-pixel/born-digital guards (43-02)"
provides:
  - "OCRConverter{}: Pairs() advertising the disjoint {jpg,png,tiff,webp,pdf} x {txt,hocr,alto} cross-product (D-01 single-target output), Engine() -> EngineOCR"
  - "OCRConverter.Convert: real tesseract image->txt/hocr/alto at pinned PSM 3 / OEM 1 (D-04), enforceImagePixelBound guard called BEFORE tesseract, empty-text-image completes as success (OCRE-05)"
  - "ocrPageMerger (newOCRPageMerger/AddPage/Finalize): the named encoding/xml-based multi-page hOCR/ALTO merge helper Plan 04's streaming PDF loop will call"
  - "testdata/ocr/adversarial_text.png, testdata/ocr/blank_image.png fixtures"
affects: [43-04-pdf-page-loop, 44-ocr-worker-wiring]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Standalone-converter shape (Pairs/Engine/Convert, guard-before-subprocess, argv builders as pure functions) cloned from AudioConverter/AVConverter for a sixth engine class"
    - "encoding/xml generic-node capture (XMLName + `,any,attr` + `,innerxml`) to re-emit an already-escaped XML fragment without re-parsing/re-escaping its content -- the pattern any future multi-fragment XML merge in this codebase should reuse"
    - "Engine's own output-format configfile is the escaping boundary for a SINGLE page (tesseract's hocr/alto writer); a NAMED Go-side merger (ocrPageMerger) is the escaping boundary for MULTIPLE pages -- two different mechanisms for the same guarantee, deliberately not conflated"

key-files:
  created:
    - internal/convert/ocr.go
    - internal/convert/ocr_test.go
    - internal/convert/testdata/ocr/adversarial_text.png
    - internal/convert/testdata/ocr/blank_image.png
  modified:
    - internal/convert/convert.go

key-decisions:
  - "webp is passed straight to tesseract with no decode-to-png pre-step: STACK.md flagged Leptonica's webp support as build-dependent, but live-verified against this environment's tesseract 5.5.2 (Leptonica 1.87.0 + libwebp 1.6.0) that a real webp file OCRs correctly with zero pre-processing -- documented as a re-verify-against-the-deployed-container's-Leptonica-build item for Phase 45/46, not built as a mandatory day-one normalize stage"
  - "tesseract's own ALTO writer names its output outputbase.xml, not outputbase.alto (live-verified) -- Convert renames the produced file to the caller's actual outPath rather than assuming tesseract's own naming convention matches the job's target-format extension, mirroring whisperArgs'/transcodeToMP4Args' explicit output-path-pinning discipline"
  - "A raw xml.Decoder token walk alone does NOT enforce 'exactly one root element' (live-verified: a naive concatenation of two full XML documents still tokenizes without error) -- the round-trip test therefore also asserts exactly one XML declaration and one root element in the merged document, which is what actually distinguishes a real merge from Pitfall 5's naive-concat failure mode"
  - "PDF source dispatch in Convert() returns a clear 'not yet implemented (see 43-04-PLAN.md)' error rather than silently no-op'ing or attempting a naive whole-PDF path -- Pairs() advertises pdf per D-01's truths, but the streaming per-page render/OCR/delete loop is Plan 04's addition to this same Convert() dispatch"
  - "Genuine RED/GREEN TDD applied to the merger (Task 3, tdd=\"true\"): the RED commit shipped a deliberately-naive concatenating stub (reproducing Pitfall 5 exactly -- 2 XML declarations, 2 root elements, no page renumbering) so the round-trip test's declaration/root-element-count assertions provably fail before the encoding/xml GREEN implementation lands"

patterns-established:
  - "ocrPageMerger's named contract (type ocrPageMerger; newOCRPageMerger(target); AddPage(pageNum, fragment) error; Finalize() ([]byte, error)) is now fixed -- Plan 04 must import and call these exact symbols, never redefine an equivalent"

requirements-completed: [OCR-01, OCR-03]

# Metrics
duration: 45min
completed: 2026-07-26
---

# Phase 43 Plan 03: OCRConverter Core (image->txt/hOCR/ALTO) Summary

**OCRConverter (Pairs/Engine/Convert) extracts text from images via tesseract at pinned PSM 3/OEM 1, emits hOCR/ALTO with word-level bounding boxes, and defines the named `ocrPageMerger` encoding/xml multi-page merge helper Plan 04 will reuse -- all validated against real tesseract 5.5.2 + poppler binaries, standalone and unregistered.**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-07-26T15:12:00+03:00 (approx.)
- **Completed:** 2026-07-26T15:57:00+03:00
- **Tasks:** 3 completed
- **Files modified:** 5 (4 created, 1 modified)

## Accomplishments
- `OCRConverter{}`: `Pairs()` advertises the disjoint `{jpg,png,tiff,webp,pdf} x {txt,hocr,alto}` cross-product (15 pairs, D-01 single-target output), `Engine()` returns the new `EngineOCR` const (the only edit to `convert.go`).
- `Convert()` runs tesseract directly against an image at pinned `--psm 3 --oem 1` (D-04, server constants, never per-job opts), resolving the `-l` language argv via Plan 01's `ocrLanguageArgs` with a server-side `eng` default, and calling Plan 02's `enforceImagePixelBound` guard BEFORE tesseract ever decodes the image (T-43-02).
- Empty/no-text images complete as SUCCESS with an empty output file (OCRE-05, PITFALLS.md Pitfall 9) -- `validateOCROutput` deliberately only checks the file exists, never checks size>0, live-verified against a committed blank-image fixture (tesseract exits 0, zero-byte `.txt`).
- hOCR and ALTO targets use tesseract's own output-format configfile (`hocr`/`alto`), which already escapes OCR'd text correctly into valid XML/XHTML -- live-verified with a real adversarial fixture whose recognized text is `A&B<C>D`, round-tripping as `&amp;`/`&lt;`/`&gt;` in valid, parseable XML. hOCR output carries word/line `bbox`/`x_wconf` title attributes.
- The named `ocrPageMerger` (`newOCRPageMerger`/`AddPage`/`Finalize`) assembles per-page hOCR/ALTO fragments into ONE multi-page document via `encoding/xml` -- a generic `ocrXMLNode` (XMLName + `,any,attr` + `,innerxml`) captures each fragment's page element without re-parsing/re-escaping its already-correct inner content, then `Finalize` wraps the accumulated nodes and serializes with `xml.MarshalIndent`. Proven via a hermetic round-trip test asserting exactly one XML declaration and one root element (the actual signal a naive-concat merge fails, since a bare `xml.Decoder` token walk alone tolerates multiple top-level elements).
- Real-tesseract tests (Pairs/Engine hermetic; argv-pinning hermetic; image convert, empty-input, hOCR/ALTO round-trip, hOCR bbox all real-binary, skip-if-absent) all ran against the locally installed tesseract 5.5.2 + poppler, not just skip-stubbed.

## Task Commits

Task 3 (`tdd="true"`) followed strict RED->GREEN; Tasks 1-2 were straightforward `feat` commits:

1. **Task 1: EngineOCR const + OCRConverter struct + Pairs()/Engine()** - `ffce0ea` (feat)
2. **Task 2: Convert() image->txt via tesseract (PSM 3/OEM 1) + empty-is-success + pixel guard** - `08fb68c` (feat)
3. **Task 3: hOCR/ALTO output + the shared ocrPageMerger XML merge helper**
   - `f535349` (test) - RED: naive-concat stub merger + round-trip tests (declaration/root-element-count assertions fail against the stub)
   - `ca213c4` (feat) - GREEN: real `encoding/xml` merger implementation, all tests pass

**Plan metadata:** (this commit) `docs(43-03): complete plan`

## TDD Gate Compliance

Task 3's RED (`f535349`) -> GREEN (`ca213c4`) gate sequence is present and verified in `git log` order. No fail-fast violation: the RED commit's round-trip test genuinely failed (declaration count 2, root-element count 2, page-renumbering assertions all failed) against the naive-concat stub before the GREEN commit landed.

## Files Created/Modified
- `internal/convert/convert.go` - added `EngineOCR = "ocr"` const (only edit; `git diff` shows nothing else changed)
- `internal/convert/ocr.go` (453 lines) - `OCRConverter` (Pairs/Engine/Convert), pinned `ocrPSM`/`ocrOEM`/`ocrDefaultLanguage` consts, `ocrConfigFileFor`/`ocrTesseractOutputExt`/`ocrResolvedLanguageArgs`/`ocrTesseractArgs` (pure argv/mapping helpers), `validateOCROutput`, and the named `ocrPageMerger`/`ocrXMLNode`/`ocrHOCRDoc`/`ocrALTODoc` merge machinery
- `internal/convert/ocr_test.go` (336 lines) - Pairs/Engine unit tests, argv-pinning unit tests, real-tesseract image-convert/empty-input/hOCR-ALTO-round-trip/hOCR-bbox tests (skip-if-absent), hermetic merger round-trip + no-page-element tests
- `internal/convert/testdata/ocr/adversarial_text.png` - text-bearing fixture ("A & B < C > D") for the escaping round-trip tests
- `internal/convert/testdata/ocr/blank_image.png` - blank white fixture for the empty-input-is-success test

## Decisions Made
See `key-decisions` in frontmatter above (webp direct-pass, ALTO output-extension rename, root-element-count assertion for the merger round-trip, PDF-source deferred-to-Plan-04 error, genuine RED/GREEN for the tdd="true" task).

## Deviations from Plan

None beyond the executor's own atomic-commit structuring (splitting the already-integrated implementation into 4 commits mapping 1:1 to the plan's 3 tasks plus Task 3's RED/GREEN split, per the sequential-execution protocol's "write code/tests -> commit" ordering) -- every `<action>`/`<behavior>`/acceptance-criteria item in `43-03-PLAN.md` was implemented as specified.

## Issues Encountered
- Go's `encoding/xml` `xml.Decoder.Token()` token walk does not itself enforce "exactly one root element" (confirmed by direct experiment: concatenating two complete XML documents still tokenizes end-to-end with no error). The plan's round-trip test needed strengthening beyond "parses without error" to also assert a single XML declaration and a single root element -- otherwise a naive-concat merge would have silently passed the round-trip test. This was caught before the GREEN commit by deliberately shipping the naive-concat RED stub first and confirming it failed the strengthened assertions.

## Known Stubs
None -- `OCRConverter`, `ocrPageMerger`, and every helper are complete, real implementations validated against the actual tesseract/poppler binaries, not placeholders. The PDF source path returns an explicit, documented "not yet implemented (see 43-04-PLAN.md)" error rather than a stub that could be mistaken for a working code path -- Pairs() advertises pdf per D-01, but Convert()'s pdf branch is intentionally out of this plan's scope.

## Threat Flags

None beyond the plan's own `<threat_model>` (T-43-02, T-43-04b, T-43-05, T-43-03c) -- no new trust-boundary surface: tesseract is invoked exclusively through the existing hardened `runCommand`, the pixel guard runs before every invocation, and the OCR'd-text-into-XML boundary is closed by tesseract's own writer (single page) plus the named `ocrPageMerger` (multi-page), both proven against adversarial `&`/`<`/`>`/`"` content.

## User Setup Required
None - no external service configuration required. (tesseract/poppler are local dev-machine binary dependencies already documented as required in 43-CONTEXT.md/STACK.md; container packaging is a later phase's concern.)

## Next Phase Readiness
- `OCRConverter.Convert`'s image path and the named `ocrPageMerger` (`newOCRPageMerger`/`AddPage`/`Finalize`) are ready for Plan 04's streaming PDF page loop (`pdfinfo` page-count cap -> per-page `pdftoppm` render -> tesseract -> delete before the next page) to extend this same `Convert()` dispatch and call the merger once per page.
- Scope fence held: `internal/convert/converters.go` untouched (`git diff --exit-code` clean, verified after every commit), `internal/convert/convert.go` carries only the `EngineOCR` const addition, no edits under `internal/worker`, `internal/api`, `internal/queue`, `internal/db`, or `cmd/*`. `OCRConverter` is not registered into `convert.Default` this phase.
- No blockers. `OCR-01` (image -> plain txt) and `OCR-03` (hOCR/ALTO with bbox/confidence, correct XML escaping) are the requirements this plan's own frontmatter assigns and completes for the IMAGE path. `OCR-02` (streaming PDF page loop) and `OCR-04` (page-granular progress) remain Pending -- they are Plan 04's job per `43-CONTEXT.md`.

---
*Phase: 43-ocr-engine-foundation*
*Completed: 2026-07-26*

## Self-Check: PASSED

- FOUND: internal/convert/ocr.go
- FOUND: internal/convert/ocr_test.go
- FOUND: internal/convert/testdata/ocr/adversarial_text.png
- FOUND: internal/convert/testdata/ocr/blank_image.png
- FOUND: .planning/phases/43-ocr-engine-foundation/43-03-SUMMARY.md
- FOUND commit: ffce0ea (feat, Task 1)
- FOUND commit: 08fb68c (feat, Task 2)
- FOUND commit: f535349 (test, Task 3 RED)
- FOUND commit: ca213c4 (feat, Task 3 GREEN)
