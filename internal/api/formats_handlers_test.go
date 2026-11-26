package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

// TestListFormats_RegistryDerived verifies GET /v1/formats returns a 200
// with a registry-derived engine->pairs map (D-06) that contains the
// "image" class with a known libvips pair.
func TestListFormats_RegistryDerived(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Engines map[string]struct {
			Pairs [][]string `json:"pairs"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}

	image, ok := resp.Engines["image"]
	if !ok {
		t.Fatalf(`response engines = %+v, want an "image" class`, resp.Engines)
	}
	found := false
	for _, pair := range image.Pairs {
		if len(pair) == 2 && pair[0] == "png" && pair[1] == "webp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(`image.pairs = %+v, want it to contain ["png","webp"]`, image.Pairs)
	}
}

// TestListFormats_Unauthenticated401 proves GET /v1/formats sits inside the
// /v1 auth group (D-07): an unauthenticated request is rejected 401.
func TestListFormats_Unauthenticated401(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := httptest.NewRequest(http.MethodGet, "/v1/formats", nil) // no Authorization header
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestListFormats_EnginesKeyUnchanged is an INDEPENDENT byte proof from
// TestPre57GoldenListFormats -- no parsing, no re-encoding of the response,
// just a raw prefix comparison against testdata/pre57_formats.golden.json.
// It proves the "engines" render landed in the now-larger response body
// VERBATIM, not merely "equivalent after re-encoding". The prefix is valid
// because encoding/json sorts map[string]any keys lexicographically,
// "engines" < "operations" (ASCII 'e' < 'o'), so "engines" is always
// rendered first; and the pair order within each class is deterministic
// because Registry.Classes() sorts pairs internally before this handler
// ever sees them. Trimming the golden file's trailing "}\n" (the top-level
// object's closing brace and writeJSON's Encode-appended newline) leaves
// exactly `{"engines":{...}}`'s content up to (and including) the engines
// sub-map's own closing brace -- which must exactly prefix the response
// verbatim (see the assertion message below) once the response has a
// second top-level key.
func TestListFormats_EnginesKeyUnchanged(t *testing.T) {
	golden, err := os.ReadFile("testdata/pre57_formats.golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	trimmed := bytes.TrimSuffix(golden, []byte("}\n"))
	if len(trimmed) == len(golden) {
		t.Fatalf("golden fixture does not end with the expected \"}\\n\" suffix")
	}

	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if !bytes.Contains(top["engines"], []byte(`"`+convert.EngineCAD+`"`)) {
		t.Fatalf("the live engines map has no %q engine; stripping it below would hide a regression", convert.EngineCAD)
	}
	top["engines"] = withoutEngine(t, top["engines"], convert.EngineCAD)
	var stripped bytes.Buffer
	enc := json.NewEncoder(&stripped)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(top); err != nil {
		t.Fatalf("re-encode response: %v", err)
	}
	respBody := stripped.Bytes()

	if !bytes.HasPrefix(respBody, trimmed) {
		t.Fatalf("response is not a byte-identical prefix of the response for the engines render\ngolden prefix: %s\nresponse:      %s", trimmed, respBody)
	}
	if len(respBody) <= len(trimmed) || respBody[len(trimmed)] != ',' {
		t.Fatalf("byte immediately after the engines prefix = %q, want ','; response=%s", respBody[len(trimmed):], respBody)
	}
}

// TestListFormats_OperationsTreeDerivedFromRegistry is a structural
// comparison (not a byte comparison -- see TestListFormats_EnginesKeyUnchanged
// for that): it asserts operations contains exactly one key, "convert" (the
// only operation registered today), and that operations.convert matches
// engines in both engine-class membership and pair content. The two
// representations diverging is the exact class of defect D-11 avoids in
// handleGetJob -- it cannot happen here because both are derived from the
// same underlying Registry.m.
func TestListFormats_OperationsTreeDerivedFromRegistry(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Engines map[string]struct {
			Pairs [][]string `json:"pairs"`
		} `json:"engines"`
		Operations map[string]map[string]struct {
			Pairs [][]string `json:"pairs"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}

	// Four keys as of Phase 129: convert, render under the cad engine,
	// sanitize under the probe engine, and archive under the archive engine.
	// Asserted by NAME rather than by count alone -- a bare length check
	// stops being able to say which operation appeared.
	if len(resp.Operations) != 4 {
		t.Fatalf("operations has %d keys, want exactly 4; operations=%+v", len(resp.Operations), resp.Operations)
	}
	archiveTree, ok := resp.Operations[convert.OperationArchive]
	if !ok {
		t.Fatalf("operations missing %q; operations=%+v", convert.OperationArchive, resp.Operations)
	}
	archiveEngine, ok := archiveTree[convert.EngineArchive]
	if !ok || len(archiveTree) != 1 {
		t.Fatalf("operations.archive must hold only the archive engine; archive=%+v", archiveTree)
	}
	if len(archiveEngine.Pairs) != 1 || len(archiveEngine.Pairs[0]) != 2 || archiveEngine.Pairs[0][0] != "files" || archiveEngine.Pairs[0][1] != "zip" {
		t.Fatalf("operations.archive pairs = %+v, want [[files zip]]", archiveEngine.Pairs)
	}
	renderTree, ok := resp.Operations["render"]
	if !ok {
		t.Fatalf(`operations missing "render" key; operations=%+v`, resp.Operations)
	}
	if _, ok := renderTree["cad"]; !ok || len(renderTree) != 1 {
		t.Errorf(`operations.render must hold only the "cad" engine; render=%+v`, renderTree)
	}
	convertTree, ok := resp.Operations["convert"]
	if !ok {
		t.Fatalf(`operations missing "convert" key; operations=%+v`, resp.Operations)
	}
	sanitizeTree, ok := resp.Operations["sanitize"]
	if !ok {
		t.Fatalf(`operations missing "sanitize" key; operations=%+v`, resp.Operations)
	}
	if _, ok := sanitizeTree["probe"]; !ok {
		t.Errorf(`operations.sanitize has no "probe" engine; sanitize=%+v`, sanitizeTree)
	}
	// The engines key must NOT have gained probe: it enumerates conversion
	// classes, and sanitize converts nothing. This is the claim Phase 57's
	// Classes() filter was written to make true, checked here now that there
	// is finally a non-convert operation to check it against.
	if _, ok := resp.Engines["probe"]; ok {
		t.Errorf(`engines unexpectedly contains "probe" -- it serves no convert pair`)
	}
	// Each sanitize pair maps a format to its SANITIZED FORM -- itself for
	// PDF and images, the macro-free equivalent for macro-enabled OOXML
	// (D-06). Asserting identity here was wrong and passed anyway, because
	// the registry was registering identity pairs and leaving
	// SanitizedOOXMLExtension unused (PR #12).
	for _, pair := range sanitizeTree["probe"].Pairs {
		if len(pair) != 2 {
			t.Fatalf("malformed sanitize pair %v", pair)
		}
		if want := convert.SanitizedOOXMLExtension(pair[0]); pair[1] != want {
			t.Errorf("sanitize pair %v: target is %q, want %q", pair, pair[1], want)
		}
	}

	if len(convertTree) != len(resp.Engines) {
		t.Fatalf("operations.convert has %d engine classes, engines has %d; want equal\nconvert=%+v\nengines=%+v", len(convertTree), len(resp.Engines), convertTree, resp.Engines)
	}
	for class, engineEntry := range resp.Engines {
		opEntry, ok := convertTree[class]
		if !ok {
			t.Errorf("operations.convert missing engine class %q present in engines", class)
			continue
		}
		if !sameSortedPairs(engineEntry.Pairs, opEntry.Pairs) {
			t.Errorf("operations.convert[%q].pairs = %+v, engines[%q].pairs = %+v; want equal sets", class, opEntry.Pairs, class, engineEntry.Pairs)
		}
	}
}

// sameSortedPairs compares two [][]string pair sets for equal membership,
// ignoring order -- used only to compare engines vs. operations.convert,
// which are independently rendered from the same registry data and are not
// guaranteed (by this test's own contract) to iterate in the same order.
func sameSortedPairs(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	toSet := func(pairs [][]string) map[string]int {
		set := make(map[string]int, len(pairs))
		for _, p := range pairs {
			if len(p) != 2 {
				continue
			}
			set[p[0]+"->"+p[1]]++
		}
		return set
	}
	setA, setB := toSet(a), toSet(b)
	if len(setA) != len(setB) {
		return false
	}
	for k, v := range setA {
		if setB[k] != v {
			return false
		}
	}
	return true
}

// TestListFormats_NoOperationOutsideRegistry proves the operations tree
// stays scoped to what the REGISTRY actually knows, not to what the
// jobs.operation DB CHECK constraint happens to accept: sanitize (not
// registered under any engine until Phase 60) and the other pre-declared
// future operations (extract/archive/inspect/render) must not appear as
// keys, even though the database schema partially accepts them already.
func TestListFormats_NoOperationOutsideRegistry(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Operations map[string]json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}

	// "sanitize" left this list in Phase 60, "render" in Phase 74, and
	// "archive" in Phase 129, when each stopped being a reserved-but-
	// unregistered DB CHECK value and gained a converter. extract and
	// inspect are still reserved and still unregistered, and they are what
	// keeps this test meaningful: without them it would assert nothing.
	for _, unregistered := range []string{"extract", "inspect"} {
		if _, ok := resp.Operations[unregistered]; ok {
			t.Errorf("operations unexpectedly contains %q, want it absent (not yet registered under any engine)", unregistered)
		}
	}
}

func TestListFormats_AdvertisesOnlyAllowlistedCADPairs(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Engines map[string]struct {
			Pairs [][]string `json:"pairs"`
		} `json:"engines"`
		Operations map[string]map[string]struct {
			Pairs [][]string `json:"pairs"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if len(resp.Engines) == 0 {
		t.Fatalf("decoded engines map is empty; body=%s", rec.Body.String())
	}

	cad := map[string]bool{
		"step": true, "stp": true, "iges": true, "igs": true,
		"stl": true, "dxf": true, "dwg": true,
	}
	convertPairs := map[string]bool{"step>stl": true, "iges>stl": true, "dxf>svg": true, "dxf>pdf": true, "dwg>svg": true, "dwg>pdf": true, "dwg>png": true}
	renderPairs := map[string]bool{"step>png": true, "iges>png": true, "stl>png": true, "dxf>png": true, "dwg>png": true}
	allowedAt := func(where string) map[string]bool {
		if where == "operations."+convert.OperationRender {
			return renderPairs
		}
		return convertPairs
	}
	found := map[string]bool{}
	check := func(where, class string, pairs [][]string) {
		for _, pair := range pairs {
			touches := class == convert.EngineCAD
			for _, format := range pair {
				if cad[format] {
					touches = true
				}
			}
			if !touches {
				continue
			}
			key := strings.Join(pair, ">")
			if !allowedAt(where)[key] || class != convert.EngineCAD {
				t.Errorf("%s advertises CAD pair %v under %q, want only step -> stl, iges -> stl, dxf -> svg, dxf -> pdf, dwg -> svg, dwg -> pdf and dwg -> png under cad for convert and step, iges, stl, dxf -> png and dwg -> png under cad for render", where, pair, class)
				continue
			}
			found[where+"|"+key] = true
		}
	}
	for class, e := range resp.Engines {
		check("engines", class, e.Pairs)
	}
	for op, classes := range resp.Operations {
		for class, e := range classes {
			check("operations."+op, class, e.Pairs)
		}
	}
	for _, where := range []string{"engines", "operations.convert", "operations.render"} {
		for key := range allowedAt(where) {
			if !found[where+"|"+key] {
				t.Errorf("%s does not advertise %s under cad", where, key)
			}
		}
	}
}
