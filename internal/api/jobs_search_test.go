package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/tileforge/octoconv/internal/jobs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestJobSearchValidationAndEcho(t *testing.T) {
	cases := []struct {
		name, query, sort string
		status            int
	}{
		{name: "default", status: 200}, {name: "trimmed", query: " Report ", sort: "oldest", status: 200},
		{name: "invalid sort", sort: "name", status: 422}, {name: "long query", query: strings.Repeat("x", 257), status: 422},
		{name: "NUL", query: "a\x00b", status: 422},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{listResult: []jobs.JobSummary{{ID: uuid.New(), Status: "queued", SourceFilename: "Report.pdf"}}}
			srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
			query := url.Values{"q": {tc.query}, "sort": {tc.sort}}
			req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?"+query.Encode(), nil))
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if tc.status != 200 {
				if repo.listClientID != uuid.Nil {
					t.Fatal("invalid query reached repository")
				}
				return
			}
			var response struct {
				Query struct{ Q, Sort string }
				Jobs  []struct {
					Filename string `json:"source_filename"`
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Query.Q != strings.TrimSpace(tc.query) || response.Query.Sort != repo.listFilter.Sort {
				t.Fatalf("query echo: %+v", response.Query)
			}
			if response.Jobs[0].Filename != "Report.pdf" {
				t.Fatal("historical filename omitted")
			}
		})
	}
}
func TestJobSearchRejectsCursorContextMismatch(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	cursor := jobs.EncodeCursor(jobs.Cursor{CreatedAt: time.Now().UTC(), ID: uuid.New(), Query: "one", Sort: "oldest"})
	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?q=two&sort=oldest&cursor="+cursor, nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != 400 || repo.listClientID != uuid.Nil {
		t.Fatalf("cursor mismatch: status=%d repo=%v", rec.Code, repo.listClientID)
	}
}
