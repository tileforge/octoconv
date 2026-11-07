package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/workerproto"
)

type fakeCatalog struct {
	match     workerproto.Match
	found     bool
	err       error
	calls     int
	operation string
	source    string
	target    string
}

func (f *fakeCatalog) Lookup(_ context.Context, operation, source, target string) (workerproto.Match, bool, error) {
	f.calls++
	f.operation = operation
	f.source = source
	f.target = target
	return f.match, f.found, f.err
}

func postCreate(t *testing.T, srv *Server, filename, target string) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, filename, target, pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func assertClientError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.Bytes())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v; body=%s", err, rec.Body.Bytes())
	}
	if body["code"] != code {
		t.Fatalf("code = %v, want %s; body=%s", body["code"], code, rec.Body.Bytes())
	}
	if _, ok := body["error"].(string); !ok {
		t.Fatalf("error = %v, want string; body=%s", body["error"], rec.Body.Bytes())
	}
	for _, key := range []string{"message", "retryable", "invalid_message"} {
		if _, ok := body[key]; ok {
			t.Fatalf("client body contains %s: %s", key, rec.Body.Bytes())
		}
	}
}

func assertNoUploadOrEnqueue(t *testing.T, repo *fakeRepo, store *fakeStorage, q *fakeQueue) {
	t.Helper()
	if store.uploadCalls != 0 || store.uploaded {
		t.Fatalf("uploadCalls = %d, want 0", store.uploadCalls)
	}
	if repo.created != nil {
		t.Fatalf("Create was called: %+v", repo.created)
	}
	if q.enqueueCalls != 0 || q.enqueuedImage != uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedImage = %s, want no enqueue", q.enqueueCalls, q.enqueuedImage)
	}
}

func TestCreateJobWithoutCatalogStillEnqueuesImage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)

	rec := postCreate(t, srv, "in.png", "webp")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Engine != convert.EngineImage || repo.created.CapabilityID != "" {
		t.Fatalf("create params = %+v, want engine image and empty pin", repo.created)
	}
	if q.enqueueCalls != 1 || q.enqueuedImage == uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedImage = %s, want one image enqueue", q.enqueueCalls, q.enqueuedImage)
	}
}

func TestCatalogPinOnImageCreate(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{
		found: true,
		match: workerproto.Match{
			CapabilityID:      "octoconv.convert.image",
			CapabilityVersion: 1,
			ClientOperation:   convert.OperationConvert,
			LegacyOperation:   convert.OperationConvert,
			LegacyEngine:      convert.EngineImage,
			ParameterSchema:   []byte(`{}`),
		},
	}
	srv.SetCapabilityCatalog(catalog)

	rec := postCreate(t, srv, "in.png", "webp")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if catalog.calls != 1 || catalog.operation != convert.OperationConvert || catalog.source != "png" || catalog.target != "webp" {
		t.Fatalf("lookup calls=%d op=%s source=%s target=%s", catalog.calls, catalog.operation, catalog.source, catalog.target)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineImage {
		t.Fatalf("engine = %+v, want image", repo.created)
	}
	if repo.created.CapabilityID != "octoconv.convert.image" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d, want octoconv.convert.image v1", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
	if q.enqueueCalls != 1 || q.enqueuedImage == uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedImage = %s, want the image queue", q.enqueueCalls, q.enqueuedImage)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["capability_id"]; ok {
		t.Fatalf("client body exposes capability_id: %s", rec.Body.Bytes())
	}
}

func TestImagepdfCatalogDoesNotCreateJob(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{
		found: true,
		match: workerproto.Match{
			CapabilityID:      "octoconv.convert.imagepdf",
			CapabilityVersion: 1,
			ClientOperation:   convert.OperationConvert,
			ParameterSchema:   []byte(`{}`),
		},
	}
	srv.SetCapabilityCatalog(catalog)

	rec := postCreate(t, srv, "in.png", "pdf")
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	if catalog.calls != 0 {
		t.Fatalf("Lookup calls = %d, want 0", catalog.calls)
	}
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestNoLegacyCatalogRowDoesNotCreateJob(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{
		found: true,
		match: workerproto.Match{
			CapabilityID:      "octoconv.convert.imagepdf",
			CapabilityVersion: 1,
			ClientOperation:   convert.OperationConvert,
			ParameterSchema:   []byte(`{}`),
		},
	}
	srv.SetCapabilityCatalog(catalog)

	rec := postCreate(t, srv, "in.png", "webp")
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	if catalog.calls != 1 {
		t.Fatalf("Lookup calls = %d, want 1", catalog.calls)
	}
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestCatalogEngineMismatchIsInternal(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{
		found: true,
		match: workerproto.Match{
			CapabilityID:      "octoconv.convert.image",
			CapabilityVersion: 1,
			ClientOperation:   convert.OperationConvert,
			LegacyOperation:   convert.OperationConvert,
			LegacyEngine:      convert.EngineDocument,
			ParameterSchema:   []byte(`{}`),
		},
	}
	srv.SetCapabilityCatalog(catalog)

	rec := postCreate(t, srv, "in.png", "webp")
	assertClientError(t, rec, http.StatusInternalServerError, errcode.InternalError)
	assertNoUploadOrEnqueue(t, repo, store, q)
}
