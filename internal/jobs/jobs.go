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
	ID           uuid.UUID
	ClientID     uuid.UUID
	Operation    string
	Engine       string
	Status       string
	SourceFormat string
	TargetFormat string
	CallbackURL  string
	// Opts carries the server-normalized, already-validated conversion options
	// serialized to/from the jobs.options jsonb column (never raw client JSON,
	// per D-08).
	Opts         map[string]any
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
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

// Output is a row of the job_outputs table.
type Output struct {
	Ordinal     int
	ObjectKey   string
	Filename    string
	Format      string
	SizeBytes   int64
	ContentType string
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
