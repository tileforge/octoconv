package convert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRunCommandStreaming_ArgvHardening pins the transport choice (T-41-07):
// the injected progress arg is EXACTLY "-progress file:<fifo>" -- never
// "pipe:", mirroring TestAVBuildersHardenEveryInvocation's style of testing
// the pure argv-building function directly, with no process spawned.
func TestRunCommandStreaming_ArgvHardening(t *testing.T) {
	got := progressStreamArgs("/tmp/octoconv-progress-xyz/progress.fifo", []string{"-i", "in.mp4", "out.mp4"})
	if len(got) < 2 || got[0] != "-progress" {
		t.Fatalf("progressStreamArgs()[0] = %v, want \"-progress\" first", got)
	}
	if got[1] != "file:/tmp/octoconv-progress-xyz/progress.fifo" {
		t.Errorf("progressStreamArgs()[1] = %q, want a file:-prefixed fifo path", got[1])
	}
	for _, a := range got {
		if strings.Contains(a, "pipe:") {
			t.Errorf("progressStreamArgs() = %v contains %q, want no pipe: transport (unavailable on the pinned build)", got, a)
		}
	}
	// The caller's own args must survive untouched, appended after the
	// progress option pair.
	if got[2] != "-i" || got[3] != "in.mp4" || got[4] != "out.mp4" {
		t.Errorf("progressStreamArgs() = %v, want caller args preserved after the progress pair", got)
	}
}

// directCommand is the command func every DIRECT (unconfined) test below
// passes to runCommandStreaming: no sandbox wrapping, just the bare engine
// name and progressStreamArgs(fifoPath, args).
func directCommand(name string, args []string) func(fifoPath string) (string, []string, error) {
	return func(fifoPath string) (string, []string, error) {
		return name, progressStreamArgs(fifoPath, args), nil
	}
}

// TestRunCommandStreaming_NeverOpenedFIFO proves the never-opened-FIFO
// unblock (PROG-05): a command that exits immediately without ever opening
// the progress FIFO ("true" ignores all arguments, including the injected
// "-progress file:<fifo>" pair) must still return promptly, with zero
// leaked goroutines -- the post-Wait() write-end poke is what guarantees
// this, since the reader would otherwise depend solely on a real ffmpeg
// writer that never materializes.
func TestRunCommandStreaming_NeverOpenedFIFO(t *testing.T) {
	before := settledGoroutines(t)

	var lines []string
	_, err := runCommandStreaming(context.Background(), func(l string) { lines = append(lines, l) },
		t.TempDir(), "true", directCommand("true", nil))
	if err != nil {
		t.Fatalf("runCommandStreaming(true) = %v, want nil error", err)
	}
	if len(lines) != 0 {
		t.Errorf("runCommandStreaming(true) onLine calls = %v, want none (fifo never opened)", lines)
	}

	after := settledGoroutines(t)
	if after != before {
		t.Errorf("goroutine count before=%d after=%d, want equal (zero leak on never-opened FIFO)", before, after)
	}
}

// TestRunCommandStreaming_SuccessPath spawns a short real ffmpeg transcode
// with a synthetic lavfi source and asserts onLine received real -progress
// blocks (including the terminal "progress=end" line) via the FIFO/file:
// transport, plus a clean goroutine count afterward. Gated behind
// testing.Short() and a live-binary check so `go test -short` stays
// hermetic (41-CONTEXT.md, PROG-05).
func TestRunCommandStreaming_SuccessPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-ffmpeg test in -short mode")
	}
	requireLiveAVBinaries(t)

	before := settledGoroutines(t)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.mp4")
	args := []string{
		"-y", "-nostdin",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac",
		"file:" + outPath,
	}

	var lines []string
	var sawEnd bool
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := runCommandStreaming(ctx, func(l string) {
		lines = append(lines, l)
		if l == "progress=end" {
			sawEnd = true
		}
	}, dir, "ffmpeg", directCommand("ffmpeg", args)); err != nil {
		t.Fatalf("runCommandStreaming(ffmpeg transcode) = %v, want nil error", err)
	}

	if len(lines) == 0 {
		t.Fatal("runCommandStreaming captured zero -progress lines, want a real block stream")
	}
	if !sawEnd {
		t.Errorf("runCommandStreaming lines = %v, want a terminal \"progress=end\" line", lines)
	}

	after := settledGoroutines(t)
	if after != before {
		t.Errorf("goroutine count before=%d after=%d, want equal (zero leak on success path)", before, after)
	}
}

// TestRunCommandStreaming_SIGKILLMidStream_NoLeak is the unit half of
// PROG-05's live-verification requirement: >=5 iterations of a real,
// guaranteed-still-running ffmpeg transcode cancelled mid-stream via a
// short context timeout, asserting runtime.NumGoroutine() delta == 0
// across the loop and that every iteration returns the "killed" error
// shape -- mirrors 41-RESEARCH.md's live-verified harness.
func TestRunCommandStreaming_SIGKILLMidStream_NoLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-ffmpeg test in -short mode")
	}
	requireLiveAVBinaries(t)

	before := settledGoroutines(t)

	const iterations = 5
	for i := 0; i < iterations; i++ {
		dir := t.TempDir()
		outPath := filepath.Join(dir, "out.mp4")
		args := []string{
			"-y", "-nostdin",
			"-f", "lavfi", "-i", "testsrc=duration=30:size=1280x720:rate=30",
			"-f", "lavfi", "-i", "sine=frequency=1000:duration=30",
			"-c:v", "libx264", "-preset", "veryslow", "-crf", "12", "-c:a", "aac",
			"file:" + outPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := runCommandStreaming(ctx, func(string) {}, dir, "ffmpeg", directCommand("ffmpeg", args))
		cancel()
		if err == nil || !strings.Contains(err.Error(), "killed") {
			t.Fatalf("iter %d: runCommandStreaming = %v, want a %q error", i, err, "killed")
		}
	}

	after := settledGoroutines(t)
	if after != before {
		t.Errorf("goroutine count before=%d after=%d (delta=%d) across %d SIGKILL iterations, want delta=0",
			before, after, after-before, iterations)
	}
}

// settledGoroutines returns runtime.NumGoroutine() after giving any
// just-finished goroutines a brief window to actually be deregistered by
// the runtime -- avoids flaking on a benign scheduling lag between a
// goroutine's return and the count reflecting it.
func settledGoroutines(t *testing.T) int {
	t.Helper()
	runtime.GC()
	n := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		next := runtime.NumGoroutine()
		if next == n {
			return n
		}
		n = next
	}
	return n
}

// ---- engineExec.stream (Phase 68 Plan 04) ----

// fakeProgressFFmpeg writes a fake ffmpeg to dir/name: it writes non-empty
// bytes to its own last argument (with the "file:" prefix stripped -- the
// engine's real output path), and -- when its FIRST argument is
// "-progress" (the streaming shape progressStreamArgs produces) -- ALSO
// writes an "out_time_us="/"progress=end" block to the FIFO named by its
// second argument (again with "file:" stripped), so a test can assert
// onLine actually receives it.
func fakeProgressFFmpeg(t *testing.T, dir, name string) {
	t.Helper()
	fakeEngine(t, dir, name, `last=""
for a in "$@"; do last="$a"; done
out="${last#file:}"
printf 'fake-ffmpeg-output' > "$out"
if [ "$1" = "-progress" ]; then
  fifo="${2#file:}"
  {
    printf 'out_time_us=1000000\n'
    printf 'progress=end\n'
  } > "$fifo"
fi
`)
}

// TestEngineExecStream_DeliversProgressLinesThroughTheHelper proves
// engineExec.stream, through a confined helper, delivers the engine's own
// -progress lines to onLine, and that the recorded helper argv carries
// "-progress" AFTER the "--" separator (on the ENGINE's own argv) rather
// than before it (T-68-18): "__sandbox-exec", the rw:/ro: tokens, "--", the
// resolved fake-ffmpeg path, "-progress", the file:-prefixed fifo path
// (inside fifoDir, under a ".progress-" directory), then the caller's own
// args.
func TestEngineExecStream_DeliversProgressLinesThroughTheHelper(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeProgressFFmpeg(t, dir, "fakeffmpeg")
	pathWith(t, dir)

	s := &engineSandbox{Helper: helper, Confine: true}
	jobDir := t.TempDir()
	x := s.forJob(jobDir, []string{"/lib"}, []string{"fakeffmpeg"})

	var lines []string
	if _, err := x.stream(context.Background(), func(l string) { lines = append(lines, l) },
		jobDir, "fakeffmpeg", []string{"-i", "in.mp4"}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(lines) != 2 || lines[0] != "out_time_us=1000000" || lines[1] != "progress=end" {
		t.Fatalf("onLine received %v, want [out_time_us=1000000 progress=end]", lines)
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
	for _, tok := range argv[1:sep] {
		if tok == "-progress" {
			t.Fatalf("argv %v carries -progress BEFORE the -- separator -- it must be part of the engine's own argv, after it", argv)
		}
	}
	var rw string
	for _, tok := range argv[1:sep] {
		if strings.HasPrefix(tok, "rw:") {
			rw = strings.TrimPrefix(tok, "rw:")
		}
	}
	if rw != jobDir {
		t.Errorf("rw token = %q, want the job directory %q", rw, jobDir)
	}
	if filepath.Base(argv[sep+1]) != "fakeffmpeg" {
		t.Fatalf("command after -- is %q, want the resolved fakeffmpeg path", argv[sep+1])
	}
	rest := argv[sep+1:]
	if len(rest) < 3 || rest[1] != "-progress" {
		t.Fatalf("engine argv after -- = %v, want [<engine>, -progress, file:..., ...]", rest)
	}
	if !strings.HasPrefix(rest[2], "file:"+jobDir) || !strings.Contains(rest[2], ".progress-") {
		t.Errorf("fifo argument = %q, want a file:-prefixed path inside %q under a .progress- directory", rest[2], jobDir)
	}
	if got := rest[3:]; len(got) != 2 || got[0] != "-i" || got[1] != "in.mp4" {
		t.Errorf("caller args = %v, want [-i in.mp4] preserved after the progress pair", got)
	}

	// No ".progress-" entry remains in the job directory once stream has
	// returned -- the FIFO's own directory is removed on every return path.
	entries, err := os.ReadDir(jobDir)
	if err != nil {
		t.Fatalf("read job dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".progress-") {
			t.Errorf("job dir %v still contains %q after stream returned, want it removed", jobDir, e.Name())
		}
	}
}

// TestEngineExecStream_UnconfinedRunsDirectlyWithProgressFirst proves an
// unconfined engineExec{}.stream runs the engine directly, with
// "-progress" as its OWN first argument (no helper, no "--" separator to
// speak of).
func TestEngineExecStream_UnconfinedRunsDirectlyWithProgressFirst(t *testing.T) {
	dir := t.TempDir()
	fakeProgressFFmpeg(t, dir, "directffmpeg")
	pathWith(t, dir)

	var x engineExec
	fifoDir := t.TempDir()
	var lines []string
	if _, err := x.stream(context.Background(), func(l string) { lines = append(lines, l) },
		fifoDir, "directffmpeg", []string{"-i", "in.mp4"}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(lines) != 2 || lines[0] != "out_time_us=1000000" || lines[1] != "progress=end" {
		t.Fatalf("onLine received %v, want [out_time_us=1000000 progress=end] -- the fake engine only "+
			"writes the fifo block when its OWN first argument is -progress", lines)
	}
}

// TestEngineExecStream_SandboxFaultSentinel proves a helper exiting
// sandbox.SetupExit (78) without ever exec'ing the engine is classified
// ErrEngineSandboxUnavailable, exactly like engineExec.run.
func TestEngineExecStream_SandboxFaultSentinel(t *testing.T) {
	s := &engineSandbox{Helper: failingSandboxHelper(t, 78), Confine: true}
	jobDir := t.TempDir()
	x := s.forJob(jobDir, nil, nil)

	_, err := x.stream(context.Background(), func(string) {}, jobDir, "true", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Errorf("err = %v, want errors.Is ErrEngineSandboxUnavailable", err)
	}
}

// TestEngineExecStream_FIFODirectoryRemovedOnFailure proves the FIFO's own
// directory is removed even when the engine invocation fails outright (no
// helper at all involved here -- the failure is the engine itself never
// resolving).
func TestEngineExecStream_FIFODirectoryRemovedOnFailure(t *testing.T) {
	var x engineExec
	fifoDir := t.TempDir()
	if _, err := x.stream(context.Background(), func(string) {}, fifoDir,
		"definitely-not-a-real-binary-xyz", nil); err == nil {
		t.Fatal("expected an error resolving a nonexistent engine")
	}

	entries, err := os.ReadDir(fifoDir)
	if err != nil {
		t.Fatalf("read fifo dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".progress-") {
			t.Errorf("fifo dir %v still contains %q after a failed stream, want it removed", fifoDir, e.Name())
		}
	}
}
