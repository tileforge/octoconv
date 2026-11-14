package inspect

import "testing"

// TestAllCodesCount pins the closed vocabulary at exactly seventeen codes
// (D-05). A change to this number must be a deliberate ADDITION (never a
// rename/removal) and must be reflected in docs/FINDINGS.md in the same
// commit -- doc_sync_test.go enforces that half mechanically.
func TestAllCodesCount(t *testing.T) {
	if got := len(AllCodes()); got != 17 {
		t.Fatalf("len(AllCodes()) = %d, want 17", got)
	}
}

// TestAllCodesReturnsCopy mirrors errcode.TestAllReturnsCopy: mutating the
// slice a caller was handed must never corrupt the package's own closed
// vocabulary for the next caller.
func TestAllCodesReturnsCopy(t *testing.T) {
	first := AllCodes()
	if len(first) == 0 {
		t.Fatal("AllCodes() returned an empty slice")
	}
	original := first[0]
	first[0] = "mutated"

	second := AllCodes()
	if second[0] != original {
		t.Fatalf("AllCodes() second call returned %q, want unmutated %q -- AllCodes() must return a copy", second[0], original)
	}
}

// TestAllCodesOrderIsDeclarationOrder pins the exact declared order --
// callers (docs generation, iteration) may rely on it being stable.
func TestAllCodesOrderIsDeclarationOrder(t *testing.T) {
	want := []Code{
		CodeOOXMLMacro,
		CodeZipBombShape,
		CodeImagePixelBombShape,
		CodeOLECFBLegacy,
		CodeOLECFBEncrypted,
		CodeOLECFBUnclassified,
		CodeExtensionMismatch,
		CodePDFJavaScript,
		CodePDFActionDict,
		CodePDFLaunchAction,
		CodePDFEmbeddedFile,
		CodePDFEncrypted,
		CodeExternalReference,
		CodeMetadataPresent,
		CodeSTLOpen,
		CodeSTLNonManifold,
		CodeSTLUnoriented,
	}
	got := AllCodes()
	if len(got) != len(want) {
		t.Fatalf("len(AllCodes()) = %d, want %d", len(got), len(want))
	}
	for i, c := range want {
		if got[i] != c {
			t.Fatalf("AllCodes()[%d] = %q, want %q", i, got[i], c)
		}
	}
}

func TestIsKnownCodeExactCaseOnly(t *testing.T) {
	if !IsKnownCode("pdf_javascript") {
		t.Fatal(`IsKnownCode("pdf_javascript") = false, want true`)
	}
	if IsKnownCode("pdf_JAVASCRIPT") {
		t.Fatal(`IsKnownCode("pdf_JAVASCRIPT") = true, want false -- comparison must be exact, no case normalization`)
	}
	if IsKnownCode("") {
		t.Fatal(`IsKnownCode("") = true, want false`)
	}
}

func TestSeverityOfKnownCodes(t *testing.T) {
	if sev, ok := SeverityOf(CodePDFJavaScript); !ok || sev != SeverityCritical {
		t.Fatalf("SeverityOf(CodePDFJavaScript) = (%q, %v), want (%q, true)", sev, ok, SeverityCritical)
	}
	if sev, ok := SeverityOf(CodeOLECFBLegacy); !ok || sev != SeverityInfo {
		t.Fatalf("SeverityOf(CodeOLECFBLegacy) = (%q, %v), want (%q, true)", sev, ok, SeverityInfo)
	}
}

// TestSeverityOfUnknownCodeNeverDefaults is the mechanical proof behind the
// doc comment on SeverityOf: an unrecognized code must never silently
// resolve to a severity, especially not the least-alarming one.
func TestSeverityOfUnknownCodeNeverDefaults(t *testing.T) {
	sev, ok := SeverityOf(Code("no_such_code"))
	if ok {
		t.Fatalf("SeverityOf(%q) ok = true, want false", "no_such_code")
	}
	if sev != "" {
		t.Fatalf("SeverityOf(%q) severity = %q, want empty string", "no_such_code", sev)
	}
}

// TestEveryCodeHasSeverity is the single mechanical guarantee that a new
// code cannot be added to allCodes without also declaring its severity: it
// walks the public enumerator (not the private map) and demands ok==true
// for every one.
func TestEveryCodeHasSeverity(t *testing.T) {
	for _, c := range AllCodes() {
		if _, ok := SeverityOf(c); !ok {
			t.Errorf("code %q has no declared severity", c)
		}
	}
}
