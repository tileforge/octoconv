package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/presets"
)

const refusalBoundary = "r19"

type refusalReply struct {
	status  int
	close   bool
	payload map[string]string
	raw     []byte
	elapsed time.Duration
	err     error
}

type keptConn struct {
	c  net.Conn
	br *bufio.Reader
}

func dialKept(t *testing.T, ts *httptest.Server) *keptConn {
	t.Helper()
	c, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &keptConn{c: c, br: bufio.NewReader(c)}
}

func requestHead(method, path string, header http.Header, contentType string, length int64) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s HTTP/1.1\r\nHost: x\r\n", method, path)
	keys := make([]string, 0, len(header))
	for k := range header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range header[k] {
			fmt.Fprintf(&sb, "%s: %s\r\n", k, v)
		}
	}
	if contentType != "" {
		fmt.Fprintf(&sb, "Content-Type: %s\r\n", contentType)
	}
	if length >= 0 {
		fmt.Fprintf(&sb, "Content-Length: %d\r\n", length)
	}
	sb.WriteString("\r\n")
	return sb.String()
}

func (k *keptConn) exchange(head string, body io.Reader, wait time.Duration) refusalReply {
	start := time.Now()
	_ = k.c.SetDeadline(start.Add(wait))
	written := make(chan error, 1)
	go func() {
		if _, err := io.WriteString(k.c, head); err != nil {
			written <- err
			return
		}
		if body != nil {
			_, err := io.Copy(k.c, body)
			written <- err
			return
		}
		written <- nil
	}()
	resp, err := http.ReadResponse(k.br, nil)
	if err != nil {
		return refusalReply{elapsed: time.Since(start), err: fmt.Errorf("read the answer: %w", err)}
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	reply := refusalReply{status: resp.StatusCode, close: resp.Close, raw: raw, elapsed: time.Since(start)}
	if err != nil {
		reply.err = fmt.Errorf("read the answer body: %w", err)
		return reply
	}
	if json.Valid(raw) {
		_ = json.Unmarshal(raw, &reply.payload)
	}
	if werr := <-written; werr != nil && !reply.close {
		reply.err = fmt.Errorf("write the request on a connection kept alive: %w", werr)
	}
	return reply
}

func (k *keptConn) reusable(t *testing.T) bool {
	t.Helper()
	reply := k.exchange(requestHead(http.MethodGet, "/v1/formats", keyHeader(testClientKey), "", -1), nil, 5*time.Second)
	t.Logf("next GET /v1/formats on the same connection: status %d, close %v, err %v", reply.status, reply.close, reply.err)
	return reply.err == nil && reply.status == http.StatusOK
}

func (k *keptConn) serverClosed(wait time.Duration) (time.Duration, error) {
	start := time.Now()
	_ = k.c.SetReadDeadline(start.Add(wait))
	_, err := k.br.ReadByte()
	return time.Since(start), err
}

func rawFormPart(header, value string) string {
	return "--" + refusalBoundary + "\r\n" + header + "\r\n\r\n" + value + "\r\n"
}

func rawFieldPart(name, value string) string {
	return rawFormPart("Content-Disposition: form-data; name=\""+name+"\"", value)
}

func rawFilePart(size int) string {
	return rawFormPart("Content-Disposition: form-data; name=\""+formFieldFile+"\"; filename=\"a.png\"\r\nContent-Type: application/octet-stream", strings.Repeat("f", size))
}

const refusalCloser = "--" + refusalBoundary + "--\r\n"

func refusalMultipart() string {
	return "multipart/form-data; boundary=" + refusalBoundary
}

func refusalProbe(mutate func(*Config)) *Server {
	resolver := newFakeResolver()
	return probeServer(resolver, func(cfg *Config) {
		cfg.OperatorClientIDs = map[uuid.UUID]struct{}{resolver.client.ID: {}}
		if mutate != nil {
			mutate(cfg)
		}
	})
}

func TestRefusalBeforeTheBodyLeavesTheServerRequestAlone(t *testing.T) {
	srv := heapProbeServer()
	type seen struct {
		before, after, inner io.ReadCloser
	}
	got := make(chan seen, 1)
	var inner io.ReadCloser
	refuse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner = r.Body
		writeError(w, http.StatusUnauthorized, errcode.Unauthorized, "unauthorized")
	})
	ts := startShippedServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		before := r.Body
		srv.armBodyWindow(refuse).ServeHTTP(w, r)
		got <- seen{before: before, after: r.Body, inner: inner}
	}))
	k := dialKept(t, ts)
	reply := k.exchange(requestHead(http.MethodPost, "/v1/jobs", nil, refusalMultipart(), 1024), strings.NewReader(strings.Repeat("b", 1024)), 5*time.Second)
	if reply.err != nil || reply.status != http.StatusUnauthorized {
		t.Fatalf("status %d err %v, want 401", reply.status, reply.err)
	}
	s := <-got
	t.Logf("server request Body before %T %p, after %T %p; the handler saw %T %p", s.before, s.before, s.after, s.after, s.inner, s.inner)
	if s.after != s.before {
		t.Errorf("armBodyWindow replaced the Body of the server's request: %T %p before, %T %p after; net/http reads its own *body there for the discard, the early close and closeWriteAndWait", s.before, s.before, s.after, s.after)
	}
	if s.inner == s.before {
		t.Errorf("the handler behind armBodyWindow read the server's own Body %T %p, want the counted body on a copy of the request", s.inner, s.inner)
	}
}

func TestMidBodyRefusalsKeepTheirHeadBehaviour(t *testing.T) {
	keyed := keyHeader(testClientKey)
	t.Run("a1 callback_url above its bound after the form", func(t *testing.T) {
		ts := startShippedServer(t, refusalProbe(nil).Routes())
		form := rawFieldPart(formFieldTarget, "png") +
			rawFieldPart(formFieldCallbackURL, "https://example.com/"+strings.Repeat("c", maxCallbackURLBytes)) +
			rawFilePart(100_000) + refusalCloser + strings.Repeat("e", 1024)
		k := dialKept(t, ts)
		reply := k.exchange(requestHead(http.MethodPost, "/v1/jobs", keyed, refusalMultipart(), int64(len(form))), &pacedReader{r: strings.NewReader(form), rate: 512 << 10, chunk: 8 << 10}, 15*time.Second)
		t.Logf("a1: %d-byte form with a %d-byte callback_url, a 100000-byte file and a 1 KiB epilogue at 512 KiB/s: status %d, Connection: close %v, after %v, payload %v, err %v", len(form), len("https://example.com/")+maxCallbackURLBytes, reply.status, reply.close, reply.elapsed, reply.payload, reply.err)
		if reply.err != nil || reply.status != http.StatusUnprocessableEntity || reply.payload["code"] != "payload_too_large" || reply.payload["error"] != "callback_url field too large" {
			t.Fatalf("status %d payload %v err %v, want the whole 422 payload_too_large envelope", reply.status, reply.payload, reply.err)
		}
		if reply.close {
			t.Errorf("a1: the 422 after the form carries Connection: close; HEAD keeps the connection")
		}
		if !k.reusable(t) {
			t.Errorf("a1: the connection was not reused after the 422")
		}
	})
	t.Run("a2 non-file data above its bound in the middle of the body", func(t *testing.T) {
		ts := startShippedServer(t, refusalProbe(nil).Routes())
		form := rawFieldPart(formFieldOpts, strings.Repeat("o", maxFormNonFileBytes+4096)) + rawFilePart(100_000) + refusalCloser
		k := dialKept(t, ts)
		reply := k.exchange(requestHead(http.MethodPost, "/v1/jobs", keyed, refusalMultipart(), int64(len(form))), &pacedReader{r: strings.NewReader(form), rate: 512 << 10, chunk: 8 << 10}, 15*time.Second)
		t.Logf("a2: %d-byte form, %d bytes of opts then a 100000-byte file at 512 KiB/s: status %d, Connection: close %v, after %v, payload %v, err %v", len(form), maxFormNonFileBytes+4096, reply.status, reply.close, reply.elapsed, reply.payload, reply.err)
		if reply.err != nil || reply.status != http.StatusRequestEntityTooLarge || reply.payload["code"] != "payload_too_large" || reply.payload["error"] != formFieldsTooLargeMessage {
			t.Fatalf("status %d payload %v err %v, want the whole 413 %q envelope", reply.status, reply.payload, reply.err, formFieldsTooLargeMessage)
		}
		if reply.close {
			t.Errorf("a2: the 413 in the middle of the body carries Connection: close; HEAD keeps the connection")
		}
		if !k.reusable(t) {
			t.Errorf("a2: the connection was not reused after the 413")
		}
	})
	t.Run("b MaxBytesReader 413 with 3 MiB against a 1 MiB cap", func(t *testing.T) {
		ts := startShippedServer(t, refusalProbe(func(c *Config) { c.MaxUploadBytes = 1 << 20 }).Routes())
		form := rawFieldPart(formFieldTarget, "png") + rawFilePart(3<<20) + refusalCloser
		closes := 0
		for i := range 20 {
			k := dialKept(t, ts)
			reply := k.exchange(requestHead(http.MethodPost, "/v1/jobs", keyed, refusalMultipart(), int64(len(form))), &pacedReader{r: strings.NewReader(form), rate: 16 << 20, chunk: 64 << 10}, 15*time.Second)
			if reply.close {
				closes++
			}
			if i == 0 || reply.err != nil || reply.status != http.StatusRequestEntityTooLarge {
				t.Logf("b #%d: status %d, Connection: close %v, after %v, payload %v, err %v", i+1, reply.status, reply.close, reply.elapsed, reply.payload, reply.err)
			}
			if reply.err != nil || reply.status != http.StatusRequestEntityTooLarge || reply.payload["code"] != "payload_too_large" || reply.payload["error"] != "file exceeds size limit" {
				t.Errorf("b #%d: status %d payload %v err %v, want the whole 413 \"file exceeds size limit\" envelope", i+1, reply.status, reply.payload, reply.err)
			}
			_ = k.c.Close()
		}
		t.Logf("b: 20 x %d-byte form against MaxUploadBytes %d at 16 MiB/s: every 413 read whole; Connection: close on %d of 20", len(form), 1<<20, closes)
	})
}

func TestWindowCutAnswerReachesTheClient(t *testing.T) {
	keyed := keyHeader(testClientKey)
	for _, tc := range []struct {
		name string
		sent string
	}{
		{"c0 no body byte", ""},
		{"c1 after 10 KB of the form", rawFieldPart(formFieldTarget, "png") + "--" + refusalBoundary + "\r\nContent-Disposition: form-data; name=\"" + formFieldFile + "\"; filename=\"a.png\"\r\n\r\n" + strings.Repeat("f", 10_000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := startShippedServer(t, bodyWindowProbe(nil).Routes())
			k := dialKept(t, ts)
			reply := k.exchange(requestHead(http.MethodPost, "/v1/jobs", keyed, refusalMultipart(), 100_000), strings.NewReader(tc.sent), bodyWindow+5*time.Second)
			closedAfter, cerr := k.serverClosed(3 * time.Second)
			t.Logf("%s: %d body bytes sent of 100000, window %v: status %d, Connection: close %v, after %v, payload %v, err %v; the server closed %v after the answer (%v)", tc.name, len(tc.sent), bodyWindow, reply.status, reply.close, reply.elapsed, reply.payload, reply.err, closedAfter, cerr)
			if reply.err != nil {
				t.Fatalf("no whole answer: %v", reply.err)
			}
			requireIntakeCut(t, reply.status, reply.payload, reply.elapsed)
			if reply.elapsed > bodyWindow+2*time.Second {
				t.Errorf("answered after %v, want no later than the window + 2 s = %v", reply.elapsed, bodyWindow+2*time.Second)
			}
			if !errors.Is(cerr, io.EOF) {
				t.Errorf("after the cut the server did not close the connection: %v", cerr)
			}
		})
	}
}

type preBodyCase struct {
	name        string
	health      bool
	server      func() *Server
	mutate      func(*Config)
	prep        func(*testing.T, *Server, *httptest.Server)
	method      string
	path        string
	header      http.Header
	contentType string
	status      int
}

func preBodyRouter(srv *Server, health bool) http.Handler {
	if health {
		return srv.HealthRoutes()
	}
	r := srv.Routes()
	r.Post("/zz-refuse/{code}", func(w http.ResponseWriter, r *http.Request) {
		var code int
		_, _ = fmt.Sscan(chi.URLParam(r, "code"), &code)
		writeError(w, code, errcode.InvalidRequest, "synthetic refusal")
	})
	r.Post("/zz-panic", func(http.ResponseWriter, *http.Request) {
		panic("synthetic panic before the body")
	})
	return r
}

func walkedOperations(t *testing.T) []string {
	t.Helper()
	srv := refusalProbe(nil)
	seen := map[string]bool{}
	for _, router := range []chi.Router{srv.Routes(), srv.HealthRoutes()} {
		if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if route != "/" {
				route = strings.TrimSuffix(route, "/")
			}
			seen[method+" "+route] = true
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	ops := make([]string, 0, len(seen))
	for op := range seen {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

func spendOn(method, path, contentType string, length int64, status int) func(*testing.T, *Server, *httptest.Server) {
	return func(t *testing.T, _ *Server, ts *httptest.Server) {
		spendToken(t, ts, method, path, contentType, length, status)
	}
}

func preBodyCases(t *testing.T) []preBodyCase {
	t.Helper()
	keyed := keyHeader(testClientKey)
	var cases []preBodyCase
	middleware := 0
	for _, op := range walkedOperations(t) {
		method, route, _ := strings.Cut(op, " ")
		if !strings.HasPrefix(route, "/v1/") {
			continue
		}
		path := concreteURL(route)
		cases = append(cases,
			preBodyCase{name: op + " without a key", method: method, path: path, status: http.StatusUnauthorized},
			preBodyCase{name: op + " with a wrong key", method: method, path: path, header: keyHeader("wrong"), status: http.StatusUnauthorized},
			preBodyCase{name: op + " past the IP rate", mutate: func(c *Config) { c.IPRateLimitRPM = 1 }, prep: spendOn(http.MethodGet, "/v1/formats", "", -1, http.StatusOK), method: method, path: path, header: keyed, status: http.StatusTooManyRequests},
			preBodyCase{name: op + " past the client rate", mutate: func(c *Config) { c.ClientRateLimitRPM = 1 }, prep: spendOn(http.MethodGet, "/v1/formats", "", -1, http.StatusOK), method: method, path: path, header: keyed, status: http.StatusTooManyRequests},
		)
		middleware += 4
		if strings.HasPrefix(route, "/v1/system/") {
			cases = append(cases, preBodyCase{name: op + " for a non-operator", mutate: func(c *Config) { c.OperatorClientIDs = map[uuid.UUID]struct{}{} }, method: method, path: path, header: keyed, status: http.StatusNotFound})
			middleware++
		}
		if route == "/v1/inspect" {
			cases = append(cases, preBodyCase{name: op + " past the inspect rate", mutate: func(c *Config) { c.InspectRateLimitRPM = 1 }, prep: spendOn(http.MethodPost, "/v1/inspect", bodyWindowMultipart, 0, http.StatusBadRequest), method: method, path: path, header: keyed, status: http.StatusTooManyRequests})
			middleware++
		}
	}
	if middleware < 70 {
		t.Fatalf("%d route x middleware scenarios from chi.Walk, want at least 70", middleware)
	}
	long := strings.Repeat("n", maxPresetNameBytes+1)
	fill := func(sem func(*Server) chan struct{}) func(*testing.T, *Server, *httptest.Server) {
		return func(_ *testing.T, srv *Server, _ *httptest.Server) {
			ch := sem(srv)
			for range cap(ch) {
				ch <- struct{}{}
			}
		}
	}
	withHeader := func(k, v string) http.Header {
		h := keyed.Clone()
		h.Set(k, v)
		return h
	}
	cases = append(cases,
		preBodyCase{name: "router: unknown path outside /v1", method: http.MethodPost, path: "/nope", status: http.StatusNotFound},
		preBodyCase{name: "router: unknown path under /v1", method: http.MethodPost, path: "/v1/nope", header: keyed, status: http.StatusNotFound},
		preBodyCase{name: "router: method not allowed", method: http.MethodPut, path: "/v1/formats", header: keyed, status: http.StatusMethodNotAllowed},
		preBodyCase{name: "health router: unknown path", health: true, method: http.MethodPost, path: "/nope", status: http.StatusNotFound},
		preBodyCase{name: "health router: method not allowed", health: true, method: http.MethodPost, path: "/healthz", status: http.StatusMethodNotAllowed},
		preBodyCase{name: "auth: resolver failure", server: func() *Server { return probeServer(failingResolver{}, nil) }, method: http.MethodPost, path: "/v1/jobs", header: keyed, status: http.StatusInternalServerError},
		preBodyCase{name: "recoverer: panic before the body", method: http.MethodPost, path: "/zz-panic", status: http.StatusInternalServerError},
		preBodyCase{name: "handler create: Correlation-Id", method: http.MethodPost, path: "/v1/jobs", header: withHeader(CorrelationHeader, strings.Repeat("c", 129)), status: http.StatusUnprocessableEntity},
		preBodyCase{name: "handler create: intake_busy", prep: fill(func(s *Server) chan struct{} { return s.createIntakeSem }), method: http.MethodPost, path: "/v1/jobs", header: keyed, status: http.StatusTooManyRequests},
		preBodyCase{name: "handler create: not multipart", method: http.MethodPost, path: "/v1/jobs", header: keyed, contentType: "application/json", status: http.StatusBadRequest},
		preBodyCase{name: "handler inspect: intake busy", prep: fill(func(s *Server) chan struct{} { return s.inspectIntakeSem }), method: http.MethodPost, path: "/v1/inspect", header: keyed, status: http.StatusTooManyRequests},
		preBodyCase{name: "handler inspect: not multipart", method: http.MethodPost, path: "/v1/inspect", header: keyed, contentType: "application/json", status: http.StatusBadRequest},
		preBodyCase{name: "handler cancel: invalid job id", method: http.MethodPost, path: "/v1/jobs/x/cancel", header: keyed, status: http.StatusBadRequest},
		preBodyCase{name: "handler cancel: foreign job", method: http.MethodPost, path: "/v1/jobs/" + uuid.NewString() + "/cancel", header: keyed, status: http.StatusNotFound},
		preBodyCase{name: "handler list jobs: status filter", method: http.MethodGet, path: "/v1/jobs?status=x", header: keyed, status: http.StatusUnprocessableEntity},
		preBodyCase{name: "handler list jobs: engine filter", method: http.MethodGet, path: "/v1/jobs?engine=x", header: keyed, status: http.StatusUnprocessableEntity},
		preBodyCase{name: "handler get job: invalid id", method: http.MethodGet, path: "/v1/jobs/x", header: keyed, status: http.StatusBadRequest},
		preBodyCase{name: "handler get job: foreign job", method: http.MethodGet, path: "/v1/jobs/" + uuid.NewString(), header: keyed, status: http.StatusNotFound},
		preBodyCase{name: "handler events: invalid id", method: http.MethodGet, path: "/v1/jobs/x/events", header: keyed, status: http.StatusBadRequest},
		preBodyCase{name: "handler events: foreign job", method: http.MethodGet, path: "/v1/jobs/" + uuid.NewString() + "/events", header: keyed, status: http.StatusNotFound},
		preBodyCase{name: "handler show preset: name", method: http.MethodGet, path: "/v1/presets/" + long, header: keyed, status: http.StatusBadRequest},
		preBodyCase{name: "handler show preset: unknown", method: http.MethodGet, path: "/v1/presets/probe", header: keyed, status: http.StatusNotFound},
		preBodyCase{name: "handler update preset: name", method: http.MethodPut, path: "/v1/presets/" + long, header: keyed, contentType: "application/json", status: http.StatusBadRequest},
		preBodyCase{name: "handler deactivate preset: name", method: http.MethodDelete, path: "/v1/presets/" + long, header: keyed, status: http.StatusBadRequest},
		preBodyCase{name: "handler show system preset: name", method: http.MethodGet, path: "/v1/system/presets/" + long, header: keyed, status: http.StatusBadRequest},
		preBodyCase{name: "handler update system preset: name", method: http.MethodPut, path: "/v1/system/presets/" + long, header: keyed, contentType: "application/json", status: http.StatusBadRequest},
		preBodyCase{name: "handler deactivate system preset: name", method: http.MethodDelete, path: "/v1/system/presets/" + long, header: keyed, status: http.StatusBadRequest},
		preBodyCase{name: "handler system usage: period", method: http.MethodGet, path: "/v1/system/usage?period=x", header: keyed, status: http.StatusUnprocessableEntity},
		preBodyCase{name: "handler system usage: client_id", method: http.MethodGet, path: "/v1/system/usage?client_id=x", header: keyed, status: http.StatusBadRequest},
	)
	for _, code := range []int{400, 404, 413, 422, 429, 500, 503} {
		cases = append(cases, preBodyCase{name: fmt.Sprintf("synthetic %d without reading the body", code), method: http.MethodPost, path: fmt.Sprintf("/zz-refuse/%d", code), status: code})
	}
	return cases
}

func TestEveryPreBodyRefusalClosesTheConnection(t *testing.T) {
	cases := preBodyCases(t)
	t.Logf("pre-body refusals found by reading every handler bound to a route: create (Correlation-Id 422, intake_busy 429, not multipart 400), inspect (intake busy 429, not multipart 400), cancel (invalid id 400, foreign job 404), list jobs (status and engine filters 422), get job and events (invalid id 400, foreign job 404), show, update and deactivate preset and system preset (name 400; show also unknown 404), system usage (period 422, client_id 400); handlers that read the body first: POST /v1/presets and POST /v1/system/presets (JSON decode before any check), PUT presets after the name check, create and inspect after the checks above, GET /healthz and GET /v1/formats (no refusal)")
	var slowestAnswer, latestClose time.Duration
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			build := tc.server
			if build == nil {
				build = func() *Server { return refusalProbe(tc.mutate) }
			}
			srv := build()
			ts := startShippedServer(t, preBodyRouter(srv, tc.health))
			if tc.prep != nil {
				tc.prep(t, srv, ts)
			}
			contentType := tc.contentType
			if contentType == "" {
				contentType = bodyWindowMultipart
			}
			k := dialKept(t, ts)
			reply := k.exchange(requestHead(tc.method, tc.path, tc.header, contentType, 100_000), strings.NewReader(strings.Repeat("s", 1024)), 10*time.Second)
			closedAfter, cerr := k.serverClosed(intakeReadDeadline + 5*time.Second)
			t.Logf("%s %s, Content-Length 100000, 1 KiB sent: status %d after %v, Connection: close %v; the server closed %v after the answer (%v)", tc.method, tc.path, reply.status, reply.elapsed, reply.close, closedAfter, cerr)
			if reply.err != nil {
				t.Fatalf("no whole answer: %v", reply.err)
			}
			if reply.status != tc.status {
				t.Errorf("status %d, want %d; body=%.200s", reply.status, tc.status, reply.raw)
			}
			if reply.elapsed > time.Second || reply.elapsed >= refusalDrainGrace {
				t.Errorf("answered after %v, want before refusalDrainGrace %v and no later than 1 s: the answer waited for the body", reply.elapsed, refusalDrainGrace)
			}
			if !reply.close {
				t.Errorf("the %d refusal before the body carries no Connection: close", reply.status)
			}
			if !errors.Is(cerr, io.EOF) || closedAfter > refusalDrainGrace+time.Second {
				t.Errorf("the server closed the connection %v after the answer (%v), want EOF no later than refusalDrainGrace + 1 s = %v", closedAfter, cerr, refusalDrainGrace+time.Second)
			}
			slowestAnswer = max(slowestAnswer, reply.elapsed)
			latestClose = max(latestClose, closedAfter)
		})
	}
	t.Logf("%d pre-body refusals: slowest answer %v, latest server close %v after the answer (refusalDrainGrace %v)", len(cases), slowestAnswer, latestClose, refusalDrainGrace)
}

func TestRefusalAfterTheBodyKeepsTheConnection(t *testing.T) {
	srv := refusalProbe(nil)
	r := srv.Routes()
	r.Post("/zz-read-all", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "synthetic refusal after the body")
	})
	r.Post("/zz-read-one", func(w http.ResponseWriter, r *http.Request) {
		_, _ = r.Body.Read(make([]byte, 1))
		writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge, "synthetic refusal after one byte")
	})
	ts := startShippedServer(t, r)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/zz-read-all", http.StatusUnprocessableEntity}, {"/zz-read-one", http.StatusRequestEntityTooLarge}} {
		t.Run(strings.TrimPrefix(tc.path, "/"), func(t *testing.T) {
			k := dialKept(t, ts)
			reply := k.exchange(requestHead(http.MethodPost, tc.path, nil, "application/octet-stream", 1024), strings.NewReader(strings.Repeat("b", 1024)), 5*time.Second)
			t.Logf("POST %s with a whole 1 KiB body: status %d after %v, Connection: close %v, err %v", tc.path, reply.status, reply.elapsed, reply.close, reply.err)
			if reply.err != nil || reply.status != tc.status {
				t.Fatalf("status %d err %v, want %d", reply.status, reply.err, tc.status)
			}
			if reply.close {
				t.Errorf("a refusal after the handler read the body carries Connection: close")
			}
			if !k.reusable(t) {
				t.Errorf("the connection was not reused after a refusal written after the body was read")
			}
		})
	}
}

func TestHonestKeyedClientKeepsItsConnection(t *testing.T) {
	srv := refusalProbe(nil)
	admin := defaultFakePresetAdmin()
	admin.getForClientErr = nil
	admin.getForClientResult = &presets.Preset{Name: "probe", Version: 1, Scope: presets.ScopeUser, TargetFormat: "png", IsActive: true}
	srv.presetAdmin = admin
	var mu sync.Mutex
	opened := 0
	ts := startShippedServer(t, srv.Routes(), func(s *http.Server) {
		s.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				mu.Lock()
				opened++
				mu.Unlock()
			}
		}
	})
	client := ts.Client()
	for i, step := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/v1/presets", `{"name":"probe","target_format":"png"}`, http.StatusCreated},
		{http.MethodGet, "/v1/formats", "", http.StatusOK},
		{http.MethodPost, "/v1/presets", `{"name":"","target_format":"png"}`, http.StatusBadRequest},
		{http.MethodGet, "/v1/formats", "", http.StatusOK},
	} {
		var body io.Reader
		if step.body != "" {
			body = strings.NewReader(step.body)
		}
		req, err := http.NewRequest(step.method, ts.URL+step.path, body)
		if err != nil {
			t.Fatal(err)
		}
		if step.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(authed(req))
		if err != nil {
			t.Fatalf("step %d %s %s: %v", i+1, step.method, step.path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		t.Logf("step %d %s %s: status %d, Connection: close %v", i+1, step.method, step.path, resp.StatusCode, resp.Close)
		if resp.StatusCode != step.status {
			t.Errorf("step %d %s %s: status %d, want %d", i+1, step.method, step.path, resp.StatusCode, step.status)
		}
		if resp.Close {
			t.Errorf("step %d %s %s: the answer to an honest keyed client carries Connection: close", i+1, step.method, step.path)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("four requests of one keyed client: %d connection(s) opened", opened)
	if opened != 1 {
		t.Errorf("the honest keyed client needed %d connections for four requests, want 1", opened)
	}
}

func TestHonestRefusedClientGetsItsAnswer(t *testing.T) {
	ts := startShippedServer(t, refusalProbe(nil).Routes())
	client := ts.Client()
	for _, tc := range []struct {
		size, repeats int
	}{{100_000, 50}, {1 << 20, 20}} {
		form := rawFieldPart(formFieldTarget, "png") + rawFilePart(tc.size) + refusalCloser
		closes := 0
		var slowest time.Duration
		for i := range tc.repeats {
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/jobs", strings.NewReader(form))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", refusalMultipart())
			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("%d-byte file, request %d: no answer: %v", tc.size, i+1, err)
				continue
			}
			raw, rerr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			slowest = max(slowest, time.Since(start))
			if resp.Close {
				closes++
			}
			if rerr != nil || resp.StatusCode != http.StatusUnauthorized || !json.Valid(raw) {
				t.Errorf("%d-byte file, request %d: status %d, body %.200q, read error %v; want the whole 401 envelope", tc.size, i+1, resp.StatusCode, raw, rerr)
			}
			if !resp.Close {
				t.Errorf("%d-byte file, request %d: the keyless 401 carries no Connection: close", tc.size, i+1)
			}
		}
		t.Logf("keyless http.Client, %d-byte form with a %d-byte file, %d repeats: every 401 received whole; Connection: close on %d; slowest %v", len(form), tc.size, tc.repeats, closes, slowest)
	}
}

func TestPreBodyRefusalIsStatedInTheAPIDocs(t *testing.T) {
	raw, err := os.ReadFile("../../docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	_, section, ok := strings.Cut(text, "**Status semantics**")
	if !ok {
		t.Fatal("docs/API.md has no Status semantics section")
	}
	section, _, _ = strings.Cut(section, "\n**")
	const want = "A refusal sent before the request body is read"
	for _, phrase := range []string{want, "`Connection: close`", "reads no more of the body", "new connection"} {
		if !strings.Contains(strings.Join(strings.Fields(section), " "), phrase) {
			t.Errorf("docs/API.md Status semantics does not say %q", phrase)
		}
	}
}
