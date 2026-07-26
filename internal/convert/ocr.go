// Package convert (this file): ocr.go implements OCRConverter, the OCR-01/03
// converter core (43-CONTEXT.md D-01/D-02/D-04): Tesseract-backed text/hOCR/
// ALTO extraction from images, mirroring AudioConverter's (whisper.go)/
// AVConverter's (av.go) standalone-converter shape -- Pairs()/Engine()/
// Convert(), a guard stage BEFORE the expensive subprocess, argv builders
// factored into their own pure functions. Built and unit-tested directly
// against the real tesseract binary this phase, but deliberately NOT
// wired into the process-wide converter registry (mirrors Phase 30/34's
// own fence) -- registration is Phase 44's job. The streaming per-page PDF
// source path (pdftoppm render -> OCR -> delete, D-01/SC2) is Plan 04's
// addition to this same Convert() dispatch; this plan builds and tests the
// IMAGE path only.
package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ocrSourceFormats is the closed set of source formats OCRConverter's
// Pairs() advertises: the four images this project already sniffs
// (jpg/png/tiff/webp) plus pdf (scanned/image-only PDF, born-digital
// rejected by ocrguards.go's detectBornDigitalPDF -- Plan 04's job to call
// it). ocrTargetFormats is the D-01 single-target output set -- one output
// per job, mirroring AudioConverter's {txt,srt,vtt,json} shape.
var (
	ocrSourceFormats = []string{"jpg", "png", "tiff", "webp", "pdf"}
	ocrTargetFormats = []string{"txt", "hocr", "alto"}
)

// OCRConverter extracts text (and, per D-01, hOCR/ALTO word/line bounding
// boxes) from images and scanned/image-only PDFs by shelling out to
// `tesseract` -- the sixth engine class (EngineOCR), mirroring
// AudioConverter's (whisper.go) and AVConverter's (av.go) shape: a plain
// struct, Pairs()/Engine()/Convert(), argv builders isolated as their own
// pure functions for unit-testability. It is built and unit-tested this
// phase directly against a real tesseract/poppler binary but deliberately
// NOT wired into the process-wide converter registry (Phase 43 scope
// fence, mirrors Phase 30's/34's own fence) -- registration is Phase 44's
// responsibility.
//
// MaxImagePixels and MaxPages let an operator-configured instance override
// the guard ceilings (ocrguards.go, Plan 02) without editing package
// consts -- a zero value on either field falls back to "no cap configured
// this phase" (enforceImagePixelBound's/enforceMaxPages's own zero-value
// convention), so a bare OCRConverter{} is well-defined, mirroring
// AVConverter's MaxSourceDuration/MaxSourceResolutionHeight
// zero-value-defaulting contract. MaxPages is unused by this plan's IMAGE
// path -- it exists for Plan 04's PDF page-count guard call.
type OCRConverter struct {
	MaxImagePixels uint64
	MaxPages       int
}

// Pairs returns every {source, target} combination of ocrSourceFormats x
// ocrTargetFormats (D-01: single-target output, 15 pairs = 5 sources x 3
// targets) -- source and target sets are disjoint, so no identity-pair
// filter is needed (mirrors AudioConverter.Pairs'/AVConverter.Pairs' shape).
func (OCRConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(ocrSourceFormats)*len(ocrTargetFormats))
	for _, from := range ocrSourceFormats {
		for _, to := range ocrTargetFormats {
			pairs = append(pairs, Pair{From: from, To: to})
		}
	}
	return pairs
}

// Engine reports the OCR engine class (D-01).
func (OCRConverter) Engine() string { return EngineOCR }

// var _ Converter ensures OCRConverter's concrete value type satisfies the
// Converter interface at compile time -- mirrors AVConverter{}'s own
// assertion (av.go), pinning a value receiver across every method here.
var _ Converter = OCRConverter{}

// ocrPSM and ocrOEM are the D-04 pinned server-side constants: PSM 3
// (fully-automatic page segmentation, no OSD -- the standard for full-page
// documents) and OEM 1 (LSTM-only, matching the baked tessdata_fast
// models, STACK.md). NEVER exposed as a per-job opt -- an unvalidated PSM
// choice is a footgun that can silently wreck otherwise-good input
// (PITFALLS.md Pitfall 3). Live-validated against this plan's committed
// scanned-text fixtures (testdata/ocr/text_sample.png,
// testdata/ocr/adversarial_text.png) in the real-tesseract accuracy tests
// below -- the D-02 eval-gate spend for this phase.
const (
	ocrPSM = "3"
	ocrOEM = "1"
)

// ocrDefaultLanguage is the server-side default resolved when no
// client-requested language is present (OCROpts.Language empty) --
// mirrors whisperArgs' explicit "auto" fallback rationale (whisper.go):
// never left to tesseract's own implicit default, which would silently
// mis-recognize a non-English document for this Russian-first company.
const ocrDefaultLanguage = "eng"

// ocrConfigFileFor maps a normalized target format onto tesseract's own
// output-format configfile name (STACK.md: "one CLI call can emit .txt,
// .hocr, and .xml simultaneously by passing multiple output-format
// configfile names as trailing args" -- this project emits exactly one
// target per job, D-01, so at most one configfile is ever passed). ""
// (ok=true) means no configfile -- tesseract's own bare invocation already
// produces plain outputbase.txt. ok=false signals an unsupported target --
// the caller must fail fast before invoking tesseract at all.
func ocrConfigFileFor(target string) (configfile string, ok bool) {
	switch target {
	case "txt":
		return "", true
	case "hocr":
		return "hocr", true
	case "alto":
		return "alto", true
	default:
		return "", false
	}
}

// ocrTesseractOutputExt maps a target format onto the file extension
// tesseract itself appends to outputbase for that configfile -- live-
// verified against tesseract 5.5.2: a bare invocation writes
// outputbase.txt, the "hocr" configfile writes outputbase.hocr, and the
// "alto" configfile writes outputbase.xml (NOT outputbase.alto). Convert
// renames tesseract's own output to the caller's actual outPath rather
// than assuming tesseract's extension matches the job's target-format
// extension -- never rely on an engine's default output naming (mirrors
// whisperArgs'/transcodeToMP4Args' explicit output-path pinning
// rationale, whisper.go/av.go).
func ocrTesseractOutputExt(target string) string {
	if target == "hocr" || target == "alto" {
		if target == "alto" {
			return ".xml"
		}
		return ".hocr"
	}
	return ".txt"
}

// ocrResolvedLanguageArgs resolves the -l argv fragment for a validated
// OCROpts: the client-requested (already allowlist-validated) language via
// ocrLanguageArgs (ocropts.go, Plan 01) when present, else an EXPLICIT
// ocrDefaultLanguage fallback -- server-side default resolution, never
// left to tesseract's own implicit default (mirrors whisperArgs' "auto"
// fallback rationale, whisper.go).
func ocrResolvedLanguageArgs(o OCROpts) []string {
	if args := ocrLanguageArgs(o); args != nil {
		return args
	}
	return []string{"-l", ocrDefaultLanguage}
}

// ocrTesseractArgs builds tesseract's argv from already-validated inputs:
// imagename, outputbase, pinned PSM 3 / OEM 1 (D-04 -- server constants,
// NEVER per-job opts), the resolved -l language fragment, and an optional
// trailing configfile ("hocr"/"alto", or none for plain txt). Isolated as
// its own pure function for unit-testable flag pinning (mirrors
// ffmpegNormalizeArgs'/whisperArgs' argv-pinning-testability rationale).
// Argument order matters: tesseract's own --help-extra documents "these
// options must occur before any configfile" -- imagename, outputbase,
// then flags, then an optional configfile last.
func ocrTesseractArgs(inPath, outBase string, langArgs []string, configfile string) []string {
	args := []string{inPath, outBase, "--psm", ocrPSM, "--oem", ocrOEM}
	args = append(args, langArgs...)
	if configfile != "" {
		args = append(args, configfile)
	}
	return args
}

// validateOCROutput confirms tesseract actually produced the output file --
// a missing file (os.Stat error) is still a real failure (tesseract
// crashed or was killed mid-write). Deliberately does NOT check size>0:
// an empty txt/hocr/alto file is a VALID, successful result for an image
// with no recognizable text (OCRE-05, PITFALLS.md Pitfall 9) -- unlike
// validateAudioOutput's/validateAVOutput's size>0 check, an empty-output
// branch here would misclassify a correct "nothing to OCR" result as a
// failure.
func validateOCROutput(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("ocr: stat output: %w", err)
	}
	return nil
}

// Convert runs tesseract directly against inPath at pinned PSM 3 / OEM 1
// (D-04), emitting the target format Pairs() advertised for outPath's
// extension: plain txt (no configfile), or hOCR/ALTO via tesseract's own
// output-format configfile (D-01 -- tesseract's own writer already
// escapes OCR'd text correctly into valid XML/XHTML, PITFALLS.md
// Pitfall 5, live-verified in the round-trip tests in Task 3). This is the
// IMAGE path only (jpg/png/tiff/webp) -- the streaming per-page PDF path
// (pdftoppm render -> OCR -> delete, never whole-PDF render) is Plan 04's
// addition to this same dispatch (43-CONTEXT.md D-01/SC2).
//
// webp is passed straight to tesseract with no decode-to-png pre-step:
// STACK.md flags Leptonica's webp support as build-dependent, but live-
// verified against this environment's tesseract 5.5.2 (bundled Leptonica
// 1.87.0 + libwebp 1.6.0) -- a real webp file OCRs correctly with no
// pre-processing. Re-verify against the deployed container's own
// Leptonica build in Phase 45/46 if that assumption ever needs revisiting.
func (c OCRConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	// Garbage opts (e.g. a corrupt jobs.options column) is a deterministic
	// failure, mirroring AudioOptsFromMap's/AVOptsFromMap's use in the
	// sibling converters' Convert (CLAUDE.md "Error Handling").
	o, err := OCROptsFromMap(opts)
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}

	// Fail fast on an unsupported target BEFORE any subprocess -- mirrors
	// AudioConverter.Convert's/AVConverter.Convert's shape (whisper.go,
	// av.go): a caller bug (unrecognized/missing outPath extension) must
	// not burn a tesseract invocation before failing.
	targetFormat := NormalizeFormat(filepath.Ext(outPath))
	configfile, ok := ocrConfigFileFor(targetFormat)
	if !ok {
		return fmt.Errorf("ocr: unsupported target format %q", targetFormat)
	}

	sourceFormat := NormalizeFormat(filepath.Ext(inPath))
	if sourceFormat == "pdf" {
		// Streaming per-page PDF loop (pdfinfo page-count cap -> per-page
		// pdftoppm render -> tesseract -> delete before the next page) is
		// Plan 04's job (43-CONTEXT.md D-01/SC2) -- this plan builds and
		// tests the image path only.
		return fmt.Errorf("ocr: pdf source conversion not yet implemented (see 43-04-PLAN.md)")
	}

	// Image pixel-dimension guard (OCRE-02, ocrguards.go, Plan 02) --
	// fail-closed BEFORE tesseract ever decodes the image (T-43-02).
	if err := enforceImagePixelBound(inPath, c.MaxImagePixels); err != nil {
		return fmt.Errorf("ocr: %w", err)
	}

	outBase := strings.TrimSuffix(outPath, filepath.Ext(outPath))
	args := ocrTesseractArgs(inPath, outBase, ocrResolvedLanguageArgs(o), configfile)
	if _, err := runCommand(ctx, "tesseract", args...); err != nil {
		return fmt.Errorf("ocr: tesseract: %w", err)
	}

	// tesseract's own ALTO writer names its output outputbase.xml, not
	// outputbase.alto -- rename to the caller's actual outPath rather than
	// assume tesseract's own naming matches the job's target-format
	// extension.
	produced := outBase + ocrTesseractOutputExt(targetFormat)
	if produced != outPath {
		if err := os.Rename(produced, outPath); err != nil {
			return fmt.Errorf("ocr: rename output: %w", err)
		}
	}

	return validateOCROutput(outPath)
}

// ocrPageMerger assembles per-page hOCR/ALTO fragments (each the output of
// one single-page tesseract invocation) into ONE multi-page document.
// STUB (RED): this initial shape naively concatenates whole per-page
// fragments -- the GREEN commit that follows replaces this body with a
// real xml.Decoder/xml.Encoder based merge (D-05, PITFALLS.md Pitfall 5).
// Naive concatenation is exactly the failure mode Pitfall 5 warns about:
// two full per-page documents (each with its own XML declaration and root
// element) glued together do not form ONE well-formed multi-page document.
type ocrPageMerger struct {
	target string
	pages  [][]byte
}

// newOCRPageMerger constructs a merger for target ("hocr" or "alto") --
// Plan 04's per-page PDF loop calls AddPage once per rasterized page in
// document order, then Finalize once at the end.
func newOCRPageMerger(target string) *ocrPageMerger {
	return &ocrPageMerger{target: target}
}

// AddPage STUB (RED): appends fragment verbatim with no validation at all
// -- deliberately wrong, replaced by the real xml.Decoder-based
// implementation in the GREEN commit.
func (m *ocrPageMerger) AddPage(pageNum int, fragment []byte) error {
	m.pages = append(m.pages, fragment)
	return nil
}

// Finalize STUB (RED): naively concatenates the raw per-page fragment
// bytes -- deliberately wrong (Pitfall 5), replaced by a real
// xml.MarshalIndent-based single-document serialization in the GREEN
// commit.
func (m *ocrPageMerger) Finalize() ([]byte, error) {
	var out []byte
	for _, p := range m.pages {
		out = append(out, p...)
	}
	return out, nil
}
