package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

func newSpillServer(t *testing.T, maxUpload int64) *Server {
	t.Helper()
	return NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        maxUpload,
		InspectMaxUploadBytes: maxUpload,
		InspectMaxWorkBytes:   maxUpload,
		MaxFormatBytes:        map[string]map[string]int64{convert.EnginePandoc: {"md": maxUpload}},
	})
}

func spillFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "multipart-*"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func requireParsed(t *testing.T, code int, body string) {
	t.Helper()
	if strings.Contains(body, "invalid multipart form") || strings.Contains(body, "file exceeds size limit") {
		t.Fatalf("the form never parsed: %d %s", code, body)
	}
}

func spillRequest(t *testing.T, target, path string, size int) *http.Request {
	t.Helper()
	body, ct := multipartBody(t, "in.md", target, paddedMarkdown(size))
	req := authed(httptest.NewRequest(http.MethodPost, path, body))
	req.Header.Set("Content-Type", ct)
	return req
}

func serveDirect(t *testing.T, srv *Server, req *http.Request) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func serveReal(t *testing.T, srv *Server, target, path string, size int) (int, string) {
	t.Helper()
	ts := httptest.NewServer(srv.Routes())
	body, ct := multipartBody(t, "in.md", target, paddedMarkdown(size))
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ct)
	resp, err := http.DefaultClient.Do(authed(req))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	ts.Close()
	return resp.StatusCode, string(raw)
}

func TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns(t *testing.T) {
	size := multipartInMemoryBudget + 1<<20
	for _, tc := range []struct {
		name, target, path string
		real               bool
	}{
		{"create through the real server", "html", "/v1/jobs", true},
		{"create through ServeHTTP", "html", "/v1/jobs", false},
		{"inspect through the real server", "", "/v1/inspect", true},
		{"inspect through ServeHTTP", "", "/v1/inspect", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			srv := newSpillServer(t, 64<<20)
			var code int
			var body string
			if tc.real {
				code, body = serveReal(t, srv, tc.target, tc.path, size)
			} else {
				code, body = serveDirect(t, srv, spillRequest(t, tc.target, tc.path, size))
			}
			requireParsed(t, code, body)
			if left := spillFiles(t, dir); len(left) != 0 {
				t.Errorf("%d multipart spill files outlived the handler: %v", len(left), left)
			}
		})
	}
	t.Run("a malformed query with the form set", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("TMPDIR", dir)
		srv := newSpillServer(t, 64<<20)
		code, body := serveDirect(t, srv, spillRequest(t, "html", "/v1/jobs?%zz", size))
		if code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 for a malformed query; body=%s", code, body)
		}
		if left := spillFiles(t, dir); len(left) != 0 {
			t.Errorf("%d multipart spill files outlived the handler: %v", len(left), left)
		}
	})
	t.Run("over the upload limit", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("TMPDIR", dir)
		srv := newSpillServer(t, multipartInMemoryBudget+4<<20)
		code, body := serveDirect(t, srv, spillRequest(t, "html", "/v1/jobs", multipartInMemoryBudget+8<<20))
		if code != http.StatusRequestEntityTooLarge || !strings.Contains(body, "file exceeds size limit") {
			t.Errorf("status = %d, body=%s, want 413 file exceeds size limit", code, body)
		}
		if left := spillFiles(t, dir); len(left) != 0 {
			t.Errorf("%d multipart spill files outlived the handler: %v", len(left), left)
		}
	})
}
