package convert

// This suite is the deliberate opposite of internal/api/pre59_pinning_test.go:
// that one freezes behavior Phase 59 must PRESERVE, this one freezes behavior
// Phase 59 must BREAK.
//
// It pins TODAY's (pre-Phase-59) outcome of a born-digital PDF submitted for
// conversion: a terminal rejection with ErrOCRBornDigital, for EVERY target,
// because ocrPDFGuardStage (ocrpdf.go) does not look at the target at all. The
// point of writing it down is to make the break visible as its own commit
// rather than dissolved into Plan 59-04's diff.
//
// REPLACEMENT PROTOCOL (59-01-PLAN.md Task 3, 59-04-PLAN.md Task 3).
// Plan 59-04 is REQUIRED to replace the assertions below. That is the goal, not
// a violation. What the protocol forbids is doing it quietly:
//
//   - the replacement must land in the SAME commit as the guard change;
//   - that commit message must say the suite was replaced and why;
//   - this header must be rewritten to describe post-phase behavior;
//   - deleting the file, or weakening an assertion so it passes both before and
//     after, is not a replacement -- it is the loss of the evidence.
//
// The measured basis for the rejection, from 59-RESEARCH.md and reproduced by
// the tests below: `pdftotext testdata/ocr/born_digital.pdf -` yields 82
// non-whitespace characters against ocrBornDigitalCharThreshold = 16. The
// rejection therefore fires with a wide margin and does not depend on the exact
// threshold value -- these tests would still pin the same outcome if the
// constant moved anywhere below 82.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pre59BornDigitalFixture is the one input this suite is about: a real
// born-digital PDF with a genuine text layer, committed since Phase 43 and
// already used by the guard's own tests.
func pre59BornDigitalFixture() string {
	return filepath.Join("testdata", "ocr", "born_digital.pdf")
}

// TestPre59PDFToTxtRejectsToday pins the txt target's pre-Phase-59 outcome:
// a terminal ErrOCRBornDigital, and NO output file written.
//
// PATH is restricted to pdfinfo+pdftotext (restrictedPATH, ocrpdf_test.go) so
// the rejection is proven to happen BEFORE any render: if the guard had let
// this through, the failure would be a missing-pdftoppm exec error, which is a
// distinguishable error and not the sentinel asserted here.
//
// This is the assertion Plan 59-04 inverts: after that plan, the same call must
// SUCCEED and write the extracted text layer.
func TestPre59PDFToTxtRejectsToday(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := OCRConverter{}.Convert(ctx, pre59BornDigitalFixture(), outPath, nil)
	if !errors.Is(err, ErrOCRBornDigital) {
		t.Fatalf("Convert(pdf -> txt) on a born-digital PDF = %v, want ErrOCRBornDigital (pre-Phase-59 behavior)", err)
	}
	if _, statErr := os.Stat(outPath); statErr == nil {
		t.Fatalf("%s exists -- today's rejection path must write no output at all", outPath)
	}
}

// TestPre59PDFToHOCRRejectsToday pins the hocr target's pre-Phase-59 outcome,
// and it is the more important of the two.
//
// Today the guard ignores the target, so hocr is refused for exactly the same
// reason txt is. Plan 59-04 (D-06) splits these two outcomes apart: txt gets
// the extracted layer, hocr goes on to a real render because pdftotext produces
// no word or line boxes and OCR is their only source. Without this snapshot,
// "the two targets now diverge" would rest on memory of what hocr used to do.
func TestPre59PDFToHOCRRejectsToday(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.hocr")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := OCRConverter{}.Convert(ctx, pre59BornDigitalFixture(), outPath, nil)
	if !errors.Is(err, ErrOCRBornDigital) {
		t.Fatalf("Convert(pdf -> hocr) on a born-digital PDF = %v, want ErrOCRBornDigital -- today the guard does not look at the target, and that is precisely what Plan 59-04 changes", err)
	}
}

// TestPre59PairAdvertisedToday pins the contradiction BDPD-02 exists to close:
// the pdf->txt pair is advertised by the registry (and therefore offered by
// GET /v1/formats and by POST /v1/inspect's operations block) while the two
// tests above show the worker refusing that exact job terminally.
//
// After Phase 59 this assertion must STILL hold -- but it will have stopped
// being a contradiction. That is why it is pinned as its own case rather than
// folded into the rejection tests: it is the half that survives.
func TestPre59PairAdvertisedToday(t *testing.T) {
	want := Pair{From: "pdf", To: "txt"}
	for _, p := range (OCRConverter{}).Pairs() {
		if p == want {
			return
		}
	}
	t.Fatalf("OCRConverter.Pairs() does not contain %+v -- the whole premise of BDPD-02 is that this pair is already advertised while being refused", want)
}
