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
	_, waitKind := integerToken(req.WaitSeconds)
	if freeKind != numOK || waitKind != numOK || freeSlots < 0 {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	bodyHash := sha256.Sum256(raw)
	wire, err := h.claimPull(r.Context(), sess, pullID, hex.EncodeToString(bodyHash[:]), freeSlots)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	writeJSON(w, http.StatusOK, wire)
}

func (h *Handler) claimPull(ctx context.Context, sess storedSession, pullID uuid.UUID, bodyHash string, freeSlots int64) (pullWire, error) {
	var wire pullWire
	err := pgx.BeginTxFunc(ctx, h.repo.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		cancelIDs, err := canceledAttempts(ctx, tx, sess.ID)
		if err != nil {
			return err
		}
		if freeSlots == 0 {
			wire = emptyPull(cancelIDs)
			return storePull(ctx, tx, sess.ID, pullID, bodyHash, uuid.Nil, wire)
		}
		var capabilityID string
		var capabilityVersion int
		err = tx.QueryRow(ctx, `
			SELECT id, version FROM worker_capabilities WHERE worker_id = $1`, sess.WorkerID).Scan(&capabilityID, &capabilityVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			wire = emptyPull(cancelIDs)
			return storePull(ctx, tx, sess.ID, pullID, bodyHash, uuid.Nil, wire)
		}
		if err != nil {
			return fmt.Errorf("read capability: %w", err)
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
			wire = emptyPull(cancelIDs)
			return storePull(ctx, tx, sess.ID, pullID, bodyHash, uuid.Nil, wire)
		}
		if err != nil {
			return fmt.Errorf("claim job: %w", err)
		}
		stepID := uuid.New()
		if storedStep != nil {
			stepID = *storedStep
		}
		attemptID := uuid.New()
		token, tokenHash, err := NewSessionToken()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO worker_attempt_credentials (
				attempt_id, token_hash, session_id, capability_id, capability_version
			) VALUES ($1, $2, $3, $4, $5)`,
			attemptID, tokenHash, sess.ID, capabilityID, capabilityVersion); err != nil {
			return fmt.Errorf("insert attempt credential: %w", err)
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
			return fmt.Errorf("insert slot hold: %w", err)
		}
		if err := h.jobsRepo.ActivateProtocol(ctx, tx, jobID, stepID); err != nil {
			return err
		}
		wire = pullWire{
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
		return storePull(ctx, tx, sess.ID, pullID, bodyHash, attemptID, wire)
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return emptyPull([]string{}), nil
		}
		return pullWire{}, err
	}
	return wire, nil
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

func storePull(ctx context.Context, tx pgx.Tx, sessionID, pullID uuid.UUID, bodyHash string, attemptID uuid.UUID, wire pullWire) error {
	body, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("marshal pull: %w", err)
	}
	var attempt any
	if attemptID != uuid.Nil {
		attempt = attemptID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO worker_request_dedup (session_id, dedup_key, body_sha256, response_body, attempt_id)
		VALUES ($1, $2, $3, $4, $5)`,
		sessionID, pullID.String(), bodyHash, body, attempt)
	if err != nil {
		return fmt.Errorf("store pull: %w", err)
	}
	return nil
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
