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
	"github.com/tileforge/octoconv/internal/webhook"
	"github.com/tileforge/octoconv/internal/worker"
)

// pandocSandboxDisabledEnv is the one value that turns filesystem
// confinement off, spelled out rather than defaulted -- mirrors
// imageSandboxDisabledEnv (cmd/worker/main.go): a deployment gets an
// unconfined engine only by saying so.
const pandocSandboxDisabledEnv = "PANDOC_SANDBOX_DISABLED"

func main() {
	// FIRST, before anything else in this process: when the converter execs
	// this binary as its sandbox helper, that invocation must establish the
	// ruleset and exec pandoc, not connect to Postgres and start a worker.
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The filesystem sandbox, decided ONCE at startup: a worker that cannot
	// confine pandoc must not consume a queue. It would fail every job -- or,
	// far worse, run the engine unconfined while the deployment believes
	// otherwise. The paths it grants were MEASURED, not reasoned: see
	// docs/SANDBOX-PATHS.md and pandocReadOnly.
	sandboxHelper, err := sandbox.ResolveHelper(pandocSandboxDisabledEnv)
	if err != nil {
		log.Fatalf("pandoc sandbox: %v", err)
	}
	pandocMaxInputBytes := envInt64("PANDOC_MAX_INPUT_BYTES", 50<<20)
	conv := buildPandocConverter(pandocMaxInputBytes).WithSandbox(sandboxHelper)
	convert.Default.Replace(conv)
	log.Printf("📏 PANDOC_MAX_INPUT_BYTES=%d", pandocMaxInputBytes)
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  %s=true — the engine can read every file in this container", pandocSandboxDisabledEnv)
	} else {
		log.Printf("🔒 pandoc sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	// Proves the ENGINE runs under the rules, not merely that the rules can be
	// built -- both directions, by content: a conversion INSIDE the job
	// directory succeeds and carries the expected output, and the identical
	// bytes OUTSIDE it are denied (CONF-04).
	preflightCtx, cancelPreflight := context.WithTimeout(ctx, 30*time.Second)
	if err := convert.PandocPreflight(preflightCtx, conv); err != nil {
		cancelPreflight()
		log.Fatalf("pandoc sandbox preflight: %v", err)
	}
	cancelPreflight()
	if len(sandboxHelper) > 0 {
		log.Printf("🔒 pandoc sandbox proven: the engine converted inside the job directory and was denied a file outside it")
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
	// convert.Default was already re-registered with a CONFIGURED,
	// sandbox-wired PandocConverter above, before db.Connect -- overriding
	// the zero-value registration converters.go's init() performed, via
	// Registry.Replace (Phase 57 D-09: Register PANICS on a triple collision,
	// so the deliberate overwrite of converters.go's own placeholder uses
	// Replace instead). That single write happens-before srv.Start(mux)
	// below, the point asynq's worker goroutines begin concurrently reading
	// the registered converter.
	//
	// PANDOC_MAX_INPUT_BYTES default (50 MiB) is Claude's Discretion
	// (48-CONTEXT.md): pandoc reads its input linearly with no exotic
	// pre-parse "bomb shape" to guard against (markupsniff.go's own
	// ErrPandocInputTooLarge doc comment), so this cap plus the engine
	// timeout below are the full defense-in-depth here, layered over the
	// API's own maxEngineBytes upload-time gate (Plan 02).
	// NOTE the 50 MiB default is an UNCONSTRAINED-HOST default, safe only where
	// memory is plentiful. Both deployed configs (docker-compose.yml and the
	// chart ConfigMap) override it to 3 MiB, because the measured cost is
	// ~300 MiB of anonymous memory per MiB of input at this markup density --
	// a 1Gi container cannot afford more (50-04 live OOMKill at 4.09 MB, then
	// evidence/blocker1-memory-probe-20260802.txt). Do not raise the deployed
	// value without re-running that probe.

	// PANDOC_ENGINE_TIMEOUT default of 90s is the UNMEASURED FALLBACK for a
	// host that sets no env -- pandoc is a fast, deterministic CPU-bound
	// engine unlike OCR/AV's heavier per-page/per-frame processing, so it is
	// deliberately smaller than OCR's own 300s provisional default. The
	// MEASURED value has since landed and is what both deployed configs set:
	// 65s = ceil(worst-cell p95 32.318s x 2.0), from Phase 49's supervised
	// scripts/pandoc-timing-measure.sh run (arch=arm64, cpus=2.0, memory=1g,
	// N=10), 92.8% under the 900s RECONCILER_ACTIVE_STALE_AFTER cap.
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
	mux.HandleFunc(queue.TypePandocConvert, worker.NewRunner(h, worker.PandocDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		// 1, not 2: pandoc is CPU-fast but MEMORY-heavy, so it belongs with the
		// 1-at-a-time OCR/AV/audio engines, not against them. Measured in the
		// pandoc-worker image at the deployed limits (cpus 2 / memory 1g) on a
		// worst-case 3.04 MB html->md: peak ANONYMOUS memory 909 MiB/1024 at
		// concurrency 1 but 1022 MiB/1024 at concurrency 2 -- two parallel
		// conversions OOMKill once this process's own RSS shares the cgroup.
		// Throughput comes from KEDA scale-out, not in-pod parallelism.
		// See .planning/phases/50-keda-helm-chart-integration/evidence/
		// blocker1-memory-probe-20260802.txt.
		Concurrency:    envInt("PANDOC_WORKER_CONCURRENCY", 1),
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
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f := firstField(v)
	n, err := strconv.ParseInt(f, 10, 64)
	// A silent fallback here is dangerous for PANDOC_MAX_INPUT_BYTES: the
	// default is the unconstrained-host 50 MiB, so a typo like "3MiB" would
	// quietly restore a value that OOM-kills this worker at ~4 MB of input in
	// its 1Gi container. A negative value is worse -- enforcePandocInputSize
	// only applies the cap when maxBytes > 0, so "-1" disables the guard
	// entirely. Both now warn loudly and fall back, matching
	// envDurationSeconds's own convention in this file.
	if err != nil {
		log.Printf("⚠️ %s=%q is not an integer; using default %d", key, f, def)
		return def
	}
	if n < 0 {
		log.Printf("⚠️ %s=%d is negative (a negative cap would DISABLE the guard); using default %d", key, n, def)
		return def
	}
	return n
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
