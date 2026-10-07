package convert

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func cadPDFStream(dict string, data []byte, flate bool) string {
	if flate {
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		zw.Write(data)
		zw.Close()
		data = z.Bytes()
		dict += " /Filter /FlateDecode"
	}
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

func cadPDFFile(objs ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.5\n%\xb5\xed\xae\xfb\n")
	var offs []int
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

const (
	cadPDFIdentityCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /X def\n1 begincodespacerange <00> <ff> endcodespacerange\n1 beginbfrange\n<00> <ff> <0000>\nendbfrange\nendcmap end end\n"
	cadPDFType0CMap    = "begincmap\n1 begincodespacerange <0000> <ffff> endcodespacerange\n1 beginbfchar\n<0001> <d83dde00>\nendbfchar\n2 beginbfrange\n<0002> <0004> <0041>\n<0005> <0006> [<0058> <0059>]\nendbfrange\nendcmap\n"
)

func cadPDFWithFonts(content string, fonts string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page % 1\n   /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /Font << " + fonts + " >> >> >>",
		cadPDFStream("", []byte(content), true),
	}
	return cadPDFFile(append(objs, extra...)...)
}

func cadPDFTwoFonts(content string) []byte {
	return cadPDFWithFonts(content, "/F1 5 0 R /F2 7 0 R",
		"<< /Type /Font /Subtype /TrueType /BaseFont /X /Encoding /WinAnsiEncoding /ToUnicode 6 0 R >>",
		cadPDFStream("", []byte(cadPDFIdentityCMap), true),
		"<< /Type /Font /Subtype /Type0 /BaseFont /Y /Encoding /Identity-H /DescendantFonts [] /ToUnicode 8 0 R >>",
		cadPDFStream("", []byte(cadPDFType0CMap), false),
	)
}

func cadPDFWrite(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "draw.pdf")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func cadPDFStrip(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func cadPDFFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cadSVGTestDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cadPDFRequireRejected(t *testing.T, err error, rule string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted, want a %q rejection", rule)
	}
	if !errors.Is(err, ErrCADPDFRejected) {
		t.Fatalf("err = %v, want it to wrap ErrCADPDFRejected", err)
	}
	if !strings.Contains(err.Error(), rule) {
		t.Fatalf("err = %v, want it to name %q", err, rule)
	}
}

func TestJudgeCADPDFAcceptsHelperPDFsWithTheirTexts(t *testing.T) {
	for _, name := range []string{"text_cases", "legacy_cp1251", "uncommon", "hatches_2"} {
		t.Run(name, func(t *testing.T) {
			texts, err := parseCADDXFTexts(filepath.Join(cadSVGTestDir, name+".texts.json"), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			want := cadDXFExpectedText(texts)
			if want == "" {
				t.Fatal("the fixture has no expected text: the case would prove nothing")
			}
			p := filepath.Join(cadSVGTestDir, name+".pdf")
			st, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := JudgeCADPDF(p, st.Size(), want)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if facts.Pages != 1 || facts.PaintOps <= 1 {
				t.Errorf("facts = %+v, want one page and more than one paint operation", facts)
			}
			if facts.ExpectedRunes != utf8.RuneCountInString(want) || facts.TextRunes < facts.ExpectedRunes {
				t.Errorf("facts = %+v for %d expected runes", facts, utf8.RuneCountInString(want))
			}
			if name == "legacy_cp1251" && !strings.Contains(facts.Text, "Деталь") {
				t.Errorf("text = %q, want Деталь", facts.Text)
			}
			if name == "text_cases" && !strings.Contains(cadPDFStrip(facts.Text), cadPDFStrip(`A&B <x> "q" 'a' & <script>alert(1)</script>`)) {
				t.Errorf("text = %q lacks the special string", facts.Text)
			}
		})
	}
}

func TestJudgeCADPDFRejectsTextlessPDFsOfATextDrawing(t *testing.T) {
	texts, err := parseCADDXFTexts(filepath.Join(cadSVGTestDir, "text_cases.texts.json"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := cadDXFExpectedText(texts)
	for _, name := range []string{"nofont.pdf", "outline.pdf"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(cadSVGTestDir, name)
			facts, err := JudgeCADPDF(p, 1<<20, want)
			cadPDFRequireRejected(t, err, "missing text")
			if facts.TextRunes != 0 {
				t.Errorf("decoded %d runes from a PDF without text", facts.TextRunes)
			}
			facts, err = JudgeCADPDF(p, 1<<20, "")
			if err != nil {
				t.Fatalf("with no expected text: %v", err)
			}
			if facts.Pages != 1 || facts.PaintOps <= 1 {
				t.Errorf("facts = %+v", facts)
			}
		})
	}
}

func TestJudgeCADPDFDecodesEveryStringFormAndBothFontKinds(t *testing.T) {
	content := `BT /F1 12 Tf (a\(b\)\\c\101\012\
d) Tj <414 243> Tj [(x) -200 (y)] TJ (z) ' 1 2 (w) " (p(q)r) Tj
/F2 10 Tf <00010002 0003> Tj [<0005> 10 <0006>] TJ ET 0 0 m 10 10 l S`
	facts, err := JudgeCADPDF(cadPDFWrite(t, cadPDFTwoFonts(content)), 1<<20, "abcAd😀XY")
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if got, want := cadPDFStrip(facts.Text), `a(b)\cAdABCxyzwp(q)r😀ABXY`; got != want {
		t.Fatalf("decoded %q, want %q", got, want)
	}
	if !strings.Contains(facts.Text, "A\nd") {
		t.Errorf("octal \\012 and the line continuation decoded to %q", facts.Text)
	}
	if facts.PaintOps != 9 {
		t.Errorf("paint ops = %d, want 8 text shows and 1 stroke", facts.PaintOps)
	}
	_, err = JudgeCADPDF(cadPDFWrite(t, cadPDFTwoFonts(content)), 1<<20, "XYa")
	cadPDFRequireRejected(t, err, "missing text")
}

func TestJudgeCADPDFRejectsStructuresItDoesNotAccept(t *testing.T) {
	real := cadPDFFixture(t, "text_cases.pdf")
	insert := func(obj string) []byte {
		i := bytes.LastIndex(real, []byte("\nxref\n"))
		return append(append(append([]byte{}, real[:i+1]...), obj...), real[i+1:]...)
	}
	lengthObj := regexp.MustCompile(`(?s)\n5 0 obj\s+(\d+)`)
	m := lengthObj.FindSubmatchIndex(real)
	n, _ := strconv.Atoi(string(real[m[2]:m[3]]))
	wrongLength := append(append(append([]byte{}, real[:m[2]]...), strconv.Itoa(n-1)...), real[m[3]:]...)
	stroke := "0 0 m 10 10 l S"
	big := strings.Repeat(" ", 1<<20) + stroke
	bigPDF := cadPDFWithFonts(big, "")
	cases := []struct {
		name string
		data []byte
		max  int64
		rule string
	}{
		{"not_pdf", append([]byte("x"), real...), 0, "%PDF-"},
		{"two_page_objects", insert("21 0 obj\n<< /Type /Page /Parent 1 0 R /Contents 4 0 R /Resources 3 0 R >>\nendobj\n"), 0, "page objects"},
		{"count_2", bytes.Replace(real, []byte("/Count 1"), []byte("/Count 2"), 1), 0, "/Count"},
		{"object_stream", insert("21 0 obj\n<< /Type /ObjStm /N 0 /First 0 /Length 0 >>\nstream\n\nendstream\nendobj\n"), 0, "ObjStm"},
		{"xref_stream", insert("21 0 obj\n<< /Type /XRef /Size 1 /W [1 1 1] /Length 0 >>\nstream\n\nendstream\nendobj\n"), 0, "XRef"},
		{"encrypted", bytes.Replace(real, []byte("/Root 20 0 R"), []byte("/Root 20 0 R /Encrypt 19 0 R"), 1), 0, "Encrypt"},
		{"duplicate_object", insert("19 0 obj\n<< >>\nendobj\n"), 0, "twice"},
		{"trailing_data", append(append([]byte{}, real...), "junk\n"...), 0, "unexpected"},
		{"stream_length_mismatch", wrongLength, 0, "length"},
		{"over_max_bytes", real, int64(len(real) - 1), "bytes"},
		{"background_only", cadPDFWithFonts("q 1 1 1 rg 0 0 100 100 re f Q", ""), 0, "empty page"},
		{"no_contents", cadPDFFile("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>"), 0, "empty page"},
		{"lzw_content", cadPDFFile("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>", cadPDFStream("/Filter /LZWDecode", []byte(stroke), false)), 0, "filter"},
		{"decode_parms", cadPDFFile("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>", cadPDFStream("/DecodeParms << /Predictor 12 >>", []byte(stroke), true)), 0, "DecodeParms"},
		{"inflated_over_cap", bigPDF, int64(len(bigPDF)), "inflated"},
		{"xobject", cadPDFWithFonts("/X0 Do "+stroke, ""), 0, "operator Do"},
		{"inline_image", cadPDFWithFonts("BI /W 1 /H 1 /BPC 8 /CS /G ID x EI "+stroke, ""), 0, "operator BI"},
		{"shading", cadPDFWithFonts("/Sh0 sh "+stroke, ""), 0, "operator sh"},
		{"font_without_tounicode", cadPDFWithFonts("BT /F1 12 Tf (a) Tj ET "+stroke, "/F1 5 0 R", "<< /Type /Font /Subtype /TrueType /BaseFont /X >>"), 0, "ToUnicode"},
		{"unknown_font_name", cadPDFTwoFonts("BT /F9 12 Tf (a) Tj ET " + stroke), 0, "font /F9"},
		{"text_without_font", cadPDFTwoFonts("BT (a) Tj ET " + stroke), 0, "without a font"},
		{"type0_not_identity", cadPDFWithFonts("BT /F1 12 Tf <0001> Tj ET "+stroke, "/F1 5 0 R", "<< /Type /Font /Subtype /Type0 /Encoding /UniJIS-UCS2-H /ToUnicode 6 0 R >>", cadPDFStream("", []byte(cadPDFType0CMap), false)), 0, "encoding"},
		{"odd_two_byte_string", cadPDFTwoFonts("BT /F2 12 Tf <000100> Tj ET " + stroke), 0, "two-byte"},
		{"unbalanced_string", cadPDFWithFonts("BT (a Tj ET "+stroke, ""), 0, "string"},
		{"no_trailer", bytes.Split(cadPDFWithFonts(stroke, ""), []byte("xref\n"))[0], 0, "trailer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			max := c.max
			if max == 0 {
				max = 1 << 20
			}
			_, err := JudgeCADPDF(cadPDFWrite(t, c.data), max, "")
			cadPDFRequireRejected(t, err, c.rule)
		})
	}
	if _, err := JudgeCADPDF(cadPDFWrite(t, cadPDFWithFonts("1 1 1 rg 0 0 100 100 re f "+stroke, "")), 1<<20, ""); err != nil {
		t.Fatalf("the synthetic base is rejected: %v", err)
	}
	if _, err := JudgeCADPDF(cadPDFWrite(t, real), int64(len(real)), ""); err != nil {
		t.Fatalf("the real base is rejected at its exact size: %v", err)
	}
}

func TestJudgeCADPDFSeparatesUsageAndReadErrorsFromRejections(t *testing.T) {
	_, err := JudgeCADPDF(filepath.Join(t.TempDir(), "missing.pdf"), 1<<20, "")
	if err == nil || errors.Is(err, ErrCADPDFRejected) {
		t.Fatalf("a missing file gave %v, want a plain error", err)
	}
	_, err = JudgeCADPDF(filepath.Join(cadSVGTestDir, "text_cases.pdf"), 0, "")
	if err == nil || errors.Is(err, ErrCADPDFRejected) {
		t.Fatalf("a zero limit gave %v, want a usage error", err)
	}
}

func cadTextsWrite(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "texts.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCADDXFTextsParsesTheHelperContract(t *testing.T) {
	texts, err := parseCADDXFTexts(cadTextsWrite(t, `{"texts":[{"text":"A b\tc","covered":"A b\tc"},{"text":"Lat中文Ωend","covered":"LatΩend"},{"text":"x","covered":""}]}`+"\n"), 1<<10)
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 3 || texts[1].Text != "Lat中文Ωend" || texts[1].Covered != "LatΩend" {
		t.Fatalf("texts = %+v", texts)
	}
	if got := cadDXFExpectedText(texts); got != "AbcLatΩend" {
		t.Fatalf("expected text = %q", got)
	}
	empty, err := parseCADDXFTexts(cadTextsWrite(t, `{"texts":[]}`), 1<<10)
	if err != nil || len(empty) != 0 || cadDXFExpectedText(empty) != "" {
		t.Fatalf("empty texts: %+v %v", empty, err)
	}
	all, err := parseCADDXFTexts(filepath.Join(cadSVGTestDir, "uncommon.texts.json"), 1<<20)
	if err != nil || len(all) != 49 {
		t.Fatalf("uncommon: %d texts, %v", len(all), err)
	}
	got, err := CADDXFExpectedTextFile(filepath.Join(cadSVGTestDir, "legacy_cp1251.texts.json"), 1<<20)
	if err != nil || got != "Деталь" {
		t.Fatalf("CADDXFExpectedTextFile = %q, %v", got, err)
	}
}

func TestCADDXFTextsRejectsWhatTheContractDoesNotAllow(t *testing.T) {
	cases := []struct{ name, body, rule string }{
		{"unknown_top_key", `{"texts":[],"extra":1}`, "unknown field"},
		{"unknown_entry_key", `{"texts":[{"text":"a","covered":"a","x":1}]}`, "unknown field"},
		{"trailing_object", `{"texts":[]}{"texts":[]}`, "trailing"},
		{"trailing_garbage", `{"texts":[]} x`, "trailing"},
		{"not_subsequence", `{"texts":[{"text":"abc","covered":"ca"}]}`, "subsequence"},
		{"covered_extra", `{"texts":[{"text":"ab","covered":"abc"}]}`, "subsequence"},
		{"missing_texts", `{}`, "texts"},
		{"null_texts", `{"texts":null}`, "texts"},
		{"missing_covered", `{"texts":[{"text":"a"}]}`, "covered"},
		{"missing_text", `{"texts":[{"covered":""}]}`, "text"},
		{"not_json", `texts`, "texts.json"},
		{"over_limit", `{"texts":[{"text":"` + strings.Repeat("a", 2000) + `","covered":""}]}`, "bytes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseCADDXFTexts(cadTextsWrite(t, c.body), 1<<10)
			cadPDFRequireRejected(t, err, c.rule)
		})
	}
	if _, err := parseCADDXFTexts(filepath.Join(t.TempDir(), "none.json"), 1<<10); err == nil || errors.Is(err, ErrCADPDFRejected) {
		t.Fatalf("a missing file gave %v, want a plain error", err)
	}
}
