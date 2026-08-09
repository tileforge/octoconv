package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a job does not exist.
var ErrNotFound = errors.New("job not found")

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
	Input         Input
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
		if err := admitLocked(ctx, tx, p.ClientID, p.Engine, p.Input.SizeBytes, r.quotaObserver, r.quotaDisabled); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO jobs (id, client_id, operation, engine, status, source_format, target_format, preset_name, preset_version, callback_url, progress_webhook, options)
			VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8, $9, $10, $11)`,
			jobID, p.ClientID, p.Operation, p.Engine, p.SourceFormat, p.TargetFormat, pname, pver, p.CallbackURL, p.ProgressWebhook, optsJSON,
		); err != nil {
			return fmt.Errorf("insert job: %w", err)
		}

		in := p.Input
		if _, err := tx.Exec(ctx, `
			INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			jobID, in.Ordinal, in.ObjectKey, in.Filename, in.Format, in.SizeBytes, in.ContentType,
		); err != nil {
			return fmt.Errorf("insert job_input: %w", err)
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

// MarkActive transitions a job to active and stamps started_at, appending an
// event. The transition allows queued->active AND active->active so asynq's
// internal same-task retry re-entering the handler does not trip the illegal-
// transition guard; started_at uses COALESCE so it stays pinned to the FIRST
// activation, not the most recent retry, which the reconciler's active-
// staleness check depends on for true elapsed running time.
func (r *Repo) MarkActive(ctx context.Context, id uuid.UUID) error {
	return r.transition(ctx, id, StatusActive, []string{StatusQueued, StatusActive}, nil, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'active', started_at = COALESCE(started_at, now()), attempts = attempts + 1 WHERE id = $1`, id)
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
	return r.transition(ctx, id, StatusDone, []string{StatusActive}, nil, func(ctx context.Context, tx pgx.Tx) error {
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
	return r.transition(ctx, id, StatusFailed, []string{StatusQueued, StatusActive}, detail, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'failed', finished_at = now(), error_code = $2, error_message = $3 WHERE id = $1`,
			id, code, message); err != nil {
			return err
		}
		return r.recordUsage(ctx, tx, id, StatusFailed, attempt)
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
	return r.transition(ctx, id, StatusCanceled, []string{StatusQueued, StatusActive}, nil, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'canceled', finished_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return r.recordUsage(ctx, tx, id, StatusCanceled, 0)
	})
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
	return r.transition(ctx, id, StatusQueued, []string{StatusQueued, StatusActive}, detail, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE jobs SET status = 'queued' WHERE id = $1`, id)
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
		WHERE (status = 'queued' AND created_at < $1)
		   OR (status = 'active' AND started_at < $2)`,
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

// Get loads a job by id. Returns ErrNotFound if it does not exist.
func (r *Repo) Get(ctx context.Context, id uuid.UUID) (*Job, error) {
	var j Job
	var src, tgt, cb, code, msg *string
	var clientID *uuid.UUID
	// options is NOT NULL (0001_init.sql:25), so it is scanned into a plain
	// []byte, NOT a *string pointer, unlike the other nullable columns above.
	var optsJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, client_id, operation, engine, status, source_format, target_format,
		       callback_url, progress_webhook, options, error_code, error_message, created_at, started_at, finished_at,
		       progress_pct, eta_seconds, source_duration_seconds
		FROM jobs WHERE id = $1`, id,
	).Scan(&j.ID, &clientID, &j.Operation, &j.Engine, &j.Status, &src, &tgt,
		&cb, &j.ProgressWebhook, &optsJSON, &code, &msg, &j.CreatedAt, &j.StartedAt, &j.FinishedAt,
		&j.ProgressPct, &j.EtaSeconds, &j.SourceDurationSeconds)
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
// N+1 (D-07). It reuses jobs_client_created_idx; no new migration/index.
//
// limit+1 rows are fetched as a probe for whether a further page exists: if
// more than limit rows come back, the extra row is dropped and the next
// Cursor is built from the LAST RETURNED row; otherwise the next cursor is
// nil (no further page).
func (r *Repo) List(ctx context.Context, clientID uuid.UUID, f ListFilter, cursor *Cursor, limit int) ([]JobSummary, *Cursor, error) {
	query := `
		SELECT id, status, engine, source_format, target_format, progress_pct,
		       created_at, started_at, finished_at, error_code
		FROM jobs WHERE client_id = $1`
	args := []any{clientID}

	if f.Status != "" {
		args = append(args, f.Status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if f.Engine != "" {
		args = append(args, f.Engine)
		query += fmt.Sprintf(" AND engine = $%d", len(args))
	}
	if cursor != nil {
		args = append(args, cursor.CreatedAt, cursor.ID)
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}

	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

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
			&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &code); err != nil {
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
	}
	return out, next, nil
}

// transition performs a guarded status change plus an append to job_events in a
// single transaction. It errors if the job is not in one of the allowed source
// statuses (concurrency/idempotency guard). detail, when non-nil, is marshaled
// into the job_events.detail jsonb column; when nil, the column stays NULL.
func (r *Repo) transition(
	ctx context.Context,
	id uuid.UUID,
	to string,
	allowedFrom []string,
	detail map[string]any,
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
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		var from string
		if err := tx.QueryRow(ctx,
			`SELECT status FROM jobs WHERE id = $1 FOR UPDATE`, id,
		).Scan(&from); err != nil {
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
