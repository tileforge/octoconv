package clients

// Per-(client, engine) quota storage (QUOT-01, D-02): a NULL column always
// means unlimited and this file never writes a sentinel number in its place
// — an absent client_limits row is equally unlimited. The one deliberate
// exception is MaxConcurrent = 0, which is a legal, explicit value meaning
// "this client may run no jobs on this engine at all" (an engine-level deny)
// and is never confused with "unset" because it round-trips as a non-nil
// pointer to 0, distinct from a nil pointer.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Limit is one client_limits row: per-engine caps for a single client.
// Pointer fields let "unset" round-trip as SQL NULL rather than as 0 — a nil
// pointer is unlimited, a non-nil pointer to 0 is an explicit deny.
type Limit struct {
	Engine        string
	MaxConcurrent *int
	MaxDailyBytes *int64
	MaxDailyJobs  *int64
}

// SetLimit upserts the full row for (clientID, engine). This is FULL-ROW
// replace semantics, not a partial update: every column is overwritten by
// whatever is in l, so a nil pointer here CLEARS a previously set limit on
// that column. Callers (cmd/manage-clients) must always pass the complete
// desired state, not just the field they mean to change.
func (r *Repo) SetLimit(ctx context.Context, clientID uuid.UUID, l Limit) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO client_limits (client_id, engine, max_concurrent, max_daily_bytes, max_daily_jobs)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (client_id, engine) DO UPDATE SET
			max_concurrent  = EXCLUDED.max_concurrent,
			max_daily_bytes = EXCLUDED.max_daily_bytes,
			max_daily_jobs  = EXCLUDED.max_daily_jobs`,
		clientID, l.Engine, l.MaxConcurrent, l.MaxDailyBytes, l.MaxDailyJobs,
	)
	if err != nil {
		return fmt.Errorf("set client limit: %w", err)
	}
	return nil
}

// ClearLimit deletes the (clientID, engine) row entirely. Deleting the row is
// equivalent to setting every column NULL (D-02: an absent row is unlimited,
// same as an all-NULL row) — it exists as an operator convenience, not
// because the two states differ.
func (r *Repo) ClearLimit(ctx context.Context, clientID uuid.UUID, engine string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM client_limits WHERE client_id = $1 AND engine = $2`,
		clientID, engine,
	)
	if err != nil {
		return fmt.Errorf("clear client limit: %w", err)
	}
	return nil
}

// GetLimit returns the (clientID, engine) row, or (nil, nil) when no row
// exists — an absent row is unlimited (D-02), not an error condition. Only a
// genuine query failure returns a non-nil error.
func (r *Repo) GetLimit(ctx context.Context, clientID uuid.UUID, engine string) (*Limit, error) {
	l := Limit{Engine: engine}
	err := r.pool.QueryRow(ctx, `
		SELECT max_concurrent, max_daily_bytes, max_daily_jobs
		FROM client_limits WHERE client_id = $1 AND engine = $2`,
		clientID, engine,
	).Scan(&l.MaxConcurrent, &l.MaxDailyBytes, &l.MaxDailyJobs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get client limit: %w", err)
	}
	return &l, nil
}

// ListLimits returns every client_limits row for clientID, ordered by
// engine, for the operator-facing show-limits subcommand.
func (r *Repo) ListLimits(ctx context.Context, clientID uuid.UUID) ([]Limit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT engine, max_concurrent, max_daily_bytes, max_daily_jobs
		FROM client_limits WHERE client_id = $1 ORDER BY engine`,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("list client limits: %w", err)
	}
	defer rows.Close()

	var out []Limit
	for rows.Next() {
		var l Limit
		if err := rows.Scan(&l.Engine, &l.MaxConcurrent, &l.MaxDailyBytes, &l.MaxDailyJobs); err != nil {
			return nil, fmt.Errorf("scan client limit: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list client limits: %w", err)
	}
	return out, nil
}

// SetEnforcementMode flips a client's enforcement_mode. mode is validated in
// Go BEFORE the query runs — fail-closed at the edge, with the DB CHECK
// constraint as the backstop, not the primary validator. A clientID that
// matches no row maps to ErrNotFound.
func (r *Repo) SetEnforcementMode(ctx context.Context, clientID uuid.UUID, mode string) error {
	if mode != "observe" && mode != "enforce" {
		return fmt.Errorf("invalid enforcement mode %q: must be \"observe\" or \"enforce\"", mode)
	}

	tag, err := r.pool.Exec(ctx,
		`UPDATE clients SET enforcement_mode = $2 WHERE id = $1`,
		clientID, mode,
	)
	if err != nil {
		return fmt.Errorf("set enforcement mode: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
