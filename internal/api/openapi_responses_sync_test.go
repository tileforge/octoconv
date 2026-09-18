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
// 500 the spec did not mention.
//
// The two halves are established differently, because they have to be.
//
// HANDLERS are read from source. A request cannot reach every branch of a
// handler, so asking one what it can return would under-report.
//
// MIDDLEWARE is OBSERVED: the router is driven with requests shaped to trip
// each rejection -- no key, a resolver that fails, a spent rate budget, a
// caller who is not an operator -- and whatever comes back must be documented.
// The revision before this one identified middleware by the symbol
// runtime.FuncForPC reports, which is a compiler artifact: Go 1.26.4 inlines
// auth.Middleware into Routes and reports it as
// "(*Server).Routes.func1.Middleware", so that test was red on the toolchain
// go.mod pins while passing on 1.27.1, where it is not inlined. A property
// that changes with an inlining decision cannot be the basis of a contract,
// and a request is the same request under every compiler.
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
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/jobs"
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
//
// Handler identity comes from the symbol too, and that is sound where
// middleware identity was not: a handler is bound as a method VALUE, which the
// compiler does not inline away -- verified on both go1.26.4 and go1.27.1. If
// that ever stops holding, the symbol stops matching handlerNamePattern and
// this fails loudly by name rather than analyzing the wrong function.
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

// probeServer builds a server whose dependencies answer instantly, so a probe
// request exercises the middleware chain rather than a fake's behaviour.
func probeServer(resolver auth.ClientResolver, mutate func(*Config)) *Server {
	cfg := Config{
		MaxUploadBytes:       1 << 20,
		IPRateLimitRPM:       100000,
		ClientRateLimitRPM:   100000,
		SSEPollInterval:      time.Millisecond,
		SSEHeartbeatInterval: time.Millisecond,
		SSEMaxStreamDuration: 10 * time.Millisecond,
		OperatorClientIDs:    map[uuid.UUID]struct{}{},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	// A job owned by somebody else: every job route answers from its ownership
	// guard and returns at once, so no probe streams or dereferences nil.
	repo := &fakeRepo{getJob: &jobs.Job{ID: uuid.New(), ClientID: uuid.New(), Status: jobs.StatusDone}}
	return NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(),
		defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver,
		healthyDeps(), cfg)
}

// failingResolver fails with an error that is NOT ErrInvalidKey, which is how
// auth.Middleware is made to answer 500 rather than 401.
type failingResolver struct{}

func (failingResolver) ResolveClient(context.Context, string) (*clients.Client, error) {
	return nil, errors.New("probe: resolver unavailable")
}

// pathParam fills a route pattern's wildcards with syntactically valid values,
// so a probe is rejected by middleware rather than by id parsing.
var pathParam = regexp.MustCompile(`\{[^}]+\}`)

func concreteURL(route string) string {
	return pathParam.ReplaceAllStringFunc(route, func(m string) string {
		if m == "{id}" {
			return uuid.NewString()
		}
		return "probe"
	})
}

// observe drives every operation against srv and records the status that came
// back, merging into seen.
//
// It asserts nothing about WHICH status arrives. The contract is that whatever
// a client can receive is documented, so a scenario that turns out not to
// trigger what it was written for simply contributes one more real status
// instead of failing on an expectation about internals.
func observe(t *testing.T, srv *Server, ops []string, repeat int, prep func(*http.Request), seen map[string]map[int]bool) {
	t.Helper()

	main := srv.Routes()
	health := srv.HealthRoutes()

	for _, op := range ops {
		method, route, ok := strings.Cut(op, " ")
		if !ok {
			t.Fatalf("malformed operation key %q", op)
		}
		router := main
		if route == "/healthz" {
			router = health
		}
		for i := 0; i < repeat; i++ {
			req := httptest.NewRequest(method, concreteURL(route), nil)
			if prep != nil {
				prep(req)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if seen[op] == nil {
				seen[op] = map[int]bool{}
			}
			seen[op][rec.Code] = true
		}
	}
}

func withKey(req *http.Request) { req.Header.Set("Authorization", "ApiKey "+testClientKey) }

// TestRecovererAnswersAPanicOn500 proves the premise behind requiring every
// operation to document 500: chi's Recoverer sits in the stack every route
// inherits, so a panic anywhere downstream reaches the client as 500.
//
// Proved by making one panic rather than by naming the middleware, for the
// same reason the rest of this file stopped naming it.
func TestRecovererAnswersAPanicOn500(t *testing.T) {
	srv := probeServer(newFakeResolver(), nil)
	router := srv.Routes()
	router.Get("/zz-panic-probe", func(http.ResponseWriter, *http.Request) {
		panic("probe")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/zz-panic-probe", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a panic in a route on this router produced %d, not 500 -- if Recoverer is no "+
			"longer in the stack, the blanket 500 requirement below is no longer justified and "+
			"must be reasoned out again rather than kept", rec.Code)
	}
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

	// Who can produce each status, so a failure names the source rather than
	// leaving the reader to find it.
	produced := map[string]map[int]string{}
	record := func(op string, code int, source string) {
		if produced[op] == nil {
			produced[op] = map[int]string{}
		}
		if _, already := produced[op][code]; !already {
			produced[op][code] = source
		}
	}

	for _, op := range ops {
		codes, ok := emitted["Server."+handlers[op]]
		if !ok {
			t.Errorf("%s is served by (*Server).%s, which package api does not declare -- "+
				"its status codes are unanalyzable", op, handlers[op])
			continue
		}
		for code := range codes {
			record(op, code, "handler "+handlers[op])
		}
		// Justified by TestRecovererAnswersAPanicOn500: a panic in any
		// handler reaches the client as 500, on every route.
		record(op, http.StatusInternalServerError, "chi's Recoverer, on a panic in any handler")
	}

	// proves is the status each scenario exists to elicit. It is checked only
	// for having happened SOMEWHERE, not per route -- the point is not to pin
	// which routes reject, it is that a scenario cannot quietly stop doing
	// anything and leave this test green over an empty observation.
	scenarios := []struct {
		name   string
		srv    *Server
		repeat int
		prep   func(*http.Request)
		proves int
	}{
		{"a request with no API key", probeServer(newFakeResolver(), nil), 1, nil, http.StatusUnauthorized},
		{"a request whose key cannot be resolved", probeServer(failingResolver{}, nil), 1, withKey, http.StatusInternalServerError},
		{"a client past its rate budget", probeServer(newFakeResolver(), func(c *Config) {
			c.ClientRateLimitRPM = 1
			c.IPRateLimitRPM = 1
		}), 3, withKey, http.StatusTooManyRequests},
		{"an authenticated caller who is not an operator", probeServer(newFakeResolver(), nil), 1, withKey, http.StatusNotFound},
	}
	for _, sc := range scenarios {
		seen := map[string]map[int]bool{}
		observe(t, sc.srv, ops, sc.repeat, sc.prep, seen)

		if len(seen) != len(ops) {
			t.Errorf("scenario %q reached %d of %d operations -- a probe that does not run is "+
				"not a probe that passed", sc.name, len(seen), len(ops))
		}
		elicited := false
		for op, codes := range seen {
			for code := range codes {
				record(op, code, sc.name)
				if code == sc.proves {
					elicited = true
				}
			}
		}
		if !elicited {
			t.Errorf("scenario %q no longer produces %d anywhere. Either the middleware it "+
				"exercises is gone -- in which case delete the scenario deliberately -- or the "+
				"probe stopped tripping it and has been silently testing nothing.",
				sc.name, sc.proves)
		}
	}

	for _, op := range ops {
		codes := make([]int, 0, len(produced[op]))
		for code := range produced[op] {
			codes = append(codes, code)
		}
		sort.Ints(codes)
		for _, code := range codes {
			if documented[op][code] {
				continue
			}
			t.Errorf("%s can return %d (from %s), which docs/openapi.yaml does not document.\n"+
				"Document the response -- never delete it from this list. A status a client "+
				"can receive and cannot parse is the failure this test exists to prevent.",
				op, code, produced[op][code])
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
