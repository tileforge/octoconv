package convert

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/apaderin/octoconv/internal/measurements"
)

type cadRenderFixture struct {
	cadFixture
	png string
}

func renderPNGBytes(t *testing.T, size int, transparent bool) []byte {
	t.Helper()
	bg := pixWhite
	if transparent {
		bg = color.NRGBA{}
	}
	img := pixFrame(size, bg)
	pixHexagon(img, size/2, size/2, size*2/5)
	return pixEncode(t, img)
}

func blankPNGBytes(t *testing.T, size int) []byte {
	t.Helper()
	return pixEncode(t, pixFrame(size, pixWhite))
}

func boxDraft(format string, triangles int) cadMeasureDraft {
	d := mmDraft()
	d.Format = format
	d.Triangles = triangles
	if format == "stl" {
		d.DeclaredUnit, d.AppliedFactor, d.Solids, d.ClosedSolids = "", nil, nil, nil
	}
	return d
}

func (f cadRenderFixture) writeDraft(t *testing.T, d cadMeasureDraft) string {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return f.writeSTL(t, "draft.json", raw)
}

func (f cadRenderFixture) writeRawDraft(t *testing.T, raw string) string {
	t.Helper()
	return f.writeSTL(t, "draft.json", []byte(raw))
}

func (f cadRenderFixture) meshScript(t *testing.T, draft string) string {
	stl := f.writeSTL(t, "box.stl", cadBoxSTL(25.4, 50.8, 76.2))
	return fmt.Sprintf("echo \"$@\" > \"$(dirname \"$0\")/mesh.args\"\ncp '%s' \"$4\"\ncp '%s' \"$8\"\n%s", stl, draft, cadSay("mesh", "ok", "", 0))
}

func (f cadRenderFixture) statsScript(draft, stl string, n int) string {
	return f.statsScriptPlacing("cp", draft, stl, n)
}

func (f cadRenderFixture) statsScriptLinking(draft, stl string, n int) string {
	return f.statsScriptPlacing("ln -s", draft, stl, n)
}

func (f cadRenderFixture) statsScriptPlacing(place, draft, stl string, n int) string {
	put := ""
	if stl != "" {
		put = fmt.Sprintf("%s '%s' \"$5\"\n", place, stl)
	}
	return fmt.Sprintf("echo \"$@\" > \"$(dirname \"$0\")/stats.args\"\ncp '%s' \"$3\"\n%s%s", draft, put, cadSayTriangles("mesh", "ok", "", n, 0))
}

func (f cadRenderFixture) boxSTL(t *testing.T) string {
	t.Helper()
	return f.writeSTL(t, "box.stl", cadBoxSTL(10, 20, 30))
}

func writeSparseSTL(t *testing.T, path string, n int) {
	t.Helper()
	header := make([]byte, stlHeaderBytes)
	copy(header, "octoconv sparse test stl")
	binary.LittleEndian.PutUint32(header[80:], uint32(n))
	if err := os.WriteFile(path, header, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, stlHeaderBytes+stlTriangleBytes*int64(n)); err != nil {
		t.Fatal(err)
	}
}

func (f cadRenderFixture) renderScript(t *testing.T, png []byte) string {
	frame := f.writeSTL(t, "frame.png", png)
	return fmt.Sprintf("echo \"$@\" > \"$(dirname \"$0\")/render.args\"\ncp '%s' \"$8\"\nprintf 'CADVERDICT {\"stage\":\"render\",\"verdict\":\"ok\",\"reason\":\"\",\"triangles\":12,\"view\":\"%%s\",\"size\":%%s,\"background\":\"%%s\"}\\n' \"$2\" \"$4\" \"$6\"\n", frame)
}

func newCADRenderFixture(t *testing.T, ext string) cadRenderFixture {
	t.Helper()
	f := newCADFixture(t, ext, cadOKGuard(), "")
	return cadRenderFixture{cadFixture: f, png: filepath.Join(f.jobDir, "out.png")}
}

func (f cadRenderFixture) setEngines(t *testing.T, guard, mesh, render string) {
	t.Helper()
	fakeEngine(t, f.dir, "cad-guard", guard)
	fakeEngine(t, f.dir, "cad-mesh", mesh)
	fakeEngine(t, f.dir, "cad-render", render)
}

func (f cadRenderFixture) args(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(f.marker(name))
	if err != nil {
		t.Fatalf("the stub %s never ran: %v", name, err)
	}
	return strings.TrimSpace(string(raw))
}

func (f cadRenderFixture) sidecar() string { return CADMeasurementsPath(f.png) }

func (f cadRenderFixture) noSidecar(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(f.sidecar()); err == nil {
		t.Error("a measurements sidecar exists after a failed render")
	}
}

func TestCADRenderStepRunsGuardMeshRenderAndWritesTheSidecar(t *testing.T) {
	f := newCADRenderFixture(t, "step")
	draft := f.writeDraft(t, unitsInchDraft())
	f.setEngines(t, "touch \"$(dirname \"$0\")/guard.ran\"\n"+cadOKGuard(), f.meshScript(t, draft), f.renderScript(t, renderPNGBytes(t, 512, true)))
	opts := map[string]any{"view": "top", "size": float64(512), "background": "transparent", "quality": "fine"}
	if err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, opts); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !f.exists("guard.ran") {
		t.Error("a STEP render skipped the guard")
	}
	if got := f.args(t, "mesh.args"); !strings.Contains(got, "--quality fine") || !strings.Contains(got, "--measure "+filepath.Join(f.jobDir, "measure.json")) {
		t.Errorf("mesh argv = %q, want --quality fine and --measure", got)
	}
	if got := f.args(t, "render.args"); !strings.HasPrefix(got, "--view top --size 512 --background transparent ") {
		t.Errorf("render argv = %q", got)
	}
	raw, err := os.ReadFile(f.sidecar())
	if err != nil {
		t.Fatalf("no sidecar: %v", err)
	}
	doc, err := measurements.Validate(raw)
	if err != nil {
		t.Fatalf("the sidecar is not a valid measurements document: %v", err)
	}
	if doc.Unit != "inch" || doc.Format != "step" {
		t.Errorf("document = %+v, want the declared inch STEP", doc)
	}
	info, err := os.Stat(f.sidecar())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("sidecar mode = %v, %v, want 0600", info.Mode().Perm(), err)
	}
}

func TestCADRenderStlSourceSkipsTheGuardAndRunsStats(t *testing.T) {
	f := newCADRenderFixture(t, "stl")
	draft := f.writeDraft(t, boxDraft("stl", 12))
	f.setEngines(t, "touch \"$(dirname \"$0\")/guard.ran\"\n"+cadOKGuard(), "exit 9\n", "")
	fakeEngine(t, f.dir, "cad-mesh", f.statsScript(draft, f.boxSTL(t), 12))
	fakeEngine(t, f.dir, "cad-render", f.renderScript(t, renderPNGBytes(t, 1280, false)))
	if err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if f.exists("guard.ran") {
		t.Error("an STL render ran the guard")
	}
	mesh := filepath.Join(f.jobDir, "mesh.stl")
	if got, want := f.args(t, "stats.args"), "stats --measure "+filepath.Join(f.jobDir, "measure.json")+" "+f.in+" "+mesh; got != want {
		t.Errorf("stats argv = %q, want %q", got, want)
	}
	got := f.args(t, "render.args")
	if want := "--view iso --size 1280 --background opaque " + mesh + " " + f.png; got != want {
		t.Errorf("render argv = %q, want %q", got, want)
	}
	if strings.Contains(got, f.in) {
		t.Errorf("render argv = %q names the client's file", got)
	}
	raw, err := os.ReadFile(f.sidecar())
	if err != nil {
		t.Fatalf("no sidecar: %v", err)
	}
	if doc, err := measurements.Validate(raw); err != nil || doc.Format != "stl" || doc.UnitSource != measurements.SourceNone {
		t.Errorf("document = %+v, %v, want an unspecified-unit stl", doc, err)
	}
}

func TestCADRenderStlRefusesAnUnjudgedMeshBeforeAnyRender(t *testing.T) {
	ascii, err := os.ReadFile(filepath.Join("testdata", "cad", "synthetic", "stl_ascii_box.stl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		stl      func(f cadRenderFixture) string
		verdict  int
		draftTri int
		want     error
	}{
		{"no mesh.stl", func(cadRenderFixture) string { return "" }, 12, 12, ErrCADEmptyResult},
		{"verdict disagrees with the records", func(f cadRenderFixture) string { return f.boxSTL(t) }, 11, 12, ErrCADEmptyResult},
		{"the client's ascii bytes", func(f cadRenderFixture) string { return f.writeSTL(t, "client.stl", ascii) }, 12, 12, ErrCADEmptyResult},
		{"draft disagrees with the records", func(f cadRenderFixture) string { return f.boxSTL(t) }, 12, 13, ErrCADMeasurementsInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCADRenderFixture(t, "stl")
			draft := f.writeDraft(t, boxDraft("stl", tc.draftTri))
			fakeEngine(t, f.dir, "cad-mesh", f.statsScript(draft, tc.stl(f), tc.verdict))
			fakeEngine(t, f.dir, "cad-render", "touch \"$(dirname \"$0\")/render.ran\"\n"+f.renderScript(t, renderPNGBytes(t, 1280, false)))
			err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if f.exists("render.ran") {
				t.Error("the renderer ran on an unjudged mesh")
			}
			f.noSidecar(t)
		})
	}
}

func TestCADRenderStepDraftTrianglesMustEqualTheMesh(t *testing.T) {
	f := newCADRenderFixture(t, "step")
	d := unitsInchDraft()
	d.Triangles = 13
	f.setEngines(t, cadOKGuard(), f.meshScript(t, f.writeDraft(t, d)), "touch \"$(dirname \"$0\")/render.ran\"\n")
	err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
	if !errors.Is(err, ErrCADMeasurementsInvalid) {
		t.Fatalf("err = %v, want ErrCADMeasurementsInvalid", err)
	}
	if f.exists("render.ran") {
		t.Error("the renderer ran with a draft that disagrees with the mesh")
	}
	f.noSidecar(t)
}

func TestCADRenderRefusesAnInputAtTheMeshPath(t *testing.T) {
	for name, rel := range map[string]string{"clean": "mesh.stl", "unclean": "./mesh.stl"} {
		t.Run(name, func(t *testing.T) {
			f := newCADRenderFixture(t, "stl")
			marker := "touch \"$(dirname \"$0\")/e.ran\"\n"
			f.setEngines(t, marker, marker, marker)
			if err := os.WriteFile(filepath.Join(f.jobDir, "mesh.stl"), cadBoxSTL(10, 20, 30), 0o600); err != nil {
				t.Fatal(err)
			}
			in := f.jobDir + string(filepath.Separator) + rel
			if err := (CADRenderConverter{}).Convert(context.Background(), in, f.png, nil); err == nil {
				t.Fatal("Convert accepted an input at the mesh path")
			}
			if f.exists("e.ran") {
				t.Error("an engine ran for an input at the mesh path")
			}
		})
	}
}

func TestCADRenderTriangleCapBoundary(t *testing.T) {
	for _, tc := range []struct {
		name      string
		triangles int
		want      error
	}{
		{"at the cap", CADMaxRenderTriangles, nil},
		{"one over the cap", CADMaxRenderTriangles + 1, ErrCADTooComplex},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCADRenderFixture(t, "stl")
			d := boxDraft("stl", tc.triangles)
			draft := f.writeDraft(t, d)
			sparse := filepath.Join(f.dir, "sparse.stl")
			writeSparseSTL(t, sparse, tc.triangles)
			fakeEngine(t, f.dir, "cad-mesh", f.statsScriptLinking(draft, sparse, tc.triangles))
			fakeEngine(t, f.dir, "cad-render", "touch \"$(dirname \"$0\")/render.ran\"\n"+f.renderScript(t, renderPNGBytes(t, 1280, false)))
			err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Convert at the cap: %v", err)
				}
				if !f.exists("render.ran") {
					t.Error("the renderer never ran at the cap")
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if f.exists("render.ran") {
				t.Error("the renderer ran past the triangle cap")
			}
			f.noSidecar(t)
		})
	}
}

func TestCADRenderZeroExtentIsEmptyBeforeAnyRender(t *testing.T) {
	f := newCADRenderFixture(t, "stl")
	d := boxDraft("stl", 12)
	d.BBox = measurements.BBox{Min: []float64{1, 2, 3}, Max: []float64{1, 2, 3}}
	fakeEngine(t, f.dir, "cad-mesh", f.statsScript(f.writeDraft(t, d), f.boxSTL(t), 12))
	fakeEngine(t, f.dir, "cad-render", "touch \"$(dirname \"$0\")/render.ran\"\n")
	err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
	if !errors.Is(err, ErrCADEmptyResult) {
		t.Fatalf("err = %v, want ErrCADEmptyResult", err)
	}
	if f.exists("render.ran") {
		t.Error("the renderer ran on a zero-extent model")
	}
}

func TestCADRenderStlAboveTheCeilingIsRefusedBeforeAnyEngine(t *testing.T) {
	f := newCADRenderFixture(t, "stl")
	f.setEngines(t, "touch \"$(dirname \"$0\")/e.ran\"\n", "touch \"$(dirname \"$0\")/e.ran\"\n", "touch \"$(dirname \"$0\")/e.ran\"\n")
	if err := os.Truncate(f.in, CADMaxSTLBytes+1); err != nil {
		t.Fatal(err)
	}
	err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
	if !errors.Is(err, ErrCADInputTooLarge) {
		t.Fatalf("err = %v, want ErrCADInputTooLarge", err)
	}
	if f.exists("e.ran") {
		t.Error("an engine ran for an STL above the ceiling")
	}
}

func TestCADRenderRefusesQualityOnAnStlSource(t *testing.T) {
	f := newCADRenderFixture(t, "stl")
	f.setEngines(t, cadOKGuard(), "exit 9\n", "exit 9\n")
	err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, map[string]any{"quality": "fine"})
	if err == nil || !strings.Contains(err.Error(), "quality does not apply") {
		t.Errorf("err = %v, want the stl-quality refusal", err)
	}
}

func cadRenderStepWith(t *testing.T, render string, opts map[string]any, convert func(*CADRenderConverter)) (cadRenderFixture, error) {
	t.Helper()
	f := newCADRenderFixture(t, "step")
	draft := f.writeDraft(t, unitsInchDraft())
	f.setEngines(t, cadOKGuard(), f.meshScript(t, draft), render)
	c := CADRenderConverter{}
	if convert != nil {
		convert(&c)
	}
	return f, c.Convert(context.Background(), f.in, f.png, opts)
}

func TestCADRenderDeadlineIsTerminalRenderTimeout(t *testing.T) {
	f, err := cadRenderStepWith(t, "sleep 30\n", nil, func(c *CADRenderConverter) { c.RenderTimeout = 300 * time.Millisecond })
	if !errors.Is(err, ErrCADRenderTimeout) {
		t.Fatalf("err = %v, want ErrCADRenderTimeout", err)
	}
	if errors.Is(err, ErrCADMeshTimeout) || errors.Is(err, ErrCADRenderFailed) {
		t.Errorf("err = %v also matches another sentinel", err)
	}
	f.noSidecar(t)
}

func TestCADRenderFailuresAreRenderFailedNotUnreadable(t *testing.T) {
	cases := map[string]string{
		"verdict failed":     "printf '%s\\n' 'CADVERDICT {\"stage\":\"render\",\"verdict\":\"failed\",\"reason\":\"render_exception\",\"triangles\":0}'\nexit 1\n",
		"exit 1, no verdict": "echo boom >&2\nexit 1\n",
		"exit 0, no verdict": "exit 0\n",
		"SIGSEGV":            "kill -SEGV $$\nsleep 5\n",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			f, err := cadRenderStepWith(t, script, nil, nil)
			if !errors.Is(err, ErrCADRenderFailed) {
				t.Fatalf("err = %v, want ErrCADRenderFailed", err)
			}
			if errors.Is(err, ErrCADUnreadable) || errors.Is(err, ErrCADEngineKilled) || errors.Is(err, ErrCADRenderTimeout) {
				t.Errorf("err = %v carries a terminal sentinel", err)
			}
			f.noSidecar(t)
		})
	}
}

func TestCADRenderEmptyVerdictIsEmptyResult(t *testing.T) {
	_, err := cadRenderStepWith(t, "printf '%s\\n' 'CADVERDICT {\"stage\":\"render\",\"verdict\":\"empty\",\"reason\":\"zero_extent\",\"triangles\":12}'\nexit 3\n", nil, nil)
	if !errors.Is(err, ErrCADEmptyResult) {
		t.Fatalf("err = %v, want ErrCADEmptyResult", err)
	}
}

func TestCADRenderSigkillStaysEngineKilled(t *testing.T) {
	_, err := cadRenderStepWith(t, "kill -KILL $$\nsleep 5\n", nil, nil)
	if !errors.Is(err, ErrCADEngineKilled) || errors.Is(err, ErrCADRenderFailed) {
		t.Fatalf("err = %v, want ErrCADEngineKilled only", err)
	}
}

func TestCADRenderVerdictFactsMustEqualTheRequest(t *testing.T) {
	png := func(t *testing.T) []byte { return renderPNGBytes(t, 1280, false) }
	for name, line := range map[string]string{
		"view":       `"view":"front","size":1280,"background":"opaque"`,
		"size":       `"view":"iso","size":640,"background":"opaque"`,
		"background": `"view":"iso","size":1280,"background":"transparent"`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newCADRenderFixture(t, "step")
			draft := f.writeDraft(t, unitsInchDraft())
			frame := f.writeSTL(t, "frame.png", png(t))
			render := fmt.Sprintf("cp '%s' \"$8\"\nprintf '%%s\\n' 'CADVERDICT {\"stage\":\"render\",\"verdict\":\"ok\",\"reason\":\"\",\"triangles\":12,%s}'\n", frame, line)
			f.setEngines(t, cadOKGuard(), f.meshScript(t, draft), render)
			err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
			if !errors.Is(err, ErrCADRenderRejected) {
				t.Fatalf("err = %v, want ErrCADRenderRejected", err)
			}
			f.noSidecar(t)
		})
	}
}

func TestCADRenderBlankPNGIsRejectedByPixels(t *testing.T) {
	f := newCADRenderFixture(t, "step")
	draft := f.writeDraft(t, unitsInchDraft())
	f.setEngines(t, cadOKGuard(), f.meshScript(t, draft), f.renderScript(t, blankPNGBytes(t, 1280)))
	err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
	if !errors.Is(err, ErrCADRenderRejected) {
		t.Fatalf("err = %v, want ErrCADRenderRejected", err)
	}
	f.noSidecar(t)
}

func TestCADRenderInvalidDraftIsMeasurementsInvalidAndWritesNothing(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":     "not json",
		"missing keys": `{"format":"step"}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newCADRenderFixture(t, "step")
			draft := f.writeRawDraft(t, raw)
			f.setEngines(t, cadOKGuard(), f.meshScript(t, draft), "touch \"$(dirname \"$0\")/render.ran\"\n")
			err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, nil)
			if !errors.Is(err, ErrCADMeasurementsInvalid) {
				t.Fatalf("err = %v, want ErrCADMeasurementsInvalid", err)
			}
			f.noSidecar(t)
			if f.exists("render.ran") {
				t.Error("the renderer ran with no valid measurements")
			}
		})
	}
}

func TestCADRenderCancelMidRenderLeavesNoProcessOfTheGroupAlive(t *testing.T) {
	f := newCADRenderFixture(t, "step")
	draft := f.writeDraft(t, unitsInchDraft())
	render := "sleep 300 &\necho $! > \"$(dirname \"$8\")/grandchild.pid\"\nwait\n"
	f.setEngines(t, cadOKGuard(), f.meshScript(t, draft), render)
	pidFile := filepath.Join(f.jobDir, "grandchild.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (CADRenderConverter{}).Convert(ctx, f.in, f.png, nil) }()

	var pid int
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(pidFile); err == nil {
			if p, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				pid = p
				break
			}
		}
	}
	if pid == 0 {
		t.Fatal("the fake renderer never forked its grandchild")
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
	for end := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(end) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d of the renderer's process group is still alive 2s after cancel", pid)
		}
	}
}

func TestCADRenderRunsThroughTheHelperUnderItsOwnSet(t *testing.T) {
	f := newCADRenderFixture(t, "step")
	draft := f.writeDraft(t, unitsInchDraft())
	f.setEngines(t, cadOKGuard(), f.meshScript(t, draft), f.renderScript(t, renderPNGBytes(t, 1280, false)))
	helper, calls := fakeSandboxHelper(t)
	c := CADRenderConverter{CADConverter{Sandbox: newEngineSandbox(helper)}}
	if err := c.Convert(context.Background(), f.in, f.png, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	inv := calls()
	if len(inv) != 3 {
		t.Fatalf("the helper ran %d times, want 3 (guard, mesh, render): %v", len(inv), inv)
	}
	if last := strings.Join(inv[2], " "); !strings.Contains(last, "cad-render") {
		t.Errorf("third helper call = %q, want the render engine", last)
	}
}

func TestCADRenderPairsOperationAndNoRegistration(t *testing.T) {
	c := CADRenderConverter{}
	if c.Operation() != OperationRender || c.Engine() != EngineCAD {
		t.Errorf("Operation=%q Engine=%q", c.Operation(), c.Engine())
	}
	want := []Pair{{"step", "png"}, {"iges", "png"}, {"stl", "png"}}
	got := c.Pairs()
	if len(got) != len(want) {
		t.Fatalf("Pairs() = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Pairs()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if CADMeasurementsPath("/job/out.png") != "/job/out.png.measurements.json" {
		t.Errorf("CADMeasurementsPath = %q", CADMeasurementsPath("/job/out.png"))
	}
	if Default.Supports("step", "png") || Default.Supports("stl", "png") || Default.Supports("iges", "png") {
		t.Error("the render converter is registered in Default; exposure belongs to plan 74-05")
	}
}

func TestCADPinnedRenderCapsComeFromTheEvidence(t *testing.T) {
	const evidence = "../../.planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt"
	raw, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	value := func(key string) int64 {
		m := regexp.MustCompile(`(?m)^DERIVED,key=` + key + `,value=(\d+),`).FindSubmatch(raw)
		if m == nil {
			t.Fatalf("no DERIVED line for %s in %s", key, evidence)
		}
		n, err := strconv.ParseInt(string(m[1]), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, c := range []struct {
		name      string
		got, want int64
	}{
		{"CADMaxRenderTriangles", CADMaxRenderTriangles, value("cad_render_triangle_cap")},
		{"CADMaxSTLBytes", CADMaxSTLBytes, value("cad_stl_ceiling_bytes")},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, evidence DERIVED line says %d: re-derive it with scripts/cad-render-probe.sh cost rather than editing the expectation", c.name, c.got, c.want)
		}
	}
}

func TestCADSetsRenderSetIsItsOwnSliceSeededFromTheMeshPaths(t *testing.T) {
	want := []string{"/opt/venv", "/usr/bin/python3.11", "/usr/lib/python3.11"}
	if fmt.Sprint(cadRenderReadOnly) != fmt.Sprint(want) || fmt.Sprint(cadRenderEngines) != "[cad-render]" {
		t.Errorf("cadRenderReadOnly = %v, cadRenderEngines = %v: this set is the seed that plan 74-06 measures on the production image; "+
			"re-measure rather than editing the expectation", cadRenderReadOnly, cadRenderEngines)
	}
	cadRenderReadOnly[0] = "/changed"
	defer func() { cadRenderReadOnly[0] = "/opt/venv" }()
	if cadMeshReadOnly[0] != "/opt/venv" {
		t.Error("cadRenderReadOnly shares its backing array with cadMeshReadOnly")
	}
}
