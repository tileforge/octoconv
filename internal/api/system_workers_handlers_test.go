package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/workerproto"
)

func TestSystemWorkersOffline(t *testing.T) {
	pool := workerAdminPool(t)
	workerID, capabilityID := insertListedWorker(t, pool)
	offlineBeat := insertListedSession(t, pool, workerID, "5 seconds", "31 seconds")
	readyBeat := insertListedSession(t, pool, workerID, "5 seconds", "5 seconds")
	offlineOpened := insertListedSession(t, pool, workerID, "31 seconds", "")
	readyOpened := insertListedSession(t, pool, workerID, "5 seconds", "")
	closed := insertListedSession(t, pool, workerID, "5 seconds", "5 seconds")
	if _, err := pool.Exec(context.Background(), `UPDATE worker_sessions SET closed_at = clock_timestamp() WHERE id = $1`, closed.sessionID); err != nil {
		t.Fatalf("close session: %v", err)
	}
	var lease time.Time
	holdID := uuid.New()
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO worker_slot_holds (id, owner_key, session_id, lease_expires_at, released_at, state)
		VALUES ($1, $2, $3, clock_timestamp() + interval '2 minutes', NULL, 'active')
		RETURNING lease_expires_at`, holdID, workerID.String(), offlineBeat.sessionID).Scan(&lease); err != nil {
		t.Fatalf("insert hold: %v", err)
	}
	released := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO worker_slot_holds (id, owner_key, session_id, released_at, state)
		VALUES ($1, $2, $3, clock_timestamp(), 'stopped')`, released, workerID.String(), offlineBeat.sessionID); err != nil {
		t.Fatalf("insert released hold: %v", err)
	}

	srv := newWorkerAdminServer(pool)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/v1/system/workers", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "session_token") || strings.Contains(raw, "href") || strings.Contains(raw, "Authorization") {
		t.Fatal("operator JSON included a token or a link")
	}
	var body struct {
		Workers []workerproto.WorkerView `json:"workers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	worker, ok := listedWorker(body.Workers, workerID)
	if !ok {
		t.Fatal("worker missing")
	}
	if worker.CapabilityID == nil || *worker.CapabilityID != capabilityID || worker.CapabilityVersion == nil || *worker.CapabilityVersion != 1 {
		t.Fatalf("capability %+v", worker)
	}
	if worker.Sessions == nil {
		t.Fatal("sessions is null")
	}
	assertListed := func(id uuid.UUID, state string, beat *time.Time, attempts int) {
		t.Helper()
		session, found := listedSession(worker.Sessions, id)
		if !found {
			t.Fatalf("session %s missing", id)
		}
		if session.State != state {
			t.Fatalf("session %s state %s", id, session.State)
		}
		if beat == nil {
			if session.LastHeartbeatAt != nil {
				t.Fatalf("session %s heartbeat %v", id, session.LastHeartbeatAt)
			}
		} else if session.LastHeartbeatAt == nil || !session.LastHeartbeatAt.Equal(*beat) {
			t.Fatalf("session %s heartbeat %v", id, session.LastHeartbeatAt)
		}
		if session.ActiveAttempts != attempts {
			t.Fatalf("session %s attempts %d", id, session.ActiveAttempts)
		}
	}
	assertListed(offlineBeat.sessionID, "offline", offlineBeat.beat, 1)
	assertListed(readyBeat.sessionID, "ready", readyBeat.beat, 0)
	assertListed(offlineOpened.sessionID, "offline", nil, 0)
	assertListed(readyOpened.sessionID, "ready", nil, 0)
	if _, found := listedSession(worker.Sessions, closed.sessionID); found {
		t.Fatal("closed session is listed")
	}

	var closedAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT closed_at FROM worker_sessions WHERE id = $1`, offlineBeat.sessionID).Scan(&closedAt); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if closedAt != nil {
		t.Fatal("listing closed the session")
	}
	var leaseAfter time.Time
	if err := pool.QueryRow(context.Background(), `SELECT lease_expires_at FROM worker_slot_holds WHERE id = $1`, holdID).Scan(&leaseAfter); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if !leaseAfter.Equal(lease) {
		t.Fatal("listing moved the lease")
	}
	var caps int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM worker_capabilities WHERE worker_id = $1`, workerID).Scan(&caps); err != nil {
		t.Fatalf("read capability: %v", err)
	}
	if caps != 1 {
		t.Fatalf("capabilities %d", caps)
	}
}

func TestSystemWorkersRejectsNonOperator(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	srv := newOperatorGateTestServer(map[uuid.UUID]struct{}{uuid.New(): {}})
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/system/workers", nil),
		httptest.NewRequest(http.MethodPost, "/v1/system/workers/"+uuid.NewString()+"/drain", nil),
	} {
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, authed(req))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status %d", req.Method, req.URL.Path, rec.Code)
		}
		want := httptest.NewRecorder()
		writeError(want, http.StatusNotFound, errcode.UnknownPreset, noSuchPreset)
		if rec.Body.String() != want.Body.String() {
			t.Fatalf("body %s", rec.Body.String())
		}
	}
}

func TestSystemWorkersDrain(t *testing.T) {
	pool := workerAdminPool(t)
	ctx := context.Background()
	workerID, _ := insertListedWorker(t, pool)
	session := insertListedSession(t, pool, workerID, "5 seconds", "31 seconds")
	idleID, _ := insertListedWorker(t, pool)

	srv := newWorkerAdminServer(pool)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/v1/system/workers/"+workerID.String()+"/drain", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("drain status %d body %s", rec.Code, rec.Body.String())
	}
	var view workerproto.WorkerView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.WorkerID != workerID.String() || len(view.Sessions) != 1 || view.Sessions[0].State != "draining" {
		t.Fatalf("drain view %+v", view)
	}
	var stamped time.Time
	if err := pool.QueryRow(ctx, `SELECT drain_at FROM worker_sessions WHERE id = $1`, session.sessionID).Scan(&stamped); err != nil {
		t.Fatalf("read drain: %v", err)
	}
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/v1/system/workers/"+workerID.String()+"/drain", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("second drain status %d", rec.Code)
	}
	var again time.Time
	if err := pool.QueryRow(ctx, `SELECT drain_at FROM worker_sessions WHERE id = $1`, session.sessionID).Scan(&again); err != nil {
		t.Fatalf("read drain: %v", err)
	}
	if !again.Equal(stamped) {
		t.Fatal("second drain moved drain_at")
	}

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/v1/system/workers", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	var listed struct {
		Workers []workerproto.WorkerView `json:"workers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	worker, ok := listedWorker(listed.Workers, workerID)
	if !ok {
		t.Fatal("drained worker missing")
	}
	got, ok := listedSession(worker.Sessions, session.sessionID)
	if !ok || got.State != "draining" {
		t.Fatalf("listed %+v", got)
	}

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/v1/system/workers/"+idleID.String()+"/drain", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("idle drain status %d body %s", rec.Code, rec.Body.String())
	}
	var idle workerproto.WorkerView
	if err := json.Unmarshal(rec.Body.Bytes(), &idle); err != nil {
		t.Fatalf("decode idle: %v", err)
	}
	if idle.WorkerID != idleID.String() || idle.Sessions == nil || len(idle.Sessions) != 0 {
		t.Fatalf("idle view %+v", idle)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_sessions WHERE worker_id = $1`, idleID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("idle drain wrote %d sessions", sessions)
	}

	unknown := uuid.New()
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/v1/system/workers/"+unknown.String()+"/drain", nil)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown status %d", rec.Code)
	}
	var missing int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_identities WHERE worker_id = $1`, unknown).Scan(&missing); err != nil {
		t.Fatalf("read identity: %v", err)
	}
	if missing != 0 {
		t.Fatal("unknown drain created an identity")
	}

	otherID, _ := insertListedWorker(t, pool)
	other := insertListedSession(t, pool, otherID, "5 seconds", "5 seconds")
	denied := newOperatorGateTestServer(map[uuid.UUID]struct{}{uuid.New(): {}})
	rec = httptest.NewRecorder()
	denied.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/v1/system/workers/"+otherID.String()+"/drain", nil)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-operator status %d", rec.Code)
	}
	var drainAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT drain_at FROM worker_sessions WHERE id = $1`, other.sessionID).Scan(&drainAt); err != nil {
		t.Fatalf("read drain: %v", err)
	}
	if drainAt != nil {
		t.Fatal("non-operator wrote drain_at")
	}
}

func workerAdminPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
		t.Fatalf("db.Migrate: %v", err)
	}
	return pool
}

func newWorkerAdminServer(pool *pgxpool.Pool) *Server {
	resolver := newFakeResolver()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes:    1 << 20,
		OperatorClientIDs: map[uuid.UUID]struct{}{resolver.client.ID: {}},
	})
	srv.SetWorkerAdmin(workerproto.NewRepo(pool))
	return srv
}

func insertListedWorker(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, string) {
	t.Helper()
	workerID := uuid.New()
	sum := sha256.Sum256(workerID[:])
	digest := hex.EncodeToString(sum[:])
	capabilityID := "octoconv.test." + strings.ReplaceAll(workerID.String(), "-", "")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_identities (worker_id, manifest_sha256, manifest_bytes)
		VALUES ($1, $2, $3)`, workerID, digest, []byte("manifest")); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capabilities (
			id, version, worker_id, client_operation, legacy_operation, legacy_engine, parameter_schema
		) VALUES ($1, 1, $2, 'convert', 'convert', 'image', '{}')`, capabilityID, workerID); err != nil {
		t.Fatalf("insert capability: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE session_id IN (SELECT id FROM worker_sessions WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_attempt_credentials WHERE session_id IN (SELECT id FROM worker_sessions WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_request_dedup WHERE session_id IN (SELECT id FROM worker_sessions WHERE worker_id = $1)`, workerID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_sessions WHERE worker_id = $1`, workerID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_capabilities WHERE worker_id = $1`, workerID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_identities WHERE worker_id = $1`, workerID)
	})
	return workerID, capabilityID
}

type listedSessionRow struct {
	sessionID uuid.UUID
	beat      *time.Time
}

func insertListedSession(t *testing.T, pool *pgxpool.Pool, workerID uuid.UUID, opened, beat string) listedSessionRow {
	t.Helper()
	id := uuid.New()
	sum := sha256.Sum256(id[:])
	var row listedSessionRow
	err := pool.QueryRow(context.Background(), `
		INSERT INTO worker_sessions (
			id, worker_id, token_hash, cert_serial_decimal, issuer_sha256,
			free_slots, opened_at, expires_at, last_heartbeat_at
		) VALUES (
			$1, $2, $3, '1', $4, 1,
			clock_timestamp() - $5::interval,
			clock_timestamp() + interval '1 day',
			CASE WHEN $6::text IS NULL THEN NULL ELSE clock_timestamp() - $6::interval END
		)
		RETURNING id, last_heartbeat_at`,
		id, workerID, hex.EncodeToString(sum[:]), hex.EncodeToString(sum[:]), opened, nilIfEmpty(beat)).Scan(&row.sessionID, &row.beat)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return row
}

func nilIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func listedWorker(workers []workerproto.WorkerView, id uuid.UUID) (workerproto.WorkerView, bool) {
	for _, worker := range workers {
		if worker.WorkerID == id.String() {
			return worker, true
		}
	}
	return workerproto.WorkerView{}, false
}

func listedSession(sessions []workerproto.SessionView, id uuid.UUID) (workerproto.SessionView, bool) {
	for _, session := range sessions {
		if session.SessionID == id.String() {
			return session, true
		}
	}
	return workerproto.SessionView{}, false
}
