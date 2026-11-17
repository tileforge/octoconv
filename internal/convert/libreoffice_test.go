package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePDF(t *testing.T) {
	dir := t.TempDir()

	validPath := filepath.Join(dir, "valid.pdf")
	if err := os.WriteFile(validPath, []byte("%PDF-1.6\n%rest of content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePDF(validPath); err != nil {
		t.Errorf("validatePDF(valid) = %v, want nil", err)
	}

	emptyPath := filepath.Join(dir, "empty.pdf")
	if err := os.WriteFile(emptyPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePDF(emptyPath); err == nil {
		t.Error("validatePDF(empty) = nil, want error")
	}

	wrongPath := filepath.Join(dir, "wrong.pdf")
	if err := os.WriteFile(wrongPath, []byte("not a pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePDF(wrongPath); err == nil {
		t.Error("validatePDF(wrong magic) = nil, want error")
	}

	// A sub-magic-length file must surface as the terminal missing-magic
	// signature, not a transient-looking "unexpected EOF" (D-04).
	tinyPath := filepath.Join(dir, "tiny.pdf")
	if err := os.WriteFile(tinyPath, []byte("%PD"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := validatePDF(tinyPath)
	if err == nil {
		t.Fatal("validatePDF(tiny) = nil, want error")
	}
	if !strings.Contains(err.Error(), "output missing %PDF- magic bytes") {
		t.Errorf("validatePDF(tiny) = %q, want the terminal missing-magic signature", err)
	}
}

// TestPDFAFilterOptions asserts the PDF/A-2b builder forces both properties
// Pitfall 7 requires (SelectPdfVersion + EmbedStandardFonts:true), and that
// empty opts produce no filter suffix at all.
func TestPDFAFilterOptions(t *testing.T) {
	suffix, isPDFA := PDFAFilterOptions(DocOpts{PDFProfile: "pdf/a-2b"})
	if !isPDFA {
		t.Fatal("PDFAFilterOptions(pdf/a-2b) isPDFA = false, want true")
	}
	if !strings.Contains(suffix, "SelectPdfVersion") {
		t.Errorf("PDFAFilterOptions(pdf/a-2b) suffix = %q, want it to contain SelectPdfVersion", suffix)
	}
	if !strings.Contains(suffix, "EmbedStandardFonts") || !strings.Contains(suffix, "true") {
		t.Errorf("PDFAFilterOptions(pdf/a-2b) suffix = %q, want it to force EmbedStandardFonts:true (Pitfall 7)", suffix)
	}

	suffix, isPDFA = PDFAFilterOptions(DocOpts{})
	if isPDFA || suffix != "" {
		t.Errorf("PDFAFilterOptions(empty opts) = (%q, %v), want (\"\", false)", suffix, isPDFA)
	}
}

// TestDocOptsInjectionResistance is the mandatory success-criterion-1
// artifact (D-07, Pitfall 9): it proves that no adversarial byte sequence in
// a client-supplied opts JSON value can ever reach the soffice filter
// argument. Every adversarial input is either rejected outright by
// ParseDocOpts (the common case, since the allow-list is a single exact
// string), or -- if it somehow parses -- must produce a filter suffix
// byte-for-byte identical to the clean pdf/a-2b case, with none of the
// adversarial tokens present.
func TestDocOptsInjectionResistance(t *testing.T) {
	cleanSuffix, cleanIsPDFA := PDFAFilterOptions(DocOpts{PDFProfile: "pdf/a-2b"})
	if !cleanIsPDFA || cleanSuffix == "" {
		t.Fatal("clean pdf/a-2b case must produce a non-empty PDF/A suffix; test setup is broken")
	}

	adversarial := []string{
		// Attempts to break out of the JSON string value and inject a
		// second filter property directly.
		`{"pdf_profile":"pdf/a-2b\",\"EncryptFile\":true,\"x\":\""}`,
		// Attempts to smuggle a different UNO export filter name onto the
		// end of the enum value.
		`{"pdf_profile":"pdf/a-2b:calc_pdf_Export"}`,
		// A structurally valid but wholly unknown key -- must be rejected
		// by DisallowUnknownFields, not silently ignored.
		`{"EncryptFile":true}`,
		// A valid enum value alongside an unknown key -- the whole decode
		// must fail, not just the unknown key.
		`{"pdf_profile":"pdf/a-2b","EncryptFile":true}`,
		// Case-variant of the only allowed value -- must NOT be treated as
		// equivalent to the exact allow-listed string.
		`{"pdf_profile":"PDF/A-2B"}`,
	}

	for _, raw := range adversarial {
		t.Run(raw, func(t *testing.T) {
			opts, err := ParseDocOpts([]byte(raw))
			if err != nil {
				// Rejected before ever reaching the builder: client bytes
				// never got a chance to influence the argv/filter string.
				return
			}
			suffix, isPDFA := PDFAFilterOptions(opts)
			if suffix != cleanSuffix || isPDFA != cleanIsPDFA {
				t.Errorf("adversarial opts %q parsed successfully and produced a DIFFERENT filter suffix (%q, %v) than the clean pdf/a-2b case (%q, %v) -- this proves client bytes reached the soffice filter argument", raw, suffix, isPDFA, cleanSuffix, cleanIsPDFA)
			}
			for _, token := range []string{"EncryptFile", "calc_pdf_Export"} {
				if strings.Contains(suffix, token) {
					t.Errorf("adversarial token %q leaked into filter suffix %q -- this proves client bytes reached the soffice filter argument", token, suffix)
				}
			}
		})
	}
}

// TestParseDocOptsStrictness proves the parser rejects the laxities
// json.Decoder.Decode alone would accept (review WR-01): trailing data after
// the first JSON value (a smuggled second object or plain garbage), a
// non-object top-level value (`null` decodes as a valid zero struct), and
// duplicate keys (silent "last wins" would resurrect a rejected value) --
// while valid inputs keep parsing exactly as before.
func TestParseDocOptsStrictness(t *testing.T) {
	rejected := []string{
		// Trailing second object -- Decode would silently drop it.
		`{"pdf_profile":"pdf/a-2b"}{"EncryptFile":true}`,
		// Trailing garbage bytes.
		`{"pdf_profile":"pdf/a-2b"} garbage`,
		// Non-object top-level values -- `null` previously yielded a valid
		// zero DocOpts.
		`null`,
		`"pdf/a-2b"`,
		`[]`,
		// Duplicate key, both orderings -- "last wins" must never resurrect
		// a value the allow-list rejected.
		`{"pdf_profile":"evil","pdf_profile":"pdf/a-2b"}`,
		`{"pdf_profile":"pdf/a-2b","pdf_profile":"evil"}`,
	}
	for _, raw := range rejected {
		if _, err := ParseDocOpts([]byte(raw)); err == nil {
			t.Errorf("ParseDocOpts(%q) = nil error, want rejection", raw)
		}
	}

	// Valid inputs are unaffected by the strictness checks.
	o, err := ParseDocOpts([]byte(`{"pdf_profile":"pdf/a-2b"}`))
	if err != nil || o.PDFProfile != pdfProfileA2b {
		t.Errorf("ParseDocOpts(valid profile) = (%+v, %v), want pdf/a-2b, nil", o, err)
	}
	o, err = ParseDocOpts([]byte(`{}`))
	if err != nil || o.PDFProfile != "" {
		t.Errorf("ParseDocOpts({}) = (%+v, %v), want zero DocOpts, nil", o, err)
	}
	o, err = ParseDocOpts([]byte(" {\n  \"pdf_profile\": \"pdf/a-2b\"\n } \n"))
	if err != nil || o.PDFProfile != pdfProfileA2b {
		t.Errorf("ParseDocOpts(whitespace-padded valid profile) = (%+v, %v), want pdf/a-2b, nil", o, err)
	}
}

func TestRegistryLibreOfficePairs(t *testing.T) {
	// Default registry is populated by converters.go init().
	if !Default.Supports("docx", "pdf") {
		t.Error("expected docx->pdf to be supported")
	}
	if !Default.Supports("DOCX", ".pdf") {
		t.Error("expected DOCX->.pdf (aliased/cased) to be supported")
	}
	for _, from := range []string{"odt", "xlsx", "ods", "pptx", "odp"} {
		if !Default.Supports(from, "pdf") {
			t.Errorf("expected %s->pdf to be supported", from)
		}
	}
	if Default.Supports("pdf", "docx") {
		t.Error("pdf->docx must not be supported (one-directional only)")
	}
	if Default.Supports("docx", "docx") {
		t.Error("identity pair docx->docx must not be registered")
	}
	if _, ok := Default.Lookup("odp", "pdf"); !ok {
		t.Error("expected a converter for odp->pdf")
	}

	// D-01: the 6 cross pairs are supported, including a cased/aliased
	// variant.
	crossCases := [][2]string{
		{"docx", "odt"}, {"odt", "docx"},
		{"xlsx", "ods"}, {"ods", "xlsx"},
		{"pptx", "odp"}, {"odp", "pptx"},
	}
	for _, p := range crossCases {
		if !Default.Supports(p[0], p[1]) {
			t.Errorf("expected %s->%s to be supported", p[0], p[1])
		}
	}
	if !Default.Supports(".DOCX", "ODT") {
		t.Error("expected .DOCX->ODT (aliased/cased) to be supported")
	}

	// No cross-family pairs are registered.
	forbidden := [][2]string{
		{"docx", "ods"}, {"docx", "odp"},
		{"xlsx", "odt"}, {"xlsx", "odp"},
		{"pptx", "odt"}, {"pptx", "ods"},
	}
	for _, p := range forbidden {
		if Default.Supports(p[0], p[1]) {
			t.Errorf("forbidden cross-family pair %s->%s must not be supported", p[0], p[1])
		}
	}

	// Identity pairs remain unregistered for every document format.
	for _, f := range documentFormats {
		if Default.Supports(f, f) {
			t.Errorf("identity pair %s->%s must not be registered", f, f)
		}
	}
}
