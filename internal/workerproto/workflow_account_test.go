package workerproto

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/storage"
)

func TestWorkflowChargesOnce(t *testing.T) {
	restoreJitter(t)
	env := newChain(t, 4)
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, `UPDATE clients SET enforcement_mode = 'enforce' WHERE id = $1`, env.clientID); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO client_limits (client_id, engine, max_daily_bytes)
		VALUES ($1, 'image', 7)`, env.clientID); err != nil {
		t.Fatalf("limit: %v", err)
	}
	err := pgx.BeginTxFunc(ctx, env.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		return env.jobs.AdmitLocked(ctx, tx, env.clientID, "image", env.size)
	})
	if !errors.Is(err, jobs.ErrDailyBytesLimit) {
		t.Fatalf("admit %v", err)
	}
	first := oneAssignment(t, env.sessions[0])
	acceptArtifact(t, env.sessions[0], first, "png", "image/png", []byte("png!"))
	if ledgerBytes(t, env) != 0 {
		t.Fatalf("step 1 charged %d", ledgerBytes(t, env))
	}
	second := oneAssignment(t, env.sessions[1])
	failTimeout(t, env.sessions[1], second, "step timed out")
	if ledgerBytes(t, env) != 0 || readChainJob(t, env.pool, env.jobID).stepID != env.steps[1] {
		t.Fatal("requeue charged or left the step")
	}
	again := oneAssignment(t, env.sessions[1])
	gotStep, err := jsonUUID(again["step_id"])
	if err != nil || gotStep != env.steps[1] {
		t.Fatalf("reissued %s", again["step_id"])
	}
	payload := []byte("jpg!")
	stored := acceptArtifact(t, env.sessions[1], again, "jpg", "image/jpeg", payload)
	replayArtifact(t, env.sessions[1], again, "jpg", "image/jpeg", payload, stored)
	rows, bytesIn := ledgerUsage(t, env)
	if rows != 1 || bytesIn != env.size {
		t.Fatalf("ledger rows %d bytes %d", rows, bytesIn)
	}
	job := readChainJob(t, env.pool, env.jobID)
	progress := jobProgress(t, env.pool, env.jobID)
	if job.status != "done" || progress == nil || *progress != 100 || primaryOutputs(t, env, env.jobID) != 1 {
		t.Fatalf("done %+v progress %v", job, progress)
	}
}

func TestWorkflowProgressStaysNull(t *testing.T) {
	env := newChain(t, 4)
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, `UPDATE jobs SET created_at = now() - interval '2 hours' WHERE id = $1`, env.jobID); err != nil {
		t.Fatalf("age chain: %v", err)
	}
	plainID := uuid.New()
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline, options
		) VALUES (
			$1, $2, 'convert', 'image', 'queued', 'png', 'png',
			'protocol', $3, 1, now() + interval '10 minutes', '{}'
		)`, plainID, env.clientID, env.caps[0]); err != nil {
		t.Fatalf("insert plain: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, 0, $2, 'in.png', 'png', 4, 'image/png')`,
		plainID, storage.InputKey(plainID, 0, "in.png")); err != nil {
		t.Fatalf("insert plain input: %v", err)
	}
	first := oneAssignment(t, env.sessions[0])
	got, err := jsonUUID(first["job_id"])
	if err != nil || got != env.jobID {
		t.Fatalf("first job %s", first["job_id"])
	}
	postPercent(t, env.sessions[0], first)
	if jobProgress(t, env.pool, env.jobID) != nil {
		t.Fatal("workflow progress was stored")
	}
	attemptID, _ := assignmentAttempt(t, first)
	hold, _ := holdProgress(t, env.pool, attemptID)
	if hold == nil || *hold != 40 {
		t.Fatalf("hold progress %v", hold)
	}
	second := oneAssignment(t, env.sessions[0])
	got, err = jsonUUID(second["job_id"])
	if err != nil || got != plainID {
		t.Fatalf("second job %s", second["job_id"])
	}
	postPercent(t, env.sessions[0], second)
	progress := jobProgress(t, env.pool, plainID)
	if progress == nil || *progress != 40 {
		t.Fatalf("plain progress %v", progress)
	}
}

func TestWorkflowTwoSessions(t *testing.T) {
	env := newChain(t, 4)
	payload := []byte("png!")
	first := oneAssignment(t, env.sessions[0])
	attemptID, _ := assignmentAttempt(t, first)
	assertOutputFormat(t, first, "png", attemptID)
	stored := acceptArtifact(t, env.sessions[0], first, "png", "image/png", payload)
	replayArtifact(t, env.sessions[0], first, "png", "image/png", payload, stored)
	if openStepHolds(t, env.pool, env.steps[1]) != 0 || countStepHolds(t, env.pool, env.steps[1]) != 0 {
		t.Fatal("replay opened the next step")
	}
	second := oneAssignment(t, env.sessions[1])
	assertInputKey(t, second, "png", acceptedObjectKey(env.jobID, attemptID, payload))
	nextAttempt, _ := assignmentAttempt(t, second)
	assertOutputFormat(t, second, "jpg", nextAttempt)
	acceptArtifact(t, env.sessions[1], second, "jpg", "image/jpeg", []byte("jpg!"))
	job := readChainJob(t, env.pool, env.jobID)
	progress := jobProgress(t, env.pool, env.jobID)
	if job.status != "done" || progress == nil || *progress != 100 || primaryOutputs(t, env, env.jobID) != 1 {
		t.Fatalf("done %+v progress %v", job, progress)
	}
	if countStepHolds(t, env.pool, env.steps[1]) != 1 {
		t.Fatalf("step holds %d", countStepHolds(t, env.pool, env.steps[1]))
	}
}

func postPercent(t *testing.T, live liveSession, assignment map[string]json.RawMessage) {
	t.Helper()
	attemptID, token := assignmentAttempt(t, assignment)
	status, _, resp := postAttempt(t, live, attemptPaths(live.addr, attemptID)[1], token, []byte(`{"protocol_version":1,"percent":40}`))
	if status != http.StatusOK {
		t.Fatalf("progress %d %s", status, resp)
	}
}

func ledgerBytes(t *testing.T, env chainEnv) int64 {
	t.Helper()
	_, n := ledgerUsage(t, env)
	return n
}

func ledgerUsage(t *testing.T, env chainEnv) (int, int64) {
	t.Helper()
	var rows int
	var bytesIn int64
	if err := env.pool.QueryRow(context.Background(), `
		SELECT count(*)::int, COALESCE(sum(bytes_in), 0)::bigint
		FROM client_usage_daily WHERE client_id = $1`, env.clientID).Scan(&rows, &bytesIn); err != nil {
		t.Fatalf("read client_usage_daily: %v", err)
	}
	return rows, bytesIn
}

func primaryOutputs(t *testing.T, env chainEnv, jobID uuid.UUID) int {
	t.Helper()
	var n int
	if err := env.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM job_outputs WHERE job_id = $1 AND role = 'primary'`, jobID).Scan(&n); err != nil {
		t.Fatalf("count primary outputs: %v", err)
	}
	return n
}

func countStepHolds(t *testing.T, pool *pgxpool.Pool, stepID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM worker_slot_holds WHERE step_id = $1`, stepID).Scan(&n); err != nil {
		t.Fatalf("count step holds: %v", err)
	}
	return n
}
