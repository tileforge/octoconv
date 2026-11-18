package workerproto

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/storage"
)

type chainEnv struct {
	pool     *pgxpool.Pool
	repo     *Repo
	jobs     *jobs.Repo
	store    *memStore
	addr     string
	clientID uuid.UUID
	jobID    uuid.UUID
	steps    [2]uuid.UUID
	caps     [2]string
	workers  [2]uuid.UUID
	sessions [2]liveSession
	certs    [2]tls.Certificate
	size     int64
}

func newChain(t *testing.T, size int64) chainEnv {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	pool := testPool(t)
	workerA, capA := insertImageCapability(t, pool, "jpg", "png")
	workerB, capB := insertImageCapability(t, pool, "png", "jpg")
	clientID := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO clients (id, name) VALUES ($1, $2)`, clientID, "chain-"+clientID.String()); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	env := chainEnv{
		pool:     pool,
		clientID: clientID,
		workers:  [2]uuid.UUID{workerA, workerB},
		caps:     [2]string{capA, capB},
		size:     size,
	}
	t.Cleanup(func() { cleanupChain(env) })
	env.jobID = uuid.New()
	env.steps = [2]uuid.UUID{uuid.New(), uuid.New()}
	raw, err := json.Marshal(workflowSnapDoc{Steps: []workflowSnapStep{
		{StepID: env.steps[0], CapabilityID: capA, CapabilityVersion: 1, SourceFormat: "jpg", TargetFormat: "png", LegacyEngine: "image"},
		{StepID: env.steps[1], CapabilityID: capB, CapabilityVersion: 1, SourceFormat: "png", TargetFormat: "jpg", LegacyEngine: "image"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			step_id, admitted_input_bytes, options
		) VALUES (
			$1, $2, 'convert', 'image', 'queued', 'jpg', 'jpg',
			'protocol', $3, 1, now() + interval '10 minutes',
			$4, $5, '{}'
		)`, env.jobID, clientID, capA, env.steps[0], size); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_snapshots (job_id, definition_name, definition_version, document)
		VALUES ($1, 'chain', 1, $2::jsonb)`, env.jobID, raw); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET workflow_snapshot_id = id WHERE id = $1`, env.jobID); err != nil {
		t.Fatalf("attach snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, 0, $2, 'in.jpg', 'jpg', $3, 'image/jpeg')`,
		env.jobID, storage.InputKey(env.jobID, 0, "in.jpg"), size); err != nil {
		t.Fatalf("insert input: %v", err)
	}
	ca, caKey := mintTestCA(t)
	env.repo = NewRepo(pool)
	env.jobs = jobs.NewRepo(pool)
	env.addr = listenWorker(t, env.repo, ca, caKey)
	env.store = listenerStore(env.addr)
	env.sessions[0], env.certs[0] = helloCapability(t, pool, env.addr, ca, caKey, workerA)
	env.sessions[1], env.certs[1] = helloCapability(t, pool, env.addr, ca, caKey, workerB)
	return env
}

func insertImageCapability(t *testing.T, pool *pgxpool.Pool, from, to string) (uuid.UUID, string) {
	t.Helper()
	workerID := uuid.New()
	hexID := strings.ReplaceAll(workerID.String(), "-", "")
	capID := "octoconv.test.a" + hexID
	op := "op." + hexID
	manifest := []byte("chain-" + workerID.String())
	sum := sha256.Sum256(manifest)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_identities (worker_id, manifest_sha256, manifest_bytes)
		VALUES ($1, $2, $3)`, workerID, hex.EncodeToString(sum[:]), manifest); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capabilities (
			id, version, worker_id, client_operation, legacy_operation, legacy_engine, parameter_schema
		) VALUES ($1, 1, $2, $3, 'convert', 'image', '{}')`, capID, workerID, op); err != nil {
		t.Fatalf("insert capability: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capability_formats (
			capability_id, capability_version, source_format, target_format, client_operation
		) VALUES ($1, 1, $2, $3, $4)`, capID, from, to, op); err != nil {
		t.Fatalf("insert format: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capability_ports (
			capability_id, capability_version, direction, role, min_count, max_count, ordinal
		) VALUES
			($1, 1, 'input', 'source', 1, 1, 0),
			($1, 1, 'output', 'primary', 1, 1, 0)`, capID); err != nil {
		t.Fatalf("insert ports: %v", err)
	}
	return workerID, capID
}

func helloCapability(t *testing.T, pool *pgxpool.Pool, addr string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, workerID uuid.UUID) (liveSession, tls.Certificate) {
	t.Helper()
	var digest string
	if err := pool.QueryRow(context.Background(), `
		SELECT manifest_sha256 FROM worker_identities WHERE worker_id = $1`, workerID).Scan(&digest); err != nil {
		t.Fatalf("read identity: %v", err)
	}
	cert := mintClientCert(t, ca, caKey, workerID)
	client := workerHTTPClient(t, ca, cert)
	body := mustJSON(t, map[string]any{
		"protocol_version": 2,
		"worker_id":        workerID.String(),
		"manifest_sha256":  digest,
		"free_slots":       1,
	})
	status, _, respBody := doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", body, nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d %s", status, respBody)
	}
	var hello struct {
		SessionID    string `json:"session_id"`
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(respBody, &hello); err != nil {
		t.Fatalf("decode HELLO: %v", err)
	}
	sessionID, err := uuid.Parse(hello.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return liveSession{
		pool:      pool,
		workerID:  workerID,
		sessionID: sessionID,
		token:     hello.SessionToken,
		addr:      addr,
		client:    client,
		store:     listenerStore(addr),
	}, cert
}

func cleanupChain(env chainEnv) {
	ctx := context.Background()
	pool := env.pool
	if env.clientID != uuid.Nil {
		_, _ = pool.Exec(ctx, `DELETE FROM worker_slot_holds WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, env.clientID)
		_, _ = pool.Exec(ctx, `UPDATE jobs SET workflow_snapshot_id = NULL WHERE client_id = $1`, env.clientID)
		_, _ = pool.Exec(ctx, `DELETE FROM jobs WHERE client_id = $1`, env.clientID)
		_, _ = pool.Exec(ctx, `DELETE FROM client_usage_daily WHERE client_id = $1`, env.clientID)
		_, _ = pool.Exec(ctx, `DELETE FROM client_limits WHERE client_id = $1`, env.clientID)
		_, _ = pool.Exec(ctx, `DELETE FROM clients WHERE id = $1`, env.clientID)
	}
	for _, workerID := range env.workers {
		if workerID == uuid.Nil {
			continue
		}
		_, _ = pool.Exec(ctx, `DELETE FROM worker_request_dedup WHERE session_id IN (SELECT id FROM worker_sessions WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_attempt_credentials WHERE session_id IN (SELECT id FROM worker_sessions WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_slot_holds WHERE session_id IN (SELECT id FROM worker_sessions WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_sessions WHERE worker_id = $1`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_ports WHERE capability_id IN (SELECT id FROM worker_capabilities WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_formats WHERE capability_id IN (SELECT id FROM worker_capabilities WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capabilities WHERE worker_id = $1`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_certificate_denylist WHERE worker_id = $1`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_identities WHERE worker_id = $1`, workerID)
	}
}

type chainJob struct {
	status   string
	stepID   uuid.UUID
	source   string
	target   string
	capID    string
	engine   string
	admitted int64
	deadline time.Time
}

func readChainJob(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) chainJob {
	t.Helper()
	var row chainJob
	err := pool.QueryRow(context.Background(), `
		SELECT status, step_id, source_format, target_format, capability_id, engine,
		       admitted_input_bytes, protocol_deadline
		FROM jobs WHERE id = $1`, id).Scan(
		&row.status, &row.stepID, &row.source, &row.target, &row.capID, &row.engine, &row.admitted, &row.deadline)
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	return row
}

func countResults(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM workflow_step_results WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("count results: %v", err)
	}
	return n
}

func openStepHolds(t *testing.T, pool *pgxpool.Pool, stepID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_slot_holds WHERE step_id = $1 AND released_at IS NULL`, stepID).Scan(&n); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	return n
}

func usageBytes(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(sum(bytes_in), 0)::bigint FROM client_usage_daily WHERE client_id = $1`, clientID).Scan(&n); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	return n
}

func oneAssignment(t *testing.T, live liveSession) map[string]json.RawMessage {
	t.Helper()
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull %d %s", status, resp)
	}
	return decodeOneAssignment(t, resp)
}

func emptyAssignment(t *testing.T, live liveSession) {
	t.Helper()
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull %d %s", status, resp)
	}
	assertEmptyPull(t, resp)
}

func assignmentAttempt(t *testing.T, assignment map[string]json.RawMessage) (uuid.UUID, string) {
	t.Helper()
	attemptID, err := jsonUUID(assignment["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err := jsonString(assignment["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	return attemptID, token
}

func acceptArtifact(t *testing.T, live liveSession, assignment map[string]json.RawMessage, format, contentType string, payload []byte) []byte {
	t.Helper()
	attemptID, token := assignmentAttempt(t, assignment)
	live.store.put(storage.AttemptOutputKey(attemptID, 0), payload)
	status, _, resp := postAttempt(t, live, attemptPaths(live.addr, attemptID)[3], token, completeBody(t, format, contentType, payload))
	if status != http.StatusOK {
		t.Fatalf("complete %d %s", status, resp)
	}
	assertHeartbeatShape(t, resp)
	return resp
}

func replayArtifact(t *testing.T, live liveSession, assignment map[string]json.RawMessage, format, contentType string, payload, want []byte) {
	t.Helper()
	attemptID, token := assignmentAttempt(t, assignment)
	status, _, resp := postAttempt(t, live, attemptPaths(live.addr, attemptID)[3], token, completeBody(t, format, contentType, payload))
	if status != http.StatusOK || string(resp) != string(want) {
		t.Fatalf("replay %d %s", status, resp)
	}
}

func failTimeout(t *testing.T, live liveSession, assignment map[string]json.RawMessage, message string) {
	t.Helper()
	attemptID, token := assignmentAttempt(t, assignment)
	body := mustJSON(t, map[string]any{
		"protocol_version": 2,
		"engine_code":      "timeout",
		"message":          message,
	})
	status, _, resp := postAttempt(t, live, attemptPaths(live.addr, attemptID)[2], token, body)
	if status != http.StatusOK {
		t.Fatalf("fail %d %s", status, resp)
	}
}

func assertInputKey(t *testing.T, assignment map[string]json.RawMessage, format, key string) {
	t.Helper()
	var inputs []fileLink
	if err := json.Unmarshal(assignment["inputs"], &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("inputs %d", len(inputs))
	}
	assertFileLink(t, inputs[0], "source", format, "GET", key)
}

func assertOutputFormat(t *testing.T, assignment map[string]json.RawMessage, format string, attemptID uuid.UUID) {
	t.Helper()
	var outputs []fileLink
	if err := json.Unmarshal(assignment["outputs"], &outputs); err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 {
		t.Fatalf("outputs %d", len(outputs))
	}
	assertFileLink(t, outputs[0], "primary", format, "PUT", storage.AttemptOutputKey(attemptID, 0))
}

func acceptedObjectKey(jobID, attemptID uuid.UUID, payload []byte) string {
	sum := sha256.Sum256(payload)
	return storage.AcceptedOutputKey(jobID, attemptID, 0, hex.EncodeToString(sum[:]))
}

func serveOne(t *testing.T, h *Handler, live liveSession) map[string]json.RawMessage {
	t.Helper()
	body := servePullBody(t, h, live)
	return decodeOneAssignment(t, body)
}

func serveNone(t *testing.T, h *Handler, live liveSession) {
	t.Helper()
	assertEmptyPull(t, servePullBody(t, h, live))
}

func servePullBody(t *testing.T, h *Handler, live liveSession) []byte {
	t.Helper()
	pullID := uuid.New()
	sum := sha256.Sum256([]byte(pullID.String()))
	body, err := h.servePull(context.Background(), storedSession{ID: live.sessionID, WorkerID: live.workerID}, pullID, hex.EncodeToString(sum[:]), 1, 0)
	if err != nil {
		t.Fatalf("servePull: %v", err)
	}
	return body
}

func TestWorkflowClaimsFirstStep(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	live := openHello(t)
	parkImageJobs(t, live.pool)
	ctx := context.Background()
	nullID := uuid.New()
	steppedID := uuid.New()
	plainID := uuid.New()
	insertClaimJob(t, live, nullID, time.Now().Add(-3*time.Hour))
	if _, err := live.pool.Exec(ctx, `
		INSERT INTO workflow_snapshots (job_id, definition_name, definition_version, document)
		VALUES ($1, 'claim-null', 1, '{"steps":[]}'::jsonb)`, nullID); err != nil {
		t.Fatalf("insert null snapshot: %v", err)
	}
	if _, err := live.pool.Exec(ctx, `UPDATE jobs SET workflow_snapshot_id = id WHERE id = $1`, nullID); err != nil {
		t.Fatalf("attach null snapshot: %v", err)
	}
	insertClaimJob(t, live, steppedID, time.Now().Add(-2*time.Hour))
	stepID := uuid.New()
	raw, err := json.Marshal(workflowSnapDoc{Steps: []workflowSnapStep{{
		StepID: stepID, CapabilityID: "octoconv.convert.image", CapabilityVersion: 1,
		SourceFormat: "png", TargetFormat: "png", LegacyEngine: "image",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.pool.Exec(ctx, `
		INSERT INTO workflow_snapshots (job_id, definition_name, definition_version, document)
		VALUES ($1, 'claim-step', 1, $2::jsonb)`, steppedID, raw); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if _, err := live.pool.Exec(ctx, `
		UPDATE jobs SET workflow_snapshot_id = id, step_id = $2 WHERE id = $1`, steppedID, stepID); err != nil {
		t.Fatalf("attach snapshot: %v", err)
	}
	insertClaimJob(t, live, plainID, time.Now().Add(-time.Hour))
	t.Cleanup(func() {
		ids := []uuid.UUID{nullID, steppedID, plainID}
		_, _ = live.pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id = ANY($1)`, ids)
		_, _ = live.pool.Exec(context.Background(), `UPDATE jobs SET workflow_snapshot_id = NULL WHERE id = ANY($1)`, ids)
		_, _ = live.pool.Exec(context.Background(), `DELETE FROM workflow_snapshots WHERE job_id = ANY($1)`, ids)
		_, _ = live.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = ANY($1)`, ids)
	})

	nullPending, _, _, err := imageBacklog(ctx, live.pool, &nullID)
	if err != nil {
		t.Fatal(err)
	}
	stepPending, _, _, err := imageBacklog(ctx, live.pool, &steppedID)
	if err != nil {
		t.Fatal(err)
	}
	if nullPending != 0 || stepPending != 1 {
		t.Fatalf("backlog null %d stepped %d", nullPending, stepPending)
	}
	assignment := oneAssignment(t, live)
	got, err := jsonUUID(assignment["job_id"])
	if err != nil {
		t.Fatal(err)
	}
	if got != steppedID {
		t.Fatalf("assigned %s", got)
	}
	gotStep, err := jsonUUID(assignment["step_id"])
	if err != nil || gotStep != stepID {
		t.Fatalf("step %s", assignment["step_id"])
	}
	attemptID, err := jsonUUID(assignment["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	assertInputKey(t, assignment, "png", storage.InputKey(steppedID, 0, "in.png"))
	assertOutputFormat(t, assignment, "png", attemptID)
	var target string
	if err := live.pool.QueryRow(ctx, `SELECT target_format FROM jobs WHERE id = $1`, steppedID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if target != "jpg" {
		t.Fatalf("target %s", target)
	}
	if jobStatusOf(t, live.pool, nullID) != "queued" || jobStatusOf(t, live.pool, plainID) != "queued" {
		t.Fatal("another job was claimed")
	}
	var attempts int
	if err := live.pool.QueryRow(ctx, `SELECT attempts FROM jobs WHERE id = $1`, nullID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || countHolds(t, live.pool, nullID) != 0 {
		t.Fatal("null step was claimed")
	}
}

func parkImageJobs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
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
	if len(held) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(held))
	for _, row := range held {
		ids = append(ids, row.id)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE jobs SET not_before = clock_timestamp() + interval '1 day'
		WHERE id = ANY($1::uuid[])`, ids); err != nil {
		t.Fatalf("park jobs: %v", err)
	}
	t.Cleanup(func() {
		for _, row := range held {
			_, _ = pool.Exec(context.Background(), `UPDATE jobs SET not_before = $2 WHERE id = $1`, row.id, row.notBefore)
		}
	})
}

func TestWorkflowRecordsOneContinuation(t *testing.T) {
	env := newChain(t, 4)
	payload := []byte("png!")
	before := readChainJob(t, env.pool, env.jobID)
	first := oneAssignment(t, env.sessions[0])
	gotJob, err := jsonUUID(first["job_id"])
	if err != nil || gotJob != env.jobID {
		t.Fatalf("job %s", first["job_id"])
	}
	gotStep, err := jsonUUID(first["step_id"])
	if err != nil || gotStep != env.steps[0] {
		t.Fatalf("step %s", first["step_id"])
	}
	attemptID, err := jsonUUID(first["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	assertInputKey(t, first, "jpg", storage.InputKey(env.jobID, 0, "in.jpg"))
	assertOutputFormat(t, first, "png", attemptID)
	stored := acceptArtifact(t, env.sessions[0], first, "png", "image/png", payload)
	replayArtifact(t, env.sessions[0], first, "png", "image/png", payload, stored)
	after := readChainJob(t, env.pool, env.jobID)
	if after.status != "queued" || after.stepID != env.steps[1] || after.capID != env.caps[1] || after.engine != "image" {
		t.Fatalf("job %+v", after)
	}
	if after.source != "jpg" || after.target != "jpg" || after.admitted != before.admitted || !after.deadline.Equal(before.deadline) {
		t.Fatalf("shape changed %+v", after)
	}
	if jobProgress(t, env.pool, env.jobID) != nil || outputCount(t, env.pool, env.jobID) != 0 || countResults(t, env.pool, env.jobID) != 1 {
		t.Fatal("continuation wrote progress, an output, or a second result")
	}
	if openStepHolds(t, env.pool, env.steps[1]) != 0 {
		t.Fatal("replay opened the next step")
	}
	key := acceptedObjectKey(env.jobID, attemptID, payload)
	h := NewHandler(NewRepo(env.pool), jobs.NewRepo(env.pool), env.store, 120*time.Second)
	second := serveOne(t, h, env.sessions[1])
	gotStep, err = jsonUUID(second["step_id"])
	if err != nil || gotStep != env.steps[1] {
		t.Fatalf("next step %s", second["step_id"])
	}
	nextAttempt, err := jsonUUID(second["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	assertInputKey(t, second, "png", key)
	assertOutputFormat(t, second, "jpg", nextAttempt)
	serveNone(t, h, env.sessions[1])
	if openStepHolds(t, env.pool, env.steps[1]) != 1 || countResults(t, env.pool, env.jobID) != 1 {
		t.Fatal("second pull changed the chain")
	}
}

func TestWorkflowUsesStepEngineBudget(t *testing.T) {
	env := newChain(t, 4)
	ctx := context.Background()
	raw, err := json.Marshal(workflowSnapDoc{Steps: []workflowSnapStep{
		{StepID: env.steps[0], CapabilityID: env.caps[0], CapabilityVersion: 1, SourceFormat: "jpg", TargetFormat: "png", LegacyEngine: "document"},
		{StepID: env.steps[1], CapabilityID: env.caps[1], CapabilityVersion: 1, SourceFormat: "png", TargetFormat: "jpg", LegacyEngine: "image"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE jobs SET engine = 'document' WHERE id = $1`, env.jobID); err != nil {
		t.Fatalf("engine: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE workflow_snapshots SET document = $2::jsonb WHERE job_id = $1`, env.jobID, raw); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	h := NewHandler(env.repo, env.jobs, env.store, 120*time.Second)
	h.SetEngineBudgets(map[string]time.Duration{"document": 300 * time.Second})
	assignment := serveOne(t, h, env.sessions[0])
	deadline, err := jsonTime(assignment["attempt_deadline"])
	if err != nil {
		t.Fatal(err)
	}
	remain := time.Until(deadline)
	if remain < 250*time.Second || remain > 310*time.Second {
		t.Fatalf("attempt deadline in %s", remain)
	}
}
