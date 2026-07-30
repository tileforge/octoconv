// Command pandoc-worker runs the OctoConv pandoc-class worker: it consumes
// the pandoc queue and executes markup-family (md/rst/org/latex/html/docx/
// epub) interconversion via the real `pandoc` binary.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/apaderin/octoconv/internal/cancel"
	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/db"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/queue"
	"github.com/apaderin/octoconv/internal/storage"
	"github.com/apaderin/octoconv/internal/webhook"
	"github.com/apaderin/octoconv/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	store, err := storage.New(ctx)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	redisOpt, err := queue.RedisOpt()
	if err != nil {
		log.Fatalf("redis: %v", err)
	}

	// D-04 (mirrors cmd/ocr-worker/main.go): run the job:cancel Subscriber
	// against the SAME process-wide cancel.Default registry process()'s
	// cancel.Default.Register/Deregister calls populate
	// (internal/worker/worker.go) -- a received cancel cancels the exact
	// in-flight attempt this worker is running for that job id (no-op
	// otherwise, D-04). Bound to the process signal ctx so Run returns
	// promptly on shutdown (T-39-09).
	cancelRDB := redis.NewClient(&redis.Options{Addr: redisOpt.Addr})
	defer cancelRDB.Close()
	go cancel.NewSubscriber(cancelRDB, cancel.Default).Run(ctx)

	qc, err := queue.NewClient()
	if err != nil {
		log.Fatalf("queue client: %v", err)
	}
	defer qc.Close()

	repo := jobs.NewRepo(pool)

	// D-01/D-03 (mirrors cmd/ocr-worker/main.go's own re-register precedent):
	// re-register a CONFIGURED PandocConverter, overriding the zero-value
	// registration converters.go's init() already performed -- documented
	// last-write-wins semantics (Registry.Register is a bare map assignment,
	// converters.go). This MUST run before srv.Start(mux) below: that is the
	// point asynq's worker goroutines begin concurrently reading the
	// registered converter, so this single write must happen-before every
	// concurrent read.
	//
	// PANDOC_MAX_INPUT_BYTES default (50 MiB) is Claude's Discretion
	// (48-CONTEXT.md): pandoc reads its input linearly with no exotic
	// pre-parse "bomb shape" to guard against (markupsniff.go's own
	// ErrPandocInputTooLarge doc comment), so this cap plus the engine
	// timeout below are the full defense-in-depth here, layered over the
	// API's own maxEngineBytes upload-time gate (Plan 02).
	pandocMaxInputBytes := envInt64("PANDOC_MAX_INPUT_BYTES", 50<<20)
	convert.Default.Register(buildPandocConverter(pandocMaxInputBytes))
	log.Printf("📏 PANDOC_MAX_INPUT_BYTES=%d", pandocMaxInputBytes)

	// PANDOC_ENGINE_TIMEOUT default of 90s is a CLEARLY-MARKED PLACEHOLDER --
	// pandoc is a fast, deterministic CPU-bound engine unlike OCR/AV's
	// heavier per-page/per-frame processing, so this fallback is
	// deliberately smaller than OCR's own 300s provisional default. The
	// real, measured/finalized value is superseded by Phase 49's supervised
	// timing-measurement run (mirrors OCR_ENGINE_TIMEOUT's own
	// provisional-fallback-then-measured comment, cmd/ocr-worker/main.go).
	// This MUST read the SAME env var (and agree with the SAME default)
	// internal/queue/client.go's NewClient reads for PandocUniqueTTL's
	// derivation -- both must stay in sync so the asynq.Unique lock TTL
	// always covers at least one full worst-case attempt.
	engineTimeout := envDuration("PANDOC_ENGINE_TIMEOUT", 90*time.Second)

	// PROG-03 (mirrors cmd/ocr-worker/main.go): the dual-gate progress-write
	// throttle (D-03) -- env-tunable, read here (not in internal/worker) and
	// wired via the SAME post-construction-setter convention; MUST complete
	// before srv.Start(mux) below, the happens-before boundary every other
	// process-wide config write in this file uses. PandocConverter does NOT
	// implement convert.ProgressConverter (a single subprocess, no
	// page-granular signal), so runConvertWithOptionalProgress's type-assert
	// seam always falls back to the plain conv.Convert path for pandoc jobs
	// -- this throttle configuration is harmless dead weight for pandoc
	// specifically, kept only for consistency with every other worker cmd's
	// wiring (reuse-unchanged infra, per this phase's own scope fence).
	progressMinInterval := envDurationSeconds("PROGRESS_MIN_INTERVAL_SECONDS", 2*time.Second)
	progressMinDeltaPct := envInt("PROGRESS_MIN_DELTA_PCT", 1)

	h := worker.NewHandler(
		repo,
		store,
		convert.Default,
		engineTimeout,
		nil, // webhookRepo — webhook-only; HandlePandocConvert never reads it
		nil, // deliverer — webhook-only; HandlePandocConvert never reads it
		qc,
		nil, // signingSecret — webhook-only; HandlePandocConvert never reads it
		0,   // presignTTL — webhook-only; HandlePandocConvert never reads it
		0,   // audioMaxDuration — 0 for every non-audio worker cmd (worker.go); pandoc's guards are self-contained inside PandocConverter.Convert, not spliced through this parameter
	)

	h.SetProgressThrottle(progressMinInterval, progressMinDeltaPct)
	log.Printf("📈 PROGRESS_MIN_INTERVAL_SECONDS=%v PROGRESS_MIN_DELTA_PCT=%d", progressMinInterval, progressMinDeltaPct)

	// D-04 (mirrors cmd/ocr-worker/main.go/PUSH-02): wire a real
	// webhook.Deliverer for the inline best-effort progress-webhook fire --
	// gated on WEBHOOK_SIGNING_SECRET being set (fail-open, not fail-closed).
	// MUST complete before srv.Start(mux) below.
	webhookSigningSecret := []byte(os.Getenv("WEBHOOK_SIGNING_SECRET"))
	progressWebhookTimeout := envDuration("PROGRESS_WEBHOOK_TIMEOUT", 1500*time.Millisecond)
	if len(webhookSigningSecret) > 0 {
		h.SetProgressWebhook(webhook.NewDeliverer(), webhookSigningSecret, progressWebhookTimeout)
		log.Printf("🔔 progress webhooks enabled (PROGRESS_WEBHOOK_TIMEOUT=%v)", progressWebhookTimeout)
	} else {
		log.Printf("🔕 progress webhooks disabled (WEBHOOK_SIGNING_SECRET unset)")
	}

	// D-04/D-05 (mirrors cmd/ocr-worker/main.go): the stale-job sweep loop
	// runs solely in cmd/webhook-worker (under a Postgres advisory lock) — no
	// sweeper of any kind is constructed or run here.

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypePandocConvert, h.HandlePandocConvert)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:    envInt("PANDOC_WORKER_CONCURRENCY", 2), // pandoc is fast/light (a single deterministic subprocess) unlike the 1-at-a-time heavy OCR/AV/audio engines -- 2 lets a slow conversion not fully block a second job.
		Queues:         map[string]int{queue.QueuePandoc: 1},
		RetryDelayFunc: queue.RetryDelayFunc,
		// asynq defaults ShutdownTimeout to 8s, silently capping the graceful
		// window regardless of the pod's terminationGracePeriodSeconds.
		// Aligning it to the engine timeout plus margin lets a genuinely
		// long in-flight pandoc attempt survive SIGTERM instead of being
		// aborted+requeued (mirrors cmd/ocr-worker/main.go's own margin).
		ShutdownTimeout: engineTimeout + 10*time.Second,
	})

	log.Printf("🐙 pandoc-worker starting (queue=%s)", queue.QueuePandoc)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("pandoc-worker: %v", err)
	}

	// Prometheus /metrics is served on its own localhost-only listener,
	// separate from the public API_ADDR (D-19/T-04-13) — same trust-model
	// reasoning as the API process.
	metricsAddr := os.Getenv("METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = "127.0.0.1:9090"
	}
	metricsSrv := &http.Server{
		Addr:              metricsAddr,
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("📊 metrics listening on %s", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("metrics listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("🛑 shutting down pandoc-worker...")
	srv.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("metrics graceful shutdown failed: %v", err)
	}
	log.Println("bye 👋")
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(firstField(v)); err == nil {
			return n
		}
	}
	return def
}

// envInt64 reads an int64 env var (PANDOC_MAX_INPUT_BYTES), tolerant of a
// trailing inline comment via firstField (same convention as envInt/
// envDuration) -- mirrors cmd/ocr-worker/main.go's own envInt64 helper
// (OCR_MAX_IMAGE_PIXELS), the same "operator-tunable guard ceiling" class,
// here enforced by enforcePandocInputSize instead of an image/OCR-specific
// guard.
func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(firstField(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}

// buildPandocConverter constructs a CONFIGURED convert.PandocConverter from
// the resolved PANDOC_MAX_INPUT_BYTES value -- isolated as its own pure
// function (rather than an inline struct literal at the call site) so a
// unit test can assert the env value actually flows into the converter's
// guard field, without depending on the global convert.Default registry
// side effect (mirrors cmd/ocr-worker/main.go's own converter-builder
// precedent, this codebase's "argv/config builder is its own,
// unit-testable function" convention).
func buildPandocConverter(maxInputBytes int64) convert.PandocConverter {
	return convert.PandocConverter{
		MaxInputBytes: maxInputBytes,
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(firstField(v)); err == nil {
			return d
		}
	}
	return def
}

// envDurationSeconds reads a duration env var whose _SECONDS-suffixed name
// invites a bare integer-seconds value (cloned verbatim from
// cmd/ocr-worker/main.go/cmd/av-worker/main.go): time.ParseDuration alone
// REJECTS "2" ("missing unit in duration"), and the codebase-wide silent-
// fallback pattern would then quietly replace an operator's explicit value
// with the default. Accepts, in order: Go duration syntax ("2s") and bare
// non-negative integer seconds ("2"), both tolerant of a trailing inline
// comment via firstField.
func envDurationSeconds(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f := firstField(v)
	if d, err := time.ParseDuration(f); err == nil && d >= 0 {
		return d
	}
	if sec, err := strconv.Atoi(f); err == nil && sec >= 0 {
		return time.Duration(sec) * time.Second
	}
	log.Printf("⚠️ %s=%q is neither a duration (\"2s\") nor bare integer seconds (\"2\"); using default %v", key, f, def)
	return def
}

func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i]
		}
	}
	return s
}
