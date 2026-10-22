package api

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	netHTTPHeaderSlack = 4096
	headerReadLimit    = apiMaxHeaderBytes + netHTTPHeaderSlack
	headerHeapPerByte  = 32
	preAuthConnBytes   = 64 << 10
	headerKeyAlphabet  = "abcdefghijklmnopqrstuvwxyz0123456789!#$%&'*+.^_`|~"
	preAuthTarget      = "/v1/jobs"
)

func preAuthRequestHeapBytes() int64 {
	return int64((headerHeapPerByte+requestLineCopies)*headerReadLimit + preAuthConnBytes)
}

func startShippedServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(handler)
	ts.Config = NewHTTPServer("", handler)
	ts.Start()
	t.Cleanup(func() { closeWithin(t, ts, 10*time.Second) })
	return ts
}

func distinctHeaderKey(i int) string {
	n := len(headerKeyAlphabet)
	length, span := 1, n
	for i >= span {
		i -= span
		length++
		span *= n
	}
	b := make([]byte, length)
	for j := length - 1; j >= 0; j-- {
		b[j] = headerKeyAlphabet[i%n]
		i /= n
	}
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

func verifierHeaderKey(i int) string {
	const up = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const lo = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := []byte{up[i%26]}
	i /= 26
	for i > 0 {
		i--
		b = append(b, lo[i%36])
		i /= 36
	}
	return string(b)
}

func headerBlock(kind string, budget int) string {
	var sb strings.Builder
	for i := 0; ; i++ {
		var line string
		switch kind {
		case "distinct-lf":
			line = distinctHeaderKey(i) + ":\n"
		case "distinct-crlf":
			line = "X" + verifierHeaderKey(i) + ":\r\n"
		case "distinct-lf-v":
			line = distinctHeaderKey(i) + ":v\n"
		case "repeated":
			line = "A:\n"
		case "pad":
			if i > 0 {
				return sb.String()
			}
			line = "X-Pad:" + strings.Repeat("p", budget-len("X-Pad:\n")) + "\n"
		default:
			return ""
		}
		if sb.Len()+len(line) > budget {
			return sb.String()
		}
		sb.WriteString(line)
	}
}

func preAuthHead(target, block string) string {
	return "POST " + target + " HTTP/1.1\r\nHost: x\r\nContent-Type: multipart/form-data; boundary=b\r\nContent-Length: 100000\r\n" + block + "\r\n"
}

func preAuthHeadOnTheLine(kind string) string {
	return preAuthHead(preAuthTarget, headerBlock(kind, headerReadLimit-len(preAuthHead(preAuthTarget, ""))-16))
}

func liveHeapAndStack() int64 {
	h := liveHeap()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(h) + int64(m.StackInuse)
}

func preAuthHeapOverTCP(t *testing.T, addr, head string, n int, settle time.Duration) (int64, []net.Conn) {
	t.Helper()
	conns := make([]net.Conn, 0, n)
	t.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close()
		}
	})
	base := liveHeapAndStack()
	for range n {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		conns = append(conns, c)
		go func() { _, _ = io.WriteString(c, head) }()
	}
	time.Sleep(settle)
	grown := liveHeapAndStack() - base
	runtime.KeepAlive(head)
	return grown, conns
}

func releasePreAuth(conns []net.Conn) {
	for _, c := range conns {
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.Copy(io.Discard, c)
		_ = c.Close()
	}
}

func headStatus(t *testing.T, addr, head string) (int, string) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer c.Close()
	go func() { _, _ = io.WriteString(c, head) }()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatalf("read the answer to a %d-byte head: %v", len(head), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func padHead(target string, total int) string {
	fixed := "GET " + target + " HTTP/1.1\r\nHost: a\r\n"
	return fixed + "X-Pad:" + strings.Repeat("p", total-len(fixed)-len("X-Pad:\r\n")-len("\r\n")) + "\r\n\r\n"
}

func TestShippedServerConfig(t *testing.T) {
	h := http.NewServeMux()
	s := NewHTTPServer(":0", h)
	if s.Addr != ":0" {
		t.Errorf("Addr %q, want %q", s.Addr, ":0")
	}
	if s.Handler != http.Handler(h) {
		t.Errorf("Handler %v, want the handler passed in", s.Handler)
	}
	if s.ReadHeaderTimeout != apiReadHeaderTimeout || apiReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout %v, want apiReadHeaderTimeout %v (10s)", s.ReadHeaderTimeout, apiReadHeaderTimeout)
	}
	if s.MaxHeaderBytes != apiMaxHeaderBytes || apiMaxHeaderBytes != 65536 {
		t.Errorf("MaxHeaderBytes %d, want apiMaxHeaderBytes %d (65536)", s.MaxHeaderBytes, apiMaxHeaderBytes)
	}
	if s.IdleTimeout != apiIdleTimeout || apiIdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout %v, want apiIdleTimeout %v (120s)", s.IdleTimeout, apiIdleTimeout)
	}
	if s.ReadTimeout != 0 || s.WriteTimeout != 0 {
		t.Errorf("ReadTimeout %v and WriteTimeout %v, want 0 and 0: a whole-request read timeout cuts an honest slow upload (260810-slf), a write timeout cuts SSE", s.ReadTimeout, s.WriteTimeout)
	}
}

func TestShippedServerAnswersHeadersAboveTheLineWith431(t *testing.T) {
	ts := startShippedServer(t, heapProbeServer().Routes())
	addr := ts.Listener.Addr().String()

	on := padHead("/healthz", headerReadLimit)
	if len(on) != headerReadLimit {
		t.Fatalf("head on the line is %d bytes, want %d", len(on), headerReadLimit)
	}
	status, body := headStatus(t, addr, on)
	t.Logf("head %d bytes (headerReadLimit): %d %q", len(on), status, body)
	if status == http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("head of exactly headerReadLimit %d bytes answered 431, want it served", headerReadLimit)
	}

	above := padHead("/healthz", headerReadLimit+1)
	status, body = headStatus(t, addr, above)
	t.Logf("head %d bytes (headerReadLimit + 1): %d %q", len(above), status, body)
	if status != http.StatusRequestHeaderFieldsTooLarge || body != "431 Request Header Fields Too Large" {
		t.Errorf("head of headerReadLimit + 1 = %d bytes answered %d %q, want 431 %q", len(above), status, body, "431 Request Header Fields Too Large")
	}
}

func TestShippedServerHoldsAtMostThePreAuthTermPerRequest(t *testing.T) {
	verifierFixed := preAuthHead(preAuthTarget, "")
	verifierFixed = verifierFixed[:len(verifierFixed)-len("\r\n")]
	heads := []struct {
		name  string
		block string
		head  string
	}{
		{name: "verifier 1 MiB distinct CRLF", block: headerBlock("distinct-crlf", http.DefaultMaxHeaderBytes-len(verifierFixed)-64)},
		{name: "distinct LF on the line", block: headerBlock("distinct-lf", headerReadLimit-len(preAuthHead(preAuthTarget, ""))-16)},
	}
	for i := range heads {
		heads[i].head = preAuthHead(preAuthTarget, heads[i].block)
	}
	bound := preAuthRequestHeapBytes()
	for _, h := range heads {
		t.Run(h.name, func(t *testing.T) {
			ts := startShippedServer(t, heapProbeServer().Routes())
			grown, conns := preAuthHeapOverTCP(t, ts.Listener.Addr().String(), h.head, 1, 2*time.Second)
			lines := strings.Count(h.block, "\n")
			t.Logf("%s: head %d bytes, %d header lines; heap+stack grown %d (%.3f MiB) = %.2f B per head byte; bound preAuthRequestHeapBytes %d (%.3f MiB) = (headerHeapPerByte %d + requestLineCopies %d) x headerReadLimit %d + preAuthConnBytes %d", h.name, len(h.head), lines, grown, mib(grown), float64(grown)/float64(len(h.head)), bound, mib(bound), headerHeapPerByte, requestLineCopies, headerReadLimit, preAuthConnBytes)
			releasePreAuth(conns)
			if grown > bound {
				t.Errorf("%s: one keyless request with a stalled body holds %d bytes (%.3f MiB) of heap and stack, above preAuthRequestHeapBytes %d (%.3f MiB)", h.name, grown, mib(grown), bound, mib(bound))
			}
		})
	}
}
