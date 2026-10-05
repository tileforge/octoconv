package convert

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/measurements"
)

func intp(v int) *int           { return &v }
func floatp(v float64) *float64 { return &v }

func nearRel(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6*math.Abs(b)
}

func unitsInchDraft() cadMeasureDraft {
	return cadMeasureDraft{
		Format:        "step",
		DeclaredUnit:  "inch",
		AppliedFactor: floatp(25.4),
		BBox:          measurements.BBox{Min: []float64{0, 0, 0}, Max: []float64{50.8, 76.19999999999999, 25.4}},
		Solids:        intp(1),
		ClosedSolids:  intp(1),
		Volume:        floatp(98322.38399999998),
		Area:          floatp(14193.519999999997),
		Triangles:     12,
	}
}

func mmDraft() cadMeasureDraft {
	return cadMeasureDraft{
		Format:        "step",
		DeclaredUnit:  "mm",
		AppliedFactor: floatp(1),
		BBox:          measurements.BBox{Min: []float64{-7.5, -12, -7.5}, Max: []float64{7.5, 12, 7.5}},
		Solids:        intp(2),
		ClosedSolids:  intp(2),
		Volume:        floatp(2833.4178604351528),
		Area:          floatp(3871.1170419338255),
		Triangles:     1644,
	}
}

func extents(d measurements.Document) []float64 {
	out := make([]float64, len(d.BBox.Min))
	for i := range out {
		out[i] = d.BBox.Max[i] - d.BBox.Min[i]
	}
	slices.Sort(out)
	return out
}

func TestBuildCADMeasurementsInchDeclared(t *testing.T) {
	for _, assume := range []string{"", "mm"} {
		doc, err := buildCADMeasurements(unitsInchDraft(), assume)
		if err != nil {
			t.Fatalf("assume %q: %v", assume, err)
		}
		if doc.Unit != measurements.UnitInch || doc.UnitSource != measurements.SourceDeclared {
			t.Fatalf("assume %q: unit %s source %s, want inch declared", assume, doc.Unit, doc.UnitSource)
		}
		for i, want := range []float64{1, 2, 3} {
			if got := extents(doc)[i]; !nearRel(got, want) {
				t.Fatalf("assume %q: extents %v, want 1 2 3", assume, extents(doc))
			}
		}
		if doc.Volume == nil || !nearRel(*doc.Volume, 6) {
			t.Fatalf("assume %q: volume %v, want 6", assume, doc.Volume)
		}
		if doc.Area == nil || !nearRel(*doc.Area, 22) {
			t.Fatalf("assume %q: area %v, want 22", assume, doc.Area)
		}
		if doc.Solids == nil || *doc.Solids != 1 || doc.ClosedSolids == nil || *doc.ClosedSolids != 1 {
			t.Fatalf("assume %q: solid counts %v %v", assume, doc.Solids, doc.ClosedSolids)
		}
	}
}

func TestBuildCADMeasurementsMillimetreUnchanged(t *testing.T) {
	d := mmDraft()
	doc, err := buildCADMeasurements(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Unit != measurements.UnitMM || doc.UnitSource != measurements.SourceDeclared {
		t.Fatalf("unit %s source %s, want mm declared", doc.Unit, doc.UnitSource)
	}
	if *doc.Volume != *d.Volume || *doc.Area != *d.Area {
		t.Fatalf("mm values changed: volume %v area %v", *doc.Volume, *doc.Area)
	}
	if !slices.Equal(doc.BBox.Min, d.BBox.Min) || !slices.Equal(doc.BBox.Max, d.BBox.Max) {
		t.Fatalf("mm bbox changed: %v %v", doc.BBox.Min, doc.BBox.Max)
	}
}

func TestBuildCADMeasurementsUndeclaredStep(t *testing.T) {
	d := mmDraft()
	d.DeclaredUnit = ""
	doc, err := buildCADMeasurements(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Unit != measurements.UnitUnspecified || doc.UnitSource != measurements.SourceNone {
		t.Fatalf("unit %s source %s, want unspecified none", doc.Unit, doc.UnitSource)
	}
	doc, err = buildCADMeasurements(d, "cm")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Unit != measurements.UnitCM || doc.UnitSource != measurements.SourceClientAssumed {
		t.Fatalf("unit %s source %s, want cm client_assumed", doc.Unit, doc.UnitSource)
	}
	if *doc.Volume != *d.Volume || *doc.Area != *d.Area {
		t.Fatalf("assumed unit rescaled the values: volume %v area %v", *doc.Volume, *doc.Area)
	}
}

func TestBuildCADMeasurementsUnverifiableFactorIsUnspecified(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(d *cadMeasureDraft)
		assume  string
		scaleBy float64
	}{
		{"declared inch with no applied factor", func(d *cadMeasureDraft) { d.AppliedFactor = nil }, "", 1},
		{"declared inch with a factor of 10", func(d *cadMeasureDraft) { d.AppliedFactor = floatp(10) }, "", 10},
		{"declared inch with a factor a hair off the table", func(d *cadMeasureDraft) { d.AppliedFactor = floatp(25.4 * (1 + 2e-9)) }, "", 25.4},
		{"unit text outside the vocabulary", func(d *cadMeasureDraft) { d.DeclaredUnit, d.DeclaredUnitRaw = "", "furlong" }, "", 25.4},
		{"unit text outside the vocabulary does not take an assumed unit", func(d *cadMeasureDraft) { d.DeclaredUnit, d.DeclaredUnitRaw = "", "mixed" }, "mm", 25.4},
		{"declared unit the table does not know", func(d *cadMeasureDraft) { d.DeclaredUnit = "furlong" }, "", 25.4},
		{"iges inch whose scaling is unproven", func(d *cadMeasureDraft) { d.Format, d.AppliedFactor = "iges", nil }, "mm", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := unitsInchDraft()
			tc.mutate(&d)
			doc, err := buildCADMeasurements(d, tc.assume)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Unit != measurements.UnitUnspecified || doc.UnitSource != measurements.SourceNone {
				t.Fatalf("unit %s source %s, want unspecified none", doc.Unit, doc.UnitSource)
			}
			if want := 98322.38399999998 / math.Pow(tc.scaleBy, 3); !nearRel(*doc.Volume, want) {
				t.Fatalf("volume %v, want %v", *doc.Volume, want)
			}
		})
	}
}

func TestBuildCADMeasurementsSTL(t *testing.T) {
	d := cadMeasureDraft{
		Format:    "stl",
		BBox:      measurements.BBox{Min: []float64{0, 0, 0}, Max: []float64{10, 20, 30}},
		Volume:    floatp(6000),
		Area:      floatp(2200),
		Triangles: 12,
	}
	doc, err := buildCADMeasurements(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Unit != measurements.UnitUnspecified || doc.UnitSource != measurements.SourceNone {
		t.Fatalf("unit %s source %s, want unspecified none", doc.Unit, doc.UnitSource)
	}
	if doc.Solids != nil || doc.ClosedSolids != nil {
		t.Fatalf("an stl document carries solid counts: %v %v", doc.Solids, doc.ClosedSolids)
	}
	if *doc.Volume != 6000 || *doc.Area != 2200 || doc.BBox.Max[2] != 30 {
		t.Fatalf("stl values changed: %v %v %v", *doc.Volume, *doc.Area, doc.BBox.Max)
	}
	doc, err = buildCADMeasurements(d, "inch")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Unit != measurements.UnitInch || doc.UnitSource != measurements.SourceClientAssumed || *doc.Volume != 6000 {
		t.Fatalf("assumed stl: unit %s source %s volume %v", doc.Unit, doc.UnitSource, *doc.Volume)
	}
	d.DeclaredUnit, d.AppliedFactor = "inch", floatp(25.4)
	doc, err = buildCADMeasurements(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.UnitSource == measurements.SourceDeclared {
		t.Fatal("an stl was reported as declared")
	}
}

func TestBuildCADMeasurementsNullReasons(t *testing.T) {
	box := measurements.BBox{Min: []float64{0, 0, 0}, Max: []float64{2, 3, 1}}
	cases := []struct {
		name string
		d    cadMeasureDraft
	}{
		{"no_solids", cadMeasureDraft{Format: "step", DeclaredUnit: "mm", AppliedFactor: floatp(1), BBox: box, Solids: intp(0), ClosedSolids: intp(0), NullReason: "no_solids"}},
		{"open_shell", cadMeasureDraft{Format: "iges", DeclaredUnit: "mm", AppliedFactor: floatp(1), BBox: box, Solids: intp(2), ClosedSolids: intp(0), NullReason: "open_shell"}},
		{"invalid_solid", cadMeasureDraft{Format: "step", DeclaredUnit: "mm", AppliedFactor: floatp(1), BBox: box, Solids: intp(1), ClosedSolids: intp(0), NullReason: "invalid_solid"}},
		{"negative_volume", cadMeasureDraft{Format: "step", DeclaredUnit: "mm", AppliedFactor: floatp(1), BBox: box, Solids: intp(1), ClosedSolids: intp(1), NullReason: "negative_volume"}},
		{"stl_not_watertight", cadMeasureDraft{Format: "stl", BBox: box, NullReason: "stl_not_watertight"}},
		{"stl invalid_solid", cadMeasureDraft{Format: "stl", BBox: box, NullReason: "invalid_solid"}},
		{"stl negative_volume", cadMeasureDraft{Format: "stl", BBox: box, NullReason: "negative_volume"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := buildCADMeasurements(tc.d, "")
			if err != nil {
				t.Fatal(err)
			}
			if doc.Volume != nil || doc.Area != nil {
				t.Fatalf("volume %v area %v, want both null", doc.Volume, doc.Area)
			}
			if doc.VolumeNullReason != tc.d.NullReason || doc.AreaNullReason != tc.d.NullReason {
				t.Fatalf("reasons %q %q, want %q", doc.VolumeNullReason, doc.AreaNullReason, tc.d.NullReason)
			}
			if err := doc.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildCADMeasurementsRefusesWhatWouldNotValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(d *cadMeasureDraft)
		assume string
	}{
		{"zero volume", func(d *cadMeasureDraft) { d.Volume = floatp(0) }, ""},
		{"negative area", func(d *cadMeasureDraft) { d.Area = floatp(-1) }, ""},
		{"volume without area", func(d *cadMeasureDraft) { d.Area = nil; d.NullReason = "no_solids" }, ""},
		{"number beside a reason", func(d *cadMeasureDraft) { d.NullReason = "negative_volume" }, ""},
		{"null without a reason", func(d *cadMeasureDraft) { d.Volume, d.Area = nil, nil }, ""},
		{"closed above solids", func(d *cadMeasureDraft) { d.ClosedSolids = intp(2) }, ""},
		{"missing solid counts", func(d *cadMeasureDraft) { d.Solids = nil }, ""},
		{"a dxf draft", func(d *cadMeasureDraft) { d.Format = "dxf" }, ""},
		{"a point bbox", func(d *cadMeasureDraft) { d.BBox = measurements.BBox{Min: []float64{1, 1, 1}, Max: []float64{1, 1, 1}} }, ""},
		{"a zero factor", func(d *cadMeasureDraft) { d.AppliedFactor = floatp(0) }, ""},
		{"an unknown assumed unit", func(d *cadMeasureDraft) {}, "furlong"},
		{"unspecified as the assumed unit", func(d *cadMeasureDraft) {}, "unspecified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := unitsInchDraft()
			tc.mutate(&d)
			doc, err := buildCADMeasurements(d, tc.assume)
			if err == nil {
				t.Fatalf("returned a document: %+v", doc)
			}
			if !errors.Is(err, ErrCADMeasure) {
				t.Fatalf("error does not wrap ErrCADMeasure: %v", err)
			}
		})
	}
}

func TestBuildCADMeasurementsUnitTable(t *testing.T) {
	want := map[string]float64{"mm": 1, "cm": 10, "m": 1000, "inch": 25.4, "foot": 304.8, "micrometre": 0.001}
	if len(cadMMPerUnit) != len(want) {
		t.Fatalf("table has %d units, want %d", len(cadMMPerUnit), len(want))
	}
	for unit, f := range want {
		if cadMMPerUnit[unit] != f {
			t.Fatalf("%s: %v, want %v", unit, cadMMPerUnit[unit], f)
		}
		d := mmDraft()
		d.DeclaredUnit = unit
		d.AppliedFactor = floatp(f)
		doc, err := buildCADMeasurements(d, "")
		if err != nil {
			t.Fatalf("%s: %v", unit, err)
		}
		if doc.Unit != unit || doc.UnitSource != measurements.SourceDeclared {
			t.Fatalf("%s: unit %s source %s", unit, doc.Unit, doc.UnitSource)
		}
		if !nearRel(*doc.Volume, *d.Volume/(f*f*f)) {
			t.Fatalf("%s: volume %v", unit, *doc.Volume)
		}
	}
}

const unitsInchDraftJSON = `{"applied_factor":25.4,"area":14193.519999999997,"bbox":{"max":[50.8,76.19999999999999,25.4],"min":[0.0,0.0,0.0]},"closed_solids":1,"declared_unit":"inch","declared_unit_raw":"","format":"step","null_reason":"","solids":1,"triangles":12,"volume":98322.38399999998}`

func writeDraft(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseCADMeasureDraft(t *testing.T) {
	d, err := parseCADMeasureDraft(writeDraft(t, unitsInchDraftJSON))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := buildCADMeasurements(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Unit != measurements.UnitInch || !nearRel(*doc.Volume, 6) {
		t.Fatalf("round trip: unit %s volume %v", doc.Unit, *doc.Volume)
	}

	stl := `{"applied_factor":null,"area":null,"bbox":{"max":[1,2,3],"min":[0,0,0]},"closed_solids":null,"declared_unit":"","declared_unit_raw":"","format":"stl","null_reason":"stl_not_watertight","solids":null,"triangles":12,"volume":null}`
	d, err = parseCADMeasureDraft(writeDraft(t, stl))
	if err != nil {
		t.Fatal(err)
	}
	if d.Solids != nil || d.Volume != nil || d.AppliedFactor != nil || d.NullReason != "stl_not_watertight" {
		t.Fatalf("stl draft: %+v", d)
	}
}

func TestParseCADMeasureDraftRefusals(t *testing.T) {
	big := strings.Replace(unitsInchDraftJSON, `"declared_unit_raw":""`, `"declared_unit_raw":"`+strings.Repeat("a", cadMeasureDraftMaxBytes)+`"`, 1)
	cases := []struct {
		name string
		body string
	}{
		{"unknown key", strings.Replace(unitsInchDraftJSON, `{"applied_factor"`, `{"extra":1,"applied_factor"`, 1)},
		{"missing key", strings.Replace(unitsInchDraftJSON, `"triangles":12,`, ``, 1)},
		{"trailing bytes", unitsInchDraftJSON + "x"},
		{"second object", unitsInchDraftJSON + unitsInchDraftJSON},
		{"above 64 KiB", big},
		{"overflowing number", strings.Replace(unitsInchDraftJSON, `98322.38399999998`, `1e999`, 1)},
		{"NaN literal", strings.Replace(unitsInchDraftJSON, `98322.38399999998`, `NaN`, 1)},
		{"not an object", `[1,2,3]`},
		{"empty", ``},
		{"wrong type", strings.Replace(unitsInchDraftJSON, `"solids":1`, `"solids":"1"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCADMeasureDraft(writeDraft(t, tc.body))
			if err == nil {
				t.Fatal("accepted the draft")
			}
			if !errors.Is(err, ErrCADMeasure) {
				t.Fatalf("error does not wrap ErrCADMeasure: %v", err)
			}
		})
	}
	if _, err := parseCADMeasureDraft(filepath.Join(t.TempDir(), "absent.json")); !errors.Is(err, ErrCADMeasure) {
		t.Fatalf("missing file: %v", err)
	}
}
