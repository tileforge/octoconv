package workerproto

import (
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestCadManifestsCoverTheEightPairs(t *testing.T) {
	convertParsed := mustParseCadManifest(t, "manifest-cad-convert.json")
	renderParsed := mustParseCadManifest(t, "manifest-cad-render.json")
	if convertParsed.id != "octoconv.convert.cad" || convertParsed.legacyEngine != convert.EngineCAD || convertParsed.legacyOp != convert.OperationConvert {
		t.Fatalf("convert capability = %s %s %s", convertParsed.id, convertParsed.legacyOp, convertParsed.legacyEngine)
	}
	if renderParsed.id != "octoconv.render.cad" || renderParsed.legacyEngine != convert.EngineCAD || renderParsed.legacyOp != convert.OperationRender {
		t.Fatalf("render capability = %s %s %s", renderParsed.id, renderParsed.legacyOp, renderParsed.legacyEngine)
	}
	assertPairs(t, convertParsed, convert.OperationConvert, map[string]string{
		"step\x00stl": "cad",
		"iges\x00stl": "cad",
		"dxf\x00svg":  "cad",
		"dxf\x00pdf":  "cad",
	})
	assertPairs(t, renderParsed, convert.OperationRender, map[string]string{
		"step\x00png": "cad",
		"iges\x00png": "cad",
		"stl\x00png":  "cad",
		"dxf\x00png":  "cad",
	})
	var measurements bool
	for _, port := range renderParsed.ports {
		if port.direction == "output" && port.role == "measurements" && port.min == 0 && port.max == 1 {
			measurements = true
		}
	}
	if !measurements {
		t.Fatal("render manifest missing optional measurements output")
	}
}

func mustParseCadManifest(t *testing.T, name string) parsedManifest {
	t.Helper()
	parsed, err := parseManifest(readPositiveFixture(t, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return parsed
}

func assertPairs(t *testing.T, parsed parsedManifest, operation string, want map[string]string) {
	t.Helper()
	if len(parsed.formats) != len(want) {
		t.Fatalf("%s formats = %d, want %d", parsed.id, len(parsed.formats), len(want))
	}
	got := map[string]struct{}{}
	for _, format := range parsed.formats {
		key := format.source + "\x00" + format.target
		got[key] = struct{}{}
		engine, ok := convert.Default.EngineForOp(operation, format.source, format.target)
		if !ok || engine != convert.EngineCAD {
			t.Fatalf("%s %s engine %q ok %v", operation, key, engine, ok)
		}
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Fatalf("%s missing %s", parsed.id, key)
		}
	}
}
