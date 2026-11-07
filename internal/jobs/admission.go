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
//
// Unlike internal/ratelimit -- whose package comment documents that its
// in-process httprate counter stops being shared the moment the API is
// horizontally scaled, with an explicit upgrade path to a Redis-backed
// counter for that day -- this mechanism carries NO api.replicas: 1-only
// caveat. The reason is structural, not empirical: the per-(client, engine)
// pg_advisory_xact_lock plus the COUNT run in the SAME transaction as the
// INSERT (see admitLocked below) is correct under any number of API
// replicas by construction, because Postgres, not process memory, is what
// serializes concurrent admissions.
//
// That structural argument is now also confirmed by direct live
// observation, closing ROADMAP Phase 55 SC1/SC2 (QUOT-05):
// scripts/quota-multireplica-liveproof.sh ran ten independent rounds of ten
// concurrent job-creation requests against a real two-pod api.replicas: 2
// deployment (image engine, max_concurrent=2), and every round admitted
// exactly K=2 and rejected exactly 8 with quota_exceeded, with zero
// rate_limited responses. Every round's cap decision was split across both
// serving pods (api-6769bc54f7-frgdx, api-6769bc54f7-t8j4m), and the
// aggregate coverage over the whole run was pod1 202=10/429=42, pod2
// 202=10/429=38 -- both pods served both outcomes. 10/10 rounds passed,
// 167 assertions passed, run id 20260807T153358Z. Since plan 55-07, each
// round additionally proves from Postgres, BEFORE asserting admitted==K,
// that no admitted job reached a terminal status at or before that round's
// last response, so the exact-K count rests on a proven premise rather
// than an assumed one. Evidence committed at
// .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/
// quota-multireplica-20260807T153358Z.tsv (per-request rows) and
// quota-multireplica-20260807T153358Z-timeline.tsv (per-admitted-job
// terminal timeline); the gate script and both files are re-runnable and
// re-derivable (the aggregate counts are re-derivable from the TSV with
// awk alone), so this claim stays falsifiable rather than merely asserted.
//
// What this run does NOT cover: only the concurrency limit (max_concurrent)
// was exercised live. The daily-jobs and daily-bytes caps were deliberately
// out of scope for the live gate (55-CONTEXT.md D-03) and remain covered
// only by the in-process burst tests in admission_test.go (e.g.
// TestConcurrentBurstAdmitsExactlyK) -- the correct scope for now, since
// those two limits share the same locked-transaction structure this
// comment already argues is replica-safe by construction, but that
// specific claim has not itself been exercised under N live replicas.
//
// The live run's parameters are likewise the bounds of what it observed,
// and nothing more: K=2, the image engine, and exactly 2 api replicas. It
// says nothing empirical about other cap values, other engine classes, or
// three-or-more replicas. Those rest on the structural argument above --
// which is generic in all three, since the advisory-lock key is derived
// per (client, engine) and the serialization happens in Postgres rather
// than in any replica -- but they have not been watched happening.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

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
//
// The two daily sentinels are now normally delivered wrapped in a
// *DailyQuotaError (see below), never bare, so any consumer MUST keep using
// errors.Is (or errors.As for DailyQuotaError itself) -- never ==, which
// would stop matching the moment a verdict arrives wrapped.
var (
	ErrConcurrencyLimit = errors.New("concurrency limit exceeded")
	ErrDailyBytesLimit  = errors.New("daily byte limit exceeded")
	ErrDailyJobsLimit   = errors.New("daily job-count limit exceeded")
)

// DailyQuotaError wraps ErrDailyBytesLimit or ErrDailyJobsLimit with the
// Postgres instant evaluate's daily branch was evaluated at (EvaluatedAt),
// so a caller computing a daily rejection's Retry-After can derive it
// against the SAME clock the daily-window filter used, rather than the API
// process's own wall clock at response-write time -- the bug closed by
// .planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md (a
// daily rejection could otherwise advertise Retry-After: ~86400 for a quota
// that had already reset, in the narrow window where the DB evaluation
// happens just before UTC midnight and the response is formatted just
// after).
//
// The sentinel stays the identity for errors.Is: Unwrap returns Err
// unchanged, so QuotaReason and every existing errors.Is(err,
// ErrDailyBytesLimit/ErrDailyJobsLimit) call site need no change at all.
// EvaluatedAt is Postgres's now() at transaction start -- the identical
// value recordUsage (usage.go) writes its client_usage_daily.day from -- not
// a value re-derived on the API side.
type DailyQuotaError struct {
	// Err is always ErrDailyBytesLimit or ErrDailyJobsLimit.
	Err error
	// EvaluatedAt is the Postgres now() the daily-window filter used.
	EvaluatedAt time.Time
}

func (e *DailyQuotaError) Error() string { return e.Err.Error() }

func (e *DailyQuotaError) Unwrap() error { return e.Err }

// Quota reason labels. The SAME taxonomy is reused for both the observe-mode
// would-be-rejected signal and any live-enforcement rejection (D-14) -- one
// metric, one set of reasons, not two.
const (
	QuotaReasonConcurrency = "concurrency"
	QuotaReasonDailyBytes  = "daily_bytes"
	QuotaReasonDailyJobs   = "daily_jobs"
)

// Quota status classification (Codex PR #5 review, P3). Every value in the
// jobs.status CHECK domain (jobs.go) must appear in EXACTLY one of these
// three sets; TestEveryJobStatusIsClassifiedForQuotaAccounting
// (admission_test.go) enforces that partition against the live schema, so a
// status that is added to the schema, or moved between these sets, cannot
// silently go unaccounted.
//
//   - quotaInFlightStatuses: the status set the in-flight term's SQL
//     literals in evaluate, and jobs_client_engine_inflight_idx's partial
//     predicate (0013_client_limits.sql), both use.
//   - quotaLedgerStatuses: the terminal statuses recordUsage (usage.go)
//     writes to client_usage_daily (migration 0014).
//   - quotaUnaccountedStatuses: statuses visible to NEITHER quota term.
//
// awaiting_upload is declared in jobs.go and admitted by the schema, but NO
// code path writes it -- its only other reference in the whole repo is a
// filter allow-list in internal/api/jobs_list_handler.go. It is therefore
// invisible to both quota terms today, with zero observable effect.
//
// Rejected alternative, rejected on correctness rather than cost: moving
// awaiting_upload into the in-flight predicate (which would also require
// changing jobs_client_engine_inflight_idx's predicate in a new migration)
// would commit to quota semantics for a status whose meaning does not exist
// yet. The byte term sums job_inputs.size_bytes; for a job still awaiting
// its upload that row is either absent -- in which case moving the status
// buys the byte term nothing -- or carries a client-declared size that has
// not been verified against any stored object. Deciding between those
// belongs to whichever phase actually introduces the two-phase upload flow,
// because that phase is the one that determines whether a trusted size
// exists at awaiting_upload time; pre-deciding it here would encode a
// semantic no test can exercise, since the status is unreachable through
// every repo API today.
//
// What the declaration buys instead: the omission stops being an accident
// of a SQL literal and becomes a named classification, so the day a status
// is added to the schema, or the in-flight index predicate changes, or
// someone moves awaiting_upload out of the unaccounted set, the invariant
// test forces the quota question to be answered rather than skipped.
//
// No SQL string and no query in this file changes because of this
// declaration -- the in-flight literals stay literals for the
// index-matching reason already documented in evaluate below.
var (
	quotaInFlightStatuses    = []string{StatusQueued, StatusActive}
	quotaLedgerStatuses      = []string{StatusDone, StatusFailed, StatusCanceled}
	quotaUnaccountedStatuses = []string{StatusAwaitingUpload}
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
// LockClientEngine takes the admission advisory lock for one client and engine.
func LockClientEngine(ctx context.Context, tx pgx.Tx, clientID uuid.UUID, engine string) error {
	k1, k2 := quotaLockKeys(clientID, engine)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, k1, k2); err != nil {
		return fmt.Errorf("lock client engine: %w", err)
	}
	return nil
}

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
		//
		// Returned bare, never wrapped in DailyQuotaError (or an analogous
		// wrapper): its Retry-After is the fixed
		// quotaConcurrencyRetryAfterSeconds constant, with no dependency on
		// any evaluation clock, so wrapping it would add a field nothing
		// reads.
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
		var evaluatedAt time.Time
		if err := q.QueryRow(ctx, `
			SELECT
				COALESCE((SELECT sum(bytes_in) FROM client_usage_daily
					WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
				+ COALESCE((SELECT sum(ji.size_bytes) FROM jobs j JOIN job_inputs ji ON ji.job_id = j.id
					WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0),
				COALESCE((SELECT sum(job_count) FROM client_usage_daily
					WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
				+ COALESCE((SELECT count(*) FROM jobs j
					WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0),
				-- Third column, added for DailyQuotaError.EvaluatedAt: bare
				-- now() (transaction-start time), NOT (now() AT TIME ZONE
				-- 'utc') as used by the day filters above. Selecting the bare
				-- form keeps it a timestamptz, so pgx returns an unambiguous
				-- instant; the API layer's secondsUntilUTCMidnight already
				-- normalizes via now.UTC(), so the zone work happens exactly
				-- once, there. Scanning a timestamp WITHOUT time zone here
				-- instead would be a silent zone trap: it would carry no
				-- offset and get reinterpreted at the read site.
				now()`,
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
			// it was created. Two consequences:
			//
			// (a) (Codex PR #5 review, P2a -- corrected; the prior text here
			// claimed a job in flight across UTC midnight was charged against
			// each day it is in flight AND again against the day it
			// terminates, "deliberately conservative and fail-closed" -- that
			// claim is false in a narrow window, and this is the verified
			// behavior instead.) While a job is queued or active it is
			// counted by the in-flight term above in every admission for its
			// (client, engine) pair, on whatever UTC day that admission
			// runs -- the in-flight term has no date predicate. When the job
			// terminates, recordUsage (usage.go) writes its ledger row with
			// day = (now() AT TIME ZONE 'utc')::date, and PostgreSQL's now()
			// is transaction-START time, not wall-clock time -- the same
			// now() this query uses for its own day filter above. Therefore
			// the ledger row lands on the UTC date at which the terminal
			// transition's transaction BEGAN, not the date at which it
			// committed. In the narrow window where a terminal transaction
			// begins before UTC midnight and commits after it -- for example
			// while blocked on the jobs-row SELECT ... FOR UPDATE -- the row
			// is written to the PREVIOUS day, and from the instant that
			// transaction commits the job appears in neither the new day's
			// in-flight term nor the new day's ledger term: its cost is
			// absent from the new day's total. This is accepted, not fixed:
			// clock_timestamp() would place the row on the commit-side day
			// but would make day non-deterministic within a transaction,
			// which is explicitly not wanted (recordUsage keeps now()); the
			// outcome is defensible because the job is charged in full to
			// the day its terminal transition began, and on the new day it
			// existed only for the tail of a transaction that was already
			// committing.
			//
			// (b) no timestamptz-versus-date comparison is
			// introduced, so this check never acquires a dependency on the
			// session TimeZone setting, which a
			// `j.created_at >= (now() AT TIME ZONE 'utc')::date` predicate
			// would silently have.
			clientID, engine,
		).Scan(&bytesUsed, &jobsUsed, &evaluatedAt); err != nil {
			return nil, fmt.Errorf("read daily usage: %w", err)
		}
		// The incoming request's own input size, and its own +1 job,
		// participate in the comparison -- the limit means "would exceed",
		// not "already exceeded".
		if st.maxDailyBytes != nil && bytesUsed+inputBytes > *st.maxDailyBytes {
			return &DailyQuotaError{Err: ErrDailyBytesLimit, EvaluatedAt: evaluatedAt}, nil
		}
		if st.maxDailyJobs != nil && jobsUsed+1 > *st.maxDailyJobs {
			return &DailyQuotaError{Err: ErrDailyJobsLimit, EvaluatedAt: evaluatedAt}, nil
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
