package workerproto

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/storage"
)

type pullRequest struct {
	ProtocolVersion json.Number     `json:"protocol_version"`
	PullRequestID   string          `json:"pull_request_id"`
	FreeSlots       json.Number     `json:"free_slots"`
	WaitSeconds     json.Number     `json:"wait_seconds"`
	Extensions      json.RawMessage `json:"extensions"`
}

type assignmentLimits struct {
	MaxOutputBytes int `json:"max_output_bytes"`
}

type fileLinkWire struct {
	Role      string `json:"role"`
	Ordinal   int    `json:"ordinal"`
	Format    string `json:"format"`
	Href      string `json:"href"`
	ExpiresAt string `json:"expires_at"`
}

type assignmentWire struct {
	JobID             string           `json:"job_id"`
	StepID            string           `json:"step_id"`
	AttemptID         string           `json:"attempt_id"`
	AttemptToken      string           `json:"attempt_token"`
	CapabilityID      string           `json:"capability_id"`
	CapabilityVersion int              `json:"capability_version"`
	AttemptDeadline   string           `json:"attempt_deadline"`
	LeaseExpiresAt    string           `json:"lease_expires_at"`
	Limits            assignmentLimits `json:"limits"`
	Inputs            []fileLinkWire   `json:"inputs"`
	Outputs           []fileLinkWire   `json:"outputs"`
	Parameters        json.RawMessage  `json:"parameters,omitempty"`
}

// assignmentParameters returns stored job options, or nil when the object has no keys.
// encoding/json omitempty drops a nil RawMessage and keeps "{}".
func assignmentParameters(raw []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("decode job options: %w", err)
	}
	if len(obj) == 0 {
		return nil, nil
	}
	return json.RawMessage(trimmed), nil
}

type completeArtifact struct {
	Role        string          `json:"role"`
	Ordinal     json.Number     `json:"ordinal"`
	Format      string          `json:"format"`
	ContentType string          `json:"content_type"`
	SHA256      string          `json:"sha256"`
	ByteSize    json.Number     `json:"byte_size"`
	Extensions  json.RawMessage `json:"extensions"`
}

type completeRequest struct {
	ProtocolVersion json.Number     `json:"protocol_version"`
	Artifacts       json.RawMessage `json:"artifacts"`
	Extensions      json.RawMessage `json:"extensions"`
}

type pullWire struct {
	ProtocolVersion int              `json:"protocol_version"`
	Assignments     []assignmentWire `json:"assignments"`
	Control         controlWire      `json:"control"`
}

func (h *Handler) handlePull(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.Header["Octoconv-Attempt"]; ok {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sess, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	raw, ok := readMessage(w, r)
	if !ok {
		return
	}
	var req pullRequest
	if !decodeRaw(w, raw, &req) {
		return
	}
	if !protocolVersionOK(w, req.ProtocolVersion) {
		return
	}
	if !extensionsObject(req.Extensions) {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	pullID, err := uuid.Parse(req.PullRequestID)
	if req.PullRequestID == "" || err != nil {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	freeSlots, freeKind := integerToken(req.FreeSlots)
	waitSeconds, waitKind := integerToken(req.WaitSeconds)
	if freeKind != numOK || waitKind != numOK || freeSlots < 0 {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	bodyHash := sha256.Sum256(raw)
	body, err := h.servePull(r.Context(), sess, pullID, hex.EncodeToString(bodyHash[:]), freeSlots, waitSeconds)
	if err != nil {
		var status *pullStatus
		if errors.As(err, &status) {
			writeCode(w, status.status, status.code)
			return
		}
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	writeStored(w, body)
}

type pullStatus struct {
	status int
	code   string
}

func (e *pullStatus) Error() string { return e.code }

var errDedupLost = errors.New("pull dedup lost")

type progressRequest struct {
	ProtocolVersion json.Number     `json:"protocol_version"`
	Percent         json.RawMessage `json:"percent"`
	Stage           json.RawMessage `json:"stage"`
	StageIndex      json.RawMessage `json:"stage_index"`
	StageCount      json.RawMessage `json:"stage_count"`
	ETASeconds      json.RawMessage `json:"eta_seconds"`
	Extensions      json.RawMessage `json:"extensions"`
}

type leaseWire struct {
	ProtocolVersion int         `json:"protocol_version"`
	LeaseExpiresAt  string      `json:"lease_expires_at"`
	AttemptDeadline string      `json:"attempt_deadline"`
	Control         controlWire `json:"control"`
}

var errAttemptInactive = errors.New("attempt not active")

func (h *Handler) handleLease(w http.ResponseWriter, r *http.Request) {
	sess, attemptID, ok := h.authenticateAttempt(w, r)
	if !ok {
		return
	}
	raw, ok := readMessage(w, r)
	if !ok {
		return
	}
	var req protocolRequest
	if !decodeRaw(w, raw, &req) || !protocolFields(w, req.ProtocolVersion, req.Extensions) {
		return
	}
	var lease, deadline time.Time
	var cancelIDs []string
	var draining bool
	err := pgx.BeginTxFunc(r.Context(), h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		err := tx.QueryRow(r.Context(), `
			UPDATE worker_slot_holds AS h
			SET lease_expires_at = LEAST(now() + interval '30 seconds', h.attempt_deadline)
			WHERE h.id = $1
			  AND h.session_id = $2
			  AND h.state = 'active'
			  AND h.released_at IS NULL
			  AND h.attempt_deadline > now()
			  AND LEAST(now() + interval '30 seconds', h.attempt_deadline) > h.lease_expires_at
			  AND LEAST(now() + interval '30 seconds', h.attempt_deadline) > now() + interval '5 seconds'
			  AND NOT EXISTS (
			      SELECT 1 FROM jobs j
			      WHERE j.id = h.job_id AND j.status IN ('canceled', 'failed', 'done')
			  )
			RETURNING h.lease_expires_at, h.attempt_deadline`, attemptID, sess.ID).Scan(&lease, &deadline)
		if errors.Is(err, pgx.ErrNoRows) {
			return errAttemptInactive
		}
		if err != nil {
			return fmt.Errorf("renew lease: %w", err)
		}
		cancelIDs, err = canceledAttempts(r.Context(), tx, sess.ID)
		if err != nil {
			return err
		}
		draining, err = sessionDraining(r.Context(), tx, sess.ID)
		return err
	})
	if errors.Is(err, errAttemptInactive) {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	writeJSON(w, http.StatusOK, leaseWire{
		ProtocolVersion: ProtocolVersion,
		LeaseExpiresAt:  lease.UTC().Format(time.RFC3339Nano),
		AttemptDeadline: deadline.UTC().Format(time.RFC3339Nano),
		Control:         controlWire{CancelAttemptIDs: cancelIDs, Drain: draining, Terminate: false},
	})
}

type failRequest struct {
	ProtocolVersion json.Number     `json:"protocol_version"`
	EngineCode      string          `json:"engine_code"`
	Message         string          `json:"message"`
	Extensions      json.RawMessage `json:"extensions"`
}

type stoppedRequest struct {
	ProtocolVersion json.Number     `json:"protocol_version"`
	Reason          string          `json:"reason"`
	Extensions      json.RawMessage `json:"extensions"`
}

var terminalEngineCodes = map[string]struct{}{
	"engine_error":          {},
	"invalid_options":       {},
	"duration_exceeded":     {},
	"timecode_out_of_range": {},
	"resolution_exceeded":   {},
	"no_video_stream":       {},
	"too_many_pages":        {},
	"page_too_large":        {},
	"image_too_large":       {},
	"unsupported_language":  {},
	"unsupported_target":    {},
	"input_too_large":       {},
	"output_too_large":      {},
	"reconciler_exhausted":  {},
	"unsupported_package":   {},
	"cad_too_complex":       {},
	"cad_unreadable":        {},
	"cad_empty_result":      {},
	"cad_mesh_timeout":      {},
}

var attemptJitter = randomAttemptDelay

func randomAttemptDelay(n int) (int, error) {
	span := 15
	if n >= 0 && n < 3 {
		span = 2 << n
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(span)+1))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func (h *Handler) handleProgress(w http.ResponseWriter, r *http.Request) {
	sess, attemptID, raw, ok := h.readAttemptBody(w, r)
	if !ok {
		return
	}
	var req progressRequest
	if !decodeRaw(w, raw, &req) || !protocolFields(w, req.ProtocolVersion, req.Extensions) {
		return
	}
	live, err := h.attemptOpen(r.Context(), attemptID, sess.ID, true)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if !live {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	parsed, code := parseProgress(req)
	if code != "" {
		writeCode(w, http.StatusBadRequest, code)
		return
	}
	limited, notice, err := h.storeProgress(r.Context(), sess.ID, attemptID, parsed)
	if errors.Is(err, errAttemptInactive) {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if limited {
		writeCode(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	h.fireProgress(notice)
	h.writeControl(w, r, sess.ID)
}

func (h *Handler) readAttemptBody(w http.ResponseWriter, r *http.Request) (storedSession, uuid.UUID, []byte, bool) {
	sess, attemptID, ok := h.authenticateAttempt(w, r)
	if !ok {
		return storedSession{}, uuid.Nil, nil, false
	}
	raw, ok := readMessage(w, r)
	if !ok {
		return storedSession{}, uuid.Nil, nil, false
	}
	return sess, attemptID, raw, true
}

func (h *Handler) handleFail(w http.ResponseWriter, r *http.Request) {
	sess, attemptID, raw, ok := h.readAttemptBody(w, r)
	if !ok {
		return
	}
	var req failRequest
	if !decodeRaw(w, raw, &req) || !protocolFields(w, req.ProtocolVersion, req.Extensions) {
		return
	}
	live, err := h.attemptOpen(r.Context(), attemptID, sess.ID, true)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if !live {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	if req.EngineCode == "" || req.Message == "" {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	if req.EngineCode == "timeout" {
		err = h.requeueLiveAttempt(r.Context(), sess.ID, attemptID, req.Message)
	} else {
		code := "engine_error"
		if _, ok := terminalEngineCodes[req.EngineCode]; ok {
			code = req.EngineCode
		}
		err = h.failLiveAttempt(r.Context(), sess.ID, attemptID, code, req.Message)
	}
	if errors.Is(err, errAttemptInactive) {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	h.deleteStagingIfReleased(r.Context(), attemptID, sess.ID)
	h.writeControl(w, r, sess.ID)
}

func (h *Handler) handleStopped(w http.ResponseWriter, r *http.Request) {
	sess, attemptID, raw, ok := h.readAttemptBody(w, r)
	if !ok {
		return
	}
	var req stoppedRequest
	if !decodeRaw(w, raw, &req) || !protocolFields(w, req.ProtocolVersion, req.Extensions) {
		return
	}
	if req.Reason != "cancel" && req.Reason != "terminate" && req.Reason != "lease_lost" {
		if req.Reason == "" {
			live, err := h.attemptOpen(r.Context(), attemptID, sess.ID, true)
			if err != nil {
				writeCode(w, http.StatusInternalServerError, "invalid_message")
				return
			}
			if !live {
				writeCode(w, http.StatusConflict, "attempt_not_active")
				return
			}
		}
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	err := h.stopAttempt(r.Context(), sess.ID, attemptID, req.Reason)
	if errors.Is(err, errAttemptInactive) {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	h.deleteStagingIfReleased(r.Context(), attemptID, sess.ID)
	h.writeControl(w, r, sess.ID)
}

func (h *Handler) handleComplete(w http.ResponseWriter, r *http.Request) {
	sess, attemptID, raw, ok := h.readAttemptBody(w, r)
	if !ok {
		return
	}
	var req completeRequest
	if !decodeRaw(w, raw, &req) || !protocolFields(w, req.ProtocolVersion, req.Extensions) {
		return
	}
	live, err := h.attemptOpen(r.Context(), attemptID, sess.ID, true)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if !live {
		_, keyOK, err := h.loadCompleteKey(r.Context(), attemptID, sess.ID)
		if err != nil {
			writeCode(w, http.StatusInternalServerError, "invalid_message")
			return
		}
		if !keyOK {
			writeCode(w, http.StatusConflict, "attempt_not_active")
			return
		}
	}
	if len(bytes.TrimSpace(req.Artifacts)) == 0 || string(bytes.TrimSpace(req.Artifacts)) == "null" {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	arts, err := parseArtifacts(req.Artifacts)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	for _, art := range arts {
		if !extensionsObject(art.Extensions) {
			writeCode(w, http.StatusBadRequest, "invalid_message")
			return
		}
	}
	rawSum := sha256.Sum256(raw)
	bodyHash := hex.EncodeToString(rawSum[:])
	stored, err := h.storedComplete(r.Context(), sess.ID, attemptID, bodyHash)
	if err != nil {
		var fail *completeFailure
		if errors.As(err, &fail) {
			writeCode(w, fail.status, fail.code)
			return
		}
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if stored != nil {
		writeStored(w, stored)
		return
	}
	body, err := h.acceptComplete(r.Context(), sess, attemptID, bodyHash, arts)
	var fail *completeFailure
	if errors.As(err, &fail) {
		writeCode(w, fail.status, fail.code)
		return
	}
	if errors.Is(err, errAttemptInactive) {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	h.deleteStaging(r.Context(), attemptID)
	writeStored(w, body)
}

func (h *Handler) deleteStagingIfReleased(ctx context.Context, attemptID, sessionID uuid.UUID) {
	var released bool
	err := h.repo.pool.QueryRow(ctx, `
		SELECT released_at IS NOT NULL
		FROM worker_slot_holds
		WHERE id = $1 AND session_id = $2`, attemptID, sessionID).Scan(&released)
	if err != nil || !released {
		return
	}
	h.deleteStaging(ctx, attemptID)
}

func (h *Handler) deleteStaging(ctx context.Context, attemptID uuid.UUID) {
	if h.store == nil {
		return
	}
	_ = h.store.Remove(ctx, storage.AttemptOutputKey(attemptID, 0))
	_ = h.store.Remove(ctx, storage.AttemptOutputKey(attemptID, 1))
}

type completeFailure struct {
	status int
	code   string
}

func (e *completeFailure) Error() string { return e.code }

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func completeDedupKey(jobID, stepID, attemptID uuid.UUID) string {
	return jobID.String() + "/" + stepID.String() + "/" + attemptID.String() + "/complete"
}

func (h *Handler) loadCompleteKey(ctx context.Context, attemptID, sessionID uuid.UUID) (string, bool, error) {
	var jobID, stepID uuid.UUID
	err := h.repo.pool.QueryRow(ctx, `
		SELECT job_id, step_id
		FROM worker_slot_holds
		WHERE id = $1 AND session_id = $2
		  AND job_id IS NOT NULL
		  AND step_id IS NOT NULL`, attemptID, sessionID).Scan(&jobID, &stepID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read complete key: %w", err)
	}
	return completeDedupKey(jobID, stepID, attemptID), true, nil
}

func (h *Handler) storedComplete(ctx context.Context, sessionID, attemptID uuid.UUID, bodyHash string) ([]byte, error) {
	key, ok, err := h.loadCompleteKey(ctx, attemptID, sessionID)
	if err != nil || !ok {
		return nil, err
	}
	hash, stored, found, err := readStoredComplete(ctx, h.repo.pool, sessionID, key)
	if err != nil || !found {
		return nil, err
	}
	if hash != bodyHash {
		return nil, &completeFailure{status: http.StatusConflict, code: "complete_conflict"}
	}
	return stored, nil
}

func readStoredComplete(ctx context.Context, q queryRower, sessionID uuid.UUID, key string) (string, []byte, bool, error) {
	var hash *string
	var response []byte
	err := q.QueryRow(ctx, `
		SELECT body_sha256, response_body
		FROM worker_request_dedup
		WHERE session_id = $1 AND dedup_key = $2`, sessionID, key).Scan(&hash, &response)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && response == nil) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("read complete: %w", err)
	}
	storedHash := ""
	if hash != nil {
		storedHash = *hash
	}
	return storedHash, response, true, nil
}

func (h *Handler) acceptComplete(ctx context.Context, sess storedSession, attemptID uuid.UUID, bodyHash string, arts []completeArtifact) ([]byte, error) {
	var body []byte
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := lockAttemptIO(ctx, tx, attemptID); err != nil {
			return err
		}
		jobID, err := readAttemptJob(ctx, tx, attemptID, sess.ID)
		if err != nil {
			return err
		}
		var status, target, engine string
		var snapshotID *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT status, target_format, engine, workflow_snapshot_id
			FROM jobs WHERE id = $1`, jobID).Scan(&status, &target, &engine, &snapshotID); err != nil {
			return fmt.Errorf("read job: %w", err)
		}
		var stepID uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT step_id FROM worker_slot_holds
			WHERE id = $1 AND session_id = $2`, attemptID, sess.ID).Scan(&stepID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errAttemptInactive
		}
		if err != nil {
			return fmt.Errorf("read attempt step: %w", err)
		}
		key := completeDedupKey(jobID, stepID, attemptID)
		handled, err := takeStoredComplete(ctx, tx, sess.ID, key, bodyHash, &body)
		if handled || err != nil {
			return err
		}
		if status != jobs.StatusActive {
			return &completeFailure{status: http.StatusConflict, code: "attempt_not_active"}
		}
		if err := reportingAttemptLive(ctx, tx, attemptID, sess.ID); err != nil {
			return err
		}
		var optsRaw []byte
		if err := tx.QueryRow(ctx, `SELECT options FROM jobs WHERE id = $1`, jobID).Scan(&optsRaw); err != nil {
			return fmt.Errorf("read job options: %w", err)
		}
		ex, extracting, err := parseExtraction(optsRaw)
		if err != nil {
			return err
		}
		if extracting && ex.Phase == "unpack" {
			handled, err := h.acceptUnpack(ctx, tx, sess, jobID, attemptID, key, bodyHash, &body, ex, arts)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}
			h.removeExtraAttemptOutputs(ctx, attemptID, len(ex.Entries))
			return storeControlResponse(ctx, tx, sess, key, bodyHash, &body)
		}
		wantFormat := target
		limitEngine := engine
		var snap workflowSnapDoc
		stepIndex := -1
		if snapshotID != nil {
			var err error
			snap, stepIndex, err = workflowStepIndex(ctx, tx, jobID, &stepID)
			if err != nil {
				return err
			}
			if stepIndex < 0 {
				return &completeFailure{status: http.StatusConflict, code: "artifact_rejected"}
			}
			wantFormat = snap.Steps[stepIndex].TargetFormat
			if snap.Steps[stepIndex].LegacyEngine != "" {
				limitEngine = snap.Steps[stepIndex].LegacyEngine
			}
		}
		if packOutputsAreZip(engine, snapshotID, optsRaw) {
			wantFormat = "zip"
		}
		maxOut := protocolOutputLimit(limitEngine, wantFormat)
		primary, report, matched := matchCompleteArtifacts(limitEngine, arts, wantFormat, maxOut)
		if !matched {
			return &completeFailure{status: http.StatusConflict, code: "artifact_rejected"}
		}
		pieces := []completeArtifact{primary}
		ordinals := []int{0}
		if report != nil {
			pieces = append(pieces, *report)
			ordinals = append(ordinals, 1)
		}
		dsts := make([]string, 0, len(pieces))
		gotSizes := make([]int64, 0, len(pieces))
		for i, piece := range pieces {
			size, kind := integerToken(piece.ByteSize)
			if kind != numOK {
				return &completeFailure{status: http.StatusConflict, code: "artifact_rejected"}
			}
			src := storage.AttemptOutputKey(attemptID, ordinals[i])
			dst := storage.AcceptedOutputKey(jobID, attemptID, ordinals[i], piece.SHA256)
			gotSize, err := h.copyMatched(ctx, tx, src, dst, piece.SHA256, size, maxOut)
			if err != nil {
				if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
					return rmErr
				}
				return err
			}
			dsts = append(dsts, dst)
			gotSizes = append(gotSizes, gotSize)
		}
		if _, err := lockReportingJob(ctx, tx, attemptID, sess.ID); err != nil {
			if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
				return rmErr
			}
			return err
		}
		handled, err = takeStoredComplete(ctx, tx, sess.ID, key, bodyHash, &body)
		if err != nil {
			if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
				return rmErr
			}
			return err
		}
		if handled {
			return nil
		}
		var clientID *uuid.UUID
		var admitted *int64
		if err := tx.QueryRow(ctx, `
			SELECT status, client_id, engine, admitted_input_bytes FROM jobs WHERE id = $1`, jobID).Scan(&status, &clientID, &engine, &admitted); err != nil {
			return fmt.Errorf("read job: %w", err)
		}
		if status != jobs.StatusActive {
			if err := h.removeUnnamedCopies(ctx, tx, dsts...); err != nil {
				return err
			}
			return &completeFailure{status: http.StatusConflict, code: "attempt_not_active"}
		}
		if err := h.attemptLiveNow(ctx, tx, attemptID, sess.ID, dsts...); err != nil {
			return err
		}
		if snapshotID != nil && stepIndex+1 < len(snap.Steps) {
			next := snap.Steps[stepIndex+1]
			quotaErr := h.admitChangedEngine(ctx, tx, clientID, engine, next.LegacyEngine, admitted)
			reason := jobs.QuotaReason(quotaErr)
			if quotaErr != nil && reason == "" {
				return quotaErr
			}
			if reason != "" {
				if err := h.removeUnnamedCopies(ctx, tx, dsts...); err != nil {
					return err
				}
				if err := h.jobsRepo.FailProtocolIn(ctx, tx, jobID, reason, quotaErr.Error()); err != nil {
					return err
				}
			} else if err := recordContinuation(ctx, tx, jobID, stepID, attemptID, dsts[0], pieces[0], gotSizes[0], next); err != nil {
				if len(dsts) > 1 {
					if rmErr := h.removeUnnamedCopies(ctx, tx, dsts[1:]...); rmErr != nil {
						return rmErr
					}
				}
				return err
			} else if len(dsts) > 1 {
				if err := h.removeUnnamedCopies(ctx, tx, dsts[1:]...); err != nil {
					return err
				}
			}
		} else if extracting && ex.Phase == "convert" {
			if err := h.acceptConvertEntry(ctx, tx, jobID, attemptID, ex, dsts, gotSizes, clientID, engine, admitted); err != nil {
				return err
			}
		} else if err := h.acceptCopied(ctx, tx, jobID, attemptID, pieces, dsts, gotSizes); err != nil {
			return err
		}
		return storeControlResponse(ctx, tx, sess, key, bodyHash, &body)
	})
	return body, err
}

func storeControlResponse(ctx context.Context, tx pgx.Tx, sess storedSession, key, bodyHash string, body *[]byte) error {
	ids, err := canceledAttempts(ctx, tx, sess.ID)
	if err != nil {
		return err
	}
	draining, err := sessionDraining(ctx, tx, sess.ID)
	if err != nil {
		return err
	}
	encoded, err := encodeWire(heartbeatWire{
		ProtocolVersion: ProtocolVersion,
		Control:         controlWire{CancelAttemptIDs: ids, Drain: draining, Terminate: false},
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO worker_request_dedup (session_id, dedup_key, body_sha256, response_body)
		VALUES ($1, $2, $3, $4)`,
		sess.ID, key, bodyHash, encoded); err != nil {
		return fmt.Errorf("store complete: %w", err)
	}
	*body = encoded
	return nil
}

func lockAttemptIO(ctx context.Context, tx pgx.Tx, attemptID uuid.UUID) error {
	sum := sha256.Sum256(attemptID[:])
	key1 := int32(binary.BigEndian.Uint32(sum[:4]))
	key2 := int32(binary.BigEndian.Uint32(sum[4:8]))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, key1, key2); err != nil {
		return fmt.Errorf("lock attempt: %w", err)
	}
	return nil
}

func readAttemptJob(ctx context.Context, tx pgx.Tx, attemptID, sessionID uuid.UUID) (uuid.UUID, error) {
	var jobID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT job_id FROM worker_slot_holds
		WHERE id = $1 AND session_id = $2 AND job_id IS NOT NULL`, attemptID, sessionID).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errAttemptInactive
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read attempt job: %w", err)
	}
	return jobID, nil
}

func takeStoredComplete(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID, key, bodyHash string, body *[]byte) (bool, error) {
	hash, stored, found, err := readStoredComplete(ctx, tx, sessionID, key)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if hash != bodyHash {
		return true, &completeFailure{status: http.StatusConflict, code: "complete_conflict"}
	}
	*body = stored
	return true, nil
}

func (h *Handler) copyMatched(ctx context.Context, tx pgx.Tx, src, dst, sha string, size, maxOut int64) (int64, error) {
	err := h.store.Copy(ctx, src, dst)
	if errors.Is(err, storage.ErrNotFound) {
		return 0, &completeFailure{status: http.StatusConflict, code: "artifact_rejected"}
	}
	if err != nil {
		return 0, &completeFailure{status: http.StatusInternalServerError, code: "invalid_message"}
	}
	gotSize, err := h.store.ObjectSize(ctx, dst)
	if errors.Is(err, storage.ErrNotFound) {
		return 0, h.rejectCopied(ctx, tx, dst)
	}
	if err != nil {
		return 0, &completeFailure{status: http.StatusInternalServerError, code: "invalid_message"}
	}
	if gotSize > maxOut || gotSize != size {
		return 0, h.rejectCopied(ctx, tx, dst)
	}
	rc, err := h.store.Download(ctx, dst)
	if errors.Is(err, storage.ErrNotFound) {
		return 0, h.rejectCopied(ctx, tx, dst)
	}
	if err != nil {
		return 0, &completeFailure{status: http.StatusInternalServerError, code: "invalid_message"}
	}
	sum, err := hashReader(rc)
	if err != nil {
		return 0, &completeFailure{status: http.StatusInternalServerError, code: "invalid_message"}
	}
	if sum != sha {
		return 0, h.rejectCopied(ctx, tx, dst)
	}
	return gotSize, nil
}

func (h *Handler) attemptLiveNow(ctx context.Context, tx pgx.Tx, attemptID, sessionID uuid.UUID, dsts ...string) error {
	var live bool
	err := tx.QueryRow(ctx, `
		SELECT state = 'active'
			AND released_at IS NULL
			AND attempt_deadline > clock_timestamp()
			AND lease_expires_at > clock_timestamp()
		FROM worker_slot_holds
		WHERE id = $1 AND session_id = $2`, attemptID, sessionID).Scan(&live)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read attempt: %w", err)
	}
	if err == nil && live {
		return nil
	}
	if err := h.removeUnnamedCopies(ctx, tx, dsts...); err != nil {
		return err
	}
	return errAttemptInactive
}

func (h *Handler) removeUnnamedCopies(ctx context.Context, tx pgx.Tx, dsts ...string) error {
	for _, dst := range dsts {
		if err := h.removeUnnamedCopy(ctx, tx, dst); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) acceptCopied(ctx context.Context, tx pgx.Tx, jobID, attemptID uuid.UUID, pieces []completeArtifact, dsts []string, gotSizes []int64) error {
	out := jobs.Output{
		Ordinal:     0,
		ObjectKey:   dsts[0],
		Format:      pieces[0].Format,
		SizeBytes:   gotSizes[0],
		ContentType: pieces[0].ContentType,
	}
	if len(pieces) == 1 {
		return h.jobsRepo.AcceptProtocol(ctx, tx, jobID, attemptID, out)
	}
	report := jobs.Output{
		Ordinal:     1,
		ObjectKey:   dsts[1],
		Filename:    "report.json",
		Format:      pieces[1].Format,
		SizeBytes:   gotSizes[1],
		ContentType: pieces[1].ContentType,
		Role:        jobs.OutputRoleReport,
	}
	return h.jobsRepo.AcceptProtocol(ctx, tx, jobID, attemptID, out, report)
}

func matchCompleteArtifacts(engine string, arts []completeArtifact, wantFormat string, maxOut int64) (completeArtifact, *completeArtifact, bool) {
	if engine != convert.EngineProbe {
		if len(arts) != 1 || !primaryArtifactMatches(arts[0], wantFormat, maxOut) {
			return completeArtifact{}, nil, false
		}
		return arts[0], nil, true
	}
	if len(arts) != 2 {
		return completeArtifact{}, nil, false
	}
	var primary completeArtifact
	var report completeArtifact
	var havePrimary, haveReport bool
	for _, art := range arts {
		switch art.Role {
		case "primary":
			if havePrimary || !primaryArtifactMatches(art, wantFormat, maxOut) {
				return completeArtifact{}, nil, false
			}
			primary = art
			havePrimary = true
		case "report":
			if haveReport || !reportArtifactListed(art, maxOut) {
				return completeArtifact{}, nil, false
			}
			report = art
			haveReport = true
		default:
			return completeArtifact{}, nil, false
		}
	}
	if !havePrimary || !haveReport {
		return completeArtifact{}, nil, false
	}
	return primary, &report, true
}

func primaryArtifactMatches(art completeArtifact, wantFormat string, maxOut int64) bool {
	if !artifactListed(art) || art.Format != wantFormat || art.ContentType != convert.MIMEType(art.Format) {
		return false
	}
	size, kind := integerToken(art.ByteSize)
	return kind == numOK && size <= maxOut
}

func reportArtifactListed(art completeArtifact, maxOut int64) bool {
	if art.Format != "json" || art.ContentType != convert.MIMEType("json") {
		return false
	}
	ordinal, kind := integerToken(art.Ordinal)
	if kind != numOK || ordinal != 1 || !hex64.MatchString(art.SHA256) {
		return false
	}
	size, kind := integerToken(art.ByteSize)
	return kind == numOK && size >= 0 && size <= maxOut
}

func (h *Handler) rejectCopied(ctx context.Context, tx pgx.Tx, dst string) error {
	if err := h.removeUnnamedCopy(ctx, tx, dst); err != nil {
		return err
	}
	return &completeFailure{status: http.StatusConflict, code: "artifact_rejected"}
}

func (h *Handler) removeUnnamedCopy(ctx context.Context, tx pgx.Tx, dst string) error {
	var named bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_outputs WHERE object_key = $1)`, dst).Scan(&named); err != nil {
		return fmt.Errorf("read output key: %w", err)
	}
	if named {
		return nil
	}
	if err := h.store.Remove(ctx, dst); err != nil {
		return &completeFailure{status: http.StatusInternalServerError, code: "invalid_message"}
	}
	return nil
}

const protocolDefaultOutputBytes int64 = 100 << 20

func protocolOutputLimit(engine, target string) int64 {
	switch engine {
	case convert.EngineCAD:
		switch convert.NormalizeFormat(target) {
		case "svg":
			return convert.CADMaxDXFSVGBytes
		case "stl":
			return convert.CADMaxSTLBytes
		default:
			return protocolDefaultOutputBytes
		}
	case convert.EngineAV:
		switch convert.NormalizeFormat(target) {
		case "mp4", "webm", "mp3", "wav", "m4a", "jpg", "png", "webp":
			return 2 << 30
		default:
			return protocolDefaultOutputBytes
		}
	case convert.EngineTectonic:
		return convert.DefaultTectonicMaxOutputBytes
	case convert.EngineArchive:
		if convert.NormalizeFormat(target) == "zip" {
			return protocolDefaultOutputBytes + (1 << 20)
		}
		return protocolDefaultOutputBytes
	default:
		return protocolDefaultOutputBytes
	}
}

func artifactListed(art completeArtifact) bool {
	if art.Role != "primary" || art.Format == "" || art.ContentType == "" {
		return false
	}
	ordinal, kind := integerToken(art.Ordinal)
	if kind != numOK || ordinal != 0 {
		return false
	}
	if !hex64.MatchString(art.SHA256) {
		return false
	}
	size, kind := integerToken(art.ByteSize)
	ceiling := protocolOutputLimit(convert.EngineCAD, art.Format)
	if av := protocolOutputLimit(convert.EngineAV, art.Format); av > ceiling {
		ceiling = av
	}
	return kind == numOK && size >= 0 && size <= ceiling
}

func parseArtifacts(raw json.RawMessage) ([]completeArtifact, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var arts []completeArtifact
	if err := dec.Decode(&arts); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing json")
		}
		return nil, err
	}
	return arts, nil
}

func hashReader(rc io.ReadCloser) (string, error) {
	defer rc.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, rc); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func (h *Handler) writeControl(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	ids, err := h.canceledAttemptIDs(r.Context(), sessionID)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	draining, err := sessionDraining(r.Context(), h.repo.pool, sessionID)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	writeJSON(w, http.StatusOK, heartbeatWire{ProtocolVersion: ProtocolVersion, Control: controlWire{CancelAttemptIDs: ids, Drain: draining, Terminate: false}})
}

type parsedProgress struct {
	unknown    bool
	percent    int
	stage      *string
	stageIndex *int
	stageCount *int
	eta        *int
	etaNull    bool
}

func parseProgress(req progressRequest) (parsedProgress, string) {
	var parsed parsedProgress
	if len(req.Percent) == 0 {
		return parsed, "invalid_message"
	}
	if string(req.Percent) == "null" {
		parsed.unknown = true
	} else {
		var n int
		if err := json.Unmarshal(req.Percent, &n); err != nil || n < 0 || n > 99 {
			if err == nil && n == 100 {
				return parsed, "invalid_progress"
			}
			return parsed, "invalid_message"
		}
		parsed.percent = n
	}
	if len(req.Stage) != 0 {
		if string(req.Stage) == "null" {
			return parsed, "invalid_message"
		}
		var stage string
		if err := json.Unmarshal(req.Stage, &stage); err != nil || stage == "" {
			return parsed, "invalid_message"
		}
		parsed.stage = &stage
	}
	index, code := optionalInt(req.StageIndex, 1)
	if code != "" {
		return parsed, code
	}
	count, code := optionalInt(req.StageCount, 1)
	if code != "" {
		return parsed, code
	}
	parsed.stageIndex = index
	parsed.stageCount = count
	if index != nil && count != nil && *index > *count {
		return parsed, "invalid_message"
	}
	if len(req.ETASeconds) != 0 && string(req.ETASeconds) == "null" {
		parsed.etaNull = true
	} else if len(req.ETASeconds) != 0 {
		eta, code := optionalInt(req.ETASeconds, 0)
		if code != "" {
			return parsed, code
		}
		parsed.eta = eta
	}
	return parsed, ""
}

func cadJobStage(stage *string, index, count *int) (string, int, int, bool) {
	if stage == nil || index == nil || count == nil {
		return "", 0, 0, false
	}
	if *index < 1 || *count < *index {
		return "", 0, 0, false
	}
	if !slices.Contains(convert.CADStageVocabulary(), *stage) {
		return "", 0, 0, false
	}
	return *stage, *index, *count, true
}

func optionalInt(raw json.RawMessage, min int) (*int, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ""
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil || n < min {
		return nil, "invalid_message"
	}
	return &n, ""
}

func (h *Handler) storeProgress(ctx context.Context, sessionID, attemptID uuid.UUID, parsed parsedProgress) (bool, progressNotice, error) {
	var limited bool
	var notice progressNotice
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if _, err := lockReportingJob(ctx, tx, attemptID, sessionID); err != nil {
			return err
		}
		if err := reportingAttemptLive(ctx, tx, attemptID, sessionID); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			WITH locked AS (
				SELECT id, progress_window_started, progress_in_window
				FROM worker_slot_holds
				WHERE id = $1
				FOR UPDATE
			)
			UPDATE worker_slot_holds AS h
			SET progress_window_started = CASE
					WHEN l.progress_window_started IS NOT NULL
					 AND l.progress_window_started > now() - interval '1 second'
					 AND l.progress_in_window >= 4 THEN h.progress_window_started
					WHEN l.progress_window_started IS NULL
					  OR l.progress_window_started <= now() - interval '1 second' THEN now()
					ELSE h.progress_window_started
				END,
				progress_in_window = CASE
					WHEN l.progress_window_started IS NOT NULL
					 AND l.progress_window_started > now() - interval '1 second'
					 AND l.progress_in_window >= 4 THEN h.progress_in_window
					WHEN l.progress_window_started IS NULL
					  OR l.progress_window_started <= now() - interval '1 second' THEN 1
					ELSE h.progress_in_window + 1
				END
			FROM locked AS l
			WHERE h.id = l.id
			RETURNING (
				l.progress_window_started IS NOT NULL
				AND l.progress_window_started > now() - interval '1 second'
				AND l.progress_in_window >= 4
			)`, attemptID).Scan(&limited)
		if err != nil {
			return fmt.Errorf("progress rate: %w", err)
		}
		if limited {
			return nil
		}
		var jobID uuid.UUID
		var current *int16
		var storedUnknown bool
		var stage *string
		var stageIndex, stageCount, eta *int
		err = tx.QueryRow(ctx, `
			SELECT job_id, progress_percent, COALESCE(progress_unknown, false),
			       stage, stage_index, stage_count, eta_seconds
			FROM worker_slot_holds WHERE id = $1`, attemptID).
			Scan(&jobID, &current, &storedUnknown, &stage, &stageIndex, &stageCount, &eta)
		if err != nil {
			return fmt.Errorf("read progress: %w", err)
		}
		percent := current
		unknown := storedUnknown
		advanced := false
		if parsed.unknown {
			unknown = true
		} else if current == nil || parsed.percent >= int(*current) {
			value := int16(parsed.percent)
			percent = &value
			unknown = false
			advanced = true
		}
		if parsed.stage != nil {
			stage = parsed.stage
		}
		if parsed.stageIndex != nil {
			stageIndex = parsed.stageIndex
		}
		if parsed.stageCount != nil {
			stageCount = parsed.stageCount
		}
		if parsed.etaNull {
			eta = nil
		} else if parsed.eta != nil {
			eta = parsed.eta
		}
		if _, err := tx.Exec(ctx, `
			UPDATE worker_slot_holds
			SET progress_percent = $2, progress_unknown = $3,
			    stage = $4, stage_index = $5, stage_count = $6, eta_seconds = $7
			WHERE id = $1`, attemptID, percent, unknown, stage, stageIndex, stageCount, eta); err != nil {
			return fmt.Errorf("store progress: %w", err)
		}
		if parsed.unknown {
			if _, err := tx.Exec(ctx, `
				UPDATE jobs
				SET progress_pct = NULL, progress_updated_at = now()
				WHERE id = $1 AND status = 'active'
				  AND workflow_snapshot_id IS NULL`, jobID); err != nil {
				return fmt.Errorf("store job progress: %w", err)
			}
		} else if advanced {
			var previous *int16
			if err := tx.QueryRow(ctx, `SELECT progress_pct FROM jobs WHERE id = $1`, jobID).Scan(&previous); err != nil {
				return fmt.Errorf("read job progress: %w", err)
			}
			var callback *string
			var hook bool
			var correlation *string
			err := tx.QueryRow(ctx, `
				UPDATE jobs
				SET progress_pct = $2, progress_updated_at = now()
				WHERE id = $1 AND status = 'active'
				  AND workflow_snapshot_id IS NULL
				  AND (progress_pct IS NULL OR progress_pct <= $2)
				RETURNING callback_url, progress_webhook, correlation_id`, jobID, parsed.percent).
				Scan(&callback, &hook, &correlation)
			if errors.Is(err, pgx.ErrNoRows) {
				err = nil
			}
			if err != nil {
				return fmt.Errorf("store job progress: %w", err)
			}
			if hook && callback != nil && *callback != "" && (previous == nil || int(*previous) < parsed.percent) {
				notice = progressNotice{
					fire:        true,
					jobID:       jobID,
					callbackURL: *callback,
					percent:     parsed.percent,
				}
				if correlation != nil {
					notice.correlationID = *correlation
				}
				if !parsed.etaNull {
					notice.eta = parsed.eta
				}
			}
		}
		if stageName, stageAt, stageN, ok := cadJobStage(stage, stageIndex, stageCount); ok {
			if _, err := tx.Exec(ctx, `
				UPDATE jobs
				SET stage = $2, stage_index = $3, stage_count = $4
				WHERE id = $1 AND status = 'active' AND engine = $5
				  AND workflow_snapshot_id IS NULL
				  AND (stage_index IS NULL OR stage_index < $3)`,
				jobID, stageName, stageAt, stageN, convert.EngineCAD); err != nil {
				return fmt.Errorf("store job stage: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return false, progressNotice{}, err
	}
	return limited, notice, nil
}

func (h *Handler) failLiveAttempt(ctx context.Context, sessionID, attemptID uuid.UUID, code, message string) error {
	return pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		jobID, err := lockReportingJob(ctx, tx, attemptID, sessionID)
		if err != nil {
			return err
		}
		if err := reportingAttemptLive(ctx, tx, attemptID, sessionID); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&status); err != nil {
			return err
		}
		if status != jobs.StatusQueued && status != jobs.StatusActive {
			return errAttemptInactive
		}
		return h.jobsRepo.FailProtocolIn(ctx, tx, jobID, code, message)
	})
}

func (h *Handler) requeueLiveAttempt(ctx context.Context, sessionID, attemptID uuid.UUID, message string) error {
	return pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		jobID, err := lockReportingJob(ctx, tx, attemptID, sessionID)
		if err != nil {
			return err
		}
		if err := reportingAttemptLive(ctx, tx, attemptID, sessionID); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&status); err != nil {
			return err
		}
		if status != jobs.StatusActive {
			return errAttemptInactive
		}
		err = h.jobsRepo.RequeueProtocolIn(ctx, tx, jobID, func(failed int) (time.Time, error) {
			delay, err := attemptJitter(failed)
			if err != nil {
				return time.Time{}, err
			}
			if delay <= 0 {
				return time.Time{}, nil
			}
			return time.Now().Add(time.Duration(delay) * time.Second), nil
		})
		if errors.Is(err, jobs.ErrProtocolBudget) {
			return h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "reconciler_exhausted", message)
		}
		return err
	})
}

func lockReportingJob(ctx context.Context, tx pgx.Tx, attemptID, sessionID uuid.UUID) (uuid.UUID, error) {
	var jobID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT job_id FROM worker_slot_holds
		WHERE id = $1 AND session_id = $2 AND job_id IS NOT NULL`, attemptID, sessionID).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errAttemptInactive
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read attempt job: %w", err)
	}
	var locked uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errAttemptInactive
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock job: %w", err)
	}
	err = tx.QueryRow(ctx, `
		SELECT job_id FROM worker_slot_holds
		WHERE id = $1 AND session_id = $2
		FOR UPDATE`, attemptID, sessionID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errAttemptInactive
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock attempt: %w", err)
	}
	if locked != jobID {
		return uuid.Nil, errAttemptInactive
	}
	return jobID, nil
}

func reportingAttemptLive(ctx context.Context, tx pgx.Tx, attemptID, sessionID uuid.UUID) error {
	var live bool
	err := tx.QueryRow(ctx, `
		SELECT
			h.state = 'active'
			AND h.released_at IS NULL
			AND COALESCE(h.attempt_deadline > now(), false)
			AND COALESCE(h.lease_expires_at > now(), false)
			AND NOT EXISTS (
				SELECT 1 FROM jobs j
				WHERE j.id = h.job_id AND j.status IN ('canceled', 'failed', 'done')
			)
		FROM worker_slot_holds h
		WHERE h.id = $1 AND h.session_id = $2`, attemptID, sessionID).Scan(&live)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
		return errAttemptInactive
	}
	if err != nil {
		return fmt.Errorf("read attempt: %w", err)
	}
	return nil
}

func (h *Handler) stopAttempt(ctx context.Context, sessionID, attemptID uuid.UUID, reason string) error {
	return pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		jobID, err := lockReportingJob(ctx, tx, attemptID, sessionID)
		if err != nil {
			return err
		}
		var state string
		var released bool
		err = tx.QueryRow(ctx, `
			SELECT state, released_at IS NOT NULL
			FROM worker_slot_holds
			WHERE id = $1 AND session_id = $2`, attemptID, sessionID).Scan(&state, &released)
		if errors.Is(err, pgx.ErrNoRows) {
			return errAttemptInactive
		}
		if err != nil {
			return err
		}
		if reason == "cancel" && state == "canceled" {
			if _, err := tx.Exec(ctx, `
				UPDATE worker_slot_holds SET state = 'stopped' WHERE id = $1`, attemptID); err != nil {
				return err
			}
			return nil
		}
		if reason == "lease_lost" {
			if state != "active" || released {
				return errAttemptInactive
			}
		} else if err := reportingAttemptLive(ctx, tx, attemptID, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE worker_slot_holds
			SET state = 'stopped', released_at = now()
			WHERE id = $1`, attemptID); err != nil {
			return err
		}
		return h.jobsRepo.QueueAfterStop(ctx, tx, jobID)
	})
}

func protocolFields(w http.ResponseWriter, version json.Number, ext json.RawMessage) bool {
	if !protocolVersionOK(w, version) {
		return false
	}
	if !extensionsObject(ext) {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return false
	}
	return true
}

func (h *Handler) attemptOpen(ctx context.Context, attemptID, sessionID uuid.UUID, requireLease bool) (bool, error) {
	var live bool
	err := h.repo.pool.QueryRow(ctx, `
		SELECT
			h.state = 'active'
			AND h.released_at IS NULL
			AND COALESCE(h.attempt_deadline > now(), false)
			AND (NOT $3 OR COALESCE(h.lease_expires_at > now(), false))
			AND NOT EXISTS (
				SELECT 1 FROM jobs j
				WHERE j.id = h.job_id AND j.status IN ('canceled', 'failed', 'done')
			)
		FROM worker_slot_holds h
		WHERE h.id = $1 AND h.session_id = $2`, attemptID, sessionID, requireLease).Scan(&live)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read attempt: %w", err)
	}
	return live, nil
}

func (h *Handler) servePull(ctx context.Context, sess storedSession, pullID uuid.UUID, bodyHash string, freeSlots, waitSeconds int64) ([]byte, error) {
	wait := waitSeconds
	if wait < 0 {
		wait = 0
	}
	if wait > 20 {
		wait = 20
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	limited, err := h.consumePullRate(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	if limited {
		return nil, &pullStatus{status: http.StatusTooManyRequests, code: "rate_limited"}
	}
	skipped := []uuid.UUID{}
	reserved := false
	for len(skipped) < 32 {
		storeEmpty := wait == 0 || freeSlots == 0 || !time.Now().Before(deadline)
		step, err := h.pullCandidate(ctx, sess, pullID, bodyHash, freeSlots, skipped, reserved, storeEmpty)
		if errors.Is(err, errDedupLost) {
			return h.replayPull(ctx, sess.ID, pullID, bodyHash, deadline)
		}
		if err != nil {
			return nil, err
		}
		if step.done {
			return step.body, nil
		}
		reserved = true
		if step.skipID != uuid.Nil {
			skipped = append(skipped, step.skipID)
			continue
		}
		if err := sleepPull(ctx, pullPause(deadline)); err != nil {
			return nil, err
		}
	}
	return h.storeEmptyPull(ctx, sess, pullID, bodyHash)
}

func (h *Handler) storeEmptyPull(ctx context.Context, sess storedSession, pullID uuid.UUID, bodyHash string) ([]byte, error) {
	var body []byte
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := lockOpenSession(ctx, tx, sess.ID); err != nil {
			return err
		}
		cancelIDs, err := canceledAttempts(ctx, tx, sess.ID)
		if err != nil {
			return err
		}
		draining, err := sessionDraining(ctx, tx, sess.ID)
		if err != nil {
			return err
		}
		wire := emptyPull(cancelIDs)
		wire.Control.Drain = draining
		encoded, err := encodeWire(wire)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE worker_request_dedup
			SET response_body = $3, attempt_id = NULL
			WHERE session_id = $1 AND dedup_key = $2 AND response_body IS NULL`,
			sess.ID, pullID.String(), encoded)
		if err != nil {
			return fmt.Errorf("store pull: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return errDedupLost
		}
		body = encoded
		return nil
	})
	if errors.Is(err, errDedupLost) {
		return h.replayPull(ctx, sess.ID, pullID, bodyHash, time.Now())
	}
	return body, err
}

type pullStep struct {
	done   bool
	body   []byte
	skipID uuid.UUID
}

func lockOpenSession(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) error {
	var closed, expired bool
	err := tx.QueryRow(ctx, `
		SELECT closed_at IS NOT NULL, expires_at <= now()
		FROM worker_sessions
		WHERE id = $1
		FOR UPDATE`, sessionID).Scan(&closed, &expired)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (closed || expired)) {
		return &pullStatus{status: http.StatusUnauthorized, code: "session_closed"}
	}
	if err != nil {
		return fmt.Errorf("lock session: %w", err)
	}
	return nil
}

func (h *Handler) consumePullRate(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	var limited bool
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			WITH locked AS (
				SELECT id, pull_window_started, pulls_in_window
				FROM worker_sessions
				WHERE id = $1
				FOR UPDATE
			)
			UPDATE worker_sessions AS s
			SET pull_window_started = CASE
					WHEN l.pull_window_started IS NOT NULL
					 AND l.pull_window_started > now() - interval '1 second'
					 AND l.pulls_in_window >= 2 THEN s.pull_window_started
					WHEN l.pull_window_started IS NULL
					  OR l.pull_window_started <= now() - interval '1 second' THEN now()
					ELSE s.pull_window_started
				END,
				pulls_in_window = CASE
					WHEN l.pull_window_started IS NOT NULL
					 AND l.pull_window_started > now() - interval '1 second'
					 AND l.pulls_in_window >= 2 THEN s.pulls_in_window
					WHEN l.pull_window_started IS NULL
					  OR l.pull_window_started <= now() - interval '1 second' THEN 1
					ELSE s.pulls_in_window + 1
				END
			FROM locked AS l
			WHERE s.id = l.id
			RETURNING (
				l.pull_window_started IS NOT NULL
				AND l.pull_window_started > now() - interval '1 second'
				AND l.pulls_in_window >= 2
			)`, sessionID).Scan(&limited)
	})
	if err != nil {
		return false, fmt.Errorf("pull rate: %w", err)
	}
	return limited, nil
}

func (h *Handler) pullCandidate(ctx context.Context, sess storedSession, pullID uuid.UUID, bodyHash string, freeSlots int64, skipped []uuid.UUID, reserved, storeEmpty bool) (pullStep, error) {
	var step pullStep
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := lockOpenSession(ctx, tx, sess.ID); err != nil {
			return err
		}
		if !reserved {
			tag, err := tx.Exec(ctx, `
				INSERT INTO worker_request_dedup (session_id, dedup_key, body_sha256)
				VALUES ($1, $2, $3)
				ON CONFLICT (session_id, dedup_key) DO NOTHING`,
				sess.ID, pullID.String(), bodyHash)
			if err != nil {
				return fmt.Errorf("reserve pull: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return errDedupLost
			}
		}
		decision, err := h.claimOnce(ctx, tx, sess, freeSlots, skipped)
		if err != nil {
			return err
		}
		if decision.skipID != uuid.Nil {
			step.skipID = decision.skipID
			return nil
		}
		if decision.attemptID == uuid.Nil && !decision.emptyNow && !storeEmpty {
			return nil
		}
		encoded, err := encodeWire(decision.wire)
		if err != nil {
			return err
		}
		if err := writePullResponse(ctx, tx, sess.ID, pullID, decision.attemptID, encoded); err != nil {
			return err
		}
		step.done = true
		step.body = encoded
		return nil
	})
	return step, err
}

func pullPause(deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	if remaining < 25*time.Millisecond {
		return remaining
	}
	return 25 * time.Millisecond
}

func (h *Handler) replayPull(ctx context.Context, sessionID, pullID uuid.UUID, bodyHash string, deadline time.Time) ([]byte, error) {
	for {
		row, err := h.readPull(ctx, sessionID, pullID)
		if err != nil {
			return nil, err
		}
		if row.hash != bodyHash {
			return nil, &pullStatus{status: http.StatusConflict, code: "pull_conflict"}
		}
		if row.response != nil {
			if row.attemptID == nil {
				return row.response, nil
			}
			active, err := h.attemptActive(ctx, *row.attemptID)
			if err != nil {
				return nil, err
			}
			if !active {
				return nil, &pullStatus{status: http.StatusConflict, code: "attempt_not_active"}
			}
			return row.response, nil
		}
		if !time.Now().Before(deadline) {
			return nil, &pullStatus{status: http.StatusConflict, code: "attempt_not_active"}
		}
		pause := 25 * time.Millisecond
		if remaining := time.Until(deadline); remaining < pause {
			pause = remaining
		}
		if err := sleepPull(ctx, pause); err != nil {
			return nil, err
		}
	}
}

type storedPull struct {
	hash      string
	response  []byte
	attemptID *uuid.UUID
}

func (h *Handler) readPull(ctx context.Context, sessionID, pullID uuid.UUID) (storedPull, error) {
	var row storedPull
	var response []byte
	err := h.repo.pool.QueryRow(ctx, `
		SELECT body_sha256, response_body, attempt_id
		FROM worker_request_dedup
		WHERE session_id = $1 AND dedup_key = $2`, sessionID, pullID.String()).
		Scan(&row.hash, &response, &row.attemptID)
	if err != nil {
		return storedPull{}, fmt.Errorf("read pull: %w", err)
	}
	if response != nil {
		row.response = response
	}
	return row, nil
}

func (h *Handler) attemptActive(ctx context.Context, attemptID uuid.UUID) (bool, error) {
	var active bool
	err := h.repo.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM worker_slot_holds
			WHERE id = $1
			  AND state = 'active'
			  AND released_at IS NULL
			  AND attempt_deadline > now()
		)`, attemptID).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("read attempt: %w", err)
	}
	return active, nil
}

type claimDecision struct {
	wire      pullWire
	attemptID uuid.UUID
	skipID    uuid.UUID
	emptyNow  bool
}

func (h *Handler) claimOnce(ctx context.Context, tx pgx.Tx, sess storedSession, freeSlots int64, skipped []uuid.UUID) (claimDecision, error) {
	cancelIDs, err := canceledAttempts(ctx, tx, sess.ID)
	if err != nil {
		return claimDecision{}, err
	}
	draining, err := sessionDraining(ctx, tx, sess.ID)
	if err != nil {
		return claimDecision{}, err
	}
	if draining {
		wire := emptyPull(cancelIDs)
		wire.Control.Drain = true
		return claimDecision{wire: wire, emptyNow: true}, nil
	}
	if freeSlots == 0 {
		return claimDecision{wire: emptyPull(cancelIDs), emptyNow: true}, nil
	}
	var capabilityID string
	var capabilityVersion int
	var legacyEngine *string
	err = tx.QueryRow(ctx, `
		SELECT id, version, legacy_engine FROM worker_capabilities WHERE worker_id = $1`, sess.WorkerID).
		Scan(&capabilityID, &capabilityVersion, &legacyEngine)
	if errors.Is(err, pgx.ErrNoRows) {
		return claimDecision{wire: emptyPull(cancelIDs), emptyNow: true}, nil
	}
	if err != nil {
		return claimDecision{}, fmt.Errorf("read capability: %w", err)
	}
	var maxSlots int
	if err := tx.QueryRow(ctx, `
		SELECT max_slots FROM worker_identities WHERE worker_id = $1 FOR UPDATE`, sess.WorkerID).Scan(&maxSlots); err != nil {
		return claimDecision{}, fmt.Errorf("lock identity: %w", err)
	}
	var held int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM worker_slot_holds
		WHERE owner_key = $1 AND released_at IS NULL`, sess.WorkerID.String()).Scan(&held); err != nil {
		return claimDecision{}, fmt.Errorf("count slots: %w", err)
	}
	if held >= maxSlots || capabilityID == "octoconv.convert.imagepdf" {
		return claimDecision{wire: emptyPull(cancelIDs), emptyNow: true}, nil
	}
	if skipped == nil {
		skipped = []uuid.UUID{}
	}
	engine := ""
	filterCap := false
	if legacyEngine != nil && *legacyEngine != "" {
		engine = *legacyEngine
		filterCap = true
	}
	budgets, fallbackSeconds := h.attemptBudgetArgs()
	var jobID uuid.UUID
	var storedStep *uuid.UUID
	var clientID *uuid.UUID
	var targetFormat *string
	var snapshotID *uuid.UUID
	var attemptDeadline, leaseExpires, linkExpires time.Time
	var leaseOK, linkOK bool
	err = tx.QueryRow(ctx, `
		SELECT id, step_id, client_id, target_format, workflow_snapshot_id,
		       LEAST(protocol_deadline, now() + make_interval(secs => `+claimAttemptBudgetSQL+`)),
		       LEAST(now() + interval '30 seconds', LEAST(protocol_deadline, now() + make_interval(secs => `+claimAttemptBudgetSQL+`))),
		       LEAST(now() + interval '30 seconds', LEAST(protocol_deadline, now() + make_interval(secs => `+claimAttemptBudgetSQL+`)))
		           > now() + interval '5 seconds',
		       LEAST(
		           `+fileLinkIntervalSQL()+`,
		           LEAST(protocol_deadline, now() + make_interval(secs => `+claimAttemptBudgetSQL+`)),
		           protocol_deadline
		       ),
		       LEAST(
		           `+fileLinkIntervalSQL()+`,
		           LEAST(protocol_deadline, now() + make_interval(secs => `+claimAttemptBudgetSQL+`)),
		           protocol_deadline
		       ) > now() + interval '5 seconds'
		FROM jobs
		WHERE dispatch_owner = 'protocol'
		  AND status = 'queued'
		  AND (workflow_snapshot_id IS NULL OR step_id IS NOT NULL)
		  AND capability_id = $1
		  AND capability_version = $2
		  AND (not_before IS NULL OR not_before <= now())
		  AND protocol_deadline > now()
		  AND NOT (id = ANY($5))
		  AND (
		      step_id IS NULL OR (
		          SELECT count(*) FROM worker_slot_holds h
		          WHERE h.step_id = jobs.step_id
		            AND h.state = 'failed'
		            AND (jobs.manual_retry_at IS NULL OR h.released_at > jobs.manual_retry_at)
		      ) < 5
		  )
		  AND (
		      NOT $6 OR NOT EXISTS (
		          SELECT 1 FROM client_limits cl
		          WHERE cl.client_id = jobs.client_id
		            AND cl.engine = $7
		            AND cl.max_concurrent IS NOT NULL
		            AND (
		                SELECT count(*) FROM jobs AS peer
		                WHERE peer.client_id = jobs.client_id
		                  AND peer.engine = $7
		                  AND peer.status = 'active'
		            ) >= cl.max_concurrent
		      )
		  )
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, capabilityID, capabilityVersion, budgets, fallbackSeconds, skipped, filterCap, engine).Scan(
		&jobID, &storedStep, &clientID, &targetFormat, &snapshotID, &attemptDeadline, &leaseExpires, &leaseOK, &linkExpires, &linkOK)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!leaseOK || !linkOK)) {
		return claimDecision{wire: emptyPull(cancelIDs)}, nil
	}
	if err != nil {
		return claimDecision{}, fmt.Errorf("claim job: %w", err)
	}
	if clientID != nil && legacyEngine != nil && *legacyEngine != "" {
		skip, err := h.clientAtCap(ctx, tx, *clientID, *legacyEngine, jobID)
		if err != nil {
			return claimDecision{}, err
		}
		if skip {
			return claimDecision{skipID: jobID}, nil
		}
	}
	outputFormat := ""
	if targetFormat != nil {
		outputFormat = *targetFormat
	}
	var inputKey, inputFormat string
	var inputOK bool
	var sources []storedJobInput
	archiveInputs := engine == convert.EngineArchive
	tectonicInputs := engine == convert.EngineTectonic
	if snapshotID != nil {
		doc, index, err := workflowStepIndex(ctx, tx, jobID, storedStep)
		if err != nil {
			return claimDecision{}, err
		}
		matched := index >= 0
		var step workflowSnapStep
		if matched {
			step = doc.Steps[index]
			matched = step.CapabilityID == capabilityID && step.CapabilityVersion == capabilityVersion && step.TargetFormat != ""
		}
		if !matched {
			if err := h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", "workflow input missing"); err != nil {
				return claimDecision{}, err
			}
			return claimDecision{skipID: jobID}, nil
		}
		outputFormat = step.TargetFormat
		switch {
		case index == 0 && archiveInputs:
			sources, inputOK, err = loadArchiveSources(ctx, tx, jobID)
			if inputOK {
				inputKey, inputFormat = sources[0].Key, sources[0].Format
			}
		case index == 0 && tectonicInputs:
			sources, inputOK, err = loadTectonicSources(ctx, tx, jobID)
			if inputOK {
				inputKey, inputFormat = sources[0].Key, sources[0].Format
			}
		case index == 0:
			inputKey, inputFormat, inputOK, err = oneJobInput(ctx, tx, jobID)
		default:
			inputKey, inputFormat, inputOK, err = workflowResultInput(ctx, tx, jobID, doc.Steps[index-1].StepID)
		}
		if err != nil {
			return claimDecision{}, err
		}
		if !inputOK {
			if err := h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", "workflow input missing"); err != nil {
				return claimDecision{}, err
			}
			return claimDecision{skipID: jobID}, nil
		}
	} else {
		if outputFormat == "" {
			return claimDecision{skipID: jobID}, nil
		}
		var err error
		switch {
		case archiveInputs:
			sources, inputOK, err = loadArchiveSources(ctx, tx, jobID)
			if inputOK {
				inputKey, inputFormat = sources[0].Key, sources[0].Format
			}
		case tectonicInputs:
			sources, inputOK, err = loadTectonicSources(ctx, tx, jobID)
			if inputOK {
				inputKey, inputFormat = sources[0].Key, sources[0].Format
			}
		default:
			inputKey, inputFormat, inputOK, err = oneJobInput(ctx, tx, jobID)
		}
		if err != nil {
			return claimDecision{}, err
		}
		if !inputOK {
			return claimDecision{skipID: jobID}, nil
		}
	}
	if _, ok := linkSignTTL(linkExpires); !ok {
		return claimDecision{wire: emptyPull(cancelIDs)}, nil
	}
	var earlyOpts []byte
	var earlyEngine string
	if err := tx.QueryRow(ctx, `SELECT options, engine FROM jobs WHERE id = $1`, jobID).Scan(&earlyOpts, &earlyEngine); err != nil {
		return claimDecision{}, fmt.Errorf("read job options: %w", err)
	}
	earlyEx, earlyExtracting, err := parseExtraction(earlyOpts)
	if err != nil {
		return claimDecision{}, err
	}
	var unpackNames []string
	if snapshotID == nil && earlyExtracting && earlyEx.Phase == "unpack" && earlyEngine == convert.EngineArchive {
		names, nameErr := h.archiveMemberNames(ctx, sources)
		if nameErr != nil {
			if errors.Is(nameErr, errBadArchive) {
				if err := h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", "invalid archive member"); err != nil {
					return claimDecision{}, err
				}
				return claimDecision{skipID: jobID}, nil
			}
			return claimDecision{}, nameErr
		}
		unpackNames = names
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT claim_attempt`); err != nil {
		return claimDecision{}, fmt.Errorf("claim savepoint: %w", err)
	}
	rollbackClaim := func() error {
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT claim_attempt`); err != nil {
			return fmt.Errorf("rollback claim: %w", err)
		}
		return nil
	}
	stepID := uuid.New()
	if storedStep != nil {
		stepID = *storedStep
	}
	attemptID := uuid.New()
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		return claimDecision{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO worker_attempt_credentials (
			attempt_id, token_hash, session_id, capability_id, capability_version
		) VALUES ($1, $2, $3, $4, $5)`,
		attemptID, tokenHash, sess.ID, capabilityID, capabilityVersion); err != nil {
		return claimDecision{}, fmt.Errorf("insert attempt credential: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO worker_slot_holds (
			id, owner_key, session_id, job_id, step_id, capability_id, capability_version,
			lease_expires_at, attempt_deadline, state
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, 'active'
		)`,
		attemptID, sess.WorkerID.String(), sess.ID, jobID, stepID, capabilityID, capabilityVersion,
		leaseExpires, attemptDeadline); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if err := rollbackClaim(); err != nil {
				return claimDecision{}, err
			}
			return claimDecision{wire: emptyPull(cancelIDs), emptyNow: true}, nil
		}
		return claimDecision{}, fmt.Errorf("insert slot hold: %w", err)
	}
	var optsRaw []byte
	var jobEngine string
	if err := tx.QueryRow(ctx, `SELECT options, engine FROM jobs WHERE id = $1`, jobID).Scan(&optsRaw, &jobEngine); err != nil {
		return claimDecision{}, fmt.Errorf("read job options: %w", err)
	}
	ex, extracting, err := parseExtraction(optsRaw)
	if err != nil {
		return claimDecision{}, err
	}
	if extracting && (ex.Phase == "unpack" || ex.Phase == "convert") {
		if err := h.jobsRepo.ActivateProtocolPreserveProgress(ctx, tx, jobID, stepID); err != nil {
			return claimDecision{}, err
		}
	} else if err := h.jobsRepo.ActivateProtocol(ctx, tx, jobID, stepID); err != nil {
		return claimDecision{}, err
	}
	getTTL, ok := linkSignTTL(linkExpires)
	if !ok {
		if err := rollbackClaim(); err != nil {
			return claimDecision{}, err
		}
		return claimDecision{wire: emptyPull(cancelIDs)}, nil
	}
	if len(sources) == 0 {
		sources = []storedJobInput{{Key: inputKey, Format: inputFormat}}
	}
	hrefs := make([]string, len(sources))
	for i, src := range sources {
		href, err := h.store.PresignGet(ctx, src.Key, getTTL)
		if err != nil {
			return claimDecision{}, fmt.Errorf("presign input: %w", err)
		}
		hrefs[i] = href
	}
	putTTL, ok := linkSignTTL(linkExpires)
	if !ok {
		if err := rollbackClaim(); err != nil {
			return claimDecision{}, err
		}
		return claimDecision{wire: emptyPull(cancelIDs)}, nil
	}
	putURL, err := h.store.PresignPut(ctx, storage.AttemptOutputKey(attemptID, 0), putTTL)
	if err != nil {
		return claimDecision{}, fmt.Errorf("presign output: %w", err)
	}
	outputs := []fileLinkWire{{
		Role: "primary", Ordinal: 0, Format: outputFormat, Href: putURL, ExpiresAt: "",
	}}
	if engine == convert.EngineProbe {
		reportURL, err := h.store.PresignPut(ctx, storage.AttemptOutputKey(attemptID, 1), putTTL)
		if err != nil {
			return claimDecision{}, fmt.Errorf("presign report: %w", err)
		}
		outputs = append(outputs, fileLinkWire{
			Role: "report", Ordinal: 1, Format: "json", Href: reportURL,
		})
	}
	if packOutputsAreZip(jobEngine, snapshotID, optsRaw) {
		outputFormat = "zip"
		if len(outputs) > 0 {
			outputs[0].Format = "zip"
		}
	}
	if extracting && ex.Phase == "unpack" && jobEngine == convert.EngineArchive {
		names := unpackNames
		if len(names) == 0 {
			var nameErr error
			names, nameErr = h.archiveMemberNames(ctx, sources)
			if nameErr != nil {
				if errors.Is(nameErr, errBadArchive) {
					if err := h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", "invalid archive member"); err != nil {
						return claimDecision{}, err
					}
					return claimDecision{skipID: jobID}, nil
				}
				return claimDecision{}, nameErr
			}
		}
		outputs = make([]fileLinkWire, len(names))
		for i := range names {
			putURL, err := h.store.PresignPut(ctx, storage.AttemptOutputKey(attemptID, i), putTTL)
			if err != nil {
				return claimDecision{}, fmt.Errorf("presign output: %w", err)
			}
			outputs[i] = fileLinkWire{Role: "primary", Ordinal: i, Format: ex.Source, Href: putURL}
		}
		entries := make([]extractionEntry, len(names))
		for i, name := range names {
			entries[i] = extractionEntry{Name: name}
		}
		ex.Entries = entries
		raw, err := marshalExtraction(ex)
		if err != nil {
			return claimDecision{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET options = $2::jsonb WHERE id = $1`, jobID, string(raw)); err != nil {
			return claimDecision{}, fmt.Errorf("write archive extraction: %w", err)
		}
		optsRaw = raw
	}
	parameters, err := assignmentParameters(optsRaw)
	if err != nil {
		return claimDecision{}, err
	}
	if extracting && ex.Phase == "convert" {
		parameters, err = assignmentParameters(pairParameters(ex))
		if err != nil {
			return claimDecision{}, err
		}
	}
	expiresAt := linkExpires.UTC().Format(time.RFC3339Nano)
	for i := range outputs {
		outputs[i].ExpiresAt = expiresAt
	}
	inputLinks := make([]fileLinkWire, 0, len(sources))
	for i, src := range sources {
		inputLinks = append(inputLinks, fileLinkWire{
			Role: "source", Ordinal: src.Ordinal, Format: src.Format, Href: hrefs[i], ExpiresAt: expiresAt,
		})
	}
	return claimDecision{
		attemptID: attemptID,
		wire: pullWire{
			ProtocolVersion: ProtocolVersion,
			Assignments: []assignmentWire{{
				JobID:             jobID.String(),
				StepID:            stepID.String(),
				AttemptID:         attemptID.String(),
				AttemptToken:      token,
				CapabilityID:      capabilityID,
				CapabilityVersion: capabilityVersion,
				AttemptDeadline:   attemptDeadline.UTC().Format(time.RFC3339Nano),
				LeaseExpiresAt:    leaseExpires.UTC().Format(time.RFC3339Nano),
				Limits:            assignmentLimits{MaxOutputBytes: int(protocolOutputLimit(engine, outputFormat))},
				Parameters:        parameters,
				Inputs:            inputLinks,
				Outputs:           outputs,
			}},
			Control: controlWire{CancelAttemptIDs: cancelIDs, Drain: false, Terminate: false},
		},
	}, nil
}

func linkSignTTL(deadline time.Time) (time.Duration, bool) {
	now := time.Now()
	if !deadline.After(now.Add(5 * time.Second)) {
		return 0, false
	}
	ttl := deadline.Sub(now).Truncate(time.Second)
	if ttl < time.Second {
		return 0, false
	}
	return ttl, true
}

type workflowSnapStep struct {
	StepID            uuid.UUID `json:"step_id"`
	CapabilityID      string    `json:"capability_id"`
	CapabilityVersion int       `json:"capability_version"`
	SourceFormat      string    `json:"source_format"`
	TargetFormat      string    `json:"target_format"`
	LegacyEngine      string    `json:"legacy_engine"`
}

type workflowSnapDoc struct {
	Steps []workflowSnapStep `json:"steps"`
}

func workflowStepIndex(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, stepID *uuid.UUID) (workflowSnapDoc, int, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT document FROM workflow_snapshots WHERE job_id = $1`, jobID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflowSnapDoc{}, -1, nil
	}
	if err != nil {
		return workflowSnapDoc{}, -1, fmt.Errorf("read workflow snapshot: %w", err)
	}
	var doc workflowSnapDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return workflowSnapDoc{}, -1, fmt.Errorf("decode workflow snapshot: %w", err)
	}
	if stepID == nil {
		return doc, -1, nil
	}
	for i, step := range doc.Steps {
		if step.StepID == *stepID {
			return doc, i, nil
		}
	}
	return doc, -1, nil
}

func workflowResultInput(ctx context.Context, tx pgx.Tx, jobID, stepID uuid.UUID) (string, string, bool, error) {
	var n int
	var key, format string
	err := tx.QueryRow(ctx, `
		SELECT count(*)::int, COALESCE(min(object_key), ''), COALESCE(min(format), '')
		FROM workflow_step_results
		WHERE job_id = $1 AND step_id = $2 AND object_key <> '' AND format <> ''`, jobID, stepID).Scan(&n, &key, &format)
	if err != nil {
		return "", "", false, fmt.Errorf("read workflow result: %w", err)
	}
	if n != 1 {
		return "", "", false, nil
	}
	return key, format, true, nil
}

const (
	claimAttemptBudgetSQL = `COALESCE((($3::jsonb) ->> jobs.engine)::bigint, $4::bigint)`
	FileLinkMaxTTLSeconds = int(convert.CADDefaultEngineTimeout / time.Second)
)

func fileLinkIntervalSQL() string {
	return fmt.Sprintf("now() + make_interval(secs => %d)", FileLinkMaxTTLSeconds)
}

func (h *Handler) attemptBudgetArgs() (string, int64) {
	fallback := int64(h.imageAttemptBudget / time.Second)
	if fallback <= 0 {
		fallback = 120
	}
	seconds := make(map[string]int64)
	if h.engineBudgets != nil {
		for _, engine := range convert.AllEngines() {
			seconds[engine] = fallback
			if budget, ok := h.engineBudgets[engine]; ok && budget > 0 {
				seconds[engine] = int64(budget / time.Second)
			}
		}
	}
	raw, err := json.Marshal(seconds)
	if err != nil {
		return "{}", fallback
	}
	return string(raw), fallback
}

func (h *Handler) admitChangedEngine(ctx context.Context, tx pgx.Tx, clientID *uuid.UUID, engine, nextEngine string, admitted *int64) error {
	if nextEngine == engine || clientID == nil {
		return nil
	}
	var n int64
	if admitted != nil {
		n = *admitted
	}
	return h.jobsRepo.AdmitLocked(ctx, tx, *clientID, nextEngine, n)
}

func recordContinuation(ctx context.Context, tx pgx.Tx, jobID, stepID, attemptID uuid.UUID, objectKey string, art completeArtifact, size int64, next workflowSnapStep) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_step_results (
			job_id, step_id, attempt_id, object_key, format, size_bytes, content_type, sha256
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		jobID, stepID, attemptID, objectKey, art.Format, size, art.ContentType, art.SHA256); err != nil {
		return fmt.Errorf("insert workflow result: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'queued',
		    step_id = $2,
		    capability_id = $3,
		    capability_version = $4,
		    engine = $5,
		    not_before = NULL,
		    progress_pct = NULL,
		    eta_seconds = NULL,
		    stage = NULL,
		    stage_index = NULL,
		    stage_count = NULL
		WHERE id = $1`,
		jobID, next.StepID, next.CapabilityID, next.CapabilityVersion, next.LegacyEngine); err != nil {
		return fmt.Errorf("continue workflow: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE worker_slot_holds
		SET released_at = now()
		WHERE id = $1 AND job_id = $2 AND state = 'active' AND released_at IS NULL`,
		attemptID, jobID)
	if err != nil {
		return fmt.Errorf("release hold: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("release attempt %s: no active hold", attemptID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, $3, NULL)`, jobID, jobs.StatusActive, jobs.StatusQueued); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	return nil
}

type storedJobInput struct {
	Ordinal  int
	Key      string
	Format   string
	Filename string
}

func loadArchiveSources(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) ([]storedJobInput, bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT ordinal, object_key, COALESCE(format, ''), COALESCE(filename, '')
		FROM job_inputs
		WHERE job_id = $1 AND object_key <> ''
		ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, false, fmt.Errorf("read job inputs: %w", err)
	}
	defer rows.Close()
	var scanned []storedJobInput
	seen := map[int]struct{}{}
	for rows.Next() {
		var row storedJobInput
		if err := rows.Scan(&row.Ordinal, &row.Key, &row.Format, &row.Filename); err != nil {
			return nil, false, fmt.Errorf("read job inputs: %w", err)
		}
		if row.Ordinal < 0 || row.Key == "" {
			return nil, false, nil
		}
		if _, dup := seen[row.Ordinal]; dup {
			return nil, false, nil
		}
		seen[row.Ordinal] = struct{}{}
		if strings.TrimSpace(row.Format) == "" {
			row.Format = "bin"
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read job inputs: %w", err)
	}
	if len(scanned) == 0 {
		return nil, false, nil
	}
	return scanned, true, nil
}

func loadTectonicSources(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) ([]storedJobInput, bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT ordinal, object_key, COALESCE(format, ''), COALESCE(filename, '')
		FROM job_inputs
		WHERE job_id = $1 AND object_key <> ''
		ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, false, fmt.Errorf("read job inputs: %w", err)
	}
	defer rows.Close()
	var scanned []storedJobInput
	for rows.Next() {
		var row storedJobInput
		if err := rows.Scan(&row.Ordinal, &row.Key, &row.Format, &row.Filename); err != nil {
			return nil, false, fmt.Errorf("read job inputs: %w", err)
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read job inputs: %w", err)
	}
	links, ok := tectonicSourceRows(scanned)
	return links, ok, nil
}

func tectonicSourceRows(rows []storedJobInput) ([]storedJobInput, bool) {
	if len(rows) == 0 || len(rows) > jobs.MaxInputParts {
		return nil, false
	}
	seen := make(map[int]struct{}, len(rows))
	extras := make([]storedJobInput, 0, len(rows))
	var head *storedJobInput
	for _, row := range rows {
		if row.Ordinal < 0 || row.Key == "" {
			return nil, false
		}
		if _, dup := seen[row.Ordinal]; dup {
			return nil, false
		}
		seen[row.Ordinal] = struct{}{}
		format := strings.TrimSpace(row.Format)
		if row.Ordinal == 0 {
			if format == "" {
				return nil, false
			}
			format = convert.NormalizeFormat(format)
			if format == "" {
				return nil, false
			}
			row.Format = format
			copied := row
			head = &copied
			continue
		}
		if format == "" {
			format = convert.NormalizeFormat(path.Ext(row.Filename))
		} else {
			format = convert.NormalizeFormat(format)
		}
		if format == "" {
			// File links require a format of at least one character.
			format = "bin"
		}
		row.Format = format
		extras = append(extras, row)
	}
	if head == nil {
		return nil, false
	}
	return append([]storedJobInput{*head}, extras...), true
}

func oneJobInput(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) (string, string, bool, error) {
	var n int
	var key, format string
	err := tx.QueryRow(ctx, `
		SELECT count(*)::int, COALESCE(min(object_key), ''), COALESCE(min(format), '')
		FROM job_inputs
		WHERE job_id = $1 AND ordinal = 0 AND object_key <> '' AND format <> ''`, jobID).Scan(&n, &key, &format)
	if err != nil {
		return "", "", false, fmt.Errorf("read job input: %w", err)
	}
	if n != 1 {
		return "", "", false, nil
	}
	return key, format, true, nil
}

func (h *Handler) clientAtCap(ctx context.Context, tx pgx.Tx, clientID uuid.UUID, engine string, jobID uuid.UUID) (bool, error) {
	var maxConcurrent *int
	err := tx.QueryRow(ctx, `
		SELECT max_concurrent FROM client_limits
		WHERE client_id = $1 AND engine = $2`, clientID, engine).Scan(&maxConcurrent)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && maxConcurrent == nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read client limit: %w", err)
	}
	if err := jobs.LockClientEngine(ctx, tx, clientID, engine); err != nil {
		return false, err
	}
	var active int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM jobs
		WHERE client_id = $1 AND engine = $2 AND status = 'active' AND id <> $3`,
		clientID, engine, jobID).Scan(&active); err != nil {
		return false, fmt.Errorf("count active jobs: %w", err)
	}
	return active >= *maxConcurrent, nil
}

func emptyPull(cancelIDs []string) pullWire {
	return pullWire{
		ProtocolVersion: ProtocolVersion,
		Assignments:     []assignmentWire{},
		Control:         controlWire{CancelAttemptIDs: cancelIDs, Drain: false, Terminate: false},
	}
}

const canceledAttemptSQL = `
	SELECT id FROM worker_slot_holds
	WHERE session_id = $1 AND state = 'canceled'
	ORDER BY id`

func (h *Handler) canceledAttemptIDs(ctx context.Context, sessionID uuid.UUID) ([]string, error) {
	rows, err := h.repo.pool.Query(ctx, canceledAttemptSQL, sessionID)
	return collectCanceled(rows, err)
}

func canceledAttempts(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, canceledAttemptSQL, sessionID)
	return collectCanceled(rows, err)
}

func collectCanceled(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, fmt.Errorf("list canceled attempts: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan canceled attempt: %w", err)
		}
		ids = append(ids, id.String())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list canceled attempts: %w", err)
	}
	return ids, nil
}

func writePullResponse(ctx context.Context, tx pgx.Tx, sessionID, pullID, attemptID uuid.UUID, body []byte) error {
	var attempt any
	if attemptID != uuid.Nil {
		attempt = attemptID
	}
	_, err := tx.Exec(ctx, `
		UPDATE worker_request_dedup
		SET response_body = $3, attempt_id = $4
		WHERE session_id = $1 AND dedup_key = $2`,
		sessionID, pullID.String(), body, attempt)
	if err != nil {
		return fmt.Errorf("store pull: %w", err)
	}
	return nil
}

func encodeWire(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("marshal pull: %w", err)
	}
	return buf.Bytes(), nil
}

func writeStored(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func sleepPull(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readMessage(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMessageBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeDecodeError(w, err)
		return nil, false
	}
	return body, true
}

func decodeRaw(w http.ResponseWriter, raw []byte, dst any) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeDecodeError(w, err)
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			writeCode(w, http.StatusBadRequest, "invalid_message")
			return false
		}
		writeDecodeError(w, err)
		return false
	}
	return true
}

const imageBacklogSQL = `
SELECT
    count(*) FILTER (
        WHERE status = 'queued'
          AND protocol_deadline > now()
          AND (not_before IS NULL OR not_before <= now())
    ),
    count(*) FILTER (
        WHERE status = 'active'
          AND protocol_deadline > now()
    ),
    count(*) FILTER (
        WHERE status = 'queued'
          AND protocol_deadline > now()
          AND not_before > now()
    )
FROM jobs
WHERE dispatch_owner = 'protocol'
  AND engine = 'image'
  AND capability_id IS DISTINCT FROM 'octoconv.convert.imagepdf'
  AND (
      workflow_snapshot_id IS NULL
      OR (
          step_id IS NOT NULL
          AND (
              SELECT count(*) FROM job_inputs i
              WHERE i.job_id = jobs.id
                AND i.ordinal = 0
                AND object_key <> ''
                AND format <> ''
          ) = 1
      )
  )
  AND ($1::uuid IS NULL OR id = $1)`

// ImageBacklog counts protocol image jobs that are still eligible to run.
func (r *Repo) ImageBacklog(ctx context.Context) (pending, active, retry int, err error) {
	return imageBacklog(ctx, r.pool, nil)
}

func imageBacklog(ctx context.Context, q queryRower, id *uuid.UUID) (pending, active, retry int, err error) {
	err = q.QueryRow(ctx, imageBacklogSQL, id).Scan(&pending, &active, &retry)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("image backlog: %w", err)
	}
	return pending, active, retry, nil
}
