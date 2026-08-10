package inspect

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// ===== fixture builders =====

// pngFixture builds a minimal PNG signature + IHDR chunk declaring width x
// height (mirrors internal/convert/dimensions_test.go's pngFixture -- kept
// as a deliberate, local copy since that helper is unexported by its own
// package's test file and cannot be imported across a package boundary).
func pngFixture(width, height uint32) []byte {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	data = append(data, 0x00, 0x00, 0x00, 0x0D)
	data = append(data, []byte("IHDR")...)
	data = append(data, byte(width>>24), byte(width>>16), byte(width>>8), byte(width))
	data = append(data, byte(height>>24), byte(height>>16), byte(height>>8), byte(height))
	data = append(data, 0x08, 0x02, 0x00, 0x00, 0x00)
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
