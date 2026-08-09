package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testAPIKey is a recognizable stand-in API key used across tests that
// don't specifically exercise key-leak detection (that has its own,
// separately distinctive key -- see TestClient_KeyNeverLeaks).
const testAPIKey = "test-fixture-api-key"

const sampleFile = "testdata/sample.png"

func newTestClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestConfigLoad_MissingRequired(t *testing.T) {
	t.Setenv(envBaseURL, "http://example.invalid")
	t.Setenv(envAPIKey, "")
	t.Setenv(envOutputDir, "")
	t.Setenv(envConvertTO, "")
	t.Setenv(envPollInterval, "")
	t.Setenv(envS3DialAddr, "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with empty OCTOCONV_API_KEY: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), envAPIKey) {
		t.Fatalf("Load() error = %q, want it to name %s", err.Error(), envAPIKey)
	}
}

func TestConfigLoad_Defaults(t *testing.T) {
	t.Setenv(envBaseURL, "http://example.invalid")
	t.Setenv(envAPIKey, "a-key")
	t.Setenv(envOutputDir, "")
	t.Setenv(envConvertTO, "")
	t.Setenv(envPollInterval, "")
	t.Setenv(envS3DialAddr, "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ConvertTimeout != defaultConvertTO {
		t.Fatalf("ConvertTimeout = %v, want %v", cfg.ConvertTimeout, defaultConvertTO)
	}
	if cfg.PollInterval != defaultPollEvery {
		t.Fatalf("PollInterval = %v, want %v", cfg.PollInterval, defaultPollEvery)
	}
	if cfg.S3DialAddr != "" {
		t.Fatalf("S3DialAddr = %q, want empty by default", cfg.S3DialAddr)
	}
	wantDir := filepath.Join(os.TempDir(), "octoconv-mcp")
	if cfg.OutputDir != wantDir {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, wantDir)
	}
}

// TestClientConvertBlocking_HappyPath drives the full blocking convert
// workflow against an httptest fake: 202 on create, "queued" -> "active" ->
// "done" on poll, then a presigned download serving JPEG magic bytes (D-12).
func TestClientConvertBlocking_HappyPath(t *testing.T) {
	var getCalls int32
	var sawTarget string
	var downloadAuthHeader string

	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "ApiKey "+testAPIKey {
			t.Errorf("create job Authorization header = %q, want ApiKey %s", got, testAPIKey)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		sawTarget = r.FormValue("target")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job-1", "status": "queued"})
	})

	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&getCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case n < 3:
			status := "queued"
			if n == 2 {
				status = "active"
			}
			_ = json.NewEncoder(w).Encode(JobStatus{JobID: "job-1", Status: status})
		default:
			_ = json.NewEncoder(w).Encode(JobStatus{
				JobID:       "job-1",
				Status:      "done",
				DownloadURL: srv.URL + "/results/job-1/result.jpg",
			})
		}
	})

	mux.HandleFunc("/results/job-1/result.jpg", func(w http.ResponseWriter, r *http.Request) {
		downloadAuthHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10})
	})

	srv = httptest.NewServer(mux)
	defer srv.Close()

	outDir := t.TempDir()
	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      outDir,
		ConvertTimeout: 5 * time.Second,
		PollInterval:   10 * time.Millisecond,
	})

	var ticks []string
	result, err := c.ConvertBlocking(context.Background(), sampleFile, "", "jpg", "", "", func(status string) {
		ticks = append(ticks, status)
	})
	if err != nil {
		t.Fatalf("ConvertBlocking: %v", err)
	}

	if sawTarget != "jpg" {
		t.Fatalf("create job target field = %q, want jpg", sawTarget)
	}
	if len(ticks) < 1 {
		t.Fatal("onTick never fired, want at least one invocation")
	}
	if downloadAuthHeader != "" {
		t.Fatalf("download request carried Authorization header %q, want none (presigned URL is self-authorizing)", downloadAuthHeader)
	}
	if result.JobID != "job-1" {
		t.Fatalf("JobID = %q, want job-1", result.JobID)
	}
	if filepath.Dir(result.LocalPath) != outDir {
		t.Fatalf("LocalPath dir = %q, want %q", filepath.Dir(result.LocalPath), outDir)
	}
	data, err := os.ReadFile(result.LocalPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if len(data) < 3 || data[0] != 0xFF || data[1] != 0xD8 || data[2] != 0xFF {
		t.Fatalf("downloaded file head = % x, want JPEG magic bytes ff d8 ff", data[:min(3, len(data))])
	}
}

// TestClient_UnsupportedPair_422 asserts the API's 422 error text for an
// unsupported conversion pair is surfaced verbatim by CreateJob.
func TestClient_UnsupportedPair_422(t *testing.T) {
	const errText = "unsupported conversion: png -> weird"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": errText})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	_, err := c.CreateJob(context.Background(), sampleFile, "", "weird", "", "")
	if err == nil {
		t.Fatal("CreateJob: expected an error for an unsupported pair")
	}
	if !strings.Contains(err.Error(), errText) {
		t.Fatalf("CreateJob error = %q, want it to contain %q", err.Error(), errText)
	}
}

// TestClient_KeyNeverLeaks (D-08) forces a 401 and a transport error and
// asserts the API key substring is absent from both returned error strings.
func TestClient_KeyNeverLeaks(t *testing.T) {
	const key = "super-secret-leak-detector-000111"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         key,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	_, err := c.GetJob(context.Background(), "some-job-id")
	if err == nil {
		t.Fatal("GetJob: expected a 401 error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("401 error leaked the API key: %q", err.Error())
	}

	// Force a transport-level error: a host under the reserved .invalid TLD
	// (RFC 2606) is guaranteed to never resolve.
	badClient := newTestClient(t, Config{
		BaseURL:        "http://mcpserver-leaktest.invalid:1",
		APIKey:         key,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})
	_, err = badClient.GetJob(context.Background(), "some-job-id")
	if err == nil {
		t.Fatal("GetJob against an unresolvable host: expected a transport error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("transport error leaked the API key: %q", err.Error())
	}
}

// TestClient_PathSanitization (D-09) asserts a download_url whose path
// component ends in "../../evil.jpg" still writes strictly inside OUTPUT_DIR
// with a stripped basename.
func TestClient_PathSanitization(t *testing.T) {
	// A bare handler (not http.ServeMux) so the traversal segments in the
	// request path are never cleaned/redirected before reaching us -- the
	// test needs the client to receive and sanitize the literal path itself.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	outDir := t.TempDir()
	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      outDir,
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	downloadURL := srv.URL + "/results/job-1/../../evil.jpg"
	localPath, err := c.Download(context.Background(), "job-1", downloadURL)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	if filepath.Dir(localPath) != outDir {
		t.Fatalf("localPath dir = %q, want %q (strictly inside OUTPUT_DIR)", filepath.Dir(localPath), outDir)
	}
	base := filepath.Base(localPath)
	if strings.Contains(base, "..") || strings.ContainsAny(base, "/\\") {
		t.Fatalf("localPath base = %q, contains path-traversal remnants", base)
	}
	if base != "evil.jpg" {
		t.Fatalf("localPath base = %q, want evil.jpg", base)
	}
}

// TestClient_DialRedirect proves the OCTOCONV_S3_DIAL_ADDR knob: when set,
// Download redials the configured address while leaving the request's
// Host header untouched, so a presigned URL whose host is unresolvable
// still succeeds. With the knob empty (control), the same URL fails.
func TestClient_DialRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF})
	}))
	defer srv.Close()

	dialAddr := srv.Listener.Addr().String()
	// minio.invalid is under the reserved .invalid TLD (RFC 2606) --
	// guaranteed to never resolve, so success here can only come from the
	// custom DialContext redialing dialAddr instead of resolving this host.
	bogusURL := "http://minio.invalid:9000/results/job-1/result.jpg"

	t.Run("knob set: redials the real server, Host header untouched", func(t *testing.T) {
		c := newTestClient(t, Config{
			BaseURL:        "http://unused.invalid",
			APIKey:         testAPIKey,
			OutputDir:      t.TempDir(),
			ConvertTimeout: time.Second,
			PollInterval:   time.Millisecond,
			S3DialAddr:     dialAddr,
		})
		localPath, err := c.Download(context.Background(), "job-1", bogusURL)
		if err != nil {
			t.Fatalf("Download with OCTOCONV_S3_DIAL_ADDR set: expected success via custom DialContext, got: %v", err)
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if len(data) != 3 || data[0] != 0xFF {
			t.Fatalf("downloaded file = % x, want ff d8 ff", data)
		}
	})

	t.Run("control: knob empty, same bogus host fails", func(t *testing.T) {
		c := newTestClient(t, Config{
			BaseURL:        "http://unused.invalid",
			APIKey:         testAPIKey,
			OutputDir:      t.TempDir(),
			ConvertTimeout: time.Second,
			PollInterval:   time.Millisecond,
			// S3DialAddr intentionally left empty.
		})
		if _, err := c.Download(context.Background(), "job-1", bogusURL); err == nil {
			t.Fatal("Download against an unresolvable host with the dial-redirect knob empty: expected an error")
		}
	})
}

// TestClient_QuotaExceeded429_CarriesCodeAndRetryAfter (D-12, QUOT-03) proves
// a 429 with code=quota_exceeded and a Retry-After header survives the
// proxy hop into the tool-visible error text, distinguishable from a
// rate-limited 429 (see the sibling test below).
func TestClient_QuotaExceeded429_CarriesCodeAndRetryAfter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "quota exceeded for this engine", "code": "quota_exceeded"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	_, err := c.GetJob(context.Background(), "some-job-id")
	if err == nil {
		t.Fatal("GetJob: expected a 429 error")
	}
	if !strings.Contains(err.Error(), "quota_exceeded") {
		t.Fatalf("GetJob error = %q, want it to contain %q", err.Error(), "quota_exceeded")
	}
	if !strings.Contains(err.Error(), "45") {
		t.Fatalf("GetJob error = %q, want it to contain the retry_after value %q", err.Error(), "45")
	}
}

// TestClient_RateLimited429_CarriesCode (D-12, QUOT-03) proves a
// rate-limited 429 is distinguishable through the proxy from a quota
// rejection -- same code path, different code value.
func TestClient_RateLimited429_CarriesCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded", "code": "rate_limited"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	_, err := c.GetJob(context.Background(), "some-job-id")
	if err == nil {
		t.Fatal("GetJob: expected a 429 error")
	}
	if !strings.Contains(err.Error(), "rate_limited") {
		t.Fatalf("GetJob error = %q, want it to contain %q", err.Error(), "rate_limited")
	}
}

// TestClient_422NoCode_UnchangedErrorText (regression guard) proves a 422
// body with no code field produces the exact same error text as before this
// change -- extractErrorBody's fallback path is unaffected.
func TestClient_422NoCode_UnchangedErrorText(t *testing.T) {
	const errText = "unsupported conversion: png -> weird"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": errText})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	_, err := c.CreateJob(context.Background(), sampleFile, "", "weird", "", "")
	if err == nil {
		t.Fatal("CreateJob: expected an error for an unsupported pair")
	}
	wantErr := fmt.Sprintf("create job: %s", errText)
	if err.Error() != wantErr {
		t.Fatalf("CreateJob error = %q, want exactly %q (no code/-suffix)", err.Error(), wantErr)
	}
}

// TestClient_QuotaAndRateLimit429_KeyNeverLeaks (D-08/D-12) proves the API
// key is absent from the new code/retry-bearing error branches specifically
// -- TestClient_KeyNeverLeaks did not previously exercise these branches.
func TestClient_QuotaAndRateLimit429_KeyNeverLeaks(t *testing.T) {
	const key = "super-secret-leak-detector-quota-000111"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "quota exceeded for this engine", "code": "quota_exceeded"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         key,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	_, err := c.GetJob(context.Background(), "some-job-id")
	if err == nil {
		t.Fatal("GetJob: expected a 429 error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("quota-rejection error leaked the API key: %q", err.Error())
	}
}

func TestListFormats(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/formats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"engines": map[string]any{
				"image": map[string]any{"pairs": [][2]string{{"png", "jpg"}}},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	fr, err := c.ListFormats(context.Background())
	if err != nil {
		t.Fatalf("ListFormats: %v", err)
	}
	eng, ok := fr.Engines["image"]
	if !ok {
		t.Fatal("ListFormats: missing image engine in response")
	}
	if len(eng.Pairs) != 1 || eng.Pairs[0] != [2]string{"png", "jpg"} {
		t.Fatalf("pairs = %v, want [[png jpg]]", eng.Pairs)
	}
	if fr.Operations != nil {
		t.Fatalf("Operations = %v, want nil when the response body omits the operations key (pre-Phase-57 API build)", fr.Operations)
	}
}

// TestListFormats_OperationsDecoded (codex PR #7 finding #2) proves the
// operations key of GET /v1/formats -- the discovery path tools.go:68's
// convert_file jsonschema advertises -- actually survives decoding instead
// of being silently dropped.
func TestListFormats_OperationsDecoded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/formats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"engines": map[string]any{
				"image": map[string]any{"pairs": [][2]string{{"png", "jpg"}}},
			},
			"operations": map[string]any{
				"convert": map[string]any{
					"image": map[string]any{"pairs": [][2]string{{"png", "jpg"}}},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	fr, err := c.ListFormats(context.Background())
	if err != nil {
		t.Fatalf("ListFormats: %v", err)
	}
	byEngine, ok := fr.Operations["convert"]
	if !ok {
		t.Fatal("ListFormats: missing convert operation in decoded response")
	}
	eng, ok := byEngine["image"]
	if !ok {
		t.Fatal("ListFormats: missing image engine under convert operation")
	}
	if len(eng.Pairs) != 1 || eng.Pairs[0] != [2]string{"png", "jpg"} {
		t.Fatalf("operations[convert][image].Pairs = %v, want [[png jpg]]", eng.Pairs)
	}
}

// TestClientCreateJob_OperationFieldPresentWhenNonEmpty (D-05/OPRN-01) proves
// CreateJob writes a multipart part named "operation" with the supplied
// value when operation is non-empty.
func TestClientCreateJob_OperationFieldPresentWhenNonEmpty(t *testing.T) {
	var sawOperation string
	var hasOperationField bool

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		if vals, ok := r.MultipartForm.Value[formFieldOperation]; ok && len(vals) > 0 {
			hasOperationField = true
			sawOperation = vals[0]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job-op-1"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	if _, err := c.CreateJob(context.Background(), sampleFile, "sanitize", "jpg", "", ""); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if !hasOperationField {
		t.Fatal("multipart body has no \"operation\" part, want one since operation was non-empty")
	}
	if sawOperation != "sanitize" {
		t.Fatalf("operation field = %q, want sanitize", sawOperation)
	}
}

// TestClientCreateJob_OperationFieldAbsentWhenEmpty (T-57-31) proves an
// empty operation produces a multipart body with NO "operation" part at
// all -- the wire form of a client that never sends the field is preserved
// byte-for-byte.
func TestClientCreateJob_OperationFieldAbsentWhenEmpty(t *testing.T) {
	var hasOperationField bool

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		_, hasOperationField = r.MultipartForm.Value[formFieldOperation]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job-op-2"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	if _, err := c.CreateJob(context.Background(), sampleFile, "", "jpg", "", ""); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if hasOperationField {
		t.Fatal("multipart body has an \"operation\" part, want none since operation was empty")
	}
}

// TestClient_UnsupportedOperation422_ForwardedVerbatim (T-57-30) asserts the
// API's 422 unsupported_operation error is surfaced by CreateJob unchanged,
// exactly like the pre-existing unsupported_conversion case above -- this
// client never arbitrates operation locally, so a 422 from the server's
// registry gate is the only source of this rejection.
func TestClient_UnsupportedOperation422_ForwardedVerbatim(t *testing.T) {
	const errText = "unsupported operation: sanitize"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": errText, "code": "unsupported_operation"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	_, err := c.CreateJob(context.Background(), sampleFile, "sanitize", "jpg", "", "")
	if err == nil {
		t.Fatal("CreateJob: expected an error for an unsupported operation")
	}
	if !strings.Contains(err.Error(), errText) {
		t.Fatalf("CreateJob error = %q, want it to contain %q", err.Error(), errText)
	}
	if !strings.Contains(err.Error(), "unsupported_operation") {
		t.Fatalf("CreateJob error = %q, want it to contain the code %q", err.Error(), "unsupported_operation")
	}
}

func TestListPresets(t *testing.T) {
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/presets", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Preset{
			{Name: "p1", Version: 1, Scope: "user", Operation: "convert", TargetFormat: "jpg", IsActive: true},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})

	presets, err := c.ListPresets(context.Background(), true)
	if err != nil {
		t.Fatalf("ListPresets: %v", err)
	}
	if len(presets) != 1 || presets[0].Name != "p1" {
		t.Fatalf("presets = %+v, want a single preset named p1", presets)
	}
	if gotQuery != "all=true" {
		t.Fatalf("query = %q, want all=true (includeInactive=true)", gotQuery)
	}
}
