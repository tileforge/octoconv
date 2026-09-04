package queue

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// Client enqueues conversion tasks. Wraps an asynq.Client.
type Client struct {
	c *asynq.Client

	// imageMaxRetry is the per-task MaxRetry budget for image conversion
	// tasks (IMAGE_MAX_RETRY, default 4 — D-05, small compared to
	// webhook's 6).
	imageMaxRetry int
	// imageUniqueTTL is the per-job asynq.Unique lock TTL for image
	// conversion tasks, derived once at construction from imageMaxRetry
	// and ENGINE_TIMEOUT via ImageUniqueTTL — see its doc comment for the
	// worst-case-lifetime derivation this TTL must always exceed.
	imageUniqueTTL time.Duration
	// webhookUniqueTTL is the per-job asynq.Unique lock TTL for webhook
	// delivery tasks, derived once at construction from the fixed
	// webhookMaxRetry/webhookPerAttemptTimeout constants via
	// WebhookUniqueTTL — see its doc comment for the worst-case-lifetime
	// derivation this TTL must always exceed. Unlike imageUniqueTTL, its
	// inputs are not env-configurable (D-05/Phase 2 fixed them).
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
	// htmlMaxRetry is the per-task MaxRetry budget for html-to-pdf
	// conversion tasks (HTML_MAX_RETRY, default 3 — mirrors
	// documentMaxRetry's bounded-lower-than-image reasoning).
	htmlMaxRetry int
	// htmlUniqueTTL is the per-job asynq.Unique lock TTL for html-to-pdf
	// conversion tasks, derived once at construction from htmlMaxRetry and
	// HTML_ENGINE_TIMEOUT via HTMLUniqueTTL — see its doc comment for the
	// worst-case-lifetime derivation this TTL must always exceed.
	htmlUniqueTTL time.Duration
	// audioMaxRetry is the per-task MaxRetry budget for audio transcription
	// tasks (AUDIO_MAX_RETRY, default 3 — mirrors documentMaxRetry's/
	// htmlMaxRetry's bounded-lower-than-image reasoning).
	audioMaxRetry int
	// audioUniqueTTL is the per-job asynq.Unique lock TTL for audio
	// transcription tasks, derived once at construction from audioMaxRetry
	// and AUDIO_ENGINE_TIMEOUT via AudioUniqueTTL — see its doc comment for
	// the worst-case-lifetime derivation this TTL must always exceed.
	// Derived fresh, never reused from another engine class's TTL (per the
	// AudioUniqueTTL binding decision, STATE.md).
	audioUniqueTTL time.Duration
	// avMaxRetry is the per-task MaxRetry budget for av conversion tasks
	// (AV_MAX_RETRY, LOCKED default 2 per D-03 — fewer attempts than
	// audio's 3, since each av attempt costs materially more).
	avMaxRetry int
	// avUniqueTTL is the per-job asynq.Unique lock TTL for av conversion
	// tasks, derived once at construction from avMaxRetry and
	// AV_ENGINE_TIMEOUT via AVUniqueTTL. Derived fresh, never reused from
	// another engine class's TTL.
	avUniqueTTL time.Duration
	// ocrMaxRetry is the per-task MaxRetry budget for OCR conversion tasks
	// (OCR_MAX_RETRY, default 2 — mirrors avMaxRetry's bounded-retry budget
	// since a genuine tesseract/pdftoppm failure is deterministic on the
	// input and unlikely to be helped by more than a couple of attempts).
	ocrMaxRetry int
	// ocrUniqueTTL is the per-job asynq.Unique lock TTL for OCR conversion
	// tasks, derived once at construction from ocrMaxRetry and
	// OCR_ENGINE_TIMEOUT via OCRUniqueTTL. Derived fresh from OCR's OWN
	// retry/timeout budget, never reused from AVUniqueTTL/AudioUniqueTTL or
	// any other engine class's TTL (D-04/IN-02) — a mismatch here would
	// silently duplicate processing.
	ocrUniqueTTL time.Duration
	// pandocMaxRetry is the per-task MaxRetry budget for pandoc conversion
	// tasks (PANDOC_MAX_RETRY, default 2 — mirrors ocrMaxRetry's
	// bounded-retry budget since a genuine pandoc engine/parse failure is
	// deterministic on the input and unlikely to be helped by more than a
	// couple of attempts, and 48-CONTEXT.md D-01 classifies timeouts and
	// engine/parse errors as TERMINAL anyway).
	pandocMaxRetry int
	// pandocUniqueTTL is the per-job asynq.Unique lock TTL for pandoc
	// conversion tasks, derived once at construction from pandocMaxRetry and
	// PANDOC_ENGINE_TIMEOUT via PandocUniqueTTL. Derived fresh from
	// pandoc's OWN retry/timeout budget, never reused from ocrUniqueTTL/
	// avUniqueTTL/audioUniqueTTL or any other engine class's TTL (D-03,
	// 48-CONTEXT.md) — a mismatch here would silently duplicate processing.
	pandocUniqueTTL time.Duration
	// probeMaxRetry / probeUniqueTTL are the sanitize (probe queue)
	// equivalents, derived at construction exactly like every pair above.
	probeMaxRetry  int
	probeUniqueTTL time.Duration
	// tectonicMaxRetry / tectonicUniqueTTL are the typesetting (tectonic
	// queue) equivalents.
	tectonicMaxRetry  int
	tectonicUniqueTTL time.Duration
}

// NewClient builds a queue client from REDIS_ADDR, IMAGE_MAX_RETRY (default
// 4), ENGINE_TIMEOUT (default 120s — same env var the worker reads to bound
// a conversion attempt), DOCUMENT_MAX_RETRY (default 3),
// DOCUMENT_ENGINE_TIMEOUT (default 300s — Phase 9 D-01), HTML_MAX_RETRY
// (default 3), HTML_ENGINE_TIMEOUT (default 60s), AUDIO_MAX_RETRY (default
// 3), AUDIO_ENGINE_TIMEOUT (default 600s — an [ASSUMED] placeholder; Phase 32
// re-derives this from real-time-factor measurement against the pinned
// whisper-cli model), AV_MAX_RETRY (default 2, D-03 LOCKED), and
// AV_ENGINE_TIMEOUT (default 600s — [ASSUMED] provisional, mirroring the
// exact precedent AUDIO_ENGINE_TIMEOUT set: a 600s Go-code placeholder later
// replaced by a 742s production value after Phase 32's RTF measurement;
// Phase 36 re-derives AV_ENGINE_TIMEOUT the same way from an RTF matrix),
// OCR_MAX_RETRY (default 2), and OCR_ENGINE_TIMEOUT (default 300s — a
// PROVISIONAL code placeholder this phase, mirroring the AV_ENGINE_TIMEOUT
// [ASSUMED] precedent; Phase 45 supplies the measured OCR_ENGINE_TIMEOUT and
// supersedes this value entirely), PANDOC_MAX_RETRY (default 2), and
// PANDOC_ENGINE_TIMEOUT (default 90s — a CLEARLY-MARKED PLACEHOLDER this
// phase: pandoc is a fast, deterministic, CPU-bound markup-to-markup
// transform, so this is a small value in the ~60-120s range, mirroring the
// OCR_ENGINE_TIMEOUT 300s-provisional-then-744s-measured precedent (Phase
// 44 -> Phase 45); Phase 49 supplies the measured PANDOC_ENGINE_TIMEOUT from
// a real conversion-time matrix and supersedes this value entirely — see
// 48-CONTEXT.md D-03).
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
	imageMaxRetry := envInt("IMAGE_MAX_RETRY", 4)
	engineTimeout := envDuration("ENGINE_TIMEOUT", 120*time.Second)
	documentMaxRetry := envInt("DOCUMENT_MAX_RETRY", 3)
	documentEngineTimeout := envDuration("DOCUMENT_ENGINE_TIMEOUT", 300*time.Second)
	htmlMaxRetry := envInt("HTML_MAX_RETRY", 3)
	htmlEngineTimeout := envDuration("HTML_ENGINE_TIMEOUT", 60*time.Second)
	audioMaxRetry := envInt("AUDIO_MAX_RETRY", 3)
	audioEngineTimeout := envDuration("AUDIO_ENGINE_TIMEOUT", 600*time.Second)
	avMaxRetry := envInt("AV_MAX_RETRY", 2)
	avEngineTimeout := envDuration("AV_ENGINE_TIMEOUT", 600*time.Second)
	ocrMaxRetry := envInt("OCR_MAX_RETRY", 2)
	ocrEngineTimeout := envDuration("OCR_ENGINE_TIMEOUT", 300*time.Second)
	pandocMaxRetry := envInt("PANDOC_MAX_RETRY", 2)
	// PANDOC_ENGINE_TIMEOUT default of 90s is a PLACEHOLDER — pandoc is
	// fast; Phase 49 supplies the measured value and supersedes this
	// entirely (D-03, mirroring the OCR 300s->744s precedent).
	pandocEngineTimeout := envDuration("PANDOC_ENGINE_TIMEOUT", 90*time.Second)
	probeMaxRetry := envInt("PROBE_MAX_RETRY", 2)
	// PROBE_ENGINE_TIMEOUT's default is a PLACEHOLDER and is marked as such
	// everywhere it appears, following the exact precedent of pandoc's own
	// 90s placeholder above (superseded by Phase 49's measurement) and OCR's
	// 300s->744s before it. Phase 60's operator checkpoint supplies the
	// measured value; until it does, nothing in this build may describe this
	// number as measured.
	probeEngineTimeout := envDuration("PROBE_ENGINE_TIMEOUT", 600*time.Second)
	tectonicMaxRetry := envInt("TECTONIC_MAX_RETRY", 2)
	// TECTONIC_ENGINE_TIMEOUT is MEASURED as of Phase 64 (2026-09-04):
	// ceil(25.76 s/pass x 7 passes x 1.5) = 271s, with 7 the engine's own rerun
	// maximum and the per-pass cost taken just above the 2 MiB input ceiling. It is read HERE for
	// TectonicUniqueTTL's derivation, which happens in every enqueuing process
	// (IN-02) -- a value that reaches one process and not another does not
	// error, it silently produces duplicate processing.
	tectonicEngineTimeout := envDuration("TECTONIC_ENGINE_TIMEOUT", 271*time.Second)
	return &Client{
		c:                 asynq.NewClient(opt),
		imageMaxRetry:     imageMaxRetry,
		imageUniqueTTL:    ImageUniqueTTL(imageMaxRetry, engineTimeout),
		webhookUniqueTTL:  WebhookUniqueTTL(webhookMaxRetry, webhookPerAttemptTimeout),
		documentMaxRetry:  documentMaxRetry,
		documentUniqueTTL: DocumentUniqueTTL(documentMaxRetry, documentEngineTimeout),
		htmlMaxRetry:      htmlMaxRetry,
		htmlUniqueTTL:     HTMLUniqueTTL(htmlMaxRetry, htmlEngineTimeout),
		audioMaxRetry:     audioMaxRetry,
		audioUniqueTTL:    AudioUniqueTTL(audioMaxRetry, audioEngineTimeout),
		avMaxRetry:        avMaxRetry,
		avUniqueTTL:       AVUniqueTTL(avMaxRetry, avEngineTimeout),
		ocrMaxRetry:       ocrMaxRetry,
		ocrUniqueTTL:      OCRUniqueTTL(ocrMaxRetry, ocrEngineTimeout),
		pandocMaxRetry:    pandocMaxRetry,
		pandocUniqueTTL:   PandocUniqueTTL(pandocMaxRetry, pandocEngineTimeout),
		probeMaxRetry:     probeMaxRetry,
		probeUniqueTTL:    ProbeUniqueTTL(probeMaxRetry, probeEngineTimeout),
		tectonicMaxRetry:  tectonicMaxRetry,
		tectonicUniqueTTL: TectonicUniqueTTL(tectonicMaxRetry, tectonicEngineTimeout),
	}, nil
}

// Close releases the underlying Redis connections.
func (c *Client) Close() error { return c.c.Close() }

// EnqueueImageConvert puts an image conversion job onto the image queue.
func (c *Client) EnqueueImageConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewImageConvertTask(jobID, c.imageMaxRetry, c.imageUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue image convert %s: %w", jobID, err)
	}
	return nil
}

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

// EnqueueHTMLConvert puts an html-to-pdf conversion job onto the html
// queue.
func (c *Client) EnqueueHTMLConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewHTMLConvertTask(jobID, c.htmlMaxRetry, c.htmlUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue html convert %s: %w", jobID, err)
	}
	return nil
}

// EnqueueAudioConvert puts an audio transcription job onto the audio queue.
func (c *Client) EnqueueAudioConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewAudioConvertTask(jobID, c.audioMaxRetry, c.audioUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue audio convert %s: %w", jobID, err)
	}
	return nil
}

// EnqueueAVConvert puts a video conversion job onto the av queue.
func (c *Client) EnqueueAVConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewAVConvertTask(jobID, c.avMaxRetry, c.avUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue av convert %s: %w", jobID, err)
	}
	return nil
}

// EnqueueOCRConvert puts an OCR conversion job onto the ocr queue.
func (c *Client) EnqueueOCRConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewOCRConvertTask(jobID, c.ocrMaxRetry, c.ocrUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue ocr convert %s: %w", jobID, err)
	}
	return nil
}

// EnqueuePandocConvert puts a pandoc markup-to-markup conversion job onto
// the pandoc queue.
func (c *Client) EnqueuePandocConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewPandocConvertTask(jobID, c.pandocMaxRetry, c.pandocUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue pandoc convert %s: %w", jobID, err)
	}
	return nil
}

// EnqueueTectonicConvert puts a latex->pdf job onto the tectonic queue.
func (c *Client) EnqueueTectonicConvert(ctx context.Context, jobID uuid.UUID) error {
	task, err := NewTectonicConvertTask(jobID, c.tectonicMaxRetry, c.tectonicUniqueTTL)
	if err != nil {
		return err
	}
	if _, err := c.c.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue tectonic convert %s: %w", jobID, err)
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
// unparseable. Mirrors the convention in cmd/worker/main.go.
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
// unset or unparseable. Mirrors the convention in cmd/worker/main.go.
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
// cmd/worker/main.go's helper of the same name (unexported, per-package
// convention — see cmd/api/main.go for the sibling copy).
func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i]
		}
	}
	return s
}
