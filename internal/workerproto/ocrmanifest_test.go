package workerproto

import (
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestOCRManifestMatchesOCRPairs(t *testing.T) {
	parsed := mustParseCadManifest(t, "manifest-ocr-convert.json")
	if parsed.id != "octoconv.convert.ocr" || parsed.legacyEngine != convert.EngineOCR || parsed.legacyOp != convert.OperationConvert || parsed.clientOperation != convert.OperationConvert {
		t.Fatalf("capability = %s op %s engine %s client %s", parsed.id, parsed.legacyOp, parsed.legacyEngine, parsed.clientOperation)
	}
	want := (convert.OCRConverter{}).Pairs()
	if len(parsed.formats) != len(want) {
		t.Fatalf("formats = %d, want %d", len(parsed.formats), len(want))
	}
	for i, format := range parsed.formats {
		if format.source != want[i].From || format.target != want[i].To {
			t.Fatalf("pair %d = %s to %s, want %s to %s", i, format.source, format.target, want[i].From, want[i].To)
		}
		engine, ok := convert.Default.EngineForOp(convert.OperationConvert, format.source, format.target)
		if !ok || engine != convert.EngineOCR {
			t.Fatalf("%s to %s engine %q ok %v", format.source, format.target, engine, ok)
		}
	}
}
