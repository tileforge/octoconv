package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/inspect"
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

func TestCADCreateRejections(t *testing.T) {
	srv := newCADTestServer(t)
	cases := []struct {
		file    string
		code    string
		message string
	}{
		{"step_truncated.step", errcode.TruncatedCADFile, "truncated or incomplete step file"},
		{"step_missing_header.step", errcode.TruncatedCADFile, "truncated or incomplete step file"},
		{"step_trailing_nul.step", errcode.TruncatedCADFile, "truncated or incomplete step file"},
		{"iges_truncated.iges", errcode.TruncatedCADFile, "truncated or incomplete iges file"},
		{"dxf_truncated.dxf", errcode.TruncatedCADFile, "truncated or incomplete dxf file"},
		{"stl_binary_truncated.stl", errcode.TruncatedCADFile, "truncated or incomplete stl file"},
		{"stl_binary_trailing_byte.stl", errcode.TruncatedCADFile, "truncated or incomplete stl file"},
		{"stl_ascii_truncated.stl", errcode.TruncatedCADFile, "truncated or incomplete stl file"},
		{"stl_ambiguous.stl", errcode.AmbiguousSTL, "stl file is valid as both ascii and binary; re-export as binary stl"},
		{"dxf_binary.dxf", errcode.BinaryDXFUnsupported, "binary dxf is not supported; re-export as ascii dxf"},
		{"dwg_ac1027.dwg", errcode.UnsupportedConversion, "unsupported conversion: dwg -> png"},
		{"step_bom.step", errcode.InvalidRequest, ""},
		{"dxf_bom.dxf", errcode.InvalidRequest, ""},
		{"stl_binary_ac10_header.stl", errcode.UnsupportedConversion, ""},
		{"stl_binary_empty.stl", errcode.UnsupportedConversion, ""},
	}

	entries, err := os.ReadDir(filepath.Join("..", "convert", "testdata", "cad", "reject"))
	if err != nil {
		t.Fatalf("read reject dir: %v", err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			continue
		}
		onDisk[e.Name()] = true
	}
	inTable := map[string]bool{}
	for _, tc := range cases {
		inTable[tc.file] = true
		if !onDisk[tc.file] {
			t.Errorf("table lists %s, which is not in testdata/cad/reject", tc.file)
		}
	}
	for name := range onDisk {
		if !inTable[name] {
			t.Errorf("testdata/cad/reject/%s has no row in the table", name)
		}
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			data := readCADFixture(t, filepath.Join("reject", tc.file))
			status, resp := postCADCreate(t, srv, tc.file, data)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; resp=%v", status, resp)
			}
			if resp["code"] != tc.code {
				t.Errorf("code = %q, want %q; resp=%v", resp["code"], tc.code, resp)
			}
			if tc.message != "" && resp["error"] != tc.message {
				t.Errorf("error = %q, want %q", resp["error"], tc.message)
			}
			if tc.message != "" && strings.Contains(resp["error"], tc.file) {
				t.Errorf("error %q echoes the filename", resp["error"])
			}
		})
	}
}

func TestCADRejectionCodeMapComplete(t *testing.T) {
	all := convert.AllCADRejections()
	if len(cadRejectionCode) != len(all) {
		t.Fatalf("cadRejectionCode has %d entries, want %d", len(cadRejectionCode), len(all))
	}
	for _, rej := range all {
		code, ok := cadRejectionCode[rej]
		if !ok {
			t.Errorf("rejection %q has no code", rej)
			continue
		}
		if !errcode.IsKnown(code) {
			t.Errorf("rejection %q maps to %q, which errcode does not know", rej, code)
		}
		if !slices.Contains(inspectPublicSentinelFormats, code) {
			t.Errorf("rejection %q maps to %q, which is not a public inspect sentinel", rej, code)
		}
	}
}

func TestCADSentinelsDocumentedInInspectFormat(t *testing.T) {
	raw, err := os.ReadFile(inspectOfferSpecPath)
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	spec := string(raw)
	start := strings.Index(spec, "\n    InspectReport:")
	if start < 0 {
		t.Fatal("InspectReport schema not found")
	}
	end := strings.Index(spec[start:], "\n        declared_format:")
	if end < 0 {
		t.Fatal("declared_format not found after InspectReport")
	}
	section := spec[start : start+end]
	for _, code := range cadRejectionCode {
		if !strings.Contains(section, "`"+code+"`") {
			t.Errorf("InspectReport.format description does not name %s", code)
		}
	}
}

type cadAgreeInput struct {
	name string
	file string
	data []byte
}

func cadAgreementInputs(t *testing.T) ([]cadAgreeInput, int) {
	t.Helper()
	var inputs []cadAgreeInput
	files := 0
	for _, dir := range []string{"public", "synthetic", "reject"} {
		entries, err := os.ReadDir(filepath.Join("..", "convert", "testdata", "cad", dir))
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) == ".md" {
				continue
			}
			files++
			inputs = append(inputs, cadAgreeInput{
				name: dir + "/" + e.Name(),
				file: e.Name(),
				data: readCADFixture(t, filepath.Join(dir, e.Name())),
			})
		}
	}
	lm := readCADFixture(t, "public/LM8UU.step")
	iges := readCADFixture(t, "synthetic/surfaces_only.iges")
	crlf := bytes.ReplaceAll(bytes.ReplaceAll(iges, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
	inputs = append(inputs,
		cadAgreeInput{"memory/aaaa.dxf", "x.dxf", bytes.Repeat([]byte("A"), 1000)},
		cadAgreeInput{"memory/aaaa.step", "x.step", bytes.Repeat([]byte("A"), 1000)},
		cadAgreeInput{"memory/half_LM8UU.step", "half.step", lm[:len(lm)/2]},
		cadAgreeInput{"memory/crlf_surfaces_only.igs", "crlf.igs", crlf},
	)
	return inputs, files
}

func TestCADCreateInspectAgree(t *testing.T) {
	srv := newCADTestServer(t)
	inputs, files := cadAgreementInputs(t)
	if files < 52 {
		t.Fatalf("read %d corpus files, want at least 52", files)
	}

	wantReason := map[string]inspect.Reason{
		errcode.TruncatedCADFile:     inspect.ReasonMalformedInput,
		errcode.AmbiguousSTL:         inspect.ReasonMalformedInput,
		errcode.BinaryDXFUnsupported: inspect.ReasonParserUnavailable,
	}
	for _, in := range inputs {
		t.Run(in.name, func(t *testing.T) {
			status, resp := postCADCreate(t, srv, in.file, in.data)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("create status = %d, want 422; resp=%v", status, resp)
			}
			rep := postCADInspect(t, srv, in.file, in.data)
			var mismatch *inspect.Check
			for i := range rep.Checks {
				if rep.Checks[i].Code == inspect.CodeExtensionMismatch {
					mismatch = &rep.Checks[i]
				}
			}
			for _, f := range rep.Findings {
				if f.Code == inspect.CodeExtensionMismatch {
					t.Fatalf("extension_mismatch is a finding for %s; format=%q", in.name, rep.Format)
				}
			}
			if mismatch == nil {
				t.Fatalf("no extension_mismatch check for %s", in.name)
			}
			code := resp["code"]
			switch code {
			case errcode.TruncatedCADFile, errcode.AmbiguousSTL, errcode.BinaryDXFUnsupported:
				if rep.Format != code {
					t.Errorf("create code %q but inspect format %q", code, rep.Format)
				}
				if mismatch.Status != inspect.StatusUnknown || mismatch.Reason != wantReason[code] {
					t.Errorf("extension_mismatch = %+v, want unknown/%s", *mismatch, wantReason[code])
				}
			case errcode.UnsupportedConversion:
				want := convert.NormalizeFormat(filepath.Ext(in.file))
				if rep.Format != want {
					t.Errorf("create accepted the content as %s but inspect format = %q", want, rep.Format)
				}
				if mismatch.Status != inspect.StatusClean {
					t.Errorf("extension_mismatch = %+v, want clean", *mismatch)
				}
			case errcode.InvalidRequest:
				if rep.Format != "" {
					t.Errorf("create found no verdict but inspect format = %q", rep.Format)
				}
				if mismatch.Status != inspect.StatusUnknown || mismatch.Reason != inspect.ReasonFormatUnrecognized {
					t.Errorf("extension_mismatch = %+v, want unknown/format_unrecognized", *mismatch)
				}
			default:
				t.Errorf("create answered code %q, which this test does not classify", code)
			}
		})
	}
}

type failingTailMultipartFile struct {
	*bytes.Reader
	failFrom int64
	failErr  error
}

func (f *failingTailMultipartFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.failFrom {
		return 0, f.failErr
	}
	return f.Reader.ReadAt(p, off)
}

func (f *failingTailMultipartFile) Close() error { return nil }

var _ io.ReaderAt = (*failingTailMultipartFile)(nil)

func TestInspectCADTailReadFailureIsReadFailed(t *testing.T) {
	data := paddedSTEP(t)
	file := &failingTailMultipartFile{
		Reader:   bytes.NewReader(data),
		failFrom: convert.SniffWindowBytes,
		failErr:  errors.New("simulated tail read failure"),
	}

	detected, sniffed, sniffedZip := detectInspectFormat(file, int64(len(data)), "step", inspect.NewTracker(inspect.Budget{}))
	if detected != inspectSniffReadFailedFormat {
		t.Fatalf("detected = %q, want %q", detected, inspectSniffReadFailedFormat)
	}

	tr := inspect.NewTracker(inspect.Budget{MaxBytes: 64 << 20, MaxDuration: time.Minute})
	families := dispatchInspectFamilies(detected, sniffed, sniffedZip, file, int64(len(data)), tr)
	families = append(families, extensionMismatchFamilyResult(detected, "step"))
	report := inspect.BuildReport(reportFormat(detected), "step", int64(len(data)), tr, families...)
	if report.Format != "" {
		t.Errorf("report format = %q, want empty", report.Format)
	}
	sawReadFailed := false
	for _, c := range report.Checks {
		if c.Reason == inspect.ReasonFormatUnrecognized {
			t.Errorf("check %+v is format_unrecognized for a read failure", c)
		}
		if c.Reason == inspect.ReasonReadFailed {
			sawReadFailed = true
		}
	}
	if !sawReadFailed {
		t.Errorf("no read_failed check in %+v", report.Checks)
	}
}
