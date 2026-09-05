package convert

import (
	"strings"
	"testing"
)

// TestUndefinedReferencesReadsLaTeXsOwnWarning is the round-2 half of a check
// this PR has now got wrong in both directions, each time caught by running it.
//
//	first version   matched the whole log  -> failed 64-04's paper and both
//	                                          bibliography corpus documents,
//	                                          which resolve everything by the
//	                                          last pass
//	second version  matched the phrase     -> a document can PRINT the phrase
//	                                          (\typeout), and a valid PDF was
//	                                          thrown away for it
//
// Both fixtures are real runs of the shipped image, captured before this test
// was written: testdata/tectonic/PROVENANCE.md.
func TestUndefinedReferencesReadsLaTeXsOwnWarning(t *testing.T) {
	t.Run("a document with genuinely undefined references", func(t *testing.T) {
		out := readTectonicFixture(t, "undefined-references.txt")
		if !strings.Contains(string(out), "LaTeX Warning: There were undefined references.") {
			t.Fatal("the fixture no longer carries LaTeX's warning -- it cannot prove the positive case")
		}
		if !tectonicUndefinedReferences(out) {
			t.Fatal("a PDF full of ?? would be published as a finished job")
		}
	})

	t.Run("a document that merely prints the phrase", func(t *testing.T) {
		out := readTectonicFixture(t, "forged-undefined-references.txt")
		// The fixture's value is exactly this: the phrase is present, and
		// LaTeX's warning is not.
		if !strings.Contains(string(out), "There were undefined references") {
			t.Fatal("the fixture no longer contains the phrase -- it cannot prove the negative case")
		}
		if strings.Contains(string(out), "LaTeX Warning: There were undefined") {
			t.Fatal("the fixture contains the real warning after all; it is the wrong capture for this test")
		}
		if tectonicUndefinedReferences(out) {
			t.Fatal("\\typeout of the phrase was believed: this document typesets correctly and its " +
				"PDF would be discarded with a terminal error")
		}
	})
}

// TestUndefinedReferencesStaysOnTheFinalPass keeps the scope that made the
// first version wrong. The warning is NORMAL on early passes -- the .aux does
// not exist yet -- so a check that reads the whole log fails every multi-pass
// document, including this phase's own end-to-end proof.
func TestUndefinedReferencesStaysOnTheFinalPass(t *testing.T) {
	early := "LaTeX Warning: There were undefined references.\n" +
		"Transcript written on doc.log.\n" +
		"note: Rerunning TeX ...\n" +
		"[1] (doc.aux)\n" +
		"Transcript written on doc.log.\n" +
		"note: Writing `doc.pdf`\n"
	if tectonicUndefinedReferences([]byte(early)) {
		t.Fatal("a warning from an EARLY pass was counted: every document with a \\ref would fail")
	}

	late := "[1] (doc.aux)\n" +
		"Transcript written on doc.log.\n" +
		"note: Rerunning TeX ...\n" +
		"LaTeX Warning: There were undefined references.\n" +
		"Transcript written on doc.log.\n"
	if !tectonicUndefinedReferences([]byte(late)) {
		t.Fatal("a warning from the FINAL pass was missed")
	}
}

// TestTectonicTailIsBoundedInBytes covers the path round 2 did not: the log
// BUFFER was capped, and the slice of it that gets written into
// job_events.detail for every failure was still bounded only by a line count.
// TeX wraps terminal output, so in practice twelve lines is small -- but a
// database row should not rest on "in practice".
func TestTectonicTailIsBoundedInBytes(t *testing.T) {
	oneHugeLine := make([]byte, 1<<20)
	for i := range oneHugeLine {
		oneHugeLine[i] = 'A'
	}
	got := tectonicTail(oneHugeLine)
	if len(got) > 4200 {
		t.Fatalf("tectonicTail returned %d bytes from a single 1 MiB line: that string is persisted "+
			"per failed job", len(got))
	}
	if !strings.HasSuffix(got, "AAA") {
		t.Fatalf("the END was not kept; the cause of a TeX failure is at the end of its log")
	}

	// The ordinary case must be untouched -- a truncating tail that also
	// mangles normal output would hide every real diagnostic.
	normal := "! LaTeX Error: File `pgfplots.sty' not found.\nEmergency stop.\n"
	if tail := tectonicTail([]byte(normal)); !strings.Contains(tail, "pgfplots.sty") {
		t.Fatalf("normal output was damaged: %q", tail)
	}
}
