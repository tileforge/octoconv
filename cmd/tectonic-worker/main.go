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
	"os/exec"
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

func main() {
	// Before flags, before config, before anything: this mode never returns.
	if sandbox.RunExecModeIfRequested(os.Args) {
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
	sandboxHelper, err := sandbox.ResolveHelper(sandboxDisabledEnv)
	if err != nil {
		log.Fatalf("tectonic sandbox: %v", err)
	}
	// Only meaningful when the sandbox is on -- see ValidateTectonicCacheDir.
	// Checked AFTER the helper is resolved so that a deployment which
	// deliberately disabled the sandbox is not refused for a variable that
	// does not matter to it.
	if len(sandboxHelper) > 0 {
		if err := convert.ValidateTectonicCacheDir(os.Getenv(convert.TectonicCacheEnv)); err != nil {
			log.Fatalf("tectonic format cache: %v", err)
		}
	}

	// The helper runs EITHER WAY: it carries the CPU budget, which does not
	// depend on Landlock. Only the filesystem ruleset is optional.
	confineFS := len(sandboxHelper) > 0
	if !confineFS {
		self, serr := os.Executable()
		if serr != nil {
			log.Fatalf("locate this binary to use as the exec helper: %v", serr)
		}
		sandboxHelper = []string{self, sandbox.ExecMode}
		log.Printf("⚠️  TECTONIC_SANDBOX_DISABLED=true — the engine can read every file this container can (the CPU budget still applies)")
	} else {
		log.Printf("🔒 tectonic sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	// TECTONIC_ENGINE_CPU_SECONDS bounds what one document may COST, as
	// opposed to how long it may occupy the clock. The default is the same
	// number as the wall-clock deadline and that is not a coincidence: 271 is
	// ceil(25.76 s/pass x 7 passes x 1.5), and at an idle 2-CPU quota this
	// engine's cpu/wall ratio is 1.00, so the work bound and the time bound
	// coincide on an unloaded node and diverge exactly when the node is busy --
	// which is the case this exists for.
	// A VALUE THAT CANNOT BE PARSED IS A REFUSAL, not a fallback.
	//
	// envDuration returns the default for anything it cannot read, which is
	// right for most settings and wrong for this one: "60" is a plausible way
	// to write sixty seconds, and an operator who wrote it would have got the
	// 271-second default -- a bound four times weaker than the one they asked
	// for, with nothing said (codex, PR #22).
	if raw := os.Getenv("TECTONIC_ENGINE_CPU_SECONDS"); raw != "" {
		if _, err := time.ParseDuration(firstField(raw)); err != nil {
			log.Fatalf("TECTONIC_ENGINE_CPU_SECONDS=%q is not a duration: %v. Write it with a "+
				"unit, e.g. 271s or 5m -- a bare number is not accepted, because silently "+
				"falling back to the default would give you a weaker bound than you asked for",
				raw, err)
		}
	}
	cpuBudget := envDuration("TECTONIC_ENGINE_CPU_SECONDS", 271*time.Second)
	// REFUSED AT STARTUP, not truncated to zero. RLIMIT_CPU is expressed in
	// whole seconds, so 0s, -1s and 500ms are all syntactically valid
	// durations that become CPUSeconds <= 0 -- at which point SpecWith omits
	// the cpu: token, ApplyLimits does nothing, and the worker starts having
	// LOGGED a budget it does not enforce (codex, PR #22).
	//
	// That is the same "configured but not plumbed" defect this variable was
	// added to fix, so it fails loudly instead: an operator who asked for a
	// bound gets a sentence rather than a service that quietly has none.
	if cpuBudget < time.Second {
		// No escape-hatch advice here, deliberately: TECTONIC_SANDBOX_DISABLED
		// does NOT resolve this. That switch turns off the filesystem ruleset,
		// and the disabled path still runs the helper precisely to carry the
		// CPU budget -- so an operator following such advice would weaken
		// confinement and still not start (codex, PR #22). Naming a remedy
		// that does not work is worse than naming none.
		log.Fatalf("TECTONIC_ENGINE_CPU_SECONDS=%s is below one second. RLIMIT_CPU is expressed "+
			"in whole seconds, so this would silently disable the CPU budget rather than set a "+
			"small one. Use a value of at least 1s", cpuBudget)
	}
	cpuSeconds := int(cpuBudget.Seconds())

	tectonicConverter := buildTectonicConverter(bundlePath, maxOutputBytes, sandboxHelper, cpuSeconds, confineFS)
	log.Printf("⏱  TECTONIC_ENGINE_CPU_SECONDS=%d", cpuSeconds)

	// Proves the ENGINE runs under the rules, not merely that the rules can be
	// built. Done after the converter exists so the preflight uses exactly the
	// configuration the jobs will.
	if err := preflightEngine(ctx, tectonicConverter, sandboxHelper); err != nil {
		log.Fatalf("tectonic sandbox preflight: %v", err)
	}

	convert.Default.Replace(tectonicConverter)
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
		nil, // audioSandboxHelper — audio-only
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
func buildTectonicConverter(bundlePath string, maxOutputBytes int64, sandboxHelper []string, cpuSeconds int, confineFS bool) convert.TectonicConverter {
	return convert.TectonicConverter{
		BundlePath:        bundlePath,
		MaxOutputBytes:    maxOutputBytes,
		SandboxHelper:     sandboxHelper,
		CPUSeconds:        cpuSeconds,
		ConfineFilesystem: confineFS,
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

// preflightSandbox proves the whole mechanism works before any job depends on
// it: create the ruleset, add a rule, restrict, exec. ctx is the caller's
// signal context, so SIGINT/SIGTERM ends the check instead of waiting it out.
func preflightSandbox(ctx context.Context, helper []string) error {
	// Something that exists in the image, does nothing, and exits 0. Resolved
	// rather than hardcoded so the check does not itself become the reason a
	// worker refuses to start.
	probe, err := exec.LookPath("true")
	if err != nil {
		return fmt.Errorf("cannot preflight the sandbox: no 'true' binary to exec: %w", err)
	}

	args := append(append([]string{}, helper[1:]...), "ro:/usr", "--", probe)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, helper[0], args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("the sandbox cannot be established on this host, though the kernel "+
			"reports a Landlock ABI: %v: %s. A runtime policy (seccomp, LSM stacking, the "+
			"container runtime) is the usual cause. Set %s=true to run the engine unconfined "+
			"deliberately", err, strings.TrimSpace(string(out)), sandboxDisabledEnv)
	}
	return nil
}

// preflightEngine is the second half: the mechanism working says nothing about
// whether THIS engine can be executed under the rules this deployment builds.
// ctx is the caller's signal context, so SIGINT/SIGTERM ends the check.
func preflightEngine(ctx context.Context, c convert.TectonicConverter, helper []string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := c.TectonicPreflight(ctx, helper); err != nil {
		return fmt.Errorf("%v.\n\nThree causes are usual, and the engine's own output above says "+
			"which:\n"+
			"  1. the format cache holds no format this engine can USE -- a stale one from another "+
			"tectonic version passes the startup file check and then cannot be rebuilt, because "+
			"the sandbox grants the cache read-only. Re-warm it, or let the image's own cache "+
			"through instead of mounting over it.\n"+
			"  2. a runtime policy permits the Landlock ABI query but denies establishing a "+
			"ruleset (seccomp, LSM stacking, the container runtime).\n"+
			"  3. the engine's runtime files sit outside the granted directories -- a wrapper "+
			"whose interpreter, or a binary whose shared libraries, live elsewhere; extend "+
			"convert.tectonicSystemReadOnly for this image.\n\n"+
			"Set %s=true to run the engine unconfined deliberately", err, sandboxDisabledEnv)
	}
	return nil
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
