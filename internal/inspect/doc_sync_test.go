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
