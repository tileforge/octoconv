package api

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"github.com/tileforge/octoconv/internal/auth"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tileforge/octoconv/internal/jobs"
)

type archiveStorageFake struct {
	fakeStorage
	reads int
	err   error
}

func (s *archiveStorageFake) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	return io.NopCloser(strings.NewReader("abc")), nil
}

func TestArchiveValidation(t *testing.T) {
	for _, body := range []string{`{}`, `{"job_ids":[]}`, `{"job_ids":["bad"]}`, `{"job_ids":null}`, `{"job_ids":[]} {}`} {
		srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/archive", strings.NewReader(body))))
		if rec.Code != 400 {
			t.Fatalf("%s: got %d", body, rec.Code)
		}
	}
}
func TestArchiveSuccessAndOwnership(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		id := uuid.New()
		repo := &fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone}, outputs: []jobs.Output{{ObjectKey: "private/key", SizeBytes: 3, Role: "primary", Format: "png"}}}
		storage := &archiveStorageFake{}
		srv, resolver := newTestServer(repo, storage, &fakeQueue{})
		repo.getJob.ClientID = resolver.client.ID
		if foreign {
			repo.getJob.ClientID = uuid.New()
		}
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/archive", strings.NewReader(`{"job_ids":["`+id.String()+`"]}`))))
		if foreign {
			if rec.Code != 404 || storage.reads != 0 {
				t.Fatalf("foreign: %d reads %d", rec.Code, storage.reads)
			}
			continue
		}
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		z, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		if len(z.File) != 1 || strings.Contains(z.File[0].Name, "private") {
			t.Fatalf("entries: %+v", z.File)
		}
		r, err := z.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(data) != "abc" {
			t.Fatalf("content %q: %v", data, err)
		}
	}
}

func TestArchiveRejectsUnavailableAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  string
		outputs []jobs.Output
		code    int
	}{
		{"not done", jobs.StatusQueued, nil, 409},
		{"empty", jobs.StatusDone, nil, 409},
		{"oversized", jobs.StatusDone, []jobs.Output{{SizeBytes: archiveMaxBytes + 1}}, 413},
		{"negative", jobs.StatusDone, []jobs.Output{{SizeBytes: -1}}, 413},
		{"too many", jobs.StatusDone, make([]jobs.Output, 101), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			repo := &fakeRepo{getJob: &jobs.Job{ID: id, Status: tc.status}, outputs: tc.outputs}
			store := &archiveStorageFake{}
			srv, resolver := newTestServer(repo, store, &fakeQueue{})
			repo.getJob.ClientID = resolver.client.ID
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/archive", strings.NewReader(`{"job_ids":["`+id.String()+`"]}`))))
			if rec.Code != tc.code || store.reads != 0 {
				t.Fatalf("status %d reads %d", rec.Code, store.reads)
			}
		})
	}
}
func TestArchiveIncompleteStorageAbortsResponse(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone}, outputs: []jobs.Output{{SizeBytes: 4}}}
	srv, resolver := newTestServer(repo, &archiveStorageFake{}, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("expected aborted response")
		}
		if _, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len())); err == nil {
			t.Error("partial ZIP finalized")
		}
	}()
	srv.handleArchive(rec, authedArchiveRequest(resolver, id))
}
func authedArchiveRequest(resolver *fakeResolver, id uuid.UUID) *http.Request {
	r := httptest.NewRequest("POST", "/v1/jobs/archive", strings.NewReader(`{"job_ids":["`+id.String()+`"]}`))
	// Use the real authentication middleware to populate the context for direct handler testing.
	var request *http.Request
	auth.Middleware(resolver)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { request = r })).ServeHTTP(httptest.NewRecorder(), authed(r))
	return request
}

type archiveRepoFake struct {
	fakeRepo
	byID        map[uuid.UUID]*jobs.Job
	byOutput    map[uuid.UUID][]jobs.Output
	outputCalls int
}

func (r *archiveRepoFake) Get(_ context.Context, id uuid.UUID) (*jobs.Job, error) {
	j, ok := r.byID[id]
	if !ok {
		return nil, jobs.ErrNotFound
	}
	return j, nil
}
func (r *archiveRepoFake) Outputs(_ context.Context, id uuid.UUID) ([]jobs.Output, error) {
	r.outputCalls++
	return r.byOutput[id], nil
}
func (r *archiveRepoFake) Inputs(_ context.Context, id uuid.UUID) ([]jobs.Input, error) {
	return []jobs.Input{{Filename: "../../same\\source.png"}}, nil
}
func TestArchiveAuthorizesAllJobsBeforeOutputReads(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &archiveRepoFake{byID: map[uuid.UUID]*jobs.Job{a: {ID: a, Status: jobs.StatusDone}, b: {ID: b, Status: jobs.StatusDone, ClientID: uuid.New()}}}
	store := &archiveStorageFake{}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	repo.byID[a].ClientID = resolver.client.ID
	for _, other := range []uuid.UUID{b, uuid.New()} {
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/archive", strings.NewReader(`{"job_ids":["`+a.String()+`","`+other.String()+`"]}`))))
		if rec.Code != 404 || repo.outputCalls != 0 || store.reads != 0 {
			t.Fatalf("status %d metadata %d reads %d", rec.Code, repo.outputCalls, store.reads)
		}
	}
}
func TestArchiveAllOutputsHaveSafeDistinctNames(t *testing.T) {
	id := uuid.New()
	repo := &archiveRepoFake{byID: map[uuid.UUID]*jobs.Job{id: {ID: id, Status: jobs.StatusDone}}, byOutput: map[uuid.UUID][]jobs.Output{id: {{SizeBytes: 3, Role: "../primary", Format: "../png"}, {SizeBytes: 3, Role: "../primary", Format: "../png"}}}}
	srv, resolver := newTestServer(repo, &archiveStorageFake{}, &fakeQueue{})
	repo.byID[id].ClientID = resolver.client.ID
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/archive", strings.NewReader(`{"job_ids":["`+id.String()+`"]}`))))
	z, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 2 || z.File[0].Name == z.File[1].Name {
		t.Fatal("missing or colliding outputs")
	}
	for _, f := range z.File {
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "\\") {
			t.Fatalf("unsafe %s", f.Name)
		}
	}
}

func TestArchiveMissingObjectAndDuplicateSelection(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone}, outputs: []jobs.Output{{SizeBytes: 3}}}
	store := &archiveStorageFake{err: errors.New("missing")}
	srv, resolver := newTestServer(repo, store, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"job_ids":["` + id.String() + `"]}`, 409},
		{`{"job_ids":["` + id.String() + `","` + id.String() + `"]}`, 400},
		{`{"job_ids":["` + id.String() + `"],"unknown":true}`, 400},
	} {
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, authed(httptest.NewRequest("POST", "/v1/jobs/archive", strings.NewReader(tc.body))))
		if rec.Code != tc.code || rec.Header().Get("Content-Type") == "application/zip" {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	}
}
func TestArchiveCanceledRequestDoesNotStartZIP(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, Status: jobs.StatusDone}, outputs: []jobs.Output{{SizeBytes: 3}}}
	srv, resolver := newTestServer(repo, &archiveStorageFake{}, &fakeQueue{})
	repo.getJob.ClientID = resolver.client.ID
	request := authedArchiveRequest(resolver, id)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	rec := httptest.NewRecorder()
	srv.handleArchive(rec, request.WithContext(ctx))
	if rec.Code != 504 {
		t.Fatalf("status %d", rec.Code)
	}
}
