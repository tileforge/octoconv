// Command document-worker runs the OctoConv document-class worker: it
// consumes the document queue and executes LibreOffice conversions.
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

	"github.com/apaderin/octoconv/internal/cancel"
	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/db"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/queue"
	"github.com/apaderin/octoconv/internal/sandbox"
	"github.com/apaderin/octoconv/internal/storage"
	"github.com/apaderin/octoconv/internal/worker"
)

// documentSandboxDisabledEnv is the one value that turns filesystem confinement
// off, spelled out rather than defaulted -- mirrors probeSandboxDisabledEnv: a
// deployment gets unconfined engines only by saying so.
const documentSandboxDisabledEnv = "DOCUMENT_SANDBOX_DISABLED"

// documentConcurrencyEnv names the SAME variable both convert.ConfinedDocument's
// guard and asynq.Config.Concurrency read below -- a mismatch would let the
// guard check one value while the server actually ran at another.
const documentConcurrencyEnv = "DOCUMENT_WORKER_CONCURRENCY"

// documentPreflightTimeout bounds the startup preflight. 70-02 measured the
// preflight-shaped sequence (two conversions, one validation, both negatives)
// at 6.0 s on the final sets, cold JVM included (5.9-6.3 s across runs).
// Twice that, rounded up to the next 10 s, is 20 s; the floor of 60 s is the
// one every other class's preflight uses, and a cold node is slower than the
// measuring host.
const documentPreflightTimeout = 60 * time.Second

func main() {
	// FIRST, before anything else in this process: when the converter execs
	// this binary as its sandbox helper, that invocation must establish the
	// ruleset and exec soffice or verapdf, not connect to Postgres and start a
	// worker.
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The filesystem sandbox, decided ONCE at startup: a worker that cannot
	// confine soffice and veraPDF must not consume a queue. It would fail every
	// job -- or, far worse, run the engines unconfined while the deployment
	// believes otherwise. The paths it grants were MEASURED, not reasoned: see
	// .planning/research/SANDBOX-PATHS.md and documentsandbox.go. The refusal is
	// sandbox.ResolveHelper's alone: no architecture or emulation detection of
	// any kind lives here (D-07).
	sandboxHelper, err := sandbox.ResolveHelper(documentSandboxDisabledEnv)
	if err != nil {
		log.Fatalf("document sandbox: %v", err)
	}

	// D-09/CONF-06: soffice's measured ruleset grants the container's shared
	// /tmp read-write, so a confined worker configured above one task at a time
	// refuses to start, naming DOCUMENT_WORKER_CONCURRENCY. The default is 1 for
	// the same reason; scale with replicas. This is the SAME value asynq uses.
	concurrency := envInt(documentConcurrencyEnv, 1)
	log.Printf("📄 %s=%d", documentConcurrencyEnv, concurrency)
	conv, err := convert.ConfinedDocument(sandboxHelper, concurrency, documentConcurrencyEnv)
	if err != nil {
		log.Fatalf("document sandbox: %v", err)
	}

	// Phase 23 (veraPDF validation) D-04/WARNING-3: VERAPDF_TIMEOUT is read
	// ONLY here (env-only-in-main, mirroring engineTimeout's threading into
	// NewHandler below) and injected into internal/convert via a setter --
	// internal/convert never calls os.Getenv directly. Moved up from below the
	// handler: the preflight runs ValidatePDFA, so the timeout must be set
	// before it, and before srv.Start(mux), the point at which asynq's worker
	// goroutines begin concurrently reading verapdfTimeout (this single write
	// happens-before every concurrent read, no mutex needed).
	convert.SetVeraPDFTimeout(envDuration("VERAPDF_TIMEOUT", 60*time.Second))

	// Replace, not Register: converters.go already registered the zero-value
	// LibreOfficeConverter at init, and Register panics on a triple collision.
	// This is the ONLY registration this process performs.
	convert.Default.Replace(conv)
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  %s=true — the engines can read every file in this container", documentSandboxDisabledEnv)
	} else {
		log.Printf("🔒 document sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	// Proves BOTH engines run under their rulesets, not merely that the rules
	// can be built -- both directions, judged by content, before Postgres or
	// the queue is touched (CONF-04): soffice is silent when denied and veraPDF
	// exits the same code for a verdict and a denial, so exit status is never
	// what decides this.
	preflightCtx, cancelPreflight := context.WithTimeout(context.Background(), documentPreflightTimeout)
	if err := convert.DocumentPreflight(preflightCtx, conv); err != nil {
		cancelPreflight()
		log.Fatalf("document sandbox preflight: %v", err)
	}
	cancelPreflight()
	if len(sandboxHelper) > 0 {
		log.Printf("🔒 document sandbox proven: soffice and veraPDF ran inside the job directory and were denied a file outside it")
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
		envDuration("DOCUMENT_ENGINE_TIMEOUT", 300*time.Second),
		nil, // webhookRepo — webhook-only; HandleDocumentConvert never reads it
		nil, // deliverer — webhook-only; HandleDocumentConvert never reads it
		qc,
		nil, // signingSecret — webhook-only; HandleDocumentConvert never reads it
		0,   // presignTTL — webhook-only; HandleDocumentConvert never reads it
		0,   // audioMaxDuration — audio-only; HandleDocumentConvert never reads it
		nil, // audioSandboxHelper — audio-only
	)

	// D-04/D-05: the stale-job sweep loop runs solely in cmd/webhook-worker
	// (under a Postgres advisory lock) — no sweeper of any kind is
	// constructed or run here, avoiding a double-sweep race between
	// independent sweep loops recovering the same stranded job.

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeDocumentConvert, worker.NewRunner(h, worker.DocumentDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:    concurrency,
		Queues:         map[string]int{queue.QueueDocument: 1},
		RetryDelayFunc: queue.RetryDelayFunc,
		// Pattern 2: asynq defaults ShutdownTimeout to 8s, silently capping
		// the graceful window regardless of the pod's
		// terminationGracePeriodSeconds (330s for document, Phase 24).
		// Aligning it to DOCUMENT_ENGINE_TIMEOUT+margin lets a genuinely long
		// in-flight LibreOffice conversion survive SIGTERM instead of being
		// aborted+requeued.
		ShutdownTimeout: envDuration("DOCUMENT_ENGINE_TIMEOUT", 300*time.Second) + 10*time.Second,
	})

	// KEDA-01/D-01: the queue-depth collector now lives solely on the
	// always-on api process (cmd/api/main.go) — a worker Deployment scaled to
	// genuine 0 replicas by KEDA would otherwise have no pod exposing the
	// metric KEDA needs to scale it back up. /metrics here still serves the
	// promauto-registered job/duration metrics; the endpoint itself is
	// unchanged.

	log.Printf("🐙 document-worker starting (queue=%s)", queue.QueueDocument)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("document-worker: %v", err)
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
	log.Println("🛑 shutting down document-worker...")
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
