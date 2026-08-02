package convert

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
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
			// Verbatim head of pandoc's own `--standalone -t latex` output.
			// This is what every `-> latex` conversion emits, so rejecting it
			// broke five advertised pairs at output validation.
			name: "leading % comment block, as pandoc emits",
			in:   []byte("% Options for packages loaded elsewhere\n\\PassOptionsToPackage{unicode}{hyperref}\n\\documentclass[]{article}\n"),
			want: true,
		},
		{
			name: "comment block with nothing after it",
			in:   []byte("% only a comment\n%% and another\n\n"),
			want: false,
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

func TestEnforcePandocInputSize(t *testing.T) {
	dir := t.TempDir()

	small := filepath.Join(dir, "small.md")
	if err := os.WriteFile(small, []byte("# small file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(small): %v", err)
	}

	large := filepath.Join(dir, "large.md")
	if err := os.WriteFile(large, bytes.Repeat([]byte("a"), 1024), 0o600); err != nil {
		t.Fatalf("WriteFile(large): %v", err)
	}

	t.Run("under cap passes", func(t *testing.T) {
		if err := enforcePandocInputSize(small, 4096); err != nil {
			t.Errorf("enforcePandocInputSize(small, 4096) = %v, want nil", err)
		}
	})

	t.Run("over cap rejected fail-closed", func(t *testing.T) {
		err := enforcePandocInputSize(large, 512)
		if err == nil {
			t.Fatal("enforcePandocInputSize(large, 512) = nil, want ErrPandocInputTooLarge")
		}
		if !bytes.Contains([]byte(err.Error()), []byte("exceeds")) {
			t.Errorf("enforcePandocInputSize error = %v, want an exceeds-cap message", err)
		}
	})

	t.Run("zero cap means no cap configured", func(t *testing.T) {
		if err := enforcePandocInputSize(large, 0); err != nil {
			t.Errorf("enforcePandocInputSize(large, 0) = %v, want nil (no cap)", err)
		}
	})

	t.Run("missing file rejected fail-closed", func(t *testing.T) {
		if err := enforcePandocInputSize(filepath.Join(dir, "missing.md"), 4096); err == nil {
			t.Error("enforcePandocInputSize(missing file) = nil, want a stat error")
		}
	})
}

func TestMIMETypeMarkupFormats(t *testing.T) {
	cases := map[string]string{
		"md":    "text/markdown",
		"rst":   "text/x-rst",
		"org":   "text/plain",
		"latex": "application/x-latex",
		"epub":  "application/epub+zip",
	}
	for format, want := range cases {
		if got := MIMEType(format); got != want {
			t.Errorf("MIMEType(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestSniffContainerEpub(t *testing.T) {
	data := odfZipFixture(t, "application/epub+zip")
	r := bytes.NewReader(data)
	res, err := SniffContainer(r, int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer(epub fixture) error = %v", err)
	}
	if res.Format != "epub" {
		t.Errorf("SniffContainer(epub fixture).Format = %q, want %q", res.Format, "epub")
	}
}

// TestEnforcePandocInputSizeArchiveBomb proves the guard bounds the DECLARED
// UNCOMPRESSED size of a container, not just its on-disk size. Without this,
// a docx/epub that compresses far below PANDOC_MAX_INPUT_BYTES can still hand
// pandoc hundreds of megabytes to parse and OOM-kill the worker (the failure
// mode measured live in Phase 50).
func TestEnforcePandocInputSizeArchiveBomb(t *testing.T) {
	dir := t.TempDir()

	// A docx-shaped zip whose entry is highly compressible: tiny on disk,
	// enormous once expanded.
	bomb := filepath.Join(dir, "bomb.docx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("zip Create: %v", err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("A"), 8<<20)); err != nil { // 8 MiB of one byte
		t.Fatalf("zip Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	if err := os.WriteFile(bomb, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile(bomb): %v", err)
	}

	// Precondition: the archive really is small enough to slip past a
	// compressed-size-only check. If this ever stops holding, the test is no
	// longer exercising the bomb path.
	const cap1MiB = 1 << 20
	info, err := os.Stat(bomb)
	if err != nil {
		t.Fatalf("Stat(bomb): %v", err)
	}
	if info.Size() > cap1MiB {
		t.Fatalf("fixture not compressible enough: %d bytes on disk, want <= %d", info.Size(), cap1MiB)
	}

	err = enforcePandocInputSize(bomb, cap1MiB)
	if !errors.Is(err, ErrPandocInputTooLarge) {
		t.Errorf("enforcePandocInputSize(archive bomb) = %v, want ErrPandocInputTooLarge", err)
	}

	// A non-container input of the same size must still pass: the container
	// check must not leak into the plain-text path.
	plain := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(plain, bytes.Repeat([]byte("a"), 4096), 0o600); err != nil {
		t.Fatalf("WriteFile(plain): %v", err)
	}
	if err := enforcePandocInputSize(plain, cap1MiB); err != nil {
		t.Errorf("enforcePandocInputSize(plain markdown) = %v, want nil", err)
	}
}

// TestEnforcePandocInputSizeOverflow covers the two size-accounting overflow
// paths: a central directory declaring entries that would wrap a plain uint64
// sum back to a small total, and an absurd configured cap whose *4 limit would
// overflow int64 into a negative (and thus, once widened, an unreachable)
// bound. Both must still reject.
func TestEnforcePandocInputSizeOverflow(t *testing.T) {
	dir := t.TempDir()
	bomb := filepath.Join(dir, "wrap.docx")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"word/document.xml", "word/second.xml"} {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		w, err := zw.CreateRaw(h)
		if err != nil {
			t.Fatalf("CreateRaw(%s): %v", name, err)
		}
		// Declare a colossal uncompressed size while storing nothing: two of
		// these sum past 2^64 and would wrap a naive accumulator.
		h.UncompressedSize64 = 1 << 63
		h.CompressedSize64 = 0
		if _, err := w.Write(nil); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	if err := os.WriteFile(bomb, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := enforcePandocInputSize(bomb, 1<<20); !errors.Is(err, ErrPandocInputTooLarge) {
		t.Errorf("enforcePandocInputSize(wrapping archive) = %v, want ErrPandocInputTooLarge", err)
	}

	// An absurd cap must not silently disable the container bound: the limit
	// saturates instead of overflowing, and a normal file still passes.
	plain := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(plain, []byte("# ok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(plain): %v", err)
	}
	if err := enforcePandocInputSize(plain, 1<<62); err != nil {
		t.Errorf("enforcePandocInputSize(plain, huge cap) = %v, want nil", err)
	}
}
