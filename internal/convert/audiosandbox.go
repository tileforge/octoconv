// Package convert (this file): audiosandbox.go is audio's half of the
// engineExec contract -- the measured grant, and the preflight that proves
// both converter stages AND the pre-Convert ffprobe duration guard
// (internal/worker) that reads the client's file before either of them does
// -- mirroring avsandbox.go's/ocrsandbox.go's own shape, for a class whose
// third engine call site (the duration guard) lives in a DIFFERENT Go
// package than the converter's own Sandbox field (orchestrator decision 1,
// 68-RESEARCH.md Pitfall 2): EnforceMaxDuration below is the one exported
// entry point internal/worker calls, and it shares this file's audioReadOnly/
// audioEngines with AudioConverter's own two stages so the guard and the
// converter can never disagree about what "confined" means for this class.
package convert

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// audioReadOnly is what ffmpeg/whisper-cli need to read, beyond their own
// binaries and the configured model FILE, MEASURED rather than reasoned --
// SANDBOX-PATHS.md's "audio" section, conf05:audio:readonly, re-measured by
// plan 02 through the real job-path argv (the duration guard's ffprobe form
// on wav/m4a/video-container input, ffmpeg normalize, whisper-cli txt and
// json). The model is deliberately NOT in this list: AUDIO_MODEL_PATH is
// configurable, so the model is granted as a single FILE at run time
// (whisper.go's Convert, AudioPreflight below) rather than baked into a
// static Go var that could not know a non-default path in advance.
var audioReadOnly = []string{
	"/lib",
	"/usr/lib",
	"/usr/local/lib",
	"/etc/ld.so.cache",
	"/proc/cpuinfo",
	"/sys/devices/system/cpu/online",
	"/sys/devices/system/cpu/possible",
}

// audioEngines is every binary this class's job-path invokes: ffmpeg
// (normalize) and whisper-cli (transcribe), the two converter-owned stages,
// plus ffprobe -- not called by AudioConverter itself, but by the
// pre-Convert duration guard (internal/worker, via EnforceMaxDuration
// below), which shares this same measured set and engine list so its own
// ruleset can never drift from the converter's.
var audioEngines = []string{"ffmpeg", "ffprobe", "whisper-cli"}

// WithSandbox returns c with Sandbox set to a *engineSandbox built from
// helper -- mirrors AVConverter's/OCRConverter's/PandocConverter's own
// shape: a method rather than a ConfinedAudio-style constructor, because
// AudioConverter also carries modelPath, and this lets cmd/audio-worker keep
// configuring it in one place: AudioConverter{...}.WithSandbox(helper).
func (c AudioConverter) WithSandbox(helper []string) AudioConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}

// audioPreflightWAVSeconds/audioPreflightSampleRate size the preflight's
// generated fixture: 1.0s at 16kHz mono 16-bit PCM is both a real, decodable
// WAV whisper-cli's ffmpeg-normalize stage passes straight through
// (already at the normalize target rate/format) and short enough that the
// preflight stays fast.
const (
	audioPreflightSampleRate = 16000
	audioPreflightSeconds    = 1.0
	audioPreflightToneHz     = 440.0
)

// audioPreflightWAV generates a 1.0-second, 16kHz mono 16-bit PCM WAV of a
// 440Hz sine tone -- a real, decodable audio fixture AudioPreflight can push
// through both converter stages and the duration guard under the real
// ruleset, without shipping a binary testdata file.
func audioPreflightWAV() []byte {
	const (
		channels      = 1
		bitsPerSample = 16
	)
	numSamples := int(audioPreflightSampleRate * audioPreflightSeconds)
	data := make([]byte, numSamples*channels*bitsPerSample/8)
	for i := 0; i < numSamples; i++ {
		v := math.Sin(2 * math.Pi * audioPreflightToneHz * float64(i) / float64(audioPreflightSampleRate))
		sample := int16(v * 32767 * 0.5)
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}

	byteRate := audioPreflightSampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16)) // PCM subchunk size
	binary.Write(&buf, binary.LittleEndian, uint16(1))  // audio format = PCM
	binary.Write(&buf, binary.LittleEndian, uint16(channels))
	binary.Write(&buf, binary.LittleEndian, uint32(audioPreflightSampleRate))
	binary.Write(&buf, binary.LittleEndian, uint32(byteRate))
	binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample))
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

// audioPreflightTranscript is the minimal shape AudioPreflight needs to
// prove whisper-cli's -ojf output is structurally a transcript, not merely
// non-empty bytes: whisper-cli v1.9.1 exits 0 with plausible-looking text
// even on silence (audioduration.go's own hallucination-on-silence caveat),
// so a content check that stops at "the file is non-empty" would prove
// nothing CONF-04 requires. This intentionally does NOT reuse
// whisper_test.go's audioTranscript (a _test.go type, unreachable from this
// production file).
type audioPreflightTranscript struct {
	Transcription []json.RawMessage `json:"transcription"`
}

// AudioPreflight proves the engine can actually be EXECUTED under the
// ruleset c would build for a job: the pre-Convert ffprobe duration guard
// (the SAME exported EnforceMaxDuration entry point internal/worker calls,
// under this same helper), both converter stages (ffmpeg normalize,
// whisper-cli transcribe), and a negative run -- before any job depends on
// it (CONF-04). Judged by output CONTENT at every step, never by exit status
// alone: whisper-cli exits 0 with plausible-looking JSON even on silence, so
// exit status and non-emptiness prove nothing on their own.
//
// nil when c is unconfined: nothing to prove for a deployment that has
// deliberately turned confinement off.
func AudioPreflight(ctx context.Context, c AudioConverter) error {
	if !c.Sandbox.confines() {
		return nil
	}

	w, cleanup, err := newPreflightWorkspace("audio")
	if err != nil {
		return err
	}
	defer cleanup()

	inside, outside, err := w.place("in.wav", audioPreflightWAV())
	if err != nil {
		return err
	}

	// The guard: the exact exported path internal/worker calls, under this
	// same helper, before the converter ever runs (orchestrator decision 1).
	// Its own spec grants the probed file's directory, so its denial cannot
	// be expressed through its own API -- EnforceMaxDuration always probes a
	// path it is handed, inside or outside. Its read-only set IS the audio
	// set whose effectiveness the converter's own negative run below proves.
	if err := EnforceMaxDuration(ctx, c.Sandbox.Helper, inside, time.Hour); err != nil {
		return fmt.Errorf("the duration guard cannot probe under the sandbox: %w", err)
	}
	d, err := probeDuration(ctx, c.Sandbox.forJob(w.jobDir, audioReadOnly, audioEngines), inside)
	if err != nil {
		return fmt.Errorf("the duration guard cannot probe under the sandbox: %w", err)
	}
	if d < 900*time.Millisecond || d > 1100*time.Millisecond {
		return fmt.Errorf("the duration guard cannot probe under the sandbox: probed %v, want ~1s", d)
	}

	// Sufficiency: the SAME converter value that will run production jobs
	// converts a file INSIDE the job directory -- both stages, ffmpeg
	// normalize then whisper-cli transcribe.
	outInside := filepath.Join(w.jobDir, "out.json")
	if err := c.Convert(ctx, inside, outInside, nil); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}
	normPath := filepath.Join(w.jobDir, "norm.wav")
	normBytes, err := os.ReadFile(normPath)
	if err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: read normalized output: %w", err)
	}
	if len(normBytes) <= 44 || !bytes.HasPrefix(normBytes, []byte("RIFF")) || !bytes.Contains(normBytes[:44], []byte("WAVE")) {
		return fmt.Errorf("the engine cannot convert under the sandbox: normalized output at %s is not a RIFF/WAVE file", normPath)
	}
	raw, err := os.ReadFile(outInside)
	if err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: read transcription output: %w", err)
	}
	var transcript audioPreflightTranscript
	if err := json.Unmarshal(raw, &transcript); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: whisper-cli output is not valid JSON: %w", err)
	}
	if transcript.Transcription == nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: whisper-cli JSON output has no top-level \"transcription\" array")
	}

	// Effectiveness: the IDENTICAL bytes, converted under the IDENTICAL
	// ruleset, from OUTSIDE the job directory. The positive run above already
	// proved the engine works; a failure here is attributable only to
	// location. norm.wav is removed first so a stale sufficiency-run
	// artifact cannot be mistaken for output the denied run produced.
	if err := os.Remove(normPath); err != nil {
		return fmt.Errorf("remove normalized output before the negative run: %w", err)
	}
	outOutside := filepath.Join(w.jobDir, "neg.json")
	convErr := c.Convert(ctx, outside, outOutside, nil)
	if err := requireDenied(convErr, outOutside, nil); err != nil {
		return fmt.Errorf("the engine read a file outside the job directory: %w", err)
	}
	return nil
}
