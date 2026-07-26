package main

import (
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// TestBuildOCRConverter pins D-03/OCRE-04's re-registration contract: the
// env-resolved OCR_MAX_PAGES/OCR_MAX_IMAGE_PIXELS values must flow into the
// returned OCRConverter's own guard fields -- not just "look correct" from
// reading main.go, a real assertion on the constructed value (mirrors
// cmd/av-worker's convert.AVConverter{MaxSourceDuration: ...} re-register,
// unit-tested here without depending on the global convert.Default registry
// side effect).
func TestBuildOCRConverter(t *testing.T) {
	c := buildOCRConverter(200, 100000000)
	if c.MaxPages != 200 {
		t.Fatalf("buildOCRConverter(200, ...).MaxPages = %d, want 200", c.MaxPages)
	}
	if c.MaxImagePixels != 100000000 {
		t.Fatalf("buildOCRConverter(..., 100000000).MaxImagePixels = %d, want 100000000", c.MaxImagePixels)
	}
	var _ convert.OCRConverter = c
}

// TestResolveOCRThreadLimit pins the CgroupCPULimit -> runtime.NumCPU
// fallback precedence (D-03: tesseract's OMP_THREAD_LIMIT must honor the
// container's cgroup CPU quota, not the host core count) -- mirrors
// cmd/av-worker's resolveAVThreads exactly, no OCR_THREADS operator
// override (out of scope, same as av-worker's own resolver). The cgroup
// branch's exact value is deliberately NOT asserted (this test's host may
// or may not be a real cgroup v2 container); only the invariant that the
// result is always >= 1 and reports one of the two known sources.
func TestResolveOCRThreadLimit(t *testing.T) {
	n, source := resolveOCRThreadLimit()
	if n < 1 {
		t.Fatalf("resolveOCRThreadLimit() = (%d, %q), want n >= 1", n, source)
	}
	if source != "cgroup" && source != "NumCPU fallback" {
		t.Fatalf("resolveOCRThreadLimit() source = %q, want \"cgroup\" or \"NumCPU fallback\"", source)
	}
}
