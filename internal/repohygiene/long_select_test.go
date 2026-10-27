package repohygiene_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const longSelectDefaultScript = "../../scripts/keda-cad-downscale-survival.sh"

func longSelectScript(t *testing.T) string {
	t.Helper()
	path := os.Getenv("OCTOCONV_LONG_SELECT_SCRIPT")
	if path == "" {
		return longSelectDefaultScript
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("OCTOCONV_LONG_SELECT_SCRIPT must be an absolute path, got %q", path)
	}
	return path
}

func longSelectOnlyLine(t *testing.T, lines []string, pattern string) string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	var found []string
	for _, line := range lines {
		if re.MatchString(line) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("anchor %s matches %d lines, want exactly 1", pattern, len(found))
	}
	return found[0]
}

func longSelectProgram(t *testing.T, path, extra string) string {
	t.Helper()
	if extra != "" && !regexp.MustCompile(`^TRANSPORT_CAP=[0-9]+$`).MatchString(extra) {
		t.Fatalf("extra must be TRANSPORT_CAP=<digits>, got %q", extra)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(source), "\n")
	transportCap := longSelectOnlyLine(t, lines, `^TRANSPORT_CAP=[0-9]+$`)
	bandLow := longSelectOnlyLine(t, lines, `^BAND_LOW=[0-9]+$`)
	derivedValue := longSelectOnlyLine(t, lines, `^derived_value\(\) \{ .* \}$`)
	derivedEngine := longSelectOnlyLine(t, lines, `^DERIVED_ENGINE=\$\(derived_value cad_engine_timeout_s\)$`)
	bandHigh := longSelectOnlyLine(t, lines, `^BAND_HIGH=\$\(\(DERIVED_ENGINE \* 2 / 3\)\)$`)
	var ceilings []string
	for _, c := range []struct{ name, format string }{{"STEP", "step"}, {"IGES", "iges"}, {"STL", "stl"}, {"DXF", "dxf"}} {
		ceilings = append(ceilings, longSelectOnlyLine(t, lines, `^CEIL_`+c.name+`=\$\(derived_value cad_`+c.format+`_ceiling_bytes\)$`))
	}
	start, end := -1, -1
	starts, ends := 0, 0
	for i, line := range lines {
		if line == `step "long_select"` {
			start = i
			starts++
		}
		if strings.HasPrefix(line, `IFS='|' read -r L_ROUTE `) {
			end = i
			ends++
		}
	}
	if starts != 1 {
		t.Fatalf(`anchor step "long_select" matches %d lines, want exactly 1`, starts)
	}
	if ends != 1 {
		t.Fatalf(`anchor IFS='|' read -r L_ROUTE matches %d lines, want exactly 1`, ends)
	}
	if end <= start {
		t.Fatalf("region end (line %d) is not after its start (line %d)", end+1, start+1)
	}
	region := strings.Join(lines[start:end], "\n")
	if n := strings.Count(region, `FILENAME == ARGV[1] && /^INWORKER_ROW,/`); n != 1 {
		t.Fatalf("the INWORKER_ROW clause occurs %d times in the long_select region, want exactly 1", n)
	}
	parts := []string{
		"set -euo pipefail",
		`ev() { printf '%s\n' "$*"; }`,
		`fail() { printf 'FAIL: %s\n' "$*"; exit 1; }`,
		"step() { :; }",
		"pass() { :; }",
		transportCap,
		bandLow,
	}
	if extra != "" {
		parts = append(parts, extra)
	}
	parts = append(parts, derivedValue, derivedEngine, bandHigh)
	parts = append(parts, ceilings...)
	parts = append(parts, region, `printf 'PICK=%s\n' "$PICK"`)
	return strings.Join(parts, "\n") + "\n"
}

func longSelectRun(t *testing.T, program, cells, inworker, derived string) (string, error) {
	t.Helper()
	var abs [3]string
	for i, p := range []string{cells, inworker, derived} {
		a, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		abs[i] = a
	}
	cmd := exec.Command("bash", "-c", program)
	cmd.Env = append(os.Environ(), "W="+t.TempDir(), "CELLS="+abs[0], "INWORKER="+abs[1], "DERIVED="+abs[2])
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w; stdout: %s; stderr: %s", err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

func longSelectLine(t *testing.T, out, prefix string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d lines start with %q, want exactly 1; output:\n%s", len(found), prefix, out)
	}
	return found[0]
}

func longSelectPick(t *testing.T, out string) []string {
	t.Helper()
	fields := strings.Split(strings.TrimPrefix(longSelectLine(t, out, "PICK="), "PICK="), "|")
	if len(fields) != 13 {
		t.Fatalf("PICK has %d fields, want 13: %q", len(fields), fields)
	}
	return fields
}

type longSelectRow struct {
	cell  string
	wall  string
	clean string
}

func longSelectFixture(t *testing.T, cells []string, rows []longSelectRow) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	var c strings.Builder
	c.WriteString("HDR,mode=derive76_select,attempt=1\n")
	for _, cell := range cells {
		sum := sha256.Sum256([]byte(cell))
		fmt.Fprintf(&c, "CELLSPEC,route=render_step,cell=%s,file=/fixture/scaled/b76/%s.step,sha256=%s,target=png,operation=render,opts=size:2048,format=step,bytes=1000\n", cell, cell, hex.EncodeToString(sum[:]))
	}
	fmt.Fprintf(&c, "SUMMARY,mode=derive76_select,routes=8,cellspec=%d,stop=0,result=ok\n", len(cells))
	var w strings.Builder
	reps := map[string]int{}
	for _, r := range rows {
		reps[r.cell]++
		fmt.Fprintf(&w, "INWORKER_ROW,route=render_step,cell=%s,rep=%d,status=done,attempts=1,attempt_wall_s=%s", r.cell, reps[r.cell], r.wall)
		if r.clean != "" {
			fmt.Fprintf(&w, ",load_clean=%s", r.clean)
		}
		w.WriteString("\n")
	}
	derived := "DERIVED,key=cad_engine_timeout_s,value=886,derivation=fixture\n" +
		"DERIVED,key=cad_step_ceiling_bytes,value=66060288,derivation=fixture\n" +
		"DERIVED,key=cad_iges_ceiling_bytes,value=125829120,derivation=fixture\n" +
		"DERIVED,key=cad_stl_ceiling_bytes,value=190840832,derivation=fixture\n" +
		"DERIVED,key=cad_dxf_ceiling_bytes,value=123731968,derivation=fixture\n"
	paths := [3]string{filepath.Join(dir, "cells.txt"), filepath.Join(dir, "inworker.txt"), filepath.Join(dir, "derived.txt")}
	for i, body := range []string{c.String(), w.String(), derived} {
		if err := os.WriteFile(paths[i], []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return paths[0], paths[1], paths[2]
}

func TestLongSelectRejectsLoadContaminatedRows(t *testing.T) {
	y := []longSelectRow{{"Y", "355.00", "1"}, {"Y", "355.00", "1"}, {"Y", "355.00", "1"}}
	cases := []struct {
		name       string
		cells      []string
		rows       []longSelectRow
		candidates string
		pickCell   string
		pickWall   string
	}{
		{
			name:  "contaminated_raise",
			cells: []string{"X", "Y"},
			rows: append([]longSelectRow{
				{"X", "170.00", "1"}, {"X", "172.00", "1"}, {"X", "175.00", "1"}, {"X", "200.00", "0"},
			}, y...),
			candidates: "LONG_CANDIDATES,list=render_step:X:175.00:below_band+render_step:Y:355.00:eligible",
			pickCell:   "Y",
			pickWall:   "355.00",
		},
		{
			name:  "row_without_flag",
			cells: []string{"Y", "Z"},
			rows: append(append([]longSelectRow{}, y...),
				longSelectRow{"Z", "170.00", "1"}, longSelectRow{"Z", "250.00", ""}),
			candidates: "LONG_CANDIDATES,list=render_step:Y:355.00:eligible+render_step:Z:170.00:below_band",
			pickCell:   "Y",
			pickWall:   "355.00",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			program := longSelectProgram(t, longSelectScript(t), "")
			cells, inworker, derived := longSelectFixture(t, tc.cells, tc.rows)
			out, err := longSelectRun(t, program, cells, inworker, derived)
			if err != nil {
				t.Fatalf("long_select region failed: %v", err)
			}
			if got := longSelectLine(t, out, "LONG_CANDIDATES,"); got != tc.candidates {
				t.Fatalf("LONG_CANDIDATES:\n got %s\nwant %s", got, tc.candidates)
			}
			pick := longSelectPick(t, out)
			if pick[1] != tc.pickCell || pick[11] != tc.pickWall || pick[12] != "eligible" {
				t.Fatalf("PICK cell=%s wall=%s status=%s, want cell=%s wall=%s status=eligible", pick[1], pick[11], pick[12], tc.pickCell, tc.pickWall)
			}
		})
	}
}

func longSelectField(t *testing.T, line, key string) string {
	t.Helper()
	var found []string
	for _, part := range strings.Split(line, ",") {
		if v, ok := strings.CutPrefix(part, key+"="); ok {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d fields %s= in %q, want exactly 1", len(found), key, line)
	}
	return found[0]
}

func TestLongSelectReproducesThePublishedV116Pick(t *testing.T) {
	cells := filepath.Join(selectionEvidenceDir, "76-03-cells.txt")
	inworker := filepath.Join(selectionEvidenceDir, "76-03-inworker.txt")
	derived := "../convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-03-derived.txt"
	published, err := os.ReadFile(filepath.Join(selectionEvidenceDir, "76-10-keda-downscale.txt"))
	if err != nil {
		t.Fatal(err)
	}
	pub := string(published)
	wantCandidates := longSelectLine(t, pub, "LONG_CANDIDATES,")
	selected := longSelectLine(t, pub, "LONG_SELECT,")
	route := longSelectField(t, selected, "route")
	cell := longSelectField(t, selected, "cell")
	sum := longSelectField(t, selected, "sha256")
	wall := longSelectField(t, selected, "expected_wall_s")
	publishedCap := longSelectField(t, longSelectLine(t, pub, "RULE,key=long_select,"), "transport_cap_bytes")
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(publishedCap) {
		t.Fatalf("published transport_cap_bytes %q is not a whole number", publishedCap)
	}
	for _, tc := range []struct{ name, extra string }{
		{"script_cap", ""},
		{"published_cap", "TRANSPORT_CAP=" + publishedCap},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program := longSelectProgram(t, longSelectScript(t), tc.extra)
			out, err := longSelectRun(t, program, cells, inworker, derived)
			if err != nil {
				t.Fatalf("long_select region failed: %v", err)
			}
			got := longSelectLine(t, out, "LONG_CANDIDATES,")
			t.Logf("LONGSELECT %s", got)
			if got != wantCandidates {
				t.Fatalf("LONG_CANDIDATES:\n got %s\nwant %s", got, wantCandidates)
			}
			pick := longSelectPick(t, out)
			t.Logf("LONGSELECT PICK=%s", strings.Join(pick, "|"))
			if pick[0] != route || pick[1] != cell || pick[3] != sum || pick[11] != wall || pick[12] != "eligible" {
				t.Fatalf("PICK route=%s cell=%s sha256=%s wall=%s status=%s, want route=%s cell=%s sha256=%s wall=%s status=eligible", pick[0], pick[1], pick[3], pick[11], pick[12], route, cell, sum, wall)
			}
		})
	}
}
