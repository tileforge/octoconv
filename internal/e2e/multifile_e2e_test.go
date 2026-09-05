package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"
)

// A LaTeX project through the whole stack: several parts in, one PDF out, with
// the parts resolved BY NAME inside the worker.
//
// This is the case phase 64's own todo recorded as impossible ("POST /v1/jobs
// takes ONE file... none of that can be sent today"), and the case every unit
// test in internal/api can only approximate: whether \input{preamble} resolves
// depends on the worker materialising the attachment beside the document, which
// no fake can prove.

// postParts sends several "file" parts in one request and returns the decoded
// create response. Deliberately not built on postJob: that helper writes a
// single part, and a multi-part sender that reuses it would be asserting about
// a request shape it does not make.
func postParts(t *testing.T, baseURL, apiKey, target string, parts [][2]string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		fw, err := w.CreateFormFile("file", p[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(p[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteField("target", target); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/jobs", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "ApiKey "+apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := e2eHTTP.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/jobs: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

const (
	e2eProjectMain = `\documentclass{article}
\input{preamble}
\usepackage[backend=bibtex]{biblatex}
\addbibresource{refs.bib}
\begin{document}
\thetitleline
Cited~\cite{k}.
\printbibliography
\end{document}
`
	e2eProjectPreamble = `\newcommand{\thetitleline}{A multi-file project.}
`
	e2eProjectRefs = `@book{k, author = {D. Knuth}, title = {The {\TeX}book}, publisher = {AW}, year = {1984}}
`
)

func TestMultiFileLaTeXProjectE2E(t *testing.T) {
	cfg := e2eSetup(t)
	apiKey := provisionClient(t)

	status, body := postParts(t, cfg.baseURL, apiKey, "pdf", [][2]string{
		{"paper.tex", e2eProjectMain},
		{"preamble.tex", e2eProjectPreamble},
		{"refs.bib", e2eProjectRefs},
	})
	if status != http.StatusAccepted {
		t.Fatalf("POST /v1/jobs status = %d, want 202; body=%v", status, body)
	}
	jobID, _ := body["job_id"].(string)
	if jobID == "" {
		t.Fatalf("no job_id in %v", body)
	}

	done := pollUntilDone(t, cfg.baseURL, apiKey, jobID, 3*time.Minute)
	downloadURL, _ := done["download_url"].(string)
	if downloadURL == "" {
		t.Fatalf("job %s done but download_url missing: %v", jobID, done)
	}
	// The PDF itself is the assertion that the parts resolved: without
	// preamble.tex the \thetitleline macro is undefined and the job fails, and
	// without refs.bib the citation cannot render.
	assertDownloadIsPDF(t, downloadURL)
}

// TestMultiFileRefusalsE2E covers the refusals through the real API, because
// each of them exists to stop something the worker would otherwise do with
// files already in S3.
func TestMultiFileRefusalsE2E(t *testing.T) {
	cfg := e2eSetup(t)
	apiKey := provisionClient(t)

	cases := []struct {
		name  string
		parts [][2]string
		want  int
		why   string
	}{
		{
			name: "an attachment named for the worker's own file",
			parts: [][2]string{
				{"paper.tex", e2eProjectMain},
				{"in.pdf", "not really a pdf"},
			},
			want: http.StatusBadRequest,
			why:  "the engine writes in.pdf itself and would overwrite it",
		},
		{
			name: "two attachments sharing a name",
			parts: [][2]string{
				{"paper.tex", e2eProjectMain},
				{"refs.bib", "a"},
				{"refs.bib", "b"},
			},
			want: http.StatusBadRequest,
			why:  "they would land on one path, with upload order deciding the winner",
		},
		{
			name: "extra parts for a conversion that takes one file",
			parts: [][2]string{
				{"a.png", "\x89PNG\r\n\x1a\n" + string(make([]byte, 64))},
				{"notes.txt", "hello"},
			},
			want: http.StatusUnprocessableEntity,
			why:  "ignoring them would charge for bytes the job cannot read and answer 202",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "pdf"
			if tc.parts[0][0] == "a.png" {
				target = "jpg"
			}
			status, body := postParts(t, cfg.baseURL, apiKey, target, tc.parts)
			if status != tc.want {
				t.Fatalf("status = %d, want %d (%s); body=%v", status, tc.want, tc.why, body)
			}
		})
	}
}
