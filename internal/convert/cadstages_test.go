package convert

import (
	"context"
	"slices"
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
