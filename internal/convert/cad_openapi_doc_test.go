package convert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/jobs"
)

const (
	cadOpenAPIPath       = "../../docs/openapi.yaml"
	cadOpenAPIMigrations = "../db/migrations"
)

var (
	cadOpenAPIToken    = regexp.MustCompile(`[a-z0-9]+->[a-z0-9]+|[a-z]+`)
	cadOpenAPIRoleList = regexp.MustCompile(`CHECK \(role IN \(([^)]*)\)\)`)
	cadOpenAPIQuoted   = regexp.MustCompile(`'([a-z_]+)'`)
	cadOpenAPIWord     = regexp.MustCompile(`[a-z][a-z0-9_]*`)
)

var cadOpenAPIStageList = regexp.MustCompile(`CHECK \(stage IN \(([^)]*)\)\)`)

func cadOpenAPIRead(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(cadOpenAPIPath)
	if err != nil {
		t.Fatalf("read %s: %v", cadOpenAPIPath, err)
	}
	return strings.Split(string(b), "\n")
}

func cadOpenAPIIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func cadOpenAPIBlock(t *testing.T, lines []string, path ...string) []string {
	t.Helper()
	block := lines
	parent := -1
	for _, key := range path {
		child := -1
		found := -1
		for i, line := range block {
			if strings.TrimSpace(line) == "" {
				continue
			}
			in := cadOpenAPIIndent(line)
			if in <= parent {
				break
			}
			if child < 0 {
				child = in
			}
			if in == child && strings.TrimSpace(line) == key {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("%s has no %s", cadOpenAPIPath, strings.Join(path, " "))
		}
		parent = cadOpenAPIIndent(block[found])
		end := len(block)
		for i := found + 1; i < len(block); i++ {
			if strings.TrimSpace(block[i]) != "" && cadOpenAPIIndent(block[i]) <= parent {
				end = i
				break
			}
		}
		block = block[found+1 : end]
	}
	return block
}

func cadOpenAPIScalars(block []string) map[string]string {
	out := map[string]string{}
	child := -1
	for _, line := range block {
		if strings.TrimSpace(line) == "" {
			continue
		}
		in := cadOpenAPIIndent(line)
		if child < 0 {
			child = in
		}
		if in != child {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

func cadOpenAPIDescription(t *testing.T, block []string, what string) string {
	t.Helper()
	child := -1
	for i, line := range block {
		if strings.TrimSpace(line) == "" {
			continue
		}
		in := cadOpenAPIIndent(line)
		if child < 0 {
			child = in
		}
		if in != child || !strings.HasPrefix(strings.TrimSpace(line), "description:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "description:"))
		if rest != ">-" && rest != ">" && rest != "|" {
			return rest
		}
		var paras []string
		var para []string
		for _, l := range block[i+1:] {
			if strings.TrimSpace(l) != "" && cadOpenAPIIndent(l) <= in {
				break
			}
			if strings.TrimSpace(l) == "" {
				if len(para) > 0 {
					paras = append(paras, strings.Join(para, " "))
					para = nil
				}
				continue
			}
			para = append(para, strings.TrimSpace(l))
		}
		if len(para) > 0 {
			paras = append(paras, strings.Join(para, " "))
		}
		return strings.Join(paras, "\n\n")
	}
	t.Fatalf("%s has no description", what)
	return ""
}

func cadOpenAPIFlowList(t *testing.T, v, what string) []string {
	t.Helper()
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		t.Fatalf("%s is %q, want a flow list [a, b]", what, v)
	}
	var out []string
	for _, s := range strings.Split(strings.Trim(v, "[]"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func cadOpenAPISet(items ...string) map[string]bool {
	out := map[string]bool{}
	for _, s := range items {
		out[s] = true
	}
	return out
}

func cadOpenAPIReservedRoles(t *testing.T) map[string]bool {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(cadOpenAPIMigrations, "*.sql"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no migrations under %s: %v", cadOpenAPIMigrations, err)
	}
	sort.Strings(names)
	var legal map[string]bool
	for _, name := range names {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range cadOpenAPIRoleList.FindAllStringSubmatch(string(b), -1) {
			legal = map[string]bool{}
			for _, q := range cadOpenAPIQuoted.FindAllStringSubmatch(m[1], -1) {
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
	return legal
}

func cadOpenAPIStageCheck(t *testing.T) map[string]bool {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(cadOpenAPIMigrations, "*.sql"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no migrations under %s: %v", cadOpenAPIMigrations, err)
	}
	sort.Strings(names)
	var legal map[string]bool
	for _, name := range names {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range cadOpenAPIStageList.FindAllStringSubmatch(string(b), -1) {
			legal = map[string]bool{}
			for _, q := range cadOpenAPIQuoted.FindAllStringSubmatch(m[1], -1) {
				legal[q[1]] = true
			}
		}
	}
	if len(legal) == 0 {
		t.Fatal("no migration declares the jobs stage CHECK: the scan read nothing")
	}
	return legal
}

func TestCADOpenAPIMatchesTheCode(t *testing.T) {
	lines := cadOpenAPIRead(t)

	t.Run("cadopts", func(t *testing.T) {
		schema := cadOpenAPIBlock(t, lines, "components:", "schemas:", "CADOpts:")
		top := cadOpenAPIScalars(schema)
		if top["type"] != "object" {
			t.Errorf("CADOpts type is %q, want object", top["type"])
		}
		if top["additionalProperties"] != "false" {
			t.Errorf("CADOpts additionalProperties is %q, want false: ParseCADOpts refuses an unknown key", top["additionalProperties"])
		}
		if desc := cadOpenAPIDescription(t, schema, "CADOpts"); !strings.Contains(desc, "docs/CAD.md") {
			t.Error("the CADOpts description does not point to docs/CAD.md")
		}
		props := cadOpenAPIBlock(t, schema, "properties:")
		docProps := map[string]bool{}
		for k := range cadOpenAPIScalars(props) {
			docProps[k] = true
		}
		cadDocSameSet(t, "CADOpts properties", docProps, cadOpenAPISet(cadDocTags()...))
		resolved := CADOpts{}.Resolved()
		for _, tag := range cadDocTags() {
			if !docProps[tag] {
				continue
			}
			p := cadOpenAPIScalars(cadOpenAPIBlock(t, props, tag+":"))
			field := cadDocField(CADOpts{}, tag)
			def := cadDocField(resolved, tag)
			switch field.Kind() {
			case reflect.String:
				if p["type"] != "string" {
					t.Errorf("CADOpts.%s type is %q, want string", tag, p["type"])
				}
				closed, ok := cadDocClosed(tag)
				if !ok {
					t.Fatalf("CADOpts.%s is a string with no closed set in the test", tag)
				}
				enum := cadOpenAPIFlowList(t, p["enum"], "CADOpts."+tag+" enum")
				cadDocSameSet(t, "CADOpts."+tag+" enum", cadOpenAPISet(enum...), closed)
				for _, v := range enum {
					raw, _ := json.Marshal(map[string]string{tag: v})
					if _, err := ParseCADOpts(raw); err != nil {
						t.Errorf("CADOpts.%s enum value %q is refused by ParseCADOpts: %v", tag, v, err)
					}
				}
				if s := def.String(); s != "" {
					if p["default"] != s {
						t.Errorf("CADOpts.%s default is %q, the code resolves %q", tag, p["default"], s)
					}
				} else if d, ok := p["default"]; ok {
					t.Errorf("CADOpts.%s has default %q, the code resolves none", tag, d)
				}
			case reflect.Pointer:
				if p["type"] != "integer" {
					t.Errorf("CADOpts.%s type is %q, want integer", tag, p["type"])
				}
				for key, want := range map[string]int{"minimum": CADMinRenderSize, "maximum": CADMaxRenderSize} {
					if p[key] != strconv.Itoa(want) {
						t.Errorf("CADOpts.%s %s is %q, the code has %d", tag, key, p[key], want)
					}
				}
				for _, n := range []int{CADMinRenderSize - 1, CADMaxRenderSize + 1} {
					if _, err := ParseCADOpts([]byte(`{"` + tag + `":` + strconv.Itoa(n) + `}`)); err == nil {
						t.Errorf("ParseCADOpts accepts %s %d, outside the range the test reads", tag, n)
					}
				}
				if def.IsNil() {
					if d, ok := p["default"]; ok {
						t.Errorf("CADOpts.%s has default %q, the code resolves none", tag, d)
					}
				} else if p["default"] != strconv.Itoa(int(def.Elem().Int())) {
					t.Errorf("CADOpts.%s default is %q, the code resolves %d", tag, p["default"], def.Elem().Int())
				}
			default:
				t.Fatalf("CADOpts.%s has kind %s, which the test cannot read", tag, field.Kind())
			}
		}
		opts := cadOpenAPIBlock(t, lines, "paths:", "/v1/jobs:", "post:", "requestBody:", "content:", "multipart/form-data:", "schema:", "properties:", "opts:")
		if desc := cadOpenAPIDescription(t, opts, "the opts field"); !strings.Contains(desc, "CADOpts") || !strings.Contains(desc, "docs/CAD.md") {
			t.Errorf("the opts field description does not name CADOpts and docs/CAD.md for the cad class: %q", desc)
		}
		for _, schemaName := range []string{"JobCreated:", "JobStatus:"} {
			echo := cadOpenAPIBlock(t, lines, "components:", "schemas:", schemaName, "properties:", "opts:")
			if !strings.Contains(strings.Join(echo, "\n"), `$ref: "#/components/schemas/CADOpts"`) {
				t.Errorf("%s opts does not offer CADOpts, which a cad job echoes", strings.TrimSuffix(schemaName, ":"))
			}
		}
	})

	t.Run("operations", func(t *testing.T) {
		field := cadOpenAPIBlock(t, lines, "paths:", "/v1/jobs:", "post:", "requestBody:", "content:", "multipart/form-data:", "schema:", "properties:", "operation:")
		desc := cadOpenAPIDescription(t, field, "the operation field")
		ops := Default.OperationClasses()
		for op := range ops {
			if !strings.Contains(desc, `"`+op+`"`) {
				t.Errorf("the operation description does not name %q, a registered operation", op)
			}
		}
		var render string
		for _, para := range strings.Split(desc, "\n\n") {
			if strings.HasPrefix(para, `"render"`) {
				render = para
			}
		}
		if render == "" {
			t.Fatal(`the operation description has no paragraph that starts with "render"`)
		}
		if !strings.Contains(render, "docs/CAD.md") {
			t.Error("the render paragraph does not point to docs/CAD.md")
		}
		doc := map[string]bool{}
		current := ""
		for _, tok := range cadOpenAPIToken.FindAllString(render, -1) {
			if _, ok := ops[tok]; ok {
				current = tok
				continue
			}
			if from, to, ok := strings.Cut(tok, "->"); ok {
				if current == "" {
					t.Errorf("the render paragraph names %s before any operation", tok)
				}
				doc[current+" "+from+" "+to] = true
			}
		}
		code := map[string]bool{}
		for op, pairs := range cadDocPairs(t) {
			for _, p := range pairs {
				code[op+" "+p.From+" "+p.To] = true
			}
		}
		cadDocSameSet(t, "cad pairs of the render paragraph", doc, code)
	})

	t.Run("roles", func(t *testing.T) {
		outputs := cadOpenAPIBlock(t, lines, "components:", "schemas:", "JobStatus:", "properties:", "outputs:")
		desc := cadOpenAPIDescription(t, outputs, "JobStatus.outputs")
		words := cadOpenAPISet(cadOpenAPIWord.FindAllString(desc, -1)...)
		for _, r := range []string{jobs.OutputRolePrimary, jobs.OutputRoleReport, jobs.OutputRoleMeasurements} {
			if !strings.Contains(desc, `"`+r+`"`) {
				t.Errorf("the outputs description does not name role %q", r)
			}
		}
		reserved := cadOpenAPIReservedRoles(t)
		if len(reserved) == 0 {
			t.Fatal("the role CHECK reserves no role beyond the produced ones: the scan read nothing")
		}
		for _, r := range cadDocSorted(reserved) {
			if !words[r] {
				t.Errorf("the outputs description does not say that role %q is reserved and never produced", r)
			}
		}
		if !strings.Contains(desc, "reserved") || !strings.Contains(desc, "never produced") {
			t.Error(`the outputs description does not say the reserved roles are "reserved" and "never produced"`)
		}
	})
	t.Run("stages", func(t *testing.T) {
		vocab := cadOpenAPISet(CADStageVocabulary()...)
		if len(vocab) == 0 {
			t.Fatal("CADStageVocabulary is empty: the comparison would read nothing")
		}
		cadDocSameSet(t, "the stage CHECK of the migrations, read as the document, against CADStageVocabulary", cadOpenAPIStageCheck(t), vocab)

		for _, name := range []string{"JobStatus", "ProgressEvent", "ProgressWebhookEvent"} {
			props := cadOpenAPIBlock(t, lines, "components:", "schemas:", name+":", "properties:")
			p := cadOpenAPIScalars(cadOpenAPIBlock(t, props, "stage:"))
			types := cadOpenAPISet(cadOpenAPIFlowList(t, p["type"], name+".stage type")...)
			if !types["string"] || !types[`"null"`] {
				t.Errorf(`%s.stage type is %s, want [string, "null"]`, name, p["type"])
			}
			enum := cadOpenAPISet(cadOpenAPIFlowList(t, p["enum"], name+".stage enum")...)
			if !enum["null"] {
				t.Errorf("%s.stage enum has no null: the schema would refuse the null of a queued cad job", name)
			}
			delete(enum, "null")
			cadDocSameSet(t, name+".stage enum", enum, vocab)
			for _, field := range []string{"stage_index", "stage_count"} {
				q := cadOpenAPIScalars(cadOpenAPIBlock(t, props, field+":"))
				types := cadOpenAPISet(cadOpenAPIFlowList(t, q["type"], name+"."+field+" type")...)
				if !types["integer"] || !types[`"null"`] {
					t.Errorf(`%s.%s type is %s, want [integer, "null"]`, name, field, q["type"])
				}
			}
		}

		stage := cadOpenAPIBlock(t, lines, "components:", "schemas:", "JobStatus:", "properties:", "stage:")
		desc := cadOpenAPIDescription(t, stage, "JobStatus.stage")
		if !strings.Contains(desc, "docs/CAD.md") {
			t.Error("the JobStatus.stage description does not point to docs/CAD.md")
		}
		ops := Default.OperationClasses()
		doc := map[string][]string{}
		for _, para := range strings.Split(desc, "\n\n") {
			toks := cadOpenAPIToken.FindAllString(para, -1)
			if len(toks) < 2 {
				continue
			}
			if _, ok := ops[toks[0]]; !ok {
				continue
			}
			from, to, ok := strings.Cut(toks[1], "->")
			if !ok {
				continue
			}
			key := toks[0] + " " + from + " " + to
			if _, dup := doc[key]; dup {
				t.Errorf("the JobStatus.stage description lists the route %s twice", key)
			}
			doc[key] = toks[2:]
		}
		docKeys := map[string]bool{}
		for k := range doc {
			docKeys[k] = true
		}
		code := map[string]bool{}
		for k, route := range cadStageRoutes {
			key := k.op + " " + k.from + " " + k.to
			code[key] = true
			if stages, ok := doc[key]; ok && !slices.Equal(stages, route) {
				t.Errorf("the JobStatus.stage description gives %s the stages %v, the code runs %v", key, stages, route)
			}
		}
		cadDocSameSet(t, "routes of the JobStatus.stage description", docKeys, code)

		form := cadOpenAPIBlock(t, lines, "paths:", "/v1/jobs:", "post:", "requestBody:", "content:", "multipart/form-data:", "schema:", "properties:", "progress_webhook:")
		if desc := cadOpenAPIDescription(t, form, "the progress_webhook field"); !strings.Contains(desc, "ProgressWebhookEvent") {
			t.Errorf("the progress_webhook field description does not name ProgressWebhookEvent: %q", desc)
		}

		events := cadOpenAPIBlock(t, lines, "paths:", "/v1/jobs/{id}/events:", "get:")
		desc = cadOpenAPIDescription(t, events, "GET /v1/jobs/{id}/events")
		words := cadOpenAPISet(cadOpenAPIWord.FindAllString(desc, -1)...)
		for _, w := range []string{"stage", "active"} {
			if !words[w] {
				t.Errorf("the GET /v1/jobs/{id}/events description does not say %q", w)
			}
		}
		if !strings.Contains(desc, "ProgressEvent") {
			t.Error("the GET /v1/jobs/{id}/events description does not name ProgressEvent")
		}
	})
}
