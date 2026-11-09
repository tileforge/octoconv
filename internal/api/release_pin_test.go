package api

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestReleasePin(t *testing.T) {
	raw, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal("openapi")
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]yaml.Node `yaml:"properties"`
				Required   []string             `yaml:"required"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal("openapi yaml")
	}
	schema, ok := doc.Components.Schemas["JobCreated"]
	if !ok {
		t.Fatal("JobCreated")
	}
	got := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"job_id", "opts", "status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JobCreated properties = %v", got)
	}
	if !reflect.DeepEqual(schema.Required, []string{"job_id", "status"}) {
		t.Fatalf("JobCreated required = %v", schema.Required)
	}
	for _, name := range []string{
		"capability_id", "current_step", "workflow", "download_url",
		"progress_pct", "correlation_id", "step_id",
	} {
		if _, ok := schema.Properties[name]; ok {
			t.Fatalf("JobCreated property %s", name)
		}
	}

	chart, err := os.ReadFile("../../deploy/chart/octoconv/templates/scaledobject-image.yaml")
	if err != nil {
		t.Fatal("scaled object")
	}
	chartText := string(chart)
	const query = `sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`
	if !strings.Contains(chartText, query) {
		t.Fatal("image query")
	}
	if strings.Contains(chartText, "job_id") || strings.Contains(chartText, "imagepdf") {
		t.Fatal("image scaler labels")
	}

	ceilings := map[string]int64{
		convert.EngineImage:    100 << 20,
		convert.EngineDocument: 100 << 20,
		convert.EngineHTML:     100 << 20,
		convert.EngineAudio:    100 << 20,
		convert.EngineOCR:      100 << 20,
		convert.EnginePandoc:   100 << 20,
		convert.EngineAV:       2 << 30,
		convert.EngineProbe:    50 << 20,
		convert.EngineTectonic: 2 << 20,
	}
	for engine, wantBytes := range ceilings {
		gotBytes, ok := defaultMaxEngineBytes[engine]
		if !ok || gotBytes != wantBytes {
			t.Fatalf("ceiling %s = %d ok=%v", engine, gotBytes, ok)
		}
	}
}
