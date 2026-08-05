// Package api implements the HTTP layer: accepting conversion jobs and
// reporting their status.
package api

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/presets"
)

// Repo is the subset of the jobs repository the API depends on.
type Repo interface {
	Create(ctx context.Context, p jobs.CreateParams) (uuid.UUID, error)
	// PreAdmit is the pre-upload quota gate (QUOT-02, ROADMAP SC2b): a
	// cheap, non-locking check the handler calls before s.storage.Upload so
	// a client already at its cap does not burn S3 bandwidth/storage on
	// every retry. It is advisory only — Create's admitLocked, running
	// inside the same transaction as the insert, remains the authoritative
	// check; the interface deliberately exposes both because the handler
	// needs to reject before the S3 write AND still correctly map a
	// post-upload rejection when PreAdmit passed but Create's authoritative
	// re-check did not (52-04-PLAN.md Task 1).
	PreAdmit(ctx context.Context, clientID uuid.UUID, engine string, inputBytes int64) error
	Get(ctx context.Context, id uuid.UUID) (*jobs.Job, error)
	Outputs(ctx context.Context, id uuid.UUID) ([]jobs.Output, error)
	Cancel(ctx context.Context, id uuid.UUID) error
	// List is the client-scoped, keyset-paginated listing read (JLIST-01/02,
	// Plan 01). clientID is ALWAYS the caller's own auth.ClientFromContext
	// id — handleListJobs never passes a client-supplied id (D-08).
	List(ctx context.Context, clientID uuid.UUID, f jobs.ListFilter, cursor *jobs.Cursor, limit int) ([]jobs.JobSummary, *jobs.Cursor, error)
}

// CancelPublisher is the nil-safe seam handleCancelJob best-effort publishes
// the cancel signal through (D-09): the speed layer that wakes a worker
// actively running the job. Plan 03 injects the real Redis pub/sub
// publisher via Server.SetCancelPublisher; until then it stays nil and
// handleCancelJob simply skips the publish. Never load-bearing for
// correctness — the guarded transition (Repo.Cancel) already committed the
// canceled status before this is ever called.
type CancelPublisher interface {
	Publish(ctx context.Context, jobID uuid.UUID) error
}

// PresetRepo is the narrow, interface-segregated subset of the presets
// repository handleCreateJob depends on (D-09): resolution only. The
// pre-Create active re-check (Pitfall 8) re-uses this SAME Resolve method
// rather than adding a second method to the interface.
type PresetRepo interface {
	Resolve(ctx context.Context, clientID uuid.UUID, name string) (*presets.Preset, error)
}

// PresetAdmin is the SECOND, separate interface (D-08) the /v1/presets REST
// handlers depend on: client-scope CRUD plus the merged effective-view reads
// (D-09/D-10). It exists alongside PresetRepo (interface segregation
// preserved, ARCHITECTURE Anti-Pattern 3) — do not widen PresetRepo instead.
// Both are backed by the same *presets.Repo.
type PresetAdmin interface {
	Create(ctx context.Context, p presets.CreateParams) (uuid.UUID, int, error)
	Update(ctx context.Context, scope string, clientID *uuid.UUID, name, targetFormat string, options map[string]any, description string) (int, error)
	Deactivate(ctx context.Context, scope string, clientID *uuid.UUID, name string) error
	Get(ctx context.Context, scope string, clientID *uuid.UUID, name string) (*presets.Preset, error)
	List(ctx context.Context, scope string, clientID *uuid.UUID, includeInactive bool) ([]presets.Preset, error)
	ListForClient(ctx context.Context, clientID *uuid.UUID, includeInactive bool) ([]presets.Preset, error)
	GetForClient(ctx context.Context, clientID *uuid.UUID, name string) (*presets.Preset, error)
}

// UsageRepo is the narrow, interface-segregated subset backing
// `GET /v1/system/usage` (OPSV-01/OPSV-02, D-08): the per-(client, engine)
// usage aggregate, backed by *jobs.Repo. It is NOT folded into the existing
// Repo interface: Repo is deliberately jobs-scoped, and adding a usage-read
// method there would force fakeRepo and every existing job-handler test to
// grow a method they never exercise — the same argument PresetAdmin's own
// doc comment makes above. UsageRepo performs no authorization of its own:
// the caller is already a confirmed operator by the time a handler using it
// runs, because requireOperator (system_presets_handlers.go) gates the
// route; this interface must never be reached from a client-scoped route.
// clientID == nil means "all clients" (D-01) — one optional bind in the
// underlying SQL, not a second code path.
//
// UTCToday (plan 53-07, WR-03) MUST be the source of the day range this
// endpoint reports: it reads the same Postgres clock evaluate()
// (internal/jobs/admission.go) reads via `(now() AT TIME ZONE 'utc')::date`,
// never the API host's wall clock. Resolving "today" from time.Now() in this
// process instead would let a UTC-midnight crossing or NTP skew make this
// endpoint report one day while the admission gate enforces another.
type UsageRepo interface {
	UsageSummary(ctx context.Context, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]jobs.UsageRow, error)
	UTCToday(ctx context.Context) (time.Time, error)
}

// LimitsRepo is the narrow, interface-segregated subset backing
// `GET /v1/system/usage` (OPSV-01/OPSV-02, D-08): the batch per-client limits
// read, backed by *clients.Repo. It is NOT folded into the existing Repo
// interface for the same reason UsageRepo is not: Repo is jobs-scoped only,
// and this method has no job-handler caller. LimitsRepo performs no
// authorization of its own: the caller is already a confirmed operator by
// the time a handler using it runs, because requireOperator
// (system_presets_handlers.go) gates the route; this interface must never be
// reached from a client-scoped route. clientID == nil means "all clients"
// (D-01) — one optional bind in the underlying SQL, not a second code path.
type LimitsRepo interface {
	ListAllLimits(ctx context.Context, clientID *uuid.UUID) ([]clients.LimitWithMeta, error)
}

// Storage is the subset of the storage client the API depends on.
type Storage interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Enqueuer dispatches conversion work to the appropriate engine-class queue.
type Enqueuer interface {
	EnqueueImageConvert(ctx context.Context, jobID uuid.UUID) error
	EnqueueDocumentConvert(ctx context.Context, jobID uuid.UUID) error
	EnqueueHTMLConvert(ctx context.Context, jobID uuid.UUID) error
	EnqueueAudioConvert(ctx context.Context, jobID uuid.UUID) error
	EnqueueAVConvert(ctx context.Context, jobID uuid.UUID) error
	EnqueueOCRConvert(ctx context.Context, jobID uuid.UUID) error
	EnqueuePandocConvert(ctx context.Context, jobID uuid.UUID) error
}

// Pinger is a narrow, read-only reachability probe for a single dependency,
// bounded by ctx's deadline (D-16). It must never write/mutate dependency
// state.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthDeps holds the three dependency pingers /healthz probes (OBS-02,
// D-16/D-17).
type HealthDeps struct {
	Postgres Pinger
	Redis    Pinger
	S3       Pinger
}

// Server holds the API dependencies and configuration.
type Server struct {
	repo        Repo
	storage     Storage
	queue       Enqueuer
	presets     PresetRepo
	presetAdmin PresetAdmin
	// usageRepo / limitsRepo back GET /v1/system/usage (OPSV-01/OPSV-02,
	// Plan 04). Never nil after NewServer because they are required
	// positional params — unlike cancelPublisher, which is a deliberately
	// nil-safe post-construction seam.
	usageRepo     UsageRepo
	limitsRepo    LimitsRepo
	resolver      auth.ClientResolver
	health        HealthDeps
	maxUploadByte int64
	// maxEngineBytes is the D-07 (Phase 35) per-engine post-detection upload
	// ceiling, checked in handleCreateJob immediately after
	// convert.Default.EngineFor -- unlike maxUploadByte (a pre-parse bound
	// enforced by http.MaxBytesReader BEFORE the engine class is even
	// knowable), this gate runs once the detected format and engine are
	// known, and it MUST precede s.storage.Upload. Never nil after
	// NewServer: holding image/document/html/audio at their pre-Phase-35
	// effective 100 MiB ceiling here is what makes raising the GLOBAL
	// maxUploadByte to 2 GiB (to admit large video uploads) a zero-
	// behavior-change event for the four existing engine classes -- only the
	// av engine class gains the larger allowance. A key absent from this map
	// is NOT rejected: the map gates known engines, it does not allowlist.
	maxEngineBytes map[string]int64
	// maxImagePixels is intentionally uint64 — WIDER than maxUploadByte's
	// int64 — because it is compared against a product of two uint32
	// declared dimensions (Pitfall 1); an adversarial max-uint32 x max-uint32
	// product must not wrap or misbehave under signed/narrower arithmetic.
	maxImagePixels uint64
	// maxDocumentUncompressedBytes is the zip-bomb guard ceiling (D-04):
	// the max total declared uncompressed size summed across every ZIP
	// central-directory entry in an office document, compared against
	// ContainerResult.TotalUncompressed (also uint64).
	maxDocumentUncompressedBytes uint64
	presignTTL                   time.Duration
	ipRateRPM                    int
	clientRateRPM                int
	// operators is the OPERATOR_CLIENT_IDS membership set (D-01, Phase 26):
	// callers whose resolved client.ID is a key here may reach the
	// /v1/system/presets subtree via requireOperator. Never nil after
	// NewServer (an unset/empty env var yields an empty, non-nil set --
	// fail-closed, T-26-03).
	operators map[uuid.UUID]struct{}
	// cancelPublisher is the nil-safe CancelPublisher seam (D-09/Plan 03):
	// nil by default, set via SetCancelPublisher after construction so
	// NewServer's signature stays stable for existing callers
	// (handlers_test.go, cmd/api/main.go).
	cancelPublisher CancelPublisher
	// jobsListDefaultPageSize / jobsListMaxPageSize are the D-06 page-size
	// knobs for GET /v1/jobs: the default page size used when ?limit is
	// absent/unparseable, and the hard ceiling ?limit clamps to (never
	// rejected above it — that's the DoS guard, T-40-06).
	jobsListDefaultPageSize int
	jobsListMaxPageSize     int
	// ssePollInterval / sseHeartbeatInterval / sseMaxStreamDuration are the
	// PUSH-01 (Phase 42) GET /v1/jobs/{id}/events tunables: how often
	// handleJobEvents re-reads the job row, how often it sends a `:
	// keepalive` comment to survive proxy idle-timeouts, and the safety cap
	// that closes a stream whose job never reaches a terminal status
	// (D-01/D-02). Never zero after NewServer -- zero-value-triggers-default
	// mirrors PresignTTL's own treatment above.
	ssePollInterval      time.Duration
	sseHeartbeatInterval time.Duration
	sseMaxStreamDuration time.Duration
	// quotaEnforcementDisabled mirrors Config.QuotaEnforcementDisabled (plan
	// 53-07): reporting-only state read by handleSystemUsage, never consulted
	// by any admission decision.
	quotaEnforcementDisabled bool
}

// SetCancelPublisher installs the CancelPublisher a successful cancel
// best-effort publishes to (Plan 03 wires the real Redis pub/sub
// implementation here). Never called is equivalent to a permanently-nil
// publisher: handleCancelJob already skips the publish entirely in that
// case.
func (s *Server) SetCancelPublisher(p CancelPublisher) {
	s.cancelPublisher = p
}

// Config configures a Server.
type Config struct {
	MaxUploadBytes               int64
	MaxImagePixels               uint64
	MaxDocumentUncompressedBytes uint64
	// MaxPandocInputBytes is the API-side mirror of the worker's
	// PANDOC_MAX_INPUT_BYTES (enforcePandocInputSize, markupsniff.go) --
	// PR#2 finding-1. Without it, NewServer's own default gives pandoc the
	// same 100 MiB ceiling as every other text engine, while every real
	// deployment configures the worker's cap far lower (3 MiB); an upload
	// between the two limits was accepted 202, burned an S3 object and a
	// queue slot, then failed asynchronously in the worker with
	// input_too_large. Setting this field makes the API reject that same
	// upload 413 at the upload gate instead.
	//
	// Precedence: this scalar is folded into NewServer's own D-07 default
	// map ONLY when MaxEngineBytes is left nil -- it never mutates a
	// caller-supplied MaxEngineBytes map. A caller that sets MaxEngineBytes
	// directly still owns every key in it, this field included; the two are
	// alternative ways to configure the same pandoc ceiling, not layered on
	// top of each other. Zero means "not configured": NewServer's 100 MiB
	// default stays in effect, so an absent env var is never a behaviour
	// change.
	MaxPandocInputBytes int64
	// MaxEngineBytes is the D-07 (Phase 35) per-engine post-detection
	// ceiling: a nil map triggers NewServer's seven-engine default
	// (image/document/html/audio/ocr/pandoc at 100 MiB, av at 2 GiB, D-03
	// Phase 44; pandoc's 100 MiB is itself overridden by MaxPandocInputBytes
	// when that scalar is set, PR#2 finding-1); a non-nil map is used as-is
	// -- an engine key you omit from it is simply never gated by this check,
	// and MaxPandocInputBytes has no effect when this map is supplied
	// directly.
	MaxEngineBytes     map[string]int64
	PresignTTL         time.Duration
	IPRateLimitRPM     int
	ClientRateLimitRPM int
	// OperatorClientIDs is the parsed OPERATOR_CLIENT_IDS allowlist (D-01).
	// A nil or empty map means zero operators (fail-closed) -- every caller,
	// including an otherwise-valid resolved client, is denied the
	// /v1/system/presets subtree.
	OperatorClientIDs map[uuid.UUID]struct{}
	// QuotaEnforcementDisabled mirrors the value cmd/api/main.go passes to
	// jobsRepo.SetQuotaEnforcementDisabled (QUOT-04/D-13) -- the service-wide
	// kill-switch that turns every quota verdict into report-only, service-
	// wide. This field is REPORTING-ONLY at the API layer: it never changes
	// an admission decision (the decision lives in internal/jobs); it exists
	// solely so GET /v1/system/usage (plan 53-07, WR-02) can stop claiming
	// its `remaining` figures are authoritative while service-wide
	// enforcement is off. Default false is correct and fail-safe: an
	// unwired call site reports "enforcement live", matching the pre-
	// existing behaviour before this field existed.
	QuotaEnforcementDisabled bool
	// JobsListDefaultPageSize / JobsListMaxPageSize are the D-06 GET
	// /v1/jobs page-size knobs. Zero triggers NewServer's 20/100 defaults --
	// measure-don't-assume starting points, tunable via env without a code
	// change.
	JobsListDefaultPageSize int
	JobsListMaxPageSize     int
	// SSEPollInterval / SSEHeartbeatInterval / SSEMaxStreamDuration are the
	// PUSH-01 GET /v1/jobs/{id}/events tunables (D-01/D-02). Zero triggers
	// NewServer's 1s / 15s / 30m defaults.
	SSEPollInterval      time.Duration
	SSEHeartbeatInterval time.Duration
	SSEMaxStreamDuration time.Duration
}

// NewServer builds an API server. resolver authenticates every /v1 request
// (see routes.go); it is a narrow interface, keeping interfaces positional
// and Config reserved for tunables only. health carries the three
// dependency pingers /healthz probes (OBS-02). presets is the narrow
// PresetRepo used by handleCreateJob to resolve preset=<name> (D-09).
// presetAdmin is the second, separate interface (D-08) the /v1/presets REST
// handlers depend on — both are typically backed by the same *presets.Repo.
// usage and limits back GET /v1/system/usage (OPSV-01/OPSV-02, Plan 04) —
// both are required positional params with no defaulting; a nil here is a
// programming error at a call site, not a configurable state.
func NewServer(repo Repo, storage Storage, queue Enqueuer, presets PresetRepo, presetAdmin PresetAdmin, usage UsageRepo, limits LimitsRepo, resolver auth.ClientResolver, health HealthDeps, cfg Config) *Server {
	if cfg.PresignTTL == 0 {
		cfg.PresignTTL = 15 * time.Minute
	}
	if cfg.MaxUploadBytes == 0 {
		cfg.MaxUploadBytes = 100 << 20 // 100 MiB
	}
	if cfg.MaxImagePixels == 0 {
		cfg.MaxImagePixels = 100_000_000 // D-05: 100 megapixels default
	}
	if cfg.MaxDocumentUncompressedBytes == 0 {
		cfg.MaxDocumentUncompressedBytes = 500 << 20 // D-04: 500 MiB default
	}
	if cfg.MaxEngineBytes == nil {
		// D-07: image/document/html/audio hold their pre-Phase-35 effective
		// 100 MiB ceiling; av alone gets the raised 2 GiB allowance video
		// legitimately needs. ocr (D-03, Phase 44) gets the same 100 MiB
		// ceiling as image -- reasonable for images and scanned PDFs; the
		// actual OCR work is separately bounded by the page-count/pixel caps
		// enforced in the worker (OCRE-02, Phase 43). pandoc (PDCP-01, Phase
		// 48, D-02) gets the configured PANDOC_MAX_INPUT_BYTES when
		// cfg.MaxPandocInputBytes is set (PR#2 finding-1), falling back to
		// the same generous 100 MiB ceiling when it is not -- markup text is
		// small, so 100 MiB is a generous upload-time bound absent a
		// configured cap. Either way the pandoc DoS posture is additionally
		// bounded by enforcePandocInputSize inside the converter itself
		// (markupsniff.go, Phase 47), enforced again at Convert-time once
		// cmd/pandoc-worker (Phase 49) re-registers a configured
		// PandocConverter -- this API-side gate is defence in depth, not a
		// replacement for it.
		cfg.MaxEngineBytes = map[string]int64{
			convert.EngineImage:    100 << 20,
			convert.EngineDocument: 100 << 20,
			convert.EngineHTML:     100 << 20,
			convert.EngineAudio:    100 << 20,
			convert.EngineAV:       2 << 30,
			convert.EngineOCR:      100 << 20,
			convert.EnginePandoc:   100 << 20,
		}
		if cfg.MaxPandocInputBytes > 0 {
			cfg.MaxEngineBytes[convert.EnginePandoc] = cfg.MaxPandocInputBytes
		}
	}
	if cfg.IPRateLimitRPM == 0 {
		cfg.IPRateLimitRPM = 60 // coarse pre-auth flood guard, conservative default
	}
	if cfg.ClientRateLimitRPM == 0 {
		cfg.ClientRateLimitRPM = 120 // per-client, conservative for internal batch + interactive usage
	}
	if cfg.JobsListDefaultPageSize == 0 {
		cfg.JobsListDefaultPageSize = 20 // D-06 default page size
	}
	if cfg.JobsListMaxPageSize == 0 {
		cfg.JobsListMaxPageSize = 100 // D-06 clamp ceiling
	}
	if cfg.SSEPollInterval == 0 {
		cfg.SSEPollInterval = 1 * time.Second // D-01 default poll cadence
	}
	if cfg.SSEHeartbeatInterval == 0 {
		cfg.SSEHeartbeatInterval = 15 * time.Second // D-01 default heartbeat cadence
	}
	if cfg.SSEMaxStreamDuration == 0 {
		cfg.SSEMaxStreamDuration = 30 * time.Minute // D-02 default safety cap
	}
	operators := cfg.OperatorClientIDs
	if operators == nil {
		operators = map[uuid.UUID]struct{}{}
	}
	return &Server{
		repo:                         repo,
		storage:                      storage,
		queue:                        queue,
		presets:                      presets,
		presetAdmin:                  presetAdmin,
		usageRepo:                    usage,
		limitsRepo:                   limits,
		resolver:                     resolver,
		health:                       health,
		maxUploadByte:                cfg.MaxUploadBytes,
		maxEngineBytes:               cfg.MaxEngineBytes,
		maxImagePixels:               cfg.MaxImagePixels,
		maxDocumentUncompressedBytes: cfg.MaxDocumentUncompressedBytes,
		presignTTL:                   cfg.PresignTTL,
		ipRateRPM:                    cfg.IPRateLimitRPM,
		clientRateRPM:                cfg.ClientRateLimitRPM,
		operators:                    operators,
		jobsListDefaultPageSize:      cfg.JobsListDefaultPageSize,
		jobsListMaxPageSize:          cfg.JobsListMaxPageSize,
		ssePollInterval:              cfg.SSEPollInterval,
		sseHeartbeatInterval:         cfg.SSEHeartbeatInterval,
		sseMaxStreamDuration:         cfg.SSEMaxStreamDuration,
		quotaEnforcementDisabled:     cfg.QuotaEnforcementDisabled,
	}
}
