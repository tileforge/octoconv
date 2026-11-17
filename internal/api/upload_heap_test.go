package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
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
	head     int
}

type uploadDoc struct {
	size  int64
	parts int
	write func(mw *multipart.Writer) error
}

func stlDoc(stl int64) uploadDoc {
	_, size := stlOfSize(stl)
	return uploadDoc{size: size, parts: 5, write: func(mw *multipart.Writer) error {
		file, _ := stlOfSize(stl)
		return writeTransportCapForm(mw, convert.EngineCAD, convert.OperationRender, "png", "model.stl", file, -1)
	}}
}

func latexDoc() uploadDoc {
	data := latexFixture()
	return uploadDoc{size: int64(len(data)), parts: 2, write: func(mw *multipart.Writer) error {
		fw, err := mw.CreateFormFile(formFieldFile, "paper.tex")
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
		if err := mw.WriteField(formFieldTarget, "pdf"); err != nil {
			return err
		}
		return mw.Close()
	}}
}

func createDuringUpload(t *testing.T, junk []int64, stl int64) uploadProbe {
	t.Helper()
	return createDuringUploadWith(t, junk, stlDoc(stl), "")
}

func createDuringUploadWith(t *testing.T, junk []int64, doc uploadDoc, blockKind string) uploadProbe {
	t.Helper()
	fake := newHoldingS3()
	sc := newS3Storage(t, fake)
	t.Cleanup(fake.releaseAll)
	srv := NewServer(&fakeRepo{}, sc, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        uploadMaxBytes,
		InspectMaxUploadBytes: uploadMaxBytes,
	})
	admitCADCatalog(srv)
	ts := startShippedServer(t, srv.Routes())

	boundary := multipart.NewWriter(io.Discard).Boundary()
	body := func(w io.Writer, gap time.Duration) error {
		mw := multipart.NewWriter(w)
		if err := mw.SetBoundary(boundary); err != nil {
			return err
		}
		for _, n := range junk {
			fw, err := mw.CreateFormFile("junk", "a")
			if err != nil {
				return err
			}
			if err := copyZeros(fw, n); err != nil {
				return err
			}
			time.Sleep(gap)
		}
		return doc.write(mw)
	}
	cw := &countingWriter{w: io.Discard}
	if err := body(cw, 0); err != nil {
		t.Fatal(err)
	}
	ct := "multipart/form-data; boundary=" + boundary
	block := ""
	if blockKind != "" {
		block = headerBlockOnTheLine(blockKind, "/v1/jobs", shortIntakeQuery, ct, cw.n)
	}
	head := intakeHead("/v1/jobs", shortIntakeQuery, block, ct, cw.n)
	if len(head) > headerReadLimit {
		t.Fatalf("the head is %d bytes, above headerReadLimit %d", len(head), headerReadLimit)
	}
	p := uploadProbe{want: min(storage.UploadPartThreads, uploadParts(doc.size)), parts: len(junk) + doc.parts, stl: doc.size, head: len(head)}

	type result struct {
		status int
		body   string
		err    error
	}
	br := bufio.NewReaderSize(nil, 4096)
	base := liveHeap()
	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		if _, err := io.WriteString(conn, head); err == nil {
			_ = body(conn, junkPartPause)
		}
	}()
	done := make(chan result, 1)
	go func() {
		br.Reset(conn)
		resp, err := http.ReadResponse(br, nil)
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
	runtime.KeepAlive(head)
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
	return createDuringUploadWithRun(t, name, junk, stlDoc(stl), "")
}

func createDuringUploadWithRun(t *testing.T, name string, junk []int64, doc uploadDoc, blockKind string) uploadProbe {
	t.Helper()
	var p uploadProbe
	t.Run(name, func(t *testing.T) {
		p = createDuringUploadWith(t, junk, doc, blockKind)
	})
	return p
}

func TestCreateUploadHeapStaysWithinItsTerm(t *testing.T) {
	b := int64(multipartInMemoryBudget)
	junk := worstSplit(b, lastDoubling(b)-(bytes.MinRead-1))
	settled := readFormSettledBound(b)
	classTerm := createFormHeapBytes(b)
	createDuringUploadRun(t, "warm-up", junk, 100<<20)
	for _, cs := range []struct {
		name    string
		junk    []int64
		stl     int64
		settled int64
		class   bool
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
		t.Logf("%s: max heap %d (%.3f MiB); bound %d (%.3f MiB) = settled form %d + uploadHeapBytes %d + intakeFixedHeapBytes %d; createFormHeapBytes(%d) = %d (%.3f MiB)", cs.name, worst.heap, mib(worst.heap), bound, mib(bound), cs.settled, uploadHeapBytes, intakeFixedHeapBytes, b, classTerm, mib(classTerm))
		if worst.heap > bound {
			t.Errorf("%s: create held %d bytes (%.3f MiB) during the upload, above its bound %d (%.3f MiB)", cs.name, worst.heap, mib(worst.heap), bound, mib(bound))
		}
		if cs.class && worst.heap > classTerm {
			t.Errorf("%s: create held %d bytes (%.3f MiB) during the upload, above createFormHeapBytes(%d) = %d (%.3f MiB)", cs.name, worst.heap, mib(worst.heap), b, classTerm, mib(classTerm))
		}
	}
}

func TestUploadTermIsMeasuredAlone(t *testing.T) {
	createDuringUploadRun(t, "warm-up", nil, 100<<20)
	for _, stl := range []int64{15 << 20, 20 << 20, 100 << 20} {
		var worst uploadProbe
		for run := 1; run <= 3; run++ {
			p := createDuringUploadRun(t, fmt.Sprintf("STL %d MiB run %d", stl>>20, run), nil, stl)
			t.Logf("STL %d MiB run %d: heap %d (%.3f MiB) with %d PUT in flight, %d per PUT", stl>>20, run, p.heap, mib(p.heap), p.inFlight, p.heap/int64(max(p.inFlight, 1)))
			if p.inFlight != p.want {
				t.Errorf("STL %d MiB run %d: %d PUT in flight, want %d", stl>>20, run, p.inFlight, p.want)
			}
			if run == 1 || p.heap > worst.heap {
				worst = p
			}
		}
		perPut := worst.heap / int64(max(worst.inFlight, 1))
		t.Logf("STL %d MiB: max heap %d (%.3f MiB) with %d PUT in flight, %d per PUT; uploadWorkerHeapBytes %d", stl>>20, worst.heap, mib(worst.heap), worst.inFlight, perPut, uploadWorkerHeapBytes)
		if stl == 100<<20 && perPut > uploadWorkerHeapBytes {
			t.Errorf("STL %d MiB: %d bytes per PUT in flight, above uploadWorkerHeapBytes %d", stl>>20, perPut, uploadWorkerHeapBytes)
		}
	}
}

type capturedObject struct {
	key string
	sum [sha256.Size]byte
	n   int64
}

type capturingStorage struct {
	*fakeStorage
	mu      sync.Mutex
	objects []capturedObject
}

func (s *capturingStorage) Upload(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	h := sha256.New()
	n, err := io.Copy(h, r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects = append(s.objects, capturedObject{key: key, sum: [sha256.Size]byte(h.Sum(nil)), n: n})
	return err
}

func (s *capturingStorage) uploaded() []capturedObject {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedObject(nil), s.objects...)
}

type validatedPart struct {
	name string
	data func() io.Reader
}

func sumOf(t *testing.T, r io.Reader) capturedObject {
	t.Helper()
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		t.Fatal(err)
	}
	return capturedObject{sum: [sha256.Size]byte(h.Sum(nil)), n: n}
}

func bytesPart(name string, b []byte) validatedPart {
	return validatedPart{name: name, data: func() io.Reader { return bytes.NewReader(b) }}
}

func jfifJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, nil); err != nil {
		t.Fatal(err)
	}
	raw := enc.Bytes()
	app0 := []byte{0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
	out := append(append(append([]byte{}, raw[:2]...), app0...), raw[2:]...)
	var markers []byte
	for i := 2; i+4 <= len(out) && out[i] == 0xFF; i += 2 + int(binary.BigEndian.Uint16(out[i+2:])) {
		markers = append(markers, out[i+1])
		if out[i+1] == 0xDA {
			break
		}
	}
	if !bytes.HasPrefix(out, []byte{0xFF, 0xD8}) || !bytes.HasSuffix(out, []byte{0xFF, 0xD9}) || !bytes.Equal(markers, []byte{0xE0, 0xDB, 0xC0, 0xC4, 0xDA}) {
		t.Fatalf("the JPEG is not SOI, APP0, DQT, SOF0, DHT, SOS, EOI: segments % X, %d bytes", markers, len(out))
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 24 || cfg.Height != 16 {
		t.Fatalf("the JPEG decodes to %dx%d (%v), want 24x16", cfg.Width, cfg.Height, err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("the JPEG does not decode: %v", err)
	}
	return out
}

func TestCreateUploadsTheValidatedBytes(t *testing.T) {
	stl := func() io.Reader { r, _ := largestAdmissibleSTL(); return r }
	cases := []struct {
		name    string
		target  string
		op      string
		parts   []validatedPart
		formats map[string]map[string]int64
	}{
		{"png Dimensions", "webp", "", []validatedPart{bytesPart("in.png", pngBytesFixture())}, nil},
		{"jpeg Dimensions", "webp", "", []validatedPart{bytesPart("in.jpg", jfifJPEG(t))}, nil},
		{"mp4 Sniff", "mp3", "", []validatedPart{bytesPart("in.mp4", mp4BytesFixture())}, nil},
		{"mkv SniffVideo", "mp4", "", []validatedPart{bytesPart("in.mkv", mkvBytesFixture())}, nil},
		{"webm SniffVideo", "mp4", "", []validatedPart{bytesPart("in.webm", webmBytesFixture())}, nil},
		{"mp3 SniffAudio", "txt", "", []validatedPart{bytesPart("in.mp3", audioMP3Fixture(t))}, nil},
		{"docx container", "pdf", "", []validatedPart{bytesPart("in.docx", docxFixture(t))}, nil},
		{"html", "pdf", "", []validatedPart{bytesPart("in.html", htmlFixture())}, nil},
		{"latex", "pdf", "", []validatedPart{bytesPart("in.tex", latexFixture())}, nil},
		{"pdf", "txt", "", []validatedPart{bytesPart("scan.pdf", pdfBytesFixture())}, nil},
		{"markdown spilled above 16 MiB", "html", "", []validatedPart{bytesPart("in.md", paddedMarkdown(uploadPartBytes+1<<20))}, map[string]map[string]int64{convert.EnginePandoc: {"md": 64 << 20}}},
		{"stl at the CAD cap", "png", convert.OperationRender, []validatedPart{{name: "model.stl", data: stl}}, nil},
		{"tex with an attachment", "pdf", "", []validatedPart{bytesPart("paper.tex", []byte(texMain)), bytesPart("preamble.tex", []byte("\\newcommand{\\x}{y}\n"))}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &capturingStorage{fakeStorage: &fakeStorage{}}
			srv, ts := newIntakeServerWith(t, &fakeRepo{}, store, Config{
				MaxUploadBytes:        uploadMaxBytes,
				InspectMaxUploadBytes: uploadMaxBytes,
				MaxFormatBytes:        tc.formats,
			}, intakeReadDeadline, intakeMinBytesPerSecond)
			admitCADCatalog(srv)
			if tc.name == "docx container" {
				srv.SetCapabilityCatalog(documentCatalog())
			}
			pr, pw := io.Pipe()
			mw := multipart.NewWriter(pw)
			go func() {
				for _, p := range tc.parts {
					fw, err := mw.CreateFormFile(formFieldFile, p.name)
					if err != nil {
						pw.CloseWithError(err)
						return
					}
					if _, err := io.Copy(fw, p.data()); err != nil {
						pw.CloseWithError(err)
						return
					}
				}
				fields := [][2]string{{formFieldTarget, tc.target}}
				if tc.op != "" {
					fields = append(fields, [2]string{formFieldOperation, tc.op})
				}
				for _, f := range fields {
					if err := mw.WriteField(f[0], f[1]); err != nil {
						pw.CloseWithError(err)
						return
					}
				}
				pw.CloseWithError(mw.Close())
			}()
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/jobs", pr)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", mw.FormDataContentType())
			resp, err := ts.Client().Do(authed(req))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("status %d, want 202: %.300s", resp.StatusCode, body)
			}
			got := store.uploaded()
			if len(got) != len(tc.parts) {
				t.Fatalf("%d objects uploaded, want %d", len(got), len(tc.parts))
			}
			for i, p := range tc.parts {
				want := sumOf(t, p.data())
				t.Logf("ordinal %d %s: object %q %d bytes sha256 %x, file part %d bytes sha256 %x", i, p.name, got[i].key, got[i].n, got[i].sum, want.n, want.sum)
				if !strings.Contains(got[i].key, fmt.Sprintf("/%d-", i)) {
					t.Errorf("upload %d went to key %q, want ordinal %d", i, got[i].key, i)
				}
				if got[i].n != want.n || got[i].sum != want.sum {
					t.Errorf("ordinal %d %s: stored %d bytes sha256 %x, the validated file part is %d bytes sha256 %x", i, p.name, got[i].n, got[i].sum, want.n, want.sum)
				}
			}
		})
	}
}
