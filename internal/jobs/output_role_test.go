package jobs

import (
	"context"
	"strings"
	"testing"
)

// TestAddOutputWithoutRoleReadsBackAsPrimary is D-16's core proof: AddOutput
// is called with Role left at its Go zero-value (""), and the row read back
// through Outputs must still be "primary". This demonstrates that the
// 'primary' default lives in the database (job_outputs.role's column
// default, 0015_operation_and_output_roles.sql), not in this Go layer --
// AddOutput's INSERT never names the role column at all (repo.go), so there
// is no Go-side default to drift out of sync with the DB default (Pitfall 13
// closed by construction, not by a matching pair of defaults kept in sync by
// hand).
func TestAddOutputWithoutRoleReadsBackAsPrimary(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	jobID := createTestJob(t, r, createTestClient(t, r), "image")

	if err := r.AddOutput(ctx, jobID, Output{
		Ordinal:     0,
		ObjectKey:   "results/role-test/0-out.webp",
		Filename:    "out.webp",
		Format:      "webp",
		SizeBytes:   567,
		ContentType: "image/webp",
		// Role intentionally left unset (zero value "").
	}); err != nil {
		t.Fatalf("AddOutput: %v", err)
	}

	outs, err := r.Outputs(ctx, jobID)
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("len(outs) = %d, want 1", len(outs))
	}
	if outs[0].Role != "primary" {
		t.Fatalf("Role = %q, want %q (DB default must apply even though the Go struct field was never set)", outs[0].Role, "primary")
	}
}

// TestOutputRoleDefaultAppliesToPreExistingRows proves -- against the
// running PostgreSQL 18 instance, not general Postgres knowledge -- that a
// row shaped exactly like the pre-migration schema (an INSERT that never
// names the role column, i.e. what every AddOutput call looked like before
// 0015_operation_and_output_roles.sql existed) reads back as 'primary' with
// no backfill statement ever having touched it. This is T-57-10's mitigation
// evidence: a row with an undefined role cannot exist, so it cannot be
// "accidentally" picked as a job's primary result.
func TestOutputRoleDefaultAppliesToPreExistingRows(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	var serverVersion int
	if err := r.pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&serverVersion); err != nil {
		t.Fatalf("read server_version_num: %v", err)
	}
	if serverVersion < 180000 {
		t.Skipf("this proof targets PostgreSQL 18 specifically (server_version_num=%d); not considered closed on an older major version", serverVersion)
	}

	jobID := createTestJob(t, r, createTestClient(t, r), "image")

	// Deliberately the SAME column list AddOutput used before this plan added
	// role to job_outputs (repo.go's INSERT), proving the additive migration
	// needed no matching Go-side change to keep existing rows correct.
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		jobID, 0, "results/role-test/pre-existing.webp", "pre-existing.webp", "webp", int64(42), "image/webp",
	); err != nil {
		t.Fatalf("insert pre-existing-shape row: %v", err)
	}

	var role string
	if err := r.pool.QueryRow(ctx,
		`SELECT role FROM job_outputs WHERE job_id = $1 AND ordinal = 0`, jobID,
	).Scan(&role); err != nil {
		t.Fatalf("read back role: %v", err)
	}
	if role != "primary" {
		t.Fatalf("role = %q, want %q for a row inserted with no role column named", role, "primary")
	}
}

// TestOutputRoleRejectsValueOutsideAllowlist proves the CHECK is closed at
// the database level: no free-text role value is admitted, even via a raw
// SQL path that bypasses the Go layer entirely (T-57-09's mitigation
// evidence). All four legal values must still insert cleanly on distinct
// ordinals of the same job.
func TestOutputRoleRejectsValueOutsideAllowlist(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	jobID := createTestJob(t, r, createTestClient(t, r), "image")

	_, err := r.pool.Exec(ctx, `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type, role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		jobID, 0, "results/role-test/bogus.webp", "bogus.webp", "webp", int64(1), "image/webp", "bogus",
	)
	if err == nil {
		t.Fatal("expected an error inserting role = 'bogus', got nil")
	}
	// job_outputs_role_check is the constraint's actual name, confirmed live
	// via `SELECT conname FROM pg_constraint WHERE conrelid = 'job_outputs'::regclass
	// AND contype = 'c'` against the running database after applying 0015 --
	// matching the standard <table>_<column>_check auto-naming convention for
	// an inline column CHECK added via ALTER TABLE ADD COLUMN.
	if !strings.Contains(err.Error(), "job_outputs_role_check") {
		t.Fatalf("error %q does not mention job_outputs_role_check", err.Error())
	}

	legal := []string{"primary", "report", "preview", "thumbnail"}
	for i, role := range legal {
		if _, err := r.pool.Exec(ctx, `
			INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type, role)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			jobID, i+1, "results/role-test/"+role+".webp", role+".webp", "webp", int64(1), "image/webp", role,
		); err != nil {
			t.Fatalf("insert legal role %q: %v", role, err)
		}
	}
}

// TestJobsOperationCheckAcceptsSanitize proves jobs_operation_check now
// admits 'sanitize' (D-15) while still rejecting garbage. This is an
// intentional divergence from the API surface: the DB accepts the value, but
// handleCreateJob rejects it with 422 unsupported_operation until Phase 60
// registers a converter for it (T-57-11's mitigation) -- this test only
// proves the schema half of that split, not the API's.
func TestJobsOperationCheckAcceptsSanitize(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	if _, err := r.Create(ctx, CreateParams{
		ClientID:     clientID,
		Operation:    "sanitize",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "png",
		Input: Input{
			Ordinal:     0,
			ObjectKey:   "uploads/role-test/sanitize-in.png",
			Filename:    "in.png",
			Format:      "png",
			SizeBytes:   1234,
			ContentType: "image/png",
		},
	}); err != nil {
		t.Fatalf("Create with operation=sanitize: %v", err)
	}

	_, err := r.Create(ctx, CreateParams{
		ClientID:     clientID,
		Operation:    "bogus",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "png",
		Input: Input{
			Ordinal:     0,
			ObjectKey:   "uploads/role-test/bogus-op-in.png",
			Filename:    "in.png",
			Format:      "png",
			SizeBytes:   1234,
			ContentType: "image/png",
		},
	})
	if err == nil {
		t.Fatal("expected an error creating a job with operation = 'bogus', got nil")
	}
	if !strings.Contains(err.Error(), "jobs_operation_check") {
		t.Fatalf("error %q does not mention jobs_operation_check", err.Error())
	}
}

// TestOutputsReturnsRoleForEveryOutput writes two outputs of one job with
// distinct roles directly via SQL and confirms Repo.Outputs returns both
// roles, in ordinal order.
func TestOutputsReturnsRoleForEveryOutput(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	jobID := createTestJob(t, r, createTestClient(t, r), "image")

	if _, err := r.pool.Exec(ctx, `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type, role)
		VALUES ($1, 0, $2, $3, $4, $5, $6, $7), ($1, 1, $8, $9, $10, $11, $12, $13)`,
		jobID,
		"results/role-test/multi-0.webp", "multi-0.webp", "webp", int64(10), "image/webp", "primary",
		"results/role-test/multi-1.json", "multi-1.json", "json", int64(20), "application/json", "report",
	); err != nil {
		t.Fatalf("insert two outputs: %v", err)
	}

	outs, err := r.Outputs(ctx, jobID)
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("len(outs) = %d, want 2", len(outs))
	}
	if outs[0].Ordinal != 0 || outs[0].Role != "primary" {
		t.Fatalf("outs[0] = %+v, want ordinal=0 role=primary", outs[0])
	}
	if outs[1].Ordinal != 1 || outs[1].Role != "report" {
		t.Fatalf("outs[1] = %+v, want ordinal=1 role=report", outs[1])
	}
}
