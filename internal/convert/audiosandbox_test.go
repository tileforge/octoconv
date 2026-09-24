package convert

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The measured set, pinned. Every entry here is proven by
// .planning/research/SANDBOX-PATHS.md's "audio" section, conf05:audio:readonly,
// re-measured by plan 02 through the real job-path argv (the duration
// guard's ffprobe form on wav/m4a/video-container input, ffmpeg normalize,
// whisper-cli txt and json).
func TestAudioReadOnlySetIsTheMeasuredOne(t *testing.T) {
	want := []string{
		"/lib",
		"/usr/lib",
		"/usr/local/lib",
		"/etc/ld.so.cache",
		"/proc/cpuinfo",
		"/sys/devices/system/cpu/online",
		"/sys/devices/system/cpu/possible",
	}
	if len(audioReadOnly) != len(want) {
		t.Fatalf("audioReadOnly has %d paths, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(audioReadOnly), len(want))
	}
	for i := range want {
		if audioReadOnly[i] != want[i] {
			t.Errorf("path %d is %q, want %q", i, audioReadOnly[i], want[i])
		}
	}
	for _, p := range audioReadOnly {
		if !filepath.IsAbs(p) {
			t.Errorf("%q is not absolute; the helper grants paths verbatim", p)
		}
		if p == "/models" || filepath.Base(p) == "models" {
			t.Errorf("audioReadOnly contains %q -- the model is granted as a FILE at run time, never a static directory entry", p)
		}
	}
}

func TestAudioEnginesAreTheMeasuredOnes(t *testing.T) {
	want := []string{"ffmpeg", "ffprobe", "whisper-cli"}
	if len(audioEngines) != len(want) {
		t.Fatalf("audioEngines has %d entries, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(audioEngines), len(want))
	}
	for i := range want {
		if audioEngines[i] != want[i] {
			t.Errorf("audioEngines[%d] = %q, want %q", i, audioEngines[i], want[i])
		}
	}
}

// AudioConverter stays comparable: WithSandbox(nil) equals the zero value, a
// confined converter does not, and the type is usable as a map key -- fails
// to COMPILE if Sandbox stops being a pointer.
func TestAudioConverterStaysComparable(t *testing.T) {
	var a, b AudioConverter
	if a != b {
		t.Fatal("two zero converters differ")
	}
	if got := (AudioConverter{}).WithSandbox(nil); got != a {
		t.Error("WithSandbox(nil) does not equal the zero value")
	}

	confined := AudioConverter{}.WithSandbox([]string{"/usr/local/bin/audio-worker", "__sandbox-exec"})
	if confined == a {
		t.Error("a confined converter equals an unconfined one, so the field is not being compared at all")
	}
	seen := map[AudioConverter]bool{a: true}
	if !seen[b] {
		t.Error("the zero converter is not its own map key")
	}
}

// audioFakeEngines writes a fake ffmpeg (writes non-empty bytes to its last
// argument, "file:"-prefix stripped) and a fake whisper-cli (writes "{}" to
// the value after -of, plus ".json") into dir.
func audioFakeEngines(t *testing.T, dir string) {
	t.Helper()
	fakeEngine(t, dir, "ffmpeg", `last=""
for a in "$@"; do last="$a"; done
out="${last#file:}"
printf 'fake-normalized-wav-bytes' > "$out"
`)
	fakeEngine(t, dir, "whisper-cli", `prev=""
for a in "$@"; do
  if [ "$prev" = "-of" ]; then
    printf '{}' > "${a}.json"
  fi
  prev="$a"
done
`)
}

// roTokens extracts the ro: tokens (before the "--" separator) from one
// recorded helper invocation.
func roTokens(t *testing.T, argv []string) []string {
	t.Helper()
	sep := indexOf(argv, "--")
	if sep < 0 {
		t.Fatalf("invocation %v has no -- separator", argv)
	}
	var ro []string
	for _, tok := range argv[:sep] {
		if strings.HasPrefix(tok, "ro:") {
			ro = append(ro, strings.TrimPrefix(tok, "ro:"))
		}
	}
	return ro
}

// TestAudioConvertRunsBothStagesThroughTheHelper proves both of
// AudioConverter's stages run through the job's helper, with the model file
// granted to every invocation (not just whisper-cli's own).
func TestAudioConvertRunsBothStagesThroughTheHelper(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	audioFakeEngines(t, dir)
	pathWith(t, dir)

	jobDir := t.TempDir()
	inPath := filepath.Join(jobDir, "in.wav")
	if err := os.WriteFile(inPath, []byte("fake-wav-bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(jobDir, "out.json")

	c := AudioConverter{}.WithSandbox(helper)
	if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	invocations := calls()
	got := recordedEngineNames(t, invocations)
	want := []string{"ffmpeg", "whisper-cli"}
	if !equalStrings(got, want) {
		t.Fatalf("recorded engines = %v, want %v", got, want)
	}
	requireOnlyRW(t, invocations, jobDir)

	model := c.model()
	for _, argv := range invocations {
		ro := roTokens(t, argv)
		if indexOf(ro, model) < 0 {
			t.Errorf("invocation %v ro tokens = %v, missing the model file %q", argv, ro, model)
		}
	}
}

// TestAudioModelFileIsGrantedWhereverConfigured proves a non-default
// AUDIO_MODEL_PATH is granted as the exact file configured, not the server
// default -- the whisper-cli invocation carries ro:<that path> and the "-m"
// flag names it too.
func TestAudioModelFileIsGrantedWhereverConfigured(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	audioFakeEngines(t, dir)
	pathWith(t, dir)

	customModel := filepath.Join(t.TempDir(), "custom.bin")
	jobDir := t.TempDir()
	inPath := filepath.Join(jobDir, "in.wav")
	if err := os.WriteFile(inPath, []byte("fake-wav-bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(jobDir, "out.json")

	c := AudioConverter{modelPath: customModel}.WithSandbox(helper)
	if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	var whisperArgv []string
	for _, argv := range calls() {
		sep := indexOf(argv, "--")
		if sep >= 0 && sep+1 < len(argv) && filepath.Base(argv[sep+1]) == "whisper-cli" {
			whisperArgv = argv
		}
	}
	if whisperArgv == nil {
		t.Fatalf("no whisper-cli invocation recorded: %v", calls())
	}
	if ro := roTokens(t, whisperArgv); indexOf(ro, customModel) < 0 {
		t.Errorf("whisper-cli invocation ro tokens = %v, missing custom model %q", ro, customModel)
	}
	sep := indexOf(whisperArgv, "--")
	if !containsPair(whisperArgv[sep+1:], "-m", customModel) {
		t.Errorf("whisper-cli argv after -- = %v, want -m %q", whisperArgv[sep+1:], customModel)
	}
}

// TestEnforceMaxDurationRunsFFprobeThroughTheHelper proves the duration
// guard's ffprobe call runs through the SAME helper mechanism the converter
// uses (a helper is a caller-supplied argument here, not read from any
// converter field -- EnforceMaxDuration has no converter to read one from).
func TestEnforceMaxDurationRunsFFprobeThroughTheHelper(t *testing.T) {
	t.Run("under ceiling, confined", func(t *testing.T) {
		helper, calls := fakeSandboxHelper(t)
		dir := t.TempDir()
		fakeEngine(t, dir, "ffprobe", "printf '1.5\\n'\n")
		pathWith(t, dir)

		jobDir := t.TempDir()
		path := filepath.Join(jobDir, "in.wav")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		if err := EnforceMaxDuration(context.Background(), helper, path, time.Hour); err != nil {
			t.Fatalf("EnforceMaxDuration = %v, want nil", err)
		}

		invocations := calls()
		if len(invocations) != 1 {
			t.Fatalf("helper invoked %d time(s), want 1: %v", len(invocations), invocations)
		}
		requireOnlyRW(t, invocations, jobDir)
	})

	t.Run("over ceiling, confined", func(t *testing.T) {
		helper, _ := fakeSandboxHelper(t)
		dir := t.TempDir()
		fakeEngine(t, dir, "ffprobe", "printf '7200\\n'\n")
		pathWith(t, dir)

		jobDir := t.TempDir()
		path := filepath.Join(jobDir, "in.wav")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		err := EnforceMaxDuration(context.Background(), helper, path, time.Hour)
		if !errors.Is(err, ErrAudioDurationExceeded) {
			t.Fatalf("EnforceMaxDuration error = %v, want errors.Is ErrAudioDurationExceeded", err)
		}
	})

	t.Run("nil helper never invokes one", func(t *testing.T) {
		_, calls := fakeSandboxHelper(t) // creates the tracking file, but is never passed below
		dir := t.TempDir()
		fakeEngine(t, dir, "ffprobe", "printf '1.5\\n'\n")
		pathWith(t, dir)

		jobDir := t.TempDir()
		path := filepath.Join(jobDir, "in.wav")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		if err := EnforceMaxDuration(context.Background(), nil, path, time.Hour); err != nil {
			t.Fatalf("EnforceMaxDuration(nil helper) = %v, want nil", err)
		}
		if invocations := calls(); invocations != nil {
			t.Errorf("helper invoked %v times with a nil helper, want zero invocations", invocations)
		}
	})
}

// TestAudioPreflightWAV pins audioPreflightWAV's own byte shape: a correct
// RIFF/WAVE header, PCM format, 1 channel, 16000Hz, 16 bits, and exactly
// 32000 bytes of data (1.0s at that rate).
func TestAudioPreflightWAV(t *testing.T) {
	data := audioPreflightWAV()
	if len(data) < 44 {
		t.Fatalf("audioPreflightWAV() = %d bytes, want at least a 44-byte WAV header", len(data))
	}
	if string(data[0:4]) != "RIFF" {
		t.Errorf("chunk ID = %q, want RIFF", data[0:4])
	}
	if string(data[8:12]) != "WAVE" {
		t.Errorf("format = %q, want WAVE", data[8:12])
	}
	if string(data[12:16]) != "fmt " {
		t.Errorf("subchunk1 ID = %q, want \"fmt \"", data[12:16])
	}
	if audioFormat := binary.LittleEndian.Uint16(data[20:22]); audioFormat != 1 {
		t.Errorf("audio format = %d, want 1 (PCM)", audioFormat)
	}
	if channels := binary.LittleEndian.Uint16(data[22:24]); channels != 1 {
		t.Errorf("channels = %d, want 1", channels)
	}
	if sampleRate := binary.LittleEndian.Uint32(data[24:28]); sampleRate != 16000 {
		t.Errorf("sample rate = %d, want 16000", sampleRate)
	}
	if bitsPerSample := binary.LittleEndian.Uint16(data[34:36]); bitsPerSample != 16 {
		t.Errorf("bits per sample = %d, want 16", bitsPerSample)
	}
	if string(data[36:40]) != "data" {
		t.Errorf("subchunk2 ID = %q, want \"data\"", data[36:40])
	}
	dataLen := binary.LittleEndian.Uint32(data[40:44])
	if dataLen != 32000 {
		t.Errorf("data length = %d, want 32000 (1.0s at 16kHz/mono/16-bit)", dataLen)
	}
	if len(data) != 44+int(dataLen) {
		t.Errorf("total length = %d, want 44+%d = %d", len(data), dataLen, 44+int(dataLen))
	}
}

// TestAudioPreflightUnconfinedProvesNothing mirrors
// TestAVPreflightUnconfinedProvesNothing/TestOCRPreflightUnconfinedProvesNothing/
// TestPandocPreflightUnconfinedProvesNothing: nothing to prove for a
// deployment that has deliberately turned confinement off, and no workspace
// is even created for it.
func TestAudioPreflightUnconfinedProvesNothing(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "audio-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	c := AudioConverter{}
	if err := AudioPreflight(context.Background(), c); err != nil {
		t.Errorf("AudioPreflight on an unconfined converter: %v, want nil", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("AudioPreflight created a workspace (%v -> %v) though the converter is unconfined", before, after)
	}
}
