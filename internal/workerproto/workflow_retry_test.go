package workerproto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/jobs"
)

func TestWorkflowRetriesCurrentStep(t *testing.T) {
	restoreJitter(t)
	env := newChain(t, 4)
	payload := []byte("png!")
	first := oneAssignment(t, env.sessions[0])
	attemptID, _ := assignmentAttempt(t, first)
	acceptArtifact(t, env.sessions[0], first, "png", "image/png", payload)
	key := acceptedObjectKey(env.jobID, attemptID, payload)
	second := oneAssignment(t, env.sessions[1])
	gotStep, err := jsonUUID(second["step_id"])
	if err != nil || gotStep != env.steps[1] {
		t.Fatalf("step %s", second["step_id"])
	}
	assertInputKey(t, second, "png", key)
	failTimeout(t, env.sessions[1], second, "step timed out")
	job := readChainJob(t, env.pool, env.jobID)
	if job.status != "queued" || job.stepID != env.steps[1] || countResults(t, env.pool, env.jobID) != 1 {
		t.Fatalf("after retry %+v results %d", job, countResults(t, env.pool, env.jobID))
	}
	if usageBytes(t, env.pool, env.clientID) != 0 || openStepHolds(t, env.pool, env.steps[0]) != 0 {
		t.Fatal("retry charged usage or held the previous step")
	}
	again := oneAssignment(t, env.sessions[1])
	gotStep, err = jsonUUID(again["step_id"])
	if err != nil || gotStep != env.steps[1] {
		t.Fatalf("reissued step %s", again["step_id"])
	}
	againAttempt, err := jsonUUID(again["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	firstAttempt, err := jsonUUID(second["attempt_id"])
	if err != nil || againAttempt == firstAttempt {
		t.Fatal("retry reused the attempt")
	}
	assertInputKey(t, again, "png", key)
	if openStepHolds(t, env.pool, env.steps[0]) != 0 {
		t.Fatal("previous step gained a hold")
	}
}

func TestWorkflowMissingInputFails(t *testing.T) {
	env := newChain(t, 4)
	first := oneAssignment(t, env.sessions[0])
	acceptArtifact(t, env.sessions[0], first, "png", "image/png", []byte("png!"))
	if _, err := env.pool.Exec(context.Background(), `DELETE FROM workflow_step_results WHERE job_id = $1`, env.jobID); err != nil {
		t.Fatalf("delete result: %v", err)
	}
	emptyAssignment(t, env.sessions[1])
	var code, message string
	if err := env.pool.QueryRow(context.Background(), `SELECT error_code, error_message FROM jobs WHERE id = $1`, env.jobID).Scan(&code, &message); err != nil {
		t.Fatal(err)
	}
	if jobStatusOf(t, env.pool, env.jobID) != "failed" || code != "engine_error" || message != "workflow input missing" {
		t.Fatalf("status %s code %s message %s", jobStatusOf(t, env.pool, env.jobID), code, message)
	}
	if openStepHolds(t, env.pool, env.steps[1]) != 0 {
		t.Fatal("missing input opened a hold")
	}
}

func TestWorkflowFifthFailureExhausts(t *testing.T) {
	restoreJitter(t)
	env := newChain(t, 4)
	first := oneAssignment(t, env.sessions[0])
	acceptArtifact(t, env.sessions[0], first, "png", "image/png", []byte("png!"))
	second := oneAssignment(t, env.sessions[1])
	step := env.steps[1]
	jobID := env.jobID
	for range 4 {
		insertSlotHold(t, env.pool, slotHold{
			owner:     env.workers[1].String(),
			sessionID: env.sessions[1].sessionID,
			jobID:     &jobID,
			stepID:    &step,
			state:     "failed",
			released:  true,
		})
	}
	const message = "step timed out"
	failTimeout(t, env.sessions[1], second, message)
	var code, got string
	if err := env.pool.QueryRow(context.Background(), `SELECT error_code, error_message FROM jobs WHERE id = $1`, env.jobID).Scan(&code, &got); err != nil {
		t.Fatal(err)
	}
	if jobStatusOf(t, env.pool, env.jobID) != "failed" || code != "reconciler_exhausted" || got != message {
		t.Fatalf("status %s code %s message %s", jobStatusOf(t, env.pool, env.jobID), code, got)
	}
	if usageBytes(t, env.pool, env.clientID) != env.size {
		t.Fatalf("bytes %d", usageBytes(t, env.pool, env.clientID))
	}
}

func TestWorkflowSweepRemovesTerminalResults(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	pool := testPool(t)
	clientID := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO clients (id, name) VALUES ($1, $2)`, clientID, "sweep-"+clientID.String()); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID)
		_, _ = pool.Exec(context.Background(), `UPDATE jobs SET workflow_snapshot_id = NULL WHERE client_id = $1`, clientID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE client_id = $1`, clientID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM clients WHERE id = $1`, clientID)
	})
	doneID := insertSweepJob(t, pool, clientID, "done")
	queuedID := insertSweepJob(t, pool, clientID, "queued")
	doneKey := "results/" + doneID.String() + "/mid"
	queuedKey := "results/" + queuedID.String() + "/mid"
	insertSweepResult(t, pool, doneID, doneKey)
	insertSweepResult(t, pool, queuedID, queuedKey)
	repo := jobs.NewRepo(pool)
	if err := protocolPass(ctx, pool, repo, nil); err != nil {
		t.Fatalf("nil store: %v", err)
	}
	if countResults(t, pool, doneID) != 1 || countResults(t, pool, queuedID) != 1 {
		t.Fatal("nil store removed a row")
	}
	store := newMemStore()
	if err := protocolPass(ctx, pool, repo, store); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if countResults(t, pool, doneID) != 0 || countResults(t, pool, queuedID) != 1 || !keyRemoved(store, doneKey) {
		t.Fatalf("removed %v queued %d", store.removedKeys(), countResults(t, pool, queuedID))
	}
	store.resetRemoves()
	if err := protocolPass(ctx, pool, repo, store); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if keyRemoved(store, doneKey) {
		t.Fatal("second pass removed the key")
	}
	errID := insertSweepJob(t, pool, clientID, "done")
	errKey := "results/" + errID.String() + "/mid"
	insertSweepResult(t, pool, errID, errKey)
	store.setRemoveErr(errors.New("remove failed"))
	if err := protocolPass(ctx, pool, repo, store); err != nil {
		t.Fatalf("remove error: %v", err)
	}
	if countResults(t, pool, errID) != 1 || countResults(t, pool, queuedID) != 1 {
		t.Fatal("remove error deleted a row")
	}
	store.setRemoveErr(nil)
	if err := protocolPass(ctx, pool, repo, store); err != nil {
		t.Fatalf("retry sweep: %v", err)
	}
	if countResults(t, pool, errID) != 0 {
		t.Fatal("row stayed after remove succeeded")
	}
	store.resetRemoves()
	if err := protocolPass(ctx, pool, repo, store); err != nil {
		t.Fatalf("final sweep: %v", err)
	}
	if keyRemoved(store, errKey) || countResults(t, pool, queuedID) != 1 {
		t.Fatal("final pass removed a settled key")
	}
}

func TestWorkflowKeepsRetryableResult(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	pool := testPool(t)
	clientID := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO clients (id, name) VALUES ($1, $2)`, clientID, "retry-sweep-"+clientID.String()); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID)
		_, _ = pool.Exec(context.Background(), `UPDATE jobs SET workflow_snapshot_id = NULL WHERE client_id = $1`, clientID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE client_id = $1`, clientID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM clients WHERE id = $1`, clientID)
	})
	kept := insertSweepJob(t, pool, clientID, "failed")
	expired := insertSweepJob(t, pool, clientID, "failed")
	if _, err := pool.Exec(ctx, `
		UPDATE jobs SET protocol_deadline = now() + interval '1 hour' WHERE id = $1`, kept); err != nil {
		t.Fatalf("future deadline: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE jobs SET protocol_deadline = now() - interval '1 second' WHERE id = $1`, expired); err != nil {
		t.Fatalf("past deadline: %v", err)
	}
	keptKey := "results/" + kept.String() + "/mid"
	expiredKey := "results/" + expired.String() + "/mid"
	insertSweepResult(t, pool, kept, keptKey)
	insertSweepResult(t, pool, expired, expiredKey)
	store := newMemStore()
	if err := protocolPass(ctx, pool, jobs.NewRepo(pool), store); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if countResults(t, pool, kept) != 1 || keyRemoved(store, keptKey) || countResults(t, pool, expired) != 0 || !keyRemoved(store, expiredKey) {
		t.Fatalf("kept %d expired %d removed %v", countResults(t, pool, kept), countResults(t, pool, expired), store.removedKeys())
	}
	if _, err := pool.Exec(ctx, `
		UPDATE jobs SET protocol_deadline = now() - interval '1 second' WHERE id = $1`, kept); err != nil {
		t.Fatalf("expire kept: %v", err)
	}
	store.resetRemoves()
	if err := protocolPass(ctx, pool, jobs.NewRepo(pool), store); err != nil {
		t.Fatalf("expire sweep: %v", err)
	}
	if countResults(t, pool, kept) != 0 || !keyRemoved(store, keptKey) {
		t.Fatal("expired retryable result stayed")
	}
}

func TestWorkflowCancelStopsChain(t *testing.T) {
	env := newChain(t, 4)
	first := oneAssignment(t, env.sessions[0])
	attemptID, _ := assignmentAttempt(t, first)
	if err := env.jobs.Cancel(context.Background(), env.jobID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if jobStatusOf(t, env.pool, env.jobID) != "canceled" || holdState(t, env.pool, attemptID) != "canceled" || countResults(t, env.pool, env.jobID) != 0 {
		t.Fatal("cancel continued the chain")
	}
	assertLateComplete(t, env.sessions[0], first, http.StatusConflict, "attempt_not_active")
	assertNoAssignment(t, env.sessions[0], attemptID)
	if usageBytes(t, env.pool, env.clientID) != env.size {
		t.Fatalf("bytes %d", usageBytes(t, env.pool, env.clientID))
	}
	if err := env.jobs.Cancel(context.Background(), env.jobID); err == nil {
		t.Fatal("second cancel succeeded")
	}
	if usageBytes(t, env.pool, env.clientID) != env.size {
		t.Fatalf("second cancel bytes %d", usageBytes(t, env.pool, env.clientID))
	}
}

func TestWorkflowRevokeKeepsStep(t *testing.T) {
	env := newChain(t, 4)
	first := oneAssignment(t, env.sessions[0])
	attemptID, _ := assignmentAttempt(t, first)
	leaf := env.certs[0].Leaf
	if err := env.repo.RevokeIdentity(context.Background(), env.workers[0], leaf.SerialNumber, issuerSHA256(leaf)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	job := readChainJob(t, env.pool, env.jobID)
	if job.status != "queued" || job.stepID != env.steps[0] || countResults(t, env.pool, env.jobID) != 0 {
		t.Fatalf("revoke %+v results %d", job, countResults(t, env.pool, env.jobID))
	}
	assertLateComplete(t, env.sessions[0], first, http.StatusUnauthorized, "session_closed")
	h := NewHandler(env.repo, env.jobs, env.store, 120*time.Second)
	sum := sha256.Sum256([]byte(attemptID.String()))
	_, err := h.acceptComplete(context.Background(), storedSession{ID: env.sessions[0].sessionID, WorkerID: env.sessions[0].workerID}, attemptID, hex.EncodeToString(sum[:]), nil)
	var fail *completeFailure
	if !errors.As(err, &fail) || fail.code != "attempt_not_active" {
		t.Fatalf("accept %v", err)
	}
}

func TestWorkflowDeadlineStopsChain(t *testing.T) {
	env := newChain(t, 4)
	first := oneAssignment(t, env.sessions[0])
	if _, err := env.pool.Exec(context.Background(), `UPDATE jobs SET protocol_deadline = now() - interval '1 second' WHERE id = $1`, env.jobID); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := failPastDeadlines(context.Background(), env.pool, env.jobs); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	var code, message string
	if err := env.pool.QueryRow(context.Background(), `SELECT error_code, error_message FROM jobs WHERE id = $1`, env.jobID).Scan(&code, &message); err != nil {
		t.Fatal(err)
	}
	if jobStatusOf(t, env.pool, env.jobID) != "failed" || code != "reconciler_exhausted" || message != "protocol deadline passed" || countResults(t, env.pool, env.jobID) != 0 {
		t.Fatalf("status %s code %s message %s", jobStatusOf(t, env.pool, env.jobID), code, message)
	}
	assertLateComplete(t, env.sessions[0], first, http.StatusConflict, "attempt_not_active")
	emptyAssignment(t, env.sessions[0])
}

func insertSweepJob(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, options
		) VALUES (
			$1, $2, 'convert', 'image', $3, 'png', 'png',
			'protocol', '{}'
		)`, id, clientID, status); err != nil {
		t.Fatalf("insert sweep job: %v", err)
	}
	return id
}

func insertSweepResult(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, key string) {
	t.Helper()
	sum := sha256.Sum256([]byte(key))
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workflow_step_results (
			job_id, step_id, attempt_id, object_key, format, size_bytes, content_type, sha256
		) VALUES ($1, $2, $3, $4, 'png', 4, 'image/png', $5)`,
		jobID, uuid.New(), uuid.New(), key, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("insert sweep result: %v", err)
	}
}

func assertNoAssignment(t *testing.T, live liveSession, canceled uuid.UUID) {
	t.Helper()
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull %d %s", status, resp)
	}
	var payload struct {
		ProtocolVersion int               `json:"protocol_version"`
		Assignments     []json.RawMessage `json:"assignments"`
		Control         struct {
			CancelAttemptIDs []string `json:"cancel_attempt_ids"`
			Drain            bool     `json:"drain"`
			Terminate        bool     `json:"terminate"`
		} `json:"control"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ProtocolVersion != ProtocolVersion || len(payload.Assignments) != 0 || payload.Control.Drain || payload.Control.Terminate {
		t.Fatalf("pull %s", resp)
	}
	if len(payload.Control.CancelAttemptIDs) != 1 || payload.Control.CancelAttemptIDs[0] != canceled.String() {
		t.Fatalf("cancel list %v", payload.Control.CancelAttemptIDs)
	}
}

func assertLateComplete(t *testing.T, live liveSession, assignment map[string]json.RawMessage, wantStatus int, code string) {
	t.Helper()
	attemptID, token := assignmentAttempt(t, assignment)
	status, hdr, resp := postAttempt(t, live, attemptPaths(live.addr, attemptID)[3], token, completeBody(t, "png", "image/png", []byte("png!")))
	assertProtocolError(t, status, hdr, resp, wantStatus, code)
}

func keyRemoved(store *memStore, key string) bool {
	for _, got := range store.removedKeys() {
		if got == key {
			return true
		}
	}
	return false
}
