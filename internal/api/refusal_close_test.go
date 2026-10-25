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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/errcode"
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
