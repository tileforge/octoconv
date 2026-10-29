package presets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrVersionConflict = errors.New("preset version changed")

// MutationParams adds opt-in concurrency checks without changing legacy method signatures.
type MutationParams struct {
	Scope           string
	ClientID        *uuid.UUID
	Name            string
	Operation       *string
	TargetFormat    string
	Options         map[string]any
	Description     string
	ExpectedVersion *int
}

func lockIdentity(ctx context.Context, tx pgx.Tx, scope string, clientID *uuid.UUID, name string) error {
	owner := "system"
	if clientID != nil {
		owner = clientID.String()
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "octoconv:preset:"+scope+":"+owner+":"+name)
	return err
}

// missingMutation distinguishes stale own history from missing/foreign resources.
func missingMutation(ctx context.Context, tx pgx.Tx, scope string, clientID *uuid.UUID, name string, expected *int) error {
	if expected == nil {
		return ErrNotFound
	}
	var owned bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM presets WHERE scope=$1 AND name=$2 AND (client_id=$3 OR ($3::uuid IS NULL AND client_id IS NULL)))`, scope, name, clientID).Scan(&owned)
	if err != nil {
		return fmt.Errorf("check preset history: %w", err)
	}
	if owned {
		return ErrVersionConflict
	}
	return ErrNotFound
}

func (r *Repo) UpdateWithVersion(ctx context.Context, p MutationParams) (int, error) {
	raw, err := json.Marshal(p.Options)
	if err != nil {
		return 0, fmt.Errorf("marshal preset options: %w", err)
	}
	if p.Options == nil {
		raw = []byte("{}")
	}
	var next int
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockIdentity(ctx, tx, p.Scope, p.ClientID, p.Name); err != nil {
			return fmt.Errorf("lock preset identity: %w", err)
		}
		var current int
		var operation string
		err := tx.QueryRow(ctx, `SELECT version,operation FROM presets WHERE scope=$1 AND name=$2 AND is_active AND (client_id=$3 OR ($3::uuid IS NULL AND client_id IS NULL)) FOR UPDATE`, p.Scope, p.Name, p.ClientID).Scan(&current, &operation)
		if errors.Is(err, pgx.ErrNoRows) {
			return missingMutation(ctx, tx, p.Scope, p.ClientID, p.Name, p.ExpectedVersion)
		}
		if err != nil {
			return fmt.Errorf("lock active preset: %w", err)
		}
		if p.ExpectedVersion != nil && *p.ExpectedVersion != current {
			return ErrVersionConflict
		}
		if p.Operation != nil {
			operation = *p.Operation
		}
		next = current + 1
		if _, err := tx.Exec(ctx, `UPDATE presets SET is_active=false WHERE scope=$1 AND name=$2 AND version=$3 AND (client_id=$4 OR ($4::uuid IS NULL AND client_id IS NULL))`, p.Scope, p.Name, current, p.ClientID); err != nil {
			return fmt.Errorf("deactivate prior version: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO presets(name,version,scope,client_id,operation,target_format,options,description,is_active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,true)`, p.Name, next, p.Scope, p.ClientID, operation, p.TargetFormat, raw, p.Description)
		if err != nil {
			return fmt.Errorf("insert preset version: %w", err)
		}
		return nil
	})
	return next, err
}
func (r *Repo) DeactivateWithVersion(ctx context.Context, scope string, clientID *uuid.UUID, name string, expected *int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockIdentity(ctx, tx, scope, clientID, name); err != nil {
			return fmt.Errorf("lock preset identity: %w", err)
		}
		var id uuid.UUID
		var version int
		err := tx.QueryRow(ctx, `SELECT id,version FROM presets WHERE scope=$1 AND name=$2 AND is_active AND (client_id=$3 OR ($3::uuid IS NULL AND client_id IS NULL)) FOR UPDATE`, scope, name, clientID).Scan(&id, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return missingMutation(ctx, tx, scope, clientID, name, expected)
		}
		if err != nil {
			return fmt.Errorf("lock active preset: %w", err)
		}
		if expected != nil && *expected != version {
			return ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE presets SET is_active=false WHERE id=$1`, id); err != nil {
			return fmt.Errorf("deactivate preset: %w", err)
		}
		return nil
	})
}
