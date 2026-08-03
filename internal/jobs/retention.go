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

	// Only LEAVES are ever deleted -- an aged terminal job that no other job
	// points at via parent_id. Everything below falls out of that one rule.
	//
	// jobs.parent_id self-references with ON DELETE CASCADE, which makes
	// deleting any non-leaf unsafe in two distinct ways:
	//
	//   1. Silent over-deletion. Deleting an aged terminal parent removes its
	//      whole subtree -- including a child still queued or active, or one
	//      created well inside the retention window. That bypasses the status
	//      predicate and the cutoff, and nothing reports it: a self-referential
	//      cascade does not count toward the statement's rows-affected, so the
	//      caller is told it deleted one row while N vanished.
	//   2. An unbounded batch. `LIMIT` caps the ids selected into `candidate`,
	//      not what the cascade takes with them, so a parent with more than
	//      `batch` descendants defeats the per-tick work ceiling entirely.
	//
	// Restricting the candidate set to leaves closes both at once, and closes
	// them by construction rather than by a guard that has to be kept correct:
	// a leaf has nothing beneath it, so its cascade removes nothing, so exactly
	// the selected rows are deleted and rows-affected is honest.
	//
	// Live descendants stay protected without a recursive check. While any
	// descendant survives, every ancestor still has a child and is therefore
	// not a leaf -- protection propagates all the way up the chain for free.
	// Fully-eligible chains drain bottom-up across successive ticks: this
	// tick's leaves are deleted, which makes their parents leaves for the next.
	//
	// This also keeps the purge live. A protected parent is simply not a leaf,
	// so it never occupies a slot in the batch; the `LIMIT` is always filled
	// with genuinely deletable rows and the sweep never head-of-line blocks
	// behind a subtree it must spare. (An earlier revision selected roots first
	// and filtered them afterwards, which meant a batch of all-blocked roots
	// deleted nothing, tripped the caller's `n < batch` loop-exit, and re-read
	// the same blocked head every tick forever.)
	//
	// Dormant today -- nothing populates parent_id -- but it activates the
	// moment sub-jobs exist (SEED-017 PDF split, SEED-018 pipelines). While no
	// row has a parent, NOT EXISTS is satisfied by every row at negligible
	// cost. It is served by jobs_parent_idx, a partial index over
	// `parent_id IS NOT NULL`; the outer scan is served by jobs_retention_idx
	// (0012), whose predicate mirrors the status filter below and must change
	// with it.
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM jobs WHERE id IN (
			SELECT j.id FROM jobs j
			WHERE j.created_at < $1
			  AND j.status NOT IN ('queued', 'active')
			  AND NOT EXISTS (SELECT 1 FROM jobs c WHERE c.parent_id = j.id)
			ORDER BY j.created_at
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
