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
package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
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

// routeHandlers maps each served operation to the handler method that serves
// it. It is hand-written, and safe to be: openapi_paths_sync_test.go holds the
// operation set itself equal to the live router, so a route added or removed
// without touching this map surfaces there rather than silently skipping a
// handler here. TestRouteHandlerMapCoversEveryDocumentedOperation below pins
// the other half.
var routeHandlers = map[string]string{
	"GET /healthz":                     "handleHealth",
	"POST /v1/jobs":                    "handleCreateJob",
	"GET /v1/jobs":                     "handleListJobs",
	"POST /v1/inspect":                 "handleInspect",
	"GET /v1/jobs/{id}":                "handleGetJob",
	"GET /v1/jobs/{id}/events":         "handleJobEvents",
	"POST /v1/jobs/{id}/cancel":        "handleCancelJob",
	"GET /v1/formats":                  "handleListFormats",
	"POST /v1/presets":                 "handleCreatePreset",
	"GET /v1/presets":                  "handleListPresets",
	"GET /v1/presets/{name}":           "handleShowPreset",
	"PUT /v1/presets/{name}":           "handleUpdatePreset",
	"DELETE /v1/presets/{name}":        "handleDeactivatePreset",
	"POST /v1/system/presets":          "handleCreateSystemPreset",
	"GET /v1/system/presets":           "handleListSystemPresets",
	"GET /v1/system/presets/{name}":    "handleShowSystemPreset",
	"PUT /v1/system/presets/{name}":    "handleUpdateSystemPreset",
	"DELETE /v1/system/presets/{name}": "handleDeactivateSystemPreset",
	"GET /v1/system/usage":             "handleSystemUsage",
}

// emittedStatuses returns, per function name in this package, every HTTP
// status code reachable from it.
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
				name := fn.Name.Name
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
								callees[name][callee.Sel.Name] = true
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

	for _, op := range sortedKeys(routeHandlers) {
		handler := routeHandlers[op]
		codes, ok := emitted[handler]
		if !ok {
			t.Errorf("%s: no function named %q in package api -- routeHandlers is stale", op, handler)
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
}

// TestRouteHandlerMapCoversEveryDocumentedOperation keeps routeHandlers from
// silently shrinking: an operation dropped from the map would stop being
// checked above while every other test stayed green.
func TestRouteHandlerMapCoversEveryDocumentedOperation(t *testing.T) {
	documented := documentedStatuses(t)
	for op := range documented {
		if _, ok := routeHandlers[op]; !ok {
			t.Errorf("%s is documented in the spec but absent from routeHandlers, so its "+
				"status codes are unchecked -- add it", op)
		}
	}
	for op := range routeHandlers {
		if _, ok := documented[op]; !ok {
			t.Errorf("%s is in routeHandlers but not in the spec -- remove it or document the "+
				"operation", op)
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

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
