// Command api runs the OctoConv HTTP API: accepts conversion jobs and reports
// their status.
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

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/apaderin/octoconv/internal/api"
	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/cancel"
	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/db"
	"github.com/apaderin/octoconv/internal/envcfg"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/metrics"
	"github.com/apaderin/octoconv/internal/presets"
	"github.com/apaderin/octoconv/internal/queue"
	"github.com/apaderin/octoconv/internal/storage"
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

	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

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
	prometheus.MustRegister(metrics.NewQueueDepthCollector(asynq.NewInspector(redisOpt),
		append(queue.AllConvertQueues(), queue.QueueWebhook)...))

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
		Postgres: pool,
		Redis:    redisPinger{client: rdb},
		S3:       store,
	}
	jobsRepo := jobs.NewRepo(pool)
	srv := api.NewServer(jobsRepo, store, qc, presetRepo, presetRepo, resolver, health, api.Config{
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
		MaxUploadBytes:               envInt64("MAX_UPLOAD_BYTES", 2<<30),
		MaxImagePixels:               uint64(envInt64("MAX_IMAGE_PIXELS", 100_000_000)),            // D-05: 100 megapixels default
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

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
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
	log.Println("🛑 shutting down API...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("metrics graceful shutdown failed: %v", err)
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

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(firstField(v)); err == nil {
			return d
		}
	}
	return def
}
