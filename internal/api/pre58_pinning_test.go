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
// Для фазы 58 -- исключений нет. Ни один план фазы 58 не имел права изменить
// ни Go-код этой сьюты, ни байты её golden-файлов, и ни один не изменил.
//
// Фаза 60 добавила восьмой engine-класс (`probe`) и вторую операцию
// (`sanitize`), что зафиксировано роадмапом 2026-08-07. Это МЕНЯЕТ контракт
// GET /v1/formats: в ответе появляется ключ operations.sanitize. Байты
// golden-файла при этом НЕ перегенерированы -- он остаётся записью того, чем
// ответ был до фазы 58, и `git rev-list --count` для него по-прежнему 1.
// Вместо этого сузилось утверждение, и ровно на одно названное исключение:
//
//  1. Появление ключа operations.sanitize -- и ничего больше. Поддерево
//     operations.convert и ключ engines сравниваются с golden ПОБАЙТНО, как
//     и раньше, так что любое другое изменение по-прежнему валит тест.
//
// Перегенерация golden-байт остаётся запрещённой: снимок «как было до фазы
// 58» перестаёт быть таким снимком в момент, когда его переписывают.
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
	assertFormatsGolden58ModuloSanitize(t, rec.Body.Bytes())
}

// assertFormatsGolden58ModuloSanitize compares the live /v1/formats body to
// the pinned golden under the ONE exception listed at the top of this file.
//
// It splits both bodies at the top level only, using json.RawMessage, so the
// subtrees it does compare are compared as the exact bytes they were pinned
// as -- no re-encoding, no canonicalization, no chance of a formatting change
// hiding inside a semantic comparison. Then:
//
//   - engines must be byte-identical. This is the load-bearing half: probe
//     serves no convert pair, so the eighth engine class must be invisible
//     here, and that claim is what Phase 57's Classes() filter was written to
//     make true.
//   - operations.convert must be byte-identical.
//   - operations may gain "sanitize" and nothing else.
func assertFormatsGolden58ModuloSanitize(t *testing.T, got []byte) {
	t.Helper()
	const path = "testdata/pre58_formats.golden.json"

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if bytes.Equal(want, got) {
		return // still identical; nothing to explain
	}

	type topLevel struct {
		Engines    json.RawMessage            `json:"engines"`
		Operations map[string]json.RawMessage `json:"operations"`
	}
	var liveTop, goldenTop topLevel
	if err := json.Unmarshal(got, &liveTop); err != nil {
		t.Fatalf("unmarshal live body: %v", err)
	}
	if err := json.Unmarshal(want, &goldenTop); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}

	// THE SECOND PERMITTED EXCEPTION: tectonic (Phase 64, TEX-05).
	//
	// probe was the first and needed no exception HERE, because sanitize is a
	// different operation and the "engines" key walks convert only. tectonic
	// serves latex -> pdf under convert, so it genuinely appears in both maps.
	//
	// The pin is NOT regenerated for it. Regenerating would replace the
	// proposition "no pre-58 pair has moved" with "this is what the build emits
	// today", which is not a pin at all. The ninth engine is removed from the
	// LIVE body instead and the remainder must still be byte-identical, so a
	// change to any existing engine's pairs fails exactly as before and only
	// the addition is tolerated.
	liveEngines := withoutEngine(t, liveTop.Engines, convert.EngineTectonic)
	if !bytes.Equal(goldenTop.Engines, liveEngines) {
		t.Errorf("engines changed beyond the permitted addition (D-15 pinning violation)"+
			"\ngot:  %s\nwant: %s", liveEngines, goldenTop.Engines)
	}
	liveConvert := withoutEngine(t, liveTop.Operations["convert"], convert.EngineTectonic)
	if !bytes.Equal(goldenTop.Operations["convert"], liveConvert) {
		t.Errorf("operations.convert changed beyond the tectonic addition (D-15 pinning violation)"+
			"\ngot:  %s\nwant: %s", liveConvert, goldenTop.Operations["convert"])
	}
	// The addition is ASSERTED, not merely tolerated: stripping a key that was
	// never there would silently turn this back into the old check while
	// looking like it still allows an exception.
	if !bytes.Contains(liveTop.Engines, []byte(`"`+convert.EngineTectonic+`"`)) {
		t.Errorf("the live body has no %q engine -- this comparator strips one, so its absence "+
			"means the strip is hiding a regression rather than permitting an addition",
			convert.EngineTectonic)
	}
	for key := range liveTop.Operations {
		if _, pinned := goldenTop.Operations[key]; pinned {
			continue
		}
		if key != "sanitize" {
			t.Errorf("operations gained key %q, which is outside the single permitted exception "+
				"listed at the top of this file", key)
		}
	}
	for key := range goldenTop.Operations {
		if _, still := liveTop.Operations[key]; !still {
			t.Errorf("operations lost pinned key %q (D-15 pinning violation)", key)
		}
	}
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

// withoutEngine re-encodes an engine-keyed map with one key removed, in the
// canonical form the handler emits, so what remains can be compared byte for
// byte against the pin.
func withoutEngine(t *testing.T, raw json.RawMessage, engine string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal engine-keyed map: %v", err)
	}
	delete(m, engine)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		t.Fatalf("re-encode engine-keyed map: %v", err)
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n"))
}
