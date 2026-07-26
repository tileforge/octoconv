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
	"strconv"
	"strings"
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
