package convert

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const cadSVGTestDir = "testdata/cad/dxf-out"

const cadSVGBase = `<?xml version='1.0' encoding='utf-8'?>
<svg xmlns="http://www.w3.org/2000/svg" width="297mm" height="164.3mm" viewBox="0 0 10000 5532"><defs /><rect fill="#ffffff" x="0" y="0" width="10000" height="5532" fill-opacity="0.0" /><g stroke-linecap="round" stroke-linejoin="round" fill-rule="evenodd"><path d="M 725 3835 l 777 0" fill="none" stroke="#000000" stroke-width="8" /><text transform="matrix(1 0 0 1 725.309 920.104)" font-family="DejaVu Sans" font-size="133.267" fill="#000000" xml:space="preserve">A&amp;B</text></g></svg>
`

const cadSVGSpecial = `A&B <x> "q" 'a' & <script>alert(1)</script>`

func cadSVGValidate(t *testing.T, in []byte, maxBytes int64) ([]byte, CADSVGFacts, error) {
	t.Helper()
	var out bytes.Buffer
	facts, err := ValidateCADSVG(bytes.NewReader(in), &out, maxBytes)
	return out.Bytes(), facts, err
}

type cadSVGTextsFile struct {
	Texts []struct {
		Text      string `json:"text"`
		Covered   string `json:"covered"`
		Invisible bool   `json:"invisible,omitempty"`
	} `json:"texts"`
}

func cadSVGWantTexts(t *testing.T, svgName string) []string {
	t.Helper()
	render := strings.HasSuffix(svgName, ".render.svg")
	base := strings.TrimSuffix(strings.TrimSuffix(svgName, ".svg"), ".render")
	data, err := os.ReadFile(filepath.Join(cadSVGTestDir, base+".texts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tf cadSVGTextsFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatal(err)
	}
	want := make([]string, 0, len(tf.Texts))
	for _, e := range tf.Texts {
		if !render {
			want = append(want, e.Text)
			continue
		}
		covered := map[rune]bool{}
		for _, r := range e.Covered {
			covered[r] = true
		}
		var b strings.Builder
		for _, r := range e.Text {
			if covered[r] {
				b.WriteRune(r)
			} else {
				b.WriteRune('�')
			}
		}
		want = append(want, b.String())
	}
	return want
}

func cadSVGRootMM(t *testing.T, data []byte, attr string) float64 {
	t.Helper()
	m := regexp.MustCompile(`<svg [^>]*\b` + attr + `="([0-9.]+)mm"`).FindSubmatch(data)
	if m == nil {
		t.Fatalf("no root %s in mm", attr)
	}
	v, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidateCADSVGAcceptsEveryHelperFixture(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(cadSVGTestDir, "*.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 12 {
		t.Fatalf("found %d helper SVGs, want the 12 client and render-only SVGs", len(paths))
	}
	for _, p := range paths {
		name := filepath.Base(p)
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out1, f1, err := cadSVGValidate(t, in, int64(len(in)))
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			out2, f2, err := cadSVGValidate(t, out1, int64(len(out1)))
			if err != nil {
				t.Fatalf("re-validation rejected: %v", err)
			}
			if !bytes.Equal(out1, out2) {
				t.Fatalf("re-serialization is not a fixed point: %d vs %d bytes", len(out1), len(out2))
			}
			if f1.WidthMM != f2.WidthMM || f1.HeightMM != f2.HeightMM || strings.Join(f1.Texts, "\x00") != strings.Join(f2.Texts, "\x00") {
				t.Fatalf("facts differ after re-validation: %+v vs %+v", f1, f2)
			}
			if w := cadSVGRootMM(t, in, "width"); f1.WidthMM != w {
				t.Errorf("WidthMM = %v, want %v", f1.WidthMM, w)
			}
			if h := cadSVGRootMM(t, in, "height"); f1.HeightMM != h {
				t.Errorf("HeightMM = %v, want %v", f1.HeightMM, h)
			}
			want := cadSVGWantTexts(t, name)
			if len(f1.Texts) != len(want) {
				t.Fatalf("%d texts, want %d", len(f1.Texts), len(want))
			}
			for i := range want {
				if f1.Texts[i] != want[i] {
					t.Errorf("text %d = %q, want %q", i, f1.Texts[i], want[i])
				}
			}
			if !bytes.HasPrefix(out1, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
				t.Errorf("output does not start with the Go declaration: %q", out1[:min(len(out1), 60)])
			}
		})
	}
}

func TestValidateCADSVGRoundTripsSpecialCharacters(t *testing.T) {
	in, err := os.ReadFile(filepath.Join(cadSVGTestDir, "text_cases.svg"))
	if err != nil {
		t.Fatal(err)
	}
	out, facts, err := cadSVGValidate(t, in, int64(len(in)))
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Texts) == 0 || facts.Texts[0] != cadSVGSpecial {
		t.Fatalf("first text = %q, want %q", facts.Texts, cadSVGSpecial)
	}
	if bytes.Contains(out, []byte("<script")) {
		t.Fatal("the output holds an unescaped <script")
	}
	dec := xml.NewDecoder(bytes.NewReader(out))
	var texts []string
	var cur *strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch tt := tok.(type) {
		case xml.StartElement:
			if tt.Name.Local == "text" {
				cur = &strings.Builder{}
			}
		case xml.CharData:
			if cur != nil {
				cur.Write(tt)
			}
		case xml.EndElement:
			if tt.Name.Local == "text" {
				texts = append(texts, cur.String())
				cur = nil
			}
		}
	}
	found := false
	for _, s := range texts {
		if s == cadSVGSpecial {
			found = true
		}
	}
	if !found {
		t.Fatalf("no decoded text equals %q in %q", cadSVGSpecial, texts)
	}
}

func TestValidateCADSVGEscapesControlWhitespaceAndStaysAFixedPoint(t *testing.T) {
	in := strings.Replace(cadSVGBase, ">A&amp;B<", ">a&#x9;b&#xA;c &apos;&quot;<", 1)
	out1, f1, err := cadSVGValidate(t, []byte(in), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(f1.Texts) != 1 || f1.Texts[0] != "a\tb\nc '\"" {
		t.Fatalf("texts = %q", f1.Texts)
	}
	out2, _, err := cadSVGValidate(t, out1, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out1, out2) {
		t.Fatalf("not a fixed point:\n%s\n%s", out1, out2)
	}
	if bytes.ContainsAny(out1[bytes.Index(out1, []byte("<text")):], "\t") {
		t.Fatal("a raw tab reached the output")
	}
}

func TestValidateCADSVGAcceptsTheD05ShapesAndTspan(t *testing.T) {
	in := `<svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="12.5mm" viewBox="0 0 100 125"><defs><clipPath><rect x="0" y="0" width="10" height="10" /></clipPath></defs><g fill="none" stroke="#00Aa00" stroke-width="1.5" stroke-opacity="0.5"><polyline points="0,0 10,10 20,0" /><polygon points="0 0 10 10 20 0" fill="#ff0000" fill-opacity="1" /><line x1="0" y1="0" x2="10" y2="-1e1" /><circle cx="5" cy="5" r="2" /><ellipse cx="5" cy="5" rx="2" ry=".5" /><text transform="matrix(1,0,0,1,0,0)" font-family="DejaVu Serif" font-size="3" fill="#000000" xml:space="preserve">a<tspan font-family="DejaVu Sans Mono" font-weight="bold">b</tspan>c</text></g></svg>`
	out, facts, err := cadSVGValidate(t, []byte(in), 1<<20)
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if facts.WidthMM != 10 || facts.HeightMM != 12.5 || len(facts.Texts) != 1 || facts.Texts[0] != "abc" {
		t.Fatalf("facts = %+v", facts)
	}
	for _, el := range []string{"<polyline", "<polygon", "<line", "<circle", "<ellipse", "<tspan", "<clipPath"} {
		if !bytes.Contains(out, []byte(el)) {
			t.Errorf("output lost %s", el)
		}
	}
}

func TestValidateCADSVGAcceptsDepthSixteenAndTheExactByteLimit(t *testing.T) {
	deep := strings.Replace(cadSVGBase, `<path d="M 725 3835 l 777 0" fill="none" stroke="#000000" stroke-width="8" />`,
		strings.Repeat("<g>", 13)+`<path d="M 0 0" />`+strings.Repeat("</g>", 13), 1)
	if _, _, err := cadSVGValidate(t, []byte(deep), int64(len(deep))); err != nil {
		t.Fatalf("depth 16 rejected: %v", err)
	}
}

func TestValidateCADSVGRejectsMutations(t *testing.T) {
	path := `<path d="M 725 3835 l 777 0" fill="none" stroke="#000000" stroke-width="8" />`
	root := `<svg xmlns="http://www.w3.org/2000/svg" width="297mm"`
	textOpen := `<text transform="matrix(1 0 0 1 725.309 920.104)" font-family="DejaVu Sans"`
	inG := func(s string) string { return strings.Replace(cadSVGBase, path, path+s, 1) }
	rep := func(old, new string) string {
		if !strings.Contains(cadSVGBase, old) {
			panic("mutation anchor missing: " + old)
		}
		return strings.Replace(cadSVGBase, old, new, 1)
	}
	decl := "<?xml version='1.0' encoding='utf-8'?>\n"
	body := strings.TrimPrefix(cadSVGBase, decl)
	cases := []struct {
		name, in, rule string
		max            int64
	}{
		{"doctype", rep(decl, decl+"<!DOCTYPE svg>"), "directive", 0},
		{"entity_declaration", rep(decl, decl+`<!DOCTYPE svg [<!ENTITY a "b">]>`), "directive", 0},
		{"xml_stylesheet", rep(decl, decl+`<?xml-stylesheet href="x.css"?>`), "processing instruction", 0},
		{"declaration_after_whitespace", "\n" + cadSVGBase, "processing instruction", 0},
		{"declaration_inside_root", body[:strings.Index(body, "<defs")] + `<?xml version="1.0"?>` + body[strings.Index(body, "<defs"):], "processing instruction", 0},
		{"declaration_latin1", rep("encoding='utf-8'", "encoding='ISO-8859-1'"), "encoding", 0},
		{"declaration_version_1_1", rep("version='1.0'", "version='1.1'"), "version", 0},
		{"declaration_standalone", rep("encoding='utf-8'", "encoding='utf-8' standalone='yes'"), "XML declaration", 0},
		{"comment", inG("<!-- x -->"), "comment", 0},
		{"second_root", cadSVGBase + `<svg xmlns="http://www.w3.org/2000/svg" />`, "after the root", 0},
		{"second_root_no_namespace", cadSVGBase + `<svg/>`, "after the root", 0},
		{"text_after_root", cadSVGBase + "x", "after the root", 0},
		{"chardata_in_g", inG("hello"), "character data", 0},
		{"chardata_in_root", rep("<defs />", "hi<defs />"), "character data", 0},
		{"script", inG("<script>alert(1)</script>"), "element script", 0},
		{"foreign_object", inG("<foreignObject />"), "element foreignObject", 0},
		{"style_element", inG("<style>path{fill:red}</style>"), "element style", 0},
		{"style_attribute", rep(`stroke-width="8" />`, `stroke-width="8" style="fill:red" />`), "attribute style", 0},
		{"style_attribute_url", rep(`stroke-width="8" />`, `stroke-width="8" style="fill:url(#a)" />`), "attribute style", 0},
		{"style_attribute_import", rep(`stroke-width="8" />`, `stroke-width="8" style="@import 'x.css'" />`), "attribute style", 0},
		{"onload", rep(root, root+` onload="alert(1)"`), "attribute onload", 0},
		{"use_href", inG(`<use href="#a" />`), "element use", 0},
		{"use_xlink_href", inG(`<use xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="#a" />`), "element use", 0},
		{"path_href", rep(`stroke-width="8" />`, `stroke-width="8" href="#a" />`), "attribute href", 0},
		{"path_xlink_href", rep(`stroke-width="8" />`, `stroke-width="8" xlink:href="#a" />`), "attribute xlink:href", 0},
		{"image", inG(`<image width="1" height="1" />`), "element image", 0},
		{"xhtml_prefixed", inG(`<h:script xmlns:h="http://www.w3.org/1999/xhtml">alert(1)</h:script>`), "namespace", 0},
		{"xhtml_default", inG(`<script xmlns="http://www.w3.org/1999/xhtml">alert(1)</script>`), "namespace", 0},
		{"prefixed_xmlns_on_root", rep(root, root+` xmlns:x="urn:x"`), "namespace declaration", 0},
		{"wrong_root_namespace", rep(`xmlns="http://www.w3.org/2000/svg"`, `xmlns="http://www.w3.org/2000/svgx"`), "namespace", 0},
		{"id", rep(`stroke-width="8" />`, `stroke-width="8" id="a" />`), "attribute id", 0},
		{"class", rep(`stroke-width="8" />`, `stroke-width="8" class="a" />`), "attribute class", 0},
		{"transform_translate", rep("matrix(1 0 0 1 725.309 920.104)", "translate(1 2)"), "attribute transform", 0},
		{"transform_five_numbers", rep("matrix(1 0 0 1 725.309 920.104)", "matrix(1 0 0 1 725.309)"), "attribute transform", 0},
		{"transform_on_path", rep(`stroke-width="8" />`, `stroke-width="8" transform="matrix(1 0 0 1 0 0)" />`), "attribute transform", 0},
		{"font_family", rep(`font-family="DejaVu Sans"`, `font-family="Arial"`), "attribute font-family", 0},
		{"font_weight_normal", rep(textOpen, textOpen+` font-weight="normal"`), "attribute font-weight", 0},
		{"fill_url", rep(`fill="none"`, `fill="url(#x)"`), "attribute fill", 0},
		{"opacity_over_one", rep(`fill-opacity="0.0"`, `fill-opacity="1.5"`), "attribute fill-opacity", 0},
		{"path_d_arc", rep(`d="M 725 3835 l 777 0"`, `d="M 725 3835 A 1 1 0 0 1 777 0"`), "attribute d", 0},
		{"path_d_exponent", rep(`d="M 725 3835 l 777 0"`, `d="M 725 3835 l 7e2 0"`), "attribute d", 0},
		{"width_without_mm", rep(`width="297mm"`, `width="297"`), "attribute width", 0},
		{"width_px", rep(`width="297mm"`, `width="297px"`), "attribute width", 0},
		{"viewbox_offset", rep(`viewBox="0 0 10000 5532"`, `viewBox="1 0 10000 5532"`), "attribute viewBox", 0},
		{"viewbox_fraction", rep(`viewBox="0 0 10000 5532"`, `viewBox="0 0 10000.5 5532"`), "attribute viewBox", 0},
		{"missing_viewbox", rep(` viewBox="0 0 10000 5532"`, ""), "viewBox", 0},
		{"duplicate_attribute", rep(`fill="none"`, `fill="none" fill="#000000"`), "duplicate attribute", 0},
		{"invalid_utf8", rep(">A&amp;B<", ">A\xff\xfeB<"), "UTF-8", 0},
		{"control_character", rep(">A&amp;B<", ">A\x01B<"), "malformed XML", 0},
		{"undefined_entity", rep(">A&amp;B<", ">A&ent;B<"), "malformed XML", 0},
		{"depth_17", rep(path, strings.Repeat("<g>", 14)+`<path d="M 0 0" />`+strings.Repeat("</g>", 14)), "depth", 0},
		{"one_byte_over", cadSVGBase, "bytes", int64(len(cadSVGBase) - 1)},
		{"root_not_svg", decl + `<g xmlns="http://www.w3.org/2000/svg" />`, "root", 0},
		{"nested_svg", inG(`<svg width="1mm" height="1mm" viewBox="0 0 1 1" />`), "element svg", 0},
		{"xmlns_on_child", inG(`<g xmlns="http://www.w3.org/2000/svg" />`), "namespace declaration", 0},
		{"anchor_in_text", rep(">A&amp;B<", "><a>x</a><"), "element a", 0},
		{"path_in_text", rep(">A&amp;B<", `><path d="M 0 0" /><`), "element path", 0},
		{"clip_path_with_id", rep("<defs />", `<defs><clipPath id="c" /></defs>`), "attribute id", 0},
		{"clip_path_outside_defs", inG(`<clipPath />`), "element clipPath", 0},
		{"unclosed_root", strings.TrimSuffix(cadSVGBase, "</svg>\n"), "malformed XML", 0},
		{"empty_input", "", "root", 0},
	}
	if len(cases) < 30 {
		t.Fatalf("only %d mutation rows", len(cases))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.name] {
			t.Fatalf("duplicate row %s", c.name)
		}
		seen[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			max := c.max
			if max == 0 {
				max = 1 << 20
			}
			_, facts, err := cadSVGValidate(t, []byte(c.in), max)
			if err == nil {
				t.Fatalf("accepted (facts %+v), want a %q rejection", facts, c.rule)
			}
			if !errors.Is(err, ErrCADSVGRejected) {
				t.Fatalf("err = %v, want it to wrap ErrCADSVGRejected", err)
			}
			if !strings.Contains(err.Error(), c.rule) {
				t.Fatalf("err = %v, want it to name %q", err, c.rule)
			}
			if len(facts.Texts) != 0 || facts.WidthMM != 0 {
				t.Fatalf("facts %+v returned with a rejection", facts)
			}
		})
	}
}

func TestValidateCADSVGBaseIsValid(t *testing.T) {
	out, facts, err := cadSVGValidate(t, []byte(cadSVGBase), int64(len(cadSVGBase)))
	if err != nil {
		t.Fatalf("the mutation base is rejected: %v", err)
	}
	if facts.WidthMM != 297 || facts.HeightMM != 164.3 || len(facts.Texts) != 1 || facts.Texts[0] != "A&B" {
		t.Fatalf("facts = %+v", facts)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="297mm" height="164.3mm" viewBox="0 0 10000 5532"><defs></defs><rect fill="#ffffff" x="0" y="0" width="10000" height="5532" fill-opacity="0.0"></rect><g stroke-linecap="round" stroke-linejoin="round" fill-rule="evenodd"><path d="M 725 3835 l 777 0" fill="none" stroke="#000000" stroke-width="8"></path><text transform="matrix(1 0 0 1 725.309 920.104)" font-family="DejaVu Sans" font-size="133.267" fill="#000000" xml:space="preserve">A&amp;B</text></g></svg>`
	if string(out) != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out, want)
	}
}

type cadSVGFailingReader struct{ err error }

func (r cadSVGFailingReader) Read([]byte) (int, error) { return 0, r.err }

func TestValidateCADSVGReportsReadErrorsAsReadErrors(t *testing.T) {
	boom := errors.New("disk on fire")
	_, err := ValidateCADSVG(cadSVGFailingReader{boom}, io.Discard, 1<<20)
	if !errors.Is(err, boom) || errors.Is(err, ErrCADSVGRejected) {
		t.Fatalf("err = %v, want the read error and no rejection", err)
	}
	if _, err := ValidateCADSVG(strings.NewReader(cadSVGBase), io.Discard, 0); err == nil || errors.Is(err, ErrCADSVGRejected) {
		t.Fatalf("a zero limit gave %v, want a usage error", err)
	}
}

func TestCADSVGRejectionQuotesAtMostFortyCharacters(t *testing.T) {
	long := strings.Repeat("x", 200)
	_, _, err := cadSVGValidate(t, []byte(strings.Replace(cadSVGBase, `font-family="DejaVu Sans"`, `font-family="`+long+`"`, 1)), 1<<20)
	if err == nil || strings.Contains(err.Error(), strings.Repeat("x", 41)) {
		t.Fatalf("err = %v, want a rejection quoting at most 40 characters", err)
	}
}

func TestValidateCADSVGReportsTheEffectiveTextOpacity(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="10mm" viewBox="0 0 100 100">` +
		`<text font-size="2">plain</text>` +
		`<text font-size="2" fill-opacity="0.498">half</text>` +
		`<text font-size="2" fill-opacity="0.000">clear</text>` +
		`<text font-size="2" fill="none">unfilled</text>` +
		`<g fill-opacity="0"><text font-size="2">inherited</text><text font-size="2" fill-opacity="1">own</text></g>` +
		`<g fill="none"><text font-size="2">inherited none</text><text font-size="2" fill="#000000">own fill</text></g>` +
		`<text font-size="2" fill-opacity="0">a<tspan fill-opacity="0.5">b</tspan></text>` +
		`<text font-size="2">a<tspan fill-opacity="0">b</tspan></text>` +
		`<g fill-opacity="0.25"><g><text font-size="2">deep</text></g></g>` +
		`</svg>`
	facts := cadSVGFactsOf(t, svg)
	want := []float64{1, 0.498, 0, 0, 0, 1, 0, 1, 0.5, 1, 0.25}
	if len(facts.TextOpacity) != len(want) || len(facts.TextOpacity) != len(facts.Texts) {
		t.Fatalf("TextOpacity = %v for %d texts, want %v", facts.TextOpacity, len(facts.Texts), want)
	}
	for i, w := range want {
		if facts.TextOpacity[i] != w {
			t.Errorf("text %d (%q) opacity = %v, want %v", i, facts.Texts[i], facts.TextOpacity[i], w)
		}
	}
}

func TestValidateCADSVGTextFillOpacityIsAUnitNumber(t *testing.T) {
	textOpen := `<text transform="matrix(1 0 0 1 725.309 920.104)" font-family="DejaVu Sans"`
	with := func(v string) string {
		return strings.Replace(cadSVGBase, textOpen, textOpen+` fill-opacity="`+v+`"`, 1)
	}
	for _, v := range []string{"0", "0.000", "0.498", "1", "1.0", ".5", "1e0", "5e-1", "-0"} {
		t.Run("accept_"+v, func(t *testing.T) {
			_, facts, err := cadSVGValidate(t, []byte(with(v)), 1<<20)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			want, _ := strconv.ParseFloat(v, 64)
			if len(facts.TextOpacity) != 1 || facts.TextOpacity[0] != want {
				t.Errorf("TextOpacity = %v, want [%v]", facts.TextOpacity, want)
			}
		})
	}
	for _, v := range []string{"1.001", "-0.1", "2", "abc", "50%", "calc(0.5)", "0.5 ", " 0.5", "+0.5", "0.5;", "", "NaN", "inf",
		"1e400", "0x1", "var(--a)", "0,5", "1/2", "0.5px", "1 0"} {
		t.Run("reject_"+v, func(t *testing.T) {
			_, _, err := cadSVGValidate(t, []byte(with(v)), 1<<20)
			if !errors.Is(err, ErrCADSVGRejected) || !strings.Contains(err.Error(), "attribute fill-opacity") {
				t.Fatalf("err = %v, want a fill-opacity grammar rejection", err)
			}
		})
	}
}

func TestValidateCADSVGCarriesTheHelperOpacityOnTextsAndShapes(t *testing.T) {
	tf := cadDXFReadTexts(t, cadDXFTestFile("text_alpha.texts.json"))
	want := []float64{1, 0.498, 0, 1, 0.498, 0, 0.498, 0.498, 0.498, 0.498, 0, 0, 0.498, 0.498}
	if len(tf.Texts) != len(want) {
		t.Fatalf("texts.json holds %d texts, want %d", len(tf.Texts), len(want))
	}
	for i, e := range tf.Texts {
		if e.Invisible != (want[i] == 0) {
			t.Errorf("texts.json entry %d (%q) invisible = %v, want %v", i, e.Text, e.Invisible, want[i] == 0)
		}
	}
	for _, name := range []string{"text_alpha.svg", "text_alpha.render.svg"} {
		in, err := os.ReadFile(cadDXFTestFile(name))
		if err != nil {
			t.Fatal(err)
		}
		_, facts, err := cadSVGValidate(t, in, int64(len(in)))
		if err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
		for i, w := range want {
			if i >= len(facts.TextOpacity) || facts.TextOpacity[i] != w {
				t.Fatalf("%s: TextOpacity = %v, want %v", name, facts.TextOpacity, want)
			}
		}
		strokes := regexp.MustCompile(`<path d="[^"]*" fill="none" stroke="#000000" stroke-width="\d+" stroke-opacity="0\.498" />`).FindAll(in, -1)
		fills := regexp.MustCompile(`<path d="[^"]*" fill="#ff0000" fill-opacity="0\.498" />`).FindAll(in, -1)
		if len(strokes) != 2 || len(fills) != 1 {
			t.Errorf("%s: %d half-transparent strokes and %d half-transparent fills, want the LINE and the XLINE and the HATCH", name, len(strokes), len(fills))
		}
	}
}

func TestValidateCADSVGFontAttributesAreStrictEnums(t *testing.T) {
	textOpen := `<text transform="matrix(1 0 0 1 725.309 920.104)" font-family="DejaVu Sans"`
	onText := func(attr string) string { return strings.Replace(cadSVGBase, textOpen, textOpen+" "+attr, 1) }
	onTspan := func(attr string) string {
		return strings.Replace(cadSVGBase, `A&amp;B</text>`, `A<tspan `+attr+`>&amp;</tspan>B</text>`, 1)
	}
	accept := []string{
		`font-style="italic"`, `font-style="oblique"`,
		`font-weight="bold"`, `font-weight="100"`, `font-weight="200"`, `font-weight="300"`, `font-weight="500"`,
		`font-weight="600"`, `font-weight="800"`, `font-weight="900"`,
		`font-stretch="ultra-condensed"`, `font-stretch="extra-condensed"`, `font-stretch="condensed"`,
		`font-stretch="semi-condensed"`, `font-stretch="semi-expanded"`, `font-stretch="expanded"`,
		`font-stretch="extra-expanded"`, `font-stretch="ultra-expanded"`,
	}
	reject := map[string]string{
		`font-style="normal"`:           "font-style",
		`font-style="Italic"`:           "font-style",
		`font-style="oblique 10deg"`:    "font-style",
		`font-style=" italic"`:          "font-style",
		`font-style="italic "`:          "font-style",
		`font-style="inherit"`:          "font-style",
		`font-style=""`:                 "font-style",
		`font-weight="normal"`:          "font-weight",
		`font-weight="400"`:             "font-weight",
		`font-weight="700"`:             "font-weight",
		`font-weight="bolder"`:          "font-weight",
		`font-weight="350"`:             "font-weight",
		`font-weight="1000"`:            "font-weight",
		`font-weight="Bold"`:            "font-weight",
		`font-weight="100.0"`:           "font-weight",
		`font-stretch="normal"`:         "font-stretch",
		`font-stretch="75%"`:            "font-stretch",
		`font-stretch="narrower"`:       "font-stretch",
		`font-stretch="Condensed"`:      "font-stretch",
		`font-stretch="condensed "`:     "font-stretch",
		`font-variant="small-caps"`:     "font-variant",
		`font="italic 2px DejaVu Sans"`: "font is not allowed",
	}
	for _, a := range accept {
		for where, svg := range map[string]string{"text": onText(a), "tspan": onTspan(a)} {
			t.Run("accept_"+where+"_"+a, func(t *testing.T) {
				if _, _, err := cadSVGValidate(t, []byte(svg), 1<<20); err != nil {
					t.Fatalf("rejected: %v", err)
				}
			})
		}
	}
	for a, rule := range reject {
		for where, svg := range map[string]string{"text": onText(a), "tspan": onTspan(a)} {
			t.Run("reject_"+where+"_"+a, func(t *testing.T) {
				_, _, err := cadSVGValidate(t, []byte(svg), 1<<20)
				if !errors.Is(err, ErrCADSVGRejected) || !strings.Contains(err.Error(), "attribute "+rule) {
					t.Fatalf("err = %v, want a %s rejection", err, rule)
				}
			})
		}
	}
	for _, a := range []string{`font-style="italic"`, `font-stretch="condensed"`, `font-weight="200"`} {
		t.Run("not_on_shapes_"+a, func(t *testing.T) {
			svg := strings.Replace(cadSVGBase, `stroke-width="8" />`, `stroke-width="8" `+a+` />`, 1)
			if _, _, err := cadSVGValidate(t, []byte(svg), 1<<20); !errors.Is(err, ErrCADSVGRejected) {
				t.Fatalf("err = %v, want a rejection on a path", err)
			}
		})
	}
}

func TestValidateCADSVGReportsEachTextsFont(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="10mm" viewBox="0 0 100 100">` +
		`<text font-family="DejaVu Sans" font-size="2">plain</text>` +
		`<text font-family="DejaVu Sans Mono" font-size="2" font-weight="bold" font-style="oblique">mono</text>` +
		`<text font-size="3" font-family="DejaVu Serif" font-style="italic" font-stretch="semi-condensed" font-weight="200">serif</text>` +
		`<text font-family="DejaVu Sans" font-size="2">a<tspan font-style="oblique">b</tspan>c<tspan font-weight="bold" font-size="4">d</tspan></text>` +
		`<g><text font-family="DejaVu Sans" font-size="2" fill-opacity="0.5">faint</text></g>` +
		`</svg>`
	facts := cadSVGFactsOf(t, svg)
	want := []string{
		`font-family="DejaVu Sans" font-size="2"`,
		`font-family="DejaVu Sans Mono" font-size="2" font-weight="bold" font-style="oblique"`,
		`font-family="DejaVu Serif" font-size="3" font-weight="200" font-style="italic" font-stretch="semi-condensed"`,
		`font-family="DejaVu Sans" font-size="2" | font-style="oblique" | font-size="4" font-weight="bold"`,
		`font-family="DejaVu Sans" font-size="2"`,
	}
	if len(facts.TextFonts) != len(want) || len(facts.TextFonts) != len(facts.Texts) {
		t.Fatalf("TextFonts = %q for %d texts, want %q", facts.TextFonts, len(facts.Texts), want)
	}
	for i, w := range want {
		if facts.TextFonts[i] != w {
			t.Errorf("text %d (%q) font = %q, want %q", i, facts.Texts[i], facts.TextFonts[i], w)
		}
	}
}

func TestValidateCADSVGCarriesTheMeasuredFontOfEveryTextFontsText(t *testing.T) {
	want := []string{
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono" font-weight="bold"`,
		`font-family="DejaVu Sans"`,
		`font-family="DejaVu Sans"`,
		`font-family="DejaVu Sans"`,
		`font-family="DejaVu Sans" font-weight="bold"`,
		`font-family="DejaVu Serif"`,
		`font-family="DejaVu Serif" font-weight="bold"`,
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono"`,
		`font-family="DejaVu Sans Mono"`,
	}
	size := regexp.MustCompile(` font-size="[^"]*"`)
	var client []string
	for _, name := range []string{"text_fonts.svg", "text_fonts.render.svg"} {
		in, err := os.ReadFile(cadDXFTestFile(name))
		if err != nil {
			t.Fatal(err)
		}
		_, facts, err := cadSVGValidate(t, in, int64(len(in)))
		if err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
		if len(facts.TextFonts) != len(want) {
			t.Fatalf("%s: %d text fonts %q, want %d", name, len(facts.TextFonts), facts.TextFonts, len(want))
		}
		for i, w := range want {
			if got := size.ReplaceAllString(facts.TextFonts[i], ""); got != w {
				t.Errorf("%s: text %d (%q) font = %q, want %q", name, i, facts.Texts[i], got, w)
			}
		}
		if client == nil {
			client = facts.TextFonts
		} else if strings.Join(client, "\x00") != strings.Join(facts.TextFonts, "\x00") {
			t.Errorf("the render-only SVG fonts %q differ from the client SVG fonts %q", facts.TextFonts, client)
		}
	}
}
