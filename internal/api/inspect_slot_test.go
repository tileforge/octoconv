package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type signalPreAdmitRepo struct {
	*fakeRepo
	mu      sync.Mutex
	calls   int
	once    sync.Once
	hit     chan struct{}
	atFirst func()
}

func (r *signalPreAdmitRepo) PreAdmit(_ context.Context, _ uuid.UUID, _ string, _ int64) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.once.Do(func() {
		if r.atFirst != nil {
			r.atFirst()
		}
		close(r.hit)
	})
	return nil
}

func newSlotServer(t *testing.T, cfg Config) (*signalPreAdmitRepo, *Server, *httptest.Server) {
	t.Helper()
	repo := &signalPreAdmitRepo{fakeRepo: &fakeRepo{}, hit: make(chan struct{})}
	cfg.InspectMaxConcurrent = 1
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), cfg)
	srv.intakeWindow = 2 * time.Second
	srv.intakeMinRate = 1 << 30
	ts := httptest.NewUnstartedServer(srv.Routes())
	ts.Config.ReadHeaderTimeout = 10 * time.Second
	ts.Start()
	t.Cleanup(func() {
		closed := make(chan struct{})
		go func() {
			ts.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Error("the server did not shut down within 10s: a handler never returned")
		}
	})
	return repo, srv, ts
}

func inspectWithin(t *testing.T, srv *Server, filename string, data []byte) (int, []byte) {
	t.Helper()
	body, ct := multipartBody(t, filename, "", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/inspect", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Routes().ServeHTTP(rec, req)
	}()
	select {
	case <-done:
		return rec.Code, rec.Body.Bytes()
	case <-time.After(5 * time.Second):
		t.Fatal("B's handler did not return within 5s")
		return 0, nil
	}
}

func stallAfterForm(t *testing.T, ts *httptest.Server, data []byte) (net.Conn, *bufio.Reader) {
	t.Helper()
	body, ct := multipartBody(t, "in.png", "", data)
	return sendFormAndStall(t, ts, body, ct)
}

func sendFormAndStall(t *testing.T, ts *httptest.Server, body *bytes.Buffer, ct string) (net.Conn, *bufio.Reader) {
	t.Helper()
	addr := ts.Listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	head := fmt.Sprintf("POST /v1/inspect HTTP/1.1\r\nHost: %s\r\nAuthorization: ApiKey %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n", addr, testClientKey, ct, body.Len()+1024)
	if _, err := io.WriteString(conn, head); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(body.Bytes()); err != nil {
		t.Fatal(err)
	}
	return conn, bufio.NewReader(conn)
}

func readStalledInspect(t *testing.T, conn net.Conn, br *bufio.Reader) (int, []byte) {
	t.Helper()
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("A: no response: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("A: read the response body: %v", err)
	}
	return resp.StatusCode, raw
}

func waitPreAdmit(t *testing.T, repo *signalPreAdmitRepo) {
	t.Helper()
	select {
	case <-repo.hit:
	case <-time.After(5 * time.Second):
		t.Fatal("A never reached PreAdmit")
	}
}

func requireDiscardInsideHandler(t *testing.T, status int, raw []byte) {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("A got %d, want 200: %.300s", status, raw)
	}
	if len(raw) <= 2048 {
		t.Fatalf("A's response is %d bytes; at 2048 or less net/http flushes it after the handler returns, so the test cannot see the discard read inside the handler", len(raw))
	}
}

func TestInspectReleasesTheAnalysisSlotBeforeTheDiscardRead(t *testing.T) {
	repo, srv, ts := newSlotServer(t, Config{})

	start := time.Now()
	conn, br := stallAfterForm(t, ts, pngBytesFixture())
	waitPreAdmit(t, repo)
	time.Sleep(200 * time.Millisecond)

	bStart := time.Now()
	bStatus, bBody := inspectWithin(t, srv, "b.png", pngBytesFixture())
	t.Logf("B while A stalls after its form: status %d after %v", bStatus, time.Since(bStart))
	if bStatus != http.StatusOK {
		t.Errorf("B got %d, want 200: A's discard read holds the analysis slot: %.300s", bStatus, bBody)
	}

	status, raw := readStalledInspect(t, conn, br)
	requireDiscardInsideHandler(t, status, raw)
	var resp inspectResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("A: decode: %v", err)
	}
	if len(resp.Operations) == 0 {
		t.Fatal("A: no operations, so no quota verdict was checked")
	}
	unknown := 0
	for _, op := range resp.Operations {
		if op.Quota.Allowed == nil {
			unknown++
			t.Errorf("A: %s %s quota is unknown (%q)", op.Operation, op.Target, op.Quota.Reason)
		}
	}
	t.Logf("A: status %d, %d body bytes, %d operations, %d unknown quota, after %v", status, len(raw), len(resp.Operations), unknown, time.Since(start))
}

func paddedPNG(size int) []byte {
	data := make([]byte, size)
	copy(data, pngBytesFixture())
	return data
}

func requireStalledInTheDiscard(t *testing.T, observedAt, answeredAt time.Duration) {
	t.Helper()
	if answeredAt < 1500*time.Millisecond || answeredAt <= observedAt {
		t.Fatalf("A answered after %v (observed at %v); it did not stall in the discard read, so the observation proves nothing", answeredAt, observedAt)
	}
}

func TestInspectRemovesTheSpillBeforeTheDiscardRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	repo, srv, ts := newSlotServer(t, Config{InspectMaxUploadBytes: 64 << 20})
	var duringAnalysis []string
	repo.atFirst = func() {
		duringAnalysis, _ = filepath.Glob(filepath.Join(dir, "multipart-*"))
	}

	data := paddedPNG(len(pngBytesFixture()) + multipartInMemoryBudget + 1<<20)
	start := time.Now()
	conn, br := stallAfterForm(t, ts, data)
	waitPreAdmit(t, repo)
	if len(duringAnalysis) == 0 {
		t.Fatal("no spill file while A was analysed, so an empty TMPDIR later proves nothing")
	}
	time.Sleep(200 * time.Millisecond)

	bStatus, bBody := inspectWithin(t, srv, "b.png", pngBytesFixture())
	if bStatus != http.StatusOK {
		t.Fatalf("B got %d, want 200: %.300s", bStatus, bBody)
	}
	during := spillFiles(t, dir)
	observedAt := time.Since(start)
	if len(during) != 0 {
		t.Errorf("spill files while A stalls after B: %v; the spill outlives the analysis slot", during)
	}

	status, raw := readStalledInspect(t, conn, br)
	answeredAt := time.Since(start)
	requireDiscardInsideHandler(t, status, raw)
	requireStalledInTheDiscard(t, observedAt, answeredAt)
	t.Logf("spill during analysis %v, during the discard %v; A: %d body bytes after %v", duringAnalysis, during, len(raw), answeredAt)
	runtime.KeepAlive(data)
}

func TestInspectDropsTheFormBeforeTheDiscardRead(t *testing.T) {
	repo, _, ts := newSlotServer(t, Config{InspectMaxUploadBytes: 64 << 20})

	const size = 12 << 20
	data := paddedPNG(size)
	body, ct := multipartBody(t, "in.png", "", data)
	base := liveHeap()
	start := time.Now()
	conn, br := sendFormAndStall(t, ts, body, ct)
	waitPreAdmit(t, repo)
	time.Sleep(200 * time.Millisecond)
	during := liveHeap()
	observedAt := time.Since(start)
	growth := int64(during) - int64(base)
	if growth >= size/2 {
		t.Errorf("live heap grew by %d bytes (%.1f MiB) while A stalls in the discard read; the %d-byte form outlives the analysis slot", growth, float64(growth)/(1<<20), size)
	}

	status, raw := readStalledInspect(t, conn, br)
	answeredAt := time.Since(start)
	requireDiscardInsideHandler(t, status, raw)
	requireStalledInTheDiscard(t, observedAt, answeredAt)
	t.Logf("live heap growth during the discard %d bytes (%.2f MiB); A: %d body bytes after %v", growth, float64(growth)/(1<<20), len(raw), answeredAt)
	runtime.KeepAlive(data)
	runtime.KeepAlive(body)
}
