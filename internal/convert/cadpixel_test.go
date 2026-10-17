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
	"os"
	"path/filepath"
	"strconv"
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

func TestJudgeCADPNGAcceptsW15ThinFrameAt256WithFewerPixelsThanItsExtent(t *testing.T) {
	img := pixFrame(256, pixWhite)
	pixRect(img, 118, 25, 118, 89, pixDark)
	pixRect(img, 137, 110, 137, 230, pixLight)
	f, err := pixJudge(t, pixEncode(t, img), 256, "opaque")
	if err != nil {
		t.Fatalf("W15 at 256 rejected: %v", err)
	}
	if f.Mode != "thin" || f.Count != 186 || f.DarkCount != 65 || f.BoxW != 20 || f.BoxH != 206 || f.Margin != 25 {
		t.Errorf("facts = %+v, want thin, 186 part pixels, 65 dark, 20 x 206, margin 25", f)
	}
	if f.Count >= max(f.BoxW, f.BoxH) {
		t.Errorf("count %d is not below the %d px extent: the case no longer exercises the span check", f.Count, max(f.BoxW, f.BoxH))
	}
}

func TestJudgeCADPNGRejectsSparseNoise(t *testing.T) {
	scattered := pixFrame(256, pixWhite)
	for _, p := range [][2]int{{30, 30}, {225, 30}, {30, 225}, {225, 225}, {128, 128}} {
		scattered.SetNRGBA(p[0], p[1], pixDark)
	}
	_, err := pixJudge(t, pixEncode(t, scattered), 256, "opaque")
	requireRejected(t, err, "J4")

	dotted := pixFrame(256, pixWhite)
	for y := 25; y <= 230; y += 20 {
		dotted.SetNRGBA(128, y, pixDark)
	}
	f, err := pixJudge(t, pixEncode(t, dotted), 256, "opaque")
	requireRejected(t, err, "J4")
	if float64(f.BoxW)/float64(f.BoxH) >= cadThinAspectMax {
		t.Errorf("box %d x %d is not thin: the dotted line must fail on dark density, not on aspect", f.BoxW, f.BoxH)
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
	requireRejected(t, err, "edge")
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

type cadFrameRow struct {
	name, background string
	size             int
	wMM, hMM         float64
	box              CADLetterbox
}

func cadFrameRows(t *testing.T) []cadFrameRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cadSVGTestDir, "frames.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if lines[0] != "name\tsize\tbackground\tw_mm\th_mm\tsw\tsh\tleft\ttop" {
		t.Fatalf("frames.tsv header = %q", lines[0])
	}
	var rows []cadFrameRow
	for _, line := range lines[1:] {
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("frames.tsv row %q", line)
		}
		var n [9]float64
		for i := 1; i < 9; i++ {
			if i == 2 {
				continue
			}
			v, err := strconv.ParseFloat(f[i], 64)
			if err != nil {
				t.Fatal(err)
			}
			n[i] = v
		}
		rows = append(rows, cadFrameRow{
			name: f[0], background: f[2], size: int(n[1]), wMM: n[3], hMM: n[4],
			box: CADLetterbox{Left: int(n[7]), Top: int(n[8]), Width: int(n[5]), Height: int(n[6])},
		})
	}
	if len(rows) < 3 {
		t.Fatalf("frames.tsv has %d rows", len(rows))
	}
	return rows
}

func TestCADDrawingLetterboxReproducesTheProbeFrames(t *testing.T) {
	for _, r := range cadFrameRows(t) {
		got, err := CADDrawingLetterbox(r.wMM, r.hMM, r.size)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		if got != r.box {
			t.Errorf("%s: letterbox %+v, the probe drew %+v", r.name, got, r.box)
		}
	}
	sq, err := CADDrawingLetterbox(297, 297, 1280)
	if err != nil || sq != (CADLetterbox{Left: 38, Top: 38, Width: 1203, Height: 1203}) {
		t.Fatalf("square at 1280: %+v %v", sq, err)
	}
	thin, err := CADDrawingLetterbox(297, 0.001, 256)
	if err != nil || thin.Height != 1 || thin.Top != 127 {
		t.Fatalf("a hairline drawing: %+v %v", thin, err)
	}
	for _, bad := range []struct {
		w, h float64
		size int
	}{{0, 10, 1280}, {10, -1, 1280}, {math.NaN(), 10, 1280}, {math.Inf(1), 10, 1280}, {10, 10, 255}, {10, 10, 2049}} {
		if b, err := CADDrawingLetterbox(bad.w, bad.h, bad.size); err == nil {
			t.Errorf("%v x %v at %d gave %+v, want an error", bad.w, bad.h, bad.size, b)
		}
	}
}

func TestJudgeCADDrawingPNGAcceptsTheHelperFrames(t *testing.T) {
	facts := map[string]CADPixelFacts{}
	for _, r := range cadFrameRows(t) {
		data, err := os.ReadFile(filepath.Join(cadSVGTestDir, r.name))
		if err != nil {
			t.Fatal(err)
		}
		f, err := JudgeCADDrawingPNG(bytes.NewReader(data), r.size, r.background, r.box)
		if err != nil {
			t.Fatalf("%s rejected: %v", r.name, err)
		}
		if f.Mode != "drawing" || f.Width != r.size || f.Height != r.size {
			t.Errorf("%s: facts %+v", r.name, f)
		}
		facts[r.name] = f
	}
	for _, name := range []string{"wide", "text_alpha_faint"} {
		o, okO := facts[name+"_1280_opaque.png"]
		tr, okT := facts[name+"_1280_transparent.png"]
		if !okO || !okT {
			t.Fatalf("frames.tsv lacks the 1280 opaque and transparent frames of %s", name)
		}
		if cadJ(o) != cadJ(tr) {
			t.Errorf("%s: the opaque frame judges as %+v, its transparent twin as %+v", name, cadJ(o), cadJ(tr))
		}
	}
}

func TestJudgeCADDrawingPNGAcceptsAFrameWhoseOnlyInkIsHalfTransparentText(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	cadDrawingSquare(img, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	if _, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, img)), 1280, "opaque", cadSquareBox); err != nil {
		t.Fatalf("opaque: rejected: %v", err)
	}
	img = pixFrame(1280, color.NRGBA{})
	cadDrawingSquare(img, color.NRGBA{R: 0, G: 0, B: 0, A: 127})
	if _, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, img)), 1280, "transparent", cadSquareBox); err != nil {
		t.Fatalf("transparent: rejected: %v", err)
	}
	for _, name := range []string{"text_alpha_faint_1280_opaque.png", "text_alpha_faint_1280_transparent.png"} {
		data, err := os.ReadFile(filepath.Join(cadSVGTestDir, name))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if strings.Contains(name, "transparent") && c.A > 128 {
					t.Fatalf("%s: pixel %d,%d is %v, more opaque than half-transparent text", name, x, y, c)
				}
				if strings.Contains(name, "opaque") && c.R < 120 {
					t.Fatalf("%s: pixel %d,%d is %v, darker than half-transparent black on white", name, x, y, c)
				}
			}
		}
	}
}

func cadDrawingSquare(img *image.NRGBA, c color.NRGBA) {
	pixRect(img, 40, 40, 1239, 41, c)
	pixRect(img, 40, 1238, 1239, 1239, c)
	pixRect(img, 40, 40, 41, 1239, c)
	pixRect(img, 1238, 40, 1239, 1239, c)
}

var cadSquareBox = CADLetterbox{Left: 38, Top: 38, Width: 1203, Height: 1203}

func TestJudgeCADDrawingPNGAcceptsAHollowOutlineThe3DJudgeRejects(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	cadDrawingSquare(img, pixDark)
	data := pixEncode(t, img)
	f, err := JudgeCADDrawingPNG(bytes.NewReader(data), 1280, "opaque", cadSquareBox)
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if f.Mode != "drawing" || f.BoxX != 40 || f.BoxY != 40 || f.BoxW != 1200 || f.BoxH != 1200 || f.Margin != 40 {
		t.Errorf("facts = %+v", f)
	}
	if _, err := pixJudge(t, data, 1280, "opaque"); err == nil {
		t.Error("the 3D judge accepted the outline: the case no longer shows the drawing mode is needed")
	}
}

func TestJudgeCADDrawingPNGAcceptsAnAntialiasedHairlineWithNoDarkPixel(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	cadDrawingSquare(img, color.NRGBA{R: 160, G: 160, B: 160, A: 255})
	f, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, img)), 1280, "opaque", cadSquareBox)
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if f.DarkCount != 0 {
		t.Errorf("dark count %d, want 0 for the case to mean anything", f.DarkCount)
	}
}

func TestJudgeCADDrawingPNGAcceptsANearlyEmptyTransparentFrameWithoutOpaquePixels(t *testing.T) {
	img := pixFrame(1280, color.NRGBA{})
	half := color.NRGBA{R: 0, G: 0, B: 0, A: 128}
	pixRect(img, 40, 640, 1039, 640, half)
	pixRect(img, 640, 100, 640, 599, half)
	f, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, img)), 1280, "transparent", cadSquareBox)
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if f.FracTransparent < 0.999 || !f.HasAlpha {
		t.Errorf("facts = %+v, want a transparent fraction of at least 0.999", f)
	}
}

func TestJudgeCADDrawingPNGRejects(t *testing.T) {
	wideBox := CADLetterbox{Left: 38, Top: 594, Width: 1203, Height: 92}
	square := func() *image.NRGBA {
		img := pixFrame(1280, pixWhite)
		cadDrawingSquare(img, pixDark)
		return img
	}
	edge := square()
	pixRect(edge, 0, 640, 0, 640, pixDark)
	outside := pixFrame(1280, pixWhite)
	pixRect(outside, 40, 640, 1239, 641, pixDark)
	outside.SetNRGBA(640, wideBox.Top-3, pixDark)
	slack := pixFrame(1280, pixWhite)
	pixRect(slack, 40, 640, 1239, 641, pixDark)
	slack.SetNRGBA(640, wideBox.Top-2, pixDark)
	dot := pixFrame(1280, pixWhite)
	pixRect(dot, 590, 590, 689, 689, pixDark)
	holed := square()
	holed.SetNRGBA(5, 5, color.NRGBA{})
	tight := pixFrame(1280, color.NRGBA{R: 255, G: 255, B: 255, A: 10})
	cadDrawingSquare(tight, pixDark)
	tight.SetNRGBA(640, 640, color.NRGBA{})
	single := pixFrame(1280, pixWhite)
	single.SetNRGBA(640, 640, pixDark)
	cases := []struct {
		name       string
		img        image.Image
		size       int
		background string
		box        CADLetterbox
		rule       string
	}{
		{"blank", pixFrame(1280, pixWhite), 1280, "opaque", cadSquareBox, "J1"},
		{"blank_transparent", pixFrame(1280, color.NRGBA{R: 1, A: 0}), 1280, "transparent", cadSquareBox, "J1"},
		{"wrong_size", image.NewNRGBA(image.Rect(0, 0, 1280, 1279)), 1280, "opaque", cadSquareBox, "J0"},
		{"touches_edge", edge, 1280, "opaque", cadSquareBox, "J3"},
		{"three_px_outside_letterbox", outside, 1280, "opaque", wideBox, "J3"},
		{"dot", dot, 1280, "opaque", cadSquareBox, "J2"},
		{"opaque_with_transparent_pixel", holed, 1280, "opaque", cadSquareBox, "transparent"},
		{"transparent_without_alpha", square(), 1280, "transparent", cadSquareBox, "alpha"},
		{"transparent_background_not_transparent", tight, 1280, "transparent", cadSquareBox, "edge"},
		{"single_ink_pixel_wrong_span", single, 1280, "opaque", cadSquareBox, "J2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, c.img)), c.size, c.background, c.box)
			requireRejected(t, err, c.rule)
			if f.Mode != "drawing" {
				t.Errorf("mode %q, want drawing", f.Mode)
			}
		})
	}
	if _, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, slack)), 1280, "opaque", wideBox); err != nil {
		t.Fatalf("ink 2 px outside the letterbox is within the slack: %v", err)
	}
}

func TestJudgeCADDrawingPNGAcceptsAnySpanningInkHoweverSparse(t *testing.T) {
	box, err := CADDrawingLetterbox(297, 297, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sparse := pixFrame(2048, pixWhite)
	pixRect(sparse, 100, 1024, 1199, 1024, pixDark)
	twoDots := pixFrame(2048, pixWhite)
	twoDots.SetNRGBA(box.Left+64, box.Top+box.Height-65, pixDark)
	twoDots.SetNRGBA(box.Left+box.Width-65, box.Top+64, pixDark)
	for _, c := range []struct {
		name  string
		img   image.Image
		count int
	}{{"line_under_the_old_fraction", sparse, 1100}, {"two_far_dots", twoDots, 2}} {
		t.Run(c.name, func(t *testing.T) {
			f, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, c.img)), 2048, "opaque", box)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if f.Count != c.count || f.Frac >= 0.0003 {
				t.Errorf("count %d frac %.6f, want %d ink pixels under the old 0.0003 fraction", f.Count, f.Frac, c.count)
			}
		})
	}
}

func TestJudgeCADDrawingPNGBoundsItsArguments(t *testing.T) {
	img := pixFrame(1280, pixWhite)
	cadDrawingSquare(img, pixDark)
	data := pixEncode(t, img)
	for _, c := range []struct {
		size       int
		background string
		box        CADLetterbox
	}{
		{100, "opaque", cadSquareBox},
		{1280, "red", cadSquareBox},
		{1280, "opaque", CADLetterbox{Left: 38, Top: 38, Width: 0, Height: 1203}},
		{1280, "opaque", CADLetterbox{Left: -1, Top: 38, Width: 1203, Height: 1203}},
		{1280, "opaque", CADLetterbox{Left: 100, Top: 38, Width: 1203, Height: 1203}},
	} {
		_, err := JudgeCADDrawingPNG(bytes.NewReader(data), c.size, c.background, c.box)
		if err == nil || errors.Is(err, ErrCADRenderRejected) {
			t.Errorf("%+v: err = %v, want a usage error", c, err)
		}
	}
}
