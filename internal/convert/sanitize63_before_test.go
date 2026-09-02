package convert

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The ICC entry was removed when SANI-07 landed; its transcript is in that
// commit message and in testdata/sanitize/PROVENANCE.md.

// The "before" transcript for phase 63. Each test here asserts what the code
// does TODAY with the three new fixtures, so the after-state has something to
// be compared against. Two of the three findings were reported by review and
// reasoned about, never observed -- and a fix for a defect nobody reproduced is
// a fix for a defect nobody has shown exists.
//
// This file is deleted by the commit that lands the fixes. It exists to be run
// once, on the tree before them.

func TestBefore63StreamRangeStopsAtTheSubstring(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(sanitizeCorpusDir, "endstream_in_stream.pdf"))
	if err != nil {
		t.Fatal(err)
	}

	ranges := pdfStreamRanges(raw)
	if len(ranges) == 0 {
		t.Fatal("no stream ranges found at all")
	}
	// The trap bytes sit AFTER the first "endstream" in the payload, so a
	// substring-derived range excludes them while the /Length-derived range
	// includes them.
	trapAt := bytes.Index(raw, []byte("startxref and %%EOF too"))
	if trapAt < 0 {
		t.Fatal("fixture does not contain the trap text")
	}
	covered := inAnyRange(ranges, trapAt)
	t.Logf("BEFORE: first stream range = %v, trap at %d, covered = %v", ranges[0], trapAt, covered)
	if covered {
		t.Error("the trap bytes are inside the protected range; the substring scan " +
			"did not truncate it, so this fixture does not reproduce the finding")
	}
}

func TestBefore63RevisionCountReadsThePayloadAsStructure(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(sanitizeCorpusDir, "endstream_in_stream.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	outside := pdfNonStreamBytes(raw)

	// The file has two of each token: one inside the stream payload, one in
	// the real trailer. A correct blanking keeps the trailer's and removes the
	// payload's. Today it does the exact opposite.
	payloadStart := bytes.Index(raw, []byte("startxref and %%EOF too"))
	realStart := bytes.LastIndex(raw, []byte("startxref"))
	if payloadStart < 0 || realStart <= payloadStart {
		t.Fatal("fixture does not have a payload marker followed by a real one")
	}

	payloadSurvived := bytes.Contains(outside[payloadStart:payloadStart+30], []byte("startxref"))
	realSurvived := bytes.Contains(outside[realStart:realStart+9], []byte("startxref"))

	t.Logf("BEFORE: payload's startxref survived blanking = %v (want false)", payloadSurvived)
	t.Logf("BEFORE: the REAL trailer's startxref survived blanking = %v (want true)", realSurvived)
	t.Logf("BEFORE: raw tail  %q", raw[len(raw)-40:])
	t.Logf("BEFORE: blanked   %q", outside[len(outside)-40:])

	if !payloadSurvived && realSurvived {
		t.Error("blanking already keeps structure and removes payload; this fixture " +
			"does not reproduce the finding")
	}
	// Stated for the record rather than asserted, because it is the finding:
	// counting 1 and 1 here is arrived at by reading the payload as structure
	// AND the structure as payload -- two errors whose totals happen to agree
	// with the correct answer.
	t.Logf("BEFORE: startxref=%d %%%%EOF=%d in the blanked bytes",
		bytes.Count(outside, []byte("startxref")), bytes.Count(outside, []byte("%%EOF")))
}

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
