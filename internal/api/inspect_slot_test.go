package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type signalPreAdmitRepo struct {
	*fakeRepo
	mu    sync.Mutex
	calls int
	once  sync.Once
	hit   chan struct{}
}

func (r *signalPreAdmitRepo) PreAdmit(_ context.Context, _ uuid.UUID, _ string, _ int64) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.once.Do(func() { close(r.hit) })
	return nil
}

func newSlotServer(t *testing.T, cfg Config) (*signalPreAdmitRepo, *Server, *httptest.Server) {
	t.Helper()
	repo := &signalPreAdmitRepo{fakeRepo: &fakeRepo{}, hit: make(chan struct{})}
	cfg.InspectMaxConcurrent = 1
	srv, ts := newIntakeServerWith(t, repo, &fakeStorage{}, cfg, 2*time.Second, 1<<30)
	return repo, srv, ts
}

func stallAfterForm(t *testing.T, ts *httptest.Server, data []byte) (net.Conn, *bufio.Reader) {
	t.Helper()
	body, ct := multipartBody(t, "in.png", "", data)
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
	bStatus, bBody := doInspect(t, srv, "b.png", pngBytesFixture())
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
