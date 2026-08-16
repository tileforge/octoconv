// This file (container.go) is the ZIP-container half of Phase 58's
// analyzers -- OOXML (docx/xlsx/pptx), ODF (odt/ods/odp), and epub. Unlike
// pdf.go, most of what this file reports is NOT new parsing: macro presence
// and the declared-uncompressed-bytes zip-bomb signal are already computed,
// in a single central-directory pass, by internal/convert's own container
// sniffer (docsniff.go). This file calls that function once (below) and reuses
// its answer AS-IS rather than recomputing the same fact a second way --
// one implementation, one answer, the same discipline behind D-03: a
// parallel reimplementation of "does this zip have a macro part" would
// eventually drift from the original the moment either one is edited
// without the other.
//
// What IS genuinely new here is external-reference detection (OOXML
// .rels parts with TargetMode="External"; ODF/epub content.xml/styles.xml
// with an xlink:href carrying a URL scheme) and the xlsx sheet count --
// both small, bounded reads of individual zip entries this file opens
// itself via the stdlib archive/zip API, following the exact same
// never-allocate-by-declared-size discipline every other parser in this
// project already applies.
package inspect

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/apaderin/octoconv/internal/convert"
)

// maxZipEntriesScanned is the hard ceiling on how many central-directory
// entries this file even LOOKS AT (name comparisons only, no decompression)
// while hunting for macro/external-reference/sheet-count candidates. A
// zip's own central directory entry count is fully attacker-controlled
// (like everything else in a zip's metadata) -- this mirrors
// internal/convert/cfb.go's cfbMaxDirSectors discipline exactly, right down
// to reusing its value: a hard structural-unit cap, not a context deadline
// (window.go's "why iterations, not context" note applies here too, even
// though this is a different package-internal loop shape).
const maxZipEntriesScanned = 4096

// maxRelsPartsScanned is the hard ceiling on how many parts this file
// actually OPENS (decompresses, however partially) while looking for
// external references -- deliberately much smaller than
// maxZipEntriesScanned, because opening a part costs real decompression
// work that merely comparing a central-directory NAME does not.
const maxRelsPartsScanned = 64

// maxRelsPartBytes is the hard ceiling on bytes read from any ONE opened
// part, enforced via io.LimitReader -- never by allocating a buffer sized
// from the part's own declared UncompressedSize64, which is exactly as
// attacker-controlled here as it is in every other format this project
// parses (T-58-18/T-58-20). A real .rels/content.xml/workbook.xml part is
// typically well under 10 KiB (58-RESEARCH.md Q1); 256 KiB is generous
// headroom, not a tight fit.
const maxRelsPartBytes = 256 << 10

// containerDomainCodes are the three Finding codes AnalyzeContainer ever
// produces a Finding or Check for -- every code below appears in EXACTLY
// one Finding or Check per call (the ValidateReport union invariant,
// status.go). CodeOOXMLMacro/CodeZipBombShape/CodeExternalReference here
// mirror pdf.go's pdfRiskTokens list in shape and purpose; CodeExternalReference
// is ALSO claimed by pdf.go for PDF's own /URI token -- one code, two
// sources for two different formats, exactly what the closed vocabulary in
// findings.go exists to allow (D-03).
var containerDomainCodes = []Code{CodeOOXMLMacro, CodeZipBombShape, CodeExternalReference}

// ooxmlMacroPartNames is the same closed set of macro-storage paths
// internal/convert/docsniff.go's ooxmlMacroParts already uses to COMPUTE
// HasMacro -- this file does NOT recompute that boolean (it trusts
// SniffContainer's answer completely); it re-checks these literal names
// SOLELY to recover a human-facing Location string for the Finding, since
// ContainerResult does not carry the matched part's path (docsniff.go
// intentionally keeps ContainerResult minimal). Duplicating a name table
// for display purposes only, while trusting the upstream boolean for the
// actual decision, does not create the drift risk D-03 warns about -- if
// this list and docsniff.go's ever disagreed, HasMacro (the decision) would
// still be correct and only the Location string could go stale, a
// cosmetic gap, not a correctness one.
var ooxmlMacroPartNames = map[string]bool{
	"word/vbaProject.bin": true,
	"xl/vbaProject.bin":   true,
	"ppt/vbaProject.bin":  true,
}

// AnalyzeContainer inspects a ZIP-shaped upload for the three closed
// container-domain finding codes plus sheet_count (xlsx)/page_count
// (docx/pptx/odt/odp/epub) characteristics. bombThreshold is the
// findings-level zip-bomb-shape threshold, deliberately independent of and
// milder than handleCreateJob's own hard rejection threshold
// (MaxDocumentUncompressedBytes) -- this function only ever REPORTS, it
// never rejects, so it is free to surface a smaller, more conservative
// ratio than the request-creation path would ever act on. It never returns
// an error: any parse failure becomes an unknown-status Check, exactly
// like AnalyzePDF (D-01's "unknown is a correct answer, not a failure").
func AnalyzeContainer(r io.ReaderAt, size int64, maxBytes int64, bombThreshold uint64, tr *Tracker) FamilyResult {
	res := FamilyResult{Characteristics: Characteristics{}}

	// The one and only call in this file to the shared sniffer: it owns the
	// ENTIRE decision of "is this even a zip, and if so what
	// format/macro/size does it declare" -- called exactly once, here, and
	// never re-implemented.
	cr, err := convert.SniffContainer(r, size)
	if err != nil {
		// convert.ErrNotAZip is a genuine answer about the upload: it is not
		// a zip container, so the container-domain codes do not apply to it.
		// Any OTHER error reached the sniffer through the io.ReaderAt, and
		// round 6's own neighbour audit flagged reporting THAT as
		// not_applicable -- "these checks do not apply to this file" is a
		// conclusion, and we have no grounds for it when the failure was our
		// inability to read the bytes at all.
		if errors.Is(err, convert.ErrNotAZip) {
			return notApplicableContainerResult(res)
		}
		return unknownContainerResult(res, ReasonReadFailed)
	}
	return AnalyzeContainerWith(cr, r, size, maxBytes, bombThreshold, tr)
}

// AnalyzeContainerWith is AnalyzeContainer for a caller that has ALREADY
// sniffed the container and can hand over the result.
//
// It exists because the API's format detection must sniff the archive to know
// what it is, and AnalyzeContainer would then sniff the identical bytes a
// second time -- two full walks of an attacker-controlled central directory
// per request where one suffices (codex PR #9 finding 5). The structural
// bound in internal/api now caps that work before archive/zip ever runs, so
// the second walk was no longer a denial-of-service vector, but it remained
// wasted work and, worse, a second opinion: two independent sniffs of the
// same upload could in principle disagree, and nothing would notice.
//
// Callers that have no ContainerResult in hand should keep using
// AnalyzeContainer, which sniffs and delegates here.
func AnalyzeContainerWith(cr convert.ContainerResult, r io.ReaderAt, size int64, maxBytes int64, bombThreshold uint64, tr *Tracker) FamilyResult {
	res := FamilyResult{Characteristics: Characteristics{}}

	if cr.DuplicateRootPart {
		// handleCreateJob treats this as fail-closed: a duplicate root part
		// makes the container's structure genuinely ambiguous, so it
		// refuses the request outright (internal/api/handlers.go). This
		// function only reports, it cannot refuse -- so the equivalent of
		// "refuse there" is "assert nothing about cleanliness here": every
		// container-domain code becomes unknown/malformed_input, and no
		// Characteristics are published at all, since any format/size/macro
		// fact derived from an ambiguous zip could itself be describing
		// either duplicate and would overclaim certainty this function does
		// not have.
		return duplicateRootPartContainerResult(res)
	}

	res.Characteristics["container_format"] = cr.Format
	res.Characteristics["declared_uncompressed_bytes"] = cr.TotalUncompressed
	res.Characteristics["has_macro"] = cr.HasMacro

	zr, zerr := zip.NewReader(r, size)
	if zerr != nil {
		// SniffContainer just parsed these exact bytes as a zip successfully
		// -- reaching here would mean archive/zip disagrees with itself
		// between two of its own entry points, which should not happen in
		// practice. Fail closed rather than trusting a partially-computed
		// result -- but fail closed with the RIGHT reason: round 6's own
		// neighbour audit found this site reporting ReasonMalformedInput
		// unconditionally, which turns "we could not read the upload" into a
		// claim about the upload's structure. archive/zip's own sentinel
		// errors say which of the two it is; anything that is not one of
		// them reached here through the io.ReaderAt, so it is a read
		// failure.
		if errors.Is(zerr, zip.ErrFormat) || errors.Is(zerr, zip.ErrAlgorithm) || errors.Is(zerr, zip.ErrChecksum) {
			return malformedContainerResult(res)
		}
		return unknownContainerResult(res, ReasonReadFailed)
	}

	appendMacroResult(&res, zr, cr.HasMacro)
	appendBombResult(&res, cr.TotalUncompressed, bombThreshold)
	appendExternalReferenceResult(&res, zr, cr.Format, maxBytes, tr)
	setSheetAndPageCharacteristics(&res, zr, cr.Format, tr)

	return res
}

func notApplicableContainerResult(res FamilyResult) FamilyResult {
	for _, code := range containerDomainCodes {
		res.Checks = append(res.Checks, Check{Code: code, Status: StatusNotApplicable, Reason: ReasonFormatNotApplicable})
	}
	return res
}

func duplicateRootPartContainerResult(res FamilyResult) FamilyResult {
	for _, code := range containerDomainCodes {
		res.Checks = append(res.Checks, Check{Code: code, Status: StatusUnknown, Reason: ReasonMalformedInput})
	}
	return res
}

func malformedContainerResult(res FamilyResult) FamilyResult {
	return unknownContainerResult(res, ReasonMalformedInput)
}

// unknownContainerResult is malformedContainerResult generalized over WHY
// nothing could be determined. Same shape -- every container-domain code
// becomes an honest unknown, never clean -- with the reason left to the
// caller, because "this archive is malformed" and "we could not read this
// archive" are different claims and only one of them is about the archive.
func unknownContainerResult(res FamilyResult, reason Reason) FamilyResult {
	for _, code := range containerDomainCodes {
		res.Checks = append(res.Checks, Check{Code: code, Status: StatusUnknown, Reason: reason})
	}
	return res
}

// appendMacroResult trusts cr.HasMacro (computed by SniffContainer)
// completely for the DECISION; it only re-walks the central directory
// (bounded by maxZipEntriesScanned) to recover a Location string. If, for
// any reason, no matching part name is found despite HasMacro being true
// (should not happen -- see the doc comment on ooxmlMacroPartNames), the
// Finding is still recorded, just with an empty Location, rather than
// silently downgrading a real positive signal to a Check.
func appendMacroResult(res *FamilyResult, zr *zip.Reader, hasMacro bool) {
	if !hasMacro {
		res.Checks = append(res.Checks, Check{Code: CodeOOXMLMacro, Status: StatusClean})
		return
	}
	sev, _ := SeverityOf(CodeOOXMLMacro)
	res.Findings = append(res.Findings, Finding{
		Code:     CodeOOXMLMacro,
		Severity: sev,
		Location: findMacroPartName(zr),
	})
}

func findMacroPartName(zr *zip.Reader) string {
	for i, f := range zr.File {
		if i >= maxZipEntriesScanned {
			return ""
		}
		if ooxmlMacroPartNames[f.Name] || strings.HasPrefix(f.Name, "Basic/") {
			return f.Name
		}
	}
	return ""
}

// appendBombResult compares cr.TotalUncompressed (SniffContainer's own
// saturating-add total, never recomputed here) against bombThreshold --
// the findings-level threshold, independent of and milder than
// handleCreateJob's hard rejection threshold. Location is left empty: a
// zip-bomb-shape finding is a fact about the WHOLE archive, not about any
// one entry.
func appendBombResult(res *FamilyResult, totalUncompressed, bombThreshold uint64) {
	if totalUncompressed > bombThreshold {
		sev, _ := SeverityOf(CodeZipBombShape)
		res.Findings = append(res.Findings, Finding{Code: CodeZipBombShape, Severity: sev})
		return
	}
	res.Checks = append(res.Checks, Check{Code: CodeZipBombShape, Status: StatusClean})
}

// appendExternalReferenceResult is the one genuinely new parse in this
// file: it opens zip.NewReader (already done by the caller) and reads the
// small subset of parts that can plausibly carry an external reference --
// OOXML's *.rels parts (under any _rels/ directory) or ODF/epub's
// content.xml/styles.xml -- looking for a TargetMode="External" attribute
// (OOXML) or an href attribute in the XLink namespace carrying a
// network/file URL scheme (ODF/epub), both parsed SEMANTICALLY with
// encoding/xml (see containsExternalRelationship/containsODFExternalHref),
// never by comparing raw bytes against a literal serialization. The first
// match found stops the scan immediately: one finding is enough to answer
// the question, and continuing to scan after that would only spend more of
// maxBytes' budget for an answer already known.
func appendExternalReferenceResult(res *FamilyResult, zr *zip.Reader, format string, maxBytes int64, tr *Tracker) {
	st := scanExternalReferences(zr, format, maxBytes, tr)
	switch {
	case st.location != "":
		sev, _ := SeverityOf(CodeExternalReference)
		res.Findings = append(res.Findings, Finding{Code: CodeExternalReference, Severity: sev, Location: st.location})
	case st.ceiling:
		// Exhausting ANY of this scan's own ceilings (entries considered,
		// parts opened, per-part bytes, or the overall maxBytes budget) is
		// OUR OWN resource limit, never the file's fault -- it must never be
		// reported as clean (T-58-16's sibling concern, applied to the
		// container family).
		res.Checks = append(res.Checks, Check{Code: CodeExternalReference, Status: StatusUnknown, Reason: ReasonWorkCeilingBytes})
	case st.malformed:
		// A candidate part was read to completion, within EVERY one of this
		// scan's own ceilings, and STILL failed to parse as XML -- a fact
		// about the FILE (malformed_input), never this scan's own resource
		// limit (PR9R5, container.go:462; see externalRefStatus's own doc
		// comment for why this is a THIRD outcome, not a synonym for ceiling).
		res.Checks = append(res.Checks, Check{Code: CodeExternalReference, Status: StatusUnknown, Reason: ReasonMalformedInput})
	default:
		res.Checks = append(res.Checks, Check{Code: CodeExternalReference, Status: StatusClean})
	}
}

// externalRefStatus is scanExternalReferences' own three-value result: at
// most one of location (non-empty), ceiling, or malformed is ever
// meaningful. ceiling means THIS SCAN's own resource limit fired before an
// answer was reached (entries considered, parts opened, per-part bytes, or
// the overall maxBytes budget -- Reason ReasonWorkCeilingBytes); malformed
// means every candidate part was read to completion WITHIN every one of
// those ceilings, but at least one failed to PARSE as its own declared XML
// format (Reason ReasonMalformedInput) -- a property of the FILE, not of
// this scan's own limits, and it must never be reported the same way a
// ceiling hit is (PR9R5, container.go:462: the previous version of this
// scan had no way to express this third outcome at all -- a decode failure
// and a scanned-clean miss both fell out as the same signal). Neither field
// set at all means the whole (bounded) candidate set was searched, every
// part that was opened parsed successfully, and none carried an external
// reference: a genuine clean.
type externalRefStatus struct {
	location  string
	ceiling   bool
	malformed bool
}

// scanExternalReferences walks zr.File looking for the first external
// reference. It returns an externalRefStatus (see that type's own doc
// comment for the three-way location/ceiling/malformed distinction).
//
// The ceiling outcome is not only the obvious pre-loop cases (too many
// entries, too many parts opened, budget already gone before opening the
// next candidate) -- it is ALSO true of a part that WAS opened but whose
// read was cut short by io.LimitReader (case ceilingReached below), of a
// candidate part that was never opened AT ALL because its own DECLARED size
// already exceeded maxRelsPartBytes (codex PR9 finding 4 / T-SLE-04), and,
// as of codex PR9 round 2 (container.go:334), of a candidate part that WAS
// opened but errored -- either f.Open itself failing (corrupt deflate
// stream, unsupported compression method) or io.ReadAll returning a
// mid-stream read error. Treating any of these as an ordinary "found
// nothing" would repeat exactly the mistake D-02a exists to prevent in
// pdf.go: a part that was only PARTIALLY read, never read at all because
// this scan chose not to pay for it, or never successfully read because
// this scan could not decode it, has not actually been ruled clean, and
// reporting it as such would be a false claim of completeness driven by
// this scan's own limits, not by the part's real content.
//
// The malformed outcome (PR9R5, container.go:462) is DIFFERENT in kind: a
// part that WAS read to completion, entirely within every one of the
// ceilings above, but whose content still failed to parse as XML. That is a
// fact about the FILE, not about this scan's own limits -- reporting it
// with the same signal as the paragraph above would mislabel a malformed
// document as this service's own resource exhaustion. A malformed result
// computed from a read this scan ALSO cut short (readErr != nil, or the
// io.LimitReader boundary trap) is an artifact of that truncation, not new
// information about the file -- truncatedAnyPart already accounts for it,
// and takes priority over malformedAnyPart below for exactly that reason.
func scanExternalReferences(zr *zip.Reader, format string, maxBytes int64, tr *Tracker) externalRefStatus {
	partsOpened := 0
	budgetLeft := maxBytes
	if budgetLeft <= 0 {
		budgetLeft = maxRelsPartBytes // a caller passing maxBytes<=0 still gets ONE part's worth of budget, never zero -- zero would make every container's external-reference check spuriously "ceiling hit".
	}
	truncatedAnyPart := false
	malformedAnyPart := false

	for i, f := range zr.File {
		if i >= maxZipEntriesScanned {
			tr.MarkCeilingHit()
			return externalRefStatus{ceiling: true}
		}
		if !isExternalRefCandidate(f.Name, format) {
			continue
		}
		if partsOpened >= maxRelsPartsScanned {
			tr.MarkCeilingHit()
			return externalRefStatus{ceiling: true}
		}
		partsOpened++

		// f.UncompressedSize64 comes straight from the central directory
		// and is fully attacker-controlled (T-58-18/T-58-20) -- it is used
		// ONLY to skip a part early when its DECLARED size already exceeds
		// this scan's per-part cap, never to size an allocation. A part
		// that lies about being small when it is actually huge would still
		// be caught by io.LimitReader below; a part that lies about being
		// huge when it is actually small is skipped WITHOUT ever being
		// opened -- a deliberately conservative tradeoff (trusting a
		// declared-huge size just enough to skip work, never trusting a
		// declared-small size enough to skip a bound) that keeps this loop's
		// total work bounded by declared metadata alone, before a single
		// byte of that part is ever read.
		//
		// Skipping the part this way is a REFUSAL to do the work, driven by
		// THIS scan's own per-part ceiling, not a clean read of the part's
		// real content (codex PR9 finding 4 / T-SLE-04): if this is the
		// only external-reference candidate the container has, silently
		// `continue`-ing here would let the caller report the whole file
		// clean having never actually looked at its one relevant part.
		// truncatedAnyPart must be set here for exactly the same reason the
		// io.LimitReader-truncation branch below sets it.
		if f.UncompressedSize64 > uint64(maxRelsPartBytes) {
			truncatedAnyPart = true
			tr.MarkCeilingHit()
			continue
		}
		if budgetLeft <= 0 {
			tr.MarkCeilingHit()
			return externalRefStatus{ceiling: true}
		}

		limit := int64(maxRelsPartBytes)
		if budgetLeft < limit {
			limit = budgetLeft
		}

		rc, operr := f.Open()
		if operr != nil {
			// codex PR9 round 2 (container.go:334): a part this scan
			// considers a genuine candidate but cannot even OPEN (corrupt
			// deflate stream, unsupported compression method) has not been
			// looked at at all -- the exact same "refusal to look" as the
			// declared-size skip above, just discovered a step later. If
			// this is the container's only relevant part, silently
			// `continue`-ing here would let the caller report the whole
			// file clean having inspected none of its content.
			truncatedAnyPart = true
			tr.MarkCeilingHit()
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, limit))
		rc.Close()
		budgetLeft -= int64(len(data))

		switch containsExternalReference(data, format) {
		case xmlScanFound:
			return externalRefStatus{location: f.Name}
		case xmlScanMalformed:
			malformedAnyPart = true
		}
		if readErr != nil {
			// codex PR9 round 2 (container.go:334)'s sibling case: the part
			// WAS opened, but the read stopped partway through with a real
			// error (e.g. a decompression error mid-stream), not merely
			// hitting io.LimitReader's allowance. Whatever bytes were read
			// before the error are still searched above (data may already
			// contain the marker), but a MISS in a partial, error-terminated
			// read is not proof the marker is absent from the rest of the
			// part this scan never got to see.
			truncatedAnyPart = true
			tr.MarkCeilingHit()
		}
		// A read that filled the entire allowance handed to io.LimitReader
		// is indistinguishable, from here, between "the part's real content
		// happened to end exactly there" and "there was more we never got
		// to see" -- the conservative (and correct, D-02a-consistent) choice
		// is to treat it as the latter, exactly like a PDF window truncated
		// by maxBytes never gets to claim clean.
		if int64(len(data)) >= limit {
			truncatedAnyPart = true
			tr.MarkCeilingHit()
		}
	}
	switch {
	case truncatedAnyPart:
		return externalRefStatus{ceiling: true}
	case malformedAnyPart:
		return externalRefStatus{malformed: true}
	default:
		return externalRefStatus{}
	}
}

// isExternalRefCandidate reports whether name is a part this format's
// external-reference convention could plausibly live in.
func isExternalRefCandidate(name, format string) bool {
	switch format {
	case "docx", "xlsx", "pptx":
		return strings.HasSuffix(name, ".rels") && strings.Contains(name, "_rels/")
	case "odt", "ods", "odp", "epub":
		return name == "content.xml" || name == "styles.xml"
	default:
		return false
	}
}

// xlinkNamespace is the URI the XLink specification conventionally binds
// to the "xlink" prefix -- but XML namespace binding is a property of
// WHICH URI a prefix resolves to, never of the prefix's own spelling: any
// document is free to bind this exact namespace to a different prefix (or
// even declare it as a default, unprefixed namespace on some element) and
// every attribute using that other prefix still means exactly the same
// thing. containsODFExternalHref below matches THIS namespace URI, never
// the literal string "xlink:" (PR9R5 finding, container.go:419: "an
// equivalent attribute using another prefix bound to the XLink namespace"
// carries an identical external reference that a literal-prefix match would
// miss entirely).
const xlinkNamespace = "http://www.w3.org/1999/xlink"

// xmlScanResult is the three-value outcome of scanning ONE already-read
// part's content for a format-specific external-reference marker.
// xmlScanFound and xmlScanNotFound are exactly what a bool return used to
// express; xmlScanMalformed is the THIRD state PR9R5's own review finding
// (container.go:462) shows was missing entirely: a part that was fully read
// (this scan's own ceilings never fired) but whose XML failed to decode. A
// bool return can only ever hold two of these three states, so returning
// bool forced every caller to fold "could not determine" into either
// "found" or "not found" -- in practice always the latter, which is exactly
// the false-absence-claim class quick/260811-r3s already closed once for
// ceiling exhaustion and this task closes again for decode failure.
// Declaring a THIRD named value here, rather than adding a second bool
// return alongside the first (which a caller remains free to ignore), makes
// the ambiguity impossible to construct: every call site must name which of
// the three results it got, not merely check a single true/false.
type xmlScanResult int

const (
	xmlScanNotFound xmlScanResult = iota
	xmlScanFound
	xmlScanMalformed
)

// odfExternalHrefSchemes are the URL schemes this file treats as an
// external reference when they prefix an xlink:href attribute's VALUE --
// already extracted by containsODFExternalHref's semantic XML parse, never
// matched against the part's raw serialized bytes -- network schemes plus
// file:, which can still exfiltrate/track via a local-resource probe.
var odfExternalHrefSchemes = []string{"http:", "https:", "ftp:", "file:"}

func containsExternalReference(data []byte, format string) xmlScanResult {
	switch format {
	case "docx", "xlsx", "pptx":
		return containsExternalRelationship(data)
	case "odt", "ods", "odp", "epub":
		return containsODFExternalHref(data)
	default:
		return xmlScanNotFound
	}
}

// containsExternalRelationship reports whether data (an OOXML .rels part)
// declares ANY <Relationship> element with TargetMode="External", parsed
// with encoding/xml (stdlib, D-02 -- a .rels part is small and bounded by
// maxRelsPartBytes, so a real XML parse here is cheap, unlike the whole-PDF
// byte scan D-02a deliberately avoids for a much larger, unbounded-shape
// input).
//
// PR9R4 finding (container.go:414), verbatim: "A valid OOXML relationship
// can use single quotes or whitespace around '=', for example
// `TargetMode = 'External'`, but this exact byte match recognizes only the
// double-quoted, unspaced serialization. Such a document reaches the clean
// external_reference result even though it contains an external target."
// The prior version of this function was a single literal byte-sequence
// comparison against the exact bytes TargetMode="External" -- exactly the
// same class of defect as comparing hex-escaped PDF names byte-for-byte: a
// STRUCTURED format compared as an opaque byte sequence, blind to any of
// its other legal serializations (attribute order, quote character,
// whitespace around '='). encoding/xml's own tokenizer handles all of those
// for free, because it actually parses the attribute rather than
// pattern-matching its serialized bytes.
//
// PR9R5 finding (container.go:462), verbatim: "When a fully read .rels part
// is malformed or otherwise rejected by encoding/xml before an external
// relationship is reached, this returns the same false as a successful
// parse with no match. Because scanExternalReferences receives no error
// signal and no byte ceiling was hit, it reports external_reference clean."
// The PREVIOUS version of THIS doc comment rationalized returning false on
// ANY decode error by pointing at the caller's own truncation bookkeeping
// (scanExternalReferences's truncatedAnyPart) -- that rationale was WRONG,
// and is deleted rather than kept alongside the fix below: truncatedAnyPart
// is set only when THIS SCAN's own byte ceiling actually fired (a
// declared-size skip, an f.Open failure, a mid-read error, or the
// io.LimitReader boundary trap), never merely because dec.Token() returned
// an error. A part that was read to completion WITHIN budget and STILL
// failed to parse (genuinely malformed XML) hits none of those ceilings, so
// the caller's truncation bookkeeping was never going to catch it -- this
// function now distinguishes the two outcomes itself: io.EOF (the decoder
// consumed the whole part and found no match -- an honest "not found") from
// any other decode error (the part's XML could not be parsed at all --
// xmlScanMalformed, never silently folded into "not found").
func containsExternalRelationship(data []byte) xmlScanResult {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return xmlScanNotFound
			}
			return xmlScanMalformed
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "TargetMode" && attr.Value == "External" {
				return xmlScanFound
			}
		}
	}
}

// containsODFExternalHref reports whether data (an ODF/epub content.xml or
// styles.xml part) declares any element attribute in the XLink namespace
// (see xlinkNamespace's own doc comment), local name "href", whose value
// carries one of odfExternalHrefSchemes -- parsed with encoding/xml (D-02,
// exactly like containsExternalRelationship above), never matched against
// the part's raw serialized bytes.
//
// PR9R5 finding (container.go:419), verbatim: "For ODT/ODS/ODP/EPUB, valid
// XML such as xlink:href = 'https://example.com' or an equivalent attribute
// using another prefix bound to the XLink namespace does not contain any of
// these exact byte strings. A fully read document using either legal
// serialization therefore falls through to a clean external_reference check
// despite containing an external URL." This is the THIRD appearance of one
// class this project has now closed three times (PDF hex-escaped names,
// OOXML's TargetMode quote/whitespace variants, and now this): a STRUCTURED
// format compared as an opaque byte sequence, blind to any of its other
// legal serializations. encoding/xml resolves whichever prefix a document
// actually bound to the XLink namespace on its own (see the xmlns handling
// in encoding/xml's own Token(): a StartElement's xmlns: declarations are
// registered into the namespace stack BEFORE that SAME element's other
// attribute names are translated, so xmlns:anyPrefix="http://www.w3.org/
// 1999/xlink" declared on the very element that also carries
// anyPrefix:href is enough) -- this function never has to know which prefix
// a given document chose.
//
// Round 5's own sweep (PR9R5-SWEEP-CLASSES) then found the SAME class one
// level down, inside the fix above, and this is why hrefAttrIsExternal
// exists rather than a direct comparison here:
//
//   - encoding/xml only fills Name.Space with the RESOLVED namespace URI
//     when the document actually bound the prefix. Probed directly against
//     Go 1.26: `<r xmlns:xlink="http://www.w3.org/1999/xlink"><a
//     xlink:href="http://evil"/></r>` yields Space=="http://www.w3.org/
//     1999/xlink", and so does the same document using any other prefix
//     bound to that URI (the case the review named) -- but
//     `<r><a xlink:href="http://evil"/></r>`, with the prefix left
//     UNDECLARED, yields Space=="xlink", the literal prefix. Comparing only
//     against the URI therefore lets an undeclared-prefix document reach a
//     clean external_reference verdict, which is precisely the defect this
//     function was written to close, expressed one serialization further
//     out. An undeclared prefix is not namespace-well-formed, but
//     encoding/xml accepts it without error, so "the parser tolerated it"
//     is exactly the situation where refusing to claim cleanliness matters.
//   - A URI scheme is case-INSENSITIVE (RFC 3986 §3.1), so `HTTP://evil`
//     and `Https://evil` are legal spellings of the same external target
//     that a case-sensitive prefix comparison misses.
//
// Both are handled by widening what counts as a match, never by narrowing
// what counts as clean: an over-report costs an external_reference FINDING
// on a document that may not have deserved one, while a miss costs a false
// clean -- and this project has now paid the second cost four times.
func containsODFExternalHref(data []byte) xmlScanResult {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return xmlScanNotFound
			}
			return xmlScanMalformed
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if hrefAttrIsExternal(attr) {
				return xmlScanFound
			}
		}
	}
}

// hrefAttrIsExternal reports whether one already-parsed XML attribute is an
// href carrying an external scheme. See containsODFExternalHref's own doc
// comment for why the namespace test accepts three spellings rather than
// only the resolved XLink URI, and why the scheme test is
// case-insensitive.
//
// An EMPTY Space (a bare href="http:...", no prefix at all) is deliberately
// NOT matched, and that asymmetry is the whole point of listing the cases
// explicitly. The bytes of an undeclared-prefix document literally say
// `xlink:href`, so a lenient consumer keying off the qualified name -- as
// real ODF importers do -- resolves it as XLink's own href and fetches the
// target. A bare `href` says no such thing: ODF's schema has no unprefixed
// href, nothing resolves it, and matching it would only manufacture
// external_reference findings for documents that carry no external target
// (see TestAnalyzeContainerODFHrefWithoutNamespaceBindingStaysClean).
func hrefAttrIsExternal(attr xml.Attr) bool {
	if attr.Name.Local != "href" {
		return false
	}
	switch attr.Name.Space {
	case xlinkNamespace, "xlink":
	default:
		return false
	}
	for _, scheme := range odfExternalHrefSchemes {
		if len(attr.Value) >= len(scheme) && strings.EqualFold(attr.Value[:len(scheme)], scheme) {
			return true
		}
	}
	return false
}

// setSheetAndPageCharacteristics fills in exactly the characteristics
// applicable to cr.Format -- a spreadsheet gets sheet_count and never
// page_count (a page count is not a meaningful question for a table); every
// other office/ODF/epub format gets an honest page_count=nil,
// parser_unavailable and never sheet_count. The true RENDERED page count
// of a docx/pptx/odt/odp/epub depends on page size, margins, fonts, and
// content reflow -- output of a layout engine, not something derivable
// from the XML structure alone (58-RESEARCH.md: "genuinely NOT worth
// attempting"; this is exactly why pdfinfo/LibreOffice exist as separate
// binaries today, and exactly why D-01 forbids adding them to this
// container). Reporting an honest nil here is not laziness, it is the same
// discipline PROG-V2-02 already applies elsewhere in this project: a
// guessed number is worse than an admitted unknown.
func setSheetAndPageCharacteristics(res *FamilyResult, zr *zip.Reader, format string, tr *Tracker) {
	switch format {
	case "xlsx":
		if n, ok, ceilingHit := extractXLSXSheetCount(zr, tr); ok {
			res.Characteristics["sheet_count"] = n
		} else {
			// codex PR9 round 2 audit finding (container.go, found applying
			// the same table this session built for scanExternalReferences
			// to extractXLSXSheetCount): a declared-size skip or an
			// f.Open/io.ReadAll error here is THIS scan's own refusal to
			// read the part, not a structural fact about the file -- the
			// exact same class scanExternalReferences already closed for
			// external_reference, just unaddressed for sheet_count.
			reason := ReasonMalformedInput
			if ceilingHit {
				reason = ReasonWorkCeilingBytes
			}
			res.Characteristics["sheet_count"] = nil
			res.Characteristics["sheet_count_reason"] = string(reason)
		}
	case "ods":
		// Sheet count for ODF spreadsheets would require parsing
		// content.xml's <table:table> elements -- a different, not-yet-
		// written parser from the OOXML workbook.xml path above (D-01: no
		// parser written yet is an honest unknown, not a failure).
		res.Characteristics["sheet_count"] = nil
		res.Characteristics["sheet_count_reason"] = string(ReasonParserUnavailable)
	case "docx", "pptx", "odt", "odp", "epub":
		res.Characteristics["page_count"] = nil
		res.Characteristics["page_count_reason"] = string(ReasonParserUnavailable)
	}
}

// extractXLSXSheetCount reads xl/workbook.xml (bounded by maxRelsPartBytes,
// via io.LimitReader, never allocated by declared size) and counts
// "<sheet " / "<sheet/>" elements in the workbook's <sheets> list. The scan
// is bounded by the already-capped data slice's length alone -- at most
// maxRelsPartBytes bytes -- so it needs no separate iteration ceiling of
// its own, unlike pdf.go's page-count scan (which walks a much larger,
// maxBytes-sized window).
//
// ceilingHit distinguishes THIS scan's own refusal to read the part
// (declared size exceeding maxRelsPartBytes, f.Open failing, io.ReadAll
// erroring, or the LimitReader-boundary trap below) from workbook.xml
// genuinely not existing in the archive -- audit finding applying
// scanExternalReferences's own already-fixed discipline to this function,
// which had not been touched by either review round.
//
// quick/260811-r3s (container.go:497 finding): io.LimitReader(rc,
// maxRelsPartBytes) returns EXACTLY maxRelsPartBytes bytes with io.EOF==nil
// when the real part decompresses to MORE than that -- io.LimitReader never
// asks rc for one more byte once its own limit is reached, so a part that is
// truncated at the boundary is byte-for-byte indistinguishable from a part
// whose real content happens to end exactly there. The fix reads ONE EXTRA
// byte (maxRelsPartBytes+1): if that many bytes actually come back, the part
// exceeded this scan's budget and every <sheet> element past byte
// maxRelsPartBytes was never seen -- reporting a count computed from a
// truncated prefix would have silently omitted trailing sheets, exactly the
// finding's own worked example ("a ZIP entry declaring exactly
// maxRelsPartBytes but decompressing to more data").
func extractXLSXSheetCount(zr *zip.Reader, tr *Tracker) (count int, ok bool, ceilingHit bool) {
	for _, f := range zr.File {
		if f.Name != "xl/workbook.xml" {
			continue
		}
		if f.UncompressedSize64 > uint64(maxRelsPartBytes) {
			tr.MarkCeilingHit()
			return 0, false, true
		}
		rc, err := f.Open()
		if err != nil {
			tr.MarkCeilingHit()
			return 0, false, true
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxRelsPartBytes+1))
		rc.Close()
		if readErr != nil {
			tr.MarkCeilingHit()
			return 0, false, true
		}
		if int64(len(data)) > maxRelsPartBytes {
			// The extra byte came back: this part's real content runs past
			// what this scan is willing to read. A count computed from the
			// maxRelsPartBytes-byte prefix could silently omit sheets
			// declared after that boundary -- our own ceiling, not a fact
			// about the workbook, so no partial count is published.
			tr.MarkCeilingHit()
			return 0, false, true
		}
		n, ok := countSheetElements(data)
		if !ok {
			// The part was read in full and still did not parse as XML.
			// That is a fact about the FILE, and it must not be published
			// as a count -- least of all as zero, which reads as "this
			// workbook has no sheets" (PR9R11, container.go:812).
			return 0, false, false
		}
		return n, true, false
	}
	return 0, false, false
}

// countSheetElements counts <sheet> elements in an xlsx workbook part by
// PARSING it, and reports ok=false if the part does not parse at all.
//
// PR9R11 finding (container.go:812), verbatim: "For a valid workbook that
// binds the spreadsheet namespace to a prefix and serializes sheets as
// `<x:sheet>`, this raw `<sheet` search finds none and publishes
// `sheet_count: 0` with complete coverage. Raw matching can also count the
// same text inside XML comments; use the XML decoder and count actual sheet
// start elements by namespace/local name instead of treating the
// serialization as opaque bytes."
//
// This is the FIFTH appearance of one class in this review -- PDF hex-
// escaped names, OOXML TargetMode, ODF xlink:href, the undeclared xlink
// prefix, and now this -- and the one that stings, because round 5's own
// class sweep was declared complete across this package while this call
// site sat in that sweep's own grep output and never made it into its
// table. A sweep that drops one of its own hits is not a sweep.
//
// Counting by local name deliberately ignores which prefix a workbook bound
// (encoding/xml resolves that on its own) and is bounded by the same
// maxRelsPartBytes ceiling the caller already enforces, so no new work is
// introduced -- only the same bytes, read as XML rather than as text.
// spreadsheetMLNamespaces are the resolved namespace URIs a <sheet> element
// must live in to be a worksheet declaration: OOXML transitional (the one
// every mainstream producer emits) and OOXML strict (ISO/IEC 29500-4).
//
// PR9R12 finding (container.go:853), verbatim: "matching only Name.Local
// also accepts foreign-namespace extension markup. For example, a workbook
// with one real <sheets><sheet/></sheets> list and an extension containing
// <e:sheets><e:sheet/></e:sheets> is parsed successfully and publishes
// sheet_count: 2 with complete coverage."
//
// PR9R13 (container.go:856) then found the hole in accepting the empty
// namespace GLOBALLY, which is what round 12 did: "when the workbook root
// uses SpreadsheetML but a nested extension resets the default namespace,
// such as <ext><sheets xmlns=""><sheet/></sheets></ext>, this global
// empty-namespace allowance counts the extension leaf as a worksheet".
//
// The reasoning that admitted it was sound as far as it went -- extension
// markup is namespaced by construction, so an unnamespaced part has nothing
// to be confused with -- and stopped one step short: xmlns="" RESETS to the
// empty namespace mid-document, so a namespaced workbook can contain
// unnamespaced elements after all. The allowance is therefore no longer
// global. The workbook's OWN namespace is taken from its root element, and
// sheets/sheet must match THAT: a wholly unnamespaced workbook still counts
// (its root is unnamespaced too), while an unnamespaced island inside a
// SpreadsheetML document no longer does.
var spreadsheetMLNamespaces = map[string]bool{
	"http://schemas.openxmlformats.org/spreadsheetml/2006/main": true,
	"http://purl.oclc.org/ooxml/spreadsheetml/main":             true,
}

func countSheetElements(data []byte) (count int, ok bool) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	inSheets := false
	// workbookNS is the namespace the ROOT element declared, and every
	// sheets/sheet that counts must match it exactly. Deciding this from the
	// document rather than from a fixed allow-list is what closes the
	// xmlns="" reset (PR9R13): the root's namespace is a property of the
	// document as a whole, which a nested element cannot redefine.
	workbookNS := ""
	haveRoot := false
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				// PR9R14 (container.go:881): reaching EOF without ever
				// seeing a root element means this part was empty, or held
				// nothing but whitespace and an XML declaration. That is not
				// a workbook that happens to declare no sheets -- it is not
				// a workbook at all, and returning ok=true here published
				// sheet_count: 0 with complete coverage for it. "Zero
				// sheets" is a claim; this part supports no claims.
				return count, haveRoot
			}
			return 0, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !haveRoot {
				haveRoot = true
				workbookNS = t.Name.Space
				// The root must be a workbook, by NAME as well as by
				// namespace (PR9R14 took the namespace and left the name
				// unchecked, so <notWorkbook><sheets><sheet/></sheets>
				// published sheet_count: 1 as a parsed characteristic).
				//
				// ok=false rather than a zero count: this part is not a
				// workbook, so it supports no statement about how many
				// sheets a workbook has -- the same reasoning as the empty
				// part above, and the reason zero is never the safe
				// fallback here. Zero is an answer; "not a workbook" is the
				// absence of one.
				if t.Name.Local != "workbook" {
					return 0, false
				}
				// The only namespace accepted without being one of the two
				// known SpreadsheetML URIs is the empty one, i.e. a wholly
				// unnamespaced (minimal or hand-written) workbook.
				if workbookNS != "" && !spreadsheetMLNamespaces[workbookNS] {
					return 0, false
				}
			}
			if t.Name.Space != workbookNS {
				continue
			}
			switch t.Name.Local {
			case "sheets":
				inSheets = true
			case "sheet":
				// Only the <sheets> list declares worksheets. Scoping to it
				// keeps an unrelated element that happens to be named
				// "sheet" elsewhere in the part from inflating the count,
				// which the previous byte scan could not distinguish at all.
				if inSheets {
					count++
				}
			}
		case xml.EndElement:
			if t.Name.Space == workbookNS && t.Name.Local == "sheets" {
				inSheets = false
			}
		}
	}
}
