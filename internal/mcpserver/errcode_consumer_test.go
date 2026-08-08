package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestUnknownErrorCodeFallsBackGenerically (D-08, Pitfall 12) proves the sync
// half of the unknown-code fallback contract: a 422 carrying a code this
// package has never seen before must still produce a typed, recoverable
// *APIError, a normal (non-panicking, non-protocol-level) tool error result
// carrying the server's own message, and must never leak the API key.
func TestUnknownErrorCodeFallsBackGenerically(t *testing.T) {
	const unknownCode = "totally_new_code_v99"
	const serverMsg = "a brand new failure"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": serverMsg, "code": unknownCode})
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

	// 1. The client returns a typed error recoverable via errors.As, with
	// Code carrying the unrecognized value verbatim -- proving the code is
	// not silently dropped or coerced.
	_, err := c.GetJob(context.Background(), "some-job-id")
	if err == nil {
		t.Fatal("GetJob: expected an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("GetJob error = %v (%T), want an *APIError recoverable via errors.As", err, err)
	}
	if apiErr.Code != unknownCode {
		t.Fatalf("APIError.Code = %q, want %q", apiErr.Code, unknownCode)
	}
	if apiErr.RetryAfter != -1 {
		t.Fatalf("APIError.RetryAfter = %d, want -1 (no Retry-After header on this response)", apiErr.RetryAfter)
	}

	// 2. classifyAPIError (the tool-handler layer's structural switch) has
	// no case for this code, so it must fall through unchanged -- not
	// panic, not rewrite, not drop it.
	classified := classifyAPIError(err)
	if classified.Error() != err.Error() {
		t.Fatalf("classifyAPIError changed the error for an unrecognized code: got %q, want unchanged %q", classified.Error(), err.Error())
	}

	// 3. Driven through the real MCP tool-call path (get_job_status), this
	// must surface as a normal CallToolResult{IsError:true} carrying the
	// server's message -- never panic, never a protocol-level error, never
	// the raw API key.
	h := newHarness(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      t.TempDir(),
		ConvertTimeout: time.Second,
		PollInterval:   time.Millisecond,
	}, nil)

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("get_job_status panicked on an unrecognized error code: %v", r)
			}
		}()
		res, callErr := h.cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "get_job_status",
			Arguments: map[string]any{"job_id": "some-job-id"},
		})
		if callErr != nil {
			t.Fatalf("CallTool returned a protocol-level error, want a normal IsError result: %v", callErr)
		}
		if !res.IsError {
			t.Fatalf("IsError = false, want true; content = %v", res.Content)
		}
		text := resultText(t, res)
		if !strings.Contains(text, serverMsg) {
			t.Fatalf("result text = %q, want it to contain the server message %q", text, serverMsg)
		}
		if strings.Contains(text, testAPIKey) {
			t.Fatal("result text leaked the raw API key")
		}
	}()
}

// TestUnknownAsyncErrorCodeFallsBackGenerically (D-08, Pitfall 12) proves the
// async half: a job that reaches status=failed with an unrecognized
// error_code must still produce a normal, non-panicking convert_file tool
// error carrying the server's error_message, and must never leak the API
// key. Exercises describeFailedJob's default branch (client.go).
func TestUnknownAsyncErrorCodeFallsBackGenerically(t *testing.T) {
	const unknownCode = "totally_new_async_code_v99"
	const serverMsg = "the engine did something nobody has ever seen before"

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "in.png")
	if err := os.WriteFile(inputPath, []byte("not a real png, only used for the multipart body"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job-async-unknown", "status": "queued"})
	})
	mux.HandleFunc("/v1/jobs/job-async-unknown", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(JobStatus{
			JobID:        "job-async-unknown",
			Status:       "failed",
			ErrorCode:    unknownCode,
			ErrorMessage: serverMsg,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := newHarness(t, Config{
		BaseURL:        srv.URL,
		APIKey:         testAPIKey,
		OutputDir:      dir,
		ConvertTimeout: 5 * time.Second,
		PollInterval:   10 * time.Millisecond,
	}, nil)

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("convert_file panicked on an unrecognized async error code: %v", r)
			}
		}()
		res, callErr := h.cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "convert_file",
			Arguments: map[string]any{"path": inputPath, "target_format": "webp"},
		})
		if callErr != nil {
			t.Fatalf("CallTool returned a protocol-level error, want a normal IsError result: %v", callErr)
		}
		if !res.IsError {
			t.Fatalf("IsError = false, want true; content = %v", res.Content)
		}
		text := resultText(t, res)
		if !strings.Contains(text, serverMsg) {
			t.Fatalf("result text = %q, want it to contain the server error_message %q", text, serverMsg)
		}
		if strings.Contains(text, testAPIKey) {
			t.Fatal("result text leaked the raw API key")
		}
	}()
}

// TestBothMCPEntryPointsShareTheClient (56-07 acceptance) asserts, by
// reading the actual entry-point sources rather than assuming it from
// proxying, that both cmd/mcp-server and cmd/mcp-http construct their tools
// through mcpserver.NewServer -- so this plan's migration (classifyAPIError,
// APIError, describeFailedJob) provably covers both binaries, not just one.
func TestBothMCPEntryPointsShareTheClient(t *testing.T) {
	repoRoot, err := findRepoRoot(t)
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}

	for _, rel := range []string{
		filepath.Join("cmd", "mcp-server", "main.go"),
		filepath.Join("cmd", "mcp-http", "main.go"),
	} {
		path := filepath.Join(repoRoot, rel)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(b)
		if !strings.Contains(src, "mcpserver.NewServer(") {
			t.Fatalf("%s does not call mcpserver.NewServer -- entry points must share internal/mcpserver, not reimplement tool construction", rel)
		}
	}
}

// findRepoRoot walks up from the current working directory (this test
// package's directory when run via `go test`) until it finds go.mod.
func findRepoRoot(t *testing.T) (string, error) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
