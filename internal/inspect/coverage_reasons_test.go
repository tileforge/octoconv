package inspect

// Phase 59, D-11. These tests cover the one rule that decides whether a
// report may call itself complete, and they are deliberately written from
// BOTH sides: the reasons that must lose coverage, and — just as important —
// the ones that must not. Widening the first set is easy; the failure mode
// worth testing for is widening it too far and dissolving the D-02a
// distinction the whole of Phase 58 was built to preserve.

import (
	"testing"
)

// coverageLosingReasons is this test file's own INDEPENDENT statement of the
// rule, deliberately not derived from makesReportIncomplete — a test that
// asks the implementation what it does and then agrees proves nothing.
//
// It is a map over the full vocabulary rather than a list of interesting
// cases: TestEveryDeclaredReasonHasACoverageExpectation below fails on any
// reason AllReasons() declares and this map does not mention, so a Reason
// added later cannot slip through with nobody having decided which side of
// the line it falls on. That is the same mechanism AllReasons() itself
// exists to provide for the docs.
var coverageLosingReasons = map[Reason]bool{
	// Loses coverage: part of the input was never examined.
	ReasonWorkCeilingBytes: true,
	ReasonWorkCeilingTime:  true,
	ReasonReadFailed:       true,

	// Keeps coverage: a finished answer about the file, or about this
	// build's declared limits, or about applicability.
	ReasonFormatNotApplicable:     false,
	ReasonFormatUnrecognized:      false,
	ReasonCompressedObjectStreams: false,
	ReasonPDFNameEscape:           false,
	ReasonPDFStreamBodyToken:      false,
	ReasonParserUnavailable:       false,
	ReasonMalformedInput:          false,
}

// TestEveryDeclaredReasonHasACoverageExpectation is the guard that keeps the
// table above honest as the vocabulary grows. Without it, adding an
// eleventh Reason would leave its coverage semantics untested and unnoticed
// — the table would still pass, having simply never heard of it.
func TestEveryDeclaredReasonHasACoverageExpectation(t *testing.T) {
	for _, r := range AllReasons() {
		if _, declared := coverageLosingReasons[r]; !declared {
			t.Fatalf("Reason %q is declared by AllReasons() but has no entry in coverageLosingReasons -- "+
				"whoever added it must decide, explicitly, whether it means 'part of the input went unexamined' "+
				"(loses coverage) or 'here is a finished answer' (keeps it), and record that decision here and in "+
				"makesReportIncomplete", r)
		}
	}
	if len(coverageLosingReasons) != len(AllReasons()) {
		t.Fatalf("coverageLosingReasons has %d entries, AllReasons() declares %d -- the table names a reason the "+
			"vocabulary no longer has", len(coverageLosingReasons), len(AllReasons()))
	}
}

// TestMarkTruncatedAdmitsExactlyTheCoverageLosingReasons drives every reason
// in the vocabulary through MarkTruncated and asserts the full outcome, not
// just Complete: an admitted reason must also be stored VERBATIM and must
// name its code in TruncatedChecks, and a refused one must leave every field
// untouched.
func TestMarkTruncatedAdmitsExactlyTheCoverageLosingReasons(t *testing.T) {
	for _, r := range AllReasons() {
		wantLoses := coverageLosingReasons[r]
		t.Run(string(r), func(t *testing.T) {
			cov := Coverage{Complete: true}
			cov.MarkTruncated(r, CodePDFJavaScript)

			if cov.Complete == wantLoses {
				t.Fatalf("MarkTruncated(%q): Complete = %v, want %v", r, cov.Complete, !wantLoses)
			}
			if !wantLoses {
				if cov.Reason != "" || len(cov.TruncatedChecks) != 0 {
					t.Fatalf("MarkTruncated(%q) must be a total no-op, got Reason=%q TruncatedChecks=%v", r, cov.Reason, cov.TruncatedChecks)
				}
				return
			}
			if cov.Reason != r {
				t.Fatalf("MarkTruncated(%q): Reason = %q, want the reason stored verbatim -- D-11 forbids re-labelling "+
					"a read failure as a ceiling on its way into this field", r, cov.Reason)
			}
			if len(cov.TruncatedChecks) != 1 || cov.TruncatedChecks[0] != CodePDFJavaScript {
				t.Fatalf("MarkTruncated(%q): TruncatedChecks = %v, want [%s] -- Complete:false beside an empty list "+
					"tells a client the report is selective while refusing to say about what", r, cov.TruncatedChecks, CodePDFJavaScript)
			}
		})
	}
}

// TestMarkTruncatedByCharacteristicUsesTheSameGate pins that the two Mark*
// methods cannot drift apart. They gate themselves independently, so a
// future edit could easily widen one and not the other — and the result
// would be invisible: one path would honestly degrade the report while the
// other kept claiming completeness for the identical reason.
func TestMarkTruncatedByCharacteristicUsesTheSameGate(t *testing.T) {
	for _, r := range AllReasons() {
		wantLoses := coverageLosingReasons[r]
		t.Run(string(r), func(t *testing.T) {
			cov := Coverage{Complete: true}
			cov.MarkTruncatedByCharacteristic(r)

			if cov.Complete == wantLoses {
				t.Fatalf("MarkTruncatedByCharacteristic(%q): Complete = %v, want %v -- this gate must admit exactly "+
					"what MarkTruncated admits", r, cov.Complete, !wantLoses)
			}
			if wantLoses && cov.Reason != r {
				t.Fatalf("MarkTruncatedByCharacteristic(%q): Reason = %q, want it verbatim", r, cov.Reason)
			}
			// TruncatedChecks is Code-keyed and a bare characteristic key
			// names no Code -- this method must never append to it.
			if len(cov.TruncatedChecks) != 0 {
				t.Fatalf("MarkTruncatedByCharacteristic(%q) appended to TruncatedChecks (%v); a characteristic key "+
					"does not name a Code", r, cov.TruncatedChecks)
			}
		})
	}
}

// TestBuildReportAllChecksReadFailedIsNotComplete is the end-to-end case this
// plan exists for.
//
// REPRODUCED before the fix: this exact report shipped coverage.complete:true
// while every single check in it said unknown/read_failed — "I examined this
// in full" and "I could not read it" in one body.
func TestBuildReportAllChecksReadFailedIsNotComplete(t *testing.T) {
	var checks []Check
	for _, code := range AllCodes() {
		checks = append(checks, Check{Code: code, Status: StatusUnknown, Reason: ReasonReadFailed})
	}

	r := BuildReport("pdf", "pdf", 1024, nil, FamilyResult{Characteristics: Characteristics{}, Checks: checks})

	if r.Coverage.Complete {
		t.Fatal("Coverage.Complete = true for a report in which every check is unknown/read_failed -- " +
			"the report claims to have examined in full an upload it could not read at all")
	}
	if r.Coverage.Reason != ReasonReadFailed {
		t.Fatalf("Coverage.Reason = %q, want %q -- a read failure must reach the client as itself, never "+
			"re-labelled as a ceiling (D-11 rejects the PR9R13 tracker-marking mechanism for exactly this reason)",
			r.Coverage.Reason, ReasonReadFailed)
	}
	if len(r.Coverage.TruncatedChecks) != len(AllCodes()) {
		t.Fatalf("TruncatedChecks has %d entries, want %d (every check was cut short)", len(r.Coverage.TruncatedChecks), len(AllCodes()))
	}
}

// TestBuildReportCharacteristicReadFailedIsNotComplete covers the OTHER path
// into Coverage: a "<name>_reason" characteristic with no accompanying Check.
//
// This is the path Plan 59-05's text_layer_reason will travel, which is why
// it is proven here rather than assumed there: if this test did not exist,
// 59-05 would be relying on a mechanism nothing had exercised for a
// non-ceiling reason.
func TestBuildReportCharacteristicReadFailedIsNotComplete(t *testing.T) {
	fam := FamilyResult{
		Characteristics: Characteristics{
			"page_count":        nil,
			"page_count_reason": string(ReasonReadFailed),
		},
	}

	r := BuildReport("pdf", "pdf", 1024, nil, fam)

	if r.Coverage.Complete {
		t.Fatal("Coverage.Complete = true while a characteristic in the same body reports read_failed")
	}
	if r.Coverage.Reason != ReasonReadFailed {
		t.Fatalf("Coverage.Reason = %q, want %q", r.Coverage.Reason, ReasonReadFailed)
	}
	if len(r.Coverage.TruncatedChecks) != 0 {
		t.Fatalf("TruncatedChecks = %v, want empty -- a bare characteristic key names no Code", r.Coverage.TruncatedChecks)
	}
}

// TestBuildReportStructuralReasonsStillKeepCoverageComplete is the test that
// matters most in this file, and the one whose absence would let a
// well-meaning widening quietly destroy INSP-03.
//
// Every reason on the keeps-coverage side of the table is driven through the
// real assembler. If any of them ever starts flipping Complete to false, the
// word "selective" stops carrying information: it would then be reported for
// a PDF whose objects merely live in an /ObjStm, which is the overwhelmingly
// common case and precisely the case D-02a decided was a complete report
// about a file this build cannot fully see.
func TestBuildReportStructuralReasonsStillKeepCoverageComplete(t *testing.T) {
	for r, loses := range coverageLosingReasons {
		if loses {
			continue
		}
		t.Run(string(r), func(t *testing.T) {
			var checks []Check
			for _, code := range AllCodes() {
				checks = append(checks, Check{Code: code, Status: StatusUnknown, Reason: r})
			}
			rep := BuildReport("pdf", "pdf", 1024, nil, FamilyResult{Characteristics: Characteristics{}, Checks: checks})

			if !rep.Coverage.Complete {
				t.Fatalf("Coverage.Complete = false when every check carries %q -- structural blindness about the "+
					"FILE, or a declared limit of this build, is a COMPLETE report about what could be determined; "+
					"only unexamined input makes a report selective (D-02a)", r)
			}
			if rep.Coverage.Reason != "" {
				t.Fatalf("Coverage.Reason = %q alongside Complete=true", rep.Coverage.Reason)
			}
		})
	}
}
