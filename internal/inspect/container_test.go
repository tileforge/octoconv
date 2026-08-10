package inspect

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
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

// --- assertion helpers shared with pdf_test.go's checkFor/findingFor ---

// --- <behavior> coverage ---

func TestAnalyzeContainerMacroFindingLocation(t *testing.T) {
	data := macroDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

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
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), bombThreshold)

	if _, ok := findingFor(res, CodeZipBombShape); !ok {
		t.Fatal("no Finding for CodeZipBombShape for a size between the findings threshold and a hypothetical hard-reject threshold")
	}
	if 5_000_000 >= hypotheticalHardRejectThreshold {
		t.Fatal("test setup error: declared size must stay below the hypothetical hard-reject threshold")
	}
}

func TestAnalyzeContainerSheetCountXLSX(t *testing.T) {
	data := xlsxFixture(t, 3)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

	if sc, ok := res.Characteristics["sheet_count"]; !ok || sc != 3 {
		t.Fatalf("Characteristics[sheet_count] = %v (ok=%v), want 3", sc, ok)
	}
}

func TestAnalyzeContainerPageCountKeyAbsentForSpreadsheets(t *testing.T) {
	data := xlsxFixture(t, 1)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

	if _, ok := res.Characteristics["page_count"]; ok {
		t.Fatalf("Characteristics contains page_count for xlsx, want absent entirely")
	}
}

func TestAnalyzeContainerDocxSheetCountAbsentPageCountUnknown(t *testing.T) {
	data := cleanDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

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
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

	f, ok := findingFor(res, CodeExternalReference)
	if !ok {
		t.Fatal("no Finding for CodeExternalReference")
	}
	if f.Location != "word/_rels/document.xml.rels" {
		t.Fatalf("Finding(CodeExternalReference).Location = %q, want %q", f.Location, "word/_rels/document.xml.rels")
	}
}

func TestAnalyzeContainerCleanDocxAllClean(t *testing.T) {
	data := cleanDocxFixture(t)
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

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
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

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
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

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
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

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

	res := AnalyzeContainer(bytes.NewReader(patched), int64(len(patched)), int64(len(patched)), 1<<30)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v for a part that should have been skipped by its (lying) declared size -- this scan must never trust declared size enough to open a part that claims to exceed maxRelsPartBytes", f)
	}
	c, ok := checkFor(res, CodeExternalReference)
	if !ok || c.Status != StatusClean {
		t.Fatalf("Check(CodeExternalReference) = %+v (ok=%v), want clean -- the only candidate part was skipped by declared size, so the scan legitimately found nothing", c, ok)
	}
}

// odtFixtureWithTwoParts builds a minimal, entirely HONEST odt (no forged
// metadata anywhere -- every declared size matches its real content
// exactly) with two external-reference candidate parts: content.xml (small,
// no marker) and styles.xml (moderate size, with an xlink:href external
// marker placed near its tail). ODF's TWO fixed candidate names
// (content.xml, styles.xml) are exactly what lets a single-format test
// exercise a MULTI-PART scan, unlike OOXML's single .rels convention.
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

	styles := append(bytes.Repeat([]byte("A"), stylesSize-len(`xlink:href="http://example.com"`)), []byte(`xlink:href="http://example.com"`)...)
	mustWriteZipEntry(t, zw, "styles.xml", string(styles))

	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// TestAnalyzeContainerLimitReaderBoundsCrossPartBudget proves io.LimitReader
// (fed the REMAINING per-request byte budget, not just the static
// maxRelsPartBytes cap) is what stops this scan from reading an external
// marker placed near the tail of a SECOND, genuinely and honestly small
// part once an earlier part has already spent most of the request's
// budget. Both parts here have entirely truthful declared sizes -- this
// is deliberately NOT the "declared size lies" scenario (that one is
// already covered by TestAnalyzeContainerNeverAllocatesOnDeclaredSize and,
// as it turns out, is independently blocked by archive/zip's own
// checksumReader, which refuses to return more bytes than a file's
// declared UncompressedSize64 regardless of this package's own code). The
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
	res := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), tightBudget, 1<<30)

	if f, ok := findingFor(res, CodeExternalReference); ok {
		t.Fatalf("got Finding %+v with a %d-byte total budget -- io.LimitReader must stop the styles.xml read long before its tail-placed marker, using the budget REMAINING after content.xml was already read, not a fresh maxRelsPartBytes allowance per part", f, tightBudget)
	}
	c, ok := checkFor(res, CodeExternalReference)
	if !ok || c.Status != StatusClean {
		t.Fatalf("Check(CodeExternalReference) = %+v (ok=%v), want clean", c, ok)
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
	resGenerous := AnalyzeContainer(bytes.NewReader(data), int64(len(data)), 1<<20, 1<<30)
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
			AnalyzeContainer(bytes.NewReader(tc.data), int64(len(tc.data)), int64(len(tc.data)), 1<<30)
		})
	}
}
