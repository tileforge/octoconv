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

// pandocReaderSpec builds the `-f` reader-format spec for src from the
// PER-SOURCE reader-extension table (47-CONTEXT.md "Hardening mechanism
// (CORRECTED)", verified against pandoc 3.10.1): md/html/epub readers
// support disabling BOTH the raw_html and raw_tex extensions
// ("<src>-raw_html-raw_tex"); the latex reader supports disabling ONLY
// raw_tex ("latex-raw_tex" -- it REJECTS a trailing "-raw_html" suffix,
// since raw_html is not one of its extensions); rst/org/docx readers expose
// NEITHER extension at all, so a blanket "-raw_html-raw_tex" suffix on any
// of those three ERRORS with "extension not supported" -- a bare `-f <src>`
// is used for them instead (their readers carry no raw-passthrough
// extension to disable in the first place, so there is nothing to turn
// off). This function NEVER turns raw_html/raw_tex ON (pandoc's own
// leading-plus extension-enable syntax) -- only ever turns one off (leading
// minus), or passes the bare format name through with no extension suffix.
func pandocReaderSpec(src string) string {
	switch src {
	case "md", "html", "epub":
		return src + "-raw_html-raw_tex"
	case "latex":
		return "latex-raw_tex"
	default: // rst, org, docx: no raw_html/raw_tex extension exists to disable
		return src
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
		"-t", tgt,
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

// Convert's Task 1 stub: the fast-reject guard ONLY -- normalize src/tgt and
// fail BEFORE any subprocess if the pair is not in the closed curated
// allowlist (PDOC-01). This is deliberately incomplete: opts parsing/
// validation, the input-size DoS guard, the real runCommand("pandoc", ...)
// dispatch, and output content validation all land in Task 2, in this same
// method body -- Task 1 exists only to prove (and unit-test) that an
// unlisted pair never reaches a subprocess, before any subprocess exists to
// reach.
func (c PandocConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	src := NormalizeFormat(filepath.Ext(inPath))
	tgt := NormalizeFormat(filepath.Ext(outPath))
	if !pandocPairSupported(src, tgt) {
		return fmt.Errorf("pandoc: unsupported conversion %s -> %s", src, tgt)
	}
	return nil
}
