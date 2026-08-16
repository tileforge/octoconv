package inspect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// ===== fixture builders =====

// pngFixture builds a minimal but STRUCTURALLY COMPLETE PNG: signature +
// IHDR declaring width x height + the mandatory terminal IEND chunk.
//
// The IEND was added in PR9R16 (image.go:426). Until then this helper
// produced an IHDR-only file -- a PNG truncated mid-stream at a tidy chunk
// boundary -- and the analyzer reported metadata_present: clean for it,
// because "the chunk walk consumed the buffer exactly" was mistaken for the
// format's own end. It is not; IEND is. Tests that want a CLEAN png must
// therefore hand it a complete one, and the truncation cases below build
// their own deliberately incomplete inputs by slicing this.
func pngFixture(width, height uint32) []byte {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	data = append(data, 0x00, 0x00, 0x00, 0x0D)
	data = append(data, []byte("IHDR")...)
	data = append(data, byte(width>>24), byte(width>>16), byte(width>>8), byte(width))
	data = append(data, byte(height>>24), byte(height>>16), byte(height>>8), byte(height))
	data = append(data, 0x08, 0x02, 0x00, 0x00, 0x00)
	data = append(data, 0x00, 0x00, 0x00, 0x00) // CRC placeholder, not validated
	data = append(data, 0x00, 0x00, 0x00, 0x00) // IEND length: zero
	data = append(data, []byte("IEND")...)
	data = append(data, 0x00, 0x00, 0x00, 0x00) // CRC placeholder, not validated
	return data
}

func pngAppendChunk(data []byte, chunkType string, chunkData []byte) []byte {
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(chunkData)))
	data = append(data, length...)
	data = append(data, []byte(chunkType)...)
	data = append(data, chunkData...)
	data = append(data, 0, 0, 0, 0) // CRC placeholder, not validated
	return data
}

func jpegSegment(marker byte, payload []byte) []byte {
	segLen := len(payload) + 2
	out := []byte{0xFF, marker, byte(segLen >> 8), byte(segLen)}
	return append(out, payload...)
}

// jpegFixtureWithAPP1 builds SOI + an APP1 segment carrying app1Payload +
// a minimal SOF0 declaring width x height (mirrors
// internal/convert/dimensions_test.go's jpegFixtureWithDHT SOF shape).
func jpegFixtureWithAPP1(app1Payload []byte, width, height uint16) []byte {
	data := []byte{0xFF, 0xD8} // SOI
	data = append(data, jpegSegment(0xE1, app1Payload)...)

	sofPayload := []byte{0x08} // precision
	sofPayload = append(sofPayload, byte(height>>8), byte(height))
	sofPayload = append(sofPayload, byte(width>>8), byte(width))
	sofPayload = append(sofPayload, 0x03, 0x01, 0x11, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01)
	data = append(data, jpegSegment(0xC0, sofPayload)...)
	return data
}

// ===== assertion helpers =====

func assertFinding(t *testing.T, res FamilyResult, code Code) Finding {
	t.Helper()
	for _, f := range res.Findings {
		if f.Code == code {
			return f
		}
	}
	t.Fatalf("findings %+v do not contain %s", res.Findings, code)
	return Finding{}
}

func assertCheckStatus(t *testing.T, res FamilyResult, code Code, status CheckStatus) {
	t.Helper()
	for _, c := range res.Checks {
		if c.Code == code {
			if c.Status != status {
				t.Fatalf("check %s status = %s, want %s", code, c.Status, status)
			}
			return
		}
	}
	t.Fatalf("checks %+v do not contain %s", res.Checks, code)
}

// assertCheckReason is assertCheckStatus's Reason-checking sibling, used by
// the quick/260811-r5p task 4 tests below to pin down which of
// ReasonWorkCeilingBytes/ReasonMalformedInput a Check carries -- the whole
// point of this task's fix is that these two are no longer interchangeable.
func assertCheckReason(t *testing.T, res FamilyResult, code Code, reason Reason) {
	t.Helper()
	for _, c := range res.Checks {
		if c.Code == code {
			if c.Reason != reason {
				t.Fatalf("check %s reason = %q, want %q", code, c.Reason, reason)
			}
			return
		}
	}
	t.Fatalf("checks %+v do not contain %s", res.Checks, code)
}

// ===== Task 2 behavior tests =====

func TestAnalyzeImagePNGDimensions(t *testing.T) {
	data := pngFixture(100, 50)
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	w, ok := res.Characteristics["width"].(uint32)
	if !ok || w != 100 {
		t.Fatalf("width = %#v, want 100", res.Characteristics["width"])
	}
	h, ok := res.Characteristics["height"].(uint32)
	if !ok || h != 50 {
		t.Fatalf("height = %#v, want 50", res.Characteristics["height"])
	}
	pixels, ok := res.Characteristics["pixels"].(uint64)
	if !ok || pixels != 5000 {
		t.Fatalf("pixels = %#v, want 5000", res.Characteristics["pixels"])
	}
}

func TestAnalyzeImagePixelBombShapeFinding(t *testing.T) {
	data := pngFixture(100000, 100000) // 10 billion declared pixels
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<20, nil)
	assertFinding(t, res, CodeImagePixelBombShape)
}

func TestAnalyzeImagePixelProductDoesNotOverflow(t *testing.T) {
	const big = 0xFFFFFFFE // just under max-uint32
	data := pngFixture(big, big)
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<40, nil)

	pixels, ok := res.Characteristics["pixels"].(uint64)
	if !ok {
		t.Fatalf("pixels missing or wrong type: %#v", res.Characteristics["pixels"])
	}
	want := uint64(big) * uint64(big)
	if pixels != want {
		t.Fatalf("pixels = %d, want %d (uint64 product, not a 32-bit-truncated one)", pixels, want)
	}
	assertFinding(t, res, CodeImagePixelBombShape)
}

func TestAnalyzeImageJPEGWithEXIFAPP1IsMetadataPresent(t *testing.T) {
	app1 := append([]byte("Exif\x00\x00"), []byte{0x4D, 0x4D, 0, 0}...)
	data := jpegFixtureWithAPP1(app1, 100, 50)
	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertFinding(t, res, CodeMetadataPresent)
}

func TestAnalyzeImageJPEGWithXMPAPP1IsMetadataPresent(t *testing.T) {
	app1 := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), []byte("<x:xmpmeta/>")...)
	data := jpegFixtureWithAPP1(app1, 100, 50)
	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertFinding(t, res, CodeMetadataPresent)
}

func TestAnalyzeImagePNGWithEXIfChunkIsMetadataPresent(t *testing.T) {
	data := pngFixture(10, 10)
	data = pngAppendChunk(data, "eXIf", []byte{0, 0, 0, 0})
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertFinding(t, res, CodeMetadataPresent)
}

func TestAnalyzeImageCleanPNGHasCleanChecks(t *testing.T) {
	data := pngFixture(10, 10)
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertCheckStatus(t, res, CodeMetadataPresent, StatusClean)
	assertCheckStatus(t, res, CodeImagePixelBombShape, StatusClean)
}

// TestAnalyzeImageUnknownDimensionsNeverReportClean is the main test of
// this task: dimensions that cannot be determined must produce
// StatusUnknown for the pixel-bomb-shape check, never StatusClean --
// "we don't know" is not "we know there is no bomb" (T-58-26).
func TestAnalyzeImageUnknownDimensionsNeverReportClean(t *testing.T) {
	data := pngFixture(100, 50)[:20] // truncated before IHDR's width/height are fully readable
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeImagePixelBombShape, StatusUnknown)
	if res.Characteristics["width"] != nil {
		t.Fatalf("width = %#v, want nil", res.Characteristics["width"])
	}
	reason, _ := res.Characteristics["width_reason"].(string)
	if reason != string(ReasonMalformedInput) {
		t.Fatalf("width_reason = %q, want %q", reason, ReasonMalformedInput)
	}
	if res.Characteristics["pixels"] != nil {
		t.Fatalf("pixels = %#v, want nil", res.Characteristics["pixels"])
	}
}

// TestAnalyzeImageMetadataPastWindowIsUnknownNotClean is codex PR9 finding 3
// (T-SLE-03): an eXIf chunk placed AFTER the bounded read window must never
// be reported clean -- the window did not cover the whole file, so a miss
// inside it is not evidence the metadata does not exist.
func TestAnalyzeImageMetadataPastWindowIsUnknownNotClean(t *testing.T) {
	data := pngFixture(10, 10) // signature(8) + length(4) + "IHDR"(4) + 13-byte IHDR body + CRC(4) = 33 bytes
	data = pngAppendChunk(data, "eXIf", []byte{0, 0, 0, 0})

	const window = 33 // covers exactly the IHDR chunk, ends before the eXIf chunk appended above
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), window, 1<<30, nil)

	if _, ok := findingFor(res, CodeMetadataPresent); ok {
		t.Fatal("got a Finding for metadata_present -- the eXIf chunk sits past the read window and must never be reported as found")
	}
	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	for _, c := range res.Checks {
		if c.Code == CodeMetadataPresent && c.Reason != ReasonWorkCeilingBytes {
			t.Fatalf("Check(metadata_present).Reason = %q, want %q", c.Reason, ReasonWorkCeilingBytes)
		}
	}
}

// TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean is codex PR9 round 2
// (image.go:209): a PNG whose bounded window covers the WHOLE file (so the
// byte-window truncation flag alone is false) can still hide metadata past
// scanPNGMetadata's own maxMetadataMarkerScanIterations cap if enough
// harmless chunks precede the eXIf chunk -- that miss must report unknown,
// never clean with complete coverage.
func TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean(t *testing.T) {
	data := pngFixture(10, 10)
	for i := 0; i < maxMetadataMarkerScanIterations+10; i++ {
		data = pngAppendChunk(data, "tEXt", []byte{'a'})
	}
	data = pngAppendChunk(data, "eXIf", []byte{0, 0, 0, 0})

	// maxBytes covers the ENTIRE file -- the byte-window truncation flag
	// must be false, isolating this test to the iteration-ceiling path.
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if _, ok := findingFor(res, CodeMetadataPresent); ok {
		t.Fatal("got a Finding for metadata_present -- the eXIf chunk sits past the iteration ceiling and must never be reported as found")
	}
	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	for _, c := range res.Checks {
		if c.Code == CodeMetadataPresent && c.Reason != ReasonWorkCeilingBytes {
			t.Fatalf("Check(metadata_present).Reason = %q, want %q", c.Reason, ReasonWorkCeilingBytes)
		}
	}
}

// TestAnalyzeImageJPEGIterationCeilingIsUnknownNotClean is
// TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean's JPEG analog --
// codex PR9 round 2's own phrasing ("The JPEG and WebP walkers have the
// same shape") names this walker explicitly.
func TestAnalyzeImageJPEGIterationCeilingIsUnknownNotClean(t *testing.T) {
	var filler []byte
	// Each COM (0xFE) segment with a 1-byte payload costs one iteration of
	// scanJPEGMetadata's marker loop.
	for i := 0; i < maxMetadataMarkerScanIterations+10; i++ {
		filler = append(filler, jpegSegment(0xFE, []byte{'x'})...)
	}
	app1 := append([]byte("Exif\x00\x00"), []byte{0x4D, 0x4D, 0, 0}...)
	data := []byte{0xFF, 0xD8} // SOI
	data = append(data, filler...)
	data = append(data, jpegSegment(0xE1, app1)...) // APP1 Exif, past the ceiling

	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if _, ok := findingFor(res, CodeMetadataPresent); ok {
		t.Fatal("got a Finding for metadata_present -- the APP1 Exif segment sits past the iteration ceiling and must never be reported as found")
	}
	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	for _, c := range res.Checks {
		if c.Code == CodeMetadataPresent && c.Reason != ReasonWorkCeilingBytes {
			t.Fatalf("Check(metadata_present).Reason = %q, want %q", c.Reason, ReasonWorkCeilingBytes)
		}
	}
}

// TestAnalyzeImageWebPIterationCeilingIsUnknownNotClean is
// TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean's WebP analog.
func TestAnalyzeImageWebPIterationCeilingIsUnknownNotClean(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WEBP")
	for i := 0; i < maxMetadataMarkerScanIterations+10; i++ {
		data = append(data, riffChunk("JUNK", []byte{'x'})...)
	}
	data = append(data, riffChunk("EXIF", []byte{0, 0, 0, 0})...)
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))

	res := AnalyzeImage("webp", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if _, ok := findingFor(res, CodeMetadataPresent); ok {
		t.Fatal("got a Finding for metadata_present -- the EXIF chunk sits past the iteration ceiling and must never be reported as found")
	}
	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	for _, c := range res.Checks {
		if c.Code == CodeMetadataPresent && c.Reason != ReasonWorkCeilingBytes {
			t.Fatalf("Check(metadata_present).Reason = %q, want %q", c.Reason, ReasonWorkCeilingBytes)
		}
	}
}

// riffChunk builds one RIFF chunk (fourCC + little-endian size + data,
// word-aligned with a trailing pad byte for odd-length data).
func riffChunk(fourCC string, data []byte) []byte {
	out := []byte(fourCC)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(data)))
	out = append(out, size...)
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func TestAnalyzeImageMetadataLocationNamesMarker(t *testing.T) {
	app1 := append([]byte("Exif\x00\x00"), []byte{0, 0, 0, 0}...)
	data := jpegFixtureWithAPP1(app1, 100, 50)
	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	f := assertFinding(t, res, CodeMetadataPresent)
	if f.Location == "" {
		t.Fatal("Location is empty, want it to name the marker")
	}
}

func TestAnalyzeImageDeclaredFormatIsPresent(t *testing.T) {
	data := pngFixture(10, 10)
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	if res.Characteristics["declared_format"] != "png" {
		t.Fatalf("declared_format = %#v, want %q", res.Characteristics["declared_format"], "png")
	}
}

func TestAnalyzeImageNeverPanics(t *testing.T) {
	cases := []struct {
		name   string
		format string
		data   []byte
		size   int64
	}{
		{"empty png", "png", []byte{}, 0},
		{"truncated png", "png", pngFixture(10, 10)[:5], 5},
		{"png size claims larger than actual", "png", pngFixture(10, 10), 1 << 20},
		{"garbage jpg", "jpg", []byte{0xFF, 0xD8, 0x00}, 3},
		{"unrecognized format", "xyz", []byte{1, 2, 3}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("AnalyzeImage panicked: %v", p)
				}
			}()
			_ = AnalyzeImage(tc.format, bytes.NewReader(tc.data), tc.size, tc.size, 1<<30, nil)
		})
	}
}

// ===== quick/260811-r5p task 4 (class sweep, PR9R5-SWEEP-CLASSES(б)) =====
//
// scanJPEGMetadata, scanPNGMetadata, scanRIFFChunkMetadata (WebP), and the
// TIFF branch all used to abort on a STRUCTURAL problem (a declared
// length/size running past the buffer, an invalid magic-byte header,
// leftover bytes too short for another header) by returning exactly what a
// fully-walked, genuinely empty scan returns. The tests below pin the fix:
// only reaching the format's own natural end (JPEG EOI/SOS; PNG/WebP chunk
// walk consuming the buffer exactly; TIFF's byte-order header check against
// a COMPLETE window) may report clean. Every other exit reports
// unknown/malformed_input, distinct from unknown/work_ceiling_bytes.

// TestAnalyzeImageJPEGReachingEOIWithNoMetadataIsClean is the JPEG positive
// natural-end case: a buffer that walks all the way to EOI without any
// metadata marker is a genuine clean, not a structural failure.
func TestAnalyzeImageJPEGReachingEOIWithNoMetadataIsClean(t *testing.T) {
	data := []byte{0xFF, 0xD8} // SOI
	data = append(data, jpegSegment(0xFE, []byte("no metadata here"))...)
	data = append(data, 0xFF, 0xD9) // EOI: JPEG's own natural end

	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertCheckStatus(t, res, CodeMetadataPresent, StatusClean)
}

// TestAnalyzeImageJPEGMissingSOIIsMalformedNotClean is the defect's own
// worked example applied to JPEG's magic bytes: a buffer that never even
// starts with a valid SOI marker must not be reported the same way a
// genuine EOI-terminated miss is.
func TestAnalyzeImageJPEGMissingSOIIsMalformedNotClean(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03} // no FFD8 at all
	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImageJPEGSegmentLengthPastBufferIsMalformedNotClean is the
// NAMED inversion test for scanJPEGMetadata's segLen-overflow exit
// (image.go's `segLen < 2 || i+2+segLen > len(b)` branch): a JPEG whose
// APP1 segment declares a length running past the buffer must never be
// reported metadata_present:clean.
func TestAnalyzeImageJPEGSegmentLengthPastBufferIsMalformedNotClean(t *testing.T) {
	data := []byte{0xFF, 0xD8} // SOI
	data = append(data, 0xFF, 0xE1, 0xFF, 0xFF)
	// APP1 marker claiming a 0xFFFF-byte segment, but the buffer ends
	// immediately after the length field -- the declared length runs
	// nowhere near an actual EOI/SOS.

	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImageJPEGTruncatedMarkerHeaderIsMalformedNotClean exercises
// scanJPEGMetadata's `i+4 > len(b)` exit: the buffer ends mid marker-length
// field, before EOI/SOS is ever reached.
func TestAnalyzeImageJPEGTruncatedMarkerHeaderIsMalformedNotClean(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE1} // SOI + start of an APP1 marker, no length bytes at all
	res := AnalyzeImage("jpg", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImagePNGWithIENDIsCleanNaturalEnd is the PNG positive
// natural-end case: a well-formed PNG that reaches IEND (its own chunk
// stream ending exactly at the buffer's end) with no metadata is genuinely
// clean.
func TestAnalyzeImagePNGWithIENDIsCleanNaturalEnd(t *testing.T) {
	data := pngFixture(10, 10)
	data = pngAppendChunk(data, "IEND", []byte{})
	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertCheckStatus(t, res, CodeMetadataPresent, StatusClean)
}

// TestAnalyzeImagePNGDeclaredLengthPastBufferIsMalformedNotClean is the
// defect's own worked example: a PNG whose second chunk declares a length
// running past the file must never be reported metadata_present:clean --
// this is the exact scenario 260811-r5p-SUMMARY-task4-sweep.md names.
func TestAnalyzeImagePNGDeclaredLengthPastBufferIsMalformedNotClean(t *testing.T) {
	data := pngFixture(10, 10)
	chunkHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(chunkHeader[0:4], 0xFFFFFF00) // huge declared length
	copy(chunkHeader[4:8], []byte("IDAT"))
	data = append(data, chunkHeader...)
	data = append(data, 0, 0, 0, 0) // a few trailing bytes, nowhere near the declared length

	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImagePNGLeftoverBytesAreMalformedNotClean is the NAMED
// inversion test for scanPNGMetadata's natural-end rule: leftover bytes too
// short to form another chunk header, with the window covering the whole
// file, must not be reported clean even though the OLD code's loop-exit
// fallthrough returned exactly that.
func TestAnalyzeImagePNGLeftoverBytesAreMalformedNotClean(t *testing.T) {
	data := pngFixture(10, 10)
	data = append(data, 0x00, 0x00, 0x00) // 3 leftover bytes: too short for another chunk header

	res := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImageWebPBufferConsumedExactlyIsCleanNaturalEnd is the WebP
// positive natural-end case: the RIFF chunk walk consuming the buffer
// exactly, with no metadata chunk seen, is a genuine clean.
func TestAnalyzeImageWebPBufferConsumedExactlyIsCleanNaturalEnd(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WEBP")
	data = append(data, riffChunk("VP8 ", []byte{1, 2, 3, 4})...) // even-length payload: no pad byte
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))

	res := AnalyzeImage("webp", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertCheckStatus(t, res, CodeMetadataPresent, StatusClean)
}

// TestAnalyzeImageWebPDeclaredSizePastBufferIsMalformedNotClean is the
// NAMED inversion test for scanRIFFChunkMetadata's declared-size-overflow
// exit -- the exact same shape as scanPNGMetadata's, which
// 260811-r5p-SUMMARY-task4-sweep.md's own table never separately examined
// for WebP.
func TestAnalyzeImageWebPDeclaredSizePastBufferIsMalformedNotClean(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WEBP")
	chunk := make([]byte, 8)
	copy(chunk[0:4], []byte("JUNK"))
	binary.LittleEndian.PutUint32(chunk[4:8], 0xFFFFFF00) // huge declared size
	data = append(data, chunk...)
	data = append(data, 0, 0, 0, 0) // trailing bytes nowhere near the declared size
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))

	res := AnalyzeImage("webp", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImageWebPLeftoverBytesAreMalformedNotClean mirrors
// TestAnalyzeImagePNGLeftoverBytesAreMalformedNotClean for WebP's RIFF
// chunk walk.
func TestAnalyzeImageWebPLeftoverBytesAreMalformedNotClean(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WEBP")
	data = append(data, riffChunk("VP8 ", []byte{1, 2, 3, 4})...)
	data = append(data, 0x00, 0x00, 0x00) // 3 leftover bytes: too short for another chunk header
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))

	res := AnalyzeImage("webp", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImageTIFFValidHeaderIsMetadataPresent confirms scanTIFFMetadata
// still reports FOUND for a minimal valid byte-order header -- TIFF carries
// an IFD by construction, so this path is unaffected by this task's fix.
func TestAnalyzeImageTIFFValidHeaderIsMetadataPresent(t *testing.T) {
	data := []byte{0x49, 0x49, 42, 0, 8, 0, 0, 0} // "II" little-endian header
	res := AnalyzeImage("tiff", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertFinding(t, res, CodeMetadataPresent)
}

// TestAnalyzeImageTIFFInvalidHeaderWithFullWindowIsMalformedNotClean is the
// NAMED inversion test for the TIFF branch: a full-window read (the byte
// window covers the WHOLE claimed file) whose first bytes are not a valid
// TIFF byte-order mark disagrees with the format claim itself and must
// report malformed_input, never the same clean a short window would have
// produced under the OLD code.
func TestAnalyzeImageTIFFInvalidHeaderWithFullWindowIsMalformedNotClean(t *testing.T) {
	data := []byte{0, 1, 2, 3, 4, 5, 6, 7} // neither "II" nor "MM"
	res := AnalyzeImage("tiff", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonMalformedInput)
}

// TestAnalyzeImageTIFFShortWindowIsCeilingNotMalformed is
// TestAnalyzeImageTIFFInvalidHeaderWithFullWindowIsMalformedNotClean's
// counterpart: when the window is too short to even validate the 8-byte
// header (maxBytes < size), the same "header did not validate" symptom
// must report work_ceiling_bytes, not malformed_input -- this is this
// task's own precedence rule (ceiling wins when the window itself was cut
// short) applied to the TIFF branch specifically.
func TestAnalyzeImageTIFFShortWindowIsCeilingNotMalformed(t *testing.T) {
	data := []byte{0x49, 0x49, 42, 0, 8, 0, 0, 0, 0, 0, 0, 0} // a genuinely valid header, 12 bytes total
	const window = 4                                          // shorter than the 8 bytes scanTIFFMetadata needs to validate a header

	res := AnalyzeImage("tiff", bytes.NewReader(data), int64(len(data)), window, 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonWorkCeilingBytes)
}

// TestAnalyzeImageHEICMissWithFullWindowStaysClean documents the
// deliberate, justified exception: scanISOBMFFMetadataTokens performs no
// structural walk to abort from (see its own doc comment), so a miss with
// a full (non-truncated) window keeps reporting clean, unlike every other
// scanner this task touches.
func TestAnalyzeImageHEICMissWithFullWindowStaysClean(t *testing.T) {
	data := []byte("ftypheicmeta no relevant tokens here")
	res := AnalyzeImage("heic", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	assertCheckStatus(t, res, CodeMetadataPresent, StatusClean)
}

// ===== quick/260812-r6c task 2 (codex PR9R6 finding 2, image.go:165) =====

// readFailureReaderAt is an io.ReaderAt whose ReadAt always fails with a
// real, non-EOF error -- simulating a temp-file or disk read failure that is
// entirely independent of the file's declared size, unlike every other
// fixture in this file (which is always a real, in-memory bytes.Reader).
// Deliberately a DIFFERENT type from pdf_test.go's own erroringReaderAt
// (which is a zero-field, value-receiver type returning a fixed "boom"
// error): this one carries its own configurable err so a future test can
// tell distinct injected errors apart if it ever needs to.
type readFailureReaderAt struct {
	err error
}

func (e *readFailureReaderAt) ReadAt(p []byte, off int64) (int, error) {
	return 0, e.err
}

var errSimulatedDiskFailure = errors.New("simulated disk read failure")

// TestAnalyzeImageReadFailureWithFullWindowIsNotClean is the NAMED
// inversion test for the finding itself, verbatim: "When readWindow fails
// with a non-EOF I/O error while size <= maxBytes, windowTruncated is
// false, so this returns all false flags and AnalyzeImage emits
// metadata_present: clean. Thus a temporary-file or disk read failure is
// presented as a successful complete scan; this exit should carry an
// unknown/malformed or explicit read-failure status rather than the clean
// zero value." size <= maxBytes here (the window is sized to cover the
// file IN FULL), so windowTruncated is false and the ONLY reason
// readWindow fails is readFailureReaderAt's own non-EOF error -- exactly the
// scenario the finding names.
func TestAnalyzeImageReadFailureWithFullWindowIsNotClean(t *testing.T) {
	r := &readFailureReaderAt{err: errSimulatedDiskFailure}
	const size = 1024
	res := AnalyzeImage("png", r, size, size, 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonReadFailed)
}

// TestAnalyzeImageReadFailureWithTruncatedWindowStillReportsCeiling proves
// the read-failure branch does not disturb the PRE-EXISTING ceiling
// classification when the window itself was ALSO too small (maxBytes <
// size): windowTruncated must still win, exactly as it did before this
// task, whether or not the underlying read happened to fail too.
func TestAnalyzeImageReadFailureWithTruncatedWindowStillReportsCeiling(t *testing.T) {
	r := &readFailureReaderAt{err: errSimulatedDiskFailure}
	const size = 1024
	const maxBytes = 256 // smaller than size: windowTruncated must win over readFailed
	res := AnalyzeImage("png", r, size, maxBytes, 1<<30, nil)

	assertCheckStatus(t, res, CodeMetadataPresent, StatusUnknown)
	assertCheckReason(t, res, CodeMetadataPresent, ReasonWorkCeilingBytes)
}

// TestAnalyzeImageEmptyFileStaysCleanNotReadFailed pins the deliberate
// boundary this task draws: size<=0 means readWindow's own off>=size guard
// fires before any read is even attempted (n<=0/off>=size, not a real
// io.ReaderAt.ReadAt failure), so this is a degenerate-input fact, not an
// operational read failure. This preserves this scan's prior, already-
// exercised behavior for a genuinely empty upload (see
// TestAnalyzeImageNeverPanics's "empty png" case) rather than mislabeling a
// read that never happened as one that failed.
func TestAnalyzeImageEmptyFileStaysCleanNotReadFailed(t *testing.T) {
	res := AnalyzeImage("png", bytes.NewReader(nil), 0, 4096, 1<<30, nil)
	assertCheckStatus(t, res, CodeMetadataPresent, StatusClean)
}

// TestAnalyzeImagePNGTruncatedAtChunkBoundaryIsNotClean is PR9R16
// (image.go:426): a PNG cut immediately after a complete chunk lands on
// pos == len(b), which the walk used to accept as the format's natural end.
// It is not -- PNG's end is IEND -- and reporting clean there asserts that
// no metadata exists in chunks the file never carried.
func TestAnalyzeImagePNGTruncatedAtChunkBoundaryIsNotClean(t *testing.T) {
	full := pngFixture(10, 10)
	// Drop the trailing IEND chunk (12 bytes: length + type + CRC), leaving
	// a file that ends exactly at IHDR's own boundary.
	truncated := full[:len(full)-12]

	res := AnalyzeImage("png", bytes.NewReader(truncated), int64(len(truncated)), int64(len(truncated)), 1<<40, nil)
	c, ok := checkFor(res, CodeMetadataPresent)
	if !ok {
		t.Fatal("no Check for CodeMetadataPresent")
	}
	if c.Status == StatusClean {
		t.Fatalf("Check(metadata_present) = %+v, want a non-clean status -- this file stops mid-stream at a chunk edge with no IEND, so nothing was proven absent from the chunks it never carried", c)
	}
}

// TestAnalyzeImageDimensionReadFailureIsReadFailed is PR9R16 (image.go:39):
// an I/O failure during the dimension peek used to report malformed_input,
// blaming the uploaded image for this service's own disk -- while the
// metadata scan of the very same reader already reported read_failed.
func TestAnalyzeImageDimensionReadFailureIsReadFailed(t *testing.T) {
	res := AnalyzeImage("png", erroringReaderAtImg{}, 1024, 1024, 1<<40, nil)

	if got, _ := res.Characteristics["width_reason"].(string); got != string(ReasonReadFailed) {
		t.Errorf("width_reason = %q, want %q -- the reader failed; the image did not", got, ReasonReadFailed)
	}
	c, ok := checkFor(res, CodeImagePixelBombShape)
	if !ok {
		t.Fatal("no Check for CodeImagePixelBombShape")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonReadFailed {
		t.Errorf("Check(image_pixel_bomb_shape) = %+v, want unknown/read_failed", c)
	}
}

// erroringReaderAtImg always fails with a real, non-EOF error.
type erroringReaderAtImg struct{}

func (erroringReaderAtImg) ReadAt([]byte, int64) (int, error) {
	return 0, errors.New("simulated disk read failure")
}
