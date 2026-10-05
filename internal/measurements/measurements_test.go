package measurements

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func validDoc(format string) map[string]any {
	switch format {
	case FormatSTEP:
		return map[string]any{
			"schema": Schema, "format": FormatSTEP, "unit": UnitMM, "unit_source": SourceDeclared,
			"bbox":   map[string]any{"min": []float64{0, 0, 0}, "max": []float64{1, 2, 3}},
			"solids": 1, "closed_solids": 1, "volume": 6.0, "area": 22.0,
		}
	case FormatIGES:
		return map[string]any{
			"schema": Schema, "format": FormatIGES, "unit": UnitMM, "unit_source": SourceDeclared,
			"bbox":   map[string]any{"min": []float64{0, 0, 0}, "max": []float64{1, 2, 3}},
			"solids": 0, "closed_solids": 0,
			"volume": nil, "volume_null_reason": ReasonNoSolids,
			"area": nil, "area_null_reason": ReasonNoSolids,
		}
	case FormatSTL:
		return map[string]any{
			"schema": Schema, "format": FormatSTL, "unit": UnitUnspecified, "unit_source": SourceNone,
			"bbox":   map[string]any{"min": []float64{0, 0, 0}, "max": []float64{1, 2, 3}},
			"volume": nil, "volume_null_reason": ReasonSTLNotWatertight,
			"area": nil, "area_null_reason": ReasonSTLNotWatertight,
		}
	case FormatDXF:
		return map[string]any{
			"schema": Schema, "format": FormatDXF, "unit": UnitInch, "unit_source": SourceDeclared,
			"bbox":   map[string]any{"min": []float64{0, 0}, "max": []float64{4, 5}},
			"volume": nil, "volume_null_reason": ReasonNotApplicable2D,
			"area": nil, "area_null_reason": ReasonNotApplicable2D,
		}
	}
	panic("unknown format " + format)
}

func marshal(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func bbox(min, max []float64) map[string]any {
	return map[string]any{"min": min, "max": max}
}

func TestValidDocuments(t *testing.T) {
	stlInch := validDoc(FormatSTL)
	stlInch["unit"] = UnitInch
	stlInch["unit_source"] = SourceClientAssumed

	stlClosed := validDoc(FormatSTL)
	stlClosed["volume"] = 12.5
	delete(stlClosed, "volume_null_reason")
	stlClosed["area"] = 30.0
	delete(stlClosed, "area_null_reason")

	cases := []struct {
		name string
		doc  map[string]any
	}{
		{"step", validDoc(FormatSTEP)},
		{"iges no solids", validDoc(FormatIGES)},
		{"stl unspecified none", validDoc(FormatSTL)},
		{"stl inch client_assumed", stlInch},
		{"stl closed with numbers", stlClosed},
		{"dxf", validDoc(FormatDXF)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Validate(marshal(t, tc.doc))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if d.Schema != Schema {
				t.Fatalf("Schema = %q", d.Schema)
			}
		})
	}
}

func TestBBoxBoundIsInclusive(t *testing.T) {
	d := validDoc(FormatSTEP)
	d["bbox"] = bbox([]float64{-1e12, 0, 0}, []float64{1e12, 1, 1})
	if _, err := Validate(marshal(t, d)); err != nil {
		t.Fatalf("exactly 1e12 must pass: %v", err)
	}
}

func TestInvalidDocuments(t *testing.T) {
	type mut func(m map[string]any)
	set := func(k string, v any) mut { return func(m map[string]any) { m[k] = v } }
	del := func(k string) mut { return func(m map[string]any) { delete(m, k) } }
	both := func(a, b mut) mut { return func(m map[string]any) { a(m); b(m) } }

	cases := []struct {
		name   string
		format string
		mut    mut
		field  string
	}{
		{"schema v2", FormatSTEP, set("schema", "octoconv.measurements/v2"), "schema"},
		{"schema missing", FormatSTEP, del("schema"), "schema"},
		{"schema integer", FormatSTEP, set("schema", 1), "schema"},
		{"unknown top-level field", FormatSTEP, set("surprise", 1), "surprise"},
		{"format missing", FormatSTEP, del("format"), "format"},
		{"format unknown value", FormatSTEP, set("format", "obj"), "format"},
		{"unit word unknown", FormatSTEP, set("unit", "unknown"), "unit"},
		{"unit missing", FormatSTEP, del("unit"), "unit"},
		{"unit_source file", FormatSTEP, set("unit_source", "file"), "unit_source"},
		{"unit_source missing", FormatSTEP, del("unit_source"), "unit_source"},
		{"unspecified with declared", FormatSTEP, set("unit", UnitUnspecified), "unit"},
		{"mm with none", FormatSTEP, set("unit_source", SourceNone), "unit_source"},
		{"stl with declared", FormatSTL, both(set("unit", UnitMM), set("unit_source", SourceDeclared)), "unit_source"},
		{"volume 0", FormatSTEP, set("volume", 0.0), "volume"},
		{"area 0", FormatSTEP, set("area", 0.0), "area"},
		{"volume negative", FormatSTEP, set("volume", -1.0), "volume"},
		{"area negative", FormatSTEP, set("area", -1.0), "area"},
		{"stl volume 0", FormatSTL, both(set("volume", 0.0), del("volume_null_reason")), "volume"},
		{"positive volume with no closed solid", FormatSTEP, both(set("closed_solids", 0), set("area", nil)), "volume"},
		{"positive area with no closed solid", FormatSTEP, set("closed_solids", 0), "volume"},
		{"volume null without reason", FormatSTEP, set("volume", nil), "volume"},
		{"area null without reason", FormatSTEP, set("area", nil), "area"},
		{"volume number with reason", FormatSTEP, set("volume_null_reason", ReasonNoSolids), "volume_null_reason"},
		{"area number with reason", FormatSTEP, set("area_null_reason", ReasonNoSolids), "area_null_reason"},
		{"volume reason outside vocabulary", FormatIGES, set("volume_null_reason", "because"), "volume_null_reason"},
		{"volume reason empty string beside number", FormatSTEP, set("volume_null_reason", ""), "volume_null_reason"},
		{"open_shell on stl", FormatSTL, set("volume_null_reason", ReasonOpenShell), "volume_null_reason"},
		{"area open_shell on stl", FormatSTL, set("area_null_reason", ReasonOpenShell), "area_null_reason"},
		{"stl_not_watertight on step", FormatIGES, set("volume_null_reason", ReasonSTLNotWatertight), "volume_null_reason"},
		{"not_applicable_2d on step", FormatIGES, set("area_null_reason", ReasonNotApplicable2D), "area_null_reason"},
		{"dxf number volume", FormatDXF, both(set("volume", 6.0), del("volume_null_reason")), "volume"},
		{"dxf number area", FormatDXF, both(set("area", 6.0), del("area_null_reason")), "area"},
		{"dxf reason not 2d", FormatDXF, set("volume_null_reason", ReasonNoSolids), "volume_null_reason"},
		{"dxf 3-coordinate bbox", FormatDXF, set("bbox", bbox([]float64{0, 0, 0}, []float64{1, 1, 1})), "bbox"},
		{"step 2-coordinate bbox", FormatSTEP, set("bbox", bbox([]float64{0, 0}, []float64{1, 1})), "bbox"},
		{"bbox missing", FormatSTEP, del("bbox"), "bbox"},
		{"bbox null", FormatSTEP, set("bbox", nil), "bbox"},
		{"bbox unknown field", FormatSTEP, set("bbox", map[string]any{"min": []float64{0, 0, 0}, "max": []float64{1, 1, 1}, "c": 1}), "c"},
		{"bbox min greater than max", FormatSTEP, set("bbox", bbox([]float64{0, 5, 0}, []float64{1, 2, 3})), "bbox"},
		{"bbox sentinel 1e100", FormatSTEP, set("bbox", bbox([]float64{-1e100, -1e100, -1e100}, []float64{1e100, 1e100, 1e100})), "bbox"},
		{"bbox coordinate 1.0000001e12", FormatSTEP, set("bbox", bbox([]float64{0, 0, 0}, []float64{1.0000001e12, 1, 1})), "bbox"},
		{"bbox min below negative bound", FormatSTEP, set("bbox", bbox([]float64{-1.0000001e12, 0, 0}, []float64{1, 1, 1})), "bbox"},
		{"volume key absent", FormatSTEP, del("volume"), "volume"},
		{"area key absent", FormatSTEP, del("area"), "area"},
		{"closed_solids greater than solids", FormatSTEP, set("closed_solids", 2), "closed_solids"},
		{"negative solids", FormatSTEP, both(set("solids", -1), set("closed_solids", -1)), "solids"},
		{"negative closed_solids", FormatSTEP, set("closed_solids", -1), "closed_solids"},
		{"step solids absent", FormatSTEP, del("solids"), "solids"},
		{"step closed_solids absent", FormatSTEP, del("closed_solids"), "closed_solids"},
		{"iges solids null", FormatIGES, set("solids", nil), "solids"},
		{"stl with solids key", FormatSTL, set("solids", 1), "solids"},
		{"dxf with closed_solids key", FormatDXF, set("closed_solids", 1), "closed_solids"},
		{"volume string", FormatSTEP, set("volume", "6.0"), "volume"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validDoc(tc.format)
			tc.mut(m)
			_, err := Validate(marshal(t, m))
			if err == nil {
				t.Fatalf("accepted an invalid document: %s", marshal(t, m))
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error does not wrap ErrInvalid: %v", err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name field %q", err, tc.field)
			}
		})
	}
}

func TestPositiveAreaWithoutClosedSolidIsRejected(t *testing.T) {
	m := validDoc(FormatIGES)
	m["area"] = 5.0
	delete(m, "area_null_reason")
	_, err := Validate(marshal(t, m))
	if err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "area") {
		t.Fatalf("err = %v", err)
	}
}

func TestRawNonFiniteLiterals(t *testing.T) {
	cases := []struct {
		name    string
		literal string
		target  string
	}{
		{"NaN volume", "NaN", "volume"},
		{"Infinity volume", "Infinity", "volume"},
		{"negative Infinity volume", "-Infinity", "volume"},
		{"1e999 volume", "1e999", "volume"},
		{"-1e999 area", "-1e999", "area"},
		{"NaN bbox", "NaN", "bbox"},
		{"1e999 bbox", "1e999", "bbox"},
		{"Infinity bbox", "Infinity", "bbox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validDoc(FormatSTEP)
			if tc.target == "bbox" {
				m["bbox"] = bbox([]float64{0, 0, 0}, []float64{1, 2, 3})
				raw := marshal(t, m)
				raw = bytes.Replace(raw, []byte(`"max":[1,`), []byte(`"max":[`+tc.literal+`,`), 1)
				if !bytes.Contains(raw, []byte(tc.literal)) {
					t.Fatalf("literal not injected: %s", raw)
				}
				_, err := Validate(raw)
				if err == nil || !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			m[tc.target] = "@@"
			raw := bytes.Replace(marshal(t, m), []byte(`"@@"`), []byte(tc.literal), 1)
			if !bytes.Contains(raw, []byte(tc.literal)) {
				t.Fatalf("literal not injected: %s", raw)
			}
			_, err := Validate(raw)
			if err == nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRawStructuralRejections(t *testing.T) {
	good := marshal(t, validDoc(FormatSTEP))
	cases := []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"not json", []byte("volume")},
		{"array", []byte("[]")},
		{"null", []byte("null")},
		{"trailing object", append(append([]byte{}, good...), []byte(` {}`)...)},
		{"trailing garbage", append(append([]byte{}, good...), []byte(`x`)...)},
		{"truncated", good[:len(good)-1]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Validate(tc.raw)
			if err == nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDocumentValidateRejectsNonFiniteStruct(t *testing.T) {
	for name, v := range map[string]float64{"NaN": math.NaN(), "+Inf": math.Inf(1), "-Inf": math.Inf(-1)} {
		t.Run(name, func(t *testing.T) {
			d, err := Validate(marshal(t, validDoc(FormatSTEP)))
			if err != nil {
				t.Fatal(err)
			}
			val := v
			d.Volume = &val
			if err := d.Validate(); err == nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("volume %s: err = %v", name, err)
			}
			d, _ = Validate(marshal(t, validDoc(FormatSTEP)))
			d.Area = &val
			if err := d.Validate(); err == nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("area %s: err = %v", name, err)
			}
			d, _ = Validate(marshal(t, validDoc(FormatSTEP)))
			d.BBox.Max[0] = v
			if err := d.Validate(); err == nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("bbox %s: err = %v", name, err)
			}
		})
	}
}

func TestVocabularyCounts(t *testing.T) {
	if got := len(AllUnits()); got != 7 {
		t.Errorf("len(AllUnits()) = %d, want 7", got)
	}
	if got := len(AllUnitSources()); got != 3 {
		t.Errorf("len(AllUnitSources()) = %d, want 3", got)
	}
	if got := len(AllNullReasons()); got != 6 {
		t.Errorf("len(AllNullReasons()) = %d, want 6", got)
	}
	if got := len(AllFormats()); got != 4 {
		t.Errorf("len(AllFormats()) = %d, want 4", got)
	}
}

func TestVocabularyExactMembers(t *testing.T) {
	check := func(name string, got, want []string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("units", AllUnits(), []string{"mm", "cm", "m", "inch", "foot", "micrometre", "unspecified"})
	check("unit sources", AllUnitSources(), []string{"declared", "client_assumed", "none"})
	check("null reasons", AllNullReasons(), []string{"no_solids", "open_shell", "invalid_solid", "negative_volume", "stl_not_watertight", "not_applicable_2d"})
	check("formats", AllFormats(), []string{"step", "iges", "stl", "dxf"})
	for _, u := range AllUnits() {
		if u == "unknown" {
			t.Fatal("the draft unit word unknown must not be in the vocabulary")
		}
	}
}

func TestVocabulariesReturnCopies(t *testing.T) {
	fams := map[string]func() []string{
		"units": AllUnits, "unit sources": AllUnitSources, "null reasons": AllNullReasons, "formats": AllFormats,
	}
	for name, f := range fams {
		first := f()
		orig := first[0]
		first[0] = "mutated"
		if f()[0] != orig {
			t.Errorf("%s: second call saw the caller's mutation", name)
		}
	}
}

func TestSchemaConstant(t *testing.T) {
	if Schema != "octoconv.measurements/v1" {
		t.Fatalf("Schema = %q", Schema)
	}
	if MaxAbsCoordinate != 1e12 {
		t.Fatalf("MaxAbsCoordinate = %v", MaxAbsCoordinate)
	}
}
