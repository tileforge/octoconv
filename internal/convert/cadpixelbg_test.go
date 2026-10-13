package convert

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

const cadRenderBGDir = "testdata/cad/render-bg"

var (
	pixClear     = color.NRGBA{R: 255, G: 255, B: 255, A: 0}
	pixClearDark = color.NRGBA{}
)

func pixOnWhite(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	for i := 0; i < len(src.Pix); i += 4 {
		a := int(src.Pix[i+3])
		for c := 0; c < 3; c++ {
			dst.Pix[i+c] = uint8((int(src.Pix[i+c])*a + 255*(255-a) + 127) / 255)
		}
		dst.Pix[i+3] = 255
	}
	return dst
}

func pixRing(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	pixRect(img, x0, y0, x1, y0, c)
	pixRect(img, x0, y1, x1, y1, c)
	pixRect(img, x0, y0, x0, y1, c)
	pixRect(img, x1, y0, x1, y1, c)
}

type cadJFacts struct {
	Count, DarkCount       int
	Frac, Dark             float64
	BoxX, BoxY, BoxW, BoxH int
	Margin                 int
	Mode                   string
}

func cadJ(f CADPixelFacts) cadJFacts {
	return cadJFacts{f.Count, f.DarkCount, f.Frac, f.Dark, f.BoxX, f.BoxY, f.BoxW, f.BoxH, f.Margin, f.Mode}
}

func errText(err error) string {
	if err == nil {
		return "<accepted>"
	}
	return err.Error()
}

type cadBGScene struct {
	name string
	size int
	draw func(img *image.NRGBA)
	want string
}

func pixSparseAssembly(img *image.NRGBA) {
	for _, c := range [][2]int{{200, 200}, {1080, 200}, {200, 1080}, {1080, 1080}} {
		pixHexagon(img, c[0], c[1], 84)
	}
}

func cadBGScenes() []cadBGScene {
	return []cadBGScene{
		{"sparse_assembly", 1280, pixSparseAssembly, "blob"},
		{"sparse_assembly_at_256", 256, func(img *image.NRGBA) {
			for _, c := range [][2]int{{40, 40}, {216, 40}, {40, 216}, {216, 216}} {
				pixHexagon(img, c[0], c[1], 17)
			}
		}, "blob"},
		{"dense_part", 256, func(img *image.NRGBA) { pixHexagon(img, 128, 128, 100) }, "blob"},
		{"thin_band", 256, func(img *image.NRGBA) {
			pixRect(img, 118, 25, 118, 89, pixDark)
			pixRect(img, 137, 110, 137, 230, pixLight)
		}, "thin"},
		{"edge_on_sliver", 256, func(img *image.NRGBA) { pixRect(img, 16, 127, 240, 128, pixDark) }, "thin"},
		{"empty_frame", 256, func(*image.NRGBA) {}, "J1"},
		{"full_coverage", 256, func(img *image.NRGBA) { pixRect(img, 0, 0, 255, 255, pixLight) }, "J3"},
		{"dot", 256, func(img *image.NRGBA) { pixRect(img, 120, 120, 135, 135, pixDark) }, "J2"},
		{"antialiased_rim", 256, func(img *image.NRGBA) {
			pixHexagon(img, 128, 128, 100)
			for i, a := range []uint8{160, 64, 8, 5, 2, 1} {
				d := 112 + i
				pixRing(img, 128-d, 128-d, 128+d, 128+d, color.NRGBA{A: a})
			}
		}, "blob"},
		{"near_white_lit_faces", 256, func(img *image.NRGBA) {
			pixHexagon(img, 128, 128, 100)
			pixRect(img, 60, 100, 196, 156, color.NRGBA{R: 252, G: 252, B: 252, A: 255})
			pixRect(img, 28, 126, 228, 130, color.NRGBA{R: 250, G: 251, B: 255, A: 255})
		}, "blob"},
		{"fxaa_fringe", 256, func(img *image.NRGBA) {
			pixHexagon(img, 128, 128, 100)
			pixRing(img, 20, 20, 235, 235, color.NRGBA{R: 120, G: 120, B: 120, A: 0})
		}, "blob"},
		{"translucent_part", 256, func(img *image.NRGBA) {
			pixDisc(img, 128, 128, 84, color.NRGBA{R: 40, G: 40, B: 45, A: 128})
			pixDisc(img, 128, 128, 76, color.NRGBA{A: 230})
		}, "blob"},
	}
}

func TestJudgeCADPNGGivesOneVerdictOnBothBackgrounds(t *testing.T) {
	for i, page := range []color.NRGBA{pixClear, pixClearDark} {
		for _, sc := range cadBGScenes() {
			if i > 0 && sc.size > 256 {
				continue
			}
			t.Run(sc.name, func(t *testing.T) {
				tr := pixFrame(sc.size, page)
				sc.draw(tr)
				op := pixOnWhite(tr)
				fo, errO := pixJudge(t, pixEncode(t, op), sc.size, CADBackgroundOpq)
				ft, errT := pixJudge(t, pixEncode(t, tr), sc.size, CADBackgroundTr)
				if errText(errO) != errText(errT) {
					t.Fatalf("page %v: opaque %s, transparent %s", page, errText(errO), errText(errT))
				}
				if cadJ(fo) != cadJ(ft) {
					t.Fatalf("page %v: J facts differ:\nopaque      %+v\ntransparent %+v", page, cadJ(fo), cadJ(ft))
				}
				switch sc.want {
				case "blob", "thin":
					if errO != nil || fo.Mode != sc.want {
						t.Fatalf("got %s mode %q, want accepted as %s", errText(errO), fo.Mode, sc.want)
					}
				default:
					requireRejected(t, errO, sc.want)
				}
				if fo.HasAlpha || fo.FracTransparent != 0 || (ft.FracTransparent > 0 && (!ft.HasAlpha || ft.RingNonTransparent != 0)) {
					t.Errorf("alpha facts: opaque %+v transparent %+v", fo, ft)
				}
			})
		}
	}
}

func TestJudgeCADPNGSparseSceneIsTheM2Shape(t *testing.T) {
	for _, sc := range cadBGScenes()[:2] {
		tr := pixFrame(sc.size, pixClear)
		sc.draw(tr)
		f, err := pixJudge(t, pixEncode(t, tr), sc.size, CADBackgroundTr)
		if err != nil {
			t.Fatalf("%s rejected: %v", sc.name, err)
		}
		if f.Frac < 0.04 || f.Frac > 0.05 || f.FracTransparent <= 0.95 {
			t.Errorf("%s: frac %.4f transparent %.4f: the case must hold 4-5%% ink and over 95%% transparent pixels to be the M2 shape", sc.name, f.Frac, f.FracTransparent)
		}
	}
}

func TestJudgeCADPNGTransparentRouteChecksOnlyTheAlphaChannel(t *testing.T) {
	part := func(page color.NRGBA) *image.NRGBA {
		img := pixFrame(256, page)
		pixHexagon(img, 128, 128, 100)
		return img
	}
	at := func(x, y int, c color.NRGBA) *image.NRGBA {
		img := part(pixClear)
		img.SetNRGBA(x, y, c)
		return img
	}
	faint := color.NRGBA{R: 255, G: 255, B: 255, A: 1}
	insideFaint := part(pixClear)
	for _, p := range [][2]int{{128, 1}, {1, 128}, {254, 128}, {128, 254}} {
		insideFaint.SetNRGBA(p[0], p[1], faint)
	}
	for _, c := range []struct {
		name string
		img  *image.NRGBA
		rule string
		ring int
	}{
		{"alpha_254_white_background", part(color.NRGBA{R: 255, G: 255, B: 255, A: 254}), "edge", 4 * 255},
		{"one_opaque_corner", at(255, 255, pixWhite), "edge", 1},
		{"one_faint_top_pixel", at(128, 0, faint), "edge", 1},
		{"one_faint_bottom_pixel", at(128, 255, faint), "edge", 1},
		{"one_faint_left_pixel", at(0, 128, faint), "edge", 1},
		{"one_faint_right_pixel", at(255, 128, faint), "edge", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, err := pixJudge(t, pixEncode(t, c.img), 256, CADBackgroundTr)
			requireRejected(t, err, c.rule)
			if f.RingNonTransparent != c.ring || f.Mode != "blob" {
				t.Errorf("facts %+v, want %d non-transparent edge pixels and the J4 mode kept", f, c.ring)
			}
		})
	}
	if f, err := pixJudge(t, pixEncode(t, insideFaint), 256, CADBackgroundTr); err != nil || f.RingNonTransparent != 0 {
		t.Errorf("faint pixels one pixel inside the edge: ring %d, %v", f.RingNonTransparent, err)
	}
	_, err := pixJudge(t, pixEncode(t, pixOnWhite(part(pixClear))), 256, CADBackgroundTr)
	requireRejected(t, err, "alpha")
}

func TestJudgeCADPNGOpaqueRouteChecksOnlyTheAlphaChannel(t *testing.T) {
	tr := pixFrame(256, pixClear)
	pixHexagon(tr, 128, 128, 100)
	img := pixOnWhite(tr)
	img.SetNRGBA(128, 128, color.NRGBA{})
	f, err := pixJudge(t, pixEncode(t, img), 256, CADBackgroundOpq)
	requireRejected(t, err, "transparent")
	if f.Mode != "blob" {
		t.Errorf("mode %q, want the J4 mode kept", f.Mode)
	}
}

func TestJudgeCADDrawingPNGGivesOneVerdictOnBothBackgrounds(t *testing.T) {
	square, err := CADDrawingLetterbox(297, 297, 256)
	if err != nil {
		t.Fatal(err)
	}
	wide, err := CADDrawingLetterbox(297, 22.8, 256)
	if err != nil {
		t.Fatal(err)
	}
	outline := func(img *image.NRGBA, c color.NRGBA) {
		pixRing(img, 8, 8, 247, 247, c)
		pixRing(img, 9, 9, 246, 246, c)
	}
	for _, c := range []struct {
		name string
		box  CADLetterbox
		draw func(img *image.NRGBA)
		want string
	}{
		{"square", square, func(img *image.NRGBA) { outline(img, pixDark) }, ""},
		{"faint_square", square, func(img *image.NRGBA) { outline(img, color.NRGBA{A: 128}) }, ""},
		{"white_lines_beyond_black", square, func(img *image.NRGBA) {
			outline(img, pixWhite)
			pixRect(img, 120, 120, 136, 120, pixDark)
		}, "J2"},
		{"white_only", square, func(img *image.NRGBA) { outline(img, pixWhite) }, "J1"},
		{"alpha_1_strokes_reach_the_edge", wide, func(img *image.NRGBA) {
			pixRect(img, 8, 127, 247, 128, pixDark)
			pixRect(img, 1, 60, 254, 60, color.NRGBA{A: 1})
		}, ""},
		{"blank", square, func(*image.NRGBA) {}, "J1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := pixFrame(256, pixClear)
			c.draw(tr)
			fo, errO := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, pixOnWhite(tr))), 256, CADBackgroundOpq, c.box)
			ft, errT := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, tr)), 256, CADBackgroundTr, c.box)
			if errText(errO) != errText(errT) || cadJ(fo) != cadJ(ft) {
				t.Fatalf("opaque %s %+v\ntransparent %s %+v", errText(errO), cadJ(fo), errText(errT), cadJ(ft))
			}
			if c.want == "" {
				if errO != nil {
					t.Fatalf("rejected: %v", errO)
				}
				return
			}
			requireRejected(t, errO, c.want)
		})
	}
}

func TestJudgeCADDrawingPNGTransparentRouteChecksOnlyTheAlphaChannel(t *testing.T) {
	box, err := CADDrawingLetterbox(297, 297, 256)
	if err != nil {
		t.Fatal(err)
	}
	img := pixFrame(256, color.NRGBA{R: 255, G: 255, B: 255, A: 10})
	pixRing(img, 8, 8, 247, 247, pixDark)
	f, err := JudgeCADDrawingPNG(bytes.NewReader(pixEncode(t, img)), 256, CADBackgroundTr, box)
	requireRejected(t, err, "edge")
	if f.RingNonTransparent != 4*255 {
		t.Errorf("RingNonTransparent = %d, want %d", f.RingNonTransparent, 4*255)
	}
}

func TestCADPartPixelIsDecidedOnAWhitePage(t *testing.T) {
	for _, c := range []struct {
		px   color.NRGBA
		part bool
		dark bool
	}{
		{color.NRGBA{A: 5}, false, false},
		{color.NRGBA{A: 6}, true, false},
		{color.NRGBA{A: 175}, true, false},
		{color.NRGBA{A: 176}, true, true},
		{color.NRGBA{R: 249, G: 255, B: 255, A: 255}, true, false},
		{color.NRGBA{R: 250, G: 250, B: 250, A: 255}, false, false},
		{color.NRGBA{R: 79, G: 79, B: 79, A: 255}, true, true},
		{color.NRGBA{R: 120, G: 120, B: 120, A: 0}, false, false},
		{color.NRGBA{R: 255, G: 255, B: 255, A: 254}, false, false},
	} {
		img := pixFrame(256, pixClear)
		img.SetNRGBA(100, 100, c.px)
		f, _ := pixJudge(t, pixEncode(t, img), 256, CADBackgroundTr)
		if (f.Count == 1) != c.part || (f.DarkCount == 1) != c.dark {
			t.Errorf("%v: count %d dark %d, want part %v dark %v", c.px, f.Count, f.DarkCount, c.part, c.dark)
		}
	}
}

type cadRealPair struct {
	name, mode string
	size       int
}

var cadRealPairs = []cadRealPair{
	{"nauo4_iso_1280", "blob", 1280},
	{"linearslide_iso_256", "blob", 256},
	{"w15_iso_256", "thin", 256},
	{"degenerate_plate_front_1280", "thin", 1280},
	{"degenerate_rod_iso_256", "J4", 256},
}

func cadStraightPix(t *testing.T, data []byte) []uint8 {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	switch p := img.(type) {
	case *image.NRGBA:
		return p.Pix
	case *image.RGBA:
		for i := 3; i < len(p.Pix); i += 4 {
			if p.Pix[i] != 255 {
				t.Fatalf("a premultiplied frame with alpha %d", p.Pix[i])
			}
		}
		return p.Pix
	}
	t.Fatalf("decoded as %T", img)
	return nil
}

func cadPartOnWhite(px []uint8) bool {
	a := int(px[3])
	for _, v := range px[:3] {
		if (int(v)*a+255*(255-a)+127)/255 < cadBackgroundCutoff {
			return true
		}
	}
	return false
}

func TestJudgeCADPNGRealRenderPairsAgreeWithinTheTolerance(t *testing.T) {
	for _, p := range cadRealPairs {
		t.Run(p.name, func(t *testing.T) {
			opData, err := os.ReadFile(filepath.Join(cadRenderBGDir, p.name+"_opaque.png"))
			if err != nil {
				t.Fatal(err)
			}
			trData, err := os.ReadFile(filepath.Join(cadRenderBGDir, p.name+"_transparent.png"))
			if err != nil {
				t.Fatal(err)
			}
			fo, errO := pixJudge(t, opData, p.size, CADBackgroundOpq)
			ft, errT := pixJudge(t, trData, p.size, CADBackgroundTr)
			switch p.mode {
			case "blob", "thin":
				if errO != nil || errT != nil {
					t.Fatalf("opaque %s, transparent %s: (a) wants both accepted", errText(errO), errText(errT))
				}
				if fo.Mode != p.mode || ft.Mode != p.mode {
					t.Fatalf("modes %q and %q, want %q on both", fo.Mode, ft.Mode, p.mode)
				}
			default:
				requireRejected(t, errO, p.mode)
				requireRejected(t, errT, p.mode)
			}
			for _, d := range []int{fo.BoxX - ft.BoxX, fo.BoxY - ft.BoxY, fo.BoxW - ft.BoxW, fo.BoxH - ft.BoxH, fo.Margin - ft.Margin} {
				if d < -1 || d > 1 {
					t.Fatalf("(b) box or margin differ by more than 1 px: opaque %+v transparent %+v", cadJ(fo), cadJ(ft))
				}
			}
			if fo.DarkCount != ft.DarkCount {
				t.Fatalf("(c) dark counts %d and %d", fo.DarkCount, ft.DarkCount)
			}
			if p.mode == "blob" && float64(fo.Count-ft.Count) > 0.10*float64(fo.Count) {
				t.Fatalf("(e) counts %d and %d differ by more than 10%%", fo.Count, ft.Count)
			}
			op, tr := cadStraightPix(t, opData), cadStraightPix(t, trData)
			if len(op) != len(tr) {
				t.Fatalf("frames of %d and %d bytes", len(op), len(tr))
			}
			for i := 0; i < len(tr); i += 4 {
				if cadPartOnWhite(tr[i:i+4]) && !cadPartOnWhite(op[i:i+4]) {
					t.Fatalf("(d) pixel %d is a part pixel only on the transparent render (%v against %v)", i/4, tr[i:i+4], op[i:i+4])
				}
			}
		})
	}
}
