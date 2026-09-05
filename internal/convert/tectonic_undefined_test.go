package convert

import (
	"os"
	"path/filepath"
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

// TestValidateTectonicBundleRefusesWhatStatAccepts is the round-3 finding as an
// invariant. os.Stat succeeds for a directory and for a file this process
// cannot open; both make the worker start, log its bundle, and consume the
// queue while unable to typeset anything.
func TestValidateTectonicBundleRefusesWhatStatAccepts(t *testing.T) {
	dir := t.TempDir()

	t.Run("a directory", func(t *testing.T) {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("precondition: %v", err)
		}
		if err := ValidateTectonicBundle(dir); err == nil {
			t.Fatal("a directory passed validation -- os.Stat accepts it, which is the whole point")
		}
	})

	t.Run("an empty file", func(t *testing.T) {
		empty := filepath.Join(dir, "empty.ttb")
		if err := os.WriteFile(empty, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ValidateTectonicBundle(empty); err == nil {
			t.Fatal("a zero-byte bundle passed validation")
		}
	})

	t.Run("an unreadable file", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root; permission bits do not apply")
		}
		locked := filepath.Join(dir, "locked.ttb")
		if err := os.WriteFile(locked, []byte("bundle"), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(locked); err != nil {
			t.Fatalf("precondition: stat should still succeed, got %v", err)
		}
		if err := ValidateTectonicBundle(locked); err == nil {
			t.Fatal("a file this process cannot open passed validation -- the worker runs as nobody, " +
				"and a bundle readable only by root stats perfectly")
		}
	})

	t.Run("a real file passes", func(t *testing.T) {
		ok := filepath.Join(dir, "ok.ttb")
		if err := os.WriteFile(ok, []byte("bundle bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ValidateTectonicBundle(ok); err != nil {
			t.Fatalf("a readable regular file was refused: %v", err)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if err := ValidateTectonicBundle(""); err == nil {
			t.Fatal("an unset path passed validation; tectonic's fallback is a NETWORK FETCH")
		}
	})
}

// TestDeniedPackagesAreFoundWhateverTheFileIsCalled is PR #20's P1.
//
// The first version of the job-directory scan filtered on .tex/.sty/.cls/.ltx.
// TeX does not: "\input{preamble}" reads a file named exactly "preamble".
// Measured before the fix -- an extensionless upload carrying
// \usepackage{fontspec} was read, the package loaded from the bundle, and a PDF
// came out, defeating a refusal this deployment documents.
func TestDeniedPackagesAreFoundWhateverTheFileIsCalled(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "in.latex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\input{noext}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No extension at all, which is exactly what \input{noext} reads.
	if err := os.WriteFile(filepath.Join(dir, "noext"), []byte("\\usepackage{fontspec}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found := ""
	for _, p := range jobFilesBeside(main) {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if _, textShaped := ValidateSniffPrefix(src, int64(len(src))); !textShaped {
			continue
		}
		if pkg := TeXDeniedInSource(src); pkg != "" {
			found = pkg
			break
		}
	}
	if found != "fontspec" {
		t.Fatalf("the denied package was not found (got %q): a name is not what makes a file TeX "+
			"source, and deciding by extension is a bypass", found)
	}
}

// TestBinaryAttachmentsAreNotScannedForPackages is the other half, and the one
// that keeps the fix above from becoming the defect it replaced. A figure that
// happens to contain the bytes of a \usepackage line must not refuse a valid
// job -- so the scan is by what the file IS (text-shaped: no NUL, valid UTF-8),
// which is the same rule the API's own sniffing uses.
func TestBinaryAttachmentsAreNotScannedForPackages(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "in.latex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A "figure" whose bytes contain a denied directive AND a NUL, which is
	// what makes it binary.
	fig := append([]byte("%PDF-1.4\n\x00 stream \\usepackage{fontspec} endstream\n"), 0x00, 0xff)
	if err := os.WriteFile(filepath.Join(dir, "fig.pdf"), fig, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, p := range jobFilesBeside(main) {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if _, textShaped := ValidateSniffPrefix(src, int64(len(src))); !textShaped {
			continue
		}
		if pkg := TeXDeniedInSource(src); pkg != "" {
			t.Fatalf("a binary attachment was read as TeX source and refused the job over %q: "+
				"refusing valid work is the failure this check must not have", pkg)
		}
	}
}
