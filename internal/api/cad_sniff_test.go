package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/inspect"
)

func newCADTestServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        8 << 20,
		InspectMaxUploadBytes: 8 << 20,
		InspectMaxWorkBytes:   8 << 20,
	})
	return srv
}

func readCADFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "convert", "testdata", "cad", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if len(data) == 0 {
		t.Fatalf("%s is empty", rel)
	}
	return data
}

func paddedSTEP(t *testing.T) []byte {
	t.Helper()
	src := readCADFixture(t, "public/nist_stc_06_asme1_ap242-e3.stp")
	marker := []byte("DATA;")
	i := bytes.Index(src, marker)
	if i < 0 {
		t.Fatal("no DATA; line in nist_stc_06_asme1_ap242-e3.stp")
	}
	i += len(marker)
	for i < len(src) && (src[i] == '\r' || src[i] == '\n') {
		i++
		if src[i-1] == '\n' {
			break
		}
	}
	pad := bytes.Repeat([]byte{'\n'}, 1100*1024)
	out := make([]byte, 0, len(src)+len(pad))
	out = append(out, src[:i]...)
	out = append(out, pad...)
	out = append(out, src[i:]...)
	if int64(len(out)) <= convert.SniffWindowBytes {
		t.Fatalf("padded STEP is %d bytes, want more than %d", len(out), convert.SniffWindowBytes)
	}
	return out
}

func postCADCreate(t *testing.T, srv *Server, filename string, data []byte) (int, map[string]string) {
	t.Helper()
	body, ct := multipartBody(t, filename, "png", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create body: %v; body=%s", err, rec.Body.String())
	}
	return rec.Code, resp
}

func postCADInspect(t *testing.T, srv *Server, filename string, data []byte) inspectResponse {
	t.Helper()
	status, body := doInspect(t, srv, filename, data)
	if status != http.StatusOK {
		t.Fatalf("inspect status = %d, want 200; body=%s", status, body)
	}
	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode inspect body: %v; body=%s", err, body)
	}
	return resp
}

func TestCADTracerSTEPBothEndpoints(t *testing.T) {
	srv := newCADTestServer(t)
	padded := paddedSTEP(t)

	accepted := []struct {
		name     string
		filename string
		data     []byte
	}{
		{"LM8UU", "LM8UU.step", readCADFixture(t, "public/LM8UU.step")},
		{"nist_ctc_01_stp", "nist_ctc_01_asme1_ap242-e1.stp", readCADFixture(t, "public/nist_ctc_01_asme1_ap242-e1.stp")},
		{"legacy_cp1251", "legacy_cp1251.step", readCADFixture(t, "synthetic/legacy_cp1251.step")},
		{"legacy_latin1", "legacy_latin1.step", readCADFixture(t, "synthetic/legacy_latin1.step")},
		{"padded_over_sniff_window", "padded.step", padded},
	}
	for _, tc := range accepted {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			status, resp := postCADCreate(t, srv, tc.filename, tc.data)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("create status = %d, want 422; resp=%v", status, resp)
			}
			if resp["code"] != errcode.UnsupportedConversion {
				t.Errorf("create code = %q, want %q; resp=%v", resp["code"], errcode.UnsupportedConversion, resp)
			}
			if resp["error"] != "unsupported conversion: step -> png" {
				t.Errorf("create error = %q, want %q", resp["error"], "unsupported conversion: step -> png")
			}

			rep := postCADInspect(t, srv, tc.filename, tc.data)
			if rep.Format != "step" {
				t.Errorf("inspect format = %q, want step", rep.Format)
			}
			if rep.DeclaredFormat != "step" {
				t.Errorf("inspect declared_format = %q, want step", rep.DeclaredFormat)
			}
			found := false
			for _, c := range rep.Checks {
				if c.Code == inspect.CodeExtensionMismatch {
					found = true
					if c.Status != inspect.StatusClean {
						t.Errorf("extension_mismatch status = %q, want clean", c.Status)
					}
				}
			}
			if !found {
				t.Errorf("no extension_mismatch check in %+v", rep.Checks)
			}
		})
	}

	lm := readCADFixture(t, "public/LM8UU.step")
	rejected := []struct {
		name     string
		filename string
		data     []byte
	}{
		{"half_of_LM8UU", "half.step", lm[:len(lm)/2]},
		{"padded_tail_cut", "cut.step", padded[:len(padded)-64]},
	}
	for _, tc := range rejected {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			status, resp := postCADCreate(t, srv, tc.filename, tc.data)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("create status = %d, want 422; resp=%v", status, resp)
			}
			if resp["code"] == errcode.UnsupportedConversion {
				t.Errorf("create code = %q, a truncated STEP must not reach the pair check", resp["code"])
			}
			rep := postCADInspect(t, srv, tc.filename, tc.data)
			if rep.Format == "step" {
				t.Errorf("inspect format = step for a truncated STEP")
			}
		})
	}
}
