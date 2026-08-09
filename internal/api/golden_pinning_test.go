package api

// This suite pins TODAY's (pre-Phase-57) exact byte body of two endpoints,
// GET /v1/formats and GET /v1/jobs/{id} (a done job with a single output),
// per 57-CONTEXT.md D-17 -- the same protocol as 56-CONTEXT.md D-10 step 1,
// reproduced here as D-17 because this phase adds a new dimension
// (operation) and a new dimension (output roles) to the exact same two
// endpoints. Both golden files under testdata/pre57_*.golden.json are
// committed in the FIRST commit of Phase 57, before a single line of new
// production code lands (57-01-PLAN.md Task 1); this file assumes that
// commit already happened and simply proves the two fixtures still
// byte-match TODAY's handler output on every subsequent run. Every later
// plan in this phase MUST leave this suite passing WITHOUT modification --
// the SUT changes, this test does not -- with EXACTLY three closed allowed
// exceptions ("Разрешённые исключения") to the Go source (never to the
// golden bytes themselves), listed here and ONLY here, so a future executor
// finds the complete list in one place instead of assembling it from other
// plans' PLAN.md files:
//
//  1. Plan 57-04, Task 2 -- TestPre57GoldenListFormats moves to comparing a
//     sub-map. GET /v1/formats gains the additive "operations" key (D-10),
//     so the full response body stops being byte-equal to the golden file.
//     The test changes to compare the golden bytes against the "engines"
//     sub-map ONLY, re-encoded with the same encoder settings writeJSON
//     uses. The golden file itself does not change by one byte.
//
//  2. Plan 57-06, Task 1 -- the TestPre57GoldenGetJobSingleOutput fixture
//     gains Role: "primary" on its jobs.Output literal. jobs.Output.Role
//     does not exist yet at the point this suite is written (plan 57-03,
//     wave 2, adds the field), so it is deliberately not set here and its
//     zero value is the empty string. Once handleGetJob starts selecting
//     download_url by Role == "primary" (57-06), this fixture would stop
//     having a primary output, download_url would disappear from the
//     response, and this pin would fail. D-13's write-time invariant does
//     NOT reach here: it is enforced by MarkDone against the database,
//     while fakeRepo.Outputs (handlers_test.go) returns the fixture literal
//     directly, bypassing both the database and its DEFAULT 'primary'. ONLY
//     the jobs.Output{...} literal inside this test is edited; the golden
//     file does not change by one byte, because "role" is not part of a
//     single-output done job's response body.
//
//  3. Plan 57-06, Task 2 -- the D-18 inversion run. A temporary revert of
//     handleGetJob to outs[0], under which a NEW test in handlers_test.go
//     MUST fail, followed by reverting the inversion. This is a procedure,
//     not a commit.
//
// Separately and explicitly: NO plan in Phase 57 may change the BYTES of
// internal/api/testdata/pre57_formats.golden.json or
// internal/api/testdata/pre57_job_done_single_output.golden.json. The three
// exceptions above are exclusively about this file's Go test CODE, never
// about golden-FILE bytes. The mechanical immutability check, and its
// expected value, is:
//
//	git rev-list --count HEAD -- internal/api/testdata/pre57_formats.golden.json            # must print 1
//	git rev-list --count HEAD -- internal/api/testdata/pre57_job_done_single_output.golden.json  # must print 1
//
// This is `git rev-list --count`, not a ref-less `git diff`: `git diff
// --name-only internal/api/testdata/` with no ref compares the working tree
// against the index and prints nothing the instant a change is committed --
// i.e. it goes blind in EXACTLY the scenario D-17 exists to catch (golden
// bytes and SUT edited together in one commit). A commit count is not
// blind to that.

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/jobs"
)

// updateGolden regenerates testdata/pre57_*.golden.json from the CURRENT
// handler output. It exists only to create/refresh the fixtures on the very
// first run against pre-Phase-57 code (57-01-PLAN.md Task 1); it must never
// be passed once the fixtures are committed, since that would defeat D-17.
var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/pre57_*.golden.json from the current handler output")

// assertGolden compares got against the on-disk fixture
// testdata/pre57_<name>, or (with -update-golden) rewrites that fixture from
// got. The read happens directly against the file on disk -- never against
// an inline literal in this test's source -- which is the entire point of
// D-17: the expectation lives where the person changing the SUT cannot
// silently edit it in the same commit.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := "testdata/pre57_" + name
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s (-update-golden)", path)
		return
	}
	want, err := os.ReadFile("testdata/pre57_" + name)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s: response bytes changed -- want byte-identical to testdata/%s (D-17 pinning violation)\ngot:  %s\nwant: %s", name, path, got, want)
	}
}

// TestPre57GoldenListFormats pins GET /v1/formats' exact pre-Phase-57 body.
// EXCEPTION 1 applied (see this file's header comment): as of Plan 57-04
// Task 2, GET /v1/formats gained the additive "operations" key, so the full
// response body is no longer byte-equal to the golden file -- that is
// expected, not a regression. This test now compares the golden bytes
// against ONLY the "engines" sub-map, re-encoded with the exact same
// encoder settings writeJSON uses (see the encoder call below) -- so the
// comparison mechanism stays bytes.Equal against the untouched golden
// file; only the INPUT to that comparison changed, not the mechanism
// itself.
func TestPre57GoldenListFormats(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	enginesOnly, ok := body["engines"]
	if !ok {
		t.Fatalf(`response missing "engines" key; body=%s`, rec.Body.String())
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]json.RawMessage{"engines": enginesOnly}); err != nil {
		t.Fatalf("re-encode engines sub-map: %v", err)
	}

	assertGolden(t, "formats.golden.json", buf.Bytes())
}

// TestPre57GoldenGetJobSingleOutput pins GET /v1/jobs/{id}'s exact
// pre-Phase-57 body for a done job with exactly one output. The job id,
// output fields and presigned URL are all fixed literals so the byte
// comparison is reproducible across runs. See the exceptions list in this
// file's header comment for the two permitted future edits to this test
// (57-06, Tasks 1 and 2) and the standing prohibition on ever touching the
// golden file's bytes.
func TestPre57GoldenGetJobSingleOutput(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000057")

	repo := &fakeRepo{}
	// The presigned URL's "&" is load-bearing: it pins writeJSON's
	// SetEscapeHTML(false) -- a naive encoder would render it as "&"
	// and this golden file would silently stop matching real presigned URLs.
	store := &fakeStorage{presigned: "https://s3.example/results/pinned/0-out.webp?sig=abc&x=1"}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})

	repo.getJob = &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone}
	// Exception 2 (this file's header comment) applied by 57-06: Role:
	// "primary" is set here so handleGetJob's role-driven selection still
	// finds a primary output for this single-output fixture. "role" is not
	// part of a single-output done job's response body, so the golden
	// file's bytes are unaffected by this literal change.
	repo.outputs = []jobs.Output{{
		Ordinal:     0,
		ObjectKey:   "results/pinned/0-out.webp",
		Filename:    "out.webp",
		Format:      "webp",
		SizeBytes:   4242,
		ContentType: "image/webp",
		Role:        "primary",
	}}

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	assertGolden(t, "job_done_single_output.golden.json", rec.Body.Bytes())
}
