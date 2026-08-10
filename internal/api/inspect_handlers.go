// This file implements POST /v1/inspect (INSP-01..INSP-06): the synchronous
// endpoint that returns the truth about an uploaded file's content -- format,
// applicable characteristics, typed findings, a full closed-vocabulary
// checks section, honest coverage, and the operations this service could
// run on it -- WITHOUT ever writing to S3 or Postgres, and without pinning
// anything a later POST /v1/jobs could rely on to skip its own fail-closed
// checks (D-09/T-58-37).
//
// Everything this handler DOES was already built by earlier plans in this
// phase: format detection reuses the SAME magic-byte sniffers
// handleCreateJob uses (internal/convert), the findings/coverage vocabulary
// and the four family analyzers are internal/inspect (plans 58-02/58-04/
// 58-05), and the work-ceiling Tracker plus the concurrency/rate gates are
// plan 58-06 (internal/inspect/budget.go, internal/api/inspectgate.go).
// This file's only job is to wire them together without losing any one
// property at the seam.
package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/inspect"
)

// inspectContainerFormats / inspectImageFormats / inspectMediaFormats are
// the closed sets of magic-byte-detected formats this handler dispatches to
// AnalyzeContainer / AnalyzeImage / AnalyzeMedia respectively (58-07-PLAN.md
// Task 3's own action item 3). internal/inspect deliberately does not
// import internal/convert's format constants (D-01/D-02 layering: the
// vocabulary package knows nothing about engines), so this mapping is
// declared here, at the one place that already imports both packages.
var (
	inspectContainerFormats = map[string]bool{
		"docx": true, "xlsx": true, "pptx": true,
		"odt": true, "ods": true, "odp": true, "epub": true,
	}
	inspectImageFormats = map[string]bool{
		"png": true, "jpg": true, "webp": true, "heic": true, "tiff": true,
	}
	inspectMediaFormats = map[string]bool{
		"mp4": true, "mov": true, "mkv": true, "webm": true, "avi": true,
		"mp3": true, "wav": true, "m4a": true, "ogg": true,
	}
)

const (
	// inspectZipBombFindingThreshold is AnalyzeContainer's own
	// findings-level zip-bomb-shape threshold -- deliberately SMALLER than
	// s.maxDocumentUncompressedBytes (handleCreateJob's hard REJECTION
	// threshold, D-04): inspect only ever reports (D-03), so it is free to
	// surface a more conservative ratio than the request-creation path
	// would ever act on. 200 MiB is an SC6-calibrated starting point, not
	// a measured number, mirroring internal/inspect.Budget's own
	// documented discretion over its two ceilings.
	inspectZipBombFindingThreshold uint64 = 200 << 20
	// inspectPixelBombFindingThreshold is AnalyzeImage's own
	// findings-level pixel-bomb-shape threshold -- deliberately smaller
	// than s.maxImagePixels for the identical reason above. 50 megapixels
	// is an SC6-calibrated starting point.
	inspectPixelBombFindingThreshold uint64 = 50_000_000
	// inspectOLECFBFormat is the Report.Format value this handler reports
	// for an OLE-CFB compound file (legacy Office / password-protected
	// OOXML) -- these share one 8-byte magic-byte header (IsOLECFB), so
	// magic bytes alone can only prove "this IS an OLE-CFB container", not
	// which of doc/xls/ppt/encrypted-ooxml it specifically is; that finer
	// classification is exactly what ClassifyCFB's own Finding reports.
	// Never registered as a source format under any (operation, target)
	// triple -- availableOperations naturally reports an empty list for it.
	inspectOLECFBFormat = "ole_cfb"
)

// inspectResponse is the flat wire shape of POST /v1/inspect's 200 body:
// inspect.Report's own fields (embedded anonymously, so its JSON fields
// flatten directly into this struct's own object rather than nesting under
// a "report" key) plus the "operations" enumeration Task 2 built. Keeping
// the two separate types (inspect.Report / operationOffer) in their
// respective packages and merging ONLY here is deliberate: internal/inspect
// stays ignorant of engines and quotas (its own package doc comment,
// findings.go), and internal/api is the one place that legitimately knows
// about both.
type inspectResponse struct {
	inspect.Report
	Operations []operationOffer `json:"operations"`
}

// handleInspect accepts a multipart upload (field: file) and synchronously
// returns the truth about its content. It never writes to S3 or Postgres
// (this is a property this file's own tests hold it to, not merely an
// aspiration): no call in this file, or anything it calls, reaches
// s.storage.Upload or s.repo.Create.
func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	// D-06 mechanism 1's time ceiling, applied at the one place in this
	// handler that is genuinely I/O-bound and context-aware: the
	// Repo.PreAdmit call inside availableOperations below. The CPU-bound
	// family-analyzer dispatch further down is NOT bounded by this ctx --
	// it is bounded by the shared Tracker plus each analyzer's own hard
	// iteration caps (window.go's maxTokenScanIterations and siblings),
	// for the identical reason internal/inspect/budget.go's own Tracker
	// doc comment gives: a context deadline does not interrupt a
	// CPU-bound loop that never checks it. This ctx is therefore a real,
	// functioning courtesy bound for the ONE sub-call that can actually
	// observe it, not decoration.
	ctx, cancel := context.WithTimeout(r.Context(), s.inspectMaxWorkDuration)
	defer cancel()

	// 1. Accept. Deliberately repeats handleCreateJob's own multipart
	// intake SHAPE (MaxBytesReader -> ParseMultipartForm -> FormFile,
	// http.MaxBytesError -> 413, anything else -> 400) rather than
	// factoring it into a shared helper: two endpoints that both accept a
	// file upload must fail identically at the body-size boundary, and
	// inspect's own ceiling (s.inspectMaxUploadBytes) is deliberately
	// separate from and smaller than s.maxUploadByte (Config.
	// InspectMaxUploadBytes's own doc comment, api.go) -- this is a
	// conscious repetition of a proven shape, not copy-paste debt.
	r.Body = http.MaxBytesReader(w, r.Body, s.inspectMaxUploadBytes)
	if err := r.ParseMultipartForm(multipartInMemoryBudget); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge, "file exceeds size limit")
			return
		}
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid multipart form")
		return
	}

	file, header, err := r.FormFile(formFieldFile)
	if err != nil {
		writeError(w, http.StatusBadRequest, errcode.MissingFile, "missing file")
		return
	}
	defer file.Close()

	filename := path.Base(header.Filename)
	// declaredFormat is the normalized filename-extension-derived format --
	// carried into the response purely for display and for the
	// extension_mismatch finding below. It NEVER selects which family
	// analyzer runs (SC1's own prohibition on choosing behavior by
	// extension): detectInspectFormat below only consults it as one more
	// FAIL-CLOSED gate on top of a real content check (LooksLikeHTML/
	// LooksLikeLaTeX/LooksLikeText), the identical discipline
	// handleCreateJob already applies to the same three ambiguous families.
	declaredFormat := convert.NormalizeFormat(strings.TrimPrefix(path.Ext(filename), "."))

	client, _ := auth.ClientFromContext(r.Context())

	// 2. Detect. Same detector order handleCreateJob uses, so the two
	// endpoints of this service never disagree about what one file IS --
	// unlike handleCreateJob, unrecognized content here is a normal 200
	// (D-03): inspect only ever reports, it never rejects.
	detected := detectInspectFormat(file, header.Size, declaredFormat)

	// 3. Bound + dispatch. One Tracker for the whole operation (D-06
	// mechanism 1), shared across every family analyzer this dispatch
	// could invoke -- today, at most one analyzer runs per call (each
	// detected format maps to exactly one family), so "shared" mainly
	// documents intent for a future format that might need more than one.
	tr := inspect.NewTracker(inspect.Budget{MaxBytes: s.inspectMaxWorkBytes, MaxDuration: s.inspectMaxWorkDuration})
	families := dispatchInspectFamilies(detected, file, header.Size, tr)
	families = append(families, extensionMismatchFamilyResult(detected, declaredFormat))

	report := inspect.BuildReport(detected, declaredFormat, header.Size, tr, families...)

	// 4. Assemble. availableOperations (Task 2) reads convert.Default's own
	// registry and calls the SAME Repo.PreAdmit the job-creation path uses,
	// read-only, once per distinct engine.
	operations := s.availableOperations(ctx, client.ID, detected, header.Size)

	// 5. Nothing is written. Neither s.storage.Upload nor s.repo.Create is
	// ever reachable from this function or anything it calls (D-09/T-58-37)
	// -- inspect has no state that could go stale and no object that would
	// need a TTL, because it never creates either.
	writeJSON(w, http.StatusOK, inspectResponse{Report: report, Operations: operations})
}

// detectInspectFormat determines format by magic bytes only (SC1), in the
// EXACT SAME detector order handleCreateJob uses (internal/api/handlers.go)
// -- a comment there and here both name the reason: two endpoints of one
// service must never call one file two different formats, or a client
// acting on this endpoint's report would get rejected by the other. Unlike
// handleCreateJob, this function never rejects: an unrecognized upload
// returns "" (D-03).
//
// file is read via io.ReaderAt only -- this handler never forwards these
// bytes to storage.Upload, so it is free of the T-31-02 `rest`-continuity
// concern handleCreateJob's own chain must track. Every detector below that
// wants a plain io.Reader gets a FRESH io.NewSectionReader starting at
// offset 0 (never file's own sequential cursor, which an earlier detector
// may have already advanced) -- this is Inspect operating over an
// io.ReaderAt rather than a stream, exactly as 58-07-PLAN.md's own action
// text requires.
func detectInspectFormat(file multipart.File, size int64, declaredFormat string) string {
	fresh := func() io.Reader { return io.NewSectionReader(file, 0, size) }

	detected, _, err := convert.Sniff(fresh())
	if err != nil {
		detected = ""
	}

	if detected == "" {
		// ZIP-based office formats (docx/xlsx/pptx/odt/ods/odp/epub) all
		// share PK\x03\x04 with each other and a bare .zip -- structurally
		// inspect the central directory instead, mirroring handleCreateJob.
		var prefix [4]byte
		if n, _ := file.ReadAt(prefix[:], 0); n == 4 && bytes.Equal(prefix[:], []byte{'P', 'K', 3, 4}) {
			if cr, cerr := convert.SniffContainer(file, size); cerr == nil && cr.Format != "" && !cr.DuplicateRootPart {
				detected = cr.Format
			}
			// A DuplicateRootPart result, ErrNotAZip, or Format=="" leaves
			// detected empty intentionally, mirroring handleCreateJob.
		}
	}
	if detected == "" && declaredFormat == "html" && convert.LooksLikeHTML(file, size) {
		detected = "html"
	}
	if detected == "" && declaredFormat == "latex" && convert.LooksLikeLaTeX(file, size) {
		detected = "latex"
	}
	if detected == "" && (declaredFormat == "md" || declaredFormat == "rst" || declaredFormat == "org") && convert.LooksLikeText(file, size) {
		detected = declaredFormat
	}
	if detected == "" && convert.IsOLECFB(file) {
		detected = inspectOLECFBFormat
	}
	if detected == "" {
		if videoDetected, _, verr := convert.SniffVideo(fresh()); verr == nil && videoDetected != "" {
			detected = videoDetected
		}
	}
	if detected == "" {
		if audioDetected, _, aerr := convert.SniffAudio(fresh()); aerr == nil && audioDetected != "" {
			detected = audioDetected
		}
	}
	return detected
}

// dispatchInspectFamilies runs the ONE family analyzer applicable to
// detected (if any) under tr's shared byte/time budget. A format matching
// none of the four families (html/latex/md/rst/org/text, a bare zip, or any
// other unrecognized content) returns an empty slice -- inspect.BuildReport
// completes the full closed-vocabulary section from `not_applicable`
// fallbacks for that case, which is itself a correct, complete report
// (58-07-PLAN.md's own action text).
//
// tr.Remaining() is passed as maxBytes to whichever analyzer runs, and the
// SAME amount is Consume()d back into tr afterward -- a deliberately
// conservative accounting choice: no family analyzer reports back how many
// bytes it ACTUALLY read, only how many it was ALLOWED to read, so charging
// the full allowance is the safe direction to be imprecise in (charging less
// than what was actually available to read would risk letting several
// dispatched families in a future format collectively exceed tr's own
// ceiling without it ever noticing).
func dispatchInspectFamilies(detected string, file multipart.File, size int64, tr *inspect.Tracker) []inspect.FamilyResult {
	maxBytes := tr.Remaining()
	var families []inspect.FamilyResult

	switch {
	case detected == "pdf":
		families = append(families, inspect.AnalyzePDF(file, size, maxBytes))
	case inspectContainerFormats[detected]:
		families = append(families, inspect.AnalyzeContainer(file, size, maxBytes, inspectZipBombFindingThreshold))
	case inspectMediaFormats[detected]:
		families = append(families, inspect.AnalyzeMedia(detected, file, size, maxBytes))
	case inspectImageFormats[detected]:
		families = append(families, inspect.AnalyzeImage(detected, file, size, maxBytes, inspectPixelBombFindingThreshold))
	case detected == inspectOLECFBFormat:
		families = append(families, cfbFamilyResult(convert.ClassifyCFB(file, size)))
	default:
		return families
	}
	tr.Consume(maxBytes)
	return families
}

// cfbFamilyResult closes the three ole_cfb_* codes for an OLE-CFB compound
// file, from the SAME ClassifyCFB call detectInspectFormat's own IsOLECFB
// branch already implies -- computed here rather than re-classified inside
// a dedicated analyzer file, because the classification is already done by
// the time this handler knows detected == inspectOLECFBFormat, and
// repeating it would mean paying for the same bounded directory walk twice
// (58-07-PLAN.md Task 3's own action item 3). A compound file structurally
// IS exactly one of the three classes, never more than one -- the other two
// report not_applicable, not unknown, because there is nothing ambiguous
// about which class this file was classified into.
func cfbFamilyResult(class convert.CFBClass) inspect.FamilyResult {
	var findingCode inspect.Code
	switch class {
	case convert.CFBLegacy:
		findingCode = inspect.CodeOLECFBLegacy
	case convert.CFBEncrypted:
		findingCode = inspect.CodeOLECFBEncrypted
	default: // convert.CFBUnknown
		findingCode = inspect.CodeOLECFBUnclassified
	}

	sev, _ := inspect.SeverityOf(findingCode)
	res := inspect.FamilyResult{
		Findings: []inspect.Finding{{Code: findingCode, Severity: sev}},
	}
	for _, code := range []inspect.Code{inspect.CodeOLECFBLegacy, inspect.CodeOLECFBEncrypted, inspect.CodeOLECFBUnclassified} {
		if code == findingCode {
			continue
		}
		res.Checks = append(res.Checks, inspect.Check{Code: code, Status: inspect.StatusNotApplicable, Reason: inspect.ReasonFormatNotApplicable})
	}
	return res
}

// extensionMismatchFamilyResult closes CodeExtensionMismatch -- the one
// code in the closed vocabulary that no family analyzer ever claims,
// because it is a REPORT-LEVEL fact (detected format vs. the client's OWN
// filename extension), not a property of any one format family. Only the
// assembler, which holds both detected and declaredFormat, may safely
// decide this code's outcome (SC1: no family analyzer is ever given
// declaredFormat at all, so none of them COULD decide this even if they
// tried).
func extensionMismatchFamilyResult(detected, declaredFormat string) inspect.FamilyResult {
	switch {
	case detected == "" || declaredFormat == "":
		// Nothing to compare: an unrecognized upload, or a filename with no
		// extension at all, leaves this check with nothing to check.
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusNotApplicable, Reason: inspect.ReasonFormatNotApplicable},
		}}
	case detected != declaredFormat:
		sev, _ := inspect.SeverityOf(inspect.CodeExtensionMismatch)
		return inspect.FamilyResult{Findings: []inspect.Finding{
			{Code: inspect.CodeExtensionMismatch, Severity: sev},
		}}
	default:
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusClean},
		}}
	}
}
