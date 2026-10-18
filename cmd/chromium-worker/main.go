// Command chromium-worker runs the OctoConv html-class worker: it consumes
// the html queue and executes chromium-headless-shell HTML->PDF conversions.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/tileforge/octoconv/internal/cancel"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/queue"
	"github.com/tileforge/octoconv/internal/sandbox"
	"github.com/tileforge/octoconv/internal/storage"
	"github.com/tileforge/octoconv/internal/worker"
)

// htmlSandboxDisabledEnv is the one value that turns filesystem confinement
// off, spelled out rather than defaulted -- mirrors pandocSandboxDisabledEnv
// (cmd/pandoc-worker/main.go): a deployment gets an unconfined engine only by
// saying so.
const htmlSandboxDisabledEnv = "HTML_SANDBOX_DISABLED"

// htmlConcurrencyEnv names the variable D-03's guard checks -- the SAME
// variable this file also passes as asynq.Config's Concurrency below, so the
// guard always evaluates the value the worker will actually run with.
const htmlConcurrencyEnv = "HTML_WORKER_CONCURRENCY"

func main() {
	// FIRST, before anything else in this process: when the converter execs
	// this binary as its sandbox helper, that invocation must establish the
	// ruleset and exec chromium-headless-shell, not connect to Postgres and
	// start a worker.
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The filesystem sandbox, decided ONCE at startup: a worker that cannot
	// confine chromium must not consume a queue. The paths it grants were
	// MEASURED, not reasoned -- see .planning/research/SANDBOX-PATHS.md's
	// "html" section and the CONF-06 /tmp decision beside it.
	sandboxHelper, err := sandbox.ResolveHelper(htmlSandboxDisabledEnv)
	if err != nil {
		log.Fatalf("html sandbox: %v", err)
	}
	// D-01: one task per container while confined. A confined worker configured
	// to anything but 1 (above 1, or below 1, which asynq runs as the CPU count)
	// refuses to start in ConfinedChromium below.
	concurrency := envInt(htmlConcurrencyEnv, 1)
	log.Printf("🧵 %s=%d", htmlConcurrencyEnv, concurrency)
	conv, err := convert.ConfinedChromium(sandboxHelper, concurrency, htmlConcurrencyEnv)
	if err != nil {
		log.Fatalf("html sandbox: %v", err)
	}
	convert.Default.Replace(conv)
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  %s=true — the engine can read every file in this container", htmlSandboxDisabledEnv)
	} else {
		log.Printf("🔒 html sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	// Proves the ENGINE runs under the rules, not merely that the rules can be
	// built -- both directions, by PDF content (D-06/D-07): a conversion
	// INSIDE the job directory succeeds and carries the marker, and the
	// identical bytes OUTSIDE it are denied.
	preflightCtx, cancelPreflight := context.WithTimeout(ctx, 60*time.Second)
	if err := convert.HTMLPreflight(preflightCtx, conv); err != nil {
		cancelPreflight()
		log.Fatalf("html sandbox preflight: %v", err)
	}
	cancelPreflight()
	if len(sandboxHelper) > 0 {
		log.Printf("🔒 html sandbox proven: the engine rendered inside the job directory and was denied a file outside every grant")
	}

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

	// D-04 (Plan 03): run the job:cancel Subscriber against the SAME
	// process-wide cancel.Default registry process()'s cancel.Default.
	// Register/Deregister calls populate (internal/worker/worker.go) -- a
	// received cancel cancels the exact in-flight attempt this worker is
	// running for that job id (no-op otherwise, D-04). Bound to the process
	// signal ctx so Run returns promptly on shutdown (T-39-09).
	cancelRDB := redis.NewClient(&redis.Options{Addr: redisOpt.Addr})
	defer cancelRDB.Close()
	go cancel.NewSubscriber(cancelRDB, cancel.Default).Run(ctx)

	qc, err := queue.NewClient()
	if err != nil {
		log.Fatalf("queue client: %v", err)
	}
	defer qc.Close()

	repo := jobs.NewRepo(pool)

	h := worker.NewHandler(
		repo,
		store,
		convert.Default,
		envDuration("HTML_ENGINE_TIMEOUT", 60*time.Second),
		nil, // webhookRepo — webhook-only; HandleHTMLConvert never reads it
		nil, // deliverer — webhook-only; HandleHTMLConvert never reads it
		qc,
		nil, // signingSecret — webhook-only; HandleHTMLConvert never reads it
		0,   // presignTTL — webhook-only; HandleHTMLConvert never reads it
		0,   // audioMaxDuration — audio-only; HandleHTMLConvert never reads it
		nil, // audioSandboxHelper — audio-only
	)

	// chromium-worker must NOT run the reconciler sweeper: the stale-job
	// sweep loop runs solely in cmd/webhook-worker (under a Postgres
	// advisory lock, D-04) — no sweeper of any kind is constructed or run
	// here, avoiding a double-sweep race between independent sweep loops
	// recovering the same stranded job.

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeHTMLConvert, worker.NewRunner(h, worker.HTMLDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:    concurrency,
		Queues:         map[string]int{queue.QueueHTML: 1},
		RetryDelayFunc: queue.RetryDelayFunc,
		// Pattern 2: asynq defaults ShutdownTimeout to 8s, silently capping
		// the graceful window regardless of the pod's
		// terminationGracePeriodSeconds (90s for html, Phase 24). Aligning it
		// to HTML_ENGINE_TIMEOUT+margin lets a genuinely long in-flight
		// chromium render survive SIGTERM instead of being aborted+requeued.
		ShutdownTimeout: envDuration("HTML_ENGINE_TIMEOUT", 60*time.Second) + 10*time.Second,
	})

	// KEDA-01/D-01: the queue-depth collector now lives solely on the
	// always-on api process (cmd/api/main.go) — a worker Deployment scaled to
	// genuine 0 replicas by KEDA would otherwise have no pod exposing the
	// metric KEDA needs to scale it back up. /metrics here still serves the
	// promauto-registered job/duration metrics; the endpoint itself is
	// unchanged.

	log.Printf("🐙 chromium-worker starting (queue=%s)", queue.QueueHTML)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("chromium-worker: %v", err)
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
	log.Println("🛑 shutting down chromium-worker...")
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

func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i]
		}
	}
	return s
}
