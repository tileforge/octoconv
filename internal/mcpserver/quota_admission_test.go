package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/apaderin/octoconv/internal/api"
	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/db"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/metrics"
	"github.com/apaderin/octoconv/internal/presets"
)

// TestQuotaAdmission is the ROADMAP SC2 cross-entry-point admission proof
// (QUOT-02) and the FAIR-01/FAIR-02-adjacent per-client fairness proof, all
// driven through ONE live api.Server instance -- the real router, the real
// auth.Middleware chain, and the real jobs repository's admission gate
// (admitLocked/PreAdmit from Plan 52-03), never a fake. It lives in package
// mcpserver specifically so it can reuse newHarness/resultText from
// tools_test.go/client_test.go and drive a genuine in-memory MCP session
// (D-09/D-11) against that same server. There is no import cycle:
// internal/mcpserver's non-test files import no internal package, so this
// test file importing internal/api, internal/auth, internal/clients,
// internal/jobs, internal/metrics and internal/presets creates no cycle.
func TestQuotaAdmission(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	h := newQuotaAdmissionHarness(t)

	// sharedClientID/sharedRawKey are set by RESTRejectsOverCap and reused by
	// MCPToolRejectsOverSameCap -- the whole point of subtest 2 is that the
	// SAME over-quota client, still holding subtest 1's one in-flight job, is
	// rejected identically through a different entry point.
	var sharedClientID uuid.UUID
	var sharedRawKey string

	t.Run("RESTRejectsOverCap", func(t *testing.T) {
		clientID, rawKey := h.newClient(t)
		sharedClientID, sharedRawKey = clientID, rawKey
		h.setMode(t, clientID, enforcementModeEnforceLiteral)
		h.setLimits(t, clientID, "image", intPtr(1))

		uploadsBefore := h.storage.count()
		enqueuesBefore := h.queue.count()

		resp1 := h.mustCreateJob(t, rawKey, "webp")
		if resp1.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(resp1.Body)
			resp1.Body.Close()
			t.Fatalf("first POST /v1/jobs status = %d, want 202; body=%s", resp1.StatusCode, body)
		}
		resp1.Body.Close()

		resp2 := h.mustCreateJob(t, rawKey, "webp")
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusTooManyRequests {
			body, _ := io.ReadAll(resp2.Body)
			t.Fatalf("second POST /v1/jobs status = %d, want exactly 429; body=%s", resp2.StatusCode, body)
		}
		retryAfter := resp2.Header.Get("Retry-After")
		n, err := strconv.Atoi(retryAfter)
		if err != nil || n <= 0 {
			t.Fatalf("second POST Retry-After = %q, want a positive integer", retryAfter)
		}
		eb := decodeErrorBody(t, resp2)
		if eb.Code != "quota_exceeded" {
			t.Fatalf("second POST body code = %q, want quota_exceeded", eb.Code)
		}

		// ROADMAP SC2b: the rejected second request must never reach
		// s.storage.Upload -- exactly one upload across both requests.
		uploadsAfter := h.storage.count()
		if uploadsAfter-uploadsBefore != 1 {
			t.Fatalf("upload count across both requests = %d, want exactly 1 (the second must be rejected before S3)", uploadsAfter-uploadsBefore)
		}
		enqueuesAfter := h.queue.count()
		if enqueuesAfter-enqueuesBefore != 1 {
			t.Fatalf("enqueue count across both requests = %d, want exactly 1", enqueuesAfter-enqueuesBefore)
		}
	})

	t.Run("MCPToolRejectsOverSameCap", func(t *testing.T) {
		pngPath := filepath.Join(t.TempDir(), "in.png")
		if err := os.WriteFile(pngPath, quotaAdmissionPNGFixture(), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		mh := newHarness(t, Config{
			BaseURL:        h.srv.URL,
			APIKey:         sharedRawKey,
			OutputDir:      t.TempDir(),
			ConvertTimeout: 5 * time.Second,
			PollInterval:   10 * time.Millisecond,
		}, nil)

		res, err := mh.cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "convert_file",
			Arguments: map[string]any{"path": pngPath, "target_format": "webp"},
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if !res.IsError {
			t.Fatalf("IsError = false, want true; content = %v", res.Content)
		}

		// This is ROADMAP SC2's "exercise at least one MCP path, not assumed
		// from proxying": the MCP server, its HTTP client, the real router,
		// the real auth chain and the real admission gate are all in the
		// call path -- the SAME sharedClientID/sharedRawKey that just hit
		// its cap over REST in the previous subtest.
		text := resultText(t, res)
		if !strings.Contains(text, "quota_exceeded") {
			t.Fatalf("result text = %q, want it to contain quota_exceeded", text)
		}
		if !strings.Contains(text, "retry_after") {
			t.Fatalf("result text = %q, want it to contain retry_after", text)
		}
		if strings.Contains(text, sharedRawKey) {
			t.Fatal("result text leaked the raw API key")
		}
	})

	t.Run("ObserveModeAdmitsBothPaths", func(t *testing.T) {
		clientID, rawKey := h.newClient(t)
		h.setMode(t, clientID, enforcementModeObserveLiteral)
		// The strictest possible limit -- zero concurrent jobs allowed.
		h.setLimits(t, clientID, "image", intPtr(0))

		before := metrics.QuotaWouldRejectValue(clientID.String(), "image", "concurrency")

		resp := h.mustCreateJob(t, rawKey, "webp")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("observe-mode POST status = %d, want 202 (observe mode must admit despite max_concurrent=0); body=%s", resp.StatusCode, body)
		}

		// QUOT-04 rollout proof: a limit that would reject everything
		// rejects nothing while still being counted -- exactly one
		// would-reject increment for this client/engine/reason.
		after := metrics.QuotaWouldRejectValue(clientID.String(), "image", "concurrency")
		if after-before != 1 {
			t.Fatalf("octoconv_quota_would_reject_total delta = %v, want exactly 1", after-before)
		}
	})

	t.Run("FairnessGuaranteeIsPerClientNotGlobal", func(t *testing.T) {
		// sharedClientID (from RESTRejectsOverCap) is still sitting at its
		// max_concurrent=1 cap with one queued job -- nothing in this test
		// has moved it to a terminal state. A SECOND, independent client
		// capped identically at max_concurrent=1 must still be admitted:
		// FAIR-01 in the ROADMAP's own concrete form -- at cap K and
		// maxReplicaCount M, a client occupies at most K of M slots, leaving
		// M-K reachable by others. Here K=1: sharedClientID occupies its one
		// slot, and this second client's own, independent K=1 budget is
		// untouched by sharedClientID's occupancy -- the cap bounds ONE
		// client, it is not a global admission throttle.
		clientID2, rawKey2 := h.newClient(t)
		h.setMode(t, clientID2, enforcementModeEnforceLiteral)
		h.setLimits(t, clientID2, "image", intPtr(1))

		resp := h.mustCreateJob(t, rawKey2, "webp")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("second client's POST status = %d, want 202 -- sharedClientID (%s) sitting at its own cap must not block an independent client; body=%s", resp.StatusCode, sharedClientID, body)
		}
	})

	t.Run("ConcurrentBurstBoundedUploadWaste", func(t *testing.T) {
		clientID, rawKey := h.newClient(t)
		h.setMode(t, clientID, enforcementModeEnforceLiteral)
		h.setLimits(t, clientID, "image", intPtr(1)) // K = 1, no in-flight job yet.

		const n = 8 // N: burst size
		const k = 1 // K: the concurrency cap under test

		uploadsBefore := h.storage.count()

		type burstResult struct {
			resp *http.Response
			err  error
		}
		results := make(chan burstResult, n)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				resp, err := h.createJob(rawKey, "webp")
				results <- burstResult{resp: resp, err: err}
			}()
		}
		close(start)
		wg.Wait()
		close(results)

		var admitted, rejected int
		for br := range results {
			if br.err != nil {
				t.Fatalf("burst POST /v1/jobs: %v", br.err)
			}
			switch br.resp.StatusCode {
			case http.StatusAccepted:
				admitted++
				br.resp.Body.Close()
			case http.StatusTooManyRequests:
				rejected++
				retryAfter := br.resp.Header.Get("Retry-After")
				rn, err := strconv.Atoi(retryAfter)
				if err != nil || rn <= 0 {
					t.Errorf("burst reject Retry-After = %q, want a positive integer", retryAfter)
				}
				eb := decodeErrorBody(t, br.resp)
				if eb.Code != "quota_exceeded" {
					t.Errorf("burst reject code = %q, want quota_exceeded", eb.Code)
				}
			default:
				body, _ := io.ReadAll(br.resp.Body)
				br.resp.Body.Close()
				t.Fatalf("unexpected burst response status %d, body=%s (the rate limiter is configured at 10000 RPM specifically so this must be impossible)", br.resp.StatusCode, body)
			}
		}

		// The authoritative guarantee -- the load-bearing FAIR-01/QUOT-02
		// property, asserted as exact integers, not a range.
		if admitted != k {
			t.Fatalf("admitted = %d, want exactly %d", admitted, k)
		}
		if rejected != n-k {
			t.Fatalf("rejected = %d, want exactly %d", rejected, n-k)
		}

		var queuedActive int
		if err := h.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM jobs WHERE client_id = $1 AND engine = 'image' AND status IN ('queued','active')`,
			clientID,
		).Scan(&queuedActive); err != nil {
			t.Fatalf("count queued/active jobs: %v", err)
		}
		if queuedActive != 1 {
			t.Fatalf("queued+active job row count = %d, want exactly 1", queuedActive)
		}

		// The residual, measured rather than assumed. PreAdmit is unlocked
		// by design (D-07/D-15), so every request that reads a below-cap
		// count before any sibling commits its insert will upload and only
		// then be rejected at admitLocked; the waste per burst is therefore
		// at most N - K uploads, (N - K) x MAX_INPUT_BYTES bytes, with N
		// bounded by the upstream per-client rate limiter. This bound is
		// looser than D-15's: D-15's K is a small, phase-owned value this
		// very admission mechanism enforces structurally, whereas N's
		// ceiling is internal/ratelimit's operator-configured fixed-window
		// RPM counter -- a different package's independently-tuned knob
		// that is not a burst or concurrency control, so N can exceed K by
		// orders of magnitude. The framing is D-15's (disclose, bound,
		// measure); the tightness is not.
		uploadsAfter := h.storage.count()
		wasted := uploadsAfter - uploadsBefore - 1 // the admitted job's own upload is not waste
		if wasted < 0 {
			t.Fatalf("wasted = %d, want >= 0", wasted)
		}
		if wasted > n-k {
			t.Fatalf("wasted = %d, want <= N - K = %d", wasted, n-k)
		}
		t.Logf("ConcurrentBurstBoundedUploadWaste: observed wasted uploads = %d (bound N - K = %d)", wasted, n-k)

		// The steady state -- ROADMAP SC2b's actual property: with the
		// client now at its cap, a further sequential retry must waste
		// exactly zero bytes; the burst waste above is a one-time
		// transient at the cap boundary, not a repeatable drain.
		uploadsBeforeRetry := h.storage.count()
		retryResp := h.mustCreateJob(t, rawKey, "webp")
		defer retryResp.Body.Close()
		if retryResp.StatusCode != http.StatusTooManyRequests {
			body, _ := io.ReadAll(retryResp.Body)
			t.Fatalf("post-burst retry status = %d, want 429; body=%s", retryResp.StatusCode, body)
		}
		eb := decodeErrorBody(t, retryResp)
		if eb.Code != "quota_exceeded" {
			t.Fatalf("post-burst retry code = %q, want quota_exceeded", eb.Code)
		}
		uploadsAfterRetry := h.storage.count()
		if uploadsAfterRetry != uploadsBeforeRetry {
			t.Fatalf("post-burst retry moved the upload counter by %d, want exactly 0", uploadsAfterRetry-uploadsBeforeRetry)
		}
	})
}

// --- harness ---

// enforcementModeEnforceLiteral/enforcementModeObserveLiteral mirror the
// unexported jobs package's own enforcementModeEnforce/enforcementModeObserve
// constants (internal/jobs/admission.go) -- this test package cannot import
// them, so the two CHECK-constrained literal values are duplicated here,
// exactly as internal/jobs/admission_test.go's own setEnforcementMode helper
// writes them.
const (
	enforcementModeEnforceLiteral = "enforce"
	enforcementModeObserveLiteral = "observe"
)

// quotaAdmissionHarness bundles one real api.Server (the real jobs
// repository is the actual admission gate under test, never a fake) exposed
// over httptest.NewServer, plus the counting Storage/Enqueuer stubs and the
// Postgres pool tests read state from directly.
type quotaAdmissionHarness struct {
	pool       *pgxpool.Pool
	salt       []byte
	clientsAPI *clients.Repo
	srv        *httptest.Server
	httpClient *http.Client
	storage    *quotaCountingStorage
	queue      *quotaCountingEnqueuer
}

// newQuotaAdmissionHarness builds the harness described in the plan: connect
// + migrate, provision auth, build the real API server with the real jobs
// repository plus local counting stubs for Storage/Enqueuer, and serve it
// over httptest.NewServer with rate limits raised high enough that
// internal/ratelimit can never emit a competing 429.
func newQuotaAdmissionHarness(t *testing.T) *quotaAdmissionHarness {
	t.Helper()
	ctx := context.Background()

	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	t.Cleanup(pool.Close)

	salt := []byte("quota-admission-test-salt")
	clientsRepo := clients.NewRepo(pool)
	resolver := auth.NewResolver(clientsRepo, salt)

	jobsRepo := jobs.NewRepo(pool)
	jobsRepo.SetQuotaObserver(func(clientID uuid.UUID, engine, reason string) {
		metrics.RecordQuotaWouldReject(clientID.String(), engine, reason)
	})

	store := &quotaCountingStorage{}
	queue := &quotaCountingEnqueuer{}

	apiSrv := api.NewServer(
		jobsRepo, store, queue,
		quotaAdmissionPresetRepo{}, nil,
		jobsRepo, clientsRepo,
		resolver, api.HealthDeps{},
		api.Config{
			// Raised so the coarse pre-auth/per-client rate limiter
			// (internal/ratelimit) can never itself produce a 429 that
			// would be mistaken for a quota rejection in these tests.
			IPRateLimitRPM:     10000,
			ClientRateLimitRPM: 10000,
		},
	)

	srv := httptest.NewServer(apiSrv.Routes())
	t.Cleanup(srv.Close)

	return &quotaAdmissionHarness{
		pool:       pool,
		salt:       salt,
		clientsAPI: clientsRepo,
		srv:        srv,
		httpClient: &http.Client{},
		storage:    store,
		queue:      queue,
	}
}

// newClient provisions one fresh client row + minted API key. A fresh
// client id per subtest keeps repeated -count=1 runs deterministic.
func (h *quotaAdmissionHarness) newClient(t *testing.T) (clientID uuid.UUID, rawKey string) {
	t.Helper()
	raw, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("auth.GenerateKey: %v", err)
	}
	hash := auth.HashKey(h.salt, raw)
	id, err := h.clientsAPI.Create(context.Background(), "quota-admission-"+uuid.NewString(), hash)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return id, raw
}

// setLimits writes a client_limits row directly via SQL, mirroring
// internal/clients/limits.go's SetLimit semantics.
func (h *quotaAdmissionHarness) setLimits(t *testing.T, clientID uuid.UUID, engine string, maxConcurrent *int) {
	t.Helper()
	_, err := h.pool.Exec(context.Background(), `
		INSERT INTO client_limits (client_id, engine, max_concurrent)
		VALUES ($1, $2, $3)
		ON CONFLICT (client_id, engine) DO UPDATE SET max_concurrent = EXCLUDED.max_concurrent`,
		clientID, engine, maxConcurrent)
	if err != nil {
		t.Fatalf("set client limits: %v", err)
	}
}

// setMode writes clients.enforcement_mode directly via SQL.
func (h *quotaAdmissionHarness) setMode(t *testing.T, clientID uuid.UUID, mode string) {
	t.Helper()
	_, err := h.pool.Exec(context.Background(), `UPDATE clients SET enforcement_mode = $2 WHERE id = $1`, clientID, mode)
	if err != nil {
		t.Fatalf("set enforcement mode: %v", err)
	}
}

// createJob POSTs a minimal PNG multipart request to /v1/jobs, returning the
// raw *http.Response (or an error) rather than failing the test directly --
// safe to call concurrently from goroutines (ConcurrentBurstBoundedUploadWaste),
// unlike a t.Fatalf-calling helper.
func (h *quotaAdmissionHarness) createJob(rawKey, target string) (*http.Response, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", "in.png")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(quotaAdmissionPNGFixture()); err != nil {
		return nil, fmt.Errorf("write file part: %w", err)
	}
	if err := w.WriteField("target", target); err != nil {
		return nil, fmt.Errorf("write target field: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/jobs", &b)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "ApiKey "+rawKey)
	return h.httpClient.Do(req)
}

// mustCreateJob is createJob plus t.Fatalf on a transport-level error --
// used from the main test goroutine only (never from the burst subtest's
// spawned goroutines).
func (h *quotaAdmissionHarness) mustCreateJob(t *testing.T, rawKey, target string) *http.Response {
	t.Helper()
	resp, err := h.createJob(rawKey, target)
	if err != nil {
		t.Fatalf("POST /v1/jobs: %v", err)
	}
	return resp
}

// quotaCountingStorage implements api.Storage, counting Upload calls so
// tests can assert an exact upload count rather than inferring one from a
// boolean. Every method is mutex-guarded for -race safety under the
// concurrent-burst subtest.
type quotaCountingStorage struct {
	mu    sync.Mutex
	calls int
}

func (s *quotaCountingStorage) Upload(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	if _, err := io.Copy(io.Discard, r); err != nil {
		return err
	}
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return nil
}

func (s *quotaCountingStorage) PresignGet(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "https://quota-admission-test.invalid/presigned", nil
}

func (s *quotaCountingStorage) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// quotaCountingEnqueuer implements api.Enqueuer, counting every Enqueue*
// invocation across all seven engine-class methods behind one mutex-guarded
// counter -- this test only ever exercises the image engine, but the
// interface requires all seven.
type quotaCountingEnqueuer struct {
	mu    sync.Mutex
	calls int
}

func (e *quotaCountingEnqueuer) inc() error {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	return nil
}

func (e *quotaCountingEnqueuer) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *quotaCountingEnqueuer) EnqueueImageConvert(_ context.Context, _ uuid.UUID) error {
	return e.inc()
}
func (e *quotaCountingEnqueuer) EnqueueDocumentConvert(_ context.Context, _ uuid.UUID) error {
	return e.inc()
}
func (e *quotaCountingEnqueuer) EnqueueHTMLConvert(_ context.Context, _ uuid.UUID) error {
	return e.inc()
}
func (e *quotaCountingEnqueuer) EnqueueAudioConvert(_ context.Context, _ uuid.UUID) error {
	return e.inc()
}
func (e *quotaCountingEnqueuer) EnqueueAVConvert(_ context.Context, _ uuid.UUID) error {
	return e.inc()
}
func (e *quotaCountingEnqueuer) EnqueueOCRConvert(_ context.Context, _ uuid.UUID) error {
	return e.inc()
}
func (e *quotaCountingEnqueuer) EnqueuePandocConvert(_ context.Context, _ uuid.UUID) error {
	return e.inc()
}

// quotaAdmissionPresetRepo implements api.PresetRepo, always reporting
// presets.ErrNotFound -- this test never exercises preset=<name> resolution.
type quotaAdmissionPresetRepo struct{}

func (quotaAdmissionPresetRepo) Resolve(_ context.Context, _ uuid.UUID, _ string) (*presets.Preset, error) {
	return nil, presets.ErrNotFound
}

// quotaAdmissionErrorBody is the REST 429/error JSON shape
// ({"error", "code"}) this test decodes -- never asserted on error text
// directly, only on the structured fields.
type quotaAdmissionErrorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// decodeErrorBody decodes and closes resp.Body as a quotaAdmissionErrorBody.
func decodeErrorBody(t *testing.T, resp *http.Response) quotaAdmissionErrorBody {
	t.Helper()
	defer resp.Body.Close()
	var eb quotaAdmissionErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return eb
}

// intPtr returns a pointer to n -- a local *int helper distinct from any
// same-named helper in sibling packages (this package has none of its own).
func intPtr(n int) *int { return &n }

// quotaAdmissionPNGFixture returns a full PNG signature+IHDR chunk declaring
// a small 100x100 image, matching internal/api/handlers_test.go's own
// pngBytesFixture -- duplicated here (not imported; that helper lives in an
// unexported _test.go file in a different package) so both convert.Sniff and
// convert.Dimensions succeed on it.
func quotaAdmissionPNGFixture() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // signature
		0x00, 0x00, 0x00, 0x0D, // chunk length = 13
		0x49, 0x48, 0x44, 0x52, // "IHDR"
		0x00, 0x00, 0x00, 0x64, // width = 100
		0x00, 0x00, 0x00, 0x64, // height = 100
	}
}
