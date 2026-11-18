package workerproto

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/storage"
)

func TestMissedHeartbeatStaysOpen(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	live := openHello(t)
	ctx := context.Background()
	holdID := uuid.New()
	var lease time.Time
	if err := live.pool.QueryRow(ctx, `
		INSERT INTO worker_slot_holds (id, owner_key, session_id, lease_expires_at, state)
		VALUES ($1, $2, $3, clock_timestamp() + interval '2 minutes', 'active')
		RETURNING lease_expires_at`, holdID, live.workerID.String(), live.sessionID).Scan(&lease); err != nil {
		t.Fatalf("insert hold: %v", err)
	}
	var beat time.Time
	if err := live.pool.QueryRow(ctx, `
		UPDATE worker_sessions
		SET last_heartbeat_at = clock_timestamp() - interval '31 seconds'
		WHERE id = $1
		RETURNING last_heartbeat_at`, live.sessionID).Scan(&beat); err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}
	status, _, _ := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/heartbeat", "abab", []byte(`{"protocol_version":2}`), nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("bad heartbeat status %d", status)
	}
	var afterFail time.Time
	if err := live.pool.QueryRow(ctx, `SELECT last_heartbeat_at FROM worker_sessions WHERE id = $1`, live.sessionID).Scan(&afterFail); err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if !afterFail.Equal(beat) {
		t.Fatal("failed auth wrote last_heartbeat_at")
	}

	workers, err := NewRepo(live.pool).ListWorkers(ctx)
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	session, ok := findSession(workers, live.sessionID)
	if !ok {
		t.Fatal("open session missing from the operator list")
	}
	if session.State != "offline" {
		t.Fatalf("state %s", session.State)
	}
	if session.LastHeartbeatAt == nil || !session.LastHeartbeatAt.Equal(beat) {
		t.Fatalf("last_heartbeat_at %v", session.LastHeartbeatAt)
	}
	if session.ActiveAttempts != 1 {
		t.Fatalf("active attempts %d", session.ActiveAttempts)
	}
	worker := findWorker(workers, live.workerID)
	if worker.CapabilityID == nil || *worker.CapabilityID != "octoconv.convert.image" || worker.CapabilityVersion == nil || *worker.CapabilityVersion != 1 {
		t.Fatalf("capability %+v %+v", worker.CapabilityID, worker.CapabilityVersion)
	}

	var closedAt *time.Time
	if err := live.pool.QueryRow(ctx, `SELECT closed_at FROM worker_sessions WHERE id = $1`, live.sessionID).Scan(&closedAt); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if closedAt != nil {
		t.Fatal("missed heartbeats closed the session")
	}
	var leaseAfter time.Time
	if err := live.pool.QueryRow(ctx, `SELECT lease_expires_at FROM worker_slot_holds WHERE id = $1`, holdID).Scan(&leaseAfter); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if !leaseAfter.Equal(lease) {
		t.Fatal("missed heartbeats moved the lease")
	}
	var caps int
	if err := live.pool.QueryRow(ctx, `SELECT count(*) FROM worker_capabilities WHERE worker_id = $1`, live.workerID).Scan(&caps); err != nil {
		t.Fatalf("read capability: %v", err)
	}
	if caps != 1 {
		t.Fatalf("capabilities %d", caps)
	}

	status, _, body := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/heartbeat", live.token, []byte(`{"protocol_version":2}`), nil)
	if status != http.StatusOK {
		t.Fatalf("heartbeat status %d", status)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	assertEmptyCancelList(t, payload["control"])
	if err := live.pool.QueryRow(ctx, `SELECT closed_at FROM worker_sessions WHERE id = $1`, live.sessionID).Scan(&closedAt); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if closedAt != nil {
		t.Fatal("heartbeat closed the session")
	}
	if err := live.pool.QueryRow(ctx, `SELECT lease_expires_at FROM worker_slot_holds WHERE id = $1`, holdID).Scan(&leaseAfter); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if !leaseAfter.Equal(lease) {
		t.Fatal("heartbeat moved the lease")
	}
}

func TestDrainAssignsNothing(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	live := openHello(t)
	ctx := context.Background()
	jobID := insertProtocolImageJob(t, live.pool)
	repo := NewRepo(live.pool)
	if _, err := repo.DrainWorker(ctx, live.workerID); err != nil {
		t.Fatalf("drain: %v", err)
	}
	var stamped time.Time
	if err := live.pool.QueryRow(ctx, `SELECT drain_at FROM worker_sessions WHERE id = $1`, live.sessionID).Scan(&stamped); err != nil {
		t.Fatalf("read drain: %v", err)
	}
	if _, err := repo.DrainWorker(ctx, live.workerID); err != nil {
		t.Fatalf("drain again: %v", err)
	}
	var again time.Time
	if err := live.pool.QueryRow(ctx, `SELECT drain_at FROM worker_sessions WHERE id = $1`, live.sessionID).Scan(&again); err != nil {
		t.Fatalf("read drain: %v", err)
	}
	if !again.Equal(stamped) {
		t.Fatal("second drain moved drain_at")
	}
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d", status)
	}
	n, drain, terminate := decodePullControl(t, resp)
	if n != 0 || !drain || terminate {
		t.Fatalf("assignments %d drain %v terminate %v", n, drain, terminate)
	}
	if countHolds(t, live.pool, jobID) != 0 {
		t.Fatal("drain inserted a hold")
	}
}

func TestDrainAllowsInFlightComplete(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	live := openHello(t)
	clientID := insertClient(t, live.pool)
	jobID := insertPullJob(t, live.pool, pullJob{clientID: &clientID})
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d", status)
	}
	assignment := decodeOneAssignment(t, resp)
	attemptID, err := jsonUUID(assignment["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err := jsonString(assignment["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.pool.Exec(context.Background(), `
		UPDATE worker_sessions SET drain_at = clock_timestamp() WHERE id = $1`, live.sessionID); err != nil {
		t.Fatalf("stamp drain: %v", err)
	}
	payload := []byte("primary-bytes")
	live.store.put(storage.AttemptOutputKey(attemptID, 0), payload)
	status, _, resp = postAttempt(t, live, attemptPaths(live.addr, attemptID)[3], token, completeBody(t, "jpg", "image/jpeg", payload))
	if status != http.StatusOK {
		t.Fatalf("complete status %d: %s", status, resp)
	}
	_, drain, terminate := decodeControlBody(t, resp)
	if !drain || terminate {
		t.Fatalf("complete control drain %v terminate %v", drain, terminate)
	}
	var jobStatus string
	var code *string
	if err := live.pool.QueryRow(context.Background(), `SELECT status, error_code FROM jobs WHERE id = $1`, jobID).Scan(&jobStatus, &code); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if jobStatus != "done" || code != nil {
		t.Fatalf("job %s code %v", jobStatus, code)
	}
}

func TestDrainReplacementSessionCanClaim(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	live := openHello(t)
	ctx := context.Background()
	if _, err := live.pool.Exec(ctx, `
		UPDATE worker_sessions
		SET last_heartbeat_at = clock_timestamp() - interval '31 seconds'
		WHERE id = $1`, live.sessionID); err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}
	repo := NewRepo(live.pool)
	if _, err := repo.DrainWorker(ctx, live.workerID); err != nil {
		t.Fatalf("drain: %v", err)
	}
	jobID := insertProtocolImageJob(t, live.pool)
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("drained pull status %d", status)
	}
	if n, drain, terminate := decodePullControl(t, resp); n != 0 || !drain || terminate {
		t.Fatalf("drained pull assignments %d drain %v terminate %v", n, drain, terminate)
	}

	next := postHello(t, live)
	var copied *time.Time
	if err := live.pool.QueryRow(ctx, `SELECT drain_at FROM worker_sessions WHERE id = $1`, next.sessionID).Scan(&copied); err != nil {
		t.Fatalf("read replacement drain: %v", err)
	}
	if copied != nil {
		t.Fatal("replacement session copied drain_at")
	}
	status, _, beatBody := doJSON(t, next.client, http.MethodPost, next.sessionURL()+"/heartbeat", next.token, []byte(`{"protocol_version":2}`), nil)
	if status != http.StatusOK {
		t.Fatalf("heartbeat status %d", status)
	}
	if _, drain, terminate := decodeControlBody(t, beatBody); drain || terminate {
		t.Fatalf("fresh heartbeat drain %v terminate %v", drain, terminate)
	}
	workers, err := repo.ListWorkers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	drained, ok := findSession(workers, live.sessionID)
	if !ok || drained.State != "draining" {
		t.Fatalf("stamped session %+v", drained)
	}
	fresh, ok := findSession(workers, next.sessionID)
	if !ok || fresh.State != "ready" {
		t.Fatalf("replacement session %+v", fresh)
	}

	status, resp = pullOnce(t, next, 1)
	if status != http.StatusOK {
		t.Fatalf("replacement pull status %d", status)
	}
	if n, drain, terminate := decodePullControl(t, resp); n != 1 || drain || terminate {
		t.Fatalf("replacement pull assignments %d drain %v terminate %v", n, drain, terminate)
	}
	if countHolds(t, live.pool, jobID) != 1 {
		t.Fatal("replacement pull inserted no hold")
	}

	status, _, _ = doJSON(t, live.client, http.MethodDelete, live.sessionURL(), live.token, []byte(`{"protocol_version":2}`), nil)
	if status != http.StatusOK {
		t.Fatalf("delete status %d", status)
	}
	third := postHello(t, live)
	if err := live.pool.QueryRow(ctx, `SELECT drain_at FROM worker_sessions WHERE id = $1`, third.sessionID).Scan(&copied); err != nil {
		t.Fatalf("read third drain: %v", err)
	}
	if copied != nil {
		t.Fatal("session opened after delete copied drain_at")
	}
	jobID = insertProtocolImageJob(t, live.pool)
	status, resp = pullOnce(t, third, 1)
	if status != http.StatusOK {
		t.Fatalf("third pull status %d", status)
	}
	if n, drain, terminate := decodePullControl(t, resp); n != 1 || drain || terminate {
		t.Fatalf("third pull assignments %d drain %v terminate %v", n, drain, terminate)
	}
	if countHolds(t, live.pool, jobID) != 1 {
		t.Fatal("third pull inserted no hold")
	}
}

func postHello(t *testing.T, live liveSession) liveSession {
	t.Helper()
	status, _, resp := doJSON(t, live.client, http.MethodPost, "https://"+live.addr+"/worker/v1/sessions", "", helloBody(live.workerID, manifestDigest(live.manifest), 1), nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d", status)
	}
	var hello struct {
		SessionID    string `json:"session_id"`
		SessionToken string `json:"session_token"`
		Control      struct {
			Drain     bool `json:"drain"`
			Terminate bool `json:"terminate"`
		} `json:"control"`
	}
	if err := json.Unmarshal(resp, &hello); err != nil {
		t.Fatalf("decode HELLO: %v", err)
	}
	if hello.Control.Drain || hello.Control.Terminate {
		t.Fatal("new HELLO carried drain or terminate")
	}
	sessionID, err := uuid.Parse(hello.SessionID)
	if err != nil {
		t.Fatal("session_id is not a uuid")
	}
	next := live
	next.sessionID = sessionID
	next.token = hello.SessionToken
	return next
}

func findWorker(workers []WorkerView, id uuid.UUID) WorkerView {
	for _, worker := range workers {
		if worker.WorkerID == id.String() {
			return worker
		}
	}
	return WorkerView{}
}

func findSession(workers []WorkerView, id uuid.UUID) (SessionView, bool) {
	for _, worker := range workers {
		for _, session := range worker.Sessions {
			if session.SessionID == id.String() {
				return session, true
			}
		}
	}
	return SessionView{}, false
}

func decodePullControl(t *testing.T, body []byte) (int, bool, bool) {
	t.Helper()
	var payload struct {
		Assignments []json.RawMessage `json:"assignments"`
		Control     struct {
			Drain     bool `json:"drain"`
			Terminate bool `json:"terminate"`
		} `json:"control"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	return len(payload.Assignments), payload.Control.Drain, payload.Control.Terminate
}

func decodeControlBody(t *testing.T, body []byte) (int, bool, bool) {
	t.Helper()
	var payload struct {
		Control struct {
			Drain     bool `json:"drain"`
			Terminate bool `json:"terminate"`
		} `json:"control"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode control: %v", err)
	}
	return 0, payload.Control.Drain, payload.Control.Terminate
}
