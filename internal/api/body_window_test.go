package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const bodyWindow = 400 * time.Millisecond

func bodyWindowProbe(mutate func(*Config)) *Server {
	resolver := newFakeResolver()
	srv := probeServer(resolver, func(cfg *Config) {
		cfg.OperatorClientIDs = map[uuid.UUID]struct{}{resolver.client.ID: {}}
		if mutate != nil {
			mutate(cfg)
		}
	})
	srv.intakeWindow = bodyWindow
	srv.intakeMinRate = 32 << 10
	return srv
}

func serveBodyWindow(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(h)
	ts.Config.ReadHeaderTimeout = 10 * time.Second
	ts.Start()
	t.Cleanup(func() { closeWithin(t, ts, 10*time.Second) })
	return ts
}

func bodyWindowServer(t *testing.T, mutate func(*Config), extend func(chi.Router)) (*Server, *httptest.Server) {
	t.Helper()
	srv := bodyWindowProbe(mutate)
	router := srv.Routes()
	if extend != nil {
		extend(router)
	}
	return srv, serveBodyWindow(t, router)
}

func bodyWindowHealthServer(t *testing.T, mutate func(*Config)) (*Server, *httptest.Server) {
	t.Helper()
	srv := bodyWindowProbe(mutate)
	return srv, serveBodyWindow(t, srv.HealthRoutes())
}

func keyHeader(key string) http.Header {
	return http.Header{"Authorization": {"ApiKey " + key}}
}

const bodyWindowMultipart = "multipart/form-data; boundary=bodywindow"

func spendToken(t *testing.T, ts *httptest.Server, method, path, contentType string, contentLength int64, status int) {
	t.Helper()
	res, ok := rawRequest(t, ts, method, path, keyHeader(testClientKey), contentType, contentLength, nil, 15*time.Second, false)
	if !ok || res.status != status {
		t.Fatalf("token-spending %s %s: answered=%v status %d, want %d", method, path, ok, res.status, status)
	}
}

func TestBodyWindowAnswersMiddlewareRefusals(t *testing.T) {
	chunkedStall := func() *strings.Reader {
		return strings.NewReader("400\r\n" + strings.Repeat("a", 100))
	}
	chunked := func(h http.Header) http.Header {
		h = h.Clone()
		if h == nil {
			h = http.Header{}
		}
		h.Set("Transfer-Encoding", "chunked")
		return h
	}
	for _, tc := range []struct {
		name        string
		health      bool
		mutate      func(*Config)
		prep        func(*testing.T, *httptest.Server)
		method      string
		path        string
		header      http.Header
		contentType string
		chunked     bool
		status      int
		code        string
		message     string
	}{
		{name: "create without a key", method: http.MethodPost, path: "/v1/jobs", status: http.StatusUnauthorized, code: "unauthorized"},
		{name: "create with a wrong key", method: http.MethodPost, path: "/v1/jobs", header: keyHeader("wrong"), status: http.StatusUnauthorized, code: "unauthorized"},
		{
			name:   "create past the IP rate",
			mutate: func(c *Config) { c.IPRateLimitRPM = 1 },
			prep: func(t *testing.T, ts *httptest.Server) {
				spendToken(t, ts, http.MethodGet, "/v1/formats", "", -1, http.StatusOK)
			},
			method: http.MethodPost, path: "/v1/jobs", header: keyHeader(testClientKey), status: http.StatusTooManyRequests, code: "rate_limited",
		},
		{
			name:   "create past the client rate",
			mutate: func(c *Config) { c.ClientRateLimitRPM = 1; c.IPRateLimitRPM = 100000 },
			prep: func(t *testing.T, ts *httptest.Server) {
				spendToken(t, ts, http.MethodGet, "/v1/formats", "", -1, http.StatusOK)
			},
			method: http.MethodPost, path: "/v1/jobs", header: keyHeader(testClientKey), status: http.StatusTooManyRequests, code: "rate_limited",
		},
		{
			name:   "inspect past its own rate",
			mutate: func(c *Config) { c.InspectRateLimitRPM = 1 },
			prep: func(t *testing.T, ts *httptest.Server) {
				spendToken(t, ts, http.MethodPost, "/v1/inspect", bodyWindowMultipart, 0, http.StatusBadRequest)
			},
			method: http.MethodPost, path: "/v1/inspect", header: keyHeader(testClientKey), status: http.StatusTooManyRequests, code: "rate_limited",
		},
		{
			name:   "system presets for a non-operator",
			mutate: func(c *Config) { c.OperatorClientIDs = map[uuid.UUID]struct{}{} },
			method: http.MethodPost, path: "/v1/system/presets", header: keyHeader(testClientKey), contentType: "application/json", status: http.StatusNotFound, code: "unknown_preset",
		},
		{name: "unknown route", method: http.MethodPost, path: "/v1/nope", header: keyHeader(testClientKey), status: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodPost, path: "/v1/formats", header: keyHeader(testClientKey), status: http.StatusMethodNotAllowed},
		{name: "user preset create body", method: http.MethodPost, path: "/v1/presets", header: keyHeader(testClientKey), contentType: "application/json", status: http.StatusBadRequest, code: "invalid_request", message: "invalid JSON body"},
		{name: "user preset update body", method: http.MethodPut, path: "/v1/presets/probe", header: keyHeader(testClientKey), contentType: "application/json", status: http.StatusBadRequest, code: "invalid_request", message: "invalid JSON body"},
		{name: "system preset create body", method: http.MethodPost, path: "/v1/system/presets", header: keyHeader(testClientKey), contentType: "application/json", status: http.StatusBadRequest, code: "invalid_request", message: "invalid JSON body"},
		{name: "system preset update body", method: http.MethodPut, path: "/v1/system/presets/probe", header: keyHeader(testClientKey), contentType: "application/json", status: http.StatusBadRequest, code: "invalid_request", message: "invalid JSON body"},
		{name: "health listener", health: true, method: http.MethodPost, path: "/healthz", status: http.StatusMethodNotAllowed},
		{name: "create without a key, chunked", method: http.MethodPost, path: "/v1/jobs", chunked: true, status: http.StatusUnauthorized, code: "unauthorized"},
		{
			name:   "create past the client rate, chunked",
			mutate: func(c *Config) { c.ClientRateLimitRPM = 1; c.IPRateLimitRPM = 100000 },
			prep: func(t *testing.T, ts *httptest.Server) {
				spendToken(t, ts, http.MethodGet, "/v1/formats", "", -1, http.StatusOK)
			},
			method: http.MethodPost, path: "/v1/jobs", header: keyHeader(testClientKey), chunked: true, status: http.StatusTooManyRequests, code: "rate_limited",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ts *httptest.Server
			if tc.health {
				_, ts = bodyWindowHealthServer(t, tc.mutate)
			} else {
				_, ts = bodyWindowServer(t, tc.mutate, nil)
			}
			if tc.prep != nil {
				tc.prep(t, ts)
			}
			contentType := tc.contentType
			if contentType == "" {
				contentType = bodyWindowMultipart
			}
			header, length, body := tc.header, int64(1024), strings.NewReader("")
			if tc.chunked {
				header, length, body = chunked(tc.header), -1, chunkedStall()
			}
			res, ok := rawRequest(t, ts, tc.method, tc.path, header, contentType, length, body, bodyWindow+5*time.Second, true)
			t.Logf("%s %s, stalled body, %v window: answered=%v status %d after %v, closed=%v", tc.method, tc.path, bodyWindow, ok, res.status, res.elapsed, res.closed)
			if !ok {
				t.Fatalf("%s %s with a stalled body: no answer within %v", tc.method, tc.path, bodyWindow+5*time.Second)
			}
			if res.status != tc.status {
				t.Errorf("status %d, want %d; body=%.300s", res.status, tc.status, res.raw)
			}
			if tc.code != "" && res.payload["code"] != tc.code {
				t.Errorf("code %q, want %q; body=%.300s", res.payload["code"], tc.code, res.raw)
			}
			if tc.message != "" && res.payload["error"] != tc.message {
				t.Errorf("error %q, want %q", res.payload["error"], tc.message)
			}
			if res.elapsed > bodyWindow+2*time.Second {
				t.Errorf("answered after %v, want no later than %v", res.elapsed, bodyWindow+2*time.Second)
			}
			if !res.closed {
				t.Errorf("the server kept the connection open after answering a stalled body")
			}
		})
	}
}
