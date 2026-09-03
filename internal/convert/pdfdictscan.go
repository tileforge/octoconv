package convert

// This file (pdfdictscan.go): read one integer key out of the dictionaries that
// declare a given /Type, using pdflex.go's tokenizer rather than a byte scan.
//
// WHY IT EXISTS, AND WHY IT LIVES HERE RATHER THAN IN internal/inspect
//
// internal/inspect needed a PDF's declared page count for the sanitize
// admission gate (PROBE-01), and derived it by scanning bytes near a
// "/Type /Pages" marker. Review found four ways to be wrong, in three rounds,
// and every one of them is a member of the SAME class:
//
//	round 1  "/Type /Pages /Count 300" read out of a CONTENT STREAM
//	round 2  /Count taken from a following /Outlines dictionary
//	round 3  "/Count300" matched as the key "/Count" by prefix
//	round 3  "/Extra [ /Count 300 ]" read as a direct key of the dictionary
//
// pdflex.go's own header records that exact class, from a different bound, in
// almost the same words -- including "/Length" matched inside "/Length1" and a
// "/Length inside [ ... ]" read as a key of the enclosing dictionary. Both of
// those are the round-3 findings above, one key name over. The lesson was
// already written down in this repository and the byte scanner in
// internal/inspect repeated it anyway, because it had no model of what a token
// is.
//
// .planning/RETROSPECTIVE.md's rule applies: a bound that keeps getting
// bypassed is replaced, not patched. So this does not add a delimiter check and
// an array counter to a byte scan -- it moves the question to the tokenizer
// that already models PDF's four lexical contexts, and the class goes with it.
//
// It lives in internal/convert because pdflex.go does. internal/inspect already
// imports this package (container.go), so there is no new dependency edge and
// no cycle. Phase 58 D-02a refused a PARSER on untrusted bytes in the API
// process, and that decision was about pdfcpu: a large third-party dependency
// that builds a document model. This is a bounded tokenizer of this project's
// own, which builds nothing, resolves no references beyond one integer and
// decodes nothing -- pdflex.go's own header says so. The operator was shown the
// distinction and chose to cross it deliberately rather than have it crossed
// silently.

// PDFTypedDictUints walks b once and returns, in file order, the value of key
// for every dictionary that BOTH declares /Type /<typeName> and carries key as
// a DIRECT key of its own.
//
// "Direct" is the whole point: not a key of a nested dictionary, not a name
// inside an array value, and not a longer name that merely starts with key.
//
// ceilingHit reports that the walk could not be trusted to completion -- more
// than maxDicts dictionaries, or a stream whose /Length this window does not
// resolve, after which later bytes may be payload read as structure. Callers
// must treat it as "no answer", never as "the answers so far".
//
// A window that cuts a file mid-stream is the ordinary case for
// internal/inspect, so an unresolved length is not an error here; it is the
// reason the caller gets nothing.
func PDFTypedDictUints(b []byte, typeName, key string, maxDicts int) (values []int, ceilingHit bool) {
	// INDIRECT LENGTHS ARE THE ORDINARY CASE, NOT AN EDGE ONE.
	//
	// Ghostscript writes "/Length 6 0 R" with object 6 defined AFTER the stream
	// it belongs to -- pdflex.go's own comment records that, and this function's
	// first version gave up on it and reported a ceiling for every real PDF in
	// the corpus. So the object index is built first, from a forward pass and
	// from the xref, exactly as pdfStreamPayloads resolves the same problem.
	objects := scanPDF(b).objects
	for num, off := range pdfXrefObjects(b) {
		if _, seen := objects[num]; !seen {
			objects[num] = off
		}
	}

	type frame struct {
		isType  bool
		val     int
		haveVal bool
	}
	var stack []frame

	arrayDepth := 0
	dicts := 0

	// Pending states, each cleared by the next token that is not the value it
	// was waiting for.
	awaitType := false // "/Type" was just read, its value is the next name
	awaitVal := false  // key was just read, its value is the next integer

	// dictClosed / length carry the same meaning they do in pdflex.go's scan:
	// a "stream" keyword is only a stream keyword directly after a dictionary,
	// and that dictionary's /Length is what bounds its payload.
	dictClosed := false
	lengthVal, haveLength := 0, false
	awaitLength := false
	pendingLength, pendingRef, pendingHave := 0, false, false

	i := 0
	for i < len(b) {
		c := b[i]

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
			dictClosed, awaitType, awaitVal = false, false, false
			continue
		}
		if c == '<' {
			if i+1 < len(b) && b[i+1] == '<' {
				if dicts++; dicts > maxDicts {
					return nil, true
				}
				stack = append(stack, frame{})
				i += 2
				dictClosed, awaitType, awaitVal = false, false, false
				continue
			}
			i = pdfSkipHexString(b, i)
			dictClosed, awaitType, awaitVal = false, false, false
			continue
		}
		if c == '>' && i+1 < len(b) && b[i+1] == '>' {
			i += 2
			if n := len(stack); n > 0 {
				f := stack[n-1]
				stack = stack[:n-1]
				if f.isType && f.haveVal {
					values = append(values, f.val)
				}
				if len(stack) == 0 {
					dictClosed = true
					lengthVal, haveLength = pendingLength, pendingHave
					if pendingHave && pendingRef {
						if off, found := objects[pendingLength]; found {
							lengthVal, _, haveLength = pdfReadIntAt(b, off)
						} else {
							haveLength = false
						}
					}
					pendingLength, pendingRef, pendingHave = 0, false, false
				}
			}
			awaitType, awaitVal = false, false
			continue
		}
		if c == '[' {
			arrayDepth++
			i++
			dictClosed, awaitType, awaitVal = false, false, false
			continue
		}
		if c == ']' {
			if arrayDepth > 0 {
				arrayDepth--
			}
			i++
			dictClosed, awaitType, awaitVal = false, false, false
			continue
		}

		if c == '/' {
			// pdfReadName, not a prefix compare: "/Count300" is the name
			// "Count300", which is not "Count". That distinction is the round-3
			// finding, and it is already this tokenizer's job.
			name, next := pdfReadName(b, i)
			i = next
			if awaitType {
				if n := len(stack); n > 0 && name == typeName {
					stack[n-1].isType = true
				}
				awaitType = false
				dictClosed = false
				continue
			}
			// A key belongs to the dictionary on top of the stack only when we
			// are not inside one of its array values.
			direct := len(stack) > 0 && arrayDepth == 0
			awaitType = direct && name == "Type"
			awaitVal = direct && name == key
			awaitLength = len(stack) == 1 && arrayDepth == 0 && name == "Length"
			dictClosed = false
			continue
		}

		if c >= '0' && c <= '9' || ((c == '+' || c == '-') && i+1 < len(b) && b[i+1] >= '0' && b[i+1] <= '9') {
			v, next := pdfReadSignedInt(b, i)
			if awaitVal {
				if n := len(stack); n > 0 && v >= 0 {
					stack[n-1].val, stack[n-1].haveVal = v, true
				}
				awaitVal = false
			}
			if awaitLength {
				pendingLength, pendingRef, pendingHave = v, pdfLooksLikeRef(b, next), true
				awaitLength = false
			}
			i = next
			dictClosed, awaitType = false, false
			continue
		}

		word, next := pdfReadKeyword(b, i)
		if word == "stream" && dictClosed {
			payload := next
			if payload < len(b) && b[payload] == '\r' {
				payload++
			}
			if payload < len(b) && b[payload] == '\n' {
				payload++
			}
			if !haveLength || lengthVal < 0 || payload+lengthVal > len(b) ||
				!pdfEndstreamFollows(b, payload+lengthVal) {
				// The payload's end is unknown, so everything after it might be
				// payload read as structure -- which is round 1's finding. Give
				// up rather than resynchronise on a guess: this function only
				// ever answers, it never has to keep reading.
				return nil, true
			}
			i = payload + lengthVal
			dictClosed, haveLength = false, false
			continue
		}
		i = next
		dictClosed, awaitType, awaitVal = false, false, false
	}

	return values, false
}
