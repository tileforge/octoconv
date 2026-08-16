package inspect

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// --- fixture helpers, mirroring internal/api/handlers_test.go's shape
// (that package's helpers cannot be imported -- different package) ---

func mustWriteZipEntry(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader(%q): %v", name, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("Write(%q): %v", name, err)
	}
}

func cleanDocxFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "[Content_Types].xml", "<Types/>")
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func macroDocxFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	mustWriteZipEntry(t, zw, "word/vbaProject.bin", "macro content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func externalRefDocxFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	mustWriteZipEntry(t, zw, "word/_rels/document.xml.rels",
		`<?xml version="1.0"?><Relationships><Relationship Id="rId1" Type="hyperlink" Target="http://example.com" TargetMode="External"/></Relationships>`)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// docxFixtureWithRels builds a minimal docx-shaped zip whose ONLY
// .rels-part content is relsXML -- used to test containsExternalRelationship
// against every legal XML serialization of TargetMode, not just the one
// externalRefDocxFixture above happens to use.
func docxFixtureWithRels(t *testing.T, relsXML string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	mustWriteZipEntry(t, zw, "word/_rels/document.xml.rels", relsXML)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// odtFixtureWithContent builds a minimal odt whose ONLY external-reference
// candidate part is content.xml, carrying contentXML verbatim -- mirrors
// docxFixtureWithRels's own shape for the OOXML side, used to exercise
// containsODFExternalHref against every legal XML serialization of an
// xlink:href attribute, not just odtFixtureWithTwoParts's own fixed marker.
func odtFixtureWithContent(t *testing.T, contentXML string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatalf("CreateHeader(mimetype): %v", err)
	}
	if _, err := mw.Write([]byte("application/vnd.oasis.opendocument.text")); err != nil {
		t.Fatalf("Write(mimetype): %v", err)
	}
	mustWriteZipEntry(t, zw, "content.xml", contentXML)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func duplicateRootPartDocxFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "first copy")
	mustWriteZipEntry(t, zw, "word/document.xml", "second copy, different content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// xlsxFixture returns a minimal xlsx with n <sheet/> elements in
// xl/workbook.xml.
func xlsxFixture(t *testing.T, n int) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	var sheets bytes.Buffer
	sheets.WriteString("<workbook><sheets>")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sheets, `<sheet name="Sheet%d" sheetId="%d" r:id="rId%d"/>`, i+1, i+1, i+1)
	}
	sheets.WriteString("</sheets></workbook>")
	mustWriteZipEntry(t, zw, "xl/workbook.xml", sheets.String())
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// zipBombDocxFixture returns a docx-shaped zip whose declared
// UncompressedSize64 equals declaredSize, written from real (highly
// compressible) content so the physical body stays tiny.
func zipBombDocxFixture(t *testing.T, declaredSize uint64) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "word/bigpart.xml", Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	chunk := make([]byte, 1<<20)
	var written uint64
	for written < declaredSize {
		n := declaredSize - written
		if n > uint64(len(chunk)) {
			n = uint64(len(chunk))
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			t.Fatalf("Write: %v", err)
		}
		written += n
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// manyEntriesFixture builds a docx-shaped zip with n tiny extra entries, all
// named so none of them are macro/external-ref/sheet-count candidates --
// used to prove this file's own scan ceilings, not the file's content,
// bound the work done against a maliciously enormous entry count.
func manyEntriesFixture(t *testing.T, n int) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	// A .rels entry placed AFTER the flood of filler entries below --
	// central-directory order is whatever archive/zip.Writer wrote them in
	// (creation order), so this proves the ceiling is hit before this
	// legitimate candidate is ever reached, rather than "happening" to be
	// found first.
	for i := 0; i < n; i++ {
		mustWriteZipEntry(t, zw, fmt.Sprintf("word/media/filler%d.bin", i), "x")
	}
	mustWriteZipEntry(t, zw, "word/_rels/document.xml.rels",
		`<Relationships><Relationship TargetMode="External" Target="http://example.com"/></Relationships>`)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// bareNonZipFixture is not a zip at all.
func bareNonZipFixture() []byte {
	return []byte("this is not a zip file at all, no PK signature anywhere in here")
}

// patchDeclaredUncompressedSize rewrites BOTH the local file header's and
// the central directory's "uncompressed size" field for the entry named
// targetName to newSize, byte-patching the raw zip -- archive/zip.Writer's
// normal CreateHeader API recomputes the true size from bytes actually
// written and offers no way to lie about it, so this test helper edits the
// already-written bytes directly. compressedSize (and the CRC32, and the
// actual local file data) are left untouched: the resulting zip is exactly
// as real archive/zip readers see it -- readable, CRC-valid content, with
// ONLY the declared UncompressedSize64 metadata field lying, precisely the
// attacker-controlled-metadata scenario T-58-18/T-58-20 exist to guard.
func patchDeclaredUncompressedSize(t *testing.T, data []byte, targetName string, newSize uint32) []byte {
	t.Helper()
	out := append([]byte{}, data...)
	patched := 0

	for i := 0; i+4 <= len(out); i++ {
		switch {
		case out[i] == 0x50 && out[i+1] == 0x4B && out[i+2] == 0x03 && out[i+3] == 0x04:
			if i+30 > len(out) {
				continue
			}
			nameLen := int(binary.LittleEndian.Uint16(out[i+26 : i+28]))
			if i+30+nameLen > len(out) {
				continue
			}
			if string(out[i+30:i+30+nameLen]) == targetName {
				binary.LittleEndian.PutUint32(out[i+22:i+26], newSize)
				patched++
			}
		case out[i] == 0x50 && out[i+1] == 0x4B && out[i+2] == 0x01 && out[i+3] == 0x02:
			if i+46 > len(out) {
				continue
			}
			nameLen := int(binary.LittleEndian.Uint16(out[i+28 : i+30]))
			if i+46+nameLen > len(out) {
				continue
			}
			if string(out[i+46:i+46+nameLen]) == targetName {
				binary.LittleEndian.PutUint32(out[i+24:i+28], newSize)
				patched++
			}
		}
	}
	if patched != 2 {
		t.Fatalf("patchDeclaredUncompressedSize(%q): patched %d header(s), want 2 (local + central)", targetName, patched)
	}
	return out
}

// corruptCompressionMethod byte-patches BOTH the local file header's and the
// central directory's "compression method" field for the entry named
// targetName to an unregistered method code (99) -- mirrors
// patchDeclaredUncompressedSize's technique (archive/zip.Writer offers no
// API to write an invalid method, so the already-written bytes are edited
// directly). zip.NewReader itself does not validate the method (it only
// parses central-directory metadata), but File.Open looks up a registered
// decompressor for it and returns an error for an unrecognized one --
// exactly the "candidate part this scan cannot even open" case codex PR9
// round 2 (container.go:334) describes, without needing to fabricate a
// genuinely corrupt DEFLATE stream.
func corruptCompressionMethod(t *testing.T, data []byte, targetName string) []byte {
	t.Helper()
	out := append([]byte{}, data...)
	patched := 0

	for i := 0; i+4 <= len(out); i++ {
		switch {
		case out[i] == 0x50 && out[i+1] == 0x4B && out[i+2] == 0x03 && out[i+3] == 0x04:
			if i+30 > len(out) {
				continue
			}
			nameLen := int(binary.LittleEndian.Uint16(out[i+26 : i+28]))
			if i+30+nameLen > len(out) {
				continue
			}
			if string(out[i+30:i+30+nameLen]) == targetName {
				binary.LittleEndian.PutUint16(out[i+8:i+10], 99)
				patched++
			}
		case out[i] == 0x50 && out[i+1] == 0x4B && out[i+2] == 0x01 && out[i+3] == 0x02:
			if i+46 > len(out) {
				continue
			}
			nameLen := int(binary.LittleEndian.Uint16(out[i+28 : i+30]))
			if i+46+nameLen > len(out) {
				continue
			}
			if string(out[i+46:i+46+nameLen]) == targetName {
				binary.LittleEndian.PutUint16(out[i+10:i+12], 99)
				patched++
			}
		}
	}
	if patched != 2 {
		t.Fatalf("corruptCompressionMethod(%q): patched %d header(s), want 2 (local + central)", targetName, patched)
	}
	return out
}

// corruptCompressedData overwrites the RAW compressed bytes of the entry
// named targetName with 0xFF -- unlike corruptCompressionMethod, this leaves
// the DEFLATE method intact, so f.Open succeeds and returns a flate.Reader,
// but reading from it fails partway through with a genuine "flate: corrupt
// input" error. This is codex PR9 round 2's OTHER container.go:334 case:
// io.ReadAll's error was previously discarded even though f.Open succeeded.
//
// zip.Writer streams its output (it does not know the final compressed size
// until Close), so the LOCAL file header's own compressedSize field is left
// at 0 (bit 3 of the flags field marks "sizes follow in a trailing data
// descriptor instead"). compSize must therefore come from the CENTRAL
// DIRECTORY entry, which IS written with the real, final size -- reading it
// from the local header (as an earlier draft of this helper did) computes
// an empty [dataStart:dataStart] range and silently corrupts nothing.
func corruptCompressedData(t *testing.T, data []byte, targetName string) []byte {
	t.Helper()
	out := append([]byte{}, data...)

	var compSize int
	foundCentral := false
	for i := 0; i+4 <= len(out); i++ {
		if !(out[i] == 0x50 && out[i+1] == 0x4B && out[i+2] == 0x01 && out[i+3] == 0x02) {
			continue
		}
		if i+46 > len(out) {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(out[i+28 : i+30]))
		if i+46+nameLen > len(out) || string(out[i+46:i+46+nameLen]) != targetName {
			continue
		}
		compSize = int(binary.LittleEndian.Uint32(out[i+20 : i+24]))
		foundCentral = true
		break
	}
	if !foundCentral {
		t.Fatalf("corruptCompressedData(%q): central directory entry not found", targetName)
	}

	for i := 0; i+4 <= len(out); i++ {
		if !(out[i] == 0x50 && out[i+1] == 0x4B && out[i+2] == 0x03 && out[i+3] == 0x04) {
			continue
		}
		if i+30 > len(out) {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(out[i+26 : i+28]))
		extraLen := int(binary.LittleEndian.Uint16(out[i+28 : i+30]))
		if i+30+nameLen > len(out) || string(out[i+30:i+30+nameLen]) != targetName {
			continue
		}
		dataStart := i + 30 + nameLen + extraLen
		dataEnd := dataStart + compSize
		if dataEnd > len(out) || compSize == 0 {
			t.Fatalf("corruptCompressedData(%q): computed data range [%d:%d) (compSize=%d from central directory) invalid against buffer length %d", targetName, dataStart, dataEnd, compSize, len(out))
		}
		for j := dataStart; j < dataEnd; j++ {
			out[j] = 0xFF
		}
		return out
	}
	t.Fatalf("corruptCompressedData(%q): local file header not found", targetName)
	return nil
}

// --- assertion helpers shared with pdf_test.go's checkFor/findingFor ---

// --- <behavior> coverage ---

func TestAnalyzeContainerMacroFindingLocation(t *testing.T) {
	data := macroDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	f, ok := findingFor(res, CodeOOXMLMacro)
	if !ok {
		t.Fatal("no Finding for CodeOOXMLMacro")
	}
	if f.Location != "word/vbaProject.bin" {
		t.Fatalf("Finding(CodeOOXMLMacro).Location = %q, want %q", f.Location, "word/vbaProject.bin")
	}
}

func TestAnalyzeContainerBombThresholdIndependentOfRejectThreshold(t *testing.T) {
	const bombThreshold = 1_000_000
	const hypotheticalHardRejectThreshold = 10_000_000 // a stand-in for handleCreateJob's own, larger, separate threshold

	data := zipBombDocxFixture(t, 5_000_000) // strictly between the two
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), bombThreshold, nil)

	if _, ok := findingFor(res, CodeZipBombShape); !ok {
		t.Fatal("no Finding for CodeZipBombShape for a size between the findings threshold and a hypothetical hard-reject threshold")
	}
	if 5_000_000 >= hypotheticalHardRejectThreshold {
		t.Fatal("test setup error: declared size must stay below the hypothetical hard-reject threshold")
	}
}

func TestAnalyzeContainerSheetCountXLSX(t *testing.T) {
	data := xlsxFixture(t, 3)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if sc, ok := res.Characteristics["sheet_count"]; !ok || sc != 3 {
		t.Fatalf("Characteristics[sheet_count] = %v (ok=%v), want 3", sc, ok)
	}
}

// TestAnalyzeContainerSheetCountDeclaredSizeSkipIsWorkCeilingNotMalformed is
// an audit finding applying scanExternalReferences's own already-fixed
// discipline to extractXLSXSheetCount, which neither review round named:
// workbook.xml being skipped by its own (lying) declared size is THIS
// scan's refusal to read the part, not evidence the workbook has no sheets
// -- sheet_count_reason must be work_ceiling_bytes, never malformed_input.
func TestAnalyzeContainerSheetCountDeclaredSizeSkipIsWorkCeilingNotMalformed(t *testing.T) {
	data := xlsxFixture(t, 3)
	patched := patchDeclaredUncompressedSize(t, data, "xl/workbook.xml", 1<<30)

	res := AnalyzeContainer(bytes.NewReader(patched), int64(len(patched)), int64(len(patched)), 1<<30, nil)

	if res.Characteristics["sheet_count"] != nil {
		t.Fatalf("sheet_count = %#v, want nil -- workbook.xml was skipped by its own lying declared size", res.Characteristics["sheet_count"])
	}
	if reason, _ := res.Characteristics["sheet_count_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("sheet_count_reason = %q, want %q -- this scan's own declared-size skip, not the file's structure, decided this miss", reason, ReasonWorkCeilingBytes)
	}
}

// TestAnalyzeContainerSheetCountLimitReaderBoundaryIsWorkCeilingNotComplete
// is the container.go:497 finding, verbatim: "For a ZIP entry declaring
// exactly maxRelsPartBytes but decompressing to more data, io.LimitReader
// returns exactly the allowed prefix with no error because it never asks
// the ZIP reader for the next byte. This function then reports the sheet
// count from that prefix as complete, so sheets located after 256 KiB are
// silently omitted; read one extra byte or otherwise verify EOF before
// returning ok=true."
//
// workbook.xml's REAL content is far larger than maxRelsPartBytes (10000
// <sheet/> elements), but its declared UncompressedSize64 is byte-patched
// down to EXACTLY maxRelsPartBytes -- passing the earlier
// "f.UncompressedSize64 > maxRelsPartBytes" skip (it is not GREATER, only
// equal) and reaching the io.LimitReader boundary itself.
func TestAnalyzeContainerSheetCountLimitReaderBoundaryIsWorkCeilingNotComplete(t *testing.T) {
	data := xlsxFixture(t, 10000) // real workbook.xml content, comfortably over maxRelsPartBytes (256 KiB)
	patched := patchDeclaredUncompressedSize(t, data, "xl/workbook.xml", uint32(maxRelsPartBytes))

	res := AnalyzeContainer(bytes.NewReader(patched), int64(len(patched)), int64(len(patched)), 1<<30, nil)

	if res.Characteristics["sheet_count"] != nil {
		t.Fatalf("sheet_count = %#v, want nil -- the part's real content runs past maxRelsPartBytes even though it DECLARES exactly maxRelsPartBytes; a count computed from the truncated prefix would silently omit sheets past the boundary", res.Characteristics["sheet_count"])
	}
	if reason, _ := res.Characteristics["sheet_count_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("sheet_count_reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
}

func TestAnalyzeContainerPageCountKeyAbsentForSpreadsheets(t *testing.T) {
	data := xlsxFixture(t, 1)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if _, ok := res.Characteristics["page_count"]; ok {
		t.Fatalf("Characteristics contains page_count for xlsx, want absent entirely")
	}
}

func TestAnalyzeContainerDocxSheetCountAbsentPageCountUnknown(t *testing.T) {
	data := cleanDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if _, ok := res.Characteristics["sheet_count"]; ok {
		t.Fatal("Characteristics contains sheet_count for docx, want absent")
	}
	pc, ok := res.Characteristics["page_count"]
	if !ok || pc != nil {
		t.Fatalf("Characteristics[page_count] = %v (ok=%v), want present and nil", pc, ok)
	}
	reason, ok := res.Characteristics["page_count_reason"]
	if !ok || reason != string(ReasonParserUnavailable) {
		t.Fatalf("Characteristics[page_count_reason] = %v (ok=%v), want %q", reason, ok, ReasonParserUnavailable)
	}
}

func TestAnalyzeContainerExternalRefLocationIsPartPath(t *testing.T) {
	data := externalRefDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	f, ok := findingFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Finding for CodeExternalReference")
	}
	if f.Location != "word/_rels/document.xml.rels" {
		t.Fatalf("Finding(CodeExternalReference).Location = %q, want %q", f.Location, "word/_rels/document.xml.rels")
	}
}

// TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization is the
// PR9R4 finding (container.go:414) verbatim, both directions: single quotes
// / whitespace around '=' must still be recognized as an external
// reference, and the original double-quoted/unspaced serialization
// externalRefDocxFixture already covers must keep working unchanged.
func TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization(t *testing.T) {
	cases := []struct {
		name string
		rels string
	}{
		{
			"single-quoted, spaces around equals",
			`<?xml version="1.0"?><Relationships><Relationship Id="rId1" Type="hyperlink" Target="http://example.com" TargetMode = 'External'/></Relationships>`,
		},
		{
			"double-quoted, no spaces (original serialization)",
			`<?xml version="1.0"?><Relationships><Relationship Id="rId1" Type="hyperlink" Target="http://example.com" TargetMode="External"/></Relationships>`,
		},
		{
			"attribute order reversed",
			`<?xml version="1.0"?><Relationships><Relationship TargetMode="External" Target="http://example.com" Type="hyperlink" Id="rId1"/></Relationships>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := docxFixtureWithRels(t, tc.rels)
			res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
			f, ok := findingFor(res, CodeExternalReference)
			if !ok {
				c, _ := checkFor(res, CodeExternalReference)
				t.Fatalf("no Finding for CodeExternalReference (got Check %+v instead) -- this serialization must NOT reach a clean verdict", c)
			}
			if f.Location != "word/_rels/document.xml.rels" {
				t.Fatalf("Finding(CodeExternalReference).Location = %q, want %q", f.Location, "word/_rels/document.xml.rels")
			}
		})
	}
}

// TestAnalyzeContainerExternalRefInternalRelationshipStaysClean proves the
// fix is not simply "any Relationship element is now a finding" -- a
// .rels part with no TargetMode="External" anywhere (an ordinary internal
// relationship, TargetMode absent, which OPC defines as defaulting to
// Internal) must still report clean.
func TestAnalyzeContainerExternalRefInternalRelationshipStaysClean(t *testing.T) {
	data := docxFixtureWithRels(t, `<?xml version="1.0"?><Relationships><Relationship Id="rId1" Type="styles" Target="styles.xml"/></Relationships>`)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status != StatusClean {
		t.Fatalf("Check(CodeExternalReference).Status = %q, want %q -- a relationship with no TargetMode=\"External\" anywhere must not be flagged", c.Status, StatusClean)
	}
}

// TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization is
// the PR9R5 finding (container.go:419) verbatim: valid XML such as
// `xlink:href = 'https://example.com'` (single quotes, whitespace around
// '=') or an equivalent attribute bound to the XLink namespace through a
// DIFFERENT prefix must still be recognized -- neither is the exact byte
// sequence the previous byte-comparison implementation matched.
func TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			"single-quoted, spaces around equals, conventional xlink prefix",
			`<office:document-content xmlns:xlink="http://www.w3.org/1999/xlink"><text:a xlink:href = 'https://example.com'/></office:document-content>`,
		},
		{
			"double-quoted, no spaces (original serialization)",
			`<office:document-content xmlns:xlink="http://www.w3.org/1999/xlink"><text:a xlink:href="https://example.com"/></office:document-content>`,
		},
		{
			"XLink namespace bound to a DIFFERENT prefix",
			`<office:document-content xmlns:ns7="http://www.w3.org/1999/xlink"><text:a ns7:href="https://example.com"/></office:document-content>`,
		},
		{
			// PR9R5-SWEEP-CLASSES: found by sweeping the class INSIDE the
			// fix written for the class. encoding/xml only resolves
			// Name.Space to the XLink URI when the document actually bound
			// the prefix; leaving xmlns:xlink undeclared yields the literal
			// Space=="xlink" instead, which a URI-only comparison misses --
			// the same false-clean the semantic parse was written to close,
			// one serialization further out. The document's own bytes still
			// say `xlink:href`, and a lenient ODF importer keying off the
			// qualified name resolves it.
			"xlink prefix used but never declared",
			`<office:document-content><text:a xlink:href="https://example.com"/></office:document-content>`,
		},
		{
			// A URI scheme is case-insensitive (RFC 3986 3.1), so this is
			// the same external target spelled legally another way.
			"uppercase scheme",
			`<office:document-content xmlns:xlink="http://www.w3.org/1999/xlink"><text:a xlink:href="HTTPS://example.com"/></office:document-content>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := odtFixtureWithContent(t, tc.content)
			res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
			f, ok := findingFor(res, CodeExternalReference)
			if !ok {
				c, _ := checkFor(res, CodeExternalReference)
				t.Fatalf("no Finding for CodeExternalReference (got Check %+v instead) -- this serialization must NOT reach a clean verdict", c)
			}
			if f.Location != "content.xml" {
				t.Fatalf("Finding(CodeExternalReference).Location = %q, want %q", f.Location, "content.xml")
			}
		})
	}
}

// TestAnalyzeContainerODFInternalHrefStaysClean proves the fix is not
// simply "any xlink:href is now a finding" -- an href with no
// external-scheme prefix (an internal document fragment reference, the
// conventional ODF use of xlink:href for cross-references within the same
// package) must still report clean.
func TestAnalyzeContainerODFInternalHrefStaysClean(t *testing.T) {
	data := odtFixtureWithContent(t, `<office:document-content xmlns:xlink="http://www.w3.org/1999/xlink"><text:a xlink:href="#Table1"/></office:document-content>`)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status != StatusClean {
		t.Fatalf("Check(CodeExternalReference).Status = %q, want %q -- an internal fragment href must not be flagged", c.Status, StatusClean)
	}
}

// TestAnalyzeContainerODFHrefWithoutNamespaceBindingStaysClean proves
// containsODFExternalHref matches the RESOLVED namespace, not merely the
// local attribute name "href" -- an unprefixed href attribute (no
// namespace at all, per XML Namespaces: attributes without a prefix are
// NEVER in any namespace, unlike elements, which inherit a default
// namespace) must not be treated as XLink's own href.
func TestAnalyzeContainerODFHrefWithoutNamespaceBindingStaysClean(t *testing.T) {
	data := odtFixtureWithContent(t, `<office:document-content><text:a href="http://example.com"/></office:document-content>`)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status != StatusClean {
		t.Fatalf("Check(CodeExternalReference).Status = %q, want %q -- an href attribute with no namespace binding is not XLink's own href", c.Status, StatusClean)
	}
}

// TestAnalyzeContainerMalformedRelsPartYieldsUnknownMalformedNotCeiling is
// the PR9R5 finding (container.go:462) verbatim: a fully read, genuinely
// malformed .rels part (never truncated by any of this scan's own
// ceilings) must report unknown/malformed_input, distinct from
// unknown/work_ceiling_bytes -- a decode failure is a fact about the FILE,
// not about this scan's own resource limits. The fixture's declared and
// real sizes match exactly (mustWriteZipEntry writes the exact string
// below, and archive/zip.Writer computes UncompressedSize64 from the bytes
// actually written) -- this part is read to completion, never truncated by
// f.UncompressedSize64's own declared-size skip or by io.LimitReader.
func TestAnalyzeContainerMalformedRelsPartYieldsUnknownMalformedNotCeiling(t *testing.T) {
	data := docxFixtureWithRels(t, `<Relationships><Relationship Id="rId1" TargetMode="External"`) // truncated mid-tag: never closed, never a complete document
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v for a part that never successfully parsed", f)
	}
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonMalformedInput {
		t.Fatalf("Check(CodeExternalReference) = %+v, want unknown/malformed_input -- the part was read to completion (declared size == real size, budget is 1<<30) and its XML genuinely failed to parse; this must not be reported the same way a ceiling hit is", c)
	}
}

// TestAnalyzeContainerMalformedODFPartYieldsUnknownMalformedNotCeiling is
// TestAnalyzeContainerMalformedRelsPartYieldsUnknownMalformedNotCeiling's
// ODF/epub sibling -- the SAME PR9R5 finding applies to
// containsODFExternalHref exactly as it does to
// containsExternalRelationship.
func TestAnalyzeContainerMalformedODFPartYieldsUnknownMalformedNotCeiling(t *testing.T) {
	data := odtFixtureWithContent(t, `<office:document-content xmlns:xlink="http://www.w3.org/1999/xlink"><text:a xlink:href="http://example.com"`) // truncated mid-tag
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v for a part that never successfully parsed", f)
	}
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonMalformedInput {
		t.Fatalf("Check(CodeExternalReference) = %+v, want unknown/malformed_input -- the part was read to completion (declared size == real size, budget is 1<<30) and its XML genuinely failed to parse; this must not be reported the same way a ceiling hit is", c)
	}
}

func TestAnalyzeContainerCleanDocxAllClean(t *testing.T) {
	data := cleanDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	for _, code := range containerDomainCodes {
		c, ok := checkFor(res, code)
		if !ok {
			t.Fatalf("no Check for code %q", code)
		}
		if c.Status != StatusClean {
			t.Fatalf("Check(%q).Status = %q, want %q", code, c.Status, StatusClean)
		}
	}
}

func TestAnalyzeContainerDuplicateRootPartAllUnknown(t *testing.T) {
	data := duplicateRootPartDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	for _, code := range containerDomainCodes {
		c, ok := checkFor(res, code)
		if !ok {
			t.Fatalf("no Check for code %q", code)
		}
		if c.Status == StatusClean {
			t.Fatalf("Check(%q).Status = clean, want unknown for a DuplicateRootPart container", code)
		}
		if c.Status != StatusUnknown || c.Reason != ReasonMalformedInput {
			t.Fatalf("Check(%q) = %+v, want unknown/malformed_input", code, c)
		}
	}
}

func TestAnalyzeContainerNotAZipNotApplicable(t *testing.T) {
	data := bareNonZipFixture()
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	for _, code := range containerDomainCodes {
		c, ok := checkFor(res, code)
		if !ok {
			t.Fatalf("no Check for code %q", code)
		}
		if c.Status != StatusNotApplicable || c.Reason != ReasonFormatNotApplicable {
			t.Fatalf("Check(%q) = %+v, want not_applicable/format_not_applicable", code, c)
		}
	}
}

// TestAnalyzeContainerCeilingYieldsUnknownNotClean proves that exceeding
// this file's own scan ceiling (maxZipEntriesScanned) degrades the
// external-reference check to unknown, never clean -- the container-family
// analog of T-58-16.
func TestAnalyzeContainerCeilingYieldsUnknownNotClean(t *testing.T) {
	data := manyEntriesFixture(t, maxZipEntriesScanned+500)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)

	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		if _, isFinding := findingFor(res, CodeExternalReference); isFinding {
			t.Fatal("got a Finding for CodeExternalReference -- want the entry-count ceiling to be hit before the legitimate .rels candidate placed after the filler entries is ever reached")
		}
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status == StatusClean {
		t.Fatal("Check(CodeExternalReference).Status = clean, want unknown -- the entry-scan ceiling was exceeded, so clean is not an honest answer")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Check(CodeExternalReference) = %+v, want unknown/work_ceiling_bytes", c)
	}
}

// TestAnalyzeContainerNeverAllocatesOnDeclaredSize proves the declared
// UncompressedSize64 field is used ONLY to skip a part early, never to
// allocate or otherwise trust its content -- a part whose declared size
// lies (claims huge, actual bytes tiny AND actually contain a real
// external reference) must never surface a Finding, because this scan
// skips it by declared size alone before ever opening it.
//
// codex PR9 finding 4 / T-SLE-04: this test used to ALSO assert that the
// resulting Check was StatusClean -- which froze exactly the false-absence
// claim the finding describes. Skipping the container's ONLY external-ref
// candidate by declared size is a refusal to look, not a clean read of it;
// the "part was never opened" assertion below stays (it is real and
// valuable), but the verdict this scan reports for having skipped its one
// candidate must be unknown/work_ceiling_bytes, never clean.
func TestAnalyzeContainerNeverAllocatesOnDeclaredSize(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	// The actual content below genuinely contains TargetMode="External" --
	// if this scan ever opened and read it, it WOULD find the marker.
	mustWriteZipEntry(t, zw, "word/_rels/document.xml.rels", `TargetMode="External"`)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	data := buf.Bytes()

	// Lie about this part's declared uncompressed size: far above
	// maxRelsPartBytes, even though the real bytes above are tiny.
	patched := patchDeclaredUncompressedSize(t, data, "word/_rels/document.xml.rels", 1<<30)

	res := AnalyzeContainer(bytes.NewReader(patched), int64(len(patched)), int64(len(patched)), 1<<30, nil)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v for a part that should have been skipped by its (lying) declared size -- this scan must never trust declared size enough to open a part that claims to exceed maxRelsPartBytes", f)
	}
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status == StatusClean {
		t.Fatalf("Check(CodeExternalReference).Status = clean, want unknown -- the only candidate part was skipped by its own declared size (this scan's own ceiling), so clean is not an honest answer about the file")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Check(CodeExternalReference) = %+v, want unknown/work_ceiling_bytes -- the only candidate part was never opened because its declared size exceeded this scan's own ceiling", c)
	}
}

// TestAnalyzeContainerUnopenableRelsPartYieldsUnknownNotClean is codex PR9
// round 2 (container.go:334): a candidate .rels part this scan cannot even
// OPEN (here: an unsupported compression method) must never be silently
// treated as "found nothing" -- if it is the container's only relevant
// part, that would report the whole file clean having inspected none of
// its content, the exact mistake the declared-size-skip case (T-SLE-04)
// already closed one step earlier in this same loop.
func TestAnalyzeContainerUnopenableRelsPartYieldsUnknownNotClean(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	// The actual content below genuinely contains TargetMode="External" --
	// if this scan ever successfully opened and read it, it WOULD find the
	// marker; corruptCompressionMethod below is what prevents that.
	mustWriteZipEntry(t, zw, "word/_rels/document.xml.rels", `TargetMode="External"`)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	data := buf.Bytes()
	corrupted := corruptCompressionMethod(t, data, "word/_rels/document.xml.rels")

	res := AnalyzeContainer(bytes.NewReader(corrupted), int64(len(corrupted)), int64(len(corrupted)), 1<<30, nil)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v for a part this scan could not even open (unsupported compression method)", f)
	}
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status == StatusClean {
		t.Fatal("Check(CodeExternalReference).Status = clean, want unknown -- the only candidate part could not be opened, so clean is not an honest answer about the file")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Check(CodeExternalReference) = %+v, want unknown/work_ceiling_bytes -- the only candidate part was never successfully read because f.Open failed", c)
	}
}

// TestAnalyzeContainerReadErrorMidPartYieldsUnknownNotClean is codex PR9
// round 2's other container.go:334 case: f.Open succeeds, but the read
// itself fails partway through (corrupted DEFLATE stream) -- io.ReadAll's
// error must not be silently discarded, or a container whose only relevant
// part has a corrupted compressed body would report external_reference
// clean having read none of its real, decompressed content.
func TestAnalyzeContainerReadErrorMidPartYieldsUnknownNotClean(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	// A long, genuinely compressible payload -- big enough that overwriting
	// its compressed bytes with 0xFF reliably breaks the DEFLATE stream
	// rather than accidentally decoding to something error-free.
	relsContent := strings.Repeat("no external reference marker here ", 200) + `TargetMode="External"`
	mustWriteZipEntry(t, zw, "word/_rels/document.xml.rels", relsContent)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	data := buf.Bytes()
	corrupted := corruptCompressedData(t, data, "word/_rels/document.xml.rels")

	res := AnalyzeContainer(bytes.NewReader(corrupted), int64(len(corrupted)), int64(len(corrupted)), 1<<30, nil)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v for a part whose compressed body was corrupted mid-read", f)
	}
	c, ok := checkFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Check for CodeExternalReference")
	}
	if c.Status == StatusClean {
		t.Fatal("Check(CodeExternalReference).Status = clean, want unknown -- the only candidate part's read failed partway through, so clean is not an honest answer")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Check(CodeExternalReference) = %+v, want unknown/work_ceiling_bytes -- io.ReadAll's error must not be silently discarded", c)
	}
}

// odtFixtureWithTwoParts builds a minimal, entirely HONEST odt (no forged
// metadata anywhere -- every declared size matches its real content
// exactly) with two external-reference candidate parts: content.xml (small,
// no marker) and styles.xml (moderate size, genuinely valid XML with an
// xlink:href external marker placed near its tail, padded via an XML
// comment so containsODFExternalHref's semantic parse -- not a byte
// comparison -- is what has to reach the marker). ODF's TWO fixed candidate
// names (content.xml, styles.xml) are exactly what lets a single-format
// test exercise a MULTI-PART scan, unlike OOXML's single .rels convention.
func odtFixtureWithTwoParts(t *testing.T, stylesSize int) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	mw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatalf("CreateHeader(mimetype): %v", err)
	}
	if _, err := mw.Write([]byte("application/vnd.oasis.opendocument.text")); err != nil {
		t.Fatalf("Write(mimetype): %v", err)
	}

	mustWriteZipEntry(t, zw, "content.xml", "small content, no external reference here at all")

	prefix := []byte(`<office:document-styles xmlns:xlink="http://www.w3.org/1999/xlink"><office:body><!--`)
	suffix := []byte(`--><text:a xlink:href="http://example.com"/></office:body></office:document-styles>`)
	padLen := stylesSize - len(prefix) - len(suffix)
	if padLen < 0 {
		padLen = 0
	}
	styles := append(append(append([]byte{}, prefix...), bytes.Repeat([]byte("A"), padLen)...), suffix...)
	mustWriteZipEntry(t, zw, "styles.xml", string(styles))

	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// TestAnalyzeContainerLimitReaderBoundsCrossPartBudget proves two things
// together: (1) io.LimitReader (fed the REMAINING per-request byte budget,
// not just the static maxRelsPartBytes cap) is what stops this scan from
// reading an external marker placed near the tail of a SECOND, genuinely
// and honestly small part once an earlier part has already spent most of
// the request's budget, and (2) a read cut short that way is reported as
// unknown/work_ceiling_bytes, never clean -- exactly the D-02a discipline
// applied to this family: a truncated read is not evidence of absence.
// Both parts here have entirely truthful declared sizes -- this is
// deliberately NOT the "declared size lies" scenario (that one is already
// covered by TestAnalyzeContainerNeverAllocatesOnDeclaredSize and, as it
// turns out, is independently blocked by archive/zip's own checksumReader,
// which refuses to return more bytes than a file's declared
// UncompressedSize64 regardless of this package's own code). The
// cross-part budget is a fact ONLY this scan's own bookkeeping enforces --
// the stdlib zip reader has no notion of a budget shared across multiple
// files in one request.
func TestAnalyzeContainerLimitReaderBoundsCrossPartBudget(t *testing.T) {
	const stylesSize = 5000
	data := odtFixtureWithTwoParts(t, stylesSize)

	// A tight per-request budget: enough to read content.xml in full (a
	// few dozen bytes) plus only a small prefix of styles.xml -- nowhere
	// near enough to reach the marker near its tail.
	const tightBudget = 100
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), tightBudget, 1<<30, nil)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v with a %d-byte total budget -- io.LimitReader must stop the styles.xml read long before its tail-placed marker, using the budget REMAINING after content.xml was already read, not a fresh maxRelsPartBytes allowance per part", f, tightBudget)
	}
	// The styles.xml read was genuinely cut short by the exhausted budget
	// (its real content is far longer than what this scan's budget allowed
	// it to see) -- clean would be a FALSE claim of completeness here,
	// exactly the D-02a mistake applied to this family: a truncated read
	// must report unknown/work_ceiling_bytes, never clean.
	c, ok := checkFor(res, CodeExternalReference)
	if !ok || c.Status != StatusUnknown || c.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Check(CodeExternalReference) = %+v (ok=%v), want unknown/work_ceiling_bytes -- the styles.xml read was truncated by the exhausted budget, so clean would overclaim completeness", c, ok)
	}

	// Sanity check on the fixture itself: with a GENEROUS budget, the exact
	// same bytes DO yield a Finding -- proving the marker is real and
	// reachable, so the tight-budget case above is actually exercising the
	// cross-part budget, not a fixture that could never match at all.
	// NOTE: the budget is counted in DECOMPRESSED bytes read (budgetLeft
	// tracks len(data) after decompression), so "generous" must be sized
	// against the entries' real uncompressed content, not against
	// len(data) (the compressed FILE's on-disk size) -- styles.xml's
	// highly-repetitive filler compresses extremely well, so the whole
	// fixture's compressed size is actually much SMALLER than its own
	// uncompressed styles.xml content alone.
	resGenerous := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), 1<<20, 1<<30, nil)
	if _, ok := findingFor(resGenerous, CodeExternalReference); !ok {
		t.Fatal("fixture sanity check failed: no Finding for CodeExternalReference even with a generous budget -- the marker must be reachable for the tight-budget assertion above to mean anything")
	}
}

func TestAnalyzeContainerNeverPanics(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"tiny garbage", []byte("x")},
		{"truncated zip", bytes.Repeat([]byte{0x50, 0x4B, 0x03, 0x04}, 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("AnalyzeContainer panicked: %v", p)
				}
			}()
			AnalyzeContainer(bytes.NewReader(tc.data), int64(len(tc.data)), int64(len(tc.data)), 1<<30, nil)
		})
	}
}

// TestAnalyzeContainerWithMatchesAnalyzeContainer guards the split introduced
// when the API stopped sniffing the same archive twice (codex PR #9 finding
// 5): AnalyzeContainer now sniffs and delegates to AnalyzeContainerWith, and
// the API calls AnalyzeContainerWith directly with detection's own result.
//
// The whole point of the deduplication was that two independent sniffs of one
// upload could in principle disagree with nothing noticing. A split that
// itself drifts would reintroduce exactly that, one layer up -- so pin the two
// entry points to the same answer on the same bytes.
func TestAnalyzeContainerWithMatchesAnalyzeContainer(t *testing.T) {
	fixtures := map[string][]byte{
		"macro docx": macroDocxFixture(t),
		"plain xlsx": xlsxFixture(t, 3),
	}

	for name, data := range fixtures {
		t.Run(name, func(t *testing.T) {
			r := bytes.NewReader(data)
			size := int64(len(data))

			viaSniff := AnalyzeContainer(r, size, size, 1<<30, nil)

			cr, err := convert.SniffContainer(r, size)
			if err != nil {
				t.Fatalf("SniffContainer: %v", err)
			}
			viaHandoff := AnalyzeContainerWith(cr, r, size, size, 1<<30, nil)

			if !reflect.DeepEqual(viaSniff, viaHandoff) {
				t.Fatalf("AnalyzeContainer and AnalyzeContainerWith disagree on identical bytes -- the deduplicated API path would report something different from the self-sniffing path\nAnalyzeContainer:     %+v\nAnalyzeContainerWith: %+v", viaSniff, viaHandoff)
			}
		})
	}
}

// TestAnalyzeContainerReadFailureIsReadFailedNotNotApplicable is round 6's
// neighbour-audit fix at the container layer. An io.ReaderAt that fails is
// not evidence that the container-domain checks do not apply -- that is a
// conclusion, and an unreadable upload gives no grounds for one. Before this
// fix the result was StatusNotApplicable, which reads as "we looked and
// these questions are irrelevant to this file".
//
// Reaching this at all required a fix one layer down: convert.SniffContainer
// collapsed EVERY zip.NewReader failure, read errors included, into
// ErrNotAZip, so this distinction was unavailable to callers no matter how
// carefully they branched on it.
func TestAnalyzeContainerReadFailureIsReadFailedNotNotApplicable(t *testing.T) {
	res := AnalyzeContainer(erroringReaderAtC{}, 1024, 1024, 1<<30, nil)
	for _, code := range containerDomainCodes {
		c, ok := checkFor(res, code)
		if !ok {
			t.Fatalf("no Check for code %q", code)
		}
		if c.Status != StatusUnknown || c.Reason != ReasonReadFailed {
			t.Errorf("Check(%q) = %+v, want unknown/read_failed -- a failed read is not a verdict that these checks are not applicable", code, c)
		}
	}
}

// TestAnalyzeContainerNonZipStaysNotApplicable is the other half: a genuine
// non-zip input IS a fact about the upload, and not_applicable remains the
// right answer for it. Without this test the fix above could be satisfied by
// reporting read_failed for everything.
func TestAnalyzeContainerNonZipStaysNotApplicable(t *testing.T) {
	data := []byte("this is definitely not a zip archive, not even slightly")
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	for _, code := range containerDomainCodes {
		c, ok := checkFor(res, code)
		if !ok {
			t.Fatalf("no Check for code %q", code)
		}
		if c.Status != StatusNotApplicable {
			t.Errorf("Check(%q).Status = %q, want %q", code, c.Status, StatusNotApplicable)
		}
	}
}

// erroringReaderAtC always fails with a real, non-EOF error.
type erroringReaderAtC struct{}

func (erroringReaderAtC) ReadAt([]byte, int64) (int, error) {
	return 0, errors.New("simulated disk read failure")
}

// xlsxFixtureWithWorkbook builds a minimal xlsx whose xl/workbook.xml is
// exactly workbookXML -- lets a test vary the SERIALIZATION of the sheets
// list independently of its content.
func xlsxFixtureWithWorkbook(t *testing.T, workbookXML string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "xl/workbook.xml", workbookXML)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// TestAnalyzeContainerSheetCountRecognizesEveryLegalSerialization is PR9R11
// (container.go:812). A raw "<sheet" byte scan sees only one spelling of a
// structured format; a workbook that binds the spreadsheet namespace to a
// prefix is just as valid and used to publish sheet_count: 0 with COMPLETE
// coverage -- a fabricated value, not a missing one.
func TestAnalyzeContainerSheetCountRecognizesEveryLegalSerialization(t *testing.T) {
	cases := []struct {
		name     string
		workbook string
		want     int
	}{
		{
			"unprefixed (the original serialization)",
			`<workbook><sheets><sheet name="A" sheetId="1"/><sheet name="B" sheetId="2"/></sheets></workbook>`,
			2,
		},
		{
			"spreadsheet namespace bound to a prefix",
			`<x:workbook xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><x:sheets><x:sheet name="A" sheetId="1"/><x:sheet name="B" sheetId="2"/></x:sheets></x:workbook>`,
			2,
		},
		{
			// PR9R12: extension markup in a foreign namespace is not a
			// worksheet declaration, however its elements are named.
			"foreign-namespace extension sheets must not count",
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets><sheet name="A" sheetId="1"/></sheets><ext xmlns:e="urn:example:vendor-extension"><e:sheets><e:sheet name="ghost"/></e:sheets></ext></workbook>`,
			1,
		},
		{
			// PR9R13: xmlns="" resets to the empty namespace mid-document,
			// so a namespaced workbook CAN contain unnamespaced elements --
			// which is exactly what round 12's global empty-namespace
			// allowance let through.
			"extension resetting the default namespace must not count",
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets><sheet name="A" sheetId="1"/></sheets><ext><sheets xmlns=""><sheet name="ghost"/></sheets></ext></workbook>`,
			1,
		},
		{
			// PR9R19: a <sheets> subtree nested below another element is
			// extension markup reusing SpreadsheetML names, not the
			// workbook's worksheet list. Only workbook > sheets > sheet
			// counts.
			"same-namespace sheets nested under another element must not count",
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets><sheet name="A" sheetId="1"/></sheets><ext><sheets><sheet name="ghost"/></sheets></ext></workbook>`,
			1,
		},
		{
			// The worksheet must be a DIRECT child of the list, too.
			"sheet buried below the real sheets list must not count",
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets><sheet name="A" sheetId="1"/><group><sheet name="ghost"/></group></sheets></workbook>`,
			1,
		},
		{
			"OOXML strict namespace",
			`<workbook xmlns="http://purl.oclc.org/ooxml/spreadsheetml/main"><sheets><sheet name="A" sheetId="1"/><sheet name="B" sheetId="2"/></sheets></workbook>`,
			2,
		},
		{
			"the literal text <sheet inside an XML comment must not count",
			`<workbook><!-- <sheet name="ghost"/> --><sheets><sheet name="A" sheetId="1"/></sheets></workbook>`,
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := xlsxFixtureWithWorkbook(t, tc.workbook)
			res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
			got, ok := res.Characteristics["sheet_count"]
			if !ok {
				t.Fatalf("no sheet_count characteristic at all; got %+v", res.Characteristics)
			}
			if got != tc.want {
				t.Errorf("sheet_count = %v, want %d -- this serialization is as valid as any other and must never publish a fabricated count", got, tc.want)
			}
		})
	}
}

// TestAnalyzeContainerUnparseableWorkbookPublishesNoSheetCount pins the
// other half: a workbook part read in full that does not parse must not
// publish a count at all, least of all zero, which reads as "this workbook
// has no sheets".
func TestAnalyzeContainerUnparseableWorkbookPublishesNoSheetCount(t *testing.T) {
	data := xlsxFixtureWithWorkbook(t, `<workbook><sheets><sheet name="A"`) // truncated mid-tag
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
	if got, ok := res.Characteristics["sheet_count"]; ok && got != nil {
		t.Errorf("sheet_count = %v, want absent or nil -- the part was read in full and did not parse; publishing 0 would assert the workbook has no sheets", got)
	}
}

// TestAnalyzeContainerEmptyWorkbookPublishesNoSheetCount is PR9R14
// (container.go:881): a workbook part that is empty, or holds nothing but
// whitespace and an XML declaration, is not a workbook declaring zero
// sheets -- it is not a workbook. Publishing sheet_count: 0 for it states
// something the part cannot support.
func TestAnalyzeContainerEmptyWorkbookPublishesNoSheetCount(t *testing.T) {
	for _, tc := range []struct{ name, workbook string }{
		{"entirely empty", ""},
		{"whitespace only", "   \n\t  "},
		{"declaration only", `<?xml version="1.0" encoding="UTF-8"?>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := xlsxFixtureWithWorkbook(t, tc.workbook)
			res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
			if got, ok := res.Characteristics["sheet_count"]; ok && got != nil {
				t.Errorf("sheet_count = %v, want absent or nil -- this part has no workbook root at all, so 0 would assert a fact it cannot support", got)
			}
		})
	}
}

// TestAnalyzeContainerNonWorkbookRootPublishesNoSheetCount is PR9R15
// (container.go:896): the previous fix read the root's NAMESPACE and never
// its NAME, so any part whose root happened to contain a <sheets> list
// published a count as a successfully parsed characteristic.
func TestAnalyzeContainerNonWorkbookRootPublishesNoSheetCount(t *testing.T) {
	for _, tc := range []struct{ name, workbook string }{
		{"unnamespaced non-workbook root with a sheets list", `<notWorkbook><sheets><sheet name="ghost"/></sheets></notWorkbook>`},
		{"SpreadsheetML namespace but the wrong root element", `<chartsheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets><sheet name="ghost"/></sheets></chartsheet>`},
		{"arbitrary unnamespaced root, no sheets at all", `<something><other/></something>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := xlsxFixtureWithWorkbook(t, tc.workbook)
			res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30, nil)
			if got, ok := res.Characteristics["sheet_count"]; ok && got != nil {
				t.Errorf("sheet_count = %v, want absent or nil -- this part's root is not a workbook, so it supports no statement about a workbook's sheets; zero would be an answer where there is none", got)
			}
		})
	}
}
