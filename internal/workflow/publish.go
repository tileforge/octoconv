// Package workflow stores a versioned conversion chain and admits a job onto a copy of it.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
)

const imagepdfCapability = "octoconv.convert.imagepdf"

// ErrRejected means the chain is not admissible. Nothing was stored.
var ErrRejected = errors.New("workflow rejected")

// ErrConflict means this name and version is already stored.
var ErrConflict = errors.New("workflow conflict")

// ErrNotFound means the caller has no snapshot for that job.
var ErrNotFound = errors.New("workflow not found")

// Step is one single-file conversion in a published chain.
type Step struct {
	CapabilityID      string
	CapabilityVersion int
	SourceFormat      string
	TargetFormat      string
	InputRole         string
	OutputRole        string
	Parameters        map[string]any
}

// PublishInput is an operator publish of one definition version.
type PublishInput struct {
	Name      string
	Version   int
	ClientIDs []uuid.UUID
	Steps     []Step
	Budgets   map[string]time.Duration
}

// Definition is one stored name@version and the clients who may admit it.
type Definition struct {
	Name      string          `json:"name"`
	Version   int             `json:"version"`
	ClientIDs []uuid.UUID     `json:"client_ids"`
	Document  json.RawMessage `json:"document"`
}

// Store is the Postgres store for definitions, grants, and snapshots.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a store for the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type rowQuery interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type storedStep struct {
	CapabilityID      string         `json:"capability_id"`
	CapabilityVersion int            `json:"capability_version"`
	SourceFormat      string         `json:"source_format"`
	TargetFormat      string         `json:"target_format"`
	InputRole         string         `json:"input_role"`
	OutputRole        string         `json:"output_role"`
	Parameters        map[string]any `json:"parameters"`
	LegacyEngine      string         `json:"legacy_engine"`
}

type storedDocument struct {
	Steps []storedStep `json:"steps"`
}

type snapshotStep struct {
	storedStep
	StepID uuid.UUID `json:"step_id"`
}

type snapshotDocument struct {
	Steps []snapshotStep `json:"steps"`
}

// Publish stores one definition version and one grant per client.
func (s *Store) Publish(ctx context.Context, in PublishInput) (Definition, error) {
	if in.Name == "" || in.Version < 1 || len(in.ClientIDs) == 0 {
		return Definition{}, ErrRejected
	}
	seen := make(map[uuid.UUID]struct{}, len(in.ClientIDs))
	for _, id := range in.ClientIDs {
		if id == uuid.Nil {
			return Definition{}, ErrRejected
		}
		if _, ok := seen[id]; ok {
			return Definition{}, ErrRejected
		}
		seen[id] = struct{}{}
	}
	var stored Definition
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM clients WHERE id = ANY($1::uuid[])`, in.ClientIDs).Scan(&n); err != nil {
			return fmt.Errorf("read clients: %w", err)
		}
		if n != len(in.ClientIDs) {
			return ErrRejected
		}
		resolved, err := resolveChain(ctx, tx, in.Steps, in.Budgets)
		if err != nil {
			return err
		}
		doc, err := json.Marshal(storedDocument{Steps: resolved})
		if err != nil {
			return fmt.Errorf("encode document: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_definitions (name, version, document)
			VALUES ($1, $2, $3::jsonb)`, in.Name, in.Version, doc); err != nil {
			if isUnique(err) {
				return ErrConflict
			}
			return fmt.Errorf("insert definition: %w", err)
		}
		for _, id := range in.ClientIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO workflow_grants (name, version, client_id)
				VALUES ($1, $2, $3)`, in.Name, in.Version, id); err != nil {
				return fmt.Errorf("insert grant: %w", err)
			}
		}
		stored = Definition{Name: in.Name, Version: in.Version, ClientIDs: append([]uuid.UUID(nil), in.ClientIDs...), Document: doc}
		return nil
	})
	if err != nil {
		return Definition{}, err
	}
	return stored, nil
}

// List returns every published definition and its client ids.
func (s *Store) List(ctx context.Context) ([]Definition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.name, d.version, d.document,
		       COALESCE(array_agg(g.client_id ORDER BY g.client_id) FILTER (WHERE g.client_id IS NOT NULL), '{}')
		FROM workflow_definitions d
		LEFT JOIN workflow_grants g ON g.name = d.name AND g.version = d.version
		GROUP BY d.name, d.version, d.document
		ORDER BY d.name, d.version`)
	if err != nil {
		return nil, fmt.Errorf("list definitions: %w", err)
	}
	defer rows.Close()
	var out []Definition
	for rows.Next() {
		var def Definition
		if err := rows.Scan(&def.Name, &def.Version, &def.Document, &def.ClientIDs); err != nil {
			return nil, fmt.Errorf("scan definition: %w", err)
		}
		if def.ClientIDs == nil {
			def.ClientIDs = []uuid.UUID{}
		}
		out = append(out, def)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list definitions: %w", err)
	}
	if out == nil {
		out = []Definition{}
	}
	return out, nil
}

// Snapshot copies the published document onto an existing job and sets one step_id per step.
func (s *Store) Snapshot(ctx context.Context, jobID uuid.UUID, name string, version int) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var raw []byte
		err := tx.QueryRow(ctx, `
			SELECT document FROM workflow_definitions WHERE name = $1 AND version = $2`, name, version).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRejected
		}
		if err != nil {
			return fmt.Errorf("read definition: %w", err)
		}
		doc, err := copyWithStepIDs(raw)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_snapshots (job_id, definition_name, definition_version, document)
			VALUES ($1, $2, $3, $4::jsonb)`, jobID, name, version, doc); err != nil {
			return fmt.Errorf("insert snapshot: %w", err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE jobs SET workflow_snapshot_id = id WHERE id = $1 AND workflow_snapshot_id IS NULL`, jobID)
		if err != nil {
			return fmt.Errorf("attach snapshot: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrRejected
		}
		return nil
	})
}

func resolveChain(ctx context.Context, q rowQuery, steps []Step, budgets map[string]time.Duration) ([]storedStep, error) {
	if len(steps) < 2 || len(steps) > 8 {
		return nil, ErrRejected
	}
	out := make([]storedStep, 0, len(steps))
	for i, step := range steps {
		resolved, err := resolveStep(ctx, q, step, budgets)
		if err != nil {
			return nil, err
		}
		if i > 0 && out[i-1].TargetFormat != resolved.SourceFormat {
			return nil, ErrRejected
		}
		out = append(out, resolved)
	}
	return out, nil
}

func resolveStep(ctx context.Context, q rowQuery, step Step, budgets map[string]time.Duration) (storedStep, error) {
	if step.CapabilityID == imagepdfCapability {
		return storedStep{}, ErrRejected
	}
	if step.CapabilityVersion < 1 || len(step.Parameters) != 0 {
		return storedStep{}, ErrRejected
	}
	source := convert.NormalizeFormat(step.SourceFormat)
	target := convert.NormalizeFormat(step.TargetFormat)
	if source == "" || target == "" {
		return storedStep{}, ErrRejected
	}
	var engine *string
	var schema []byte
	err := q.QueryRow(ctx, `
		SELECT legacy_engine, parameter_schema
		FROM worker_capabilities
		WHERE id = $1 AND version = $2`, step.CapabilityID, step.CapabilityVersion).Scan(&engine, &schema)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedStep{}, ErrRejected
	}
	if err != nil {
		return storedStep{}, fmt.Errorf("read capability: %w", err)
	}
	if engine == nil || *engine == "" {
		return storedStep{}, ErrRejected
	}
	if _, ok := budgets[*engine]; !ok {
		return storedStep{}, ErrRejected
	}
	input, output, err := loadSingleFilePorts(ctx, q, step.CapabilityID, step.CapabilityVersion)
	if err != nil {
		return storedStep{}, err
	}
	if step.InputRole != input.role || step.OutputRole != output.role {
		return storedStep{}, ErrRejected
	}
	var pair int
	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM worker_capability_formats
		WHERE capability_id = $1 AND capability_version = $2
		  AND source_format = $3 AND target_format = $4`,
		step.CapabilityID, step.CapabilityVersion, source, target).Scan(&pair); err != nil {
		return storedStep{}, fmt.Errorf("read format: %w", err)
	}
	if pair != 1 {
		return storedStep{}, ErrRejected
	}
	if err := validateEmptyParameters(schema); err != nil {
		return storedStep{}, ErrRejected
	}
	return storedStep{
		CapabilityID:      step.CapabilityID,
		CapabilityVersion: step.CapabilityVersion,
		SourceFormat:      source,
		TargetFormat:      target,
		InputRole:         input.role,
		OutputRole:        output.role,
		Parameters:        map[string]any{},
		LegacyEngine:      *engine,
	}, nil
}

type portRow struct {
	role string
	min  int
	max  int
}

func loadSingleFilePorts(ctx context.Context, q rowQuery, capabilityID string, version int) (portRow, portRow, error) {
	rows, err := q.Query(ctx, `
		SELECT direction, role, min_count, max_count
		FROM worker_capability_ports
		WHERE capability_id = $1 AND capability_version = $2
		ORDER BY direction, ordinal`, capabilityID, version)
	if err != nil {
		return portRow{}, portRow{}, fmt.Errorf("read ports: %w", err)
	}
	defer rows.Close()
	var inputs, outputs []portRow
	for rows.Next() {
		var direction string
		var row portRow
		if err := rows.Scan(&direction, &row.role, &row.min, &row.max); err != nil {
			return portRow{}, portRow{}, fmt.Errorf("scan port: %w", err)
		}
		switch direction {
		case "input":
			inputs = append(inputs, row)
		case "output":
			outputs = append(outputs, row)
		default:
			return portRow{}, portRow{}, ErrRejected
		}
	}
	if err := rows.Err(); err != nil {
		return portRow{}, portRow{}, fmt.Errorf("read ports: %w", err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return portRow{}, portRow{}, ErrRejected
	}
	in, out := inputs[0], outputs[0]
	if in.role != "source" || in.min != 1 || in.max != 1 || out.role != "primary" || out.min != 1 || out.max != 1 {
		return portRow{}, portRow{}, ErrRejected
	}
	return in, out, nil
}

func validateEmptyParameters(schema []byte) error {
	resolved, err := resolveParameterSchema(schema)
	if err != nil {
		return err
	}
	var instance any
	if err := json.Unmarshal([]byte(`{}`), &instance); err != nil {
		return err
	}
	return resolved.Validate(instance)
}

func resolveParameterSchema(raw []byte) (*jsonschema.Resolved, error) {
	if len(raw) == 0 || raw[0] != '{' {
		trimmed := raw
		for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\n' || trimmed[0] == '\t' || trimmed[0] == '\r') {
			trimmed = trimmed[1:]
		}
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, errors.New("parameter schema must be an object")
		}
		raw = trimmed
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	return schema.Resolve(nil)
}

func copyWithStepIDs(raw []byte) ([]byte, error) {
	var doc storedDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode document: %w", err)
	}
	snap := snapshotDocument{Steps: make([]snapshotStep, 0, len(doc.Steps))}
	for _, step := range doc.Steps {
		if step.Parameters == nil {
			step.Parameters = map[string]any{}
		}
		snap.Steps = append(snap.Steps, snapshotStep{storedStep: step, StepID: uuid.New()})
	}
	out, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	return out, nil
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
