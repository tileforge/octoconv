package workerproto

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/storage"
)

func TestWorkflowSnapshotIsNotClaimed(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	live := openHello(t)
	ctx := context.Background()
	rows, err := live.pool.Query(ctx, `
		SELECT id, not_before FROM jobs
		WHERE dispatch_owner = 'protocol'
		  AND status = 'queued'
		  AND capability_id = 'octoconv.convert.image'
		  AND capability_version = 1
		  AND workflow_snapshot_id IS NULL
		  AND (not_before IS NULL OR not_before <= now())`)
	if err != nil {
		t.Fatalf("list eligible: %v", err)
	}
	type parked struct {
		id        uuid.UUID
		notBefore *time.Time
	}
	var held []parked
	for rows.Next() {
		var row parked
		if err := rows.Scan(&row.id, &row.notBefore); err != nil {
			rows.Close()
			t.Fatalf("scan eligible: %v", err)
		}
		held = append(held, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("list eligible: %v", err)
	}
	rows.Close()
	if len(held) > 0 {
		ids := make([]uuid.UUID, 0, len(held))
		for _, row := range held {
			ids = append(ids, row.id)
		}
		if _, err := live.pool.Exec(ctx, `
			UPDATE jobs SET not_before = clock_timestamp() + interval '1 day'
			WHERE id = ANY($1::uuid[])`, ids); err != nil {
			t.Fatalf("park jobs: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, row := range held {
			_, _ = live.pool.Exec(context.Background(), `UPDATE jobs SET not_before = $2 WHERE id = $1`, row.id, row.notBefore)
		}
	})

	snapID := uuid.New()
	plainID := uuid.New()
	insertClaimJob(t, live, snapID, time.Now().Add(-2*time.Hour))
	if _, err := live.pool.Exec(ctx, `
		INSERT INTO workflow_snapshots (job_id, definition_name, definition_version, document)
		VALUES ($1, 'claim-skip', 1, '{"steps":[]}'::jsonb)`, snapID); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if _, err := live.pool.Exec(ctx, `UPDATE jobs SET workflow_snapshot_id = id WHERE id = $1`, snapID); err != nil {
		t.Fatalf("attach snapshot: %v", err)
	}
	insertClaimJob(t, live, plainID, time.Now().Add(-time.Hour))
	t.Cleanup(func() {
		_, _ = live.pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id = ANY($1)`, []uuid.UUID{snapID, plainID})
		_, _ = live.pool.Exec(context.Background(), `UPDATE jobs SET workflow_snapshot_id = NULL WHERE id = ANY($1)`, []uuid.UUID{snapID, plainID})
		_, _ = live.pool.Exec(context.Background(), `DELETE FROM workflow_snapshots WHERE job_id = ANY($1)`, []uuid.UUID{snapID, plainID})
		_, _ = live.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = ANY($1)`, []uuid.UUID{snapID, plainID})
	})

	status, resp := pullOnce(t, live, 1)
	if status != 200 {
		t.Fatalf("pull status %d", status)
	}
	assignment := decodeOneAssignment(t, resp)
	got, err := jsonUUID(assignment["job_id"])
	if err != nil {
		t.Fatal(err)
	}
	if got != plainID {
		t.Fatalf("assigned %s", got)
	}
	var snapStatus string
	var attempts int
	if err := live.pool.QueryRow(ctx, `SELECT status, attempts FROM jobs WHERE id = $1`, snapID).Scan(&snapStatus, &attempts); err != nil {
		t.Fatalf("read snapshot job: %v", err)
	}
	if snapStatus != "queued" || attempts != 0 {
		t.Fatalf("snapshot job %s attempts %d", snapStatus, attempts)
	}
	if countHolds(t, live.pool, snapID) != 0 {
		t.Fatal("snapshot job has a hold")
	}
	if countHolds(t, live.pool, plainID) != 1 {
		t.Fatal("plain job was not assigned")
	}
}

func insertClaimJob(t *testing.T, live liveSession, id uuid.UUID, created time.Time) {
	t.Helper()
	if _, err := live.pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			options, created_at
		) VALUES (
			$1, 'convert', 'image', 'queued', 'png', 'jpg',
			'protocol', 'octoconv.convert.image', 1, now() + interval '1 hour',
			'{}', $2
		)`, id, created); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := live.pool.Exec(context.Background(), `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes)
		VALUES ($1, 0, $2, 'in.png', 'png', 4)`, id, storage.InputKey(id, 0, "in.png")); err != nil {
		t.Fatalf("insert input: %v", err)
	}
}
