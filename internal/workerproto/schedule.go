package workerproto

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tileforge/octoconv/internal/jobs"
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
	Inputs            []struct{}       `json:"inputs"`
	Outputs           []struct{}       `json:"outputs"`
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
		ProtocolVersion: 1,
		LeaseExpiresAt:  lease.UTC().Format(time.RFC3339Nano),
		AttemptDeadline: deadline.UTC().Format(time.RFC3339Nano),
		Control:         controlWire{CancelAttemptIDs: cancelIDs, Drain: false, Terminate: false},
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
	limited, err := h.storeProgress(r.Context(), attemptID, parsed)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if limited {
		writeCode(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
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
	jobID, stepID, err := h.attemptJob(r.Context(), attemptID)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if req.EngineCode == "timeout" {
		if err := h.retryTimeout(r.Context(), jobID, stepID, req.Message); err != nil {
			writeCode(w, http.StatusInternalServerError, "invalid_message")
			return
		}
		h.writeControl(w, r, sess.ID)
		return
	}
	code := "engine_error"
	if _, ok := terminalEngineCodes[req.EngineCode]; ok {
		code = req.EngineCode
	}
	if err := h.jobsRepo.FailProtocol(r.Context(), jobID, code, req.Message); err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
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
	h.writeControl(w, r, sess.ID)
}

func (h *Handler) handleComplete(w http.ResponseWriter, r *http.Request) {
	sess, attemptID, raw, ok := h.readAttemptBody(w, r)
	if !ok {
		return
	}
	var req protocolRequest
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
	writeCode(w, http.StatusConflict, "artifact_rejected")
}

func (h *Handler) writeControl(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	ids, err := h.canceledAttemptIDs(r.Context(), sessionID)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	writeJSON(w, http.StatusOK, heartbeatWire{ProtocolVersion: 1, Control: controlWire{CancelAttemptIDs: ids, Drain: false, Terminate: false}})
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

func (h *Handler) storeProgress(ctx context.Context, attemptID uuid.UUID, parsed parsedProgress) (bool, error) {
	var limited bool
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
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
		var stage *string
		var stageIndex, stageCount, eta *int
		err = tx.QueryRow(ctx, `
			SELECT job_id, progress_percent, stage, stage_index, stage_count, eta_seconds
			FROM worker_slot_holds WHERE id = $1`, attemptID).
			Scan(&jobID, &current, &stage, &stageIndex, &stageCount, &eta)
		if err != nil {
			return fmt.Errorf("read progress: %w", err)
		}
		percent := current
		unknown := false
		if parsed.unknown {
			percent = nil
			unknown = true
		} else if current == nil || parsed.percent >= int(*current) {
			value := int16(parsed.percent)
			percent = &value
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
		if !parsed.unknown && (current == nil || parsed.percent >= int(*current)) {
			if _, err := tx.Exec(ctx, `
				UPDATE jobs
				SET progress_pct = $2, progress_updated_at = now()
				WHERE id = $1 AND status = 'active'
				  AND (progress_pct IS NULL OR progress_pct <= $2)`, jobID, parsed.percent); err != nil {
				return fmt.Errorf("store job progress: %w", err)
			}
		}
		return nil
	})
	return limited, err
}

func (h *Handler) attemptJob(ctx context.Context, attemptID uuid.UUID) (uuid.UUID, *uuid.UUID, error) {
	var jobID uuid.UUID
	var stepID *uuid.UUID
	err := h.repo.pool.QueryRow(ctx, `
		SELECT job_id, step_id FROM worker_slot_holds WHERE id = $1`, attemptID).Scan(&jobID, &stepID)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("read attempt job: %w", err)
	}
	return jobID, stepID, nil
}

func (h *Handler) retryTimeout(ctx context.Context, jobID uuid.UUID, stepID *uuid.UUID, message string) error {
	n := 1
	if stepID != nil {
		var failed int
		if err := h.repo.pool.QueryRow(ctx, `
			SELECT count(*) FROM worker_slot_holds
			WHERE step_id = $1 AND state = 'failed'`, *stepID).Scan(&failed); err != nil {
			return err
		}
		n = failed + 1
	}
	delay, err := attemptJitter(n)
	if err != nil {
		return err
	}
	var notBefore time.Time
	if delay > 0 {
		notBefore = time.Now().Add(time.Duration(delay) * time.Second)
	}
	err = h.jobsRepo.RequeueProtocol(ctx, jobID, notBefore)
	if errors.Is(err, jobs.ErrProtocolBudget) {
		return h.jobsRepo.FailProtocol(ctx, jobID, "reconciler_exhausted", message)
	}
	return err
}

func (h *Handler) stopAttempt(ctx context.Context, sessionID, attemptID uuid.UUID, reason string) error {
	return pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		var state string
		var released *time.Time
		var jobID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT state, released_at, job_id
			FROM worker_slot_holds
			WHERE id = $1 AND session_id = $2
			FOR UPDATE`, attemptID, sessionID).Scan(&state, &released, &jobID)
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
		live, err := h.attemptOpen(ctx, attemptID, sessionID, true)
		if err != nil {
			return err
		}
		if !live {
			return errAttemptInactive
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
	return nil, fmt.Errorf("pull skipped too many jobs")
}

type pullStep struct {
	done   bool
	body   []byte
	skipID uuid.UUID
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
		if _, err := tx.Exec(ctx, `SELECT id FROM worker_sessions WHERE id = $1 FOR UPDATE`, sess.ID); err != nil {
			return fmt.Errorf("lock session: %w", err)
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
	budgetSeconds := int64(h.imageAttemptBudget / time.Second)
	var jobID uuid.UUID
	var storedStep *uuid.UUID
	var clientID *uuid.UUID
	var attemptDeadline, leaseExpires time.Time
	var leaseOK bool
	err = tx.QueryRow(ctx, `
		SELECT id, step_id, client_id,
		       LEAST(protocol_deadline, now() + make_interval(secs => $3)),
		       LEAST(now() + interval '30 seconds', LEAST(protocol_deadline, now() + make_interval(secs => $3))),
		       LEAST(now() + interval '30 seconds', LEAST(protocol_deadline, now() + make_interval(secs => $3)))
		           > now() + interval '5 seconds'
		FROM jobs
		WHERE dispatch_owner = 'protocol'
		  AND status = 'queued'
		  AND capability_id = $1
		  AND capability_version = $2
		  AND (not_before IS NULL OR not_before <= now())
		  AND protocol_deadline > now()
		  AND NOT (id = ANY($4))
		  AND (
		      step_id IS NULL OR (
		          SELECT count(*) FROM worker_slot_holds h
		          WHERE h.step_id = jobs.step_id AND h.state = 'failed'
		      ) < 5
		  )
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, capabilityID, capabilityVersion, budgetSeconds, skipped).Scan(
		&jobID, &storedStep, &clientID, &attemptDeadline, &leaseExpires, &leaseOK)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !leaseOK) {
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
	if _, err := tx.Exec(ctx, `SAVEPOINT claim_attempt`); err != nil {
		return claimDecision{}, fmt.Errorf("claim savepoint: %w", err)
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
			if _, rollErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT claim_attempt`); rollErr != nil {
				return claimDecision{}, fmt.Errorf("rollback claim: %w", rollErr)
			}
			return claimDecision{wire: emptyPull(cancelIDs), emptyNow: true}, nil
		}
		return claimDecision{}, fmt.Errorf("insert slot hold: %w", err)
	}
	if err := h.jobsRepo.ActivateProtocol(ctx, tx, jobID, stepID); err != nil {
		return claimDecision{}, err
	}
	return claimDecision{
		attemptID: attemptID,
		wire: pullWire{
			ProtocolVersion: 1,
			Assignments: []assignmentWire{{
				JobID:             jobID.String(),
				StepID:            stepID.String(),
				AttemptID:         attemptID.String(),
				AttemptToken:      token,
				CapabilityID:      capabilityID,
				CapabilityVersion: capabilityVersion,
				AttemptDeadline:   attemptDeadline.UTC().Format(time.RFC3339Nano),
				LeaseExpiresAt:    leaseExpires.UTC().Format(time.RFC3339Nano),
				Limits:            assignmentLimits{MaxOutputBytes: 100 << 20},
				Inputs:            []struct{}{},
				Outputs:           []struct{}{},
			}},
			Control: controlWire{CancelAttemptIDs: cancelIDs, Drain: false, Terminate: false},
		},
	}, nil
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
		ProtocolVersion: 1,
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
