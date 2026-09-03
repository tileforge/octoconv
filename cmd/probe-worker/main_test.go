package main

import (
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// TestBuildSanitizeConverter pins the PROBE-01/PROBE-02 plumbing contract: the
// env-resolved PROBE_MAX_PAGES value must reach the converter's own guard
// field.
//
// It exists for the reason cmd/ocr-worker's TestBuildOCRConverter states and
// this engine has already demonstrated: a bound that is configured and not
// plumbed is a bound that does not exist. PROBE_ENGINE_TIMEOUT was read by the
// worker and omitted by cmd/api, and the API published estimated_max_seconds: 0
// for months of the phase before review caught it (PR #12). "Looks correct from
// reading main.go" is exactly the standard that missed it.
func TestBuildSanitizeConverter(t *testing.T) {
	c := buildSanitizeConverter(200)
	if c.MaxPages != 200 {
		t.Fatalf("buildSanitizeConverter(200).MaxPages = %d, want 200", c.MaxPages)
	}
	var _ convert.Converter = c
}

// TestProbeMaxPagesDefaultIsShared pins that this process's default is the
// SHARED constant, not a literal of its own.
//
// cmd/api reads the same env var for its admission gate. Two literals would be
// two defaults, and an operator who sets PROBE_MAX_PAGES on one deployment and
// not the other would get two different ceilings from one service -- the exact
// shape of the PROBE_ENGINE_TIMEOUT drift, one bound over.
func TestProbeMaxPagesDefaultIsShared(t *testing.T) {
	t.Setenv("PROBE_MAX_PAGES", "")
	if got := envInt("PROBE_MAX_PAGES", convert.DefaultProbeMaxPages); got != convert.DefaultProbeMaxPages {
		t.Fatalf("unset PROBE_MAX_PAGES resolved to %d, want convert.DefaultProbeMaxPages (%d)",
			got, convert.DefaultProbeMaxPages)
	}
	t.Setenv("PROBE_MAX_PAGES", "17")
	if got := envInt("PROBE_MAX_PAGES", convert.DefaultProbeMaxPages); got != 17 {
		t.Fatalf("PROBE_MAX_PAGES=17 resolved to %d, want 17 -- an operator's explicit value must "+
			"never be silently replaced by the default", got)
	}
}
