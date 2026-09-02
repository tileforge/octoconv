package convert

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The ICC and PDF entries were removed as SANI-07 and SANI-08 landed; their
// transcripts are in those commit messages and in testdata/sanitize/PROVENANCE.md.

// The "before" transcript for phase 63. Each test here asserts what the code
// does TODAY with the three new fixtures, so the after-state has something to
// be compared against. Two of the three findings were reported by review and
// reasoned about, never observed -- and a fix for a defect nobody reproduced is
// a fix for a defect nobody has shown exists.
//
// This file is deleted by the commit that lands the fixes. It exists to be run
// once, on the tree before them.

func TestBefore63OOXMLVerifierAcceptsAnEmptyPackage(t *testing.T) {
	requireVerifierBinaries(t)

	src := filepath.Join(sanitizeCorpusDir, "ooxml_rel_targets_main.docx")
	out := filepath.Join(t.TempDir(), "out.docx")
	res, err := SanitizeFile(context.Background(), "docx", src, out)
	if err != nil {
		t.Logf("BEFORE: SanitizeFile returned %v", err)
		t.Skip("the sanitizer already refuses this input; the finding is about the verifier accepting it")
	}
	t.Logf("BEFORE: SanitizeFile succeeded, removed=%v", res.Removed)

	blob, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("word/document.xml")) {
		t.Error("the main part survived; this fixture does not reproduce the finding")
	} else {
		t.Log("BEFORE: the sanitized package has NO word/document.xml and was still delivered")
	}
}
