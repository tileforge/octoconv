package convert

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	cadGuardHelperPath     = "../../cmd/cad-worker/helper/cad-guard"
	cadMeshHelperPath      = "../../cmd/cad-worker/helper/cad-mesh"
	guardTestEntityCap     = 754762
	guardTestNAUOCap       = 1518
	guardTestDXFInserts    = 170031
	guardTestDXFEntities   = 850155
	guardTestDXFNesting    = 16
	guardTestDXFCoordinate = "1e12"
)

type guardResult struct {
	exit    int
	verdict string
	reason  string
	fields  map[string]any
	stdout  string
}

func (r guardResult) number(key string) int {
	v, ok := r.fields[key].(float64)
	if !ok {
		return -1
	}
	return int(v)
}

func requireCADPython(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	return p
}

func runCADGuard(t *testing.T, format string, entityCap, nauoCap int, path string) guardResult {
	t.Helper()
	return runCADGuardArgs(t,
		"--format", format,
		"--max-entities", fmt.Sprint(entityCap),
		"--max-nauo", fmt.Sprint(nauoCap),
		path)
}

func runCADGuardDXF(t *testing.T, path string) guardResult {
	t.Helper()
	return runCADGuardArgs(t, dxfGuardArgs(path)...)
}

func dxfGuardArgs(path string) []string {
	return []string{
		"--format", "dxf",
		"--max-expanded-inserts", fmt.Sprint(guardTestDXFInserts),
		"--max-expanded-entities", fmt.Sprint(guardTestDXFEntities),
		"--max-nesting", fmt.Sprint(guardTestDXFNesting),
		"--max-abs-coordinate", guardTestDXFCoordinate,
		path,
	}
}

func execCADGuard(t *testing.T, args ...string) (int, string) {
	t.Helper()
	py := requireCADPython(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, append([]string{"-IB", cadGuardHelperPath}, args...)...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, stdout.String()
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), stdout.String()
	default:
		t.Fatalf("run cad-guard: %v", err)
	}
	return -1, ""
}

func runCADGuardArgs(t *testing.T, args ...string) guardResult {
	t.Helper()
	exit, out := execCADGuard(t, args...)
	res := guardResult{exit: exit, stdout: out}
	var last string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			last = line
		}
	}
	if !strings.HasPrefix(last, "CADVERDICT ") {
		t.Fatalf("the last stdout line is %q, want a CADVERDICT line (stdout %q)", last, res.stdout)
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(last, "CADVERDICT ")), &res.fields); err != nil {
		t.Fatalf("CADVERDICT is not JSON: %v: %q", err, last)
	}
	res.verdict, _ = res.fields["verdict"].(string)
	res.reason, _ = res.fields["reason"].(string)
	if stage, _ := res.fields["stage"].(string); stage != "guard" {
		t.Errorf("stage = %q, want guard", stage)
	}
	return res
}

func guardStepFile(statements []string) []byte {
	var b bytes.Buffer
	b.WriteString("ISO-10303-21;\nHEADER;\nFILE_DESCRIPTION(('guard test'),'2;1');\nENDSEC;\nDATA;\n")
	for _, s := range statements {
		b.WriteString(s)
		b.WriteString("\n")
	}
	b.WriteString("ENDSEC;\nEND-ISO-10303-21;\n")
	return b.Bytes()
}

func writeGuardFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func writeTrivialStep(t *testing.T, dir, name string, entities int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	w.WriteString("ISO-10303-21;\nHEADER;\nENDSEC;\nDATA;\n")
	for i := 1; i <= entities; i++ {
		fmt.Fprintf(w, "#%d=T();\n", i)
	}
	w.WriteString("ENDSEC;\nEND-ISO-10303-21;\n")
	if err := w.Flush(); err != nil {
		t.Fatalf("flush %s: %v", name, err)
	}
	return p
}

func nauoStatements(first, count, relating, related int) []string {
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, fmt.Sprintf("#%d=NEXT_ASSEMBLY_USAGE_OCCURRENCE('%d','','',#%d,#%d,$);", first+i, i, relating, related))
	}
	return out
}

func doublingNAUO(levels int) []string {
	var out []string
	id := 1000
	for k := 0; k < levels; k++ {
		for twice := 0; twice < 2; twice++ {
			out = append(out, fmt.Sprintf("#%d=NEXT_ASSEMBLY_USAGE_OCCURRENCE('','','',#%d,#%d,$);", id, 100+k, 101+k))
			id++
		}
	}
	return out
}

func doublingMapped(levels int) []string {
	out := []string{
		"#1=AXIS2_PLACEMENT_3D('',#2,#3,#4);",
		"#10=SHAPE_REPRESENTATION('',(#1),#9);",
	}
	prev := 10
	id := 20
	for l := 1; l <= levels; l++ {
		rm, a, b, rep := id, id+1, id+2, id+3
		id += 4
		out = append(out,
			fmt.Sprintf("#%d=REPRESENTATION_MAP(#1,#%d);", rm, prev),
			fmt.Sprintf("#%d=MAPPED_ITEM('',#%d,#1);", a, rm),
			fmt.Sprintf("#%d=MAPPED_ITEM('',#%d,#1);", b, rm),
			fmt.Sprintf("#%d=SHAPE_REPRESENTATION('',(#1,#%d,#%d),#9);", rep, a, b),
		)
		prev = rep
	}
	return out
}

func TestCADGuardEntityBoundary(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	atCap := writeTrivialStep(t, dir, "at_cap.step", guardTestEntityCap)
	overCap := writeTrivialStep(t, dir, "over_cap.step", guardTestEntityCap+1)

	t.Run("exactly at the cap is ok", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, atCap)
		if r.verdict != "ok" || r.exit != 0 || r.number("entities") != guardTestEntityCap {
			t.Errorf("verdict=%q exit=%d entities=%d, want ok/0/%d", r.verdict, r.exit, r.number("entities"), guardTestEntityCap)
		}
	})
	t.Run("one over the cap is too_complex", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, overCap)
		if r.verdict != "too_complex" || r.reason != "entities_over_cap" || r.exit != 4 {
			t.Errorf("verdict=%q reason=%q exit=%d, want too_complex/entities_over_cap/4", r.verdict, r.reason, r.exit)
		}
	})
}

func TestCADGuardNAUOBoundary(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	atCap := writeGuardFile(t, dir, "nauo_at_cap.step", guardStepFile(nauoStatements(10, guardTestNAUOCap, 1, 2)))
	overCap := writeGuardFile(t, dir, "nauo_over_cap.step", guardStepFile(nauoStatements(10, guardTestNAUOCap+1, 1, 2)))

	t.Run("exactly at the cap is ok and a flat file expands to its statement count", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, atCap)
		if r.verdict != "ok" || r.exit != 0 || r.number("nauo") != guardTestNAUOCap || r.number("expanded") != guardTestNAUOCap {
			t.Errorf("verdict=%q exit=%d nauo=%d expanded=%d, want ok/0/%d/%d",
				r.verdict, r.exit, r.number("nauo"), r.number("expanded"), guardTestNAUOCap, guardTestNAUOCap)
		}
	})
	t.Run("one over the cap is too_complex", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, overCap)
		if r.verdict != "too_complex" || r.reason != "nauo_over_cap" || r.exit != 4 {
			t.Errorf("verdict=%q reason=%q exit=%d, want too_complex/nauo_over_cap/4", r.verdict, r.reason, r.exit)
		}
	})
}

func TestCADGuardEvasionFormsCountTheSame(t *testing.T) {
	requireCADPython(t)
	plain, err := os.ReadFile("testdata/cad/synthetic/units_inch.step")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	base := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, "plain.step", plain))
	if base.verdict != "ok" || base.number("entities") <= 0 {
		t.Fatalf("plain file: verdict=%q entities=%d", base.verdict, base.number("entities"))
	}

	variants := map[string][]byte{
		"comment before each statement": bytes.ReplaceAll(plain, []byte(";\n#"), []byte(";\n/* hide */#")),
		"single line":                   bytes.ReplaceAll(plain, []byte("\n"), nil),
		"CR only":                       bytes.ReplaceAll(plain, []byte("\n"), []byte("\r")),
	}
	for name, data := range variants {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(data, plain) {
				t.Fatal("the variant did not change the bytes")
			}
			r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, strings.ReplaceAll(name, " ", "_")+".step", data))
			if r.verdict != "ok" || r.number("entities") != base.number("entities") {
				t.Errorf("verdict=%q entities=%d, want ok/%d", r.verdict, r.number("entities"), base.number("entities"))
			}
		})
	}
}

func TestCADGuardCorpusNeverUndercountsTheLineAnchoredRegex(t *testing.T) {
	requireCADPython(t)
	lineAnchored := regexp.MustCompile(`(?m)(?:^|;)[ \t]*#[0-9]+[ \t]*=`)
	var files []string
	for _, dir := range []string{"public", "synthetic"} {
		for _, pat := range []string{"*.step", "*.stp"} {
			m, err := filepath.Glob(filepath.Join("testdata", "cad", dir, pat))
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, m...)
		}
	}
	if len(files) < 10 {
		t.Fatalf("found %d corpus STEP files, want at least 10", len(files))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		idx := bytes.Index(data, []byte("DATA;"))
		if idx < 0 {
			t.Fatalf("%s has no DATA;", f)
		}
		regexCount := len(lineAnchored.FindAll(data[idx:], -1))
		r := runCADGuard(t, "step", 1<<40, 1<<40, f)
		if r.verdict != "ok" {
			t.Errorf("%s: verdict %q reason %q", filepath.Base(f), r.verdict, r.reason)
			continue
		}
		if got := r.number("entities"); got < regexCount {
			t.Errorf("%s: entities %d < line-anchored regex count %d", filepath.Base(f), got, regexCount)
		}
	}
}

func TestCADGuardCannotCountIsUnreadableNeverOK(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	cases := map[string]string{
		"empty file":      writeGuardFile(t, dir, "empty.step", nil),
		"no DATA section": writeGuardFile(t, dir, "nodata.step", []byte("ISO-10303-21;\nHEADER;\nENDSEC;\nEND-ISO-10303-21;\n")),
		"a directory":     dir,
		"a missing path":  filepath.Join(dir, "missing.step"),
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, path)
			if r.verdict != "unreadable" || r.exit != 2 {
				t.Errorf("verdict=%q exit=%d, want unreadable/2", r.verdict, r.exit)
			}
			if r.reason != "no_data_section" && r.reason != "cannot_count" {
				t.Errorf("reason=%q, want no_data_section or cannot_count", r.reason)
			}
		})
	}
	t.Run("empty iges", func(t *testing.T) {
		r := runCADGuard(t, "iges", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, "empty.iges", nil))
		if r.verdict != "unreadable" || r.exit != 2 {
			t.Errorf("verdict=%q exit=%d, want unreadable/2", r.verdict, r.exit)
		}
	})
}

func TestCADGuardIGESCountsRecords(t *testing.T) {
	requireCADPython(t)
	for _, name := range []string{"surfaces_only.iges", "iges_no_newlines.iges"} {
		t.Run(name, func(t *testing.T) {
			r := runCADGuard(t, "iges", guardTestEntityCap, guardTestNAUOCap, filepath.Join("testdata", "cad", "synthetic", name))
			if r.verdict != "ok" || r.exit != 0 || r.number("records") <= 0 {
				t.Errorf("verdict=%q exit=%d records=%d, want ok/0/>0", r.verdict, r.exit, r.number("records"))
			}
		})
	}
}

func TestCADGuardExpandedInstancesBoundTheNAUOHierarchy(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()

	t.Run("2^11 expanded instances from 22 statements is too_complex", func(t *testing.T) {
		stmts := doublingNAUO(11)
		if len(stmts) != 22 {
			t.Fatalf("fixture has %d NAUO statements, want 22", len(stmts))
		}
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, "doubling11.step", guardStepFile(stmts)))
		if r.verdict != "too_complex" || r.reason != "expansion_over_cap" || r.exit != 4 || r.number("nauo") != 22 {
			t.Errorf("verdict=%q reason=%q exit=%d nauo=%d, want too_complex/expansion_over_cap/4/22", r.verdict, r.reason, r.exit, r.number("nauo"))
		}
	})
	t.Run("a hierarchy under the cap is ok and reports its expanded count", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, "doubling9.step", guardStepFile(doublingNAUO(9))))
		if r.verdict != "ok" || r.number("expanded") != 1022 {
			t.Errorf("verdict=%q expanded=%d, want ok/1022", r.verdict, r.number("expanded"))
		}
	})
	t.Run("the committed four-instance assembly stays ok", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, filepath.Join("testdata", "cad", "synthetic", "assembly_nauo4.step"))
		if r.verdict != "ok" || r.number("expanded") != 4 {
			t.Errorf("verdict=%q expanded=%d, want ok/4", r.verdict, r.number("expanded"))
		}
	})
	t.Run("a token that is not an edge is counted and adds no expansion", func(t *testing.T) {
		stmts := []string{"#5=PRODUCT('NEXT_ASSEMBLY_USAGE_OCCURRENCE','','',());"}
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, "tokenonly.step", guardStepFile(stmts)))
		if r.verdict != "ok" || r.number("nauo") != 1 || r.number("expanded") != 0 {
			t.Errorf("verdict=%q nauo=%d expanded=%d, want ok/1/0", r.verdict, r.number("nauo"), r.number("expanded"))
		}
	})
}

func TestCADGuardExpandedInstancesBoundMappedItemNesting(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()

	t.Run("2^10 expanded instances with no NAUO at all is too_complex", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, "mapped10.step", guardStepFile(doublingMapped(10))))
		if r.verdict != "too_complex" || r.reason != "mapped_item_expansion_over_cap" || r.exit != 4 || r.number("nauo") != 0 {
			t.Errorf("verdict=%q reason=%q exit=%d nauo=%d, want too_complex/mapped_item_expansion_over_cap/4/0",
				r.verdict, r.reason, r.exit, r.number("nauo"))
		}
	})
	t.Run("nesting under the cap is ok", func(t *testing.T) {
		r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, "mapped9.step", guardStepFile(doublingMapped(9))))
		if r.verdict != "ok" || r.number("expanded") != 1022 {
			t.Errorf("verdict=%q expanded=%d, want ok/1022", r.verdict, r.number("expanded"))
		}
	})
}

func TestCADGuardExpansionCountTerminatesOnCyclicEdges(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	cases := map[string][]string{
		"self reference": {"#1=NEXT_ASSEMBLY_USAGE_OCCURRENCE('','','',#5,#5,$);"},
		"two-node cycle": {
			"#1=NEXT_ASSEMBLY_USAGE_OCCURRENCE('','','',#5,#6,$);",
			"#2=NEXT_ASSEMBLY_USAGE_OCCURRENCE('','','',#6,#5,$);",
		},
	}
	for name, stmts := range cases {
		t.Run(name, func(t *testing.T) {
			r := runCADGuard(t, "step", guardTestEntityCap, guardTestNAUOCap, writeGuardFile(t, dir, strings.ReplaceAll(name, " ", "_")+".step", guardStepFile(stmts)))
			if r.verdict != "ok" {
				t.Errorf("verdict=%q reason=%q, want ok: a cycle is left to the reader", r.verdict, r.reason)
			}
		})
	}
}

func TestCADGuardHelpersParseAsPython311(t *testing.T) {
	py := requireCADPython(t)
	for _, helper := range []string{cadGuardHelperPath, cadMeshHelperPath} {
		t.Run(filepath.Base(helper), func(t *testing.T) {
			out, err := exec.Command(py, "-c",
				"import ast, sys\nast.parse(open(sys.argv[1]).read(), feature_version=(3, 11))", helper).CombinedOutput()
			if err != nil {
				t.Errorf("%s does not parse as Python 3.11: %v: %s", helper, err, out)
			}
		})
	}
}

func TestCADGuardHelperHoldsNoNumbersOfItsOwn(t *testing.T) {
	data, err := os.ReadFile(cadGuardHelperPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"754762", "1518", fmt.Sprint(guardTestDXFInserts), fmt.Sprint(guardTestDXFEntities)} {
		if bytes.Contains(data, []byte(n)) {
			t.Errorf("cad-guard contains the cap %s: the caps arrive on argv only", n)
		}
	}
	caps := []string{"754762", "1518", fmt.Sprint(guardTestDXFInserts), fmt.Sprint(guardTestDXFEntities), fmt.Sprint(guardTestDXFNesting), guardTestDXFCoordinate}
	t.Logf("forbidden cap literals: %s", strings.Join(caps, " "))
	py := requireCADPython(t)
	script := `import ast, io, sys, tokenize
caps = {ast.literal_eval(c) for c in sys.argv[2:]}
src = open(sys.argv[1], encoding="utf-8").read()
for tok in tokenize.generate_tokens(io.StringIO(src).readline):
    if tok.type == tokenize.NUMBER and ast.literal_eval(tok.string) in caps:
        print("%d:%s" % (tok.start[0], tok.string))
`
	out, err := exec.Command(py, append([]string{"-c", script, cadGuardHelperPath}, caps...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("tokenize cad-guard: %v: %s", err, out)
	}
	if hits := strings.TrimSpace(string(out)); hits != "" {
		t.Errorf("cad-guard holds numeric literals equal to a cap (line:literal): %s", hits)
	}
	if !bytes.HasPrefix(data, []byte("#!/usr/bin/python3 -IB\n")) {
		t.Errorf("cad-guard shebang is %q", bytes.SplitN(data, []byte("\n"), 2)[0])
	}
	for _, mod := range []string{"OCP", "numpy", "ezdxf"} {
		if bytes.Contains(data, []byte("import "+mod)) || bytes.Contains(data, []byte("from "+mod)) {
			t.Errorf("cad-guard imports %s: the guard is standard library only", mod)
		}
	}
}

type dxfPair struct {
	code  string
	value string
}

func dxfFile(acadver, codepage string, blocks, entities []dxfPair, extra ...[]dxfPair) []byte {
	var pairs []dxfPair
	pairs = append(pairs, dxfPair{"0", "SECTION"}, dxfPair{"2", "HEADER"})
	if acadver != "" {
		pairs = append(pairs, dxfPair{"9", "$ACADVER"}, dxfPair{"1", acadver})
	}
	if codepage != "" {
		pairs = append(pairs, dxfPair{"9", "$DWGCODEPAGE"}, dxfPair{"3", codepage})
	}
	pairs = append(pairs, dxfPair{"0", "ENDSEC"})
	for _, section := range extra {
		pairs = append(pairs, section...)
	}
	if blocks != nil {
		pairs = append(pairs, dxfPair{"0", "SECTION"}, dxfPair{"2", "BLOCKS"})
		pairs = append(pairs, blocks...)
		pairs = append(pairs, dxfPair{"0", "ENDSEC"})
	}
	pairs = append(pairs, dxfPair{"0", "SECTION"}, dxfPair{"2", "ENTITIES"})
	pairs = append(pairs, entities...)
	pairs = append(pairs, dxfPair{"0", "ENDSEC"}, dxfPair{"0", "EOF"})
	var b bytes.Buffer
	for _, p := range pairs {
		b.WriteString(p.code)
		b.WriteString("\n")
		b.WriteString(p.value)
		b.WriteString("\n")
	}
	return b.Bytes()
}

func dxfLinePairs() []dxfPair {
	return []dxfPair{{"0", "LINE"}, {"8", "0"}, {"10", "0.0"}, {"20", "0.0"}, {"30", "0.0"}, {"11", "10.0"}, {"21", "0.0"}, {"31", "0.0"}}
}

func dxfBlockPairs(name string, body ...[]dxfPair) []dxfPair {
	out := []dxfPair{{"0", "BLOCK"}, {"8", "0"}, {"2", name}, {"70", "0"}, {"10", "0.0"}, {"20", "0.0"}, {"30", "0.0"}}
	for _, b := range body {
		out = append(out, b...)
	}
	return append(out, dxfPair{"0", "ENDBLK"}, dxfPair{"8", "0"})
}

func dxfInsertPairs(name string, extra ...dxfPair) []dxfPair {
	out := []dxfPair{{"0", "INSERT"}, {"8", "0"}, {"2", name}, {"10", "1.0"}, {"20", "1.0"}, {"30", "0.0"}}
	return append(out, extra...)
}

func dxfFixturePath(name string) string {
	return filepath.Join("testdata", "cad", "dxf", name)
}

func dxfCorpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"public", "synthetic"} {
		m, err := filepath.Glob(filepath.Join("testdata", "cad", dir, "*.dxf"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) != 12 {
		t.Fatalf("found %d committed corpus DXF files, want 12", len(files))
	}
	return files
}

func requireGuardCountersWithinCaps(t *testing.T, r guardResult) {
	t.Helper()
	limits := map[string]int{
		"expanded_inserts":  guardTestDXFInserts + 1,
		"expanded_entities": guardTestDXFEntities + 1,
		"entities":          guardTestDXFEntities + 1,
		"nesting":           guardTestDXFNesting + 1,
	}
	for key, limit := range limits {
		if _, ok := r.fields[key]; !ok {
			continue
		}
		if got := r.number(key); got < 0 || got > limit {
			t.Errorf("counter %s = %d, want 0..%d", key, got, limit)
		}
	}
}

func TestCADGuardDXFAdmitsCorpusAndDrawingFixtures(t *testing.T) {
	requireCADPython(t)
	files := dxfCorpusFiles(t)
	maxNesting := 0
	for _, f := range files {
		t.Run("corpus/"+filepath.Base(f), func(t *testing.T) {
			r := runCADGuardDXF(t, f)
			if r.verdict != "ok" || r.exit != 0 {
				t.Fatalf("verdict=%q reason=%q exit=%d, want ok/0 (%v)", r.verdict, r.reason, r.exit, r.fields)
			}
			for _, key := range []string{"entities", "expanded_inserts", "expanded_entities", "nesting", "blocks"} {
				if _, ok := r.fields[key]; !ok {
					t.Errorf("counter %s missing from %v", key, r.fields)
				}
			}
			requireGuardCountersWithinCaps(t, r)
			if n := r.number("nesting"); n > maxNesting {
				maxNesting = n
			}
		})
	}
	if maxNesting > 4 {
		t.Errorf("corpus nesting maximum is %d, want at most 4", maxNesting)
	}
	t.Logf("corpus nesting maximum %d", maxNesting)
	fixtures := []string{
		"text_cases.dxf", "text_illegal_chars.dxf", "layers_off_frozen.dxf", "all_hidden.dxf", "never_resolved.dxf",
		"wide.dxf", "tall.dxf", "far_point.dxf", "single_point.dxf", "xline_ray.dxf",
		"units_0.dxf", "units_1.dxf", "units_2.dxf", "units_3.dxf", "units_4.dxf", "units_5.dxf", "units_6.dxf",
		"units_13.dxf", "units_21.dxf", "units_missing.dxf",
		"header_far_extents.dxf", "wrong_extents.dxf",
		"nonfinite_header.dxf", "nonfinite_tables.dxf", "nonfinite_objects.dxf",
	}
	for _, name := range fixtures {
		t.Run("fixture/"+name, func(t *testing.T) {
			r := runCADGuardDXF(t, dxfFixturePath(name))
			if r.verdict != "ok" || r.exit != 0 {
				t.Errorf("verdict=%q reason=%q exit=%d, want ok/0 (%v)", r.verdict, r.reason, r.exit, r.fields)
			}
			requireGuardCountersWithinCaps(t, r)
		})
	}
}

func TestCADGuardDXFRejectsCyclesAndDepth(t *testing.T) {
	requireCADPython(t)
	for _, name := range []string{"cyclic_self.dxf", "cyclic_ab.dxf", "cyclic_unreferenced.dxf", "cyclic_case.dxf"} {
		t.Run(name, func(t *testing.T) {
			r := runCADGuardDXF(t, dxfFixturePath(name))
			if r.verdict != "too_complex" || r.reason != "cyclic_block_reference" || r.exit != 4 {
				t.Errorf("verdict=%q reason=%q exit=%d, want too_complex/cyclic_block_reference/4", r.verdict, r.reason, r.exit)
			}
		})
	}
	t.Run("nest_d16 is at the bound", func(t *testing.T) {
		r := runCADGuardDXF(t, dxfFixturePath("nest_d16.dxf"))
		if r.verdict != "ok" || r.exit != 0 || r.number("nesting") != 16 {
			t.Errorf("verdict=%q exit=%d nesting=%d, want ok/0/16", r.verdict, r.exit, r.number("nesting"))
		}
	})
	t.Run("nest_d17 is one over", func(t *testing.T) {
		r := runCADGuardDXF(t, dxfFixturePath("nest_d17.dxf"))
		if r.verdict != "too_complex" || r.reason != "nesting_too_deep" || r.exit != 4 || r.number("nesting") != 17 {
			t.Errorf("verdict=%q reason=%q exit=%d nesting=%d, want too_complex/nesting_too_deep/4/17", r.verdict, r.reason, r.exit, r.number("nesting"))
		}
		requireGuardCountersWithinCaps(t, r)
	})
}

func TestCADGuardDXFExpansionCaps(t *testing.T) {
	requireCADPython(t)
	for _, name := range []string{"minsert_over_inserts.dxf", "dimension_minsert.dxf"} {
		t.Run(name, func(t *testing.T) {
			r := runCADGuardDXF(t, dxfFixturePath(name))
			if r.verdict != "too_complex" || r.reason != "expanded_inserts_over_cap" || r.exit != 4 {
				t.Errorf("verdict=%q reason=%q exit=%d, want too_complex/expanded_inserts_over_cap/4", r.verdict, r.reason, r.exit)
			}
			if r.number("expanded_inserts") != guardTestDXFInserts+1 {
				t.Errorf("expanded_inserts=%d, want the saturated %d", r.number("expanded_inserts"), guardTestDXFInserts+1)
			}
			requireGuardCountersWithinCaps(t, r)
		})
	}
	t.Run("entities_over", func(t *testing.T) {
		r := runCADGuardDXF(t, dxfFixturePath("entities_over.dxf"))
		if r.verdict != "too_complex" || r.reason != "expanded_entities_over_cap" || r.exit != 4 {
			t.Errorf("verdict=%q reason=%q exit=%d, want too_complex/expanded_entities_over_cap/4", r.verdict, r.reason, r.exit)
		}
		if got := r.number("expanded_inserts"); got != 851 {
			t.Errorf("expanded_inserts=%d, want 851 (under the insert cap)", got)
		}
		requireGuardCountersWithinCaps(t, r)
	})
	dir := t.TempDir()
	cell := dxfBlockPairs("CELL", dxfLinePairs())
	grid := func(cols, rows string) string {
		return writeGuardFile(t, dir, "grid_"+cols+"x"+rows+".dxf", dxfFile("AC1009", "ANSI_1252", cell,
			dxfInsertPairs("CELL", dxfPair{"70", cols}, dxfPair{"71", rows}, dxfPair{"44", "1.0"}, dxfPair{"45", "1.0"})))
	}
	t.Run("a grid of exactly the cap is ok", func(t *testing.T) {
		r := runCADGuardDXF(t, grid("1", fmt.Sprint(guardTestDXFInserts)))
		if r.verdict != "ok" || r.number("expanded_inserts") != guardTestDXFInserts || r.number("expanded_entities") != guardTestDXFInserts+1 {
			t.Errorf("verdict=%q expanded_inserts=%d expanded_entities=%d, want ok/%d/%d (one LINE per cell plus the INSERT)", r.verdict,
				r.number("expanded_inserts"), r.number("expanded_entities"), guardTestDXFInserts, guardTestDXFInserts+1)
		}
	})
	t.Run("a grid of 1e300 rows saturates", func(t *testing.T) {
		r := runCADGuardDXF(t, grid("2", "1e300"))
		if r.verdict != "too_complex" || r.reason != "expanded_inserts_over_cap" || r.number("expanded_inserts") != guardTestDXFInserts+1 {
			t.Errorf("verdict=%q reason=%q expanded_inserts=%d", r.verdict, r.reason, r.number("expanded_inserts"))
		}
		requireGuardCountersWithinCaps(t, r)
	})
}

func TestCADGuardDXFNumericRulesInsideEntitiesAndBlocks(t *testing.T) {
	requireCADPython(t)
	for _, name := range []string{"nonnumeric_abc.dxf", "nonnumeric_comma.dxf", "nonnumeric_nan.dxf", "nonnumeric_inf.dxf", "nonnumeric_underscore.dxf"} {
		t.Run(name, func(t *testing.T) {
			r := runCADGuardDXF(t, dxfFixturePath(name))
			if r.verdict != "unreadable" || r.reason != "non_numeric_value" || r.exit != 2 {
				t.Errorf("verdict=%q reason=%q exit=%d, want unreadable/non_numeric_value/2", r.verdict, r.reason, r.exit)
			}
			if r.number("line") <= 0 || r.number("code") != 10 {
				t.Errorf("line=%d code=%d, want a line and code 10", r.number("line"), r.number("code"))
			}
		})
	}
	for _, name := range []string{"coord_1e300.dxf", "coord_1e400.dxf"} {
		t.Run(name, func(t *testing.T) {
			r := runCADGuardDXF(t, dxfFixturePath(name))
			if r.verdict != "unreadable" || r.reason != "coordinate_out_of_range" || r.exit != 2 || r.number("line") <= 0 {
				t.Errorf("verdict=%q reason=%q exit=%d line=%d, want unreadable/coordinate_out_of_range/2", r.verdict, r.reason, r.exit, r.number("line"))
			}
		})
	}
	for _, name := range []string{"dxf_binary.dxf", "dxf_bom.dxf"} {
		t.Run(name, func(t *testing.T) {
			r := runCADGuardDXF(t, filepath.Join("testdata", "cad", "reject", name))
			if r.verdict != "unreadable" || r.reason != "bad_group_code" || r.exit != 2 {
				t.Errorf("verdict=%q reason=%q exit=%d, want unreadable/bad_group_code/2", r.verdict, r.reason, r.exit)
			}
		})
	}
	dir := t.TempDir()
	line := func(x string) []dxfPair {
		return []dxfPair{{"0", "LINE"}, {"8", "0"}, {"10", x}, {"20", "0.0"}, {"30", "0.0"}, {"11", "1.0"}, {"21", "0.0"}, {"31", "0.0"}}
	}
	cases := []struct {
		name, reason string
		data         []byte
	}{
		{"coordinate exactly at the bound", "", dxfFile("AC1009", "", nil, line("1e12"))},
		{"coordinate just above the bound", "coordinate_out_of_range", dxfFile("AC1009", "", nil, line("1.0000000001e12"))},
		{"negative coordinate above the bound", "coordinate_out_of_range", dxfFile("AC1009", "", nil, line("-2e12"))},
		{"nan inside a block", "non_numeric_value", dxfFile("AC1009", "", dxfBlockPairs("B", line("nan")), nil)},
		{"-inf inside a block", "non_numeric_value", dxfFile("AC1009", "", dxfBlockPairs("B", line("-inf")), nil)},
		{"overflowing radius", "non_numeric_value", dxfFile("AC1009", "", nil,
			[]dxfPair{{"0", "CIRCLE"}, {"8", "0"}, {"10", "0"}, {"20", "0"}, {"30", "0"}, {"40", "1e400"}})},
		{"integer code holding 2.0", "", dxfFile("AC1009", "", dxfBlockPairs("C", dxfLinePairs()),
			dxfInsertPairs("C", dxfPair{"70", "2.0"}))},
		{"integer code holding x", "non_numeric_value", dxfFile("AC1009", "", dxfBlockPairs("C", dxfLinePairs()),
			dxfInsertPairs("C", dxfPair{"70", "x"}))},
		{"nan in the header", "", dxfFile("AC1009", "", nil, dxfLinePairs(),
			[]dxfPair{{"0", "SECTION"}, {"2", "TABLES"}, {"0", "TABLE"}, {"2", "VPORT"}, {"0", "VPORT"}, {"40", "nan"}, {"0", "ENDTAB"}, {"0", "ENDSEC"}})},
		{"a group code that is not an integer", "bad_group_code", []byte("0\nSECTION\n2\nENTITIES\n1_0\n0.0\n0\nENDSEC\n0\nEOF\n")},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runCADGuardDXF(t, writeGuardFile(t, dir, fmt.Sprintf("numeric_%d.dxf", i), c.data))
			want := c.reason
			if want == "" && (r.verdict != "ok" || r.exit != 0) {
				t.Errorf("verdict=%q reason=%q exit=%d, want ok/0", r.verdict, r.reason, r.exit)
			}
			if want != "" && (r.verdict != "unreadable" || r.reason != want || r.exit != 2) {
				t.Errorf("verdict=%q reason=%q exit=%d, want unreadable/%s/2", r.verdict, r.reason, r.exit, want)
			}
		})
	}
}

func TestCADGuardDXFKeysBlocksLikeEzdxf(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	selfRef := func(acadver, codepage, block, insert string) []byte {
		return dxfFile(acadver, codepage, dxfBlockPairs(block, dxfLinePairs(), dxfInsertPairs(insert)), dxfInsertPairs(block))
	}
	cases := []struct {
		name   string
		cyclic bool
		data   []byte
	}{
		{"ascii case folds", true, selfRef("AC1009", "ANSI_1252", "A", "a")},
		{"cp1251 capital and small IO fold", true, selfRef("AC1015", "ANSI_1251", "\xa8", "\xb8")},
		{"the same bytes under cp1252 do not fold", false, selfRef("AC1015", "ANSI_1252", "\xa8", "\xb8")},
		{"utf-8 Cyrillic from AC1032", true, selfRef("AC1032", "ANSI_1252", "Ж", "ж")},
		{"utf-8 Kelvin sign folds to k", true, selfRef("AC1032", "ANSI_1252", "k", "K")},
		{"utf-8 Greek from AC1021", true, selfRef("AC1021", "ANSI_1253", "Σ", "σ")},
		{"cp1253 Greek before AC1021", true, selfRef("AC1018", "ANSI_1253", "\xd3", "\xf3")},
		{"a lower-case version string sorts above AC1021", true, selfRef("ac1015", "ANSI_1252", "Ж", "ж")},
		{"a trailing space is part of the name", false, selfRef("AC1009", "ANSI_1252", "A ", "A")},
		{"a leading space is part of the name", false, selfRef("AC1009", "ANSI_1252", " A", "A")},
		{"a \\U+ escape matches its literal lower case", true, selfRef("AC1032", "ANSI_1252", "\\U+0416", "\\u+0416")},
		{"a \\U+ escape is not decoded", false, selfRef("AC1032", "ANSI_1252", "\\U+0416", "ж")},
		{"model space inserted into itself", true, dxfFile("AC1009", "", nil, dxfInsertPairs("*Model_Space"))},
		{"a nameless block takes its owner record name", true, dxfFile("AC1015", "ANSI_1252",
			[]dxfPair{{"0", "BLOCK"}, {"5", "20"}, {"330", "1F"}, {"100", "AcDbEntity"}, {"8", "0"}, {"100", "AcDbBlockBegin"}, {"70", "0"},
				{"10", "0.0"}, {"20", "0.0"}, {"30", "0.0"}, {"0", "INSERT"}, {"8", "0"}, {"2", "R"}, {"10", "0"}, {"20", "0"}, {"30", "0"},
				{"0", "ENDBLK"}, {"8", "0"}},
			dxfInsertPairs("R"),
			[]dxfPair{{"0", "SECTION"}, {"2", "TABLES"}, {"0", "TABLE"}, {"2", "BLOCK_RECORD"},
				{"0", "BLOCK_RECORD"}, {"5", "1F"}, {"100", "AcDbSymbolTableRecord"}, {"100", "AcDbBlockTableRecord"}, {"2", "R"},
				{"0", "ENDTAB"}, {"0", "ENDSEC"}})},
		{"a dimension geometry block holding its dimension", true, dxfFile("AC1009", "",
			append(dxfBlockPairs("*D1", dxfInsertPairs("HOST")),
				dxfBlockPairs("HOST", []dxfPair{{"0", "DIMENSION"}, {"8", "0"}, {"2", "*d1"}, {"10", "0"}, {"20", "0"}, {"30", "0"}, {"70", "0"}})...),
			dxfInsertPairs("HOST"))},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runCADGuardDXF(t, writeGuardFile(t, dir, fmt.Sprintf("keys_%d.dxf", i), c.data))
			if c.cyclic && (r.verdict != "too_complex" || r.reason != "cyclic_block_reference" || r.exit != 4) {
				t.Errorf("verdict=%q reason=%q exit=%d, want too_complex/cyclic_block_reference/4", r.verdict, r.reason, r.exit)
			}
			if !c.cyclic && (r.verdict != "ok" || r.exit != 0) {
				t.Errorf("verdict=%q reason=%q exit=%d, want ok/0", r.verdict, r.reason, r.exit)
			}
		})
	}
}

func TestCADGuardDXFGraphWorkIsIterative(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	const depth = 5000
	var chain, ring []dxfPair
	for i := 0; i < depth; i++ {
		chain = append(chain, dxfBlockPairs(fmt.Sprintf("C%d", i), dxfInsertPairs(fmt.Sprintf("C%d", i+1)))...)
		ring = append(ring, dxfBlockPairs(fmt.Sprintf("R%d", i), dxfInsertPairs(fmt.Sprintf("R%d", (i+1)%depth)))...)
	}
	t.Run("a chain far deeper than the recursion limit", func(t *testing.T) {
		r := runCADGuardDXF(t, writeGuardFile(t, dir, "chain.dxf", dxfFile("AC1009", "", chain, dxfInsertPairs("C0"))))
		if r.verdict != "too_complex" || r.reason != "nesting_too_deep" || r.number("nesting") != guardTestDXFNesting+1 || r.number("blocks") != depth {
			t.Errorf("verdict=%q reason=%q nesting=%d blocks=%d", r.verdict, r.reason, r.number("nesting"), r.number("blocks"))
		}
	})
	t.Run("a ring far longer than the recursion limit", func(t *testing.T) {
		r := runCADGuardDXF(t, writeGuardFile(t, dir, "ring.dxf", dxfFile("AC1009", "", ring, dxfLinePairs())))
		if r.verdict != "too_complex" || r.reason != "cyclic_block_reference" {
			t.Errorf("verdict=%q reason=%q", r.verdict, r.reason)
		}
	})
}

func TestCADGuardDXFLineEndingsAndCommentsDoNotChangeCounts(t *testing.T) {
	requireCADPython(t)
	plain, err := os.ReadFile(filepath.Join("testdata", "cad", "synthetic", "blocks_minsert.dxf"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	base := runCADGuardDXF(t, writeGuardFile(t, dir, "plain.dxf", plain))
	if base.verdict != "ok" || base.number("expanded_inserts") <= 0 {
		t.Fatalf("plain file: verdict=%q expanded_inserts=%d", base.verdict, base.number("expanded_inserts"))
	}
	lf := bytes.ReplaceAll(plain, []byte("\r\n"), []byte("\n"))
	variants := map[string][]byte{
		"LF only":  lf,
		"CR only":  bytes.ReplaceAll(lf, []byte("\n"), []byte("\r")),
		"CRLF":     bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n")),
		"comments": bytes.ReplaceAll(lf, []byte("\n  0\nINSERT\n"), []byte("\n999\nnan\n  0\nINSERT\n999\n1_0\n")),
	}
	for name, data := range variants {
		t.Run(name, func(t *testing.T) {
			r := runCADGuardDXF(t, writeGuardFile(t, dir, strings.ReplaceAll(name, " ", "_")+".dxf", data))
			for _, key := range []string{"entities", "expanded_inserts", "expanded_entities", "nesting", "blocks"} {
				if r.verdict != "ok" || r.number(key) != base.number(key) {
					t.Errorf("verdict=%q %s=%d, want ok/%d", r.verdict, key, r.number(key), base.number(key))
				}
			}
		})
	}
}

func TestCADGuardDXFUsage(t *testing.T) {
	requireCADPython(t)
	path := dxfFixturePath("wide.dxf")
	cases := map[string][]string{
		"step without --max-entities":      {"--format", "step", "--max-nauo", "1", path},
		"step without --max-nauo":          {"--format", "step", "--max-entities", "1", path},
		"iges without --max-entities":      {"--format", "iges", "--max-nauo", "1", path},
		"dxf without --max-nesting":        {"--format", "dxf", "--max-expanded-inserts", "1", "--max-expanded-entities", "1", "--max-abs-coordinate", "1", path},
		"dxf without --max-abs-coordinate": {"--format", "dxf", "--max-expanded-inserts", "1", "--max-expanded-entities", "1", "--max-nesting", "1", path},
		"dxf with a nan coordinate bound":  {"--format", "dxf", "--max-expanded-inserts", "1", "--max-expanded-entities", "1", "--max-nesting", "1", "--max-abs-coordinate", "nan", path},
		"dxf with a negative cap":          {"--format", "dxf", "--max-expanded-inserts", "-1", "--max-expanded-entities", "1", "--max-nesting", "1", "--max-abs-coordinate", "1", path},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			exit, out := execCADGuard(t, args...)
			if exit != 64 || strings.Contains(out, "CADVERDICT") {
				t.Errorf("exit=%d stdout=%q, want 64 and no verdict", exit, out)
			}
		})
	}
	t.Run("dxf accepts the step caps and ignores them", func(t *testing.T) {
		r := runCADGuardArgs(t, append([]string{"--max-entities", "1", "--max-nauo", "1"}, dxfGuardArgs(path)...)...)
		if r.verdict != "ok" || r.exit != 0 {
			t.Errorf("verdict=%q exit=%d, want ok/0", r.verdict, r.exit)
		}
	})
}

func dxfRecordTable(records ...[2]string) []dxfPair {
	out := []dxfPair{{"0", "SECTION"}, {"2", "TABLES"}, {"0", "TABLE"}, {"2", "BLOCK_RECORD"}}
	for _, r := range records {
		out = append(out, dxfPair{"0", "BLOCK_RECORD"}, dxfPair{"5", r[0]}, dxfPair{"100", "AcDbSymbolTableRecord"},
			dxfPair{"100", "AcDbBlockTableRecord"}, dxfPair{"2", r[1]})
	}
	return append(out, dxfPair{"0", "ENDTAB"}, dxfPair{"0", "ENDSEC"})
}

func TestCADGuardDXFFollowsAnnotationBlockHandles(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	multileader := func(content string, leaderLines int) []dxfPair {
		out := []dxfPair{{"0", "MULTILEADER"}, {"8", "0"}, {"100", "AcDbMLeader"}, {"300", "CONTEXT_DATA{"}, {"302", "LEADER{"}}
		for i := 0; i < leaderLines; i++ {
			out = append(out, dxfPair{"304", "LEADER_LINE{"}, dxfPair{"10", "0.0"}, dxfPair{"20", "0.0"}, dxfPair{"30", "0.0"}, dxfPair{"305", "}"})
		}
		return append(out, dxfPair{"303", "}"}, dxfPair{"341", content}, dxfPair{"301", "}"}, dxfPair{"344", content})
	}
	leader := []dxfPair{{"0", "LEADER"}, {"8", "0"}, {"3", "STANDARD"}, {"71", "1"}, {"76", "2"},
		{"10", "0.0"}, {"20", "0.0"}, {"30", "0.0"}, {"10", "5.0"}, {"20", "5.0"}, {"30", "0.0"}}
	dimstyleTable := func(pairs ...dxfPair) []dxfPair {
		out := []dxfPair{{"0", "TABLE"}, {"2", "DIMSTYLE"}, {"0", "DIMSTYLE"}, {"105", "30"}, {"2", "STANDARD"}}
		return append(append(out, pairs...), dxfPair{"0", "ENDTAB"})
	}
	withDimstyle := func(records []dxfPair, dim []dxfPair) []dxfPair {
		return append(records[:len(records)-1], append(dim, dxfPair{"0", "ENDSEC"})...)
	}
	grid := dxfInsertPairs("CELL", dxfPair{"70", "1000"}, dxfPair{"71", "1000"}, dxfPair{"44", "1.0"}, dxfPair{"45", "1.0"})
	cases := []struct {
		name, reason string
		data         []byte
	}{
		{"a multileader holding its own block by handle", "cyclic_block_reference", dxfFile("AC1027", "",
			dxfBlockPairs("M", multileader("1F", 1)), dxfInsertPairs("M"), dxfRecordTable([2]string{"1F", "M"}))},
		{"a multileader whose block content is a million cells", "expanded_inserts_over_cap", dxfFile("AC1027", "",
			append(dxfBlockPairs("CELL", dxfLinePairs()), dxfBlockPairs("BIG", grid)...), multileader("2B", 1),
			dxfRecordTable([2]string{"2B", "BIG"}))},
		{"a leader whose dimstyle arrow block holds the leader", "cyclic_block_reference", dxfFile("AC1015", "",
			dxfBlockPairs("ARR", leader), leader,
			withDimstyle(dxfRecordTable([2]string{"2A", "ARR"}), dimstyleTable(dxfPair{"341", "2A"})))},
		{"an R12 dimstyle arrow name", "cyclic_block_reference", dxfFile("AC1009", "",
			dxfBlockPairs("ARR", leader), leader,
			[]dxfPair{{"0", "SECTION"}, {"2", "TABLES"}, {"0", "TABLE"}, {"2", "DIMSTYLE"}, {"0", "DIMSTYLE"}, {"2", "STANDARD"}, {"5", "arr"}, {"0", "ENDTAB"}, {"0", "ENDSEC"}})},
		{"a multileader style block", "cyclic_block_reference", dxfFile("AC1027", "",
			dxfBlockPairs("S", multileader("0", 1)), dxfInsertPairs("S"), dxfRecordTable([2]string{"3C", "S"}),
			[]dxfPair{{"0", "SECTION"}, {"2", "OBJECTS"}, {"0", "MLEADERSTYLE"}, {"5", "40"}, {"343", "3C"}, {"0", "ENDSEC"}})},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runCADGuardDXF(t, writeGuardFile(t, dir, fmt.Sprintf("annotation_%d.dxf", i), c.data))
			if r.verdict != "too_complex" || r.reason != c.reason || r.exit != 4 {
				t.Errorf("verdict=%q reason=%q exit=%d, want too_complex/%s/4 (%v)", r.verdict, r.reason, r.exit, c.reason, r.fields)
			}
			requireGuardCountersWithinCaps(t, r)
		})
	}
	t.Run("a style block adds one level, not two", func(t *testing.T) {
		data := dxfFile("AC1015", "", dxfBlockPairs("ARR", dxfLinePairs()), leader,
			withDimstyle(dxfRecordTable([2]string{"2A", "ARR"}), dimstyleTable(dxfPair{"341", "2A"})))
		r := runCADGuardDXF(t, writeGuardFile(t, dir, "leader_ok.dxf", data))
		if r.verdict != "ok" || r.number("nesting") != 1 || r.number("expanded_inserts") != 1 || r.number("expanded_entities") != 2 {
			t.Errorf("verdict=%q nesting=%d expanded_inserts=%d expanded_entities=%d, want ok/1/1/2", r.verdict,
				r.number("nesting"), r.number("expanded_inserts"), r.number("expanded_entities"))
		}
	})
	t.Run("leader lines multiply the arrow references", func(t *testing.T) {
		data := dxfFile("AC1027", "", dxfBlockPairs("ARR", dxfLinePairs()), multileader("0", 4),
			dxfRecordTable([2]string{"3C", "ARR"}),
			[]dxfPair{{"0", "SECTION"}, {"2", "OBJECTS"}, {"0", "MLEADERSTYLE"}, {"5", "40"}, {"341", "3C"}, {"0", "ENDSEC"}})
		r := runCADGuardDXF(t, writeGuardFile(t, dir, "mleader_lines.dxf", data))
		if r.verdict != "ok" || r.number("expanded_inserts") != 5 {
			t.Errorf("verdict=%q expanded_inserts=%d, want ok/5", r.verdict, r.number("expanded_inserts"))
		}
	})
}

func TestCADGuardDXFLineNumbersSurviveChunkBoundaries(t *testing.T) {
	requireCADPython(t)
	dir := t.TempDir()
	var entities []dxfPair
	for i := 0; i < 200000; i++ {
		entities = append(entities, dxfLinePairs()...)
	}
	bad := []dxfPair{{"0", "LINE"}, {"8", "0"}, {"10", "nan"}}
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		t.Run(fmt.Sprintf("%q", newline), func(t *testing.T) {
			data := bytes.ReplaceAll(dxfFile("AC1009", "", nil, append(append([]dxfPair{}, entities...), bad...)), []byte("\n"), []byte(newline))
			if len(data) < 9<<20 {
				t.Fatalf("file is %d bytes, want more than two read chunks", len(data))
			}
			marker := bytes.Index(data, []byte("nan"))
			want := bytes.Count(data[:marker], []byte(newline)) + 1
			r := runCADGuardDXF(t, writeGuardFile(t, dir, "big.dxf", data))
			if r.reason != "non_numeric_value" || r.number("line") != want || r.number("code") != 10 {
				t.Errorf("reason=%q line=%d code=%d, want non_numeric_value at line %d code 10", r.reason, r.number("line"), r.number("code"), want)
			}
		})
	}
	t.Run("bad group codes after EOF are never read", func(t *testing.T) {
		data := append(dxfFile("AC1009", "", nil, dxfLinePairs()), []byte("x\nnan\n1_0\n")...)
		r := runCADGuardDXF(t, writeGuardFile(t, dir, "after_eof.dxf", data))
		if r.verdict != "ok" || r.exit != 0 {
			t.Errorf("verdict=%q reason=%q exit=%d, want ok/0", r.verdict, r.reason, r.exit)
		}
	})
}
