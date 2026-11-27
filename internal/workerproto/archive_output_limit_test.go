package workerproto

import (
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestArchiveOutputLimitHasSlack(t *testing.T) {
	const want int64 = (100 << 20) + (1 << 20)
	if got := protocolOutputLimit(convert.EngineArchive, "zip"); got != want {
		t.Fatalf("archive zip = %d, want %d", got, want)
	}
	if got := protocolOutputLimit(convert.EngineArchive, "ZIP"); got != want {
		t.Fatalf("archive ZIP = %d, want %d", got, want)
	}
	if got := protocolOutputLimit(convert.EngineArchive, "tar"); got != protocolDefaultOutputBytes {
		t.Fatalf("archive tar = %d, want %d", got, protocolDefaultOutputBytes)
	}
	if got := protocolOutputLimit(convert.EngineImage, "jpg"); got != protocolDefaultOutputBytes {
		t.Fatalf("image jpg = %d, want %d", got, protocolDefaultOutputBytes)
	}
}
