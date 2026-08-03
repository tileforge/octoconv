// Package jobs (this file): the QUOT/FAIR admission gate (QUOT-01, QUOT-02,
// QUOT-04, FAIR-01, Phase 52).
//
// The authoritative admission check runs INSIDE Repo.Create's existing
// transaction, as the first statement before INSERT INTO jobs (repo.go) --
// never as a separate pre-check transaction. A bare count query committed in
// its own transaction, followed by an insert in a second transaction, lets
// two API replicas both observe "below cap" and both admit (ROADMAP SC2a,
// D-07). Serializing the count and the insert under one advisory-locked
// transaction is what makes the cap hold under N replicas.
//
// There is deliberately NO slot-release code anywhere in this file, or
// anywhere else in this codebase (D-08). Concurrency occupancy is derived
// live from jobs.status -- queued + active jobs occupy a slot (D-06) -- so
// every terminal transition (done, failed, canceled, and the reconciler's
// exhaustion path via MarkFailed) frees the slot as a direct consequence of
// the status change that ALREADY happens, with zero additional code. A
// dedicated counter column keeping its own running tally was considered and
// rejected: its drift failure modes -- a slot that leaks and never frees, or
// a count that goes negative -- are permanent, unlike an occupancy count
// derived live from jobs.status, which can never drift from ground truth
// because it IS ground truth.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// enforcementModeObserve/enforcementModeEnforce mirror the two-value CHECK
// on clients.enforcement_mode (0013_client_limits.sql, D-13). observe is the
// column's DEFAULT, so a client with no explicit mode set behaves as if
// enforcement is off -- reported, never rejected -- until an operator opts
// it in.
const (
	enforcementModeObserve = "observe"
	enforcementModeEnforce = "enforce"
)

// Quota sentinels returned by PreAdmit and, via Repo.Create, by admitLocked.
// Three distinct sentinels -- never one generic ErrQuotaExceeded -- are what
// let the handler compute a per-limit-type Retry-After via errors.Is (D-11):
// a daily rejection returns the seconds remaining until UTC midnight, a
// concurrency rejection returns a short constant, because a slot frees
// unpredictably.
var (
	ErrConcurrencyLimit = errors.New("concurrency limit exceeded")
	ErrDailyBytesLimit  = errors.New("daily byte limit exceeded")
	ErrDailyJobsLimit   = errors.New("daily job-count limit exceeded")
)

// Quota reason labels. The SAME taxonomy is reused for both the observe-mode
// would-be-rejected signal and any live-enforcement rejection (D-14) -- one
// metric, one set of reasons, not two.
const (
	QuotaReasonConcurrency = "concurrency"
	QuotaReasonDailyBytes  = "daily_bytes"
	QuotaReasonDailyJobs   = "daily_jobs"
)

// QuotaReason maps a quota sentinel to its metric/log reason label,
// returning "" for any non-quota error. The handler and the quota observer
// both call this, so the reason taxonomy exists in exactly one place.
func QuotaReason(err error) string {
	switch {
	case errors.Is(err, ErrConcurrencyLimit):
		return QuotaReasonConcurrency
	case errors.Is(err, ErrDailyBytesLimit):
		return QuotaReasonDailyBytes
	case errors.Is(err, ErrDailyJobsLimit):
		return QuotaReasonDailyJobs
	default:
		return ""
	}
}

// SetQuotaObserver installs the callback fired once per rejected-or-would-
// be-rejected job admission (D-14): once when admitLocked (or PreAdmit)
// finds a violation while enforcement is off for that client (observe mode
// or the global kill-switch), and once when a violation is actually
// enforced. Startup-only wiring -- called once before the repo serves
// traffic, identical contract to api.Server.SetCancelPublisher. quotaObserver
// exists so this package never imports internal/metrics: the closure
// cmd/api/main.go installs here is what calls
// metrics.RecordQuotaWouldReject. Never calling this leaves quotaObserver at
// its zero value (nil), which every call site below treats as a no-op,
// never a panic.
func (r *Repo) SetQuotaObserver(fn func(clientID uuid.UUID, engine, reason string)) {
	r.quotaObserver = fn
}

// SetQuotaEnforcementDisabled installs the global kill-switch (D-13): when
// disabled is true, every quota violation on every client and engine is
// still reported (the observer still fires) but is never rejected, service-
// wide, overriding any per-client enforcement_mode. Startup-only wiring,
// same contract as SetQuotaObserver.
func (r *Repo) SetQuotaEnforcementDisabled(disabled bool) {
	r.quotaDisabled = disabled
}

// quotaLockKeys derives the two int32 keys passed to the transaction-scoped,
// two-key advisory lock admitLocked acquires for a given (client_id,
// engine) pair. FNV-1a (stdlib hash/fnv) is used rather than Postgres's own
// undocumented internal hash function, so the mapping is deterministic,
// testable, and identical across every replica and every Postgres version.
// The uint32->int32 conversion is a well-defined, deterministic
// two's-complement reinterpretation (never a random/varying bit pattern), so
// the same (clientID, engine) pair always yields the same key pair,
// everywhere.
//
// This key space cannot collide with internal/reconciler's leader-election
// lock (internal/reconciler/reconciler.go): that lock is a single, fixed,
// session-scoped, non-blocking lock keyed by ONE bigint, electing exactly
// one fleet-wide sweeper. This admission lock is transaction-scoped,
// blocking, and keyed by TWO ints, one pair PER (client_id, engine).
// PostgreSQL's advisory-lock key spaces for the one-bigint form and the
// two-int form are entirely separate, so the two locks are structurally
// incapable of colliding even in principle -- not merely tuned to avoid it.
func quotaLockKeys(clientID uuid.UUID, engine string) (int32, int32) {
	h1 := fnv.New32a()
	_, _ = h1.Write([]byte(clientID.String()))
	h2 := fnv.New32a()
	_, _ = h2.Write([]byte(engine))
	return int32(h1.Sum32()), int32(h2.Sum32())
}

// pgxQuerier is the minimal QueryRow-only interface satisfied by both
// *pgxpool.Pool and pgx.Tx, so loadQuotaState and evaluate serve both the
// non-locking pre-check (PreAdmit, on r.pool) and the authoritative check
// (admitLocked, on tx) with one implementation each, with no duplication.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// quotaState is the per-(client, engine) enforcement mode plus the three
// nullable limits, as loaded by loadQuotaState.
type quotaState struct {
	enforcementMode string
	maxConcurrent   *int
	maxDailyBytes   *int64
	maxDailyJobs    *int64
}

// unlimited reports whether none of the three limits are set -- the ROADMAP
// SC1 fast path: a client with no client_limits row, or a row with every
// column NULL, is unlimited (D-02), and both cases collapse to this same
// check.
func (st quotaState) unlimited() bool {
	return st.maxConcurrent == nil && st.maxDailyBytes == nil && st.maxDailyJobs == nil
}

// loadQuotaState reads clients.enforcement_mode and client_limits in a
// single statement. The LEFT JOIN is what makes "no client_limits row at
// all" and "a row with every column NULL" indistinguishable at the read
// site -- both yield nil pointers on the three limit fields, both mean
// unlimited (D-02).
func loadQuotaState(ctx context.Context, q pgxQuerier, clientID uuid.UUID, engine string) (quotaState, error) {
	var st quotaState
	err := q.QueryRow(ctx, `
		SELECT c.enforcement_mode, l.max_concurrent, l.max_daily_bytes, l.max_daily_jobs
		FROM clients c
		LEFT JOIN client_limits l ON l.client_id = c.id AND l.engine = $2
		WHERE c.id = $1`,
		clientID, engine,
	).Scan(&st.enforcementMode, &st.maxConcurrent, &st.maxDailyBytes, &st.maxDailyJobs)
	if err != nil {
		return quotaState{}, fmt.Errorf("load quota state: %w", err)
	}
	return st, nil
}

// evaluate runs the concurrency-then-daily checks for an already-loaded
// state st, using q for the count/usage reads (r.pool for PreAdmit's
// non-locking read, tx for admitLocked's authoritative post-lock re-check).
// Returns the quota verdict sentinel as the first result (nil when
// admitted) and a real infrastructure error as the second -- an
// infrastructure error is never mapped to an admit by any caller. Checks
// run in this order and the first violation wins, so the reason taxonomy is
// deterministic:
//  1. concurrency: queued+active occupancy vs max_concurrent
//  2. daily bytes / daily jobs: today's client_usage_daily rollup PLUS
//     currently in-flight (queued+active) admitted work vs the daily limits
func evaluate(ctx context.Context, q pgxQuerier, st quotaState, clientID uuid.UUID, engine string, inputBytes int64) (error, error) {
	if st.maxConcurrent != nil {
		var count int
		if err := q.QueryRow(ctx, `
			SELECT count(*) FROM jobs
			WHERE client_id = $1 AND engine = $2 AND status IN ($3, $4)`,
			clientID, engine, StatusQueued, StatusActive,
		).Scan(&count); err != nil {
			return nil, fmt.Errorf("count in-flight jobs: %w", err)
		}
		// >= is deliberate: it is what makes an explicit max_concurrent = 0
		// deny the engine entirely (0013_client_limits.sql).
		if count >= *st.maxConcurrent {
			return ErrConcurrencyLimit, nil
		}
	}

	if st.maxDailyBytes != nil || st.maxDailyJobs != nil {
		// D-15 (superseded 2026-08-03, see 52-CONTEXT.md's dated amendment
		// under "Accepted imprecision (SC2b)" -- the original text there is
		// left intact, not rewritten): this check now compares today's
		// committed client_usage_daily rollup PLUS all currently in-flight
		// (queued+active) admitted work for this (client, engine) against
		// the limit, including the incoming request's own contribution.
		//
		// Why there is no double count: a job has exactly one status at any
		// instant; client_usage_daily rows exist only for the terminal
		// statuses recordUsage writes (usage.go); queued/active jobs are
		// never in the rollup. The crossover from the in-flight term to the
		// rollup term happens atomically inside Repo.transition's
		// transaction (the same transaction that flips jobs.status AND
		// calls recordUsage), so no interleaving can ever observe a job
		// counted in both terms, or in neither.
		//
		// Why this is safe to compute here: admitLocked already holds
		// pg_advisory_xact_lock on this same tx by the time evaluate runs,
		// so the in-flight count below is serialized against every other
		// admission attempt for this exact (client, engine) pair -- an
		// unlocked read would reintroduce the precise race ROADMAP SC2(a)
		// and plan 52-03 exist to close.
		//
		// What residual remains (ROADMAP SC2b still asks for a disclosed
		// bound): the daily cap is now exact at the admission boundary in
		// every operator configuration, and the only remaining slack is
		// PreAdmit's unlocked pre-upload gate, which can let a concurrent
		// burst upload before admitLocked rejects it -- wasted uploads,
		// never over-admission, already bounded and measured by plan
		// 52-05's TestQuotaAdmission/ConcurrentBurstBoundedUploadWaste.
		var bytesUsed, jobsUsed int64
		if err := q.QueryRow(ctx, `
			SELECT
				COALESCE((SELECT sum(bytes_in) FROM client_usage_daily
					WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
				+ COALESCE((SELECT sum(ji.size_bytes) FROM jobs j JOIN job_inputs ji ON ji.job_id = j.id
					WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0),
				COALESCE((SELECT sum(job_count) FROM client_usage_daily
					WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
				+ COALESCE((SELECT count(*) FROM jobs j
					WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0)`,
			// The in-flight subqueries above write their two statuses as SQL
			// literals, NOT as bound parameters: the literal form is what the
			// planner can match against jobs_client_engine_inflight_idx's
			// partial predicate (WHERE status IN ('queued', 'active')), and
			// this query runs while holding the per-(client, engine)
			// advisory lock, so a sequential scan here would hold that lock
			// across every in-flight row in the whole table. This is
			// intentionally NOT unified with the pre-existing concurrency
			// count query above, which keeps binding its statuses as $3/$4
			// (52-REVIEW.md WR-03, out of scope for this plan).
			//
			// Deliberately NO created_at predicate on the in-flight term:
			// the terminal term is scoped to today's UTC date because that
			// is the `day` value recordUsage writes, while the in-flight
			// term is date-agnostic because a job that is queued or active
			// right now is consuming capacity right now, regardless of when
			// it was created. Two consequences: (a) a job in flight across a
			// UTC midnight is charged against each day it is in flight and
			// again against the day it terminates -- deliberately
			// conservative and fail-closed rather than a free window at the
			// day boundary; (b) no timestamptz-versus-date comparison is
			// introduced, so this check never acquires a dependency on the
			// session TimeZone setting, which a
			// `j.created_at >= (now() AT TIME ZONE 'utc')::date` predicate
			// would silently have.
			clientID, engine,
		).Scan(&bytesUsed, &jobsUsed); err != nil {
			return nil, fmt.Errorf("read daily usage: %w", err)
		}
		// The incoming request's own input size, and its own +1 job,
		// participate in the comparison -- the limit means "would exceed",
		// not "already exceeded".
		if st.maxDailyBytes != nil && bytesUsed+inputBytes > *st.maxDailyBytes {
			return ErrDailyBytesLimit, nil
		}
		if st.maxDailyJobs != nil && jobsUsed+1 > *st.maxDailyJobs {
			return ErrDailyJobsLimit, nil
		}
	}

	return nil, nil
}

// admitLocked is the authoritative admission path (D-07). Repo.Create calls
// this as the first statement inside its existing transaction, before
// INSERT INTO jobs (see repo.go). report and disabled carry the calling
// Repo's quotaObserver/quotaDisabled through as plain values rather than a
// receiver, so this stays a free function callable with exactly the
// arguments Create already has in scope. Infrastructure errors are wrapped
// and abort the transaction; they are NEVER converted into an admit -- only
// the explicit observe-mode/kill-switch branch below turns a real violation
// into an admit, and it reports the observer first.
//
// Isolation dependency (52-REVIEW.md WR-02): the re-evaluate call below,
// after pg_advisory_xact_lock, only observes the inserts committed by
// admissions that finished while THIS transaction waited on the lock --
// and that is true only under READ COMMITTED. Under REPEATABLE READ the
// re-count would instead be served from the snapshot taken by the pre-lock
// loadQuotaState above, the cap would silently over-admit, and nothing
// would raise a serialization failure because the concurrent inserts do
// not conflict with each other. This is why Repo.Create pins
// pgx.TxOptions{IsoLevel: pgx.ReadCommitted} explicitly rather than
// trusting default_transaction_isolation, and
// TestAdmissionHoldsUnderRepeatableReadDefault proves the cap still holds
// even when the deployment default is the level that would have broken it.
func admitLocked(ctx context.Context, tx pgx.Tx, clientID uuid.UUID, engine string, inputBytes int64, report func(clientID uuid.UUID, engine, reason string), disabled bool) error {
	// Pre-lock read: decides ONLY whether locking (and the authoritative
	// re-check) is worth doing at all. A (client, engine) pair with no
	// limits takes NO lock and runs NO further query (ROADMAP SC1).
	st, err := loadQuotaState(ctx, tx, clientID, engine)
	if err != nil {
		return fmt.Errorf("quota admission: %w", err)
	}
	if st.unlimited() {
		return nil
	}

	k1, k2 := quotaLockKeys(clientID, engine)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, k1, k2); err != nil {
		return fmt.Errorf("quota admission: acquire advisory lock: %w", err)
	}

	// Re-run the count/daily evaluation on tx, now serialized against every
	// other admission attempt for this exact (client, engine) pair -- the
	// pre-lock state read above is only used to decide whether locking was
	// worth it, never as the basis for the actual verdict.
	verdict, err := evaluate(ctx, tx, st, clientID, engine, inputBytes)
	if err != nil {
		return fmt.Errorf("quota admission: %w", err)
	}
	if verdict == nil {
		return nil
	}

	reason := QuotaReason(verdict)
	if report != nil {
		report(clientID, engine, reason)
	}
	if disabled || st.enforcementMode == enforcementModeObserve {
		// Global kill-switch or per-client observe mode (D-13): report but
		// admit. The observer has already fired above -- exactly once per
		// rejected-or-would-be-rejected job on this path, in both modes
		// (D-14).
		return nil
	}
	return verdict
}

// PreAdmit is the cheap, NON-authoritative pre-upload gate (ROADMAP SC2b),
// run on r.pool with NO advisory lock -- the handler calls it before
// writing the client's upload to S3, so an over-quota client does not burn
// S3 bandwidth and storage on every attempt. admitLocked, not PreAdmit, is
// the correctness mechanism (D-07): it is what actually enforces the cap
// under concurrent API replicas.
//
// When enforcement is off for this client (observe mode or the global
// kill-switch), PreAdmit returns nil WITHOUT firing the observer --
// admitLocked fires it when Create runs, and firing here too would
// double-count every observe-mode job.
//
// When enforcement is on and a violation is found, PreAdmit fires the
// observer and returns the sentinel, so the handler can 429 before writing
// to S3; admitLocked never runs for that rejected request, so there is
// still exactly one increment.
//
// What PreAdmit is NOT: it is an optimization, not the correctness
// mechanism. A request that passes PreAdmit and is then rejected by
// admitLocked leaves its just-uploaded object in place for TTL cleanup,
// exactly like the pre-existing preset-TOCTOU path in handleCreateJob
// already does.
//
// Because PreAdmit shares evaluate with admitLocked, this pre-upload gate
// now reaches the same daily verdict (committed rollup plus in-flight
// admitted work) as the authoritative check -- while remaining
// non-authoritative, because this read is unlocked.
//
// The concurrent burst residual, disclosed and bounded the same honest way
// the rest of this package's residuals are: an unlocked read cannot
// serialize, so if a client currently holding fewer than K in-flight jobs
// issues N requests simultaneously, every one of them reads the same
// below-cap count before any of them reaches Create's insert -- all N pass
// this pre-check, all N upload, and admitLocked then rejects N minus K of
// them AFTER their upload. Wasted uploads are therefore bounded by
// (N - K) x the per-engine input ceiling enforced ahead of the upload in
// handleCreateJob, per concurrent burst, with N itself bounded by the
// per-client rate limiter that runs upstream in the router
// (internal/ratelimit), never by this package. K is enforced structurally
// by this same admission code (exactly, for concurrency and daily limits
// alike, per evaluate's comment above), whereas N's ceiling is
// internal/ratelimit's operator-configured fixed-window RPM counter -- an
// independently-tuned knob in another package, not a burst or concurrency
// control -- so N may exceed K by orders of magnitude, and an operator
// tuning RPM is implicitly tuning this bound too. What ROADMAP SC2b actually
// cares about holds regardless: the
// waste is a one-time transient AT the cap boundary, never per-attempt --
// once the client sits at its cap, every subsequent request reads a count
// >= K here and is rejected before the S3 write, so a client retrying
// against a full cap burns nothing. Plan 52-05's
// TestQuotaAdmission/ConcurrentBurstBoundedUploadWaste measures the burst
// number and pins the steady-state zero, so the residual is a measured
// quantity, not an unknown.
//
// Deviation from the literal wording of ROADMAP SC2b, recorded for the
// record: SC2b asks for an authoritative reservation before the S3 write
// with a release path on later failure. CONTEXT.md D-07/D-08/D-15
// deliberately chose no reservation record at all -- occupancy is derived
// from jobs.status -- so this pre-upload gate is advisory and the
// authoritative gate sits at the insert (admitLocked). The user-visible
// property SC2b protects -- an over-quota client does not repeatedly burn
// S3 before its 429 -- is delivered for the sequential-retry case SC2b
// describes, with the concurrent burst window above stated as an accepted,
// bounded and measured residual; the reservation-leak failure class SC2b's
// release path would create is avoided entirely.
func (r *Repo) PreAdmit(ctx context.Context, clientID uuid.UUID, engine string, inputBytes int64) error {
	st, err := loadQuotaState(ctx, r.pool, clientID, engine)
	if err != nil {
		return fmt.Errorf("quota pre-admit: %w", err)
	}
	if st.unlimited() {
		return nil
	}

	verdict, err := evaluate(ctx, r.pool, st, clientID, engine, inputBytes)
	if err != nil {
		return fmt.Errorf("quota pre-admit: %w", err)
	}
	if verdict == nil {
		return nil
	}

	if r.quotaDisabled || st.enforcementMode == enforcementModeObserve {
		return nil
	}
	if r.quotaObserver != nil {
		r.quotaObserver(clientID, engine, QuotaReason(verdict))
	}
	return verdict
}
