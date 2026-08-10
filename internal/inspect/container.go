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
func AnalyzeContainer(r io.ReaderAt, size int64, maxBytes int64, bombThreshold uint64) FamilyResult {
	res := FamilyResult{Characteristics: Characteristics{}}

	// The one and only call in this file to the shared sniffer: it owns the
	// ENTIRE decision of "is this even a zip, and if so what
	// format/macro/size does it declare" -- called exactly once, here, and
	// never re-implemented.
	cr, err := convert.SniffContainer(r, size)
	if err != nil {
		return notApplicableContainerResult(res)
	}

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
		// practice. Fail closed exactly like a malformed input rather than
		// trusting a partially-computed result.
		return malformedContainerResult(res)
	}

	appendMacroResult(&res, zr, cr.HasMacro)
	appendBombResult(&res, cr.TotalUncompressed, bombThreshold)
	appendExternalReferenceResult(&res, zr, cr.Format, maxBytes)
	setSheetAndPageCharacteristics(&res, zr, cr.Format)

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
	for _, code := range containerDomainCodes {
		res.Checks = append(res.Checks, Check{Code: code, Status: StatusUnknown, Reason: ReasonMalformedInput})
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
// content.xml/styles.xml -- looking for a literal TargetMode="External"
// (OOXML) or an xlink:href carrying a network/file URL scheme (ODF/epub).
// The first match found stops the scan immediately: one finding is enough
// to answer the question, and continuing to scan after that would only
// spend more of maxBytes' budget for an answer already known.
func appendExternalReferenceResult(res *FamilyResult, zr *zip.Reader, format string, maxBytes int64) {
	loc, ceilingHit := scanExternalReferences(zr, format, maxBytes)
	switch {
	case loc != "":
		sev, _ := SeverityOf(CodeExternalReference)
		res.Findings = append(res.Findings, Finding{Code: CodeExternalReference, Severity: sev, Location: loc})
	case ceilingHit:
		// Exhausting ANY of this scan's own ceilings (entries considered,
		// parts opened, per-part bytes, or the overall maxBytes budget) is
		// OUR OWN resource limit, never the file's fault -- it must never be
		// reported as clean (T-58-16's sibling concern, applied to the
		// container family).
		res.Checks = append(res.Checks, Check{Code: CodeExternalReference, Status: StatusUnknown, Reason: ReasonWorkCeilingBytes})
	default:
		res.Checks = append(res.Checks, Check{Code: CodeExternalReference, Status: StatusClean})
	}
}

// scanExternalReferences walks zr.File looking for the first external
// reference. It returns the matching part's Location and false, or
// ("", true) if any of this scan's own ceilings were exhausted before an
// answer was reached, or ("", false) if the whole (unbounded-by-ceiling)
// candidate set was searched and nothing was found.
//
// "Exhausted" is not only the obvious pre-loop cases (too many entries, too
// many parts opened, budget already gone before opening the next
// candidate) -- it is ALSO true of a part that WAS opened but whose read
// was cut short by io.LimitReader (case ceilingReached below), AND of a
// candidate part that was never opened AT ALL because its own DECLARED size
// already exceeded maxRelsPartBytes (codex PR9 finding 4 / T-SLE-04).
// Treating either case as an ordinary "found nothing" would repeat exactly
// the mistake D-02a exists to prevent in pdf.go: a part that was only
// PARTIALLY read, or never read at all because this scan chose not to pay
// for it, has not actually been ruled clean, and reporting it as such would
// be a false claim of completeness driven by this scan's own budget, not by
// the part's real content.
func scanExternalReferences(zr *zip.Reader, format string, maxBytes int64) (location string, ceilingHit bool) {
	partsOpened := 0
	budgetLeft := maxBytes
	if budgetLeft <= 0 {
		budgetLeft = maxRelsPartBytes // a caller passing maxBytes<=0 still gets ONE part's worth of budget, never zero -- zero would make every container's external-reference check spuriously "ceiling hit".
	}
	truncatedAnyPart := false

	for i, f := range zr.File {
		if i >= maxZipEntriesScanned {
			return "", true
		}
		if !isExternalRefCandidate(f.Name, format) {
			continue
		}
		if partsOpened >= maxRelsPartsScanned {
			return "", true
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
			continue
		}
		if budgetLeft <= 0 {
			return "", true
		}

		limit := int64(maxRelsPartBytes)
		if budgetLeft < limit {
			limit = budgetLeft
		}

		rc, operr := f.Open()
		if operr != nil {
			continue // a single unreadable part is not a ceiling hit -- keep scanning the rest
		}
		data, _ := io.ReadAll(io.LimitReader(rc, limit))
		rc.Close()
		budgetLeft -= int64(len(data))

		if containsExternalReference(data, format) {
			return f.Name, false
		}
		// A read that filled the entire allowance handed to io.LimitReader
		// is indistinguishable, from here, between "the part's real content
		// happened to end exactly there" and "there was more we never got
		// to see" -- the conservative (and correct, D-02a-consistent) choice
		// is to treat it as the latter, exactly like a PDF window truncated
		// by maxBytes never gets to claim clean.
		if int64(len(data)) >= limit {
			truncatedAnyPart = true
		}
	}
	if truncatedAnyPart {
		return "", true
	}
	return "", false
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

// odfExternalHrefSchemes are the xlink:href URL schemes this file treats as
// an external reference for ODF/epub -- network schemes plus file:, which
// can still exfiltrate/track via a local-resource probe.
var odfExternalHrefSchemes = [][]byte{
	[]byte(`xlink:href="http:`),
	[]byte(`xlink:href="https:`),
	[]byte(`xlink:href="ftp:`),
	[]byte(`xlink:href="file:`),
}

func containsExternalReference(data []byte, format string) bool {
	switch format {
	case "docx", "xlsx", "pptx":
		return bytes.Contains(data, []byte(`TargetMode="External"`))
	case "odt", "ods", "odp", "epub":
		for _, tok := range odfExternalHrefSchemes {
			if bytes.Contains(data, tok) {
				return true
			}
		}
		return false
	default:
		return false
	}
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
func setSheetAndPageCharacteristics(res *FamilyResult, zr *zip.Reader, format string) {
	switch format {
	case "xlsx":
		if n, ok := extractXLSXSheetCount(zr); ok {
			res.Characteristics["sheet_count"] = n
		} else {
			res.Characteristics["sheet_count"] = nil
			res.Characteristics["sheet_count_reason"] = string(ReasonMalformedInput)
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
func extractXLSXSheetCount(zr *zip.Reader) (int, bool) {
	for _, f := range zr.File {
		if f.Name != "xl/workbook.xml" {
			continue
		}
		if f.UncompressedSize64 > uint64(maxRelsPartBytes) {
			return 0, false
		}
		rc, err := f.Open()
		if err != nil {
			return 0, false
		}
		data, _ := io.ReadAll(io.LimitReader(rc, maxRelsPartBytes))
		rc.Close()
		return countSheetElements(data), true
	}
	return 0, false
}

func countSheetElements(data []byte) int {
	count := 0
	pos := 0
	marker := []byte("<sheet")
	for {
		idx := bytes.Index(data[pos:], marker)
		if idx < 0 {
			return count
		}
		abs := pos + idx
		next := abs + len(marker)
		if next < len(data) {
			switch data[next] {
			case ' ', '/', '>':
				count++
			}
		}
		pos = next
	}
}
