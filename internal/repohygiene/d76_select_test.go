package repohygiene_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	d76DefaultScript = "../../scripts/cad-dxf-bounds.sh"
	d76CommonScript  = "../../scripts/cad-measure/common.sh"
	d76EvidRel       = ".planning/milestones/v1.16-phases/76-measured-bounds-keda-proof-client-docs/evidence"
	d76SelectHDR     = "HDR,mode=derive76_select,"
	d76SelectSummary = "SUMMARY,mode=derive76_select,"
)

var d76Routes = []string{"convert_step", "convert_iges", "render_step", "render_iges", "render_stl", "dxf_svg", "dxf_pdf", "dxf_png"}

var d76Amendments = []string{
	"RULE_AMEND,key=render_remainder_all,mode=derive76",
	"RULE_AMEND,key=render_route_cell,mode=derive76",
	"RULE_AMEND,key=iges_memory_ceiling,mode=derive76",
	"RULE_AMEND,key=nothing_else,mode=derive76",
}

func d76Script(t *testing.T) string {
	t.Helper()
	path := os.Getenv("OCTOCONV_D76_SCRIPT")
	if path == "" {
		return d76DefaultScript
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("OCTOCONV_D76_SCRIPT must be an absolute path, got %q", path)
	}
	return path
}

func d76Write(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func d76Read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func d76Sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func d76Root(t *testing.T, evid map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	d76Write(t, filepath.Join(root, "scripts", "cad-dxf-bounds.sh"), d76Read(t, d76Script(t)), 0o755)
	d76Write(t, filepath.Join(root, "scripts", "cad-measure", "common.sh"), d76Read(t, d76CommonScript), 0o644)
	for name, data := range evid {
		d76Write(t, filepath.Join(root, d76EvidRel, name), data, 0o644)
	}
	return root
}

const d76DockerStub = `#!/bin/sh
printf 'docker %s\n' "$*" >> "$D76_STUB_LOG"
exit 99
`

const d76GitStub = `#!/bin/sh
if [ "$#" -eq 2 ] && [ "$1" = rev-parse ] && [ "$2" = HEAD ]; then
  printf 'git rev-parse HEAD\n' >> "$D76_STUB_LOG"
  printf '0000000000000000000000000000000000000000\n'
  exit 0
fi
if [ "$1" = status ]; then
  printf 'git status\n' >> "$D76_STUB_LOG"
  exit 0
fi
printf 'git-unexpected %s\n' "$*" >> "$D76_STUB_LOG"
exit 99
`

func d76Stubs(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	d76Write(t, filepath.Join(bin, "docker"), []byte(d76DockerStub), 0o755)
	d76Write(t, filepath.Join(bin, "git"), []byte(d76GitStub), 0o755)
	return bin, filepath.Join(t.TempDir(), "stub-calls.log")
}

func d76Run(t *testing.T, root, out string) (string, string, int, []string) {
	t.Helper()
	bin, log := d76Stubs(t)
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "cad-dxf-bounds.sh"), "derive76", "--select-cells", "--out", out)
	cmd.Dir = root
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "PATH", "TMPDIR", "D76_STUB_LOG":
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "PATH="+bin+":"+os.Getenv("PATH"), "TMPDIR="+t.TempDir(), "D76_STUB_LOG="+log)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	rc := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s: %v", cmd.Path, err)
		}
		rc = exitErr.ExitCode()
	}
	var calls []string
	if data, err := os.ReadFile(log); err == nil {
		calls = d76SplitLines(string(data))
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "docker") || strings.HasPrefix(c, "git-unexpected") {
			t.Errorf("stub call %q, want no docker and only git rev-parse HEAD or status", c)
		}
	}
	if strings.Contains(stdout.String(), "Traceback") || strings.Contains(stderr.String(), "Traceback") {
		t.Errorf("Traceback in the run\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String(), rc, calls
}

func d76SplitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func d76Section(t *testing.T, out string) []string {
	t.Helper()
	lines := d76SplitLines(string(d76Read(t, out)))
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, d76SelectHDR) {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("%s has no %s line", out, d76SelectHDR)
	}
	return lines[start:]
}

func d76Lines(sec []string, prefix string) []string {
	var found []string
	for _, line := range sec {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}
	return found
}

func d76KV(line string) map[string]string {
	out := map[string]string{}
	_, rest, ok := strings.Cut(line, ",")
	if !ok {
		return out
	}
	for _, part := range strings.Split(rest, ",") {
		if key, value, ok := strings.Cut(part, "="); ok {
			out[key] = value
		}
	}
	return out
}

type d76Row struct {
	cell, stage string
	nonSetter   bool
	rep         int
	wall        string
	loaded      bool
	noEnd       bool
	oom         bool
	quality     string
}

func (r d76Row) line() string {
	setter, rep, load1, oom := "1", r.rep, "1.00", "0"
	if r.nonSetter {
		setter = "0"
	}
	if rep == 0 {
		rep = 1
	}
	if r.loaded {
		load1 = "12.00"
	}
	if r.oom {
		oom = "1"
	}
	s := fmt.Sprintf("BOUND76_ROW,cell=%s,stage=%s,setter=%s,rep=%d,wall_s=%s,band=within,rc=0,timeout=0,oom_kill=%s,load1=%s", r.cell, r.stage, setter, rep, r.wall, oom, load1)
	if !r.noEnd {
		s += ",load1_end=1.00"
	}
	if r.quality != "" {
		s += ",quality=" + r.quality
	}
	return s
}

func d76Cell(t *testing.T, dir, name, format string) string {
	t.Helper()
	data := []byte("fixture cell " + name + "\n")
	path := filepath.Join(dir, name+"."+format)
	d76Write(t, path, data, 0o644)
	return fmt.Sprintf("CELL,cell=%s,format=%s,bytes=%d,path=%s,sha256=%s,role=setter", name, format, len(data), path, d76Sha(data))
}

func d76Bounds(cells []string, rows []d76Row) []byte {
	lines := []string{"HDR,group=fixture,load1=1.00"}
	lines = append(lines, cells...)
	for _, r := range rows {
		lines = append(lines, r.line())
	}
	lines = append(lines, "GROUP_END,load1=1.00", "SUMMARY,mode=bounds76,result=ok")
	return []byte(strings.Join(lines, "\n") + "\n")
}

func d76BaseRows() []d76Row {
	return []d76Row{
		{cell: "S", stage: "guard", wall: "1.00"},
		{cell: "S", stage: "mesh", wall: "1.00", quality: "fine"},
		{cell: "P", stage: "guard", wall: "1.00"},
		{cell: "P", stage: "dxf", wall: "10.00"},
		{cell: "P", stage: "rsvg_ink", wall: "10.00"},
	}
}

func d76BaseCells(t *testing.T, dir string, dxf ...string) []string {
	t.Helper()
	cells := []string{d76Cell(t, dir, "S", "step"), d76Cell(t, dir, "P", "dxf")}
	for _, name := range dxf {
		cells = append(cells, d76Cell(t, dir, name, "dxf"))
	}
	return cells
}

func d76Fixture(t *testing.T, bounds, quiet []byte) ([]string, int) {
	t.Helper()
	evid := map[string][]byte{"76-02-bounds.txt": bounds}
	if quiet != nil {
		evid["76-03-quiet.txt"] = quiet
	}
	root := d76Root(t, evid)
	out := filepath.Join(t.TempDir(), "cells.txt")
	d76Write(t, out, []byte(strings.Join(d76Amendments, "\n")+"\n"), 0o644)
	_, _, rc, _ := d76Run(t, root, out)
	return d76Section(t, out), rc
}

func d76WantPick(t *testing.T, sec []string, route, cell, sum string, candidates ...string) {
	t.Helper()
	specs := d76Lines(sec, "CELLSPEC,route="+route+",")
	if len(specs) != 1 {
		t.Fatalf("%d CELLSPEC lines for %s, want 1\nsection:\n%s", len(specs), route, strings.Join(sec, "\n"))
	}
	spec := d76KV(specs[0])
	if spec["cell"] != cell || spec["stage_max_sum_s"] != sum {
		t.Errorf("%s picks cell=%s stage_max_sum_s=%s, want cell=%s stage_max_sum_s=%s\n%s", route, spec["cell"], spec["stage_max_sum_s"], cell, sum, specs[0])
	}
	lists := d76Lines(sec, "CANDIDATES,route="+route+",")
	if len(lists) != 1 {
		t.Fatalf("%d CANDIDATES lines for %s, want 1", len(lists), route)
	}
	have := map[string]bool{}
	for _, c := range strings.Split(d76KV(lists[0])["list"], "+") {
		have[c] = true
	}
	for _, c := range candidates {
		if !have[c] {
			t.Errorf("CANDIDATES for %s lacks %s\n%s", route, c, lists[0])
		}
	}
}

func TestD76SelectRejectsLoadContaminatedRows(t *testing.T) {
	t.Run("contaminated_raise", func(t *testing.T) {
		dir := t.TempDir()
		rows := append(d76BaseRows(),
			d76Row{cell: "Q", stage: "guard", wall: "1.00"},
			d76Row{cell: "Q", stage: "dxf", wall: "5.00"},
			d76Row{cell: "Q", stage: "rsvg_ink", wall: "5.00"},
			d76Row{cell: "Q", stage: "rsvg_ink", rep: 2, wall: "100.00", loaded: true},
		)
		sec, _ := d76Fixture(t, d76Bounds(d76BaseCells(t, dir, "Q"), rows), nil)
		d76WantPick(t, sec, "dxf_svg", "P", "21.00", "Q:11.00")
	})
}

func d76HeadLines(t *testing.T, data []byte, n int) []byte {
	t.Helper()
	end := 0
	for i := 0; i < n; i++ {
		j := bytes.IndexByte(data[end:], '\n')
		if j < 0 {
			t.Fatalf("data has fewer than %d lines", n)
		}
		end += j + 1
	}
	return data[:end]
}

var d76SpecFields = []string{"cell", "file", "sha256", "target", "operation", "opts", "band", "format", "bytes", "stacked", "setter"}

func TestD76SelectCommittedRows(t *testing.T) {
	if os.Getenv("OCTOCONV_D76_COMMITTED") != "1" {
		t.Skip("set OCTOCONV_D76_COMMITTED=1 to re-run select on the v1.16 rows; it needs the CAD cache ~/.cache/octoconv/cad-corpus/scaled/b76")
	}
	sectionDir := os.Getenv("OCTOCONV_D76_SECTION_DIR")
	if sectionDir != "" && !filepath.IsAbs(sectionDir) {
		t.Fatalf("OCTOCONV_D76_SECTION_DIR must be an absolute path, got %q", sectionDir)
	}
	bounds := d76Read(t, filepath.Join(selectionEvidenceDir, "76-02-bounds.txt"))
	ladders := d76Read(t, filepath.Join(selectionEvidenceDir, "76-03-ladders.txt"))
	quiet := d76Read(t, filepath.Join(selectionEvidenceDir, "76-03-quiet.txt"))
	cellsCopy := filepath.Join(selectionEvidenceDir, "76-03-cells.txt")
	cells := d76Read(t, cellsCopy)
	ladders48 := d76HeadLines(t, ladders, 48)

	lines := d76SplitLines(string(cells))
	var hdrs []int
	for i, line := range lines {
		if strings.HasPrefix(line, d76SelectHDR) {
			hdrs = append(hdrs, i)
		}
	}
	if len(hdrs) < 2 {
		t.Fatalf("76-03-cells.txt has %d %s lines, want at least 2", len(hdrs), d76SelectHDR)
	}
	hdr := d76KV(lines[hdrs[1]])
	if got := d76Sha(bounds); got != hdr["source_sha256"] {
		t.Fatalf("76-02-bounds.txt copy has sha256 %s, the published HDR says source_sha256=%s", got, hdr["source_sha256"])
	}
	if got := d76Sha(ladders48); got != hdr["ladders_sha256"] {
		t.Fatalf("the first 48 lines of the 76-03-ladders.txt copy have sha256 %s, the published HDR says ladders_sha256=%s", got, hdr["ladders_sha256"])
	}
	end := -1
	for i := hdrs[1] + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], d76SelectSummary) {
			end = i
			break
		}
	}
	if end < 0 {
		t.Fatalf("the published select section has no %s line", d76SelectSummary)
	}
	published := map[string]map[string]string{}
	for _, line := range d76Lines(lines[hdrs[1]:end+1], "CELLSPEC,") {
		spec := d76KV(line)
		published[spec["route"]] = spec
		if _, err := os.Stat(spec["file"]); err != nil {
			t.Fatalf("v1.16 CAD cache missing: %s", spec["file"])
		}
	}
	if len(published) != len(d76Routes) {
		t.Fatalf("the published section has %d CELLSPEC routes, want %d", len(published), len(d76Routes))
	}

	var im120 map[string]string
	for _, line := range d76SplitLines(string(ladders)) {
		if strings.HasPrefix(line, "CELL,cell=IM120MiB,") {
			im120 = d76KV(line)
		}
	}
	if im120 == nil {
		t.Fatal("the 76-03-ladders.txt copy has no CELL line for IM120MiB")
	}

	cases := []struct {
		name    string
		ladders []byte
		want    func(route string) map[string]string
	}{
		{"select_time", ladders48, func(route string) map[string]string { return published[route] }},
		{"archived", ladders, func(route string) map[string]string {
			if route != "convert_iges" {
				return published[route]
			}
			want := map[string]string{}
			for k, v := range published[route] {
				want[k] = v
			}
			want["cell"], want["file"], want["sha256"], want["bytes"] = "IM120MiB", im120["path"], im120["sha256"], im120["bytes"]
			return want
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := d76Sha(d76Read(t, cellsCopy))
			root := d76Root(t, map[string][]byte{
				"76-02-bounds.txt":  bounds,
				"76-03-ladders.txt": tc.ladders,
				"76-03-quiet.txt":   quiet,
			})
			out := filepath.Join(t.TempDir(), "76-03-cells.txt")
			d76Write(t, out, cells, 0o644)
			_, stderr, rc, _ := d76Run(t, root, out)
			sec := d76Section(t, out)
			for _, line := range sec {
				if strings.HasPrefix(line, "BASIS,") || strings.HasPrefix(line, "CELLSPEC,") || strings.HasPrefix(line, "SUMMARY,") {
					t.Logf("D76SECTION %s", line)
				}
			}
			if sectionDir != "" {
				d76Write(t, filepath.Join(sectionDir, tc.name+".txt"), []byte(strings.Join(sec, "\n")+"\n"), 0o644)
			}
			if rc != 0 {
				t.Errorf("rc %d, want 0\nstderr:\n%s", rc, stderr)
			}
			if s := d76Lines(sec, d76SelectSummary); len(s) != 1 || s[0] != "SUMMARY,mode=derive76_select,routes=8,cellspec=8,stop=0,result=ok" {
				t.Errorf("SUMMARY lines %q, want exactly the ok line with cellspec=8", s)
			}
			for _, route := range d76Routes {
				specs := d76Lines(sec, "CELLSPEC,route="+route+",")
				if len(specs) != 1 {
					t.Errorf("%d CELLSPEC lines for %s, want 1", len(specs), route)
					continue
				}
				got, want := d76KV(specs[0]), tc.want(route)
				for _, f := range d76SpecFields {
					if got[f] != want[f] {
						t.Errorf("%s %s=%q, want %q", route, f, got[f], want[f])
					}
				}
			}
			if after := d76Sha(d76Read(t, cellsCopy)); after != before {
				t.Errorf("the tracked 76-03-cells.txt copy changed: sha256 %s -> %s", before, after)
			}
		})
	}
}
