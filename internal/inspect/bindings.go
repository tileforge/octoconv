// This file declares the D-04 binding between every EXISTING content-
// validation rejection in handleCreateJob (internal/api/handlers.go) and
// this package's findings vocabulary -- WITHOUT migrating handleCreateJob
// itself onto that vocabulary.
//
// What this file does NOT do (D-04, worth saying explicitly): it does not
// change one line of internal/api/handlers.go. It does not rewrite a
// single writeError call. It does not change any existing rejection's
// HTTP status code or response body byte-for-byte. The map below is a
// DECLARATION of correspondence, nothing more -- migrating handleCreateJob
// onto this vocabulary is deliberately deferred (candidate: Phase 61). The
// fail-closed request-rejection path is the single riskiest piece of code
// in this milestone; a phase that is already the riskiest one does not
// also get to rewrite it.
package inspect

import "github.com/apaderin/octoconv/internal/errcode"

// RejectBinding names, for one reason-token logged by handleCreateJob's
// "content validation rejected:" lines, whether that rejection is a
// statement about the FILE'S CONTENT (a FindingCode from this package's
// vocabulary) or a statement about the REQUEST/POLICY (an errcode.* value)
// that is not a finding about the file at all. Exactly one of the two
// fields is non-empty for any given binding -- never both, never neither
// (enforced by TestRejectBindingsAreWellFormed).
type RejectBinding struct {
	FindingCode Code
	ErrCode     string
}

// IsFinding reports whether this binding maps to a finding code (true) or
// to a non-finding errcode sentinel (false).
func (b RejectBinding) IsFinding() bool {
	return b.FindingCode != ""
}

// RejectBindings is the complete, closed set of reason tokens
// handleCreateJob's "content validation rejected:" log lines can emit,
// mapped to their D-04 binding. Completeness against the real source is
// proven mechanically by TestRejectBindingsCoverEveryContentValidationReason
// (internal/inspect/bindings_test.go), which scans internal/api/handlers.go
// itself rather than trusting this list to stay in sync by hand.
var RejectBindings = map[string]RejectBinding{
	"zip_bomb":                     {FindingCode: CodeZipBombShape},
	"macro_detected":               {FindingCode: CodeOOXMLMacro},
	"encrypted_document":           {FindingCode: CodeOLECFBEncrypted},
	"legacy_document":              {FindingCode: CodeOLECFBLegacy},
	"legacy_or_encrypted_document": {FindingCode: CodeOLECFBUnclassified},
	"mismatch":                     {FindingCode: CodeExtensionMismatch},
	"dimension_limit":              {FindingCode: CodeImagePixelBombShape},

	// unrecognized_content: the service could not identify the format at
	// all. In an inspect report this is a fact about the FORMAT-DETECTION
	// step itself, not a finding about a recognized file's content -- there
	// is no Code in the vocabulary for "we don't know what this is".
	"unrecognized_content": {ErrCode: errcode.InvalidRequest},
	// dimensions_unknown: convert.Dimensions failed to extract declared
	// pixel dimensions. In an inspect report this is an unknown-status
	// CHARACTERISTIC extraction failure, not a finding -- a failed
	// measurement is not itself a risk signal.
	"dimensions_unknown": {ErrCode: errcode.InvalidRequest},
	// engine_size_limit: the upload exceeds a configured PER-ENGINE size
	// policy. This is a property of server configuration, not of the file's
	// content. It now covers the TOTAL of a multi-part upload, for the reason
	// the tex ceiling exists: cost is linear in source size, so a project
	// split across parts costs what its bytes cost.
	"engine_size_limit": {ErrCode: errcode.PayloadTooLarge},
	// multiple_inputs: extra "file" parts were sent to a conversion that takes
	// one file. A property of the REQUEST's shape against the engine's
	// capability, not of any file's content -- and refused rather than ignored,
	// because ignoring it would charge the client for bytes the job cannot read
	// and answer 202.
	"multiple_inputs": {ErrCode: errcode.InvalidRequest},
	// unsafe_filename: an attachment's name cannot be used as a plain file in
	// the job's own directory. A statement about the NAME, not the bytes: these
	// names become files beside the document the engine compiles.
	"unsafe_filename": {ErrCode: errcode.InvalidRequest},
	// too_many_parts: more files in one job than this service accepts. A
	// property of the REQUEST, and a count bound rather than a byte bound --
	// size does not bound count, and a thousand one-byte parts cost a thousand
	// uploads, rows and downloads while passing every byte ceiling.
	"too_many_parts": {ErrCode: errcode.InvalidRequest},
	// reserved_filename: an attachment is named for a file the WORKER creates
	// (in.<source>, out.<target>). Refused because it would overwrite the
	// document that passed every check with a part that passed none.
	"reserved_filename": {ErrCode: errcode.InvalidRequest},
	// duplicate_filename: two parts share a name and would overwrite each other
	// in the job's directory, with upload order deciding which survived. Also a
	// property of the request, not of any file's content.
	"duplicate_filename": {ErrCode: errcode.InvalidRequest},
	// invalid_opts: the request's `opts` field failed to parse. This is a
	// property of the request's opts field, not of the uploaded file.
	"invalid_opts": {ErrCode: errcode.InvalidOptions},
	// opts_not_applicable: `opts` parsed but is not meaningful for this
	// conversion. Also a property of the request's opts field.
	"opts_not_applicable": {ErrCode: errcode.OptsNotApplicable},
	// opts_too_large: `opts` exceeds its configured size cap. Also a
	// property of the request's opts field.
	"opts_too_large": {ErrCode: errcode.PayloadTooLarge},
	// unsupported_package (TEX-03): the .tex preamble loads a \usepackage
	// this deployment's TeX bundle cannot resolve. An ERRCODE binding, not a
	// finding: nothing about the client's file is risky or malformed -- the
	// document is valid LaTeX that some other deployment, with a fuller
	// bundle, would typeset. It is a fact about OUR configuration, and an
	// inspect report that listed it among a file's findings would be telling
	// the client their document is at fault.
	"unsupported_package": {ErrCode: errcode.UnsupportedPackage},

	"truncated_cad_file":     {ErrCode: errcode.TruncatedCADFile},
	"ambiguous_stl":          {ErrCode: errcode.AmbiguousSTL},
	"binary_dxf_unsupported": {ErrCode: errcode.BinaryDXFUnsupported},
	"dwg_unsupported":        {ErrCode: errcode.DWGUnsupported},
}
