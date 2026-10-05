package api

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/errcode"
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
	if len(srv.maxFormatBytes) != 1 || len(srv.maxFormatBytes[convert.EngineCAD]) != 2 {
		t.Errorf("maxFormatBytes = %v, want exactly cad: step and iges", srv.maxFormatBytes)
	}
}

func TestCADFormatCeilingCallerMapNeverMutated(t *testing.T) {
	supplied := map[string]map[string]int64{convert.EnginePandoc: {"md": 1 << 20}}
	_ = newFormatCeilingServer(t, &fakeRepo{}, &fakeStorage{}, &fakeQueue{}, supplied)
	if len(supplied) != 1 || len(supplied[convert.EnginePandoc]) != 1 {
		t.Errorf("caller-supplied map changed to %v", supplied)
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
