package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
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
	respBody := rec.Body.Bytes()

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

	// Two keys as of Phase 60: convert, and sanitize under the probe engine.
	// Asserted by NAME rather than by count alone -- a bare length check
	// stops being able to say which operation appeared.
	if len(resp.Operations) != 2 {
		t.Fatalf("operations has %d keys, want exactly 2; operations=%+v", len(resp.Operations), resp.Operations)
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

	// "sanitize" left this list in Phase 60, when it stopped being a
	// reserved-but-unregistered DB CHECK value and gained a converter. The
	// remaining four are still reserved and still unregistered, and they are
	// what keeps this test meaningful: without them it would assert nothing.
	for _, unregistered := range []string{"extract", "archive", "inspect", "render"} {
		if _, ok := resp.Operations[unregistered]; ok {
			t.Errorf("operations unexpectedly contains %q, want it absent (not yet registered under any engine)", unregistered)
		}
	}
}
