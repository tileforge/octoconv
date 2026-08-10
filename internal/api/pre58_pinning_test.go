package api

// This suite pins TODAY's (pre-Phase-58) exact byte body of three surfaces --
// GET /healthz, GET /v1/formats, and all ten distinguishable content-
// validation rejections of POST /v1/jobs -- per 58-CONTEXT.md D-15, the same
// protocol as 57-CONTEXT.md D-17 (itself a reproduction of 56-CONTEXT.md D-10
// step 1), reproduced here because Phase 58 touches all three surfaces:
//
//   - GET /healthz -- Plan 58-03 moves this endpoint onto its own listener
//     (HEALTH_ADDR) and repoints the Helm chart's liveness/readiness probes at
//     the new port. The response BODY must stay byte-identical across that
//     move, or the "move" silently became a "rewrite".
//   - GET /v1/formats -- this phase adds the inspect report's operation
//     enumeration to a NEW endpoint, not this one. Pinning this body makes
//     that claim checkable instead of asserted.
//   - Every content-validation rejection of POST /v1/jobs -- Plan 58-02 (D-04)
//     binds each of these to a code from the new findings vocabulary. That
//     binding must be purely declarative: not one status code and not one
//     byte of any rejection body may change. Without this snapshot, "nothing
//     changed" is a claim, not a fact.
//
// Golden files under testdata/pre58_*.golden.json are committed in the FIRST
// commit of Phase 58, before a single line of new production code lands
// (58-01-PLAN.md Task 1). Every later plan in this phase MUST leave this
// suite passing WITHOUT modification.
//
// Разрешённые исключения (closed list of permitted exceptions):
// Исключений нет. Ни один план фазы 58 не имеет права изменить ни Go-код
// этой сьюты, ни байты её golden-файлов. Если исполнитель считает, что
// исключение необходимо, это сигнал, что меняется контракт, а не тест:
// остановиться и эскалировать, а не править сьюту.
//
// The mechanical immutability check, and its expected value, is:
//
//	git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json               # must print 1
//	git rev-list --count HEAD -- internal/api/testdata/pre58_formats.golden.json               # must print 1
//	git rev-list --count HEAD -- internal/api/testdata/pre58_createjob_rejections.golden.json  # must print 1
//
// This is `git rev-list --count`, not a ref-less `git diff`: `git diff
// --name-only internal/api/testdata/` with no ref compares the working tree
// against the index and prints nothing the instant a change is committed --
// i.e. it goes blind in EXACTLY the scenario D-15 exists to catch (golden
// bytes and SUT edited together in one commit). A commit count is not blind
// to that.
import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// updateGolden58 regenerates testdata/pre58_*.golden.json from the CURRENT
// handler output. It exists only to create/refresh the fixtures on the very
// first run against pre-Phase-58 code (58-01-PLAN.md Task 1); it must never
// be passed once the fixtures are committed, since that would defeat D-15.
// Named distinctly from golden_pinning_test.go's -update-golden flag: two
// flags with the identical name registered in the same package panic with
// "flag redefined" at test-binary startup.
var updateGolden58 = flag.Bool("update-golden-58", false, "rewrite testdata/pre58_*.golden.json from the current handler output")

// assertGolden58 compares got against the on-disk fixture
// testdata/pre58_<name>, or (with -update-golden-58) rewrites that fixture
// from got. The read happens directly against the file on disk -- never
// against an inline literal in this test's source -- which is the entire
// point of D-15: the expectation lives where the person changing the SUT
// cannot silently edit it in the same commit.
func assertGolden58(t *testing.T, name string, got []byte) {
	t.Helper()
	path := "testdata/pre58_" + name
	if *updateGolden58 {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s (-update-golden-58)", path)
		return
	}
	want, err := os.ReadFile("testdata/pre58_" + name)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s: response bytes changed -- want byte-identical to testdata/%s (D-15 pinning violation)\ngot:  %s\nwant: %s", name, path, got, want)
	}
}

// TestPre58GoldenHealthz pins GET /healthz's exact pre-Phase-58 body, unauthenticated,
// with all three dependencies healthy -- the shape Plan 58-03 must preserve
// byte-for-byte when it moves this handler onto HEALTH_ADDR.
func TestPre58GoldenHealthz(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil) // deliberately no Authorization header
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	assertGolden58(t, "healthz.golden.json", rec.Body.Bytes())
}

// TestPre58GoldenListFormats pins GET /v1/formats' exact pre-Phase-58 body.
// convert.Default in this package's test binary carries exactly the engines
// registered by internal/convert's init()s (plus the synthetic
// fakeAVConverter registered by this package's own init() in
// handlers_test.go) -- this test does not configure or substitute the
// registry, so the snapshot reflects the real build as it exists today.
func TestPre58GoldenListFormats(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	assertGolden58(t, "formats.golden.json", rec.Body.Bytes())
}

// doPre58CreateJob posts a multipart job-create request to srv and returns
// the resulting status code and raw response body.
func doPre58CreateJob(t *testing.T, srv *Server, filename, target string, data []byte) (int, []byte) {
	t.Helper()
	body, ct := multipartBody(t, filename, target, data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// pre58Rejection is the per-case shape pinned inside
// pre58_createjob_rejections.golden.json: the HTTP status AND the raw
// response body, so both dimensions of "nothing changed on the wire" are
// checkable, not just the body.
type pre58Rejection struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// TestPre58GoldenCreateJobRejections pins the exact pre-Phase-58 status and
// body of every distinguishable content-validation rejection of
// POST /v1/jobs, in ONE golden file keyed by the reason= token handleCreateJob
// already logs for that rejection. This is the base against which Plan 58-02
// (D-04) proves that binding these rejections to the new findings vocabulary
// changes nothing on the wire. The closed list below is exactly ten cases --
// engine_size_limit is deliberately the only one with a DIFFERENT status
// (413, not 422), which is why status is pinned alongside body rather than
// assumed constant.
func TestPre58GoldenCreateJobRejections(t *testing.T) {
	type wantCase struct {
		name       string
		wantStatus int
		run        func(t *testing.T) (int, []byte)
	}

	cases := []wantCase{
		{
			name:       "zip_bomb",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				resolver := newFakeResolver()
				srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
					MaxUploadBytes:               1 << 20,
					MaxDocumentUncompressedBytes: 1 << 20, // 1 MiB test limit
				})
				return doPre58CreateJob(t, srv, "in.docx", "pdf", zipBombDocxFixture(t, 2<<20)) // declares 2 MiB uncompressed
			},
		},
		{
			name:       "macro_detected",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.docx", "pdf", macroDocxFixture(t))
			},
		},
		{
			name:       "encrypted_document",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				data, err := os.ReadFile("testdata/encrypted.docx")
				if err != nil {
					t.Fatalf("read fixture encrypted.docx: %v", err)
				}
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.docx", "pdf", data)
			},
		},
		{
			name:       "legacy_document",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				data, err := os.ReadFile("testdata/legacy.doc")
				if err != nil {
					t.Fatalf("read fixture legacy.doc: %v", err)
				}
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.doc", "pdf", data)
			},
		},
		{
			name:       "legacy_or_encrypted_document",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				// Synthetic OLE-CFB header whose directory entries classify as
				// convert.CFBUnknown (neither recognizably encrypted nor
				// recognizably legacy) -- mirrors TestCreateJob_OLECFBRejected.
				cfb := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, []byte("legacy sector padding")...)
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.doc", "pdf", cfb)
			},
		},
		{
			name:       "unrecognized_content",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				garbage := []byte("this is plain text, not a real image, matching no known magic-byte signature")
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.png", "webp", garbage)
			},
		},
		{
			name:       "mismatch",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				// Valid PNG bytes declared under a .jpg filename.
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.jpg", "webp", pngBytesFixture())
			},
		},
		{
			name:       "dimensions_unknown",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				// PNG signature, but the chunk after it is not IHDR, so
				// convert.Dimensions cannot locate the declared width/height.
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.png", "webp", truncatedIHDRPNGFixture())
			},
		},
		{
			name:       "dimension_limit",
			wantStatus: http.StatusUnprocessableEntity,
			run: func(t *testing.T) (int, []byte) {
				// PNG declaring 20000x20000 (400 megapixels), over the 100
				// megapixel default MaxImagePixels.
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				return doPre58CreateJob(t, srv, "in.png", "webp", oversizedPNGFixture())
			},
		},
		{
			name: "engine_size_limit",
			// The ONE case in this closed list with a status other than 422:
			// pinned alongside body precisely so this difference is checked,
			// not assumed away.
			wantStatus: http.StatusRequestEntityTooLarge,
			run: func(t *testing.T) (int, []byte) {
				resolver := newFakeResolver()
				srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
					MaxUploadBytes: 10 << 20,
					MaxEngineBytes: map[string]int64{convert.EngineImage: 100},
				})
				return doPre58CreateJob(t, srv, "in.png", "webp", paddedPNGFixture(500))
			},
		},
	}

	got := make(map[string]pre58Rejection, len(cases))
	for _, tc := range cases {
		status, body := tc.run(t)
		if status != tc.wantStatus {
			t.Fatalf("case %s: status = %d, want %d; body=%s", tc.name, status, tc.wantStatus, body)
		}
		got[tc.name] = pre58Rejection{Status: status, Body: json.RawMessage(body)}
	}

	// Single encoder for the whole map, same settings writeJSON uses
	// (SetEscapeHTML(false)) plus SetIndent for a readable diff. Go's
	// encoding/json sorts map[string]V keys lexicographically on encode, so
	// this file's key order is deterministic across runs without any
	// explicit sorting here.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(got); err != nil {
		t.Fatalf("encode rejections map: %v", err)
	}

	assertGolden58(t, "createjob_rejections.golden.json", buf.Bytes())
}
