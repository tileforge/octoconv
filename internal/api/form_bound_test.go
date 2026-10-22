package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func readBytesOf(t *testing.T, router http.Handler, method, url, contentType string, body []byte) (int64, int) {
	t.Helper()
	counted := &countingBody{r: bytes.NewReader(body)}
	req := authed(httptest.NewRequest(method, url, counted))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return counted.n.Load(), rec.Code
}

func formBoundMultipartBody(t *testing.T) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if err := mw.WriteField("x_pad", strings.Repeat("x", 1<<20)); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile(formFieldFile, "in.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(paddedMarkdown(4 << 10)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), mw.FormDataContentType()
}

func formBoundServer() *Server {
	resolver := newFakeResolver()
	return probeServer(resolver, func(cfg *Config) {
		cfg.OperatorClientIDs = map[uuid.UUID]struct{}{resolver.client.ID: {}}
		cfg.MaxUploadBytes = 64 << 20
		cfg.InspectMaxUploadBytes = 64 << 20
	})
}

func TestNoRouteBuffersNonFileFormDataPastTheBound(t *testing.T) {
	multipartBody, multipartType := formBoundMultipartBody(t)
	urlencodedBody := []byte("a=" + strings.Repeat("b", 1<<20-2))
	ops := routerOperations(t)
	keys := make([]string, 0, len(ops))
	for op := range ops {
		keys = append(keys, op)
	}
	sort.Strings(keys)

	bodies := []struct {
		name        string
		contentType string
		body        []byte
		bound       int64
	}{
		{"multipart", multipartType, multipartBody, maxFormNonFileBytes + 8192},
		{"urlencoded", "application/x-www-form-urlencoded", urlencodedBody, 8193},
	}
	for _, b := range bodies {
		t.Run(b.name, func(t *testing.T) {
			checked := 0
			for _, op := range keys {
				method, route, _ := strings.Cut(op, " ")
				srv := formBoundServer()
				var router http.Handler = srv.Routes()
				if route == "/healthz" {
					router = srv.HealthRoutes()
				}
				read, status := readBytesOf(t, router, method, concreteURL(route), b.contentType, b.body)
				t.Logf("%s %s: read %d of %d bytes, status %d", method, route, read, len(b.body), status)
				if read > b.bound {
					t.Errorf("%s %s: %d bytes of a %s body with non-file data in front were read, want at most %d", method, route, read, b.name, b.bound)
				}
				checked++
			}
			if checked != len(ops) {
				t.Errorf("checked %d operations, the routers serve %d", checked, len(ops))
			}
		})
	}

	t.Run("the check catches an ungated route", func(t *testing.T) {
		router := formBoundServer().Routes()
		router.Post("/zz-ungated-probe", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseMultipartForm(32 << 20)
			w.WriteHeader(http.StatusNoContent)
		})
		read, status := readBytesOf(t, router, http.MethodPost, "/zz-ungated-probe", multipartType, multipartBody)
		t.Logf("POST /zz-ungated-probe: read %d of %d bytes, status %d", read, len(multipartBody), status)
		if read <= maxFormNonFileBytes+8192 {
			t.Errorf("a route calling ParseMultipartForm directly read %d bytes, at or below %d: the check would not catch it", read, maxFormNonFileBytes+8192)
		}
	})
}
