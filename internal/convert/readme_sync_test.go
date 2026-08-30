package convert_test

// This file makes README.md's engine-class list a CONTRACT rather than prose,
// the same way internal/errcode/doc_sync_test.go does for the error-code tables
// and internal/inspect/doc_sync_test.go for the findings vocabulary.
//
// It exists because the README previously wrote "the seven known engine
// classes". Phase 60 adds an eighth (probe), which would have made that
// sentence false from a commit that never opens the README — the failure mode
// an unenforced document always eventually has, and one this repository has
// already been bitten by twice.
//
// Replacing the number with a bigger number would only move the problem to the
// ninth class. The list is enumerated instead, and held equal to AllEngines()
// from both sides.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// readmePath is repo-relative from this package's directory.
const readmePath = "../../README.md"

// engineListMarker anchors the machine-checked list. An HTML comment rather
// than a heading: it survives prose being rewritten around it, and it tells the
// next editor that the line below is under test before they change it.
const engineListMarker = "<!-- engine-classes:"

// engineTokenPattern matches the backticked tokens on the list line.
var engineTokenPattern = regexp.MustCompile("`([a-z][a-z0-9]*)`")

// readmeEngineClasses returns the classes enumerated in README.md, or fails
// the test with a message naming what to look for.
func readmeEngineClasses(t *testing.T) []string {
	t.Helper()

	doc, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}

	idx := strings.Index(string(doc), engineListMarker)
	if idx < 0 {
		t.Fatalf("%s no longer contains the marker %q -- the engine-class list is the thing this test holds equal to "+
			"convert.AllEngines(); if the section moved, move the marker with it rather than deleting the check",
			readmePath, engineListMarker)
	}

	// The list is the first line after the marker comment ends.
	rest := string(doc)[idx:]
	end := strings.Index(rest, "-->")
	if end < 0 {
		t.Fatalf("marker comment in %s is unterminated", readmePath)
	}
	lines := strings.Split(rest[end:], "\n")
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		found := engineTokenPattern.FindAllStringSubmatch(line, -1)
		if len(found) == 0 {
			t.Fatalf("the line after the engine-class marker carries no backticked tokens: %q", line)
		}
		out := make([]string, 0, len(found))
		for _, m := range found {
			out = append(out, m[1])
		}
		return out
	}
	t.Fatalf("no content line follows the engine-class marker in %s", readmePath)
	return nil
}

// TestREADMEEngineClassesMatchAllEngines compares the two sets in BOTH
// directions.
//
// One-way checking would miss both halves of what can go wrong, and both have
// happened in this repository: a class present in the document but not in the
// code is a promise of something that does not exist, and a class in the code
// but not the document is silence about something that does. Neither is
// detectable by checking only the other direction.
func TestREADMEEngineClassesMatchAllEngines(t *testing.T) {
	got := readmeEngineClasses(t)
	want := convert.AllEngines()

	gotSet := make(map[string]bool, len(got))
	for _, e := range got {
		gotSet[e] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, e := range want {
		wantSet[e] = true
	}

	var missing, extra []string
	for _, e := range want {
		if !gotSet[e] {
			missing = append(missing, e)
		}
	}
	for _, e := range got {
		if !wantSet[e] {
			extra = append(extra, e)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("README.md's engine-class list has drifted from convert.AllEngines().\n"+
			"  missing from the README: %v\n"+
			"  present in the README but not declared in Go: %v\n\n"+
			"Fix whichever side is wrong — never relax this test. A class in the document but not the code promises "+
			"something that does not exist; a class in the code but not the document leaves an operator unable to set "+
			"a limit they are entitled to.", missing, extra)
	}
}

// TestREADMEDoesNotHardcodeTheEngineCount guards the specific mistake this file
// was written for: a sentence that states HOW MANY classes there are.
//
// Deliberately narrow. Matching any number near the word "engine" would fire on
// unrelated prose, and a test that cries wolf gets deleted rather than obeyed —
// so it looks for the handful of spellings the sentence would actually take,
// and says so, so a future reader can extend the list knowingly.
func TestREADMEDoesNotHardcodeTheEngineCount(t *testing.T) {
	doc, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	lower := strings.ToLower(string(doc))

	// Word forms first, then digit forms, both immediately qualifying
	// "engine". Extend deliberately if a new phrasing appears; do not widen
	// this into a general number matcher.
	for _, form := range []string{
		"five engine", "six engine", "seven engine", "eight engine", "nine engine", "ten engine",
		"5 engine", "6 engine", "7 engine", "8 engine", "9 engine", "10 engine",
		"five known engine", "six known engine", "seven known engine", "eight known engine",
	} {
		if strings.Contains(lower, form) {
			t.Errorf("README.md contains %q.\n\n"+
				"The number of engine classes must not be written out: phase 60 adds an eighth, and a sentence "+
				"stating a count becomes false from a commit that never opens this file. Enumerate the classes "+
				"instead — TestREADMEEngineClassesMatchAllEngines keeps the enumeration honest.", form)
		}
	}
}
