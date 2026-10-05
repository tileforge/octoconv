package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cadMeshFakeOCP = "testdata/cadmesh-fake/run.py"

type fakeMesh struct {
	Faces    []*int `json:"faces"`
	Flags    int    `json:"flags"`
	NotDone  bool   `json:"-"`
	STL      int    `json:"stl"`
	STLExtra int    `json:"stl_extra"`
}

type meshRun struct {
	exit    int
	verdict string
	reason  string
	fields  map[string]any
	stlSize int64
}

func faceCounts(counts ...int) []*int {
	out := make([]*int, len(counts))
	for i := range counts {
		if counts[i] >= 0 {
			n := counts[i]
			out[i] = &n
		}
	}
	return out
}

func runFakeMesh(t *testing.T, m fakeMesh) meshRun {
	t.Helper()
	py := requireCADPython(t)
	cfg := map[string]any{"faces": m.Faces, "flags": m.Flags, "done": !m.NotDone, "stl": m.STL, "stl_extra": m.STLExtra}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.step")
	out := filepath.Join(dir, "out.stl")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, "-B", cadMeshFakeOCP, cadMeshHelperPath, "--format", "step", in, out)
	cmd.Env = append(os.Environ(), "FAKE_MESH="+string(raw))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	res := meshRun{stlSize: -1}
	var exitErr *exec.ExitError
	switch err := cmd.Run(); {
	case err == nil:
	case errors.As(err, &exitErr):
		res.exit = exitErr.ExitCode()
	default:
		t.Fatalf("run cad-mesh: %v", err)
	}
	var last string
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, cadVerdictPrefix) {
			last = strings.TrimPrefix(line, cadVerdictPrefix)
		}
	}
	if last == "" {
		t.Fatalf("cad-mesh printed no verdict: %q", stdout.String())
	}
	if err := json.Unmarshal([]byte(last), &res.fields); err != nil {
		t.Fatalf("verdict %q: %v", last, err)
	}
	res.verdict, _ = res.fields["verdict"].(string)
	res.reason, _ = res.fields["reason"].(string)
	if info, err := os.Stat(out); err == nil {
		res.stlSize = info.Size()
	}
	return res
}

func TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL(t *testing.T) {
	cases := []struct {
		name        string
		mesh        fakeMesh
		wantExit    int
		wantVerdict string
		wantReason  string
		wantTri     int
	}{
		{"every face meshed", fakeMesh{Faces: faceCounts(2, 2, 2, 2, 2, 2), STL: 12}, 0, "ok", "", 12},
		{"one face without a triangulation", fakeMesh{Faces: faceCounts(2, 2, -1, 2, 2, 2), STL: 10}, 2, "incomplete", "face_not_triangulated", 0},
		{"one face with an empty triangulation", fakeMesh{Faces: faceCounts(2, 2, 0, 2, 2, 2), STL: 10}, 2, "incomplete", "face_not_triangulated", 0},
		{"the mesher reports the failure flag with every face triangulated", fakeMesh{Faces: faceCounts(2, 2, 2), Flags: 4, STL: 6}, 2, "incomplete", "mesh_status", 0},
		{"the mesher reports the failure flag and one face is missing", fakeMesh{Faces: faceCounts(2, -1, 2), Flags: 4, STL: 4}, 2, "incomplete", "mesh_status", 0},
		{"the mesher is not done", fakeMesh{Faces: faceCounts(2, 2, 2), NotDone: true, STL: 6}, 2, "incomplete", "mesh_status", 0},
		{"open wire", fakeMesh{Faces: faceCounts(2, 2, 2), Flags: 1, STL: 6}, 2, "incomplete", "mesh_status", 0},
		{"self intersecting wire", fakeMesh{Faces: faceCounts(2, 2, 2), Flags: 2, STL: 6}, 2, "incomplete", "mesh_status", 0},
		{"unoriented wire", fakeMesh{Faces: faceCounts(2, 2, 2), Flags: 16, STL: 6}, 2, "incomplete", "mesh_status", 0},
		{"too few points", fakeMesh{Faces: faceCounts(2, 2, 2), Flags: 32, STL: 6}, 2, "incomplete", "mesh_status", 0},
		{"user break", fakeMesh{Faces: faceCounts(2, 2, 2), Flags: 256, STL: 6}, 2, "incomplete", "mesh_status", 0},
		{"remesh, outdated and reused are bookkeeping, not failures", fakeMesh{Faces: faceCounts(2, 2, 2), Flags: 8 | 64 | 128, STL: 6}, 0, "ok", "", 6},
		{"no face has a triangulation", fakeMesh{Faces: faceCounts(-1, -1, -1), Flags: 4}, 3, "empty", "no_triangles", 0},
		{"the shape has no face", fakeMesh{Faces: faceCounts()}, 3, "empty", "no_triangles", 0},
		{"the writer drops triangles the mesher made", fakeMesh{Faces: faceCounts(2, 2, 2), STL: 5}, 2, "incomplete", "stl_count_mismatch", 0},
		{"the writer adds triangles the mesher did not make", fakeMesh{Faces: faceCounts(2, 2, 2), STL: 7}, 2, "incomplete", "stl_count_mismatch", 0},
		{"the file is longer than 84 plus 50 per triangle", fakeMesh{Faces: faceCounts(2, 2, 2), STL: 6, STLExtra: 7}, 2, "incomplete", "stl_count_mismatch", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runFakeMesh(t, c.mesh)
			if r.exit != c.wantExit || r.verdict != c.wantVerdict || r.reason != c.wantReason {
				t.Fatalf("exit=%d verdict=%q reason=%q, want exit=%d verdict=%q reason=%q", r.exit, r.verdict, r.reason, c.wantExit, c.wantVerdict, c.wantReason)
			}
			if got, _ := r.fields["triangles"].(float64); int(got) != c.wantTri {
				t.Errorf("reported triangles = %v, want %d", r.fields["triangles"], c.wantTri)
			}
			refusedBeforeWriting := c.wantReason == "mesh_status" || c.wantReason == "face_not_triangulated" || c.wantReason == "no_triangles"
			if refusedBeforeWriting && r.stlSize != -1 {
				t.Errorf("an STL of %d bytes was written before the refusal", r.stlSize)
			}
			if c.wantVerdict == "ok" {
				if want := int64(84 + 50*c.wantTri); r.stlSize != want {
					t.Errorf("STL is %d bytes, want %s", r.stlSize, fmt.Sprint(want))
				}
			}
		})
	}
}

func TestCADIncompleteVerdictIsAnUnreadableFile(t *testing.T) {
	f := newCADFixture(t, "step", cadOKGuard(), cadSay("mesh", "incomplete", "mesh_status", 2))
	err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
	if !errors.Is(err, ErrCADUnreadable) || errors.Is(err, ErrCADEmptyResult) {
		t.Fatalf("err = %v, want ErrCADUnreadable alone", err)
	}
}
