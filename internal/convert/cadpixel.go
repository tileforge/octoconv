package convert

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
)

const (
	cadPixelMaxBytes       = 96 << 20
	cadBackgroundCutoff    = 250
	cadDarkCutoff          = 80
	cadBlobFracMin         = 0.02
	cadBlobFracMax         = 0.90
	cadBlobDarkMin         = 0.001
	cadThinAspectMax       = 0.15
	cadThinDarkPerLongSide = 0.25
	cadExtentMin           = 0.50
	cadMarginMin           = 0.03
	cadTransparentMin      = 0.05
	cadTransparentMax      = 0.95
	cadDrawingFracMin      = 0.0003
	cadDrawingSpanMin      = 0.5
	cadLetterboxSlack      = 2
)

type CADPixelFacts struct {
	Width, Height    int
	Count, DarkCount int
	BoxW, BoxH       int
	BoxX, BoxY       int
	Margin           int
	Frac, Dark       float64
	FracTransparent  float64
	HasAlpha         bool
	Mode             string
}

func cadPNGHasAlpha(data []byte) bool {
	if len(data) < 33 {
		return false
	}
	if colorType := data[25]; colorType == 4 || colorType == 6 {
		return true
	}
	off := 8
	for off+8 <= len(data) {
		n := int(binary.BigEndian.Uint32(data[off : off+4]))
		kind := string(data[off+4 : off+8])
		if kind == "tRNS" {
			return true
		}
		if kind == "IDAT" || kind == "IEND" || n < 0 || off+12+n > len(data) {
			return false
		}
		off += 12 + n
	}
	return false
}

func JudgeCADPNG(r io.Reader, size int, background string) (CADPixelFacts, error) {
	var facts CADPixelFacts
	facts.Mode = "none"
	img, err := cadReadFrame(r, size, background, &facts)
	if err != nil {
		return facts, err
	}
	opaquePx := cadMeasure(img, background, &facts)
	return facts, cadJudgeFacts(&facts, size, background, opaquePx)
}

func cadReadFrame(r io.Reader, size int, background string, facts *CADPixelFacts) (image.Image, error) {
	if size < CADMinRenderSize || size > CADMaxRenderSize {
		return nil, fmt.Errorf("cad: judge size %d is outside %d..%d", size, CADMinRenderSize, CADMaxRenderSize)
	}
	if background != CADBackgroundOpq && background != CADBackgroundTr {
		return nil, fmt.Errorf("cad: judge background %q is not opaque or transparent", background)
	}
	data, err := io.ReadAll(io.LimitReader(r, cadPixelMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cad: read the rendered image: %w", err)
	}
	if len(data) > cadPixelMaxBytes {
		return nil, fmt.Errorf("%w: the image exceeds %d bytes", ErrCADRenderRejected, cadPixelMaxBytes)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: not a decodable PNG: %v", ErrCADRenderRejected, err)
	}
	facts.Width, facts.Height = cfg.Width, cfg.Height
	if cfg.Width != size || cfg.Height != size {
		return nil, fmt.Errorf("%w: J0 frame is %dx%d, want %dx%d", ErrCADRenderRejected, cfg.Width, cfg.Height, size, size)
	}
	facts.HasAlpha = cadPNGHasAlpha(data)
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: not a decodable PNG: %v", ErrCADRenderRejected, err)
	}
	return img, nil
}

type CADLetterbox struct{ Left, Top, Width, Height int }

func CADDrawingLetterbox(wMM, hMM float64, size int) (CADLetterbox, error) {
	if size < CADMinRenderSize || size > CADMaxRenderSize {
		return CADLetterbox{}, fmt.Errorf("cad: letterbox size %d is outside %d..%d", size, CADMinRenderSize, CADMaxRenderSize)
	}
	if !(wMM > 0) || !(hMM > 0) || math.IsInf(wMM, 0) || math.IsInf(hMM, 0) {
		return CADLetterbox{}, fmt.Errorf("cad: letterbox of a %v x %v mm drawing", wMM, hMM)
	}
	k := float64(size) * (1 - 2*cadMarginMin) / math.Max(wMM, hMM)
	sw := max(1, int(math.Floor(wMM*k+0.5)))
	sh := max(1, int(math.Floor(hMM*k+0.5)))
	return CADLetterbox{Left: (size - sw) / 2, Top: (size - sh) / 2, Width: sw, Height: sh}, nil
}

func JudgeCADDrawingPNG(r io.Reader, size int, background string, box CADLetterbox) (CADPixelFacts, error) {
	var facts CADPixelFacts
	facts.Mode = "drawing"
	if box.Width < 1 || box.Height < 1 || box.Left < 0 || box.Top < 0 || box.Left+box.Width > size || box.Top+box.Height > size {
		return facts, fmt.Errorf("cad: letterbox %+v does not fit a %d px frame", box, size)
	}
	img, err := cadReadFrame(r, size, background, &facts)
	if err != nil {
		return facts, err
	}
	cadMeasure(img, background, &facts)
	if background == CADBackgroundTr {
		if !facts.HasAlpha {
			return facts, fmt.Errorf("%w: background transparent but the PNG carries no alpha", ErrCADRenderRejected)
		}
		if facts.FracTransparent < cadTransparentMin {
			return facts, fmt.Errorf("%w: transparent fraction %.4f is under %.2f", ErrCADRenderRejected, facts.FracTransparent, cadTransparentMin)
		}
	} else if facts.FracTransparent > 0 {
		return facts, fmt.Errorf("%w: background opaque but %.4f of the frame is transparent", ErrCADRenderRejected, facts.FracTransparent)
	}
	if facts.Count == 0 {
		return facts, fmt.Errorf("%w: J1 the frame holds no ink", ErrCADRenderRejected)
	}
	if facts.Frac < cadDrawingFracMin {
		return facts, fmt.Errorf("%w: J1 ink fraction %.6f is under %.4f", ErrCADRenderRejected, facts.Frac, cadDrawingFracMin)
	}
	long, boxLong := max(facts.BoxW, facts.BoxH), max(box.Width, box.Height)
	if float64(long) < cadDrawingSpanMin*float64(boxLong) {
		return facts, fmt.Errorf("%w: J2 the ink spans %d px of a %d px letterbox", ErrCADRenderRejected, long, boxLong)
	}
	if need := int(cadMarginMin * float64(size)); facts.Margin < need {
		return facts, fmt.Errorf("%w: J3 the margin is %d px, want at least %d", ErrCADRenderRejected, facts.Margin, need)
	}
	x1, y1 := facts.BoxX+facts.BoxW-1, facts.BoxY+facts.BoxH-1
	if facts.BoxX < box.Left-cadLetterboxSlack || facts.BoxY < box.Top-cadLetterboxSlack ||
		x1 > box.Left+box.Width-1+cadLetterboxSlack || y1 > box.Top+box.Height-1+cadLetterboxSlack {
		return facts, fmt.Errorf("%w: J3 the ink box %d,%d..%d,%d leaves the letterbox %d,%d..%d,%d by more than %d px", ErrCADRenderRejected,
			facts.BoxX, facts.BoxY, x1, y1, box.Left, box.Top, box.Left+box.Width-1, box.Top+box.Height-1, cadLetterboxSlack)
	}
	return facts, nil
}

func cadMeasure(img image.Image, background string, f *CADPixelFacts) int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	x0, y0, x1, y1 := w, h, -1, -1
	transparentPx, opaquePx := 0, 0
	nrgba, _ := img.(*image.NRGBA)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl, a uint8
			if nrgba != nil {
				i := nrgba.PixOffset(b.Min.X+x, b.Min.Y+y)
				r, g, bl, a = nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2], nrgba.Pix[i+3]
			} else {
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				r, g, bl, a = c.R, c.G, c.B, c.A
			}
			if a == 0 {
				transparentPx++
			}
			if a == 255 {
				opaquePx++
			}
			if a == 0 {
				continue
			}
			inMask := true
			if background == CADBackgroundOpq {
				inMask = r < cadBackgroundCutoff || g < cadBackgroundCutoff || bl < cadBackgroundCutoff
			}
			if !inMask {
				continue
			}
			f.Count++
			if r < cadDarkCutoff && g < cadDarkCutoff && bl < cadDarkCutoff {
				f.DarkCount++
			}
			if x < x0 {
				x0 = x
			}
			if x > x1 {
				x1 = x
			}
			if y < y0 {
				y0 = y
			}
			if y > y1 {
				y1 = y
			}
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

func cadJudgeFacts(f *CADPixelFacts, size int, background string, opaquePx int) error {
	if background == CADBackgroundTr {
		if !f.HasAlpha {
			return fmt.Errorf("%w: background transparent but the PNG carries no alpha", ErrCADRenderRejected)
		}
		if f.FracTransparent < cadTransparentMin || f.FracTransparent > cadTransparentMax {
			return fmt.Errorf("%w: transparent fraction %.4f is outside %.2f..%.2f", ErrCADRenderRejected, f.FracTransparent, cadTransparentMin, cadTransparentMax)
		}
		if opaquePx == 0 {
			return fmt.Errorf("%w: background transparent but no pixel is opaque", ErrCADRenderRejected)
		}
	} else if f.FracTransparent > 0 {
		return fmt.Errorf("%w: background opaque but %.4f of the frame is transparent", ErrCADRenderRejected, f.FracTransparent)
	}
	if f.Count == 0 {
		return fmt.Errorf("%w: J1 the frame holds no part pixels", ErrCADRenderRejected)
	}
	long, short := max(f.BoxW, f.BoxH), min(f.BoxW, f.BoxH)
	if float64(long) < cadExtentMin*float64(size) {
		return fmt.Errorf("%w: J2 the long side is %d px of a %d px frame", ErrCADRenderRejected, long, size)
	}
	if need := int(cadMarginMin * float64(size)); f.Margin < need {
		return fmt.Errorf("%w: J3 the margin is %d px, want at least %d", ErrCADRenderRejected, f.Margin, need)
	}
	aspect := float64(short) / float64(long)
	switch {
	case f.Frac >= cadBlobFracMin && f.Frac <= cadBlobFracMax && f.Dark >= cadBlobDarkMin:
		if f.Count < long {
			return fmt.Errorf("%w: J1 %d part pixels cannot span a %d px extent", ErrCADRenderRejected, f.Count, long)
		}
		f.Mode = "blob"
	case aspect < cadThinAspectMax && float64(f.DarkCount) >= cadThinDarkPerLongSide*float64(long):
		f.Mode = "thin"
	default:
		return fmt.Errorf("%w: J4 neither a blob (frac %.4f, dark %.4f) nor a thin part (aspect %.4f, %d dark pixels)", ErrCADRenderRejected, f.Frac, f.Dark, aspect, f.DarkCount)
	}
	return nil
}
