// Package convert (this file): avsandbox.go is av's half of the engineExec
// contract -- the measured grant, the per-job ruleset builder, and (Task 3)
// the preflight that proves the streaming re-encode path -- mirroring
// pandocsandbox.go's/ocrsandbox.go's own shape, for a class with TWO
// engines (ffmpeg, ffprobe) and call sites split across two files (av.go,
// avduration.go) sharing ONE ruleset per job.
package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// avReadOnly is what ffmpeg/ffprobe need to read, beyond their own
// binaries, MEASURED rather than reasoned -- SANDBOX-PATHS.md's "av"
// section, conf05:av:readonly, re-measured by plan 02 through the full real
// job-path argv inventory (three ffprobe forms, progress-wrapped and plain
// transcode, webm, hevc, audio-extract x3, thumbnail x3, and the
// AVPreflight lavfi source generator) -- unchanged from the original
// 2026-09-20 measurement; no gap found for this class.
var avReadOnly = []string{
	"/lib",
	"/etc/ld.so.cache",
	"/dev/urandom",
	"/proc/self/status",
	"/proc/sys/vm/overcommit_memory",
	"/sys/devices/system/cpu/online",
	"/sys/devices/system/cpu/possible",
}

// avEngines is every binary AVConverter's job-path invokes: ffmpeg (every
// transcode/audio-extract/thumbnail/preflight-source-generation call) and
// ffprobe (every duration/stream/audio-codec probe). Each is resolved
// through PATH and added to the ruleset by forJob, because neither appears
// in a filesystem trace -- both are execve'd, not opened.
var avEngines = []string{"ffmpeg", "ffprobe"}

// exec builds this job's engineExec from outPath's directory -- the ONE
// ruleset shared by every ffmpeg/ffprobe call site of this job, whichever
// stage (probe, transcode, audio-extract, thumbnail) it ends up taking.
// Mirrors OCRConverter.exec's own shape: AV's call sites span av.go
// (transcode/audio-extract/thumbnail/the probe orchestrator) and
// avduration.go (the stream probes themselves), all of which must share the
// SAME engineExec for one job rather than each rebuilding it.
func (c AVConverter) exec(outPath string) engineExec {
	return c.Sandbox.forJob(filepath.Dir(outPath), avReadOnly, avEngines)
}

// WithSandbox returns c with Sandbox set to a *engineSandbox built from
// helper -- mirrors OCRConverter.WithSandbox's/PandocConverter.WithSandbox's
// own shape: a method rather than a ConfinedAV-style constructor, because
// AVConverter also carries MaxSourceDuration/MaxSourceResolutionHeight, and
// this lets cmd/av-worker keep configuring it in one place:
// buildAVConverter(...).WithSandbox(helper).
func (c AVConverter) WithSandbox(helper []string) AVConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}

// avPreflightSourceArgs builds ffmpeg's argv for the preflight's own source
// generation: a 1-second, 64x64 lavfi testsrc pattern, encoded h264/yuv420p,
// with NO audio stream at all -- deliberately, so avStreamCopyLegal (av.go)
// can never find the mp4/h264+aac contract satisfied and the positive run
// below is FORCED onto the full re-encode branch (convertTranscodeWithProgress's
// streaming path, its progress FIFO included), the path a real job actually
// takes, not a vacuous stream-copy remux (CONF-04).
func avPreflightSourceArgs(outPath string) []string {
	return []string{
		"-y", "-nostdin", "-protocol_whitelist", "file,crypto",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=10:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"file:" + outPath,
	}
}

// avPreflightSink implements ProgressSink (progress.go), recording only what
// AVPreflight needs to judge: the source duration reported once, and how
// many Update calls arrived -- proof the progress FIFO actually delivered
// ticks under the ruleset, not merely that the conversion succeeded.
type avPreflightSink struct {
	sourceDuration float64
	updates        int
}

func (s *avPreflightSink) SourceDuration(seconds float64) { s.sourceDuration = seconds }
func (s *avPreflightSink) Update(ProgressUpdate)          { s.updates++ }

// AVPreflight proves the engine can actually be EXECUTED, under the ruleset
// c would build for a job, along the path a real job takes -- the streaming
// re-encode, progress FIFO included, not a stream-copy remux or a bare probe
// (CONF-04) -- plus a negative run, before any job depends on it. Judged by
// output CONTENT (a real h264 64x64 stream reaches the far side, and the
// sink actually received ticks), never by exit status alone.
//
// nil when c is unconfined: nothing to prove for a deployment that has
// deliberately turned confinement off.
func AVPreflight(ctx context.Context, c AVConverter) error {
	if !c.Sandbox.confines() {
		return nil
	}

	w, cleanup, err := newPreflightWorkspace("av")
	if err != nil {
		return err
	}
	defer cleanup()

	// Generate the source INSIDE the job directory, through the SAME ruleset
	// a real job's conversion stage would use -- proves the generator itself
	// runs confined, not merely that its output is later usable.
	genPath := filepath.Join(w.jobDir, "in.mov")
	if _, err := c.exec(genPath).run(ctx, "ffmpeg", avPreflightSourceArgs(genPath)...); err != nil {
		return fmt.Errorf("generate preflight source: %w", err)
	}
	srcBytes, err := os.ReadFile(genPath)
	if err != nil {
		return fmt.Errorf("read preflight source: %w", err)
	}
	inside, outside, err := w.place("in.mov", srcBytes)
	if err != nil {
		return err
	}

	// Sufficiency: the SAME converter value that will run production jobs
	// streams the re-encode INSIDE the job directory -- video-only h264
	// source, so avStreamCopyEligible is false and the full re-encode branch
	// (with its progress FIFO) runs, not a remux.
	outInside := filepath.Join(w.jobDir, "out.mp4")
	sink := &avPreflightSink{}
	if err := c.ConvertWithProgress(ctx, inside, outInside, nil, sink); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}
	if sink.sourceDuration < 0.8 || sink.sourceDuration > 1.2 {
		return fmt.Errorf("the engine cannot convert under the sandbox: reported source duration %.3fs, want ~1s", sink.sourceDuration)
	}
	if sink.updates < 1 {
		return fmt.Errorf("the engine cannot convert under the sandbox: zero progress updates -- the FIFO did not deliver a tick")
	}
	if err := requireOutput(outInside, nil); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}
	streams, err := probeVideoStreams(ctx, c.exec(outInside), outInside)
	if err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: probe output: %w", err)
	}
	if len(streams) != 1 || streams[0].CodecName != "h264" || streams[0].Width != 64 || streams[0].Height != 64 {
		return fmt.Errorf("the engine cannot convert under the sandbox: output streams = %+v, want exactly one 64x64 h264 stream", streams)
	}

	// Effectiveness: the IDENTICAL bytes, converted under the IDENTICAL
	// ruleset, from OUTSIDE the job directory. The positive run above already
	// proved the engine works; a failure here is attributable only to
	// location.
	outOutside := filepath.Join(w.jobDir, "neg.mp4")
	convErr := c.ConvertWithProgress(ctx, outside, outOutside, nil, &avPreflightSink{})
	if err := requireDenied(convErr, outOutside, nil); err != nil {
		return fmt.Errorf("the engine read a file outside the job directory: %w", err)
	}
	return nil
}
