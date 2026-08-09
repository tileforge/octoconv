// This test is what makes docs/openapi.yaml's Error.code enum a CONTRACT
// rather than plain documentation: it mechanically proves the enum never
// drifts from internal/errcode.All(), the Go source of truth (D-06,
// 56-CONTEXT.md). If this test fails, the correct response is to fix
// whichever side is wrong -- the Go constants or the spec -- never to
// relax this test.
//
// This file lives in the external test package errcode_test, mirroring
// doc_sync_test.go's own package placement, even though this particular
// test only needs internal/errcode -- keeping both sync-proof tests in the
// same package makes their shared idioms (docPath-style repo-relative
// const, line-anchored regexp scan, diffSets) easy to find together.
package errcode_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/errcode"
)

// openapiPath is repo-relative from this package's directory
// (internal/errcode/), so it resolves to <repo-root>/docs/openapi.yaml
// regardless of the working directory `go test` is invoked from.
const openapiPath = "../../docs/openapi.yaml"

// errorSchemaHeader / enumHeader / enumItemPattern locate the Error
// schema's code property enum block by line-anchored structure rather than
// a whole-file token scan (docs/openapi.yaml's prose elsewhere legitimately
// mentions error codes by name, e.g. in response descriptions). The Error
// schema is declared at 4-space indent under `schemas:`; its `code:`
// property's `enum:` block lists one `- value` per line at 12-space
// indent, one level deeper than the two-line "enum:" header itself
// (required by Task 3's action: a flow-style [a, b, ...] list would not
// match this pattern, deliberately -- the plan requires a multi-line
// block sequence).
const (
	errorSchemaHeader = "    Error:"
	enumHeader        = "          enum:"
)

var enumItemPattern = regexp.MustCompile(`^            - ([a-z][a-z0-9_]*)$`)

// readOpenAPI reads docs/openapi.yaml, failing the test with an explicit
// message naming the path if it is missing or unreadable.
func readOpenAPI(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("read %s: %v -- docs/openapi.yaml must exist at the repo root", openapiPath, err)
	}
	return string(b)
}

// extractErrorCodeEnum returns, in order of appearance, every enum value
// listed under the Error schema's code property in doc. It scans from the
// line carrying errorSchemaHeader to the next schema declaration at the
// same 4-space indent (or EOF), and within that span from enumHeader to
// the first line that is no longer a "- value" item at the expected
// indent.
func extractErrorCodeEnum(doc string) []string {
	var codes []string
	inErrorSchema := false
	inEnum := false
	for _, line := range strings.Split(doc, "\n") {
		if line == errorSchemaHeader {
			inErrorSchema = true
			continue
		}
		if inErrorSchema && !inEnum {
			// Leaving the Error schema: the next schema key at the same
			// 4-space indent starts (e.g. "    QuotaError:", "    JobCreated:").
			if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     ") && line != errorSchemaHeader {
				return codes
			}
			if line == enumHeader {
				inEnum = true
			}
			continue
		}
		if inEnum {
			if m := enumItemPattern.FindStringSubmatch(line); m != nil {
				codes = append(codes, m[1])
				continue
			}
			// First non-item line after the enum block ends the scan --
			// the Error schema's code enum is a single contiguous block.
			return codes
		}
	}
	return codes
}

// diffSets is declared once in doc_sync_test.go and reused here -- both
// sync-proof tests live in the same errcode_test package specifically so
// this helper (and readDoc's sibling readOpenAPI above) do not need a
// second copy.

// TestOpenAPIErrorCodeEnumMatchesGoConstants is the bidirectional proof
// this file exists for: every code errcode.All() declares must appear in
// docs/openapi.yaml's Error.code enum, and the enum may not contain a code
// errcode.All() does not declare.
func TestOpenAPIErrorCodeEnumMatchesGoConstants(t *testing.T) {
	doc := readOpenAPI(t)

	specCodes := extractErrorCodeEnum(doc)
	if len(specCodes) == 0 {
		t.Fatal("extracted zero codes from docs/openapi.yaml's Error.code enum -- the schema structure or enum formatting changed in a way this parser no longer understands (see errorSchemaHeader/enumHeader/enumItemPattern)")
	}

	goCodes := errcode.All()

	if missing, extra := diffSets(goCodes, specCodes); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("drift between errcode.All() and docs/openapi.yaml's Error.code enum -- missing from the spec: %v; present in the spec but not declared by errcode.All(): %v. Fix whichever side is wrong (Go constants are the source of truth, D-06) -- never relax this test.", missing, extra)
	}
}
