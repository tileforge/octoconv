package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
)

// requirePandocIntegrationBinary skips the calling test unless pandoc is on
// PATH -- mirrors internal/convert/pandoc_test.go's requirePandoc and
// requireOCRIntegrationBinaries' precedent in this package, so `go test
// -short` (and any CI without pandoc installed) stays hermetic.
func requirePandocIntegrationBinary(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not found on PATH, skipping real-binary pandoc integration test")
	}
}

// TestPandocConvertIntegration proves a real md->html pandoc job runs
// end-to-end through the EXACT worker seam process() uses,
// runConvertWithOptionalProgress -- not a bare call to PandocConverter.Convert
// (mirrors TestOCRProgressIntegration's own documented precedent,
// ocr_progress_integration_test.go). This does NOT exercise HandlePandocConvert
// itself: process() still shells out to a real converter, downloads/uploads
// through h.store (a concrete *storage.Client), and drives the real
// engine binary -- none of that is faked by the convertRepo/webhookEnqueuer/
// processFn seams introduced in 56-01 (D-10 step 1), which only lift the
// restriction on constructing a *Handler without a live Postgres/Redis.
// This integration test remains the proxy for the process()-internal
// conversion seam specifically -- a real pandoc binary invocation end to
// end -- while internal/worker/pinning_test.go now exercises
// HandlePandocConvert (and its six siblings) directly via a Handler built
// from fakes, proving the smallest testable proxy for
// queued->active->done: a real pandoc invocation, through the exact
// conversion seam the handler calls, succeeds and produces valid output.
// PandocConverter does NOT implement convert.ProgressConverter (a single
// subprocess, no page-granular signal, per pandoc.go), so
// runConvertWithOptionalProgress falls back to the plain conv.Convert path
// here -- proven by the repo receiving zero UpdateProgress calls below.
func TestPandocConvertIntegration(t *testing.T) {
	requirePandocIntegrationBinary(t)

	repo := &spyProgressRepo{}
	jobID := uuid.New()
	conv := convert.PandocConverter{}
	dir := t.TempDir()

	inPath := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(inPath, []byte("# Title\n\nSome *emphasis* and a [link](https://example.com).\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.html")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := runConvertWithOptionalProgress(ctx, conv, inPath, outPath, nil, repo, jobID, "", 2*time.Second, 1, false, "", nil, nil, 0); err != nil {
		t.Fatalf("runConvertWithOptionalProgress(md->html): %v, want nil (queued->active->done proxy)", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("Convert(md->html) produced empty output, want non-empty for a non-empty source")
	}

	// PandocConverter does NOT implement convert.ProgressConverter, so
	// runConvertWithOptionalProgress's type-assert fails and it takes the
	// early-return plain conv.Convert(...) path (D-01) -- that path never
	// touches repo at all (not even the terminal-100 bypass, which only
	// fires after a successful ConvertWithProgress call). Zero
	// UpdateProgress calls is therefore the correct, proven contract for
	// pandoc jobs: no fabricated progress signal.
	if len(repo.updates) != 0 {
		t.Fatalf("UpdateProgress calls = %+v, want zero (pandoc is not a ProgressConverter; plain conv.Convert path takes no progress seam)", repo.updates)
	}
}

// TestPandocConvertIntegrationTerminalOnError proves a genuine pandoc engine
// failure (an unsupported source extension that reaches the subprocess
// itself, forcing pandoc's own reader-detection to fail rather than this
// project's own pandocPairSupported fast-reject) surfaces an error that
// isPandocTerminal classifies terminal -- the "pandoc: pandoc:" prefix
// pandoc.go's runCommand-failure branch emits.
func TestPandocConvertIntegrationTerminalOnError(t *testing.T) {
	requirePandocIntegrationBinary(t)

	repo := &spyProgressRepo{}
	jobID := uuid.New()
	conv := convert.PandocConverter{}
	dir := t.TempDir()

	// A corrupt/binary-garbage "markdown" input: pandoc's markdown reader is
	// lenient (it will happily treat raw bytes as a paragraph), so instead
	// force a genuine engine-level failure via a docx target reading from a
	// source pandoc's own container reader rejects outright.
	inPath := filepath.Join(dir, "doc.docx")
	if err := os.WriteFile(inPath, []byte("not a real docx zip container"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.md")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := runConvertWithOptionalProgress(ctx, conv, inPath, outPath, nil, repo, jobID, "", 0, 0, false, "", nil, nil, 0)
	if err == nil {
		t.Fatal("runConvertWithOptionalProgress(corrupt docx->md) = nil, want a genuine pandoc engine failure")
	}
	if !isPandocTerminal(err) {
		t.Fatalf("isPandocTerminal(%v) = false, want true (a corrupt/unreadable input is a deterministic pandoc engine failure, D-01)", err)
	}
}
