// Command probe-worker runs the OctoConv probe-class worker: it consumes the
// probe queue and executes the sanitize operation -- rebuilding PDF, OOXML and
// image uploads with the enumerated classes of construct removed, then
// verifying their absence with an implementation that did not do the removing.
//
// It is the eighth engine class and the first whose queue carries no
// conversion at all. Everything about its wiring is the shape every worker cmd
// already has; what differs is documented where it differs.
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

	// Same job:cancel subscriber every other worker cmd runs, against the
	// same process-wide cancel.Default registry process() populates.
	cancelRDB := redis.NewClient(&redis.Options{Addr: redisOpt.Addr})
	defer cancelRDB.Close()
	go cancel.NewSubscriber(cancelRDB, cancel.Default).Run(ctx)

	qc, err := queue.NewClient()
	if err != nil {
		log.Fatalf("queue client: %v", err)
	}
	defer qc.Close()

	repo := jobs.NewRepo(pool)

	// NO CONVERTER RE-REGISTRATION HERE, and that is a difference worth
	// stating rather than leaving as an absence.
	//
	// ocr-worker, av-worker and pandoc-worker all call convert.Default.Replace
	// at startup to install a CONFIGURED converter, because each carries a
	// runtime ceiling (max pixels, max duration, max input bytes) that only
	// the process env knows. SanitizeConverter has no such field: sanitize
	// takes no options and has no per-engine ceiling of its own, so the
	// registration converters.go performed at init is already the final one.
	// Adding a Replace call "for symmetry" would be a no-op overwrite whose
	// only effect is to suggest a configuration knob that does not exist.

	// PROBE_ENGINE_TIMEOUT.
	//
	// The default below is a PLACEHOLDER, not a measurement, and this build
	// says so everywhere the number appears -- .env.example, the API's
	// estimate provenance table, and here. The measured value comes from
	// scripts/probe-timeout-measure.sh run against an operator's own corpus
	// (Phase 60 SC5); copying 65s/742s/744s/753s from a neighbouring engine
	// is recorded as a project antipattern (PITFALLS.md Pitfall 7) and is
	// exactly what this placeholder must not become by being left alone.
	//
	// This MUST read the same env var, with the same default, that
	// internal/queue/client.go's NewClient reads for ProbeUniqueTTL's
	// derivation (IN-02): a mismatch silently produces duplicate processing.
	engineTimeout := envDuration("PROBE_ENGINE_TIMEOUT", 180*time.Second)

	progressMinInterval := envDurationSeconds("PROGRESS_MIN_INTERVAL_SECONDS", 2*time.Second)
	progressMinDeltaPct := envInt("PROGRESS_MIN_DELTA_PCT", 1)

	h := worker.NewHandler(
		repo,
		store,
		convert.Default,
		engineTimeout,
		nil, // webhookRepo — webhook-only
		nil, // deliverer — webhook-only
		qc,
		nil, // signingSecret — webhook-only
		0,   // presignTTL — webhook-only
		0,   // audioMaxDuration — 0 for every non-audio worker cmd
	)

	// SanitizeConverter does not implement convert.ProgressConverter (the
	// ghostscript passes give no page-granular signal), so this throttle is
	// inert for probe jobs. Wired anyway, for the same reason pandoc-worker
	// wires it: every worker cmd having the same shape is worth more than
	// each one being individually minimal.
	h.SetProgressThrottle(progressMinInterval, progressMinDeltaPct)
	log.Printf("📈 PROGRESS_MIN_INTERVAL_SECONDS=%v PROGRESS_MIN_DELTA_PCT=%d", progressMinInterval, progressMinDeltaPct)

	webhookSigningSecret := []byte(os.Getenv("WEBHOOK_SIGNING_SECRET"))
	progressWebhookTimeout := envDuration("PROGRESS_WEBHOOK_TIMEOUT", 1500*time.Millisecond)
	if len(webhookSigningSecret) > 0 {
		h.SetProgressWebhook(webhook.NewDeliverer(), webhookSigningSecret, progressWebhookTimeout)
		log.Printf("🔔 progress webhooks enabled (PROGRESS_WEBHOOK_TIMEOUT=%v)", progressWebhookTimeout)
	} else {
		log.Printf("🔕 progress webhooks disabled (WEBHOOK_SIGNING_SECRET unset)")
	}

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeProbeSanitize, worker.NewRunner(h, worker.ProbeDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		// 1, with the OCR/AV/audio/pandoc engines rather than against them:
		// a sanitize job runs two ghostscript passes plus a pdfcpu parse, all
		// of which hold the whole document, and the container is limited to
		// 2 CPU / 1 GiB. Throughput comes from KEDA scale-out.
		//
		// This is a REASONED default, not a measured one, and it shares that
		// status with the timeout above. Both are what the operator
		// checkpoint exists to replace.
		Concurrency:     envInt("PROBE_WORKER_CONCURRENCY", 1),
		Queues:          map[string]int{queue.QueueProbe: 1},
		RetryDelayFunc:  queue.RetryDelayFunc,
		ShutdownTimeout: engineTimeout + 10*time.Second,
	})

	log.Printf("🐙 probe-worker starting (queue=%s)", queue.QueueProbe)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("probe-worker: %v", err)
	}

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
	log.Println("🛑 shutting down probe-worker...")
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

// envDurationSeconds accepts Go duration syntax ("2s") and bare integer
// seconds ("2"), cloned from cmd/pandoc-worker/main.go for the reason stated
// there: time.ParseDuration alone rejects "2", and the codebase-wide silent
// fallback would then replace an operator's explicit value with the default.
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
