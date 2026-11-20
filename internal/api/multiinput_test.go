package api

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/jobs"
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
	return postPartsCatalog(t, repo, target, false, parts...)
}

func postPartsCatalog(t *testing.T, repo *fakeRepo, target string, tectonic bool, parts ...[2]string) *httptest.ResponseRecorder {
	t.Helper()
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	if tectonic {
		srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: tectonicMatch()})
	}
	body, ct := multipartParts(t, target, parts...)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestMultipleInputsAreRecordedInOrder(t *testing.T) {
	repo := &fakeRepo{}
	rec := postPartsCatalog(t, repo, "pdf", true,
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

// The four findings review made against the first version of this feature, each
// reproduced before it was fixed.

// TestAttachmentCannotTakeTheWorkersOwnName is the sharpest of them. The worker
// does not keep the document's name -- it downloads ordinal 0 to in.<source>
// and writes out.<target> beside it -- so an attachment called "in.latex"
// overwrote the document that passed every check, and the engine compiled the
// part that passed none.
func TestAttachmentCannotTakeTheWorkersOwnName(t *testing.T) {
	// The whole stem, not two names: the ENGINE writes from the same stem the
	// worker picks, so tectonic turns in.latex into in.pdf. Reserving only
	// in.<source> and out.<target> left in.pdf to be overwritten by the
	// compiled output -- measured, with the attachment replaced by a PDF.
	for _, name := range []string{"in.latex", "out.pdf", "in.pdf", "in.log", "in.aux", "out.anything"} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			rec := postParts(t, repo, "pdf",
				[2]string{"paper.tex", texMain},
				[2]string{name, "\\usepackage{fontspec}\n"},
			)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if repo.created != nil {
				t.Fatalf("a job was created with an attachment named %q: it lands on the path the "+
					"worker writes the validated document to", name)
			}
		})
	}
}

// TestPartCountIsBounded: size does not bound count. A thousand one-byte
// attachments pass every byte ceiling while costing a thousand S3 uploads,
// rows and worker downloads, all charged as one small job.
func TestPartCountIsBounded(t *testing.T) {
	parts := [][2]string{{"paper.tex", texMain}}
	for i := 0; i < jobs.MaxInputParts; i++ {
		parts = append(parts, [2]string{fmt.Sprintf("f%d.tex", i), "x"})
	}
	repo := &fakeRepo{}
	rec := postParts(t, repo, "pdf", parts...)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 for %d parts; body=%s", rec.Code, len(parts), rec.Body.String())
	}
	if repo.created != nil {
		t.Fatal("the job was created: every part would have been uploaded before anything refused it")
	}
}

// TestQuotaChargesEveryPart: with Inputs populated the legacy single-input
// field is the zero value, so charging it admitted a whole project as ZERO
// bytes past the daily byte limit.
func TestQuotaChargesEveryPart(t *testing.T) {
	repo := &fakeRepo{}
	body := strings.Repeat("x", 5000)
	rec := postPartsCatalog(t, repo, "pdf", true,
		[2]string{"paper.tex", texMain},
		[2]string{"a.tex", body},
		[2]string{"b.tex", body},
	)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.preAdmitBytes == 0 {
		t.Fatal("PreAdmit was charged nothing")
	}
	var want int64
	for _, in := range repo.created.Inputs {
		want += in.SizeBytes
	}
	if repo.preAdmitBytes != want {
		t.Fatalf("PreAdmit charged %d bytes, the job's inputs total %d: the advisory gate must charge "+
			"what the authoritative one will, or a project passes here and is refused at Create -- "+
			"after every part is already uploaded", repo.preAdmitBytes, want)
	}
}

// TestTheReservedStemDoesNotSwallowOrdinaryNames is the other half of that
// reservation, and the half a "reserve more" fix invites forgetting: refusing
// "in.pdf" must not also refuse "input.tex". Over-refusal here is the same
// defect as the collision, arriving as a client whose ordinary project stops
// being accepted.
func TestTheReservedStemDoesNotSwallowOrdinaryNames(t *testing.T) {
	for _, name := range []string{"input.tex", "into.tex", "index.tex", "instructions.bib", "outline.tex", "output-notes.tex"} {
		t.Run(name, func(t *testing.T) {
			if jobs.WorkerOwnedName(name) {
				t.Fatalf("%q was treated as a worker-owned name: the stem is what is reserved, and "+
					"refusing an ordinary file is how a bound stops being one", name)
			}
		})
	}
	for _, name := range []string{"in", "in.pdf", "in.latex", "out", "out.pdf"} {
		t.Run("reserved "+name, func(t *testing.T) {
			if !jobs.WorkerOwnedName(name) {
				t.Fatalf("%q was allowed: the worker or the engine writes it", name)
			}
		})
	}
}

// TestPathBearingAttachmentNamesAreRefusedNotRepaired is the promise this
// package makes, kept where it was previously only documented.
//
// Go's mime/multipart applies filepath.Base while parsing, so a client naming
// "chapters/preamble.tex" reached the rule as "preamble.tex" and was ACCEPTED.
// The job then failed in the worker for a reason the client could not see from
// the request: the file was materialised at the root, while their document read
// \input{chapters/preamble}.
//
// The raw name survives in the part's own Content-Disposition, so the rule can
// see what was actually sent.
func TestPathBearingAttachmentNamesAreRefusedNotRepaired(t *testing.T) {
	for _, sent := range []string{"chapters/preamble.tex", "../../etc/evil.bib", "/etc/evil.bib"} {
		t.Run(sent, func(t *testing.T) {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			// CreateFormFile escapes the name; write the part by hand so the
			// filename reaches the server exactly as a client would send it.
			main, _ := w.CreateFormFile("file", "paper.tex")
			_, _ = main.Write([]byte(texMain))
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", `form-data; name="file"; filename="`+sent+`"`)
			h.Set("Content-Type", "application/octet-stream")
			part, err := w.CreatePart(h)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = part.Write([]byte("x"))
			_ = w.WriteField("target", "pdf")
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			repo := &fakeRepo{}
			srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", &buf))
			req.Header.Set("Content-Type", w.FormDataContentType())
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %q; body=%s", rec.Code, sent, rec.Body.String())
			}
			if repo.created != nil {
				t.Fatalf("a job was created for %q: the parser repaired the name, and the client would "+
					"learn about it as a conversion failure they cannot explain", sent)
			}
		})
	}
}

// TestRawNameSurvivesHostileContentDisposition probes the fallback in
// rawPartFilename, because a fallback is where a fix goes to be bypassed: if a
// crafted header made this package's parse fail while Go's succeeded, the code
// would quietly go back to reading the REPAIRED name and the promise would be
// false again.
//
// It is not: every shape below that Go accepts as a file part also parses here.
// The RFC 5987 row is the one worth keeping — "filename*=UTF-8”chapters%2Fx.tex"
// reaches FileHeader.Filename as "x.tex", so the encoded path is invisible
// unless the raw header is read and decoded.
func TestRawNameSurvivesHostileContentDisposition(t *testing.T) {
	cases := []struct {
		label      string
		cd         string
		isFile     bool
		wantRaw    string
		wantRefuse bool
	}{
		{"plain", `form-data; name="file"; filename="chapters/x.tex"`, true, "chapters/x.tex", true},
		{"rfc5987 encoded path", `form-data; name="file"; filename*=UTF-8''chapters%2Fx.tex`, true, "chapters/x.tex", true},
		{"both forms", `form-data; name="file"; filename="x.tex"; filename*=UTF-8''chapters%2Fx.tex`, true, "chapters/x.tex", true},
		{"trailing semicolon", `form-data; name="file"; filename="chapters/x.tex";`, true, "chapters/x.tex", true},
		{"ordinary", `form-data; name="file"; filename="refs.bib"`, true, "refs.bib", false},
		// Go rejects these as file parts outright, so nothing reaches this
		// package -- recorded because "it never gets here" is a claim worth
		// holding, not assuming.
		{"duplicate filename params", `form-data; name="file"; filename="a.tex"; filename="chapters/b.tex"`, false, "", false},
		{"unquoted with a slash", `form-data; name="file"; filename=chapters/x.tex`, false, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			body := "--B\r\nContent-Disposition: " + tc.cd + "\r\n\r\nx\r\n--B--\r\n"
			form, err := multipart.NewReader(strings.NewReader(body), "B").ReadForm(1 << 20)
			if err != nil {
				if tc.isFile {
					t.Fatalf("parse: %v", err)
				}
				return
			}
			fhs := form.File["file"]
			if len(fhs) == 0 {
				if tc.isFile {
					t.Fatal("expected a file part, got none")
				}
				return
			}
			if !tc.isFile {
				t.Fatalf("Go now accepts %q as a file part; this package's rules have to cover it", tc.cd)
			}
			raw := rawPartFilename(fhs[0])
			if raw != tc.wantRaw {
				t.Fatalf("rawPartFilename = %q, want %q -- the fallback to the repaired name is where "+
					"this fix would be bypassed", raw, tc.wantRaw)
			}
			_, serr := jobs.SafeInputFilename(raw)
			if refused := serr != nil; refused != tc.wantRefuse {
				t.Fatalf("refused = %v, want %v for %q", refused, tc.wantRefuse, raw)
			}
		})
	}
}
