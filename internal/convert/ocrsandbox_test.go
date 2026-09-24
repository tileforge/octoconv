package convert

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The measured set, pinned. Every entry here is proven by
// .planning/research/SANDBOX-PATHS.md's "ocr" section, conf05:ocr:readonly,
// re-measured by plan 02 through the real job-path argv (pdfinfo/pdftotext
// included, not just tesseract/pdftoppm).
func TestOCRReadOnlySetIsTheMeasuredOne(t *testing.T) {
	want := []string{
		"/lib",
		"/usr/local/lib",
		"/usr/local/share/tessdata",
		"/etc/ld.so.cache",
		"/etc/localtime",
		"/usr/share/fonts",
		"/sys/devices/system/cpu/possible",
	}
	if len(ocrReadOnly) != len(want) {
		t.Fatalf("ocrReadOnly has %d paths, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(ocrReadOnly), len(want))
	}
	for i := range want {
		if ocrReadOnly[i] != want[i] {
			t.Errorf("path %d is %q, want %q", i, ocrReadOnly[i], want[i])
		}
	}
	for _, p := range ocrReadOnly {
		if !filepath.IsAbs(p) {
			t.Errorf("%q is not absolute; the helper grants paths verbatim", p)
		}
	}
}

func TestOCREnginesAreTheMeasuredOnes(t *testing.T) {
	want := []string{"tesseract", "pdftoppm", "pdfinfo", "pdftotext"}
	if len(ocrEngines) != len(want) {
		t.Fatalf("ocrEngines has %d entries, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(ocrEngines), len(want))
	}
	for i := range want {
		if ocrEngines[i] != want[i] {
			t.Errorf("ocrEngines[%d] = %q, want %q", i, ocrEngines[i], want[i])
		}
	}
}

// OCRConverter stays comparable: WithSandbox(nil) equals the zero value, a
// confined converter does not, and the type is usable as a map key -- fails
// to COMPILE if Sandbox stops being a pointer.
func TestOCRConverterStaysComparable(t *testing.T) {
	var a, b OCRConverter
	if a != b {
		t.Fatal("two zero converters differ")
	}
	if got := (OCRConverter{}).WithSandbox(nil); got != a {
		t.Error("WithSandbox(nil) does not equal the zero value")
	}

	confined := OCRConverter{}.WithSandbox([]string{"/usr/local/bin/ocr-worker", "__sandbox-exec"})
	if confined == a {
		t.Error("a confined converter equals an unconfined one, so the field is not being compared at all")
	}
	seen := map[OCRConverter]bool{a: true}
	if !seen[b] {
		t.Error("the zero converter is not its own map key")
	}
}

// ---- fakes shared by TestOCREveryEngineRunsThroughTheHelper and
// TestOCRRenderDirectoryIsInsideTheJobDirectory ----

// ocrFakeEngineSet writes pdfinfo/pdftoppm/tesseract fakes -- and a
// pdftotext fake that reports either a usable or an unusable text layer --
// into a fresh directory and prepends it to PATH.
//
//   - pdfinfo always prints BOTH the whole-document "Pages:" line and a
//     per-page "Page    1 size:" line, so it answers ocrPDFPageCount's and
//     ocrPDFPageSizeForPage's different invocation forms identically.
//   - pdftotext prints a 30-character line (usableTextLayer) or nothing at
//     all (not usableTextLayer) -- above/below ocrUsableTextLayerThreshold
//     (16), so it drives ocrPDFGuardStage's routing decision.
//   - pdftoppm touches "<last argv token>.png" -- ocrRasterizePageArgs's own
//     -singlefile convention, where the last token is the render's outBase.
//   - tesseract writes "hello" to "<second argv token>.txt" --
//     ocrTesseractArgs' own convention, where the second token is always
//     outBase (inPath is first) for every txt-target invocation in this
//     file's scenarios (no configfile).
func ocrFakeEngineSet(t *testing.T, usableTextLayer bool) string {
	t.Helper()
	dir := t.TempDir()
	fakeEngine(t, dir, "pdfinfo", "printf 'Pages:          1\\n'\nprintf 'Page    1 size: 612 x 144 pts\\n'\n")
	if usableTextLayer {
		fakeEngine(t, dir, "pdftotext", "printf '%s' '123456789012345678901234567890'\n")
	} else {
		fakeEngine(t, dir, "pdftotext", ":\n")
	}
	fakeEngine(t, dir, "pdftoppm", "last=\"\"\nfor a in \"$@\"; do last=\"$a\"; done\ntouch \"$last.png\"\n")
	fakeEngine(t, dir, "tesseract", "printf 'hello' > \"$2.txt\"\n")
	pathWith(t, dir)
	return dir
}

// writeOCRTestPNG writes a real, decodable NxN PNG -- the image route's
// pixel-dimension guard (enforceImagePixelBound) decodes real image bytes
// via Go's stdlib, not a fake tool, so a placeholder byte string will not do
// here the way it does for the PDF fixtures (pdfinfo/pdftotext/pdftoppm are
// all faked and never actually parse their input).
func writeOCRTestPNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}

// recordedEngineNames extracts, per recorded helper invocation, the base
// name of the resolved engine path run after the "--" separator.
func recordedEngineNames(t *testing.T, invocations [][]string) []string {
	t.Helper()
	names := make([]string, 0, len(invocations))
	for _, argv := range invocations {
		sep := indexOf(argv, "--")
		if sep < 0 || sep+1 >= len(argv) {
			t.Fatalf("invocation %v has no -- separator", argv)
		}
		names = append(names, filepath.Base(argv[sep+1]))
	}
	return names
}

// requireOnlyRW asserts every invocation carries exactly one rw: token,
// equal to want -- the job directory -- and no other.
func requireOnlyRW(t *testing.T, invocations [][]string, want string) {
	t.Helper()
	for _, argv := range invocations {
		sep := indexOf(argv, "--")
		if sep < 0 {
			t.Fatalf("invocation %v has no -- separator", argv)
		}
		var rw []string
		for _, tok := range argv[:sep] {
			if strings.HasPrefix(tok, "rw:") {
				rw = append(rw, strings.TrimPrefix(tok, "rw:"))
			}
		}
		if len(rw) != 1 || rw[0] != want {
			t.Errorf("invocation %v rw tokens = %v, want exactly [%s]", argv, rw, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestOCREveryEngineRunsThroughTheHelper proves every one of OCR's four
// engines, across the three source/route combinations, runs through the
// job's helper with a ruleset scoped to that job's own directory -- never a
// direct runCommand call and never a stray writable path.
func TestOCREveryEngineRunsThroughTheHelper(t *testing.T) {
	t.Run("scanned pdf to txt", func(t *testing.T) {
		helper, calls := fakeSandboxHelper(t)
		ocrFakeEngineSet(t, false)
		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.pdf")
		if err := os.WriteFile(inPath, []byte("%PDF-fake-scanned\n"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		outPath := filepath.Join(jobDir, "out.txt")

		c := OCRConverter{}.WithSandbox(helper)
		if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}

		invocations := calls()
		got := recordedEngineNames(t, invocations)
		// pdfinfo (page count) -> pdftotext (guard stage's txt-target check,
		// unusable here) -> pdfinfo (per-page box size) -> pdftoppm (render)
		// -> tesseract (OCR).
		want := []string{"pdfinfo", "pdftotext", "pdfinfo", "pdftoppm", "tesseract"}
		if !equalStrings(got, want) {
			t.Fatalf("recorded engines = %v, want %v", got, want)
		}
		requireOnlyRW(t, invocations, jobDir)
	})

	t.Run("born-digital pdf to txt", func(t *testing.T) {
		helper, calls := fakeSandboxHelper(t)
		ocrFakeEngineSet(t, true)
		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.pdf")
		if err := os.WriteFile(inPath, []byte("%PDF-fake-born-digital\n"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		outPath := filepath.Join(jobDir, "out.txt")

		c := OCRConverter{}.WithSandbox(helper)
		if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}

		invocations := calls()
		got := recordedEngineNames(t, invocations)
		// pdfinfo (page count) -> pdftotext (usable text layer: extraction
		// IS the result, done=true) -- no render loop at all.
		want := []string{"pdfinfo", "pdftotext"}
		if !equalStrings(got, want) {
			t.Fatalf("recorded engines = %v, want %v", got, want)
		}
		requireOnlyRW(t, invocations, jobDir)
	})

	t.Run("png to txt", func(t *testing.T) {
		helper, calls := fakeSandboxHelper(t)
		ocrFakeEngineSet(t, false)
		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.png")
		writeOCRTestPNG(t, inPath)
		outPath := filepath.Join(jobDir, "out.txt")

		c := OCRConverter{}.WithSandbox(helper)
		if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}

		invocations := calls()
		got := recordedEngineNames(t, invocations)
		want := []string{"tesseract"}
		if !equalStrings(got, want) {
			t.Fatalf("recorded engines = %v, want %v", got, want)
		}
		requireOnlyRW(t, invocations, jobDir)
	})
}

// TestOCRRenderDirectoryIsInsideTheJobDirectory proves the per-page render
// directory (ocrPageWorkDir) lies under the job directory (outPath's own
// directory), not the shared system temp dir -- pdftoppm's own outBase
// argument is the evidence, since it is the last token of its argv.
func TestOCRRenderDirectoryIsInsideTheJobDirectory(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	ocrFakeEngineSet(t, false)
	jobDir := t.TempDir()
	inPath := filepath.Join(jobDir, "in.pdf")
	if err := os.WriteFile(inPath, []byte("%PDF-fake-scanned\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(jobDir, "out.txt")

	c := OCRConverter{}.WithSandbox(helper)
	if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	var pdftoppmArgv []string
	for _, argv := range calls() {
		sep := indexOf(argv, "--")
		if sep >= 0 && sep+1 < len(argv) && filepath.Base(argv[sep+1]) == "pdftoppm" {
			pdftoppmArgv = argv[sep+1:]
		}
	}
	if pdftoppmArgv == nil {
		t.Fatal("pdftoppm was never invoked")
	}
	outBase := pdftoppmArgv[len(pdftoppmArgv)-1]
	renderDir := filepath.Dir(outBase)
	if renderDir != jobDir && !strings.HasPrefix(renderDir, jobDir+string(filepath.Separator)) {
		t.Errorf("pdftoppm's own output base %q is not inside the job directory %q -- the render "+
			"directory has escaped the only path the ruleset grants write access to", outBase, jobDir)
	}
}

// TestOCRPreflightPDFIsBornDigitalWithANonEmbeddedFont pins the preflight
// fixture's own shape: a structurally valid, single-page PDF whose Helvetica
// font carries no embedded font program, so pdftoppm must substitute a real
// one through fontconfig -- exactly the born-digital case plan 02 measured
// (/usr/share/fonts's necessity).
func TestOCRPreflightPDFIsBornDigitalWithANonEmbeddedFont(t *testing.T) {
	pdf := ocrPreflightPDF(ocrPreflightText)

	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("ocrPreflightPDF does not start with %%PDF-: %q", pdf[:min(20, len(pdf))])
	}
	if !bytes.Contains(pdf, []byte("/BaseFont /Helvetica")) {
		t.Errorf("ocrPreflightPDF does not name /BaseFont /Helvetica")
	}
	if bytes.Contains(pdf, []byte("/FontFile")) {
		t.Errorf("ocrPreflightPDF embeds a font program (/FontFile present) -- it must not: the whole " +
			"point of this fixture is that pdftoppm has to substitute a real font")
	}

	xrefMarker := []byte("\nxref\n")
	xrefIdx := bytes.Index(pdf, xrefMarker)
	if xrefIdx < 0 {
		t.Fatalf("no xref table found in %q", pdf)
	}
	lines := strings.Split(string(pdf[xrefIdx+len(xrefMarker):]), "\n")
	var objCount int
	if _, err := fmt.Sscanf(lines[0], "0 %d", &objCount); err != nil {
		t.Fatalf("malformed xref subsection header %q: %v", lines[0], err)
	}
	// lines[1] is the free-list entry ("0000000000 65535 f "); the object
	// offsets follow, one per remaining line.
	for i := 0; i < objCount-1; i++ {
		var offset int
		if _, err := fmt.Sscanf(lines[2+i], "%d", &offset); err != nil {
			t.Fatalf("malformed xref entry %q: %v", lines[2+i], err)
		}
		want := fmt.Sprintf("%d 0 obj", i+1)
		if offset+len(want) > len(pdf) {
			t.Fatalf("xref offset %d for object %d is past the end of the PDF (%d bytes)", offset, i+1, len(pdf))
		}
		if got := string(pdf[offset : offset+len(want)]); got != want {
			t.Errorf("xref offset %d points at %q, want %q", offset, got, want)
		}
	}

	pdftotext, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed, skipping content extraction check")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "preflight.pdf")
	if err := os.WriteFile(path, pdf, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	out, err := exec.Command(pdftotext, path, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("pdftotext %s: %v: %s", path, err, out)
	}
	if !strings.Contains(string(out), ocrPreflightText) {
		t.Errorf("pdftotext output %q does not contain %q", out, ocrPreflightText)
	}
}

// TestOCRPreflightUnconfinedProvesNothing mirrors
// TestPandocPreflightUnconfinedProvesNothing: nothing to prove for a
// deployment that has deliberately turned confinement off, and no
// workspace is even created for it.
func TestOCRPreflightUnconfinedProvesNothing(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "ocr-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	c := OCRConverter{}
	if err := OCRPreflight(context.Background(), c); err != nil {
		t.Errorf("OCRPreflight on an unconfined converter: %v, want nil", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("OCRPreflight created a workspace (%v -> %v) though the converter is unconfined",
			before, after)
	}
}
