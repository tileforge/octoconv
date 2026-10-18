package main

import (
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

// TestBuildTectonicConverter asserts the bundle path resolved from the
// environment actually reaches the converter's own field -- mirroring
// cmd/ocr-worker's TestBuildOCRConverter and 63-02's
// TestBuildSanitizeConverter, and for the reason both of those record: a bound
// that is configured and not plumbed is a bound that does not exist, and this
// fleet has shipped that defect once already.
//
// It matters more here than in either of those. TectonicConverter refuses to
// run with an empty BundlePath precisely because tectonic's fallback is a
// NETWORK FETCH (TEX-02), so a path that was read from the environment, logged,
// and then dropped on the floor would leave the registry holding the zero-value
// converter converters.go installed at init -- and the failure would be a
// stream of refusals at job time, not a startup error.
func TestBuildTectonicConverter(t *testing.T) {
	const path = "/opt/tectonic/some-other-bundle.ttb"

	c := buildTectonicConverter(path, convert.DefaultTectonicMaxOutputBytes, nil, 0, true)

	if c.BundlePath != path {
		t.Fatalf("buildTectonicConverter(%q, nil, 0, true).BundlePath = %q, want %q", path, c.BundlePath, path)
	}
	var _ convert.Converter = c
}

// TestBuildTectonicConverterDoesNotSubstituteADefault pins the negative half.
// main() decides whether to fall back to convert.DefaultTectonicBundlePath and
// then STATS the result, failing at startup if it is not readable; if the
// builder quietly substituted a default of its own, that startup check would be
// checking a different path than the converter ends up using -- the two would
// agree today and diverge the first time the default moved.
func TestBuildTectonicConverterDoesNotSubstituteADefault(t *testing.T) {
	if got := buildTectonicConverter("", convert.DefaultTectonicMaxOutputBytes, nil, 0, true).BundlePath; got != "" {
		t.Fatalf("buildTectonicConverter(\"\", nil, 0, true).BundlePath = %q, want \"\" -- the fallback belongs to main(), "+
			"which stats the path it chose; a second one here would be unverified", got)
	}
}

// TestBuildTectonicConverterCarriesTheOutputCeiling is the same "configured and
// not plumbed" check for the bound round 2 of PR #16's review found missing.
// The input ceiling (2 MiB) does not bound the OUTPUT -- 198 bytes of source
// measured 6 040 163 bytes of PDF -- so this is the only thing standing between
// a 199-byte upload and the 19 545 890-byte PDF it measured in 93 seconds.
func TestBuildTectonicConverterCarriesTheOutputCeiling(t *testing.T) {
	const ceiling = 7 << 20

	c := buildTectonicConverter("/opt/tectonic/octoconv-bundle.ttb", ceiling, nil, 0, true)
	if c.MaxOutputBytes != ceiling {
		t.Fatalf("buildTectonicConverter(..., %d, nil, 0, true).MaxOutputBytes = %d -- the env value never reaches "+
			"the guard, so the ceiling is whatever the converter defaults to", ceiling, c.MaxOutputBytes)
	}
}

// TestEnvInt64RefusesNonsense keeps the resolver from turning a typo into "no
// ceiling". Zero and negative values are the dangerous ones: they would sail
// past a naive > 0 comparison in the converter and disable the bound.
func TestEnvInt64RefusesNonsense(t *testing.T) {
	for _, tc := range []struct{ set, why string }{
		{"", "unset"},
		{"0", "zero would disable the bound"},
		{"-1", "negative would disable the bound"},
		{"lots", "unparseable"},
	} {
		t.Setenv("TECTONIC_MAX_OUTPUT_BYTES", tc.set)
		if got := envInt64("TECTONIC_MAX_OUTPUT_BYTES", convert.DefaultTectonicMaxOutputBytes); got != convert.DefaultTectonicMaxOutputBytes {
			t.Errorf("envInt64 with %q = %d, want the default (%s)", tc.set, got, tc.why)
		}
	}
	t.Setenv("TECTONIC_MAX_OUTPUT_BYTES", "1048576 # a comment, as .env.example writes them")
	if got := envInt64("TECTONIC_MAX_OUTPUT_BYTES", convert.DefaultTectonicMaxOutputBytes); got != 1048576 {
		t.Errorf("envInt64 with a trailing comment = %d, want 1048576", got)
	}
}
