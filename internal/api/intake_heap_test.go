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
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
)

const chartValues = "../../deploy/chart/octoconv/values.yaml"

const multipartPeekBufferBytes = 4096

var intakeQueryRepeats = (http.DefaultMaxHeaderBytes - 8<<10) / 3

var memoryQuantity = regexp.MustCompile(`^(\d+)(Gi|Mi|g|m)?$`)

func parseMemoryLimit(t *testing.T, where, memory string) int64 {
	t.Helper()
	m := memoryQuantity.FindStringSubmatch(memory)
	if m == nil {
		t.Fatalf("%s: memory limit %q is not <digits> followed by Gi, Mi, g, m or nothing", where, memory)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		t.Fatalf("%s: memory limit %q is not a positive int64", where, memory)
	}
	switch m[2] {
	case "Gi", "g":
		n <<= 30
	case "Mi", "m":
		n <<= 20
	}
	return n
}

func chartAPIMemory(t *testing.T, file string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		API struct {
			Resources struct {
				Limits struct {
					Memory *string `yaml:"memory"`
				} `yaml:"limits"`
			} `yaml:"resources"`
		} `yaml:"api"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	if values.API.Resources.Limits.Memory == nil {
		return "", false
	}
	return *values.API.Resources.Limits.Memory, true
}

type intakeHeapConfig struct {
	name                 string
	memory               int64
	inspectMaxConcurrent int64
	forms                int64
	share                int64
}

func newIntakeHeapConfig(name string, memory, inspectMaxConcurrent int64) intakeHeapConfig {
	forms := int64(createIntakeMaxConcurrent+inspectIntakeMaxConcurrent) + inspectMaxConcurrent
	return intakeHeapConfig{name: name, memory: memory, inspectMaxConcurrent: inspectMaxConcurrent, forms: forms, share: memory / 2 / forms}
}

func intakeHeapConfigs(t *testing.T) []intakeHeapConfig {
	t.Helper()
	baseMemory, ok := chartAPIMemory(t, chartValues)
	if !ok {
		t.Fatalf("%s sets no api.resources.limits.memory", chartValues)
	}
	base := readChartAPISpill(t, chartValues)
	configs := []intakeHeapConfig{newIntakeHeapConfig("values.yaml", parseMemoryLimit(t, chartValues, baseMemory), spillCaps(t, "values.yaml", base.API.ExtraEnv)["INSPECT_MAX_CONCURRENT"])}
	overlays, err := filepath.Glob(transportCapChartDir + "values-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(overlays) == 0 {
		t.Fatalf("no values-*.yaml overlay under %s", transportCapChartDir)
	}
	sort.Strings(overlays)
	for _, f := range overlays {
		name := filepath.Base(f)
		memory := baseMemory
		if m, ok := chartAPIMemory(t, f); ok {
			memory = m
		}
		caps := spillCaps(t, name, base.API.ExtraEnv, readChartAPISpill(t, f).API.ExtraEnv)
		configs = append(configs, newIntakeHeapConfig(name, parseMemoryLimit(t, f, memory), caps["INSPECT_MAX_CONCURRENT"]))
	}

	raw, err := os.ReadFile(transportCapCompose)
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Deploy struct {
				Resources struct {
					Limits struct {
						Memory string `yaml:"memory"`
					} `yaml:"limits"`
				} `yaml:"resources"`
			} `yaml:"deploy"`
			Environment yaml.Node `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatalf("decode %s: %v", transportCapCompose, err)
	}
	api, ok := compose.Services["api"]
	if !ok {
		t.Fatalf("%s has no api service", transportCapCompose)
	}
	var env map[string]any
	if err := api.Environment.Decode(&env); err != nil {
		t.Fatalf("%s services.api.environment is not a map this test can read: %v", transportCapCompose, err)
	}
	inspect, ok := env["INSPECT_MAX_CONCURRENT"]
	if !ok {
		t.Fatalf("%s services.api.environment has no INSPECT_MAX_CONCURRENT", transportCapCompose)
	}
	configs = append(configs, newIntakeHeapConfig("docker-compose.yml api",
		parseMemoryLimit(t, transportCapCompose+" services.api.deploy.resources.limits.memory", api.Deploy.Resources.Limits.Memory),
		transportCapInt(t, transportCapCompose+" services.api.environment", "INSPECT_MAX_CONCURRENT", fmt.Sprint(inspect))))

	if len(configs) < 3 {
		t.Fatalf("%d configurations, want at least three", len(configs))
	}
	return configs
}

func liveHeap() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

type intakeProbe struct {
	heap    uint64
	status  int
	payload map[string]string
	sent    int64
	err     error
}

type intakeForm struct {
	name  string
	write func(mw *multipart.Writer, pause func()) error
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func copyZeros(w io.Writer, n int64) error {
	_, err := io.Copy(w, io.LimitReader(transportCapZeros{}, n))
	return err
}

func writeFileWithPause(mw *multipart.Writer, size int64, pause func()) error {
	fw, err := mw.CreateFormFile(formFieldFile, "intake")
	if err != nil {
		return err
	}
	if err := copyZeros(fw, size-1<<20); err != nil {
		return err
	}
	pause()
	return copyZeros(fw, 1<<20)
}

func writeZeroField(mw *multipart.Writer, name string, size int64) error {
	fw, err := mw.CreateFormField(name)
	if err != nil {
		return err
	}
	return copyZeros(fw, size)
}

func liveHeapMidIntake(t *testing.T, srv *Server, path string, form intakeForm) intakeProbe {
	t.Helper()
	base := liveHeap()
	pr, pw := io.Pipe()
	cw := &countingWriter{w: pw}
	mw := multipart.NewWriter(cw)
	contentType := mw.FormDataContentType()
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	signal := func() { once.Do(func() { close(paused) }) }
	writeErr := make(chan error, 1)
	go func() {
		err := form.write(mw, func() {
			signal()
			<-resume
		})
		signal()
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
		writeErr <- err
	}()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := authed(httptest.NewRequest(http.MethodPost, path+"?q="+strings.Repeat("%41", intakeQueryRepeats), pr))
		req.Header.Set("Content-Type", contentType)
		srv.Routes().ServeHTTP(rec, req)
		_ = pr.Close()
	}()
	<-paused
	mid := liveHeap()
	close(resume)
	<-done
	probe := intakeProbe{status: rec.Code, err: <-writeErr, sent: cw.n}
	_ = json.Unmarshal(rec.Body.Bytes(), &probe.payload)
	if mid > base {
		probe.heap = mid - base
	}
	return probe
}

func minHeapOfThree(t *testing.T, srv *Server, path string, form intakeForm) intakeProbe {
	t.Helper()
	var best intakeProbe
	for i := range 3 {
		p := liveHeapMidIntake(t, srv, path, form)
		if i == 0 || p.heap < best.heap {
			best = p
		}
	}
	return best
}

func heapProbeServer() *Server {
	return NewServer(&fakeRepo{}, &transportCapStorage{fakeStorage: &fakeStorage{}}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        64 << 20,
		InspectMaxUploadBytes: 64 << 20,
	})
}

func mainFilePart(mw *multipart.Writer, headerPad int64) (io.Writer, error) {
	if headerPad == 0 {
		return mw.CreateFormFile(formFieldFile, "intake")
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="intake"`)
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Pad", strings.Repeat("p", int(headerPad)))
	return mw.CreatePart(h)
}

func admissibleFileSize(mode string) int64 {
	if mode == "memory" {
		return multipartInMemoryBudget
	}
	return int64(multipartInMemoryBudget) + 16<<20
}

func admissibleForm(kind, mode string, a, b int64) intakeForm {
	nonFile := func(mw *multipart.Writer) error {
		switch kind {
		case "value":
			return writeZeroField(mw, "x_pad", a)
		case "parts":
			for i := int64(1); i <= a; i++ {
				name := fmt.Sprintf("f%04d", i)
				if i == a {
					name += strings.Repeat("p", int(b))
				}
				if _, err := mw.CreateFormFile(name, "a"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	var headerPad int64
	if kind == "header" {
		headerPad = a
	}
	size := admissibleFileSize(mode)
	return intakeForm{kind + " " + mode, func(mw *multipart.Writer, pause func()) error {
		if mode == "memory" {
			fw, err := mainFilePart(mw, headerPad)
			if err != nil {
				return err
			}
			if err := copyZeros(fw, size); err != nil {
				return err
			}
			if err := nonFile(mw); err != nil {
				return err
			}
			pause()
			return nil
		}
		if err := nonFile(mw); err != nil {
			return err
		}
		fw, err := mainFilePart(mw, headerPad)
		if err != nil {
			return err
		}
		if err := copyZeros(fw, size-1<<20); err != nil {
			return err
		}
		pause()
		return copyZeros(fw, 1<<20)
	}}
}

func nonFileBytesOf(t *testing.T, form intakeForm, fileContent int64) int64 {
	t.Helper()
	cw := &countingWriter{w: io.Discard}
	mw := multipart.NewWriter(cw)
	if err := form.write(mw, func() {}); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return cw.n - fileContent
}

func admissibleFormOnTheLine(t *testing.T, kind, mode string) intakeForm {
	t.Helper()
	line := int64(maxFormNonFileBytes - multipartPeekBufferBytes)
	size := admissibleFileSize(mode)
	var form intakeForm
	if kind == "parts" {
		base := nonFileBytesOf(t, admissibleForm(kind, mode, 0, 0), size)
		cost := nonFileBytesOf(t, admissibleForm(kind, mode, 1, 0), size) - base
		form = admissibleForm(kind, mode, (line-base)/cost, (line-base)%cost)
	} else {
		one := nonFileBytesOf(t, admissibleForm(kind, mode, 1, 0), size)
		form = admissibleForm(kind, mode, 1+line-one, 0)
	}
	if got := nonFileBytesOf(t, form, size); got != line {
		t.Fatalf("%s: the form carries %d bytes of non-file data, want exactly %d", form.name, got, line)
	}
	return form
}

func TestIntakeHeapFitsThePodMemoryLimit(t *testing.T) {
	configs := intakeHeapConfigs(t)
	smallest := configs[0]
	for _, c := range configs {
		t.Logf("%s: api memory limit %d, INSPECT_MAX_CONCURRENT %d, %d live forms, share %d bytes (%.1f MiB)", c.name, c.memory, c.inspectMaxConcurrent, c.forms, c.share, float64(c.share)/(1<<20))
		if c.share < smallest.share {
			smallest = c
		}
	}
	spillSize := int64(multipartInMemoryBudget) + 16<<20
	probeField := int64(10<<20 - 64<<10)
	fileOnly := intakeForm{"file only", func(mw *multipart.Writer, pause func()) error {
		return writeFileWithPause(mw, spillSize, pause)
	}}
	verifierProbe := intakeForm{"verifier probe", func(mw *multipart.Writer, pause func()) error {
		if err := writeZeroField(mw, "x_unknown", probeField); err != nil {
			return err
		}
		return writeFileWithPause(mw, spillSize, pause)
	}}
	forms := []intakeForm{fileOnly, verifierProbe}
	for _, kind := range []string{"value", "parts", "header"} {
		for _, mode := range []string{"memory", "spill"} {
			forms = append(forms, admissibleFormOnTheLine(t, kind, mode))
		}
	}
	for _, path := range []string{"/v1/jobs", "/v1/inspect"} {
		for _, form := range forms {
			t.Run(path+" "+form.name, func(t *testing.T) {
				p := minHeapOfThree(t, heapProbeServer(), path, form)
				m := int64(p.heap)
				t.Logf("%s %s: live heap mid-intake m = %d bytes (%.1f MiB); smallest share = %d bytes (%s: limit %d / 2 / %d forms); m × %d = %d; status %d %v; %d bytes sent", path, form.name, m, float64(m)/(1<<20), smallest.share, smallest.name, smallest.memory, smallest.forms, smallest.forms, m*smallest.forms, p.status, p.payload, p.sent)
				if form.name == verifierProbe.name {
					if p.status != http.StatusRequestEntityTooLarge || p.payload["code"] != "payload_too_large" || p.payload["error"] != formFieldsTooLargeMessage {
						t.Errorf("%s: status %d payload %v, want 413 payload_too_large %q", path, p.status, p.payload, formFieldsTooLargeMessage)
					}
					if p.sent > maxFormNonFileBytes+64<<10 {
						t.Errorf("%s: the handler read %d bytes of the body, want at most %d", path, p.sent, maxFormNonFileBytes+64<<10)
					}
					return
				}
				if p.err != nil || p.payload["error"] == formFieldsTooLargeMessage {
					t.Fatalf("%s: an admissible form was not read whole (write error %v, status %d %v)", path, p.err, p.status, p.payload)
				}
				for _, c := range configs {
					if m > c.share {
						t.Errorf("%s %s: one form holds %d bytes of live heap, more than the share %d of %s (limit %d / 2 / %d live forms)", path, form.name, m, c.share, c.name, c.memory, c.forms)
					}
				}
			})
		}
	}
}

func TestIntakeBudgetIsDerivedFromThePodShare(t *testing.T) {
	configs := intakeHeapConfigs(t)
	var want int64
	for i, c := range configs {
		w := ((c.share - maxFormNonFileBytes - http.DefaultMaxHeaderBytes) / 2) >> 20 << 20
		t.Logf("%s: share %d = %d / 2 / (%d + %d + %d); share - non-file bound %d - request header %d = %d; / 2 = %d; whole MiB = %d (%d MiB)", c.name, c.share, c.memory, createIntakeMaxConcurrent, inspectIntakeMaxConcurrent, c.inspectMaxConcurrent, maxFormNonFileBytes, http.DefaultMaxHeaderBytes, c.share-maxFormNonFileBytes-http.DefaultMaxHeaderBytes, (c.share-maxFormNonFileBytes-http.DefaultMaxHeaderBytes)/2, w, w>>20)
		if i == 0 || w < want {
			want = w
		}
	}
	if multipartInMemoryBudget != want {
		t.Errorf("multipartInMemoryBudget = %d, want %d: the largest whole MiB with 2 × budget + %d + %d within the smallest share", multipartInMemoryBudget, want, maxFormNonFileBytes, http.DefaultMaxHeaderBytes)
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

func TestDropMultipartFormLeavesNothingReachable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	body, ct := multipartBody(t, "in.md", "html", paddedMarkdown(multipartInMemoryBudget+1<<20))
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", body)
	req.Header.Set("Content-Type", ct)
	if err := req.ParseMultipartForm(multipartInMemoryBudget); err != nil {
		t.Fatal(err)
	}
	if len(spillFiles(t, dir)) == 0 || req.MultipartForm == nil || req.Form == nil || req.PostForm == nil {
		t.Fatalf("the fixture did not spill and set the form: spill %v", spillFiles(t, dir))
	}
	dropMultipartForm(req)
	if left := spillFiles(t, dir); len(left) != 0 {
		t.Errorf("%d spill files outlived dropMultipartForm: %v", len(left), left)
	}
	if req.MultipartForm != nil || req.Form != nil || req.PostForm != nil {
		t.Errorf("dropMultipartForm left MultipartForm %v, Form %v, PostForm %v reachable from the request", req.MultipartForm != nil, req.Form != nil, req.PostForm != nil)
	}
	dropMultipartForm(req)
}
