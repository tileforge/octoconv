package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/errcode"
)

func postCADJobWithOpts(t *testing.T, repo *fakeRepo, store *fakeStorage, q *fakeQueue, opts string) *httptest.ResponseRecorder {
	t.Helper()
	srv, _ := newTestServer(repo, store, q)
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

func TestCreateJobCADQualityStoredNormalized(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	rec := postCADJobWithOpts(t, repo, store, q, "  {\n \"quality\" : \"fine\" }  ")
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 2xx; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil {
		t.Fatal("no job was created")
	}
	if want := map[string]any{"quality": "fine"}; !reflect.DeepEqual(repo.created.Opts, want) {
		t.Errorf("stored opts = %#v, want %#v", repo.created.Opts, want)
	}
}

func postCADRender(t *testing.T, repo *fakeRepo, store *fakeStorage, q *fakeQueue, filename string, data []byte, opts string) *httptest.ResponseRecorder {
	t.Helper()
	srv, _ := newTestServer(repo, store, q)
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
		opts     string
		want     map[string]any
	}{
		{"step", "in.step", "public/LM8UU.step", ` { "size" : 512, "view":"top" }`, map[string]any{"size": float64(512), "view": "top"}},
		{"iges", "in.iges", "synthetic/surfaces_only.iges", `{"quality":"draft"}`, map[string]any{"quality": "draft"}},
		{"stl", "in.stl", "public/nasa-hubble-all-pieces.stl", `{"background":"transparent"}`, map[string]any{"background": "transparent"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			rec := postCADRender(t, repo, store, q, tc.filename, readCADFixture(t, tc.fixture), tc.opts)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if repo.created == nil || repo.created.Operation != "render" || repo.created.TargetFormat != "png" || repo.created.Engine != "cad" {
				t.Fatalf("created = %+v, want a cad render job targeting png", repo.created)
			}
			if !reflect.DeepEqual(repo.created.Opts, tc.want) {
				t.Errorf("stored opts = %#v, want %#v", repo.created.Opts, tc.want)
			}
			if q.enqueueCalls != 1 {
				t.Errorf("enqueueCalls = %d, want 1", q.enqueueCalls)
			}
		})
	}
}
