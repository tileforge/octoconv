package convert

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var cadSentinels = []error{
	ErrCADInputTooLarge, ErrCADTooComplex, ErrCADUnreadable,
	ErrCADEmptyResult, ErrCADMeshTimeout, ErrCADEngineKilled,
}

func requireNoCADSentinel(t *testing.T, err error) {
	t.Helper()
	for _, s := range cadSentinels {
		if errors.Is(err, s) {
			t.Errorf("error %q matches %q, want no CAD sentinel", err, s)
		}
	}
}

func cadBoxSTL(dx, dy, dz float32) []byte {
	corners := [8][3]float32{
		{0, 0, 0}, {dx, 0, 0}, {dx, dy, 0}, {0, dy, 0},
		{0, 0, dz}, {dx, 0, dz}, {dx, dy, dz}, {0, dy, dz},
	}
	faces := [12][3]int{
		{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4},
		{1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7},
	}
	return cadSTL(faces[:], func(i int) [3]float32 { return corners[i] })
}

func cadSTL(faces [][3]int, vertex func(int) [3]float32) []byte {
	out := make([]byte, stlHeaderBytes+stlTriangleBytes*len(faces))
	binary.LittleEndian.PutUint32(out[80:], uint32(len(faces)))
	for i, f := range faces {
		tri := out[stlHeaderBytes+i*stlTriangleBytes:]
		for v, idx := range f {
			for axis, x := range vertex(idx) {
				binary.LittleEndian.PutUint32(tri[12+v*12+axis*4:], math.Float32bits(x))
			}
		}
	}
	return out
}

func cadStlWithVertex(x float32) []byte {
	data := cadBoxSTL(25.4, 50.8, 76.2)
	binary.LittleEndian.PutUint32(data[stlHeaderBytes+12:], math.Float32bits(x))
	return data
}

func cadLine(stage, verdict, reason string) string {
	return fmt.Sprintf(`CADVERDICT {"stage":"%s","verdict":"%s","reason":"%s","triangles":12,"entities":353}`, stage, verdict, reason)
}

func cadSayTriangles(stage, verdict, reason string, triangles, exit int) string {
	line := fmt.Sprintf(`CADVERDICT {"stage":"%s","verdict":"%s","reason":"%s","triangles":%d,"entities":353}`, stage, verdict, reason, triangles)
	return fmt.Sprintf("printf '%%s\\n' '%s'\nexit %d\n", line, exit)
}

func cadSay(stage, verdict, reason string, exit int) string {
	return fmt.Sprintf("printf '%%s\\n' '%s'\nexit %d\n", cadLine(stage, verdict, reason), exit)
}

type cadFixture struct {
	dir    string
	jobDir string
	in     string
	out    string
}

func newCADFixture(t *testing.T, ext, guard, mesh string) cadFixture {
	t.Helper()
	dir := t.TempDir()
	jobDir := t.TempDir()
	fakeEngine(t, dir, "cad-guard", guard)
	fakeEngine(t, dir, "cad-mesh", mesh)
	pathWith(t, dir)
	in := filepath.Join(jobDir, "in."+ext)
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cadFixture{dir: dir, jobDir: jobDir, in: in, out: filepath.Join(jobDir, "out.stl")}
}

func (f cadFixture) writeSTL(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(f.dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f cadFixture) marker(name string) string { return filepath.Join(f.dir, name) }

func (f cadFixture) exists(name string) bool {
	_, err := os.Stat(f.marker(name))
	return err == nil
}

func (f cadFixture) goodMesh(t *testing.T) string {
	stl := f.writeSTL(t, "box.stl", cadBoxSTL(25.4, 50.8, 76.2))
	return fmt.Sprintf("cp '%s' \"$4\"\n%s", stl, cadSay("mesh", "ok", "", 0))
}

func cadOKGuard() string { return cadSay("guard", "ok", "", 0) }

func TestCADConverterIsRegisteredAndComparable(t *testing.T) {
	c := CADConverter{}
	if got := c.Pairs(); len(got) != 2 || got[0] != (Pair{"step", "stl"}) || got[1] != (Pair{"iges", "stl"}) {
		t.Errorf("Pairs() = %v, want exactly step->stl and iges->stl", got)
	}
	if c.Engine() != "cad" || c.Engine() != EngineCAD {
		t.Errorf("Engine() = %q, want cad", c.Engine())
	}
	if c != (CADConverter{}) {
		t.Error("CADConverter values must compare equal with ==")
	}
	if len(AllEngines()) != 10 || !slices.Contains(AllEngines(), EngineCAD) {
		t.Errorf("AllEngines() = %v, want ten classes including cad", AllEngines())
	}
	if !Default.Supports("step", "stl") || !Default.Supports("iges", "stl") {
		t.Error("Default does not register the cad pairs")
	}
}

func TestCADPinnedConstantsComeFromTheEvidence(t *testing.T) {
	const decision = ".planning/phases/71-evaluation-gate/71-DECISION.md"
	const spikes = ".planning/phases/73-cad-class-convert-slice-confined/evidence/73-01-spikes.txt"
	checks := []struct {
		name      string
		got, want int64
		source    string
	}{
		{"CADMaxSTEPBytes", CADMaxSTEPBytes, 66060288, decision + " and " + spikes + " cad_step_ceiling_bytes"},
		{"CADMaxIGESBytes", CADMaxIGESBytes, 141557760, spikes + " cad_iges_ceiling_bytes (supersedes " + decision + ")"},
		{"CADMaxSTEPEntities", CADMaxSTEPEntities, 754762, decision},
		{"CADMaxNAUO", CADMaxNAUO, 1518, decision},
		{"CADDefaultGuardTimeout", int64(CADDefaultGuardTimeout / time.Second), 30, spikes + " cad_guard_timeout_s"},
		{"CADDefaultMeshTimeout", int64(CADDefaultMeshTimeout / time.Second), 316, spikes + " cad_mesh_timeout_s"},
		{"CADDefaultRenderTimeout", int64(CADDefaultRenderTimeout / time.Second), 480, ".planning/phases/74-3d-render-measurements/74-RESEARCH.md Render stage timeout (chosen under the 900 s window, not measured)"},
		{"CADIOAllowance", int64(CADIOAllowance / time.Second), 60, spikes + " cad_io_allowance_s"},
		{"CADDefaultEngineTimeout", int64(CADDefaultEngineTimeout / time.Second), 886, "30 + 316 + 480 + 60"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d: re-derive it from %s rather than editing the expectation", c.name, c.got, c.want, c.source)
		}
	}
	if CADDefaultEngineTimeout != CADDefaultGuardTimeout+CADDefaultMeshTimeout+CADDefaultRenderTimeout+CADIOAllowance {
		t.Errorf("CADDefaultEngineTimeout must equal guard + mesh + render + io allowance (%s)", spikes)
	}
}

func TestCADOptsFromMapEmptyAndUnion(t *testing.T) {
	for _, m := range []map[string]any{nil, {}} {
		if o, err := CADOptsFromMap(m); err != nil || !o.isZero() {
			t.Errorf("CADOptsFromMap(%v) = %+v, %v, want zero and nil", m, o, err)
		}
	}
	if _, err := CADOptsFromMap(map[string]any{"units": "in"}); err == nil {
		t.Error("CADOptsFromMap with an unknown option returned nil, want an error")
	}
}

func TestCADConvertAcceptsQualityOnlyOptions(t *testing.T) {
	c := CADConverter{}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.step")
	out := filepath.Join(dir, "out.stl")
	err := c.Convert(context.Background(), in, out, map[string]any{"view": "top"})
	if err == nil || !strings.Contains(err.Error(), "only quality applies") {
		t.Errorf("Convert with a render key = %v, want the only-quality refusal", err)
	}
	err = c.Convert(context.Background(), in, out, map[string]any{"quality": "ultra"})
	if err == nil || !strings.Contains(err.Error(), "unsupported quality") {
		t.Errorf("Convert with a bad quality = %v, want the unsupported-quality refusal", err)
	}
}

func TestCADConvertPassesQualityToTheMesher(t *testing.T) {
	for _, tc := range []struct {
		opts map[string]any
		want string
	}{
		{nil, "standard"},
		{map[string]any{"quality": "draft"}, "draft"},
		{map[string]any{"quality": "fine"}, "fine"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			f := newCADFixture(t, "step", cadOKGuard(), "")
			std := f.writeSTL(t, "std.stl", cadBoxSTL(25.4, 50.8, 76.2))
			fine := f.writeSTL(t, "fine.stl", cadSTL(slices.Repeat([][3]int{{0, 2, 1}}, 48), func(i int) [3]float32 { return [3]float32{float32(i), float32(i * i), 1} }))
			mesh := fmt.Sprintf("echo \"$@\" > \"$(dirname \"$0\")/mesh.args\"\ncase \"$6\" in\nfine) cp '%s' \"$4\"\n%s;;\n*) cp '%s' \"$4\"\n%s;;\nesac\n",
				fine, cadSayTriangles("mesh", "ok", "", 48, 0), std, cadSayTriangles("mesh", "ok", "", 12, 0))
			fakeEngine(t, f.dir, "cad-mesh", mesh)
			if err := (CADConverter{}).Convert(context.Background(), f.in, f.out, tc.opts); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			raw, err := os.ReadFile(f.marker("mesh.args"))
			if err != nil {
				t.Fatal(err)
			}
			if got := string(raw); !strings.Contains(got, "--quality "+tc.want) {
				t.Errorf("mesh argv = %q, want --quality %s", got, tc.want)
			}
			n, _, err := judgeBinarySTL(f.out)
			if err != nil {
				t.Fatal(err)
			}
			if (tc.want == "fine") != (n == 48) || (tc.want != "fine" && n != 12) {
				t.Errorf("triangles = %d for quality %s, want fine to yield at least as many as standard", n, tc.want)
			}
		})
	}
}

func TestCADConvertRefusesWhatItDoesNotConvert(t *testing.T) {
	f := newCADFixture(t, "step", "touch \"$(dirname \"$0\")/guard.ran\"\n"+cadOKGuard(), "touch \"$(dirname \"$0\")/mesh.ran\"\n"+cadSay("mesh", "ok", "", 0))
	cases := []struct {
		name string
		in   string
		out  string
		opts map[string]any
	}{
		{"an option", f.in, f.out, map[string]any{"x": 1}},
		{"a dxf input", filepath.Join(f.jobDir, "in.dxf"), f.out, nil},
		{"a stl target other than stl", f.in, filepath.Join(f.jobDir, "out.png"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := (CADConverter{}).Convert(context.Background(), c.in, c.out, c.opts); err == nil {
				t.Fatal("Convert returned nil, want an error")
			}
			if f.exists("guard.ran") || f.exists("mesh.ran") {
				t.Error("an engine ran for a conversion Convert refuses")
			}
		})
	}
}

func TestCADConvertReChecksTheByteCeilingBeforeAnyEngine(t *testing.T) {
	guardRan := "touch \"$(dirname \"$0\")/guard.ran\"\n" + cadSay("guard", "too_complex", "entities_over_cap", 4)
	for _, tc := range []struct {
		ext     string
		ceiling int64
	}{{"step", CADMaxSTEPBytes}, {"iges", CADMaxIGESBytes}} {
		t.Run(tc.ext+" one byte over", func(t *testing.T) {
			f := newCADFixture(t, tc.ext, guardRan, "exit 1\n")
			if err := os.Truncate(f.in, tc.ceiling+1); err != nil {
				t.Fatal(err)
			}
			err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
			if !errors.Is(err, ErrCADInputTooLarge) {
				t.Fatalf("err = %v, want ErrCADInputTooLarge", err)
			}
			if !strings.Contains(err.Error(), strconv.FormatInt(tc.ceiling, 10)) {
				t.Errorf("err %q does not name the ceiling %d", err, tc.ceiling)
			}
			if f.exists("guard.ran") {
				t.Error("the guard ran for an oversized input")
			}
		})
		t.Run(tc.ext+" exactly at the ceiling reaches the guard", func(t *testing.T) {
			f := newCADFixture(t, tc.ext, guardRan, "exit 1\n")
			if err := os.Truncate(f.in, tc.ceiling); err != nil {
				t.Fatal(err)
			}
			err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
			if errors.Is(err, ErrCADInputTooLarge) {
				t.Fatalf("err = %v, a file exactly at the ceiling is accepted", err)
			}
			if !f.exists("guard.ran") {
				t.Error("the guard did not run for an input exactly at the ceiling")
			}
		})
	}
}

func TestCADConvertPassesTheCapsAndPathsToTheStages(t *testing.T) {
	f := newCADFixture(t, "iges",
		"printf '%s\\n' \"$*\" > \"$(dirname \"$0\")/guard.args\"\n"+cadOKGuard(),
		"printf '%s\\n' \"$*\" > \"$(dirname \"$0\")/mesh.args\"\ncp \"$(dirname \"$0\")/box.stl\" \"$4\"\n"+cadSay("mesh", "ok", "", 0))
	f.writeSTL(t, "box.stl", cadBoxSTL(25.4, 50.8, 76.2))
	if err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	guardArgs, _ := os.ReadFile(f.marker("guard.args"))
	wantGuard := fmt.Sprintf("--format iges --max-entities %d --max-nauo %d %s", CADMaxSTEPEntities, CADMaxNAUO, f.in)
	if strings.TrimSpace(string(guardArgs)) != wantGuard {
		t.Errorf("guard argv = %q, want %q", guardArgs, wantGuard)
	}
	meshArgs, _ := os.ReadFile(f.marker("mesh.args"))
	wantMesh := fmt.Sprintf("--format iges %s %s --quality standard", f.in, f.out)
	if strings.TrimSpace(string(meshArgs)) != wantMesh {
		t.Errorf("mesh argv = %q, want %q", meshArgs, wantMesh)
	}
}

func TestCADGuardVerdictsStopBeforeTheMesh(t *testing.T) {
	cases := []struct {
		name    string
		script  string
		want    error
		wantNot []error
	}{
		{"too_complex", cadSay("guard", "too_complex", "entities_over_cap", 4), ErrCADTooComplex, nil},
		{"unreadable", cadSay("guard", "unreadable", "no_data_section", 2), ErrCADUnreadable, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCADFixture(t, "step", c.script, "touch \"$(dirname \"$0\")/mesh.ran\"\n"+cadSay("mesh", "ok", "", 0))
			err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if f.exists("mesh.ran") {
				t.Error("the mesh ran after a non-ok guard verdict")
			}
		})
	}
}

func TestCADMeshVerdictsAndTheSTLJudgment(t *testing.T) {
	cases := []struct {
		name string
		want error
		mesh func(f cadFixture, t *testing.T) string
	}{
		{"unreadable", ErrCADUnreadable, func(cadFixture, *testing.T) string { return cadSay("mesh", "unreadable", "read_failed", 2) }},
		{"empty", ErrCADEmptyResult, func(cadFixture, *testing.T) string { return cadSay("mesh", "empty", "zero_size", 3) }},
		{"incomplete mesh status", ErrCADUnreadable, func(cadFixture, *testing.T) string { return cadSay("mesh", "incomplete", "mesh_status", 2) }},
		{"incomplete untriangulated face", ErrCADUnreadable, func(f cadFixture, t *testing.T) string {
			stl := f.writeSTL(t, "partial.stl", cadBoxSTL(1, 2, 3))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", stl, cadSay("mesh", "incomplete", "face_not_triangulated", 2))
		}},
		{"too_complex", ErrCADTooComplex, func(cadFixture, *testing.T) string { return cadSay("mesh", "too_complex", "out_of_memory", 4) }},
		{"ok with a good STL", nil, func(f cadFixture, t *testing.T) string { return f.goodMesh(t) }},
		{"ok with a trailing byte", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "bad.stl", append(cadBoxSTL(1, 2, 3), 0))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSay("mesh", "ok", "", 0))
		}},
		{"ok with a truncated STL", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "bad.stl", cadBoxSTL(1, 2, 3)[:200])
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSay("mesh", "ok", "", 0))
		}},
		{"ok with zero triangles", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "bad.stl", make([]byte, stlHeaderBytes))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSay("mesh", "ok", "", 0))
		}},
		{"ok with a NaN vertex", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "bad.stl", cadStlWithVertex(float32(math.NaN())))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSay("mesh", "ok", "", 0))
		}},
		{"ok with an infinite vertex", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "bad.stl", cadStlWithVertex(float32(math.Inf(-1))))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSay("mesh", "ok", "", 0))
		}},
		{"ok with fewer file triangles than the helper reported", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "box.stl", cadBoxSTL(1, 2, 3))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSayTriangles("mesh", "ok", "", 13, 0))
		}},
		{"ok with more file triangles than the helper reported", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "box.stl", cadBoxSTL(1, 2, 3))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSayTriangles("mesh", "ok", "", 11, 0))
		}},
		{"ok with a well-formed short STL and a longer helper count", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			short := cadBoxSTL(1, 2, 3)[:stlHeaderBytes+stlTriangleBytes*11]
			binary.LittleEndian.PutUint32(short[80:], 11)
			p := f.writeSTL(t, "box.stl", short)
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSayTriangles("mesh", "ok", "", 12, 0))
		}},
		{"ok with no helper triangle count", ErrCADEmptyResult, func(f cadFixture, t *testing.T) string {
			p := f.writeSTL(t, "box.stl", cadBoxSTL(1, 2, 3))
			return fmt.Sprintf("cp '%s' \"$4\"\n%s", p, cadSayTriangles("mesh", "ok", "", 0, 0))
		}},
		{"ok with no STL at all", ErrCADEmptyResult, func(cadFixture, *testing.T) string { return cadSay("mesh", "ok", "", 0) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCADFixture(t, "step", cadOKGuard(), "exit 1\n")
			fakeEngine(t, f.dir, "cad-mesh", c.mesh(f, t))
			err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
			if c.want == nil {
				if err != nil {
					t.Fatalf("Convert: %v", err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestCADVerdictParsingTrustsOnlyTheLastLineOfTheRunningStage(t *testing.T) {
	t.Run("a forged ok earlier loses to the real last line", func(t *testing.T) {
		f := newCADFixture(t, "step", cadOKGuard(), "")
		script := fmt.Sprintf("printf '%%s\\n' '%s'\n%s", cadLine("mesh", "ok", ""), cadSay("mesh", "unreadable", "read_failed", 2))
		fakeEngine(t, f.dir, "cad-mesh", script)
		err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrCADUnreadable) {
			t.Fatalf("err = %v, want ErrCADUnreadable", err)
		}
	})
	t.Run("a verdict for the other stage is no verdict", func(t *testing.T) {
		f := newCADFixture(t, "step", cadOKGuard(), cadSay("guard", "ok", "", 0))
		err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if err == nil {
			t.Fatal("Convert returned nil on a mesh stage that never produced a mesh verdict")
		}
		requireNoCADSentinel(t, err)
	})
	t.Run("an ok verdict with a non-zero exit is not trusted", func(t *testing.T) {
		f := newCADFixture(t, "step", cadOKGuard(), "")
		fakeEngine(t, f.dir, "cad-mesh", cadSay("mesh", "ok", "", 1))
		err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if err == nil {
			t.Fatal("Convert returned nil")
		}
		requireNoCADSentinel(t, err)
	})
	t.Run("parseCADVerdict takes the last prefixed line", func(t *testing.T) {
		out := []byte(cadLine("mesh", "ok", "") + "\nnoise\r\n" + cadLine("mesh", "empty", "zero_size") + "\r\n")
		v, ok := parseCADVerdict("mesh", out)
		if !ok || v.Verdict != "empty" {
			t.Errorf("parseCADVerdict = %+v, %v, want the empty verdict", v, ok)
		}
		if _, ok := parseCADVerdict("guard", out); ok {
			t.Error("a mesh verdict was accepted for the guard stage")
		}
		if _, ok := parseCADVerdict("mesh", []byte("CADVERDICT {not json\n")); ok {
			t.Error("a malformed verdict was accepted")
		}
	})
}

func TestCADSandboxFaultIsKeyedOnExit78Alone(t *testing.T) {
	f := newCADFixture(t, "step", cadOKGuard(), "exit 1\n")
	t.Run("78 is the sandbox sentinel and no CAD sentinel", func(t *testing.T) {
		c := CADConverter{Sandbox: newEngineSandbox(failingSandboxHelper(t, 78))}
		err := c.Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrEngineSandboxUnavailable) {
			t.Fatalf("err = %v, want ErrEngineSandboxUnavailable", err)
		}
		requireNoCADSentinel(t, err)
	})
	t.Run("any other exit is not the sandbox sentinel", func(t *testing.T) {
		c := CADConverter{Sandbox: newEngineSandbox(failingSandboxHelper(t, 1))}
		err := c.Convert(context.Background(), f.in, f.out, nil)
		if err == nil || errors.Is(err, ErrEngineSandboxUnavailable) {
			t.Fatalf("err = %v, want a plain failure", err)
		}
		requireNoCADSentinel(t, err)
	})
}

func TestCADSignalsAndSilenceAreClassifiedByWhatKilledTheHelper(t *testing.T) {
	cases := []struct {
		name string
		mesh string
		want error
	}{
		{"SIGKILL with both contexts alive", "kill -9 $$\n", ErrCADEngineKilled},
		{"SIGSEGV", "kill -s SEGV $$\n", ErrCADUnreadable},
		{"exit 1 with no verdict", "echo boom >&2\nexit 1\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCADFixture(t, "step", cadOKGuard(), c.mesh)
			err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
			if err == nil {
				t.Fatal("Convert returned nil")
			}
			if c.want == nil {
				requireNoCADSentinel(t, err)
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestCADStageDeadlineIsNotTheAttemptDeadline(t *testing.T) {
	t.Run("the mesh stage's own deadline is ErrCADMeshTimeout", func(t *testing.T) {
		f := newCADFixture(t, "step", cadOKGuard(), "sleep 30\n")
		c := CADConverter{MeshTimeout: 300 * time.Millisecond}
		start := time.Now()
		err := c.Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrCADMeshTimeout) {
			t.Fatalf("err = %v, want ErrCADMeshTimeout", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Error("a mesh stage timeout must not read as the attempt deadline")
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("took %s", time.Since(start))
		}
	})
	t.Run("the attempt deadline stays a plain context error", func(t *testing.T) {
		f := newCADFixture(t, "step", cadOKGuard(), "sleep 30\n")
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		c := CADConverter{MeshTimeout: time.Minute}
		err := c.Convert(ctx, f.in, f.out, nil)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
		if errors.Is(err, ErrCADMeshTimeout) {
			t.Error("the attempt deadline was classified as the mesh stage's timeout")
		}
		requireNoCADSentinel(t, err)
	})
	t.Run("a guard deadline is no CAD sentinel and the mesh never runs", func(t *testing.T) {
		f := newCADFixture(t, "step", "sleep 30\n", "touch \"$(dirname \"$0\")/mesh.ran\"\n"+cadSay("mesh", "ok", "", 0))
		c := CADConverter{GuardTimeout: 300 * time.Millisecond}
		err := c.Convert(context.Background(), f.in, f.out, nil)
		if err == nil {
			t.Fatal("Convert returned nil")
		}
		requireNoCADSentinel(t, err)
		if !strings.Contains(err.Error(), "guard") {
			t.Errorf("err %q does not name the guard stage", err)
		}
		if f.exists("mesh.ran") {
			t.Error("the mesh ran after the guard deadline")
		}
	})
}

func TestCADCancelMidMeshLeavesNoProcessOfTheGroupAlive(t *testing.T) {
	mesh := "sleep 300 &\necho $! > \"$(dirname \"$3\")/grandchild.pid\"\nwait\n"
	f := newCADFixture(t, "step", cadOKGuard(), mesh)
	pidFile := filepath.Join(f.jobDir, "grandchild.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (CADConverter{}).Convert(ctx, f.in, f.out, nil) }()

	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			if p, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				pid = p
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the fake mesh never forked its grandchild")
	}
	time.Sleep(200 * time.Millisecond)
	cancelled := time.Now()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if time.Since(cancelled) > 2*time.Second {
			t.Errorf("Convert took %s to return after cancel", time.Since(cancelled))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Convert did not return within 2s of the cancel")
	}

	for end := time.Now().Add(2 * time.Second); ; {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(end) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d of the helper's process group is still alive 2s after cancel", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCADStagesRunThroughTheHelperUnderTheirOwnRulesets(t *testing.T) {
	envDump := "env > \"$(dirname \"$0\")/$(basename \"$0\").env\"\n"
	f := newCADFixture(t, "step", envDump+cadOKGuard(), "")
	stl := f.writeSTL(t, "box.stl", cadBoxSTL(25.4, 50.8, 76.2))
	fakeEngine(t, f.dir, "cad-mesh", envDump+fmt.Sprintf("cp '%s' \"$4\"\n", stl)+cadSay("mesh", "ok", "", 0))
	helper, calls := fakeSandboxHelper(t)

	c := CADConverter{Sandbox: newEngineSandbox(helper)}
	if err := c.Convert(context.Background(), f.in, f.out, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	invocations := calls()
	if len(invocations) != 2 {
		t.Fatalf("the helper ran %d times, want 2 (guard, mesh): %v", len(invocations), invocations)
	}
	triple := cadMultiarchLib[runtime.GOARCH]
	for i, stage := range []struct {
		engine   string
		readOnly []string
	}{{"cad-guard", cadGuardReadOnly}, {"cad-mesh", cadMeshReadOnly}} {
		argv := invocations[i]
		sep := indexOf(argv, "--")
		if sep < 0 {
			t.Fatalf("%s: no -- separator in %v", stage.engine, argv)
		}
		var rw, ro []string
		for _, tok := range argv[1:sep] {
			switch {
			case strings.HasPrefix(tok, "rw:"):
				rw = append(rw, strings.TrimPrefix(tok, "rw:"))
			case strings.HasPrefix(tok, "ro:"):
				ro = append(ro, strings.TrimPrefix(tok, "ro:"))
			}
		}
		if len(rw) != 1 || rw[0] != f.jobDir {
			t.Errorf("%s: rw tokens = %v, want exactly [%s]", stage.engine, rw, f.jobDir)
		}
		want := append(slices.Clone(stage.readOnly), "/usr/lib/"+triple, filepath.Join(f.dir, stage.engine))
		for _, w := range want {
			if indexOf(ro, w) < 0 {
				t.Errorf("%s: ro tokens %v missing %q", stage.engine, ro, w)
			}
		}
		if argv[sep+1] != filepath.Join(f.dir, stage.engine) {
			t.Errorf("%s: command after -- is %q", stage.engine, argv[sep+1])
		}
		env, err := os.ReadFile(f.marker(stage.engine + ".env"))
		if err != nil {
			t.Fatalf("%s: engine environment not recorded: %v", stage.engine, err)
		}
		lines := strings.Split(string(env), "\n")
		for _, w := range []string{"LP_NUM_THREADS=2", "TMPDIR=" + f.jobDir} {
			if !slices.Contains(lines, w) {
				t.Errorf("%s: engine environment lacks %s", stage.engine, w)
			}
		}
	}
}

func TestCADJudgeBinarySTL(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("a box reports its triangle count and extents", func(t *testing.T) {
		n, b, err := judgeBinarySTL(write("box.stl", cadBoxSTL(25.4, 50.8, 76.2)))
		if err != nil || n != 12 {
			t.Fatalf("n=%d err=%v, want 12 and no error", n, err)
		}
		got := b.Extents()
		sort.Float64s(got[:])
		for i, want := range []float64{25.4, 50.8, 76.2} {
			if math.Abs(got[i]-want) > 1e-3 {
				t.Errorf("sorted extents = %v, want 25.4 50.8 76.2", got)
			}
		}
	})
	t.Run("more triangles than one read block", func(t *testing.T) {
		faces := make([][3]int, stlBlockTriangle*2+7)
		p := write("big.stl", cadSTL(faces, func(i int) [3]float32 { return [3]float32{1, 2, 3} }))
		n, _, err := judgeBinarySTL(p)
		if err != nil || n != len(faces) {
			t.Fatalf("n=%d err=%v, want %d", n, err, len(faces))
		}
	})
	bad := map[string][]byte{
		"empty file":       {},
		"short header":     make([]byte, 50),
		"zero triangles":   make([]byte, stlHeaderBytes),
		"truncated":        cadBoxSTL(1, 2, 3)[:300],
		"trailing byte":    append(cadBoxSTL(1, 2, 3), 7),
		"NaN vertex":       cadStlWithVertex(float32(math.NaN())),
		"positive Inf":     cadStlWithVertex(float32(math.Inf(1))),
		"negative Inf":     cadStlWithVertex(float32(math.Inf(-1))),
		"declared too big": func() []byte { d := cadBoxSTL(1, 2, 3); binary.LittleEndian.PutUint32(d[80:], 13); return d }(),
	}
	for name, data := range bad {
		t.Run(name, func(t *testing.T) {
			if _, _, err := judgeBinarySTL(write(strings.ReplaceAll(name, " ", "_"), data)); !errors.Is(err, ErrCADEmptyResult) {
				t.Errorf("err = %v, want ErrCADEmptyResult", err)
			}
		})
	}
	t.Run("a missing file", func(t *testing.T) {
		if _, _, err := judgeBinarySTL(filepath.Join(dir, "missing.stl")); !errors.Is(err, ErrCADEmptyResult) {
			t.Errorf("err = %v, want ErrCADEmptyResult", err)
		}
	})
}
