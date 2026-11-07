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

func (h *Handler) servePull(ctx context.Context, sess storedSession, pullID uuid.UUID, bodyHash string, freeSlots, waitSeconds int64) ([]byte, error) {
	wait := waitSeconds
	if wait < 0 {
		wait = 0
	}
	if wait > 20 {
		wait = 20
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	if wait == 0 {
		body, err := h.claimAndStore(ctx, sess, pullID, bodyHash, freeSlots)
		if errors.Is(err, errDedupLost) {
			return h.replayPull(ctx, sess.ID, pullID, bodyHash, deadline)
		}
		return body, err
	}
	owned, err := h.reservePull(ctx, sess.ID, pullID, bodyHash)
	if err != nil {
		return nil, err
	}
	if !owned {
		return h.replayPull(ctx, sess.ID, pullID, bodyHash, deadline)
	}
	for {
		wire, attemptID, err := h.claimOnceTx(ctx, sess, freeSlots)
		if err != nil {
			return nil, err
		}
		if attemptID != uuid.Nil || freeSlots == 0 || !time.Now().Before(deadline) {
			body, err := encodeWire(wire)
			if err != nil {
				return nil, err
			}
			if err := h.finishPull(ctx, sess.ID, pullID, attemptID, body); err != nil {
				return nil, err
			}
			return body, nil
		}
		remaining := time.Until(deadline)
		pause := 25 * time.Millisecond
		if remaining < pause {
			pause = remaining
		}
		if err := sleepPull(ctx, pause); err != nil {
			return nil, err
		}
	}
}

func (h *Handler) claimAndStore(ctx context.Context, sess storedSession, pullID uuid.UUID, bodyHash string, freeSlots int64) ([]byte, error) {
	var body []byte
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
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
		wire, attemptID, err := h.claimOnce(ctx, tx, sess, freeSlots)
		if err != nil {
			return err
		}
		encoded, err := encodeWire(wire)
		if err != nil {
			return err
		}
		if err := writePullResponse(ctx, tx, sess.ID, pullID, attemptID, encoded); err != nil {
			return err
		}
		body = encoded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (h *Handler) reservePull(ctx context.Context, sessionID, pullID uuid.UUID, bodyHash string) (bool, error) {
	var owned bool
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO worker_request_dedup (session_id, dedup_key, body_sha256)
			VALUES ($1, $2, $3)
			ON CONFLICT (session_id, dedup_key) DO NOTHING`,
			sessionID, pullID.String(), bodyHash)
		if err != nil {
			return fmt.Errorf("reserve pull: %w", err)
		}
		owned = tag.RowsAffected() == 1
		return nil
	})
	return owned, err
}

func (h *Handler) claimOnceTx(ctx context.Context, sess storedSession, freeSlots int64) (pullWire, uuid.UUID, error) {
	var wire pullWire
	var attemptID uuid.UUID
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		var err error
		wire, attemptID, err = h.claimOnce(ctx, tx, sess, freeSlots)
		return err
	})
	return wire, attemptID, err
}

func (h *Handler) finishPull(ctx context.Context, sessionID, pullID, attemptID uuid.UUID, body []byte) error {
	return pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		return writePullResponse(ctx, tx, sessionID, pullID, attemptID, body)
	})
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

func (h *Handler) claimOnce(ctx context.Context, tx pgx.Tx, sess storedSession, freeSlots int64) (pullWire, uuid.UUID, error) {
	cancelIDs, err := canceledAttempts(ctx, tx, sess.ID)
	if err != nil {
		return pullWire{}, uuid.Nil, err
	}
	if freeSlots == 0 {
		return emptyPull(cancelIDs), uuid.Nil, nil
	}
	var capabilityID string
	var capabilityVersion int
	err = tx.QueryRow(ctx, `
		SELECT id, version FROM worker_capabilities WHERE worker_id = $1`, sess.WorkerID).Scan(&capabilityID, &capabilityVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return emptyPull(cancelIDs), uuid.Nil, nil
	}
	if err != nil {
		return pullWire{}, uuid.Nil, fmt.Errorf("read capability: %w", err)
	}
	budgetSeconds := int64(h.imageAttemptBudget / time.Second)
	var jobID uuid.UUID
	var storedStep *uuid.UUID
	var attemptDeadline, leaseExpires time.Time
	var leaseOK bool
	err = tx.QueryRow(ctx, `
		SELECT id, step_id,
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
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, capabilityID, capabilityVersion, budgetSeconds).Scan(
		&jobID, &storedStep, &attemptDeadline, &leaseExpires, &leaseOK)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !leaseOK) {
		return emptyPull(cancelIDs), uuid.Nil, nil
	}
	if err != nil {
		return pullWire{}, uuid.Nil, fmt.Errorf("claim job: %w", err)
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT claim_attempt`); err != nil {
		return pullWire{}, uuid.Nil, fmt.Errorf("claim savepoint: %w", err)
	}
	stepID := uuid.New()
	if storedStep != nil {
		stepID = *storedStep
	}
	attemptID := uuid.New()
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		return pullWire{}, uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO worker_attempt_credentials (
			attempt_id, token_hash, session_id, capability_id, capability_version
		) VALUES ($1, $2, $3, $4, $5)`,
		attemptID, tokenHash, sess.ID, capabilityID, capabilityVersion); err != nil {
		return pullWire{}, uuid.Nil, fmt.Errorf("insert attempt credential: %w", err)
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
				return pullWire{}, uuid.Nil, fmt.Errorf("rollback claim: %w", rollErr)
			}
			return emptyPull(cancelIDs), uuid.Nil, nil
		}
		return pullWire{}, uuid.Nil, fmt.Errorf("insert slot hold: %w", err)
	}
	if err := h.jobsRepo.ActivateProtocol(ctx, tx, jobID, stepID); err != nil {
		return pullWire{}, uuid.Nil, err
	}
	wire := pullWire{
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
	}
	return wire, attemptID, nil
}

func emptyPull(cancelIDs []string) pullWire {
	return pullWire{
		ProtocolVersion: 1,
		Assignments:     []assignmentWire{},
		Control:         controlWire{CancelAttemptIDs: cancelIDs, Drain: false, Terminate: false},
	}
}

func canceledAttempts(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT id FROM worker_slot_holds
		WHERE session_id = $1 AND state = 'canceled'
		ORDER BY id`, sessionID)
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
