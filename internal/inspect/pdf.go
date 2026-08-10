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
// characteristics, spending at most maxBytes reading the head of the file
// plus, when the file is larger than maxBytes, one additional
// defaultTailWindow-sized read of the tail (the trailer is conventionally
// there). It never returns an error: any parse failure is expressed as an
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
func AnalyzePDF(r io.ReaderAt, size int64, maxBytes int64) FamilyResult {
	res := FamilyResult{Characteristics: Characteristics{}}

	if size <= 0 || maxBytes <= 0 {
		return malformedPDFResult(res)
	}

	headLen := maxBytes
	if size < headLen {
		headLen = size
	}
	head, headOK := readWindow(r, 0, headLen, size)
	if !headOK {
		return malformedPDFResult(res)
	}

	// truncated is the source of ReasonWorkCeilingBytes below -- OUR OWN
	// byte budget, not a fact about the file. When the file fits entirely
	// within maxBytes, head already covers it end-to-end and a second,
	// tail-anchored read would just re-read bytes already in hand.
	truncated := size > maxBytes
	var tail []byte
	var tailOK bool
	var tailOff int64
	if truncated {
		tailOff = size - defaultTailWindow
		if tailOff < 0 {
			tailOff = 0
		}
		tail, tailOK = readWindow(r, tailOff, defaultTailWindow, size)
	}

	// STEP 1 (D-02a) -- the compression-signal detector runs FIRST, before
	// any risk-token verdict is decided, because its result gates whether
	// this whole block is ever allowed to say "clean". See the file-level
	// doc comment above for the full ISO 32000 §7.5.7 rationale.
	compressed := containsCompressionSignal(head) || (tailOK && containsCompressionSignal(tail))
	res.Characteristics["uses_compressed_object_streams"] = compressed

	if v, ok := extractPDFVersion(head); ok {
		res.Characteristics["pdf_version"] = v
	}

	// STEP 2/3/4: token-scan every risk code, then apply the single verdict
	// rule (verdictForCode) uniformly to every one of them.
	for _, rt := range pdfRiskTokens {
		pos, found := firstTokenLocation(head, rt.tokens)
		loc := pos
		if !found && rt.code == CodePDFEncrypted && tailOK {
			// /Encrypt lives in the trailer or the XRef-stream dict, which
			// is conventionally at the file's tail -- scan there too for
			// this one code (58-RESEARCH.md Q1).
			if tpos, tfound := firstTokenLocation(tail, rt.tokens); tfound {
				found = true
				loc = int(tailOff) + tpos
			}
		}

		if found {
			sev, _ := SeverityOf(rt.code)
			res.Findings = append(res.Findings, Finding{
				Code:     rt.code,
				Severity: sev,
				Location: strconv.Itoa(loc),
			})
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
		res.Checks = append(res.Checks, Check{
			Code:   rt.code,
			Status: verdictStatus(compressed, truncated),
			Reason: verdictReason(compressed, truncated),
		})
	}

	count, reason := extractPDFPageCount(head, compressed, truncated)
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
//   - no compression signal, but OUR OWN byte ceiling truncated the window -> StatusUnknown / ReasonWorkCeilingBytes
//   - neither -> StatusClean
//
// Split into two functions purely so the call site above reads as "status,
// reason" rather than duplicating the same three-way branch twice.
func verdictStatus(compressed, truncated bool) CheckStatus {
	if compressed || truncated {
		return StatusUnknown
	}
	return StatusClean
}

func verdictReason(compressed, truncated bool) Reason {
	switch {
	case compressed:
		return ReasonCompressedObjectStreams
	case truncated:
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
	res.Characteristics["uses_compressed_object_streams"] = false
	res.Characteristics["page_count"] = nil
	res.Characteristics["page_count_reason"] = string(ReasonMalformedInput)
	for _, rt := range pdfRiskTokens {
		res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: ReasonMalformedInput})
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

// firstTokenLocation returns the earliest byte offset within buf at which
// any spelling in tokens occurs, and whether any spelling occurred at all.
// Like containsCompressionSignal, this is bounded by the small, fixed
// number of spellings per code (at most two), not by the file's content --
// it needs no iteration ceiling.
func firstTokenLocation(buf []byte, tokens [][]byte) (int, bool) {
	best := -1
	for _, tok := range tokens {
		if idx := bytes.Index(buf, tok); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
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
func scanAllOccurrences(buf, token []byte, max int, fn func(pos int) bool) (ceilingHit bool) {
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
// (nil, ReasonWorkCeilingBytes) when the iteration budget was exhausted or
// when truncated is true and no match was found (the real Pages dict may
// simply live past the read window). Returns (nil, ReasonMalformedInput)
// when the whole (untruncated) window was searched and no Pages dict with a
// legible /Count was ever found.
func extractPDFPageCount(head []byte, compressed, truncated bool) (*int, Reason) {
	if compressed {
		return nil, ReasonCompressedObjectStreams
	}

	var found *int
	ceilingHit := false
	for _, variant := range pdfPagesDictTokens {
		hit := scanAllOccurrences(head, variant, maxTokenScanIterations, func(pos int) bool {
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

	if found != nil {
		return found, ""
	}
	if ceilingHit || truncated {
		return nil, ReasonWorkCeilingBytes
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
