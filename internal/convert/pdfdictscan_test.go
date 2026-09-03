package convert

import (
	"bytes"
	"fmt"
	"testing"
)

// TestPDFTypedDictUintsXrefOverridesSpeculativeOffsets pins the precedence
// found by review (PR #15 round 4).
//
// scanPDF resynchronises to the next "endstream" when a stream's /Length
// cannot be resolved on its first pass, and while doing so it can record an
// object header that is really PAYLOAD. A file can plant one deliberately. If
// the speculative index outranks the xref, that decoy decides what an indirect
// value resolves to -- the opposite of how a reader resolves references.
//
// The fixture below is that file: the page tree's /Count is indirect through
// object 9, a stream whose /Length is a forward reference carries a fake
// "9 0 obj 300" in its payload, and the real object 9 (holding 2) is what the
// xref points at.
func TestPDFTypedDictUintsXrefOverridesSpeculativeOffsets(t *testing.T) {
	var b bytes.Buffer
	off := func() int { return b.Len() }

	b.WriteString("%PDF-1.4\n")

	o1 := off()
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	o2 := off()
	b.WriteString("2 0 obj\n<< /Type /Pages /Count 9 0 R /Kids [3 0 R] >>\nendobj\n")
	o3 := off()
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")

	// The real object 9, declared BEFORE the decoy on purpose.
	o9 := off()
	b.WriteString("9 0 obj\n2\nendobj\n")

	// A stream whose length is a FORWARD reference (object 8, defined after),
	// so the first pass cannot resolve it and RESYNCHRONISES to the next
	// "endstream" -- which the payload provides EARLY, on purpose. The scan
	// then resumes inside the payload and reads the decoy header that follows
	// as if it were structure.
	//
	// ORDER IS THE WHOLE FIXTURE. scanPDF's index is last-write-wins, so a
	// decoy placed BEFORE the real object 9 is simply overwritten by it and
	// nothing is proved -- the first two versions of this test were arranged
	// that way and their inversions passed. The decoy comes AFTER, which is
	// the only arrangement where the speculative index and the xref disagree.
	o5 := off()
	payload := "junk\nendstream\n9 0 obj\n300\nendobj\n"
	b.WriteString("5 0 obj\n<< /Length 8 0 R >>\nstream\n" + payload + "endstream\nendobj\n")
	o8 := off()
	b.WriteString(fmt.Sprintf("8 0 obj\n%d\nendobj\n", len(payload)))

	xref := off()
	b.WriteString("xref\n0 10\n")
	b.WriteString("0000000000 65535 f \n")
	for _, o := range []int{o1, o2, o3, 0, o5, 0, 0, o8, o9} {
		b.WriteString(fmt.Sprintf("%010d 00000 n \n", o))
	}
	b.WriteString(fmt.Sprintf("trailer\n<< /Size 10 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref))

	values, ceilingHit := PDFTypedDictUints(b.Bytes(), "Pages", "Count", 4096)
	if ceilingHit {
		t.Skip("this build reports a ceiling on the fixture, so the precedence is not exercised here")
	}
	if len(values) != 1 {
		t.Fatalf("PDFTypedDictUints returned %v, want exactly one count", values)
	}
	if values[0] != 2 {
		t.Fatalf("PDFTypedDictUints = %d, want 2 -- the xref points object 9 at the real value; a "+
			"decoy header planted in a stream payload must not outrank the file's own declared "+
			"structure", values[0])
	}
}
