package worker

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
)

const (
	cadCodesDocPath     = "../../docs/CAD.md"
	cadCodesOpenAPIPath = "../../docs/openapi.yaml"
)

var (
	cadCodesSentinelName = regexp.MustCompile(`^ErrCAD[A-Za-z]*$`)
	cadCodesSyncWord     = regexp.MustCompile(`\bsync\b`)
	cadCodesAsyncWord    = regexp.MustCompile(`\basync\b`)
	cadCodesWord         = regexp.MustCompile(`[a-z][a-z0-9_]*`)
)

var cadCodesSentinels = map[string]struct {
	err  error
	code string
}{
	"ErrCADInputTooLarge":       {convert.ErrCADInputTooLarge, CodeInputTooLarge},
	"ErrCADTooComplex":          {convert.ErrCADTooComplex, CodeCADTooComplex},
	"ErrCADUnreadable":          {convert.ErrCADUnreadable, CodeCADUnreadable},
	"ErrCADEmptyResult":         {convert.ErrCADEmptyResult, CodeCADEmptyResult},
	"ErrCADMeshTimeout":         {convert.ErrCADMeshTimeout, CodeCADMeshTimeout},
	"ErrCADEngineKilled":        {convert.ErrCADEngineKilled, CodeCADTooComplex},
	"ErrCADRenderRejected":      {convert.ErrCADRenderRejected, CodeCADEmptyResult},
	"ErrCADRenderTimeout":       {convert.ErrCADRenderTimeout, CodeCADTooComplex},
	"ErrCADRenderFailed":        {convert.ErrCADRenderFailed, CodeEngineError},
	"ErrCADMeasurementsInvalid": {convert.ErrCADMeasurementsInvalid, CodeEngineError},
	"ErrCADDXFTimeout":          {convert.ErrCADDXFTimeout, CodeCADTooComplex},
	"ErrCADRsvgTimeout":         {convert.ErrCADRsvgTimeout, CodeCADTooComplex},
	"ErrCADDrawingFailed":       {convert.ErrCADDrawingFailed, CodeEngineError},
	"ErrCADNothingDrawable":     {convert.ErrCADNothingDrawable, CodeCADEmptyResult},
	"ErrCADSVGRejected":         {convert.ErrCADSVGRejected, CodeEngineError},
	"ErrCADPDFRejected":         {convert.ErrCADPDFRejected, CodeCADEmptyResult},
	"ErrCADMeasure":             {convert.ErrCADMeasure, CodeEngineError},
}

var cadCodesSync = map[string]bool{
	"invalid_request":        true,
	"unsupported_conversion": true,
	"unsupported_operation":  false,
	"invalid_options":        true,
	"opts_not_applicable":    true,
	"missing_file":           false,
	"unknown_preset":         false,
	"payload_too_large":      true,
	"not_found":              false,
	"invalid_cursor":         false,
	"already_exists":         false,
	"unauthorized":           false,
	"quota_exceeded":         false,
	"rate_limited":           false,
	"inspect_busy":           false,
	"unsupported_package":    false,
	"truncated_cad_file":     true,
	"ambiguous_stl":          true,
	"binary_dxf_unsupported": true,
	"dwg_unsupported":        true,
	"internal_error":         false,
}

func cadCodesParse(t *testing.T, dir string) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, p := range pkgs {
		for name, f := range p.Files {
			files[name] = f
		}
	}
	if len(files) == 0 {
		t.Fatalf("no Go files under %s", dir)
	}
	return files
}

func cadCodesConstStrings(files map[string]*ast.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				v := s.(*ast.ValueSpec)
				for i, n := range v.Names {
					if i >= len(v.Values) {
						continue
					}
					if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if u, err := strconv.Unquote(lit.Value); err == nil {
							out[n.Name] = u
						}
					}
				}
			}
		}
	}
	return out
}

type cadCodesRow struct{ kind, when string }

func cadCodesTable(t *testing.T, doc, header string) map[string]cadCodesRow {
	t.Helper()
	row := regexp.MustCompile("^\\|\\s*`([a-z][a-z0-9_]*)`\\s*\\|([^|]*)\\|(.*)\\|\\s*$")
	rows := map[string]cadCodesRow{}
	in := false
	for _, line := range strings.Split(doc, "\n") {
		if line == header {
			in = true
			continue
		}
		if in && strings.HasPrefix(line, "## ") {
			break
		}
		if !in {
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil {
			if _, dup := rows[m[1]]; dup {
				t.Errorf("%s lists %s twice", header, m[1])
			}
			rows[m[1]] = cadCodesRow{strings.TrimSpace(m[2]), strings.TrimSpace(m[3])}
		}
	}
	if !in {
		t.Fatalf("%s has no %q section", cadCodesDocPath, header)
	}
	return rows
}

func cadCodesSorted(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestCADCodesDocMatchesTheMapping(t *testing.T) {
	declared := map[string]bool{}
	for _, f := range cadCodesParse(t, "../convert") {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.VAR {
				continue
			}
			for _, s := range g.Specs {
				for _, n := range s.(*ast.ValueSpec).Names {
					if cadCodesSentinelName.MatchString(n.Name) {
						declared[n.Name] = true
					}
				}
			}
		}
	}
	if len(declared) < 10 {
		t.Fatalf("parsed %d ErrCAD sentinels from internal/convert: the scan read nothing", len(declared))
	}
	for _, name := range cadCodesSorted(declared) {
		if _, ok := cadCodesSentinels[name]; !ok {
			t.Errorf("internal/convert declares %s, which has no row in cadCodesSentinels: map it through cadFailureCode and document its code", name)
		}
	}
	async := map[string]bool{}
	for name, s := range cadCodesSentinels {
		if !declared[name] {
			t.Errorf("cadCodesSentinels lists %s, which internal/convert does not declare", name)
		}
		code, message := cadFailureCode(s.err)
		if code != s.code {
			t.Errorf("cadFailureCode(%s) = %q, want %q", name, code, s.code)
		}
		if message == "" {
			t.Errorf("cadFailureCode(%s) has no message", name)
		}
		async[code] = true
	}
	fallback, _ := cadFailureCode(errors.New("an error no sentinel wraps"))
	async[fallback] = true

	runner := cadCodesParse(t, ".")
	codes := cadCodesConstStrings(runner)
	for _, f := range runner {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 4 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "finish" {
				return true
			}
			status, ok := call.Args[2].(*ast.SelectorExpr)
			if !ok || status.Sel.Name != "StatusFailed" {
				return true
			}
			if id, ok := call.Args[3].(*ast.Ident); ok && strings.HasPrefix(id.Name, "Code") {
				async[codes[id.Name]] = true
			}
			return true
		})
	}
	reconciled := 0
	for _, f := range cadCodesParse(t, "../reconciler") {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasPrefix(sel.Sel.Name, "MarkFailed") {
				return true
			}
			if lit, ok := call.Args[2].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if u, err := strconv.Unquote(lit.Value); err == nil {
					async[u] = true
					reconciled++
				}
			}
			return true
		})
	}
	if reconciled == 0 {
		t.Fatal("found no code the reconciler marks a job failed with: the scan read nothing")
	}
	delete(async, "")
	known := map[string]bool{}
	for _, c := range AllFailureCodes() {
		known[c] = true
	}
	for _, c := range cadCodesSorted(async) {
		if !known[c] {
			t.Errorf("a cad job can end with %q, which AllFailureCodes does not list", c)
		}
	}

	errcodes := cadCodesConstStrings(cadCodesParse(t, "../errcode"))
	syncValues := map[string]bool{}
	for _, v := range errcodes {
		syncValues[v] = true
	}
	for _, v := range cadCodesSorted(syncValues) {
		if _, ok := cadCodesSync[v]; !ok {
			t.Errorf("internal/errcode declares %q, which cadCodesSync does not classify as a code a cad request can get or not", v)
		}
	}
	sync := map[string]bool{}
	for v, cad := range cadCodesSync {
		if !syncValues[v] {
			t.Errorf("cadCodesSync lists %q, which internal/errcode does not declare", v)
		}
		if cad {
			sync[v] = true
		}
	}
	detected := 0
	for _, f := range cadCodesParse(t, "../api") {
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			k, ok := kv.Key.(*ast.SelectorExpr)
			if !ok || !strings.HasPrefix(k.Sel.Name, "CADReject") {
				return true
			}
			v, ok := kv.Value.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			detected++
			if code := errcodes[v.Sel.Name]; !sync[code] {
				t.Errorf("internal/api answers convert.%s with errcode.%s (%q), which cadCodesSync does not mark as a cad code", k.Sel.Name, v.Sel.Name, code)
			}
			return true
		})
	}
	if detected < 4 {
		t.Fatalf("found %d CADReject mappings in internal/api, want at least 4: the scan read nothing", detected)
	}

	b, err := os.ReadFile(cadCodesDocPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	rows := cadCodesTable(t, doc, "## Error codes")
	want := map[string]bool{}
	for c := range async {
		want[c] = true
	}
	for c := range sync {
		want[c] = true
	}
	for _, c := range cadCodesSorted(want) {
		r, ok := rows[c]
		if !ok {
			t.Errorf("a cad job or request can end with %q, but the Error codes table of docs/CAD.md has no row for it", c)
			continue
		}
		if async[c] && !cadCodesAsyncWord.MatchString(r.kind) {
			t.Errorf("docs/CAD.md row %s has kind %q, want it to say async", c, r.kind)
		}
		if sync[c] && !cadCodesSyncWord.MatchString(r.kind) {
			t.Errorf("docs/CAD.md row %s has kind %q, want it to say sync", c, r.kind)
		}
		if r.when == "" {
			t.Errorf("docs/CAD.md row %s says nothing about when", c)
		}
	}
	for c := range rows {
		if !want[c] {
			t.Errorf("the Error codes table of docs/CAD.md lists %q, which no cad mapping, runner path, reconciler path or cad request produces", c)
		}
	}
	if !strings.Contains(doc, "docs/ERROR-CODES.md") && !strings.Contains(doc, "(ERROR-CODES.md)") {
		t.Error("docs/CAD.md does not link docs/ERROR-CODES.md")
	}

	spec := cadCodesOpenAPIErrorCode(t)
	named := map[string]bool{}
	for _, w := range cadCodesWord.FindAllString(spec, -1) {
		named[w] = true
	}
	for _, c := range cadCodesSorted(async) {
		if !named[c] {
			t.Errorf("a cad job can end with %q, but the error_code description of docs/openapi.yaml does not name it", c)
		}
	}
	for _, w := range cadCodesSorted(named) {
		if strings.HasPrefix(w, "cad_") && !want[w] {
			t.Errorf("the error_code description of docs/openapi.yaml names %q, which no cad job or request can end with", w)
		}
	}
	if !strings.Contains(spec, "docs/CAD.md") {
		t.Error("the error_code description of docs/openapi.yaml does not point to docs/CAD.md")
	}

	outputs := cadCodesOutputs(t, doc)
	for _, role := range []string{jobs.OutputRolePrimary, jobs.OutputRoleMeasurements} {
		if !outputs[role] {
			t.Errorf("the Outputs table of docs/CAD.md has no row for role %q", role)
		}
	}
	for role := range outputs {
		if role != jobs.OutputRolePrimary && role != jobs.OutputRoleMeasurements {
			t.Errorf("the Outputs table of docs/CAD.md lists role %q, which a cad job does not write", role)
		}
	}
}

func cadCodesOutputs(t *testing.T, doc string) map[string]bool {
	t.Helper()
	row := regexp.MustCompile("^\\|\\s*`([a-z][a-z0-9_]*)`\\s*\\|")
	out := map[string]bool{}
	in := false
	for _, line := range strings.Split(doc, "\n") {
		if line == "## Outputs" {
			in = true
			continue
		}
		if in && strings.HasPrefix(line, "## ") {
			break
		}
		if m := row.FindStringSubmatch(line); in && m != nil {
			out[m[1]] = true
		}
	}
	if !in {
		t.Fatalf("%s has no ## Outputs section", cadCodesDocPath)
	}
	return out
}

func cadCodesOpenAPIErrorCode(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(cadCodesOpenAPIPath)
	if err != nil {
		t.Fatal(err)
	}
	var text []string
	state := 0
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch state {
		case 0:
			if line == "    JobStatus:" {
				state = 1
			}
		case 1:
			if trimmed != "" && indent <= 4 {
				t.Fatalf("%s JobStatus has no error_code property", cadCodesOpenAPIPath)
			}
			if line == "        error_code:" {
				state = 2
			}
		case 2:
			if trimmed != "" && indent <= 8 {
				t.Fatalf("%s JobStatus.error_code has no description", cadCodesOpenAPIPath)
			}
			if indent == 10 && strings.HasPrefix(trimmed, "description:") {
				rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
				if rest != ">-" && rest != ">" && rest != "|" {
					return rest
				}
				state = 3
			}
		case 3:
			if trimmed != "" && indent <= 10 {
				return strings.Join(text, "\n")
			}
			text = append(text, trimmed)
		}
	}
	if state != 3 {
		t.Fatalf("%s has no JobStatus.error_code description", cadCodesOpenAPIPath)
	}
	return strings.Join(text, "\n")
}
