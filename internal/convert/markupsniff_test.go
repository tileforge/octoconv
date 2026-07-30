package convert

import (
	"bytes"
	"strings"
	"testing"
)

func TestLooksLikeLaTeX(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{
			name: "documentclass preamble",
			in:   []byte("\\documentclass{article}\n\\begin{document}\nHello\n\\end{document}"),
			want: true,
		},
		{
			name: "begin document fragment, no preamble",
			in:   []byte("\\begin{document}\nHello\n\\end{document}"),
			want: true,
		},
		{
			name: "bare leading control word fragment",
			in:   []byte("\\section{Intro}\nSome body text.\n"),
			want: true,
		},
		{
			name: "leading whitespace before marker",
			in:   []byte("  \n\t\\documentclass{article}\n"),
			want: true,
		},
		{
			name: "leading BOM before marker",
			in:   append([]byte{0xEF, 0xBB, 0xBF}, []byte("\\documentclass{article}\n")...),
			want: true,
		},
		{
			name: "plain prose, no backslash command",
			in:   []byte("This is just a plain sentence with no markup at all.\n"),
			want: false,
		},
		{
			name: "bare backslash with nothing after it",
			in:   []byte("\\"),
			want: false,
		},
		{
			name: "backslash followed by non-letter",
			in:   []byte("\\1 is not a control word"),
			want: false,
		},
		{
			name: "contains NUL byte",
			in:   append([]byte("\\documentclass{article}\x00"), []byte("\\begin{document}\\end{document}")...),
			want: false,
		},
		{
			name: "invalid utf-8 byte sequence",
			in:   []byte{0x5C, 0x64, 0xFF, 0xFE, 0x6F, 0x63},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.in)
			got := LooksLikeLaTeX(r, int64(len(tc.in)))
			if got != tc.want {
				t.Errorf("LooksLikeLaTeX(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLooksLikeLaTeXEmptyInput(t *testing.T) {
	r := bytes.NewReader(nil)
	if LooksLikeLaTeX(r, 0) {
		t.Error("LooksLikeLaTeX on empty input = true, want false")
	}
}

func TestLooksLikeLaTeXNeverPanicsShortReaderAt(t *testing.T) {
	sr := strings.NewReader("\\documentclass{article}\\begin{document}\\end{document}")
	// Claim a size far larger than the actual content -- LooksLikeLaTeX must
	// not panic when ReadAt returns fewer bytes than requested.
	if LooksLikeLaTeX(sr, 10_000_000) {
		t.Log("short-read case returned true; no panic occurred (expected)")
	}
}

func TestLooksLikeLaTeXOversizedInputBoundedRead(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("\\documentclass{article}\n\\begin{document}\n")
	buf.Write(bytes.Repeat([]byte("a"), htmlSniffCap*2))
	buf.WriteString("\\end{document}")
	data := buf.Bytes()
	r := bytes.NewReader(data)
	if !LooksLikeLaTeX(r, int64(len(data))) {
		t.Error("LooksLikeLaTeX on oversized-but-valid latex = false, want true")
	}
}

func TestLooksLikeText(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{
			name: "plain sentence, valid for md/rst/org alike",
			in:   []byte("This is a plain sentence. It could be md, rst, or org.\n"),
			want: true,
		},
		{
			name: "markdown-flavored text",
			in:   []byte("# Heading\n\nSome *emphasis* and a [link](http://example.com).\n"),
			want: true,
		},
		{
			name: "text with tab/CR/LF/form-feed, all allowed",
			in:   []byte("line one\tindented\r\nline two\x0cpage break\n"),
			want: true,
		},
		{
			name: "contains NUL byte",
			in:   []byte("plain text\x00with a NUL"),
			want: false,
		},
		{
			name: "invalid utf-8 byte sequence",
			in:   []byte{0x68, 0x69, 0xFF, 0xFE},
			want: false,
		},
		{
			name: "disallowed control byte (0x01)",
			in:   []byte("plain text\x01with a control byte"),
			want: false,
		},
		{
			name: "disallowed control byte (0x1b escape)",
			in:   []byte("plain text\x1b[31mred\x1b[0m"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.in)
			got := LooksLikeText(r, int64(len(tc.in)))
			if got != tc.want {
				t.Errorf("LooksLikeText(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLooksLikeTextEmptyInput(t *testing.T) {
	r := bytes.NewReader(nil)
	if LooksLikeText(r, 0) {
		t.Error("LooksLikeText on empty input = true, want false")
	}
}

func TestLooksLikeTextNeverPanicsShortReaderAt(t *testing.T) {
	sr := strings.NewReader("a plain sentence with no marker at all")
	// Claim a size far larger than the actual content -- LooksLikeText must
	// not panic when ReadAt returns fewer bytes than requested. The result
	// value itself is not asserted here; reaching this line without a
	// runtime panic is the pass condition (mirrors
	// TestLooksLikeHTMLNeverPanicsShortReaderAt).
	got := LooksLikeText(sr, 10_000_000)
	t.Logf("short-read case returned %v; no panic occurred (expected)", got)
}

func TestLooksLikeTextOversizedInputBoundedRead(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("a plain sentence, ")
	buf.Write(bytes.Repeat([]byte("a"), htmlSniffCap*2))
	data := buf.Bytes()
	r := bytes.NewReader(data)
	if !LooksLikeText(r, int64(len(data))) {
		t.Error("LooksLikeText on oversized-but-valid text = false, want true")
	}
}

