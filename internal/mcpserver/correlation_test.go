package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// An agent must be able to SET the identifier on its own conversions, not only
// read one on jobs created elsewhere. Without the header on the create
// request, the decoder fix exposed the field and nothing ever filled it
// (codex, PR #23).
func TestCreateJobSendsTheCorrelationHeader(t *testing.T) {
	var got string
	var seen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			got, seen = r.Header.Get("Correlation-Id"), true
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":"11111111-1111-1111-1111-111111111111","status":"queued"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "in.png")
	if err := os.WriteFile(file, []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      dir,
		ConvertTimeout: 5 * time.Second,
		PollInterval:   10 * time.Millisecond,
	})
	if _, err := c.CreateJob(context.Background(), CreateRequest{
		FilePath: file, Target: "webp", CorrelationID: "agent/task-42",
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if !seen {
		t.Fatal("the create request never reached the server")
	}
	if got != "agent/task-42" {
		t.Fatalf("Correlation-Id = %q, want %q. An agent cannot correlate the conversions it "+
			"performs, only ones somebody else created.", got, "agent/task-42")
	}
}

// And no header at all when none was supplied: an empty Correlation-Id would
// be a value the API has to decide about, rather than an absent one.
func TestCreateJobOmitsAnEmptyCorrelationHeader(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, present = r.Header["Correlation-Id"]
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":"11111111-1111-1111-1111-111111111111","status":"queued"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "in.png")
	if err := os.WriteFile(file, []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      dir,
		ConvertTimeout: 5 * time.Second,
		PollInterval:   10 * time.Millisecond,
	})
	if _, err := c.CreateJob(context.Background(), CreateRequest{FilePath: file, Target: "webp"}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if present {
		t.Fatal("an empty Correlation-Id header was sent")
	}
}
