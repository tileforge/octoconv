package convert

import (
	"os"
	"regexp"
	"testing"
)

const frontendFormatsPath = "../../frontend/src/formats.ts"

var frontendAliasPattern = regexp.MustCompile(`case '([a-z]+)':\s*return '([a-z]+)';`)

func TestFormatAliasesMatchFrontend(t *testing.T) {
	src, err := os.ReadFile(frontendFormatsPath)
	if err != nil {
		t.Fatalf("read %s: %v", frontendFormatsPath, err)
	}

	ts := map[string]string{}
	for _, m := range frontendAliasPattern.FindAllStringSubmatch(string(src), -1) {
		ts[m[1]] = m[2]
	}
	if len(ts) == 0 {
		t.Fatalf("parsed zero alias cases from %s; the sync test would be vacuous", frontendFormatsPath)
	}

	for from, to := range formatAliases {
		got, ok := ts[from]
		if !ok {
			t.Errorf("alias %q -> %q is in Go formatAliases but missing from %s", from, to, frontendFormatsPath)
			continue
		}
		if got != to {
			t.Errorf("alias %q maps to %q in Go but %q in %s", from, to, got, frontendFormatsPath)
		}
	}
	for from, to := range ts {
		if _, ok := formatAliases[from]; !ok {
			t.Errorf("alias %q -> %q is in %s but missing from Go formatAliases", from, to, frontendFormatsPath)
		}
	}
}

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
