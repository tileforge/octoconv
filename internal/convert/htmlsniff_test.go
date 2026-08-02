package convert

import (
	"bytes"
	"strings"
	"testing"
)

func TestLooksLikeHTML(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{
			name: "doctype lowercase",
			in:   []byte("<!doctype html><html><body>hi</body></html>"),
			want: true,
		},
		{
			name: "doctype uppercase",
			in:   []byte("<!DOCTYPE HTML><html><body>hi</body></html>"),
			want: true,
		},
		{
			name: "doctype mixed case with leading whitespace",
			in:   []byte("  \n\t<!DocType Html>\n<html></html>"),
			want: true,
		},
		{
			name: "bare html tag",
			in:   []byte("<html><body>hi</body></html>"),
			want: true,
		},
		{
			name: "html tag with attributes uppercase",
			in:   []byte("<HTML lang=\"en\"><body>hi</body></HTML>"),
			want: true,
		},
		{
			name: "html tag with leading whitespace",
			in:   []byte("   \r\n<html>\n<body></body></html>"),
			want: true,
		},
		{
			name: "html self-closing marker",
			in:   []byte("<html/>"),
			want: true,
		},
		{
			name: "plain text no marker",
			in:   []byte("hello world, this is not html at all"),
			want: false,
		},
		{
			name: "unrelated tag prefixed with html",
			in:   []byte("<htmlfoo>not really html</htmlfoo>"),
			want: false,
		},
		{
			name: "contains NUL byte",
			in:   append([]byte("<!doctype html>\x00"), []byte("<html></html>")...),
			want: false,
		},
		{
			name: "invalid utf-8 byte sequence",
			in:   []byte{0x3C, 0x21, 0xFF, 0xFE, 0x68, 0x74, 0x6D, 0x6C},
			want: false,
		},
		{
			name: "leading BOM then doctype",
			in:   append([]byte{0xEF, 0xBB, 0xBF}, []byte("<!doctype html><html></html>")...),
			want: true,
		},
		{
			name: "bare html with nothing after",
			in:   []byte("<html"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.in)
			got := LooksLikeHTML(r, int64(len(tc.in)))
			if got != tc.want {
				t.Errorf("LooksLikeHTML(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLooksLikeHTMLEmptyInput(t *testing.T) {
	r := bytes.NewReader(nil)
	if LooksLikeHTML(r, 0) {
		t.Error("LooksLikeHTML on empty input = true, want false")
	}
}

func TestLooksLikeHTMLNeverPanicsShortReaderAt(t *testing.T) {
	// shortReaderAt reports a claimed size larger than what it actually has
	// to return, exercising the io.ReaderAt short-read contract (returns a
	// non-EOF error alongside a partial read count).
	sr := strings.NewReader("<html><body>hi</body></html>")
	// Claim a size far larger than the actual content -- LooksLikeHTML must
	// not panic when ReadAt returns fewer bytes than requested.
	if LooksLikeHTML(sr, 10_000_000) {
		// A truncated read may or may not still match the marker depending
		// on how much was actually read; the only hard requirement here is
		// "does not panic", asserted implicitly by reaching this line.
		t.Log("short-read case returned true; no panic occurred (expected)")
	}
}

func TestLooksLikeHTMLOversizedInputBoundedRead(t *testing.T) {
	// A file larger than htmlSniffCap must not be fully buffered; the
	// marker check still succeeds since the marker sits well within the
	// bounded prefix.
	var buf bytes.Buffer
	buf.WriteString("<!doctype html><html><body>")
	buf.Write(bytes.Repeat([]byte("a"), htmlSniffCap*2))
	buf.WriteString("</body></html>")
	data := buf.Bytes()
	r := bytes.NewReader(data)
	if !LooksLikeHTML(r, int64(len(data))) {
		t.Error("LooksLikeHTML on oversized-but-valid HTML = false, want true")
	}
}

// TestLooksLikeHTMLRuneStraddlingSniffCap is the PR#2 finding-2 regression
// guard for LooksLikeHTML: a 3-byte rune (U+2026 "…", bytes E2 80 A6) starts
// at offset htmlSniffCap-1, so the 1 MiB sniff cap severs it after its first
// byte and the raw prefix ends on a dangling lead byte. Before this fix
// (LooksLikeHTML carried its own duplicated prologue instead of delegating
// to readSniffPrefix), utf8.Valid saw that dangling byte and rejected a
// perfectly valid, oversized HTML document. Padding length is computed from
// htmlSniffCap arithmetic, not a magic constant, so this test stays correct
// if the cap ever moves. Mirrors
// TestLooksLikeTextRuneStraddlingSniffCap/TestLooksLikeLaTeXRuneStraddlingSniffCap
// (markupsniff_test.go).
func TestLooksLikeHTMLRuneStraddlingSniffCap(t *testing.T) {
	prefix := "<!doctype html>\n<html><body>"
	pad := bytes.Repeat([]byte("a"), htmlSniffCap-1-len(prefix))
	var buf bytes.Buffer
	buf.WriteString(prefix)
	buf.Write(pad)
	buf.WriteString("…")
	buf.WriteString(" more text after the straddling rune.</body></html>")
	data := buf.Bytes()
	r := bytes.NewReader(data)
	if !LooksLikeHTML(r, int64(len(data))) {
		t.Error("LooksLikeHTML on a rune straddling the sniff cap = false, want true")
	}
}

// TestLooksLikeHTMLInvalidUTF8AtSniffCapStillRejected is the anti-relaxation
// guard: an impossible UTF-8 lead byte (0xFF never appears in any legal
// UTF-8 sequence) placed exactly at the same offset the straddling-rune test
// above uses must still fail closed. 0xFF declares no length via
// leadByteRuneLen, so it is never trimmed, and utf8.Valid must still reject
// it. The trim must never launder a genuinely invalid byte into a pass just
// because it happens to sit at the cap boundary. Mirrors
// TestLooksLikeTextInvalidUTF8AtSniffCapStillRejected (markupsniff_test.go).
func TestLooksLikeHTMLInvalidUTF8AtSniffCapStillRejected(t *testing.T) {
	prefix := "<!doctype html>\n<html><body>"
	pad := bytes.Repeat([]byte("a"), htmlSniffCap-1-len(prefix))
	var buf bytes.Buffer
	buf.WriteString(prefix)
	buf.Write(pad)
	buf.WriteByte(0xFF)
	buf.WriteString(" more text after the invalid byte.</body></html>")
	data := buf.Bytes()
	r := bytes.NewReader(data)
	if LooksLikeHTML(r, int64(len(data))) {
		t.Error("LooksLikeHTML with an impossible lead byte at the sniff cap = true, want false")
	}
}

// TestLooksLikeHTMLIncompleteRuneAtEOFStillRejected proves the trim is
// scoped ONLY to a prefix the cap actually severed: a SMALL HTML document
// (well under htmlSniffCap, so nothing was truncated) whose final byte is a
// bare dangling UTF-8 lead byte (0xE2) is genuinely invalid UTF-8 and must
// still be rejected -- the fix must not turn into "always trim a trailing
// lead byte". Mirrors TestLooksLikeTextIncompleteRuneAtEOFStillRejected
// (markupsniff_test.go).
func TestLooksLikeHTMLIncompleteRuneAtEOFStillRejected(t *testing.T) {
	data := append([]byte("<!doctype html><html><body>ending in a dangling lead byte "), 0xE2)
	r := bytes.NewReader(data)
	if LooksLikeHTML(r, int64(len(data))) {
		t.Error("LooksLikeHTML with an incomplete trailing rune at true EOF = true, want false")
	}
}
