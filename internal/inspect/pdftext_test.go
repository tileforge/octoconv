package inspect

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// pdfCorpusPath resolves a fixture from the committed corpus (Plan 59-01).
// Expected values come from that directory's PROVENANCE.md, which records the
// live `pdftotext … | tr -d '[:space:]' | wc -c` result for each file.
func pdfCorpusPath(name string) string { return filepath.Join("testdata", "pdf", name) }

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestTextLayerVerdictOnCorpus is the table the whole detector answers to.
//
// Every expectation is the number PROVENANCE.md records for that fixture from
// a live pdftotext run, so this table is a cross-check against an independent
// implementation rather than against my own reasoning about PDF.
//
// The budget is the DEFAULT one throughout. That matters most for the two
// image-only fixtures: they must reach "absent" with the ordinary allowance,
// which is only possible because image XObjects are never inflated. Run this
// table with a generous custom budget and it would pass while the feature was
// broken for every scan in production.
func TestTextLayerVerdictOnCorpus(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		wantValue  any
		wantReason Reason
	}{
		{"born_digital", filepath.Join("..", "convert", "testdata", "ocr", "born_digital.pdf"), TextLayerPresent, ""},
		{"objstm_born_digital", pdfCorpusPath("objstm_born_digital.pdf"), TextLayerPresent, ""},
		{"uncompressed_text", pdfCorpusPath("uncompressed_text.pdf"), TextLayerPresent, ""},
		{"embedded_font", pdfCorpusPath("embedded_font.pdf"), TextLayerPresent, ""},
		{"multipage_image_only", filepath.Join("..", "convert", "testdata", "ocr", "multipage_image_only.pdf"), TextLayerAbsent, ""},
		{"multipage_text_is_an_image", filepath.Join("..", "convert", "testdata", "ocr", "multipage_text.pdf"), TextLayerAbsent, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := readFixture(t, tc.path)
			tr := NewTracker(Budget{}) // default budget, deliberately
			res := AnalyzePDF(bytes.NewReader(data), int64(len(data)), defaultMaxBytesForTest(), tr)

			got := res.Characteristics["text_layer"]
			if got != tc.wantValue {
				t.Fatalf("text_layer = %v, want %v", got, tc.wantValue)
			}
			gotReason, _ := res.Characteristics["text_layer_reason"].(string)
			if Reason(gotReason) != tc.wantReason {
				t.Fatalf("text_layer_reason = %q, want %q", gotReason, tc.wantReason)
			}

			r := BuildReport("pdf", "pdf", int64(len(data)), tr, res)
			if !r.Coverage.Complete {
				t.Fatalf("Coverage.Complete = false (reason %q) -- a small valid PDF under the default budget must "+
					"never report selective coverage; if this fires for an image-only fixture the image-skip is broken "+
					"and the field has gone silent on the very files it exists for", r.Coverage.Reason)
			}
		})
	}
}

// defaultMaxBytesForTest mirrors what dispatchInspectFamilies hands an analyzer
// under the default Budget, so the corpus table exercises production sizing
// rather than a number chosen to make it pass.
func defaultMaxBytesForTest() int64 { return NewTracker(Budget{}).Remaining() }

// TestObjStmPDFReportsTextLayerWhileEveryCheckIsUnknown is the empirical half
// of D-01, and the single most important test in this file.
//
// One report body must simultaneously say two things that look contradictory
// and are not: every PDF risk check is unknown/compressed_object_streams,
// because objects can hide inside an /ObjStm and a byte scan cannot see them --
// AND text_layer is present, because ISO 32000 §7.5.7 forbids stream objects
// from living inside an /ObjStm, and a text layer lives in content streams.
//
// Asserting them separately would prove less than asserting them together: the
// claim is precisely that the two coexist.
func TestObjStmPDFReportsTextLayerWhileEveryCheckIsUnknown(t *testing.T) {
	data := readFixture(t, pdfCorpusPath("objstm_born_digital.pdf"))
	tr := NewTracker(Budget{})
	res := AnalyzePDF(bytes.NewReader(data), int64(len(data)), defaultMaxBytesForTest(), tr)

	if compressed, _ := res.Characteristics["uses_compressed_object_streams"].(bool); !compressed {
		t.Fatal("fixture assumption broken: objstm_born_digital.pdf no longer reports uses_compressed_object_streams")
	}

	blind := 0
	for _, c := range res.Checks {
		if c.Reason == ReasonCompressedObjectStreams {
			blind++
		}
		if c.Status == StatusClean {
			t.Errorf("Check(%s) = clean on a PDF with compressed object streams -- the /ObjStm downgrade must still "+
				"apply to every risk code; the text-layer exception is about ONE characteristic, not a general licence", c.Code)
		}
	}
	if blind == 0 {
		t.Fatal("no check carried compressed_object_streams -- the /ObjStm downgrade stopped firing")
	}

	if got := res.Characteristics["text_layer"]; got != TextLayerPresent {
		t.Fatalf("text_layer = %v, want %q -- this is the fixture that turns D-01's ISO 32000 argument into an "+
			"observation, and it is the one file where losing it would go unnoticed everywhere else", got, TextLayerPresent)
	}
}

// TestTextLayerDegradesRatherThanClaimingAbsence walks each way the scan can
// fail to see everything and requires a null verdict with a reason.
//
// D-09 in test form: "absent" is a claim about the WHOLE file and may only be
// made by a scan that saw the whole file. "Absent whenever no text was found"
// is the exact defect eight rounds of PR #9 review removed elsewhere here.
func TestTextLayerDegradesRatherThanClaimingAbsence(t *testing.T) {
	cases := []struct {
		name       string
		state      pdfTextScanState
		truncated  bool
		encrypted  bool
		wantReason Reason
	}{
		{"budget exhausted", pdfTextScanState{covered: true, budgetHit: true}, false, false, ReasonWorkCeilingBytes},
		{"window truncated", pdfTextScanState{covered: true}, true, false, ReasonWorkCeilingBytes},
		{"stream not decodable", pdfTextScanState{covered: false}, false, false, ReasonMalformedInput},
		{"encrypted", pdfTextScanState{covered: true}, false, true, ReasonMalformedInput},
		{"encrypted outranks a lost coverage", pdfTextScanState{covered: false}, false, true, ReasonMalformedInput},
		{"budget outranks a lost coverage", pdfTextScanState{covered: false, budgetHit: true}, false, false, ReasonWorkCeilingBytes},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, reason := pdfTextLayerVerdict(tc.state, tc.truncated, tc.encrypted)
			if value != nil {
				t.Fatalf("text_layer = %v, want nil -- a scan that did not see everything may not answer %q", value, value)
			}
			if reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

// TestTextLayerPresenceSurvivesEveryDegradation is the asymmetry D-09 demands,
// and it is easy to get backwards.
//
// Finding text is a POSITIVE observation. It is not weakened by having failed
// to see something else -- the same rule pdf.go applies to findings, where a
// discovered /JavaScript stays a Finding no matter how blind the rest of the
// scan was. Only the ABSENCE claim degrades.
func TestTextLayerPresenceSurvivesEveryDegradation(t *testing.T) {
	plenty := pdfUsableTextLayerThreshold + 1
	for _, tc := range []struct {
		name  string
		state pdfTextScanState
		trunc bool
		enc   bool
	}{
		{"budget hit", pdfTextScanState{nonWhitespace: plenty, budgetHit: true}, false, false},
		{"not covered", pdfTextScanState{nonWhitespace: plenty, covered: false}, false, false},
		{"truncated", pdfTextScanState{nonWhitespace: plenty, covered: true}, true, false},
		{"encrypted", pdfTextScanState{nonWhitespace: plenty, covered: true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, reason := pdfTextLayerVerdict(tc.state, tc.trunc, tc.enc)
			if value != TextLayerPresent {
				t.Fatalf("text_layer = %v, want %q -- text that WAS seen stays seen", value, TextLayerPresent)
			}
			if reason != "" {
				t.Fatalf("reason = %q, want empty alongside a present verdict", reason)
			}
		})
	}
}

// TestTextLayerThresholdIsExactlyOverNotAtOrOver pins the boundary, matching
// internal/convert's own strictly-greater comparison since Phase 43.
func TestTextLayerThresholdIsExactlyOverNotAtOrOver(t *testing.T) {
	at, _ := pdfTextLayerVerdict(pdfTextScanState{nonWhitespace: pdfUsableTextLayerThreshold, covered: true}, false, false)
	if at != TextLayerAbsent {
		t.Fatalf("exactly at the threshold = %v, want %q (strictly greater, as in internal/convert)", at, TextLayerAbsent)
	}
	over, _ := pdfTextLayerVerdict(pdfTextScanState{nonWhitespace: pdfUsableTextLayerThreshold + 1, covered: true}, false, false)
	if over != TextLayerPresent {
		t.Fatalf("one over the threshold = %v, want %q", over, TextLayerPresent)
	}
}

// TestTextLayerThresholdMatchesConverter is what keeps the duplicated constant
// honest.
//
// internal/inspect may not import internal/convert (findings.go's layering
// note), so the threshold exists twice. D-04 requires the two detectors to
// share a unit AND a threshold, because D-10 measures the divergence between
// them and two different scales cannot be compared at all. This test reads
// BOTH declarations out of source -- the same mechanism
// TestAllReasonsCoversEveryDeclaredConstant uses -- so a change to either side
// alone fails loudly instead of quietly making the divergence table
// meaningless.
func TestTextLayerThresholdMatchesConverter(t *testing.T) {
	const converterPath = "../convert/ocrguards.go"

	src, err := os.ReadFile(converterPath)
	if err != nil {
		t.Fatalf("read %s: %v", converterPath, err)
	}
	re := regexp.MustCompile(`ocrUsableTextLayerThreshold\s*=\s*(\d+)`)
	m := re.FindSubmatch(src)
	if m == nil {
		t.Fatalf("could not find ocrUsableTextLayerThreshold in %s -- it was renamed or restructured, and this parity "+
			"check must be updated with it rather than deleted", converterPath)
	}
	want, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("parse threshold %q: %v", m[1], err)
	}
	if want != pdfUsableTextLayerThreshold {
		t.Fatalf("internal/convert threshold = %d, internal/inspect threshold = %d -- the two detectors must share a "+
			"threshold, or the divergence D-10 measures between them is measured on different scales", want, pdfUsableTextLayerThreshold)
	}
}

// TestTextLayerBudgetExhaustionIsUnknownNotAbsent gives the scan an allowance
// far too small for the fixture's own content streams and requires an honest
// ceiling rather than a confident "absent".
func TestTextLayerBudgetExhaustionIsUnknownNotAbsent(t *testing.T) {
	data := readFixture(t, filepath.Join("..", "convert", "testdata", "ocr", "born_digital.pdf"))
	tr := NewTracker(Budget{MaxBytes: 1 << 20, MaxDuration: time.Minute})

	// maxBytes/4 is the inflate allowance, so 8 leaves 2 bytes for every
	// decompressed stream in the file.
	res := AnalyzePDF(bytes.NewReader(data), int64(len(data)), 8, tr)

	if got := res.Characteristics["text_layer"]; got == TextLayerAbsent {
		t.Fatal("text_layer = absent under an allowance too small to decode anything -- absence claimed for input " +
			"that was never examined is the defect D-09 exists to prevent")
	}
}

// TestTextLayerAbsentFromNonPDFReports pins INSP-01's own rule for this key: a
// characteristic that does not apply to the detected format is ABSENT, never
// present-with-a-null and never a not_applicable value of its own.
func TestTextLayerAbsentFromNonPDFReports(t *testing.T) {
	r := BuildReport("png", "png", 100, nil, FamilyResult{Characteristics: Characteristics{"width": 10, "height": 10}})
	if _, present := r.Characteristics["text_layer"]; present {
		t.Fatal("text_layer key present in a PNG report -- a key inapplicable to the detected format must be absent")
	}
	if _, present := r.Characteristics["text_layer_reason"]; present {
		t.Fatal("text_layer_reason key present in a PNG report")
	}
}

// TestCountTextOperatorArgBytesRequiresARealOperatorToken is 59-RESEARCH.md R6
// as a unit test: two of PDF's four text operators are the single bytes ' and
// ", and a nearby-match rule counts binary payload as shown text.
func TestCountTextOperatorArgBytesRequiresARealOperatorToken(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want int
	}{
		{"literal then Tj", "BT (hello) Tj ET", 5},
		{"literal then TJ array", "[(ab) -250 (cd)] TJ", 4},
		{"literal then quote operator", "(line) '", 4},
		{"literal then dquote operator", "1 2 (line) \"", 4},
		{"hex string then Tj", "<48656C6C6F> Tj", 10},
		{"operator-like prefix of a longer token", "(hello) Tjunk", 0},
		{"string with no operator at all", "(hello) 5 0 obj", 0},
		{"apostrophe far away inside payload", "(hello) \x00\x01\x02 zzzz '", 0},
		{"dictionary open is not a hex string", "<< /Type /Page >> (hi) Tj", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := countTextOperatorArgBytes([]byte(tc.in)); got != tc.want {
				t.Fatalf("countTextOperatorArgBytes(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestDecodePDFStreamClassification pins the three-way split from
// 59-RESEARCH.md R1, and especially that the third branch exists at all.
//
// A stream that declares a filter this build cannot decode is OPAQUE, not
// empty: it might carry text, so the report loses its right to claim absence.
// Collapsing it into "nothing found here" is the false-absence direction.
func TestDecodePDFStreamClassification(t *testing.T) {
	build := func(dict, body string) []byte {
		return []byte("1 0 obj\n" + dict + "\nstream\n" + body + "\nendstream\nendobj\n")
	}

	for _, tc := range []struct {
		name string
		buf  []byte
		want pdfStreamClass
	}{
		{"raw content stream, no filter", build("<< /Length 12 >>", "BT (hi) Tj ET"), pdfStreamScannable},
		{"image xobject", build("<< /Subtype /Image /Filter /FlateDecode >>", "\x00\x01\x02"), pdfStreamSkipped},
		{"embedded font program", build("<< /Length1 900 >>", "\x00\x01\x02"), pdfStreamSkipped},
		{"undecodable declared filter", build("<< /Filter /LZWDecode >>", "\x80\x0b\x60"), pdfStreamOpaque},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ranges, _ := scanPDFStreamRanges(tc.buf, nil)
			if len(ranges) == 0 {
				t.Fatal("fixture produced no stream range")
			}
			state := pdfTextScanState{covered: true, remaining: 1 << 20}
			_, got := decodePDFStream(tc.buf, ranges[0], nil, &state)
			if got != tc.want {
				t.Fatalf("class = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStreamDictWindowDoesNotCrossObjectBoundaries is the regression test for
// the defect the corpus caught during implementation.
//
// The lookback window originally took a flat 512 bytes and asked whether they
// contained "/Filter". On a plain scanned document the window reached back
// across an object boundary, picked up the PRECEDING object's
// "/Filter [ /FlateDecode ]", classified three raw streams as opaque, and lost
// coverage -- so every scan degraded to unknown. Every COUNT was still correct,
// which is what made it dangerous: the numbers looked right and the answer was
// useless on exactly the files this check exists for.
func TestStreamDictWindowDoesNotCrossObjectBoundaries(t *testing.T) {
	buf := []byte("7 0 obj\n<< /Filter /FlateDecode /Length 9 >>\nstream\nxxxxxxxxx\nendstream\nendobj\n" +
		"8 0 obj\n<< /Length 13 >>\nstream\nBT (hi) Tj ET\nendstream\nendobj\n")

	idx := bytes.LastIndex(buf, []byte("stream\nBT"))
	if idx < 0 {
		t.Fatal("fixture assumption broken")
	}
	window := pdfStreamDictWindow(buf, idx+len("stream"))

	if bytes.Contains(window, []byte("/Filter")) {
		t.Fatalf("the dictionary window for object 8 contains object 7's /Filter: %q -- a stream must never inherit a "+
			"neighbour's filter declaration, or raw content streams are classified opaque and every scanned PDF loses "+
			"coverage", window)
	}
	if !bytes.Contains(window, []byte("/Length 13")) {
		t.Fatalf("the window lost object 8's own dictionary: %q", window)
	}
}

// TestImageSkipIsWhatKeepsAbsentReachable measures the thing the skip is FOR,
// rather than asserting that the skip exists.
//
// A first version of this suite claimed the image skip was what kept scanned
// documents answerable, and an inversion disproved it: with the skip removed,
// multipage_image_only.pdf still reported "absent", because the default
// allowance is large enough to absorb a three-page fixture's images. The claim
// was true of production scans and not demonstrated by anything committed.
//
// Measured on that fixture, decompressed bytes produced by the scan:
//
//	with the skip:     2406
//	without the skip:  182406   (x76, on THREE pages)
//
// So this test picks an allowance between the two. With the skip it is ample;
// without it, it is gone before any content stream is reached, and the verdict
// degrades from a usable "absent" to "unknown". That is the whole argument, and
// now it fails when the skip is removed instead of merely describing it.
//
// Real scans are far worse than the ratio suggests: 59-RESEARCH.md R2 measured
// a SINGLE image stream inflating 3217 -> 540000 bytes.
func TestImageSkipIsWhatKeepsAbsentReachable(t *testing.T) {
	data := readFixture(t, filepath.Join("..", "convert", "testdata", "ocr", "multipage_image_only.pdf"))

	// Between the two measured figures above: roomy for content streams,
	// nowhere near enough for the page images.
	const allowance = 100_000

	state := scanPDFTextLayer(data, allowance, nil)
	if state.budgetHit {
		t.Fatalf("the scan exhausted a %d-byte inflate allowance on an image-only PDF -- image XObjects are being "+
			"decompressed, and every real scan will therefore report a ceiling instead of an answer", allowance)
	}

	value, reason := pdfTextLayerVerdict(state, false, false)
	if value != TextLayerAbsent {
		t.Fatalf("text_layer = %v (reason %q), want %q -- a scanned document is the primary client of this field, and "+
			"it must get a usable answer under an ordinary allowance", value, reason, TextLayerAbsent)
	}
}

// TestTextLayerCountMatchesPdftotextOnCorpus pins the COUNT, not just the
// verdict, against the reference numbers PROVENANCE.md records from live
// pdftotext runs.
//
// The verdict alone is too coarse to protect this. An inversion that loosened
// the operator match to "appears within 64 bytes" made embedded_font.pdf count
// 647 where the true answer is 42 -- and every verdict test stayed green,
// because 647 and 42 are both over the threshold and both mean "present". The
// magnitude has to be asserted for the defect to be visible at all.
//
// This is also what makes D-04's shared unit real rather than nominal: these
// are the same numbers internal/convert's countNonWhitespace produces over
// pdftotext output, so the two detectors are demonstrably on one scale before
// Plan 59-07 starts measuring the distance between them.
func TestTextLayerCountMatchesPdftotextOnCorpus(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want int // from testdata/pdf/PROVENANCE.md, measured with pdftotext
	}{
		{"born_digital", filepath.Join("..", "convert", "testdata", "ocr", "born_digital.pdf"), 82},
		{"objstm_born_digital", pdfCorpusPath("objstm_born_digital.pdf"), 82},
		{"uncompressed_text", pdfCorpusPath("uncompressed_text.pdf"), 53},
		{"embedded_font", pdfCorpusPath("embedded_font.pdf"), 42},
		{"multipage_image_only", filepath.Join("..", "convert", "testdata", "ocr", "multipage_image_only.pdf"), 0},
		{"multipage_text_is_an_image", filepath.Join("..", "convert", "testdata", "ocr", "multipage_text.pdf"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := readFixture(t, tc.path)
			state := scanPDFTextLayer(data, defaultMaxBytesForTest()/4, nil)
			if state.nonWhitespace != tc.want {
				t.Fatalf("counted %d non-whitespace bytes, want %d (pdftotext's own answer, recorded in "+
					"testdata/pdf/PROVENANCE.md) -- a count that drifts from pdftotext's makes the two detectors "+
					"incomparable, which is the one thing D-04 exists to prevent", state.nonWhitespace, tc.want)
			}
			if !state.covered {
				t.Fatalf("coverage lost on %s -- every fixture in this corpus must be fully examinable, or the "+
					"absence claim is unreachable for the files it is meant for", tc.name)
			}
		})
	}
}
