package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type boundFormCase struct {
	path     string
	filename string
	target   string
	data     func() []byte
	accepted int
}

func boundFormCases() []boundFormCase {
	return []boundFormCase{
		{"/v1/jobs", "in.md", "html", func() []byte { return paddedMarkdown(4096) }, http.StatusAccepted},
		{"/v1/inspect", "in.png", "", pngBytesFixture, http.StatusOK},
	}
}

func formWithUnknownField(t *testing.T, pad int, filename, target string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormField("x_unknown")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte{'u'}, pad)); err != nil {
		t.Fatal(err)
	}
	fw, err = w.CreateFormFile(formFieldFile, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if target != "" {
		if err := w.WriteField(formFieldTarget, target); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

type boundFormResult struct {
	rec   *httptest.ResponseRecorder
	read  int64
	repo  *fakeRepo
	store *fakeStorage
}

func postBoundForm(t *testing.T, path, contentType string, body io.Reader) boundFormResult {
	t.Helper()
	repo, store := &fakeRepo{}, &fakeStorage{}
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), createIntakeConfig(64<<20))
	counted := &countingBody{r: body}
	req := authed(httptest.NewRequest(http.MethodPost, path, counted))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return boundFormResult{rec: rec, read: counted.n.Load(), repo: repo, store: store}
}

func TestIntakeRefusesNonFileFormDataOverTheBound(t *testing.T) {
	for _, c := range boundFormCases() {
		t.Run(c.path, func(t *testing.T) {
			body, ct := formWithUnknownField(t, 10<<20-64<<10, c.filename, c.target, c.data())
			res := postBoundForm(t, c.path, ct, body)
			t.Logf("%s: status %d after %d bytes of the body were read; body %s", c.path, res.rec.Code, res.read, res.rec.Body.Bytes())
			if res.rec.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("%s: status = %d, want 413", c.path, res.rec.Code)
			}
			assertErrorCode(t, res.rec.Body.Bytes(), "payload_too_large")
			if got := errorMessage(t, res.rec.Body.Bytes()); got != formFieldsTooLargeMessage {
				t.Errorf("%s: error = %q, want %q", c.path, got, formFieldsTooLargeMessage)
			}
			if res.read > maxFormNonFileBytes+64<<10 {
				t.Errorf("%s: %d bytes of the body were read, want at most %d", c.path, res.read, maxFormNonFileBytes+64<<10)
			}
			if res.store.uploaded || res.repo.preAdmitCalls != 0 {
				t.Errorf("%s: uploaded %v, PreAdmit called %d times, want neither", c.path, res.store.uploaded, res.repo.preAdmitCalls)
			}
		})
	}
}

func TestIntakeIgnoresUnknownFieldsWithinTheBound(t *testing.T) {
	for _, c := range boundFormCases() {
		t.Run(c.path, func(t *testing.T) {
			body, ct := formWithUnknownField(t, 1<<10, c.filename, c.target, c.data())
			res := postBoundForm(t, c.path, ct, body)
			if res.rec.Code != c.accepted {
				t.Errorf("%s: status = %d body %s, want %d", c.path, res.rec.Code, res.rec.Body.Bytes(), c.accepted)
			}
		})
	}
}

func TestIntakeRefusesANonMultipartBodyUnread(t *testing.T) {
	for _, c := range boundFormCases() {
		t.Run(c.path, func(t *testing.T) {
			body := io.MultiReader(strings.NewReader("a="), strings.NewReader(strings.Repeat("b", 1<<20)))
			res := postBoundForm(t, c.path, "application/x-www-form-urlencoded", body)
			t.Logf("%s: status %d after %d bytes of the body were read", c.path, res.rec.Code, res.read)
			if res.rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", c.path, res.rec.Code)
			}
			assertErrorCode(t, res.rec.Body.Bytes(), "invalid_request")
			if got := errorMessage(t, res.rec.Body.Bytes()); got != "invalid multipart form" {
				t.Errorf("%s: error = %q, want %q", c.path, got, "invalid multipart form")
			}
			if res.read != 0 {
				t.Errorf("%s: %d bytes of a non-multipart body were read, want 0", c.path, res.read)
			}
		})
	}
}
