// Package convert (this file): ocrpdf.go implements OCRConverter's
// streaming per-page PDF source path (OCR-02, 43-CONTEXT.md SC2, the
// memory-bomb-safety load-bearing criterion, PITFALLS.md Pitfall 1): a
// scanned/image-only PDF is OCR'd exactly one page at a time --
// pdfinfo page-count cap -> born-digital check (both BEFORE any render,
// Plan 02's ocrguards.go) -> per page: a per-page page-box physical-size
// bound -> pdftoppm render -> tesseract -> delete the rendered page image
// (and tesseract's own raw output file) before the next iteration. The
// whole PDF is NEVER rasterized up front -- memory footprint is bounded to
// one page in flight. For hocr/alto targets, per-page fragments are merged
// into ONE valid-XML document via Plan 03's named ocrPageMerger
// (encoding/xml, never string concatenation). ocr.go's Convert dispatches
// its pdf-source branch to convertPDF, defined here; ConvertWithProgress
// (OCR-04, the v1.9 ProgressConverter seam) also lives in this file and
// drives the SAME pipeline via convertPDF's onPage callback.
package convert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ocrRasterizeDPI is the fixed rasterize-DPI target this phase (STACK.md):
// every pdftoppm page render and every enforcePageBoxBound pixel-bound
// computation use this SAME constant, so the guard's declared-size math and
// the actual render always agree on the same DPI. [ASSUMED] Claude's
// Discretion per 43-CONTEXT.md ("the exact DPI computation ... all
// executor/planner's call") -- 300 DPI is STACK.md's named target for
// legible OCR input without an unreasonable per-page pixel count.
const ocrRasterizeDPI = 300

// ocrRasterizePageArgs builds pdftoppm's argv for rendering EXACTLY ONE
// page (page, both -f and -l set to the same value -- the single-page
// bound that proves this is a streaming, never whole-PDF, render) to a
// deterministically-named PNG. -singlefile suppresses pdftoppm's own
// page-number-suffix naming convention (live-verified: without it,
// pdftoppm names its output "<outBase>-<page>.png"; with it, plain
// "<outBase>.png") so the caller never needs to parse/guess a
// zero-padded page-number suffix to find the rendered file. Isolated as
// its own pure function (mirrors av.go's/whisper.go's argv-builder
// convention) so the single-page bound is directly unit-testable without
// spawning any process.
func ocrRasterizePageArgs(pdfPath, outBase string, page, dpi int) []string {
	return []string{
		"-r", strconv.Itoa(dpi),
		"-f", strconv.Itoa(page),
		"-l", strconv.Itoa(page),
		"-singlefile",
		"-png",
		pdfPath, outBase,
	}
}

// ocrPDFPageSizeArgs builds pdfinfo's argv for a per-page page-box probe
// (-f page -l page). Isolated as its own pure function for unit-testable
// argv pinning, mirroring ocrRasterizePageArgs.
func ocrPDFPageSizeArgs(pdfPath string, page int) []string {
	return []string{"-f", strconv.Itoa(page), "-l", strconv.Itoa(page), pdfPath}
}

// parsePdfinfoPerPagePageSize scans pdfinfo's stdout for a per-page
// "Page    N size:  W x H pts" line (live-verified: when pdfinfo is
// invoked with -f/-l, its "Page size:" line grows a page-number field --
// "Page    2 size:  300 x 200 pts" -- unlike the whole-document form
// ocrguards.go's parsePdfinfoPageSize already handles, "Page size:  W x H
// pts" with no embedded page number). Fails closed (ok=false) when no
// matching line is found for page -- never a silent zero-accept, mirroring
// parsePdfinfoPageSize's own discipline (ocrguards.go, Plan 02).
func parsePdfinfoPerPagePageSize(out []byte, page int) (widthPts, heightPts float64, ok bool) {
	pageStr := strconv.Itoa(page)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		if fields[0] != "Page" || fields[1] != pageStr || fields[2] != "size:" {
			continue
		}
		if fields[4] != "x" || fields[6] != "pts" {
			continue
		}
		w, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		h, err := strconv.ParseFloat(fields[5], 64)
		if err != nil {
			continue
		}
		return w, h, true
	}
	return 0, 0, false
}

// ocrPDFPageSizeForPage runs pdfinfo (hardened runCommand) restricted to a
// single page and parses its declared physical page-box size via
// parsePdfinfoPerPagePageSize -- called by ocrPDFPage BEFORE that page's
// pdftoppm render (defense-in-depth over the whole-document page-count cap,
// T-43-01b).
func ocrPDFPageSizeForPage(ctx context.Context, x engineExec, pdfPath string, page int) (widthPts, heightPts float64, err error) {
	out, runErr := x.run(ctx, "pdfinfo", ocrPDFPageSizeArgs(pdfPath, page)...)
	if runErr != nil {
		return 0, 0, fmt.Errorf("pdfinfo: %w", runErr)
	}
	w, h, ok := parsePdfinfoPerPagePageSize(out, page)
	if !ok {
		return 0, 0, fmt.Errorf("pdfinfo: no page-box size for page %d", page)
	}
	return w, h, nil
}

// ocrPDFGuardResult is what ocrPDFGuardStage hands back to convertPDF: the
// page count it established, and -- when the answer is already in hand -- the
// finished output.
//
// The `done` flag exists so the CALLER never has to re-derive the routing
// decision. convertPDF must not look at targetFormat to work out whether to
// render; if it did, the target-dependence would live in two places and the
// two could disagree, which is exactly the property ocrPDFGuardStage was
// factored out to prevent for Convert and ConvertWithProgress.
type ocrPDFGuardResult struct {
	totalPages int
	// text is the extracted text layer, set only when done is true.
	text []byte
	// done reports that no render is needed: text below IS the conversion
	// result and the caller should write it out.
	done bool
}

// ocrPDFGuardStage runs the fail-closed checks that must fire BEFORE any
// render, and -- new in Phase 59 (D-05/D-06) -- decides which of the two
// routes a PDF takes. It is shared by Convert's PDF branch (via convertPDF)
// and by ConvertWithProgress, so both entry points make the PROVABLY
// identical decision on the same bytes (mirrors av.go's avPrepareConversion
// sharing discipline).
//
// The page-count cap (ocrPDFPageCount + enforceMaxPages) runs FIRST and on
// EVERY route, extraction included. It bounds the size of the work, not the
// method: a ten-thousand-page document is refused whether or not it has a text
// layer.
//
// Then the target decides, and this is what Phase 59 changed:
//
//   - txt, with a usable text layer -> the extracted text IS the result. No
//     rasterize, no tesseract. Before this phase the same pdftotext output was
//     computed, counted, thrown away, and the job failed terminally with
//     born_digital -- while GET /v1/formats and POST /v1/inspect both went on
//     advertising pdf->txt as an available operation. The route did not need
//     building; it needed to stop being discarded.
//
//   - txt, without a usable text layer -> a scan. The ordinary per-page OCR
//     loop, exactly as before.
//
//   - hocr / alto -> the per-page OCR loop, ALWAYS, and pdftotext is not
//     invoked at all. These targets ask for word and line bounding boxes;
//     pdftotext does not produce them in any form, so an existing text layer
//     cannot answer the request and its presence tells us nothing worth
//     spending a subprocess on. Refusing these was refusing the only route
//     that could ever satisfy them.
//
// Cost is already bounded for the render routes by enforceMaxPages and the
// per-page enforcePageBoxBound; D-06 adds no new limit because none is needed.
func (c OCRConverter) ocrPDFGuardStage(ctx context.Context, x engineExec, pdfPath, targetFormat string) (ocrPDFGuardResult, error) {
	totalPages, err := ocrPDFPageCount(ctx, x, pdfPath)
	if err != nil {
		return ocrPDFGuardResult{}, fmt.Errorf("ocr: %w", err)
	}
	if err := enforceMaxPages(totalPages, c.MaxPages); err != nil {
		return ocrPDFGuardResult{}, fmt.Errorf("ocr: %w", err)
	}

	if targetFormat != "txt" {
		// hocr/alto: OCR is the only source of the boxes these targets are
		// defined by. Not calling pdftotext here is deliberate -- a probe
		// whose answer cannot change the outcome is a subprocess spent for
		// nothing, and TestOCRPDFBoxTargetsSkipTextExtraction proves it is
		// not spent by running with pdftotext absent from PATH entirely.
		return ocrPDFGuardResult{totalPages: totalPages}, nil
	}

	text, nonWhitespace, err := extractPDFTextLayer(ctx, x, pdfPath)
	if err != nil {
		return ocrPDFGuardResult{}, fmt.Errorf("ocr: %w", err)
	}
	if hasUsableTextLayer(nonWhitespace) {
		// The SAME bytes the count was taken from (<specifics>): one
		// pdftotext invocation decides and delivers, so the count that chose
		// this route and the text returned by it can never disagree.
		return ocrPDFGuardResult{totalPages: totalPages, text: text, done: true}, nil
	}
	return ocrPDFGuardResult{totalPages: totalPages}, nil
}

// ocrPDFPage renders and OCRs exactly ONE page of pdfPath: bound the page's
// declared physical page-box (enforcePageBoxBound, Plan 02, T-43-01b)
// BEFORE invoking pdftoppm at all, render that single page to a PNG
// (ocrRasterizePageArgs, hardened runCommand), OCR it via Plan 03's
// tesseract argv builder, and return the raw output fragment bytes. BOTH
// the rendered page image AND tesseract's own raw output file are removed
// via defer -- on every return path, success or error -- before this
// function returns, so the per-page loop (convertPDF) never has more than
// one page's image in flight and never leaks a page's worth of disk state
// (T-43-01/T-43-07).
func (c OCRConverter) ocrPDFPage(ctx context.Context, x engineExec, pdfPath, tmpDir string, page int, langArgs []string, configfile, targetFormat string) ([]byte, error) {
	wPts, hPts, err := ocrPDFPageSizeForPage(ctx, x, pdfPath, page)
	if err != nil {
		return nil, fmt.Errorf("ocr: %w", err)
	}
	if err := enforcePageBoxBound(wPts, hPts, ocrRasterizeDPI, c.MaxImagePixels); err != nil {
		return nil, fmt.Errorf("ocr: %w", err)
	}

	pageImageBase := filepath.Join(tmpDir, "page")
	if _, err := x.run(ctx, "pdftoppm", ocrRasterizePageArgs(pdfPath, pageImageBase, page, ocrRasterizeDPI)...); err != nil {
		return nil, fmt.Errorf("ocr: pdftoppm: %w", err)
	}
	pageImage := pageImageBase + ".png"
	defer os.Remove(pageImage) // delete BEFORE the next iteration -- one page in flight, never all N (SC2)

	pageOutBase := filepath.Join(tmpDir, "pageout")
	args := ocrTesseractArgs(pageImage, pageOutBase, langArgs, configfile)
	if _, err := x.run(ctx, "tesseract", args...); err != nil {
		return nil, fmt.Errorf("ocr: tesseract: %w", err)
	}
	produced := pageOutBase + ocrTesseractOutputExt(targetFormat)
	defer os.Remove(produced)

	fragment, err := os.ReadFile(produced)
	if err != nil {
		return nil, fmt.Errorf("ocr: read page output: %w", err)
	}
	return fragment, nil
}

// convertPDF runs the full streaming per-page PDF pipeline: the guard stage
// (ocrPDFGuardStage) fires BEFORE any render, then the per-page loop
// (ocrPDFPage) renders/OCRs/deletes one page at a time, accumulating plain
// txt or feeding hocr/alto fragments to Plan 03's ocrPageMerger. onPage,
// when non-nil, is invoked after each page completes with that page's own
// elapsed processing duration -- ConvertWithProgress (Task 3) uses this to
// drive a page-granular sink.Update ramp; Convert (ocr.go) passes nil. Every
// engine call site shares ONE engineExec (c.exec(outPath), Phase 68), built
// once from outPath's directory; the per-page render directory
// (ocrPageWorkDir) lives INSIDE that same job directory -- the only
// writable path the job's ruleset grants -- and is removed unconditionally
// on every return path.
func (c OCRConverter) convertPDF(ctx context.Context, inPath, outPath, targetFormat, configfile string, o OCROpts, onPage func(page, total int, elapsed time.Duration)) error {
	x := c.exec(outPath)
	guard, err := c.ocrPDFGuardStage(ctx, x, inPath, targetFormat)
	if err != nil {
		return err
	}
	if guard.done {
		// The extraction route. Deliberately the SAME completion path the OCR
		// route uses below -- write outPath, then validateOCROutput -- rather
		// than a special case: an empty result is valid for both, and a second
		// completion path is a second place for the two to drift.
		if err := os.WriteFile(outPath, guard.text, 0o644); err != nil {
			return fmt.Errorf("ocr: write output: %w", err)
		}
		return validateOCROutput(outPath)
	}
	totalPages := guard.totalPages

	// Inside the job directory (filepath.Dir(outPath)) -- the only writable
	// path the job's ruleset grants. A directory under the system temp dir
	// would be denied to pdftoppm/tesseract under Landlock.
	tmpDir, err := ocrPageWorkDir(outPath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	langArgs := ocrResolvedLanguageArgs(o)

	var merger *ocrPageMerger
	if configfile != "" {
		merger = newOCRPageMerger(targetFormat)
	}
	var textBuf bytes.Buffer

	for page := 1; page <= totalPages; page++ {
		start := time.Now()
		fragment, err := c.ocrPDFPage(ctx, x, inPath, tmpDir, page, langArgs, configfile, targetFormat)
		if err != nil {
			return err
		}
		elapsed := time.Since(start)

		if merger != nil {
			if err := merger.AddPage(page, fragment); err != nil {
				return fmt.Errorf("ocr: %w", err)
			}
		} else {
			textBuf.Write(fragment)
		}

		if onPage != nil {
			onPage(page, totalPages, elapsed)
		}
	}

	var finalBytes []byte
	if merger != nil {
		finalBytes, err = merger.Finalize()
		if err != nil {
			return fmt.Errorf("ocr: %w", err)
		}
	} else {
		finalBytes = textBuf.Bytes()
	}

	if err := os.WriteFile(outPath, finalBytes, 0o644); err != nil {
		return fmt.Errorf("ocr: write output: %w", err)
	}
	return validateOCROutput(outPath)
}

// var _ ProgressConverter ensures OCRConverter's concrete value type
// satisfies the v1.9 optional progress seam at compile time (OCR-04) --
// mirrors AVConverter{}'s own assertion (av.go), pinning a value receiver
// across every OCRConverter method, OCR being the SECOND ProgressConverter
// implementor (av.go was first).
var _ ProgressConverter = OCRConverter{}

// ocrPrepareConversion runs the EXACT SAME preamble Convert (ocr.go) and
// ConvertWithProgress both need -- OCROptsFromMap, the fail-fast
// unsupported-target check, and the source-format/configfile resolution --
// factored into ONE function so the two entry points can never drift into
// making a DIFFERENT accept/reject decision on the same bytes (mirrors
// av.go's avPrepareConversion sharing discipline). Every error is already
// wrapped with the "ocr:" prefix, so callers return it unwrapped.
func ocrPrepareConversion(outPath string, opts map[string]any) (o OCROpts, targetFormat, configfile string, err error) {
	o, err = OCROptsFromMap(opts)
	if err != nil {
		return OCROpts{}, "", "", fmt.Errorf("ocr: %w", err)
	}
	targetFormat = NormalizeFormat(filepath.Ext(outPath))
	configfile, ok := ocrConfigFileFor(targetFormat)
	if !ok {
		return OCROpts{}, "", "", fmt.Errorf("ocr: unsupported target format %q", targetFormat)
	}
	return o, targetFormat, configfile, nil
}

// ConvertWithProgress implements ProgressConverter (OCR-04): it reuses the
// EXACT SAME preamble as Convert (ocrPrepareConversion) so both entry
// points make the provably identical accept/reject decision on the same
// bytes -- including the same page-count cap, born-digital, and per-page
// page-box gates, since a PDF source is routed through the SAME convertPDF
// pipeline Convert itself calls (ocr.go). For a PDF source, onPage is a
// non-nil callback that folds each completed page's elapsed processing
// time into a page-based estimator (ocrprogress.go) and forwards the
// resulting ProgressUpdate to sink.Update -- a page-granular ramp keyed on
// pages_done/total_pages, never on progress.go's ffmpeg-specific tick
// parser. For a single-image source, this delegates straight to Convert
// and therefore emits ZERO sink.Update calls -- the image path stays on
// the plain Convert code path exactly as Convert itself runs it, and the
// worker maps a converter that reports nothing to null->100 (OCR-04). This
// function never writes a terminal 100% itself (mirrors av.go's own
// ConvertWithProgress contract): a mid-stream error simply leaves sink's
// last-reported value in place, never throttling on its own (the sink owns
// throttling, D-03).
//
// Phase 59 adds a SECOND zero-Update case alongside the single-image one, and
// it is named here so it does not read as an oversight: a born-digital PDF
// converted to txt is answered from its existing text layer, so there is no
// per-page loop at all and onPage is never called. The worker's existing "a
// converter that reports nothing maps to null->100" rule (OCR-04) covers it
// unchanged -- the same rule, reached by a new route.
func (c OCRConverter) ConvertWithProgress(ctx context.Context, inPath, outPath string, opts map[string]any, sink ProgressSink) error {
	o, targetFormat, configfile, err := ocrPrepareConversion(outPath, opts)
	if err != nil {
		return err
	}

	sourceFormat := NormalizeFormat(filepath.Ext(inPath))
	if sourceFormat != "pdf" {
		// Zero Update calls (OCR-04): the image path is cheap and carries
		// no natural page-granular signal, so it stays on the plain
		// Convert path, identically to Convert's own behavior for this
		// source.
		return c.Convert(ctx, inPath, outPath, opts)
	}

	var progress *ocrPageProgress
	onPage := func(page, total int, elapsed time.Duration) {
		if progress == nil {
			progress = newOCRPageProgress(total)
		}
		sink.Update(progress.recordPage(elapsed))
	}
	return c.convertPDF(ctx, inPath, outPath, targetFormat, configfile, o, onPage)
}
