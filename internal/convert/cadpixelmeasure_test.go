package convert

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

func cadMeasurePerPixel(img image.Image, background string, f *CADPixelFacts) int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	x0, y0, x1, y1 := w, h, -1, -1
	transparentPx, opaquePx := 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if c.A == 0 {
				transparentPx++
				continue
			}
			if c.A == 255 {
				opaquePx++
			}
			if background == CADBackgroundOpq && c.R >= cadBackgroundCutoff && c.G >= cadBackgroundCutoff && c.B >= cadBackgroundCutoff {
				continue
			}
			f.Count++
			if c.R < cadDarkCutoff && c.G < cadDarkCutoff && c.B < cadDarkCutoff {
				f.DarkCount++
			}
			x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
		}
	}
	area := float64(w * h)
	f.Frac = float64(f.Count) / area
	f.Dark = float64(f.DarkCount) / area
	f.FracTransparent = float64(transparentPx) / area
	f.Margin = -1
	if f.Count > 0 {
		f.BoxW, f.BoxH = x1-x0+1, y1-y0+1
		f.BoxX, f.BoxY = x0, y0
		f.Margin = min(x0, y0, w-1-x1, h-1-y1)
	}
	return opaquePx
}

func TestCADMeasureSkipsOnlyRowsThatCannotHoldInk(t *testing.T) {
	rng := rand.New(rand.NewSource(75))
	channels := []uint8{0, 79, 80, 249, 250, 254, 255}
	alphas := []uint8{0, 1, 128, 254, 255, 255, 255}
	frame := func(kind string) image.Image {
		const w, h = 41, 37
		var img interface {
			image.Image
			Set(x, y int, c color.Color)
		}
		switch kind {
		case "nrgba":
			img = image.NewNRGBA(image.Rect(-3, 5, w-3, h+5))
		case "rgba":
			img = image.NewRGBA(image.Rect(-3, 5, w-3, h+5))
		default:
			img = image.NewGray(image.Rect(-3, 5, w-3, h+5))
		}
		b := img.Bounds()
		flat := []color.NRGBA{{R: 250, G: 250, B: 250, A: 250}, {R: 255, G: 255, B: 255, A: 254}, {R: 254, G: 254, B: 254, A: 255}, {R: 255, G: 255, B: 255, A: 0}}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			plain := rng.Intn(4)
			row := flat[rng.Intn(len(flat))]
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				if plain == 3 {
					c = row
				}
				if plain == 0 || rng.Intn(9) == 0 {
					c = color.NRGBA{R: channels[rng.Intn(len(channels))], G: channels[rng.Intn(len(channels))], B: channels[rng.Intn(len(channels))], A: alphas[rng.Intn(len(alphas))]}
				}
				img.Set(x, y, c)
			}
		}
		return img
	}
	for i := 0; i < 300; i++ {
		for _, kind := range []string{"nrgba", "rgba", "gray"} {
			img := frame(kind)
			if i%2 == 1 {
				if s, ok := img.(interface {
					SubImage(image.Rectangle) image.Image
				}); ok {
					img = s.SubImage(image.Rect(0, 8, 30, 33))
				}
			}
			for _, bg := range []string{CADBackgroundOpq, CADBackgroundTr} {
				var got, want CADPixelFacts
				gotOpaque := cadMeasure(img, bg, &got)
				wantOpaque := cadMeasurePerPixel(img, bg, &want)
				if got != want || gotOpaque != wantOpaque {
					t.Fatalf("%s %s frame %d: cadMeasure %+v (opaque %d), per pixel %+v (opaque %d)", kind, bg, i, got, gotOpaque, want, wantOpaque)
				}
			}
		}
	}
}
