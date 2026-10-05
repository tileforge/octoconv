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
	cadGuardHelperPath = "../../cmd/cad-worker/helper/cad-guard"
	cadMeshHelperPath  = "../../cmd/cad-worker/helper/cad-mesh"
	guardTestEntityCap = 754762
	guardTestNAUOCap   = 1518
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
	py := requireCADPython(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, "-IB", cadGuardHelperPath,
		"--format", format,
		"--max-entities", fmt.Sprint(entityCap),
		"--max-nauo", fmt.Sprint(nauoCap),
		path)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	res := guardResult{stdout: stdout.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.exit = exitErr.ExitCode()
	default:
		t.Fatalf("run cad-guard: %v", err)
	}
	var last string
	sc := bufio.NewScanner(&stdout)
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
	for _, n := range []string{"754762", "1518"} {
		if bytes.Contains(data, []byte(n)) {
			t.Errorf("cad-guard contains the cap %s: the caps arrive on argv only", n)
		}
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
