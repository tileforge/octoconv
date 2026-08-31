package convert

// This file (findings.go): the closed, versioned vocabulary of THREAT CLASSES
// the sanitize operation finds and removes (Phase 60, D-08).
//
// It is a third vocabulary alongside internal/errcode ("why was the REQUEST
// rejected") and internal/inspect ("what was FOUND IN THE FILE"), and it
// answers a question neither of them does: "what was REMOVED FROM THE FILE".
// The three overlap in subject and differ in verb -- inspect can report a
// pdf_javascript finding on a file nobody ever sanitizes, and sanitize removes
// constructs on files nobody ever inspected.
//
// CLOSED BY MEANING, NOT BY SPELLING
//
// Several distinct tokens collapse into one class on purpose. /OpenAction and
// /AA are two different PDF dictionary keys and one client-visible fact: the
// document tried to run something on open. A client deciding what to do with
// the sanitized file needs to know an auto-triggered action was removed; which
// key carried it is an implementation detail of the format, not of the risk.
//
// The same rule going the other way is what keeps the vocabulary honest: a new
// spelling of an existing threat extends an existing class, while a genuinely
// different client-visible fact earns a new constant. "We found a new token"
// is not a reason to grow this list.
//
// WHAT THIS VOCABULARY MAY NEVER SAY
//
// No constant, and no text assembled from these constants, may assert that a
// file is safe, clean, virus-free, or free of malicious content. The report
// built from this vocabulary states which ENUMERATED classes were looked for
// and removed, and nothing wider. CDR removes carriers; it does not determine
// whether code is malicious, and a service that removes seven classes of
// carrier and reports "safe" has made a claim it did not test.
//
// That restriction is mechanical, not advisory: Plan 60-05 adds a doc-sync
// test that fails on the affirmative phrasings, in both this file and
// docs/SANITIZE.md. Adding a class here means adding a row there.

// ThreatClass is one client-visible class of construct the sanitizer finds and
// removes. Compared exactly, with no case normalization -- the same discipline
// as inspect.Code and errcode.Code.
type ThreatClass string

const (
	// ThreatJavaScript is embedded JavaScript: a /JavaScript name tree entry
	// or a /S /JavaScript action. Auto-executing in most readers.
	ThreatJavaScript ThreatClass = "javascript"

	// ThreatActionDict is an automatic action dictionary -- /OpenAction on
	// the catalog, or /AA (additional actions) anywhere. Both mean the
	// document asked for something to happen without the reader choosing it.
	ThreatActionDict ThreatClass = "action_dict"

	// ThreatLaunchAction is a /Launch action: the document asks the reader to
	// start an external program.
	ThreatLaunchAction ThreatClass = "launch_action"

	// ThreatEmbeddedFile is an attached file -- an /EmbeddedFiles name tree,
	// a /Filespec with an /EF stream, or an OOXML part carrying one. The
	// attachment travels with the document and is opened by hand later,
	// which is why inspect classifies the same construct as a passive rather
	// than an active risk.
	ThreatEmbeddedFile ThreatClass = "embedded_file"

	// ThreatExternalReference is a reference to a resource outside the file:
	// a /URI action, or an OOXML relationship with TargetMode="External".
	// Removing it is what stops a document from phoning home when opened.
	ThreatExternalReference ThreatClass = "external_reference"

	// ThreatDocumentMetadata is authorship and provenance metadata: PDF
	// /Info, an XMP packet, or image EXIF/GPS/IPTC. Grouped as one class
	// because the client-visible fact is identical across all three -- who
	// made this, when, with what, and where -- even though the carriers are
	// entirely different formats.
	ThreatDocumentMetadata ThreatClass = "document_metadata"

	// ThreatVBAMacro is a VBA project in an OOXML container. Removed as a
	// whole part rather than by editing module source: VBA stomping keeps
	// benign-looking source and hides behaviour in compiled p-code, so any
	// sanitizer that reads source and keeps the part is defeated by
	// construction (60-CONTEXT.md D-05).
	ThreatVBAMacro ThreatClass = "vba_macro"

	// ThreatOLEObject is an embedded OLE compound-file object inside an
	// OOXML container -- distinct from ThreatVBAMacro, which is specifically
	// the VBA project part. Wired up by Plan 60-03.
	ThreatOLEObject ThreatClass = "ole_object"
)

// allThreatClasses is the closed set, in the order the report presents them:
// active/auto-triggered risks first, then passive ones, then metadata. The
// order is part of the contract -- docs/SANITIZE.md's table mirrors it, and
// Plan 60-05's doc-sync test compares both membership and nothing else, so a
// reordering here is a documentation edit, not a behavioural one.
var allThreatClasses = []ThreatClass{
	ThreatJavaScript,
	ThreatActionDict,
	ThreatLaunchAction,
	ThreatVBAMacro,
	ThreatOLEObject,
	ThreatEmbeddedFile,
	ThreatExternalReference,
	ThreatDocumentMetadata,
}

// AllThreatClasses returns a fresh copy of the closed set, so a caller
// ranging over it cannot mutate the package's own slice -- the same defensive
// copy inspect.AllCodes and errcode.AllFailureCodes make, for the same reason.
func AllThreatClasses() []ThreatClass {
	out := make([]ThreatClass, len(allThreatClasses))
	copy(out, allThreatClasses)
	return out
}

// IsKnownThreatClass reports whether c is one of the closed set. Exact
// comparison, no case normalization -- "JavaScript" is not "javascript".
func IsKnownThreatClass(c ThreatClass) bool {
	for _, known := range allThreatClasses {
		if c == known {
			return true
		}
	}
	return false
}
