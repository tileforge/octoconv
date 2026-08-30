package convert

// Phase 59, D-12. The four content matchers can now be driven two ways: over
// an io.ReaderAt (the conversion path) or over an already-read prefix (the
// inspect path, which reads ONE window and hands it to all four). These tests
// exist to make "two ways, one decision" a property of the code rather than a
// claim in a doc comment.
//
// The equivalence test is written so that RE-SPLITTING the implementation
// breaks it: it compares the two forms' verdicts on a corpus that includes the
// window boundary itself, which is where a duplicated envelope would drift
// first -- and did, historically. LooksLikeHTML once carried its own copy of
// the prologue and therefore missed the PR#2 finding-2 mid-rune fix when it
// landed for its two siblings.

import (
	"bytes"
	"strings"
	"testing"
)

// sniffPrefixCorpus is deliberately weighted toward the boundary rather than
// toward happy paths: a copy of the envelope would agree with the original on
// ordinary input and diverge exactly at the cap, on a NUL, or on a severed
// rune.
func sniffPrefixCorpus(t *testing.T) []struct {
	name string
	data []byte
} {
	t.Helper()

	// A 3-byte rune placed so the window cap lands INSIDE it.
	overCap := make([]byte, 0, SniffWindowBytes+8)
	overCap = append(overCap, []byte("<!doctype html>\n")...)
	overCap = append(overCap, bytes.Repeat([]byte("a"), SniffWindowBytes-len("<!doctype html>\n")-1)...)
	overCap = append(overCap, []byte("→")...) // 3 bytes: the cap falls after its first
	overCap = append(overCap, []byte("tail")...)

	exactlyCap := append([]byte("<!doctype html>\n"), bytes.Repeat([]byte("b"), SniffWindowBytes-len("<!doctype html>\n"))...)
	oneOverCap := append(append([]byte{}, exactlyCap...), 'c')

	return []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"html doctype", []byte("<!doctype html>\n<p>hi</p>")},
		{"html tag", []byte("<html><body></body></html>")},
		{"html tag not a real tag", []byte("<htmlfoo>")},
		{"html with BOM", append([]byte{0xEF, 0xBB, 0xBF}, []byte("  <html>")...)},
		{"latex documentclass", []byte(`\documentclass{article}`)},
		{"latex begin", []byte(`\begin{document}`)},
		{"latex bare command", []byte(`\section{Intro}`)},
		{"plain text", []byte("just some markdown\n\n# heading\n")},
		{"NUL contaminated", []byte("hello\x00world")},
		{"invalid utf8", []byte{'h', 'i', 0xC3, 0x28}},
		{"ole cfb magic", append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, bytes.Repeat([]byte{0}, 64)...)},
		{"ole cfb magic truncated", []byte{0xD0, 0xCF, 0x11}},
		{"control byte", []byte("text with a \x07 bell")},
		{"exactly SniffWindowBytes", exactlyCap},
		{"one byte over SniffWindowBytes", oneOverCap},
		{"cap lands mid-rune", overCap},
	}
}

// TestMatcherFormsAgree drives every corpus case through BOTH forms of all
// four matchers and requires identical verdicts.
//
// The prefix form is fed exactly what the inspect path feeds it: one window
// read of min(size, SniffWindowBytes) bytes, passed raw to IsOLECFBFromPrefix
// and through ValidateSniffPrefix for the three text matchers. If that
// sequence ever stops reproducing what the io.ReaderAt form does internally,
// the two endpoints of this service would start disagreeing about what a file
// IS -- which is the thing detectInspectFormat's own doc comment forbids.
func TestMatcherFormsAgree(t *testing.T) {
	for _, tc := range sniffPrefixCorpus(t) {
		t.Run(tc.name, func(t *testing.T) {
			size := int64(len(tc.data))
			r := bytes.NewReader(tc.data)

			// Exactly what internal/api's readSniffWindow does.
			n := size
			if n > SniffWindowBytes {
				n = SniffWindowBytes
			}
			var window []byte
			if n > 0 {
				window = make([]byte, n)
				read, _ := r.ReadAt(window, 0)
				window = window[:read]
			}
			prefix, textShaped := ValidateSniffPrefix(window, size)

			checks := []struct {
				name    string
				wrapper bool
				direct  bool
			}{
				{"LooksLikeHTML", LooksLikeHTML(r, size), textShaped && LooksLikeHTMLFromPrefix(prefix)},
				{"LooksLikeLaTeX", LooksLikeLaTeX(r, size), textShaped && LooksLikeLaTeXFromPrefix(prefix)},
				{"LooksLikeText", LooksLikeText(r, size), textShaped && LooksLikeTextFromPrefix(prefix)},
				{"IsOLECFB", IsOLECFB(r), IsOLECFBFromPrefix(window)},
			}
			for _, c := range checks {
				if c.wrapper != c.direct {
					t.Errorf("%s: io.ReaderAt form = %v, prefix form = %v -- the two must be one decision, not two "+
						"implementations that happen to agree", c.name, c.wrapper, c.direct)
				}
			}
		})
	}
}

// TestValidateSniffPrefixTrimsOnlyWhenTheCapSeveredTheRune pins both halves of
// the mid-rune condition, which is the one rule in this envelope that is wrong
// in BOTH directions if simplified.
//
// Drop "the file is larger than the cap" and a small file ending in a genuinely
// broken rune gets silently repaired instead of failing closed. Drop "the read
// actually reached the cap" and a client that overstates its size gets the same
// free repair. PR#2 finding-2 was the first of those two.
func TestValidateSniffPrefixTrimsOnlyWhenTheCapSeveredTheRune(t *testing.T) {
	// A lone lead byte of a 3-byte rune.
	severed := []byte("ok\xE2\x86")

	t.Run("small file with a genuinely broken rune fails closed", func(t *testing.T) {
		if _, ok := ValidateSniffPrefix(severed, int64(len(severed))); ok {
			t.Fatal("ValidateSniffPrefix accepted a small file ending in an invalid UTF-8 sequence -- " +
				"the trim must apply only when the WINDOW CAP did the severing, never to repair a genuinely broken file")
		}
	})

	t.Run("window-capped file keeps its valid prefix", func(t *testing.T) {
		full := append(bytes.Repeat([]byte("a"), SniffWindowBytes-2), []byte("\xE2\x86\x92")...)
		window := full[:SniffWindowBytes]
		got, ok := ValidateSniffPrefix(window, int64(len(full)))
		if !ok {
			t.Fatal("ValidateSniffPrefix rejected a valid file merely for being larger than the window -- this is PR#2 finding-2")
		}
		if strings.HasSuffix(string(got), "\xE2") {
			t.Fatal("the severed lead byte survived the trim")
		}
	})

	t.Run("client overstates size but read fell short", func(t *testing.T) {
		// size claims more than the cap, but the window is far shorter than
		// the cap: the cap did NOT do the severing, so no repair is owed.
		if _, ok := ValidateSniffPrefix(severed, SniffWindowBytes*4); ok {
			t.Fatal("ValidateSniffPrefix repaired a short read from a client that overstated its size")
		}
	})
}

// TestIsOLECFBFromPrefixShortInputIsFalseNotPanic pins the one boundary the
// prefix form introduces that the io.ReaderAt form never had: a caller can now
// hand it fewer bytes than the signature.
func TestIsOLECFBFromPrefixShortInputIsFalseNotPanic(t *testing.T) {
	for _, n := range []int{0, 1, 7} {
		if IsOLECFBFromPrefix(bytes.Repeat([]byte{0xD0}, n)) {
			t.Fatalf("IsOLECFBFromPrefix(%d bytes) = true, want false", n)
		}
	}
}
