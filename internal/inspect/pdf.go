// This file (pdf.go) is the single most consequential analyzer in Phase 58
// (D-02a). PDF 1.5+ lets objects live inside a compressed object stream
// (/ObjStm) instead of as a plain top-level object -- and ISO 32000 §7.5.7
// forbids only TWO things from living inside an /ObjStm: stream objects
// themselves, and the encryption dictionary. Every other dictionary,
// INCLUDING one carrying /JavaScript or /OpenAction, is legal there. A byte
// scan that never decompresses object streams therefore has a real,
// unavoidable false-negative rate on exactly the files this phase cares
// about most -- and "a large and growing share" of real-world PDFs use
// compressed object streams (58-RESEARCH.md Q1).
//
// The fix this file implements is NOT "parse harder": pdfcpu (the one
// library that could decompress object streams correctly) is deliberately
// NOT introduced here (D-01/D-02/D-02a) -- it is deferred to Phase 60's
// sanitize verifier, which runs in an isolated worker with a measured
// timeout and cgroup limits, nothing like the conditions this file runs
// under (inline, in the same process that serves /healthz, no cgroup
// backstop yet -- D-06b). Instead this file detects the COMPRESSION SIGNAL
// ITSELF -- a single cheap token scan for /ObjStm or /Type/XRef -- and uses
// its presence as the primary trigger that downgrades the entire PDF
// findings block to StatusUnknown with ReasonCompressedObjectStreams rather
// than ever answering StatusClean. That downgrade rule (verdictForCode
// below) is the load-bearing line in this whole phase: a plan that lets
// this file answer "clean" for a file whose content it structurally cannot
// see is wrong even if every other test is green.
//
// The inverse matters just as much and is easy to get backwards: a
// POSITIVE finding is never weakened by the same blindness. If /JavaScript
// is actually found in the bounded window, that is a real observation and
// stays a Finding regardless of whether /ObjStm is also present -- only the
// ABSENCE-claim (StatusClean) degrades, never a presence-claim.
package inspect

import (
	"bytes"
	"io"
	"sort"
	"strconv"
)

// pdfCountSearchWindow bounds how far past a "/Type/Pages" (or "/Type
// /Pages") token match extractPDFPageCount looks for an adjacent "/Count"
// key -- real PDF writers place /Count in the same small dictionary as
// /Type, so a few hundred bytes is generous; this is a search window, not
// an allocation, and it is clamped to the already-bounded head slice below.
const pdfCountSearchWindow = 512

// pdfPagesDictTokens are the two literal spellings of the Pages-dict type
// marker this file's fast page-count path recognizes -- "/Type/Pages" (no
// space) and "/Type /Pages" (one space). This is deliberately NOT a general
// whitespace-tolerant matcher: two fixed literal spellings cover what real
// PDF writers actually emit, and adding true arbitrary-whitespace tolerance
// would only widen the surface for very little real gain.
//
// What this file explicitly does NOT do, and why: it never counts
// occurrences of the SINGULAR "/Type /Page" token as a page-count fallback.
// Incremental updates (PDF's native "append a new revision" mechanism)
// leave every earlier revision's object bodies physically present in the
// file -- a naive byte count of "/Type /Page" therefore counts every
// REVISION of every page, not just the current, live set, and a PDF saved a
// few times by a real editor can report 2-5x the true page count
// (58-RESEARCH.md Pitfall 3). A plausible-looking but inflated integer is
// worse than an honest nil -- the same PROG-V2-02 discipline ("null is an
// honest answer, a guess is not") this project already applies to
// progress_pct/eta_seconds. So: no singular-Page counting fallback exists
// in this file at all; a Pages dict without a legible /Count nearby simply
// reports unknown.
var pdfPagesDictTokens = [][]byte{
	[]byte("/Type/Pages"),
	[]byte("/Type /Pages"),
}

// pdfCompressionSignalTokens are the literal tokens whose mere PRESENCE
// (not their content) proves this PDF uses compressed object streams and/or
// a compressed cross-reference stream -- the structural blindness signal
// D-02a exists to detect. "/Type/XRef" and "/Type /XRef" both name a
// cross-reference STREAM object (PDF 1.5+), which is itself only legal
// because object streams exist in this file at all; either token alongside
// /ObjStm is sufficient evidence.
var pdfCompressionSignalTokens = [][]byte{
	[]byte("/ObjStm"),
	[]byte("/Type/XRef"),
	[]byte("/Type /XRef"),
}

// pdfRiskToken pairs a Finding Code with every literal spelling that can
// trigger it. Multiple spellings collapsing onto ONE code (e.g.
// "/JavaScript" and its abbreviation "/JS" both mean CodePDFJavaScript) is
// deliberate: the vocabulary in findings.go is closed by MEANING, not by
// token spelling, so this file's own token table is allowed to be richer
// than the Code enum without creating new codes.
type pdfRiskToken struct {
	code   Code
	tokens [][]byte
}

// pdfRiskTokens is the complete, ordered set of PDF-domain risk tokens this
// file scans for. This is also the exhaustive list of Codes AnalyzePDF ever
// produces a Finding or Check for -- every code below appears in EXACTLY
// one Finding or Check per AnalyzePDF call (the ValidateReport union
// invariant, status.go), and no other family analyzer in this phase claims
// these particular codes for the PDF format (CodeExternalReference and
// CodeMetadataPresent are ALSO claimed by container.go/image.go for their
// own formats -- one code, several sources, is exactly what the closed
// vocabulary in findings.go exists to allow, D-03).
var pdfRiskTokens = []pdfRiskToken{
	{CodePDFJavaScript, [][]byte{[]byte("/JavaScript"), []byte("/JS")}},
	{CodePDFActionDict, [][]byte{[]byte("/OpenAction"), []byte("/AA")}},
	{CodePDFLaunchAction, [][]byte{[]byte("/Launch")}},
	{CodePDFEmbeddedFile, [][]byte{[]byte("/EmbeddedFile")}},
	{CodeExternalReference, [][]byte{[]byte("/URI")}},
	{CodePDFEncrypted, [][]byte{[]byte("/Encrypt")}},
	{CodeMetadataPresent, [][]byte{[]byte("/Info"), []byte("/Metadata")}},
}

// AnalyzePDF inspects r (size bytes long) for the closed set of PDF-domain
// finding codes plus the page_count/pdf_version/uses_compressed_object_streams
// characteristics, spending at most maxBytes IN TOTAL: for a file larger than
// maxBytes the allowance is SPLIT between a head window and one tail window
// (the trailer is conventionally at the tail), never spent twice.
//
// PR9R23 (pdf.go:172), verbatim: "For any PDF larger than maxBytes, the head
// read has already consumed the full allowance, but this performs another tail
// read of up to 64 KiB. dispatchInspectFamilies subsequently charges only
// maxBytes, so the operation can exceed its configured shared byte ceiling
// while accounting still reports that ceiling as enforced; with a small custom
// ceiling, the overshoot can be many times the configured limit."
//
// The split mirrors media.go's own findMoov fix for the identical defect,
// including the ratio: at most a quarter of the allowance is reserved for the
// tail, so the head -- where a PDF's header, and most of its risk tokens, live
// -- keeps the bulk of it. The caller charges maxBytes exactly once
// (dispatchInspectFamilies), and after this split that charge is the truth
// rather than an underestimate.
//
// It never returns an error: any parse failure is expressed as an
// unknown-status Check, not a Go error -- D-01's "unknown is a correct
// answer, not a failure" applies here exactly as it does everywhere else in
// this package.
//
// Every byte this function reads comes from readWindow (window.go) -- the
// one bounded-ReadAt primitive this package allows -- and every scanning
// loop that walks MULTIPLE matches (not just "does this token appear once")
// is capped by maxTokenScanIterations, never by a context deadline: a
// CPU-bound loop that never yields to the scheduler is not interrupted by
// context.WithTimeout (window.go's own doc comment; 58-RESEARCH.md Q4). A
// PDF that repeats a short token millions of times is cheap to construct
// and would otherwise turn a "cheap" scan into real, attacker-controlled
// CPU work.
func AnalyzePDF(r io.ReaderAt, size int64, maxBytes int64, tr *Tracker) FamilyResult {
	res := FamilyResult{Characteristics: Characteristics{}}

	if size <= 0 || maxBytes <= 0 {
		return malformedPDFResult(res)
	}

	// The allowance is carved up BEFORE the first read, so no window can be
	// sized against bytes another window has already spent (PR9R23).
	// tailReserve is zero unless a tail read will actually happen, i.e. unless
	// the file is larger than the allowance -- when it fits, the head window
	// covers it end to end and the tail would only re-read bytes in hand.
	headLen := maxBytes
	var tailReserve int64
	if size > maxBytes {
		tailReserve = maxBytes / 4
		if tailReserve > defaultTailWindow {
			tailReserve = defaultTailWindow
		}
		headLen = maxBytes - tailReserve
	}
	if size < headLen {
		headLen = size
	}
	head, headOK := readWindow(r, 0, headLen, size)
	if !headOK {
		// readWindow only reports failure on a genuine, non-EOF read error
		// (size and maxBytes were already validated above), so this is an
		// operational failure to READ the upload, not a discovery about its
		// structure. Round 6's own neighbour audit found this site labeling
		// it ReasonMalformedInput -- never clean, so not the severe half of
		// the class, but still a claim about the FILE made on the strength
		// of our own inability to read it. The two are different facts and
		// carry different Reason values (status.go's own vocabulary note).
		return unknownPDFResult(res, ReasonReadFailed)
	}

	// truncated is the source of ReasonWorkCeilingBytes below -- OUR OWN
	// byte budget, not a fact about the file. When the file fits entirely
	// within maxBytes, head already covers it end-to-end and a second,
	// tail-anchored read would just re-read bytes already in hand.
	truncated := size > maxBytes
	var tail []byte
	var tailOK bool
	var tailOff int64
	if truncated && tailReserve > 0 {
		// tailReserve, not defaultTailWindow: the window this read is allowed
		// is what was carved out for it above, which is the whole point of the
		// split. A tiny configured ceiling therefore yields a tiny tail window
		// rather than a 64 KiB read the budget never accounted for.
		tailOff = size - tailReserve
		if tailOff < 0 {
			tailOff = 0
		}
		tail, tailOK = readWindow(r, tailOff, tailReserve, size)
	}

	// STEP 1 (D-02a) -- the compression-signal detector runs FIRST, before
	// any risk-token verdict is decided, because its result gates whether
	// this whole block is ever allowed to say "clean". See the file-level
	// doc comment above for the full ISO 32000 §7.5.7 rationale.
	compressed := containsCompressionSignal(head) || (tailOK && containsCompressionSignal(tail))
	res.Characteristics["uses_compressed_object_streams"] = compressed

	// STEP 1b (codex PR9 finding 1 / T-SLE-01) -- PDF names permit
	// hexadecimal escapes ("#XX"), so a dictionary can spell /JavaScript as
	// /Java#53cript. This scanner recognizes only literal byte spellings
	// (pdfRiskTokens above), so a name using an escape would otherwise be
	// invisible to it -- option (б) from 58-CONTEXT.md's own discipline
	// (bounded, no new parsing): rather than writing a PDF name-escape
	// decoder, ANY "#" byte in a position syntactically possible inside a
	// name (immediately after an unterminated "/" token, before the next
	// PDF delimiter/whitespace) downgrades this whole block to unknown,
	// exactly like /ObjStm does. This is deliberately a DIFFERENT Reason
	// from ReasonCompressedObjectStreams: it is a different blind spot (an
	// escape this scanner chose not to decode, not a compressed object
	// stream this scanner cannot see into at all), and collapsing the two
	// would rob a future reader of the ability to tell which one fired.
	hexEscape := containsPDFNameHexEscape(head) || (tailOK && containsPDFNameHexEscape(tail))

	if v, ok := extractPDFVersion(head); ok {
		res.Characteristics["pdf_version"] = v
	}

	// headOpaqueCeilingHit/tailOpaqueCeilingHit (quick/260811-r3s, pdf.go:543
	// finding): scanPDFOpaqueRanges's own three sub-scans are EACH capped by
	// maxTokenScanIterations (their own doc comments). When one of them hits
	// that cap, the returned range SET itself is incomplete -- this corrupts
	// the INPUT to scanTokenOccurrences below, not merely this function's own
	// completeness claim: a risk-token literal that scanTokenOccurrences
	// reports as "found" outside every KNOWN opaque range might actually sit
	// inside a literal/hex string past the 4096th delimiter this scan never
	// reached, which is exactly the false-positive class the opaque-range
	// filter was built to close (codex PR9 round 2). A filter that ran on an
	// incomplete range set has no right to confirm a Finding OR declare
	// clean for ANY code -- both directions are handled below: a "found"
	// match gets downgraded to unknown right where it would otherwise become
	// a Finding, and the ordinary verdict rule (verdictStatus/verdictReason)
	// gains this as a fifth gate so a "no match" answer cannot fall through
	// to StatusClean either.
	headOpaque, headOpaqueCeilingHit := scanPDFOpaqueRanges(head, tr)
	var tailOpaque []pdfOpaqueRange
	var tailOpaqueCeilingHit bool
	if tailOK {
		tailOpaque, tailOpaqueCeilingHit = scanPDFOpaqueRanges(tail, tr)
	}

	// STEP 2/3/4: token-scan every risk code, then apply the single verdict
	// rule (verdictForCode) uniformly to every one of them.
	for _, rt := range pdfRiskTokens {
		// scanTokenOccurrences is the single primitive that now does
		// EVERYTHING firstTokenLocation/firstTokenLocationOutsideOpaqueRanges
		// used to do separately (codex PR9 round 2, finding pdf.go:515/520):
		// it rejects a match whose PDF-name boundary is not real (so "/JS"
		// inside "/JScript" is not a match at all, not merely an ambiguous
		// one), classifies a boundary-valid match inside an opaque range as
		// "filtered" rather than "found", and reports ceilingHit when its own
		// per-token occurrence cap -- not the buffer's content -- decided
		// when the walk stopped. See its own doc comment for why a single
		// function replaces what used to be two.
		pos, found, filtered, ceilingHit := scanTokenOccurrences(head, rt.tokens, headOpaque, tr)
		loc := pos
		foundOpaqueCeilingHit := headOpaqueCeilingHit
		if !found && rt.code == CodePDFEncrypted && tailOK {
			// /Encrypt lives in the trailer or the XRef-stream dict, which
			// is conventionally at the file's tail -- scan there too for
			// this one code (58-RESEARCH.md Q1).
			if tpos, tfound, tfiltered, tceilingHit := scanTokenOccurrences(tail, rt.tokens, tailOpaque, tr); tfound {
				found = true
				loc = int(tailOff) + tpos
				foundOpaqueCeilingHit = tailOpaqueCeilingHit
			} else {
				filtered = filtered || tfiltered
				ceilingHit = ceilingHit || tceilingHit
			}
		}

		if found {
			if foundOpaqueCeilingHit {
				// pdf.go:543 (quick/260811-r3s): this occurrence was found
				// OUTSIDE every opaque range this scan actually completed --
				// but the range scan itself was truncated by its own
				// iteration cap, so "outside every known range" is not the
				// same claim as "outside every range that actually exists".
				// Confirming a Finding on that basis would repeat the exact
				// false-positive class the literal/hex-string filter exists
				// to close.
				res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: ReasonWorkCeilingBytes})
				continue
			}
			sev, _ := SeverityOf(rt.code)
			res.Findings = append(res.Findings, Finding{
				Code:     rt.code,
				Severity: sev,
				Location: strconv.Itoa(loc),
			})
			continue
		}

		if ceilingHit {
			// codex PR9 round 2 (pdf.go:520): exhausting THIS SCAN'S OWN
			// occurrence cap -- not the file's structure -- decided the
			// answer here. That is OUR ceiling, exactly like a byte-window
			// truncation, and must carry the same Reason so report.go's
			// Coverage accounting (status.go's MarkTruncated) sees it: a
			// scan that gave up because of its own budget must mark the
			// report selective, never silently agree with "clean".
			res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: ReasonWorkCeilingBytes})
			continue
		}

		if filtered {
			// matchedInsideOpaque (this scan's own former name for this
			// case): the literal occurs SOMEWHERE in the scanned bytes, but
			// only inside a stream body, a comment, or a literal/hex string
			// (codex PR9 finding 2 / T-SLE-02, extended in round 2 to
			// literal and hex strings). The lexical filter above correctly
			// refuses to promote that occurrence to a Finding, but it must
			// NEVER be allowed to fall through to the ordinary verdict rule
			// below either -- that rule would report StatusClean for "no
			// match found outside opaque ranges", which is a false claim of
			// absence when a match exists, just one this scanner cannot
			// verify.
			res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: ReasonPDFStreamBodyToken})
			continue
		}

		// The verdict rule -- the single place the honesty of this whole
		// phase lives. Applied identically to EVERY code, including
		// CodePDFEncrypted even though ISO 32000 §7.5.7 forbids the
		// encryption dictionary from living inside an /ObjStm (so a naive
		// reading might argue /Encrypt should be exempt from the
		// downgrade). This project's own research verified that
		// restriction via qpdf's documentation, not a direct read of ISO
		// 32000 itself (58-RESEARCH.md Assumptions Log A5) -- a
		// MEDIUM-HIGH, not HIGH, confidence claim. The conservative
		// direction on a MEDIUM-HIGH-confidence claim is to downgrade
		// anyway, not to carve out an exception: if A5 turns out to be
		// wrong, pdf_encrypted would need the exact same /ObjStm blind
		// spot as every other PDF finding, and this uniform rule already
		// has it covered. A future reader who "optimizes" this exception
		// back in on the assumption that A5 is certain would be trading a
		// safe redundancy for an unverified one.
		opaqueRangesCeilingHit := headOpaqueCeilingHit || (tailOK && tailOpaqueCeilingHit)
		res.Checks = append(res.Checks, Check{
			Code:   rt.code,
			Status: verdictStatus(compressed, truncated, hexEscape, opaqueRangesCeilingHit),
			Reason: verdictReason(compressed, truncated, hexEscape, opaqueRangesCeilingHit),
		})
	}

	count, reason := extractPDFPageCount(head, compressed, truncated, hexEscape, tr)
	if count != nil {
		res.Characteristics["page_count"] = *count
	} else {
		res.Characteristics["page_count"] = nil
		if reason == "" {
			reason = ReasonMalformedInput
		}
		res.Characteristics["page_count_reason"] = string(reason)
	}

	return res
}

// verdictStatus and verdictReason implement, together, the ONE verdict rule
// every PDF risk code (excluding an actual Finding, handled before this is
// ever called) is subject to:
//
//   - compression signal present -> StatusUnknown / ReasonCompressedObjectStreams
//   - no compression signal, but a hex-escaped name was seen -> StatusUnknown / ReasonPDFNameEscape
//   - neither of the above, but OUR OWN byte ceiling truncated the window -> StatusUnknown / ReasonWorkCeilingBytes
//   - none of the above, but the opaque-range scan itself was truncated
//     (quick/260811-r3s, pdf.go:543) -> StatusUnknown / ReasonWorkCeilingBytes
//   - none of the above -> StatusClean
//
// opaqueRangesCeilingHit is deliberately the LAST gate, not folded into
// truncated: truncated already means "OUR byte window did not cover the
// file", a coarser and unconditionally-true-when-set signal, whereas
// opaqueRangesCeilingHit can be true even on an UNtruncated (fully-buffered)
// window whose literal/hex-string filter alone gave up early -- keeping
// them as separate named parameters, rather than pre-OR'ing them at the
// call site, keeps each one's own doc comment attached to what it actually
// means.
//
// Split into two functions purely so the call site above reads as "status,
// reason" rather than duplicating the same five-way branch twice.
func verdictStatus(compressed, truncated, hexEscape, opaqueRangesCeilingHit bool) CheckStatus {
	if compressed || truncated || hexEscape || opaqueRangesCeilingHit {
		return StatusUnknown
	}
	return StatusClean
}

func verdictReason(compressed, truncated, hexEscape, opaqueRangesCeilingHit bool) Reason {
	switch {
	case compressed:
		return ReasonCompressedObjectStreams
	case hexEscape:
		return ReasonPDFNameEscape
	case truncated:
		return ReasonWorkCeilingBytes
	case opaqueRangesCeilingHit:
		return ReasonWorkCeilingBytes
	default:
		return ""
	}
}

// malformedPDFResult builds the FamilyResult for every path that never got
// a readable head window at all (size<=0, maxBytes<=0, or readWindow itself
// failed) -- every PDF-domain code becomes StatusUnknown/ReasonMalformedInput,
// page_count is nil with the same reason, and
// uses_compressed_object_streams is reported false: not because compression
// is positively ruled out, but because zero bytes were ever read to look
// for it, and false is the only claim this function can make truthfully
// about zero evidence.
func malformedPDFResult(res FamilyResult) FamilyResult {
	return unknownPDFResult(res, ReasonMalformedInput)
}

// unknownPDFResult is malformedPDFResult generalized over WHY nothing could
// be determined. The shape is identical -- every PDF-domain code becomes an
// honest unknown, never clean -- but the reason is the caller's to choose,
// because "this file is malformed" and "we could not read this file" are
// different claims and only one of them is about the file.
func unknownPDFResult(res FamilyResult, reason Reason) FamilyResult {
	res.Characteristics["uses_compressed_object_streams"] = false
	res.Characteristics["page_count"] = nil
	res.Characteristics["page_count_reason"] = string(reason)
	for _, rt := range pdfRiskTokens {
		res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: reason})
	}
	return res
}

// containsCompressionSignal reports whether ANY of pdfCompressionSignalTokens
// appears anywhere in buf. A single bytes.Contains call per token is
// sufficient here -- this is a presence check, not a multi-occurrence walk,
// so it needs no iteration ceiling of its own (the loop runs at most
// len(pdfCompressionSignalTokens) times, a small fixed constant).
func containsCompressionSignal(buf []byte) bool {
	for _, tok := range pdfCompressionSignalTokens {
		if bytes.Contains(buf, tok) {
			return true
		}
	}
	return false
}

// isPDFNameByte reports whether b can appear inside a PDF name token (ISO
// 32000 §7.3.5): anything except whitespace and the eight delimiter
// characters. A name STARTS at "/" and runs until the first byte for which
// this returns false -- that is the boundary this file uses to decide
// whether a "#" sits in a position syntactically possible inside a name.
func isPDFNameByte(b byte) bool {
	switch b {
	case 0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20, // whitespace (ISO 32000 Table 1)
		'(', ')', '<', '>', '[', ']', '{', '}', '/', '%': // delimiters
		return false
	}
	return true
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'A' && b <= 'F') || (b >= 'a' && b <= 'f')
}

// containsPDFNameHexEscape reports whether buf contains a "/" immediately
// followed (within the same unbroken run of name bytes) by a "#" spelled
// with two hex digits -- codex PR9 finding 1 / T-SLE-01. This is
// deliberately NOT a name-escape decoder: it is the cheaper, conservative
// alternative 58-CONTEXT.md's own D-01/D-02 discipline prefers (option (б)
// in the plan) -- treat the mere POSSIBILITY of an escaped name as reason
// enough to stop trusting a literal-byte token match, the same way
// containsCompressionSignal treats the mere presence of /ObjStm as reason
// enough to stop trusting an absence claim.
//
// This is a single linear pass over buf: the outer loop only ever restarts
// its inner scan from the position where the previous name token ended, so
// total work is bounded by len(buf) regardless of how many "/" bytes the
// buffer contains -- no separate iteration ceiling is needed (a genuinely
// monotonic single pass, unlike scanTokenOccurrences below, which walks
// occurrences of a FIXED token and therefore does need one).
func containsPDFNameHexEscape(buf []byte) bool {
	for i := 0; i < len(buf); i++ {
		if buf[i] != '/' {
			continue
		}
		j := i + 1
		for j < len(buf) && isPDFNameByte(buf[j]) {
			if buf[j] == '#' && j+2 < len(buf) && isHexDigit(buf[j+1]) && isHexDigit(buf[j+2]) {
				return true
			}
			j++
		}
		i = j - 1 // resume right after this name token; the outer i++ advances past it
	}
	return false
}

// pdfOpaqueRange marks a [start, end) byte range within a scanned buffer
// whose content this scanner does not attempt to interpret as PDF syntax --
// a stream body (between the "stream" keyword and "endstream"), a comment
// (from "%" to the end of its line), or a literal "(...)"/hex "<...>" string
// (codex PR9 round 2, finding pdf.go:515: a dictionary key like /JScript
// must not match /JS, and neither must a literal string such as
// "(documentation says /JS)"). A risk-token literal that occurs ONLY inside
// one of these ranges is not evidence either way (codex PR9 finding 2 /
// T-SLE-02): it could be a coincidental byte sequence inside
// compressed/opaque payload data (far more likely for a short token like
// "/JS" or "/AA"), or inside string DATA rather than PDF syntax, or it could
// genuinely be a PDF name -- this scanner cannot tell, and the correct
// response to "cannot tell" is never to fall back to StatusClean (see the
// "filtered" handling in AnalyzePDF).
type pdfOpaqueRange struct {
	start, end int
}

// scanPDFOpaqueRanges finds every stream-body, comment, and literal/hex
// string range in buf, then merges and sorts them (mergeOpaqueRanges) so
// inOpaqueRange can binary-search instead of linear-scanning -- codex PR9
// round 2's performance finding (pdf.go:520): the ORIGINAL per-occurrence
// linear scan over every range made scanning a token whose occurrences
// number in the millions (trivial to construct: repeat a 3-byte token
// inside one giant, otherwise-legitimate stream) cost occurrences times
// ranges comparisons -- billions of comparisons for a bounded, 8 MiB input,
// and neither the tracker's time budget nor maxTokenScanIterations
// interrupts a tight loop that never checks either (window.go's own "why
// iterations, not context" note). Sorting once per scanPDFOpaqueRanges call
// (called at most twice per AnalyzePDF call: head and tail) and then doing
// O(log ranges) lookups per occurrence removes the multiplier this file
// itself introduced.
//
// All three sub-scans intentionally OVER-approximate rather than
// under-approximate: a bare substring match of "stream" (not anchored to a
// preceding newline), treating the "%PDF-"/"%%EOF" markers as ordinary
// comments, or marking a "(" that is actually inside an already-opaque
// stream body as ALSO the start of a literal string, can all mark a byte
// range as opaque when a stricter parser would not. That direction is safe
// here -- opaque ranges only ever SUPPRESS a Finding in favor of
// StatusUnknown, never promote a Check to StatusClean, so over-marking (and
// the merge step below, which only ever WIDENS a range by combining
// overlaps) costs at most a missed positive Finding, never a false absence
// claim.
// scanPDFOpaqueRanges's ceilingHit return (quick/260811-r3s, pdf.go:543
// finding) is the OR of its three sub-scans' own ceilingHit -- if ANY of
// them stopped early because of maxTokenScanIterations, the combined range
// SET this function returns is incomplete, and AnalyzePDF's caller-facing
// verdict rule (verdictStatus/verdictReason) must know that, not just this
// function's own local state.
func scanPDFOpaqueRanges(buf []byte, tr *Tracker) ([]pdfOpaqueRange, bool) {
	var ranges []pdfOpaqueRange
	streamRanges, streamCeilingHit := scanPDFStreamRanges(buf, tr)
	commentRanges, commentCeilingHit := scanPDFCommentRanges(buf, tr)
	stringRanges, stringCeilingHit := scanPDFLiteralAndHexStringRanges(buf, tr)
	ranges = append(ranges, streamRanges...)
	ranges = append(ranges, commentRanges...)
	ranges = append(ranges, stringRanges...)
	return mergeOpaqueRanges(ranges), streamCeilingHit || commentCeilingHit || stringCeilingHit
}

// scanPDFStreamRanges finds every "stream"..."endstream" body in buf,
// capped by maxTokenScanIterations: an adversarial PDF could repeat
// "stream"/"endstream" pairs without bound (window.go's "why iterations,
// not context" note). ceilingHit reports whether that cap -- not the
// buffer's own content -- decided when this walk stopped; tr.MarkCeilingHit
// is called at the exact point that happens (quick/260811-r3s: the
// receptacle, not a return value a caller could drop, is what makes this
// fact reach Coverage unconditionally).
func scanPDFStreamRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool) {
	pos := 0
	for iter := 0; ; iter++ {
		if iter >= maxTokenScanIterations {
			ceilingHit = true
			tr.MarkCeilingHit()
			break
		}
		idx := bytes.Index(buf[pos:], []byte("stream"))
		if idx < 0 {
			break
		}
		start := pos + idx + len("stream")
		endIdx := bytes.Index(buf[start:], []byte("endstream"))
		if endIdx < 0 {
			// No matching "endstream" within the scanned window at all --
			// treat everything from here to the end of buf as opaque
			// (the stream may simply continue past this window).
			ranges = append(ranges, pdfOpaqueRange{start: start, end: len(buf)})
			break
		}
		end := start + endIdx
		ranges = append(ranges, pdfOpaqueRange{start: start, end: end})
		pos = end + len("endstream")
		if pos >= len(buf) {
			break
		}
	}
	return ranges, ceilingHit
}

// scanPDFCommentRanges finds every "%"-to-end-of-line comment range in buf,
// capped by maxTokenScanIterations for the same reason scanPDFStreamRanges
// is; ceilingHit/tr.MarkCeilingHit mirror scanPDFStreamRanges's own.
func scanPDFCommentRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool) {
	pos := 0
	for iter := 0; ; iter++ {
		if iter >= maxTokenScanIterations {
			ceilingHit = true
			tr.MarkCeilingHit()
			break
		}
		idx := bytes.IndexByte(buf[pos:], '%')
		if idx < 0 {
			break
		}
		start := pos + idx
		end := start
		for end < len(buf) && buf[end] != '\n' && buf[end] != '\r' {
			end++
		}
		ranges = append(ranges, pdfOpaqueRange{start: start, end: end})
		pos = end
		if pos >= len(buf) {
			break
		}
	}
	return ranges, ceilingHit
}

// scanPDFLiteralAndHexStringRanges finds every PDF literal "(...)" string
// (ISO 32000 §7.3.4.2, honoring "\(" / "\)" escapes and balanced unescaped
// nesting) and every hex "<...>" string (§7.3.4.3) in buf -- codex PR9
// round 2's own worked example, "(documentation says /JS)", is exactly a
// literal string whose DATA happens to contain a risk-token spelling. "<<"
// is explicitly excluded: it opens a PDF dictionary, not a hex string, and
// treating it as one would wrongly mark an entire dictionary body opaque.
//
// The outer loop advances via bytes.IndexByte jumps between candidate
// delimiters (not one byte at a time), so its iteration count is bounded by
// the number of "(" / "<" bytes actually found, not by len(buf) -- capped by
// maxTokenScanIterations for the same adversarial-repetition reason every
// other multi-match loop in this file is. ceilingHit/tr.MarkCeilingHit
// mirror scanPDFStreamRanges's own -- this is the sub-scan quick/260811-r3s's
// pdf.go:543 finding names directly (4096 harmless delimiters followed by a
// risk token inside the 4097th string).
func scanPDFLiteralAndHexStringRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool) {
	pos := 0
	for iter := 0; ; iter++ {
		if iter >= maxTokenScanIterations {
			ceilingHit = true
			tr.MarkCeilingHit()
			return ranges, ceilingHit
		}
		idxParen := bytes.IndexByte(buf[pos:], '(')
		idxAngle := bytes.IndexByte(buf[pos:], '<')

		var next int
		isParen := false
		switch {
		case idxParen < 0 && idxAngle < 0:
			return ranges, ceilingHit
		case idxParen < 0:
			next, isParen = idxAngle, false
		case idxAngle < 0:
			next, isParen = idxParen, true
		case idxParen < idxAngle:
			next, isParen = idxParen, true
		default:
			next, isParen = idxAngle, false
		}
		start := pos + next

		if isParen {
			depth := 1
			j := start + 1
			for j < len(buf) && depth > 0 {
				if buf[j] == '\\' {
					j += 2
					continue
				}
				if buf[j] == '(' {
					depth++
				} else if buf[j] == ')' {
					depth--
				}
				j++
			}
			ranges = append(ranges, pdfOpaqueRange{start: start, end: j})
			pos = j
		} else {
			if start+1 < len(buf) && buf[start+1] == '<' {
				// "<<" opens a dictionary, not a hex string -- skip both
				// bytes and keep scanning from there.
				pos = start + 2
				if pos >= len(buf) {
					return ranges, ceilingHit
				}
				continue
			}
			closeIdx := bytes.IndexByte(buf[start+1:], '>')
			if closeIdx < 0 {
				// No closing ">" within the scanned window -- treat the
				// rest of buf as opaque, the same over-approximation
				// scanPDFStreamRanges applies to an unterminated stream.
				ranges = append(ranges, pdfOpaqueRange{start: start, end: len(buf)})
				return ranges, ceilingHit
			}
			end := start + 1 + closeIdx + 1
			ranges = append(ranges, pdfOpaqueRange{start: start, end: end})
			pos = end
		}
		if pos >= len(buf) {
			break
		}
	}
	return ranges, ceilingHit
}

// mergeOpaqueRanges sorts ranges by start and merges any that overlap or
// touch, so inOpaqueRange's binary search can trust that ranges never
// overlap (a point can be inside at most one merged range) and are in
// ascending order. Merging only ever WIDENS coverage -- safe per
// scanPDFOpaqueRanges's own doc comment on over-approximation.
func mergeOpaqueRanges(ranges []pdfOpaqueRange) []pdfOpaqueRange {
	if len(ranges) < 2 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	merged := ranges[:1]
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.start <= last.end {
			if r.end > last.end {
				last.end = r.end
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

// inOpaqueRange reports whether pos falls inside any of ranges via binary
// search -- codex PR9 round 2's performance finding (pdf.go:520): ranges
// MUST already be sorted and non-overlapping (mergeOpaqueRanges's
// postcondition) for this to be correct; a linear scan here, repeated once
// per occurrence of a token that can repeat millions of times in an
// attacker-controlled stream, is exactly the quadratic blowup this fix
// closes.
func inOpaqueRange(pos int, ranges []pdfOpaqueRange) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].start > pos })
	if i == 0 {
		return false
	}
	return pos < ranges[i-1].end
}

// scanTokenOccurrences is the single primitive AnalyzePDF's token loop uses
// for every risk code -- it replaces what used to be two separate functions
// (firstTokenLocation and firstTokenLocationOutsideOpaqueRanges) because
// codex PR9 round 2 (pdf.go:515) requires the SAME lexical-boundary check in
// both: a match is only real if the byte immediately following it is a PDF
// delimiter/whitespace or EOF (ISO 32000 §7.3.5 -- a name ends there), so
// "/JS" inside "/JScript" is not a match for "/JS" AT ALL, in neither
// function, not merely an ambiguous one.
//
//   - found: a boundary-valid match exists outside every range in opaque
//     (or opaque is nil, meaning "do not filter by range at all" -- the mode
//     firstTokenLocation used to run in for the matchedInsideOpaque
//     fallback).
//   - filtered: a boundary-valid match exists, but every one seen so far was
//     inside a range in opaque -- the caller must report unknown, never
//     clean and never a Finding (see AnalyzePDF's own handling).
//   - ceilingHit: this call's own per-token-spelling occurrence cap
//     (maxTokenScanIterations) was reached before a conclusive answer
//     (found or "no more occurrences") was reached -- codex PR9 round 2's
//     DoS finding (pdf.go:520). This is needed IN ADDITION to
//     inOpaqueRange's binary search, not instead of it: the binary search
//     removes the RANGE-COUNT multiplier, but the number of OCCURRENCES of a
//     short token is itself attacker-controlled independently of how many
//     opaque ranges exist, so the walk over occurrences still needs its own
//     bound.
func scanTokenOccurrences(buf []byte, tokens [][]byte, opaque []pdfOpaqueRange, tr *Tracker) (pos int, found bool, filtered bool, ceilingHit bool) {
	best := -1
	for _, tok := range tokens {
		p := 0
		for iter := 0; iter < maxTokenScanIterations; iter++ {
			idx := bytes.Index(buf[p:], tok)
			if idx < 0 {
				break
			}
			abs := p + idx
			end := abs + len(tok)
			validBoundary := end >= len(buf) || !isPDFNameByte(buf[end])
			if validBoundary {
				if opaque != nil && inOpaqueRange(abs, opaque) {
					filtered = true
				} else {
					if best < 0 || abs < best {
						best = abs
					}
					break
				}
			}
			p = end
			if p >= len(buf) {
				break
			}
			if iter == maxTokenScanIterations-1 {
				// About to exit only because the cap was reached, not
				// because occurrences ran out or a conclusive answer was
				// found -- the occurrence walk, not the buffer's content,
				// decided the outcome for this token spelling.
				ceilingHit = true
				tr.MarkCeilingHit()
			}
		}
	}
	if best >= 0 {
		return best, true, filtered, false
	}
	return 0, false, filtered, ceilingHit
}

// scanAllOccurrences invokes fn(pos) for every successive, non-overlapping
// occurrence of token in buf, in file order, stopping when fn returns
// false, when no more occurrences exist, or when max iterations have been
// spent -- whichever comes first. It returns ceilingHit==true only in the
// last case: the iteration budget, not the buffer's content, decided when
// scanning stopped.
//
// This is the one loop in this file whose LENGTH is driven by the FILE'S
// OWN CONTENT (how many times a short token repeats) rather than by the
// bounded window size alone -- exactly the CPU-bound-loop shape
// window.go's "why iterations, not context" note warns about, and the
// reason extractPDFPageCount (the loop's only caller) passes
// maxTokenScanIterations rather than looping unconditionally: a PDF
// containing a few hundred thousand repeats of a 12-byte token costs almost
// nothing to construct and, without this cap, would cost this process real,
// unbounded CPU time to walk.
func scanAllOccurrences(buf, token []byte, max int, tr *Tracker, fn func(pos int) bool) (ceilingHit bool) {
	pos := 0
	for i := 0; i < max; i++ {
		idx := bytes.Index(buf[pos:], token)
		if idx < 0 {
			return false
		}
		abs := pos + idx
		if !fn(abs) {
			return false
		}
		pos = abs + len(token)
		if pos >= len(buf) {
			return false
		}
	}
	tr.MarkCeilingHit()
	return true
}

// extractPDFPageCount implements the fast path described in the file-level
// doc comment: find a "/Type/Pages" (or "/Type /Pages") dictionary marker
// and read the integer following a nearby "/Count" key. When several
// occurrences exist (an incrementally-updated file can legitimately have
// more than one Pages-dict marker across revisions), the LAST occurrence
// with a legible /Count wins -- the revision closest to EOF is the most
// likely to be the current, authoritative one, which is the closest this
// bounded byte scan can honestly get to "the current page count" without
// resolving the file's xref chain. This is deliberately NOT the forbidden
// fallback (counting singular "/Type /Page" occurrences, see the doc
// comment on pdfPagesDictTokens) -- it never sums or counts anything, it
// only remembers the most recently observed of possibly several PLAUSIBLE
// answers.
//
// Returns (nil, ReasonCompressedObjectStreams) unconditionally when
// compressed is true: the Pages dict itself may be one of the objects
// hidden inside an /ObjStm, so even a nearby /Count found via this scan
// cannot be trusted to be the WHOLE truth (D-02a). Returns
// (nil, ReasonPDFNameEscape) unconditionally when hexEscape is true, for the
// identical reason applied to a different blind spot: a hex-escaped
// "/Type#XX..." spelling of the Pages-dict marker would not be recognized
// by pdfPagesDictTokens's literal byte match either. Returns
// (nil, ReasonWorkCeilingBytes) when the iteration budget was exhausted or
// when truncated is true and no match was found (the real Pages dict may
// simply live past the read window). Returns (nil, ReasonMalformedInput)
// when the whole (untruncated) window was searched and no Pages dict with a
// legible /Count was ever found.
func extractPDFPageCount(head []byte, compressed, truncated, hexEscape bool, tr *Tracker) (*int, Reason) {
	if compressed {
		return nil, ReasonCompressedObjectStreams
	}
	if hexEscape {
		return nil, ReasonPDFNameEscape
	}

	var found *int
	ceilingHit := false
	for _, variant := range pdfPagesDictTokens {
		hit := scanAllOccurrences(head, variant, maxTokenScanIterations, tr, func(pos int) bool {
			if n, ok := findCountAfter(head, pos+len(variant)); ok {
				v := n
				found = &v
			}
			return true // keep scanning: a later occurrence may be a more current revision
		})
		if hit {
			ceilingHit = true
		}
	}

	// ceilingHit is checked BEFORE found is ever trusted (quick/260811-r3s,
	// pdf.go:800 finding): the "keep scanning" callback above overwrites
	// found with EVERY occurrence it visits, on the theory that the
	// occurrence closest to EOF is the most current revision (this
	// function's own doc comment above). When the walk is cut short by
	// maxTokenScanIterations before reaching the file's true last
	// occurrence, an EARLIER marker's count is left in found and would
	// otherwise be returned as if it were authoritative -- exactly the
	// stale-page-count defect the finding names: "A later marker can belong
	// to the current incremental revision and carry a different count, so
	// the response publishes a stale page_count with complete coverage even
	// though the scan stopped early." A ceiling-truncated walk never gets to
	// assert found's value is the final word, regardless of whether it
	// happened to see one along the way.
	if ceilingHit || truncated {
		return nil, ReasonWorkCeilingBytes
	}
	if found != nil {
		return found, ""
	}
	return nil, ReasonMalformedInput
}

// findCountAfter looks for the literal "/Count" key within
// pdfCountSearchWindow bytes after position from in buf, and parses the
// unsigned decimal integer immediately following it (after skipping
// whitespace). It returns ok==false if "/Count" is not found in that window
// or is not followed by at least one digit -- never a guessed value.
func findCountAfter(buf []byte, from int) (int, bool) {
	if from < 0 || from >= len(buf) {
		return 0, false
	}
	end := from + pdfCountSearchWindow
	if end > len(buf) {
		end = len(buf)
	}
	window := buf[from:end]

	idx := bytes.Index(window, []byte("/Count"))
	if idx < 0 {
		return 0, false
	}
	rest := window[idx+len("/Count"):]
	rest = bytes.TrimLeft(rest, " \t\r\n")

	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(string(rest[:digits]))
	if err != nil {
		return 0, false
	}
	return n, true
}

// extractPDFVersion reads the "%PDF-1.x" (or "%PDF-2.x") header token and
// returns the version digits after "%PDF-". Per the PDF spec a small amount
// of leading garbage before the header is technically permitted, so this
// also tries a bounded Index scan (capped at 1024 bytes in) before giving
// up -- mirrors matchPDF's own tolerance
// (internal/convert/sniff.go's isPDFLeadingWhitespace, read_first).
func extractPDFVersion(head []byte) (string, bool) {
	marker := []byte("%PDF-")
	idx := 0
	if !bytes.HasPrefix(head, marker) {
		idx = bytes.Index(head, marker)
		if idx < 0 || idx > 1024 {
			return "", false
		}
	}
	rest := head[idx+len(marker):]

	end := 0
	for end < len(rest) && (rest[end] == '.' || (rest[end] >= '0' && rest[end] <= '9')) {
		end++
	}
	if end == 0 {
		return "", false
	}
	return string(rest[:end]), true
}
