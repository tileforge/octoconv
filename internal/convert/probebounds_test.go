package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestEnforceProbeMaxPages walks the boundary rather than sampling near it:
// max-1, max, max+1 and the unconfigured zero. Off-by-one at a ceiling is the
// defect this shape exists to catch, and "somewhere below"/"somewhere above"
// cases cannot catch it.
func TestEnforceProbeMaxPages(t *testing.T) {
	const max = 200

	cases := []struct {
		name   string
		count  int
		max    int
		reject bool
		why    string
	}{
		{"one under the bound", max - 1, max, false,
			"199 pages against a 200 bound is work the service accepted when the timeout was derived"},
		{"exactly the bound", max, max, false,
			"the bound is inclusive: PROBE_ENGINE_TIMEOUT was derived FOR 200 pages, so 200 pages must run"},
		{"one over the bound", max + 1, max, true,
			"the first count the ceiling exists to refuse"},
		{"far over the bound", 3000, max, true,
			"the case from the phase-60 measurement: ten minutes of work, then a TRANSIENT failure that retries"},
		{"no cap configured", 3000, 0, false,
			"max == 0 is 'not configured', never a fail-closed reject-all -- ocrguards.go's own convention"},
		{"negative bound", 3000, -1, false,
			"a nonsensical bound must not become a reject-all either"},
		{"zero pages under a bound", 0, max, false,
			"a page count of zero is a question for the sanitizer, not for the ceiling"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EnforceProbeMaxPages(tc.count, tc.max)
			if !tc.reject {
				if err != nil {
					t.Fatalf("EnforceProbeMaxPages(%d, %d) = %v, want nil -- %s", tc.count, tc.max, err, tc.why)
				}
				return
			}
			if err == nil {
				t.Fatalf("EnforceProbeMaxPages(%d, %d) = nil, want rejection -- %s", tc.count, tc.max, tc.why)
			}
			if !errors.Is(err, ErrProbeTooManyPages) {
				t.Fatalf("EnforceProbeMaxPages(%d, %d) error does not wrap ErrProbeTooManyPages: %v", tc.count, tc.max, err)
			}
			// Both numbers, like OCR's message: an operator reading a failed
			// job needs what the file had AND what the bound was, or they
			// cannot tell "raise the ceiling" from "split the document".
			msg := err.Error()
			if !strings.Contains(msg, strconv.Itoa(tc.count)) || !strings.Contains(msg, strconv.Itoa(tc.max)) {
				t.Fatalf("EnforceProbeMaxPages(%d, %d) message %q does not carry both the count and the bound",
					tc.count, tc.max, msg)
			}
		})
	}
}

// TestProbeSentinelIsNotTheOCRSentinel pins 63-CONTEXT.md D-04's one
// deliberate NON-sharing.
//
// The two engines report the same client-facing CODE on purpose, and a reader
// who knows that is one step from "so share the sentinel too". They must not:
// isOCRTerminal and ocrFailureCode both branch on errors.Is against the OCR
// sentinel, so a shared error value would silently hand the probe path OCR's
// classification and OCR's message -- on a bound that is a different number
// derived from a different measurement.
func TestProbeSentinelIsNotTheOCRSentinel(t *testing.T) {
	if errors.Is(EnforceProbeMaxPages(10, 1), ErrOCRTooManyPages) {
		t.Error("a probe page-ceiling error satisfies errors.Is(err, ErrOCRTooManyPages) -- " +
			"the two engines share a code, never a sentinel (D-04)")
	}
	if errors.Is(enforceMaxPages(10, 1), ErrProbeTooManyPages) {
		t.Error("an OCR page-ceiling error satisfies errors.Is(err, ErrProbeTooManyPages) -- " +
			"the two engines share a code, never a sentinel (D-04)")
	}
}

// TestProbeMaxPagesHasOneDefault is the anti-drift guard 63-CONTEXT.md D-04
// requires, aimed at the half that has actually broken before.
//
// This phase deliberately enforces ONE rule in TWO processes. The failure mode
// is not that one of them forgets the comparison -- that is loud. It is that
// each grows its own default literal and the two diverge, which is silent, and
// which is exactly what happened to this same engine's OTHER bound: cmd/api
// omitted PROBE_ENGINE_TIMEOUT entirely and published estimated_max_seconds: 0
// while the worker ran with its own number. Nothing failed until a human read
// the diff (PR #12).
//
// So: every non-test Go file that READS PROBE_MAX_PAGES must resolve its
// default through DefaultProbeMaxPages, never through a literal of its own.
func TestProbeMaxPagesHasOneDefault(t *testing.T) {
	// The QUOTED form, not the bare name. A bare-name scan matches every
	// doc comment that mentions the ceiling -- and this phase wrote several,
	// deliberately, because the two gates are supposed to explain themselves.
	// A Go string literal is what a READER of the variable has, and nothing
	// else in this tree spells it that way.
	const envLiteral = `"PROBE_MAX_PAGES"`

	readers := map[string]string{}
	root := repoRootForProbeScan(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), envLiteral) {
			rel, _ := filepath.Rel(root, path)
			readers[rel] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(readers) == 0 {
		t.Fatal("no non-test Go source reads PROBE_MAX_PAGES -- either the env var was renamed or " +
			"this scan no longer reaches the source tree, and in both cases the anti-drift guarantee is gone")
	}
	names := make([]string, 0, len(readers))
	for k := range readers {
		names = append(names, k)
	}
	sort.Strings(names)

	if len(readers) < 2 {
		t.Errorf("PROBE_MAX_PAGES is read in %d place(s): %v -- the ceiling is enforced in BOTH cmd/api "+
			"and cmd/probe-worker (D-04), so a single reader means one of the two gates lost its bound",
			len(readers), names)
	}
	for _, name := range names {
		if !strings.Contains(readers[name], "DefaultProbeMaxPages") {
			t.Errorf("%s reads PROBE_MAX_PAGES but does not resolve its default through "+
				"DefaultProbeMaxPages -- a second default literal is how PROBE_ENGINE_TIMEOUT drifted "+
				"between these same two processes", name)
		}
	}
}

// repoRootForProbeScan walks up from this package's directory to the module
// root (the directory carrying go.mod), so the scan above covers cmd/ and
// internal/ regardless of the working directory `go test` was invoked from.
func repoRootForProbeScan(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("walked to the filesystem root without finding go.mod")
		}
		dir = parent
	}
}

// --- PROBE-02: the worker gate ------------------------------------------

// multiPagePDFFixture builds a structurally valid PDF with the requested page
// count, xref offsets included, so pdfcpu reads it as a real document rather
// than repairing a broken one.
//
// Built rather than committed because the property under test is the page
// COUNT, and the committed corpus (testdata/sanitize) is single-page by
// design -- those files exist to carry threat constructs, not pages. A
// 201-page fixture committed to prove an integer comparison would be a large
// binary whose only content is padding.
func multiPagePDFFixture(pages int) []byte {
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>"}

	kids := ""
	for i := 0; i < pages; i++ {
		kids += strconv.Itoa(3+i) + " 0 R "
	}
	objs = append(objs, "<< /Type /Pages /Kids ["+kids+"] /Count "+strconv.Itoa(pages)+" >>")
	for i := 0; i < pages; i++ {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> >>")
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = b.Len()
		b.WriteString(strconv.Itoa(i+1) + " 0 obj\n" + o + "\nendobj\n")
	}
	xref := b.Len()
	b.WriteString("xref\n0 " + strconv.Itoa(len(objs)+1) + "\n0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		b.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	b.WriteString("trailer\n<< /Size " + strconv.Itoa(len(objs)+1) + " /Root 1 0 R >>\nstartxref\n" +
		strconv.Itoa(xref) + "\n%%EOF\n")
	return b.Bytes()
}

// TestSanitizeConverterEnforcesPageCeilingBeforeGhostscript is PROBE-02's
// acceptance test, driven through the REGISTRY FACE (SanitizeConverter.Convert)
// rather than through sanitizePDF, because the registry face is what the worker
// actually calls and is where the configured bound lives.
func TestSanitizeConverterEnforcesPageCeilingBeforeGhostscript(t *testing.T) {
	write := func(t *testing.T, pages int) (in, out string) {
		t.Helper()
		dir := t.TempDir()
		in = filepath.Join(dir, "in.pdf")
		if err := os.WriteFile(in, multiPagePDFFixture(pages), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return in, filepath.Join(dir, "out.pdf")
	}

	t.Run("over the bound is refused and nothing is produced", func(t *testing.T) {
		in, out := write(t, 5)

		err := SanitizeConverter{MaxPages: 2}.Convert(context.Background(), in, out, nil)

		if !errors.Is(err, ErrProbeTooManyPages) {
			t.Fatalf("Convert = %v, want an error wrapping ErrProbeTooManyPages", err)
		}
		// Ghostscript's pdfwrite pass is what creates outPath. Its absence is
		// the observable form of "the refusal happened before the engine ran"
		// -- which is the whole point: at ~2.0 s/page a 3000-page document is
		// ten minutes of CPU followed by a timeout that then RETRIES.
		if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
			t.Errorf("os.Stat(%q) = %v, want IsNotExist -- ghostscript ran despite the ceiling", out, statErr)
		}
		if _, statErr := os.Stat(SanitizeReportPath(out)); !os.IsNotExist(statErr) {
			t.Error("a sanitize report was written for a refused document")
		}
	})

	t.Run("exactly the bound is not refused by the ceiling", func(t *testing.T) {
		in, out := write(t, 5)

		err := SanitizeConverter{MaxPages: 5}.Convert(context.Background(), in, out, nil)

		// Deliberately NOT asserting success: this hand-built fixture has no
		// content streams, and whether ghostscript and the verifier are happy
		// with it is a different question from the one this test asks. What
		// must hold is that the CEILING did not fire at count == max, which is
		// the boundary EnforceProbeMaxPages's own table pins from the other
		// side.
		if errors.Is(err, ErrProbeTooManyPages) {
			t.Fatalf("Convert = %v -- 5 pages against a bound of 5 must pass the ceiling; "+
				"the bound is inclusive", err)
		}
	})

	t.Run("an unconfigured bound does not parse the input at all", func(t *testing.T) {
		dir := t.TempDir()
		in := filepath.Join(dir, "in.pdf")
		// Not a PDF pdfcpu can read. With MaxPages == 0 the ceiling must not
		// invoke the parser, so this must NOT come back as the parse failure
		// probePDFPageCount would report -- it must reach ghostscript and fail
		// (or not) on its own terms.
		if err := os.WriteFile(in, []byte("%PDF-1.4\nnot really a pdf\n"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		err := SanitizeConverter{}.Convert(context.Background(), in, filepath.Join(dir, "out.pdf"), nil)

		if errors.Is(err, ErrProbeTooManyPages) {
			t.Fatalf("Convert = %v -- max == 0 means no cap configured, never a rejection", err)
		}
		if err != nil && strings.Contains(err.Error(), "pdfcpu could not count the pages") {
			t.Fatalf("Convert = %v -- an unconfigured bound must cost nothing, not a parse whose "+
				"only use would be a comparison that cannot fail", err)
		}
	})
}

// TestProbePDFPageCountReportsMalformedInput pins the third outcome.
//
// A count pdfcpu cannot produce is not "no pages" and is not a transient
// engine problem: it is a file the sanitizer's own verifier could not read
// either. Reporting it as ErrSanitizeMalformedInput makes it terminal and
// makes the message name which side failed.
func TestProbePDFPageCountReportsMalformedInput(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.pdf")
	if err := os.WriteFile(bad, []byte("not a pdf at all"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := probePDFPageCount(bad); !errors.Is(err, ErrSanitizeMalformedInput) {
		t.Fatalf("probePDFPageCount(unreadable) = %v, want an error wrapping ErrSanitizeMalformedInput", err)
	}
}
