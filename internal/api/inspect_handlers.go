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
	"encoding/binary"
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
	// inspectZipTooLargeFormat is the Detected-format sentinel for a
	// PK-shaped upload whose OWN declared End-Of-Central-Directory fields
	// (entry count and/or central-directory byte size) exceed
	// zipDirectoryExceedsStructuralBound's ceiling -- like
	// inspectOLECFBFormat above, never a real registered format, and never
	// matched by inspectContainerFormats. This is T-SLF-01's own fix: the
	// PR #9 finding at inspect_handlers.go:216 is that archive/zip.NewReader
	// (invoked, today, from BOTH detectInspectFormat's own
	// convert.SniffContainer call AND, again, from inside
	// inspect.AnalyzeContainer's dispatch) walks the ENTIRE declared
	// central directory before anything -- this handler, SniffContainer, or
	// AnalyzeContainer -- sees a single entry, so a near-100 MiB archive
	// declaring a huge number of tiny entries burns CPU/heap far beyond
	// s.inspectMaxWorkBytes regardless of every ceiling that only applies
	// AFTER that parse. Reaching this sentinel means neither
	// convert.SniffContainer's own zip.NewReader call NOR
	// inspect.AnalyzeContainer's ran at all for this request -- the bound
	// is checked from the EOCD record's OWN declared fields, at most 64 KiB
	// of ReadAt, zero decompression, never archive/zip.NewReader.
	inspectZipTooLargeFormat = "zip_directory_too_large"
)

const (
	// inspectMaxZipDirEntries is the structural ceiling on a ZIP-shaped
	// upload's central-directory entry count, expressed as an entry count
	// for readability -- but, since round 2 of PR #9 review
	// (PR9R2-F5-EOCD-UINT16-WRAP), it is NEVER compared directly against a
	// value the archive declares about itself. The EOCD's own entry-count
	// field is a uint16 that wraps at 65536: an archive with 65,537 minimal
	// 46-byte central-directory records declares entries=1, a value that
	// would sail under any ceiling this field could ever be compared
	// against, however tight. This constant is used ONLY to derive
	// inspectMaxZipDirBytes below, which is what the exceeds decision
	// actually reads (see zipDirectoryExceedsStructuralBound's own doc
	// comment). It deliberately matches internal/inspect/container.go's own
	// maxZipEntriesScanned value, for the same reason the round-1 comment
	// gave: setting it lower than that one would reject, at this earlier
	// gate, an archive AnalyzeContainer's own later per-entry scan cap
	// would have tolerated just fine. A real docx/xlsx/pptx/odt/ods/odp/
	// epub upload is a few dozen parts; 4096 is generous headroom over any
	// legitimate file.
	inspectMaxZipDirEntries = 4096
	// zipCentralDirMinRecordLen is the smallest a single ZIP central
	// directory file header record can ever be: 46 fixed bytes, with a
	// zero-length name, extra field, and comment. Every real record costs
	// AT LEAST this many bytes, so dirBytes / zipCentralDirMinRecordLen is
	// a valid UPPER bound on how many records a directory of a given
	// declared byte size could possibly contain -- unlike the EOCD's own
	// entry-count field, dirBytes is a uint32 (wraps at 4 GiB, far outside
	// any bound this handler would ever set), so this upper bound cannot be
	// defeated by the same wraparound trick.
	zipCentralDirMinRecordLen = 46
	// inspectMaxZipDirBytes bounds the DECLARED byte size of the central
	// directory itself (the EOCD's own cd-size field) -- and, since round 2
	// of PR #9 review, is the ONLY field zipDirectoryExceedsStructuralBound's
	// exceeds decision reads. Deriving it as inspectMaxZipDirEntries *
	// zipCentralDirMinRecordLen (~184 KiB) -- rather than round 1's flat
	// 4 MiB, chosen independently of the entry ceiling -- guarantees this
	// bound can never wave through a directory containing MORE than
	// inspectMaxZipDirEntries records, however those bytes are distributed
	// among them: every real record costs at least
	// zipCentralDirMinRecordLen bytes, so exceeding the entry ceiling
	// always costs more declared bytes than this bound allows, regardless
	// of what the EOCD's own (untrusted, wrapping) entry-count field says.
	//
	// The correspondence with inspectMaxZipDirEntries is now
	// ONE-DIRECTIONAL, not exact, and that asymmetry is a deliberate,
	// documented trade, not an oversight: an archive whose ACTUAL entry
	// count is within inspectMaxZipDirEntries but whose average record is
	// larger than the 46-byte minimum (long filenames, extra fields) can
	// still trip this bound even though internal/inspect/container.go's own
	// maxZipEntriesScanned cap would have scanned it to completion happily.
	// A real office/epub central directory is typically well under 100 KiB
	// (a few dozen parts, short ASCII names) -- comfortably inside the
	// ~184 KiB this derivation yields -- so this is headroom being traded
	// for a bound that cannot be defeated by a uint16 wraparound, not a
	// tightening expected to reject real uploads.
	inspectMaxZipDirBytes = inspectMaxZipDirEntries * zipCentralDirMinRecordLen
	// zipEOCDRecordMaxLen is the largest a ZIP End-Of-Central-Directory
	// record can ever be: the 22-byte fixed header plus the largest
	// possible comment (the comment-length field is a uint16, so at most
	// 65535 bytes). zipDirectoryExceedsStructuralBound never reads more
	// than this many bytes from the TAIL of the upload, regardless of the
	// upload's own total size.
	zipEOCDRecordMaxLen = 22 + 65535
	// zipEOCDFixedLen is the fixed (non-comment) portion of the EOCD record.
	zipEOCDFixedLen = 22
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
	// 1. Accept. Deliberately repeats handleCreateJob's own multipart
	// intake SHAPE (MaxBytesReader -> ParseMultipartForm -> FormFile,
	// http.MaxBytesError -> 413, anything else -> 400) rather than
	// factoring it into a shared helper: two endpoints that both accept a
	// file upload must fail identically at the body-size boundary, and
	// inspect's own ceiling (s.inspectMaxUploadBytes) is deliberately
	// separate from and smaller than s.maxUploadByte (Config.
	// InspectMaxUploadBytes's own doc comment, api.go) -- this is a
	// conscious repetition of a proven shape, not copy-paste debt.
	//
	// T-SLF-02/T-SLF-03 (PR #9 findings at inspectgate.go:120 and
	// inspect_handlers.go:114): body intake happens BEFORE this handler
	// takes the concurrency-gate slot AND before the D-06 mechanism-1 work
	// ceiling exists. Neither guards network transfer time -- the slot
	// exists to bound concurrent PARSING work (a client trickling its
	// upload arbitrarily slowly must not hold a slot it is not using), and
	// the work ceiling exists to bound the ANALYSIS this handler does
	// AFTER it already has the full body (a legitimately slow upload must
	// not arrive at Repo.PreAdmit below with an already-expired context,
	// which previously turned every quota verdict into "unknown"
	// regardless of the client's real quota state). Read intake is still
	// itself size-bounded by MaxBytesReader above, so this reordering does
	// not simply move an unbounded read earlier -- it only stops charging
	// a slot/deadline for time this handler spends waiting on the
	// network, neither of which it can shorten anyway.
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

	// 2. Gate. The D-06 mechanism-2 concurrency slot is taken HERE -- after
	// body intake has fully completed, immediately before the CPU-bound
	// analysis it actually protects -- rather than as outer middleware
	// wrapping this entire handler (routes.go no longer mounts a
	// concurrency gate ahead of this route for exactly this reason). See
	// inspectSemaphore.acquireOrReject's own doc comment (inspectgate.go)
	// for the T-SLF-02 finding this addresses. The D-06a property this
	// slot has always guaranteed -- that a held slot bounds concurrent
	// Repo.PreAdmit calls against the shared *pgxpool.Pool -- still holds
	// unchanged: PreAdmit (step 4 below) still runs while this slot is
	// held, only body transfer no longer does.
	release, ok := s.inspectSem.acquireOrReject(w)
	if !ok {
		return
	}
	defer release()

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
	detected, sniffedContainer := detectInspectFormat(file, header.Size, declaredFormat)

	// 3. Bound + dispatch. One Tracker for the whole operation (D-06
	// mechanism 1), shared across every family analyzer this dispatch
	// could invoke -- today, at most one analyzer runs per call (each
	// detected format maps to exactly one family), so "shared" mainly
	// documents intent for a future format that might need more than one.
	tr := inspect.NewTracker(inspect.Budget{MaxBytes: s.inspectMaxWorkBytes, MaxDuration: s.inspectMaxWorkDuration})
	families := dispatchInspectFamilies(detected, sniffedContainer, file, header.Size, tr)
	families = append(families, extensionMismatchFamilyResult(detected, declaredFormat))

	report := inspect.BuildReport(detected, declaredFormat, header.Size, tr, families...)

	// 4. Assemble. availableOperations (Task 2) reads convert.Default's own
	// registry and calls the SAME Repo.PreAdmit the job-creation path uses,
	// read-only, once per distinct engine.
	//
	// The work-ceiling context is created HERE -- after body intake AND
	// after the CPU-bound family-analyzer dispatch above -- not at handler
	// entry (T-SLF-03, PR #9 finding at inspect_handlers.go:114): this
	// ctx's only functioning consumer is the Repo.PreAdmit call inside
	// availableOperations, so its deadline must bound THAT call, not
	// however long the client's own upload took to arrive. Creating it at
	// handler entry meant any legitimately slow upload taking longer than
	// s.inspectMaxWorkDuration arrived at PreAdmit with an
	// already-expired context, turning every quota verdict into "unknown"
	// (quotaOfferFromPreAdmitError's non-quota-error branch) regardless of
	// the client's actual quota state -- INSP-04 degraded to permanently
	// unknown for exactly the uploads it most needs to answer honestly
	// about. The CPU-bound dispatch above is unaffected by this ctx either
	// way (bounded by tr's own Tracker, not by a context deadline -- see
	// this handler's own step-3 comment history / budget.go's doc comment
	// on why a context cannot interrupt a tight loop), so moving this
	// creation point later changes nothing about what that step bounds.
	ctx, cancel := context.WithTimeout(r.Context(), s.inspectMaxWorkDuration)
	defer cancel()
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
// detectInspectFormat also returns the ContainerResult it computed for a
// ZIP-shaped upload, if any, so dispatchInspectFamilies can reuse it instead
// of walking the same attacker-controlled central directory a second time
// (codex PR #9 finding 5). nil for every non-container upload.
func detectInspectFormat(file multipart.File, size int64, declaredFormat string) (string, *convert.ContainerResult) {
	fresh := func() io.Reader { return io.NewSectionReader(file, 0, size) }
	var sniffed *convert.ContainerResult

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
			// T-SLF-01 (PR #9 finding at inspect_handlers.go:216): check
			// the structural ceiling BEFORE convert.SniffContainer ever
			// runs -- that call invokes archive/zip.NewReader, which walks
			// the ENTIRE declared central directory unconditionally,
			// regardless of how many (or how few) entries it declares.
			// Checking this from the EOCD record's own declared fields
			// costs at most 64 KiB of ReadAt and zero decompression; a
			// check placed AFTER zip.NewReader would only bound work that
			// has already been paid for.
			if exceeds, _, _ := zipDirectoryExceedsStructuralBound(file, size); exceeds {
				detected = inspectZipTooLargeFormat
			} else if cr, cerr := convert.SniffContainer(file, size); cerr == nil && cr.Format != "" && !cr.DuplicateRootPart {
				detected = cr.Format
				sniffed = &cr
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
	return detected, sniffed
}

// zipDirectoryExceedsStructuralBound reports whether r's own declared
// central-directory byte size (the EOCD's cd-size field) exceeds this
// handler's structural ceiling (inspectMaxZipDirBytes), reading ONLY the
// tail of the file -- never more than zipEOCDRecordMaxLen bytes -- and
// never invoking archive/zip.NewReader (T-SLF-01). This is the ONE place in
// this handler that answers "how big a central directory is this upload
// declaring" before anything downstream pays the cost of actually parsing
// it.
//
// Round 2 of PR #9 review (PR9R2-F5-EOCD-UINT16-WRAP) found that the
// round-1 version of this function based its exceeds decision on BOTH the
// EOCD's entry-count field AND its cd-size field -- and the entry-count
// field is a uint16 that wraps at 65536. An archive with 65,537 minimal
// 46-byte central-directory records declares entries=1 while dirBytes lands
// around 2.9 MiB, comfortably under round 1's flat 4 MiB ceiling: both
// checks passed, and archive/zip.NewReader went on to allocate and walk all
// 65,537 records anyway. THIS version's exceeds decision reads dirBytes
// ONLY -- entries is still returned (and still read from the same bytes)
// purely for the caller's own diagnostics, but nothing in this function's
// control flow branches on its value. dirBytes is a uint32 (wraps at 4 GiB,
// far outside inspectMaxZipDirBytes), and inspectMaxZipDirBytes is itself
// derived from inspectMaxZipDirEntries * zipCentralDirMinRecordLen (its own
// doc comment), so a directory containing more than inspectMaxZipDirEntries
// records can never declare a dirBytes value under this bound, however that
// count is achieved.
//
// ZIP64 sentinel: dirBytes == 0xFFFFFFFF means "the real central-directory
// size is in the ZIP64 end-of-central-directory record, not this field" --
// it is treated as EXCEEDING the bound explicitly, rather than relying on
// the plain numeric comparison to reject it merely because 0xFFFFFFFF
// happens to be a large number. The same sentinel convention applies to the
// entries field (0xFFFF), but since entries is no longer part of the
// decision, no separate handling is needed for it here.
//
// The consistency check below (tailOffset+i+zipEOCDFixedLen+commentLen ==
// size) is the same one archive/zip's own unexported reader applies to
// resolve an identical ambiguity: an attacker-controlled EOCD comment could
// itself contain bytes that happen to match the signature, and without
// requiring the record's OWN declared comment length to land EXACTLY at the
// file's true end, this function could be fooled into reading a FAKE EOCD
// with a small declared directory size sitting earlier in the comment,
// letting the REAL (huge) directory right behind it through. Scanning
// backward from the highest offset and returning the FIRST occurrence
// satisfying that consistency check finds the genuine record (extremely
// unlikely to collide, and no more exploitable than the identical
// resolution the standard library itself relies on).
//
// If no EOCD record can be located within the tail at all (corrupt or
// truncated zip), this reports exceeds=false: that upload is not this
// function's problem to diagnose -- convert.SniffContainer's own zip.
// NewReader call fails it as ErrNotAZip exactly as it does today, and
// failing to locate a signature is not, itself, attacker-controlled WORK.
func zipDirectoryExceedsStructuralBound(r io.ReaderAt, size int64) (exceeds bool, entries uint16, dirBytes uint32) {
	if size < zipEOCDFixedLen {
		return false, 0, 0
	}
	tailLen := int64(zipEOCDRecordMaxLen)
	if tailLen > size {
		tailLen = size
	}
	tailOffset := size - tailLen
	buf := make([]byte, tailLen)
	n, err := r.ReadAt(buf, tailOffset)
	if err != nil && err != io.EOF {
		return false, 0, 0
	}
	buf = buf[:n]

	for i := len(buf) - zipEOCDFixedLen; i >= 0; i-- {
		if buf[i] != 'P' || buf[i+1] != 'K' || buf[i+2] != 0x05 || buf[i+3] != 0x06 {
			continue
		}
		commentLen := int64(binary.LittleEndian.Uint16(buf[i+20 : i+22]))
		if tailOffset+int64(i)+zipEOCDFixedLen+commentLen != size {
			// A signature that merely happens to appear inside an earlier,
			// attacker-controlled comment byte sequence, not the real EOCD.
			continue
		}
		// entries is read here for the caller's diagnostics ONLY -- the
		// exceeds decision below never uses it (see this function's own
		// doc comment for PR9R2-F5-EOCD-UINT16-WRAP, the finding that made
		// this split necessary).
		entries = binary.LittleEndian.Uint16(buf[i+10 : i+12])
		dirBytes = binary.LittleEndian.Uint32(buf[i+12 : i+16])
		// 0xFFFFFFFF is the ZIP64 "look elsewhere" sentinel, not a literal
		// declared size -- treated as exceeding explicitly, not merely
		// because the comparison below would also reject it as a large
		// number.
		exceeds = dirBytes == 0xFFFFFFFF || dirBytes > inspectMaxZipDirBytes
		return exceeds, entries, dirBytes
	}
	return false, 0, 0
}

// zipDirectoryCeilingFamilyResult is the FamilyResult reached when
// zipDirectoryExceedsStructuralBound trips: every container-domain code
// (mirroring internal/inspect/container.go's own containerDomainCodes)
// becomes an honest unknown/work_ceiling_bytes -- NOT clean (D-03's own
// prohibition on ever claiming cleanliness about bytes this handler
// declined to read), NOT not_applicable (this format IS a container, this
// handler simply refused to pay for parsing it), and never a 500. Using
// ReasonWorkCeilingBytes (not ReasonMalformedInput) is deliberate:
// status.go's own Reason doc comment draws exactly this line -- THIS
// SERVICE's own resource ceiling, not a property of the file, which is
// also why inspect.BuildReport's Coverage claim degrades to incomplete for
// this report (status.go buildCoverage), honestly telling the client this
// report is selective, not merely "this file happens to be unknown".
func zipDirectoryCeilingFamilyResult() inspect.FamilyResult {
	var res inspect.FamilyResult
	for _, code := range []inspect.Code{inspect.CodeOOXMLMacro, inspect.CodeZipBombShape, inspect.CodeExternalReference} {
		res.Checks = append(res.Checks, inspect.Check{Code: code, Status: inspect.StatusUnknown, Reason: inspect.ReasonWorkCeilingBytes})
	}
	return res
}

// dispatchInspectFamilies runs the ONE family analyzer applicable to
// detected (if any) under tr's shared byte/time budget. A format matching
// none of the four families (html/latex/md/rst/org/text, a bare zip, or any
// other unrecognized content) returns an empty slice, and inspect.BuildReport
// completes the closed-vocabulary section itself.
//
// WHICH fallback it uses depends on whether a format was determined at all,
// and the distinction is load-bearing: a KNOWN format with no family analyzer
// gets `not_applicable/format_not_applicable` (there was genuinely nothing of
// that kind to look for), while an UNRECOGNIZED format gets
// `unknown/format_unrecognized` -- applicability cannot be claimed when the
// format itself was never established. An earlier version of this comment
// said `not_applicable` for both cases; that was true of the code until the
// codex PR #9 review (finding 6) showed it made an unrecognized upload look
// like a file whose every risk check was confidently irrelevant.
//
// tr.Remaining() is passed as maxBytes to whichever analyzer runs, and the
// SAME amount is Consume()d back into tr afterward -- a deliberately
// conservative accounting choice: no family analyzer reports back how many
// bytes it ACTUALLY read, only how many it was ALLOWED to read, so charging
// the full allowance is the safe direction to be imprecise in (charging less
// than what was actually available to read would risk letting several
// dispatched families in a future format collectively exceed tr's own
// ceiling without it ever noticing).
func dispatchInspectFamilies(detected string, sniffed *convert.ContainerResult, file multipart.File, size int64, tr *inspect.Tracker) []inspect.FamilyResult {
	maxBytes := tr.Remaining()
	var families []inspect.FamilyResult

	switch {
	case detected == inspectZipTooLargeFormat:
		// T-SLF-01: the structural ceiling already tripped in
		// detectInspectFormat, before any zip.NewReader call -- reaching
		// here spends nothing further. tr.Consume is deliberately skipped
		// (this is the SAME `return` shape the default case below uses):
		// charging a byte budget for a parse that never happened would be
		// dishonest accounting, and this path never reads or allocates
		// based on this upload's own declared size at all.
		return append(families, zipDirectoryCeilingFamilyResult())
	case detected == "pdf":
		families = append(families, inspect.AnalyzePDF(file, size, maxBytes))
	case inspectContainerFormats[detected]:
		// Reuse detection's own sniff instead of walking the same
		// attacker-controlled central directory a second time (codex PR #9
		// finding 5). sniffed is non-nil for every format in
		// inspectContainerFormats, because that is precisely the set
		// detectInspectFormat can only have reached BY sniffing; the nil
		// branch stays as a correctness fallback rather than an assumption.
		if sniffed != nil {
			families = append(families, inspect.AnalyzeContainerWith(*sniffed, file, size, maxBytes, inspectZipBombFindingThreshold))
		} else {
			families = append(families, inspect.AnalyzeContainer(file, size, maxBytes, inspectZipBombFindingThreshold))
		}
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
	case detected == inspectZipTooLargeFormat:
		// T-SLF-01: detected does not name a real format at all here -- it
		// is the "declined to parse" sentinel, so there is genuinely
		// nothing to compare declaredFormat against, EITHER a match or a
		// mismatch would overclaim knowledge this handler does not have.
		// Unknown/work_ceiling_bytes (not not_applicable/
		// format_not_applicable below) is the honest answer: this IS a
		// container this handler could have compared, had it not declined
		// to parse it for its own resource reasons -- the identical
		// distinction zipDirectoryCeilingFamilyResult already draws for
		// the three container-domain codes.
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusUnknown, Reason: inspect.ReasonWorkCeilingBytes},
		}}
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
