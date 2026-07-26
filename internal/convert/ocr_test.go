package convert

import (
	"context"
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
