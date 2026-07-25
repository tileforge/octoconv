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

// TestComputeProgressPct_ClampsOverAndUnder proves the [0,100] clamp on
// both ends: a raw ratio past 100% (encoder lookahead overshoot) clamps to
// exactly 100, and a nonsensical <=0 source duration reports ok=false
// rather than dividing by zero/garbage (PROG-01's null-denominator policy).
func TestComputeProgressPct_ClampsOverAndUnder(t *testing.T) {
	if pct, ok := computeProgressPct(150, 100); !ok || pct != 100 {
		t.Errorf("computeProgressPct(150, 100) = (%d, %v), want (100, true)", pct, ok)
	}
	if pct, ok := computeProgressPct(10, 0); ok {
		t.Errorf("computeProgressPct(10, 0) = (%d, %v), want ok=false", pct, ok)
	}
	if pct, ok := computeProgressPct(10, -5); ok {
		t.Errorf("computeProgressPct(10, -5) = (%d, %v), want ok=false", pct, ok)
	}
}

// TestComputeProgressPct_MultiMinuteMicrosecondScale is the Pitfall-1
// regression pin (41-RESEARCH.md): a wrong divisor (e.g. /1000 instead of
// /1e6) is invisible on a few-second clip but blows past 100% at
// multi-minute scale. 1800s of a 3600s (1hr) source must read exactly 50%,
// not clamp to 100 the way a /1000-bug's inflated value would.
func TestComputeProgressPct_MultiMinuteMicrosecondScale(t *testing.T) {
	pct, ok := computeProgressPct(1_800.0, 3_600.0)
	if !ok {
		t.Fatal("computeProgressPct(1800, 3600) ok = false, want true")
	}
	if pct != 50 {
		t.Errorf("computeProgressPct(1800, 3600) = %d, want 50", pct)
	}
}

// TestComputeProgressPct_MonotonicRamp proves a sequence of increasing
// out_time values yields non-decreasing percent (D-02a).
func TestComputeProgressPct_MonotonicRamp(t *testing.T) {
	outTimes := []float64{0, 5, 12, 19.933333}
	const duration = 20.0
	prev := -1
	for _, ot := range outTimes {
		pct, ok := computeProgressPct(ot, duration)
		if !ok {
			t.Fatalf("computeProgressPct(%v, %v) ok = false", ot, duration)
		}
		if pct < prev {
			t.Errorf("computeProgressPct ramp went backward: %d after %d (outTime=%v)", pct, prev, ot)
		}
		prev = pct
	}
}

// TestETA_NilBeforeWarmup proves etaSeconds returns nil until
// progressWarmupSamples positive samples have been folded into the EMA
// (D-04) -- an ETA computed off the first noisy sample(s) would be
// misleading (Pitfall 4, 41-RESEARCH.md).
func TestETA_NilBeforeWarmup(t *testing.T) {
	var est etaEstimator
	for i := 0; i < progressWarmupSamples-1; i++ {
		est.update(2.0)
		if eta := est.etaSeconds(100); eta != nil {
			t.Fatalf("etaSeconds before warm-up (sample %d) = %v, want nil", i, *eta)
		}
	}
}

// TestETA_PositiveAfterWarmup proves etaSeconds returns a non-negative
// estimate once warm-up completes, computed from the EMA-smoothed speed.
func TestETA_PositiveAfterWarmup(t *testing.T) {
	var est etaEstimator
	for i := 0; i < progressWarmupSamples; i++ {
		est.update(2.0)
	}
	eta := est.etaSeconds(100)
	if eta == nil {
		t.Fatal("etaSeconds after warm-up = nil, want a value")
	}
	if *eta < 0 {
		t.Errorf("etaSeconds = %d, want non-negative", *eta)
	}
	// speed is a steady 2.0x for every sample, so the EMA converges to
	// 2.0 exactly regardless of alpha -- 100s remaining / 2.0x = 50s.
	if *eta != 50 {
		t.Errorf("etaSeconds(100) with steady speed=2.0 = %d, want 50", *eta)
	}
}

// TestETA_NonPositiveSamplesDoNotPollute proves a non-positive (or
// effectively N/A-derived zero) speed sample is skipped rather than folded
// into the EMA -- feeding a burst of zeros between real samples must not
// drag the estimate toward zero/reset the warm-up counter.
func TestETA_NonPositiveSamplesDoNotPollute(t *testing.T) {
	var est etaEstimator
	est.update(2.0)
	est.update(0)  // must be ignored
	est.update(-1) // must be ignored
	est.update(2.0)
	est.update(2.0)
	eta := est.etaSeconds(100)
	if eta == nil {
		t.Fatal("etaSeconds = nil, want a value (3 real samples fed, non-positive ones ignored)")
	}
	if *eta != 50 {
		t.Errorf("etaSeconds(100) = %d, want 50 (non-positive samples must not skew the EMA)", *eta)
	}
}
