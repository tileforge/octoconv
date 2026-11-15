package api

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
)

func newFormatCeilingServer(t *testing.T, repo *fakeRepo, store *fakeStorage, q *fakeQueue, formats map[string]map[string]int64) *Server {
	t.Helper()
	return NewServer(repo, store, q, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        8 << 20,
		InspectMaxUploadBytes: 8 << 20,
		InspectMaxWorkBytes:   8 << 20,
		MaxFormatBytes:        formats,
	})
}

func paddedMarkdown(size int) []byte {
	out := append([]byte{}, markdownFixture()...)
	return append(out, bytes.Repeat([]byte(" "), size-len(out))...)
}

func postFormatCeilingCreate(t *testing.T, srv *Server, filename, target string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, filename, target, data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestCADFormatCeilingCreateRefusesBeforeUpload(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	srv := newFormatCeilingServer(t, repo, store, q, map[string]map[string]int64{
		convert.EnginePandoc: {"md": 1 << 20},
	})

	rec := postFormatCeilingCreate(t, srv, "in.md", "html", paddedMarkdown(1<<20+1))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	if want := `"code":"` + errcode.PayloadTooLarge + `"`; !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
		t.Errorf("body = %s, want it to carry %s", rec.Body.String(), want)
	}
	if want := "md file exceeds the 1 MiB limit for this conversion"; !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
		t.Errorf("body = %s, want message %q", rec.Body.String(), want)
	}
	if store.uploaded || store.uploadCalls != 0 {
		t.Error("a per-format refusal must not reach storage.Upload")
	}
	if repo.created != nil {
		t.Error("a per-format refusal must not create a job")
	}
	if q.enqueueCalls != 0 {
		t.Errorf("enqueueCalls = %d, want 0", q.enqueueCalls)
	}
}

func TestCADFormatCeilingAtLimitAndOtherFormatPass(t *testing.T) {
	formats := map[string]map[string]int64{convert.EnginePandoc: {"md": 1 << 20}}

	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	srv := newFormatCeilingServer(t, repo, store, q, formats)
	rec := postFormatCeilingCreate(t, srv, "in.md", "html", paddedMarkdown(1<<20))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a file exactly at the limit: status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	repo, store, q = &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	srv = newFormatCeilingServer(t, repo, store, q, formats)
	rst := append([]byte("Title\n=====\n\nA small rst document.\n"), bytes.Repeat([]byte(" "), 1<<20+1)...)
	rec = postFormatCeilingCreate(t, srv, "in.rst", "html", rst)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("another pandoc format at the same size: status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if q.enqueuedPandoc == uuid.Nil {
		t.Error("the other format must still reach the pandoc queue")
	}
}

func TestCADFormatCeilingInspectReportsMaxBytes(t *testing.T) {
	srv := newFormatCeilingServer(t, &fakeRepo{}, &fakeStorage{}, &fakeQueue{}, map[string]map[string]int64{
		convert.EnginePandoc: {"md": 1 << 20},
	})
	rep := postCADInspect(t, srv, "in.md", paddedMarkdown(1<<20+1))
	pandocOffers := 0
	for _, o := range rep.Operations {
		if o.Engine != convert.EnginePandoc {
			continue
		}
		pandocOffers++
		if o.MaxBytes != 1<<20 {
			t.Errorf("offer %+v: MaxBytes = %d, want 1048576", o, o.MaxBytes)
		}
		if o.Available {
			t.Errorf("offer %+v: Available = true, want false above the per-format limit", o)
		}
		if o.UnavailableReason != errcode.PayloadTooLarge {
			t.Errorf("offer %+v: UnavailableReason = %q, want %q", o, o.UnavailableReason, errcode.PayloadTooLarge)
		}
	}
	if pandocOffers == 0 {
		t.Fatalf("no pandoc offer for a markdown file; operations=%+v", rep.Operations)
	}
}

func TestCADFormatCeilingMessageStatesNonMiBLimitInBytes(t *testing.T) {
	if got, want := formatLimitMessage("step", 63<<20), "step file exceeds the 63 MiB limit for this conversion"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	if got, want := formatLimitMessage("iges", 1<<20+1), "iges file exceeds the 1048577 bytes limit for this conversion"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestCADFormatCeilingDefaultsPinnedToConvertConstants(t *testing.T) {
	srv := newCADTestServer(t)
	if got := srv.maxFormatBytes[convert.EngineCAD]["step"]; got != convert.CADMaxSTEPBytes {
		t.Errorf("cad step ceiling = %d, want convert.CADMaxSTEPBytes = %d", got, int64(convert.CADMaxSTEPBytes))
	}
	if got := srv.maxFormatBytes[convert.EngineCAD]["iges"]; got != convert.CADMaxIGESBytes {
		t.Errorf("cad iges ceiling = %d, want convert.CADMaxIGESBytes = %d", got, int64(convert.CADMaxIGESBytes))
	}
	if got := srv.maxFormatBytes[convert.EngineCAD]["stl"]; got != convert.CADMaxSTLBytes {
		t.Errorf("cad stl ceiling = %d, want convert.CADMaxSTLBytes = %d", got, int64(convert.CADMaxSTLBytes))
	}
	if got := srv.maxFormatBytes[convert.EngineCAD]["dxf"]; got != convert.CADMaxDXFBytes {
		t.Errorf("cad dxf ceiling = %d, want convert.CADMaxDXFBytes = %d", got, int64(convert.CADMaxDXFBytes))
	}
	if len(srv.maxFormatBytes) != 1 || len(srv.maxFormatBytes[convert.EngineCAD]) != 4 {
		t.Errorf("maxFormatBytes = %v, want exactly cad: step, iges, stl and dxf", srv.maxFormatBytes)
	}
}

func TestCADFormatCeilingCallerMapNeverMutated(t *testing.T) {
	supplied := map[string]map[string]int64{convert.EnginePandoc: {"md": 1 << 20}}
	_ = newFormatCeilingServer(t, &fakeRepo{}, &fakeStorage{}, &fakeQueue{}, supplied)
	if len(supplied) != 1 || len(supplied[convert.EnginePandoc]) != 1 {
		t.Errorf("caller-supplied map changed to %v", supplied)
	}
}

func TestCADFormatCeilingDWGHasNoCeiling(t *testing.T) {
	srv := newCADTestServer(t)
	if limit, perFormat, gated := srv.sizeLimit(convert.EngineCAD, "dwg"); limit != 0 || perFormat || gated {
		t.Errorf("dwg: got (%d, %t, %t), want (0, false, false)", limit, perFormat, gated)
	}
	if _, ok := srv.maxFormatBytes[convert.EngineCAD]["dwg"]; ok {
		t.Error("dwg is a key in the format ceiling map")
	}
}

func TestCADFormatCeilingSizeLimitPrefersFormatThenEngine(t *testing.T) {
	srv := newFormatCeilingServer(t, &fakeRepo{}, &fakeStorage{}, &fakeQueue{}, map[string]map[string]int64{
		convert.EnginePandoc: {"md": 1 << 20},
	})
	if limit, perFormat, gated := srv.sizeLimit(convert.EnginePandoc, "md"); limit != 1<<20 || !perFormat || !gated {
		t.Errorf("md: got (%d, %t, %t), want (1048576, true, true)", limit, perFormat, gated)
	}
	if limit, perFormat, gated := srv.sizeLimit(convert.EnginePandoc, "rst"); limit != 100<<20 || perFormat || !gated {
		t.Errorf("rst: got (%d, %t, %t), want the 100 MiB engine limit", limit, perFormat, gated)
	}
	if _, perFormat, gated := srv.sizeLimit("nonexistent", "md"); perFormat || gated {
		t.Errorf("unknown engine: got perFormat=%t gated=%t, want neither", perFormat, gated)
	}
}

func lyingBinarySTL() []byte {
	data := make([]byte, 80, 80+4+50)
	data = binary.LittleEndian.AppendUint32(data, 0xFFFFFFFF)
	return append(data, make([]byte, 50)...)
}

func TestCADLyingSTLRefusedAtCreateWithoutUploadOrEnqueue(t *testing.T) {
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	srv := newFormatCeilingServer(t, repo, store, q, nil)

	rec := postFormatCeilingCreate(t, srv, "part.stl", "png", lyingBinarySTL())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if want := `"code":"` + errcode.TruncatedCADFile + `"`; !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
		t.Errorf("body = %s, want it to carry %s", rec.Body.String(), want)
	}
	if store.uploaded || store.uploadCalls != 0 {
		t.Error("a lying STL must not reach storage.Upload")
	}
	if repo.created != nil {
		t.Error("a lying STL must not create a job")
	}
	if q.enqueueCalls != 0 {
		t.Errorf("enqueueCalls = %d, want 0", q.enqueueCalls)
	}
}

func cadStepPaddedTo(t *testing.T, size int) []byte {
	t.Helper()
	src := readCADFixture(t, "public/nist_stc_06_asme1_ap242-e3.stp")
	i := bytes.Index(src, []byte("DATA;"))
	if i < 0 {
		t.Fatal("no DATA; line in nist_stc_06_asme1_ap242-e3.stp")
	}
	i += len("DATA;")
	for i < len(src) && src[i] != '\n' {
		i++
	}
	i++
	out := make([]byte, 0, size)
	out = append(out, src[:i]...)
	out = append(out, bytes.Repeat([]byte{'\n'}, size-len(src))...)
	return append(out, src[i:]...)
}

func cadIGESPaddedTo(t *testing.T, size int) []byte {
	t.Helper()
	src := readCADFixture(t, "synthetic/surfaces_only.iges")
	lines := strings.Split(strings.TrimRight(string(src), "\n"), "\n")
	last := lines[len(lines)-1]
	var counts [3]int
	for i, from := range []int{9, 17, 25} {
		n, err := strconv.Atoi(strings.TrimSpace(last[from : from+7]))
		if err != nil {
			t.Fatalf("parse the terminate record %q: %v", last[:32], err)
		}
		counts[i] = n
	}
	extra := (size - len(src) + 80) / 81
	var b strings.Builder
	b.Grow(size + 1024)
	b.WriteString(lines[0] + "\n")
	pad := strings.Repeat(" ", 72)
	for k := 0; k < extra; k++ {
		fmt.Fprintf(&b, "%sS%07d\n", pad, k+2)
	}
	for _, l := range lines[1 : len(lines)-1] {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "%-72sT0000001\n", fmt.Sprintf("S%7dG%7dD%7dP%7d", 1+extra, counts[0], counts[1], counts[2]))
	return []byte(b.String())
}

func newCADCeilingServer(repo *fakeRepo, store *fakeStorage, q *fakeQueue) *Server {
	return NewServer(repo, store, q, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxUploadBytes:        256 << 20,
		InspectMaxUploadBytes: 256 << 20,
		InspectMaxWorkBytes:   256 << 20,
	})
}

func TestCADCeilingsAtTheDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("builds inputs above the STEP and IGES ceilings")
	}
	cases := []struct {
		name     string
		filename string
		data     func(t *testing.T) []byte
		message  string
	}{
		{"step", "big.step", func(t *testing.T) []byte { return cadStepPaddedTo(t, convert.CADMaxSTEPBytes+1) }, fmt.Sprintf("step file exceeds the %d MiB limit for this conversion", convert.CADMaxSTEPBytes>>20)},
		{"stp_alias", "big.stp", func(t *testing.T) []byte { return cadStepPaddedTo(t, convert.CADMaxSTEPBytes+1) }, fmt.Sprintf("step file exceeds the %d MiB limit for this conversion", convert.CADMaxSTEPBytes>>20)},
		{"iges", "big.iges", func(t *testing.T) []byte { return cadIGESPaddedTo(t, convert.CADMaxIGESBytes+1) }, fmt.Sprintf("iges file exceeds the %d MiB limit for this conversion", convert.CADMaxIGESBytes>>20)},
		{"igs_alias", "big.igs", func(t *testing.T) []byte { return cadIGESPaddedTo(t, convert.CADMaxIGESBytes+1) }, fmt.Sprintf("iges file exceeds the %d MiB limit for this conversion", convert.CADMaxIGESBytes>>20)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.data(t)
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			srv := newCADCeilingServer(repo, store, q)

			body, ct := multipartBody(t, tc.filename, "stl", data)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413; body=%.300s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+errcode.PayloadTooLarge+`"`) || !strings.Contains(rec.Body.String(), tc.message) {
				t.Errorf("body = %s, want code %s and message %q", rec.Body.String(), errcode.PayloadTooLarge, tc.message)
			}
			if store.uploaded || store.uploadCalls != 0 || repo.created != nil || q.enqueueCalls != 0 {
				t.Errorf("a refused file must not upload, create or enqueue: uploadCalls=%d created=%v enqueueCalls=%d", store.uploadCalls, repo.created != nil, q.enqueueCalls)
			}
		})
	}
}

const cad76DerivedEvidence = "../convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-03-derived.txt"

func cad76DerivedCeiling(t *testing.T, format string) int64 {
	t.Helper()
	raw, err := os.ReadFile(cad76DerivedEvidence)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^DERIVED,key=cad_` + regexp.QuoteMeta(format) + `_ceiling_bytes,value=(\d+),`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no DERIVED cad_%s_ceiling_bytes line in %s", format, cad76DerivedEvidence)
	}
	n, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func cadZeroSTLAbove(size int64) []byte {
	n := (size - 84 + 50) / 50
	data := make([]byte, 84+n*50)
	binary.LittleEndian.PutUint32(data[80:], uint32(n))
	return data
}

func TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("builds inputs above the STEP, IGES and STL ceilings")
	}
	type route struct{ target, operation string }
	for _, tc := range []struct {
		format   string
		filename string
		build    func(t *testing.T, ceiling int64) []byte
		routes   []route
	}{
		{"step", "big.step", func(t *testing.T, c int64) []byte { return cadStepPaddedTo(t, int(c)+1) },
			[]route{{"stl", convert.OperationConvert}, {"png", convert.OperationRender}}},
		{"iges", "big.iges", func(t *testing.T, c int64) []byte { return cadIGESPaddedTo(t, int(c)+1) },
			[]route{{"stl", convert.OperationConvert}, {"png", convert.OperationRender}}},
		{"stl", "big.stl", func(t *testing.T, c int64) []byte { return cadZeroSTLAbove(c) },
			[]route{{"png", convert.OperationRender}}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			ceiling := cad76DerivedCeiling(t, tc.format)
			if ceiling%(1<<20) != 0 {
				t.Fatalf("the derived %s ceiling %d is not whole MiB", tc.format, ceiling)
			}
			data := tc.build(t, ceiling)
			if int64(len(data)) <= ceiling {
				t.Fatalf("built %d bytes, want more than the ceiling %d", len(data), ceiling)
			}
			message := fmt.Sprintf("%s file exceeds the %d MiB limit for this conversion", tc.format, ceiling>>20)
			for _, r := range tc.routes {
				repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
				srv := newCADCeilingServer(repo, store, q)
				body, ct := multipartBodyWithOperation(t, tc.filename, r.target, r.operation, data)
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				rec := httptest.NewRecorder()
				srv.Routes().ServeHTTP(rec, req)
				if rec.Code != http.StatusRequestEntityTooLarge {
					t.Fatalf("%s %s: status = %d, want 413; body=%.300s", r.operation, r.target, rec.Code, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), `"code":"`+errcode.PayloadTooLarge+`"`) || !strings.Contains(rec.Body.String(), message) {
					t.Errorf("%s %s: body = %s, want code %s and message %q", r.operation, r.target, rec.Body.String(), errcode.PayloadTooLarge, message)
				}
				if store.uploaded || store.uploadCalls != 0 || repo.created != nil || q.enqueueCalls != 0 {
					t.Errorf("%s %s: a refused file must not upload, create or enqueue: uploadCalls=%d created=%v enqueueCalls=%d", r.operation, r.target, store.uploadCalls, repo.created != nil, q.enqueueCalls)
				}
			}
			rep := postCADInspect(t, newCADCeilingServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}), tc.filename, data)
			seen := map[route]bool{}
			for _, o := range rep.Operations {
				if o.Engine != convert.EngineCAD {
					continue
				}
				seen[route{o.Target, o.Operation}] = true
				if o.MaxBytes != ceiling || o.Available || o.UnavailableReason != errcode.PayloadTooLarge {
					t.Errorf("offer %+v: want MaxBytes %d, Available false and %s", o, ceiling, errcode.PayloadTooLarge)
				}
			}
			for _, r := range tc.routes {
				if !seen[r] {
					t.Errorf("no cad %s %s offer for a %s file above its ceiling; operations=%+v", r.operation, r.target, tc.format, rep.Operations)
				}
			}
			if len(seen) != len(tc.routes) {
				t.Errorf("cad offers %v for a %s file, want exactly %v", seen, tc.format, tc.routes)
			}
		})
	}
}

func TestCADInspectReportsPerFormatCeilingAsMaxBytes(t *testing.T) {
	cases := []struct {
		name       string
		filename   string
		fixture    string
		ceiling    int64
		wantOffers map[string]bool
	}{
		{"step", "LM8UU.step", "public/LM8UU.step", convert.CADMaxSTEPBytes,
			map[string]bool{convert.OperationConvert + " stl": true, convert.OperationRender + " png": true}},
		{"iges", "surfaces_only.iges", "synthetic/surfaces_only.iges", convert.CADMaxIGESBytes,
			map[string]bool{convert.OperationConvert + " stl": true, convert.OperationRender + " png": true}},
		{"stl", "hubble.stl", "public/nasa-hubble-all-pieces.stl", convert.CADMaxSTLBytes,
			map[string]bool{convert.OperationRender + " png": true}},
		{"dxf", "legacy_cp1251.dxf", "synthetic/legacy_cp1251.dxf", convert.CADMaxDXFBytes,
			map[string]bool{convert.OperationConvert + " svg": true, convert.OperationConvert + " pdf": true, convert.OperationRender + " png": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCADCeilingServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
			rep := postCADInspect(t, srv, tc.filename, readCADFixture(t, tc.fixture))
			seen := map[string]bool{}
			for _, o := range rep.Operations {
				if o.Engine != convert.EngineCAD {
					continue
				}
				key := o.Operation + " " + o.Target
				if !tc.wantOffers[key] {
					t.Errorf("offer %+v: unexpected cad offer for a %s file, want %v", o, tc.name, tc.wantOffers)
					continue
				}
				seen[key] = true
				if o.MaxBytes != tc.ceiling {
					t.Errorf("offer %+v: MaxBytes = %d, want %d", o, o.MaxBytes, tc.ceiling)
				}
				if !o.Available {
					t.Errorf("offer %+v: a small %s file must be available", o, tc.name)
				}
				if o.EstimateSource != EstimateSourceMeasured || o.EstimatedMaxSeconds != int(convert.CADDefaultEngineTimeout/time.Second) {
					t.Errorf("offer %+v: EstimateSource = %q and EstimatedMaxSeconds = %d, want %q and the measured %s", o, o.EstimateSource, o.EstimatedMaxSeconds, EstimateSourceMeasured, convert.CADDefaultEngineTimeout)
				}
			}
			for key := range tc.wantOffers {
				if !seen[key] {
					t.Errorf("no cad %s offer for a %s file; operations=%+v", key, tc.name, rep.Operations)
				}
			}
		})
	}
}

func cadDXFPaddedTo(t *testing.T, size int) []byte {
	t.Helper()
	src := readCADFixture(t, "synthetic/legacy_cp1251.dxf")
	at := bytes.Index(src, []byte("  0\r\nENDSEC\r\n"))
	if at < 0 {
		t.Fatal("no ENDSEC in legacy_cp1251.dxf")
	}
	const chunk = 1025
	need := size - len(src)
	if need < chunk {
		t.Fatalf("size %d leaves no room for padding", size)
	}
	full := need/chunk - 1
	last := need - full*chunk
	out := make([]byte, 0, size)
	out = append(out, src[:at]...)
	line := append(append([]byte("999\r\n"), bytes.Repeat([]byte("x"), chunk-7)...), "\r\n"...)
	for k := 0; k < full; k++ {
		out = append(out, line...)
	}
	out = append(out, "999\r\n"...)
	out = append(out, bytes.Repeat([]byte("x"), last-7)...)
	out = append(out, "\r\n"...)
	out = append(out, src[at:]...)
	if len(out) != size {
		t.Fatalf("padded dxf is %d bytes, want %d", len(out), size)
	}
	return out
}

func TestCADDXFCeilingAtTheDefaultRefusesBeforeUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an input above 118 MiB")
	}
	for _, tc := range []struct {
		target    string
		operation string
	}{
		{"svg", ""},
		{"png", convert.OperationRender},
	} {
		t.Run(tc.target, func(t *testing.T) {
			repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			srv := newCADCeilingServer(repo, store, q)
			var body *bytes.Buffer
			var ct string
			if tc.operation == "" {
				body, ct = multipartBody(t, "big.dxf", tc.target, cadDXFPaddedTo(t, convert.CADMaxDXFBytes+1))
			} else {
				body, ct = multipartBodyWithOperation(t, "big.dxf", tc.target, tc.operation, cadDXFPaddedTo(t, convert.CADMaxDXFBytes+1))
			}
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413; body=%.300s", rec.Code, rec.Body.String())
			}
			want := fmt.Sprintf("dxf file exceeds the %d MiB limit for this conversion", convert.CADMaxDXFBytes>>20)
			if !strings.Contains(rec.Body.String(), `"code":"`+errcode.PayloadTooLarge+`"`) || !strings.Contains(rec.Body.String(), want) {
				t.Errorf("body = %s, want code %s and message %q", rec.Body.String(), errcode.PayloadTooLarge, want)
			}
			if store.uploaded || store.uploadCalls != 0 || repo.created != nil || q.enqueueCalls != 0 {
				t.Errorf("a refused file must not upload, create or enqueue: uploadCalls=%d created=%v enqueueCalls=%d", store.uploadCalls, repo.created != nil, q.enqueueCalls)
			}
		})
	}
}

func TestCADDXFPaddedInputIsAcceptedBelowTheCeiling(t *testing.T) {
	data := cadDXFPaddedTo(t, 1<<20)
	repo, store, q := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
	srv := newCADCeilingServer(repo, store, q)
	admitCADCatalog(srv)
	rec := postFormatCeilingCreate(t, srv, "padded.dxf", "svg", data)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	assertAdmittedCAD(t, repo, q, convert.OperationConvert, "dxf", "svg", "octoconv.convert.caddraw")
}
