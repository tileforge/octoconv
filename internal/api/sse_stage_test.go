package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

var (
	stageClient = &clients.Client{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000c1")}
	stageJobID  = uuid.MustParse("00000000-0000-0000-0000-000000000077")
)

func cadJob(status string, stage string, idx, count int) *jobs.Job {
	j := &jobs.Job{
		ID:           stageJobID,
		ClientID:     stageClient.ID,
		Operation:    convert.OperationRender,
		Engine:       convert.EngineCAD,
		SourceFormat: "step",
		TargetFormat: "png",
		Status:       status,
	}
	if stage != "" {
		s, i, c := stage, idx, count
		j.Stage, j.StageIndex, j.StageCount = &s, &i, &c
	}
	return j
}

func sseFrames(t *testing.T, body string) []map[string]json.RawMessage {
	t.Helper()
	var frames []map[string]json.RawMessage
	for _, chunk := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		if len(lines) != 2 || lines[0] != "event: progress" || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("frame is not an event line plus a data line: %q; body=%q", chunk, body)
		}
		var f map[string]json.RawMessage
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &f); err != nil {
			t.Fatalf("decode frame %q: %v", lines[1], err)
		}
		frames = append(frames, f)
	}
	return frames
}

func runCADStream(t *testing.T, script ...*jobs.Job) (string, []map[string]json.RawMessage) {
	t.Helper()
	srv := newSSETestServer(&sequencedFakeRepo{jobs: script})
	req, cancel := requestForSSE(stageJobID, stageClient)
	defer cancel()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.handleJobEvents(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatalf("handler did not return on the terminal status; body=%q", rec.Body.String())
	}
	body := rec.Body.String()
	return body, sseFrames(t, body)
}

func rawField(f map[string]json.RawMessage, key string) string {
	if v, ok := f[key]; ok {
		return string(v)
	}
	return "absent"
}

func frameLine(f map[string]json.RawMessage) string {
	if len(f) == 1 {
		return "terminal " + rawField(f, "status")
	}
	return fmt.Sprintf("%s %s %s %s %s/%s",
		rawField(f, "status"), rawField(f, "progress_pct"), rawField(f, "eta_seconds"),
		rawField(f, "stage"), rawField(f, "stage_index"), rawField(f, "stage_count"))
}

func frameLines(frames []map[string]json.RawMessage) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, frameLine(f))
	}
	return out
}

func stageChanges(frames []map[string]json.RawMessage) []string {
	var out []string
	prev := "null null"
	for _, f := range frames {
		if _, ok := f["stage_index"]; !ok {
			continue
		}
		pair := rawField(f, "stage") + " " + rawField(f, "stage_index")
		if pair != prev {
			out = append(out, pair)
		}
		prev = pair
	}
	return out
}

func assertFrames(t *testing.T, body string, frames []map[string]json.RawMessage, want []string) {
	t.Helper()
	got := frameLines(frames)
	if !slices.Equal(got, want) {
		t.Fatalf("frames:\n  got  %q\n  want %q\nbody=%q", got, want, body)
	}
	for i, f := range frames {
		if len(f) != 1 && len(f) != 6 {
			t.Errorf("frame %d has %d keys, want 6 (CAD progress) or 1 (terminal): %s", i, len(f), frameLine(f))
		}
	}
}

func TestSSE_CADStageFramesOneEachInOrder(t *testing.T) {
	body, frames := runCADStream(t,
		cadJob(jobs.StatusQueued, "", 0, 0),
		cadJob(jobs.StatusActive, "", 0, 0),
		cadJob(jobs.StatusActive, "guard", 1, 4),
		cadJob(jobs.StatusActive, "guard", 1, 4),
		cadJob(jobs.StatusActive, "mesh", 2, 4),
		cadJob(jobs.StatusActive, "mesh", 2, 4),
		cadJob(jobs.StatusDone, "judge", 4, 4),
	)
	assertFrames(t, body, frames, []string{
		`"queued" null null null null/null`,
		`"active" null null null null/null`,
		`"active" null null "guard" 1/4`,
		`"active" null null "mesh" 2/4`,
		`"active" null null "render" 3/4`,
		`"done" null null "judge" 4/4`,
		`terminal "done"`,
	})
	if got, want := stageChanges(frames), []string{`"guard" 1`, `"mesh" 2`, `"render" 3`, `"judge" 4`}; !slices.Equal(got, want) {
		t.Fatalf("stage changes = %q, want %q", got, want)
	}
	if got := string(frames[len(frames)-1]["status"]); got != `"done"` || len(frames[len(frames)-1]) != 1 {
		t.Fatalf("last frame = %s, want {\"status\":\"done\"}", frameLine(frames[len(frames)-1]))
	}
}

func TestSSE_CADStatusChangeIsNotAStageChange(t *testing.T) {
	body, frames := runCADStream(t,
		cadJob(jobs.StatusActive, "render", 3, 4),
		cadJob(jobs.StatusActive, "judge", 4, 4),
		cadJob(jobs.StatusDone, "judge", 4, 4),
	)
	assertFrames(t, body, frames, []string{
		`"active" null null "render" 3/4`,
		`"active" null null "judge" 4/4`,
		`"done" null null "judge" 4/4`,
		`terminal "done"`,
	})
	if got, want := stageChanges(frames), []string{`"render" 3`, `"judge" 4`}; !slices.Equal(got, want) {
		t.Fatalf("stage changes = %q, want %q", got, want)
	}
}

func TestSSE_CADConnectMidJobSendsOneFrame(t *testing.T) {
	body, frames := runCADStream(t,
		cadJob(jobs.StatusActive, "mesh", 2, 4),
		cadJob(jobs.StatusActive, "mesh", 2, 4),
		cadJob(jobs.StatusDone, "judge", 4, 4),
	)
	assertFrames(t, body, frames, []string{
		`"active" null null "mesh" 2/4`,
		`"active" null null "render" 3/4`,
		`"done" null null "judge" 4/4`,
		`terminal "done"`,
	})
}

func TestSSE_CADRetryResetSendsOneFrame(t *testing.T) {
	t.Run("reset_to_null", func(t *testing.T) {
		body, frames := runCADStream(t,
			cadJob(jobs.StatusActive, "mesh", 2, 4),
			cadJob(jobs.StatusActive, "", 0, 0),
			cadJob(jobs.StatusActive, "guard", 1, 4),
			cadJob(jobs.StatusFailed, "guard", 1, 4),
		)
		assertFrames(t, body, frames, []string{
			`"active" null null "mesh" 2/4`,
			`"active" null null null null/null`,
			`"active" null null "guard" 1/4`,
			`"failed" null null "guard" 1/4`,
			`terminal "failed"`,
		})
	})
	t.Run("index_goes_down", func(t *testing.T) {
		body, frames := runCADStream(t,
			cadJob(jobs.StatusActive, "render", 3, 4),
			cadJob(jobs.StatusActive, "guard", 1, 4),
			cadJob(jobs.StatusFailed, "guard", 1, 4),
		)
		assertFrames(t, body, frames, []string{
			`"active" null null "render" 3/4`,
			`"active" null null "guard" 1/4`,
			`"failed" null null "guard" 1/4`,
			`terminal "failed"`,
		})
	})
}

func TestSSE_CADFailedCatchesUpOnlyToTheFrozenStage(t *testing.T) {
	t.Run("failed_at_render", func(t *testing.T) {
		body, frames := runCADStream(t,
			cadJob(jobs.StatusActive, "guard", 1, 4),
			cadJob(jobs.StatusFailed, "render", 3, 4),
		)
		assertFrames(t, body, frames, []string{
			`"active" null null "guard" 1/4`,
			`"active" null null "mesh" 2/4`,
			`"failed" null null "render" 3/4`,
			`terminal "failed"`,
		})
	})
	t.Run("canceled_at_mesh", func(t *testing.T) {
		body, frames := runCADStream(t,
			cadJob(jobs.StatusQueued, "", 0, 0),
			cadJob(jobs.StatusCanceled, "mesh", 2, 4),
		)
		assertFrames(t, body, frames, []string{
			`"queued" null null null null/null`,
			`"active" null null "guard" 1/4`,
			`"canceled" null null "mesh" 2/4`,
			`terminal "canceled"`,
		})
	})
	t.Run("failed_at_first_stage", func(t *testing.T) {
		body, frames := runCADStream(t,
			cadJob(jobs.StatusActive, "", 0, 0),
			cadJob(jobs.StatusFailed, "guard", 1, 4),
		)
		assertFrames(t, body, frames, []string{
			`"active" null null null null/null`,
			`"failed" null null "guard" 1/4`,
			`terminal "failed"`,
		})
		if got := stageChanges(frames); len(got) != 1 {
			t.Fatalf("stage changes = %q, want exactly one", got)
		}
	})
}

func TestSSE_CADCatchUpRefusesAMismatchedRoute(t *testing.T) {
	unknown := cadJob(jobs.StatusActive, "render", 3, 4)
	unknown.TargetFormat = "obj"
	unknownDone := cadJob(jobs.StatusDone, "render", 3, 4)
	unknownDone.TargetFormat = "obj"
	cases := []struct {
		name   string
		script []*jobs.Job
		want   []string
	}{
		{"count_differs_from_route", []*jobs.Job{
			cadJob(jobs.StatusActive, "guard", 1, 4),
			cadJob(jobs.StatusActive, "render", 3, 5),
			cadJob(jobs.StatusDone, "render", 3, 5),
		}, []string{
			`"active" null null "guard" 1/4`,
			`"active" null null "render" 3/5`,
			`"done" null null "render" 3/5`,
			`terminal "done"`,
		}},
		{"stage_differs_from_route", []*jobs.Job{
			cadJob(jobs.StatusActive, "guard", 1, 4),
			cadJob(jobs.StatusActive, "judge", 3, 4),
			cadJob(jobs.StatusDone, "judge", 3, 4),
		}, []string{
			`"active" null null "guard" 1/4`,
			`"active" null null "judge" 3/4`,
			`"done" null null "judge" 3/4`,
			`terminal "done"`,
		}},
		{"route_not_in_table", []*jobs.Job{
			cadJob(jobs.StatusActive, "", 0, 0),
			unknown,
			unknownDone,
		}, []string{
			`"active" null null null null/null`,
			`"active" null null "render" 3/4`,
			`"done" null null "render" 3/4`,
			`terminal "done"`,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, frames := runCADStream(t, c.script...)
			assertFrames(t, body, frames, c.want)
		})
	}
}

func TestSSE_CADQueuedFrameCarriesNullStageKeys(t *testing.T) {
	body, _ := runCADStream(t,
		cadJob(jobs.StatusQueued, "", 0, 0),
		cadJob(jobs.StatusCanceled, "", 0, 0),
	)
	want := "event: progress\ndata: " +
		`{"status":"queued","progress_pct":null,"eta_seconds":null,"stage":null,"stage_index":null,"stage_count":null}` +
		"\n\n"
	if !strings.HasPrefix(body, want) {
		t.Fatalf("first frame:\n  got  %q\n  want %q", body, want)
	}
}

func describeCatchUp(frames []sseCADProgressPayload) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		pct, eta, stage, idx, count := "nil", "nil", "nil", "nil", "nil"
		if f.ProgressPct != nil {
			pct = fmt.Sprint(*f.ProgressPct)
		}
		if f.EtaSeconds != nil {
			eta = fmt.Sprint(*f.EtaSeconds)
		}
		if f.Stage != nil {
			stage = *f.Stage
		}
		if f.StageIndex != nil {
			idx = fmt.Sprint(*f.StageIndex)
		}
		if f.StageCount != nil {
			count = fmt.Sprint(*f.StageCount)
		}
		out = append(out, fmt.Sprintf("%s %s %s %s %s/%s", f.Status, pct, eta, stage, idx, count))
	}
	return out
}

func stageOfJob(j *jobs.Job) string {
	stage, idx, count := "nil", "nil", "nil"
	if j.Stage != nil {
		stage = *j.Stage
	}
	if j.StageIndex != nil {
		idx = fmt.Sprint(*j.StageIndex)
	}
	if j.StageCount != nil {
		count = fmt.Sprint(*j.StageCount)
	}
	return fmt.Sprintf("%s %s %s %s/%s", j.Engine, j.Status, stage, idx, count)
}

func TestStageCatchUpIsPure(t *testing.T) {
	av := cadJob(jobs.StatusActive, "render", 3, 4)
	av.Engine = convert.EngineAV
	noStage := cadJob(jobs.StatusActive, "render", 3, 4)
	noStage.Stage = nil
	cases := []struct {
		name string
		last *jobs.Job
		cur  *jobs.Job
		want []string
	}{
		{"from_2_to_3", cadJob(jobs.StatusActive, "mesh", 2, 4), cadJob(jobs.StatusActive, "render", 3, 4), nil},
		{"same_index", cadJob(jobs.StatusActive, "render", 3, 4), cadJob(jobs.StatusActive, "render", 3, 4), nil},
		{"index_goes_down", cadJob(jobs.StatusActive, "render", 3, 4), cadJob(jobs.StatusActive, "guard", 1, 4), nil},
		{"reset_to_null", cadJob(jobs.StatusActive, "render", 3, 4), cadJob(jobs.StatusActive, "", 0, 0), nil},
		{"stage_nil", cadJob(jobs.StatusActive, "guard", 1, 4), noStage, nil},
		{"engine_av", cadJob(jobs.StatusActive, "guard", 1, 4), av, nil},
		{"null_to_first", cadJob(jobs.StatusQueued, "", 0, 0), cadJob(jobs.StatusActive, "guard", 1, 4), nil},
		{"null_to_render", cadJob(jobs.StatusQueued, "", 0, 0), cadJob(jobs.StatusActive, "render", 3, 4),
			[]string{"active nil nil guard 1/4", "active nil nil mesh 2/4"}},
		{"guard_to_done_judge", cadJob(jobs.StatusActive, "guard", 1, 4), cadJob(jobs.StatusDone, "judge", 4, 4),
			[]string{"active nil nil mesh 2/4", "active nil nil render 3/4"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := stageOfJob(c.cur)
			last := snapshotOf(c.last)
			first := describeCatchUp(stageCatchUp(last, c.cur))
			second := describeCatchUp(stageCatchUp(last, c.cur))
			if !slices.Equal(first, c.want) {
				t.Fatalf("stageCatchUp = %q, want %q", first, c.want)
			}
			if !slices.Equal(first, second) {
				t.Fatalf("second call = %q, first = %q", second, first)
			}
			if after := stageOfJob(c.cur); after != before {
				t.Fatalf("stageCatchUp changed the job stage: %s, was %s", after, before)
			}
		})
	}
}
