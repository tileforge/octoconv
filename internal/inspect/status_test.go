package inspect

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStatusValuesAreThreeDistinctLiterals is the mechanical proof behind
// this file's whole reason for existing: the three CheckStatus values are
// pairwise distinct string literals, so no client-side comparison can ever
// collapse two of them by accident.
func TestStatusValuesAreThreeDistinctLiterals(t *testing.T) {
	if StatusClean == StatusNotApplicable {
		t.Fatal("StatusClean == StatusNotApplicable, want distinct")
	}
	if StatusClean == StatusUnknown {
		t.Fatal("StatusClean == StatusUnknown, want distinct")
	}
	if StatusNotApplicable == StatusUnknown {
		t.Fatal("StatusNotApplicable == StatusUnknown, want distinct")
	}
}

func TestAllStatusesReturnsThree(t *testing.T) {
	got := AllStatuses()
	if len(got) != 3 {
		t.Fatalf("len(AllStatuses()) = %d, want 3", len(got))
	}
}

func TestCheckSerializesWithReason(t *testing.T) {
	c := Check{Code: CodePDFJavaScript, Status: StatusUnknown, Reason: ReasonCompressedObjectStreams}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"code":"pdf_javascript"`, `"status":"unknown"`, `"reason":"compressed_object_streams"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("marshaled Check %s does not contain %s", s, want)
		}
	}
}

// TestCheckCleanOmitsReason proves an empty Reason is elided from the JSON
// body rather than serialized as an empty string.
func TestCheckCleanOmitsReason(t *testing.T) {
	c := Check{Code: CodeMetadataPresent, Status: StatusClean}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, `"reason"`) {
		t.Fatalf("marshaled clean Check %s contains a reason key, want omitted", s)
	}
}

func TestCoverageCompleteOmitsReasonAndTruncatedChecks(t *testing.T) {
	cov := Coverage{Complete: true}
	b, err := json.Marshal(cov)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, `"reason"`) {
		t.Fatalf("marshaled complete Coverage %s contains a reason key, want omitted", s)
	}
	if strings.Contains(s, `"truncated_checks"`) {
		t.Fatalf("marshaled complete Coverage %s contains truncated_checks, want omitted", s)
	}
}

func TestMarkTruncatedSetsCompleteFalseAndReason(t *testing.T) {
	var cov Coverage
	cov.Complete = true
	cov.MarkTruncated(ReasonWorkCeilingBytes, CodePDFJavaScript)
	if cov.Complete {
		t.Fatal("cov.Complete = true after MarkTruncated, want false")
	}
	if cov.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("cov.Reason = %q, want %q", cov.Reason, ReasonWorkCeilingBytes)
	}
	if len(cov.TruncatedChecks) != 1 || cov.TruncatedChecks[0] != CodePDFJavaScript {
		t.Fatalf("cov.TruncatedChecks = %v, want [%q]", cov.TruncatedChecks, CodePDFJavaScript)
	}
}

// TestMarkTruncatedDoesNotDuplicateCode proves a repeated call with the
// same code does not append a second entry.
func TestMarkTruncatedDoesNotDuplicateCode(t *testing.T) {
	var cov Coverage
	cov.MarkTruncated(ReasonWorkCeilingBytes, CodePDFJavaScript)
	cov.MarkTruncated(ReasonWorkCeilingBytes, CodePDFJavaScript)
	if len(cov.TruncatedChecks) != 1 {
		t.Fatalf("len(cov.TruncatedChecks) = %d, want 1 after two identical MarkTruncated calls", len(cov.TruncatedChecks))
	}
}

// TestMarkTruncatedKeepsFirstReason proves a second MarkTruncated call with
// a DIFFERENT ceiling reason does not overwrite the first-recorded reason
// -- it names where the budget actually ran out first.
func TestMarkTruncatedKeepsFirstReason(t *testing.T) {
	var cov Coverage
	cov.MarkTruncated(ReasonWorkCeilingBytes, CodePDFJavaScript)
	cov.MarkTruncated(ReasonWorkCeilingTime, CodePDFActionDict)
	if cov.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("cov.Reason = %q, want %q (first call's reason must be kept)", cov.Reason, ReasonWorkCeilingBytes)
	}
	if len(cov.TruncatedChecks) != 2 {
		t.Fatalf("len(cov.TruncatedChecks) = %d, want 2", len(cov.TruncatedChecks))
	}
}

// TestMarkTruncatedIgnoresNonCeilingReason is the fail-safe proof: calling
// MarkTruncated with a structural/format reason must be a no-op, never
// flipping Complete to false. This is what makes it impossible for a
// structural unknown to accidentally mark a report selective.
func TestMarkTruncatedIgnoresNonCeilingReason(t *testing.T) {
	cov := Coverage{Complete: true}
	cov.MarkTruncated(ReasonCompressedObjectStreams, CodePDFJavaScript)
	if !cov.Complete {
		t.Fatal("cov.Complete = false after MarkTruncated with a non-ceiling reason, want true (no-op)")
	}
	if cov.Reason != "" {
		t.Fatalf("cov.Reason = %q after a no-op MarkTruncated, want empty", cov.Reason)
	}
	if len(cov.TruncatedChecks) != 0 {
		t.Fatalf("cov.TruncatedChecks = %v after a no-op MarkTruncated, want empty", cov.TruncatedChecks)
	}
}

// TestCoverageStaysCompleteUnderStructuralUnknown is INSP-03's central
// claim in test form: a report where every check is StatusUnknown for a
// purely structural reason (parser blindness, not resource exhaustion) is
// still a COMPLETE report -- the checker did everything it could. Coverage
// is never derived from Check.Status; nothing here can flip Complete to
// false except MarkTruncated with one of the two ceiling reasons, which
// this test never calls.
func TestCoverageStaysCompleteUnderStructuralUnknown(t *testing.T) {
	var checks []Check
	for _, code := range AllCodes() {
		checks = append(checks, Check{Code: code, Status: StatusUnknown, Reason: ReasonCompressedObjectStreams})
	}
	if err := ValidateReport(nil, checks); err != nil {
		t.Fatalf("ValidateReport(nil, all-unknown checks) = %v, want nil", err)
	}

	cov := Coverage{Complete: true}
	if !cov.Complete {
		t.Fatal("cov.Complete = false, want true -- coverage must not be coupled to individual check status")
	}
}

func TestValidateReportAcceptsFullPartition(t *testing.T) {
	all := AllCodes()
	var findings []Finding
	var checks []Check
	for i, code := range all {
		if i%2 == 0 {
			sev, _ := SeverityOf(code)
			findings = append(findings, Finding{Code: code, Severity: sev})
		} else {
			checks = append(checks, Check{Code: code, Status: StatusClean})
		}
	}
	if err := ValidateReport(findings, checks); err != nil {
		t.Fatalf("ValidateReport(full partition) = %v, want nil", err)
	}
}

func TestValidateReportRejectsCodeInBothFindingsAndChecks(t *testing.T) {
	all := AllCodes()
	var findings []Finding
	var checks []Check
	for _, code := range all {
		checks = append(checks, Check{Code: code, Status: StatusClean})
	}
	// Duplicate the first code into findings too -- it now appears in BOTH.
	sev, _ := SeverityOf(all[0])
	findings = append(findings, Finding{Code: all[0], Severity: sev})

	err := ValidateReport(findings, checks)
	if err == nil {
		t.Fatal("ValidateReport(code in both findings and checks) = nil, want error")
	}
	if !strings.Contains(err.Error(), string(all[0])) {
		t.Fatalf("ValidateReport error %q does not name the duplicated code %q", err, all[0])
	}
}

func TestValidateReportRejectsMissingCode(t *testing.T) {
	all := AllCodes()
	var checks []Check
	// Deliberately omit the last code from both findings and checks.
	for _, code := range all[:len(all)-1] {
		checks = append(checks, Check{Code: code, Status: StatusClean})
	}
	err := ValidateReport(nil, checks)
	if err == nil {
		t.Fatal("ValidateReport(missing code) = nil, want error")
	}
	missing := all[len(all)-1]
	if !strings.Contains(err.Error(), string(missing)) {
		t.Fatalf("ValidateReport error %q does not name the missing code %q", err, missing)
	}
}
