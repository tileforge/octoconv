package convert_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// TestScanTeXPreamble is a table over shapes that occur in real preambles,
// not over shapes that are convenient to parse. Every fixture below is either
// taken from a document this repository already compiles (64-04's end-to-end
// paper, pandoc's default template) or is a form LaTeX documents them as legal.
func TestScanTeXPreamble(t *testing.T) {
	cases := []struct {
		name         string
		src          string
		wantPackages []string
		wantComplete bool
	}{
		{
			name:         "plain",
			src:          "\\documentclass{article}\n\\usepackage{amsmath}\n\\begin{document}\nx\n\\end{document}\n",
			wantPackages: []string{"amsmath"},
			wantComplete: true,
		},
		{
			name:         "options",
			src:          "\\documentclass{article}\n\\usepackage[T1]{fontenc}\n\\begin{document}\n",
			wantPackages: []string{"fontenc"},
			wantComplete: true,
		},
		{
			name:         "several packages in one call",
			src:          "\\usepackage{amsmath,amssymb, booktabs}\n\\begin{document}\n",
			wantPackages: []string{"amsmath", "amssymb", "booktabs"},
			wantComplete: true,
		},
		{
			name:         "options with a comma inside",
			src:          "\\usepackage[backend=bibtex,style=numeric]{biblatex}\n\\begin{document}\n",
			wantPackages: []string{"biblatex"},
			wantComplete: true,
		},
		{
			name:         "RequirePackage",
			src:          "\\RequirePackage{fix-cm}\n\\documentclass{article}\n\\begin{document}\n",
			wantPackages: []string{"fix-cm"},
			wantComplete: true,
		},
		{
			name:         "commented out",
			src:          "% \\usepackage{pgfplots}\n\\usepackage{amsmath}\n\\begin{document}\n",
			wantPackages: []string{"amsmath"},
			wantComplete: true,
		},
		{
			name:         "trailing comment on a real line",
			src:          "\\usepackage{amsmath} % why: display equations\n\\begin{document}\n",
			wantPackages: []string{"amsmath"},
			wantComplete: true,
		},
		{
			name:         "escaped percent is not a comment",
			src:          "\\usepackage{siunitx} \\newcommand{\\pct}{100\\%} \\usepackage{url}\n\\begin{document}\n",
			wantPackages: []string{"siunitx", "url"},
			wantComplete: true,
		},
		{
			// The preamble ENDS at \begin{document}. A \usepackage after it is
			// not a preamble entry, and treating it as one would let a
			// document extend its own preamble past the window.
			name:         "usepackage after begin document is not the preamble",
			src:          "\\usepackage{amsmath}\n\\begin{document}\n\\usepackage{pgfplots}\n",
			wantPackages: []string{"amsmath"},
			wantComplete: true,
		},
		{
			// The case the whole `complete` return value exists for.
			name:         "truncated prefix",
			src:          "\\documentclass{article}\n\\usepackage{amsmath}\n\\usepackage{grap",
			wantPackages: []string{"amsmath"},
			wantComplete: false,
		},
		{
			name:         "two on one line",
			src:          "\\usepackage{amsmath}\\usepackage{amssymb}\n\\begin{document}\n",
			wantPackages: []string{"amsmath", "amssymb"},
			wantComplete: true,
		},
		{
			name:         "no packages at all",
			src:          "\\documentclass{article}\n\\begin{document}\nhello\n",
			wantPackages: nil,
			wantComplete: true,
		},
		{
			// Pandoc's own standalone output opens with a comment block and
			// loads five packages the declared set does not name -- the exact
			// document 64-03's measurement was about.
			name: "pandoc default template head",
			src: "% Options for packages loaded elsewhere\n" +
				"\\PassOptionsToPackage{unicode}{hyperref}\n" +
				"\\documentclass[]{article}\n" +
				"\\usepackage{lmodern}\n" +
				"\\usepackage[T1]{fontenc}\n" +
				"\\usepackage[utf8]{inputenc}\n" +
				"\\usepackage{textcomp}\n" +
				"\\usepackage{microtype}\n" +
				"\\begin{document}\n",
			wantPackages: []string{"lmodern", "fontenc", "inputenc", "textcomp", "microtype"},
			wantComplete: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, complete := convert.ScanTeXPreamble([]byte(tc.src))
			if complete != tc.wantComplete {
				t.Errorf("complete = %v, want %v", complete, tc.wantComplete)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantPackages, ",") {
				t.Errorf("packages = %v, want %v", got, tc.wantPackages)
			}
		})
	}
}

// TestScanTeXPreambleNeverInventsAPackage states the asymmetry the scanner is
// built around, as executable text rather than as a comment.
//
// Missing a package is TOLERATED -- the worker gate is the backstop, and
// `complete` reports when the window ran out. Inventing one is not: it is a
// valid document refused at admission, which is the failure 63-02 removed a
// whole gate for.
//
// The three inputs below are the macro/conditional/input forms the doc comment
// names. Each hides a package from the scanner, and each must produce NOTHING
// rather than a guess.
func TestScanTeXPreambleNeverInventsAPackage(t *testing.T) {
	hidden := []struct {
		name string
		src  string
	}{
		{"macro definition", "\\newcommand{\\loadit}[1]{\\usepackage{#1}}\n\\loadit{pgfplots}\n\\begin{document}\n"},
		{"input", "\\input{preamble}\n\\begin{document}\n"},
		{"conditional", "\\ifpdf\\else\\fi\n\\begin{document}\n"},
	}
	for _, tc := range hidden {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := convert.ScanTeXPreamble([]byte(tc.src))
			for _, pkg := range got {
				// The macro case DOES contain the literal \usepackage{#1}; the
				// scanner reads "#1" from it, which is a name no bundle has and
				// would be a false refusal.
				if pkg == "#1" || pkg == "pgfplots" || pkg == "preamble" {
					t.Errorf("scanner reported %q from a construct it does not evaluate: an admission "+
						"gate would refuse this valid document", pkg)
				}
			}
		})
	}
}

// TestTeXUnsupportedPackageErrorRoundTrips holds the sentinel and its name
// extraction together. Both gates format their client-facing message from
// TeXUnsupportedPackageName, so a change that broke extraction would produce
// two refusals that name nothing, in two places, with every other test green.
func TestTeXUnsupportedPackageErrorRoundTrips(t *testing.T) {
	err := convert.TeXUnsupportedPackageError("pgfplots")
	if !errors.Is(err, convert.ErrTeXUnsupportedPackage) {
		t.Fatalf("errors.Is(err, ErrTeXUnsupportedPackage) = false for %v", err)
	}
	if got := convert.TeXUnsupportedPackageName(err); got != "pgfplots" {
		t.Fatalf("TeXUnsupportedPackageName = %q, want %q", got, "pgfplots")
	}
	if got := convert.TeXUnsupportedPackageName(errors.New("something else")); got != "" {
		t.Fatalf("TeXUnsupportedPackageName of an unrelated error = %q, want \"\"", got)
	}
	// Wrapped one layer deeper, which is how the worker will carry it.
	wrapped := errors.Join(errors.New("convert"), err)
	if got := convert.TeXUnsupportedPackageName(wrapped); got != "pgfplots" {
		t.Fatalf("TeXUnsupportedPackageName of a wrapped error = %q, want %q", got, "pgfplots")
	}
}
