package worker

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
)

// requireOCRIntegrationBinaries skips the calling test unless pdfinfo,
// pdftoppm, pdftotext, AND tesseract are all on PATH -- mirrors
// internal/convert/ocrpdf_test.go's requireOCRPDFBinaries so `go test
// -short` (and any CI without poppler/tesseract installed) stays hermetic,
// per this plan's acceptance criterion that the test skips cleanly rather
// than fails when the engines are absent.
func requireOCRIntegrationBinaries(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"pdfinfo", "pdftoppm", "pdftotext", "tesseract"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found on PATH, skipping real-binary OCR progress integration test", bin)
		}
	}
}

// TestOCRProgressIntegration proves SC4 at the integration level: OCR jobs
// drive the completely unmodified v1.9 progress machinery
// (progressSink/UpdateProgress) through the EXACT worker seam process()
// uses, runConvertWithOptionalProgress -- not a bare call to
// OCRConverter.ConvertWithProgress. A real convert.OCRConverter{} runs
// against the committed Phase 43 fixtures (internal/convert/testdata/ocr/),
// and a spyProgressRepo (the same fake worker_test.go's throttle/freeze
// tests already use) records every UpdateProgress call so both null->100
// (single image) and the page-by-page ramp (multi-page scanned PDF) are
// pinned as integration-level, not unit-level, behavior. No new progress
// type, no edits to progress.go/sse.go -- this test only consumes their
// existing exported contract (asserted separately by
// TestReuseUnchangedFence in this same file).
func TestOCRProgressIntegration(t *testing.T) {
	requireOCRIntegrationBinaries(t)

	// runConvertWithOptionalProgress only drives the ProgressConverter path
	// when minInterval > 0 (D-01's own "throttling configured" gate) -- a
	// minInterval of exactly 0 falls back to the plain conv.Convert() path
	// and emits ZERO UpdateProgress calls, which would defeat this test's
	// purpose. minInterval=1ns effectively disables the interval gate (real
	// per-page tesseract/pdftoppm processing time always exceeds 1ns) while
	// minDeltaPct=0 disables the delta gate, so every sink.Update the
	// converter emits lands immediately -- the ramp asserted below reflects
	// the converter's OWN page-granular signal (ocrprogress.go), not an
	// artifact of the PROG-03 throttle gate.
	const minInterval = time.Nanosecond
	const minDeltaPct = 0

	t.Run("multi-page scanned PDF ramps page-by-page", func(t *testing.T) {
		repo := &spyProgressRepo{}
		jobID := uuid.New()
		conv := convert.OCRConverter{}
		dir := t.TempDir()
		outPath := filepath.Join(dir, "out.txt")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		inPath := filepath.Join("..", "convert", "testdata", "ocr", "multipage_image_only.pdf")
		if err := runConvertWithOptionalProgress(ctx, conv, inPath, outPath, nil, repo, jobID, "", minInterval, minDeltaPct, false, "", nil, nil, 0); err != nil {
			t.Fatalf("runConvertWithOptionalProgress: %v", err)
		}

		// The 3-page fixture drives 3 page-native ticks (33, 66, 100 --
		// pctForPage's own last-page value naturally lands on 100) PLUS the
		// unconditional terminal-100 bypass write runConvertWithOptionalProgress
		// always issues on success, so 4 total UpdateProgress calls are
		// expected -- not just "at least 3".
		if len(repo.updates) < 3 {
			t.Fatalf("UpdateProgress calls = %+v, want at least 3 (>=2 distinct pre-100 page ticks + a terminal 100)", repo.updates)
		}

		last := -1
		distinctBelow100 := map[int]bool{}
		for i, u := range repo.updates {
			if u.pct < last {
				t.Fatalf("update %d: pct regressed from %d to %d -- want a genuinely non-decreasing ramp", i, last, u.pct)
			}
			last = u.pct
			if u.pct < 100 {
				distinctBelow100[u.pct] = true
			}
		}
		if len(distinctBelow100) < 2 {
			t.Fatalf("distinct pre-100 pct values = %v, want >= 2 (genuine page-by-page ramp, not a single jump)", distinctBelow100)
		}

		finalPct := repo.updates[len(repo.updates)-1].pct
		if finalPct != 100 {
			t.Fatalf("final UpdateProgress pct = %d, want 100 (the unconditional terminal bypass write)", finalPct)
		}
	})

	t.Run("single image job reports null then terminal 100 (no fabricated ramp)", func(t *testing.T) {
		repo := &spyProgressRepo{}
		jobID := uuid.New()
		conv := convert.OCRConverter{}
		dir := t.TempDir()
		outPath := filepath.Join(dir, "out.txt")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		inPath := filepath.Join("..", "convert", "testdata", "ocr", "text_sample.png")
		if err := runConvertWithOptionalProgress(ctx, conv, inPath, outPath, nil, repo, jobID, "", minInterval, minDeltaPct, false, "", nil, nil, 0); err != nil {
			t.Fatalf("runConvertWithOptionalProgress: %v", err)
		}

		// The image path emits ZERO sink.Update calls (OCR-04: ConvertWithProgress
		// delegates straight to Convert for a non-PDF source) -- the ONLY
		// UpdateProgress call must be the unconditional terminal-100 bypass
		// write runConvertWithOptionalProgress issues on success. That is the
		// null->100 contract: no intermediate value is ever recorded.
		if len(repo.updates) != 1 {
			t.Fatalf("UpdateProgress calls = %+v, want exactly 1 (the terminal 100 bypass only, no fabricated intermediate ramp)", repo.updates)
		}
		if repo.updates[0].pct != 100 {
			t.Fatalf("only UpdateProgress call pct = %d, want 100", repo.updates[0].pct)
		}
	})
}

// TestOCRProgressIntegrationSingleOutput pins the W2 plan-checker
// invariant: a successful OCR conversion through the SAME
// runConvertWithOptionalProgress seam process() uses produces exactly one
// output file on disk (single-output, D-01 Phase 43) -- proving
// process()'s single-AddOutput-per-job shape was never widened for OCR.
// This does not exercise process() itself (h.repo/h.store are concrete
// types, not interfaces -- ARCHITECTURE.md's "Key Abstractions" note --
// so a full Handler cannot be constructed in a pure unit test); it proves
// the smallest testable proxy: one converter invocation, through the exact
// worker seam, writes to exactly one outPath.
func TestOCRProgressIntegrationSingleOutput(t *testing.T) {
	requireOCRIntegrationBinaries(t)

	repo := &spyProgressRepo{}
	jobID := uuid.New()
	conv := convert.OCRConverter{}
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inPath := filepath.Join("..", "convert", "testdata", "ocr", "text_sample.png")
	if err := runConvertWithOptionalProgress(ctx, conv, inPath, outPath, nil, repo, jobID, "", 0, 0, false, "", nil, nil, 0); err != nil {
		t.Fatalf("runConvertWithOptionalProgress: %v", err)
	}

	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("output directory contains %d files (%v), want exactly 1 (single-output invariant)", len(entries), entries)
	}
	if entries[0] != outPath {
		t.Fatalf("output file = %q, want %q", entries[0], outPath)
	}
}
