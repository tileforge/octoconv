package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/presets"
	"github.com/apaderin/octoconv/internal/storage"
)

const (
	formFieldFile            = "file"
	formFieldTarget          = "target"
	formFieldCallbackURL     = "callback_url"
	formFieldProgressWebhook = "progress_webhook"
	formFieldOpts            = "opts"
	formFieldPreset          = "preset"
	formFieldOperation       = "operation"
	// operationConv is kept as a local alias of convert.OperationConvert so
	// existing call sites in this file (the operation-omitted default and
	// the Create call below) do not need touching beyond this one line
	// (57-04): the literal "convert" now lives in exactly one place,
	// internal/convert/convert.go.
	operationConv = convert.OperationConvert
	// outputRolePrimary is the job_outputs.role value that selects the
	// output handleGetJob presigns into "download_url" (ROLE-02, 57-06):
	// the primary result of the job, chosen by role, never by ordinal or
	// position in the []jobs.Output slice returned by Repo.Outputs. Aliased
	// from jobs.OutputRolePrimary (codex PR #7 finding #1) so the literal
	// "primary" is declared exactly once in Go, shared with
	// internal/worker's terminal webhook selection.
	outputRolePrimary = jobs.OutputRolePrimary
	// maxOptsBytes bounds the opts JSON field before it is even parsed
	// (T-14-02): a conservative 4 KiB comfortably fits the closed DocOpts
	// schema while bounding the DoS surface of an oversized field.
	maxOptsBytes = 4096
	// multipartInMemoryBudget caps how many bytes of a multipart upload
	// ParseMultipartForm buffers in the process heap; larger file parts spill
	// to temp files on disk (Go's documented behavior). It is deliberately
	// DECOUPLED from maxUploadByte (CR-01, Phase 35): the total request size is
	// already bounded pre-parse by http.MaxBytesReader, so passing the full
	// upload ceiling (2 GiB since D-07) as the in-memory budget would let one
	// large-video upload buffer up to 2 GiB in RAM -- linear in concurrent
	// uploads, a real memory-exhaustion vector. 32 MiB matches Go's own
	// net/http defaultMaxMemory.
	multipartInMemoryBudget = 32 << 20
	// maxPresetNameBytes bounds the client-supplied preset name before any DB
	// lookup (T-18-09) -- an opaque-string DoS guard, request-independent of
	// preset existence, so a 400 here leaks nothing about the presets table.
	maxPresetNameBytes = 128
	// errUnknownPreset is the SINGLE non-leaking 422 text for every preset
	// resolution miss -- nonexistent, inactive, or cross-client (D-03). No
	// other branch in handleCreateJob may return a distinguishable message
	// for a preset lookup failure.
	errUnknownPreset = "unknown or inactive preset"
	// quotaConcurrencyRetryAfterSeconds is the fixed Retry-After hint for a
	// concurrency-limit 429 (QUOT-03, D-11). A concurrency slot frees only
	// when one of this client's in-flight jobs reaches a terminal state --
	// single-digit seconds for the image class, minutes for the av class --
	// so a fixed hint must be long enough not to invite a hot retry loop and
	// short enough not to stall a fast client; 30s sits an order of
	// magnitude above the fast case and well below the slow one (measured,
	// not copied, per this project's convention). A daily rejection does
	// NOT use this constant -- a constant would be wrong by hours there;
	// see secondsUntilUTCMidnight below.
	quotaConcurrencyRetryAfterSeconds = 30
)

// handleHealth probes Postgres, Redis, and S3/MinIO reachability under a
// shared short timeout and reports per-dependency status (OBS-02, D-16/D-17).
// It is read-only: it only pings, never writes.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	result := map[string]string{}
	healthy := true

	if err := s.health.Postgres.Ping(ctx); err != nil {
		result["postgres"] = "unreachable"
		healthy = false
	} else {
		result["postgres"] = "ok"
	}

	if err := s.health.Redis.Ping(ctx); err != nil {
		result["redis"] = "unreachable"
		healthy = false
	} else {
		result["redis"] = "ok"
	}

	if err := s.health.S3.Ping(ctx); err != nil {
		result["s3"] = "unreachable"
		healthy = false
	} else {
		result["s3"] = "ok"
	}

	status := http.StatusOK
	result["status"] = "ok"
	if !healthy {
		status = http.StatusServiceUnavailable
		result["status"] = "degraded"
	}
	writeJSON(w, status, result)
}

// handleCreateJob accepts a multipart upload (fields: file, target), validates
// the conversion pair before touching storage, uploads the input to S3, records
// the job in Postgres (queued) and enqueues the conversion task.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Cap the request body to reject oversized uploads before buffering them.
	// The total-size ceiling is enforced here by MaxBytesReader; the separate,
	// smaller multipartInMemoryBudget bounds heap use during parsing (CR-01).
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadByte)
	if err := r.ParseMultipartForm(multipartInMemoryBudget); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge, "file exceeds size limit")
			return
		}
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid multipart form")
		return
	}

	// preset, target, and opts are read up front so the D-01 mutual-
	// exclusivity gate can run before any other field validation -- this is a
	// pure request-shape check, independent of client/content/DB state.
	presetName := r.FormValue(formFieldPreset)
	rawTarget := r.FormValue(formFieldTarget)
	rawOpts := r.FormValue(formFieldOpts)
	usingPreset := presetName != ""

	// D-04: normalize the operation field to operationConv ONE line after
	// FormValue, before any branching whatsoever. Below this point, "the
	// client sent no operation field" and "the client sent operation=convert"
	// are physically indistinguishable -- there is nowhere for the API
	// default and the DB default to diverge, because there is only ever one
	// value from here on (Pitfall 13). A three-valued scheme ("absent" vs
	// "empty" vs "explicit") was deliberately rejected: it would require a
	// third 422 branch to reject an empty-but-present field for no operator
	// benefit, and it is exactly the kind of extra branch this normalization
	// exists to make impossible. No strings.TrimSpace and no lower-casing --
	// operation is compared exactly, the same discipline as an engine class.
	operation := r.FormValue(formFieldOperation)
	if operation == "" {
		operation = operationConv
	}

	if usingPreset && (rawTarget != "" || (rawOpts != "" && rawOpts != "{}")) {
		// D-01: preset and an explicit target/opts are mutually exclusive --
		// no merge, no precedence guessing.
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "specify either preset or target/opts, not both")
		return
	}
	if usingPreset && len(presetName) > maxPresetNameBytes {
		// Length is request-independent of DB state, so a 400 here leaks
		// nothing about preset existence (T-18-09).
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid preset name")
		return
	}

	target := convert.NormalizeFormat(rawTarget)
	if !usingPreset && target == "" {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "missing target format")
		return
	}

	file, header, err := r.FormFile(formFieldFile)
	if err != nil {
		writeError(w, http.StatusBadRequest, errcode.MissingFile, "missing file")
		return
	}
	defer file.Close()

	filename := path.Base(header.Filename)
	// Declared source, from the (attacker-controllable) filename extension.
	// Still needed below for the D-01 honesty comparison against the
	// magic-byte-detected format — it is no longer trusted on its own.
	source := convert.NormalizeFormat(strings.TrimPrefix(path.Ext(filename), "."))
	if source == "" {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "cannot determine source format from filename")
		return
	}

	// Middleware guarantees a resolved client is present before this handler
	// runs. Resolved BEFORE content detection because a mismatch/unrecognized
	// rejection below must log the client id (D-08).
	client, _ := auth.ClientFromContext(ctx)

	// Preset resolution (D-07): resolved AFTER client auth but BEFORE content
	// detection / EngineFor, so a preset supplies target_format (and opts,
	// below) exactly as if the client had sent them directly. presetID/
	// presetVer are stashed for the pre-Create active re-check (Pitfall 8);
	// presetOptsMap feeds the SAME opts validation pipeline as ad-hoc opts
	// (D-06, below) -- no bypass branch.
	var presetOptsMap map[string]any
	var presetNameProv string
	var presetVerProv int
	var presetID uuid.UUID
	var presetVer int
	if usingPreset {
		p, err := s.presets.Resolve(ctx, client.ID, presetName)
		if err != nil {
			if errors.Is(err, presets.ErrNotFound) {
				// D-03: nonexistent, inactive, and cross-client all collapse
				// to ErrNotFound inside Resolve -- no ownership branch here.
				writeError(w, http.StatusUnprocessableEntity, errcode.UnknownPreset, errUnknownPreset)
				return
			}
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to resolve preset")
			return
		}
		target = convert.NormalizeFormat(p.TargetFormat)
		presetOptsMap = p.Options
		presetNameProv = p.Name
		presetVerProv = p.Version
		presetID = p.ID
		presetVer = p.Version
	}

	// Detect the actual content format by magic bytes BEFORE anything else
	// touches storage or Postgres (D-01/D-02/D-05). rest re-stitches the
	// peeked prefix onto the remaining stream so the full file still reaches
	// s3.Upload below.
	detected, rest, err := convert.Sniff(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid multipart form")
		return
	}
	if detected == "" {
		// Sniff's prefix table doesn't disambiguate ZIP-based office formats
		// (docx/xlsx/pptx/odt/ods/odp share PK\x03\x04 with each other and a
		// bare .zip) — structurally inspect the ZIP central directory instead
		// (D-01). Read from the original file (io.ReaderAt), NOT rest (an
		// io.MultiReader that does not implement ReaderAt); ReadAt never
		// disturbs Sniff's sequential cursor, so rest remains valid below.
		var prefix [4]byte
		if n, _ := file.ReadAt(prefix[:], 0); n == 4 && bytes.Equal(prefix[:], []byte{'P', 'K', 3, 4}) {
			cr, cerr := convert.SniffContainer(file, header.Size)
			if cerr == nil && cr.Format != "" && !cr.DuplicateRootPart {
				detected = cr.Format
				// D-02/D-04: reject a zip-bomb-shaped declared uncompressed
				// total before any storage write or decompression.
				if cr.TotalUncompressed > s.maxDocumentUncompressedBytes {
					log.Printf("content validation rejected: client_id=%s filename=%q reason=zip_bomb declared_uncompressed=%d limit=%d", client.ID, filename, cr.TotalUncompressed, s.maxDocumentUncompressedBytes)
					writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
						"declared uncompressed size exceeds configured limit")
					return
				}
				// D-05: unconditional macro rejection, no operator opt-out —
				// macro code never executes as part of producing a PDF output.
				if cr.HasMacro {
					log.Printf("content validation rejected: client_id=%s filename=%q reason=macro_detected", client.ID, filename)
					writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
						"macro-carrying documents are not accepted")
					return
				}
			}
			// A DuplicateRootPart result, ErrNotAZip, or Format=="" leaves
			// detected empty intentionally — fail closed to the unrecognized-
			// content rejection below rather than accept an ambiguous archive.
		}
	}
	if detected == "" && source == "html" && convert.LooksLikeHTML(file, header.Size) {
		// D-07/HTML-02: HTML has no magic bytes for Sniff's table to match,
		// so content detection here is gated on the (still-untrusted)
		// declared source already claiming "html" (post NormalizeFormat's
		// htm->html alias) PLUS a fail-closed structural content check
		// (UTF-8, no NUL, doctype/html marker). A .html-named file whose
		// content fails LooksLikeHTML leaves detected=="" and falls through
		// to the generic unrecognized-content 422 below (fail-closed,
		// before any storage write) -- never silently accepted just
		// because the extension claims html.
		detected = "html"
	}
	if detected == "" && source == "latex" && convert.LooksLikeLaTeX(file, header.Size) {
		// D-02 (48-CONTEXT.md), mirrors the source=="html" LooksLikeHTML
		// branch above verbatim: latex has no magic bytes for Sniff's table
		// to match, so content detection here is gated on the (still-
		// untrusted) declared source already claiming "latex" PLUS a
		// fail-closed structural content check (UTF-8, no NUL, a
		// \documentclass/\begin{/backslash-command marker,
		// markupsniff.go). A .tex-named file whose content fails
		// LooksLikeLaTeX leaves detected=="" and falls through to the
		// generic unrecognized-content 422 below (fail-closed, before any
		// storage write) -- never silently accepted just because the
		// extension claims latex.
		detected = "latex"
	}
	if detected == "" && (source == "md" || source == "rst" || source == "org") && convert.LooksLikeText(file, header.Size) {
		// D-02 (48-CONTEXT.md): md/rst/org are mutually-interchangeable
		// plaintext with no format-specific magic bytes to disambiguate
		// among themselves (LooksLikeText's own documented limitation,
		// markupsniff.go) -- content detection here therefore trusts the
		// client's DECLARED source among the text family once the
		// fail-closed plaintext gate (UTF-8, no NUL, no binary-control
		// bytes) passes, mirroring the source=="html" LooksLikeHTML branch's
		// "declared source PLUS a fail-closed structural content check"
		// discipline. A body whose content fails LooksLikeText (binary/NUL-
		// contaminated) leaves detected=="" and falls through to the
		// generic unrecognized-content 422 below -- never silently accepted
		// just because the extension claims md/rst/org.
		detected = source
	}
	if detected == "" && convert.IsOLECFB(file) {
		// D-06 (22-cfb-classification): legacy binary Office (.doc/.xls/.ppt)
		// and password-protected OOXML ("Agile Encryption") share the
		// identical 8-byte OLE-CFB header, so IsOLECFB stays the cheap
		// pre-filter (D-01) — but ClassifyCFB (internal/convert/cfb.go, Plan
		// 22-01) now reads the directory entry names to tell them apart and
		// give each case its own accurate 422. CFBUnknown (unrecognized/
		// corrupt CFB) falls through to the original combined message
		// unchanged — fail-closed compat (Pitfall 11): it must never be
		// treated as anything other than a rejection.
		switch convert.ClassifyCFB(file, header.Size) {
		case convert.CFBEncrypted:
			log.Printf("content validation rejected: client_id=%s filename=%q reason=encrypted_document", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
				"password-protected Office file is not supported; remove the password and re-upload")
			return
		case convert.CFBLegacy:
			log.Printf("content validation rejected: client_id=%s filename=%q reason=legacy_document", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
				"legacy binary Office format (.doc/.xls/.ppt) is not supported; convert to docx/xlsx/pptx")
			return
		default: // convert.CFBUnknown
			log.Printf("content validation rejected: client_id=%s filename=%q reason=legacy_or_encrypted_document", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
				"legacy binary or password-protected Office format is not supported; convert to docx/xlsx/pptx or remove the password")
			return
		}
	}
	if detected == "" {
		// D-08 (Phase 35): mkv/webm EBML-container detection, placed after the
		// OLE-CFB check and before SniffAudio below. mp4/mov/avi are ALREADY
		// caught by Sniff()'s fixed-12-byte-window signatures table earlier in
		// this chain (matchMP4/matchMOV/matchAVI, avsniff.go) -- SniffVideo
		// exists here only to close the mkv/webm gap that has had zero
		// non-test callers since Phase 34 (WR-02: registering AVConverter
		// without wiring this would ship an engine for formats the service
		// cannot recognize). Placed before SniffAudio, not after, because
		// SniffAudio's mp3PeekLen=512KiB peek is the single most expensive
		// buffer in this chain -- ordering video first keeps that cost paid
		// only when video detection has already declined.
		//
		// CRITICAL deviation from SniffAudio's exact reassignment shape
		// (auto-fixed, Rule 1): SniffAudio only reassigns `rest` when it gets
		// a non-empty match because it is the LAST detector in this chain --
		// on a miss, `detected` stays "" and the handler returns the
		// unrecognized-content 422 immediately after, so a stale `rest` is
		// never read again. SniffVideo is NOT last (SniffAudio still runs
		// after it): SniffVideo's io.ReadFull(rest, ...) call ALWAYS drains
		// up to avPeekLen bytes from the underlying stream `rest` refers to,
		// whether or not it finds a match. If `rest` were only reassigned on
		// a match, a miss would silently leave `rest` pointing at a reader
		// that has already lost its first bytes to this drain, and
		// SniffAudio's subsequent read (or, on a genuine unrecognized upload
		// with no further sniffer, s.storage.Upload) would receive a
		// truncated stream (the exact T-31-02 class of bug). `rest` is
		// therefore ALWAYS rebound to SniffVideo's returned reader on
		// verr == nil, regardless of match; only `detected` is conditional.
		if videoDetected, videoRest, verr := convert.SniffVideo(rest); verr == nil {
			rest = videoRest
			if videoDetected != "" {
				detected = videoDetected
			}
		}
	}
	if detected == "" {
		// AUD-05: mp3/wav/m4a/ogg content detection, placed LAST (after the
		// OLE-CFB check, before the final fail-closed 422 below) because
		// mp3PeekLen=512KiB is the most expensive buffer of the whole chain --
		// ordering it last means a non-audio upload never pays for it.
		// CRITICAL: chained off `rest` (Sniff's re-stitched full-file-from-
		// byte-0 reader), NOT `file` -- file's sequential cursor has already
		// advanced past Sniff's first sniffLen=12 bytes, and SniffAudio
		// re-stitches its OWN rest from whatever reader it's given. Passing
		// `file` here instead would silently drop the first 12 bytes of every
		// audio upload at s.storage.Upload below (T-31-02). No source gate --
		// SniffAudio self-identifies from magic bytes, matching the
		// unconditional ZIP/OLE-CFB detection shape above.
		if audioDetected, audioRest, aerr := convert.SniffAudio(rest); aerr == nil && audioDetected != "" {
			detected = audioDetected
			rest = audioRest
		}
	}
	if detected == "" {
		// D-02: no known signature matches — reject rather than let the
		// (untrustworthy) extension win. D-08: scoped internal/* logging
		// exception for this rejection, tagged with the resolved client.
		log.Printf("content validation rejected: client_id=%s filename=%q reason=unrecognized_content", client.ID, filename)
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
			"unrecognized file content for "+filename)
		return
	}
	if detected != source {
		// D-01/D-04: declared extension must be honest about the actual
		// content; no auto-correction to the detected format.
		log.Printf("content validation rejected: client_id=%s filename=%q reason=mismatch declared=%s detected=%s", client.ID, filename, source, detected)
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
			"declared format "+source+" does not match detected content "+detected)
		return
	}

	// Two fail-closed 422 gates, in this order, both strictly before
	// s.storage.Upload and s.repo.Create below (57-04, T-57-13): a garbage
	// operation or an unregistered triple must never spend S3 bytes or a
	// client's daily quota. These are two DIFFERENT failure classes with
	// different client actions (D-03) -- "this operation does not exist at
	// all" vs "this operation exists but not for this pair" -- so splitting
	// them into two error codes follows the existing per-failure-class
	// granularity, it does not violate it.
	//
	// Gate 1: is the operation itself known to this build at all? The
	// allowlist is the REGISTRY (convert.Default.IsKnownOperation), not the
	// jobs.operation DB CHECK constraint -- a value legal in the schema but
	// with no registered triple (e.g. sanitize before Phase 60, or
	// extract/archive/inspect/render) is rejected exactly like any other
	// garbage value (T-57-14). The message names the rejected value and
	// points at GET /v1/formats for the current list -- it does not itself
	// enumerate operations, which stays the responsibility of that
	// authenticated endpoint (T-57-16).
	if !convert.Default.IsKnownOperation(operation) {
		writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedOperation,
			"unsupported operation: "+operation+" -- see GET /v1/formats for supported operations")
		return
	}

	// Gate 2: given a KNOWN operation, is this (source, target) pair
	// registered under it? The DETECTED format (not the extension-derived
	// one) is the source of truth fed into the pair-check (D-05). This
	// message text is unchanged from before this plan -- it is already on
	// the wire and consumers key off it.
	engine, ok := convert.Default.EngineForOp(operation, detected, target)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedConversion,
			"unsupported conversion: "+detected+" -> "+target)
		return
	}

	// D-07 (Phase 35): per-engine post-detection ceiling, checked immediately
	// after the engine class is known and strictly before s.storage.Upload
	// below -- an oversized non-video upload must never reach S3. This is
	// the SECOND tier: s.maxUploadByte (checked pre-parse, above) bounds
	// every request at 2 GiB before the engine class is even knowable;
	// this tier restores image/document/html/audio to their prior 100 MiB
	// effective ceiling so raising the global cap for video does not weaken
	// their DoS posture. s.maxEngineBytes gates known engines only -- an
	// engine absent from the map is never rejected here (the map gates, it
	// does not allowlist).
	if limit, ok := s.maxEngineBytes[engine]; ok && header.Size > limit {
		log.Printf("content validation rejected: client_id=%s filename=%q reason=engine_size_limit engine=%s size=%d limit=%d", client.ID, filename, engine, header.Size, limit)
		writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge,
			"file exceeds size limit for this conversion type")
		return
	}

	// QUOT-02 (ROADMAP SC2b): the pre-upload quota gate. This is the
	// earliest point at which both engine and header.Size are known, and it
	// runs strictly before s.storage.Upload below -- which is what keeps a
	// client already at its cap from repeatedly burning S3 bandwidth and
	// storage on every retry. Limit of the guarantee: PreAdmit is unlocked
	// (internal/jobs/admission.go), so it stops the SEQUENTIAL-retry burn
	// but not a simultaneous burst from a client still below its cap -- that
	// residual is bounded and documented at PreAdmit's own definition, and
	// measured by plan 52-05's concurrent-burst subtest.
	if err := s.repo.PreAdmit(ctx, client.ID, engine, header.Size); err != nil {
		if msg, retryAfter, ok := quotaRejection(err); ok {
			log.Printf("quota rejected: client_id=%s engine=%s reason=%s stage=preupload", client.ID, engine, jobs.QuotaReason(err))
			writeQuotaError(w, msg, retryAfter)
			return
		}
		// A NON-quota error from PreAdmit must NOT fail the request open and
		// must NOT 500 the request either: log it and continue, because
		// Create's authoritative admitLocked check still runs below and is
		// the correctness mechanism for this request. Do not "fix" this
		// into a 500 -- that would turn a best-effort optimization failure
		// into an outage.
		log.Printf("quota pre-admit check failed (continuing to authoritative check): client_id=%s engine=%s", client.ID, engine)
	}

	// VALID-03: reject a decompression-bomb-shaped upload (declared pixel
	// dimensions exceeding the configured limit) before any storage write.
	// convert.Dimensions re-stitches its own bounded peek onto rest, so the
	// full original stream still reaches s.storage.Upload below unmodified.
	// HasDimensionLimit scopes this to image formats only — pixel dimensions
	// are not a document concept, so documents skip the block entirely
	// (fixes the confirmed regression where Dimensions() unconditionally
	// 422'd every document upload, RESEARCH.md Pitfall 5).
	if convert.HasDimensionLimit(detected) {
		width, height, dimRest, err := convert.Dimensions(detected, rest)
		if err != nil {
			log.Printf("content validation rejected: client_id=%s filename=%q reason=dimensions_unknown", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
				"cannot determine declared image dimensions for "+filename)
			return
		}
		rest = dimRest
		totalPixels := uint64(width) * uint64(height)
		if totalPixels > s.maxImagePixels {
			log.Printf("content validation rejected: client_id=%s filename=%q reason=dimension_limit width=%d height=%d limit=%d", client.ID, filename, width, height, s.maxImagePixels)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
				"declared image dimensions exceed configured limit")
			return
		}
	}

	// callback_url is optional (per-job, D-02); an empty value leaves the
	// existing polling path unchanged. When present it is SSRF-validated
	// BEFORE writing anything to storage, same discipline as the format pair.
	callbackURL := r.FormValue(formFieldCallbackURL)
	if callbackURL != "" {
		if err := validateCallbackURL(callbackURL); err != nil {
			writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid callback_url")
			return
		}
	}

	// progress_webhook is a top-level opt-in delivery flag (D-03/PUSH-02),
	// NOT part of opts (delivery config is orthogonal to conversion
	// params). It gates delivery to the SAME already-SSRF-validated
	// callback_url above -- no second validateCallbackURL call site (T-42-01).
	// Opting in with no delivery target is a silent no-op, not an error: the
	// worker simply has nothing to POST to (Assumption A4).
	progressWebhook := r.FormValue(formFieldProgressWebhook) == "true"
	if progressWebhook && callbackURL == "" {
		progressWebhook = false
	}

	// opts is optional (D-02); an empty value or the literal "{}" means "no
	// opts" (D-09) and normalizedOpts stays nil, skipping validation
	// entirely. Otherwise: size cap, then engine-keyed syntax parse
	// (ParseDocOpts/ParseHTMLOpts), then engine-keyed applicability
	// (ValidateApplicability/ValidateHTMLApplicability, now that
	// engine/detected/target are known) -- all BEFORE s.storage.Upload below
	// (D-03/D-04). HTMLOpts is a structurally different closed type from
	// DocOpts (page_size/margin_mm/landscape/print_background, not
	// pdf_profile), so the dispatch is engine-keyed rather than a single
	// unconditional call (HTML-03). The API never duplicates
	// internal/convert's validation logic; it only calls it (single
	// validation authority, D-04/D-10).
	//
	// D-06: when a preset was used, rawOpts is re-sourced from the preset's
	// resolved options map (re-marshaled to JSON) INSTEAD of the client's
	// opts form field (which the D-01 mutual-exclusivity gate above already
	// guaranteed is empty). The validators below are completely unaware of
	// this substitution -- stored preset opts flow through the identical
	// fail-closed ParseDocOpts/ParseHTMLOpts + ValidateApplicability path as
	// ad-hoc opts, with no bypass branch. A stored preset whose opts fail
	// current validation fails job creation here, exactly like bad ad-hoc opts.
	var normalizedOpts map[string]any
	if usingPreset {
		if len(presetOptsMap) > 0 {
			presetOptsJSON, err := json.Marshal(presetOptsMap)
			if err != nil {
				writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
				return
			}
			rawOpts = string(presetOptsJSON)
		} else {
			rawOpts = ""
		}
	}
	if rawOpts != "" && rawOpts != "{}" {
		if len(rawOpts) > maxOptsBytes {
			log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_too_large size=%d limit=%d", client.ID, filename, len(rawOpts), maxOptsBytes)
			writeError(w, http.StatusUnprocessableEntity, errcode.PayloadTooLarge, "opts field too large")
			return
		}
		var normalizedRaw []byte
		switch engine {
		case convert.EngineAudio:
			audioOpts, err := convert.ParseAudioOpts([]byte(rawOpts))
			if err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=invalid_opts", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
				return
			}
			if err := convert.ValidateAudioApplicability(engine, detected, target, audioOpts); err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_not_applicable", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
				return
			}
			// D-08: persist the normalized struct, never the raw client
			// bytes -- round-trip through json.Marshal so only the
			// validated enum value ever reaches storage.
			normalizedRaw, err = json.Marshal(audioOpts)
			if err != nil {
				writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
				return
			}
		case convert.EngineAV:
			// AVE-03/AVT-01 (Phase 35): ParseAVOpts and ValidateAVApplicability
			// already exist and were ASVS-audited in Phase 34 (avopts.go:99,
			// :145) -- this branch is registration of that existing closed-
			// allowlist validation into the API's opts-dispatch switch, not
			// new validation logic, mirroring the EngineAudio branch above
			// exactly.
			avOpts, err := convert.ParseAVOpts([]byte(rawOpts))
			if err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=invalid_opts", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
				return
			}
			if err := convert.ValidateAVApplicability(engine, detected, target, avOpts); err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_not_applicable", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
				return
			}
			// D-08: persist the normalized struct, never the raw client
			// bytes -- round-trip through json.Marshal so only the
			// validated enum value ever reaches storage.
			//
			// Deliberately NOT clamped here: an out-of-range thumbnail
			// Timecode is not rejected at this layer -- the source duration
			// is unknown until ffprobe runs in the worker, so ParseAVOpts
			// only range-checks Timecode >= 0. It surfaces later as
			// convert.ErrAVTimecodeOutOfRange and is mapped by
			// HandleAVConvert (internal/worker/worker.go, plan 03) to the
			// distinguishable "timecode_out_of_range" failure code (D-09).
			// Silently retargeting a client's request to the nearest valid
			// timecode is the exact defect class CR-01/CR-02 already closed
			// once (34-REVIEW-FIX.md) -- it must not be reintroduced here.
			normalizedRaw, err = json.Marshal(avOpts)
			if err != nil {
				writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
				return
			}
		case convert.EngineHTML:
			htmlOpts, err := convert.ParseHTMLOpts([]byte(rawOpts))
			if err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=invalid_opts", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
				return
			}
			if err := convert.ValidateHTMLApplicability(engine, detected, target, htmlOpts); err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_not_applicable", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
				return
			}
			// D-08: persist the normalized struct, never the raw client
			// bytes -- round-trip through json.Marshal so only the
			// validated enum value ever reaches storage.
			normalizedRaw, err = json.Marshal(htmlOpts)
			if err != nil {
				writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
				return
			}
		case convert.EngineOCR:
			// OCR opts (closed language allowlist eng/rus, ocropts.go) are
			// dispatched here rather than falling through to ParseDocOpts in
			// the default branch, which would reject the `language` field as
			// an unknown key -- mirroring the EngineAudio/EngineAV/EngineHTML
			// branches above exactly.
			ocrOpts, err := convert.ParseOCROpts([]byte(rawOpts))
			if err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=invalid_opts", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
				return
			}
			if err := convert.ValidateOCRApplicability(engine, detected, target, ocrOpts); err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_not_applicable", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
				return
			}
			// D-08: persist the normalized struct, never the raw client
			// bytes -- round-trip through json.Marshal so only the
			// validated enum value ever reaches storage.
			normalizedRaw, err = json.Marshal(ocrOpts)
			if err != nil {
				writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
				return
			}
		case convert.EnginePandoc:
			// Pandoc opts (closed standalone/toc/number_sections/wrap/
			// section_divs allowlist, pandocopts.go) are dispatched here
			// rather than falling through to ParseDocOpts in the default
			// branch, which would reject every pandoc-specific field as an
			// unknown key -- mirroring the EngineOCR/EngineAudio/EngineAV/
			// EngineHTML branches above exactly (D-02, 48-CONTEXT.md).
			pandocOpts, err := convert.ParsePandocOpts([]byte(rawOpts))
			if err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=invalid_opts", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
				return
			}
			if err := convert.ValidatePandocApplicability(engine, detected, target, pandocOpts); err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_not_applicable", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
				return
			}
			// D-08: persist the normalized struct, never the raw client
			// bytes -- round-trip through json.Marshal so only the
			// validated enum value ever reaches storage.
			normalizedRaw, err = json.Marshal(pandocOpts)
			if err != nil {
				writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
				return
			}
		default:
			docOpts, err := convert.ParseDocOpts([]byte(rawOpts))
			if err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=invalid_opts", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
				return
			}
			if err := convert.ValidateApplicability(engine, detected, target, docOpts); err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_not_applicable", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
				return
			}
			// D-08: persist the normalized struct, never the raw client
			// bytes -- round-trip through json.Marshal so only the
			// validated enum value ever reaches storage.
			normalizedRaw, err = json.Marshal(docOpts)
			if err != nil {
				writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
				return
			}
		}
		if err := json.Unmarshal(normalizedRaw, &normalizedOpts); err != nil {
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to normalize opts")
			return
		}
	}

	jobID := uuid.New()
	key := storage.InputKey(jobID, 0, filename)
	// Stored Content-Type is the canonical MIME of the detected format, never
	// the client-supplied multipart header (D-06).
	contentType := convert.MIMEType(detected)

	if err := s.storage.Upload(ctx, key, rest, header.Size, contentType); err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to store upload")
		return
	}

	// Pitfall 8 (TOCTOU): a preset can be deactivated or bumped to a new
	// version in the window between the resolution above and this job-row
	// insert. Immediately before repo.Create, re-run the SAME cheap
	// non-locking Resolve; ErrNotFound or a changed id/version means the
	// preset is no longer the one that was resolved, so the job must NOT be
	// created. Mirrors the existing repo.Create-failure path exactly: the
	// just-uploaded object is left in place for TTL cleanup, never deleted
	// here. No preset row is ever locked.
	if usingPreset {
		p2, err := s.presets.Resolve(ctx, client.ID, presetName)
		if err != nil {
			if errors.Is(err, presets.ErrNotFound) {
				writeError(w, http.StatusUnprocessableEntity, errcode.UnknownPreset, errUnknownPreset)
				return
			}
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to resolve preset")
			return
		}
		if p2.ID != presetID || p2.Version != presetVer {
			writeError(w, http.StatusUnprocessableEntity, errcode.UnknownPreset, errUnknownPreset)
			return
		}
	}

	// Postgres-first double write: record the job, then enqueue. The job id is
	// the one already embedded in the storage key above so they stay aligned.
	createdID, err := s.repo.Create(ctx, jobs.CreateParams{
		ID:              jobID,
		ClientID:        client.ID,
		Operation:       operation,
		Engine:          engine,
		SourceFormat:    detected,
		TargetFormat:    target,
		CallbackURL:     callbackURL,
		ProgressWebhook: progressWebhook,
		Opts:            normalizedOpts,
		PresetName:      presetNameProv,
		PresetVersion:   presetVerProv,
		Input: jobs.Input{
			Ordinal:     0,
			ObjectKey:   key,
			Filename:    filename,
			Format:      detected,
			SizeBytes:   header.Size,
			ContentType: contentType,
		},
	})
	if err != nil {
		// Reaching this branch means the request passed PreAdmit and then
		// lost the race at Create's advisory lock (or the daily rollup
		// moved between PreAdmit's read and this insert). No job row was
		// created -- the whole transaction rolled back -- and the
		// already-uploaded object above is left in place for TTL cleanup,
		// exactly like the preset-TOCTOU path already does. The enqueue
		// switch below is never reached, so QUOT-02's "before enqueue"
		// holds by construction.
		if msg, retryAfter, ok := quotaRejection(err); ok {
			log.Printf("quota rejected: client_id=%s engine=%s reason=%s stage=create", client.ID, engine, jobs.QuotaReason(err))
			writeQuotaError(w, msg, retryAfter)
			return
		}
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to create job")
		return
	}

	// Route to the matching engine-class queue (mirrors reconciler.go's
	// engine switch). The row stays in 'queued' on any enqueue failure; a
	// reconciler will recover it.
	var enqueueErr error
	switch engine {
	case convert.EngineImage:
		enqueueErr = s.queue.EnqueueImageConvert(ctx, createdID)
	case convert.EngineDocument:
		enqueueErr = s.queue.EnqueueDocumentConvert(ctx, createdID)
	case convert.EngineHTML:
		enqueueErr = s.queue.EnqueueHTMLConvert(ctx, createdID)
	case convert.EngineAudio:
		enqueueErr = s.queue.EnqueueAudioConvert(ctx, createdID)
	case convert.EngineAV:
		enqueueErr = s.queue.EnqueueAVConvert(ctx, createdID)
	case convert.EngineOCR:
		enqueueErr = s.queue.EnqueueOCRConvert(ctx, createdID)
	case convert.EnginePandoc:
		enqueueErr = s.queue.EnqueuePandocConvert(ctx, createdID)
	case convert.EngineProbe:
		enqueueErr = s.queue.EnqueueProbeSanitize(ctx, createdID)
	default:
		// Fail closed: an engine class with no known queue must never be
		// silently dropped (T-11-02). EngineFor above only ever returns a
		// value produced by a registered Converter.Engine(), so this branch
		// signals a registry/routing bug, not a client input.
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to enqueue job")
		return
	}
	if enqueueErr != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to enqueue job")
		return
	}

	resp := map[string]any{
		"job_id": createdID,
		"status": jobs.StatusQueued,
	}
	if len(normalizedOpts) > 0 {
		resp["opts"] = normalizedOpts
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// handleGetJob returns the job status; when done, a presigned download URL for
// the first output.
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid job id")
		return
	}

	job, err := s.repo.Get(ctx, id)
	if errors.Is(err, jobs.ErrNotFound) {
		writeError(w, http.StatusNotFound, errcode.NotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to load job")
		return
	}

	// Ownership guard: a job belonging to a different client is reported
	// with the EXACT SAME status and message as a truly-missing job, so
	// cross-client access is indistinguishable from not-found (AUTH-03) —
	// never a 403, never a distinct message.
	client, _ := auth.ClientFromContext(ctx)
	if job.ClientID != client.ID {
		writeError(w, http.StatusNotFound, errcode.NotFound, "job not found")
		return
	}

	resp := map[string]any{
		"job_id": job.ID,
		"status": job.Status,
	}
	if len(job.Opts) > 0 {
		resp["opts"] = job.Opts
	}
	// PROG-01/PROG-04 (Phase 41 Plan 03): surfaced null-aware, regardless of
	// status — a done job reads 100, a canceled/failed-mid-stream job reads
	// its frozen last value (D-06), and any non-av engine (or an av job
	// before its first tick) reads null. json.Marshal renders a nil *int as
	// JSON null and a non-nil one as the plain integer, so a polling client
	// sees a stable "indeterminate vs a real number" contract without any
	// fabricated value ever being invented here.
	resp["progress_pct"] = job.ProgressPct
	resp["eta_seconds"] = job.EtaSeconds

	switch job.Status {
	case jobs.StatusDone:
		outs, err := s.repo.Outputs(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to load outputs")
			return
		}
		if len(outs) > 0 {
			// ROLE-02/D-11/D-12/D-13 (57-06): every output is presigned
			// eager, in one pass, with the same s.presignTTL as before --
			// D-12, this only happens inside case jobs.StatusDone, so
			// polling an active job still makes zero PresignGet calls.
			// D-11: this single outputs[] shape covers any number of
			// future roles (preview, thumbnail, ...) without a new
			// response key per role; a flat report_url field was rejected
			// because every additional role would need its own key, and
			// carrying both a flat field AND outputs[] was rejected as two
			// representations of the same data that could silently drift
			// apart from each other.
			items := make([]map[string]any, 0, len(outs))
			var downloadURL string
			var hasPrimary bool
			for _, o := range outs {
				url, err := s.storage.PresignGet(ctx, o.ObjectKey, s.presignTTL)
				if err != nil {
					writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to presign output")
					return
				}
				items = append(items, map[string]any{
					"role":       o.Role,
					"ordinal":    o.Ordinal,
					"format":     o.Format,
					"size_bytes": o.SizeBytes,
					"url":        url,
				})
				// The primary result is selected by ROLE, never by ordinal
				// or by position in outs -- a report or preview output
				// must never end up in download_url just because it sorts
				// first (T-57-24).
				if o.Role == outputRolePrimary {
					downloadURL = url
					hasPrimary = true
				}
			}
			// D-13 (plan 57-05): MarkDone's apply closure refuses to
			// commit a done job unless job_outputs has EXACTLY ONE row
			// with role='primary', enforced against the database on the
			// same locked transaction as the status update. A done job
			// with no primary output is therefore unreachable by
			// construction, not merely unobserved -- this handler does
			// not branch or guess at a substitute here; hasPrimary simply
			// stays false in that unreachable case and download_url is
			// omitted exactly like the pre-existing empty-outs case above.
			if hasPrimary {
				resp["download_url"] = downloadURL
			}
			// Additive key, guarded on non-emptiness like resp["opts"]
			// above: outputs is present only when there is more than one
			// output, so a single-output convert job's response body
			// stays byte-identical to the pre-Phase-57 golden fixture
			// (SC4) -- an absent map key, not an empty array, is what
			// keeps the bytes unchanged.
			if len(outs) > 1 {
				resp["outputs"] = items
			}
		}
	case jobs.StatusFailed:
		if job.ErrorCode != "" {
			resp["error_code"] = job.ErrorCode
		}
		if job.ErrorMessage != "" {
			resp["error_message"] = job.ErrorMessage
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// isTerminalStatus reports whether a job status is one of the terminal
// outcomes (done/failed/canceled) that a cancel must never re-transition
// away from (D-02).
func isTerminalStatus(status string) bool {
	switch status {
	case jobs.StatusDone, jobs.StatusFailed, jobs.StatusCanceled:
		return true
	default:
		return false
	}
}

// handleCancelJob transitions a queued or active job the caller owns to
// canceled. Ownership mirrors handleGetJob exactly: a cross-client or
// nonexistent job is reported as 404, never 403 (T-39-01). Per D-02/D-09 the
// response is always 200 (never 202/403/409): an already-terminal job
// returns its current status without re-transitioning (idempotent), and a
// terminal-race (the job reaches done/failed between the read and the
// transition) re-reads and returns the now-current status rather than
// surfacing the illegal-transition error as a failure. The process kill
// itself is asynchronous/best-effort via the nil-safe cancelPublisher seam
// (Plan 03 wires the real publisher) — the request never blocks on it.
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid job id")
		return
	}

	job, err := s.repo.Get(ctx, id)
	if errors.Is(err, jobs.ErrNotFound) {
		writeError(w, http.StatusNotFound, errcode.NotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to load job")
		return
	}

	// Ownership guard: identical to handleGetJob (T-39-01) — a job belonging
	// to a different client is reported exactly as a truly-missing job.
	client, _ := auth.ClientFromContext(ctx)
	if job.ClientID != client.ID {
		writeError(w, http.StatusNotFound, errcode.NotFound, "job not found")
		return
	}

	// Idempotent: an already-terminal job is reported as-is, never
	// re-transitioned (D-02) — Cancel is not even called.
	if isTerminalStatus(job.Status) {
		writeJSON(w, http.StatusOK, map[string]any{"job_id": job.ID, "status": job.Status})
		return
	}

	if err := s.repo.Cancel(ctx, id); err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeError(w, http.StatusNotFound, errcode.NotFound, "job not found")
			return
		}
		// D-02 terminal-race: the job reached done/failed between the read
		// above and the transition. Re-read and report the now-current
		// status rather than surfacing the illegal-transition error as a
		// failure — never a 409/500 for this race.
		current, getErr := s.repo.Get(ctx, id)
		if getErr == nil && isTerminalStatus(current.Status) {
			writeJSON(w, http.StatusOK, map[string]any{"job_id": current.ID, "status": current.Status})
			return
		}
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to cancel job")
		return
	}

	// Best-effort publish of the cancel signal (speed layer, D-09): a
	// worker actively running this job may wake and kill the process
	// sooner. Fire-and-forget — its error is ignored and never blocks the
	// response, since correctness is already committed by Cancel above.
	if s.cancelPublisher != nil {
		_ = s.cancelPublisher.Publish(ctx, id)
	}

	writeJSON(w, http.StatusOK, map[string]any{"job_id": id, "status": jobs.StatusCanceled})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	// Don't HTML-escape: presigned URLs contain & that would become &.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// secondsUntilUTCMidnight returns the seconds remaining until the next UTC
// midnight after now, rounded UP to the next whole second, and clamped to a
// minimum of 1 -- a zero Retry-After would invite an immediate retry into the
// same rejection. Rounding up (rather than truncating) matters because a
// floored Retry-After points at an instant strictly before the quota window
// actually resets, so a client that obeys it lands before the reset and earns
// a second 429 for the identical reason (D-11, Codex review on PR #5). now is
// taken as a parameter rather than read internally via time.Now() so this
// stays unit testable.
func secondsUntilUTCMidnight(now time.Time) int {
	now = now.UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	remaining := nextMidnight.Sub(now)
	secs := int((remaining + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return secs
}

// writeQuotaError writes the single 429 body shape D-10 reserves for a
// quota rejection: a Retry-After header plus an {"error", "code"} body whose
// fixed code literal distinguishes it from ratelimit's rate_limited 429.
// Status is 429 unconditionally -- 403 is forbidden here
// because a quota is a consumption limit, not an authorization boundary, and
// conflating them would make a client treat a temporary condition as
// permanent (QUOT-03). Do not widen writeError's signature and do not add
// code to any other response -- D-10 scopes it to the two 429s in this
// codebase (this one and internal/ratelimit's rate_limited); the general
// sweep is SEED-020.
func writeQuotaError(w http.ResponseWriter, msg string, retryAfterSeconds int) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": msg, "code": errcode.QuotaExceeded})
}

// quotaRejection maps a jobs error to the (message, Retry-After, ok) a 429
// quota response needs. ok is false for any non-quota error. Messages
// describe only the caller's OWN limit -- never a count, never another
// client's state, never the configured limit value: an operator reads
// limits from manage-clients, a client does not need them and should not be
// able to probe them (T-52-24).
//
// A daily rejection's Retry-After is computed against the instant Postgres
// evaluated the daily window at (jobs.DailyQuotaError.EvaluatedAt), not
// against this process's own wall clock -- this is the fix for
// .planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md: a
// transaction that evaluates just before UTC midnight while the response is
// formatted just after would otherwise advertise ~86400 seconds for a quota
// that has already reset. The time.Now() fallback below exists only for a
// bare sentinel arriving from a path that did not go through evaluate
// (today: none in production -- every daily verdict evaluate returns is
// wrapped -- and only the fake repos in handlers_test.go construct one
// directly); it is deliberately not a panic or a zero, because a missing
// Retry-After would be a worse regression than an over-long one.
func quotaRejection(err error) (msg string, retryAfterSeconds int, ok bool) {
	var dqe *jobs.DailyQuotaError
	errors.As(err, &dqe)
	dailyNow := time.Now()
	if dqe != nil && !dqe.EvaluatedAt.IsZero() {
		dailyNow = dqe.EvaluatedAt
	}
	switch {
	case errors.Is(err, jobs.ErrConcurrencyLimit):
		return "too many concurrent jobs for this conversion type", quotaConcurrencyRetryAfterSeconds, true
	case errors.Is(err, jobs.ErrDailyBytesLimit):
		return "daily byte quota exceeded for this conversion type", secondsUntilUTCMidnight(dailyNow), true
	case errors.Is(err, jobs.ErrDailyJobsLimit):
		return "daily job quota exceeded for this conversion type", secondsUntilUTCMidnight(dailyNow), true
	default:
		return "", 0, false
	}
}

// writeError writes the closed-vocabulary error envelope every 4xx/429 in
// internal/api uses: a FLAT {"error": msg, "code": code} body, never nested
// as {"error": {"code": ..., "message": ...}} (D-01) -- frontend/src/api.ts:54
// reads body.error as a plain string, so nesting would break every existing
// caller. code must always be an internal/errcode constant, assigned by
// failure CLASS, never a bare string literal and never one code per message
// (D-02, D-03, SEED-020) -- sync invalid_options reuses the identical code
// name the async worker taxonomy uses for the same failure class.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
