package measurements

import (
	"encoding/json"
	"errors"
	"testing"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestMeshFindings(t *testing.T) {
	ok := validDoc(FormatSTL)
	ok["mesh_findings"] = []string{FindingSTLOpen, FindingSTLUnoriented}
	if _, err := Validate(mustJSON(t, ok)); err != nil {
		t.Fatalf("canonical findings: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"empty", func(m map[string]any) { m["mesh_findings"] = []string{} }},
		{"null", func(m map[string]any) { m["mesh_findings"] = nil }},
		{"unknown", func(m map[string]any) { m["mesh_findings"] = []string{"stl_hole"} }},
		{"duplicate", func(m map[string]any) { m["mesh_findings"] = []string{FindingSTLOpen, FindingSTLOpen} }},
		{"order", func(m map[string]any) { m["mesh_findings"] = []string{FindingSTLUnoriented, FindingSTLOpen} }},
		{"step", func(m map[string]any) {
			for k := range m {
				delete(m, k)
			}
			for k, v := range validDoc(FormatSTEP) {
				m[k] = v
			}
			m["mesh_findings"] = []string{FindingSTLOpen}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validDoc(FormatSTL)
			tc.edit(m)
			if _, err := Validate(mustJSON(t, m)); err == nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}
