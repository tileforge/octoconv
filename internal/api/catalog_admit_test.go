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

func admitCADCatalog(srv *Server) {
	srv.SetCapabilityCatalog(&fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			id := "octoconv.convert.cad"
			switch source {
			case "step", "iges", "stl":
			case "dxf", "dwg":
				id = "octoconv.convert.caddraw"
			default:
				return workerproto.Match{}, false
			}
			if operation == convert.OperationRender {
				id = "octoconv.render.cad"
				if source == "dxf" || source == "dwg" {
					id = "octoconv.render.caddraw"
				}
			}
			return cadMatch(id, operation), true
		},
	})
}

func assertAdmittedCAD(t *testing.T, repo *fakeRepo, q *fakeQueue, operation, source, target, pin string) {
	t.Helper()
	if repo.created == nil || repo.created.Engine != convert.EngineCAD || repo.created.Operation != operation ||
		repo.created.SourceFormat != source || repo.created.TargetFormat != target {
		t.Fatalf("created = %+v, want cad %s %s -> %s", repo.created, operation, source, target)
	}
	if repo.created.CapabilityID != pin || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d, want %s v1", repo.created.CapabilityID, repo.created.CapabilityVersion, pin)
	}
	if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != convert.CADDefaultEngineTimeout {
		t.Fatalf("dispatch = %q budget = %s", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if len(repo.created.Opts) != 0 {
		t.Errorf("stored opts = %#v, want none", repo.created.Opts)
	}
	if q.enqueueCalls != 0 || q.enqueuedCAD != uuid.Nil {
		t.Errorf("enqueueCalls = %d enqueuedCAD = %s, want no enqueue", q.enqueueCalls, q.enqueuedCAD)
	}
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

func TestApprovedCADOverLinkWindowIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	over := time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.engineTimeouts = map[string]time.Duration{convert.EngineCAD: over}
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: cadMatch("octoconv.convert.cad", convert.OperationConvert)})

	rec := postCAD(t, srv, "part.step", "stl", "", readCADFixture(t, "synthetic/units_inch.step"))
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
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

func TestApprovedCADProgressWebhookStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: cadMatch("octoconv.convert.cad", convert.OperationConvert)})
	data := readCADFixture(t, "synthetic/units_inch.step")
	body, ct := multipartBodyWithCallback(t, "part.step", "stl", data, "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestApprovedCADCallbackWithoutProgressWebhookUsesProtocol(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: cadMatch("octoconv.convert.cad", convert.OperationConvert)})
	data := readCADFixture(t, "synthetic/units_inch.step")
	body, ct := multipartBodyWithCallback(t, "part.step", "stl", data, "https://8.8.8.8/hook", "")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.ProgressWebhook || repo.created.CallbackURL != "https://8.8.8.8/hook" {
		t.Fatalf("create params = %+v, want callback without progress webhook", repo.created)
	}
	if repo.created.DispatchOwner != "protocol" || repo.created.CapabilityID != "octoconv.convert.cad" {
		t.Fatalf("dispatch = %q pin = %q, want protocol", repo.created.DispatchOwner, repo.created.CapabilityID)
	}
	if q.enqueueCalls != 0 {
		t.Fatalf("enqueueCalls = %d, want 0", q.enqueueCalls)
	}
}

func TestApprovedCADRenderSkipsEnqueue(t *testing.T) {
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
	if repo.created.CapabilityID != "octoconv.render.cad" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
	if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != convert.CADDefaultEngineTimeout {
		t.Fatalf("dispatch = %q budget = %s", repo.created.DispatchOwner, repo.created.ProtocolAttemptBudget)
	}
	if q.enqueueCalls != 0 || q.enqueuedCAD != uuid.Nil {
		t.Fatalf("enqueueCalls = %d enqueuedCAD = %s, want no enqueue", q.enqueueCalls, q.enqueuedCAD)
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
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.OptsNotApplicable)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestCatalogMissRefusesCAD(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{found: false})

	rec := postCAD(t, srv, "part.step", "stl", "", readCADFixture(t, "synthetic/units_inch.step"))
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
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

func TestUnapprovedDocumentIsRefused(t *testing.T) {
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
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
	if q.enqueuedDocument != uuid.Nil {
		t.Fatalf("enqueuedDocument = %s, want none", q.enqueuedDocument)
	}

	rec = postCreate(t, srv, "in.png", "webp")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedImage != uuid.Nil {
		t.Fatalf("image enqueue calls=%d image=%s", q.enqueueCalls, q.enqueuedImage)
	}
	if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.Engine != convert.EngineImage {
		t.Fatalf("dispatch = %+v, want protocol image", repo.created)
	}
}

func TestApprovedDocumentCreateSkipsEnqueue(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(documentCatalog())

	body, ct := multipartBody(t, "in.docx", "pdf", docxFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedDocument != uuid.Nil {
		t.Fatalf("enqueue calls=%d document=%s", q.enqueueCalls, q.enqueuedDocument)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineDocument || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol document", repo.created)
	}
	if repo.created.ProtocolAttemptBudget != 300*time.Second {
		t.Fatalf("budget = %s, want 300s", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.CapabilityID != "octoconv.convert.document" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
}

func TestDocumentPDFProfileIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(documentCatalog())

	body, ct := multipartBodyWithOpts(t, "in.docx", "pdf", docxFixture(t), `{"pdf_profile":"pdf/a-2b"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.OptsNotApplicable)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestDocumentProgressWebhookIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(documentCatalog())

	body, ct := multipartBodyWithCallback(t, "in.docx", "pdf", docxFixture(t), "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.InvalidRequest)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestApprovedDocumentOverLinkWindowIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.engineTimeouts = map[string]time.Duration{
		convert.EngineDocument: time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second,
	}
	srv.SetCapabilityCatalog(documentCatalog())

	body, ct := multipartBody(t, "in.docx", "pdf", docxFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestApprovedHTMLCreateSkipsEnqueue(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(htmlCatalog())

	body, ct := multipartBody(t, "in.html", "pdf", htmlFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedHTML != uuid.Nil {
		t.Fatalf("enqueue calls=%d html=%s", q.enqueueCalls, q.enqueuedHTML)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineHTML || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol html", repo.created)
	}
	if repo.created.ProtocolAttemptBudget != 60*time.Second {
		t.Fatalf("budget = %s, want 60s", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.CapabilityID != "octoconv.convert.html" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
}

func TestApprovedHTMLPrintOptionStaysOnAsynq(t *testing.T) {
	for _, opts := range []string{
		`{"page_size":"a4"}`,
		`{"margin_mm":10}`,
		`{"landscape":true}`,
		`{"print_background":true}`,
	} {
		t.Run(opts, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)
			srv.SetCapabilityCatalog(htmlCatalog())

			body, ct := multipartBodyWithOpts(t, "in.html", "pdf", htmlFixture(), opts)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if q.enqueueCalls != 1 || q.enqueuedHTML != repo.createdID {
				t.Fatalf("enqueue calls=%d html=%s", q.enqueueCalls, q.enqueuedHTML)
			}
			if repo.created == nil || repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
				t.Fatalf("created = %+v, want html on asynq", repo.created)
			}
		})
	}
}

func TestApprovedHTMLProgressWebhookStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(htmlCatalog())

	body, ct := multipartBodyWithCallback(t, "in.html", "pdf", htmlFixture(), "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedHTML != repo.createdID {
		t.Fatalf("enqueue calls=%d html=%s", q.enqueueCalls, q.enqueuedHTML)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "" {
		t.Fatalf("created = %+v, want webhook on asynq", repo.created)
	}
}

func TestApprovedHTMLOverLinkWindowStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.engineTimeouts[convert.EngineHTML] = time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.SetCapabilityCatalog(htmlCatalog())

	body, ct := multipartBody(t, "in.html", "pdf", htmlFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedHTML != repo.createdID {
		t.Fatalf("enqueue calls=%d html=%s", q.enqueueCalls, q.enqueuedHTML)
	}
	if repo.created == nil || repo.created.DispatchOwner != "" {
		t.Fatalf("created = %+v, want html on asynq", repo.created)
	}
}

func TestUnapprovedHTMLStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{
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
	})

	body, ct := multipartBody(t, "in.html", "pdf", htmlFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedHTML != repo.createdID {
		t.Fatalf("enqueue calls=%d html=%s", q.enqueueCalls, q.enqueuedHTML)
	}
	if repo.created == nil || repo.created.DispatchOwner != "" || repo.created.CapabilityID != "" {
		t.Fatalf("created = %+v, want unpinned html on asynq", repo.created)
	}
}

func htmlCatalog() *fakeCatalog {
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "html" && target == "pdf" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.html",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineHTML,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
	}
}

func TestApprovedAudioCreateSkipsEnqueue(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(audioCatalog())

	body, ct := multipartBody(t, "in.mp3", "txt", audioMP3Fixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedAudio != uuid.Nil {
		t.Fatalf("enqueue calls=%d audio=%s", q.enqueueCalls, q.enqueuedAudio)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineAudio || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol audio", repo.created)
	}
	if repo.created.ProtocolAttemptBudget != 742*time.Second {
		t.Fatalf("budget = %s, want 742s", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.CapabilityID != "octoconv.convert.audio" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
}

func TestApprovedAudioLanguageOrTranslateStaysOnAsynq(t *testing.T) {
	for _, opts := range []string{
		`{"language":"auto"}`,
		`{"language":"en"}`,
		`{"language":"ru"}`,
		`{"language":"es"}`,
		`{"language":"fr"}`,
		`{"language":"de"}`,
		`{"translate":true}`,
	} {
		t.Run(opts, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)
			srv.SetCapabilityCatalog(audioCatalog())

			body, ct := multipartBodyWithOpts(t, "in.mp3", "txt", audioMP3Fixture(t), opts)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if q.enqueueCalls != 1 || q.enqueuedAudio != repo.createdID {
				t.Fatalf("enqueue calls=%d audio=%s", q.enqueueCalls, q.enqueuedAudio)
			}
			if repo.created == nil || repo.created.DispatchOwner != "" || repo.created.ProtocolAttemptBudget != 0 {
				t.Fatalf("created = %+v, want audio on asynq", repo.created)
			}
			if opts == `{"translate":true}` && repo.created.Opts["translate"] != true {
				t.Fatalf("opts = %#v, want translate true", repo.created.Opts)
			}
		})
	}
}

func TestApprovedAudioProgressWebhookStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(audioCatalog())

	body, ct := multipartBodyWithCallback(t, "in.mp3", "txt", audioMP3Fixture(t), "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedAudio != repo.createdID {
		t.Fatalf("enqueue calls=%d audio=%s", q.enqueueCalls, q.enqueuedAudio)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "" {
		t.Fatalf("created = %+v, want webhook on asynq", repo.created)
	}
}

func TestApprovedAudioOverLinkWindowStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.engineTimeouts[convert.EngineAudio] = time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.SetCapabilityCatalog(audioCatalog())

	body, ct := multipartBody(t, "in.mp3", "txt", audioMP3Fixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedAudio != repo.createdID {
		t.Fatalf("enqueue calls=%d audio=%s", q.enqueueCalls, q.enqueuedAudio)
	}
	if repo.created == nil || repo.created.DispatchOwner != "" {
		t.Fatalf("created = %+v, want audio on asynq", repo.created)
	}
}

func TestUnapprovedAudioStaysOnAsynq(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{
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
	})

	body, ct := multipartBody(t, "in.mp3", "txt", audioMP3Fixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedAudio != repo.createdID {
		t.Fatalf("enqueue calls=%d audio=%s", q.enqueueCalls, q.enqueuedAudio)
	}
	if repo.created == nil || repo.created.DispatchOwner != "" || repo.created.CapabilityID != "" {
		t.Fatalf("created = %+v, want unpinned audio on asynq", repo.created)
	}
}

func audioCatalog() *fakeCatalog {
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "mp3" && target == "txt" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.audio",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineAudio,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
	}
}

func TestApprovedAVCreateStillEnqueues(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(avCatalog())

	body, ct := multipartBody(t, "in.mp4", "mp3", mp4BytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 1 || q.enqueuedAV != repo.createdID {
		t.Fatalf("enqueue calls=%d av=%s", q.enqueueCalls, q.enqueuedAV)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineAV || repo.created.DispatchOwner != "" {
		t.Fatalf("created = %+v, want av on asynq", repo.created)
	}
	if repo.created.CapabilityID != "octoconv.convert.av" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
}

func avCatalog() *fakeCatalog {
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "mp4" && target == "mp3" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.av",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineAV,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
	}
}

func documentCatalog() *fakeCatalog {
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "docx" && target == "pdf" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.document",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineDocument,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
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
