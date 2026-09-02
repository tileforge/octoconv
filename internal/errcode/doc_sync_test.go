// This test is what makes docs/ERROR-CODES.md a CONTRACT rather than plain
// documentation: it mechanically proves docs/ERROR-CODES.md and the two
// Go-declared error-code vocabularies (errcode.All(), the sync half, and
// worker.AllFailureCodes(), the async half) never drift apart. If this
// test fails, the correct response is to update whichever side (the Go
// constants or the document) is wrong -- never to relax this test. D-06
// (56-CONTEXT.md): Go constants are the source of truth; the document is
// generated/maintained from them.
//
// This file lives in the external test package errcode_test specifically
// so it can import both internal/errcode and internal/worker without
// creating a production dependency between them (worker never imports
// errcode -- verified by `go list -deps ./internal/worker | grep
// octoconv/internal/errcode` returning zero matches before this file was
// written).
package errcode_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/worker"
)

// docPath is repo-relative from this package's directory
// (internal/errcode/), so it resolves to <repo-root>/docs/ERROR-CODES.md
// regardless of the working directory `go test` is invoked from.
const docPath = "../../docs/ERROR-CODES.md"

// unknownCodeFallbackPhrase is the exact stable phrase docs/ERROR-CODES.md
// must contain to satisfy the Pitfall-12 unknown-code consumer contract.
// Keeping this as a named constant makes the coupling between this test
// and the document's prose visible -- if the document's exact wording
// changes, this constant is the one place that needs to change with it.
const unknownCodeFallbackPhrase = "MUST treat an unrecognized code as a generic fallback"

// syncSectionHeader / asyncSectionHeader delimit the two tables inside
// docs/ERROR-CODES.md. Matched as a line PREFIX so trailing parenthetical
// annotations in the actual headers ("(HTTP 4xx / 429)", "(`error_code`)")
// don't need to be reproduced verbatim here.
const (
	syncSectionHeader  = "## Synchronous error codes"
	asyncSectionHeader = "## Asynchronous job-failure codes"
	// historicalSectionHeader delimits the codes this build can no longer
	// produce but which clients may still receive (Phase 59).
	//
	// extractTableCodes stops at the next "## " line, so this section is
	// invisible to the async check above BY CONSTRUCTION rather than by
	// careful placement -- a property worth stating, because the alternative
	// (a shared table with a "retired" column) would have required teaching
	// that parser about a column it has no notion of.
	historicalSectionHeader = "## Historical job-failure codes"
)

// codeRowPattern matches a markdown table row whose first (code) column is
// a single backtick-delimited machine-safe token, e.g.
// "| `invalid_request` | 400 | ... |". This intentionally extracts ONLY
// the first column of a table row -- not every backtick span in the file
// -- because the document's prose legitimately uses backticks around
// non-code identifiers (`opts`, `message`, `callback_url`, `GET
// /v1/formats`, etc.) and elsewhere in a row's own detail columns (e.g.
// the async invalid_options row's detail text mentions the sync
// `invalid_options` code by name). A whole-file or whole-row backtick scan
// would misreport those as extra/unknown codes; anchoring the pattern to
// the start of the line and requiring it to be the row's FIRST cell avoids
// that entirely.
var codeRowPattern = regexp.MustCompile("^\\|\\s*`([a-z][a-z0-9_]*)`")

// extractTableCodes returns, in order of appearance, every code-column
// token from the markdown table located between the line carrying
// startHeader as a prefix and the next "## " section header (or EOF).
func extractTableCodes(doc, startHeader string) []string {
	var codes []string
	inSection := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, startHeader) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		if !inSection {
			continue
		}
		if m := codeRowPattern.FindStringSubmatch(line); m != nil {
			codes = append(codes, m[1])
		}
	}
	return codes
}

// readDoc reads docs/ERROR-CODES.md, failing the test with an explicit
// message naming the path if it is missing or unreadable.
func readDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v -- docs/ERROR-CODES.md must exist at the repo root (56-06-PLAN.md Task 1)", docPath, err)
	}
	return string(b)
}

// diffSets returns the elements of want not present in got ("missing")
// and the elements of got not present in want ("extra"), each named
// individually so a test failure can point at the exact offending code.
func diffSets(want, got []string) (missing, extra []string) {
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
	}
	gotSet := make(map[string]bool, len(got))
	for _, g := range got {
		gotSet[g] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !wantSet[g] {
			extra = append(extra, g)
		}
	}
	return missing, extra
}

// TestErrorCodesDocMatchesGoConstants is the bidirectional proof this file
// exists for: every code errcode.All() declares must appear in the
// document's synchronous table, every code worker.AllFailureCodes()
// declares must appear in the document's asynchronous table, and neither
// table may contain a code that the corresponding Go source does not
// declare. invalid_options appearing in BOTH tables is expected and
// correct (D-03, one code space) and does not trip either direction here,
// since each table is checked independently against its own Go-declared
// vocabulary, both of which independently declare an invalid_options
// constant (errcode.InvalidOptions and worker.CodeInvalidOptions --
// TestSyncAndAsyncShareInvalidOptions below pins that these two are the
// same string).
func TestErrorCodesDocMatchesGoConstants(t *testing.T) {
	doc := readDoc(t)

	docSync := extractTableCodes(doc, syncSectionHeader)
	docAsync := extractTableCodes(doc, asyncSectionHeader)

	if len(docSync) == 0 {
		t.Fatal("extracted zero codes from the synchronous table -- section header or table format in docs/ERROR-CODES.md changed in a way this parser no longer understands")
	}
	if len(docAsync) == 0 {
		t.Fatal("extracted zero codes from the asynchronous table -- section header or table format in docs/ERROR-CODES.md changed in a way this parser no longer understands")
	}

	goSync := errcode.All()
	goAsync := worker.AllFailureCodes()

	if missing, extra := diffSets(goSync, docSync); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("sync table drift between errcode.All() and docs/ERROR-CODES.md -- missing from the document: %v; present in the document but not declared by errcode.All(): %v", missing, extra)
	}
	if missing, extra := diffSets(goAsync, docAsync); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("async table drift between worker.AllFailureCodes() and docs/ERROR-CODES.md -- missing from the document: %v; present in the document but not declared by worker.AllFailureCodes(): %v", missing, extra)
	}
}

// TestHistoricalCodesDocMatchesGoConstants extends this file's contract to the
// retired vocabulary.
//
// Without it the historical section would be prose: a place to write codes down
// with nothing checking that the list is right, which is exactly the state the
// two active tables were in before Phase 56 added the test above. An unenforced
// table drifts -- that is not a prediction, it is what happened here.
func TestHistoricalCodesDocMatchesGoConstants(t *testing.T) {
	doc := readDoc(t)

	docHistorical := extractTableCodes(doc, historicalSectionHeader)
	if len(docHistorical) == 0 {
		t.Fatal("extracted zero codes from the historical table -- the section header or table format in " +
			"docs/ERROR-CODES.md changed in a way this parser no longer understands, or the section was deleted; " +
			"a retired code removed from the document becomes a value the API returns and the contract denies")
	}

	if missing, extra := diffSets(worker.HistoricalFailureCodes(), docHistorical); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("historical table drift between worker.HistoricalFailureCodes() and docs/ERROR-CODES.md -- missing "+
			"from the document: %v; present in the document but not declared in Go: %v", missing, extra)
	}
}

// TestActiveAndHistoricalFailureCodesAreDisjoint is the invariant that makes
// retirement a MOVE rather than a copy.
//
// A code in both lists would have the document promising two incompatible
// things at once: that a job can still fail this way, and that nothing can
// produce it. It would also let a careless retirement half-happen -- added to
// the historical list, never removed from the active one -- and pass every
// other check in this file.
func TestActiveAndHistoricalFailureCodesAreDisjoint(t *testing.T) {
	active := make(map[string]bool, len(worker.AllFailureCodes()))
	for _, c := range worker.AllFailureCodes() {
		active[c] = true
	}
	for _, c := range worker.HistoricalFailureCodes() {
		if active[c] {
			t.Errorf("%q is in BOTH worker.AllFailureCodes() and worker.HistoricalFailureCodes() -- retiring a code "+
				"is a move between the two lists, never a copy into the second", c)
		}
	}
}

// TestBornDigitalIsRetiredNotDeleted pins Phase 59's specific decision, so it
// can fail on its own rather than only as part of the general machinery above.
//
// The decision was not obvious and is worth a test of its own: the code became
// unproducible when the OCR guard stopped refusing born-digital PDFs, but
// jobs.error_code is persisted and GET /v1/jobs/{id} still serves it, so
// deleting it from the contract would have made the contract wrong about jobs
// that already exist.
func TestBornDigitalIsRetiredNotDeleted(t *testing.T) {
	const code = "born_digital"

	for _, c := range worker.AllFailureCodes() {
		if c == code {
			t.Fatalf("%q is still in the active vocabulary -- nothing can produce it since Phase 59 (see "+
				"convert.TestBornDigitalHasNoRefusalPath)", code)
		}
	}

	found := false
	for _, c := range worker.HistoricalFailureCodes() {
		if c == code {
			found = true
		}
	}
	if !found {
		t.Fatalf("%q is in neither vocabulary -- it must stay documented as historical, because clients reading a job "+
			"that failed before Phase 59 still receive it", code)
	}
}

// TestSyncAndAsyncShareInvalidOptions pins D-03: the sync and async
// invalid_options codes are required to be the identical string forever.
// A future rename of either side would silently split what is meant to be
// one code space into two, and neither package's own tests would notice
// on their own -- each package only knows about its own constant.
func TestSyncAndAsyncShareInvalidOptions(t *testing.T) {
	if errcode.InvalidOptions != worker.CodeInvalidOptions {
		t.Fatalf("errcode.InvalidOptions = %q, worker.CodeInvalidOptions = %q -- D-03 requires these to be identical (one code space, not two)", errcode.InvalidOptions, worker.CodeInvalidOptions)
	}
}

// TestDocDeclaresUnknownCodeFallback makes Pitfall 12's unknown-code
// fallback requirement mechanical rather than aspirational: the document
// must state, as a requirement rather than a suggestion, that consumers
// treat an unrecognized code as a generic fallback and never as a hard
// error.
func TestDocDeclaresUnknownCodeFallback(t *testing.T) {
	doc := readDoc(t)
	if !strings.Contains(doc, unknownCodeFallbackPhrase) {
		t.Fatalf("docs/ERROR-CODES.md does not contain the required phrase %q -- the Pitfall-12 unknown-code consumer contract must be stated explicitly", unknownCodeFallbackPhrase)
	}
}

// TestSyncAndAsyncShareTooManyPages pins the SECOND shared code (Phase 63,
// 63-CONTEXT.md D-04), for the same reason TestSyncAndAsyncShareInvalidOptions
// pins the first: two spellings for one client-facing condition is a
// vocabulary defect, and neither package's own tests can notice a rename on
// the other side.
//
// It is a separate test rather than a second assertion inside the one above so
// a failure names WHICH code drifted. The two codes were shared for the same
// reason but at different times and by different phases; they can break apart
// independently.
func TestSyncAndAsyncShareTooManyPages(t *testing.T) {
	if errcode.TooManyPages != worker.CodeTooManyPages {
		t.Fatalf("errcode.TooManyPages = %q, worker.CodeTooManyPages = %q -- D-03 requires these to be identical (one code space, not two)", errcode.TooManyPages, worker.CodeTooManyPages)
	}
}
