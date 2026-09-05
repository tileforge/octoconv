package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A LaTeX project is a .tex plus its .bib and its figures, and until now
// POST /v1/jobs took one file. job_inputs has carried an ordinal column since
// Phase 01 with nothing populating more than one; this is that column being
// used.
//
// The FIRST part is the document. It decides the source format, the engine and
// every check that already existed. The rest are attachments: opaque bytes with
// admitted names, which the worker materialises beside the document so
// \input{preamble} and \addbibresource{refs.bib} resolve.

func multipartParts(t *testing.T, target string, parts ...[2]string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, p := range parts {
		fw, err := w.CreateFormFile("file", p[0])
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(p[1]))
	}
	if target != "" {
		_ = w.WriteField("target", target)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

const texMain = "\\documentclass{article}\n\\input{preamble}\n\\begin{document}x\\end{document}\n"

func postParts(t *testing.T, repo *fakeRepo, target string, parts ...[2]string) *httptest.ResponseRecorder {
	t.Helper()
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	body, ct := multipartParts(t, target, parts...)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestMultipleInputsAreRecordedInOrder(t *testing.T) {
	repo := &fakeRepo{}
	rec := postParts(t, repo, "pdf",
		[2]string{"paper.tex", texMain},
		[2]string{"preamble.tex", "\\newcommand{\\x}{y}\n"},
		[2]string{"refs.bib", "@book{k, title={T}}\n"},
	)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil {
		t.Fatal("no job was created")
	}
	got := repo.created.Inputs
	if len(got) != 3 {
		t.Fatalf("inputs = %d, want 3 -- the attachments were dropped, and a client charged for bytes "+
			"the job cannot read would still have got a 202", len(got))
	}
	for i, want := range []string{"paper.tex", "preamble.tex", "refs.bib"} {
		if got[i].Ordinal != i {
			t.Fatalf("input %d has ordinal %d: order is the client's, and the document must stay at 0",
				i, got[i].Ordinal)
		}
		if got[i].Filename != want {
			t.Fatalf("input %d filename = %q, want %q -- the worker resolves these BY NAME", i, got[i].Filename, want)
		}
	}
	if got[0].Format == "" {
		t.Fatal("the document at ordinal 0 lost its detected format")
	}
	if got[1].Format != "" {
		t.Fatalf("attachment claims format %q: attachments are opaque bytes, and guessing a format for "+
			"one would be a claim this service does not need to make", got[1].Format)
	}
}

func TestSingleInputEnginesRefuseAttachments(t *testing.T) {
	repo := &fakeRepo{}
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64)
	rec := postParts(t, repo, "jpg",
		[2]string{"a.png", png},
		[2]string{"notes.txt", "hello"},
	)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Fatal("a job was created for an engine that cannot read the extra part: silently ignoring it " +
			"would charge the client for bytes and return success for work not done")
	}
}

func TestTwoAttachmentsCannotShareAName(t *testing.T) {
	repo := &fakeRepo{}
	rec := postParts(t, repo, "pdf",
		[2]string{"paper.tex", texMain},
		[2]string{"refs.bib", "a"},
		[2]string{"refs.bib", "b"},
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Fatal("both parts would land on one path in the worker, and which survived would depend on " +
			"upload order")
	}
}

func TestTheCeilingAppliesToTheTotal(t *testing.T) {
	repo := &fakeRepo{}
	// The tex ceiling is 2 MiB and is paired with the measured 271s timeout
	// (64-05): cost is linear in source size, so a project split across parts
	// costs what its bytes cost however they arrived.
	half := "%" + strings.Repeat("x", 1_400_000) + "\n"
	rec := postParts(t, repo, "pdf",
		[2]string{"paper.tex", texMain},
		[2]string{"a.tex", half},
		[2]string{"b.tex", half},
	)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}
