package convert

// This file (sanitizepdf.go): the PDF sanitize path -- a TWO-STEP round trip
// through PostScript, not a single pdfwrite pass.
//
// WHY TWO STEPS. THE NUMBERS, NOT THE ARGUMENT.
//
// A single `gs -sDEVICE=pdfwrite` pass looks equivalent, reads better, and is
// what most write-ups recommend. It is also measurably insufficient. Marker
// counts on internal/convert/testdata/sanitize/threat_all_classes.pdf, gs
// 10.07.1 (see that directory's PROVENANCE.md, which carries the commands):
//
//	marker                   input   one pdfwrite pass   via ps2write
//	/OpenAction                  1                   0              0
//	/AA                          1                   0              0
//	/Launch                      1                   0              0
//	/URI                         1                   0              0
//	evil.example                 1                   0              0
//	JavaScript                   3                   1              0   <- live /S /JavaScript survived
//	EmbeddedFile                 2                   2              0   <- attachment survived, both objects
//	Secret Internal Title        1                   1              0   <- migrated into XMP
//	jane.doe@corp.example        1                   1              0   <- migrated into XMP
//	xpacket                      0                   2              0   <- pdfwrite CREATED an XMP packet
//
// The last row is the sharpest one and the least expected: the input has no
// XMP packet at all, and a single pdfwrite pass MANUFACTURES one carrying the
// title and author it was invoked to remove. A sanitizer built on one pass
// would be introducing the metadata carrier it exists to strip.
//
// None of this is a bug in ghostscript. pdfwrite is a CONVERTER: it carries
// the catalog's /Names tree and document metadata forward deliberately.
// -dPreserveAnnots=false and -dPreserveMarkedContent=false do not change the
// surviving rows (60-RESEARCH.md R1).
//
// PostScript has no name trees, no actions, no attachments and no metadata
// dictionary. Going through it makes "what the interpreter did not draw does
// not exist" true by construction rather than by hope -- which is the whole
// content of 60-CONTEXT.md D-01.
//
// The numbers live here AND in a test (sanitizepdf_test.go's one-pass control)
// because a comment cannot fail. Phase 59 found a comment in ocrguards.go that
// had promised a route which never existed; nothing noticed for months. The
// comment explains what the test protects; the test is what stops the
// simplification.
//
// WHY -dOmitXMP RATHER THAN EMPTY XMP FIELDS (D-09)
//
// Ghostscript can be made to write an XMP packet with the document fields
// blanked. Measured, that residue still leaks pdf:Producer (the exact
// ghostscript version), xmp:CreateDate (with the SERVER's timezone offset),
// and xmpMM:DocumentID -- infrastructure facts riding along in every artifact
// the client goes on to distribute.
//
// The evidentiary argument matters more than the leak. "No xpacket appears in
// the output bytes" is a claim that cannot be subtly wrong. "Every XMP field
// is empty" requires enumerating the fields we thought to look for, and is
// wrong exactly in the cases we failed to anticipate -- which is the same
// shape as the pdfwrite pass above, correct about /Info and wrong about the
// file.
//
// WHY -dSAFER ON BOTH PASSES
//
// The second pass parses the intermediate PostScript, which is a derivative of
// untrusted input and is therefore untrusted itself. Relaxing -dSAFER there
// because "we generated that file" would be parsing attacker-influenced bytes
// with the interpreter's file and pipe operators enabled.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// pdfLostCapabilities are the document features the PostScript round trip
// destroys as a side effect. They are reported to the client as costs, not as
// removed threats (D-02) -- a fillable form is not an attack, and a client who
// needs one has to know before choosing this operation, not after the form
// stops working.
//
// This list is the honest price of the evidence: the same property that makes
// removal provable -- PostScript has no notion of these structures -- is what
// destroys them.
var pdfLostCapabilities = []LostCapability{
	LostFillableForms,
	LostBookmarks,
	LostTaggedStructure,
	LostDocumentAnnotations,
}

// pdfThreatClasses are the classes the PDF path removes -- every class in the
// vocabulary EXCEPT the two that are OOXML container constructs and cannot
// occur in a PDF at all (ThreatVBAMacro, ThreatOLEObject).
//
// Declared as its own list rather than derived by filtering AllThreatClasses()
// so that adding a class to the vocabulary does not silently add it to what
// this format claims to remove. A new class has to be considered per format,
// which is the point at which someone notices whether it applies.
var pdfThreatClasses = []ThreatClass{
	ThreatJavaScript,
	ThreatActionDict,
	ThreatLaunchAction,
	ThreatEmbeddedFile,
	ThreatExternalReference,
	ThreatDocumentMetadata,
}

// ps2writeArgs is the first pass: PDF -> intermediate PostScript.
//
// Split into its own function for the same reason ocrRasterizePageArgs is:
// the argument vector is the security-relevant part, and a test can assert on
// it directly instead of reaching into a running process.
func ps2writeArgs(inPath, psPath string) []string {
	return []string{
		"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER",
		"-sDEVICE=ps2write",
		"-sOutputFile=" + psPath,
		inPath,
	}
}

// pdfwriteArgs is the second pass: intermediate PostScript -> sanitized PDF.
func pdfwriteArgs(psPath, outPath string) []string {
	return []string{
		"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER",
		"-sDEVICE=pdfwrite",
		"-dOmitXMP",
		// Measured, not assumed: -dOmitInfoDate drops /CreationDate and
		// /ModDate, which otherwise record WHEN the sanitizer ran and make
		// the output non-deterministic. Adding -dOmitID alongside it silently
		// reverts that -- the dates come back -- so the two are not combined.
		"-dOmitInfoDate",
		// 1.4 predates object streams, so the sanitized output contains no
		// /ObjStm at all. Two things follow. Our own /Info lands as a plain
		// object, which is what makes the redaction below possible at all;
		// and a downstream byte scanner (phase 58's, or a client's) can
		// actually see our output, instead of being structurally blind to
		// half of it the way it is blind to a 1.5+ file.
		"-dCompatibilityLevel=1.4",
		"-sOutputFile=" + outPath,
		psPath,
	}
}

// sanitizePDF rebuilds inPath into outPath through PostScript.
//
// The intermediate lives in a server-owned temp dir removed on every return
// path; temp paths are never client-controlled. The overhead is roughly a
// constant ~170 KB rather than a multiple of the input (60-RESEARCH.md R4),
// measured on 1.9-36 KB inputs only -- Plan 60-06's timeout must come from a
// production-scale corpus, not from that range.
//
// Both passes go through runCommand, which puts each child in its own process
// group and kills the whole group on context cancellation. That matters more
// here than for a single-shot converter: a hung first pass leaving an orphan
// interpreter would hold the intermediate file open while this function's
// defer tries to remove the directory under it.
//
// Removed reports the classes this METHOD removes from a PDF -- pdfThreatClasses,
// not AllThreatClasses(). The difference is not tidiness: reporting vba_macro
// as removed from a PDF would be a false statement about a construct that
// cannot exist in the format, in a report whose entire value is that it claims
// only what it can support.
//
// It is also not a per-document finding list, and cannot be. Reconstruction
// does not "find" anything: it draws the page and discards everything it did
// not draw, so there is no per-class detection step whose result could be
// reported. What this states is what the method removes from this format; Plan
// 60-04's verifier is what turns it into a checked claim by confirming each
// class is absent from the output bytes.
func sanitizePDF(ctx context.Context, inPath, outPath string) (SanitizeResult, error) {
	tmpDir, err := os.MkdirTemp("", "octoconv-sanitize-pdf-*")
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize pdf: tmpdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	psPath := filepath.Join(tmpDir, "intermediate.ps")

	if _, err := runCommand(ctx, "gs", ps2writeArgs(inPath, psPath)...); err != nil {
		return SanitizeResult{}, classifyGSFailure("ps2write pass", err)
	}

	// A zero-byte intermediate means the first pass "succeeded" without
	// producing anything, and the second pass would then emit a valid, empty
	// PDF -- a green job handing the client a blank document. Checked rather
	// than assumed because gs exits 0 on some inputs it declines to render.
	info, err := os.Stat(psPath)
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize pdf: intermediate missing: %w", err)
	}
	if info.Size() == 0 {
		return SanitizeResult{}, fmt.Errorf("%w: ps2write produced an empty intermediate",
			ErrSanitizeMalformedInput)
	}

	if _, err := runCommand(ctx, "gs", pdfwriteArgs(psPath, outPath)...); err != nil {
		return SanitizeResult{}, classifyGSFailure("pdfwrite pass", err)
	}

	// Ghostscript signs its own output. See redactEngineIdentity's header for
	// what it writes, what was measured trying to stop it, and why blanking
	// in place is safe.
	produced, err := os.ReadFile(outPath)
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize pdf: read output: %w", err)
	}
	if err := os.WriteFile(outPath, redactEngineIdentity(produced), 0o644); err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize pdf: redact engine identity: %w", err)
	}

	return SanitizeResult{
		Removed:          append([]ThreatClass(nil), pdfThreatClasses...),
		LostCapabilities: append([]LostCapability(nil), pdfLostCapabilities...),
	}, nil
}

// classifyGSFailure separates the ONE ghostscript failure that depends on
// something other than the input from every other one.
//
// A timeout is about load: the box was busy, and the next attempt may not be.
// Everything else -- a parse error, a refusal, a non-zero exit on a file gs
// will not read -- is deterministic on the input and will reproduce exactly on
// every retry. Wrapping the second kind lets the worker mark the job failed
// immediately instead of burning its retry budget and then leaving it active
// for the reconciler to exhaust.
//
// The deadline check comes FIRST because runCommand wraps ctx.Err(), so a
// killed process carries context.DeadlineExceeded through the chain.
func classifyGSFailure(stage string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("sanitize pdf: %s: %w", stage, err)
	}
	return fmt.Errorf("%w: %s: %v", ErrSanitizeMalformedInput, stage, err)
}

// ---------------------------------------------------------------------------
// residual engine identity
// ---------------------------------------------------------------------------

// WHAT GHOSTSCRIPT WRITES ABOUT ITSELF, AND WHY IT HAD TO BE REDACTED
//
// This was not found by design. Plan 60-04's verifier -- pdfcpu, which
// decompresses /ObjStm and so sees what a byte grep cannot -- rejected the
// sanitized output on its very first run: /Info was present with four
// entries. Plan 60-02's tests had grepped for the INPUT's values ("Secret
// Internal Title", "jane.doe@corp.example"), all of which were gone, and were
// structurally incapable of seeing what Ghostscript had added in their place:
//
//	/Producer     (GPL Ghostscript 10.07.1)
//	/Creator      (GPL Ghostscript 10071 (ps2write))
//	/CreationDate (D:20260831221239+03'00')
//	/ModDate      (D:20260831221239+03'00')
//
// plus, in plaintext at the top of every output file, a `%%Invocation:` line
// carrying our full command line and flags.
//
// D-09 removed the XMP packet whole and named "does not leak the producer
// version" as one of its reasons. Leaving /Producer in place would contradict
// that decision in the same file it was made for.
//
// Three ways to make Ghostscript stop were measured, and two failed:
//
//	-dOmitInfoDate                 -> removes the two dates. Kept.
//	-dOmitInfoDate -dOmitID        -> the dates COME BACK. Not used.
//	[ /Producer () /DOCINFO pdfmark -> ignored for Producer and Creator, and
//	                                   ADDS empty /Author and /Title. Not used.
//
// So Ghostscript always signs its output, and the remaining two values are
// redacted here instead.
//
// LENGTH-PRESERVING ON PURPOSE. Every replacement writes exactly as many bytes
// as it removes, so the byte offsets recorded in the xref table stay correct
// and the file needs no rewriting. Shortening `(GPL Ghostscript 10.07.1)` to
// `()` would invalidate every offset after it; padding the difference with
// spaces INSIDE the dictionary is legal PDF and costs nothing. This is a
// redaction, not a PDF writer, and the distinction is the whole reason it is
// safe to do here.
//
// It works only because the second pass targets PDF 1.4: at 1.5+ Ghostscript
// packs /Info into a compressed /ObjStm where no byte-level edit is possible.

var (
	// producerKeys are the /Info entries Ghostscript writes about itself.
	producerKeys = []string{"/Producer", "/Creator"}
	// invocationMarker is the plaintext comment carrying our command line.
	invocationMarker = []byte("%%Invocation:")
)

// redactEngineIdentity blanks Ghostscript's self-description in place.
//
// Absence of any target is not an error: a different Ghostscript build may
// simply not write it. Corruption IS an error, and the caller does not have to
// detect it -- Plan 60-04's verifier re-parses the result with pdfcpu, so a
// redaction that broke the file fails the job rather than shipping it.
func redactEngineIdentity(raw []byte) []byte {
	out := append([]byte(nil), raw...)

	for _, key := range producerKeys {
		for i := 0; ; {
			j := bytes.Index(out[i:], []byte(key+"("))
			if j < 0 {
				break
			}
			start := i + j + len(key) // at the '('
			end := closingParen(out, start)
			if end < 0 {
				break
			}
			// out[start] == '(' and out[end] == ')'; write "()" then spaces.
			out[start] = '('
			out[start+1] = ')'
			for k := start + 2; k <= end; k++ {
				out[k] = ' '
			}
			i = end + 1
		}
	}

	// Blank the invocation comment's payload, keeping the leading "%" so the
	// line stays a comment and keeping every byte in place.
	for i := 0; ; {
		j := bytes.Index(out[i:], invocationMarker)
		if j < 0 {
			break
		}
		start := i + j
		end := bytes.IndexByte(out[start:], '\n')
		if end < 0 {
			end = len(out) - start
		}
		for k := start + 1; k < start+end; k++ {
			out[k] = ' '
		}
		i = start + end
	}

	return out
}

// closingParen finds the ')' matching the '(' at start, honouring PDF's
// backslash escapes and nested balanced parens (ISO 32000 7.3.4.2). A naive
// search for the next ')' would stop early on `(GPL Ghostscript 10071 \(ps2write\))`
// and leave half the string behind -- which is exactly the shape Ghostscript
// writes into /Creator.
func closingParen(b []byte, start int) int {
	if start >= len(b) || b[start] != '(' {
		return -1
	}
	depth := 0
	for i := start; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++ // skip the escaped byte
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
