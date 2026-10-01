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
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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

	// Sandbox confines the engine's filesystem access, mirroring
	// PandocConverter's/LibvipsConverter's own field: zero value runs every
	// OCR engine directly, which is what every unit test wants and what a
	// deployment gets only by saying so out loud -- cmd/ocr-worker refuses
	// to start otherwise.
	Sandbox *engineSandbox
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
	// ocrPrepareConversion (ocrpdf.go, Plan 04) is the SAME preamble
	// ConvertWithProgress calls -- OCROptsFromMap (garbage opts, e.g. a
	// corrupt jobs.options column, is a deterministic failure, mirroring
	// AudioOptsFromMap's/AVOptsFromMap's use in the sibling converters'
	// Convert, CLAUDE.md "Error Handling") plus the fail-fast unsupported-
	// target check (a caller bug -- unrecognized/missing outPath extension
	// -- must not burn a tesseract invocation before failing, mirrors
	// AudioConverter.Convert's/AVConverter.Convert's shape). Sharing this
	// ONE function means Convert and ConvertWithProgress can never drift
	// into making a different accept/reject decision on the same bytes.
	o, targetFormat, configfile, err := ocrPrepareConversion(outPath, opts)
	if err != nil {
		return err
	}

	sourceFormat := NormalizeFormat(filepath.Ext(inPath))
	if sourceFormat == "pdf" {
		// Streaming per-page PDF loop (pdfinfo page-count cap -> born-digital
		// check -> per-page page-box bound -> pdftoppm render -> tesseract ->
		// delete before the next page), defined in ocrpdf.go (Plan 04,
		// 43-CONTEXT.md D-01/SC2). onPage is nil here -- this plain Convert
		// entry point reports no progress; ConvertWithProgress (ocrpdf.go)
		// drives the same pipeline with a non-nil onPage callback.
		return c.convertPDF(ctx, inPath, outPath, targetFormat, configfile, o, nil)
	}

	// Image pixel-dimension guard (OCRE-02, ocrguards.go, Plan 02) --
	// fail-closed BEFORE tesseract ever decodes the image (T-43-02).
	if err := enforceImagePixelBound(inPath, c.MaxImagePixels); err != nil {
		return fmt.Errorf("ocr: %w", err)
	}

	outBase := strings.TrimSuffix(outPath, filepath.Ext(outPath))
	args := ocrTesseractArgs(inPath, outBase, ocrResolvedLanguageArgs(o), configfile)
	x := c.exec(outPath)
	if _, err := x.run(ctx, "tesseract", args...); err != nil {
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
// one single-page tesseract invocation) into ONE multi-page document via
// Go's stdlib encoding/xml -- NEVER string concatenation or a formatted-
// string builder (43-CONTEXT.md D-05 Claude's Discretion; PITFALLS.md
// Pitfall 5, the project's highest-risk OCR failure mode: OCR'd text is
// untrusted content that can contain literal &/</>/" and must round-trip
// as valid XML even after a post-processing merge step, not just in
// tesseract's own already-correct per-page output). This is the SINGLE
// home this plan defines for the merge contract Plan 04's streaming
// multi-page PDF loop calls, so the two plans cannot diverge on the merge
// contract or its escaping discipline (43-03-PLAN.md interfaces section).
//
// Mechanism (Claude's Discretion, D-05): stdlib encoding/xml. AddPage
// decodes each incoming fragment with an xml.Decoder token walk, locates
// the fragment's own page element (a `<div class="ocr_page">` for hocr, a
// `<Page>` for alto), and captures it via DecodeElement into a generic
// node (XMLName + attrs + already-escaped innerxml) -- never re-parsing or
// re-escaping the page's inner content, which tesseract's own writer
// already escaped correctly. Finalize wraps the accumulated nodes in a
// minimal multi-page document struct and serializes it with
// xml.MarshalIndent -- the SAME stdlib encoder, so the merge step carries
// zero hand-rolled XML assembly anywhere in this file (a naive
// concatenation of whole per-page documents, this type's original RED
// stub, produces TWO XML declarations and TWO root elements instead of
// one merged document -- PITFALLS.md Pitfall 5).
type ocrPageMerger struct {
	target string
	pages  []ocrXMLNode
}

// ocrXMLNode is a generic single-element XML capture: XMLName + every
// attribute (`,any,attr`) + the element's raw already-escaped inner
// content (`,innerxml`). Re-marshaling this struct reproduces the
// original element's name/attrs/content byte-for-byte (modulo the
// id/ID attribute AddPage rewrites for the merged page ordinal), without
// ever re-parsing or re-escaping the inner OCR text.
type ocrXMLNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Inner   string     `xml:",innerxml"`
}

// newOCRPageMerger constructs a merger for target ("hocr" or "alto") --
// Plan 04's per-page PDF loop calls AddPage once per rasterized page in
// document order, then Finalize once at the end.
func newOCRPageMerger(target string) *ocrPageMerger {
	return &ocrPageMerger{target: target}
}

// ocrPageElementMatch reports whether se is the single page-level element
// this merger's target wraps its per-page content in.
func ocrPageElementMatch(target string, se xml.StartElement) bool {
	switch target {
	case "hocr":
		if se.Name.Local != "div" {
			return false
		}
		for _, a := range se.Attr {
			if a.Name.Local == "class" && a.Value == "ocr_page" {
				return true
			}
		}
		return false
	case "alto":
		return se.Name.Local == "Page"
	default:
		return false
	}
}

// ocrPageIDAttr reports the attribute name a merged page's renumbered id
// is written to -- "id" for hocr's lowercase-attribute HTML convention,
// "ID" for ALTO's uppercase-attribute XML convention (both live-verified
// against a real tesseract 5.5.2 hocr/alto invocation).
func ocrPageIDAttr(target string) string {
	if target == "alto" {
		return "ID"
	}
	return "id"
}

// ocrPageID formats a merged page's renumbered id/ID attribute value,
// built without string concatenation or a formatted-string builder (a
// plain bytes.Buffer instead) -- the merge step's discipline of never
// hand-rolling XML/attribute content extends even to this small helper.
func ocrPageID(pageNum int) string {
	var b bytes.Buffer
	b.WriteString("page_")
	b.WriteString(strconv.Itoa(pageNum))
	return b.String()
}

// setOCRAttr replaces attrs' entry named local (if present) with value,
// else appends a new one -- used by AddPage to renumber a page's id/ID
// attribute to the merged document's own page ordinal.
func setOCRAttr(attrs []xml.Attr, local, value string) []xml.Attr {
	for i, a := range attrs {
		if a.Name.Local == local {
			attrs[i].Value = value
			return attrs
		}
	}
	return append(attrs, xml.Attr{Name: xml.Name{Local: local}, Value: value})
}

// AddPage decodes fragment (one single-page tesseract hOCR/ALTO output)
// via an xml.Decoder token walk, locates its page element, and appends it
// to the merged document -- renumbering the page's id/ID attribute to
// pageNum (tesseract always numbers a fresh single-page invocation
// "page_1"/PHYSICAL_IMG_NR 0, so every fragment collides on the same id
// unless renumbered here). Returns an error if fragment is not
// well-formed XML or contains no page element for this merger's target --
// fail-closed, never a silently-skipped page.
func (m *ocrPageMerger) AddPage(pageNum int, fragment []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(fragment))
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("ocr: merge: page %d: no %s page element found", pageNum, m.target)
			}
			return fmt.Errorf("ocr: merge: page %d: %w", pageNum, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || !ocrPageElementMatch(m.target, se) {
			continue
		}
		var node ocrXMLNode
		if err := dec.DecodeElement(&node, &se); err != nil {
			return fmt.Errorf("ocr: merge: page %d: decode page element: %w", pageNum, err)
		}
		node.Attrs = setOCRAttr(node.Attrs, ocrPageIDAttr(m.target), ocrPageID(pageNum))
		m.pages = append(m.pages, node)
		return nil
	}
}

// ocrHOCRXmlns and ocrALTOXmlns are the fixed XML namespace URIs Finalize
// stamps on the merged document's root element -- the same namespaces
// tesseract's own single-page hOCR/ALTO writer declares.
const (
	ocrHOCRXmlns = "http://www.w3.org/1999/xhtml"
	ocrALTOXmlns = "http://www.loc.gov/standards/alto/ns-v3#"
)

// ocrHOCRDoc/ocrHOCRBody wrap the merged per-page div nodes in a minimal
// XHTML document -- Finalize's hocr-target output shape.
type ocrHOCRDoc struct {
	XMLName xml.Name    `xml:"html"`
	Xmlns   string      `xml:"xmlns,attr"`
	Body    ocrHOCRBody `xml:"body"`
}

type ocrHOCRBody struct {
	Pages []ocrXMLNode `xml:"div"`
}

// ocrALTODoc/ocrALTOLayout wrap the merged per-page Page nodes in a
// minimal ALTO document -- Finalize's alto-target output shape.
type ocrALTODoc struct {
	XMLName xml.Name      `xml:"alto"`
	Xmlns   string        `xml:"xmlns,attr"`
	Layout  ocrALTOLayout `xml:"Layout"`
}

type ocrALTOLayout struct {
	Pages []ocrXMLNode `xml:"Page"`
}

// Finalize serializes the accumulated pages into one multi-page document
// via xml.MarshalIndent -- never a formatted-string builder or string
// concatenation (PITFALLS.md Pitfall 5). Each page's already-escaped inner
// content (captured verbatim by AddPage's `,innerxml` field) is emitted
// byte-for-byte, so adversarial literal &/</>/" content already escaped by
// tesseract's own writer round-trips correctly through the merge with no
// double-escaping and no corruption.
func (m *ocrPageMerger) Finalize() ([]byte, error) {
	var doc any
	switch m.target {
	case "hocr":
		doc = ocrHOCRDoc{Xmlns: ocrHOCRXmlns, Body: ocrHOCRBody{Pages: m.pages}}
	case "alto":
		doc = ocrALTODoc{Xmlns: ocrALTOXmlns, Layout: ocrALTOLayout{Pages: m.pages}}
	default:
		return nil, fmt.Errorf("ocr: merge: unsupported target %q", m.target)
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("ocr: merge: marshal: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}
