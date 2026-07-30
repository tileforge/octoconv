package convert

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestNormalizeFormat(t *testing.T) {
	cases := map[string]string{
		"PNG":   "png",
		".jpg":  "jpg",
		"JPEG":  "jpg",
		" tif ": "tiff",
		"webp":  "webp",
		"htm":   "html",
		".HTM":  "html",
	}
	for in, want := range cases {
		if got := NormalizeFormat(in); got != want {
			t.Errorf("NormalizeFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistryLibvipsPairs(t *testing.T) {
	// Default registry is populated by converters.go init().
	if !Default.Supports("png", "webp") {
		t.Error("expected png->webp to be supported")
	}
	// Alias + case normalization should resolve to the same pair.
	if !Default.Supports("JPEG", ".PNG") {
		t.Error("expected jpeg->png (aliased/cased) to be supported")
	}
	if Default.Supports("png", "png") {
		t.Error("identity pair png->png must not be registered")
	}
	if Default.Supports("png", "mp3") {
		t.Error("unsupported pair png->mp3 should not be supported")
	}
	if _, ok := Default.Lookup("heic", "jpg"); !ok {
		t.Error("expected a converter for heic->jpg")
	}
}

func TestConverterEngine(t *testing.T) {
	if got := (LibvipsConverter{}).Engine(); got != "image" {
		t.Errorf("LibvipsConverter{}.Engine() = %q, want image", got)
	}
	if got := (LibreOfficeConverter{}).Engine(); got != "document" {
		t.Errorf("LibreOfficeConverter{}.Engine() = %q, want document", got)
	}
}

func TestRegistryEngineFor(t *testing.T) {
	// Default registry is populated by converters.go init().
	if engine, ok := Default.EngineFor("png", "webp"); !ok || engine != "image" {
		t.Errorf("EngineFor(png, webp) = (%q, %v), want (image, true)", engine, ok)
	}
	if engine, ok := Default.EngineFor("docx", "pdf"); !ok || engine != "document" {
		t.Errorf("EngineFor(docx, pdf) = (%q, %v), want (document, true)", engine, ok)
	}
	// Alias normalization via Lookup.
	if engine, ok := Default.EngineFor("jpeg", "webp"); !ok || engine != "image" {
		t.Errorf("EngineFor(jpeg, webp) = (%q, %v), want (image, true)", engine, ok)
	}
	// Unsupported pair: zero-value string, false.
	if engine, ok := Default.EngineFor("png", "mp3"); ok || engine != "" {
		t.Errorf("EngineFor(png, mp3) = (%q, %v), want (\"\", false)", engine, ok)
	}
	// All 6 document source formats -> ("document", true).
	for _, from := range []string{"docx", "xlsx", "pptx", "odt", "ods", "odp"} {
		if engine, ok := Default.EngineFor(from, "pdf"); !ok || engine != "document" {
			t.Errorf("EngineFor(%s, pdf) = (%q, %v), want (document, true)", from, engine, ok)
		}
	}
}

// TestRegistryEngineForOCRRouting pins OCRP-01/D-01 (44-CONTEXT.md): once
// OCRConverter{} is registered (converters.go init()), EngineFor disambiguates
// purely by TARGET with zero new routing code -- jpg->txt and pdf->txt route
// to "ocr" while jpg->png (the pre-existing image pair) still routes to
// "image". This is the single acceptance test proving the target-based
// disambiguation the whole phase's routing design rests on.
func TestRegistryEngineForOCRRouting(t *testing.T) {
	if engine, ok := Default.EngineFor("jpg", "txt"); !ok || engine != EngineOCR {
		t.Errorf("EngineFor(jpg, txt) = (%q, %v), want (%q, true)", engine, ok, EngineOCR)
	}
	if engine, ok := Default.EngineFor("pdf", "txt"); !ok || engine != EngineOCR {
		t.Errorf("EngineFor(pdf, txt) = (%q, %v), want (%q, true)", engine, ok, EngineOCR)
	}
	if engine, ok := Default.EngineFor("jpg", "png"); !ok || engine != EngineImage {
		t.Errorf("EngineFor(jpg, png) = (%q, %v), want (%q, true) -- image engine must still win its own target", engine, ok, EngineImage)
	}
	// hocr/alto targets also route to ocr for every advertised OCR source.
	for _, from := range []string{"jpg", "png", "tiff", "webp", "pdf"} {
		for _, to := range []string{"txt", "hocr", "alto"} {
			if engine, ok := Default.EngineFor(from, to); !ok || engine != EngineOCR {
				t.Errorf("EngineFor(%s, %s) = (%q, %v), want (%q, true)", from, to, engine, ok, EngineOCR)
			}
		}
	}
}

// TestRegistryEngineForPandocRouting pins PDCP-01/D-02 (48-CONTEXT.md): once
// PandocConverter{} is registered (converters.go init()), EngineFor
// disambiguates purely by TARGET with zero new routing code -- md->html,
// docx->md, and html->md all route to "pandoc" while docx->pdf keeps routing
// to "document" (LibreOfficeConverter) and html->pdf keeps routing to "html"
// (ChromiumConverter). This is the acceptance test proving the target-based
// disambiguation the whole plan's registration-only wiring rests on.
func TestRegistryEngineForPandocRouting(t *testing.T) {
	if engine, ok := Default.EngineFor("md", "html"); !ok || engine != EnginePandoc {
		t.Errorf("EngineFor(md, html) = (%q, %v), want (%q, true)", engine, ok, EnginePandoc)
	}
	if engine, ok := Default.EngineFor("docx", "md"); !ok || engine != EnginePandoc {
		t.Errorf("EngineFor(docx, md) = (%q, %v), want (%q, true)", engine, ok, EnginePandoc)
	}
	if engine, ok := Default.EngineFor("html", "md"); !ok || engine != EnginePandoc {
		t.Errorf("EngineFor(html, md) = (%q, %v), want (%q, true)", engine, ok, EnginePandoc)
	}
	if engine, ok := Default.EngineFor("docx", "pdf"); !ok || engine != EngineDocument {
		t.Errorf("EngineFor(docx, pdf) = (%q, %v), want (%q, true) -- document engine must still win its own target", engine, ok, EngineDocument)
	}
	if engine, ok := Default.EngineFor("html", "pdf"); !ok || engine != EngineHTML {
		t.Errorf("EngineFor(html, pdf) = (%q, %v), want (%q, true) -- html/chromium engine must still win its own target", engine, ok, EngineHTML)
	}
}

// TestRegistryClasses verifies D-06: Classes() groups every registered pair
// under its engine class, a known libvips pair (png->webp) surfaces under
// "image", and repeated calls return a stable (deterministically sorted)
// result.
func TestRegistryClasses(t *testing.T) {
	classes := Default.Classes()

	imagePairs, ok := classes["image"]
	if !ok {
		t.Fatal(`Classes() missing "image" class`)
	}
	found := false
	for _, p := range imagePairs {
		if p.From == "png" && p.To == "webp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(`Classes()["image"] = %+v, want it to contain {png webp}`, imagePairs)
	}

	// Stability: a second call must return the identical grouping/order.
	again := Default.Classes()
	againPairs, ok := again["image"]
	if !ok || len(againPairs) != len(imagePairs) {
		t.Fatalf("Classes() not stable across calls: first=%+v second=%+v", imagePairs, againPairs)
	}
	for i := range imagePairs {
		if imagePairs[i] != againPairs[i] {
			t.Errorf("Classes() order not stable at index %d: first=%+v second=%+v", i, imagePairs[i], againPairs[i])
		}
	}
}

// TestRunCommandTimeout verifies the hardened exec kills a long-running child
// when the context deadline fires, and returns the context error.
func TestRunCommandTimeout(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runCommand(ctx, "sleep", "10")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from timed-out command, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("command not killed promptly: took %v", elapsed)
	}
}

// TestRunCommandFailure surfaces a non-zero exit as an error.
func TestRunCommandFailure(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("false not available")
	}
	if _, err := runCommand(context.Background(), "false"); err == nil {
		t.Fatal("expected error from `false`, got nil")
	}
}
