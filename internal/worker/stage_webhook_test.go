package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/webhook"
)

type stageDelivery struct {
	url  string
	body []byte
	ts   int64
	sig  string
}

type stageDeliveries struct {
	mu         sync.Mutex
	started    bool
	firstDelay time.Duration
	got        []stageDelivery
}

func (d *stageDeliveries) Deliver(_ context.Context, url string, body []byte, ts int64, sig string) (int, error) {
	d.mu.Lock()
	first := !d.started
	d.started = true
	d.mu.Unlock()
	if first && d.firstDelay > 0 {
		time.Sleep(d.firstDelay)
	}
	d.mu.Lock()
	d.got = append(d.got, stageDelivery{url: url, body: body, ts: ts, sig: sig})
	d.mu.Unlock()
	return 200, nil
}

func (d *stageDeliveries) snapshot() []stageDelivery {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.got)
}

func (d *stageDeliveries) wait(t *testing.T, n int) []stageDelivery {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := d.snapshot(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("got %d stage deliveries within 2s, want %d", len(d.snapshot()), n)
	return nil
}

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	return m
}

func sortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}

func stageSink(d progressWebhookDeliverer, correlationID string) *progressSink {
	return &progressSink{
		ctx: context.Background(), repo: &spyProgressRepo{}, jobID: uuid.New(), correlationID: correlationID,
		webhookEnabled: true, callbackURL: "https://example.invalid/hook", webhookDelivr: d,
		webhookSecret: []byte("s3cr3t"), webhookTimeout: 200 * time.Millisecond,
	}
}

type failingRouteConverter struct {
	routeConverter
	stop int
}

func (c failingRouteConverter) ConvertWithStages(_ context.Context, _, _ string, _ map[string]any, sink convert.StageSink) error {
	route, _ := convert.CADStages(c.op, c.from, c.to)
	for i, stage := range route[:c.stop] {
		sink.Stage(convert.StageUpdate{Stage: stage, Index: i + 1, Count: len(route)})
	}
	return errors.New("engine failed")
}

func TestStageWebhookBody(t *testing.T) {
	cases := []struct {
		name          string
		correlationID string
		wantKeys      []string
	}{
		{"without_correlation_id", "", []string{"eta_seconds", "event", "job_id", "progress_pct", "stage", "stage_count", "stage_index"}},
		{"with_correlation_id", "svc-cad/plan-7", []string{"correlation_id", "eta_seconds", "event", "job_id", "progress_pct", "stage", "stage_count", "stage_index"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &stageDeliveries{}
			s := stageSink(d, tc.correlationID)
			s.Stage(convert.StageUpdate{Stage: "mesh", Index: 2, Count: 4})
			s.closeStages()

			got := d.wait(t, 1)
			time.Sleep(50 * time.Millisecond)
			if n := len(d.snapshot()); n != 1 {
				t.Fatalf("deliveries = %d, want exactly 1", n)
			}
			if got[0].url != s.callbackURL {
				t.Errorf("url = %q, want %q", got[0].url, s.callbackURL)
			}
			m := decodeBody(t, got[0].body)
			if keys := sortedKeys(m); !slices.Equal(keys, tc.wantKeys) {
				t.Fatalf("body keys = %v, want %v\nbody = %s", keys, tc.wantKeys, got[0].body)
			}
			if m["job_id"] != s.jobID.String() {
				t.Errorf("job_id = %v, want %s", m["job_id"], s.jobID)
			}
			if m["event"] != "progress" {
				t.Errorf("event = %v, want progress", m["event"])
			}
			for _, k := range []string{"progress_pct", "eta_seconds"} {
				if v, ok := m[k]; !ok || v != nil {
					t.Errorf("%s = %v (present %v), want present and null", k, v, ok)
				}
			}
			if m["stage"] != "mesh" || m["stage_index"] != float64(2) || m["stage_count"] != float64(4) {
				t.Errorf("stage = %v %v/%v, want mesh 2/4", m["stage"], m["stage_index"], m["stage_count"])
			}
			if tc.correlationID != "" && m["correlation_id"] != tc.correlationID {
				t.Errorf("correlation_id = %v, want %q", m["correlation_id"], tc.correlationID)
			}
		})
	}
}

func TestStageWebhookSignature(t *testing.T) {
	d := &stageDeliveries{}
	s := stageSink(d, "")
	before := time.Now().Unix()
	s.Stage(convert.StageUpdate{Stage: "guard", Index: 1, Count: 3})
	s.Stage(convert.StageUpdate{Stage: "mesh", Index: 2, Count: 3})
	s.closeStages()

	for i, got := range d.wait(t, 2) {
		if got.ts < before || got.ts > time.Now().Unix() {
			t.Errorf("delivery %d timestamp = %d, want within the test window", i, got.ts)
		}
		if want := webhook.SignPayload(s.webhookSecret, got.ts, got.body); got.sig != want {
			t.Errorf("delivery %d signature = %q, want %q", i, got.sig, want)
		}
	}
}

func TestStageWebhooksArriveInStageOrder(t *testing.T) {
	t.Run("direct_sink", func(t *testing.T) {
		d := &stageDeliveries{firstDelay: 150 * time.Millisecond}
		s := stageSink(d, "")

		start := time.Now()
		s.Stage(convert.StageUpdate{Stage: "guard", Index: 1, Count: 3})
		s.Stage(convert.StageUpdate{Stage: "mesh", Index: 2, Count: 3})
		s.Stage(convert.StageUpdate{Stage: "judge", Index: 3, Count: 3})
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Fatalf("three Stage calls took %v, want <50ms", elapsed)
		}
		q := s.stageQueue
		s.closeStages()
		if s.stageQueue != nil {
			t.Fatal("closeStages left the queue on the sink")
		}

		assertStageOrder(t, d.wait(t, 3), 1, 2, 3)
		if q == nil {
			t.Fatal("no stage queue was created")
		}
		if _, open := <-q; open {
			t.Fatal("stage queue still open after closeStages")
		}
	})

	t.Run("conversion_fails", func(t *testing.T) {
		d := &stageDeliveries{firstDelay: 150 * time.Millisecond}
		repo := &spyProgressRepo{}
		conv := failingRouteConverter{routeConverter{convert.OperationRender, "step", "png"}, 2}

		start := time.Now()
		err := runConvertWithOptionalProgress(context.Background(), conv, "in.step", "out.png", nil, repo, uuid.New(), "", 2*time.Second, 1, true, "https://example.invalid/hook", d, []byte("s3cr3t"), 200*time.Millisecond)
		if err == nil {
			t.Fatal("runConvertWithOptionalProgress returned nil, want the engine error")
		}
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Fatalf("failed conversion returned after %v, want <50ms: it must not wait for a delivery", elapsed)
		}

		assertStageOrder(t, d.wait(t, 2), 1, 2)
		time.Sleep(50 * time.Millisecond)
		if n := len(d.snapshot()); n != 2 {
			t.Fatalf("deliveries = %d, want 2", n)
		}
	})
}

func assertStageOrder(t *testing.T, got []stageDelivery, want ...int) {
	t.Helper()
	var order []int
	for _, g := range got {
		order = append(order, int(decodeBody(t, g.body)["stage_index"].(float64)))
	}
	if !slices.Equal(order, want) {
		t.Fatalf("delivery order = %v, want %v", order, want)
	}
}

func TestStageNeverWaitsForASlowCallback(t *testing.T) {
	var reqCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := &progressSink{
		ctx: context.Background(), repo: &spyProgressRepo{}, jobID: uuid.New(),
		webhookEnabled: true, callbackURL: srv.URL, webhookDelivr: webhook.NewDeliverer(),
		webhookSecret: []byte("s3cr3t"), webhookTimeout: 200 * time.Millisecond,
	}

	start := time.Now()
	s.Stage(convert.StageUpdate{Stage: "guard", Index: 1, Count: 3})
	s.Stage(convert.StageUpdate{Stage: "mesh", Index: 2, Count: 3})
	s.Stage(convert.StageUpdate{Stage: "judge", Index: 3, Count: 3})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("three Stage calls took %v, want <50ms against a callback that hangs 2s", elapsed)
	}
	s.closeStages()

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&reqCount) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := atomic.LoadInt32(&reqCount); n != 3 {
		t.Fatalf("callback received %d requests, want 3", n)
	}
}

func TestStageBypassesTheThrottle(t *testing.T) {
	d := &stageDeliveries{}
	repo := &spyProgressRepo{}
	conv := routeConverter{convert.OperationRender, "step", "png"}
	if err := runConvertWithOptionalProgress(context.Background(), conv, "in.step", "out.png", nil, repo, uuid.New(), "", 2*time.Second, 1, true, "https://example.invalid/hook", d, []byte("s3cr3t"), 200*time.Millisecond); err != nil {
		t.Fatalf("runConvertWithOptionalProgress: %v", err)
	}
	if len(repo.stages) != 4 {
		t.Fatalf("UpdateStage calls = %+v, want 4", repo.stages)
	}
	assertStageOrder(t, d.wait(t, 4), 1, 2, 3, 4)
}

func TestStageWebhookNeverEnqueues(t *testing.T) {
	repo := &poisonedProgressRepo{}
	d := &stageDeliveries{}
	conv := routeConverter{convert.OperationRender, "step", "png"}
	if err := runConvertWithOptionalProgress(context.Background(), conv, "in.step", "out.png", nil, repo, uuid.New(), "", 2*time.Second, 1, true, "https://example.invalid/hook", d, []byte("s3cr3t"), 200*time.Millisecond); err != nil {
		t.Fatalf("runConvertWithOptionalProgress: %v", err)
	}
	d.wait(t, 1)
	if n := atomic.LoadInt32(&repo.enqueueCalls); n != 0 {
		t.Fatalf("stage path called EnqueueWebhookDeliver %d times, want 0", n)
	}
	if n := atomic.LoadInt32(&repo.recordCalls); n != 0 {
		t.Fatalf("stage path called RecordAttempt %d times, want 0", n)
	}
}

func TestStageWebhookOffWhenNotOptedIn(t *testing.T) {
	cases := []struct {
		name        string
		enabled     bool
		callbackURL string
		nilDelivr   bool
	}{
		{"opted_out", false, "https://example.invalid/hook", false},
		{"empty_callback_url", true, "", false},
		{"nil_deliverer", true, "https://example.invalid/hook", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &stageDeliveries{}
			var delivr progressWebhookDeliverer = d
			if tc.nilDelivr {
				delivr = nil
			}
			repo := &spyProgressRepo{}
			s := &progressSink{
				ctx: context.Background(), repo: repo, jobID: uuid.New(),
				webhookEnabled: tc.enabled, callbackURL: tc.callbackURL, webhookDelivr: delivr,
				webhookSecret: []byte("s3cr3t"), webhookTimeout: 200 * time.Millisecond,
			}
			s.Stage(convert.StageUpdate{Stage: "guard", Index: 1, Count: 3})
			s.closeStages()
			time.Sleep(50 * time.Millisecond)
			if n := len(d.snapshot()); n != 0 {
				t.Fatalf("deliveries = %d, want 0", n)
			}
			if want := []convert.StageUpdate{{Stage: "guard", Index: 1, Count: 3}}; !slices.Equal(repo.stages, want) {
				t.Fatalf("UpdateStage calls = %+v, want %+v", repo.stages, want)
			}
		})
	}
}

func TestProgressWebhookBodyHasNoStageKeys(t *testing.T) {
	capture := &bodyCapture{}
	s := &progressSink{
		jobID:          uuid.New(),
		webhookEnabled: true,
		callbackURL:    "https://example.invalid/hook",
		webhookDelivr:  capture,
	}
	s.fireDetached(convert.ProgressUpdate{Pct: 42})

	got := capture.wait(t)
	if keys, want := sortedKeys(got), []string{"eta_seconds", "event", "job_id", "progress_pct"}; !slices.Equal(keys, want) {
		t.Fatalf("progress body keys = %v, want %v", keys, want)
	}
}
