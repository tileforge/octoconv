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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

// largeDocFixtureFloor and largeDocFixtureCeiling bound the large-document
// fixture TestChromiumLargeDocumentNeedsNoDevShmLive builds (69-05-PLAN.md
// Task 1.b): large enough to be a genuine production-scale document (never
// shrunk to make this check easier -- project rule), and no larger than the
// html engine's own upload ceiling (internal/api/api.go:
// defaultMaxEngineBytes[EngineHTML] = 100 << 20).
const (
	largeDocFixtureFloor   = 64 << 20
	largeDocFixtureCeiling = 100 << 20
)

// noisePNG renders a deterministic w x h grayscale PNG of pseudo-random noise
// from seed (math/rand/v2, a fixed seed -- reproducible across runs). Every
// pixel is independent noise, so the image is effectively incompressible:
// PNG's deflate stage cannot shrink it, which is why the fixture's actual
// byte size tracks its raw pixel count closely (measured at plan time: a
// 3500x1000 image encodes to 3,503,410 bytes against 3,500,000 raw pixels).
func noisePNG(t *testing.T, w, h int, seed uint64) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	rnd := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: byte(rnd.IntN(256))})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("noisePNG: encode: %v", err)
	}
	return buf.Bytes()
}

// buildLargeDocFixture builds the html document TestChromiumLargeDocumentNeedsNoDevShmLive
// converts (69-05-PLAN.md Task 1.b): a doctype'd HTML document titled with
// marker, 18 page-break sections each holding a heading, a paragraph and one
// of three large incompressible PNGs (rotated across sections, embedded as
// base64 data URIs), and one further section holding a 5000-row table.
func buildLargeDocFixture(t *testing.T, marker string) []byte {
	t.Helper()
	// 3500x1000 grayscale noise, ~3.34 MiB raw / ~4.45 MiB base64 per image;
	// 18 sections cycling 3 such images plus the 5000-row table lands at
	// ~80 MiB, comfortably inside [largeDocFixtureFloor, largeDocFixtureCeiling].
	const imgW, imgH = 3500, 1000
	images := [3][]byte{}
	for i := range images {
		images[i] = noisePNG(t, imgW, imgH, uint64(i)+1)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html>\n<html><head><title>%s</title></head><body>\n", marker)
	const sections = 18
	for i := 0; i < sections; i++ {
		b64 := base64.StdEncoding.EncodeToString(images[i%len(images)])
		fmt.Fprintf(&b, "<section style=\"page-break-before: always\">\n"+
			"<h2>Section %d</h2>\n<p>%s section %d</p>\n"+
			"<img src=\"data:image/png;base64,%s\" alt=\"noise\">\n</section>\n",
			i+1, marker, i+1, b64)
	}

	fmt.Fprintf(&b, "<section style=\"page-break-before: always\">\n<h2>%s table</h2>\n<table>\n", marker)
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "<tr><td>%d</td><td>%s-row-%d</td><td>%d</td></tr>\n", i, marker, i, i*i)
	}
	b.WriteString("</table>\n</section>\n</body></html>\n")

	return []byte(b.String())
}

// devShmWatchDir is the directory the observer watches. It is devShmDir; a
// variable only so a deliberate inversion of the check can point it elsewhere.
var devShmWatchDir = devShmDir

// devShmDir is the directory the large-document check's observer watches, and
// the ONE extra writable entry its observation ruleset carries. It exists only
// in this test's ruleset; htmlReadWrite, the production grant, never names it
// (G-69-3: the deployment mounts and sizes no /dev/shm for chromium).
const devShmDir = "/dev/shm"

// devShmObservationExec builds the ruleset the large-document check observes
// /dev/shm under: exactly what ChromiumConverter.exec builds (forJobRW with
// htmlReadWrite, htmlReadOnly and htmlEngines, the engineEnv("") allowlist)
// with one extra writable entry, devShmDir, appended to the shared set. With
// /dev/shm REACHABLE a run that wants it gets it, so no denial-triggered
// fallback can hide the wish -- which is what a denied-and-failed control
// could not promise (70-06, evidence/chromium-devshm-measure-70-06.txt).
func devShmObservationExec(conv ChromiumConverter, runDir string) engineExec {
	rw := append(append([]string{}, htmlReadWrite...), devShmDir)
	return conv.Sandbox.forJobRW(runDir, rw, htmlReadOnly, htmlEngines).withEnv(engineEnv(""))
}

// logDevShmMount logs this container's actual /dev/shm mount line (or that
// none is mounted) from /proc/mounts -- 69-05-PLAN.md Task 1.a: the
// large-document check's transcript must record the size actually in effect,
// never assume the diagnosis session's stale "~64Mi Docker default".
func logDevShmMount(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		t.Logf("dev-shm: none mounted (/proc/mounts unreadable: %v)", err)
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "/dev/shm" {
			t.Logf("dev-shm: %s", line)
			return
		}
	}
	t.Logf("dev-shm: none mounted")
}

// liveEngineRun runs name through e exactly as engineExec.run would --
// wrapEngine with e's own sandbox and spec, e's own env -- but returns the
// raw exit status and the captured STDERR that run() discards on success
// (its own doc comment), and reports the sandbox helper's own setup failure
// (sandbox.SetupExit) distinctly from a genuine engine failure (69-05-
// PLAN.md Task 1.a). Mirrors runCommandCore's process-group hardening
// (internal/convert/exec.go: Setpgid, SIGKILL of the whole group on ctx.Done)
// WITHOUT touching exec.go itself (D-12: exec.go stays byte-identical across
// this phase) -- this is test-only code, and the production path
// (ChromiumConverter.Convert -> c.exec(workDir).run) is unchanged.
func liveEngineRun(ctx context.Context, e engineExec, name string, args ...string) (exitCode int, stderrOut []byte, setupFailed bool, err error) {
	cmdName, cmdArgs, err := e.sandbox.wrapEngine(name, e.spec, args)
	if err != nil {
		return -1, nil, false, err
	}
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if e.env != nil {
		cmd.Env = e.env
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if startErr := cmd.Start(); startErr != nil {
		return -1, nil, false, fmt.Errorf("start %s: %w", name, startErr)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return -1, stderrBuf.Bytes(), false, ctx.Err()
	case waitErr := <-done:
		if waitErr == nil {
			return 0, stderrBuf.Bytes(), false, nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code := exitErr.ExitCode()
			return code, stderrBuf.Bytes(), code == sandbox.SetupExit, nil
		}
		return -1, stderrBuf.Bytes(), false, waitErr
	}
}

// TestChromiumLargeDocumentNeedsNoDevShmLive is the large-document check
// 69-05-PLAN.md Task 1 demands, closing UAT gap G-69-3: on a production-scale
// HTML document, at the deployed limits (--cpus=2 --memory=2g, applied by
// scripts/sandbox-liveproof.sh), the confined production ChromiumConverter.Convert
// path needs no /dev/shm -- proven by a method that would see a real /dev/shm
// dependency if one existed (step e), not merely by reading the code. Never
// parallel (matches liveConfinedChromium's own D-03 concurrency-1 discipline);
// every file this test writes lives under one t.TempDir() dir, so
// ConfinedChromium's D-04 shared-/tmp sweep (a sibling test's cleanup, or a
// future run) never races this one's own fixtures.
//
// Step e was rebuilt in 70-06. Phase 69's control ran the engine WITHOUT
// --disable-dev-shm-usage and demanded that the run FAIL on /dev/shm while the
// ruleset denied it. That inferred a dependency from a failure, and a failure
// is only one thing a denial can cause: Chromium 153 failed on the denied
// /dev/shm, Chromium 154 does not (measured, chromium-devshm-measure-70-06.txt
// D3: flag off, production ruleset, exit 0, marker present, nothing created
// under /dev/shm; its shared memory comes from memfd_create), so the control
// stopped failing and the gate went red on a version bump -- not on a real
// dependency. What the observation needs is REACHABILITY, not denial: with
// /dev/shm granted nothing is denied, so no fallback can hide a wish for it,
// and an inotify watch in this (unconfined) process reports every entry the
// engine creates there.
//
// Control first: the observer must see an entry created under the same
// observation ruleset, or "the method cannot see a /dev/shm dependency".
// Measured on this Chromium the engine itself creates nothing under /dev/shm
// with or without the flag (D4, D6), so the control is a program that does --
// dash, granted by htmlReadOnly -- and the flag is NOT load-bearing on this
// version. Then the production argv, unchanged, under the same ruleset must
// render its marker with no entry seen.
func TestChromiumLargeDocumentNeedsNoDevShmLive(t *testing.T) {
	conv := liveConfinedChromium(t)

	logDevShmMount(t)

	marker, err := preflightMarker()
	if err != nil {
		t.Fatalf("preflightMarker: %v", err)
	}
	titleMarker := []byte("/Title (" + marker + ")")

	dir := t.TempDir()

	fixture := buildLargeDocFixture(t, marker)
	if len(fixture) < largeDocFixtureFloor || len(fixture) > largeDocFixtureCeiling {
		t.Fatalf("fixture is %d bytes, want between %d and %d -- adjust image dimensions, never the window",
			len(fixture), largeDocFixtureFloor, largeDocFixtureCeiling)
	}
	inPath := filepath.Join(dir, "in.html")
	if err := os.WriteFile(inPath, fixture, 0o600); err != nil {
		t.Fatalf("write in.html: %v", err)
	}

	// c. Production: the real Convert path, timed.
	outPath := filepath.Join(dir, "out.pdf")
	convCtx, convCancel := context.WithTimeout(context.Background(), htmlLiveEngineTimeout)
	start := time.Now()
	convErr := conv.Convert(convCtx, inPath, outPath, nil)
	elapsed := time.Since(start)
	convCancel()
	if convErr != nil {
		t.Fatalf("production Convert failed: %v", convErr)
	}
	if err := requireOutput(outPath, titleMarker); err != nil {
		t.Fatalf("production PDF missing %s: %v", titleMarker, err)
	}
	pdfInfo, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat out.pdf: %v", err)
	}
	if pdfInfo.Size() < 10<<20 {
		t.Fatalf("production PDF is %d bytes, want at least 10 MiB (a denied/blank chromium render is 652 bytes, SANDBOX-PATHS.md html section)",
			pdfInfo.Size())
	}

	// Convert wrote workDir/rendered.html, workDir == dir (filepath.Dir(outPath)).
	renderedPath := filepath.Join(dir, "rendered.html")
	if _, err := os.Stat(renderedPath); err != nil {
		t.Fatalf("Convert did not write rendered.html: %v", err)
	}

	// d. Stderr run: the helper runs the engine on file://dir/rendered.html
	// through conv.exec(runDir), runDir a subdirectory of dir, with
	// chromiumPrintArgs(runDir, ...) unchanged.
	runDir := filepath.Join(dir, "run-stderr")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir run-stderr: %v", err)
	}
	runOut := filepath.Join(runDir, "run.pdf")
	runArgs := chromiumPrintArgs(runDir, runOut, "file://"+renderedPath)
	runCtx, runCancel := context.WithTimeout(context.Background(), htmlLiveEngineTimeout)
	exitCode, stderrOut, setupFailed, runErr := liveEngineRun(runCtx, conv.exec(runDir), "chromium-headless-shell", runArgs...)
	runCancel()
	if runErr != nil {
		t.Fatalf("stderr run: could not start/wait the engine: %v", runErr)
	}
	if setupFailed {
		t.Fatalf("stderr run: the sandbox helper itself failed to set up (exit %d), not the engine -- stderr: %s",
			exitCode, stderrOut)
	}
	if exitCode != 0 {
		t.Fatalf("stderr run: engine exited %d, want 0 -- stderr: %s", exitCode, stderrOut)
	}
	if bytes.Contains(stderrOut, []byte("/dev/shm")) {
		t.Fatalf("stderr run: stderr mentions /dev/shm despite --disable-dev-shm-usage: %s", stderrOut)
	}
	if err := requireOutput(runOut, titleMarker); err != nil {
		t.Fatalf("stderr run: output missing %s: %v", titleMarker, err)
	}

	// e. Observation with /dev/shm reachable (see the test's doc comment). One
	// run directory, one ruleset (devShmObservationExec), two runs.
	obsDir := filepath.Join(dir, "run-observe")
	if err := os.MkdirAll(obsDir, 0o700); err != nil {
		t.Fatalf("mkdir run-observe: %v", err)
	}
	obsExec := devShmObservationExec(conv, obsDir)

	// e1. Control: a program that creates an entry under /dev/shm, run through
	// the same helper and ruleset. The observer must see it.
	controlName := "devshm-control-" + marker
	controlPath := filepath.Join(devShmDir, controlName)
	t.Cleanup(func() { _ = os.Remove(controlPath) })
	drainControl := startDevShmObserver(t, devShmWatchDir)
	ctlCtx, ctlCancel := context.WithTimeout(context.Background(), htmlLiveEngineTimeout)
	ctlExit, ctlStderr, ctlSetupFailed, ctlErr := liveEngineRun(ctlCtx, obsExec, "dash", "-c", ": > "+controlPath)
	ctlCancel()
	controlSeen := drainControl()
	_ = os.Remove(controlPath)
	if ctlErr != nil {
		t.Fatalf("control: could not start/wait dash: %v", ctlErr)
	}
	if ctlSetupFailed || ctlExit != 0 {
		t.Fatalf("control: dash exited %d (setup failure: %v) under the observation ruleset -- the method cannot see a /dev/shm dependency: %s",
			ctlExit, ctlSetupFailed, ctlStderr)
	}
	if len(controlSeen) == 0 {
		t.Fatalf("control: created %s under %s and the observer saw nothing -- the method cannot see a /dev/shm dependency",
			controlPath, devShmDir)
	}

	// e2. Production argv, unchanged, under the same ruleset: it must render
	// the marker and create nothing under /dev/shm.
	obsOut := filepath.Join(obsDir, "obs.pdf")
	obsArgs := chromiumPrintArgs(obsDir, obsOut, "file://"+renderedPath)
	drainProd := startDevShmObserver(t, devShmWatchDir)
	obsCtx, obsCancel := context.WithTimeout(context.Background(), htmlLiveEngineTimeout)
	obsExit, obsStderr, obsSetupFailed, obsErr := liveEngineRun(obsCtx, obsExec, "chromium-headless-shell", obsArgs...)
	obsCancel()
	prodSeen := drainProd()
	if obsErr != nil {
		t.Fatalf("production observation: could not start/wait the engine: %v", obsErr)
	}
	if obsSetupFailed {
		t.Fatalf("production observation: the sandbox helper itself failed to set up (exit %d), not the engine -- stderr: %s",
			obsExit, obsStderr)
	}
	if obsExit != 0 {
		t.Fatalf("production observation: engine exited %d, want 0 -- stderr: %s", obsExit, obsStderr)
	}
	if err := requireOutput(obsOut, titleMarker); err != nil {
		t.Fatalf("production observation: output missing %s: %v", titleMarker, err)
	}
	if len(prodSeen) != 0 {
		t.Fatalf("production observation: the production argv created %d entr(ies) under %s with it reachable: %q -- "+
			"the engine does depend on /dev/shm (G-69-3's no-mount decision is reopened)",
			len(prodSeen), devShmDir, prodSeen)
	}

	// The engine's version, run directly (unconfined), so a later red gate can
	// be read against the Chromium it ran on -- the image installs it unpinned
	// on purpose (Dockerfile.chromium-worker).
	version, verErr := exec.Command("chromium-headless-shell", "--version").Output()
	if verErr != nil {
		t.Fatalf("chromium-headless-shell --version: %v", verErr)
	}
	t.Logf("chromium: %s", strings.TrimSpace(string(version)))
	t.Logf("dev-shm-observe: control=%d production=%d", len(controlSeen), len(prodSeen))
	t.Logf("large-doc: fixture=%d bytes pdf=%d bytes wall=%s", len(fixture), pdfInfo.Size(), elapsed)
}
