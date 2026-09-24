// Package convert (this file): avsandbox.go is av's half of the engineExec
// contract -- the measured grant, the per-job ruleset builder, and (Task 3)
// the preflight that proves the streaming re-encode path -- mirroring
// pandocsandbox.go's/ocrsandbox.go's own shape, for a class with TWO
// engines (ffmpeg, ffprobe) and call sites split across two files (av.go,
// avduration.go) sharing ONE ruleset per job.
package convert

import (
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
