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
	"runtime"
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

	// D-01/D-03 (Phase 45, mirrors cmd/av-worker's D-09/Pitfall-4 re-Register
	// precedent): re-register a CONFIGURED OCRConverter, overriding the
	// zero-value registration converters.go's init() already performed --
	// documented last-write-wins semantics (Registry.Register is a bare map
	// assignment, converters.go). This MUST run before srv.Start(mux) below:
	// that is the point asynq's worker goroutines begin concurrently reading
	// the registered converter, so this single write must happen-before
	// every concurrent read (same happens-before boundary as
	// AV_MAX_DURATION_SECONDS/AV_DISK_SAFETY_FACTOR in cmd/av-worker/main.go).
	//
	// Phase 57, D-09: Registry.Register now PANICS on a triple collision
	// instead of silently last-write-winning -- this deliberate overwrite of
	// converters.go's own placeholder registration moved to Registry.Replace,
	// which performs the identical unconditional overwrite without the
	// occupancy check. Using Register here would panic on startup against
	// the very placeholder this re-registration exists to override.
	//
	// OCR_MAX_PAGES default (200) is PROVISIONAL -- Plan 03's measurement
	// finalizes the real ceiling from a measured p95 seconds-per-page run,
	// mirroring AV_MAX_DURATION_SECONDS's own provisional-then-measured
	// precedent. OCR_MAX_IMAGE_PIXELS reuses MAX_IMAGE_PIXELS's own 100
	// megapixel magnitude (cmd/api/main.go's decompression-bomb ceiling) as
	// its Claude's-Discretion default -- the same declared-dimension guard
	// class, just enforced by the OCR converter instead of the API layer.
	ocrMaxPages := envInt("OCR_MAX_PAGES", 200)
	ocrMaxPixels := uint64(envInt64("OCR_MAX_IMAGE_PIXELS", 100_000_000))
	convert.Default.Replace(buildOCRConverter(ocrMaxPages, ocrMaxPixels))

	// D-03 (Phase 45): tesseract's own OpenMP thread use is pinned to the
	// cgroup CPU quota via OMP_THREAD_LIMIT (mirrors av-worker's
	// resolveAVThreads -> -threads cgroup discipline), so tesseract honors
	// the container's CPU limit rather than the host core count. Must
	// complete before srv.Start(mux) below -- the same happens-before
	// boundary every other process-wide config write in this file uses;
	// tesseract itself only reads OMP_THREAD_LIMIT from its own process
	// environment at invocation time (os/exec.Command inherits the parent
	// process env unless overridden), so this write must land before any
	// asynq worker goroutine can shell out to tesseract.
	ompThreads, ompSource := resolveOCRThreadLimit()
	os.Setenv("OMP_THREAD_LIMIT", strconv.Itoa(ompThreads))
	log.Printf("🧵 OCR_MAX_PAGES=%d OCR_MAX_IMAGE_PIXELS=%d OMP_THREAD_LIMIT=%d (source=%s)",
		ocrMaxPages, ocrMaxPixels, ompThreads, ompSource)

	engineTimeout := envDuration("OCR_ENGINE_TIMEOUT", 300*time.Second) // 300s is purely a docker-compose.yml-unset fallback default -- the real, measured/finalized value arrives via Phase 45 Plan 03's supervised scripts/ocr-timing-measure.sh matrix run and is set explicitly in docker-compose.yml/.env.example (mirrors AV_ENGINE_TIMEOUT's own provisional-fallback-then-measured comment, cmd/av-worker/main.go).

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
	mux.HandleFunc(queue.TypeOCRConvert, worker.NewRunner(h, worker.OCRDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:    envInt("OCR_WORKER_CONCURRENCY", 1), // D-03 (Phase 45): one OCR job at a time (mirrors av/audio) -- avoids CPU oversubscription/CFS-thrashing on the cgroup-limited worker; a per-job OCR already streams pages sequentially. A concurrency > 1 is a measured-evidence future tweak, not v1.
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

// envInt64 reads an int64 env var (OCR_MAX_IMAGE_PIXELS, cast to uint64 by
// the caller), tolerant of a trailing inline comment via firstField (same
// convention as envInt/envDuration) -- mirrors cmd/api/main.go's own
// envInt64 helper (MAX_IMAGE_PIXELS), the same decompression-bomb guard
// class enforced here by the OCR converter instead of the API layer.
func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(firstField(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}

// buildOCRConverter constructs a CONFIGURED convert.OCRConverter from the
// resolved OCR_MAX_PAGES/OCR_MAX_IMAGE_PIXELS values -- isolated as its own
// pure function (rather than an inline struct literal at the call site) so
// a unit test can assert the env values actually flow into the converter's
// guard fields, without depending on the global convert.Default registry
// side effect (mirrors this codebase's "argv/config builder is its own,
// unit-testable function" convention, e.g. ocrTesseractArgs/whisperArgs).
func buildOCRConverter(maxPages int, maxPixels uint64) convert.OCRConverter {
	return convert.OCRConverter{
		MaxPages:       maxPages,
		MaxImagePixels: maxPixels,
	}
}

// resolveOCRThreadLimit mirrors cmd/av-worker/main.go's resolveAVThreads
// exactly (CgroupCPULimit -> runtime.NumCPU fallback), so tesseract's
// OMP_THREAD_LIMIT (D-03) reports the SAME thread count the cgroup CPU
// quota actually entitles this container to, not the host's full core
// count (PITFALLS.md Pitfall 5's threading discipline, applied to OCR's
// OpenMP-parallel LSTM engine instead of whisper.cpp's --threads flag). An
// OCR_THREADS operator override is deliberately out of scope here, same as
// av-worker's own resolver.
func resolveOCRThreadLimit() (int, string) {
	if n, ok := convert.CgroupCPULimit(); ok {
		return n, "cgroup"
	}
	return runtime.NumCPU(), "NumCPU fallback"
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
