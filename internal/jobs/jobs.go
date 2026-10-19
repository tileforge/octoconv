// Package jobs is the Postgres-backed repository for conversion jobs and their
// inputs, outputs and event log. Postgres is the system of record: status truth
// always lives here.
package jobs

import (
	"time"

	"github.com/google/uuid"
)

// Job statuses (mirror the CHECK constraint in the jobs table).
const (
	StatusAwaitingUpload = "awaiting_upload"
	StatusQueued         = "queued"
	StatusActive         = "active"
	StatusDone           = "done"
	StatusFailed         = "failed"
	StatusCanceled       = "canceled"
)

// Job is a row of the jobs table (subset used by the image slice).
type Job struct {
	ID        uuid.UUID
	ClientID  uuid.UUID
	Operation string
	Engine    string
	// CorrelationID is the caller's own identifier, echoed back in the job
	// status and in the webhook. Empty when the caller sent none -- this
	// service never generates one, because job_id already identifies the job.
	CorrelationID string
	Status        string
	SourceFormat  string
	TargetFormat  string
	CallbackURL   string
	// ProgressWebhook is the per-job opt-in flag (0008_progress_webhook.sql,
	// PUSH-02/D-03) gating the worker's best-effort inline progress-webhook
	// fire; deliberately a dedicated NOT NULL boolean column, never stored in
	// Opts/options jsonb, so it is never reachable through the opts
	// validation pipeline.
	ProgressWebhook bool
	// Opts carries the server-normalized, already-validated conversion options
	// serialized to/from the jobs.options jsonb column (never raw client JSON,
	// per D-08).
	Opts         map[string]any
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Attempts     int
	// ProgressPct / EtaSeconds / SourceDurationSeconds are the Phase 41
	// denormalized progress columns (0007_job_progress.sql), written by the
	// non-locking Repo.UpdateProgress/Repo.SetSourceDuration paths (D-03b) --
	// deliberately outside the guarded transition() lifecycle. All three stay
	// nil for every engine with no machine-readable progress signal
	// (image/document/html, the whisper transcribe stage), and for an av job
	// before its first tick lands (PROG-01/02/04).
	ProgressPct           *int
	EtaSeconds            *int
	SourceDurationSeconds *float64
	Stage                 *string
	StageIndex            *int
	StageCount            *int
}

// Input is a row of the job_inputs table.
type Input struct {
	Ordinal     int
	ObjectKey   string
	Filename    string
	Format      string
	SizeBytes   int64
	ContentType string
}

// OutputRolePrimary is the job_outputs.role value that identifies a job's
// PRIMARY result -- the value the database defaults to (migration
// 0015_operation_and_output_roles.sql), the value Repo.MarkDone's D-13 guard
// counts (that SQL literal inside the guard's query string stays a literal;
// this constant does not reach into SQL text), and the value EVERY consumer
// (internal/api's handleGetJob, internal/worker's terminal webhook) must
// select on instead of taking the first element of a []Output slice. Declared
// once here, on the type it describes, so a second unexported copy of
// "primary" can never drift from this one the way internal/api and
// internal/worker had drifted before this constant existed (codex PR #7
// finding #1).
const OutputRolePrimary = "primary"

// OutputRoleReport is the job_outputs.role value for an output that DESCRIBES
// the job rather than being its result -- the sanitize report (Phase 60,
// SANI-05). Declared here for the same reason OutputRolePrimary is: the
// string is written by internal/worker and selected on by internal/api, and
// two unexported copies of "report" in those packages is exactly the drift
// that made OutputRolePrimary necessary.
//
// The database CHECK constraint in 0015_operation_and_output_roles.sql is the
// authority on which role values are legal; this constant names one of them,
// it does not define the set.
const OutputRoleReport = "report"

const OutputRoleMeasurements = "measurements"

// Output is a row of the job_outputs table. Role comes from job_outputs.role;
// its default (OutputRolePrimary) is supplied by the database (0015_operation_and_output_roles.sql,
// D-16), never by this Go layer.
type Output struct {
	Ordinal     int
	ObjectKey   string
	Filename    string
	Format      string
	SizeBytes   int64
	ContentType string
	Role        string
}

// JobSummary is the per-row listing shape returned by Repo.List, selected
// directly from the jobs row (D-07, no N+1: no per-row Outputs/Inputs/
// PresignGet calls). ProgressPct stays nil until Phase 41 populates it
// (0007_job_progress.sql). ErrorCode is empty unless Status is StatusFailed.
type JobSummary struct {
	ID           uuid.UUID
	Status       string
	Engine       string
	SourceFormat string
	TargetFormat string
	ProgressPct  *int
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ErrorCode    string
}

// ListFilter narrows Repo.List's result set. Each field is empty-means-unset;
// validating field values against the known status/engine sets is the
// handler's responsibility (Plan 02) — this type only carries them through.
type ListFilter struct {
	Status string
	Engine string
}
