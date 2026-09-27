package convert

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// readSourceFile reads a .go source file in this package by name, relative
// to the package directory (the test's working directory) -- used only to
// grep the assembled argv literal without launching a live chromium binary.
func readSourceFile(name string) (string, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestChromiumConverterPairs(t *testing.T) {
	got := ChromiumConverter{}.Pairs()
	want := []Pair{{From: "html", To: "pdf"}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Pairs() = %+v, want %+v", got, want)
	}
}

func TestChromiumConverterEngine(t *testing.T) {
	if got := (ChromiumConverter{}).Engine(); got != EngineHTML {
		t.Errorf("Engine() = %q, want %q", got, EngineHTML)
	}
}

func TestChromiumRegisteredInDefault(t *testing.T) {
	engine, ok := Default.EngineFor("html", "pdf")
	if !ok {
		t.Fatal("EngineFor(html, pdf) ok=false, want true after ChromiumConverter registration")
	}
	if engine != EngineHTML {
		t.Errorf("EngineFor(html, pdf) engine = %q, want %q", engine, EngineHTML)
	}
}

func TestInjectPrintCSSWithHeadClose(t *testing.T) {
	original := []byte("<!doctype html>\n<html>\n<head>\n<title>x</title>\n</head>\n<body>hi</body>\n</html>")
	originalCopy := append([]byte(nil), original...)
	css := "<style>@page { size: A4 !important; }</style>"

	got := injectPrintCSS(original, css)

	if !strings.Contains(string(got), css) {
		t.Fatalf("injectPrintCSS output does not contain the CSS block: %s", got)
	}
	headCloseIdx := strings.Index(string(got), "</head>")
	cssIdx := strings.Index(string(got), css)
	if cssIdx < 0 || headCloseIdx < 0 || cssIdx > headCloseIdx {
		t.Errorf("CSS block not placed immediately before </head>: css at %d, </head> at %d", cssIdx, headCloseIdx)
	}
	// inPath content must never be mutated.
	if string(original) != string(originalCopy) {
		t.Error("injectPrintCSS mutated its input html slice in place")
	}
}

// cspShapeRow is one row of this plan's Shape table (69-04-PLAN.md): a
// crafted document plus the byte offset injectCSPFirst must splice the CSP
// at. Task 1 builds only row s1 (the canonical CR-01 bypass); Task 2 extends
// this to all 18 top-level rows plus the six unit-only edge rows.
type cspShapeRow struct {
	name        string
	raw         []byte
	wantAt      int
	subDocument bool
}

// cspShapeTable builds the shared shape table (one table, two levels: the
// unit invariant below and the live differential in
// chromium_live_test.go) -- all 18 top-level rows of 69-04-PLAN.md's Shape
// table section (s1-s9, x1-x5, f1-f4). marker must be a fresh
// preflightMarker() value per invocation so a live run's oracle can never be
// confused with a stale one.
func cspShapeTable(marker string) []cspShapeRow {
	const d = "<!doctype html>"
	title := "<title>SAFE-" + marker + "</title>"
	script := "<script>document.title='BYPASS-" + marker + "'</script>"
	// frameScript (F) acts only inside a frame: its effect is an uncompressed
	// /URI annotation on the produced PDF, never document.title, since a
	// sub-document's own title is not the top-level document's.
	frameScript := "<script>if(window!==top){var a=document.createElement('a');" +
		"a.href='https://bypass.invalid/BYPASS-" + marker + "';a.textContent='BYPASS';" +
		"(document.body||document.documentElement).prepend(a)}</script>"
	tail := "<head>" + title + "</head><body><p>x</p></body></html>"

	x4Doctype := `<!DOCTYPE html SYSTEM "about:legacy-compat>">`
	x4At := strings.IndexByte(x4Doctype, '>') + 1 // the doctype ends at its FIRST '>', even inside a quoted identifier

	return []cspShapeRow{
		{name: "s1", raw: []byte(d + script + "<html>" + tail), wantAt: len(d)},
		{name: "s2", raw: []byte("<html>" + script + tail), wantAt: 0},
		{
			name:   "s3", // control: script legitimately inside literal <head>
			raw:    []byte(d + "<html><head>" + script + title + "</head><body><p>x</p></body></html>"),
			wantAt: len(d),
		},
		{name: "s4", raw: []byte(d + "<header>decoy</header>" + script + "<html>" + tail), wantAt: len(d)},
		{name: "s5", raw: []byte(d + script + "<header>decoy</header><html>" + tail), wantAt: len(d)},
		{name: "s6", raw: []byte(d + script + "<!-- <head> --><html>" + tail), wantAt: len(d)},
		{name: "s7", raw: []byte(d + "<!-- <head> -->" + script + "<html>" + tail), wantAt: len(d)},
		{
			name: "s8", // uppercase
			raw: []byte("<!DOCTYPE HTML><SCRIPT>document.title='BYPASS-" + marker +
				"'</SCRIPT><HTML><HEAD><TITLE>SAFE-" + marker + "</TITLE></HEAD><BODY><P>x</P></BODY></HTML>"),
			wantAt: len("<!DOCTYPE HTML>"),
		},
		{
			name:   "s9", // BOM, then s1
			raw:    append([]byte{0xEF, 0xBB, 0xBF}, []byte(d+script+"<html>"+tail)...),
			wantAt: 3 + len(d),
		},
		{
			name:   "x1", // HTML whitespace before the doctype
			raw:    []byte("\n\t " + d + script + "<html>" + tail),
			wantAt: 3 + len(d),
		},
		{
			name:   "x2", // vertical tab before the doctype -- NOT HTML whitespace
			raw:    []byte("\v" + d + script + "<html>" + tail),
			wantAt: 0,
		},
		{
			name:   "x3", // BOM, no doctype
			raw:    append([]byte{0xEF, 0xBB, 0xBF}, []byte("<html>"+script+tail)...),
			wantAt: 3,
		},
		{
			name:   "x4", // '>' inside a quoted doctype identifier
			raw:    []byte(x4Doctype + script + "<html>" + tail),
			wantAt: x4At,
		},
		{
			name:   "x5", // control: script in body
			raw:    []byte(d + "<html><head>" + title + "</head><body>" + script + "<p>x</p></body></html>"),
			wantAt: len(d),
		},
		{
			name:        "f1", // iframe of the raw upload
			raw:         []byte(d + "<html><head>" + title + `</head><body><p>b</p><iframe src="in.html" width="600" height="300"></iframe>` + frameScript + "</body></html>"),
			wantAt:      len(d),
			subDocument: true,
		},
		{
			name:        "f2", // frameset of the raw upload
			raw:         []byte(d + "<html><head>" + title + frameScript + `</head><frameset><frame src="in.html"></frameset></html>`),
			wantAt:      len(d),
			subDocument: true,
		},
		{
			name:        "f3", // object of the raw upload
			raw:         []byte(d + "<html><head>" + title + `</head><body><p>b</p><object data="in.html" type="text/html" width="600" height="300"></object>` + frameScript + "</body></html>"),
			wantAt:      len(d),
			subDocument: true,
		},
		{
			name:        "f4", // embed of the raw upload
			raw:         []byte(d + "<html><head>" + title + `</head><body><p>b</p><embed src="in.html" type="text/html" width="600" height="300">` + frameScript + "</body></html>"),
			wantAt:      len(d),
			subDocument: true,
		},
	}
}

// cspEdgeRows are the six unit-only edge rows of 69-04-PLAN.md's Shape table
// section (e1-e6): none of them reach ChromiumConverter.Convert through
// LooksLikeHTML (kept for defence in depth only), so they are exercised by
// TestInjectCSPFirstLeadsEveryShape alone, never by the live differential.
func cspEdgeRows(marker string) []cspShapeRow {
	const d = "<!doctype html>"
	title := "<title>SAFE-" + marker + "</title>"
	script := "<script>document.title='BYPASS-" + marker + "'</script>"
	tail := "<head>" + title + "</head><body><p>x</p></body></html>"

	return []cspShapeRow{
		{name: "e1", raw: []byte{}, wantAt: 0},
		{name: "e2", raw: []byte("<!doctype html"), wantAt: 0}, // no '>' at all
		{name: "e3", raw: []byte(script + d + "<html>" + tail), wantAt: 0},     // script first
		{name: "e4", raw: []byte("<!-- c -->" + d + "<html>" + tail), wantAt: 0}, // comment first
		{
			name:   "e5",
			raw:    append(append([]byte{0xEF, 0xBB, 0xBF}, []byte("\f\r\n")...), []byte(d+script+"<html>"+tail)...),
			wantAt: 3 + 3 + len(d),
		},
		{name: "e6", raw: []byte("<html><body><script>x()</script>hi</body></html>"), wantAt: 0}, // the removed no-head test's input
	}
}

// validateCSPPrefixInvariant asserts that raw[:wantAt] is at most a leading
// UTF-8 BOM, then only HTML whitespace (TAB, LF, FF, CR, SPACE), then at most
// one case-insensitive "<!doctype" run ending at its first '>' -- the
// structural invariant injectCSPFirst's document-start splice must hold for
// every shape-table row (69-04-PLAN.md Task 1 <behavior>).
func validateCSPPrefixInvariant(t *testing.T, name string, raw []byte, wantAt int) {
	t.Helper()
	p := raw[:wantAt]
	if bytes.HasPrefix(p, []byte{0xEF, 0xBB, 0xBF}) {
		p = p[3:]
	}
	for len(p) > 0 {
		switch p[0] {
		case '\t', '\n', '\f', '\r', ' ':
			p = p[1:]
			continue
		}
		break
	}
	if len(p) == 0 {
		return
	}
	lower := bytes.ToLower(p)
	if !bytes.HasPrefix(lower, []byte("<!doctype")) {
		t.Fatalf("%s: prefix %q is neither whitespace nor a doctype opener", name, p)
	}
	gt := bytes.IndexByte(p, '>')
	if gt < 0 || gt != len(p)-1 {
		t.Fatalf("%s: prefix %q does not end exactly at the doctype's first '>'", name, p)
	}
}

// TestInjectCSPFirstLeadsEveryShape is the shared shape-table invariant, over
// all 18 top-level rows plus the six unit-only edge rows (24 total). For
// every row, injectCSPFirst's output must equal raw[:wantAt] + csp +
// raw[wantAt:], the prefix must satisfy validateCSPPrefixInvariant, the input
// slice must be left unmodified, and a leading BOM must survive into the
// output.
func TestInjectCSPFirstLeadsEveryShape(t *testing.T) {
	marker, err := preflightMarker()
	if err != nil {
		t.Fatalf("preflightMarker: %v", err)
	}
	rows := append(append([]cspShapeRow{}, cspShapeTable(marker)...), cspEdgeRows(marker)...)
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			original := append([]byte(nil), row.raw...)
			validateCSPPrefixInvariant(t, row.name, row.raw, row.wantAt)

			got := injectCSPFirst(row.raw, cspNoScriptMeta)

			want := append(append(append([]byte{}, row.raw[:row.wantAt]...), cspNoScriptMeta...), row.raw[row.wantAt:]...)
			if !bytes.Equal(got, want) {
				t.Errorf("%s: injectCSPFirst output mismatch\n got: %s\nwant: %s", row.name, got, want)
			}
			if !bytes.Equal(row.raw, original) {
				t.Errorf("%s: injectCSPFirst mutated its input slice in place", row.name)
			}
			if bytes.HasPrefix(original, []byte{0xEF, 0xBB, 0xBF}) && !bytes.HasPrefix(got, []byte{0xEF, 0xBB, 0xBF}) {
				t.Errorf("%s: leading BOM did not survive into the output", row.name)
			}
		})
	}
}

// TestInjectCSPFirstKeepsPrintStyleAtHeadClose composes injectCSPFirst then
// injectPrintCSS over rows s1 and s3: the CSP must stay at its wantAt offset
// (injectPrintCSS must not move it), and the print style must still land
// immediately before the first case-insensitive </head> -- injectPrintCSS
// itself is byte-identical to before this plan (D-12, Task 3's gate).
func TestInjectCSPFirstKeepsPrintStyleAtHeadClose(t *testing.T) {
	marker, err := preflightMarker()
	if err != nil {
		t.Fatalf("preflightMarker: %v", err)
	}
	css := "<style>marker</style>"
	byName := map[string]cspShapeRow{}
	for _, r := range cspShapeTable(marker) {
		byName[r.name] = r
	}
	for _, name := range []string{"s1", "s3"} {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("shape table missing row %s", name)
		}
		t.Run(name, func(t *testing.T) {
			withCSP := injectCSPFirst(row.raw, cspNoScriptMeta)
			got := injectPrintCSS(withCSP, css)

			cspIdx := bytes.Index(got, []byte(cspNoScriptMeta))
			if cspIdx != row.wantAt {
				t.Errorf("%s: CSP moved after composing with injectPrintCSS: at %d, want %d", name, cspIdx, row.wantAt)
			}
			lower := bytes.ToLower(got)
			headCloseIdx := bytes.Index(lower, headCloseTag)
			cssIdx := bytes.Index(got, []byte(css))
			if cssIdx < 0 || headCloseIdx < 0 || cssIdx > headCloseIdx {
				t.Errorf("%s: print style not placed immediately before </head>: css at %d, </head> at %d", name, cssIdx, headCloseIdx)
			}
		})
	}
}

func TestInjectPrintCSSCaseInsensitiveHeadClose(t *testing.T) {
	original := []byte("<!DOCTYPE HTML><HTML><HEAD><TITLE>x</TITLE></HEAD><BODY>hi</BODY></HTML>")
	css := "<style>marker</style>"
	got := injectPrintCSS(original, css)
	if !strings.Contains(string(got), css) {
		t.Fatalf("injectPrintCSS output does not contain the CSS block: %s", got)
	}
	// The css must land before the (case-insensitive) closing head tag.
	lower := strings.ToLower(string(got))
	headCloseIdx := strings.Index(lower, "</head>")
	cssIdx := strings.Index(string(got), css)
	if cssIdx < 0 || headCloseIdx < 0 || cssIdx > headCloseIdx {
		t.Errorf("CSS block not placed before case-insensitive </head>: css at %d, </head> at %d", cssIdx, headCloseIdx)
	}
}

func TestInjectPrintCSSNoHeadCloseFallsBackToAfterHTMLOpen(t *testing.T) {
	original := []byte("<!doctype html>\n<html lang=\"en\">\n<body>hi</body>\n</html>")
	originalCopy := append([]byte(nil), original...)
	css := "<style>marker</style>"

	got := injectPrintCSS(original, css)

	if !strings.Contains(string(got), css) {
		t.Fatalf("injectPrintCSS output does not contain the CSS block: %s", got)
	}
	htmlOpenEnd := strings.Index(string(got), "<html lang=\"en\">") + len("<html lang=\"en\">")
	cssIdx := strings.Index(string(got), css)
	if cssIdx != htmlOpenEnd {
		t.Errorf("CSS block not placed immediately after opening <html> tag: css at %d, want %d", cssIdx, htmlOpenEnd)
	}
	if string(original) != string(originalCopy) {
		t.Error("injectPrintCSS mutated its input html slice in place")
	}
}

func TestInjectPrintCSSNoMarkerAtAllPrependsAtStart(t *testing.T) {
	original := []byte("not really html content")
	css := "<style>marker</style>"
	got := injectPrintCSS(original, css)
	if !strings.HasPrefix(string(got), css) {
		t.Errorf("injectPrintCSS with no marker should prepend css, got: %s", got)
	}
}

func TestChromiumArgvContainsRequiredFlags(t *testing.T) {
	// This mirrors the exact argv Convert assembles (RESEARCH.md Code
	// Examples), asserted directly against the source so no live chromium
	// invocation is required for this check.
	data, err := readSourceFile("chromium.go")
	if err != nil {
		t.Fatalf("read chromium.go: %v", err)
	}
	required := []string{
		`"--proxy-server=127.0.0.1:9"`,
		`"--proxy-bypass-list=<-loopback>"`,
		`"--host-resolver-rules=MAP * ~NOTFOUND"`,
		`"--no-sandbox"`,
		`"--disable-dev-shm-usage"`,
		`"--no-pdf-header-footer"`,
		// IN-02: chromium scratch/profile state pinned inside the per-job
		// workDir so it lands within the caller's os.RemoveAll cleanup boundary.
		`"--user-data-dir=" + profileDir`,
		// Phase 69: the URL is now built at the Convert call site and passed
		// into chromiumPrintArgs as a plain function argument, where gofmt
		// drops the spaces around the binary + it kept inside the slice
		// literal above (verified: `gofmt` collapses `"a" + b` to `"a"+b`
		// when it is a call argument rather than a composite-literal
		// element) -- the expression is unchanged, only its formatting
		// context is.
		`"file://"+renderedPath`,
	}
	for _, want := range required {
		if !strings.Contains(data, want) {
			t.Errorf("chromium.go argv missing required flag/expression: %s", want)
		}
	}
	// --blink-settings=scriptEnabled=false must NOT be in argv (Plan 04 live
	// finding: it makes the one-shot command handler silently produce no
	// output at all in chromium-headless-shell 150.0.7871.100). JS-disable
	// moved to the injected CSP meta tag (cspNoScriptMeta) instead.
	if strings.Contains(data, `"--blink-settings=scriptEnabled=false"`) {
		t.Error("chromium.go argv still contains --blink-settings=scriptEnabled=false, live-tested to break --print-to-pdf entirely (Plan 04)")
	}
	// --disable-javascript must NOT be relied on either -- live-tested no-op
	// (does not actually disable script execution in this build).
	if strings.Contains(data, `"--disable-javascript"`) {
		t.Error("chromium.go argv contains --disable-javascript, live-tested as a no-op that does not disable JS (Plan 04)")
	}
}

func TestChromiumInjectsCSPNoScriptMeta(t *testing.T) {
	data, err := readSourceFile("chromium.go")
	if err != nil {
		t.Fatalf("read chromium.go: %v", err)
	}
	if !strings.Contains(data, "cspNoScriptMeta") {
		t.Error("chromium.go no longer references cspNoScriptMeta -- D-05's JS-disable mechanism must be injected via the same CSS injection point")
	}
	for _, want := range []string{`script-src 'none'`, `object-src 'none'`, `frame-src 'none'`, `child-src 'none'`} {
		if !strings.Contains(cspNoScriptMeta, want) {
			t.Errorf("cspNoScriptMeta = %q, want it to contain %q", cspNoScriptMeta, want)
		}
	}
}

func TestChromiumDoesNotReimplementValidatePDF(t *testing.T) {
	data, err := readSourceFile("chromium.go")
	if err != nil {
		t.Fatalf("read chromium.go: %v", err)
	}
	if strings.Contains(data, "func validatePDF") {
		t.Error("chromium.go re-implements validatePDF; it must reuse libreoffice.go's implementation verbatim")
	}
	if !strings.Contains(data, "validatePDF(outPath)") {
		t.Error("chromium.go does not call validatePDF(outPath)")
	}
}
