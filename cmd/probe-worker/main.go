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

// probeSandboxDisabledEnv is the one value that turns filesystem confinement
// off, spelled out rather than defaulted -- mirrors pandocSandboxDisabledEnv
// (cmd/pandoc-worker/main.go): a deployment gets an unconfined engine only
// by saying so.
const probeSandboxDisabledEnv = "PROBE_SANDBOX_DISABLED"

// probeConcurrencyEnv names the SAME variable both ProbeConcurrencyGuard and
// asynq.Config.Concurrency read below -- a mismatch would let the guard
// check one value while the server actually ran at another.
const probeConcurrencyEnv = "PROBE_WORKER_CONCURRENCY"

func main() {
	// FIRST, before anything else in this process: when the converter execs
	// this binary as its sandbox helper, that invocation must establish the
	// ruleset and exec gs/pdfinfo/unzip/file, not connect to Postgres and
	// start a worker.
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The filesystem sandbox, decided ONCE at startup: a worker that cannot
	// confine gs/pdfinfo/unzip/file must not consume a queue. It would fail
	// every job -- or, far worse, run the engines unconfined while the
	// deployment believes otherwise. The paths it grants were MEASURED, not
	// reasoned: see docs/SANDBOX-PATHS.md and probeReadOnly.
	sandboxHelper, err := sandbox.ResolveHelper(probeSandboxDisabledEnv)
	if err != nil {
		log.Fatalf("probe sandbox: %v", err)
	}

	// CONVERTER RE-REGISTRATION, exactly as ocr-worker/av-worker/pandoc-worker
	// do it, and the reason this file used to say the opposite is worth
	// keeping rather than deleting.
	//
	// Until Phase 63 this comment read "SanitizeConverter has no such field:
	// sanitize takes no options and has no per-engine ceiling of its own",
	// and it was true. PROBE_MAX_PAGES is that ceiling (PROBE-02):
	// sanitize cost is per PAGE and linear (~2.0 s/page, measured in phase
	// 60), so an over-long document otherwise burns PROBE_ENGINE_TIMEOUT and
	// the whole PROBE_MAX_RETRY budget rediscovering the same thing.
	//
	// Replace, not Register: Register PANICS on a triple collision since
	// Phase 57 D-09, and converters.go already registered the zero-value
	// converter at init. Replace performs the identical unconditional
	// overwrite without the occupancy check.
	//
	// The default is convert.DefaultProbeMaxPages, where the derivation lives
	// -- ceil(2.0 s/page x 200 pages x 1.5) is also how PROBE_ENGINE_TIMEOUT
	// was derived, so the two describe one document bound rather than two.
	//
	// This is now the ONLY process that reads PROBE_MAX_PAGES. cmd/api read it
	// too, for an admission gate that was removed after six review rounds
	// (63-CONTEXT.md D-04, reversed): a page count cannot be derived reliably
	// from untrusted bytes there, because a page tree is a tree and a long
	// document declares several correct-but-different counts.
	//
	// This is the ONLY registration this process performs -- moved here
	// (before db.Connect, per D-11's "preflight before Postgres/queue") from
	// where it used to live further down, so there is exactly one write to
	// convert.Default in this file.
	probeMaxPages := envInt("PROBE_MAX_PAGES", convert.DefaultProbeMaxPages)
	conv := buildSanitizeConverter(probeMaxPages).WithSandbox(sandboxHelper)

	// D-05/CONF-06: probe's ruleset grants no shared /tmp at all (measured:
	// ghostscript honours TMPDIR), so this is a no-op today -- the guard is
	// keyed on the GRANT SET, not the class name, so it re-arms itself
	// automatically the moment /tmp is ever re-added to probeReadWrite.
	concurrency := envInt(probeConcurrencyEnv, 1)
	if err := convert.ProbeConcurrencyGuard(conv, concurrency, probeConcurrencyEnv); err != nil {
		log.Fatalf("probe sandbox: %v", err)
	}

	convert.Default.Replace(conv)
	log.Printf("📄 PROBE_MAX_PAGES=%d", probeMaxPages)
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  %s=true — the engines can read every file in this container", probeSandboxDisabledEnv)
	} else {
		log.Printf("🔒 probe sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	// Proves the ENGINES run under the rules, not merely that the rules can
	// be built -- both directions, by content, for all four engines (CONF-04):
	// gs/pdfinfo/unzip fail LOUDLY when denied, but file(1) exits 0 and
	// prints a non-MIME answer, so the exit status is never what decides
	// this.
	preflightCtx, cancelPreflight := context.WithTimeout(ctx, 60*time.Second)
	if err := convert.ProbePreflight(preflightCtx, conv); err != nil {
		cancelPreflight()
		log.Fatalf("probe sandbox preflight: %v", err)
	}
	cancelPreflight()
	if len(sandboxHelper) > 0 {
		log.Printf("🔒 probe sandbox proven: every probe engine ran inside the job directory and was denied a file outside it")
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

	// PROBE_ENGINE_TIMEOUT.
	//
	// MEASURED, as of Phase 60: scripts/probe-timeout-measure.sh against a real
	// corpus in the worker container gave 1.99-2.01 s/page across three points,
	// and ceil(2.0 s/page x 200 pages x 1.5) = 600s. See .env.example for the
	// run and the full derivation.
	//
	// This comment called it "a PLACEHOLDER, not a measurement" while the value
	// beside it already read 600s -- the same stale sentence was corrected in
	// internal/api/api.go and cmd/api/main.go and missed here, which is the
	// ordinary way a comment goes false: the change that invalidates it touches
	// three files and the sweep touches two.
	//
	// Never copied from 65s/742s/744s/753s -- copying a neighbour's timeout is
	// a recorded project antipattern (PITFALLS.md Pitfall 7), and 600s is
	// distinct from all four by measurement rather than by choice.
	//
	// This MUST read the same env var, with the same default, that
	// internal/queue/client.go's NewClient reads for ProbeUniqueTTL's
	// derivation (IN-02): a mismatch silently produces duplicate processing.
	engineTimeout := envDuration("PROBE_ENGINE_TIMEOUT", 600*time.Second)

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
		nil, // audioSandboxHelper — audio-only
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
		//
		// The SAME variable ProbeConcurrencyGuard already checked above --
		// a mismatch here would let the guard reason about one value while
		// asynq actually ran at another.
		Concurrency:     concurrency,
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

// buildSanitizeConverter constructs a CONFIGURED convert.SanitizeConverter
// from the resolved PROBE_MAX_PAGES value -- its own pure function rather than
// an inline struct literal, so a unit test can assert the env value actually
// reaches the converter's guard field without depending on the global
// convert.Default registry side effect. Mirrors cmd/ocr-worker's
// buildOCRConverter exactly, for the reason stated there: a bound that is
// configured and not plumbed is a bound that does not exist.
func buildSanitizeConverter(maxPages int) convert.SanitizeConverter {
	return convert.SanitizeConverter{MaxPages: maxPages}
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
