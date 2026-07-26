// Command ocr-worker runs the OctoConv ocr-class worker: it consumes the ocr
// queue and executes tesseract/pdftoppm-based text/hOCR/ALTO extraction from
// images and scanned/image-only PDFs.
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

	// D-04 (mirrors cmd/av-worker/main.go): run the job:cancel Subscriber
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

	// OCRP-01/44-CONTEXT.md: unlike cmd/av-worker's re-Register of a
	// configured, env-tunable converter before it starts consuming tasks,
	// ocr-worker does NOT re-register OCRConverter -- converters.go's init()
	// already registered a zero-value OCRConverter{} (uncapped
	// MaxImagePixels/MaxPages this phase). OCR_MAX_PAGES/pixel-cap env wiring
	// is Phase 45's job.
	engineTimeout := envDuration("OCR_ENGINE_TIMEOUT", 300*time.Second) // provisional default; Phase 45 measures and finalizes the real ceiling (mirrors AV_ENGINE_TIMEOUT's own provisional-then-measured precedent).

	// PROG-03 (mirrors cmd/av-worker/main.go): the dual-gate progress-write
	// throttle (D-03) -- env-tunable, read here (not in internal/worker) and
	// wired via the SAME post-construction-setter convention; MUST complete
	// before srv.Start(mux) below, the happens-before boundary every other
	// process-wide config write in this file uses.
	progressMinInterval := envDurationSeconds("PROGRESS_MIN_INTERVAL_SECONDS", 2*time.Second)
	progressMinDeltaPct := envInt("PROGRESS_MIN_DELTA_PCT", 1)

	h := worker.NewHandler(
		repo,
		store,
		convert.Default,
		engineTimeout,
		nil, // webhookRepo — webhook-only; HandleOCRConvert never reads it
		nil, // deliverer — webhook-only; HandleOCRConvert never reads it
		qc,
		nil, // signingSecret — webhook-only; HandleOCRConvert never reads it
		0,   // presignTTL — webhook-only; HandleOCRConvert never reads it
		0,   // audioMaxDuration — 0 for every non-audio worker cmd (worker.go); OCR's guards are self-contained inside OCRConverter.Convert, not spliced through this parameter
	)

	// SC4 (44-CONTEXT.md): install the throttle BEFORE srv.Start(mux) below --
	// this is REQUIRED so process()'s ProgressConverter type-assert seam
	// actually takes the ConvertWithProgress path (page-granular ramp for a
	// multi-page scanned PDF, null->100 for a single image); leaving this
	// unset would silently disable OCR progress reporting entirely.
	h.SetProgressThrottle(progressMinInterval, progressMinDeltaPct)
	log.Printf("📈 PROGRESS_MIN_INTERVAL_SECONDS=%v PROGRESS_MIN_DELTA_PCT=%d", progressMinInterval, progressMinDeltaPct)

	// D-04 (mirrors cmd/av-worker/main.go/PUSH-02): wire a real
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

	// D-04/D-05 (mirrors cmd/av-worker/main.go): the stale-job sweep loop
	// runs solely in cmd/webhook-worker (under a Postgres advisory lock) — no
	// sweeper of any kind is constructed or run here.

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeOCRConvert, h.HandleOCRConvert)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:    envInt("OCR_WORKER_CONCURRENCY", 2),
		Queues:         map[string]int{queue.QueueOCR: 1},
		RetryDelayFunc: queue.RetryDelayFunc,
		// asynq defaults ShutdownTimeout to 8s, silently capping the graceful
		// window regardless of the pod's terminationGracePeriodSeconds.
		// Aligning it to the engine timeout plus margin lets a genuinely
		// long in-flight OCR attempt (a large multi-page scanned PDF)
		// survive SIGTERM instead of being aborted+requeued (mirrors
		// cmd/av-worker/main.go's own margin).
		ShutdownTimeout: engineTimeout + 10*time.Second,
	})

	log.Printf("🐙 ocr-worker starting (queue=%s)", queue.QueueOCR)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("ocr-worker: %v", err)
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
	log.Println("🛑 shutting down ocr-worker...")
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
// cmd/av-worker/main.go/cmd/audio-worker/main.go): time.ParseDuration alone
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
