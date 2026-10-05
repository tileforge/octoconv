package convert

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

//go:embed testdata/cad/synthetic/units_inch.step
var cadPreflightSTEP []byte

const (
	cadPreflightTriangles = 12
	cadPreflightTolerance = 1e-3
)

var cadPreflightExtents = [3]float64{25.4, 50.8, 76.2}

var cadMeshReadOnly = []string{
	"/opt/venv",
	"/usr/bin/python3.11",
	"/usr/lib/python3.11",
}

var cadGuardReadOnly = []string{
	"/usr/bin/python3.11",
	"/usr/lib/python3.11",
}

var cadMeshEngines = []string{"cad-mesh"}

var cadGuardEngines = []string{"cad-guard"}

var cadMultiarchLib = map[string]string{"arm64": "aarch64-linux-gnu", "amd64": "x86_64-linux-gnu"}

func (c CADConverter) cadStage(jobDir string, readOnly, engines []string) (engineExec, error) {
	ro := append([]string{}, readOnly...)
	if c.Sandbox.confines() {
		triple, ok := cadMultiarchLib[runtime.GOARCH]
		if !ok {
			return engineExec{}, fmt.Errorf("cad: no multiarch library directory is known for GOARCH %s", runtime.GOARCH)
		}
		ro = append(ro, "/usr/lib/"+triple)
	}
	env := append(engineEnv(jobDir), "LP_NUM_THREADS=2")
	return c.Sandbox.forJob(jobDir, ro, engines).withEnv(env), nil
}

func (c CADConverter) WithSandbox(helper []string) CADConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}

func ValidateCADTimeouts(guard, mesh, render, engine time.Duration) error {
	for _, v := range []struct {
		name  string
		value time.Duration
	}{{"CAD_GUARD_TIMEOUT", guard}, {"CAD_MESH_TIMEOUT", mesh}, {"CAD_RENDER_TIMEOUT", render}, {"CAD_ENGINE_TIMEOUT", engine}} {
		if v.value <= 0 {
			return fmt.Errorf("%s=%s must be positive", v.name, v.value)
		}
	}
	if guard+mesh+render+CADIOAllowance > engine {
		return fmt.Errorf("CAD_GUARD_TIMEOUT=%s + CAD_MESH_TIMEOUT=%s + CAD_RENDER_TIMEOUT=%s + %s of I/O allowance exceeds CAD_ENGINE_TIMEOUT=%s: "+
			"the attempt deadline would cut a stage short of its own limit", guard, mesh, render, CADIOAllowance, engine)
	}
	return nil
}

func ConfinedCAD(helper []string, concurrency int, concurrencyEnv string, guard, mesh, render, engine time.Duration) (CADConverter, error) {
	if concurrency != 1 {
		return CADConverter{}, fmt.Errorf("%s=%d: the cad pod limit was derived for one job at a time; set %s=1 and scale with replicas",
			concurrencyEnv, concurrency, concurrencyEnv)
	}
	if err := ValidateCADTimeouts(guard, mesh, render, engine); err != nil {
		return CADConverter{}, err
	}
	c := CADConverter{GuardTimeout: guard, MeshTimeout: mesh, RenderTimeout: render}.WithSandbox(helper)
	return c, nil
}

func cadStepEntities(data []byte) (int, error) {
	idx := bytes.Index(data, []byte("DATA;"))
	if idx < 0 {
		return 0, errors.New("no DATA; section")
	}
	return max(bytes.Count(data[idx:], []byte(";"))-3, 0), nil
}

func requireCADDenied(stage string, err error) error {
	if errors.Is(err, ErrEngineSandboxUnavailable) {
		return fmt.Errorf("the %s stage could not establish its sandbox, so its denial of the outside file proves nothing: %w", stage, err)
	}
	return nil
}

func CADPreflight(ctx context.Context, c CADConverter) error {
	if !c.Sandbox.confines() {
		return nil
	}
	triple, ok := cadMultiarchLib[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("cad preflight: no multiarch library directory is known for GOARCH %s", runtime.GOARCH)
	}
	wantEntities, err := cadStepEntities(cadPreflightSTEP)
	if err != nil {
		return fmt.Errorf("cad preflight: embedded fixture: %w", err)
	}

	w, cleanup, err := newPreflightWorkspace("cad")
	if err != nil {
		return err
	}
	defer cleanup()
	if err := w.requireOutsideGrants(cadGuardReadOnly, cadMeshReadOnly, []string{"/usr/lib/" + triple}); err != nil {
		return err
	}
	inside, outside, err := w.place("in.step", cadPreflightSTEP)
	if err != nil {
		return err
	}

	v, err := c.guardStage(ctx, w.jobDir, "step", inside)
	if err != nil {
		return fmt.Errorf("the cad guard stage cannot count a file inside the job directory under its sandbox: %w", err)
	}
	if v.Entities == nil || *v.Entities != wantEntities {
		return fmt.Errorf("the cad guard stage counted %v entities in the fixture, want %d", v.Entities, wantEntities)
	}

	outInside := filepath.Join(w.jobDir, "out.stl")
	meshVerdict, err := c.meshStage(ctx, w.jobDir, "step", inside, outInside)
	if err != nil {
		return fmt.Errorf("the cad mesh stage cannot mesh a file inside the job directory under its sandbox: %w", err)
	}
	n, bounds, err := judgeBinarySTL(outInside)
	if err != nil {
		return fmt.Errorf("the cad mesh stage output inside the job directory is not a usable STL: %w", err)
	}
	if err := requireSTLMatchesVerdict(n, meshVerdict); err != nil {
		return fmt.Errorf("the cad mesh stage output inside the job directory disagrees with its own verdict: %w", err)
	}
	if n != cadPreflightTriangles {
		return fmt.Errorf("the cad mesh stage produced %d triangles for the fixture, want %d", n, cadPreflightTriangles)
	}
	got := bounds.Extents()
	sort.Float64s(got[:])
	for i, want := range cadPreflightExtents {
		if math.Abs(got[i]-want) > cadPreflightTolerance {
			return fmt.Errorf("units: the cad mesh stage produced sorted extents %.4f %.4f %.4f for an inch fixture, want %.1f %.1f %.1f millimetres",
				got[0], got[1], got[2], cadPreflightExtents[0], cadPreflightExtents[1], cadPreflightExtents[2])
		}
	}

	_, guardErr := c.guardStage(ctx, w.jobDir, "step", outside)
	if guardErr == nil {
		return errors.New("the cad guard stage returned ok for a file outside the job directory: confinement is not in effect")
	}
	if err := requireCADDenied("cad guard", guardErr); err != nil {
		return err
	}

	outOutside := filepath.Join(w.jobDir, "neg.stl")
	_, meshErr := c.meshStage(ctx, w.jobDir, "step", outside, outOutside)
	if err := requireCADDenied("cad mesh", meshErr); err != nil {
		return err
	}
	if err := requireDenied(meshErr, outOutside, nil); err != nil {
		return fmt.Errorf("the cad mesh stage read a file outside the job directory: %w", err)
	}
	return nil
}
