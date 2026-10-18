package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

// The webhook body is the ONE thing a caller cannot reconstruct for itself.
// It arrives later, possibly at a different instance than the one that made
// the request, and until OBS-03 it carried only job_id -- so a service
// investigating an incident from its own logs had nothing to match on.
//
// Proved live once. A live proof ages: this is what keeps it true.
func TestTerminalWebhookBodyCarriesTheCorrelationID(t *testing.T) {
	const cid = "svc-orders/inc-2026-09-07-b1"
	body := buildTerminalWebhookBody(&jobs.Job{
		Status:        jobs.StatusDone,
		CorrelationID: cid,
	})

	got, ok := body["correlation_id"]
	if !ok {
		t.Fatalf("the webhook body has no correlation_id, so the caller receives a delivery it "+
			"cannot tie to anything of its own.\nbody = %v", body)
	}
	if got != cid {
		t.Fatalf("correlation_id = %v, want %q", got, cid)
	}
}

// Absent means absent: the field is omitted rather than sent as null, because
// a field that is usually null trains readers to ignore it -- and this one
// matters exactly when somebody is reading carefully.
func TestTerminalWebhookBodyOmitsAnAbsentCorrelationID(t *testing.T) {
	body := buildTerminalWebhookBody(&jobs.Job{Status: jobs.StatusDone})

	if v, ok := body["correlation_id"]; ok {
		t.Fatalf("correlation_id is present as %v for a job that never had one", v)
	}
	// And the fields that must always be there still are -- a test that only
	// checks an absence passes just as well against an empty body.
	for _, k := range []string{"job_id", "status", "event"} {
		if _, ok := body[k]; !ok {
			t.Errorf("the body lost %q", k)
		}
	}
}

// A PROGRESS delivery is asynchronous too, arrives at whatever instance the
// caller has, and carried only job_id -- so until the terminal event arrived
// there was nothing for the caller to match on. For a long AV job that is the
// entire duration of the work (codex, PR #23).
func TestProgressWebhookCarriesTheCorrelationID(t *testing.T) {
	const cid = "svc-media/transcode-88"
	capture := &bodyCapture{}

	s := &progressSink{
		correlationID:  cid,
		webhookEnabled: true,
		callbackURL:    "https://example.invalid/hook",
		webhookDelivr:  capture,
	}
	s.fireDetached(convert.ProgressUpdate{Pct: 42})

	got := capture.wait(t)
	if got["correlation_id"] != cid {
		t.Fatalf("progress body has correlation_id=%v, want %q -- a caller watching a long job "+
			"cannot tie these deliveries to anything of its own until it finishes.\nbody = %v",
			got["correlation_id"], cid, got)
	}
	if got["event"] != "progress" {
		t.Fatalf("this is not the progress body: %v", got)
	}
}

// Absent stays absent here too, matching the terminal body exactly.
func TestProgressWebhookOmitsAnAbsentCorrelationID(t *testing.T) {
	capture := &bodyCapture{}
	s := &progressSink{
		webhookEnabled: true,
		callbackURL:    "https://example.invalid/hook",
		webhookDelivr:  capture,
	}
	s.fireDetached(convert.ProgressUpdate{Pct: 7})

	got := capture.wait(t)
	if _, ok := got["correlation_id"]; ok {
		t.Fatalf("correlation_id present for a job that never had one: %v", got)
	}
	if got["progress_pct"] == nil {
		t.Fatalf("the body lost its own fields: %v", got)
	}
}

// bodyCapture records the JSON body of one detached progress delivery.
//
// fireDetached is fire-and-forget by design (Pitfall 5: the progress reader
// must never wait on an HTTP round trip), so the body arrives on another
// goroutine and the test has to wait for it rather than read it straight
// after the call.
type bodyCapture struct {
	mu   sync.Mutex
	body map[string]any
}

func (c *bodyCapture) Deliver(_ context.Context, _ string, body []byte, _ int64, _ string) (int, error) {
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	c.mu.Lock()
	c.body = m
	c.mu.Unlock()
	return 200, nil
}

func (c *bodyCapture) wait(t *testing.T) map[string]any {
	t.Helper()
	for i := 0; i < 200; i++ {
		c.mu.Lock()
		m := c.body
		c.mu.Unlock()
		if m != nil {
			return m
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no progress webhook body was delivered within 2s")
	return nil
}
