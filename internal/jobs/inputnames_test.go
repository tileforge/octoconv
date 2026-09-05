package jobs

import (
	"errors"
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
