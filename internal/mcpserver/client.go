package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	formFieldFile      = "file"
	formFieldOperation = "operation"
	formFieldTarget    = "target"
	formFieldPreset    = "preset"
	formFieldOpts      = "opts"

	// Job status values, mirroring internal/jobs' status constants. Hardcoded
	// here (not imported) because internal/mcpserver may import no other
	// internal/* package (D-02) -- this package only speaks the API's public
	// wire contract.
	statusDone   = "done"
	statusFailed = "failed"

	// Error codes this package branches on structurally (docs/ERROR-CODES.md).
	// Hardcoded here rather than importing internal/errcode, for the same
	// D-02 reason statusDone/statusFailed are hardcoded above: this package
	// speaks only the API's public wire contract, never its internal Go
	// types. These are a curated SUBSET (only the codes this package
	// actually branches on) of the full closed vocabulary; any other code,
	// known or future, falls through to the default/generic path everywhere
	// it is checked (Pitfall 12).
	codeRateLimited    = "rate_limited"    // sync 429: wait-then-retry is meaningful (D-12)
	codeQuotaExceeded  = "quota_exceeded"  // sync 429: retrying will not help until the quota resets
	codeInvalidOptions = "invalid_options" // shared sync/async code space (D-03): the request's opts were rejected

	// apiRequestTimeout bounds a single control-plane request (create/get
	// job, list formats/presets) -- distinct from the overall convert
	// deadline, which is enforced by ConvertBlocking across many such calls.
	apiRequestTimeout = 30 * time.Second
	// downloadClientTimeout bounds a single presigned-download GET.
	downloadClientTimeout = 5 * time.Minute
	// maxErrorBodyBytes bounds how much of a non-2xx response body is read
	// when building an error message -- an opaque-string DoS guard,
	// independent of the actual body size the server sent.
	maxErrorBodyBytes = 4096
)

// Client is a hand-rolled HTTP client of the OctoConv public API
// implementing the blocking convert workflow: multipart upload, poll,
// presigned download. The API key is confined to this struct (Pitfall 2) --
// it is never passed around bare and never appears in a returned error
// (D-08, enforced by redact on every error path).
type Client struct {
	baseURL        string
	apiKey         string
	httpClient     *http.Client
	outputDir      string
	convertTimeout time.Duration
	pollInterval   time.Duration
	s3DialAddr     string
	resultMode     ResultMode
}

// NewClient builds a Client from cfg. In local mode it ensures OutputDir
// exists so Download can always write into it; in remote mode (D-04) the
// filesystem is never touched -- no directory is created, since remote
// results are presigned-only and Download is never called.
func NewClient(cfg Config) (*Client, error) {
	if cfg.ResultMode != ResultRemote {
		if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
			return nil, fmt.Errorf("create output dir: %w", err)
		}
	}
	return &Client{
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:         cfg.APIKey,
		httpClient:     &http.Client{Timeout: apiRequestTimeout},
		outputDir:      cfg.OutputDir,
		convertTimeout: cfg.ConvertTimeout,
		pollInterval:   cfg.PollInterval,
		s3DialAddr:     cfg.S3DialAddr,
		resultMode:     cfg.ResultMode,
	}, nil
}

// NewClientForKey builds a per-request Client from base, substituting apiKey
// as the caller's own credential (D-03: per-request caller-key pass-through).
// Every other knob (BaseURL, timeouts, poll interval, result mode) is reused
// from base unchanged. base is received by value, so the substitution never
// mutates shared state -- two concurrent requests with different keys get two
// fully isolated Clients.
func NewClientForKey(base Config, apiKey string) (*Client, error) {
	base.APIKey = apiKey
	return NewClient(base)
}

// JobStatus is the decoded shape of GET /v1/jobs/{id}.
type JobStatus struct {
	JobID        string         `json:"job_id"`
	Status       string         `json:"status"`
	Opts         map[string]any `json:"opts,omitempty"`
	DownloadURL  string         `json:"download_url,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

// EngineFormats is one engine class's supported (source, target) pairs, as
// returned by GET /v1/formats.
type EngineFormats struct {
	Pairs [][2]string `json:"pairs"`
}

// FormatsResponse is the decoded shape of GET /v1/formats. It is ALSO the
// list_supported_formats MCP tool's output type (tools.go:269), so every
// field's json tag doubles as agent-facing documentation.
type FormatsResponse struct {
	Engines map[string]EngineFormats `json:"engines"`
	// Operations is the {operation -> engine -> supported (source, target)
	// pairs} tree GET /v1/formats emits unconditionally since 57-04
	// (internal/api/formats_handlers.go:41-61). tools.go:68's convert_file
	// jsonschema already tells agents to consult "the operations key of the
	// list_supported_formats/GET /v1/formats response" to discover the
	// currently available operations (convert is the default when the
	// operation field is omitted); without this field that key was silently
	// dropped by this decoder, so the advertised discovery path could not
	// actually be performed (codex PR #7 finding #2). Both MCP binaries
	// (cmd/mcp, cmd/mcp-http) route through this one package, so this single
	// field serves both. omitempty so a pre-Phase-57 API build that does not
	// emit the key yields an absent field rather than an empty object.
	Operations map[string]map[string]EngineFormats `json:"operations,omitempty" jsonschema:"operation -> engine -> supported (source, target) pairs; convert is the default operation when convert_file's operation field is omitted"`
}

// Preset is the decoded shape of one entry from GET /v1/presets.
type Preset struct {
	Name         string         `json:"name"`
	Version      int            `json:"version"`
	Scope        string         `json:"scope"`
	Operation    string         `json:"operation"`
	TargetFormat string         `json:"target_format"`
	Options      map[string]any `json:"options"`
	Description  string         `json:"description"`
	IsActive     bool           `json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// ConvertResult is what ConvertBlocking returns on success: the presigned
// URL plus a local path -- NEVER inline file bytes (D-07).
type ConvertResult struct {
	JobID       string
	DownloadURL string
	LocalPath   string
}

// TimeoutError is returned by ConvertBlocking when CONVERT_TIMEOUT elapses
// before the job reaches a terminal state. It carries the job id so a
// calling MCP tool can hand off to get_job_status/download_result instead of
// losing track of the in-flight job (D-04/D-05).
type TimeoutError struct {
	JobID string
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("conversion timed out waiting for job %s; use get_job_status to check on it", e.JobID)
}

// CreateJob uploads the file at filePath as a multipart POST /v1/jobs
// request. target and preset are mutually exclusive per the wire contract;
// this client forwards whatever the caller supplies without arbitrating
// between them -- the API itself is the single enforcement authority. The
// same "forward without arbitrating" rule applies to operation: this client
// never checks whether operation is a known value -- it writes it verbatim
// (guarded on non-emptiness, like target/preset/opts) and lets the API's
// registry (convert.Default) be the single source of truth. A local
// allowlist here would be a second source of truth that could silently
// diverge from the registry, and the API's 422 unsupported_operation
// response is forwarded to the caller unchanged either way. opts is an
// optional raw JSON string. Returns the created job id.
func (c *Client) CreateJob(ctx context.Context, filePath, operation, target, preset, opts string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open input file: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile(formFieldFile, filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("build multipart request: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", fmt.Errorf("build multipart request: %w", err)
	}
	if operation != "" {
		if err := mw.WriteField(formFieldOperation, operation); err != nil {
			return "", fmt.Errorf("build multipart request: %w", err)
		}
	}
	if target != "" {
		if err := mw.WriteField(formFieldTarget, target); err != nil {
			return "", fmt.Errorf("build multipart request: %w", err)
		}
	}
	if preset != "" {
		if err := mw.WriteField(formFieldPreset, preset); err != nil {
			return "", fmt.Errorf("build multipart request: %w", err)
		}
	}
	if opts != "" {
		if err := mw.WriteField(formFieldOpts, opts); err != nil {
			return "", fmt.Errorf("build multipart request: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("build multipart request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/jobs", &body)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", c.wrapTransportErr("create job", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return "", c.apiError("create job", resp)
	}

	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("create job: decode response: %w", err)
	}
	return out.JobID, nil
}

// GetJob fetches the current status of jobID via GET /v1/jobs/{id}.
func (c *Client) GetJob(ctx context.Context, jobID string) (JobStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/jobs/"+jobID, nil)
	if err != nil {
		return JobStatus{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return JobStatus{}, c.wrapTransportErr("get job", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return JobStatus{}, c.apiError("get job", resp)
	}

	var js JobStatus
	if err := json.NewDecoder(resp.Body).Decode(&js); err != nil {
		return JobStatus{}, fmt.Errorf("get job: decode response: %w", err)
	}
	return js, nil
}

// ListFormats fetches GET /v1/formats.
func (c *Client) ListFormats(ctx context.Context) (FormatsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/formats", nil)
	if err != nil {
		return FormatsResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return FormatsResponse{}, c.wrapTransportErr("list formats", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return FormatsResponse{}, c.apiError("list formats", resp)
	}

	var fr FormatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		return FormatsResponse{}, fmt.Errorf("list formats: decode response: %w", err)
	}
	return fr, nil
}

// ListPresets fetches GET /v1/presets, appending ?all=true when
// includeInactive is set.
func (c *Client) ListPresets(ctx context.Context, includeInactive bool) ([]Preset, error) {
	u := c.baseURL + "/v1/presets"
	if includeInactive {
		u += "?all=true"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.wrapTransportErr("list presets", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.apiError("list presets", resp)
	}

	var presets []Preset
	if err := json.NewDecoder(resp.Body).Decode(&presets); err != nil {
		return nil, fmt.Errorf("list presets: decode response: %w", err)
	}
	return presets, nil
}

// Download fetches presignedURL (self-authorizing -- NO Authorization
// header) and streams it directly to a file inside OUTPUT_DIR, never
// buffering the body into memory for return (D-07). The local filename is
// the sanitized basename of the presigned URL's path (the server-controlled
// S3 key), never any client-supplied name (D-09); jobID is used to derive a
// fallback name if sanitization rejects the path entirely.
func (c *Client) Download(ctx context.Context, jobID, presignedURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, presignedURL, nil)
	if err != nil {
		return "", fmt.Errorf("build download request: %w", err)
	}

	resp, err := c.downloadClient().Do(req)
	if err != nil {
		return "", c.wrapTransportErr("download result", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", c.apiError("download result", resp)
	}

	u, err := url.Parse(presignedURL)
	if err != nil {
		return "", fmt.Errorf("parse download url: %w", err)
	}
	name := sanitizeBasename(u.Path)
	if name == "" {
		name = jobID + "-result"
	}
	localPath := filepath.Join(c.outputDir, name)

	out, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("create local file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("write local file: %w", err)
	}
	return localPath, nil
}

// downloadClient builds the *http.Client used for presigned downloads.
// Mirrors internal/e2e's downloadClient() exactly (dial_redirect_contract):
// the presigned V4 signature covers the Host header, so rewriting the URL's
// host would break the signature -- but dialing a DIFFERENT address under
// the ORIGINAL Host does not. Empty s3DialAddr (production default) is a
// strict no-op: a plain client with only a Timeout, no custom Transport.
func (c *Client) downloadClient() *http.Client {
	if c.s3DialAddr == "" {
		return &http.Client{Timeout: downloadClientTimeout}
	}
	return &http.Client{
		Timeout: downloadClientTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, c.s3DialAddr)
			},
		},
	}
}

// ConvertBlocking runs the full blocking convert workflow: CreateJob, then
// poll GetJob every PollInterval (invoking onTick with the latest status on
// every tick) bounded by ConvertTimeout, then Download the presigned result
// on success. operation is forwarded to CreateJob verbatim -- see CreateJob's
// doc comment for why this client never arbitrates its value.
func (c *Client) ConvertBlocking(ctx context.Context, filePath, operation, target, preset, opts string, onTick func(status string)) (ConvertResult, error) {
	jobID, err := c.CreateJob(ctx, filePath, operation, target, preset, opts)
	if err != nil {
		return ConvertResult{}, err
	}

	deadline := time.Now().Add(c.convertTimeout)
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		job, err := c.GetJob(ctx, jobID)
		if err != nil {
			return ConvertResult{}, err
		}
		if onTick != nil {
			onTick(job.Status)
		}

		switch job.Status {
		case statusDone:
			// Remote mode (D-04): the result is presigned-only -- never
			// download server-side, never touch the filesystem. The caller
			// fetches the URL directly.
			if c.resultMode == ResultRemote {
				return ConvertResult{JobID: jobID, DownloadURL: job.DownloadURL}, nil
			}
			localPath, err := c.Download(ctx, jobID, job.DownloadURL)
			if err != nil {
				return ConvertResult{}, err
			}
			return ConvertResult{JobID: jobID, DownloadURL: job.DownloadURL, LocalPath: localPath}, nil
		case statusFailed:
			return ConvertResult{}, describeFailedJob(jobID, job)
		}

		if !time.Now().Before(deadline) {
			return ConvertResult{}, &TimeoutError{JobID: jobID}
		}

		select {
		case <-ctx.Done():
			return ConvertResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// describeFailedJob builds the error ConvertBlocking returns for a job that
// finished in the failed status, selecting its wording structurally from
// job.ErrorCode (docs/ERROR-CODES.md's async table) -- never by matching
// job.ErrorMessage's text. An unrecognized or absent code falls through to
// the pre-existing generic wording unchanged, satisfying the unknown-code
// fallback contract (Pitfall 12): a future additive async code must degrade
// safely here without a code change.
func describeFailedJob(jobID string, job JobStatus) error {
	msg := job.ErrorMessage
	if msg == "" {
		msg = "conversion failed"
	}

	switch job.ErrorCode {
	case codeInvalidOptions:
		return fmt.Errorf("job %s failed: %s -- the request's opts were rejected; fix them and submit a new job", jobID, msg)
	default:
		return fmt.Errorf("job %s failed: %s", jobID, msg)
	}
}

// sanitizeBasename returns the bare filename component of rawPath (a
// presigned URL's path, i.e. a server-controlled S3 key) with every
// directory component, path separator, and leading dot stripped. It returns
// "" if nothing safe remains (e.g. the path was empty, ".", "..", or a bare
// separator) -- callers must supply their own fallback name in that case
// (D-09: never write using an unsanitized server-supplied name).
func sanitizeBasename(rawPath string) string {
	name := filepath.Base(rawPath)
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return ""
	}
	name = strings.TrimLeft(name, ".")
	if name == "" || strings.ContainsAny(name, "/\\") {
		return ""
	}
	return name
}

// apiError builds an error from a non-2xx response, surfacing the API's own
// {"error": "..."} text verbatim (D-11 -- the API's own no-leak texts pass
// through unchanged) while redacting the API key from the result as a
// defense-in-depth measure (D-08), unconditionally, on every return branch.
//
// D-12: without a machine-readable code and a wait hint, an MCP agent
// receiving a 429 cannot tell "wait N seconds, then retry" (code=rate_limited)
// from "you are not allowed at all right now" (code=quota_exceeded, plan
// 52-04) and either retries into a wall or gives up when it didn't need to.
// So when the body carries a code, it is appended to the message; when the
// response also carries a Retry-After header that parses as a non-negative
// integer, the wait hint is appended too. A malformed or missing Retry-After
// is simply omitted -- never injected verbatim -- so a hostile or buggy
// upstream can't smuggle arbitrary text into the agent-visible error.
func (c *Client) apiError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	msg, code := extractErrorBody(body)
	if msg == "" {
		msg = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}

	// retryAfter is -1 (no code, or a code with no usable Retry-After
	// header) unless the response carried both -- distinct from an explicit
	// "retry in 0 seconds", which parseRetryAfterSeconds treats as valid.
	retryAfter := -1
	if code != "" {
		if ra, ok := parseRetryAfterSeconds(resp.Header.Get("Retry-After")); ok {
			retryAfter = ra
		}
	}

	return &APIError{
		Action:     action,
		Status:     resp.StatusCode,
		Code:       code,
		Message:    redact(msg, c.apiKey),
		RetryAfter: retryAfter,
	}
}

// APIError is the typed error every non-2xx API response produces (D-12,
// ERRC-03). Error() reproduces byte-for-byte the message format this
// package has always emitted -- the pre-existing text assertions in
// client_test.go and quota_admission_test.go stay unmodified and passing.
// A caller that needs to act on the failure structurally (a tool handler
// choosing retry-now vs do-not-retry, or falling back generically on an
// unrecognized code) should recover *APIError via errors.As and branch on
// Code, never by matching Error()'s text (ERRC-03, docs/ERROR-CODES.md).
type APIError struct {
	Action string // the operation that failed, e.g. "get job"
	Status int    // the HTTP status code
	Code   string // the machine-readable code (docs/ERROR-CODES.md); "" if the body carried none
	// Message is the human-readable text, already redacted (D-08) -- a
	// typed field is just as agent-visible as Error()'s string, so
	// redaction is applied once here rather than trusting every future
	// caller to redact it again before logging or echoing it.
	Message string
	// RetryAfter is the parsed Retry-After header value in whole seconds,
	// or -1 when the response carried no code, or a code but no (usable)
	// Retry-After header.
	RetryAfter int
}

// Error implements the error interface, reproducing the exact three
// formats apiError has always produced: message alone, message+code, or
// message+code+retry_after.
func (e *APIError) Error() string {
	msg := e.Message
	switch {
	case e.Code == "":
		// No code at all: the message exactly as today.
	case e.RetryAfter < 0:
		msg = fmt.Sprintf("%s (code=%s)", msg, e.Code)
	default:
		msg = fmt.Sprintf("%s (code=%s, retry_after=%ds)", msg, e.Code, e.RetryAfter)
	}
	return fmt.Sprintf("%s: %s", e.Action, msg)
}

// extractErrorBody pulls the "error" and "code" fields out of a JSON body
// shaped like {"error":"...","code":"..."}, falling back to (the raw
// trimmed body, "") when it isn't JSON-shaped that way.
func extractErrorBody(body []byte) (msg, code string) {
	var e struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		return e.Error, e.Code
	}
	return strings.TrimSpace(string(body)), ""
}

// parseRetryAfterSeconds parses an HTTP Retry-After header value as a
// non-negative whole-seconds integer. It deliberately does not support the
// HTTP-date form of Retry-After -- this codebase's own 429 producers
// (internal/ratelimit, and quota rejections in plan 52-04) only ever emit
// the delta-seconds form.
func parseRetryAfterSeconds(v string) (int, bool) {
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// wrapTransportErr wraps a network/transport-level error (DNS, dial, TLS,
// timeout) with action context, redacting the API key as a defense-in-depth
// measure (D-08) -- the key is never part of the request URL, only the
// Authorization header, but this guards against any future error path that
// might otherwise echo request details.
func (c *Client) wrapTransportErr(action string, err error) error {
	return fmt.Errorf("%s: %s", action, redact(err.Error(), c.apiKey))
}

// redact replaces every occurrence of key in s with a fixed placeholder
// (D-08). A no-op when key is empty.
func redact(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "[REDACTED]")
}
