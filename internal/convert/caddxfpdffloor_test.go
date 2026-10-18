package convert

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func cadSVGFactsOf(t *testing.T, svg string) CADSVGFacts {
	t.Helper()
	facts, err := ValidateCADSVG(strings.NewReader(svg), io.Discard, 1<<20)
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	return facts
}

func TestValidateCADSVGReportsViewBoxAndTextSizes(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="297mm" height="20.3mm" viewBox="0 0 10000 684">` +
		`<text font-size="2" transform="matrix(1 0 0 1 5 5)">a</text>` +
		`<text font-size="2" transform="matrix(0 -3 3 0 5 5)">b</text>` +
		`<g><text font-size="1">c<tspan font-size="4">d</tspan></text></g>` +
		`<text transform="matrix(1 0 0 1 0 0)">e</text>` +
		`</svg>`
	facts := cadSVGFactsOf(t, svg)
	if facts.ViewBoxW != 10000 || facts.ViewBoxH != 684 {
		t.Fatalf("viewBox = %v x %v, want 10000 x 684", facts.ViewBoxW, facts.ViewBoxH)
	}
	want := []float64{2, 6, 4, math.Inf(1)}
	if len(facts.TextSizes) != len(want) {
		t.Fatalf("TextSizes = %v, want %v", facts.TextSizes, want)
	}
	for i, w := range want {
		if facts.TextSizes[i] != w && !(math.IsInf(w, 1) && math.IsInf(facts.TextSizes[i], 1)) {
			t.Errorf("TextSizes[%d] = %v, want %v", i, facts.TextSizes[i], w)
		}
	}
}

func TestCADPDFDecodeReadsTheMediaBoxAndEveryShowSize(t *testing.T) {
	content := "q 2 0 0 2 0 0 cm BT /F1 3 Tf 0.5 0 0 0.5 10 10 Tm (AB) Tj 0 1 -1 0 0 0 Tm [(C)] TJ ET Q BT /F1 4 Tf (D) Tj ET"
	p := cadPDFWrite(t, cadPDFTwoFonts(content))
	facts, shows, err := cadPDFDecode(p, 1<<20, true)
	if err != nil {
		t.Fatal(err)
	}
	if facts.PageW != 100 || facts.PageH != 100 {
		t.Fatalf("page = %v x %v, want 100 x 100", facts.PageW, facts.PageH)
	}
	want := []CADPDFShow{{"AB", 3}, {"C", 6}, {"D", 4}}
	if len(shows) != len(want) {
		t.Fatalf("shows = %+v, want %+v", shows, want)
	}
	for i, w := range want {
		if shows[i].Text != w.Text || math.Abs(shows[i].SizePt-w.SizePt) > 1e-9 {
			t.Errorf("show %d = %+v, want %+v", i, shows[i], w)
		}
	}
}

func TestCADDXFPDFAlignMarksLostTextsAndKeepsTheRest(t *testing.T) {
	shows := []CADPDFShow{{"AB", 1}, {"C D", 2}, {"F", 3}}
	expect := []string{"AB", "X", "CD", "", "F"}
	rows, residual := cadDXFPDFAlign(expect, shows)
	if residual != 0 {
		t.Fatalf("residual = %d, want 0", residual)
	}
	want := []int{1, 0, 1, -1, 1}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v", rows)
	}
	for i := range want {
		if rows[i].Present != want[i] {
			t.Errorf("row %d present = %d, want %d", i, rows[i].Present, want[i])
		}
	}
	if rows[2].PDFPt != 2 || rows[4].PDFPt != 3 || rows[0].PDFPt != 1 {
		t.Errorf("pdf sizes = %+v", rows)
	}
	if _, residual := cadDXFPDFAlign([]string{"AB"}, shows); residual != 3 {
		t.Errorf("residual = %d, want 3 (C, D, F left over)", residual)
	}
}

func TestCADDXFPDFReportOnTheHelperFixtures(t *testing.T) {
	for _, name := range []string{"text_cases", "legacy_cp1251", "uncommon", "hatches_2"} {
		t.Run(name, func(t *testing.T) {
			pdf := filepath.Join(cadSVGTestDir, name+".pdf")
			texts := filepath.Join(cadSVGTestDir, name+".texts.json")
			svg := filepath.Join(cadSVGTestDir, name+".render.svg")
			if _, err := os.Stat(svg); err != nil {
				svg = filepath.Join(cadSVGTestDir, name+".svg")
			}
			rows, residual, err := CADDXFPDFReport(pdf, texts, svg, 1<<30)
			if err != nil {
				t.Fatal(err)
			}
			lost := 0
			for _, r := range rows {
				if r.Present == 0 {
					lost++
				}
				if r.Present == 1 && (r.SVGPt <= 0 || r.PDFPt <= 0 || math.Abs(r.SVGPt-r.PDFPt) > 0.01*r.SVGPt+0.002) {
					t.Errorf("text %q: svg %.6g pt, pdf %.6g pt", r.Text, r.SVGPt, r.PDFPt)
				}
			}
			if lost != 0 {
				t.Errorf("%d texts reported lost in a fixture whose PDF passes the judge", lost)
			}
			if residual != 0 {
				t.Errorf("residual = %d runes", residual)
			}
		})
	}
}

func TestCADDXFPDFTextFloorComesFromTheEvidence(t *testing.T) {
	const evidence = "testdata/evidence/75-dxf-2d-pipeline/75-05-bounds.txt"
	raw, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^DERIVED,key=cad_dxf_pdf_text_floor,value=([0-9.eE+-]+),`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no DERIVED line for cad_dxf_pdf_text_floor in %s", evidence)
	}
	want, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil {
		t.Fatal(err)
	}
	if CADDXFPDFTextFloorPt != want {
		t.Errorf("CADDXFPDFTextFloorPt = %v, evidence DERIVED line says %v: re-derive it with scripts/cad-dxf-bounds.sh floor rather than editing the expectation", CADDXFPDFTextFloorPt, want)
	}
}

func cadDXFFixtureInputs(t *testing.T, name string) (string, []cadDXFText, CADSVGFacts) {
	t.Helper()
	texts, err := parseCADDXFTexts(filepath.Join(cadSVGTestDir, name+".texts.json"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	svg, err := cadDXFReadSVGFacts(filepath.Join(cadSVGTestDir, name+".render.svg"), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cadSVGTestDir, name+".pdf"), texts, svg
}

func TestJudgeCADDXFPDFAcceptsHelperPDFsAndExcludesNothingAboveTheFloor(t *testing.T) {
	for _, name := range []string{"text_cases", "legacy_cp1251", "uncommon", "hatches_2"} {
		t.Run(name, func(t *testing.T) {
			pdf, texts, svg := cadDXFFixtureInputs(t, name)
			facts, err := JudgeCADDXFPDF(pdf, 1<<30, texts, svg)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if facts.BelowRenderFloor != 0 || facts.BelowFloorRunes != 0 || facts.FloorPt != CADDXFPDFTextFloorPt {
				t.Errorf("facts = %+v, want nothing below the %v pt floor", facts, CADDXFPDFTextFloorPt)
			}
			if facts.InvisibleText != 0 || facts.InvisibleRunes != 0 {
				t.Errorf("facts = %+v, want no invisible text", facts)
			}
			if facts.ExpectedRunes != utf8.RuneCountInString(cadDXFExpectedText(texts)) {
				t.Errorf("expected runes = %d, want %d", facts.ExpectedRunes, utf8.RuneCountInString(cadDXFExpectedText(texts)))
			}
		})
	}
}

func TestJudgeCADDXFPDFRejectsAnAlteredTextAboveTheFloor(t *testing.T) {
	pdf, texts, svg := cadDXFFixtureInputs(t, "uncommon")
	texts[7] = cadDXFText{Text: "☃", Covered: "☃"}
	_, err := JudgeCADDXFPDF(pdf, 1<<30, texts, svg)
	cadPDFRequireRejected(t, err, "missing text")
}

func TestJudgeCADDXFPDFExcludesAndCountsAnAlteredTextBelowTheFloor(t *testing.T) {
	pdf, texts, svg := cadDXFFixtureInputs(t, "uncommon")
	texts[7] = cadDXFText{Text: "☃☃", Covered: "☃☃"}
	svg.TextSizes = append([]float64(nil), svg.TextSizes...)
	svg.TextSizes[7] = 1e-9
	facts, err := JudgeCADDXFPDF(pdf, 1<<30, texts, svg)
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if facts.BelowRenderFloor != 1 || facts.BelowFloorRunes != 2 {
		t.Errorf("facts = %+v, want one text and two runes below the floor", facts)
	}
}

func TestJudgeCADDXFPDFRejectsMismatchedInputs(t *testing.T) {
	pdf, texts, svg := cadDXFFixtureInputs(t, "uncommon")
	_, err := JudgeCADDXFPDF(pdf, 1<<30, texts[1:], svg)
	cadPDFRequireRejected(t, err, "texts.json has 48 entries")
	noBox := cadPDFFile(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>",
		cadPDFStream("", []byte("0 0 1 1 re f 0 0 2 2 re f"), true),
	)
	svgOne := CADSVGFacts{ViewBoxW: 10, ViewBoxH: 10, TextSizes: []float64{1}}
	_, err = JudgeCADDXFPDF(cadPDFWrite(t, noBox), 1<<20, []cadDXFText{{Text: "A", Covered: "A"}}, svgOne)
	cadPDFRequireRejected(t, err, "MediaBox")
}

func TestJudgeCADDXFPDFExcludesAndCountsInvisibleTexts(t *testing.T) {
	for _, c := range []struct {
		name             string
		invisible, runes int
		expected         string
	}{
		{"text_alpha", 4, 29, "OPAQUETEXTHALFTEXTOPAQUEMTEXTHALFMTEXTHALFSPLITINLINEREDLAYERTEXTBLOCKTEXT"},
		{"text_alpha_faint", 1, 4, "FAINTONLY"},
	} {
		t.Run(c.name, func(t *testing.T) {
			pdf, texts, svg := cadDXFFixtureInputs(t, c.name)
			facts, err := JudgeCADDXFPDF(pdf, 1<<30, texts, svg)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if facts.InvisibleText != c.invisible || facts.InvisibleRunes != c.runes || facts.BelowRenderFloor != 0 {
				t.Errorf("facts = %+v, want %d invisible texts of %d runes and none below the floor", facts, c.invisible, c.runes)
			}
			if facts.ExpectedRunes != utf8.RuneCountInString(c.expected) {
				t.Errorf("expected runes = %d, want %d", facts.ExpectedRunes, utf8.RuneCountInString(c.expected))
			}
			if got := cadDXFExpectedText(texts); got != c.expected {
				t.Errorf("expected text = %q, want %q", got, c.expected)
			}
			if _, err := JudgeCADPDF(pdf, 1<<30, cadDXFExpectedText(texts)); err != nil {
				t.Errorf("JudgeCADPDF rejects the expectation without invisible texts: %v", err)
			}
		})
	}
}

func TestJudgeCADDXFPDFRequiresAHalfTransparentText(t *testing.T) {
	pdf, texts, svg := cadDXFFixtureInputs(t, "text_alpha")
	texts[1] = cadDXFText{Text: "☃", Covered: "☃"}
	_, err := JudgeCADDXFPDF(pdf, 1<<30, texts, svg)
	cadPDFRequireRejected(t, err, "missing text")
}

func TestJudgeCADDXFPDFRejectsAnInvisibleFlagTheSVGContradicts(t *testing.T) {
	pdf, texts, svg := cadDXFFixtureInputs(t, "text_alpha")
	for _, c := range []struct {
		name string
		edit func(texts []cadDXFText, svg *CADSVGFacts)
		rule string
	}{
		{"an invisible text without the flag", func(texts []cadDXFText, _ *CADSVGFacts) { texts[2].Invisible = false }, "invisible"},
		{"a visible text with the flag", func(texts []cadDXFText, _ *CADSVGFacts) { texts[0].Invisible = true }, "invisible"},
		{"a half-transparent text with the flag", func(texts []cadDXFText, _ *CADSVGFacts) { texts[1].Invisible = true }, "invisible"},
		{"an SVG without opacities", func(_ []cadDXFText, svg *CADSVGFacts) { svg.TextOpacity = nil }, "opacities"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx := append([]cadDXFText(nil), texts...)
			sv := svg
			sv.TextOpacity = append([]float64(nil), svg.TextOpacity...)
			c.edit(tx, &sv)
			_, err := JudgeCADDXFPDF(pdf, 1<<30, tx, sv)
			cadPDFRequireRejected(t, err, c.rule)
		})
	}
}

func TestParseCADDXFTextsReadsTheInvisibleFlag(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		body string
		want []bool
		ok   bool
	}{
		{`{"texts":[{"text":"a","covered":"a"},{"text":"b","covered":"b","invisible":true}]}`, []bool{false, true}, true},
		{`{"texts":[{"text":"a","covered":"a","invisible":false}]}`, []bool{false}, true},
		{`{"texts":[{"text":"a","covered":"a","invisible":"yes"}]}`, nil, false},
		{`{"texts":[{"text":"a","covered":"a","invisible":1}]}`, nil, false},
		{`{"texts":[{"text":"a","covered":"a","invisible":null}]}`, nil, false},
	} {
		p := filepath.Join(dir, "t.json")
		if err := os.WriteFile(p, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := parseCADDXFTexts(p, 1<<20)
		if !c.ok {
			if !errors.Is(err, ErrCADPDFRejected) {
				t.Errorf("%s: err = %v, want a rejection", c.body, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		for i, w := range c.want {
			if got[i].Invisible != w {
				t.Errorf("%s: entry %d invisible = %v, want %v", c.body, i, got[i].Invisible, w)
			}
		}
	}
}
