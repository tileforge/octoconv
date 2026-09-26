package convert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The measured writable set, pinned. Every entry here is proven by
// .planning/research/SANDBOX-PATHS.md's "html" section and this phase's
// CONF-06 decision: chromium puts its shared memory in /tmp regardless of
// TMPDIR (measured, does not move), and needs /dev/null.
func TestHTMLReadWriteSetIsTheMeasuredOne(t *testing.T) {
	want := []string{"/tmp", "/dev/null"}
	if len(htmlReadWrite) != len(want) {
		t.Fatalf("htmlReadWrite has %d paths, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation", len(htmlReadWrite), len(want))
	}
	for i := range want {
		if htmlReadWrite[i] != want[i] {
			t.Errorf("htmlReadWrite[%d] = %q, want %q", i, htmlReadWrite[i], want[i])
		}
	}
}

// The measured read-only set, pinned, in order.
func TestHTMLReadOnlySetIsTheMeasuredOne(t *testing.T) {
	want := []string{
		"/usr/bin/dash",
		"/lib",
		"/usr/lib",
		"/usr/share",
		"/usr/local/share",
		"/etc/fonts",
		"/etc/ld.so.cache",
		"/etc/localtime",
		"/etc/nsswitch.conf",
		"/etc/host.conf",
		"/etc/hosts",
		"/etc/resolv.conf",
		"/proc",
		"/sys/devices",
		"/dev/urandom",
	}
	if len(htmlReadOnly) != len(want) {
		t.Fatalf("htmlReadOnly has %d paths, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation", len(htmlReadOnly), len(want))
	}
	for i := range want {
		if htmlReadOnly[i] != want[i] {
			t.Errorf("htmlReadOnly[%d] = %q, want %q", i, htmlReadOnly[i], want[i])
		}
	}
}

func TestHTMLEnginesAreTheMeasuredOnes(t *testing.T) {
	want := []string{"chromium-headless-shell"}
	if len(htmlEngines) != len(want) {
		t.Fatalf("htmlEngines has %d entries, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation", len(htmlEngines), len(want))
	}
	for i := range want {
		if htmlEngines[i] != want[i] {
			t.Errorf("htmlEngines[%d] = %q, want %q", i, htmlEngines[i], want[i])
		}
	}
}

// D-08: the wrapper is granted through htmlEngines (a PATH lookup), but
// forJob never resolves its INTERPRETER or the real binary by name -- both
// must be literal/covered entries in htmlReadOnly, or the preflight fails
// with "exec: permission denied" on the wrapper's own interpreter.
func TestHTMLGrantsTheWrapperItsInterpreterAndTheRealBinary(t *testing.T) {
	if len(htmlEngines) != 1 || htmlEngines[0] != "chromium-headless-shell" {
		t.Fatalf("htmlEngines = %v, want exactly [chromium-headless-shell] (the wrapper, resolved via PATH)", htmlEngines)
	}
	found := false
	for _, ro := range htmlReadOnly {
		if ro == "/usr/bin/dash" {
			found = true
			break
		}
	}
	if !found {
		t.Error("htmlReadOnly does not contain /usr/bin/dash, the wrapper's interpreter (D-08)")
	}
	realBinary := "/usr/lib/chromium/chromium-headless-shell"
	covered := false
	for _, ro := range htmlReadOnly {
		if ro == realBinary || strings.HasPrefix(realBinary, ro+"/") {
			covered = true
			break
		}
	}
	if !covered {
		t.Errorf("no htmlReadOnly entry covers the real binary %s (D-08)", realBinary)
	}
}

// ChromiumConverter stays comparable: WithSandbox(nil) equals the zero
// value, a confined converter does not, and the type is usable as a map
// key -- fails to COMPILE if Sandbox stops being a pointer.
func TestChromiumConverterStaysComparable(t *testing.T) {
	var a, b ChromiumConverter
	if a != b {
		t.Fatal("two zero converters differ")
	}
	if got := (ChromiumConverter{}).WithSandbox(nil); got != a {
		t.Error("WithSandbox(nil) does not equal the zero value")
	}

	confined := ChromiumConverter{}.WithSandbox([]string{"/usr/local/bin/chromium-worker", "__sandbox-exec"})
	if confined == a {
		t.Error("a confined converter equals an unconfined one, so Sandbox is not being compared at all")
	}
	seen := map[ChromiumConverter]bool{a: true}
	if !seen[b] {
		t.Error("the zero converter is not its own map key")
	}
}

// ConfinedChromium enforces D-03 before returning anything usable, and
// wires D-04's sweep only when actually confined AND the writable set
// grants the shared /tmp. It never calls Convert.
func TestConfinedChromium(t *testing.T) {
	helper, _ := fakeSandboxHelper(t)

	if _, err := ConfinedChromium(helper, 2, "HTML_WORKER_CONCURRENCY"); err == nil ||
		!strings.Contains(err.Error(), "HTML_WORKER_CONCURRENCY=2") {
		t.Errorf("ConfinedChromium(helper, 2, ...) err = %v, want it to name HTML_WORKER_CONCURRENCY=2", err)
	}

	c, err := ConfinedChromium(helper, 1, "HTML_WORKER_CONCURRENCY")
	if err != nil {
		t.Fatalf("ConfinedChromium(helper, 1, ...): %v", err)
	}
	if c.sharedTmp != sharedTmpDir {
		t.Errorf("confined at concurrency 1: sharedTmp = %q, want %q", c.sharedTmp, sharedTmpDir)
	}

	c2, err := ConfinedChromium(nil, 2, "HTML_WORKER_CONCURRENCY")
	if err != nil {
		t.Fatalf("ConfinedChromium(nil, 2, ...): %v", err)
	}
	if c2.sharedTmp != "" {
		t.Errorf("unconfined: sharedTmp = %q, want empty (nothing to sweep)", c2.sharedTmp)
	}

	c3, err := ConfinedChromium(nil, 1, "HTML_WORKER_CONCURRENCY")
	if err != nil {
		t.Fatalf("ConfinedChromium(nil, 1, ...): %v", err)
	}
	if c3.sharedTmp != "" {
		t.Errorf("unconfined: sharedTmp = %q, want empty", c3.sharedTmp)
	}
}

// HTMLPreflight on a zero (unconfined) converter returns nil without
// creating a preflight workspace at all.
func TestHTMLPreflightUnconfinedProvesNothing(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "html-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	c := ChromiumConverter{}
	if err := HTMLPreflight(context.Background(), c); err != nil {
		t.Errorf("HTMLPreflight on an unconfined converter: %v, want nil", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("HTMLPreflight created a workspace (%v -> %v) though the converter is unconfined", before, after)
	}
}

// A confined converter built WITHOUT ConfinedChromium (e.g. bare
// WithSandbox) has not wired the D-04 sweep -- HTMLPreflight must refuse
// before creating any workspace, since a preflight run without the sweep
// would itself race whatever a prior task left in the shared /tmp.
func TestHTMLPreflightRefusesAConfinedConverterThatDoesNotSweep(t *testing.T) {
	helper, _ := fakeSandboxHelper(t)
	pattern := filepath.Join(os.TempDir(), "html-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	c := ChromiumConverter{}.WithSandbox(helper)
	err := HTMLPreflight(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "sweep") {
		t.Errorf("HTMLPreflight err = %v, want it to mention the sweep", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("HTMLPreflight created a workspace before refusing (%v -> %v)", before, after)
	}
}

// sweepSharedTmp removes every entry of dir except the one holding keep: a
// plain file, a dotfile, and a directory with contents are gone; a symlink
// to a file outside dir is removed as a symlink (its target survives); and
// keep itself (with its contents) survives untouched.
func TestSweepSharedTmp(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "job-keep")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatalf("mkdir keep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keep, "input.html"), []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("write keep input: %v", err)
	}

	plainFile := filepath.Join(dir, "leftover.txt")
	if err := os.WriteFile(plainFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write plain file: %v", err)
	}
	dotFile := filepath.Join(dir, ".hidden")
	if err := os.WriteFile(dotFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write dotfile: %v", err)
	}
	subdir := filepath.Join(dir, "subdir-with-contents")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write subdir file: %v", err)
	}

	outsideTarget := filepath.Join(t.TempDir(), "outside-target")
	if err := os.WriteFile(outsideTarget, []byte("target-data"), 0o600); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	symlinkPath := filepath.Join(dir, "link-to-outside")
	if err := os.Symlink(outsideTarget, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := sweepSharedTmp(dir, keep); err != nil {
		t.Fatalf("sweepSharedTmp: %v", err)
	}

	if _, err := os.Stat(plainFile); !os.IsNotExist(err) {
		t.Errorf("plain leftover file survived the sweep: %v", err)
	}
	if _, err := os.Stat(dotFile); !os.IsNotExist(err) {
		t.Errorf("dotfile survived the sweep: %v", err)
	}
	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Errorf("subdir with contents survived the sweep: %v", err)
	}
	if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
		t.Errorf("symlink itself survived the sweep: %v", err)
	}
	if _, err := os.Stat(outsideTarget); err != nil {
		t.Errorf("the symlink's OUTSIDE target was removed (must survive): %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(keep, "input.html")); err != nil || string(data) != "keep-me" {
		t.Errorf("kept entry's contents did not survive: data=%q err=%v", data, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("running as root: an unreadable directory would still be readable, skipping the failure case")
	}
	unreadable := t.TempDir()
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(unreadable, 0o700)
	err := sweepSharedTmp(unreadable, filepath.Join(unreadable, "keep"))
	if err == nil || !errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Errorf("sweepSharedTmp on an unreadable directory = %v, want errors.Is ErrEngineSandboxUnavailable", err)
	}
}

// fakeChromiumScript is a fake chromium-headless-shell that, when it sees
// --print-to-pdf=<path> in its own argv, writes a minimal valid PDF to
// <path> AND dumps its own environment to <path>.env -- letting a test
// assert on both validatePDF's requirement and what the engine's
// environment actually contained.
const fakeChromiumScript = `for a in "$@"; do
  case "$a" in
    --print-to-pdf=*)
      out="${a#--print-to-pdf=}"
      printf '%%PDF-1.4\n%%fake\n' > "$out"
      env > "$out.env"
      ;;
  esac
done
`

// ChromiumConverter.Convert, confined through the fake sandbox helper,
// records rw:<job dir>, rw:/tmp, rw:/dev/null, and ro:/usr/bin/dash -- and
// runs the resolved fake engine after "--".
func TestChromiumConvertRunsTheEngineThroughTheHelper(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeEngine(t, dir, "chromium-headless-shell", fakeChromiumScript)
	pathWith(t, dir)

	jobDir := t.TempDir()
	inPath := filepath.Join(jobDir, "in.html")
	if err := os.WriteFile(inPath, []byte("<html><head><title>x</title></head><body>hi</body></html>"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	outPath := filepath.Join(jobDir, "out.pdf")

	c := ChromiumConverter{}.WithSandbox(helper)
	if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	invocations := calls()
	if len(invocations) != 1 {
		t.Fatalf("helper invoked %d time(s), want 1: %v", len(invocations), invocations)
	}
	argv := invocations[0]
	sep := indexOf(argv, "--")
	if sep < 0 {
		t.Fatalf("no -- separator in %v", argv)
	}
	var rw, ro []string
	for _, tok := range argv[1:sep] {
		switch {
		case strings.HasPrefix(tok, "rw:"):
			rw = append(rw, strings.TrimPrefix(tok, "rw:"))
		case strings.HasPrefix(tok, "ro:"):
			ro = append(ro, strings.TrimPrefix(tok, "ro:"))
		}
	}
	wantRW := []string{jobDir, "/tmp", "/dev/null"}
	if len(rw) != len(wantRW) {
		t.Fatalf("rw tokens = %v, want %v", rw, wantRW)
	}
	for i := range wantRW {
		if rw[i] != wantRW[i] {
			t.Errorf("rw[%d] = %q, want %q", i, rw[i], wantRW[i])
		}
	}
	if indexOf(ro, "/usr/bin/dash") < 0 {
		t.Errorf("ro tokens %v missing /usr/bin/dash", ro)
	}
	if argv[sep+1] != filepath.Join(dir, "chromium-headless-shell") {
		t.Errorf("command after -- is %q, want the resolved fake engine path", argv[sep+1])
	}
}

// D-04: Convert sweeps the shared /tmp before the engine ever runs, keeping
// only the current job's own directory (which itself lives inside the swept
// directory, exactly as a real job directory lives under /tmp).
func TestChromiumConvertSweepsTheSharedTmpBeforeTheEngine(t *testing.T) {
	helper, _ := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeEngine(t, dir, "chromium-headless-shell", fakeChromiumScript)
	pathWith(t, dir)

	sharedTmp := t.TempDir()
	jobDir := filepath.Join(sharedTmp, "job-under-test")
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatalf("mkdir jobDir: %v", err)
	}
	inPath := filepath.Join(jobDir, "in.html")
	if err := os.WriteFile(inPath, []byte("<html><head><title>x</title></head><body>hi</body></html>"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	leftover := filepath.Join(sharedTmp, "leftover-from-a-killed-task")
	if err := os.WriteFile(leftover, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write leftover: %v", err)
	}
	outPath := filepath.Join(jobDir, "out.pdf")

	c := ChromiumConverter{}.WithSandbox(helper)
	c.sharedTmp = sharedTmp
	if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("leftover file from a prior task survived the sweep: %v", err)
	}
	if _, err := os.Stat(inPath); err != nil {
		t.Errorf("the job directory's own input did not survive the sweep: %v", err)
	}
}

// The engine's environment carries no secrets -- html's grant includes
// /proc (and therefore /proc/self/environ), so the environment itself must
// be scrubbed, not merely the filesystem.
func TestChromiumEngineEnvCarriesNoSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "secret-canary")
	helper, _ := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeEngine(t, dir, "chromium-headless-shell", fakeChromiumScript)
	pathWith(t, dir)

	jobDir := t.TempDir()
	inPath := filepath.Join(jobDir, "in.html")
	if err := os.WriteFile(inPath, []byte("<html><head><title>x</title></head><body>hi</body></html>"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	outPath := filepath.Join(jobDir, "out.pdf")

	c := ChromiumConverter{}.WithSandbox(helper)
	if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	dumped, err := os.ReadFile(outPath + ".env")
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	if strings.Contains(string(dumped), "DATABASE_URL") {
		t.Errorf("the engine's environment carried DATABASE_URL: %s", dumped)
	}
	if strings.Contains(string(dumped), "TMPDIR") {
		t.Errorf("the engine's environment carried TMPDIR (D-01: chromium sees the measured default /tmp, unset): %s", dumped)
	}
}
