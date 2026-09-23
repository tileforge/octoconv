package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSandboxHelper writes an executable bash script that plays the role of
// the sandbox helper (`<worker> __sandbox-exec ...`) without ever applying
// Landlock: it records its own argv, then execs whatever follows the first
// "--" -- exactly what the real helper does after RunExecMode's Apply call,
// minus the syscalls, so a test can assert on the spec engineExec built
// without a kernel.
//
// calls() returns every invocation recorded so far, parsed back from the
// argv log, oldest first; nil if the helper was never invoked.
func fakeSandboxHelper(t *testing.T) (helper []string, calls func() [][]string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-sandbox-helper.sh")
	callsPath := script + ".calls"
	const body = `#!/bin/bash
script="$0"
for a in "$@"; do printf '%s\n' "$a" >> "${script}.calls"; done
printf -- '--end--\n' >> "${script}.calls"

args=("$@")
sep=-1
for i in "${!args[@]}"; do
  if [ "${args[$i]}" = "--" ]; then sep=$i; break; fi
done
if [ "$sep" -lt 0 ]; then
  echo "fake-sandbox-helper: no -- separator in argv" >&2
  exit 1
fi
rest=("${args[@]:$((sep+1))}")
exec "${rest[@]}"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("fakeSandboxHelper: %v", err)
	}

	calls = func() [][]string {
		data, err := os.ReadFile(callsPath)
		if err != nil {
			return nil
		}
		var invocations [][]string
		var cur []string
		for _, line := range strings.Split(string(data), "\n") {
			switch line {
			case "--end--":
				invocations = append(invocations, cur)
				cur = nil
			case "":
				// A blank line is the trailing newline of the file, not an
				// argument -- an argv token is never recorded as "".
			default:
				cur = append(cur, line)
			}
		}
		return invocations
	}
	return []string{script, "__sandbox-exec"}, calls
}

// failingSandboxHelper writes a helper that exits with code WITHOUT exec'ing
// anything -- the shape sandbox.RunExecMode's own Fatal takes when it cannot
// establish the ruleset (sandbox.SetupExit == 78), and any other exit is the
// control that proves the sentinel is keyed on the STATUS, not merely on
// "the helper exited before the engine ran".
func failingSandboxHelper(t *testing.T, code int) []string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "failing-sandbox-helper.sh")
	body := fmt.Sprintf("#!/bin/bash\nexit %d\n", code)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("failingSandboxHelper: %v", err)
	}
	return []string{script, "__sandbox-exec"}
}

// fakeEngine writes an executable sh script named name in dir and returns its
// path -- a stand-in for a real engine binary (vips, pandoc, tesseract...)
// whose output/exit behaviour a test controls directly.
func fakeEngine(t *testing.T, dir, name, script string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	body := "#!/bin/sh\n" + script
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("fakeEngine: %v", err)
	}
	return p
}

// pathWith prepends dir to PATH for the duration of the test, so
// exec.LookPath (forJob's own resolution, and Go's exec.Command for a bare
// name) finds a fake engine placed there.
func pathWith(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A confined engineExec.run shows the helper's own mode flag, exactly one
// rw: token equal to the job directory, a ro: token for every static path
// AND for the resolved engine, the "--" separator, the resolved engine path,
// then the engine's own arguments -- and the fake engine's stdout comes back
// as the call's output, proving run actually executed what wrapEngine built.
func TestEngineExecRunsThroughTheHelperWithTheExpectedSpec(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeEngine(t, dir, "fakeengine", "echo hello-from-fake-engine\n")
	pathWith(t, dir)
	enginePath, err := exec.LookPath("fakeengine")
	if err != nil {
		t.Fatalf("exec.LookPath(fakeengine): %v", err)
	}

	s := &engineSandbox{Helper: helper, Confine: true}
	jobDir := t.TempDir()
	x := s.forJob(jobDir, []string{"/lib", "/usr/lib"}, []string{"fakeengine"})

	out, err := x.run(context.Background(), "fakeengine", "arg1", "arg2")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hello-from-fake-engine" {
		t.Errorf("stdout = %q, want the fake engine's own output", out)
	}

	invocations := calls()
	if len(invocations) != 1 {
		t.Fatalf("helper invoked %d time(s), want 1: %v", len(invocations), invocations)
	}
	argv := invocations[0]
	if len(argv) == 0 || argv[0] != "__sandbox-exec" {
		t.Fatalf("argv[0] = %v, want __sandbox-exec first", argv)
	}
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
	if len(rw) != 1 || rw[0] != jobDir {
		t.Errorf("rw tokens = %v, want exactly [%s]", rw, jobDir)
	}
	for _, want := range []string{"/lib", "/usr/lib", enginePath} {
		if indexOf(ro, want) < 0 {
			t.Errorf("ro tokens %v missing %q", ro, want)
		}
	}
	if argv[sep+1] != enginePath {
		t.Errorf("command after -- is %q, want the resolved engine path %q", argv[sep+1], enginePath)
	}
	if got := argv[sep+2:]; len(got) != 2 || got[0] != "arg1" || got[1] != "arg2" {
		t.Errorf("engine args = %v, want [arg1 arg2]", got)
	}
}

// An input directory outside the job directory is never a token in the
// spec: forJob grants only the job directory, the class's static readOnly
// set, and the resolved engines -- nothing else, unlike libvipsSpec's own
// separate-input-directory grant.
func TestEngineExecGrantsNothingBesidesTheJobDirAndItsStaticSet(t *testing.T) {
	s := &engineSandbox{Helper: []string{"helper"}, Confine: true}
	x := s.forJob("/job", []string{"/lib"}, nil)
	outside := "/uploads/somewhere"
	for _, tok := range x.spec {
		if strings.Contains(tok, outside) {
			t.Errorf("spec %v grants a path never passed to forJob", x.spec)
		}
	}
}

// An engine name that does not resolve on PATH is absent from the ro:
// tokens -- it cannot be exec'd either way, and the call site that actually
// needs it reports "locate engine" through wrapEngine when it tries.
func TestEngineExecUnresolvedEngineNameIsSkipped(t *testing.T) {
	s := &engineSandbox{Helper: []string{"helper"}, Confine: true}
	x := s.forJob("/job", nil, []string{"definitely-not-a-real-binary-xyz"})
	for _, tok := range x.spec {
		if strings.Contains(tok, "definitely-not-a-real-binary-xyz") {
			t.Errorf("spec %v grants an engine name that never resolved on PATH", x.spec)
		}
	}
}

// engineExec{} and forJob on a nil sandbox run the engine directly: no spec,
// and no helper invocation.
func TestEngineExecNilSandboxRunsDirectly(t *testing.T) {
	dir := t.TempDir()
	fakeEngine(t, dir, "directengine", "echo direct-output\n")
	pathWith(t, dir)

	var s *engineSandbox
	x := s.forJob("/job", []string{"/lib"}, []string{"directengine"})
	if len(x.spec) != 0 {
		t.Errorf("nil sandbox produced a non-empty spec: %v", x.spec)
	}

	out, err := x.run(context.Background(), "directengine")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(out)) != "direct-output" {
		t.Errorf("stdout = %q, want the engine's own output directly", out)
	}
}

func TestEngineExecZeroValueRunsDirectly(t *testing.T) {
	dir := t.TempDir()
	fakeEngine(t, dir, "directengine2", "echo also-direct\n")
	pathWith(t, dir)

	var x engineExec
	out, err := x.run(context.Background(), "directengine2")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(out)) != "also-direct" {
		t.Errorf("stdout = %q", out)
	}
}

// engineSandbox{Helper: h, Confine: false}.forJob yields no rw:/ro: tokens --
// tectonic's own "run through the helper, restrict nothing" shape, because an
// EMPTY ruleset denies everything rather than nothing.
func TestEngineExecConfineFalseRestrictsNothing(t *testing.T) {
	s := &engineSandbox{Helper: []string{"helper", "__sandbox-exec"}, Confine: false}
	x := s.forJob("/job", []string{"/lib"}, []string{"vips"})
	if len(x.spec) != 0 {
		t.Errorf("spec = %v, want empty -- Confine:false means 'run through the helper, restrict nothing'", x.spec)
	}
}

// A helper that exits sandbox.SetupExit (78) without ever exec'ing the
// engine is ErrEngineSandboxUnavailable, and the underlying *exec.ExitError
// stays reachable through errors.As.
func TestEngineExecSandboxFaultSentinel(t *testing.T) {
	helper78 := failingSandboxHelper(t, 78)
	s := &engineSandbox{Helper: helper78, Confine: true}
	x := s.forJob(t.TempDir(), nil, nil)

	_, err := x.run(context.Background(), "true")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Errorf("err = %v, want errors.Is ErrEngineSandboxUnavailable", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("err = %v, want errors.As *exec.ExitError to hold", err)
	}
}

// A different exit status is NOT the sentinel, and the error text still
// names the engine (not the helper).
func TestEngineExecSandboxFaultOnlyExitCode78(t *testing.T) {
	helper1 := failingSandboxHelper(t, 1)
	s := &engineSandbox{Helper: helper1, Confine: true}
	x := s.forJob(t.TempDir(), nil, nil)

	_, err := x.run(context.Background(), "true")
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Error("exit code 1 must not be classified as the sandbox sentinel")
	}
	if !strings.HasPrefix(err.Error(), "true failed:") {
		t.Errorf("err = %q, want it to start with %q", err.Error(), "true failed:")
	}
}

// An UNCONFINED engine that happens to exit 78 on its own is not the
// sentinel -- there is no helper here that could have failed to establish
// anything.
func TestEngineExecUnconfinedExit78IsNotTheSentinel(t *testing.T) {
	dir := t.TempDir()
	fakeEngine(t, dir, "exit78engine", "exit 78\n")
	pathWith(t, dir)

	var s *engineSandbox
	x := s.forJob("/job", nil, []string{"exit78engine"})
	_, err := x.run(context.Background(), "exit78engine")
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Error("an unconfined engine exiting 78 must not be classified as a sandbox fault -- there is no helper to have failed")
	}
}

// An engine outliving its context is killed, and the error names the ENGINE
// ("slowengine killed:", not the helper) and carries context.DeadlineExceeded.
func TestEngineExecKilledOnContextDeadline(t *testing.T) {
	dir := t.TempDir()
	fakeEngine(t, dir, "slowengine", "sleep 5\n")
	pathWith(t, dir)

	var s *engineSandbox
	x := s.forJob("/job", nil, []string{"slowengine"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := x.run(ctx, "slowengine")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "slowengine killed:") {
		t.Errorf("err = %q, want it to contain %q", err.Error(), "slowengine killed:")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want errors.Is context.DeadlineExceeded", err)
	}
}
