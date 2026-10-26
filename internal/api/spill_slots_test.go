package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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
	return NewServer(&lockedRepo{fakeRepo: &fakeRepo{}}, &fakeStorage{}, &lockedQueue{fakeQueue: &fakeQueue{}}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), spillSlotsConfig(cfg))
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
