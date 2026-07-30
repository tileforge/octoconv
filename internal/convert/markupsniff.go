// Package convert (this file): markupsniff.go adds the fail-closed
// content-validation probes for the pandoc markup family (PDCE-01) plus the
// input-size DoS guard pandoc's Convert calls before shelling out
// (PDCE-03/T-47-04). It extends the existing LooksLikeHTML (htmlsniff.go)
// and SniffContainer (docsniff.go) precedents rather than reinventing them.
package convert

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

// latexDocumentClassMarker and latexBeginMarker are the two strong LaTeX
// structural markers checked (after stripping a leading UTF-8 BOM and
// whitespace) by LooksLikeLaTeX -- mirrors htmlsniff.go's marker-table
// discipline for a format that, unlike md/rst/org, has distinctive syntax.
var (
	latexDocumentClassMarker = []byte(`\documentclass`)
	latexBeginMarker         = []byte(`\begin{`)
)

// readSniffPrefix is the shared fail-closed envelope LooksLikeLaTeX and
// LooksLikeText both build on: it bounds how much of r is read into memory
// via ReadAt (never a full buffer for an oversized claimed size, mirroring
// LooksLikeHTML/SniffContainer's "never buffer the whole upload" discipline),
// then rejects (ok==false) on empty input, a NUL byte, or invalid UTF-8. r's
// io.ReaderAt short-read contract (a partial read alongside a non-EOF error)
// is honored the same way LooksLikeHTML honors it -- callers must never
// panic on a short or oversized-claimed input.
func readSniffPrefix(r io.ReaderAt, size int64, cap int64) ([]byte, bool) {
	if size <= 0 {
		return nil, false
	}
	n := size
	if n > cap {
		n = cap
	}
	buf := make([]byte, n)
	read, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return nil, false
	}
	buf = buf[:read]
	if len(buf) == 0 {
		return nil, false
	}
	if bytes.IndexByte(buf, 0) != -1 {
		return nil, false
	}
	if !utf8.Valid(buf) {
		return nil, false
	}
	return buf, true
}

// isASCIILetter reports whether b is an ASCII letter -- used by
// LooksLikeLaTeX to recognize a leading TeX control-word (a backslash
// immediately followed by a letter, e.g. "\section{...}") when neither of
// the two named markers matches.
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// LooksLikeLaTeX reports whether r's content is valid UTF-8, contains no NUL
// bytes, and begins (after leading whitespace/BOM) with a LaTeX marker --
// D-01's fail-closed content check for the latex source format, run by the
// caller AFTER the client's declared source already claims latex. The
// recognized markers are, in order: `\documentclass`, `\begin{`, or a bare
// leading backslash-command (`\` immediately followed by an ASCII letter,
// e.g. `\section{Intro}`) -- the last case covers a fragment document that
// starts mid-body without a preamble. It never panics on empty, short, or
// oversized-claimed input (bounded ReadAt, mirrors LooksLikeHTML).
func LooksLikeLaTeX(r io.ReaderAt, size int64) bool {
	buf, ok := readSniffPrefix(r, size, htmlSniffCap)
	if !ok {
		return false
	}

	buf = bytes.TrimPrefix(buf, []byte{0xEF, 0xBB, 0xBF}) // leading UTF-8 BOM
	buf = bytes.TrimLeft(buf, " \t\r\n\f\v")
	if len(buf) == 0 {
		return false
	}

	if bytes.HasPrefix(buf, latexDocumentClassMarker) {
		return true
	}
	if bytes.HasPrefix(buf, latexBeginMarker) {
		return true
	}
	if buf[0] == '\\' && len(buf) > 1 && isASCIILetter(buf[1]) {
		return true
	}
	return false
}

// hasDisallowedControlByte reports whether buf contains a C0 control byte
// other than tab/LF/CR/form-feed -- the discriminator LooksLikeText applies
// in place of LooksLikeHTML's structural marker, since plaintext carries no
// distinctive magic prefix to check against.
func hasDisallowedControlByte(buf []byte) bool {
	for _, b := range buf {
		if b >= 0x20 {
			continue
		}
		switch b {
		case '\t', '\n', '\r', '\f':
			continue
		default:
			return true
		}
	}
	return false
}

// LooksLikeText reports whether r's content is valid UTF-8, contains no NUL
// bytes, and contains no other disallowed binary-control bytes -- the shared
// fail-closed content check for the md, rst, and org source formats (D-01).
// It never panics on empty, short, or oversized-claimed input (bounded
// ReadAt, mirrors LooksLikeHTML).
//
// ACCEPTED LIMITATION (documented, never silent — D-01/T-47-05b, mirrors the
// discipline of OCR's "empty-but-valid output is done" and audio's
// "hallucination-on-silence" documented gaps): md, rst, and org are
// mutually-interchangeable plaintext — a plain sentence with no
// format-specific syntax is simultaneously valid md AND rst AND org. Content
// alone cannot reliably distinguish which ONE of the three a given upload
// actually is (unlike latex's `\documentclass` marker or html's `<!doctype`
// marker). LooksLikeText therefore intentionally degrades the fail-closed
// question from "is this specifically rst-vs-md-vs-org?" down to the
// honest, weaker "is this plausibly a text document at all?" — valid UTF-8
// text with no NUL/binary-control contamination. Once that plaintext gate
// passes, the caller trusts the client's DECLARED source format among the
// text-family (the same declared-format trust every other engine extends
// once its own content gate has passed). This is a recorded, accepted gap,
// not an oversight: a genuinely binary or NUL-injected upload still fails
// closed regardless of its declared source.
func LooksLikeText(r io.ReaderAt, size int64) bool {
	buf, ok := readSniffPrefix(r, size, htmlSniffCap)
	if !ok {
		return false
	}
	return !hasDisallowedControlByte(buf)
}

// ErrPandocInputTooLarge is returned when an input file's size exceeds the
// configured PANDOC_MAX_INPUT_BYTES cap -- fail-closed BEFORE any pandoc
// subprocess runs (T-47-04/PDCE-03). Pandoc reads its input linearly, so
// there is no exotic pre-parse "bomb shape" to walk the way there is for
// zip/pdf (D-04, RESEARCH.md) -- this size cap, plus the engine timeout
// (Phase 49) bounding output-expansion runaways, are the full defense here.
var ErrPandocInputTooLarge = errors.New("pandoc: input size exceeds configured maximum")

// enforcePandocInputSize stats inPath and rejects fail-closed when its size
// exceeds maxBytes. maxBytes == 0 means "no cap configured this phase" --
// mirrors enforceImagePixelBound/enforceMaxPages's zero-value convention
// (ocrguards.go). A missing/unstattable file is itself a fail-closed error,
// never a silent pass -- the same discipline enforceImagePixelBound applies
// to its own os.Open failure.
func enforcePandocInputSize(inPath string, maxBytes int64) error {
	info, err := os.Stat(inPath)
	if err != nil {
		return fmt.Errorf("pandoc: stat %q: %w", inPath, err)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d byte cap", ErrPandocInputTooLarge, info.Size(), maxBytes)
	}
	return nil
}
