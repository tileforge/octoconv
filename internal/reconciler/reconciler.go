// Package reconciler periodically sweeps Postgres for jobs stranded in
// queued/active past a staleness threshold and requeues or terminally fails
// them, bounded by a recovery cap.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	// pgxpool is imported directly here — the one place in this package that
	// breaks its otherwise pure-interface-dependency style (jobStore/enqueuer
	// below). D-01/D-02 require Postgres session-level advisory-lock
	// semantics (pg_try_advisory_lock tied to a single dedicated connection's
	// lifetime), which no interface abstraction over jobStore can express.
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/metrics"
)

// advisoryLockKey is the Postgres session-level advisory-lock key used to
// elect exactly one fleet-wide sweeper (D-01/D-02). The value is arbitrary
// but fixed and must never collide with another subsystem's advisory-lock
// usage — there are none today (verified: zero other pg_try_advisory_lock
// callers in this codebase). If a second advisory-lock use is ever added, it
// MUST use a different key.
const advisoryLockKey int64 = 0x6F63746F

// Config tunes the sweep: how stale a queued/active job must be before it is
// considered stranded, how often to sweep, and how many recoveries a single
// job may accumulate before it is terminally failed.
type Config struct {
	QueuedStaleAfter time.Duration
	ActiveStaleAfter time.Duration
	SweepInterval    time.Duration
	MaxRecoveries    int

	// JobsRetention/UsageRetention/PurgeBatchSize tune the retention purge
	// (RETN-01, D-11/D-12/D-13) that runs on the same advisory-lock-gated
	// tick as sweep -- see purge() below. JobsRetention/UsageRetention <= 0
	// disables the respective purge entirely (jobs.PurgeJobs/PurgeUsage's
	// own guard, internal/jobs/retention.go); the zero value of this struct
	// (every pre-existing Config literal in this codebase, including every
	// test's) therefore leaves retention off, never "delete everything".
	JobsRetention  time.Duration
	UsageRetention time.Duration
	// PurgeBatchSize <= 0 falls back to defaultPurgeBatchSize below.
	PurgeBatchSize int
}

// defaultPurgeBatchSize is the fallback row-count-per-DELETE-statement bound
// used when Config.PurgeBatchSize is unset (<=0).
const defaultPurgeBatchSize = 1000

// maxPurgeBatchesPerSweep bounds how many PurgeJobs/PurgeUsage batches a
// single purge() call will issue for EACH kind, giving a hard per-tick
// ceiling on purge work (T-51-13, DoS mitigation) independent of how large
// the batch size itself is.
const maxPurgeBatchesPerSweep = 10

// jobStore is the subset of *jobs.Repo the sweeper depends on (interface
// segregation, mirroring internal/api/api.go), so Sweeper is unit-testable
// with an in-memory fake — no DB required.
type jobStore interface {
	FindStale(ctx context.Context, queuedStaleAfter, activeStaleAfter time.Duration) ([]jobs.StaleJob, error)
	RecoveryCount(ctx context.Context, id uuid.UUID) (int, error)
	RequeueStale(ctx context.Context, id uuid.UUID, reason string) error
	MarkFailedUnmeasured(ctx context.Context, id uuid.UUID, code, message string, detail map[string]any) error
	Get(ctx context.Context, id uuid.UUID) (*jobs.Job, error)
	FindWebhookGaps(ctx context.Context, activeStaleAfter time.Duration) ([]jobs.WebhookGapJob, error)
	RecordWebhookGapRecovered(ctx context.Context, id uuid.UUID, status string) error
	PurgeJobs(ctx context.Context, olderThan time.Duration, batch int) (int64, error)
	PurgeUsage(ctx context.Context, olderThan time.Duration, batch int) (int64, error)
}

// enqueuer is the subset of *queue.Client the sweeper depends on.
type enqueuer interface {
	EnqueueWebhookDeliver(ctx context.Context, id uuid.UUID) error
	EnqueueDocumentConvert(ctx context.Context, id uuid.UUID) error
	EnqueueProbeSanitize(ctx context.Context, id uuid.UUID) error
	EnqueueCADJob(ctx context.Context, id uuid.UUID) error
}

// Sweeper periodically scans for stale jobs and recovers or exhausts them.
type Sweeper struct {
	store jobStore
	enq   enqueuer
	cfg   Config
}

// NewSweeper builds a Sweeper. store and enq are typically *jobs.Repo and
// *queue.Client, which satisfy jobStore/enqueuer concretely.
func NewSweeper(store jobStore, enq enqueuer, cfg Config) *Sweeper {
	return &Sweeper{store: store, enq: enq, cfg: cfg}
}

// Run ticks every cfg.SweepInterval and calls sweep until ctx is cancelled,
// at which point it stops the ticker and returns promptly (no leaked
// goroutine).
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
			s.purge(ctx)
		}
	}
}

// AdvisoryLock elects exactly one fleet-wide sweeper across replicas
// (D-01/D-02). TryAcquire returns (true, nil) if this process now holds the
// fleet-wide sweep lock, (false, nil) if another holder currently has it,
// and a non-nil error when the lock state could not be determined — in
// which case the caller MUST treat the tick as fail-safe (skip sweeping
// rather than sweep unguarded).
type AdvisoryLock interface {
	TryAcquire(ctx context.Context) (bool, error)
}

// PGAdvisoryLock implements AdvisoryLock using Postgres session-level
// pg_try_advisory_lock on a single dedicated, long-lived connection. Session
// scope (not transaction scope) is required so the lock's lifetime is tied
// to the connection/process, not to any single query or transaction (D-02).
type PGAdvisoryLock struct {
	pool *pgxpool.Pool
	conn *pgxpool.Conn
	// mu serializes TryAcquire against a shutdown-time Close() invoked from a
	// different goroutine (main's defer chain races the not-yet-returned
	// sweeper goroutine's final tick).
	mu sync.Mutex
	// closed makes Close terminal: without it, a TryAcquire racing past a
	// completed Close would lazily re-acquire a fresh dedicated connection
	// that nothing ever releases, and pool.Close() would block forever on
	// the resurrected slot (DEFER-17-01).
	closed bool
}

// NewPGAdvisoryLock acquires one dedicated connection from pool and never
// releases it back for the life of the process (D-02: lock release must be
// tied to process/session lifetime, not returned to the pool where it could
// be recycled while still holding the session lock).
func NewPGAdvisoryLock(ctx context.Context, pool *pgxpool.Pool) (*PGAdvisoryLock, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire dedicated advisory-lock connection: %w", err)
	}
	return &PGAdvisoryLock{pool: pool, conn: conn}, nil
}

// TryAcquire attempts pg_try_advisory_lock(advisoryLockKey) on the dedicated
// connection. Once this session holds the lock, subsequent calls on the SAME
// session return true again (Postgres allows re-acquire by the owning
// session) — the intended steady state for the elected leader.
func (l *PGAdvisoryLock) TryAcquire(ctx context.Context) (bool, error) {
	// Held across the entire body (lazy re-acquire, the query, and the error
	// path) because Close() may run concurrently at shutdown and must not
	// Release the conn mid-query. Contention is near-zero (TryAcquire fires
	// once per sweep interval, Close once at shutdown).
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		// Terminal: after Close() the lock must never resurrect a dedicated
		// connection (it would leak and stall pool.Close, DEFER-17-01).
		// Fail-safe closed — the caller skips this sweep tick.
		return false, fmt.Errorf("advisory lock is closed")
	}

	if l.conn == nil {
		// Lost on a prior tick's error path — lazily re-acquire a fresh
		// dedicated connection.
		conn, err := l.pool.Acquire(ctx)
		if err != nil {
			return false, fmt.Errorf("re-acquire dedicated advisory-lock connection: %w", err)
		}
		l.conn = conn
	}

	var acquired bool
	if err := l.conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&acquired); err != nil {
		// The connection is now suspect. Hard-close the underlying pgconn
		// rather than Release() it back into the shared pool: a plain
		// Release() would hand a still-protocol-healthy connection back into
		// general pool circulation while it may STILL hold the session-level
		// advisory lock, tying the lock's release to pool recycling
		// (MaxConnLifetime) instead of process life — silently blocking any
		// worker from ever becoming sweeper leader (D-02 CRITICAL).
		// Hard-closing guarantees Postgres releases the session lock
		// immediately. Hard-close and Release are complementary, not
		// mutually exclusive (16-REVIEW.md CR-01): Close() alone would leave
		// the now-dead resource pinned in the pool forever (a permanent slot
		// leak), so Release() it after closing to reclaim the pgxpool slot.
		conn := l.conn
		l.conn = nil
		conn.Conn().Close(ctx)
		conn.Release()
		return false, fmt.Errorf("pg_try_advisory_lock: %w", err)
	}
	return acquired, nil
}

// Close releases the dedicated connection and with it the session-level
// advisory lock; safe to call at shutdown. Close is TERMINAL: any concurrent
// or subsequent TryAcquire returns an error instead of lazily re-acquiring a
// connection that nothing would release (DEFER-17-01). This is what lets
// pool.Close() return in bounded time (WR-01) instead of blocking forever on
// a never-released pgxpool slot. Idempotent.
func (l *PGAdvisoryLock) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.conn != nil {
		l.conn.Release()
		l.conn = nil
	}
}

// RunWithLock ticks every cfg.SweepInterval like Run, but gates each tick on
// lock.TryAcquire: sweep only runs when this process currently holds the
// fleet-wide advisory lock. Any TryAcquire error or a false result skips the
// tick without sweeping (fail-safe closed — never sweep on uncertainty or
// when another replica holds the lock, D-01/D-02).
func (s *Sweeper) RunWithLock(ctx context.Context, lock AdvisoryLock) {
	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := lock.TryAcquire(ctx)
			if err != nil || !ok {
				// Fail-safe / not-leader: skip this tick, try again next
				// tick. No logging here — same best-effort discipline as
				// sweep (visibility is job_events).
				continue
			}
			s.sweep(ctx)
			// purge is called here, immediately after sweep, NOT from
			// sweep's own tail — sweep returns early on any finder error
			// (FindStale/FindWebhookGaps), and hosting the purge at its tail
			// would let a persistently failing stale-scan silently disable
			// retention. This placement is still inside the TryAcquire gate
			// above, so exactly one fleet-wide replica ever purges (T-51-14,
			// reusing the same advisory-lock election sweep already uses) —
			// it is only OUTSIDE sweep()'s own error-return paths, not
			// outside the lock.
			s.purge(ctx)
		}
	}
}

// sweep scans for stale jobs and, for each one under the recovery cap,
// attempts an enqueue-first recovery guarded by asynq.ErrDuplicateTask; jobs
// at or over the cap are terminally failed with a webhook fired on
// exhaustion if callback_url is set. A per-job error is swallowed (best
// effort — the next tick retries) so one bad job never stalls the sweep. No
// logging is added here — visibility is limited to job_events (D-15); Phase
// 4 owns OBS logging/metrics.
//
// sweep also runs a SECOND, independent scan (RECON-04, Phase 6) for
// done/failed jobs whose completion webhook was silently never enqueued
// (e.g. a Redis blip at the exact moment the worker tried to fire
// EnqueueWebhookDeliver). This webhook-gap recovery is enqueue-first and
// asynq.ErrDuplicateTask-guarded exactly like the queued/active loop above,
// but it is a ONE-SHOT, self-terminating action: it uses
// detailActionWebhookGapRecovered (not detailActionRecovery), so
// RecoveryCount never counts it toward MaxRecoveries. There is nothing to
// "exhaust" — once a webhook_deliveries row exists (delivered, undelivered,
// or dead-lettered), FindWebhookGaps never matches that job again (D-05).
func (s *Sweeper) sweep(ctx context.Context) {
	stale, err := s.store.FindStale(ctx, s.cfg.QueuedStaleAfter, s.cfg.ActiveStaleAfter)
	if err != nil {
		// Best-effort: the next tick retries.
		return
	}

	for _, j := range stale {
		n, err := s.store.RecoveryCount(ctx, j.ID)
		if err != nil {
			// Best-effort: skip this job, the next tick retries.
			continue
		}

		if n >= s.cfg.MaxRecoveries {
			// Cap exceeded: terminally fail and, if a callback_url is set,
			// fire a webhook (D-14, Postgres-first best-effort — the failed
			// status is already committed by MarkFailed above, so a failed
			// enqueue must not undo it). The webhook enqueue below is
			// itself guarded on ferr == nil (RUNR-02) -- a webhook telling
			// the client the job failed must never fire off the back of a
			// MarkFailed call that did not actually commit; the row is
			// still stranded and the next sweep tick re-evaluates it.
			job, _ := s.store.Get(ctx, j.ID)
			// attempt=0: the reconciler never ran the engine and has no
			// attempt clock of its own -- recordUsage (D-05, internal/jobs/
			// usage.go) derives wall_seconds from the tx-locked jobs row's
			// started_at instead. For a job exhausted while still stranded
			// in `queued` (never activated, started_at NULL) that correctly
			// falls back to 0 -- it never occupied a worker at all.
			// Unmeasured, not zero: this path never ran the job, so the
			// elapsed time it would report is derived from started_at rather
			// than invented (66-02).
			ferr := s.store.MarkFailedUnmeasured(ctx, j.ID, "reconciler_exhausted", "recovery attempts exhausted", map[string]any{"action": "reconciler_exhausted"})
			// RUNR-02: RecordReconcilerAction("exhausted") is guarded on
			// ferr == nil for the same reason RecordClientJobOutcome below
			// already is -- MarkFailed's error was previously discarded
			// here, so a transition that never committed still inflated the
			// exhaustion count every sweep tick (the same job is retried
			// and would increment again on top). This is the exact defect
			// class PR #4 found and fixed in the seven Handle*Convert
			// wrappers (see internal/worker/runner.go's single guarded
			// finish() call site) but had not yet been closed here. Kept as
			// its own `if`, not merged with the guard below, because this
			// call does not read job and must not be skipped just because
			// job is nil.
			if ferr == nil {
				metrics.RecordReconcilerAction("exhausted")
			}
			if job != nil && ferr == nil {
				// Closes T-51-10 (Repudiation): a job the reconciler
				// terminally fails after exhausting recoveries was, before
				// this, counted in the Postgres rollup (via MarkFailed
				// above) but invisible per-client in Prometheus. Reuses the
				// job != nil guard already in place for the webhook enqueue
				// below rather than a second s.store.Get call.
				//
				// Also guarded on ferr: MarkFailed's error was previously
				// discarded here, so a transition that never committed still
				// produced a per-client failure. The next sweep retries the
				// same job and would increment again.
				metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
			}
			if job != nil && ferr == nil && job.CallbackURL != "" {
				_ = s.enq.EnqueueWebhookDeliver(ctx, j.ID)
			}
			continue
		}

		// Under the cap: attempt recovery ENQUEUE-FIRST so the asynq.Unique
		// lock (Plan 01) decides whether a task is genuinely needed. Only a
		// successful, non-duplicate enqueue proves the job is actually
		// stranded (no live task/lock) rather than merely backlogged or
		// still being retried by asynq. Which queue to recover onto is
		// decided by the job's engine class (DOC-09, D-04) — never
		// hardcoded to image, so a document job is never misrouted onto the
		// image queue.
		var enqueueErr error
		switch j.Engine {
		case convert.EngineDocument:
			job, _ := s.store.Get(ctx, j.ID)
			ferr := s.store.MarkFailedUnmeasured(ctx, j.ID, "engine_error", "unsupported conversion", map[string]any{"action": "document_retired"})
			if ferr == nil {
				metrics.RecordReconcilerAction("document_retired")
			}
			if job != nil && ferr == nil {
				metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
			}
			if job != nil && ferr == nil && job.CallbackURL != "" {
				_ = s.enq.EnqueueWebhookDeliver(ctx, j.ID)
			}
			continue
		case convert.EngineImage, convert.EngineHTML, convert.EngineAudio, convert.EngineAV, convert.EngineOCR, convert.EnginePandoc, convert.EngineTectonic:
			continue
		case convert.EngineProbe:
			// Added by Phase 60, which is the "future engine" the note below
			// was addressed to. Without this case a queued sanitize job whose
			// initial enqueue failed took the unroutable_engine branch on
			// every sweep and stayed queued forever -- the job row existed,
			// the client's quota was charged, and nothing would ever run it.
			// Caught in review (PR #12), not by a test.
			enqueueErr = s.enq.EnqueueProbeSanitize(ctx, j.ID)
		case convert.EngineCAD:
			job, _ := s.store.Get(ctx, j.ID)
			ferr := s.store.MarkFailedUnmeasured(ctx, j.ID, "engine_error", "unsupported conversion", map[string]any{"action": "cad_retired"})
			if ferr == nil {
				metrics.RecordReconcilerAction("cad_retired")
			}
			if job != nil && ferr == nil {
				metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
			}
			if job != nil && ferr == nil && job.CallbackURL != "" {
				_ = s.enq.EnqueueWebhookDeliver(ctx, j.ID)
			}
			continue
		default:
			// Fail closed (T-10-03): archive is out of scope this
			// milestone and a corrupted/unrecognized engine value must never
			// be guessed at. Do NOT enqueue and do NOT RequeueStale — either
			// would risk running the job through the wrong engine's worker
			// or silently losing it from the recovery cap accounting. A
			// future engine must add its own case here rather than fall
			// through to a default route. This is a clear, non-fatal,
			// metric-visible skip, not a crash — the job stays stranded and
			// is re-evaluated (unrecovered) on the next sweep tick.
			metrics.RecordReconcilerAction("unroutable_engine")
			continue
		}
		if enqueueErr != nil {
			if errors.Is(enqueueErr, asynq.ErrDuplicateTask) {
				// A live task/lock for this job already exists — the job is
				// backlogged or asynq is still retrying it, NOT stranded.
				// This is the expected, safe case: no status change, no
				// recovery event, so a merely-backlogged queued job can
				// never accrue a spurious recovery toward MaxRecoveries and
				// be falsely driven to reconciler_exhausted (RECON-01).
				continue
			}
			// Any other transient enqueue error: best-effort, retry next tick.
			continue
		}

		// Only after a SUCCESSFUL, non-duplicate enqueue do we flip the row
		// back to queued and record the recovery event. RequeueStale is
		// called AFTER (not before) the enqueue specifically so a
		// legitimately-backlogged job never accrues a spurious recovery.
		reason := "stale_" + j.Status
		if err := s.store.RequeueStale(ctx, j.ID, reason); err != nil {
			// A fresh task has ALREADY been enqueued at this point. If the
			// recovery event silently fails to record, RecoveryCount would
			// under-count against MaxRecoveries — a permanently-broken job
			// could then receive MORE than MaxRecoveries real re-enqueues
			// before finally being exhausted. To degrade gracefully instead
			// of silently under-counting, retry exactly ONCE more (bounded —
			// never an unbounded loop, so a persistently-failing DB never
			// stalls the sweep).
			if err := s.store.RequeueStale(ctx, j.ID, reason); err != nil {
				// Both attempts failed: discard the error. This job's
				// recovery event is lost for this sweep (the cap
				// under-counts by one); the already-enqueued fresh task
				// still processes idempotently via MarkActive, and the next
				// sweep re-checks this job's (still active/queued) state.
				//
				// The common benign case behind this path: a legitimately-
				// slow original worker completed the job between FindStale
				// and RequeueStale (status now done/failed), so
				// RequeueStale's guarded transition correctly returns an
				// illegal-transition error on both attempts — no recovery
				// was actually needed, and the freshly-enqueued task no-ops
				// at MarkActive.
				//
				// T-03-12 (accepted residual, symmetric over-count case): if
				// the FIRST RequeueStale call actually committed
				// server-side but the client observed a transport error
				// between COMMIT and ack (a case pgx.BeginFunc does not
				// exclude), this bounded retry runs a SECOND genuinely-
				// successful transition and writes a SECOND
				// reconciler_recovery event for ONE real recovery —
				// over-counting RecoveryCount by one, so the job could hit
				// reconciler_exhausted one recovery earlier than intended.
				// This is bounded (one extra event, since the retry is
				// single-shot), low-probability, and does NOT reopen the
				// no-duplicate-task guarantee (independently protected by
				// asynq.Unique, T-03-10) — accepted and documented rather
				// than defended with a pre-write idempotency check, since a
				// clean "was this exact recovery already recorded this
				// sweep" check against the append-only event log is not
				// straightforward and the cost (one early exhaustion in a
				// rare double-fault) is acceptable for the MVP.
				continue
			}
		}

		// Reached only after a genuinely successful RequeueStale (first
		// attempt or the single bounded retry) — never on the
		// asynq.ErrDuplicateTask continue above, which is a backlogged
		// no-op, not a recovery.
		metrics.RecordReconcilerAction("recovered")
	}

	// Second scan (RECON-04): done/failed jobs with a callback_url and zero
	// webhook_deliveries rows, past ActiveStaleAfter since finished_at
	// (D-04, reusing the existing threshold rather than a new one).
	// Best-effort on a finder error, same discipline as FindStale above.
	gaps, err := s.store.FindWebhookGaps(ctx, s.cfg.ActiveStaleAfter)
	if err != nil {
		return
	}

	for _, g := range gaps {
		// Enqueue-first: the asynq.Unique lock (Plan 01) is the actual
		// duplicate-delivery guard. Only a successful, non-duplicate
		// enqueue proves this is a genuine gap rather than a delivery
		// already live/queued for this job.
		if err := s.enq.EnqueueWebhookDeliver(ctx, g.ID); err != nil {
			if errors.Is(err, asynq.ErrDuplicateTask) {
				// A delivery is already live/queued for this job — not
				// actually a gap, skip silently (same reasoning as the
				// image-recovery loop's duplicate guard above).
				continue
			}
			// Any other transient enqueue error: best-effort, retry next tick.
			continue
		}

		// Only after a successful, non-duplicate enqueue: record the
		// recovery event and the metric. This is uncapped/single-shot —
		// RecordWebhookGapRecovered does not touch RecoveryCount/MaxRecoveries.
		_ = s.store.RecordWebhookGapRecovered(ctx, g.ID, g.Status)
		metrics.RecordReconcilerAction("webhook_gap_recovered")
	}
}

// purge runs the retention purge (RETN-01, D-11/D-12/D-13): up to
// maxPurgeBatchesPerSweep batches of jobs.PurgeJobs, then up to
// maxPurgeBatchesPerSweep batches of jobs.PurgeUsage, stopping each loop
// early once a batch deletes fewer rows than the batch size (nothing more to
// do this tick). Each non-zero batch is reported through
// metrics.RecordReconcilerPurge so an operator can see retention running
// (T-51-15). On any error, this returns silently — the same best-effort
// discipline as every other scan in sweep() above ("the next tick
// retries"); a batch that partially completed before an error still counts
// toward this tick's total.
//
// Called from Run/RunWithLock immediately after sweep(), never from inside
// sweep() itself — see the call sites' comments for why.
func (s *Sweeper) purge(ctx context.Context) {
	batch := s.cfg.PurgeBatchSize
	if batch <= 0 {
		batch = defaultPurgeBatchSize
	}

	for i := 0; i < maxPurgeBatchesPerSweep; i++ {
		n, err := s.store.PurgeJobs(ctx, s.cfg.JobsRetention, batch)
		if err != nil {
			// Best-effort: the next tick retries.
			return
		}
		if n > 0 {
			metrics.RecordReconcilerPurge("purged_jobs", int(n))
		}
		if n < int64(batch) {
			// Fewer rows than the batch bound means nothing more is
			// eligible right now (or the purge is disabled, in which case
			// jobs.PurgeJobs already returned (0, nil) without a DELETE).
			break
		}
	}

	for i := 0; i < maxPurgeBatchesPerSweep; i++ {
		n, err := s.store.PurgeUsage(ctx, s.cfg.UsageRetention, batch)
		if err != nil {
			return
		}
		if n > 0 {
			metrics.RecordReconcilerPurge("purged_usage", int(n))
		}
		if n < int64(batch) {
			break
		}
	}
}
