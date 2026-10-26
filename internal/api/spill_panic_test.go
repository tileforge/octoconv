package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
)

type panicGate struct {
	armed  atomic.Bool
	in     chan struct{}
	gate   chan struct{}
	panics bool
}

func newPanicGate(panics bool) *panicGate {
	g := &panicGate{in: make(chan struct{}, 1), gate: make(chan struct{}), panics: panics}
	g.armed.Store(true)
	return g
}

func (g *panicGate) hold(where string) {
	if !g.armed.CompareAndSwap(true, false) {
		return
	}
	g.in <- struct{}{}
	<-g.gate
	if g.panics {
		panic("synthetic panic " + where)
	}
}

type gatedRepo struct {
	*fakeRepo
	g *panicGate
}

func (r *gatedRepo) PreAdmit(ctx context.Context, id uuid.UUID, engine string, n int64) error {
	r.g.hold("in PreAdmit")
	return r.fakeRepo.PreAdmit(ctx, id, engine, n)
}

type gatedStorage struct {
	*fakeStorage
	g      *panicGate
	suffix string
}

func (s *gatedStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, ct string) error {
	if strings.HasSuffix(key, s.suffix) {
		s.g.hold("in the attachment upload")
	}
	return s.fakeStorage.Upload(ctx, key, r, size, ct)
}

type gatedCtx struct {
	context.Context
	g *panicGate
}

func (c gatedCtx) Deadline() (time.Time, bool) {
	c.g.hold("at the first Deadline under the analysis slot")
	return c.Context.Deadline()
}

func spillIdentity(t *testing.T, path string) *syscall.Stat_t {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Sys().(*syscall.Stat_t)
}

func openHandlesOn(t *testing.T, want *syscall.Stat_t) int {
	t.Helper()
	if want == nil {
		return 0
	}
	d, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	names, err := d.Readdirnames(-1)
	_ = d.Close()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, name := range names {
		fd, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		var s syscall.Stat_t
		if syscall.Fstat(fd, &s) == nil && s.Dev == want.Dev && s.Ino == want.Ino {
			n++
		}
	}
	return n
}

func TestPanicUnderASlotDropsTheFormBeforeTheRelease(t *testing.T) {
	inMemSize := multipartInMemoryBudget - 1<<20
	type row struct {
		name     string
		path     string
		at       string
		panics   bool
		inMemory bool
		cfg      func(Config) Config
		slot     func(*Server) chan struct{}
		form     func(t *testing.T) (*bytes.Buffer, string)
		status   int
	}
	analysis := func(s *Server) chan struct{} { return s.inspectSem.sem }
	create := func(s *Server) chan struct{} { return s.createIntakeSem }
	png := func(t *testing.T) (*bytes.Buffer, string) {
		return spillForm(t, formFieldFile, "in.png", spillPNG(), nil)
	}
	rows := []row{
		{name: "inspect 200 normal path (control)", path: "/v1/inspect", at: "repo", slot: analysis, form: png, status: http.StatusOK},
		{name: "inspect 500 panic in PreAdmit", path: "/v1/inspect", at: "repo", panics: true, slot: analysis, form: png, status: http.StatusInternalServerError},
		{name: "inspect 500 panic before the work-ceiling context", path: "/v1/inspect", at: "trap", panics: true, slot: analysis, form: png, status: http.StatusInternalServerError},
		{name: "inspect 500 panic in PreAdmit, form in memory", path: "/v1/inspect", at: "repo", panics: true, inMemory: true, slot: analysis,
			form: func(t *testing.T) (*bytes.Buffer, string) {
				return spillForm(t, formFieldFile, "in.png", paddedPNG(inMemSize), nil)
			}, status: http.StatusInternalServerError},
		{name: "create 500 panic in PreAdmit", path: "/v1/jobs", at: "repo", panics: true, slot: create,
			form: func(t *testing.T) (*bytes.Buffer, string) {
				return spillForm(t, formFieldFile, "in.md", spillMarkdown(), map[string]string{"target": "html"})
			}, status: http.StatusInternalServerError},
		{name: "create 500 panic in the upload of a spilled attachment", path: "/v1/jobs", at: "storage", panics: true, slot: create,
			form: func(t *testing.T) (*bytes.Buffer, string) {
				return multipartParts(t, "pdf",
					[2]string{"paper.tex", texMain},
					[2]string{"preamble.tex", "\\newcommand{\\x}{y}\n"},
					[2]string{"fig.bin", strings.Repeat("x", multipartInMemoryBudget+1<<20)},
				)
			}, status: http.StatusInternalServerError,
			cfg: func(c Config) Config {
				c.MaxEngineBytes = map[string]int64{convert.EngineTectonic: 64 << 20, convert.EnginePandoc: 64 << 20}
				return c
			}},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			g := newPanicGate(tc.panics)
			var repo Repo = &fakeRepo{}
			var st Storage = &fakeStorage{}
			switch tc.at {
			case "repo":
				repo = &gatedRepo{fakeRepo: &fakeRepo{}, g: g}
			case "storage":
				st = &gatedStorage{fakeStorage: &fakeStorage{}, g: g, suffix: "-fig.bin"}
			}
			cfg := spillSlotsConfig(createIntakeConfig(64 << 20))
			if tc.cfg != nil {
				cfg = tc.cfg(cfg)
			}
			srv := NewServer(repo, st, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), cfg)
			var h http.Handler = srv.Routes()
			if tc.at == "trap" {
				router := h
				h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					router.ServeHTTP(w, r.WithContext(gatedCtx{Context: r.Context(), g: g}))
				})
			}
			ts := startShippedServer(t, h)
			body, ct := tc.form(t)
			raw := body.Bytes()
			base := liveHeap()
			c := probeSend(t, ts.Listener.Addr().String(), tc.path, ct, tc.name, raw, len(raw), len(raw))
			select {
			case <-g.in:
			case <-c.done:
				t.Fatalf("answered %d %s before the pause: %v", c.status, c.code, c.err)
			case <-time.After(20 * time.Second):
				t.Fatal("the request never reached the pause")
			}
			slot := tc.slot(srv)
			if len(slot) != 1 || heldSlots(srv) != 1 {
				t.Fatalf("at the pause the request holds %d of its slot class and %d slots in all, want 1 and 1", len(slot), heldSlots(srv))
			}
			var id *syscall.Stat_t
			pauseFiles := spillFiles(t, dir)
			pauseHandles := 0
			var pauseGrowth int64
			if tc.inMemory {
				pauseGrowth = int64(liveHeap()) - int64(base)
				if len(pauseFiles) != 0 || pauseGrowth < int64(inMemSize)/2 {
					t.Fatalf("at the pause %d spill files and %d B heap growth: the in-memory form row needs 0 files and at least %d B", len(pauseFiles), pauseGrowth, inMemSize/2)
				}
			} else {
				if len(pauseFiles) != 1 {
					t.Fatalf("at the pause %d spill files, want 1", len(pauseFiles))
				}
				id = spillIdentity(t, pauseFiles[0])
				pauseHandles = openHandlesOn(t, id)
				if pauseHandles < 1 {
					t.Fatalf("at the pause the handle probe sees %d open handles on the spill, want at least 1", pauseHandles)
				}
			}
			<-slot
			close(g.gate)
			var files, handles, others int
			var growth int64
			cleared := false
			start := time.Now()
			for {
				files, handles, others = len(spillFiles(t, dir)), openHandlesOn(t, id), heldSlots(srv)
				ok := files == 0 && handles == 0
				if tc.inMemory {
					growth = int64(liveHeap()) - int64(base)
					ok = ok && growth < int64(inMemSize)/2
				}
				if ok {
					cleared = true
					break
				}
				if time.Since(start) > 5*time.Second {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			waited := time.Since(start)
			slot <- struct{}{}
			waitAnswered(t, 20*time.Second, c)
			afterFiles := filesGoneWithin(t, dir, 2*time.Second)
			afterHandles, afterSlots := openHandlesOn(t, id), heldSlots(srv)
			t.Logf("PANIC-RELEASE | %s | status %d | pause [files handles growth] [%d %d %d] | at the release [files handles other-slots] [%d %d %d] growth %d | cleared %v after %v | after [files handles slots] [%d %d %d]",
				tc.name, c.status, len(pauseFiles), pauseHandles, pauseGrowth, files, handles, others, growth, cleared, waited.Round(time.Millisecond), afterFiles, afterHandles, afterSlots)
			if c.status != tc.status {
				t.Errorf("status %d (%s, %v), want %d", c.status, c.code, c.err, tc.status)
			}
			if !cleared {
				t.Errorf("the form outlived the slot: at the release %d spill files, %d open spill handles, %d other slots, heap growth %d", files, handles, others, growth)
			}
			if others != 0 {
				t.Errorf("%d other slots held at the release", others)
			}
			if afterFiles != 0 || afterHandles != 0 || afterSlots != 0 {
				t.Errorf("after the answer %d files, %d handles, %d slots", afterFiles, afterHandles, afterSlots)
			}
			runtime.KeepAlive(raw)
		})
	}
}
