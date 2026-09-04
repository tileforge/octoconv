package convert

// This file (texpreamble.go): a bounded scan of a LaTeX preamble for the
// packages it loads, used by BOTH gates in TEX-03 -- cmd/api's admission
// refusal and cmd/tectonic-worker's terminal classification.
//
// IT IS A SCANNER, NOT A PARSER, AND THAT IS NOT A CAVEAT -- IT IS THE DESIGN
//
// TeX is a macro language whose expansion is Turing-complete. This scanner
// does NOT follow \input or \include, does NOT expand macros, and does NOT
// understand \if.../\else/\fi. Every one of those is a way for a \usepackage
// to be invisible to it, and none of them is a bug here:
//
//	\newcommand{\loadit}[1]{\usepackage{#1}}   % invisible: not expanded
//	\input{preamble}                            % invisible: not followed
//	\ifpdf\usepackage{pgfplots}\fi              % visible, but may not execute
//
// The scanner is allowed to MISS packages, and the worker gate exists exactly
// for what it misses. What it must never do is INVENT one: a false positive is
// a valid document refused at admission, which is the failure 63-02 removed a
// whole gate for.
//
// This project has a recorded history of a bounded scanner being quietly
// treated as authoritative -- 63-02's page-count gate abstained on precisely
// the long documents it existed to refuse, because a PDF page tree is a tree.
// Hence `complete`, returned rather than kept internal: a caller that ignores
// it has made a decision, not an oversight.

import (
	"errors"
	"fmt"
	"strings"
)

// ErrTeXUnsupportedPackage is the sentinel both gates carry. Wrap it with
// TeXUnsupportedPackageError so the message names the package -- a refusal
// that does not say WHICH package leaves the client removing \usepackage lines
// one at a time, one upload each.
var ErrTeXUnsupportedPackage = errors.New("tex: package is not in the supported set")

// TeXUnsupportedPackageError wraps ErrTeXUnsupportedPackage with the offending
// name.
func TeXUnsupportedPackageError(pkg string) error {
	return fmt.Errorf("%w: %s", ErrTeXUnsupportedPackage, pkg)
}

// TeXUnsupportedPackageName extracts the package name from an error produced
// by TeXUnsupportedPackageError, or "" if err is not one.
//
// Exists so the worker's failure message and the API's can be built from the
// same value rather than each formatting its own string around the same
// sentinel -- two messages for one condition drift, and this project has a
// name for that shape.
func TeXUnsupportedPackageName(err error) string {
	if !errors.Is(err, ErrTeXUnsupportedPackage) {
		return ""
	}
	msg := err.Error()
	i := strings.LastIndex(msg, ErrTeXUnsupportedPackage.Error()+": ")
	if i < 0 {
		return ""
	}
	name := msg[i+len(ErrTeXUnsupportedPackage.Error())+2:]
	if j := strings.IndexAny(name, " :\n"); j >= 0 {
		name = name[:j]
	}
	return name
}

// beginDocument ends the preamble. A \usepackage after it is not a preamble
// entry (LaTeX will error on it anyway), and treating it as one would let a
// document past the window extend its own preamble arbitrarily.
const beginDocument = `\begin{document}`

// ScanTeXPreamble collects the package names a LaTeX preamble loads, from an
// already-read prefix of the file.
//
// It returns the names in source order (duplicates preserved -- a document
// that loads a package twice says so twice, and a caller counting occurrences
// is not this function's business) and `complete`, which is true only when
// \begin{document} was seen inside the prefix.
//
// complete == false means the preamble ran past the window: some of it was not
// read at all, so the absence of a violation says nothing. That is the whole
// reason the worker gate exists, which is why it is a return value.
//
// THE PREFIX FORM, NOT AN io.ReaderAt FORM. Phase 59 D-12 split every markup
// matcher in two precisely so a caller needing several answers about one
// upload reads ONE window instead of one per matcher, and handleCreateJob is
// already holding that window for LooksLikeLaTeXFromPrefix. A second read here
// would be a second answer to one question about one file.
func ScanTeXPreamble(prefix []byte) (packages []string, complete bool) {
	src := string(prefix)

	// Cut at \begin{document} first, so everything below scans the preamble
	// only. Done on the raw text rather than after comment-stripping because
	// the marker itself cannot be inside a comment and still open a document.
	if i := strings.Index(src, beginDocument); i >= 0 {
		src = src[:i]
		complete = true
	}

	for _, line := range strings.Split(src, "\n") {
		line = stripTeXComment(line)
		for _, cmd := range []string{`\usepackage`, `\RequirePackage`} {
			rest := line
			for {
				i := strings.Index(rest, cmd)
				if i < 0 {
					break
				}
				rest = rest[i+len(cmd):]
				args, ok := takeTeXBraceGroup(skipTeXOptionGroup(rest))
				if !ok {
					// A \usepackage whose brace group is split across the line
					// break, or truncated by the window. Not an error and not
					// a guess: skipped, and `complete` is what tells the caller
					// the scan may be partial.
					break
				}
				for _, name := range strings.Split(args, ",") {
					if name = strings.TrimSpace(name); looksLikeTeXPackageName(name) {
						packages = append(packages, name)
					}
				}
			}
		}
	}
	return packages, complete
}

// looksLikeTeXPackageName reports whether name could be a package at all.
//
// A package name is a FILENAME: \usepackage{x} resolves x.sty, so anything
// that cannot be part of a filename cannot be a package. Anything else is
// dropped, silently and deliberately -- the alternative is refusing a valid
// document over a string the engine would never have looked up.
//
// The case that produced this function is not hypothetical; it was found by
// the test that asserts the scanner never invents a name:
//
//	\newcommand{\loadit}[1]{\usepackage{#1}}
//
// The scanner does not expand macros, so it read the literal "#1" out of the
// definition body -- and an admission gate would then have refused the document
// for loading a package called "#1". Missing the real package here is correct
// (the worker gate is the backstop); naming a fictional one is not.
func looksLikeTeXPackageName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.':
		default:
			return false
		}
	}
	return true
}

// stripTeXComment removes a `%` comment from a line, honouring `\%`.
//
// Without the escape handling, a line containing a literal percent sign would
// have everything after it discarded -- which only ever LOSES packages, never
// invents them, but a scanner that silently truncates on a common character
// would make `complete` mean less than it says.
func stripTeXComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != '%' {
			continue
		}
		if i > 0 && line[i-1] == '\\' {
			continue
		}
		return line[:i]
	}
	return line
}

// skipTeXOptionGroup skips a leading [...] option group, if present, so
// \usepackage[T1]{fontenc} reaches {fontenc}.
//
// Only a leading one, and only if it opens before any brace: options come
// before the argument in every form LaTeX accepts here.
func skipTeXOptionGroup(s string) string {
	t := strings.TrimLeft(s, " \t")
	if !strings.HasPrefix(t, "[") {
		return s
	}
	depth := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return t[i+1:]
			}
		case '{':
			// An unterminated option group; do not swallow the argument.
			return s
		}
	}
	return s
}

// takeTeXBraceGroup returns the contents of a leading {...} group.
func takeTeXBraceGroup(s string) (string, bool) {
	t := strings.TrimLeft(s, " \t")
	if !strings.HasPrefix(t, "{") {
		return "", false
	}
	if end := strings.Index(t, "}"); end > 0 {
		return t[1:end], true
	}
	return "", false
}
