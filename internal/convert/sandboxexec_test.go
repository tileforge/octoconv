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

// A confined forJobRW run grants rw:<job dir>, then rw: for every extraRW
// path IN THE ORDER GIVEN -- CONF-06's html/tmp decision depends on that
// order being stable and on jobDir always coming first. forJob (no extras)
// stays byte-identical: exactly one rw: token, the job directory.
func TestEngineExecRWGrantsJobDirThenExtrasInOrder(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeEngine(t, dir, "fakeengine", "echo ok\n")
	pathWith(t, dir)

	s := &engineSandbox{Helper: helper, Confine: true}
	jobDir := t.TempDir()

	x := s.forJobRW(jobDir, []string{"/extra1", "/extra2"}, nil, []string{"fakeengine"})
	if _, err := x.run(context.Background(), "fakeengine"); err != nil {
		t.Fatalf("run: %v", err)
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
	var rw []string
	for _, tok := range argv[1:sep] {
		if strings.HasPrefix(tok, "rw:") {
			rw = append(rw, strings.TrimPrefix(tok, "rw:"))
		}
	}
	want := []string{jobDir, "/extra1", "/extra2"}
	if len(rw) != len(want) {
		t.Fatalf("rw tokens = %v, want %v", rw, want)
	}
	for i := range want {
		if rw[i] != want[i] {
			t.Errorf("rw[%d] = %q, want %q", i, rw[i], want[i])
		}
	}

	// forJob unchanged: exactly one rw: token, the job dir.
	x2 := s.forJob(jobDir, nil, []string{"fakeengine"})
	if _, err := x2.run(context.Background(), "fakeengine"); err != nil {
		t.Fatalf("run: %v", err)
	}
	invocations = calls()
	argv2 := invocations[len(invocations)-1]
	sep2 := indexOf(argv2, "--")
	var rw2 []string
	for _, tok := range argv2[1:sep2] {
		if strings.HasPrefix(tok, "rw:") {
			rw2 = append(rw2, strings.TrimPrefix(tok, "rw:"))
		}
	}
	if len(rw2) != 1 || rw2[0] != jobDir {
		t.Errorf("forJob's rw tokens = %v, want exactly [%s]", rw2, jobDir)
	}
}

// engineExec.withEnv REPLACES the engine's environment entirely -- not a
// merge -- both directly (unconfined) and through the real helper path
// (confined), which inherits the replaced environment across its own exec.
func TestEngineExecEnvReplacesTheInheritedEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "secret-canary")
	dir := t.TempDir()
	fakeEngine(t, dir, "envengine", "env\n")
	pathWith(t, dir)

	runWith := func(s *engineSandbox) string {
		// PATH includes /usr/bin:/bin too -- the fake engine's own script
		// calls the external `env` binary to dump its environment, and that
		// binary must itself be resolvable under the REPLACED environment
		// this test is proving.
		x := s.forJob(t.TempDir(), nil, []string{"envengine"}).withEnv([]string{"PATH=" + dir + ":/usr/bin:/bin", "ONLY=1"})
		out, err := x.run(context.Background(), "envengine")
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		return string(out)
	}

	var unconfined *engineSandbox
	if out := runWith(unconfined); !strings.Contains(out, "ONLY=1") || strings.Contains(out, "DATABASE_URL") {
		t.Errorf("unconfined output = %q, want ONLY=1 present and DATABASE_URL absent", out)
	}

	helper, _ := fakeSandboxHelper(t)
	confined := &engineSandbox{Helper: helper, Confine: true}
	if out := runWith(confined); !strings.Contains(out, "ONLY=1") || strings.Contains(out, "DATABASE_URL") {
		t.Errorf("confined output = %q, want ONLY=1 present and DATABASE_URL absent", out)
	}
}

// Without withEnv, every phase-68 class is unchanged: the fake engine still
// sees this process's own environment (DATABASE_URL included).
func TestEngineExecNilEnvInherits(t *testing.T) {
	t.Setenv("DATABASE_URL", "secret-canary")
	dir := t.TempDir()
	fakeEngine(t, dir, "envengine2", "env\n")
	pathWith(t, dir)

	var s *engineSandbox
	x := s.forJob(t.TempDir(), nil, []string{"envengine2"})
	out, err := x.run(context.Background(), "envengine2")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(string(out), "DATABASE_URL=secret-canary") {
		t.Errorf("output = %q, want it to contain the inherited DATABASE_URL", out)
	}
}

// engineEnv is an allowlist: PATH/HOME/LANG/LC_ALL only when set, TMPDIR
// only when requested, and never a secret -- the same answer tectonicEnv
// already gives, reused here for html.
func TestEngineEnvIsAnAllowlist(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/tester")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("DATABASE_URL", "secret-canary")
	t.Setenv("S3_SECRET_KEY", "secret-canary-2")

	env := engineEnv("")
	for _, want := range []string{"PATH=/usr/bin:/bin", "HOME=/home/tester", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"} {
		if indexOf(env, want) < 0 {
			t.Errorf("engineEnv(\"\") = %v, missing %q", env, want)
		}
	}
	for _, bad := range []string{"DATABASE_URL=", "S3_SECRET_KEY=", "TMPDIR="} {
		for _, e := range env {
			if strings.HasPrefix(e, bad) {
				t.Errorf("engineEnv(\"\") leaked %q", e)
			}
		}
	}

	env2 := engineEnv("/job/tmp")
	if len(env2) == 0 || env2[len(env2)-1] != "TMPDIR=/job/tmp" {
		t.Errorf("engineEnv(\"/job/tmp\") = %v, want it to end with TMPDIR=/job/tmp", env2)
	}
}

// sharedTmpGuard fires ONLY when all three hold: confined, the writable set
// grants sharedTmpDir, and concurrency is anything but exactly 1 -- keyed on
// the GRANT SET, never on a class name (CONF-06). A value below 1 is not
// "one at a time": asynq runs runtime.NumCPU() tasks for it.
func TestSharedTmpGuard(t *testing.T) {
	confined := &engineSandbox{Helper: []string{"helper"}, Confine: true}
	var unconfined *engineSandbox

	cases := []struct {
		name        string
		readWrite   []string
		sandbox     *engineSandbox
		concurrency int
		wantErr     bool
	}{
		{"confined shared /tmp concurrency 2", []string{"/tmp", "/dev/null"}, confined, 2, true},
		{"confined shared /tmp concurrency 1", []string{"/tmp", "/dev/null"}, confined, 1, false},
		{"confined shared /tmp concurrency 0", []string{"/tmp", "/dev/null"}, confined, 0, true},
		{"confined shared /tmp concurrency -1", []string{"/tmp", "/dev/null"}, confined, -1, true},
		{"unconfined shared /tmp concurrency 2", []string{"/tmp"}, unconfined, 2, false},
		{"unconfined shared /tmp concurrency 0", []string{"/tmp"}, unconfined, 0, false},
		{"confined without /tmp concurrency 0", []string{"/dev/null"}, confined, 0, false},
		{"confined without /tmp concurrency -1 (empty)", nil, confined, -1, false},
		{"confined without /tmp concurrency 4 (empty)", nil, confined, 4, false},
		{"confined without /tmp concurrency 4 (/dev/null only)", []string{"/dev/null"}, confined, 4, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := sharedTmpGuard(c.readWrite, c.sandbox, c.concurrency, "X_CONCURRENCY")
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !strings.Contains(err.Error(), fmt.Sprintf("X_CONCURRENCY=%d", c.concurrency)) || !strings.Contains(err.Error(), "shared /tmp") {
					t.Errorf("err = %q, missing expected substrings", err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// engineExec.stream refuses a per-exec environment outright rather than
// silently dropping it -- runCommandStreaming has no env parameter, so a
// caller that opted into withEnv and then streamed would otherwise run the
// engine with the WRONG (inherited) environment.
func TestEngineExecStreamRefusesAnEnv(t *testing.T) {
	dir := t.TempDir()
	fakeEngine(t, dir, "streamengine", "echo should-not-run\n")
	pathWith(t, dir)

	var s *engineSandbox
	jobDir := t.TempDir()
	x := s.forJob(jobDir, nil, []string{"streamengine"}).withEnv([]string{"ONLY=1"})
	if _, err := x.stream(context.Background(), func(string) {}, jobDir, "streamengine", nil); err == nil {
		t.Fatal("expected an error: stream must refuse a per-exec environment rather than silently drop it")
	}
}

// forJobRWSock renders the socket-only grant kind between the writable and
// the read-only tokens (rw:, then sock:, then ro:), and forJobRW -- what every
// other class calls -- renders no sock: token at all, so their token streams
// are unchanged by the socket right's existence.
func TestEngineExecSocketTokensFollowRWAndPrecedeRO(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeEngine(t, dir, "fakeengine", "true\n")
	pathWith(t, dir)

	s := &engineSandbox{Helper: helper, Confine: true}
	jobDir := t.TempDir()
	withSock := s.forJobRWSock(jobDir, []string{"/extra-rw"}, []string{"/sockdir"}, []string{"/lib"}, []string{"fakeengine"})
	plain := s.forJobRW(jobDir, []string{"/extra-rw"}, []string{"/lib"}, []string{"fakeengine"})

	for _, x := range []engineExec{withSock, plain} {
		if _, err := x.run(context.Background(), "fakeengine"); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	invocations := calls()
	if len(invocations) != 2 {
		t.Fatalf("helper invoked %d time(s), want 2", len(invocations))
	}

	prefixOf := func(tok string) string { return tok[:strings.Index(tok, ":")+1] }
	socket := invocations[0][1:indexOf(invocations[0], "--")]
	var kinds []string
	for _, tok := range socket {
		if k := prefixOf(tok); len(kinds) == 0 || kinds[len(kinds)-1] != k {
			kinds = append(kinds, k)
		}
	}
	if strings.Join(kinds, " ") != "rw: sock: ro:" {
		t.Errorf("token kinds in order = %v, want rw: then sock: then ro: (tokens %v)", kinds, socket)
	}
	if want := "sock:/sockdir"; indexOf(socket, want) < 0 {
		t.Errorf("tokens %v lack %s", socket, want)
	}
	if want := "rw:/extra-rw"; indexOf(socket, want) != indexOf(socket, "rw:"+jobDir)+1 {
		t.Errorf("tokens %v: the extra writable entry must directly follow the job directory", socket)
	}

	for _, tok := range invocations[1][1:indexOf(invocations[1], "--")] {
		if strings.HasPrefix(tok, "sock:") {
			t.Errorf("forJobRW rendered a socket token %q: every other class's token stream must stay unchanged", tok)
		}
	}
}
