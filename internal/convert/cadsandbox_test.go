package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type cadPreflightFakes struct {
	guardEntities int
	guardOutside  string
	meshInsideSTL []byte
	meshTriangles int
	meshOutside   string
}

func newCADPreflightFakes(t *testing.T) cadPreflightFakes {
	t.Helper()
	n, err := cadStepEntities(cadPreflightSTEP)
	if err != nil {
		t.Fatal(err)
	}
	return cadPreflightFakes{
		guardEntities: n,
		guardOutside:  "unreadable",
		meshInsideSTL: cadBoxSTL(25.4, 50.8, 76.2),
		meshTriangles: 12,
		meshOutside:   "deny",
	}
}

func (p cadPreflightFakes) converter(t *testing.T) CADConverter {
	t.Helper()
	dir := t.TempDir()
	stl := filepath.Join(dir, "inside.stl")
	if err := os.WriteFile(stl, p.meshInsideSTL, 0o600); err != nil {
		t.Fatal(err)
	}
	guardOK := fmt.Sprintf(`printf '%%s\n' 'CADVERDICT {"stage":"guard","verdict":"ok","reason":"","entities":%d}'`, p.guardEntities)
	guardOutside := guardOK + "\nexit 0"
	if p.guardOutside == "unreadable" {
		guardOutside = `printf '%s\n' 'CADVERDICT {"stage":"guard","verdict":"unreadable","reason":"cannot_count"}'` + "\nexit 2"
	}
	fakeEngine(t, dir, "cad-guard", fmt.Sprintf("case \"$7\" in\n*cad-preflight-job-*)\n%s\nexit 0;;\n*)\n%s;;\nesac\n", guardOK, guardOutside))

	meshOK := fmt.Sprintf("cp '%s' \"$4\"\n%s", stl, cadSayTriangles("mesh", "ok", "", p.meshTriangles, 0))
	meshOutside := cadSay("mesh", "unreadable", "read_failed", 2)
	if p.meshOutside == "leak" {
		meshOutside = meshOK
	}
	fakeEngine(t, dir, "cad-mesh", fmt.Sprintf("case \"$3\" in\n*cad-preflight-job-*)\n%s;;\n*)\n%s;;\nesac\n", meshOK, meshOutside))
	pathWith(t, dir)

	helper, _ := fakeSandboxHelper(t)
	return CADConverter{}.WithSandbox(helper)
}

func TestCADPreflightUnconfinedCreatesNoWorkspace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if err := CADPreflight(context.Background(), CADConverter{}); err != nil {
		t.Fatalf("CADPreflight on an unconfined converter = %v, want nil", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("an unconfined preflight created %d entries in the temp directory", len(entries))
	}
}

func TestCADPreflightPassesWhenBothStagesBehaveBothWays(t *testing.T) {
	c := newCADPreflightFakes(t).converter(t)
	if err := CADPreflight(context.Background(), c); err != nil {
		t.Fatalf("CADPreflight = %v, want nil", err)
	}
}

func TestCADPreflightNamesTheStageAndDirectionThatFailed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p *cadPreflightFakes)
		want   []string
	}{
		{"the guard says ok for the outside file", func(p *cadPreflightFakes) { p.guardOutside = "ok" }, []string{"guard", "outside"}},
		{"the mesh meshes the outside file", func(p *cadPreflightFakes) { p.meshOutside = "leak" }, []string{"mesh", "outside"}},
		{"unscaled inch output", func(p *cadPreflightFakes) { p.meshInsideSTL = cadBoxSTL(1, 2, 3) }, []string{"units"}},
		{"inch output scaled twice", func(p *cadPreflightFakes) { p.meshInsideSTL = cadBoxSTL(645.16, 1290.32, 1935.48) }, []string{"units"}},
		{"the mesh reports a different triangle count than its file", func(p *cadPreflightFakes) { p.meshTriangles = 11 }, []string{"mesh", "verdict"}},
		{"the guard counts a different number of entities", func(p *cadPreflightFakes) { p.guardEntities++ }, []string{"guard", "entities"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newCADPreflightFakes(t)
			tc.mutate(&p)
			err := CADPreflight(context.Background(), p.converter(t))
			if err == nil {
				t.Fatal("CADPreflight returned nil")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %q", err, w)
				}
			}
		})
	}
}

func TestCADPreflightFailsWhenTheSandboxCannotBeEstablished(t *testing.T) {
	c := CADConverter{}.WithSandbox(failingSandboxHelper(t, 78))
	if err := CADPreflight(context.Background(), c); err == nil {
		t.Fatal("CADPreflight returned nil for a helper that cannot set up its sandbox")
	}
}

func TestCADPreflightFixtureCountMatchesTheRealGuard(t *testing.T) {
	requireCADPython(t)
	want, err := cadStepEntities(cadPreflightSTEP)
	if err != nil {
		t.Fatal(err)
	}
	r := runCADGuard(t, "step", CADMaxSTEPEntities, CADMaxNAUO, filepath.Join("testdata", "cad", "synthetic", "units_inch.step"))
	if r.verdict != "ok" || r.number("entities") != want {
		t.Errorf("verdict=%q entities=%d, want ok/%d", r.verdict, r.number("entities"), want)
	}
}

func TestConfinedCADRefusesEveryConcurrencyButOne(t *testing.T) {
	for _, n := range []int{0, 2, -1} {
		_, err := ConfinedCAD([]string{"helper"}, n, "CAD_WORKER_CONCURRENCY", CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultEngineTimeout)
		if err == nil || !strings.Contains(err.Error(), "CAD_WORKER_CONCURRENCY") {
			t.Errorf("concurrency %d: err = %v, want a refusal naming CAD_WORKER_CONCURRENCY", n, err)
		}
	}
	c, err := ConfinedCAD([]string{"helper"}, 1, "CAD_WORKER_CONCURRENCY", CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultEngineTimeout)
	if err != nil {
		t.Fatalf("concurrency 1: %v", err)
	}
	if !c.Sandbox.confines() || c.GuardTimeout != CADDefaultGuardTimeout || c.MeshTimeout != CADDefaultMeshTimeout {
		t.Errorf("ConfinedCAD = %+v, want a confined converter carrying the given stage timeouts", c)
	}
	open, err := ConfinedCAD(nil, 1, "CAD_WORKER_CONCURRENCY", CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultEngineTimeout)
	if err != nil || open.Sandbox.confines() {
		t.Errorf("ConfinedCAD with no helper = %+v, %v, want an unconfined converter", open, err)
	}
}

func TestValidateCADTimeouts(t *testing.T) {
	s := time.Second
	if err := ValidateCADTimeouts(CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultEngineTimeout); err != nil {
		t.Errorf("the defaults: %v", err)
	}
	cases := []struct {
		name                string
		guard, mesh, engine time.Duration
		want                string
	}{
		{"zero guard", 0, 316 * s, 406 * s, "CAD_GUARD_TIMEOUT"},
		{"negative mesh", 30 * s, -s, 406 * s, "CAD_MESH_TIMEOUT"},
		{"zero engine", 30 * s, 316 * s, 0, "CAD_ENGINE_TIMEOUT"},
		{"stages and allowance exceed the engine", 30 * s, 316 * s, 405 * s, "CAD_ENGINE_TIMEOUT"},
		{"a long mesh alone breaks the sum", 30 * s, 400 * s, 406 * s, "CAD_MESH_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCADTimeouts(tc.guard, tc.mesh, tc.engine)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one naming %s", err, tc.want)
			}
		})
	}
}

func TestCADSetsAreTheMeasuredOnes(t *testing.T) {
	const remeasure = "these are the sets scripts/cad-worker-sandbox-measure.sh measured on the production engine image " +
		"(evidence/73-03-landlock.txt, SANDBOX-PATHS.md section cad); re-measure with that script and update the " +
		"document, the Go vars and this pin together rather than editing the expectation"
	pins := []struct {
		name      string
		got, want []string
	}{
		{"cadMeshReadOnly", cadMeshReadOnly, []string{"/opt/venv", "/usr/bin/python3.11", "/usr/lib/python3.11"}},
		{"cadGuardReadOnly", cadGuardReadOnly, []string{"/usr/bin/python3.11", "/usr/lib/python3.11"}},
		{"cadMeshEngines", cadMeshEngines, []string{"cad-mesh"}},
		{"cadGuardEngines", cadGuardEngines, []string{"cad-guard"}},
	}
	for _, p := range pins {
		if !slices.Equal(p.got, p.want) {
			t.Errorf("%s = %v, want %v: %s", p.name, p.got, p.want, remeasure)
		}
	}
	if len(cadMultiarchLib) != 2 || cadMultiarchLib["arm64"] != "aarch64-linux-gnu" || cadMultiarchLib["amd64"] != "x86_64-linux-gnu" {
		t.Errorf("cadMultiarchLib = %v: %s", cadMultiarchLib, remeasure)
	}
}

func TestCADGuardSetHasNoPathToTheReader(t *testing.T) {
	for _, p := range cadGuardReadOnly {
		if p == "/opt/venv" || strings.HasPrefix(p, "/opt/venv/") {
			t.Errorf("cadGuardReadOnly grants %s: the stage that reads hostile bytes first must not reach OCP; "+
				"re-measure with scripts/cad-worker-sandbox-measure.sh rather than widening the set", p)
		}
	}
}

func TestCADSetsGrantNoSharedWritableOrProcessPath(t *testing.T) {
	for name, set := range map[string][]string{"cadMeshReadOnly": cadMeshReadOnly, "cadGuardReadOnly": cadGuardReadOnly} {
		for _, p := range set {
			for _, banned := range []string{"/tmp", "/proc", "/sys"} {
				if p == banned || strings.HasPrefix(p, banned+"/") {
					t.Errorf("%s grants %s: the measured sets carry none of /tmp, /proc, /sys; "+
						"a removal row of scripts/cad-worker-sandbox-measure.sh has to mark it load-bearing first", name, p)
				}
			}
		}
	}
}

func TestCADStageRefusesAnArchitectureWithNoMultiarchEntry(t *testing.T) {
	saved := cadMultiarchLib
	t.Cleanup(func() { cadMultiarchLib = saved })
	cadMultiarchLib = map[string]string{}
	helper, _ := fakeSandboxHelper(t)
	if _, err := (CADConverter{}).WithSandbox(helper).cadStage(t.TempDir(), cadMeshReadOnly, cadMeshEngines); err == nil {
		t.Error("cadStage returned nil for an architecture with no multiarch directory: a skip would ship an unconfined-by-omission set")
	}
	if _, err := (CADConverter{}).cadStage(t.TempDir(), cadMeshReadOnly, cadMeshEngines); err != nil {
		t.Errorf("an unconfined stage has no use for the table: %v", err)
	}
}
