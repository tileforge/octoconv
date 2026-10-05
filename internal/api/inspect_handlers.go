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
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

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
	// inspectZipReadFailedFormat is the Detected-format sentinel for a
	// PK-shaped upload whose first four bytes were readable (the PK\x03\x04
	// prefix matched) but convert.SniffContainer could not finish reading
	// what came after -- docsniff.go's own readerAtRecorder OBSERVED the
	// underlying io.ReaderAt itself fail (PR9R8-F3-SNIFF-FAILURE-OBSERVABLE,
	// PR9R8-F4-STRUCTURAL-VS-IO): this handler DID recognize the upload as
	// zip-shaped, it simply could not finish reading it, which is a fact
	// about THIS SERVICE's ability to read the upload, never a verdict
	// about the upload's own bytes -- see inspect.ReasonReadFailed's own
	// doc comment for the identical distinction drawn at the vocabulary
	// level. Never a real registered format, and never matched by
	// inspectContainerFormats, like inspectZipTooLargeFormat above.
	inspectZipReadFailedFormat = "zip_read_failed"
	// inspectZipMalformedFormat is the Detected-format sentinel for a
	// PK-shaped upload whose first four bytes were readable but
	// convert.SniffContainer's own zip.NewReader call failed STRUCTURALLY
	// (errors.Is(err, convert.ErrNotAZip)) -- docsniff.go's own
	// readerAtRecorder observed every ReadAt call it made succeed, so the
	// failure describes THIS UPLOAD's own bytes, not this handler's ability
	// to read them. Reported as inspect.ReasonMalformedInput, distinct from
	// inspectZipReadFailedFormat above precisely so a client can tell "this
	// service could not read your upload" apart from "your upload's own
	// central directory does not parse" -- collapsing the two back into one
	// generic format_unrecognized fallback is the exact defect PR9R8-F3
	// closes. Never a real registered format, and never matched by
	// inspectContainerFormats.
	inspectZipMalformedFormat = "zip_malformed"
	// inspectSniffReadFailedFormat is the Detected-format sentinel for an
	// upload whose very first read -- convert.Sniff's 12-byte magic peek --
	// failed operationally, and which no later detector managed to recognize
	// either.
	//
	// PR9R25 (inspect_handlers.go:509), verbatim: "When a disk-backed
	// multipart file returns a non-EOF error during the initial 12-byte magic
	// sniff, this branch discards the error and continues as though no
	// signature matched... the response consequently reports an empty format
	// with format_unrecognized checks instead of read_failed, conflating an
	// operational temp-file failure with genuinely unknown content."
	//
	// convert.Sniff returns a non-nil error ONLY for a genuine read failure
	// (an unrecognized upload is ("", rest, nil)), so this signal is
	// unambiguous at the source -- it was simply thrown away one line later.
	// Unlike its zip-shaped siblings above, this sentinel says nothing about
	// WHAT the upload is: the failure happened before any signature matched,
	// so every code in the vocabulary reports read_failed rather than only
	// the container-domain three.
	inspectSniffReadFailedFormat = "sniff_read_failed"
)

// inspectPublicSentinelFormats are the Detected-format sentinels that DO reach
// the wire as Report.Format. Each one says something true about the upload that
// an empty format would lose: this IS a zip, and here is why this service did
// not finish parsing it. docs/openapi.yaml documents them, and
// TestInspectSentinelFormatsMatchTheContract keeps the two lists in step.
var inspectPublicSentinelFormats = []string{
	inspectZipTooLargeFormat,
	inspectZipReadFailedFormat,
	inspectZipMalformedFormat,
}

// inspectInternalOnlyFormats are sentinels that must NEVER reach the wire.
//
// PR9R26 (inspect_handlers.go:626): inspectSniffReadFailedFormat leaked into
// the response as "format":"sniff_read_failed". Unlike the public sentinels
// above it describes no upload: the failure happened before any signature
// matched, so nothing about this file was recognized -- which is exactly the
// case the contract already covers ("Empty when nothing recognized the content
// at all"). The sentinel earns its keep by routing the dispatch to read_failed
// checks; it has nothing to say in a field that names formats.
var inspectInternalOnlyFormats = []string{
	inspectSniffReadFailedFormat,
}

// reportFormat maps a detected format to the value the public report carries,
// blanking the internal-only sentinels. Called once, at the report boundary,
// so every consumer BEFORE it (dispatch, extension-mismatch) still sees which
// sentinel it was.
func reportFormat(detected string) string {
	for _, internal := range inspectInternalOnlyFormats {
		if detected == internal {
			return ""
		}
	}
	return detected
}

const (
	// inspectIntakeMaxConcurrent bounds how many requests may be inside
	// http.Request.ParseMultipartForm -- buffering an untrusted upload into
	// the process heap -- AT THE SAME TIME (PR9R8-F1-INTAKE-BOUND, the
	// finding quoted verbatim at inspect_handlers.go:227 in this quick
	// task's PLAN.md). Moving s.inspectSem's own acquisition to AFTER body
	// intake (T-SLF-02, see handleInspect's own step-2 comment) correctly
	// stopped a slow client from starving the four analysis slots without
	// doing any analysis -- but it left THIS call, the one that actually
	// buffers the bytes, with no concurrency bound of its own: every
	// request that reaches this handler runs ParseMultipartForm
	// unconditionally, however many are already running it right now.
	//
	// inspectIntakeSem (api.go) holds this slot for exactly the duration of
	// ONE ParseMultipartForm call, released the instant it returns
	// (success or failure) -- NOT for the rest of the handler. This is
	// deliberately unlike the T-SLF-02 mistake, not a repeat of it: the
	// resource this slot protects (heap bytes ParseMultipartForm is
	// actively buffering) really is occupied for exactly as long as the
	// client's own network transfer takes, so holding the slot for that
	// long is correct, not wasteful -- unlike s.inspectSem's CPU/DB-pool
	// slot, which was never actually consumed while waiting on the network
	// and therefore should never be held for it.
	//
	// The arithmetic this capacity was chosen from, stated here because a
	// future "simplification" that drops it is exactly the kind of change
	// this project's own history shows recurring:
	//   - multipartInMemoryBudget (handlers.go) = 32 MiB: the MOST heap
	//     ParseMultipartForm will ever hold for ONE request, regardless of
	//     INSPECT_MAX_UPLOAD_BYTES's own 100 MiB ceiling on total body size
	//     -- bytes past this budget spill to a disk-backed temp file, not
	//     RAM (net/http's own ParseMultipartForm doc comment).
	//   - inspectIntakeMaxConcurrent (this constant) = 8.
	//   - Worst case: 8 * 32 MiB = 256 MiB -- a QUARTER of the deployed api
	//     pod's own resources.limits.memory = 1 GiB
	//     (deploy/chart/octoconv/values.yaml), not the ~30 * 32 MiB ≈
	//     960 MiB (94% of that same 1 GiB) an UNBOUNDED intake stage can
	//     reach under the default RATE_LIMIT_INSPECT_RPM=30 burst budget
	//     (api.go's own InspectRateLimitRPM default) -- the exact PR9R8-F1
	//     scenario this constant closes.
	//   - This is a SEPARATE budget from INSPECT_MAX_CONCURRENT's own
	//     default of 4 (Config.InspectMaxConcurrent): that one bounds the
	//     ANALYSIS phase, which only starts once a request has already
	//     released THIS slot (see handleInspect's own step-1/step-2 split),
	//     so the two are never charged against the same 1 GiB for the same
	//     concurrent request at the same time -- 4 concurrent analyses at
	//     INSPECT_MAX_WORK_BYTES's own 8 MiB default costs a further
	//     32 MiB, comfortably inside the 768 MiB this constant leaves free.
	//   - Deliberately NOT sized to 30 (matching RATE_LIMIT_INSPECT_RPM's
	//     own default): that would merely rename PR9R8-F1's own bug, not
	//     close it.
	//   - Deliberately a fixed Go constant, not a new env-configurable
	//     Config field: this bound protects a deployment-topology fact
	//     already hard-coded in the Helm chart (the pod's own memory
	//     ceiling), not a per-environment tuning knob like
	//     INSPECT_MAX_CONCURRENT.
	inspectIntakeMaxConcurrent = 8

	// inspectIntakeReadDeadline bounds how long ONE request's
	// ParseMultipartForm call may block waiting on that request's own body
	// bytes to arrive. cmd/api/main.go sets httpSrv.ReadHeaderTimeout
	// (10s) but configures no equivalent deadline for the REQUEST BODY --
	// PR9R8-F1's own finding text names this gap explicitly ("cmd/api/
	// main.go still configures no request-body timeout"). Without it, a
	// client that opens a connection and then sends bytes at an
	// arbitrarily slow rate could hold an inspectIntakeSem slot
	// indefinitely, which is a slowloris-shaped resource hold even though
	// this slot's OWN capacity (unlike s.inspectSem's) is meant to
	// tolerate genuinely slow network transfer, not an unbounded one.
	// Applied via http.NewResponseController(w).SetReadDeadline -- the
	// standard net/http mechanism (Go 1.20+) for bounding how long the
	// underlying connection's Read may block -- rather than a context
	// timeout: a context deadline alone does not unblock an in-progress
	// net.Conn.Read the way SetReadDeadline does.
	inspectIntakeReadDeadline = 30 * time.Second

	// inspectIntakeBusyMessage mirrors inspectBusyMessage's own
	// non-leaking discipline (inspectgate.go) -- names the SERVICE's
	// state, never a number -- but is worded for the intake stage
	// specifically: a client that saw inspectBusyMessage's own wording
	// ("...parsing ceiling...") after being rejected here, before any
	// parsing began, could reasonably read it as a report about the wrong
	// stage.
	inspectIntakeBusyMessage = "service is at its concurrent inspect-intake ceiling right now"
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
	// inspectMaxZipDirBytes is round 4's own bound, and it is READ
	// differently from every earlier round: it is no longer compared
	// against anything the archive declares about itself. It is now the
	// FIXED-SIZE WINDOW, starting at the EOCD's declared directoryOffset,
	// that zipDirectoryExceedsStructuralBound is willing to read and walk
	// one real central-directory record at a time (see that function's own
	// doc comment for the full history and the walk itself). Deriving it as
	// inspectMaxZipDirEntries * zipCentralDirMinRecordLen (~184 KiB) keeps
	// the same value round 2 chose, for the same reason: a window this size
	// can hold at least inspectMaxZipDirEntries real minimal-size records,
	// so a legitimate archive within the entry ceiling is never rejected
	// merely because the window ran out first (a real office/epub central
	// directory is typically well under 100 KiB -- a few dozen parts, short
	// ASCII names -- comfortably inside the ~184 KiB this derivation
	// yields). What changed is the READ site: round 2 used this constant to
	// bound a COMPARISON against a declared field; round 4 uses it to bound
	// an actual ReadAt call, so its value is now load-bearing for a
	// completely different reason (see zipDirectoryExceedsStructuralBound).
	inspectMaxZipDirBytes = inspectMaxZipDirEntries * zipCentralDirMinRecordLen
	// zipEOCDRecordMaxLen is round 5's own redefinition of this constant.
	// It used to be sized to the largest a single EOCD record could ever
	// legitimately BE (22 fixed bytes + the largest possible comment,
	// 65535 bytes = 65,557) -- reasoning that no well-formed record could
	// ever need this function to look further back than that. Round 5's
	// own divergence audit (see zipDirectoryExceedsStructuralBound's own
	// doc comment, divergence row #3) found that is the WRONG thing to
	// size this against: archive/zip's own readDirectoryEnd
	// ($GOROOT/src/archive/zip/reader.go:572) does not search "as far back
	// as the largest legitimate record could require" -- it unconditionally
	// searches the last 65*1024 (66,560) bytes of the file in its second
	// stage, 1,003 bytes MORE than 65,557, regardless of what any comment
	// declares. A comment that ends BEFORE the file's own end (Go tolerates
	// this -- divergence row #2) does not bound how far from the file's end
	// the EOCD SIGNATURE itself can sit, only how far the record it
	// introduces extends past it: the signature can sit anywhere within
	// that 66,560-byte window, all the way to its front edge, with a
	// near-zero declared comment length. A window narrower than Go's own
	// could miss a real EOCD record Go's own reader would find and walk --
	// this function would return exceeds=false having never SEEN the
	// record at all, the single most dangerous mistake it can make (worse
	// than any miscounted record: an unconditional pass). This constant is
	// now sized to match Go's own second-stage window exactly, not the
	// smaller, more "reasonable-looking" largest-legitimate-record bound.
	zipEOCDRecordMaxLen = 65 * 1024
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
	//
	// PR9R8-F1-INTAKE-BOUND (finding at inspect_handlers.go:227, quoted
	// verbatim in this quick task's PLAN.md): the paragraph above fixed
	// T-SLF-02 by moving s.inspectSem's own acquisition to AFTER this
	// point, but that left THIS call -- the one that actually buffers an
	// untrusted upload into the process heap -- with no concurrency bound
	// of its own at all. inspectIntakeSem closes that gap; see
	// inspectIntakeMaxConcurrent's own doc comment for the arithmetic this
	// capacity was chosen from. Acquired non-blocking, exactly like
	// s.inspectSem's own acquireOrReject below: a saturated intake stage is
	// an immediate, honest 429 (the same closed-vocabulary InspectBusy
	// code, never a 500), not a queued wait that would itself accumulate
	// goroutines.
	select {
	case s.inspectIntakeSem <- struct{}{}:
	default:
		w.Header().Set("Retry-After", strconv.Itoa(inspectBusyRetryAfterSeconds))
		writeError(w, http.StatusTooManyRequests, errcode.InspectBusy, inspectIntakeBusyMessage)
		return
	}

	// See inspectIntakeReadDeadline's own doc comment: bounds how long THIS
	// request's own ParseMultipartForm call may block on the network before
	// the slot just acquired above is released. Ignored when unsupported
	// (e.g. this package's own tests, which exercise handleInspect through
	// httptest.NewRecorder rather than a real net.Conn) -- the
	// inspectIntakeMaxConcurrent bound above still holds either way, since
	// it does not depend on this deadline to be effective.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(inspectIntakeReadDeadline))

	r.Body = http.MaxBytesReader(w, r.Body, s.inspectMaxUploadBytes)
	parseErr := r.ParseMultipartForm(multipartInMemoryBudget)
	// Released HERE, immediately after the buffering call returns --
	// success or failure -- not deferred to handler exit: the resource this
	// slot bounds (heap bytes ParseMultipartForm is actively filling) is
	// spent by the time this call returns, whatever ended up in memory from
	// here on is already accounted for by multipartInMemoryBudget's own
	// per-request ceiling, the same as it always was before this fix.
	<-s.inspectIntakeSem
	if parseErr != nil {
		var maxErr *http.MaxBytesError
		if errors.As(parseErr, &maxErr) {
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

	// 2. Bound, THEN detect. One Tracker for the whole operation (D-06
	// mechanism 1), shared across every family analyzer this dispatch could
	// invoke -- today, at most one analyzer runs per call (each detected
	// format maps to exactly one family), so "shared" mainly documents
	// intent for a future format that might need more than one.
	//
	// It is constructed BEFORE detection since Phase 59 (D-12). It used to
	// come after, which meant detection's own reads -- the content-matcher
	// window, up to convert.SniffWindowBytes -- were spent outside any
	// budget at all: the gap this package's round-23 sweep found. Moving the
	// construction up is the whole fix; nothing about the budget's size or
	// its semantics changes.
	tr := inspect.NewTracker(inspect.Budget{MaxBytes: s.inspectMaxWorkBytes, MaxDuration: s.inspectMaxWorkDuration})

	// 3. Detect. Same detector order handleCreateJob uses, so the two
	// endpoints of this service never disagree about what one file IS --
	// unlike handleCreateJob, unrecognized content here is a normal 200
	// (D-03): inspect only ever reports, it never rejects.
	detected, sniffedContainer, sniffedZip := detectInspectFormat(file, header.Size, declaredFormat, tr)

	// 4. Dispatch.
	families := dispatchInspectFamilies(detected, sniffedContainer, sniffedZip, file, header.Size, tr)
	families = append(families, extensionMismatchFamilyResult(detected, declaredFormat))

	// reportFormat, not detected: an internal routing sentinel must not be
	// published as though it were a format this service detected (PR9R26).
	// Every consumer above still sees the sentinel itself.
	report := inspect.BuildReport(reportFormat(detected), declaredFormat, header.Size, tr, families...)

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

// readSniffWindow reads the single window the four content matchers share,
// and is the ONE place a read failure among them becomes visible (Phase 59,
// D-12).
//
// It returns a distinguishable error for a genuine, non-EOF ReadAt failure,
// and (bytes, nil) otherwise -- INCLUDING for a short read or a zero-length
// result. Those are not failures: io.EOF is the ordinary end of input, and a
// caller that treated a legitimately-empty upload as an I/O error would have
// invented the mirror image of the bug this function exists to fix.
//
// The window is min(size, convert.SniffWindowBytes), allocated at exactly
// that length and never at a length taken from the file's own content --
// the same rule readWindow (internal/inspect/window.go) states for its side
// of the service.
//
// Why the matchers cannot do this themselves: LooksLikeHTML, LooksLikeLaTeX,
// LooksLikeText and IsOLECFB all return a bare bool, so a read failure
// inside any of them is indistinguishable from "not this format". Widening
// those four signatures to (bool, error) was considered and rejected (D-12):
// they are shared with the conversion path (internal/api/handlers.go,
// internal/convert/pandoc.go), where the caller genuinely wants only a bool,
// and adding error plumbing to fail-closed conversion code to serve
// inspect's reporting needs is a bad trade. Reading once out here costs those
// callers nothing.
func readSniffWindow(r io.ReaderAt, size int64) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	n := size
	if n > convert.SniffWindowBytes {
		n = convert.SniffWindowBytes
	}
	buf := make([]byte, n)
	read, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:read], nil
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
func detectInspectFormat(file multipart.File, size int64, declaredFormat string, tr *inspect.Tracker) (string, *convert.ContainerResult, *zip.Reader) {
	fresh := func() io.Reader { return io.NewSectionReader(file, 0, size) }
	var sniffed *convert.ContainerResult
	// PR9R24 (container.go:157): the archive this sniff parses is handed on to
	// the container analyzer, so the central directory is walked ONCE per
	// request -- SniffContainer's own long-standing rule, which the analyzer
	// could not honor while it was given the verdict without the reader.
	var sniffedZip *zip.Reader

	detected, _, err := convert.Sniff(fresh())
	// sniffReadFailed is remembered rather than acted on immediately: a failed
	// magic peek does not prove the later detectors will fail too, and
	// suppressing a detection they DO make would trade one false report for
	// another. It decides the outcome only if the whole chain below comes up
	// empty (PR9R25).
	//
	// PR9R27 (inspect_handlers.go:657): it accumulates across EVERY sniffer in
	// the chain that can report a read failure, not just the first one. Round
	// 25 fixed the initial peek and left its own siblings -- SniffVideo and
	// SniffAudio, whose errors were discarded exactly as this one used to be --
	// so a temp file that failed mid-chain still reported format_unrecognized.
	// Fixing the named site and not its neighbours is how the previous six
	// rounds of this class kept recurring.
	//
	// All three sniffers share one contract that makes this safe: an
	// unrecognized upload is ("", rest, nil), so a non-nil error is ALWAYS a
	// genuine read failure, never a verdict about the bytes.
	sniffReadFailed := err != nil
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
			if outcome, _, _ := zipDirectoryExceedsStructuralBound(file, size); outcome.exceeds() {
				// The refusal stands in all three cases; only the reason
				// differs, and reporting one as another is a false statement
				// about a file the client may be able to fix (PR9R11 for the
				// read failure, PR9R12 for the malformed case: a tiny corrupt
				// archive crosses no configured ceiling at all).
				switch outcome {
				case zipBoundReadFailed:
					detected = inspectZipReadFailedFormat
				case zipBoundMalformed:
					detected = inspectZipMalformedFormat
				default:
					detected = inspectZipTooLargeFormat
				}
			} else if cr, zr, cerr := convert.SniffContainerReader(file, size); cerr == nil && cr.Format != "" && !cr.DuplicateRootPart {
				detected = cr.Format
				sniffed = &cr
				sniffedZip = zr
			} else if cerr != nil {
				// PR9R8-F2/F3 (finding at inspect_handlers.go:371, quoted
				// verbatim in this quick task's PLAN.md): the PREVIOUS
				// version of this branch was a success-only conditional --
				// any non-nil cerr silently left detected empty, the SAME
				// outcome as a genuinely unrecognized upload. That made
				// inspect.ReasonReadFailed unreachable on this production
				// sniff path: a real ReadAt failure while reading the EOCD
				// or central directory produced the identical
				// unknown/format_unrecognized report a truly unknown format
				// would, so the new reason existed only in a unit test, not
				// in what a client could ever observe. docsniff.go's own
				// readerAtRecorder (PR9R8-F4-STRUCTURAL-VS-IO) makes the
				// distinction below an OBSERVED fact, not a guess from
				// cerr's own shape: errors.Is(cerr, convert.ErrNotAZip) is
				// true if, and only if, every ReadAt the recorder wrapped
				// actually succeeded.
				if errors.Is(cerr, convert.ErrNotAZip) {
					detected = inspectZipMalformedFormat
				} else {
					detected = inspectZipReadFailedFormat
				}
			}
			// A DuplicateRootPart result or Format=="" (cerr == nil) leaves
			// detected empty intentionally, mirroring handleCreateJob.
		}
	}
	// ONE window for all four content matchers below (Phase 59, D-12).
	//
	// Each of them used to do its own ReadAt, so a read that failed inside
	// any of them was indistinguishable from "not this format" -- they
	// return a bare bool. An upload whose temp file died on exactly these
	// reads was reported to the client as format_unrecognized: a claim about
	// the FILE, made on the strength of our own inability to read it. That
	// is the same class as PR9R25/PR9R27 above, one layer down.
	//
	// Three outcomes, and all three must stay distinguishable:
	//
	//   - read error   -> sniffReadFailed, and the matchers do not run at all
	//   - bytes read, envelope rejects them (NUL, invalid UTF-8, empty)
	//                  -> genuinely "not a text-shaped upload"; the chain
	//                     continues, and this is NOT a read failure: a binary
	//                     file reads perfectly well
	//   - bytes read and accepted -> the three text matchers judge them
	//
	// Collapsing the middle case into the first would be a new lie in the
	// opposite direction, so it is kept separate rather than folded in.
	//
	// The bytes are charged to the operation's budget here, which they never
	// were before -- the gap this package's own round-23 sweep found. Without
	// it a client could make this handler read SniffWindowBytes for detection
	// AND the full configured allowance for analysis, exceeding the ceiling
	// the service reports as enforced.
	//
	// Consume's RESULT is deliberately not a gate, and this is the subtle
	// part. The bytes have already been read by the time it is called;
	// refusing to LOOK at bytes already paid for buys nothing and costs
	// something real -- detection would fall through to an empty format, and
	// the client would be told format_unrecognized, which is a claim about
	// the FILE made on the strength of OUR budget. That is the exact
	// substitution this phase removes elsewhere. What the call is for is the
	// accounting and the exhaustion latch: once tr is exhausted,
	// dispatchInspectFamilies hands the family analyzer tr.Remaining() == 0
	// and every check honestly reports work_ceiling_bytes.
	//
	// On the PR9R9 "sole owner" invariant: that finding was about media.go
	// charging tr a SECOND time for THE SAME bytes dispatchInspectFamilies
	// had already charged -- one resource, two owners, double-counted. This
	// call charges a DIFFERENT read (the detection window) exactly once, and
	// dispatch continues to charge its own allowance exactly once from
	// whatever remains. Two distinct reads, two distinct charges, no byte
	// counted twice. dispatchInspectFamilies's own doc comment is updated to
	// say so rather than left claiming an exclusivity that no longer holds.
	if detected == "" {
		window, windowErr := readSniffWindow(file, size)
		switch {
		case windowErr != nil:
			sniffReadFailed = true
		default:
			tr.Consume(int64(len(window)))

			// Matcher ORDER is preserved exactly as it was before this
			// refactor -- the three text matchers, then OLE-CFB. The two
			// groups cannot both match one file (the OLE-CFB signature
			// starts 0xD0 0xCF, which is neither valid UTF-8 nor free of
			// NUL, so the text envelope rejects it), which means the order
			// is unobservable today. It is kept anyway: this change is
			// supposed to move where bytes are read, not what the chain
			// decides, and an unobservable reordering is still a second
			// change riding along inside the first.
			//
			// ValidateSniffPrefix's rejection here means "not a text-shaped
			// upload", never "the read failed" -- it does no I/O at all.
			if prefix, textShaped := convert.ValidateSniffPrefix(window, size); textShaped {
				switch {
				case declaredFormat == "html" && convert.LooksLikeHTMLFromPrefix(prefix):
					detected = "html"
				case declaredFormat == "latex" && convert.LooksLikeLaTeXFromPrefix(prefix):
					detected = "latex"
				case (declaredFormat == "md" || declaredFormat == "rst" || declaredFormat == "org") && convert.LooksLikeTextFromPrefix(prefix):
					detected = declaredFormat
				}
			}
			if detected == "" && convert.IsCADDeclared(declaredFormat) {
				head := window[:min(len(window), convert.CADHeadBytes)]
				var tail []byte
				var tailErr error
				if int64(len(window)) == size {
					tail = window[max(len(window)-convert.CADTailBytes, 0):]
				} else if tail, tailErr = convert.ReadCADTail(file, size); tailErr == nil {
					tr.Consume(int64(len(tail)))
				}
				if tailErr != nil {
					sniffReadFailed = true
				} else if res := convert.SniffCADWindows(head, tail, size, declaredFormat); res.Rejection == convert.CADRejectNone {
					detected = res.Format
				}
			}
			// IsOLECFB works on RAW bytes, deliberately not on the validated
			// prefix: the signature is binary throughout, so the text
			// envelope would (correctly, for its own purpose) reject it
			// outright. One read, two views of it.
			if detected == "" && convert.IsOLECFBFromPrefix(window) {
				detected = inspectOLECFBFormat
			}
		}
	}
	if detected == "" {
		videoDetected, _, verr := convert.SniffVideo(fresh())
		switch {
		case verr != nil:
			sniffReadFailed = true
		case videoDetected != "":
			detected = videoDetected
		}
	}
	if detected == "" {
		audioDetected, _, aerr := convert.SniffAudio(fresh())
		switch {
		case aerr != nil:
			sniffReadFailed = true
		case audioDetected != "":
			detected = audioDetected
		}
	}
	if detected == "" && sniffReadFailed {
		// Nothing in the chain recognized the upload, and at least one read of
		// it failed. "We could not read your file" and "we do not recognize your
		// file" are different facts about different subjects -- this service
		// and the upload -- and reporting the first as the second is the exact
		// conflation PR9R8-F3 closed for the zip path and PR9R25 found still
		// open on the path that runs before it.
		detected = inspectSniffReadFailedFormat
	}
	return detected, sniffed, sniffedZip
}

// zipDirectoryExceedsStructuralBound reports whether the ZIP EOCD record
// found in the tail of r describes a central directory this handler is
// willing to pay to parse. This is the FIFTH time this decision has been
// implemented, and the history is recorded here, in full, because it is
// exactly the kind of thing a future "simplification" would reintroduce
// without it:
//
//  1. Round 1 compared the EOCD's own entry-count field (a uint16) against a
//     ceiling. The entry-count field WRAPS at 65536: an archive with 65,537
//     minimal 46-byte central-directory records declares entries=1, sailing
//     under any ceiling that field could ever be compared against.
//
//  2. Round 2 (PR9R2-F5-EOCD-UINT16-WRAP) dropped entries from the decision
//     and compared the EOCD's declared central-directory BYTE SIZE (dirBytes,
//     a uint32) against inspectMaxZipDirBytes instead -- reasoning that a
//     uint32 cannot represent the same wraparound at any bound this handler
//     would ever set. This closed the SPECIFIC round-1 bypass, but not the
//     underlying mistake: dirBytes is EQUALLY a field the archive declares
//     about itself, and nothing forces it to describe the real bytes on
//     disk. Round 4's review built an archive with 65,537 REAL, valid
//     46-byte central-directory records (so archive/zip.NewReader really
//     does walk all of them once it starts reading), entries=1 (still
//     wrapped, now irrelevant either way), and dirBytes=46 -- a bald-faced
//     lie about the directory's true size that round 2's comparison
//     (46 > inspectMaxZipDirBytes? no) waved straight through.
//
//  4. Round 4 stopped comparing declared fields against ceilings entirely.
//     It reads the EOCD's declared directoryOffset (still untrusted -- only
//     ever used as a place to START reading), then WALKS the actual bytes at
//     that offset, counting real "PK\x01\x02" signatures one at a time,
//     advancing by each record's own real length, inside a window WE choose
//     (inspectMaxZipDirBytes) -- exceeds=true if the count crosses
//     inspectMaxZipDirEntries, or if the window runs out before the walk
//     reaches the EOCD's own LOCATED offset ("I don't know" means "I don't
//     let it through"). This closed round 1 and round 2's mistake (bounding
//     a hostile structure by reading that structure's OWN description of
//     itself) -- but it was still, itself, a PREDICTION of what
//     archive/zip.Reader.init would do with that same directoryOffset,
//     written by re-deriving the shape of $GOROOT/src/archive/zip/reader.go
//     from memory rather than checking against it line by line.
//
//  5. Round 5's own finding (inspect_handlers.go:530, quoted verbatim in
//     this quick task's PLAN.md): "archive/zip does not necessarily start
//     at the raw EOCD directoryOffset: it derives baseOffset = eocdOffset -
//     dirBytes - directoryOffset and reads from baseOffset+directoryOffset."
//     An archive can declare an HONEST full dirBytes (matching the real,
//     total directory size), a wrapped entries=1, and a directoryOffset
//     field pointing only at the FINAL ~100 records of a real 65,537-record
//     directory. Round 4's walk starts exactly at that directoryOffset,
//     finds ~100 real records, reaches eocdOffset, and returns
//     exceeds=false. archive/zip does not: it computes
//     baseOffset = eocdOffset - dirBytes - directoryOffset, which -- because
//     dirBytes is honest and describes the FULL 65,537-record span -- comes
//     out sharply negative, and reads from baseOffset+directoryOffset
//     instead: the TRUE start of the real directory, walking all 65,537
//     records. Round 4's walk and archive/zip's walk start at two DIFFERENT
//     offsets for the identical input.
//
// Orchestrator-read divergence audit (round 5), built by reading
// readDirectoryEnd, findSignatureInBlock, Reader.init, and
// readDirectoryHeader in $GOROOT/src/archive/zip/reader.go line by line,
// not by re-deriving their shape from memory -- the review named only
// row #1 below; rows #2-4 were found by this audit and are exactly as
// load-bearing:
//
//	# | Go does (reader.go)                          | Round 4 did             | Divergent input                                            | Closed by
//	1 | baseOffset = eocdOffset-dirBytes-dirOffset;  | walked from the raw     | Honest full dirBytes, wrapped entries=1,                   | Refuse whenever
//	  | reads from baseOffset+dirOffset (:624,:137). | dirOffsetField          | dirOffsetField pointing at the directory's                | baseOffset != 0
//	  | baseOffset>0 has its OWN fallback to 0       | directly, ignoring      | final ~100 records                                         | (see below)
//	  | (:636-641): two possible starts, content-     | baseOffset entirely.   |                                                             |
//	  | dependent.                                    |                        |                                                             |
//	2 | findSignatureInBlock accepts a comment that   | Required the comment   | An EOCD whose comment ends BEFORE the file's own end,      | locateZipEOCDInBlock
//	  | ends AT OR BEFORE the block's own end          | to end EXACTLY at the  | with a real, larger directory further back that Go        | mirrors Go's own
//	  | (n+22+i<=len(b)); on the FIRST 'PK\x05\x06'    | file's own end,        | would locate and walk, that this locator would either      | rule exactly: accept
//	  | candidate found scanning backward, truncation  | and kept scanning      | miss (continuing past it) or misidentify (an earlier,      | with <=, and on the
//	  | is a HARD failure -- it never tries an earlier  | backward past any      | attacker-placed fake signature).                           | FIRST candidate,
//	  | candidate (:697-713).                          | mismatch for another   |                                                             | truncated or not,
//	  |                                                 | signature.             |                                                             | stop -- never keep
//	  |                                                 |                        |                                                             | scanning backward.
//	3 | readDirectoryEnd searches the last 1024 bytes, | Read a single tail     | An EOCD signature sitting between (size-66560) and         | zipEOCDRecordMaxLen
//	  | then (if not found) the last 65*1024 (66,560)  | window of 22+65535=    | (size-65557): within Go's own second-stage window, outside | widened to 65*1024,
//	  | bytes -- unconditionally, regardless of what   | 65,557 bytes.          | ours -- this function would locate NO EOCD at all and       | matching Go's own
//	  | any comment declares (:569-588).               |                        | return exceeds=false, never having seen the real record.    | second-stage size.
//	4 | ZIP64 "look elsewhere" trigger compares         | Compared dirBytes      | dirBytes == 0xFFFF (65,535): triggers Go's own ZIP64        | Added dirBytes ==
//	  | directorySize against 0xffff, NOT 0xffffffff   | against 0xFFFFFFFF     | detour (a value we were NOT treating as a sentinel at      | 0xFFFF as an
//	  | (:608).                                        | only.                  | all), while our own check let it fall through as an        | additional (not
//	  |                                                 |                        | ordinary, comparably-small declared size.                  | replacing) sentinel.
//
// Reviewed and found to introduce NO further divergence relevant to this
// function's own decision (see this quick task's SUMMARY.md for the fuller
// writeup): Reader.init's preallocation heuristic (:132, a pure allocation
// optimization); the uint16-masked directoryRecords comparison Reader.init
// makes AFTER the per-record read loop (:158-162, irrelevant here because
// round 4 already abandoned comparing declared entry counts); readDirectory-
// Header's zip64-extra-field handling for an individual record's own sizes
// (:426-454, changes what a record MEANS, never how many bytes it occupies,
// so it cannot affect this function's own position-advancing walk); and the
// "record boundary overshoots eocdOffset" edge case (a record's own declared
// nameLen/extraLen/commentLen carries the walk PAST the located EOCD offset
// without landing on it exactly) -- pos only ever increases, so once it
// overshoots it can never return to == eocdOffset-directoryOffset, meaning
// the walk can only end via window-exhaustion, a bad signature, or the
// count ceiling -- all three already exceeds=true.
//
// Round 5's fix is a REFUSAL, not a mirror of Go's own baseOffset>0 fallback
// (:636-641): rather than replicating that fallback's own content-dependent
// choice between two possible starts, this function computes the identical
// baseOffset formula and refuses outright (exceeds=true) whenever it is
// anything other than zero. This is sound, not merely conservative: every
// legitimate docx/xlsx/pptx/odt/ods/odp/epub upload has baseOffset==0 (no
// leading prefix bytes before the ZIP proper), and when baseOffset==0, both
// of Go's own possible starts collapse into the SAME one -- Go's own
// fallback branch (:636) never triggers, because it only exists to correct
// a NONZERO baseOffset. Refusing whenever baseOffset!=0 removes the
// divergence surface entirely rather than trying to track it.
//
// A cheap, sound, non-bypassable pre-check rides alongside the walk: dirBytes
// cannot legitimately exceed the upload's own real total byte count (size) --
// that comparison is NOT round 2's mistake repeated, because size is ground
// truth (the actual number of bytes this handler received), not a second
// number the archive also controls.
//
// ZIP64 sentinels (dirBytes == 0xFFFFFFFF or 0xFFFF, the declared directory
// offset == 0xFFFFFFFF, or entries == 0xFFFF) are treated as exceeding
// explicitly, not merely because a numeric comparison happens to also
// reject them.
//
// The EOCD-location step below now mirrors archive/zip's own
// findSignatureInBlock rule exactly (locateZipEOCDInBlock, divergence row #2
// above) over a window sized to Go's own second-stage search (divergence
// row #3 above) -- a single such window is provably equivalent to Go's own
// two-stage (1024-byte, then 65*1024-byte) search for locating the SAME
// EOCD record: both scans start from the same absolute end-of-file anchor
// and walk backward in the same order, so the nearest-to-end candidate
// either scan encounters first is identical whenever both windows are large
// enough to reach it, and a smaller Go stage failing to reach it at all
// simply defers to Go's own larger stage, which this function's own single
// maximal window already covers.
//
// If no EOCD record can be located within the tail at all (corrupt or
// truncated zip), this reports exceeds=false: that upload is not this
// function's problem to diagnose -- convert.SniffContainer's own zip.
// NewReader call fails it as ErrNotAZip (or an equivalent parse error)
// exactly as it does today, and failing to locate a signature is not,
// itself, attacker-controlled WORK -- if archive/zip's own two-stage search
// also cannot locate a valid, non-truncated EOCD anywhere in the identical
// bytes (which locateZipEOCDInBlock's mirrored rule guarantees), it never
// reaches the per-record walk that round 4/5 exist to bound in the first
// place.
func zipDirectoryExceedsStructuralBound(r io.ReaderAt, size int64) (outcome zipBoundOutcome, entries uint16, dirBytes uint32) {
	if size < zipEOCDFixedLen {
		return zipBoundOK, 0, 0
	}
	tailLen := int64(zipEOCDRecordMaxLen)
	if tailLen > size {
		tailLen = size
	}
	tailOffset := size - tailLen
	buf := make([]byte, tailLen)
	n, err := r.ReadAt(buf, tailOffset)
	if err != nil && err != io.EOF {
		return zipBoundOK, 0, 0
	}
	buf = buf[:n]

	i, found := locateZipEOCDInBlock(buf)
	if !found {
		return zipBoundOK, 0, 0
	}
	eocdOffset := tailOffset + int64(i)
	// entries and dirBytes are read here and RETURNED for the caller's
	// diagnostics -- exactly as round 2 left them -- but the exceeds
	// decision (below) never compares either of them against a ceiling by
	// itself; see this function's own doc comment.
	entries = binary.LittleEndian.Uint16(buf[i+10 : i+12])
	dirBytes = binary.LittleEndian.Uint32(buf[i+12 : i+16])
	dirOffsetField := binary.LittleEndian.Uint32(buf[i+16 : i+20])

	return zipCentralDirectoryExceedsBound(r, size, eocdOffset, dirOffsetField, dirBytes, entries), entries, dirBytes
}

// locateZipEOCDInBlock finds the EOCD signature within buf using the EXACT
// selection rule archive/zip's own unexported findSignatureInBlock uses
// ($GOROOT/src/archive/zip/reader.go:697-713, divergence row #2 in
// zipDirectoryExceedsStructuralBound's own doc comment): scan backward from
// buf's own end, and on the VERY FIRST 'PK\x05\x06' byte sequence
// encountered (nearest to buf's own end), either accept it -- if its
// declared comment fits inside buf, where the comment is explicitly allowed
// to end BEFORE buf's own end, not only exactly at it -- or reject the
// ENTIRE search outright, with no further scanning, if the declared comment
// would run past buf's own end (a truncated comment). Go NEVER continues
// past the first candidate it finds, truncated or not; rounds 1 through 4 of
// this function's own history required an EXACT match to the file's end and
// kept scanning backward past any mismatch, a divergence this round's own
// audit found undocumented in every prior round. Mirroring Go's rule exactly
// is the only way to guarantee this locator and archive/zip's own never
// select a different EOCD record for the same bytes.
func locateZipEOCDInBlock(buf []byte) (i int, ok bool) {
	for i := len(buf) - zipEOCDFixedLen; i >= 0; i-- {
		if buf[i] != 'P' || buf[i+1] != 'K' || buf[i+2] != 0x05 || buf[i+3] != 0x06 {
			continue
		}
		commentLen := int(buf[i+20]) | int(buf[i+21])<<8
		if commentLen+zipEOCDFixedLen+i > len(buf) {
			// Truncated: the declared comment would run past buf's own end.
			// Go hard-fails here (returns "not found" for the WHOLE search),
			// it does not keep scanning backward for an earlier candidate --
			// see this function's own doc comment.
			return 0, false
		}
		return i, true
	}
	return 0, false
}

// zipCentralDirectoryExceedsBound is round 4/5's own decision, isolated from
// the EOCD-location scan above so its own doc comment (on
// zipDirectoryExceedsStructuralBound) stays readable. size is the upload's
// real, ground-truth byte count; eocdOffset is the REAL, LOCATED byte offset
// of the EOCD record (a structural fact, not a declared one);
// dirOffsetField/dirBytes/entriesField are read directly from the EOCD's own
// bytes and are, individually, exactly as untrustworthy as everything else
// in a ZIP's metadata.
// zipBoundOutcome is what this bound concluded. It replaces the pair of
// bools PR9R11 introduced, because the question turned out to have THREE
// answers, not two, and two bools describe eight states of which five are
// meaningless -- the sort of shape that invites the next round to find the
// state nobody enumerated (PR9R12, inspect_handlers.go:912).
//
// The refusal is identical in every non-clean case; only the reason differs,
// and the reason is the whole point: "this directory is bigger than we will
// parse" blames the upload's size, "these bytes are not a directory" blames
// its structure, and "we could not read it" blames this service. Reporting
// any of the three as another is a false statement about a file the client
// may well be able to fix.
type zipBoundOutcome int

const (
	// zipBoundOK: the directory was walked to its real end, under ceiling.
	zipBoundOK zipBoundOutcome = iota
	// zipBoundExceedsCeiling: a real ceiling this service chose was crossed
	// (record count, or a window we declined to grow) -> work_ceiling_bytes.
	zipBoundExceedsCeiling
	// zipBoundMalformed: we READ directory bytes and they are not a
	// directory -- a record signature that is not one. A tiny corrupt
	// archive crosses no ceiling at all (PR9R12) -> malformed_input.
	//
	// The line between this and zipBoundExceedsCeiling is "did we conclude
	// from work we actually did, or refuse to do the work". Every EOCD-level
	// inconsistency above is decided BEFORE any directory byte is read, so
	// it stays a ceiling: reporting those as malformed would let an archive
	// we never parsed be published with COMPLETE coverage, which is the
	// false-completeness class rounds 2, 4 and 5 each closed here. Only a
	// contradiction found inside bytes we did read earns a verdict about the
	// file's structure.
	zipBoundMalformed
	// zipBoundReadFailed: a non-EOF ReadAt error. Nothing was measured, so
	// nothing about the archive was learned (PR9R11) -> read_failed.
	zipBoundReadFailed
)

// exceeds reports whether this outcome refuses the upload. Every outcome
// except zipBoundOK does; the classification only decides WHY.
func (o zipBoundOutcome) exceeds() bool { return o != zipBoundOK }

// readFailed, returned alongside exceeds, is PR9R11 (inspect_handlers.go:871):
// this function refuses in BOTH cases, but the two say opposite things about
// whose fault it is. "The directory is larger than we will parse" is a fact
// about the upload; "we could not read the bytes" is a fact about this
// service, and reporting the second as work_ceiling_bytes claims a ceiling
// was exceeded when nothing was ever measured. The refusal is unchanged --
// only the reason travels now.
func zipCentralDirectoryExceedsBound(r io.ReaderAt, size, eocdOffset int64, dirOffsetField, dirBytes uint32, entriesField uint16) zipBoundOutcome {
	// ZIP64 "look elsewhere" sentinels -- explicit, not merely large
	// numbers. Go's own trigger (reader.go:608) is
	// directoryRecords==0xffff || directorySize==0xffff ||
	// directoryOffset==0xffffffff -- note directorySize is compared against
	// 0xffff, NOT 0xffffffff (round 5's own divergence row #4). The
	// pre-existing 0xFFFFFFFF check for dirBytes is kept alongside it:
	// refusing on EITHER value only ever makes this function MORE cautious
	// than Go, never less, so keeping both is safe.
	if dirBytes == 0xFFFFFFFF || dirBytes == 0xFFFF || dirOffsetField == 0xFFFFFFFF || entriesField == 0xFFFF {
		// ZIP64 is a shape this service declines to follow, not a defect in
		// the archive -- our ceiling, not its structure.
		return zipBoundExceedsCeiling
	}
	// A central directory cannot legitimately be larger than the whole
	// upload -- size is ground truth (the real number of bytes this
	// handler received), not a second archive-controlled number, so this
	// comparison is NOT round 2's mistake repeated. It exists purely to
	// catch a lying dirBytes cheaply; round 4's own bypass sets dirBytes to
	// a small, plausible-looking value specifically to slip past checks
	// like this one, so this alone does not, and is not meant to, close
	// round 4 -- the walk below does that.
	if uint64(dirBytes) > uint64(size) {
		// A directory larger than the whole upload cannot exist, and that is
		// PROVEN by bytes already read (the EOCD) against ground truth (the
		// real byte count) -- no legal archive has this shape, and raising
		// any ceiling cannot make the directory exist. Round 12 classified
		// this as a ceiling on the reasoning that no directory byte had been
		// read yet; round 13 corrected it, and the correction is right: what
		// matters is whether the inconsistency is PROVEN, not which bytes
		// proved it. Coverage is handled separately -- see the dispatcher,
		// which marks the tracker so a refusal never publishes as complete.
		return zipBoundMalformed
	}

	// Round 5's own fix (divergence row #1): mirror archive/zip's own
	// baseOffset formula EXACTLY (reader.go:624,
	// baseOffset = directoryEndOffset - directorySize - directoryOffset),
	// and refuse whenever it is anything other than zero -- see this
	// function's own package-level doc comment for why refusing here,
	// rather than replicating Go's own content-dependent fallback
	// (reader.go:636-641), is both safe and sufficient. Every legitimate
	// office/epub upload has baseOffset==0; a nonzero value means this
	// upload either carries a leading prefix (a JAR-style script, or an
	// adversarial one) or an EOCD whose own fields are inconsistent with
	// where this function located it, and "honest full dirBytes with a
	// directoryOffset field pointing only at the directory's OWN final
	// records" (round 5's exact reviewer scenario) always produces a
	// sharply nonzero baseOffset, caught here before any walk begins.
	baseOffset := eocdOffset - int64(dirBytes) - int64(dirOffsetField)
	if baseOffset != 0 {
		// Deliberately NOT malformed, and the distinction is not the one
		// round 12 drew: a nonzero baseOffset is a shape archive/zip
		// TOLERATES (a self-extracting archive with a prefix is legal), so
		// it is not proof of anything being broken -- it is a legal shape
		// this service declines to follow. Declining is a ceiling.
		return zipBoundExceedsCeiling
	}

	// directoryOffset is now provably <= eocdOffset given baseOffset==0
	// above (dirBytes >= 0, so dirOffsetField = eocdOffset-dirBytes <=
	// eocdOffset) -- this check is kept anyway, as defense in depth against
	// a future change to the baseOffset check above, not because it can
	// currently trigger.
	directoryOffset := int64(dirOffsetField)
	if directoryOffset > eocdOffset {
		// The directory claims to start after its own terminator --
		// structurally impossible. Fail closed, and again before reading any
		// directory content: ceiling.
		return zipBoundExceedsCeiling
	}
	if directoryOffset == eocdOffset {
		return zipBoundOK // Zero real bytes of directory content: an empty archive.
	}

	windowLen := eocdOffset - directoryOffset
	if windowLen > inspectMaxZipDirBytes {
		// This is the ENTIRE point: read at most a constant WE chose,
		// never however many bytes the real gap (itself derived from an
		// untrusted offset) happens to be. A directory bigger than this
		// window can never be fully verified, and "cannot be fully
		// verified" is exceeds=true below, not exceeds=false.
		windowLen = inspectMaxZipDirBytes
	}
	window := make([]byte, windowLen)
	n, rerr := r.ReadAt(window, directoryOffset)
	if rerr != nil && rerr != io.EOF {
		// Still a refusal (nothing here was verified), but an I/O one.
		return zipBoundReadFailed
	}
	window = window[:n]

	pos := int64(0)
	count := 0
	for {
		if directoryOffset+pos > eocdOffset {
			// PR9R14 (inspect_handlers.go:968): the previous record's own
			// declared name/extra/comment lengths carried the walk PAST the
			// located EOCD -- a record claiming to extend beyond the
			// directory that contains it. Every byte needed to see this was
			// already read, and no ceiling increase can repair it, so this
			// is a verdict about the archive, not about our budget.
			//
			// An earlier round noticed this overshoot and reasoned it was
			// harmless because pos only increases, so the walk could never
			// land on eocdOffset again and would refuse anyway. True, and it
			// refused for the wrong stated reason -- which is precisely the
			// distinction rounds 11 through 13 spent their time on.
			return zipBoundMalformed
		}
		if directoryOffset+pos == eocdOffset {
			// The walk reached the REAL (located, not declared) end of the
			// directory with the record count staying under the ceiling
			// the whole way -- a legitimate, fully-verified directory.
			return zipBoundOK
		}
		if pos+zipCentralDirMinRecordLen > int64(len(window)) {
			// The window ran out before the walk reached eocdOffset. This
			// function does not know what the remaining (unread) bytes
			// contain -- possibly thousands more real records, exactly
			// like round 4's own reviewer scenario. Not knowing means not
			// letting it through. Our window, our ceiling.
			return zipBoundExceedsCeiling
		}
		if window[pos] != 'P' || window[pos+1] != 'K' || window[pos+2] != 0x01 || window[pos+3] != 0x02 {
			// Not a real central-directory record where the walk expected
			// one -- directoryOffset (or a prior record's declared length)
			// is inconsistent with the actual bytes present. A tiny corrupt
			// archive crosses no ceiling at all (PR9R12).
			return zipBoundMalformed
		}
		nameLen := int64(binary.LittleEndian.Uint16(window[pos+28 : pos+30]))
		extraLen := int64(binary.LittleEndian.Uint16(window[pos+30 : pos+32]))
		commentLen := int64(binary.LittleEndian.Uint16(window[pos+32 : pos+34]))
		pos += int64(zipCentralDirMinRecordLen) + nameLen + extraLen + commentLen
		count++
		if count > inspectMaxZipDirEntries {
			return zipBoundExceedsCeiling
		}
	}
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

// zipReadFailedFamilyResult is the FamilyResult reached when
// detectInspectFormat observes -- via docsniff.go's own readerAtRecorder,
// not by guessing from an error's shape -- that the underlying io.ReaderAt
// itself failed while convert.SniffContainer was reading a PK-prefixed
// upload's EOCD or central directory (PR9R8-F3-SNIFF-FAILURE-OBSERVABLE):
// every container-domain code becomes an honest unknown/read_failed, the
// SAME closed-vocabulary reason a unit test could already produce before
// this fix but the production sniff path never actually reached. NOT a
// silent fold into format_unrecognized (this handler DID recognize the
// PK\x03\x04 prefix; it simply could not finish reading what came after
// it), and never a 500.
func zipReadFailedFamilyResult() inspect.FamilyResult {
	var res inspect.FamilyResult
	for _, code := range []inspect.Code{inspect.CodeOOXMLMacro, inspect.CodeZipBombShape, inspect.CodeExternalReference} {
		res.Checks = append(res.Checks, inspect.Check{Code: code, Status: inspect.StatusUnknown, Reason: inspect.ReasonReadFailed})
	}
	return res
}

// sniffReadFailedFamilyResult is the FamilyResult for an upload whose first
// read failed and which no detector recognized (PR9R25,
// inspect_handlers.go:509). Unlike zipReadFailedFamilyResult below it claims
// EVERY code except CodeExtensionMismatch -- which
// extensionMismatchFamilyResult owns and answers with the same reason -- because
// the failure happened before any format was established: there is no family
// this upload is known to belong to, so no code can be called not_applicable.
func sniffReadFailedFamilyResult() inspect.FamilyResult {
	var res inspect.FamilyResult
	for _, code := range inspect.AllCodes() {
		if code == inspect.CodeExtensionMismatch {
			continue
		}
		res.Checks = append(res.Checks, inspect.Check{Code: code, Status: inspect.StatusUnknown, Reason: inspect.ReasonReadFailed})
	}
	return res
}

// zipMalformedFamilyResult is the FamilyResult reached when
// convert.SniffContainer's own zip.NewReader call fails STRUCTURALLY
// (errors.Is(cerr, convert.ErrNotAZip)) -- docsniff.go's own
// readerAtRecorder observed every ReadAt it made succeed, so the failure
// describes THIS UPLOAD's own bytes, not this handler's ability to read
// them (PR9R8-F4-STRUCTURAL-VS-IO). Every container-domain code becomes an
// honest unknown/malformed_input, distinct from zipReadFailedFamilyResult
// above precisely so a client can tell "this service could not read your
// upload" apart from "your upload's own central directory does not parse".
func zipMalformedFamilyResult() inspect.FamilyResult {
	var res inspect.FamilyResult
	for _, code := range []inspect.Code{inspect.CodeOOXMLMacro, inspect.CodeZipBombShape, inspect.CodeExternalReference} {
		res.Checks = append(res.Checks, inspect.Check{Code: code, Status: inspect.StatusUnknown, Reason: inspect.ReasonMalformedInput})
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
//
// tr itself is ALSO passed to every analyzer now (quick/260811-r3s), on top
// of maxBytes: it is the shared receptacle a scanner deep inside any family
// analyzer calls tr.MarkCeilingHit() on the instant it hits its OWN local
// resource ceiling, so inspect.BuildReport's Coverage claim can never come
// back Complete:true for an operation where some scanner gave up early --
// regardless of whether the specific Check/characteristic Reason wiring
// this file never sees got it right.
//
// dispatchInspectFamilies is the sole owner of tr's shared byte/time budget
// FOR THE ANALYSIS PHASE (PR9R9 finding #3, media.go:337): the single
// tr.Consume(maxBytes) call below is the only place any family analyzer's
// bytes are charged -- no family analyzer (pdf.go, container.go, image.go,
// and, as of that fix, media.go too) ever calls tr.Consume itself.
//
// Since Phase 59 (D-12) it is no longer the only Consume call in the inspect
// path: detectInspectFormat charges its own content-sniff window before this
// function is reached. That is not a return of the PR9R9 defect and the
// difference is worth stating precisely, because the two look alike. PR9R9
// was DOUBLE-COUNTING -- media.go charged the very same bytes this function
// had already charged, so one read had two owners. Detection's window is a
// SEPARATE read, charged once by the code that performs it; this function
// then charges tr.Remaining(), so nothing is counted twice and the total
// stays inside the configured ceiling. The invariant that actually matters
// -- one charge per read -- is intact; only the sentence "the only Consume
// call" stopped being true, and leaving it standing would have been the kind
// of comment that quietly becomes a lie. This was already
// true for three of the four families before this fix; media.go's own
// findMoov was the one outlier, charging tr a SECOND time (via a now-
// deleted consumeActual helper) with the bytes it actually read, on top of
// this function's own unconditional full-allowance charge -- a resource
// with two owners, which is what let a small, fully-parsed, valid MP4
// report a lying `coverage.complete=false reason="work_ceiling_bytes"` in
// production. Naming this function as the single owner is not merely
// "delete the other charge": every family analyzer's own local iteration/
// box/entry ceilings still report through tr.MarkCeilingHit() (a SEPARATE
// receptacle from Consume's own byte/time accounting, budget.go), so an
// analyzer that gives up early still marks the shared Tracker exhausted --
// it simply never calls Consume to do so.
func dispatchInspectFamilies(detected string, sniffed *convert.ContainerResult, sniffedZip *zip.Reader, file multipart.File, size int64, tr *inspect.Tracker) []inspect.FamilyResult {
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
	case detected == inspectSniffReadFailedFormat:
		// Nothing was parsed and nothing CAN be: the failure predates any
		// format claim, so every code in the vocabulary is unknown for the
		// same operational reason. tr.Consume is deliberately skipped, exactly
		// as in the inspectZipTooLargeFormat case above -- charging a byte
		// budget for a parse that never happened would be dishonest
		// accounting.
		return append(families, sniffReadFailedFamilyResult())
	case detected == inspectZipReadFailedFormat:
		// PR9R8-F3: unlike inspectZipTooLargeFormat above, SniffContainer
		// DID run (and DID spend the read work this attempt cost) before
		// failing -- tr.Consume below still applies, same as every other
		// case that actually dispatched an analyzer.
		families = append(families, zipReadFailedFamilyResult())
	case detected == inspectZipMalformedFormat:
		// PR9R8-F4: same reasoning as inspectZipReadFailedFormat above --
		// SniffContainer ran and read real bytes before concluding the
		// structure itself does not parse.
		//
		// PR9R13: the checks say malformed_input, which is the honest reason
		// -- and the report must ALSO say the coverage is incomplete, which
		// is a separate fact: this archive's contents were never parsed. The
		// two are decoupled deliberately, because collapsing them is what
		// made round 12's first attempt publish an unparsed archive as fully
		// inspected. Marking the tracker is how a refusal reaches
		// buildCoverage without borrowing a ceiling reason for a check that
		// is not about a ceiling (round 3's exhaustion sink, reused exactly
		// as intended).
		tr.MarkCeilingHit()
		families = append(families, zipMalformedFamilyResult())
	case detected == "pdf":
		families = append(families, inspect.AnalyzePDF(file, size, maxBytes, tr))
	case inspectContainerFormats[detected]:
		// Reuse detection's own sniff instead of walking the same
		// attacker-controlled central directory a second time (codex PR #9
		// finding 5). sniffed is non-nil for every format in
		// inspectContainerFormats, because that is precisely the set
		// detectInspectFormat can only have reached BY sniffing; the nil
		// branch stays as a correctness fallback rather than an assumption.
		if sniffed != nil {
			families = append(families, inspect.AnalyzeContainerWith(*sniffed, sniffedZip, file, size, maxBytes, inspectZipBombFindingThreshold, tr))
		} else {
			families = append(families, inspect.AnalyzeContainer(file, size, maxBytes, inspectZipBombFindingThreshold, tr))
		}
	case inspectMediaFormats[detected]:
		families = append(families, inspect.AnalyzeMedia(detected, file, size, maxBytes, tr))
	case inspectImageFormats[detected]:
		families = append(families, inspect.AnalyzeImage(detected, file, size, maxBytes, inspectPixelBombFindingThreshold, tr))
	case detected == inspectOLECFBFormat:
		// convert.ClassifyCFB takes no byte allowance of its own -- it is
		// shared with the conversion path, where there is none -- and its
		// bounded directory walk can read up to ~16 MiB (cfbMaxDirSectors x a
		// 4096-byte sector), well past this operation's 8 MiB default ceiling
		// that tr.Consume below then reports as enforced. PR9R23 named this
		// class in pdf.go and image.go; this was the largest instance of it and
		// was not in the finding. Bounding the reader from the outside keeps
		// ClassifyCFB's own signature honest for both of its callers.
		budgeted := inspect.NewBudgetedReaderAt(file, maxBytes)
		class := convert.ClassifyCFB(budgeted, size)
		families = append(families, cfbFamilyResult(class, budgeted.Exceeded(), tr))
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
//
// budgetExceeded is the one case where that last sentence does not hold: the
// walk was cut short by this operation's byte ceiling, so the file was NOT
// classified -- it merely was not finished. Publishing CFBUnclassified as a
// Finding there would state a structural conclusion drawn from bytes nobody
// read, which is the whole class PR9R23 belongs to; all three codes report
// unknown/work_ceiling_bytes instead, and the tracker is marked so the
// report's coverage says partial.
func cfbFamilyResult(class convert.CFBClass, budgetExceeded bool, tr *inspect.Tracker) inspect.FamilyResult {
	if budgetExceeded {
		tr.MarkCeilingHit()
		res := inspect.FamilyResult{}
		for _, code := range []inspect.Code{inspect.CodeOLECFBLegacy, inspect.CodeOLECFBEncrypted, inspect.CodeOLECFBUnclassified} {
			res.Checks = append(res.Checks, inspect.Check{Code: code, Status: inspect.StatusUnknown, Reason: inspect.ReasonWorkCeilingBytes})
		}
		return res
	}

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
	case detected == inspectSniffReadFailedFormat:
		// PR9R25: the first read of this upload failed, so there is no
		// detected format to compare a declared extension against -- and
		// saying "not applicable" would imply we looked and found the question
		// irrelevant, when in fact we never got to ask it.
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusUnknown, Reason: inspect.ReasonReadFailed},
		}}
	case detected == inspectZipReadFailedFormat:
		// PR9R8-F3: same reasoning as inspectZipTooLargeFormat above --
		// detected names a "could not finish reading" sentinel, not a real
		// format, so there is nothing honest to compare declaredFormat
		// against; unknown/read_failed says so explicitly rather than
		// falling through to the generic format_unrecognized case below.
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusUnknown, Reason: inspect.ReasonReadFailed},
		}}
	case detected == inspectZipMalformedFormat:
		// PR9R8-F4: same reasoning, for the structural (not operational)
		// sibling sentinel.
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusUnknown, Reason: inspect.ReasonMalformedInput},
		}}
	case detected == "":
		// PR9R27 (inspect_handlers.go:1393): an UNRECOGNIZED upload is not the
		// same as an inapplicable check. not_applicable is the claim "there was
		// nothing here to look for", and making it requires knowing what the
		// file IS -- which is exactly the reasoning buildChecks already applies
		// to every OTHER code when format is empty (codex finding 6 /
		// T-SLE-06). This code escaped that rule only because it is claimed
		// explicitly here rather than left to the fallback, so it alone said
		// "irrelevant" while the rest of the vocabulary said "unknown".
		//
		// The client-visible difference is the point: not_applicable invites a
		// reader to conclude the extension is fine, when in truth nothing was
		// ever compared against it.
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusUnknown, Reason: inspect.ReasonFormatUnrecognized},
		}}
	case declaredFormat == "":
		// A filename with no extension at all is the genuinely inapplicable
		// case, and it stays that way: there is no declared extension for a
		// mismatch to exist between, whatever the detected format turned out
		// to be. Nothing is unknown here -- both sides were established, and
		// one of them is absent.
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
