package convert

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestSanitizeImageRemovesEXIFAndGPS is the SANI-02 proof, checked in the
// OUTPUT BYTES rather than through a metadata library: "physically absent"
// is the requirement, and a library that reports no EXIF could be reporting
// that it failed to find the block, not that the block is gone.
func TestSanitizeImageRemovesEXIFAndGPS(t *testing.T) {
	in := filepath.Join(sanitizeCorpusDir, "exif_gps.jpg")
	src, err := os.ReadFile(in)
	if err != nil {
		t.Fatalf("read fixture: %v -- committed by Plan 60-01", err)
	}
	for _, m := range []string{"Exif\x00\x00", "OctoConvCam", "FixtureModel X1"} {
		if !bytes.Contains(src, []byte(m)) {
			t.Fatalf("fixture lacks %q -- regenerate with scripts/sanitize-corpus.py", m)
		}
	}

	out := filepath.Join(t.TempDir(), "clean.jpg")
	res, err := SanitizeFile(context.Background(), "jpg", in, out)
	if err != nil {
		t.Fatalf("SanitizeFile: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []string{"Exif\x00\x00", "OctoConvCam", "FixtureModel X1"} {
		if bytes.Contains(got, []byte(m)) {
			t.Errorf("marker %q survived into the sanitized output", m)
		}
	}
	// The GPS IFD's own tag pointer (0x8825) is what makes the coordinates
	// reachable; checking for it separately means a future EXIF block written
	// without the camera-model strings is still caught.
	if bytes.Contains(got, []byte{0x88, 0x25}) {
		t.Error("the GPS IFD pointer tag survived into the sanitized output")
	}

	if len(res.Removed) != 1 || res.Removed[0] != ThreatDocumentMetadata {
		t.Errorf("Removed = %v, want exactly [document_metadata] -- an image container holds no actions, macros or attachments, and claiming otherwise would be false", res.Removed)
	}
}

// TestSanitizeImageStaysDecodable checks the file is still an image: same
// dimensions, decodes without error.
func TestSanitizeImageStaysDecodable(t *testing.T) {
	in := filepath.Join(sanitizeCorpusDir, "exif_gps.jpg")
	out := filepath.Join(t.TempDir(), "clean.jpg")
	if _, err := SanitizeFile(context.Background(), "jpg", in, out); err != nil {
		t.Fatalf("SanitizeFile: %v", err)
	}

	before, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	cfgBefore, _, err := image.DecodeConfig(before)
	if err != nil {
		t.Fatalf("decode input: %v", err)
	}

	after, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	cfgAfter, _, err := image.DecodeConfig(after)
	if err != nil {
		t.Fatalf("sanitized image no longer decodes: %v", err)
	}

	if cfgBefore.Width != cfgAfter.Width || cfgBefore.Height != cfgAfter.Height {
		t.Errorf("dimensions changed: %dx%d -> %dx%d",
			cfgBefore.Width, cfgBefore.Height, cfgAfter.Width, cfgAfter.Height)
	}
}

// TestSanitizeImageIsLossless backs the empty LostCapabilities list with
// evidence.
//
// The structural strip copies the entropy-coded scan data verbatim, so the
// compressed image bytes must be IDENTICAL before and after -- not merely
// visually similar, which is the most a re-encoding route could claim. If
// this ever fails, the empty LostCapabilities list has become a false
// statement and the report owes the client a quality-loss entry.
func TestSanitizeImageIsLossless(t *testing.T) {
	in := filepath.Join(sanitizeCorpusDir, "exif_gps.jpg")
	out := filepath.Join(t.TempDir(), "clean.jpg")
	res, err := SanitizeFile(context.Background(), "jpg", in, out)
	if err != nil {
		t.Fatalf("SanitizeFile: %v", err)
	}
	if len(res.LostCapabilities) != 0 {
		t.Fatalf("LostCapabilities = %v, want empty -- this test exists to back that claim", res.LostCapabilities)
	}

	src, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	// The scan data is everything from the SOS marker onward.
	scanOf := func(b []byte) []byte {
		if i := bytes.Index(b, []byte{0xFF, 0xDA}); i >= 0 {
			return b[i:]
		}
		return nil
	}
	sBefore, sAfter := scanOf(src), scanOf(got)
	if sBefore == nil || sAfter == nil {
		t.Fatal("no SOS marker found in one of the files")
	}
	if !bytes.Equal(sBefore, sAfter) {
		t.Errorf("compressed scan data changed (%d -> %d bytes) -- the strip is not lossless, and LostCapabilities must say so",
			len(sBefore), len(sAfter))
	}
}

// TestSanitizeImagePNGChunks covers the second container. A PNG carrying tEXt
// and eXIf must come out with the metadata chunks gone and IDAT untouched.
func TestSanitizeImagePNGChunks(t *testing.T) {
	src := buildPNGWithMetadata(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "meta.png")
	if err := os.WriteFile(in, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(src, []byte("tEXt")) || !bytes.Contains(src, []byte("eXIf")) {
		t.Fatal("test input lacks the metadata chunks it exists to carry")
	}

	out := filepath.Join(dir, "clean.png")
	if _, err := SanitizeFile(context.Background(), "png", in, out); err != nil {
		t.Fatalf("SanitizeFile: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []string{"tEXt", "eXIf", "SecretAuthor"} {
		if bytes.Contains(got, []byte(m)) {
			t.Errorf("%q survived into the sanitized PNG", m)
		}
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := image.DecodeConfig(f); err != nil {
		t.Errorf("sanitized PNG no longer decodes: %v", err)
	}
}

// buildPNGWithMetadata produces a minimal valid PNG carrying a tEXt and an
// eXIf chunk, by inserting them into a stdlib-encoded image.
func buildPNGWithMetadata(t *testing.T) []byte {
	t.Helper()

	var base bytes.Buffer
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&base, img); err != nil {
		t.Fatal(err)
	}
	raw := base.Bytes()

	chunk := func(ctype string, data []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
		b.WriteString(ctype)
		b.Write(data)
		crc := crc32.NewIEEE()
		crc.Write([]byte(ctype))
		crc.Write(data)
		_ = binary.Write(&b, binary.BigEndian, crc.Sum32())
		return b.Bytes()
	}

	// Insert after IHDR (signature 8 + length 4 + type 4 + data 13 + crc 4).
	at := 8 + 4 + 4 + 13 + 4
	var out bytes.Buffer
	out.Write(raw[:at])
	out.Write(chunk("tEXt", []byte("Author\x00SecretAuthor")))
	out.Write(chunk("eXIf", []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x00")))
	out.Write(raw[at:])
	return out.Bytes()
}
