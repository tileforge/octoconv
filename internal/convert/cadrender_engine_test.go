//go:build integration

package convert

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Run against a locally built Dockerfile.cad-worker engine (or worker) image:
// OCTOCONV_CAD_RENDER_IMAGE=octoconv-cad-worker:engine go test -tags=integration ./internal/convert -run '^TestCADRenderEnginePreflightFrame$' -count=1
// Mount the checkout's helper so this checks renderer changes without rebuilding
// the image. A mock renderer cannot catch the FXAA/line-width startup regression.
func TestCADRenderEnginePreflightFrame(t *testing.T) {
	image := os.Getenv("OCTOCONV_CAD_RENDER_IMAGE")
	if image == "" {
		t.Skip("OCTOCONV_CAD_RENDER_IMAGE unset; requires the real CAD engine image and Docker")
	}
	helper, err := filepath.Abs("../../cmd/cad-worker/helper/cad-render")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "in.step"), cadPreflightSTEP, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none",
		"--cpus", "2", "--memory", "4g", "--user", "nobody",
		"-e", "HOME=/job", "-e", "LP_NUM_THREADS=2",
		"-v", dir+":/job", "-v", helper+":/usr/local/bin/cad-render:ro",
		"--entrypoint", "/bin/sh", image, "-ec", `
cad-mesh --format step /job/in.step /job/part.stl
for size in 256 1280 2048; do
    for background in opaque transparent; do
        cad-render --view iso --size "$size" --background "$background" \
            /job/part.stl "/job/$size-$background.png"
    done
done
`)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("real CAD rendering failed: %v\n%s", err, output)
	}
	for _, size := range []int{256, 1280, 2048} {
		for _, background := range []string{CADBackgroundOpq, CADBackgroundTr} {
			t.Run(fmt.Sprintf("%d-%s", size, background), func(t *testing.T) {
				f, err := os.Open(filepath.Join(dir, fmt.Sprintf("%d-%s.png", size, background)))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				facts, err := JudgeCADPNG(f, size, background)
				if err != nil {
					t.Fatalf("the real renderer's startup fixture fails the production judge: %v; facts: %+v", err, facts)
				}
				t.Logf("dark pixels: %d, dark fraction: %.6f", facts.DarkCount, facts.Dark)
			})
		}
	}
}
