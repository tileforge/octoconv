package main

import (
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
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

	c := buildTectonicConverter(path)

	if c.BundlePath != path {
		t.Fatalf("buildTectonicConverter(%q).BundlePath = %q, want %q", path, c.BundlePath, path)
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
	if got := buildTectonicConverter("").BundlePath; got != "" {
		t.Fatalf("buildTectonicConverter(\"\").BundlePath = %q, want \"\" -- the fallback belongs to main(), "+
			"which stats the path it chose; a second one here would be unverified", got)
	}
}
