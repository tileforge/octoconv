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
	"github.com/tileforge/octoconv/internal/storage"
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
	if q.enqueueCalls != 0 || store.uploadCalls != 1 {
		t.Fatalf("enqueue %d upload %d", q.enqueueCalls, store.uploadCalls)
	}
	if n := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id = $1`, created.JobID); n != 1 {
		t.Fatalf("snapshots %d", n)
	}
	var attempts int
	var snapshotID uuid.UUID
	var owner, status, engine string
	var holds, creds int
	if err := pool.QueryRow(context.Background(), `
		SELECT attempts, workflow_snapshot_id, dispatch_owner, status, engine
		FROM jobs WHERE id = $1`, created.JobID).Scan(&attempts, &snapshotID, &owner, &status, &engine); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if attempts != 0 || snapshotID != created.JobID || owner != "protocol" || status != jobs.StatusQueued || engine != "image" {
		t.Fatalf("job attempts=%d snapshot=%s owner=%s status=%s engine=%s", attempts, snapshotID, owner, status, engine)
	}
	if n := countQuery(t, pool, `SELECT count(*) FROM job_inputs WHERE job_id = $1`, created.JobID); n != 1 {
		t.Fatalf("inputs %d", n)
	}
	var stepID uuid.UUID
	var ordinal int
	var objectKey, filename, format, contentType string
	var size int64
	if err := pool.QueryRow(context.Background(), `
		SELECT j.step_id, i.ordinal, i.object_key, i.filename, i.format, i.size_bytes, i.content_type
		FROM jobs j JOIN job_inputs i ON i.job_id = j.id
		WHERE j.id = $1`, created.JobID).Scan(&stepID, &ordinal, &objectKey, &filename, &format, &size, &contentType); err != nil {
		t.Fatalf("read input: %v", err)
	}
	var rawSnap []byte
	if err := pool.QueryRow(context.Background(), `SELECT document FROM workflow_snapshots WHERE job_id = $1`, created.JobID).Scan(&rawSnap); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap struct {
		Steps []struct {
			StepID uuid.UUID `json:"step_id"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(rawSnap, &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	wantKey := storage.InputKey(created.JobID, 0, "photo.jpeg")
	if len(snap.Steps) != 2 || stepID != snap.Steps[0].StepID || ordinal != 0 || objectKey != wantKey || filename != "photo.jpeg" || format != "jpg" || contentType != "image/jpeg" || size != int64(len(pngBytesFixture())) {
		t.Fatalf("step %s key %s filename %s format %s type %s size %d", stepID, objectKey, filename, format, contentType, size)
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
	if store.uploadCalls != 1 || q.enqueueCalls != 0 || repo.created != nil {
		t.Fatalf("bad deadline uploaded or enqueued: upload %d enqueue %d", store.uploadCalls, q.enqueueCalls)
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
	if store.uploadCalls != 2 || q.enqueueCalls != 0 || repo.created != nil {
		t.Fatalf("unknown workflow upload %d enqueue %d", store.uploadCalls, q.enqueueCalls)
	}
}

func TestWorkflowQuotaRejectsBeforeInsert(t *testing.T) {
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
	if _, err := pool.Exec(context.Background(), `UPDATE clients SET enforcement_mode = 'enforce' WHERE id = $1`, clientID); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO client_limits (client_id, engine, max_concurrent)
		VALUES ($1, 'image', 0)`, clientID); err != nil {
		t.Fatalf("limit: %v", err)
	}
	owned := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID)
	snaps := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID)
	rec := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow": name + "@1",
		"deadline": time.Now().Add(10 * time.Minute).Format(time.RFC3339),
	})
	assertClientError(t, rec, http.StatusTooManyRequests, errcode.QuotaExceeded)
	if rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	if decodedError(t, rec) != "too many concurrent jobs for this conversion type" {
		t.Fatalf("error %q", decodedError(t, rec))
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID); got != owned {
		t.Fatalf("jobs %d, want %d", got, owned)
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID); got != snaps {
		t.Fatalf("snapshots %d, want %d", got, snaps)
	}
	if store.uploadCalls != 1 || q.enqueueCalls != 0 || repo.created != nil {
		t.Fatalf("quota upload %d enqueue %d", store.uploadCalls, q.enqueueCalls)
	}
}

func TestWorkflowRejectsExtraFile(t *testing.T) {
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
	owned := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID)
	snaps := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, filename := range []string{"a.jpeg", "b.jpeg"} {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(pngBytesFixture()); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteField("workflow", name+"@1"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("deadline", time.Now().Add(10*time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", &body))
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	if decodedError(t, rec) != "this conversion accepts a single file" {
		t.Fatalf("error %q", decodedError(t, rec))
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID); got != owned {
		t.Fatalf("jobs %d, want %d", got, owned)
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID); got != snaps {
		t.Fatalf("snapshots %d, want %d", got, snaps)
	}
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestWorkflowKeepsCallbackSettings(t *testing.T) {
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
	owned := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID)
	snaps := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID)
	rejected := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow":         name + "@1",
		"deadline":         deadline,
		"callback_url":     "https://127.0.0.1/hook",
		"progress_webhook": "true",
	})
	assertClientError(t, rejected, http.StatusBadRequest, errcode.InvalidRequest)
	if decodedError(t, rejected) != "invalid callback_url" {
		t.Fatalf("error %q", decodedError(t, rejected))
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID); got != owned {
		t.Fatalf("jobs %d, want %d", got, owned)
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID); got != snaps {
		t.Fatalf("snapshots %d, want %d", got, snaps)
	}
	assertNoUploadOrEnqueue(t, repo, store, q)

	flagOnly := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow":         name + "@1",
		"deadline":         deadline,
		"progress_webhook": "true",
	})
	if flagOnly.Code != http.StatusCreated {
		t.Fatalf("flag-only status %d body %s", flagOnly.Code, flagOnly.Body.String())
	}
	flagJob := decodeCreatedJob(t, flagOnly)
	var flagURL *string
	var flagProgress bool
	if err := pool.QueryRow(context.Background(), `
		SELECT callback_url, progress_webhook FROM jobs WHERE id = $1`, flagJob).Scan(&flagURL, &flagProgress); err != nil {
		t.Fatalf("read flag-only: %v", err)
	}
	if flagURL != nil && *flagURL != "" || flagProgress {
		t.Fatalf("flag-only callback %v progress %v", flagURL, flagProgress)
	}

	const hook = "https://8.8.8.8/hook"
	kept := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), map[string]string{
		"workflow":         name + "@1",
		"deadline":         deadline,
		"callback_url":     hook,
		"progress_webhook": "true",
	})
	if kept.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", kept.Code, kept.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(kept.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw["progress_pct"]; ok || len(raw) != 2 {
		t.Fatalf("body %s", kept.Body.String())
	}
	keptJob := decodeCreatedJob(t, kept)
	var stored string
	var progress bool
	if err := pool.QueryRow(context.Background(), `
		SELECT callback_url, progress_webhook FROM jobs WHERE id = $1`, keptJob).Scan(&stored, &progress); err != nil {
		t.Fatalf("read callback: %v", err)
	}
	if stored != hook || !progress {
		t.Fatalf("callback %q progress %v", stored, progress)
	}
	if store.uploadCalls != 2 || q.enqueueCalls != 0 || repo.created != nil {
		t.Fatalf("upload %d enqueue %d", store.uploadCalls, q.enqueueCalls)
	}
}

func TestWorkflowKeepsCorrelationID(t *testing.T) {
	pool := workerAdminPool(t)
	repo := jobs.NewRepo(pool)
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
	fields := map[string]string{"workflow": name + "@1", "deadline": deadline}
	owned := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID)
	snaps := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID)
	rejected := postWorkflowHeader(t, srv, "photo.jpeg", pngBytesFixture(), fields, "bad id")
	assertClientError(t, rejected, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	if decodedError(t, rejected) != "Correlation-Id must be at most 128 printable ASCII characters without spaces" {
		t.Fatalf("error %q", decodedError(t, rejected))
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID); got != owned {
		t.Fatalf("jobs %d, want %d", got, owned)
	}
	if got := countQuery(t, pool, `SELECT count(*) FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, clientID); got != snaps {
		t.Fatalf("snapshots %d, want %d", got, snaps)
	}
	if q.enqueueCalls != 0 || store.uploadCalls != 0 {
		t.Fatal("an invalid correlation id stored or enqueued")
	}

	const want = "svc-checkout/req-8f31c2a4"
	kept := postWorkflowHeader(t, srv, "photo.jpeg", pngBytesFixture(), fields, want)
	if kept.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", kept.Code, kept.Body.String())
	}
	var created map[string]json.RawMessage
	if err := json.Unmarshal(kept.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if _, ok := created["correlation_id"]; ok || len(created) != 2 {
		t.Fatalf("create body %s", kept.Body.String())
	}
	keptID := decodeCreatedJob(t, kept)
	statusRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(statusRec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+keptID.String(), nil)))
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", statusRec.Code, statusRec.Body.String())
	}
	var status map[string]json.RawMessage
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if string(status["correlation_id"]) != `"`+want+`"` {
		t.Fatalf("status correlation %s", status["correlation_id"])
	}
	var stored *string
	if err := pool.QueryRow(context.Background(), `SELECT correlation_id FROM jobs WHERE id = $1`, keptID).Scan(&stored); err != nil {
		t.Fatalf("read correlation: %v", err)
	}
	if stored == nil || *stored != want {
		t.Fatalf("stored %v", stored)
	}

	plain := postWorkflow(t, srv, "photo.jpeg", pngBytesFixture(), fields)
	if plain.Code != http.StatusCreated {
		t.Fatalf("plain status %d body %s", plain.Code, plain.Body.String())
	}
	plainID := decodeCreatedJob(t, plain)
	plainRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(plainRec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+plainID.String(), nil)))
	if plainRec.Code != http.StatusOK {
		t.Fatalf("plain get %d body %s", plainRec.Code, plainRec.Body.String())
	}
	if strings.Contains(plainRec.Body.String(), "correlation_id") {
		t.Fatalf("absent correlation echoed: %s", plainRec.Body.String())
	}
	var empty *string
	if err := pool.QueryRow(context.Background(), `SELECT correlation_id FROM jobs WHERE id = $1`, plainID).Scan(&empty); err != nil {
		t.Fatalf("read empty correlation: %v", err)
	}
	if empty != nil {
		t.Fatalf("stored %q", *empty)
	}
	if q.enqueueCalls != 0 || store.uploadCalls != 2 {
		t.Fatalf("upload %d enqueue %d", store.uploadCalls, q.enqueueCalls)
	}
}

func postWorkflowHeader(t *testing.T, srv *Server, filename string, data []byte, fields map[string]string, correlationID string) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartFields(t, filename, data, fields)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set(CorrelationHeader, correlationID)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func decodeCreatedJob(t *testing.T, rec *httptest.ResponseRecorder) uuid.UUID {
	t.Helper()
	var created struct {
		JobID uuid.UUID `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if created.JobID == uuid.Nil {
		t.Fatal("missing job id")
	}
	return created.JobID
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
	var stepID uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT step_id FROM jobs WHERE id = $1`, job.JobID).Scan(&stepID); err != nil {
		t.Fatalf("read step: %v", err)
	}
	if len(body) != 3 || string(body["current_step"]) != `"`+stepID.String()+`"` || string(body["status"]) != `"`+jobs.StatusQueued+`"` {
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
	if _, ok := jobBody["download_url"]; ok || strings.Contains(jobRec.Body.String(), "download_url") {
		t.Fatalf("queued download %s", jobRec.Body.String())
	}

	midKey := "results/" + job.JobID.String() + "/intermediate"
	outKey := "results/" + job.JobID.String() + "/final"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workflow_step_results (
			job_id, step_id, attempt_id, object_key, format, size_bytes, content_type, sha256
		) VALUES ($1, $2, $3, $4, 'png', 4, 'image/png', $5)`,
		job.JobID, stepID, uuid.New(), midKey, strings.Repeat("cd", 32)); err != nil {
		t.Fatalf("insert result: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type, role)
		VALUES ($1, 0, $2, 'out.jpg', 'jpg', 4, 'image/jpeg', 'primary')`, job.JobID, outKey); err != nil {
		t.Fatalf("insert output: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE jobs SET status = 'done', progress_pct = 100 WHERE id = $1`, job.JobID); err != nil {
		t.Fatalf("finish job: %v", err)
	}
	store.presignByKey = map[string]string{outKey: "https://files.test/final"}
	doneRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(doneRec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+job.JobID.String(), nil)))
	if doneRec.Code != http.StatusOK {
		t.Fatalf("done status %d body %s", doneRec.Code, doneRec.Body.String())
	}
	var doneBody map[string]json.RawMessage
	if err := json.Unmarshal(doneRec.Body.Bytes(), &doneBody); err != nil {
		t.Fatalf("decode done: %v", err)
	}
	if string(doneBody["download_url"]) != `"https://files.test/final"` || string(doneBody["progress_pct"]) != "100" {
		t.Fatalf("done body %s", doneRec.Body.String())
	}
	if _, ok := doneBody["outputs"]; ok || strings.Contains(doneRec.Body.String(), midKey) {
		t.Fatalf("done body exposed an intermediate: %s", doneRec.Body.String())
	}
	doneFlow := httptest.NewRecorder()
	srv.Routes().ServeHTTP(doneFlow, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+job.JobID.String()+"/workflow", nil)))
	if doneFlow.Code != http.StatusOK {
		t.Fatalf("done workflow %d body %s", doneFlow.Code, doneFlow.Body.String())
	}
	var doneView map[string]json.RawMessage
	if err := json.Unmarshal(doneFlow.Body.Bytes(), &doneView); err != nil {
		t.Fatalf("decode done workflow: %v", err)
	}
	if len(doneView) != 3 || string(doneView["current_step"]) != "null" || string(doneView["status"]) != `"`+jobs.StatusDone+`"` {
		t.Fatalf("done workflow %s", doneFlow.Body.String())
	}
	var doneSteps []map[string]json.RawMessage
	if err := json.Unmarshal(doneView["steps"], &doneSteps); err != nil {
		t.Fatalf("decode done steps: %v", err)
	}
	if len(doneSteps) != 2 {
		t.Fatalf("done steps %d", len(doneSteps))
	}
	for _, step := range doneSteps {
		if string(step["progress_pct"]) != "null" {
			t.Fatalf("done step %s", doneFlow.Body.String())
		}
	}
	if q.enqueueCalls != 0 || store.uploadCalls != 1 {
		t.Fatalf("upload %d enqueue %d", store.uploadCalls, q.enqueueCalls)
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
