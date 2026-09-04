// Command tectonic-worker runs the OctoConv tex-class worker: it consumes the
// tectonic queue and typesets latex into pdf.
//
// It is the ninth engine class, and the only one whose input is a PROGRAM
// rather than a document. Everything about its wiring is the shape every other
// worker cmd already has; what differs is documented where it differs.
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

	// CONVERTER RE-REGISTRATION, the same shape ocr/av/pandoc/probe use: the
	// registry entry converters.go installed at init carries no bundle, and a
	// converter with no bundle REFUSES to run rather than falling back to
	// tectonic's default, which is a network fetch (TEX-02).
	//
	// Replace, not Register: Register panics on a triple collision since Phase
	// 57 D-09, and this is a deliberate overwrite of that placeholder.
	bundlePath := os.Getenv(convert.TectonicBundleEnv)
	if bundlePath == "" {
		bundlePath = convert.DefaultTectonicBundlePath
	}
	if _, err := os.Stat(bundlePath); err != nil {
		// Fail at STARTUP rather than per job. A worker that cannot typeset
		// anything should not sit consuming a queue and failing every task --
		// that turns one misconfiguration into a stream of client-visible
		// failures, each of which looks like a bad document.
		log.Fatalf("tectonic bundle: %v (set %s)", err, convert.TectonicBundleEnv)
	}
	// TECTONIC_MAX_OUTPUT_BYTES bounds the PRODUCED PDF, which the 2 MiB input
	// ceiling does not: TeX ships pages out and frees the memory, so 198 bytes
	// of source measured 6 040 163 bytes of PDF in 16 seconds. Read HERE and
	// nowhere else, the way PROBE_MAX_PAGES is -- a second reader is a second
	// value that can disagree.
	maxOutputBytes := envInt64("TECTONIC_MAX_OUTPUT_BYTES", convert.DefaultTectonicMaxOutputBytes)
	convert.Default.Replace(buildTectonicConverter(bundlePath, maxOutputBytes))
	log.Printf("📚 %s=%s", convert.TectonicBundleEnv, bundlePath)
	log.Printf("📄 TECTONIC_MAX_OUTPUT_BYTES=%d", maxOutputBytes)

	// TECTONIC_ENGINE_TIMEOUT.
	//
	// MEASURED 2026-09-04 (scripts/tectonic-timeout-measure.sh, evidence under
	// the phase directory), in THIS image at the chart's own 2 CPU / 1 GiB:
	//
	//	ceil(25.76 s/pass x 7 passes x 1.5) = 271s
	//
	// 25.76 s/pass is measured on a 2 143 444-byte document -- 2.2% ABOVE the
	// 2 097 152-byte input ceiling (api.defaultMaxEngineBytes) -- rather than on
	// a smaller one scaled up, so the per-pass cost covers marginally more work
	// than this deployment will ever admit. 7 is the ENGINE's own rerun maximum
	// -- it stops and says "stopping at 6 passes" -- so the bound holds for any
	// document, not for the corpus.
	// Deliberately not a p95: with pass counts of 2, 3 and 7 for the same body
	// text, a percentile measures the corpus's mix of classes.
	//
	// Never copied from 65s/600s/742s/744s/753s -- copying a neighbour's
	// timeout is a recorded project antipattern (PITFALLS.md Pitfall 7).
	//
	// This MUST read the same env var, with the same default, that
	// internal/queue/client.go's NewClient reads for TectonicUniqueTTL's
	// derivation (IN-02): a mismatch silently produces duplicate processing.
	engineTimeout := envDuration("TECTONIC_ENGINE_TIMEOUT", 271*time.Second)

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

	// TectonicConverter does not implement convert.ProgressConverter, so this
	// throttle is inert for tex jobs. It could not be otherwise: tectonic
	// reruns until convergence and does not know in advance how many passes a
	// document needs, so there is no denominator to report progress against --
	// which is the same fact 64-05's timeout measurement has to work around.
	// Wired anyway, for the reason pandoc-worker gives: every worker cmd having
	// the same shape is worth more than each one being individually minimal.
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
	mux.HandleFunc(queue.TypeTectonicConvert, worker.NewRunner(h, worker.TectonicDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		// 1, with the OCR/AV/audio/pandoc engines rather than against them:
		// a sanitize job runs two ghostscript passes plus a pdfcpu parse, all
		// of which hold the whole document, and the container is limited to
		// 2 CPU / 1 GiB. Throughput comes from KEDA scale-out.
		//
		// This is a REASONED default, not a measured one, and it shares that
		// status with the timeout above. Both are what the operator
		// checkpoint exists to replace.
		Concurrency:     envInt("TECTONIC_WORKER_CONCURRENCY", 1),
		Queues:          map[string]int{queue.QueueTectonic: 1},
		RetryDelayFunc:  queue.RetryDelayFunc,
		ShutdownTimeout: engineTimeout + 10*time.Second,
	})

	log.Printf("🐙 tectonic-worker starting (queue=%s)", queue.QueueTectonic)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("tectonic-worker: %v", err)
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
	log.Println("🛑 shutting down tectonic-worker...")
	srv.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("metrics graceful shutdown failed: %v", err)
	}
	log.Println("bye 👋")
}

// buildTectonicConverter constructs a CONFIGURED convert.TectonicConverter
// from the resolved bundle path -- its own pure function rather than
// an inline struct literal, so a unit test can assert the env value actually
// reaches the converter's guard field without depending on the global
// convert.Default registry side effect. Mirrors cmd/ocr-worker's
// buildOCRConverter exactly, for the reason stated there: a bound that is
// configured and not plumbed is a bound that does not exist.
func buildTectonicConverter(bundlePath string, maxOutputBytes int64) convert.TectonicConverter {
	return convert.TectonicConverter{BundlePath: bundlePath, MaxOutputBytes: maxOutputBytes}
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(firstField(v), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
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
