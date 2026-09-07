package worker

import (
	"testing"

	"github.com/apaderin/octoconv/internal/jobs"
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
