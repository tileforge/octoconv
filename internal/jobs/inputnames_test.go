package jobs

import (
	"errors"
	"mime/multipart"
	"strings"
	"testing"
)

// A rejected name is a name the client can fix; a repaired one is a file the
// engine reads under a different name than the document asks for. So this table
// is mostly refusals, and each row says what it refuses.
func TestSafeInputFilename(t *testing.T) {
	ok := []string{
		"refs.bib", "preamble.tex", "fig.pdf", "figure-1.png",
		"Data_2026.csv", "a.tex", "имя.tex", "sec.tex",
	}
	for _, name := range ok {
		t.Run("accept "+name, func(t *testing.T) {
			got, err := SafeInputFilename(name)
			if err != nil {
				t.Fatalf("SafeInputFilename(%q) = %v; ordinary names must pass or the feature is "+
					"unusable", name, err)
			}
			if got != name {
				t.Fatalf("SafeInputFilename(%q) = %q: names are accepted verbatim or refused, never "+
					"rewritten -- a rewritten name is not the name the document \\inputs", name, got)
			}
		})
	}

	bad := []struct{ name, why string }{
		{"", "not a file"},
		{".", "not a file"},
		{"..", "the parent directory"},
		{"../refs.bib", "traversal"},
		{"../../etc/passwd", "traversal to somewhere that matters"},
		{"/etc/passwd", "absolute"},
		{"sub/refs.bib", "a directory component"},
		{`sub\refs.bib`, "a Windows separator a later join could read"},
		{"-rf", "a name a command line reads as a flag"},
		{"--output=x", "the same, with a value"},
		{"refs.bib\x00.png", "a NUL that truncates the path at the syscall"},
		{"refs\nbib", "a control character"},
		{" refs.bib", "a leading space some filesystems normalise away"},
		{"refs.bib ", "a trailing space, same reason"},
		{".hidden", "a leading dot"},
		{"refs.", "a trailing dot"},
		{strings.Repeat("a", 256) + ".tex", "longer than a filesystem component"},
	}
	for _, tc := range bad {
		t.Run("refuse "+tc.why, func(t *testing.T) {
			if _, err := SafeInputFilename(tc.name); !errors.Is(err, ErrUnsafeInputFilename) {
				t.Fatalf("SafeInputFilename(%q) was accepted; it is %s, and these bytes become a file "+
					"on disk next to the document the engine compiles", tc.name, tc.why)
			}
		})
	}
}

// TestMultipartAndOurRulesDivideTheWork pins who refuses what, because the
// division is not what a reader would guess and a future change to either side
// should have to notice.
//
// Go's mime/multipart applies filepath.Base to the parsed filename, so the
// POSIX traversal this package looks like it refuses never reaches it. What
// does reach it is everything the standard library leaves alone.
func TestMultipartAndOurRulesDivideTheWork(t *testing.T) {
	cases := []struct {
		sent       string
		stdlibGave string
		weRefuse   bool
	}{
		{"../../etc/evil.bib", "evil.bib", false},
		{"/etc/evil.bib", "evil.bib", false},
		{`sub\evil.bib`, `sub\evil.bib`, true},
		{"..", "..", true},
		{"refs.bib", "refs.bib", false},
	}
	for _, tc := range cases {
		t.Run(tc.sent, func(t *testing.T) {
			body := "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"" + tc.sent +
				"\"\r\n\r\nx\r\n--B--\r\n"
			form, err := multipart.NewReader(strings.NewReader(body), "B").ReadForm(1 << 20)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := form.File["file"][0].Filename
			if got != tc.stdlibGave {
				t.Fatalf("mime/multipart gave %q for %q, this file's comments say %q -- the division of "+
					"labour changed and the comments are now wrong", got, tc.sent, tc.stdlibGave)
			}
			_, serr := SafeInputFilename(got)
			if refused := serr != nil; refused != tc.weRefuse {
				t.Fatalf("SafeInputFilename(%q) refused = %v, want %v", got, refused, tc.weRefuse)
			}
		})
	}
}
