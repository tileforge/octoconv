package workerproto

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tileforge/octoconv/internal/jobs"
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

var errSessionNotFound = errors.New("session not found")

// NewSessionToken reads 32 CSPRNG bytes and returns the lowercase hex wire
// token plus the hex SHA-256 of those raw bytes.
func NewSessionToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("mint session token: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(raw), hex.EncodeToString(sum[:]), nil
}

// HashToken returns the hex SHA-256 of the raw bytes of a 64-character
// lowercase hex session token.
func HashToken(wire string) (string, error) {
	if !hex64.MatchString(wire) {
		return "", errors.New("bad session token")
	}
	raw, err := hex.DecodeString(wire)
	if err != nil || len(raw) != 32 {
		return "", errors.New("bad session token")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type storedSession struct {
	ID       uuid.UUID
	WorkerID uuid.UUID
	Closed   bool
	Expired  bool
}

type newSession struct {
	ID        uuid.UUID
	WorkerID  uuid.UUID
	TokenHash string
	Serial    string
	IssuerSHA string
	FreeSlots int
}

func (r *Repo) openSession(ctx context.Context, s newSession) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO worker_sessions (
			id, worker_id, token_hash, cert_serial_decimal, issuer_sha256, free_slots, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, now() + interval '86400 seconds'
		)`,
		s.ID, s.WorkerID, s.TokenHash, s.Serial, s.IssuerSHA, s.FreeSlots,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *Repo) closeSession(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE worker_sessions
			SET closed_at = COALESCE(closed_at, now())
			WHERE id = $1`, id); err != nil {
			return fmt.Errorf("close session: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM worker_request_dedup WHERE session_id = $1`, id); err != nil {
			return fmt.Errorf("delete dedup: %w", err)
		}
		return nil
	})
}

func (r *Repo) attemptTokenHash(ctx context.Context, attemptID uuid.UUID) (string, bool, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `
		SELECT token_hash FROM worker_attempt_credentials WHERE attempt_id = $1`, attemptID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read attempt credential: %w", err)
	}
	return hash, true, nil
}

func (r *Repo) sessionByHash(ctx context.Context, tokenHash string) (storedSession, error) {
	var s storedSession
	err := r.pool.QueryRow(ctx, `
		SELECT id, worker_id, closed_at IS NOT NULL, expires_at <= now()
		FROM worker_sessions
		WHERE token_hash = $1`, tokenHash).Scan(&s.ID, &s.WorkerID, &s.Closed, &s.Expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedSession{}, errSessionNotFound
	}
	if err != nil {
		return storedSession{}, fmt.Errorf("read session: %w", err)
	}
	return s, nil
}

func (r *Repo) approvedDigest(ctx context.Context, workerID uuid.UUID) (string, bool, error) {
	var digest string
	err := r.pool.QueryRow(ctx, `
		SELECT manifest_sha256 FROM worker_identities WHERE worker_id = $1`, workerID).Scan(&digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read identity: %w", err)
	}
	return digest, true, nil
}

func (r *Repo) identityExists(ctx context.Context, workerID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM worker_identities WHERE worker_id = $1)`, workerID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("identity exists: %w", err)
	}
	return exists, nil
}

// RevokeIdentity denylists the certificate and closes every session for that worker.
func (r *Repo) RevokeIdentity(ctx context.Context, workerID uuid.UUID, serial *big.Int, issuerSHA256 string) error {
	if err := validateDenial(workerID, serial, issuerSHA256); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := insertDenylist(ctx, tx, workerID, serial, issuerSHA256); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE worker_sessions
			SET closed_at = COALESCE(closed_at, now())
			WHERE worker_id = $1`, workerID); err != nil {
			return fmt.Errorf("close worker sessions: %w", err)
		}
		rows, err := tx.Query(ctx, `
			UPDATE worker_slot_holds AS h
			SET state = 'canceled', released_at = now()
			WHERE h.released_at IS NULL
			  AND h.session_id IN (SELECT id FROM worker_sessions WHERE worker_id = $1)
			RETURNING h.job_id`, workerID)
		if err != nil {
			return fmt.Errorf("fence revoked holds: %w", err)
		}
		seen := map[uuid.UUID]struct{}{}
		var jobIDs []uuid.UUID
		for rows.Next() {
			var jobID *uuid.UUID
			if err := rows.Scan(&jobID); err != nil {
				rows.Close()
				return fmt.Errorf("scan revoked hold: %w", err)
			}
			if jobID == nil {
				continue
			}
			if _, ok := seen[*jobID]; ok {
				continue
			}
			seen[*jobID] = struct{}{}
			jobIDs = append(jobIDs, *jobID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("list revoked holds: %w", err)
		}
		rows.Close()
		for _, jobID := range jobIDs {
			if err := r.requeueRevokedJob(ctx, tx, jobID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repo) requeueRevokedJob(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) error {
	var status string
	var retry bool
	err := tx.QueryRow(ctx, `
		SELECT j.status,
		       (SELECT count(*) FROM worker_slot_holds h
		        WHERE h.step_id = j.step_id
		          AND h.state = 'failed'
		          AND (j.manual_retry_at IS NULL OR h.released_at > j.manual_retry_at)) < 5
		       AND COALESCE(j.protocol_deadline > now(), false)
		FROM jobs j
		WHERE j.id = $1
		FOR UPDATE`, jobID).Scan(&status, &retry)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock revoked job: %w", err)
	}
	if status != jobs.StatusActive {
		return nil
	}
	if !retry {
		return jobs.NewRepo(r.pool).FailProtocolIn(ctx, tx, jobID, "reconciler_exhausted", "protocol budget exhausted")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'queued', progress_pct = NULL, eta_seconds = NULL
		WHERE id = $1`, jobID); err != nil {
		return fmt.Errorf("requeue revoked job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, $3, NULL)`, jobID, jobs.StatusActive, jobs.StatusQueued); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	return nil
}

// DenylistSerial denylists one certificate and closes only sessions opened with that serial.
func (r *Repo) DenylistSerial(ctx context.Context, workerID uuid.UUID, serial *big.Int, issuerSHA256 string) error {
	if err := validateDenial(workerID, serial, issuerSHA256); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := insertDenylist(ctx, tx, workerID, serial, issuerSHA256); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE worker_sessions
			SET closed_at = COALESCE(closed_at, now())
			WHERE worker_id = $1
			  AND cert_serial_decimal = $2
			  AND issuer_sha256 = $3`, workerID, serial.String(), issuerSHA256); err != nil {
			return fmt.Errorf("close certificate sessions: %w", err)
		}
		return nil
	})
}

func validateDenial(workerID uuid.UUID, serial *big.Int, issuerSHA256 string) error {
	if workerID == uuid.Nil || serial == nil || issuerSHA256 == "" {
		return errors.New("deny certificate: missing certificate")
	}
	return nil
}

func insertDenylist(ctx context.Context, tx pgx.Tx, workerID uuid.UUID, serial *big.Int, issuerSHA256 string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO worker_certificate_denylist (issuer_sha256, serial_decimal, worker_id)
		VALUES ($1, $2, $3)`, issuerSHA256, serial.String(), workerID); err != nil {
		return fmt.Errorf("insert denylist: %w", err)
	}
	return nil
}

func (r *Repo) serialDenied(ctx context.Context, issuerSHA, serial string) (bool, error) {
	var denied bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM worker_certificate_denylist
			WHERE issuer_sha256 = $1 AND serial_decimal = $2
		)`, issuerSHA, serial).Scan(&denied)
	if err != nil {
		return false, fmt.Errorf("read denylist: %w", err)
	}
	return denied, nil
}
