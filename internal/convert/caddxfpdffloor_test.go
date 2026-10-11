package convert

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
