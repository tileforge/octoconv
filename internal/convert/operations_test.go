package convert

import (
	"context"
	"strings"
	"testing"
)

// sanitizeIdentityConverter is a synthetic test-only Converter that also
// implements OperationalConverter (Phase 57, D-06's optional-interface
// template). It advertises the pdf->pdf pair under the "sanitize"
// operation on the "probe" engine class -- the identity case (From == To)
// that never existed in this registry before Phase 57 and is exactly the
// case Success Criterion 2 requires be structurally unreachable from the
// convert operation. Convert's body is never called by these tests.
type sanitizeIdentityConverter struct{}

func (sanitizeIdentityConverter) Pairs() []Pair     { return []Pair{{From: "pdf", To: "pdf"}} }
func (sanitizeIdentityConverter) Operation() string { return "sanitize" }
func (sanitizeIdentityConverter) Engine() string    { return "probe" }
func (sanitizeIdentityConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	return nil
}

// inspectIdentityConverter is a second synthetic OperationalConverter,
// advertising the SAME pdf->pdf pair as sanitizeIdentityConverter but under
// a different operation ("inspect") -- the negative control for
// TestRegisterPanicsOnTripleCollision: registering both must NOT panic,
// because the (operation, from, to) triples differ even though (from, to)
// is identical.
type inspectIdentityConverter struct{}

func (inspectIdentityConverter) Pairs() []Pair     { return []Pair{{From: "pdf", To: "pdf"}} }
func (inspectIdentityConverter) Operation() string { return "inspect" }
func (inspectIdentityConverter) Engine() string    { return "probe" }
func (inspectIdentityConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	return nil
}

// sanitizeIdentityConverterAlt is a distinct Go type from
// sanitizeIdentityConverter, but registers the exact same triple
// (sanitize, pdf, pdf) -- used as the second, colliding registration in
// TestRegisterPanicsOnTripleCollision and the overwriting converter B in
// TestReplaceOverwritesWithoutPanic. Being a different %T than
// sanitizeIdentityConverter lets LookupOp's returned value prove WHICH
// converter answered a triple.
type sanitizeIdentityConverterAlt struct{}

func (sanitizeIdentityConverterAlt) Pairs() []Pair     { return []Pair{{From: "pdf", To: "pdf"}} }
func (sanitizeIdentityConverterAlt) Operation() string { return "sanitize" }
func (sanitizeIdentityConverterAlt) Engine() string    { return "probe" }
func (sanitizeIdentityConverterAlt) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	return nil
}

// TestSanitizeIdentityPairDisjointFromConvert is the direct proof of
// Success Criterion 2: a pdf->pdf pair registered under a non-convert
// operation cannot be selected for convert, structurally, by the triple
// key -- not merely "does not happen to collide today."
func TestSanitizeIdentityPairDisjointFromConvert(t *testing.T) {
	r := NewRegistry()
	r.Register(LibreOfficeConverter{}) // includes docx->pdf, a real convert pair
	r.Register(sanitizeIdentityConverter{})

	c, ok := r.LookupOp("sanitize", "pdf", "pdf")
	if !ok {
		t.Fatal("LookupOp(sanitize, pdf, pdf) = false, want true")
	}
	if _, ok := c.(sanitizeIdentityConverter); !ok {
		t.Fatalf("LookupOp(sanitize, pdf, pdf) returned %T, want sanitizeIdentityConverter", c)
	}

	if _, ok := r.Lookup("pdf", "pdf"); ok {
		t.Fatal("Lookup(pdf, pdf) (== convert operation) = true, want false -- sanitize triple leaked into convert")
	}
	if _, ok := r.LookupOp("convert", "pdf", "pdf"); ok {
		t.Fatal("LookupOp(convert, pdf, pdf) = true, want false -- sanitize triple leaked into convert")
	}

	engine, ok := r.EngineForOp("sanitize", "pdf", "pdf")
	if !ok || engine != "probe" {
		t.Fatalf("EngineForOp(sanitize, pdf, pdf) = (%q, %v), want (\"probe\", true)", engine, ok)
	}
	if engine, ok := r.EngineFor("pdf", "pdf"); ok || engine != "" {
		t.Fatalf("EngineFor(pdf, pdf) = (%q, %v), want (\"\", false)", engine, ok)
	}
}

// TestRegisterPanicsOnTripleCollision proves D-08: Register panics rather
// than silently overwriting when a triple is already occupied, and the
// panic message names the triple and points at Replace. The negative
// control proves the panic is triple-scoped, not pair-scoped: the same
// (from, to) under a DIFFERENT operation registers cleanly.
func TestRegisterPanicsOnTripleCollision(t *testing.T) {
	r := NewRegistry()
	r.Register(sanitizeIdentityConverter{})

	func() {
		defer func() {
			rec := recover()
			if rec == nil {
				t.Fatal("Register of a second converter for the same (sanitize, pdf, pdf) triple did not panic")
			}
			msg, ok := rec.(string)
			if !ok {
				t.Fatalf("panic value is %T, want string", rec)
			}
			if !strings.Contains(msg, "pdf") {
				t.Errorf("panic message %q does not mention \"pdf\"", msg)
			}
			if !strings.Contains(msg, "Replace") {
				t.Errorf("panic message %q does not mention Replace", msg)
			}
		}()
		r.Register(sanitizeIdentityConverterAlt{})
	}()

	// Negative control: same (from, to), different operation -- must NOT
	// panic, because the triple (inspect, pdf, pdf) is unoccupied.
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("Register of (inspect, pdf, pdf) panicked unexpectedly: %v", rec)
			}
		}()
		r.Register(inspectIdentityConverter{})
	}()
}

// TestReplaceOverwritesWithoutPanic proves D-09: Replace performs the exact
// overwrite Register refuses, without panicking, both when the triple is
// already occupied and when it is not (a plain insert).
func TestReplaceOverwritesWithoutPanic(t *testing.T) {
	r := NewRegistry()
	r.Register(sanitizeIdentityConverter{})

	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("Replace panicked unexpectedly: %v", rec)
			}
		}()
		r.Replace(sanitizeIdentityConverterAlt{})
	}()

	c, ok := r.LookupOp("sanitize", "pdf", "pdf")
	if !ok {
		t.Fatal("LookupOp(sanitize, pdf, pdf) = false after Replace, want true")
	}
	if _, ok := c.(sanitizeIdentityConverterAlt); !ok {
		t.Fatalf("LookupOp(sanitize, pdf, pdf) returned %T after Replace, want sanitizeIdentityConverterAlt (the replacement)", c)
	}

	// Replace of an unoccupied triple also works -- it is a superset of
	// Register's insert behavior, just without the occupancy check.
	if _, ok := r.LookupOp("inspect", "pdf", "pdf"); ok {
		t.Fatal("triple (inspect, pdf, pdf) unexpectedly already occupied before Replace")
	}
	r.Replace(inspectIdentityConverter{})
	if _, ok := r.LookupOp("inspect", "pdf", "pdf"); !ok {
		t.Fatal("LookupOp(inspect, pdf, pdf) = false after Replace insert, want true")
	}
}

// TestOperationsDerivedFromRegistry proves D-02: Operations()/
// IsKnownOperation are derived from Default's actual registrations, not a
// hardcoded Go slice and not the jobs.operation DB CHECK constraint.
//
// NOTE: the moment Operations() starts returning a second value is the
// moment Phase 60 adds Default.Register(SanitizeConverter{}) to
// converters.go -- updating this test's exact-length assertion at that
// point is expected, not a regression.
func TestOperationsDerivedFromRegistry(t *testing.T) {
	ops := Default.Operations()
	if len(ops) != 1 || ops[0] != "convert" {
		t.Fatalf("Default.Operations() = %v, want [\"convert\"]", ops)
	}

	if !Default.IsKnownOperation("convert") {
		t.Error(`Default.IsKnownOperation("convert") = false, want true`)
	}

	// Reserved-but-empty DB CHECK values, plus the not-yet-registered
	// "sanitize" and an empty string, and a case-mismatched "Convert" that
	// pins the no-normalization rule (OperationConvert's own doc comment).
	for _, bad := range []string{"sanitize", "extract", "archive", "inspect", "render", "", "Convert"} {
		if Default.IsKnownOperation(bad) {
			t.Errorf("Default.IsKnownOperation(%q) = true, want false", bad)
		}
	}
}

// TestOneOperationPerEngineClass proves the D-14 invariant: every engine
// class registered in Default serves exactly one operation. This invariant
// is what keeps the reconciler's job.Engine routing switch, the unique-lock
// TTL lookup, queue names, all seven ScaledObjects' PromQL, and the
// client_limits(client_id, engine)/client_usage_daily(client_id, engine,
// day, status) grain all correct BY CONSTRUCTION, unchanged, in this phase
// -- lifting this invariant (e.g. a second operation sharing an existing
// engine class) requires an explicit decision, not a silent drift, and
// every one of those subsystems would need to be revisited at that point.
func TestOneOperationPerEngineClass(t *testing.T) {
	byEngine := make(map[string]map[string]bool)
	for op, classes := range Default.OperationClasses() {
		for engine := range classes {
			if byEngine[engine] == nil {
				byEngine[engine] = make(map[string]bool)
			}
			byEngine[engine][op] = true
		}
	}

	for engine, ops := range byEngine {
		if len(ops) != 1 {
			opList := make([]string, 0, len(ops))
			for op := range ops {
				opList = append(opList, op)
			}
			t.Errorf("engine %q serves %d operations (%v), want exactly 1", engine, len(ops), opList)
		}
	}
}

// TestClassesExcludesNonConvertOperations proves D-10: Classes() (the
// source GET /v1/formats' "engines" key walks) never surfaces a
// non-convert operation's engine class, while OperationClasses() (the
// source for the new tree Plan 04 adds) does.
func TestClassesExcludesNonConvertOperations(t *testing.T) {
	r := NewRegistry()
	r.Register(LibvipsConverter{})
	r.Register(sanitizeIdentityConverter{})

	if _, ok := r.Classes()["probe"]; ok {
		t.Error(`Classes()["probe"] exists, want absent -- non-convert engine leaked into the convert-only view`)
	}

	pairs, ok := r.OperationClasses()["sanitize"]["probe"]
	if !ok {
		t.Fatal(`OperationClasses()["sanitize"]["probe"] missing, want present`)
	}
	want := Pair{From: "pdf", To: "pdf"}
	found := false
	for _, p := range pairs {
		if p == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(`OperationClasses()["sanitize"]["probe"] = %v, want to contain %v`, pairs, want)
	}
}
