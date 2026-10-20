package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
)

const chartValues = "../../deploy/chart/octoconv/values.yaml"

func chartAPIMemoryLimit(t *testing.T) int64 {
	t.Helper()
	raw, err := os.ReadFile(chartValues)
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		API struct {
			Resources struct {
				Limits struct {
					Memory string `yaml:"memory"`
				} `yaml:"limits"`
			} `yaml:"resources"`
		} `yaml:"api"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("decode %s: %v", chartValues, err)
	}
	memory := values.API.Resources.Limits.Memory
	m := regexp.MustCompile(`^(\d+)(Gi|Mi)?$`).FindStringSubmatch(memory)
	if m == nil {
		t.Fatalf("%s: api.resources.limits.memory = %q is not <digits>Gi, <digits>Mi or <digits>", chartValues, memory)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		t.Fatalf("%s: api.resources.limits.memory = %q is not a positive int64", chartValues, memory)
	}
	switch m[2] {
	case "Gi":
		n <<= 30
	case "Mi":
		n <<= 20
	}
	return n
}

func liveHeap() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func liveHeapMidIntake(t *testing.T, srv *Server, path string, size int64) uint64 {
	t.Helper()
	base := liveHeap()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	paused, resume := make(chan struct{}), make(chan struct{})
	writeErr := make(chan error, 1)
	go func() {
		fw, err := mw.CreateFormFile(formFieldFile, "intake")
		if err == nil {
			_, err = io.Copy(fw, io.LimitReader(transportCapZeros{}, size-1<<20))
		}
		close(paused)
		<-resume
		if err == nil {
			_, err = io.Copy(fw, io.LimitReader(transportCapZeros{}, 1<<20))
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
		writeErr <- err
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := authed(httptest.NewRequest(http.MethodPost, path, pr))
		req.Header.Set("Content-Type", mw.FormDataContentType())
		srv.Routes().ServeHTTP(httptest.NewRecorder(), req)
		_ = pr.Close()
	}()
	<-paused
	mid := liveHeap()
	close(resume)
	<-done
	if err := <-writeErr; err != nil {
		t.Fatalf("%s: write the request: %v", path, err)
	}
	if mid < base {
		return 0
	}
	return mid - base
}

func TestIntakeHeapFitsThePodMemoryLimit(t *testing.T) {
	limit := chartAPIMemoryLimit(t)
	slots := int64(createIntakeMaxConcurrent + inspectIntakeMaxConcurrent)
	perIntake := limit / 2 / slots
	for _, path := range []string{"/v1/jobs", "/v1/inspect"} {
		t.Run(path, func(t *testing.T) {
			srv := NewServer(&fakeRepo{}, &transportCapStorage{fakeStorage: &fakeStorage{}}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
				MaxUploadBytes:        64 << 20,
				InspectMaxUploadBytes: 64 << 20,
			})
			size := int64(multipartInMemoryBudget) + 16<<20
			var m uint64
			for i := range 3 {
				h := liveHeapMidIntake(t, srv, path, size)
				if i == 0 || h < m {
					m = h
				}
			}
			t.Logf("%s: live heap of one %d-byte intake = %d bytes (%.1f MiB); share of the pod = %d bytes; %d intakes = %d bytes against a %d-byte limit", path, size, m, float64(m)/(1<<20), perIntake, slots, int64(m)*slots, limit)
			if int64(m) > perIntake+1<<20 {
				t.Errorf("%s: one intake holds %d bytes of live heap, more than %d (the pod limit %d / 2 / %d intake slots, plus 1 MiB of measurement slack)", path, m, perIntake+1<<20, limit, slots)
			}
		})
	}
}

type signalStorage struct {
	*fakeStorage
	once     sync.Once
	uploaded chan struct{}
}

func (s *signalStorage) Upload(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	_, err := io.Copy(io.Discard, r)
	s.once.Do(func() { close(s.uploaded) })
	return err
}

type stalledResult struct {
	status  int
	payload map[string]string
	err     error
}

func postWithoutRemainder(t *testing.T, ts *httptest.Server, path, contentType string, body []byte, remainder int) <-chan stalledResult {
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
	out := make(chan stalledResult, 1)
	go func() {
		head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: ApiKey %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n", path, addr, testClientKey, contentType, len(body)+remainder)
		if _, err := io.WriteString(conn, head); err != nil {
			out <- stalledResult{err: err}
			return
		}
		if _, err := conn.Write(body); err != nil {
			out <- stalledResult{err: err}
			return
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			out <- stalledResult{err: err}
			return
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		var payload map[string]string
		if err == nil {
			_ = json.Unmarshal(raw, &payload)
		}
		out <- stalledResult{status: resp.StatusCode, payload: payload, err: err}
	}()
	return out
}

func awaitStalled(t *testing.T, ch <-chan stalledResult) stalledResult {
	t.Helper()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("no response: %v", res.err)
		}
		return res
	case <-time.After(20 * time.Second):
		t.Fatal("no response within 20s")
	}
	return stalledResult{}
}

func heapGrowth(base uint64) int64 {
	return int64(liveHeap()) - int64(base)
}

func TestNoFormMemoryOutlivesTheIntakeSlot(t *testing.T) {
	const fileSize = 12 << 20
	t.Run("create", func(t *testing.T) {
		store := &signalStorage{fakeStorage: &fakeStorage{}, uploaded: make(chan struct{})}
		srv, ts := newIntakeServerWith(t, &fakeRepo{}, store, Config{
			MaxUploadBytes:        64 << 20,
			InspectMaxUploadBytes: 64 << 20,
			InspectMaxWorkBytes:   64 << 20,
			MaxFormatBytes:        map[string]map[string]int64{convert.EnginePandoc: {"md": 64 << 20}},
		}, 3*time.Second, 1<<30)
		buf, ct := multipartBody(t, "in.md", "html", paddedMarkdown(fileSize))
		body := buf.Bytes()
		base := liveHeap()
		res := postWithoutRemainder(t, ts, "/v1/jobs", ct, body, 1024)
		select {
		case <-store.uploaded:
		case <-time.After(5 * time.Second):
			t.Fatal("the create never reached Upload within 5s")
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(srv.createIntakeSem) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("the create handler still holds its intake slot 2s after Upload")
			}
			time.Sleep(5 * time.Millisecond)
		}
		growth := heapGrowth(base)
		t.Logf("create: live heap grew %d bytes (%.1f MiB) after the handler returned, with %d bytes of the body still announced", growth, float64(growth)/(1<<20), 1024)
		if growth >= fileSize/2 {
			t.Errorf("create: %d bytes of live heap outlive the handler and its intake slot, want less than %d", growth, fileSize/2)
		}
		got := awaitStalled(t, res)
		runtime.KeepAlive(body)
		if got.status != http.StatusAccepted {
			t.Errorf("create: status = %d payload %v, want 202", got.status, got.payload)
		}
	})
	t.Run("inspect", func(t *testing.T) {
		srv, ts := newIntakeServer(t, Config{InspectMaxConcurrent: 1, InspectMaxUploadBytes: 64 << 20}, 3*time.Second, 1<<30)
		srv.inspectSem.sem <- struct{}{}
		t.Cleanup(func() { <-srv.inspectSem.sem })
		buf, ct := multipartBody(t, "intake", "", make([]byte, fileSize))
		body := buf.Bytes()
		base := liveHeap()
		res := postWithoutRemainder(t, ts, "/v1/inspect", ct, body, 1024)
		time.Sleep(500 * time.Millisecond)
		growth := heapGrowth(base)
		t.Logf("inspect: live heap grew %d bytes (%.1f MiB) 500ms after the request was sent, with %d bytes of the body still announced", growth, float64(growth)/(1<<20), 1024)
		if growth >= fileSize/2 {
			t.Errorf("inspect: %d bytes of live heap outlive the refused handler, want less than %d", growth, fileSize/2)
		}
		got := awaitStalled(t, res)
		runtime.KeepAlive(body)
		if got.status != http.StatusTooManyRequests || got.payload["code"] != "inspect_busy" {
			t.Errorf("inspect: status = %d payload %v, want 429 inspect_busy", got.status, got.payload)
		}
	})
}
