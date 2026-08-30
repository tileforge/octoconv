package convert

// REPLACED by Plan 59-04, in the same commit as the guard change, per the
// protocol the pre-phase version of this file carried.
//
// What it used to pin (committed by Plan 59-01, commit b9127a9): a born-digital
// PDF was refused terminally with ErrOCRBornDigital for EVERY target, because
// ocrPDFGuardStage did not look at the target at all. That snapshot existed so
// the break would be visible as its own commit rather than dissolved into a
// diff, and so "the targets now diverge" would rest on a recorded baseline
// rather than on memory of what hocr used to do.
//
// What it pins now, after D-05/D-06:
//
//   - pdf -> txt on a born-digital PDF SUCCEEDS and returns the extracted text
//     layer. The same pdftotext output the old guard computed, counted, and
//     threw away.
//   - pdf -> hocr on the same file is NOT refused for having a text layer; it
//     proceeds to a render, because pdftotext produces no word or line boxes
//     and OCR is their only source.
//   - Pairs() still advertises pdf -> txt. That assertion is unchanged from the
//     pre-phase version and is the half that survives -- it has simply stopped
//     being a contradiction.
//
// The replacement is the goal, not a violation. What the protocol forbade was
// doing it quietly: deleting the file, or weakening an assertion until it
// passed both before and after. Neither happened -- the assertions are
// inverted, and the commit message says so.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pre59BornDigitalFixture is the one input this suite is about: a real
// born-digital PDF with a genuine text layer, committed since Phase 43 and
// already used by the guard's own tests. `pdftotext <it> -` yields 82
// non-whitespace characters against ocrUsableTextLayerThreshold = 16, so the
// routing decision has a wide margin and does not depend on the exact
// threshold value.
func pre59BornDigitalFixture() string {
	return filepath.Join("testdata", "ocr", "born_digital.pdf")
}

// TestPre59PDFToTxtReturnsTheTextLayer is the inverted assertion: what used to
// be a terminal rejection is now the answer.
//
// PATH is restricted to pdfinfo+pdftotext, which does double duty. It proves
// the decision still happens BEFORE any render (an implementation that fell
// through to rasterizing would fail with a missing-pdftoppm exec error, which
// is distinguishable from success), and it proves the extraction route needs
// nothing beyond the two binaries the old rejection route already used --
// D-05's "no new binary, no new container" claim, checked rather than asserted.
//
// The expected content is compared against a LIVE pdftotext run rather than
// against the literal 82: a test pinned to the constant would break if the
// fixture were ever replaced, while this one keeps checking the property that
// actually matters -- that we return exactly what we extracted.
func TestPre59PDFToTxtReturnsTheTextLayer(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := (OCRConverter{}).Convert(ctx, pre59BornDigitalFixture(), outPath, nil); err != nil {
		t.Fatalf("Convert(pdf -> txt) on a born-digital PDF = %v, want success -- the extracted text layer IS the result (D-05)", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read %s: %v", outPath, err)
	}
	if countNonWhitespace(got) == 0 {
		t.Fatalf("output is empty -- the route returned nothing where the text layer should be; got %q", got)
	}
	if !strings.Contains(string(got), "born-digital PDF") {
		t.Fatalf("output does not contain the fixture's own text; got %q", got)
	}

	// The count that chose this route and the bytes that were written must be
	// the same pdftotext answer (<specifics>).
	_, want, err := extractPDFTextLayer(ctx, pre59BornDigitalFixture())
	if err != nil {
		t.Fatalf("reference extraction failed: %v", err)
	}
	if n := countNonWhitespace(got); n != want {
		t.Fatalf("written output has %d non-whitespace characters, a live pdftotext run yields %d -- these must be "+
			"the same invocation's answer, not two", n, want)
	}
}

// TestPre59PDFToHOCRIsNotRefusedForHavingATextLayer is the more important
// inversion of the two.
//
// Before this phase, hocr was refused for exactly the same reason txt was --
// the guard never looked at the target. D-06 splits them: a client asking for
// hocr wants word and line bounding boxes, pdftotext does not produce them in
// any form, and refusing the request was refusing the only route that could
// satisfy it.
//
// The assertion is deliberately on the SENTINEL, not on success: with PATH
// restricted, pdftoppm is absent and the render cannot complete. What must be
// true is that the failure is a missing-renderer failure -- proof the guard
// let it through -- and never ErrOCRBornDigital.
func TestPre59PDFToHOCRIsNotRefusedForHavingATextLayer(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.hocr")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := (OCRConverter{}).Convert(ctx, pre59BornDigitalFixture(), outPath, nil)
	// Checked by message rather than by sentinel: Plan 59-06 deleted
	// ErrOCRBornDigital outright, so the identifier no longer exists to compare
	// against. That deletion is the stronger guarantee -- the compiler now
	// enforces it -- and this remains as the behavioral half, since a refusal
	// reintroduced under a NEW sentinel would still be caught here.
	if err != nil && strings.Contains(err.Error(), "already has an extractable text layer") {
		t.Fatalf("Convert(pdf -> hocr) = %v -- a text layer must no longer refuse a box-producing target, since OCR "+
			"is the only thing that can produce boxes (D-06)", err)
	}
	if err == nil || !strings.Contains(err.Error(), "pdftoppm") {
		t.Fatalf("Convert(pdf -> hocr) = %v, want a pdftoppm failure -- with PATH restricted the render cannot "+
			"complete, and reaching pdftoppm at all is the proof the guard let this target through to a render", err)
	}
}

// TestPre59PairAdvertisedToday is UNCHANGED from the pre-phase version, and
// that is the point. It was pinned as its own case precisely because it is the
// half that survives the phase: pdf -> txt was always advertised. Before, that
// sat beside a terminal refusal and was a contradiction. Now it does not.
func TestPre59PairAdvertisedToday(t *testing.T) {
	want := Pair{From: "pdf", To: "txt"}
	for _, p := range (OCRConverter{}).Pairs() {
		if p == want {
			return
		}
	}
	t.Fatalf("OCRConverter.Pairs() does not contain %+v -- the whole premise of BDPD-02 is that this pair is advertised", want)
}
