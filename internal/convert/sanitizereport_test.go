package convert

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeReportKeepsTheTwoListsApart is the D-02 test. A lost form is not
// a found attack, and the only structural way to keep a client from reading it
// as one is for the two never to appear in the same field.
func TestSanitizeReportKeepsTheTwoListsApart(t *testing.T) {
	rep := BuildSanitizeReport("pdf", SanitizeResult{
		Removed:          []ThreatClass{ThreatJavaScript, ThreatActionDict},
		LostCapabilities: []LostCapability{LostFillableForms, LostBookmarks},
		Verified:         []ThreatClass{ThreatJavaScript},
	})

	for _, lost := range rep.LostCapabilities {
		for _, removed := range rep.Removed {
			if string(lost) == string(removed) {
				t.Errorf("%q appears in both lists; a client cannot tell a cost from a threat", lost)
			}
		}
	}

	blob, err := MarshalSanitizeReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"removed", "lost_capabilities", "verified"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("report has no %q field", field)
		}
	}
}

// TestSanitizeReportCarriesOnlyVocabularyCodes is the "no prose" rule made
// mechanical. Every string in either list must be a declared code -- a report
// that grew a free-text entry would otherwise pass every other test here.
func TestSanitizeReportCarriesOnlyVocabularyCodes(t *testing.T) {
	rep := BuildSanitizeReport("docm", SanitizeResult{
		Removed:          AllThreatClasses(),
		LostCapabilities: AllLostCapabilities(),
		Verified:         AllThreatClasses(),
	})

	for _, c := range append(rep.Removed, rep.Verified...) {
		if !IsKnownThreatClass(c) {
			t.Errorf("report carries an undeclared threat class %q", c)
		}
	}
	for _, c := range rep.LostCapabilities {
		if !IsKnownLostCapability(c) {
			t.Errorf("report carries an undeclared lost capability %q", c)
		}
	}
}

// TestSanitizeReportEmptyListsMarshalAsArrays covers the lossless image path.
// "Nothing was lost" and "the field is missing" are different facts and a
// client should not have to guess which one a null means.
func TestSanitizeReportEmptyListsMarshalAsArrays(t *testing.T) {
	rep := BuildSanitizeReport("jpg", SanitizeResult{
		Removed:  []ThreatClass{ThreatDocumentMetadata},
		Verified: []ThreatClass{ThreatDocumentMetadata},
		// LostCapabilities deliberately nil: the image path loses nothing.
	})
	blob, err := MarshalSanitizeReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "null") {
		t.Errorf("report marshalled a null:\n%s", blob)
	}
	if !strings.Contains(string(blob), `"lost_capabilities": []`) {
		t.Errorf("empty lost_capabilities did not marshal as an empty array:\n%s", blob)
	}
}

// TestSanitizeReportFromARealRun ties the report to what the operation
// actually did, rather than to hand-built inputs. It is the one test here that
// would notice if a format's handler stopped filling a field.
func TestSanitizeReportFromARealRun(t *testing.T) {
	requireGhostscript(t)
	requireVerifierBinaries(t)

	out := filepath.Join(t.TempDir(), "out.pdf")
	res, err := SanitizeFile(context.Background(), "pdf",
		filepath.Join(sanitizeCorpusDir, "threat_all_classes.pdf"), out)
	if err != nil {
		t.Fatalf("SanitizeFile: %v", err)
	}

	rep := BuildSanitizeReport("pdf", res)
	if len(rep.Removed) == 0 {
		t.Error("removed is empty after a successful run")
	}
	if len(rep.Verified) == 0 {
		t.Error("verified is empty: the removal ran and nothing confirmed it")
	}
	if len(rep.LostCapabilities) == 0 {
		t.Error("lost_capabilities is empty for the PostScript round trip, which does lose things")
	}
	if rep.GuaranteeScope == "" {
		t.Error("guarantee_scope is empty: the report points nowhere")
	}
}

// TestSanitizeReportDoesNotAliasThePackageLists proves the defensive copy. A
// caller mutating the report must not be able to edit the per-format handler's
// own declared list, which every later job would then inherit.
func TestSanitizeReportDoesNotAliasThePackageLists(t *testing.T) {
	res := SanitizeResult{
		Removed:          append([]ThreatClass(nil), pdfThreatClasses...),
		LostCapabilities: append([]LostCapability(nil), pdfLostCapabilities...),
	}
	rep := BuildSanitizeReport("pdf", res)
	if len(rep.Removed) == 0 || len(rep.LostCapabilities) == 0 {
		t.Fatal("fixture is empty; this test proves nothing")
	}

	rep.Removed[0] = "mutated"
	rep.LostCapabilities[0] = "mutated"

	if pdfThreatClasses[0] == "mutated" {
		t.Error("report aliases pdfThreatClasses; a caller's edit reaches the package's list")
	}
	if pdfLostCapabilities[0] == "mutated" {
		t.Error("report aliases pdfLostCapabilities")
	}
}
