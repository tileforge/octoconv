package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
)

func TestWorkflowFieldAbsentKeepsMissingTarget(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	body, ct := multipartFields(t, "in.png", pngBytesFixture(), map[string]string{
		"deadline": "not-a-deadline",
	})
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusBadRequest, errcode.InvalidRequest)
	var decoded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["error"] != "missing target format" {
		t.Fatalf("error %q", decoded["error"])
	}
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestWorkflowCreate(t *testing.T) {
	pool := workerAdminPool(t)
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, clientID := newWorkflowOperator(pool, repo, store, q)
	capabilityID := insertAPICapability(t, pool)
	insertAPIClient(t, pool, clientID)
	name := "wf-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	published := postSystemWorkflow(t, srv, map[string]any{
		"name": name, "version": 1, "client_ids": []uuid.UUID{clientID}, "steps": apiSteps(capabilityID),
	})
	if published.Code != http.StatusCreated {
		t.Fatalf("publish status %d body %s", published.Code, published.Body.String())
	}

	deadline := time.Now().Add(10 * time.Minute).Format(time.RFC3339)
	rec := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow": name + "@1",
		"deadline": deadline,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw["progress_pct"]; ok || len(raw) != 2 {
		t.Fatalf("body %s", rec.Body.String())
	}
	var created struct {
		JobID  uuid.UUID `json:"job_id"`
		Status string    `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if created.JobID == uuid.Nil || created.Status != jobs.StatusQueued {
		t.Fatalf("created %+v", created)
	}
	if repo.created != nil {
		t.Fatalf("Create was called: %+v", repo.created)
	}
	assertNoUploadOrEnqueue(t, repo, store, q)
	if n := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id = $1`, created.JobID); n != 1 {
		t.Fatalf("snapshots %d", n)
	}
	var attempts int
	var snapshotID uuid.UUID
	var owner, status, engine string
	var inputs, holds, creds int
	if err := pool.QueryRow(context.Background(), `
		SELECT attempts, workflow_snapshot_id, dispatch_owner, status, engine
		FROM jobs WHERE id = $1`, created.JobID).Scan(&attempts, &snapshotID, &owner, &status, &engine); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if attempts != 0 || snapshotID != created.JobID || owner != "protocol" || status != jobs.StatusQueued || engine != "image" {
		t.Fatalf("job attempts=%d snapshot=%s owner=%s status=%s engine=%s", attempts, snapshotID, owner, status, engine)
	}
	if n := countQuery(t, pool, `SELECT count(*) FROM job_inputs WHERE job_id = $1`, created.JobID); n != inputs {
		t.Fatalf("inputs %d", n)
	}
	if n := countQuery(t, pool, `SELECT count(*) FROM worker_slot_holds WHERE job_id = $1`, created.JobID); n != holds {
		t.Fatalf("holds %d", n)
	}
	if n := countQuery(t, pool, `SELECT count(*) FROM worker_attempt_credentials WHERE attempt_id IN (SELECT step_id FROM jobs WHERE id = $1)`, created.JobID); n != creds {
		t.Fatalf("credentials %d", n)
	}

	owned := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID)
	snaps := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID)
	badDeadline := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow": name + "@1",
		"deadline": "not-a-deadline",
	})
	assertClientError(t, badDeadline, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	if decodedError(t, badDeadline) != "invalid deadline" {
		t.Fatalf("error %q", decodedError(t, badDeadline))
	}
	missing := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow": "missing@1",
		"deadline": deadline,
	})
	assertClientError(t, missing, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	if decodedError(t, missing) != "invalid workflow" {
		t.Fatalf("error %q", decodedError(t, missing))
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID); got != owned {
		t.Fatalf("jobs %d, want %d", got, owned)
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID); got != snaps {
		t.Fatalf("snapshots %d, want %d", got, snaps)
	}
	if q.enqueueCalls != 0 || store.uploadCalls != 0 || repo.created != nil {
		t.Fatal("a rejected workflow stored or enqueued")
	}
}

func TestWorkflowStatus(t *testing.T) {
	pool := workerAdminPool(t)
	repo := jobs.NewRepo(pool)
	q := &fakeQueue{}
	store := &fakeStorage{}
	srv, clientID := newWorkflowOperator(pool, repo, store, q)
	capabilityID := insertAPICapability(t, pool)
	insertAPIClient(t, pool, clientID)
	name := "wf-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	published := postSystemWorkflow(t, srv, map[string]any{
		"name": name, "version": 1, "client_ids": []uuid.UUID{clientID}, "steps": apiSteps(capabilityID),
	})
	if published.Code != http.StatusCreated {
		t.Fatalf("publish status %d body %s", published.Code, published.Body.String())
	}
	created := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow": name + "@1",
		"deadline": time.Now().Add(10 * time.Minute).Format(time.RFC3339),
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", created.Code, created.Body.String())
	}
	var job struct {
		JobID uuid.UUID `json:"job_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+job.JobID.String()+"/workflow", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("workflow status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	if len(body) != 3 || string(body["current_step"]) != "null" || string(body["status"]) != `"`+jobs.StatusQueued+`"` {
		t.Fatalf("body %s", rec.Body.String())
	}
	var steps []map[string]json.RawMessage
	if err := json.Unmarshal(body["steps"], &steps); err != nil {
		t.Fatalf("decode steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps %d", len(steps))
	}
	for _, step := range steps {
		if len(step) != 6 || string(step["progress_pct"]) != "null" {
			t.Fatalf("step %s", rec.Body.String())
		}
		if _, err := uuid.Parse(strings.Trim(string(step["step_id"]), `"`)); err != nil {
			t.Fatalf("step_id %s", step["step_id"])
		}
		if _, ok := step["legacy_engine"]; ok {
			t.Fatal("status returned legacy_engine")
		}
		if string(step["capability_id"]) != `"`+capabilityID+`"` {
			t.Fatalf("capability %s", step["capability_id"])
		}
	}

	jobRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(jobRec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+job.JobID.String(), nil)))
	if jobRec.Code != http.StatusOK {
		t.Fatalf("job status %d body %s", jobRec.Code, jobRec.Body.String())
	}
	assertOmitsWorkflowKeys(t, jobRec.Body.Bytes())
	var jobBody map[string]json.RawMessage
	if err := json.Unmarshal(jobRec.Body.Bytes(), &jobBody); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if string(jobBody["progress_pct"]) != "null" {
		t.Fatalf("progress %s", jobBody["progress_pct"])
	}
	if q.enqueueCalls != 0 || store.uploadCalls != 0 {
		t.Fatal("status read enqueued or uploaded")
	}

	plain := insertPlainAPIJob(t, pool, clientID)
	plainRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(plainRec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+plain.String()+"/workflow", nil)))
	if plainRec.Code != http.StatusNotFound {
		t.Fatalf("one-step workflow status %d body %s", plainRec.Code, plainRec.Body.String())
	}
	want := httptest.NewRecorder()
	writeError(want, http.StatusNotFound, errcode.NotFound, "job not found")
	if plainRec.Body.String() != want.Body.String() {
		t.Fatalf("one-step body %s", plainRec.Body.String())
	}

	otherRec := httptest.NewRecorder()
	otherReq := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+job.JobID.String()+"/workflow", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", job.JobID.String())
	ctx := auth.WithClient(otherReq.Context(), &clients.Client{ID: uuid.New(), Name: "other"})
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	srv.handleJobWorkflow(otherRec, otherReq.WithContext(ctx))
	if otherRec.Code != http.StatusNotFound || otherRec.Body.String() != want.Body.String() {
		t.Fatalf("other client %d %s", otherRec.Code, otherRec.Body.String())
	}
}

func TestOneStepJobOmitsWorkflowKeys(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	id := uuid.New()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusQueued, Engine: "image"}}
	srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	assertOmitsWorkflowKeys(t, rec.Body.Bytes())

	png := postCreate(t, srv, "in.png", "pdf")
	assertClientError(t, png, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
}

func postWorkflow(t *testing.T, srv *Server, filename string, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartFields(t, filename, data, fields)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func multipartFields(t *testing.T, filename string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if filename != "" {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range fields {
		if err := w.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func decodedError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body["error"]
}

func assertOmitsWorkflowKeys(t *testing.T, raw []byte) {
	t.Helper()
	text := string(raw)
	for _, key := range []string{`"workflow"`, `"current_step"`, `"step_id"`} {
		if strings.Contains(text, key) {
			t.Fatalf("body contains %s: %s", key, text)
		}
	}
}
