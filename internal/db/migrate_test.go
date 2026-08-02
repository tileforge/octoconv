package db

import (
	"context"
	"os"
	"sort"
	"testing"
)

// TestMigrationsApplyClientUsageDaily is a schema contract for Plans 02 and
// 04: it asserts, by querying the live catalog (not by re-reading the .sql
// file) that migration 0011 actually produced the shape the rollup UPSERT's
// conflict target and the purge index depend on. A future migration that
// changes the key shape must fail here loudly instead of silently breaking
// Plan 02's INSERT ... ON CONFLICT or Plan 04's index-driven purge.
func TestMigrationsApplyClientUsageDaily(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()

	pool, err := Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// 1. Table exists.
	var regclass *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.client_usage_daily')::text`).Scan(&regclass); err != nil {
		t.Fatalf("query to_regclass: %v", err)
	}
	if regclass == nil {
		t.Fatal("client_usage_daily table does not exist")
	}

	// 2. Primary-key column set is exactly client_id, engine, day, status.
	rows, err := pool.Query(ctx, `
		SELECT a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY(c.conkey)
		WHERE c.conrelid = 'client_usage_daily'::regclass AND c.contype = 'p'`)
	if err != nil {
		t.Fatalf("query PK columns: %v", err)
	}
	var pkCols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan PK column: %v", err)
		}
		pkCols = append(pkCols, col)
	}
	rows.Close()
	sort.Strings(pkCols)
	wantPK := []string{"client_id", "day", "engine", "status"}
	if len(pkCols) != len(wantPK) {
		t.Fatalf("PK column set: want %v, got %v", wantPK, pkCols)
	}
	for i, col := range wantPK {
		if pkCols[i] != col {
			t.Fatalf("PK column set: want %v, got %v", wantPK, pkCols)
		}
	}

	// 3. job_count/bytes_in/bytes_out are bigint NOT NULL, wall_seconds is
	// double precision NOT NULL.
	wantTypes := map[string]string{
		"job_count":    "bigint",
		"bytes_in":     "bigint",
		"bytes_out":    "bigint",
		"wall_seconds": "double precision",
	}
	for col, wantType := range wantTypes {
		var dataType, isNullable string
		err := pool.QueryRow(ctx, `
			SELECT data_type, is_nullable
			FROM information_schema.columns
			WHERE table_name = 'client_usage_daily' AND column_name = $1`, col,
		).Scan(&dataType, &isNullable)
		if err != nil {
			t.Fatalf("query column %s: %v", col, err)
		}
		if dataType != wantType {
			t.Fatalf("column %s: want type %s, got %s", col, wantType, dataType)
		}
		if isNullable != "NO" {
			t.Fatalf("column %s: want NOT NULL, got nullable", col)
		}
	}

	// 4. client_usage_daily_day_idx exists (Plan 04's purge depends on it).
	var idxName *string
	err = pool.QueryRow(ctx, `
		SELECT indexname FROM pg_indexes
		WHERE tablename = 'client_usage_daily' AND indexname = 'client_usage_daily_day_idx'`,
	).Scan(&idxName)
	if err != nil || idxName == nil {
		t.Fatalf("client_usage_daily_day_idx: want to exist, query error: %v", err)
	}

	// 5. client_id foreign key to clients has delete rule CASCADE.
	var deleteRule string
	err = pool.QueryRow(ctx, `
		SELECT rc.delete_rule
		FROM information_schema.referential_constraints rc
		JOIN information_schema.table_constraints tc
			ON tc.constraint_name = rc.constraint_name AND tc.table_name = 'client_usage_daily'
		WHERE tc.constraint_type = 'FOREIGN KEY'`,
	).Scan(&deleteRule)
	if err != nil {
		t.Fatalf("query FK delete rule: %v", err)
	}
	if deleteRule != "CASCADE" {
		t.Fatalf("client_id FK delete rule: want CASCADE, got %s", deleteRule)
	}

	// 6. Running Migrate twice in the same test is idempotent.
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate call: want nil, got %v", err)
	}
}
