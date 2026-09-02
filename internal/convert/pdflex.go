package convert

import (
	"bytes"
	"fmt"
)

// A SMALL PDF TOKENIZER, AND WHY THIS FILE EXISTS AT ALL
//
// 63-CONTEXT.md D-02 said, in as many words: "no general PDF object parser. The
// resolver indexes N G obj offsets and reads one integer; anything more is a
// parser, and this phase is not writing one." That decision is reversed here,
// deliberately and on evidence.
//
// Three rounds of review found five defects in the byte-scanning version, and
// every one of them was the same defect: a token recognised by its bytes without
// regard to the lexical context it sits in.
//
//	round 1  "stream" matched inside "endstream"
//	round 1  "/Length" matched inside "/Length1"
//	round 1  object headers read out of stream payloads
//	round 3  object headers read out of COMMENTS
//	round 3  "stream" recognised inside a comment
//	round 3  ">>" inside a literal string counted as a dictionary close
//
// Patching a sixth would have been the fifth clever comparison in a row against
// a bound that keeps getting bypassed -- the pattern this project has already
// recorded in .planning/RETROSPECTIVE.md and resolved once before by replacing
// the comparison rather than refining it. A PDF's bytes have exactly four
// lexical contexts (whitespace, comment, literal string, hex string) plus tokens,
// and code that does not model those four will keep finding new ways to be
// wrong. So this models them. It is a tokenizer, not an object model: it does not
// build a document, resolve references beyond one integer, or decode anything.
//
// ONE TOKENIZER, TWO CONSUMERS -- ALSO A REVERSAL, ALSO ON EVIDENCE.
//
// sanitizepdf.go and sanitizeverify.go previously walked the file separately, so
// that "one subtle byte walker cannot feed both the redaction AND the check that
// is supposed to catch its mistakes". The reasoning was sound and the outcome was
// not: in rounds 1 through 3 the two walkers had IDENTICAL bugs every single
// time -- review said so explicitly, "the verifier repeats the same indexing
// behavior" -- because both were written by one author from one misunderstanding.
// The independence bought nothing on any occasion, and cost a second place to fix
// and a second chance to diverge on trivia (the two disagreed about whether a
// form feed is whitespace, which cost a valid file its output).
//
// What the two must NOT share is the proposition: the sanitizer decides what to
// remove, the verifier independently decides whether it is gone. Agreeing about
// where a stream's bytes are is a prerequisite for both, not a check of either --
// and there is already a test forcing that agreement, which means sharing loses
// no coverage that was real.
type pdfScan struct {
	// objects maps an object number to the offset just past its "N G obj"
	// header. Only headers found at top level, outside comments, strings and
	// stream payloads, are recorded.
	objects map[int]int
	// streams holds the payload range of every stream whose /Length resolved
	// and whose bytes agreed with it.
	streams [][2]int
	// unresolved counts streams whose length could not be established on this
	// pass -- for example one whose /Length object is defined later in the
	// file.
	unresolved int
}

// pdfLexInt is an integer token and where it started, so "N G obj" can be read
// from the two integers preceding the keyword.
type pdfLexInt struct {
	value int
	at    int
}

// scanPDF walks b once, in order, and reports the object headers and stream
// payloads it finds.
//
// ORDER IS LOAD-BEARING, not incidental: a stream's /Length is resolved only
// from objects seen BEFORE it, and a resolved payload is jumped over whole, so
// no byte inside a payload can ever be read as structure. That is what makes a
// decoy planted in a payload unable to poison the lookup that precedes it.
func scanPDF(b []byte) pdfScan { return scanPDFSeeded(b, nil) }

// scanPDFSeeded is scanPDF with an object index from an earlier pass already in
// hand, so a /Length object defined AFTER the stream that refers to it can be
// resolved on the second look. Seeding is the only thing that makes the retry
// different from the first pass; everything else about the walk is identical,
// which is why there is one implementation and not two.
func scanPDFSeeded(b []byte, seed map[int]int) pdfScan {
	res := pdfScan{objects: map[int]int{}}
	for k, v := range seed {
		res.objects[k] = v
	}

	var ints []pdfLexInt // the last two integer tokens at depth 0
	depth := 0

	// State for the dictionary that most recently closed at top level. A
	// "stream" keyword is only a stream keyword when it directly follows one.
	dictClosed := false
	lengthValue, lengthRef, haveLength := 0, false, false

	// State while inside a dictionary: /Length was just read, so the next value
	// token is its value.
	awaitLength := false
	pendingValue, pendingRef, pendingOK := 0, false, false

	i := 0
	for i < len(b) {
		c := b[i]

		// The four non-token contexts. None of them clears dictClosed:
		// whitespace and comments may legally separate a dictionary from its
		// stream keyword (ISO 32000-1 7.2.4).
		if isPDFSpace(c) {
			i++
			continue
		}
		if c == '%' {
			for i < len(b) && b[i] != '\n' && b[i] != '\r' {
				i++
			}
			continue
		}
		if c == '(' {
			i = pdfSkipLiteralString(b, i)
			ints = nil
			dictClosed = false
			continue
		}

		if c == '<' {
			if i+1 < len(b) && b[i+1] == '<' {
				depth++
				i += 2
				ints = nil
				dictClosed = false
				continue
			}
			i = pdfSkipHexString(b, i)
			ints = nil
			dictClosed = false
			continue
		}
		if c == '>' && i+1 < len(b) && b[i+1] == '>' {
			depth--
			i += 2
			if depth <= 0 {
				depth = 0
				dictClosed = true
				lengthValue, lengthRef, haveLength = pendingValue, pendingRef, pendingOK
				pendingValue, pendingRef, pendingOK = 0, false, false
			}
			ints = nil
			continue
		}

		if c == '/' {
			name, next := pdfReadName(b, i)
			i = next
			// /Length at the OUTERMOST level of this dictionary. A /Length in a
			// /DecodeParms sub-dictionary is not the stream's own.
			awaitLength = depth == 1 && name == "Length"
			ints = nil
			dictClosed = false
			continue
		}

		if c >= '0' && c <= '9' {
			v, next := pdfReadUint(b, i)
			if awaitLength {
				pendingValue, pendingOK = v, true
				pendingRef = pdfLooksLikeRef(b, next)
				awaitLength = false
			}
			ints = append(ints, pdfLexInt{value: v, at: i})
			if len(ints) > 2 {
				ints = ints[len(ints)-2:]
			}
			i = next
			dictClosed = false
			continue
		}

		// A keyword: a run of regular characters.
		word, next := pdfReadKeyword(b, i)
		switch {
		case word == "obj" && depth == 0 && len(ints) == 2:
			res.objects[ints[0].value] = next
			i = next
			ints = nil
			dictClosed = false

		case word == "stream" && dictClosed:
			payload := next
			if payload < len(b) && b[payload] == '\r' {
				payload++
			}
			if payload < len(b) && b[payload] == '\n' {
				payload++
			}

			n, ok := 0, false
			if haveLength {
				if lengthRef {
					if off, found := res.objects[lengthValue]; found {
						n, _, ok = pdfReadIntAt(b, off)
					}
				} else {
					n, ok = lengthValue, true
				}
			}
			if !ok || payload+n > len(b) || !pdfEndstreamFollows(b, payload+n) {
				// RESYNCHRONISATION, NOT A RANGE.
				//
				// The length is unknown, so where the payload ends is unknown
				// -- but continuing to tokenize from here would read binary
				// payload bytes as PDF syntax, and a single "(" in compressed
				// data swallows the rest of the file as a literal string. That
				// is what happened on Ghostscript's own output, whose streams
				// carry "/Length 6 0 R" with object 6 written AFTER them: the
				// first pass indexed one object out of a whole file and the
				// retry had nothing better to work with.
				//
				// So resume at the next "endstream". This is the lexical guess
				// the rest of this file exists to avoid -- and it is safe HERE
				// and only here, because it decides nothing: no range is
				// recorded, the stream stays counted as unresolved, and if the
				// retry cannot resolve it properly the whole scan fails closed.
				// A guess used to keep reading is not a guess used to answer.
				res.unresolved++
				if e := bytes.Index(b[payload:], []byte("endstream")); e >= 0 {
					i = payload + e + len("endstream")
				} else {
					i = next
				}
				dictClosed = false
				ints = nil
				haveLength = false
				continue
			}
			res.streams = append(res.streams, [2]int{payload, payload + n})
			i = payload + n
			dictClosed = false
			ints = nil
			haveLength = false

		default:
			i = next
			dictClosed = false
			ints = nil
		}
	}
	return res
}

// pdfSkipLiteralString returns the offset just past a "(...)" string, honouring
// backslash escapes and PDF's balanced-parenthesis nesting (ISO 32000-1 7.3.4.2).
//
// Without this, a dictionary carrying a string such as (>>) unbalanced the
// walker that used to scan backwards for the opening "<<", so the stream was
// silently treated as non-structural and its payload left unprotected.
func pdfSkipLiteralString(b []byte, i int) int {
	i++ // past '('
	nest := 1
	for i < len(b) {
		switch b[i] {
		case '\\':
			i += 2
			continue
		case '(':
			nest++
		case ')':
			nest--
			if nest == 0 {
				return i + 1
			}
		}
		i++
	}
	return len(b)
}

// pdfSkipHexString returns the offset just past a "<...>" hex string.
func pdfSkipHexString(b []byte, i int) int {
	i++ // past '<'
	for i < len(b) && b[i] != '>' {
		i++
	}
	if i < len(b) {
		i++
	}
	return i
}

// pdfReadName reads a "/Name" token and returns its text without the slash.
func pdfReadName(b []byte, i int) (string, int) {
	i++ // past '/'
	start := i
	for i < len(b) && !isPDFSpace(b[i]) && !isPDFDelimiter(b[i]) {
		i++
	}
	return string(b[start:i]), i
}

// pdfReadKeyword reads a run of regular characters. A single delimiter that is
// not part of any token is consumed on its own so the scan always advances.
func pdfReadKeyword(b []byte, i int) (string, int) {
	if isPDFDelimiter(b[i]) {
		return "", i + 1
	}
	start := i
	for i < len(b) && !isPDFSpace(b[i]) && !isPDFDelimiter(b[i]) {
		i++
	}
	return string(b[start:i]), i
}

func pdfReadUint(b []byte, i int) (int, int) {
	v := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		v = v*10 + int(b[i]-'0')
		if v > 1<<30 {
			v = 1 << 30
		}
		i++
	}
	return v, i
}

// pdfReadIntAt reads the first integer at or after off, skipping whitespace and
// comments. Used to read the value of an indirect /Length object.
func pdfReadIntAt(b []byte, off int) (int, int, bool) {
	i := off
	for i < len(b) {
		if isPDFSpace(b[i]) {
			i++
			continue
		}
		if b[i] == '%' {
			for i < len(b) && b[i] != '\n' && b[i] != '\r' {
				i++
			}
			continue
		}
		break
	}
	if i >= len(b) || b[i] < '0' || b[i] > '9' {
		return 0, i, false
	}
	v, next := pdfReadUint(b, i)
	return v, next, true
}

// pdfLooksLikeRef reports whether the tokens after an integer are "G R", making
// that integer an object number rather than a value.
func pdfLooksLikeRef(b []byte, i int) bool {
	for i < len(b) && isPDFSpace(b[i]) {
		i++
	}
	if i >= len(b) || b[i] < '0' || b[i] > '9' {
		return false
	}
	_, i = pdfReadUint(b, i)
	for i < len(b) && isPDFSpace(b[i]) {
		i++
	}
	return i < len(b) && b[i] == 'R'
}

func isPDFDelimiter(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

// pdfEndstreamFollows confirms the dictionary and the bytes agree: "endstream"
// must appear after at most a run of whitespace. Disagreement is malformed
// input, not something to guess at.
func pdfEndstreamFollows(b []byte, end int) bool {
	i := end
	for i < len(b) && isPDFSpace(b[i]) {
		i++
	}
	return bytes.HasPrefix(b[i:], []byte("endstream"))
}

func isPDFSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

// pdfStreamPayloads returns the payload range of every stream in b.
//
// A stream whose length cannot be established is an error, never a guess: a
// silent fallback to "scan for endstream" is exactly the defect this file
// replaced, and it would hide behind a passing test.
//
// One retry covers the legal case of a /Length object defined AFTER the stream
// that refers to it: the second pass starts from an object index built by the
// first, which by then knows about objects the forward scan had not reached.
func pdfStreamPayloads(b []byte) ([][2]int, error) {
	first := scanPDF(b)
	if first.unresolved == 0 {
		return first.streams, nil
	}

	second := scanPDFSeeded(b, first.objects)
	if second.unresolved > 0 {
		return nil, fmt.Errorf("%w: %d stream(s) declare a /Length this file does not resolve",
			ErrSanitizeMalformedInput, second.unresolved)
	}
	return second.streams, nil
}
