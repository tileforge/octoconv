package convert

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type cadStageRecorder struct{ got []StageUpdate }

func (r *cadStageRecorder) Stage(u StageUpdate) { r.got = append(r.got, u) }

func (r *cadStageRecorder) names() []string {
	var out []string
	for _, u := range r.got {
		out = append(out, u.Stage)
	}
	return out
}

func TestCADStageRoutesMatchTheRegistry(t *testing.T) {
	registered := map[cadRouteKey]bool{}
	for op, classes := range Default.OperationClasses() {
		for _, p := range classes[EngineCAD] {
			key := cadRouteKey{op, p.From, p.To}
			registered[key] = true
			route, ok := CADStages(op, p.From, p.To)
			if !ok || len(route) == 0 {
				t.Errorf("CADStages(%q, %q, %q) = %v, %v; every registered cad pair needs a route", op, p.From, p.To, route, ok)
			}
		}
	}
	for key := range cadStageRoutes {
		if !registered[key] {
			t.Errorf("route %+v is not a registered cad pair", key)
		}
	}

	want := []string{cadStageGuard, cadStageMesh, cadStageRender, cadStageDXF, cadStageInk, cadStageRsvg, cadStageJudge}
	slices.Sort(want)
	if got := CADStageVocabulary(); !slices.Equal(got, want) {
		t.Errorf("CADStageVocabulary() = %v, want %v", got, want)
	}

	for _, alias := range []struct{ op, from, to, canon string }{
		{OperationConvert, "STP", "stl", "step"},
		{OperationConvert, "igs", "stl", "iges"},
		{OperationConvert, "STEP", "STL", "step"},
	} {
		got, ok := CADStages(alias.op, alias.from, alias.to)
		want, _ := CADStages(alias.op, alias.canon, "stl")
		if !ok || !slices.Equal(got, want) {
			t.Errorf("CADStages(%q, %q, %q) = %v, %v; want the %s row %v", alias.op, alias.from, alias.to, got, ok, alias.canon, want)
		}
	}

	for _, miss := range []struct{ op, from, to string }{
		{OperationConvert, "dxf", "png"},
		{OperationRender, "stl", "stl"},
		{"Convert", "step", "stl"},
	} {
		if got, ok := CADStages(miss.op, miss.from, miss.to); ok || len(got) != 0 {
			t.Errorf("CADStages(%q, %q, %q) = %v, %v; want nil, false", miss.op, miss.from, miss.to, got, ok)
		}
	}

	first, _ := CADStages(OperationConvert, "step", "stl")
	first[0] = "tampered"
	again, _ := CADStages(OperationConvert, "step", "stl")
	if again[0] != cadStageGuard {
		t.Errorf("mutating a returned route changed the table: %v", again)
	}
}

func TestCADConvertersReportExactlyTheirRoute(t *testing.T) {
	cases := []struct {
		op, from, to string
		run          func(t *testing.T, conv StageConverter, sink StageSink) error
	}{
		{OperationConvert, "step", "stl", cadSTLStageRun("step")},
		{OperationConvert, "iges", "stl", cadSTLStageRun("iges")},
		{OperationRender, "step", "png", cadRenderStageRun("step")},
		{OperationRender, "iges", "png", cadRenderStageRun("iges")},
		{OperationRender, "stl", "png", cadRenderSTLStageRun},
		{OperationConvert, "dxf", "svg", cadDXFSVGStageRun},
		{OperationConvert, "dxf", "pdf", cadDXFPDFStageRun},
		{OperationRender, "dxf", "png", cadDXFPNGStageRun},
	}
	covered := map[cadRouteKey]bool{}
	for _, c := range cases {
		covered[cadRouteKey{c.op, c.from, c.to}] = true
	}
	for key := range cadStageRoutes {
		if !covered[key] {
			t.Errorf("route %+v has no row in this test", key)
		}
	}
	for key := range covered {
		if _, ok := cadStageRoutes[key]; !ok {
			t.Errorf("test row %+v is not a route", key)
		}
	}
	for _, c := range cases {
		t.Run(c.op+"/"+c.from+"->"+c.to, func(t *testing.T) {
			found, ok := Default.LookupOp(c.op, c.from, c.to)
			if !ok {
				t.Fatalf("no converter registered for %s %s -> %s", c.op, c.from, c.to)
			}
			conv, ok := found.(StageConverter)
			if !ok {
				t.Fatalf("%T does not implement StageConverter", found)
			}
			rec := &cadStageRecorder{}
			if err := c.run(t, conv, rec); err != nil {
				t.Fatalf("ConvertWithStages: %v", err)
			}
			route, _ := CADStages(c.op, c.from, c.to)
			if got := rec.names(); !slices.Equal(got, route) {
				t.Fatalf("reported stages %v, want %v", got, route)
			}
			for i, u := range rec.got {
				if u.Index != i+1 || u.Count != len(route) {
					t.Errorf("update %d = %+v, want Index %d Count %d", i, u, i+1, len(route))
				}
			}
		})
	}
}

func cadSTLStageRun(ext string) func(t *testing.T, conv StageConverter, sink StageSink) error {
	return func(t *testing.T, conv StageConverter, sink StageSink) error {
		f := newCADFixture(t, ext, cadOKGuard(), "")
		fakeEngine(t, f.dir, "cad-mesh", f.goodMesh(t))
		return conv.ConvertWithStages(context.Background(), f.in, f.out, nil, sink)
	}
}

func cadRenderStageRun(ext string) func(t *testing.T, conv StageConverter, sink StageSink) error {
	return func(t *testing.T, conv StageConverter, sink StageSink) error {
		f := newCADRenderFixture(t, ext)
		draft := f.writeDraft(t, unitsInchDraft())
		f.setEngines(t, cadOKGuard(), f.meshScript(t, draft), f.renderScript(t, renderPNGBytes(t, 512, true)))
		opts := map[string]any{"view": "top", "size": float64(512), "background": "transparent", "quality": "fine"}
		return conv.ConvertWithStages(context.Background(), f.in, f.png, opts, sink)
	}
}

func cadRenderSTLStageRun(t *testing.T, conv StageConverter, sink StageSink) error {
	f := newCADRenderFixture(t, "stl")
	draft := f.writeDraft(t, boxDraft("stl", 12))
	f.setEngines(t, cadOKGuard(), f.statsScript(draft, f.boxSTL(t), 12), f.renderScript(t, renderPNGBytes(t, 1280, false)))
	return conv.ConvertWithStages(context.Background(), f.in, f.png, nil, sink)
}

func cadDXFSVGStageRun(t *testing.T, conv StageConverter, sink StageSink) error {
	f := newCADDXFFixture(t, "svg")
	f.setDXF(t, cadDXFFixtureOutputs(t, "wide"))
	f.setRsvg(t, "", 0, "")
	return conv.ConvertWithStages(context.Background(), f.in, f.out, nil, sink)
}

func cadDXFPDFStageRun(t *testing.T, conv StageConverter, sink StageSink) error {
	f := newCADDXFFixture(t, "pdf")
	f.setDXF(t, cadDXFFixtureOutputs(t, "text_cases"))
	f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, "")
	return conv.ConvertWithStages(context.Background(), f.in, f.out, nil, sink)
}

func cadDXFPNGStageRun(t *testing.T, conv StageConverter, sink StageSink) error {
	var frame cadDXFFrame
	for _, fr := range cadDXFFrames(t) {
		if fr.size == CADDefaultRenderSize && fr.background == CADBackgroundOpq {
			frame = fr
		}
	}
	if frame.name == "" {
		t.Fatal("frames.tsv has no default frame")
	}
	drawing := strings.TrimSuffix(frame.name, fmt.Sprintf("_%d_%s.png", frame.size, frame.background))
	f := newCADDXFFixture(t, "png")
	f.setDXF(t, cadDXFRenderOutputs(t, drawing, cadDXFTestFile("wide.measure.json")))
	f.setRsvg(t, cadDXFTestFile(frame.name), 0, "")
	return conv.ConvertWithStages(context.Background(), f.in, f.out, nil, sink)
}

func TestCADStagesStopAtTheFailingStage(t *testing.T) {
	cases := []struct {
		name string
		op   string
		from string
		to   string
		run  func(t *testing.T, conv StageConverter, sink StageSink) error
		want []string
	}{
		{"guard refuses step", OperationConvert, "step", "stl", func(t *testing.T, conv StageConverter, sink StageSink) error {
			f := newCADFixture(t, "step", cadSay("guard", "too_complex", "entities_over_cap", 4), "")
			return conv.ConvertWithStages(context.Background(), f.in, f.out, nil, sink)
		}, []string{cadStageGuard}},
		{"mesh fails step", OperationConvert, "step", "stl", func(t *testing.T, conv StageConverter, sink StageSink) error {
			f := newCADFixture(t, "step", cadOKGuard(), cadSay("mesh", "unreadable", "x", 2))
			return conv.ConvertWithStages(context.Background(), f.in, f.out, nil, sink)
		}, []string{cadStageGuard, cadStageMesh}},
		{"final rsvg fails dxf pdf", OperationConvert, "dxf", "pdf", func(t *testing.T, conv StageConverter, sink StageSink) error {
			f := newCADDXFFixture(t, "pdf")
			f.setDXF(t, cadDXFFixtureOutputs(t, "text_cases"))
			f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, `case "$*" in *"-f pdf"*) exit 1;; esac`)
			return conv.ConvertWithStages(context.Background(), f.in, f.out, nil, sink)
		}, []string{cadStageGuard, cadStageDXF, cadStageInk, cadStageRsvg}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, ok := Default.LookupOp(c.op, c.from, c.to)
			if !ok {
				t.Fatalf("no converter registered for %s %s -> %s", c.op, c.from, c.to)
			}
			conv, ok := found.(StageConverter)
			if !ok {
				t.Fatalf("%T does not implement StageConverter", found)
			}
			rec := &cadStageRecorder{}
			if err := c.run(t, conv, rec); err == nil {
				t.Fatal("ConvertWithStages = nil, want the stage failure")
			}
			if got := rec.names(); !slices.Equal(got, c.want) {
				t.Errorf("reported stages %v, want %v", got, c.want)
			}
		})
	}
}
