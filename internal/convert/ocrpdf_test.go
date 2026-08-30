package convert

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireOCRPDFBinaries skips the calling test unless pdfinfo, pdftoppm,
// pdftotext, AND tesseract are all on PATH -- keeps `go test -short` (and
// any CI without poppler/tesseract installed) hermetic, mirroring
// ocr_test.go's requireTesseract / ocrguards_test.go's own real-binary
// skip-if-absent gates.
func requireOCRPDFBinaries(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"pdfinfo", "pdftoppm", "pdftotext", "tesseract"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found on PATH, skipping real-binary test", bin)
		}
	}
}

// restrictedPATH builds a temp dir containing ONLY symlinks to the named
// binaries (resolved via exec.LookPath) and returns its path, so a test can
// t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext")) to PROVE a
// later stage (e.g. pdftoppm/tesseract) was never invoked: if the code
// under test tried to exec a binary NOT in this restricted set, exec would
// fail with a distinct "executable file not found in $PATH" error rather
// than the guard's own expected sentinel -- so asserting the EXPECTED
// sentinel (not that unrelated exec error) is real proof of invocation
// order, not just a code-review claim about which line runs first.
func restrictedPATH(t *testing.T, bins ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, bin := range bins {
		real, err := exec.LookPath(bin)
		if err != nil {
			t.Skipf("%s not found on PATH, skipping real-binary test", bin)
		}
		if err := os.Symlink(real, filepath.Join(dir, bin)); err != nil {
			t.Fatalf("symlink %s: %v", bin, err)
		}
	}
	return dir
}

func TestOCRRasterizePageArgs(t *testing.T) {
	args := ocrRasterizePageArgs("/work/in.pdf", "/work/out/page", 3, 300)

	fIdx := indexOfArg(args, "-f")
	if fIdx < 0 || args[fIdx+1] != "3" {
		t.Fatalf("expected -f 3 as discrete argv elements (single-page lower bound), got %v", args)
	}
	lIdx := indexOfArg(args, "-l")
	if lIdx < 0 || args[lIdx+1] != "3" {
		t.Fatalf("expected -l 3 as discrete argv elements (single-page upper bound -- proves per-page streaming, never a whole-PDF render), got %v", args)
	}
	rIdx := indexOfArg(args, "-r")
	if rIdx < 0 || args[rIdx+1] != "300" {
		t.Fatalf("expected -r 300 (the fixed rasterize DPI), got %v", args)
	}
	if fIdx == lIdx {
		t.Fatalf("-f and -l must be distinct argv positions, got %v", args)
	}
}

// TestOCRMergePagesRoundTrip is HERMETIC (pure encoding/xml, no
// tesseract/poppler binary needed): it constructs >=2 synthetic per-page
// hOCR fragments AND >=2 synthetic ALTO fragments whose OCR'd text contains
// literal &/</>, feeds them through newOCRPageMerger/AddPage/Finalize, and
// asserts the MERGED document parses via a full xml.Decoder token walk as
// valid XML with the adversarial content preserved -- proving the
// MULTI-PAGE merged document (not just a single tesseract-written page) is
// valid XML for the pdf->hocr / pdf->alto pairs (PITFALLS.md Pitfall 5).
func TestOCRMergePagesRoundTrip(t *testing.T) {
	assertMergedValidXML := func(t *testing.T, out []byte) {
		t.Helper()
		dec := xml.NewDecoder(bytes.NewReader(out))
		for {
			_, err := dec.Token()
			if err != nil {
				if err.Error() == "EOF" {
					return
				}
				t.Fatalf("merged document is not valid XML: %v\n---\n%s", err, out)
			}
		}
	}

	t.Run("hocr", func(t *testing.T) {
		frag1 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<body>
<div class='ocr_page' id='page_1' title='bbox 0 0 400 120'>
<span class='ocrx_word'>Fig &amp; 1 &lt; 2 &gt; report</span>
</div>
</body>
</html>`)
		frag2 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<body>
<div class='ocr_page' id='page_1' title='bbox 0 0 400 120'>
<span class='ocrx_word'>Section &quot;B&quot; &amp; appendix</span>
</div>
</body>
</html>`)

		m := newOCRPageMerger("hocr")
		if err := m.AddPage(1, frag1); err != nil {
			t.Fatalf("AddPage(1): %v", err)
		}
		if err := m.AddPage(2, frag2); err != nil {
			t.Fatalf("AddPage(2): %v", err)
		}
		out, err := m.Finalize()
		if err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		assertMergedValidXML(t, out)
		if !bytes.Contains(out, []byte("&amp;")) || !bytes.Contains(out, []byte("&lt;")) || !bytes.Contains(out, []byte("&gt;")) {
			t.Errorf("expected escaped adversarial content preserved in merged hOCR, got:\n%s", out)
		}
		if bytes.Count(out, []byte("ocr_page")) != 2 {
			t.Errorf("expected 2 merged hOCR pages, got:\n%s", out)
		}
	})

	t.Run("alto", func(t *testing.T) {
		frag1 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<alto xmlns="http://www.loc.gov/standards/alto/ns-v3#">
<Layout>
<Page WIDTH="400" HEIGHT="120" ID="page_0">
<PrintSpace><String CONTENT="Fig &amp; 1 &lt; 2 &gt; report"/></PrintSpace>
</Page>
</Layout>
</alto>`)
		frag2 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<alto xmlns="http://www.loc.gov/standards/alto/ns-v3#">
<Layout>
<Page WIDTH="400" HEIGHT="120" ID="page_0">
<PrintSpace><String CONTENT="Section &quot;B&quot; &amp; appendix"/></PrintSpace>
</Page>
</Layout>
</alto>`)

		m := newOCRPageMerger("alto")
		if err := m.AddPage(1, frag1); err != nil {
			t.Fatalf("AddPage(1): %v", err)
		}
		if err := m.AddPage(2, frag2); err != nil {
			t.Fatalf("AddPage(2): %v", err)
		}
		out, err := m.Finalize()
		if err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		assertMergedValidXML(t, out)
		if !bytes.Contains(out, []byte("&amp;")) || !bytes.Contains(out, []byte("&lt;")) || !bytes.Contains(out, []byte("&gt;")) {
			t.Errorf("expected escaped adversarial content preserved in merged ALTO, got:\n%s", out)
		}
		if bytes.Count(out, []byte("<Page")) != 2 {
			t.Errorf("expected 2 merged ALTO pages, got:\n%s", out)
		}
	})
}

// TestOCRPDFPageCap proves the pdfinfo page-count cap fires BEFORE any
// pdftoppm render: PATH is restricted to pdfinfo+pdftotext ONLY (no
// pdftoppm/tesseract), so if the guard stage did not run first, the
// subsequent pdftoppm invocation would fail with an unrelated
// "executable file not found" error instead of the expected sentinel.
func TestOCRPDFPageCap(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := OCRConverter{MaxPages: 2} // fixture has 3 declared pages
	err := c.Convert(ctx, filepath.Join("testdata", "ocr", "multipage_image_only.pdf"), outPath, nil)
	if !errors.Is(err, ErrOCRTooManyPages) {
		t.Fatalf("Convert on an over-cap PDF (pdftoppm/tesseract unavailable to prove pre-render ordering) = %v, want ErrOCRTooManyPages", err)
	}
}

// TestOCRPDFPageBox proves the per-page page-box bound fires BEFORE that
// page's pdftoppm render -- a defense-in-depth axis the page-count cap
// alone does not cover (T-43-01b). Same PATH-restriction technique as
// TestOCRPDFPageCap proves pdftoppm was never invoked.
func TestOCRPDFPageBox(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The fixture's declared 300x200pt page rasterizes to ~1.04M pixels at
	// 300 DPI -- MaxImagePixels here is far below that.
	c := OCRConverter{MaxImagePixels: 1000}
	err := c.Convert(ctx, filepath.Join("testdata", "ocr", "multipage_image_only.pdf"), outPath, nil)
	if !errors.Is(err, ErrOCRPageBoxTooLarge) {
		t.Fatalf("Convert with a tiny MaxImagePixels bound (pdftoppm/tesseract unavailable to prove pre-render ordering) = %v, want ErrOCRPageBoxTooLarge", err)
	}
}

// TestBornDigitalSentinelIsUnreachable is the PROOF Plan 59-06 will cite when
// it removes born_digital from the active failure-code vocabulary and deletes
// a row from internal/worker's pinning suite.
//
// That suite's own header declares any change to an assertion a protocol
// violation requiring explicit justification. "The code is unreachable" is a
// claim; this test is what turns it into a fact. It runs a born-digital PDF
// through ALL THREE targets and requires that none of them produces
// ErrOCRBornDigital -- not the two that changed behavior, all of them, because
// a proof of unreachability that skips a route proves nothing about that route.
//
// PATH is restricted to pdfinfo+pdftotext, so the box targets fail at the
// renderer. That failure is expected and is not what is being asserted: the
// assertion is on the sentinel's absence, which holds regardless of how the
// render itself ends.
func TestBornDigitalSentinelIsUnreachable(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, target := range []string{"txt", "hocr", "alto"} {
		t.Run(target, func(t *testing.T) {
			outPath := filepath.Join(t.TempDir(), "out."+target)
			err := OCRConverter{}.Convert(ctx, filepath.Join("testdata", "ocr", "born_digital.pdf"), outPath, nil)
			if errors.Is(err, ErrOCRBornDigital) {
				t.Fatalf("Convert(pdf -> %s) produced ErrOCRBornDigital -- the sentinel must be unreachable on every "+
					"route before Plan 59-06 may retire the born_digital error code", target)
			}
		})
	}
}

// TestPDFToTxtRouteExists is the test named BY NAME in ocrguards.go's own doc
// comment, so that the comment's claim -- that a born-digital PDF has a route
// -- cannot silently become false again.
//
// The comment it replaces said the client was being "steered to the document
// engine instead", and that engine did not exist: pdf is a SOURCE format in
// exactly one place in the repository (ocr.go), and every other engine only
// produces PDFs. The claim had been wrong since it was written. Tying the
// replacement to a test is the only thing that stops the same drift recurring.
//
// Two halves, and both are needed: the pair must be advertised, AND converting
// over it must actually produce something. Either alone is the state Phase 59
// found the code in.
func TestPDFToTxtRouteExists(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	advertised := false
	for _, p := range (OCRConverter{}).Pairs() {
		if p == (Pair{From: "pdf", To: "txt"}) {
			advertised = true
		}
	}
	if !advertised {
		t.Fatal("OCRConverter.Pairs() no longer advertises pdf -> txt")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	outPath := filepath.Join(t.TempDir(), "out.txt")
	if err := (OCRConverter{}).Convert(ctx, filepath.Join("testdata", "ocr", "born_digital.pdf"), outPath, nil); err != nil {
		t.Fatalf("the advertised pdf -> txt pair failed to convert: %v -- an advertised route that refuses every "+
			"request is the exact contradiction BDPD-02 closed", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read %s: %v", outPath, err)
	}
	if countNonWhitespace(got) == 0 {
		t.Fatal("the advertised pdf -> txt pair produced empty output for a PDF with a real text layer")
	}
}

// TestOCRPDFBoxTargetsSkipTextExtraction is the test named in
// ocrPDFGuardStage's own doc comment: for hocr/alto, pdftotext is not invoked
// at all.
//
// Proven by ABSENCE rather than by inspection: PATH carries pdfinfo and
// pdftoppm but NOT pdftotext, so an implementation that still probed the text
// layer would fail with a missing-pdftotext exec error. Reaching pdftoppm is
// therefore positive evidence that the probe was skipped -- a claim a code
// review could only have asserted.
func TestOCRPDFBoxTargetsSkipTextExtraction(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftoppm"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, target := range []string{"hocr", "alto"} {
		t.Run(target, func(t *testing.T) {
			outPath := filepath.Join(t.TempDir(), "out."+target)
			err := OCRConverter{}.Convert(ctx, filepath.Join("testdata", "ocr", "born_digital.pdf"), outPath, nil)
			if err == nil {
				return // tesseract happened to be reachable; nothing to prove here
			}
			if strings.Contains(err.Error(), "pdftotext") {
				t.Fatalf("Convert(pdf -> %s) tried to run pdftotext: %v -- a box target's outcome cannot depend on "+
					"the text layer, so probing for it spends a subprocess for nothing", target, err)
			}
		})
	}
}

// TestOCRConvertPDF is real-binary (skip-if-absent for
// pdfinfo/pdftoppm/pdftotext/tesseract): OCRs the committed 2-page
// text-bearing image-only PDF fixture (page 1 = "PAGE ONE", page 2 = "PAGE
// TWO", 0 pdftotext-extractable characters -- built via ImageMagick +
// verified image-only) and asserts the concatenated txt output spans BOTH
// pages, proving the streaming per-page loop actually processed every
// page, not just the first.
func TestOCRConvertPDF(t *testing.T) {
	requireOCRPDFBinaries(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := OCRConverter{}
	if err := c.Convert(ctx, filepath.Join("testdata", "ocr", "multipage_text.pdf"), outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Contains(b, []byte("PAGE ONE")) {
		t.Errorf("concatenated txt output missing page 1's text, got:\n%s", b)
	}
	if !bytes.Contains(b, []byte("PAGE TWO")) {
		t.Errorf("concatenated txt output missing page 2's text, got:\n%s", b)
	}
}

// TestOCRConvertPDFHOCRMerged is real-binary (skip-if-absent): OCRs the
// same 2-page fixture to hocr and asserts the merged multi-page document
// parses as valid XML AND contains 2 distinct ocr_page elements (proving
// Finalize's merge ran across ALL pages, not just the last one rendered).
func TestOCRConvertPDFHOCRMerged(t *testing.T) {
	requireOCRPDFBinaries(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.hocr")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := OCRConverter{}
	if err := c.Convert(ctx, filepath.Join("testdata", "ocr", "multipage_text.pdf"), outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("merged hOCR output is not valid XML: %v\n---\n%s", err, b)
		}
	}
	if n := bytes.Count(b, []byte("ocr_page")); n != 2 {
		t.Fatalf("expected 2 merged hOCR pages, got %d in:\n%s", n, b)
	}
}

// fakeOCRProgressSink is a recording ProgressSink used to assert
// OCRConverter.ConvertWithProgress's page-granular-ramp-vs-null split
// without a real worker/DB in the loop -- mirrors fakeAVProgressSink's
// shape (av_progress_test.go), kept as its own OCR-scoped type so this
// plan's tests carry zero coupling to av.go's progress test helpers.
type fakeOCRProgressSink struct {
	durationCalls int
	updates       []ProgressUpdate
}

func (f *fakeOCRProgressSink) SourceDuration(seconds float64) { f.durationCalls++ }
func (f *fakeOCRProgressSink) Update(u ProgressUpdate)        { f.updates = append(f.updates, u) }

var _ ProgressSink = (*fakeOCRProgressSink)(nil)

// TestOCRConvertWithProgress is real-binary (skip-if-absent): OCRing the
// committed 2-page fixture via ConvertWithProgress must drive a
// monotonically non-decreasing Pct sequence ending at exactly 100 on the
// final page -- driven purely by the page loop's onPage callback
// (ocrpdf.go), never by progress.go's ffmpeg-specific tick parser.
func TestOCRConvertWithProgress(t *testing.T) {
	requireOCRPDFBinaries(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sink := &fakeOCRProgressSink{}
	c := OCRConverter{}
	if err := c.ConvertWithProgress(ctx, filepath.Join("testdata", "ocr", "multipage_text.pdf"), outPath, nil, sink); err != nil {
		t.Fatalf("ConvertWithProgress: %v", err)
	}

	if len(sink.updates) != 2 {
		t.Fatalf("expected exactly 2 Update calls (one per fixture page), got %d: %+v", len(sink.updates), sink.updates)
	}
	last := -1
	for i, u := range sink.updates {
		if u.Pct < last {
			t.Fatalf("update %d: Pct regressed from %d to %d", i, last, u.Pct)
		}
		last = u.Pct
	}
	if last != 100 {
		t.Fatalf("final Update Pct = %d, want 100 (the last page)", last)
	}
	if sink.updates[0].ETA == nil {
		t.Errorf("expected a derived ETA after the first completed page, got nil")
	}
}

// TestOCRPDFPageCapWithProgress proves ConvertWithProgress rejects an
// over-cap PDF via the SAME guard stage Convert uses -- zero sink.Update
// calls, ErrOCRTooManyPages, identically to TestOCRPDFPageCap's assertion
// against plain Convert (the "reuses the same guard preamble" acceptance
// criterion, proven by identical-outcome assertions on both entry points).
func TestOCRPDFPageCapWithProgress(t *testing.T) {
	t.Setenv("PATH", restrictedPATH(t, "pdfinfo", "pdftotext"))

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sink := &fakeOCRProgressSink{}
	c := OCRConverter{MaxPages: 2}
	err := c.ConvertWithProgress(ctx, filepath.Join("testdata", "ocr", "multipage_image_only.pdf"), outPath, nil, sink)
	if !errors.Is(err, ErrOCRTooManyPages) {
		t.Fatalf("ConvertWithProgress on an over-cap PDF = %v, want ErrOCRTooManyPages", err)
	}
	if len(sink.updates) != 0 {
		t.Fatalf("expected zero Update calls on a rejected over-cap PDF, got %d", len(sink.updates))
	}
}

// TestOCRSingleImageNullProgress asserts a single-image job produces ZERO
// sink.Update calls (OCR-04: null->100 is the worker's job, not the
// converter's) -- the image path never enters the PDF page loop at all.
func TestOCRSingleImageNullProgress(t *testing.T) {
	requireTesseract(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sink := &fakeOCRProgressSink{}
	c := OCRConverter{}
	if err := c.ConvertWithProgress(ctx, filepath.Join("testdata", "ocr", "text_sample.png"), outPath, nil, sink); err != nil {
		t.Fatalf("ConvertWithProgress: %v", err)
	}
	if len(sink.updates) != 0 {
		t.Fatalf("expected zero Update calls for a single-image job, got %d: %+v", len(sink.updates), sink.updates)
	}
	if sink.durationCalls != 0 {
		t.Fatalf("expected zero SourceDuration calls for a single-image job, got %d", sink.durationCalls)
	}
}
