package convert

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireTesseract skips the calling test when the tesseract binary is not
// on PATH -- keeps `go test -short` (and any CI without the binary
// installed) hermetic, mirroring the av/ocrguards real-binary skip-if-
// absent gates.
func requireTesseract(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract not found on PATH, skipping real-binary test")
	}
}

func TestOCRConverterPairs(t *testing.T) {
	pairs := OCRConverter{}.Pairs()
	want := len(ocrSourceFormats) * len(ocrTargetFormats)
	if len(pairs) != want {
		t.Fatalf("Pairs() returned %d pairs, want %d", len(pairs), want)
	}
	mustHave := []Pair{
		{From: "pdf", To: "txt"},
		{From: "png", To: "hocr"},
		{From: "jpg", To: "alto"},
	}
	for _, wp := range mustHave {
		found := false
		for _, p := range pairs {
			if p == wp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Pairs() missing %+v", wp)
		}
	}
}

func TestOCRConverterEngine(t *testing.T) {
	if got := (OCRConverter{}).Engine(); got != EngineOCR {
		t.Fatalf("Engine() = %q, want %q", got, EngineOCR)
	}
}

func indexOfArg(args []string, v string) int {
	for i, a := range args {
		if a == v {
			return i
		}
	}
	return -1
}

func TestOCRTesseractArgs(t *testing.T) {
	args := ocrTesseractArgs("/work/in.png", "/work/out", []string{"-l", "eng"}, "hocr")

	psmIdx := indexOfArg(args, "--psm")
	if psmIdx < 0 || args[psmIdx+1] != ocrPSM {
		t.Fatalf("expected --psm %s as discrete argv elements, got %v", ocrPSM, args)
	}
	oemIdx := indexOfArg(args, "--oem")
	if oemIdx < 0 || args[oemIdx+1] != ocrOEM {
		t.Fatalf("expected --oem %s as discrete argv elements, got %v", ocrOEM, args)
	}
	lIdx := indexOfArg(args, "-l")
	if lIdx < 0 || args[lIdx+1] != "eng" {
		t.Fatalf("expected -l eng as discrete argv elements, got %v", args)
	}
	if args[len(args)-1] != "hocr" {
		t.Fatalf("expected trailing configfile 'hocr', got %v", args)
	}

	// txt target: no trailing configfile.
	txtArgs := ocrTesseractArgs("/work/in.png", "/work/out", []string{"-l", "eng"}, "")
	if txtArgs[len(txtArgs)-1] == "hocr" || txtArgs[len(txtArgs)-1] == "alto" {
		t.Fatalf("txt target must not carry a trailing configfile, got %v", txtArgs)
	}
}

func TestOCRConfigFileMapping(t *testing.T) {
	cases := map[string]string{"txt": "", "hocr": "hocr", "alto": "alto"}
	for target, want := range cases {
		got, ok := ocrConfigFileFor(target)
		if !ok || got != want {
			t.Errorf("ocrConfigFileFor(%q) = (%q, %v), want (%q, true)", target, got, ok, want)
		}
	}
	if _, ok := ocrConfigFileFor("bogus"); ok {
		t.Fatalf("ocrConfigFileFor(\"bogus\") should not be ok")
	}
}

func TestOCRConvertImage(t *testing.T) {
	requireTesseract(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := OCRConverter{}
	if err := c.Convert(ctx, "testdata/ocr/text_sample.png", outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(b), "OctoConv") {
		t.Fatalf("output %q does not contain the fixture's known words", b)
	}
}

func TestOCREmptyInput(t *testing.T) {
	requireTesseract(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := OCRConverter{}
	if err := c.Convert(ctx, "testdata/ocr/blank_image.png", outPath, nil); err != nil {
		t.Fatalf("Convert on a blank/no-text image must succeed (OCRE-05), got: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file to exist even though empty: %v", err)
	}
}

// assertValidXML asserts b parses as valid XML via a full xml.Decoder
// token walk -- the SC3 round-trip proof (PITFALLS.md Pitfall 5). A raw
// token walk alone tolerates a sequence of top-level elements/declarations
// (Go's encoding/xml does not enforce "exactly one root element" during a
// bare Token() walk), so callers merging multiple fragments into ONE
// document must additionally assert a single root/declaration count --
// see TestOCRPageMergerRoundTrip.
func assertValidXML(t *testing.T, b []byte) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("output is not valid XML: %v\n---\n%s", err, b)
		}
	}
}

func TestOCRHOCRRoundTrip(t *testing.T) {
	requireTesseract(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.hocr")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := OCRConverter{}
	if err := c.Convert(ctx, "testdata/ocr/adversarial_text.png", outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	assertValidXML(t, b)
	if !bytes.Contains(b, []byte("&amp;")) || !bytes.Contains(b, []byte("&lt;")) || !bytes.Contains(b, []byte("&gt;")) {
		t.Errorf("expected escaped adversarial content (&amp;/&lt;/&gt;) in hOCR output, got:\n%s", b)
	}
}

func TestOCRALTORoundTrip(t *testing.T) {
	requireTesseract(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.alto")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := OCRConverter{}
	if err := c.Convert(ctx, "testdata/ocr/adversarial_text.png", outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	assertValidXML(t, b)
}

func TestOCRHOCRBBox(t *testing.T) {
	requireTesseract(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.hocr")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := OCRConverter{}
	if err := c.Convert(ctx, "testdata/ocr/text_sample.png", outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	assertValidXML(t, b)
	if !bytes.Contains(b, []byte("bbox")) {
		t.Fatalf("expected word/line bbox title attributes in hOCR output, got:\n%s", b)
	}
}

// TestOCRPageMergerRoundTrip is hermetic (no binary): it feeds synthetic
// single-page hOCR/ALTO fragments -- shaped like real tesseract per-page
// output, each carrying adversarial already-escaped &/</> content -- through
// newOCRPageMerger/AddPage/Finalize and asserts the MERGED multi-page
// document is a SINGLE well-formed document (exactly one XML declaration,
// exactly one root element) containing both pages -- a naive string-concat
// merge of two full per-page documents parses "fine" under a bare
// xml.Decoder token walk (Go's Token() does not itself enforce a single
// root element) but produces TWO XML declarations and TWO root elements,
// which the counts below catch.
func TestOCRPageMergerRoundTrip(t *testing.T) {
	hocrFrag1 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<body>
<div class='ocr_page' id='page_1' title='bbox 0 0 400 120'>
<span class='ocrx_word'>A&amp;B&lt;C&gt;D</span>
</div>
</body>
</html>`)
	hocrFrag2 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<body>
<div class='ocr_page' id='page_1' title='bbox 0 0 400 120'>
<span class='ocrx_word'>E&quot;F</span>
</div>
</body>
</html>`)

	m := newOCRPageMerger("hocr")
	if err := m.AddPage(1, hocrFrag1); err != nil {
		t.Fatalf("AddPage(1): %v", err)
	}
	if err := m.AddPage(2, hocrFrag2); err != nil {
		t.Fatalf("AddPage(2): %v", err)
	}
	out, err := m.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	assertValidXML(t, out)
	if n := bytes.Count(out, []byte("<?xml")); n != 1 {
		t.Errorf("expected exactly ONE XML declaration in the merged hOCR document (naive concat would produce 2), got %d in:\n%s", n, out)
	}
	if n := bytes.Count(out, []byte("<html")); n != 1 {
		t.Errorf("expected exactly ONE root <html> element in the merged hOCR document (naive concat would produce 2), got %d in:\n%s", n, out)
	}
	if !bytes.Contains(out, []byte("&amp;")) || !bytes.Contains(out, []byte("&lt;")) || !bytes.Contains(out, []byte("&gt;")) {
		t.Errorf("expected escaped adversarial content preserved in merged hOCR, got:\n%s", out)
	}
	if bytes.Count(out, []byte("ocr_page")) != 2 {
		t.Errorf("expected 2 merged hOCR pages, got:\n%s", out)
	}
	if !bytes.Contains(out, []byte(`id="page_1"`)) || !bytes.Contains(out, []byte(`id="page_2"`)) {
		t.Errorf("expected renumbered page_1/page_2 ids in merged hOCR, got:\n%s", out)
	}

	altoFrag1 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<alto xmlns="http://www.loc.gov/standards/alto/ns-v3#">
<Layout>
<Page WIDTH="400" HEIGHT="120" ID="page_0">
<PrintSpace><String CONTENT="A&amp;B&lt;C&gt;D"/></PrintSpace>
</Page>
</Layout>
</alto>`)
	altoFrag2 := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<alto xmlns="http://www.loc.gov/standards/alto/ns-v3#">
<Layout>
<Page WIDTH="400" HEIGHT="120" ID="page_0">
<PrintSpace><String CONTENT="E&quot;F"/></PrintSpace>
</Page>
</Layout>
</alto>`)

	am := newOCRPageMerger("alto")
	if err := am.AddPage(1, altoFrag1); err != nil {
		t.Fatalf("AddPage(1): %v", err)
	}
	if err := am.AddPage(2, altoFrag2); err != nil {
		t.Fatalf("AddPage(2): %v", err)
	}
	aout, err := am.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	assertValidXML(t, aout)
	if n := bytes.Count(aout, []byte("<?xml")); n != 1 {
		t.Errorf("expected exactly ONE XML declaration in the merged ALTO document (naive concat would produce 2), got %d in:\n%s", n, aout)
	}
	if n := bytes.Count(aout, []byte("<alto")); n != 1 {
		t.Errorf("expected exactly ONE root <alto> element in the merged ALTO document (naive concat would produce 2), got %d in:\n%s", n, aout)
	}
	if bytes.Count(aout, []byte("<Page")) != 2 {
		t.Errorf("expected 2 merged ALTO pages, got:\n%s", aout)
	}
	if !bytes.Contains(aout, []byte(`ID="page_1"`)) || !bytes.Contains(aout, []byte(`ID="page_2"`)) {
		t.Errorf("expected renumbered page_1/page_2 IDs in merged ALTO, got:\n%s", aout)
	}
}

func TestOCRPageMergerNoPageElement(t *testing.T) {
	m := newOCRPageMerger("hocr")
	if err := m.AddPage(1, []byte(`<html><body><p>no page div here</p></body></html>`)); err == nil {
		t.Fatalf("expected AddPage to fail closed when no ocr_page element is present")
	}
}
