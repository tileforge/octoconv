package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/logfmt"
	"github.com/tileforge/octoconv/internal/presets"
	"github.com/tileforge/octoconv/internal/storage"
	"github.com/tileforge/octoconv/internal/workerproto"
)

const (
	formFieldFile            = "file"
	formFieldTarget          = "target"
	formFieldCallbackURL     = "callback_url"
	formFieldProgressWebhook = "progress_webhook"
	formFieldOpts            = "opts"
	formFieldPreset          = "preset"
	formFieldOperation       = "operation"
	formFieldWorkflow        = "workflow"
	formFieldDeadline        = "deadline"
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
	maxOptsBytes                      = 4096
	maxCallbackURLBytes               = 2048
	maxInputFilenameBytes             = 255
	maxFormNonFileBytes               = 64 << 10
	formFieldsTooLargeMessage         = "form fields too large"
	createIntakeMaxConcurrent         = 8
	createIntakeBusyRetryAfterSeconds = 3
	createIntakeBusyMessage           = "service is at its concurrent upload-intake ceiling right now"
	createZipDirectoryTooLargeMessage = "the archive's central directory is larger than this service reads; repackage the archive with fewer entries"
	// multipartInMemoryBudget caps how many bytes of a multipart upload
	// ParseMultipartForm buffers in the process heap; larger file parts spill
	// to temp files on disk (Go's documented behavior). It is deliberately
	// DECOUPLED from maxUploadByte (CR-01, Phase 35): the total request size is
	// already bounded pre-parse by http.MaxBytesReader, so passing the full
	// upload ceiling (2 GiB since D-07) as the in-memory budget would let one
	// large-video upload buffer up to 2 GiB in RAM -- linear in concurrent
	// uploads, a real memory-exhaustion vector.
	multipartInMemoryBudget = 4 << 20
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

func cadProtocolJob(engine, operation string, opts map[string]any) bool {
	if engine != convert.EngineCAD || len(opts) != 0 {
		return false
	}
	return operation == convert.OperationConvert || operation == convert.OperationRender
}

// handleCreateJob accepts a multipart upload (fields: file, target), validates
// the conversion pair before touching storage, uploads the input to S3, records
// the job in Postgres (queued) and enqueues the conversion task.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rc, start := s.armIntakeDeadline(w)

	// BEFORE the body is read, let alone uploaded. A value this service will
	// not carry is worth refusing while the request has cost nothing -- and
	// the alternative, discovering it after the upload, would mean either
	// dropping the id (silently useless) or discarding work already done.
	correlationID, cerr := parseCorrelationID(r.Header)
	if cerr != nil {
		log.Printf("correlation id rejected: reason=invalid len=%d", len(r.Header.Get(CorrelationHeader)))
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
			"Correlation-Id must be at most 128 printable ASCII characters without spaces")
		return
	}

	select {
	case s.createIntakeSem <- struct{}{}:
	default:
		w.Header().Set("Retry-After", strconv.Itoa(createIntakeBusyRetryAfterSeconds))
		writeError(w, http.StatusTooManyRequests, errcode.IntakeBusy, createIntakeBusyMessage)
		return
	}
	defer func() {
		dropMultipartForm(r)
		<-s.createIntakeSem
	}()

	r.Body = s.newIntakeReader(w, r.Body, rc, start, s.maxUploadByte)
	err := parseIntakeForm(r)
	if err != nil {
		if errors.Is(err, errFormFieldsTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge, formFieldsTooLargeMessage)
			return
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge, "file exceeds size limit")
			return
		}
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid multipart form")
		return
	}

	if len(r.FormValue(formFieldCallbackURL)) > maxCallbackURLBytes {
		writeError(w, http.StatusUnprocessableEntity, errcode.PayloadTooLarge, "callback_url field too large")
		return
	}
	if r.MultipartForm != nil {
		if fhs := r.MultipartForm.File[formFieldFile]; len(fhs) > 0 && len(rawPartFilename(fhs[0])) > maxInputFilenameBytes {
			writeError(w, http.StatusUnprocessableEntity, errcode.PayloadTooLarge, "file name too long")
			return
		}
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

	if r.FormValue(formFieldWorkflow) != "" {
		s.admitWorkflow(w, r, operation, correlationID)
		return
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

	// EVERY OTHER "file" PART IS AN ATTACHMENT, in the order sent.
	//
	// A real LaTeX project is a .tex plus its .bib and its figures, and
	// job_inputs has carried an ordinal column since Phase 01 with nothing
	// populating more than one. The FIRST part stays the document: it decides
	// the source format, the engine and every check below, exactly as it did
	// when one part was all there was.
	//
	// Nothing about attachments is trusted here beyond their bytes. Their names
	// are admitted by SafeInputFilename (they become files on disk beside the
	// document), only engines that can use them accept them at all, and their
	// sizes count against the same per-engine ceiling as the document.
	var attachments []*multipart.FileHeader
	if r.MultipartForm != nil {
		if parts := r.MultipartForm.File[formFieldFile]; len(parts) > 1 {
			attachments = parts[1:]
		}
	}

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
		if r.FormValue(formFieldOperation) == "" && p.Operation != "" {
			operation = p.Operation
		}
		target = convert.NormalizeFormat(p.TargetFormat)
		presetOptsMap = p.Options
		presetNameProv = p.Name
		presetVerProv = p.Version
		presetID = p.ID
		presetVer = p.Version
	}

	// latexPrefix holds the validated sniff window for a declared-latex upload,
	// read ONCE below and used twice: for the content check that sets
	// detected == "latex", and for the tex-package admission gate after the
	// engine is known (TEX-03). nil for every other upload.
	var latexPrefix []byte

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
			cr, outcome, cerr := sniffCreateContainer(file, header.Size)
			if outcome == zipBoundExceedsCeiling {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=zip_directory_too_large", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, createZipDirectoryTooLargeMessage)
				return
			}
			if cerr == nil && outcome == zipBoundOK && cr.Format != "" && !cr.DuplicateRootPart {
				detected = cr.Format
				// D-02/D-04: reject a zip-bomb-shaped declared uncompressed
				// total before any storage write or decompression.
				if cr.TotalUncompressed > s.maxDocumentUncompressedBytes {
					log.Printf("content validation rejected: client_id=%s filename=%q reason=zip_bomb declared_uncompressed=%d limit=%d", client.ID, filename, cr.TotalUncompressed, s.maxDocumentUncompressedBytes)
					writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
						"declared uncompressed size exceeds configured limit")
					return
				}
				// D-05: macro rejection with ONE exception, added by Phase
				// 60. The original rule was unconditional and its stated
				// reason was that "macro code never executes as part of
				// producing a PDF output" -- true, and written when convert
				// was the only operation there was.
				//
				// operation=sanitize exists to REMOVE those macros. Rejecting
				// the upload here made the entire OOXML macro-removal path
				// unreachable through the API: the code was registered, the
				// pairs were advertised in GET /v1/formats, and no request
				// could ever reach it. Caught in review (PR #12).
				//
				// Every other operation keeps the unconditional rejection,
				// byte-identically -- the pre-Phase-58 golden pins that body
				// and it does not move.
				if cr.HasMacro {
					if operation != convert.OperationSanitize {
						log.Printf("content validation rejected: client_id=%s filename=%q reason=macro_detected", client.ID, filename)
						writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
							"macro-carrying documents are not accepted")
						return
					}
					// SniffContainer reports the macro-free BASE format
					// (word/document.xml -> "docx"), so a .docm would fail the
					// declared-vs-detected check one gate later. Report what
					// the package actually is, so the extension check stays a
					// real check rather than being skipped for this path.
					if macroFmt, ok := convert.MacroEnabledExtension(cr.Format); ok {
						detected = macroFmt
					}
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
	if detected == "" && source == "latex" {
		// ONE READ, TWO ANSWERS (Phase 59 D-12, and TEX-03's reason for
		// existing). The window below answers "is this really LaTeX" here and
		// "which packages does the preamble load" at the tectonic gate further
		// down. LooksLikeLaTeX(file, size) would have read the same bytes and
		// thrown them away, leaving the gate to read them again -- two answers
		// to one question about one upload, which is exactly the split D-12
		// introduced the *FromPrefix forms to avoid.
		//
		// Behaviour is unchanged from the LooksLikeLaTeX call this replaced:
		// that function IS readSniffPrefix + ValidateSniffPrefix +
		// LooksLikeLaTeXFromPrefix, and readSniffWindow uses the same
		// convert.SniffWindowBytes cap. A read error leaves latexPrefix nil and
		// detected "", failing closed to the unrecognized-content 422 below --
		// the same outcome readSniffPrefix's ok == false produced.
		if window, werr := readSniffWindow(file, header.Size); werr == nil {
			if prefix, textShaped := convert.ValidateSniffPrefix(window, header.Size); textShaped {
				latexPrefix = prefix
			}
		}
	}
	if detected == "" && source == "latex" && convert.LooksLikeLaTeXFromPrefix(latexPrefix) {
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
	if detected == "" && convert.IsCADDeclared(source) {
		if head, tail, cerr := convert.ReadCADWindows(file, header.Size); cerr == nil {
			res := convert.SniffCADWindows(head, tail, header.Size, source)
			switch res.Rejection {
			case convert.CADRejectTruncated:
				log.Printf("content validation rejected: client_id=%s filename=%q reason=truncated_cad_file format=%s", client.ID, filename, res.Format)
				writeError(w, http.StatusUnprocessableEntity, errcode.TruncatedCADFile,
					"truncated or incomplete "+res.Format+" file")
				return
			case convert.CADRejectAmbiguousSTL:
				log.Printf("content validation rejected: client_id=%s filename=%q reason=ambiguous_stl format=%s", client.ID, filename, res.Format)
				writeError(w, http.StatusUnprocessableEntity, errcode.AmbiguousSTL,
					"stl file is valid as both ascii and binary; re-export as binary stl")
				return
			case convert.CADRejectBinaryDXF:
				log.Printf("content validation rejected: client_id=%s filename=%q reason=binary_dxf_unsupported format=%s", client.ID, filename, res.Format)
				writeError(w, http.StatusUnprocessableEntity, errcode.BinaryDXFUnsupported,
					"binary dxf is not supported; re-export as ascii dxf")
				return
			}
			detected = res.Format
		}
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
		if videoDetected, videoRest, verr := convert.SniffVideo(rest); verr == nil {
			rest = videoRest
			if videoDetected != "" {
				detected = videoDetected
			}
		}
	}
	if detected == "" {
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

	var pinID string
	var pinVersion int
	var pinSchema []byte
	var legacyMatch bool
	if s.catalog != nil {
		match, found, err := s.catalog.Lookup(ctx, operation, detected, target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to create job")
			return
		}
		if found && match.LegacyEngine == "" {
			writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedConversion,
				"unsupported conversion: "+detected+" -> "+target)
			return
		}
		if found && match.LegacyEngine != engine {
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to create job")
			return
		}
		if found && match.LegacyOperation == operation {
			pinID = match.CapabilityID
			pinVersion = match.CapabilityVersion
			pinSchema = match.ParameterSchema
			legacyMatch = true
		}
	}

	// TEX-03: the tex-package admission gate. Placed after the engine class is
	// known and strictly BEFORE s.storage.Upload below, for the reason every
	// gate in this handler is placed here -- a document this deployment cannot
	// typeset must not spend S3 bytes, a queue slot, or a client's quota to be
	// told so.
	//
	// WHAT IT REFUSES, AND WHY IT IS NOT THE DECLARED SET. The allowlist is
	// convert.TeXPackageResolvable -- what the shipped bundle can LOAD -- and
	// not convert.TeXPackages, which is what this service DOCUMENTS. Gating on
	// the documented eighteen was measured against the real bundle in the
	// container (64-03 evidence) and would have refused twelve packages that
	// compile perfectly, five of them in pandoc's own default template and one
	// of them in the paper this phase ran end to end. A gate is only worth
	// having if what it refuses genuinely cannot succeed; 63-02 removed a whole
	// admission gate for exactly this defect.
	if engine == convert.EngineTectonic && len(latexPrefix) > 0 {
		var unsupported string
		complete := convert.VisitTeXPreamblePackages(latexPrefix, func(pkg string) bool {
			if convert.TeXPackageResolvable(pkg) {
				return true
			}
			unsupported = pkg
			return false
		})
		if unsupported != "" {
			// The package name is echoed to the client deliberately: a refusal
			// that does not say WHICH one leaves them bisecting their own
			// preamble, one upload per attempt. It is client-supplied text, so
			// it is logged quoted and reaches the response inside a fixed
			// sentence rather than being formatted as one.
			log.Printf("content validation rejected: client_id=%s filename=%q reason=unsupported_package package=%q", client.ID, filename, unsupported)
			// The sentence is built in convert, not here: cmd/tectonic-worker
			// says the same thing for the same package when this scan does not
			// see it, and two formatters for one condition is how the wordings
			// drift apart while the code stays equal.
			writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedPackage,
				convert.TeXUnsupportedPackageMessage(unsupported))
			return
		}
		if !complete {
			// Not a rejection: an ABSTENTION, logged at the same level so an
			// operator can see how often the cheap gate gives up rather than
			// inferring it from worker failures. Deliberately NOT phrased as
			// "content validation rejected:" -- that prefix is the D-04 bound
			// vocabulary, and nothing was rejected here.
			log.Printf("content validation abstained: client_id=%s filename=%q check=tex_packages cause=preamble_exceeds_window window=%d", client.ID, filename, len(latexPrefix))
		}
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
	// ONLY ENGINES THAT CAN USE ATTACHMENTS ACCEPT THEM. Silently ignoring the
	// extra parts would upload bytes the client is charged for, into a job that
	// cannot read them, and return 202 -- a success for work not done.
	if len(attachments) > 0 && !convert.EngineTakesMultipleInputs(engine) {
		log.Printf("content validation rejected: client_id=%s filename=%q reason=multiple_inputs engine=%s parts=%d", client.ID, filename, engine, len(attachments)+1)
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
			"this conversion accepts a single file")
		return
	}

	// HOW MANY, before how big. A request may carry up to Go's default 1 000
	// multipart parts, and nothing about their SIZE bounds their COUNT: a
	// thousand one-byte attachments are a thousand S3 uploads, a thousand rows
	// and a thousand worker downloads, charged as one small job by every quota
	// this service has. That amplification did not exist while a job was one
	// file (PR #20 review).
	//
	// 32 is past any real project -- a paper with a bibliography, a preamble
	// and two dozen figures -- and far below the parser's own ceiling.
	if len(attachments)+1 > jobs.MaxInputParts {
		log.Printf("content validation rejected: client_id=%s filename=%q reason=too_many_parts parts=%d limit=%d", client.ID, filename, len(attachments)+1, jobs.MaxInputParts)
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
			"too many files in one job")
		return
	}

	// Names first, because a refusal here costs nothing and an accepted name
	// becomes a file on disk in the worker.
	names := map[string]bool{filename: true}
	totalBytes := header.Size
	for _, a := range attachments {
		name, nerr := jobs.SafeInputFilename(rawPartFilename(a))
		if nerr != nil {
			log.Printf("content validation rejected: client_id=%s reason=unsafe_filename filename=%q", client.ID, a.Filename)
			writeError(w, http.StatusBadRequest, errcode.InvalidRequest,
				"an attached file has a name this service cannot use: send a plain file name")
			return
		}
		if jobs.WorkerOwnedName(name) {
			// The worker does not keep the document's name: it downloads
			// ordinal 0 to in.<source> and writes out.<target> beside it. An
			// attachment called in.latex therefore OVERWRITES the validated
			// document, and the engine compiles the part that passed no checks
			// (PR #20 review). Refused rather than renamed, for the reason
			// every other name here is: a renamed file is not the file the
			// document \inputs.
			log.Printf("content validation rejected: client_id=%s reason=reserved_filename filename=%q", client.ID, name)
			writeError(w, http.StatusBadRequest, errcode.InvalidRequest,
				"an attached file uses a name this service reserves for the job's own files")
			return
		}
		if names[name] {
			// Two parts with one name would overwrite each other in the job's
			// directory, and which one survived would depend on upload order.
			log.Printf("content validation rejected: client_id=%s reason=duplicate_filename filename=%q", client.ID, name)
			writeError(w, http.StatusBadRequest, errcode.InvalidRequest,
				"two attached files have the same name")
			return
		}
		names[name] = true
		totalBytes += a.Size
	}

	// The ceiling applies to the TOTAL, and it has to: the tex timeout is
	// derived from source size (64-05), so a project split across five parts
	// costs what its bytes cost regardless of how they arrived.
	if limit, perFormat, gated := s.sizeLimit(engine, detected); gated && totalBytes > limit {
		if perFormat {
			log.Printf("content validation rejected: client_id=%s filename=%q reason=format_size_limit engine=%s format=%s size=%d parts=%d limit=%d", client.ID, filename, engine, detected, totalBytes, len(attachments)+1, limit)
			writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge,
				formatLimitMessage(detected, limit))
			return
		}
		log.Printf("content validation rejected: client_id=%s filename=%q reason=engine_size_limit engine=%s size=%d parts=%d limit=%d", client.ID, filename, engine, totalBytes, len(attachments)+1, limit)
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
	// totalBytes, not header.Size: the advisory gate must charge what the
	// authoritative one will, or a project passes here and is refused at
	// Create -- after every part has been uploaded, which is precisely the
	// bandwidth this gate exists to save.
	if err := s.repo.PreAdmit(ctx, client.ID, engine, totalBytes); err != nil {
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

	if convert.HasDimensionLimit(detected) {
		width, height, _, err := convert.Dimensions(detected, rest)
		if err != nil {
			log.Printf("content validation rejected: client_id=%s filename=%q reason=dimensions_unknown", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
				"cannot determine declared image dimensions for "+filename)
			return
		}
		totalPixels := uint64(width) * uint64(height)
		if totalPixels > s.maxImagePixels {
			log.Printf("content validation rejected: client_id=%s filename=%q reason=dimension_limit width=%d height=%d limit=%d", client.ID, filename, width, height, s.maxImagePixels)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest,
				"declared image dimensions exceed configured limit")
			return
		}
	}

	// NO PAGE CEILING IS ENFORCED HERE, and the absence is a decision with
	// evidence behind it rather than an omission.
	//
	// PROBE-01 originally asked for the sanitize page ceiling to refuse an
	// over-long PDF at admission, before the S3 write. It was built, and then
	// removed after six review rounds, because deriving a page count from
	// untrusted bytes in this process could not be made to work:
	//
	//   * seven distinct ways a VALID document was rejected with 422 -- the
	//     marker sequence inside a content stream, a /Count from a neighbouring
	//     /Outlines dictionary, /Count300 matched by prefix, /Count inside an
	//     array, an indirect /Count read as the object number, a decoy object
	//     header outranking the xref, and a name that is a value swallowing the
	//     real /Type;
	//   * three resource defects on this route, which holds no concurrency
	//     semaphore: 113 MB per request from a discarded scan, 40 MB from an
	//     unbounded object index, 313 MB from unbounded dictionary nesting;
	//   * and finally the finding that settled it -- a page tree is a TREE, so
	//     a 300-page document with two 150-page children declares three
	//     different counts and the gate abstained. It answered confidently only
	//     for single-node trees, which are short documents where the ceiling is
	//     irrelevant. It could not refuse the documents it existed to refuse.
	//
	// Making it able would mean following /Root to the catalog and resolving
	// its /Pages reference -- building a document graph in the API process,
	// which is what phase 58 D-02a refused.
	//
	// The bound is enforced where an accurate count is affordable: in
	// cmd/probe-worker, with pdfinfo, terminally (PROBE-02). What was lost is
	// one S3 write and one queue slot per over-long document; what was gained
	// is that a valid document is never refused by a heuristic. See
	// 63-CONTEXT.md D-04 for the reversal and the evidence.
	//
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
		case convert.EngineCAD:
			cadOpts, err := convert.ParseCADOpts([]byte(rawOpts))
			if err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=invalid_opts", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
				return
			}
			if err := convert.ValidateCADApplicability(engine, operation, detected, target, cadOpts); err != nil {
				log.Printf("content validation rejected: client_id=%s filename=%q reason=opts_not_applicable", client.ID, filename)
				writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
				return
			}
			normalizedRaw, err = json.Marshal(cadOpts)
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

	if legacyMatch {
		instance := map[string]any{}
		if normalizedOpts != nil {
			instance = normalizedOpts
		}
		if err := workerproto.ValidateParameters(pinSchema, instance); err != nil {
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidOptions, "invalid opts")
			return
		}
	}

	dispatchOwner := ""
	var attemptBudget time.Duration
	imageProtocol := engine == convert.EngineImage
	cadProtocol := cadProtocolJob(engine, operation, normalizedOpts) && !progressWebhook
	documentProtocol := engine == convert.EngineDocument && len(normalizedOpts) == 0 && !progressWebhook
	htmlProtocol := engine == convert.EngineHTML && len(normalizedOpts) == 0 && !progressWebhook
	audioProtocol := engine == convert.EngineAudio && len(normalizedOpts) == 0 && !progressWebhook
	avProtocol := engine == convert.EngineAV && len(normalizedOpts) == 0 && !progressWebhook
	if legacyMatch && pinID != "" && (imageProtocol || cadProtocol || documentProtocol || htmlProtocol || audioProtocol || avProtocol) {
		budget := s.engineTimeouts[engine]
		if budget <= 0 {
			budget = 120 * time.Second
			if engine == convert.EngineCAD {
				budget = convert.CADDefaultEngineTimeout
			}
		}
		linkCap := time.Duration(workerproto.FileLinkMaxTTLSeconds) * time.Second
		if budget <= linkCap {
			dispatchOwner = "protocol"
			attemptBudget = budget
		}
	}
	if engine == convert.EngineCAD && dispatchOwner != "protocol" {
		switch {
		case progressWebhook:
			log.Printf("content validation rejected: client_id=%s filename=%q reason=cad_progress_webhook", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "cad jobs do not accept progress_webhook")
		case len(normalizedOpts) > 0:
			log.Printf("content validation rejected: client_id=%s filename=%q reason=cad_opts", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
		default:
			writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedConversion,
				"unsupported conversion: "+detected+" -> "+target)
		}
		return
	}
	if engine == convert.EngineDocument && dispatchOwner != "protocol" {
		switch {
		case progressWebhook:
			log.Printf("content validation rejected: client_id=%s filename=%q reason=document_progress_webhook", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "document jobs do not accept progress_webhook")
		case len(normalizedOpts) > 0:
			log.Printf("content validation rejected: client_id=%s filename=%q reason=document_opts", client.ID, filename)
			writeError(w, http.StatusUnprocessableEntity, errcode.OptsNotApplicable, "opts not applicable to this conversion")
		default:
			writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedConversion,
				"unsupported conversion: "+detected+" -> "+target)
		}
		return
	}

	jobID := uuid.New()
	key := storage.InputKey(jobID, 0, filename)
	// Stored Content-Type is the canonical MIME of the detected format, never
	// the client-supplied multipart header (D-06).
	contentType := convert.MIMEType(detected)

	if err := s.storage.Upload(ctx, key, io.NewSectionReader(file, 0, header.Size), header.Size, contentType); err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to store upload")
		return
	}

	// Attachments, at ordinals 1..N in the order the client sent them.
	//
	// They are stored as opaque bytes: no sniffing, no format claim, no
	// content-type guess. The document at ordinal 0 is what decides the
	// conversion, and an attachment is data that document names -- calling a
	// .bib "text/plain" or a figure "application/pdf" here would be a guess
	// this service does not need to make. The stored format is empty for the
	// same reason.
	//
	// A failure part-way leaves earlier objects in place for TTL cleanup and
	// creates no job row, which is the same shape the single-input path
	// already has for a Create failure.
	inputs := []jobs.Input{{
		Ordinal:     0,
		ObjectKey:   key,
		Filename:    filename,
		Format:      detected,
		SizeBytes:   header.Size,
		ContentType: contentType,
	}}
	for i, a := range attachments {
		ordinal := i + 1
		name, _ := jobs.SafeInputFilename(rawPartFilename(a)) // admitted above; re-derived, never re-decided
		src, oerr := a.Open()
		if oerr != nil {
			writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "could not read an attached file")
			return
		}
		akey := storage.InputKey(jobID, ordinal, name)
		uerr := func() error {
			defer src.Close()
			return s.storage.Upload(ctx, akey, src, a.Size, "application/octet-stream")
		}()
		if uerr != nil {
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to store upload")
			return
		}
		inputs = append(inputs, jobs.Input{
			Ordinal:     ordinal,
			ObjectKey:   akey,
			Filename:    name,
			SizeBytes:   a.Size,
			ContentType: "application/octet-stream",
		})
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
		CorrelationID:         correlationID,
		ID:                    jobID,
		ClientID:              client.ID,
		Operation:             operation,
		Engine:                engine,
		SourceFormat:          detected,
		TargetFormat:          target,
		CallbackURL:           callbackURL,
		ProgressWebhook:       progressWebhook,
		Opts:                  normalizedOpts,
		PresetName:            presetNameProv,
		PresetVersion:         presetVerProv,
		Inputs:                inputs,
		CapabilityID:          pinID,
		CapabilityVersion:     pinVersion,
		DispatchOwner:         dispatchOwner,
		ProtocolAttemptBudget: attemptBudget,
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
	// THE JOIN BETWEEN THE TWO WORLDS, and the only place it exists.
	//
	// chi's middleware.RequestID gives every HTTP request an id and its Logger
	// prints it, but that id is never stored and never leaves this process.
	// job_id is stored everywhere and never appears in an HTTP log line. This
	// record is where they meet: from here, request_id finds the chi access
	// log, and job_id finds every worker line about the same job.
	//
	// After Create and before enqueue, deliberately: a job that exists is
	// worth a line even if the enqueue then fails, and that is precisely the
	// case an operator has to investigate (the Postgres-first double write
	// exists so such a row is inspectable rather than lost).
	//
	// filename is the client's, so it goes through the same escaping the
	// worker uses -- a newline in it would otherwise split this record.
	log.Println(logfmt.KV("job accepted",
		"request_id", middleware.GetReqID(ctx),
		"job_id", createdID.String(),
		"client_id", client.ID.String(),
		"engine", engine,
		"source", detected,
		"target", target,
		"filename", filename,
		"bytes", strconv.FormatInt(totalBytes, 10),
		"correlation_id", correlationID,
	))

	if dispatchOwner != "protocol" {
		routed, enqueueErr := s.enqueueForEngine(ctx, engine, createdID)
		if !routed {
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

func sniffCreateContainer(file multipart.File, size int64) (cr convert.ContainerResult, outcome zipBoundOutcome, err error) {
	bounded := &readFailureRecorder{r: file}
	if outcome, _, _ = zipDirectoryExceedsStructuralBound(bounded, size); outcome.exceeds() {
		return convert.ContainerResult{}, outcome, nil
	}
	if bounded.err != nil {
		return convert.ContainerResult{}, zipBoundReadFailed, bounded.err
	}
	cr, err = convert.SniffContainer(file, size)
	switch {
	case err == nil:
		return cr, zipBoundOK, nil
	case errors.Is(err, convert.ErrNotAZip):
		return convert.ContainerResult{}, zipBoundMalformed, err
	default:
		return convert.ContainerResult{}, zipBoundReadFailed, err
	}
}

type readFailureRecorder struct {
	r   io.ReaderAt
	err error
}

func (f *readFailureRecorder) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.r.ReadAt(p, off)
	if err != nil && err != io.EOF && f.err == nil {
		f.err = err
	}
	return n, err
}

// enqueueForEngine routes one created job to its engine-class queue. The
// FIRST return value is false for an engine this switch does not know --
// which the caller turns into a fail-closed 500.
//
// It is a METHOD rather than the inline switch it replaced for one reason:
// a switch inside a handler can only be exercised through a full multipart
// request, so an engine class that reached the registry but never reached
// this routing table stayed invisible until someone happened to write a
// fixture for it. Tectonic did exactly that -- registered in 64-02, missing
// here, and every real .tex upload 500'd. Extracted so
// TestEveryEngineHasAnEnqueueRoute can walk convert.AllEngines() directly
// and fail by NAME on the tenth engine, with no fixture to invent.
func (s *Server) enqueueForEngine(ctx context.Context, engine string, id uuid.UUID) (routed bool, err error) {
	switch engine {
	case convert.EngineImage:
		return true, s.queue.EnqueueImageConvert(ctx, id)
	case convert.EngineDocument:
		return true, s.queue.EnqueueDocumentConvert(ctx, id)
	case convert.EngineHTML:
		return true, s.queue.EnqueueHTMLConvert(ctx, id)
	case convert.EngineAudio:
		return true, s.queue.EnqueueAudioConvert(ctx, id)
	case convert.EngineAV:
		return true, s.queue.EnqueueAVConvert(ctx, id)
	case convert.EngineOCR:
		return true, s.queue.EnqueueOCRConvert(ctx, id)
	case convert.EnginePandoc:
		return true, s.queue.EnqueuePandocConvert(ctx, id)
	case convert.EngineProbe:
		return true, s.queue.EnqueueProbeSanitize(ctx, id)
	case convert.EngineTectonic:
		return true, s.queue.EnqueueTectonicConvert(ctx, id)
	case convert.EngineCAD:
		return true, s.queue.EnqueueCADJob(ctx, id)
	}
	return false, nil
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
	// Echoed so the caller can VERIFY its value arrived, rather than assume
	// it. Omitted when absent for the same reason it is omitted from the
	// webhook: a field that is usually null trains readers to ignore it.
	if job.CorrelationID != "" {
		resp["correlation_id"] = job.CorrelationID
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
	if job.Engine == convert.EngineCAD {
		resp["stage"] = job.Stage
		resp["stage_index"] = job.StageIndex
		resp["stage_count"] = job.StageCount
	}

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

	// NO RECORD HERE. jobs.Repo.transition emits it, which is the only place
	// that sees EVERY cancellation -- this handler, the operator CLI's
	// cancel-job, and any future caller -- and that reads from_status under
	// the same lock as the transition. Logging here covered one of the three
	// and reported a possibly stale prior status (codex, PR #23).

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
