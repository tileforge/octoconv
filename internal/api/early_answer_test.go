package api

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func headOnTheLine(prefix string) string {
	return prefix + headerBlock("distinct-lf", headerReadLimit-len(prefix+"\r\n")-16) + "\r\n"
}

func listenerRouter(srv *Server, listener string) http.Handler {
	if listener == "HealthRoutes" {
		return srv.HealthRoutes()
	}
	return srv.Routes()
}

type earlyReply struct {
	informational []int
	status        int
	close         bool
	body          []byte
	elapsed       time.Duration
	err           error
}

func (k *keptConn) earlyExchange(head string, pieces []string, gap, wait time.Duration) earlyReply {
	start := time.Now()
	_ = k.c.SetDeadline(start.Add(wait))
	written := make(chan error, 1)
	go func() {
		if _, err := io.WriteString(k.c, head); err != nil {
			written <- err
			return
		}
		for _, p := range pieces {
			time.Sleep(gap)
			if _, err := io.WriteString(k.c, p); err != nil {
				written <- err
				return
			}
		}
		written <- nil
	}()
	var reply earlyReply
	for {
		resp, err := http.ReadResponse(k.br, nil)
		if err != nil {
			reply.elapsed = time.Since(start)
			reply.err = fmt.Errorf("read the answer: %w", err)
			return reply
		}
		if resp.StatusCode < http.StatusOK && resp.StatusCode != http.StatusSwitchingProtocols {
			reply.informational = append(reply.informational, resp.StatusCode)
			_ = resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		reply.status, reply.close, reply.body, reply.elapsed = resp.StatusCode, resp.Close, body, time.Since(start)
		if err != nil {
			reply.err = fmt.Errorf("read the answer body: %w", err)
			return reply
		}
		break
	}
	if werr := <-written; werr != nil && !reply.close {
		reply.err = fmt.Errorf("write the request on a connection kept alive: %w", werr)
	}
	return reply
}

func TestHealthzMassWithAStalledBodyIsReleasedAtTheGrace(t *testing.T) {
	const n = 80
	head := headOnTheLine("GET /healthz HTTP/1.1\r\nHost: x\r\nContent-Length: 100000\r\n")
	limit := refusalDrainGrace + time.Second
	for _, listener := range []string{"Routes", "HealthRoutes"} {
		t.Run(listener, func(t *testing.T) {
			ts := startShippedServer(t, listenerRouter(heapProbeServer(), listener))
			burst := preAuthRefusals(t, ts.Listener.Addr().String(), head, n, 0, 0, 1, 0, 2*time.Second)
			statuses := map[int]int{}
			var slowest, latest time.Duration
			for i, o := range burst.outcomes {
				if err := errors.Join(o.writeErr, o.err); err != nil {
					t.Errorf("request %d: %v", i, err)
					continue
				}
				statuses[o.status]++
				answer, closed := o.answered.Sub(o.headSent), o.closed.Sub(o.answered)
				slowest, latest = max(slowest, answer), max(latest, closed)
				if o.status != http.StatusOK {
					t.Errorf("request %d: status %d, want 200", i, o.status)
				}
				if answer > limit {
					t.Errorf("request %d: answered %v after its head, want no later than refusalDrainGrace + 1 s = %v: the answer waited for a body GET /healthz never reads", i, answer, limit)
				}
				if closed > limit {
					t.Errorf("request %d: the server closed the connection %v after the answer, want no later than refusalDrainGrace + 1 s = %v", i, closed, limit)
				}
			}
			bound := preAuthRequestHeapBytes()
			t.Logf("HEALTHZ | %s | n %d | tail %d B (%.2f MiB) | bound %d B | statuses %v | slowest answer %v | latest close %v | intakeReadDeadline %v", listener, n, burst.tail, mib(burst.tail), bound, statuses, slowest.Round(time.Millisecond), latest.Round(time.Millisecond), intakeReadDeadline)
			if burst.tail > bound {
				t.Errorf("%d GET /healthz with a stalled body still hold %d bytes (%.1f MiB) of heap and stack 2 s after the last head, above preAuthRequestHeapBytes = %d (%.3f MiB)", n, burst.tail, mib(burst.tail), bound, mib(bound))
			}
		})
	}
}

type streamProbe struct {
	alive  atomic.Int64
	reason atomic.Value
}

func (p *streamProbe) stream(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	_ = rc.Flush()
	for i := range 20 {
		_, _ = io.WriteString(w, "x")
		_ = rc.Flush()
		if err := r.Context().Err(); err != nil {
			p.reason.Store(fmt.Sprintf("context cancelled at tick %d: %v", i, err))
			return
		}
		p.alive.Add(1)
		time.Sleep(100 * time.Millisecond)
	}
}

func countNewConns(news *atomic.Int64) func(*http.Server) {
	return func(s *http.Server) {
		prev := s.ConnState
		s.ConnState = func(c net.Conn, st http.ConnState) {
			if st == http.StateNew {
				news.Add(1)
			}
			if prev != nil {
				prev(c, st)
			}
		}
	}
}

func TestHonestBodyKeepsItsConnectionAfterAnEarlyAnswer(t *testing.T) {
	body := strings.Repeat("b", 1024)
	whole := []string{body}
	t.Run("Routes", func(t *testing.T) {
		srv := heapProbeServer()
		r := srv.Routes()
		early, after := &streamProbe{}, &streamProbe{}
		var readLen atomic.Int64
		var readErr atomic.Value
		r.Post("/zz-early-stream", early.stream)
		r.Post("/zz-early-then-read", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			b, err := io.ReadAll(r.Body)
			readLen.Store(int64(len(b)))
			readErr.Store(fmt.Sprint(err))
			w.WriteHeader(http.StatusOK)
		})
		r.Post("/zz-read-then-stream", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			after.stream(w, r)
		})
		var news atomic.Int64
		ts := startShippedServer(t, r, countNewConns(&news))
		k := dialKept(t, ts)
		var quarters, sixteenths []string
		for i := range 4 {
			quarters = append(quarters, body[i*256:(i+1)*256])
		}
		long := strings.Repeat("l", 4096)
		for i := range 16 {
			sixteenths = append(sixteenths, long[i*256:(i+1)*256])
		}
		check := func(step string, reply earlyReply, status int) {
			t.Helper()
			t.Logf("%s: informational %v, status %d, Connection: close %v, body %d B, after %v, err %v", step, reply.informational, reply.status, reply.close, len(reply.body), reply.elapsed.Round(time.Millisecond), reply.err)
			if reply.err != nil {
				t.Fatalf("%s: %v", step, reply.err)
			}
			if reply.status != status {
				t.Errorf("%s: status %d, want %d", step, reply.status, status)
			}
			if reply.close {
				t.Errorf("%s: the answer carries Connection: close: an honest body that arrived in time lost its connection", step)
			}
		}
		for i := range 3 {
			check(fmt.Sprintf("GET /healthz, 1 KiB body whole #%d", i), k.earlyExchange(requestHead(http.MethodGet, "/healthz", nil, "", 1024), whole, 0, 10*time.Second), http.StatusOK)
		}
		check("GET /healthz, 1 KiB body in 4 pieces 80 ms apart", k.earlyExchange(requestHead(http.MethodGet, "/healthz", nil, "", 1024), quarters, 80*time.Millisecond, 10*time.Second), http.StatusOK)
		check("GET /v1/formats with a key, 1 KiB body whole", k.earlyExchange(requestHead(http.MethodGet, "/v1/formats", keyHeader(testClientKey), "", 1024), whole, 0, 10*time.Second), http.StatusOK)
		reply := k.earlyExchange(requestHead(http.MethodPost, "/zz-early-stream", nil, "", 1024), whole, 0, 10*time.Second)
		check("POST /zz-early-stream, 1 KiB body whole", reply, http.StatusOK)
		if len(reply.body) != 20 || early.alive.Load() != 20 {
			t.Errorf("the stream answered before the body sent %d B with the request context alive for %d ticks (%v), want 20 B and 20 ticks", len(reply.body), early.alive.Load(), early.reason.Load())
		}
		reply = k.earlyExchange(requestHead(http.MethodPost, "/zz-early-then-read", nil, "", 4096), sixteenths, 100*time.Millisecond, 10*time.Second)
		check("POST /zz-early-then-read, 4 KiB body in 16 pieces 100 ms apart", reply, http.StatusOK)
		if len(reply.informational) != 1 || reply.informational[0] != http.StatusEarlyHints {
			t.Errorf("informational answers %v, want one 103 before the 200", reply.informational)
		}
		if readLen.Load() != 4096 || readErr.Load() != "<nil>" {
			t.Errorf("after 103 the handler read %d B of the body with error %v, want 4096 B and no error: an informational answer cut the body read", readLen.Load(), readErr.Load())
		}
		reply = k.earlyExchange(requestHead(http.MethodPost, "/zz-read-then-stream", nil, "", 1024), whole, 0, 10*time.Second)
		check("POST /zz-read-then-stream, 1 KiB body whole", reply, http.StatusOK)
		if len(reply.body) != 20 || after.alive.Load() != 20 {
			t.Errorf("the stream answered after the body sent %d B with the request context alive for %d ticks (%v), want 20 B and 20 ticks", len(reply.body), after.alive.Load(), after.reason.Load())
		}
		check("GET /healthz without a body", k.earlyExchange(requestHead(http.MethodGet, "/healthz", nil, "", -1), nil, 0, 10*time.Second), http.StatusOK)
		t.Logf("connections opened by the server: %d", news.Load())
		if news.Load() != 1 {
			t.Errorf("the server saw %d new connections, want 1: an honest client lost its keep-alive", news.Load())
		}
	})
	t.Run("HealthRoutes", func(t *testing.T) {
		var news atomic.Int64
		ts := startShippedServer(t, heapProbeServer().HealthRoutes(), countNewConns(&news))
		k := dialKept(t, ts)
		for i := range 3 {
			reply := k.earlyExchange(requestHead(http.MethodGet, "/healthz", nil, "", 1024), whole, 0, 10*time.Second)
			t.Logf("GET /healthz, 1 KiB body whole #%d: status %d, Connection: close %v, after %v, err %v", i, reply.status, reply.close, reply.elapsed.Round(time.Millisecond), reply.err)
			if reply.err != nil || reply.status != http.StatusOK || reply.close {
				t.Errorf("GET /healthz #%d: status %d, Connection: close %v, err %v; want 200 on a kept connection", i, reply.status, reply.close, reply.err)
			}
		}
		t.Logf("connections opened by the server: %d", news.Load())
		if news.Load() != 1 {
			t.Errorf("the server saw %d new connections, want 1: an honest client lost its keep-alive", news.Load())
		}
	})
}

type deadlineJournal struct {
	header http.Header
	events []journalEvent
}

type journalEvent struct {
	kind     string
	code     int
	conn     string
	deadline time.Time
}

func (j *deadlineJournal) Header() http.Header {
	return j.header
}

func (j *deadlineJournal) WriteHeader(code int) {
	j.events = append(j.events, journalEvent{kind: "header", code: code, conn: j.header.Get("Connection")})
}

func (j *deadlineJournal) Write(p []byte) (int, error) {
	j.events = append(j.events, journalEvent{kind: "write"})
	return len(p), nil
}

func (j *deadlineJournal) Flush() {
	j.events = append(j.events, journalEvent{kind: "flush"})
}

func (j *deadlineJournal) SetReadDeadline(d time.Time) error {
	j.events = append(j.events, journalEvent{kind: "deadline", deadline: d})
	return nil
}

func (j *deadlineJournal) trace(before, after time.Time, window time.Duration) string {
	var tokens []string
	for _, e := range j.events {
		switch e.kind {
		case "deadline":
			switch {
			case !e.deadline.Before(before.Add(refusalDrainGrace)) && !e.deadline.After(after.Add(refusalDrainGrace)):
				tokens = append(tokens, "grace")
			case !e.deadline.Before(before.Add(window)) && !e.deadline.After(after.Add(window)):
				tokens = append(tokens, "window")
			default:
				tokens = append(tokens, fmt.Sprintf("deadline+%v", e.deadline.Sub(before)))
			}
		case "header":
			token := fmt.Sprintf("WH%d", e.code)
			if e.conn != "" {
				token += "+" + e.conn
			}
			tokens = append(tokens, token)
		default:
			tokens = append(tokens, e.kind)
		}
	}
	return strings.Join(tokens, " ")
}

func TestEveryFinalHeaderBeforeTheBodyArmsTheGrace(t *testing.T) {
	header := func(codes ...int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			for _, code := range codes {
				w.WriteHeader(code)
			}
		}
	}
	readFirst := func(code int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			_, _ = r.Body.Read(make([]byte, 1))
			w.WriteHeader(code)
		}
	}
	rows := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		want    string
	}{
		{"WriteHeader 101", header(101), "window grace WH101 return"},
		{"WriteHeader 200", header(200), "window grace WH200 return"},
		{"WriteHeader 204", header(204), "window grace WH204 return"},
		{"WriteHeader 302", header(302), "window grace WH302 return"},
		{"WriteHeader 400", header(400), "window grace WH400+close return"},
		{"WriteHeader 429", header(429), "window grace WH429+close return"},
		{"WriteHeader 500", header(500), "window grace WH500+close return"},
		{"WriteHeader 100 then 200", header(100, 200), "window WH100 grace WH200 return"},
		{"WriteHeader 102 then 200", header(102, 200), "window WH102 grace WH200 return"},
		{"WriteHeader 103 then 200", header(103, 200), "window WH103 grace WH200 return"},
		{"WriteHeader 103 and return", header(103), "window WH103 return grace"},
		{"Write without WriteHeader", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "x") }, "window grace write return"},
		{"Flush first", func(w http.ResponseWriter, _ *http.Request) { _ = http.NewResponseController(w).Flush() }, "window grace flush return"},
		{"return without writing", func(http.ResponseWriter, *http.Request) {}, "window return grace"},
		{"read one body byte then 200", readFirst(200), "window WH200 return"},
		{"read one body byte then 400", readFirst(400), "window WH400 return"},
	}
	srv := heapProbeServer()
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			j := &deadlineJournal{header: http.Header{}}
			h := srv.armBodyWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				row.handler(w, r)
				j.events = append(j.events, journalEvent{kind: "return"})
			}))
			before := time.Now()
			h.ServeHTTP(j, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x")))
			after := time.Now()
			got := j.trace(before, after, srv.intakeWindow)
			t.Logf("%s: %s", row.name, got)
			if got != row.want {
				t.Errorf("%s: journal %q, want %q", row.name, got, row.want)
			}
		})
	}
}
