package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/errcode"
)

func callbackURLOfLength(t *testing.T, n int) string {
	t.Helper()
	prefix := "https://8.8.8.8/"
	u := prefix + strings.Repeat("a", n-len(prefix))
	if err := validateCallbackURL(u); err != nil {
		t.Fatalf("validateCallbackURL refuses the %d-byte fixture: %v", n, err)
	}
	return u
}

func errorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	msg, _ := resp["error"].(string)
	return msg
}

func assertNothingStored(t *testing.T, repo *fakeRepo, store *fakeStorage) {
	t.Helper()
	if store.uploaded || store.uploadCalls != 0 {
		t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
	}
	if repo.created != nil {
		t.Errorf("repo.Create ran with %+v, want no job", repo.created)
	}
	if repo.preAdmitCalls != 0 {
		t.Errorf("repo.PreAdmit called %d times, want 0", repo.preAdmitCalls)
	}
}

func TestCreateRefusesAnOverlongCallbackURL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length int
		want   int
	}{
		{"at the bound", 2048, http.StatusAccepted},
		{"one byte over", 2049, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, store := &fakeRepo{}, &fakeStorage{}
			srv, _ := newTestServer(repo, store, &fakeQueue{})
			srv.SetCapabilityCatalog(imageCatalog())
			u := callbackURLOfLength(t, tc.length)
			body, ct := multipartBodyWithCallback(t, "in.png", "webp", pngBytesFixture(), u, "")
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()

			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%.300s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusAccepted {
				if repo.created == nil || repo.created.CallbackURL != u {
					t.Errorf("stored callback_url differs from the %d bytes sent", len(u))
				}
				return
			}
			assertErrorCode(t, rec.Body.Bytes(), errcode.PayloadTooLarge)
			if msg := errorMessage(t, rec.Body.Bytes()); msg != "callback_url field too large" {
				t.Errorf("error = %q, want %q", msg, "callback_url field too large")
			}
			assertNothingStored(t, repo, store)
		})
	}
	t.Run("query", func(t *testing.T) {
		repo, store := &fakeRepo{}, &fakeStorage{}
		srv, _ := newTestServer(repo, store, &fakeQueue{})
		srv.SetCapabilityCatalog(imageCatalog())
		body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs?"+formFieldCallbackURL+"="+url.QueryEscape(callbackURLOfLength(t, 2049)), body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%.300s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.PayloadTooLarge)
		if msg := errorMessage(t, rec.Body.Bytes()); msg != "callback_url field too large" {
			t.Errorf("error = %q, want %q", msg, "callback_url field too large")
		}
		assertNothingStored(t, repo, store)
	})
}

func multipartBodyWithRawName(t *testing.T, rawName, target string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="`+formFieldFile+`"; filename="`+rawName+`"`)
	h.Set("Content-Type", "application/octet-stream")
	fw, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField(formFieldTarget, target); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, mw.FormDataContentType()
}

func TestCreateRefusesAnOverlongInputFilename(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{"255 bytes", strings.Repeat("a", 251) + ".png", http.StatusAccepted},
		{"256 bytes", strings.Repeat("a", 252) + ".png", http.StatusUnprocessableEntity},
		{"254 bytes in 129 runes", strings.Repeat("é", 125) + ".png", http.StatusAccepted},
		{"256 bytes in 130 runes", strings.Repeat("é", 126) + ".png", http.StatusUnprocessableEntity},
		{"256 raw bytes with a 252-byte base", "dir/" + strings.Repeat("a", 248) + ".png", http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, store := &fakeRepo{}, &fakeStorage{}
			srv, _ := newTestServer(repo, store, &fakeQueue{})
			srv.SetCapabilityCatalog(imageCatalog())
			body, ct := multipartBodyWithRawName(t, tc.raw, "webp", pngBytesFixture())
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()

			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("raw name of %d bytes: status = %d, want %d; body=%.300s", len(tc.raw), rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusAccepted {
				if repo.created == nil {
					t.Error("no job created")
				}
				return
			}
			assertErrorCode(t, rec.Body.Bytes(), errcode.PayloadTooLarge)
			if msg := errorMessage(t, rec.Body.Bytes()); msg != "file name too long" {
				t.Errorf("error = %q, want %q", msg, "file name too long")
			}
			assertNothingStored(t, repo, store)
		})
	}
}

func TestInputFilenameBoundMatchesTheJobsRule(t *testing.T) {
	if got := maxInputFilenameLength(t); maxInputFilenameBytes != got {
		t.Errorf("maxInputFilenameBytes = %d, jobs.SafeInputFilename admits names up to %d bytes", maxInputFilenameBytes, got)
	}
}

func TestFieldBoundsAreStatedInTheContracts(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", specPath, err)
	}
	props := []string{"paths", "/v1/jobs", "post", "requestBody", "content", "multipart/form-data", "schema", "properties"}
	for _, c := range []struct {
		where string
		keys  []string
		want  []string
	}{
		{"callback_url", append(append([]string{}, props...), formFieldCallbackURL, "description"), []string{
			fmt.Sprintf("At most %d bytes; a longer value is refused with 422 (payload_too_large), never truncated", maxCallbackURLBytes),
		}},
		{"file", append(append([]string{}, props...), formFieldFile, "description"), []string{
			fmt.Sprintf("is at most %d bytes; a longer name is refused with 422 (payload_too_large), never truncated", maxInputFilenameBytes),
		}},
		{"the 422", []string{"paths", "/v1/jobs", "post", "responses", "422", "description"}, []string{
			fmt.Sprintf("A %s above %d bytes or a document file name above %d bytes is refused with payload_too_large before anything is stored", formFieldCallbackURL, maxCallbackURLBytes, maxInputFilenameBytes),
			fmt.Sprintf("%s and any field this endpoint does not read count toward the %d-byte bound on the form's non-file data", formFieldProgressWebhook, maxFormNonFileBytes),
		}},
		{"the 413", []string{"paths", "/v1/jobs", "post", "responses", "413", "description"}, []string{
			fmt.Sprintf("every field value and every part's headers together, above %d bytes (%s)", maxFormNonFileBytes, formFieldsTooLargeMessage),
		}},
	} {
		description := openAPIString(t, doc, c.keys...)
		for _, want := range c.want {
			if !strings.Contains(description, want) {
				t.Errorf("%s: %s of POST /v1/jobs does not state %q; it reads %q", specPath, c.where, want, description)
			}
		}
	}
	if d := openAPIString(t, doc, "paths", "/v1/jobs", "post", "responses", "422", "description"); strings.Contains(d, "multipart parser's allowance") {
		t.Errorf("%s: the 422 of POST /v1/jobs still bounds fields only by the multipart parser's allowance; it reads %q", specPath, d)
	}
	inspect413 := fmt.Sprintf("every field value and every part's headers together, above %d bytes (%s)", maxFormNonFileBytes, formFieldsTooLargeMessage)
	if d := openAPIString(t, doc, "paths", "/v1/inspect", "post", "responses", "413", "description"); !strings.Contains(d, inspect413) {
		t.Errorf("%s: the 413 of POST /v1/inspect does not state %q; it reads %q", specPath, inspect413, d)
	}

	const errorCodesPath = "../../docs/ERROR-CODES.md"
	codes, err := os.ReadFile(errorCodesPath)
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, line := range strings.Split(string(codes), "\n") {
		if strings.HasPrefix(line, "| `payload_too_large` |") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("%s has no payload_too_large row", errorCodesPath)
	}
	for _, want := range []string{
		fmt.Sprintf("`%s` above %d bytes", formFieldOpts, maxOptsBytes),
		fmt.Sprintf("`%s` above %d bytes", formFieldCallbackURL, maxCallbackURLBytes),
		fmt.Sprintf("file name above %d bytes", maxInputFilenameBytes),
		fmt.Sprintf("every field value and every part's headers together above %d bytes", maxFormNonFileBytes),
	} {
		if !strings.Contains(row, want) {
			t.Errorf("%s: the payload_too_large row does not state %q; it reads %q", errorCodesPath, want, row)
		}
	}
}
