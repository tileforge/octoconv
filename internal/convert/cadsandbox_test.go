package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apaderin/octoconv/internal/measurements"
)

type cadPreflightFakes struct {
	guardEntities int
	guardOutside  string
	meshInsideSTL []byte
	meshTriangles int
	meshOutside   string
	draft         cadMeasureDraft
	renderFrame   func(t *testing.T, size int, transparent bool) []byte
	renderOutside string
}

func inchBoxDraft() cadMeasureDraft {
	return cadMeasureDraft{
		Format:        "step",
		DeclaredUnit:  "inch",
		AppliedFactor: floatp(25.4),
		BBox:          measurements.BBox{Min: []float64{0, 0, 0}, Max: []float64{25.4, 50.8, 76.2}},
		Solids:        intp(1),
		ClosedSolids:  intp(1),
		Volume:        floatp(6 * 25.4 * 25.4 * 25.4),
		Area:          floatp(22 * 25.4 * 25.4),
		Triangles:     12,
	}
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
		draft:         inchBoxDraft(),
		renderFrame:   renderPNGBytes,
		renderOutside: "deny",
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

	draftJSON, err := json.Marshal(p.draft)
	if err != nil {
		t.Fatal(err)
	}
	draft := filepath.Join(dir, "draft.json")
	if err := os.WriteFile(draft, draftJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	meshOK := fmt.Sprintf("cp '%s' \"$4\"\nif [ -n \"$8\" ]; then cp '%s' \"$8\"; fi\n%s", stl, draft, cadSayTriangles("mesh", "ok", "", p.meshTriangles, 0))
	meshOutside := cadSay("mesh", "unreadable", "read_failed", 2)
	if p.meshOutside == "leak" {
		meshOutside = meshOK
	}
	fakeEngine(t, dir, "cad-mesh", fmt.Sprintf("case \"$3\" in\n*cad-preflight-job-*)\n%s;;\n*)\n%s;;\nesac\n", meshOK, meshOutside))

	size := cadPreflightFrame
	for _, tr := range []bool{false, true} {
		frame := filepath.Join(dir, fmt.Sprintf("frame-%v.png", tr))
		if err := os.WriteFile(frame, p.renderFrame(t, size, tr), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	renderDraw := "case \"$6\" in\ntransparent) cp '" + filepath.Join(dir, "frame-true.png") + "' \"$8\";;\n*) cp '" + filepath.Join(dir, "frame-false.png") + "' \"$8\";;\nesac\n" +
		`printf 'CADVERDICT {"stage":"render","verdict":"ok","reason":"","triangles":12,"view":"%s","size":%s,"background":"%s"}\n' "$2" "$4" "$6"` + "\nexit 0\n"
	renderOutside := cadSay("render", "unreadable", "read_failed", 2)
	if p.renderOutside == "leak" {
		renderOutside = renderDraw
	}
	fakeEngine(t, dir, "cad-render", fmt.Sprintf("case \"$7\" in\n*cad-preflight-job-*)\n%s;;\n*)\n%s;;\nesac\n", renderDraw, renderOutside))
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
		{"the render meshes a blank frame", func(p *cadPreflightFakes) {
			p.renderFrame = func(t *testing.T, size int, _ bool) []byte { return blankPNGBytes(t, size) }
		}, []string{"pixel judge"}},
		{"the render draws the outside file", func(p *cadPreflightFakes) { p.renderOutside = "leak" }, []string{"render", "confinement"}},
		{"the measured unit is not inch", func(p *cadPreflightFakes) { p.draft.DeclaredUnit, p.draft.AppliedFactor = "mm", floatp(1) }, []string{"units", "inch"}},
		{"the volume is not the inch box volume", func(p *cadPreflightFakes) { *p.draft.Volume *= 2 }, []string{"units", "volume"}},
		{"the area is not the inch box area", func(p *cadPreflightFakes) { *p.draft.Area *= 2 }, []string{"units", "area"}},
		{"the extents are not 1 2 3", func(p *cadPreflightFakes) { p.draft.BBox.Max = []float64{25.4, 50.8, 25.4} }, []string{"units", "extents"}},
		{"the unit is not declared", func(p *cadPreflightFakes) { p.draft.DeclaredUnit = "" }, []string{"units"}},
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

func TestCADPreflightDoesNotTakeASandboxFailureOnTheOutsideRenderForADenial(t *testing.T) {
	p := newCADPreflightFakes(t)
	c := p.converter(t)
	helper, _ := fakeSandboxHelper(t)
	wrapper := filepath.Join(t.TempDir(), "refuse-outside-render.sh")
	body := fmt.Sprintf("#!/bin/bash\nfor a in \"$@\"; do\n  case \"$a\" in\n    */out.stl) case \"$*\" in *cad-preflight-outside-*cad-render*|*cad-render*cad-preflight-outside-*) exit 78;; esac;;\n  esac\ndone\nexec %q \"$@\"\n", helper[0])
	if err := os.WriteFile(wrapper, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	c = c.WithSandbox([]string{wrapper, helper[1]})
	err := CADPreflight(context.Background(), c)
	if err == nil {
		t.Fatal("CADPreflight accepted a sandbox that could not be established for the outside render")
	}
	if !strings.Contains(err.Error(), "the cad render stage could not establish its sandbox") {
		t.Errorf("error %q does not name the render stage as the one without a sandbox", err)
	}
}

func TestCADRenderReadOnlyIsGrantedToNoOutsideDirectory(t *testing.T) {
	w, cleanup, err := newPreflightWorkspace("cad")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := w.requireOutsideGrants(cadRenderReadOnly); err != nil {
		t.Fatal(err)
	}
	if err := w.requireOutsideGrants([]string{w.outsideDir}); err == nil {
		t.Fatal("requireOutsideGrants accepted a grant that covers the outside directory")
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
		_, err := ConfinedCAD([]string{"helper"}, n, "CAD_WORKER_CONCURRENCY", CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultRenderTimeout, CADDefaultDXFTimeout, CADDefaultRsvgTimeout, CADDefaultEngineTimeout)
		if err == nil || !strings.Contains(err.Error(), "CAD_WORKER_CONCURRENCY") {
			t.Errorf("concurrency %d: err = %v, want a refusal naming CAD_WORKER_CONCURRENCY", n, err)
		}
	}
	c, err := ConfinedCAD([]string{"helper"}, 1, "CAD_WORKER_CONCURRENCY", CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultRenderTimeout, CADDefaultDXFTimeout, CADDefaultRsvgTimeout, CADDefaultEngineTimeout)
	if err != nil {
		t.Fatalf("concurrency 1: %v", err)
	}
	if !c.Sandbox.confines() || c.GuardTimeout != CADDefaultGuardTimeout || c.MeshTimeout != CADDefaultMeshTimeout || c.RenderTimeout != CADDefaultRenderTimeout ||
		c.DXFTimeout != CADDefaultDXFTimeout || c.RsvgTimeout != CADDefaultRsvgTimeout {
		t.Errorf("ConfinedCAD = %+v, want a confined converter carrying the given stage timeouts", c)
	}
	open, err := ConfinedCAD(nil, 1, "CAD_WORKER_CONCURRENCY", CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultRenderTimeout, CADDefaultDXFTimeout, CADDefaultRsvgTimeout, CADDefaultEngineTimeout)
	if err != nil || open.Sandbox.confines() {
		t.Errorf("ConfinedCAD with no helper = %+v, %v, want an unconfined converter", open, err)
	}
}

func TestValidateCADTimeouts(t *testing.T) {
	s := time.Second
	if err := ValidateCADTimeouts(CADDefaultGuardTimeout, CADDefaultMeshTimeout, CADDefaultRenderTimeout, CADDefaultDXFTimeout, CADDefaultRsvgTimeout, CADDefaultEngineTimeout); err != nil {
		t.Errorf("the defaults: %v", err)
	}
	if err := ValidateCADTimeouts(30*s, 316*s, 480*s, 162*s, 141*s, 886*s); err != nil {
		t.Errorf("30+316+480 and 30+162+141 against 886: %v", err)
	}
	if err := ValidateCADTimeouts(30*s, 316*s, 480*s, 500*s, 296*s, 886*s); err != nil {
		t.Errorf("a DXF sum exactly at the engine timeout: %v", err)
	}
	cases := []struct {
		name                                   string
		guard, mesh, render, dxf, rsvg, engine time.Duration
		want                                   []string
	}{
		{"zero guard", 0, 316 * s, 480 * s, 162 * s, 141 * s, 886 * s, []string{"CAD_GUARD_TIMEOUT"}},
		{"negative mesh", 30 * s, -s, 480 * s, 162 * s, 141 * s, 886 * s, []string{"CAD_MESH_TIMEOUT"}},
		{"zero render", 30 * s, 316 * s, 0, 162 * s, 141 * s, 886 * s, []string{"CAD_RENDER_TIMEOUT"}},
		{"zero dxf", 30 * s, 316 * s, 480 * s, 0, 141 * s, 886 * s, []string{"CAD_DXF_TIMEOUT"}},
		{"negative dxf", 30 * s, 316 * s, 480 * s, -s, 141 * s, 886 * s, []string{"CAD_DXF_TIMEOUT"}},
		{"zero rsvg", 30 * s, 316 * s, 480 * s, 162 * s, 0, 886 * s, []string{"CAD_RSVG_TIMEOUT"}},
		{"negative rsvg", 30 * s, 316 * s, 480 * s, 162 * s, -s, 886 * s, []string{"CAD_RSVG_TIMEOUT"}},
		{"zero engine", 30 * s, 316 * s, 480 * s, 162 * s, 141 * s, 0, []string{"CAD_ENGINE_TIMEOUT"}},
		{"render one second over the sum", 30 * s, 316 * s, 481 * s, 162 * s, 141 * s, 886 * s, []string{"CAD_RENDER_TIMEOUT"}},
		{"stages and allowance exceed the engine", 30 * s, 316 * s, 480 * s, 162 * s, 141 * s, 885 * s, []string{"CAD_ENGINE_TIMEOUT"}},
		{"a long mesh alone breaks the sum", 30 * s, 400 * s, 480 * s, 162 * s, 141 * s, 886 * s, []string{"CAD_MESH_TIMEOUT"}},
		{"the DXF sum one second over the engine", 30 * s, 316 * s, 480 * s, 500 * s, 297 * s, 886 * s, []string{"CAD_DXF_TIMEOUT", "CAD_RSVG_TIMEOUT", "CAD_ENGINE_TIMEOUT"}},
		{"a long dxf stage alone breaks the DXF sum", 30 * s, 100 * s, 100 * s, 800 * s, 141 * s, 886 * s, []string{"CAD_DXF_TIMEOUT", "CAD_RSVG_TIMEOUT"}},
		{"a long rsvg stage alone breaks the DXF sum", 30 * s, 100 * s, 100 * s, 162 * s, 700 * s, 886 * s, []string{"CAD_DXF_TIMEOUT", "CAD_RSVG_TIMEOUT"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCADTimeouts(tc.guard, tc.mesh, tc.render, tc.dxf, tc.rsvg, tc.engine)
			if err == nil {
				t.Fatalf("accepted, want a refusal naming %v", tc.want)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err = %v, want one naming %s", err, w)
				}
			}
		})
	}
}

func TestConfinedCADCarriesTheDXFTimeoutsAndRefusesAnOverBudgetSum(t *testing.T) {
	s := time.Second
	c, err := ConfinedCAD([]string{"helper"}, 1, "CAD_WORKER_CONCURRENCY", 30*s, 316*s, 480*s, 7*s, 9*s, 886*s)
	if err != nil {
		t.Fatal(err)
	}
	if c.DXFTimeout != 7*s || c.RsvgTimeout != 9*s || c.dxfTimeout() != 7*s || c.rsvgTimeout() != 9*s {
		t.Errorf("ConfinedCAD = %+v, want DXFTimeout 7s and RsvgTimeout 9s", c)
	}
	if _, err := ConfinedCAD([]string{"helper"}, 1, "CAD_WORKER_CONCURRENCY", 30*s, 316*s, 480*s, 600*s, 300*s, 886*s); err == nil || !strings.Contains(err.Error(), "CAD_DXF_TIMEOUT") {
		t.Errorf("an over-budget DXF sum: err = %v, want a refusal naming CAD_DXF_TIMEOUT", err)
	}
	if _, err := ConfinedCAD([]string{"helper"}, 2, "CAD_WORKER_CONCURRENCY", 30*s, 316*s, 480*s, 162*s, 141*s, 886*s); err == nil {
		t.Error("concurrency 2 was accepted with the DXF timeouts")
	}
}

func cadEnvValue(env []string, key string) (string, int) {
	var v string
	n := 0
	for _, e := range env {
		if k, val, ok := strings.Cut(e, "="); ok && k == key {
			v = val
			n++
		}
	}
	return v, n
}

func TestCADDrawStageGivesAJobLocalHome(t *testing.T) {
	t.Setenv("HOME", "/nonexistent-worker-home")
	t.Setenv("LANG", "C.UTF-8")
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("LP_NUM_THREADS", "8")
	jobDir := t.TempDir()
	for _, stage := range []struct {
		name     string
		readOnly []string
		engines  []string
	}{{"dxf", cadDXFReadOnly, cadDXFEngines}, {"rsvg", cadRsvgReadOnly, cadRsvgEngines}} {
		t.Run(stage.name, func(t *testing.T) {
			x, err := (CADConverter{}).cadDrawStage(jobDir, stage.readOnly, stage.engines)
			if err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(jobDir, ".home")
			for _, dir := range []string{home, filepath.Join(home, ".cache"), filepath.Join(home, ".config")} {
				info, err := os.Lstat(dir)
				if err != nil {
					t.Fatalf("%s was not created: %v", dir, err)
				}
				if !info.IsDir() || info.Mode().Perm() != 0o700 {
					t.Errorf("%s: mode %v, want a 0700 directory", dir, info.Mode())
				}
			}
			for key, want := range map[string]string{
				"HOME":            home,
				"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
				"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
				"TMPDIR":          jobDir,
				"PATH":            os.Getenv("PATH"),
				"LANG":            "C.UTF-8",
				"LC_ALL":          "C.UTF-8",
			} {
				if got, n := cadEnvValue(x.env, key); n != 1 || got != want {
					t.Errorf("%s appears %d times as %q, want once as %q (env %v)", key, n, got, want, x.env)
				}
			}
			if _, n := cadEnvValue(x.env, "LP_NUM_THREADS"); n != 0 {
				t.Errorf("the draw stage passes LP_NUM_THREADS: %v", x.env)
			}
			if len(x.env) != 7 {
				t.Errorf("env %v has %d entries, want exactly the seven named ones", x.env, len(x.env))
			}
		})
	}
}

func TestCADDrawStageEnvironmentReachesTheEngine(t *testing.T) {
	t.Setenv("HOME", "/nonexistent-worker-home")
	t.Setenv("DATABASE_URL", "postgres://secret")
	dir := t.TempDir()
	jobDir := t.TempDir()
	fakeEngine(t, dir, "cad-dxf", "env > \"$TMPDIR/engine.env\"\n")
	pathWith(t, dir)
	x, err := (CADConverter{}).cadDrawStage(jobDir, cadDXFReadOnly, cadDXFEngines)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := x.runBounded(context.Background(), cadOutputLimit, "cad-dxf"); err != nil {
		t.Fatalf("run: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(jobDir, "engine.env"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if got, n := cadEnvValue(lines, "HOME"); n != 1 || got != filepath.Join(jobDir, ".home") {
		t.Errorf("the engine saw HOME %q (%d times), want the job-local home", got, n)
	}
	if _, n := cadEnvValue(lines, "DATABASE_URL"); n != 0 {
		t.Error("the engine saw the worker's DATABASE_URL")
	}
}

func TestCADDrawStageMultiarchOnlyWhenConfined(t *testing.T) {
	triple := cadMultiarchLib[runtime.GOARCH]
	lib := "ro:/usr/lib/" + triple
	helper, _ := fakeSandboxHelper(t)
	confined, err := (CADConverter{}).WithSandbox(helper).cadDrawStage(t.TempDir(), cadRsvgReadOnly, cadRsvgEngines)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(confined.spec, lib) || !slices.Contains(confined.spec, "ro:/usr/share/fonts") {
		t.Errorf("confined spec %v lacks %s or the fonts", confined.spec, lib)
	}
	open, err := (CADConverter{}).cadDrawStage(t.TempDir(), cadRsvgReadOnly, cadRsvgEngines)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(open.spec, lib) {
		t.Errorf("unconfined spec %v carries the multiarch directory", open.spec)
	}
	saved := cadMultiarchLib
	t.Cleanup(func() { cadMultiarchLib = saved })
	cadMultiarchLib = map[string]string{}
	if _, err := (CADConverter{}).WithSandbox(helper).cadDrawStage(t.TempDir(), cadDXFReadOnly, cadDXFEngines); err == nil {
		t.Error("cadDrawStage returned nil for an architecture with no multiarch directory")
	}
}

func TestCADDrawStageRefusesASymlinkedHome(t *testing.T) {
	jobDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(jobDir, ".home")); err != nil {
		t.Fatal(err)
	}
	if _, err := (CADConverter{}).cadDrawStage(jobDir, cadDXFReadOnly, cadDXFEngines); err == nil {
		t.Error("cadDrawStage accepted a .home that is a symlink out of the job directory")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("cadDrawStage wrote %d entries through the symlink", len(entries))
	}
}

func TestCADSetsDrawSetsAreTheSeeds(t *testing.T) {
	const remeasure = "these are the 75-RESEARCH seed sets that plan 75-08 measures on the production image; " +
		"re-measure and update SANDBOX-PATHS.md, the Go vars and this pin together rather than editing the expectation"
	for _, p := range []struct {
		name      string
		got, want []string
	}{
		{"cadDXFReadOnly", cadDXFReadOnly, []string{"/opt/venv", "/usr/bin/python3.11", "/usr/lib/python3.11", "/usr/share/fonts"}},
		{"cadDXFEngines", cadDXFEngines, []string{"cad-dxf"}},
		{"cadRsvgReadOnly", cadRsvgReadOnly, []string{"/usr/share/fonts"}},
		{"cadRsvgEngines", cadRsvgEngines, []string{"rsvg-convert"}},
	} {
		if !slices.Equal(p.got, p.want) {
			t.Errorf("%s = %v, want %v: %s", p.name, p.got, p.want, remeasure)
		}
	}
	for name, set := range map[string][]string{"cadDXFReadOnly": cadDXFReadOnly, "cadRsvgReadOnly": cadRsvgReadOnly} {
		for _, p := range set {
			for _, banned := range []string{"/tmp", "/proc", "/sys"} {
				if p == banned || strings.HasPrefix(p, banned+"/") {
					t.Errorf("%s grants %s", name, p)
				}
			}
		}
	}
}

func TestCADEngineTimeoutStaysUnderTheReconcilerWindow(t *testing.T) {
	const window = 900 * time.Second
	if CADDefaultEngineTimeout >= window {
		t.Errorf("CADDefaultEngineTimeout = %s, must stay under the %s RECONCILER_ACTIVE_STALE_AFTER window or the reconciler re-enqueues live cad jobs", CADDefaultEngineTimeout, window)
	}
	if grace := CADDefaultEngineTimeout + 20*time.Second; grace >= window+10*time.Second || grace != 906*time.Second {
		t.Errorf("pod grace = %s, want 906s (engine + 20s)", grace)
	}
}

func TestCADSetsAreTheMeasuredOnes(t *testing.T) {
	const remeasure = "these are the sets scripts/cad-worker-sandbox-measure.sh measured on the production engine image " +
		"(evidence/74-06-landlock.txt, SANDBOX-PATHS.md section cad); re-run scripts/cad-worker-sandbox-measure.sh and update the " +
		"document, the Go vars and this pin together rather than editing the expectation"
	pins := []struct {
		name      string
		got, want []string
	}{
		{"cadMeshReadOnly", cadMeshReadOnly, []string{"/opt/venv", "/usr/bin/python3.11", "/usr/lib/python3.11"}},
		{"cadGuardReadOnly", cadGuardReadOnly, []string{"/usr/bin/python3.11", "/usr/lib/python3.11"}},
		{"cadRenderReadOnly", cadRenderReadOnly, []string{"/opt/venv", "/usr/bin/python3.11", "/usr/lib/python3.11"}},
		{"cadMeshEngines", cadMeshEngines, []string{"cad-mesh"}},
		{"cadGuardEngines", cadGuardEngines, []string{"cad-guard"}},
		{"cadRenderEngines", cadRenderEngines, []string{"cad-render"}},
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
	for name, set := range map[string][]string{"cadMeshReadOnly": cadMeshReadOnly, "cadGuardReadOnly": cadGuardReadOnly, "cadRenderReadOnly": cadRenderReadOnly} {
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

func TestCADRenderHelperImportsNoOpenCascade(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cad-worker/helper/cad-render")
	if err != nil {
		t.Fatal(err)
	}
	importLine := regexp.MustCompile(`^\s*(import|from)\s`)
	forbidden := regexp.MustCompile(`\b(OCP|cadquery|OCC)\b|__import__|importlib`)
	vtk, imports := 0, 0
	for i, line := range strings.Split(string(raw), "\n") {
		code, _, _ := strings.Cut(line, "#")
		if !importLine.MatchString(code) && !strings.Contains(code, "__import__") && !strings.Contains(code, "importlib") {
			continue
		}
		imports++
		if strings.Contains(code, "vtkmodules") {
			vtk++
		}
		if forbidden.MatchString(code) {
			t.Errorf("cad-render line %d imports %q: the render stage must not reach OpenCascade; its separate exec grant "+
				"is what keeps OCP out, and a path set cannot (R-09)", i+1, strings.TrimSpace(line))
		}
	}
	if imports == 0 || vtk == 0 {
		t.Fatalf("read %d import lines and %d from vtkmodules: the check would pass on a helper it did not read", imports, vtk)
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
