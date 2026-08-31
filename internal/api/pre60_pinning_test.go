package api

// This suite pins TODAY's (pre-Phase-60) exact byte body of POST /v1/jobs for
// four values of the `operation` form field, per 60-CONTEXT.md and the D-15
// protocol inherited from 59-CONTEXT.md (itself a reproduction of 58's D-15,
// 57's D-17 and 56's D-10 step 1).
//
// What is being pinned, and why it is worth pinning:
//
// `sanitize` is rejected today with 422 unsupported_operation. That rejection
// is DELIBERATE and recorded in the schema itself -- 0015_operation_and_output_roles.sql
// calls it "Intentional divergence #1": the DB CHECK constraint accepts
// operation='sanitize', while handleCreateJob's Gate 1 (handlers.go,
// convert.Default.IsKnownOperation) rejects it because no engine registers a
// triple under that operation. Phase 60 removes the divergence by registering
// the engine, at which point the 422 disappears.
//
// Pinning the rejection first is what makes "the 422 was lifted for sanitize
// and for nothing else" a checkable statement rather than an assertion by the
// person who lifted it. The three neighbouring values exist precisely so that
// "nothing else" has a subject: without them, a change that accidentally
// widened Gate 1 to accept every unknown operation would produce exactly the
// same diff for the sanitize case alone.
//
// Разрешённые исключения (closed list of permitted exceptions) -- non-empty,
// and exactly one entry long:
//
//  1. The disappearance of the "sanitize" case from this snapshot, because
//     that operation stops being rejected AT GATE 1 (Plan 60-06 Task 4).
//     Realized as measured rather than as predicted: the request this suite
//     sends is png -> webp, which is not a sanitize pair, so sanitize is
//     still refused 422 -- but by Gate 2, with unsupported_conversion. The
//     suite now asserts exactly that, which is a stronger statement than the
//     case simply vanishing: a 202 here would mean the pair gate was lifted
//     too, and nothing asked for that.
//
// Everything else is frozen. The other three cases must stay byte-identical:
// same status, same error code, same message text. A plan that needs a wider
// exception is changing the contract, not the test: stop and escalate rather
// than editing this suite or regenerating its golden bytes.
//
// The mechanical immutability check, and its expected value, is:
//
//	git rev-list --count HEAD -- internal/api/testdata/pre60_sanitize_rejection.golden.json   # must print 1
//
// This is `git rev-list --count`, not a ref-less `git diff`: `git diff
// --name-only` with no ref compares the working tree against the index and
// prints nothing the instant a change is committed -- i.e. it goes blind in
// EXACTLY the scenario D-15 exists to catch (golden bytes and SUT edited
// together in one commit). A commit count is not blind to that.
//
// Determinism note. These bodies are error responses built by writeError,
// which discards the underlying error and emits a fixed code plus a message
// composed only from the client's own input. There is no timestamp, no job id
// and no quota verdict anywhere in them, so the snapshot is deterministic
// without any normalization: what is pinned is the whole body exactly as a
// client would receive it.

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/apaderin/octoconv/internal/errcode"
	"testing"
)

// updateGolden60 regenerates testdata/pre60_*.golden.json from the CURRENT
// handler output. It exists only to create the fixture on the very first run
// against pre-Phase-60 code (60-01-PLAN.md Task 3), and later to apply the one
// permitted exception above -- never to make a failing pin pass.
//
// The name must differ from -update-golden, -update-golden-58 and
// -update-golden-59: two flags registered under one name in the same package
// panic with "flag redefined" at test-binary startup, which would take down
// every test in internal/api, not merely this file. All four coexist; the
// verification step of this plan runs the 58 and 59 suites to prove it.
var updateGolden60 = flag.Bool("update-golden-60", false, "rewrite testdata/pre60_sanitize_rejection.golden.json from the current handler output")

// pre60Operations is the closed set of `operation` values this snapshot
// covers.
//
//   - sanitize -- the subject: legal in the DB schema, rejected by the API,
//     and the one case Phase 60 is allowed to change.
//   - inspect -- also named in the 0015 CHECK constraint and also rejected
//     today, but NOT lifted by this phase. It is the sharpest control: if a
//     change widens Gate 1 rather than registering one engine, this case moves
//     too, and the diff says so.
//   - extract -- a second schema-legal, engine-less operation, for the same
//     reason.
//   - bogus -- never legal anywhere. Distinguishes "Gate 1 stopped rejecting
//     schema-legal values" from "Gate 1 stopped rejecting anything at all".
var pre60Operations = []string{"sanitize", "inspect", "extract", "bogus"}

// assertGolden60 compares got against testdata/pre60_<name>, or (with
// -update-golden-60) rewrites that fixture from got. The expectation is read
// from a file on disk, never from an inline literal in this source: that is
// the entire point of D-15 -- the expectation must live somewhere the person
// changing the SUT cannot silently edit in the same breath.
func assertGolden60(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "pre60_"+name)
	if *updateGolden60 {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s (-update-golden-60)", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s: response bytes changed -- want byte-identical to %s (D-15 pinning violation).\n"+
			"Exactly one change is permitted in Phase 60, listed in this file's header: the \"sanitize\"\n"+
			"case disappearing because that operation is no longer rejected. If any OTHER case moved,\n"+
			"Gate 1 was widened rather than one engine registered -- that is the contract moving, not\n"+
			"the test.\ngot:  %s\nwant: %s", name, path, got, want)
	}
}

// TestPre60GoldenSanitizeRejection pins POST /v1/jobs's exact pre-Phase-60
// rejection body for every operation in pre60Operations. One golden file holds
// all four cases, keyed by operation, so the snapshot is a single artifact
// whose diff shows every affected case at once rather than four files that
// could drift apart.
//
// The upload is a valid PNG with target=webp -- a triple that IS registered
// under operation=convert. That matters: it means each case fails at Gate 1
// (unknown operation) and not at Gate 2 (unregistered pair), so what is pinned
// is the operation check itself rather than an incidental format mismatch.
func TestPre60GoldenSanitizeRejection(t *testing.T) {
	type pinnedResponse struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}

	snapshot := make(map[string]pinnedResponse, len(pre60Operations))
	for _, operation := range pre60Operations {
		srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

		body, ct := multipartBodyWithOperation(t, "in.png", "webp", operation, pngBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if operation == sanitizeOperation {
			// The permitted exception, exercised rather than assumed. This
			// request is png -> webp, which is not a sanitize pair, so it is
			// still refused -- but it must now be refused by Gate 2 with
			// unsupported_conversion. Seeing unsupported_operation here would
			// mean the 422 was never lifted; seeing a 202 would mean the pair
			// gate was lifted too, which nothing asked for.
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("operation=sanitize: status = %d, want 422 -- png -> webp is not a "+
					"sanitize pair and must still be refused", rec.Code)
			}
			assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
			continue
		}

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("operation=%s: status = %d, want 422 -- this suite pins a REJECTION, and a "+
				"non-422 here means the rejection is already gone before the phase that lifts it; "+
				"body=%s", operation, rec.Code, rec.Body.String())
		}
		snapshot[operation] = pinnedResponse{Status: rec.Code, Body: json.RawMessage(rec.Body.Bytes())}
	}

	// The golden still holds all four cases and its bytes are untouched --
	// `git rev-list --count` for it is still 1, and it remains the record of
	// what this endpoint did before Phase 60. What changed is the assertion:
	// the three frozen cases are compared as the exact bytes they were pinned
	// as, and the fourth is required to be exactly "sanitize" and nothing
	// else. Regenerating the file instead would have deleted the evidence
	// that the 422 ever existed.
	want, err := os.ReadFile(filepath.Join("testdata", "pre60_sanitize_rejection.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden map[string]pinnedResponse
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}

	if _, ok := golden[sanitizeOperation]; !ok {
		t.Fatalf("the golden has no %q case -- this suite exists to prove that case was lifted, "+
			"and it cannot prove the lifting of something that was never pinned", sanitizeOperation)
	}
	if len(golden) != len(snapshot)+1 {
		t.Fatalf("golden has %d cases and the live snapshot has %d; exactly one case (%q) may "+
			"disappear", len(golden), len(snapshot), sanitizeOperation)
	}

	for operation, goldenEntry := range golden {
		if operation == sanitizeOperation {
			continue
		}
		liveEntry, ok := snapshot[operation]
		if !ok {
			t.Errorf("case %q disappeared, and it is not the permitted exception -- Gate 1 was "+
				"widened rather than one engine registered", operation)
			continue
		}
		if liveEntry.Status != goldenEntry.Status {
			t.Errorf("%s: status = %d, want %d", operation, liveEntry.Status, goldenEntry.Status)
		}
		// Compared after json.Compact on BOTH sides. The golden was written
		// by an encoder with SetIndent, which re-indented these nested
		// bodies, while the live ones come straight off the wire compact.
		// Stripping insignificant whitespace from both is not a relaxation:
		// the indentation was added by the snapshot writer, never by the
		// handler, so it was never part of what the handler produced.
		if !bytes.Equal(compactJSON(t, goldenEntry.Body), compactJSON(t, liveEntry.Body)) {
			t.Errorf("%s: body changed (D-15 pinning violation)\ngot:  %s\nwant: %s",
				operation, liveEntry.Body, goldenEntry.Body)
		}
	}
}

// compactJSON strips insignificant whitespace so a snapshot written with
// indentation can be compared to a response that was never indented.
func compactJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact json: %v", err)
	}
	return buf.Bytes()
}

// sanitizeOperation is the single permitted exception's subject, named once
// so the checks above read as "the exception" rather than as a repeated
// string literal that could be edited in one place and not another.
const sanitizeOperation = "sanitize"
