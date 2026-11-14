package api

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/inspect"
)

func TestSTLInspectReportsMeshFindings(t *testing.T) {
	srv := newCADTestServer(t)
	cases := []struct {
		name string
		tris [][3]int
		want []inspect.Code
	}{
		{"triangle", [][3]int{{0, 1, 2}}, []inspect.Code{inspect.CodeSTLOpen}},
		{"same_wind", [][3]int{{0, 1, 2}, {0, 1, 3}}, []inspect.Code{inspect.CodeSTLOpen, inspect.CodeSTLUnoriented}},
		{"fan", [][3]int{{0, 1, 2}, {0, 1, 3}, {0, 1, 4}}, []inspect.Code{inspect.CodeSTLOpen, inspect.CodeSTLNonManifold}},
		{"tet", [][3]int{{0, 1, 2}, {0, 2, 3}, {0, 3, 1}, {1, 3, 2}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := doInspect(t, srv, "mesh.stl", meshSTL(tc.tris))
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", status, body)
			}
			var resp inspectResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("decode: %v; body=%s", err, body)
			}
			if resp.Format != "stl" {
				t.Fatalf("format = %q, want stl", resp.Format)
			}
			got := map[inspect.Code]inspect.Severity{}
			for _, finding := range resp.Findings {
				switch finding.Code {
				case inspect.CodeSTLOpen, inspect.CodeSTLNonManifold, inspect.CodeSTLUnoriented:
					got[finding.Code] = finding.Severity
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("stl findings = %v, want %v", got, tc.want)
			}
			for _, code := range tc.want {
				if got[code] != inspect.SeverityInfo {
					t.Fatalf("%s severity = %q, want info", code, got[code])
				}
			}
			for _, code := range []inspect.Code{inspect.CodeSTLOpen, inspect.CodeSTLNonManifold, inspect.CodeSTLUnoriented} {
				if _, present := got[code]; present {
					continue
				}
				var found bool
				for _, check := range resp.Checks {
					if check.Code != code {
						continue
					}
					found = true
					if check.Status != inspect.StatusClean {
						t.Fatalf("%s status = %s, want clean", code, check.Status)
					}
				}
				if !found {
					t.Fatalf("%s missing from checks", code)
				}
			}
		})
	}
}

func TestSTLInspectSTEPIsNotApplicable(t *testing.T) {
	srv := newCADTestServer(t)
	resp := postCADInspect(t, srv, "LM8UU.step", readCADFixture(t, "public/LM8UU.step"))
	for _, code := range []inspect.Code{inspect.CodeSTLOpen, inspect.CodeSTLNonManifold, inspect.CodeSTLUnoriented} {
		var found bool
		for _, check := range resp.Checks {
			if check.Code != code {
				continue
			}
			found = true
			if check.Status != inspect.StatusNotApplicable || check.Reason != inspect.ReasonFormatNotApplicable {
				t.Fatalf("%s = %+v, want not_applicable", code, check)
			}
		}
		if !found {
			t.Fatalf("%s missing from checks", code)
		}
		for _, finding := range resp.Findings {
			if finding.Code == code {
				t.Fatalf("STEP report carries %s", code)
			}
		}
	}
}

func TestSTLMeshFindingsDoNotRejectJobs(t *testing.T) {
	srv := newCADTestServer(t)
	body, ct := multipartBody(t, "open.stl", "png", meshSTL([][3]int{{0, 1, 2}}))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	raw := rec.Body.String()
	for _, code := range []string{"stl_open", "stl_non_manifold", "stl_unoriented"} {
		if strings.Contains(raw, code) {
			t.Fatalf("create body names %s: %s", code, raw)
		}
	}
	switch rec.Code {
	case http.StatusAccepted:
	case http.StatusUnprocessableEntity:
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode create body: %v; body=%s", err, raw)
		}
		if resp["code"] != errcode.UnsupportedConversion {
			t.Fatalf("create code = %q, want %q; body=%s", resp["code"], errcode.UnsupportedConversion, raw)
		}
	default:
		t.Fatalf("create status = %d, want 202 or 422; body=%s", rec.Code, raw)
	}
}

func meshSTL(tris [][3]int) []byte {
	buf := make([]byte, 84+50*len(tris))
	binary.LittleEndian.PutUint32(buf[80:84], uint32(len(tris)))
	for i, tri := range tris {
		rec := buf[84+50*i:]
		for v := 0; v < 3; v++ {
			off := 12 + 4*(3*v)
			binary.LittleEndian.PutUint32(rec[off:], math.Float32bits(float32(tri[v])))
		}
	}
	return buf
}
