---
phase: 43-ocr-engine-foundation
verified: 2026-07-26T13:11:17Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
---

# Phase 43: OCR Engine Foundation Verification Report

**Phase Goal:** A standalone `OCRConverter` extracts plain text (txt) and word-coordinate output (hOCR/ALTO) from images and scanned/image-only PDFs via Tesseract, built and testable directly against the binary — memory-bomb-safe and born-digital-PDF-safe from the first line — before any queue/worker plumbing.
**Verified:** 2026-07-26T13:11:17Z
**Status:** passed
**Re-verification:** No — initial verification

## Environment

- `tesseract 5.5.2` (Leptonica 1.87.0, libwebp 1.6.0) at `/opt/homebrew/bin/tesseract`
- poppler `pdfinfo`/`pdftoppm`/`pdftotext` 26.07.0 at `/opt/homebrew/bin/*`
- Both installed locally → all real-binary tests in `internal/convert/*_test.go` ran for real (verified via `-v` output: zero `SKIP` lines for any OCR test), not stub-skipped.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SCOPE FENCE held — OCRConverter standalone, NOT registered; zero touch outside `internal/convert/` | ✓ VERIFIED | `grep OCRConverter internal/convert/converters.go` = 0 matches; `git diff --exit-code internal/convert/converters.go internal/convert/exec.go internal/convert/progress.go` clean; `git diff --stat da85af6..HEAD` shows the ONLY non-`.planning`/non-`internal/convert` change in the whole milestone-to-date is the 1-line `EngineOCR` const in `convert.go` (confirmed via `git show ffce0ea -- internal/convert/convert.go`); zero diff under `internal/worker`, `internal/api`, `internal/jobs`, `internal/queue`, `internal/db`, `cmd/*` |
| 2 | OCR-01: image (jpg/png/tiff/webp) → txt via real Tesseract at pinned PSM 3/OEM 1 (server constants) | ✓ VERIFIED | `internal/convert/ocr.go:93-96` (`ocrPSM="3"`, `ocrOEM="1"` consts, no client-facing PSM/OEM field); `TestOCRTesseractArgs` asserts discrete `--psm 3 --oem 1` argv; `TestOCRConvertImage` runs real tesseract against `testdata/ocr/text_sample.png` — PASS (0.10s, real binary, not skipped) |
| 3 | Image pixel cap enforced BEFORE tesseract decodes | ✓ VERIFIED | `internal/convert/ocr.go:237` calls `enforceImagePixelBound` immediately before the `runCommand(ctx,"tesseract",...)` call at line 243 (sequential Go code, no race); guard function unit-proven by `TestEnforceImagePixelBound_*` (within-bound/exceeds/unknown-dims-fail-closed/zero-uncapped/non-image-skip), all PASS |
| 4 | OCR-02: PDF path streams ONE page at a time (`pdftoppm -f N -l N -r 300`), never whole-PDF render | ✓ VERIFIED | `internal/convert/ocrpdf.go:49-58` `ocrRasterizePageArgs` pins `-f page -l page` to the SAME value (`TestOCRRasterizePageArgs` asserts this); `ocrPDFPage` (ocrpdf.go:149-178) renders/OCRs/deletes exactly one page per call via `defer os.Remove` on both the rendered PNG and tesseract's raw output; `TestOCRConvertPDF` (real binary, PASS 0.43s) proves concatenated txt spans BOTH pages of `multipage_text.pdf`; `TestOCRPDFPageCap`/`PageBox`/`BornDigital` use a novel PATH-restriction technique (`restrictedPATH`) that symlinks ONLY the guard-stage binaries so any pre-guard `pdftoppm`/`tesseract` exec would fail with a distinct error — all PASS, proving guard-before-render ordering, not just asserting the expected sentinel |
| 5 | OCR-03: single-target {txt,hocr,alto}, hOCR/ALTO via `encoding/xml` (never string concat); single-page AND merged multi-page round-trip on adversarial `&`/`<`/`>` | ✓ VERIFIED | `ocr.go:261-452` `ocrPageMerger`/`ocrXMLNode` uses stdlib `encoding/xml` exclusively (verified by reading — no `fmt.Sprintf`/string-concat XML assembly anywhere in the merge path); single-page: `TestOCRHOCRRoundTrip`/`TestOCRALTORoundTrip`/`TestOCRHOCRBBox` (real tesseract on `adversarial_text.png`, text "A & B < C > D") all PASS; merged multi-page: `TestOCRPageMergerRoundTrip` (hermetic) + `TestOCRMergePagesRoundTrip` (hocr+alto subtests) + `TestOCRConvertPDFHOCRMerged` (real binary, 2-page PDF, confirms exactly 2 merged `ocr_page` elements) all PASS; round-trip assertions go beyond "parses" to assert single XML declaration + single root element (the actual naive-concat failure signal per PITFALLS.md Pitfall 5) |
| 6 | OCR-04: `ConvertWithProgress` implements `ProgressConverter` (compile-time assertion), page-granular `sink.Update(pages_done*100/total)` + page-based ETA; single image → zero Updates; zero coupling to progress.go's ffmpeg parser | ✓ VERIFIED | `var _ ProgressConverter = OCRConverter{}` at `ocrpdf.go:252`; `pctForPage` (ocrprogress.go:22-33) computes exactly `done*100/total`, clamped; `TestOCRConvertWithProgress`/`TestOCRPDFPageCapWithProgress` (real binary) PASS; `TestOCRSingleImageNullProgress` asserts `len(sink.updates)==0` and `sink.durationCalls==0` for an image source — PASS; `grep -c 'progressBlockParser\|etaEstimator\|computeProgressPct\|out_time\|speed=' internal/convert/ocrprogress.go internal/convert/ocrpdf.go` = 0 |
| 7 | OCRO-01: `OCROpts` closed allowlist `eng`/`rus`/`eng+rus`, unlisted → error, injection test; `{"language":"osd"}` rejected; zero osd/orientation/PSM/OEM literal tokens in ocropts.go | ✓ VERIFIED | `ocropts.go:23-26` closed `ocrLanguageAllowlist{eng,rus}`; `TestParseOCROpts` subtests cover accept (eng/rus/eng+rus/rus+eng) + reject (chi_sim, partial-combo, **osd** — explicit "language osd rejected (OSD deferred to v2, D-05a)" subtest, PASS); `TestOCROptsInjection` (3 subtests, PASS) proves 5 shell-metacharacter/path-traversal payloads rejected via both `ParseOCROpts` and `OCROptsFromMap`, plus a structural argv-shape proof on `ocrLanguageArgs`; `grep -Eic 'osd\|orientation' ocropts.go` = 0, `grep -Ec 'PSM\|OEM\|psm\|oem' ocropts.go` = 0 |
| 8 | OCRE-02 (guard layer): page-count cap + per-page page-box size guard + image pixel cap — all fail-closed BEFORE render, distinct sentinels | ✓ VERIFIED (guard-logic scope; env wiring deferred to Phase 45 per documented decision) | `ocrguards.go` defines `ErrOCRTooManyPages`/`ErrOCRPageBoxTooLarge`/`ErrOCRImageTooLarge` (3 distinct sentinels) + `enforceMaxPages`/`enforcePageBoxBound`/`enforceImagePixelBound`, all pure fail-closed functions; `ocrpdf.go`'s `ocrPDFGuardStage` (page-count, before any render) and `ocrPDFPage` (per-page page-box, before that page's `pdftoppm`) call them in the correct order — PATH-restriction tests (`TestOCRPDFPageCap`, `TestOCRPDFPageBox`) PROVE the ordering, not just the sentinel. `MaxPages`/`MaxImagePixels` are `OCRConverter` struct fields defaulting to 0 = uncapped-this-phase (mirrors `av.go`'s documented convention) — env-var wiring (`OCR_MAX_PAGES`/cap) is explicitly Phase 45's job per 43-02-SUMMARY.md and 43-CONTEXT.md; REQUIREMENTS.md's traceability table correctly still shows OCRE-02 "Pending" (not yet fully closed) — this is accurate, not a doc bug: the guard MECHANISM is complete and tested, the operator-configured threshold is not yet wired to any env var or worker call site |
| 9 | OCRE-03 (guard layer): born-digital detect via pdftotext, conservative reject-on-any-substantial-text, distinct sentinel, real pdftotext test with born-digital + image-only fixtures | ✓ VERIFIED | `ErrOCRBornDigital` sentinel (ocrguards.go:203); `detectBornDigitalPDF` (ocrguards.go:230-239) runs pdftotext via hardened `runCommand`, rejects when non-whitespace char count > `ocrBornDigitalCharThreshold=16`; `TestDetectBornDigitalPDF_RealBinary` (real pdftotext) has two subtests — born-digital PDF rejected (82 chars), image-only PDF passes (0 chars) — both PASS |

**Score:** 9/9 truths verified (0 overrides; OCRE-02/03 verified at their documented phase-43 scope — guard logic complete, env-configured threshold correctly deferred to Phase 45, matching REQUIREMENTS.md's own "Pending" status which reflects that deferral honestly, not an inconsistency)

### Empty-input / hermeticity truth

| Truth | Status | Evidence |
|---|---|---|
| Empty/no-text image → success with empty txt (not error) | ✓ VERIFIED | `validateOCROutput` (ocr.go:185-190) deliberately never checks size>0; `TestOCREmptyInput` (real binary) runs tesseract on `blank_image.png` and asserts `Convert` returns nil + output file exists — PASS |
| Real-binary tests skip-if-absent (hermetic without binaries) | ✓ VERIFIED | Every real-binary OCR test is gated behind `exec.LookPath(...)` + `t.Skip(...)` (`ocr_test.go`, `ocrguards_test.go`, `ocrpdf_test.go`); no `testing.Short()` guard is used in the OCR files specifically (only `progressexec_test.go`, a pre-existing file, uses it) — binary-presence gating alone achieves hermeticity equally well under `-short` when binaries are absent; not a gap |

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/ocropts.go` | `OCROpts` closed allowlist | ✓ VERIFIED | 120 lines, substantive, no stubs; wired (imported by `ocr.go`/`ocrpdf.go` via `ocrLanguageArgs`/`OCROptsFromMap`) |
| `internal/convert/ocrguards.go` | 4 fail-closed guards | ✓ VERIFIED | 239 lines; wired (called from `ocr.go`'s image path and `ocrpdf.go`'s PDF path) |
| `internal/convert/ocr.go` | `OCRConverter` core (Pairs/Engine/Convert) + `ocrPageMerger` | ✓ VERIFIED | 452 lines; `var _ Converter = OCRConverter{}` compile assertion; real-binary tests pass |
| `internal/convert/ocrpdf.go` | streaming PDF path + `ConvertWithProgress` | ✓ VERIFIED | 315 lines; `var _ ProgressConverter = OCRConverter{}` compile assertion; real-binary tests pass |
| `internal/convert/ocrprogress.go` | page-based percent/ETA estimator | ✓ VERIFIED | 90 lines; zero coupling to progress.go internals (grep-proven) |
| `internal/convert/convert.go` | `EngineOCR` const only | ✓ VERIFIED | 1-line diff confirmed via `git show ffce0ea` |
| `internal/convert/converters.go` | UNCHANGED (not registered) | ✓ VERIFIED | `git diff --exit-code` clean; 0 matches for `OCRConverter` |
| `internal/convert/exec.go` | UNCHANGED | ✓ VERIFIED | `git diff --exit-code` clean |
| `internal/convert/progress.go` | UNCHANGED | ✓ VERIFIED | `git diff --exit-code` clean |
| `internal/worker`, `internal/api`, `internal/jobs`, `internal/queue`, `internal/db`, `cmd/*` | UNCHANGED this milestone | ✓ VERIFIED | `git diff --stat da85af6..HEAD` shows zero files under these paths |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `ocr.go` Convert (image) | `enforceImagePixelBound` | direct call before `runCommand("tesseract")` | WIRED | ocr.go:237 precedes ocr.go:243 |
| `ocr.go` Convert (PDF branch) | `convertPDF` (ocrpdf.go) | direct call | WIRED | ocr.go:232 |
| `convertPDF` | `ocrPDFGuardStage` → `ocrPDFPage` loop → `ocrPageMerger` | sequential calls | WIRED | ocrpdf.go:191-244, PATH-restriction-proven ordering |
| `ConvertWithProgress` | `ocrPrepareConversion` (shared preamble with `Convert`) | direct call | WIRED | ocrpdf.go:293, same function `Convert` calls (ocr.go:219) — proven identical accept/reject via `TestOCRPDFPageCapWithProgress` matching `TestOCRPDFPageCap`'s sentinel |
| `ocrpdf.go` onPage callback | `ocrPageProgress.recordPage` → `sink.Update` | closure | WIRED | ocrpdf.go:308-313 |

### Behavioral Spot-Checks / Test Execution

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full OCR test suite, real binaries | `go test ./internal/convert/ -count=1 -v -run OCR` | 100% PASS, zero SKIP, zero FAIL (33 top-level tests + subtests) | ✓ PASS |
| Full convert package suite | `go test ./internal/convert/ -count=1` | `ok ... 16.061s` | ✓ PASS |
| Build | `go build ./...` | clean | ✓ PASS |
| Vet | `go vet ./...` | clean | ✓ PASS |
| Fence: converters.go/exec.go/progress.go unchanged | `git diff --exit-code internal/convert/{converters,exec,progress}.go` | clean (exit 0) | ✓ PASS |
| Fence: no worker/api/jobs/queue/db/cmd edits this milestone | `git diff --stat da85af6..HEAD -- internal/worker internal/api internal/jobs internal/queue internal/db cmd` | empty | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|---|---|---|---|---|
| OCR-01 | 43-03 | image → txt via Tesseract PSM3/OEM1 | ✓ SATISFIED | Truth #2 |
| OCR-02 | 43-04 | streaming PDF page loop | ✓ SATISFIED | Truth #4 |
| OCR-03 | 43-03/43-04 | hOCR/ALTO via real XML encoder | ✓ SATISFIED | Truth #5 |
| OCR-04 | 43-04 | page-granular progress | ✓ SATISFIED | Truth #6 |
| OCRO-01 | 43-01 | closed language allowlist | ✓ SATISFIED | Truth #7 |
| OCRE-02 | 43-02 | oversize-input guard | ✓ SATISFIED (guard-logic scope; REQUIREMENTS.md correctly still tracks "Pending" pending Phase 45's env-wiring closure) | Truth #8 |
| OCRE-03 | 43-02 | born-digital reject | ✓ SATISFIED (guard-logic scope; same Phase-45 note applies to any env-tunable char threshold, though the threshold itself is currently a compiled const, not an env var — no further wiring needed for the born-digital check specifically) | Truth #9 |

No orphaned requirements found for Phase 43 in REQUIREMENTS.md's Phase 43 assignment (OCR-01/02/03/04, OCRO-01, OCRE-02, OCRE-03 — all 7 accounted for above).

### Anti-Patterns Found

None. `grep -n -E "TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER"` across all 5 new OCR source files returns zero matches. No placeholder/"not yet implemented" language in shipped code paths (the one "not yet implemented (see 43-04-PLAN.md)" stub error mentioned in 43-03-SUMMARY.md was replaced by Plan 04's real `convertPDF` dispatch — confirmed current `ocr.go:232` calls `c.convertPDF(...)`, not a stub).

### Human Verification Required

None. All truths are verifiable via grep/build/test against real, locally-installed binaries; no visual/UX/external-service judgment calls are in scope for this standalone, unregistered converter phase.

### Gaps Summary

No gaps. All 9 must-have truths (5 ROADMAP Success Criteria plus OCRO-01/OCRE-02/OCRE-03 requirement-level truths, decomposed for clarity) are VERIFIED against real code and real-binary test execution — not SUMMARY.md narrative. The scope fence (standalone, unregistered, zero touch outside `internal/convert/` plus a 1-line `EngineOCR` const) holds byte-for-byte across the entire milestone-to-date diff (`da85af6..HEAD`). `go build`/`go vet`/`go test` are all clean with tesseract 5.5.2 + poppler 26.07.0 installed, so every "real-binary" test actually executed rather than skip-stubbing.

One documentation nuance worth flagging for awareness (not a blocker): REQUIREMENTS.md's Traceability table lists OCRE-02/OCRE-03 as "Phase 43 | Pending" rather than "Complete," while 43-02-SUMMARY.md's frontmatter claims `requirements-completed: [OCRE-02, OCRE-03]`. Investigation shows this is NOT a false completion claim — the guard *logic* is fully implemented, tested, and fail-closed; what remains open (correctly reflected as "Pending") is Phase 45's job of wiring `OCR_MAX_PAGES`/pixel-cap values from environment variables into the `OCRConverter{MaxPages, MaxImagePixels}` struct fields, which currently default to zero (uncapped) exactly as `av.go`'s established precedent does for the same reason. This is a deliberate, documented phase split, not a hidden gap — no action needed for Phase 43 to be considered goal-achieved.

---

*Verified: 2026-07-26T13:11:17Z*
*Verifier: Claude (gsd-verifier)*
