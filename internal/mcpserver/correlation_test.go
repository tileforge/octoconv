package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"
)

// JobStatus is BOTH the decoded REST response and the get_job_status tool's
// output type, so a field it does not declare is a field the MCP surface
// silently drops -- Go's decoder discards unknown keys without complaint.
//
// That is how correlation_id reached the REST response and not the agent
// (codex, PR #23): nothing failed, the value simply was not there.
func TestJobStatusDecodesTheCorrelationID(t *testing.T) {
	const body = `{"job_id":"0a001d7a-66fa-4e68-98dd-d2103a291e21","status":"done",` +
		`"correlation_id":"svc-checkout/req-8f31c2a4"}`

	var js JobStatus
	if err := json.Unmarshal([]byte(body), &js); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if js.CorrelationID != "svc-checkout/req-8f31c2a4" {
		t.Fatalf("correlation_id = %q, want the value from the REST response. An agent using "+
			"get_job_status cannot tie the job to the operation that asked for it.", js.CorrelationID)
	}
}

// And it round-trips out again, omitted when absent so an agent is not handed
// an empty field to reason about.
func TestJobStatusEmitsTheCorrelationID(t *testing.T) {
	out, err := json.Marshal(JobStatus{JobID: "x", Status: "done", CorrelationID: "svc/req-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"correlation_id":"svc/req-1"`) {
		t.Fatalf("the tool output drops the identifier: %s", out)
	}

	out, err = json.Marshal(JobStatus{JobID: "x", Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "correlation_id") {
		t.Fatalf("an absent identifier is emitted as an empty field: %s", out)
	}
}
