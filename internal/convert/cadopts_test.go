package convert

import (
	"strings"
	"testing"
)

func TestParseCADOptsAccepts(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want CADOpts
	}{
		{"empty", `{}`, CADOpts{}},
		{"view", `{"view":"front"}`, CADOpts{View: "front"}},
		{"quality", `{"quality":"fine"}`, CADOpts{Quality: "fine"}},
		{"background", `{"background":"transparent"}`, CADOpts{Background: "transparent"}},
		{"unit", `{"assume_unit":"inch"}`, CADOpts{AssumeUnit: "inch"}},
		{"size min", `{"size":256}`, CADOpts{Size: cadIntPtr(256)}},
		{"size max", `{"size":2048}`, CADOpts{Size: cadIntPtr(2048)}},
		{"size plain", `{"size":1280}`, CADOpts{Size: cadIntPtr(1280)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCADOpts([]byte(tc.raw))
			if err != nil {
				t.Fatalf("ParseCADOpts(%s) = %v", tc.raw, err)
			}
			if got.View != tc.want.View || got.Quality != tc.want.Quality || got.Background != tc.want.Background || got.AssumeUnit != tc.want.AssumeUnit {
				t.Errorf("ParseCADOpts(%s) = %+v, want %+v", tc.raw, got, tc.want)
			}
			if (got.Size == nil) != (tc.want.Size == nil) || (got.Size != nil && *got.Size != *tc.want.Size) {
				t.Errorf("ParseCADOpts(%s) size = %v, want %v", tc.raw, got.Size, tc.want.Size)
			}
		})
	}
}

func TestParseCADOptsAcceptsEveryClosedValueAndAllKeys(t *testing.T) {
	for v := range cadViews {
		if _, err := ParseCADOpts([]byte(`{"view":"` + v + `"}`)); err != nil {
			t.Errorf("view %q: %v", v, err)
		}
	}
	for v := range cadUnits {
		if _, err := ParseCADOpts([]byte(`{"assume_unit":"` + v + `"}`)); err != nil {
			t.Errorf("assume_unit %q: %v", v, err)
		}
	}
	o, err := ParseCADOpts([]byte(`{"view":"left","size":512,"quality":"draft","background":"opaque","assume_unit":"cm"}`))
	if err != nil || o.View != "left" || *o.Size != 512 || o.Quality != "draft" || o.Background != "opaque" || o.AssumeUnit != "cm" {
		t.Errorf("all five keys = %+v, %v", o, err)
	}
}

func TestParseCADOptsRejects(t *testing.T) {
	for _, raw := range []string{
		`{"unknown":1}`,
		`{"view":"diagonal"}`,
		`{"view":""}`,
		`{"view":null}`,
		`{"view":1}`,
		`{"quality":"ultra"}`,
		`{"background":"red"}`,
		`{"assume_unit":"parsec"}`,
		`{"size":0}`,
		`{"size":255}`,
		`{"size":2049}`,
		`{"size":1280.5}`,
		`{"size":1280.0}`,
		`{"size":"1280"}`,
		`{"size":null}`,
		`{"size":1e3}`,
		`null`,
		`[]`,
		`{"view":"iso","view":"top"}`,
		`{"view":"iso"} {}`,
		`{"view":"iso"}x`,
		``,
	} {
		if _, err := ParseCADOpts([]byte(raw)); err == nil {
			t.Errorf("ParseCADOpts(%q) accepted", raw)
		}
	}
}

func TestCADOptsFromMapRoundTripsStoredShape(t *testing.T) {
	in := CADOpts{View: "top", Size: cadIntPtr(1024), Background: "transparent", AssumeUnit: "m"}
	stored := map[string]any{"view": "top", "size": float64(1024), "background": "transparent", "assume_unit": "m"}
	got, err := CADOptsFromMap(stored)
	if err != nil {
		t.Fatalf("CADOptsFromMap = %v", err)
	}
	if got.View != in.View || *got.Size != *in.Size || got.Background != in.Background || got.AssumeUnit != in.AssumeUnit {
		t.Errorf("CADOptsFromMap = %+v, want %+v", got, in)
	}
	if _, err := CADOptsFromMap(map[string]any{"size": 1280.5}); err == nil {
		t.Error("a fractional stored size was accepted")
	}
}

func TestCADOptsResolvedDefaults(t *testing.T) {
	r := CADOpts{}.Resolved()
	if r.View != "iso" || *r.Size != 1280 || r.Quality != "standard" || r.Background != "opaque" || r.AssumeUnit != "" {
		t.Errorf("Resolved() = %+v", r)
	}
	n := 512
	r = CADOpts{View: "top", Size: &n, Quality: "fine", Background: "transparent"}.Resolved()
	if r.View != "top" || *r.Size != 512 || r.Quality != "fine" || r.Background != "transparent" {
		t.Errorf("Resolved() overwrote a requested value: %+v", r)
	}
}

func TestValidateCADApplicability(t *testing.T) {
	all := CADOpts{View: "top", Size: cadIntPtr(512), Quality: "fine", Background: "opaque", AssumeUnit: "mm"}
	noQuality := all
	noQuality.Quality = ""
	cases := []struct {
		name                       string
		engine, op, source, target string
		o                          CADOpts
		wantErr                    string
	}{
		{"zero opts always apply", EngineCAD, OperationConvert, "step", "stl", CADOpts{}, ""},
		{"zero opts on render", EngineCAD, OperationRender, "stl", "png", CADOpts{}, ""},
		{"quality on convert", EngineCAD, OperationConvert, "step", "stl", CADOpts{Quality: "draft"}, ""},
		{"view on convert", EngineCAD, OperationConvert, "step", "stl", CADOpts{View: "top"}, "only quality"},
		{"size on convert", EngineCAD, OperationConvert, "step", "stl", CADOpts{Size: cadIntPtr(512)}, "only quality"},
		{"unit on convert", EngineCAD, OperationConvert, "step", "stl", CADOpts{AssumeUnit: "mm"}, "only quality"},
		{"quality on stl render", EngineCAD, OperationRender, "stl", "png", CADOpts{Quality: "fine"}, "stl source"},
		{"all five on step render", EngineCAD, OperationRender, "step", "png", all, ""},
		{"all five on stp alias render", EngineCAD, OperationRender, "stp", "png", all, ""},
		{"all but quality on stl render", EngineCAD, OperationRender, "stl", "png", noQuality, ""},
		{"wrong engine", EngineImage, OperationConvert, "step", "stl", CADOpts{Quality: "draft"}, "cad-engine"},
		{"unknown operation", EngineCAD, "sanitize", "step", "stl", CADOpts{Quality: "draft"}, "operation"},
		{"view on dxf render", EngineCAD, OperationRender, "dxf", "png", CADOpts{View: "top"}, "view does not apply to a dxf source"},
		{"quality on dxf render", EngineCAD, OperationRender, "dxf", "png", CADOpts{Quality: "fine"}, "quality does not apply to a dxf source"},
		{"view on an upper-case DXF render", EngineCAD, OperationRender, "DXF", "png", CADOpts{View: "top"}, "view does not apply to a dxf source"},
		{"quality on a dotted dxf render", EngineCAD, OperationRender, ".dxf", "png", CADOpts{Quality: "draft"}, "quality does not apply to a dxf source"},
		{"size, background and unit on dxf render", EngineCAD, OperationRender, "dxf", "png", CADOpts{Size: cadIntPtr(1024), Background: "transparent", AssumeUnit: "mm"}, ""},
		{"size alone on dxf render", EngineCAD, OperationRender, "dxf", "png", CADOpts{Size: cadIntPtr(256)}, ""},
		{"zero opts on dxf render", EngineCAD, OperationRender, "dxf", "png", CADOpts{}, ""},
		{"zero opts on dxf to svg", EngineCAD, OperationConvert, "dxf", "svg", CADOpts{}, ""},
		{"zero opts on dxf to pdf", EngineCAD, OperationConvert, "dxf", "pdf", CADOpts{}, ""},
	}
	for _, target := range []string{"svg", "pdf"} {
		for name, o := range map[string]CADOpts{
			"view":        {View: "top"},
			"size":        {Size: cadIntPtr(512)},
			"quality":     {Quality: "standard"},
			"background":  {Background: "opaque"},
			"assume_unit": {AssumeUnit: "mm"},
		} {
			cases = append(cases, struct {
				name                       string
				engine, op, source, target string
				o                          CADOpts
				wantErr                    string
			}{name + " on dxf to " + target, EngineCAD, OperationConvert, "dxf", target, o, "no option applies to a dxf conversion"})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCADApplicability(tc.engine, tc.op, tc.source, tc.target, tc.o)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("got %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func cadIntPtr(n int) *int { return &n }
