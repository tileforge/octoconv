package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"go.yaml.in/yaml/v3"
)

func newIntakeServer(t *testing.T, cfg Config, window time.Duration, minRate int64) (*Server, *httptest.Server) {
	t.Helper()
	return newIntakeServerWith(t, &fakeRepo{}, &fakeStorage{}, cfg, window, minRate)
}

func newIntakeServerWith(t *testing.T, repo Repo, store Storage, cfg Config, window time.Duration, minRate int64) (*Server, *httptest.Server) {
	t.Helper()
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), cfg)
	srv.intakeWindow = window
	srv.intakeMinRate = minRate
	ts := httptest.NewUnstartedServer(srv.Routes())
	ts.Config.ReadHeaderTimeout = 10 * time.Second
	ts.Start()
	t.Cleanup(func() { closeWithin(t, ts, 10*time.Second) })
	return srv, ts
}

func closeWithin(t *testing.T, ts *httptest.Server, limit time.Duration) {
	t.Helper()
	closed := make(chan struct{})
	go func() {
		ts.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(limit):
		t.Errorf("the server did not shut down within %v: a handler never returned", limit)
	}
}

type pacedReader struct {
	r     io.Reader
	rate  int64
	chunk int
	start time.Time
	sent  int64
}

func (p *pacedReader) Read(b []byte) (int, error) {
	if p.start.IsZero() {
		p.start = time.Now()
	}
	if len(b) > p.chunk {
		b = b[:p.chunk]
	}
	time.Sleep(time.Until(p.start.Add(time.Duration(p.sent) * time.Second / time.Duration(p.rate))))
	n, err := p.r.Read(b)
	p.sent += int64(n)
	return n, err
}

type rawResult struct {
	status  int
	header  http.Header
	payload map[string]string
	elapsed time.Duration
}

func rawPost(t *testing.T, ts *httptest.Server, path string, extra http.Header, contentType string, contentLength int64, body io.Reader) rawResult {
	t.Helper()
	addr := ts.Listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var head strings.Builder
	fmt.Fprintf(&head, "POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: ApiKey %s\r\n", path, addr, testClientKey)
	for k, vs := range extra {
		for _, v := range vs {
			fmt.Fprintf(&head, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(&head, "Content-Type: %s\r\nContent-Length: %d\r\n\r\n", contentType, contentLength)
	if _, err := io.WriteString(conn, head.String()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(conn, body)
	}()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("no response after %v: %v", elapsed, err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read the response body after %v: %v", elapsed, err)
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d after %v with a body that is not a JSON error: %v; body=%.300s", resp.StatusCode, elapsed, err, raw)
		}
		payload = nil
	}
	conn.Close()
	<-done
	return rawResult{status: resp.StatusCode, header: resp.Header, payload: payload, elapsed: elapsed}
}

func intakeBody(t *testing.T, size int) (*bytes.Buffer, string) {
	t.Helper()
	return multipartBody(t, "in.bin", "", bytes.Repeat([]byte{'a'}, size))
}

func requireIntakeCut(t *testing.T, status int, payload map[string]string, elapsed time.Duration) {
	t.Helper()
	if status != http.StatusBadRequest || payload["code"] != "invalid_request" || payload["error"] != "invalid multipart form" {
		t.Errorf("status %d payload %v after %v, want 400 invalid_request \"invalid multipart form\"", status, payload, elapsed)
	}
}

func TestInspectIntakeAdmitsASteadyBodyThatOutlastsTheWindow(t *testing.T) {
	window := 400 * time.Millisecond
	_, ts := newIntakeServer(t, Config{}, window, 32<<10)
	body, ct := intakeBody(t, 96<<10)
	size := int64(body.Len())
	res := rawPost(t, ts, "/v1/inspect", nil, ct, size, &pacedReader{r: body, rate: 64 << 10, chunk: 4 << 10})
	status, payload, elapsed := res.status, res.payload, res.elapsed
	t.Logf("%d bytes at 64 KiB/s against a %v window and 32 KiB/s: status %d after %v", size, window, status, elapsed)
	if status != http.StatusOK {
		t.Errorf("status %d payload %v after %v, want 200 for a body sent at twice the minimum rate", status, payload, elapsed)
	}
	if elapsed < 3*window {
		t.Errorf("the transfer took %v, want at least %v so that it outlasts the window", elapsed, 3*window)
	}
}

func TestInspectIntakeCutsABodyBelowTheMinimumRate(t *testing.T) {
	window := 400 * time.Millisecond
	_, ts := newIntakeServer(t, Config{}, window, 32<<10)
	body, ct := intakeBody(t, 96<<10)
	size := int64(body.Len())
	res := rawPost(t, ts, "/v1/inspect", nil, ct, size, &pacedReader{r: body, rate: 8 << 10, chunk: 512})
	status, payload, elapsed := res.status, res.payload, res.elapsed
	t.Logf("%d bytes at 8 KiB/s against a %v window and 32 KiB/s: status %d after %v", size, window, status, elapsed)
	requireIntakeCut(t, status, payload, elapsed)
	if elapsed < window || elapsed >= 3*time.Second {
		t.Errorf("cut after %v, want at least the %v window and below 3s", elapsed, window)
	}
}

func TestInspectIntakeCutsAStalledClientAtTheWindow(t *testing.T) {
	window := 400 * time.Millisecond
	_, ts := newIntakeServer(t, Config{}, window, 32<<10)
	res := rawPost(t, ts, "/v1/inspect", nil, "multipart/form-data; boundary=intakestall", 4096, strings.NewReader(""))
	status, payload, elapsed := res.status, res.payload, res.elapsed
	t.Logf("no body byte against a %v window: status %d after %v", window, status, elapsed)
	requireIntakeCut(t, status, payload, elapsed)
	if elapsed < window || elapsed > window+2*time.Second {
		t.Errorf("cut after %v, want between %v and %v", elapsed, window, window+2*time.Second)
	}
}

type deadlineRecorder struct {
	t      *testing.T
	values []time.Time
	max    time.Time
}

func (d *deadlineRecorder) set(v time.Time) error {
	if n := len(d.values); n > 0 && v.Before(d.values[n-1]) {
		d.t.Errorf("deadline %v moved back from %v", v, d.values[n-1])
	}
	d.values = append(d.values, v)
	if v.After(d.max) {
		d.max = v
	}
	return nil
}

func (d *deadlineRecorder) last() time.Time {
	if len(d.values) == 0 {
		return time.Time{}
	}
	return d.values[len(d.values)-1]
}

func intakeBound(start time.Time, window time.Duration, minRate, limit int64) time.Time {
	return start.Add(window + time.Duration(limit/minRate)*time.Second + time.Duration(limit%minRate)*time.Second/time.Duration(minRate))
}

func TestInspectIntakeDeadlineIsBoundedByTheUploadCap(t *testing.T) {
	start := time.Unix(1700000000, 0)

	t.Run("a body past a small limit", func(t *testing.T) {
		rec := &deadlineRecorder{t: t}
		r := &intakeReader{body: io.NopCloser(bytes.NewReader(make([]byte, 25))), setDeadline: rec.set, start: start, window: time.Second, minRate: 1000, limit: 10}
		buf := make([]byte, 3)
		for {
			_, err := r.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		want := start.Add(time.Second + 10*time.Millisecond)
		if !rec.max.Equal(want) || !rec.last().Equal(want) {
			t.Errorf("max deadline %v, last %v, want both %v", rec.max, rec.last(), want)
		}
		for i, v := range rec.values {
			if v.After(want) {
				t.Errorf("deadline %d = %v is past %v", i, v, want)
			}
		}
		if len(rec.values) != 9 {
			t.Errorf("the deadline was set %d times, want 9, once per non-empty read", len(rec.values))
		}
	})

	t.Run("the chart cap with the production constants", func(t *testing.T) {
		limit := chartLiteral(t, "INSPECT_MAX_UPLOAD_BYTES")
		rec := &deadlineRecorder{t: t}
		r := &intakeReader{body: io.NopCloser(io.LimitReader(transportCapZeros{}, limit+1<<20)), setDeadline: rec.set, start: start, window: intakeReadDeadline, minRate: intakeMinBytesPerSecond, limit: limit}
		if _, err := io.Copy(io.Discard, r); err != nil {
			t.Fatal(err)
		}
		want := intakeBound(start, intakeReadDeadline, intakeMinBytesPerSecond, limit)
		if !rec.max.Equal(want) {
			t.Errorf("max deadline %v, want %v", rec.max, want)
		}
		t.Logf("worst-case intake slot hold at the chart cap of %d bytes: %v", limit, rec.max.Sub(start))
	})
}

func TestInspectIntakeCreditsBytesReturnedNotRequested(t *testing.T) {
	start := time.Unix(1700000000, 0)
	rec := &deadlineRecorder{t: t}
	r := &intakeReader{body: io.NopCloser(iotest.OneByteReader(strings.NewReader("abcde"))), setDeadline: rec.set, start: start, window: time.Second, minRate: 1000, limit: 1 << 20}
	buf := make([]byte, 4096)
	for {
		_, err := r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.values) != 5 {
		t.Fatalf("the deadline was set %d times (%v), want 5", len(rec.values), rec.values)
	}
	for k, v := range rec.values {
		if want := start.Add(time.Second + time.Duration(k+1)*time.Millisecond); !v.Equal(want) {
			t.Errorf("deadline %d = %v, want %v", k+1, v, want)
		}
	}
}

func openAPIString(t *testing.T, doc any, keys ...string) string {
	t.Helper()
	node := doc
	walked := "$"
	for _, key := range keys {
		m, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("%s: %s is %T, not a mapping", specPath, walked, node)
		}
		node, ok = m[key]
		walked += "." + key
		if !ok {
			t.Fatalf("%s: %s is missing", specPath, walked)
		}
	}
	s, ok := node.(string)
	if !ok {
		t.Fatalf("%s: %s is %T, not a string", specPath, walked, node)
	}
	return s
}

func TestIntakeRuleIsStatedInTheOpenAPI(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", specPath, err)
	}
	if intakeReadDeadline%time.Second != 0 || intakeMinBytesPerSecond%(1<<20) != 0 {
		t.Fatalf("intakeReadDeadline %v is not whole seconds or intakeMinBytesPerSecond %d is not whole MiB", intakeReadDeadline, intakeMinBytesPerSecond)
	}
	w := int64(intakeReadDeadline / time.Second)
	r := intakeMinBytesPerSecond >> 20
	for _, path := range []string{"/v1/inspect", "/v1/jobs"} {
		description := openAPIString(t, doc, "paths", path, "post", "responses", "400", "description")
		for _, want := range []string{
			fmt.Sprintf("The body has %d s, plus one second for every %d MiB received so far, to arrive", w, r),
			fmt.Sprintf("a body that falls below %d MiB/s once its first %d s have passed is cut and answered with this 400 (invalid_request, invalid multipart form)", r, w),
		} {
			if !strings.Contains(description, want) {
				t.Errorf("%s: the 400 of POST %s does not state %q; it reads %q", specPath, path, want, description)
			}
		}
	}
	if description := openAPIString(t, doc, "paths", "/v1/jobs", "post", "responses", "429", "description"); !strings.Contains(description, "intake_busy") {
		t.Errorf("%s: the 429 of POST /v1/jobs does not name intake_busy; it reads %q", specPath, description)
	}
}
