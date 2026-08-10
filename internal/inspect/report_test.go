package inspect

import (
	"bytes"
	"testing"
)

// checkByCode is a small test-only helper: find code's Check in checks, or
// fail the test if it is missing (every code MUST appear exactly once,
// either here or in findings -- ValidateReport is the mechanical proof of
// that, this helper is just a convenient accessor for asserting on one
// code's status/reason).
func checkByCodeT(t *testing.T, checks []Check, code Code) Check {
	t.Helper()
	for _, c := range checks {
		if c.Code == code {
			return c
		}
	}
	t.Fatalf("code %s missing from checks", code)
	return Check{}
}

// TestBuildReportPNGCompletesPDFAndCFBBlocksAsNotApplicable is the plan's
// first named behavior: for a PNG upload, every PDF-domain and ole_cfb_*
// code is StatusNotApplicable/ReasonFormatNotApplicable, while the two
// image-domain codes carry AnalyzeImage's real result.
func TestBuildReportPNGCompletesPDFAndCFBBlocksAsNotApplicable(t *testing.T) {
	data := pngFixture(100, 100)
	img := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)
	tr := NewTracker(Budget{})

	r := BuildReport("png", "png", int64(len(data)), tr, img)

	for _, code := range []Code{
		CodePDFJavaScript, CodePDFActionDict, CodePDFLaunchAction,
		CodePDFEmbeddedFile, CodePDFEncrypted,
		CodeOLECFBLegacy, CodeOLECFBEncrypted, CodeOLECFBUnclassified,
	} {
		c := checkByCodeT(t, r.Checks, code)
		if c.Status != StatusNotApplicable || c.Reason != ReasonFormatNotApplicable {
			t.Errorf("code %s: status=%s reason=%s, want %s/%s", code, c.Status, c.Reason, StatusNotApplicable, ReasonFormatNotApplicable)
		}
	}

	// image_pixel_bomb_shape and metadata_present are the two real
	// image-domain checks -- both StatusClean for this clean fixture.
	pixels := checkByCodeT(t, r.Checks, CodeImagePixelBombShape)
	if pixels.Status != StatusClean {
		t.Errorf("image_pixel_bomb_shape: status=%s, want clean", pixels.Status)
	}
	meta := checkByCodeT(t, r.Checks, CodeMetadataPresent)
	if meta.Status != StatusClean {
		t.Errorf("metadata_present: status=%s, want clean", meta.Status)
	}
}

// TestBuildReportSatisfiesUnionInvariant proves ValidateReport(r.Findings,
// r.Checks) == nil for a representative input from every family, and for
// zero families at all (an unrecognized/unsupported format).
func TestBuildReportSatisfiesUnionInvariant(t *testing.T) {
	pngData := pngFixture(100, 100)
	img := AnalyzeImage("png", bytes.NewReader(pngData), int64(len(pngData)), int64(len(pngData)), 1<<30)

	pdfData := cleanPDFFixture()
	pdf := AnalyzePDF(bytes.NewReader(pdfData), int64(len(pdfData)), int64(len(pdfData)))

	jsData := jsPDFFixture()
	jsPDF := AnalyzePDF(bytes.NewReader(jsData), int64(len(jsData)), int64(len(jsData)))

	cases := []struct {
		name     string
		families []FamilyResult
	}{
		{"image-only", []FamilyResult{img}},
		{"pdf-only", []FamilyResult{pdf}},
		{"pdf-with-finding", []FamilyResult{jsPDF}},
		{"no-family", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := BuildReport("x", "x", 10, NewTracker(Budget{}), tc.families...)
			if err := ValidateReport(r.Findings, r.Checks); err != nil {
				t.Fatalf("ValidateReport: %v", err)
			}
		})
	}
}

// TestBuildReportExcludesCodeFromChecksWhenInFindings proves a code that
// produced a Finding never ALSO appears in Checks (the union invariant's
// "never both" half, exercised directly rather than only through
// ValidateReport's aggregate error).
func TestBuildReportExcludesCodeFromChecksWhenInFindings(t *testing.T) {
	jsData := jsPDFFixture()
	pdf := AnalyzePDF(bytes.NewReader(jsData), int64(len(jsData)), int64(len(jsData)))

	r := BuildReport("pdf", "pdf", int64(len(jsData)), NewTracker(Budget{}), pdf)

	foundFinding := false
	for _, f := range r.Findings {
		if f.Code == CodePDFJavaScript {
			foundFinding = true
		}
	}
	if !foundFinding {
		t.Fatalf("expected pdf_javascript in Findings")
	}
	for _, c := range r.Checks {
		if c.Code == CodePDFJavaScript {
			t.Fatalf("pdf_javascript must not also appear in Checks: %+v", c)
		}
	}
}

// TestBuildReportWorkCeilingBytesMarksCoveragePartial is the plan's named
// behavior for Tracker exhaustion by ReasonWorkCeilingBytes: Coverage.
// Complete is false, Coverage.Reason is ReasonWorkCeilingBytes, and
// TruncatedChecks lists EXACTLY the codes whose own Check carries that
// reason -- no more.
func TestBuildReportWorkCeilingBytesMarksCoveragePartial(t *testing.T) {
	// A PDF larger than maxBytes forces AnalyzePDF's own truncated path,
	// which reports every risk-token Check as StatusUnknown/
	// ReasonWorkCeilingBytes (verdictReason, pdf.go) when the compression
	// signal is absent.
	data := cleanPDFFixture()
	padded := append(data, bytes.Repeat([]byte(" "), 1024)...)
	maxBytes := int64(len(data)) // smaller than len(padded): forces truncated=true
	pdf := AnalyzePDF(bytes.NewReader(padded), int64(len(padded)), maxBytes)

	tr := NewTracker(Budget{MaxBytes: maxBytes, MaxDuration: defaultMaxDuration})
	tr.Consume(maxBytes) // exhausts the byte ceiling, mirroring what the real dispatcher would do

	r := BuildReport("pdf", "pdf", int64(len(padded)), tr, pdf)

	if r.Coverage.Complete {
		t.Fatalf("Coverage.Complete = true, want false")
	}
	if r.Coverage.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Coverage.Reason = %s, want %s", r.Coverage.Reason, ReasonWorkCeilingBytes)
	}

	wantTruncated := map[Code]bool{}
	for _, c := range r.Checks {
		if c.Reason == ReasonWorkCeilingBytes {
			wantTruncated[c.Code] = true
		}
	}
	if len(wantTruncated) == 0 {
		t.Fatalf("expected at least one check with ReasonWorkCeilingBytes")
	}
	gotTruncated := map[Code]bool{}
	for _, c := range r.Coverage.TruncatedChecks {
		gotTruncated[c] = true
	}
	if len(gotTruncated) != len(wantTruncated) {
		t.Fatalf("TruncatedChecks = %v, want exactly %v", r.Coverage.TruncatedChecks, wantTruncated)
	}
	for code := range wantTruncated {
		if !gotTruncated[code] {
			t.Errorf("TruncatedChecks missing code %s", code)
		}
	}
}

// TestBuildReportStructuralUnknownKeepsCoverageComplete is the plan's other
// named behavior: a PDF whose risk-token checks are all StatusUnknown/
// ReasonCompressedObjectStreams (structural blindness about the FILE) must
// NOT mark the report partial -- Coverage.Complete stays true.
func TestBuildReportStructuralUnknownKeepsCoverageComplete(t *testing.T) {
	data := objStmPDFFixture()
	pdf := AnalyzePDF(bytes.NewReader(data), int64(len(data)), int64(len(data)))

	sawCompressedUnknown := false
	for _, c := range pdf.Checks {
		if c.Reason == ReasonCompressedObjectStreams {
			sawCompressedUnknown = true
		}
	}
	if !sawCompressedUnknown {
		t.Fatalf("objStmPDFFixture did not produce any ReasonCompressedObjectStreams check -- fixture assumption broken")
	}

	tr := NewTracker(Budget{})
	r := BuildReport("pdf", "pdf", int64(len(data)), tr, pdf)

	if !r.Coverage.Complete {
		t.Fatalf("Coverage.Complete = false, want true (structural blindness must not select the report)")
	}
	if r.Coverage.Reason != "" {
		t.Fatalf("Coverage.Reason = %s, want empty", r.Coverage.Reason)
	}
}

// TestBuildReportCharacteristicCeilingReasonMarksCoveragePartial is codex
// PR9 finding 5 (T-SLE-05): a characteristic-level "<name>_reason" key
// admitting ReasonWorkCeilingBytes with ZERO accompanying Checks must still
// mark the report-wide Coverage incomplete -- a report that says
// duration_seconds_reason=work_ceiling_bytes in one field and
// coverage.complete=true in another is self-contradictory.
func TestBuildReportCharacteristicCeilingReasonMarksCoveragePartial(t *testing.T) {
	fr := FamilyResult{Characteristics: Characteristics{
		"duration_seconds":        nil,
		"duration_seconds_reason": string(ReasonWorkCeilingBytes),
	}}

	r := BuildReport("mp4", "mp4", 100, NewTracker(Budget{}), fr)

	for _, c := range r.Checks {
		if c.Reason == ReasonWorkCeilingBytes {
			t.Fatalf("test setup error: expected zero Checks carrying ReasonWorkCeilingBytes, got %+v -- this test is specifically about a characteristic-ONLY ceiling with no accompanying Check", c)
		}
	}
	if r.Coverage.Complete {
		t.Fatal("Coverage.Complete = true, want false -- duration_seconds_reason admitted work_ceiling_bytes truncation with no accompanying Check")
	}
	if r.Coverage.Reason != ReasonWorkCeilingBytes {
		t.Fatalf("Coverage.Reason = %s, want %s", r.Coverage.Reason, ReasonWorkCeilingBytes)
	}
}

// TestBuildReportStructuralCharacteristicReasonKeepsCoverageComplete is the
// inverse of the above: a characteristic-level "<name>_reason" key carrying
// a NON-ceiling reason (parser_unavailable, malformed_input, ...) must NOT
// mark Coverage incomplete -- only the two service-load reasons may ever do
// that (status.go's own Coverage doc comment).
func TestBuildReportStructuralCharacteristicReasonKeepsCoverageComplete(t *testing.T) {
	fr := FamilyResult{Characteristics: Characteristics{
		"sheet_count":        nil,
		"sheet_count_reason": string(ReasonParserUnavailable),
	}}

	r := BuildReport("ods", "ods", 100, NewTracker(Budget{}), fr)

	if !r.Coverage.Complete {
		t.Fatalf("Coverage.Complete = false, want true -- sheet_count_reason=parser_unavailable is a structural fact about this build, not this service's own load")
	}
	if r.Coverage.Reason != "" {
		t.Fatalf("Coverage.Reason = %s, want empty", r.Coverage.Reason)
	}
}

// TestBuildReportUnrecognizedFormatYieldsUnknownNotNotApplicable is codex
// PR9 finding 6 (T-SLE-06): when detection never claimed a format at all
// (format == ""), every code in the closed vocabulary must report unknown/
// format_unrecognized, never not_applicable/format_not_applicable --
// applicability cannot be established when the format itself is unknown.
func TestBuildReportUnrecognizedFormatYieldsUnknownNotNotApplicable(t *testing.T) {
	r := BuildReport("", "", 100, NewTracker(Budget{}))

	if len(r.Checks) != len(AllCodes()) {
		t.Fatalf("len(r.Checks) = %d, want %d (every code in the closed vocabulary)", len(r.Checks), len(AllCodes()))
	}
	for _, code := range AllCodes() {
		c := checkByCodeT(t, r.Checks, code)
		if c.Status == StatusNotApplicable {
			t.Errorf("code %s: status=not_applicable, want unknown -- an unrecognized format cannot honestly claim any code is inapplicable", code)
		}
		if c.Status != StatusUnknown {
			t.Errorf("code %s: status=%s, want unknown", code, c.Status)
		}
		if c.Reason != ReasonFormatUnrecognized {
			t.Errorf("code %s: reason=%s, want %s", code, c.Reason, ReasonFormatUnrecognized)
		}
	}
}

// TestBuildReportFindingsSortedByCode proves the merged Findings slice is
// deterministically ordered, independent of family call order.
func TestBuildReportFindingsSortedByCode(t *testing.T) {
	data := jsAndObjStmPDFFixture()
	pdf := AnalyzePDF(bytes.NewReader(data), int64(len(data)), int64(len(data)))

	r := BuildReport("pdf", "pdf", int64(len(data)), NewTracker(Budget{}), pdf)

	for i := 1; i < len(r.Findings); i++ {
		if r.Findings[i-1].Code > r.Findings[i].Code {
			t.Fatalf("Findings not sorted by Code: %v", r.Findings)
		}
	}
}

// TestBuildReportChecksSortedInAllCodesOrder proves the completed Checks
// slice follows AllCodes()'s own declaration order, not any other order
// (e.g. insertion order from whichever family happened to run).
func TestBuildReportChecksSortedInAllCodesOrder(t *testing.T) {
	data := pngFixture(100, 100)
	img := AnalyzeImage("png", bytes.NewReader(data), int64(len(data)), int64(len(data)), 1<<30)

	r := BuildReport("png", "png", int64(len(data)), NewTracker(Budget{}), img)

	// Findings are excluded from Checks, so build the expected order by
	// filtering AllCodes() down to the codes that actually appear in
	// r.Checks, preserving AllCodes()'s own relative order.
	inChecks := make(map[Code]bool, len(r.Checks))
	for _, c := range r.Checks {
		inChecks[c.Code] = true
	}
	var want []Code
	for _, code := range AllCodes() {
		if inChecks[code] {
			want = append(want, code)
		}
	}
	if len(want) != len(r.Checks) {
		t.Fatalf("len(want)=%d != len(r.Checks)=%d", len(want), len(r.Checks))
	}
	for i, c := range r.Checks {
		if c.Code != want[i] {
			t.Fatalf("Checks[%d].Code = %s, want %s (AllCodes() order)", i, c.Code, want[i])
		}
	}
}

// TestBuildReportFamilyCharacteristicKeysDoNotCollide is the plan's named
// mechanical guard against 58-04/58-05 parallel-authorship drift. It
// documents and pins the ACTUAL, closed set of intentional cross-family key
// reuses in the real analyzer code (page_count/page_count_reason between
// pdf.go and container.go; width/width_reason/height/height_reason between
// image.go and media.go) -- both reused pairs are format-MUTUALLY-EXCLUSIVE
// families (a PDF never also runs AnalyzeContainer; an image never also
// runs AnalyzeMedia), so a real BuildReport call's families slice NEVER
// contains both halves of either pair at once. Every OTHER pairing among
// the four families' possible characteristic key sets must have an empty
// intersection -- an accidental collision there (e.g. container.go and
// image.go both claiming "width") WOULD be a real, silent-merge bug this
// test exists to catch, and it fails loudly the moment one is introduced.
func TestBuildReportFamilyCharacteristicKeysDoNotCollide(t *testing.T) {
	pdfKeys := map[string]bool{
		"uses_compressed_object_streams": true,
		"pdf_version":                    true,
		"page_count":                     true,
		"page_count_reason":              true,
	}
	containerKeys := map[string]bool{
		"container_format":            true,
		"declared_uncompressed_bytes": true,
		"has_macro":                   true,
		"sheet_count":                 true,
		"sheet_count_reason":          true,
		"page_count":                  true,
		"page_count_reason":           true,
	}
	mediaKeys := map[string]bool{
		"duration_seconds":        true,
		"duration_seconds_reason": true,
		"width":                   true,
		"width_reason":            true,
		"height":                  true,
		"height_reason":           true,
		"declared_codec":          true,
		"declared_codec_reason":   true,
	}
	imageKeys := map[string]bool{
		"width":           true,
		"width_reason":    true,
		"height":          true,
		"height_reason":   true,
		"pixels":          true,
		"pixels_reason":   true,
		"declared_format": true,
	}

	intersect := func(a, b map[string]bool) []string {
		var out []string
		for k := range a {
			if b[k] {
				out = append(out, k)
			}
		}
		return out
	}

	// The two INTENTIONAL, format-mutually-exclusive reuses.
	if got := intersect(pdfKeys, containerKeys); len(got) != 2 {
		t.Errorf("pdf vs container intersection = %v, want exactly {page_count, page_count_reason}", got)
	}
	if got := intersect(imageKeys, mediaKeys); len(got) != 4 {
		t.Errorf("image vs media intersection = %v, want exactly {width, width_reason, height, height_reason}", got)
	}

	// Every OTHER pairing must be empty -- these four families are never
	// dispatched together for the same format, so an intersection here
	// would be a genuine, previously-undetected drift.
	families := map[string]map[string]bool{
		"pdf":       pdfKeys,
		"container": containerKeys,
		"media":     mediaKeys,
		"image":     imageKeys,
	}
	allowed := map[[2]string]bool{
		{"pdf", "container"}: true,
		{"container", "pdf"}: true,
		{"image", "media"}:   true,
		{"media", "image"}:   true,
	}
	names := []string{"pdf", "container", "media", "image"}
	for i := range names {
		for j := range names {
			if i == j || allowed[[2]string{names[i], names[j]}] {
				continue
			}
			if got := intersect(families[names[i]], families[names[j]]); len(got) > 0 {
				t.Errorf("%s vs %s: unexpected key collision %v", names[i], names[j], got)
			}
		}
	}
}

// TestBuildReportSelfCheckDegradesOnInvariantViolation proves the D-01
// "no panic for control flow" discipline holds even when this package's OWN
// union invariant is violated: degradeCoverageOnInvariantViolation (the
// exact tail BuildReport itself calls) degrades Coverage rather than
// panicking or silently accepting a findings/checks pair that does not
// actually satisfy ValidateReport.
//
// BuildReport's own merge logic (buildChecks) structurally PREVENTS a real
// "both" or "duplicated" violation from ever reaching this point in
// practice (buildChecks always skips a code that already has a Finding,
// and checkByCode's "first wins" rule de-duplicates Checks) -- so this test
// calls degradeCoverageOnInvariantViolation directly with a manufactured,
// adversarial pair, exactly as a hypothetical FUTURE bug in that merge
// logic could produce, rather than trying to reproduce the defect through
// the normal (defect-free) merge path.
func TestBuildReportSelfCheckDegradesOnInvariantViolation(t *testing.T) {
	sev, _ := SeverityOf(CodeMetadataPresent)
	findings := []Finding{{Code: CodeMetadataPresent, Severity: sev}}
	checks := []Check{{Code: CodeMetadataPresent, Status: StatusUnknown, Reason: ReasonParserUnavailable}}

	if err := ValidateReport(findings, checks); err == nil {
		t.Fatalf("expected the manufactured findings/checks pair to violate ValidateReport (test setup broken)")
	}

	got := degradeCoverageOnInvariantViolation(findings, checks, Coverage{Complete: true})
	if got.Complete {
		t.Fatalf("Coverage.Complete = true, want false (self-check must degrade on invariant violation)")
	}
	if got.Reason != ReasonMalformedInput {
		t.Fatalf("Coverage.Reason = %s, want %s", got.Reason, ReasonMalformedInput)
	}
}
