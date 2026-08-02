---
phase: 43-ocr-engine-foundation
plan: 02
subsystem: convert
tags: [pdfinfo, pdftotext, poppler, ocr, guard, decompression-bomb, born-digital]

# Dependency graph
requires:
  - phase: 43-ocr-engine-foundation
    provides: "OCROpts closed language allowlist + injection-safety pattern (43-01)"
provides:
  - "PDF page-count cap (pdfinfo) fail-closed before any render"
  - "Per-page page-box physical-size guard (pdfinfo Page size -> pixels@300dpi) fail-closed before that page renders"
  - "Image declared-pixel-dimension cap reusing dimensions.go's bound"
  - "Born-digital PDF detection via pdftotext, conservative reject-on-any-substantial-text"
  - "testdata/ocr/ fixtures: multipage_image_only.pdf, born_digital.pdf, text_sample.png"
affects: [44-ocr-worker-integration, 43-03-ocr-converter, 43-04-pdf-page-loop]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "pdfinfo/pdftotext invoked through the existing hardened runCommand (Setpgid+SIGKILL+timeout), same as ffmpeg/ffprobe"
    - "guard-before-expensive-subprocess: page-count/page-box/pixel/born-digital checks all run BEFORE any pdftoppm render or image decode"
    - "zero-value-means-uncapped-this-phase convention (mirrors av.go's avMaxSourceDuration/avMaxSourceResolutionHeight), plain params, env wiring deferred to Phase 45"
    - "fail-closed parse: parsePdfinfoPages/parsePdfinfoPageSize never silently accept a missing/malformed line"

key-files:
  created:
    - internal/convert/ocrguards.go
    - internal/convert/ocrguards_test.go
    - internal/convert/testdata/ocr/multipage_image_only.pdf
    - internal/convert/testdata/ocr/born_digital.pdf
    - internal/convert/testdata/ocr/text_sample.png
  modified: []

key-decisions:
  - "ocrBornDigitalCharThreshold = 16 non-whitespace chars (Claude's Discretion per D-03): small enough that any substantial real text layer trips reject, tolerant of pdftotext formatting noise on genuinely image-only PDFs (verified: 0 chars on the committed image-only fixture)"
  - "max/maxPixels == 0 means 'no cap configured this phase' across all four guards (enforceMaxPages, enforcePageBoxBound, enforceImagePixelBound) -- mirrors av.go's plain-param-this-phase convention; OCR_MAX_PAGES/OCR_MAX_PIXELS env wiring is Phase 45"
  - "Page-box guard reuses pdfinfo's whole-document 'Page size' line (not a per-page -f N -l N probe) this phase -- sufficient for the guard's pure parse+threshold contract; Plan 04's per-page loop may call a per-page variant using the same enforcePageBoxBound function"

patterns-established:
  - "Third external-process trust boundary (pdfinfo/pdftotext) routed through runCommand identically to ffmpeg/ffprobe/tesseract's future call sites"
  - "Distinct sentinel per guard (ErrOCRTooManyPages/ErrOCRPageBoxTooLarge/ErrOCRImageTooLarge/ErrOCRBornDigital) for Phase 44's 422 mapping, mirroring ErrAVResolutionExceeded/ErrAVReencodeResolutionExceeded's shape"

requirements-completed: [OCRE-02, OCRE-03]

# Metrics
duration: 20min
completed: 2026-07-26
---

# Phase 43 Plan 02: OCR Guard Layer Summary

**PDF page-count cap + per-page page-box physical-size guard + image pixel cap + born-digital pdftotext detection, all fail-closed pre-render, in `internal/convert/ocrguards.go`**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-26T15:09:00+03:00 (approx)
- **Completed:** 2026-07-26T15:16:00+03:00
- **Tasks:** 3 completed
- **Files modified:** 5 (2 Go files + 3 committed testdata fixtures)

## Accomplishments
- PDF page-count cap: `ocrPDFPageCount` (pdfinfo, hardened `runCommand`) + `enforceMaxPages` reject a declared page count over the (plain-param) cap before any `pdftoppm` render, distinct `ErrOCRTooManyPages` sentinel.
- Per-page page-box physical-size guard: `parsePdfinfoPageSize`/`enforcePageBoxBound` compute rasterized pixels at 300 DPI from pdfinfo's declared `Page size` (points) and reject a degenerate huge single-page box with `ErrOCRPageBoxTooLarge` — closes the axis the page-count cap alone misses.
- Image pixel-dimension cap: `enforceImagePixelBound` reuses `dimensions.go`'s `Dimensions`/`HasDimensionLimit` verbatim (no new parser), rejecting oversized declared dimensions with `ErrOCRImageTooLarge` before decode; a `Dimensions` failure (`ErrDimensionsUnknown`) is itself a fail-closed rejection.
- Born-digital PDF detection: `detectBornDigitalPDF` (pdftotext, hardened `runCommand`) counts non-whitespace extracted characters and rejects with `ErrOCRBornDigital` above a small conservative threshold (`ocrBornDigitalCharThreshold = 16`), steering the client to the document engine — runs before any render.
- Real-binary tests actually ran (not skipped) since poppler (`pdfinfo`/`pdftotext` 26.07.0) is installed locally; all still gated behind `exec.LookPath` + `t.Skip` so `go test -short`/CI-without-poppler stays hermetic.
- Committed three small testdata fixtures under `internal/convert/testdata/ocr/`: a 3-page image-only PDF (built via ImageMagick, 300x200pt pages, 0 extractable chars per pdftotext), a born-digital PDF (built via a hand-authored PostScript + `ps2pdf`, 82 extractable non-whitespace chars), and a text-bearing PNG (400x120, tesseract round-trips it to "Hello OctoConv OCR" — reusable by Plan 03's accuracy test).

## Task Commits

Each task was committed atomically, strict RED->GREEN TDD per task:

1. **Task 1: PDF page-count cap + per-page page-box physical-size cap** - `e6b5c74` (test) -> `81c670a` (feat)
2. **Task 2: Image pixel-dimension cap** - `8457bd9` (test) -> `0dad327` (feat)
3. **Task 3: Born-digital PDF detection** - `77b4a21` (test) -> `c1a4072` (feat)

**Plan metadata:** (this commit) `docs(43-02): complete plan`

## Files Created/Modified
- `internal/convert/ocrguards.go` - page-count cap, page-box size guard, image pixel cap, born-digital detection (239 lines)
- `internal/convert/ocrguards_test.go` - pure parse/threshold tests + real-binary skip-if-absent tests
- `internal/convert/testdata/ocr/multipage_image_only.pdf` - 3-page image-only PDF fixture (page-count cap + born-digital-pass tests)
- `internal/convert/testdata/ocr/born_digital.pdf` - PDF with a real text layer (born-digital-reject test)
- `internal/convert/testdata/ocr/text_sample.png` - small text-bearing PNG (image pixel cap test; reusable by Plan 03)

## Decisions Made
- `ocrBornDigitalCharThreshold = 16`: conservative per D-03 ("reject on ANY substantial extractable text"), documented in the const comment, live-verified against both fixtures (0 chars on image-only, 82 on born-digital).
- Zero-value-means-uncapped-this-phase convention applied uniformly across `enforceMaxPages`/`enforcePageBoxBound`/`enforceImagePixelBound`, mirroring `av.go`'s existing `avMaxSourceDuration`/`avMaxSourceResolutionHeight` pattern — keeps this plan's guards provably a no-op ceiling change until Phase 45 wires real env-configured caps.
- Page-box guard reads pdfinfo's document-level `Page size` line (all committed test PDFs are single-page-size documents); the parse+threshold functions (`parsePdfinfoPageSize`/`enforcePageBoxBound`) are pure and reusable by Plan 04's per-page loop against a per-page `pdfinfo -f N -l N` probe if a future PDF has mixed page sizes — no rework needed, just a different caller.

## Deviations from Plan

None - plan executed exactly as written. All four guard behaviors, both real-binary skip-if-absent gates, and the testdata fixture set match the plan's `<action>`/`<behavior>` spec verbatim.

## Issues Encountered
None.

## Known Stubs
None — all four guard functions are complete, fail-closed implementations, not placeholders. (Env-var wiring for the caps is explicitly out of scope this phase per the plan/context, deferred to Phase 45 — not a stub, a documented scope boundary.)

## Threat Flags

None beyond what the plan's own `<threat_model>` already covers (T-43-01, T-43-01b, T-43-02, T-43-06, T-43-04a) — no new trust-boundary surface was introduced beyond pdfinfo/pdftotext invocation, both routed through the existing hardened `runCommand`.

## User Setup Required

None - no external service configuration required. (poppler `pdfinfo`/`pdftotext` is a local dev-machine binary dependency already documented as required in 43-CONTEXT.md/STACK.md; container packaging is a later phase's concern.)

## Next Phase Readiness
- All four sentinels (`ErrOCRTooManyPages`, `ErrOCRPageBoxTooLarge`, `ErrOCRImageTooLarge`, `ErrOCRBornDigital`) are ready for Plan 03's converter and Plan 04's PDF page loop to call directly, and for Phase 44 to map to 422 responses.
- `internal/convert/converters.go` untouched (verified via `git diff --exit-code`); zero new Go dependencies; `internal/convert/testdata/ocr/` fixtures are ready for reuse by Plan 03's accuracy tests.
- No blockers.

---
*Phase: 43-ocr-engine-foundation*
*Completed: 2026-07-26*

## Self-Check: PASSED

All created files verified present on disk; all 6 task commit hashes (e6b5c74, 81c670a, 8457bd9, 0dad327, 77b4a21, c1a4072) verified present in `git log`.
