package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
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
	available     bool
	probes, reads int
	probeErr      error
	data          []byte
	dataByKey     map[string][]byte
	expireOnRead  bool
}

func (s *repeatStoreFake) ObjectExists(context.Context, string) (bool, error) {
	s.probes++
	return s.available, s.probeErr
}
func (s *repeatStoreFake) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.reads++
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
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/"+id.String()+"/repeat", strings.NewReader(`{"operation":"convert","target":"webp","opts":{}}`))))
	if rec.Code != 202 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	p := repo.created
	if p == nil || p.ID == id || p.TargetFormat != "webp" || p.CallbackURL != "" || p.ProgressWebhook || p.CorrelationID != "" || len(p.Inputs) != 1 || p.Inputs[0].ObjectKey == "uploads/old" {
		t.Fatalf("created %+v", p)
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
