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

var solidCountRules = map[string]func(solids, closed int) bool{
	"none":                             func(solids, closed int) bool { return true },
	"solids = 0":                       func(solids, closed int) bool { return solids == 0 },
	"solids > 0 and closed_solids = 0": func(solids, closed int) bool { return solids > 0 && closed == 0 },
	"closed_solids > 0":                func(solids, closed int) bool { return closed > 0 },
}

type nullReasonRow struct {
	reason  string
	formats []string
	counts  string
}

func extractNullReasonRows(t *testing.T, doc string) []nullReasonRow {
	t.Helper()
	var rows []nullReasonRow
	inSection := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## Null reasons") {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		if !inSection || codeRowPattern.FindStringSubmatch(line) == nil {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			t.Fatalf("Null reasons row has %d cells, want reason, formats, solid counts and observed: %q", len(cells), line)
		}
		row := nullReasonRow{
			reason: strings.Trim(strings.TrimSpace(cells[1]), "`"),
			counts: strings.TrimSpace(strings.ReplaceAll(cells[3], "`", "")),
		}
		for _, f := range strings.Split(strings.ReplaceAll(cells[2], "`", ""), ",") {
			row.formats = append(row.formats, strings.TrimSpace(f))
		}
		rows = append(rows, row)
	}
	return rows
}

func probeDocument(format string, solids, closed int, reason string) Document {
	d := Document{
		Schema: Schema, Format: format, Unit: UnitMM, UnitSource: SourceDeclared,
		BBox:             BBox{Min: []float64{0, 0, 0}, Max: []float64{1, 2, 3}},
		VolumeNullReason: reason, AreaNullReason: reason,
	}
	switch format {
	case FormatSTL:
		d.Unit, d.UnitSource = UnitUnspecified, SourceNone
	case FormatDXF:
		d.BBox = BBox{Min: []float64{0, 0}, Max: []float64{4, 5}}
	case FormatSTEP, FormatIGES:
		d.Solids, d.ClosedSolids = &solids, &closed
	}
	return d
}

func TestMeasurementsDocNullReasonTableMatchesBehaviour(t *testing.T) {
	rows := extractNullReasonRows(t, readMeasurementsDoc(t))
	if len(rows) == 0 {
		t.Fatal("extracted zero rows under \"## Null reasons\": the table format in docs/MEASUREMENTS.md changed in a way this parser no longer understands")
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.reason] = true
		rule, ok := solidCountRules[row.counts]
		if !ok {
			t.Fatalf("%s: the Solid counts cell %q is not one of the forms this test understands", row.reason, row.counts)
		}
		accepted, rejected := 0, 0
		for _, format := range AllFormats() {
			listed := false
			for _, f := range row.formats {
				listed = listed || f == format
			}
			cad := format == FormatSTEP || format == FormatIGES
			for solids := 0; solids <= 3; solids++ {
				for closed := 0; closed <= solids; closed++ {
					if !cad && (solids > 0 || closed > 0) {
						continue
					}
					want := listed && (!cad || rule(solids, closed))
					err := probeDocument(format, solids, closed, row.reason).Validate()
					if (err == nil) != want {
						t.Errorf("%s on %s with solids %d and closed_solids %d: doc says valid=%v (formats %v, counts %q), Validate returned %v",
							row.reason, format, solids, closed, want, row.formats, row.counts, err)
					}
					if err == nil {
						accepted++
					} else {
						rejected++
					}
				}
			}
		}
		if accepted == 0 || rejected == 0 {
			t.Errorf("%s: the probe saw %d accepted and %d rejected combinations; a table that never discriminates proves nothing", row.reason, accepted, rejected)
		}
	}
	for _, r := range AllNullReasons() {
		if !seen[r] {
			t.Errorf("%q is declared in Go but has no row in the Null reasons table", r)
		}
	}
}
