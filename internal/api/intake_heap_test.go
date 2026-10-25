package api

import (
	"bufio"
	"bytes"
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
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/storage"
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
	inspectWorkBytes     int64
}

func newIntakeHeapConfig(name string, memory, inspectMaxConcurrent, inspectWorkBytes int64) intakeHeapConfig {
	return intakeHeapConfig{name: name, memory: memory, inspectMaxConcurrent: inspectMaxConcurrent, inspectWorkBytes: inspectWorkBytes}
}

func chartInspectWorkBytes(t *testing.T, where string, extraEnvs ...map[string]any) int64 {
	t.Helper()
	work := chartLiteral(t, "INSPECT_MAX_WORK_BYTES")
	for _, env := range extraEnvs {
		if v, ok := env["INSPECT_MAX_WORK_BYTES"]; ok {
			work = transportCapInt(t, where+" api.extraEnv", "INSPECT_MAX_WORK_BYTES", fmt.Sprint(v))
		}
	}
	return work
}

func intakeHeapConfigs(t *testing.T) []intakeHeapConfig {
	t.Helper()
	baseMemory, ok := chartAPIMemory(t, chartValues)
	if !ok {
		t.Fatalf("%s sets no api.resources.limits.memory", chartValues)
	}
	base := readChartAPISpill(t, chartValues)
	configs := []intakeHeapConfig{newIntakeHeapConfig("values.yaml", parseMemoryLimit(t, chartValues, baseMemory), spillCaps(t, "values.yaml", base.API.ExtraEnv)["INSPECT_MAX_CONCURRENT"], chartInspectWorkBytes(t, "values.yaml", base.API.ExtraEnv))}
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
		overlay := readChartAPISpill(t, f).API.ExtraEnv
		caps := spillCaps(t, name, base.API.ExtraEnv, overlay)
		configs = append(configs, newIntakeHeapConfig(name, parseMemoryLimit(t, f, memory), caps["INSPECT_MAX_CONCURRENT"], chartInspectWorkBytes(t, name, base.API.ExtraEnv, overlay)))
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
	work := heapProbeServer().inspectMaxWorkBytes
	if w, ok := env["INSPECT_MAX_WORK_BYTES"]; ok {
		work = transportCapInt(t, transportCapCompose+" services.api.environment", "INSPECT_MAX_WORK_BYTES", fmt.Sprint(w))
	}
	configs = append(configs, newIntakeHeapConfig("docker-compose.yml api",
		parseMemoryLimit(t, transportCapCompose+" services.api.deploy.resources.limits.memory", api.Deploy.Resources.Limits.Memory),
		transportCapInt(t, transportCapCompose+" services.api.environment", "INSPECT_MAX_CONCURRENT", fmt.Sprint(inspect)), work))

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

func liveHeapOf(t *testing.T, srv *Server, path, query string, header http.Header, contentType string, write func(w io.Writer, pause func()) error, settle time.Duration) intakeProbe {
	t.Helper()
	base := liveHeap()
	pr, pw := io.Pipe()
	cw := &countingWriter{w: pw}
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	signal := func() { once.Do(func() { close(paused) }) }
	writeErr := make(chan error, 1)
	go func() {
		err := write(cw, func() {
			signal()
			<-resume
		})
		signal()
		pw.CloseWithError(err)
		writeErr <- err
	}()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := authed(httptest.NewRequest(http.MethodPost, path+"?"+query, pr))
		req.Header.Set("Content-Type", contentType)
		for k, v := range header {
			req.Header[k] = v
		}
		srv.Routes().ServeHTTP(rec, req)
		_ = pr.Close()
	}()
	<-paused
	if settle > 0 {
		time.Sleep(settle)
	}
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

func intakeHead(path, query, headerBlock, contentType string, contentLength int64) string {
	target := path
	if query != "" {
		target += "?" + query
	}
	return "POST " + target + " HTTP/1.1\r\nHost: x\r\nAuthorization: ApiKey " + testClientKey + "\r\nContent-Type: " + contentType + "\r\nContent-Length: " + strconv.FormatInt(contentLength, 10) + "\r\n" + headerBlock + "\r\n"
}

type tcpAnswer struct {
	status  int
	payload map[string]string
	err     error
}

const tcpSettleFloor = 100 * time.Millisecond

func liveHeapOverTCP(t *testing.T, ts *httptest.Server, path, query, headerBlock, contentType string, contentLength int64, write func(io.Writer, func()) error, settle time.Duration) intakeProbe {
	t.Helper()
	head := intakeHead(path, query, headerBlock, contentType, contentLength)
	if len(head) > headerReadLimit {
		t.Fatalf("%s: the head is %d bytes, above headerReadLimit %d: the shipped server answers it 431 before any handler", path, len(head), headerReadLimit)
	}
	br := bufio.NewReaderSize(nil, 4096)
	paused, resume := make(chan struct{}), make(chan struct{})
	writeErr := make(chan error, 1)
	answered := make(chan tcpAnswer, 1)
	base := liveHeap()
	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cw := &countingWriter{w: conn}
	var once sync.Once
	signal := func() { once.Do(func() { close(paused) }) }
	go func() {
		_, err := io.WriteString(conn, head)
		if err == nil {
			err = write(cw, func() {
				signal()
				<-resume
			})
		}
		signal()
		writeErr <- err
	}()
	go func() {
		br.Reset(conn)
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			answered <- tcpAnswer{err: err}
			return
		}
		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		a := tcpAnswer{status: resp.StatusCode, err: err}
		_ = json.Unmarshal(raw, &a.payload)
		answered <- a
	}()
	var early *tcpAnswer
	select {
	case <-paused:
	case a := <-answered:
		early = &a
		select {
		case <-paused:
		case <-time.After(10 * time.Second):
		}
	case <-time.After(2 * time.Minute):
		t.Fatalf("%s: the writer did not reach its pause within 2 minutes", path)
	}
	if early == nil {
		select {
		case a := <-answered:
			early = &a
		default:
		}
	}
	time.Sleep(max(settle, tcpSettleFloor))
	mid := liveHeap()
	runtime.KeepAlive(head)
	close(resume)
	probe := intakeProbe{sent: cw.n}
	a := early
	if a == nil {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		select {
		case got := <-answered:
			a = &got
		case <-time.After(2*time.Minute + 5*time.Second):
			t.Fatalf("%s: no answer within 2 minutes of the resume", path)
		}
	}
	_ = conn.Close()
	select {
	case probe.err = <-writeErr:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: the writer did not return within 30 s of the answer", path)
	}
	probe.status, probe.payload = a.status, a.payload
	if probe.err == nil && a.err != nil {
		probe.err = a.err
	}
	if probe.err == nil && early != nil {
		probe.err = fmt.Errorf("the answer %d %v came before the writer paused", a.status, a.payload)
	}
	if mid > base {
		probe.heap = mid - base
	}
	return probe
}

func rawFormLength(f rawIntakeForm) int64 {
	return rawFormFileBytes(f) + rawFormNonFileBytes(f)
}

func liveHeapOfRawOverTCP(t *testing.T, ts *httptest.Server, path, query, headerBlock string, form rawIntakeForm, settle time.Duration) intakeProbe {
	t.Helper()
	return liveHeapOverTCP(t, ts, path, query, headerBlock, "multipart/form-data; boundary="+rawFormBoundary, rawFormLength(form), form.write, settle)
}

func liveHeapMidIntake(t *testing.T, srv *Server, path string, form intakeForm) intakeProbe {
	t.Helper()
	boundary := multipart.NewWriter(io.Discard).Boundary()
	return liveHeapOf(t, srv, path, "q="+strings.Repeat("%41", intakeQueryRepeats), nil, "multipart/form-data; boundary="+boundary, func(w io.Writer, pause func()) error {
		mw := multipart.NewWriter(w)
		if err := mw.SetBoundary(boundary); err != nil {
			return err
		}
		if err := form.write(mw, pause); err != nil {
			return err
		}
		return mw.Close()
	}, 0)
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

const (
	readFormMaxParts     = 1000
	readFormMaxHeaders   = 10000
	nonFileHeapPerByte   = 32
	requestLineCopies    = 3
	urlMaxQueryParams    = 10000
	queryParamHeapBytes  = 256
	intakeFixedHeapBytes = 256 << 10
	rawFormBoundary      = "b"
	rawFilePartHeader    = "Content-Disposition:form-data;name=" + formFieldFile + ";filename=a"
)

func lastDoubling(budget int64) int64 {
	p := int64(bytes.MinRead)
	for 2*p <= budget+bytes.MinRead {
		p *= 2
	}
	return p
}

func readFormContentBound(budget int64) int64 {
	return 2*budget + lastDoubling(budget) + 2*bytes.MinRead*(readFormMaxParts+1)
}

func readFormSettledBound(budget int64) int64 {
	return 2*budget + 2*bytes.MinRead*(readFormMaxParts+1)
}

type intakeHeapTerms struct {
	content   int64
	settled   int64
	nonFile   int64
	request   int64
	fixed     int64
	postParse int64
	total     int64
}

func intakeHeapModel(budget, postParse int64) intakeHeapTerms {
	m := intakeHeapTerms{
		content:   readFormContentBound(budget),
		settled:   readFormSettledBound(budget),
		nonFile:   nonFileHeapPerByte * maxFormNonFileBytes,
		request:   (requestLineCopies+headerHeapPerByte)*headerReadLimit + urlMaxQueryParams*queryParamHeapBytes,
		fixed:     intakeFixedHeapBytes,
		postParse: postParse,
	}
	m.total = max(m.content, m.settled+m.postParse) + m.nonFile + m.request + m.fixed
	return m
}

const createRetainedPath = "latex_falls_through"

func createRetainedHeapBytes() int64 {
	_, sums := createSniffHeapTerm()
	return sums[createRetainedPath]
}

func createPostParseHeapBytes() int64 {
	return max(createSniffHeapBytes, uploadHeapBytes+createRetainedHeapBytes())
}

func postParseHeapBytes(t *testing.T) int64 {
	t.Helper()
	return max(createPostParseHeapBytes(), inspectAnalysisHeapBytes(inspectWorkBytes(t)))
}

func createFormHeapBytes(budget int64) int64 {
	return intakeHeapModel(budget, createPostParseHeapBytes()).total
}

func inspectIntakeFormHeapBytes(budget int64) int64 {
	m := intakeHeapModel(budget, 0)
	return m.content + m.nonFile + m.request + m.fixed
}

func analysisFormHeapBytes(budget, work int64) int64 {
	m := intakeHeapModel(budget, inspectAnalysisHeapBytes(work))
	return m.settled + m.postParse + m.nonFile + m.request + m.fixed
}

type intakeHeapClassTerms struct {
	createForm        int64
	inspectIntakeForm int64
	analysisForm      int64
	sum               int64
	half              int64
}

func intakeHeapClasses(budget int64, c intakeHeapConfig) intakeHeapClassTerms {
	k := intakeHeapClassTerms{
		createForm:        createFormHeapBytes(budget),
		inspectIntakeForm: inspectIntakeFormHeapBytes(budget),
		analysisForm:      analysisFormHeapBytes(budget, c.inspectWorkBytes),
		half:              c.memory / 2,
	}
	k.sum = createIntakeMaxConcurrent*k.createForm + inspectIntakeMaxConcurrent*k.inspectIntakeForm + c.inspectMaxConcurrent*k.analysisForm
	return k
}

func intakeClassTermFor(path string, budget int64) (string, int64) {
	if path == "/v1/jobs" {
		return "createFormHeapBytes", createFormHeapBytes(budget)
	}
	return "inspectIntakeFormHeapBytes", inspectIntakeFormHeapBytes(budget)
}

func leadPieces(d int64) []int64 {
	var pieces []int64
	for d > 0 {
		c := int64(2 * bytes.MinRead)
		for 2*c-(bytes.MinRead-1) <= d {
			c *= 2
		}
		piece := min(c-(bytes.MinRead-1), d)
		pieces = append(pieces, piece)
		d -= piece
	}
	return pieces
}

func worstSplit(budget, last int64) []int64 {
	return append(leadPieces(budget-last), last)
}

type rawSegment struct {
	raw   string
	zeros int64
	pause bool
}

type rawIntakeForm struct {
	name     string
	segments []rawSegment
}

func rawPartOpening(first bool) string {
	if first {
		return "--" + rawFormBoundary + "\r\n"
	}
	return "\r\n--" + rawFormBoundary + "\r\n"
}

func rawFileParts(name string, sizes []int64, beforeLast []rawSegment, spillTail int64) rawIntakeForm {
	return rawFilePartsWithLastHeader(name, sizes, beforeLast, spillTail, "")
}

func rawFilePartsWithLastHeader(name string, sizes []int64, beforeLast []rawSegment, spillTail int64, lastHeader string) rawIntakeForm {
	return rawFilePartsWithLast(name, sizes, beforeLast, spillTail, rawFilePartHeader, lastHeader)
}

func rawFilePartsWithLast(name string, sizes []int64, beforeLast []rawSegment, spillTail int64, lastDisposition, lastHeader string) rawIntakeForm {
	var segments []rawSegment
	for i, size := range sizes {
		header := rawFilePartHeader + "\r\n"
		if i == len(sizes)-1 {
			segments = append(segments, beforeLast...)
			header = lastDisposition + "\r\n" + lastHeader
		}
		segments = append(segments, rawSegment{raw: rawPartOpening(len(segments) == 0) + header + "\r\n", zeros: size})
	}
	segments = append(segments, rawSegment{pause: true})
	if spillTail > 0 {
		segments = append(segments, rawSegment{zeros: spillTail})
	}
	segments = append(segments, rawSegment{raw: "\r\n--" + rawFormBoundary + "--\r\n"})
	return rawIntakeForm{name: name, segments: segments}
}

func (f rawIntakeForm) write(w io.Writer, pause func()) error {
	for i, s := range f.segments {
		if s.pause {
			pause()
			continue
		}
		if _, err := io.WriteString(w, s.raw); err != nil {
			return err
		}
		if err := copyZeros(w, s.zeros); err != nil {
			return err
		}
		if s.zeros > 0 && (i+1 == len(f.segments) || !f.segments[i+1].pause) {
			time.Sleep(rawSegmentGap(s.zeros))
		}
	}
	return nil
}

func rawSegmentGap(zeros int64) time.Duration {
	return 5*time.Millisecond + time.Duration(zeros)*20*time.Millisecond/(1<<20)
}

func rawFormFileBytes(f rawIntakeForm) int64 {
	var n int64
	for _, s := range f.segments {
		n += s.zeros
	}
	return n
}

func rawFormNonFileBytes(f rawIntakeForm) int64 {
	var n int64
	for _, s := range f.segments {
		n += int64(len(s.raw))
	}
	return n
}

func rawFormParts(f rawIntakeForm) int {
	var n int
	for _, s := range f.segments {
		n += strings.Count(s.raw, "--"+rawFormBoundary+"\r\n")
	}
	return n
}

func liveHeapOfRaw(t *testing.T, srv *Server, path, query string, header http.Header, form rawIntakeForm, settle time.Duration) intakeProbe {
	t.Helper()
	return liveHeapOf(t, srv, path, query, header, "multipart/form-data; boundary="+rawFormBoundary, form.write, settle)
}

type heapRange struct {
	min  int64
	max  int64
	last intakeProbe
}

func heapOfThree(probe func() intakeProbe) heapRange {
	var r heapRange
	for i := range 3 {
		p := probe()
		m := int64(p.heap)
		if i == 0 || m < r.min {
			r.min = m
		}
		if i == 0 || m > r.max {
			r.max = m
		}
		r.last = p
	}
	return r
}

func readWhole(p intakeProbe) bool {
	return p.err == nil && p.status != http.StatusRequestEntityTooLarge && p.payload["error"] != "invalid multipart form" && p.payload["error"] != formFieldsTooLargeMessage
}

const shortIntakeQuery = "q=a"

const heapSettle = 250 * time.Millisecond

type heapLimit struct {
	name  string
	bytes int64
}

type rawHeapCase struct {
	form   rawIntakeForm
	query  string
	header http.Header
	settle time.Duration
	upper  []heapLimit
	floor  int64
}

func mib(n int64) float64 {
	return float64(n) / (1 << 20)
}

func budgetHeapCases() []rawHeapCase {
	b := int64(multipartInMemoryBudget)
	p := lastDoubling(b)
	x := p - (bytes.MinRead - 1)
	content := []heapLimit{{"readFormContentBound + intakeFixedHeapBytes", readFormContentBound(b) + intakeFixedHeapBytes}}
	pending := worstSplit(b, x)
	spill := spillLast(worstSplit(b, x))
	cases := []rawHeapCase{
		{form: rawFileParts("worst pending", pending, nil, 0), query: shortIntakeQuery, upper: content},
		{form: rawFileParts("worst spill", spill, nil, 1<<20), query: shortIntakeQuery, upper: content},
		{form: rawFileParts("worst spill settled", spill, nil, 1<<20), query: shortIntakeQuery, settle: heapSettle, upper: content, floor: 2*b - 1<<20},
		{form: rawFileParts("worst spill settled p8", spillLast(worstSplit(b, p/8-(bytes.MinRead-1))), nil, 1<<20), query: shortIntakeQuery, settle: heapSettle, upper: content, floor: 2*b - 1<<20},
	}

	gapPending := []int64{8 << 20, 4 << 20}
	gapSpill := []int64{8 << 20, 19 << 20}
	gapPendingEmpty := append(append([]int64{}, gapPending...), make([]int64, emptyPartsToTheLine(rawFileParts("", gapPending, nil, 0)))...)
	gapSpillEmpty := emptyFileParts(emptyPartsToTheLine(rawFileParts("", gapSpill, nil, 1<<20)))
	cases = append(cases,
		rawHeapCase{form: rawFileParts("gap 8MiB+4MiB", gapPending, nil, 0), query: percentIntakeQuery()},
		rawHeapCase{form: rawFileParts("gap 8MiB+spill", gapSpill, nil, 1<<20), query: percentIntakeQuery()},
		rawHeapCase{form: rawFileParts("gap 8MiB+4MiB+empty parts", gapPendingEmpty, nil, 0), query: percentIntakeQuery()},
		rawHeapCase{form: rawFileParts("gap 8MiB+spill+empty parts", gapSpill, gapSpillEmpty, 1<<20), query: percentIntakeQuery()},
	)

	cases = append(cases,
		rawHeapCase{form: rawFileParts("doubling+rest", []int64{p, b - p}, nil, 0), query: shortIntakeQuery, upper: content},
		rawHeapCase{form: rawFileParts("doubling+spill", []int64{p, b - p + 1}, nil, 1<<20), query: shortIntakeQuery, upper: content},
	)

	plus := plusIntakeQuery()
	cases = append(cases,
		rawHeapCase{form: rawFileParts("worst pending+empty parts+query", pending, emptyFileParts(emptyPartsToTheLine(rawFileParts("", pending, nil, 0))), 0), query: plus},
		rawHeapCase{form: rawFileParts("worst spill+empty parts+query", spill, emptyFileParts(emptyPartsToTheLine(rawFileParts("", spill, nil, 1<<20))), 1<<20), query: plus},
		rawHeapCase{form: rawFileParts("worst pending+header lines+query", pending, headerLineParts(20, headerLinePartsToTheLine(20, rawFileParts("", pending, nil, 0))), 0), query: plus},
		rawHeapCase{form: rawFileParts("worst spill+header lines+query", spill, headerLineParts(20, headerLinePartsToTheLine(20, rawFileParts("", spill, nil, 1<<20))), 1<<20), query: plus},
		rawHeapCase{form: rawFileParts("worst pending+empty parts+escaped params", pending, emptyFileParts(emptyPartsToTheLine(rawFileParts("", pending, nil, 0))), 0), query: distinctParamsQuery("+")},
		rawHeapCase{form: rawFileParts("worst spill+empty parts+escaped params", spill, emptyFileParts(emptyPartsToTheLine(rawFileParts("", spill, nil, 1<<20))), 1<<20), query: distinctParamsQuery("+")},
	)

	cases = append(cases, rawHeapCase{form: rawFileParts("tiny", []int64{1}, nil, 0), query: shortIntakeQuery, upper: []heapLimit{{"intakeFixedHeapBytes", intakeFixedHeapBytes}}})

	for _, level := range []int64{p, p / 2, p / 4, p / 8} {
		for _, d := range []int64{-513, -512, -511, 0, 1, 511} {
			last := level + d
			if last <= 0 || last > b {
				continue
			}
			cases = append(cases,
				rawHeapCase{form: rawFileParts(fmt.Sprintf("sweep p=%d d=%d pending", level, d), worstSplit(b, last), nil, 0), query: shortIntakeQuery, upper: content},
				rawHeapCase{form: rawFileParts(fmt.Sprintf("sweep p=%d d=%d spill", level, d), spillLast(worstSplit(b, last)), nil, 1<<20), query: shortIntakeQuery, upper: content},
			)
		}
	}
	return cases
}

func spillLast(sizes []int64) []int64 {
	out := append([]int64{}, sizes...)
	out[len(out)-1]++
	return out
}

func distinctParamsQuery(char string) string {
	var sb strings.Builder
	for i := 0; i < urlMaxQueryParams-1; i++ {
		next := fmt.Sprintf("k%d=%s&", i, strings.Repeat(char, 90))
		if sb.Len()+len(next) > http.DefaultMaxHeaderBytes-16<<10 {
			break
		}
		sb.WriteString(next)
	}
	return sb.String()
}

func percentIntakeQuery() string {
	return "q=" + strings.Repeat("%41", intakeQueryRepeats)
}

func plusIntakeQuery() string {
	return "q=" + strings.Repeat("+", http.DefaultMaxHeaderBytes-8<<10)
}

func intakeAdmitLine() int64 {
	return maxFormNonFileBytes - multipartPeekBufferBytes
}

func emptyFilePart() string {
	return rawPartOpening(false) + rawFilePartHeader + "\r\n\r\n"
}

func emptyFileParts(n int) []rawSegment {
	return []rawSegment{{raw: strings.Repeat(emptyFilePart(), n)}}
}

func partsToTheLine(form rawIntakeForm, cost int64, limit int) int {
	byBytes := (intakeAdmitLine() - rawFormNonFileBytes(form)) / cost
	byParts := int64(readFormMaxParts - rawFormParts(form) - 1)
	return int(max(0, min(byBytes, byParts, int64(limit))))
}

func emptyPartsToTheLine(form rawIntakeForm) int {
	return partsToTheLine(form, int64(len(emptyFilePart())), readFormMaxParts)
}

func headerLinePart(lines string) string {
	return rawPartOpening(false) + rawFilePartHeader + "\r\n" + lines + "\r\n"
}

func repeatedHeaderLines(h int) string {
	return strings.Repeat("A:\n", h)
}

func distinctHeaderLines(h int) string {
	var sb strings.Builder
	for i := range h {
		fmt.Fprintf(&sb, "K%02d:\n", i)
	}
	return sb.String()
}

func headerLineParts(h, n int) []rawSegment {
	return []rawSegment{{raw: strings.Repeat(headerLinePart(repeatedHeaderLines(h)), n)}}
}

func headerLinePartsToTheLine(h int, form rawIntakeForm) int {
	return partsToTheLine(form, int64(len(headerLinePart(repeatedHeaderLines(h)))), (readFormMaxHeaders-100)/(h+1))
}

func distinctHeaderParts(h int, form rawIntakeForm) []rawSegment {
	part := headerLinePart(distinctHeaderLines(h))
	n := partsToTheLine(form, int64(len(part)), (readFormMaxHeaders-100)/(h+1))
	return []rawSegment{{raw: strings.Repeat(part, n)}}
}

func namedValueParts(form rawIntakeForm) []rawSegment {
	room := intakeAdmitLine() - rawFormNonFileBytes(form)
	parts := readFormMaxParts - rawFormParts(form) - 1
	var sb strings.Builder
	for i := 1; i <= parts; i++ {
		part := rawPartOpening(false) + fmt.Sprintf("Content-Disposition:form-data;name=v%d\r\n\r\n", i)
		if int64(sb.Len()+len(part)) > room {
			break
		}
		sb.WriteString(part)
	}
	return []rawSegment{{raw: sb.String()}}
}

func worstPendingForm(name string, beforeLast []rawSegment, lastHeader string) rawIntakeForm {
	b := int64(multipartInMemoryBudget)
	return rawFilePartsWithLastHeader(name, worstSplit(b, lastDoubling(b)-(bytes.MinRead-1)), beforeLast, 0, lastHeader)
}

func rfc2231Disposition(budget, pieces int) string {
	d := "Content-Disposition:form-data;name=" + formFieldFile + ";filename=a"
	for i := 0; ; i++ {
		var p strings.Builder
		for j := range pieces {
			fmt.Fprintf(&p, ";k%x*%d=v", i, j)
		}
		if len(d)+p.Len() > budget {
			return d
		}
		d += p.String()
	}
}

func rfc2231Params(form rawIntakeForm) int {
	var n int
	for _, seg := range form.segments {
		n += strings.Count(seg.raw, "*")
	}
	return n
}

func rfc2231Part(budget, pieces int) string {
	return rawPartOpening(false) + rfc2231Disposition(budget, pieces) + "\r\n\r\n"
}

func rfc2231Parts(form rawIntakeForm, budget, pieces int) []rawSegment {
	part := rfc2231Part(budget, pieces)
	n := partsToTheLine(form, int64(len(part)), readFormMaxParts)
	return []rawSegment{{raw: strings.Repeat(part, n)}}
}

func worstPendingFormWithDisposition(name string, beforeLast []rawSegment, lastDisposition string) rawIntakeForm {
	b := int64(multipartInMemoryBudget)
	return rawFilePartsWithLast(name, worstSplit(b, lastDoubling(b)-(bytes.MinRead-1)), beforeLast, 0, lastDisposition, "")
}

func rfc2231OneHeaderForm() rawIntakeForm {
	room := intakeAdmitLine() - rawFormNonFileBytes(worstPendingForm("", nil, "")) + int64(len(rawFilePartHeader))
	return worstPendingFormWithDisposition("rfc2231 one header", nil, rfc2231Disposition(int(room), 1))
}

func rfc2231ContinuationsForm() rawIntakeForm {
	pending := worstPendingForm("", nil, "")
	return worstPendingForm("rfc2231 continuations", rfc2231Parts(pending, 512, 2), "")
}

type tcpTermCase struct {
	form        rawIntakeForm
	query       string
	headerBlock string
}

func termIncrementOverTCP(t *testing.T, path string, base, hc tcpTermCase) (heapRange, heapRange) {
	t.Helper()
	ts := startShippedServer(t, heapProbeServer().Routes())
	measure := func(c tcpTermCase) heapRange {
		return heapOfThree(func() intakeProbe {
			return liveHeapOfRawOverTCP(t, ts, path, c.query, c.headerBlock, c.form, heapSettle)
		})
	}
	b := measure(base)
	r := measure(hc)
	for _, p := range []intakeProbe{b.last, r.last} {
		if p.status == http.StatusTooManyRequests {
			t.Fatalf("%s %s: answered 429 %v: the measurement ran into a rate limit", path, hc.form.name, p.payload)
		}
	}
	if !readWhole(b.last) || !readWhole(r.last) {
		t.Fatalf("%s %s: a form was not read whole (base: write error %v, status %d %v; form: write error %v, status %d %v)", path, hc.form.name, b.last.err, b.last.status, b.last.payload, r.last.err, r.last.status, r.last.payload)
	}
	return b, r
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

func raceInstrumentedBuild() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, s := range info.Settings {
		if s.Key == "-race" && s.Value == "true" {
			return true
		}
	}
	return false
}

func logIntakeClasses(t *testing.T, configs []intakeHeapConfig, budget int64) {
	t.Helper()
	for _, c := range configs {
		k := intakeHeapClasses(budget, c)
		t.Logf("%s: api memory limit %d, half %d; at budget %d: %d x createFormHeapBytes %d + %d x inspectIntakeFormHeapBytes %d + INSPECT_MAX_CONCURRENT %d x analysisFormHeapBytes(work %d) %d = %d (%.2f MiB); fits %v",
			c.name, c.memory, k.half, budget, createIntakeMaxConcurrent, k.createForm, inspectIntakeMaxConcurrent, k.inspectIntakeForm, c.inspectMaxConcurrent, c.inspectWorkBytes, k.analysisForm, k.sum, mib(k.sum), k.sum <= k.half)
	}
}

func headerBlockOnTheLine(kind, path, query, contentType string, contentLength int64) string {
	return headerBlock(kind, headerReadLimit-len(intakeHead(path, query, "", contentType, contentLength))-16)
}

func TestIntakeHeapFitsThePodMemoryLimit(t *testing.T) {
	if raceInstrumentedBuild() {
		t.Skip("built with -race: go1.26.4 cmd/compile disables isAppendOfMake under instrumentation, so bytes.growSlice holds an extra temporary buffer; this test measures the shipped, uninstrumented build (D-24), proven by the runs without -race")
	}
	configs := intakeHeapConfigs(t)
	logIntakeClasses(t, configs, multipartInMemoryBudget)
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
	b := int64(multipartInMemoryBudget)
	x := lastDoubling(b) - (bytes.MinRead - 1)
	pendingForm := rawFileParts("worst pending", worstSplit(b, x), nil, 0)
	spillForm := rawFileParts("worst spill", spillLast(worstSplit(b, x)), nil, 1<<20)
	for _, path := range []string{"/v1/jobs", "/v1/inspect"} {
		termName, term := intakeClassTermFor(path, b)
		for _, form := range forms {
			t.Run(path+" "+form.name, func(t *testing.T) {
				p := minHeapOfThree(t, heapProbeServer(), path, form)
				m := int64(p.heap)
				t.Logf("%s %s: live heap mid-intake m = %d bytes (%.1f MiB); %s(%d) = %d (%.2f MiB); status %d %v; %d bytes sent", path, form.name, m, mib(m), termName, b, term, mib(term), p.status, p.payload, p.sent)
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
				if m > term {
					t.Errorf("%s %s: one form holds %d bytes of live heap, more than %s(%d) = %d", path, form.name, m, termName, b, term)
				}
			})
		}
		var worstName string
		var worstMax, pendingMax int64
		for _, hc := range budgetHeapCases() {
			t.Run(path+" "+hc.form.name, func(t *testing.T) {
				r := checkRawHeapCase(t, path, hc)
				if r.max > worstMax {
					worstName, worstMax = hc.form.name, r.max
				}
				if hc.form.name == "worst pending" {
					pendingMax = r.max
				}
			})
		}
		for _, base := range []rawIntakeForm{pendingForm, spillForm} {
			t.Run(path+" "+base.name+"+header block", func(t *testing.T) {
				ts := startShippedServer(t, heapProbeServer().Routes())
				ct := "multipart/form-data; boundary=" + rawFormBoundary
				block := headerBlockOnTheLine("distinct-lf", path, shortIntakeQuery, ct, rawFormLength(base))
				without := heapOfThree(func() intakeProbe { return liveHeapOfRawOverTCP(t, ts, path, shortIntakeQuery, "", base, 0) })
				with := heapOfThree(func() intakeProbe { return liveHeapOfRawOverTCP(t, ts, path, shortIntakeQuery, block, base, 0) })
				head := len(intakeHead(path, shortIntakeQuery, block, ct, rawFormLength(base)))
				t.Logf("%s %s+header block over TCP through the shipped server: head %d bytes (headerReadLimit %d), %d header lines; without the block min %d max %d; with it min %d max %d (%.2f MiB); block holds min-min %d (%.3f MiB); %s(%d) = %d (%.2f MiB); status %d %v", path, base.name, head, headerReadLimit, strings.Count(block, "\n"), without.min, without.max, with.min, with.max, mib(with.max), with.min-without.min, mib(with.min-without.min), termName, b, term, mib(term), with.last.status, with.last.payload)
				if !readWhole(without.last) || !readWhole(with.last) {
					t.Fatalf("%s %s: a form was not read whole (without: %v %d %v; with: %v %d %v)", path, base.name, without.last.err, without.last.status, without.last.payload, with.last.err, with.last.status, with.last.payload)
				}
				if with.max > term {
					t.Errorf("%s %s+header block: one form holds up to %d bytes of live heap, more than %s(%d) = %d", path, base.name, with.max, termName, b, term)
				}
				if with.min-without.min < 512<<10 {
					t.Errorf("%s %s+header block: the parsed header block holds %d bytes, less than 512 KiB: the measurement does not reach the server's header parse", path, base.name, with.min-without.min)
				}
			})
		}
		t.Logf("%s summary: largest max %d (%.2f MiB) on %q; %s(%d) = %d (%.2f MiB); worst pending max / readFormContentBound(%d) = %d / %d = %.3f",
			path, worstMax, mib(worstMax), worstName, termName, b, term, mib(term), b, pendingMax, readFormContentBound(b), float64(pendingMax)/float64(readFormContentBound(b)))
	}
}

func checkRawHeapCase(t *testing.T, path string, hc rawHeapCase) heapRange {
	t.Helper()
	line := int64(maxFormNonFileBytes - multipartPeekBufferBytes)
	b := int64(multipartInMemoryBudget)
	termName, term := intakeClassTermFor(path, b)
	srv := heapProbeServer()
	r := heapOfThree(func() intakeProbe {
		return liveHeapOfRaw(t, srv, path, hc.query, hc.header, hc.form, hc.settle)
	})
	nonFile := rawFormNonFileBytes(hc.form)
	t.Logf("%s %s: %d parts, %d file bytes, %d non-file bytes (line %d), query %d bytes, settle %v; live heap min %d (%.2f MiB) max %d (%.2f MiB); %s(%d) = %d (%.2f MiB); floor %d; upper %v; status %d %v; %d bytes sent",
		path, hc.form.name, rawFormParts(hc.form), rawFormFileBytes(hc.form), nonFile, line, len(hc.query), hc.settle, r.min, mib(r.min), r.max, mib(r.max), termName, b, term, mib(term), hc.floor, hc.upper, r.last.status, r.last.payload, r.last.sent)
	if !readWhole(r.last) {
		t.Fatalf("%s %s: an admissible form was not read whole (write error %v, status %d %v)", path, hc.form.name, r.last.err, r.last.status, r.last.payload)
	}
	if nonFile > line {
		t.Errorf("%s %s: the form carries %d bytes of non-file data, above the admit line %d", path, hc.form.name, nonFile, line)
	}
	if r.max > term {
		t.Errorf("%s %s: one form holds up to %d bytes of live heap, more than %s(%d) = %d", path, hc.form.name, r.max, termName, b, term)
	}
	for _, u := range hc.upper {
		if r.max > u.bytes {
			t.Errorf("%s %s: one form holds up to %d bytes of live heap, more than %s %d", path, hc.form.name, r.max, u.name, u.bytes)
		}
	}
	if r.min < hc.floor {
		t.Errorf("%s %s: the form holds at least %d bytes of live heap, less than %d: the generator no longer builds the worst split", path, hc.form.name, r.min, hc.floor)
	}
	return r
}

func intakeBudgetFor(configs []intakeHeapConfig, fits func(budget int64, c intakeHeapConfig) bool) int64 {
	var want int64
	for n := int64(1); n <= 64; n++ {
		ok := true
		for _, c := range configs {
			if !fits(n<<20, c) {
				ok = false
			}
		}
		if ok {
			want = n << 20
		}
	}
	return want
}

func TestIntakeBudgetIsDerivedFromTheSlotClassSum(t *testing.T) {
	configs := intakeHeapConfigs(t)
	want := intakeBudgetFor(configs, func(budget int64, c intakeHeapConfig) bool {
		k := intakeHeapClasses(budget, c)
		return k.sum <= k.half
	})
	if want == 0 {
		t.Fatalf("no whole MiB from 1 to 64 keeps the slot-class sum within half the pod memory limit in every shipped configuration")
	}
	retained := createRetainedHeapBytes()
	_, sums := createSniffHeapTerm()
	t.Logf("inputs: lastDoubling, readFormMaxParts %d, bytes.MinRead %d; nonFileHeapPerByte %d x maxFormNonFileBytes %d; requestLineCopies %d, headerHeapPerByte %d, headerReadLimit %d (apiMaxHeaderBytes %d + %d), urlMaxQueryParams %d x queryParamHeapBytes %d; intakeFixedHeapBytes %d; createSniffHeapBytes %d; uploadHeapBytes %d = %d x %d; createRetainedHeapBytes %d (path %s of the create sniffer inventory: %d); createPostParseHeapBytes %d = max(createSniffHeapBytes, uploadHeapBytes + createRetainedHeapBytes)",
		readFormMaxParts, bytes.MinRead, nonFileHeapPerByte, maxFormNonFileBytes, requestLineCopies, headerHeapPerByte, headerReadLimit, apiMaxHeaderBytes, netHTTPHeaderSlack, urlMaxQueryParams, queryParamHeapBytes, intakeFixedHeapBytes, createSniffHeapBytes, uploadHeapBytes, storage.UploadPartThreads, uploadWorkerHeapBytes, retained, createRetainedPath, sums[createRetainedPath], createPostParseHeapBytes())
	for _, b := range []int64{want, want + 1<<20} {
		for _, c := range configs {
			analysis := inspectAnalysisHeapBytes(c.inspectWorkBytes)
			m := intakeHeapModel(b, analysis)
			k := intakeHeapClasses(b, c)
			t.Logf("budget %d (%d MiB), %s (memory %d, INSPECT_MAX_CONCURRENT %d, INSPECT_MAX_WORK_BYTES %d): lastDoubling %d; content %d = 2 x %d + %d + 2 x %d x (%d + 1); settled %d = 2 x %d + 2 x %d x (%d + 1); nonFile %d; request %d = (%d + %d) x %d + %d x %d; fixed %d; createForm %d = max(content %d, settled %d + createPostParse %d) + nonFile + request + fixed; inspectIntakeForm %d = content + nonFile + request + fixed; analysisForm %d = settled + inspectAnalysisHeapBytes(%d) %d + nonFile + request + fixed; sum %d x %d + %d x %d + %d x %d = %d (%.2f MiB); half %d (%.2f MiB); fits %v",
				b, b>>20, c.name, c.memory, c.inspectMaxConcurrent, c.inspectWorkBytes, lastDoubling(b), m.content, b, lastDoubling(b), bytes.MinRead, readFormMaxParts, m.settled, b, bytes.MinRead, readFormMaxParts, m.nonFile, m.request, requestLineCopies, headerHeapPerByte, headerReadLimit, urlMaxQueryParams, queryParamHeapBytes, m.fixed,
				k.createForm, m.content, m.settled, createPostParseHeapBytes(), k.inspectIntakeForm, k.analysisForm, c.inspectWorkBytes, analysis,
				createIntakeMaxConcurrent, k.createForm, inspectIntakeMaxConcurrent, k.inspectIntakeForm, c.inspectMaxConcurrent, k.analysisForm, k.sum, mib(k.sum), k.half, mib(k.half), k.sum <= k.half)
		}
	}
	postParse := postParseHeapBytes(t)
	share := intakeBudgetFor(configs, func(budget int64, c intakeHeapConfig) bool {
		return intakeHeapModel(budget, postParse).total <= c.memory/2/(createIntakeMaxConcurrent+inspectIntakeMaxConcurrent+c.inspectMaxConcurrent)
	})
	for _, c := range configs {
		forms := createIntakeMaxConcurrent + inspectIntakeMaxConcurrent + c.inspectMaxConcurrent
		t.Logf("for comparison, the equal share (no longer the rule, D-30): %s share %d = %d / 2 / %d forms; intakeHeapModel(1 MiB, postParse %d).total = %d", c.name, c.memory/2/forms, c.memory, forms, postParse, intakeHeapModel(1<<20, postParse).total)
	}
	if share == 0 {
		t.Logf("for comparison, the equal share derives no budget: no whole MiB from 1 to 64 keeps intakeHeapModel(budget, postParseHeapBytes %d).total within memory / 2 / forms of every configuration", postParse)
	} else {
		t.Logf("for comparison, the equal share derives %d (%d MiB)", share, share>>20)
	}
	t.Logf("the slot-class sum derives %d (%d MiB); multipartInMemoryBudget = %d", want, want>>20, multipartInMemoryBudget)
	for _, c := range configs {
		k := intakeHeapClasses(multipartInMemoryBudget, c)
		t.Logf("intakeHeapClasses(multipartInMemoryBudget, %s): sum %d, half %d, fits %v", c.name, k.sum, k.half, k.sum <= k.half)
	}
	if multipartInMemoryBudget != want {
		t.Errorf("multipartInMemoryBudget = %d, want %d: the budget is derived from the slot-class sum (D-30), the largest whole MiB whose sum of createIntakeMaxConcurrent create forms, inspectIntakeMaxConcurrent inspect intake forms and INSPECT_MAX_CONCURRENT analysis forms stays within half the pod memory limit of every shipped configuration", multipartInMemoryBudget, want)
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

type termHeapCase struct {
	form   rawIntakeForm
	query  string
	header http.Header
}

func termIncrement(t *testing.T, path string, base, hc termHeapCase) (heapRange, heapRange) {
	t.Helper()
	srv := heapProbeServer()
	measure := func(c termHeapCase) heapRange {
		return heapOfThree(func() intakeProbe {
			return liveHeapOfRaw(t, srv, path, c.query, c.header, c.form, heapSettle)
		})
	}
	b := measure(base)
	r := measure(hc)
	if !readWhole(b.last) || !readWhole(r.last) {
		t.Fatalf("%s %s: a form was not read whole (base: write error %v, status %d %v; form: write error %v, status %d %v)", path, hc.form.name, b.last.err, b.last.status, b.last.payload, r.last.err, r.last.status, r.last.payload)
	}
	return b, r
}

func TestIntakeHeapModelCoversNonFileData(t *testing.T) {
	line := intakeAdmitLine()
	bound := int64(nonFileHeapPerByte * maxFormNonFileBytes)
	base := termHeapCase{form: worstPendingForm("base", nil, ""), query: shortIntakeQuery}
	pending := base.form
	forms := []rawIntakeForm{worstPendingForm("empty file parts", emptyFileParts(emptyPartsToTheLine(pending)), "")}
	for _, h := range []int{1, 10, 20, 100, 500} {
		forms = append(forms, worstPendingForm(fmt.Sprintf("header lines %d", h), headerLineParts(h, headerLinePartsToTheLine(h, pending)), ""))
	}
	forms = append(forms,
		worstPendingForm("distinct header keys", distinctHeaderParts(20, pending), ""),
		worstPendingForm("named values", namedValueParts(pending), ""),
		worstPendingForm("file header pad", nil, "X-Pad:"+strings.Repeat("p", int(line-rawFormNonFileBytes(pending)-int64(len("X-Pad:\r\n"))))+"\r\n"),
	)
	rfc := []rawIntakeForm{rfc2231ContinuationsForm(), rfc2231OneHeaderForm()}
	for _, path := range []string{"/v1/jobs", "/v1/inspect"} {
		for _, form := range rfc {
			t.Run(path+" "+form.name, func(t *testing.T) {
				nonFile := rawFormNonFileBytes(form)
				b, r := termIncrementOverTCP(t, path, tcpTermCase{form: base.form, query: shortIntakeQuery}, tcpTermCase{form: form, query: shortIntakeQuery})
				inc := r.max - b.min
				t.Logf("%s %s over TCP through the shipped server: %d parts, %d non-file bytes (base %d, line %d), %d RFC 2231 parameters; settled live heap base min %d max %d, form min %d (%.3f MiB) max %d; increment max-min %d (%.3f MiB); nonFile = nonFileHeapPerByte %d x maxFormNonFileBytes %d = %d (%.3f MiB); status %d %v",
					path, form.name, rawFormParts(form), nonFile, rawFormNonFileBytes(pending), line, rfc2231Params(form), b.min, b.max, r.min, mib(r.min), r.max, inc, mib(inc), nonFileHeapPerByte, maxFormNonFileBytes, bound, mib(bound), r.last.status, r.last.payload)
				if nonFile > line {
					t.Errorf("%s %s: the form carries %d bytes of non-file data, above the admit line %d", path, form.name, nonFile, line)
				}
				if rfc2231Params(form) < 1000 {
					t.Errorf("%s %s: the form carries %d RFC 2231 parameters, the generator no longer fills the line", path, form.name, rfc2231Params(form))
				}
				if inc > bound {
					t.Errorf("%s %s: RFC 2231 parameters add %d bytes of live heap, more than nonFileHeapPerByte x maxFormNonFileBytes = %d", path, form.name, inc, bound)
				}
			})
		}
		for _, form := range forms {
			t.Run(path+" "+form.name, func(t *testing.T) {
				nonFile := rawFormNonFileBytes(form)
				b, r := termIncrement(t, path, base, termHeapCase{form: form, query: shortIntakeQuery})
				inc := r.max - b.min
				t.Logf("%s %s: %d parts, %d non-file bytes (base %d, line %d); settled live heap base min %d max %d, form min %d (%.3f MiB) max %d; increment max-min %d (%.3f MiB); nonFileHeapPerByte × maxFormNonFileBytes = %d (%.3f MiB); status %d %v",
					path, form.name, rawFormParts(form), nonFile, rawFormNonFileBytes(pending), line, b.min, b.max, r.min, mib(r.min), r.max, inc, mib(inc), bound, mib(bound), r.last.status, r.last.payload)
				if nonFile > line {
					t.Errorf("%s %s: the form carries %d bytes of non-file data, above the admit line %d", path, form.name, nonFile, line)
				}
				if inc > bound {
					t.Errorf("%s %s: non-file data adds %d bytes of live heap, more than nonFileHeapPerByte × maxFormNonFileBytes = %d", path, form.name, inc, bound)
				}
			})
		}
	}
}

func TestIntakeHeapModelCoversTheRequestLine(t *testing.T) {
	bound := int64(requestLineCopies*http.DefaultMaxHeaderBytes + urlMaxQueryParams*queryParamHeapBytes)
	base := termHeapCase{form: worstPendingForm("base", nil, ""), query: shortIntakeQuery}
	forms := []termHeapCase{
		{form: worstPendingForm("%41 query", nil, ""), query: percentIntakeQuery()},
		{form: worstPendingForm("plus query", nil, ""), query: plusIntakeQuery()},
		{form: worstPendingForm("distinct params", nil, ""), query: distinctParamsQuery("v")},
		{form: worstPendingForm("distinct escaped params", nil, ""), query: distinctParamsQuery("+")},
		{form: worstPendingForm("header pad", nil, ""), query: shortIntakeQuery, header: http.Header{"X-Pad": {strings.Repeat("p", http.DefaultMaxHeaderBytes-8<<10)}}},
	}
	for _, path := range []string{"/v1/jobs", "/v1/inspect"} {
		for _, hc := range forms {
			t.Run(path+" "+hc.form.name, func(t *testing.T) {
				size := len(path) + 1 + len(hc.query)
				for k, vs := range hc.header {
					for _, v := range vs {
						size += len(k) + len(v)
					}
				}
				b, r := termIncrement(t, path, base, hc)
				inc := r.max - b.min
				t.Logf("%s %s: request line and header %d bytes (http.DefaultMaxHeaderBytes %d); settled live heap base min %d max %d, form min %d (%.3f MiB) max %d; increment max-min %d (%.3f MiB); requestLineCopies × http.DefaultMaxHeaderBytes + urlMaxQueryParams × queryParamHeapBytes = %d (%.3f MiB); status %d %v",
					path, hc.form.name, size, http.DefaultMaxHeaderBytes, b.min, b.max, r.min, mib(r.min), r.max, inc, mib(inc), bound, mib(bound), r.last.status, r.last.payload)
				if size > http.DefaultMaxHeaderBytes {
					t.Errorf("%s %s: request line and header take %d bytes, above http.DefaultMaxHeaderBytes %d", path, hc.form.name, size, http.DefaultMaxHeaderBytes)
				}
				if inc > bound {
					t.Errorf("%s %s: the request line adds %d bytes of live heap, more than requestLineCopies × http.DefaultMaxHeaderBytes + urlMaxQueryParams × queryParamHeapBytes = %d", path, hc.form.name, inc, bound)
				}
			})
		}
	}
}
