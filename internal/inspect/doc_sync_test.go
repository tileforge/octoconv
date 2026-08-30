// This test is what makes docs/FINDINGS.md a CONTRACT rather than plain
// documentation: it mechanically proves docs/FINDINGS.md and
// inspect.AllCodes() (the Go source of truth) never drift apart. If this
// test fails, the correct response is to update whichever side (the Go
// constants or the document) is wrong -- never to relax this test. Mirrors
// internal/errcode/doc_sync_test.go's TestErrorCodesDocMatchesGoConstants
// exactly (58-02-PLAN.md Task 1).
//
// This file lives in the external test package inspect_test, mirroring
// errcode_test's own placement, so the coupling between this test and the
// document's prose is easy to find alongside any future sibling sync test
// in this package (bindings_test.go).
package inspect_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/inspect"
)

// docPath is repo-relative from this package's directory
// (internal/inspect/), so it resolves to <repo-root>/docs/FINDINGS.md
// regardless of the working directory `go test` is invoked from.
const docPath = "../../docs/FINDINGS.md"

// unknownCodeFallbackPhrase is the exact stable phrase docs/FINDINGS.md
// must contain to satisfy the consumer contract mirrored from
// docs/ERROR-CODES.md's Pitfall 12. Keeping this as a named constant makes
// the coupling between this test and the document's prose visible -- if
// the document's exact wording changes, this constant is the one place
// that needs to change with it.
const unknownCodeFallbackPhrase = "MUST treat an unrecognized finding code as a generic fallback"

// findingsTableHeader delimits the single findings table inside
// docs/FINDINGS.md. Matched as a line PREFIX so a trailing annotation on
// the actual header doesn't need to be reproduced verbatim here.
const findingsTableHeader = "## Findings vocabulary"

// codeRowPattern matches a markdown table row whose first (code) column is
// a single backtick-delimited machine-safe token, e.g.
// "| `ooxml_macro` | warning | ... |". This intentionally extracts ONLY the
// first column of a table row -- not every backtick span in the file --
// because the document's prose legitimately uses backticks around
// non-code identifiers (`sanitize`, `opts`, `POST /v1/inspect`, etc.) and
// elsewhere in a row's own detail columns. Anchoring the pattern to the
// start of the line and requiring it to be the row's FIRST cell avoids
// misreporting those as extra/unknown codes.
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

// readDoc reads docs/FINDINGS.md, failing the test with an explicit message
// naming the path if it is missing or unreadable.
func readDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v -- docs/FINDINGS.md must exist at the repo root (58-02-PLAN.md Task 1)", docPath, err)
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

// TestFindingCodesDocMatchesGoConstants is the bidirectional proof this
// file exists for: every code inspect.AllCodes() declares must appear in
// the document's findings table, and the table may not contain a code that
// inspect.AllCodes() does not declare.
func TestFindingCodesDocMatchesGoConstants(t *testing.T) {
	doc := readDoc(t)

	docCodes := extractTableCodes(doc, findingsTableHeader)
	if len(docCodes) == 0 {
		t.Fatal("extracted zero codes from the findings table -- section header or table format in docs/FINDINGS.md changed in a way this parser no longer understands")
	}

	var goCodes []string
	for _, c := range inspect.AllCodes() {
		goCodes = append(goCodes, string(c))
	}

	if missing, extra := diffSets(goCodes, docCodes); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("findings table drift between inspect.AllCodes() and docs/FINDINGS.md -- in Go but not in the document: %v; in the document but not declared by inspect.AllCodes(): %v", missing, extra)
	}
}

// reasonTableHeader delimits the reason table inside docs/FINDINGS.md.
// Matched as a line PREFIX, like findingsTableHeader.
const reasonTableHeader = "## Report coverage is a separate claim"

// selectiveReasonsPhrase is the exact, stable sentence docs/FINDINGS.md must
// carry to state which reasons may set coverage.complete:false (Phase 59,
// D-11). Keeping it as a named constant here makes the coupling between this
// test and the document's PROSE visible: if the wording changes, this
// constant is the one place that has to change with it.
//
// A table-parity test alone would not catch this. The table says which
// reasons exist and marks a Yes/No column; the sentence is what a client
// actually reads to learn the rule, and it is exactly the kind of prose that
// silently went stale before -- the previous version of this section said
// "Only the last two reasons" and stayed there while the code moved. Same
// failure mode as the reason table itself before TestReasonsDocMatchesGoConstants
// existed: unenforced text drifts.
const selectiveReasonsPhrase = "Exactly three reasons may set `coverage.complete: false`: `work_ceiling_bytes`,\n`work_ceiling_time`, and `read_failed`."

// TestDocDeclaresSelectiveReasons pins that the document states the rule, in
// so many words, rather than leaving a reader to infer it from a column of
// Yes/No cells.
func TestDocDeclaresSelectiveReasons(t *testing.T) {
	doc := readDoc(t)
	if !strings.Contains(doc, selectiveReasonsPhrase) {
		t.Fatalf("docs/FINDINGS.md does not contain the required phrase %q -- the set of reasons that make a report "+
			"selective must be stated explicitly in prose, not only implied by the table's Yes/No column", selectiveReasonsPhrase)
	}
}

// TestDocMarksExactlyTheSelectiveReasons is the table half of the same claim:
// every reason row must carry a Yes/No cell, and the Yes rows must be exactly
// the three that makesReportIncomplete admits.
//
// The two tests are complementary and neither is redundant: the prose could be
// updated while the table rots, or the reverse. This one reads the table; the
// one above reads the sentence.
func TestDocMarksExactlyTheSelectiveReasons(t *testing.T) {
	doc := readDoc(t)

	wantSelective := map[string]bool{
		"work_ceiling_bytes": true,
		"work_ceiling_time":  true,
		"read_failed":        true,
	}

	inSection := false
	seen := map[string]bool{}
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, reasonTableHeader) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		if !inSection {
			continue
		}
		m := codeRowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		reason := m[1]
		seen[reason] = true

		// The final cell of the row is the "Makes the report selective?"
		// column. A Yes is emboldened in this document; the test reads the
		// cell rather than the emphasis so a formatting change does not
		// silently invert the meaning.
		cells := strings.Split(strings.Trim(line, "|"), "|")
		last := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(cells[len(cells)-1], "*", "")))
		gotSelective := last == "yes"
		if last != "yes" && last != "no" {
			t.Fatalf("reason row %q: last cell = %q, want exactly Yes or No -- an ambiguous cell makes this table "+
				"unenforceable", reason, last)
		}
		if gotSelective != wantSelective[reason] {
			t.Errorf("docs/FINDINGS.md marks %q as selective=%v, want %v", reason, gotSelective, wantSelective[reason])
		}
	}

	for reason := range wantSelective {
		if !seen[reason] {
			t.Errorf("docs/FINDINGS.md reason table has no row for %q", reason)
		}
	}
}

// TestReasonsDocMatchesGoConstants does for the reason vocabulary exactly
// what TestFindingCodesDocMatchesGoConstants does for the code vocabulary.
//
// It exists because its absence had already cost something. The codes table
// was mechanically enforced from the start; the reason table was not, and it
// silently fell out of date the moment the codex PR #9 fixes added
// format_unrecognized, pdf_name_escape and pdf_stream_body_token to the Go
// constants. Nothing failed, because nothing was checking. That is the same
// failure mode that let docs/openapi.yaml drift from internal/errcode until
// openapi_sync_test.go was added -- an unenforced table is documentation, and
// documentation drifts.
//
// If this test fails, fix whichever side is wrong. Never relax the test.
func TestReasonsDocMatchesGoConstants(t *testing.T) {
	doc := readDoc(t)

	docReasons := extractTableCodes(doc, reasonTableHeader)
	if len(docReasons) == 0 {
		t.Fatal("extracted zero reasons from the reason table -- the section header or table format in docs/FINDINGS.md changed in a way this parser no longer understands")
	}

	var goReasons []string
	for _, r := range inspect.AllReasons() {
		goReasons = append(goReasons, string(r))
	}

	if missing, extra := diffSets(goReasons, docReasons); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("reason table drift between inspect.AllReasons() and docs/FINDINGS.md -- in Go but not in the document: %v; in the document but not declared by inspect.AllReasons(): %v", missing, extra)
	}
}

// TestAllReasonsCoversEveryDeclaredConstant guards the one way
// TestReasonsDocMatchesGoConstants can be defeated without anybody noticing:
// declaring a new Reason constant in status.go and forgetting to add it to
// AllReasons(). Both sides of the parity check would then agree with each
// other while both omitting the new reason, and the document would once again
// be silently incomplete.
//
// Parsing the source is deliberate. Reflection cannot enumerate untyped
// string constants, so the only way to see a constant that AllReasons()
// forgot is to read the declaration itself.
func TestAllReasonsCoversEveryDeclaredConstant(t *testing.T) {
	const statusPath = "status.go"

	src, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("read %s: %v", statusPath, err)
	}

	declPattern := regexp.MustCompile(`(?m)^\s*Reason[A-Za-z0-9]*\s*=\s*"([a-z][a-z0-9_]*)"`)
	var declared []string
	for _, m := range declPattern.FindAllStringSubmatch(string(src), -1) {
		declared = append(declared, m[1])
	}
	if len(declared) == 0 {
		t.Fatalf("found zero Reason constant declarations in %s -- the declaration shape changed in a way this parser no longer understands", statusPath)
	}

	var listed []string
	for _, r := range inspect.AllReasons() {
		listed = append(listed, string(r))
	}

	if missing, extra := diffSets(declared, listed); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("AllReasons() does not match the Reason constants declared in %s -- declared but not returned by AllReasons(): %v; returned but not declared: %v", statusPath, missing, extra)
	}
}

// TestDocDeclaresUnknownCodeFallback makes the unknown-code fallback
// requirement mechanical rather than aspirational: the document must
// state, as a requirement rather than a suggestion, that consumers treat
// an unrecognized finding code as a generic fallback and never as a hard
// error.
func TestDocDeclaresUnknownCodeFallback(t *testing.T) {
	doc := readDoc(t)
	if !strings.Contains(doc, unknownCodeFallbackPhrase) {
		t.Fatalf("docs/FINDINGS.md does not contain the required phrase %q -- the unknown-code consumer contract must be stated explicitly", unknownCodeFallbackPhrase)
	}
}
