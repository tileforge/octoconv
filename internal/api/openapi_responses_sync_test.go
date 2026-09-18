// This file extends the path-set contract of openapi_paths_sync_test.go to
// STATUS CODES, closing the gap that file deliberately left open: comparing
// only method/path lets a handler grow a response the spec never mentions,
// which is how fourteen operations came to emit a 500 that docs/openapi.yaml
// did not document -- including three added in the very commit that fixed the
// missing paths.
//
// The check runs in ONE direction: every status a route can produce must be
// documented. The reverse -- a documented response nothing produces -- is not
// enforced, because over-documenting an error costs a client nothing while a
// status it cannot parse breaks it.
//
// "A route" means the whole chain, not the handler. An earlier revision
// analyzed handlers only and called middleware statuses out of scope; that
// scope note was a claim with a consequence, since auth.Middleware answers a
// failed key resolution with 500 and chi's Recoverer answers a panic with 500
// on EVERY route -- so GET /healthz and GET /v1/formats could hand a client a
// 500 the spec did not mention. Middleware is therefore part of the
// comparison, declared per symbol as the router reports it, with the
// declaration for in-module middleware cross-checked against its source.
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

// symbolSuffix strips the decorations Go appends to a symbol for a method
// value ("-fm") and for a closure returned from a function (".1", ".func1"),
// leaving the name of the declaration a reader can go and open.
var symbolSuffix = regexp.MustCompile(`(?:-fm)?(?:\.(?:func)?\d+)*$`)

func normalizeSymbol(sym string) string {
	return symbolSuffix.ReplaceAllString(sym, "")
}

// middlewareDecl is what one middleware can write to the response.
//
// codes is authoritative for the comparison. verifyPkg/verifyFunc, when set,
// name the in-module declaration those codes are cross-checked against, so a
// status added inside our own middleware fails this file rather than reaching
// a client undocumented. They are empty for third-party middleware, whose
// behaviour is pinned by the module version rather than by anything here --
// stated plainly instead of implied.
type middlewareDecl struct {
	codes      []int
	verifyPkg  string
	verifyFunc string
	why        string
}

// middlewareStatuses is keyed by the symbol the ROUTER reports, normalized --
// not by a name chosen here. A middleware on a route whose symbol is absent
// from this table fails the test: silence about what a wrapper can write is
// the hole this table exists to close, so an unrecognized one must be loud.
var middlewareStatuses = map[string]middlewareDecl{
	"github.com/apaderin/octoconv/internal/auth.Middleware": {
		codes:      []int{401, 500},
		verifyPkg:  "../auth",
		verifyFunc: "Middleware",
	},
	"github.com/apaderin/octoconv/internal/api.(*Server).requireOperator": {
		codes:      []int{404},
		verifyPkg:  ".",
		verifyFunc: "Server.requireOperator",
	},
	"github.com/go-chi/httprate.(*RateLimiter).Handler": {
		codes: []int{429},
		why: "the 429 body is built by internal/ratelimit.limitHandler and installed as an " +
			"httprate option, so the symbol the router reports belongs to httprate even though " +
			"the bytes come from this module -- which is why this entry cannot be cross-checked " +
			"against a package of ours",
	},
	"github.com/go-chi/chi/v5/middleware.Recoverer": {
		codes: []int{500},
		why:   "answers a panic in any downstream handler with 500, on every route it wraps",
	},
	"github.com/go-chi/chi/v5/middleware.RequestID":              {why: "sets a header, never writes a status"},
	"github.com/go-chi/chi/v5/middleware.Logger":                 {why: "observes the response, never writes a status"},
	"github.com/go-chi/chi/v5/middleware.ClientIPFromRemoteAddr": {why: "sets a context value, never writes a status"},
}

// handlerNamePattern matches the symbol runtime.FuncForPC reports for a method
// value bound as a chi endpoint: "<pkgpath>.(*Server).handleX-fm". Anchored at
// the end so an anonymous handler ("...Routes.func1") does not match and is
// reported rather than quietly skipped.
var handlerNamePattern = regexp.MustCompile(`\.\(\*Server\)\.(\w+)-fm$`)

// routeChain is everything that can write a response for one operation: the
// endpoint handler, and every middleware wrapped around it.
type routeChain struct {
	handler     string
	middlewares map[string]bool
}

// routerHandlers returns, for every operation the service serves, the chain
// the router will actually run.
func routerHandlers(t *testing.T) map[string]routeChain {
	t.Helper()

	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(),
		defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(),
		healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	out := map[string]routeChain{}
	for _, router := range []chi.Router{srv.Routes(), srv.HealthRoutes()} {
		err := chi.Walk(router, func(method, route string, h http.Handler, mws ...func(http.Handler) http.Handler) error {
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
			if prev, ok := out[op]; ok && prev.handler != m[1] {
				t.Errorf("%s resolves to two different handlers (%s and %s)", op, prev.handler, m[1])
			}
			chain, ok := out[op]
			if !ok {
				chain = routeChain{middlewares: map[string]bool{}}
			}
			chain.handler = m[1]
			// The same operation can be reachable through more than one
			// router (GET /healthz is), so middleware accumulates across
			// walks rather than replacing: a client can meet any of them.
			for _, mw := range mws {
				if mw == nil {
					continue
				}
				chain.middlewares[normalizeSymbol(runtime.FuncForPC(reflect.ValueOf(mw).Pointer()).Name())] = true
			}
			out[op] = chain
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
	return emittedStatusesIn(t, ".")
}

// emittedStatusesIn is emittedStatuses for an arbitrary package directory, so
// an in-module middleware's declared statuses can be checked against the
// package it actually lives in.
func emittedStatusesIn(t *testing.T, dir string) map[string]map[int]bool {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package %s: %v", dir, err)
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
	chains := routerHandlers(t)

	ops := make([]string, 0, len(chains))
	for op := range chains {
		ops = append(ops, op)
	}
	sort.Strings(ops)

	for _, op := range ops {
		chain := chains[op]

		codes, ok := emitted["Server."+chain.handler]
		if !ok {
			t.Errorf("%s is served by (*Server).%s, which package api does not declare -- "+
				"its status codes are unanalyzable", op, chain.handler)
			continue
		}

		// Who can produce each status, so a failure names the source rather
		// than leaving the reader to find it.
		produced := map[int]string{}
		for code := range codes {
			produced[code] = "handler " + chain.handler
		}
		for _, sym := range sortedSet(chain.middlewares) {
			decl, known := middlewareStatuses[sym]
			if !known {
				t.Errorf("%s is wrapped by middleware %q, which middlewareStatuses does not "+
					"describe. Add it with the statuses it can write -- none is a valid answer, "+
					"stated explicitly. An undescribed wrapper is the silence this table exists "+
					"to prevent.", op, sym)
				continue
			}
			for _, code := range decl.codes {
				if _, already := produced[code]; !already {
					produced[code] = "middleware " + sym
				}
			}
		}

		var missing []int
		for code := range produced {
			if !documented[op][code] {
				missing = append(missing, code)
			}
		}
		sort.Ints(missing)
		for _, code := range missing {
			t.Errorf("%s can return %d (from %s), which docs/openapi.yaml does not document.\n"+
				"Document the response -- never delete it from this list. A status a client "+
				"can receive and cannot parse is the failure this test exists to prevent.",
				op, code, produced[code])
		}
	}

	// Every documented operation must be one this test actually walked.
	// TestOpenAPIPathsMatchRegisteredRoutes compares the same two sets, but
	// this assertion guards THIS test's own coverage: without it a documented
	// operation the walk never reached would go unchecked here, while the
	// other test explained the discrepancy in a message about paths.
	for op := range documented {
		if _, ok := chains[op]; !ok {
			t.Errorf("%s is documented but the router walk never reached it, so its status "+
				"codes went unchecked", op)
		}
	}
}

// TestInModuleMiddlewareDeclarationsMatchTheirSource keeps middlewareStatuses
// from drifting for the middleware this repository owns: a status added inside
// auth.Middleware or requireOperator fails here rather than reaching a client
// undocumented.
//
// Third-party entries are deliberately not checked -- their behaviour is
// pinned by the module version, not by anything in this repository. Each one
// must still say why it cannot be verified, so "unverified" stays a stated
// fact rather than an omission.
func TestInModuleMiddlewareDeclarationsMatchTheirSource(t *testing.T) {
	byPkg := map[string]map[string]map[int]bool{}
	for _, sym := range sortedDecls(middlewareStatuses) {
		decl := middlewareStatuses[sym]
		if decl.verifyPkg == "" {
			if decl.why == "" {
				t.Errorf("%s is declared with neither a package to verify it against nor a "+
					"stated reason it cannot be verified", sym)
			}
			continue
		}
		if byPkg[decl.verifyPkg] == nil {
			byPkg[decl.verifyPkg] = emittedStatusesIn(t, decl.verifyPkg)
		}
		actual, ok := byPkg[decl.verifyPkg][decl.verifyFunc]
		if !ok {
			t.Errorf("%s names %s in %s, which that package does not declare",
				sym, decl.verifyFunc, decl.verifyPkg)
			continue
		}
		declared := map[int]bool{}
		for _, c := range decl.codes {
			declared[c] = true
		}
		var undeclared, unreachable []int
		for c := range actual {
			if !declared[c] {
				undeclared = append(undeclared, c)
			}
		}
		for c := range declared {
			if !actual[c] {
				unreachable = append(unreachable, c)
			}
		}
		sort.Ints(undeclared)
		sort.Ints(unreachable)
		if len(undeclared) > 0 || len(unreachable) > 0 {
			t.Errorf("middlewareStatuses[%q] has drifted from %s.%s.\n"+
				"  it can write, undeclared here: %v\n"+
				"  declared here, but it cannot write: %v",
				sym, decl.verifyPkg, decl.verifyFunc, undeclared, unreachable)
		}
	}
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedDecls(m map[string]middlewareDecl) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
