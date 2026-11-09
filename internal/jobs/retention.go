// Package jobs (this file): the retention purge (RETN-01, D-11/D-12/D-13).
//
// Deleting `jobs` is enough: job_events, job_inputs, job_outputs and
// webhook_deliveries all declare `job_id ... REFERENCES jobs (id) ON DELETE
// CASCADE` (0001_init.sql), workflow_snapshots does the same (0027), and
// jobs.parent_id self-references with CASCADE too. A separate
// `DELETE FROM job_events` (etc.) here would be wrong-order churn, not a
// missing feature — do not add one.
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
// cascades to its job_events/job_inputs/job_outputs/webhook_deliveries and
// workflow_snapshots (see the package doc comment above) — no separate
// deletes are issued here.
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
	// The cutoff is computed by POSTGRES, not here (DEBT-11). created_at is
	// written by the database, so comparing it against a boundary derived from
	// this process's clock measures one timeline against another: a container
	// whose clock runs behind keeps rows past their retention, one that runs
	// ahead deletes them early, and neither is visible from either side.
	//
	// The interval form deliberately preserves the TIME-OF-DAY component the
	// previous Go-side boundary had. Truncating to a date here would be a
	// tidier-looking expression and would silently move the boundary by up to a
	// day — a correctness fix that changes what gets deleted is two changes,
	// and only one of them was asked for.
	//
	// The Go call this replaced is not named literally anywhere in this file:
	// TestRetentionTakesItsClockFromPostgres greps the source for it, and a
	// comment quoting it would trip that gate and invite weakening the test
	// instead of the code.
	//
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
			WHERE j.created_at < (now() - $1::interval)
			  AND j.status NOT IN ('queued', 'active')
			  AND NOT EXISTS (SELECT 1 FROM jobs c WHERE c.parent_id = j.id)
			ORDER BY j.created_at
			LIMIT $2
		)`, olderThan, batch)
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
// The cutoff is computed by Postgres, in UTC. client_usage_daily.day is
// always written as `(now() AT TIME ZONE 'utc')::date` (recordUsage,
// usage.go), and this function now reads its boundary from the same clock.
//
// The comment this replaces was half right and therefore worse than useless:
// it correctly warned that a non-UTC boundary would drift the day, and drew
// the wrong conclusion — that computing it in Go was fine as long as it was
// UTC. The zone was never the whole problem. Two correctly-zoned clocks that
// disagree by seconds still disagree across midnight, and a DATE boundary
// turns that disagreement into a whole day purged or spared.
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
	// Same rule as PurgeJobs above, and it bites harder here (DEBT-11).
	// client_usage_daily.day is a DATE written by Postgres, and every other
	// reader of that column already takes its clock from the database:
	// evaluate(), recordUsage, and UTCToday — whose doc comment states the rule
	// outright, that Go never touches the value. This function was the one
	// place still comparing that column against the host's clock.
	//
	// Why it matters more than for PurgeJobs: a timestamp boundary shifts by
	// exactly the clock skew, but a DATE boundary is a cliff. A container a few
	// seconds off across midnight deletes, or spares, a whole day of a client's
	// usage rollup — the rows the daily quota is computed from.
	//
	// The boundary is cast back to DATE, and this is the opposite of what the
	// sibling above needs -- a difference that cost a review round to find.
	//
	// The reasoning that produced the first version was right about PurgeJobs
	// and exactly wrong here. It argued that keeping the time-of-day component
	// preserves the old boundary, because dropping it "moves the boundary a
	// full day". For a timestamptz column that holds. For `day`, which is a
	// DATE, the old expression never had a time-of-day component to preserve:
	// in `WHERE day < $1`, PostgreSQL infers the parameter type from the `day`
	// operand, so pgx encoded the Go time.Time AS A DATE and its clock time was
	// discarded before the comparison ever happened.
	//
	// Measured against a live PostgreSQL 18, for a cutoff of 2026-06-01 08:14
	// and rows on that day:
	//
	//	old    `day < $1` (pgx-bound time.Time) -> cutoff day SURVIVES
	//	first  `day < timestamp`                -> cutoff day DELETED
	//	this   `day < timestamp::date`          -> cutoff day SURVIVES
	//
	// So the first version silently deleted one extra day of every client's
	// usage rollup -- irreversibly, and in the name of not moving the boundary.
	// The cast restores the old cutoff exactly.
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM client_usage_daily WHERE ctid IN (
			SELECT ctid FROM client_usage_daily
			WHERE day < (((now() AT TIME ZONE 'utc') - $1::interval)::date)
			LIMIT $2
		)`, olderThan, batch)
	if err != nil {
		return 0, fmt.Errorf("purge usage older than %s: %w", olderThan, err)
	}
	return tag.RowsAffected(), nil
}
