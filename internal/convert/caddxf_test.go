package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apaderin/octoconv/internal/measurements"
)

const cadDXFCRDir = "testdata/cad/dxf-cr"

type cadDXFOutputs struct {
	client, render, texts, measure string
	verdict                        string
	exit                           int
	pre, post                      string
}

type cadDXFFixture struct{ cadFixture }

func newCADDXFFixture(t *testing.T, target string) cadDXFFixture {
	t.Helper()
	f := newCADFixture(t, "dxf", cadDXFGuard(cadOKGuard()), "")
	f.out = filepath.Join(f.jobDir, "out."+target)
	return cadDXFFixture{f}
}

func cadDXFGuard(body string) string {
	return "echo \"$@\" > \"$(dirname \"$0\")/guard.args\"\n" + body
}

func cadDXFTestFile(name string) string { return filepath.Join(cadSVGTestDir, name) }

func cadDXFQuote(p string) string { return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'" }

func (f cadDXFFixture) setDXF(t *testing.T, o cadDXFOutputs) {
	t.Helper()
	var b strings.Builder
	b.WriteString("echo \"$@\" > \"$(dirname \"$0\")/dxf.args\"\n")
	b.WriteString(o.pre + "\n")
	b.WriteString("T=''; R=''; M=''\n")
	b.WriteString("while [ $# -gt 2 ]; do\n  case \"$1\" in\n    --texts) T=$2; shift 2 ;;\n    --render-svg) R=$2; shift 2 ;;\n    --measure) M=$2; shift 2 ;;\n    *) shift ;;\n  esac\ndone\n")
	if o.client != "" {
		fmt.Fprintf(&b, "cp %s \"$2\"\n", cadDXFQuote(o.client))
	}
	if o.render != "" {
		fmt.Fprintf(&b, "cp %s \"$R\"\n", cadDXFQuote(o.render))
	}
	if o.texts != "" {
		fmt.Fprintf(&b, "cp %s \"$T\"\n", cadDXFQuote(o.texts))
	}
	if o.measure != "" {
		fmt.Fprintf(&b, "if [ -n \"$M\" ]; then cp %s \"$M\"; fi\n", cadDXFQuote(o.measure))
	}
	b.WriteString(o.post + "\n")
	fmt.Fprintf(&b, "printf '%%s\\n' %s\nexit %d\n", cadDXFQuote(o.verdict), o.exit)
	fakeEngine(t, f.dir, "cad-dxf", b.String())
}

func (f cadDXFFixture) setRsvg(t *testing.T, src string, exit int, pre string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("echo \"$@\" > \"$(dirname \"$0\")/rsvg.args\"\n")
	b.WriteString(pre + "\n")
	b.WriteString("o=''\nwhile [ $# -gt 0 ]; do\n  case \"$1\" in\n    -o) o=$2; shift 2 ;;\n    *) shift ;;\n  esac\ndone\n")
	if src != "" {
		fmt.Fprintf(&b, "cp %s \"$o\"\n", cadDXFQuote(src))
	}
	fmt.Fprintf(&b, "exit %d\n", exit)
	fakeEngine(t, f.dir, "rsvg-convert", b.String())
}

func (f cadDXFFixture) args(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(f.marker(name))
	if err != nil {
		t.Fatalf("the stub %s never ran: %v", name, err)
	}
	return strings.TrimSpace(string(raw))
}

func (f cadDXFFixture) noOutput(t *testing.T) {
	t.Helper()
	if _, err := os.Lstat(f.out); err == nil {
		t.Errorf("%s exists after a failed conversion", f.out)
	}
}

func (f cadDXFFixture) job(name string) string { return filepath.Join(f.jobDir, name) }

func cadDXFReadTexts(t *testing.T, path string) cadSVGTextsFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tf cadSVGTextsFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatal(err)
	}
	return tf
}

func cadDXFVerdictFor(t *testing.T, svgPath, textsPath string) string {
	t.Helper()
	data, err := os.ReadFile(svgPath)
	if err != nil {
		t.Fatal(err)
	}
	w, h := cadSVGRootMM(t, data, "width"), cadSVGRootMM(t, data, "height")
	return cadDXFVerdict(t, textsPath, w, h, -1)
}

func cadDXFVerdict(t *testing.T, textsPath string, w, h float64, texts int) string {
	t.Helper()
	tf := cadDXFReadTexts(t, textsPath)
	uncovered, substituted := 0, 0
	for _, e := range tf.Texts {
		uncovered += utf8.RuneCountInString(e.Text) - utf8.RuneCountInString(e.Covered)
		covered := map[rune]bool{}
		for _, r := range e.Covered {
			covered[r] = true
		}
		for _, r := range e.Text {
			if !covered[r] && r != '�' {
				substituted++
			}
		}
	}
	if texts < 0 {
		texts = len(tf.Texts)
	}
	return fmt.Sprintf(`CADVERDICT {"stage":"dxf","verdict":"ok","reason":"","texts":%d,"uncovered":%d,"render_substituted":%d,"page_w_mm":%s,"page_h_mm":%s}`,
		texts, uncovered, substituted, strconv.FormatFloat(w, 'g', -1, 64), strconv.FormatFloat(h, 'g', -1, 64))
}

func cadDXFFixtureOutputs(t *testing.T, name string) cadDXFOutputs {
	t.Helper()
	o := cadDXFOutputs{
		client: cadDXFTestFile(name + ".svg"),
		render: cadDXFTestFile(name + ".render.svg"),
		texts:  cadDXFTestFile(name + ".texts.json"),
	}
	o.verdict = cadDXFVerdictFor(t, o.client, o.texts)
	return o
}

func cadDXFValidated(t *testing.T, path string) []byte {
	t.Helper()
	in, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := cadSVGValidate(t, in, CADMaxDXFSVGBytes)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}

func cadDXFWrite(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func cadDXFMutate(t *testing.T, dir, src, name, old, repl string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("%s holds no %q", src, old)
	}
	return cadDXFWrite(t, dir, name, bytes.Replace(data, []byte(old), []byte(repl), 1))
}

func TestCADDXFConvertPairsOperationAndEngine(t *testing.T) {
	c := CADDXFConverter{}
	if got := c.Pairs(); len(got) != 2 || got[0] != (Pair{"dxf", "svg"}) || got[1] != (Pair{"dxf", "pdf"}) {
		t.Errorf("Pairs() = %v, want exactly dxf->svg and dxf->pdf", got)
	}
	if got := operationOf(c); got != OperationConvert {
		t.Errorf("operation = %q, want %q", got, OperationConvert)
	}
	if got := c.Engine(); got != EngineCAD {
		t.Errorf("Engine() = %q, want %q", got, EngineCAD)
	}
}

func TestCADDXFConvertRefusesBeforeAnyEngine(t *testing.T) {
	cases := []struct {
		name   string
		target string
		opts   map[string]any
		in     func(f cadDXFFixture) string
	}{
		{"an option on svg", "svg", map[string]any{"size": 512}, nil},
		{"an option on pdf", "pdf", map[string]any{"background": "transparent"}, nil},
		{"a step source", "svg", nil, func(f cadDXFFixture) string { return cadDXFWrite(t, f.jobDir, "in.step", []byte("x")) }},
		{"a png target", "png", nil, nil},
		{"an stl target", "stl", nil, nil},
		{"an input above the ceiling", "svg", nil, func(f cadDXFFixture) string {
			if err := os.Truncate(f.in, CADMaxDXFBytes+1); err != nil {
				t.Fatal(err)
			}
			return f.in
		}},
	}
	for _, name := range []string{"draw.svg", "draw.render.svg", "texts.json", "measure.json", "clean.svg", ".home"} {
		cases = append(cases, struct {
			name   string
			target string
			opts   map[string]any
			in     func(f cadDXFFixture) string
		}{"input at " + name, "pdf", nil, func(f cadDXFFixture) string { return f.job(name) }})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCADDXFFixture(t, tc.target)
			f.setDXF(t, cadDXFFixtureOutputs(t, "wide"))
			f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, "")
			in := f.in
			if tc.in != nil {
				in = tc.in(f)
			}
			err := (CADDXFConverter{}).Convert(context.Background(), in, f.out, tc.opts)
			if err == nil {
				t.Fatal("Convert accepted the request")
			}
			for _, m := range []string{"guard.args", "dxf.args", "rsvg.args"} {
				if f.exists(m) {
					t.Errorf("%s ran before the refusal: %v", m, err)
				}
			}
			f.noOutput(t)
		})
	}
	t.Run("an input above the ceiling is ErrCADInputTooLarge", func(t *testing.T) {
		f := newCADDXFFixture(t, "svg")
		if err := os.Truncate(f.in, CADMaxDXFBytes+1); err != nil {
			t.Fatal(err)
		}
		err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrCADInputTooLarge) {
			t.Fatalf("err = %v, want ErrCADInputTooLarge", err)
		}
	})
	t.Run("an option is opts not applicable", func(t *testing.T) {
		f := newCADDXFFixture(t, "svg")
		err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, map[string]any{"view": "top"})
		if err == nil || !strings.Contains(err.Error(), "no option applies to a dxf conversion") {
			t.Fatalf("err = %v, want the D-07 refusal", err)
		}
	})
}

func TestCADDXFConvertSVGReturnsOnlyGoBytes(t *testing.T) {
	for _, name := range []string{"text_cases", "legacy_cp1251", "uncommon", "hatches_2", "wide", "tall", "text_alpha", "text_alpha_faint", "text_fonts"} {
		t.Run(name, func(t *testing.T) {
			f := newCADDXFFixture(t, "svg")
			f.setDXF(t, cadDXFFixtureOutputs(t, name))
			f.setRsvg(t, "", 0, "")
			if err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			got, err := os.ReadFile(f.out)
			if err != nil {
				t.Fatal(err)
			}
			if want := cadDXFValidated(t, cadDXFTestFile(name+".svg")); !bytes.Equal(got, want) {
				t.Errorf("the output is %d bytes, Go's re-serialization of the client SVG is %d", len(got), len(want))
			}
			raw, err := os.ReadFile(cadDXFTestFile(name + ".svg"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(got, raw) {
				t.Error("the output equals the helper's bytes; it must be Go's re-serialization")
			}
			if f.exists("rsvg.args") {
				t.Error("rsvg-convert ran for an svg target")
			}
			if g, w := f.args(t, "guard.args"), strings.Join(cadGuardArgs("dxf", f.in), " "); g != w {
				t.Errorf("guard argv = %q, want %q", g, w)
			}
			want := fmt.Sprintf("--max-drawn-entities %d --texts %s --render-svg %s %s %s",
				CADMaxDXFExpandedEntities, f.job("texts.json"), f.job("draw.render.svg"), f.in, f.job("draw.svg"))
			if g := f.args(t, "dxf.args"); g != want {
				t.Errorf("cad-dxf argv = %q, want %q", g, want)
			}
			info, err := os.Lstat(f.out)
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() {
				t.Errorf("the output is %v, want a regular file", info.Mode())
			}
		})
	}
}

func TestCADDXFConvertPDFRunsRsvgOnTheValidatedRenderSVGAndJudgesIt(t *testing.T) {
	t.Run("text_cases", func(t *testing.T) {
		f := newCADDXFFixture(t, "pdf")
		f.setDXF(t, cadDXFFixtureOutputs(t, "text_cases"))
		f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, "")
		if err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if g, w := f.args(t, "rsvg.args"), "-f pdf -b white -o "+f.out+" "+f.job("clean.svg"); g != w {
			t.Errorf("rsvg argv = %q, want %q", g, w)
		}
		clean, err := os.ReadFile(f.job("clean.svg"))
		if err != nil {
			t.Fatal(err)
		}
		if want := cadDXFValidated(t, cadDXFTestFile("text_cases.render.svg")); !bytes.Equal(clean, want) {
			t.Error("clean.svg is not Go's re-serialization of the render-only SVG")
		}
		got, err := os.ReadFile(f.out)
		if err != nil {
			t.Fatal(err)
		}
		if want, _ := os.ReadFile(cadDXFTestFile("text_cases.pdf")); !bytes.Equal(got, want) {
			t.Error("the PDF output is not the rsvg-convert output")
		}
	})
	t.Run("nofont is rejected and removed", func(t *testing.T) {
		f := newCADDXFFixture(t, "pdf")
		f.setDXF(t, cadDXFFixtureOutputs(t, "text_cases"))
		f.setRsvg(t, cadDXFTestFile("nofont.pdf"), 0, "")
		err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrCADPDFRejected) {
			t.Fatalf("err = %v, want ErrCADPDFRejected", err)
		}
		f.noOutput(t)
	})
	t.Run("outline is rejected and removed", func(t *testing.T) {
		f := newCADDXFFixture(t, "pdf")
		f.setDXF(t, cadDXFFixtureOutputs(t, "text_cases"))
		f.setRsvg(t, cadDXFTestFile("outline.pdf"), 0, "")
		err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrCADPDFRejected) {
			t.Fatalf("err = %v, want ErrCADPDFRejected", err)
		}
		f.noOutput(t)
	})
	for _, name := range []string{"legacy_cp1251", "uncommon", "hatches_2", "text_alpha", "text_alpha_faint", "text_fonts"} {
		t.Run(name, func(t *testing.T) {
			f := newCADDXFFixture(t, "pdf")
			f.setDXF(t, cadDXFFixtureOutputs(t, name))
			f.setRsvg(t, cadDXFTestFile(name+".pdf"), 0, "")
			if err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
				t.Fatalf("Convert: %v", err)
			}
		})
	}
}

func TestCADDXFConvertRejectsUntrustedHelperOutputs(t *testing.T) {
	scratch := t.TempDir()
	base := cadDXFFixtureOutputs(t, "text_cases")
	tf := cadDXFReadTexts(t, base.texts)
	data, err := os.ReadFile(base.client)
	if err != nil {
		t.Fatal(err)
	}
	w, h := cadSVGRootMM(t, data, "width"), cadSVGRootMM(t, data, "height")
	textsWith := func(name string, edit func(*cadSVGTextsFile)) string {
		c := cadDXFReadTexts(t, base.texts)
		edit(&c)
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return cadDXFWrite(t, scratch, name, raw)
	}
	script := `<script>alert(1)</script></g>`
	cases := []struct {
		name string
		edit func(o *cadDXFOutputs)
		want error
	}{
		{"a script element in the client SVG", func(o *cadDXFOutputs) {
			o.client = cadDXFMutate(t, scratch, base.client, "script.svg", "</g>", script)
		}, ErrCADSVGRejected},
		{"a script element in the render-only SVG", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "script.render.svg", "</g>", script)
		}, ErrCADSVGRejected},
		{"texts.json text differs from the client SVG", func(o *cadDXFOutputs) {
			o.texts = textsWith("differ.json", func(c *cadSVGTextsFile) { c.Texts[0].Text += "X"; c.Texts[0].Covered += "X" })
		}, ErrCADSVGRejected},
		{"texts.json count differs", func(o *cadDXFOutputs) {
			o.texts = textsWith("count.json", func(c *cadSVGTextsFile) { c.Texts = c.Texts[:len(c.Texts)-1] })
		}, ErrCADSVGRejected},
		{"texts.json covered disagrees with the render-only SVG", func(o *cadDXFOutputs) {
			o.texts = textsWith("covered.json", func(c *cadSVGTextsFile) {
				for i := range c.Texts {
					c.Texts[i].Covered = c.Texts[i].Text
				}
			})
		}, ErrCADSVGRejected},
		{"texts.json is malformed", func(o *cadDXFOutputs) {
			o.texts = cadDXFWrite(t, scratch, "bad.json", []byte(`{"texts":[{"text":"a"}]}`))
		}, ErrCADSVGRejected},
		{"the verdict counts other texts", func(o *cadDXFOutputs) {
			o.verdict = cadDXFVerdict(t, base.texts, w, h, len(tf.Texts)+1)
		}, ErrCADSVGRejected},
		{"the verdict page width differs", func(o *cadDXFOutputs) {
			o.verdict = cadDXFVerdict(t, base.texts, w+0.001, h, -1)
		}, ErrCADSVGRejected},
		{"the verdict page height differs", func(o *cadDXFOutputs) {
			o.verdict = cadDXFVerdict(t, base.texts, w, h-0.001, -1)
		}, ErrCADSVGRejected},
		{"the render-only SVG has another page width", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "page.render.svg", `width="297mm"`, `width="296mm"`)
		}, ErrCADSVGRejected},
		{"the render-only SVG has another viewBox", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "vb.render.svg", `viewBox="0 0 10000 5532"`, `viewBox="0 0 10000 5533"`)
		}, ErrCADSVGRejected},
		{"the verdict substitution count differs", func(o *cadDXFOutputs) {
			o.verdict = strings.Replace(o.verdict, `"render_substituted":2`, `"render_substituted":3`, 1)
		}, ErrCADSVGRejected},
		{"the client SVG is a symlink", func(o *cadDXFOutputs) {
			o.post = fmt.Sprintf("rm -f \"$2\"\nln -s %s \"$2\"", cadDXFQuote(base.client))
		}, ErrCADSVGRejected},
		{"the client SVG is above the SVG ceiling", func(o *cadDXFOutputs) {
			o.post = fmt.Sprintf("dd if=/dev/null of=\"$2\" bs=1 seek=%d 2>/dev/null", CADMaxDXFSVGBytes+1)
		}, ErrCADTooComplex},
		{"the render-only SVG is above the SVG ceiling", func(o *cadDXFOutputs) {
			o.post = fmt.Sprintf("dd if=/dev/null of=\"$R\" bs=1 seek=%d 2>/dev/null", CADMaxDXFSVGBytes+1)
		}, ErrCADTooComplex},
		{"an ok verdict without the client SVG", func(o *cadDXFOutputs) { o.client = "" }, ErrCADDrawingFailed},
		{"an ok verdict without the render-only SVG", func(o *cadDXFOutputs) { o.render = "" }, ErrCADDrawingFailed},
		{"an ok verdict without texts.json", func(o *cadDXFOutputs) { o.texts = "" }, ErrCADDrawingFailed},
	}
	for _, target := range []string{"svg", "pdf"} {
		for _, tc := range cases {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				f := newCADDXFFixture(t, target)
				o := base
				tc.edit(&o)
				f.setDXF(t, o)
				f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, "")
				err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if f.exists("rsvg.args") {
					t.Error("rsvg-convert ran on a rejected drawing")
				}
				f.noOutput(t)
				if _, err := os.Lstat(f.job("clean.svg")); err == nil {
					t.Error("clean.svg exists after a rejection")
				}
			})
		}
	}
}

func TestCADDXFConvertClassifiesEveryStage(t *testing.T) {
	ok := cadDXFFixtureOutputs(t, "text_cases")
	verdictLine := func(v, reason string) string {
		return fmt.Sprintf(`CADVERDICT {"stage":"dxf","verdict":"%s","reason":"%s"}`, v, reason)
	}
	type stub struct {
		guard   string
		dxf     cadDXFOutputs
		rsvgPre string
		rsvgRC  int
		c       CADConverter
	}
	cases := []struct {
		name    string
		s       stub
		want    []error
		noDXF   bool
		message string
	}{
		{"guard too complex stops before cad-dxf", stub{guard: cadSay("guard", "too_complex", "expanded_inserts", 3), dxf: ok}, []error{ErrCADTooComplex}, true, ""},
		{"guard unreadable stops before cad-dxf", stub{guard: cadSay("guard", "unreadable", "non_numeric", 2), dxf: ok}, []error{ErrCADUnreadable}, true, ""},
		{"cad-dxf empty", stub{dxf: cadDXFOutputs{verdict: verdictLine("empty", "nothing_drawable"), exit: 4}}, []error{ErrCADNothingDrawable, ErrCADEmptyResult}, false, ""},
		{"cad-dxf too complex", stub{dxf: cadDXFOutputs{verdict: verdictLine("too_complex", "drawn_entities_over_cap"), exit: 3}}, []error{ErrCADTooComplex}, false, ""},
		{"cad-dxf unreadable", stub{dxf: cadDXFOutputs{verdict: verdictLine("unreadable", "read_error"), exit: 2}}, []error{ErrCADUnreadable}, false, ""},
		{"cad-dxf failed", stub{dxf: cadDXFOutputs{verdict: verdictLine("failed", "draw_exception"), exit: 1}}, []error{ErrCADDrawingFailed}, false, ""},
		{"cad-dxf deadline", stub{dxf: cadDXFOutputs{pre: "sleep 5", verdict: ok.verdict}, c: CADConverter{DXFTimeout: 300 * time.Millisecond}}, []error{ErrCADDXFTimeout}, false, "cad dxf"},
		{"rsvg deadline", stub{dxf: ok, rsvgPre: "sleep 5", c: CADConverter{RsvgTimeout: 300 * time.Millisecond}}, []error{ErrCADRsvgTimeout}, false, "cad rsvg"},
		{"rsvg exit 1", stub{dxf: ok, rsvgRC: 1}, []error{ErrCADDrawingFailed}, false, "cad rsvg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCADDXFFixture(t, "pdf")
			if tc.s.guard != "" {
				fakeEngine(t, f.dir, "cad-guard", cadDXFGuard(tc.s.guard))
			}
			f.setDXF(t, tc.s.dxf)
			f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), tc.s.rsvgRC, tc.s.rsvgPre)
			err := (CADDXFConverter{tc.s.c}).Convert(context.Background(), f.in, f.out, nil)
			for _, w := range tc.want {
				if !errors.Is(err, w) {
					t.Errorf("err = %v, want it to match %v", err, w)
				}
			}
			if tc.message != "" && (err == nil || !strings.Contains(err.Error(), tc.message)) {
				t.Errorf("err = %v, want it to name %q", err, tc.message)
			}
			if tc.noDXF && f.exists("dxf.args") {
				t.Error("cad-dxf ran after a guard verdict")
			}
			f.noOutput(t)
		})
	}
}

func TestCADDXFConvertNormalizesLineEndingsOnBothSides(t *testing.T) {
	crFile := func(name string) string { return filepath.Join(cadDXFCRDir, name) }
	verdictOf := func(t *testing.T, name string) string {
		raw, err := os.ReadFile(crFile(name + ".verdict"))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(raw))
	}
	outputs := func(t *testing.T, name string) cadDXFOutputs {
		return cadDXFOutputs{
			client:  crFile(name + ".svg"),
			render:  crFile(name + ".render.svg"),
			texts:   crFile(name + ".texts.json"),
			verdict: verdictOf(t, name),
		}
	}
	t.Run("no DXF carrier reaches texts.json as a CR", func(t *testing.T) {
		for _, e := range cadDXFReadTexts(t, crFile("cr_carriers.texts.json")).Texts {
			if strings.ContainsAny(e.Text, "\r\n") {
				t.Errorf("the real helper emitted a line ending in %q", e.Text)
			}
		}
		f := newCADDXFFixture(t, "svg")
		f.setDXF(t, outputs(t, "cr_carriers"))
		if err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}
	})
	t.Run("the injected CR fixture differs before normalization", func(t *testing.T) {
		raw, err := os.ReadFile(crFile("cr_injected.svg"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte("E\rF")) || !bytes.Contains(raw, []byte("J\r\nK")) || !bytes.Contains(raw, []byte("G\r\rH")) {
			t.Fatal("cr_injected.svg does not carry raw CR, CRLF and CR CR")
		}
		_, facts, err := cadSVGValidate(t, raw, CADMaxDXFSVGBytes)
		if err != nil {
			t.Fatal(err)
		}
		tf := cadDXFReadTexts(t, crFile("cr_injected.texts.json"))
		want := []string{"E\nF", "J\nK", "G\n\nH"}
		for i, w := range want {
			if facts.Texts[i] != w {
				t.Errorf("SVG text %d parses as %q, want %q", i, facts.Texts[i], w)
			}
			if tf.Texts[i].Text == facts.Texts[i] {
				t.Errorf("texts.json text %d equals the parsed SVG text without normalization", i)
			}
		}
	})
	for _, target := range []string{"svg", "pdf"} {
		t.Run("the injected CR fixture passes the cross-check for "+target, func(t *testing.T) {
			f := newCADDXFFixture(t, target)
			f.setDXF(t, outputs(t, "cr_injected"))
			f.setRsvg(t, cadDXFTestFile("hatches_2.pdf"), 0, "")
			err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
			if target == "svg" {
				if err != nil {
					t.Fatalf("Convert: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrCADPDFRejected) {
				t.Fatalf("err = %v, want the PDF judge (not the SVG cross-check) to be the one that rejects a PDF without these texts", err)
			}
		})
	}
	t.Run("a real difference beside a CR still fails", func(t *testing.T) {
		scratch := t.TempDir()
		texts := cadDXFMutate(t, scratch, crFile("cr_injected.texts.json"), "t.json", `"E\rF","covered":"EF"`, `"E\rG","covered":"EG"`)
		o := outputs(t, "cr_injected")
		o.texts = texts
		f := newCADDXFFixture(t, "svg")
		f.setDXF(t, o)
		err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrCADSVGRejected) {
			t.Fatalf("err = %v, want ErrCADSVGRejected", err)
		}
		f.noOutput(t)
	})
	t.Run("a CR on one side only is not a line ending difference", func(t *testing.T) {
		scratch := t.TempDir()
		texts := cadDXFMutate(t, scratch, crFile("cr_injected.texts.json"), "t.json", `"E\rF","covered":"EF"`, `"EF","covered":"EF"`)
		o := outputs(t, "cr_injected")
		o.texts = texts
		f := newCADDXFFixture(t, "svg")
		f.setDXF(t, o)
		err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
		if !errors.Is(err, ErrCADSVGRejected) {
			t.Fatalf("err = %v, want ErrCADSVGRejected", err)
		}
	})
}

type cadDXFFrame struct {
	name, background string
	size             int
	sw, sh           int
	left, top        int
}

func cadDXFFrames(t *testing.T) []cadDXFFrame {
	t.Helper()
	raw, err := os.ReadFile(cadDXFTestFile("frames.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var out []cadDXFFrame
	for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if i == 0 {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) != 9 {
			t.Fatalf("frames.tsv row %d has %d columns", i, len(col))
		}
		n := func(s string) int {
			v, err := strconv.Atoi(s)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		out = append(out, cadDXFFrame{name: col[0], size: n(col[1]), background: col[2], sw: n(col[5]), sh: n(col[6]), left: n(col[7]), top: n(col[8])})
	}
	if len(out) != 5 {
		t.Fatalf("frames.tsv has %d frames, want 5", len(out))
	}
	return out
}

func (f cadDXFFixture) sidecar() string { return CADMeasurementsPath(f.out) }

func (f cadDXFFixture) noSidecar(t *testing.T) {
	t.Helper()
	if _, err := os.Lstat(f.sidecar()); err == nil {
		t.Error("a measurements sidecar exists after a failed render")
	}
}

func cadDXFRenderOutputs(t *testing.T, name, measure string) cadDXFOutputs {
	t.Helper()
	o := cadDXFFixtureOutputs(t, name)
	o.measure = measure
	return o
}

func cadDXFDraftWith(t *testing.T, dir, name string, edit func(map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile(cadDXFTestFile("wide.measure.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return cadDXFWrite(t, dir, name, out)
}

func cadDXFSidecarDoc(t *testing.T, f cadDXFFixture) measurements.Document {
	t.Helper()
	raw, err := os.ReadFile(f.sidecar())
	if err != nil {
		t.Fatalf("no sidecar: %v", err)
	}
	doc, err := measurements.Validate(raw)
	if err != nil {
		t.Fatalf("the sidecar is not a valid measurements document: %v", err)
	}
	return doc
}

func TestCADDXFRenderPairsOperationAndOptions(t *testing.T) {
	c := CADDXFRenderConverter{}
	if got := c.Pairs(); len(got) != 1 || got[0] != (Pair{"dxf", "png"}) {
		t.Errorf("Pairs() = %v, want exactly dxf->png", got)
	}
	if got := operationOf(c); got != OperationRender {
		t.Errorf("operation = %q, want %q", got, OperationRender)
	}
	if got := c.Engine(); got != EngineCAD {
		t.Errorf("Engine() = %q, want %q", got, EngineCAD)
	}
	refusals := []struct {
		name string
		opts map[string]any
		in   func(f cadDXFFixture) string
		out  string
		want string
	}{
		{"view top", map[string]any{"view": "top"}, nil, "", "view does not apply to a dxf source"},
		{"view iso", map[string]any{"view": "iso"}, nil, "", "view does not apply to a dxf source"},
		{"quality", map[string]any{"quality": "fine"}, nil, "", "quality does not apply to a dxf source"},
		{"an unknown option", map[string]any{"page": 2}, nil, "", ""},
		{"an svg target", nil, nil, "out.svg", ""},
		{"a step source", nil, func(f cadDXFFixture) string { return cadDXFWrite(t, f.jobDir, "in.step", []byte("x")) }, "", ""},
		{"an input above the ceiling", nil, func(f cadDXFFixture) string {
			if err := os.Truncate(f.in, CADMaxDXFBytes+1); err != nil {
				t.Fatal(err)
			}
			return f.in
		}, "", ""},
		{"an input at measure.json", nil, func(f cadDXFFixture) string { return f.job("measure.json") }, "", ""},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			f := newCADDXFFixture(t, "png")
			if tc.out != "" {
				f.out = f.job(tc.out)
			}
			f.setDXF(t, cadDXFRenderOutputs(t, "wide", cadDXFTestFile("wide.measure.json")))
			f.setRsvg(t, cadDXFTestFile("wide_1280_opaque.png"), 0, "")
			in := f.in
			if tc.in != nil {
				in = tc.in(f)
			}
			err := (CADDXFRenderConverter{}).Convert(context.Background(), in, f.out, tc.opts)
			if err == nil {
				t.Fatal("Convert accepted the request")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			for _, m := range []string{"guard.args", "dxf.args", "rsvg.args"} {
				if f.exists(m) {
					t.Errorf("%s ran before the refusal", m)
				}
			}
			f.noOutput(t)
			f.noSidecar(t)
		})
	}
	t.Run("size, background and assume_unit are honoured", func(t *testing.T) {
		f := newCADDXFFixture(t, "png")
		scratch := t.TempDir()
		draft := cadDXFDraftWith(t, scratch, "unspecified.json", func(m map[string]any) { m["declared_unit"] = "" })
		f.setDXF(t, cadDXFRenderOutputs(t, "tall", draft))
		f.setRsvg(t, cadDXFTestFile("tall_256_opaque.png"), 0, "")
		opts := map[string]any{"size": float64(256), "background": "opaque", "assume_unit": "inch"}
		if err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, opts); err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if doc := cadDXFSidecarDoc(t, f); doc.Unit != measurements.UnitInch || doc.UnitSource != measurements.SourceClientAssumed {
			t.Errorf("unit %q source %q, want inch client_assumed", doc.Unit, doc.UnitSource)
		}
		if got := f.args(t, "rsvg.args"); !strings.Contains(got, "--page-width 256 --page-height 256") {
			t.Errorf("rsvg argv = %q, want the 256 frame", got)
		}
	})
}

func TestCADDXFRenderLetterboxesTheValidatedSVGWithTheProbeArgv(t *testing.T) {
	for _, fr := range cadDXFFrames(t) {
		t.Run(fr.name, func(t *testing.T) {
			drawing := strings.TrimSuffix(fr.name, fmt.Sprintf("_%d_%s.png", fr.size, fr.background))
			f := newCADDXFFixture(t, "png")
			f.setDXF(t, cadDXFRenderOutputs(t, drawing, cadDXFTestFile("wide.measure.json")))
			f.setRsvg(t, cadDXFTestFile(fr.name), 0, "")
			opts := map[string]any{"size": float64(fr.size), "background": fr.background}
			if fr.size == CADDefaultRenderSize && fr.background == CADBackgroundOpq {
				opts = nil
			}
			if err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, opts); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			want := fmt.Sprintf("--max-drawn-entities %d --texts %s --render-svg %s --measure %s %s %s",
				CADMaxDXFExpandedEntities, f.job("texts.json"), f.job("draw.render.svg"), f.job("measure.json"), f.in, f.job("draw.svg"))
			if g := f.args(t, "dxf.args"); g != want {
				t.Errorf("cad-dxf argv = %q, want %q", g, want)
			}
			bg := ""
			if fr.background == CADBackgroundOpq {
				bg = "-b white "
			}
			want = fmt.Sprintf("%s-w %d -h %d --page-width %d --page-height %d --left %d --top %d -o %s %s",
				bg, fr.sw, fr.sh, fr.size, fr.size, fr.left, fr.top, f.out, f.job("clean.svg"))
			if g := f.args(t, "rsvg.args"); g != want {
				t.Errorf("rsvg argv = %q, want %q", g, want)
			}
			for _, m := range []string{"guard.args", "dxf.args", "rsvg.args"} {
				if g := f.args(t, m); strings.Contains(g, "iso") {
					t.Errorf("%s carries the 3D view: %q", m, g)
				}
			}
			clean, err := os.ReadFile(f.job("clean.svg"))
			if err != nil {
				t.Fatal(err)
			}
			if w := cadDXFValidated(t, cadDXFTestFile(drawing+".render.svg")); !bytes.Equal(clean, w) {
				t.Error("clean.svg is not Go's re-serialization of the render-only SVG")
			}
			doc := cadDXFSidecarDoc(t, f)
			if doc.Format != measurements.FormatDXF {
				t.Errorf("format %q, want dxf", doc.Format)
			}
			info, err := os.Lstat(f.sidecar())
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Errorf("sidecar mode = %v, %v, want a regular 0600 file", info.Mode(), err)
			}
		})
	}
}

func TestCADDXFRenderRejectsABlankFrameWithoutASidecar(t *testing.T) {
	f := newCADDXFFixture(t, "png")
	f.setDXF(t, cadDXFRenderOutputs(t, "wide", cadDXFTestFile("wide.measure.json")))
	f.setRsvg(t, cadDXFWrite(t, t.TempDir(), "blank.png", blankPNGBytes(t, CADDefaultRenderSize)), 0, "")
	err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, nil)
	if !errors.Is(err, ErrCADRenderRejected) {
		t.Fatalf("err = %v, want ErrCADRenderRejected", err)
	}
	f.noOutput(t)
	f.noSidecar(t)
}

func TestCADDXFRenderMeasurementsComeFromTheDraft(t *testing.T) {
	scratch := t.TempDir()
	t.Run("wrong_extents", func(t *testing.T) {
		f := newCADDXFFixture(t, "png")
		f.setDXF(t, cadDXFRenderOutputs(t, "wide", cadDXFTestFile("wrong_extents.measure.json")))
		f.setRsvg(t, cadDXFTestFile("wide_1280_opaque.png"), 0, "")
		if err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}
		doc := cadDXFSidecarDoc(t, f)
		if fmt.Sprint(doc.BBox.Min) != "[10 20]" || fmt.Sprint(doc.BBox.Max) != "[40 30]" {
			t.Errorf("bbox %v..%v, want [10 20]..[40 30]", doc.BBox.Min, doc.BBox.Max)
		}
		if doc.Unit != measurements.UnitMM || doc.UnitSource != measurements.SourceDeclared {
			t.Errorf("unit %q source %q, want mm declared", doc.Unit, doc.UnitSource)
		}
	})
	t.Run("unspecified with assume_unit mm", func(t *testing.T) {
		f := newCADDXFFixture(t, "png")
		draft := cadDXFDraftWith(t, scratch, "unspecified.json", func(m map[string]any) { m["declared_unit"] = "" })
		f.setDXF(t, cadDXFRenderOutputs(t, "wide", draft))
		f.setRsvg(t, cadDXFTestFile("wide_1280_opaque.png"), 0, "")
		if err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, map[string]any{"assume_unit": "mm"}); err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if doc := cadDXFSidecarDoc(t, f); doc.Unit != measurements.UnitMM || doc.UnitSource != measurements.SourceClientAssumed {
			t.Errorf("unit %q source %q, want mm client_assumed", doc.Unit, doc.UnitSource)
		}
	})
	failures := []struct {
		name  string
		draft func() string
		want  error
	}{
		{"a step draft", func() string {
			return cadDXFDraftWith(t, scratch, "step.json", func(m map[string]any) { m["format"] = "step" })
		}, ErrCADMeasurementsInvalid},
		{"a zero-extent draft", func() string {
			return cadDXFDraftWith(t, scratch, "zero.json", func(m map[string]any) {
				m["bbox"] = map[string]any{"min": []float64{5, 5}, "max": []float64{5, 5}}
			})
		}, ErrCADEmptyResult},
		{"a draft that is not JSON", func() string { return cadDXFWrite(t, scratch, "junk.json", []byte("{")) }, ErrCADMeasurementsInvalid},
		{"a 3D draft", func() string {
			return cadDXFDraftWith(t, scratch, "3d.json", func(m map[string]any) {
				m["bbox"] = map[string]any{"min": []float64{0, 0, 0}, "max": []float64{1, 1, 1}}
			})
		}, ErrCADMeasurementsInvalid},
		{"an ok verdict without a draft", func() string { return "" }, ErrCADDrawingFailed},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			f := newCADDXFFixture(t, "png")
			f.setDXF(t, cadDXFRenderOutputs(t, "wide", tc.draft()))
			f.setRsvg(t, cadDXFTestFile("wide_1280_opaque.png"), 0, "")
			err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if f.exists("rsvg.args") {
				t.Error("rsvg-convert ran before the measurements were built")
			}
			f.noOutput(t)
			f.noSidecar(t)
		})
	}
}

func TestCADDXFRenderLeavesNoOutputOrSidecarOnAnyFailure(t *testing.T) {
	ok := cadDXFRenderOutputs(t, "wide", cadDXFTestFile("wide.measure.json"))
	script := ok
	script.client = cadDXFMutate(t, t.TempDir(), ok.client, "s.svg", "</g>", "<script>x</script></g>")
	failed := cadDXFOutputs{verdict: `CADVERDICT {"stage":"dxf","verdict":"failed","reason":"draw_exception"}`, exit: 1}
	cases := []struct {
		name    string
		dxf     cadDXFOutputs
		png     string
		rsvgRC  int
		rsvgPre string
		c       CADConverter
		want    error
	}{
		{"an unsafe SVG", script, cadDXFTestFile("wide_1280_opaque.png"), 0, "", CADConverter{}, ErrCADSVGRejected},
		{"a drawing fault", failed, cadDXFTestFile("wide_1280_opaque.png"), 0, "", CADConverter{}, ErrCADDrawingFailed},
		{"rsvg exit 1", ok, cadDXFTestFile("wide_1280_opaque.png"), 1, "", CADConverter{}, ErrCADDrawingFailed},
		{"rsvg deadline", ok, cadDXFTestFile("wide_1280_opaque.png"), 0, "sleep 5", CADConverter{RsvgTimeout: 300 * time.Millisecond}, ErrCADRsvgTimeout},
		{"a frame of the wrong size", ok, cadDXFTestFile("tall_256_opaque.png"), 0, "", CADConverter{}, ErrCADRenderRejected},
		{"a transparent frame for an opaque request", ok, cadDXFTestFile("wide_1280_transparent.png"), 0, "", CADConverter{}, ErrCADRenderRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCADDXFFixture(t, "png")
			cadDXFWrite(t, f.jobDir, filepath.Base(f.sidecar()), []byte(`{"stale":true}`))
			cadDXFWrite(t, f.jobDir, filepath.Base(f.out), []byte("stale"))
			f.setDXF(t, tc.dxf)
			f.setRsvg(t, tc.png, tc.rsvgRC, tc.rsvgPre)
			err := (CADDXFRenderConverter{tc.c}).Convert(context.Background(), f.in, f.out, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			f.noOutput(t)
			f.noSidecar(t)
		})
	}
}

func TestCADDXFRenderWritesTheSidecarAsANewFile(t *testing.T) {
	for _, planter := range []string{"cad-dxf", "rsvg-convert"} {
		t.Run("a symlink planted by "+planter, func(t *testing.T) {
			f := newCADDXFFixture(t, "png")
			outside := filepath.Join(t.TempDir(), "outside.json")
			if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
				t.Fatal(err)
			}
			plant := fmt.Sprintf("ln -s %s %s", cadDXFQuote(outside), cadDXFQuote(f.sidecar()))
			o := cadDXFRenderOutputs(t, "wide", cadDXFTestFile("wide.measure.json"))
			rsvgPre := ""
			if planter == "cad-dxf" {
				o.post = plant
			} else {
				rsvgPre = plant
			}
			f.setDXF(t, o)
			f.setRsvg(t, cadDXFTestFile("wide_1280_opaque.png"), 0, rsvgPre)
			if err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if got, _ := os.ReadFile(outside); string(got) != "untouched" {
				t.Errorf("the worker wrote through a planted sidecar symlink: %q", got)
			}
			info, err := os.Lstat(f.sidecar())
			if err != nil || !info.Mode().IsRegular() {
				t.Fatalf("sidecar = %v, %v, want a regular file", info, err)
			}
			cadDXFSidecarDoc(t, f)
		})
	}
}

func TestCADDXFConvertRejectsAnOpacityTheOutputsDisagreeOn(t *testing.T) {
	scratch := t.TempDir()
	base := cadDXFFixtureOutputs(t, "text_alpha")
	textsWith := func(name string, edit func(*cadSVGTextsFile)) string {
		c := cadDXFReadTexts(t, base.texts)
		edit(&c)
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return cadDXFWrite(t, scratch, name, raw)
	}
	clear := `fill="#000000" fill-opacity="0.000"`
	half := `fill="#000000" fill-opacity="0.498"`
	cases := []struct {
		name string
		edit func(o *cadDXFOutputs)
	}{
		{"texts.json drops the flag of an invisible text", func(o *cadDXFOutputs) {
			o.texts = textsWith("unflagged.json", func(c *cadSVGTextsFile) { c.Texts[2].Invisible = false })
		}},
		{"texts.json flags a visible text", func(o *cadDXFOutputs) {
			o.texts = textsWith("flagged.json", func(c *cadSVGTextsFile) { c.Texts[0].Invisible = true })
		}},
		{"texts.json flags a half-transparent text", func(o *cadDXFOutputs) {
			o.texts = textsWith("half.json", func(c *cadSVGTextsFile) { c.Texts[1].Invisible = true })
		}},
		{"texts.json carries a non-boolean flag", func(o *cadDXFOutputs) {
			o.texts = cadDXFMutate(t, scratch, base.texts, "string.json", `"invisible":true`, `"invisible":"yes"`)
		}},
		{"the client SVG makes an invisible text faintly visible", func(o *cadDXFOutputs) {
			o.client = cadDXFMutate(t, scratch, base.client, "faint.svg", clear, `fill="#000000" fill-opacity="0.001"`)
		}},
		{"the render-only SVG makes an invisible text faintly visible", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "faint.render.svg", clear, `fill="#000000" fill-opacity="0.001"`)
		}},
		{"the render-only SVG hides a half-transparent text", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "hidden.render.svg", half, `fill="#000000" fill-opacity="0"`)
		}},
		{"the render-only SVG shifts a text's opacity", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "shift.render.svg", half, `fill="#000000" fill-opacity="0.499"`)
		}},
	}
	for _, target := range []string{"svg", "pdf"} {
		for _, tc := range cases {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				f := newCADDXFFixture(t, target)
				o := base
				tc.edit(&o)
				f.setDXF(t, o)
				f.setRsvg(t, cadDXFTestFile("text_alpha.pdf"), 0, "")
				err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
				if !errors.Is(err, ErrCADSVGRejected) {
					t.Fatalf("err = %v, want ErrCADSVGRejected", err)
				}
				if f.exists("rsvg.args") {
					t.Error("rsvg-convert ran on a rejected drawing")
				}
				f.noOutput(t)
			})
		}
	}
}

func TestCADDXFConvertRejectsAFontTheOutputsDisagreeOn(t *testing.T) {
	scratch := t.TempDir()
	base := cadDXFFixtureOutputs(t, "text_fonts")
	bold := `font-family="DejaVu Sans Mono" font-size="399.751" font-weight="bold"`
	book := `font-family="DejaVu Sans" font-size="399.751" fill`
	cases := []struct {
		name string
		edit func(o *cadDXFOutputs)
	}{
		{"the render-only SVG slants an upright text", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "oblique.render.svg", bold, bold+` font-style="oblique"`)
		}},
		{"the client SVG slants an upright text", func(o *cadDXFOutputs) {
			o.client = cadDXFMutate(t, scratch, base.client, "oblique.svg", bold, bold+` font-style="italic"`)
		}},
		{"the render-only SVG drops a weight", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "light.render.svg", bold, `font-family="DejaVu Sans Mono" font-size="399.751"`)
		}},
		{"the render-only SVG changes a weight", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "w200.render.svg", bold, `font-family="DejaVu Sans Mono" font-size="399.751" font-weight="200"`)
		}},
		{"the render-only SVG narrows a text", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "narrow.render.svg", book, `font-family="DejaVu Sans" font-size="399.751" font-stretch="semi-condensed" fill`)
		}},
		{"the render-only SVG swaps a family", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "family.render.svg", book, `font-family="DejaVu Serif" font-size="399.751" fill`)
		}},
		{"the render-only SVG resizes a text", func(o *cadDXFOutputs) {
			o.render = cadDXFMutate(t, scratch, base.render, "size.render.svg", book, `font-family="DejaVu Sans" font-size="399.752" fill`)
		}},
	}
	for _, target := range []string{"svg", "pdf"} {
		for _, tc := range cases {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				f := newCADDXFFixture(t, target)
				o := base
				tc.edit(&o)
				f.setDXF(t, o)
				f.setRsvg(t, cadDXFTestFile("text_fonts.pdf"), 0, "")
				err := (CADDXFConverter{}).Convert(context.Background(), f.in, f.out, nil)
				if !errors.Is(err, ErrCADSVGRejected) {
					t.Fatalf("err = %v, want ErrCADSVGRejected", err)
				}
				if f.exists("rsvg.args") {
					t.Error("rsvg-convert ran on a rejected drawing")
				}
				f.noOutput(t)
			})
		}
	}
}
