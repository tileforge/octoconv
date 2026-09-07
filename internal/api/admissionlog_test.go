package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/jobs"
)

// captureLog redirects the standard logger into a buffer for one test and
// puts it back afterwards.
//
// log.Writer() and not log.SetOutput(nil): nil is not "the default", it is a
// nil io.Writer, and every log call after it panics inside log.output. The
// first version of this file did exactly that, and the damage showed up as
// five UNRELATED tests failing later in the package -- the new tests passed.
// Running one test proves less than running the package.
//
// The standard logger is global, so tests using this must not run in parallel
// with anything that logs.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

// The admission record is the ONE place an HTTP request id and a job id appear
// together. chi's middleware.RequestID never leaves this process and is never
// stored; job_id is stored everywhere and appears in no HTTP log line. Without
// this record the two never meet, and every worker line about the job is
// unreachable from the request that created it.
//
// So this test is not "a log line exists" -- it is the join, and it checks both
// halves are in the same record.
func TestAdmissionRecordJoinsRequestIDToJobID(t *testing.T) {
	buf := captureLog(t)

	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "job accepted") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no admission record was logged, so nothing connects the HTTP request to the "+
			"job it created.\nlog was:\n%s", buf.String())
	}

	for _, want := range []string{"request_id=", "job_id=", "client_id=", "engine=", "target="} {
		if !strings.Contains(line, want) {
			t.Errorf("the admission record has no %s field: %s", want, line)
		}
	}
	// The request id must be the one chi assigned, not an empty placeholder --
	// a field that is always empty joins nothing and reads as if it worked.
	if strings.Contains(line, `request_id=""`) {
		t.Errorf("request_id is empty, so the record joins nothing: %s", line)
	}
}

// A record that reaches the log as two lines is not a record. The API's own
// values are safe, but filename comes from the client, and the escaping is
// what keeps that true.
//
// Note on scope: Go's multipart parser rejects a Content-Disposition carrying
// a newline before this handler ever runs (verified live: HTTP 400), so this
// is defence in depth at the API boundary rather than the only guard. It is
// the ONLY guard in the worker, where the engine's stderr -- the engine
// reacting to a document the client wrote -- reaches a log field.
func TestAdmissionRecordIsOneLine(t *testing.T) {
	buf := captureLog(t)

	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	body, ct := multipartBody(t, "in\t\"quoted\".png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	srv.Routes().ServeHTTP(httptest.NewRecorder(), req)

	for _, l := range strings.Split(buf.String(), "\n") {
		if !strings.Contains(l, "job accepted") {
			continue
		}
		// Everything after the filename field must still be on this line.
		if !strings.Contains(l, "bytes=") {
			t.Fatalf("the record was cut short before its last field, so a value ended it "+
				"early: %s", l)
		}
	}
}

// The cancellation record belongs where the guarded transition COMMITS.
//
// It first lived in the worker, keyed on the error wrapping context.Canceled,
// and was wrong in both directions (codex, PR #23): an asynq shutdown or any
// parent-context cancellation reached that branch too, so it could claim "job
// canceled" for a row still active -- and a job canceled while QUEUED never
// enters the worker at all, so it had no record.
//
// Here it fires for exactly the jobs that were canceled, exactly once.
func TestCancelRecordIsWrittenWhereTheTransitionCommits(t *testing.T) {
	buf := captureLog(t)

	// Seeded the way the existing cancel tests do. The first version of this
	// test built the job through the API and then SKIPPED, because fakeRepo.Get
	// returns nil and the handler 500'd -- and its skip message claimed an
	// "absence check below" that did not exist. A test that skips proves
	// nothing, and one that says otherwise is worse than one that fails.
	id := uuid.New()
	resolver := newFakeResolver()
	job := &jobs.Job{
		ID: id, ClientID: resolver.client.ID, Status: jobs.StatusQueued,
		Engine: "image", TargetFormat: "webp", CorrelationID: "svc-x/req-77",
	}
	repo := &fakeRepo{getJob: job}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(),
		defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver,
		healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+id.String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "job canceled") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("a QUEUED job was canceled and left no record. That job never reaches a worker, "+
			"so if the record lived there it would not exist at all.\nlog:\n%s", buf.String())
	}
	for _, want := range []string{id.String(), "correlation_id=svc-x/req-77", "from_status=queued"} {
		if !strings.Contains(line, want) {
			t.Errorf("the cancellation record lacks %s: %s", want, line)
		}
	}
}
