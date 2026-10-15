package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
)

const mcpDescMigrations = "../db/migrations"

var (
	mcpDescWord     = regexp.MustCompile(`[a-z][a-z0-9_]*`)
	mcpDescRoleList = regexp.MustCompile(`CHECK \(role IN \(([^)]*)\)\)`)
	mcpDescQuoted   = regexp.MustCompile(`'([a-z_]+)'`)
)

func mcpDescTag(t *testing.T, v any, field string) string {
	t.Helper()
	f, ok := reflect.TypeOf(v).FieldByName(field)
	if !ok {
		t.Fatalf("%T has no field %s", v, field)
	}
	tag := f.Tag.Get("jsonschema")
	if tag == "" {
		t.Fatalf("%T.%s has no jsonschema tag", v, field)
	}
	return tag
}

func mcpDescWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range mcpDescWord.FindAllString(s, -1) {
		out[w] = true
	}
	return out
}

func mcpDescSorted(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mcpDescCADOptions() map[string]bool {
	out := map[string]bool{}
	ty := reflect.TypeOf(convert.CADOpts{})
	for i := 0; i < ty.NumField(); i++ {
		out[strings.Split(ty.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	return out
}

func mcpDescReservedRoles(t *testing.T) map[string]bool {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(mcpDescMigrations, "*.sql"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no migrations under %s: %v", mcpDescMigrations, err)
	}
	sort.Strings(names)
	var legal map[string]bool
	for _, name := range names {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mcpDescRoleList.FindAllStringSubmatch(string(b), -1) {
			legal = map[string]bool{}
			for _, q := range mcpDescQuoted.FindAllStringSubmatch(m[1], -1) {
				legal[q[1]] = true
			}
		}
	}
	if len(legal) == 0 {
		t.Fatal("no migration declares the job_outputs role CHECK: the scan read nothing")
	}
	for _, r := range []string{jobs.OutputRolePrimary, jobs.OutputRoleReport, jobs.OutputRoleMeasurements} {
		if !legal[r] {
			t.Fatalf("the role CHECK does not allow %q", r)
		}
		delete(legal, r)
	}
	if len(legal) == 0 {
		t.Fatal("the role CHECK reserves no role beyond the produced ones: the scan read nothing")
	}
	return legal
}

func mcpDescPublished(t *testing.T, mode ResultMode, dir string) (string, map[string]string, string) {
	t.Helper()
	srv := httptest.NewServer(http.NewServeMux())
	defer srv.Close()
	h := newHarness(t, Config{BaseURL: srv.URL, OutputDir: dir, ResultMode: mode}, nil)
	var desc, status string
	props := map[string]string{}
	for tool, err := range h.cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("Tools: %v", err)
		}
		switch tool.Name {
		case "convert_file":
			desc = tool.Description
			raw, err := json.Marshal(tool.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			for k, v := range schema.Properties {
				props[k] = v.Description
			}
		case "get_job_status":
			raw, err := json.Marshal(tool.OutputSchema)
			if err != nil {
				t.Fatal(err)
			}
			status = string(raw)
		}
	}
	if desc == "" || len(props) == 0 || status == "" {
		t.Fatalf("mode %q: convert_file or get_job_status was not published", mode)
	}
	return desc, props, status
}

func TestMCPDescriptionsMatchTheRegistry(t *testing.T) {
	classes := convert.Default.OperationClasses()
	if len(classes) < 2 {
		t.Fatalf("the registry has %d operations: the scan read nothing", len(classes))
	}
	render := map[string]bool{}
	for _, pairs := range classes[convert.OperationRender] {
		for _, p := range pairs {
			render[p.From] = true
			render[p.To] = true
		}
	}
	if len(render) == 0 {
		t.Fatal("the registry has no render pair")
	}

	operation := mcpDescTag(t, ConvertFileInput{}, "Operation")
	opWords := mcpDescWords(operation)
	for _, op := range mcpDescSorted(func() map[string]bool {
		m := map[string]bool{}
		for op := range classes {
			m[op] = true
		}
		return m
	}()) {
		if !opWords[op] {
			t.Errorf("the operation field description does not name %q, a registered operation", op)
		}
	}
	for _, s := range []string{"docs/CAD.md", "GET /v1/formats", "operations"} {
		if !strings.Contains(operation, s) {
			t.Errorf("the operation field description does not mention %q", s)
		}
	}

	opts := mcpDescTag(t, ConvertFileInput{}, "Opts")
	optWords := mcpDescWords(opts)
	for _, o := range mcpDescSorted(mcpDescCADOptions()) {
		if !optWords[o] {
			t.Errorf("the opts field description does not name the CAD option %q", o)
		}
	}
	if !strings.Contains(opts, "docs/CAD.md") {
		t.Error("the opts field description does not point to docs/CAD.md")
	}

	role := mcpDescTag(t, JobOutput{}, "Role")
	roleWords := mcpDescWords(role)
	for _, r := range []string{jobs.OutputRolePrimary, jobs.OutputRoleReport, jobs.OutputRoleMeasurements} {
		if !roleWords[r] {
			t.Errorf("the role field description does not name role %q", r)
		}
	}
	for _, r := range mcpDescSorted(mcpDescReservedRoles(t)) {
		if !roleWords[r] {
			t.Errorf("the role field description does not say that role %q is reserved and never produced", r)
		}
	}
	if !strings.Contains(role, "reserved") || !strings.Contains(role, "never produced") {
		t.Error(`the role field description does not say the reserved roles are "reserved" and "never produced"`)
	}

	for _, mode := range []ResultMode{ResultLocal, ResultRemote} {
		dir := t.TempDir() + "/distinctive-output-dir-marker"
		desc := convertToolDescription(mode, dir)
		words := mcpDescWords(desc)
		if !words[convert.OperationRender] {
			t.Errorf("mode %q: the convert_file description does not name %q", mode, convert.OperationRender)
		}
		for _, f := range mcpDescSorted(render) {
			if !words[f] {
				t.Errorf("mode %q: the convert_file description does not name %q, a format of a render pair", mode, f)
			}
		}
		if !strings.Contains(desc, "docs/CAD.md") {
			t.Errorf("mode %q: the convert_file description does not point to docs/CAD.md", mode)
		}
		if got := strings.Contains(desc, dir); got != (mode == ResultLocal) {
			t.Errorf("mode %q: the convert_file description names the output dir: %v", mode, got)
		}
		published, props, status := mcpDescPublished(t, mode, dir)
		if published != desc {
			t.Errorf("mode %q: the published convert_file description is %q, convertToolDescription returns %q", mode, published, desc)
		}
		if props["operation"] != operation {
			t.Errorf("mode %q: the published operation description is %q, the tag is %q", mode, props["operation"], operation)
		}
		if props["opts"] != opts {
			t.Errorf("mode %q: the published opts description is %q, the tag is %q", mode, props["opts"], opts)
		}
		quoted, _ := json.Marshal(role)
		if !strings.Contains(status, string(quoted)) {
			t.Errorf("mode %q: the published get_job_status schema does not carry the role description", mode)
		}
	}
}
