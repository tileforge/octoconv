package queue

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/tileforge/octoconv/internal/convert"
)

// Client enqueues conversion tasks. Wraps an asynq.Client.
type Client struct {
	c *asynq.Client

	// webhookUniqueTTL is the per-job asynq.Unique lock TTL for webhook
	// delivery tasks, derived once at construction from the fixed
	// webhookMaxRetry/webhookPerAttemptTimeout constants via
	// WebhookUniqueTTL — see its doc comment for the worst-case-lifetime
	// derivation this TTL must always exceed. Its inputs are not
	// env-configurable (D-05/Phase 2 fixed them).
	webhookUniqueTTL time.Duration
	// documentMaxRetry is the per-task MaxRetry budget for document
	// conversion tasks (DOCUMENT_MAX_RETRY, default 3 — bounded lower than
	// image's 4, since each document attempt is expensive at up to
	// DOCUMENT_ENGINE_TIMEOUT seconds and DOC-08 requires documents not be
	// retried forever).
	documentMaxRetry int
	// documentUniqueTTL is the per-job asynq.Unique lock TTL for document
	// conversion tasks, derived once at construction from documentMaxRetry
	// and DOCUMENT_ENGINE_TIMEOUT via DocumentUniqueTTL — see its doc
	// comment for the worst-case-lifetime derivation this TTL must always
	// exceed.
	documentUniqueTTL time.Duration
	// probeMaxRetry / probeUniqueTTL are the sanitize (probe queue)
	// equivalents, derived at construction exactly like every pair above.
	probeMaxRetry  int
	probeUniqueTTL time.Duration
	cadMaxRetry    int
	cadUniqueTTL   time.Duration
}

// NewClient builds a queue client from REDIS_ADDR, DOCUMENT_MAX_RETRY
// (default 3), and DOCUMENT_ENGINE_TIMEOUT (default 300s — Phase 9 D-01).
// ENGINE_TIMEOUT is the image protocol attempt budget and is read by
// cmd/api, not here. TECTONIC_ENGINE_TIMEOUT is the tectonic protocol
// attempt budget and is read by cmd/api, not here.
//
// COUPLING (Pitfall 4, RESEARCH.md): RECONCILER_ACTIVE_STALE_AFTER is a
// single global threshold (default 900s) that must comfortably exceed every
// engine's *_ENGINE_TIMEOUT, or the reconciler will re-enqueue a still-
// legitimately-running job as stale. This is a documented near-miss from the
// audio engine (see docker-compose.yml): audio's duration-ceiling-derived
// timeout once breached the 900s/15m reconciler cap and forced
// AUDIO_MAX_DURATION_SECONDS to be lowered as a NO-GO lever. Any future
// raise of AV_ENGINE_TIMEOUT toward 900s is therefore a coupled decision
// requiring RECONCILER_ACTIVE_STALE_AFTER to be raised in the same change —
// do not raise one without the other.
func NewClient() (*Client, error) {
	opt, err := RedisOpt()
	if err != nil {
		return nil, err
	}
	documentMaxRetry := envInt("DOCUMENT_MAX_RETRY", 3)
	documentEngineTimeout := envDuration("DOCUMENT_ENGINE_TIMEOUT", 300*time.Second)
	probeMaxRetry := envInt("PROBE_MAX_RETRY", 2)
	// PROBE_ENGINE_TIMEOUT's default is a PLACEHOLDER and is marked as such
	// everywhere it appears, following the exact precedent of pandoc's own
	// 90s placeholder above (superseded by Phase 49's measurement) and OCR's
	// 300s->744s before it. Phase 60's operator checkpoint supplies the
	// measured value; until it does, nothing in this build may describe this
	// number as measured.
	probeEngineTimeout := envDuration("PROBE_ENGINE_TIMEOUT", 600*time.Second)
	cadMaxRetry := envInt("CAD_MAX_RETRY", 2)
	cadEngineTimeout := envDuration("CAD_ENGINE_TIMEOUT", convert.CADDefaultEngineTimeout)
	return &Client{
		c:                 asynq.NewClient(opt),
		webhookUniqueTTL:  WebhookUniqueTTL(webhookMaxRetry, webhookPerAttemptTimeout),
		documentMaxRetry:  documentMaxRetry,
		documentUniqueTTL: DocumentUniqueTTL(documentMaxRetry, documentEngineTimeout),
		probeMaxRetry:     probeMaxRetry,
		probeUniqueTTL:    ProbeUniqueTTL(probeMaxRetry, probeEngineTimeout),
		cadMaxRetry:       cadMaxRetry,
		cadUniqueTTL:      CADUniqueTTL(cadMaxRetry, cadEngineTimeout),
	}, nil
}

// Close releases the underlying Redis connections.
func (c *Client) Close() error { return c.c.Close() }

// EnqueueWebhookDeliver puts a webhook delivery task onto the webhook queue.
func (c *Client) EnqueueWebhookDeliver(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewWebhookDeliverTask(jobID, c.webhookUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue webhook deliver %s: %w", jobID, err)
	}
	return nil
}

// EnqueueDocumentConvert puts a document conversion job onto the document
// queue.
func (c *Client) EnqueueDocumentConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewDocumentConvertTask(jobID, c.documentMaxRetry, c.documentUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue document convert %s: %w", jobID, err)
	}
	return nil
}

func (c *Client) EnqueueCADJob(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewCADJobTask(jobID, c.cadMaxRetry, c.cadUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue cad job %s: %w", jobID, err)
	}
	return nil
}

// EnqueueProbeSanitize puts a sanitize job onto the probe queue.
func (c *Client) EnqueueProbeSanitize(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewProbeSanitizeTask(jobID, c.probeMaxRetry, c.probeUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue probe sanitize %s: %w", jobID, err)
	}
	return nil
}

// envInt reads an integer environment variable, tolerating a trailing
// inline `# comment` (see firstField), falling back to def if unset or
// unparseable. Mirrors the convention in cmd/api/main.go.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(firstField(v)); err == nil {
			return n
		}
	}
	return def
}

// envDuration reads a time.Duration environment variable, tolerating a
// trailing inline `# comment` (see firstField), falling back to def if
// unset or unparseable. Mirrors the convention in cmd/api/main.go.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(firstField(v)); err == nil {
			return d
		}
	}
	return def
}

// firstField strips a trailing whitespace-delimited inline comment from an
// env value, e.g. "120s   # comment" -> "120s". Duplicated from
// cmd/api/main.go's helper of the same name (unexported, per-package).
func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i]
		}
	}
	return s
}
