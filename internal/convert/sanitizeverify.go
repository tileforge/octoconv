package convert

// This file (sanitizeverify.go): independent verification of sanitizer output
// (SANI-04).
//
// WHY THE VERIFIER MUST NOT SHARE CODE WITH THE SANITIZER
//
// The research measured the failure mode directly. A single `pdfwrite` pass
// regenerates /Info -- and the values it was invoked to remove reappear in an
// XMP packet the pass itself creates. A verifier that read /Info and reported
// "metadata removed" would be right about /Info and wrong about the file. It
// would not be proving cleanliness; it would be proving its own blindness.
//
// So the implementation that checks is never the implementation that removed:
//
//	PDF   -- ghostscript removes, pdfcpu checks (D-03)
//	OOXML -- Go archive/zip removes, unzip + libmagic check (D-04)
//
// THREE OUTCOMES, ALL DISTINGUISHABLE (D-07)
//
//	clean          -> nil; output may proceed to storage
//	residue found  -> ErrSanitizeResidue, naming the class found
//	cannot parse   -> ErrSanitizeUnverifiable, its own separate cause
//
// The third is the one worth defending. Handing back a file marked
// "unverified" loses to a specific failure: a file that went through an
// operation named *sanitize* will be treated as clean by everyone who did not
// read the report, and that is most people. It is the same principle phase 59
// held for `absent`: "could not look" is never "nothing there".
//
// WHY pdfcpu IS ALLOWED HERE AT ALL
//
// It is the project's first parsing dependency pointed at untrusted bytes, and
// phase 58 (D-02a) refused it in the API process for exactly that reason. It
// was agreed on three conditions, all of which hold here and only here: it
// runs in an isolated worker, under a measured timeout, with cgroup limits.
// A future attempt to call pdfcpu from cmd/api should read that decision
// before assuming this file is a precedent -- it is not; it is the exception
// the conditions were written for.
//
// WHAT pdfcpu BUYS THAT PHASE 58'S BYTE SCANNER COULD NOT
//
// It decompresses /ObjStm. An object hidden inside an object stream is
// structurally invisible to a byte scanner (internal/inspect/pdf.go documents
// that limit); it is ordinary to a parser.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	pdftypes "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// The two failure modes are separate sentinels because they mean different
// things to an operator: residue means the sanitizer is wrong, unverifiable
// means the verifier could not do its job. Collapsing them into one error
// would make the second look like the first and send the wrong person
// looking.
var (
	// ErrSanitizeResidue means the verifier found, in the OUTPUT bytes, a
	// construct the sanitizer claimed to remove.
	ErrSanitizeResidue = errors.New("sanitize verification: residue found in output")
	// ErrSanitizeUnverifiable means the verifier could not parse or inspect
	// the output at all. Fail closed: this is a job failure, not a caveat.
	ErrSanitizeUnverifiable = errors.New("sanitize verification: output could not be verified")
	// ErrSanitizeMalformedInput means the INPUT could not be processed: the
	// engine refused to parse it, the archive would not open, or a part this
	// path must rewrite is not well-formed XML.
	//
	// It exists so the worker can classify these as TERMINAL. They are
	// deterministic on the input -- a file ghostscript cannot read now is a
	// file ghostscript cannot read in twenty seconds -- and without a sentinel
	// they fell through to the shared isTerminal, which knows nothing about
	// sanitizer or ghostscript errors and returned false. Asynq then exhausted
	// its retries WITHOUT MarkFailed ever running, leaving the job active
	// until the reconciler exhausted it over repeated 15-minute cycles.
	//
	// ProbeDescriptor's own doc comment had claimed this classification since
	// the class was added; the code never implemented it. Caught in review
	// (PR #12, round 4) -- a comment describing behaviour that does not exist
	// is precisely the defect this phase's tests were built against, and it
	// was in this phase's own comment.
	ErrSanitizeMalformedInput = errors.New("sanitize: input could not be processed")
)

// verifiers is the closed set of formats that have an independent verifier.
// A format that sanitizes but has no verifier must not silently skip
// verification -- SanitizeFile turns a missing entry into an error, because
// "nothing checked it" and "it checked out" must never share an outcome.
var verifiers = map[string]func(context.Context, string) ([]ThreatClass, error){}

// VerifySanitized inspects the sanitized OUTPUT at path and reports which
// threat classes an independent implementation confirmed absent.
//
// It never reports a class it did not actually look for: the returned slice is
// what feeds SanitizeResult.Verified, and that field exists precisely so that
// "removed" and "confirmed absent" stay separate claims.
func VerifySanitized(ctx context.Context, format, path string) ([]ThreatClass, error) {
	fn, ok := verifiers[NormalizeFormat(format)]
	if !ok {
		return nil, fmt.Errorf("%w: no verifier registered for format %q",
			ErrSanitizeUnverifiable, format)
	}
	return fn(ctx, path)
}

// ---------------------------------------------------------------------------
// PDF
// ---------------------------------------------------------------------------

func init() { verifiers["pdf"] = verifyPDF }

// pdfActionResidue maps a dictionary key or /S action name to the threat class
// its presence in the output would disprove. Keys are checked as they appear
// in a parsed dictionary, so an object reachable only through /ObjStm is
// covered the same as one at top level.
var pdfActionResidue = map[string]ThreatClass{
	"JavaScript":    ThreatJavaScript,
	"JS":            ThreatJavaScript,
	"OpenAction":    ThreatActionDict,
	"AA":            ThreatActionDict,
	"Launch":        ThreatLaunchAction,
	"EmbeddedFiles": ThreatEmbeddedFile,
	"EmbeddedFile":  ThreatEmbeddedFile,
	"URI":           ThreatExternalReference,
	"GoToR":         ThreatExternalReference,
	"SubmitForm":    ThreatExternalReference,
	"ImportData":    ThreatExternalReference,
}

// pdfByteResidue is the second view: markers greppable in the raw output,
// OUTSIDE its compressed streams.
//
// This is not redundancy with the parse. A parser can miss what a grep finds
// (an object the parser drops as unreachable is still bytes on disk) and a
// grep can miss what a parser finds (a name split across an /ObjStm boundary).
// Two kinds of blindness, not two copies of one check.
//
// SCOPED TO THE NON-STREAM BYTES, and that scoping is a correction. Scanning
// the whole file treated Flate-compressed content-stream bytes as structure:
// a page's compressed data can contain "/AA" or "/URI" by chance, and on a
// document with enough content eventually does. Because verification failure
// DELETES the output (D-07), a false positive here did not mislabel a job --
// it destroyed a correct result and failed it. Caught in review (PR #12),
// the same class as the image verifier's whole-file scan.
//
// Object DICTIONARIES are never compressed in this build's output: the
// sanitizer targets PDF 1.4, which has no object streams, so every dictionary
// -- the only place these names can carry meaning -- lies in the plain bytes
// this still scans. Skipping stream payloads costs no reach.
var pdfByteResidue = map[string]ThreatClass{
	"/JavaScript":  ThreatJavaScript,
	"/OpenAction":  ThreatActionDict,
	"/AA":          ThreatActionDict,
	"/Launch":      ThreatLaunchAction,
	"EmbeddedFile": ThreatEmbeddedFile,
	"/URI":         ThreatExternalReference,
	// D-09 removes the XMP packet whole, so the metadata check is binary:
	// the packet is not there. Checking field-by-field would fail on an
	// honest file that legitimately has none of those fields, and checking
	// for a known secret value ("jane.doe") does not scale past the fixture.
	// "No packet" is the only form of this check that is both correct and
	// general -- which is a second reason D-09 chose whole removal, beyond
	// not leaking the producer version.
	"xpacket": ThreatDocumentMetadata,
	// The engine's own signature is metadata too, and D-09 named "does not
	// leak the producer version" as a reason for removing XMP whole. Leaving
	// Ghostscript's name in /Info while stripping it from XMP would be
	// incoherent, so both are checked.
	"Ghostscript":   ThreatDocumentMetadata,
	"%%Invocation:": ThreatDocumentMetadata,
	// PDF 1.4 output has no object streams by construction. If one appears,
	// something changed the compatibility level, and every byte-level check
	// in this project silently lost half its reach.
	"/ObjStm": ThreatDocumentMetadata,
}

func verifyPDF(ctx context.Context, path string) ([]ThreatClass, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read output: %v", ErrSanitizeUnverifiable, err)
	}

	// --- view 0: the one neither of the other two can see ---
	//
	// SC3: a construct removed from the current revision is still recoverable
	// if the file keeps a previous one. pdfcpu reads by the last xref, exactly
	// as a reader does, so a superseded object never enters the parse at all;
	// the byte grep WOULD find it but could not tell "still in the current
	// revision" from "sitting in a superseded one", and only one of those is a
	// bug in the sanitizer.
	//
	// It runs FIRST, before the parse, and that ordering is not cosmetic. It
	// is a pure byte count that needs no parser, and putting it behind a
	// successful parse made a two-revision file report "could not be
	// verified" -- true, but useless, and it hid the actual defect behind a
	// parser complaint about the older revision's page tree.
	if err := verifySingleRevision(raw); err != nil {
		return nil, err
	}

	// --- view 1: an independent parser ---
	pctx, err := api.ReadContextFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: pdfcpu could not read the output: %v",
			ErrSanitizeUnverifiable, err)
	}
	if found := scanPDFObjects(pctx.XRefTable.Table); len(found) > 0 {
		return nil, fmt.Errorf("%w: pdfcpu found %s in the parsed output",
			ErrSanitizeResidue, joinClasses(found))
	}
	// /Info is checked by VALUE, not by key. Ghostscript always writes
	// /Producer and /Creator and cannot be told not to (sanitizepdf.go's
	// redactEngineIdentity records the three attempts that were measured), so
	// the keys survive by construction and the redaction empties them. A key
	// with a non-empty value is residue no matter which key it is -- that
	// covers the client's /Title and /Author and the engine's own signature
	// under one rule, rather than an allow-list that would have to be revised
	// every time an engine invents a field.
	if pctx.XRefTable.Info != nil {
		if d, err := pctx.XRefTable.DereferenceDict(*pctx.XRefTable.Info); err == nil {
			for k, v := range d {
				if s, ok := v.(pdftypes.StringLiteral); ok && strings.TrimSpace(s.Value()) != "" {
					return nil, fmt.Errorf("%w: /Info carries a non-empty %s value",
						ErrSanitizeResidue, k)
				}
				if s, ok := v.(pdftypes.HexLiteral); ok && strings.TrimSpace(s.Value()) != "" {
					return nil, fmt.Errorf("%w: /Info carries a non-empty %s value",
						ErrSanitizeResidue, k)
				}
			}
		}
	}

	// --- view 2: the raw bytes ---
	if found := scanPDFBytes(raw); len(found) > 0 {
		return nil, fmt.Errorf("%w: %s greppable in the output bytes",
			ErrSanitizeResidue, joinClasses(found))
	}

	return append([]ThreatClass(nil), pdfThreatClasses...), nil
}

// verifySingleRevision requires exactly one startxref and one %%EOF.
func verifySingleRevision(raw []byte) error {
	// Counted OUTSIDE stream payloads, for the reason scanPDFBytes already
	// counted its markers there: compressed and image data can contain
	// "%%EOF" or "startxref" by chance, and a false multi-revision verdict
	// DELETES a valid output. The scoping existed one function away and was
	// not applied here -- the same rule in one of two places that need it,
	// which is the fourth time this review has named that shape. Caught in
	// review (PR #12, round 7).
	outside := pdfNonStreamBytes(raw)
	starts := bytes.Count(outside, []byte("startxref"))
	eofs := bytes.Count(outside, []byte("%%EOF"))

	// Zero of either is not "too many revisions" -- it is "this does not have
	// the structure of a PDF", which is the unverifiable outcome, not the
	// residue one. Collapsing the two would report a file of garbage as a
	// sanitizer defect and send the wrong person looking. Found by the
	// outcome-separation test after this check moved ahead of the parse.
	if starts == 0 || eofs == 0 {
		return fmt.Errorf("%w: output has %d startxref and %d %%%%EOF markers; "+
			"it does not have the trailer structure of a PDF",
			ErrSanitizeUnverifiable, starts, eofs)
	}
	if starts > 1 || eofs > 1 {
		return fmt.Errorf("%w: output has %d startxref and %d %%%%EOF markers; "+
			"more than one revision means removed objects stay recoverable in a prior one",
			ErrSanitizeResidue, starts, eofs)
	}
	return nil
}

// scanPDFObjects walks every object the parser resolved -- including those
// unpacked out of /ObjStm -- and reports the classes it finds.
//
// Both a key check and a VALUE check run here, and the value check is the one
// that matters: /S is an ordinary key on every action dictionary, so its
// presence proves nothing, while /S with the name Launch is a launch action
// and nothing else.
func scanPDFObjects(table map[int]*model.XRefTableEntry) []ThreatClass {
	seen := map[ThreatClass]bool{}
	var found []ThreatClass
	add := func(c ThreatClass) {
		if !seen[c] {
			seen[c] = true
			found = append(found, c)
		}
	}

	var walk func(pdftypes.Object, int)
	walk = func(obj pdftypes.Object, depth int) {
		if depth > 64 { // a malformed file can nest without an indirect ref
			return
		}
		switch o := obj.(type) {
		case pdftypes.Dict:
			for k, v := range o {
				if c, ok := pdfActionResidue[k]; ok {
					add(c)
				}
				if k == "S" || k == "Type" {
					if n, ok := v.(pdftypes.Name); ok {
						if c, ok := pdfActionResidue[n.Value()]; ok {
							add(c)
						}
					}
				}
				walk(v, depth+1)
			}
		case pdftypes.StreamDict:
			walk(o.Dict, depth+1)
		case pdftypes.Array:
			for _, v := range o {
				walk(v, depth+1)
			}
		}
	}

	for _, e := range table {
		if e == nil || e.Free || e.Object == nil {
			continue
		}
		walk(e.Object, 0)
	}
	return sortClasses(found)
}

// sortClasses orders findings by the canonical vocabulary order, so the same
// set of findings always renders as the same string. An error message that
// reorders itself between runs is one nobody can grep for.
func sortClasses(cs []ThreatClass) []ThreatClass {
	if len(cs) < 2 {
		return cs
	}
	in := map[ThreatClass]bool{}
	for _, c := range cs {
		in[c] = true
	}
	var out []ThreatClass
	for _, c := range allThreatClasses {
		if in[c] {
			out = append(out, c)
		}
	}
	return out
}

func scanPDFBytes(raw []byte) []ThreatClass {
	outside := pdfNonStreamBytes(raw)
	seen := map[ThreatClass]bool{}
	var found []ThreatClass
	for marker, class := range pdfByteResidue {
		if bytes.Contains(outside, []byte(marker)) && !seen[class] {
			seen[class] = true
			found = append(found, class)
		}
	}
	return sortClasses(found)
}

// pdfNonStreamBytes returns the file with every stream payload blanked out,
// so a scan sees dictionaries, names and trailers but not compressed content.
//
// Blanking rather than deleting keeps every offset where it was, which makes
// a reported offset in a failure message still mean something. The scan is
// deliberately crude about pathological input -- an unterminated "stream"
// keyword blanks the rest of the file, which fails CLOSED for a grep whose
// only job is to find things.
func pdfNonStreamBytes(raw []byte) []byte {
	out := append([]byte(nil), raw...)
	i := 0
	for {
		j := bytes.Index(out[i:], []byte("stream"))
		if j < 0 {
			break
		}
		start := i + j + len("stream")
		// Skip the EOL that ISO 32000 7.3.8.1 requires after the keyword.
		for start < len(out) && (out[start] == '\r' || out[start] == '\n') {
			start++
		}
		end := bytes.Index(out[start:], []byte("endstream"))
		if end < 0 {
			for k := start; k < len(out); k++ {
				out[k] = ' '
			}
			break
		}
		for k := start; k < start+end; k++ {
			out[k] = ' '
		}
		i = start + end + len("endstream")
	}
	return out
}

// ---------------------------------------------------------------------------
// OOXML
// ---------------------------------------------------------------------------

func init() {
	for _, f := range []string{"docm", "docx", "xlsm", "xlsx", "pptm", "pptx", "dotm", "potm", "ppsm"} {
		verifiers[f] = verifyOOXML
	}
}

// TOOL CHOICE FOR OOXML, AND THE MEASUREMENT THAT DECIDED IT
//
// The research recommended oletools/olevba and said plainly that it had not
// run it. Running it is what changed the answer.
//
// olevba 0.60.2 on the corpus fixture -- a package that contains
// word/vbaProject.bin whose first eight bytes are the OLE CFB signature --
// reports `"macros": []` and exits 0. So does `oleid`, printing
// "VBA Macros: No". The reason is in olevba.py: it scans every zip member by
// magic (so it is not fooled by a rename), but then requires a well-formed VBA
// project -- a storage named VBA, plus PROJECT, VBA/_VBA_PROJECT and VBA/dir
// streams -- before it reports anything at all. Anything short of that is
// "no macros".
//
// That makes olevba a fine macro ANALYSER and the wrong VERIFIER. Its "clean"
// verdict means "I found no VBA project I could parse", and a residual OLE
// container that it cannot parse is precisely what a verifier must not wave
// through. Reporting clean because parsing failed is the D-07 failure mode
// wearing a tool's clothes.
//
// So: unzip (Info-ZIP, C) enumerates and extracts the parts, and file/libmagic
// (a third codebase, also C) classifies each extracted part by content. The
// OLE container is identified as application/CDFV2 by its bytes, under
// whatever name it carries. Measured on the same fixture:
//
//	word/vbaProject.bin  ->  application/CDFV2
//	word/document.xml    ->  text/xml
//
// A rename does not move that line, because no part of this path reads names.

// oleMIMETypes is what libmagic calls an OLE/CFB compound document. Compared
// case-insensitively: libmagic's spelling has changed across releases.
var oleMIMETypes = []string{
	"application/cdfv2",
	"application/vnd.ms-office",
	"application/x-ole-storage",
	"application/msword",
}

// WHY THIS VERIFIER PARSES RELATIONSHIPS AT ALL
//
// It did not, and that was the sharpest gap review found. The sanitizer drops
// parts named by an embedded-content, properties or external relationship;
// this verifier checked part MIME types, the OLE signature and the
// macroEnabled content type, and then returned embedded_file,
// external_reference AND document_metadata as CONFIRMED ABSENT. An archive
// still carrying metadata/core.xml plus a core-properties relationship passed
// -- the verifier could not catch the one thing on the sanitizer side it
// exists to catch. Caught in review (PR #12, round 5), after round 4 had
// expanded the removal and left the check where it was: the third time in this
// review the remover moved and the checker did not.
//
// The tables and the parse below are written separately from
// sanitizeooxml.go's, deliberately. They share encoding/xml, as pdfcpu and
// ghostscript share Go, and that is unavoidable; what they do not share is the
// struct, the type table or the traversal, so a mistake in the sanitizer's
// relationship handling cannot travel into its own verification.

// ooxmlPropertyRoots are the root elements of the OOXML properties parts,
// matched on CONTENT so a properties part under any name is recognised --
// and matched on NAMESPACE as well as local name.
//
// The local name alone was too broad. An OPC package may carry
// application-defined XML parts, and <Properties xmlns="urn:example"> is an
// ordinary one; the sanitizer keeps it, having neither a properties
// relationship nor a docProps/ path to go on, and then this check called it
// residue and DELETED THE OUTPUT. A false positive here does not mislabel a
// job, it destroys a correct result -- the same consequence the whole-file
// byte scans had, arrived at from the opposite direction: too narrow a view
// misses residue, too broad a predicate invents it. Caught in review
// (PR #12, round 6).
var ooxmlPropertyRoots = map[xml.Name]bool{
	{Space: "http://schemas.openxmlformats.org/package/2006/metadata/core-properties", Local: "coreProperties"}: true,
	{Space: "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties", Local: "Properties"}:   true,
	{Space: "http://schemas.openxmlformats.org/officeDocument/2006/custom-properties", Local: "Properties"}:     true,
	// Dublin Core is what core.xml's own elements live in; some producers
	// write the root in it.
	{Space: "http://purl.org/dc/elements/1.1/", Local: "coreProperties"}: true,
}

// verifyResidualRelTypes are relationship type leaves that must not appear in
// a sanitized package. Kept as its own table rather than imported: two lists
// that must agree and are checked against each other by a test are worth more
// than one list checked against nothing.
var verifyResidualRelTypes = map[string]ThreatClass{
	"package":             ThreatEmbeddedFile,
	"oleObject":           ThreatEmbeddedFile,
	"aFChunk":             ThreatEmbeddedFile,
	"attachment":          ThreatEmbeddedFile,
	"core-properties":     ThreatDocumentMetadata,
	"extended-properties": ThreatDocumentMetadata,
	"custom-properties":   ThreatDocumentMetadata,
	"thumbnail":           ThreatDocumentMetadata,
}

// xmlRootElement returns the namespace-qualified name of an XML document's
// first element.
func xmlRootElement(raw []byte) (xml.Name, bool) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err != nil {
			return xml.Name{}, false
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name, true
		}
	}
}

// residualRelationship reports the first relationship in a .rels part that a
// sanitized package must not contain.
func residualRelationship(raw []byte) (string, bool) {
	var doc struct {
		Rel []struct {
			Type       string `xml:"Type,attr"`
			Target     string `xml:"Target,attr"`
			TargetMode string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		// Unreadable wiring is not a clean package. The caller turns this
		// into residue rather than into "could not verify", because the
		// sanitizer WROTE this file: if it is not parseable, the sanitizer
		// produced something malformed.
		return "an unparseable relationship file", true
	}
	for _, r := range doc.Rel {
		if strings.EqualFold(r.TargetMode, "External") {
			return fmt.Sprintf("an external relationship to %q (%s)", r.Target, ThreatExternalReference), true
		}
		leaf := r.Type
		if i := strings.LastIndex(leaf, "/"); i >= 0 {
			leaf = leaf[i+1:]
		}
		if class, bad := verifyResidualRelTypes[leaf]; bad {
			return fmt.Sprintf("a %s relationship to %q (%s)", leaf, r.Target, class), true
		}
	}
	return "", false
}

func verifyOOXML(ctx context.Context, path string) ([]ThreatClass, error) {
	listing, err := runCommand(ctx, "unzip", "-Z1", path)
	if err != nil {
		return nil, fmt.Errorf("%w: unzip could not list the output: %v",
			ErrSanitizeUnverifiable, err)
	}
	parts := strings.Fields(strings.TrimSpace(string(listing)))
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: unzip listed no parts in the output",
			ErrSanitizeUnverifiable)
	}

	// Counted rather than assumed. An upload whose zip carries
	// word/document.xml and no [Content_Types].xml is admitted as docx by
	// SniffContainer, the sanitizer copies the root part through, and the
	// macro-enabled check below was simply never entered -- so every OOXML
	// class came back verified and the job reached done with a package Office
	// cannot open. A check that only runs when a part is present says nothing
	// about a package that omits it. Caught in review (PR #12, round 6).
	sawContentTypes := 0

	dir, err := os.MkdirTemp("", "octoconv-verify-*")
	if err != nil {
		return nil, fmt.Errorf("%w: temp dir: %v", ErrSanitizeUnverifiable, err)
	}
	defer os.RemoveAll(dir)

	for i, part := range parts {
		// unzip treats [, ] and * in the member name as wildcard syntax, so
		// [Content_Types].xml -- a part every OOXML package has -- matches
		// nothing unless escaped. Left unhandled this reads as "the part is
		// empty", which is a clean verdict produced by a quoting bug.
		extracted := filepath.Join(dir, fmt.Sprintf("part-%03d", i))
		blob, err := runCommand(ctx, "unzip", "-p", path, escapeZipGlob(part))
		if err != nil {
			return nil, fmt.Errorf("%w: unzip could not extract %q: %v",
				ErrSanitizeUnverifiable, part, err)
		}
		if err := os.WriteFile(extracted, blob, 0o600); err != nil {
			return nil, fmt.Errorf("%w: write %q: %v", ErrSanitizeUnverifiable, part, err)
		}

		out, err := runCommand(ctx, "file", "--mime-type", "-b", extracted)
		if err != nil {
			return nil, fmt.Errorf("%w: file(1) could not classify %q: %v",
				ErrSanitizeUnverifiable, part, err)
		}
		mime := strings.ToLower(strings.TrimSpace(string(out)))
		for _, ole := range oleMIMETypes {
			if strings.Contains(mime, ole) {
				return nil, fmt.Errorf("%w: part %q is an OLE container (%s)",
					ErrSanitizeResidue, part, mime)
			}
		}

		// libmagic has a blind spot here, found by test rather than reasoning:
		// it needs enough of a well-formed compound file before it will say
		// CDFV2, so a part carrying the OLE signature and little else is
		// classified as something harmless. The sanitizer drops a part on the
		// signature alone, so such a part surviving means the sanitizer
		// failed -- and the check that catches it must therefore be at least
		// as broad as the one that drops it.
		//
		// This is the one place the verifier deliberately shares a PREDICATE
		// with the sanitizer. It does not share an implementation, and it is
		// not the only check: libmagic covers well-formed containers under any
		// name, this covers degenerate ones. Dropping either leaves a range
		// uncovered, and a range uncovered by a verifier reads as clean.
		if IsOLECFBFromPrefix(blob) {
			return nil, fmt.Errorf("%w: part %q begins with the OLE compound-file signature "+
				"(libmagic called it %s)", ErrSanitizeResidue, part, mime)
		}

		if strings.EqualFold(part, "[Content_Types].xml") {
			sawContentTypes++
			if bytes.Contains(blob, []byte("macroEnabled")) {
				return nil, fmt.Errorf("%w: [Content_Types].xml still declares a macro-enabled type",
					ErrSanitizeResidue)
			}
			if root, ok := xmlRootElement(blob); !ok || root.Local != "Types" {
				return nil, fmt.Errorf("%w: [Content_Types].xml does not parse as a Types document",
					ErrSanitizeUnverifiable)
			}
		}

		// A properties part identified by its ROOT ELEMENT, not by its path.
		// The sanitizer drops these; this is the independent confirmation,
		// and it looks at content for the same reason the OLE check does.
		if root, ok := xmlRootElement(blob); ok && ooxmlPropertyRoots[root] {
			return nil, fmt.Errorf("%w: part %q is a document-properties part (<%s> in %s)",
				ErrSanitizeResidue, part, root.Local, root.Space)
		}

		if strings.HasSuffix(strings.ToLower(part), ".rels") {
			if found, ok := residualRelationship(blob); ok {
				return nil, fmt.Errorf("%w: %s declares %s", ErrSanitizeResidue, part, found)
			}
		}
	}

	if sawContentTypes != 1 {
		return nil, fmt.Errorf("%w: the package has %d [Content_Types].xml parts, want exactly 1; "+
			"without it this is not an OOXML package and nothing here describes it",
			ErrSanitizeUnverifiable, sawContentTypes)
	}

	return append([]ThreatClass(nil), ooxmlThreatClasses...), nil
}

// escapeZipGlob backslash-escapes the characters Info-ZIP reads as wildcards.
func escapeZipGlob(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '[', ']', '*', '?', '\\':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// shared
// ---------------------------------------------------------------------------

func joinClasses(cs []ThreatClass) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = string(c)
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// images
// ---------------------------------------------------------------------------

// Plan 60-04 specified verifiers for PDF and OOXML only, because it was
// written expecting the image path to shell out to exiftool. Plan 60-03 chose
// a structural strip instead, so an image verifier is needed and this is it.
//
// The rule that verification cannot be skipped (see SanitizeFile) is what
// forced the issue rather than letting it slide: with no verifier registered,
// every image sanitize failed closed. That is the correct failure, and it is
// louder than a TODO.
//
// Independence: the sanitizer walks segments and never decodes. This verifier
// does the two things the sanitizer does not -- it DECODES the output with the
// standard library, proving the strip left a real image behind, and it asks
// libmagic what the file is. Neither shares a line with the walker.

func init() {
	verifiers["jpg"] = verifyImage
	verifiers["png"] = verifyImage
}

// WHY THIS WALKS THE CONTAINER INSTEAD OF GREPPING IT
//
// The first version scanned the whole output for "tEXt", "Exif\0\0" and
// friends. Compressed image data is arbitrary bytes: a PNG's IDAT stream or a
// JPEG's entropy-coded scan can contain any four-byte sequence, and on a large
// enough image it eventually does. The verifier would then report residue on a
// perfectly clean output, and because verification failure DELETES the output
// (D-07), a false positive here does not merely mislabel a job -- it destroys
// a correct result and fails the job. Caught in review (PR #12).
//
// Structure is not optional here for a second reason: a byte scan cannot tell
// a kept APP2 colour profile from a dropped APP1 EXIF block, so once the
// sanitizer started preserving colour management the grep had no way to
// express the actual rule.
//
// The walk below is written independently of the sanitizer's, and it checks a
// different proposition: the sanitizer asks "should I copy this segment", this
// asks "is any segment of a forbidden class present in what was copied". Two
// implementations of one rule, which is what SANI-04 requires; sharing a
// walker would make this check prove only that the walker agrees with itself.

// jpegSegmentAllowed mirrors sanitizeimage.go's colour-management allowance,
// restated here rather than imported so the two can disagree loudly.
//
// Restated INCLUDING THE SHAPE CHECK, which the prefix-only version lacked: a
// verifier that allows any APP14 starting with "Adobe" confirms as clean the
// exact segment the sanitizer was fixed to stop preserving. The two halves of
// this fix are useless apart.
func jpegSegmentAllowed(marker byte, payload []byte) bool {
	switch marker {
	case 0xEE:
		return len(payload) == 12 && bytes.HasPrefix(payload, []byte("Adobe"))
	case 0xE2:
		const iccHeader = 14
		if !bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")) || len(payload) < iccHeader+4 {
			return false
		}
		declared := int(binary.BigEndian.Uint32(payload[iccHeader : iccHeader+4]))
		return len(payload)-iccHeader <= declared
	default:
		return false
	}
}

// jpegMetadataResidue walks a JPEG's segment chain and reports any APPn or COM
// segment that is not one of the allowed colour-management segments.
func jpegMetadataResidue(raw []byte) (string, bool) {
	i := 2 // past SOI
	for i+1 < len(raw) {
		if raw[i] != 0xFF {
			return "", false // malformed; the decode check above already ruled
		}
		// Fill bytes before a marker, same rule as the sanitizer's walker and
		// as skipEntropyCodedScan below. Without it this walker stopped early
		// on a legal file and reported CLEAN on segments it never reached.
		for i+2 < len(raw) && raw[i+1] == 0xFF {
			i++
		}
		marker := raw[i+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if marker == 0xD9 { // EOI
			return "", false
		}
		if marker == 0xDA {
			// SOS. The scan's compressed bytes carry no segments, but the
			// FILE does not end here: a JPEG may hold several scans and may
			// legally carry APP or COM segments after one. Returning here
			// made this verifier blind in exactly the same place the
			// sanitizer was, so a post-scan EXIF block survived AND was
			// reported as verified -- two independent implementations sharing
			// one wrong assumption prove nothing at all. Caught in review
			// (PR #12, round 3).
			if i+4 > len(raw) {
				return "", false
			}
			segLen := int(binary.BigEndian.Uint16(raw[i+2 : i+4]))
			if segLen < 2 || i+2+segLen > len(raw) {
				return "", false
			}
			i = skipEntropyCodedScan(raw, i+2+segLen)
			continue
		}
		if i+4 > len(raw) {
			return "", false
		}
		segLen := int(binary.BigEndian.Uint16(raw[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(raw) {
			return "", false
		}
		end := i + 2 + segLen

		isAPP := marker >= 0xE0 && marker <= 0xEF
		if isAPP || marker == 0xFE {
			if !jpegSegmentAllowed(marker, raw[i+4:end]) {
				return fmt.Sprintf("APP%d/COM segment 0x%02X at offset %d", marker-0xE0, marker, i), true
			}
		}
		i = end
	}
	return "", false
}

// skipEntropyCodedScan returns the offset of the next real marker after a
// scan's compressed data.
//
// Written separately from sanitizeimage.go's endOfEntropyCodedData on purpose,
// even though the rule is the same one: SANI-04 requires the implementation
// that checks to differ from the one that removed, and calling the sanitizer's
// helper here would make this check prove that the helper agrees with itself.
// The rule itself -- FF 00 is stuffing, FF D0..D7 are restart markers, a run
// of FF is padding -- comes from ISO/IEC 10918-1, not from the other function.
func skipEntropyCodedScan(b []byte, from int) int {
	i := from
	for i+1 < len(b) {
		if b[i] != 0xFF {
			i++
			continue
		}
		j := i
		for j+1 < len(b) && b[j+1] == 0xFF {
			j++
		}
		next := b[j+1]
		if next == 0x00 || (next >= 0xD0 && next <= 0xD7) {
			i = j + 2
			continue
		}
		return j
	}
	return len(b)
}

// pngMetadataResidue walks a PNG's chunk chain and reports any chunk whose
// TYPE is a metadata type. A four-byte sequence inside IDAT is data, not a
// chunk header, and this is what tells them apart.
func pngMetadataResidue(raw []byte) (string, bool) {
	const sigLen = 8
	i := sigLen
	for i+8 <= len(raw) {
		length := int(binary.BigEndian.Uint32(raw[i : i+4]))
		if length < 0 || i+12+length > len(raw) {
			return "", false
		}
		ctype := string(raw[i+4 : i+8])
		if pngMetadataChunks[ctype] {
			return fmt.Sprintf("%s chunk at offset %d", ctype, i), true
		}
		i += 12 + length
		if ctype == "IEND" {
			break
		}
	}
	return "", false
}

// verifyMaxDecodePixels bounds the full decode above. 100 megapixels is the
// project's own documented image ceiling (MaxImagePixels, .env.example), so
// this refuses exactly the images the API would already have refused at upload
// and no others -- picking a smaller number here would turn a legitimate
// upload the API accepted into a failed job at the last step.
const verifyMaxDecodePixels = 100_000_000

func verifyImage(ctx context.Context, path string) ([]ThreatClass, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read output: %v", ErrSanitizeUnverifiable, err)
	}

	// --- view 1: it still decodes as an image, ALL of it ---
	//
	// DecodeConfig alone was not enough, and the gap it left was a false
	// "verified": it reads only enough structure to report dimensions, so a
	// file with an intact header and a truncated or corrupted compressed
	// stream passed. The sanitizer copies that damaged payload through
	// unchanged and libmagic still recognises the header, so the job finished
	// successfully with an output no decoder can open, and the report claimed
	// document_metadata verified. Caught in review (PR #12, round 3) and
	// reproduced before it was fixed: removing one scan byte from the corpus
	// fixture passes DecodeConfig and fails image.Decode with
	// "missing 0xff00 sequence".
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: the stripped image no longer decodes: %v",
			ErrSanitizeUnverifiable, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("%w: decoded %s has no dimensions", ErrSanitizeUnverifiable, format)
	}
	// The full decode is bounded BEFORE it is attempted. Go's image decoders
	// materialise the whole raster, so an image admitted at the API's pixel
	// ceiling would allocate hundreds of megabytes inside a 1 GiB worker.
	// Above the bound the answer is "could not verify" -- which deletes the
	// output and fails the job -- rather than a decode that takes the worker
	// with it. Refusing to claim is the fail-closed outcome; claiming without
	// checking is the one D-07 rules out.
	if px := int64(cfg.Width) * int64(cfg.Height); px > verifyMaxDecodePixels {
		return nil, fmt.Errorf("%w: %s is %dx%d (%d pixels), above the %d-pixel bound this "+
			"verifier will decode within the worker's memory",
			ErrSanitizeUnverifiable, format, cfg.Width, cfg.Height, px, verifyMaxDecodePixels)
	}
	if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("%w: the stripped image does not fully decode: %v",
			ErrSanitizeUnverifiable, err)
	}

	// --- view 2: libmagic, a different codebase, agrees what this is ---
	out, err := runCommand(ctx, "file", "--mime-type", "-b", path)
	if err != nil {
		return nil, fmt.Errorf("%w: file(1) could not classify the output: %v",
			ErrSanitizeUnverifiable, err)
	}
	mime := strings.ToLower(strings.TrimSpace(string(out)))
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("%w: libmagic calls the output %q, not an image",
			ErrSanitizeUnverifiable, mime)
	}

	// --- view 3: no metadata-carrying segment or chunk survives ---
	var found string
	var isResidue bool
	switch format {
	case "jpeg":
		found, isResidue = jpegMetadataResidue(raw)
	case "png":
		found, isResidue = pngMetadataResidue(raw)
	default:
		return nil, fmt.Errorf("%w: no structural check for decoded format %q",
			ErrSanitizeUnverifiable, format)
	}
	if isResidue {
		return nil, fmt.Errorf("%w: %s survives in the output", ErrSanitizeResidue, found)
	}

	return append([]ThreatClass(nil), imageThreatClasses...), nil
}
