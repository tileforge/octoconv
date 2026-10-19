package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

func TestGetJobCADStageKeys(t *testing.T) {
	stage := "mesh"
	index, count := 2, 3
	cases := []struct {
		name   string
		engine string
		status string
		stage  *string
		index  *int
		count  *int
		want   map[string]string
	}{
		{"cad queued", convert.EngineCAD, jobs.StatusQueued, nil, nil, nil,
			map[string]string{"stage": "null", "stage_index": "null", "stage_count": "null"}},
		{"cad active", convert.EngineCAD, jobs.StatusActive, &stage, &index, &count,
			map[string]string{"stage": `"mesh"`, "stage_index": "2", "stage_count": "3", "progress_pct": "null"}},
		{"image active", convert.EngineImage, jobs.StatusActive, &stage, &index, &count, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
			id := uuid.New()
			repo.getJob = &jobs.Job{
				ID: id, ClientID: resolver.client.ID, Engine: c.engine, Status: c.status,
				Stage: c.stage, StageIndex: c.index, StageCount: c.count,
			}
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil)))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
			}
			if c.want == nil {
				for _, k := range []string{"stage", "stage_index", "stage_count"} {
					if v, ok := body[k]; ok {
						t.Errorf("%s job carries %q = %s, want no such key", c.engine, k, v)
					}
				}
				return
			}
			for k, want := range c.want {
				got, ok := body[k]
				if !ok {
					t.Errorf("key %q missing; body=%s", k, rec.Body.String())
					continue
				}
				if string(got) != want {
					t.Errorf("%s = %s, want %s", k, got, want)
				}
			}
		})
	}
}
