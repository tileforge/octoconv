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
func ocrPDFPageSizeForPage(ctx context.Context, pdfPath string, page int) (widthPts, heightPts float64, err error) {
	out, runErr := runCommand(ctx, "pdfinfo", ocrPDFPageSizeArgs(pdfPath, page)...)
	if runErr != nil {
		return 0, 0, fmt.Errorf("pdfinfo: %w", runErr)
	}
	w, h, ok := parsePdfinfoPerPagePageSize(out, page)
	if !ok {
		return 0, 0, fmt.Errorf("pdfinfo: no page-box size for page %d", page)
	}
	return w, h, nil
}

// ocrPDFGuardStage runs the two fail-closed checks that must fire BEFORE
// any render (OCRE-02/OCRE-03, T-43-01/T-43-06): the pdfinfo page-count cap
// (Plan 02's ocrPDFPageCount/enforceMaxPages) and the pdftotext born-digital
// detector (Plan 02's detectBornDigitalPDF). Shared by Convert's PDF branch
// (via convertPDF) and ConvertWithProgress so both entry points make the
// PROVABLY identical accept/reject decision on the same bytes (mirrors
// av.go's avPrepareConversion sharing discipline).
func (c OCRConverter) ocrPDFGuardStage(ctx context.Context, pdfPath string) (totalPages int, err error) {
	totalPages, err = ocrPDFPageCount(ctx, pdfPath)
	if err != nil {
		return 0, fmt.Errorf("ocr: %w", err)
	}
	if err := enforceMaxPages(totalPages, c.MaxPages); err != nil {
		return 0, fmt.Errorf("ocr: %w", err)
	}
	if err := detectBornDigitalPDF(ctx, pdfPath); err != nil {
		return 0, fmt.Errorf("ocr: %w", err)
	}
	return totalPages, nil
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
func (c OCRConverter) ocrPDFPage(ctx context.Context, pdfPath, tmpDir string, page int, langArgs []string, configfile, targetFormat string) ([]byte, error) {
	wPts, hPts, err := ocrPDFPageSizeForPage(ctx, pdfPath, page)
	if err != nil {
		return nil, fmt.Errorf("ocr: %w", err)
	}
	if err := enforcePageBoxBound(wPts, hPts, ocrRasterizeDPI, c.MaxImagePixels); err != nil {
		return nil, fmt.Errorf("ocr: %w", err)
	}

	pageImageBase := filepath.Join(tmpDir, "page")
	if _, err := runCommand(ctx, "pdftoppm", ocrRasterizePageArgs(pdfPath, pageImageBase, page, ocrRasterizeDPI)...); err != nil {
		return nil, fmt.Errorf("ocr: pdftoppm: %w", err)
	}
	pageImage := pageImageBase + ".png"
	defer os.Remove(pageImage) // delete BEFORE the next iteration -- one page in flight, never all N (SC2)

	pageOutBase := filepath.Join(tmpDir, "pageout")
	args := ocrTesseractArgs(pageImage, pageOutBase, langArgs, configfile)
	if _, err := runCommand(ctx, "tesseract", args...); err != nil {
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
// drive a page-granular sink.Update ramp; Convert (ocr.go) passes nil. A
// server-owned temp dir (os.MkdirTemp) holds each page's transient files
// and is removed unconditionally on every return path -- temp paths are
// never client-controlled.
func (c OCRConverter) convertPDF(ctx context.Context, inPath, outPath, targetFormat, configfile string, o OCROpts, onPage func(page, total int, elapsed time.Duration)) error {
	totalPages, err := c.ocrPDFGuardStage(ctx, inPath)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "octoconv-ocr-pdf-*")
	if err != nil {
		return fmt.Errorf("ocr: pdf tmpdir: %w", err)
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
		fragment, err := c.ocrPDFPage(ctx, inPath, tmpDir, page, langArgs, configfile, targetFormat)
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
