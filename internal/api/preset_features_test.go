package api

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tileforge/octoconv/internal/presets"
)

type versionedPresetFake struct {
	fakePresetAdmin
	mutation    presets.MutationParams
	mutationErr error
	name        string
}

func (f *versionedPresetFake) UpdateWithVersion(_ context.Context, p presets.MutationParams) (int, error) {
	f.mutation = p
	return 2, f.mutationErr
}
func (f *versionedPresetFake) DeactivateWithVersion(_ context.Context, _ string, _ *uuid.UUID, _ string, _ *int) error {
	return f.mutationErr
}
func (f *versionedPresetFake) GetForClient(_ context.Context, _ *uuid.UUID, name string) (*presets.Preset, error) {
	f.name = name
	return f.getForClientResult, f.getForClientErr
}
func TestPresetCapabilitiesAndVersionConflicts(t *testing.T) {
	for _, modern := range []bool{false, true} {
		srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
		fake := &versionedPresetFake{mutationErr: presets.ErrVersionConflict}
		if modern {
			srv.presetAdmin = fake
		}
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("GET", "/v1/presets/", nil)))
		if modern && rec.Header().Get("X-Octoconv-Presets") == "" {
			t.Fatal("missing capability")
		}
		if !modern && rec.Header().Get("X-Octoconv-Presets") != "" {
			t.Fatal("false capability")
		}
		for _, method := range []string{"PUT", "DELETE"} {
			request := authed(httptest.NewRequest(method, "/v1/presets/named", strings.NewReader(`{"operation":"render","target_format":"png","expected_version":1}`)))
			if method == "DELETE" {
				request.Header.Set("If-Match", `"1"`)
			}
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, request)
			want := 501
			if modern {
				want = 409
			}
			if rec.Code != want {
				t.Fatalf("modern %v %s status %d body %s", modern, method, rec.Code, rec.Body.String())
			}
		}
	}
}
func TestPresetCreateOperationAndEncodedName(t *testing.T) {
	row := &presets.Preset{Name: "set/100% ready", Operation: "render", Scope: "user", Version: 1, TargetFormat: "png"}
	fake := &versionedPresetFake{fakePresetAdmin: fakePresetAdmin{getForClientResult: row}}
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	srv.presetAdmin = fake
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/presets/", strings.NewReader(`{"name":"render","operation":"render","target_format":"png","options":{"size":512}}`))))
	if rec.Code != 201 || fake.lastCreateParams == nil || fake.lastCreateParams.Operation != "render" {
		t.Fatalf("status %d params %+v", rec.Code, fake.lastCreateParams)
	}
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("GET", "/v1/presets/"+url.PathEscape(row.Name), nil)))
	if rec.Code != 200 || fake.name != row.Name {
		t.Fatalf("name %q status %d", fake.name, rec.Code)
	}
}

func TestPresetManagementRequiresVersionAndCapability(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	request := func(method, path, body string) int {
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(method, path, strings.NewReader(body))))
		return rec.Code
	}
	if code := request("POST", "/v1/preset-settings/", `{"name":"a","target_format":"pdf"}`); code != 501 {
		t.Fatalf("old adapter %d", code)
	}
	srv.presetAdmin = &versionedPresetFake{}
	if code := request("PUT", "/v1/preset-settings/a", `{"target_format":"pdf"}`); code != 400 {
		t.Fatalf("missing update version %d", code)
	}
	if code := request("DELETE", "/v1/preset-settings/a", ""); code != 400 {
		t.Fatalf("missing delete version %d", code)
	}
}
