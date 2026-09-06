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
	"fmt"
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
	"github.com/apaderin/octoconv/internal/webhook"
	"github.com/apaderin/octoconv/internal/worker"
)

// sandboxExecMode is the argv[1] that turns this binary into the thing that
// applies the sandbox and then BECOMES the engine.
//
// It has to work this way. Landlock restricts the calling process and cannot be
// applied to another one, so something must apply the rules and then exec
// tectonic in the same process -- and the only binary guaranteed to be present
// in this image is this one. os/exec offers no hook between fork and exec that
// can run Go, which is why this is a mode rather than a callback.
//
// Not a subcommand anyone should call: it is an implementation detail of
// convert.TectonicConverter, which builds the argv.
const sandboxExecMode = "__sandbox-exec"

// sandboxSetupExit is the exit status meaning "the ENGINE never ran".
//
// 78 is sysexits.h's EX_CONFIG. The value matters only in that it is not 1 and
// not any status tectonic produces; convert.ErrTeXSandboxUnavailable is the
// other half of this contract and the two are held together by a test.
const sandboxSetupExit = convert.TectonicSandboxSetupExit

// fatalSandbox reports on stderr and exits with the status that means the
// engine never started.
func fatalSandbox(format string, args ...any) {
	fmt.Fprintf(os.Stderr, sandboxExecMode+": "+format+"\n", args...)
	os.Exit(sandboxSetupExit)
}

// runSandboxExec applies the ruleset from argv and execs the rest.
//
//	tectonic-worker __sandbox-exec rw:/tmp/job ro:/opt/tectonic ... -- /usr/local/bin/tectonic ...
//
// It exits with sandboxSetupExit, never 1: a sandbox that cannot be established
// is an infrastructure fault the job should be RETRIED on, and tectonic's own
// failure exit is 1. Sharing a code would report a broken node to the client as
// a document that failed to typeset, and never retry it.
func runSandboxExec(argv []string) {
	p, cmd, err := sandbox.ParseSpec(argv)
	if err != nil {
		fatalSandbox("%v", err)
	}
	if err := sandbox.Apply(p); err != nil {
		// FAIL CLOSED. Running the engine unconfined because the confinement
		// could not be established is exactly the outcome this exists to
		// prevent, and it would be invisible.
		fatalSandbox("%v", err)
	}
	restricted := sandbox.ThreadID()

	// The rules live on the THREAD that established them, and execve carries
	// the security state of the thread that calls it. Apply locks the thread
	// so this cannot drift; this asserts it rather than trusting it, because
	// the failure -- the engine running unsandboxed -- produces a correct PDF
	// and no error at all.
	if now := sandbox.ThreadID(); now != restricted {
		fatalSandbox("thread changed between applying the sandbox (%d) and exec (%d): "+
			"the engine would run unconfined", restricted, now)
	}
	if err := syscall.Exec(cmd[0], cmd, os.Environ()); err != nil {
		fatalSandbox("exec %s: %v", cmd[0], err)
	}
}

func main() {
	// Before flags, before config, before anything: this mode never returns.
	if len(os.Args) > 1 && os.Args[1] == sandboxExecMode {
		runSandboxExec(os.Args[2:])
		return
	}

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
	if err := convert.ValidateTectonicBundle(bundlePath); err != nil {
		// Fail at STARTUP rather than per job. A worker that cannot typeset
		// anything should not sit consuming a queue and failing every task --
		// that turns one misconfiguration into a stream of client-visible
		// failures, each of which looks like a bad document.
		//
		// os.Stat was not enough to keep that promise: pointed at the DIRECTORY
		// /opt/tectonic this process started, logged its bundle and began
		// consuming the queue (PR #16, codex round 3, reproduced before the
		// fix). The check now OPENS the file, under this process's own
		// identity -- the worker runs as nobody, and a bundle readable only by
		// root stats perfectly.
		log.Fatalf("tectonic bundle: %v (set %s)", err, convert.TectonicBundleEnv)
	}
	// TECTONIC_MAX_OUTPUT_BYTES bounds the PRODUCED PDF, which the 2 MiB input
	// ceiling does not: TeX ships pages out and frees the memory, so 198 bytes
	// of source measured 6 040 163 bytes of PDF in 16 seconds. Read HERE and
	// nowhere else, the way PROBE_MAX_PAGES is -- a second reader is a second
	// value that can disagree.
	maxOutputBytes := envInt64("TECTONIC_MAX_OUTPUT_BYTES", convert.DefaultTectonicMaxOutputBytes)

	// The filesystem sandbox, decided ONCE at startup for the same reason the
	// bundle is: a worker that cannot confine the engine should not consume a
	// queue and fail every task, and -- much worse -- must not quietly run it
	// unconfined.
	//
	// --untrusted refuses \write18 and does NOT confine reads; that was
	// measured, not assumed (PR #19 typeset /etc/hostname into a client's PDF).
	// So this is the control that removes the read, and it is on unless
	// somebody says otherwise in writing.
	sandboxHelper, err := resolveSandboxHelper()
	if err != nil {
		log.Fatalf("tectonic sandbox: %v", err)
	}
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  TECTONIC_SANDBOX_DISABLED=true — the engine can read every file this container can")
	} else {
		log.Printf("🔒 tectonic sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	convert.Default.Replace(buildTectonicConverter(bundlePath, maxOutputBytes, sandboxHelper))
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
func buildTectonicConverter(bundlePath string, maxOutputBytes int64, sandboxHelper []string) convert.TectonicConverter {
	return convert.TectonicConverter{
		BundlePath:     bundlePath,
		MaxOutputBytes: maxOutputBytes,
		SandboxHelper:  sandboxHelper,
	}
}

// sandboxDisabledEnv is the escape hatch, and it is deliberately awkward.
//
// It exists because a kernel without Landlock is a real thing (a managed node
// pool on an old LTS, a developer running the worker on a Mac) and "the
// service does not start" is a worse answer there than "the service starts and
// says what it gave up". It is NOT a default: the operator has to write it
// down, and the log line it produces says what it costs.
const sandboxDisabledEnv = "TECTONIC_SANDBOX_DISABLED"

// resolveSandboxHelper returns the argv prefix the converter should run the
// engine through, or an empty slice if the sandbox is explicitly disabled.
//
// An error means the sandbox was WANTED and cannot be established, and the
// caller turns that into a refusal to start.
func resolveSandboxHelper() ([]string, error) {
	if os.Getenv(sandboxDisabledEnv) == "true" {
		return nil, nil
	}
	if !sandbox.Available() {
		return nil, fmt.Errorf("landlock is unavailable on this kernel; "+
			"the engine would run with read access to every file in this container. "+
			"Set %s=true to accept that deliberately", sandboxDisabledEnv)
	}
	// os.Executable and not a hardcoded path: this binary is the helper, and
	// resolving it by name would depend on a PATH the engine's own allowlist
	// environment controls.
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate this binary to use as the sandbox helper: %w", err)
	}
	return []string{self, sandboxExecMode}, nil
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
