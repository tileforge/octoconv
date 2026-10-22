package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/storage"
)

const (
	uploadWorkerHeapBytes = 384 << 10
	uploadHeapBytes       = storage.UploadPartThreads * uploadWorkerHeapBytes
	uploadPartBytes       = 16 << 20
	uploadMaxBytes        = 191889408
	junkPartPause         = 50 * time.Millisecond
)

type holdingS3 struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	puts        int
	firstPut    chan struct{}
	firstOnce   sync.Once
	release     chan struct{}
	releaseOnce sync.Once
}

func newHoldingS3() *holdingS3 {
	return &holdingS3{firstPut: make(chan struct{}), release: make(chan struct{})}
}

func (f *holdingS3) releaseAll() {
	f.releaseOnce.Do(func() { close(f.release) })
}

func (f *holdingS3) held() (inFlight, maxInFlight, puts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlight, f.maxInFlight, f.puts
}

func (f *holdingS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	switch {
	case r.Method == http.MethodGet && q.Has("location"):
		io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
	case r.Method == http.MethodHead && len(parts) == 1:
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && q.Has("uploads"):
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Bucket>%s</Bucket><Key>%s</Key><UploadId>u1</UploadId></InitiateMultipartUploadResult>`, parts[0], parts[1])
	case r.Method == http.MethodPost && q.Has("uploadId"):
		io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Location>x</Location><Bucket>%s</Bucket><Key>%s</Key><ETag>"e"</ETag></CompleteMultipartUploadResult>`, parts[0], parts[1])
	case r.Method == http.MethodPut:
		io.CopyN(io.Discard, r.Body, 32<<10)
		f.mu.Lock()
		f.inFlight++
		f.puts++
		f.maxInFlight = max(f.maxInFlight, f.inFlight)
		f.mu.Unlock()
		f.firstOnce.Do(func() { close(f.firstPut) })
		<-f.release
		io.Copy(io.Discard, r.Body)
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
		w.Header().Set("ETag", `"e1"`)
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func newS3Storage(t *testing.T, h http.Handler) *storage.Client {
	t.Helper()
	s3 := httptest.NewServer(h)
	t.Setenv("S3_ENDPOINT", strings.TrimPrefix(s3.URL, "http://"))
	t.Setenv("S3_ACCESS_KEY", "a")
	t.Setenv("S3_SECRET_KEY", "secretsecret")
	t.Setenv("S3_BUCKET", "bkt")
	t.Setenv("S3_USE_SSL", "false")
	sc, err := storage.New(context.Background())
	t.Cleanup(s3.Close)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func stlOfSize(size int64) (io.Reader, int64) {
	triangles := (size - 84) / 50
	header := make([]byte, 84)
	binary.LittleEndian.PutUint32(header[80:], uint32(triangles))
	return io.MultiReader(bytes.NewReader(header), io.LimitReader(transportCapZeros{}, 50*triangles)), 84 + 50*triangles
}

func uploadParts(size int64) int {
	if size <= uploadPartBytes {
		return 1
	}
	return int((size + uploadPartBytes - 1) / uploadPartBytes)
}

type uploadProbe struct {
	heap     int64
	inFlight int
	want     int
	status   int
	parts    int
	stl      int64
}

func createDuringUpload(t *testing.T, junk []int64, stl int64) uploadProbe {
	t.Helper()
	fake := newHoldingS3()
	sc := newS3Storage(t, fake)
	t.Cleanup(fake.releaseAll)
	srv := NewServer(&fakeRepo{}, sc, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        uploadMaxBytes,
		InspectMaxUploadBytes: uploadMaxBytes,
	})
	ts := startShippedServer(t, srv.Routes())

	file, size := stlOfSize(stl)
	p := uploadProbe{want: min(storage.UploadPartThreads, uploadParts(size)), parts: len(junk) + 5, stl: size}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		for _, n := range junk {
			fw, err := mw.CreateFormFile("junk", "a")
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			if err := copyZeros(fw, n); err != nil {
				pw.CloseWithError(err)
				return
			}
			time.Sleep(junkPartPause)
		}
		pw.CloseWithError(writeTransportCapForm(mw, convert.EngineCAD, convert.OperationRender, "png", "model.stl", file, -1))
	}()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/jobs?q=a", pr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	authed(req)
	client := ts.Client()

	type result struct {
		status int
		body   string
		err    error
	}
	base := liveHeap()
	done := make(chan result, 1)
	go func() {
		resp, err := client.Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		done <- result{status: resp.StatusCode, body: string(b)}
	}()
	select {
	case <-fake.firstPut:
	case r := <-done:
		t.Fatalf("create finished without an upload: %d %v %.200s", r.status, r.err, r.body)
	case <-time.After(60 * time.Second):
		t.Fatal("no PUT reached the fake S3 within 60 s")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		n, _, _ := fake.held()
		if n >= p.want || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	mid := liveHeap()
	p.inFlight, _, _ = fake.held()
	p.heap = int64(mid) - int64(base)
	fake.releaseAll()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("create: %v", r.err)
		}
		p.status = r.status
		if r.status != http.StatusAccepted {
			t.Errorf("create status %d, want 202: %.200s", r.status, r.body)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("create did not answer within 60 s of the release")
	}
	return p
}

func createDuringUploadRun(t *testing.T, name string, junk []int64, stl int64) uploadProbe {
	t.Helper()
	var p uploadProbe
	t.Run(name, func(t *testing.T) {
		p = createDuringUpload(t, junk, stl)
	})
	return p
}

func TestCreateUploadHeapStaysWithinItsTerm(t *testing.T) {
	b := int64(multipartInMemoryBudget)
	junk := worstSplit(b, lastDoubling(b)-(bytes.MinRead-1))
	settled := readFormSettledBound(b)
	configs := intakeHeapConfigs(t)
	createDuringUploadRun(t, "warm-up", junk, 100<<20)
	for _, cs := range []struct {
		name    string
		junk    []int64
		stl     int64
		settled int64
		share   bool
	}{
		{"a STL 100 MiB alone", nil, 100 << 20, 0, false},
		{"b worst split + STL 100 MiB", junk, 100 << 20, settled, true},
		{"c worst split + STL 15 MiB", junk, 15 << 20, settled, true},
	} {
		bound := cs.settled + uploadHeapBytes + intakeFixedHeapBytes
		var worst uploadProbe
		for run := 1; run <= 3; run++ {
			p := createDuringUploadRun(t, fmt.Sprintf("%s run %d", cs.name, run), cs.junk, cs.stl)
			t.Logf("%s run %d: heap %d (%.3f MiB), PUT in flight %d of %d, form parts %d, STL %d, status %d", cs.name, run, p.heap, mib(p.heap), p.inFlight, p.want, p.parts, p.stl, p.status)
			if p.inFlight != p.want {
				t.Errorf("%s run %d: %d PUT in flight at the measurement, want min(%d, %d parts) = %d", cs.name, run, p.inFlight, storage.UploadPartThreads, uploadParts(p.stl), p.want)
			}
			if run == 1 || p.heap > worst.heap {
				worst = p
			}
		}
		t.Logf("%s: max heap %d (%.3f MiB); bound %d (%.3f MiB) = settled form %d + uploadHeapBytes %d + intakeFixedHeapBytes %d", cs.name, worst.heap, mib(worst.heap), bound, mib(bound), cs.settled, uploadHeapBytes, intakeFixedHeapBytes)
		if cs.share {
			for _, c := range configs {
				t.Logf("%s: share of %s = %d (%.3f MiB)", cs.name, c.name, c.share, mib(c.share))
			}
		}
		if worst.heap > bound {
			t.Errorf("%s: create held %d bytes (%.3f MiB) during the upload, above its bound %d (%.3f MiB)", cs.name, worst.heap, mib(worst.heap), bound, mib(bound))
		}
		if !cs.share {
			continue
		}
		for _, c := range configs {
			if worst.heap > c.share {
				t.Errorf("%s: create held %d bytes (%.3f MiB) during the upload, above the share %d (%.3f MiB) of %s", cs.name, worst.heap, mib(worst.heap), c.share, mib(c.share), c.name)
			}
		}
	}
}
