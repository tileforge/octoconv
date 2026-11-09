package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

// AdmitFile is the stored upload that becomes job_inputs ordinal 0.
type AdmitFile struct {
	ObjectKey   string
	Filename    string
	Format      string
	SizeBytes   int64
	ContentType string
}

// AdmitInput is one client request to run a published chain.
// A nil File leaves jobs.step_id null. A File requires JobID and stores that upload.
type AdmitInput struct {
	ClientID        uuid.UUID
	Name            string
	Version         int
	SourceFormat    string
	Deadline        time.Time
	Budgets         map[string]time.Duration
	InputBytes      int64
	CallbackURL     string
	ProgressWebhook bool
	CorrelationID   string
	JobID           uuid.UUID
	File            *AdmitFile
}

// StatusStep is one step as the status read returns it.
type StatusStep struct {
	StepID            uuid.UUID
	CapabilityID      string
	CapabilityVersion int
	SourceFormat      string
	TargetFormat      string
}

// JobWorkflow is the chain status for one job.
// CurrentStep is set while the job is queued or active.
type JobWorkflow struct {
	Status      string
	CurrentStep uuid.UUID
	Steps       []StatusStep
}

// Admit stores one queued protocol job and a snapshot. It does not enqueue the job.
// A File also stores job_inputs ordinal 0 and sets jobs.step_id to the first snapshot step.
func (s *Store) Admit(ctx context.Context, in AdmitInput) (uuid.UUID, error) {
	if in.ClientID == uuid.Nil || in.Name == "" || in.Version < 1 || in.Deadline.IsZero() {
		return uuid.Nil, ErrRejected
	}
	source := convert.NormalizeFormat(in.SourceFormat)
	if source == "" {
		return uuid.Nil, ErrRejected
	}
	in.SourceFormat = source
	jobID := uuid.New()
	if in.File != nil {
		fileFormat := convert.NormalizeFormat(in.File.Format)
		if in.JobID == uuid.Nil || in.File.ObjectKey == "" || in.File.Filename == "" || fileFormat == "" || in.File.ContentType == "" || in.File.SizeBytes != in.InputBytes || fileFormat != source {
			return uuid.Nil, ErrRejected
		}
		in.File.Format = fileFormat
		jobID = in.JobID
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		resolved, raw, err := readResolvedChain(ctx, tx, in)
		if err != nil {
			return err
		}
		var sum time.Duration
		for _, step := range resolved {
			budget, ok := in.Budgets[step.LegacyEngine]
			if !ok {
				return ErrRejected
			}
			sum += budget
		}
		if in.Deadline.Before(time.Now().Add(sum)) {
			return ErrRejected
		}
		repo := s.jobs
		if repo == nil {
			repo = jobs.NewRepo(s.pool)
		}
		if err := repo.AdmitLocked(ctx, tx, in.ClientID, resolved[0].LegacyEngine, in.InputBytes); err != nil {
			return err
		}
		snap, err := copyWithStepIDs(raw)
		if err != nil {
			return err
		}
		var snapDoc snapshotDocument
		if err := json.Unmarshal(snap, &snapDoc); err != nil {
			return fmt.Errorf("decode snapshot: %w", err)
		}
		if len(snapDoc.Steps) == 0 {
			return ErrRejected
		}
		var firstStep *uuid.UUID
		if in.File != nil {
			id := snapDoc.Steps[0].StepID
			firstStep = &id
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO jobs (
				id, client_id, operation, engine, status, source_format, target_format,
				options, capability_id, capability_version, dispatch_owner, protocol_deadline, attempts,
				admitted_input_bytes, callback_url, progress_webhook, correlation_id, step_id
			) VALUES (
				$1, $2, 'convert', $3, 'queued', $4, $5,
				'{}'::jsonb, $6, $7, 'protocol', $8, 0, $9, $10, $11, NULLIF($12, ''), $13
			)`,
			jobID, in.ClientID, resolved[0].LegacyEngine, source, resolved[len(resolved)-1].TargetFormat,
			resolved[0].CapabilityID, resolved[0].CapabilityVersion, in.Deadline, in.InputBytes,
			in.CallbackURL, in.ProgressWebhook && in.CallbackURL != "", in.CorrelationID, firstStep,
		); err != nil {
			return fmt.Errorf("insert job: %w", err)
		}
		if in.File != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
				VALUES ($1, 0, $2, $3, $4, $5, $6)`,
				jobID, in.File.ObjectKey, in.File.Filename, in.File.Format, in.File.SizeBytes, in.File.ContentType); err != nil {
				return fmt.Errorf("insert job input: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_snapshots (job_id, definition_name, definition_version, document)
			VALUES ($1, $2, $3, $4::jsonb)`, jobID, in.Name, in.Version, snap); err != nil {
			return fmt.Errorf("insert snapshot: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE jobs SET workflow_snapshot_id = id WHERE id = $1`, jobID); err != nil {
			return fmt.Errorf("attach snapshot: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO job_events (job_id, from_status, to_status)
			VALUES ($1, NULL, 'queued')`, jobID); err != nil {
			return fmt.Errorf("insert job event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return jobID, nil
}

// FirstEngine reports the engine of the first step when the chain matches the source.
func (s *Store) FirstEngine(ctx context.Context, in AdmitInput) (string, error) {
	if in.ClientID == uuid.Nil || in.Name == "" || in.Version < 1 || in.Deadline.IsZero() {
		return "", ErrRejected
	}
	source := convert.NormalizeFormat(in.SourceFormat)
	if source == "" {
		return "", ErrRejected
	}
	in.SourceFormat = source
	var engine string
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		resolved, _, err := readResolvedChain(ctx, tx, in)
		if err != nil {
			return err
		}
		engine = resolved[0].LegacyEngine
		return nil
	})
	if err != nil {
		return "", err
	}
	return engine, nil
}

func readResolvedChain(ctx context.Context, tx pgx.Tx, in AdmitInput) ([]storedStep, []byte, error) {
	var granted int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM workflow_grants
		WHERE name = $1 AND version = $2 AND client_id = $3`,
		in.Name, in.Version, in.ClientID).Scan(&granted); err != nil {
		return nil, nil, fmt.Errorf("read grant: %w", err)
	}
	if granted != 1 {
		return nil, nil, ErrRejected
	}
	var raw []byte
	err := tx.QueryRow(ctx, `
		SELECT document FROM workflow_definitions WHERE name = $1 AND version = $2`,
		in.Name, in.Version).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrRejected
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read definition: %w", err)
	}
	var doc storedDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("decode document: %w", err)
	}
	steps := make([]Step, 0, len(doc.Steps))
	for _, step := range doc.Steps {
		steps = append(steps, Step{
			CapabilityID:      step.CapabilityID,
			CapabilityVersion: step.CapabilityVersion,
			SourceFormat:      step.SourceFormat,
			TargetFormat:      step.TargetFormat,
			InputRole:         step.InputRole,
			OutputRole:        step.OutputRole,
			Parameters:        step.Parameters,
		})
	}
	resolved, err := resolveChain(ctx, tx, steps, in.Budgets)
	if err != nil {
		return nil, nil, err
	}
	if resolved[0].SourceFormat != in.SourceFormat {
		return nil, nil, ErrRejected
	}
	return resolved, raw, nil
}

// Status loads the snapshot for a job owned by clientID.
func (s *Store) Status(ctx context.Context, clientID, jobID uuid.UUID) (JobWorkflow, error) {
	var status string
	var stepID *uuid.UUID
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT j.status, j.step_id, s.document
		FROM jobs j
		JOIN workflow_snapshots s ON s.job_id = j.id
		WHERE j.id = $1 AND j.client_id = $2 AND j.workflow_snapshot_id = j.id`,
		jobID, clientID).Scan(&status, &stepID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return JobWorkflow{}, ErrNotFound
	}
	if err != nil {
		return JobWorkflow{}, fmt.Errorf("read workflow: %w", err)
	}
	var doc snapshotDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return JobWorkflow{}, fmt.Errorf("decode snapshot: %w", err)
	}
	view := JobWorkflow{Status: status, Steps: make([]StatusStep, 0, len(doc.Steps))}
	if (status == jobs.StatusQueued || status == jobs.StatusActive) && stepID != nil {
		view.CurrentStep = *stepID
	}
	for _, step := range doc.Steps {
		view.Steps = append(view.Steps, StatusStep{
			StepID:            step.StepID,
			CapabilityID:      step.CapabilityID,
			CapabilityVersion: step.CapabilityVersion,
			SourceFormat:      step.SourceFormat,
			TargetFormat:      step.TargetFormat,
		})
	}
	return view, nil
}
