package workerproto

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrWorkerNotFound is returned when the worker id is not an identity.
var ErrWorkerNotFound = errors.New("worker not found")

// WorkerView is one worker in the operator list.
type WorkerView struct {
	WorkerID          string        `json:"worker_id"`
	CapabilityID      *string       `json:"capability_id"`
	CapabilityVersion *int          `json:"capability_version"`
	Sessions          []SessionView `json:"sessions"`
}

// SessionView is one open session in the operator list.
type SessionView struct {
	SessionID       string     `json:"session_id"`
	State           string     `json:"state"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
	ActiveAttempts  int        `json:"active_attempts"`
}

type rowQuery interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const workerAdminSQL = `
SELECT i.worker_id,
       c.id,
       c.version,
       s.id,
       CASE
           WHEN s.id IS NULL THEN NULL
           WHEN s.drain_at IS NOT NULL THEN 'draining'
           WHEN s.last_heartbeat_at IS NOT NULL
                AND s.last_heartbeat_at < clock_timestamp() - interval '30 seconds' THEN 'offline'
           WHEN s.last_heartbeat_at IS NULL
                AND s.opened_at < clock_timestamp() - interval '30 seconds' THEN 'offline'
           ELSE 'ready'
       END,
       s.last_heartbeat_at,
       COALESCE(holds.active_attempts, 0)
FROM worker_identities i
LEFT JOIN worker_capabilities c ON c.worker_id = i.worker_id
LEFT JOIN worker_sessions s ON s.worker_id = i.worker_id AND s.closed_at IS NULL
LEFT JOIN LATERAL (
    SELECT count(*)::int AS active_attempts
    FROM worker_slot_holds h
    WHERE s.id IS NOT NULL
      AND h.session_id = s.id
      AND h.released_at IS NULL
) holds ON true
WHERE ($1::uuid IS NULL OR i.worker_id = $1)
  AND (c.id IS NOT NULL OR s.id IS NOT NULL)
ORDER BY i.worker_id, s.opened_at, s.id`

// ListWorkers returns identities that have an open session or an approved capability.
func (r *Repo) ListWorkers(ctx context.Context) ([]WorkerView, error) {
	return collectWorkers(ctx, r.pool, nil)
}

func (r *Repo) touchHeartbeat(ctx context.Context, id uuid.UUID) (bool, error) {
	var draining bool
	err := r.pool.QueryRow(ctx, `
		UPDATE worker_sessions
		SET last_heartbeat_at = clock_timestamp()
		WHERE id = $1 AND closed_at IS NULL
		RETURNING drain_at IS NOT NULL`, id).Scan(&draining)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("touch heartbeat: %w", err)
	}
	return draining, nil
}

// DrainWorker stamps drain_at on sessions that are already open and returns that worker.
func (r *Repo) DrainWorker(ctx context.Context, workerID uuid.UUID) (WorkerView, error) {
	var view WorkerView
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM worker_identities WHERE worker_id = $1)`, workerID).Scan(&exists); err != nil {
			return fmt.Errorf("read identity: %w", err)
		}
		if !exists {
			return ErrWorkerNotFound
		}
		if _, err := tx.Exec(ctx, `
			UPDATE worker_sessions
			SET drain_at = clock_timestamp()
			WHERE worker_id = $1
			  AND closed_at IS NULL
			  AND drain_at IS NULL`, workerID); err != nil {
			return fmt.Errorf("drain sessions: %w", err)
		}
		views, err := collectWorkers(ctx, tx, &workerID)
		if err != nil {
			return err
		}
		if len(views) == 0 {
			view = WorkerView{WorkerID: workerID.String(), Sessions: []SessionView{}}
			return nil
		}
		view = views[0]
		return nil
	})
	if err != nil {
		return WorkerView{}, err
	}
	return view, nil
}

func sessionDraining(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, id uuid.UUID) (bool, error) {
	var draining bool
	err := q.QueryRow(ctx, `SELECT drain_at IS NOT NULL FROM worker_sessions WHERE id = $1`, id).Scan(&draining)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read drain: %w", err)
	}
	return draining, nil
}

func collectWorkers(ctx context.Context, q rowQuery, workerID *uuid.UUID) ([]WorkerView, error) {
	rows, err := q.Query(ctx, workerAdminSQL, workerID)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	defer rows.Close()

	order := make([]uuid.UUID, 0)
	grouped := map[uuid.UUID]*WorkerView{}
	for rows.Next() {
		var (
			id       uuid.UUID
			capID    *string
			capVer   *int
			session  *uuid.UUID
			state    *string
			beat     *time.Time
			attempts int
		)
		if err := rows.Scan(&id, &capID, &capVer, &session, &state, &beat, &attempts); err != nil {
			return nil, fmt.Errorf("scan worker: %w", err)
		}
		view, ok := grouped[id]
		if !ok {
			view = &WorkerView{
				WorkerID:          id.String(),
				CapabilityID:      capID,
				CapabilityVersion: capVer,
				Sessions:          []SessionView{},
			}
			grouped[id] = view
			order = append(order, id)
		}
		if session == nil {
			continue
		}
		if beat != nil {
			utc := beat.UTC()
			beat = &utc
		}
		view.Sessions = append(view.Sessions, SessionView{
			SessionID:       session.String(),
			State:           *state,
			LastHeartbeatAt: beat,
			ActiveAttempts:  attempts,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	out := make([]WorkerView, 0, len(order))
	for _, id := range order {
		out = append(out, *grouped[id])
	}
	return out, nil
}
