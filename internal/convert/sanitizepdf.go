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
	redacted, err := redactEngineIdentity(produced)
	if err != nil {
		return SanitizeResult{}, err
	}
	if err := os.WriteFile(outPath, redacted, 0o644); err != nil {
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
// The context checks come FIRST because runCommand wraps ctx.Err(), so a
// killed process carries the cause through the chain.
//
// CANCELLATION IS THE SECOND ONE, and leaving it out was a regression this
// function introduced in the same change that added it. A job cancelled by the
// client kills ghostscript, and the resulting error wraps context.Canceled.
// Classifying that as malformed input meant the runner's isCanceled check
// stopped recognising it: the attempt was treated as a terminal bad upload,
// MarkFailed was attempted on a job already `canceled`, and the outcome was
// recorded as a failure the client never caused. Caught in review (PR #12,
// round 5).
//
// The %v in the malformed branch is deliberate and is why this ordering
// matters: it flattens the cause so nothing downstream mistakes an engine
// message for a context outcome. That flattening is exactly what destroyed the
// cancellation signal when the check above did not cover it.
func classifyGSFailure(stage string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
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
// pdfStreamRanges returns the byte ranges of every stream payload, derived
// from each stream's own /Length.
//
// Written here rather than shared with sanitizeverify.go's own scan: the two
// answer the same question for opposite purposes, and a single subtle byte
// walker feeding both would put one bug in front of the redaction AND the
// check that is supposed to catch it. TestStreamRangesAgreeWithTheVerifier
// holds them to the same answer on real output, so divergence is loud.
//
// WHY /Length AND NOT bytes.Index(b, "endstream"), WHICH IS SANI-08.
//
// A stream payload is arbitrary bytes. It may contain "endstream" -- and then
// the substring scan ends the range early, so this function reports a
// protected range that stops in the middle of a stream and the redaction above
// happily rewrites the rest of it.
//
// The keyword search had a second defect underneath that one: "stream" is a
// substring of "endstream", so after ending a range the walker re-entered on
// the tail of the keyword it had just consumed. On endstream_in_stream.pdf that
// made the file's REAL trailer look like stream payload. The fix for that is
// not a longer search string but a structural one: a stream keyword is only a
// stream keyword when it follows the ">>" that closes its own dictionary.
//
// Measured before it was written: Ghostscript emits only DIRECT /Length values
// on this project's corpus (4 streams across two files, 4 direct, 0 indirect).
// The indirect resolver below is therefore exercised by a hand-built fixture
// rather than by real output -- which is exactly why it exists, since "the gs
// build we happened to measure" is not a property to depend on.
//
// An unresolvable /Length is an error, never a fall back to the substring
// scan. A silent fallback would put the defect back exactly where it was and
// hide it behind a passing test (D-07 fail-closed).
func pdfStreamRanges(b []byte) ([][2]int, error) {
	objects := pdfObjectOffsets(b)

	var out [][2]int
	i := 0
	for {
		j := bytes.Index(b[i:], []byte("stream"))
		if j < 0 {
			return out, nil
		}
		kw := i + j
		dict, ok := pdfDictBefore(b, kw)
		if !ok {
			// Not a stream keyword: either the tail of "endstream" or the
			// word appearing inside content this scan has no business
			// interpreting. Step past it and keep looking.
			i = kw + 1
			continue
		}
		start := kw + len("stream")
		// ISO 32000-1 7.3.8.1: the keyword is followed by CRLF or LF.
		if start < len(b) && b[start] == '\r' {
			start++
		}
		if start < len(b) && b[start] == '\n' {
			start++
		}

		length, err := pdfLengthAt(b, dict, kw, objects)
		if err != nil {
			return nil, err
		}
		end := start + length
		if end > len(b) {
			return nil, fmt.Errorf("%w: stream at offset %d declares /Length %d, which runs past the end of the file",
				ErrSanitizeMalformedInput, kw, length)
		}
		if !pdfEndstreamFollows(b, end) {
			return nil, fmt.Errorf("%w: stream at offset %d declares /Length %d but no endstream follows it",
				ErrSanitizeMalformedInput, kw, length)
		}
		out = append(out, [2]int{start, end})
		i = end
	}
}

// pdfObjectOffsets indexes "N G obj" headers so an indirect /Length can be
// resolved. It is an index, not a parser: it records where each object starts
// and nothing about what is in it.
func pdfObjectOffsets(b []byte) map[int]int {
	offsets := make(map[int]int)
	for i := 0; ; {
		j := bytes.Index(b[i:], []byte(" obj"))
		if j < 0 {
			return offsets
		}
		at := i + j
		// Walk back over "G" and "N".
		k := at
		gen, ok := pdfIntBefore(b, k)
		if ok {
			k = gen.start
			if num, ok2 := pdfIntBefore(b, k); ok2 {
				offsets[num.value] = at + len(" obj")
			}
		}
		i = at + len(" obj")
	}
}

type pdfInt struct {
	value int
	start int
}

// pdfIntBefore reads the integer immediately preceding at, skipping one run of
// whitespace.
func pdfIntBefore(b []byte, at int) (pdfInt, bool) {
	i := at
	for i > 0 && isPDFSpace(b[i-1]) {
		i--
	}
	end := i
	for i > 0 && b[i-1] >= '0' && b[i-1] <= '9' {
		i--
	}
	if i == end {
		return pdfInt{}, false
	}
	v := 0
	for _, c := range b[i:end] {
		v = v*10 + int(c-'0')
		if v > 1<<30 {
			return pdfInt{}, false
		}
	}
	return pdfInt{value: v, start: i}, true
}

func isPDFSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

// pdfDictBefore returns the offset of the "<<" that opens the dictionary
// immediately preceding the stream keyword at kw, and whether there is one.
//
// This is what makes a stream keyword structural rather than lexical: the tail
// of "endstream" is not preceded by a closing ">>", so it is not mistaken for
// the start of a stream. Nesting is balanced, so a /Filter or /DecodeParms
// sub-dictionary does not end the search early.
func pdfDictBefore(b []byte, kw int) (int, bool) {
	i := kw
	for i > 0 && isPDFSpace(b[i-1]) {
		i--
	}
	if i < 2 || b[i-1] != '>' || b[i-2] != '>' {
		return 0, false
	}
	depth := 0
	for i >= 2 {
		switch {
		case b[i-1] == '>' && b[i-2] == '>':
			depth++
			i -= 2
		case b[i-1] == '<' && b[i-2] == '<':
			depth--
			if depth == 0 {
				return i - 2, true
			}
			i -= 2
		default:
			i--
		}
	}
	return 0, false
}

// pdfLengthAt reads the /Length entry of the dictionary starting at dict,
// resolving one level of indirection.
func pdfLengthAt(b []byte, dict, kw int, objects map[int]int) (int, error) {
	d := b[dict:kw]
	at := pdfTopLevelKey(d, "/Length")
	if at < 0 {
		return 0, fmt.Errorf("%w: stream at offset %d has no /Length in its dictionary",
			ErrSanitizeMalformedInput, kw)
	}
	rest := d[at+len("/Length"):]

	num, n, ok := pdfReadInt(rest)
	if !ok {
		return 0, fmt.Errorf("%w: stream at offset %d has a /Length that is not a number",
			ErrSanitizeMalformedInput, kw)
	}
	// Direct value, unless what follows is "G R".
	tail := rest[n:]
	if gen, gn, ok := pdfReadInt(tail); ok {
		_ = gen
		after := tail[gn:]
		k := 0
		for k < len(after) && isPDFSpace(after[k]) {
			k++
		}
		if k < len(after) && after[k] == 'R' {
			off, found := objects[num]
			if !found {
				return 0, fmt.Errorf("%w: stream at offset %d has /Length %d 0 R and object %d is not in the file",
					ErrSanitizeMalformedInput, kw, num, num)
			}
			v, _, ok := pdfReadInt(b[off:])
			if !ok {
				return 0, fmt.Errorf("%w: stream at offset %d resolves /Length to object %d, which is not an integer",
					ErrSanitizeMalformedInput, kw, num)
			}
			return v, nil
		}
	}
	return num, nil
}

// pdfTopLevelKey finds a key at nesting depth 1 of a dictionary slice, so a
// /Length inside a /DecodeParms sub-dictionary is not mistaken for the
// stream's own.
func pdfTopLevelKey(dict []byte, key string) int {
	depth := 0
	for i := 0; i+1 < len(dict); i++ {
		if dict[i] == '<' && dict[i+1] == '<' {
			depth++
			i++
			continue
		}
		if dict[i] == '>' && dict[i+1] == '>' {
			depth--
			i++
			continue
		}
		if depth == 1 && bytes.HasPrefix(dict[i:], []byte(key)) && pdfNameEndsAt(dict, i+len(key)) {
			return i
		}
	}
	return -1
}

// pdfNameEndsAt reports whether a PDF name token ends at i -- that is, whether
// the byte there is a delimiter or whitespace rather than another name
// character.
//
// Without this, "/Length" matches the first seven bytes of "/Length1", a key
// real Type1 font streams carry, and the value read is the "1" of the KEY. Found
// by embedded_font.pdf in this project's own corpus on the first run of the
// /Length derivation: "stream at offset 1607 declares /Length 1 but no
// endstream follows it". A name is a token, not a prefix -- the same distinction
// this phase is fixing three times over in other places.
func pdfNameEndsAt(b []byte, i int) bool {
	if i >= len(b) {
		return true
	}
	switch c := b[i]; c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return isPDFSpace(c)
	}
}

func pdfReadInt(b []byte) (int, int, bool) {
	i := 0
	for i < len(b) && isPDFSpace(b[i]) {
		i++
	}
	start := i
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	if i == start {
		return 0, 0, false
	}
	v := 0
	for _, c := range b[start:i] {
		v = v*10 + int(c-'0')
		if v > 1<<30 {
			return 0, 0, false
		}
	}
	return v, i, true
}

// pdfEndstreamFollows confirms the dictionary and the bytes agree: "endstream"
// must appear after at most a short run of whitespace. Disagreement is
// malformed input, not something to guess at.
func pdfEndstreamFollows(b []byte, end int) bool {
	i := end
	for i < len(b) && isPDFSpace(b[i]) {
		i++
	}
	return bytes.HasPrefix(b[i:], []byte("endstream"))
}

func inAnyRange(ranges [][2]int, i int) bool {
	for _, r := range ranges {
		if i >= r[0] && i < r[1] {
			return true
		}
	}
	return false
}

func redactEngineIdentity(raw []byte) ([]byte, error) {
	out := append([]byte(nil), raw...)

	// SKIP STREAM PAYLOADS. These are whole-file searches, and compressed page
	// or image data can contain "/Producer(" or "%%Invocation:" by chance. The
	// redaction would then rewrite compressed bytes -- preserving every xref
	// offset while destroying the stream they point at, which is the worst
	// available shape of corruption because nothing structural looks wrong.
	// The verifier's object walk does not decode every page stream, so it
	// would not necessarily catch it either. Caught in review (PR #12,
	// round 7); the same scoping the verifier's byte scan already had, applied
	// to the second of the two places that needed it.
	streams, err := pdfStreamRanges(out)
	if err != nil {
		return nil, err
	}

	for _, key := range producerKeys {
		for i := 0; ; {
			j := bytes.Index(out[i:], []byte(key+"("))
			if j < 0 {
				break
			}
			start := i + j + len(key) // at the '('
			if inAnyRange(streams, start) {
				i = start + 1
				continue
			}
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
		if inAnyRange(streams, start) {
			i = start + 1
			continue
		}
		end := bytes.IndexByte(out[start:], '\n')
		if end < 0 {
			end = len(out) - start
		}
		for k := start + 1; k < start+end; k++ {
			out[k] = ' '
		}
		i = start + end
	}

	return out, nil
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
