package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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
