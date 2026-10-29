package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/ratelimit"
)

// Routes builds the chi router for the API. /healthz stays outside the /v1
// group and requires no API key (D-09); every /v1 route requires a valid key
// via auth.Middleware — hard cutover (D-08), no unauthenticated path.
//
// The /v1 middleware order is deliberate: ratelimit.ByIP runs FIRST as a
// coarse pre-auth flood guard (before any DB lookup), then auth.Middleware
// resolves the client, then ratelimit.PerClient — keyed on the now-resolved
// client_id — enforces the fair per-client quota.
//
// Pre-auth IP identity comes from middleware.ClientIPFromRemoteAddr, which
// trusts only the raw TCP peer address of the connection. This service sits
// behind no trusted reverse proxy (PROJECT.md / 01-CONTEXT.md), so any
// client-supplied forwarding header (X-Forwarded-For, X-Real-IP,
// True-Client-IP) is intentionally ignored — trusting them here would let a
// single attacker manufacture unlimited fresh rate-limit buckets by varying
// the header per request, defeating ratelimit.ByIP entirely.
//
// /healthz stays registered here too (Phase 58, D-08) even though
// HealthRoutes below now also serves it on its own listener:
// deploy/chart/octoconv/templates/job-e2e.yaml:48 waits on
// api:8090/healthz before starting E2E, and scripts/cancel-liveproof.sh:179
// / scripts/quota-burst-driver.sh:219 both probe it at this same address.
// Removing the route from here would break three live consumers for zero
// isolation gain — the isolation HealthRoutes buys comes from a SEPARATE
// probe hitting a SEPARATE port, not from this path disappearing.
func (s *Server) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(s.armBodyWindow)
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromRemoteAddr)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.handleHealth)
	r.Route("/v1", func(r chi.Router) {
		r.Use(ratelimit.ByIP(s.ipRateRPM))
		r.Use(auth.Middleware(s.resolver))
		r.Use(ratelimit.PerClient(s.clientRateRPM))
		r.Post("/jobs", s.handleCreateJob)
		r.Post("/jobs/archive", s.handleArchive)
		// POST /v1/inspect (Plan 58-07, INSP-01..INSP-06): a dedicated
		// sub-group so this route carries an extra layer the rest of
		// /v1 does not (58-CONTEXT.md D-06). ratelimit.ByIP ->
		// auth.Middleware -> ratelimit.PerClient above are inherited from
		// the outer /v1 group and stay first -- they gate every route
		// identically. Inspect's own rate budget runs NEXT, AFTER auth,
		// because it is keyed by the now-resolved client identity
		// (mechanism 4: an independent, smaller rate budget than the
		// general per-client limit, since one inspect parse is
		// materially more expensive than one status poll).
		//
		// The D-06 mechanism-2 concurrency gate is deliberately NOT
		// mounted here as middleware (T-SLF-02, PR #9 finding at
		// inspectgate.go:120): wrapping the whole handler would acquire
		// the slot BEFORE handleInspect reads its own request body,
		// letting a client trickling a multipart upload arbitrarily
		// slowly hold a slot without ever doing any parsing.
		// handleInspect itself now calls s.inspectSem.acquireOrReject
		// directly, AFTER its own body intake completes and immediately
		// before the CPU-bound work the slot exists to bound -- see that
		// method's own doc comment (inspectgate.go) for the D-06a
		// property (bounding concurrent Repo.PreAdmit calls against the
		// shared pool) this move preserves unchanged.
		r.Group(func(r chi.Router) {
			r.Use(s.inspectRateLimit())
			r.Post("/inspect", s.handleInspect)
		})
		r.Get("/jobs", s.handleListJobs)
		r.Get("/jobs/{id}", s.handleGetJob)
		r.Get("/jobs/{id}/repeat", s.handleRepeatMetadata)
		r.Post("/jobs/{id}/repeat", s.handleRepeat)
		// GET /jobs/{id}/events (PUSH-01, Phase 42): SSE progress stream.
		// Registered here so it inherits the EXACT SAME ByIP -> auth ->
		// PerClient middleware chain as every other /v1 route (T-42-05) --
		// chi v5.3.0's middleware.Logger/Recoverer preserve http.Flusher
		// through their wrap, so streaming works unmodified inside this
		// stack (42-RESEARCH.md Pitfall 3).
		r.Get("/jobs/{id}/events", s.handleJobEvents)
		r.Post("/jobs/{id}/cancel", s.handleCancelJob)
		r.Get("/formats", s.handleListFormats)
		r.Route("/presets", func(r chi.Router) {
			r.Post("/", s.handleCreatePreset)
			r.Get("/", s.handleListPresets)
			r.Get("/{name}", s.handleShowPreset)
			r.Put("/{name}", s.handleUpdatePreset)
			r.Delete("/{name}", s.handleDeactivatePreset)
		})
		r.Route("/preset-settings", func(r chi.Router) {
			r.Use(s.requirePresetManagement)
			r.Post("/", s.handleCreatePreset)
			r.Put("/{name}", s.handleUpdatePreset)
			r.Delete("/{name}", s.handleDeactivatePreset)
		})

		// /v1/system/presets (OPER-01, D-02/D-03): a distinct literal path
		// segment under /v1, not nested under /v1/presets/{name} -- no chi
		// routing conflict. Gated by requireOperator, which runs AFTER
		// auth.Middleware has already resolved the caller; a non-operator
		// gets the uniform no-leak 404, never 403.
		r.Route("/system/presets", func(r chi.Router) {
			r.Use(s.requireOperator)
			r.Post("/", s.handleCreateSystemPreset)
			r.Get("/", s.handleListSystemPresets)
			r.Get("/{name}", s.handleShowSystemPreset)
			r.Put("/{name}", s.handleUpdateSystemPreset)
			r.Delete("/{name}", s.handleDeactivateSystemPreset)
		})
		// /v1/system/usage (OPSV-01/OPSV-02, Plan 53-04): a sibling of the
		// /system/presets block above, registered INSIDE a requireOperator
		// sub-router -- never as a bare top-level /v1 route. It reuses the
		// same middleware and therefore the same no-leak 404 as
		// /system/presets, and is a distinct literal path segment with no
		// chi routing conflict.
		r.Route("/system/usage", func(r chi.Router) {
			r.Use(s.requireOperator)
			r.Get("/", s.handleSystemUsage)
		})
	})
	return r
}

// HealthRoutes builds a SECOND, minimal chi router carrying exactly one
// route: GET /healthz. cmd/api/main.go binds this to its own http.Server on
// HEALTH_ADDR (Phase 58 Task 2), separate from the http.Server Routes()
// above binds to API_ADDR.
//
// What this buys, precisely: a burst against /v1/* (in particular
// POST /v1/inspect, INSP-05) saturates Routes()'s http.Server — its accept
// queue, its goroutines, its listener socket — without taking a single
// accept-path resource away from THIS router's http.Server, because they
// are two separate net.Listeners in two separate http.Server values. The
// liveness probe gets its own place in line, unconditionally.
//
// What this does NOT buy — stated explicitly because an earlier design
// discussion in this project assumed otherwise, and that assumption was
// wrong (58-CONTEXT.md D-06a): this router does not isolate handleHealth's
// DEPENDENCY pings. The Postgres ping inside handleHealth still runs
// against whatever *pgxpool.Pool sits in s.health.Postgres, and the Redis/
// S3 pings still run against whatever s.health.Redis/S3 point at. Splitting
// the HTTP listener does nothing about a shared, exhausted connection pool
// — that half of the fix is db.ConnectHealth's dedicated health pool
// (internal/db/db.go, Task 1), wired in by cmd/api/main.go, not this
// router. HTTP-accept isolation and dependency-pool isolation are two
// independent claims; this method only proves the first one.
//
// Middleware is deliberately minimal: the body read window and middleware.Recoverer. No
// middleware.Logger — a probe firing every 5-10s produces no useful signal
// and only adds log noise; no middleware.RequestID/ClientIPFromRemoteAddr —
// nothing on this router ever reads either value, unlike Routes()'s /v1
// subtree.
func (s *Server) HealthRoutes() chi.Router {
	r := chi.NewRouter()
	r.Use(s.armBodyWindow)
	r.Use(middleware.Recoverer)
	r.Get("/healthz", s.handleHealth)
	return r
}
