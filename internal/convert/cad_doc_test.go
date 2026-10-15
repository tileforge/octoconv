package convert

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	cadDocPath     = "../../docs/CAD.md"
	cadDocChart    = "../../deploy/chart/octoconv/templates/configmap.yaml"
	cadDocWorker   = "../../cmd/cad-worker/main.go"
	cadDocEstimate = "../../internal/api/estimate.go"
)

var (
	cadDocTick     = regexp.MustCompile("`([^`]+)`")
	cadDocChartCap = regexp.MustCompile(`(?m)^\s+MAX_UPLOAD_BYTES:\s*"(\d+)"`)
	cadDocEnv      = regexp.MustCompile(`envDuration\("(CAD_[A-Z_]+)", convert\.(\w+)\)`)
)

func cadDocRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func cadDocSection(t *testing.T, doc, header string) string {
	t.Helper()
	var out []string
	in := false
	for _, line := range strings.Split(doc, "\n") {
		if line == header {
			in = true
			continue
		}
		if in && strings.HasPrefix(line, "## ") {
			break
		}
		if in {
			out = append(out, line)
		}
	}
	if !in {
		t.Fatalf("%s has no %q section", cadDocPath, header)
	}
	return strings.Join(out, "\n")
}

func cadDocCells(line string) []string {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) < 3 {
		return nil
	}
	cells := parts[1 : len(parts)-1]
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func cadDocTable(t *testing.T, doc, header string, cols ...string) [][]string {
	t.Helper()
	want := strings.Join(cols, "|")
	var rows [][]string
	found, in := false, false
	for _, line := range strings.Split(cadDocSection(t, doc, header), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			if in {
				break
			}
			continue
		}
		cells := cadDocCells(line)
		if !in {
			if strings.Join(cells, "|") == want {
				found, in = true, true
			}
			continue
		}
		if strings.Trim(strings.Join(cells, ""), "-") == "" {
			continue
		}
		if len(cells) != len(cols) {
			t.Fatalf("%s %s: row %q has %d cells, want %d", cadDocPath, header, line, len(cells), len(cols))
		}
		rows = append(rows, cells)
	}
	if !found {
		t.Fatalf("%s %s has no table with the header %q", cadDocPath, header, want)
	}
	if len(rows) == 0 {
		t.Fatalf("%s %s: the table %q has no rows", cadDocPath, header, want)
	}
	return rows
}

func cadDocTicks(cell string) map[string]bool {
	out := map[string]bool{}
	for _, m := range cadDocTick.FindAllStringSubmatch(cell, -1) {
		out[m[1]] = true
	}
	return out
}

func cadDocOne(t *testing.T, cell, what string) string {
	t.Helper()
	m := cadDocTicks(cell)
	if len(m) != 1 {
		t.Fatalf("%s: %s cell %q names %d backticked values, want 1", cadDocPath, what, cell, len(m))
	}
	for k := range m {
		return k
	}
	return ""
}

func cadDocSorted(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func cadDocSameSet(t *testing.T, what string, doc, code map[string]bool) {
	t.Helper()
	for _, k := range cadDocSorted(code) {
		if !doc[k] {
			t.Errorf("%s: the code has %q, the document does not", what, k)
		}
	}
	for _, k := range cadDocSorted(doc) {
		if !code[k] {
			t.Errorf("%s: the document has %q, the code does not", what, k)
		}
	}
}

func cadDocPairs(t *testing.T) map[string][]Pair {
	t.Helper()
	out := map[string][]Pair{}
	for op, classes := range Default.OperationClasses() {
		if pairs := classes[EngineCAD]; len(pairs) > 0 {
			out[op] = pairs
		}
	}
	if len(out) < 2 {
		t.Fatalf("the registry has cad pairs under %d operations, want convert and render", len(out))
	}
	return out
}

func cadDocTags() []string {
	var tags []string
	ty := reflect.TypeOf(CADOpts{})
	for i := 0; i < ty.NumField(); i++ {
		tags = append(tags, strings.Split(ty.Field(i).Tag.Get("json"), ",")[0])
	}
	return tags
}

func cadDocField(o CADOpts, tag string) reflect.Value {
	v := reflect.ValueOf(&o).Elem()
	for i := 0; i < v.NumField(); i++ {
		if strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0] == tag {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func cadDocClosed(tag string) (map[string]bool, bool) {
	switch tag {
	case "view":
		return cadViews, true
	case "quality":
		return cadQualities, true
	case "background":
		return cadBackgrounds, true
	case "assume_unit":
		return cadUnits, true
	}
	return nil, false
}

func cadDocOnly(t *testing.T, tag string) CADOpts {
	t.Helper()
	var o CADOpts
	f := reflect.ValueOf(&o).Elem()
	for i := 0; i < f.NumField(); i++ {
		if strings.Split(f.Type().Field(i).Tag.Get("json"), ",")[0] != tag {
			continue
		}
		switch fv := f.Field(i); fv.Kind() {
		case reflect.String:
			closed, ok := cadDocClosed(tag)
			if !ok {
				t.Fatalf("option %s is a string with no closed set in the test", tag)
			}
			fv.SetString(cadDocSorted(closed)[0])
		case reflect.Pointer:
			n := CADMinRenderSize
			fv.Set(reflect.ValueOf(&n))
		default:
			t.Fatalf("option %s has kind %s, which the test cannot set", tag, fv.Kind())
		}
		return o
	}
	t.Fatalf("CADOpts has no option %s", tag)
	return o
}

func cadDocSeconds(d time.Duration) string {
	return strconv.Itoa(int(d / time.Second))
}

func cadDocEvidence(t *testing.T, kind string) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, line := range strings.Split(cadDocRead(t, cad76Evidence), "\n") {
		if !strings.HasPrefix(line, kind+",") {
			continue
		}
		f := map[string]string{}
		for _, kv := range strings.Split(line, ",")[1:] {
			if k, v, ok := strings.Cut(kv, "="); ok {
				f[k] = v
			}
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		t.Fatalf("%s has no %s line", cad76Evidence, kind)
	}
	return out
}

var cadDocPipeline = map[string]string{
	"CADMaxSTEPEntities":        strconv.Itoa(CADMaxSTEPEntities),
	"CADMaxNAUO":                strconv.Itoa(CADMaxNAUO),
	"CADMaxRenderTriangles":     strconv.Itoa(CADMaxRenderTriangles),
	"CADMaxDXFExpandedInserts":  strconv.Itoa(CADMaxDXFExpandedInserts),
	"CADMaxDXFExpandedEntities": strconv.Itoa(CADMaxDXFExpandedEntities),
	"CADMaxDXFNesting":          strconv.Itoa(CADMaxDXFNesting),
	"CADMaxDXFSVGBytes":         strconv.FormatInt(CADMaxDXFSVGBytes, 10),
	"CADMaxDXFPDFBytes":         strconv.FormatInt(CADMaxDXFPDFBytes, 10),
	"cadPDFInflateFactor":       strconv.Itoa(cadPDFInflateFactor),
}

var cadDocDurations = map[string]time.Duration{
	"CADDefaultGuardTimeout":  CADDefaultGuardTimeout,
	"CADDefaultMeshTimeout":   CADDefaultMeshTimeout,
	"CADDefaultRenderTimeout": CADDefaultRenderTimeout,
	"CADDefaultDXFTimeout":    CADDefaultDXFTimeout,
	"CADDefaultRsvgTimeout":   CADDefaultRsvgTimeout,
	"CADDefaultEngineTimeout": CADDefaultEngineTimeout,
}

var cadDocLimitConsts = map[string]string{
	"CADDXFPDFTextFloorPt":    strconv.FormatFloat(CADDXFPDFTextFloorPt, 'g', -1, 64),
	"cadBackgroundCutoff":     strconv.Itoa(cadBackgroundCutoff),
	"CADDXFInkSize":           strconv.Itoa(CADDXFInkSize),
	"CADMaxRenderSize":        strconv.Itoa(CADMaxRenderSize),
	"CADDefaultRenderTimeout": cadDocSeconds(CADDefaultRenderTimeout),
	"CADMaxIGESBytes":         strconv.FormatInt(CADMaxIGESBytes, 10),
	"CADIOAllowance":          cadDocSeconds(CADIOAllowance),
}

var cadDocRequiredLimits = []string{
	"R-1", "R-9", "R-10", "R-11", "arm64_only", "text_oblique", "font_family_style", "font_fallback",
	"near_white", "empty_white_page", "empty_result_causes", "iges_inch_unproven", "stl_shell_overlap",
}

func TestCADDocMatchesTheCode(t *testing.T) {
	doc := cadDocRead(t, cadDocPath)
	pairs := cadDocPairs(t)

	t.Run("formats", func(t *testing.T) {
		wantOps := map[string]map[string]bool{}
		for op, ps := range pairs {
			for _, p := range ps {
				if wantOps[p.From] == nil {
					wantOps[p.From] = map[string]bool{}
				}
				wantOps[p.From][op] = true
			}
		}
		rows := cadDocTable(t, doc, "## Formats", "format", "aliases", "operations")
		got := map[string]bool{}
		for _, r := range rows {
			f := cadDocOne(t, r[0], "format")
			got[f] = true
			aliases := map[string]bool{}
			for a, canon := range formatAliases {
				if canon == f {
					aliases[a] = true
				}
			}
			cadDocSameSet(t, "aliases of "+f, cadDocTicks(r[1]), aliases)
			cadDocSameSet(t, "operations of "+f, cadDocTicks(r[2]), wantOps[f])
		}
		want := map[string]bool{}
		for f := range wantOps {
			want[f] = true
		}
		cadDocSameSet(t, "source formats", got, want)
	})

	t.Run("operations", func(t *testing.T) {
		want := map[string]bool{}
		for op, ps := range pairs {
			for _, p := range ps {
				want[op+" "+p.From+" "+p.To] = true
			}
		}
		got := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Operations and pairs", "operation", "from", "to") {
			key := cadDocOne(t, r[0], "operation") + " " + cadDocOne(t, r[1], "from") + " " + cadDocOne(t, r[2], "to")
			if got[key] {
				t.Errorf("docs/CAD.md lists the pair %s twice", key)
			}
			got[key] = true
		}
		cadDocSameSet(t, "pairs", got, want)
	})

	t.Run("options", func(t *testing.T) {
		resolved := CADOpts{}.Resolved()
		code := map[string]bool{}
		for _, tag := range cadDocTags() {
			code[tag] = true
		}
		got := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Render options", "option", "values", "default") {
			tag := cadDocOne(t, r[0], "option")
			got[tag] = true
			if !code[tag] {
				continue
			}
			if closed, ok := cadDocClosed(tag); ok {
				cadDocSameSet(t, "values of "+tag, cadDocTicks(r[1]), closed)
			} else if tag == "size" {
				if want := fmt.Sprintf("`%d..%d`", CADMinRenderSize, CADMaxRenderSize); r[1] != want {
					t.Errorf("docs/CAD.md size values %q, want %q", r[1], want)
				}
			} else {
				t.Errorf("option %s has neither a closed set nor a range in the test", tag)
			}
			want := "none"
			switch fv := cadDocField(resolved, tag); fv.Kind() {
			case reflect.String:
				if fv.String() != "" {
					want = fv.String()
				}
			case reflect.Pointer:
				if !fv.IsNil() {
					want = strconv.Itoa(int(fv.Elem().Int()))
				}
			}
			if d := cadDocOne(t, r[2], "default of "+tag); d != want {
				t.Errorf("docs/CAD.md says the default of %s is %q, the code resolves %q", tag, d, want)
			}
		}
		cadDocSameSet(t, "options", got, code)
	})

	t.Run("applicability", func(t *testing.T) {
		want := map[string]map[string]bool{}
		for op, ps := range pairs {
			for _, p := range ps {
				key := op + " " + p.From
				accepted := map[string]bool{}
				for _, tag := range cadDocTags() {
					if ValidateCADApplicability(EngineCAD, op, p.From, p.To, cadDocOnly(t, tag)) == nil {
						accepted[tag] = true
					}
				}
				if prev, ok := want[key]; ok && !reflect.DeepEqual(prev, accepted) {
					t.Fatalf("%s accepts different options per target; the table has no target column", key)
				}
				want[key] = accepted
			}
		}
		got := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Option applicability", "operation", "format", "accepted options") {
			key := cadDocOne(t, r[0], "operation") + " " + cadDocOne(t, r[1], "format")
			got[key] = true
			if r[2] == "—" {
				r[2] = ""
			}
			cadDocSameSet(t, "options accepted by "+key, cadDocTicks(r[2]), want[key])
		}
		keys := map[string]bool{}
		for k := range want {
			keys[k] = true
		}
		cadDocSameSet(t, "applicability rows", got, keys)
	})

	t.Run("ceilings", func(t *testing.T) {
		m := cadDocChartCap.FindStringSubmatch(cadDocRead(t, cadDocChart))
		if m == nil {
			t.Fatalf("%s has no MAX_UPLOAD_BYTES", cadDocChart)
		}
		chart, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		header := fmt.Sprintf("effective under the chart's %d MiB transport cap", chart>>20)
		if chart%(1<<20) != 0 {
			t.Fatalf("the chart's MAX_UPLOAD_BYTES %d is not whole MiB", chart)
		}
		want := map[string]bool{}
		for op, ps := range pairs {
			for _, p := range ps {
				want[p.From+" "+op] = true
			}
		}
		got := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Size ceilings", "format", "operation", "bytes", "MiB", header) {
			f, op := cadDocOne(t, r[0], "format"), cadDocOne(t, r[1], "operation")
			got[f+" "+op] = true
			ceiling := cadMaxBytes(f)
			if ceiling%(1<<20) != 0 {
				t.Errorf("the %s ceiling %d is not whole MiB", f, ceiling)
			}
			for _, c := range []struct {
				what, cell string
				want       int64
			}{
				{"bytes", r[2], ceiling},
				{"MiB", r[3], ceiling >> 20},
				{"effective", r[4], min(ceiling, chart)},
			} {
				if c.cell != strconv.FormatInt(c.want, 10) {
					t.Errorf("docs/CAD.md %s %s %s = %q, the code gives %d", f, op, c.what, c.cell, c.want)
				}
			}
		}
		cadDocSameSet(t, "ceiling rows", got, want)
		limits := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Size ceilings", "limit", "constant", "value") {
			name := cadDocOne(t, r[1], "constant")
			limits[name] = true
			if v, ok := cadDocPipeline[name]; !ok {
				t.Errorf("docs/CAD.md names the pipeline limit %s, which the test does not know", name)
			} else if r[2] != v {
				t.Errorf("docs/CAD.md states %s = %s, the code has %s", name, r[2], v)
			}
		}
		code := map[string]bool{}
		for k := range cadDocPipeline {
			code[k] = true
		}
		cadDocSameSet(t, "pipeline limits", limits, code)
	})

	t.Run("time_limits", func(t *testing.T) {
		vars := map[string]time.Duration{}
		for _, m := range cadDocEnv.FindAllStringSubmatch(cadDocRead(t, cadDocWorker), -1) {
			d, ok := cadDocDurations[m[2]]
			if !ok {
				t.Errorf("%s reads %s with the default convert.%s, which the test does not know", cadDocWorker, m[1], m[2])
				continue
			}
			vars[m[1]] = d
		}
		if len(vars) < 6 {
			t.Fatalf("found %d CAD timeout variables in %s, want 6", len(vars), cadDocWorker)
		}
		got := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Time limits", "variable", "default seconds", "covers") {
			v := cadDocOne(t, r[0], "variable")
			got[v] = true
			if d, ok := vars[v]; ok && r[1] != cadDocSeconds(d) {
				t.Errorf("docs/CAD.md %s = %s s, the worker's default is %s s", v, r[1], cadDocSeconds(d))
			}
		}
		code := map[string]bool{}
		for v := range vars {
			code[v] = true
		}
		cadDocSameSet(t, "timeout variables", got, code)

		sec := cadDocSection(t, doc, "## Time limits")
		g, m, r, io := int(CADDefaultGuardTimeout/time.Second), int(CADDefaultMeshTimeout/time.Second), int(CADDefaultRenderTimeout/time.Second), int(CADIOAllowance/time.Second)
		d, s, e := int(CADDefaultDXFTimeout/time.Second), int(CADDefaultRsvgTimeout/time.Second), int(CADDefaultEngineTimeout/time.Second)
		for _, w := range []string{
			fmt.Sprintf("%d s of I/O", io),
			fmt.Sprintf("%d + %d + %d + %d = %d", g, m, r, io, g+m+r+io),
			fmt.Sprintf("%d + %d + 2 x %d + %d = %d", g, d, s, io, g+d+2*s+io),
			"`estimate_source: measured`",
			fmt.Sprintf("estimated maximum of %d s", e),
		} {
			if !strings.Contains(sec, w) {
				t.Errorf("the Time limits section of docs/CAD.md does not state %q", w)
			}
		}
		if max(g+m+r+io, g+d+2*s+io) != e {
			t.Errorf("the engine timeout %d is not the longer route", e)
		}
		for _, l := range cadDocEvidence(t, "DERIVED") {
			if l["key"] != "cad_engine_timeout_s" {
				continue
			}
			if l["value"] != strconv.Itoa(e) {
				t.Errorf("the evidence derives the engine timeout %s, the code has %d", l["value"], e)
			}
			for _, w := range []string{l["worst_route"], l["worst_cell"]} {
				if w == "" || !strings.Contains(sec, w) {
					t.Errorf("the Time limits section does not name the worst route or cell %q of the evidence", w)
				}
			}
		}
		est := cadDocRead(t, cadDocEstimate)
		if !regexp.MustCompile(`convert\.EngineCAD:\s+EstimateSourceMeasured,`).MatchString(est) {
			t.Errorf("%s does not mark the cad estimate measured", cadDocEstimate)
		}
		if mm := regexp.MustCompile(`convert\.EngineCAD:\s+(\d+),`).FindStringSubmatch(est); mm == nil || mm[1] != strconv.Itoa(e) {
			t.Errorf("%s does not give the cad estimate %d s: %v", cadDocEstimate, e, mm)
		}
	})

	t.Run("known_limits", func(t *testing.T) {
		rows := cadDocTable(t, doc, "## Known limits", "id", "limit", "constant", "value")
		byID := map[string][]string{}
		named := map[string]bool{}
		for _, r := range rows {
			if _, dup := byID[r[0]]; dup {
				t.Errorf("docs/CAD.md lists the limit %s twice", r[0])
			}
			byID[r[0]] = r
			if r[1] == "" {
				t.Errorf("docs/CAD.md limit %s says nothing", r[0])
			}
			if r[2] == "—" {
				if r[3] != "—" {
					t.Errorf("docs/CAD.md limit %s has no constant but the value %q", r[0], r[3])
				}
				continue
			}
			name := cadDocOne(t, r[2], "constant of "+r[0])
			named[name] = true
			if v, ok := cadDocLimitConsts[name]; !ok {
				t.Errorf("docs/CAD.md limit %s names %s, which the test does not know", r[0], name)
			} else if r[3] != v {
				t.Errorf("docs/CAD.md limit %s states %s = %s, the code has %s", r[0], name, r[3], v)
			}
		}
		for name := range cadDocLimitConsts {
			if !named[name] {
				t.Errorf("no Known limits row of docs/CAD.md states %s", name)
			}
		}
		for _, id := range cadDocRequiredLimits {
			if _, ok := byID[id]; !ok {
				t.Errorf("docs/CAD.md Known limits has no row %s", id)
			}
		}
		render := cadDocSeconds(CADDefaultRenderTimeout)
		for _, l := range cadDocEvidence(t, "KNOWN_LIMIT") {
			r, ok := byID[l["key"]]
			if !ok {
				t.Errorf("the evidence's KNOWN_LIMIT %s has no row in docs/CAD.md", l["key"])
				continue
			}
			if rr, ok := l["R"]; ok && rr != render {
				t.Errorf("KNOWN_LIMIT %s uses R=%s, the render timeout is %s", l["key"], rr, render)
			}
			if b := l["smallest_refused_bytes"]; b != "" && !strings.Contains(r[1], b+" bytes ("+strings.ToUpper(l["format"])+")") {
				t.Errorf("docs/CAD.md %s does not state the smallest refused %s input, %s bytes", l["key"], l["format"], b)
			}
			if b := l["from_bytes"]; b != "" && !strings.Contains(r[1], b) {
				t.Errorf("docs/CAD.md %s does not state the old ceiling %s", l["key"], b)
			}
			if b := l["to_bytes"]; b != "" && b != r[3] {
				t.Errorf("docs/CAD.md %s states %s, the evidence lowered the ceiling to %s", l["key"], r[3], b)
			}
		}
	})
}
