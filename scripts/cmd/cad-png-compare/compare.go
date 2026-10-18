package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/tileforge/octoconv/internal/convert"
)

const (
	madMax255        = 1.0
	fracOverMax      = 0.001
	channelThreshold = 16
	bboxMaxDeltaPx   = 1
	maxSVGBytes      = 1 << 30
)

type box struct{ X, Y, W, H int }

type frameFacts struct {
	Width, Height int
	Alpha         bool
	Verdict       string
	Box           box
	Pix           *image.NRGBA
}

type pngComparison struct {
	MAD255, FracOver16 float64
	BoxDelta           int
	Within             bool
	Reasons            []string
	statsKnown         bool
}

var verdictToken = regexp.MustCompile(`\bJ[0-9]\b`)

func canonicalJSON(r io.Reader) ([]byte, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty JSON input")
		}
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("trailing data after the JSON value")
		}
		return nil, fmt.Errorf("trailing data after the JSON value: %w", err)
	}
	return json.Marshal(v)
}

func pixelStats(a, b *image.NRGBA) (float64, float64) {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	var sum, over int64
	for y := 0; y < h; y++ {
		ra := a.Pix[y*a.Stride : y*a.Stride+4*w]
		rb := b.Pix[y*b.Stride : y*b.Stride+4*w]
		for x := 0; x < w; x++ {
			worst := 0
			for c := 0; c < 4; c++ {
				d := int(ra[4*x+c]) - int(rb[4*x+c])
				if d < 0 {
					d = -d
				}
				sum += int64(d)
				worst = max(worst, d)
			}
			if worst > channelThreshold {
				over++
			}
		}
	}
	px := int64(w) * int64(h)
	if px == 0 {
		return 0, 0
	}
	return float64(sum) / float64(4*px), float64(over) / float64(px)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func boxDelta(a, b box) int {
	return max(abs(a.X-b.X), abs(a.Y-b.Y), abs((a.X+a.W)-(b.X+b.W)), abs((a.Y+a.H)-(b.Y+b.H)))
}

func compareFrames(a, b frameFacts) pngComparison {
	var c pngComparison
	if a.Width != b.Width || a.Height != b.Height {
		c.Reasons = append(c.Reasons, "dims")
	}
	if a.Alpha != b.Alpha {
		c.Reasons = append(c.Reasons, "alpha")
	}
	if a.Verdict != b.Verdict {
		c.Reasons = append(c.Reasons, "verdict")
	}
	c.BoxDelta = boxDelta(a.Box, b.Box)
	if c.BoxDelta > bboxMaxDeltaPx {
		c.Reasons = append(c.Reasons, "bbox")
	}
	if a.Width == b.Width && a.Height == b.Height && a.Pix != nil && b.Pix != nil {
		c.MAD255, c.FracOver16 = pixelStats(a.Pix, b.Pix)
		c.statsKnown = true
		if !(c.MAD255 <= madMax255) {
			c.Reasons = append(c.Reasons, "mad")
		}
		if !(c.FracOver16 <= fracOverMax) {
			c.Reasons = append(c.Reasons, "frac")
		}
	} else {
		c.Reasons = append(c.Reasons, "pixels_not_compared")
	}
	c.Within = len(c.Reasons) == 0
	return c
}

func pngHasAlpha(data []byte) bool {
	if len(data) < 33 {
		return false
	}
	if ct := data[25]; ct == 4 || ct == 6 {
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

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA))
		}
	}
	return out
}

func frameOf(data []byte, size int, background string, lb *convert.CADLetterbox) (frameFacts, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return frameFacts{}, fmt.Errorf("not a decodable PNG: %w", err)
	}
	pix := toNRGBA(img)
	f := frameFacts{Width: pix.Rect.Dx(), Height: pix.Rect.Dy(), Alpha: pngHasAlpha(data), Pix: pix}
	var facts convert.CADPixelFacts
	if lb != nil {
		facts, err = convert.JudgeCADDrawingPNG(bytes.NewReader(data), size, background, *lb)
	} else {
		facts, err = convert.JudgeCADPNG(bytes.NewReader(data), size, background)
	}
	f.Box = box{X: facts.BoxX, Y: facts.BoxY, W: facts.BoxW, H: facts.BoxH}
	switch {
	case err == nil:
		f.Verdict = "ok"
	case verdictToken.MatchString(err.Error()):
		f.Verdict = verdictToken.FindString(err.Error())
	default:
		f.Verdict = "reject"
	}
	return f, nil
}

func letterboxOf(svg string, size int) (convert.CADLetterbox, error) {
	fh, err := os.Open(svg)
	if err != nil {
		return convert.CADLetterbox{}, err
	}
	defer fh.Close()
	facts, err := convert.ValidateCADSVG(fh, io.Discard, maxSVGBytes)
	if err != nil {
		return convert.CADLetterbox{}, err
	}
	return convert.CADDrawingLetterbox(facts.WidthMM, facts.HeightMM, size)
}

func pair(a, b int) string {
	if a == b {
		return fmt.Sprint(a)
	}
	return fmt.Sprintf("%d:%d", a, b)
}

func boxText(b box) string { return fmt.Sprintf("%d:%d:%d:%d", b.X, b.Y, b.W, b.H) }

func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

func usage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: cad-png-compare --json A.json B.json")
	fmt.Fprintln(stderr, "       cad-png-compare --size N --background opaque|transparent [--drawing --letterbox-svg FILE] A.png B.png")
	return 64
}

func runJSON(a, b string, stdout, stderr io.Writer) int {
	var raw [2][]byte
	var canon [2][]byte
	for i, p := range []string{a, b} {
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(stderr, "cad-png-compare: %v\n", err)
			return 2
		}
		c, err := canonicalJSON(bytes.NewReader(data))
		if err != nil {
			fmt.Fprintf(stderr, "cad-png-compare: %s: %v\n", p, err)
			return 2
		}
		raw[i], canon[i] = data, c
	}
	equal := bytes.Equal(canon[0], canon[1])
	fmt.Fprintf(stdout, "JSON_COMPARE,equal=%d,a_bytes=%d,b_bytes=%d,canonical_bytes=%s\n",
		bit(equal), len(raw[0]), len(raw[1]), pair(len(canon[0]), len(canon[1])))
	if !equal {
		return 1
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cad-png-compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonMode := fs.Bool("json", false, "compare two JSON documents after canonical encoding")
	size := fs.Int("size", 0, "requested frame size in pixels")
	background := fs.String("background", "", "opaque or transparent")
	drawing := fs.Bool("drawing", false, "judge DXF drawing frames against the letterbox of --letterbox-svg")
	letterboxSVG := fs.String("letterbox-svg", "", "SVG whose root size sets the drawing letterbox")
	if err := fs.Parse(args); err != nil {
		return usage(stderr)
	}
	if fs.NArg() != 2 {
		return usage(stderr)
	}
	if *jsonMode {
		if *size != 0 || *background != "" || *drawing || *letterboxSVG != "" {
			return usage(stderr)
		}
		return runJSON(fs.Arg(0), fs.Arg(1), stdout, stderr)
	}
	if *size < convert.CADMinRenderSize || *size > convert.CADMaxRenderSize {
		return usage(stderr)
	}
	if *background != convert.CADBackgroundOpq && *background != convert.CADBackgroundTr {
		return usage(stderr)
	}
	if *drawing != (*letterboxSVG != "") {
		return usage(stderr)
	}
	var lb *convert.CADLetterbox
	if *drawing {
		b, err := letterboxOf(*letterboxSVG, *size)
		if err != nil {
			fmt.Fprintf(stderr, "cad-png-compare: letterbox of %s: %v\n", *letterboxSVG, err)
			return 2
		}
		lb = &b
	}
	var frames [2]frameFacts
	for i := 0; i < 2; i++ {
		p := fs.Arg(i)
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(stderr, "cad-png-compare: %v\n", err)
			return 2
		}
		f, err := frameOf(data, *size, *background, lb)
		if err != nil {
			fmt.Fprintf(stderr, "cad-png-compare: %s: %v\n", p, err)
			return 2
		}
		frames[i] = f
	}
	a, b := frames[0], frames[1]
	c := compareFrames(a, b)
	mad, frac := "na", "na"
	if c.statsKnown {
		mad, frac = fmt.Sprintf("%.6f", c.MAD255), fmt.Sprintf("%.6f", c.FracOver16)
	}
	reasons := "none"
	if len(c.Reasons) > 0 {
		reasons = strings.Join(c.Reasons, ";")
	}
	fmt.Fprintf(stdout, "PNG_COMPARE,w=%s,h=%s,alpha_a=%d,alpha_b=%d,verdict_a=%s,verdict_b=%s,bbox_a=%s,bbox_b=%s,bbox_max_delta_px=%d,mad_255=%s,frac_over_16=%s,within=%d,reasons=%s\n",
		pair(a.Width, b.Width), pair(a.Height, b.Height), bit(a.Alpha), bit(b.Alpha), a.Verdict, b.Verdict,
		boxText(a.Box), boxText(b.Box), c.BoxDelta, mad, frac, bit(c.Within), reasons)
	if !c.Within {
		return 1
	}
	return 0
}
