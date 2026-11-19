package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/jobs"
)

type answerMoment struct {
	code, files, slots int
}

type momentWriter struct {
	http.ResponseWriter
	at   func(code int)
	done bool
}

func (m *momentWriter) WriteHeader(code int) {
	if !m.done && code >= 200 {
		m.done = true
		m.at(code)
	}
	m.ResponseWriter.WriteHeader(code)
}

func (m *momentWriter) Write(p []byte) (int, error) {
	if !m.done {
		m.done = true
		m.at(http.StatusOK)
	}
	return m.ResponseWriter.Write(p)
}

func (m *momentWriter) Unwrap() http.ResponseWriter { return m.ResponseWriter }

func heldSlots(srv *Server) int {
	return len(srv.createIntakeSem) + len(srv.inspectIntakeSem) + len(srv.inspectSem.sem)
}

func spillSlotsConfig(cfg Config) Config {
	cfg.IPRateLimitRPM = 1 << 20
	cfg.ClientRateLimitRPM = 1 << 20
	cfg.InspectRateLimitRPM = 1 << 20
	return cfg
}

func newSpillSlotsServer(cfg Config) *Server {
	srv := NewServer(&lockedRepo{fakeRepo: &fakeRepo{}}, &fakeStorage{}, &lockedQueue{fakeQueue: &fakeQueue{}}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), spillSlotsConfig(cfg))
	srv.SetCapabilityCatalog(pandocCatalog())
	return srv
}

func startAnswerMoments(t *testing.T, srv *Server, dir string, tokens int) (*httptest.Server, func() []answerMoment) {
	t.Helper()
	router := srv.Routes()
	var mu sync.Mutex
	var seen []answerMoment
	ts := startShippedServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(&momentWriter{ResponseWriter: w, at: func(code int) {
			m := answerMoment{code: code, files: len(spillFiles(t, dir)), slots: heldSlots(srv) - tokens}
			mu.Lock()
			seen = append(seen, m)
			mu.Unlock()
		}}, r)
	}))
	return ts, func() []answerMoment {
		mu.Lock()
		defer mu.Unlock()
		return append([]answerMoment(nil), seen...)
	}
}

func spillForm(t *testing.T, field, filename string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, mw.FormDataContentType()
}

func spillPNG() []byte {
	return paddedPNG(len(pngBytesFixture()) + multipartInMemoryBudget + 1<<20)
}

func spillMarkdown() []byte {
	return paddedMarkdown(multipartInMemoryBudget + 1<<20)
}

func filesGoneWithin(t *testing.T, dir string, limit time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		n := len(spillFiles(t, dir))
		if n == 0 || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEverySpillHasAHeldSlotAtTheAnswer(t *testing.T) {
	type row struct {
		name      string
		cfg       Config
		path      string
		fillSlots bool
		form      func(t *testing.T) (*bytes.Buffer, string)
		status    int
		code      string
		wantFiles int
		wantSlots int
	}
	rows := []row{
		{
			name: "inspect 429 inspect_busy", cfg: createIntakeConfig(64 << 20), path: "/v1/inspect", fillSlots: true,
			form: func(t *testing.T) (*bytes.Buffer, string) {
				return spillForm(t, formFieldFile, "in.png", spillPNG(), nil)
			},
			status: http.StatusTooManyRequests, code: "inspect_busy", wantFiles: 1, wantSlots: 1,
		},
		{
			name: "inspect 400 missing_file", cfg: createIntakeConfig(64 << 20), path: "/v1/inspect",
			form:   func(t *testing.T) (*bytes.Buffer, string) { return spillForm(t, "other", "in.png", spillPNG(), nil) },
			status: http.StatusBadRequest, code: "missing_file", wantFiles: 1, wantSlots: 1,
		},
		{
			name: "inspect 200", cfg: createIntakeConfig(64 << 20), path: "/v1/inspect",
			form: func(t *testing.T) (*bytes.Buffer, string) {
				return spillForm(t, formFieldFile, "in.png", spillPNG(), nil)
			},
			status: http.StatusOK, wantFiles: 0, wantSlots: 0,
		},
		{
			name: "create 202", cfg: createIntakeConfig(64 << 20), path: "/v1/jobs",
			form: func(t *testing.T) (*bytes.Buffer, string) {
				return spillForm(t, formFieldFile, "in.md", spillMarkdown(), map[string]string{"target": "html"})
			},
			status: http.StatusAccepted, wantFiles: 1, wantSlots: 1,
		},
		{
			name: "create 422 callback_url too long", cfg: createIntakeConfig(64 << 20), path: "/v1/jobs",
			form: func(t *testing.T) (*bytes.Buffer, string) {
				cb := "https://example.com/" + strings.Repeat("a", maxCallbackURLBytes)
				return spillForm(t, formFieldFile, "in.md", spillMarkdown(), map[string]string{"target": "html", formFieldCallbackURL: cb})
			},
			status: http.StatusUnprocessableEntity, code: "payload_too_large", wantFiles: 1, wantSlots: 1,
		},
		{
			name: "inspect 413 mid-spill",
			cfg: Config{
				MaxUploadBytes:        64 << 20,
				InspectMaxUploadBytes: multipartInMemoryBudget + 2<<20,
				InspectMaxWorkBytes:   64 << 20,
			},
			path: "/v1/inspect",
			form: func(t *testing.T) (*bytes.Buffer, string) {
				return spillForm(t, formFieldFile, "in.png", paddedPNG(multipartInMemoryBudget+4<<20), nil)
			},
			status: http.StatusRequestEntityTooLarge, code: "payload_too_large", wantFiles: 0,
		},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			srv := newSpillSlotsServer(tc.cfg)
			tokens := 0
			if tc.fillSlots {
				fillIntakeSlots(t, srv.inspectSem.sem)
				tokens = cap(srv.inspectSem.sem)
			}
			ts, moments := startAnswerMoments(t, srv, dir, tokens)
			body, ct := tc.form(t)
			res := rawPost(t, ts, tc.path, nil, ct, int64(body.Len()), body)
			after := filesGoneWithin(t, dir, 2*time.Second)
			seen := moments()
			if len(seen) != 1 {
				t.Fatalf("%d answer moments %+v, want exactly one", len(seen), seen)
			}
			m := seen[0]
			t.Logf("ANSWER | %s | %d | %d | %d | %d", tc.name, m.code, m.files, m.slots, after)
			if res.status != tc.status || (tc.code != "" && res.payload["code"] != tc.code) {
				t.Fatalf("status %d payload %v, want %d %s: %.300s", res.status, res.payload, tc.status, tc.code, res.raw)
			}
			if m.code != tc.status {
				t.Errorf("the answer moment saw code %d, want %d", m.code, tc.status)
			}
			if m.files > m.slots {
				t.Errorf("at the answer %d spill files and %d held slots: a spill file has no slot", m.files, m.slots)
			}
			if tc.wantFiles > 0 && m.files < tc.wantFiles {
				t.Errorf("at the answer %d spill files, want at least %d: the row proves nothing without a spill", m.files, tc.wantFiles)
			}
			if tc.wantFiles == 0 && m.files != 0 {
				t.Errorf("at the answer %d spill files, want 0", m.files)
			}
			if tc.status != http.StatusRequestEntityTooLarge && m.slots != tc.wantSlots {
				t.Errorf("at the answer %d held slots, want %d", m.slots, tc.wantSlots)
			}
			if after != 0 {
				t.Errorf("%d spill files 2s after the answer", after)
			}
		})
	}
}

type gatedPreAdmitRepo struct {
	*fakeRepo
	gate  chan struct{}
	mu    sync.Mutex
	sizes []int64
}

func (r *gatedPreAdmitRepo) PreAdmit(_ context.Context, _ uuid.UUID, _ string, inputBytes int64) error {
	r.mu.Lock()
	r.sizes = append(r.sizes, inputBytes)
	r.mu.Unlock()
	<-r.gate
	return nil
}

func (r *gatedPreAdmitRepo) Create(_ context.Context, p jobs.CreateParams) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fakeRepo.created = &p
	return uuid.New(), nil
}

func (r *gatedPreAdmitRepo) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sizes)
}

func (r *gatedPreAdmitRepo) sawAllSince(from int, sizes []int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if from > len(r.sizes) {
		return false
	}
	for _, want := range sizes {
		found := false
		for _, got := range r.sizes[from:] {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type lockedStorage struct {
	*fakeStorage
	mu sync.Mutex
}

func (s *lockedStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStorage.Upload(ctx, key, r, size, contentType)
}

type spillSample struct {
	at                     time.Time
	phase                  string
	f1, f2, persistent, s  int
	create, intake, slots3 int
}

func multipartNames(dir string) map[string]struct{} {
	entries, _ := os.ReadDir(dir)
	out := map[string]struct{}{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "multipart-") {
			out[e.Name()] = struct{}{}
		}
	}
	return out
}

type spillSampler struct {
	srv     *Server
	dir     string
	phase   atomic.Value
	maxA    atomic.Int64
	stop    chan struct{}
	stopped chan struct{}
	samples []spillSample
}

func startSpillSampler(srv *Server, dir string) *spillSampler {
	sp := &spillSampler{srv: srv, dir: dir, stop: make(chan struct{}), stopped: make(chan struct{})}
	sp.phase.Store("A")
	go func() {
		defer close(sp.stopped)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-sp.stop:
				return
			case <-tick.C:
			}
			phase := sp.phase.Load().(string)
			f1 := multipartNames(dir)
			c := len(srv.createIntakeSem)
			i := len(srv.inspectIntakeSem)
			a := len(srv.inspectSem.sem)
			f2 := multipartNames(dir)
			persistent := 0
			for name := range f1 {
				if _, ok := f2[name]; ok {
					persistent++
				}
			}
			smp := spillSample{at: time.Now(), phase: phase, f1: len(f1), f2: len(f2), persistent: persistent, s: c + i + a, create: c, intake: i, slots3: a}
			sp.samples = append(sp.samples, smp)
			if phase == "A" && int64(persistent) > sp.maxA.Load() {
				sp.maxA.Store(int64(persistent))
			}
		}
	}()
	return sp
}

func (sp *spillSampler) halt() []spillSample {
	close(sp.stop)
	<-sp.stopped
	return sp.samples
}

type probeClient struct {
	class  string
	conn   net.Conn
	done   chan struct{}
	status int
	code   string
	size   int
	err    error
	at     time.Time
}

func probeSend(t *testing.T, addr, path, ct, class string, body []byte, declared, send int) *probeClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(intakeReadDeadline + 30*time.Second)); err != nil {
		t.Fatal(err)
	}
	c := &probeClient{class: class, conn: conn, done: make(chan struct{})}
	head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: ApiKey %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n", path, addr, testClientKey, ct, declared)
	if _, err := io.WriteString(conn, head); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = conn.Write(body[:send])
	}()
	go func() {
		defer close(c.done)
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		c.at = time.Now()
		if err != nil {
			c.err = err
			return
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.status, c.size, c.err = resp.StatusCode, len(raw), err
		var payload map[string]any
		if json.Unmarshal(raw, &payload) == nil {
			if code, ok := payload["code"].(string); ok {
				c.code = code
			}
		}
	}()
	return c
}

func waitUntil(t *testing.T, what string, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not reached within %v", what, limit)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitAnswered(t *testing.T, limit time.Duration, clients ...*probeClient) {
	t.Helper()
	deadline := time.After(limit)
	for _, c := range clients {
		select {
		case <-c.done:
		case <-deadline:
			t.Fatalf("%s: no answer within %v", c.class, limit)
		}
	}
}

func midFile(body []byte) int {
	return bytes.Index(body, []byte("\r\n\r\n")) + 4 + multipartInMemoryBudget + 1<<20
}

func TestSpillFilesNeverOutnumberHeldSlots(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	repo := &gatedPreAdmitRepo{fakeRepo: &fakeRepo{}, gate: make(chan struct{})}
	var gateOnce sync.Once
	openGate := func() { gateOnce.Do(func() { close(repo.gate) }) }
	t.Cleanup(openGate)
	srv := NewServer(repo, &lockedStorage{fakeStorage: &fakeStorage{}}, &lockedQueue{fakeQueue: &fakeQueue{}}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), spillSlotsConfig(createIntakeConfig(64<<20)))
	srv.SetCapabilityCatalog(pandocCatalog())
	ts := startShippedServer(t, srv.Routes())
	addr := ts.Listener.Addr().String()
	t.Logf("intake window %v, intake min rate %d B/s", srv.intakeWindow, srv.intakeMinRate)

	createCap, analysisCap := cap(srv.createIntakeSem), cap(srv.inspectSem.sem)
	lowerBound := createCap + analysisCap + 2
	started := time.Now()
	sp := startSpillSampler(srv, dir)
	var all []*probeClient
	send := func(path, ct, class string, body []byte, declared, n int) *probeClient {
		c := probeSend(t, addr, path, ct, class, body, declared, n)
		all = append(all, c)
		return c
	}
	createForm := func(size int) ([]byte, string) {
		b, ct := spillForm(t, formFieldFile, "in.md", paddedMarkdown(size), map[string]string{"target": "html"})
		return b.Bytes(), ct
	}
	inspectForm := func(size int) ([]byte, string) {
		b, ct := spillForm(t, formFieldFile, "in.png", paddedPNG(size), nil)
		return b.Bytes(), ct
	}
	spillSize := multipartInMemoryBudget + 1<<20
	stallSize := multipartInMemoryBudget + 4<<20
	pngSpill := len(pngBytesFixture()) + spillSize

	var a1, a2, a4, a5, a6, cc []*probeClient
	for range createCap - 2 {
		body, ct := createForm(spillSize)
		a1 = append(a1, send("/v1/jobs", ct, "A1 create in PreAdmit", body, len(body), len(body)))
	}
	waitUntil(t, "A1 creates in PreAdmit", 20*time.Second, func() bool { return repo.calls() == createCap-2 })
	for range 2 {
		body, ct := createForm(stallSize)
		a2 = append(a2, send("/v1/jobs", ct, "A2 create stalled mid-file", body, len(body), midFile(body)))
	}
	waitUntil(t, "A2 creates spilled mid-file", 20*time.Second, func() bool {
		return len(srv.createIntakeSem) == createCap && len(spillFiles(t, dir)) == createCap
	})
	small, smallCT := createForm(4096)
	a3 := send("/v1/jobs", smallCT, "A3 create intake_busy", small, len(small), len(small))
	waitAnswered(t, 10*time.Second, a3)
	for range analysisCap {
		body, ct := inspectForm(pngSpill)
		a4 = append(a4, send("/v1/inspect", ct, "A4 inspect in PreAdmit", body, len(body), len(body)))
	}
	waitUntil(t, "A4 inspects in PreAdmit", 20*time.Second, func() bool { return repo.calls() == createCap-2+analysisCap })
	for range 2 {
		body, ct := inspectForm(len(pngBytesFixture()) + stallSize)
		a5 = append(a5, send("/v1/inspect", ct, "A5 inspect stalled mid-file", body, len(body), midFile(body)))
	}
	waitUntil(t, "A5 inspects spilled mid-file", 20*time.Second, func() bool {
		return len(srv.inspectIntakeSem) == 2 && len(spillFiles(t, dir)) == createCap+analysisCap+2
	})
	for range 3 {
		body, ct := inspectForm(pngSpill)
		a6 = append(a6, send("/v1/inspect", ct, "A6 inspect inspect_busy", body, len(body), len(body)))
	}
	waitAnswered(t, 20*time.Second, a6...)
	waitUntil(t, fmt.Sprintf("a phase A sample with persistent >= %d", lowerBound), 10*time.Second, func() bool { return sp.maxA.Load() >= int64(lowerBound) })

	sp.phase.Store("B")
	openGate()
	waitAnswered(t, 30*time.Second, append(append([]*probeClient{}, a1...), a4...)...)

	sp.phase.Store("C")
	from := repo.calls()
	var cSizes []int64
	for i := range 3 {
		size := pngSpill + i
		cSizes = append(cSizes, int64(size))
		body, ct := inspectForm(size)
		cc = append(cc, send("/v1/inspect", ct, "C inspect stalled after the form", body, len(body)+1024, len(body)))
	}
	waitUntil(t, "C inspects in PreAdmit", 20*time.Second, func() bool { return repo.sawAllSince(from, cSizes) })
	cReady := time.Now()
	waitAnswered(t, intakeReadDeadline+30*time.Second, cc[0])
	sp.phase.Store("D")
	waitAnswered(t, intakeReadDeadline+30*time.Second, append(append(append([]*probeClient{}, a2...), a5...), cc...)...)

	for _, c := range all {
		_ = c.conn.Close()
	}
	settle := time.Now().Add(10 * time.Second)
	for (len(spillFiles(t, dir)) != 0 || heldSlots(srv) != 0) && time.Now().Before(settle) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	samples := sp.halt()
	duration := time.Since(started)

	firstC := cc[0].at
	for _, c := range cc {
		if c.at.Before(firstC) {
			firstC = c.at
		}
	}

	type agg struct{ samples, maxPersistent, maxS, maxRaw, violations, rawOver int }
	phases := map[string]*agg{}
	total := agg{}
	phaseC := 0
	for _, smp := range samples {
		a := phases[smp.phase]
		if a == nil {
			a = &agg{}
			phases[smp.phase] = a
		}
		for _, x := range []*agg{a, &total} {
			x.samples++
			x.maxPersistent = max(x.maxPersistent, smp.persistent)
			x.maxS = max(x.maxS, smp.s)
			x.maxRaw = max(x.maxRaw, smp.f2)
			if smp.persistent > smp.s {
				x.violations++
			}
			if smp.f2 > smp.s {
				x.rawOver++
			}
		}
		if smp.persistent > smp.s && total.violations <= 10 {
			t.Errorf("phase %s at %v: %d spill files lived through the slot read, %d slots held (create %d, inspect intake %d, analysis %d)", smp.phase, smp.at.Sub(started).Round(time.Millisecond), smp.persistent, smp.s, smp.create, smp.intake, smp.slots3)
		}
		if smp.at.After(cReady) && smp.at.Before(firstC) {
			phaseC++
		}
	}
	for _, name := range []string{"A", "B", "C", "D"} {
		a := phases[name]
		if a == nil {
			a = &agg{}
		}
		t.Logf("SPILL | %s | %d | %d | %d | %d | %d", name, a.samples, a.maxPersistent, a.maxS, a.maxRaw, a.violations)
		t.Logf("SPILL-RAW | %s | samples with raw len(F2) > S: %d", name, a.rawOver)
	}
	maxA := 0
	if a := phases["A"]; a != nil {
		maxA = a.maxPersistent
	}
	t.Logf("SPILL-DONE | %d | %d | %d | %d | %d | %v", total.samples, total.violations, lowerBound, maxA, phaseC, duration.Round(time.Millisecond))

	if floor := int(duration / (10 * time.Millisecond)); total.samples < floor {
		t.Errorf("%d samples over %v, want at least %d", total.samples, duration, floor)
	}
	if maxA < lowerBound {
		t.Errorf("max persistent in phase A %d, want at least %d = cap(createIntakeSem) %d + cap(inspectSem.sem) %d + 2 inspects stalled mid-file", maxA, lowerBound, createCap, analysisCap)
	}
	if phaseC < 20 {
		t.Errorf("%d samples while all three C inspects stood in the discard, want at least 20", phaseC)
	}

	expect := func(group []*probeClient, status int, code string, bigger int) {
		for _, c := range group {
			if c.err != nil || c.status != status || (code != "" && c.code != code) || (bigger > 0 && c.size <= bigger) {
				t.Errorf("%s: status %d code %q size %d err %v, want %d %s", c.class, c.status, c.code, c.size, c.err, status, code)
			}
		}
	}
	expect(a1, http.StatusAccepted, "", 0)
	expect(a2, http.StatusBadRequest, "", 0)
	expect([]*probeClient{a3}, http.StatusTooManyRequests, "intake_busy", 0)
	expect(a4, http.StatusOK, "", 0)
	expect(a5, http.StatusBadRequest, "", 0)
	expect(a6, http.StatusTooManyRequests, "inspect_busy", 0)
	expect(cc, http.StatusOK, "", 2048)
	statuses := map[string]map[int]int{}
	for _, c := range all {
		if statuses[c.class] == nil {
			statuses[c.class] = map[int]int{}
		}
		statuses[c.class][c.status]++
	}
	for _, c := range []string{"A1 create in PreAdmit", "A2 create stalled mid-file", "A3 create intake_busy", "A4 inspect in PreAdmit", "A5 inspect stalled mid-file", "A6 inspect inspect_busy", "C inspect stalled after the form"} {
		t.Logf("STATUS | %s | %v", c, statuses[c])
	}

	if files := spillFiles(t, dir); len(files) != 0 {
		t.Errorf("spill files at the end: %v", files)
	}
	if c, i, a := len(srv.createIntakeSem), len(srv.inspectIntakeSem), len(srv.inspectSem.sem); c+i+a != 0 {
		t.Errorf("slots held at the end: create %d, inspect intake %d, analysis %d", c, i, a)
	}
}
