// Package jobs (this file): the READ side of client_usage_daily for the
// operator visibility endpoint (OPSV-01/OPSV-02, Phase 53).
//
// This deliberately mirrors evaluate()'s formula (admission.go, D-05): the
// terminal aggregate sums client_usage_daily with no status predicate, and
// the in-flight (queued/active) term uses the same literal-status SQL shape
// evaluate() uses, so this endpoint's "used" figure is byte-for-byte the
// figure the admission gate actually enforces. If this file's SQL ever
// drifts from evaluate()'s, the operator-facing numbers lie about what the
// admission gate will do — the exact failure D-05 exists to prevent.
//
// This runs WITHOUT admitLocked's per-(client, engine) advisory lock: a
// read-only visibility query tolerates a benign race with a concurrent
// admission (a figure that is a few milliseconds stale), unlike the
// admission gate itself, which must be exact under concurrent replicas.
//
// Callers resolving "today" for this endpoint MUST use UTCToday, not the API
// host's wall clock — see UTCToday's doc comment for why (review WR-03).
package jobs

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// UsageRow is one (client, engine) usage summary over a requested day range,
// returned by UsageSummary. It carries both the terminal (rolled-up,
// completed) usage and the live in-flight (queued/active) usage as separate
// figures — D-06 — so a caller can render "used so far today" and "in
// progress right now" distinctly.
type UsageRow struct {
	// ClientID identifies the client this row summarizes.
	ClientID uuid.UUID
	// Engine is the engine class this row summarizes (e.g. "image").
	Engine string

	// TerminalJobCount is the number of terminal (done/failed/canceled) jobs
	// for this (client, engine) over the requested day range, summed from
	// client_usage_daily with NO status filter — see the package doc comment
	// above for why the absent filter is deliberate.
	TerminalJobCount int64
	// TerminalBytesIn is the total input bytes for those terminal jobs. This
	// is the figure evaluate() compares against max_daily_bytes (plus the
	// in-flight term below).
	TerminalBytesIn int64
	// TerminalBytesOut is the total output bytes for those terminal jobs.
	// Reported for operator visibility ONLY — it is NOT part of the
	// daily-bytes budget arithmetic. evaluate() compares max_daily_bytes
	// against sum(bytes_in) plus in-flight job_inputs.size_bytes, never
	// against bytes_out.
	TerminalBytesOut int64

	// InFlightJobCount is the number of jobs currently queued or active for
	// this (client, engine), evaluated live (not scoped to the requested day
	// range — a job in flight right now consumes capacity right now,
	// regardless of when it was created; see usageInFlightJobsSQL's comment).
	InFlightJobCount int64
	// InFlightBytesIn is the total input bytes across all job_inputs rows of
	// those in-flight jobs.
	InFlightBytesIn int64
}

// usageTerminalSQL groups client_usage_daily over the requested day range.
// Deliberately NO status predicate anywhere in this statement — no
// `status IN (...)` filter, no status in the select list, no status in the
// GROUP BY. This mirrors admission.go:287-292's own daily-usage subqueries
// exactly: migration 0014 widened client_usage_daily's CHECK to admit
// 'canceled' rows, and evaluate() sums the table unfiltered. Adding a status
// predicate here would reintroduce the enforcement/visibility drift D-05
// exists to prevent.
const usageTerminalSQL = `
	SELECT client_id, engine,
		COALESCE(SUM(job_count), 0),
		COALESCE(SUM(bytes_in), 0),
		COALESCE(SUM(bytes_out), 0)
	FROM client_usage_daily
	WHERE day BETWEEN $1::date AND $2::date
		AND ($3::uuid IS NULL OR client_id = $3)
	GROUP BY client_id, engine`

// usageInFlightJobsSQL counts live (queued/active) jobs per (client, engine).
// The two statuses are written as SQL LITERALS, never bound $n parameters —
// this is what lets the query planner match jobs_client_engine_inflight_idx's
// partial predicate (its WHERE clause matches the literal-status form below,
// 0013_client_limits.sql). Binding them as parameters instead would silently
// degrade the plan to a sequential scan of jobs, exactly as
// admission.go:295-304 explains for evaluate()'s own in-flight subqueries.
// Adding the client_id IS NOT NULL term below does not affect this partial
// index's applicability: jobs_client_engine_inflight_idx's predicate is on
// status only, so it stays matched regardless of what else is in the WHERE
// clause.
//
// Deliberately NO created_at / day predicate here — matching
// admission.go:306-347: a job that is queued or active right now consumes
// capacity right now regardless of when it was created, and adding a date
// predicate would introduce a timestamptz-vs-date comparison with a hidden
// session-TimeZone dependency.
const usageInFlightJobsSQL = `
	SELECT client_id, engine, count(*)
	FROM jobs
	WHERE status IN ('queued', 'active')
		AND client_id IS NOT NULL -- jobs.client_id is nullable with ON DELETE SET NULL (0001_init.sql:43); a deleted client's still-queued/active jobs would otherwise collapse into one NULL group that scans into uuid.Nil without error, fabricating a phantom client row carrying enforcement_mode: "" — a value docs/openapi.yaml declares as enum: [observe, enforce] and required. client_usage_daily.client_id is NOT NULL + ON DELETE CASCADE, so the terminal term (usageTerminalSQL) needs no equivalent guard.
		AND ($1::uuid IS NULL OR client_id = $1)
	GROUP BY client_id, engine`

// usageInFlightBytesSQL sums live input bytes per (client, engine). This is a
// SEPARATE statement from usageInFlightJobsSQL, rather than one JOINed query,
// because a job with N job_inputs rows would otherwise contribute N to a
// count(*) over the join — a job must contribute exactly 1 to the job count
// and N sizes to the byte sum. Two separate grouped queries is the faithful
// generalization of evaluate()'s own two-separate-subqueries structure
// (admission.go:289-294).
//
// Like usageInFlightJobsSQL, the two statuses are SQL literals, not bound
// parameters, for the same jobs_client_engine_inflight_idx partial-index
// reason. Deliberately NO created_at / day predicate — see
// usageInFlightJobsSQL's comment above.
const usageInFlightBytesSQL = `
	SELECT client_id, engine, COALESCE(sum(bytes), 0)
	FROM (
		SELECT j.client_id, j.engine, ji.size_bytes AS bytes
		FROM jobs j
		JOIN job_inputs ji ON ji.job_id = j.id
		WHERE j.admitted_input_bytes IS NULL
			AND j.status IN ('queued', 'active')
			AND j.client_id IS NOT NULL -- jobs.client_id is nullable with ON DELETE SET NULL (0001_init.sql:43); a deleted client's still-queued/active jobs would otherwise collapse into one NULL group that scans into uuid.Nil without error, fabricating a phantom client row carrying enforcement_mode: "" — a value docs/openapi.yaml declares as enum: [observe, enforce] and required. client_usage_daily.client_id is NOT NULL + ON DELETE CASCADE, so the terminal term (usageTerminalSQL) needs no equivalent guard.
			AND ($1::uuid IS NULL OR j.client_id = $1)
		UNION ALL
		SELECT j.client_id, j.engine, j.admitted_input_bytes
		FROM jobs j
		WHERE j.admitted_input_bytes IS NOT NULL
			AND j.status IN ('queued', 'active')
			AND j.client_id IS NOT NULL
			AND ($1::uuid IS NULL OR j.client_id = $1)
	) accounted
	GROUP BY client_id, engine`

// usageKey identifies one (client, engine) group while accumulating
// UsageSummary's result — the key set is the UNION of keys produced by all
// three queries, so a (client, engine) pair with only terminal rows, or only
// in-flight rows, still appears in the output.
type usageKey struct {
	clientID uuid.UUID
	engine   string
}

// UsageSummary returns per-(client, engine) terminal usage over the
// inclusive UTC day range [dayFrom, dayTo] PLUS the live in-flight
// (queued/active) term, using the exact SQL shape evaluate() uses (D-05).
//
// clientID == nil means "all clients" (D-01 primary shape); a non-nil
// clientID narrows to that one client — one code path, one optional bind,
// not two branches. dayFrom/dayTo are an INCLUSIVE UTC day range; the caller
// is responsible for normalizing them to UTC midnight.
//
// The returned slice is sorted deterministically by ClientID.String()
// ascending, then Engine ascending — the consumption sort (D-02) is the HTTP
// layer's job, not this method's. Returns a non-nil empty slice when there
// are no rows.
//
// This method carries no authorization of its own — authorization is the
// HTTP layer's job (requireOperator, plan 53-04). This method must never be
// reached from a client-scoped route.
func (r *Repo) UsageSummary(ctx context.Context, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]UsageRow, error) {
	rows := make(map[usageKey]*UsageRow)

	get := func(id uuid.UUID, engine string) *UsageRow {
		k := usageKey{clientID: id, engine: engine}
		row, ok := rows[k]
		if !ok {
			row = &UsageRow{ClientID: id, Engine: engine}
			rows[k] = row
		}
		return row
	}

	terminalRows, err := r.pool.Query(ctx, usageTerminalSQL, dayFrom, dayTo, clientID)
	if err != nil {
		return nil, fmt.Errorf("read terminal usage: %w", err)
	}
	for terminalRows.Next() {
		var id uuid.UUID
		var engine string
		var jobCount, bytesIn, bytesOut int64
		if err := terminalRows.Scan(&id, &engine, &jobCount, &bytesIn, &bytesOut); err != nil {
			terminalRows.Close()
			return nil, fmt.Errorf("scan terminal usage: %w", err)
		}
		row := get(id, engine)
		row.TerminalJobCount = jobCount
		row.TerminalBytesIn = bytesIn
		row.TerminalBytesOut = bytesOut
	}
	terminalRows.Close()
	if err := terminalRows.Err(); err != nil {
		return nil, fmt.Errorf("read terminal usage: %w", err)
	}

	inFlightJobRows, err := r.pool.Query(ctx, usageInFlightJobsSQL, clientID)
	if err != nil {
		return nil, fmt.Errorf("count in-flight jobs: %w", err)
	}
	for inFlightJobRows.Next() {
		var id uuid.UUID
		var engine string
		var count int64
		if err := inFlightJobRows.Scan(&id, &engine, &count); err != nil {
			inFlightJobRows.Close()
			return nil, fmt.Errorf("scan in-flight jobs: %w", err)
		}
		get(id, engine).InFlightJobCount = count
	}
	inFlightJobRows.Close()
	if err := inFlightJobRows.Err(); err != nil {
		return nil, fmt.Errorf("count in-flight jobs: %w", err)
	}

	inFlightBytesRows, err := r.pool.Query(ctx, usageInFlightBytesSQL, clientID)
	if err != nil {
		return nil, fmt.Errorf("sum in-flight bytes: %w", err)
	}
	for inFlightBytesRows.Next() {
		var id uuid.UUID
		var engine string
		var bytesIn int64
		if err := inFlightBytesRows.Scan(&id, &engine, &bytesIn); err != nil {
			inFlightBytesRows.Close()
			return nil, fmt.Errorf("scan in-flight bytes: %w", err)
		}
		get(id, engine).InFlightBytesIn = bytesIn
	}
	inFlightBytesRows.Close()
	if err := inFlightBytesRows.Err(); err != nil {
		return nil, fmt.Errorf("sum in-flight bytes: %w", err)
	}

	out := make([]UsageRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClientID.String() != out[j].ClientID.String() {
			return out[i].ClientID.String() < out[j].ClientID.String()
		}
		return out[i].Engine < out[j].Engine
	})
	return out, nil
}

// UTCToday returns the database server's current UTC date, at UTC midnight,
// by reading the same day expression evaluate() (admission.go) uses to
// resolve "today" for the daily quota check. This exists so the operator
// visibility endpoint and
// evaluate() resolve "today" from ONE clock, the database server's, not two.
// Deriving the day from the API host's wall clock instead lets a
// UTC-midnight crossing or ordinary NTP skew make the endpoint query one day
// while the admission gate enforces another, while the response still
// claims remaining_matches_admission_gate: true (review finding WR-03, the
// exact D-05 drift this package's own header comment claims to have
// closed). This costs one extra round trip per operator request, deliberately
// accepted for an operator-only, rate-limited route.
//
// The returned value is exactly what Postgres returns — this method does not
// re-truncate or re-zone it in Go, because the entire point is that Go never
// touches the value.
func (r *Repo) UTCToday(ctx context.Context) (time.Time, error) {
	var d time.Time
	if err := r.pool.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'utc')::date`).Scan(&d); err != nil {
		return time.Time{}, fmt.Errorf("read utc today: %w", err)
	}
	return d, nil
}
