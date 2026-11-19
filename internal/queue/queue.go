// Package queue defines the asynq task types and helpers used to dispatch
// conversion work to the engine-class workers.
package queue

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/tileforge/octoconv/internal/convert"
)

// Task type names. One asynq task type per engine class operation.
const (
	TypeWebhookDeliver  = "webhook:deliver"
	TypeDocumentConvert = "document:convert"
	// TypeProbeSanitize is the first task type whose operation half is not
	// "convert". The naming convention is "<engine>:<operation>", and probe
	// serves sanitize; calling it "probe:convert" for symmetry would put a
	// false operation name into every asynq dashboard and log line.
	TypeProbeSanitize = "probe:sanitize"
	// TypeTectonicConvert is the ninth class and back to the "<engine>:
	// <operation>" convention probe departed from: tectonic serves convert.
	TypeTectonicConvert = "tectonic:convert"
	TypeCADJob          = "cad:job"
)

// Queue names. asynq routes tasks to a queue per engine class so workers and
// autoscaling can be scoped to a single class. QueueDocument is tied to
// convert.EngineDocument (the single source of truth for engine-class
// literals, DEBT-02) so queue names cannot drift from the engine-class
// identifiers. Image has no queue.
const (
	QueueWebhook  = "webhook"
	QueueDocument = convert.EngineDocument
	QueueProbe    = convert.EngineProbe
	QueueTectonic = convert.EngineTectonic
	QueueCAD      = convert.EngineCAD
)

// ConvertPayload is the task payload. It carries only the job id — all task
// details live in Postgres, the system of record.
type ConvertPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// WebhookPayload is the task payload for a webhook delivery attempt. It
// carries only the job id — the handler re-reads callback_url/status/error
// details from Postgres per attempt, same minimal-payload discipline as
// ConvertPayload (D-04).
type WebhookPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// ParseConvertPayload decodes a ConvertPayload from a task body.
func ParseConvertPayload(b []byte) (ConvertPayload, error) {
	var p ConvertPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("unmarshal convert payload: %w", err)
	}
	return p, nil
}

// NewDocumentConvertTask builds an asynq task for a document conversion job,
// routed to the document queue, bounded to maxRetry (DOCUMENT_MAX_RETRY,
// since each document attempt is expensive at up to DOCUMENT_ENGINE_TIMEOUT
// seconds), and carrying a per-job asynq.Unique lock (uniqueTTL, see
// DocumentUniqueTTL) so a second enqueue for the same jobID while the first
// task/lock is still live collides on the same uniqueness key and returns
// asynq.ErrDuplicateTask instead of creating a second concurrent task.
// Reuses ConvertPayload/ParseConvertPayload (no new payload type needed, all
// job detail is re-read from Postgres).
func NewDocumentConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeDocumentConvert, b,
		asynq.Queue(QueueDocument),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

// NewWebhookDeliverTask builds an asynq task for a single webhook delivery
// job, routed to the webhook queue and bounded to MaxRetry=6 (D-05) — retry
// count is a property of the task, set once at creation. Carries a per-job
// asynq.Unique lock (uniqueTTL, see WebhookUniqueTTL) so a second enqueue for
// the same jobID while the first task/lock is still live collides on the
// same uniqueness key and returns asynq.ErrDuplicateTask instead of creating
// a second concurrent task — the mechanism RECON-04's gap sweep relies on,
// mirroring NewDocumentConvertTask's asynq.Unique shape exactly.
func NewWebhookDeliverTask(jobID uuid.UUID, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(WebhookPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal webhook payload: %w", err)
	}
	return asynq.NewTask(TypeWebhookDeliver, b,
		asynq.Queue(QueueWebhook),
		asynq.MaxRetry(webhookMaxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

// ParseWebhookPayload decodes a WebhookPayload from a task body.
func ParseWebhookPayload(b []byte) (WebhookPayload, error) {
	var p WebhookPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("unmarshal webhook payload: %w", err)
	}
	return p, nil
}

// webhookRetrySchedule is the base backoff schedule for webhook delivery
// retries (D-05): 30s, 1m, 2m, 4m, 8m, 15m — a ~30 minute total retry window
// across MaxRetry=6 attempts.
var webhookRetrySchedule = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	15 * time.Minute,
}

// WebhookRetryDelay is an asynq RetryDelayFunc for the webhook queue. asynq
// calls this with n = the 0-based count of retries so far (0 on the first
// retry, after the first delivery attempt failed), so n indexes directly
// into webhookRetrySchedule with no off-by-one adjustment. The index is
// clamped to the last entry once n exceeds the schedule length, with up to
// ±25% jitter so simultaneously-failing deliveries don't thundering-herd a
// recovering endpoint (D-05).
func WebhookRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(webhookRetrySchedule) {
		idx = len(webhookRetrySchedule) - 1
	}
	base := webhookRetrySchedule[idx]

	// Jitter of up to ±25%.
	jitterRange := float64(base) * 0.25
	jitter := (rand.Float64()*2 - 1) * jitterRange
	delay := time.Duration(float64(base) + jitter)
	if delay < 0 {
		delay = 0
	}
	return delay
}

// documentRetrySchedule is the backoff schedule for document conversion
// retries: 5s, 15s, 30s — no jitter, mirroring imageRetrySchedule's shape
// exactly but scaled up proportionally to DOCUMENT_ENGINE_TIMEOUT being
// ~2.5x ENGINE_TIMEOUT (300s vs 120s), since a transient document conversion
// failure (LibreOffice engine hiccup, storage blip) should still be retried
// a few times quickly, just on a slightly longer cadence than image's
// 2s/5s/15s given the heavier per-attempt cost.
var documentRetrySchedule = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
}

// DocumentRetryDelay is an asynq RetryDelayFunc for the document queue.
// Mirrors ImageRetryDelay's clamp-index-to-schedule-length shape exactly —
// NOT WebhookRetryDelay's jittered shape — since document retries don't need
// jitter to avoid thundering-herding a shared external endpoint the way
// webhook deliveries do.
func DocumentRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(documentRetrySchedule) {
		idx = len(documentRetrySchedule) - 1
	}
	return documentRetrySchedule[idx]
}

// probeRetrySchedule is the backoff schedule for sanitize retries: 5s, 20s.
//
// Same shape as pandocRetrySchedule and, like it, not copied from it in
// meaning: the schedule only ever fires for the branches isProbeTerminal
// classifies as transient, which for sanitize is a ghostscript timeout under
// load. A load spike clears on the order of seconds, not minutes, so a short
// schedule is right here for a reason that is about ghostscript-under-load and
// not about pandoc.
var probeRetrySchedule = []time.Duration{
	5 * time.Second,
	20 * time.Second,
}

// ProbeRetryDelay is an asynq RetryDelayFunc for the probe queue, with the
// same clamp-index-to-schedule-length shape as PandocRetryDelay so
// probeBackoffSum can call it deterministically.
func ProbeRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(probeRetrySchedule) {
		idx = len(probeRetrySchedule) - 1
	}
	return probeRetrySchedule[idx]
}

// RetryDelayFunc dispatches to a per-queue retry delay function based on the
// task type. It fixes the confirmed defect where every queue silently
// inherited WebhookRetryDelay via a single server-wide
// asynq.Config.RetryDelayFunc: webhook tasks keep their existing schedule,
// and any future task type falls back to asynq's own default rather than
// accidentally reusing the webhook schedule.
func RetryDelayFunc(n int, e error, t *asynq.Task) time.Duration {
	switch t.Type() {
	case TypeWebhookDeliver:
		return WebhookRetryDelay(n, e, t)
	case TypeDocumentConvert:
		return DocumentRetryDelay(n, e, t)
	case TypeProbeSanitize:
		return ProbeRetryDelay(n, e, t)
	case TypeTectonicConvert:
		return TectonicRetryDelay(n, e, t)
	case TypeCADJob:
		return CADRetryDelay(n, e, t)
	default:
		return asynq.DefaultRetryDelayFunc(n, e, t)
	}
}

// uniqueTTLSafetyMargin is fixed headroom added on top of the computed
// worst-case image-retry lifetime, covering the case where every attempt
// hangs for the full ENGINE_TIMEOUT.
const uniqueTTLSafetyMargin = 2 * time.Minute

// documentBackoffSum sums DocumentRetryDelay(i) for i in [0, maxRetry) — the
// total backoff time asynq spends waiting before each of maxRetry retries,
// clamped to the last schedule entry once i reaches
// len(documentRetrySchedule). This mirrors imageBackoffSum and deliberately
// does NOT follow webhookBackoffSum's jitter-ceiling shape: calling
// DocumentRetryDelay directly here is safe because DocumentRetryDelay — like
// ImageRetryDelay — has no jitter, so there is no random sample to
// accidentally bake into the derivation.
func documentBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += DocumentRetryDelay(i, nil, nil)
	}
	return sum
}

// DocumentUniqueTTL derives the per-job asynq.Unique lock TTL for document
// conversion tasks from the actual retry budget (maxRetry, normally
// DOCUMENT_MAX_RETRY) and the per-attempt bound (engineTimeout, normally
// DOCUMENT_ENGINE_TIMEOUT), mirroring ImageUniqueTTL's derivation exactly so
// the lock can never silently drift under asynq's true worst-case retry
// lifetime if either env var changes later.
//
// Worst-case formula: (maxRetry+1) * engineTimeout + documentBackoffSum(maxRetry) + margin.
// Same "(maxRetry+1) executions, not maxRetry" correction as ImageUniqueTTL
// applies here (asynq's archive condition is checked AFTER each failed
// attempt). REUSES the shared uniqueTTLSafetyMargin const verbatim — no
// document-specific margin constant.
//
// The TTL is DERIVED rather than a hardcoded constant deliberately: if
// DOCUMENT_MAX_RETRY or DOCUMENT_ENGINE_TIMEOUT are later changed via env,
// the margin scales with them automatically and cannot fall behind the real
// retry lifetime.
func DocumentUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + documentBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// AllConvertQueues returns the queue name for every engine class that still
// has an asynq queue (document/probe/tectonic/cad).
// Image, html, audio, av, ocr, and pandoc are admitted and have no queue. QueueWebhook is excluded:
// webhook is not an engine class and is never KEDA-scaled per conversion.
// This is the single source of truth for cmd/api/main.go's
// queue-depth collector arg list: metrics.NewQueueDepthCollector is
// VARIADIC, so a hand-maintained call-site list can silently omit a queue
// with no compile error, dropping the Prometheus series KEDA scales the
// worker on (D-06, RESEARCH.md Pitfall 2). See
// TestAllConvertQueuesCoversEveryEngine for the completeness guard against a
// future engine class being added here without also being added to
// convert.go's Engine* constants (or vice versa). Plan 04 rewires
// cmd/api/main.go to spread this helper.
func AllConvertQueues() []string {
	return []string{QueueDocument, QueueProbe, QueueTectonic, QueueCAD}
}

// probeBackoffSum sums ProbeRetryDelay(i) for i in [0, maxRetry).
func probeBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += ProbeRetryDelay(i, nil, nil)
	}
	return sum
}

// ProbeUniqueTTL derives the per-job asynq.Unique lock TTL for sanitize tasks
// from the real retry budget and the per-attempt bound, mirroring
// PandocUniqueTTL's derivation exactly so the lock cannot drift under asynq's
// worst case. Reuses uniqueTTLSafetyMargin verbatim -- no probe-specific
// margin constant.
func ProbeUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + probeBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// NewProbeSanitizeTask builds an asynq task for a single sanitize job, routed
// to the probe queue, bounded to maxRetry (PROBE_MAX_RETRY, default 2) and
// carrying a per-job asynq.Unique lock. Reuses ConvertPayload: the payload
// still carries only the job id, and every detail is still re-read from
// Postgres, so the operation change needs no payload of its own.
func NewProbeSanitizeTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeProbeSanitize, b,
		asynq.Queue(QueueProbe),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

// webhookJitterCeiling is WebhookRetryDelay's documented maximum +25% jitter
// bound. webhookBackoffSum uses this to compute a genuine worst-case backoff
// sum rather than a random sample.
const webhookJitterCeiling = 1.25

// webhookMaxRetry mirrors the literal 6 passed to asynq.MaxRetry in
// NewWebhookDeliverTask. Unlike imageMaxRetry (IMAGE_MAX_RETRY), this is NOT
// env-configurable — Phase 2's D-05 fixed MaxRetry=6 as a constant; kept here
// as a named const (not a magic number) so NewWebhookDeliverTask and
// WebhookUniqueTTL can never silently drift apart. Revisit only if a future
// phase (D-05 in that phase's own numbering) makes it configurable.
const webhookMaxRetry = 6

// webhookPerAttemptTimeout MUST match internal/webhook/deliver.go's
// NewDeliverer HTTP client Timeout field. There is no shared exported
// constant between the two packages (internal/webhook does not export one,
// and internal/queue does not currently import internal/webhook) — this is a
// manually-maintained invariant, same discipline as detailActionRecovery's
// single-source-of-truth comment in internal/jobs/repo.go. If deliver.go's
// timeout ever changes, this constant must change with it.
const webhookPerAttemptTimeout = 10 * time.Second

// webhookBackoffSum sums the WORST-CASE (jitter-inflated) backoff for i in
// [0, maxRetry), using the raw webhookRetrySchedule directly rather than
// calling the jittered WebhookRetryDelay. Calling WebhookRetryDelay here
// would bake in a random sample each time, silently violating the "always
// exceeds worst case" contract this TTL must satisfy (see ImageUniqueTTL's
// doc comment for the same contract on the image side, where it holds
// trivially because ImageRetryDelay has no jitter). This is the single most
// important deviation from imageBackoffSum's shape — do NOT port it
// verbatim.
func webhookBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		idx := i
		if idx >= len(webhookRetrySchedule) {
			idx = len(webhookRetrySchedule) - 1
		}
		sum += time.Duration(float64(webhookRetrySchedule[idx]) * webhookJitterCeiling)
	}
	return sum
}

// WebhookUniqueTTL derives the per-job asynq.Unique lock TTL for webhook
// delivery tasks, mirroring ImageUniqueTTL's derivation exactly: (maxRetry+1)
// total attempts (asynq's archive check runs AFTER each failed attempt, same
// as the image queue) times the per-attempt bound, plus the worst-case
// (jitter-inflated) backoff sum, plus the shared safety margin. REUSES
// uniqueTTLSafetyMargin verbatim (RESEARCH.md A1) rather than a
// webhook-specific margin constant.
//
// Worst-case formula: (maxRetry+1) * perAttemptTimeout + webhookBackoffSum(maxRetry) + margin.
// For the fixed webhook constants (maxRetry=6, perAttemptTimeout=10s):
//
//	7*10s + (37.5+75+150+300+600+1125)s + 120s = 70s + 2287.5s + 120s = 2477.5s (~41m17.5s)
//
// — meaningfully longer than the "~30 minute" backoff-window figure quoted in
// Phase 2's context, because that figure did not account for jitter, the
// "+1 attempt" correction, or the safety margin.
//
// The TTL is DERIVED rather than a hardcoded constant deliberately: if
// webhookMaxRetry or webhookPerAttemptTimeout are later changed, the margin
// scales with them automatically and cannot fall behind the real retry
// lifetime.
//
// SOUNDNESS CAVEAT (weaker than ImageUniqueTTL's): ImageUniqueTTL's
// correctness depends on Plan 02 (03-02) wrapping the ENTIRE image-conversion
// attempt in a single context.WithTimeout(ctx, ENGINE_TIMEOUT). No equivalent
// wrapping exists for HandleWebhookDeliver (internal/worker/worker.go) — only
// the outbound HTTP POST itself is bounded, at 10s, via webhook.Deliverer's
// http.Client.Timeout. The Postgres reads (Get, Outputs) and presign-URL
// generation that happen before the POST are NOT wrapped in any per-attempt
// deadline. This is an accepted, pre-existing residual risk (out of this
// phase's scope), documented here rather than silently assumed.
func WebhookUniqueTTL(maxRetry int, perAttemptTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*perAttemptTimeout + webhookBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// RedisOpt builds the asynq Redis connection options from REDIS_ADDR.
func RedisOpt() (asynq.RedisClientOpt, error) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		return asynq.RedisClientOpt{}, fmt.Errorf("REDIS_ADDR must be set")
	}
	return asynq.RedisClientOpt{Addr: addr}, nil
}

// TectonicRetryDelay is the tex engine's backoff. Identical in shape to
// ProbeRetryDelay and PandocRetryDelay -- and identical is the right answer
// here, unlike a TIMEOUT, which must never be copied between engines
// (PITFALLS.md Pitfall 7). A backoff schedule expresses how long to wait
// before trying again; nothing about typesetting makes that different from
// sanitizing.
func TectonicRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	return ProbeRetryDelay(n, e, t)
}

// tectonicBackoffSum sums TectonicRetryDelay(i) for i in [0, maxRetry).
func tectonicBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += TectonicRetryDelay(i, nil, nil)
	}
	return sum
}

// TectonicUniqueTTL derives the per-job asynq.Unique lock TTL from the real
// retry budget and the per-attempt bound, mirroring ProbeUniqueTTL exactly.
//
// IN-02 applies: this must be computed from the SAME TECTONIC_ENGINE_TIMEOUT
// every queue.NewClient()-constructing process reads, because the derivation
// happens in each enqueuing process and a mismatch silently produces duplicate
// processing rather than an error.
func TectonicUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + tectonicBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// NewTectonicConvertTask builds an asynq task for a single latex->pdf job,
// routed to the tectonic queue. Reuses ConvertPayload: the payload carries only
// the job id and every detail is re-read from Postgres, the system of record.
func NewTectonicConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeTectonicConvert, b,
		asynq.Queue(QueueTectonic),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

func CADRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	return ProbeRetryDelay(n, e, t)
}

func cadBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += CADRetryDelay(i, nil, nil)
	}
	return sum
}

func CADUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + cadBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

func NewCADJobTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeCADJob, b,
		asynq.Queue(QueueCAD),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}
