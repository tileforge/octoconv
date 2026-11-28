package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
)

type repeatRepoFake struct {
	fakeRepo
	inputs []jobs.Input
}

func (r *repeatRepoFake) Inputs(context.Context, uuid.UUID) ([]jobs.Input, error) {
	return r.inputs, nil
}

type repeatStoreFake struct {
	fakeStorage
	available                    bool
	probes, reads                int
	probeErr                     error
	data                         []byte
	dataByKey                    map[string][]byte
	expireOnRead                 bool
	streamFailure, streamExpired bool
}

func (s *repeatStoreFake) ObjectExists(context.Context, string) (bool, error) {
	s.probes++
	return s.available, s.probeErr
}
func (s *repeatStoreFake) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.reads++
	if s.streamFailure {
		return &repeatFailingStream{store: s}, nil
	}
	if s.expireOnRead {
		s.available = false
		return nil, errors.New("expired during preparation")
	}
	data := s.data
	if value, ok := s.dataByKey[key]; ok {
		data = value
	}
	if data == nil {
		data = pngBytesFixture()
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func TestRepeatMetadataDoesNotCreateOrExposeKeys(t *testing.T) {
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, Operation: "convert", SourceFormat: "png", TargetFormat: "jpg", Opts: map[string]any{"zero": 0}}}, inputs: []jobs.Input{{Filename: "source.png", Format: "png", ObjectKey: "private/key", SizeBytes: 24}}}
	store := &repeatStoreFake{available: true}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("GET", "/v1/jobs/"+id.String()+"/repeat", nil)))
	if rec.Code != 200 || repo.created != nil || store.uploaded || store.reads != 0 || strings.Contains(rec.Body.String(), "private/key") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
func TestRepeatCreatesFreshTaskThroughCurrentIntake(t *testing.T) {
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, Operation: "convert", SourceFormat: "png", TargetFormat: "jpg", CallbackURL: "https://old.invalid", ProgressWebhook: true}}, inputs: []jobs.Input{{Filename: "source.png", Format: "png", ObjectKey: "uploads/old", SizeBytes: 24}}}
	store := &repeatStoreFake{available: true}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	srv.SetCapabilityCatalog(imageCatalog())
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"webp","opts":{}}`))))
	if rec.Code != 202 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	p := repo.created
	if p == nil || p.ID == id || p.Operation != "convert" || p.Engine != "image" || p.TargetFormat != "webp" || p.CallbackURL != "" || p.ProgressWebhook || p.CorrelationID != "" || len(p.Inputs) != 1 || p.Inputs[0].ObjectKey == "uploads/old" {
		t.Fatalf("created %+v", p)
	}
	if _, ok := p.Opts["archive_extraction"]; ok {
		t.Fatalf("extraction opts %+v", p.Opts)
	}
}

func TestRepeatExtractionForwardsSource(t *testing.T) {
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{
		ID: id, Status: jobs.StatusDone, Operation: "convert", SourceFormat: "png", TargetFormat: "webp",
		Opts: map[string]any{"archive_extraction": map[string]any{"phase": "unpack"}},
	}}, inputs: []jobs.Input{{Filename: "source.png", Format: "png", ObjectKey: "uploads/old", SizeBytes: 24}}}
	store := &repeatStoreFake{available: true}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	srv.SetCapabilityCatalog(imageCatalog())
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"webp","opts":{}}`))))
	if rec.Code != http.StatusUnprocessableEntity || repo.created != nil {
		t.Fatalf("status %d created %+v body %s", rec.Code, repo.created, rec.Body.String())
	}
}

func TestRepeatSourceErrorsNeverWrite(t *testing.T) {
	for _, tc := range []struct {
		name               string
		available, foreign bool
		err                error
		code               int
	}{
		{name: "expired", code: 409}, {name: "outage", err: errors.New("offline"), code: 503}, {name: "foreign", available: true, foreign: true, code: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, SourceFormat: "png"}}, inputs: []jobs.Input{{Filename: "a.png", Format: "png", SizeBytes: 24}}}
			store := &repeatStoreFake{available: tc.available, probeErr: tc.err}
			srv, resolver := newTestServer(repo, store, &fakeQueue{})
			repo.getJob.ClientID = resolver.client.ID
			if tc.foreign {
				repo.getJob.ClientID = uuid.New()
			}
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"jpg","opts":{}}`))))
			if rec.Code != tc.code || store.uploaded || repo.created != nil || store.reads != 0 {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
			if tc.foreign && store.probes != 0 {
				t.Fatal("foreign source was probed")
			}
		})
	}
}
func TestRepeatInvalidOptionsUseCurrentValidation(t *testing.T) {
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, SourceFormat: "png"}}, inputs: []jobs.Input{{Filename: "a.png", Format: "png", SizeBytes: 24}}}
	store := &repeatStoreFake{available: true}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"jpg","opts":{"unknown":42}}`))))
	if rec.Code != 422 || store.uploaded || repo.created != nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestRepeatSourceSizeAndQuotaRejectBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int64
		preErr error
		code   int
	}{
		{"short object", 25, nil, 409}, {"long object", 23, nil, 409}, {"over limit", 101 << 20, nil, 413}, {"quota failure", 24, errors.New("database offline"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, SourceFormat: "png"}, preAdmitErr: tc.preErr}, inputs: []jobs.Input{{Filename: "a.png", Format: "png", SizeBytes: tc.size}}}
			store := &repeatStoreFake{available: true}
			srv, resolver := newTestServer(repo, store, &fakeQueue{})
			repo.getJob.ClientID = resolver.client.ID
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"jpg","opts":{}}`))))
			if rec.Code != tc.code || repo.created != nil || store.uploaded {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
			if tc.preErr != nil && store.reads != 0 {
				t.Fatal("download before quota preflight")
			}
		})
	}
}
func TestRepeatMetadataOutageIsNotExpiry(t *testing.T) {
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone}}, inputs: []jobs.Input{{Filename: "a.png"}}}
	store := &repeatStoreFake{probeErr: errors.New("offline")}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("GET", "/v1/jobs/"+id.String()+"/repeat", nil)))
	if rec.Code != 503 || strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestRepeatPreservesCompleteProjectInputs(t *testing.T) {
	id := uuid.New()
	main := []byte(`\documentclass{article}\begin{document}Hello\end{document}`)
	bib := []byte(`@book{a,title={A}}`)
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, SourceFormat: "latex"}}, inputs: []jobs.Input{{Ordinal: 0, Filename: "main.tex", Format: "latex", ObjectKey: "old/main", SizeBytes: int64(len(main))}, {Ordinal: 1, Filename: "refs.bib", Format: "bib", ObjectKey: "old/refs", SizeBytes: int64(len(bib))}}}
	store := &repeatStoreFake{available: true, dataByKey: map[string][]byte{"old/main": main, "old/refs": bib}}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: tectonicMatch()})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"pdf","opts":{}}`))))
	if rec.Code != 202 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	inputs := repo.created.Inputs
	if len(inputs) != 2 || inputs[0].Filename != "main.tex" || inputs[1].Filename != "refs.bib" || inputs[0].Ordinal != 0 || inputs[1].Ordinal != 1 || strings.HasPrefix(inputs[0].ObjectKey, "old/") || strings.HasPrefix(inputs[1].ObjectKey, "old/") {
		t.Fatalf("inputs %+v", inputs)
	}
}
func TestRepeatExpiredDuringDownloadDoesNotCreate(t *testing.T) {
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, SourceFormat: "png"}}, inputs: []jobs.Input{{Filename: "a.png", Format: "png", SizeBytes: 24}}}
	store := &repeatStoreFake{available: true, expireOnRead: true}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"jpg","opts":{}}`))))
	if rec.Code != 409 || repo.created != nil || store.uploaded {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}
func TestRepeatCancellationDuringPreparationDoesNotCreate(t *testing.T) {
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, SourceFormat: "png"}}, inputs: []jobs.Input{{Filename: "a.png", Format: "png", SizeBytes: 24}}}
	store := &repeatStoreFake{available: true}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	request := httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"jpg","opts":{}}`))
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(request.WithContext(ctx)))
	if rec.Code != http.StatusServiceUnavailable || repo.created != nil || store.uploaded {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}

type repeatFailingStream struct{ store *repeatStoreFake }

func (r *repeatFailingStream) Read([]byte) (int, error) {
	if r.store.streamExpired {
		r.store.available = false
	}
	return 0, errors.New("stream failed")
}
func (r *repeatFailingStream) Close() error { return nil }
func TestRepeatRechecksAvailabilityAfterStreamFailure(t *testing.T) {
	for _, expired := range []bool{true, false} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			id := uuid.New()
			repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone, SourceFormat: "png"}}, inputs: []jobs.Input{{Filename: "a.png", Format: "png", SizeBytes: 24}}}
			store := &repeatStoreFake{available: true, streamFailure: true, streamExpired: expired}
			srv, resolver := newTestServer(repo, store, &fakeQueue{})
			repo.getJob.ClientID = resolver.client.ID
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"jpg","opts":{}}`))))
			want := 503
			if expired {
				want = 409
			}
			if rec.Code != want || store.probes < 2 || repo.created != nil || store.uploaded {
				t.Fatalf("status %d probes %d body %s", rec.Code, store.probes, rec.Body.String())
			}
		})
	}
}

func TestRepeatArchivePreservesMemberPaths(t *testing.T) {
	a := []byte("alpha")
	b := []byte("beta")
	inputs := []jobs.Input{
		{Ordinal: 0, Filename: "a.txt", Format: "txt", ObjectKey: "old/a", SizeBytes: int64(len(a))},
		{Ordinal: 1, Filename: "b.txt", Format: "txt", ObjectKey: "old/b", SizeBytes: int64(len(b))},
	}
	storeData := map[string][]byte{"old/a": a, "old/b": b}

	t.Run("empty opts keeps stored paths", func(t *testing.T) {
		repo, store := repeatArchive(t, inputs, map[string]any{"paths": []any{"docs/a.txt", "b.txt"}}, storeData)
		rec := postRepeat(t, repo, store, `{"operation":"archive","target":"zip","opts":{}}`)
		assertArchiveRepeat(t, rec, repo, store, []string{"docs/a.txt", "b.txt"})
	})

	t.Run("replayed opts keeps stored paths", func(t *testing.T) {
		repo, store := repeatArchive(t, inputs, map[string]any{"paths": []string{"docs/a.txt", "b.txt"}}, storeData)
		body := `{"operation":"archive","target":"zip","opts":{"paths":["docs/a.txt","b.txt"]}}`
		rec := postRepeat(t, repo, store, body)
		assertArchiveRepeat(t, rec, repo, store, []string{"docs/a.txt", "b.txt"})
	})

	t.Run("one nested file keeps its directory", func(t *testing.T) {
		one := []jobs.Input{{Ordinal: 0, Filename: "a.txt", Format: "txt", ObjectKey: "old/a", SizeBytes: int64(len(a))}}
		repo, store := repeatArchive(t, one, map[string]any{"paths": []any{"docs/a.txt"}}, map[string][]byte{"old/a": a})
		rec := postRepeat(t, repo, store, `{"operation":"archive","target":"zip","opts":{}}`)
		assertArchiveRepeat(t, rec, repo, store, []string{"docs/a.txt"})
	})

	t.Run("other opts are refused before download", func(t *testing.T) {
		repo, store := repeatArchive(t, inputs, map[string]any{"paths": []any{"docs/a.txt", "b.txt"}}, storeData)
		rec := postRepeat(t, repo, store, `{"operation":"archive","target":"zip","opts":{"quality":1}}`)
		if rec.Code != http.StatusUnprocessableEntity || repo.created != nil || store.uploaded || store.reads != 0 {
			t.Fatalf("status %d reads %d body %s", rec.Code, store.reads, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.InvalidRequest)
	})

	t.Run("a different path list is refused before download", func(t *testing.T) {
		repo, store := repeatArchive(t, inputs, map[string]any{"paths": []any{"docs/a.txt", "b.txt"}}, storeData)
		body := `{"operation":"archive","target":"zip","opts":{"paths":["other/a.txt","b.txt"]}}`
		rec := postRepeat(t, repo, store, body)
		if rec.Code != http.StatusUnprocessableEntity || repo.created != nil || store.uploaded || store.reads != 0 {
			t.Fatalf("status %d reads %d body %s", rec.Code, store.reads, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.InvalidRequest)
	})
}

func repeatArchive(t *testing.T, inputs []jobs.Input, opts map[string]any, data map[string][]byte) (*repeatRepoFake, *repeatStoreFake) {
	t.Helper()
	id := uuid.New()
	repo := &repeatRepoFake{fakeRepo: fakeRepo{getJob: &jobs.Job{
		ID:           id,
		Status:       jobs.StatusDone,
		Operation:    "archive",
		SourceFormat: "files",
		TargetFormat: "zip",
		Opts:         opts,
	}}, inputs: inputs}
	store := &repeatStoreFake{available: true, dataByKey: data}
	return repo, store
}

func postRepeat(t *testing.T, repo *repeatRepoFake, store *repeatStoreFake, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: archiveMatch()})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+repo.getJob.ID.String()+"/repeat", strings.NewReader(body)))
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func assertArchiveRepeat(t *testing.T, rec *httptest.ResponseRecorder, repo *repeatRepoFake, store *repeatStoreFake, want []string) {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Operation != "archive" || repo.created.Engine != "archive" {
		t.Fatalf("created %+v", repo.created)
	}
	got := storedPaths(t, repo.created.Opts)
	if len(got) != len(want) || len(repo.created.Inputs) != len(want) {
		t.Fatalf("paths %#v inputs %+v", got, repo.created.Inputs)
	}
	for i, path := range want {
		base := path
		if slash := strings.LastIndex(path, "/"); slash >= 0 {
			base = path[slash+1:]
		}
		in := repo.created.Inputs[i]
		if got[i] != path || in.Filename != base || in.Ordinal != i {
			t.Fatalf("member %d path %q input %+v", i, got[i], in)
		}
		if strings.Contains(in.ObjectKey, "docs") || strings.Contains(in.ObjectKey, "other") {
			t.Fatalf("key uses a directory: %s", in.ObjectKey)
		}
	}
	if store.uploadCalls != len(want) {
		t.Fatalf("uploads %d", store.uploadCalls)
	}
}
