package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"github.com/tileforge/octoconv/internal/measurements"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

func TestCADDimensionsOptions(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		raw, _ := json.Marshal(map[string]bool{"dimensions": enabled})
		o, err := ParseCADOpts(raw)
		if err != nil {
			t.Fatalf("ParseCADOpts(%s): %v", raw, err)
		}
		stored, _ := json.Marshal(o)
		if !bytes.Equal(stored, raw) {
			t.Errorf("stored %s, want %s", stored, raw)
		}
		for _, format := range []string{"step", "iges", "stl"} {
			if err := ValidateCADApplicability(EngineCAD, OperationRender, format, "png", o); err != nil {
				t.Errorf("%s render: %v", format, err)
			}
		}
		if err := ValidateCADApplicability(EngineCAD, OperationRender, "dxf", "png", o); err == nil {
			t.Error("DXF accepted dimensions")
		}
		if err := ValidateCADApplicability(EngineCAD, OperationConvert, "step", "stl", o); err == nil {
			t.Error("convert accepted dimensions")
		}
	}
	for _, value := range []string{`null`, `"true"`, `1`, `[]`, `{}`} {
		if _, err := ParseCADOpts([]byte(`{"dimensions":` + value + `}`)); err == nil {
			t.Errorf("accepted dimensions %s", value)
		}
	}
}

func TestCADDimensionsThinLabelsDoNotPaintOverEachOther(t *testing.T) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 9, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	red := image.NewUniform(color.RGBA{R: 255, A: 255})
	blue := image.NewUniform(color.RGBA{B: 255, A: 255})
	first := cadDimensionText("100000.0", face, red)
	second := cadDimensionText("100000.0", face, blue)
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	pen := cadDimensionPen{image: img, ink: red, width: 1}
	pen.text(first, cadPoint{114.74, 225.41}, math.Pi/6)
	before := append([]byte{}, img.Pix...)
	pen.text(second, cadPoint{135.26, 225.41}, -math.Pi/6)
	bluePixels := 0
	for i := 0; i < len(before); i += 4 {
		if before[i] > 0 && img.Pix[i] != before[i] {
			t.Fatal("the second short-axis label paints over the first label")
		}
		if img.Pix[i+2] > 0 {
			bluePixels++
		}
	}
	if bluePixels == 0 {
		t.Fatal("the second label disappeared")
	}
}

func TestCADDimensionsLabelsUseDocumentUnitsAndPrecision(t *testing.T) {
	for _, tc := range []struct {
		unit, source, want string
	}{
		{"mm", "declared", "Size: 15.0 × 60.0 × 125.0 mm"},
		{"inch", "declared", "Size: 15.0 × 60.0 × 125.0 inch"},
		{"mm", "client_assumed", "Size: 15.0 × 60.0 × 125.0 mm (assumed)"},
		{"unspecified", "none", "Size: 15.0 × 60.0 × 125.0 (units unspecified)"},
	} {
		doc := measurements.Document{Unit: tc.unit, UnitSource: tc.source,
			BBox: measurements.BBox{Min: []float64{100, -25, 9}, Max: []float64{115, 35, 134}}}
		if got := cadDimensionTitle(doc); got != tc.want {
			t.Errorf("title = %q, want %q", got, tc.want)
		}
	}
	for _, tc := range []struct {
		value float64
		want  string
	}{
		{0, "0.0"}, {1e-5, "1e-05"}, {1e12, "1e+12"}, {15.00000001, "15.0"}, {2.54, "2.54"},
	} {
		if got := cadDimensionNumber(tc.value); got != tc.want {
			t.Errorf("%g formatted as %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestCADDimensionsProjectionMatchesOrthographicViews(t *testing.T) {
	bounds := stlBounds{Min: [3]float64{100, -25, 9}, Max: [3]float64{110, -5, 39}}
	frame := image.Rect(0, 0, 1000, 1000)
	for _, tc := range []struct {
		view             string
		corner0, corner7 cadPoint
	}{
		{"front", cadPoint{353.333333, 940}, cadPoint{646.666667, 60}},
		{"back", cadPoint{646.666667, 940}, cadPoint{353.333333, 60}},
		{"left", cadPoint{793.333333, 940}, cadPoint{206.666667, 60}},
		{"right", cadPoint{206.666667, 940}, cadPoint{793.333333, 60}},
		{"top", cadPoint{280, 940}, cadPoint{720, 60}},
		{"bottom", cadPoint{720, 940}, cadPoint{280, 60}},
	} {
		points := cadDimensionProjection(bounds, tc.view, frame)
		for i, want := range map[int]cadPoint{0: tc.corner0, 7: tc.corner7} {
			for axis := range 2 {
				if math.Abs(points[i][axis]-want[axis]) > 1e-5 {
					t.Errorf("%s corner %d = %v, want %v", tc.view, i, points[i], want)
				}
			}
		}
	}
}

func TestCADDimensionsAllViewsPreserveFrameAndAlpha(t *testing.T) {
	for _, size := range []int{256, 1280, 2048} {
		for view := range cadViews {
			for _, background := range []string{CADBackgroundOpq, CADBackgroundTr} {
				bg := pixWhite
				if background == CADBackgroundTr {
					bg = color.NRGBA{}
				}
				model := pixFrame(size, bg)
				pixHexagon(model, size/2, size/2, size*2/5)
				opts := CADOpts{Size: &size, View: view, Background: background}.Resolved()
				doc, err := buildCADMeasurements(unitsInchDraft(), "")
				if err != nil {
					t.Fatal(err)
				}
				img, err := cadDimensionImage(model, stlBounds{Max: [3]float64{25.4, 50.8, 76.2}}, doc, opts)
				if err != nil {
					t.Fatal(err)
				}
				if img.Bounds() != image.Rect(0, 0, size, size) {
					t.Fatal(img.Bounds())
				}
				var facts CADPixelFacts
				cadMeasure(img, &facts)
				if facts.BoxY > size/10 || facts.Margin < size*3/100 {
					t.Errorf("size=%d view=%s background=%s: heading absent or content clipped: %+v", size, view, background, facts)
				}
				if background == CADBackgroundTr && facts.RingNonTransparent != 0 {
					t.Errorf("%s: transparent border was painted", view)
				}
				if background == CADBackgroundOpq && facts.FracTransparent != 0 {
					t.Errorf("%s: opaque background became transparent", view)
				}
			}
		}
	}
}

func TestCADDimensionsHiddenAxisStaysInHeading(t *testing.T) {
	doc := measurements.Document{Unit: "mm", UnitSource: "declared",
		BBox: measurements.BBox{Min: []float64{0, 0, 0}, Max: []float64{15, 60, 125}}}
	points := cadDimensionProjection(stlBounds{Max: [3]float64{15, 60, 125}}, "front", image.Rect(0, 0, 500, 500))
	lines := cadDimensionLines(points, doc)
	if len(lines) != 2 || lines[0].Value != "15.0" || lines[1].Value != "125.0" {
		t.Fatalf("front dimension lines = %+v", lines)
	}
	if got := cadDimensionTitle(doc); got != "Size: 15.0 × 60.0 × 125.0 mm" {
		t.Fatal(got)
	}
}

func TestCADDimensionsRenderDefaultAndOptOut(t *testing.T) {
	for _, mode := range []string{"default", "true", "false"} {
		t.Run(mode, func(t *testing.T) {
			f := newCADRenderFixture(t, "step")
			original := renderPNGBytes(t, 256, false)
			f.setEngines(t, cadOKGuard(), f.meshScript(t, f.writeDraft(t, unitsInchDraft())), f.renderScript(t, original))
			opts := map[string]any{"size": 256}
			if mode != "default" {
				opts["dimensions"] = mode == "true"
			}
			if err := (CADRenderConverter{}).Convert(context.Background(), f.in, f.png, opts); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(f.png)
			if err != nil {
				t.Fatal(err)
			}
			if unchanged := bytes.Equal(original, actual); unchanged != (mode == "false") {
				t.Errorf("PNG unchanged = %t with dimensions %s", unchanged, mode)
			}
		})
	}
}
