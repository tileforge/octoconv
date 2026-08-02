// Package convert (this file): pandoc.go implements PandocConverter, the
// Pandoc engine foundation core (PDOC-01, PDOC-02, PDCE-02, 47-CONTEXT.md
// D-02/D-04) -- markup-family (md/rst/org/latex/html/docx/epub)
// interconversion via the real `pandoc` binary, mirroring OCRConverter's
// (ocr.go) / AudioConverter's (whisper.go) standalone-converter shape:
// Pairs()/Engine()/Convert(), argv builders isolated as their own pure
// functions, a guard stage BEFORE the subprocess. Built and unit-tested this
// phase directly against a real pandoc 3.10.1 binary but deliberately NOT
// wired into the process-wide converter registry (mirrors Phase 30/34/43's
// own scope fence) -- registration is Phase 48's job.
//
// This is the phase's highest-risk core: every invocation is hardened from
// line one -- raw HTML/TeX passthrough disabled via a PER-SOURCE reader-
// extension table (never a blanket suffix, which errors for rst/org/docx and
// over-claims for latex), `--sandbox` (pandoc 2.19+'s own IO-restricting
// flag -- NOT the TeX-engine's own shell-escape-suppressing flag, which
// pandoc's CLI rejects outright as an unknown option), no network-enabling
// flag ever added, and dispatch exclusively through the existing hardened
// runCommand (exec.go, Setpgid + SIGKILL-on-timeout, no shell) -- unchanged.
//
// This file lands the Pairs()/Engine() allowlist and the hardened pure argv
// builder first (Task 1) -- Convert's runCommand dispatch and output
// validation follow in the same file (Task 2), so the argv shape is unit-
// tested against fixed inputs before any real subprocess is ever invoked.
package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// pandocPairTable is the CLOSED curated allowlist of meaningful markup
// interconversions across {md, rst, org, latex, html, docx, epub} (D-02) --
// an explicit hardcoded table, NOT the full 7x7 cartesian and NOT "anything
// pandoc's reader/writer matrix technically accepts". Identity pairs are
// excluded (never present as a from-key's own target); nonsensical/low-value
// directions are excluded too (epub->latex, epub->org -- an ebook container
// converting into bare TeX/org source has no meaningful use case in this
// project). Finalized against pandoc 3.10.1's real reader/writer
// capabilities during Phase 47 execution/testing (Claude's Discretion,
// 47-CONTEXT.md). An unlisted (from, to) pair is rejected by
// pandocPairSupported BEFORE any subprocess runs (PDOC-01).
var pandocPairTable = map[string][]string{
	"md":    {"html", "docx", "epub", "rst", "org", "latex"},
	"rst":   {"html", "docx", "epub", "md"},
	"org":   {"html", "docx", "epub", "md"},
	"latex": {"html", "docx", "epub", "md"},
	"html":  {"md", "docx", "epub", "rst", "latex"},
	"docx":  {"md", "html", "rst", "org", "latex", "epub"},
	"epub":  {"md", "html", "docx"},
}

// pandocStructuralTargets is the closed set of target formats that must be
// produced as a complete standalone document (full doctype/root-element
// wrapper, not a bare fragment) rather than a plain content fragment
// (PDOC-02) -- html/latex/docx/epub. md/rst/org targets never need
// --standalone: they are always emitted as plain markup text.
var pandocStructuralTargets = map[string]bool{
	"html":  true,
	"latex": true,
	"docx":  true,
	"epub":  true,
}

// PandocConverter converts across the closed markup-family allowlist
// (pandocPairTable) by shelling out to the real `pandoc` binary -- the
// seventh engine class (EnginePandoc), mirroring OCRConverter's/
// AudioConverter's shape: a plain struct, Pairs()/Engine()/Convert(), argv
// builders isolated as their own pure functions for unit-testability. It is
// built and unit-tested this phase directly against a real pandoc 3.10.1
// binary but deliberately NOT wired into the process-wide converter
// registry (Phase 47 scope fence, mirrors Phase 30's/34's/43's own fence) --
// registration is Phase 48's responsibility.
//
// MaxInputBytes lets an operator-configured instance override the input-size
// DoS guard (enforcePandocInputSize, markupsniff.go, Plan 02) without
// editing package consts -- a zero value means "no cap configured this
// phase" (enforcePandocInputSize's own zero-value convention), so a bare
// PandocConverter{} is well-defined, mirroring OCRConverter.MaxImagePixels's/
// AVConverter.MaxSourceDuration's zero-value-defaulting contract.
type PandocConverter struct {
	MaxInputBytes int64
}

// var _ Converter ensures PandocConverter's concrete value type satisfies the
// Converter interface at compile time -- mirrors OCRConverter{}'s own
// assertion (ocr.go), pinning a value receiver across every method here.
var _ Converter = PandocConverter{}

// Pairs returns the closed curated (from, to) markup-family allowlist
// (pandocPairTable, D-02) -- NOT a generated cartesian product.
func (PandocConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, 32)
	for from, tos := range pandocPairTable {
		for _, to := range tos {
			pairs = append(pairs, Pair{From: from, To: to})
		}
	}
	return pairs
}

// Engine reports the pandoc engine class (D-01/DEBT-02, convert.go).
func (PandocConverter) Engine() string { return EnginePandoc }

// pandocPairSupported reports whether (from, to) is in the closed curated
// allowlist -- the SAME table Pairs() walks, so Convert's fast-reject and
// the advertised Pairs() can never drift apart. Called BEFORE any subprocess
// runs (PDOC-01) -- an unlisted pair never burns a pandoc invocation.
func pandocPairSupported(from, to string) bool {
	tos, ok := pandocPairTable[from]
	if !ok {
		return false
	}
	for _, t := range tos {
		if t == to {
			return true
		}
	}
	return false
}

// pandocFormatName translates this project's own normalized format token
// (NormalizeFormat, convert.go) into pandoc's own reader/writer format
// identifier. Live-verified against pandoc 3.10.1's `--list-input-formats`/
// `--list-output-formats`: pandoc has NO format literally named "md" --
// only "markdown" (plus markdown_strict/markdown_github/etc, none of which
// this project exposes) -- so the project's canonical "md" alias
// (NormalizeFormat's own alias table, and this project's own file-extension
// convention throughout jobs/storage/api) is translated here at the pandoc
// argv boundary. Every other curated format in pandocPairTable (rst, org,
// latex, html, docx, epub) already IS a valid pandoc format name verbatim
// and passes through unchanged.
func pandocFormatName(f string) string {
	if f == "md" {
		return "markdown"
	}
	return f
}

// pandocReaderSpec builds the `-f` reader-format spec for src from the
// PER-SOURCE reader-extension table (47-CONTEXT.md "Hardening mechanism
// (CORRECTED)", verified against pandoc 3.10.1): md/html/epub readers
// support disabling BOTH the raw_html and raw_tex extensions
// ("<pandoc-format>-raw_html-raw_tex"); the latex reader supports disabling
// ONLY raw_tex ("latex-raw_tex" -- it REJECTS a trailing "-raw_html"
// suffix, since raw_html is not one of its extensions); rst/org/docx
// readers expose NEITHER extension at all, so a blanket
// "-raw_html-raw_tex" suffix on any of those three ERRORS with "extension
// not supported" -- a bare `-f <pandoc-format>` is used for them instead
// (their readers carry no raw-passthrough extension to disable in the
// first place, so there is nothing to turn off). This function NEVER turns
// raw_html/raw_tex ON (pandoc's own leading-plus extension-enable syntax)
// -- only ever turns one off (leading minus), or passes the bare pandoc
// format name through with no extension suffix. src is this project's own
// normalized format token (pandocFormatName translates "md" -> "markdown"
// before the switch below decides the extension suffix).
func pandocReaderSpec(src string) string {
	name := pandocFormatName(src)
	switch src {
	case "md", "html", "epub":
		return name + "-raw_html-raw_tex"
	case "latex":
		return "latex-raw_tex"
	default: // rst, org, docx: no raw_html/raw_tex extension exists to disable
		return name
	}
}

// pandocArgsContains reports whether args already carries flag -- used to
// avoid appending a duplicate --standalone element when the caller's own
// PandocOpts already requested it (pandocArgs, pandocopts.go) AND the target
// independently requires it (pandocStructuralTargets).
func pandocArgsContains(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// pandocConvertArgs assembles pandoc's full discrete argv from
// already-validated, already-normalized inputs: the `-f` reader spec from
// the per-source table (pandocReaderSpec, never a blind concat), `-t tgt`,
// `--sandbox` (pandoc 2.19+'s own IO-restricting flag -- the documented
// defense against \input/include-file disclosure and the SSRF class, cf.
// CVE-2025-51591; this is NOT the TeX-engine's own shell-escape-suppressing
// flag, which pandoc's CLI rejects outright as an unknown option),
// `--standalone` when tgt is a structural target (and not already present
// via the opts fragment, so it is never duplicated), the already-validated
// PandocOpts argv fragment (pandocArgs, pandocopts.go, Plan 01), then the
// input path and `-o outPath`. No element here is ever a shell string or a
// raw client byte sequence -- the reader/writer format names come from the
// normalized (from, to) pair, never from client-controlled bytes; every
// PandocOpts value already passed the closed pandocWrapAllowlist/strict-JSON
// gate before this function ever sees it (pandocopts.go). No
// network-enabling flag is ever added -- pandoc runs offline by default and
// --sandbox additionally restricts reader/writer IO to the named files.
func pandocConvertArgs(src, tgt, inPath, outPath string, o PandocOpts) []string {
	args := []string{
		"-f", pandocReaderSpec(src),
		"-t", pandocFormatName(tgt),
		"--sandbox",
	}

	optArgs := pandocArgs(o)
	if pandocStructuralTargets[tgt] && !pandocArgsContains(optArgs, "--standalone") {
		args = append(args, "--standalone")
	}
	args = append(args, optArgs...)
	args = append(args, inPath, "-o", outPath)
	return args
}

// pandocValidateOutput validates a produced pandoc output against the
// target-appropriate sniffer (PDOC-02/SC2, markupsniff.go/htmlsniff.go/
// docsniff.go, Plan 02): html->LooksLikeHTML, latex->LooksLikeLaTeX,
// docx/epub->SniffContainer (matching container format), md/rst/org->
// LooksLikeText -- proving the output is a complete document (or valid
// container), not a bare/corrupt fragment. An unrecognized tgt is itself a
// validation failure rather than a silently-accepted output.
//
// An empty-but-valid output (e.g. an empty-body source producing an empty
// plaintext/fragment result) completes as `done`, not `failed` -- there is
// NO `if size == 0 { error }` branch here (D-04, 47-CONTEXT.md), mirroring
// validateOCROutput's own "no size check" rationale (ocr.go). This is not
// merely a style choice: every sniffer below (LooksLikeHTML/LooksLikeLaTeX/
// LooksLikeText) is ITSELF fail-closed on a zero-size input
// (readSniffPrefix, markupsniff.go) -- calling one against a genuinely
// empty pandoc output would misclassify a correct "nothing to render"
// result as a corrupt/bare-fragment failure. Skipping the sniffer call on
// size==0 is therefore required, not an optimization.
func pandocValidateOutput(tgt, outPath string) error {
	info, err := os.Stat(outPath)
	if err != nil {
		return fmt.Errorf("pandoc: stat output: %w", err)
	}

	if info.Size() == 0 {
		return nil
	}

	f, err := os.Open(outPath)
	if err != nil {
		return fmt.Errorf("pandoc: open output: %w", err)
	}
	defer f.Close()

	switch tgt {
	case "html":
		if !LooksLikeHTML(f, info.Size()) {
			return fmt.Errorf("pandoc: output failed html content validation")
		}
	case "latex":
		if !LooksLikeLaTeX(f, info.Size()) {
			return fmt.Errorf("pandoc: output failed latex content validation")
		}
	case "docx", "epub":
		res, err := SniffContainer(f, info.Size())
		if err != nil {
			return fmt.Errorf("pandoc: output failed container validation: %w", err)
		}
		if res.Format != tgt {
			return fmt.Errorf("pandoc: output container format %q, want %q", res.Format, tgt)
		}
	case "md", "rst", "org":
		if !LooksLikeText(f, info.Size()) {
			return fmt.Errorf("pandoc: output failed text content validation")
		}
	default:
		return fmt.Errorf("pandoc: no output validator for target %q", tgt)
	}
	return nil
}

// Convert runs `pandoc` directly against inPath, emitting the target format
// implied by outPath's extension. Preamble-then-work shape (mirrors
// OCRConverter.Convert/AudioConverter.Convert, ocr.go/whisper.go): normalize
// src/tgt, fail fast BEFORE any subprocess if the pair is not in the closed
// allowlist (PDOC-01 -- a caller bug must not burn a pandoc invocation
// before failing), parse/validate opts (a corrupt persisted opts value is a
// deterministic failure, CLAUDE.md "Error Handling"), fail-closed on an
// oversized input (enforcePandocInputSize, markupsniff.go, PDCE-03), run
// pandoc via the existing hardened runCommand (exec.go, byte-for-byte
// unchanged), then validate the produced output with the target-appropriate
// sniffer (PDOC-02/SC2) so a bare-fragment or corrupt output never silently
// passes as a complete document. An empty-but-valid output is `done`
// (pandocValidateOutput's own doc comment).
func (c PandocConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	src := NormalizeFormat(filepath.Ext(inPath))
	tgt := NormalizeFormat(filepath.Ext(outPath))
	if !pandocPairSupported(src, tgt) {
		return fmt.Errorf("pandoc: unsupported conversion %s -> %s", src, tgt)
	}

	o, err := PandocOptsFromMap(opts)
	if err != nil {
		return fmt.Errorf("pandoc: %w", err)
	}

	if err := enforcePandocInputSize(inPath, c.MaxInputBytes); err != nil {
		return fmt.Errorf("pandoc: %w", err)
	}

	args := pandocConvertArgs(src, tgt, inPath, outPath, o)
	if _, err := runCommand(ctx, "pandoc", args...); err != nil {
		return fmt.Errorf("pandoc: pandoc: %w", err)
	}

	return pandocValidateOutput(tgt, outPath)
}
