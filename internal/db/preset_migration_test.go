package db

import (
	"context"
	"os"
	"testing"
)

func TestPresetOperationsMigrationPreservesRecords(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// The temporary table shadows public.presets only on this connection.
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE presets(name text,operation text CONSTRAINT presets_operation_check CHECK(operation IN ('convert','extract','archive','inspect','render')),options jsonb) ON COMMIT DROP; INSERT INTO presets VALUES('old','convert','{"pdf_profile":"pdf/a-2b"}')`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0020_preset_operations.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	var operation, profile string
	if err := tx.QueryRow(ctx, `SELECT operation,options->>'pdf_profile' FROM presets WHERE name='old'`).Scan(&operation, &profile); err != nil {
		t.Fatal(err)
	}
	if operation != "convert" || profile != "pdf/a-2b" {
		t.Fatalf("old row changed %s %s", operation, profile)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO presets VALUES('new','sanitize','{}')`); err != nil {
		t.Fatal(err)
	}
	validation, err := migrationsFS.ReadFile("migrations/0021_validate_preset_operations.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(validation)); err != nil {
		t.Fatal(err)
	}
	var validated bool
	if err := tx.QueryRow(ctx, `SELECT convalidated FROM pg_constraint WHERE conrelid='pg_temp.presets'::regclass AND conname='presets_operation_check'`).Scan(&validated); err != nil {
		t.Fatal(err)
	}
	if !validated {
		t.Fatal("preset operation constraint remains unvalidated")
	}
}
