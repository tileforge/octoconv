package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

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
