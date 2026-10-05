package measurements

import (
	"errors"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func cadNullDoc(format string, solids, closed int, volumeReason, areaReason string) map[string]any {
	m := validDoc(format)
	m["solids"] = solids
	m["closed_solids"] = closed
	m["volume"] = nil
	m["volume_null_reason"] = volumeReason
	m["area"] = nil
	m["area_null_reason"] = areaReason
	return m
}

func expectRejected(t *testing.T, m map[string]any, field string) {
	t.Helper()
	_, err := Validate(marshal(t, m))
	if err == nil {
		t.Fatalf("accepted a contradictory document: %s", marshal(t, m))
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error does not wrap ErrInvalid: %v", err)
	}
	if !strings.Contains(err.Error(), field) {
		t.Fatalf("error %q does not name field %q", err, field)
	}
}

func expectAccepted(t *testing.T, m map[string]any) {
	t.Helper()
	if _, err := Validate(marshal(t, m)); err != nil {
		t.Fatalf("rejected a consistent document %s: %v", marshal(t, m), err)
	}
}

func TestNullReasonAgainstSolidCounts(t *testing.T) {
	cases := []struct {
		name           string
		solids, closed int
		reason         string
		ok             bool
	}{
		{"no_solids with zero solids", 0, 0, ReasonNoSolids, true},
		{"no_solids with one closed solid", 1, 1, ReasonNoSolids, false},
		{"no_solids with one open solid", 1, 0, ReasonNoSolids, false},
		{"no_solids with three solids", 3, 0, ReasonNoSolids, false},

		{"open_shell with one solid none closed", 1, 0, ReasonOpenShell, true},
		{"open_shell with three solids none closed", 3, 0, ReasonOpenShell, true},
		{"open_shell with zero solids", 0, 0, ReasonOpenShell, false},
		{"open_shell with a closed solid", 1, 1, ReasonOpenShell, false},
		{"open_shell beside a closed solid among several", 2, 1, ReasonOpenShell, false},

		{"invalid_solid with one solid none closed", 1, 0, ReasonInvalidSolid, true},
		{"invalid_solid with zero solids", 0, 0, ReasonInvalidSolid, false},
		{"invalid_solid with a closed solid", 1, 1, ReasonInvalidSolid, false},
		{"invalid_solid beside a closed solid among several", 2, 1, ReasonInvalidSolid, false},

		{"negative_volume with one closed solid", 1, 1, ReasonNegativeVolume, true},
		{"negative_volume with a closed solid among several", 3, 2, ReasonNegativeVolume, true},
		{"negative_volume with zero solids", 0, 0, ReasonNegativeVolume, false},
		{"negative_volume with no closed solid", 1, 0, ReasonNegativeVolume, false},
	}
	for _, format := range []string{FormatSTEP, FormatIGES} {
		for _, tc := range cases {
			t.Run(format+" "+tc.name, func(t *testing.T) {
				m := cadNullDoc(format, tc.solids, tc.closed, tc.reason, tc.reason)
				if tc.ok {
					expectAccepted(t, m)
					return
				}
				expectRejected(t, m, "volume_null_reason")
			})
		}
	}
}

func TestAreaNullReasonAgainstSolidCounts(t *testing.T) {
	m := cadNullDoc(FormatSTEP, 1, 1, ReasonNegativeVolume, ReasonNoSolids)
	expectRejected(t, m, "area_null_reason")
}

func TestCodexContradictoryDocuments(t *testing.T) {
	t.Run("solids 1 closed 1 volume null no_solids", func(t *testing.T) {
		expectRejected(t, cadNullDoc(FormatSTEP, 1, 1, ReasonNoSolids, ReasonNoSolids), "volume_null_reason")
	})
	t.Run("open_shell despite a positive closed count", func(t *testing.T) {
		expectRejected(t, cadNullDoc(FormatSTEP, 1, 1, ReasonOpenShell, ReasonOpenShell), "volume_null_reason")
	})
	t.Run("invalid_solid despite a positive closed count", func(t *testing.T) {
		expectRejected(t, cadNullDoc(FormatIGES, 2, 1, ReasonInvalidSolid, ReasonInvalidSolid), "volume_null_reason")
	})
	t.Run("negative_volume with no closed solid", func(t *testing.T) {
		expectRejected(t, cadNullDoc(FormatSTEP, 1, 0, ReasonNegativeVolume, ReasonNegativeVolume), "volume_null_reason")
	})
}

func TestVolumeAndAreaTravelTogether(t *testing.T) {
	t.Run("step number volume with null area", func(t *testing.T) {
		m := validDoc(FormatSTEP)
		m["area"] = nil
		m["area_null_reason"] = ReasonNegativeVolume
		expectRejected(t, m, "area")
	})
	t.Run("step null volume with number area", func(t *testing.T) {
		m := validDoc(FormatSTEP)
		m["volume"] = nil
		m["volume_null_reason"] = ReasonNegativeVolume
		expectRejected(t, m, "area")
	})
	t.Run("stl number volume with null area", func(t *testing.T) {
		m := validDoc(FormatSTL)
		m["volume"] = 12.5
		delete(m, "volume_null_reason")
		expectRejected(t, m, "area")
	})
	t.Run("stl null volume with number area", func(t *testing.T) {
		m := validDoc(FormatSTL)
		m["area"] = 30.0
		delete(m, "area_null_reason")
		expectRejected(t, m, "area")
	})
	t.Run("stl both null with different reasons", func(t *testing.T) {
		m := validDoc(FormatSTL)
		m["area_null_reason"] = ReasonNegativeVolume
		expectRejected(t, m, "area_null_reason")
	})
	t.Run("stl both null with the same negative_volume reason", func(t *testing.T) {
		m := validDoc(FormatSTL)
		m["volume_null_reason"] = ReasonNegativeVolume
		m["area_null_reason"] = ReasonNegativeVolume
		expectAccepted(t, m)
	})
	t.Run("struct built in code", func(t *testing.T) {
		d, err := Validate(marshal(t, validDoc(FormatSTEP)))
		if err != nil {
			t.Fatal(err)
		}
		d.Area = nil
		d.AreaNullReason = ReasonNegativeVolume
		if err := d.Validate(); err == nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPositiveVolumeNeedsExtentOnEveryAxis(t *testing.T) {
	cases := []struct {
		name string
		box  map[string]any
	}{
		{"flat in z", bbox([]float64{0, 0, 5}, []float64{1, 2, 5})},
		{"flat in x", bbox([]float64{3, 0, 0}, []float64{3, 2, 3})},
		{"line", bbox([]float64{0, 0, 0}, []float64{1, 0, 0})},
		{"point", bbox([]float64{4, 4, 4}, []float64{4, 4, 4})},
	}
	for _, format := range []string{FormatSTEP, FormatSTL} {
		for _, tc := range cases {
			t.Run(format+" "+tc.name, func(t *testing.T) {
				m := validDoc(format)
				if format == FormatSTL {
					m["volume"] = 12.5
					delete(m, "volume_null_reason")
					m["area"] = 30.0
					delete(m, "area_null_reason")
				}
				m["bbox"] = tc.box
				field := "volume"
				if format == FormatSTEP && tc.name == "point" {
					field = "bbox"
				}
				expectRejected(t, m, field)
			})
		}
	}
}

func TestCADBBoxIsNotAPoint(t *testing.T) {
	for _, m := range []map[string]any{
		validDoc(FormatIGES),
		cadNullDoc(FormatSTEP, 1, 0, ReasonOpenShell, ReasonOpenShell),
	} {
		m["bbox"] = bbox([]float64{2, 2, 2}, []float64{2, 2, 2})
		expectRejected(t, m, "bbox")
	}
	flat := validDoc(FormatIGES)
	flat["bbox"] = bbox([]float64{0, 0, 0}, []float64{1, 1, 0})
	expectAccepted(t, flat)
}

func TestSTLInvalidSolidIsAllowed(t *testing.T) {
	m := validDoc(FormatSTL)
	m["volume"] = nil
	m["volume_null_reason"] = ReasonInvalidSolid
	m["area"] = nil
	m["area_null_reason"] = ReasonInvalidSolid
	expectAccepted(t, m)

	m["area_null_reason"] = ReasonSTLNotWatertight
	expectRejected(t, m, "area_null_reason")
}

func TestNullReasonFormatMatrix(t *testing.T) {
	allowed := map[string][]string{
		FormatSTEP: {ReasonNoSolids, ReasonOpenShell, ReasonInvalidSolid, ReasonNegativeVolume},
		FormatIGES: {ReasonNoSolids, ReasonOpenShell, ReasonInvalidSolid, ReasonNegativeVolume},
		FormatSTL:  {ReasonSTLNotWatertight, ReasonInvalidSolid, ReasonNegativeVolume},
		FormatDXF:  {ReasonNotApplicable2D},
	}
	for _, format := range AllFormats() {
		for _, reason := range AllNullReasons() {
			want := false
			for _, a := range allowed[format] {
				want = want || a == reason
			}
			if want && (format == FormatSTEP || format == FormatIGES) {
				continue
			}
			t.Run(format+" "+reason, func(t *testing.T) {
				m := validDoc(format)
				m["volume"] = nil
				m["volume_null_reason"] = reason
				m["area"] = nil
				m["area_null_reason"] = reason
				if format == FormatSTEP || format == FormatIGES {
					m["solids"] = 1
					m["closed_solids"] = 1
				}
				if want {
					expectAccepted(t, m)
					return
				}
				expectRejected(t, m, "volume_null_reason")
			})
		}
	}
}

var (
	observedNumber  = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:e[+-]?\d+)?`)
	stlAreaStandIn  = 2200.0
	observedDXFUnit = map[string][2]string{
		"6_m":              {UnitM, SourceDeclared},
		"4_mm":             {UnitMM, SourceDeclared},
		"1_in":             {UnitInch, SourceDeclared},
		"none_unspecified": {UnitUnspecified, SourceNone},
	}
)

func observedFields(t *testing.T, out string) map[string]string {
	t.Helper()
	fields := map[string]string{}
	for _, kv := range strings.Split(out, ";") {
		k, v, ok := strings.Cut(kv, ":")
		if !ok {
			t.Fatalf("malformed helper field %q", kv)
		}
		fields[k] = v
	}
	return fields
}

func observedNumberOrNil(t *testing.T, s string) any {
	t.Helper()
	if s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("number %q: %v", s, err)
	}
	return v
}

func observedCube(t *testing.T, diag string) map[string]any {
	t.Helper()
	d, err := strconv.ParseFloat(diag, 64)
	if err != nil || d <= 0 {
		t.Fatalf("bbox_diag %q", diag)
	}
	e := d / math.Sqrt(3)
	return bbox([]float64{0, 0, 0}, []float64{e, e, e})
}

func TestPhase71HelperRowsValidate(t *testing.T) {
	raw, err := os.ReadFile("testdata/helpers-71-04-rows.txt")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 36 {
		t.Fatalf("fixture has %d rows, want the 36 rc=0 mesh, stlstats and dxf rows of 71-04-helpers.txt", len(lines))
	}
	seen := map[string]int{}
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			t.Fatalf("malformed fixture row %q", line)
		}
		format, name := parts[0], parts[1]
		f := observedFields(t, parts[2])
		var m map[string]any
		switch format {
		case FormatSTEP, FormatIGES:
			reason := f["volume_null_reason"]
			m = map[string]any{
				"schema": Schema, "format": format, "unit": f["unit_text"], "unit_source": SourceDeclared,
				"bbox":   observedCube(t, f["bbox_diag"]),
				"solids": int(observedNumberOrNil(t, f["solids"]).(float64)), "closed_solids": int(observedNumberOrNil(t, f["closed_solids"]).(float64)),
				"volume": observedNumberOrNil(t, f["volume"]), "area": observedNumberOrNil(t, f["area"]),
			}
			if reason != "null" {
				m["volume_null_reason"] = reason
				m["area_null_reason"] = reason
			}
			seen[format+" "+reason]++
		case FormatSTL:
			reason := f["volume_null_reason"]
			m = map[string]any{
				"schema": Schema, "format": format, "unit": UnitUnspecified, "unit_source": SourceNone,
				"bbox":   observedCube(t, f["bbox_diag"]),
				"volume": observedNumberOrNil(t, f["volume"]),
			}
			if reason == "null" {
				m["area"] = stlAreaStandIn
			} else {
				m["area"] = nil
				m["volume_null_reason"] = reason
				m["area_null_reason"] = reason
			}
			seen[format+" "+reason]++
		case FormatDXF:
			unit, ok := observedDXFUnit[f["insunits"]]
			if !ok {
				t.Fatalf("%s: unmapped insunits %q", name, f["insunits"])
			}
			nums := observedNumber.FindAllString(f["bbox2d"], -1)
			if len(nums) != 4 {
				t.Fatalf("%s: bbox2d %q has %d numbers", name, f["bbox2d"], len(nums))
			}
			c := make([]float64, 4)
			for i, n := range nums {
				c[i], err = strconv.ParseFloat(n, 64)
				if err != nil {
					t.Fatal(err)
				}
			}
			m = map[string]any{
				"schema": Schema, "format": format, "unit": unit[0], "unit_source": unit[1],
				"bbox":   bbox([]float64{c[0], c[1]}, []float64{c[2], c[3]}),
				"volume": nil, "volume_null_reason": ReasonNotApplicable2D,
				"area": nil, "area_null_reason": ReasonNotApplicable2D,
			}
			seen[format+" "+f["insunits"]]++
		default:
			t.Fatalf("%s: unknown format %q", name, format)
		}
		if _, err := Validate(marshal(t, m)); err != nil {
			t.Errorf("%s (%s): the helper produced this combination and the validator rejects it: %v", name, format, err)
		}
	}
	for _, want := range []string{
		"step null", "iges no_solids", "step no_solids", "stl null", "stl stl_not_watertight",
		"dxf 6_m", "dxf 4_mm", "dxf 1_in", "dxf none_unspecified",
	} {
		if seen[want] == 0 {
			t.Errorf("combination %q is absent from the fixture; the test would pass without exercising it", want)
		}
	}
}
