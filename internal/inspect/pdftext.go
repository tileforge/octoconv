// This file answers ONE question about a PDF -- does it carry a usable text
// layer -- and it is the only check in this package permitted to answer
// substantively when /ObjStm is present. That exception is the whole reason
// the file exists separately from pdf.go, so it is stated first.
//
// pdf.go's own header explains the rule that governs everything else here:
// ISO 32000 §7.5.7 forbids exactly TWO things from living inside a compressed
// object stream -- stream objects themselves, and the encryption dictionary.
// Every other dictionary, /JavaScript included, is legal there, which is why a
// byte scan must degrade the entire PDF findings block to unknown the moment
// it sees the compression signal.
//
// A text layer lives in CONTENT STREAMS. Content streams ARE stream objects.
// They therefore cannot be hidden inside an /ObjStm, and the blindness that
// governs every other check in pdf.go does not apply to this one. That is not
// an argument from the standard alone: 59-RESEARCH.md R4 ran a ghostscript
// -dCompatibilityLevel=1.5 rewrite of the born-digital fixture -- a file that
// really does contain /ObjStm -- and this detector returned the correct count
// for it. internal/inspect/testdata/pdf/objstm_born_digital.pdf is that file,
// and a test asserts both halves in one report.
//
// A future reader "fixing" this file to match the general rule would be
// removing the one check whose subject the rule provably cannot hide.
//
// The two things this file does NOT trust, both measured rather than assumed
// (59-RESEARCH.md R1/R2/R6):
//
//   - Not every content stream is Flate-compressed. Two of the three committed
//     OCR fixtures carry unfiltered ones. A detector that inflates Flate and
//     silently skips whatever fails to inflate would report "no text" for a
//     whole class of writers -- a false claim of ABSENCE, the one direction
//     D-09 forbids.
//   - Image XObjects and embedded font programs must not be inflated at all.
//     An image stream measured 3217 -> 540000 bytes, x168 on ONE stream; and
//     an inflated font program is full of the bytes ' and ", two of PDF's four
//     text operators, which produced a x19 overcount before the operator match
//     was tightened to a real token.
package inspect

import (
	"bytes"
	"compress/zlib"
	"io"
)

// pdfStreamDictLookback bounds how far back from a "stream" keyword this file
// looks for the dictionary that describes it. 512 bytes covers every stream
// dictionary in the measured corpus with room to spare; real writers put
// /Filter, /Subtype and /Length in the same small dictionary that immediately
// precedes the keyword.
//
// The lookback is a HEURISTIC and is treated as one. Its failure mode is
// routed to the safe side: a stream whose dictionary cannot be read
// confidently is neither inflated nor counted as examined, which costs the
// report its right to say "absent" (see pdfTextScanState.covered).
//
// Safe is not the same as free, and the difference was measured. The first
// version of this file took the raw 512 bytes and asked whether they contained
// "/Filter". On multipage_image_only.pdf -- a plain scanned document, the
// single most important input this whole check exists for -- the window
// reached back across an object boundary and picked up the PRECEDING object's
// "/Filter [ /FlateDecode ]". Three streams went opaque, coverage was lost, and
// the verdict degraded to unknown. The field would have gone silent on exactly
// the files a client asks it about, while every count in the corpus was
// nonetheless correct: the numbers looked right and the answer was useless.
// pdfStreamDictWindow below is what confines the window to one object.
const pdfStreamDictLookback = 512

// pdfObjectBoundaryMarkers end the PREVIOUS object. A stream's describing
// dictionary is the one between the nearest of these and the "stream" keyword;
// anything earlier belongs to a different object and must not be read as
// though it described this one.
var pdfObjectBoundaryMarkers = [][]byte{
	[]byte("endobj"),
	[]byte("endstream"),
}

// pdfStreamDictWindow returns the bytes that plausibly hold the dictionary
// describing the stream body starting at streamStart.
//
// It walks back at most pdfStreamDictLookback bytes and then trims forward
// past the LAST object-boundary marker inside that span, so a neighbouring
// object's keys cannot be attributed to this stream. Both directions of error
// remain possible -- a dictionary longer than the lookback is still cut short,
// and a boundary marker appearing inside binary payload still truncates early
// -- and both stay safe, because a window that fails to show /Filter yields
// either a scannable raw stream (whose content is then judged on its own
// bytes) or an opaque one (which forfeits the absence claim). What it can no
// longer do is silently inherit a filter declaration from someone else.
func pdfStreamDictWindow(buf []byte, streamStart int) []byte {
	start := streamStart - pdfStreamDictLookback
	if start < 0 {
		start = 0
	}
	window := buf[start:streamStart]
	cut := 0
	for _, marker := range pdfObjectBoundaryMarkers {
		if idx := bytes.LastIndex(window, marker); idx >= 0 && idx+len(marker) > cut {
			cut = idx + len(marker)
		}
	}
	return window[cut:]
}

// pdfMaxInflatedStreamBytes caps the OUTPUT of decompressing a SINGLE stream.
//
// This is not a second budget -- D-03 rejected a second counter outright, and
// rightly: two places to charge are two places to forget. Every inflated byte
// is still charged to the one shared Tracker. This is a bound on the size of
// one READ, which the Tracker cannot express: without it the first image
// XObject in a scanned document exhausts the whole operation's allowance and
// the detector never reaches the content streams it was looking for -- the
// field would go silent on scans, which are the very files a client asks about.
//
// The number: legitimate content streams in the measured corpus inflate to
// 194..698 bytes, while image streams reach 60000..540000. A 200-page
// production document has legitimately larger content streams than a fixture,
// so this is set well above the measured content range and well below the
// measured image range, and it is a CEILING on a single stream rather than an
// expectation about one. A stream that exceeds it is not examined, and the
// report loses its right to claim absence, exactly like an undecodable one.
const pdfMaxInflatedStreamBytes = 1 << 20 // 1 MiB

// pdfProvenSkipMarkers name keys that describe THE STREAM ITSELF, so a stream
// carrying one cannot hold a text layer and skipping it costs the report
// nothing:
//
//   - /Subtype /Image (both spellings): an image XObject. This is the
//     decompression-bomb surface AND the reason the detector would otherwise go
//     silent on scans -- x168 measured on one stream, 59-RESEARCH.md R2.
//   - /Length1: present only on a font-PROGRAM stream, where it gives the
//     uncompressed length of the embedded file.
//   - /Subtype /Type1C and /CIDFontType0C: compact font programs, likewise.
//
// An inflated font program is binary full of ' and " bytes -- two of PDF's four
// text operators -- which produced a x19 overcount before the operator match was
// tightened (R6). The token rule closes that on its own; skipping removes the
// surface rather than merely surviving it.
//
// WHY these are provably safe and /FontFile* is not, rather than merely feeling
// safer. ISO 32000 §7.3.8.1 requires a stream to be an INDIRECT object, so a
// stream dictionary can never be nested inside another stream's dictionary --
// a Form XObject that draws an image references it (/Im0 9 0 R) and cannot
// contain it. Therefore /Subtype /Image and /Length1, which are keys OF a
// stream dictionary, can only ever describe the stream they precede.
//
// /FontFile* is the opposite case and that is the whole distinction: it is a
// key of a font DESCRIPTOR, an ordinary dictionary that may legitimately be
// nested, by value, inside a text-bearing Form XObject's own /Resources. The
// rule is not "these markers are more specific" but "these markers cannot
// appear in a dictionary describing something else".
// Also here: stream types that exist in a PDF but cannot contribute PAGE text.
// An /EmbeddedFile attachment may contain literally anything, including bytes
// that read as `BT (…) Tj` -- codex's review of PR #10 supplied that case, and
// before this list it counted as page content and reported present for a
// document pdftotext exposes no such text for. /Metadata, /ObjStm and /XRef are
// the same class: real streams, no page text.
//
// The §7.3.8.1 argument covers these exactly as it covers /Subtype /Image --
// they are /Type keys OF a stream dictionary, and a stream is always an
// indirect object, so they cannot be describing something nested.
var pdfProvenSkipMarkers = [][]byte{
	[]byte("/Image"),
	[]byte("/Length1"),
	[]byte("/Type1C"),
	[]byte("/CIDFontType0C"),
	[]byte("/EmbeddedFile"),
	[]byte("/Metadata"),
	[]byte("/ObjStm"),
	[]byte("/XRef"),
}

// pdfAmbiguousSkipMarkers name keys a dictionary may merely REFERENCE while
// also describing a stream that does carry text. A stream matching one of these
// is treated as OPAQUE -- not examined, and therefore not eligible to support a
// claim of absence.
//
// /FontFile, /FontFile2 and /FontFile3 are the case codex's review of PR #10
// named, and the distinction they turn on is worth stating: a font DESCRIPTOR
// points at a font file, it is not one. A text-bearing Form XObject may
// legitimately carry `/Resources << /Font << … /FontFile2 9 0 R >> >> >>` in its
// own dictionary. Under the previous rule that stream was silently skipped,
// coverage stayed true, and the report answered `text_layer: absent` for a
// document full of text -- the one direction D-09 forbids outright.
//
// The doc comment this replaces claimed over-matching was safe "because a
// skipped stream is not counted as examined". That was simply false about the
// code: pdfStreamSkipped never touched `covered`. The claim and the mechanism
// disagreed, and the mechanism won. Ambiguity now costs coverage, which is what
// the old comment had promised all along.
var pdfAmbiguousSkipMarkers = [][]byte{
	[]byte("/FontFile"),
	[]byte("/FontFile2"),
	[]byte("/FontFile3"),
}

// containsPDFNameToken reports whether name occurs in buf as a COMPLETE PDF
// name token, i.e. not as a prefix of a longer one.
//
// A plain substring test is not sufficient, and the failure is not theoretical:
// codex's review of PR #10 pointed out that a dictionary carrying an unrelated
// `/Length10` key matched the `/Length1` marker, so a text-bearing stream was
// classified as a proven-skippable font program, coverage was preserved, and the
// report answered `text_layer: absent` for a document full of text -- the one
// direction D-09 forbids outright.
//
// This is the same defect the risk-token scanner in pdf.go already closes for
// its own table ("/JS inside /JScript is not a match"), using the same
// isPDFNameByte boundary rule from ISO 32000 §7.3.5. Reusing that rule rather
// than restating it is the point: one definition of where a name ends.
func containsPDFNameToken(buf, name []byte) bool {
	from := 0
	for {
		idx := bytes.Index(buf[from:], name)
		if idx < 0 {
			return false
		}
		end := from + idx + len(name)
		if end >= len(buf) || !isPDFNameByte(buf[end]) {
			return true
		}
		from = from + idx + 1
	}
}

// pdfStreamClass is what the lookback dictionary plus a decode attempt say
// about one stream.
type pdfStreamClass int

const (
	// pdfStreamScannable: the bytes are in hand and can be searched.
	pdfStreamScannable pdfStreamClass = iota
	// pdfStreamSkipped: an image or font program. Cannot carry a text layer,
	// so not examining it costs the report nothing.
	pdfStreamSkipped
	// pdfStreamOpaque: could not be decoded because THIS BUILD has no decoder
	// for it -- an unsupported filter, an encrypted body, an illegible
	// dictionary, or a stream past the single-stream inflate ceiling. This is
	// the branch that must never be confused with pdfStreamSkipped: an opaque
	// stream MIGHT have carried text, so after one of these the report may no
	// longer say "absent".
	pdfStreamOpaque
	// pdfStreamMalformed: the stream declared a filter this build DOES decode
	// and the decode then failed part-way -- a corrupt or truncated body.
	//
	// Separate from pdfStreamOpaque because the two lead to different REASONS,
	// and the difference is the distinction this whole phase enforces:
	// "no decoder here" is a limit of this build (parser_unavailable), while
	// "the bytes are broken" is a claim about the FILE (malformed_input).
	// Reporting the first as the second tells a client their file is corrupt
	// when it is merely using a feature we did not implement.
	pdfStreamMalformed
	pdfStreamOverBudget
)

// pdfTextScanState accumulates what a whole-buffer scan learned.
//
// covered is the field D-09 turns on: it is true only while every stream was
// either scanned or safely skipped. One opaque stream, one exhausted budget,
// or one truncated scan is enough to lose it -- and losing it downgrades
// "absent" to "unknown", never the other way round. A claim of PRESENCE is
// never weakened by it, mirroring pdf.go's own rule that structural blindness
// degrades absence claims and leaves positive observations alone.
type pdfTextScanState struct {
	nonWhitespace int
	covered       bool
	budgetHit     bool
	// parserLimit: at least one stream was undecodable by THIS BUILD.
	parserLimit bool
	// malformed: at least one stream declared a filter we do decode, and the
	// decode failed part-way.
	malformed bool
	// remaining is the LOCAL inflate allowance, counted down as stream bodies
	// are decoded. It is not the shared Tracker.
	//
	// This file must never call tr.Consume, and the reason is a rule the
	// package already had: dispatchInspectFamilies charges tr.Remaining() --
	// the analyzer's ENTIRE allowance -- before the analyzer runs, so any
	// Consume from inside one instantly exhausts the shared budget. That is
	// PR9R9's defect (media.go was the original offender), and the first
	// version of this file reproduced it exactly: every PDF in the corpus came
	// back coverage.complete:false / work_ceiling_bytes, including four small
	// valid files. The 59-01 golden pin caught it.
	//
	// So the allowance is spent LOCALLY, exactly as every other analyzer
	// spends its maxBytes, and the shared Tracker hears about exhaustion only
	// through MarkCeilingHit -- the receptacle built for precisely this: a
	// ceiling INSIDE an analyzer, not the shared byte budget.
	remaining int64
	zr        io.ReadCloser
	out       []byte
}

// scanPDFTextLayer walks every stream body in buf and counts the
// non-whitespace bytes inside the string arguments of text-showing operators.
//
// allowance bounds the TOTAL decompressed bytes this scan may produce. The
// caller derives it from the allowance it was itself given (AnalyzePDF's
// maxBytes, minus the windows already read), so inflated bytes and read bytes
// are spent from one pool -- D-03's "one budget, not two counters" -- without
// this file ever touching tr.Consume. See pdfTextScanState.remaining.
//
// tr may be nil (a unit test exercising this file in isolation), matching
// every other analyzer in this package. It is used only as the MarkCeilingHit
// receptacle.
func scanPDFTextLayer(buf []byte, allowance int64, tr *Tracker) pdfTextScanState {
	state := pdfTextScanState{covered: true, remaining: allowance}

	ranges, ceilingHit, lexComplete := scanValidatedStreamRanges(buf, tr)
	if !lexComplete {
		// An unterminated literal string swallowed part of the window. See
		// skipPDFLiteral for why silence here would be the dangerous choice.
		state.covered = false
		state.parserLimit = true
	}
	if ceilingHit {
		// The range scan gave up on its own iteration cap, so the stream SET
		// is incomplete -- there may be streams past the last one found. Same
		// reasoning pdf.go applies when its opaque-range scan is truncated:
		// a filter that ran on an incomplete set has no standing to declare
		// anything absent.
		state.covered = false
		state.budgetHit = true
	}

	for _, rng := range ranges {
		if tr.OutOfTime() {
			tr.MarkCeilingHit()
			state.covered = false
			state.budgetHit = true
			return state
		}
		if rng.start < 0 || rng.end > len(buf) || rng.start >= rng.end {
			continue
		}
		data, class := decodePDFStream(buf, rng, tr, &state)
		switch class {
		case pdfStreamSkipped:
			continue
		case pdfStreamOpaque:
			state.covered = false
			state.parserLimit = true
			continue
		case pdfStreamMalformed:
			state.covered = false
			state.malformed = true
			continue
		case pdfStreamOverBudget:
			state.covered = false
			state.parserLimit = true
			return state
		}
		state.nonWhitespace += countTextOperatorArgBytes(data, tr)
	}
	if tr.OutOfTime() {
		tr.MarkCeilingHit()
		state.covered = false
		state.budgetHit = true
	}

	return state
}

// scanValidatedStreamRanges returns the bodies of streams that are REAL stream
// objects, by walking the buffer once with a state machine that understands the
// three lexical contexts a "stream" keyword can appear in.
//
// This replaces the earlier approach of reusing pdf.go's scanPDFStreamRanges and
// adding a predicate on top, and the replacement was forced by evidence rather
// than chosen for elegance. codex's review of PR #10 defeated two successive
// versions of the layered check:
//
//  1. The original used scanPDFStreamRanges directly. That scanner is
//     deliberately lexical -- every bare "stream" substring opens a range --
//     because its OTHER callers build the opaque-range filter, where
//     over-matching is the safe direction. A literal string containing the word
//     `stream` was therefore scanned as page content.
//  2. The first fix required the keyword to be preceded by `>>`. A literal
//     string can contain `>>` too: `(>>stream BT (abc) Tj endstream)` passed the
//     predicate and reported present all the same.
//
// The lesson is that no amount of LOOKING BACKWARD from a candidate can tell you
// whether that candidate is inside a string, because the answer depends on
// everything before it. Only a forward pass that has tracked the context can.
// Two bypasses of one bound is the point at which adding a third clever
// comparison is the wrong move.
//
// What the pass understands, and nothing more:
//
//   - literal strings (…), with backslash escapes and nesting, so neither a
//     `stream` keyword nor a `>>` inside one is ever seen as syntax;
//   - comments %…EOL, which PDF treats as whitespace between tokens -- this is
//     also what lets a legal `>> % note` + EOL + `stream` be recognized, a form
//     the previous backward walk rejected and thereby degraded a perfectly
//     readable document to unknown;
//   - stream bodies themselves, which are skipped wholesale to `endstream` so
//     their arbitrary binary content cannot open a spurious string or comment.
//
// It does NOT parse dictionaries, resolve objects, or validate /Length. The
// question it answers is exactly "is this keyword real", and its value is that
// it is small enough to audit.
//
// ceilingHit mirrors scanPDFStreamRanges's own contract: it reports that the
// iteration cap, not the buffer's content, decided when the walk stopped, and
// it marks the shared receptacle at that point.
func scanValidatedStreamRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool, complete bool) {
	limit := pdfMaxOpaqueRanges(len(buf))
	var ok bool
	complete = true
	i := 0
	for iter := 0; i < len(buf); iter++ {
		if iter >= maxTokenScanIterations || (iter%pdfTimeCheckEvery == 0 && tr.OutOfTime()) {
			tr.MarkCeilingHit()
			return ranges, true, complete
		}

		switch buf[i] {
		case '(':
			next, terminated := skipPDFLiteral(buf, i)
			if !terminated {
				// Everything from here to the end of the window was swallowed
				// by a string that never closed. Those bytes are unexamined,
				// and the report may no longer claim absence.
				complete = false
			}
			i = next
			continue
		case '%':
			i = skipPDFComment(buf, i)
			continue
		case '>':
			if i+1 >= len(buf) || buf[i+1] != '>' {
				i++
				continue
			}
			// Candidate: a dictionary just closed. Everything between it and
			// the keyword must be whitespace or comments.
			k := skipPDFWhitespaceAndComments(buf, i+2)
			if !bytes.HasPrefix(buf[k:], []byte("stream")) {
				i += 2
				continue
			}
			bodyStart := k + len("stream")
			endIdx := bytes.Index(buf[bodyStart:], []byte("endstream"))
			if endIdx < 0 {
				// The body runs past the end of this window. Report what is
				// there; the caller charges it and the truncation is already
				// visible through its own window accounting.
				ranges, ok = appendOpaqueRange(ranges, pdfOpaqueRange{start: bodyStart, end: len(buf)}, limit, tr)
				return ranges, !ok, complete
			}
			bodyEnd := bodyStart + endIdx
			ranges, ok = appendOpaqueRange(ranges, pdfOpaqueRange{start: bodyStart, end: bodyEnd}, limit, tr)
			if !ok {
				return ranges, true, complete
			}
			// Resume AFTER the body, so its binary content is never lexed.
			i = bodyEnd + len("endstream")
			continue
		default:
			i++
		}
	}
	return ranges, false, complete
}

// skipPDFLiteral returns the index just past the literal string starting at
// open, honouring backslash escapes and nesting, and reports whether the
// literal was actually TERMINATED.
//
// The second return value matters more than it looks. An unterminated literal
// consumes the rest of the buffer, so nothing after it is mistaken for syntax
// -- fail-closed against a false `present`. But silence about it produces the
// opposite defect, and an adversarial probe written while awaiting review found
// it: `(unterminated << /Length 30 >> stream … endstream` yielded zero streams,
// full coverage, and `text_layer: absent`. The scanner had examined none of the
// document and claimed there was no text in it -- exactly the D-09 violation
// this whole file exists to prevent, arriving through the one path that looked
// like a safe default.
//
// So an unterminated literal costs coverage. `absent` becomes unreachable;
// `present` is unaffected, since anything already counted really was seen.
func skipPDFLiteral(buf []byte, open int) (next int, terminated bool) {
	depth := 1
	for i := open + 1; i < len(buf); i++ {
		switch buf[i] {
		case '\\':
			i++ // skip the escaped byte, whatever it is
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return len(buf), false
}

// skipPDFComment returns the index just past the end-of-line that terminates
// the comment starting at pct.
func skipPDFComment(buf []byte, pct int) int {
	for i := pct; i < len(buf); i++ {
		if buf[i] == '\n' || buf[i] == '\r' {
			return i + 1
		}
	}
	return len(buf)
}

// skipPDFWhitespaceAndComments advances past any run of whitespace and comments
// starting at i. PDF treats a comment as whitespace between tokens (ISO 32000
// §7.2.4), which is why `>> % producer note` followed by an EOL and `stream` is
// a legal stream object -- a form the earlier backward walk rejected, degrading
// a fully readable document from absent to unknown for no gain.
func skipPDFWhitespaceAndComments(buf []byte, i int) int {
	for i < len(buf) {
		switch buf[i] {
		case ' ', '\t', '\r', '\n', '\f', 0:
			i++
		case '%':
			i = skipPDFComment(buf, i)
		default:
			return i
		}
	}
	return i
}

// decodePDFStream classifies one stream and, when it is scannable, returns its
// bytes.
//
// The order of the checks is the point: classify from the dictionary FIRST,
// decode only after. Deciding "is this an image?" before spending any
// decompression is what keeps a scanned document affordable; doing it the
// other way round would mean inflating half a megabyte to then discard it.
func decodePDFStream(buf []byte, rng pdfOpaqueRange, tr *Tracker, state *pdfTextScanState) ([]byte, pdfStreamClass) {
	dict := pdfStreamDictWindow(buf, rng.start)

	for _, marker := range pdfProvenSkipMarkers {
		if containsPDFNameToken(dict, marker) {
			return nil, pdfStreamSkipped
		}
	}
	for _, marker := range pdfAmbiguousSkipMarkers {
		if containsPDFNameToken(dict, marker) {
			return nil, pdfStreamOpaque
		}
	}

	// The stream keyword is followed by an EOL the body does not include
	// (ISO 32000 §7.3.8.1). scanPDFStreamRanges hands back everything after
	// the keyword, so trim it here rather than teaching that shared scanner
	// about this file's needs.
	body := bytes.TrimLeft(buf[rng.start:rng.end], "\r\n")
	if len(body) == 0 {
		// Nothing there to examine, and nothing hidden either.
		return nil, pdfStreamSkipped
	}

	if state.remaining == 0 {
		state.budgetHit = true
		tr.MarkCeilingHit()
		return nil, pdfStreamOverBudget
	}

	zr, err := state.inflater(body)
	if err != nil {
		// Not Flate. Two possibilities, and they are NOT the same answer
		// (59-RESEARCH.md R1). A stream with no /Filter at all is a raw
		// content stream and is scannable as-is -- measured in two of the
		// three committed OCR fixtures. A stream that declares a filter we
		// did not decode is opaque: it might carry text this build cannot
		// see, so the report loses its right to say "absent".
		if bytes.Contains(dict, []byte("/Filter")) {
			return nil, pdfStreamOpaque
		}
		if !chargeInflatedBytes(tr, len(body), state) {
			return nil, pdfStreamOverBudget
		}
		return body, pdfStreamScannable
	}

	// LimitReader at ceiling+1 so an output that would EXCEED the ceiling is
	// distinguishable from one that lands exactly on it -- reading exactly
	// pdfMaxInflatedStreamBytes cannot tell those apart, and treating a
	// legitimately ceiling-sized stream as truncated would cost coverage for
	// nothing.
	limit := min(int64(pdfMaxInflatedStreamBytes), state.remaining)
	out, readErr := state.inflate(zr, limit+1)
	if int64(len(out)) > limit {
		state.budgetHit = true
		tr.MarkCeilingHit()
		return nil, pdfStreamOverBudget
	}
	if readErr != nil {
		// zlib accepted the header and then failed: the body really is corrupt
		// or truncated. That IS a claim about the file, unlike everything else
		// on this path, so it gets its own class.
		return nil, pdfStreamMalformed
	}
	if !chargeInflatedBytes(tr, len(out), state) {
		return nil, pdfStreamOverBudget
	}
	return out, pdfStreamScannable
}

func (state *pdfTextScanState) inflater(body []byte) (io.Reader, error) {
	if state.zr == nil {
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		state.zr = zr
		return zr, nil
	}
	if err := state.zr.(zlib.Resetter).Reset(bytes.NewReader(body), nil); err != nil {
		return nil, err
	}
	return state.zr, nil
}

func (state *pdfTextScanState) inflate(zr io.Reader, limit int64) ([]byte, error) {
	out := state.out[:0]
	for int64(len(out)) < limit {
		if len(out) == cap(out) {
			grown := make([]byte, len(out), min(max(512, 2*int64(cap(out))), limit))
			copy(grown, out)
			out = grown
		}
		n, err := zr.Read(out[len(out):cap(out)])
		out = out[:len(out)+n]
		if err != nil {
			state.out = out
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
	}
	state.out = out
	return out, nil
}

// chargeInflatedBytes spends n bytes of the scan's LOCAL allowance and reports
// whether they fit.
//
// Inflated bytes come out of the same pool as bytes read from disk (D-03): a
// decompressed megabyte costs the process exactly what a read megabyte costs,
// and the ceiling exists to bound what one operation spends regardless of HOW
// it spends it. What makes it one pool is that the caller derives this
// allowance by SUBTRACTING the windows it already read from the maxBytes it
// was given -- not a second counter, a division of the first.
//
// On exhaustion it marks the shared Tracker's ceiling receptacle, so
// buildCoverage learns the report is selective even though this file never
// calls Consume. tr.MarkCeilingHit tolerates a nil receiver by design.
func chargeInflatedBytes(tr *Tracker, n int, state *pdfTextScanState) bool {
	if int64(n) > state.remaining {
		state.budgetHit = true
		tr.MarkCeilingHit()
		return false
	}
	state.remaining -= int64(n)
	return true
}

// pdfTextOperators are the four text-showing operators of ISO 32000 §9.4.3.
// Two of them are single bytes, and that is the entire difficulty of this
// file: ' is 0x27 and " is 0x22, both of which occur constantly inside binary
// payloads. Matching them as substrings rather than as tokens is what produced
// the x19 overcount in 59-RESEARCH.md R6.
var pdfTextOperators = [][]byte{
	[]byte("TJ"),
	[]byte("Tj"),
	[]byte("'"),
	[]byte(`"`),
}

// countTextOperatorArgBytes counts the non-whitespace bytes inside the string
// arguments of text-showing operators in one already-decoded content stream.
//
// The unit is deliberately the worker's unit (D-04): internal/convert's
// countNonWhitespace over what pdftotext extracts. Sharing it is what makes
// the divergence between the two detectors measurable at all (D-08/D-10) --
// two different units could not be compared, only compared-to-nothing. The
// measured result on the corpus: 82/82, 53/53, 42/42, 0/0, 0/0 against a live
// pdftotext, byte for byte.
//
// The rule, in order:
//
//  1. Find a string argument: a literal (…) with backslash escapes and nesting,
//     or a hex string <…>. Hex strings are a legal argument to the same
//     operators; they did not appear in the measured corpus, which is exactly
//     why omitting them would be an easy and invisible false "absent".
//  2. Skip whitespace, numbers, and further array elements -- a TJ argument is
//     an array of alternating strings and kerning numbers.
//  3. The next token must BE one of the four operators, delimiter-bounded on
//     the right.
//
// Step 3 is the load-bearing line. Relaxing it to "an operator appears
// somewhere nearby" is precisely the defect R6 measured, and any future
// simplification here should be checked against
// internal/inspect/testdata/pdf/embedded_font.pdf, whose true answer is 42 and
// whose loose-rule answer is 817.
//
// The walk is capped by maxTokenScanIterations for the same reason every other
// scanner in this package is: an adversarial stream can repeat "(" without
// bound, and a CPU-bound loop is not interrupted by a context deadline.
func countTextOperatorArgBytes(buf []byte, tr *Tracker) int {
	total := 0
	i := 0
	for iter := 0; i < len(buf); iter++ {
		if iter >= maxTokenScanIterations || (iter%pdfTimeCheckEvery == 0 && tr.OutOfTime()) {
			break
		}
		switch buf[i] {
		case '(':
			content, next := readPDFLiteralString(buf, i)
			if operatorFollows(buf, next, tr) {
				total += countNonWhitespaceBytes(content)
			}
			i = next
		case '<':
			// "<<" opens a dictionary, not a hex string.
			if i+1 < len(buf) && buf[i+1] == '<' {
				i += 2
				continue
			}
			content, next := readPDFHexString(buf, i)
			if operatorFollows(buf, next, tr) {
				total += countNonWhitespaceBytes(content)
			}
			i = next
		default:
			i++
		}
	}
	return total
}

// readPDFLiteralString reads a (…) literal starting at open, DECODES its
// escapes, and returns the decoded bytes plus the index just past the closing
// paren.
//
// On decoding, and on the comment this replaces. An earlier version kept escape
// sequences as literal digits and argued the resulting over-count was safe:
// "an octal escape contributes its literal digits rather than one character,
// which can only over-count a genuinely text-bearing stream that is already far
// past the threshold, never manufacture text in an empty one."
//
// That was the same argument readPDFHexString made, and it was already
// disproved for hex by measurement before this function was left alone with it.
// codex's review of PR #10 supplied the counterexample for the other half:
// `(\101\102\103\104\105\106) Tj` is six characters and counted eighteen,
// and the shared threshold is sixteen. It did not exaggerate a verdict, it
// manufactured one -- inspect reporting a usable text layer for a document the
// worker still sends to OCR.
//
// The lesson is not "decode octal too". It is that fixing the named instance
// and leaving the identical reasoning standing next to it is how a defect class
// survives its own fix.
//
// ISO 32000 §7.3.4.2 escapes handled: \n \r \t \b \f \( \) \\, one to
// three octal digits, and a backslash before an end-of-line, which is a line
// continuation contributing nothing. A backslash before anything else drops the
// backslash and keeps the byte, which is what the standard says.
func readPDFLiteralString(buf []byte, open int) (content []byte, next int) {
	var out []byte
	depth := 1
	i := open + 1

	for i < len(buf) && depth > 0 {
		c := buf[i]
		switch c {
		case '\\':
			i++
			if i >= len(buf) {
				return out, len(buf)
			}
			e := buf[i]
			switch {
			case e >= '0' && e <= '7':
				// One to three octal digits, per the standard. Values above
				// 255 are taken modulo 256, which is also what the standard
				// says to do.
				v := 0
				n := 0
				for n < 3 && i < len(buf) && buf[i] >= '0' && buf[i] <= '7' {
					v = v*8 + int(buf[i]-'0')
					i++
					n++
				}
				out = append(out, byte(v))
				continue
			case e == 'n':
				out = append(out, '\n')
			case e == 'r':
				out = append(out, '\r')
			case e == 't':
				out = append(out, '\t')
			case e == 'b':
				out = append(out, '\b')
			case e == 'f':
				out = append(out, '\f')
			case e == '\n':
				// Line continuation: contributes nothing at all.
			case e == '\r':
				if i+1 < len(buf) && buf[i+1] == '\n' {
					i++
				}
			default:
				// Includes \( \) \\ and any other byte: drop the backslash,
				// keep the byte. Crucially this means an escaped paren does not
				// change depth below.
				out = append(out, e)
			}
			i++
		case '(':
			depth++
			out = append(out, c)
			i++
		case ')':
			depth--
			if depth == 0 {
				return out, i + 1
			}
			out = append(out, c)
			i++
		default:
			out = append(out, c)
			i++
		}
	}

	// Unterminated string: take what was decoded. An unterminated literal is
	// malformed input, and refusing to count it would be a claim about the file
	// this function is not the right place to make.
	return out, len(buf)
}

// readPDFHexString reads a <…> hex string starting at open, DECODES it, and
// returns the decoded bytes plus the index just past the '>'.
//
// The decode is not a nicety. An earlier version returned the raw hex digits
// and this comment argued the over-count was harmless -- "it can only push a
// present verdict further past a threshold it already cleared". The
// differential run against pdftotext disproved that on the first fixture built
// to provoke it (cid_identity_h.pdf): raw digits counted 44 where pdftotext
// counts 10, and 10 is BELOW the shared threshold while 44 is above it. The
// over-count did not exaggerate a verdict, it manufactured one -- inspect
// reported a usable text layer for a document the worker would send to OCR,
// steering a client away from the OCR they need.
//
// Decoding makes the two agree exactly, and the arithmetic shows why it is not
// a coincidence. Identity-H writes one glyph as two bytes, and for the BMP the
// high byte is 0x00 -- which countNonWhitespaceBytes already excludes, along
// with 0x20 for a space. So decoded-and-counted lands on the glyph count, which
// for a text-bearing document IS the character count. The raw-digit form was
// off by exactly the factor the encoding introduces.
//
// ISO 32000 §7.3.4.3 details handled: whitespace between digits is legal and
// ignored, an odd final digit is padded with 0, and a non-hex byte ends the
// useful content. An unterminated string yields what was read -- malformed
// input is not this function's claim to make.
func readPDFHexString(buf []byte, open int) (content []byte, next int) {
	var out []byte
	var hi byte
	haveHi := false

	i := open + 1
	for ; i < len(buf); i++ {
		c := buf[i]
		if c == '>' {
			if haveHi {
				out = append(out, hi<<4) // odd digit count: pad with 0
			}
			return out, i + 1
		}
		v, ok := hexDigitValue(c)
		if !ok {
			// Whitespace is legal between digits; anything else is malformed,
			// and skipping it is the lenient direction -- consistent with the
			// rest of this package, which never rejects a file for being odd.
			continue
		}
		if haveHi {
			out = append(out, hi<<4|v)
			haveHi = false
			continue
		}
		hi = v
		haveHi = true
	}
	if haveHi {
		out = append(out, hi<<4)
	}
	return out, len(buf)
}

// hexDigitValue returns the numeric value of one ASCII hex digit.
func hexDigitValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// operatorFollows reports whether the next real token at or after i is one of
// the four text-showing operators.
//
// It skips whitespace, numeric array elements, and further strings inside a TJ
// array, then requires an EXACT token match with a delimiter (or end of input)
// on the right. isPDFNameByte (pdf.go) already encodes ISO 32000 §7.3.5's
// delimiter set, so the boundary rule is shared rather than restated.
func operatorFollows(buf []byte, i int, tr *Tracker) bool {
	for iter := 0; i < len(buf); iter++ {
		if iter >= maxTokenScanIterations || (iter%pdfTimeCheckEvery == 0 && iter > 0 && tr.OutOfTime()) {
			return false
		}
		c := buf[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0:
			i++
		case (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.':
			i++
		case c == ']':
			// End of a TJ array -- the operator is what comes after it.
			i++
		case c == '(':
			_, next := readPDFLiteralString(buf, i)
			i = next
		case c == '<':
			if i+1 < len(buf) && buf[i+1] == '<' {
				return false
			}
			_, next := readPDFHexString(buf, i)
			i = next
		default:
			for _, op := range pdfTextOperators {
				if !bytes.HasPrefix(buf[i:], op) {
					continue
				}
				end := i + len(op)
				if end >= len(buf) || !isPDFNameByte(buf[end]) {
					return true
				}
			}
			return false
		}
	}
	return false
}

// countNonWhitespaceBytes is this package's copy of internal/convert's
// countNonWhitespace, byte-for-byte equivalent for the ASCII whitespace PDF
// uses.
//
// It is a copy rather than an import because internal/inspect may not import
// internal/convert -- findings.go's own doc comment draws that line: this
// package answers "what is in the file" and knows nothing about engines. The
// duplication is the price of the layering, and it is paid under a test:
// 59-05's threshold-parity test reads both declarations from source and
// requires them equal, so the two cannot drift into different units while
// D-10 is busy comparing them.
func countNonWhitespaceBytes(b []byte) int {
	n := 0
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n', '\v', '\f', 0:
		default:
			n++
		}
	}
	return n
}

// pdfUsableTextLayerThreshold is the non-whitespace character count above
// which a PDF's text layer is reported as present.
//
// It is a DUPLICATE of internal/convert's ocrUsableTextLayerThreshold, and the
// duplication is deliberate and tested. This package may not import
// internal/convert -- findings.go's own doc comment draws that line: inspect
// answers "what is in the file" and knows nothing about engines. But the two
// detectors must share a unit AND a threshold, or the divergence D-08 predicts
// and D-10 measures could not be expressed as a number at all: two scales
// cannot be compared, only compared to nothing.
//
// TestTextLayerThresholdMatchesConverter reads both declarations out of source
// and requires them equal, so the copy cannot drift unnoticed.
const pdfUsableTextLayerThreshold = 16

// TextLayer values reported in the text_layer characteristic. Deliberately NOT
// CheckStatus values (D-02): a text layer is not a risk, so "clean" would mean
// nothing about it and severity does not apply. It gets its own small
// vocabulary and carries its own reason.
const (
	// TextLayerPresent: a usable text layer was found.
	TextLayerPresent = "present"
	// TextLayerAbsent: no usable text layer, AND everything that could hold
	// one was examined. See pdfTextLayerVerdict for how narrow that is.
	TextLayerAbsent = "absent"
)

// pdfTextLayerVerdict turns a scan into the pair reported to the client:
// (value, reason). A nil value serializes to JSON null -- an honest "we do not
// know", the same present-but-null convention page_count already uses.
//
// The rule, and it is asymmetric on purpose (D-09):
//
//   - PRESENT is reported whenever enough text was actually seen, regardless of
//     coverage. Finding text is a positive observation, and a positive
//     observation is never weakened by not having seen everything else. This
//     mirrors pdf.go's own rule for findings.
//   - ABSENT requires the opposite: full coverage, an unexhausted budget, and
//     a readable window. "I found no text" is a claim about the whole file, and
//     it may only be made by a scan that actually saw the whole file.
//
// Anything else is unknown with a reason. "Absent whenever no text was found"
// is the exact defect eight rounds of PR #9 review removed elsewhere in this
// package -- an optimistic answer for input nobody examined.
func pdfTextLayerVerdict(state pdfTextScanState, truncated, encrypted bool) (value any, reason Reason) {
	if state.nonWhitespace > pdfUsableTextLayerThreshold {
		return TextLayerPresent, ""
	}
	switch {
	case encrypted:
		// Stream bodies are encrypted and this build does not decrypt them, so
		// an inflate failure says nothing about whether text exists. NOT
		// malformed_input: the file is not broken, we simply cannot open it --
		// and telling a client their document is corrupt when it is merely
		// password-protected is the kind of claim-about-the-file-made-from-our-
		// own-limits this phase exists to remove. Not a ceiling reason either:
		// no budget would change it (status.go's own vocabulary note).
		return nil, ReasonParserUnavailable
	case state.budgetHit:
		return nil, ReasonWorkCeilingBytes
	case truncated:
		return nil, ReasonWorkCeilingBytes
	case state.malformed:
		// A decoder we DO have failed part-way through a body. Checked before
		// parserLimit because a genuinely corrupt file is the more specific and
		// more actionable finding when both are true.
		return nil, ReasonMalformedInput
	case state.parserLimit:
		// A filter, or an encrypted body, or a stream too large to inflate --
		// all limits of THIS BUILD, honestly declared.
		return nil, ReasonParserUnavailable
	case !state.covered:
		// Coverage was lost without either specific cause being recorded --
		// today only the truncated stream-range scan reaches here. Report the
		// build's own limit rather than inventing a fact about the file.
		return nil, ReasonParserUnavailable
	}
	return TextLayerAbsent, ""
}
