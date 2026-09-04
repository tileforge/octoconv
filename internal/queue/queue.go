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

	"github.com/apaderin/octoconv/internal/convert"
)

// Task type names. One asynq task type per engine class operation.
const (
	TypeImageConvert    = "image:convert"
	TypeWebhookDeliver  = "webhook:deliver"
	TypeDocumentConvert = "document:convert"
	TypeHTMLConvert     = "html:convert"
	TypeAudioConvert    = "audio:convert"
	TypeAVConvert       = "av:convert"
	TypeOCRConvert      = "ocr:convert"
	TypePandocConvert   = "pandoc:convert"
	// TypeProbeSanitize is the first task type whose operation half is not
	// "convert". The naming convention is "<engine>:<operation>", and probe
	// serves sanitize; calling it "probe:convert" for symmetry would put a
	// false operation name into every asynq dashboard and log line.
	TypeProbeSanitize = "probe:sanitize"
	// TypeTectonicConvert is the ninth class and back to the "<engine>:
	// <operation>" convention probe departed from: tectonic serves convert.
	TypeTectonicConvert = "tectonic:convert"
)

// Queue names. asynq routes tasks to a queue per engine class so workers and
// autoscaling can be scoped to a single class. QueueImage/QueueDocument/
// QueueHTML/QueueAudio/QueueAV/QueueOCR/QueuePandoc are tied to
// convert.EngineImage/EngineDocument/EngineHTML/EngineAudio/EngineAV/
// EngineOCR/EnginePandoc (the single source of truth for engine-class
// literals, DEBT-02) so queue names cannot drift from the engine-class
// identifiers.
const (
	QueueImage    = convert.EngineImage
	QueueWebhook  = "webhook"
	QueueDocument = convert.EngineDocument
	QueueHTML     = convert.EngineHTML
	QueueAudio    = convert.EngineAudio
	QueueAV       = convert.EngineAV
	QueueOCR      = convert.EngineOCR
	QueuePandoc   = convert.EnginePandoc
	QueueProbe    = convert.EngineProbe
	QueueTectonic = convert.EngineTectonic
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

// NewImageConvertTask builds an asynq task for an image conversion job,
// routed to the image queue, bounded to maxRetry (D-05, IMAGE_MAX_RETRY —
// small compared to webhook's 6, since image conversion failures should
// retry a few times fast, not linger), and carrying a per-job asynq.Unique
// lock (uniqueTTL, see ImageUniqueTTL) so a second enqueue for the same
// jobID while the first task/lock is still live collides on the same
// uniqueness key and returns asynq.ErrDuplicateTask instead of creating a
// second concurrent task — the mechanism the Plan 03 reconciler relies on.
func NewImageConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeImageConvert, b,
		asynq.Queue(QueueImage),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
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
// routed to the document queue, bounded to maxRetry (DOCUMENT_MAX_RETRY —
// a bounded budget lower than image's, since each document attempt is
// expensive at up to DOCUMENT_ENGINE_TIMEOUT seconds), and carrying a
// per-job asynq.Unique lock (uniqueTTL, see DocumentUniqueTTL) so a second
// enqueue for the same jobID while the first task/lock is still live
// collides on the same uniqueness key and returns asynq.ErrDuplicateTask
// instead of creating a second concurrent task — mirrors
// NewImageConvertTask exactly; reuses ConvertPayload/ParseConvertPayload
// (no new payload type needed, all job detail is re-read from Postgres).
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

// NewHTMLConvertTask builds an asynq task for an html-to-pdf conversion job,
// routed to the html queue, bounded to maxRetry (HTML_MAX_RETRY), and
// carrying a per-job asynq.Unique lock (uniqueTTL, see HTMLUniqueTTL) so a
// second enqueue for the same jobID while the first task/lock is still live
// collides on the same uniqueness key and returns asynq.ErrDuplicateTask
// instead of creating a second concurrent task — mirrors
// NewDocumentConvertTask exactly; reuses ConvertPayload/ParseConvertPayload
// (no new payload type needed, all job detail is re-read from Postgres).
func NewHTMLConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeHTMLConvert, b,
		asynq.Queue(QueueHTML),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

// NewAudioConvertTask builds an asynq task for an audio transcription job,
// routed to the audio queue, bounded to maxRetry (AUDIO_MAX_RETRY), and
// carrying a per-job asynq.Unique lock (uniqueTTL, see AudioUniqueTTL) so a
// second enqueue for the same jobID while the first task/lock is still live
// collides on the same uniqueness key and returns asynq.ErrDuplicateTask
// instead of creating a second concurrent task — mirrors
// NewDocumentConvertTask exactly; reuses ConvertPayload/ParseConvertPayload
// (no new payload type needed, all job detail is re-read from Postgres).
func NewAudioConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeAudioConvert, b,
		asynq.Queue(QueueAudio),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

// NewAVConvertTask builds an asynq task for a video conversion job, routed
// to the av queue, bounded to maxRetry (AV_MAX_RETRY, D-03 LOCKED default
// 2 -- fewer attempts than audio's 3, since each av attempt costs
// materially more), and carrying a per-job asynq.Unique lock (uniqueTTL,
// see AVUniqueTTL) so a second enqueue for the same jobID while the first
// task/lock is still live collides on the same uniqueness key and returns
// asynq.ErrDuplicateTask instead of creating a second concurrent task --
// mirrors NewAudioConvertTask exactly; reuses ConvertPayload/
// ParseConvertPayload (no new payload type needed, all job detail is
// re-read from Postgres).
func NewAVConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeAVConvert, b,
		asynq.Queue(QueueAV),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

// NewOCRConvertTask builds an asynq task for an OCR conversion job (image/pdf
// -> txt/hocr/alto), routed to the ocr queue, bounded to maxRetry
// (OCR_MAX_RETRY, default 2 -- mirrors AV_MAX_RETRY's bounded-retry budget
// since a genuine tesseract/pdftoppm failure is deterministic on the input
// and unlikely to be helped by more than a couple of attempts), and carrying
// a per-job asynq.Unique lock (uniqueTTL, see OCRUniqueTTL) so a second
// enqueue for the same jobID while the first task/lock is still live
// collides on the same uniqueness key and returns asynq.ErrDuplicateTask
// instead of creating a second concurrent task -- mirrors NewAVConvertTask
// exactly; reuses ConvertPayload/ParseConvertPayload (no new payload type
// needed, all job detail is re-read from Postgres).
func NewOCRConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypeOCRConvert, b,
		asynq.Queue(QueueOCR),
		asynq.MaxRetry(maxRetry),
		asynq.Unique(uniqueTTL),
	), nil
}

// NewPandocConvertTask builds an asynq task for a pandoc markup-to-markup
// conversion job (md/rst/org/latex/docx/epub -> another markup format),
// routed to the pandoc queue, bounded to maxRetry (PANDOC_MAX_RETRY, default
// 2 -- mirrors OCRConvertTask's/NewAVConvertTask's bounded-retry budget since
// a genuine pandoc engine/parse failure is deterministic on the input and
// unlikely to be helped by more than a couple of attempts, and D-01 (48-
// CONTEXT.md) classifies timeouts and engine/parse errors as TERMINAL
// anyway), and carrying a per-job asynq.Unique lock (uniqueTTL, see
// PandocUniqueTTL) so a second enqueue for the same jobID while the first
// task/lock is still live collides on the same uniqueness key and returns
// asynq.ErrDuplicateTask instead of creating a second concurrent task --
// mirrors NewOCRConvertTask exactly; reuses ConvertPayload/
// ParseConvertPayload (no new payload type needed, all job detail is
// re-read from Postgres).
func NewPandocConvertTask(jobID uuid.UUID, maxRetry int, uniqueTTL time.Duration) (*asynq.Task, error) {
	b, err := json.Marshal(ConvertPayload{JobID: jobID})
	if err != nil {
		return nil, fmt.Errorf("marshal convert payload: %w", err)
	}
	return asynq.NewTask(TypePandocConvert, b,
		asynq.Queue(QueuePandoc),
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
// mirroring NewImageConvertTask's asynq.Unique shape exactly.
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

// imageRetrySchedule is the fast backoff schedule for image conversion
// retries (D-06): 2s, 5s, 15s — deliberately a seconds-scale schedule,
// distinct from webhookRetrySchedule's minutes-scale 30s->15m window, since
// a transient image conversion failure (engine timeout, storage hiccup)
// should be retried quickly a few times, not ground through slowly.
var imageRetrySchedule = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	15 * time.Second,
}

// ImageRetryDelay is an asynq RetryDelayFunc for the image queue. Mirrors
// WebhookRetryDelay's clamp-index-to-schedule-length shape but WITHOUT
// jitter — D-06 does not require jitter for image retries, since the
// schedule is already short and image tasks are not competing to avoid a
// thundering herd on a shared external endpoint the way webhook deliveries
// are.
func ImageRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(imageRetrySchedule) {
		idx = len(imageRetrySchedule) - 1
	}
	return imageRetrySchedule[idx]
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

// htmlRetrySchedule is the backoff schedule for html-to-pdf conversion
// retries: 5s, 15s, 30s — no jitter, mirroring documentRetrySchedule's shape
// exactly (same rationale: a transient chromium render failure should be
// retried a few times quickly, not ground through slowly, and there is no
// shared external endpoint here that needs jitter to avoid a thundering
// herd).
var htmlRetrySchedule = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
}

// HTMLRetryDelay is an asynq RetryDelayFunc for the html queue. Mirrors
// DocumentRetryDelay's clamp-index-to-schedule-length shape exactly — NOT
// WebhookRetryDelay's jittered shape.
func HTMLRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(htmlRetrySchedule) {
		idx = len(htmlRetrySchedule) - 1
	}
	return htmlRetrySchedule[idx]
}

// audioRetrySchedule is the backoff schedule for audio transcription
// retries: 5s, 15s, 30s — no jitter, mirroring documentRetrySchedule's/
// htmlRetrySchedule's shape exactly (a defensible default: a transient
// audio-pipeline failure — ffmpeg normalize or whisper-cli transcribe — should
// still be retried a few times quickly, on the same cadence as the other
// non-webhook engine classes).
var audioRetrySchedule = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
}

// AudioRetryDelay is an asynq RetryDelayFunc for the audio queue. Mirrors
// DocumentRetryDelay's/HTMLRetryDelay's clamp-index-to-schedule-length shape
// exactly — NOT WebhookRetryDelay's jittered shape.
func AudioRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(audioRetrySchedule) {
		idx = len(audioRetrySchedule) - 1
	}
	return audioRetrySchedule[idx]
}

// avRetrySchedule is the backoff schedule for av conversion retries: 30s,
// 2m -- LOCKED by D-03 (CONTEXT.md): fewer attempts, longer pauses than
// audio's 5s/15s/30s. Each av attempt (ffmpeg transcode/extract/thumbnail)
// costs materially more than audio's normalize+transcribe pass, so the long
// first backoff lets load drain rather than hammering a busy worker 5
// seconds later.
var avRetrySchedule = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
}

// AVRetryDelay is an asynq RetryDelayFunc for the av queue. Mirrors
// AudioRetryDelay's/DocumentRetryDelay's clamp-index-to-schedule-length
// shape exactly -- NOT WebhookRetryDelay's jittered shape -- since
// avBackoffSum must be able to call AVRetryDelay directly and
// deterministically.
func AVRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(avRetrySchedule) {
		idx = len(avRetrySchedule) - 1
	}
	return avRetrySchedule[idx]
}

// ocrRetrySchedule is the backoff schedule for OCR conversion retries: 15s,
// 1m -- PROVISIONAL pending Phase 45's measured OCR_ENGINE_TIMEOUT. Mirrors
// avRetrySchedule's shape (two entries, no jitter) since OCR_MAX_RETRY
// defaults to 2 like AV_MAX_RETRY, but a genuine tesseract/pdftoppm attempt
// is expected to be materially cheaper per-page than an av transcode, so the
// backoff is shorter than av's 30s/2m.
var ocrRetrySchedule = []time.Duration{
	15 * time.Second,
	1 * time.Minute,
}

// OCRRetryDelay is an asynq RetryDelayFunc for the ocr queue. Mirrors
// AVRetryDelay's/AudioRetryDelay's clamp-index-to-schedule-length shape
// exactly -- NOT WebhookRetryDelay's jittered shape -- since ocrBackoffSum
// must be able to call OCRRetryDelay directly and deterministically.
func OCRRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ocrRetrySchedule) {
		idx = len(ocrRetrySchedule) - 1
	}
	return ocrRetrySchedule[idx]
}

// pandocRetrySchedule is the backoff schedule for pandoc conversion retries:
// 5s, 20s -- PROVISIONAL pending Phase 49's measured PANDOC_ENGINE_TIMEOUT.
// Mirrors ocrRetrySchedule's shape (two entries, no jitter) since
// PANDOC_MAX_RETRY defaults to 2 like OCR_MAX_RETRY, but pandoc's own
// values, NOT copied from OCR/AV/audio: pandoc is a fast, deterministic,
// CPU-bound text-transform (D-01, 48-CONTEXT.md classifies its timeouts and
// engine/parse errors as TERMINAL, so this schedule only ever fires for
// genuine infra transients), which is expected to be materially cheaper
// per-attempt than tesseract/pdftoppm OCR, hence the shorter backoff than
// ocrRetrySchedule's 15s/1m.
var pandocRetrySchedule = []time.Duration{
	5 * time.Second,
	20 * time.Second,
}

// PandocRetryDelay is an asynq RetryDelayFunc for the pandoc queue. Mirrors
// OCRRetryDelay's/AVRetryDelay's clamp-index-to-schedule-length shape
// exactly -- NOT WebhookRetryDelay's jittered shape -- since pandocBackoffSum
// must be able to call PandocRetryDelay directly and deterministically.
func PandocRetryDelay(n int, e error, t *asynq.Task) time.Duration {
	idx := n
	if idx < 0 {
		idx = 0
	}
	if idx >= len(pandocRetrySchedule) {
		idx = len(pandocRetrySchedule) - 1
	}
	return pandocRetrySchedule[idx]
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
// asynq.Config.RetryDelayFunc: image tasks now retry on their own fast
// schedule, webhook tasks keep their existing schedule, and any future task
// type falls back to asynq's own default rather than accidentally reusing
// the webhook schedule.
func RetryDelayFunc(n int, e error, t *asynq.Task) time.Duration {
	switch t.Type() {
	case TypeImageConvert:
		return ImageRetryDelay(n, e, t)
	case TypeWebhookDeliver:
		return WebhookRetryDelay(n, e, t)
	case TypeDocumentConvert:
		return DocumentRetryDelay(n, e, t)
	case TypeHTMLConvert:
		return HTMLRetryDelay(n, e, t)
	case TypeAudioConvert:
		return AudioRetryDelay(n, e, t)
	case TypeAVConvert:
		return AVRetryDelay(n, e, t)
	case TypeOCRConvert:
		return OCRRetryDelay(n, e, t)
	case TypePandocConvert:
		return PandocRetryDelay(n, e, t)
	case TypeProbeSanitize:
		return ProbeRetryDelay(n, e, t)
	case TypeTectonicConvert:
		return TectonicRetryDelay(n, e, t)
	default:
		return asynq.DefaultRetryDelayFunc(n, e, t)
	}
}

// uniqueTTLSafetyMargin is fixed headroom added on top of the computed
// worst-case image-retry lifetime, covering the case where every attempt
// hangs for the full ENGINE_TIMEOUT.
const uniqueTTLSafetyMargin = 2 * time.Minute

// imageBackoffSum sums ImageRetryDelay(i) for i in [0, maxRetry) — the total
// backoff time asynq spends waiting before each of maxRetry retries,
// clamped to the last schedule entry once i reaches len(imageRetrySchedule).
func imageBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += ImageRetryDelay(i, nil, nil)
	}
	return sum
}

// ImageUniqueTTL derives the per-job asynq.Unique lock TTL for image
// conversion tasks from the actual retry budget (maxRetry, normally
// IMAGE_MAX_RETRY) and the per-attempt bound (engineTimeout, normally
// ENGINE_TIMEOUT), so the lock can never silently drift under asynq's true
// worst-case retry lifetime if either env var changes later.
//
// Worst-case formula: (maxRetry+1) * engineTimeout + imageBackoffSum(maxRetry) + margin.
// asynq's archive condition is `msg.Retried >= msg.Retry`, checked AFTER
// each failed attempt, so a task with MaxRetry=maxRetry gets maxRetry+1
// total engine executions (1 initial attempt + maxRetry retries) before
// archival — NOT maxRetry. For the defaults (IMAGE_MAX_RETRY=4,
// ENGINE_TIMEOUT=120s) this is 5*120s + (2+5+15+15)s + 120s = 757s,
// comfortably above asynq's 637s worst-case retry lifetime
// (5*120s + 37s).
//
// The lock must outlive that lifetime for two reasons: (a) while asynq is
// still legitimately retrying, a reconciler re-enqueue for the same job
// collides on the still-live lock and is a safe no-op; (b) asynq does NOT
// release the unique lock on archive (only on success or TTL expiry), so
// once the retry budget is exhausted the lock lapses within a bounded
// window and the reconciler can genuinely re-enqueue.
//
// The TTL is DERIVED rather than a hardcoded constant deliberately: if
// IMAGE_MAX_RETRY or ENGINE_TIMEOUT are later changed via env, the margin
// scales with them automatically and cannot fall behind the real retry
// lifetime.
//
// SOUNDNESS DEPENDENCY: this "each attempt <= engineTimeout" worst-case
// bound is sound only because Plan 02 (03-02 T-03-11) bounds the ENTIRE
// conversion attempt — download + convert + upload + record, not just
// conv.Convert() — with a single context.WithTimeout(ctx, ENGINE_TIMEOUT)
// in process(); a stalled S3 transfer on an unbounded ctx would otherwise
// let one attempt exceed engineTimeout and silently outlive this lock.
func ImageUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + imageBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

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

// htmlBackoffSum sums HTMLRetryDelay(i) for i in [0, maxRetry) — the total
// backoff time asynq spends waiting before each of maxRetry retries,
// clamped to the last schedule entry once i reaches len(htmlRetrySchedule).
// Mirrors documentBackoffSum; safe to call HTMLRetryDelay directly since it
// has no jitter (same reasoning as documentBackoffSum's doc comment).
func htmlBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += HTMLRetryDelay(i, nil, nil)
	}
	return sum
}

// HTMLUniqueTTL derives the per-job asynq.Unique lock TTL for html-to-pdf
// conversion tasks from the actual retry budget (maxRetry, normally
// HTML_MAX_RETRY) and the per-attempt bound (engineTimeout, normally
// HTML_ENGINE_TIMEOUT), mirroring DocumentUniqueTTL's derivation exactly so
// the lock can never silently drift under asynq's true worst-case retry
// lifetime if either env var changes later.
//
// Worst-case formula: (maxRetry+1) * engineTimeout + htmlBackoffSum(maxRetry) + margin.
// Same "(maxRetry+1) executions, not maxRetry" correction as
// ImageUniqueTTL/DocumentUniqueTTL applies here. REUSES the shared
// uniqueTTLSafetyMargin const verbatim — no html-specific margin constant.
func HTMLUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + htmlBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// audioBackoffSum sums AudioRetryDelay(i) for i in [0, maxRetry) — the total
// backoff time asynq spends waiting before each of maxRetry retries, clamped
// to the last schedule entry once i reaches len(audioRetrySchedule). Mirrors
// documentBackoffSum/htmlBackoffSum; safe to call AudioRetryDelay directly
// since it has no jitter (same reasoning as documentBackoffSum's doc
// comment).
func audioBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += AudioRetryDelay(i, nil, nil)
	}
	return sum
}

// AudioUniqueTTL derives the per-job asynq.Unique lock TTL for audio
// transcription tasks from the actual retry budget (maxRetry, normally
// AUDIO_MAX_RETRY) and the per-attempt bound (engineTimeout, normally
// AUDIO_ENGINE_TIMEOUT), mirroring DocumentUniqueTTL's/HTMLUniqueTTL's
// derivation exactly so the lock can never silently drift under asynq's true
// worst-case retry lifetime if either env var changes later.
//
// Per the AudioUniqueTTL binding decision (STATE.md): this is DERIVED FRESH
// from AUDIO_MAX_RETRY/AUDIO_ENGINE_TIMEOUT — it deliberately never reuses
// ImageUniqueTTL/DocumentUniqueTTL/HTMLUniqueTTL's values, since audio's
// per-attempt cost (ffmpeg normalize + whisper-cli transcribe, bounded by a
// distinct AUDIO_ENGINE_TIMEOUT) is not comparable to any other engine
// class's.
//
// Worst-case formula: (maxRetry+1) * engineTimeout + audioBackoffSum(maxRetry) + margin.
// Same "(maxRetry+1) executions, not maxRetry" correction as
// ImageUniqueTTL/DocumentUniqueTTL/HTMLUniqueTTL applies here. REUSES the
// shared uniqueTTLSafetyMargin const verbatim — no audio-specific margin
// constant.
//
// The TTL is DERIVED rather than a hardcoded constant deliberately: if
// AUDIO_MAX_RETRY or AUDIO_ENGINE_TIMEOUT are later changed via env, the
// margin scales with them automatically and cannot fall behind the real
// retry lifetime.
func AudioUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + audioBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// avBackoffSum sums AVRetryDelay(i) for i in [0, maxRetry) -- the total
// backoff time asynq spends waiting before each of maxRetry retries, clamped
// to the last schedule entry once i reaches len(avRetrySchedule). Mirrors
// audioBackoffSum; safe to call AVRetryDelay directly since it has no jitter
// (same reasoning as documentBackoffSum's doc comment).
func avBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += AVRetryDelay(i, nil, nil)
	}
	return sum
}

// AVUniqueTTL derives the per-job asynq.Unique lock TTL for av conversion
// tasks from the actual retry budget (maxRetry, normally AV_MAX_RETRY) and
// the per-attempt bound (engineTimeout, normally AV_ENGINE_TIMEOUT),
// mirroring AudioUniqueTTL's derivation exactly so the lock can never
// silently drift under asynq's true worst-case retry lifetime if either env
// var changes later.
//
// SEQUENCING NOTE (D-03/CONTEXT.md): AV_ENGINE_TIMEOUT is only [ASSUMED]
// (provisional) in this phase -- Phase 36 measures it for real from an RTF
// matrix and supersedes the input value entirely. The formula below is
// unaffected by that; only the engineTimeout argument changes.
//
// Worst-case formula: (maxRetry+1) * engineTimeout + avBackoffSum(maxRetry) + margin.
// Same "(maxRetry+1) executions, not maxRetry" correction as
// ImageUniqueTTL/DocumentUniqueTTL/HTMLUniqueTTL/AudioUniqueTTL applies here
// (asynq's archive condition msg.Retried >= msg.Retry is checked AFTER each
// failed attempt). REUSES the shared uniqueTTLSafetyMargin const verbatim --
// no av-specific margin constant (D-03, locked).
func AVUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + avBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// ocrBackoffSum sums OCRRetryDelay(i) for i in [0, maxRetry) -- the total
// backoff time asynq spends waiting before each of maxRetry retries, clamped
// to the last schedule entry once i reaches len(ocrRetrySchedule). Mirrors
// avBackoffSum; safe to call OCRRetryDelay directly since it has no jitter
// (same reasoning as documentBackoffSum's doc comment).
func ocrBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += OCRRetryDelay(i, nil, nil)
	}
	return sum
}

// OCRUniqueTTL derives the per-job asynq.Unique lock TTL for OCR conversion
// tasks from the actual retry budget (maxRetry, normally OCR_MAX_RETRY) and
// the per-attempt bound (engineTimeout, normally OCR_ENGINE_TIMEOUT),
// mirroring AVUniqueTTL's derivation shape exactly so the lock can never
// silently drift under asynq's true worst-case retry lifetime if either env
// var changes later.
//
// DERIVED FRESH (D-04/IN-02): this is computed from OCR's OWN
// OCR_MAX_RETRY/OCR_ENGINE_TIMEOUT budget -- it deliberately never reuses
// AVUniqueTTL's/AudioUniqueTTL's/any other engine's values, since OCR's
// per-attempt cost (tesseract/pdftoppm, bounded by a distinct
// OCR_ENGINE_TIMEOUT) is not comparable to any other engine class's. A
// mismatch here would silently duplicate processing (T-44-05): if
// OCRUniqueTTL under-estimated the true worst-case retry lifetime, a
// reconciler re-enqueue could race a still-legitimately-retrying task.
//
// SEQUENCING NOTE (mirrors AVUniqueTTL's D-03 precedent): OCR_ENGINE_TIMEOUT
// is only a provisional code default in this phase -- Phase 45 measures it
// for real and supersedes the input value entirely. The formula below is
// unaffected by that; only the engineTimeout argument changes.
//
// Worst-case formula: (maxRetry+1) * engineTimeout + ocrBackoffSum(maxRetry) + margin.
// Same "(maxRetry+1) executions, not maxRetry" correction as
// ImageUniqueTTL/DocumentUniqueTTL/HTMLUniqueTTL/AudioUniqueTTL/AVUniqueTTL
// applies here (asynq's archive condition msg.Retried >= msg.Retry is
// checked AFTER each failed attempt). REUSES the shared uniqueTTLSafetyMargin
// const verbatim -- no ocr-specific margin constant.
func OCRUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + ocrBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// pandocBackoffSum sums PandocRetryDelay(i) for i in [0, maxRetry) -- the
// total backoff time asynq spends waiting before each of maxRetry retries,
// clamped to the last schedule entry once i reaches
// len(pandocRetrySchedule). Mirrors ocrBackoffSum; safe to call
// PandocRetryDelay directly since it has no jitter (same reasoning as
// documentBackoffSum's doc comment).
func pandocBackoffSum(maxRetry int) time.Duration {
	var sum time.Duration
	for i := 0; i < maxRetry; i++ {
		sum += PandocRetryDelay(i, nil, nil)
	}
	return sum
}

// PandocUniqueTTL derives the per-job asynq.Unique lock TTL for pandoc
// conversion tasks from the actual retry budget (maxRetry, normally
// PANDOC_MAX_RETRY) and the per-attempt bound (engineTimeout, normally
// PANDOC_ENGINE_TIMEOUT), mirroring OCRUniqueTTL's derivation shape exactly
// so the lock can never silently drift under asynq's true worst-case retry
// lifetime if either env var changes later.
//
// DERIVED FRESH (D-03, 48-CONTEXT.md): this is computed from pandoc's OWN
// PANDOC_MAX_RETRY/PANDOC_ENGINE_TIMEOUT budget -- it deliberately never
// reuses OCRUniqueTTL's/AVUniqueTTL's/AudioUniqueTTL's/any other engine's
// values, since pandoc's per-attempt cost (a deterministic, CPU-bound
// markup-to-markup transform, bounded by a distinct PANDOC_ENGINE_TIMEOUT)
// is not comparable to any other engine class's. A mismatch here would
// silently duplicate processing (mirrors T-44-05/T-48-02): if
// PandocUniqueTTL under-estimated the true worst-case retry lifetime, a
// reconciler re-enqueue could race a still-legitimately-retrying task.
//
// SEQUENCING NOTE (mirrors OCRUniqueTTL's/AVUniqueTTL's precedent):
// PANDOC_ENGINE_TIMEOUT is only a CLEARLY-MARKED PLACEHOLDER code default in
// this phase (Phase 48) -- Phase 49 measures it for real and supersedes the
// input value entirely. The formula below is unaffected by that; only the
// engineTimeout argument changes.
//
// Worst-case formula: (maxRetry+1) * engineTimeout + pandocBackoffSum(maxRetry) + margin.
// Same "(maxRetry+1) executions, not maxRetry" correction as
// ImageUniqueTTL/DocumentUniqueTTL/HTMLUniqueTTL/AudioUniqueTTL/AVUniqueTTL/
// OCRUniqueTTL applies here (asynq's archive condition msg.Retried >=
// msg.Retry is checked AFTER each failed attempt). REUSES the shared
// uniqueTTLSafetyMargin const verbatim -- no pandoc-specific margin
// constant.
func PandocUniqueTTL(maxRetry int, engineTimeout time.Duration) time.Duration {
	return time.Duration(maxRetry+1)*engineTimeout + pandocBackoffSum(maxRetry) + uniqueTTLSafetyMargin
}

// AllConvertQueues returns the queue name for every engine class
// (image/document/html/audio/av/ocr/pandoc/probe), deliberately excluding
// QueueWebhook (webhook is not an engine class and is never KEDA-scaled
// per-conversion). This is the single source of truth for cmd/api/main.go's
// queue-depth collector arg list: metrics.NewQueueDepthCollector is
// VARIADIC, so a hand-maintained call-site list can silently omit a queue
// with no compile error, dropping the Prometheus series KEDA scales the
// worker on (D-06, RESEARCH.md Pitfall 2). See
// TestAllConvertQueuesCoversEveryEngine for the completeness guard against a
// future engine class being added here without also being added to
// convert.go's Engine* constants (or vice versa). Plan 04 rewires
// cmd/api/main.go to spread this helper.
func AllConvertQueues() []string {
	return []string{QueueImage, QueueDocument, QueueHTML, QueueAudio, QueueAV, QueueOCR, QueuePandoc, QueueProbe, QueueTectonic}
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
