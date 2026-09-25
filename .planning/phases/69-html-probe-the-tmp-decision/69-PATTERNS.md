# Phase 69: HTML, Probe & the /tmp Decision - Pattern Map

**Mapped:** 2026-09-26
**Files analyzed:** 17 (10 new, 7 modified)
**Analogs found:** 17 / 17

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `internal/convert/chromium.go` (modify) | converter | event-driven (single subprocess exec) | `internal/convert/pandoc.go` (single-call-site engine wiring) | role-match |
| `internal/convert/htmlsandbox.go` (new) | service (sandbox wiring) | request-response (per-job ruleset + preflight) | `internal/convert/pandocsandbox.go` | exact |
| `internal/convert/htmlsandbox_test.go` (new) | test | — | `internal/convert/pandocsandbox_test.go` | exact |
| `internal/convert/sanitizeconverter.go` (modify) | converter | CRUD/transform (dispatch + report write) | `internal/convert/ocr.go` (multi-route Convert building one `engineExec`) | role-match |
| `internal/convert/sanitize.go` (modify) | service (dispatcher) | transform | `internal/convert/ocr.go`'s route dispatch | role-match |
| `internal/convert/sanitizepdf.go` (modify) | converter (format handler) | transform, file-I/O | `internal/convert/ocrpdf.go` (multi-engine PDF route through `x engineExec`) | role-match |
| `internal/convert/sanitizeverify.go` (modify) | service (verifier) | transform | `internal/convert/ocrguards.go` (guard/verify stage threading `x engineExec`) | role-match |
| `internal/convert/probesandbox.go` (new) | service (sandbox wiring) | request-response, multi-call-site | `internal/convert/ocrsandbox.go` | exact |
| `internal/convert/probesandbox_test.go` (new) | test | — | `internal/convert/ocrsandbox_test.go` | exact |
| `internal/convert/sandboxcoverage_test.go` (modify) | test (AST coverage gate) | — | itself, prior `sandboxPendingFiles`/`sandboxClassFileEngines` entries for pandoc/ocr/av/audio | exact |
| `internal/worker/worker.go` (modify: `isHTMLTerminal`) | terminal classifier | request-response | `isPandocTerminal`/`isAudioTerminal`/`isAVTerminal`/`isOCRTerminal` (all four already have the `ErrEngineSandboxUnavailable` guard) | exact |
| `internal/worker/descriptors.go` (modify: `isProbeTerminal`) | terminal classifier | request-response | `isOCRTerminal`/`isAVTerminal` (own-file, own-descriptor placement of the sentinel check) | exact |
| `internal/worker/engine_sandbox_retryable_test.go` (modify) | test | — | itself, existing 4-row table (pandoc/ocr/av/audio) | exact |
| `cmd/chromium-worker/main.go` (modify) | worker entry point | request-response (startup wiring) | `cmd/pandoc-worker/main.go` | exact |
| `cmd/probe-worker/main.go` (modify) | worker entry point | request-response (startup wiring) | `cmd/ocr-worker/main.go` (multi-engine class wiring shape) and `cmd/pandoc-worker/main.go` (RunExecModeIfRequested + ResolveHelper + Preflight block) | exact |
| `docker-compose.yml` (modify) | config | — | `PANDOC_SANDBOX_DISABLED` forwarding block (line ~1276) and `HTML_WORKER_CONCURRENCY: "2"` five-occurrence pattern | exact |
| `deploy/chart/octoconv/templates/deployment-chromium-worker.yaml` (modify) | config (Helm template) | — | `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml`'s `extraEnv` block (probe's own `deployment-probe-worker.yaml` already has one, no template change needed there) | exact |
| `deploy/chart/octoconv/templates/configmap.yaml` (modify: `HTML_WORKER_CONCURRENCY`) | config | — | same file, existing `PANDOC_WORKER_CONCURRENCY: "1"` entry | exact |
| `scripts/conf05-crosscheck.sh` (modify) | script (test-adjacent) | — | itself, existing `pandoc`/`ocr`/`av`/`audio` cases in `class_go_file`/`class_go_readonly_var`/`class_go_engines_var` | exact |
| `.planning/research/SANDBOX-PATHS.md` (modify) | doc (conf05 marker blocks) | — | existing `pandoc`/`ocr`/`av`/`audio` sections' `<!-- conf05:CLASS:KIND -->` blocks | exact |

## Pattern Assignments

### `internal/convert/htmlsandbox.go` (new)

**Analog:** `internal/convert/pandocsandbox.go` (single-call-site class — html has exactly one `runCommand` in `chromium.go:187`, same shape as pandoc's one call site).

**Full file structure to copy** (`internal/convert/pandocsandbox.go:1-93`, read in full above):
- Package-doc comment naming the file's role, mirroring line 1-5.
- `htmlReadOnly []string` var, MEASURED from `.planning/research/SANDBOX-PATHS.md` §html + this phase's D-08 wrapper/interpreter finding. **Deviation from pandoc that must be explicit** (RESEARCH.md Pattern 1): pandoc's static binary needs nothing beyond `pandocEngines` (empty `pandocReadOnly`), but html's wrapper triad (`/usr/bin/chromium-headless-shell` wrapper → `/usr/bin/dash` interpreter → `/usr/lib/chromium/chromium-headless-shell` real binary) is NOT fully auto-resolved by `forJob`'s `engines []string` mechanism — only the wrapper itself gets auto-granted via `htmlEngines`. `/usr/bin/dash` MUST be a literal entry in `htmlReadOnly` (the real binary is covered by `/usr/lib` if that's in the measured set already).
- `htmlEngines = []string{"chromium-headless-shell"}` (one engine, like `pandocEngines = []string{"pandoc"}`).
- `func (c ChromiumConverter) WithSandbox(helper []string) ChromiumConverter { c.Sandbox = newEngineSandbox(helper); return c }` — copy `pandocsandbox.go:35-38` verbatim, renaming the receiver type.
- `HTMLPreflight(ctx, c) error` — copy the `PandocPreflight` shape (`pandocsandbox.go:49-92`) but the marker placement and judgment differ (see below): sufficiency run (inside file, `requireOutput` for the `/Title` marker) + effectiveness run (outside file, `requireDenied` for the blank/no-`/Title` shape). Both `newPreflightWorkspace("html")` and `w.place(...)` reused unmodified from `sandboxpreflight.go`.

**D-06/D-07 marker fixture** (`internal/convert/htmlsandbox.go`, new — no existing analog in this codebase, use the measured shape from 69-RESEARCH.md "Code Examples" and "Measurements" sections verbatim):
```go
marker, err := preflightMarker() // "octoconv-preflight-<16 hex chars>"
html := []byte("<html><head><title>" + marker + "</title></head><body></body></html>")
```
`requireOutput(outInside, []byte("/Title (" + marker + ")"))` — the raw PDF bytes carry `/Title (marker)` verbatim, uncompressed, in object 1's `/Info` dictionary (measured, `octoconv-chromium-worker:measure69`). `requireDenied` needs no marker-specific change: the denied run produces the documented 652-byte blank PDF with no `/Title` key at all — `requireDenied`'s existing "absent, or present but not carrying marker" contract already covers this.

**ChromiumConverter.Convert wiring** (`internal/convert/chromium.go:187`, the ONE call site):
```go
// BEFORE
if _, err := runCommand(ctx, "chromium-headless-shell", args...); err != nil {
```
Becomes (mirrors how `pandoc.go`'s single call site threads `c.Sandbox.forJob(...)`/`x.run(...)` — read `pandoc.go`'s own `Convert` method for the exact before/after shape, since `chromium.go` currently has NO `Sandbox` field at all):
```go
x := c.Sandbox.forJob(workDir, htmlReadOnly, htmlEngines)
if _, err := x.run(ctx, "chromium-headless-shell", args...); err != nil {
```
Requires adding `Sandbox *engineSandbox` to `ChromiumConverter` (currently an empty struct, `chromium.go:14`) — this breaks the struct's current zero-size/comparable shape the same way every other confined converter already accepted (`sandboxwrap.go:12-20`'s own doc comment on why `*engineSandbox` is held by pointer and nil-safe).

**Sandboxcoverage_test.go entries this file feeds:** `sandboxClassFileEngines`/`sandboxClassFileGroups` gain a `"chromium.go", "htmlsandbox.go": return htmlEngines, true` / `return []string{"html"}` case, mirroring `pandoc.go", "pandocsandbox.go"` (`sandboxcoverage_test.go:84-85`, `:104-105`).

---

### `internal/convert/htmlsandbox_test.go` (new)

**Analog:** `internal/convert/pandocsandbox_test.go` (`TestPandocReadOnlySetIsTheMeasuredOne`, `TestPandocEnginesAreTheMeasuredOnes`, `TestPandocConverterStaysComparable` — read in full above).

**Pattern to copy** (`pandocsandbox_test.go:12-40`):
```go
func TestHTMLReadOnlySetIsTheMeasuredOne(t *testing.T) {
	want := []string{ /* the measured set, including /usr/bin/dash per Pitfall 3 */ }
	if len(htmlReadOnly) != len(want) { t.Fatalf(...) }
	...
}

func TestHTMLEnginesAreTheMeasuredOnes(t *testing.T) {
	want := []string{"chromium-headless-shell"}
	...
}

func TestChromiumConverterStaysComparable(t *testing.T) {
	var a, b ChromiumConverter
	if a != b { ... } // fails to COMPILE once Sandbox stops being a pointer
}
```
**Also add** (D-03, CONF-06's "a test holds it"): a concurrency-guard unit test — no direct analog exists yet in this codebase (this is genuinely new machinery per RESEARCH.md's Wave 0 Gaps), but it should live beside these tests or in `cmd/chromium-worker` as a small table test asserting "confined AND concurrency>1 refuses to start" — model the refusal-message assertion style on `sandbox.ResolveHelper`'s own existing CONF-02 test (`internal/sandbox` package, not read this session but referenced in RESEARCH.md's Validation Architecture table as "generic; already exists").

---

### `internal/convert/probesandbox.go` (new)

**Analog:** `internal/convert/ocrsandbox.go` (multi-call-site, multi-engine class — probe has SIX call sites across `sanitizepdf.go`/`sanitizeverify.go`, four distinct binaries, exactly OCR's shape, NOT pandoc's/html's single-call-site shape — RESEARCH.md Pattern 2).

**Imports pattern** (`ocrsandbox.go:1-16`):
```go
package convert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
)
```
(probe's version drops `bytes` unless the preflight also needs byte comparison; keep `context`, `fmt`, `path/filepath` — `os` only if `probesandbox.go` itself creates directories, which it does not since sanitize's own `os.MkdirTemp` calls move to `sanitizepdf.go`/`sanitizeverify.go`, see Pattern 4 below).

**Core "one ruleset shared by several call sites" pattern** (`ocrsandbox.go:56-58`, the exact shape to copy — RESEARCH.md explicitly names this line range):
```go
// exec builds this job's engineExec from outPath's directory -- the ONE
// ruleset shared by every OCR engine call site of this job...
func (c OCRConverter) exec(outPath string) engineExec {
	return c.Sandbox.forJob(filepath.Dir(outPath), ocrReadOnly, ocrEngines)
}
```
becomes:
```go
func (c SanitizeConverter) exec(outPath string) engineExec {
	return c.Sandbox.forJob(filepath.Dir(outPath), probeReadOnly, probeEngines)
}
```
Called ONCE in `SanitizeConverter.Convert` (`sanitizeconverter.go:112`), BEFORE the format dispatch — RESEARCH.md Pattern 2's explicit anti-pattern warning: do NOT build `x` per-format; build it statically for all four engines unconditionally, exactly as `ocrEngines` includes `pdftotext`/`pdfinfo` even for an image-only job.

**Measured grant set** (`probeReadOnly`/`probeEngines` — from 69-RESEARCH.md's D-10 measurements, corrected from the stale 2026-09-20 SANDBOX-PATHS.md set):
```go
var probeEngines = []string{"gs", "pdfinfo", "unzip", "file"}

// probeReadOnly is what probe's four engines need beyond their own binaries,
// MEASURED this session (D-10) against the real exec sites, not the
// 2026-09-20 document (which omitted `file` entirely). /etc/magic and
// /usr/share/misc are NOT here -- both proven unnecessary by removal
// (file falls back through the already-granted /usr/lib path to the same
// compiled-in magic.mgc). /usr/share/poppler IS here -- kept conservatively
// (Assumption A1) even though the one ASCII-metadata fixture tested did not
// strictly need it.
var probeReadOnly = []string{
	"/lib", "/usr/lib", "/usr/share/ghostscript", "/usr/share/color",
	"/usr/share/fonts", "/usr/share/poppler", "/var/lib/ghostscript",
	"/etc/ld.so.cache", "/etc/localtime", "/etc/papersize",
	// NOTE: no "/tmp" -- D-05 measured gs honours TMPDIR; see Pattern 4.
}
```
(Exact entries must be reconciled against SANDBOX-PATHS.md's corrected §probe section and `scripts/sandbox-paths-measure.sh` re-run output — the list above is RESEARCH.md's own aggregated candidate set, Measurements/D-10.)

**WithSandbox** — copy `ocrsandbox.go:74-82` verbatim, renaming the receiver:
```go
func (c SanitizeConverter) WithSandbox(helper []string) SanitizeConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}
```
Requires adding `Sandbox *engineSandbox` field to `SanitizeConverter` (`sanitizeconverter.go:55-57`, currently only `MaxPages int`).

**Preflight** — mirrors `OCRPreflight`'s multi-route shape (`ocrsandbox.go:164-229`) but probe's routes are PDF (gs×2 + pdfinfo) / OOXML (unzip×2 + file) / image (file), each needing its own fixture placed via `newPreflightWorkspace("probe")` / `w.place(...)`, judged with `requireOutput`/`requireDenied` — probe's denial mode is LOUD (non-zero exit + a permission-denied string on stdout/stderr for all four engines, unlike chromium's silent blank output), but RESEARCH.md is explicit: still use `requireOutput`/`requireDenied` for consistency with every other class (CONF-04's letter). Effectiveness proof per RESEARCH.md Measurements/D-10 (`gs`: 166046-byte skeleton with grep for content markers = 0; `pdfinfo`: "I/O Error...Permission denied"; `unzip -Z1`: "cannot open zipfile...Permission denied"; `file`: "writable, regular file, no read permission").

---

### `internal/convert/probesandbox_test.go` (new)

**Analog:** `internal/convert/ocrsandbox_test.go` (not read line-by-line this session, but structurally mirrors `pandocsandbox_test.go`'s pinned-set tests, extended for four engines instead of one). Copy `TestPandocReadOnlySetIsTheMeasuredOne`'s shape (`pandocsandbox_test.go:15-21`) for `probeReadOnly`/`probeEngines`, plus a `TestSanitizeConverterStaysComparable` mirroring `TestPandocConverterStaysComparable`.

---

### `internal/convert/chromium.go` (modify)

**Analog:** `internal/convert/pandoc.go`'s `Convert` method (single call site threading `c.Sandbox.forJob`/`x.run`) — not read verbatim this session, but its shape is fully specified by `pandocsandbox.go`'s doc comments and `sandboxwrap.go`'s `forJob`/`run` contract, both read in full.

**Current state read in full** (`chromium.go:1-205`, above): `ChromiumConverter` is `struct{}` (line 14), `Convert` builds `workDir := filepath.Dir(outPath)` (line 121, already exists — this is exactly what `forJob`'s `jobDir` parameter needs) and calls `runCommand(ctx, "chromium-headless-shell", args...)` at line 187.

**Required change:**
```go
// ADD to the struct:
type ChromiumConverter struct {
	Sandbox *engineSandbox
}

// CHANGE line 187:
x := c.Sandbox.forJob(workDir, htmlReadOnly, htmlEngines)
if _, err := x.run(ctx, "chromium-headless-shell", args...); err != nil {
	return fmt.Errorf("chromium: %w", err)
}
```
`c` must become a value receiver capable of reading `Sandbox` — check whether `Convert`'s current receiver `(ChromiumConverter)` (value receiver, line 120) needs no change since `*engineSandbox` is itself nil-safe (`sandboxwrap.go:64-67`, `:127-129`).

**D-04 sweep wiring** (Pitfall 7, Claude's discretion): a `sweepTmp(workDir's sibling /tmp)` call at the top of `Convert`, classified retryable on failure — no existing analog for "sweep shared /tmp before a task" anywhere in this codebase; this is genuinely new code the plan must design, not copy. RESEARCH.md's own recommendation: model it as a small helper called where "the audio ffprobe duration guard runs today" (`internal/worker/worker.go`'s `process()` — not read this session, locate via grep for `EnforceMaxDuration` before writing the plan).

---

### `internal/convert/sanitizeconverter.go` (modify)

**Analog:** `internal/convert/ocr.go`'s `Convert` method — builds `engineExec` once, before dispatching by route (not read verbatim this session; its shape is what `ocrsandbox.go`'s `func (c OCRConverter) exec(outPath string) engineExec` doc comment describes as its own caller contract).

**Current state read in full above** (`sanitizeconverter.go:112-155`): `Convert` calls `enforce(ctx, inPath, c.MaxPages)` (page ceiling, no exec) then `SanitizeFile(ctx, format, inPath, outPath)` (line 138) — no `x engineExec` exists anywhere in this call chain today.

**Required change** (RESEARCH.md's "Code Examples" / Pattern 2, illustrative signature given there):
```go
func (c SanitizeConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	...
	x := c.exec(outPath) // built ONCE, threaded through everything below
	if enforce, ok := sanitizePageCeilings[format]; ok {
		if err := enforce(ctx, x, inPath, c.MaxPages); err != nil { // enforcePDFPageCeiling gains x
			return err
		}
	}
	res, err := SanitizeFile(ctx, x, format, inPath, outPath) // SanitizeFile gains x
	...
}
```

---

### `internal/convert/sanitize.go` (modify)

**Analog:** the dispatch-map pattern itself has no single-function analog; the closest shape is `ocr.go`'s free-function dispatch by route, but the SPECIFIC complication (RESEARCH.md Pattern 2, "The complication OCR does not have") is that `sanitizeFormats` is a `map[string]func(...)` whose VALUE TYPE must change — no existing map in this codebase has been through this exact migration, so this is new work guided by the illustrative signature RESEARCH.md gives verbatim:

```go
// sanitize.go, CHANGED (RESEARCH.md Code Examples, quoted exactly)
var sanitizeFormats = map[string]func(context.Context, engineExec, string, string) (SanitizeResult, error){
	"pdf":  sanitizePDF,        // uses x
	"docm": sanitizeOOXML,      // x unused -- no exec in this file
	...
}

func SanitizeFile(ctx context.Context, x engineExec, format, inPath, outPath string) (SanitizeResult, error) {
	...
	res, err := fn(ctx, x, inPath, outPath)
	...
	verified, verr := VerifySanitized(ctx, x, norm, outPath)
	...
}
```
Current file read in full above (`sanitize.go:70-132`): `sanitizeFormats` (line 70), `SanitizeFile` (line 105) call `fn(ctx, inPath, outPath)` (line 111) and `VerifySanitized(ctx, norm, outPath)` (line 121) — both gain the `x engineExec` parameter. `sanitizeOOXML`/`sanitizeImage` (in `sanitizeooxml.go`/`sanitizeimage.go`, not read this session — locate before planning) carry the unused `x` parameter per the anti-pattern warning: do NOT skip them, the map value type is shared and a partial change does not compile.

---

### `internal/convert/sanitizepdf.go` (modify)

**Analog:** `internal/convert/ocrpdf.go` (multi-engine PDF route threading `x engineExec` through `pdfinfo`/render calls — not read verbatim this session, but its call shape is `ocrsandbox.go`'s own doc-comment description of `ocrpdf.go` as one of OCR's three call-site files sharing one `x`).

**Exec sites to change** (all three read in full above, `sanitizepdf.go:220-304`):
```go
// probePDFPageCount, line 220-221 -- CHANGE
func probePDFPageCount(ctx context.Context, x engineExec, inPath string) (int, error) {
	out, err := x.run(ctx, "pdfinfo", ocrPageCountArgs(inPath)...)
	...
}

// enforcePDFPageCeiling, line 244 -- CHANGE (threads x through to probePDFPageCount)
func enforcePDFPageCeiling(ctx context.Context, x engineExec, inPath string, max int) error {
	...
	count, err := probePDFPageCount(ctx, x, inPath)
	...
}

// sanitizePDF, lines 255-304 -- CHANGE both gs calls, and relocate MkdirTemp (Pattern 4 below)
func sanitizePDF(ctx context.Context, x engineExec, inPath, outPath string) (SanitizeResult, error) {
	jobDir := filepath.Dir(outPath)
	tmpDir, err := os.MkdirTemp(jobDir, ".sanitize-pdf-") // WAS: os.MkdirTemp("", "octoconv-sanitize-pdf-*")
	...
	if x.sandbox.confines() {
		os.Setenv("TMPDIR", tmpDir) // safe: PROBE_WORKER_CONCURRENCY=1 (D-01)
	}
	...
	if _, err := x.run(ctx, "gs", ps2writeArgs(inPath, psPath)...); err != nil { // WAS runCommand
	...
	if _, err := x.run(ctx, "gs", pdfwriteArgs(psPath, outPath)...); err != nil { // WAS runCommand
	...
}
```
(The `os.MkdirTemp`/`TMPDIR` shape above is RESEARCH.md's "D-05 TMPDIR fix" code example, quoted verbatim from `sanitizepdf.go:304-317` of the research doc — this is the authoritative pattern to copy for D-05's precondition, see Pattern 4/Pitfall 2 below.)

**`classifyEngineFailure` fix — the sandbox-fault-preservation defect (RESEARCH.md Pattern 3, this phase's own most significant original finding):**

Current code, read in full above (`sanitizepdf.go:348-356`):
```go
func classifyEngineFailure(stage string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("sanitize pdf: %s: %w", stage, err)
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("sanitize pdf: %s: engine binary is not available in this image: %w", stage, err)
	}
	return fmt.Errorf("%w: %s: %v", ErrSanitizeMalformedInput, stage, err)
}
```
**MUST gain a new branch, checked BEFORE the generic `%v` fallback** (mirrors the four existing `errors.Is(err, convert.ErrEngineSandboxUnavailable)` precedents in `worker.go`, e.g. `isAudioTerminal` at `worker.go:301`, read in full above):
```go
func classifyEngineFailure(stage string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("sanitize pdf: %s: %w", stage, err)
	}
	if errors.Is(err, ErrEngineSandboxUnavailable) {
		return err // preserve the sentinel unwrapped -- isProbeTerminal must see it
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("sanitize pdf: %s: engine binary is not available in this image: %w", stage, err)
	}
	return fmt.Errorf("%w: %s: %v", ErrSanitizeMalformedInput, stage, err)
}
```
Without this, every sandbox-establishment fault on a probe node gets silently rewritten to `ErrSanitizeMalformedInput` — TERMINAL per `isProbeTerminal` (`descriptors.go:334-336`, reads `errors.Is(err, convert.ErrSanitizeMalformedInput)` and returns `true`) — misclassifying a node-level fault as a client-blaming permanent failure (Pitfall 1).

---

### `internal/convert/sanitizeverify.go` (modify)

**Analog:** `internal/convert/ocrguards.go` (guard/verify stage threading `x engineExec`; not read verbatim this session, cited by `ocrsandbox.go`'s doc comment as one of OCR's three call-site files).

**Exec sites to change** (all read in full above):
```go
// verifyOOXML, line 790 -- CHANGE signature and both exec calls
func verifyOOXML(ctx context.Context, x engineExec, format, path string) ([]ThreatClass, error) {
	listing, err := x.run(ctx, "unzip", "-Z1", path) // WAS runCommand, line 791
	if err != nil {
		if errors.Is(err, ErrEngineSandboxUnavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: unzip could not list the output: %v", ErrSanitizeUnverifiable, err)
	}
	...
	dir, err := os.MkdirTemp(filepath.Dir(path), ".verify-") // WAS: os.MkdirTemp("", "octoconv-verify-*"), line 829
	...
	blob, err := x.run(ctx, "unzip", "-p", path, escapeZipGlob(part)) // WAS runCommand, line 841
	...
	out, err := x.run(ctx, "file", "--mime-type", "-b", extracted) // WAS runCommand, line 850
	...
}

// verifyPDF, line 216 -- signature gains unused x (no exec in this function, pdfcpu-based)
func verifyPDF(ctx context.Context, x engineExec, _, path string) ([]ThreatClass, error) { ... }

// verifyImage, line 1283 -- CHANGE signature and the one exec call
func verifyImage(ctx context.Context, x engineExec, _, path string) ([]ThreatClass, error) {
	...
	out, err := x.run(ctx, "file", "--mime-type", "-b", path) // WAS runCommand, line 1327
	if err != nil {
		if errors.Is(err, ErrEngineSandboxUnavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: file(1) could not classify the output: %v", ErrSanitizeUnverifiable, err)
	}
	...
}
```
**Every `%v`-wrap around an unzip/file failure must gain the same sandbox-fault-preservation guard** as `classifyEngineFailure` above (Pattern 3) — RESEARCH.md names four such sites explicitly: `verifyOOXML`'s three (`sanitizeverify.go:791-794` unzip listing, `:841-844` unzip extract, `:850-853` file classify) and `verifyImage`'s one (`:1327-1330`). The `verifiers` map (`sanitizeverify.go:128`) also needs its value-type signature changed to match `sanitizeFormats`' new shape.

---

## Shared Patterns

### `engineExec` construction and `run`
**Source:** `internal/convert/sandboxwrap.go` (read in full above, unchanged this phase per D-12/D-11 — this file is `sandboxInfraFiles` in `sandboxcoverage_test.go:47-50` and may be called freely).
**Apply to:** `htmlsandbox.go`, `probesandbox.go`, `chromium.go`, `sanitizepdf.go`, `sanitizeverify.go`.
```go
func (s *engineSandbox) forJob(jobDir string, readOnly, engines []string) engineExec {
	if !s.confines() {
		return engineExec{sandbox: s}
	}
	ro := append([]string{}, readOnly...)
	for _, name := range engines {
		if p, err := exec.LookPath(name); err == nil {
			ro = append(ro, p)
		}
	}
	return engineExec{sandbox: s, spec: spec([]string{jobDir}, ro)}
}

func (e engineExec) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmdName, cmdArgs, err := e.sandbox.wrapEngine(name, e.spec, args)
	if err != nil { return nil, err }
	out, err := runCommandAs(ctx, name, cmdName, cmdArgs...)
	return out, e.sandboxFault(err)
}
```

### Preflight scaffolding (paired inside/outside workspace, content-not-exit-code judgment)
**Source:** `internal/convert/sandboxpreflight.go` (read in full above, unchanged this phase).
**Apply to:** `HTMLPreflight` (new, `htmlsandbox.go`), `ProbePreflight` (new, `probesandbox.go`).
```go
w, cleanup, err := newPreflightWorkspace("<class>")
defer cleanup()
inside, outside, err := w.place("in.<ext>", fixtureBytes)
// sufficiency
if err := c.Convert(ctx, inside, outInside, nil); err != nil { ... }
if err := requireOutput(outInside, []byte(marker)); err != nil { ... }
// effectiveness
convErr := c.Convert(ctx, outside, outOutside, nil)
if err := requireDenied(convErr, outOutside, []byte(marker)); err != nil { ... }
```

### Startup refusal / escape-hatch resolution (CONF-02)
**Source:** `internal/sandbox.ResolveHelper` (referenced by every `cmd/*-worker/main.go`, e.g. `cmd/pandoc-worker/main.go:55-58` read in full above; the package itself was not re-read this session, unchanged per D-11).
**Apply to:** `cmd/chromium-worker/main.go` (new: `HTML_SANDBOX_DISABLED`), `cmd/probe-worker/main.go` (new: `PROBE_SANDBOX_DISABLED`) — NEITHER worker wires this today (confirmed by reading both files in full above).
```go
const htmlSandboxDisabledEnv = "HTML_SANDBOX_DISABLED" // mirrors pandocSandboxDisabledEnv, cmd/pandoc-worker/main.go:37
...
if sandbox.RunExecModeIfRequested(os.Args) {
	return
}
...
sandboxHelper, err := sandbox.ResolveHelper(htmlSandboxDisabledEnv)
if err != nil {
	log.Fatalf("html sandbox: %v", err)
}
```

### D-03 concurrency-vs-shared-/tmp refusal (html only — NEW pattern, no direct prior-phase analog)
**Source:** no existing precedent in phases 67/68; nearest structural cousin is the `sandbox.ResolveHelper` fail-closed shape itself (refuse to start, name the variable and the reason — CONF-02's own posture, applied to a different invariant per D-03's own text: "Hold the invariant by refusing to start, not with a comment").
**Apply to:** `cmd/chromium-worker/main.go` only (per D-05, probe drops `/tmp` and D-03/D-04 do not apply to it).
```go
// NEW, no analog: if the ruleset still grants shared /tmp (i.e. html, which
// keeps it per D-05's per-class finding) AND configured concurrency > 1,
// refuse to start -- the shared-/tmp grant is safe ONLY at concurrency 1.
if len(sandboxHelper) > 0 && envInt("HTML_WORKER_CONCURRENCY", 1) > 1 {
	log.Fatalf("HTML_WORKER_CONCURRENCY=%d with sandbox confinement enabled: "+
		"the grant set includes the shared /tmp, which is safe only at concurrency 1 (D-01/D-03)",
		envInt("HTML_WORKER_CONCURRENCY", 1))
}
```

### `ErrEngineSandboxUnavailable` preservation through the terminal classifier
**Source:** `internal/worker/worker.go` — `isAudioTerminal` (`:294-303`), `isAVTerminal` (`:362-368`), `isOCRTerminal` (`:443-450`), `isPandocTerminal` (`:555-562`), all read in full above; each has the identical guard:
```go
if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
	return false
}
```
**Apply to:** `isHTMLTerminal` (`worker.go:253-255`, currently ONLY `return timeoutIsTerminal(err)` — accidentally safe today per RESEARCH.md, but should gain the explicit check to convert an accident into a stated invariant) and `isProbeTerminal` (`descriptors.go:313-351`, read in full above — this one is NOT accidentally safe; it actively misclassifies via `classifyEngineFailure`'s current `%v` flattening, see Pattern 3 above). Add the check as the FIRST branch in `isProbeTerminal`, before the `ErrSanitizeResidue`/`ErrSanitizeUnverifiable`/`ErrSanitizeMalformedInput` checks:
```go
func isProbeTerminal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
		return false
	}
	if errors.Is(err, convert.ErrSanitizeResidue) || errors.Is(err, convert.ErrSanitizeUnverifiable) {
		return true
	}
	...
}
```
**Test to extend:** `internal/worker/engine_sandbox_retryable_test.go` (read in full above) — add two rows to BOTH tables' `cases` slices:
```go
{"html", isHTMLTerminal, fmt.Errorf("chromium: %w", convert.ErrEngineSandboxUnavailable)},
{"probe", isProbeTerminal, fmt.Errorf("sanitize pdf: ps2write pass: %w", convert.ErrEngineSandboxUnavailable)},
```
and the corresponding control-shape rows in `TestEngineSandboxControlShapesAreTerminal` with `plain := errors.New("exit status 1")` in place of the sentinel.

### `/tmp` relocation — Go-level `os.MkdirTemp` fix (probe only, D-05's precondition)
**Source:** no prior-phase analog exists for this specific bug shape (a scratch-dir creation call using the system temp dir instead of the job directory); closest structural precedent is `ocrsandbox.go`'s own `ocrPageWorkDir` (`ocrsandbox.go:60-72`, read in full above):
```go
func ocrPageWorkDir(outPath string) (string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(outPath), ".ocr-pdf-")
	if err != nil {
		return "", fmt.Errorf("ocr: pdf tmpdir: %w", err)
	}
	return dir, nil
}
```
**Apply to:** `sanitizePDF` (`sanitizepdf.go:256`, currently `os.MkdirTemp("", "octoconv-sanitize-pdf-*")`) and `verifyOOXML` (`sanitizeverify.go:829`, currently `os.MkdirTemp("", "octoconv-verify-*")`) — both MUST become `os.MkdirTemp(filepath.Dir(outPath), pattern)` (or `filepath.Dir(path)` for `verifyOOXML`, which only has `path`). This is a REQUIRED precondition for D-05's `/tmp`-free grant set to work at all (Pitfall 2: skipping this breaks every single PDF sanitize job at job-directory creation, immediately, the moment `/tmp` leaves the ruleset).

### Compose escape-hatch forwarding (CONF-03 pattern)
**Source:** `docker-compose.yml:1273-1276` (`PANDOC_SANDBOX_DISABLED`, read above) — the pattern every other confined class already has (`IMAGE_SANDBOX_DISABLED` at line 250, `AUDIO_SANDBOX_DISABLED` at 803, `AV_SANDBOX_DISABLED` at 928, `OCR_SANDBOX_DISABLED` at 1093, `TECTONIC_SANDBOX_DISABLED` at 1608).
**Apply to:** the `chromium-worker` service block (add `HTML_SANDBOX_DISABLED: "${HTML_SANDBOX_DISABLED:-}"`, comment style identical to line 1273-1276) and the `probe-worker` service block (add `PROBE_SANDBOX_DISABLED: "${PROBE_SANDBOX_DISABLED:-}"`) — grep for both across the whole file this session found zero existing matches, confirming neither is wired.
```yaml
# Same reasoning as IMAGE_SANDBOX_DISABLED on the worker service:
# forwarded from .env, not set here -- unset means confined, and the
# worker refuses to start if it cannot be.
HTML_SANDBOX_DISABLED: "${HTML_SANDBOX_DISABLED:-}"
```

### `HTML_WORKER_CONCURRENCY: "2"` → `"1"` (D-01) — all five occurrences, plus the chromium-worker service's own default
**Source:** `docker-compose.yml`, `grep -n 'HTML_WORKER_CONCURRENCY: "2"'` (per RESEARCH.md Pitfall 6) — appears in `chromium-worker`'s own block (line 690, read above) AND seven other services that unconditionally cross-read every engine var for IN-02 parity (`queue.TestComposeReplicatesEveryEngineVarToEveryQueueClientService` — read that test, cited but not read this session, BEFORE deciding whether value or presence is asserted; if value equality, change ALL occurrences together, not just line 690).
**Apply to:** `docker-compose.yml` (all matching lines), `deploy/chart/octoconv/templates/configmap.yaml:48` (`HTML_WORKER_CONCURRENCY: "2"` → `"1"`), and `cmd/chromium-worker/main.go:91`'s own `envInt("HTML_WORKER_CONCURRENCY", 2)` default → `1`.

### Chart `extraEnv` block (html needs a NEW one; probe already has one)
**Source:** `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml:62-81` (read above, full block).
**Apply to:** `deploy/chart/octoconv/templates/deployment-chromium-worker.yaml` — confirmed by `grep -n extraEnv` returning nothing (RESEARCH.md Pitfall 5) — needs the identical block, `.Values.chromiumWorker.extraEnv` in place of `.Values.pandocWorker.extraEnv`:
```yaml
{{- if .Values.chromiumWorker.extraEnv }}
env:
  {{- range $k, $v := .Values.chromiumWorker.extraEnv }}
  - name: {{ $k }}
    value: {{ $v | quote }}
  {{- end }}
{{- end }}
```
`deployment-probe-worker.yaml` already has this block (`:75-88`, read above, Phase 28) — no template change needed there, only the compose-side var.

### `conf05-crosscheck.sh` class wiring
**Source:** `scripts/conf05-crosscheck.sh:36-63` (read in full above) — `class_go_file`/`class_go_readonly_var`/`class_go_engines_var` functions, each a `case` statement currently covering only `pandoc`/`ocr`/`av`/`audio`.
**Apply to:** add `html)` and `probe)` cases:
```bash
class_go_file() {
  case "$1" in
    pandoc) echo "internal/convert/pandocsandbox.go" ;;
    ocr)    echo "internal/convert/ocrsandbox.go" ;;
    av)     echo "internal/convert/avsandbox.go" ;;
    audio)  echo "internal/convert/audiosandbox.go" ;;
    html)   echo "internal/convert/htmlsandbox.go" ;;
    probe)  echo "internal/convert/probesandbox.go" ;;
    *) die "unknown class: $1 (expected pandoc, ocr, av, audio, html or probe)" ;;
  esac
}
class_go_readonly_var() {
  case "$1" in
    ...
    html)  echo "htmlReadOnly" ;;
    probe) echo "probeReadOnly" ;;
  esac
}
class_go_engines_var() {
  case "$1" in
    ...
    html)  echo "htmlEngines" ;;
    probe) echo "probeEngines" ;;
  esac
}
```

### `sandboxcoverage_test.go` reclassification
**Source:** `internal/convert/sandboxcoverage_test.go:63-133` (read in full above).
**Apply to:**
1. REMOVE `chromium.go`, `sanitizepdf.go`, `sanitizeverify.go` from `sandboxPendingFiles` (`:67-73`) — leaving `libreoffice.go`/`verapdf.go` (phase 70) in place.
2. ADD to `sandboxClassFileEngines` (`:82-96`):
```go
case "chromium.go", "htmlsandbox.go":
	return htmlEngines, true
case "sanitizeconverter.go", "sanitize.go", "sanitizepdf.go", "sanitizeverify.go", "probesandbox.go":
	return probeEngines, true
```
3. ADD to `sandboxClassFileGroups` (`:102-116`):
```go
case "chromium.go", "htmlsandbox.go":
	return []string{"html"}
case "sanitizeconverter.go", "sanitize.go", "sanitizepdf.go", "sanitizeverify.go", "probesandbox.go":
	return []string{"probe"}
```
4. ADD to `sandboxGroupEngines` (`:118-133`):
```go
case "html":
	return htmlEngines
case "probe":
	return probeEngines
```
5. `TestConfinedClassesExecOnlyThroughTheSandbox`'s non-vacuity loop (`:466`, currently `[]string{"pandoc.go", "ocrguards.go", "av.go", "whisper.go"}`) and group loop (`:472`, currently `[]string{"pandoc", "ocr", "av", "audio"}`) BOTH need `"chromium.go"`/`"html"` and `"sanitizepdf.go"`/`"probe"` (or similar) added — this is a required-not-optional change since the gate is exactly what turns "wired but unchecked" into "wired and checked."

### SANDBOX-PATHS.md conf05 marker blocks
**Source:** existing `<!-- conf05:pandoc:readonly -->`/`<!-- conf05:pandoc:engines -->` pairs (not read verbatim this session, but their exact shape is what `conf05-crosscheck.sh`'s `require_doc_block`/`extract_doc_block` functions parse, read in full above — `open="<!-- conf05:${class}:${kind} -->"`).
**Apply to:** add `<!-- conf05:html:readonly -->`...`<!-- /conf05:html:readonly -->`, `<!-- conf05:html:engines -->`...`<!-- /conf05:html:engines -->`, and the same pair for `probe`, matching the corrected `htmlReadOnly`/`htmlEngines`/`probeReadOnly`/`probeEngines` Go vars exactly (CONF-05's own "held by a test, not a comment" requirement runs through this exact string-for-string equality).

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| D-03 concurrency-guard refusal in `cmd/chromium-worker/main.go` | startup guard | — | No prior phase needed "refuse to start above concurrency N when the grant set depends on it" — genuinely new invariant (see Shared Patterns above for the closest structural cousin, `sandbox.ResolveHelper`'s CONF-02 posture, and RESEARCH.md's own code sketch) |
| D-04 `/tmp` sweep helper (html) | utility | file-I/O | No existing "sweep before every task" mechanism anywhere in this codebase; RESEARCH.md Pitfall 7 gives a design sketch (`sweepTmp(dir string) error`, called once per task, classified retryable) but there is no file to copy from |
| Per-run PDF `/Title` marker fixture generator | test fixture helper | — | `ocrsandbox.go`'s `ocrPreflightPDF` (a full xref-correct synthetic PDF builder) is the closest shape for "build a fixture PDF a real engine will accept," but html's fixture is an HTML file with a `<title>`, structurally simpler — RESEARCH.md's own "Code Examples" section is the authoritative source, not a Go file in the repo |

## Metadata

**Analog search scope:** `internal/convert/` (all `*sandbox*.go`, `chromium.go`, `sanitize*.go`, `pandoc.go`, `ocr*.go`, `sandboxwrap.go`, `sandboxpreflight.go`, `sandboxcoverage_test.go`), `internal/worker/` (`worker.go`, `descriptors.go`, `engine_sandbox_retryable_test.go`), `cmd/pandoc-worker/`, `cmd/chromium-worker/`, `cmd/probe-worker/`, `docker-compose.yml`, `deploy/chart/octoconv/templates/`, `scripts/conf05-crosscheck.sh`
**Files scanned:** 20 read in full or substantially, all confirmed git-tracked (no `.gsd/` or plugin-mirror paths involved — this is a pure in-repo Go/YAML/shell codebase, no capability-mirror gate applies)
**Pattern extraction date:** 2026-09-26
