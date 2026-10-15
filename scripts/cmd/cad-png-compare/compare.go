package main

import (
	"image"
	"io"

	"github.com/apaderin/octoconv/internal/convert"
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
}

func canonicalJSON(r io.Reader) ([]byte, error) { return nil, nil }

func pixelStats(a, b *image.NRGBA) (float64, float64) { return 0, 0 }

func boxDelta(a, b box) int { return 0 }

func compareFrames(a, b frameFacts) pngComparison { return pngComparison{Within: true} }

func frameOf(data []byte, size int, background string, lb *convert.CADLetterbox) (frameFacts, error) {
	return frameFacts{}, nil
}

func run(args []string, stdout, stderr io.Writer) int { return 0 }
