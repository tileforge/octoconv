package api

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
)

const (
	netHTTPHeaderSlack       = 4096
	headerReadLimit          = apiMaxHeaderBytes + netHTTPHeaderSlack
	headerHeapPerByte        = 32
	preAuthConnBytes         = 64 << 10
	headerKeyAlphabet        = "abcdefghijklmnopqrstuvwxyz0123456789!#$%&'*+.^_`|~"
	preAuthTarget            = "/v1/jobs"
	headerPhaseFloor         = 512 << 10
	netHTTPRSTAvoidanceDelay = 500 * time.Millisecond
)

func preAuthRequestHeapBytes() int64 {
	return int64((headerHeapPerByte+requestLineCopies)*headerReadLimit + preAuthConnBytes)
}

func startShippedServer(t *testing.T, handler http.Handler, scale ...func(*http.Server)) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(handler)
	ts.Config = NewHTTPServer("", handler)
	for _, f := range scale {
		f(ts.Config)
	}
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

func headerPhase(head string) string {
	return strings.TrimSuffix(head, "\r\n")
}

func settledHeapAndStack() int64 {
	runtime.GC()
	return liveHeapAndStack()
}

func preAuthHeapOverTCP(t *testing.T, addr, head string, n int, settle time.Duration) (int64, []net.Conn) {
	t.Helper()
	conns := make([]net.Conn, 0, n)
	t.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close()
		}
	})
	phase := headerPhase(head)
	base := liveHeapAndStack()
	for range n {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		conns = append(conns, c)
		go func() { _, _ = io.WriteString(c, phase) }()
	}
	time.Sleep(settle)
	grown := liveHeapAndStack() - base
	runtime.KeepAlive(phase)
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

func padHead(method, target string, total int) string {
	fixed := method + " " + target + " HTTP/1.1\r\nHost: a\r\n"
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

	on := padHead(http.MethodGet, "/healthz", headerReadLimit)
	if len(on) != headerReadLimit {
		t.Fatalf("head on the line is %d bytes, want %d", len(on), headerReadLimit)
	}
	status, body := headStatus(t, addr, on)
	t.Logf("head %d bytes (headerReadLimit): %d %q", len(on), status, body)
	if status == http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("head of exactly headerReadLimit %d bytes answered 431, want it served", headerReadLimit)
	}

	above := padHead(http.MethodGet, "/healthz", headerReadLimit+1)
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
			if strings.HasPrefix(h.name, "distinct LF") && grown < headerPhaseFloor {
				t.Errorf("%s: the head in the header phase holds %d bytes, below %d: the measurement did not reach the parsed headers", h.name, grown, headerPhaseFloor)
			}
			if grown > bound {
				t.Errorf("%s: one keyless request with a stalled body holds %d bytes (%.3f MiB) of heap and stack, above preAuthRequestHeapBytes %d (%.3f MiB)", h.name, grown, mib(grown), bound, mib(bound))
			}
		})
	}
}

func queryHeadOnTheLine(unit string) string {
	prefix := preAuthTarget + "?q="
	k := (headerReadLimit - len(preAuthHead(prefix, "")) - 16) / len(unit)
	return preAuthHead(prefix+strings.Repeat(unit, k), "")
}

func TestShippedServerHeaderTermPerShape(t *testing.T) {
	ts := startShippedServer(t, heapProbeServer().Routes())
	addr := ts.Listener.Addr().String()
	measure := func(t *testing.T, head string) (int64, int64) {
		t.Helper()
		var lo, hi int64
		for run := range 3 {
			grown, conns := preAuthHeapOverTCP(t, addr, head, 1, time.Second)
			releasePreAuth(conns)
			if run == 0 || grown < lo {
				lo = grown
			}
			if run == 0 || grown > hi {
				hi = grown
			}
		}
		return lo, hi
	}
	baseHead := preAuthHead(preAuthTarget, "")
	baseLo, baseHi := measure(t, baseHead)
	t.Logf("base, no extra headers: head %d bytes; heap+stack grown min %d max %d", len(baseHead), baseLo, baseHi)
	bound := int64((headerHeapPerByte + requestLineCopies) * headerReadLimit)
	shapes := []struct {
		name string
		head string
	}{
		{"distinct-lf", preAuthHeadOnTheLine("distinct-lf")},
		{"distinct-crlf", preAuthHeadOnTheLine("distinct-crlf")},
		{"distinct-lf-v", preAuthHeadOnTheLine("distinct-lf-v")},
		{"repeated", preAuthHeadOnTheLine("repeated")},
		{"pad", preAuthHeadOnTheLine("pad")},
		{"query-%41", queryHeadOnTheLine("%41")},
		{"query-plus", queryHeadOnTheLine("+")},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			if len(sh.head) > headerReadLimit || len(sh.head) < headerReadLimit-64 {
				t.Fatalf("%s: head %d bytes, want within [headerReadLimit - 64, headerReadLimit] = [%d, %d]", sh.name, len(sh.head), headerReadLimit-64, headerReadLimit)
			}
			lo, hi := measure(t, sh.head)
			grown := hi - baseLo
			t.Logf("%s: head %d bytes, %d lines; heap+stack grown min %d max %d; term max(form) - min(base) = %d (%.3f MiB) = %.2f B per head byte; bound (headerHeapPerByte %d + requestLineCopies %d) x headerReadLimit %d = %d (%.3f MiB)", sh.name, len(sh.head), strings.Count(sh.head, "\n"), lo, hi, grown, mib(grown), float64(grown)/float64(len(sh.head)), headerHeapPerByte, requestLineCopies, headerReadLimit, bound, mib(bound))
			if sh.name == "distinct-lf" && grown < headerPhaseFloor {
				t.Errorf("%s: the head in the header phase holds %d bytes, below %d: the measurement did not reach the parsed headers", sh.name, grown, headerPhaseFloor)
			}
			if grown > bound {
				t.Errorf("%s: a head on the line holds %d bytes (%.3f MiB) before auth, above (headerHeapPerByte + requestLineCopies) x headerReadLimit = %d (%.3f MiB)", sh.name, grown, mib(grown), bound, mib(bound))
			}
		})
	}
}

func TestPreAuthConnectionTermIsMeasured(t *testing.T) {
	const n = 80
	ts := startShippedServer(t, heapProbeServer().Routes())
	head := preAuthHead(preAuthTarget, "")
	grown, conns := preAuthHeapOverTCP(t, ts.Listener.Addr().String(), head, n, 2*time.Second)
	releasePreAuth(conns)
	per := grown / n
	t.Logf("%d keyless stalled requests without extra headers, head %d bytes: heap+stack grown %d (%.3f MiB) = %d bytes per request; preAuthConnBytes %d", n, len(head), grown, mib(grown), per, preAuthConnBytes)
	if per > preAuthConnBytes {
		t.Errorf("one keyless request without extra headers holds %d bytes of heap and stack, above preAuthConnBytes %d", per, preAuthConnBytes)
	}
}

type preAuthOutcome struct {
	headSent time.Time
	answered time.Time
	closed   time.Time
	status   int
	close    bool
	writeErr error
	err      error
}

type preAuthBurst struct {
	outcomes []preAuthOutcome
	lastHead time.Time
	tail     int64
	peak     int64
	samples  int
}

func preAuthRefusals(t *testing.T, addr, head string, n int, interval, spread time.Duration, pieces int, sample, tail time.Duration) preAuthBurst {
	t.Helper()
	burst := preAuthBurst{outcomes: make([]preAuthOutcome, n)}
	conns := make([]net.Conn, 0, n)
	t.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close()
		}
	})
	var written, read sync.WaitGroup
	stop := make(chan struct{})
	sampled := make(chan struct{})
	base := settledHeapAndStack()
	go func() {
		defer close(sampled)
		if sample <= 0 {
			return
		}
		tick := time.NewTicker(sample)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				burst.peak = max(burst.peak, settledHeapAndStack()-base)
				burst.samples++
			}
		}
	}()
	first := time.Now()
	for i := range n {
		time.Sleep(time.Until(first.Add(time.Duration(i) * interval)))
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		conns = append(conns, c)
		o := &burst.outcomes[i]
		written.Add(1)
		read.Add(1)
		go func() {
			defer written.Done()
			step := len(head) / pieces
			for p := range pieces {
				lo, hi := p*step, (p+1)*step
				if p == pieces-1 {
					hi = len(head)
				}
				if p > 0 {
					time.Sleep(spread / time.Duration(pieces-1))
				}
				if _, err := io.WriteString(c, head[lo:hi]); err != nil {
					o.writeErr = fmt.Errorf("write the head: %w", err)
					break
				}
			}
			o.headSent = time.Now()
		}()
		go func() {
			defer read.Done()
			_ = c.SetReadDeadline(time.Now().Add(spread + intakeReadDeadline + 10*time.Second))
			br := bufio.NewReader(c)
			resp, err := http.ReadResponse(br, nil)
			o.answered = time.Now()
			if err != nil {
				o.err = fmt.Errorf("read the answer: %w", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			o.status, o.close = resp.StatusCode, resp.Close
			_, err = br.ReadByte()
			o.closed = time.Now()
			if !errors.Is(err, io.EOF) {
				o.err = fmt.Errorf("after the answer the read returned %v, want EOF from the server", err)
			}
		}()
	}
	written.Wait()
	for i := range burst.outcomes {
		if sent := burst.outcomes[i].headSent; sent.After(burst.lastHead) {
			burst.lastHead = sent
		}
	}
	time.Sleep(time.Until(burst.lastHead.Add(tail)))
	burst.tail = settledHeapAndStack() - base
	close(stop)
	<-sampled
	runtime.KeepAlive(head)
	read.Wait()
	return burst
}

func (b preAuthBurst) check(t *testing.T, name string) (map[int]int, time.Duration, time.Duration) {
	t.Helper()
	statuses := map[int]int{}
	var answer, closed time.Duration
	for i, o := range b.outcomes {
		if err := errors.Join(o.writeErr, o.err); err != nil {
			t.Errorf("%s request %d: %v", name, i, err)
			continue
		}
		statuses[o.status]++
		answer = max(answer, o.answered.Sub(o.headSent))
		closed = max(closed, o.closed.Sub(o.answered))
		if d := o.answered.Sub(o.headSent); d > time.Second || d >= refusalDrainGrace {
			t.Errorf("%s request %d: answered %v after its head was sent, want before refusalDrainGrace %v and no later than 1 s: the answer waited for the body", name, i, d, refusalDrainGrace)
		}
		if !o.close {
			t.Errorf("%s request %d: the %d refusal before the body carries no Connection: close", name, i, o.status)
		}
		if d := o.closed.Sub(o.answered); d > refusalDrainGrace+time.Second {
			t.Errorf("%s request %d: the server closed the connection %v after the answer, want no later than refusalDrainGrace + 1 s = %v", name, i, d, refusalDrainGrace+time.Second)
		}
	}
	return statuses, answer, closed
}

func TestMassPreAuthRequestsStayWithinTheirTerm(t *testing.T) {
	const n = 80
	ts := startShippedServer(t, heapProbeServer().Routes())
	head := preAuthHeadOnTheLine("distinct-lf")
	burst := preAuthRefusals(t, ts.Listener.Addr().String(), head, n, 0, 0, 1, 0, 2*time.Second)
	statuses, answer, closed := burst.check(t, "mass")
	bound := preAuthRequestHeapBytes()
	t.Logf("MASS %d keyless POST %s from one IP, Content-Length 100000, no body bytes, distinct-lf head %d bytes on the line %d: heap+stack 2 s after the last head grown %d (%.3f MiB); bound preAuthRequestHeapBytes %d (%.3f MiB), less than one held request; statuses %v; slowest answer %v after its head (limit 1 s); latest server close %v after the answer (limit refusalDrainGrace %v + 1 s)", n, preAuthTarget, len(head), headerReadLimit, burst.tail, mib(burst.tail), bound, mib(bound), statuses, answer.Round(time.Millisecond), closed.Round(time.Millisecond), refusalDrainGrace)
	if burst.tail > bound {
		t.Errorf("%d keyless refused requests still hold %d bytes (%.1f MiB) of heap and stack 2 s after the last head, above preAuthRequestHeapBytes = %d (%.3f MiB): the refusals were not released", n, burst.tail, mib(burst.tail), bound, mib(bound))
	}
	if statuses[http.StatusUnauthorized] != 60 || statuses[http.StatusTooManyRequests] != 20 || len(statuses) != 2 {
		t.Errorf("statuses %v, want 60 x 401 and 20 x 429", statuses)
	}
}

func TestPreAuthHeapFollowsTheHeaderIntakeRate(t *testing.T) {
	if refusalDrainGrace != netHTTPRSTAvoidanceDelay {
		t.Errorf("refusalDrainGrace %v, want net/http's rstAvoidanceDelay %v: the bound below holds a refused request for the time net/http gives a client to take its last answer", refusalDrainGrace, netHTTPRSTAvoidanceDelay)
	}
	const answer = time.Second
	per := preAuthRequestHeapBytes()
	head := preAuthHeadOnTheLine("distinct-lf")
	for _, form := range []struct {
		name     string
		n        int
		interval time.Duration
		spread   time.Duration
		pieces   int
	}{
		{"b 200 fast heads at 50 per second", 200, 20 * time.Millisecond, 0, 1},
		{"c 40 heads at 20 per second, each sent over 2 s", 40, 50 * time.Millisecond, 2 * time.Second, 21},
	} {
		t.Run(form.name, func(t *testing.T) {
			ts := startShippedServer(t, heapProbeServer().Routes())
			burst := preAuthRefusals(t, ts.Listener.Addr().String(), head, form.n, form.interval, form.spread, form.pieces, 100*time.Millisecond, 2*time.Second)
			statuses, slowest, latest := burst.check(t, form.name)
			rate := float64(time.Second) / float64(form.interval)
			life := form.spread + answer + refusalDrainGrace
			bound := int64(rate * life.Seconds() * float64(per))
			held := int64(rate * intakeReadDeadline.Seconds() * float64(per))
			t.Logf("%s: H <= lambda_hdr x (T_hdr + t_answer + refusalDrainGrace) x preAuthRequestHeapBytes = %.0f/s x (%v + %v + %v) x %d = %d B (%.1f MiB); measured peak of %d samples every 100 ms %d B (%.1f MiB), 2 s after the last head %d B (%.3f MiB); statuses %v; slowest answer %v after its head, latest server close %v after the answer; before D-28 a refusal was held for the body window: lambda x intakeReadDeadline x preAuthRequestHeapBytes = %.0f/s x %v x %d = %d B (%.1f MiB)", form.name, rate, form.spread, answer, refusalDrainGrace, per, bound, mib(bound), burst.samples, burst.peak, mib(burst.peak), burst.tail, mib(burst.tail), statuses, slowest.Round(time.Millisecond), latest.Round(time.Millisecond), rate, intakeReadDeadline, per, held, mib(held))
			if burst.samples < int(form.interval*time.Duration(form.n)/(100*time.Millisecond)) {
				t.Errorf("%d heap samples, want one every 100 ms over the burst", burst.samples)
			}
			if burst.peak > bound {
				t.Errorf("%s: the pre-auth heap peaked at %d B (%.1f MiB), above the header-intake bound %d B (%.1f MiB)", form.name, burst.peak, mib(burst.peak), bound, mib(bound))
			}
			for st := range statuses {
				if st != http.StatusUnauthorized && st != http.StatusTooManyRequests {
					t.Errorf("status %d among the keyless refusals %v, want only 401 and 429", st, statuses)
				}
			}
		})
	}
}

func TestShippedServerClosesAnIdleKeepAliveConnection(t *testing.T) {
	const idle = time.Second
	ts := startShippedServer(t, heapProbeServer().Routes(), func(s *http.Server) { s.IdleTimeout = idle })
	c, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	br := bufio.NewReader(c)
	start := time.Now()
	for i := range 2 {
		if _, err := io.WriteString(c, "GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
			t.Fatalf("request %d on the kept-alive connection: %v", i+1, err)
		}
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatalf("answer %d on the kept-alive connection: %v", i+1, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Close {
			t.Fatalf("answer %d: status %d, close %v; want 200 on a kept-alive connection", i+1, resp.StatusCode, resp.Close)
		}
	}
	answered := time.Now()
	if d := answered.Sub(start); d > 500*time.Millisecond {
		t.Errorf("two requests on one connection took %v, want within 500ms", d)
	}
	_ = c.SetReadDeadline(time.Now().Add(idle + 5*time.Second))
	_, err = br.ReadByte()
	closed := time.Since(answered)
	t.Logf("IdleTimeout %v: two GET /healthz on one connection in %v; the idle connection was closed %v after the second answer (%v)", idle, answered.Sub(start), closed, err)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("read on the idle connection returned %v after %v, want EOF from the server", err, closed)
	}
	if closed < idle-100*time.Millisecond || closed > idle+2*time.Second {
		t.Errorf("the idle connection was closed %v after the second answer, want within [%v, %v]", closed, idle-100*time.Millisecond, idle+2*time.Second)
	}
}

func TestIdleTimeoutDoesNotCutAnActiveUpload(t *testing.T) {
	const idle = time.Second
	const rate = 2 << 20
	ts := startShippedServer(t, heapProbeServer().Routes(), func(s *http.Server) { s.IdleTimeout = idle })
	triangles := int64(12<<20-84) / 50
	header := make([]byte, 84)
	binary.LittleEndian.PutUint32(header[80:], uint32(triangles))
	size := 84 + 50*triangles
	file := io.MultiReader(bytes.NewReader(header), io.LimitReader(transportCapZeros{}, 50*triangles))
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		pw.CloseWithError(writeTransportCapRequest(mw, convert.EngineCAD, convert.OperationRender, "png", "a.stl", file, 0))
	}()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/jobs", &pacedReader{r: pr, rate: rate, chunk: 256 << 10})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	start := time.Now()
	resp, err := ts.Client().Do(authed(req))
	elapsed := time.Since(start)
	_ = pr.Close()
	if err != nil {
		t.Fatalf("create over a paced connection failed after %v: %v", elapsed, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	t.Logf("IdleTimeout %v: %d-byte STL at %d B/s took %v; status %d", idle, size, rate, elapsed, resp.StatusCode)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create over a paced connection: status %d after %v, want 202; body=%.300s", resp.StatusCode, elapsed, raw)
	}
	if elapsed < 5*idle {
		t.Errorf("the upload took %v, want at least 5 x IdleTimeout = %v", elapsed, 5*idle)
	}
}

func TestIntakeTestServersUseTheShippedConfig(t *testing.T) {
	_, intake := newIntakeServer(t, Config{MaxUploadBytes: 1 << 20, InspectMaxUploadBytes: 1 << 20}, intakeReadDeadline, intakeMinBytesPerSecond)
	window := serveBodyWindow(t, http.NewServeMux())
	for _, c := range []struct {
		name string
		ts   *httptest.Server
	}{{"newIntakeServer", intake}, {"serveBodyWindow", window}} {
		cfg := c.ts.Config
		if cfg.MaxHeaderBytes != apiMaxHeaderBytes || cfg.IdleTimeout != apiIdleTimeout || cfg.ReadHeaderTimeout != apiReadHeaderTimeout {
			t.Errorf("%s: MaxHeaderBytes %d, IdleTimeout %v, ReadHeaderTimeout %v; want the shipped %d, %v, %v", c.name, cfg.MaxHeaderBytes, cfg.IdleTimeout, cfg.ReadHeaderTimeout, apiMaxHeaderBytes, apiIdleTimeout, apiReadHeaderTimeout)
		}
	}
}

var routeParamPattern = regexp.MustCompile(`\{[^}]+\}`)

func TestHeaderLimitAnswerIsDocumentedAndServed(t *testing.T) {
	handlers := routerHandlers(t)
	documented := documentedStatuses(t)
	ops := make([]string, 0, len(handlers))
	for op := range handlers {
		ops = append(ops, op)
	}
	sort.Strings(ops)

	srv := heapProbeServer()
	apiAddr := startShippedServer(t, srv.Routes()).Listener.Addr().String()
	healthAddr := startShippedServer(t, srv.HealthRoutes()).Listener.Addr().String()
	for _, op := range ops {
		if !documented[op][http.StatusRequestHeaderFieldsTooLarge] {
			t.Errorf("%s: %s does not document 431", specPath, op)
		}
		method, route, _ := strings.Cut(op, " ")
		target := routeParamPattern.ReplaceAllString(route, "x")
		addrs := []string{apiAddr}
		if route == "/healthz" {
			addrs = append(addrs, healthAddr)
		}
		for _, addr := range addrs {
			status, body := headStatus(t, addr, padHead(method, target, headerReadLimit+1))
			if status != http.StatusRequestHeaderFieldsTooLarge {
				t.Errorf("%s %s with a head of headerReadLimit + 1 = %d bytes on %s: %d %.120q, want 431", method, target, headerReadLimit+1, addr, status, body)
			}
		}
	}
	t.Logf("%d operations document 431 and answer it above the line of %d bytes", len(ops), headerReadLimit)

	limit := fmt.Sprintf("%d KiB", apiMaxHeaderBytes>>10)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", specPath, err)
	}
	texts := map[string]string{specPath + " components.responses.RequestHeaderFieldsTooLarge": openAPIString(t, doc, "components", "responses", "RequestHeaderFieldsTooLarge", "description")}
	for _, path := range []string{"../../docs/ERROR-CODES.md", "../../README.md"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		texts[path] = string(b)
	}
	for where, text := range texts {
		if !strings.Contains(text, limit) || !strings.Contains(text, "431") {
			t.Errorf("%s does not name 431 and the header limit %q", where, limit)
		}
	}
}
