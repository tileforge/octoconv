package api

import (
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

func TestCreateJobRenderStaysUnregistered(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	body, ct := multipartBodyWithOperation(t, "in.step", "png", "render", readCADFixture(t, "public/LM8UU.step"))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), errcode.UnsupportedOperation) {
		t.Fatalf("render before registration = %d %s, want 422 unsupported_operation", rec.Code, rec.Body.String())
	}
	if repo.created != nil || store.uploadCalls != 0 || q.enqueueCalls != 0 {
		t.Error("an unregistered render must not upload, create or enqueue")
	}
}
