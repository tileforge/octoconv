package convert

import (
	"context"
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
	_, err := runCommandStreaming(context.Background(), func(l string) { lines = append(lines, l) }, "true")
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
	}, "ffmpeg", args...); err != nil {
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
		_, err := runCommandStreaming(ctx, func(string) {}, "ffmpeg", args...)
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
