// Package convert (this file): ocrsandbox.go is OCR's half of the
// engineExec contract -- the measured grant, the per-job ruleset builder,
// the per-page render-directory placement, and (Task 2) the preflight that
// proves all three OCR routes -- mirroring pandocsandbox.go's own shape, but
// for a class with FOUR engines and two call-site files (ocr.go, ocrpdf.go,
// ocrguards.go) sharing ONE ruleset per job instead of pandoc's single call
// site.
package convert

import (
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
