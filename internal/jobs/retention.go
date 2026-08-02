// Package jobs (this file): the retention purge (RETN-01, D-11/D-12/D-13).
//
// Deleting `jobs` is enough: job_events, job_inputs, job_outputs and
// webhook_deliveries all declare `job_id ... REFERENCES jobs (id) ON DELETE
// CASCADE` (0001_init.sql), and jobs.parent_id self-references with CASCADE
// too. A separate `DELETE FROM job_events` (etc.) here would be wrong-order
// churn, not a missing feature — do not add one.
//
// client_usage_daily is purged independently, on its OWN longer window, by
// PurgeUsage below. That independence is the entire reason D-01 chose a
// dedicated rollup table over aggregating `jobs` on the fly: without a
// separate purge here, PurgeJobs deleting aged `jobs` rows would silently
// truncate consumption history to the jobs retention window.
package jobs

import (
	"context"
	"fmt"
	"time"
)

// PurgeJobs deletes up to batch `jobs` rows whose created_at is older than
// olderThan AND whose status is NOT 'queued' or 'active'. The status
// exclusion is the safety property, not an optimization: a queued job
// stranded for months is a reconciler problem, and deleting live work while
// a worker holds it would be unrecoverable data loss. Deleting a jobs row
// cascades to its job_events/job_inputs/job_outputs/webhook_deliveries (see
// the package doc comment above) — no separate deletes are issued here.
//
// cutoff is computed in Go and bound as a timestamptz parameter, never an
// inline SQL interval expression, matching Repo.FindStale's convention.
// LIMIT lives inside the subquery because Postgres does not accept LIMIT
// directly on DELETE.
//
// olderThan <= 0 or batch <= 0 returns (0, nil) WITHOUT executing a DELETE.
// This is not an edge-case nicety: an operator who sets JOBS_RETENTION to an
// empty value, or a reconciler.Config literal that omits the field entirely
// (the exact shape every pre-existing test in this codebase uses), must get
// "retention off", never "delete everything older than now". This is the
// single highest-consequence defect available in this file — do not narrow
// or remove this guard.
func (r *Repo) PurgeJobs(ctx context.Context, olderThan time.Duration, batch int) (int64, error) {
	if olderThan <= 0 || batch <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-olderThan)

	tag, err := r.pool.Exec(ctx, `
		DELETE FROM jobs WHERE id IN (
			SELECT id FROM jobs
			WHERE created_at < $1 AND status NOT IN ('queued', 'active')
			ORDER BY created_at
			LIMIT $2
		)`, cutoff, batch)
	if err != nil {
		return 0, fmt.Errorf("purge jobs older than %s: %w", olderThan, err)
	}
	return tag.RowsAffected(), nil
}

// PurgeUsage deletes up to batch client_usage_daily rows whose day is older
// than olderThan, using the client_usage_daily_day_idx index created
// alongside the table (0011_client_usage_daily.sql) so this never becomes a
// sequential scan.
//
// cutoff is computed in Go, in UTC — client_usage_daily.day is always
// written as `(now() AT TIME ZONE 'utc')::date` (recordUsage, usage.go), so
// computing the cutoff from a non-UTC time.Now() here would drift the day
// boundary by the host's local offset and could purge (or spare) a row a
// day early/late relative to how it was written.
//
// client_usage_daily has no surrogate id (its primary key is the composite
// (client_id, engine, day, status)), so ctid — not id — is the batch-bound
// row selector.
//
// Same olderThan <= 0 / batch <= 0 disables-purging guard as PurgeJobs; see
// that doc comment for why this is a correctness requirement, not a nicety.
func (r *Repo) PurgeUsage(ctx context.Context, olderThan time.Duration, batch int) (int64, error) {
	if olderThan <= 0 || batch <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-olderThan)

	tag, err := r.pool.Exec(ctx, `
		DELETE FROM client_usage_daily WHERE ctid IN (
			SELECT ctid FROM client_usage_daily
			WHERE day < $1
			LIMIT $2
		)`, cutoff, batch)
	if err != nil {
		return 0, fmt.Errorf("purge usage older than %s: %w", olderThan, err)
	}
	return tag.RowsAffected(), nil
}
