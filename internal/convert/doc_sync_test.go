// This test is what makes docs/SANITIZE.md a CONTRACT rather than plain
// documentation. It proves three things mechanically: that the document's two
// tables equal the two closed vocabularies in findings.go, in both directions;
// that the document still carries the exact phrase stating the boundary of the
// guarantee; and that neither the document nor the Go vocabulary contains an
// affirmative claim that a file is safe.
//
// If any of these fails, the correct response is to fix whichever side is
// wrong -- never to relax the test. Phase 59 is the reason that sentence is
// here: ocrguards.go promised a route that had never existed, in a comment,
// for months, and nothing could notice because a comment cannot fail.
//
// Same mechanism as internal/errcode/doc_sync_test.go and
// internal/inspect/doc_sync_test.go, deliberately -- a third vocabulary held
// to the document the same way the first two are.
package convert

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Paths are repo-relative from this package's directory (internal/convert/),
// so they resolve regardless of where `go test` was invoked from.
const (
	sanitizeDocPath   = "../../docs/SANITIZE.md"
	findingsFilePath  = "findings.go"
	reportFilePath    = "sanitizereport.go"
	threatTableHeader = "## Threat classes removed"
	lostTableHeader   = "## What the method destroys"
)

// guaranteeBoundaryPhrase is the exact stable phrase docs/SANITIZE.md must
// contain. Keeping it as a named constant is what makes the coupling between
// this test and the document's prose visible: if the wording is rewritten,
// this constant is the one place that must change with it, and changing it is
// a deliberate act rather than an accident.
//
// The phrase chosen is the one a client is most likely to need and least
// likely to infer: the operation removes what it enumerates and nothing wider.
const guaranteeBoundaryPhrase = "does not detect malicious content outside the enumerated classes"

// codeRowPattern matches a markdown table row whose first cell is a single
// backtick-delimited machine-safe token. Anchored to the line start and to the
// FIRST cell on purpose: the tables' later columns legitimately use backticks
// around PDF keys (`/OpenAction`), part names (`vbaProject.bin`) and tool
// names (`unzip`), and a whole-row scan would report every one of those as an
// undeclared code.
var codeRowPattern = regexp.MustCompile("^\\|\\s*`([a-z][a-z0-9_]*)`")

func readSanitizeDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(sanitizeDocPath)
	if err != nil {
		t.Fatalf("read %s: %v -- docs/SANITIZE.md must exist (60-05-PLAN.md Task 2)", sanitizeDocPath, err)
	}
	return string(b)
}

// extractTableCodes returns every first-column token from the table between
// the line carrying startHeader as a prefix and the next "## " header.
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

// diffSets names each offender individually, so a failure points at the exact
// code rather than at two lists the reader has to compare by eye.
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

// TestSanitizeDocMatchesGoVocabularies is the bidirectional proof.
//
// One direction alone is not enough, and both failure modes have happened in
// this repository: a code in the document that Go does not declare is a
// promise of something that does not happen, and a code Go declares that the
// document omits is silence about something that does. Neither is caught by
// checking only the other way.
func TestSanitizeDocMatchesGoVocabularies(t *testing.T) {
	doc := readSanitizeDoc(t)

	docThreats := extractTableCodes(doc, threatTableHeader)
	docLost := extractTableCodes(doc, lostTableHeader)

	if len(docThreats) == 0 {
		t.Fatalf("extracted zero codes under %q -- the section header or table format in "+
			"docs/SANITIZE.md changed in a way this parser no longer understands", threatTableHeader)
	}
	if len(docLost) == 0 {
		t.Fatalf("extracted zero codes under %q -- same problem", lostTableHeader)
	}

	var goThreats []string
	for _, c := range AllThreatClasses() {
		goThreats = append(goThreats, string(c))
	}
	var goLost []string
	for _, c := range AllLostCapabilities() {
		goLost = append(goLost, string(c))
	}

	if missing, extra := diffSets(goThreats, docThreats); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("threat-class drift between AllThreatClasses() and docs/SANITIZE.md -- "+
			"declared in Go but missing from the document: %v; present in the document but not "+
			"declared in Go: %v", missing, extra)
	}
	if missing, extra := diffSets(goLost, docLost); len(missing) > 0 || len(extra) > 0 {
		t.Errorf("lost-capability drift between AllLostCapabilities() and docs/SANITIZE.md -- "+
			"declared in Go but missing from the document: %v; present in the document but not "+
			"declared in Go: %v", missing, extra)
	}
}

// TestSanitizeDocCarriesTheGuaranteeBoundary pins the sentence that stops a
// client from reading the tables as a wider claim than they are.
func TestSanitizeDocCarriesTheGuaranteeBoundary(t *testing.T) {
	doc := readSanitizeDoc(t)
	if !strings.Contains(doc, guaranteeBoundaryPhrase) {
		t.Errorf("docs/SANITIZE.md no longer contains the guarantee-boundary phrase %q.\n"+
			"If the wording was deliberately rewritten, update guaranteeBoundaryPhrase in this "+
			"file to match. If it was deleted, put it back: without it the document's tables "+
			"read as a claim about the file rather than about the enumerated classes.",
			guaranteeBoundaryPhrase)
	}
}

// ---------------------------------------------------------------------------
// the ban on affirmative safety claims
// ---------------------------------------------------------------------------

// THE PROBLEM THIS MATCHER HAS TO SOLVE, AND WHY IT IS NARROW ON PURPOSE
//
// The honest prose in docs/SANITIZE.md and findings.go is FULL of the banned
// words, because saying what the service does not claim requires naming the
// claim:
//
//	"does not determine whether a document was malicious"
//	"is not a substitute for antivirus scanning"
//	"may assert that a file is safe, clean, virus-free, or free of malicious content"
//	"There is no `clean` boolean"
//
// A matcher that fires on the word "safe" would redden on all four. It would
// be the test that is deleted, not the prose -- and the ban would be gone with
// it. So the matcher works in two stages: it discards any sentence carrying a
// prohibition or negation cue, then looks for AFFIRMATIVE frames in what is
// left.
//
// The narrowness of that rule is not asserted -- it is pinned by two tables
// below, one of sentences that MUST match and one of the document's real
// sentences that MUST NOT. A future widening of the matcher that starts firing
// on honest prose fails its own test before it fails anyone else's.
//
// Extend the pattern list deliberately, and add to both tables when you do.

// negationCues mark a CLAUSE as being ABOUT the claim rather than making it.
//
// CLAUSE, not sentence -- and that word is the third correction this matcher
// has taken. Skipping a whole sentence on any cue anywhere in it let a mixed
// sentence through:
//
//	"The output is safe and does not contain macros."
//
// carries the banned frame in its first clause and a negation cue in its
// second, and the sentence-level rule discarded the whole thing. Caught in
// review (PR #12) after the must-not-match table had already made the matcher
// narrow enough to survive -- narrow is not the same as correct, and the
// table could only ever prove the first.
//
// Splitting on clause boundaries first means a cue shelters what it actually
// negates and nothing else.
//
// Note "no " carries a trailing space deliberately: without it, "nor", "none"
// and "normalize" would all read as negations.
var negationCues = []string{
	"not ", "never", "no ", "cannot", "without", "forbid", "ban ", "banned",
	"may assert", "would be", "would assert", "prohibit", "must not", "does not",
	// "neither ... nor" is a negation this list originally missed, and
	// docs/SANITIZE.md's own description of this very test tripped over it.
	"neither", "nor ",
}

// clauseSeparators split a sentence into the units a negation cue governs.
// Deliberately short: a coordinating conjunction joins two independent
// assertions, and "and" is the one that carried the bypass above.
var clauseSeparators = []string{" and ", " but ", " while ", " although ", "; ", ", and ", ", but "}

// splitClauses breaks one sentence into clauses, longest separator first so
// ", and " is not split twice.
func splitClauses(sentence string) []string {
	parts := []string{sentence}
	for _, sep := range clauseSeparators {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	return parts
}

// bannedAffirmativePatterns are the frames that actually assert safety.
var bannedAffirmativePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(file|document|output|result)\s+is\s+(now\s+)?(safe|clean|virus[-\s]free|malware[-\s]free)\b`),
	regexp.MustCompile(`(?i)\b(safe|clean|virus[-\s]free|malware[-\s]free)\s+to\s+(open|use|share)\b`),
	regexp.MustCompile(`(?i)\bguarantee[sd]?\s+(that\s+)?(the\s+)?(file|document|output)\s+is\b`),
	regexp.MustCompile(`(?i)\bfree\s+of\s+(malware|viruses|malicious\s+code)\b`),
	regexp.MustCompile(`(?i)\b(malware|viruses)\s+(were\s+)?(removed|eliminated)\b`),
	regexp.MustCompile(`(?i)\bcertified\s+(safe|clean)\b`),
}

// unwrap rejoins hard-wrapped lines into whole sentences before anything is
// split.
//
// This step is not cosmetic; without it the check fires on its own source.
// Both markdown and Go comments wrap at a column, so findings.go's
//
//	// No constant, and no text assembled from these constants, may assert that a
//	// file is safe, clean, virus-free, or free of malicious content.
//
// arrives as two lines, and the second one -- "file is safe, clean,
// virus-free" -- carries the banned frame with its negation cue stranded on
// the line above. Splitting on newlines therefore reports the ban's own
// statement as a violation of the ban. Reconstruct the sentence first, then
// split it.
//
// A blank line ends a paragraph, so two unrelated paragraphs never merge and
// a cue in one cannot shelter a claim in the other.
func unwrap(text string) string {
	var out strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		for _, line := range strings.Split(para, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//"))
			out.WriteString(line)
			out.WriteByte(' ')
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// splitSentences splits on sentence terminators, on paragraph breaks, and on
// markdown cell boundaries. The last one matters: a banned claim smuggled into
// a table cell must still be found, and a negation cue in a neighbouring cell
// must not shelter it.
func splitSentences(text string) []string {
	f := func(r rune) bool { return r == '.' || r == '!' || r == '?' || r == '\n' || r == '|' }
	return strings.FieldsFunc(unwrap(text), f)
}

func affirmativeSafetyClaims(text string) []string {
	var hits []string
	for _, sentence := range splitSentences(text) {
		for _, clause := range splitClauses(sentence) {
			lower := strings.ToLower(clause)
			negated := false
			for _, cue := range negationCues {
				if strings.Contains(lower, cue) {
					negated = true
					break
				}
			}
			if negated {
				continue
			}
			for _, pat := range bannedAffirmativePatterns {
				if pat.MatchString(clause) {
					hits = append(hits, strings.TrimSpace(clause))
					break
				}
			}
		}
	}
	return hits
}

// TestMatcherIsNarrowEnoughToSurvive pins the matcher against both kinds of
// mistake BEFORE it is pointed at any real file. Without this, the first false
// positive on honest prose would be answered by deleting the check.
func TestMatcherIsNarrowEnoughToSurvive(t *testing.T) {
	mustMatch := []string{
		"After sanitization the file is safe.",
		"The output is clean and ready for distribution.",
		"The result is now virus-free.",
		"This document is safe to open.",
		"We guarantee that the file is free of threats.",
		"The sanitized output is free of malware.",
		"All viruses were removed.",
		"| `javascript` | the file is safe afterwards | ... |",
		// The mixed-clause bypass (PR #12): banned frame in one clause, a
		// negation cue in another. Sentence-level shelter let this through.
		"The output is safe and does not contain macros.",
		"This document is clean, but you should still scan it.",
		"No macros remain and the file is safe to open.",
	}
	for _, s := range mustMatch {
		if len(affirmativeSafetyClaims(s)) == 0 {
			t.Errorf("matcher missed an affirmative safety claim: %q", s)
		}
	}

	// These are real sentences from the files this test guards. Every one of
	// them contains a banned word and none of them makes the banned claim.
	mustNotMatch := []string{
		"does not determine whether a document was malicious.",
		"is not a substitute for antivirus scanning.",
		"No constant may assert that a file is safe, clean, virus-free, or free of malicious content.",
		"There is no clean boolean and no summary sentence.",
		"a service that removes seven classes of carrier and reports safe has made a claim it did not test.",
		"Absence from the report means not enumerated, not not present.",
		"It does not analyse what any code would have done.",
		// The wrapped form of the ban's own statement, exactly as findings.go
		// carries it. This case is the reason unwrap() exists; without it the
		// check reports the prohibition as a violation of itself.
		"// No constant, and no text assembled from these constants, may assert that a\n// file is safe, clean, virus-free, or free of malicious content.",
		// docs/SANITIZE.md describing this test. Caught by the "neither" cue,
		// which the first version of this list did not have.
		"that neither this document nor the Go vocabulary contains an affirmative claim that a file is safe, clean, virus-free or free of malicious content",
	}
	for _, s := range mustNotMatch {
		if hits := affirmativeSafetyClaims(s); len(hits) > 0 {
			t.Errorf("matcher fired on honest prose %q (hit: %q) -- widen the negation cues, "+
				"do not delete the check", s, hits)
		}
	}
}

// TestNoAffirmativeSafetyClaims is the ban itself, applied to the document and
// to the Go files that could carry such a claim into a client's hands.
func TestNoAffirmativeSafetyClaims(t *testing.T) {
	targets := []string{sanitizeDocPath, findingsFilePath, reportFilePath}
	for _, path := range targets {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if hits := affirmativeSafetyClaims(string(b)); len(hits) > 0 {
			t.Errorf("%s asserts that a file is safe: %q.\n"+
				"CDR removes carriers; it does not determine whether code is malicious. "+
				"A service that removes the enumerated classes and reports safety has made a "+
				"claim it did not test (SANI-05).", path, hits)
		}
	}
}

// TestReportHasNoVerdictField pins the shape rather than the prose. A boolean
// or a summary string would be the forbidden claim in structured form, and
// structure is what an SDK generator turns into a client-visible field.
func TestReportHasNoVerdictField(t *testing.T) {
	rep := BuildSanitizeReport("pdf", SanitizeResult{
		Removed:          []ThreatClass{ThreatJavaScript},
		LostCapabilities: []LostCapability{LostBookmarks},
		Verified:         []ThreatClass{ThreatJavaScript},
	})
	blob, err := MarshalSanitizeReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"clean"`, `"safe"`, `"verdict"`, `"summary"`, `"is_safe"`} {
		if strings.Contains(string(blob), forbidden) {
			t.Errorf("the report carries a %s field -- a client will read it as a verdict, "+
				"which is the claim SANI-05 forbids", forbidden)
		}
	}
}
