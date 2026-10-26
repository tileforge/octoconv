package api

import (
	"context"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/presets"
)

func headOnTheLine(prefix string) string {
	return prefix + headerBlock("distinct-lf", headerReadLimit-len(prefix+"\r\n")-16) + "\r\n"
}

func listenerRouter(srv *Server, listener string) chi.Router {
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

type serialRepo struct {
	*fakeRepo
	mu *sync.Mutex
}

func (l serialRepo) Create(ctx context.Context, p jobs.CreateParams) (uuid.UUID, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeRepo.Create(ctx, p)
}

func (l serialRepo) PreAdmit(ctx context.Context, clientID uuid.UUID, engine string, inputBytes int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeRepo.PreAdmit(ctx, clientID, engine, inputBytes)
}

func (l serialRepo) Get(ctx context.Context, id uuid.UUID) (*jobs.Job, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeRepo.Get(ctx, id)
}

func (l serialRepo) Outputs(ctx context.Context, id uuid.UUID) ([]jobs.Output, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeRepo.Outputs(ctx, id)
}

func (l serialRepo) Cancel(ctx context.Context, id uuid.UUID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeRepo.Cancel(ctx, id)
}

func (l serialRepo) List(ctx context.Context, clientID uuid.UUID, f jobs.ListFilter, cursor *jobs.Cursor, limit int) ([]jobs.JobSummary, *jobs.Cursor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeRepo.List(ctx, clientID, f, cursor, limit)
}

type serialStorage struct {
	*fakeStorage
	mu *sync.Mutex
}

func (l serialStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeStorage.Upload(ctx, key, r, size, contentType)
}

func (l serialStorage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeStorage.PresignGet(ctx, key, ttl)
}

type serialPresetRepo struct {
	*fakePresetRepo
	mu *sync.Mutex
}

func (l serialPresetRepo) Resolve(ctx context.Context, clientID uuid.UUID, name string) (*presets.Preset, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakePresetRepo.Resolve(ctx, clientID, name)
}

type serialPresetAdmin struct {
	*fakePresetAdmin
	mu *sync.Mutex
}

func (l serialPresetAdmin) Create(ctx context.Context, p presets.CreateParams) (uuid.UUID, int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakePresetAdmin.Create(ctx, p)
}

type serialUsageRepo struct {
	*fakeUsageRepo
	mu *sync.Mutex
}

func (l serialUsageRepo) UsageSummary(ctx context.Context, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]jobs.UsageRow, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeUsageRepo.UsageSummary(ctx, clientID, dayFrom, dayTo)
}

func (l serialUsageRepo) UTCToday(ctx context.Context) (time.Time, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeUsageRepo.UTCToday(ctx)
}

type serialLimitsRepo struct {
	*fakeLimitsRepo
	mu *sync.Mutex
}

func (l serialLimitsRepo) ListAllLimits(ctx context.Context, clientID *uuid.UUID) ([]clients.LimitWithMeta, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fakeLimitsRepo.ListAllLimits(ctx, clientID)
}

func earlyAnswerProbe() (*Server, uuid.UUID) {
	mu := &sync.Mutex{}
	resolver := newFakeResolver()
	job := &jobs.Job{ID: uuid.New(), ClientID: resolver.client.ID, Status: jobs.StatusDone}
	srv := NewServer(serialRepo{fakeRepo: &fakeRepo{getJob: job}, mu: mu}, serialStorage{fakeStorage: &fakeStorage{}, mu: mu}, &fakeQueue{}, serialPresetRepo{fakePresetRepo: defaultFakePresetRepo(), mu: mu}, serialPresetAdmin{fakePresetAdmin: defaultFakePresetAdmin(), mu: mu}, serialUsageRepo{fakeUsageRepo: &fakeUsageRepo{}, mu: mu}, serialLimitsRepo{fakeLimitsRepo: &fakeLimitsRepo{}, mu: mu}, resolver, healthyDeps(), Config{
		MaxUploadBytes:        64 << 20,
		InspectMaxUploadBytes: 64 << 20,
		OperatorClientIDs:     map[uuid.UUID]struct{}{resolver.client.ID: {}},
	})
	return srv, job.ID
}

type readMarks struct {
	readers atomic.Int64
}

type readMark struct {
	io.ReadCloser
	marks *readMarks
	seen  atomic.Bool
}

func (b *readMark) Read(p []byte) (int, error) {
	if b.seen.CompareAndSwap(false, true) {
		b.marks.readers.Add(1)
	}
	return b.ReadCloser.Read(p)
}

func (m *readMarks) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}
		marked := r.WithContext(r.Context())
		marked.Body = &readMark{ReadCloser: r.Body, marks: m}
		next.ServeHTTP(w, marked)
	})
}

type earlyOperation struct {
	listener, method, route string
}

func earlyOperations(t *testing.T) []earlyOperation {
	t.Helper()
	srv, _ := earlyAnswerProbe()
	var ops []earlyOperation
	for _, listener := range []string{"Routes", "HealthRoutes"} {
		var found []earlyOperation
		if err := chi.Walk(listenerRouter(srv, listener), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if route != "/" {
				route = strings.TrimSuffix(route, "/")
			}
			found = append(found, earlyOperation{listener: listener, method: method, route: route})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		sort.Slice(found, func(i, j int) bool {
			return found[i].route+" "+found[i].method < found[j].route+" "+found[j].method
		})
		t.Logf("chi.Walk over %s(): %d operations", listener, len(found))
		want := 19
		if listener == "HealthRoutes" {
			want = 1
		}
		if len(found) < want {
			t.Fatalf("chi.Walk over %s() found %d operations, want at least %d", listener, len(found), want)
		}
		ops = append(ops, found...)
	}
	return ops
}

type earlyRow struct {
	op        earlyOperation
	keyed     bool
	statuses  map[int]int
	readers   int64
	late      int64
	slotsPeak int64
}

func TestEveryEarlyAnswerReleasesAStalledBodyMass(t *testing.T) {
	const n = 80
	ops := earlyOperations(t)
	early := refusalDrainGrace + time.Second
	small := smallBodyWindow(smallBodyMaxBytes) + time.Second
	settle := max(2*time.Second, small)
	if settle >= intakeReadDeadline {
		t.Fatalf("smallBodyWindow(smallBodyMaxBytes) + 1 s = %v is not shorter than intakeReadDeadline %v: the walk cannot tell a small body from a request held for the body window", small, intakeReadDeadline)
	}
	lateLimit := intakeReadDeadline + 2*time.Second
	per := preAuthRequestHeapBytes()
	var rows []earlyRow
	for _, op := range ops {
		for _, keyed := range []bool{false, true} {
			scenario := "keyless"
			if keyed {
				scenario = "keyed"
			}
			t.Run(fmt.Sprintf("%s %s %s %s", op.listener, op.method, op.route, scenario), func(t *testing.T) {
				srv, job := earlyAnswerProbe()
				marks := &readMarks{}
				ts := startShippedServer(t, marks.wrap(listenerRouter(srv, op.listener)))
				var header http.Header
				if keyed {
					header = keyHeader(testClientKey)
				}
				target := concreteURL(strings.ReplaceAll(op.route, "{id}", job.String()))
				head := headOnTheLine(strings.TrimSuffix(requestHead(op.method, target, header, bodyWindowMultipart, 100_000), "\r\n"))
				var slotsPeak int64
				stop := make(chan struct{})
				sampled := make(chan struct{})
				go func() {
					defer close(sampled)
					tick := time.NewTicker(5 * time.Millisecond)
					defer tick.Stop()
					for {
						slotsPeak = max(slotsPeak, int64(len(srv.createIntakeSem)+len(srv.inspectIntakeSem)))
						select {
						case <-stop:
							return
						case <-tick.C:
						}
					}
				}()
				burst := preAuthRefusals(t, ts.Listener.Addr().String(), head, n, 0, 0, 1, 0, settle)
				close(stop)
				<-sampled
				readers := marks.readers.Load()
				statuses := map[int]int{}
				var earlyN, smallN, lateN int64
				var answer, closed time.Duration
				for i, o := range burst.outcomes {
					if err := errors.Join(o.writeErr, o.err); err != nil {
						t.Errorf("request %d: %v", i, err)
						continue
					}
					statuses[o.status]++
					took := o.answered.Sub(o.headSent)
					switch {
					case took <= early:
						earlyN++
						answer = max(answer, took)
						closed = max(closed, o.closed.Sub(o.answered))
						if d := o.closed.Sub(o.answered); d > early {
							t.Errorf("request %d: answered %d before the body, the server closed the connection %v after the answer, want no later than refusalDrainGrace + 1 s = %v", i, o.status, d, early)
						}
					case readers == 0:
						t.Errorf("request %d: answered %d %v after its head, later than refusalDrainGrace + 1 s = %v, but no request read the body: an answer before the body held its request", i, o.status, took, early)
					case took <= small:
						smallN++
					default:
						lateN++
						if took > lateLimit {
							t.Errorf("request %d: answered %d %v after its head, want no later than intakeReadDeadline + 2 s = %v", i, o.status, took, lateLimit)
						}
					}
				}
				if lateN > slotsPeak {
					t.Errorf("%d answers came later than smallBodyWindow(smallBodyMaxBytes) + 1 s = %v, above the peak %d of create and inspect intake slots taken: a request was held past the short window without an intake slot", lateN, small, slotsPeak)
				}
				if most := int64(min(n, srv.ipRateRPM, srv.clientRateRPM)); readers > most {
					t.Errorf("%d requests read the body, above min(%d, ipRateRPM %d, clientRateRPM %d) = %d", readers, n, srv.ipRateRPM, srv.clientRateRPM, most)
				}
				bound := max(1, lateN) * per
				if burst.tail > bound {
					t.Errorf("%v after the last head the mass holds %d bytes (%.1f MiB), above max(1, late %d) x preAuthRequestHeapBytes = %d (%.1f MiB)", settle, burst.tail, mib(burst.tail), lateN, bound, mib(bound))
				}
				if floor := lateN * headerPhaseFloor; lateN > 0 && burst.tail < floor {
					t.Errorf("%v after the last head the mass holds %d bytes, below late %d x headerPhaseFloor = %d: the measurement does not see the held heads", settle, burst.tail, lateN, floor)
				}
				verdict := "PASS"
				if t.Failed() {
					verdict = "FAIL"
				}
				t.Logf("EARLY | %s | %s %s | %s | statuses %v | readers %d | early %d | small %d | late %d | slots peak %d | answer %v | close %v | tail %d | bound %d | %s", op.listener, op.method, op.route, scenario, statuses, readers, earlyN, smallN, lateN, slotsPeak, answer.Round(time.Millisecond), closed.Round(time.Millisecond), burst.tail, bound, verdict)
				rows = append(rows, earlyRow{op: op, keyed: keyed, statuses: statuses, readers: readers, late: lateN, slotsPeak: slotsPeak})
			})
		}
	}
	answeredBelow400 := map[string]bool{}
	readRows := 0
	smallBodyRows, slotRows := 0, 0
	keyedSeen := map[string]earlyRow{}
	healthz := map[string]bool{}
	for _, row := range rows {
		if row.readers == 0 {
			for code := range row.statuses {
				if code < http.StatusBadRequest {
					answeredBelow400[row.op.listener] = true
				}
			}
		} else {
			readRows++
			if row.late == 0 && row.slotsPeak == 0 {
				smallBodyRows++
			}
		}
		if row.late > 0 && row.late <= row.slotsPeak {
			slotRows++
		}
		if row.keyed {
			keyedSeen[row.op.method+" "+row.op.route] = row
		}
		if row.op.method == http.MethodGet && row.op.route == "/healthz" {
			healthz[row.op.listener] = true
		}
	}
	for _, listener := range []string{"Routes", "HealthRoutes"} {
		if !answeredBelow400[listener] {
			t.Errorf("no row on %s answered below 400 without reading the body", listener)
		}
		if !healthz[listener] {
			t.Errorf("no GET /healthz row on %s", listener)
		}
	}
	if readRows == 0 {
		t.Errorf("no row read the body: the positive control is missing")
	}
	if smallBodyRows == 0 {
		t.Errorf("no row read the body with no late answer and no intake slot taken: the small-body control is missing")
	}
	if slotRows == 0 {
		t.Errorf("no row answered late within the peak of intake slots taken: the slot control is missing")
	}
	for _, op := range []string{"GET /v1/formats", "POST /v1/jobs/{id}/cancel"} {
		row, ok := keyedSeen[op]
		if !ok || row.readers != 0 || row.statuses[http.StatusOK] == 0 {
			t.Errorf("keyed %s: row found %v, readers %d, statuses %v; want 200 answers before the body and no reader", op, ok, row.readers, row.statuses)
		}
	}
	if row, ok := keyedSeen["POST /v1/presets"]; !ok || row.readers == 0 || row.late != 0 {
		t.Errorf("keyed POST /v1/presets: row found %v, readers %d, late %d; want a row whose requests read the body and none answered past the short window", ok, row.readers, row.late)
	}
	t.Logf("%d rows over %d operations; %d rows read the body, %d of them small bodies, %d rows answered late within the intake slots", len(rows), len(ops), readRows, smallBodyRows, slotRows)
}

func answerClassRouter(srv *Server, listener string) chi.Router {
	r := listenerRouter(srv, listener)
	code := func(r *http.Request) int {
		var c int
		_, _ = fmt.Sscan(chi.URLParam(r, "code"), &c)
		return c
	}
	r.Post("/zz-answer/{code}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code(r))
	})
	r.Post("/zz-implicit-write", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("i", 8<<10))
	})
	r.Post("/zz-flush-first", func(w http.ResponseWriter, _ *http.Request) {
		_ = http.NewResponseController(w).Flush()
		_, _ = io.WriteString(w, "flushed")
	})
	r.Post("/zz-silent", func(http.ResponseWriter, *http.Request) {})
	r.Post("/zz-informational/{code}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code(r))
		w.WriteHeader(http.StatusOK)
	})
	return r
}

func TestEveryAnswerClassBeforeTheBodyCutsTheBodyRead(t *testing.T) {
	type class struct {
		path          string
		status        int
		informational []int
	}
	var classes []class
	for _, code := range []int{200, 201, 202, 204, 301, 302, 304, 307} {
		classes = append(classes, class{path: fmt.Sprintf("/zz-answer/%d", code), status: code})
	}
	classes = append(classes,
		class{path: "/zz-implicit-write", status: http.StatusOK},
		class{path: "/zz-flush-first", status: http.StatusOK},
		class{path: "/zz-silent", status: http.StatusOK},
	)
	for _, code := range []int{100, 102, 103} {
		classes = append(classes, class{path: fmt.Sprintf("/zz-informational/%d", code), status: http.StatusOK, informational: []int{code}})
	}
	limit := refusalDrainGrace + time.Second
	for _, listener := range []string{"Routes", "HealthRoutes"} {
		ts := startShippedServer(t, answerClassRouter(heapProbeServer(), listener))
		for _, c := range classes {
			t.Run(listener+" "+c.path, func(t *testing.T) {
				k := dialKept(t, ts)
				reply := k.earlyExchange(requestHead(http.MethodPost, c.path, nil, "application/octet-stream", 100_000), []string{strings.Repeat("s", 1024)}, 0, intakeReadDeadline+10*time.Second)
				closedAfter, cerr := k.serverClosed(intakeReadDeadline + 5*time.Second)
				t.Logf("CLASS | %s | POST %s | informational %v | status %d | answer %v | Connection: close %v | server closed %v after the answer (%v)", listener, c.path, reply.informational, reply.status, reply.elapsed.Round(time.Millisecond), reply.close, closedAfter.Round(time.Millisecond), cerr)
				if reply.err != nil {
					t.Fatalf("no whole answer: %v", reply.err)
				}
				if reply.status != c.status {
					t.Errorf("status %d, want %d", reply.status, c.status)
				}
				if fmt.Sprint(reply.informational) != fmt.Sprint(c.informational) {
					t.Errorf("informational answers %v before the final one, want %v", reply.informational, c.informational)
				}
				if reply.elapsed > limit {
					t.Errorf("answered after %v, want no later than refusalDrainGrace + 1 s = %v: the answer waited for a body the route does not read", reply.elapsed, limit)
				}
				if !errors.Is(cerr, io.EOF) || closedAfter > limit {
					t.Errorf("the server closed the connection %v after the answer (%v), want EOF no later than refusalDrainGrace + 1 s = %v", closedAfter, cerr, limit)
				}
			})
		}
	}
}

func TestEarlyAnswerIsStatedInTheReadme(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(raw), "**Status semantics**")
	if !ok {
		t.Fatal("README.md has no Status semantics section")
	}
	section, _, _ = strings.Cut(section, "\n**")
	flat := strings.Join(strings.Fields(section), " ")
	for _, phrase := range []string{
		"Any other answer sent before the request body is read",
		fmt.Sprintf("waits at most %d ms", refusalDrainGrace.Milliseconds()),
		"the connection closes after the answer",
		"keeps the connection",
	} {
		if !strings.Contains(flat, phrase) {
			t.Errorf("README.md Status semantics does not say %q", phrase)
		}
	}
}
