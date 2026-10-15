package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func filled(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func partFrame(size int, bg color.NRGBA, x0, y0, x1, y1 int) *image.NRGBA {
	img := filled(size, size, bg)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetNRGBA(x, y, color.NRGBA{20, 20, 20, 255})
		}
	}
	return img
}

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func okFacts(img *image.NRGBA) frameFacts {
	b := img.Bounds()
	return frameFacts{Width: b.Dx(), Height: b.Dy(), Verdict: "ok", Box: box{X: 10, Y: 10, W: 50, H: 50}, Pix: img}
}

var white = color.NRGBA{255, 255, 255, 255}

func TestPixelStatsIdenticalFrames(t *testing.T) {
	a := partFrame(100, white, 20, 20, 80, 80)
	b := partFrame(100, white, 20, 20, 80, 80)
	mad, frac := pixelStats(a, b)
	if mad != 0 || frac != 0 {
		t.Fatalf("identical frames: mad %v frac %v, want 0 0", mad, frac)
	}
	got := compareFrames(okFacts(a), okFacts(b))
	if !got.Within || got.MAD255 != 0 || got.FracOver16 != 0 {
		t.Fatalf("identical frames: %+v, want within with zero statistics", got)
	}
}

func TestMeanDifferenceBoundaryIsInclusive(t *testing.T) {
	base := filled(100, 100, color.NRGBA{100, 100, 100, 200})
	one := filled(100, 100, color.NRGBA{101, 101, 101, 201})
	two := filled(100, 100, color.NRGBA{102, 102, 102, 202})
	mad, frac := pixelStats(base, one)
	if mad != 1 || frac != 0 {
		t.Fatalf("every channel off by 1: mad %v frac %v, want 1 0", mad, frac)
	}
	if got := compareFrames(okFacts(base), okFacts(one)); !got.Within {
		t.Fatalf("a mean difference of exactly 1/255 must be within: %+v", got)
	}
	mad, _ = pixelStats(base, two)
	if mad != 2 {
		t.Fatalf("every channel off by 2: mad %v, want 2", mad)
	}
	got := compareFrames(okFacts(base), okFacts(two))
	if got.Within || !strings.Contains(strings.Join(got.Reasons, ";"), "mad") {
		t.Fatalf("a mean difference of 2/255 must be outside with reason mad: %+v", got)
	}
}

func shifted(n int, by uint8) *image.NRGBA {
	img := filled(100, 100, color.NRGBA{100, 100, 100, 255})
	for i := 0; i < n; i++ {
		img.SetNRGBA(i, 0, color.NRGBA{100 + by, 100, 100, 255})
	}
	return img
}

func TestFractionBoundaryIsInclusive(t *testing.T) {
	base := filled(100, 100, color.NRGBA{100, 100, 100, 255})
	_, frac := pixelStats(base, shifted(10, 17))
	if frac != 0.001 {
		t.Fatalf("10 of 10000 pixels off by 17: frac %v, want 0.001", frac)
	}
	if got := compareFrames(okFacts(base), okFacts(shifted(10, 17))); !got.Within {
		t.Fatalf("a fraction of exactly 0.001 must be within: %+v", got)
	}
	got := compareFrames(okFacts(base), okFacts(shifted(11, 17)))
	if got.Within || !strings.Contains(strings.Join(got.Reasons, ";"), "frac") {
		t.Fatalf("11 of 10000 pixels off by 17 must be outside with reason frac: %+v", got)
	}
	_, frac = pixelStats(base, shifted(50, 16))
	if frac != 0 {
		t.Fatalf("pixels off by exactly 16 must not count: frac %v", frac)
	}
}

func TestBoxDeltaBoundary(t *testing.T) {
	a := box{X: 10, Y: 20, W: 100, H: 50}
	cases := []struct {
		b    box
		want int
	}{
		{box{X: 10, Y: 20, W: 100, H: 50}, 0},
		{box{X: 11, Y: 20, W: 99, H: 50}, 1},
		{box{X: 10, Y: 20, W: 101, H: 50}, 1},
		{box{X: 10, Y: 19, W: 100, H: 52}, 1},
		{box{X: 12, Y: 20, W: 98, H: 50}, 2},
		{box{X: 10, Y: 20, W: 100, H: 52}, 2},
	}
	for _, c := range cases {
		if got := boxDelta(a, c.b); got != c.want {
			t.Errorf("boxDelta(%+v, %+v) = %d, want %d", a, c.b, got, c.want)
		}
	}
	img := filled(100, 100, white)
	fa, fb := okFacts(img), okFacts(img)
	fb.Box.X++
	fb.Box.W--
	if got := compareFrames(fa, fb); !got.Within || got.BoxDelta != 1 {
		t.Fatalf("a 1 px bbox shift must be within: %+v", got)
	}
	fb = okFacts(img)
	fb.Box.X += 2
	fb.Box.W -= 2
	got := compareFrames(fa, fb)
	if got.Within || got.BoxDelta != 2 || !strings.Contains(strings.Join(got.Reasons, ";"), "bbox") {
		t.Fatalf("a 2 px bbox shift must be outside with reason bbox: %+v", got)
	}
}

func TestDimensionsAndAlphaModeMustMatch(t *testing.T) {
	a := okFacts(filled(100, 100, white))
	b := okFacts(filled(100, 90, white))
	got := compareFrames(a, b)
	if got.Within || !strings.Contains(strings.Join(got.Reasons, ";"), "dims") {
		t.Fatalf("different dimensions must be outside with reason dims: %+v", got)
	}
	b = okFacts(filled(100, 100, white))
	b.Alpha = true
	got = compareFrames(a, b)
	if got.Within || !strings.Contains(strings.Join(got.Reasons, ";"), "alpha") {
		t.Fatalf("a different alpha mode must be outside with reason alpha: %+v", got)
	}
	opaque := partFrame(256, white, 40, 40, 216, 216)
	withAlpha := partFrame(256, white, 40, 40, 216, 216)
	withAlpha.SetNRGBA(128, 128, color.NRGBA{20, 20, 20, 254})
	fa, err := frameOf(encode(t, opaque), 256, "opaque", nil)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := frameOf(encode(t, withAlpha), 256, "opaque", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fa.Alpha || !fb.Alpha {
		t.Fatalf("alpha mode from the file bytes: opaque frame %v, RGBA frame %v, want false true", fa.Alpha, fb.Alpha)
	}
}

func TestVerdictsMustMatch(t *testing.T) {
	part, err := frameOf(encode(t, partFrame(256, white, 40, 40, 216, 216)), 256, "opaque", nil)
	if err != nil {
		t.Fatal(err)
	}
	blank, err := frameOf(encode(t, filled(256, 256, white)), 256, "opaque", nil)
	if err != nil {
		t.Fatal(err)
	}
	if part.Verdict != "ok" {
		t.Fatalf("a frame with a part: verdict %q, want ok", part.Verdict)
	}
	if blank.Verdict != "J1" {
		t.Fatalf("a blank frame: verdict %q, want J1", blank.Verdict)
	}
	if part.Box != (box{X: 40, Y: 40, W: 176, H: 176}) {
		t.Fatalf("part bbox %+v, want 40,40 176x176", part.Box)
	}
	got := compareFrames(part, blank)
	if got.Within || !strings.Contains(strings.Join(got.Reasons, ";"), "verdict") {
		t.Fatalf("a blank frame against a part must be outside with reason verdict: %+v", got)
	}
}

func TestCanonicalJSONSortsKeysOnly(t *testing.T) {
	canon := func(s string) string {
		t.Helper()
		b, err := canonicalJSON(strings.NewReader(s))
		if err != nil {
			t.Fatalf("canonicalJSON(%q): %v", s, err)
		}
		return string(b)
	}
	if a, b := canon(`{"b":1,"a":{"y":[1,2],"x":null}}`), canon(` {"a":{"x":null,"y":[1,2]},"b":1} `); a != b || a == "" {
		t.Fatalf("key order must not matter: %q vs %q", a, b)
	}
	if a, b := canon(`{"v":[1,2,3]}`), canon(`{"v":[3,2,1]}`); a == b {
		t.Fatalf("array order must matter: both %q", a)
	}
	if a, b := canon(`{"v":1}`), canon(`{"v":1.0}`); a == b {
		t.Fatalf("number literals must be kept: both %q", a)
	}
	if a, b := canon(`{"v":0.1}`), canon(`{"v":1e-1}`); a == b {
		t.Fatalf("number literals must be kept: both %q", a)
	}
}

func TestCanonicalJSONRejectsEmptyAndTrailing(t *testing.T) {
	for _, s := range []string{"", "   ", `{"a":1} x`, `{"a":1}{"b":2}`, `{"a":`, `nul`} {
		if b, err := canonicalJSON(strings.NewReader(s)); err == nil {
			t.Errorf("canonicalJSON(%q) = %q, want an error", s, b)
		}
	}
}

func TestCLIExitCodes(t *testing.T) {
	dir := t.TempDir()
	good := encode(t, partFrame(256, white, 40, 40, 216, 216))
	a := writeFile(t, dir, "a.png", good)
	b := writeFile(t, dir, "b.png", good)
	empty := writeFile(t, dir, "empty.png", nil)
	truncated := writeFile(t, dir, "trunc.png", good[:len(good)/2])
	notPNG := writeFile(t, dir, "not.png", []byte("GIF89a not a png at all"))
	ja := writeFile(t, dir, "a.json", []byte(`{"b":1,"a":2}`))
	jb := writeFile(t, dir, "b.json", []byte(`{"a":2,"b":1}`))
	jc := writeFile(t, dir, "c.json", []byte(`{"a":2,"b":1.0}`))
	jEmpty := writeFile(t, dir, "e.json", nil)
	jTrail := writeFile(t, dir, "t.json", []byte(`{"a":2} trailing`))
	missing := filepath.Join(dir, "missing.png")

	cases := []struct {
		name   string
		args   []string
		code   int
		prefix string
	}{
		{"identical pngs", []string{"--size", "256", "--background", "opaque", a, b}, 0, "PNG_COMPARE,"},
		{"zero-byte png", []string{"--size", "256", "--background", "opaque", a, empty}, 2, ""},
		{"truncated png", []string{"--size", "256", "--background", "opaque", truncated, b}, 2, ""},
		{"not a png", []string{"--size", "256", "--background", "opaque", a, notPNG}, 2, ""},
		{"missing png", []string{"--size", "256", "--background", "opaque", a, missing}, 2, ""},
		{"equal json", []string{"--json", ja, jb}, 0, "JSON_COMPARE,equal=1,"},
		{"unequal json", []string{"--json", ja, jc}, 1, "JSON_COMPARE,equal=0,"},
		{"empty json", []string{"--json", ja, jEmpty}, 2, ""},
		{"trailing json", []string{"--json", jTrail, ja}, 2, ""},
		{"one png", []string{"--size", "256", "--background", "opaque", a}, 64, ""},
		{"no size", []string{"--background", "opaque", a, b}, 64, ""},
		{"bad background", []string{"--size", "256", "--background", "green", a, b}, 64, ""},
		{"drawing without svg", []string{"--size", "256", "--background", "opaque", "--drawing", a, b}, 64, ""},
		{"json with three files", []string{"--json", ja, jb, jc}, 64, ""},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		code := run(c.args, &out, &errb)
		if code != c.code {
			t.Errorf("%s: exit %d, want %d (stdout %q stderr %q)", c.name, code, c.code, out.String(), errb.String())
		}
		if c.prefix != "" && !strings.HasPrefix(out.String(), c.prefix) {
			t.Errorf("%s: stdout %q, want prefix %q", c.name, out.String(), c.prefix)
		}
		if c.code == 0 && c.prefix == "PNG_COMPARE," && !strings.Contains(out.String(), ",within=1,") {
			t.Errorf("%s: stdout %q, want within=1", c.name, out.String())
		}
		if c.code == 2 || c.code == 64 {
			if s := out.String(); strings.Contains(s, "within=") || strings.Contains(s, "equal=") {
				t.Errorf("%s: an error run printed a verdict line %q", c.name, s)
			}
		}
	}
}
