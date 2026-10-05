package convert

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"
)

var (
	pixLight = color.NRGBA{R: 200, G: 200, B: 205, A: 255}
	pixDark  = color.NRGBA{R: 40, G: 40, B: 45, A: 255}
	pixWhite = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
)

func pixFrame(n int, bg color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bg.R, bg.G, bg.B, bg.A
	}
	return img
}

func pixRect(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func pixHexagon(img *image.NRGBA, cx, cy, r int) {
	h := float64(r) * math.Sqrt(3) / 2
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := math.Abs(float64(x-cx)), math.Abs(float64(y-cy))
			if dy <= h && dx+dy/math.Sqrt(3) <= float64(r) {
				c := pixLight
				if dx+dy/math.Sqrt(3) > float64(r)-24 || dy > h-24 {
					c = pixDark
				}
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

func pixDisc(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

func pixEncode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pixJudge(t *testing.T, data []byte, size int, background string) (CADPixelFacts, error) {
	t.Helper()
	return JudgeCADPNG(bytes.NewReader(data), size, background)
}

func pixThinBand(img *image.NRGBA) {
	pixRect(img, 594, 124, 594, 1153, pixDark)
	pixRect(img, 640, 124, 640, 1153, pixLight)
	pixRect(img, 686, 124, 686, 1153, pixLight)
	pixRect(img, 594, 124, 686, 124, pixLight)
	pixRect(img, 594, 1153, 686, 1153, pixLight)
}

func requireRejected(t *testing.T, err error, rule string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted, want a %s rejection", rule)
	}
	if !errors.Is(err, ErrCADRenderRejected) {
		t.Fatalf("err = %v, want it to wrap ErrCADRenderRejected", err)
	}
	if !strings.Contains(err.Error(), rule) {
		t.Fatalf("err = %v, want it to name %s", err, rule)
	}
}

func TestJudgeCADPNGAcceptsHexagonBlob(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixHexagon(img, 640, 640, 520)
	f, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	if err != nil {
		t.Fatalf("hexagon rejected: %v", err)
	}
	if f.Mode != "blob" || f.Frac < 0.02 || f.Frac > 0.90 || f.Margin < 76 {
		t.Errorf("facts = %+v, want a blob with frac in range and margin >= 76", f)
	}
}

func TestJudgeCADPNGAcceptsW15ThinBand(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixThinBand(img)
	f, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	if err != nil {
		t.Fatalf("W15 band rejected: %v", err)
	}
	if f.Mode != "thin" || f.BoxW != 93 || f.BoxH != 1030 || f.Margin != 124 {
		t.Errorf("facts = %+v, want thin 93 x 1030 margin 124", f)
	}
	if f.Frac >= 0.02 || f.Dark >= 0.001 {
		t.Errorf("frac %.4f dark %.4f: the band must fail the 71 blob rule for this case to mean anything", f.Frac, f.Dark)
	}
}

func TestJudgeCADPNGAcceptsEdgeOnSliver(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixRect(img, 77, 639, 1202, 640, pixDark)
	f, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	if err != nil {
		t.Fatalf("sliver rejected: %v", err)
	}
	if f.Mode != "thin" || f.BoxW != 1126 || f.BoxH != 2 {
		t.Errorf("facts = %+v, want thin 1126 x 2", f)
	}
}

func TestJudgeCADPNGRejectsEmptyFrame(t *testing.T) {
	f, err := pixJudge(t, pixEncode(t, pixFrame(1280, pixWhite)), 1280, "opaque")
	requireRejected(t, err, "J1")
	if f.Count != 0 || f.Mode != "none" {
		t.Errorf("facts = %+v, want an empty none frame", f)
	}
}

func TestJudgeCADPNGRejectsDot(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixRect(img, 620, 620, 659, 659, pixDark)
	_, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	requireRejected(t, err, "J2")
}

func TestJudgeCADPNGRejectsEdgeToEdgeFrame(t *testing.T) {
	img := pixFrame(1280, pixLight)
	_, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	requireRejected(t, err, "J3")
}

func TestJudgeCADPNGRejectsAllDarkNearlyFullFrame(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixRect(img, 15, 15, 1264, 1264, pixDark)
	f, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	requireRejected(t, err, "J3")
	if f.Frac < 0.9 {
		t.Errorf("frac = %.4f, want above 0.9", f.Frac)
	}
}

func TestJudgeCADPNGRejectsHollowOutlineThatIsNeitherBlobNorThin(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixRect(img, 140, 140, 1139, 141, pixDark)
	pixRect(img, 140, 1138, 1139, 1139, pixDark)
	pixRect(img, 140, 140, 141, 1139, pixDark)
	pixRect(img, 1138, 140, 1139, 1139, pixDark)
	_, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	requireRejected(t, err, "J4")
}

func TestJudgeCADPNGRejectsWrongFrameSizeBeforeAnalysis(t *testing.T) {
	img := pixFrame(640, pixWhite)
	pixHexagon(img, 320, 320, 260)
	f, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	requireRejected(t, err, "J0")
	if f.Count != 0 {
		t.Errorf("Count = %d, want pixel analysis skipped", f.Count)
	}
}

func TestJudgeCADPNGRejectsHugeHeaderWithoutDecodingTheBody(t *testing.T) {
	var ihdr bytes.Buffer
	binary.Write(&ihdr, binary.BigEndian, uint32(20000))
	binary.Write(&ihdr, binary.BigEndian, uint32(20000))
	ihdr.Write([]byte{8, 6, 0, 0, 0})
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	binary.Write(&b, binary.BigEndian, uint32(ihdr.Len()))
	chunk := append([]byte("IHDR"), ihdr.Bytes()...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	f, err := pixJudge(t, b.Bytes(), 1280, "opaque")
	requireRejected(t, err, "J0")
	if f.Width != 20000 {
		t.Errorf("Width = %d, want the declared 20000 reported", f.Width)
	}
}

func TestJudgeCADPNGRejectsNonPNG(t *testing.T) {
	_, err := pixJudge(t, []byte("not a png"), 1280, "opaque")
	if !errors.Is(err, ErrCADRenderRejected) {
		t.Fatalf("err = %v, want ErrCADRenderRejected", err)
	}
}

func TestJudgeCADPNGTransparentAcceptsAlphaFrame(t *testing.T) {
	img := pixFrame(1280, color.NRGBA{})
	pixDisc(img, 640, 640, 420, pixLight)
	pixDisc(img, 640, 640, 380, pixDark)
	f, err := pixJudge(t, pixEncode(t, img), 1280, "transparent")
	if err != nil {
		t.Fatalf("transparent frame rejected: %v", err)
	}
	if !f.HasAlpha || f.FracTransparent < 0.05 || f.FracTransparent > 0.95 || f.Mode != "blob" {
		t.Errorf("facts = %+v", f)
	}
}

func TestJudgeCADPNGTransparentRejectsFrameWithoutAlpha(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixHexagon(img, 640, 640, 520)
	_, err := pixJudge(t, pixEncode(t, img), 1280, "transparent")
	requireRejected(t, err, "alpha")
}

func TestJudgeCADPNGTransparentRejectsOpaqueBackground(t *testing.T) {
	img := pixFrame(1280, color.NRGBA{R: 255, G: 255, B: 255, A: 254})
	pixDisc(img, 640, 640, 420, pixDark)
	_, err := pixJudge(t, pixEncode(t, img), 1280, "transparent")
	requireRejected(t, err, "transparent fraction")
}

func TestJudgeCADPNGOpaqueRejectsTransparentPixels(t *testing.T) {
	img := pixFrame(1280, color.NRGBA{})
	pixDisc(img, 640, 640, 420, pixDark)
	_, err := pixJudge(t, pixEncode(t, img), 1280, "opaque")
	requireRejected(t, err, "transparent")
}

func TestJudgeCADPNGOpaqueAcceptsRGBAWithFullAlpha(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	pixHexagon(img, 640, 640, 520)
	data := pixEncode(t, img)
	f, err := pixJudge(t, data, 1280, "opaque")
	if err != nil || f.FracTransparent != 0 {
		t.Fatalf("full-alpha frame: facts %+v err %v", f, err)
	}
	rgba := image.NewRGBA(img.Bounds())
	for y := 0; y < 1280; y++ {
		for x := 0; x < 1280; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	if _, err := pixJudge(t, pixEncode(t, rgba), 1280, "opaque"); err != nil {
		t.Fatalf("RGBA frame rejected: %v", err)
	}
}

func TestCADPixelJudgeBoundsItsArguments(t *testing.T) {
	data := pixEncode(t, pixFrame(1280, pixWhite))
	if _, err := pixJudge(t, data, 100, "opaque"); err == nil {
		t.Error("size 100 was accepted")
	}
	if _, err := pixJudge(t, data, 4096, "opaque"); err == nil {
		t.Error("size 4096 was accepted")
	}
	if _, err := pixJudge(t, data, 1280, "red"); err == nil {
		t.Error("background red was accepted")
	}
}

func TestCADPixelJudgeAcceptsTheSmallestFrame(t *testing.T) {
	img := pixFrame(256, pixWhite)
	pixHexagon(img, 128, 128, 100)
	f, err := pixJudge(t, pixEncode(t, img), 256, "opaque")
	if err != nil || f.Mode != "blob" {
		t.Fatalf("256 frame: facts %+v err %v", f, err)
	}
}
