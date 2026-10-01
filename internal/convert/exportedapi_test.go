package convert

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"
)

// mentionsEngineExec reports whether t reaches this package's engineExec
// through its structure. The match is by object identity (package + name of
// the *types.TypeName), not by text, so an inferred var type, an alias, a
// type argument, a struct field and a func signature are all seen.
func mentionsEngineExec(pkg *types.Package, t types.Type, seen map[types.Type]bool) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if seen[t] {
		return false
	}
	seen[t] = true

	switch t := t.(type) {
	case *types.Named:
		obj := t.Obj()
		if obj.Pkg() == pkg && obj.Name() == "engineExec" {
			return true
		}
		if targs := t.TypeArgs(); targs != nil {
			for i := 0; i < targs.Len(); i++ {
				if mentionsEngineExec(pkg, targs.At(i), seen) {
					return true
				}
			}
		}
		// An exported function taking an unexported wrapper around engineExec
		// is the same defect. Exported named types are judged on their own
		// surface, so they are not followed here.
		if obj.Pkg() == pkg && !obj.Exported() {
			return mentionsEngineExec(pkg, t.Underlying(), seen)
		}
		return false
	case *types.Pointer:
		return mentionsEngineExec(pkg, t.Elem(), seen)
	case *types.Slice:
		return mentionsEngineExec(pkg, t.Elem(), seen)
	case *types.Array:
		return mentionsEngineExec(pkg, t.Elem(), seen)
	case *types.Chan:
		return mentionsEngineExec(pkg, t.Elem(), seen)
	case *types.Map:
		return mentionsEngineExec(pkg, t.Key(), seen) || mentionsEngineExec(pkg, t.Elem(), seen)
	case *types.Signature:
		return tupleMentionsEngineExec(pkg, t.Params(), seen) || tupleMentionsEngineExec(pkg, t.Results(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if mentionsEngineExec(pkg, t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			if mentionsEngineExec(pkg, t.Method(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func tupleMentionsEngineExec(pkg *types.Package, tup *types.Tuple, seen map[types.Type]bool) bool {
	if tup == nil {
		return false
	}
	for i := 0; i < tup.Len(); i++ {
		if mentionsEngineExec(pkg, tup.At(i).Type(), seen) {
			return true
		}
	}
	return false
}

// engineExecLeaks walks the exported surface of pkg and returns the exported
// symbols whose surface mentions engineExec, plus the number of exported
// objects examined (so a caller can refuse a vacuous scan).
func engineExecLeaks(pkg *types.Package) (leaks []string, examined int) {
	check := func(name string, t types.Type) {
		if mentionsEngineExec(pkg, t, map[types.Type]bool{}) {
			leaks = append(leaks, name)
		}
	}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		if !token.IsExported(name) {
			continue
		}
		examined++
		obj := scope.Lookup(name)
		tn, isType := obj.(*types.TypeName)
		if !isType || tn.IsAlias() {
			check(name, obj.Type())
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			check(name, obj.Type())
			continue
		}
		switch u := named.Underlying().(type) {
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if f := u.Field(i); f.Exported() {
					check(name+"."+f.Name(), f.Type())
				}
			}
		case *types.Interface:
			for i := 0; i < u.NumMethods(); i++ {
				if m := u.Method(i); m.Exported() {
					check(name+"."+m.Name(), m.Type())
				}
			}
		default:
			check(name, u)
		}
		ms := types.NewMethodSet(types.NewPointer(named))
		for i := 0; i < ms.Len(); i++ {
			if m := ms.At(i).Obj(); m.Exported() {
				check(name+"."+m.Name(), m.Type())
			}
		}
	}
	sort.Strings(leaks)
	return leaks, examined
}

// TestExportedAPIDoesNotExposeEngineExec closes the class behind the Codex P2
// finding on PR #30: an exported symbol whose surface reaches the
// package-private engineExec is either uncallable from outside the package,
// or invites an exported wrapper that supplies a zero engineExec -- an engine
// path with no sandbox. The fix is to unexport the symbol, never to add such a
// wrapper.
//
// What this does NOT catch: an exported wrapper that has no engineExec in its
// signature at all. Only review catches that;
// TestConfinedClassesExecOnlyThroughTheSandbox checks how exec is routed, not
// the shape of the API.
func TestExportedAPIDoesNotExposeEngineExec(t *testing.T) {
	fset, files := sandboxParseRealPackage(t)
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check("octoconv/exportedapi", fset, files, nil)
	if err != nil {
		t.Fatalf("type-check the package: %v", err)
	}
	leaks, examined := engineExecLeaks(pkg)
	if examined == 0 {
		t.Fatal("examined zero exported objects -- the scan read nothing, so a clean result proves nothing")
	}
	t.Logf("examined %d exported objects", examined)
	if len(leaks) > 0 {
		t.Errorf("exported symbols expose engineExec: %s\n"+
			"unexport them; do not add an exported wrapper that supplies a zero engineExec (that runs the engine without a sandbox)",
			strings.Join(leaks, ", "))
	}
}

// TestEngineExecLeakCheckCatchesEveryShape is the inversion of the gate above:
// a synthetic package containing every leak shape and a set of non-leaks. The
// result must EQUAL the expected set, in both directions.
func TestEngineExecLeakCheckCatchesEveryShape(t *testing.T) {
	const src = `package p

type engineExec struct{}

func Leak(x engineExec) {}
func Returns() engineExec { return engineExec{} }
func Variadic(xs ...engineExec) {}

type wrapper struct{ x engineExec }

func Wrapped(w wrapper) {}

var Inferred = engineExec{}

type Alias = engineExec

type Holder struct {
	X engineExec
	y engineExec
}

type Svc struct{}

func (Svc) Run(x engineExec) {}

type Box[T any] struct{ V T }

var Boxed Box[engineExec]

type Hook func(engineExec)

type Iface interface{ Do(engineExec) }

func ok(x engineExec) {}

func (*Svc) run(x engineExec) {}

func Fine(s string) {}

type Hidden struct{ y engineExec }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := (&types.Config{}).Check("p", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatalf("type-check: %v", err)
	}
	got, examined := engineExecLeaks(pkg)
	if examined == 0 {
		t.Fatal("examined zero exported objects")
	}
	want := []string{"Alias", "Boxed", "Holder.X", "Hook", "Iface.Do", "Inferred", "Leak", "Returns", "Svc.Run", "Variadic", "Wrapped"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("leaks = %v\nwant    %v", got, want)
	}
}
