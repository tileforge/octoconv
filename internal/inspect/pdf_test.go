package inspect

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// --- fixtures, built as in-code byte literals (D-15 discipline: a
// generated fixture is readable and editable by the next person; a
// committed binary file is not) ---

// cleanPDFFixture is a minimal, classic (non-compressed) PDF: a Catalog, a
// Pages dict with /Count 3, and one Page object -- no risk tokens, no
// compression signal.
func cleanPDFFixture() []byte {
	return []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 3 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R >>
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// jsPDFFixture is cleanPDFFixture plus an object carrying a /JavaScript
// action -- the one risk token this fixture set is built to exercise.
func jsPDFFixture() []byte {
	return []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R /OpenAction 4 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 3 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R >>
endobj
4 0 obj
<< /S /JavaScript /JS (app.alert('x')) >>
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// objStmPDFFixture carries the /ObjStm compression signal and otherwise no
// risk tokens at all -- the central fixture for D-02a's own test.
func objStmPDFFixture() []byte {
	return []byte(`%PDF-1.6
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /ObjStm /N 3 /First 20 >>
stream
somecompresseddatagoeshere
endstream
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// jsAndObjStmPDFFixture combines both signals -- the case that proves a
// positive finding survives the /ObjStm downgrade instead of being
// swallowed by it.
func jsAndObjStmPDFFixture() []byte {
	return []byte(`%PDF-1.6
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /ObjStm /N 3 /First 20 >>
stream
somecompresseddatagoeshere
endstream
endobj
4 0 obj
<< /S /JavaScript /JS (app.alert('x')) >>
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// encryptedPDFFixture carries /Encrypt in its trailer.
func encryptedPDFFixture() []byte {
	return []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R >>
endobj
trailer
<< /Root 1 0 R /Encrypt 5 0 R >>
%%EOF`)
}

// --- assertion helpers ---

func findingFor(res FamilyResult, code Code) (Finding, bool) {
	for _, f := range res.Findings {
		if f.Code == code {
			return f, true
		}
	}
	return Finding{}, false
}

func checkFor(res FamilyResult, code Code) (Check, bool) {
	for _, c := range res.Checks {
		if c.Code == code {
			return c, true
		}
	}
	return Check{}, false
}

// otherFamilyNotApplicableChecks fills in every non-PDF-domain code from
// AllCodes() with a StatusNotApplicable check -- standing in for the other
// three family analyzers' own results (58-05's media.go/image.go, this
// plan's container.go) so ValidateReport can be exercised against a
// complete report, exactly as this plan's <behavior> section specifies:
// "если дополнить результат проверками остальных семейств".
func otherFamilyNotApplicableChecks(res FamilyResult) []Check {
	owned := map[Code]bool{}
	for _, rt := range pdfRiskTokens {
		owned[rt.code] = true
	}
	var extra []Check
	for _, code := range AllCodes() {
		if owned[code] {
			continue
		}
		found := false
		for _, f := range res.Findings {
			if f.Code == code {
				found = true
			}
		}
		if !found {
			extra = append(extra, Check{Code: code, Status: StatusNotApplicable, Reason: ReasonFormatNotApplicable})
		}
	}
	return extra
}

// --- <behavior> coverage ---

func TestAnalyzePDFCleanFileReportsPageCountAndAllClean(t *testing.T) {
	data := cleanPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)))

	if pc, ok := res.Characteristics["page_count"]; !ok || pc != 3 {
		t.Fatalf("Characteristics[page_count] = %v (ok=%v), want 3", pc, ok)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("Findings = %v, want empty", res.Findings)
	}
	for _, rt := range pdfRiskTokens {
		c, ok := checkFor(res, rt.code)
		if !ok {
			t.Fatalf("no Check for code %q", rt.code)
		}
		if c.Status != StatusClean {
			t.Fatalf("Check(%q).Status = %q, want %q", rt.code, c.Status, StatusClean)
		}
	}

	checks := append(append([]Check{}, res.Checks...), otherFamilyNotApplicableChecks(res)...)
	if err := ValidateReport(res.Findings, checks); err != nil {
		t.Fatalf("ValidateReport = %v, want nil", err)
	}
}

func TestAnalyzePDFJavaScriptIsFindingNotCheck(t *testing.T) {
	data := jsPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)))

	f, ok := findingFor(res, CodePDFJavaScript)
	if !ok {
		t.Fatal("no Finding for CodePDFJavaScript")
	}
	if f.Location == "" {
		t.Fatal("Finding(CodePDFJavaScript).Location is empty, want a byte offset")
	}
	if _, ok := checkFor(res, CodePDFJavaScript); ok {
		t.Fatal("CodePDFJavaScript appears in Checks -- union invariant violated (must be Finding XOR Check)")
	}

	checks := append(append([]Check{}, res.Checks...), otherFamilyNotApplicableChecks(res)...)
	if err := ValidateReport(res.Findings, checks); err != nil {
		t.Fatalf("ValidateReport = %v, want nil", err)
	}
}

// TestAnalyzePDFObjStmNeverReportsClean is the central test of this plan
// (D-02a): a PDF carrying the /ObjStm compression signal must never let ANY
// PDF-domain code answer StatusClean, because ISO 32000 permits any
// dictionary except a stream object or the encryption dictionary to live
// inside an object stream -- absence of a token in a byte scan is not
// evidence of its absence in the file.
func TestAnalyzePDFObjStmNeverReportsClean(t *testing.T) {
	data := objStmPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)))

	if pc, ok := res.Characteristics["page_count"]; !ok || pc != nil {
		t.Fatalf("Characteristics[page_count] = %v, want nil under /ObjStm", pc)
	}

	for _, rt := range pdfRiskTokens {
		if _, isFinding := findingFor(res, rt.code); isFinding {
			continue // a positive finding is not what this test is about
		}
		c, ok := checkFor(res, rt.code)
		if !ok {
			t.Fatalf("no Check for code %q", rt.code)
		}
		if c.Status == StatusClean {
			t.Fatalf("D-02a violated: code %q reported StatusClean on a PDF with /ObjStm present -- a compressed-object-stream PDF must never answer clean", rt.code)
		}
		if c.Reason != ReasonCompressedObjectStreams {
			t.Fatalf("Check(%q).Reason = %q, want %q (D-02a)", rt.code, c.Reason, ReasonCompressedObjectStreams)
		}
	}
}

// TestAnalyzePDFFindingSurvivesObjStm proves the inverse of the above: a
// POSITIVE observation is not weakened by the same structural blindness
// that degrades absence-claims.
func TestAnalyzePDFFindingSurvivesObjStm(t *testing.T) {
	data := jsAndObjStmPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)))

	f, ok := findingFor(res, CodePDFJavaScript)
	if !ok {
		t.Fatal("CodePDFJavaScript finding did not survive /ObjStm presence -- a positive observation must never be downgraded")
	}
	if f.Location == "" {
		t.Fatal("surviving Finding(CodePDFJavaScript).Location is empty")
	}

	for _, rt := range pdfRiskTokens {
		if rt.code == CodePDFJavaScript {
			continue
		}
		c, ok := checkFor(res, rt.code)
		if !ok {
			t.Fatalf("no Check for code %q", rt.code)
		}
		if c.Status != StatusUnknown || c.Reason != ReasonCompressedObjectStreams {
			t.Fatalf("Check(%q) = %+v, want unknown/compressed_object_streams", rt.code, c)
		}
	}
}

func TestAnalyzePDFEncryptedYieldsFinding(t *testing.T) {
	data := encryptedPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)))

	if _, ok := findingFor(res, CodePDFEncrypted); !ok {
		t.Fatal("no Finding for CodePDFEncrypted on a PDF with /Encrypt in its trailer")
	}
}

// TestAnalyzePDFMalformedInputNeverPanicsAndReportsUnknown covers a hard
// read failure (not merely a truncation): every PDF-domain code must
// degrade to unknown/malformed_input, and page_count must be nil with the
// same reason.
func TestAnalyzePDFMalformedInputNeverPanicsAndReportsUnknown(t *testing.T) {
	r := erroringReaderAt{}
	res := AnalyzePDF(r, 100, 100)

	if pc, ok := res.Characteristics["page_count"]; !ok || pc != nil {
		t.Fatalf("Characteristics[page_count] = %v, want nil", pc)
	}
	if reason, ok := res.Characteristics["page_count_reason"]; !ok || reason != string(ReasonMalformedInput) {
		t.Fatalf("Characteristics[page_count_reason] = %v, want %q", reason, ReasonMalformedInput)
	}
	for _, rt := range pdfRiskTokens {
		c, ok := checkFor(res, rt.code)
		if !ok {
			t.Fatalf("no Check for code %q", rt.code)
		}
		if c.Status != StatusUnknown || c.Reason != ReasonMalformedInput {
			t.Fatalf("Check(%q) = %+v, want unknown/malformed_input", rt.code, c)
		}
	}
}

// erroringReaderAt always fails a real (non-EOF) error -- exercises
// readWindow's !headOK branch deterministically.
type erroringReaderAt struct{}

func (erroringReaderAt) ReadAt([]byte, int64) (int, error) {
	return 0, errors.New("boom")
}

// TestAnalyzePDFCeilingReasonDiffersFromCompressionReason proves the two
// downgrade reasons are never conflated: OUR OWN byte ceiling and the
// FILE'S structural blindness are different facts and must carry different
// Reason values.
func TestAnalyzePDFCeilingReasonDiffersFromCompressionReason(t *testing.T) {
	objStmData := objStmPDFFixture()
	objStmRes := AnalyzePDF(bytes.NewReader(objStmData), int64(len(objStmData)), int64(len(objStmData)))
	c1, ok := checkFor(objStmRes, CodePDFEmbeddedFile)
	if !ok || c1.Reason != ReasonCompressedObjectStreams {
		t.Fatalf("objStm case: Check(pdf_embedded_file) = %+v, want reason=%q", c1, ReasonCompressedObjectStreams)
	}

	cleanData := cleanPDFFixture()
	// maxBytes smaller than the header alone -- forces truncation with no
	// compression signal anywhere in what little was read.
	truncRes := AnalyzePDF(bytes.NewReader(cleanData), int64(len(cleanData)), 8)
	c2, ok := checkFor(truncRes, CodePDFEmbeddedFile)
	if !ok || c2.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("truncated case: Check(pdf_embedded_file) = %+v, want reason=%q", c2, ReasonWorkCeilingBytes)
	}

	if c1.Reason == c2.Reason {
		t.Fatalf("compression-blindness reason and work-ceiling reason must differ, both were %q", c1.Reason)
	}
}

// --- countingTailReaderAt: a virtual, arbitrarily large io.ReaderAt that
// never actually allocates its declared size -- zero-filled everywhere
// except an explicit tail overlay, and counts every byte requested via
// ReadAt so tests can assert a bound on total bytes read. ---

type countingTailReaderAt struct {
	size      int64
	tail      []byte // overlaid onto the LAST len(tail) bytes of the virtual file
	totalRead int64
}

func (c *countingTailReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n := int64(len(p))
	if off >= c.size {
		return 0, errors.New("EOF")
	}
	if off+n > c.size {
		n = c.size - off
	}
	c.totalRead += n

	tailStart := c.size - int64(len(c.tail))
	for i := int64(0); i < n; i++ {
		abs := off + i
		if abs >= tailStart {
			p[i] = c.tail[abs-tailStart]
		} else {
			p[i] = 0
		}
	}
	if n < int64(len(p)) {
		return int(n), errors.New("EOF")
	}
	return int(n), nil
}

// TestAnalyzePDFBoundedReads proves AnalyzePDF never reads more than
// maxBytes+defaultTailWindow total bytes from the underlying io.ReaderAt,
// even against a 200 MiB virtual file.
func TestAnalyzePDFBoundedReads(t *testing.T) {
	const virtualSize = 200 << 20
	const maxBytes = 1 << 20
	r := &countingTailReaderAt{size: virtualSize}

	_ = AnalyzePDF(r, virtualSize, maxBytes)

	limit := int64(maxBytes + defaultTailWindow)
	if r.totalRead > limit {
		t.Fatalf("AnalyzePDF read %d bytes, want <= %d (maxBytes + defaultTailWindow)", r.totalRead, limit)
	}
}

// TestAnalyzePDFLocationIsFileOffsetNotWindowOffset places /Encrypt only in
// the tail of a large virtual file (the one code this file scans a tail
// window for) and proves the reported Location is the real file offset, not
// an offset relative to the start of the tail window.
func TestAnalyzePDFLocationIsFileOffsetNotWindowOffset(t *testing.T) {
	const virtualSize = 200 << 20
	const maxBytes = 1 << 20

	tail := make([]byte, defaultTailWindow)
	const relOffset = 100
	copy(tail[relOffset:], []byte("/Encrypt"))

	r := &countingTailReaderAt{size: virtualSize, tail: tail}
	res := AnalyzePDF(r, virtualSize, maxBytes)

	f, ok := findingFor(res, CodePDFEncrypted)
	if !ok {
		t.Fatal("no Finding for CodePDFEncrypted -- tail scan did not find the token")
	}

	wantOffset := virtualSize - int64(defaultTailWindow) + relOffset
	gotOffset, err := strconv.ParseInt(f.Location, 10, 64)
	if err != nil {
		t.Fatalf("Location %q is not a decimal integer: %v", f.Location, err)
	}
	if gotOffset != wantOffset {
		t.Fatalf("Location = %d, want %d (real file offset, not window-relative)", gotOffset, wantOffset)
	}
}

// TestAnalyzePDFNeverPanics is a table-driven pass over adversarial inputs.
func TestAnalyzePDFNeverPanics(t *testing.T) {
	cleanFixture := cleanPDFFixture()
	cases := []struct {
		name     string
		r        readerAtFunc
		size     int64
		maxBytes int64
	}{
		{"empty", bytes.NewReader(nil).ReadAt, 0, 1024},
		{"tiny garbage", bytes.NewReader([]byte("x")).ReadAt, 1, 1024},
		{"not a pdf at all", bytes.NewReader([]byte("this is definitely not a PDF file at all")).ReadAt, 41, 1024},
		{"bare header no digits", bytes.NewReader([]byte("%PDF-")).ReadAt, 5, 1024},
		{"claims size larger than actual data", bytes.NewReader([]byte("%PDF-1.4 short")).ReadAt, 10_000, 1024},
		{"erroring reader", erroringReaderAt{}.ReadAt, 100, 100},
		{"zero maxBytes", bytes.NewReader(cleanFixture).ReadAt, int64(len(cleanFixture)), 0},
		{"negative size", bytes.NewReader(cleanFixture).ReadAt, -1, 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("AnalyzePDF panicked: %v", p)
				}
			}()
			res := AnalyzePDF(tc.r, tc.size, tc.maxBytes)
			if _, ok := res.Characteristics["page_count"]; !ok {
				t.Fatalf("Characteristics missing page_count key")
			}
		})
	}
}

// readerAtFunc adapts a bound ReadAt method value to satisfy io.ReaderAt,
// so the table above can carry heterogeneous reader sources uniformly.
type readerAtFunc func([]byte, int64) (int, error)

func (f readerAtFunc) ReadAt(p []byte, off int64) (int, error) { return f(p, off) }

func TestAnalyzePDFPageCountReasonIsMalformedNotACeilingWord(t *testing.T) {
	// A readable, untruncated, but structurally nonsensical body: no Pages
	// dict, no /Count, no compression signal, and no risk tokens either --
	// every risk-token Check legitimately reports clean (the whole
	// available window was scanned and nothing was found), while the
	// page-count fast path still could not find its structure.
	data := []byte(`%PDF-1.4
this is not a pages dictionary at all
%%EOF`)
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)))

	if pc, ok := res.Characteristics["page_count"]; !ok || pc != nil {
		t.Fatalf("Characteristics[page_count] = %v, want nil", pc)
	}
	if reason, ok := res.Characteristics["page_count_reason"]; !ok || reason != string(ReasonMalformedInput) {
		t.Fatalf("Characteristics[page_count_reason] = %v, want %q", reason, ReasonMalformedInput)
	}
}

// TestAnalyzePDFVersionExtracted is a small sanity check on the
// pdf_version characteristic -- not in <behavior> explicitly but cheap
// insurance against a silent regression in extractPDFVersion.
func TestAnalyzePDFVersionExtracted(t *testing.T) {
	data := cleanPDFFixture()
	res := AnalyzePDF(bytes.NewReader(data), int64(len(data)), int64(len(data)))
	v, ok := res.Characteristics["pdf_version"]
	if !ok {
		t.Fatal("Characteristics[pdf_version] missing")
	}
	if !strings.HasPrefix(v.(string), "1.4") {
		t.Fatalf("Characteristics[pdf_version] = %q, want prefix %q", v, "1.4")
	}
}

// --- scanAllOccurrences direct unit test (the one loop whose iteration
// ceiling is genuinely load-bearing against adversarial repetitive input) ---

func TestScanAllOccurrencesRespectsIterationCeiling(t *testing.T) {
	token := []byte("AB")
	buf := bytes.Repeat(token, 10) // 10 occurrences

	var calls int
	ceilingHit := scanAllOccurrences(buf, token, 5, func(pos int) bool {
		calls++
		return true
	})
	if !ceilingHit {
		t.Fatal("scanAllOccurrences ceilingHit = false, want true (buffer has more occurrences than the cap)")
	}
	if calls != 5 {
		t.Fatalf("scanAllOccurrences invoked fn %d times, want exactly 5 (the cap)", calls)
	}
}

func TestScanAllOccurrencesNoCeilingWhenUnderCap(t *testing.T) {
	token := []byte("AB")
	buf := bytes.Repeat(token, 3)

	var calls int
	ceilingHit := scanAllOccurrences(buf, token, 100, func(pos int) bool {
		calls++
		return true
	})
	if ceilingHit {
		t.Fatal("scanAllOccurrences ceilingHit = true, want false -- fewer occurrences than the cap")
	}
	if calls != 3 {
		t.Fatalf("scanAllOccurrences invoked fn %d times, want 3", calls)
	}
}
