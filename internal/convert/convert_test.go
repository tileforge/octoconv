package convert

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestNormalizeFormat(t *testing.T) {
	cases := map[string]string{
		"PNG":   "png",
		".jpg":  "jpg",
		"JPEG":  "jpg",
		" tif ": "tiff",
		"webp":  "webp",
		"htm":   "html",
		".HTM":  "html",
		// `.tex` is the conventional LaTeX extension; without this alias a
		// real paper.tex matched no registered pair and 422'd.
		"tex":   "latex",
		".TeX":  "latex",
		"latex": "latex",
	}
	for in, want := range cases {
		if got := NormalizeFormat(in); got != want {
			t.Errorf("NormalizeFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistryLibvipsPairs(t *testing.T) {
	// Default registry is populated by converters.go init().
	if !Default.Supports("png", "webp") {
		t.Error("expected png->webp to be supported")
	}
	// Alias + case normalization should resolve to the same pair.
	if !Default.Supports("JPEG", ".PNG") {
		t.Error("expected jpeg->png (aliased/cased) to be supported")
	}
	if Default.Supports("png", "png") {
		t.Error("identity pair png->png must not be registered")
	}
	if Default.Supports("png", "mp3") {
		t.Error("unsupported pair png->mp3 should not be supported")
	}
	if _, ok := Default.Lookup("heic", "jpg"); !ok {
		t.Error("expected a converter for heic->jpg")
	}
}

func TestConverterEngine(t *testing.T) {
	if got := (LibvipsConverter{}).Engine(); got != "image" {
		t.Errorf("LibvipsConverter{}.Engine() = %q, want image", got)
	}
	if got := (LibreOfficeConverter{}).Engine(); got != "document" {
		t.Errorf("LibreOfficeConverter{}.Engine() = %q, want document", got)
	}
}

func TestRegistryEngineFor(t *testing.T) {
	// Default registry is populated by converters.go init().
	if engine, ok := Default.EngineFor("png", "webp"); !ok || engine != "image" {
		t.Errorf("EngineFor(png, webp) = (%q, %v), want (image, true)", engine, ok)
	}
	if engine, ok := Default.EngineFor("docx", "pdf"); !ok || engine != "document" {
		t.Errorf("EngineFor(docx, pdf) = (%q, %v), want (document, true)", engine, ok)
	}
	// Alias normalization via Lookup.
	if engine, ok := Default.EngineFor("jpeg", "webp"); !ok || engine != "image" {
		t.Errorf("EngineFor(jpeg, webp) = (%q, %v), want (image, true)", engine, ok)
	}
	// Unsupported pair: zero-value string, false.
	if engine, ok := Default.EngineFor("png", "mp3"); ok || engine != "" {
		t.Errorf("EngineFor(png, mp3) = (%q, %v), want (\"\", false)", engine, ok)
	}
	// All 6 document source formats -> ("document", true).
	for _, from := range []string{"docx", "xlsx", "pptx", "odt", "ods", "odp"} {
		if engine, ok := Default.EngineFor(from, "pdf"); !ok || engine != "document" {
			t.Errorf("EngineFor(%s, pdf) = (%q, %v), want (document, true)", from, engine, ok)
		}
	}
}

// TestRegistryEngineForOCRRouting pins OCRP-01/D-01 (44-CONTEXT.md): once
// OCRConverter{} is registered (converters.go init()), EngineFor disambiguates
// purely by TARGET with zero new routing code -- jpg->txt and pdf->txt route
// to "ocr" while jpg->png (the pre-existing image pair) still routes to
// "image". This is the single acceptance test proving the target-based
// disambiguation the whole phase's routing design rests on.
func TestRegistryEngineForOCRRouting(t *testing.T) {
	if engine, ok := Default.EngineFor("jpg", "txt"); !ok || engine != EngineOCR {
		t.Errorf("EngineFor(jpg, txt) = (%q, %v), want (%q, true)", engine, ok, EngineOCR)
	}
	if engine, ok := Default.EngineFor("pdf", "txt"); !ok || engine != EngineOCR {
		t.Errorf("EngineFor(pdf, txt) = (%q, %v), want (%q, true)", engine, ok, EngineOCR)
	}
	if engine, ok := Default.EngineFor("jpg", "png"); !ok || engine != EngineImage {
		t.Errorf("EngineFor(jpg, png) = (%q, %v), want (%q, true) -- image engine must still win its own target", engine, ok, EngineImage)
	}
	// hocr/alto targets also route to ocr for every advertised OCR source.
	for _, from := range []string{"jpg", "png", "tiff", "webp", "pdf"} {
		for _, to := range []string{"txt", "hocr", "alto"} {
			if engine, ok := Default.EngineFor(from, to); !ok || engine != EngineOCR {
				t.Errorf("EngineFor(%s, %s) = (%q, %v), want (%q, true)", from, to, engine, ok, EngineOCR)
			}
		}
	}
}

// TestRegistryEngineForPandocRouting pins PDCP-01/D-02 (48-CONTEXT.md): once
// PandocConverter{} is registered (converters.go init()), EngineFor
// disambiguates purely by TARGET with zero new routing code -- md->html,
// docx->md, and html->md all route to "pandoc" while docx->pdf keeps routing
// to "document" (LibreOfficeConverter) and html->pdf keeps routing to "html"
// (ChromiumConverter). This is the acceptance test proving the target-based
// disambiguation the whole plan's registration-only wiring rests on.
func TestRegistryEngineForPandocRouting(t *testing.T) {
	if engine, ok := Default.EngineFor("md", "html"); !ok || engine != EnginePandoc {
		t.Errorf("EngineFor(md, html) = (%q, %v), want (%q, true)", engine, ok, EnginePandoc)
	}
	if engine, ok := Default.EngineFor("docx", "md"); !ok || engine != EnginePandoc {
		t.Errorf("EngineFor(docx, md) = (%q, %v), want (%q, true)", engine, ok, EnginePandoc)
	}
	if engine, ok := Default.EngineFor("html", "md"); !ok || engine != EnginePandoc {
		t.Errorf("EngineFor(html, md) = (%q, %v), want (%q, true)", engine, ok, EnginePandoc)
	}
	if engine, ok := Default.EngineFor("docx", "pdf"); !ok || engine != EngineDocument {
		t.Errorf("EngineFor(docx, pdf) = (%q, %v), want (%q, true) -- document engine must still win its own target", engine, ok, EngineDocument)
	}
	if engine, ok := Default.EngineFor("html", "pdf"); !ok || engine != EngineHTML {
		t.Errorf("EngineFor(html, pdf) = (%q, %v), want (%q, true) -- html/chromium engine must still win its own target", engine, ok, EngineHTML)
	}
}

// TestRegistryClasses verifies D-06: Classes() groups every registered pair
// under its engine class, a known libvips pair (png->webp) surfaces under
// "image", and repeated calls return a stable (deterministically sorted)
// result.
func TestRegistryClasses(t *testing.T) {
	classes := Default.Classes()

	imagePairs, ok := classes["image"]
	if !ok {
		t.Fatal(`Classes() missing "image" class`)
	}
	found := false
	for _, p := range imagePairs {
		if p.From == "png" && p.To == "webp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(`Classes()["image"] = %+v, want it to contain {png webp}`, imagePairs)
	}

	// Stability: a second call must return the identical grouping/order.
	again := Default.Classes()
	againPairs, ok := again["image"]
	if !ok || len(againPairs) != len(imagePairs) {
		t.Fatalf("Classes() not stable across calls: first=%+v second=%+v", imagePairs, againPairs)
	}
	for i := range imagePairs {
		if imagePairs[i] != againPairs[i] {
			t.Errorf("Classes() order not stable at index %d: first=%+v second=%+v", i, imagePairs[i], againPairs[i])
		}
	}
}

// TestAllEnginesCoversEveryEngineClass is the completeness guard for
// AllEngines/IsKnownEngine (52-07, mirroring
// queue.TestAllConvertQueuesCoversEveryEngine): it asserts the eight
// Engine* constants each appear exactly once, that there are no extras,
// that IsKnownEngine agrees with membership, and -- the load-bearing
// assertion -- that every key of Default.Classes() (the registry-derived
// set of engine classes actually wired up via converters.go's init()) is
// present in AllEngines(). Without that last check, a future engine class
// could register a converter and start routing real jobs while staying
// permanently un-validatable at the manage-clients CLI, silently
// reintroducing the class of bug this plan closes.
func TestAllEnginesCoversEveryEngineClass(t *testing.T) {
	want := map[string]bool{
		EngineImage:    true,
		EngineDocument: true,
		EngineHTML:     true,
		EngineAudio:    true,
		EngineAV:       true,
		EngineOCR:      true,
		EnginePandoc:   true,
		EngineProbe:    true,
		EngineTectonic: true,
		EngineCAD:      true,
	}

	got := AllEngines()
	// Spelled out rather than derived from want: deriving it would make the
	// assertion vacuous, and the point is that adding an engine class is a
	// decision taken in two places at once. Ten as of Phase 73 (cad).
	if len(got) != 10 {
		t.Fatalf("len(AllEngines()) = %d, want exactly 10", len(got))
	}

	seen := make(map[string]int, len(got))
	for _, e := range got {
		seen[e]++
	}
	for e := range want {
		if seen[e] != 1 {
			t.Errorf("AllEngines() contains engine %q %d times, want exactly 1", e, seen[e])
		}
	}
	for e, count := range seen {
		if !want[e] {
			t.Errorf("AllEngines() contains unexpected extra entry %q (count %d)", e, count)
		}
	}

	for _, e := range got {
		if !IsKnownEngine(e) {
			t.Errorf("IsKnownEngine(%q) = false, want true (member of AllEngines())", e)
		}
	}
	for _, bad := range []string{"images", "Image", "", "*", "webhook"} {
		// "webhook" specifically: queue.QueueWebhook is a real queue name but
		// not an engine class, so it must not be accepted here.
		if IsKnownEngine(bad) {
			t.Errorf("IsKnownEngine(%q) = true, want false", bad)
		}
	}

	// Registry-derived cross-check: every engine class the registry actually
	// routes conversions to (Default.Classes(), populated by converters.go's
	// init()) must be present in AllEngines(), or a real engine would be
	// silently un-validatable at the CLI.
	for class := range Default.Classes() {
		if !want[class] {
			t.Errorf("Default.Classes() has engine class %q which is missing from AllEngines()", class)
		}
	}

	// The same cross-check over EVERY operation, not just convert.
	//
	// Classes() above walks OperationConvert only, so it is structurally
	// blind to EngineProbe -- which serves no convert pair at all. Phase 57's
	// Classes() doc comment flagged that as "a known, intentional consequence
	// Phase 60 will revisit"; this is the revisit. Without it, the eighth
	// class would be the first entry in AllEngines() with no registry-derived
	// check of any kind behind it.
	routed := make(map[string]bool)
	for _, byClass := range Default.OperationClasses() {
		for class := range byClass {
			routed[class] = true
			if !want[class] {
				t.Errorf("OperationClasses() routes engine class %q which is missing from AllEngines()", class)
			}
		}
	}

	// And the other direction, for probe specifically: an engine class listed
	// in AllEngines() that nothing routes is a limit an operator can set
	// against a queue that will never receive a job.
	if !routed[EngineProbe] {
		t.Errorf("AllEngines() lists %q but no registered converter routes it -- "+
			"an operator can set a quota against a queue nothing reaches", EngineProbe)
	}
}

// TestRunCommandTimeout verifies the hardened exec kills a long-running child
// when the context deadline fires, and returns the context error.
func TestRunCommandTimeout(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runCommand(ctx, "sleep", "10")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from timed-out command, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("command not killed promptly: took %v", elapsed)
	}
}

// TestRunCommandFailure surfaces a non-zero exit as an error.
func TestRunCommandFailure(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("false not available")
	}
	if _, err := runCommand(context.Background(), "false"); err == nil {
		t.Fatal("expected error from `false`, got nil")
	}
}

func TestDefaultRegistersOnlyAllowlistedCADPairs(t *testing.T) {
	cad := map[string]bool{
		"step": true, "stp": true, "iges": true, "igs": true,
		"stl": true, "dxf": true, "dwg": true,
	}
	allowed := map[string]bool{
		OperationConvert + " step>stl": true,
		OperationConvert + " iges>stl": true,
		OperationRender + " step>png":  true,
		OperationRender + " iges>png":  true,
		OperationRender + " stl>png":   true,
	}

	walked := 0
	found := map[string]bool{}
	check := func(where, op, class string, pairs []Pair) {
		for _, p := range pairs {
			walked++
			touches := cad[p.From] || cad[p.To] || class == EngineCAD
			if !touches {
				continue
			}
			key := op + " " + p.From + ">" + p.To
			if class != EngineCAD || !allowed[key] {
				t.Errorf("%s registers CAD pair %s -> %s under operation %q class %q, want only (convert, step, stl), (convert, iges, stl), (render, step, png), (render, iges, png) and (render, stl, png) under cad", where, p.From, p.To, op, class)
				continue
			}
			found[key] = true
		}
	}

	for class, pairs := range Default.Classes() {
		check("Classes", OperationConvert, class, pairs)
	}
	for op, classes := range Default.OperationClasses() {
		for class, pairs := range classes {
			check("OperationClasses["+op+"]", op, class, pairs)
		}
	}

	if walked == 0 {
		t.Fatal("walked zero registered pairs; the guard would be vacuous")
	}
	for key := range allowed {
		if !found[key] {
			t.Errorf("allowlisted CAD pair %s is not registered", key)
		}
	}
}
