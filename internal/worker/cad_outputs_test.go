package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/measurements"
	"github.com/apaderin/octoconv/internal/storage"
)

type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    []string
	failPut func(key string) bool
}

func newFakeS3(t *testing.T, seed map[string][]byte, failPut func(key string) bool) (*storage.Client, *fakeS3) {
	t.Helper()
	f := &fakeS3{objects: seed, failPut: failPut}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	t.Setenv("S3_ENDPOINT", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("S3_ACCESS_KEY", "ak")
	t.Setenv("S3_SECRET_KEY", "sk")
	t.Setenv("S3_BUCKET", "bkt")
	t.Setenv("S3_USE_SSL", "false")
	store, err := storage.New(context.Background())
	if err != nil {
		t.Fatalf("storage.New against the fake S3: %v", err)
	}
	return store, f
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/bkt")
	key := strings.TrimPrefix(path, "/")
	if r.URL.Query().Has("location") {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
		return
	}
	if key == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		_, _ = bytes.NewBuffer(nil).ReadFrom(r.Body)
		if f.failPut != nil && f.failPut(key) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)
			return
		}
		f.puts = append(f.puts, key)
		w.Header().Set("ETag", `"fake"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet, http.MethodHead:
		body, ok := f.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			return
		}
		w.Header().Set("ETag", `"fake"`)
		http.ServeContent(w, r, "", time.Unix(1700000000, 0), bytes.NewReader(body))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeS3) putKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.puts...)
}

type cadOutRepo struct {
	inputs        []jobs.Input
	log           []string
	rows          []jobs.Output
	addOutputsErr error
}

func (r *cadOutRepo) Get(_ context.Context, _ uuid.UUID) (*jobs.Job, error) { return nil, nil }
func (r *cadOutRepo) MarkActive(_ context.Context, _ uuid.UUID) error       { return nil }
func (r *cadOutRepo) MarkFailed(_ context.Context, _ uuid.UUID, _, _ string, _ map[string]any, _ time.Duration) error {
	return nil
}
func (r *cadOutRepo) MarkDone(_ context.Context, _ uuid.UUID, _ time.Duration) error {
	r.log = append(r.log, "MarkDone")
	return nil
}
func (r *cadOutRepo) Inputs(_ context.Context, _ uuid.UUID) ([]jobs.Input, error) {
	return r.inputs, nil
}
func (r *cadOutRepo) AddOutput(_ context.Context, _ uuid.UUID, _ jobs.Output) error {
	r.log = append(r.log, "AddOutput")
	return nil
}
func (r *cadOutRepo) AddOutputWithRole(_ context.Context, _ uuid.UUID, _ jobs.Output) error {
	r.log = append(r.log, "AddOutputWithRole")
	return nil
}
func (r *cadOutRepo) AddOutputs(_ context.Context, _ uuid.UUID, outs []jobs.Output) error {
	r.log = append(r.log, "AddOutputs")
	if r.addOutputsErr != nil {
		return r.addOutputsErr
	}
	r.rows = append(r.rows, outs...)
	return nil
}
func (r *cadOutRepo) Outputs(_ context.Context, _ uuid.UUID) ([]jobs.Output, error) {
	return r.rows, nil
}
func (r *cadOutRepo) UpdateProgress(_ context.Context, _ uuid.UUID, _ int, _ *int) error {
	return nil
}
func (r *cadOutRepo) SetSourceDuration(_ context.Context, _ uuid.UUID, _ float64) error {
	return nil
}

func (r *cadOutRepo) count(name string) int {
	n := 0
	for _, l := range r.log {
		if l == name {
			n++
		}
	}
	return n
}

type sidecarConverter struct {
	op      string
	from    string
	to      string
	sidecar []byte
	sideOf  func(string) string
}

func (c sidecarConverter) Pairs() []convert.Pair { return []convert.Pair{{From: c.from, To: c.to}} }
func (c sidecarConverter) Engine() string        { return convert.EngineCAD }
func (c sidecarConverter) Operation() string     { return c.op }
func (c sidecarConverter) Convert(_ context.Context, _, outPath string, _ map[string]any) error {
	if err := os.WriteFile(outPath, []byte("primary-bytes"), 0o600); err != nil {
		return err
	}
	if c.sidecar != nil {
		return os.WriteFile(c.sideOf(outPath), c.sidecar, 0o600)
	}
	return nil
}

func validSTLMeasurements(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"schema": measurements.Schema, "format": measurements.FormatSTL,
		"unit": measurements.UnitUnspecified, "unit_source": measurements.SourceNone,
		"bbox":   map[string]any{"min": []float64{0, 0, 0}, "max": []float64{1, 2, 3}},
		"volume": nil, "volume_null_reason": measurements.ReasonSTLNotWatertight,
		"area": nil, "area_null_reason": measurements.ReasonSTLNotWatertight,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func zeroVolumeSTLMeasurements(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"schema": measurements.Schema, "format": measurements.FormatSTEP,
		"unit": measurements.UnitMM, "unit_source": measurements.SourceDeclared,
		"bbox":   map[string]any{"min": []float64{0, 0, 0}, "max": []float64{1, 2, 3}},
		"solids": 1, "closed_solids": 1, "volume": 0.0, "area": 22.0,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type cadHarness struct {
	h    *Handler
	repo *cadOutRepo
	s3   *fakeS3
	job  *jobs.Job
}

func newCADHarness(t *testing.T, conv sidecarConverter, hook func(context.Context, *jobs.Job, string) ([]SideOutput, error), failPut func(string) bool) *cadHarness {
	t.Helper()
	inKey := "uploads/74-05/0-in." + conv.from
	store, s3 := newFakeS3(t, map[string][]byte{inKey: []byte("model-bytes")}, failPut)
	registry := convert.NewRegistry()
	registry.Register(conv)
	repo := &cadOutRepo{inputs: []jobs.Input{{Ordinal: 0, ObjectKey: inKey, Filename: "in." + conv.from, Format: conv.from}}}
	h := &Handler{repo: repo, store: store, registry: registry, engineTimout: 30 * time.Second, sideOutputs: hook}
	return &cadHarness{h: h, repo: repo, s3: s3, job: &jobs.Job{
		ID: uuid.New(), Operation: conv.op, SourceFormat: conv.from, TargetFormat: conv.to,
	}}
}

func renderConv(sidecar []byte) sidecarConverter {
	return sidecarConverter{op: convert.OperationRender, from: "stl", to: "png", sidecar: sidecar, sideOf: convert.CADMeasurementsPath}
}

func TestCADOutputsRenderRecordsBothRowsInOneCallThenMarksDone(t *testing.T) {
	hs := newCADHarness(t, renderConv(validSTLMeasurements(t)), cadSideOutputs, nil)
	if err := hs.h.process(context.Background(), hs.job); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := strings.Join(hs.repo.log, ","); got != "AddOutputs,MarkDone" {
		t.Fatalf("repo calls = %s, want exactly AddOutputs then MarkDone (no AddOutput, no AddOutputWithRole)", got)
	}
	if len(hs.repo.rows) != 2 {
		t.Fatalf("rows = %+v, want two", hs.repo.rows)
	}
	p, m := hs.repo.rows[0], hs.repo.rows[1]
	if p.Ordinal != 0 || p.Role != jobs.OutputRolePrimary || p.Format != "png" || p.Filename != "out.png" {
		t.Errorf("primary row = %+v", p)
	}
	if m.Ordinal != 1 || m.Role != jobs.OutputRoleMeasurements || m.Format != "json" || m.Filename != "measurements.json" || m.ContentType != "application/json" {
		t.Errorf("measurements row = %+v", m)
	}
	if puts := hs.s3.putKeys(); len(puts) != 2 {
		t.Errorf("uploads = %v, want the png and the json", puts)
	}
}

func TestCADOutputsSecondUploadFailureLeavesNoRowAndNoMarkDone(t *testing.T) {
	failJSON := func(key string) bool { return strings.HasSuffix(key, "measurements.json") }
	hs := newCADHarness(t, renderConv(validSTLMeasurements(t)), cadSideOutputs, failJSON)
	err := hs.h.process(context.Background(), hs.job)
	if err == nil || !strings.Contains(err.Error(), "measurements.json") {
		t.Fatalf("err = %v, want the failed measurements upload", err)
	}
	if puts := hs.s3.putKeys(); len(puts) != 1 || !strings.HasSuffix(puts[0], "out.png") {
		t.Fatalf("uploads = %v, want only the png before the failure", puts)
	}
	if n := hs.repo.count("AddOutputs") + hs.repo.count("AddOutput") + hs.repo.count("AddOutputWithRole"); n != 0 {
		t.Errorf("a row write happened after a failed upload: %v", hs.repo.log)
	}
	if hs.repo.count("MarkDone") != 0 {
		t.Errorf("MarkDone called after a failed upload: %v", hs.repo.log)
	}
	if outs, _ := hs.repo.Outputs(context.Background(), hs.job.ID); len(outs) != 0 {
		t.Errorf("visible outputs = %+v, want none", outs)
	}
}

func TestCADOutputsAddOutputsFailureNeverMarksDone(t *testing.T) {
	hs := newCADHarness(t, renderConv(validSTLMeasurements(t)), cadSideOutputs, nil)
	hs.repo.addOutputsErr = errors.New("db down")
	if err := hs.h.process(context.Background(), hs.job); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v, want the AddOutputs failure", err)
	}
	if hs.repo.count("MarkDone") != 0 {
		t.Errorf("MarkDone called after a failed AddOutputs: %v", hs.repo.log)
	}
}

func TestCADOutputsRejectedMeasurementsUploadNothing(t *testing.T) {
	cases := []struct {
		name    string
		sidecar []byte
	}{
		{"missing", nil},
		{"volume_zero", zeroVolumeSTLMeasurements(t)},
		{"not_json", []byte("nope")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := newCADHarness(t, renderConv(tc.sidecar), cadSideOutputs, nil)
			err := hs.h.process(context.Background(), hs.job)
			if err == nil {
				t.Fatal("process succeeded with unusable measurements")
			}
			if !errors.Is(err, convert.ErrCADMeasurementsInvalid) || !isCADTerminal(err) {
				t.Errorf("err = %v, want a terminal ErrCADMeasurementsInvalid", err)
			}
			code, msg := cadFailureCode(err)
			if code != CodeEngineError || msg != "the measurements of this model could not be produced" {
				t.Errorf("failure = %s %q", code, msg)
			}
			if puts := hs.s3.putKeys(); len(puts) != 0 {
				t.Errorf("uploads happened before the measurements were validated: %v", puts)
			}
			if len(hs.repo.log) != 0 {
				t.Errorf("repo written: %v", hs.repo.log)
			}
		})
	}
}

func TestCADOutputsRejectedPNGUploadsNothing(t *testing.T) {
	conv := renderConv(validSTLMeasurements(t))
	hs := newCADHarness(t, conv, cadSideOutputs, nil)
	hs.h.registry = convert.NewRegistry()
	hs.h.registry.Register(rejectingRenderConverter{})
	err := hs.h.process(context.Background(), hs.job)
	if !errors.Is(err, convert.ErrCADRenderRejected) {
		t.Fatalf("err = %v, want ErrCADRenderRejected", err)
	}
	if puts := hs.s3.putKeys(); len(puts) != 0 {
		t.Errorf("uploads after a rejected PNG: %v", puts)
	}
	if len(hs.repo.log) != 0 {
		t.Errorf("repo written after a rejected PNG: %v", hs.repo.log)
	}
}

type rejectingRenderConverter struct{}

func (rejectingRenderConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "stl", To: "png"}}
}
func (rejectingRenderConverter) Engine() string    { return convert.EngineCAD }
func (rejectingRenderConverter) Operation() string { return convert.OperationRender }
func (rejectingRenderConverter) Convert(context.Context, string, string, map[string]any) error {
	return fmt.Errorf("%w: blank frame", convert.ErrCADRenderRejected)
}

func TestCADOutputsConvertJobGetsNoExtrasButOneRowThroughAddOutputs(t *testing.T) {
	conv := sidecarConverter{op: convert.OperationConvert, from: "step", to: "stl", sideOf: convert.CADMeasurementsPath}
	hs := newCADHarness(t, conv, cadSideOutputs, nil)
	if err := hs.h.process(context.Background(), hs.job); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := strings.Join(hs.repo.log, ","); got != "AddOutputs,MarkDone" {
		t.Fatalf("repo calls = %s", got)
	}
	if len(hs.repo.rows) != 1 || hs.repo.rows[0].Role != jobs.OutputRolePrimary || hs.repo.rows[0].Ordinal != 0 {
		t.Fatalf("rows = %+v, want one primary at ordinal 0", hs.repo.rows)
	}
}

func TestCADOutputsEnginesWithoutAHookKeepTheSingleOutputPath(t *testing.T) {
	conv := sidecarConverter{op: convert.OperationConvert, from: "step", to: "stl", sideOf: convert.CADMeasurementsPath}
	hs := newCADHarness(t, conv, nil, nil)
	if err := hs.h.process(context.Background(), hs.job); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := strings.Join(hs.repo.log, ","); got != "AddOutput,MarkDone" {
		t.Fatalf("repo calls = %s, want AddOutput then MarkDone and no AddOutputs", got)
	}
}

func TestCADOutputsProbeReportGoesThroughAddOutputsAtOrdinalOne(t *testing.T) {
	conv := sidecarConverter{op: convert.OperationSanitize, from: "pdf", to: "pdf", sidecar: []byte(`{"removed":[]}`), sideOf: convert.SanitizeReportPath}
	hs := newCADHarness(t, conv, probeSideOutputs, nil)
	if err := hs.h.process(context.Background(), hs.job); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := strings.Join(hs.repo.log, ","); got != "AddOutputs,MarkDone" {
		t.Fatalf("repo calls = %s", got)
	}
	if len(hs.repo.rows) != 2 || hs.repo.rows[0].Role != jobs.OutputRolePrimary ||
		hs.repo.rows[1].Role != jobs.OutputRoleReport || hs.repo.rows[1].Ordinal != 1 {
		t.Fatalf("rows = %+v, want primary at 0 and report at 1", hs.repo.rows)
	}
}

func TestCADOutputsDescriptorDeclaresTheHook(t *testing.T) {
	if CADDescriptor.SideOutputs == nil {
		t.Fatal("CADDescriptor declares no SideOutputs: a render job would finish with no measurements")
	}
	h := &Handler{}
	NewRunner(h, CADDescriptor)
	if h.sideOutputs == nil {
		t.Fatal("NewRunner did not install the CAD hook")
	}
}

func validDXFMeasurements(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"schema": measurements.Schema, "format": measurements.FormatDXF,
		"unit": measurements.UnitMM, "unit_source": measurements.SourceDeclared,
		"bbox":   map[string]any{"min": []float64{0, 0}, "max": []float64{120, 40}},
		"volume": nil, "volume_null_reason": measurements.ReasonNotApplicable2D,
		"area": nil, "area_null_reason": measurements.ReasonNotApplicable2D,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := measurements.Validate(b); err != nil {
		t.Fatalf("the 2D fixture document is not valid: %v", err)
	}
	return b
}

func TestCADOutputsDXFRenderRecordsPNGAndMeasurementsInOneCallThenMarksDone(t *testing.T) {
	sidecar := validDXFMeasurements(t)
	conv := sidecarConverter{op: convert.OperationRender, from: "dxf", to: "png", sidecar: sidecar, sideOf: convert.CADMeasurementsPath}
	hs := newCADHarness(t, conv, cadSideOutputs, nil)
	if err := hs.h.process(context.Background(), hs.job); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := strings.Join(hs.repo.log, ","); got != "AddOutputs,MarkDone" {
		t.Fatalf("repo calls = %s, want exactly AddOutputs then MarkDone", got)
	}
	if len(hs.repo.rows) != 2 {
		t.Fatalf("rows = %+v, want two", hs.repo.rows)
	}
	p, m := hs.repo.rows[0], hs.repo.rows[1]
	if p.Ordinal != 0 || p.Role != jobs.OutputRolePrimary || p.Format != "png" || p.Filename != "out.png" || p.ContentType != "image/png" {
		t.Errorf("primary row = %+v", p)
	}
	if m.Ordinal != 1 || m.Role != jobs.OutputRoleMeasurements || m.Format != "json" || m.Filename != "measurements.json" || m.ContentType != "application/json" {
		t.Errorf("measurements row = %+v", m)
	}
	puts := hs.s3.putKeys()
	if len(puts) != 2 || !strings.HasSuffix(puts[0], "out.png") || !strings.HasSuffix(puts[1], "measurements.json") {
		t.Errorf("uploads = %v, want the png then the json", puts)
	}
}

func TestCADOutputsDXFRenderWithA3DBBoxUploadsNothing(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(validDXFMeasurements(t), &doc); err != nil {
		t.Fatal(err)
	}
	doc["bbox"] = map[string]any{"min": []float64{0, 0, 0}, "max": []float64{120, 40, 1}}
	sidecar, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	conv := sidecarConverter{op: convert.OperationRender, from: "dxf", to: "png", sidecar: sidecar, sideOf: convert.CADMeasurementsPath}
	hs := newCADHarness(t, conv, cadSideOutputs, nil)
	err = hs.h.process(context.Background(), hs.job)
	if !errors.Is(err, convert.ErrCADMeasurementsInvalid) || !isCADTerminal(err) {
		t.Fatalf("err = %v, want a terminal ErrCADMeasurementsInvalid", err)
	}
	if puts := hs.s3.putKeys(); len(puts) != 0 {
		t.Errorf("uploads = %v, want none", puts)
	}
	if len(hs.repo.log) != 0 {
		t.Errorf("repo written: %v", hs.repo.log)
	}
}

func TestCADOutputsDXFConvertRecordsOnePrimaryAndNoExtras(t *testing.T) {
	for _, tc := range []struct {
		to          string
		contentType string
	}{
		{"svg", "image/svg+xml"},
		{"pdf", "application/pdf"},
	} {
		t.Run(tc.to, func(t *testing.T) {
			conv := sidecarConverter{op: convert.OperationConvert, from: "dxf", to: tc.to, sidecar: validDXFMeasurements(t), sideOf: convert.CADMeasurementsPath}
			hs := newCADHarness(t, conv, cadSideOutputs, nil)
			if err := hs.h.process(context.Background(), hs.job); err != nil {
				t.Fatalf("process: %v", err)
			}
			if got := strings.Join(hs.repo.log, ","); got != "AddOutputs,MarkDone" {
				t.Fatalf("repo calls = %s, want exactly AddOutputs then MarkDone", got)
			}
			if len(hs.repo.rows) != 1 {
				t.Fatalf("rows = %+v, want one primary row", hs.repo.rows)
			}
			p := hs.repo.rows[0]
			if p.Ordinal != 0 || p.Role != jobs.OutputRolePrimary || p.Format != tc.to || p.Filename != "out."+tc.to || p.ContentType != tc.contentType {
				t.Errorf("primary row = %+v, want ordinal 0, primary, %s, out.%s, %s", p, tc.to, tc.to, tc.contentType)
			}
			if puts := hs.s3.putKeys(); len(puts) != 1 || !strings.HasSuffix(puts[0], "out."+tc.to) {
				t.Errorf("uploads = %v, want only out.%s", puts, tc.to)
			}
		})
	}
}
