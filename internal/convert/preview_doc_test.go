package convert

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/jobs"
)

const (
	previewDocPath     = "../../docs/PREVIEW.md"
	previewDocRenderer = "../../cmd/cad-worker/helper/cad-render"
	previewDocHandlers = "../../internal/api/handlers.go"
)

var (
	previewDocMargin  = regexp.MustCompile(`(?m)^MARGIN = ([0-9.]+)$`)
	previewDocRefusal = regexp.MustCompile(`convert\.ValidateCADApplicability\([^)]*\); err != nil \{[^}]*errcode\.OptsNotApplicable`)
)

func previewDocSet(keys ...string) map[string]bool {
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func previewDocKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPreviewDocMatchesTheCode(t *testing.T) {
	doc := cadDocRead(t, previewDocPath)
	pairs := cadDocPairs(t)

	t.Run("fixed", func(t *testing.T) {
		rows := map[string]string{}
		for _, r := range cadDocTable(t, doc, "## Fixed by CAD", "property", "value", "source") {
			if _, dup := rows[r[0]]; dup {
				t.Errorf("docs/PREVIEW.md lists %s twice", r[0])
			}
			rows[r[0]] = r[1]
		}
		for _, p := range pairs[OperationRender] {
			if p.To != "png" {
				t.Errorf("the cad render pair %s -> %s is not a PNG", p.From, p.To)
			}
		}
		for _, p := range pairs[OperationConvert] {
			if p.To == "png" {
				t.Errorf("the cad convert pair %s -> png is a preview outside render", p.From)
			}
		}
		m := previewDocMargin.FindStringSubmatch(cadDocRead(t, previewDocRenderer))
		if m == nil {
			t.Fatalf("%s has no MARGIN line", previewDocRenderer)
		}
		threeD := map[string]bool{}
		for _, p := range pairs[OperationRender] {
			if p.From != "dxf" {
				threeD[p.From] = true
			}
		}
		for f := range threeD {
			for _, v := range previewDocKeys(cadViews) {
				if err := ValidateCADApplicability(EngineCAD, OperationRender, f, "png", CADOpts{View: v}); err != nil {
					t.Errorf("a %s render refuses the view %s: %v", f, v, err)
				}
			}
		}
		for _, v := range previewDocKeys(cadViews) {
			if ValidateCADApplicability(EngineCAD, OperationRender, "dxf", "png", CADOpts{View: v}) == nil {
				t.Errorf("a dxf render accepts the view %s", v)
			}
		}
		if !previewDocRefusal.MatchString(cadDocRead(t, previewDocHandlers)) {
			t.Errorf("%s does not answer a ValidateCADApplicability error with errcode.OptsNotApplicable", previewDocHandlers)
		}
		want := map[string]struct {
			ticks    map[string]bool
			contains []string
		}{
			"operation":    {previewDocSet(OperationRender), nil},
			"output":       {previewDocSet("png", jobs.OutputRolePrimary), []string{"role `" + jobs.OutputRolePrimary + "`"}},
			"size":         {previewDocSet(fmt.Sprintf("%d..%d", CADMinRenderSize, CADMaxRenderSize), strconv.Itoa(CADDefaultRenderSize)), []string{fmt.Sprintf("default `%d`", CADDefaultRenderSize)}},
			"background":   {cadBackgrounds, []string{"`" + CADBackgroundOpq + "` (white #FFFFFF, the default)", "`" + CADBackgroundTr + "` (alpha 0)"}},
			"3D framing":   {previewDocSet(m[1]), []string{"orthographic"}},
			"DXF framing":  {previewDocSet(strconv.FormatFloat(cadMarginMin, 'g', -1, 64)), []string{"never cropped or stretched"}},
			"3D views":     {cadViews, []string{"`" + CADViewIso + "` (the default)"}},
			"DXF views":    {previewDocSet("top", "view", "opts_not_applicable"), []string{"422"}},
			"measurements": {previewDocSet(jobs.OutputRoleMeasurements), []string{"together"}},
			"pixel judge":  {map[string]bool{}, []string{"before anything is uploaded"}},
		}
		for prop, w := range want {
			v, ok := rows[prop]
			if !ok {
				t.Errorf("docs/PREVIEW.md has no Fixed by CAD row %q", prop)
				continue
			}
			cadDocSameSet(t, "PREVIEW "+prop, cadDocTicks(v), w.ticks)
			for _, c := range w.contains {
				if !strings.Contains(v, c) {
					t.Errorf("docs/PREVIEW.md %s does not state %q: %q", prop, c, v)
				}
			}
		}
		for prop := range rows {
			if _, ok := want[prop]; !ok {
				t.Errorf("docs/PREVIEW.md fixes %q, which the test does not hold to the code", prop)
			}
		}
		rest := cadDocSection(t, doc, "## Non-previewable input")
		if !strings.Contains(rest, "No placeholder or substitute image is ever returned") {
			t.Error("docs/PREVIEW.md does not say that no placeholder image is returned")
		}
		outcomes := map[string]bool{}
		for _, r := range cadDocTable(t, doc, "## Non-previewable input", "input", "outcome") {
			for k := range cadDocTicks(r[1]) {
				outcomes[k] = true
			}
		}
		cadDocSameSet(t, "non-previewable outcomes", outcomes, previewDocSet("cad_empty_result", "cad_unreadable", "cad_too_complex", "unsupported_conversion", "dwg_unsupported"))
	})

	t.Run("not_fixed", func(t *testing.T) {
		sec := strings.ToLower(cadDocSection(t, doc, "## Not fixed"))
		for _, item := range []string{
			"a separate preview engine",
			"non-square sizes",
			"jpeg or webp",
			"page or frame selection",
			"colour profile and gamma",
			"dpi metadata",
			fmt.Sprintf("sizes below %d", CADMinRenderSize),
			"cross-arch determinism",
			"font fallback outside dejavu",
			"caching and re-use",
			"`preview` and `thumbnail`",
		} {
			if !strings.Contains(sec, item) {
				t.Errorf("the Not fixed section of docs/PREVIEW.md does not list %q", item)
			}
		}
	})
}
