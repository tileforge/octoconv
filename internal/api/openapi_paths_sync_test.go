// This file makes docs/openapi.yaml's PATH SET a contract rather than
// documentation, the same way internal/errcode/openapi_sync_test.go does for
// the Error.code enum and internal/convert/readme_sync_test.go for the
// README's engine-class list.
//
// It exists because the spec drifted exactly the way an unenforced document
// always eventually does: GET /v1/jobs/{id}/events and POST
// /v1/jobs/{id}/cancel were registered in routes.go and never written into
// the spec, so an SDK generated from a file whose own description promises it
// matches "exactly as implemented" could neither stream progress nor cancel a
// job. Nothing failed, because nothing was checking.
//
// The source of truth is the ROUTER, walked with chi.Walk -- not a regexp over
// routes.go. A route reaches a client only by being registered, so walking the
// thing that serves requests is the only comparison that cannot be satisfied
// by a line of source that no longer does anything.
package api

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// specPath is repo-relative from this package's directory (internal/api/), so
// it resolves regardless of the directory `go test` runs from.
const specPath = "../../docs/openapi.yaml"

var (
	// specPathKey matches a path item key: two-space indent, leading slash.
	specPathKey = regexp.MustCompile(`^  (/\S*):\s*$`)
	// specMethodKey matches an operation key nested under one.
	specMethodKey = regexp.MustCompile(`^    (get|put|post|delete|patch|head|options):\s*$`)
	// specSectionKey matches any top-level key, used to bound the scan to
	// the paths: block -- `components:` also nests two-space keys, and none
	// of them start with a slash today, but bounding the scan means that
	// staying true is not something this test depends on.
	specSectionKey = regexp.MustCompile(`^([a-zA-Z]\S*):\s*$`)
)

// specOperations returns every "METHOD /path" declared under the spec's
// paths: block.
func specOperations(t *testing.T) map[string]bool {
	t.Helper()

	b, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	ops := make(map[string]bool)
	inPaths := false
	current := ""
	for _, line := range strings.Split(string(b), "\n") {
		if m := specSectionKey.FindStringSubmatch(line); m != nil {
			inPaths = m[1] == "paths"
			current = ""
			continue
		}
		if !inPaths {
			continue
		}
		if m := specPathKey.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := specMethodKey.FindStringSubmatch(line); m != nil && current != "" {
			ops[strings.ToUpper(m[1])+" "+current] = true
		}
	}
	if len(ops) == 0 {
		t.Fatalf("no operations found under paths: in %s -- the scan, not the spec, is probably what broke", specPath)
	}
	return ops
}

// routerOperations returns every "METHOD /path" the service actually serves,
// across both routers a running api process mounts.
func routerOperations(t *testing.T) map[string]bool {
	t.Helper()

	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(),
		defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(),
		healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	ops := make(map[string]bool)
	for _, r := range []chi.Router{srv.Routes(), srv.HealthRoutes()} {
		err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			// chi renders a sub-router's index route as ".../" -- the
			// same endpoint OpenAPI writes without the trailing slash.
			if route != "/" {
				route = strings.TrimSuffix(route, "/")
			}
			ops[method+" "+route] = true
			return nil
		})
		if err != nil {
			t.Fatalf("walk router: %v", err)
		}
	}
	return ops
}

// TestOpenAPIPathsMatchRegisteredRoutes compares the two sets in BOTH
// directions.
//
// One-way checking misses one of the two things that go wrong, and both have
// happened here: an operation in the spec but not the router is a promise of
// an endpoint that 404s, and an operation in the router but not the spec is
// silence about a feature clients cannot generate a call for.
func TestOpenAPIPathsMatchRegisteredRoutes(t *testing.T) {
	spec := specOperations(t)
	router := routerOperations(t)

	var missing, extra []string
	for op := range router {
		if !spec[op] {
			missing = append(missing, op)
		}
	}
	for op := range spec {
		if !router[op] {
			extra = append(extra, op)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("docs/openapi.yaml has drifted from the routes the service registers.\n"+
			"  served but undocumented: %v\n"+
			"  documented but not served: %v\n\n"+
			"Fix whichever side is wrong -- never relax this test. The spec's own description "+
			"promises it matches the implementation exactly, and an SDK generated from it is only "+
			"as good as that promise.", missing, extra)
	}
}
