package inspect

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
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

// hexEscapeJSPDFFixture spells /JavaScript as /Java#53cript (0x53 = 'S') --
// codex PR9 finding 1's own worked example (pdf.go:112). A scanner that only
// ever matches the literal byte spelling "/JavaScript"/"/JS" would report
// this file clean despite carrying a real, active JavaScript action.
func hexEscapeJSPDFFixture() []byte {
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
<< /S /Java#53cript (app.alert('x')) >>
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// hexEscapeObjStmPDFFixture spells /ObjStm as /Obj#53tm (0x53 = 'S') --
// codex PR9 finding 1's OTHER worked example, proving the hex-escape
// downgrade fires independently of pdfCompressionSignalTokens's own literal
// "/ObjStm" match (which this spelling does NOT trigger).
func hexEscapeObjStmPDFFixture() []byte {
	return []byte(`%PDF-1.6
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Obj#53tm /N 3 /First 20 >>
stream
somecompresseddatagoeshere
endstream
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// jsInsideStreamOnlyPDFFixture places the literal "/JS" ONLY inside a
// stream body -- codex PR9 finding 2's own example (pdf.go:199): three-byte
// markers like /JS and /AA are especially prone to appearing in arbitrary
// compressed/binary stream payloads. No dictionary outside the stream
// carries a JavaScript action anywhere in this fixture.
func jsInsideStreamOnlyPDFFixture() []byte {
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
5 0 obj
<< /Length 28 >>
stream
totally unrelated /JS bytes
endstream
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// jscriptOnlyPDFFixture places the dictionary key /JScript (a real, legal
// PDF name distinct from /JS) as the ONLY thing in the file that contains
// "/JS" as a substring -- codex PR9 round 2's own worked example
// (pdf.go:515): "/JS" must not match inside "/JScript" because the byte
// immediately after the substring match ('c') continues the same PDF name.
// No genuine /JS or /JavaScript token exists anywhere in this fixture.
func jscriptOnlyPDFFixture() []byte {
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
4 0 obj
<< /Language /JScript /Source (some script text) >>
endobj
trailer
<< /Root 1 0 R >>
%%EOF`)
}

// jsInsideLiteralStringOnlyPDFFixture places the literal "/JS" ONLY inside a
// PDF literal string's DATA -- codex PR9 round 2's own worked example
// (pdf.go:515): "(documentation says /JS)" is string content, not PDF
// dictionary syntax, and must not be promoted to a critical Finding. Unlike
// jscriptOnlyPDFFixture, this occurrence DOES pass the name-boundary check
// (the byte after "/JS" is ")", a delimiter) -- it is excluded because it
// sits inside a literal-string opaque range, not because of the boundary
// rule, so this fixture exercises the OTHER new branch of the round 2 fix.
func jsInsideLiteralStringOnlyPDFFixture() []byte {
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
4 0 obj
<< /Title (documentation says /JS) >>
endobj
trailer
<< /Root 1 0 R >>
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
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

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
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

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
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

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
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

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

// TestAnalyzePDFHexEscapedJavaScriptNeverReportsClean is codex PR9 finding 1
// (T-SLE-01): PDF names permit hexadecimal escapes, so /JavaScript can be
// spelled /Java#53cript and still be a completely legal, active JavaScript
// action -- a scanner that only matches literal byte spellings would
// report this file clean. No PDF-domain code may answer StatusClean here.
func TestAnalyzePDFHexEscapedJavaScriptNeverReportsClean(t *testing.T) {
	data := hexEscapeJSPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

	for _, rt := range pdfRiskTokens {
		if _, isFinding := findingFor(res, rt.code); isFinding {
			continue // a positive finding is not what this test is about
		}
		c, ok := checkFor(res, rt.code)
		if !ok {
			t.Fatalf("no Check for code %q", rt.code)
		}
		if c.Status == StatusClean {
			t.Fatalf("code %q reported StatusClean on a PDF using a hex-escaped name (/Java#53cript) -- an escaped name must never be trusted as absent", rt.code)
		}
		if c.Reason != ReasonPDFNameEscape {
			t.Fatalf("Check(%q).Reason = %q, want %q", rt.code, c.Reason, ReasonPDFNameEscape)
		}
	}
}

// TestAnalyzePDFHexEscapedObjStmDowngradesToUnknown is codex PR9 finding 1's
// other worked example: /ObjStm spelled /Obj#53tm is invisible to
// containsCompressionSignal's literal match, but the hex-escape detector
// alone must still be enough to downgrade the whole PDF block to unknown.
func TestAnalyzePDFHexEscapedObjStmDowngradesToUnknown(t *testing.T) {
	data := hexEscapeObjStmPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

	if res.Characteristics["uses_compressed_object_streams"] != false {
		t.Fatalf("uses_compressed_object_streams = %v, want false -- this test is about the hex-escape path, not the literal /ObjStm compression-signal path", res.Characteristics["uses_compressed_object_streams"])
	}
	for _, rt := range pdfRiskTokens {
		c, ok := checkFor(res, rt.code)
		if !ok {
			t.Fatalf("no Check for code %q", rt.code)
		}
		if c.Status == StatusClean {
			t.Fatalf("code %q reported StatusClean on a PDF with a hex-escaped /ObjStm marker (/Obj#53tm)", rt.code)
		}
		if c.Reason != ReasonPDFNameEscape {
			t.Fatalf("Check(%q).Reason = %q, want %q", rt.code, c.Reason, ReasonPDFNameEscape)
		}
	}
}

// TestAnalyzePDFTokenOnlyInsideStreamBodyIsUnknownNotCriticalNotClean is
// codex PR9 finding 2 (T-SLE-02): a literal match found ONLY inside a
// stream body must never be promoted to a critical Finding (this scanner
// cannot verify the bytes are a real PDF name, not coincidental
// opaque-payload content) -- but it must equally never fall back to
// StatusClean, which would be an equally false claim in the other
// direction.
func TestAnalyzePDFTokenOnlyInsideStreamBodyIsUnknownNotCriticalNotClean(t *testing.T) {
	data := jsInsideStreamOnlyPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

	if f, ok := findingFor(res, CodePDFJavaScript); ok {
		t.Fatalf("got a critical Finding %+v for a /JS token that occurs ONLY inside a stream body", f)
	}
	c, ok := checkFor(res, CodePDFJavaScript)
	if !ok {
		t.Fatal("no Check for CodePDFJavaScript")
	}
	if c.Status == StatusClean {
		t.Fatal("Check(pdf_javascript).Status = clean, want unknown -- a /JS match inside a stream body is not evidence of absence either")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonPDFStreamBodyToken {
		t.Fatalf("Check(pdf_javascript) = %+v, want unknown/pdf_stream_body_token", c)
	}
}

// TestAnalyzePDFJScriptNameDoesNotMatchJS is codex PR9 round 2's own worked
// example (pdf.go:515): a dictionary key /JScript must never trigger
// CodePDFJavaScript by matching its "/JS" prefix as a substring. Since no
// genuine /JS or /JavaScript token exists anywhere in this fixture, and the
// PDF carries no compression signal, no hex escape, and no truncation, the
// honest answer is StatusClean -- unlike the opaque-range cases, there is
// no genuine ambiguity here: "/JScript" is unambiguously a different name.
func TestAnalyzePDFJScriptNameDoesNotMatchJS(t *testing.T) {
	data := jscriptOnlyPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

	if f, ok := findingFor(res, CodePDFJavaScript); ok {
		t.Fatalf("got a critical Finding %+v for /JScript, which is a different PDF name from /JS -- a substring match without a name-boundary check is exactly codex PR9 round 2's finding", f)
	}
	c, ok := checkFor(res, CodePDFJavaScript)
	if !ok {
		t.Fatal("no Check for CodePDFJavaScript")
	}
	if c.Status != StatusClean {
		t.Fatalf("Check(pdf_javascript) = %+v, want clean -- /JScript is unambiguously not /JS, there is no ambiguity left to report as unknown", c)
	}
}

// TestAnalyzePDFJSInsideLiteralStringIsUnknownNotCriticalNotClean is codex
// PR9 round 2's other worked example (pdf.go:515): "/JS" appearing only
// inside a PDF literal string's DATA ("(documentation says /JS)") must
// never be promoted to a critical Finding, but must also never fall back to
// StatusClean -- this scanner cannot rule out that the same bytes also
// appear as a real PDF name elsewhere, and treating string-embedded
// coincidental content as proof of absence would repeat exactly the
// mistake round 1 already fixed for stream bodies and comments.
func TestAnalyzePDFJSInsideLiteralStringIsUnknownNotCriticalNotClean(t *testing.T) {
	data := jsInsideLiteralStringOnlyPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

	if f, ok := findingFor(res, CodePDFJavaScript); ok {
		t.Fatalf("got a critical Finding %+v for a /JS token that occurs ONLY inside a literal string's data", f)
	}
	c, ok := checkFor(res, CodePDFJavaScript)
	if !ok {
		t.Fatal("no Check for CodePDFJavaScript")
	}
	if c.Status == StatusClean {
		t.Fatal("Check(pdf_javascript).Status = clean, want unknown -- a /JS match inside a literal string's data is not evidence of absence either")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonPDFStreamBodyToken {
		t.Fatalf("Check(pdf_javascript) = %+v, want unknown/pdf_stream_body_token", c)
	}
}

// TestAnalyzePDFOpaqueRangeFilterIsNotQuadratic is codex PR9 round 2's
// performance/DoS finding (pdf.go:520): a PDF with thousands of opaque
// (stream) ranges and a huge number of "/JS" occurrences packed into the
// LAST range must still return quickly -- the original, pre-fix
// implementation made inOpaqueRange a linear scan over every range, so
// occurrences times ranges is billions of comparisons for these sizes.
// Deliberately does not check ceilingHit/StatusUnknown here (that is
// TestAnalyzePDFOpaqueOccurrenceCeilingIsUnknownNotClean's job) -- this test
// is purely about wall-clock time, run with an explicit deadline rather than
// t.Short() so it always exercises the real cost in CI.
func TestAnalyzePDFOpaqueRangeFilterIsNotQuadratic(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	// 16384 small, harmless streams -- each contributes one opaque range
	// that a linear inOpaqueRange scan must walk past before reaching the
	// LAST range below, where every "/JS" occurrence actually lives.
	const rangeCount = 16384
	for i := 0; i < rangeCount; i++ {
		buf.WriteString("stream\nx\nendstream\n")
	}
	// One final, large stream packed with "/JS" occurrences -- every one of
	// them must be checked against all 4096+ preceding ranges by
	// inOpaqueRange under the old linear-scan implementation.
	buf.WriteString("stream\n")
	buf.Write(bytes.Repeat([]byte("/JS "), maxTokenScanIterations-100))
	buf.WriteString("\nendstream\n%%EOF")
	data := buf.Bytes()

	done := make(chan FamilyResult, 1)
	go func() {
		done <- AnalyzePDF(bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	}()

	select {
	case res := <-done:
		if c, ok := checkFor(res, CodePDFJavaScript); ok && c.Status == StatusClean {
			t.Fatal("Check(pdf_javascript).Status = clean, want unknown -- /JS occurs (inside opaque stream bodies) throughout this fixture")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AnalyzePDF did not return within 5s -- the opaque-range filter is quadratic in occurrences x ranges again")
	}
}

// TestAnalyzePDFOpaqueOccurrenceCeilingIsUnknownNotClean is codex PR9 round
// 2's occurrence-cap requirement (pdf.go:520): even with inOpaqueRange's
// binary search removing the range-count multiplier, the NUMBER of
// occurrences a scan walks is itself attacker-controlled, so
// scanTokenOccurrences must have its OWN ceiling, and exhausting it must
// report unknown, never clean. A token with no delimiter after it anywhere
// (so every occurrence keeps advancing the walk rather than concluding via
// a real match) repeated well past maxTokenScanIterations forces the cap.
func TestAnalyzePDFOpaqueOccurrenceCeilingIsUnknownNotClean(t *testing.T) {
	// "/JSX" never satisfies "/JS" boundary-valid (next byte 'X' continues
	// the name), so scanTokenOccurrences must walk every occurrence without
	// ever concluding "found" or "no more occurrences" -- forcing the cap.
	repeats := maxTokenScanIterations + 1000
	data := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("/JSX"), repeats)...)
	data = append(data, []byte("\n%%EOF")...)

	res := AnalyzePDF(bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)

	c, ok := checkFor(res, CodePDFJavaScript)
	if !ok {
		t.Fatal("no Check for CodePDFJavaScript")
	}
	if c.Status == StatusClean {
		t.Fatal("Check(pdf_javascript).Status = clean, want unknown -- the occurrence-scan ceiling was exhausted, so clean is not an honest answer")
	}
	if c.Status != StatusUnknown || c.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Check(pdf_javascript) = %+v, want unknown/work_ceiling_bytes", c)
	}
}

func TestAnalyzePDFEncryptedYieldsFinding(t *testing.T) {
	data := encryptedPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

	if _, ok := findingFor(res, CodePDFEncrypted); !ok {
		t.Fatal("no Finding for CodePDFEncrypted on a PDF with /Encrypt in its trailer")
	}
}

// TestAnalyzePDFMalformedInputNeverPanicsAndReportsUnknown covers a hard
// read failure (not merely a truncation): every PDF-domain code must
// degrade to unknown/malformed_input, and page_count must be nil with the
// same reason.
// TestAnalyzePDFReadFailureNeverPanicsAndReportsReadFailed replaces the
// former TestAnalyzePDFMalformedInputNeverPanicsAndReportsUnknown, which
// pinned ReasonMalformedInput for this exact erroringReaderAt fixture.
//
// That pinning was itself the defect, not a contract: the fixture never
// produces a malformed PDF, it produces an unreadable one. Round 6's
// neighbour audit found AnalyzePDF labeling an operational read failure as a
// structural claim about the file, and the test had frozen that mislabeling
// in place -- which is exactly how a wrong behavior survives review rounds.
// What the test was really guarding (never panic, never clean, always an
// honest unknown for every PDF-domain code) is preserved and strengthened
// below; only the reason is corrected.
func TestAnalyzePDFReadFailureNeverPanicsAndReportsReadFailed(t *testing.T) {
	r := erroringReaderAt{}
	res := AnalyzePDF(r, 100, 100, nil)

	if pc, ok := res.Characteristics["page_count"]; !ok || pc != nil {
		t.Fatalf("Characteristics[page_count] = %v, want nil", pc)
	}
	if reason, ok := res.Characteristics["page_count_reason"]; !ok || reason != string(ReasonReadFailed) {
		t.Fatalf("Characteristics[page_count_reason] = %v, want %q -- an unreadable upload is not a malformed one", reason, ReasonReadFailed)
	}
	for _, rt := range pdfRiskTokens {
		c, ok := checkFor(res, rt.code)
		if !ok {
			t.Fatalf("no Check for code %q", rt.code)
		}
		if c.Status != StatusUnknown || c.Reason != ReasonReadFailed {
			t.Fatalf("Check(%q) = %+v, want unknown/read_failed -- never clean, and never a claim about the FILE when the failure was our inability to read it", rt.code, c)
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
	objStmRes := AnalyzePDF(bytes.NewReader(objStmData), int64(len(objStmData)), int64(len(objStmData)), nil)
	c1, ok := checkFor(objStmRes, CodePDFEmbeddedFile)
	if !ok || c1.Reason != ReasonCompressedObjectStreams {
		t.Fatalf("objStm case: Check(pdf_embedded_file) = %+v, want reason=%q", c1, ReasonCompressedObjectStreams)
	}

	cleanData := cleanPDFFixture()
	// maxBytes smaller than the header alone -- forces truncation with no
	// compression signal anywhere in what little was read.
	truncRes := AnalyzePDF(bytes.NewReader(cleanData), int64(len(cleanData)), 8, nil)
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

	_ = AnalyzePDF(r, virtualSize, maxBytes, nil)

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
	res := AnalyzePDF(r, virtualSize, maxBytes, nil)

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
			res := AnalyzePDF(tc.r, tc.size, tc.maxBytes, nil)
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
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

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
	res := AnalyzePDF(bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
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
	ceilingHit := scanAllOccurrences(buf, token, 5, nil, func(pos int) bool {
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
	ceilingHit := scanAllOccurrences(buf, token, 100, nil, func(pos int) bool {
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

// --- quick/260811-r3s Task 2: the four named findings ---

// TestExtractPDFPageCountCeilingHitBeforeFoundIsUnknownNotStale is the
// pdf.go:800 finding, verbatim: "When a PDF contains more than
// maxTokenScanIterations Pages-dictionary markers and an early marker has a
// legible count, scanAllOccurrences sets ceilingHit, but this branch
// returns the early value before examining that signal. A later marker can
// belong to the current incremental revision and carry a different count,
// so the response publishes a stale page_count with complete coverage even
// though the scan stopped early."
//
// The fixture places exactly maxTokenScanIterations+1 occurrences of
// "/Type/Pages": the FIRST carries a legible "/Count 5", then
// maxTokenScanIterations-1 filler occurrences with no /Count of their OWN
// nearby, then ONE further occurrence -- past the cap, never itself
// visited by the loop -- carrying a DIFFERENT "/Count 99". (findCountAfter's
// own 512-byte lookahead means the LAST occurrence the loop does visit can,
// depending on exact spacing, end up seeing either count -- what matters
// for this test is not WHICH value leaks, but that a ceiling-truncated walk
// must never publish ANY value it saw as if it were authoritative: this
// fixture is guaranteed to sit at the ceiling boundary either way.)
func TestExtractPDFPageCountCeilingHitBeforeFoundIsUnknownNotStale(t *testing.T) {
	variant := []byte("/Type/Pages")
	var buf bytes.Buffer
	buf.Write(variant)
	buf.WriteString("/Count 5 ") // first occurrence's own nearby count: the stale, early answer
	buf.Write(bytes.Repeat(variant, maxTokenScanIterations-1))
	buf.Write(variant)
	buf.WriteString("/Count 99 ") // ONE further occurrence past the cap, with a DIFFERENT count
	head := buf.Bytes()

	count, reason := extractPDFPageCount(head, false, false, false, nil, false, nil)
	if count != nil {
		t.Fatalf("page_count = %d, want nil -- a ceiling-truncated walk must never publish a value it saw along the way as if it were authoritative", *count)
	}
	if reason != ReasonWorkCeilingBytes {
		t.Fatalf("reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
}

// jsInsideLiteralStringPastOpaqueScanCeilingPDFFixture is the pdf.go:543
// finding's own worked example, made concrete: maxTokenScanIterations
// harmless, cheaply-closed "()" literal strings BEFORE
// "(documentation says /JS)" -- scanPDFLiteralAndHexStringRanges's own
// iteration cap fires while walking the filler, so the range covering the
// target string is never recorded, and a naive scanTokenOccurrences call
// would report "/JS" as found OUTSIDE every known opaque range (a false
// positive) rather than correctly recognizing it sits inside string DATA.
func jsInsideLiteralStringPastOpaqueScanCeilingPDFFixture() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 3 >>\nendobj\n3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n4 0 obj\n<< /Filler ")
	// Each pair below is its own COMPLETE, top-level literal string (not
	// nested inside a wrapping "(...)"): the depth tracker in
	// scanPDFLiteralAndHexStringRanges returns to depth 0 after every pair,
	// so each one costs its OWN iteration of the outer loop -- exactly
	// maxTokenScanIterations of them exhausts the cap before the target
	// string below is ever reached.
	//
	// Every 16th pair is "(<)" instead of "()": scanPDFLiteralAndHexStringRanges
	// also probes for the NEXT '<' on every iteration (to decide paren-vs-hex-
	// string precedence), and a '(' is always immediately at hand here, so
	// that probe's result never actually changes which branch is taken -- but
	// bytes.IndexByte still has to WALK to find it. Scattering a harmless '<'
	// DATA byte (not a delimiter -- the depth tracker inside a "(...)" string
	// only special-cases '\\', '(', ')') every 32 bytes keeps that probe O(1)
	// instead of degrading into the O(n) remaining-buffer scan a fixture with
	// no '<' anywhere until the very end would otherwise force -- a
	// pre-existing, out-of-scope cost this test must not pay to prove its own
	// point.
	const block = 16
	pairs := make([]byte, 0, maxTokenScanIterations*2)
	for i := 0; i < maxTokenScanIterations; i++ {
		if i%block == 0 {
			pairs = append(pairs, '(', '<', ')')
		} else {
			pairs = append(pairs, '(', ')')
		}
	}
	buf.Write(pairs)
	buf.WriteString(" /Title (documentation says /JS) >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")
	return buf.Bytes()
}

// TestAnalyzePDFOpaqueRangeScanCeilingSuppressesFalsePositiveFinding proves
// the pdf.go:543 fix: a risk-token match "found" only because the opaque-
// range scan that would have filtered it out was itself truncated by its
// own iteration cap must NEVER become a critical Finding, and must NOT fall
// back to StatusClean either -- both directions the finding names
// explicitly ("the filter... has no right to confirm a finding OR declare
// clean").
func TestAnalyzePDFOpaqueRangeScanCeilingSuppressesFalsePositiveFinding(t *testing.T) {
	data := jsInsideLiteralStringPastOpaqueScanCeilingPDFFixture()
	r := bytes.NewReader(data)
	res := AnalyzePDF(r, int64(len(data)), int64(len(data)), nil)

	if _, isFinding := findingFor(res, CodePDFJavaScript); isFinding {
		t.Fatal("got a critical Finding for pdf_javascript -- the opaque-range scan that would have recognized this match sits inside a literal string was itself truncated by its own cap, so this is exactly the false-positive class the literal/hex-string filter exists to close")
	}
	c, ok := checkFor(res, CodePDFJavaScript)
	if !ok {
		t.Fatal("no Check for pdf_javascript")
	}
	if c.Status != StatusUnknown {
		t.Fatalf("Check(pdf_javascript).Status = %s, want %s -- the opaque-range scan's own ceiling means this scan cannot confirm the match is real dictionary syntax, and it must not fall back to declaring the file clean either", c.Status, StatusUnknown)
	}
	if c.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Check(pdf_javascript).Reason = %s, want %s", c.Reason, ReasonWorkCeilingBytes)
	}
}

// --- PR #15 review: the admission gate must not reject on payload ---------

// buildPDFWithPagesMarker assembles a PDF whose real page tree declares
// realCount, optionally embedding extra bytes (a content stream payload, a
// comment, a second revision) that a substring scan would also read as a Pages
// dictionary.
func buildPDFWithPagesMarker(t *testing.T, realCount int, extra string) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Count %d /Kids [3 0 R] >>\nendobj\n", realCount))
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
	if extra != "" {
		b.WriteString(extra)
	}
	b.WriteString("trailer\n<< /Root 1 0 R >>\nstartxref\n0\n%%EOF\n")
	return b.Bytes()
}

// TestPDFPageCountIgnoresPayloadThatSpellsAPagesDict is the regression for the
// false-rejection finding on PR #15.
//
// While page_count was only ever REPORTED, a marker found inside a content
// stream was a cosmetic inaccuracy. Feeding an admission gate that returns 422,
// it became a valid document refused: measured at 300 pages for a file whose
// page tree declares 2, because the walk -- unlike the risk-token scan beside
// it -- did not filter its matches through the opaque-range set.
func TestPDFPageCountIgnoresPayloadThatSpellsAPagesDict(t *testing.T) {
	payload := "BT (/Type /Pages /Count 300) Tj ET\n"
	stream := fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(payload), payload)

	cases := []struct {
		name  string
		extra string
		why   string
	}{
		{"content stream payload", stream,
			"a stream body is payload; the bytes inside it are not structure"},
		{"comment", "% /Type /Pages /Count 300\n",
			"a comment is not a dictionary, however it reads"},
		{"literal string", "6 0 obj\n(/Type /Pages /Count 300)\nendobj\n",
			"a literal string is data, even one that spells a marker"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildPDFWithPagesMarker(t, 2, tc.extra)
			tr := NewTracker(Budget{MaxBytes: 8 << 20})

			count, reason := PDFPageCount(bytes.NewReader(raw), int64(len(raw)), 8<<20, tr)

			// EXACTLY 2 -- not "2 or an abstention", and the difference is what
			// makes this test able to fail at all.
			//
			// The first version accepted an abstention as "at least it did not
			// reject", and it passed with the opaque filtering ripped out
			// entirely: without filtering the walk finds BOTH 2 and 300, they
			// disagree, the gate's unambiguity rule abstains, and the test took
			// that for success. Two independent mechanisms were masking each
			// other and neither was under test. Found by running the inversion,
			// not by reading the test.
			//
			// Requiring the definite answer isolates the filtering: only a walk
			// that discards the payload sees ONE marker and can say 2. It is
			// also the behaviour the gate actually needs -- if every PDF with a
			// text-drawing stream abstained, the admission gate would refuse
			// nothing and the S3 write it exists to save would always be spent.
			if count == nil {
				t.Fatalf("PDFPageCount abstained (reason %q), want a definite 2 -- %s. An abstention here "+
					"means the payload was still seen as a second, disagreeing marker", reason, tc.why)
			}
			if *count != 2 {
				t.Fatalf("PDFPageCount = %d, want 2 -- %s. A gate that rejects on this number turns a "+
					"valid document into a 422", *count, tc.why)
			}
		})
	}
}

// TestPDFPageCountAbstainsWhenMarkersDisagree pins the gate's own stricter
// rule, which is deliberately NOT AnalyzePDF's.
//
// AnalyzePDF publishes the last count it saw: for a characteristic, the
// revision closest to EOF is the best available answer. The admission gate
// refuses that tie-break, because "several structurally-plausible answers, we
// took the last" is not a basis on which to reject a client's upload.
func TestPDFPageCountAbstainsWhenMarkersDisagree(t *testing.T) {
	// A second, disagreeing Pages dict OUTSIDE any opaque range -- the shape an
	// incremental update produces.
	raw := buildPDFWithPagesMarker(t, 2, "7 0 obj\n<< /Type /Pages /Count 900 /Kids [] >>\nendobj\n")
	tr := NewTracker(Budget{MaxBytes: 8 << 20})

	count, _ := PDFPageCount(bytes.NewReader(raw), int64(len(raw)), 8<<20, tr)
	if count != nil {
		t.Fatalf("PDFPageCount = %d on a file with two disagreeing Pages dictionaries -- the gate must "+
			"abstain rather than pick one and refuse an upload on it", *count)
	}

	// And the characteristic path still answers, because it is answering a
	// different question. If this half ever starts abstaining too, inspect's
	// page_count silently became null for every incrementally-updated PDF.
	res := AnalyzePDF(bytes.NewReader(raw), int64(len(raw)), 8<<20, NewTracker(Budget{MaxBytes: 8 << 20}))
	if res.Characteristics["page_count"] == nil {
		t.Error("AnalyzePDF now abstains on a multi-revision file too -- the gate's stricter rule leaked " +
			"into the characteristic, which must keep publishing its best available answer")
	}
}
