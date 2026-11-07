package workerproto

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/jobs"
)

const protocolAdvisoryKey int64 = 0x70726F74

const protocolPassInterval = 5 * time.Second

const failedHoldBudgetSQL = `
	(SELECT count(*) FROM worker_slot_holds h
	 WHERE h.step_id = j.step_id
	   AND h.state = 'failed'
	   AND (j.manual_retry_at IS NULL OR h.released_at > j.manual_retry_at))`

// StartProtocolReconciler runs the lease pass until ctx is canceled.
// imageAttemptBudget is the image engine timeout the caller already loaded.
// This loop does not read ENGINE_TIMEOUT and does not mint attempt deadlines.
func StartProtocolReconciler(ctx context.Context, pool *pgxpool.Pool, jobsRepo *jobs.Repo, imageAttemptBudget time.Duration) error {
	if imageAttemptBudget < 0 {
		return fmt.Errorf("image attempt budget is negative")
	}
	lock, err := newProtocolLock(ctx, pool)
	if err != nil {
		return err
	}
	defer lock.Close()
	ticker := time.NewTicker(protocolPassInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			ok, err := lock.tryAcquire(ctx)
			if err != nil || !ok {
				continue
			}
			if err := protocolPass(ctx, pool, jobsRepo); err != nil {
				continue
			}
		}
	}
}

func protocolPass(ctx context.Context, pool *pgxpool.Pool, jobsRepo *jobs.Repo) error {
	if _, err := pool.Exec(ctx, `
		DELETE FROM worker_request_dedup
		WHERE response_body IS NULL
		  AND attempt_id IS NULL
		  AND created_at < now() - interval '25 seconds'`); err != nil {
		return fmt.Errorf("delete stale pull reservations: %w", err)
	}
	if err := releaseExpiredLeases(ctx, pool, jobsRepo); err != nil {
		return err
	}
	if err := requeueCanceledActive(ctx, pool, jobsRepo); err != nil {
		return err
	}
	return failPastDeadlines(ctx, pool, jobsRepo)
}

func releaseExpiredLeases(ctx context.Context, pool *pgxpool.Pool, jobsRepo *jobs.Repo) error {
	rows, err := pool.Query(ctx, `
		SELECT h.id, h.job_id
		FROM worker_slot_holds h
		JOIN jobs j ON j.id = h.job_id
		WHERE h.state = 'active'
		  AND h.released_at IS NULL
		  AND h.lease_expires_at <= now()
		  AND j.dispatch_owner = 'protocol'
		  AND j.status = 'active'`)
	if err != nil {
		return fmt.Errorf("list expired leases: %w", err)
	}
	type pair struct {
		holdID uuid.UUID
		jobID  uuid.UUID
	}
	var due []pair
	for rows.Next() {
		var item pair
		var jobID *uuid.UUID
		if err := rows.Scan(&item.holdID, &jobID); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired lease: %w", err)
		}
		if jobID == nil {
			continue
		}
		item.jobID = *jobID
		due = append(due, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list expired leases: %w", err)
	}
	rows.Close()
	for _, item := range due {
		if err := releaseOneLease(ctx, pool, jobsRepo, item.holdID, item.jobID); err != nil {
			return err
		}
	}
	return nil
}

func releaseOneLease(ctx context.Context, pool *pgxpool.Pool, jobsRepo *jobs.Repo, holdID, jobID uuid.UUID) error {
	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&status); err != nil {
			return fmt.Errorf("lock expired job: %w", err)
		}
		var state string
		var open, expired bool
		if err := tx.QueryRow(ctx, `
			SELECT state, released_at IS NULL, COALESCE(lease_expires_at <= now(), false)
			FROM worker_slot_holds WHERE id = $1 FOR UPDATE`, holdID).Scan(&state, &open, &expired); err != nil {
			return fmt.Errorf("lock expired hold: %w", err)
		}
		if status != jobs.StatusActive || state != "active" || !open || !expired {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE worker_slot_holds
			SET state = 'failed', released_at = now()
			WHERE id = $1`, holdID); err != nil {
			return fmt.Errorf("fail expired hold: %w", err)
		}
		return queueOrExhaust(ctx, tx, jobsRepo, jobID)
	})
}

func requeueCanceledActive(ctx context.Context, pool *pgxpool.Pool, jobsRepo *jobs.Repo) error {
	rows, err := pool.Query(ctx, `
		SELECT j.id
		FROM jobs j
		WHERE j.dispatch_owner = 'protocol'
		  AND j.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM worker_slot_holds h
		      WHERE h.job_id = j.id AND h.released_at IS NULL
		  )
		  AND EXISTS (
		      SELECT 1 FROM worker_slot_holds h
		      WHERE h.job_id = j.id AND h.state = 'canceled' AND h.released_at IS NOT NULL
		  )`)
	if err != nil {
		return fmt.Errorf("list canceled active jobs: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan canceled active job: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list canceled active jobs: %w", err)
	}
	rows.Close()
	for _, id := range ids {
		if err := requeueOneCanceled(ctx, pool, jobsRepo, id); err != nil {
			return err
		}
	}
	return nil
}

func requeueOneCanceled(ctx context.Context, pool *pgxpool.Pool, jobsRepo *jobs.Repo, jobID uuid.UUID) error {
	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		var status string
		var open int
		var canceled int
		if err := tx.QueryRow(ctx, `
			SELECT j.status,
			       (SELECT count(*) FROM worker_slot_holds h
			        WHERE h.job_id = j.id AND h.released_at IS NULL),
			       (SELECT count(*) FROM worker_slot_holds h
			        WHERE h.job_id = j.id AND h.state = 'canceled' AND h.released_at IS NOT NULL)
			FROM jobs j WHERE j.id = $1 FOR UPDATE`, jobID).Scan(&status, &open, &canceled); err != nil {
			return fmt.Errorf("lock canceled active job: %w", err)
		}
		if status != jobs.StatusActive || open != 0 || canceled == 0 {
			return nil
		}
		return queueOrExhaust(ctx, tx, jobsRepo, jobID)
	})
}

func queueOrExhaust(ctx context.Context, tx pgx.Tx, jobsRepo *jobs.Repo, jobID uuid.UUID) error {
	var failed int
	var retry bool
	if err := tx.QueryRow(ctx, `
		SELECT `+failedHoldBudgetSQL+`,
		       `+failedHoldBudgetSQL+` < 5
		       AND COALESCE(j.protocol_deadline > now(), false)
		FROM jobs j WHERE j.id = $1`, jobID).Scan(&failed, &retry); err != nil {
		return fmt.Errorf("count protocol budget: %w", err)
	}
	if !retry {
		return jobsRepo.FailProtocolIn(ctx, tx, jobID, "reconciler_exhausted", "protocol budget exhausted")
	}
	delay, err := attemptJitter(failed)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'queued',
		    not_before = now() + make_interval(secs => $2),
		    progress_pct = NULL,
		    eta_seconds = NULL
		WHERE id = $1`, jobID, delay); err != nil {
		return fmt.Errorf("requeue protocol job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, $3, NULL)`, jobID, jobs.StatusActive, jobs.StatusQueued); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	return nil
}

func failPastDeadlines(ctx context.Context, pool *pgxpool.Pool, jobsRepo *jobs.Repo) error {
	rows, err := pool.Query(ctx, `
		SELECT id FROM jobs
		WHERE dispatch_owner = 'protocol'
		  AND status IN ('queued', 'active')
		  AND protocol_deadline <= now()`)
	if err != nil {
		return fmt.Errorf("list past protocol deadlines: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan past protocol deadline: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list past protocol deadlines: %w", err)
	}
	rows.Close()
	for _, id := range ids {
		if err := jobsRepo.FailProtocol(ctx, id, "reconciler_exhausted", "protocol deadline passed"); err != nil {
			return err
		}
	}
	return nil
}

type protocolLock struct {
	pool   *pgxpool.Pool
	conn   *pgxpool.Conn
	mu     sync.Mutex
	closed bool
}

func newProtocolLock(ctx context.Context, pool *pgxpool.Pool) (*protocolLock, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire protocol lock connection: %w", err)
	}
	return &protocolLock{pool: pool, conn: conn}, nil
}

func (l *protocolLock) tryAcquire(ctx context.Context) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false, fmt.Errorf("protocol lock is closed")
	}
	if l.conn == nil {
		conn, err := l.pool.Acquire(ctx)
		if err != nil {
			return false, fmt.Errorf("re-acquire protocol lock connection: %w", err)
		}
		l.conn = conn
	}
	var acquired bool
	if err := l.conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, protocolAdvisoryKey).Scan(&acquired); err != nil {
		l.hardClose()
		return false, fmt.Errorf("pg_try_advisory_lock: %w", err)
	}
	return acquired, nil
}

func (l *protocolLock) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	l.hardClose()
}

func (l *protocolLock) hardClose() {
	if l.conn == nil {
		return
	}
	conn := l.conn
	l.conn = nil
	// Close the session so Postgres drops pg_try_advisory_lock. Release
	// after that only returns the pool slot. Releasing a live connection
	// would recycle a session that still holds the lock.
	conn.Conn().Close(context.Background())
	conn.Release()
}
