// Command api runs the OctoConv HTTP API: accepts conversion jobs and reports
// their status.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/tileforge/octoconv/internal/api"
	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/cancel"
	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/envcfg"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/metrics"
	"github.com/tileforge/octoconv/internal/presets"
	"github.com/tileforge/octoconv/internal/queue"
	"github.com/tileforge/octoconv/internal/storage"
	"github.com/tileforge/octoconv/internal/webhook"
	"github.com/tileforge/octoconv/internal/workerproto"
	"github.com/tileforge/octoconv/internal/workflow"
)

// redisPinger adapts a *redis.Client to api.Pinger. asynq's own Client
// exposes no public Ping method (RESEARCH.md Open Question 2), so the API
// process opens a small, dedicated Redis connection purely for /healthz.
type redisPinger struct {
	client *redis.Client
}

func (p redisPinger) Ping(ctx context.Context) error {
	return p.client.Ping(ctx).Err()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	spillDir := os.TempDir()
	if removed, err := sweepMultipartSpill(spillDir); err != nil {
		log.Printf("⚠️  multipart spill sweep of %s removed %d files: %v", spillDir, removed, err)
	} else {
		log.Printf("🧹 removed %d orphaned multipart spill files from %s", removed, spillDir)
	}

	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// D-06a (Phase 58): a dedicated, deliberately tiny pool used for
	// exactly one thing -- the Postgres ping inside GET /healthz -- so a
	// burst of concurrent POST /v1/inspect requests exhausting pool above
	// (through Repo.PreAdmit, INSP-04) cannot delay or fail that ping.
	// Mirrors the redisPinger precedent below (a dedicated connection
	// purely for /healthz), applied to Postgres instead of Redis. See
	// db.ConnectHealth's own doc comment for why the second /healthz
	// listener (internal/api/routes.go HealthRoutes) does NOT make this
	// pool redundant.
	healthPool, err := db.ConnectHealth(ctx)
	if err != nil {
		log.Fatalf("postgres (health pool): %v", err)
	}
	defer healthPool.Close()

	store, err := storage.New(ctx)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	// The API is the single owner of the bucket lifecycle rule (Pitfall 4);
	// SetBucketLifecycle is an idempotent full-document PUT, so calling it
	// on every startup is safe (D-12).
	if err := store.EnsureLifecycle(ctx, envDuration("STORAGE_TTL", 168*time.Hour)); err != nil {
		log.Fatalf("storage lifecycle: %v", err)
	}

	qc, err := queue.NewClient()
	if err != nil {
		log.Fatalf("queue: %v", err)
	}
	defer qc.Close()

	// Dedicated Redis connection for /healthz only (D-16) — asynq's Client
	// exposes no public Ping, so the API opens its own lightweight ping
	// client against the same REDIS_ADDR the queue already uses.
	redisOpt, err := queue.RedisOpt()
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisOpt.Addr})
	defer rdb.Close()

	// KEDA-01/D-01/D-02/D-06 (Phase 35): the queue-depth collector now lives
	// solely on the always-on api process, registered for EVERY engine-class
	// queue — a worker Deployment scaled to genuine 0 replicas by KEDA would
	// otherwise have no pod exposing the metric KEDA needs to scale it back
	// up. The queue list is DERIVED from queue.AllConvertQueues() rather than
	// hand-listed: this call is variadic, so a hand-maintained list can
	// silently drop an engine class with no compile error (RESEARCH.md
	// Pitfall 2) -- queue.TestAllConvertQueuesCoversEveryEngine guards the
	// derivation itself. webhook is not an engine class and is therefore not
	// covered by AllConvertQueues(), so it is appended explicitly here; it is
	// included even though webhook-worker is never KEDA-scaled (D-01 in
	// PROJECT.md), so its depth stays observable. Matching every worker's
	// existing precedent, the Inspector is never closed — Collect() is
	// pull-based/lazy, invoked once per scrape.
	depth := metrics.NewQueueDepthCollector(asynq.NewInspector(redisOpt),
		append(queue.AllConvertQueues(), queue.QueueWebhook)...)

	salt := []byte(os.Getenv("API_KEY_SALT"))
	if len(salt) == 0 {
		log.Fatalf("API_KEY_SALT must be set")
	}
	clientRepo := clients.NewRepo(pool)
	resolver := auth.NewResolver(clientRepo, salt)
	presetRepo := presets.NewRepo(pool)

	// D-01/OPER-01: OPERATOR_CLIENT_IDS is parsed once, here, at startup.
	// A malformed entry aborts startup loudly (fail-loud, T-26-03) -- it
	// must never silently shrink the intended operator set. Empty/unset is
	// not an error: it yields zero operators (fail-closed).
	operatorIDs, err := api.ParseOperatorClientIDs(os.Getenv("OPERATOR_CLIENT_IDS"))
	if err != nil {
		log.Fatalf("OPERATOR_CLIENT_IDS: %v", err)
	}

	// D-3: QUOTA_ENFORCEMENT_DISABLED, WEBHOOK_ALLOW_PRIVATE_IPS and
	// WEBHOOK_ALLOW_INSECURE_HTTP are read once, here, at process start, and
	// validated together. cmd/api is the ONLY process that reads either
	// WEBHOOK_ALLOW_* var (.env.example:38-40; cmd/webhook-worker does not),
	// so this is their one chance to be surfaced — internal/api/callbackurl.go
	// re-reads both per request/per resolved IP and silently falls back to
	// the fail-closed default on an unrecognized value there instead of
	// logging per request. QUOTA_ENFORCEMENT_DISABLED's own "this is
	// relaxed" warning and its jobsRepo wiring stay below (QUOT-04/D-13),
	// once jobsRepo exists; the value is captured here so it is validated
	// alongside the other two.
	quotaDisabled, quotaRecognized := envcfg.Bool("QUOTA_ENFORCEMENT_DISABLED", false)
	allowPrivateIPs, allowPrivateIPsRecognized := envcfg.Bool("WEBHOOK_ALLOW_PRIVATE_IPS", false)
	allowInsecureHTTP, allowInsecureHTTPRecognized := envcfg.Bool("WEBHOOK_ALLOW_INSECURE_HTTP", false)
	for _, flag := range []struct {
		key        string
		recognized bool
		value      bool
	}{
		{"QUOTA_ENFORCEMENT_DISABLED", quotaRecognized, quotaDisabled},
		{"WEBHOOK_ALLOW_PRIVATE_IPS", allowPrivateIPsRecognized, allowPrivateIPs},
		{"WEBHOOK_ALLOW_INSECURE_HTTP", allowInsecureHTTPRecognized, allowInsecureHTTP},
	} {
		if !flag.recognized {
			log.Printf("⚠️  %s=%q is not a recognized boolean (expected \"true\" or \"false\"); using %v", flag.key, os.Getenv(flag.key), flag.value)
		}
	}

	// D-04: surface a startup-visible warning when the webhook SSRF guard's
	// RFC1918 private-IP block is relaxed, so a relaxed safety posture is
	// obvious from logs without reading .env or source (loopback/link-local/
	// unspecified remain hard-blocked regardless — see internal/api/callbackurl.go).
	if allowPrivateIPs {
		log.Printf("⚠️  WEBHOOK_ALLOW_PRIVATE_IPS=true: webhook SSRF guard allows RFC1918 private-IP callback_url targets")
	}
	// D-4: mirrors the WEBHOOK_ALLOW_PRIVATE_IPS warning above —
	// WEBHOOK_ALLOW_INSECURE_HTTP was simply never given the same treatment.
	// cmd/api does not otherwise read this var; internal/api/callbackurl.go
	// is the only consumer, so this is the only place its relaxed posture
	// can be surfaced.
	if allowInsecureHTTP {
		log.Printf("⚠️  WEBHOOK_ALLOW_INSECURE_HTTP=true: webhook SSRF guard allows plain-http callback_url targets")
	}

	health := api.HealthDeps{
		// D-06a: healthPool, NEVER pool. pool is the same pool
		// Repo.PreAdmit reads through on every synchronous inspect request
		// (INSP-04); pointing this field back at pool re-opens exactly the
		// pool-contention path healthPool exists to close, and the second
		// /healthz listener (internal/api/routes.go HealthRoutes) does not
		// catch that regression -- HTTP-accept isolation and Postgres-pool
		// isolation are independent claims. Proven mechanically by
		// cmd/api/wiring_test.go, not just asserted in this comment.
		Postgres: healthPool,
		Redis:    redisPinger{client: rdb},
		S3:       store,
	}
	// jobsRepo is built from pool (the MAIN pool), never healthPool -- the
	// jobs repository, and therefore Repo.PreAdmit, must stay on the pool
	// sized for ordinary + inspect traffic (D-06a); healthPool is sized
	// (MaxConns=2) for exactly one caller, the health ping, and would starve
	// immediately if anything else were routed through it.
	jobsRepo := jobs.NewRepo(pool)
	// jobsRepo appears twice deliberately -- once as the jobs Repo and once
	// as the UsageRepo -- because the two interfaces are narrow views of the
	// same concrete *jobs.Repo, exactly as presetRepo already appears twice
	// for PresetRepo/PresetAdmin. clientRepo was already constructed above
	// for the auth resolver and now doubles as the LimitsRepo (OPSV-01/02).
	srv := api.NewServer(jobsRepo, store, qc, presetRepo, presetRepo, jobsRepo, clientRepo, resolver, health, api.Config{
		// D-07 (Phase 35, operator-confirmed 2026-07-21): raised from 100
		// MiB to 2 GiB so video uploads are admissible -- this is a pre-
		// parse http.MaxBytesReader bound, enforced BEFORE the engine class
		// is known. MaxEngineBytes is left unset (nil) here deliberately:
		// NewServer's own D-07 defaulting remains the single source of
		// truth for the per-engine ceilings that restore image/document/
		// html/audio to their prior 100 MiB effective bound, so that map is
		// not duplicated at this call site. MaxPandocInputBytes below is the
		// one exception: it is a single scalar (not a map), read here and
		// folded into NewServer's own default map -- it does not duplicate
		// the map, it configures one entry in it.
		MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 2<<30),
		MaxImagePixels: uint64(envInt64("MAX_IMAGE_PIXELS", 100_000_000)), // D-05: 100 megapixels default
		// PROBE_MAX_PAGES is deliberately NOT read here. The sanitize page
		// ceiling is enforced only in cmd/probe-worker (PROBE-02); the
		// admission gate that once read it was removed after six review
		// rounds -- see internal/api/handlers.go's own note where it stood,
		// and 63-CONTEXT.md D-04.
		MaxDocumentUncompressedBytes: uint64(envInt64("MAX_DOCUMENT_UNCOMPRESSED_BYTES", 500<<20)), // D-04: 500 MiB default
		// PR#2 finding-1: the API-side mirror of the worker's own
		// PANDOC_MAX_INPUT_BYTES (enforcePandocInputSize, markupsniff.go).
		// Default 0 -- not 100<<20, not the deployed 3 MiB value -- is
		// deliberate: an absent env var must land on NewServer's own
		// default rather than silently introduce a second hard-coded number
		// here. When set, it must be byte-identical to pandoc-worker's own
		// PANDOC_MAX_INPUT_BYTES so the API never accepts what the worker
		// would certainly reject.
		MaxPandocInputBytes:     envInt64("PANDOC_MAX_INPUT_BYTES", 0),
		IPRateLimitRPM:          int(envInt64("RATE_LIMIT_IP_RPM", 60)),
		ClientRateLimitRPM:      int(envInt64("RATE_LIMIT_CLIENT_RPM", 120)),
		OperatorClientIDs:       operatorIDs,
		JobsListDefaultPageSize: int(envInt64("JOBS_LIST_DEFAULT_PAGE_SIZE", 20)),
		JobsListMaxPageSize:     int(envInt64("JOBS_LIST_MAX_PAGE_SIZE", 100)),
		// PUSH-01 (Phase 42): GET /v1/jobs/{id}/events SSE tunables (D-01/
		// D-02). Defaults (1s/15s/30m) applied by api.NewServer itself when
		// these are zero -- envDuration here only overrides them.
		SSEPollInterval:      envDuration("SSE_POLL_INTERVAL", 1*time.Second),
		SSEHeartbeatInterval: envDuration("SSE_HEARTBEAT_INTERVAL", 15*time.Second),
		SSEMaxStreamDuration: envDuration("SSE_MAX_STREAM_DURATION", 30*time.Minute),
		// Plan 53-07: the SAME quotaDisabled value handed to
		// jobsRepo.SetQuotaEnforcementDisabled below -- the two must never
		// diverge, or GET /v1/system/usage would report an enforcement
		// posture the admission gate does not actually have.
		QuotaEnforcementDisabled: quotaDisabled,
		// Five inspect boundaries (58-CONTEXT.md D-06, Plan 58-06 Task 3):
		// api.NewServer applies its own documented defaults (100 MiB / 8
		// MiB / 2s / 4 / 30) when any of these are left at zero -- an
		// absent env var is never a behaviour change, same defaulting
		// discipline as every other Config field above. See api.Config's
		// own doc comments for what each one bounds and why the two byte
		// ceilings (upload vs. work) are separate fields.
		InspectMaxUploadBytes:  envInt64("INSPECT_MAX_UPLOAD_BYTES", 0),
		InspectMaxWorkBytes:    envInt64("INSPECT_MAX_WORK_BYTES", 0),
		InspectMaxWorkDuration: envDuration("INSPECT_MAX_WORK_DURATION", 0),
		InspectMaxConcurrent:   int(envInt64("INSPECT_MAX_CONCURRENT", 0)),
		InspectRateLimitRPM:    int(envInt64("RATE_LIMIT_INSPECT_RPM", 0)),
		// INSP-04 (Plan 58-07): the SAME per-engine timeout env vars every
		// worker process already reads at its own startup, now ALSO read here
		// so POST /v1/inspect can report each engine's upper bound. Built by
		// a named function rather than inline so a test can walk it -- see
		// engineTimeoutsFromEnv.
		EngineTimeouts: engineTimeoutsFromEnv(),
	})

	// D-03/D-04 (Plan 03): activate the speed layer -- srv's CancelPublisher
	// seam (Plan 01) was nil-safe until now. Reuses the SAME rdb already
	// constructed above for the /healthz pinger (same REDIS_ADDR the queue
	// uses), so no second Redis connection is opened; the existing
	// `defer rdb.Close()` above is still correct since publishing only
	// happens while the API is serving requests, before shutdown begins.
	srv.SetCancelPublisher(cancel.NewPublisher(rdb))

	// QUOT-04/D-13: global quota kill-switch. quotaDisabled/quotaRecognized
	// were already read and validated above (D-3), alongside the two
	// WEBHOOK_ALLOW_* flags -- cmd/api is the single startup site that can
	// surface an unrecognized value without flooding a per-request path.
	// Deliberately NOT envInt64-style empty-means-default parsing -- the trap
	// that avoids (recorded in the PR #4 review): if an empty env value fell
	// back to a code default of "disabled", empty would silently mean
	// "enforcement is off", which is exactly backwards. Empty/absent still
	// leaves enforcement governed per-client by clients.enforcement_mode;
	// only "true" -- optionally followed by an inline "#" comment -- forces
	// observe-only service-wide; any other non-empty value is rejected as
	// unrecognized (warned about above) and leaves enforcement ON.
	if quotaDisabled {
		log.Printf("⚠️  QUOTA_ENFORCEMENT_DISABLED=true: quota enforcement is disabled service-wide; verdicts are still computed and counted, no request is rejected")
	}
	jobsRepo.SetQuotaEnforcementDisabled(quotaDisabled)
	// This closure is the ONLY bridge between internal/jobs and
	// internal/metrics -- it exists so the repository package keeps no
	// metrics dependency of its own (internal/jobs/admission.go).
	jobsRepo.SetQuotaObserver(func(clientID uuid.UUID, engine, reason string) {
		metrics.RecordQuotaWouldReject(clientID.String(), engine, reason)
	})

	workerCatalog := workerproto.NewRepo(pool)
	srv.SetCapabilityCatalog(workerCatalog)
	srv.SetWorkerAdmin(workerCatalog)
	wfStore := workflow.NewStore(pool)
	wfStore.SetJobs(jobsRepo)
	srv.SetWorkflows(wfStore)
	depth.SetImageBacklog(func() (metrics.ImageBacklog, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		pending, active, retry, err := workerCatalog.ImageBacklog(ctx)
		if err != nil {
			return metrics.ImageBacklog{}, err
		}
		return metrics.ImageBacklog{Pending: pending, Active: active, Retry: retry}, nil
	})
	prometheus.MustRegister(depth)

	reconcileDone := make(chan struct{})
	go func() {
		defer close(reconcileDone)
		log.Printf("protocol reconciler starting")
		err := workerproto.StartProtocolReconciler(ctx, pool, jobsRepo, store, engineTimeoutsFromEnv()[convert.EngineImage], 0x70726F74)
		if err != nil && ctx.Err() == nil {
			log.Printf("protocol reconciler: %v", err)
		}
	}()

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	httpSrv := api.NewHTTPServer(addr, srv.Routes())

	enabled, listenerEnv, err := workerproto.ParseListenerEnv(os.Getenv)
	if err != nil {
		log.Fatalf("worker listener: %v", err)
	}
	var workerSrv *http.Server
	if enabled {
		clientCAs, err := readWorkerClientCAs(listenerEnv.ClientCA)
		if err != nil {
			log.Fatalf("worker client ca: %v", err)
		}
		if err := workerLeafAllowsOrigin(listenerEnv.Cert, listenerEnv.Origin); err != nil {
			log.Fatalf("worker tls cert: %v", err)
		}
		if _, err := tls.LoadX509KeyPair(listenerEnv.Cert, listenerEnv.Key); err != nil {
			log.Fatalf("worker tls key: %v", err)
		}
		compared, err := workerCatalog.CompareInstalled(ctx, listenerEnv.ManifestDir)
		if err != nil {
			log.Fatalf("worker manifests: %v", err)
		}
		for _, id := range compared.Mismatched {
			log.Printf("worker manifest hash mismatch: %s", id)
		}
		tlsCfg, err := workerproto.ServerTLSConfig(workerCatalog)
		if err != nil {
			log.Fatalf("worker tls: %v", err)
		}
		tlsCfg.ClientCAs = clientCAs
		workerHandler := workerproto.NewHandler(workerCatalog, jobsRepo, store, engineTimeoutsFromEnv()[convert.EngineImage])
		workerHandler.SetEngineBudgets(engineTimeoutsFromEnv())
		if secret := os.Getenv("WEBHOOK_SIGNING_SECRET"); secret != "" {
			progressWebhookTimeout := envDuration("PROGRESS_WEBHOOK_TIMEOUT", 1500*time.Millisecond)
			workerHandler.SetProgressWebhook(webhook.NewDeliverer(), []byte(secret), progressWebhookTimeout)
			log.Printf("🔔 progress webhooks enabled (PROGRESS_WEBHOOK_TIMEOUT=%v)", progressWebhookTimeout)
		} else {
			log.Printf("🔕 progress webhooks disabled (WEBHOOK_SIGNING_SECRET unset)")
		}
		workerSrv = api.NewHTTPServer(listenerEnv.Addr, workerHandler.Routes())
		workerSrv.TLSConfig = tlsCfg
	}

	go func() {
		log.Printf("🚀 API listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	// Prometheus /metrics is served on its own localhost-only listener,
	// separate from the public API_ADDR (D-19/T-04-13) — operational job/
	// queue data must not be reachable by arbitrary internal API callers.
	metricsAddr := os.Getenv("METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = "127.0.0.1:9090"
	}
	metricsSrv := api.NewHTTPServer(metricsAddr, promhttp.Handler())

	go func() {
		log.Printf("📊 metrics listening on %s", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("metrics listen: %v", err)
		}
	}()

	// GET /healthz gets its OWN listener on HEALTH_ADDR (Phase 58 Task 2,
	// D-06/D-08): a burst against API_ADDR (in particular POST /v1/inspect,
	// INSP-05) saturates httpSrv's accept queue and goroutines without
	// taking a single accept-path resource away from THIS http.Server --
	// they are two separate net.Listeners. This closes the HTTP-accept half
	// of the liveness risk; the DEPENDENCY half (the Postgres ping inside
	// handleHealth) is closed separately by healthPool above (D-06a) --
	// see internal/api/routes.go's HealthRoutes doc comment for why the two
	// are independent fixes, not one. srv.Routes() (API_ADDR) keeps serving
	// /healthz too (D-08) for the three existing consumers that already
	// probe it there.
	healthAddr := os.Getenv("HEALTH_ADDR")
	if healthAddr == "" {
		healthAddr = ":8091"
	}
	healthSrv := api.NewHTTPServer(healthAddr, srv.HealthRoutes())

	go func() {
		log.Printf("🩺 health listening on %s", healthAddr)
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("health listen: %v", err)
		}
	}()

	if workerSrv != nil {
		go func() {
			log.Printf("🔐 worker listening on %s", workerSrv.Addr)
			err := workerSrv.ListenAndServeTLS(listenerEnv.Cert, listenerEnv.Key)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("worker listen: %v", err)
			}
		}()
	}

	<-ctx.Done()
	log.Println("🛑 shutting down API...")

	// Shutdown order is deliberate, not incidental (Phase 58 Task 2): the
	// worker listener, when it was started, and httpSrv first, then
	// metricsSrv, and healthSrv LAST. During a rolling deploy's drain
	// window, the liveness probe must keep answering as long as possible --
	// shutting healthSrv down first would make kubelet see a dead probe
	// while httpSrv is still gracefully draining in-flight /v1 requests,
	// which is exactly the false-restart risk this ordering avoids. Every
	// Shutdown call shares shutdownCtx.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	select {
	case <-reconcileDone:
	case <-shutdownCtx.Done():
	}
	if workerSrv != nil {
		if err := workerSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("worker graceful shutdown failed: %v", err)
		}
	}
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("metrics graceful shutdown failed: %v", err)
	}
	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("health graceful shutdown failed: %v", err)
	}
	log.Println("bye 👋")
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		// Tolerate trailing inline comments / whitespace from .env files.
		if n, err := strconv.ParseInt(firstField(v), 10, 64); err == nil {
			return n
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

// engineTimeoutsFromEnv reads every engine class's own timeout env var -- the
// SAME variable, with the SAME default, the matching worker process reads at
// its own startup (IN-02). The result is what POST /v1/inspect publishes as
// estimated_max_seconds.
//
// It is a function, not an inline map literal, because the inline form could
// only be checked by reading it. api.NewServer merges its own defaults into
// whatever this returns, so a missing engine no longer publishes a zero upper
// bound (the PR #12 defect, fixed structurally in 63-02) -- but a missing
// engine still publishes the DEFAULT while its worker uses the operator's
// override, which is the same divergence one layer quieter. Walkable by
// TestEngineTimeoutsFromEnvCoversEveryEngine, which fails by name on the tenth
// engine class.
func engineTimeoutsFromEnv() map[string]time.Duration {
	return map[string]time.Duration{
		convert.EngineImage:    envDuration("ENGINE_TIMEOUT", 120*time.Second),
		convert.EngineDocument: envDuration("DOCUMENT_ENGINE_TIMEOUT", 300*time.Second),
		convert.EngineHTML:     envDuration("HTML_ENGINE_TIMEOUT", 60*time.Second),
		convert.EngineAudio:    envDuration("AUDIO_ENGINE_TIMEOUT", 742*time.Second),
		convert.EngineAV:       envDuration("AV_ENGINE_TIMEOUT", 753*time.Second),
		convert.EngineOCR:      envDuration("OCR_ENGINE_TIMEOUT", 744*time.Second),
		convert.EnginePandoc:   envDuration("PANDOC_ENGINE_TIMEOUT", 65*time.Second),
		// PROBE_ENGINE_TIMEOUT is MEASURED as of Phase 60 (see .env.example
		// for the run and the ceil(2.0 s/page x 200 pages x 1.5) derivation;
		// this comment called it a placeholder until Phase 63). It must still
		// be passed HERE: omitting it published sanitize offers with
		// estimated_max_seconds: 0 while the worker used its own value.
		// Caught in review (PR #12).
		convert.EngineProbe: envDuration("PROBE_ENGINE_TIMEOUT", 600*time.Second),
		// TECTONIC_ENGINE_TIMEOUT is the protocol attempt budget. 271s is
		// MEASURED (2026-09-04): ceil(25.76 s/pass x 7 passes x 1.5).
		convert.EngineTectonic: envDuration("TECTONIC_ENGINE_TIMEOUT", 271*time.Second),
		convert.EngineCAD:      envDuration("CAD_ENGINE_TIMEOUT", convert.CADDefaultEngineTimeout),
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

func readWorkerClientCAs(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("empty client ca")
	}
	return pool, nil
}

// workerLeafAllowsOrigin checks the leaf DNS SAN. Common Name is not a SAN.
func workerLeafAllowsOrigin(certPath, origin string) error {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", certPath, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return errors.New("server certificate is not pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse server certificate: %w", err)
	}
	if err := cert.VerifyHostname(origin); err != nil {
		return fmt.Errorf("origin %q: %w", origin, err)
	}
	return nil
}
