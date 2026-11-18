package convert

import (
	"context"
	"errors"
)

var (
	audioSourceFormats = []string{
		"mp3", "wav", "m4a", "ogg",
		"mp4", "mov", "avi", "mkv", "webm",
	}
	audioTargetFormats = []string{"txt", "srt", "vtt", "json"}
)

// AudioConverter admits the thirty-six transcription pairs. Conversion runs
// in tileforge/octoconv-worker-audio. Convert fails closed so this process
// cannot shell out to ffmpeg or whisper-cli.
type AudioConverter struct{}

// Pairs returns every source and target combination.
func (AudioConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(audioSourceFormats)*len(audioTargetFormats))
	for _, from := range audioSourceFormats {
		for _, to := range audioTargetFormats {
			pairs = append(pairs, Pair{From: from, To: to})
		}
	}
	return pairs
}

// Engine returns the audio engine class.
func (AudioConverter) Engine() string { return EngineAudio }

// Convert refuses the call. The in-repo audio process has been removed.
func (AudioConverter) Convert(context.Context, string, string, map[string]any) error {
	return errors.New("audio: the in-repo engine is retired")
}
