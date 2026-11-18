package convert

import "testing"

func TestAudioConverterPairs(t *testing.T) {
	got := AudioConverter{}.Pairs()
	if len(got) != 36 {
		t.Fatalf("Pairs() has %d entries, want 36", len(got))
	}
	seen := map[Pair]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("duplicate pair %+v", p)
		}
		seen[p] = true
	}
	for _, from := range []string{"mp3", "wav", "m4a", "ogg", "mp4", "mov", "avi", "mkv", "webm"} {
		for _, to := range []string{"txt", "srt", "vtt", "json"} {
			if !seen[Pair{From: from, To: to}] {
				t.Errorf("missing pair %s -> %s", from, to)
			}
		}
	}
}

func TestAudioConverterEngine(t *testing.T) {
	if got := (AudioConverter{}).Engine(); got != EngineAudio {
		t.Errorf("Engine() = %q, want %q", got, EngineAudio)
	}
}

func TestAudioRegisteredInDefault(t *testing.T) {
	engine, ok := Default.EngineFor("mp3", "txt")
	if !ok {
		t.Fatal("EngineFor(mp3, txt) ok=false, want true after AudioConverter registration")
	}
	if engine != EngineAudio {
		t.Errorf("EngineFor(mp3, txt) engine = %q, want %q", engine, EngineAudio)
	}
}

func TestAudioConvertRefuses(t *testing.T) {
	err := AudioConverter{}.Convert(t.Context(), "in.mp3", "out.txt", nil)
	if err == nil || err.Error() != "audio: the in-repo engine is retired" {
		t.Fatalf("Convert() = %v", err)
	}
}
