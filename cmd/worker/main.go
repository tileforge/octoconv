// Command worker runs the OctoConv image-class worker: it consumes the image
// queue and executes libvips conversions.
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

// imageSandboxDisabledEnv is the one value that turns filesystem confinement
// off, spelled out rather than defaulted: a deployment gets an unconfined
// engine only by saying so.
const imageSandboxDisabledEnv = "IMAGE_SANDBOX_DISABLED"

func main() {
	// FIRST, before anything else in this process: when the converter execs
	// this binary as its sandbox helper, that invocation must establish the
	// ruleset and exec the engine, not connect to Postgres and start a worker.
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The filesystem sandbox, decided ONCE at startup: a worker that cannot
	// confine its engine must not consume a queue. It would fail every job --
	// or, far worse, run the engine unconfined while the deployment believes
	// otherwise.
	//
	// The paths it grants were MEASURED, not reasoned: see
	// docs/SANDBOX-PATHS.md and libvipsReadOnly.
	sandboxHelper, err := sandbox.ResolveHelper(imageSandboxDisabledEnv)
	if err != nil {
		log.Fatalf("image sandbox: %v", err)
	}
	convert.Default.Replace(convert.ConfinedLibvips(sandboxHelper))
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  %s=true — the engine can read every file in this container", imageSandboxDisabledEnv)
	} else {
		log.Printf("🔒 image sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	// Proves the ENGINE runs under the rules, not merely that the rules can be
	// built. A kernel can answer the ABI query and still refuse restrict_self.
	preflightCtx, cancelPreflight := context.WithTimeout(ctx, 30*time.Second)
	if err := convert.LibvipsPreflight(preflightCtx, sandboxHelper); err != nil {
		cancelPreflight()
		log.Fatalf("image sandbox preflight: %v", err)
	}
	cancelPreflight()

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

	// Webhook delivery now lives solely in cmd/webhook-worker (D-03/D-07);
	// image-worker neither delivers nor signs webhooks, so no signing secret
	// is read here and webhookRepo/deliverer are passed as nil — inert for
	// HandleImageConvert, which never touches h.webhookRepo/h.deliverer/
	// h.signingSecret.
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
		envDuration("ENGINE_TIMEOUT", 120*time.Second),
		nil, // webhookRepo — webhook-only; HandleImageConvert never reads it
		nil, // deliverer — webhook-only; HandleImageConvert never reads it
		qc,
		nil, // signingSecret — webhook-only; HandleImageConvert never reads it
		0,   // presignTTL — webhook-only; HandleImageConvert never reads it
		0,   // audioMaxDuration — audio-only; HandleImageConvert never reads it
		nil, // audioSandboxHelper — audio-only
	)

	// D-03/D-04: the stale-job sweep loop runs solely in cmd/webhook-worker
	// (under the Postgres advisory lock) — no sweeper of any kind is
	// constructed or run here.

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeImageConvert, worker.NewRunner(h, worker.ImageDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:    envInt("WORKER_CONCURRENCY", 4),
		Queues:         map[string]int{queue.QueueImage: 4},
		RetryDelayFunc: queue.RetryDelayFunc,
		// Pattern 2: asynq defaults ShutdownTimeout to 8s, silently capping
		// the graceful window regardless of the pod's
		// terminationGracePeriodSeconds (150s for image, Phase 24). Aligning
		// it to ENGINE_TIMEOUT+margin lets a genuinely long in-flight
		// conversion survive SIGTERM instead of being aborted+requeued.
		ShutdownTimeout: envDuration("ENGINE_TIMEOUT", 120*time.Second) + 10*time.Second,
	})

	// KEDA-01/D-01: the queue-depth collector now lives solely on the
	// always-on api process (cmd/api/main.go) — a worker Deployment scaled to
	// genuine 0 replicas by KEDA would otherwise have no pod exposing the
	// metric KEDA needs to scale it back up. /metrics here still serves the
	// promauto-registered job/duration metrics; the endpoint itself is
	// unchanged.

	log.Printf("🐙 worker starting (queue=%s)", queue.QueueImage)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("worker: %v", err)
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
	log.Println("🛑 shutting down worker...")
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
