package workerproto

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/storage"
)

func TestMigrateTwiceKeepsQueuedActiveAndDone(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatal("db.Connect failed")
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	clientID := insertClient(t, pool)
	queuedID := uuid.New()
	activeID := uuid.New()
	doneID := uuid.New()
	ids := []uuid.UUID{queuedID, activeID, doneID}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM worker_slot_holds WHERE job_id = ANY($1)`, ids)
		_, _ = pool.Exec(cctx, `DELETE FROM job_outputs WHERE job_id = ANY($1)`, ids)
		_, _ = pool.Exec(cctx, `DELETE FROM jobs WHERE id = ANY($1)`, ids)
		_, _ = pool.Exec(cctx, `DELETE FROM clients WHERE id = $1`, clientID)
	})

	insertRollbackJob(t, pool, queuedID, clientID, "queued", "legacy", "now() - interval '2 hours'")
	insertRollbackJob(t, pool, activeID, clientID, "active", "protocol", "now() - interval '1 hour'")
	insertRollbackJob(t, pool, doneID, clientID, "done", "legacy", "now()")
	if _, err := pool.Exec(ctx, `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, 0, $2, 'accepted.png', 'png', 4, 'image/png')`,
		doneID, "results/rollback/accepted"); err != nil {
		t.Fatalf("insert output: %v", err)
	}

	before := map[uuid.UUID]jobPin{
		queuedID: jobPinOf(t, pool, queuedID),
		activeID: jobPinOf(t, pool, activeID),
		doneID:   jobPinOf(t, pool, doneID),
	}
	if before[queuedID].status != "queued" || before[queuedID].owner != "legacy" || before[queuedID].output != "" {
		t.Fatalf("queued pin %+v", before[queuedID])
	}
	if before[activeID].status != "active" || before[activeID].owner != "protocol" || before[activeID].output != "" {
		t.Fatalf("active pin %+v", before[activeID])
	}
	if before[doneID].status != "done" || before[doneID].owner != "legacy" || before[doneID].output != "results/rollback/accepted" {
		t.Fatalf("done pin %+v", before[doneID])
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	for id, want := range before {
		got := jobPinOf(t, pool, id)
		if got != want {
			t.Fatalf("job %s changed from %+v to %+v", id, want, got)
		}
	}
	var migrations int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM schema_migrations WHERE version = $1`,
		"0028_workflow_step_results.sql").Scan(&migrations); err != nil {
		t.Fatalf("count migration: %v", err)
	}
	if migrations != 1 {
		t.Fatalf("0028 count %d", migrations)
	}

	stale, err := jobs.NewRepo(pool).FindStale(ctx, time.Second, time.Second)
	if err != nil {
		t.Fatalf("FindStale: %v", err)
	}
	var sawQueued, sawActive, sawDone bool
	for _, job := range stale {
		switch job.ID {
		case queuedID:
			sawQueued = true
		case activeID:
			sawActive = true
		case doneID:
			sawDone = true
		}
	}
	if !sawQueued || sawActive || sawDone {
		t.Fatal("FindStale mixed the three jobs")
	}

	live := openHello(t)
	parkImageJobs(t, live.pool)
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d", status)
	}
	var payload struct {
		Assignments []map[string]json.RawMessage `json:"assignments"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		t.Fatal("decode pull")
	}
	for _, assignment := range payload.Assignments {
		got, err := jsonUUID(assignment["job_id"])
		if err != nil {
			t.Fatal("assignment job_id")
		}
		if got == queuedID {
			t.Fatal("pull returned the legacy job")
		}
	}
}

func TestRollbackStopsAdmissionBeforeDrain(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	ctx := context.Background()
	listener := startWorkerListener(t)
	manifest := imageLegacyManifest(t, "octoconv.convert.image", "png", "jpg")
	live := listener.hello(t, manifest)

	_, found, err := listener.repo.Lookup(ctx, convert.OperationConvert, "png", "jpg")
	if err != nil || !found {
		t.Fatal("lookup before delete")
	}
	tag, err := listener.pool.Exec(ctx, `
		DELETE FROM worker_capability_formats
		WHERE capability_id = $1 AND capability_version = $2`,
		"octoconv.convert.image", 1)
	if err != nil || tag.RowsAffected() < 1 {
		t.Fatal("delete formats")
	}
	_, found, err = listener.repo.Lookup(ctx, convert.OperationConvert, "png", "jpg")
	if err != nil || found {
		t.Fatal("lookup after delete")
	}
	var capabilities int
	if err := listener.pool.QueryRow(ctx, `
		SELECT count(*) FROM worker_capabilities
		WHERE worker_id = $1 AND id = $2 AND version = 1`,
		live.workerID, "octoconv.convert.image").Scan(&capabilities); err != nil || capabilities != 1 {
		t.Fatalf("capabilities %d", capabilities)
	}

	parkImageJobs(t, listener.pool)
	clientID := insertClient(t, listener.pool)
	jobID := uuid.New()
	t.Cleanup(func() {
		_, _ = listener.pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id = $1`, jobID)
	})
	if _, err := listener.pool.Exec(ctx, `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			step_id, created_at, options
		) VALUES (
			$1, $2, 'convert', 'image', 'queued', 'png', 'jpg',
			'protocol', 'octoconv.convert.image', 1, now() + interval '1 hour',
			$3, now(), '{"test":"protocol-pull"}'
		)`, jobID, clientID, uuid.New()); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := listener.pool.Exec(ctx, `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes)
		VALUES ($1, 0, $2, 'in.png', 'png', 4)`,
		jobID, storage.InputKey(jobID, 0, "in.png")); err != nil {
		t.Fatalf("insert input: %v", err)
	}

	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d", status)
	}
	var payload struct {
		Assignments []map[string]json.RawMessage `json:"assignments"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		t.Fatal("decode pull")
	}
	if len(payload.Assignments) != 1 {
		t.Fatalf("assignments %d", len(payload.Assignments))
	}
	got, err := jsonUUID(payload.Assignments[0]["job_id"])
	if err != nil || got != jobID {
		t.Fatalf("pulled %s", got)
	}

	if _, err := listener.repo.DrainWorker(ctx, live.workerID); err != nil {
		t.Fatalf("drain: %v", err)
	}
	status, resp = pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("drained pull status %d", status)
	}
	n, drain, _ := decodePullControl(t, resp)
	if n != 0 || !drain {
		t.Fatalf("drained pull assignments=%d drain=%v", n, drain)
	}
	var owner string
	if err := listener.pool.QueryRow(ctx, `SELECT dispatch_owner FROM jobs WHERE id = $1`, jobID).Scan(&owner); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	if owner != "protocol" {
		t.Fatalf("dispatch_owner %s", owner)
	}
}

type jobPin struct {
	status string
	owner  string
	output string
}

func jobPinOf(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) jobPin {
	t.Helper()
	var pin jobPin
	if err := pool.QueryRow(context.Background(),
		`SELECT status, dispatch_owner FROM jobs WHERE id = $1`, id,
	).Scan(&pin.status, &pin.owner); err != nil {
		t.Fatalf("read job: %v", err)
	}
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(object_key, '') FROM job_outputs WHERE job_id = $1`, id,
	).Scan(&pin.output)
	if errors.Is(err, pgx.ErrNoRows) {
		return pin
	}
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return pin
}

func insertRollbackJob(t *testing.T, pool *pgxpool.Pool, id, clientID uuid.UUID, status, owner, createdAt string) {
	t.Helper()
	var sql string
	switch createdAt {
	case "now() - interval '2 hours'":
		sql = `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			step_id, created_at, options
		) VALUES (
			$1, $2, 'convert', 'image', $3, 'png', 'jpg',
			$4, 'octoconv.convert.image', 1, now() + interval '1 hour',
			$5, now() - interval '2 hours', '{"test":"protocol-pull"}'
		)`
	case "now() - interval '1 hour'":
		sql = `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			step_id, created_at, options
		) VALUES (
			$1, $2, 'convert', 'image', $3, 'png', 'jpg',
			$4, 'octoconv.convert.image', 1, now() + interval '1 hour',
			$5, now() - interval '1 hour', '{"test":"protocol-pull"}'
		)`
	default:
		sql = `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			step_id, created_at, options
		) VALUES (
			$1, $2, 'convert', 'image', $3, 'png', 'jpg',
			$4, 'octoconv.convert.image', 1, now() + interval '1 hour',
			$5, now(), '{"test":"protocol-pull"}'
		)`
	}
	if _, err := pool.Exec(context.Background(), sql, id, clientID, status, owner, uuid.New()); err != nil {
		t.Fatalf("insert %s job: %v", status, err)
	}
}
