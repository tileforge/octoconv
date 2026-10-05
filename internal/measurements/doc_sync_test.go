package measurements

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const measurementsDocPath = "../../docs/MEASUREMENTS.md"

var codeRowPattern = regexp.MustCompile("^\\|\\s*`([a-z][a-z0-9_]*)`")

func readMeasurementsDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(measurementsDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", measurementsDocPath, err)
	}
	return string(b)
}

func extractTableCodes(doc, startHeader string) []string {
	var codes []string
	inSection := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, startHeader) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		if !inSection {
			continue
		}
		if m := codeRowPattern.FindStringSubmatch(line); m != nil {
			codes = append(codes, m[1])
		}
	}
	return codes
}

func diffSets(want, got []string) (missing, extra []string) {
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
	}
	gotSet := make(map[string]bool, len(got))
	for _, g := range got {
		gotSet[g] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !wantSet[g] {
			extra = append(extra, g)
		}
	}
	return missing, extra
}

func TestMeasurementsDocMatchesVocabularies(t *testing.T) {
	doc := readMeasurementsDoc(t)
	cases := []struct {
		header string
		want   []string
	}{
		{"## Formats", AllFormats()},
		{"## Units", AllUnits()},
		{"## Unit sources", AllUnitSources()},
		{"## Null reasons", AllNullReasons()},
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			got := extractTableCodes(doc, tc.header)
			if len(got) == 0 {
				t.Fatalf("extracted zero codes under %q: the section header or table format in docs/MEASUREMENTS.md changed in a way this parser no longer understands", tc.header)
			}
			missing, extra := diffSets(tc.want, got)
			for _, m := range missing {
				t.Errorf("%q is declared in Go but missing from the %q table in docs/MEASUREMENTS.md", m, tc.header)
			}
			for _, e := range extra {
				t.Errorf("%q is in the %q table in docs/MEASUREMENTS.md but not declared in Go", e, tc.header)
			}
		})
	}
}

func TestMeasurementsDocMatchesFields(t *testing.T) {
	doc := readMeasurementsDoc(t)
	got := extractTableCodes(doc, "## Fields")
	if len(got) == 0 {
		t.Fatal("extracted zero fields under \"## Fields\": the section header or table format in docs/MEASUREMENTS.md changed in a way this parser no longer understands")
	}
	var want []string
	typ := reflect.TypeOf(Document{})
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Fatalf("Document field %s has no json tag", typ.Field(i).Name)
		}
		want = append(want, name)
	}
	missing, extra := diffSets(want, got)
	for _, m := range missing {
		t.Errorf("field %q is on Document but missing from the Fields table in docs/MEASUREMENTS.md", m)
	}
	for _, e := range extra {
		t.Errorf("field %q is in the Fields table in docs/MEASUREMENTS.md but not on Document", e)
	}
}

func TestMeasurementsDocNamesSchema(t *testing.T) {
	doc := readMeasurementsDoc(t)
	if !strings.Contains(doc, "`"+Schema+"`") {
		t.Fatalf("docs/MEASUREMENTS.md does not name the schema `%s`", Schema)
	}
}
