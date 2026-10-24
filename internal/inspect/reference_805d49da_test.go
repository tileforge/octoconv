package inspect

import (
	"bytes"
	"compress/zlib"
	"io"
	"strconv"

	"github.com/tileforge/octoconv/internal/convert/pdfref805d49da"
)

type referenceStreamStep struct {
	before       pdfTextScanState
	decoded      bool
	flate        bool
	out          int
	readErr      bool
	contribution int
	after        pdfTextScanState
}

type referenceRun struct {
	steps      []referenceStreamStep
	textState  pdfTextScanState
	textRow    string
	headRanges int
	tailRanges int
	headLen    int
	tailLen    int
	truncated  bool
	encrypted  bool
	merged     int
}

type referenceOracle struct {
	textLayer func(steps []referenceStreamStep) (row string, state pdfTextScanState)
	opaqueCap bool
	mergedCap bool
}

func referenceAnalyzePDF(r io.ReaderAt, size int64, maxBytes int64, tr *Tracker, o referenceOracle) (FamilyResult, referenceRun) {
	var run referenceRun
	res := FamilyResult{Characteristics: Characteristics{}}

	if size <= 0 || maxBytes <= 0 {
		return malformedPDFResult(res), run
	}

	inflateAllowance := maxBytes / 4

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
		return unknownPDFResult(res, ReasonReadFailed), run
	}

	truncated := size > maxBytes
	var tail []byte
	var tailOK bool
	var tailOff int64
	if truncated && tailReserve > 0 {
		tailOff = size - tailReserve
		if tailOff < 0 {
			tailOff = 0
		}
		tail, tailOK = readWindow(r, tailOff, tailReserve, size)
	}

	compressed := containsCompressionSignal(head) || (tailOK && containsCompressionSignal(tail))
	res.Characteristics["uses_compressed_object_streams"] = compressed

	hexEscape := containsPDFNameHexEscape(head) || (tailOK && containsPDFNameHexEscape(tail))

	if v, ok := extractPDFVersion(head); ok {
		res.Characteristics["pdf_version"] = v
	}

	headOpaque, headOpaqueCeilingHit, headRanges := referenceCappedOpaqueRanges(head, tr, o.opaqueCap)
	run.headRanges = headRanges
	run.headLen = len(head)
	run.tailLen = len(tail)
	var tailOpaque []pdfOpaqueRange
	var tailOpaqueCeilingHit bool
	if tailOK {
		tailOpaque, tailOpaqueCeilingHit, run.tailRanges = referenceCappedOpaqueRanges(tail, tr, o.opaqueCap)
	}

	encrypted := false

	for _, rt := range pdfRiskTokens {
		pos, found, filtered, ceilingHit := referenceScanTokenOccurrences(head, rt.tokens, headOpaque, tr)
		loc := pos
		foundOpaqueCeilingHit := headOpaqueCeilingHit
		if !found && rt.code == CodePDFEncrypted && tailOK {
			if tpos, tfound, tfiltered, tceilingHit := referenceScanTokenOccurrences(tail, rt.tokens, tailOpaque, tr); tfound {
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
				res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: ReasonWorkCeilingBytes})
				continue
			}
			if rt.code == CodePDFEncrypted {
				encrypted = true
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
			res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: ReasonWorkCeilingBytes})
			continue
		}

		if filtered {
			res.Checks = append(res.Checks, Check{Code: rt.code, Status: StatusUnknown, Reason: ReasonPDFStreamBodyToken})
			continue
		}

		opaqueRangesCeilingHit := headOpaqueCeilingHit || (tailOK && tailOpaqueCeilingHit)
		res.Checks = append(res.Checks, Check{
			Code:   rt.code,
			Status: verdictStatus(compressed, truncated, hexEscape, opaqueRangesCeilingHit),
			Reason: verdictReason(compressed, truncated, hexEscape, opaqueRangesCeilingHit),
		})
	}

	state := referenceScanPDFTextLayer(head, inflateAllowance, tr, &run.steps)
	if o.textLayer != nil {
		if row, expected := o.textLayer(run.steps); row != "" {
			run.textRow = row
			state = expected
			tr.MarkCeilingHit()
		}
	}
	run.textState = state
	run.truncated = truncated
	run.encrypted = encrypted
	textLayer, textLayerReason := pdfTextLayerVerdict(state, truncated, encrypted)
	res.Characteristics["text_layer"] = textLayer
	if textLayerReason != "" {
		res.Characteristics["text_layer_reason"] = string(textLayerReason)
	}

	run.merged = -1
	if !compressed && !hexEscape {
		run.merged = pdfref805d49da.MergedObjects(head)
	}
	count, reason := referenceExtractPDFPageCount(head, compressed, truncated, hexEscape, o.mergedCap && run.merged > pdfref805d49da.MaxIndexedObjects, tr)
	if count != nil {
		res.Characteristics["page_count"] = *count
	} else {
		res.Characteristics["page_count"] = nil
		if reason == "" {
			reason = ReasonMalformedInput
		}
		res.Characteristics["page_count_reason"] = string(reason)
	}

	return res, run
}

func referenceCappedOpaqueRanges(buf []byte, tr *Tracker, capped bool) ([]pdfOpaqueRange, bool, int) {
	var ranges []pdfOpaqueRange
	streamRanges, streamCeilingHit := referenceScanPDFStreamRanges(buf, tr)
	commentRanges, commentCeilingHit := referenceScanPDFCommentRanges(buf, tr)
	stringRanges, stringCeilingHit := referenceScanPDFLiteralAndHexStringRanges(buf, tr)
	ranges = append(ranges, streamRanges...)
	ranges = append(ranges, commentRanges...)
	ranges = append(ranges, stringRanges...)
	total := len(ranges)
	ceilingHit := streamCeilingHit || commentCeilingHit || stringCeilingHit
	if limit := pdfMaxOpaqueRanges(len(buf)); capped && total > limit {
		ranges = ranges[:limit]
		ceilingHit = true
		tr.MarkCeilingHit()
	}
	return mergeOpaqueRanges(ranges), ceilingHit, total
}

func referenceScanPDFStreamRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool) {
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

func referenceScanPDFCommentRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool) {
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

func referenceScanPDFLiteralAndHexStringRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool) {
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
				pos = start + 2
				if pos >= len(buf) {
					return ranges, ceilingHit
				}
				continue
			}
			closeIdx := bytes.IndexByte(buf[start+1:], '>')
			if closeIdx < 0 {
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

func referenceScanTokenOccurrences(buf []byte, tokens [][]byte, opaque []pdfOpaqueRange, tr *Tracker) (pos int, found bool, filtered bool, ceilingHit bool) {
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

func referenceExtractPDFPageCount(head []byte, compressed, truncated, hexEscape, mergedCapped bool, tr *Tracker) (*int, Reason) {
	if compressed {
		return nil, ReasonCompressedObjectStreams
	}
	if hexEscape {
		return nil, ReasonPDFNameEscape
	}

	values, ceilingHit := pdfref805d49da.PDFTypedDictUints(head, "Pages", "Count", maxPagesDictsScanned)
	if mergedCapped {
		values, ceilingHit = nil, true
	}
	if ceilingHit {
		tr.MarkCeilingHit()
	}
	var found *int
	if n := len(values); n > 0 {
		v := values[n-1]
		found = &v
	}

	if ceilingHit || truncated {
		return nil, ReasonWorkCeilingBytes
	}
	if found != nil {
		return found, ""
	}
	return nil, ReasonMalformedInput
}

func referenceScanPDFTextLayer(buf []byte, allowance int64, tr *Tracker, steps *[]referenceStreamStep) pdfTextScanState {
	state := pdfTextScanState{covered: true, remaining: allowance}

	ranges, ceilingHit, lexComplete := referenceScanValidatedStreamRanges(buf, tr)
	if !lexComplete {
		state.covered = false
		state.parserLimit = true
	}
	if ceilingHit {
		state.covered = false
		state.budgetHit = true
	}

	for _, rng := range ranges {
		if rng.start < 0 || rng.end > len(buf) || rng.start >= rng.end {
			continue
		}
		step := referenceStreamStep{before: state}
		data, class := referenceDecodePDFStream(buf, rng, tr, &state, &step)
		switch class {
		case pdfStreamSkipped:
			step.after = state
			*steps = append(*steps, step)
			continue
		case pdfStreamOpaque:
			state.covered = false
			state.parserLimit = true
			step.after = state
			*steps = append(*steps, step)
			continue
		case pdfStreamMalformed:
			state.covered = false
			state.malformed = true
			step.after = state
			*steps = append(*steps, step)
			continue
		}
		step.contribution = referenceCountTextOperatorArgBytes(data)
		state.nonWhitespace += step.contribution
		step.after = state
		*steps = append(*steps, step)
	}

	return state
}

func referenceScanValidatedStreamRanges(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool, complete bool) {
	complete = true
	i := 0
	for iter := 0; i < len(buf); iter++ {
		if iter >= maxTokenScanIterations {
			tr.MarkCeilingHit()
			return ranges, true, complete
		}

		switch buf[i] {
		case '(':
			next, terminated := skipPDFLiteral(buf, i)
			if !terminated {
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
			k := skipPDFWhitespaceAndComments(buf, i+2)
			if !bytes.HasPrefix(buf[k:], []byte("stream")) {
				i += 2
				continue
			}
			bodyStart := k + len("stream")
			endIdx := bytes.Index(buf[bodyStart:], []byte("endstream"))
			if endIdx < 0 {
				ranges = append(ranges, pdfOpaqueRange{start: bodyStart, end: len(buf)})
				return ranges, false, complete
			}
			bodyEnd := bodyStart + endIdx
			ranges = append(ranges, pdfOpaqueRange{start: bodyStart, end: bodyEnd})
			i = bodyEnd + len("endstream")
			continue
		default:
			i++
		}
	}
	return ranges, false, complete
}

func referenceDecodePDFStream(buf []byte, rng pdfOpaqueRange, tr *Tracker, state *pdfTextScanState, step *referenceStreamStep) ([]byte, pdfStreamClass) {
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

	body := bytes.TrimLeft(buf[rng.start:rng.end], "\r\n")
	if len(body) == 0 {
		return nil, pdfStreamSkipped
	}

	step.decoded = true
	zr, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		if bytes.Contains(dict, []byte("/Filter")) {
			return nil, pdfStreamOpaque
		}
		step.out = len(body)
		if !referenceChargeInflatedBytes(tr, len(body), state) {
			return nil, pdfStreamOpaque
		}
		return body, pdfStreamScannable
	}
	defer zr.Close()

	step.flate = true
	out, readErr := io.ReadAll(io.LimitReader(zr, pdfMaxInflatedStreamBytes+1))
	step.out = len(out)
	step.readErr = readErr != nil && readErr != io.EOF
	if len(out) > pdfMaxInflatedStreamBytes {
		state.budgetHit = true
		tr.MarkCeilingHit()
		return nil, pdfStreamOpaque
	}
	if readErr != nil && readErr != io.EOF {
		return nil, pdfStreamMalformed
	}
	if !referenceChargeInflatedBytes(tr, len(out), state) {
		return nil, pdfStreamOpaque
	}
	return out, pdfStreamScannable
}

func referenceChargeInflatedBytes(tr *Tracker, n int, state *pdfTextScanState) bool {
	if int64(n) > state.remaining {
		state.budgetHit = true
		tr.MarkCeilingHit()
		return false
	}
	state.remaining -= int64(n)
	return true
}

func referenceCountTextOperatorArgBytes(buf []byte) int {
	total := 0
	i := 0
	for iter := 0; i < len(buf); iter++ {
		if iter >= maxTokenScanIterations {
			break
		}
		switch buf[i] {
		case '(':
			content, next := readPDFLiteralString(buf, i)
			if referenceOperatorFollows(buf, next) {
				total += countNonWhitespaceBytes(content)
			}
			i = next
		case '<':
			if i+1 < len(buf) && buf[i+1] == '<' {
				i += 2
				continue
			}
			content, next := readPDFHexString(buf, i)
			if referenceOperatorFollows(buf, next) {
				total += countNonWhitespaceBytes(content)
			}
			i = next
		default:
			i++
		}
	}
	return total
}

func referenceOperatorFollows(buf []byte, i int) bool {
	for iter := 0; i < len(buf); iter++ {
		if iter >= maxTokenScanIterations {
			return false
		}
		c := buf[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0:
			i++
		case (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.':
			i++
		case c == ']':
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
