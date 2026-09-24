package convert

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The measured set, pinned. Every entry here is proven by
// .planning/research/SANDBOX-PATHS.md's "av" section, conf05:av:readonly,
// re-measured by plan 02 through the real job-path argv (three ffprobe
// forms, progress-wrapped and plain transcode, webm, hevc, audio-extract
// x3, thumbnail x3, and the AVPreflight lavfi source generator).
func TestAVReadOnlySetIsTheMeasuredOne(t *testing.T) {
	want := []string{
		"/lib",
		"/etc/ld.so.cache",
		"/dev/urandom",
		"/proc/self/status",
		"/proc/sys/vm/overcommit_memory",
		"/sys/devices/system/cpu/online",
		"/sys/devices/system/cpu/possible",
	}
	if len(avReadOnly) != len(want) {
		t.Fatalf("avReadOnly has %d paths, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(avReadOnly), len(want))
	}
	for i := range want {
		if avReadOnly[i] != want[i] {
			t.Errorf("path %d is %q, want %q", i, avReadOnly[i], want[i])
		}
	}
	for _, p := range avReadOnly {
		if !filepath.IsAbs(p) {
			t.Errorf("%q is not absolute; the helper grants paths verbatim", p)
		}
	}
}

func TestAVEnginesAreTheMeasuredOnes(t *testing.T) {
	want := []string{"ffmpeg", "ffprobe"}
	if len(avEngines) != len(want) {
		t.Fatalf("avEngines has %d entries, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(avEngines), len(want))
	}
	for i := range want {
		if avEngines[i] != want[i] {
			t.Errorf("avEngines[%d] = %q, want %q", i, avEngines[i], want[i])
		}
	}
}

// AVConverter stays comparable: WithSandbox(nil) equals the zero value, a
// confined converter does not, and the type is usable as a map key -- fails
// to COMPILE if Sandbox stops being a pointer.
func TestAVConverterStaysComparable(t *testing.T) {
	var a, b AVConverter
	if a != b {
		t.Fatal("two zero converters differ")
	}
	if got := (AVConverter{}).WithSandbox(nil); got != a {
		t.Error("WithSandbox(nil) does not equal the zero value")
	}

	confined := AVConverter{}.WithSandbox([]string{"/usr/local/bin/av-worker", "__sandbox-exec"})
	if confined == a {
		t.Error("a confined converter equals an unconfined one, so the field is not being compared at all")
	}
	seen := map[AVConverter]bool{a: true}
	if !seen[b] {
		t.Error("the zero converter is not its own map key")
	}
}

// ---- fakes shared by TestAVEveryEngineRunsThroughTheHelper ----

// avFakeProbe writes a fake ffprobe into dir: the "a:0" audio-codec-probe
// form (ffprobeAudioCodecArgs) prints nothing (a video-only source has no
// audio stream), the "format=duration" duration-probe form
// (ffprobeDurationArgs) prints "1.0", and every other form (the
// "-select_streams v" stream probe, ffprobeStreamArgs) prints a one-stream
// JSON answer: index 0, h264, 64x64, attached_pic 0.
func avFakeProbe(t *testing.T, dir string) {
	t.Helper()
	fakeEngine(t, dir, "ffprobe", `for a in "$@"; do
  if [ "$a" = "a:0" ]; then
    exit 0
  fi
  if [ "$a" = "format=duration" ]; then
    printf '1.0\n'
    exit 0
  fi
done
printf '{"streams":[{"index":0,"codec_name":"h264","width":64,"height":64,"disposition":{"attached_pic":0}}]}\n'
`)
}

// TestAVEveryEngineRunsThroughTheHelper proves every one of AV's two
// engines, across the plain Convert path and the progress-streaming
// ConvertWithProgress path, runs through the job's helper with a ruleset
// scoped to that job's own directory -- never a direct runCommand call and
// never a stray writable path.
func TestAVEveryEngineRunsThroughTheHelper(t *testing.T) {
	t.Run("Convert mp4 to mp3", func(t *testing.T) {
		helper, calls := fakeSandboxHelper(t)
		dir := t.TempDir()
		avFakeProbe(t, dir)
		fakeProgressFFmpeg(t, dir, "ffmpeg")
		pathWith(t, dir)

		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.mp4")
		if err := os.WriteFile(inPath, []byte("fake-mp4-bytes"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		outPath := filepath.Join(jobDir, "out.mp3")

		c := AVConverter{}.WithSandbox(helper)
		if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}

		invocations := calls()
		got := recordedEngineNames(t, invocations)
		// probeDuration -> probeVideoStreams -> probeAudioCodec (avProbeSource,
		// in that order) -> ffmpeg (the audio-extract stage; mp3 is never
		// stream-copy eligible so this is always a real re-encode).
		want := []string{"ffprobe", "ffprobe", "ffprobe", "ffmpeg"}
		if !equalStrings(got, want) {
			t.Fatalf("recorded engines = %v, want %v", got, want)
		}
		requireOnlyRW(t, invocations, jobDir)
	})

	t.Run("ConvertWithProgress mov to mp4", func(t *testing.T) {
		helper, calls := fakeSandboxHelper(t)
		dir := t.TempDir()
		avFakeProbe(t, dir)
		fakeProgressFFmpeg(t, dir, "ffmpeg")
		pathWith(t, dir)

		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.mov")
		if err := os.WriteFile(inPath, []byte("fake-mov-bytes"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		outPath := filepath.Join(jobDir, "out.mp4")

		c := AVConverter{}.WithSandbox(helper)
		sink := &avPreflightSink{}
		if err := c.ConvertWithProgress(context.Background(), inPath, outPath, nil, sink); err != nil {
			t.Fatalf("ConvertWithProgress: %v", err)
		}

		invocations := calls()
		got := recordedEngineNames(t, invocations)
		// The probed source (h264 video, no audio stream) is never
		// stream-copy eligible for an mp4 target (avStreamCopyLegal needs
		// aac audio too), so this is forced onto the streaming re-encode
		// branch -- exactly one ffmpeg, the same three-probe shape as above.
		want := []string{"ffprobe", "ffprobe", "ffprobe", "ffmpeg"}
		if !equalStrings(got, want) {
			t.Fatalf("recorded engines = %v, want %v", got, want)
		}
		requireOnlyRW(t, invocations, jobDir)

		// The FINAL invocation is ffmpeg's own streaming call: after the "--"
		// separator its argv starts with the ffmpeg path, THEN "-progress"
		// (T-68-18) -- the progress option is part of the ENGINE's own argv,
		// never the helper's, or helper-mode detection would break.
		last := invocations[len(invocations)-1]
		sep := indexOf(last, "--")
		if sep < 0 {
			t.Fatalf("no -- separator in %v", last)
		}
		rest := last[sep+1:]
		if len(rest) < 2 || filepath.Base(rest[0]) != "ffmpeg" || rest[1] != "-progress" {
			t.Fatalf("engine argv after -- = %v, want [<ffmpeg path>, -progress, ...]", rest)
		}
	})
}

// containsPair reports whether args contains flag immediately followed by
// value, anywhere in the slice.
func containsPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// TestAVPreflightSourceArgs pins avPreflightSourceArgs' own argv shape: it
// reads ONLY a lavfi testsrc (64x64, 1s) -- no second, audio, input -- and
// encodes libx264/yuv420p to "file:"+outPath.
func TestAVPreflightSourceArgs(t *testing.T) {
	got := avPreflightSourceArgs("/job/in.mov")

	if !containsPair(got, "-f", "lavfi") {
		t.Errorf("avPreflightSourceArgs = %v, want -f lavfi", got)
	}
	if !containsPair(got, "-i", "testsrc=size=64x64:rate=10:duration=1") {
		t.Errorf("avPreflightSourceArgs = %v, want an -i testsrc=size=64x64:rate=10:duration=1 lavfi source", got)
	}
	if !containsPair(got, "-c:v", "libx264") {
		t.Errorf("avPreflightSourceArgs = %v, want -c:v libx264", got)
	}
	if !containsPair(got, "-pix_fmt", "yuv420p") {
		t.Errorf("avPreflightSourceArgs = %v, want -pix_fmt yuv420p", got)
	}
	iCount := 0
	for _, tok := range got {
		if tok == "-i" {
			iCount++
		}
		if tok == "-c:a" || tok == "-an" {
			t.Errorf("avPreflightSourceArgs = %v, want no audio-related flag -- the source declares no audio stream at all", got)
		}
	}
	if iCount != 1 {
		t.Errorf("avPreflightSourceArgs has %d -i flag(s), want exactly 1 (no second, audio, input)", iCount)
	}
	if last := got[len(got)-1]; last != "file:/job/in.mov" {
		t.Errorf("avPreflightSourceArgs last element = %q, want %q", last, "file:/job/in.mov")
	}
}

// TestAVPreflightUnconfinedProvesNothing mirrors
// TestOCRPreflightUnconfinedProvesNothing/TestPandocPreflightUnconfinedProvesNothing:
// nothing to prove for a deployment that has deliberately turned confinement
// off, and no workspace is even created for it.
func TestAVPreflightUnconfinedProvesNothing(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "av-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	c := AVConverter{}
	if err := AVPreflight(context.Background(), c); err != nil {
		t.Errorf("AVPreflight on an unconfined converter: %v, want nil", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("AVPreflight created a workspace (%v -> %v) though the converter is unconfined", before, after)
	}
}
