package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/presets"
)

// errorScenario drives one request through the real chi router (built by
// routes.go) using the existing fake Repo/Storage/Enqueuer/PresetRepo/
// PresetAdmin harness from handlers_test.go / presets_handlers_test.go --
// no second harness is introduced here (56-03-PLAN.md Task 2).
type errorScenario struct {
	name       string
	build      func(t *testing.T) (*Server, *http.Request)
	wantStatus int
}

// errorScenarios covers at least one reachable-without-live-infrastructure
// scenario per distinct errcode the 56-03 sweep assigned: InvalidRequest
// (invalid job id, missing target format, invalid status filter),
// NotFound, UnsupportedConversion, InvalidOptions, OptsNotApplicable,
// UnknownPreset, PayloadTooLarge, InvalidCursor, MissingFile, AlreadyExists.
func errorScenarios() []errorScenario {
	return []errorScenario{
		{
			name: "invalid job id",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/not-a-uuid", nil))
				return srv, req
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "job not found",
			build: func(t *testing.T) (*Server, *http.Request) {
				repo := &fakeRepo{getErr: jobs.ErrNotFound}
				srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
				req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+uuid.New().String(), nil))
				return srv, req
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "missing target format",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				body, ct := multipartBody(t, "in.png", "", pngBytesFixture())
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				return srv, req
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing file",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				body, ct := multipartBody(t, "", "webp", nil)
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				return srv, req
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unsupported conversion pair",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				body, ct := multipartBody(t, "in.png", "mp3", pngBytesFixture())
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				return srv, req
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "invalid opts",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				body, ct := multipartBodyWithOpts(t, "in.docx", "pdf", docxFixture(t), `{"EncryptFile":true}`)
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				return srv, req
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "opts not applicable",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				body, ct := multipartBodyWithOpts(t, "in.png", "webp", pngBytesFixture(), `{"pdf_profile":"pdf/a-2b"}`)
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				return srv, req
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "unknown preset",
			build: func(t *testing.T) (*Server, *http.Request) {
				presetRepo := &fakePresetRepo{resolveErr: presets.ErrNotFound}
				srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{MaxUploadBytes: 1 << 20})
				body, ct := multipartBodyWithPreset(t, "in.png", "ghost", "", pngBytesFixture(), "")
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				return srv, req
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "oversize upload",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				big := make([]byte, (1<<20)+1024) // exceed the 1 MiB test-server limit
				body, ct := multipartBody(t, "in.png", "webp", big)
				req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
				req.Header.Set("Content-Type", ct)
				return srv, req
			},
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "invalid cursor",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?cursor=not-valid-base64!!", nil))
				return srv, req
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid status filter",
			build: func(t *testing.T) (*Server, *http.Request) {
				srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
				req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?status=bogus", nil))
				return srv, req
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "preset name collision",
			build: func(t *testing.T) (*Server, *http.Request) {
				admin := &fakePresetAdmin{createErr: presets.ErrAlreadyExists}
				srv, _ := newPresetTestServer(admin)
				req := presetJSONReq(t, http.MethodPost, "/v1/presets", map[string]any{
					"name": "thumb", "target_format": "webp",
				})
				return srv, req
			},
			wantStatus: http.StatusConflict,
		},
	}
}

// TestEveryErrorResponseCarriesKnownCode drives every scenario in
// errorScenarios through the real router and proves the closed-vocabulary
// contract SEED-020/ERRC-01 establishes: the HTTP status matches the
// pre-sweep expectation, the body decodes to {"error": non-empty string,
// "code": non-empty string}, and errcode.IsKnown(code) is true.
func TestEveryErrorResponseCarriesKnownCode(t *testing.T) {
	scenarios := errorScenarios()
	if len(scenarios) < 12 {
		t.Fatalf("only %d scenarios defined, want at least 12", len(scenarios))
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			srv, req := sc.build(t)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != sc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, sc.wantStatus, rec.Body.String())
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v; raw=%s", err, rec.Body.String())
			}

			errMsg, ok := body["error"].(string)
			if !ok || errMsg == "" {
				t.Fatalf(`body["error"] = %#v, want a non-empty string`, body["error"])
			}

			code, ok := body["code"].(string)
			if !ok || code == "" {
				t.Fatalf(`body["code"] = %#v, want a non-empty string`, body["code"])
			}
			if !errcode.IsKnown(code) {
				t.Errorf("code %q is not a member of errcode.All()", code)
			}
		})
	}
}

// TestErrorEnvelopeIsFlat asserts D-01 for one representative error
// response: body["error"] and body["code"] are both plain strings, never a
// nested object -- frontend/src/api.ts:54 reads body.error as a string, so
// {"error": {"code": ..., "message": ...}} would break every existing
// caller.
func TestErrorEnvelopeIsFlat(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/not-a-uuid", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, rec.Body.String())
	}

	switch v := body["error"].(type) {
	case string:
		if v == "" {
			t.Error(`body["error"] is an empty string`)
		}
	default:
		t.Errorf(`body["error"] is %T (%v), want a flat string -- D-01`, v, v)
	}

	switch v := body["code"].(type) {
	case string:
		if v == "" {
			t.Error(`body["code"] is an empty string`)
		}
	default:
		t.Errorf(`body["code"] is %T (%v), want a flat string -- D-01`, v, v)
	}
}

// literalErrorCodePattern flags a writeError call whose code argument is a
// bare double-quoted literal rather than an errcode.* constant: a quote
// character immediately following the status argument's trailing comma
// means a literal slipped in where a constant was required (D-02/D-03).
// [^,]+ intentionally spans newlines (Go's RE2 character-class negation is
// not restricted by dot-matches-newline), so a multi-line call whose status
// argument is followed by a line break still matches.
var literalErrorCodePattern = regexp.MustCompile(`writeError\(\s*w\s*,\s*[^,]+,\s*"`)

// stripCommentLines blanks out (but does not remove -- line numbers must
// stay aligned for accurate failure reporting) every line that is, after
// leading whitespace, a `//` comment. This is what keeps a documentation
// example inside a doc comment from spuriously failing
// TestNoLiteralErrorCodesInAPISource.
func stripCommentLines(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// scanForLiteralErrorCodes walks the non-test .go files in dir (the current
// package directory) and returns "file:line" for every writeError call
// whose code argument is a bare string literal instead of an errcode
// constant.
func scanForLiteralErrorCodes(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %q: %v", dir, err)
	}

	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		stripped := stripCommentLines(string(raw))
		for _, loc := range literalErrorCodePattern.FindAllStringIndex(stripped, -1) {
			lineNo := strings.Count(stripped[:loc[0]], "\n") + 1
			violations = append(violations, fmt.Sprintf("%s:%d", name, lineNo))
		}
	}
	return violations
}

// TestNoLiteralErrorCodesInAPISource is the static half of the closed-
// vocabulary proof: no writeError call site in non-test internal/api source
// may pass a bare string literal as its code argument (D-02/D-03). A
// hand-edit of one call site to a literal, run against this test, and
// revert was performed manually during 56-03 execution to confirm the
// regexp actually catches a regression; the observed failure message is
// recorded in 56-03-SUMMARY.md.
func TestNoLiteralErrorCodesInAPISource(t *testing.T) {
	violations := scanForLiteralErrorCodes(t, ".")
	if len(violations) > 0 {
		t.Errorf("literal error code(s) found (must reference an errcode constant instead): %v", violations)
	}
}

// TestWriteErrorSignatureCarriesCode is the tripwire against a future
// accidental revert of writeError's signature: it asserts handlers.go's
// source (comments stripped) contains the exact widened signature.
func TestWriteErrorSignatureCarriesCode(t *testing.T) {
	raw, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("read handlers.go: %v", err)
	}
	stripped := stripCommentLines(string(raw))
	const want = "func writeError(w http.ResponseWriter, status int, code, msg string)"
	if !strings.Contains(stripped, want) {
		t.Errorf("handlers.go does not contain the widened writeError signature %q -- signature reverted?", want)
	}
}
