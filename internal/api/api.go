// Package api implements the HTTP layer: accepting conversion jobs and
// reporting their status.
package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
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
	EnqueueProbeSanitize(ctx context.Context, jobID uuid.UUID) error
	EnqueueTectonicConvert(ctx context.Context, jobID uuid.UUID) error
	EnqueueCADJob(ctx context.Context, jobID uuid.UUID) error
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
	maxFormatBytes map[string]map[string]int64
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
	// inspectMaxUploadBytes / inspectMaxWorkBytes / inspectMaxWorkDuration
	// mirror Config's identically-named fields (D-06, Plan 58-06 Task 3);
	// see those doc comments for the ceiling each one enforces and why the
	// two byte ceilings are separate fields, not one. Plan 58-07's POST
	// /v1/inspect handler is the only reader.
	inspectMaxUploadBytes  int64
	inspectMaxWorkBytes    int64
	inspectMaxWorkDuration time.Duration
	// inspectSem is the constructed D-06 mechanism-2 semaphore
	// (internal/api/inspectgate.go), sized by Config.InspectMaxConcurrent.
	// Never nil after NewServer -- newInspectSemaphore applies its own
	// documented default for a non-positive input.
	inspectSem *inspectSemaphore
	// inspectIntakeSem bounds concurrent http.Request.ParseMultipartForm
	// calls -- the buffering step BEFORE inspectSem above is even requested
	// (PR9R8-F1-INTAKE-BOUND, internal/api/inspect_handlers.go). Deliberately
	// a fixed-capacity channel, not something Config sizes: see
	// inspectIntakeMaxConcurrent's own doc comment (inspect_handlers.go) for
	// why this bound protects a deployment-topology fact (the pod's own
	// resources.limits.memory), not a per-environment tuning knob. Never nil
	// after NewServer.
	inspectIntakeSem chan struct{}
	// inspectRateRPM mirrors Config.InspectRateLimitRPM (D-06 mechanism 4).
	inspectRateRPM int
	// engineTimeouts mirrors Config.EngineTimeouts (INSP-04, Plan 58-07):
	// the per-engine upper bound POST /v1/inspect's availableOperations
	// reports as estimated_max_seconds. Never nil after NewServer -- a
	// caller that leaves Config.EngineTimeouts nil gets the seven-engine
	// default map (see NewServer), never an empty map that would silently
	// report every engine's ceiling as 0.
	engineTimeouts map[string]time.Duration
}

// inspectRateLimit is the chi middleware constructor Plan 58-07's POST
// /v1/inspect route mounts for its own rate budget (D-06 mechanism 4).
// See newInspectRateLimit's doc comment (internal/api/inspectgate.go) for
// the independence argument and the D-07 replicas:1 caveat; this method
// only supplies the rpm NewServer already resolved from Config.
func (s *Server) inspectRateLimit() func(http.Handler) http.Handler {
	return newInspectRateLimit(s.inspectRateRPM)
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
	MaxFormatBytes     map[string]map[string]int64
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
	// InspectMaxUploadBytes is the pre-parse http.MaxBytesReader ceiling
	// Plan 58-07's POST /v1/inspect applies to the request BODY, before any
	// byte is handed to a parser. It is deliberately SEPARATE from, and
	// smaller than, MaxUploadBytes (the job-creation path's own ceiling,
	// currently 2 GiB): inspect never writes to S3 and never enqueues a
	// job, so there is no reason to accept a multi-gigabyte body just to
	// look at its magic bytes. Zero triggers NewServer's 100 MiB default
	// (58-CONTEXT.md D-06, SC6-calibrated starting value, not measured).
	// Exceeding THIS ceiling is a 413 -- a rejection of the request itself
	// -- which is a different failure than exceeding InspectMaxWorkBytes
	// below (INSP-03's partial-coverage report, never a rejection).
	InspectMaxUploadBytes int64
	// InspectMaxWorkBytes is the byte ceiling on how much of the ALREADY-
	// ACCEPTED upload every family analyzer sharing one inspect operation
	// may read in total (internal/inspect.Budget.MaxBytes, D-06 mechanism
	// 1). The distinction from InspectMaxUploadBytes above, stated plainly
	// because it is easy to conflate: InspectMaxUploadBytes bounds how many
	// bytes the service will ACCEPT onto the wire; InspectMaxWorkBytes
	// bounds how many of the accepted bytes the service will actually
	// READ/PARSE. Exhausting the former is a 413 (request rejected);
	// exhausting the latter marks the report's Coverage partial (INSP-03)
	// -- the report is still returned, honestly labeled incomplete, never
	// refused. Zero triggers NewServer's 8 MiB default (SC6-calibrated,
	// mirrors internal/inspect.Budget's own default).
	InspectMaxWorkBytes int64
	// InspectMaxWorkDuration is the wall-clock ceiling on the same
	// operation (internal/inspect.Budget.MaxDuration, D-06 mechanism 1) --
	// a courtesy outer bound, not the load-bearing mechanism (see
	// internal/inspect/budget.go's Tracker doc comment for why a context
	// deadline alone cannot stop a CPU-bound parsing loop). Zero triggers
	// NewServer's 2s default (SC6-calibrated).
	InspectMaxWorkDuration time.Duration
	// InspectMaxConcurrent is the capacity of the semaphore bounding how
	// many inspect requests may be parsing bytes at once (D-06 mechanism
	// 2, internal/api/inspectgate.go). This number MUST stay comfortably
	// below DB_MAX_CONNS (internal/db/db.go's Connect, default 10 = 4
	// inspect + 6 headroom): the semaphore slot is held for the whole
	// inspect request, including its Repo.PreAdmit call (INSP-04), so the
	// number of concurrent PreAdmit calls a burst of inspect requests can
	// issue against the shared *pgxpool.Pool is bounded by this exact
	// number (D-06a) -- raising this without raising DB_MAX_CONNS to match
	// reopens the pool-contention path that plan closed. Zero triggers
	// NewServer's 4 default (SC6-calibrated, matches the arithmetic
	// internal/db/db.go's defaultMaxConns doc comment already assumes).
	InspectMaxConcurrent int
	// InspectRateLimitRPM is inspect's OWN per-client rate budget (D-06
	// mechanism 4, D-07 caveat -- see internal/api/inspectgate.go's
	// newInspectRateLimit doc comment for the full in-process/replicas:1
	// caveat), independent of ClientRateLimitRPM above. Zero triggers
	// NewServer's 30 default -- noticeably below ClientRateLimitRPM's 120,
	// because one inspect parse is a materially more expensive request
	// than one status poll.
	InspectRateLimitRPM int
	// EngineTimeouts is the per-engine upper-bound duration POST
	// /v1/inspect's availableOperations reports as estimated_max_seconds
	// (INSP-04, Plan 58-07) -- the SAME per-engine timeout value each
	// worker process already reads at its own startup (ENGINE_TIMEOUT,
	// DOCUMENT_ENGINE_TIMEOUT, HTML_ENGINE_TIMEOUT, AUDIO_ENGINE_TIMEOUT,
	// AV_ENGINE_TIMEOUT, OCR_ENGINE_TIMEOUT, PANDOC_ENGINE_TIMEOUT), now
	// ALSO read by cmd/api so the API process can report it without a new
	// env var. A nil map (the default when this field is left unset)
	// triggers NewServer's own seven-engine default, mirroring
	// MaxEngineBytes' nil-triggers-default discipline above -- an engine
	// key absent from a caller-supplied map is simply reported with a
	// zero-second ceiling (engineTimeoutSeconds' own documented behavior
	// for a non-positive duration), it is not backfilled from the default.
	EngineTimeouts map[string]time.Duration
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

// defaultEngineTimeouts is the code-level fallback for each engine's upper
// bound, merged into Config.EngineTimeouts per engine by NewServer. Mirrors
// each worker's own startup default for the same env vars
// (ENGINE_TIMEOUT=120s, DOCUMENT_ENGINE_TIMEOUT=300s, HTML_ENGINE_TIMEOUT=60s,
// AUDIO_ENGINE_TIMEOUT=742s, AV_ENGINE_TIMEOUT=753s, OCR_ENGINE_TIMEOUT=744s,
// PANDOC_ENGINE_TIMEOUT=65s, PROBE_ENGINE_TIMEOUT=600s).
//
// PROBE_ENGINE_TIMEOUT is MEASURED as of Phase 60 -- ceil(2.0 s/page x 200
// pages x 1.5) = 600s, see .env.example for the run and the derivation. This
// comment said 180s and "PLACEHOLDER, not a measurement" while the value below
// already read 600s; corrected in Phase 63, whose PROBE_MAX_PAGES ceiling is
// derived from the same arithmetic and would otherwise cite a number this file
// contradicts.
var defaultEngineTimeouts = map[string]time.Duration{
	convert.EngineImage:    120 * time.Second,
	convert.EngineDocument: 300 * time.Second,
	convert.EngineHTML:     60 * time.Second,
	convert.EngineAudio:    742 * time.Second,
	convert.EngineAV:       753 * time.Second,
	convert.EngineOCR:      744 * time.Second,
	convert.EnginePandoc:   65 * time.Second,
	convert.EngineProbe:    600 * time.Second,
	// tectonic: MEASURED 2026-09-04 (scripts/tectonic-timeout-measure.sh; the
	// run, the derivation and the full output are in .env.example and under the
	// phase's evidence/). ceil(25.76 s/pass x 7 passes x 1.5) = 271s, where 7 is
	// the ENGINE's own rerun maximum, not a corpus statistic, and 25.76 s/pass
	// comes from a 2 143 444-byte document -- 2.2% ABOVE the 2 097 152-byte
	// ceiling below, so the cost is taken on slightly more work than the ceiling
	// admits. Conservative by that margin, and stated rather than rounded off:
	// the live check that found this discrepancy is the reason it is written
	// down.
	//
	// The two numbers are ONE decision: cost is linear in source size, so this
	// timeout means nothing without defaultMaxEngineBytes[EngineTectonic]
	// holding at 2 MiB. Changing either alone breaks the pair -- exactly what
	// PROBE_MAX_PAGES=200 and PROBE_ENGINE_TIMEOUT=600s are to each other.
	convert.EngineTectonic: 271 * time.Second,
	convert.EngineCAD:      convert.CADDefaultEngineTimeout,
}

// defaultMaxEngineBytes is the per-engine upload ceiling, merged into
// Config.MaxEngineBytes per engine by NewServer.
//
// D-07: image/document/html/audio hold their pre-Phase-35 effective 100 MiB
// ceiling; av alone gets the raised 2 GiB allowance video legitimately needs.
// ocr (D-03, Phase 44) gets the same 100 MiB as image -- the actual OCR work
// is separately bounded by the page-count/pixel caps in the worker. pandoc
// gets the configured PANDOC_MAX_INPUT_BYTES when Config.MaxPandocInputBytes
// is set, falling back to 100 MiB, and is additionally bounded by
// enforcePandocInputSize inside the converter itself -- this API-side gate is
// defence in depth, not a replacement for it.
//
// probe gets 50 MiB, HALF the ordinary document ceiling, and REASONED rather
// than measured -- the same status as PROBE_ENGINE_TIMEOUT and tracked by the
// same open item. The sanitize path holds the whole input in memory more than
// once (the image walkers os.ReadFile it, the OOXML path buffers every
// uncompressed part plus the rebuilt archive) and then hands the result to
// pdfcpu, which parses it into its own address space, inside a 1 GiB worker.
var defaultMaxEngineBytes = map[string]int64{
	convert.EngineImage:    100 << 20,
	convert.EngineDocument: 100 << 20,
	convert.EngineHTML:     100 << 20,
	convert.EngineAudio:    100 << 20,
	convert.EngineAV:       2 << 30,
	convert.EngineOCR:      100 << 20,
	convert.EnginePandoc:   100 << 20,
	convert.EngineProbe:    50 << 20,
	// tectonic: 2 MiB, and this one IS load-bearing rather than a policy
	// default. It is the other half of TECTONIC_ENGINE_TIMEOUT above, in the
	// same sense PROBE_MAX_PAGES=200 is the other half of probe's 600s.
	//
	// Cost here is linear in SOURCE SIZE at a fixed 7-pass ceiling, measured
	// 2026-09-04 in the worker container at 2 CPU / 1 GiB: 1 071 659 bytes ->
	// 373 pages -> 90.0s, and 2 143 444 bytes -> 745 pages -> 180.3s. That
	// second document is 2.2% LARGER than this ceiling, so the timeout is
	// derived from marginally more work than is admissible. At the 100 MiB this engine
	// inherited from the document-shaped engines, a dense .tex is ~36 000 pages
	// and ~9000s -- TEN TIMES the 900s RECONCILER_ACTIVE_STALE_AFTER cap, which
	// would have the reconciler re-enqueue jobs that are still running. A
	// timeout without this ceiling bounds nothing at all.
	//
	// 2 MiB is 745 pages of dense typeset output -- past any real document --
	// and the timeout above is derived from a run AT this number, never
	// extrapolated to it. Raising one without re-measuring the other breaks the
	// pair: a higher ceiling admits work the timeout will kill, a lower one
	// refuses work the timeout was sized for.
	//
	// NOT about memory, unlike pandoc's 3 MiB: the 2 MiB run peaked well inside
	// the 1 GiB cgroup. This is a bound on TIME.
	convert.EngineTectonic: 2 << 20,
	convert.EngineCAD:      convert.CADMaxIGESBytes,
}

var defaultMaxFormatBytes = map[string]map[string]int64{
	convert.EngineCAD: {
		"step": convert.CADMaxSTEPBytes,
		"iges": convert.CADMaxIGESBytes,
		"stl":  convert.CADMaxSTLBytes,
	},
}

func (s *Server) sizeLimit(engine, format string) (limit int64, perFormat, gated bool) {
	if l, ok := s.maxFormatBytes[engine][format]; ok {
		return l, true, true
	}
	l, ok := s.maxEngineBytes[engine]
	return l, false, ok
}

func formatLimitMessage(format string, limit int64) string {
	if limit%(1<<20) == 0 {
		return format + " file exceeds the " + strconv.FormatInt(limit>>20, 10) + " MiB limit for this conversion"
	}
	return format + " file exceeds the " + strconv.FormatInt(limit, 10) + " bytes limit for this conversion"
}

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
	// ALL-OR-NOTHING here, unlike EngineTimeouts below, and the difference is
	// meaningful rather than an inconsistency: a caller who supplies this map
	// and omits an engine is saying that engine is UNGATED, and
	// availableOperations reports it that way. Merging defaults in would make
	// "ungated" inexpressible. cmd/api/main.go leaves the field nil on
	// purpose (see its own comment), so the defaults below are what production
	// actually runs.
	if cfg.MaxEngineBytes == nil {
		cfg.MaxEngineBytes = make(map[string]int64, len(defaultMaxEngineBytes))
		for engine, def := range defaultMaxEngineBytes {
			cfg.MaxEngineBytes[engine] = def
		}
		if cfg.MaxPandocInputBytes > 0 {
			cfg.MaxEngineBytes[convert.EnginePandoc] = cfg.MaxPandocInputBytes
		}
	}
	if cfg.MaxFormatBytes == nil {
		cfg.MaxFormatBytes = make(map[string]map[string]int64, len(defaultMaxFormatBytes))
		for engine, formats := range defaultMaxFormatBytes {
			cfg.MaxFormatBytes[engine] = make(map[string]int64, len(formats))
			for format, def := range formats {
				cfg.MaxFormatBytes[engine][format] = def
			}
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
	// Five inspect boundaries (58-CONTEXT.md D-06, Plan 58-06 Task 3): every
	// default below is a STARTING value the live gate (Plan 58-08, SC6)
	// calibrates, not a measured number -- copying a number from an
	// unrelated engine class is this project's own documented anti-pattern
	// (see .env.example's AUDIO_ENGINE_TIMEOUT/OCR_ENGINE_TIMEOUT measured-
	// provenance comments), and inventing one with no calibration plan is
	// no better.
	if cfg.InspectMaxUploadBytes == 0 {
		cfg.InspectMaxUploadBytes = 100 << 20 // 100 MiB
	}
	if cfg.InspectMaxWorkBytes == 0 {
		cfg.InspectMaxWorkBytes = 8 << 20 // 8 MiB, mirrors internal/inspect.Budget's own default
	}
	if cfg.InspectMaxWorkDuration == 0 {
		cfg.InspectMaxWorkDuration = 2 * time.Second
	}
	if cfg.InspectMaxConcurrent == 0 {
		cfg.InspectMaxConcurrent = 4 // MUST stay well below DB_MAX_CONNS (internal/db/db.go, D-06a)
	}
	if cfg.InspectRateLimitRPM == 0 {
		cfg.InspectRateLimitRPM = 30 // deliberately below ClientRateLimitRPM's 120
	}
	// MERGED PER ENGINE, unlike MaxEngineBytes above. A missing entry here is
	// not a meaningful state -- EngineTimeouts feeds the upper bound POST
	// /v1/inspect publishes, and zero is not an upper bound. cmd/api/main.go
	// supplies a non-nil map, so before this merge existed, adding the eighth
	// engine to the default map below did nothing in production: the real API
	// published sanitize offers with estimated_max_seconds 0 while the worker
	// used 180s. That is a defect the compiler cannot see and no test at this
	// layer could reach, and it shipped (PR #12). Filling in what a caller
	// omitted makes the omission harmless instead of silent.
	if cfg.EngineTimeouts == nil {
		cfg.EngineTimeouts = map[string]time.Duration{}
	}
	for engine, def := range defaultEngineTimeouts {
		if _, ok := cfg.EngineTimeouts[engine]; !ok {
			cfg.EngineTimeouts[engine] = def
		}
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
		maxFormatBytes:               cfg.MaxFormatBytes,
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
		inspectMaxUploadBytes:        cfg.InspectMaxUploadBytes,
		inspectMaxWorkBytes:          cfg.InspectMaxWorkBytes,
		inspectMaxWorkDuration:       cfg.InspectMaxWorkDuration,
		inspectSem:                   newInspectSemaphore(cfg.InspectMaxConcurrent),
		inspectIntakeSem:             make(chan struct{}, inspectIntakeMaxConcurrent),
		inspectRateRPM:               cfg.InspectRateLimitRPM,
		engineTimeouts:               cfg.EngineTimeouts,
	}
}
