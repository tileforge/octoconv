package convert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ChromiumConverter renders HTML to PDF via chromium-headless-shell's
// one-shot --print-to-pdf mode (D-01). It is the third engine class
// (EngineHTML), after image (libvips) and document (LibreOffice).
type ChromiumConverter struct {
	// Sandbox is nil-safe and empty by default (Phase 69): a converter built
	// as a bare ChromiumConverter{} (every test predating this phase) stays
	// exactly as comparable and as unconfined as before.
	Sandbox *engineSandbox
	// sharedTmp is the directory Convert sweeps before every task (D-04) --
	// empty means no sweep at all. Set only by ConfinedChromium, never by a
	// bare WithSandbox call, so a converter built for a preflight's own
	// negative run (which must NOT sweep the real /tmp mid-test) never
	// sweeps anything unless a test opts in explicitly.
	sharedTmp string
}

// Pairs returns the single supported {html, pdf} pair -- no cross-pairs
// exist for this engine (D-06/HTML-03 scope).
func (ChromiumConverter) Pairs() []Pair {
	return []Pair{{From: "html", To: "pdf"}}
}

// Engine reports the html engine class (D-01).
func (ChromiumConverter) Engine() string { return EngineHTML }

// cspNoScriptMeta is a server-constant meta tag (never client-controlled)
// that disables script execution via a strict Content-Security-Policy
// (D-05). Plan 04's live smoke checklist found that chromium-headless-shell
// 150.0.7871.100's launch-time --blink-settings=scriptEnabled=false flag
// makes the one-shot --print-to-pdf/--dump-dom command handler silently
// produce NO output at all (exit 0, no file written) -- confirmed
// reproducible on both a script-bearing and a script-free fixture, so this
// is not "JS-heavy pages fail," it is a hard incompatibility between that
// flag and the one-shot command handler in this build. The documented
// alternative flag, --disable-javascript, was also live-tested and found to
// be a no-op (a <script>document.write(...)</script> fixture still executed
// and its output appeared in --dump-dom's DOM). This CSP meta tag, injected
// into the SAME worker-built copy of the HTML that already carries the
// print CSS (Pattern 1's injection point), was live-verified to block
// script execution (document.write's effect was absent from --dump-dom
// output) while leaving --print-to-pdf fully functional -- it is the
// mechanism this converter uses to satisfy D-05, in place of the
// launch-time flag RESEARCH.md originally specified.
const cspNoScriptMeta = `<meta http-equiv="Content-Security-Policy" content="script-src 'none'; object-src 'none'">`

// headCloseTag and htmlOpenPrefix are the case-insensitive markers
// injectPrintCSS searches for to place the print CSS block (RESEARCH.md
// Pattern 1).
var (
	headCloseTag   = []byte("</head>")
	htmlOpenPrefix = []byte("<html")
)

// cspDoctypePrefix is the case-insensitive opener injectCSPFirst recognizes
// at the document start, and htmlWhitespace is the whitespace set it skips
// before looking for it.
var cspDoctypePrefix = []byte("<!doctype")

// injectPrintCSS returns a NEW copy of html with css inserted immediately
// before the last child position of <head> -- i.e. right before the first
// case-insensitive "</head>" -- so the injected rule has cascade priority
// over anything else already in <head> (RESEARCH.md Pattern 1). If no
// "</head>" is found, css is inserted immediately after the end of the
// opening "<html ...>" tag; if neither marker is found (should not happen
// once LooksLikeHTML has already gated the upload, D-07), css is prepended
// at position 0 as a last resort. html is never mutated in place -- the
// caller writes the result to a new file (rendered.html), never
// re-uploading/overwriting the original input.
func injectPrintCSS(html []byte, css string) []byte {
	lower := bytes.ToLower(html)

	if idx := bytes.Index(lower, headCloseTag); idx >= 0 {
		return spliceAt(html, idx, css)
	}
	if idx := bytes.Index(lower, htmlOpenPrefix); idx >= 0 {
		if end := bytes.IndexByte(html[idx:], '>'); end >= 0 {
			return spliceAt(html, idx+end+1, css)
		}
	}
	return spliceAt(html, 0, css)
}

// injectCSPFirst returns a NEW copy of html with csp spliced at the
// DOCUMENT START -- the only point in the document that is guaranteed to
// precede every parser-executable byte, regardless of where a literal
// <head>/<html> tag sits in the source text (UAT gap G-69-2,
// .planning/debug/csp-bypass-before-head.md).
//
// The earlier version located its injection point with a bare byte search
// for "<head" then "<html" -- but source order is not tree-construction
// order: chromium's real HTML5 parser opens an implicit <head>/<body> for
// any content it meets before the literal tag, and then DISCARDS that later
// literal tag as a duplicate. A CSP spliced relative to the discarded tag's
// byte offset lands in the parser's <body> and is REJECTED OUTRIGHT
// ("delivered via a <meta> element outside the document's <head>, which is
// disallowed"), not merely installed late -- confirmed live against the
// production chromium-headless-shell binary for 8 crafted shapes (the debug
// session's divergence table). A byte search can also fire inside an HTML
// comment, splicing the CSP as inert comment text that is never parsed as
// markup at all. Neither failure is possible when nothing is searched for:
// this function performs NO search of the document for a head or html tag.
//
// The splice point itself: skip one leading UTF-8 BOM, then skip only HTML
// whitespace (TAB, LF, FF, CR, SPACE), then, if a case-insensitive
// "<!doctype" run starts there and a '>' follows, splice right after that
// FIRST '>' -- the HTML tokenizer ends a doctype at its first '>', even
// inside a quoted identifier (row x4 of the shape table), so matching that
// exactly keeps the CSP outside the doctype rather than splitting it.
// Otherwise splice at that position (row x3: BOM, no doctype -- byte 3, not
// 0, so the encoding signature stays first).
//
// The whitespace set is deliberately HTML's five characters, not
// LooksLikeHTML's wider trim set (which also accepts a vertical tab):
// measured live, a vertical tab before the doctype is not itself parsed as
// leading whitespace by chromium, so splicing after the doctype there lands
// the CSP outside <head> and the script runs (row x2). Reusing
// LooksLikeHTML's set here would be a live bypass, not mere leniency.
//
// The doctype stays the first token in the output (so the document keeps
// standards mode) and the BOM stays at byte 0 (so encoding detection is
// unaffected) -- both are asserted by every shape-table row's structural
// invariant (chromium_test.go), not merely implied by this comment.
func injectCSPFirst(html []byte, csp string) []byte {
	p := 0
	if bytes.HasPrefix(html, []byte{0xEF, 0xBB, 0xBF}) {
		p = 3
	}
	for p < len(html) {
		switch html[p] {
		case '\t', '\n', '\f', '\r', ' ':
			p++
			continue
		}
		break
	}
	if p+len(cspDoctypePrefix) <= len(html) && bytes.EqualFold(html[p:p+len(cspDoctypePrefix)], cspDoctypePrefix) {
		if end := bytes.IndexByte(html[p:], '>'); end >= 0 {
			return spliceAt(html, p+end+1, csp)
		}
	}
	return spliceAt(html, p, csp)
}

// spliceAt returns a new byte slice with css inserted into html at byte
// offset at, without mutating html's underlying array.
func spliceAt(html []byte, at int, css string) []byte {
	out := make([]byte, 0, len(html)+len(css))
	out = append(out, html[:at]...)
	out = append(out, css...)
	out = append(out, html[at:]...)
	return out
}

// exec builds this job's engineExec: the measured html ruleset (job
// directory + the CONF-06 shared writable paths, read-only the measured
// set), with the engine's environment replaced by the allowlist engineEnv
// builds -- TMPDIR is deliberately left UNSET ("") here, so chromium sees
// the measured default (/tmp) rather than a job-scoped directory it cannot
// use for its shared memory (SANDBOX-PATHS.md html section).
func (c ChromiumConverter) exec(workDir string) engineExec {
	return c.Sandbox.forJobRW(workDir, htmlReadWrite, htmlReadOnly, htmlEngines).withEnv(engineEnv(""))
}

// Convert builds a CSS+CSP-injected copy of inPath (never mutating inPath
// itself, mirroring LibreOfficeConverter's never-touch-inPath discipline)
// and invokes chromium-headless-shell's one-shot print-to-pdf mode against
// that copy, with the layered network-block flags (D-03) and JS disabled
// via the injected CSP meta tag (D-05, see cspNoScriptMeta). ctx must carry
// the engine timeout.
func (c ChromiumConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	workDir := filepath.Dir(outPath) // caller's per-job workDir; already unique, already cleaned up

	// D-04: sweep the container's shared /tmp before this task's engine ever
	// runs, keeping only the current job's own directory (which itself lives
	// under /tmp -- internal/worker/worker.go creates job directories via
	// os.MkdirTemp("", ...)). c.sharedTmp is empty for any converter not
	// built by ConfinedChromium (every pre-Phase-69 test, and the preflight's
	// own negative-run converter), so this is a no-op there.
	if c.sharedTmp != "" {
		if err := sweepSharedTmp(c.sharedTmp, workDir); err != nil {
			return fmt.Errorf("chromium: %w", err)
		}
	}

	// Garbage opts (e.g. a corrupt jobs.options column) is a deterministic
	// failure, not a transient one -- HTMLOptsFromMap applies the same
	// strictness (DisallowUnknownFields + allow-list) here as at the API
	// write path (D-10), mirroring DocOptsFromMap's use in Convert.
	o, err := HTMLOptsFromMap(opts)
	if err != nil {
		return fmt.Errorf("chromium: %w", err)
	}

	input, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("chromium: read input: %w", err)
	}
	// Two separate injections with distinct anchors (CR-01, and G-69-2's
	// fix): the CSP meta is spliced at the DOCUMENT START -- the only point
	// guaranteed to precede every parser-executable byte, wherever a literal
	// <head>/<html> tag sits (injectCSPFirst's own doc comment); the print
	// <style> goes just before </head> for cascade priority, unchanged. Both
	// are server-constant strings selected only by already-validated
	// HTMLOpts fields -- never built from raw client bytes (RESEARCH.md
	// Pattern 1, Pitfall 9's lesson applied to CSS). See cspNoScriptMeta's
	// doc comment for why JS-disable moved from a launch flag to this
	// injection point (Plan 04 live finding).
	rendered := injectCSPFirst(input, cspNoScriptMeta)
	rendered = injectPrintCSS(rendered, buildPrintCSS(o))
	renderedPath := filepath.Join(workDir, "rendered.html")
	if err := os.WriteFile(renderedPath, rendered, 0o600); err != nil {
		return fmt.Errorf("chromium: write rendered html: %w", err)
	}

	// chromiumPrintArgs builds the full flag set (Plan 04 smoke checklist,
	// D-08) -- see its own doc comment for what each flag does and why.
	args := chromiumPrintArgs(workDir, outPath, "file://"+renderedPath)
	if _, err := c.exec(workDir).run(ctx, "chromium-headless-shell", args...); err != nil {
		return fmt.Errorf("chromium: %w", err)
	}

	// --print-to-pdf=<path> writes directly to outPath -- no rename step is
	// needed (unlike soffice's --outdir convention in libreoffice.go).
	// validatePDF is reused verbatim from libreoffice.go; target is always
	// "pdf" for this engine, so no new validator is needed. Wrap in a
	// chromium: context so a failure here doesn't surface with a misleading
	// bare "libreoffice:" prefix (CR-02); the terminal-signature substrings
	// it emits ("output is empty", "output missing %PDF- magic bytes") are
	// preserved inside the wrapped message, so isHTMLTerminal still classifies
	// correctly via the shared terminalLibreOfficeSignatures list.
	if err := validatePDF(outPath); err != nil {
		return fmt.Errorf("chromium: %w", err)
	}
	return nil
}

// chromiumPrintArgs builds the argv chromium-headless-shell is exec'd with,
// for both the production Convert path and the preflight's own sufficiency
// and effectiveness runs (htmlsandbox.go) -- one builder, so every run this
// converter ever makes carries the identical flag set. sourceURL is the
// "file://..." URL of the document to render.
//
// flags verify-live-confirmed in Plan 04 smoke checklist. Notably ABSENT:
// --blink-settings=scriptEnabled=false (live-tested to make the one-shot
// command handler silently produce no output at all -- see
// cspNoScriptMeta's doc comment) and --disable-javascript (live-tested
// no-op, does not actually disable script execution in this build).
// JS-disable is instead enforced via the injected CSP meta tag Convert
// writes into rendered.html. --headless is live-confirmed
// optional-but-harmless for the standalone shell binary (kept for
// explicitness); --no-sandbox is live-confirmed REQUIRED under USER nobody
// (D-08) -- omitting it fails with "No usable sandbox!". --no-pdf-header-
// footer suppresses chromium's default print header/footer, which otherwise
// leaks the internal file:///workDir/rendered.html path and generation
// timestamp into every produced PDF (live-observed default-on behavior, not
// requested by any HTML-03 option).
//
// --user-data-dir pins chromium's profile/scratch/crash-dump state to a
// per-job directory INSIDE workDir (IN-02 hardening). Without it,
// chromium-headless-shell falls back to a $HOME-relative default, which
// under USER nobody is typically non-writable (/nonexistent); pinning it
// here guarantees all scratch state lands within the caller's
// os.RemoveAll(workDir) cleanup boundary -- no stray writes, no cross-job
// state. The value is a server-derived path (workDir), never client bytes.
func chromiumPrintArgs(workDir, outPath, sourceURL string) []string {
	profileDir := filepath.Join(workDir, "chrome-profile")
	return []string{
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--no-pdf-header-footer",
		"--user-data-dir=" + profileDir,
		"--proxy-server=127.0.0.1:9",
		"--proxy-bypass-list=<-loopback>",
		"--host-resolver-rules=MAP * ~NOTFOUND",
		"--print-to-pdf=" + outPath,
		sourceURL,
	}
}
