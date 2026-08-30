// Package convert (this file): ocrguards.go implements the memory-bomb and
// wrong-input defenses (OCRE-02/OCRE-03, 43-CONTEXT.md, PITFALLS.md
// Pitfalls 1 and 6) that gate the OCR converter's PDF page loop (Plan 04)
// and image path (Plan 03): a PDF page-count cap, a per-page declared
// page-box physical-size cap, an image declared-pixel-dimension cap, and
// born-digital PDF detection -- all fail closed BEFORE any pdftoppm render
// or image decode. Mirrors av.go's guard-before-subprocess discipline and
// dimensions.go's pre-render declared-dimension bound.
package convert

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// ErrOCRTooManyPages is returned when a PDF's pdfinfo-declared page count
// exceeds the configured cap -- fail-closed BEFORE any pdftoppm render
// (OCRE-02, PITFALLS.md Pitfall 1).
var ErrOCRTooManyPages = errors.New("ocr: pdf page count exceeds bound")

// ErrOCRPageBoxTooLarge is returned when a PDF page's declared physical page
// box (pdfinfo "Page size") would rasterize beyond the pixel bound at the
// target DPI -- a defense-in-depth axis the page-count cap alone does not
// cover: a single-page PDF with a degenerate huge declared page box can
// still exceed the pixel bound at the fixed rasterize DPI even though its
// page COUNT is fine (OCRE-02, PITFALLS.md Pitfall 1).
var ErrOCRPageBoxTooLarge = errors.New("ocr: pdf page box would rasterize beyond the pixel bound")

// ocrPageCountArgs builds pdfinfo's argv against a plain server-generated
// workdir path. pdfinfo takes a bare path (no protocol/flag-injection
// surface like ffmpeg's "-i"), and runCommand never invokes a shell, so
// this is a thin, deliberately unadorned argv builder kept as its own
// function only to mirror this package's "argv builder is its own,
// unit-testable function" convention (av.go, audioduration.go).
func ocrPageCountArgs(pdfPath string) []string {
	return []string{pdfPath}
}

// parsePdfinfoPages scans pdfinfo's stdout for the "Pages:" line and parses
// its integer value. Fails closed (returns an error) when the line is
// absent or its value is unparseable -- this parser never panics on
// unexpected shape and never silently defaults to "0 pages" or "unlimited".
func parsePdfinfoPages(out []byte) (int, error) {
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "Pages:")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return 0, fmt.Errorf("ocr: unparseable pdfinfo Pages line %q: %w", line, err)
		}
		return n, nil
	}
	return 0, errors.New("ocr: pdfinfo output has no Pages line")
}

// ocrPDFPageCount runs pdfinfo through the hardened runCommand (Setpgid +
// SIGKILL-on-timeout, exec.go) and parses the declared page count.
func ocrPDFPageCount(ctx context.Context, pdfPath string) (int, error) {
	out, err := runCommand(ctx, "pdfinfo", ocrPageCountArgs(pdfPath)...)
	if err != nil {
		return 0, fmt.Errorf("ocr: pdfinfo: %w", err)
	}
	return parsePdfinfoPages(out)
}

// enforceMaxPages rejects count > max with ErrOCRTooManyPages. max == 0
// means "no cap this phase" -- mirrors av.go's avMaxSourceDuration/
// avMaxSourceResolutionHeight zero-value-falls-back convention.
// OCR_MAX_PAGES env wiring is Phase 45; a bare zero max here documents
// "cap not configured yet", never a fail-closed reject-all.
func enforceMaxPages(count, max int) error {
	if max > 0 && count > max {
		return fmt.Errorf("%w: declared page count %d exceeds bound %d", ErrOCRTooManyPages, count, max)
	}
	return nil
}

// parsePdfinfoPageSize scans pdfinfo's stdout for the "Page size:" line
// (e.g. "Page size:      612 x 792 pts (letter)") and parses the declared
// width/height in points. Fails closed (ok=false) when the line is absent
// or malformed -- never a silent zero-accept.
func parsePdfinfoPageSize(out []byte) (widthPts, heightPts float64, ok bool) {
	for _, line := range strings.Split(string(out), "\n") {
		rest, found := strings.CutPrefix(line, "Page size:")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		// fields[0]=width, fields[1]="x", fields[2]=height, fields[3]="pts",
		// with an optional trailing "(letter)"-style annotation ignored.
		if len(fields) < 4 || fields[1] != "x" || fields[3] != "pts" {
			return 0, 0, false
		}
		w, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, 0, false
		}
		h, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			return 0, 0, false
		}
		return w, h, true
	}
	return 0, 0, false
}

// enforcePageBoxBound computes the rasterized pixel count a declared
// widthPts x heightPts page box would produce at dpi and rejects it with
// ErrOCRPageBoxTooLarge when that exceeds maxPixels. maxPixels == 0 means
// "no cap this phase", mirroring enforceMaxPages's zero-value convention.
// dpi/maxPixels are plain params this phase -- env wiring is Phase 45.
func enforcePageBoxBound(widthPts, heightPts float64, dpi int, maxPixels uint64) error {
	widthPx := uint64(math.Round(widthPts / 72 * float64(dpi)))
	heightPx := uint64(math.Round(heightPts / 72 * float64(dpi)))
	pixels := widthPx * heightPx
	if maxPixels > 0 && pixels > maxPixels {
		return fmt.Errorf("%w: declared page box %.0fx%.0f pts at %d DPI rasterizes to %d pixels, exceeds bound %d",
			ErrOCRPageBoxTooLarge, widthPts, heightPts, dpi, pixels, maxPixels)
	}
	return nil
}

// ocrPDFPageSize runs pdfinfo and parses the declared page-box physical
// size, failing closed via parsePdfinfoPageSize. Plan 04's PDF loop calls
// this (or a per-page `pdfinfo -f N -l N` variant) before each render.
func ocrPDFPageSize(ctx context.Context, pdfPath string) (widthPts, heightPts float64, err error) {
	out, err := runCommand(ctx, "pdfinfo", ocrPageCountArgs(pdfPath)...)
	if err != nil {
		return 0, 0, fmt.Errorf("ocr: pdfinfo: %w", err)
	}
	w, h, ok := parsePdfinfoPageSize(out)
	if !ok {
		return 0, 0, errors.New("ocr: pdfinfo output has no Page size line")
	}
	return w, h, nil
}

// ErrOCRImageTooLarge is returned when an image's declared pixel
// dimensions exceed the configured bound -- fail-closed BEFORE decode
// (OCRE-02), reusing dimensions.go's declared-dimension parser (the same
// decompression-bomb precedent MAX_IMAGE_PIXELS enforces in
// internal/api/handlers.go).
var ErrOCRImageTooLarge = errors.New("ocr: image dimensions exceed bound")

// enforceImagePixelBound opens inPath, derives its format from the file
// extension, and -- only for formats with a registered dimension parser
// (HasDimensionLimit) -- reads the declared width/height via Dimensions
// (dimensions.go) WITHOUT decoding any pixel data, rejecting when the
// declared pixel count exceeds maxPixels. maxPixels == 0 means "no cap
// this phase" -- mirrors enforceMaxPages/enforcePageBoxBound's convention.
// A Dimensions error (ErrDimensionsUnknown) is itself a fail-closed
// rejection -- never a silent accept. Formats with no registered
// dimension parser (document formats -- HasDimensionLimit false) have no
// pixel-dimension concept and are skipped entirely, mirroring
// HasDimensionLimit's own documented scope guard.
func enforceImagePixelBound(inPath string, maxPixels uint64) error {
	format := NormalizeFormat(strings.TrimPrefix(filepath.Ext(inPath), "."))
	if !HasDimensionLimit(format) {
		return nil
	}
	f, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("ocr: open %q: %w", inPath, err)
	}
	defer f.Close()

	width, height, _, err := Dimensions(format, f)
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}
	pixels := uint64(width) * uint64(height)
	if maxPixels > 0 && pixels > maxPixels {
		return fmt.Errorf("%w: declared %dx%d (%d pixels) exceeds bound %d", ErrOCRImageTooLarge, width, height, pixels, maxPixels)
	}
	return nil
}

// ocrUsableTextLayerThreshold is the non-whitespace character count above
// which a PDF's existing text layer is treated as USABLE -- i.e. worth
// returning to the client instead of rasterizing the pages and OCR-ing them.
//
// The value is unchanged from Phase 43 (16) and its calibration still holds:
// small enough that even a short paragraph of real body text clears it, large
// enough to tolerate the handful of stray non-whitespace bytes pdftotext's own
// formatting can emit for a genuinely image-only PDF (a page number burned
// INTO the image is not extractable text and yields zero -- live-verified
// against the committed multipage_image_only.pdf fixture).
//
// What DID change in Phase 59 is what crossing it means. It used to be a
// REJECTION threshold: over it, the job failed with ErrOCRBornDigital and the
// extracted text was thrown away. It is now a ROUTING threshold: over it, for
// a txt target, that same extracted text IS the result (D-05). The number is
// the same; the sentence around it is the opposite one.
//
// internal/inspect carries its own copy of this constant for its advisory
// text-layer verdict, and a test pins the two equal -- see 59-05. They must
// share a unit, or the divergence between the two detectors (D-08) could not
// be measured at all.
const ocrUsableTextLayerThreshold = 16

// ErrOCRBornDigital is UNREACHABLE as of Phase 59 (D-05/D-06) and is kept
// alive only until Plan 59-06 removes it together with its consumers.
//
// It used to mean "this PDF already has an extractable text layer, so refuse
// the job". Since Plan 59-04 nothing returns it: a txt target now receives
// that text layer as its result, and an hocr/alto target goes on to a real
// render because pdftotext produces no word or line boxes and OCR is their
// only source.
//
// It is deliberately NOT deleted here. internal/worker still references it in
// three places (isOCRTerminal, ocrFailureCode, and a pinning-suite row), and
// unwinding those -- including retiring the born_digital error code from the
// active vocabulary while keeping it documented for jobs that already failed
// with it -- is Plan 59-06's whole subject. Deleting it in this plan would
// break the build across packages to save one commit.
//
// Unreachability is not asserted here, it is PROVEN:
// TestBornDigitalSentinelIsUnreachable (ocrpdf_test.go) runs a born-digital
// PDF through all three targets and requires that none of them produces it.
// Plan 59-06 cites that test as its justification for editing a pinning suite.
var ErrOCRBornDigital = errors.New("ocr: pdf already has an extractable text layer")

// ocrTextLayerArgs builds pdftotext's argv to extract pdfPath's text layer
// to stdout ("-" as the output path, mirroring runCommand's "some engines
// write their machine-readable result to stdout" contract, exec.go).
func ocrTextLayerArgs(pdfPath string) []string {
	return []string{pdfPath, "-"}
}

// countNonWhitespace counts non-whitespace runes in b -- the pure threshold
// input extractPDFTextLayer and its tests both use.
func countNonWhitespace(b []byte) int {
	count := 0
	for _, r := range string(b) {
		if !unicode.IsSpace(r) {
			count++
		}
	}
	return count
}

// hasUsableTextLayer reports whether nonWhitespace clears
// ocrUsableTextLayerThreshold. A one-line function on purpose: it is the
// entire decision, it takes no I/O, and keeping it separate from
// extractPDFTextLayer means the threshold can be tested without spawning a
// process -- and, more to the point, means the code that EXTRACTS text has no
// opinion about what should happen to it.
func hasUsableTextLayer(nonWhitespace int) bool {
	return nonWhitespace > ocrUsableTextLayerThreshold
}

// extractPDFTextLayer runs pdftotext (hardened runCommand) ONCE and returns
// both the extracted bytes and their non-whitespace count.
//
// Returning both from a single invocation is a requirement, not a
// convenience (59-CONTEXT.md, <specifics>). Before Phase 59 this function's
// ancestor -- detectBornDigitalPDF -- ran pdftotext, counted the characters,
// threw the text away, and returned a rejection. The text it discarded was
// the very thing a pdf->txt client had asked for. Fixing that by calling
// pdftotext a second time to re-extract would introduce two answers to one
// question inside a single job: the count that made the decision and the text
// that was returned could disagree, on the same input, for no reason a reader
// could ever reconstruct.
//
// It makes NO decision. A non-nil error means pdftotext itself failed -- a
// genuine engine failure, wrapped as such -- never a verdict about the file.
//
// On the comment this one replaces, and why it is bound to a test. The old
// doc comment here said the rejection was "steering the client to the document
// engine instead". No such route existed: pdf is a SOURCE format in exactly
// one place in this repository (ocr.go's ocrSourceFormats), and every other
// engine only PRODUCES pdf -- libreoffice.go builds {f, "pdf"} pairs, pandoc
// reads markup, chromium does html->pdf. The sentence had been false since it
// was written, and nothing could notice, because a comment cannot fail.
//
// A born-digital PDF now genuinely has a route, and the claim is pinned by
// TestPDFToTxtRouteExists (ocrpdf_test.go), which requires BOTH that the pair
// is advertised and that converting over it produces real output. Either half
// alone is the state this phase found the code in: advertised and refused. If
// that test is ever deleted or weakened, this paragraph goes back to being an
// unbacked assertion -- treat its removal as a contract change, not a cleanup.
func extractPDFTextLayer(ctx context.Context, pdfPath string) (text []byte, nonWhitespace int, err error) {
	out, err := runCommand(ctx, "pdftotext", ocrTextLayerArgs(pdfPath)...)
	if err != nil {
		return nil, 0, fmt.Errorf("ocr: pdftotext: %w", err)
	}
	return out, countNonWhitespace(out), nil
}
