package convert

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cadSVGVisibleOf(t *testing.T, body string) int {
	t.Helper()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="10mm" viewBox="0 0 100 100">` + body + `</svg>`
	return cadSVGFactsOf(t, svg).Visible
}

func TestValidateCADSVGCountsVisibleElements(t *testing.T) {
	closed := `d="M 0 0 l 10 0 l 0 10 Z"`
	open := `d="M 0 0 l 10 0"`
	cases := []struct {
		name, body string
		want       int
	}{
		{"background rect at fill-opacity 0", `<rect fill="#ffffff" x="0" y="0" width="100" height="100" fill-opacity="0.0" />`, 0},
		{"stroke at opacity 0", `<path ` + open + ` fill="none" stroke="#000000" stroke-width="8" stroke-opacity="0.000" />`, 0},
		{"fill at opacity 0", `<path ` + closed + ` fill="#ff0000" fill-opacity="0.000" />`, 0},
		{"stroke at opacity 0.498", `<path ` + open + ` fill="none" stroke="#000000" stroke-width="8" stroke-opacity="0.498" />`, 1},
		{"fill at opacity 0.004", `<path ` + closed + ` fill="#ff0000" fill-opacity="0.004" />`, 1},
		{"fill none and no stroke", `<path ` + open + ` fill="none" />`, 0},
		{"default fill is black", `<path ` + closed + ` />`, 1},
		{"stroke none", `<path ` + open + ` fill="none" stroke="none" stroke-width="8" />`, 0},
		{"stroke width 0", `<path ` + open + ` fill="none" stroke="#000000" stroke-width="0" />`, 0},
		{"fill-opacity 0 inherited from g", `<g fill-opacity="0"><path ` + closed + ` fill="#ff0000" /></g>`, 0},
		{"own fill-opacity overrides g", `<g fill-opacity="0"><path ` + closed + ` fill-opacity="0.5" /></g>`, 1},
		{"fill none inherited from g", `<g fill="none"><path ` + closed + ` /></g>`, 0},
		{"stroke inherited from g", `<g stroke="#000000"><path ` + open + ` fill="none" /></g>`, 1},
		{"stroke-opacity 0 inherited from g", `<g stroke="#000000" stroke-opacity="0"><path ` + open + ` fill="none" /></g>`, 0},
		{"stroke-width 0 inherited from g", `<g stroke="#000000" stroke-width="0"><path ` + open + ` fill="none" /></g>`, 0},
		{"deep inheritance", `<g fill-opacity="0"><g><path ` + closed + ` /></g></g>`, 0},
		{"line has no fill", `<line x1="0" y1="0" x2="10" y2="0" />`, 0},
		{"line stroked", `<line x1="0" y1="0" x2="10" y2="0" stroke="#000000" />`, 1},
		{"rect of zero width", `<rect x="0" y="0" width="0" height="10" />`, 0},
		{"rect", `<rect x="0" y="0" width="10" height="10" />`, 1},
		{"circle of radius 0", `<circle cx="5" cy="5" r="0" stroke="#000000" />`, 0},
		{"circle", `<circle cx="5" cy="5" r="3" />`, 1},
		{"ellipse of zero ry", `<ellipse cx="5" cy="5" rx="3" ry="0" stroke="#000000" />`, 0},
		{"path with only a moveto", `<path d="M 1 1" stroke="#000000" />`, 0},
		{"zero-length segment with a cap", `<path d="M 1 1 l 0 0" fill="none" stroke="#000000" stroke-width="8" />`, 1},
		{"polyline of one point", `<polyline points="1 1" stroke="#000000" />`, 0},
		{"polyline", `<polyline points="1 1 5 5" stroke="#000000" />`, 1},
		{"shapes inside a clipPath", `<defs><clipPath><rect x="0" y="0" width="10" height="10" /></clipPath></defs>`, 0},
		{"text at opacity 0", `<text font-size="2" fill-opacity="0">gone</text>`, 0},
		{"text with fill none", `<text font-size="2" fill="none">gone</text>`, 0},
		{"text", `<text font-size="2">shown</text>`, 1},
		{"text of whitespace", `<text font-size="2"> </text>`, 0},
		{"text in a g at opacity 0", `<g fill-opacity="0"><text font-size="2">gone</text></g>`, 0},
		{"tspan shows a character", `<text font-size="2" fill-opacity="0">a<tspan fill-opacity="0.5">b</tspan></text>`, 1},
		{"tspan shows only space", `<text font-size="2" fill-opacity="0">a<tspan fill-opacity="1"> </tspan></text>`, 0},
		{"visible text around a hidden tspan", `<text font-size="2">a<tspan fill-opacity="0">b</tspan></text>`, 1},
		{"hidden text after a tspan", `<text font-size="2" fill-opacity="0"><tspan fill-opacity="1"> </tspan>tail</text>`, 0},
		{"text stroked by its g", `<g stroke="#000000"><text font-size="2" fill-opacity="0">x</text></g>`, 1},
		{"two visible and one hidden", `<path ` + closed + ` /><text font-size="2">a</text><path ` + closed + ` fill-opacity="0" />`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cadSVGVisibleOf(t, tc.body); got != tc.want {
				t.Errorf("Visible = %d, want %d", got, tc.want)
			}
		})
	}
}

func cadDXFClearedOneVisible(t *testing.T, dir string) cadDXFOutputs {
	t.Helper()
	o := cadDXFFixtureOutputs(t, "alpha_one_visible")
	half, clear := `stroke-opacity="0.498"`, `stroke-opacity="0.000"`
	o.client = cadDXFMutate(t, dir, o.client, "clear.svg", half, clear)
	o.render = cadDXFMutate(t, dir, o.render, "clear.render.svg", half, clear)
	return o
}

func cadDXFClearedFaint(t *testing.T, dir string) cadDXFOutputs {
	t.Helper()
	o := cadDXFFixtureOutputs(t, "text_alpha_faint")
	half, clear := `fill-opacity="0.498"`, `fill-opacity="0.000"`
	o.client = cadDXFMutate(t, dir, o.client, "faint.svg", half, clear)
	o.render = cadDXFMutate(t, dir, o.render, "faint.render.svg", half, clear)
	tf := cadDXFReadTexts(t, o.texts)
	for i := range tf.Texts {
		tf.Texts[i].Invisible = true
	}
	raw, err := json.Marshal(tf)
	if err != nil {
		t.Fatal(err)
	}
	o.texts = cadDXFWrite(t, dir, "faint.texts.json", raw)
	return o
}

func TestValidateCADSVGCountsTheVisibleElementsOfTheHelperOutputs(t *testing.T) {
	names, err := filepath.Glob(filepath.Join(cadSVGTestDir, "*.svg"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no helper SVGs: %v", err)
	}
	for _, p := range names {
		in, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		_, facts, err := cadSVGValidate(t, in, int64(len(in)))
		if err != nil {
			t.Fatalf("%s rejected: %v", p, err)
		}
		if facts.Visible < 1 {
			t.Errorf("%s: Visible = %d, want at least 1", filepath.Base(p), facts.Visible)
		}
		switch filepath.Base(p) {
		case "alpha_one_visible.svg", "alpha_one_visible.render.svg", "text_alpha_faint.svg", "text_alpha_faint.render.svg":
			if facts.Visible != 1 {
				t.Errorf("%s: Visible = %d, want exactly 1", filepath.Base(p), facts.Visible)
			}
		}
	}
	dir := t.TempDir()
	for _, o := range []cadDXFOutputs{cadDXFClearedOneVisible(t, dir), cadDXFClearedFaint(t, dir)} {
		for _, p := range []string{o.client, o.render} {
			in, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			_, facts, err := cadSVGValidate(t, in, int64(len(in)))
			if err != nil {
				t.Fatalf("%s rejected: %v", p, err)
			}
			if facts.Visible != 0 {
				t.Errorf("%s: Visible = %d, want 0", filepath.Base(p), facts.Visible)
			}
		}
	}
}

func cadDXFRunTarget(t *testing.T, f cadDXFFixture, target string) error {
	t.Helper()
	if target == "png" {
		return (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, nil)
	}
	return (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
}

func TestCADDXFRefusesADrawingWithNothingVisibleOnEveryRoute(t *testing.T) {
	builds := map[string]func(*testing.T, string) cadDXFOutputs{
		"transparent geometry and text": cadDXFClearedOneVisible,
		"transparent text only":         cadDXFClearedFaint,
	}
	for name, build := range builds {
		for _, target := range []string{"svg", "pdf", "png"} {
			t.Run(name+"/"+target, func(t *testing.T) {
				o := build(t, t.TempDir())
				o.measure = cadDXFTestFile("wide.measure.json")
				f := newCADDXFFixture(t, target)
				f.setDXF(t, o)
				f.setRsvg(t, cadDXFTestFile("alpha_one_visible.pdf"), 0, "")
				err := cadDXFRunTarget(t, f, target)
				if !errors.Is(err, ErrCADEmptyResult) || !errors.Is(err, ErrCADNothingDrawable) {
					t.Fatalf("err = %v, want ErrCADNothingDrawable and ErrCADEmptyResult", err)
				}
				if !strings.Contains(err.Error(), "nothing visible") {
					t.Errorf("err = %v, want it to say nothing is visible", err)
				}
				if f.exists("rsvg.args") {
					t.Error("rsvg-convert ran on a drawing with nothing visible")
				}
				f.noOutput(t)
				f.noSidecar(t)
			})
		}
	}
}

func TestCADDXFRejectsAVisibleCountTheOutputsDisagreeOn(t *testing.T) {
	for _, side := range []string{"client", "render"} {
		for _, target := range []string{"svg", "pdf"} {
			t.Run(side+"/"+target, func(t *testing.T) {
				dir := t.TempDir()
				o := cadDXFFixtureOutputs(t, "alpha_one_visible")
				cleared := cadDXFClearedOneVisible(t, dir)
				if side == "client" {
					o.client = cleared.client
				} else {
					o.render = cleared.render
				}
				f := newCADDXFFixture(t, target)
				f.setDXF(t, o)
				f.setRsvg(t, cadDXFTestFile("alpha_one_visible.pdf"), 0, "")
				err := cadDXFRunTarget(t, f, target)
				if !errors.Is(err, ErrCADSVGRejected) || errors.Is(err, ErrCADEmptyResult) {
					t.Fatalf("err = %v, want ErrCADSVGRejected and not an empty result", err)
				}
				if !strings.Contains(err.Error(), "visible") {
					t.Errorf("err = %v, want it to name the visible count", err)
				}
				f.noOutput(t)
			})
		}
	}
}

func TestCADDXFConvertsADrawingWithOneVisibleElement(t *testing.T) {
	for _, target := range []string{"svg", "pdf"} {
		t.Run(target, func(t *testing.T) {
			f := newCADDXFFixture(t, target)
			f.setDXF(t, cadDXFFixtureOutputs(t, "alpha_one_visible"))
			f.setRsvg(t, cadDXFTestFile("alpha_one_visible.pdf"), 0, "")
			if err := cadDXFRunTarget(t, f, target); err != nil {
				t.Fatalf("Convert: %v", err)
			}
		})
	}
}

const (
	cadPDFBackground = "1 0 0 -1 0 100 cm\nq\n1 1 1 rg /a0 gs\n0 0 100 100 re f\nQ\n"
	cadPDFOpaqueGS   = "/a0 << /CA 1 /ca 1 >>"
)

func cadPDFWithGS(content, extg string) []byte {
	res := "/Font << /F1 5 0 R >>"
	if extg != "" {
		res += " /ExtGState << " + extg + " >>"
	}
	return cadPDFFile(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << "+res+" >> >>",
		cadPDFStream("", []byte(content), true),
		"<< /Type /Font /Subtype /TrueType /BaseFont /X /Encoding /WinAnsiEncoding /ToUnicode 6 0 R >>",
		cadPDFStream("", []byte(cadPDFIdentityCMap), true),
	)
}

func TestJudgeCADPDFCountsOnlyInkTheGraphicsStateShows(t *testing.T) {
	stroke := "0 0 m 50 50 l S\n"
	fill := "0 0 m 50 0 l 50 50 l h f\n"
	both := "0 0 m 50 0 l 50 50 l h B\n"
	text := "BT /F1 10 Tf 10 10 Td (A) Tj ET\n"
	cases := []struct {
		name, content, extg string
		ink                 int
	}{
		{"stroke at CA 0", "/a1 gs\n" + stroke, "/a1 << /CA 0 /ca 0 >>", 1},
		{"stroke at CA 0.498", "/a1 gs\n" + stroke, "/a1 << /CA 0.498039 /ca 0.498039 >>", 2},
		{"stroke at CA 1 and ca 0", "/a1 gs\n" + stroke, "/a1 << /CA 1 /ca 0 >>", 2},
		{"fill at ca 0 and CA 1", "/a1 gs\n" + fill, "/a1 << /CA 1 /ca 0 >>", 1},
		{"fill at ca 0.5", "/a1 gs\n" + fill, "/a1 << /CA 0 /ca 0.5 >>", 2},
		{"fill and stroke at ca 0 and CA 0.5", "/a1 gs\n" + both, "/a1 << /CA 0.5 /ca 0 >>", 2},
		{"fill and stroke at both 0", "/a1 gs\n" + both, "/a1 << /CA 0 /ca 0 >>", 1},
		{"alpha restored by Q", "q /a1 gs Q\n" + stroke, "/a1 << /CA 0 /ca 0 >>", 2},
		{"alpha set inside q is kept until Q", "q /a1 gs\n" + stroke + "Q\n", "/a1 << /CA 0 /ca 0 >>", 1},
		{"stroke under a soft mask", "/a1 gs\n" + stroke, "/a1 << /CA 1 /ca 1 /SMask << /S /Alpha /G 4 0 R >> >>", 1},
		{"stroke with the soft mask removed", "/a1 gs\n" + stroke, "/a1 << /CA 1 /ca 1 /SMask /None >>", 2},
		{"a later gs clears the soft mask", "/a1 gs /a2 gs\n" + stroke, "/a1 << /SMask << /S /Alpha /G 4 0 R >> >> /a2 << /SMask /None >>", 2},
		{"a gs without alpha keeps the alpha", "/a1 gs /a2 gs\n" + stroke, "/a1 << /CA 0 >> /a2 << /LW 2 >>", 1},
		{"text in fill mode", text, "", 2},
		{"text at render mode 3", "BT /F1 10 Tf 3 Tr (A) Tj ET\n", "", 1},
		{"text at render mode 7", "BT /F1 10 Tf 7 Tr (A) Tj ET\n", "", 1},
		{"text at render mode 1 with CA 0", "/a1 gs\nBT /F1 10 Tf 1 Tr (A) Tj ET\n", "/a1 << /CA 0 /ca 1 >>", 1},
		{"text at render mode 2 with ca 0", "/a1 gs\nBT /F1 10 Tf 2 Tr (A) Tj ET\n", "/a1 << /CA 1 /ca 0 >>", 2},
		{"text at ca 0", "/a1 gs\n" + text, "/a1 << /CA 1 /ca 0 >>", 1},
		{"render mode restored by Q", "q BT /F1 10 Tf 3 Tr ET Q\n" + text, "", 2},
		{"an empty string shows nothing", "BT /F1 10 Tf () Tj ET\n", "", 1},
		{"a TJ of numbers shows nothing", "BT /F1 10 Tf [10 20] TJ ET\n", "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := cadPDFWrite(t, cadPDFWithGS(cadPDFBackground+tc.content, cadPDFOpaqueGS+" "+tc.extg))
			facts, err := JudgeCADPDF(p, 1<<20, "")
			if facts.InkOps != tc.ink {
				t.Errorf("InkOps = %d, want %d (PaintOps %d)", facts.InkOps, tc.ink, facts.PaintOps)
			}
			if tc.ink < 2 {
				cadPDFRequireRejected(t, err, "empty page")
			} else if err != nil {
				t.Fatalf("rejected: %v", err)
			}
		})
	}
}

func TestJudgeCADPDFRejectsAGraphicsStateItCannotRead(t *testing.T) {
	cases := []struct {
		name, content, extg, rule string
	}{
		{"a gs the resources do not name", "/zz gs\n0 0 m 5 5 l S\n", "", "ExtGState /zz"},
		{"a gs on a page without ExtGState", "/a1 gs\n0 0 m 5 5 l S\n", "-", "ExtGState /a1"},
		{"a stroke alpha that is not a number", "/a1 gs\n0 0 m 5 5 l S\n", "/a1 << /CA /One >>", "/CA"},
		{"a fill alpha above 1", "/a1 gs\n0 0 m 5 5 l S\n", "/a1 << /ca 2 >>", "/ca"},
		{"a negative alpha", "/a1 gs\n0 0 m 5 5 l S\n", "/a1 << /CA -0.5 >>", "/CA"},
		{"an ExtGState that is not a dictionary", "/a1 gs\n0 0 m 5 5 l S\n", "/a1 7", "ExtGState /a1"},
		{"a gs without a name", "1 gs\n0 0 m 5 5 l S\n", "", "gs"},
		{"a text render mode out of range", "BT /F1 10 Tf 8 Tr (A) Tj ET\n", "", "Tr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extg := cadPDFOpaqueGS + " " + tc.extg
			content := cadPDFBackground + tc.content
			if tc.extg == "-" {
				extg = ""
				content = "1 1 1 rg 0 0 100 100 re f\n" + tc.content
			}
			p := cadPDFWrite(t, cadPDFWithGS(content, extg))
			_, err := JudgeCADPDF(p, 1<<20, "")
			cadPDFRequireRejected(t, err, tc.rule)
		})
	}
}

func TestJudgeCADPDFCountsTheInkOfTheHelperPDFs(t *testing.T) {
	for _, name := range []string{"text_cases", "legacy_cp1251", "uncommon", "hatches_2", "text_alpha", "text_alpha_faint", "text_fonts", "alpha_one_visible"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(cadSVGTestDir, name+".pdf")
			facts, err := JudgeCADDXFPDFFiles(p, filepath.Join(cadSVGTestDir, name+".texts.json"), filepath.Join(cadSVGTestDir, name+".render.svg"), 1<<24)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if facts.InkOps < 2 || facts.InkOps > facts.PaintOps {
				t.Errorf("InkOps = %d, PaintOps = %d, want 2 <= InkOps <= PaintOps", facts.InkOps, facts.PaintOps)
			}
			if name == "alpha_one_visible" && (facts.InkOps != 2 || facts.PaintOps != 2 || facts.InvisibleText != 2 || facts.ExpectedRunes != 0) {
				t.Errorf("facts = %+v, want the background and the one visible line as the only ink and two invisible texts", facts)
			}
		})
	}
}
