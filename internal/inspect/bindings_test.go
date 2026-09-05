package inspect_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"sort"

	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/inspect"
)

// handlersPath is repo-relative from this package's directory
// (internal/inspect/), so it resolves to
// <repo-root>/internal/api/handlers.go regardless of the working directory
// `go test` is invoked from -- same technique as
// internal/errcode/openapi_sync_test.go's repo-relative constant.
const handlersPath = "../api/handlers.go"

// contentValidationPrefix anchors the scan to exactly the log lines D-04
// concerns itself with. handleCreateJob ALSO logs "quota rejected: ...
// reason=%s" lines (a format string, not a literal token, and a different
// class of rejection entirely -- daily/concurrent job budget, not file
// content) -- those must never be swept into this set.
const contentValidationPrefix = "content validation rejected"

// reasonTokenPattern extracts the literal value following `reason=` on a
// log line already known to contain contentValidationPrefix. It stops at
// the next non-token character, matching handleCreateJob's
// log.Printf field-per-space-separated-token convention.
var reasonTokenPattern = regexp.MustCompile(`reason=([a-z_]+)`)

// extractContentValidationReasons scans src line by line and returns the
// set of every reason= token found on a line that also contains
// contentValidationPrefix.
func extractContentValidationReasons(src string) map[string]bool {
	tokens := make(map[string]bool)
	for _, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, contentValidationPrefix) {
			continue
		}
		if m := reasonTokenPattern.FindStringSubmatch(line); m != nil {
			tokens[m[1]] = true
		}
	}
	return tokens
}

// readHandlers reads internal/api/handlers.go, failing the test with an
// explicit message naming the path if it is missing or unreadable.
func readHandlers(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(handlersPath)
	if err != nil {
		t.Fatalf("read %s: %v -- internal/api/handlers.go must exist", handlersPath, err)
	}
	return string(b)
}

// TestRejectBindingsCoverEveryContentValidationReason is the mechanical
// parity proof this file exists for: every reason= token logged on a
// "content validation rejected:" line in the REAL internal/api/handlers.go
// source must have a binding in inspect.RejectBindings, and every key in
// inspect.RejectBindings must correspond to a reason token the source
// actually logs. A new content-validation rejection class landing in
// handlers.go without a binding here is exactly the drift D-04 exists to
// catch mechanically rather than by code-review memory.
func TestRejectBindingsCoverEveryContentValidationReason(t *testing.T) {
	src := readHandlers(t)
	sourceTokens := extractContentValidationReasons(src)
	if len(sourceTokens) == 0 {
		t.Fatal("extracted zero reason= tokens from \"content validation rejected:\" lines -- the log-line format in handlers.go changed in a way this scanner no longer understands")
	}

	bindingKeys := make(map[string]bool, len(inspect.RejectBindings))
	for k := range inspect.RejectBindings {
		bindingKeys[k] = true
	}

	var unbound, dead []string
	for tok := range sourceTokens {
		if !bindingKeys[tok] {
			unbound = append(unbound, tok)
		}
	}
	for k := range bindingKeys {
		if !sourceTokens[k] {
			dead = append(dead, k)
		}
	}

	if len(unbound) > 0 || len(dead) > 0 {
		t.Errorf("RejectBindings drift against internal/api/handlers.go -- reason tokens logged in handlers.go but with no binding in RejectBindings (D-04 requires every content-validation rejection class to be bound): %v; keys present in RejectBindings but never logged by handlers.go (a dead binding for a removed rejection): %v", unbound, dead)
	}
}

// TestRejectBindingsAreWellFormed proves the closed-vocabulary invariants
// of every entry: exactly one of FindingCode/ErrCode is set, every
// non-empty FindingCode is a known finding code, and every non-empty
// ErrCode is a known sync error code.
func TestRejectBindingsAreWellFormed(t *testing.T) {
	for token, b := range inspect.RejectBindings {
		hasFinding := b.FindingCode != ""
		hasErrCode := b.ErrCode != ""
		if hasFinding == hasErrCode {
			t.Errorf("RejectBindings[%q] has FindingCode=%q ErrCode=%q -- exactly one of the two must be set, never both, never neither", token, b.FindingCode, b.ErrCode)
			continue
		}
		if hasFinding && !inspect.IsKnownCode(b.FindingCode) {
			t.Errorf("RejectBindings[%q].FindingCode = %q is not a known finding code", token, b.FindingCode)
		}
		if hasErrCode && !errcode.IsKnown(b.ErrCode) {
			t.Errorf("RejectBindings[%q].ErrCode = %q is not a known sync error code", token, b.ErrCode)
		}
	}
}

// TestRejectBindingKeysMatchPinnedGoldenCases proves RejectBindings' key
// set is a SUPERSET of the pinned pre-58 golden case names (plan 58-01),
// not an equal set -- the golden file only pins the ten rejection classes
// actually reproducible over HTTP in that plan; RejectBindings additionally
// covers the three opts_* reason tokens the 58-01 golden snapshot never
// exercised. Asserting equality here would be wrong and a future editor
// "fixing" this test to require equality would silently break D-04's
// broader coverage -- this comment and the message below exist so nobody
// makes that mistake.
func TestRejectBindingKeysMatchPinnedGoldenCases(t *testing.T) {
	const goldenPath = "../api/testdata/pre58_createjob_rejections.golden.json"
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", goldenPath, err)
	}
	var golden map[string]json.RawMessage
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatalf("unmarshal %s: %v", goldenPath, err)
	}
	if len(golden) == 0 {
		t.Fatal("golden file has zero top-level keys -- nothing to compare")
	}

	var missing []string
	for k := range golden {
		if _, ok := inspect.RejectBindings[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Errorf("golden case(s) %v have no entry in RejectBindings -- RejectBindings must be a SUPERSET of the pinned golden keys (%d golden keys, %d RejectBindings keys; RejectBindings additionally covers opts_too_large/invalid_opts/opts_not_applicable which the golden snapshot does not exercise -- do not change this assertion to require set equality)", missing, len(golden), len(inspect.RejectBindings))
	}
}

// TestFindingsDocListsEveryRejectBinding closes the gap the other drift test
// left open.
//
// TestRejectBindingsCoverEveryContentValidationReason holds handlers.go equal to
// RejectBindings, and docs/FINDINGS.md claims to declare the correspondence for
// every token -- but nothing compared the PROSE with either. It said "thirteen"
// through two milestones that added six more, and passed the whole time
// (PR #20 review).
//
// A document that states a complete correspondence is making a checkable claim,
// so this checks it.
func TestFindingsDocListsEveryRejectBinding(t *testing.T) {
	raw, err := os.ReadFile("../../docs/FINDINGS.md")
	if err != nil {
		t.Fatalf("read FINDINGS.md: %v", err)
	}
	doc := string(raw)

	var missing []string
	for token := range inspect.RejectBindings {
		if !strings.Contains(doc, "| `"+token+"` |") {
			missing = append(missing, token)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("docs/FINDINGS.md's binding table omits %v.\n\nThe document declares a COMPLETE "+
			"correspondence for every reason token; a token missing from it is a rejection a reader "+
			"is told cannot happen.", missing)
	}

	// And the other direction: a row for a token that no longer exists is a
	// rejection the document promises and the code cannot produce.
	//
	// Scoped to the binding SECTION, because this file carries a second table
	// -- the findings vocabulary -- whose rows are finding codes, not reason
	// tokens. The first version of this check read both and reported every
	// finding code as a phantom rejection: an assertion aimed at the whole
	// document when it is about one section of it.
	const heading = "## Binding to existing job-creation rejections"
	start := strings.Index(doc, heading)
	if start < 0 {
		t.Fatalf("docs/FINDINGS.md no longer has the %q section this test is about", heading)
	}
	section := doc[start:]
	if end := strings.Index(section[len(heading):], "\n## "); end >= 0 {
		section = section[:len(heading)+end]
	}

	rowRe := regexp.MustCompile(`(?m)^\| ` + "`" + `([a-z_]+)` + "`" + ` \|`)
	rows := rowRe.FindAllStringSubmatch(section, -1)
	if len(rows) == 0 {
		t.Fatal("no binding rows found in that section: the parser is wrong and every assertion above " +
			"about this table would be vacuous")
	}
	for _, m := range rows {
		token := m[1]
		if _, ok := inspect.RejectBindings[token]; !ok {
			t.Errorf("docs/FINDINGS.md lists %q, which RejectBindings does not declare: a row for a "+
				"rejection that cannot happen", token)
		}
	}
}
