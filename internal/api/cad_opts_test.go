package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
)

func postCADJobWithOpts(t *testing.T, repo *fakeRepo, store *fakeStorage, q *fakeQueue, opts string) *httptest.ResponseRecorder {
	t.Helper()
	srv, _ := newTestServer(repo, store, q)
	admitCADCatalog(srv)
	body, ct := multipartBodyWithOpts(t, "in.step", "stl", readCADFixture(t, "public/LM8UU.step"), opts)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestCreateJobCADOptsRejections(t *testing.T) {
	cases := []struct {
		name string
		opts string
		code string
	}{
		{"unknown value", `{"quality":"ultra"}`, errcode.InvalidOptions},
		{"unknown key", `{"scale":2}`, errcode.InvalidOptions},
		{"bad view value", `{"view":"diagonal"}`, errcode.InvalidOptions},
		{"fractional size", `{"size":1280.5}`, errcode.InvalidOptions},
		{"string size", `{"size":"1280"}`, errcode.InvalidOptions},
		{"zero size", `{"size":0}`, errcode.InvalidOptions},
		{"duplicate key", `{"quality":"fine","quality":"draft"}`, errcode.InvalidOptions},
		{"trailing bytes", `{"quality":"fine"} {}`, errcode.InvalidOptions},
		{"view on convert", `{"view":"iso"}`, errcode.OptsNotApplicable},
		{"size on convert", `{"size":512}`, errcode.OptsNotApplicable},
		{"background on convert", `{"background":"transparent"}`, errcode.OptsNotApplicable},
		{"unit on convert", `{"assume_unit":"inch"}`, errcode.OptsNotApplicable},
		{"dimensions on convert", `{"dimensions":true}`, errcode.OptsNotApplicable},
		{"dimensions false on convert", `{"dimensions":false}`, errcode.OptsNotApplicable},
		{"dimensions string", `{"dimensions":"true"}`, errcode.InvalidOptions},
		{"dimensions null", `{"dimensions":null}`, errcode.InvalidOptions},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			rec := postCADJobWithOpts(t, repo, store, q, tc.opts)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("body = %s, want code %s", rec.Body.String(), tc.code)
			}
			if store.uploaded || store.uploadCalls != 0 {
				t.Error("rejected opts must not reach storage.Upload")
			}
			if repo.created != nil {
				t.Error("rejected opts must not create a job")
			}
			if q.enqueueCalls != 0 {
				t.Errorf("enqueueCalls = %d, want 0", q.enqueueCalls)
			}
		})
	}
}

func TestCreateJobCADDimensionsAreRefused(t *testing.T) {
	for _, raw := range []string{`{"dimensions":false}`, `{"dimensions":true}`} {
		repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
		rec := postCADRender(t, repo, store, q, "in.step", readCADFixture(t, "public/LM8UU.step"), raw)
		assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.OptsNotApplicable)
	}
}

func TestCreateJobCADQualityIsRefused(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	rec := postCADJobWithOpts(t, repo, store, q, "  {\n \"quality\" : \"fine\" }  ")
	assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.OptsNotApplicable)
}

func postCADRender(t *testing.T, repo *fakeRepo, store *fakeStorage, q *fakeQueue, filename string, data []byte, opts string) *httptest.ResponseRecorder {
	t.Helper()
	srv, _ := newTestServer(repo, store, q)
	admitCADCatalog(srv)
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = w.WriteField("target", "png")
	_ = w.WriteField("operation", "render")
	if opts != "" {
		_ = w.WriteField("opts", opts)
	}
	_ = w.Close()
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", &b))
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func assertRenderRefusedBeforeAnySideEffect(t *testing.T, rec *httptest.ResponseRecorder, repo *fakeRepo, store *fakeStorage, q *fakeQueue, code string) {
	t.Helper()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
		t.Errorf("body = %s, want code %s", rec.Body.String(), code)
	}
	if store.uploaded || store.uploadCalls != 0 || repo.created != nil || q.enqueueCalls != 0 {
		t.Errorf("a refused render must not upload, create or enqueue: uploadCalls=%d created=%v enqueueCalls=%d", store.uploadCalls, repo.created != nil, q.enqueueCalls)
	}
}

func TestCreateJobRenderInvalidViewIsInvalidOptions(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	rec := postCADRender(t, repo, store, q, "in.step", readCADFixture(t, "public/LM8UU.step"), `{"view":"diagonal"}`)
	assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.InvalidOptions)
}

func TestCreateJobRenderOfAnSTLWithQualityIsOptsNotApplicable(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	rec := postCADRender(t, repo, store, q, "in.stl", readCADFixture(t, "public/nasa-hubble-all-pieces.stl"), `{"quality":"fine"}`)
	assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.OptsNotApplicable)
}

func TestCreateJobRenderIsAcceptedForEachCADSource(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		fixture  string
		source   string
	}{
		{"step", "in.step", "public/LM8UU.step", "step"},
		{"iges", "in.iges", "synthetic/surfaces_only.iges", "iges"},
		{"stl", "in.stl", "public/nasa-hubble-all-pieces.stl", "stl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			rec := postCADRender(t, repo, store, q, tc.filename, readCADFixture(t, tc.fixture), "")
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			assertAdmittedCAD(t, repo, q, convert.OperationRender, tc.source, "png", "octoconv.render.cad")
			if store.uploadCalls != 1 {
				t.Errorf("uploadCalls = %d, want 1", store.uploadCalls)
			}
		})
	}
}

func TestCreateJobRenderOptionsAreRefused(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		fixture  string
		opts     string
	}{
		{"step", "in.step", "public/LM8UU.step", ` { "size" : 512, "view":"top" }`},
		{"iges", "in.iges", "synthetic/surfaces_only.iges", `{"quality":"draft"}`},
		{"stl", "in.stl", "public/nasa-hubble-all-pieces.stl", `{"background":"transparent"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			rec := postCADRender(t, repo, store, q, tc.filename, readCADFixture(t, tc.fixture), tc.opts)
			assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.OptsNotApplicable)
		})
	}
}

func postDXFJob(t *testing.T, repo *fakeRepo, store *fakeStorage, q *fakeQueue, target, operation, opts string) *httptest.ResponseRecorder {
	t.Helper()
	srv, _ := newTestServer(repo, store, q)
	admitCADCatalog(srv)
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", "drawing.dxf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(readCADFixture(t, "synthetic/legacy_cp1251.dxf"))
	_ = w.WriteField("target", target)
	if operation != "" {
		_ = w.WriteField("operation", operation)
	}
	if opts != "" {
		_ = w.WriteField("opts", opts)
	}
	_ = w.Close()
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", &b))
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestCreateJobDXFRenderViewOrQualityIsOptsNotApplicable(t *testing.T) {
	for _, opts := range []string{
		`{"view":"top"}`,
		`{"quality":"fine"}`,
		`{"size":512,"view":"iso"}`,
		`{"background":"transparent","quality":"draft"}`,
		`{"dimensions":true}`,
		`{"dimensions":false}`,
	} {
		t.Run(opts, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			rec := postDXFJob(t, repo, store, q, "png", "render", opts)
			assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.OptsNotApplicable)
		})
	}
}

func TestCreateJobDXFRenderInvalidViewIsInvalidOptions(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	rec := postDXFJob(t, repo, store, q, "png", "render", `{"view":"sideways"}`)
	assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.InvalidOptions)
}

func TestCreateJobDXFConvertWithAnyOptionIsOptsNotApplicable(t *testing.T) {
	cases := []struct {
		target string
		opts   string
	}{
		{"svg", `{"size":1024}`},
		{"svg", `{"quality":"fine"}`},
		{"svg", `{"assume_unit":"mm"}`},
		{"pdf", `{"background":"transparent"}`},
		{"pdf", `{"view":"top"}`},
		{"pdf", `{"quality":"draft"}`},
	}
	for _, tc := range cases {
		t.Run(tc.target+" "+tc.opts, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			rec := postDXFJob(t, repo, store, q, tc.target, "", tc.opts)
			assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.OptsNotApplicable)
		})
	}
}

func TestCreateJobDXFConvertWithoutOptionsUsesProtocol(t *testing.T) {
	for _, target := range []string{"pdf", "svg"} {
		t.Run(target, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			rec := postDXFJob(t, repo, store, q, target, "", "")
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			assertAdmittedCAD(t, repo, q, convert.OperationConvert, "dxf", target, "octoconv.convert.caddraw")
			if store.uploadCalls != 1 {
				t.Errorf("uploadCalls = %d, want 1", store.uploadCalls)
			}
		})
	}
}

func TestCreateJobDXFRenderWithoutOptionsUsesProtocol(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	rec := postDXFJob(t, repo, store, q, "png", "render", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	assertAdmittedCAD(t, repo, q, convert.OperationRender, "dxf", "png", "octoconv.render.caddraw")
	if store.uploadCalls != 1 {
		t.Errorf("uploadCalls = %d, want 1", store.uploadCalls)
	}
}

func TestCreateJobDXFRenderWithSizeBackgroundAndUnitIsRefused(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	rec := postDXFJob(t, repo, store, q, "png", "render", " {\n \"assume_unit\" : \"mm\", \"background\":\"transparent\",  \"size\": 2048 } ")
	assertRenderRefusedBeforeAnySideEffect(t, rec, repo, store, q, errcode.OptsNotApplicable)
}
