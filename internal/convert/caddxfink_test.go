package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var cadDXFInkFrames sync.Map

func cadDXFEncodeOnce(t *testing.T, key string, build func() image.Image) []byte {
	t.Helper()
	if v, ok := cadDXFInkFrames.Load(key); ok {
		return v.([]byte)
	}
	var b bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, build()); err != nil {
		t.Fatal(err)
	}
	v, _ := cadDXFInkFrames.LoadOrStore(key, b.Bytes())
	return v.([]byte)
}

func cadDXFFrameBytes(t *testing.T, key string, draw func(img *image.NRGBA)) []byte {
	t.Helper()
	return cadDXFEncodeOnce(t, key, func() image.Image {
		img := pixFrame(CADDXFInkSize, pixWhite)
		if draw != nil {
			draw(img)
		}
		return img
	})
}

func cadDXFInkedFrame(t *testing.T) []byte {
	return cadDXFFrameBytes(t, "inked", func(img *image.NRGBA) { pixRect(img, 900, 1000, 1100, 1003, pixDark) })
}

func cadDXFBlankFrame(t *testing.T) []byte { return cadDXFFrameBytes(t, "blank", nil) }

func cadDXFOnePixelFrame(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	return cadDXFFrameBytes(t, fmt.Sprintf("pixel-%v", c), func(img *image.NRGBA) { img.SetNRGBA(1024, 1024, c) })
}

func cadDXFRGBFrame(t *testing.T, size int, dot bool) []byte {
	t.Helper()
	return cadDXFEncodeOnce(t, fmt.Sprintf("rgb-%d-%v", size, dot), func() image.Image {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		for i := range img.Pix {
			img.Pix[i] = 255
		}
		if dot {
			img.SetRGBA(size/2, size/2, color.RGBA{R: 30, G: 30, B: 30, A: 255})
		}
		return img
	})
}

func cadDXFTransparentFrame(t *testing.T) []byte {
	t.Helper()
	return cadDXFEncodeOnce(t, "transparent", func() image.Image {
		img := pixFrame(CADDXFInkSize, color.NRGBA{})
		pixRect(img, 900, 1000, 1100, 1003, pixDark)
		return img
	})
}

func TestJudgeCADDrawingInkIsJ1OnAWhitePage(t *testing.T) {
	size := CADDXFInkSize
	cases := []struct {
		name  string
		data  func(t *testing.T) []byte
		size  int
		count int
		empty bool
		fault bool
	}{
		{"a blank white frame has no ink", cadDXFBlankFrame, size, 0, true, false},
		{"a frame with a dark bar has ink", cadDXFInkedFrame, size, 201 * 4, false, false},
		{"a pixel at 249 is ink", func(t *testing.T) []byte { return cadDXFOnePixelFrame(t, color.NRGBA{R: 249, G: 255, B: 255, A: 255}) }, size, 1, false, false},
		{"a pixel at 250 in every channel is not ink", func(t *testing.T) []byte {
			return cadDXFOnePixelFrame(t, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
		}, size, 0, true, false},
		{"a white pixel is not ink", func(t *testing.T) []byte { return cadDXFOnePixelFrame(t, color.NRGBA{R: 255, G: 255, B: 255, A: 255}) }, size, 0, true, false},
		{"an RGB frame as rsvg-convert -b white writes it", func(t *testing.T) []byte { return cadDXFRGBFrame(t, size, true) }, size, 1, false, false},
		{"a blank RGB frame", func(t *testing.T) []byte { return cadDXFRGBFrame(t, size, false) }, size, 0, true, false},
		{"a transparent pixel is a malformed check frame", func(t *testing.T) []byte {
			return cadDXFOnePixelFrame(t, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
		}, size, 0, false, true},
		{"a half-opaque pixel is a malformed check frame", func(t *testing.T) []byte {
			return cadDXFOnePixelFrame(t, color.NRGBA{R: 0, G: 0, B: 0, A: 128})
		}, size, 0, false, true},
		{"a frame of another size", func(t *testing.T) []byte { return cadDXFRGBFrame(t, 2047, true) }, size, 0, false, true},
		{"not a PNG", func(t *testing.T) []byte { return []byte("%PDF-1.5\n") }, size, 0, false, true},
		{"a size below the render range", func(t *testing.T) []byte { return cadDXFRGBFrame(t, 64, true) }, 64, 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts, err := JudgeCADDrawingInk(bytes.NewReader(tc.data(t)), tc.size)
			switch {
			case tc.fault:
				if err == nil || !errors.Is(err, ErrCADDrawingFailed) || errors.Is(err, ErrCADEmptyResult) {
					t.Fatalf("err = %v, want ErrCADDrawingFailed and not an empty result", err)
				}
			case tc.empty:
				if !errors.Is(err, ErrCADNothingDrawable) || !errors.Is(err, ErrCADEmptyResult) {
					t.Fatalf("err = %v, want ErrCADNothingDrawable and ErrCADEmptyResult", err)
				}
				if !strings.Contains(err.Error(), "J1") {
					t.Errorf("err = %v, want it to name J1", err)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want ink", err)
				}
			}
			if !tc.fault && facts.Count != tc.count {
				t.Errorf("Count = %d, want %d", facts.Count, tc.count)
			}
		})
	}
}

func TestJudgeCADDrawingInkCountsWhatTheDrawingJudgeCounts(t *testing.T) {
	n := 0
	for _, r := range cadFrameRows(t) {
		if r.background != CADBackgroundOpq {
			continue
		}
		n++
		data, err := os.ReadFile(filepath.Join(cadSVGTestDir, r.name))
		if err != nil {
			t.Fatal(err)
		}
		drawing, err := JudgeCADDrawingPNG(bytes.NewReader(data), r.size, r.background, r.box)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		ink, err := JudgeCADDrawingInk(bytes.NewReader(data), r.size)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		if ink.Count != drawing.Count || ink.DarkCount != drawing.DarkCount || ink.BoxW != drawing.BoxW || ink.BoxH != drawing.BoxH {
			t.Errorf("%s: the ink judge counts %+v, the drawing judge %+v", r.name, ink, drawing)
		}
		if ink.Mode != "ink" {
			t.Errorf("%s: mode %q, want ink", r.name, ink.Mode)
		}
	}
	if n == 0 {
		t.Fatal("no opaque helper frame")
	}
	blank := blankPNGBytes(t, CADDefaultRenderSize)
	_, drawingErr := JudgeCADDrawingPNG(bytes.NewReader(blank), CADDefaultRenderSize, CADBackgroundOpq, cadSquareBox)
	_, inkErr := JudgeCADDrawingInk(bytes.NewReader(blank), CADDefaultRenderSize)
	if drawingErr == nil || inkErr == nil || !strings.Contains(drawingErr.Error(), "J1") || !strings.Contains(inkErr.Error(), "J1") {
		t.Errorf("a blank frame: drawing judge %v, ink judge %v, want both to fail J1", drawingErr, inkErr)
	}
}

func TestCADDXFInkSizeIsTheLargestRenderSize(t *testing.T) {
	if CADDXFInkSize != CADMaxRenderSize {
		t.Errorf("CADDXFInkSize = %d, want CADMaxRenderSize %d: a smaller check frame can lose a thin feature a render shows", CADDXFInkSize, CADMaxRenderSize)
	}
}

func cadDXFInkArgv(t *testing.T, f cadDXFFixture, renderSVG string) string {
	t.Helper()
	data, err := os.ReadFile(renderSVG)
	if err != nil {
		t.Fatal(err)
	}
	box, err := CADDrawingLetterbox(cadSVGRootMM(t, data, "width"), cadSVGRootMM(t, data, "height"), CADDXFInkSize)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("-b white -w %d -h %d --page-width %d --page-height %d --left %d --top %d -o %s %s",
		box.Width, box.Height, CADDXFInkSize, CADDXFInkSize, box.Left, box.Top, f.job("ink.png"), f.job("clean.svg"))
}

func TestCADDXFJudgesInkOnEveryRouteBeforeItsOutput(t *testing.T) {
	cases := []struct {
		target, name, rsvgSrc string
		calls                 int
	}{
		{"svg", "text_cases", "", 1},
		{"pdf", "text_cases", "text_cases.pdf", 2},
		{"png", "wide", "wide_1280_opaque.png", 2},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			f := newCADDXFFixture(t, tc.target)
			f.setDXF(t, cadDXFRenderOutputs(t, tc.name, cadDXFTestFile("wide.measure.json")))
			src := ""
			if tc.rsvgSrc != "" {
				src = cadDXFTestFile(tc.rsvgSrc)
			}
			f.setRsvg(t, src, 0, "")
			if err := cadDXFRunTarget(t, f, tc.target); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			calls := f.rsvgCalls(t)
			if len(calls) != tc.calls {
				t.Fatalf("rsvg-convert ran %d times (%q), want %d", len(calls), calls, tc.calls)
			}
			if want := cadDXFInkArgv(t, f, cadDXFTestFile(tc.name+".render.svg")); calls[0] != want {
				t.Errorf("the ink check argv = %q, want %q", calls[0], want)
			}
			if _, err := os.Lstat(f.job("ink.png")); err == nil {
				t.Error("ink.png is left in the job directory")
			}
			if tc.target == "pdf" && calls[1] != "-f pdf -b white -o "+f.out+" "+f.job("clean.svg") {
				t.Errorf("the PDF argv = %q", calls[1])
			}
		})
	}
}

func TestCADDXFRefusesAnInklessDrawingOnEveryRoute(t *testing.T) {
	for _, target := range []string{"svg", "pdf", "png"} {
		t.Run(target, func(t *testing.T) {
			f := newCADDXFFixture(t, target)
			f.setDXF(t, cadDXFRenderOutputs(t, "text_cases", cadDXFTestFile("wide.measure.json")))
			f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, "")
			f.setInk(t, cadDXFBlankFrame(t))
			err := cadDXFRunTarget(t, f, target)
			if !errors.Is(err, ErrCADNothingDrawable) || !errors.Is(err, ErrCADEmptyResult) {
				t.Fatalf("err = %v, want ErrCADNothingDrawable and ErrCADEmptyResult", err)
			}
			if errors.Is(err, ErrCADRenderRejected) || errors.Is(err, ErrCADPDFRejected) {
				t.Errorf("err = %v, want the drawing refused, not its output", err)
			}
			if calls := f.rsvgCalls(t); len(calls) != 1 {
				t.Errorf("rsvg-convert ran %d times (%q), want only the ink check", len(calls), calls)
			}
			f.noOutput(t)
			f.noSidecar(t)
			for _, name := range []string{"ink.png", "clean.svg"} {
				if _, err := os.Lstat(f.job(name)); err == nil {
					t.Errorf("%s is left after the refusal", name)
				}
			}
		})
	}
}

func TestCADDXFTreatsAMalformedInkFrameAsAnEngineFault(t *testing.T) {
	frames := map[string]func(t *testing.T) []byte{
		"a PDF":              func(t *testing.T) []byte { b, _ := os.ReadFile(cadDXFTestFile("text_cases.pdf")); return b },
		"a 1280 frame":       func(t *testing.T) []byte { return cadDXFRGBFrame(t, 1280, true) },
		"a transparent page": cadDXFTransparentFrame,
	}
	for name, frame := range frames {
		for _, target := range []string{"svg", "pdf", "png"} {
			t.Run(name+"/"+target, func(t *testing.T) {
				f := newCADDXFFixture(t, target)
				f.setDXF(t, cadDXFRenderOutputs(t, "text_cases", cadDXFTestFile("wide.measure.json")))
				f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, "")
				f.setInk(t, frame(t))
				err := cadDXFRunTarget(t, f, target)
				if !errors.Is(err, ErrCADDrawingFailed) || errors.Is(err, ErrCADEmptyResult) {
					t.Fatalf("err = %v, want ErrCADDrawingFailed and not an empty result", err)
				}
				f.noOutput(t)
				f.noSidecar(t)
				if _, err := os.Lstat(f.job("ink.png")); err == nil {
					t.Error("ink.png is left after the fault")
				}
			})
		}
	}
}

func TestCADDXFRefusesAnInkCheckFrameThatIsASymlink(t *testing.T) {
	for _, target := range []string{"svg", "pdf", "png"} {
		t.Run(target, func(t *testing.T) {
			f := newCADDXFFixture(t, target)
			f.setDXF(t, cadDXFRenderOutputs(t, "text_cases", cadDXFTestFile("wide.measure.json")))
			f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, fmt.Sprintf("ln -s %s %s", cadDXFQuote(f.marker("ink.src.png")), cadDXFQuote(f.job("ink.png"))))
			err := cadDXFRunTarget(t, f, target)
			if !errors.Is(err, ErrCADDrawingFailed) || errors.Is(err, ErrCADEmptyResult) || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("err = %v, want ErrCADDrawingFailed naming the symbolic link", err)
			}
			f.noOutput(t)
			if _, err := os.Lstat(f.job("ink.png")); err == nil {
				t.Error("the planted ink.png is left after the fault")
			}
		})
	}
}

func TestCADDXFRefusesAnOutputAtTheInkCheckFrame(t *testing.T) {
	f := newCADDXFFixture(t, "png")
	f.out = f.job("ink.png")
	f.setDXF(t, cadDXFRenderOutputs(t, "wide", cadDXFTestFile("wide.measure.json")))
	f.setRsvg(t, cadDXFTestFile("wide_1280_opaque.png"), 0, "")
	err := (CADDXFRenderConverter{}).Convert(context.Background(), f.in, f.out, nil)
	if err == nil || !strings.Contains(err.Error(), "ink.png path of its own job") {
		t.Fatalf("err = %v, want the output refused as the ink check frame of its own job", err)
	}
	for _, m := range []string{"guard.args", "dxf.args", "rsvg.args"} {
		if f.exists(m) {
			t.Errorf("%s ran before the refusal", m)
		}
	}
}

func TestCADDXFInkCheckRsvgFailuresAreClassifiedOnEveryRoute(t *testing.T) {
	for _, target := range []string{"svg", "pdf", "png"} {
		t.Run(target+"/exit 1", func(t *testing.T) {
			f := newCADDXFFixture(t, target)
			f.setDXF(t, cadDXFRenderOutputs(t, "text_cases", cadDXFTestFile("wide.measure.json")))
			f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 1, "")
			err := cadDXFRunTarget(t, f, target)
			if !errors.Is(err, ErrCADDrawingFailed) || !strings.Contains(err.Error(), "cad rsvg") {
				t.Fatalf("err = %v, want ErrCADDrawingFailed from the rsvg stage", err)
			}
			f.noOutput(t)
		})
		t.Run(target+"/deadline", func(t *testing.T) {
			f := newCADDXFFixture(t, target)
			f.setDXF(t, cadDXFRenderOutputs(t, "text_cases", cadDXFTestFile("wide.measure.json")))
			f.setRsvg(t, cadDXFTestFile("text_cases.pdf"), 0, "sleep 5")
			c := CADConverter{RsvgTimeout: 300 * time.Millisecond}
			var err error
			if target == "png" {
				err = (CADDXFRenderConverter{c}).Convert(context.Background(), f.in, f.out, nil)
			} else {
				err = (CADDXFConverter{c}).Convert(context.Background(), f.in, f.out, nil)
			}
			if !errors.Is(err, ErrCADRsvgTimeout) {
				t.Fatalf("err = %v, want ErrCADRsvgTimeout", err)
			}
			f.noOutput(t)
		})
	}
}
