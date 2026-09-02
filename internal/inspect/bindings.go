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
	// content.
	"engine_size_limit": {ErrCode: errcode.PayloadTooLarge},
	// invalid_opts: the request's `opts` field failed to parse. This is a
	// property of the request's opts field, not of the uploaded file.
	"invalid_opts": {ErrCode: errcode.InvalidOptions},
	// opts_not_applicable: `opts` parsed but is not meaningful for this
	// conversion. Also a property of the request's opts field.
	"opts_not_applicable": {ErrCode: errcode.OptsNotApplicable},
	// opts_too_large: `opts` exceeds its configured size cap. Also a
	// property of the request's opts field.
	"opts_too_large": {ErrCode: errcode.PayloadTooLarge},
	// page_limit: a sanitize upload's PDF declares more pages than
	// PROBE_MAX_PAGES (Phase 63, PROBE-01). An errcode binding, NOT a
	// finding, and the contrast with dimension_limit above is the reason
	// worth stating: declared pixel dimensions far past any real image ARE a
	// decompression-bomb SHAPE -- the number itself is the risk signal, which
	// is why that one binds to CodeImagePixelBombShape. A 300-page PDF is not
	// suspicious in any way; it is an ordinary document that costs more than
	// this deployment is configured to spend on it (~2.0 s/page). Reporting
	// it as a finding would put a risk claim in an inspect report about a
	// file that carries no risk, which is precisely the kind of false
	// statement this vocabulary exists to prevent.
	"page_limit": {ErrCode: errcode.TooManyPages},
}
