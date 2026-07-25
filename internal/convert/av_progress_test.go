package convert

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// fakeAVProgressSink is a recording ProgressSink used to assert
// AVConverter.ConvertWithProgress's D-02 null-vs-ramp split without a real
// worker/DB in the loop.
type fakeAVProgressSink struct {
	durationCalls int
	duration      float64
	updates       []ProgressUpdate
}

func (f *fakeAVProgressSink) SourceDuration(seconds float64) {
	f.durationCalls++
	f.duration = seconds
}

func (f *fakeAVProgressSink) Update(u ProgressUpdate) {
	f.updates = append(f.updates, u)
}

var _ ProgressSink = (*fakeAVProgressSink)(nil)

// TestNewTranscodeProgressReporter_MonotonicAndClamped feeds a synthetic
// -progress block stream directly (no subprocess) -- including a
// deliberately regressive raw tick (encoder lookahead/B-frame reorder can
// transiently report a LOWER out_time than a prior tick) -- and asserts the
// composed ProgressUpdate sequence sink receives is monotonic non-decreasing
// (D-02a): the reporter must clamp a regression rather than passing it
// through.
func TestNewTranscodeProgressReporter_MonotonicAndClamped(t *testing.T) {
	sink := &fakeAVProgressSink{}
	report := newTranscodeProgressReporter(20, sink)

	lines := []string{
		"out_time_us=N/A", "speed=N/A", "progress=continue",
		"out_time_us=5000000", "speed=1.0x", "progress=continue",
		"out_time_us=10000000", "speed=1.0x", "progress=continue",
		"out_time_us=8000000", "speed=1.0x", "progress=continue", // regressive raw tick
		"out_time_us=20000000", "speed=1.0x", "progress=end",
	}
	for _, l := range lines {
		report(l)
	}

	if len(sink.updates) != 5 {
		t.Fatalf("sink.updates = %d entries, want 5 (one per progress= terminator)", len(sink.updates))
	}
	last := -1
	for i, u := range sink.updates {
		if u.Pct < last {
			t.Errorf("updates[%d].Pct = %d, want >= previous %d (monotonic, D-02a)", i, u.Pct, last)
		}
		last = u.Pct
	}
	if got := sink.updates[len(sink.updates)-1].Pct; got != 100 {
		t.Errorf("final update Pct = %d, want 100 (out_time == source duration)", got)
	}
}

// TestAVConverter_ConvertWithProgress_TranscodeRamps_LiveBinary proves the
// live end-to-end path: an mp4(h264/aac) -> webm request is ALWAYS a full
// re-encode (transcodeToWebMArgs never selects a copy fast path), so it
// must drive SourceDuration exactly once and a monotonic non-decreasing
// Update sequence via the real runCommandStreaming/ffmpeg -progress feed
// (PROG-01/PROG-02/D-02).
func TestAVConverter_ConvertWithProgress_TranscodeRamps_LiveBinary(t *testing.T) {
	requireLiveAVBinaries(t)
	dir := t.TempDir()
	src := mustGenerateAVFixture(t, dir, "src.mp4", "-c:v", "libx264", "-c:a", "aac")
	outPath := filepath.Join(dir, "out.webm")

	sink := &fakeAVProgressSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := (AVConverter{}).ConvertWithProgress(ctx, src, outPath, nil, sink); err != nil {
		t.Fatalf("ConvertWithProgress(mp4->webm) = %v, want nil", err)
	}

	if sink.durationCalls != 1 {
		t.Errorf("SourceDuration calls = %d, want exactly 1 (D-05)", sink.durationCalls)
	}
	if sink.duration <= 0 {
		t.Errorf("SourceDuration reported %v, want > 0", sink.duration)
	}
	last := -1
	for i, u := range sink.updates {
		if u.Pct < last {
			t.Errorf("updates[%d].Pct = %d, want >= previous %d (monotonic, D-02a)", i, u.Pct, last)
		}
		last = u.Pct
	}
}

// TestAVConverter_ConvertWithProgress_StreamCopyEligible_NoRamp_LiveBinary
// pins D-02's other half: an h264/aac mp4 source -> mp4 target with no
// resize/codec opts IS copy-eligible (TestAVStreamCopyEligible's "no opts
// copies" case) -- convertTranscodeWithProgress must delegate straight to
// the plain (non-streaming) convertTranscode path and emit ZERO
// sink.Update calls, while still reporting SourceDuration once.
func TestAVConverter_ConvertWithProgress_StreamCopyEligible_NoRamp_LiveBinary(t *testing.T) {
	requireLiveAVBinaries(t)
	dir := t.TempDir()
	src := mustGenerateAVFixture(t, dir, "src.mp4", "-c:v", "libx264", "-c:a", "aac")
	outPath := filepath.Join(dir, "out.mp4")

	sink := &fakeAVProgressSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := (AVConverter{}).ConvertWithProgress(ctx, src, outPath, nil, sink); err != nil {
		t.Fatalf("ConvertWithProgress(mp4->mp4, stream-copy-eligible) = %v, want nil", err)
	}

	if sink.durationCalls != 1 {
		t.Errorf("SourceDuration calls = %d, want exactly 1 (D-05)", sink.durationCalls)
	}
	if len(sink.updates) != 0 {
		t.Errorf("sink.updates = %v, want none -- a stream-copy remux performs no decode/encode to ramp (D-02)", sink.updates)
	}
}

// TestAVConverter_ConvertWithProgress_NullPaths_LiveBinary pins D-02's
// null-until-terminal paths: audio-extract and thumbnail both report
// SourceDuration once but emit ZERO sink.Update calls -- these operations
// are cheap enough that a ramp gains nothing and null->100 is honest and
// simpler.
func TestAVConverter_ConvertWithProgress_NullPaths_LiveBinary(t *testing.T) {
	requireLiveAVBinaries(t)
	dir := t.TempDir()
	src := mustGenerateAVFixture(t, dir, "src.mp4", "-c:v", "libx264", "-c:a", "aac")

	for _, target := range []string{"mp3", "jpg"} {
		t.Run(target, func(t *testing.T) {
			outPath := filepath.Join(dir, "out-"+target+"."+target)
			sink := &fakeAVProgressSink{}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := (AVConverter{}).ConvertWithProgress(ctx, src, outPath, nil, sink); err != nil {
				t.Fatalf("ConvertWithProgress(mp4->%s) = %v, want nil", target, err)
			}
			if sink.durationCalls != 1 {
				t.Errorf("SourceDuration calls = %d, want exactly 1 (D-05)", sink.durationCalls)
			}
			if len(sink.updates) != 0 {
				t.Errorf("sink.updates = %v, want none -- %s is a null-until-terminal path (D-02)", sink.updates, target)
			}
		})
	}
}
