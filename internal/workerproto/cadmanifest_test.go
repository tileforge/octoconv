package workerproto

import (
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestCadManifestsCoverTheCADPairs(t *testing.T) {
	modelConvert := mustParseCadManifest(t, "manifest-cad-convert.json")
	modelRender := mustParseCadManifest(t, "manifest-cad-render.json")
	drawConvert := mustParseCadManifest(t, "manifest-caddraw-convert.json")
	drawRender := mustParseCadManifest(t, "manifest-caddraw-render.json")
	assertCadCapability(t, modelConvert, "octoconv.convert.cad", convert.OperationConvert)
	assertCadCapability(t, modelRender, "octoconv.render.cad", convert.OperationRender)
	assertCadCapability(t, drawConvert, "octoconv.convert.caddraw", convert.OperationConvert)
	assertCadCapability(t, drawRender, "octoconv.render.caddraw", convert.OperationRender)
	assertPairs(t, modelConvert, convert.OperationConvert, map[string]string{
		"step\x00stl": "cad",
		"iges\x00stl": "cad",
	})
	assertPairs(t, drawConvert, convert.OperationConvert, map[string]string{
		"dxf\x00svg": "cad",
		"dxf\x00pdf": "cad",
		"dwg\x00svg": "cad",
		"dwg\x00pdf": "cad",
		"dwg\x00png": "cad",
	})
	assertPairs(t, modelRender, convert.OperationRender, map[string]string{
		"step\x00png": "cad",
		"iges\x00png": "cad",
		"stl\x00png":  "cad",
	})
	assertPairs(t, drawRender, convert.OperationRender, map[string]string{
		"dxf\x00png": "cad",
		"dwg\x00png": "cad",
	})
	assertOptionalMeasurements(t, modelRender)
	assertOptionalMeasurements(t, drawRender)
	seen := map[string]string{}
	for _, parsed := range []parsedManifest{modelConvert, modelRender, drawConvert, drawRender} {
		for _, format := range parsed.formats {
			key := parsed.legacyOp + "\x00" + format.source + "\x00" + format.target
			if prev, ok := seen[key]; ok {
				t.Fatalf("pair %s claimed by %s and %s", key, prev, parsed.id)
			}
			seen[key] = parsed.id
		}
	}
	if len(seen) != 12 {
		t.Fatalf("covered pairs = %d, want 12", len(seen))
	}
}

func assertCadCapability(t *testing.T, parsed parsedManifest, id, operation string) {
	t.Helper()
	if parsed.id != id || parsed.legacyEngine != convert.EngineCAD || parsed.legacyOp != operation {
		t.Fatalf("capability = %s %s %s, want %s %s %s", parsed.id, parsed.legacyOp, parsed.legacyEngine, id, operation, convert.EngineCAD)
	}
}

func assertOptionalMeasurements(t *testing.T, parsed parsedManifest) {
	t.Helper()
	for _, port := range parsed.ports {
		if port.direction == "output" && port.role == "measurements" && port.min == 0 && port.max == 1 {
			return
		}
	}
	t.Fatalf("%s missing optional measurements output", parsed.id)
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
