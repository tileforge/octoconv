package convert

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requirePandoc skips the calling test when the pandoc binary is not on
// PATH -- keeps `go test -short` (and any CI without pandoc installed)
// hermetic, mirroring requireTesseract's (ocr_test.go) real-binary
// skip-if-absent gate.
func requirePandoc(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not found on PATH, skipping real-binary test")
	}
}

// TestPandocConverterEngine proves Engine() reports EnginePandoc.
func TestPandocConverterEngine(t *testing.T) {
	if got := (PandocConverter{}).Engine(); got != EnginePandoc {
		t.Fatalf("Engine() = %q, want %q", got, EnginePandoc)
	}
}

// TestPandocPairsClosedSet proves Pairs() is a curated CLOSED allowlist
// (D-02): a sample of real, meaningful pairs are present, and identity
// pairs plus nonsensical directions (epub->latex, epub->org) are absent --
// Pairs() is the hardcoded pandocPairTable, never a generated cartesian.
func TestPandocPairsClosedSet(t *testing.T) {
	pairs := (PandocConverter{}).Pairs()

	mustHave := []Pair{
		{From: "md", To: "html"},
		{From: "md", To: "docx"},
		{From: "md", To: "epub"},
		{From: "rst", To: "html"},
		{From: "latex", To: "md"},
		{From: "docx", To: "md"},
		{From: "html", To: "md"},
	}
	for _, want := range mustHave {
		found := false
		for _, p := range pairs {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Pairs() missing %+v, want a curated closed set including it", want)
		}
	}

	mustNotHave := []Pair{
		{From: "epub", To: "latex"}, // nonsensical/low-value direction, excluded
		{From: "epub", To: "org"},   // nonsensical/low-value direction, excluded
		{From: "md", To: "md"},      // identity pair, excluded
		{From: "html", To: "html"},  // identity pair, excluded
	}
	for _, bad := range mustNotHave {
		for _, p := range pairs {
			if p == bad {
				t.Errorf("Pairs() contains excluded pair %+v, want it absent (closed curated set)", bad)
			}
		}
	}
}

// TestPandocPairSupportedMatchesPairs proves pandocPairSupported (Convert's
// fast-reject helper) and Pairs() (the advertised allowlist) never drift
// apart -- both walk the SAME pandocPairTable.
func TestPandocPairSupportedMatchesPairs(t *testing.T) {
	for _, p := range (PandocConverter{}).Pairs() {
		if !pandocPairSupported(p.From, p.To) {
			t.Errorf("pandocPairSupported(%q, %q) = false, want true (advertised by Pairs())", p.From, p.To)
		}
	}
	if pandocPairSupported("epub", "latex") {
		t.Error("pandocPairSupported(epub, latex) = true, want false (excluded nonsensical direction)")
	}
	if pandocPairSupported("txt", "html") {
		t.Error("pandocPairSupported(txt, html) = true, want false (txt is not a curated source)")
	}
}

// TestPandocConvertUnsupportedPairRejectedBeforeSubprocess proves Convert
// fails fast on an unlisted pair before any subprocess runs -- no pandoc
// binary is required for this test to pass, since the rejection must
// happen before pandoc is ever invoked (PDOC-01).
func TestPandocConvertUnsupportedPairRejectedBeforeSubprocess(t *testing.T) {
	c := PandocConverter{}
	err := c.Convert(context.Background(), "/nonexistent/in.epub", "/nonexistent/out.latex", nil)
	if err == nil {
		t.Fatal("Convert(epub->latex, unlisted pair) = nil, want a fail-fast unsupported-pair error")
	}
}

func indexOfPandocArg(args []string, v string) int {
	for i, a := range args {
		if a == v {
			return i
		}
	}
	return -1
}

// TestPandocConvertArgs pins the hardened argv shape (Task 1's core):
// the per-source reader-extension table, --sandbox, --standalone selection,
// and the absolute absence of any raw-passthrough-ENABLING or
// network-enabling token.
func TestPandocConvertArgs(t *testing.T) {
	t.Run("md source: raw_html-raw_tex suffix (pandoc's own markdown format name), sandbox, standalone for structural target", func(t *testing.T) {
		args := pandocConvertArgs("md", "html", "/work/in.md", "/work/out.html", PandocOpts{})
		idx := indexOfPandocArg(args, "-f")
		if idx == -1 || args[idx+1] != "markdown-raw_html-raw_tex" {
			t.Fatalf("args = %v, want -f markdown-raw_html-raw_tex (pandoc has no format literally named md)", args)
		}
		if indexOfPandocArg(args, "--sandbox") == -1 {
			t.Errorf("args missing --sandbox: %v", args)
		}
		if indexOfPandocArg(args, "--standalone") == -1 {
			t.Errorf("args missing --standalone for structural target html: %v", args)
		}
	})

	t.Run("html source: raw_html-raw_tex suffix", func(t *testing.T) {
		args := pandocConvertArgs("html", "md", "/work/in.html", "/work/out.md", PandocOpts{})
		idx := indexOfPandocArg(args, "-f")
		if idx == -1 || args[idx+1] != "html-raw_html-raw_tex" {
			t.Fatalf("args = %v, want -f html-raw_html-raw_tex", args)
		}
	})

	t.Run("latex source: raw_tex ONLY, never raw_html", func(t *testing.T) {
		args := pandocConvertArgs("latex", "md", "/work/in.latex", "/work/out.md", PandocOpts{})
		idx := indexOfPandocArg(args, "-f")
		if idx == -1 || args[idx+1] != "latex-raw_tex" {
			t.Fatalf("args = %v, want -f latex-raw_tex", args)
		}
		for _, a := range args {
			if strings.Contains(a, "raw_html") {
				t.Errorf("args for latex source must never carry raw_html: %v", args)
			}
		}
	})

	t.Run("rst source: bare -f, no -raw suffix at all", func(t *testing.T) {
		args := pandocConvertArgs("rst", "html", "/work/in.rst", "/work/out.html", PandocOpts{})
		idx := indexOfPandocArg(args, "-f")
		if idx == -1 || args[idx+1] != "rst" {
			t.Fatalf("args = %v, want bare -f rst", args)
		}
	})

	t.Run("org source: bare -f, no -raw suffix at all", func(t *testing.T) {
		args := pandocConvertArgs("org", "md", "/work/in.org", "/work/out.md", PandocOpts{})
		idx := indexOfPandocArg(args, "-f")
		if idx == -1 || args[idx+1] != "org" {
			t.Fatalf("args = %v, want bare -f org", args)
		}
	})

	t.Run("docx source: bare -f, no -raw suffix at all", func(t *testing.T) {
		args := pandocConvertArgs("docx", "md", "/work/in.docx", "/work/out.md", PandocOpts{})
		idx := indexOfPandocArg(args, "-f")
		if idx == -1 || args[idx+1] != "docx" {
			t.Fatalf("args = %v, want bare -f docx", args)
		}
	})

	t.Run("no +raw enabling token ever, and no network flag", func(t *testing.T) {
		for _, tc := range []struct{ src, tgt string }{
			{"md", "docx"}, {"latex", "html"}, {"rst", "html"}, {"docx", "epub"},
		} {
			args := pandocConvertArgs(tc.src, tc.tgt, "/work/in", "/work/out", PandocOpts{})
			for _, a := range args {
				if strings.Contains(a, "+raw") {
					t.Errorf("pandocConvertArgs(%s, %s) = %v, must never enable +raw", tc.src, tc.tgt, args)
				}
				if strings.Contains(strings.ToLower(a), "network") {
					t.Errorf("pandocConvertArgs(%s, %s) = %v, must never carry a network-enabling flag", tc.src, tc.tgt, args)
				}
			}
		}
	})

	t.Run("non-structural target: no forced --standalone", func(t *testing.T) {
		args := pandocConvertArgs("html", "md", "/work/in.html", "/work/out.md", PandocOpts{})
		if indexOfPandocArg(args, "--standalone") != -1 {
			t.Errorf("args for non-structural target md must not force --standalone: %v", args)
		}
	})

	t.Run("opts-requested standalone on a structural target is never duplicated", func(t *testing.T) {
		args := pandocConvertArgs("md", "html", "/work/in.md", "/work/out.html", PandocOpts{Standalone: true})
		count := 0
		for _, a := range args {
			if a == "--standalone" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("--standalone appears %d times in %v, want exactly 1", count, args)
		}
	})

	t.Run("opts-requested standalone on a non-structural target is honored via pandocArgs", func(t *testing.T) {
		args := pandocConvertArgs("html", "md", "/work/in.html", "/work/out.md", PandocOpts{Standalone: true})
		if indexOfPandocArg(args, "--standalone") == -1 {
			t.Errorf("args = %v, want --standalone present (opts requested it explicitly)", args)
		}
	})

	t.Run("input path and -o output path present", func(t *testing.T) {
		args := pandocConvertArgs("md", "html", "/work/in.md", "/work/out.html", PandocOpts{})
		if indexOfPandocArg(args, "/work/in.md") == -1 {
			t.Errorf("args = %v, missing input path", args)
		}
		oIdx := indexOfPandocArg(args, "-o")
		if oIdx == -1 || args[oIdx+1] != "/work/out.html" {
			t.Errorf("args = %v, want -o /work/out.html", args)
		}
	})
}

// TestPandocFormatNameTranslatesMdToMarkdown proves the project's own "md"
// alias is translated to pandoc's real format identifier "markdown" for
// BOTH the -f reader spec and the -t writer spec -- live-verified against
// pandoc 3.10.1 (`--list-input-formats`/`--list-output-formats`): pandoc has
// no format literally named "md", so passing "md" straight through to -f/-t
// unconditionally fails every md-source and md-target conversion with
// "Unknown input/output format 'md'" (Rule 1 bug caught by the real-pandoc
// matrix test, fixed here).
func TestPandocFormatNameTranslatesMdToMarkdown(t *testing.T) {
	if got := pandocFormatName("md"); got != "markdown" {
		t.Errorf("pandocFormatName(md) = %q, want markdown", got)
	}
	for _, f := range []string{"rst", "org", "latex", "html", "docx", "epub"} {
		if got := pandocFormatName(f); got != f {
			t.Errorf("pandocFormatName(%q) = %q, want unchanged %q", f, got, f)
		}
	}

	args := pandocConvertArgs("html", "md", "/work/in.html", "/work/out.md", PandocOpts{})
	tIdx := indexOfPandocArg(args, "-t")
	if tIdx == -1 || args[tIdx+1] != "markdown" {
		t.Fatalf("args = %v, want -t markdown (md target translated)", args)
	}
}

// TestPandocConvertArgsIncludesPandocOptsFragment proves the already-
// validated PandocOpts fragment (pandocArgs, pandocopts.go) is threaded
// through into the final argv unchanged.
func TestPandocConvertArgsIncludesPandocOptsFragment(t *testing.T) {
	o := PandocOpts{Toc: true, NumberSections: true, Wrap: "none"}
	args := pandocConvertArgs("md", "html", "/work/in.md", "/work/out.html", o)
	for _, want := range []string{"--toc", "--number-sections", "--wrap=none"} {
		if indexOfPandocArg(args, want) == -1 {
			t.Errorf("args = %v, missing expected opts fragment element %q", args, want)
		}
	}
}

// TestPandocConvertOversizeInputRejectedFailClosed proves
// enforcePandocInputSize (markupsniff.go, Plan 02) rejects an oversize
// input BEFORE any subprocess -- no pandoc binary required for this test to
// pass or fail meaningfully, since the guard must fire before pandoc is
// ever invoked (PDCE-03).
func TestPandocConvertOversizeInputRejectedFailClosed(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "large.md")
	if err := os.WriteFile(in, bytes.Repeat([]byte("a"), 4096), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out := filepath.Join(dir, "large.html")

	c := PandocConverter{MaxInputBytes: 100}
	err := c.Convert(context.Background(), in, out, nil)
	if err == nil {
		t.Fatal("Convert with oversize input = nil, want a fail-closed size-cap error")
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("Convert with oversize input produced an output file -- want none, rejected before the subprocess ever ran")
	}
}

// TestPandocConvertRealMatrix proves the curated Pairs() allowlist actually
// works against the real pandoc 3.10.1 binary for the exact pairs the old
// (rejected) blanket raw-passthrough suffix broke: rst->html, docx->md,
// latex->md, plus md->html, md->docx, html->md, and a ->epub target
// (PDOC-01/PDOC-02). Skips hermetically when pandoc is not on PATH.
func TestPandocConvertRealMatrix(t *testing.T) {
	requirePandoc(t)

	dir := t.TempDir()
	c := PandocConverter{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mdFixture := filepath.Join(dir, "doc.md")
	writeFixture(t, mdFixture, "# Title\n\nSome *emphasis* and a [link](https://example.com).\n")

	rstFixture := filepath.Join(dir, "doc.rst")
	writeFixture(t, rstFixture, "Title\n=====\n\nSome body text.\n")

	latexFixture := filepath.Join(dir, "doc.latex")
	writeFixture(t, latexFixture, "\\documentclass{article}\n\\begin{document}\nHello world.\n\\end{document}\n")

	htmlFixture := filepath.Join(dir, "doc.html")
	writeFixture(t, htmlFixture, "<!doctype html><html><body><p>Hello world.</p></body></html>")

	var docxOut string

	cases := []struct {
		name   string
		in     string
		outExt string
	}{
		{"md->html", mdFixture, "html"},
		{"md->docx", mdFixture, "docx"},
		{"rst->html", rstFixture, "html"},
		{"latex->md", latexFixture, "md"},
		{"html->md", htmlFixture, "md"},
		{"md->epub", mdFixture, "epub"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(dir, "out-"+strings.ReplaceAll(tc.name, ">", "")+"."+tc.outExt)
			if err := c.Convert(ctx, tc.in, out, nil); err != nil {
				t.Fatalf("Convert(%s) error = %v", tc.name, err)
			}
			info, err := os.Stat(out)
			if err != nil {
				t.Fatalf("stat output: %v", err)
			}
			if info.Size() == 0 {
				t.Fatalf("Convert(%s) produced empty output, want non-empty for a non-empty source", tc.name)
			}
			if tc.outExt == "docx" {
				docxOut = out
			}
		})
	}

	t.Run("docx->md", func(t *testing.T) {
		if docxOut == "" {
			t.Skip("md->docx sub-test did not produce an output to round-trip")
		}
		out := filepath.Join(dir, "out-docx-md.md")
		if err := c.Convert(ctx, docxOut, out, nil); err != nil {
			t.Fatalf("Convert(docx->md) error = %v", err)
		}
		info, err := os.Stat(out)
		if err != nil {
			t.Fatalf("stat output: %v", err)
		}
		if info.Size() == 0 {
			t.Fatalf("Convert(docx->md) produced empty output, want non-empty for a non-empty source")
		}
	})
}

// TestPandocConvertEmptySourceIsDone proves an empty-but-valid source
// completes Convert as `done` (nil error), not `failed` -- D-04's
// documented "no empty-output error branch" contract, exercised against
// the real pandoc binary end-to-end (pandocValidateOutput's own unit-level
// rationale is exercised here at the Convert level).
func TestPandocConvertEmptySourceIsDone(t *testing.T) {
	requirePandoc(t)

	dir := t.TempDir()
	in := filepath.Join(dir, "empty.html")
	writeFixture(t, in, "")
	out := filepath.Join(dir, "empty.md")

	c := PandocConverter{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Convert(ctx, in, out, nil); err != nil {
		t.Fatalf("Convert(empty html->md) = %v, want nil (empty-but-valid output is done)", err)
	}
}

// writeFixture writes content to path, failing the test on any write error.
func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
