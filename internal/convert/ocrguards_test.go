package convert

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// pdfinfoFixtureWithPages mirrors a real captured `pdfinfo` stdout block
// (live-verified against the committed testdata/ocr/multipage_image_only.pdf)
// declaring a Pages line -- used by the pure parsePdfinfoPages tests so they
// never need to shell out.
const pdfinfoFixtureWithPages = `Title:           multipage_image_only
Author:          https://imagemagick.org
Creator:         https://imagemagick.org
Producer:        https://imagemagick.org
CreationDate:    Sun Jul 26 15:11:55 2026 MSK
ModDate:         Sun Jul 26 15:11:55 2026 MSK
Custom Metadata: no
Metadata Stream: no
Tagged:          no
UserProperties:  no
Suspects:        no
Form:            none
JavaScript:      no
Pages:           7
Encrypted:       no
Page size:       300 x 200 pts
Page rot:        0
File size:       9503 bytes
Optimized:       no
PDF version:     1.3
`

// pdfinfoFixtureNoPages has no Pages line at all -- exercises the fail-closed
// path.
const pdfinfoFixtureNoPages = `Custom Metadata: no
Metadata Stream: no
Encrypted:       no
Page rot:        0
File size:       9503 bytes
Optimized:       no
PDF version:     1.3
`

// pdfinfoFixtureLetterPageSize mirrors a real captured pdfinfo block (from
// the committed born_digital.pdf fixture) declaring a standard letter page
// size.
const pdfinfoFixtureLetterPageSize = `Producer:        GPL Ghostscript 10.07.1
Pages:           1
Encrypted:       no
Page size:       612 x 792 pts (letter)
Page rot:        0
PDF version:     1.4
`

const pdfinfoFixtureNoPageSize = `Producer:        GPL Ghostscript 10.07.1
Pages:           1
Encrypted:       no
Page rot:        0
PDF version:     1.4
`

func TestParsePdfinfoPages_Found(t *testing.T) {
	n, err := parsePdfinfoPages([]byte(pdfinfoFixtureWithPages))
	if err != nil {
		t.Fatalf("parsePdfinfoPages error: %v", err)
	}
	if n != 7 {
		t.Fatalf("got %d, want 7", n)
	}
}

func TestParsePdfinfoPages_MissingFailsClosed(t *testing.T) {
	_, err := parsePdfinfoPages([]byte(pdfinfoFixtureNoPages))
	if err == nil {
		t.Fatal("expected an error when Pages line is absent, got nil")
	}
}

func TestParsePdfinfoPageSize_Found(t *testing.T) {
	w, h, ok := parsePdfinfoPageSize([]byte(pdfinfoFixtureLetterPageSize))
	if !ok {
		t.Fatal("parsePdfinfoPageSize: ok = false, want true")
	}
	if w != 612 || h != 792 {
		t.Fatalf("got (%.0f,%.0f), want (612,792)", w, h)
	}
}

func TestParsePdfinfoPageSize_MissingFailsClosed(t *testing.T) {
	_, _, ok := parsePdfinfoPageSize([]byte(pdfinfoFixtureNoPageSize))
	if ok {
		t.Fatal("parsePdfinfoPageSize: ok = true for output with no Page size line, want false")
	}
}

func TestEnforceMaxPages(t *testing.T) {
	if err := enforceMaxPages(7, 5); !errors.Is(err, ErrOCRTooManyPages) {
		t.Fatalf("enforceMaxPages(7,5) = %v, want ErrOCRTooManyPages", err)
	}
	if err := enforceMaxPages(5, 5); err != nil {
		t.Fatalf("enforceMaxPages(5,5) = %v, want nil (at the cap is allowed)", err)
	}
	// max == 0 means "no cap this phase" -- mirrors av.go's zero-value
	// falls-back convention; a huge declared page count must still pass
	// when no cap has been configured yet.
	if err := enforceMaxPages(1_000_000, 0); err != nil {
		t.Fatalf("enforceMaxPages(_,0) = %v, want nil (zero max = uncapped this phase)", err)
	}
}

func TestEnforcePageBoxBound(t *testing.T) {
	// A normal US letter page at 300 DPI: 612/72*300 = 2550, 792/72*300 = 3300
	// -> 8,415,000 pixels. A generous bound comfortably above that passes.
	if err := enforcePageBoxBound(612, 792, 300, 50_000_000); err != nil {
		t.Fatalf("normal letter page at 300 DPI: got %v, want nil", err)
	}
	// A degenerate huge declared page box (e.g. a crafted PDF claiming a
	// 100000x100000 pt page) at 300 DPI must be rejected.
	if err := enforcePageBoxBound(100_000, 100_000, 300, 50_000_000); !errors.Is(err, ErrOCRPageBoxTooLarge) {
		t.Fatalf("degenerate huge page box: got %v, want ErrOCRPageBoxTooLarge", err)
	}
	// maxPixels == 0 documents "no cap this phase" -- mirrors
	// enforceMaxPages's zero-value convention.
	if err := enforcePageBoxBound(100_000, 100_000, 300, 0); err != nil {
		t.Fatalf("enforcePageBoxBound(_,_,_,0) = %v, want nil (zero maxPixels = uncapped this phase)", err)
	}
}

func TestOCRPDFPageCount_RealBinary(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed, skipping real-binary test")
	}
	ctx := context.Background()
	n, err := ocrPDFPageCount(ctx, filepath.Join("testdata", "ocr", "multipage_image_only.pdf"))
	if err != nil {
		t.Fatalf("ocrPDFPageCount error: %v", err)
	}
	if n != 3 {
		t.Fatalf("got %d pages, want 3", n)
	}
}

func TestOCRPDFPageSize_RealBinary(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed, skipping real-binary test")
	}
	ctx := context.Background()
	w, h, err := ocrPDFPageSize(ctx, filepath.Join("testdata", "ocr", "multipage_image_only.pdf"))
	if err != nil {
		t.Fatalf("ocrPDFPageSize error: %v", err)
	}
	if w != 300 || h != 200 {
		t.Fatalf("got (%.0f,%.0f), want (300,200)", w, h)
	}
}

func TestEnforceImagePixelBound_WithinBoundPasses(t *testing.T) {
	// text_sample.png is 400x120 = 48,000 declared pixels (identify-verified
	// at fixture-creation time).
	path := filepath.Join("testdata", "ocr", "text_sample.png")
	if err := enforceImagePixelBound(path, 1_000_000); err != nil {
		t.Fatalf("enforceImagePixelBound within bound: got %v, want nil", err)
	}
}

func TestEnforceImagePixelBound_ExceedsBoundRejects(t *testing.T) {
	path := filepath.Join("testdata", "ocr", "text_sample.png")
	if err := enforceImagePixelBound(path, 100); !errors.Is(err, ErrOCRImageTooLarge) {
		t.Fatalf("enforceImagePixelBound over tiny bound: got %v, want ErrOCRImageTooLarge", err)
	}
}

func TestEnforceImagePixelBound_UnknownDimensionsFailsClosed(t *testing.T) {
	// A .png extension whose bytes are not a valid PNG must fail closed via
	// ErrDimensionsUnknown -- never a silent accept.
	dir := t.TempDir()
	path := filepath.Join(dir, "bogus.png")
	if err := os.WriteFile(path, []byte("not a real png"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	err := enforceImagePixelBound(path, 1_000_000)
	if !errors.Is(err, ErrDimensionsUnknown) {
		t.Fatalf("enforceImagePixelBound on malformed png: got %v, want wrapped ErrDimensionsUnknown", err)
	}
}

func TestEnforceImagePixelBound_ZeroMaxPixelsUncapped(t *testing.T) {
	// maxPixels == 0 documents "no cap this phase" -- mirrors
	// enforceMaxPages/enforcePageBoxBound's zero-value convention.
	path := filepath.Join("testdata", "ocr", "text_sample.png")
	if err := enforceImagePixelBound(path, 0); err != nil {
		t.Fatalf("enforceImagePixelBound(_,0) = %v, want nil (zero maxPixels = uncapped this phase)", err)
	}
}

func TestEnforceImagePixelBound_NonImageFormatSkipsCheck(t *testing.T) {
	// A format with no registered dimension parser (e.g. a document format)
	// has no pixel-dimension concept and must not be rejected by this guard
	// at all -- mirrors HasDimensionLimit's own document-format scope guard.
	dir := t.TempDir()
	path := filepath.Join(dir, "whatever.docx")
	if err := os.WriteFile(path, []byte("not a real docx"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := enforceImagePixelBound(path, 1); err != nil {
		t.Fatalf("enforceImagePixelBound on non-image format: got %v, want nil (no dimension concept)", err)
	}
}

func TestCountNonWhitespace(t *testing.T) {
	if n := countNonWhitespace([]byte("  ab\tcd\n ef  ")); n != 6 {
		t.Fatalf("got %d, want 6", n)
	}
	if n := countNonWhitespace([]byte("   \n\t  ")); n != 0 {
		t.Fatalf("got %d, want 0 for whitespace-only input", n)
	}
	if n := countNonWhitespace([]byte("")); n != 0 {
		t.Fatalf("got %d, want 0 for empty input", n)
	}
}

// TestExtractPDFTextLayer_RealBinary is the Phase 59 replacement for
// TestDetectBornDigitalPDF_RealBinary. Same two fixtures, same threshold, but
// it asserts what the function now does: extract and count, without deciding.
//
// The born-digital case additionally checks that the returned BYTES and the
// returned COUNT come from one another -- the single-invocation requirement
// from 59-CONTEXT.md <specifics>. Its predecessor could not check this,
// because it discarded the bytes.
func TestExtractPDFTextLayer_RealBinary(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed, skipping real-binary test")
	}
	ctx := context.Background()

	t.Run("born-digital PDF yields a usable layer", func(t *testing.T) {
		text, n, err := extractPDFTextLayer(ctx, filepath.Join("testdata", "ocr", "born_digital.pdf"))
		if err != nil {
			t.Fatalf("extractPDFTextLayer = %v, want nil", err)
		}
		if !hasUsableTextLayer(n) {
			t.Fatalf("hasUsableTextLayer(%d) = false, want true (threshold %d)", n, ocrUsableTextLayerThreshold)
		}
		if got := countNonWhitespace(text); got != n {
			t.Fatalf("returned text has %d non-whitespace chars but the returned count is %d -- the bytes and the "+
				"count must come from ONE pdftotext invocation, or the answer that chose the route and the answer "+
				"returned to the client can disagree", got, n)
		}
	})

	t.Run("image-only PDF yields no usable layer", func(t *testing.T) {
		_, n, err := extractPDFTextLayer(ctx, filepath.Join("testdata", "ocr", "multipage_image_only.pdf"))
		if err != nil {
			t.Fatalf("extractPDFTextLayer = %v, want nil", err)
		}
		if hasUsableTextLayer(n) {
			t.Fatalf("hasUsableTextLayer(%d) = true for an image-only PDF, want false", n)
		}
	})
}

// TestHasUsableTextLayerThreshold pins the boundary without spawning a
// process -- the threshold is a pure decision and is tested as one.
func TestHasUsableTextLayerThreshold(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want bool
	}{
		{0, false},
		{ocrUsableTextLayerThreshold - 1, false},
		{ocrUsableTextLayerThreshold, false}, // strictly greater, matching Phase 43
		{ocrUsableTextLayerThreshold + 1, true},
	} {
		if got := hasUsableTextLayer(tc.n); got != tc.want {
			t.Errorf("hasUsableTextLayer(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}
