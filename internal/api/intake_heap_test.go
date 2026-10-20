package api

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"go.yaml.in/yaml/v3"
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
