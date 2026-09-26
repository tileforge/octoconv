// Package convert (this file): ocrsandbox.go is OCR's half of the
// engineExec contract -- the measured grant, the per-job ruleset builder,
// the per-page render-directory placement, and (Task 2) the preflight that
// proves all three OCR routes -- mirroring pandocsandbox.go's own shape, but
// for a class with FOUR engines and two call-site files (ocr.go, ocrpdf.go,
// ocrguards.go) sharing ONE ruleset per job instead of pandoc's single call
// site.
package convert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ocrReadOnly is what OCR's four engines need to read, beyond their own
// binaries, MEASURED rather than reasoned -- SANDBOX-PATHS.md's "ocr"
// section, conf05:ocr:readonly, re-measured by plan 02 through the REAL
// job-path argv sequence (pdfinfo page-count/per-page-size, pdftotext text
// layer, pdftoppm render, tesseract) -- not tesseract/pdftoppm alone, which
// is what the original 2026-09-20 measurement covered and which silently
// omitted pdfinfo/pdftotext.
//
// /usr/share/fonts is here because it is NECESSARY, proven by removal: a
// born-digital PDF with a non-embedded font renders a DIFFERENT (font-less)
// page without it, and tesseract then returns EMPTY text on that page --
// both pdftoppm and tesseract exit 0 either way, so this is exactly the
// class of trap a return-code check alone would miss.
var ocrReadOnly = []string{
	"/lib",
	"/usr/local/lib",
	"/usr/local/share/tessdata",
	"/etc/ld.so.cache",
	"/etc/localtime",
	"/usr/share/fonts",
	"/sys/devices/system/cpu/possible",
}

// ocrEngines is every binary OCRConverter's job-path invokes: tesseract
// (image route and every per-page PDF render), pdftoppm (PDF page render),
// pdfinfo (page count AND per-page page-box size -- called twice, on every
// PDF job), pdftotext (text-layer extraction, txt target only). Each is
// resolved through PATH and added to the ruleset by forJob, because none of
// the four appear in a filesystem trace -- they are execve'd, not opened.
var ocrEngines = []string{"tesseract", "pdftoppm", "pdfinfo", "pdftotext"}

// exec builds this job's engineExec from outPath's directory -- the ONE
// ruleset shared by every OCR engine call site of this job, whichever route
// (image, PDF text-layer, or PDF render) it ends up taking. Mirrors
// PandocConverter.Convert's own forJob call, but as a named method because
// OCR has several call sites (ocr.go's image route, ocrpdf.go's PDF route,
// ocrguards.go's guard stage) that must all share the SAME engineExec for
// one job rather than each rebuilding it.
func (c OCRConverter) exec(outPath string) engineExec {
	return c.Sandbox.forJob(filepath.Dir(outPath), ocrReadOnly, ocrEngines)
}

// ocrPageWorkDir creates the per-page PDF render directory INSIDE the job
// directory (filepath.Dir(outPath)) -- the only writable path the job's
// ruleset grants. A directory under the shared system temp dir, as this
// pipeline used before this plan, would be denied to pdftoppm and tesseract
// under Landlock (nothing outside the job directory is writable), and would
// be visible to other jobs sharing that same temp dir even when unconfined.
func ocrPageWorkDir(outPath string) (string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(outPath), ".ocr-pdf-")
	if err != nil {
		return "", fmt.Errorf("ocr: pdf tmpdir: %w", err)
	}
	return dir, nil
}

// WithSandbox returns c with Sandbox set to a *engineSandbox built from
// helper -- mirrors PandocConverter.WithSandbox's own shape: a method
// rather than a ConfinedOCR-style constructor, because OCRConverter also
// carries MaxImagePixels/MaxPages, and this lets cmd/ocr-worker keep
// configuring it in one place: buildOCRConverter(...).WithSandbox(helper).
func (c OCRConverter) WithSandbox(helper []string) OCRConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}

// ocrPreflightText is the preflight fixture's own content -- 24
// non-whitespace characters, well above ocrUsableTextLayerThreshold (16),
// so a txt target takes the text-layer route (pdfinfo + pdftotext) rather
// than falling through to a render. Restricted to A-Z and a space so
// ocrPreflightPDF never needs PDF string escaping.
//
// ocrPreflightWord is the first word of ocrPreflightText -- what the
// render and image routes (OCR through tesseract, not pdftotext) are
// judged against: a tesseract recognition of the full phrase is not
// guaranteed byte-for-byte, but its first, most legible word is.
const (
	ocrPreflightText = "OCTOCONV SANDBOX PREFLIGHT"
	ocrPreflightWord = "OCTOCONV"
)

// ocrPreflightPDF assembles a minimal, xref-correct, single-page PDF 1.4
// whose page draws text with a font that is deliberately NOT embedded --
// exactly the born-digital shape plan 02 measured (SANDBOX-PATHS.md's "ocr"
// section): pdftoppm must substitute a real Helvetica through fontconfig to
// render any glyphs at all, which is what makes /usr/share/fonts's presence
// in ocrReadOnly provable by this preflight rather than merely assumed.
//
// text is restricted to A-Z and spaces by every caller (ocrPreflightText),
// so no PDF string escaping is needed here. Reused directly by
// probesandbox.go (plan 69-02): a preflightMarker() value (lowercase hex,
// digits and hyphens) is equally safe here, because text must not contain
// the PDF string delimiters '(', ')' or '\' -- none of which a hex marker
// ever produces.
//
// The object numbering/xref-table shape mirrors this package's own
// pdflex_test.go buildPDF helper (sequential object numbers, a correct
// cross-reference table recording each object's real byte offset) -- not a
// call to that test helper, which is test-only, but the same approach,
// extended with a /Root entry in the trailer so real tools (pdfinfo,
// pdftoppm, pdftotext, tesseract) accept the file as valid, not just this
// package's own lenient PDF lexer.
func ocrPreflightPDF(text string) []byte {
	const width, height = 1000, 200
	content := []byte(fmt.Sprintf("BT\n/F1 40 Tf\n20 100 Td\n(%s) Tj\nET\n", text))
	contentObj := append(
		[]byte(fmt.Sprintf("<< /Length %d >>\nstream\n", len(content))),
		append(content, []byte("\nendstream")...)...,
	)

	objects := [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>", width, height)),
		contentObj,
		// /Type /Font /Subtype /Type1 /BaseFont /Helvetica, no /FontDescriptor
		// and no /FontFile of any kind -- the font is NOT embedded.
		[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n", i+1)
		buf.Write(body)
		buf.WriteString("\nendobj\n")
	}
	xrefAt := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefAt)
	return buf.Bytes()
}

// OCRPreflight proves the engine can actually be EXECUTED, under the
// ruleset c would build for a job, along all THREE routes a real job mix
// takes -- text-layer extraction, per-page render, and a bare image -- plus
// a negative run, before any job depends on it. Judged by output CONTENT in
// every direction (CONF-04), never by exit status alone: an empty hOCR from
// the render route means the page rendered without glyphs (the font-
// substitution trap plan 02 measured), not a legitimate empty result.
//
// nil when c is unconfined: nothing to prove for a deployment that has
// deliberately turned confinement off.
func OCRPreflight(ctx context.Context, c OCRConverter) error {
	if !c.Sandbox.confines() {
		return nil
	}

	w, cleanup, err := newPreflightWorkspace("ocr")
	if err != nil {
		return err
	}
	defer cleanup()

	inside, outside, err := w.place("in.pdf", ocrPreflightPDF(ocrPreflightText))
	if err != nil {
		return err
	}

	// Text-layer route: pdfinfo (page count) + pdftotext (extraction) -- the
	// extracted text layer IS the result for a txt target with a usable
	// text layer (D-05/D-06, ocrpdf.go).
	layerOut := filepath.Join(w.jobDir, "layer.txt")
	if err := c.Convert(ctx, inside, layerOut, nil); err != nil {
		return fmt.Errorf("text-layer route: %w", err)
	}
	if err := requireOutput(layerOut, []byte(ocrPreflightText)); err != nil {
		return fmt.Errorf("text-layer route: %w", err)
	}

	// Render route: hocr ALWAYS renders (D-06 -- pdftotext produces no word
	// or line boxes, so an existing text layer cannot answer the request)
	// -- pdfinfo (count) + pdfinfo (per-page box size) + pdftoppm (render,
	// with font substitution) + tesseract (OCR). An empty hOCR here means
	// the page rendered without glyphs, the silent failure this preflight
	// exists to catch (T-68-14).
	renderOut := filepath.Join(w.jobDir, "render.hocr")
	if err := c.Convert(ctx, inside, renderOut, nil); err != nil {
		return fmt.Errorf("render route: %w", err)
	}
	if err := requireOutput(renderOut, []byte(ocrPreflightWord)); err != nil {
		return fmt.Errorf("render route: %w", err)
	}

	// Image route: render page 1 to a PNG through the SAME job's ruleset,
	// then run the plain image path (tesseract alone) over it -- proves the
	// image-source code path independently of the PDF-source guard stage.
	pageOutBase := filepath.Join(w.jobDir, "page")
	if _, err := c.exec(pageOutBase+".png").run(ctx, "pdftoppm",
		ocrRasterizePageArgs(inside, pageOutBase, 1, ocrRasterizeDPI)...); err != nil {
		return fmt.Errorf("image route: render page: %w", err)
	}
	imageOut := filepath.Join(w.jobDir, "image.txt")
	if err := c.Convert(ctx, pageOutBase+".png", imageOut, nil); err != nil {
		return fmt.Errorf("image route: %w", err)
	}
	if err := requireOutput(imageOut, []byte(ocrPreflightWord)); err != nil {
		return fmt.Errorf("image route: %w", err)
	}

	// Effectiveness: the IDENTICAL bytes, converted under the IDENTICAL
	// ruleset, from OUTSIDE the job directory.
	negOut := filepath.Join(w.jobDir, "neg.txt")
	convErr := c.Convert(ctx, outside, negOut, nil)
	if err := requireDenied(convErr, negOut, []byte(ocrPreflightWord)); err != nil {
		return fmt.Errorf("read a file outside the job directory: %w", err)
	}
	return nil
}
