package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goMessageBranchPattern and tsMessageBranchPattern are deliberately
// anchored to error-MESSAGE expressions only -- err.Error(), .Message,
// .ErrorMessage on the Go side; .message on the TypeScript side -- NOT a
// blanket ban on strings.Contains/HasPrefix/EqualFold or .includes/
// .startsWith, which this codebase legitimately uses elsewhere for
// filename/path handling (client.go's sanitizeBasename, sanitizeInputPath)
// and would produce constant false positives if banned outright. If a
// future maintainer is tempted to "strengthen" this into a bare
// strings.Contains ban, don't: narrow it back down to the message
// expressions below instead, or every unrelated use of these very common
// stdlib functions starts failing this test.
var (
	goMessageBranchPattern = regexp.MustCompile(`strings\.(Contains|HasPrefix|EqualFold)\([^\n]*\b(err\.Error\(\)|\.Message\b|\.ErrorMessage\b)`)
	tsMessageBranchPattern = regexp.MustCompile(`\.message\.(includes|startsWith|indexOf|match)\(|error\.message\s*(===|==|!==|!=)|(===|==|!==|!=)\s*error\.message`)
)

// TestNoConsumerBranchesOnErrorMessageText (ERRC-03, D-08, PITFALLS.md
// Pitfall 12) mechanically scans every non-test consumer source this plan
// migrated -- internal/mcpserver's production Go files (excluding
// *_test.go) and the frontend's API and workspace error consumers, scanned as plain text --
// for a branch keyed on human-readable error message text instead of the
// machine-readable code. Test files are intentionally excluded: asserting
// on message text IN A TEST is legitimate and several existing tests
// (client_test.go's quota_exceeded/rate_limited substring checks) do it
// deliberately, to pin the human-readable wire format -- this test would
// wrongly fail the entire suite if it scanned test files too.
func TestNoConsumerBranchesOnErrorMessageText(t *testing.T) {
	repoRoot, err := findRepoRoot(t)
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}

	var violations []string

	goFiles, err := filepath.Glob(filepath.Join(repoRoot, "internal", "mcpserver", "*.go"))
	if err != nil {
		t.Fatalf("glob internal/mcpserver: %v", err)
	}
	if len(goFiles) == 0 {
		t.Fatal("glob internal/mcpserver/*.go returned no files -- repoRoot detection is broken")
	}
	for _, f := range goFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		violations = append(violations, scanFileForMessageBranch(t, f, goMessageBranchPattern)...)
	}

	for _, rel := range []string{
		filepath.Join("frontend", "src", "api.ts"),
		filepath.Join("frontend", "src", "App.tsx"),
		filepath.Join("frontend", "src", "errors.ts"),
		filepath.Join("frontend", "src", "Workspace.tsx"),
	} {
		violations = append(violations, scanFileForMessageBranch(t, filepath.Join(repoRoot, rel), tsMessageBranchPattern)...)
	}

	if len(violations) > 0 {
		t.Fatalf("found %d consumer branch(es) on error message text -- branch on the machine-readable code instead:\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

// scanFileForMessageBranch reads path and returns one "path:line: text"
// entry per line that (a) is not a comment/doc-comment line (trimmed prefix
// "//" or "*", so this plan's own explanatory comments about the pattern
// can never trip it) and (b) matches pattern.
func scanFileForMessageBranch(t *testing.T, path string, pattern *regexp.Regexp) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var hits []string
	for i, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if pattern.MatchString(line) {
			hits = append(hits, fmt.Sprintf("%s:%d: %s", path, i+1, trimmed))
		}
	}
	return hits
}
