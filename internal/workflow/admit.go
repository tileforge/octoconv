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

// AdmitInput is one client request to run a published chain.
type AdmitInput struct {
	ClientID     uuid.UUID
	Name         string
	Version      int
	SourceFormat string
	Deadline     time.Time
	Budgets      map[string]time.Duration
	InputBytes   int64
}

// StatusStep is one step as the status read returns it.
type StatusStep struct {
	StepID            uuid.UUID
	CapabilityID      string
	CapabilityVersion int
	SourceFormat      string
	TargetFormat      string
}

// JobWorkflow is the chain status for one job. CurrentStep stays unset.
type JobWorkflow struct {
	Status string
	Steps  []StatusStep
}

// Admit stores one queued protocol job and a snapshot. It does not enqueue the job.
func (s *Store) Admit(ctx context.Context, in AdmitInput) (uuid.UUID, error) {
	if in.ClientID == uuid.Nil || in.Name == "" || in.Version < 1 || in.Deadline.IsZero() {
		return uuid.Nil, ErrRejected
	}
	source := convert.NormalizeFormat(in.SourceFormat)
	if source == "" {
		return uuid.Nil, ErrRejected
	}
	jobID := uuid.New()
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		var granted int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM workflow_grants
			WHERE name = $1 AND version = $2 AND client_id = $3`,
			in.Name, in.Version, in.ClientID).Scan(&granted); err != nil {
			return fmt.Errorf("read grant: %w", err)
		}
		if granted != 1 {
			return ErrRejected
		}
		var raw []byte
		err := tx.QueryRow(ctx, `
			SELECT document FROM workflow_definitions WHERE name = $1 AND version = $2`,
			in.Name, in.Version).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRejected
		}
		if err != nil {
			return fmt.Errorf("read definition: %w", err)
		}
		var doc storedDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("decode document: %w", err)
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
			return err
		}
		if resolved[0].SourceFormat != source {
			return ErrRejected
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO jobs (
				id, client_id, operation, engine, status, source_format, target_format,
				options, capability_id, capability_version, dispatch_owner, protocol_deadline, attempts,
				admitted_input_bytes
			) VALUES (
				$1, $2, 'convert', $3, 'queued', $4, $5,
				'{}'::jsonb, $6, $7, 'protocol', $8, 0, $9
			)`,
			jobID, in.ClientID, resolved[0].LegacyEngine, source, resolved[len(resolved)-1].TargetFormat,
			resolved[0].CapabilityID, resolved[0].CapabilityVersion, in.Deadline, in.InputBytes,
		); err != nil {
			return fmt.Errorf("insert job: %w", err)
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

// Status loads the snapshot for a job owned by clientID.
func (s *Store) Status(ctx context.Context, clientID, jobID uuid.UUID) (JobWorkflow, error) {
	var status string
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT j.status, s.document
		FROM jobs j
		JOIN workflow_snapshots s ON s.job_id = j.id
		WHERE j.id = $1 AND j.client_id = $2 AND j.workflow_snapshot_id = j.id`,
		jobID, clientID).Scan(&status, &raw)
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
