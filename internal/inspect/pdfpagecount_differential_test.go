// This file holds ONE test, and it is the only one in this package that asks
// whether the page-count gate is right about real files rather than about
// fixtures written to exercise it.
//
// .planning/RETROSPECTIVE.md records the rule it follows, learned on a
// different bound in this repository after four bypasses: do not predict what a
// parser will say -- run one and tabulate the divergences. Every hand-built
// fixture in pdf_test.go encodes a defect somebody already found; this one can
// fail on a defect nobody has thought of, because the inputs were not written
// with any defect in mind.
package inspect

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pdfinfoPageCount is the reference implementation's answer. pdfinfo is poppler,
// a third codebase entirely, and it is the SAME tool the probe worker counts
// with -- so a divergence here is a divergence between the two gates that
// 63-CONTEXT.md D-04 requires to agree.
func pdfinfoPageCount(path string) (int, bool) {
	out, err := exec.Command("pdfinfo", path).Output()
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "Pages:")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// TestPDFPageCountAgreesWithPdfinfoOnEveryCommittedPDF walks every PDF this
// repository commits -- fixtures written for OCR, for sanitize, for inspect,
// for the e2e suite, none of them written for this gate -- and requires that
// where the gate ANSWERS, it answers what poppler answers.
//
// Abstaining is always acceptable and is recorded rather than asserted: an
// abstention admits the upload and leaves the bound to the worker, which is the
// designed fallback. A CONFIDENT DISAGREEMENT is the failure, because that is
// the shape that rejects a valid document with 422 -- seven distinct ways of
// producing one were found by review on PR #15, and this is the test that would
// have caught them without a reviewer.
func TestPDFPageCountAgreesWithPdfinfoOnEveryCommittedPDF(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not on PATH; this test needs the reference implementation")
	}

	roots := []string{
		"testdata/pdf",
		"../convert/testdata/sanitize",
		"../convert/testdata/ocr",
		"../e2e/testdata",
		"../../scripts/fixtures",
	}
	var files []string
	for _, r := range roots {
		m, _ := filepath.Glob(filepath.Join(r, "*.pdf"))
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Fatal("no committed PDF fixtures found -- the roots above no longer resolve, and this " +
			"test is silently covering nothing")
	}

	answered := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		want, ok := pdfinfoPageCount(f)
		if !ok {
			continue // poppler declines it too; nothing to compare
		}

		got, reason := PDFPageCount(bytes.NewReader(raw), int64(len(raw)), 8<<20,
			NewTracker(Budget{MaxBytes: 8 << 20}))
		if got == nil {
			t.Logf("%-34s pdfinfo=%-4d gate=abstain(%s)", filepath.Base(f), want, reason)
			continue
		}
		answered++
		if *got != want {
			t.Errorf("%s: gate says %d pages, pdfinfo says %d -- a confident disagreement is what "+
				"rejects a valid document with 422", filepath.Base(f), *got, want)
		}
	}

	// If the gate abstained on everything, the loop above asserted nothing and
	// would keep passing while the walk rotted.
	if answered == 0 {
		t.Fatal("the gate abstained on every committed PDF -- this test then proves nothing, and " +
			"the admission gate would be refusing no uploads at all")
	}
	t.Logf("answered confidently on %d/%d comparable fixtures, all agreeing with poppler",
		answered, len(files))
}
