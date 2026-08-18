// This file holds the cross-family property PR9R23 turned out to be about:
// dispatchInspectFamilies charges an operation ONE allowance
// (tr.Consume(maxBytes)) on the assumption that the analyzer it dispatched read
// at most that much. Every analyzer that quietly performed a second read broke
// that assumption -- pdf.go's tail window, image.go's dimension peek,
// container.go's workbook part, and ClassifyCFB's directory walk, of which only
// the first two were reported.
//
// A per-analyzer test that "the tail window is now maxBytes/4" would pin the
// FIX and miss the next instance. These tests pin the PROPERTY instead, by
// counting the bytes each analyzer actually asks the reader for, so a future
// analyzer (or a future extra read inside an existing one) fails here without
// anyone having to remember this round.
package inspect

import (
	"bytes"
	"testing"
)

// The counting reader lives in media_test.go (same package) and records every
// byte requested, which is the honest quantity here: a read that asks for more
// than the allowance has already cost the process that much I/O, whether or not
// the file happened to be long enough to satisfy it.

func TestAnalyzersNeverRequestMoreThanTheirAllowance(t *testing.T) {
	// Every fixture is deliberately LARGER than the allowance below, since that
	// is the only case where a second read can overshoot: when the file fits,
	// one window already covers it.
	const maxBytes = 4096

	bigPDF := func() []byte {
		d := cleanPDFFixture()
		pad := bytes.Repeat([]byte("% padding line\n"), 4000)
		// Trailer at the tail, body in the middle: the shape that makes the
		// analyzer want both a head and a tail read.
		return append(append(d[:len(d)-6], pad...), []byte("trailer\n<< /Root 1 0 R >>\n%%EOF")...)
	}

	for _, tc := range []struct {
		name    string
		analyze func(r *countingReaderAt, size int64)
	}{
		{
			"pdf: head plus trailer tail",
			func(r *countingReaderAt, size int64) {
				AnalyzePDF(r, size, maxBytes, nil)
			},
		},
		{
			"image/png: dimension peek plus metadata scan",
			func(r *countingReaderAt, size int64) {
				AnalyzeImage("png", r, size, maxBytes, 1<<30, nil)
			},
		},
		{
			"image/jpeg: dimension peek plus metadata scan",
			func(r *countingReaderAt, size int64) {
				AnalyzeImage("jpg", r, size, maxBytes, 1<<30, nil)
			},
		},
		{
			"media/mp4: head plus tail moov search",
			func(r *countingReaderAt, size int64) {
				AnalyzeMedia("mp4", r, size, maxBytes, nil)
			},
		},
	} {
		var data []byte
		switch {
		case tc.name == "pdf: head plus trailer tail":
			data = bigPDF()
		case tc.name == "media/mp4: head plus tail moov search":
			data = tailMoovMP4Fixture(1000, 5000, 640, 480, "avc1", isobmffHandlerVideo, 60000)
		case tc.name == "image/jpeg: dimension peek plus metadata scan":
			data = append(jpegFixtureWithAPP1([]byte("Exif\x00\x00padding"), 100, 50), bytes.Repeat([]byte{0x00}, 60000)...)
		default:
			data = append(pngFixture(64, 64), bytes.Repeat([]byte{0x00}, 60000)...)
		}
		if int64(len(data)) <= maxBytes {
			t.Fatalf("%s: fixture is %d bytes, must exceed the %d-byte allowance for this test to mean anything", tc.name, len(data), maxBytes)
		}

		t.Run(tc.name, func(t *testing.T) {
			r := &countingReaderAt{data: data}
			tc.analyze(r, int64(len(data)))
			if r.totalRequested > maxBytes {
				t.Errorf("requested %d bytes with an allowance of %d -- the dispatcher charges the allowance ONCE (tr.Consume(maxBytes)), so any analyzer reading more makes that accounting a false statement about a ceiling it reports as enforced", r.totalRequested, maxBytes)
			}
		})
	}
}

// TestBudgetedReaderAtStopsAtTheAllowance covers the wrapper that bounds a
// parser taking no allowance of its own (ClassifyCFB), including the part that
// matters most: it REPORTS that it cut a read short, so the caller can say the
// answer is unknown because of our ceiling rather than publish a structural
// verdict drawn from bytes nobody read.
func TestBudgetedReaderAtStopsAtTheAllowance(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 8192)

	t.Run("serves up to the limit and reports exceeded", func(t *testing.T) {
		inner := &countingReaderAt{data: data}
		b := NewBudgetedReaderAt(inner, 1024)

		buf := make([]byte, 4096)
		n, _ := b.ReadAt(buf, 0)
		if n != 1024 {
			t.Errorf("first ReadAt served %d bytes, want 1024 (the whole allowance)", n)
		}
		if inner.totalRequested > 1024 {
			t.Errorf("underlying reader was asked for %d bytes, want at most 1024 -- clamping must happen BEFORE the read, or the bytes are already spent", inner.totalRequested)
		}
		if !b.Exceeded() {
			t.Error("Exceeded() = false after a read was clamped -- the caller cannot distinguish our ceiling from a real answer without this")
		}

		n2, err := b.ReadAt(buf, 1024)
		if n2 != 0 || err == nil {
			t.Errorf("second ReadAt = (%d, %v), want (0, non-nil) once the allowance is gone", n2, err)
		}
	})

	t.Run("a read within the allowance is untouched", func(t *testing.T) {
		inner := &countingReaderAt{data: data}
		b := NewBudgetedReaderAt(inner, 8192)

		buf := make([]byte, 512)
		n, err := b.ReadAt(buf, 0)
		if n != 512 || err != nil {
			t.Errorf("ReadAt = (%d, %v), want (512, nil)", n, err)
		}
		if b.Exceeded() {
			t.Error("Exceeded() = true for a read well inside the allowance")
		}
	})

	t.Run("a non-positive allowance serves nothing", func(t *testing.T) {
		inner := &countingReaderAt{data: data}
		b := NewBudgetedReaderAt(inner, 0)
		if n, err := b.ReadAt(make([]byte, 16), 0); n != 0 || err == nil {
			t.Errorf("ReadAt = (%d, %v), want (0, non-nil)", n, err)
		}
		if inner.totalRequested != 0 {
			t.Errorf("underlying reader was asked for %d bytes with a zero allowance", inner.totalRequested)
		}
		if !b.Exceeded() {
			t.Error("Exceeded() = false after refusing a read outright")
		}
	})
}

// TestAnalyzeImageDimensionsUnderTinyAllowanceIsCeilingNotMalformed is the
// misattribution guard on the image half of the fix. When the shared window is
// smaller than the peek convert.Dimensions would have taken for itself, the
// parser fails for want of bytes -- and blaming the IMAGE for that is exactly
// the defect PR9R16 fixed on this same code path. The reason must name our
// ceiling.
func TestAnalyzeImageDimensionsUnderTinyAllowanceIsCeilingNotMalformed(t *testing.T) {
	data := append(pngFixture(64, 64), bytes.Repeat([]byte{0x00}, 60000)...)

	// 8 bytes: enough to be a read, nowhere near a PNG header's IHDR.
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), 8, 1<<30, nil)

	if got := res.Characteristics["width"]; got != nil {
		t.Errorf("width = %v, want nil -- 8 bytes cannot have located a declared width", got)
	}
	if got := res.Characteristics["width_reason"]; got != string(ReasonWorkCeilingBytes) {
		t.Errorf("width_reason = %v, want %q -- the parser was handed less than its own peek, so its failure is our ceiling, not a fact about the image", got, ReasonWorkCeilingBytes)
	}
	// The pixel-bomb check must stay unknown for the same reason, and never
	// clean: unknown dimensions are never evidence of a safe size (T-58-26).
	assertCheckStatus(t, res, CodeImagePixelBombShape, StatusUnknown)
}
