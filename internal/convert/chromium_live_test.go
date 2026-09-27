// Package convert (this file): chromium_live_test.go proves G-69-2's fix on
// the real chromium-headless-shell binary, through the production
// ChromiumConverter.Convert path, confined by ConfinedChromium exactly the
// way cmd/chromium-worker builds it (69-04-PLAN.md, .planning/debug/
// csp-bypass-before-head.md). It runs ONLY inside the built
// chromium-worker image, cross-compiled and driven by
// scripts/sandbox-liveproof.sh html -- OCTOCONV_CHROMIUM_LIVE=1 is the
// switch. Outside that image the engine this test needs is not on PATH, so
// an ordinary developer `go test ./...` (and ci-local.sh) skips this file's
// one test entirely; the switch never fails silently the other way --
// OCTOCONV_CHROMIUM_LIVE=1 with a missing engine or helper is a t.Fatal, not
// a skip, because the whole point of setting it is proving the real thing.
package convert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/apaderin/octoconv/internal/sandbox"
)

// htmlLiveEngineTimeout bounds each chromium-headless-shell invocation this
// test makes (69-04-PLAN.md Task 1 <behavior>: "60 s context").
const htmlLiveEngineTimeout = 60 * time.Second

// liveConfinedChromium builds the production ConfinedChromium converter --
// the same constructor cmd/chromium-worker uses, so the D-03 concurrency
// guard and D-04 shared-/tmp sweep in effect here are the production ones,
// not a test-only stand-in.
func liveConfinedChromium(t *testing.T) ChromiumConverter {
	t.Helper()
	if os.Getenv("OCTOCONV_CHROMIUM_LIVE") != "1" {
		t.Skip("OCTOCONV_CHROMIUM_LIVE != 1 -- this test only runs inside the built chromium-worker image via scripts/sandbox-liveproof.sh html")
	}
	if _, err := exec.LookPath("chromium-headless-shell"); err != nil {
		t.Fatalf("OCTOCONV_CHROMIUM_LIVE=1 but chromium-headless-shell is not on PATH: %v", err)
	}
	const helperBin = "/usr/local/bin/chromium-worker"
	if _, err := os.Stat(helperBin); err != nil {
		t.Fatalf("OCTOCONV_CHROMIUM_LIVE=1 but %s is missing: %v", helperBin, err)
	}
	c, err := ConfinedChromium([]string{helperBin, sandbox.ExecMode}, 1, "HTML_WORKER_CONCURRENCY")
	if err != nil {
		t.Fatalf("ConfinedChromium: %v", err)
	}
	if !c.Sandbox.confines() {
		t.Fatal("OCTOCONV_CHROMIUM_LIVE=1 but the built converter does not confine -- this test proves nothing without confinement")
	}
	return c
}

// TestChromiumCSPPrecedesEveryScriptLive is the live differential this
// plan's shape table demands (one subtest per cspShapeTable row, never
// parallel -- ConfinedChromium's D-03 guard holds concurrency to 1 for the
// same reason a real deployment does): for every row, a CONTROL render of
// the raw, uninjected bytes must show the script's effect, and a PRODUCTION
// render through ChromiumConverter.Convert must not -- proving the fix on
// the real engine rather than by reading the code (project rule: don't
// predict a parser, test against it). Every judgment is by PDF content
// (requireOutput/requireAbsent), never by chromium's exit status --
// measured to exit 0 and write a blank PDF when denied (D-06/D-07).
func TestChromiumCSPPrecedesEveryScriptLive(t *testing.T) {
	conv := liveConfinedChromium(t)

	marker, err := preflightMarker()
	if err != nil {
		t.Fatalf("preflightMarker: %v", err)
	}
	rows := cspShapeTable(marker)
	bypassMarker := []byte("BYPASS-" + marker)
	safeMarker := []byte("/Title (SAFE-" + marker + ")")

	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			// One dir per row, directly under the shared /tmp: ConfinedChromium's
			// sweep (D-04) keeps only the top-level /tmp entry holding the
			// current job directory, so everything this row still needs --
			// in.html, control.html, the control's own chrome profile -- must
			// live under this one dir.
			dir := t.TempDir()

			inPath := filepath.Join(dir, "in.html")
			if err := os.WriteFile(inPath, row.raw, 0o600); err != nil {
				t.Fatalf("%s: write in.html: %v", row.name, err)
			}
			controlPath := filepath.Join(dir, "control.html")
			if err := os.WriteFile(controlPath, row.raw, 0o600); err != nil {
				t.Fatalf("%s: write control.html: %v", row.name, err)
			}

			// CONTROL: the engine run directly on the raw, uninjected bytes,
			// through the same argv builder and the same ruleset production
			// uses -- controlDir is a subdirectory of dir so the control gets
			// its own chrome profile, distinct from Convert's.
			controlDir := filepath.Join(dir, "control")
			if err := os.MkdirAll(controlDir, 0o700); err != nil {
				t.Fatalf("%s: mkdir control dir: %v", row.name, err)
			}
			controlOut := filepath.Join(controlDir, "control.pdf")
			ctrlCtx, ctrlCancel := context.WithTimeout(context.Background(), htmlLiveEngineTimeout)
			_, controlErr := conv.exec(controlDir).run(ctrlCtx, "chromium-headless-shell",
				chromiumPrintArgs(controlDir, controlOut, "file://"+controlPath)...)
			ctrlCancel()
			_ = controlErr // judged by PDF content only, never by exit status
			if err := requireOutput(controlOut, bypassMarker); err != nil {
				t.Fatalf("%s: control did not show the script's effect (oracle-blind control): %v", row.name, err)
			}

			// PRODUCTION: the real Convert path.
			outPath := filepath.Join(dir, "out.pdf")
			prodCtx, prodCancel := context.WithTimeout(context.Background(), htmlLiveEngineTimeout)
			convErr := conv.Convert(prodCtx, inPath, outPath, nil)
			prodCancel()
			if convErr != nil {
				t.Fatalf("%s: production Convert failed: %v", row.name, convErr)
			}
			if err := requireOutput(outPath, safeMarker); err != nil {
				t.Fatalf("%s: production render missing %s: %v", row.name, safeMarker, err)
			}
			if err := requireAbsent(outPath, bypassMarker); err != nil {
				t.Fatalf("%s: the script executed despite the CSP: %v", row.name, err)
			}
		})
	}

	t.Logf("csp-live: %d rows: every control executed its script, every production render blocked it", len(rows))
}
