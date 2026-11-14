// Package inspect is the closed, versioned vocabulary of TYPED FINDINGS about
// a file's content -- a separate concern from internal/errcode. errcode
// answers "why was the REQUEST rejected"; this package answers "what was
// FOUND IN THE FILE". A single upload can produce a finding without
// producing a rejection at all, and that is the normal case, not a
// discrepancy: the synchronous inspect path (Phase 58) only REPORTS these
// findings and never rejects on them, while handleCreateJob continues to
// reject on its own (smaller) subset of the same codes (see bindings.go,
// D-04). Two consumers of one declaration cannot drift apart from each
// other because there is only one declaration -- this is the same defect
// class Phase 57 closed three times (FormatsResponse.Operations,
// JobStatus.Outputs, progress_pct/eta_seconds all emitted server-side and
// silently dropped by a consumer that kept its own copy of the same list).
//
// Go constants below are the single source of truth (mirrors
// internal/errcode's D-06 discipline). docs/FINDINGS.md is generated/
// maintained FROM them, and doc_sync_test.go
// (TestFindingCodesDocMatchesGoConstants) mechanically proves the two never
// drift apart in either direction.
package inspect

// Severity is a closed, three-value classification of how a client should
// react to a Finding. Named for the CLIENT ACTION the finding implies, not
// for how dangerous it "feels" -- the whole point of three tiers instead of
// five is that each one maps to a genuinely different action; a finer
// scale invites debate about the boundary between adjacent levels with no
// behavioral difference behind it (see docs/ERROR-CODES.md's own framing:
// granularity is by failure/finding CLASS, not by call site).
type Severity string

const (
	// SeverityInfo is a structural fact about the file that is not, by
	// itself, a risk. No client action is required.
	SeverityInfo Severity = "info"
	// SeverityWarning is a passive risk: the content COULD be a vector for
	// data exfiltration or tracking, but doing anything harmful requires the
	// recipient to take some further action. Sanitizing before further
	// processing is recommended, not mandatory.
	SeverityWarning Severity = "warning"
	// SeverityCritical is an active/auto-triggered risk: the content can
	// execute or take effect WITHOUT any further action by the recipient.
	// Sanitizing or rejecting the file outright is strongly recommended.
	SeverityCritical Severity = "critical"
)

// Code is a closed, machine-readable identifier for one class of finding.
// The set below is closed at seventeen values (D-05: no typed findings exist
// anywhere in the project before this phase -- this is a new layer, not a
// refactor of an existing bool/error/422 collapse). Later phases may ADD
// codes; none may be renamed or repurposed once shipped (same discipline as
// docs/ERROR-CODES.md).
//
// The warning+critical-severity PDF codes below (pdf_javascript,
// pdf_action_dict, pdf_launch_action, pdf_embedded_file) are exactly Phase
// 60's SANI-01 removal list. They are declared here in the form Phase 60's
// sanitize report is expected to consume unchanged -- do not rename them
// there.
type Code string

const (
	// CodeOOXMLMacro: docx/xlsx/pptx/odt/ods/odp/epub. Detected by
	// ContainerResult.HasMacro (internal/convert/docsniff.go), reused as-is.
	// Severity warning: a macro-carrying part is a passive risk -- it does
	// nothing unless the recipient's own tooling later executes it.
	CodeOOXMLMacro Code = "ooxml_macro"
	// CodeZipBombShape: any zip-based container. ContainerResult.
	// TotalUncompressed above a findings-level threshold, which is
	// deliberately SEPARATE from and milder than handleCreateJob's hard
	// reject threshold (D-04) -- inspect's job is to report, not reject, so
	// it surfaces a smaller ratio than the request-rejection gate does.
	// Severity critical: decompressing a zip-bomb-shaped archive is itself
	// the harm; no further recipient action is required.
	CodeZipBombShape Code = "zip_bomb_shape"
	// CodeImagePixelBombShape: image formats. Declared dimensions from
	// convert.Dimensions give a pixel count above a findings-level
	// threshold. Symmetric with zip_bomb_shape -- the same "unpacking alone
	// is the harm" class, just for raster decode cost instead of archive
	// decompression. Severity critical.
	CodeImagePixelBombShape Code = "image_pixel_bomb_shape"
	// CodeOLECFBLegacy: ClassifyCFB returned CFBLegacy (legacy binary
	// Office: .doc/.xls/.ppt). Severity info: a structural fact about the
	// container format, not itself a risk.
	CodeOLECFBLegacy Code = "ole_cfb_legacy"
	// CodeOLECFBEncrypted: ClassifyCFB returned CFBEncrypted. Severity
	// info: encryption without any other finding is a privacy signal (the
	// recipient cannot read it without the password either), not a
	// code-execution risk.
	CodeOLECFBEncrypted Code = "ole_cfb_encrypted"
	// CodeOLECFBUnclassified: ClassifyCFB returned CFBUnknown -- an OLE-CFB
	// header is present but the directory did not classify cleanly. This is
	// its own code, not the absence of a finding: an unparsed container is
	// NOT the same claim as a clean one. Severity info.
	CodeOLECFBUnclassified Code = "ole_cfb_unclassified"
	// CodeExtensionMismatch: the filename extension normalizes to a format
	// different from the one detected by magic bytes. Severity info: a
	// structural fact about client-supplied metadata, not a content risk by
	// itself.
	CodeExtensionMismatch Code = "extension_mismatch"
	// CodePDFJavaScript: the literal token /JavaScript or /JS was found.
	// Severity critical: PDF JavaScript can execute automatically in a
	// compliant viewer with no further recipient action.
	CodePDFJavaScript Code = "pdf_javascript"
	// CodePDFActionDict: the literal token /OpenAction or /AA was found.
	// Severity critical: an action dictionary can auto-trigger on open or
	// on another document event with no further recipient action.
	CodePDFActionDict Code = "pdf_action_dict"
	// CodePDFLaunchAction: the literal token /Launch was found. Severity
	// critical: a launch action can invoke an external application/command
	// with no further recipient action beyond opening the PDF.
	CodePDFLaunchAction Code = "pdf_launch_action"
	// CodePDFEmbeddedFile: the literal token /EmbeddedFile was found.
	// Severity warning: an embedded file is a passive risk -- it requires
	// the recipient to explicitly open/extract it before any harm.
	CodePDFEmbeddedFile Code = "pdf_embedded_file"
	// CodePDFEncrypted: /Encrypt was found in the trailer or XRef-stream
	// dictionary. Severity info: same rationale as ole_cfb_encrypted --
	// encryption alone is a privacy signal, not a code-execution risk.
	CodePDFEncrypted Code = "pdf_encrypted"
	// CodeExternalReference: PDF /URI, or an OOXML/ODF relationship with
	// TargetMode="External". Deliberately named WITHOUT a format prefix --
	// one finding, two sources, is exactly the case this vocabulary being
	// closed exists to collapse correctly instead of duplicating. Severity
	// warning: an external reference is a passive
	// exfiltration/tracking-vector risk that requires the recipient (or
	// their viewer) to follow the link before anything happens.
	CodeExternalReference Code = "external_reference"
	// CodeMetadataPresent: presence of an EXIF/XMP/GPS segment (JPEG APP1,
	// PNG eXIf/iTXt, TIFF IFD) or a PDF /Info dictionary. Severity info: a
	// structural fact about embedded metadata, not itself a risk.
	CodeMetadataPresent Code = "metadata_present"
	// CodeSTLOpen: one undirected edge of an STL belongs to one triangle.
	CodeSTLOpen Code = "stl_open"
	// CodeSTLNonManifold: one undirected edge of an STL belongs to three or more triangles.
	CodeSTLNonManifold Code = "stl_non_manifold"
	// CodeSTLUnoriented: one undirected edge of an STL belongs to two triangles that traverse it in the same direction.
	CodeSTLUnoriented Code = "stl_unoriented"
)

// Finding is one occurrence of Code detected inside a file's content.
//
// Location is best-effort and its SHAPE DEPENDS ON THE SOURCE that produced
// it -- a zip entry path for container-based findings, a decimal byte
// offset for PDF token scans, or empty when no meaningful location exists
// (e.g. a whole-file structural fact like zip_bomb_shape). Clients MUST
// treat Location as an opaque, human-facing string and must never parse it
// as a stable machine-readable field.
type Finding struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Location string   `json:"location,omitempty"`
}

// allCodes is the single place this vocabulary is enumerated, in
// declaration order. AllCodes(), IsKnownCode(), and severityByCode are all
// built from it.
var allCodes = []Code{
	CodeOOXMLMacro,
	CodeZipBombShape,
	CodeImagePixelBombShape,
	CodeOLECFBLegacy,
	CodeOLECFBEncrypted,
	CodeOLECFBUnclassified,
	CodeExtensionMismatch,
	CodePDFJavaScript,
	CodePDFActionDict,
	CodePDFLaunchAction,
	CodePDFEmbeddedFile,
	CodePDFEncrypted,
	CodeExternalReference,
	CodeMetadataPresent,
	CodeSTLOpen,
	CodeSTLNonManifold,
	CodeSTLUnoriented,
}

// severityByCode is the closed severity assignment for every code in
// allCodes. Every entry in allCodes MUST have an entry here -- enforced by
// TestEveryCodeHasSeverity.
var severityByCode = map[Code]Severity{
	CodeOOXMLMacro:          SeverityWarning,
	CodeZipBombShape:        SeverityCritical,
	CodeImagePixelBombShape: SeverityCritical,
	CodeOLECFBLegacy:        SeverityInfo,
	CodeOLECFBEncrypted:     SeverityInfo,
	CodeOLECFBUnclassified:  SeverityInfo,
	CodeExtensionMismatch:   SeverityInfo,
	CodePDFJavaScript:       SeverityCritical,
	CodePDFActionDict:       SeverityCritical,
	CodePDFLaunchAction:     SeverityCritical,
	CodePDFEmbeddedFile:     SeverityWarning,
	CodePDFEncrypted:        SeverityInfo,
	CodeExternalReference:   SeverityWarning,
	CodeMetadataPresent:     SeverityInfo,
	CodeSTLOpen:             SeverityInfo,
	CodeSTLNonManifold:      SeverityInfo,
	CodeSTLUnoriented:       SeverityInfo,
}

// AllCodes returns every declared finding code exactly once, in declaration
// order. The returned slice is a fresh copy on every call, so callers
// cannot mutate the package's closed vocabulary through it.
func AllCodes() []Code {
	out := make([]Code, len(allCodes))
	copy(out, allCodes)
	return out
}

// IsKnownCode reports whether code is one of the closed set of finding
// codes declared in this package. The comparison is exact, with no case
// normalization -- "pdf_JAVASCRIPT" is not "pdf_javascript".
func IsKnownCode(code Code) bool {
	for _, c := range allCodes {
		if c == code {
			return true
		}
	}
	return false
}

// SeverityOf returns the declared severity for code and ok==true if code is
// known. For an unrecognized code it returns ("", false) -- NEVER a default
// severity. Defaulting an unrecognized code to "info" would silently
// understate the risk of a code this package does not yet know about,
// which is a quiet lie this function refuses to tell.
func SeverityOf(code Code) (Severity, bool) {
	s, ok := severityByCode[code]
	return s, ok
}
