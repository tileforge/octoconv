package workerproto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func (h *Handler) handleProgress(w http.ResponseWriter, r *http.Request) {
	sess, attemptID, ok := h.authenticateAttempt(w, r)
	if !ok {
		return
	}
	raw, ok := readMessage(w, r)
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
	h.writeControl(w, r, sess.ID)
}

func (h *Handler) handleFail(w http.ResponseWriter, r *http.Request) {
	h.rejectClosedAttempt(w, r)
}

func (h *Handler) handleStopped(w http.ResponseWriter, r *http.Request) {
	h.rejectClosedAttempt(w, r)
}

func (h *Handler) handleComplete(w http.ResponseWriter, r *http.Request) {
	h.rejectClosedAttempt(w, r)
}

func (h *Handler) rejectClosedAttempt(w http.ResponseWriter, r *http.Request) {
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
	live, err := h.attemptOpen(r.Context(), attemptID, sess.ID, true)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if !live {
		writeCode(w, http.StatusConflict, "attempt_not_active")
		return
	}
	writeCode(w, http.StatusConflict, "attempt_not_active")
}

func (h *Handler) writeControl(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	ids, err := h.canceledAttemptIDs(r.Context(), sessionID)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	writeJSON(w, http.StatusOK, heartbeatWire{ProtocolVersion: 1, Control: controlWire{CancelAttemptIDs: ids, Drain: false, Terminate: false}})
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
