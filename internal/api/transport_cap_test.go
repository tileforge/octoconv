package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

const transportCapChart = "../../deploy/chart/octoconv/templates/configmap.yaml"

func chartLiteral(t *testing.T, key string) int64 {
	t.Helper()
	raw, err := os.ReadFile(transportCapChart)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(key) + `: "(\d+)"$`)
	ms := re.FindAllSubmatch(raw, -1)
	if len(ms) != 1 {
		t.Fatalf("%s: %s is not a literal this test can read (%d lines match %s)", transportCapChart, key, len(ms), re)
	}
	n, err := strconv.ParseInt(string(ms[0][1]), 10, 64)
	if err != nil || n <= 0 {
		t.Fatalf("%s: %s = %q is not a positive int64", transportCapChart, key, ms[0][1])
	}
	return n
}

func maxInputFilenameLength(t *testing.T) int {
	t.Helper()
	best, refused := 0, false
	for n := 5; n <= 4096; n++ {
		if _, err := jobs.SafeInputFilename(strings.Repeat("a", n-4) + ".stl"); err != nil {
			refused = true
		} else {
			best = n
		}
	}
	if !refused {
		t.Fatal("jobs.SafeInputFilename accepted every name up to 4096 bytes: the filename bound the request envelope relies on is gone")
	}
	if best < 5 {
		t.Fatalf("jobs.SafeInputFilename accepted no name of 5 to 4096 bytes (largest accepted %d)", best)
	}
	return best
}

func transportCapFileName(length int, ext string) string {
	return strings.Repeat("a", length-len(ext)-1) + "." + ext
}

func transportCapCallbackURL() string {
	prefix := "https://8.8.8.8/"
	return prefix + strings.Repeat("a", maxCallbackURLBytes-len(prefix))
}

func writeTransportCapRequest(mw *multipart.Writer, engine, operation, target, fileName string, file io.Reader, padTo int64) error {
	padLen := int64(-1)
	if padTo > 0 {
		var c transportCapCounter
		probe := multipart.NewWriter(&c)
		if err := probe.SetBoundary(mw.Boundary()); err != nil {
			return err
		}
		if err := writeTransportCapForm(probe, engine, operation, target, fileName, bytes.NewReader(nil), 0); err != nil {
			return err
		}
		padLen = padTo - c.n
		if padLen < 0 {
			return fmt.Errorf("the known fields carry %d bytes of non-file data, above the pad target %d", c.n, padTo)
		}
	}
	return writeTransportCapForm(mw, engine, operation, target, fileName, file, padLen)
}

func writeTransportCapForm(mw *multipart.Writer, engine, operation, target, fileName string, file io.Reader, padLen int64) error {
	fw, err := mw.CreateFormFile(formFieldFile, fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return err
	}
	if convert.EngineTakesMultipleInputs(engine) {
		for i := 1; i < jobs.MaxInputParts; i++ {
			prefix := strconv.Itoa(i) + "-"
			if _, err := mw.CreateFormFile(formFieldFile, prefix+fileName[len(prefix):]); err != nil {
				return err
			}
		}
	}
	opts := "{" + strings.Repeat(" ", maxOptsBytes-2) + "}"
	for _, f := range [][2]string{{formFieldTarget, target}, {formFieldOperation, operation}, {formFieldOpts, opts}, {formFieldCallbackURL, transportCapCallbackURL()}} {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	if padLen >= 0 {
		if err := mw.WriteField("x_pad", strings.Repeat("x", int(padLen))); err != nil {
			return err
		}
	}
	return mw.Close()
}

type transportCapZeros struct{}

func (transportCapZeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func largestAdmissibleSTL() (io.Reader, int64) {
	triangles := (int64(convert.CADMaxSTLBytes) - 84) / 50
	header := make([]byte, 84)
	binary.LittleEndian.PutUint32(header[80:], uint32(triangles))
	return io.MultiReader(bytes.NewReader(header), io.LimitReader(transportCapZeros{}, 50*triangles)), 84 + 50*triangles
}

type transportCapStorage struct {
	*fakeStorage
	bytes int64
}

func (s *transportCapStorage) Upload(_ context.Context, _ string, r io.Reader, _ int64, contentType string) error {
	n, err := io.Copy(io.Discard, r)
	s.bytes += n
	s.uploaded = true
	s.uploadCalls++
	s.contentType = contentType
	return err
}

func postLargestSTL(t *testing.T, srv *Server, path, fileName string) *httptest.ResponseRecorder {
	t.Helper()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	file, _ := largestAdmissibleSTL()
	go func() {
		pw.CloseWithError(writeTransportCapRequest(mw, convert.EngineCAD, convert.OperationRender, "png", fileName, file, maxFormNonFileBytes-multipartPeekBufferBytes))
	}()
	req := authed(httptest.NewRequest(http.MethodPost, path, pr))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	_ = pr.Close()
	return rec
}

func TestChartTransportCapsAdmitTheLargestCeilingFile(t *testing.T) {
	if testing.Short() {
		t.Skip("streams a request of the largest admissible STL")
	}
	maxUpload := chartLiteral(t, "MAX_UPLOAD_BYTES")
	inspectUpload := chartLiteral(t, "INSPECT_MAX_UPLOAD_BYTES")
	fileName := transportCapFileName(maxInputFilenameLength(t), "stl")
	_, size := largestAdmissibleSTL()
	if size > int64(convert.CADMaxSTLBytes) || size+50 <= int64(convert.CADMaxSTLBytes) {
		t.Fatalf("the STL is %d bytes, want the largest binary STL at or below %d", size, int64(convert.CADMaxSTLBytes))
	}
	t.Logf("chart MAX_UPLOAD_BYTES=%d INSPECT_MAX_UPLOAD_BYTES=%d, file %q (%d bytes) of %d bytes", maxUpload, inspectUpload, fileName, len(fileName), size)

	repo, q := &fakeRepo{}, &fakeQueue{}
	store := &transportCapStorage{fakeStorage: &fakeStorage{}}
	srv := NewServer(repo, store, q, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        maxUpload,
		InspectMaxUploadBytes: inspectUpload,
	})
	admitCADCatalog(srv)

	rec := postLargestSTL(t, srv, "/v1/jobs", fileName)
	if rec.Code != http.StatusAccepted {
		t.Errorf("create at the chart's caps: status = %d, want 202; body=%.300s", rec.Code, rec.Body.String())
	} else {
		assertAdmittedCAD(t, repo, q, convert.OperationRender, "stl", "png", "octoconv.render.cad")
		if store.uploadCalls != 1 || store.bytes != size {
			t.Errorf("uploadCalls = %d, uploaded %d bytes, want 1 upload of %d bytes", store.uploadCalls, store.bytes, size)
		}
	}

	rec = postLargestSTL(t, srv, "/v1/inspect", fileName)
	if rec.Code != http.StatusOK {
		t.Fatalf("inspect at the chart's caps: status = %d, want 200; body=%.300s", rec.Code, rec.Body.String())
	}
	var rep inspectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode inspect body: %v; body=%.300s", err, rec.Body.String())
	}
	found := false
	for _, o := range rep.Operations {
		if o.Engine != convert.EngineCAD || o.Operation != convert.OperationRender || o.Target != "png" {
			continue
		}
		found = true
		if !o.Available || o.MaxBytes != int64(convert.CADMaxSTLBytes) {
			t.Errorf("offer %+v: want Available true and MaxBytes %d", o, int64(convert.CADMaxSTLBytes))
		}
	}
	if !found {
		t.Errorf("no cad render png offer; operations=%+v", rep.Operations)
	}
}

const (
	transportCapChartDir = "../../deploy/chart/octoconv/"
	transportCapCompose  = "../../docker-compose.yml"
	transportCapEnv      = "../../.env.example"
)

var transportCapKeys = []string{"MAX_UPLOAD_BYTES", "INSPECT_MAX_UPLOAD_BYTES", "PANDOC_MAX_INPUT_BYTES"}

type transportCapConfig struct {
	name   string
	values map[string]int64
}

func transportCapInt(t *testing.T, where, key, raw string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		t.Fatalf("%s: %s = %q is not a positive int64", where, key, raw)
	}
	return n
}

func chartExtraEnv(t *testing.T, file string) map[string]int64 {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		API struct {
			ExtraEnv map[string]any `yaml:"extraEnv"`
		} `yaml:"api"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	out := map[string]int64{}
	for _, key := range transportCapKeys {
		if v, ok := doc.API.ExtraEnv[key]; ok {
			out[key] = transportCapInt(t, file+" api.extraEnv", key, fmt.Sprint(v))
		}
	}
	return out
}

func transportCapMerge(base, over map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

func shippedTransportCapConfigs(t *testing.T) []transportCapConfig {
	t.Helper()
	defaults := map[string]int64{}
	for _, key := range transportCapKeys {
		defaults[key] = chartLiteral(t, key)
	}
	base := transportCapMerge(defaults, chartExtraEnv(t, transportCapChartDir+"values.yaml"))
	configs := []transportCapConfig{{"chart", base}}
	overlays, err := filepath.Glob(transportCapChartDir + "values-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(overlays) == 0 {
		t.Fatalf("no values-*.yaml overlay under %s", transportCapChartDir)
	}
	sort.Strings(overlays)
	for _, f := range overlays {
		configs = append(configs, transportCapConfig{"chart -f " + filepath.Base(f), transportCapMerge(base, chartExtraEnv(t, f))})
	}

	raw, err := os.ReadFile(transportCapCompose)
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
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
	composeValues := map[string]int64{}
	for _, key := range transportCapKeys {
		v, ok := env[key]
		if !ok {
			if key == "PANDOC_MAX_INPUT_BYTES" {
				continue
			}
			t.Fatalf("%s services.api.environment has no %s", transportCapCompose, key)
		}
		composeValues[key] = transportCapInt(t, transportCapCompose+" services.api.environment", key, fmt.Sprint(v))
	}
	configs = append(configs, transportCapConfig{"docker-compose.yml api", composeValues})

	raw, err = os.ReadFile(transportCapEnv)
	if err != nil {
		t.Fatal(err)
	}
	envValues := map[string]int64{}
	for _, key := range transportCapKeys {
		ms := regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(key)+`=(\d+)(?:\s|$)`).FindAllSubmatch(raw, -1)
		if len(ms) == 0 && key == "PANDOC_MAX_INPUT_BYTES" {
			continue
		}
		if len(ms) != 1 {
			t.Fatalf("%s: %d lines set %s to a number, want exactly one", transportCapEnv, len(ms), key)
		}
		envValues[key] = transportCapInt(t, transportCapEnv, key, string(ms[0][1]))
	}
	configs = append(configs, transportCapConfig{".env.example", envValues})

	if len(configs) < 4 {
		t.Fatalf("%d configurations, want at least four", len(configs))
	}
	return configs
}

type transportCapCounter struct{ n int64 }

func (c *transportCapCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

func transportCapEnvelope(t *testing.T, engine, operation string, pair convert.Pair, nameLen int) int64 {
	t.Helper()
	var c transportCapCounter
	if err := writeTransportCapRequest(multipart.NewWriter(&c), engine, operation, pair.To, transportCapFileName(nameLen, pair.From), bytes.NewReader(nil), 0); err != nil {
		t.Fatal(err)
	}
	return c.n
}

type transportCapEntry struct {
	operation, engine string
	pair              convert.Pair
	limit, envelope   int64
}

func (e transportCapEntry) need() int64 { return e.limit + e.envelope }

func transportCapNextMiB(n int64) int64 { return (n + 1<<20 - 1) >> 20 << 20 }

func TestDeployedTransportCapsCoverEveryAdvertisedCeiling(t *testing.T) {
	exempt := map[string]string{
		convert.EngineAV: "av's 2 GiB ceiling is kept out of the API pod's pre-detection spill in the chart and out of POST /v1/inspect everywhere (Phase 58 D-06); decision D1 of quick 261018-bmr",
	}
	nameLen := maxInputFilenameLength(t)
	configs := shippedTransportCapConfigs(t)
	classes := convert.Default.OperationClasses()

	entries := map[string][]transportCapEntry{}
	var largest transportCapEntry
	var largestNeed int64
	for _, cfg := range configs {
		srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
			MaxUploadBytes:        cfg.values["MAX_UPLOAD_BYTES"],
			InspectMaxUploadBytes: cfg.values["INSPECT_MAX_UPLOAD_BYTES"],
			MaxPandocInputBytes:   cfg.values["PANDOC_MAX_INPUT_BYTES"],
		})
		cad := 0
		for operation, engines := range classes {
			for engine, pairs := range engines {
				for _, pair := range pairs {
					limit, _, gated := srv.sizeLimit(engine, pair.From)
					if !gated {
						t.Errorf("%s: %s %s %s->%s is advertised with no ceiling, so the transport cap is its only bound", cfg.name, operation, engine, pair.From, pair.To)
						continue
					}
					if env := transportCapEnvelope(t, engine, operation, pair, nameLen); env > maxFormNonFileBytes-multipartPeekBufferBytes {
						t.Errorf("%s: %s %s %s->%s: the known fields carry %d bytes of non-file data, above the admit line maxFormNonFileBytes - multipartPeekBufferBytes = %d", cfg.name, operation, engine, pair.From, pair.To, env, maxFormNonFileBytes-multipartPeekBufferBytes)
					}
					e := transportCapEntry{operation, engine, pair, limit, maxFormNonFileBytes}
					entries[cfg.name] = append(entries[cfg.name], e)
					if engine == convert.EngineCAD {
						cad++
					}
					if _, ok := exempt[engine]; ok {
						continue
					}
					if e.limit > largest.limit {
						largest = e
					}
					largestNeed = max(largestNeed, e.need())
				}
			}
		}
		if cad < 7 {
			t.Fatalf("%s: %d cad entries, want at least seven", cfg.name, cad)
		}
	}
	nextMiB := transportCapNextMiB(largestNeed)
	t.Logf("largest requirement %d bytes (%s %s %s->%s ceiling %d plus the non-file bound %d); next whole MiB %d (%d MiB)", largestNeed, largest.operation, largest.engine, largest.pair.From, largest.pair.To, largest.limit, largest.envelope, nextMiB, nextMiB>>20)
	t.Logf("D-19: every non-file byte of a create or inspect form is inside maxFormNonFileBytes %d; the largest requirement is a format ceiling plus that bound", maxFormNonFileBytes)

	atLargest := map[string]bool{}
	for _, cfg := range configs {
		for _, e := range entries[cfg.name] {
			if _, ok := exempt[e.engine]; !ok && e.limit == largest.limit {
				atLargest[e.operation+" "+e.engine+" "+e.pair.From+"->"+e.pair.To] = true
			}
		}
	}
	tracer := convert.OperationRender + " " + convert.EngineCAD + " stl->png"
	if largest.limit != int64(convert.CADMaxSTLBytes) || len(atLargest) != 1 || !atLargest[tracer] {
		at := make([]string, 0, len(atLargest))
		for k := range atLargest {
			at = append(at, k)
		}
		sort.Strings(at)
		t.Errorf("the largest non-exempt ceiling is %d on %s, want convert.CADMaxSTLBytes %d on %s alone: TestChartTransportCapsAdmitTheLargestCeilingFile no longer posts the worst case", largest.limit, strings.Join(at, ", "), int64(convert.CADMaxSTLBytes), tracer)
	}

	needed := map[string]map[string]bool{}
	advertised := map[string]bool{}
	for _, cfg := range configs {
		type group struct {
			key, operation, engine string
			limit, envelope        int64
		}
		failures := map[group][]string{}
		var order []group
		for _, e := range entries[cfg.name] {
			for _, key := range []string{"MAX_UPLOAD_BYTES", "INSPECT_MAX_UPLOAD_BYTES"} {
				if cfg.values[key] >= e.need() {
					continue
				}
				if _, ok := exempt[e.engine]; ok {
					if needed[e.engine] == nil {
						needed[e.engine] = map[string]bool{}
					}
					needed[e.engine][cfg.name+" "+key] = true
					continue
				}
				g := group{key, e.operation, e.engine, e.limit, e.envelope}
				if _, ok := failures[g]; !ok {
					order = append(order, g)
				}
				failures[g] = append(failures[g], e.pair.From+"->"+e.pair.To)
			}
			if _, ok := exempt[e.engine]; ok {
				advertised[e.engine] = true
			}
		}
		sort.Slice(order, func(i, j int) bool {
			return fmt.Sprint(order[i]) < fmt.Sprint(order[j])
		})
		for _, g := range order {
			t.Errorf("%s: %s = %d is below %s %s ceiling %d plus the non-file bound %d = %d for %s; set at least %d (next whole MiB above the largest requirement %d)", cfg.name, g.key, cfg.values[g.key], g.operation, g.engine, g.limit, g.envelope, g.limit+g.envelope, strings.Join(failures[g], " "), nextMiB, largestNeed)
		}
		if cfg.values["INSPECT_MAX_UPLOAD_BYTES"] > cfg.values["MAX_UPLOAD_BYTES"] {
			t.Errorf("%s: INSPECT_MAX_UPLOAD_BYTES %d is above MAX_UPLOAD_BYTES %d", cfg.name, cfg.values["INSPECT_MAX_UPLOAD_BYTES"], cfg.values["MAX_UPLOAD_BYTES"])
		}
	}

	var chartInspect, chartMax int64
	for _, cfg := range configs {
		if cfg.name != "chart" && !strings.HasPrefix(cfg.name, "chart -f ") {
			continue
		}
		chartInspect = max(chartInspect, cfg.values["INSPECT_MAX_UPLOAD_BYTES"])
		if chartMax == 0 || cfg.values["MAX_UPLOAD_BYTES"] < chartMax {
			chartMax = cfg.values["MAX_UPLOAD_BYTES"]
		}
	}
	if chartInspect > chartMax {
		t.Errorf("across the chart's values files the largest INSPECT_MAX_UPLOAD_BYTES %d is above the smallest MAX_UPLOAD_BYTES %d", chartInspect, chartMax)
	}

	engines := make([]string, 0, len(exempt))
	for engine := range exempt {
		engines = append(engines, engine)
	}
	sort.Strings(engines)
	for _, engine := range engines {
		if !advertised[engine] {
			t.Errorf("the %s exemption names an engine that advertises no pair", engine)
			continue
		}
		if len(needed[engine]) == 0 {
			t.Errorf("the %s exemption is not needed: every configuration covers every %s ceiling plus the envelope", engine, engine)
			continue
		}
		where := make([]string, 0, len(needed[engine]))
		for w := range needed[engine] {
			where = append(where, w)
		}
		sort.Strings(where)
		t.Logf("exempt %s (%s): not covered by %s", engine, exempt[engine], strings.Join(where, ", "))
	}
}

type atomicCapStorage struct {
	*fakeStorage
	bytes   atomic.Int64
	uploads atomic.Int32
}

func (s *atomicCapStorage) Upload(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	n, err := io.Copy(io.Discard, r)
	s.bytes.Add(n)
	s.uploads.Add(1)
	return err
}

func TestChartTransportCapsAdmitTheLargestCeilingFileOverAPacedConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("streams a request of the largest admissible STL over a paced connection")
	}
	window := intakeReadDeadline / 10
	minRate := intakeMinBytesPerSecond * 10
	cfg := Config{
		MaxUploadBytes:        chartLiteral(t, "MAX_UPLOAD_BYTES"),
		InspectMaxUploadBytes: chartLiteral(t, "INSPECT_MAX_UPLOAD_BYTES"),
	}
	fileName := transportCapFileName(maxInputFilenameLength(t), "stl")
	const rate = 25000000

	paced := func(t *testing.T, ts *httptest.Server, path string) (int, []byte, int64, time.Duration) {
		t.Helper()
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		file, size := largestAdmissibleSTL()
		go func() {
			pw.CloseWithError(writeTransportCapRequest(mw, convert.EngineCAD, convert.OperationRender, "png", fileName, file, maxFormNonFileBytes-multipartPeekBufferBytes))
		}()
		req, err := http.NewRequest(http.MethodPost, ts.URL+path, &pacedReader{r: pr, rate: rate, chunk: 256 << 10})
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		start := time.Now()
		resp, err := ts.Client().Do(authed(req))
		elapsed := time.Since(start)
		_ = pr.Close()
		t.Logf("%s: %d-byte STL at %d B/s (20 Mbit/s with time scaled down by 10) against a %v window and %d B/s: %v", path, size, rate, window, minRate, elapsed)
		if err != nil {
			t.Fatalf("%s over a paced connection failed after %v: %v", path, elapsed, err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if elapsed <= window {
			t.Errorf("the upload took %v, want more than the %v window", elapsed, window)
		}
		return resp.StatusCode, raw, size, elapsed
	}

	t.Run("/v1/jobs", func(t *testing.T) {
		store := &atomicCapStorage{fakeStorage: &fakeStorage{}}
		srv, ts := newIntakeServerWith(t, &fakeRepo{}, store, cfg, window, minRate)
		admitCADCatalog(srv)
		status, raw, size, elapsed := paced(t, ts, "/v1/jobs")
		if status != http.StatusAccepted {
			t.Fatalf("create over a paced connection: status = %d after %v, want 202; body=%.300s", status, elapsed, raw)
		}
		if store.uploads.Load() != 1 || store.bytes.Load() != size {
			t.Errorf("uploads = %d, uploaded %d bytes, want 1 upload of %d bytes", store.uploads.Load(), store.bytes.Load(), size)
		}
	})

	t.Run("/v1/inspect", func(t *testing.T) {
		_, ts := newIntakeServer(t, cfg, window, minRate)
		status, raw, _, elapsed := paced(t, ts, "/v1/inspect")
		if status != http.StatusOK {
			t.Fatalf("inspect over a paced connection: status = %d after %v, want 200; body=%.300s", status, elapsed, raw)
		}
		var rep inspectResponse
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatalf("decode inspect body: %v; body=%.300s", err, raw)
		}
		found := false
		for _, o := range rep.Operations {
			if o.Engine != convert.EngineCAD || o.Operation != convert.OperationRender || o.Target != "png" {
				continue
			}
			found = true
			if !o.Available || o.MaxBytes != int64(convert.CADMaxSTLBytes) {
				t.Errorf("offer %+v: want Available true and MaxBytes %d", o, int64(convert.CADMaxSTLBytes))
			}
		}
		if !found {
			t.Errorf("no cad render png offer; operations=%+v", rep.Operations)
		}
	})
}

func TestTransportCapEnvelopeCarriesEveryBoundedField(t *testing.T) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if err := writeTransportCapRequest(mw, convert.EngineCAD, convert.OperationRender, "png", transportCapFileName(maxInputFilenameBytes, "stl"), strings.NewReader("solid"), 0); err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(&b, mw.Boundary())
	fields := map[string]string{}
	var fileNames []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if p.FormName() == formFieldFile {
			_, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
			if err != nil {
				t.Fatalf("file part Content-Disposition: %v", err)
			}
			fileNames = append(fileNames, params["filename"])
			continue
		}
		v, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		fields[p.FormName()] = string(v)
	}
	if len(fileNames) == 0 || len(fileNames[0]) != maxInputFilenameBytes {
		t.Errorf("file part names %q, want a first part whose raw name is %d bytes", fileNames, maxInputFilenameBytes)
	}
	cb, ok := fields[formFieldCallbackURL]
	if !ok || len(cb) != maxCallbackURLBytes {
		t.Errorf("callback_url present %v, %d bytes, want %d bytes", ok, len(cb), maxCallbackURLBytes)
	} else if err := validateCallbackURL(cb); err != nil {
		t.Errorf("the envelope's callback_url is refused by validateCallbackURL: %v", err)
	}
	if len(fields[formFieldOpts]) != maxOptsBytes {
		t.Errorf("opts is %d bytes, want %d", len(fields[formFieldOpts]), maxOptsBytes)
	}

	type envelopeCase struct {
		operation, engine string
		pair              convert.Pair
	}
	var multi []envelopeCase
	for operation, engines := range convert.Default.OperationClasses() {
		for engine, pairs := range engines {
			if !convert.EngineTakesMultipleInputs(engine) {
				continue
			}
			for _, pair := range pairs {
				multi = append(multi, envelopeCase{operation, engine, pair})
			}
		}
	}
	if len(multi) == 0 {
		t.Fatal("no engine takes multiple inputs, so the envelope with MaxInputParts file parts is not exercised")
	}
	sort.Slice(multi, func(i, j int) bool { return fmt.Sprint(multi[i]) < fmt.Sprint(multi[j]) })
	nameLen := maxInputFilenameLength(t)
	line := int64(maxFormNonFileBytes - multipartPeekBufferBytes)
	for _, c := range []envelopeCase{{convert.OperationRender, convert.EngineCAD, convert.Pair{From: "stl", To: "png"}}, multi[0]} {
		for _, padTo := range []int64{0, line} {
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			if err := writeTransportCapRequest(mw, c.engine, c.operation, c.pair.To, transportCapFileName(nameLen, c.pair.From), bytes.NewReader(nil), padTo); err != nil {
				t.Fatal(err)
			}
			size := int64(body.Len())
			if padTo > 0 && size != padTo {
				t.Errorf("%s %s %s->%s padded to %d: the request carries %d bytes of non-file data", c.operation, c.engine, c.pair.From, c.pair.To, padTo, size)
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/jobs", &body)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			err := parseIntakeForm(req)
			dropMultipartForm(req)
			t.Logf("%s %s %s->%s: %d bytes of non-file data, parseIntakeForm = %v", c.operation, c.engine, c.pair.From, c.pair.To, size, err)
			if err != nil {
				t.Errorf("%s %s %s->%s: the request with %d bytes of non-file data is refused by parseIntakeForm: %v", c.operation, c.engine, c.pair.From, c.pair.To, size, err)
			}
		}
	}
}
