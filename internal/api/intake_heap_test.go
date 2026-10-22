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

type intakeHeapTerms struct {
	content int64
	nonFile int64
	request int64
	fixed   int64
	total   int64
}

func intakeHeapModel(budget int64) intakeHeapTerms {
	m := intakeHeapTerms{
		content: readFormContentBound(budget),
		nonFile: nonFileHeapPerByte * maxFormNonFileBytes,
		request: requestLineCopies * http.DefaultMaxHeaderBytes,
		fixed:   intakeFixedHeapBytes,
	}
	m.total = m.content + m.nonFile + m.request + m.fixed
	return m
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
	var segments []rawSegment
	for i, size := range sizes {
		header := rawFilePartHeader + "\r\n"
		if i == len(sizes)-1 {
			segments = append(segments, beforeLast...)
			header += lastHeader
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
	for _, s := range f.segments {
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
	}
	return nil
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
	x := lastDoubling(b) - (bytes.MinRead - 1)
	content := []heapLimit{{"readFormContentBound + intakeFixedHeapBytes", readFormContentBound(b) + intakeFixedHeapBytes}}
	spill := worstSplit(b, x)
	spill[len(spill)-1]++
	return []rawHeapCase{
		{form: rawFileParts("worst pending", worstSplit(b, x), nil, 0), query: shortIntakeQuery, upper: content},
		{form: rawFileParts("worst spill", spill, nil, 1<<20), query: shortIntakeQuery, upper: content},
		{form: rawFileParts("worst spill settled", spill, nil, 1<<20), query: shortIntakeQuery, settle: 250 * time.Millisecond, upper: content, floor: 2*b - 1<<20},
	}
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
		for _, hc := range budgetHeapCases() {
			t.Run(path+" "+hc.form.name, func(t *testing.T) {
				checkRawHeapCase(t, configs, smallest, path, hc)
			})
		}
	}
}

func checkRawHeapCase(t *testing.T, configs []intakeHeapConfig, smallest intakeHeapConfig, path string, hc rawHeapCase) heapRange {
	t.Helper()
	line := int64(maxFormNonFileBytes - multipartPeekBufferBytes)
	model := intakeHeapModel(multipartInMemoryBudget)
	srv := heapProbeServer()
	r := heapOfThree(func() intakeProbe {
		return liveHeapOfRaw(t, srv, path, hc.query, hc.header, hc.form, hc.settle)
	})
	nonFile := rawFormNonFileBytes(hc.form)
	t.Logf("%s %s: %d parts, %d file bytes, %d non-file bytes (line %d), query %d bytes, settle %v; live heap min %d (%.2f MiB) max %d (%.2f MiB); model total %d (%.2f MiB); smallest share %d (%s); max × %d = %d; floor %d; upper %v; status %d %v; %d bytes sent",
		path, hc.form.name, rawFormParts(hc.form), rawFormFileBytes(hc.form), nonFile, line, len(hc.query), hc.settle, r.min, mib(r.min), r.max, mib(r.max), model.total, mib(model.total), smallest.share, smallest.name, smallest.forms, r.max*smallest.forms, hc.floor, hc.upper, r.last.status, r.last.payload, r.last.sent)
	if !readWhole(r.last) {
		t.Fatalf("%s %s: an admissible form was not read whole (write error %v, status %d %v)", path, hc.form.name, r.last.err, r.last.status, r.last.payload)
	}
	if nonFile > line {
		t.Errorf("%s %s: the form carries %d bytes of non-file data, above the admit line %d", path, hc.form.name, nonFile, line)
	}
	for _, c := range configs {
		if r.max > c.share {
			t.Errorf("%s %s: one form holds up to %d bytes of live heap, more than the share %d of %s (limit %d / 2 / %d live forms)", path, hc.form.name, r.max, c.share, c.name, c.memory, c.forms)
		}
	}
	if r.max > model.total {
		t.Errorf("%s %s: one form holds up to %d bytes of live heap, more than intakeHeapModel(multipartInMemoryBudget).total %d", path, hc.form.name, r.max, model.total)
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

func TestIntakeBudgetIsDerivedFromThePodShare(t *testing.T) {
	configs := intakeHeapConfigs(t)
	smallest := configs[0]
	for _, c := range configs {
		t.Logf("%s: share %d = %d / 2 / (%d + %d + %d)", c.name, c.share, c.memory, createIntakeMaxConcurrent, inspectIntakeMaxConcurrent, c.inspectMaxConcurrent)
		if c.share < smallest.share {
			smallest = c
		}
	}
	var want int64
	for n := int64(1); n <= 64; n++ {
		if intakeHeapModel(n<<20).total <= smallest.share {
			want = n << 20
		}
	}
	if want == 0 {
		t.Fatalf("no whole MiB from 1 to 64 keeps intakeHeapModel within the smallest share %d of %s", smallest.share, smallest.name)
	}
	for _, b := range []int64{want, want + 1<<20} {
		m := intakeHeapModel(b)
		t.Logf("budget %d (%d MiB): lastDoubling %d; content %d = 2 × %d + %d + 2 × %d × (%d + 1); nonFile %d = %d × %d; request %d = %d × %d; fixed %d; total %d (%.2f MiB); smallest share %d (%s); fits %v",
			b, b>>20, lastDoubling(b), m.content, b, lastDoubling(b), bytes.MinRead, readFormMaxParts, m.nonFile, nonFileHeapPerByte, maxFormNonFileBytes, m.request, requestLineCopies, http.DefaultMaxHeaderBytes, m.fixed, m.total, mib(m.total), smallest.share, smallest.name, m.total <= smallest.share)
	}
	if multipartInMemoryBudget != want {
		t.Errorf("multipartInMemoryBudget = %d, want %d: the budget is derived from what ReadForm holds, the largest whole MiB with intakeHeapModel(budget).total within the smallest share %d of %s", multipartInMemoryBudget, want, smallest.share, smallest.name)
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
