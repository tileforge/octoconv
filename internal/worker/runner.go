package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/hibiken/asynq"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/logfmt"
	"github.com/apaderin/octoconv/internal/metrics"
	"github.com/apaderin/octoconv/internal/queue"
)

// EngineDescriptor carries every per-engine difference the seven
// Handle*Convert wrappers used to hardcode into seven near-identical
// 79-163-line functions (SEED-020, 56-CONTEXT.md D-09): the shared skeleton
// now lives ONCE in Runner.Handle, and each engine supplies its own
// differences as DATA, not a branch. Package-level `var`s per engine
// (ImageDescriptor, DocumentDescriptor, ... in descriptors.go) -- never a
// map[string]EngineDescriptor registry, matching the one-engine-per-binary
// deployment model every worker cmd already uses (D-09).
type EngineDescriptor struct {
	// Engine is convert.EngineX -- byte-identical to queue.QueueX by
	// construction (internal/queue/queue.go:38-45). finish() uses this
	// field (not job.Engine) for every metric label: the two are
	// byte-identical in production, but this Runner is structurally bound
	// to Engine at construction (NewRunner(h, d)), so metric labeling needs
	// no defensive check and no queue.QueueX literal of its own.
	Engine string

	// Type is queue.TypeXConvert, the asynq task type this engine's Runner
	// is registered against (mux.HandleFunc in each worker cmd's main.go).
	Type string

	// OptsFromMap re-parses the job's persisted Opts strictly, discarding
	// the parsed value and keeping only the error (D-10: garbage in
	// jobs.options is a terminal, not transient, failure -- a corrupt
	// column value can never fix itself on retry). nil for image only:
	// image is the one engine with no opts-guard branch
	// (TestPinnedImageHasNoOptsGuard).
	OptsFromMap func(map[string]any) (any, error)

	// IsTerminal classifies a process() error as terminal (no retry can
	// help) vs. transient (broad-retry default). Required for all seven
	// engines -- carried as a field VALUE with its body unchanged (the
	// pre-existing isTerminal/isDocumentTerminal/isHTMLTerminal/
	// isAudioTerminal/isAVTerminal/isOCRTerminal/isPandocTerminal
	// functions).
	//
	// CRITICAL (PITFALLS.md Pitfall 2): this is func(error) bool, the exact
	// signature of today's isXTerminal functions -- do NOT "simplify" this
	// into a TimeoutIsTerminal bool or any (engine, isTimeout) shape.
	// isAudioTerminal (worker.go) treats an ffmpeg-stage timeout as
	// terminal and a whisper-stage timeout as transient; isAVTerminal
	// (worker.go) has the OPPOSITE polarity -- a transcode-stage timeout is
	// transient but any audio-extract/thumbnail-stage failure is terminal.
	// A boolean cannot express either asymmetry; only a per-engine function
	// value can. TestPinnedAudioStageAsymmetry and TestPinnedAVStageAsymmetry
	// (pinning_test.go) are the acceptance gate for this constraint.
	IsTerminal func(error) bool

	// FailureCode maps a terminal error to the client-facing (code,
	// message) pair MarkFailed records. nil means the shared default pair
	// ("engine_error", "unsupported or corrupted input format") -- used by
	// image/document/html. audio/av/ocr/pandoc supply their own mapping
	// (audioFailureCode/avFailureCode/ocrFailureCode/pandocFailureCode,
	// descriptors.go/worker.go), each already unit-tested independently of
	// this refactor.
	FailureCode func(err error) (code, message string)

	// PreConvertGuard, when non-nil, splices additional validation
	// immediately before the engine's Convert call (installed onto
	// Handler.preConvertGuard by NewRunner). nil for six of the seven
	// engines; only audio uses this hook, and NewRunner installs it via a
	// dedicated engine-specific closure rather than populating this field
	// directly on AudioDescriptor (see NewRunner's doc comment for why).
	// Kept as a generic hook for any future engine with the same shape of
	// need, even though no descriptor populates it today.
	PreConvertGuard func(ctx context.Context, job *jobs.Job, inPath string, convertFn func() error) error

	// SideOutputs, when non-nil, is called after a successful convert and
	// AFTER the primary output has been uploaded and recorded, and returns
	// additional outputs to store beside it (Phase 60, SANI-05).
	//
	// nil for seven of the eight engines, which have nothing to report
	// alongside their result; only probe populates it, to carry the sanitize
	// report as the job's role=report output. The hook mirrors
	// PreConvertGuard's shape deliberately: an optional, descriptor-declared
	// splice into the shared process() path, so the generic pipeline gains a
	// capability without gaining a per-engine branch.
	//
	// Ordering is deliberate and is the reason this runs AFTER the primary
	// upload rather than before: a report describing an output that failed
	// to store would be a record of something the client cannot fetch.
	SideOutputs func(ctx context.Context, job *jobs.Job, outPath string) ([]SideOutput, error)
}

// SideOutput is one additional artifact an engine produces beside its primary
// result. The body is carried in memory rather than as a path because every
// current producer builds it in memory anyway, and a []byte cannot be a
// dangling reference to a temp directory that process() has already removed.
type SideOutput struct {
	// Role is the job_outputs.role value -- "report" for the sanitize
	// report. Constrained by the database CHECK, not by this struct.
	Role string
	// Filename is the object's name within the job's result prefix.
	Filename string
	// Format and ContentType describe the body.
	Format      string
	ContentType string
	// Body is the artifact itself.
	Body []byte
}

// Runner is the single descriptor-driven asynq task handler every one of
// the seven conversion engine classes now runs through (D-09): one Runner
// per worker binary, wrapping the SAME *Handler every engine already
// shared via process()/downloadTo/uploadFrom, plus one EngineDescriptor
// naming that engine's own classifier/opts-parser/failure-code mapping.
type Runner struct {
	h *Handler
	d EngineDescriptor
}

// NewRunner builds a Runner bound to h and d, installing d's pre-convert
// guard (if any) onto h.preConvertGuard so process()'s splice
// (worker.go) picks it up on every subsequent attempt -- mirrors the
// established post-construction-setter precedent already used by
// SetProgressThrottle/SetProgressWebhook: one Handler per binary, one
// Runner per binary, the verified deployment model. Every worker cmd calls
// this exactly once at startup, immediately before registering
// r.Handle on the asynq mux, so the install always happens-before any
// concurrent request processing (same happens-before boundary every other
// process-wide config write in this codebase uses).
//
// AudioDescriptor.PreConvertGuard is deliberately left nil in
// descriptors.go and special-cased here instead: EngineDescriptor is a
// package-level var built once at package-init time, before any *Handler
// (and therefore before h.audioMaxDuration, itself env-configured per
// binary via NewHandler's last parameter) exists, so a closure assigned at
// var-init time could never read a specific Handler's audioMaxDuration.
// NewRunner is the one place with both the descriptor AND a concrete
// *Handler in scope, so it is where the ceiling actually gets threaded
// into enforceAudioGuardBeforeConvert (unmodified, T-30-08/IN-02) via
// audioPreConvertGuard below. Chosen over adding a dedicated
// EngineDescriptor.AudioMaxDuration field, which would violate the "exactly
// these fields" instruction for a value only one of seven engines needs.
func NewRunner(h *Handler, d EngineDescriptor) *Runner {
	switch {
	case d.Engine == convert.EngineAudio:
		h.preConvertGuard = audioPreConvertGuard(h.audioMaxDuration)
	case d.PreConvertGuard != nil:
		// Generic hook for any future engine whose guard needs no
		// per-Handler runtime config -- unused today (audio is the only
		// PreConvertGuard consumer and takes the case above instead).
		h.preConvertGuard = d.PreConvertGuard
	}
	h.sideOutputs = d.SideOutputs
	return &Runner{h: h, d: d}
}

// audioPreConvertGuard builds a Handler.preConvertGuard-shaped closure
// bound to a specific duration ceiling, so NewRunner can thread
// h.audioMaxDuration (a per-Handler, env-configured value) into the
// unmodified enforceAudioGuardBeforeConvert without changing that
// function's own signature or body (T-30-08/IN-02's explicit constraint).
func audioPreConvertGuard(maxDuration time.Duration) func(ctx context.Context, job *jobs.Job, inPath string, convertFn func() error) error {
	return func(ctx context.Context, job *jobs.Job, inPath string, convertFn func() error) error {
		return enforceAudioGuardBeforeConvert(ctx, job.Engine, inPath, maxDuration, convertFn)
	}
}

// Handle runs one conversion attempt for r.d.Engine: parse payload -> load
// job -> (opts guard, if any) -> mark active -> run the engine-agnostic
// process() (via r.h.processFn) -> classify the outcome -> commit exactly
// once through r.finish. This reproduces the shared skeleton every one of
// the seven Handle*Convert wrappers duplicated, now written ONCE.
//
// A terminal error (bad input, unsupported pair, missing storage object,
// invalid persisted opts) marks the job failed immediately and skips
// asynq's retry (D-01/D-02); a transient error (network, S3/engine
// timeout, Postgres write blip) is returned unwrapped so the job stays
// active and asynq retries the same task (D-01/D-03/D-04) -- mirroring
// HandleWebhookDeliver's own classification pattern, which this refactor
// does not touch.
func (r *Runner) Handle(ctx context.Context, t *asynq.Task) error {
	payload, err := queue.ParseConvertPayload(t.Payload())
	if err != nil {
		// Unparseable payload: nothing we can retry into success.
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	jobID := payload.JobID

	job, err := r.h.repo.Get(ctx, jobID)
	if err != nil {
		// A TRANSIENT failure BEFORE the job is loaded, and until now it was
		// invisible: this returns to asynq without reaching the retry logger
		// further down, so a Postgres outage produced repeated attempts with
		// no correlatable record at all (codex, PR #23).
		//
		// Only job_id is known here -- correlation_id, engine and client_id
		// live in the row that could not be read -- and job_id is what the
		// admission record already carries, so the chain still joins.
		fields := []string{"job_id", jobID.String(), "reason", err.Error()}
		if n, ok := asynq.GetRetryCount(ctx); ok {
			fields = append(fields, "attempt", strconv.Itoa(n+1))
		}
		log.Println(logfmt.KV("job load failed", fields...))
		return fmt.Errorf("load job %s: %w", jobID, err)
	}

	if r.d.OptsFromMap != nil {
		// Strict re-parse of the persisted opts (D-10): garbage in
		// jobs.options is a terminal, not transient, failure -- a corrupt
		// column value can never fix itself on retry. This is a strictness
		// check only; the applicability/enum business rules already ran
		// once, at the API layer (single source of validation truth), and
		// are deliberately not duplicated here. Runs BEFORE MarkActive --
		// the job never leaves `queued`, which is why elapsed is 0
		// (recordUsage derives wall_seconds from jobs.started_at, still
		// NULL here, and falls back to 0, mirroring the reconciler
		// exhaustion path's own 0).
		//
		// ERRC-02 (this plan's one intentional behavior change): routing
		// this branch through r.finish means BOTH outcome counters now
		// move on a terminal invalid_options failure, closing the metrics
		// gap plan 56-01 deliberately left un-pinned
		// (TestPinnedOptsGuardPrecedesMarkActive makes no counter
		// assertion on this branch for exactly that reason).
		if _, optsErr := r.d.OptsFromMap(job.Opts); optsErr != nil {
			_ = r.finish(ctx, job, jobs.StatusFailed, CodeInvalidOptions, "stored conversion options are invalid", map[string]any{"opts_error": optsErr.Error()}, 0)
			return fmt.Errorf("%w: opts: %v", asynq.SkipRetry, optsErr)
		}
	}

	if err := r.h.repo.MarkActive(ctx, jobID); err != nil {
		// Already active/done/canceled -- let asynq drop it rather than
		// loop.
		return fmt.Errorf("%w: mark active: %v", asynq.SkipRetry, err)
	}

	start := time.Now()
	if err := r.runProcess(ctx, job); err != nil {
		if errors.Is(err, ErrEnginePanic) {
			// A panic is OURS, not the client's, and it must still leave the
			// job in a terminal state. Placed before the descriptor's own
			// classifier so no engine can accidentally call a panic transient
			// and retry it: the same input panics the same way every time.
			//
			// This exists because it happened, invisibly. pdfcpu panics when
			// it cannot write its config file, which it cannot as `nobody`,
			// and asynq's own recover() sent every sanitize job around the
			// retry loop and then left it ACTIVE -- a client watching a job
			// that never finishes, and no failure recorded anywhere. The
			// engine bug is fixed; the shape that hid it is fixed here.
			_ = r.finish(ctx, job, jobs.StatusFailed, CodeEngineError,
				"the conversion failed unexpectedly on this deployment",
				map[string]any{"engine_stderr": err.Error()}, time.Since(start))
			return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
		}
		if isCanceled(err) {
			// D-07: the attempt was killed by a job:cancel signal via the
			// registered CancelFunc (internal/cancel) -- the job is already
			// `canceled` in Postgres (API's guarded transition). Do NOT
			// MarkFailed (it would be rejected by the allow-list anyway,
			// D-05, and re-failing a canceled job is wrong) and record no
			// job-outcome metric (mirrors the transient branch's own
			// no-double-count comment below).
			// NO RECORD HERE, deliberately -- and it had one until codex
			// pointed out that it was wrong in both directions (PR #23).
			//
			// isCanceled only says the error wraps context.Canceled: an asynq
			// shutdown or any parent-context cancellation reaches it too, so
			// this would claim "job canceled" for a row that is still active.
			// And a job canceled while QUEUED never enters this branch at all.
			//
			// The record belongs where the guarded transition COMMITS, which
			// is handleCancelJob -- exactly the jobs that were canceled,
			// exactly once.
			return fmt.Errorf("%w: canceled", asynq.SkipRetry)
		}
		// A SkipRetry from the engine path means "no attempt can succeed", and a
		// job that will never be attempted again must not be left ACTIVE. Before
		// this, an error wrapping the sentinel fell to the transient branch --
		// no MarkFailed, no metric -- while asynq read the sentinel and archived
		// the task, so the row sat active until the reconciler noticed. That is
		// the same shape as the recovered panic above, from the other side, and
		// it is worth a branch of its own rather than an assumption that every
		// engine's IsTerminal happens to recognise the sentinel (PR #20 review).
		if errors.Is(err, asynq.SkipRetry) || r.d.IsTerminal(err) {
			// Sanitized message only in error_message (exposed via GET
			// /jobs/{id} and webhook payloads, T-03-03); the raw stderr
			// (which contains local temp paths) is kept in
			// job_events.detail for internal diagnostics only.
			code, message := CodeEngineError, "unsupported or corrupted input format"
			if r.d.FailureCode != nil {
				code, message = r.d.FailureCode(err)
			}
			_ = r.finish(ctx, job, jobs.StatusFailed, code, message, map[string]any{"engine_stderr": err.Error()}, time.Since(start))
			return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
		}
		// Transient: do NOT mark failed -- the job stays active so asynq's
		// own retry/backoff (per-engine RetryDelay/MAX_RETRY) applies. Not
		// a terminal outcome, so it is NOT recorded in the job-outcome
		// metric (Pitfall 6 -- one asynq retry must not double-count).
		//
		// It IS logged, and with the attempt number, for the reason the metric
		// is not: without it, three attempts at one job read as three jobs
		// that each vanished, and the retry -- the very thing that makes this
		// branch different from the terminal one -- is invisible.
		//
		// The reason carries the engine's own error text, which is the engine
		// reacting to a document the client wrote: jobLine escapes it.
		fields := []string{
			"job_id", job.ID.String(),
			"correlation_id", job.CorrelationID,
			"client_id", job.ClientID.String(),
			"engine", job.Engine,
		}
		// GetRetryCount reports FALSE outside an asynq handler -- which is
		// every unit test that drives Handle directly. Rendering "attempt=1"
		// there would be a claim the context cannot support, so the field is
		// omitted rather than guessed.
		//
		// 1-based when it is present: asynq counts RETRIES, and "attempt=0"
		// for the first try reads as a bug in the logging rather than as the
		// first try.
		if n, ok := asynq.GetRetryCount(ctx); ok {
			fields = append(fields, "attempt", strconv.Itoa(n+1))
		}
		fields = append(fields,
			"duration_ms", logfmt.MS(time.Since(start).Milliseconds()),
			"reason", err.Error(),
		)
		log.Println(logfmt.KV("job retrying", fields...))
		return err
	}

	_ = r.finish(ctx, job, jobs.StatusDone, "", "", nil, time.Since(start))
	return nil
}

// ErrEnginePanic is a panic that escaped an engine's process function. Not a
// client-visible category (its message says "on this deployment", because that
// is whose fault it is), but a distinct sentinel so the branch handling it can
// never be confused with an engine's own verdict.
var ErrEnginePanic = errors.New("worker: the engine panicked")

// runProcess calls the engine's process function and turns a PANIC into an
// ordinary terminal error carrying the stack.
//
// asynq recovers panics itself, which sounds like enough and is not: its
// recover happens ABOVE this frame, so r.finish never runs, the job row stays
// `active`, no outcome metric moves, and the client is left watching a job that
// never ends. Recovering HERE keeps the single terminal-commit point intact and
// makes the failure honest.
//
// The stack goes into job_events.detail rather than a log line: this package
// does not log (only cmd/*/main.go does), and a panic with no trace anywhere
// would trade one invisible failure for another. It is bounded for the same
// reason every other stored diagnostic in this codebase is.
func (r *Runner) runProcess(ctx context.Context, job *jobs.Job) (err error) {
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		const maxStack = 4096
		stack := debug.Stack()
		if len(stack) > maxStack {
			stack = stack[:maxStack]
		}
		err = fmt.Errorf("%w: %v\n%s", ErrEnginePanic, rec, stack)
	}()
	return r.h.processFn(ctx, job)
}

// finish is the SOLE terminal-outcome commit point for every one of the
// seven engines -- the ONLY place in the conversion path calling
// metrics.RecordJobOutcome, metrics.RecordClientJobOutcome and
// EnqueueWebhookDeliver (TestSingleTerminalEmissionCallSite,
// runner_test.go, asserts this structurally). Every emission is textually
// AFTER the MarkFailed assignment and inside or after its ferr check --
// that single-site structure is what makes "fire before commit" impossible
// (T-56-12), and plan 56-05's inversion run is the proof.
func (r *Runner) finish(ctx context.Context, job *jobs.Job, status, code, message string, detail map[string]any, elapsed time.Duration) error {
	// ONE log record per terminal outcome, emitted from the one place every
	// terminal outcome already flows through. Putting it at the four call
	// sites instead would mean four chances for a later branch to be added
	// without one -- and a job that ends with no line at all is exactly the
	// gap this is closing.
	//
	// job_id is the join: internal/api logs it once at admission alongside the
	// HTTP request id, and from there this is the same job across processes.
	var ferr error
	if status == jobs.StatusFailed {
		ferr = r.h.repo.MarkFailed(ctx, job.ID, code, message, detail, elapsed)
	}

	// AFTER the commit, and the outcome of the commit decides what is said.
	//
	// Logging first claimed a terminal state that MarkFailed might then fail
	// to reach -- a Postgres error, or a concurrent transition that already
	// took the job elsewhere. Everything below this treats such a job as NOT
	// having reached the status, and terminal callers discard finish's error
	// and return SkipRetry, so the log would have been the only record and it
	// would have been wrong (codex, PR #23).
	//
	// A failed commit is NOT silent either: a job that could not be marked
	// failed is exactly what an operator needs to see, and it is the one case
	// where the row and the log genuinely disagree. It gets its own event
	// rather than the terminal one.
	event := "job " + status
	fields := []string{
		"job_id", job.ID.String(),
		"correlation_id", job.CorrelationID,
		"client_id", job.ClientID.String(),
		"engine", job.Engine,
		"target", job.TargetFormat,
		"code", code,
		"duration_ms", logfmt.MS(elapsed.Milliseconds()),
	}
	if ferr != nil {
		event = "job finish uncommitted"
		fields = append(fields, "intended_status", status, "commit_error", ferr.Error())
	}
	log.Println(logfmt.KV(event, fields...))
	// jobs.StatusDone: ferr stays nil -- process() (or a test's processFn
	// stub standing in for it) already committed MarkDone inside its own
	// transaction (worker.go's process(), the final line) before Handle
	// ever calls finish. Guarding the done branch on this same nil is what
	// keeps every terminal outcome (failed or done) flowing through the
	// identical ferr==nil gate below.

	// Deliberately NOT guarded on ferr, preserving the pre-existing
	// asymmetry documented at every terminal branch this Runner replaces
	// and pinned by TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook:
	// a lost Prometheus increment is tolerable and never retried. This is
	// NOT the same durability contract as MarkFailed's client_usage_daily
	// rollup (D-05), which is transactional and exactly-once -- the two
	// calls sit on adjacent lines here but must not be read as equivalent.
	// This is NOT the RUNR-02 defect (internal/reconciler/reconciler.go's
	// entirely unguarded RecordReconcilerAction("exhausted")), which plan
	// 56-05 fixes separately.
	//
	// r.d.Engine, not job.Engine: both are byte-identical in production
	// (a job's persisted engine column always matches the descriptor its
	// own asynq task type routed to), but r.d.Engine is the value this
	// Runner is STRUCTURALLY bound to at construction (NewRunner(h, d)) --
	// it needs no defensive nil/empty check and, unlike job.Engine, does
	// not depend on every test fixture populating Job.Engine (frozen
	// pinning_test.go's newPinJob deliberately does not, D-10 step 2).
	metrics.RecordJobOutcome(r.d.Engine, status, elapsed)

	// Guarded on ferr: if MarkFailed did not commit (Postgres blip, or a
	// concurrent transition won the guarded allow-list), the job is NOT
	// actually in `status` yet. A retry or the reconciler will reach a
	// terminal state later and increment then -- counting here too would
	// double-count this client, or attribute an outcome that never
	// happened.
	if ferr == nil {
		metrics.RecordClientJobOutcome(job.ClientID.String(), r.d.Engine, status)
	}

	// Postgres-first: enqueue the webhook ONLY once the transition is
	// actually committed. If MarkFailed itself failed (Postgres blip), the
	// job is still active -- a webhook now would re-read the row and
	// deliver a status the contract never defines (WR-04); the reconciler
	// will requeue the still-active job instead. A failed enqueue after a
	// successful commit must not fail the conversion -- best-effort only.
	if ferr == nil && job.CallbackURL != "" {
		_ = r.h.enqueuer.EnqueueWebhookDeliver(ctx, job.ID)
	}
	return ferr
}
