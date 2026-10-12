package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/storage"
)

type linkedPrimaryConverter struct {
	outside string
	sidecar []byte
}

func (linkedPrimaryConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "stl", To: "png"}}
}
func (linkedPrimaryConverter) Engine() string    { return convert.EngineCAD }
func (linkedPrimaryConverter) Operation() string { return convert.OperationRender }
func (c linkedPrimaryConverter) Convert(_ context.Context, _, outPath string, _ map[string]any) error {
	if err := os.Symlink(c.outside, outPath); err != nil {
		return err
	}
	return os.WriteFile(convert.CADMeasurementsPath(outPath), c.sidecar, 0o600)
}

type bodyRecorder struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (b *bodyRecorder) holds(want []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, body := range b.bodies {
		if bytes.Contains(body, want) {
			return true
		}
	}
	return false
}

func newRecordingS3(t *testing.T, seed map[string][]byte) (*storage.Client, *bodyRecorder) {
	t.Helper()
	f := &fakeS3{objects: seed}
	rec := &bodyRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			rec.mu.Lock()
			rec.bodies = append(rec.bodies, body)
			rec.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		f.serve(w, r)
	}))
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
	return store, rec
}

func plantedOutside(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkerReadsOfTheJobDirRefuseAPlantedLink(t *testing.T) {
	t.Run("the primary output", func(t *testing.T) {
		secret := []byte("outside-bytes-that-must-never-be-uploaded")
		conv := linkedPrimaryConverter{outside: plantedOutside(t, secret), sidecar: validSTLMeasurements(t)}
		inKey := "uploads/quick/0-in.stl"
		store, rec := newRecordingS3(t, map[string][]byte{inKey: []byte("model-bytes")})
		registry := convert.NewRegistry()
		registry.Register(conv)
		repo := &cadOutRepo{inputs: []jobs.Input{{Ordinal: 0, ObjectKey: inKey, Filename: "in.stl", Format: "stl"}}}
		h := &Handler{repo: repo, store: store, registry: registry, engineTimout: 30 * time.Second, sideOutputs: cadSideOutputs}
		job := &jobs.Job{ID: uuid.New(), Operation: convert.OperationRender, SourceFormat: "stl", TargetFormat: "png"}
		err := h.process(context.Background(), job)
		if err == nil {
			t.Error("process uploaded a primary output that is a planted link")
		}
		if rec.holds(secret) {
			t.Error("the outside bytes reached the object store")
		}
		if repo.count("MarkDone") != 0 {
			t.Errorf("MarkDone after a planted primary output: %v", repo.log)
		}
	})
	t.Run("the sanitize report", func(t *testing.T) {
		rep, err := convert.BuildSanitizeReport("pdf", convert.SanitizeResult{Removed: []convert.ThreatClass{convert.ThreatJavaScript}})
		if err != nil {
			t.Fatal(err)
		}
		blob, err := convert.MarshalSanitizeReport(rep)
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "out.pdf")
		if err := os.Symlink(plantedOutside(t, blob), convert.SanitizeReportPath(out)); err != nil {
			t.Fatal(err)
		}
		extra, err := probeSideOutputs(context.Background(), &jobs.Job{}, out)
		if !errors.Is(err, convert.ErrJobFileIrregular) {
			t.Errorf("probeSideOutputs = %d outputs, %v, want ErrJobFileIrregular", len(extra), err)
		}
	})
	t.Run("the measurements sidecar", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out.png")
		if err := os.Symlink(plantedOutside(t, validSTLMeasurements(t)), convert.CADMeasurementsPath(out)); err != nil {
			t.Fatal(err)
		}
		extra, err := cadSideOutputs(context.Background(), &jobs.Job{Operation: convert.OperationRender}, out)
		if !errors.Is(err, convert.ErrJobFileIrregular) {
			t.Errorf("cadSideOutputs = %d outputs, %v, want ErrJobFileIrregular", len(extra), err)
		}
	})
}
