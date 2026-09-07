package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
