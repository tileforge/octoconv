package convert

import (
	"slices"
	"testing"
)

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

	want := []string{cadStageGuard, cadStageMesh, cadStageRender, cadStageDXF, cadStageDWG, cadStageInk, cadStageRsvg, cadStageJudge}
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
