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

func TestCreateJobWithoutCatalogRefusesImage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)

	rec := postCreate(t, srv, "in.png", "webp")
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestCatalogMissRefusesImage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	catalog := &fakeCatalog{found: false}
	srv.SetCapabilityCatalog(catalog)

	rec := postCreate(t, srv, "in.png", "webp")
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	if catalog.calls != 1 {
		t.Fatalf("Lookup calls = %d, want 1", catalog.calls)
	}
	assertNoUploadOrEnqueue(t, repo, store, q)
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

func tectonicMatch() workerproto.Match {
	return workerproto.Match{
		CapabilityID:      "octoconv.convert.tectonic",
		CapabilityVersion: 1,
		ClientOperation:   convert.OperationConvert,
		LegacyOperation:   convert.OperationConvert,
		LegacyEngine:      convert.EngineTectonic,
		ParameterSchema:   []byte(`{}`),
	}
}

func TestApprovedTectonicOverLinkWindowIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	over := time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.engineTimeouts[convert.EngineTectonic] = over
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: tectonicMatch()})

	body, ct := multipartBody(t, "paper.tex", "pdf", texDoc())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestApprovedTectonicProgressWebhookUsesProtocol(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: tectonicMatch()})

	body, ct := multipartBodyWithCallback(t, "paper.tex", "pdf", texDoc(), "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedTectonic != uuid.Nil {
		t.Fatalf("enqueue calls=%d tectonic=%s", q.enqueueCalls, q.enqueuedTectonic)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 271*time.Second {
		t.Fatalf("created = %+v, want protocol tectonic with progress webhook", repo.created)
	}
}

func TestApprovedImageOverLinkWindowIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	over := time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.engineTimeouts = map[string]time.Duration{convert.EngineImage: over}
	srv.SetCapabilityCatalog(legacyImageCatalog(`{}`))

	rec := postCreate(t, srv, "in.png", "webp")
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
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

func TestApprovedHTMLPrintOptionSkipsEnqueue(t *testing.T) {
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
			if q.enqueueCalls != 0 || q.enqueuedHTML != uuid.Nil {
				t.Fatalf("enqueue calls=%d html=%s", q.enqueueCalls, q.enqueuedHTML)
			}
			if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 60*time.Second {
				t.Fatalf("created = %+v, want protocol html", repo.created)
			}
			if len(repo.created.Opts) == 0 {
				t.Fatalf("opts = %#v, want print options", repo.created.Opts)
			}
		})
	}
}

func TestApprovedHTMLProgressWebhookUsesProtocol(t *testing.T) {
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
	if q.enqueueCalls != 0 || q.enqueuedHTML != uuid.Nil {
		t.Fatalf("enqueue calls=%d html=%s", q.enqueueCalls, q.enqueuedHTML)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol html with progress webhook", repo.created)
	}
}

func TestApprovedHTMLOverLinkWindowIsRefused(t *testing.T) {
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
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestUnapprovedHTMLIsRefused(t *testing.T) {
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
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
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

func TestApprovedAudioLanguageOrTranslateSkipsEnqueue(t *testing.T) {
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
			if q.enqueueCalls != 0 || q.enqueuedAudio != uuid.Nil {
				t.Fatalf("enqueue calls=%d audio=%s", q.enqueueCalls, q.enqueuedAudio)
			}
			if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 742*time.Second {
				t.Fatalf("created = %+v, want protocol audio", repo.created)
			}
			if opts == `{"translate":true}` && repo.created.Opts["translate"] != true {
				t.Fatalf("opts = %#v, want translate true", repo.created.Opts)
			}
		})
	}
}

func TestApprovedAudioProgressWebhookUsesProtocol(t *testing.T) {
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
	if q.enqueueCalls != 0 || q.enqueuedAudio != uuid.Nil {
		t.Fatalf("enqueue calls=%d audio=%s", q.enqueueCalls, q.enqueuedAudio)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol audio with progress webhook", repo.created)
	}
}

func TestApprovedAudioOverLinkWindowIsRefused(t *testing.T) {
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
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestUnapprovedAudioIsRefused(t *testing.T) {
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
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
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

func TestApprovedAVCreateSkipsEnqueue(t *testing.T) {
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
	if q.enqueueCalls != 0 || q.enqueuedAV != uuid.Nil {
		t.Fatalf("enqueue calls=%d av=%s", q.enqueueCalls, q.enqueuedAV)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineAV || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol av", repo.created)
	}
	if repo.created.ProtocolAttemptBudget != 753*time.Second {
		t.Fatalf("budget = %s, want 753s", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.CapabilityID != "octoconv.convert.av" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
}

func TestApprovedAVOptionsSkipEnqueue(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		target   string
		opts     string
		data     []byte
		wantKey  string
		wantVal  any
	}{
		{"timecode zero", "in.mp4", "jpg", `{"timecode":0}`, mp4BytesFixture(), "timecode", float64(0)},
		{"height 480 webm", "in.mp4", "webm", `{"resolution_height":480}`, mp4BytesFixture(), "resolution_height", float64(480)},
		{"height 720 webm", "in.mp4", "webm", `{"resolution_height":720}`, mp4BytesFixture(), "resolution_height", float64(720)},
		{"height 1080 webm", "in.mp4", "webm", `{"resolution_height":1080}`, mp4BytesFixture(), "resolution_height", float64(1080)},
		{"height 480 mp4", "in.mkv", "mp4", `{"resolution_height":480}`, mkvBytesFixture(), "resolution_height", float64(480)},
		{"height 720 mp4", "in.mkv", "mp4", `{"resolution_height":720}`, mkvBytesFixture(), "resolution_height", float64(720)},
		{"height 1080 mp4", "in.mkv", "mp4", `{"resolution_height":1080}`, mkvBytesFixture(), "resolution_height", float64(1080)},
		{"codec h264", "in.mkv", "mp4", `{"codec":"h264"}`, mkvBytesFixture(), "codec", "h264"},
		{"codec hevc", "in.mkv", "mp4", `{"codec":"hevc"}`, mkvBytesFixture(), "codec", "hevc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)
			srv.SetCapabilityCatalog(avCatalog())

			body, ct := multipartBodyWithOpts(t, tc.filename, tc.target, tc.data, tc.opts)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if q.enqueueCalls != 0 || q.enqueuedAV != uuid.Nil {
				t.Fatalf("enqueue calls=%d av=%s", q.enqueueCalls, q.enqueuedAV)
			}
			if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 753*time.Second {
				t.Fatalf("created = %+v, want protocol av", repo.created)
			}
			if repo.created.Opts[tc.wantKey] != tc.wantVal {
				t.Fatalf("opts = %#v, want %s=%v", repo.created.Opts, tc.wantKey, tc.wantVal)
			}
		})
	}
}

func TestApprovedAVProgressWebhookUsesProtocol(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(avCatalog())

	body, ct := multipartBodyWithCallback(t, "in.mp4", "mp3", mp4BytesFixture(), "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedAV != uuid.Nil {
		t.Fatalf("enqueue calls=%d av=%s", q.enqueueCalls, q.enqueuedAV)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol av with progress webhook", repo.created)
	}
}

func TestApprovedAVOverLinkWindowIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.engineTimeouts[convert.EngineAV] = time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.SetCapabilityCatalog(avCatalog())

	body, ct := multipartBody(t, "in.mp4", "mp3", mp4BytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestUnapprovedAVIsRefused(t *testing.T) {
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

	body, ct := multipartBody(t, "in.mp4", "mp3", mp4BytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func avCatalog() *fakeCatalog {
	allowed := map[string]struct{}{
		"mp4/mp3":  {},
		"mp4/jpg":  {},
		"mp4/webm": {},
		"mkv/mp4":  {},
		"webm/mp4": {},
	}
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert {
				if _, ok := allowed[source+"/"+target]; ok {
					return workerproto.Match{
						CapabilityID:      "octoconv.convert.av",
						CapabilityVersion: 1,
						ClientOperation:   convert.OperationConvert,
						LegacyOperation:   convert.OperationConvert,
						LegacyEngine:      convert.EngineAV,
						ParameterSchema:   []byte(`{}`),
					}, true
				}
			}
			return workerproto.Match{}, false
		},
	}
}

func TestApprovedOCRCreateSkipsEnqueue(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(ocrCatalog())

	body, ct := multipartBody(t, "in.png", "txt", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedOCR != uuid.Nil {
		t.Fatalf("enqueue calls=%d ocr=%s", q.enqueueCalls, q.enqueuedOCR)
	}
	if repo.created == nil || repo.created.Engine != convert.EngineOCR || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol ocr", repo.created)
	}
	if repo.created.ProtocolAttemptBudget != 744*time.Second {
		t.Fatalf("budget = %s, want 744s", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.ProtocolAttemptBudget > time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second {
		t.Fatalf("budget = %s exceeds the file-link window", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.CapabilityID != "octoconv.convert.ocr" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
}

func TestApprovedOCREmptyLanguageSkipsEnqueue(t *testing.T) {
	for _, opts := range []string{`{}`, `{"language":""}`} {
		t.Run(opts, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)
			srv.SetCapabilityCatalog(ocrCatalog())

			body, ct := multipartBodyWithOpts(t, "in.png", "txt", pngBytesFixture(), opts)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if q.enqueueCalls != 0 || q.enqueuedOCR != uuid.Nil {
				t.Fatalf("enqueue calls=%d ocr=%s", q.enqueueCalls, q.enqueuedOCR)
			}
			if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 744*time.Second {
				t.Fatalf("created = %+v, want protocol ocr", repo.created)
			}
			if len(repo.created.Opts) != 0 {
				t.Fatalf("opts = %#v, want none", repo.created.Opts)
			}
		})
	}
}

func TestApprovedOCRLanguageSkipsEnqueue(t *testing.T) {
	for _, language := range []string{"eng", "rus", "eng+rus"} {
		t.Run(language, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)
			srv.SetCapabilityCatalog(ocrCatalog())

			body, ct := multipartBodyWithOpts(t, "in.png", "txt", pngBytesFixture(), `{"language":"`+language+`"}`)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if q.enqueueCalls != 0 || q.enqueuedOCR != uuid.Nil {
				t.Fatalf("enqueue calls=%d ocr=%s", q.enqueueCalls, q.enqueuedOCR)
			}
			if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 744*time.Second {
				t.Fatalf("created = %+v, want protocol ocr", repo.created)
			}
			if repo.created.Opts["language"] != language {
				t.Fatalf("opts = %#v, want language=%s", repo.created.Opts, language)
			}
		})
	}
}

func TestApprovedOCRProgressWebhookUsesProtocol(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(ocrCatalog())

	body, ct := multipartBodyWithCallback(t, "in.png", "txt", pngBytesFixture(), "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedOCR != uuid.Nil {
		t.Fatalf("enqueue calls=%d ocr=%s", q.enqueueCalls, q.enqueuedOCR)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol ocr with progress webhook", repo.created)
	}
}

func TestApprovedOCROverLinkWindowIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.engineTimeouts[convert.EngineOCR] = time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.SetCapabilityCatalog(ocrCatalog())

	body, ct := multipartBody(t, "in.png", "txt", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestUnapprovedOCRIsRefused(t *testing.T) {
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

	body, ct := multipartBody(t, "in.png", "txt", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func ocrCatalog() *fakeCatalog {
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "png" && target == "txt" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.ocr",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineOCR,
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

func imageCatalog() *fakeCatalog {
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			engine, ok := convert.Default.EngineFor(source, target)
			if operation != convert.OperationConvert || !ok || engine != convert.EngineImage {
				return workerproto.Match{}, false
			}
			return workerproto.Match{
				CapabilityID:      "octoconv.convert.image",
				CapabilityVersion: 1,
				ClientOperation:   convert.OperationConvert,
				LegacyOperation:   convert.OperationConvert,
				LegacyEngine:      convert.EngineImage,
				ParameterSchema:   []byte(`{}`),
			}, true
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

func TestApprovedPandocCreateSkipsEnqueue(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(pandocCatalog())

	body, ct := multipartBody(t, "in.md", "html", markdownFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedPandoc != uuid.Nil {
		t.Fatalf("enqueue calls=%d pandoc=%s", q.enqueueCalls, q.enqueuedPandoc)
	}
	if repo.created == nil || repo.created.Engine != convert.EnginePandoc || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol pandoc", repo.created)
	}
	if repo.created.ProtocolAttemptBudget != 65*time.Second {
		t.Fatalf("budget = %s, want 65s", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.ProtocolAttemptBudget > time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second {
		t.Fatalf("budget = %s exceeds the file-link window", repo.created.ProtocolAttemptBudget)
	}
	if repo.created.CapabilityID != "octoconv.convert.pandoc" || repo.created.CapabilityVersion != 1 {
		t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
	}
}

func TestApprovedPandocEmptyOptionsSkipEnqueue(t *testing.T) {
	for _, opts := range []string{`{}`, `{"standalone":false}`, `{"wrap":""}`, `{"toc":false,"number_sections":false,"section_divs":false}`} {
		t.Run(opts, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)
			srv.SetCapabilityCatalog(pandocCatalog())

			body, ct := multipartBodyWithOpts(t, "in.md", "html", markdownFixture(), opts)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if q.enqueueCalls != 0 || q.enqueuedPandoc != uuid.Nil {
				t.Fatalf("enqueue calls=%d pandoc=%s", q.enqueueCalls, q.enqueuedPandoc)
			}
			if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 65*time.Second {
				t.Fatalf("created = %+v, want protocol pandoc", repo.created)
			}
			if len(repo.created.Opts) != 0 {
				t.Fatalf("opts = %#v, want none", repo.created.Opts)
			}
		})
	}
}

func TestApprovedPandocOptionSkipsEnqueue(t *testing.T) {
	cases := []struct {
		name string
		opts string
		key  string
		want any
	}{
		{name: "standalone", opts: `{"standalone":true}`, key: "standalone", want: true},
		{name: "toc", opts: `{"toc":true}`, key: "toc", want: true},
		{name: "number_sections", opts: `{"number_sections":true}`, key: "number_sections", want: true},
		{name: "section_divs", opts: `{"section_divs":true}`, key: "section_divs", want: true},
		{name: "wrap auto", opts: `{"wrap":"auto"}`, key: "wrap", want: "auto"},
		{name: "wrap none", opts: `{"wrap":"none"}`, key: "wrap", want: "none"},
		{name: "wrap preserve", opts: `{"wrap":"preserve"}`, key: "wrap", want: "preserve"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)
			srv.SetCapabilityCatalog(pandocCatalog())

			body, ct := multipartBodyWithOpts(t, "in.md", "html", markdownFixture(), tc.opts)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if q.enqueueCalls != 0 || q.enqueuedPandoc != uuid.Nil {
				t.Fatalf("enqueue calls=%d pandoc=%s", q.enqueueCalls, q.enqueuedPandoc)
			}
			if repo.created == nil || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 65*time.Second {
				t.Fatalf("created = %+v, want protocol pandoc", repo.created)
			}
			if repo.created.Opts[tc.key] != tc.want {
				t.Fatalf("opts = %#v, want %s=%v", repo.created.Opts, tc.key, tc.want)
			}
		})
	}
}

func TestApprovedPandocProgressWebhookUsesProtocol(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(pandocCatalog())

	body, ct := multipartBodyWithCallback(t, "in.md", "html", markdownFixture(), "https://8.8.8.8/hook", "true")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueueCalls != 0 || q.enqueuedPandoc != uuid.Nil {
		t.Fatalf("enqueue calls=%d pandoc=%s", q.enqueueCalls, q.enqueuedPandoc)
	}
	if repo.created == nil || !repo.created.ProgressWebhook || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("created = %+v, want protocol pandoc with progress webhook", repo.created)
	}
}

func TestApprovedPandocOverLinkWindowIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.engineTimeouts[convert.EnginePandoc] = time.Duration(workerproto.FileLinkMaxTTLSeconds)*time.Second + time.Second
	srv.SetCapabilityCatalog(pandocCatalog())

	body, ct := multipartBody(t, "in.md", "html", markdownFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestUnapprovedPandocIsRefused(t *testing.T) {
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

	body, ct := multipartBody(t, "in.md", "html", markdownFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.UnsupportedConversion)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func TestUnknownPandocWrapIs422(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(pandocCatalog())

	body, ct := multipartBodyWithOpts(t, "in.md", "html", markdownFixture(), `{"wrap":"sideways"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	assertClientError(t, rec, http.StatusUnprocessableEntity, errcode.InvalidOptions)
	assertNoUploadOrEnqueue(t, repo, store, q)
}

func pandocCatalog() *fakeCatalog {
	return &fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && (source == "md" || source == "rst") && target == "html" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.pandoc",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EnginePandoc,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
	}
}
