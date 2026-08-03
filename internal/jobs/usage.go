// Package jobs (this file): the client_usage_daily rollup write (ACCT-02,
// ACCT-03).
//
// Exactly-once, by construction, not by a separate idempotency key: this
// write is deliberately NON-idempotent (a repeated call adds another
// increment, see the ON CONFLICT arm below) and safe to be so ONLY because
// it is always invoked from inside Repo.transition's apply closure, on the
// SAME pgx.Tx that already holds `SELECT ... FOR UPDATE` on the jobs row and
// has already checked the source status against transition's allow-list
// (D-06). A second terminal transition for the same job can never commit —
// the allow-list rejects it before apply ever runs — so recordUsage can
// never be invoked twice for one job's transition to done/failed. There is
// no dedupe key on client_usage_daily and none is needed; do not add a
// job_id-keyed idempotency table here, it would be solving a problem that
// does not exist at this write site.
package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// recordUsage upserts the daily consumption rollup for job id's client, on
// the caller's tx — see the package doc comment above for why this MUST run
// inside Repo.transition's apply closure and MUST NOT be called adjacent to
// it (D-05). status is one of the three terminal statuses StatusDone,
// StatusFailed or StatusCanceled (migration 0014, 52-REVIEW.md WR-01) —
// still always a Go constant from the caller, never a literal derived from
// client input, so the no-injection argument is unchanged. attempt is the
// elapsed engine time for THIS attempt; pass 0 when the caller has no
// attempt clock (e.g. the reconciler's exhaustion path, or Cancel, which
// never has one), and the statement falls back to deriving wall time from
// jobs.started_at.
//
// Deliberately not swallowed: the surrounding worker/reconciler code is full
// of best-effort, fire-and-forget calls (metrics.RecordJobOutcome and
// friends), and the next reader will assume this is one of those. It is
// not — a failed rollup here ABORTS the whole transition (the caller's
// pgx.BeginFunc rolls back), because losing usage silently is exactly the
// defect D-05 exists to close.
func (r *Repo) recordUsage(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, attempt time.Duration) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)
		SELECT
			j.client_id,
			j.engine,
			(now() AT TIME ZONE 'utc')::date,
			$2,
			1,
			-- Byte totals are summed over ALL of the job's input/output rows,
			-- never a single fixed row — the SEED-017 constraint. Neither
			-- subquery below filters by row position; a split/merge job
			-- producing N outputs is counted correctly with no accounting
			-- rewrite.
			COALESCE((SELECT sum(size_bytes) FROM job_inputs WHERE job_id = j.id), 0),
			COALESCE((SELECT sum(size_bytes) FROM job_outputs WHERE job_id = j.id), 0),
			-- wall_seconds: prefer the caller's attempt clock ($3, seconds) when
			-- positive. Otherwise derive it from started_at, which is the ONLY
			-- case a caller with no attempt clock (the reconciler's exhaustion
			-- path, RECON) reaches this statement. And if the job never started
			-- at all — exhausted while still stranded in 'queued' — record 0:
			-- charging it the age of the row would over-report wall time for a
			-- job that never occupied a worker.
			CASE
				WHEN $3::double precision > 0 THEN $3
				WHEN j.started_at IS NOT NULL THEN EXTRACT(EPOCH FROM (now() - j.started_at))
				ELSE 0
			END
		FROM jobs j
		WHERE j.id = $1 AND j.client_id IS NOT NULL
		ON CONFLICT (client_id, engine, day, status) DO UPDATE SET
			job_count    = client_usage_daily.job_count + 1,
			bytes_in     = client_usage_daily.bytes_in + EXCLUDED.bytes_in,
			bytes_out    = client_usage_daily.bytes_out + EXCLUDED.bytes_out,
			wall_seconds = client_usage_daily.wall_seconds + EXCLUDED.wall_seconds`,
		id, status, attempt.Seconds(),
	)
	if err != nil {
		return fmt.Errorf("record usage for job %s: %w", id, err)
	}
	return nil
}
