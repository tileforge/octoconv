package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/workflow"
)

func TestSystemWorkflows(t *testing.T) {
	pool := workerAdminPool(t)
	srv, clientID := newWorkflowOperator(pool, &fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	capabilityID := insertAPICapability(t, pool)
	insertAPIClient(t, pool, clientID)
	name := "wf-" + strings.ReplaceAll(uuid.NewString(), "-", "")

	empty := postSystemWorkflow(t, srv, map[string]any{
		"name": name, "version": 1, "client_ids": []uuid.UUID{}, "steps": apiSteps(capabilityID),
	})
	assertClientError(t, empty, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	if empty.Body.String() == "" || !strings.Contains(empty.Body.String(), "invalid workflow") {
		t.Fatalf("empty clients body %s", empty.Body.String())
	}
	if n := countQuery(t, pool, `SELECT count(*) FROM workflow_definitions WHERE name = $1`, name); n != 0 {
		t.Fatalf("empty client_ids stored %d definitions", n)
	}

	unknown := postSystemWorkflow(t, srv, map[string]any{
		"name": name, "version": 1, "client_ids": []uuid.UUID{uuid.New()}, "steps": apiSteps(capabilityID),
	})
	assertClientError(t, unknown, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	if n := countQuery(t, pool, `SELECT count(*) FROM workflow_definitions WHERE name = $1`, name); n != 0 {
		t.Fatalf("unknown client stored %d definitions", n)
	}

	denied := newOperatorGateTestServer(map[uuid.UUID]struct{}{uuid.New(): {}})
	denied.SetWorkflows(workflow.NewStore(pool))
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/system/workflows", nil),
		httptest.NewRequest(http.MethodPost, "/v1/system/workflows", strings.NewReader(`{"name":"`+name+`","version":1,"client_ids":["`+clientID.String()+`"],"steps":[]}`)),
	} {
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		denied.Routes().ServeHTTP(rec, authed(req))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status %d body %s", req.Method, rec.Code, rec.Body.String())
		}
		want := httptest.NewRecorder()
		writeError(want, http.StatusNotFound, errcode.UnknownPreset, noSuchPreset)
		if rec.Body.String() != want.Body.String() {
			t.Fatalf("body %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), name) {
			t.Fatalf("non-operator body contains %s", name)
		}
	}

	bareResolver := newFakeResolver()
	bare := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, bareResolver, healthyDeps(), Config{
		MaxUploadBytes:    1 << 20,
		OperatorClientIDs: map[uuid.UUID]struct{}{bareResolver.client.ID: {}},
	})
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/system/workflows", nil),
		httptest.NewRequest(http.MethodPost, "/v1/system/workflows", strings.NewReader(`{}`)),
	} {
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		bare.Routes().ServeHTTP(rec, authed(req))
		assertClientError(t, rec, http.StatusInternalServerError, errcode.InternalError)
	}

	created := postSystemWorkflow(t, srv, map[string]any{
		"name": name, "version": 1, "client_ids": []uuid.UUID{clientID}, "steps": apiSteps(capabilityID),
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("publish status %d body %s", created.Code, created.Body.String())
	}
	var stored workflow.Definition
	if err := json.Unmarshal(created.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode publish: %v body %s", err, created.Body.String())
	}
	if stored.Name != name || stored.Version != 1 || len(stored.ClientIDs) != 1 || stored.ClientIDs[0] != clientID {
		t.Fatalf("stored %+v", stored)
	}
	assertDocumentOmitsStepID(t, stored.Document)

	again := postSystemWorkflow(t, srv, map[string]any{
		"name": name, "version": 1, "client_ids": []uuid.UUID{clientID}, "steps": apiSteps(capabilityID),
	})
	assertClientError(t, again, http.StatusConflict, errcode.AlreadyExists)
	if n := countQuery(t, pool, `SELECT count(*) FROM workflow_definitions WHERE name = $1 AND version = 1`, name); n != 1 {
		t.Fatalf("definitions %d", n)
	}
	if n := countQuery(t, pool, `SELECT count(*) FROM workflow_grants WHERE name = $1 AND version = 1`, name); n != 1 {
		t.Fatalf("grants %d", n)
	}

	listedRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(listedRec, authed(httptest.NewRequest(http.MethodGet, "/v1/system/workflows", nil)))
	if listedRec.Code != http.StatusOK {
		t.Fatalf("list status %d body %s", listedRec.Code, listedRec.Body.String())
	}
	var listed struct {
		Workflows []workflow.Definition `json:"workflows"`
	}
	if err := json.Unmarshal(listedRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found *workflow.Definition
	for i := range listed.Workflows {
		if listed.Workflows[i].Name == name && listed.Workflows[i].Version == 1 {
			found = &listed.Workflows[i]
			break
		}
	}
	if found == nil || len(found.ClientIDs) != 1 || found.ClientIDs[0] != clientID {
		t.Fatalf("listed %+v", found)
	}
	assertDocumentOmitsStepID(t, found.Document)
}

func newWorkflowOperator(pool *pgxpool.Pool, repo Repo, store Storage, q Enqueuer) (*Server, uuid.UUID) {
	resolver := newFakeResolver()
	srv := NewServer(repo, store, q, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes:    1 << 20,
		OperatorClientIDs: map[uuid.UUID]struct{}{resolver.client.ID: {}},
	})
	srv.SetWorkflows(workflow.NewStore(pool))
	return srv, resolver.client.ID
}

func postSystemWorkflow(t *testing.T, srv *Server, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/system/workflows", bytes.NewReader(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

type apiStep struct {
	CapabilityID      string `json:"capability_id"`
	CapabilityVersion int    `json:"capability_version"`
	SourceFormat      string `json:"source_format"`
	TargetFormat      string `json:"target_format"`
	InputRole         string `json:"input_role"`
	OutputRole        string `json:"output_role"`
}

func apiSteps(id string) []apiStep {
	return []apiStep{
		{id, 1, "jpg", "png", "source", "primary"},
		{id, 1, "png", "jpg", "source", "primary"},
	}
}

func insertAPIClient(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO clients (id, name) VALUES ($1, $2)`, id, "api-"+id.String()); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `UPDATE jobs SET workflow_snapshot_id = NULL WHERE client_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM jobs WHERE client_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_grants WHERE client_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM clients WHERE id = $1`, id)
	})
}

func insertAPICapability(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	workerID := uuid.New()
	hexID := strings.ReplaceAll(workerID.String(), "-", "")
	id := "octoconv.test.a" + hexID
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_identities (worker_id, manifest_sha256, manifest_bytes)
		VALUES ($1, $2, $3)`, workerID, strings.Repeat("ab", 32), []byte("manifest")); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capabilities (
			id, version, worker_id, client_operation, legacy_operation, legacy_engine, parameter_schema
		) VALUES ($1, 1, $2, $3, 'convert', 'image', '{}')`, id, workerID, "op."+hexID); err != nil {
		t.Fatalf("insert capability: %v", err)
	}
	for _, pair := range [][2]string{{"jpg", "png"}, {"png", "jpg"}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO worker_capability_formats (
				capability_id, capability_version, source_format, target_format, client_operation
			) VALUES ($1, 1, $2, $3, $4)`, id, pair[0], pair[1], "op."+hexID); err != nil {
			t.Fatalf("insert format: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capability_ports (
			capability_id, capability_version, direction, role, min_count, max_count, ordinal
		) VALUES
			($1, 1, 'input', 'source', 1, 1, 0),
			($1, 1, 'output', 'primary', 1, 1, 0)`, id); err != nil {
		t.Fatalf("insert ports: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_grants WHERE name IN (SELECT name FROM workflow_definitions WHERE document::text LIKE $1)`, "%"+id+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_definitions WHERE document::text LIKE $1`, "%"+id+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_ports WHERE capability_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_formats WHERE capability_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capabilities WHERE id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_identities WHERE worker_id = $1`, workerID)
	})
	return id
}

func countQuery(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func assertDocumentOmitsStepID(t *testing.T, raw []byte) {
	t.Helper()
	var doc struct {
		Steps []map[string]json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if len(doc.Steps) != 2 {
		t.Fatalf("steps %d", len(doc.Steps))
	}
	for _, step := range doc.Steps {
		if _, ok := step["step_id"]; ok {
			t.Fatal("published document has step_id")
		}
	}
}

func insertPlainAPIJob(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (id, client_id, operation, engine, status, source_format, target_format, options)
		VALUES ($1, $2, 'convert', 'image', $3, 'png', 'webp', '{}')`, id, clientID, jobs.StatusQueued); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return id
}
