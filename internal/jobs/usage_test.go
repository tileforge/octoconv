package jobs

import (
	"context"
	"testing"
)

// TestRecordUsageSumsAllOrdinals pins D-03 (the corrected reading): a job
// with two job_inputs and two job_outputs must yield a client_usage_daily
// row whose bytes_in/bytes_out are the SUM over every ordinal, never the
// ordinal-0 row alone. This is the primary RED case for Task 1: it fails to
// compile until recordUsage/the widened MarkDone signature exist, then fails
// on assertion until the correlated sum(size_bytes) subqueries are wired in.
func TestRecordUsageSumsAllOrdinals(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	id, err := r.Create(ctx, CreateParams{
		ClientID:  clientID,
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{Ordinal: 0, ObjectKey: "uploads/ru1/0-in.png", Filename: "in.png", Format: "png", SizeBytes: 100},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A second input row at ordinal 1 — the whole point of this test is that
	// the rollup must sum across it, not stop at ordinal 0 (SEED-017).
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, 1, 'uploads/ru1/1-in.png', 'in2.png', 'png', 200, 'image/png')`, id,
	); err != nil {
		t.Fatalf("insert second job_input: %v", err)
	}

	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if err := r.AddOutput(ctx, id, Output{Ordinal: 0, ObjectKey: "results/ru1/0-out.webp", Filename: "out.webp", Format: "webp", SizeBytes: 10, ContentType: "image/webp"}); err != nil {
		t.Fatalf("AddOutput (0): %v", err)
	}
	if err := r.AddOutput(ctx, id, Output{Ordinal: 1, ObjectKey: "results/ru1/1-out.webp", Filename: "out2.webp", Format: "webp", SizeBytes: 20, ContentType: "image/webp"}); err != nil {
		t.Fatalf("AddOutput (1): %v", err)
	}

	if err := r.MarkDone(ctx, id, 0); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	var jobCount int64
	var bytesIn, bytesOut int64
	var status string
	if err := r.pool.QueryRow(ctx, `
		SELECT job_count, bytes_in, bytes_out, status FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND status = 'done'`, clientID,
	).Scan(&jobCount, &bytesIn, &bytesOut, &status); err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("job_count = %d, want 1", jobCount)
	}
	if bytesIn != 300 {
		t.Fatalf("bytes_in = %d, want 300 (100+200, summed over both ordinals)", bytesIn)
	}
	if bytesOut != 30 {
		t.Fatalf("bytes_out = %d, want 30 (10+20, summed over both ordinals)", bytesOut)
	}
	if status != StatusDone {
		t.Fatalf("status = %q, want %q", status, StatusDone)
	}
}
