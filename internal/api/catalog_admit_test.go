package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

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
	allow     func(operation, source, target string) (workerproto.Match, bool)
}

func (f *fakeCatalog) Lookup(_ context.Context, operation, source, target string) (workerproto.Match, bool, error) {
	f.calls++
	f.operation = operation
	f.source = source
	f.target = target
	if f.allow != nil {
		match, found := f.allow(operation, source, target)
		return match, found, nil
	}
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
	if repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
		t.Fatalf("dispatch = %q budget = %s, want legacy defaults", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 1 || q.enqueuedImage == uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedImage = %s, want one image enqueue", q.enqueueCalls, q.enqueuedImage)
	}
}

func TestCatalogMissEnqueuesImage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{found: false}
	srv.SetCapabilityCatalog(catalog)

	rec := postCreate(t, srv, "in.png", "webp")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if catalog.calls != 1 {
		t.Fatalf("Lookup calls = %d, want 1", catalog.calls)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineImage || repo.created.CapabilityID != "" {
		t.Fatalf("create params = %+v, want engine image and empty pin", repo.created)
	}
	if repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
		t.Fatalf("dispatch = %q budget = %s, want legacy defaults", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
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
	if q.enqueueCalls != 0 || q.enqueuedImage != uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedImage = %s, want no enqueue", q.enqueueCalls, q.enqueuedImage)
	}
	if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 120*time.Second {
		t.Fatalf("dispatch = %q budget = %s, want protocol 120s", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["capability_id"]; ok {
		t.Fatalf("client body exposes capability_id: %s", rec.Body.Bytes())
	}
}

func postCAD(t *testing.T, srv *Server, filename, target, operation string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBodyWithOperation(t, filename, target, operation, data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func cadMatch(id, operation string) workerproto.Match {
	return workerproto.Match{
		CapabilityID:      id,
		CapabilityVersion: 1,
		ClientOperation:   operation,
		LegacyOperation:   operation,
		LegacyEngine:      convert.EngineCAD,
		ParameterSchema:   []byte(`{}`),
	}
}

func TestApprovedCADConvertSkipsEnqueue(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{found: true, match: cadMatch("octoconv.convert.cad", convert.OperationConvert)}
	srv.SetCapabilityCatalog(catalog)

	rec := postCAD(t, srv, "part.step", "stl", "", readCADFixture(t, "synthetic/units_inch.step"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if catalog.operation != convert.OperationConvert || catalog.source != "step" || catalog.target != "stl" {
		t.Fatalf("lookup op=%s source=%s target=%s", catalog.operation, catalog.source, catalog.target)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineCAD || repo.created.Operation != convert.OperationConvert {
		t.Fatalf("create params = %+v, want engine cad operation convert", repo.created)
	}
	if repo.created.CapabilityID != "octoconv.convert.cad" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
	if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != convert.CADDefaultEngineTimeout {
		t.Fatalf("dispatch = %q budget = %s", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 0 || q.enqueuedCAD != uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedCAD = %s, want no enqueue", q.enqueueCalls, q.enqueuedCAD)
	}
}

func TestApprovedCADUsesConfiguredBudget(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.engineTimeouts = map[string]time.Duration{convert.EngineCAD: 5 * time.Minute}
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: cadMatch("octoconv.convert.cad", convert.OperationConvert)})

	rec := postCAD(t, srv, "part.step", "stl", "", readCADFixture(t, "synthetic/units_inch.step"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.ProtocolAttemptBudget != 5*time.Minute {
		t.Fatalf("budget = %s, want 5m", repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 0 {
		t.Fatalf("enqueueCalls = %d, want 0", q.enqueueCalls)
	}
}

func TestApprovedCADOverLinkWindowStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	over := time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.engineTimeouts = map[string]time.Duration{convert.EngineCAD: over}
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: cadMatch("octoconv.convert.cad", convert.OperationConvert)})

	rec := postCAD(t, srv, "part.step", "stl", "", readCADFixture(t, "synthetic/units_inch.step"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Engine != convert.EngineCAD || repo.created.Operation != convert.OperationConvert {
		t.Fatalf("create params = %+v, want engine cad operation convert", repo.created)
	}
	if repo.created.CapabilityID != "" || repo.created.CapabilityVersion != 0 || repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
		t.Fatalf("pin = %q v%d dispatch = %q budget = %s, want asynq", repo.created.CapabilityID, repo.created.CapabilityVersion, repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 1 || q.enqueuedCAD != repo.createdID {
		t.Fatalf("enqueueCalls = %d enqueuedCAD = %s created = %s", q.enqueueCalls, q.enqueuedCAD, repo.createdID)
	}
}

func TestApprovedImageOverLinkWindowStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	over := time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.engineTimeouts = map[string]time.Duration{convert.EngineImage: over}
	srv.SetCapabilityCatalog(&fakeCatalog{
		found: true,
		match: workerproto.Match{
			CapabilityID:      "octoconv.convert.image",
			CapabilityVersion: 1,
			ClientOperation:   convert.OperationConvert,
			LegacyOperation:   convert.OperationConvert,
			LegacyEngine:      convert.EngineImage,
			ParameterSchema:   []byte(`{}`),
		},
	})

	rec := postCreate(t, srv, "in.png", "webp")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Engine != convert.EngineImage {
		t.Fatalf("engine = %+v, want image", repo.created)
	}
	if repo.created.CapabilityID != "octoconv.convert.image" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d, want the image pin kept", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
	if repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
		t.Fatalf("dispatch = %q budget = %s, want asynq", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 1 || q.enqueuedImage != repo.createdID {
		t.Fatalf("enqueueCalls = %d enqueuedImage = %s created = %s", q.enqueueCalls, q.enqueuedImage, repo.createdID)
	}
}

func TestApprovedCADRenderStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{found: true, match: cadMatch("octoconv.render.cad", convert.OperationRender)}
	srv.SetCapabilityCatalog(catalog)

	rec := postCAD(t, srv, "part.step", "png", convert.OperationRender, readCADFixture(t, "synthetic/units_inch.step"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if catalog.operation != convert.OperationRender || catalog.source != "step" || catalog.target != "png" {
		t.Fatalf("lookup op=%s source=%s target=%s", catalog.operation, catalog.source, catalog.target)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineCAD || repo.created.Operation != convert.OperationRender {
		t.Fatalf("create params = %+v, want engine cad operation render", repo.created)
	}
	if repo.created.CapabilityID != "" || repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
		t.Fatalf("pin = %q dispatch = %q budget = %s, want asynq", repo.created.CapabilityID, repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 1 || q.enqueuedCAD != repo.createdID {
		t.Fatalf("enqueueCalls = %d enqueuedCAD = %s created = %s", q.enqueueCalls, q.enqueuedCAD, repo.createdID)
	}
}

func TestApprovedCADOptionsStayOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: cadMatch("octoconv.convert.cad", convert.OperationConvert)})
	data := readCADFixture(t, "synthetic/units_inch.step")
	body, ct := multipartBodyWithOpts(t, "part.step", "stl", data, `{"quality":"fine"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Engine != convert.EngineCAD || len(repo.created.Opts) == 0 {
		t.Fatalf("create params = %+v, want cad opts", repo.created)
	}
	if repo.created.CapabilityID != "" || repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
		t.Fatalf("pin = %q dispatch = %q budget = %s, want asynq", repo.created.CapabilityID, repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 1 || q.enqueuedCAD != repo.createdID {
		t.Fatalf("enqueueCalls = %d enqueuedCAD = %s created = %s", q.enqueueCalls, q.enqueuedCAD, repo.createdID)
	}
}

func TestCatalogMissEnqueuesCAD(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{found: false})

	rec := postCAD(t, srv, "part.step", "stl", "", readCADFixture(t, "synthetic/units_inch.step"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Engine != convert.EngineCAD || repo.created.CapabilityID != "" {
		t.Fatalf("create params = %+v, want engine cad and empty pin", repo.created)
	}
	if repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
		t.Fatalf("dispatch = %q budget = %s, want asynq", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 1 || q.enqueuedCAD != repo.createdID {
		t.Fatalf("enqueueCalls = %d enqueuedCAD = %s created = %s", q.enqueueCalls, q.enqueuedCAD, repo.createdID)
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

func TestCatalogSchemaAcceptsEmptyObject(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(legacyImageCatalog(`{}`))

	rec := postCreate(t, srv, "in.png", "webp")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded || store.uploadCalls == 0 {
		t.Fatal("schema {} must still upload")
	}
	if repo.created == nil || repo.created.CapabilityID != "octoconv.convert.image" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %+v, want octoconv.convert.image v1", repo.created)
	}
	if q.enqueueCalls != 0 || q.enqueuedImage != uuid.Nil {
		t.Fatalf("enqueueCalls = %d, want no enqueue", q.enqueueCalls)
	}
	if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 120*time.Second {
		t.Fatalf("dispatch = %q budget = %s, want protocol 120s", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
}

func TestApprovedImageCreateSkipsEnqueue(t *testing.T) {
	data, err := os.ReadFile("../convert/testdata/sanitize/exif_gps.jpg")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(legacyImageCatalog(`{}`))

	body, ct := multipartBody(t, "exif_gps.jpg", "png", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "queued" || resp["job_id"] == "" {
		t.Fatalf("body = %s, want job_id and status queued", rec.Body.Bytes())
	}
	if _, ok := resp["capability_id"]; ok {
		t.Fatalf("client body exposes capability_id: %s", rec.Body.Bytes())
	}
	if repo.created == nil || repo.created.Engine != convert.EngineImage || repo.created.SourceFormat != "jpg" || repo.created.TargetFormat != "png" {
		t.Fatalf("create params = %+v, want image jpg to png", repo.created)
	}
	if repo.created.CapabilityID != "octoconv.convert.image" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
	if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 120*time.Second {
		t.Fatalf("dispatch = %q budget = %s", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 0 || q.enqueuedImage != uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedImage = %s, want no enqueue", q.enqueueCalls, q.enqueuedImage)
	}
}

func TestCatalogSchemaRejectsBeforeUpload(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(legacyImageCatalog(`{"type":"object","required":["n"],"additionalProperties":true}`))

	rec := postCreate(t, srv, "in.png", "webp")
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.InvalidOptions)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestDocumentCreateStillEnqueues(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "png" && target == "webp" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.image",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineImage,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
	}
	srv.SetCapabilityCatalog(catalog)

	body, ct := multipartBody(t, "in.docx", "pdf", docxFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedDocument != repo.createdID || q.enqueuedImage != uuid.Nil {
		t.Fatalf("document enqueue calls=%d document=%s image=%s", q.enqueueCalls, q.enqueuedDocument, q.enqueuedImage)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineDocument {
		t.Fatalf("engine = %+v, want document", repo.created)
	}

	rec = postCreate(t, srv, "in.png", "webp")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedImage != uuid.Nil {
		t.Fatalf("image enqueue calls=%d image=%s", q.enqueueCalls, q.enqueuedImage)
	}
	if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.Engine != convert.EngineImage {
		t.Fatalf("dispatch = %+v, want protocol image", repo.created)
	}
}

func legacyImageCatalog(schema string) *fakeCatalog {
	return &fakeCatalog{
		found: true,
		match: workerproto.Match{
			CapabilityID:      "octoconv.convert.image",
			CapabilityVersion: 1,
			ClientOperation:   convert.OperationConvert,
			LegacyOperation:   convert.OperationConvert,
			LegacyEngine:      convert.EngineImage,
			ParameterSchema:   []byte(schema),
		},
	}
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
