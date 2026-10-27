package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

func nonCADEngines(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, e := range convert.AllEngines() {
		if e != convert.EngineCAD {
			out = append(out, e)
		}
	}
	if len(out) != 9 {
		t.Fatalf("non-CAD engines = %q (%d), want 9", out, len(out))
	}
	return out
}

func withStage(j *jobs.Job, stage string, idx, count int) *jobs.Job {
	j.Stage, j.StageIndex, j.StageCount = &stage, &idx, &count
	return j
}

func TestNonCADClassesKeepTheirGetBody(t *testing.T) {
	for _, e := range nonCADEngines(t) {
		t.Run(e, func(t *testing.T) {
			t.Run("done", func(t *testing.T) {
				id := uuid.MustParse("00000000-0000-0000-0000-000000000057")
				repo := &fakeRepo{}
				store := &fakeStorage{presigned: "https://s3.example/results/pinned/0-out.webp?sig=abc&x=1"}
				srv, resolver := newTestServer(repo, store, &fakeQueue{})
				repo.getJob = withStage(&jobs.Job{ID: id, ClientID: resolver.client.ID, Engine: e, Status: jobs.StatusDone}, "mesh", 2, 4)
				repo.outputs = []jobs.Output{{
					Ordinal:     0,
					ObjectKey:   "results/pinned/0-out.webp",
					Filename:    "out.webp",
					Format:      "webp",
					SizeBytes:   4242,
					ContentType: "image/webp",
					Role:        "primary",
				}}
				rec := httptest.NewRecorder()
				srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil)))
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
				}
				assertGolden(t, "job_done_single_output.golden.json", rec.Body.Bytes())
			})
			t.Run("active", func(t *testing.T) {
				repo := &fakeRepo{}
				srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
				id := uuid.New()
				repo.getJob = withStage(&jobs.Job{ID: id, ClientID: resolver.client.ID, Engine: e, Status: jobs.StatusActive}, "mesh", 2, 4)
				rec := httptest.NewRecorder()
				srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil)))
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
				}
				var keys []string
				for k := range body {
					keys = append(keys, k)
				}
				slices.Sort(keys)
				if want := []string{"eta_seconds", "job_id", "progress_pct", "status"}; !slices.Equal(keys, want) {
					t.Fatalf("keys = %q, want %q; body=%s", keys, want, rec.Body.String())
				}
			})
		})
	}
}

func TestNonCADClassesKeepTheirSSEFrames(t *testing.T) {
	want := "event: progress\n" +
		`data: {"status":"active","progress_pct":42,"eta_seconds":18}` + "\n\n" +
		"event: progress\n" +
		`data: {"status":"done","progress_pct":42,"eta_seconds":18}` + "\n\n" +
		"event: progress\n" +
		`data: {"status":"done"}` + "\n\n"
	for _, e := range nonCADEngines(t) {
		t.Run(e, func(t *testing.T) {
			job := func(status, stage string, idx int) *jobs.Job {
				return withStage(&jobs.Job{
					ID: stageJobID, ClientID: stageClient.ID, Engine: e, Status: status,
					ProgressPct: intPtr(42), EtaSeconds: intPtr(18),
				}, stage, idx, 4)
			}
			body, _ := runCADStream(t,
				job(jobs.StatusActive, "mesh", 2),
				job(jobs.StatusActive, "render", 3),
				job(jobs.StatusDone, "judge", 4),
			)
			if body != want {
				t.Fatalf("body:\n  got  %q\n  want %q", body, want)
			}
		})
	}
}
