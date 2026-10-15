package convert

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	ErrCADDXFTimeout, ErrCADRsvgTimeout, ErrCADDrawingFailed, ErrCADNothingDrawable,
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
	checks := []struct {
		name      string
		got, want int64
		source    string
	}{
		{"CADMaxSTEPEntities", CADMaxSTEPEntities, 754762, decision},
		{"CADMaxNAUO", CADMaxNAUO, 1518, decision},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d: re-derive it from %s rather than editing the expectation", c.name, c.got, c.want, c.source)
		}
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

func cadDerivedValue(t *testing.T, evidence, key string) int64 {
	t.Helper()
	raw, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^DERIVED,key=` + regexp.QuoteMeta(key) + `,value=(\d+),`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no integer DERIVED line for %s in %s", key, evidence)
	}
	n, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

const cad76Evidence = "../../.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-derived.txt"

func TestCADConstantsComeFromThe76Evidence(t *testing.T) {
	seconds := func(d time.Duration) int64 { return int64(d / time.Second) }
	for _, c := range []struct {
		name  string
		got   int64
		whole bool
		key   string
	}{
		{"CADDefaultGuardTimeout", seconds(CADDefaultGuardTimeout), CADDefaultGuardTimeout%time.Second == 0, "cad_guard_timeout_s"},
		{"CADDefaultMeshTimeout", seconds(CADDefaultMeshTimeout), CADDefaultMeshTimeout%time.Second == 0, "cad_mesh_timeout_s"},
		{"CADDefaultRenderTimeout", seconds(CADDefaultRenderTimeout), CADDefaultRenderTimeout%time.Second == 0, "cad_render_timeout_s"},
		{"CADDefaultDXFTimeout", seconds(CADDefaultDXFTimeout), CADDefaultDXFTimeout%time.Second == 0, "cad_dxf_timeout_s"},
		{"CADDefaultRsvgTimeout", seconds(CADDefaultRsvgTimeout), CADDefaultRsvgTimeout%time.Second == 0, "cad_rsvg_timeout_s"},
		{"CADIOAllowance", seconds(CADIOAllowance), CADIOAllowance%time.Second == 0, "cad_io_allowance_s"},
		{"CADDefaultEngineTimeout", seconds(CADDefaultEngineTimeout), CADDefaultEngineTimeout%time.Second == 0, "cad_engine_timeout_s"},
		{"CADMaxSTEPBytes", CADMaxSTEPBytes, true, "cad_step_ceiling_bytes"},
		{"CADMaxIGESBytes", CADMaxIGESBytes, true, "cad_iges_ceiling_bytes"},
		{"CADMaxSTLBytes", CADMaxSTLBytes, true, "cad_stl_ceiling_bytes"},
		{"CADMaxDXFBytes", CADMaxDXFBytes, true, "cad_dxf_ceiling_bytes"},
		{"CADMaxDXFSVGBytes", CADMaxDXFSVGBytes, true, "cad_dxf_svg_max_bytes"},
		{"CADMaxDXFPDFBytes", CADMaxDXFPDFBytes, true, "cad_dxf_pdf_max_bytes"},
	} {
		if want := cadDerivedValue(t, cad76Evidence, c.key); !c.whole || c.got != want {
			t.Errorf("%s = %d (whole=%v), %s DERIVED %s says %d: re-derive it with scripts/cad-dxf-bounds.sh derive76 rather than editing the expectation",
				c.name, c.got, c.whole, cad76Evidence, c.key, want)
		}
	}
}

func cadLongerRoute(guard, mesh, render, dxf, rsvg, io time.Duration) time.Duration {
	return max(guard+mesh+render+io, guard+dxf+2*rsvg+io)
}

func TestCADEngineTimeoutIsTheLongerRoute(t *testing.T) {
	sum3D := CADDefaultGuardTimeout + CADDefaultMeshTimeout + CADDefaultRenderTimeout + CADIOAllowance
	sumDXF := CADDefaultGuardTimeout + CADDefaultDXFTimeout + 2*CADDefaultRsvgTimeout + CADIOAllowance
	if CADDefaultEngineTimeout != max(sum3D, sumDXF) {
		t.Errorf("CADDefaultEngineTimeout = %s, want the longer route: max(3D %s, DXF %s)", CADDefaultEngineTimeout, sum3D, sumDXF)
	}
	if sum3D > CADDefaultEngineTimeout || sumDXF > CADDefaultEngineTimeout {
		t.Errorf("a route sum exceeds the engine timeout %s: 3D %s, DXF %s", CADDefaultEngineTimeout, sum3D, sumDXF)
	}
	if err := ValidateCADTimeouts(CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultRenderTimeout, CADDefaultDXFTimeout, CADDefaultRsvgTimeout, CADDefaultEngineTimeout); err != nil {
		t.Errorf("the defaults fail ValidateCADTimeouts: %v", err)
	}
	s := time.Second
	for _, c := range []struct {
		name                               string
		guard, mesh, render, dxf, rsvg, io time.Duration
		want                               time.Duration
	}{
		{"the 3D route is longer", 21 * s, 324 * s, 521 * s, 186 * s, 217 * s, 20 * s, 886 * s},
		{"the DXF route is longer", 21 * s, 100 * s, 100 * s, 400 * s, 300 * s, 20 * s, 1041 * s},
	} {
		got := cadLongerRoute(c.guard, c.mesh, c.render, c.dxf, c.rsvg, c.io)
		if got != c.want || got < c.guard+c.dxf+2*c.rsvg+c.io || got < c.guard+c.mesh+c.render+c.io {
			t.Errorf("%s: longer route = %s, want %s and at least both sums", c.name, got, c.want)
		}
	}
}

const cad76StackEvidence = "../../.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt"

func TestCADPathBFiredForNoFormat(t *testing.T) {
	raw, err := os.ReadFile(cad76Evidence)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := os.ReadFile(cad76StackEvidence)
	if err != nil {
		t.Fatal(err)
	}
	decisions := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^STACK_DECISION,format=([a-z]+),decision=([a-z_]+),`).FindAllSubmatch(stack, -1) {
		decisions[string(m[1])] = string(m[2])
	}
	if len(decisions) == 0 {
		t.Fatalf("no STACK_DECISION line in %s", cad76StackEvidence)
	}
	all := regexp.MustCompile(`(?m)^PATHB,`).FindAllIndex(raw, -1)
	lines := regexp.MustCompile(`(?m)^PATHB,format=([a-z]+),fired=([01]),reason=([^\n]+)$`).FindAllSubmatch(raw, -1)
	if len(lines) != len(all) {
		t.Fatalf("%s has %d PATHB lines and %d of them parse", cad76Evidence, len(all), len(lines))
	}
	seen := map[string]int{}
	for _, m := range lines {
		format, fired, reason := string(m[1]), string(m[2]), string(m[3])
		seen[format]++
		if fired != "0" {
			t.Errorf("PATHB fired=%s for %s in %s: D-04 then needs an operation-aware render ceiling for %s at create, in inspect and in the worker (76-05-PLAN, fired branch), and the code has none", fired, format, cad76Evidence, format)
		}
		switch {
		case strings.HasPrefix(reason, "not_byte_bound_in_76-01_STACK_DECISION"):
			if decisions[format] != "not_byte_bound" {
				t.Errorf("PATHB %s says not_byte_bound in 76-01, but %s has decision %q", format, cad76StackEvidence, decisions[format])
			}
		case strings.HasPrefix(reason, "no_76-01_byte_bound_decision_for_"+format):
			if d, ok := decisions[format]; ok {
				t.Errorf("PATHB %s says 76-01 has no decision for it, but %s has decision %q", format, cad76StackEvidence, d)
			}
		default:
			t.Errorf("PATHB %s reason %q is not one this test can check against %s", format, reason, cad76StackEvidence)
		}
	}
	for _, format := range []string{"step", "iges", "stl", "dxf"} {
		if seen[format] != 1 {
			t.Errorf("%s has %d PATHB lines for %s, want 1", cad76Evidence, seen[format], format)
		}
	}
	if len(seen) != 4 {
		t.Errorf("%s has PATHB lines for %v, want exactly step, iges, stl and dxf", cad76Evidence, seen)
	}
}

func TestCADDXFConstantsComeFromTheEvidence(t *testing.T) {
	const bounds = "../../.planning/phases/75-dxf-2d-pipeline/evidence/75-05-bounds.txt"
	const guard = "../../.planning/phases/75-dxf-2d-pipeline/evidence/75-03-guard.txt"
	const boundsScript = "scripts/cad-dxf-bounds.sh run"
	const guardScript = "scripts/cad-dxf-guard-diff.sh"
	for _, c := range []struct {
		name     string
		got      int64
		evidence string
		key      string
		script   string
	}{
		{"CADMaxDXFExpandedInserts", CADMaxDXFExpandedInserts, bounds, "cad_dxf_expanded_inserts_cap", boundsScript},
		{"CADMaxDXFExpandedEntities", CADMaxDXFExpandedEntities, bounds, "cad_dxf_expanded_entities_cap", boundsScript},
		{"CADMaxDXFNesting", CADMaxDXFNesting, guard, "cad_dxf_nesting_bound", guardScript},
	} {
		if want := cadDerivedValue(t, c.evidence, c.key); c.got != want {
			t.Errorf("%s = %d, %s DERIVED %s says %d: re-derive it with %s rather than editing the expectation",
				c.name, c.got, c.evidence, c.key, want, c.script)
		}
	}
}

func TestCADDXFMaxBytesIsTheDXFCeiling(t *testing.T) {
	for format, want := range map[string]int64{
		"dxf": CADMaxDXFBytes, "step": CADMaxSTEPBytes, "iges": CADMaxIGESBytes, "stl": CADMaxSTLBytes,
	} {
		if got := cadMaxBytes(format); got != want {
			t.Errorf("cadMaxBytes(%q) = %d, want %d", format, got, want)
		}
	}
}

func TestCADDXFTimeoutsDefaultAndOverride(t *testing.T) {
	c := CADConverter{}
	if c.dxfTimeout() != CADDefaultDXFTimeout || c.rsvgTimeout() != CADDefaultRsvgTimeout {
		t.Errorf("zero converter timeouts = %s, %s, want the defaults", c.dxfTimeout(), c.rsvgTimeout())
	}
	c = CADConverter{DXFTimeout: 7 * time.Second, RsvgTimeout: 9 * time.Second}
	if c.dxfTimeout() != 7*time.Second || c.rsvgTimeout() != 9*time.Second {
		t.Errorf("configured timeouts = %s, %s, want 7s and 9s", c.dxfTimeout(), c.rsvgTimeout())
	}
}

func TestCADDXFGuardArgv(t *testing.T) {
	record := "printf '%s\\n' \"$*\" > \"$(dirname \"$0\")/guard.args\"\n" + cadOKGuard()
	t.Run("dxf", func(t *testing.T) {
		f := newCADFixture(t, "dxf", record, "exit 1\n")
		if _, err := (CADConverter{}).guardStage(context.Background(), f.jobDir, "dxf", f.in); err != nil {
			t.Fatalf("guardStage: %v", err)
		}
		got, _ := os.ReadFile(f.marker("guard.args"))
		want := fmt.Sprintf("--format dxf --max-expanded-inserts %d --max-expanded-entities %d --max-nesting %d --max-abs-coordinate 1e+12 %s",
			CADMaxDXFExpandedInserts, CADMaxDXFExpandedEntities, CADMaxDXFNesting, f.in)
		if strings.TrimSpace(string(got)) != want {
			t.Errorf("guard argv = %q, want %q", got, want)
		}
	})
	t.Run("the real guard accepts the production argv", func(t *testing.T) {
		r := runCADGuardArgs(t, cadGuardArgs("dxf", filepath.Join("testdata", "cad", "dxf", "all_hidden.dxf"))...)
		if r.exit != 0 || r.verdict != "ok" {
			t.Fatalf("cad-guard exit %d verdict %q reason %q: %s", r.exit, r.verdict, r.reason, r.stdout)
		}
	})
	t.Run("step is unchanged", func(t *testing.T) {
		f := newCADFixture(t, "step", record, "exit 1\n")
		if _, err := (CADConverter{}).guardStage(context.Background(), f.jobDir, "step", f.in); err != nil {
			t.Fatalf("guardStage: %v", err)
		}
		got, _ := os.ReadFile(f.marker("guard.args"))
		want := fmt.Sprintf("--format step --max-entities %d --max-nauo %d %s", CADMaxSTEPEntities, CADMaxNAUO, f.in)
		if strings.TrimSpace(string(got)) != want {
			t.Errorf("guard argv = %q, want %q", got, want)
		}
	})
}

func TestCADDXFVerdictCarriesTheDrawingFacts(t *testing.T) {
	line := `CADVERDICT {"auditor_fixes":2,"codepage":"ANSI_1251","entities_drawn":41,"hatch_truncated":1,"insunits":4,"page_h_mm":120.5,"page_w_mm":297.0,"reason":"","render_substituted":3,"skipped":{"3DSOLID":2,"IMAGE":1},"stage":"dxf","texts":7,"uncovered":3,"verdict":"ok"}`
	v, ok := parseCADVerdict(cadStageDXF, []byte(line+"\n"))
	if !ok {
		t.Fatal("the dxf verdict did not parse")
	}
	if v.Texts != 7 || v.Uncovered != 3 || v.EntitiesDrawn != 41 || v.PageWMM != 297 || v.PageHMM != 120.5 || v.AuditorFixes != 2 ||
		v.RenderSubstituted != 3 || v.HatchTruncated != 1 {
		t.Errorf("verdict counts = %+v", v)
	}
	if v.InsUnits == nil || *v.InsUnits != 4 || v.Codepage == nil || *v.Codepage != "ANSI_1251" {
		t.Errorf("insunits %v codepage %v, want 4 and ANSI_1251", v.InsUnits, v.Codepage)
	}
	if len(v.Skipped) != 2 || v.Skipped["3DSOLID"] != 2 || v.Skipped["IMAGE"] != 1 {
		t.Errorf("skipped = %v", v.Skipped)
	}
	v, ok = parseCADVerdict(cadStageDXF, []byte(`CADVERDICT {"stage":"dxf","verdict":"ok","insunits":null,"codepage":null,"skipped":{}}`+"\n"))
	if !ok || v.InsUnits != nil || v.Codepage != nil {
		t.Errorf("null insunits and codepage = %+v, %v", v, ok)
	}
}

func cadShellErr(t *testing.T, script string) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", script).Run()
	if err == nil {
		t.Fatalf("%q exited 0, want a failure", script)
	}
	return err
}

func cadExpiredStage(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	t.Cleanup(cancel)
	<-ctx.Done()
	return ctx
}

func TestCADDXFStageClassification(t *testing.T) {
	alive := context.Background()
	exit := func(code int) error { return cadShellErr(t, "exit "+strconv.Itoa(code)) }
	cases := []struct {
		name     string
		out      string
		stageCtx context.Context
		err      error
		want     []error
		wantNot  []error
		contains string
	}{
		{"empty", cadLine("dxf", "empty", "nothing_drawable"), alive, exit(3), []error{ErrCADEmptyResult, ErrCADNothingDrawable}, nil, "nothing_drawable"},
		{"too_complex", cadLine("dxf", "too_complex", "drawn_entities_over_cap"), alive, exit(4), []error{ErrCADTooComplex}, []error{ErrCADDrawingFailed}, ""},
		{"unreadable", cadLine("dxf", "unreadable", "structure"), alive, exit(2), []error{ErrCADUnreadable}, []error{ErrCADDrawingFailed}, ""},
		{"failed", cadLine("dxf", "failed", "draw_exception"), alive, exit(1), []error{ErrCADDrawingFailed}, []error{ErrCADUnreadable, ErrCADTooComplex}, "draw_exception"},
		{"stage deadline", "", cadExpiredStage(t), fmt.Errorf("cad-dxf killed: %w", context.DeadlineExceeded), []error{ErrCADDXFTimeout}, []error{ErrCADEngineKilled, context.DeadlineExceeded}, "cad dxf"},
		{"SIGKILL", "", alive, cadShellErr(t, "kill -9 $$"), []error{ErrCADEngineKilled}, []error{ErrCADDrawingFailed}, ""},
		{"SIGSEGV", "", alive, cadShellErr(t, "kill -s SEGV $$"), []error{ErrCADDrawingFailed}, []error{ErrCADUnreadable}, ""},
		{"non-zero exit without a verdict", "Traceback\n", alive, exit(1), []error{ErrCADDrawingFailed}, nil, ""},
		{"exit 0 without a verdict", "", alive, nil, []error{ErrCADDrawingFailed}, nil, ""},
		{"ok with a non-zero exit", cadLine("dxf", "ok", ""), alive, exit(1), []error{ErrCADDrawingFailed}, nil, ""},
		{"a verdict of another stage", cadLine("guard", "ok", ""), alive, nil, []error{ErrCADDrawingFailed}, nil, ""},
		{"sandbox unavailable", cadLine("dxf", "ok", ""), alive, fmt.Errorf("wrap: %w", ErrEngineSandboxUnavailable), []error{ErrEngineSandboxUnavailable}, cadSentinels, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := classifyCADStage(cadStageDXF, alive, c.stageCtx, CADDefaultDXFTimeout, []byte(c.out), engineCPU{}, c.err)
			if err == nil {
				t.Fatal("classifyCADStage returned nil")
			}
			for _, w := range c.want {
				if !errors.Is(err, w) {
					t.Errorf("err = %v, want it to wrap %v", err, w)
				}
			}
			for _, w := range c.wantNot {
				if errors.Is(err, w) {
					t.Errorf("err = %v, must not wrap %v", err, w)
				}
			}
			if !strings.HasPrefix(err.Error(), "cad dxf: ") || !strings.Contains(err.Error(), c.contains) {
				t.Errorf("err %q does not have the cad dxf shape or does not contain %q", err, c.contains)
			}
		})
	}
	t.Run("ok is accepted", func(t *testing.T) {
		v, err := classifyCADStage(cadStageDXF, alive, alive, CADDefaultDXFTimeout, []byte(cadLine("dxf", "ok", "")), engineCPU{}, nil)
		if err != nil || v.Verdict != "ok" {
			t.Fatalf("ok verdict = %+v, %v", v, err)
		}
	})
	t.Run("the attempt deadline stays a context error", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := classifyCADStage(cadStageDXF, parent, parent, CADDefaultDXFTimeout, nil, engineCPU{}, fmt.Errorf("cad-dxf killed: %w", parent.Err()))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if errors.Is(err, ErrCADDXFTimeout) || errors.Is(err, ErrCADDrawingFailed) {
			t.Errorf("err = %v, a cancelled attempt must not read as a stage fault", err)
		}
	})
}

func TestCADDXFStageLeavesTheOtherStagesClassesAlone(t *testing.T) {
	alive := context.Background()
	for _, c := range []struct {
		stage string
		out   string
		err   error
		want  error
	}{
		{cadStageMesh, cadLine("mesh", "empty", "zero_size"), nil, ErrCADEmptyResult},
		{cadStageMesh, "", cadShellErr(t, "kill -s SEGV $$"), ErrCADUnreadable},
		{cadStageRender, cadLine("render", "failed", "x"), nil, ErrCADRenderFailed},
		{cadStageGuard, cadLine("guard", "too_complex", "x"), nil, ErrCADTooComplex},
	} {
		_, err := classifyCADStage(c.stage, alive, alive, time.Minute, []byte(c.out), engineCPU{}, c.err)
		if !errors.Is(err, c.want) || errors.Is(err, ErrCADDrawingFailed) || errors.Is(err, ErrCADNothingDrawable) {
			t.Errorf("%s %q: err = %v, want %v and no DXF sentinel", c.stage, c.out, err, c.want)
		}
	}
	_, err := classifyCADStage(cadStageMesh, alive, cadExpiredStage(t), time.Minute, nil, engineCPU{}, cadShellErr(t, "exit 1"))
	if !errors.Is(err, ErrCADMeshTimeout) || errors.Is(err, ErrCADDXFTimeout) {
		t.Errorf("mesh deadline: err = %v, want ErrCADMeshTimeout only", err)
	}
}

func TestCADRsvgClassification(t *testing.T) {
	alive := context.Background()
	dir := t.TempDir()
	full := filepath.Join(dir, "full.png")
	if err := os.WriteFile(full, []byte("\x89PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.png")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.png")

	if err := classifyCADRsvg(alive, alive, CADDefaultRsvgTimeout, engineCPU{}, nil, full); err != nil {
		t.Fatalf("a clean exit with output = %v, want nil", err)
	}
	cases := []struct {
		name     string
		stageCtx context.Context
		err      error
		out      string
		want     []error
		wantNot  []error
		contains string
	}{
		{"stage deadline", cadExpiredStage(t), fmt.Errorf("rsvg-convert killed: %w", context.DeadlineExceeded), full, []error{ErrCADRsvgTimeout}, []error{ErrCADEngineKilled, ErrCADDrawingFailed, context.DeadlineExceeded}, "cad rsvg"},
		{"SIGKILL", alive, cadShellErr(t, "kill -9 $$"), full, []error{ErrCADEngineKilled}, []error{ErrCADDrawingFailed}, ""},
		{"SIGSEGV", alive, cadShellErr(t, "kill -s SEGV $$"), full, []error{ErrCADDrawingFailed}, nil, "segmentation"},
		{"non-zero exit", alive, cadShellErr(t, "exit 1"), full, []error{ErrCADDrawingFailed}, nil, ""},
		{"clean exit with an empty output", alive, nil, empty, []error{ErrCADDrawingFailed}, nil, "empty"},
		{"clean exit with no output", alive, nil, missing, []error{ErrCADDrawingFailed}, nil, ""},
		{"sandbox unavailable", alive, fmt.Errorf("wrap: %w", ErrEngineSandboxUnavailable), full, []error{ErrEngineSandboxUnavailable}, cadSentinels, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := classifyCADRsvg(alive, c.stageCtx, CADDefaultRsvgTimeout, engineCPU{}, c.err, c.out)
			if err == nil {
				t.Fatal("classifyCADRsvg returned nil")
			}
			for _, w := range c.want {
				if !errors.Is(err, w) {
					t.Errorf("err = %v, want it to wrap %v", err, w)
				}
			}
			for _, w := range c.wantNot {
				if errors.Is(err, w) {
					t.Errorf("err = %v, must not wrap %v", err, w)
				}
			}
			if !strings.HasPrefix(err.Error(), "cad rsvg: ") || !strings.Contains(err.Error(), c.contains) {
				t.Errorf("err %q does not have the cad rsvg shape or does not contain %q", err, c.contains)
			}
		})
	}
	t.Run("a cancelled parent is cancellation", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		cancel()
		err := classifyCADRsvg(parent, parent, CADDefaultRsvgTimeout, engineCPU{}, fmt.Errorf("rsvg-convert killed: %w", parent.Err()), full)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if errors.Is(err, ErrCADRsvgTimeout) || errors.Is(err, ErrCADDrawingFailed) || errors.Is(err, ErrCADEngineKilled) {
			t.Errorf("err = %v, a cancelled attempt must not read as a stage fault", err)
		}
	})
}
