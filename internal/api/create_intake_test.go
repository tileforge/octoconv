package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

func createIntakeConfig(maxUpload int64) Config {
	return Config{
		MaxUploadBytes:        maxUpload,
		InspectMaxUploadBytes: maxUpload,
		InspectMaxWorkBytes:   maxUpload,
		MaxFormatBytes:        map[string]map[string]int64{convert.EnginePandoc: {"md": maxUpload}},
	}
}

func createIntakeBody(t *testing.T, size int) (*bytes.Buffer, string) {
	t.Helper()
	return multipartBody(t, "in.md", "html", paddedMarkdown(size))
}

func createIntakeRequest(body io.Reader, contentType string) *http.Request {
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", contentType)
	return req
}

type gateStorage struct {
	arrived chan struct{}
	release chan struct{}
}

func (g *gateStorage) Upload(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	_, _ = io.Copy(io.Discard, r)
	g.arrived <- struct{}{}
	<-g.release
	return nil
}

func (g *gateStorage) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

type lockedRepo struct {
	*fakeRepo
	mu sync.Mutex
}

func (r *lockedRepo) PreAdmit(ctx context.Context, clientID uuid.UUID, engine string, inputBytes int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakeRepo.PreAdmit(ctx, clientID, engine, inputBytes)
}

func (r *lockedRepo) Create(_ context.Context, p jobs.CreateParams) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fakeRepo.created = &p
	return uuid.New(), nil
}

type lockedQueue struct {
	*fakeQueue
	mu sync.Mutex
}

func (q *lockedQueue) EnqueuePandocConvert(ctx context.Context, id uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.fakeQueue.EnqueuePandocConvert(ctx, id)
}

type countingBody struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func TestCreateIntakeCutsAStalledClientAtTheWindow(t *testing.T) {
	window := 400 * time.Millisecond
	_, ts := newIntakeServer(t, createIntakeConfig(64<<20), window, 32<<10)
	res := rawPost(t, ts, "/v1/jobs", nil, "multipart/form-data; boundary=intakestall", 4096, strings.NewReader(""))
	t.Logf("no body byte against a %v window: status %d after %v", window, res.status, res.elapsed)
	requireIntakeCut(t, res.status, res.payload, res.elapsed)
	if res.elapsed < window || res.elapsed > window+2*time.Second {
		t.Errorf("cut after %v, want between %v and %v", res.elapsed, window, window+2*time.Second)
	}
}

func TestCreateIntakeConcurrencyIsBounded(t *testing.T) {
	store := &gateStorage{arrived: make(chan struct{}, 16), release: make(chan struct{})}
	var once sync.Once
	open := func() { once.Do(func() { close(store.release) }) }
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	t.Cleanup(open)
	srv := NewServer(&lockedRepo{fakeRepo: &fakeRepo{}}, store, &lockedQueue{fakeQueue: &fakeQueue{}}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), createIntakeConfig(64<<20))
	h := srv.Routes()

	serve := func(req *http.Request) <-chan *httptest.ResponseRecorder {
		out := make(chan *httptest.ResponseRecorder, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			out <- rec
		}()
		return out
	}

	held := make([]<-chan *httptest.ResponseRecorder, 0, createIntakeMaxConcurrent)
	for range createIntakeMaxConcurrent {
		body, ct := createIntakeBody(t, 4096)
		held = append(held, serve(createIntakeRequest(body, ct)))
	}
	reached := time.After(5 * time.Second)
	for i := range createIntakeMaxConcurrent {
		select {
		case <-store.arrived:
		case <-reached:
			t.Fatalf("%d of %d creates reached Upload within 5s", i, createIntakeMaxConcurrent)
		}
	}

	for i := range 3 {
		body, ct := createIntakeBody(t, 4096)
		cb := &countingBody{r: body}
		select {
		case rec := <-serve(createIntakeRequest(cb, ct)):
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("create %d over %d busy slots: status %d, want 429; body=%s", i+1, createIntakeMaxConcurrent, rec.Code, rec.Body.String())
			}
			assertErrorCode(t, rec.Body.Bytes(), "intake_busy")
			if got, want := rec.Header().Get("Retry-After"), strconv.Itoa(createIntakeBusyRetryAfterSeconds); got != want {
				t.Errorf("Retry-After = %q, want %q", got, want)
			}
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if payload["error"] != createIntakeBusyMessage {
				t.Errorf("error = %q, want %q", payload["error"], createIntakeBusyMessage)
			}
			if strings.ContainsAny(payload["error"], "0123456789") {
				t.Errorf("error %q names a number", payload["error"])
			}
			if n := cb.n.Load(); n != 0 {
				t.Errorf("the refused create had %d body bytes read, want 0", n)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("create %d over %d busy slots got no answer within 2s", i+1, createIntakeMaxConcurrent)
		}
	}
	if n := len(srv.createIntakeSem); n != createIntakeMaxConcurrent {
		t.Errorf("after three refusals %d create slots are taken, want %d", n, createIntakeMaxConcurrent)
	}

	open()
	finished := time.After(5 * time.Second)
	for i, ch := range held {
		select {
		case rec := <-ch:
			if rec.Code != http.StatusAccepted {
				t.Errorf("held create %d: status %d, want 202; body=%s", i+1, rec.Code, rec.Body.String())
			}
		case <-finished:
			t.Fatalf("held create %d did not finish within 5s of the release", i+1)
		}
	}
	if n := len(srv.createIntakeSem); n != 0 {
		t.Errorf("after the held creates finished %d create slots are taken, want 0", n)
	}
	body, ct := createIntakeBody(t, 4096)
	select {
	case rec := <-serve(createIntakeRequest(body, ct)):
		if rec.Code != http.StatusAccepted {
			t.Errorf("create after the release: status %d, want 202; body=%s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("create after the release did not finish within 5s")
	}
}

type ctxStorage struct {
	delay time.Duration
	mu    sync.Mutex
	seen  bool
	err   error
}

func (c *ctxStorage) Upload(ctx context.Context, _ string, r io.Reader, _ int64, _ string) error {
	_, _ = io.Copy(io.Discard, r)
	time.Sleep(c.delay)
	err := ctx.Err()
	c.mu.Lock()
	c.seen = true
	c.err = err
	c.mu.Unlock()
	return err
}

func (c *ctxStorage) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (c *ctxStorage) result() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen, c.err
}

type slowPreAdmitRepo struct {
	*fakeRepo
	delay time.Duration
	once  sync.Once
}

func (r *slowPreAdmitRepo) PreAdmit(ctx context.Context, _ uuid.UUID, _ string, _ int64) error {
	r.once.Do(func() { time.Sleep(r.delay) })
	return ctx.Err()
}

type errAfterDataReader struct {
	data []byte
	err  error
}

func (e *errAfterDataReader) Read(p []byte) (int, error) {
	n := copy(p, e.data)
	e.data = e.data[n:]
	return n, e.err
}

func fillIntakeSlots(t *testing.T, sem chan struct{}) {
	t.Helper()
	for range cap(sem) {
		sem <- struct{}{}
	}
	t.Cleanup(func() {
		for range cap(sem) {
			<-sem
		}
	})
}

func TestIntakeReaderDoesNotRearmAtEOF(t *testing.T) {
	start := time.Unix(1700000000, 0)

	t.Run("data returned with EOF", func(t *testing.T) {
		rec := &deadlineRecorder{t: t}
		r := &intakeReader{body: io.NopCloser(iotest.DataErrReader(strings.NewReader("abcde"))), setDeadline: rec.set, start: start, window: time.Second, minRate: 1000, limit: 1 << 20}
		buf := make([]byte, 2)
		for {
			_, err := r.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if len(rec.values) != 2 {
			t.Errorf("the deadline was set %d times (%v), want 2: once for ab, once for cd, never for e with EOF", len(rec.values), rec.values)
		}
		if r.received != 5 {
			t.Errorf("received = %d, want 5", r.received)
		}
	})

	t.Run("data returned with another error", func(t *testing.T) {
		rec := &deadlineRecorder{t: t}
		boom := errors.New("boom")
		r := &intakeReader{body: io.NopCloser(&errAfterDataReader{data: []byte("abc"), err: boom}), setDeadline: rec.set, start: start, window: time.Second, minRate: 1000, limit: 1 << 20}
		n, err := r.Read(make([]byte, 8))
		if n != 3 || !errors.Is(err, boom) {
			t.Fatalf("Read = %d, %v; want 3, %v", n, err, boom)
		}
		if len(rec.values) != 0 {
			t.Errorf("the deadline was set %d times (%v), want 0 on a read that returned an error", len(rec.values), rec.values)
		}
		if r.received != 3 {
			t.Errorf("received = %d, want 3", r.received)
		}
	})
}

func TestRequestContextSurvivesTheDeadlineAfterBodyEOF(t *testing.T) {
	window := 300 * time.Millisecond
	delay := 700 * time.Millisecond

	t.Run("create", func(t *testing.T) {
		store := &ctxStorage{delay: delay}
		_, ts := newIntakeServerWith(t, &fakeRepo{}, store, createIntakeConfig(64<<20), window, 1<<30)
		body, ct := createIntakeBody(t, 4096)
		res := rawPost(t, ts, "/v1/jobs", nil, ct, int64(body.Len()), body)
		t.Logf("whole body at once, Upload sleeping %v against a %v window: status %d after %v", delay, window, res.status, res.elapsed)
		if res.status != http.StatusAccepted {
			t.Errorf("status %d payload %v, want 202", res.status, res.payload)
		}
		seen, err := store.result()
		if !seen {
			t.Fatal("Upload never ran")
		}
		if err != nil {
			t.Errorf("Upload saw ctx.Err() = %v after the body ended, want nil", err)
		}
	})

	t.Run("inspect", func(t *testing.T) {
		repo := &slowPreAdmitRepo{fakeRepo: &fakeRepo{}, delay: delay}
		_, ts := newIntakeServerWith(t, repo, &fakeStorage{}, Config{InspectMaxWorkDuration: 10 * time.Second}, window, 1<<30)
		body, ct := multipartBody(t, "in.png", "", pngBytesFixture())
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/inspect", bytes.NewReader(body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", ct)
		resp, err := ts.Client().Do(authed(req))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200; body=%.300s", resp.StatusCode, raw)
		}
		var rep inspectResponse
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatalf("decode: %v; body=%.300s", err, raw)
		}
		if len(rep.Operations) == 0 {
			t.Fatal("no operations for a png")
		}
		for _, op := range rep.Operations {
			if op.Quota.Allowed == nil {
				t.Errorf("operation %s %s: quota unknown after the body ended and the first PreAdmit slept %v past the %v window", op.Operation, op.Target, delay, window)
			}
		}
	})
}

func TestIntakeBusyAnswerIsNotHeldByAStalledBody(t *testing.T) {
	window := 400 * time.Millisecond
	stalled := func(t *testing.T, ts *httptest.Server, path string, extra http.Header) rawResult {
		t.Helper()
		return rawPost(t, ts, path, extra, "multipart/form-data; boundary=intakestall", 1024, strings.NewReader(""))
	}
	for _, tc := range []struct {
		name   string
		path   string
		extra  http.Header
		fill   func(*Server) chan struct{}
		status int
		code   string
	}{
		{"create", "/v1/jobs", nil, func(s *Server) chan struct{} { return s.createIntakeSem }, http.StatusTooManyRequests, "intake_busy"},
		{"inspect", "/v1/inspect", nil, func(s *Server) chan struct{} { return s.inspectIntakeSem }, http.StatusTooManyRequests, "inspect_busy"},
		{"correlation id", "/v1/jobs", http.Header{CorrelationHeader: {"a b"}}, nil, http.StatusUnprocessableEntity, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, ts := newIntakeServer(t, createIntakeConfig(64<<20), window, 32<<10)
			if tc.fill != nil {
				fillIntakeSlots(t, tc.fill(srv))
			}
			res := stalled(t, ts, tc.path, tc.extra)
			t.Logf("1024 announced body bytes never sent, %v window: status %d after %v", window, res.status, res.elapsed)
			if res.status != tc.status || res.payload["code"] != tc.code {
				t.Errorf("status %d payload %v, want %d %s", res.status, res.payload, tc.status, tc.code)
			}
			if res.elapsed > window+2*time.Second {
				t.Errorf("answered after %v, want no later than %v", res.elapsed, window+2*time.Second)
			}
		})
	}
}

func TestCreateIntakeCutsABodyBelowTheMinimumRate(t *testing.T) {
	window := 400 * time.Millisecond
	_, ts := newIntakeServer(t, createIntakeConfig(64<<20), window, 32<<10)
	body, ct := createIntakeBody(t, 96<<10)
	size := int64(body.Len())
	res := rawPost(t, ts, "/v1/jobs", nil, ct, size, &pacedReader{r: body, rate: 8 << 10, chunk: 512})
	t.Logf("%d bytes at 8 KiB/s against a %v window and 32 KiB/s: status %d after %v", size, window, res.status, res.elapsed)
	requireIntakeCut(t, res.status, res.payload, res.elapsed)
	if res.elapsed < window || res.elapsed >= 3*time.Second {
		t.Errorf("cut after %v, want at least the %v window and below 3s", res.elapsed, window)
	}
}

func TestCreateIntakeAdmitsASteadyBodyThatOutlastsTheWindow(t *testing.T) {
	window := 400 * time.Millisecond
	_, ts := newIntakeServer(t, createIntakeConfig(64<<20), window, 32<<10)
	body, ct := createIntakeBody(t, 96<<10)
	size := int64(body.Len())
	res := rawPost(t, ts, "/v1/jobs", nil, ct, size, &pacedReader{r: body, rate: 64 << 10, chunk: 4 << 10})
	t.Logf("%d bytes at 64 KiB/s against a %v window and 32 KiB/s: status %d after %v", size, window, res.status, res.elapsed)
	if res.status != http.StatusAccepted {
		t.Errorf("status %d payload %v after %v, want 202 for a body sent at twice the minimum rate", res.status, res.payload, res.elapsed)
	}
	if res.elapsed < 3*window {
		t.Errorf("the transfer took %v, want at least %v so that it outlasts the window", res.elapsed, 3*window)
	}
}

func TestCreateIntakeUsesTheCreateCap(t *testing.T) {
	window := 300 * time.Millisecond
	cfg := createIntakeConfig(4 << 20)
	cfg.InspectMaxUploadBytes = 64 << 10
	cfg.InspectMaxWorkBytes = 64 << 10
	_, ts := newIntakeServer(t, cfg, window, 256<<10)
	body, ct := createIntakeBody(t, 1<<20)
	size := int64(body.Len())
	res := rawPost(t, ts, "/v1/jobs", nil, ct, size, &pacedReader{r: body, rate: 512 << 10, chunk: 16 << 10})
	t.Logf("%d bytes at 512 KiB/s against a %v window and 256 KiB/s, create cap 4 MiB, inspect cap 64 KiB: status %d after %v", size, window, res.status, res.elapsed)
	if res.status != http.StatusAccepted {
		t.Errorf("status %d payload %v after %v, want 202: the create deadline credits bytes up to the create cap", res.status, res.payload, res.elapsed)
	}
}

func TestCreateIntakeCutMidSpillReleasesEverything(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	repo := &fakeRepo{}
	store := &fakeStorage{}
	window := 1500 * time.Millisecond
	srv, ts := newIntakeServerWith(t, repo, store, createIntakeConfig(64<<20), window, 1<<30)
	body, ct := createIntakeBody(t, multipartInMemoryBudget+4<<20)
	sent := body.Bytes()[:multipartInMemoryBudget+2<<20]

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
	if _, err := fmt.Fprintf(conn, "POST /v1/jobs HTTP/1.1\r\nHost: %s\r\nAuthorization: ApiKey %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n", addr, testClientKey, ct, body.Len()); err != nil {
		t.Fatal(err)
	}
	wrote := make(chan struct{})
	go func() {
		defer close(wrote)
		_, _ = conn.Write(sent)
	}()

	var spilled []string
	for until := time.Now().Add(time.Second); ; time.Sleep(10 * time.Millisecond) {
		if spilled = spillFiles(t, dir); len(spilled) > 0 || time.Now().After(until) {
			break
		}
	}
	if len(spilled) == 0 {
		t.Fatalf("no multipart spill file in %s within 1s of sending %d of %d body bytes", dir, len(sent), body.Len())
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("no response after %v: %v", elapsed, err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	_ = json.Unmarshal(raw, &payload)
	t.Logf("%d of %d bytes sent, then a stall against a %v window: spill %v, status %d after %v", len(sent), body.Len(), window, spilled, resp.StatusCode, elapsed)
	requireIntakeCut(t, resp.StatusCode, payload, elapsed)
	if _, err := br.ReadByte(); err != io.EOF {
		t.Errorf("the next read on the connection returned %v, want EOF", err)
	}
	if left := spillFiles(t, dir); len(left) != 0 {
		t.Errorf("%d multipart spill files outlived the cut: %v", len(left), left)
	}
	for until := time.Now().Add(time.Second); len(srv.createIntakeSem) != 0 && time.Now().Before(until); {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(srv.createIntakeSem); n != 0 {
		t.Errorf("%d create slots still taken 1s after the cut, want 0", n)
	}
	if store.uploaded || store.uploadCalls != 0 {
		t.Errorf("Upload ran %d times after the cut, want 0", store.uploadCalls)
	}
	if repo.created != nil || repo.preAdmitCalls != 0 {
		t.Errorf("Create ran (%v) and PreAdmit ran %d times after the cut, want neither", repo.created != nil, repo.preAdmitCalls)
	}
	conn.Close()
	<-wrote
}

func TestCreateAndInspectIntakePoolsAreIndependent(t *testing.T) {
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), createIntakeConfig(64<<20))

	t.Run("inspect while every create slot is taken", func(t *testing.T) {
		fillIntakeSlots(t, srv.createIntakeSem)
		code, body := doInspect(t, srv, "in.png", pngBytesFixture())
		if code != http.StatusOK {
			t.Errorf("inspect status %d, want 200; body=%s", code, body)
		}
	})

	t.Run("create while every inspect intake slot is taken", func(t *testing.T) {
		fillIntakeSlots(t, srv.inspectIntakeSem)
		body, ct := createIntakeBody(t, 4096)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, createIntakeRequest(body, ct))
		if rec.Code != http.StatusAccepted {
			t.Errorf("create status %d, want 202; body=%s", rec.Code, rec.Body.String())
		}
	})
}
