package convert

import "testing"

func TestNormalizeFormatCADAliases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"stp", "step"},
		{".STP", "step"},
		{"igs", "iges"},
		{" .IGS ", "iges"},
		{"step", "step"},
		{"iges", "iges"},
		{"stl", "stl"},
		{"dxf", "dxf"},
		{"dwg", "dwg"},
		{"jpeg", "jpg"},
		{"tif", "tiff"},
		{"htm", "html"},
		{"tex", "latex"},
	}
	for _, c := range cases {
		if got := NormalizeFormat(c.in); got != c.want {
			t.Errorf("NormalizeFormat(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
