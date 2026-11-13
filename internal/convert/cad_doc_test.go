package convert

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	cadDocPath     = "../../docs/CAD.md"
	cadDocChart    = "../../deploy/chart/octoconv/templates/configmap.yaml"
	cadDocEstimate = "../../internal/api/estimate.go"
	cadDocValues   = "../../deploy/chart/octoconv/values.yaml"
	cadDocEvidDir  = "testdata/evidence/76-measured-bounds-keda-proof-client-docs/"
	cad76Evidence  = "testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-03-derived.txt"
)

var (
	cadDocTick        = regexp.MustCompile("`([^`]+)`")
	cadDocChartCap    = regexp.MustCompile(`(?m)^\s+MAX_UPLOAD_BYTES:\s*"(\d+)"`)
	cadDocConcurrency = regexp.MustCompile(`(?m)^\s+CAD_WORKER_CONCURRENCY:\s*"?([^"\s]+)"?\s*$`)
	cadDocSource      = regexp.MustCompile(`^evidence/(\S+) ([A-Z_]+)(?:\[(\w+)=([^\]]+)\])?\.(\w+)$`)
	cadDocNearInk     = regexp.MustCompile(`\((\d{1,3}), (\d{1,3}), (\d{1,3})\) is ink`)
	cadDocNearNot     = regexp.MustCompile(`\((\d{1,3}), (\d{1,3}), (\d{1,3})\) is not`)
	cadDocStageWord   = regexp.MustCompile("`([a-z]+)`")
)

var cadDocScalingEvidence = []string{
	"evidence/76-09-image-size.txt IMAGE_SIZE.uncompressed_mib",
	"evidence/76-09-image-size.txt IMAGE_SIZE.compressed_mib",
	"evidence/76-09-image-size.txt COLD_PULL_DIST.wall_max",
	"evidence/76-09-image-size.txt PULL_CALC[bandwidth_mbit=100].transfer_s",
	"evidence/76-09-image-size.txt PULL_CALC[bandwidth_mbit=1000].transfer_s",
	"evidence/76-09-image-size.txt PULL_CALC[bandwidth_mbit=10000].transfer_s",
	"evidence/76-09-image-size.txt PREFLIGHT_DIST.max",
	"evidence/76-09-image-size.txt DECISION.cold_start_s",
	"evidence/76-09-image-size.txt DECISION.premise_holds",
	"evidence/76-09-image-size.txt DECISION.value",
	"evidence/76-09-keda-loadproof.txt CALIBRATION.submit_to_ready_s",
	"evidence/76-09-keda-loadproof.txt TRANSITION[ready=2].since_submit_s",
	"evidence/76-09-keda-loadproof.txt DRAIN.drain_s",
	"evidence/76-09-keda-loadproof.txt CALIBRATION.pull_events",
}

func cadDocKEDACAD(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	inKEDA, inCAD := false, false
	for _, line := range strings.Split(cadDocRead(t, cadDocValues), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0:
			inKEDA, inCAD = strings.TrimRight(line, " ") == "keda:", false
		case inKEDA && indent == 2:
			inCAD = trimmed == "cad:"
		case inCAD && indent == 4:
			k, v, ok := strings.Cut(trimmed, ":")
			if !ok {
				t.Fatalf("%s keda.cad line %q is not key: value", cadDocValues, line)
			}
			out[k] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s has no keda.cad block", cadDocValues)
	}
	return out
}

func cadDocEvidenceValue(t *testing.T, src string) (string, bool) {
	t.Helper()
	m := cadDocSource.FindStringSubmatch(src)
	if m == nil {
		t.Errorf("docs/CAD.md source %q is not evidence/<file> <LINE>[<key>=<value>].<field>", src)
		return "", false
	}
	var hits []map[string]string
	for _, line := range strings.Split(cadDocRead(t, cadDocEvidDir+m[1]), "\n") {
		if !strings.HasPrefix(line, m[2]+",") {
			continue
		}
		f := map[string]string{}
		for _, kv := range strings.Split(line, ",")[1:] {
			if k, v, ok := strings.Cut(kv, "="); ok {
				if _, dup := f[k]; !dup {
					f[k] = v
				}
			}
		}
		if m[3] != "" && f[m[3]] != m[4] {
			continue
		}
		hits = append(hits, f)
	}
	if len(hits) == 0 {
		t.Errorf("%s has no %s line for the source %q", m[1], m[2], src)
		return "", false
	}
	if m[3] == "" && len(hits) > 1 {
		t.Errorf("%s has %d %s lines, the source %q names no selector", m[1], len(hits), m[2], src)
		return "", false
	}
	v, ok := hits[0][m[5]]
	if !ok {
		t.Errorf("the %s line of %s has no field %s", m[2], m[1], m[5])
	}
	return v, ok
}

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
	case "dimensions":
		return map[string]bool{"true": true, "false": true}, true
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
			if fv.Type().Elem().Kind() == reflect.Bool {
				enabled := true
				fv.Set(reflect.ValueOf(&enabled))
				break
			}
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

	t.Run("stages", func(t *testing.T) {
		got := map[string][]string{}
		words := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Stages", "operation", "from", "to", "stages", "stage_count") {
			key := cadDocOne(t, r[0], "operation") + " " + cadDocOne(t, r[1], "from") + " " + cadDocOne(t, r[2], "to")
			if _, dup := got[key]; dup {
				t.Errorf("docs/CAD.md lists the stages of %s twice", key)
			}
			var stages []string
			for _, m := range cadDocStageWord.FindAllStringSubmatch(r[3], -1) {
				stages = append(stages, m[1])
				words[m[1]] = true
			}
			got[key] = stages
			n, err := strconv.Atoi(cadDocOne(t, r[4], "stage_count"))
			if err != nil {
				t.Errorf("docs/CAD.md: the stage_count of %s is not an integer: %v", key, err)
			} else if n != len(stages) {
				t.Errorf("docs/CAD.md gives %s stage_count %d for %d stages", key, n, len(stages))
			}
		}
		docKeys := map[string]bool{}
		for k := range got {
			docKeys[k] = true
		}
		code := map[string]bool{}
		for k, route := range cadStageRoutes {
			key := k.op + " " + k.from + " " + k.to
			code[key] = true
			if stages, ok := got[key]; ok && !slices.Equal(stages, route) {
				t.Errorf("docs/CAD.md gives %s the stages %v, the code runs %v", key, stages, route)
			}
		}
		cadDocSameSet(t, "stage routes", docKeys, code)
		cadDocSameSet(t, "stage words", words, cadOpenAPISet(CADStageVocabulary()...))
		registry := map[string]bool{}
		for op, ps := range pairs {
			for _, p := range ps {
				registry[op+" "+p.From+" "+p.To] = true
			}
		}
		cadDocSameSet(t, "stage routes against the registry pairs", docKeys, registry)
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
					want = fmt.Sprint(fv.Elem().Interface())
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
		vars := map[string]time.Duration{
			"CAD_GUARD_TIMEOUT":  CADDefaultGuardTimeout,
			"CAD_MESH_TIMEOUT":   CADDefaultMeshTimeout,
			"CAD_RENDER_TIMEOUT": CADDefaultRenderTimeout,
			"CAD_DXF_TIMEOUT":    CADDefaultDXFTimeout,
			"CAD_RSVG_TIMEOUT":   CADDefaultRsvgTimeout,
			"CAD_ENGINE_TIMEOUT": CADDefaultEngineTimeout,
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

	t.Run("near_white_rule", func(t *testing.T) {
		var limit string
		for _, r := range cadDocTable(t, doc, "## Known limits", "id", "limit", "constant", "value") {
			if r[0] == "near_white" {
				limit = r[1]
			}
		}
		if limit == "" {
			t.Fatalf("docs/CAD.md Known limits has no near_white row")
		}
		if strings.Contains(limit, "any channel at or above") {
			t.Errorf("docs/CAD.md near_white says paint with any channel at or above the cutoff is no ink; a pixel is background only when every channel is at or above %d", cadBackgroundCutoff)
		}
		triples := func(re *regexp.Regexp) [][3]int {
			var out [][3]int
			for _, m := range re.FindAllStringSubmatch(limit, -1) {
				var p [3]int
				for i := range p {
					v, err := strconv.Atoi(m[i+1])
					if err != nil || v > 255 {
						t.Fatalf("docs/CAD.md near_white example %q is not an 8-bit colour", m[0])
					}
					p[i] = v
				}
				out = append(out, p)
			}
			return out
		}
		ink, not := triples(cadDocNearInk), triples(cadDocNearNot)
		if len(ink) == 0 || len(not) == 0 {
			t.Fatalf("docs/CAD.md near_white states %d \"(r, g, b) is ink\" and %d \"(r, g, b) is not\" examples, want at least one of each", len(ink), len(not))
		}
		high, edge := false, false
		for _, p := range ink {
			high = high || max(p[0], p[1], p[2]) >= cadBackgroundCutoff
		}
		for _, p := range not {
			edge = edge || min(p[0], p[1], p[2]) == cadBackgroundCutoff
		}
		if !high {
			t.Errorf("docs/CAD.md near_white has no ink example with a channel at or above %d", cadBackgroundCutoff)
		}
		if !edge {
			t.Errorf("docs/CAD.md near_white has no background example whose smallest channel is %d", cadBackgroundCutoff)
		}
	})

	t.Run("scaling", func(t *testing.T) {
		keda := cadDocKEDACAD(t)
		conc := cadDocConcurrency.FindStringSubmatch(cadDocRead(t, cadDocChart))
		if conc == nil {
			t.Fatalf("%s has no CAD_WORKER_CONCURRENCY", cadDocChart)
		}
		keys := map[string]bool{}
		sources := map[string]bool{}
		decision := ""
		for _, r := range cadDocTable(t, doc, "## Scaling", "item", "value", "source") {
			src := strings.Trim(r[2], "`")
			if sources[src] {
				t.Errorf("docs/CAD.md Scaling names the source %q twice", src)
			}
			sources[src] = true
			var want string
			switch {
			case strings.HasPrefix(src, "values.yaml keda.cad."):
				k := strings.TrimPrefix(src, "values.yaml keda.cad.")
				keys[k] = true
				v, ok := keda[k]
				if !ok {
					t.Errorf("docs/CAD.md Scaling names keda.cad.%s, which %s does not have", k, cadDocValues)
					continue
				}
				want = v
			case src == "configmap CAD_WORKER_CONCURRENCY":
				want = conc[1]
			case strings.HasPrefix(src, "evidence/"):
				v, ok := cadDocEvidenceValue(t, src)
				if !ok {
					continue
				}
				want = v
			default:
				t.Errorf("docs/CAD.md Scaling row %s has the unknown source %q", r[0], src)
				continue
			}
			if r[1] != want {
				t.Errorf("docs/CAD.md Scaling %s = %q, %s gives %q", r[0], r[1], src, want)
			}
			if strings.HasSuffix(src, " DECISION.value") {
				decision = r[1]
			}
		}
		code := map[string]bool{}
		for k := range keda {
			code[k] = true
		}
		cadDocSameSet(t, "keda.cad keys", keys, code)
		for _, s := range append([]string{"configmap CAD_WORKER_CONCURRENCY"}, cadDocScalingEvidence...) {
			if !sources[s] {
				t.Errorf("docs/CAD.md Scaling has no row for %s", s)
			}
		}
		if decision != keda["minReplicaCount"] {
			t.Errorf("docs/CAD.md Scaling decision %q differs from values.yaml keda.cad.minReplicaCount %q", decision, keda["minReplicaCount"])
		}
		if k, _ := cadDocEvidenceValue(t, "evidence/76-09-image-size.txt DECISION.key"); k != "keda_cad_min_replica_count" {
			t.Errorf("the DECISION line decides %q, want keda_cad_min_replica_count", k)
		}
		holds, _ := cadDocEvidenceValue(t, "evidence/76-09-image-size.txt DECISION.premise_holds")
		if r, _ := cadDocEvidenceValue(t, "evidence/76-09-keda-loadproof.txt SUMMARY.result"); r != "PASS" {
			t.Errorf("the loadproof evidence ends %q, want PASS", r)
		}
		for _, step := range []string{"calibration", "timed"} {
			if c, _ := cadDocEvidenceValue(t, "evidence/76-09-keda-loadproof.txt QUIET[step="+step+"].contaminated"); c != "0" {
				t.Errorf("the loadproof %s step is contaminated=%q, its timings do not count", step, c)
			}
		}
		phrases := map[string]string{"1": "the premise holds", "0": "the premise does not hold"}
		sec := cadDocSection(t, doc, "## Scaling")
		for v, p := range phrases {
			if strings.Contains(sec, p) != (v == holds) {
				t.Errorf("docs/CAD.md Scaling and the evidence premise_holds=%s disagree on %q", holds, p)
			}
		}
	})
}
