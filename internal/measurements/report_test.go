package measurements

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func box3(min, max []float64) map[string]any {
	return map[string]any{"bbox": bbox(min, max)}
}

func TestSkippedAndBodies(t *testing.T) {
	dxf := validDoc(FormatDXF)
	dxf["skipped"] = map[string]int{"IMAGE": 2, "PROXY:ACAD_PROXY_ENTITY": 1, "3DSOLID": 1}
	if _, err := Validate(marshal(t, dxf)); err != nil {
		t.Fatalf("dxf skips: %v", err)
	}

	step := validDoc(FormatSTEP)
	step["bodies"] = []any{box3([]float64{0, 0, 0}, []float64{1, 2, 3})}
	if _, err := Validate(marshal(t, step)); err != nil {
		t.Fatalf("one solid box: %v", err)
	}

	omitted := validDoc(FormatSTEP)
	if _, err := Validate(marshal(t, omitted)); err != nil {
		t.Fatalf("step without bodies: %v", err)
	}

	flat := validDoc(FormatSTEP)
	flat["bodies"] = []any{box3([]float64{1, 1, 1}, []float64{1, 1, 1})}
	if _, err := Validate(marshal(t, flat)); err != nil {
		t.Fatalf("a solid box may have no extent: %v", err)
	}

	kinds := map[string]int{}
	for i := range MaxSkippedKinds {
		kinds[fmt.Sprintf("K%02d", i)] = 1
	}
	many := validDoc(FormatDXF)
	many["skipped"] = kinds
	if _, err := Validate(marshal(t, many)); err != nil {
		t.Fatalf("%d kinds: %v", MaxSkippedKinds, err)
	}
	longKind := strings.Repeat("A", MaxSkipKindLen)
	long := validDoc(FormatDXF)
	long["skipped"] = map[string]int{longKind: 1}
	if _, err := Validate(marshal(t, long)); err != nil {
		t.Fatalf("kind of %d characters: %v", MaxSkipKindLen, err)
	}

	two := validDoc(FormatSTEP)
	two["solids"] = 2
	two["bodies"] = []any{
		box3([]float64{0, 0, 0}, []float64{1, 1, 1}),
		box3([]float64{2, 0, 0}, []float64{3, 1, 1}),
	}
	if _, err := Validate(marshal(t, two)); err != nil {
		t.Fatalf("two solid boxes: %v", err)
	}
}

func TestSkippedAndBodiesRejected(t *testing.T) {
	type mut func(m map[string]any)
	set := func(k string, v any) mut { return func(m map[string]any) { m[k] = v } }

	cases := []struct {
		name   string
		format string
		mut    mut
		field  string
	}{
		{"skipped empty", FormatDXF, set("skipped", map[string]int{}), "skipped"},
		{"skipped null", FormatDXF, set("skipped", nil), "skipped"},
		{"skipped zero", FormatDXF, set("skipped", map[string]int{"IMAGE": 0}), "skipped"},
		{"skipped negative", FormatDXF, set("skipped", map[string]int{"IMAGE": -1}), "skipped"},
		{"skipped lower", FormatDXF, set("skipped", map[string]int{"image": 1}), "skipped"},
		{"skipped space", FormatDXF, set("skipped", map[string]int{"IMAGE DEF": 1}), "skipped"},
		{"skipped bare colon", FormatDXF, set("skipped", map[string]int{"IMAGE:DEF": 1}), "skipped"},
		{"skipped proxy empty", FormatDXF, set("skipped", map[string]int{"PROXY:": 1}), "skipped"},
		{"skipped two colons", FormatDXF, set("skipped", map[string]int{"PROXY:IMAGE:DEF": 1}), "skipped"},
		{"skipped too long", FormatDXF, set("skipped", map[string]int{strings.Repeat("A", MaxSkipKindLen+1): 1}), "skipped"},
		{"skipped on step", FormatSTEP, set("skipped", map[string]int{"IMAGE": 1}), "skipped"},
		{"skipped on stl", FormatSTL, set("skipped", map[string]int{"IMAGE": 1}), "skipped"},
		{"bodies empty", FormatSTEP, set("bodies", []any{}), "bodies"},
		{"bodies null", FormatSTEP, set("bodies", nil), "bodies"},
		{"bodies length", FormatSTEP, set("bodies", []any{
			box3([]float64{0, 0, 0}, []float64{1, 1, 1}),
			box3([]float64{0, 0, 0}, []float64{1, 1, 1}),
		}), "bodies"},
		{"bodies on empty iges", FormatIGES, set("bodies", []any{box3([]float64{0, 0, 0}, []float64{1, 1, 1})}), "bodies"},
		{"bodies on dxf", FormatDXF, set("bodies", []any{box3([]float64{0, 0, 0}, []float64{1, 1, 1})}), "bodies"},
		{"bodies on stl", FormatSTL, set("bodies", []any{box3([]float64{0, 0, 0}, []float64{1, 1, 1})}), "bodies"},
		{"body flat coordinates", FormatSTEP, set("bodies", []any{box3([]float64{0, 0}, []float64{1, 1})}), "bodies"},
		{"body min above max", FormatSTEP, set("bodies", []any{box3([]float64{0, 5, 0}, []float64{1, 2, 3})}), "bodies"},
		{"body extra field", FormatSTEP, set("bodies", []any{map[string]any{
			"bbox": bbox([]float64{0, 0, 0}, []float64{1, 1, 1}),
			"id":   1,
		}}), "id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validDoc(tc.format)
			tc.mut(m)
			_, err := Validate(marshal(t, m))
			if err == nil {
				t.Fatalf("accepted an invalid document")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error does not wrap ErrInvalid: %v", err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name field %q", err, tc.field)
			}
		})
	}

	over := map[string]int{}
	for i := range MaxSkippedKinds + 1 {
		over[fmt.Sprintf("K%02d", i)] = 1
	}
	m := validDoc(FormatDXF)
	m["skipped"] = over
	_, err := Validate(marshal(t, m))
	if err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("65 kinds: %v", err)
	}
}
