package api

// This suite pins TODAY's (pre-Phase-59) exact byte body of POST /v1/inspect
// for four PDF uploads, per 59-CONTEXT.md and the D-15 protocol inherited from
// 58-CONTEXT.md (itself a reproduction of 57-CONTEXT.md D-17 and 56-CONTEXT.md
// D-10 step 1). Phase 59 changes this response body in two specific ways, and
// pinning it first is what makes "exactly this changed, and nothing else" a
// checkable statement rather than an assertion by the person who changed it:
//
//   - Plan 59-05 adds the keys text_layer and text_layer_reason to
//     characteristics. Every other field -- format, declared_format,
//     size_bytes, findings, the composition and ORDER of checks, coverage, and
//     the whole operations block -- must stay byte-identical.
//   - Plan 59-02 widens which reasons flip coverage.complete to false. For
//     these four fixtures, all of which are small, valid, and fully readable,
//     coverage must not change at all; if it does, the widening caught
//     something it was not meant to catch.
//
// Разрешённые исключения (closed list of permitted exceptions) -- and unlike
// Phase 58's own list, this one is NOT empty:
//
//  1. The appearance of exactly two new keys, "text_layer" and
//     "text_layer_reason", inside characteristics (Plan 59-05).
//  2. A change to the coverage object caused by Plan 59-02's widened
//     reason set.
//
// Everything else is frozen. A plan that needs a wider exception is changing
// the contract, not the test: stop and escalate rather than editing this suite
// or regenerating its golden bytes.
//
// The mechanical immutability check, and its expected value, is:
//
//	git rev-list --count HEAD -- internal/api/testdata/pre59_inspect_pdf.golden.json   # must print 1
//
// This is `git rev-list --count`, not a ref-less `git diff`: `git diff
// --name-only` with no ref compares the working tree against the index and
// prints nothing the instant a change is committed -- i.e. it goes blind in
// EXACTLY the scenario D-15 exists to catch (golden bytes and SUT edited
// together in one commit). A commit count is not blind to that.
//
// Determinism note. The operations block carries live quota verdicts and
// per-engine duration estimates, so a floating value there would make this
// snapshot worthless. It is deterministic here without any normalization, and
// deliberately so rather than by luck: newTestServer builds the server with
// fakeRepo (PreAdmit returns its zero-value nil error), and engineTimeouts /
// maxEngineBytes come from NewServer's own compiled-in defaults (api.go), not
// from the environment. No field of this response is masked or rewritten
// before comparison -- what is pinned is the whole body exactly as a client
// would receive it.

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden59 regenerates testdata/pre59_*.golden.json from the CURRENT
// handler output. It exists only to create the fixture on the very first run
// against pre-Phase-59 code (59-01-PLAN.md Task 2), and later to apply one of
// the two permitted exceptions above -- never to make a failing pin pass.
//
// The name must differ from -update-golden and -update-golden-58: two flags
// registered under one name in the same package panic with "flag redefined"
// at test-binary startup, which would take down every test in internal/api,
// not merely this file.
var updateGolden59 = flag.Bool("update-golden-59", false, "rewrite testdata/pre59_inspect_pdf.golden.json from the current handler output")

// pre59InspectFixtures is the closed set of PDFs this snapshot covers. Three
// come from the corpus committed by 59-01 Task 1 (see that directory's own
// PROVENANCE.md for what each one proves and its reference pdftotext count);
// the fourth is the OCR fixture the worker's own born-digital guard has been
// tested against since Phase 43, included here so the two halves of this phase
// are pinned against a file they share.
//
// Paths are repo-relative from this package's directory, resolved with
// filepath.Join so this works regardless of the working directory `go test`
// was invoked from.
var pre59InspectFixtures = []struct {
	name string
	path []string
}{
	{"objstm_born_digital", []string{"..", "inspect", "testdata", "pdf", "objstm_born_digital.pdf"}},
	{"uncompressed_text", []string{"..", "inspect", "testdata", "pdf", "uncompressed_text.pdf"}},
	{"embedded_font", []string{"..", "inspect", "testdata", "pdf", "embedded_font.pdf"}},
	{"born_digital", []string{"..", "convert", "testdata", "ocr", "born_digital.pdf"}},
}

// assertGolden59 compares got against testdata/pre59_<name>, or (with
// -update-golden-59) rewrites that fixture from got. The expectation is read
// from a file on disk, never from an inline literal in this source: that is
// the entire point of D-15 -- the expectation must live somewhere the person
// changing the SUT cannot silently edit in the same breath.
func assertGolden59(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "pre59_"+name)
	if *updateGolden59 {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s (-update-golden-59)", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s: response bytes changed -- want byte-identical to %s (D-15 pinning violation).\n"+
			"Only two changes are permitted in Phase 59, both listed in this file's header: the two new\n"+
			"characteristics keys (Plan 59-05) and the coverage object (Plan 59-02). Anything else means the\n"+
			"contract moved, not the test.\ngot:  %s\nwant: %s", name, path, got, want)
	}
}

// TestPre59GoldenInspectPDF pins POST /v1/inspect's exact pre-Phase-59 body for
// every fixture in pre59InspectFixtures. One golden file holds all four cases,
// keyed by case name, so the snapshot is a single artifact whose diff shows
// every affected fixture at once rather than four files that could drift apart.
//
// The map is encoded with the SAME settings writeJSON uses (json.NewEncoder
// with SetEscapeHTML(false)) plus SetIndent for a readable diff. Go's encoder
// sorts map keys, so the file is byte-deterministic across runs. Each body is
// stored as json.RawMessage, preserving the handler's own bytes verbatim
// instead of a re-marshalled approximation of them.
func TestPre59GoldenInspectPDF(t *testing.T) {
	type pinnedResponse struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}

	snapshot := make(map[string]pinnedResponse, len(pre59InspectFixtures))
	for _, fx := range pre59InspectFixtures {
		data, err := os.ReadFile(filepath.Join(fx.path...))
		if err != nil {
			t.Fatalf("read fixture %s: %v -- the corpus is committed by 59-01 Task 1 and must exist before this suite runs", fx.name, err)
		}

		srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
		status, body := doInspect(t, srv, fx.name+".pdf", data)
		if status != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200; body=%s", fx.name, status, body)
		}
		snapshot[fx.name] = pinnedResponse{Status: status, Body: json.RawMessage(body)}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snapshot); err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}

	assertGolden59(t, "inspect_pdf.golden.json", buf.Bytes())
}
