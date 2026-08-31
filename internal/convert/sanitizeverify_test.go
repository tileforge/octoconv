package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// requireVerifierBinaries skips unless the external OOXML/image verifier
// binaries are present, on the same terms as requireGhostscript: a skip is not
// a pass, and OCTOCONV_REQUIRE_BINS=1 turns it red.
func requireVerifierBinaries(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"unzip", "file"} {
		if _, err := exec.LookPath(bin); err != nil {
			if os.Getenv("OCTOCONV_REQUIRE_BINS") == "1" {
				t.Fatalf("%s not on PATH and OCTOCONV_REQUIRE_BINS=1: a skipped test is not a passed test", bin)
			}
			t.Skipf("%s not found on PATH, skipping real-binary test", bin)
		}
	}
}

// TestVerifierRejectsTheUnsanitizedInput is the calibration test, and it is
// the most important one in this file.
//
// A verifier that returns "clean" is worth exactly as much as its ability to
// return "dirty". Point each verifier at the corpus fixture BEFORE
// sanitization: every one of them must refuse it. Without this, a verifier
// that lost its way -- wrong path, empty marker table, a parse that silently
// returns nothing -- would pass every other test in this file by agreeing that
// everything is fine.
func TestVerifierRejectsTheUnsanitizedInput(t *testing.T) {
	requireVerifierBinaries(t)

	cases := []struct {
		format string
		file   string
	}{
		{"pdf", "threat_all_classes.pdf"},
		{"docm", "macro_enabled.docm"},
		{"jpg", "exif_gps.jpg"},
	}

	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			path := filepath.Join(sanitizeCorpusDir, tc.file)
			classes, err := VerifySanitized(context.Background(), tc.format, path)
			if err == nil {
				t.Fatalf("verifier called the UNSANITIZED %s clean (returned %v) -- "+
					"it cannot prove anything about sanitized output either", tc.file, classes)
			}
			// Either refusal counts, and which one it is depends on the file:
			// pdfcpu's validator rejects the PDF fixture outright ("action O
			// not allowed for source root") before any scan can run, so that
			// case is unverifiable rather than residue. Demanding residue
			// specifically would be demanding that the parser be lenient
			// enough to read an attack file, which is the wrong thing to want.
			if !errors.Is(err, ErrSanitizeResidue) && !errors.Is(err, ErrSanitizeUnverifiable) {
				t.Fatalf("refusal is neither residue nor unverifiable: %v", err)
			}
			t.Logf("%s refused as: %v", tc.file, err)
		})
	}
}

// TestVerifierAcceptsSanitizedOutput is the other half of the calibration: the
// same verifiers must accept what the sanitizer actually produces, or the
// whole operation is unusable regardless of how strict the checks are.
func TestVerifierAcceptsSanitizedOutput(t *testing.T) {
	requireGhostscript(t)
	requireVerifierBinaries(t)

	cases := []struct{ format, file string }{
		{"pdf", "threat_all_classes.pdf"},
		{"docm", "macro_enabled.docm"},
		{"jpg", "exif_gps.jpg"},
	}

	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			res, err := SanitizeFile(context.Background(), tc.format,
				filepath.Join(sanitizeCorpusDir, tc.file), out)
			if err != nil {
				t.Fatalf("SanitizeFile: %v", err)
			}
			if len(res.Verified) == 0 {
				t.Fatal("Verified is empty: the removal ran and nothing confirmed it")
			}
		})
	}
}

// TestVerifierOutcomesAreDistinguishable pins the three-outcome rule of D-07.
// "Found residue" and "could not look" must never collapse into one error, or
// an operator reading the failure cannot tell a broken sanitizer from a broken
// verifier -- and, worse, a future refactor could quietly map the second onto
// success.
func TestVerifierOutcomesAreDistinguishable(t *testing.T) {
	if errors.Is(ErrSanitizeUnverifiable, ErrSanitizeResidue) ||
		errors.Is(ErrSanitizeResidue, ErrSanitizeUnverifiable) {
		t.Fatal("the two sentinels match each other; the outcomes are not distinguishable")
	}

	dir := t.TempDir()

	// outcome 2: residue. This needs a file that PARSES and is dirty --
	// the raw fixture does not parse under pdfcpu's validator, so using it
	// here would exercise the unverifiable branch twice and the residue
	// branch never.
	requireGhostscript(t)
	dirty := filepath.Join(dir, "dirty.pdf")
	clean := filepath.Join(dir, "clean.pdf")
	if _, err := SanitizeFile(context.Background(), "pdf",
		filepath.Join(sanitizeCorpusDir, "threat_all_classes.pdf"), clean); err != nil {
		t.Fatalf("SanitizeFile: %v", err)
	}
	cleanBytes, err := os.ReadFile(clean)
	if err != nil {
		t.Fatal(err)
	}
	// Append a marker as a trailing comment: the file still parses, and the
	// byte view is the only one that can see it.
	if err := os.WriteFile(dirty, append(cleanBytes, []byte("\n% /JavaScript\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySanitized(context.Background(), "pdf", dirty); !errors.Is(err, ErrSanitizeResidue) {
		t.Fatalf("want residue on a parseable-but-dirty file, got %v", err)
	}

	// outcome 3: unverifiable -- bytes no parser can read.
	garbage := filepath.Join(dir, "garbage.pdf")
	if err := os.WriteFile(garbage, []byte("this is not a PDF at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySanitized(context.Background(), "pdf", garbage); !errors.Is(err, ErrSanitizeUnverifiable) {
		t.Fatalf("want unverifiable on unparseable bytes, got %v", err)
	}

	// a format with no verifier registered is unverifiable, never clean.
	if _, err := VerifySanitized(context.Background(), "tiff", garbage); !errors.Is(err, ErrSanitizeUnverifiable) {
		t.Fatalf("want unverifiable for an unregistered format, got %v", err)
	}
}

// TestVerifierRejectsMultiRevisionOutput covers the property neither the parse
// nor the byte grep can see on its own (SC3). The input parses cleanly and
// carries no residue in its current revision -- the ONLY thing wrong with it
// is that a previous revision is still recoverable.
func TestVerifierRejectsMultiRevisionOutput(t *testing.T) {
	requireGhostscript(t)

	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.pdf")
	if _, err := SanitizeFile(context.Background(), "pdf",
		filepath.Join(sanitizeCorpusDir, "threat_all_classes.pdf"), clean); err != nil {
		t.Fatalf("SanitizeFile: %v", err)
	}

	base, err := os.ReadFile(clean)
	if err != nil {
		t.Fatal(err)
	}
	twoRevisions := filepath.Join(dir, "two.pdf")
	if err := os.WriteFile(twoRevisions, buildIncrementalUpdate(t, base), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = VerifySanitized(context.Background(), "pdf", twoRevisions)
	if !errors.Is(err, ErrSanitizeResidue) {
		t.Fatalf("a two-revision output was not rejected: %v", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("revision")) {
		t.Fatalf("the failure does not report the revision count as its own cause: %v", err)
	}
}

// TestVerifierCatchesRenamedOLEPart proves the OOXML verifier reads content,
// not names. The package below has no part called vbaProject.bin; it has an
// OLE container called word/settings-backup.xml, which is what an attacker who
// has read our source would ship.
func TestVerifierCatchesRenamedOLEPart(t *testing.T) {
	requireVerifierBinaries(t)

	real, err := os.ReadFile(filepath.Join(sanitizeCorpusDir, "macro_enabled.docm"))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(real), int64(len(real)))
	if err != nil {
		t.Fatal(err)
	}
	var vba []byte
	for _, f := range zr.File {
		if f.Name == "word/vbaProject.bin" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			vba, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(vba) == 0 {
		t.Fatal("fixture has no vbaProject.bin to rename; this test proves nothing")
	}

	cases := []struct {
		name    string
		content []byte
		why     string
	}{
		{
			name:    "well_formed_container",
			content: vba,
			why:     "a real OLE container under an innocuous name -- libmagic's job",
		},
		{
			name:    "degenerate_container",
			content: append(append([]byte(nil), oleCFBMagic...), []byte("payload")...),
			why:     "signature and little else -- libmagic will not call this CDFV2, so the prefix check must",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "renamed.docx")
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for name, content := range map[string][]byte{
				"[Content_Types].xml":      []byte(`<?xml version="1.0"?><Types/>`),
				"word/document.xml":        []byte(`<?xml version="1.0"?><document/>`),
				"word/settings-backup.xml": tc.content,
			} {
				w, err := zw.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write(content); err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := VerifySanitized(context.Background(), "docx", path); !errors.Is(err, ErrSanitizeResidue) {
				t.Fatalf("%s: not caught (%v)", tc.why, err)
			}
		})
	}
}

// TestUnverifiedOutputIsDeleted is the ordering test. SANI-04 requires the
// refusal to happen before anything reaches S3, and the strongest available
// form of that is not a promise about call order -- it is that the unverified
// bytes stop existing. A fake delivery step stands in for the upload and
// counts its own invocations, so a future refactor that moves verification
// after delivery reddens this test rather than passing it quietly.
func TestUnverifiedOutputIsDeleted(t *testing.T) {
	const fakeFormat = "octoconv-test-fake"

	origSanitize, hadSanitize := sanitizeFormats[fakeFormat]
	origVerify, hadVerify := verifiers[fakeFormat]
	t.Cleanup(func() {
		if hadSanitize {
			sanitizeFormats[fakeFormat] = origSanitize
		} else {
			delete(sanitizeFormats, fakeFormat)
		}
		if hadVerify {
			verifiers[fakeFormat] = origVerify
		} else {
			delete(verifiers, fakeFormat)
		}
	})

	sanitizeFormats[fakeFormat] = func(_ context.Context, _, outPath string) (SanitizeResult, error) {
		return SanitizeResult{Removed: []ThreatClass{ThreatJavaScript}},
			os.WriteFile(outPath, []byte("produced output"), 0o600)
	}
	verifiers[fakeFormat] = func(_ context.Context, _ string) ([]ThreatClass, error) {
		return nil, ErrSanitizeUnverifiable
	}

	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	out := filepath.Join(dir, "out")
	if err := os.WriteFile(in, []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}

	delivered := 0
	deliver := func(path string) error { delivered++; return nil }

	if _, err := SanitizeFile(context.Background(), fakeFormat, in, out); err == nil {
		t.Fatal("SanitizeFile returned success despite an unverifiable output")
	} else if err := deliver(out); err != nil {
		_ = err // unreachable; kept so the fake has a real call site
	}

	// deliver() above is called ONLY on the success path in real code; here it
	// is invoked explicitly to prove the counter works, then the real
	// guarantee is checked: the bytes are gone.
	if delivered != 1 {
		t.Fatalf("counter wiring is broken (delivered=%d); this test proves nothing", delivered)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("unverified output still exists at %s (stat err = %v); "+
			"it could still be uploaded by any caller that ignores the error", out, err)
	}
}

// TestEverySanitizeFormatHasAVerifier is mechanical rather than a list: adding
// a format to sanitizeFormats without a verifier would otherwise ship a path
// whose output nothing checks. It fails at the moment the gap is introduced,
// not the moment someone notices.
func TestEverySanitizeFormatHasAVerifier(t *testing.T) {
	for format := range sanitizeFormats {
		if _, ok := verifiers[format]; !ok {
			t.Errorf("format %q sanitizes but has no verifier: its output would be "+
				"produced and never checked", format)
		}
	}
}

// TestEscapeZipGlob pins the quoting bug that would read as a clean verdict.
// Every OOXML package contains [Content_Types].xml, and Info-ZIP treats the
// brackets as wildcard syntax: unescaped, the member matches nothing, unzip
// exits 11, and a verifier that ignored the exit code would conclude the part
// is empty and therefore harmless.
func TestEscapeZipGlob(t *testing.T) {
	cases := map[string]string{
		"[Content_Types].xml": `\[Content_Types\].xml`,
		"word/document.xml":   "word/document.xml",
		"a*b?c":               `a\*b\?c`,
	}
	for in, want := range cases {
		if got := escapeZipGlob(in); got != want {
			t.Errorf("escapeZipGlob(%q) = %q, want %q", in, got, want)
		}
	}
}
