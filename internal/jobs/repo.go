package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tileforge/octoconv/internal/logfmt"
)

// ErrNotFound is returned when a job does not exist.
var ErrNotFound = errors.New("job not found")

// ErrProtocolBudget is returned when a protocol retry would be the sixth
// counted failure or the protocol deadline has passed. The caller's
// transaction is rolled back.
var ErrProtocolBudget = errors.New("protocol budget exhausted")

// detailActionRecovery is the job_events.detail->>'action' tag written by
// RequeueStale and read back by RecoveryCount. Both MUST reference this
// single constant (never a literal string) so the tag can never drift out of
// sync between the writer and the reader — a mismatch would silently break
// the reconciler's recovery cap (RECON-02, Pitfall 5).
const detailActionRecovery = "reconciler_recovery"

// detailActionWebhookGapRecovered is the job_events.detail->>'action' tag
// written by RecordWebhookGapRecovered when the reconciler detects a
// done/failed job whose completion webhook was silently never enqueued
// (RECON-04). Distinct from detailActionRecovery since this action never
// changes jobs.status and is not counted by RecoveryCount's cap check — a
// webhook-gap recovery is a one-shot, self-terminating action per D-05 (once
// any webhook_deliveries row exists, the job is never re-swept).
const detailActionWebhookGapRecovered = "webhook_gap_recovered"

// StaleJob is a lightweight row returned by FindStale: just enough for the
// reconciler to decide how to recover the job (id + the status it was found
// stranded in). Engine drives the reconciler's engine-aware recovery routing
// (D-04) — it must never be misrouted onto the wrong engine-class queue.
type StaleJob struct {
	ID     uuid.UUID
	Status string
	Engine string
}

// WebhookGapJob is a lightweight row returned by FindWebhookGaps: enough for
// the sweeper to enqueue a delivery and record the recovery event. Status is
// carried through unchanged into the job_events row written by
// RecordWebhookGapRecovered.
type WebhookGapJob struct {
	ID     uuid.UUID
	Status string
}

// nullIfEmpty stores "" as SQL NULL.
//
// So that "the caller sent no correlation id" and "the caller sent an empty
// one" cannot be confused by anything reading the column -- and the API cannot
// send an empty one, because parseCorrelationID returns "" only for absent.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Repo is the jobs repository backed by a pgx pool.
type Repo struct {
	pool *pgxpool.Pool
	// quotaObserver and quotaDisabled are the QUOT/FAIR admission-gate
	// wiring seams (Phase 52, admission.go) -- startup-only, zero value ("no
	// observer, enforcement governed per-client") for every existing caller
	// of NewRepo. See SetQuotaObserver/SetQuotaEnforcementDisabled.
	quotaObserver func(clientID uuid.UUID, engine, reason string)
	quotaDisabled bool
}

// NewRepo wraps a pgx pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// CreateParams describes a new convert job and its single input. ID is the
// caller-provided job id so storage keys (which embed the id) and the row match.
type CreateParams struct {
	ID           uuid.UUID
	ClientID     uuid.UUID
	Operation    string
	Engine       string
	SourceFormat string
	TargetFormat string
	CallbackURL  string
	// CorrelationID is the caller's own identifier, already bounded by the API
	// (length + charset). Empty means the caller sent none, and is stored as
	// NULL rather than an empty string so "absent" and "empty" cannot be
	// confused by anything reading the column later.
	CorrelationID string
	// ProgressWebhook is the per-job opt-in flag (0008_progress_webhook.sql,
	// PUSH-02/D-03) gating the worker's best-effort inline progress-webhook
	// fire. Zero value (false) for every existing caller shape.
	ProgressWebhook bool
	// Opts carries the server-normalized, already-validated conversion options
	// serialized to/from the jobs.options jsonb column (never raw client JSON,
	// per D-08).
	Opts map[string]any
	// PresetName and PresetVersion are optional provenance for preset-created
	// jobs (D-08, 18-presets): when a job is created via a resolved preset,
	// these persist to the existing jobs.preset_name/preset_version columns
	// alongside the resolved Opts snapshot. Left zero-valued (empty
	// name/version 0) for non-preset jobs, which stay NULL in the DB.
	PresetName    string
	PresetVersion int
	// Input is the single-input form, which every caller but one uses and
	// which the schema has always supported at ordinal 0.
	Input Input
	// Inputs is the MULTI-input form: one job, several uploaded files, ordinals
	// as given. job_inputs has carried an ordinal column since Phase 01 and no
	// code path populated more than one until tex projects needed a .tex plus
	// its .bib and figures.
	//
	// Setting both is an ERROR rather than a precedence. Two fields that can
	// each answer "what is this job's input" is the drift shape this package
	// keeps paying for; a caller that sets both has two opinions and the
	// database should not be asked to pick one.
	Inputs []Input
	// CapabilityID pins an approved catalog row. Empty leaves capability_id
	// and capability_version NULL. Create never writes dispatch_owner, so the
	// column default stays legacy.
	CapabilityID      string
	CapabilityVersion int
}

// Create inserts a job (status=queued), its input row, and the initial
// job_events transition in one transaction, returning the job id. If ID is the
// zero value one is generated. The caller enqueues the asynq task only after
// this succeeds (Postgres-first double write). Create can also return
// ErrConcurrencyLimit, ErrDailyBytesLimit or ErrDailyJobsLimit
// (admission.go) when the client's quota rejects this job; callers should
// branch on these via errors.Is.
func (r *Repo) Create(ctx context.Context, p CreateParams) (uuid.UUID, error) {
	jobID := p.ID
	if jobID == uuid.Nil {
		jobID = uuid.New()
	}

	inputs := p.Inputs
	if len(inputs) == 0 {
		inputs = []Input{p.Input}
	} else if p.Input != (Input{}) {
		return uuid.Nil, fmt.Errorf("CreateParams sets both Input and Inputs: two answers to what this "+
			"job's input is, and %s would get whichever this function preferred", jobID)
	}

	// The options column is NOT NULL (0001_init.sql:25); a nil p.Opts defaults
	// to the inert two-byte empty object rather than SQL NULL, per D-08.
	optsJSON := []byte("{}")
	if p.Opts != nil {
		var err error
		optsJSON, err = json.Marshal(p.Opts)
		if err != nil {
			return uuid.Nil, fmt.Errorf("marshal job options: %w", err)
		}
	}

	// preset_name/preset_version are nullable and must stay NULL for
	// non-preset jobs, preserving existing behavior (D-08).
	var pname *string
	var pver *int
	if p.PresetName != "" {
		pname = &p.PresetName
		pver = &p.PresetVersion
	}

	if p.CapabilityID != "" && p.CapabilityVersion < 1 {
		return uuid.Nil, errors.New("capability version must be at least 1")
	}

	// IsoLevel is pinned to ReadCommitted rather than inherited from the pool/
	// DSN default (52-REVIEW.md WR-02): admitLocked's post-lock re-check
	// inside this transaction only re-observes admissions committed while it
	// waited on the advisory lock if each statement takes a fresh snapshot,
	// which is a property of READ COMMITTED, not of the code. See
	// admitLocked's doc comment (admission.go) for the failure mode this
	// pin prevents.
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		// admitLocked runs FIRST, inside this same transaction, under a
		// per-(client, engine) advisory lock: the occupancy count and the
		// INSERT below must not be separable, or two API replicas could
		// both observe "below cap" and both admit (ROADMAP SC2a, D-07). A
		// quota rejection here rolls back the whole transaction, so no
		// jobs, job_inputs or job_events row is left behind -- the failure
		// mode is a clean no-op plus (if the handler already wrote to S3
		// past PreAdmit's advisory pre-check) an orphaned S3 object that
		// TTL cleanup collects, matching the existing preset-TOCTOU path.
		// THE WHOLE JOB'S BYTES, not the first input's. With Inputs populated,
		// p.Input is the zero value, so charging p.Input.SizeBytes admitted a
		// multi-part project as ZERO bytes and let it past max_daily_bytes
		// (PR #20 review). The sum is computed from the same slice the rows
		// below are written from, so the quota and the record cannot disagree.
		var admitBytes int64
		for _, in := range inputs {
			admitBytes += in.SizeBytes
		}
		if err := admitLocked(ctx, tx, p.ClientID, p.Engine, admitBytes, r.quotaObserver, r.quotaDisabled); err != nil {
			return err
		}

		if p.CapabilityID == "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO jobs (id, client_id, operation, engine, status, source_format, target_format, preset_name, preset_version, callback_url, progress_webhook, options, correlation_id)
				VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8, $9, $10, $11, $12)`,
				jobID, p.ClientID, p.Operation, p.Engine, p.SourceFormat, p.TargetFormat, pname, pver, p.CallbackURL, p.ProgressWebhook, optsJSON, nullIfEmpty(p.CorrelationID),
			); err != nil {
				return fmt.Errorf("insert job: %w", err)
			}
		} else if _, err := tx.Exec(ctx, `
			INSERT INTO jobs (id, client_id, operation, engine, status, source_format, target_format, preset_name, preset_version, callback_url, progress_webhook, options, correlation_id, capability_id, capability_version)
			VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			jobID, p.ClientID, p.Operation, p.Engine, p.SourceFormat, p.TargetFormat, pname, pver, p.CallbackURL, p.ProgressWebhook, optsJSON, nullIfEmpty(p.CorrelationID), p.CapabilityID, p.CapabilityVersion,
		); err != nil {
			return fmt.Errorf("insert job: %w", err)
		}

		for _, in := range inputs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				jobID, in.Ordinal, in.ObjectKey, in.Filename, in.Format, in.SizeBytes, in.ContentType,
			); err != nil {
				return fmt.Errorf("insert job_input ordinal %d: %w", in.Ordinal, err)
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO job_events (job_id, from_status, to_status)
			VALUES ($1, NULL, 'queued')`, jobID,
		); err != nil {
			return fmt.Errorf("insert job_event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return jobID, nil
}

// ActivateProtocol marks a queued job active on a transaction that already
// holds the row lock. Opening another transaction here deadlocks on that lock.
func (r *Repo) ActivateProtocol(ctx context.Context, tx pgx.Tx, id, stepID uuid.UUID) error {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock job: %w", err)
	}
	if status != StatusQueued {
		return fmt.Errorf("illegal transition %s -> %s for job %s", status, StatusActive, id)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'active',
		    started_at = COALESCE(started_at, now()),
		    progress_pct = NULL,
		    eta_seconds = NULL,
		    step_id = $2
		WHERE id = $1`, id, stepID); err != nil {
		return fmt.Errorf("activate protocol job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, $3, NULL)`, id, StatusQueued, StatusActive); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	return nil
}

// FailProtocol marks a protocol job failed and records daily usage.
func (r *Repo) FailProtocol(ctx context.Context, id uuid.UUID, code, message string) error {
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		return r.FailProtocolIn(ctx, tx, id, code, message)
	})
}

// FailProtocolIn is FailProtocol for a caller that already holds tx.
// The caller must not open another transaction around it.
func (r *Repo) FailProtocolIn(ctx context.Context, tx pgx.Tx, id uuid.UUID, code, message string) error {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock job: %w", err)
	}
	if status != StatusQueued && status != StatusActive {
		return fmt.Errorf("illegal transition %s -> %s for job %s", status, StatusFailed, id)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'failed', finished_at = now(), error_code = $2, error_message = $3
		WHERE id = $1`, id, code, message); err != nil {
		return fmt.Errorf("fail protocol job: %w", err)
	}
	if err := r.recordUsage(ctx, tx, id, StatusFailed, 0); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, 'failed', NULL)`, id, status); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE worker_slot_holds
		SET state = 'failed', released_at = now()
		WHERE job_id = $1 AND released_at IS NULL`, id); err != nil {
		return fmt.Errorf("release hold: %w", err)
	}
	return nil
}

// RequeueProtocol returns an active protocol job to the queue after a retryable failure.
// notBefore zero means Postgres now(). A sixth counted failure rolls back and returns ErrProtocolBudget.
func (r *Repo) RequeueProtocol(ctx context.Context, id uuid.UUID, notBefore time.Time) error {
	return r.transition(ctx, id, StatusQueued, []string{StatusActive}, nil, outcome{}, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE worker_slot_holds
			SET state = 'failed', released_at = now()
			WHERE job_id = $1 AND released_at IS NULL`, id); err != nil {
			return err
		}
		var retry bool
		if err := tx.QueryRow(ctx, `
			SELECT
				(SELECT count(*) FROM worker_slot_holds h
				 WHERE h.step_id = j.step_id
				   AND h.state = 'failed'
				   AND (j.manual_retry_at IS NULL OR h.released_at > j.manual_retry_at)) < 5
				AND j.protocol_deadline > now()
			FROM jobs j WHERE j.id = $1`, id).Scan(&retry); err != nil {
			return err
		}
		if !retry {
			return ErrProtocolBudget
		}
		if notBefore.IsZero() {
			_, err := tx.Exec(ctx, `
				UPDATE jobs
				SET status = 'queued', not_before = now(), progress_pct = NULL, eta_seconds = NULL
				WHERE id = $1`, id)
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE jobs
			SET status = 'queued', not_before = $2, progress_pct = NULL, eta_seconds = NULL
			WHERE id = $1`, id, notBefore)
		return err
	})
}

// RequeueProtocolIn queues an active protocol job on tx.
// notBefore maps the counted failed holds to the new not_before. A zero time means Postgres now().
// ErrProtocolBudget leaves that hold update on tx for the caller to fail or roll back.
func (r *Repo) RequeueProtocolIn(ctx context.Context, tx pgx.Tx, id uuid.UUID, notBefore func(failed int) (time.Time, error)) error {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock job: %w", err)
	}
	if status != StatusActive {
		return fmt.Errorf("illegal transition %s -> %s for job %s", status, StatusQueued, id)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE worker_slot_holds
		SET state = 'failed', released_at = now()
		WHERE job_id = $1 AND released_at IS NULL`, id); err != nil {
		return err
	}
	var failed int
	var retry bool
	if err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM worker_slot_holds h
			 WHERE h.step_id = j.step_id
			   AND h.state = 'failed'
			   AND (j.manual_retry_at IS NULL OR h.released_at > j.manual_retry_at)),
			(SELECT count(*) FROM worker_slot_holds h
			 WHERE h.step_id = j.step_id
			   AND h.state = 'failed'
			   AND (j.manual_retry_at IS NULL OR h.released_at > j.manual_retry_at)) < 5
			AND j.protocol_deadline > now()
		FROM jobs j WHERE j.id = $1`, id).Scan(&failed, &retry); err != nil {
		return err
	}
	if !retry {
		return ErrProtocolBudget
	}
	when, err := notBefore(failed)
	if err != nil {
		return err
	}
	if when.IsZero() {
		if _, err := tx.Exec(ctx, `
			UPDATE jobs
			SET status = 'queued', not_before = now(), progress_pct = NULL, eta_seconds = NULL
			WHERE id = $1`, id); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'queued', not_before = $2, progress_pct = NULL, eta_seconds = NULL
		WHERE id = $1`, id, when); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, $3, NULL)`, id, status, StatusQueued); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	return nil
}

// QueueAfterStop queues an active protocol job when its failed-hold count is below 5
// and the protocol deadline remains. It does not change slot holds.
func (r *Repo) QueueAfterStop(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	var status string
	var retry bool
	if err := tx.QueryRow(ctx, `
		SELECT j.status,
		       (SELECT count(*) FROM worker_slot_holds h
		        WHERE h.step_id = j.step_id
		          AND h.state = 'failed'
		          AND (j.manual_retry_at IS NULL OR h.released_at > j.manual_retry_at)) < 5
		       AND COALESCE(j.protocol_deadline > now(), false)
		FROM jobs j WHERE j.id = $1 FOR UPDATE`, id).Scan(&status, &retry); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock job: %w", err)
	}
	if status != StatusActive || !retry {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'queued', progress_pct = NULL, eta_seconds = NULL
		WHERE id = $1`, id); err != nil {
		return fmt.Errorf("requeue stopped job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, $3, NULL)`, id, StatusActive, StatusQueued); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	return nil
}

// MarkActive transitions a job to active and stamps started_at, appending an
// event. The transition allows queued->active AND active->active so asynq's
// internal same-task retry re-entering the handler does not trip the illegal-
// transition guard; started_at uses COALESCE so it stays pinned to the FIRST
// activation, not the most recent retry, which the reconciler's active-
// staleness check depends on for true elapsed running time.
func (r *Repo) MarkActive(ctx context.Context, id uuid.UUID) error {
	return r.transition(ctx, id, StatusActive, []string{StatusQueued, StatusActive}, nil, outcome{}, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'active', started_at = COALESCE(started_at, now()), attempts = attempts + 1, stage = NULL, stage_index = NULL, stage_count = NULL WHERE id = $1`, id)
		return err
	})
}

// MarkDone transitions an active job to done and stamps finished_at. The
// client_usage_daily rollup write (recordUsage) runs INSIDE this same
// transition transaction, after the status UPDATE — not as an adjacent call
// — because a worker dying between a committed status change and a separate
// accounting write would lose that job's usage permanently: neither an
// asynq retry nor the reconciler revisits a terminal job (D-05). attempt is
// the elapsed engine time for this attempt (0 if the caller has no attempt
// clock).
func (r *Repo) MarkDone(ctx context.Context, id uuid.UUID, attempt time.Duration) error {
	return r.transition(ctx, id, StatusDone, []string{StatusActive}, nil, outcome{elapsed: attempt, hasElapsed: true}, func(ctx context.Context, tx pgx.Tx) error {
		// D-13: a done job without exactly one primary output is an
		// unreachable state, enforced here rather than in transition()
		// because this is a precondition of MarkDone's own semantics, not
		// of the transition mechanics transition() implements — the
		// transition helper itself is a documented untouchable invariant
		// (PROJECT.md, 56-CONTEXT.md) shared by MarkFailed/Cancel/
		// MarkActive/RequeueStale, none of which need this check and none
		// of which call this closure. There is no race: this SELECT runs
		// on the SAME tx that transition() already holds the job row
		// locked on (`SELECT status ... FOR UPDATE` above, this function),
		// under the pinned ReadCommitted isolation level, so no concurrent
		// AddOutput/MarkDone can slip between the count and the UPDATE
		// below. If this returns an error, transition()'s pgx.BeginTxFunc
		// rolls back the whole apply closure — the UPDATE jobs above never
		// happened either, and recordUsage never runs — so the job stays
		// active, not half-done. For the caller (the worker), this is an
		// ordinary, retryable error: it is NOT wrapped in asynq.SkipRetry
		// like the illegal-transition case in internal/worker/worker.go,
		// because the path self-heals — a retried task re-runs AddOutput
		// (upsert on (job_id, ordinal)) before calling MarkDone again.
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM job_outputs WHERE job_id = $1 AND role = 'primary'`, id,
		).Scan(&n); err != nil {
			return fmt.Errorf("count primary outputs for job %s: %w", id, err)
		}
		if n != 1 {
			return fmt.Errorf("job %s has %d primary outputs, exactly 1 required to mark done", id, n)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'done', finished_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return r.recordUsage(ctx, tx, id, StatusDone, attempt)
	})
}

// MarkFailed transitions a job to failed, recording the error and finished_at.
// detail is an optional structured payload (e.g. raw engine stderr) attached
// to the job_events row for internal diagnostics only — error_message/code
// stay short and sanitized since they are exposed via GET /jobs/{id} and
// webhook payloads. Like MarkDone, the client_usage_daily rollup write runs
// INSIDE this same transition transaction (D-05) — see MarkDone's comment
// for why. attempt is the elapsed engine time for this attempt; pass 0 when
// the caller has no attempt clock (e.g. the reconciler's exhaustion path).
func (r *Repo) MarkFailed(ctx context.Context, id uuid.UUID, code, message string, detail map[string]any, attempt time.Duration) error {
	return r.markFailed(ctx, id, code, message, detail, outcome{code: code, elapsed: attempt, hasElapsed: true})
}

// MarkFailedUnmeasured is MarkFailed for a caller that never ran the job and
// therefore has no attempt clock -- the reconciler's exhaustion path.
//
// A SEPARATE METHOD rather than a zero sentinel, because zero is a legitimate
// measurement: the worker's invalid-options guard fails a job before the
// engine starts and passes a real 0, and inferring "unmeasured" from the value
// made that record lose its duration entirely (codex, PR #23). The two callers
// differ in whether they KNOW the duration, which is not something the number
// can express.
//
// transition derives an elapsed time from the locked row's started_at when it
// gets no measurement, so a stale ACTIVE job still reports how long it had
// been running.
func (r *Repo) MarkFailedUnmeasured(ctx context.Context, id uuid.UUID, code, message string, detail map[string]any) error {
	return r.markFailed(ctx, id, code, message, detail, outcome{code: code})
}

func (r *Repo) markFailed(ctx context.Context, id uuid.UUID, code, message string, detail map[string]any, out outcome) error {
	return r.transition(ctx, id, StatusFailed, []string{StatusQueued, StatusActive}, detail, out, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'failed', finished_at = now(), error_code = $2, error_message = $3 WHERE id = $1`,
			id, code, message); err != nil {
			return err
		}
		return r.recordUsage(ctx, tx, id, StatusFailed, out.elapsed)
	})
}

// Cancel transitions a queued or active job to canceled and stamps
// finished_at, reusing the SAME guarded transition() helper as
// MarkDone/MarkFailed — no new transition machinery (D-01). The allow-list
// ({queued,active}) is the correctness backstop: it also makes Cancel
// idempotent at the DB layer, since a job already done/failed/canceled is
// simply outside allowedFrom and the illegal-transition error surfaces
// instead of silently overwriting a terminal outcome. detail is nil (no
// webhook is fired on cancel, D-08; no structured detail is needed).
//
// A canceled job's input bytes and its job count ARE charged to the
// client's daily ledger (52-REVIEW.md WR-01, migration 0014), superseding
// the earlier rule that a cancel never touches the rollup: Phase 52 made
// client_usage_daily the input to quota admission (admission.go), so
// leaving cancels free makes `create -> cancel -> create ...` an unbounded
// evasion path against max_daily_bytes/max_daily_jobs — and after plan
// 52-06 it is the one hole in-flight counting cannot cover on its own, since
// a canceled job leaves the queued/active set and, absent this write, its
// cost would vanish entirely at the instant of cancellation. recordUsage
// runs INSIDE this same transition transaction, after the status UPDATE —
// exactly like MarkDone/MarkFailed — so the crossover from "in-flight" to
// "rolled up" is atomic and the {queued,active} allow-list above (which
// rejects a second cancel of the same job before apply ever runs) is what
// keeps this exactly-once, for the same reason it holds for MarkDone and
// MarkFailed. attempt is passed as 0: a job canceled while active is
// charged the elapsed time recordUsage derives from started_at, and a job
// canceled while still queued — which never occupied a worker — is charged
// 0, via the same fallback CASE expression recordUsage already implements
// for the reconciler's exhaustion path (no SQL changes needed).
func (r *Repo) Cancel(ctx context.Context, id uuid.UUID) error {
	return r.transition(ctx, id, StatusCanceled, []string{StatusQueued, StatusActive}, nil, outcome{}, func(ctx context.Context, tx pgx.Tx) error {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT dispatch_owner FROM jobs WHERE id = $1`, id).Scan(&owner); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'canceled', finished_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if owner == "protocol" {
			if _, err := tx.Exec(ctx, `
				UPDATE worker_slot_holds
				SET state = 'canceled', released_at = now()
				WHERE job_id = $1 AND released_at IS NULL`, id); err != nil {
				return fmt.Errorf("fence canceled hold: %w", err)
			}
		}
		return r.recordUsage(ctx, tx, id, StatusCanceled, 0)
	})
}

var errManualRetryRefused = errors.New("manual retry refused")

// ManualRetry queues a failed protocol job again when its deadline, inputs, and quota allow it.
func (r *Repo) ManualRetry(ctx context.Context, id uuid.UUID) error {
	return r.transition(ctx, id, StatusQueued, []string{StatusFailed}, nil, outcome{}, func(ctx context.Context, tx pgx.Tx) error {
		var eligible bool
		var clientID *uuid.UUID
		var engine string
		var inputBytes int64
		if err := tx.QueryRow(ctx, `
			SELECT
				j.dispatch_owner = 'protocol'
					AND COALESCE(j.protocol_deadline > now(), false)
					AND NOT EXISTS (
						SELECT 1 FROM worker_slot_holds h
						WHERE h.job_id = j.id AND h.released_at IS NULL
					)
					AND EXISTS (SELECT 1 FROM job_inputs i WHERE i.job_id = j.id),
				j.client_id,
				j.engine,
				COALESCE((SELECT sum(size_bytes) FROM job_inputs WHERE job_id = j.id), 0)
			FROM jobs j
			WHERE j.id = $1`, id).Scan(&eligible, &clientID, &engine, &inputBytes); err != nil {
			return err
		}
		if !eligible || clientID == nil {
			return errManualRetryRefused
		}
		if err := admitLocked(ctx, tx, *clientID, engine, inputBytes, r.quotaObserver, r.quotaDisabled); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE jobs
			SET status = 'queued', progress_pct = NULL, eta_seconds = NULL, manual_retry_at = now()
			WHERE id = $1`, id)
		return err
	})
}

// InFlightJob is one job currently occupying a client's quota, as reported to
// an operator by cmd/manage-clients list-inflight.
//
// Age is computed by POSTGRES, not by the caller, and that is the point of
// having it here rather than deriving it in the CLI from CreatedAt: the
// quota gate this listing exists to explain reads its own "today" from the
// database (usage_query.go's UTCToday and its doc comment on why Go never
// touches the value). An operator shown an age measured by the CLI host's
// clock would be reasoning about a different timeline than the one that
// actually decides whether the job still consumes budget.
type InFlightJob struct {
	ID        uuid.UUID
	ClientID  uuid.UUID
	Engine    string
	Status    string
	CreatedAt time.Time
	StartedAt *time.Time
	Age       time.Duration
}

// FindInFlight returns the jobs currently occupying quota — the same
// {queued, active} set the admission gate counts — newest first, optionally
// narrowed to one client and to jobs older than a threshold.
//
// It exists because 52 WR-01 has two halves, and a remedy without a way to
// FIND the subject is only half a remedy: an in-flight job charges the daily
// cap with no time bound, so one permanently stuck job locks out a client's
// daily budget every subsequent UTC day, and an operator cannot cancel what
// they cannot enumerate.
//
// The in-flight definition comes from quotaInFlightStatuses (admission.go),
// the SAME slice evaluate() counts against max_concurrent. That sharing is
// deliberate rather than tidy: a second literal list here could drift, and a
// listing that disagreed with the gate would send an operator to cancel jobs
// that were not the ones holding the budget — which is worse than no listing,
// because it looks like it worked.
//
// olderThan <= 0 means no age filter. A nil clientID means every client.
func (r *Repo) FindInFlight(ctx context.Context, clientID *uuid.UUID, olderThan time.Duration) ([]InFlightJob, error) {
	// The age threshold is applied by Postgres against Postgres-written
	// timestamps, so the CLI host's clock never enters the comparison. This
	// is the same rule DEBT-11 applies to retention, adopted here at the
	// point of writing rather than left to be found later.
	rows, err := r.pool.Query(ctx, `
		SELECT id, client_id, engine, status, created_at, started_at,
		       now() - created_at AS age
		FROM jobs
		WHERE status = ANY($1)
		  AND ($2::uuid IS NULL OR client_id = $2::uuid)
		  AND ($3::interval IS NULL OR now() - created_at >= $3::interval)
		ORDER BY created_at ASC`,
		quotaInFlightStatuses, clientID, nullableInterval(olderThan),
	)
	if err != nil {
		return nil, fmt.Errorf("query in-flight jobs: %w", err)
	}
	defer rows.Close()

	var out []InFlightJob
	for rows.Next() {
		var j InFlightJob
		if err := rows.Scan(&j.ID, &j.ClientID, &j.Engine, &j.Status, &j.CreatedAt, &j.StartedAt, &j.Age); err != nil {
			return nil, fmt.Errorf("scan in-flight job: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// nullableInterval renders a non-positive duration as SQL NULL, which the
// query above reads as "no age filter". Returning a zero interval instead
// would filter on `age >= 0`, which is true for every row and therefore looks
// identical -- until a clock adjustment makes a freshly created row's age
// briefly negative and it silently disappears from an operator's listing
// during an incident.
//
// A sub-microsecond duration is NOT rejected and behaves as no filter at all:
// PostgreSQL's interval type has microsecond resolution, so anything smaller
// rounds to zero. Measured, not assumed. It is left alone rather than guarded
// because the operator-facing form of this argument is a duration string like
// "2h", and a guard for an input nobody can produce is a branch nobody tests.
func nullableInterval(d time.Duration) any {
	if d <= 0 {
		return nil
	}
	return d
}

// RequeueStale requeues a job stranded in queued (lost enqueue) or active
// (crashed worker / exhausted asynq retry budget) back to queued, via the
// same guarded, row-locked transition every other status change goes through
// — NEVER an ad-hoc UPDATE. reason is a short machine-readable tag (e.g.
// "stale_queued"/"stale_active") recorded in the job_events.detail payload
// alongside the reconciler_recovery action tag, so the recovery cap
// (RecoveryCount) and audit trail (RECON-03) stay consistent.
func (r *Repo) RequeueStale(ctx context.Context, id uuid.UUID, reason string) error {
	detail := map[string]any{"action": detailActionRecovery, "reason": reason}
	return r.transition(ctx, id, StatusQueued, []string{StatusQueued, StatusActive}, detail, outcome{}, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE jobs SET status = 'queued', stage = NULL, stage_index = NULL, stage_count = NULL WHERE id = $1`, id)
		return err
	})
}

// RecoveryCount returns how many times the reconciler has already requeued
// this job (i.e. the number of job_events rows tagged detailActionRecovery),
// so the sweeper can compare against the recovery cap (RECONCILER_MAX_RECOVERIES).
func (r *Repo) RecoveryCount(ctx context.Context, id uuid.UUID) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM job_events WHERE job_id = $1 AND detail->>'action' = $2`,
		id, detailActionRecovery,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("count recoveries for job %s: %w", id, err)
	}
	return n, nil
}

// FindStale returns jobs stranded past their staleness threshold: queued
// jobs older than queuedStaleAfter (lost enqueue) or active jobs whose
// started_at is older than activeStaleAfter (crashed worker / exhausted
// asynq retry budget). Cutoffs are computed in Go and bound as timestamptz
// parameters so the comparison stays index-friendly against jobs_inflight_idx
// (created_at) WHERE status IN ('queued','active').
func (r *Repo) FindStale(ctx context.Context, queuedStaleAfter, activeStaleAfter time.Duration) ([]StaleJob, error) {
	now := time.Now()
	queuedCutoff := now.Add(-queuedStaleAfter)
	activeCutoff := now.Add(-activeStaleAfter)

	rows, err := r.pool.Query(ctx, `
		SELECT id, status, engine FROM jobs
		WHERE dispatch_owner = 'legacy'
		  AND ((status = 'queued' AND created_at < $1)
		    OR (status = 'active' AND started_at < $2))`,
		queuedCutoff, activeCutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query stale jobs: %w", err)
	}
	defer rows.Close()

	var out []StaleJob
	for rows.Next() {
		var j StaleJob
		if err := rows.Scan(&j.ID, &j.Status, &j.Engine); err != nil {
			return nil, fmt.Errorf("scan stale job: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// FindWebhookGaps returns done/failed jobs with a non-empty callback_url and
// ZERO rows in webhook_deliveries (any row — delivered, undelivered, or
// dead-lettered — excludes a job; see D-05), whose finished_at is older than
// activeStaleAfter (reusing the existing reconciler threshold, D-04, so a
// job whose webhook enqueue is legitimately still in flight through the same
// tick that marked it done/failed is not falsely flagged). The cutoff is
// computed in Go and bound as a single timestamptz parameter, matching
// FindStale's existing convention.
func (r *Repo) FindWebhookGaps(ctx context.Context, activeStaleAfter time.Duration) ([]WebhookGapJob, error) {
	cutoff := time.Now().Add(-activeStaleAfter)

	rows, err := r.pool.Query(ctx, `
		SELECT j.id, j.status FROM jobs j
		WHERE j.status IN ('done', 'failed')
		  AND j.callback_url IS NOT NULL AND j.callback_url <> ''
		  AND j.finished_at < $1
		  AND NOT EXISTS (
		      SELECT 1 FROM webhook_deliveries wd WHERE wd.job_id = j.id
		  )`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query webhook gaps: %w", err)
	}
	defer rows.Close()

	var out []WebhookGapJob
	for rows.Next() {
		var g WebhookGapJob
		if err := rows.Scan(&g.ID, &g.Status); err != nil {
			return nil, fmt.Errorf("scan webhook gap: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RecordWebhookGapRecovered appends a job_events row documenting that the
// reconciler detected and recovered a silently-dropped webhook enqueue
// (RECON-04). Unlike every other write in this file, this does NOT go
// through transition(): the job's status is NOT changing (it is already
// done/failed and stays that way), so from_status == to_status == status,
// and no row lock is needed — the correctness guard against a duplicate
// delivery is the asynq.Unique lock the sweeper already checked before
// calling this (enqueue-first, D-03), not a DB-level lock.
func (r *Repo) RecordWebhookGapRecovered(ctx context.Context, id uuid.UUID, status string) error {
	detail := map[string]any{"action": detailActionWebhookGapRecovered}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal webhook gap detail: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO job_events (job_id, from_status, to_status, detail) VALUES ($1, $2, $2, $3)`,
		id, status, detailJSON,
	); err != nil {
		return fmt.Errorf("record webhook gap recovery for job %s: %w", id, err)
	}
	return nil
}

// AddOutput upserts a job_outputs row, keyed by the logical output identity
// (job_id, ordinal).
//
// This MUST stay an upsert rather than a plain INSERT. recordUsage sums
// size_bytes over every row for the job without filtering on ordinal (so a
// future multi-output job is not under-counted), and MarkActive permits
// active -> active so a same-task asynq retry re-enters the handler and runs
// the conversion again. A plain INSERT therefore left two rows for one logical
// output whenever AddOutput committed and the following MarkDone failed
// transiently, and the eventual terminal transition summed both -- permanently
// overstating bytes_out in a rollup that is never revisited. See
// 0012_output_identity_and_retention_index.sql.
//
// DO UPDATE rather than DO NOTHING: the retried conversion may legitimately
// produce a different object_key or size, and the newest attempt is the one
// whose object actually exists in storage.
//
// role is deliberately absent from both the INSERT column list and the
// ON CONFLICT SET clause (D-16): job_outputs.role carries a DB-level default
// ('primary', 0015_operation_and_output_roles.sql), and that default -- not
// this Go layer -- is the source of truth, closing the class of defect where
// an API-side default silently drifts out of sync with the DB default. When
// Phase 60 starts writing an output with role='report', it needs a dedicated
// write path for that column, added deliberately alongside a guarantee that
// no existing caller of AddOutput starts silently overwriting a row's role
// back to a Go zero-value ("") on retry -- do not add role to this upsert
// without that guarantee in place.
func (r *Repo) AddOutput(ctx context.Context, jobID uuid.UUID, o Output) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (job_id, ordinal) DO UPDATE SET
			object_key   = EXCLUDED.object_key,
			filename     = EXCLUDED.filename,
			format       = EXCLUDED.format,
			size_bytes   = EXCLUDED.size_bytes,
			content_type = EXCLUDED.content_type`,
		jobID, o.Ordinal, o.ObjectKey, o.Filename, o.Format, o.SizeBytes, o.ContentType)
	if err != nil {
		return fmt.Errorf("insert job_output: %w", err)
	}
	return nil
}

const upsertOutputWithRoleSQL = `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type, role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (job_id, ordinal) DO UPDATE SET
			object_key   = EXCLUDED.object_key,
			filename     = EXCLUDED.filename,
			format       = EXCLUDED.format,
			size_bytes   = EXCLUDED.size_bytes,
			content_type = EXCLUDED.content_type,
			role         = EXCLUDED.role`

// AddOutputWithRole is the dedicated write path AddOutput's doc comment above
// required before any caller could store a non-primary output (D-16). It is
// the ONLY place in this package that writes job_outputs.role.
//
// # WHY IT IS A SECOND FUNCTION RATHER THAN A PARAMETER ON THE FIRST
//
// AddOutput has ten callers across the engines, none of which has a role to
// give. Adding role to its INSERT would make every one of them write Go's
// zero value ("") into a column with a DB-level default, and the ON CONFLICT
// clause would then overwrite a correct role with "" on any retry -- the exact
// failure AddOutput's comment warned against. Two functions keep that
// impossible by construction: a caller with no role cannot express one.
//
// PHASE 60 SHIPPED WITHOUT THIS AND IT WAS A REAL DEFECT, NOT A TIDINESS ONE.
// The sanitize report was built with Role set on the struct and handed to
// AddOutput, which silently discarded it; the row took the DB default
// 'primary', so every sanitize job ended with TWO primary outputs and
// MarkDone's D-13 guard -- which requires exactly one -- failed the job on
// every attempt. Caught in review (PR #12), not by a test, because the tests
// exercised the side-output hook in isolation and never the repository write.
// TestAddOutputWithRolePersistsAndUpserts exists so that cannot recur.
func (r *Repo) AddOutputWithRole(ctx context.Context, jobID uuid.UUID, o Output) error {
	if o.Role == "" {
		return fmt.Errorf("AddOutputWithRole called with an empty role for job %s ordinal %d: "+
			"use AddOutput when the DB default is what you want", jobID, o.Ordinal)
	}
	_, err := r.pool.Exec(ctx, upsertOutputWithRoleSQL,
		jobID, o.Ordinal, o.ObjectKey, o.Filename, o.Format, o.SizeBytes, o.ContentType, o.Role)
	if err != nil {
		return fmt.Errorf("insert job_output with role: %w", err)
	}
	return nil
}

func (r *Repo) AddOutputs(ctx context.Context, jobID uuid.UUID, outs []Output) error {
	if len(outs) == 0 {
		return fmt.Errorf("AddOutputs called with no outputs for job %s", jobID)
	}
	for _, o := range outs {
		if o.Role == "" {
			return fmt.Errorf("AddOutputs called with an empty role for job %s ordinal %d", jobID, o.Ordinal)
		}
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		for _, o := range outs {
			if _, err := tx.Exec(ctx, upsertOutputWithRoleSQL,
				jobID, o.Ordinal, o.ObjectKey, o.Filename, o.Format, o.SizeBytes, o.ContentType, o.Role); err != nil {
				return fmt.Errorf("insert job_output ordinal %d: %w", o.Ordinal, err)
			}
		}
		return nil
	})
}

// Get loads a job by id. Returns ErrNotFound if it does not exist.
func (r *Repo) Get(ctx context.Context, id uuid.UUID) (*Job, error) {
	var j Job
	var src, tgt, cb, code, msg *string
	var clientID *uuid.UUID
	// options is NOT NULL (0001_init.sql:25), so it is scanned into a plain
	// []byte, NOT a *string pointer, unlike the other nullable columns above.
	var optsJSON []byte
	var corr *string
	err := r.pool.QueryRow(ctx, `
		SELECT id, client_id, operation, engine, status, source_format, target_format,
		       callback_url, progress_webhook, options, error_code, error_message, created_at, started_at, finished_at,
		       progress_pct, eta_seconds, source_duration_seconds, correlation_id,
		       stage, stage_index, stage_count, attempts
		FROM jobs WHERE id = $1`, id,
	).Scan(&j.ID, &clientID, &j.Operation, &j.Engine, &j.Status, &src, &tgt,
		&cb, &j.ProgressWebhook, &optsJSON, &code, &msg, &j.CreatedAt, &j.StartedAt, &j.FinishedAt,
		&j.ProgressPct, &j.EtaSeconds, &j.SourceDurationSeconds, &corr,
		&j.Stage, &j.StageIndex, &j.StageCount, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	// client_id is ON DELETE SET NULL (0001_init.sql); a legacy or orphaned
	// row can have a null client_id, so scan via a pointer and default to
	// uuid.Nil, matching the nullable-column deref style used elsewhere here.
	if clientID != nil {
		j.ClientID = *clientID
	}
	j.SourceFormat = deref(src)
	j.TargetFormat = deref(tgt)
	j.CallbackURL = deref(cb)
	j.ErrorCode = deref(code)
	j.ErrorMessage = deref(msg)
	j.CorrelationID = deref(corr)
	if err := json.Unmarshal(optsJSON, &j.Opts); err != nil {
		return nil, fmt.Errorf("unmarshal job options: %w", err)
	}
	return &j, nil
}

// UpdateProgress records one progress tick for an in-flight job via a
// single, plain, non-locking UPDATE — deliberately OUTSIDE the guarded
// transition() path (D-03b): progress is denormalized telemetry, not an
// audited lifecycle change, so this never locks the row (no SELECT ...
// FOR UPDATE) and never appends an event to the audit log. The WHERE clause
// self-conditions on two invariants: only a currently-active job accepts a
// write (a late/out-of-order tick for a job that has already finished, was
// canceled, or was never activated is silently dropped), and the write is
// monotonic (a pct lower than what is already stored is rejected, guarding
// against a stale/retried tick resurrecting a lower value, T-41-09). Zero
// rows affected is NOT an error — it is the expected, self-conditioned
// no-op outcome for both cases above.
func (r *Repo) UpdateProgress(ctx context.Context, id uuid.UUID, pct int, eta *int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET progress_pct = $2, eta_seconds = $3, progress_updated_at = now()
		WHERE id = $1 AND status = $4 AND (progress_pct IS NULL OR progress_pct <= $2)`,
		id, pct, eta, StatusActive,
	)
	if err != nil {
		return fmt.Errorf("update progress for job %s: %w", id, err)
	}
	return nil
}

func (r *Repo) UpdateStage(ctx context.Context, id uuid.UUID, stage string, index, count int) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE jobs SET stage = $2, stage_index = $3, stage_count = $4
		WHERE id = $1 AND status = $5 AND (stage_index IS NULL OR stage_index < $3)`,
		id, stage, index, count, StatusActive,
	)
	if err != nil {
		return false, fmt.Errorf("update stage for job %s: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}

// SetSourceDuration persists the ffprobe-derived source duration once per
// job — the percent denominator for av/audio ffmpeg jobs (D-05). Write-once
// idempotent via the source_duration_seconds IS NULL guard; like
// UpdateProgress this is a plain, non-locking UPDATE outside the guarded
// transition path (no row lock, no audit-log append).
func (r *Repo) SetSourceDuration(ctx context.Context, id uuid.UUID, seconds float64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET source_duration_seconds = $2
		WHERE id = $1 AND status = $3 AND source_duration_seconds IS NULL`,
		id, seconds, StatusActive,
	)
	if err != nil {
		return fmt.Errorf("set source duration for job %s: %w", id, err)
	}
	return nil
}

// Inputs lists a job's inputs ordered by ordinal.
func (r *Repo) Inputs(ctx context.Context, jobID uuid.UUID) ([]Input, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ordinal, object_key, filename, format, size_bytes, content_type
		FROM job_inputs WHERE job_id = $1 ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query inputs: %w", err)
	}
	defer rows.Close()

	var out []Input
	for rows.Next() {
		var in Input
		var name, format, ct *string
		var size *int64
		if err := rows.Scan(&in.Ordinal, &in.ObjectKey, &name, &format, &size, &ct); err != nil {
			return nil, fmt.Errorf("scan input: %w", err)
		}
		in.Filename, in.Format, in.ContentType = deref(name), deref(format), deref(ct)
		if size != nil {
			in.SizeBytes = *size
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// Outputs lists a job's outputs ordered by ordinal. ORDER BY ordinal is kept
// as-is (not extended to role): job_outputs_job_ordinal_key UNIQUE (job_id,
// ordinal) (0012_output_identity_and_retention_index.sql) already makes
// ordinal a stable sort key, and primary-output selection in handleGetJob is
// role-driven, not positional -- adding role to this ORDER BY would resolve
// no existing ambiguity while introducing a second, hidden dependency on row
// order.
func (r *Repo) Outputs(ctx context.Context, jobID uuid.UUID) ([]Output, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ordinal, object_key, filename, format, size_bytes, content_type, role
		FROM job_outputs WHERE job_id = $1 ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query outputs: %w", err)
	}
	defer rows.Close()

	var out []Output
	for rows.Next() {
		var o Output
		var key, name, format, ct *string
		var size *int64
		// role is NOT NULL (0015_operation_and_output_roles.sql), so unlike
		// object_key/filename/format/content_type above it scans directly into
		// a plain string, not through the *string + deref() nullable-column
		// pattern.
		if err := rows.Scan(&o.Ordinal, &key, &name, &format, &size, &ct, &o.Role); err != nil {
			return nil, fmt.Errorf("scan output: %w", err)
		}
		o.ObjectKey, o.Filename, o.Format, o.ContentType = deref(key), deref(name), deref(format), deref(ct)
		if size != nil {
			o.SizeBytes = *size
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// List returns one keyset-paginated page of clientID's jobs, newest-first
// (created_at DESC, id DESC — D-09), optionally narrowed by f. client_id is
// ALWAYS in the SQL WHERE (never a Go-side filter) — the load-bearing
// cross-client-isolation guarantee (JLIST-01, T-40-01). Every filter/cursor
// value is a bound $N parameter, never string-interpolated (T-40-02). This
// is exactly one r.pool.Query call: the selected columns are everything a
// listing item needs, so there is no per-row Outputs/Inputs/PresignGet
// N+1 (D-07). Tenant/time and primary-input lookup indexes are reused.
// Literal substring search may scan the caller's retained history; no schema change.
//
// limit+1 rows are fetched as a probe for whether a further page exists: if
// more than limit rows come back, the extra row is dropped and the next
// Cursor is built from the LAST RETURNED row; otherwise the next cursor is
// nil (no further page).
func (r *Repo) List(ctx context.Context, clientID uuid.UUID, f ListFilter, cursor *Cursor, limit int) ([]JobSummary, *Cursor, error) {
	f, err := NormalizeListFilter(f)
	if err != nil {
		return nil, nil, err
	}
	if cursor != nil && !cursor.Matches(f) {
		return nil, nil, fmt.Errorf("cursor does not match query or sort")
	}
	query := `
  SELECT j.id, j.status, j.engine, j.source_format, j.target_format, j.progress_pct,
         j.created_at, j.started_at, j.finished_at, j.error_code, coalesce(input.filename,'')
  FROM jobs j
  LEFT JOIN LATERAL (
   SELECT filename FROM job_inputs WHERE job_id=j.id AND ordinal=0 ORDER BY id LIMIT 1
  ) input ON true
  WHERE j.client_id=$1`
	args := []any{clientID}
	if f.Status != "" {
		args = append(args, f.Status)
		query += fmt.Sprintf(" AND j.status=$%d", len(args))
	}
	if f.Engine != "" {
		args = append(args, f.Engine)
		query += fmt.Sprintf(" AND j.engine=$%d", len(args))
	}
	if f.Query != "" {
		args = append(args, f.Query)
		query += fmt.Sprintf(" AND (strpos(lower(coalesce(input.filename,'')),lower($%d))>0 OR strpos(j.id::text,lower($%d))>0)", len(args), len(args))
	}
	direction, comparison := "DESC", "<"
	if f.Sort == "oldest" {
		direction, comparison = "ASC", ">"
	}
	if cursor != nil {
		args = append(args, cursor.CreatedAt, cursor.ID)
		query += fmt.Sprintf(" AND (j.created_at,j.id) %s ($%d,$%d)", comparison, len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY j.created_at %s,j.id %s LIMIT $%d", direction, direction, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query job listing: %w", err)
	}
	defer rows.Close()

	var out []JobSummary
	for rows.Next() {
		var j JobSummary
		var src, tgt, code *string
		if err := rows.Scan(&j.ID, &j.Status, &j.Engine, &src, &tgt, &j.ProgressPct,
			&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &code, &j.SourceFilename); err != nil {
			return nil, nil, fmt.Errorf("scan job summary: %w", err)
		}
		j.SourceFormat = deref(src)
		j.TargetFormat = deref(tgt)
		j.ErrorCode = deref(code)
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("query job listing: %w", err)
	}

	var next *Cursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		if f.Query != "" || f.Sort != "newest" {
			next.Query = f.Query
			next.Sort = f.Sort
		}
	}
	return out, next, nil
}

// transition performs a guarded status change plus an append to job_events in a
// single transaction. It errors if the job is not in one of the allowed source
// statuses (concurrency/idempotency guard). detail, when non-nil, is marshaled
// into the job_events.detail jsonb column; when nil, the column stays NULL.
// outcome is what only the CALLER knows about a transition: why it failed and
// how long the attempt took.
//
// A struct rather than two more parameters on transition, because three of the
// five callers have neither and would have to pass zero values -- and a
// parameter every caller must supply and most cannot is how a signature grows
// into noise.
//
// NOT re-read from the row after the commit: the caller has these values in
// hand, and a second read is a second chance for the record to describe
// something other than what happened.
type outcome struct {
	code    string
	elapsed time.Duration
	// hasElapsed distinguishes "no duration applies" from "the duration was
	// zero". Without it, a genuinely instant failure -- invalid_options and
	// reconciler_exhausted both pass zero -- looked identical to a transition
	// that has no notion of elapsed time, and the field was dropped from both
	// (codex, PR #23).
	hasElapsed bool
}

func (r *Repo) transition(
	ctx context.Context,
	id uuid.UUID,
	to string,
	allowedFrom []string,
	detail map[string]any,
	out outcome,
	apply func(ctx context.Context, tx pgx.Tx) error,
) error {
	var detailJSON []byte
	if detail != nil {
		var err error
		detailJSON, err = json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("marshal transition detail: %w", err)
		}
	}
	// IsoLevel is pinned to ReadCommitted for the same reason as Repo.Create
	// (52-REVIEW.md WR-02): the SELECT ... FOR UPDATE re-read below and
	// recordUsage's ON CONFLICT upsert (MarkDone/MarkFailed/Cancel) both
	// assume the same per-statement snapshot.
	// Identity read under the SAME lock as the status, so the record below
	// describes the row the transition actually saw.
	var from, engine, target string
	var clientID *uuid.UUID
	var corr *string
	// NULL when the job never started: a duration that does not exist, as
	// opposed to one that is zero.
	var runningMS *float64

	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			// The elapsed time is computed BY POSTGRES, not by this process.
			//
			// started_at is stamped with the database's now(), so subtracting
			// it from the application host's clock measures the skew between
			// two machines as well as the job's duration -- and on a quick
			// cancellation that can be NEGATIVE (codex, PR #23). recordUsage
			// already keeps its arithmetic on the database clock for the same
			// reason.
			`SELECT status, client_id, engine, coalesce(target_format,''), correlation_id,
			        EXTRACT(EPOCH FROM (now() - started_at)) * 1000
			   FROM jobs WHERE id = $1 FOR UPDATE`, id,
		).Scan(&from, &clientID, &engine, &target, &corr, &runningMS); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("lock job: %w", err)
		}

		if !contains(allowedFrom, from) {
			return fmt.Errorf("illegal transition %s -> %s for job %s", from, to, id)
		}

		if err := apply(ctx, tx); err != nil {
			return fmt.Errorf("apply transition: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO job_events (job_id, from_status, to_status, detail) VALUES ($1, $2, $3, $4)`,
			id, from, to, detailJSON,
		); err != nil {
			return fmt.Errorf("insert job_event: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// THE LIFECYCLE RECORD, emitted here and nowhere else.
	//
	// Every status change in this service goes through this function -- the
	// worker's terminal outcomes, the API's cancel, the operator CLI's
	// cancel-job, and the reconciler's exhaustion path. Logging at the callers
	// instead meant four places to remember, and three of them were already
	// missing when review looked (PR #23): a queued cancellation, an operator
	// cancellation, and a reconciler-exhausted job each ended with no record.
	//
	// from_status comes from the LOCKED read above rather than from whatever
	// the caller happened to see earlier, so a queued job that a worker
	// activated between the two reads is recorded as it actually was.
	//
	// After the commit, deliberately: a record is a claim that the transition
	// happened, and until BeginTxFunc returns it has not.
	//
	// internal/* logging is a deliberate exception here, the same one
	// internal/api already carries. The alternative is what this replaces.
	fields := []string{
		"job_id", id.String(),
		"correlation_id", derefStr(corr),
		"engine", engine,
		"target", target,
		"from_status", from,
	}
	if clientID != nil {
		fields = append(fields, "client_id", clientID.String())
	}
	// The outcome fields, which centralising this record had DROPPED (codex,
	// PR #23): without code an operator cannot tell engine_error from
	// invalid_options from reconciler_exhausted, and without duration_ms
	// completion latency means going back to Postgres -- both of which are the
	// point of the phase.
	//
	// Present only where they mean something: a cancellation has no error code
	// and MarkActive has no elapsed time, and a field that is usually empty
	// trains readers to ignore it.
	if out.code != "" {
		fields = append(fields, "code", out.code)
	}
	// The caller's measurement when it has one, otherwise derived from the
	// row: a job canceled while ACTIVE has a real elapsed time that only the
	// locked row knows, and reporting nothing there loses the one number an
	// operator wants about a cancellation.
	//
	// EVERY terminal outcome carries the field, a job that never started
	// included, and that last case is 0 rather than an omission.
	//
	// It was an omission, on the reasoning that zero would be a number
	// invented rather than measured. That reasoning was already contradicted
	// three files away: recordUsage (usage.go) writes wall_seconds = 0 for
	// exactly this case, in the SAME transaction, because a job that never
	// occupied a worker consumed no wall time. The ledger and the log were
	// answering one question two ways, and the phase asks for a duration on
	// every done/failed/canceled record (66-01-PLAN.md) -- a consumer should
	// not have to treat a terminal record as two shapes (codex, PR #23).
	//
	// Zero is not a guess here: it is how long the job ran.
	switch {
	case out.hasElapsed:
		fields = append(fields, "duration_ms", logfmt.MS(out.elapsed.Milliseconds()))
	case isTerminal(to) && runningMS != nil:
		fields = append(fields, "duration_ms", logfmt.MS(int64(*runningMS)))
	case isTerminal(to):
		fields = append(fields, "duration_ms", logfmt.MS(0))
	}
	log.Println(logfmt.KV("job "+to, fields...))
	return nil
}

// isTerminal reports whether a status ends a job's life, and therefore whether
// a duration is meaningful for it.
func isTerminal(status string) bool {
	return status == StatusDone || status == StatusFailed || status == StatusCanceled
}

// derefStr reads a nullable text column as a string.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
