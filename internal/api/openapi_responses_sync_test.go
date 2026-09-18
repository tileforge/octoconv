// This file extends the path-set contract of openapi_paths_sync_test.go to
// STATUS CODES, closing the gap that file deliberately left open: comparing
// only method/path lets a handler grow a response the spec never mentions,
// which is how fourteen operations came to emit a 500 that docs/openapi.yaml
// did not document -- including three added in the very commit that fixed the
// missing paths.
//
// The check runs in ONE direction on purpose: every status a handler can emit
// must be documented. The reverse is not enforced, and that is not laziness --
// the spec legitimately documents responses no handler body contains, because
// auth.Middleware produces 401, ratelimit produces 429 and requireOperator
// produces 404 for every route they wrap. Demanding the reverse would make
// this test fire on correct documentation, and a test that cries wolf gets
// deleted rather than obeyed.
//
// Emitted statuses come from the AST: the http.Status* constants reachable
// from a handler, following calls to package-local functions and to methods on
// the Server receiver so a status buried in a helper (writeQuotaError's 429)
// still counts. The closure UNDER-approximates -- an unresolved indirect call
// hides whatever it emits -- and that error direction is the safe one: a
// missed status makes this test permissive, never noisy.
//
// WHICH handler to analyze is read off the live router, never from a list kept
// here. An earlier revision of this file did keep one, arguing it was safe
// because the path-set test pins the operation set. That argument is exactly
// wrong: the path-set test compares method and path and never looks at what is
// bound to them, so rebinding a route to a different handler leaves it green
// while this test goes on analyzing a function that no longer serves the
// route. The check and the checked thing have to be the same object -- and
// chi.Walk hands over the endpoint handler itself, whose symbol name
// runtime.FuncForPC recovers.
package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// httpStatusConstants maps the net/http constant names this package uses to
// their numeric codes. Listed rather than derived because net/http exposes no
// name->code table, and a constant this package starts using but that is
// missing here would silently drop out of the comparison -- so
// TestEveryEmittedStatusConstantIsMapped below fails on an unlisted one.
var httpStatusConstants = map[string]int{
	"StatusOK":                    200,
	"StatusCreated":               201,
	"StatusAccepted":              202,
	"StatusBadRequest":            400,
	"StatusUnauthorized":          401,
	"StatusForbidden":             403,
	"StatusNotFound":              404,
	"StatusConflict":              409,
	"StatusRequestEntityTooLarge": 413,
	"StatusUnprocessableEntity":   422,
	"StatusTooManyRequests":       429,
	"StatusInternalServerError":   500,
	"StatusServiceUnavailable":    503,
}

// handlerNamePattern matches the symbol runtime.FuncForPC reports for a method
// value bound as a chi endpoint: "<pkgpath>.(*Server).handleX-fm". Anchored at
// the end so an anonymous handler ("...Routes.func1") does not match and is
// reported rather than quietly skipped.
var handlerNamePattern = regexp.MustCompile(`\.\(\*Server\)\.(\w+)-fm$`)

// routerHandlers returns, for every operation the service serves, the name of
// the function the router will actually dispatch to.
func routerHandlers(t *testing.T) map[string]string {
	t.Helper()

	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(),
		defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(),
		healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	out := map[string]string{}
	for _, router := range []chi.Router{srv.Routes(), srv.HealthRoutes()} {
		err := chi.Walk(router, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
			// chi renders a sub-router's index route as ".../" -- the same
			// endpoint OpenAPI writes without the trailing slash.
			if route != "/" {
				route = strings.TrimSuffix(route, "/")
			}
			op := method + " " + route

			if h == nil {
				t.Errorf("%s is registered with a nil handler", op)
				return nil
			}
			symbol := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
			m := handlerNamePattern.FindStringSubmatch(symbol)
			if m == nil {
				t.Errorf("%s is served by %q, which is not a named method on *Server, so its "+
					"status codes cannot be analyzed. Bind the route to a named method rather "+
					"than letting this gate silently stop covering it.", op, symbol)
				return nil
			}
			if prev, ok := out[op]; ok && prev != m[1] {
				t.Errorf("%s resolves to two different handlers (%s and %s)", op, prev, m[1])
			}
			out[op] = m[1]
			return nil
		})
		if err != nil {
			t.Fatalf("walk router: %v", err)
		}
	}
	if len(out) == 0 {
		t.Fatal("walked the router and found no routes -- the walk, not the router, is probably what broke")
	}
	return out
}

// qualify names a declaration the way emittedStatuses keys it: a method on T
// as "T.Name", a package-level function as "Name".
//
// Qualified rather than keyed by bare name so a method on another receiver
// cannot merge its statuses into a Server handler of the same name. No such
// collision exists today -- but "it is fine because the names happen to be
// unique" is an unchecked premise, and this file exists because one of those
// failed.
func qualify(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// emittedStatuses returns, per qualified declaration name in this package,
// every HTTP status code reachable from it.
func emittedStatuses(t *testing.T) map[string]map[int]bool {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	direct := map[string]map[int]bool{}
	callees := map[string]map[string]bool{}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				name := qualify(fn)
				if direct[name] == nil {
					direct[name] = map[int]bool{}
					callees[name] = map[string]bool{}
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					switch node := n.(type) {
					case *ast.SelectorExpr:
						if id, ok := node.X.(*ast.Ident); ok && id.Name == "http" {
							if code, ok := httpStatusConstants[node.Sel.Name]; ok {
								direct[name][code] = true
							}
						}
					case *ast.CallExpr:
						switch callee := node.Fun.(type) {
						case *ast.Ident:
							callees[name][callee.Name] = true
						case *ast.SelectorExpr:
							// s.helper(...) -- a method on the Server
							// receiver, the only receiver handlers use.
							if id, ok := callee.X.(*ast.Ident); ok && id.Name == "s" {
								callees[name]["Server."+callee.Sel.Name] = true
							}
						}
					}
					return true
				})
			}
		}
	}

	// Transitive closure to a fixed point: a handler owns every status its
	// helpers can emit.
	for changed := true; changed; {
		changed = false
		for fn, cs := range callees {
			for callee := range cs {
				for code := range direct[callee] {
					if !direct[fn][code] {
						direct[fn][code] = true
						changed = true
					}
				}
			}
		}
	}
	return direct
}

var (
	specPathKeyRe   = regexp.MustCompile(`^  (/\S*):\s*$`)
	specMethodKeyRe = regexp.MustCompile(`^    (get|put|post|delete|patch|head|options):\s*$`)
	specStatusKeyRe = regexp.MustCompile(`^        "(\d{3})":\s*$`)
)

// documentedStatuses returns, per "METHOD /path", every status code the spec
// declares a response for.
func documentedStatuses(t *testing.T) map[string]map[int]bool {
	t.Helper()

	b, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	out := map[string]map[int]bool{}
	path, op := "", ""
	for _, line := range strings.Split(string(b), "\n") {
		if m := specPathKeyRe.FindStringSubmatch(line); m != nil {
			path, op = m[1], ""
			continue
		}
		if m := specMethodKeyRe.FindStringSubmatch(line); m != nil && path != "" {
			op = strings.ToUpper(m[1]) + " " + path
			if out[op] == nil {
				out[op] = map[int]bool{}
			}
			continue
		}
		if m := specStatusKeyRe.FindStringSubmatch(line); m != nil && op != "" {
			code, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("unparseable status key %q", m[1])
			}
			out[op][code] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("no operations found in %s -- the scan, not the spec, is probably what broke", specPath)
	}
	return out
}

// TestOpenAPIDocumentsEveryEmittedStatus is the contract: a client generated
// from the spec never meets a status the spec did not declare.
func TestOpenAPIDocumentsEveryEmittedStatus(t *testing.T) {
	emitted := emittedStatuses(t)
	documented := documentedStatuses(t)
	handlers := routerHandlers(t)

	ops := make([]string, 0, len(handlers))
	for op := range handlers {
		ops = append(ops, op)
	}
	sort.Strings(ops)

	for _, op := range ops {
		handler := handlers[op]
		codes, ok := emitted["Server."+handler]
		if !ok {
			t.Errorf("%s is served by (*Server).%s, which package api does not declare -- "+
				"its status codes are unanalyzable", op, handler)
			continue
		}
		var missing []int
		for code := range codes {
			if !documented[op][code] {
				missing = append(missing, code)
			}
		}
		sort.Ints(missing)
		if len(missing) > 0 {
			t.Errorf("%s (%s) can emit %v, which docs/openapi.yaml does not document.\n"+
				"Document the response -- never delete it from this list. A status a client "+
				"can receive and cannot parse is the failure this test exists to prevent.",
				op, handler, missing)
		}
	}

	// Every documented operation must be one this test actually walked.
	// TestOpenAPIPathsMatchRegisteredRoutes compares the same two sets, but
	// this assertion guards THIS test's own coverage: without it a documented
	// operation the walk never reached would go unchecked here, while the
	// other test explained the discrepancy in a message about paths.
	for op := range documented {
		if _, ok := handlers[op]; !ok {
			t.Errorf("%s is documented but the router walk never reached it, so its status "+
				"codes went unchecked", op)
		}
	}
}

// TestEveryEmittedStatusConstantIsMapped fails if this package starts using a
// net/http status constant httpStatusConstants does not know, which would
// otherwise drop that status out of the comparison unnoticed.
func TestEveryEmittedStatusConstantIsMapped(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				se, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := se.X.(*ast.Ident)
				if !ok || id.Name != "http" || !strings.HasPrefix(se.Sel.Name, "Status") {
					return true
				}
				if _, known := httpStatusConstants[se.Sel.Name]; !known {
					t.Errorf("http.%s is used in package api but missing from httpStatusConstants "+
						"-- add it, or the status it names goes unchecked", se.Sel.Name)
				}
				return true
			})
		}
	}
}
