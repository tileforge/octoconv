package convert

import (
	"bufio"
	"os"
	"testing"
)

// loadProgressFixture reads the committed golden ffmpeg -progress capture
// (testdata/ffmpeg_progress_transcode.txt) and feeds every non-comment line
// through a fresh progressBlockParser, returning every completed tick in
// stream order. Mirrors generateTestVideo's t.Helper()-fixture-loader
// convention (avduration_test.go) but reads a committed file instead of
// invoking a live ffmpeg.
func loadProgressFixture(t *testing.T) []progressTick {
	t.Helper()
	f, err := os.Open("testdata/ffmpeg_progress_transcode.txt")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	var ticks []progressTick
	p := newProgressBlockParser()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if len(line) > 0 && line[0] == '#' {
			continue // header comment, not a progress key=value line
		}
		if tick, complete := p.feedLine(line); complete {
			ticks = append(ticks, tick)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return ticks
}

// TestParseProgress_WarmupBlockIsNotYetDerivable proves the fixture's FIRST
// block (out_time_us=N/A, speed=N/A) parses into a tick flagged
// not-yet-derivable, never a strconv error/garbage value (Pitfall 2,
// 41-RESEARCH.md).
func TestParseProgress_WarmupBlockIsNotYetDerivable(t *testing.T) {
	ticks := loadProgressFixture(t)
	if len(ticks) == 0 {
		t.Fatal("loadProgressFixture returned zero ticks")
	}
	first := ticks[0]
	if first.hasData {
		t.Errorf("first (warm-up) tick hasData = true, want false (N/A out_time/speed)")
	}
	if first.outTimeSeconds != 0 {
		t.Errorf("first (warm-up) tick outTimeSeconds = %v, want 0 (never a garbage parse)", first.outTimeSeconds)
	}
	if first.speed != 0 {
		t.Errorf("first (warm-up) tick speed = %v, want 0 (never a garbage parse)", first.speed)
	}
	if first.done {
		t.Errorf("first tick done = true, want false (progress=continue)")
	}
}

// TestParseProgress_MidStreamNARblockIsAlsoNotYetDerivable proves an N/A
// block occurring in the MIDDLE of the stream (not just the first block) is
// handled identically -- the fixture captures one at frame=569.
func TestParseProgress_MidStreamNABlockIsAlsoNotYetDerivable(t *testing.T) {
	ticks := loadProgressFixture(t)
	found := false
	for i, tk := range ticks {
		if i == 0 {
			continue // the leading warm-up block is covered by a dedicated test
		}
		if !tk.hasData && !tk.done {
			found = true
		}
	}
	if !found {
		t.Fatal("expected at least one mid-stream N/A (not-yet-derivable, non-terminal) tick in the fixture")
	}
}

// TestParseProgress_MicrosecondDivision proves out_time_us/out_time_ms
// (identical microsecond values, the "_ms" naming wart) are divided by 1e6,
// and that a trailing "x" (optionally leading-space-padded, e.g. "   3x") is
// stripped from speed= before parsing (Pitfall 1, 41-RESEARCH.md).
func TestParseProgress_MicrosecondDivision(t *testing.T) {
	ticks := loadProgressFixture(t)
	last := ticks[len(ticks)-1]
	if !last.hasData {
		t.Fatal("last tick hasData = false, want true (real numeric block)")
	}
	const wantSeconds = 19.933333
	if diff := last.outTimeSeconds - wantSeconds; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("last tick outTimeSeconds = %v, want %v (out_time_us=19933333 / 1e6)", last.outTimeSeconds, wantSeconds)
	}
	if last.speed != 3 {
		t.Errorf("last tick speed = %v, want 3 (speed=\"   3x\" trailing-x/whitespace stripped)", last.speed)
	}
}

// TestParseProgress_TerminatorFlagsDone proves progress=end yields a
// terminal tick and progress=continue does not.
func TestParseProgress_TerminatorFlagsDone(t *testing.T) {
	ticks := loadProgressFixture(t)
	last := ticks[len(ticks)-1]
	if !last.done {
		t.Errorf("last tick (progress=end) done = false, want true")
	}
	for i, tk := range ticks[:len(ticks)-1] {
		if tk.done {
			t.Errorf("tick %d (progress=continue) done = true, want false", i)
		}
	}
}

// TestParseProgress_NoClampAtParseTime proves the parser itself does not
// clamp an out_time that exceeds a (hypothetical, small) source duration --
// clamping is computeProgressPct's job (Task 2), not the parser's.
func TestParseProgress_NoClampAtParseTime(t *testing.T) {
	p := newProgressBlockParser()
	lines := []string{
		"frame=1000",
		"out_time_us=999000000", // 999s -- deliberately huge
		"out_time_ms=999000000",
		"speed=5.0x",
		"progress=end",
	}
	var tick progressTick
	var complete bool
	for _, l := range lines {
		tick, complete = p.feedLine(l)
	}
	if !complete {
		t.Fatal("feedLine on the terminator line did not report a completed tick")
	}
	if tick.outTimeSeconds != 999 {
		t.Errorf("outTimeSeconds = %v, want 999 (unclamped at parse time)", tick.outTimeSeconds)
	}
}

// TestParseProgress_ContractsExistAlongsideConverter proves the three seam
// contracts (D-01) are usable as ordinary Go types/interfaces without
// touching Converter itself -- a compile-time proof more than a runtime
// assertion, but exercised here so `go vet`/`go build` catch any accidental
// signature drift.
func TestParseProgress_ContractsExistAlongsideConverter(t *testing.T) {
	var _ ProgressSink = fakeProgressSink{}
	var u ProgressUpdate
	if u.Pct != 0 || u.ETA != nil {
		t.Fatalf("zero-value ProgressUpdate = %+v, want Pct=0, ETA=nil", u)
	}
}

// fakeProgressSink is a minimal ProgressSink used only to prove the
// interface's method set compiles against a trivial implementation.
type fakeProgressSink struct{}

func (fakeProgressSink) SourceDuration(seconds float64) {}
func (fakeProgressSink) Update(u ProgressUpdate)         {}
