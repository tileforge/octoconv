package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
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

func writeTransportCapRequest(mw *multipart.Writer, engine, operation, target, fileName string, file io.Reader) error {
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
	for _, f := range [][2]string{{formFieldTarget, target}, {formFieldOperation, operation}, {formFieldOpts, opts}} {
		if err := mw.WriteField(f[0], f[1]); err != nil {
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
		pw.CloseWithError(writeTransportCapRequest(mw, convert.EngineCAD, convert.OperationRender, "png", fileName, file))
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

	rec := postLargestSTL(t, srv, "/v1/jobs", fileName)
	if rec.Code != http.StatusAccepted {
		t.Errorf("create at the chart's caps: status = %d, want 202; body=%.300s", rec.Code, rec.Body.String())
	} else {
		if repo.createdID == uuid.Nil || q.enqueuedCAD != repo.createdID {
			t.Errorf("enqueuedCAD = %s, created = %s, want the created job on the cad queue", q.enqueuedCAD, repo.createdID)
		}
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
