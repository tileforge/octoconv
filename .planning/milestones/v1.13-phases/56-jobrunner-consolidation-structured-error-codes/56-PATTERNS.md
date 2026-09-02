# Phase 56: JobRunner Consolidation & Structured Error Codes - Pattern Map

**Mapped:** 2026-08-08
**Files analyzed:** 8 (1 new package + 7 modified/split files)
**Analogs found:** 8 / 8

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/worker/runner.go` (new) | worker/dispatcher | event-driven | `internal/worker/worker.go`'s seven `Handle*Convert` methods (esp. `HandleImageConvert:704-782`, `HandleAudioConvert:1033-1142`) + `internal/convert/convert.go`'s `Converter`/`Registry` (interface-driven dispatch precedent) | exact (same package, same orchestration shape) |
| `internal/worker/worker.go` (modified — descriptors extracted, `Handle*Convert` removed) | worker/dispatcher | event-driven | itself (pre-refactor) — `process()` (1847-1942) stays untouched; `isXTerminal`/`*FailureCode` functions relocate | exact |
| `internal/errcode/errcode.go` (new package) | model/constants | N/A (pure data) | `internal/jobs/jobs.go:13-20` (`StatusX` untyped string consts) + `internal/jobs/repo.go:15-16` (`var ErrNotFound = errors.New(...)`) for the *shape* of a small constants file; `internal/api/handlers.go:1007-1010` `writeQuotaError` for the *envelope* it must match | role-match (no prior "error code constants" package exists; closest precedent is the enum-like string-const idiom already used for job statuses) |
| `internal/api/handlers.go` (`writeError` signature change, ~60 call sites) | controller | request-response | `writeQuotaError` (`handlers.go:997-1010`) — the ONE existing `{error,code}` 4xx/429 writer in this codebase | exact (explicit precedent, cited in its own doc comment as "the general sweep is SEED-020") |
| `internal/api/jobs_list_handler.go` (4 `writeError` call sites: `:38`, `:46`, `:68`, `:76`) | controller | request-response | same file's own existing `writeError` calls + `handlers.go`'s `writeQuotaError` | exact |
| `internal/reconciler/reconciler.go` (guard fix at `:311-312`) | service/sweeper | event-driven (poll-and-recover) | same file's own already-correctly-guarded pattern at `:313-329` (`ferr == nil` before `RecordClientJobOutcome`) — the fix is to make `RecordReconcilerAction("exhausted")` match the sibling guard three lines below it | exact (self-referential fix — the correct pattern already exists two lines away) |
| `cmd/worker/main.go`, `cmd/document-worker/main.go`, `cmd/chromium-worker/main.go`, `cmd/audio-worker/main.go`, `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go` (one `mux.HandleFunc` line each) | entry point / wiring | event-driven | `cmd/worker/main.go` (simplest, no descriptor extras) and `cmd/av-worker/main.go` (has `SetProgressThrottle`/`SetProgressWebhook` post-construction setters — the precedent for "additive wiring without touching `NewHandler`'s signature") | exact |

## Pattern Assignments

### `internal/worker/runner.go` (new — worker/dispatcher, event-driven)

**Analog:** `internal/worker/worker.go` — `HandleImageConvert` (lines 704-782, simplest of the seven — no opts guard, no pre-convert splice) and `HandleAudioConvert` (lines 1033-1142, richest — has the opts guard, the stage-aware terminal classifier via `isAudioTerminal`, AND a `PreConvertGuard`-shaped splice inside `process()`). Also `internal/convert/convert.go`'s `Converter` interface (76-85) / `Registry` (111-148) as the structural precedent for "an interface/struct captures the varying part, one generic caller drives it."

**Imports pattern** (worker.go:1-26 — new `runner.go` should reuse the same import block since it stays in `package worker`):
```go
package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/metrics"
	"github.com/apaderin/octoconv/internal/queue"
)
```

**Core skeleton to generalize** — every `Handle*Convert` shares this exact shape (verbatim from `HandleImageConvert`, worker.go:704-782; only the four data points in ARCHITECTURE.md §A's table vary):
```go
func (h *Handler) HandleImageConvert(ctx context.Context, t *asynq.Task) error {
	payload, err := queue.ParseConvertPayload(t.Payload())
	if err != nil {
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	jobID := payload.JobID

	job, err := h.repo.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", jobID, err)
	}

	// [VARYING: OptsFromMap guard — nil for image, present for the other six.
	// See document handler below for the guard's full shape.]

	if err := h.repo.MarkActive(ctx, jobID); err != nil {
		return fmt.Errorf("%w: mark active: %v", asynq.SkipRetry, err)
	}

	start := time.Now()
	if err := h.process(ctx, job); err != nil {
		if isCanceled(err) {
			return fmt.Errorf("%w: canceled", asynq.SkipRetry)
		}
		if isTerminal(err) { // [VARYING: IsTerminal]
			// [VARYING: FailureCode — image/document/html always ("engine_error", ...);
			// audio/av/ocr/pandoc branch on errors.Is(...) first]
			ferr := h.repo.MarkFailed(ctx, jobID, "engine_error", "unsupported or corrupted input format", map[string]any{"engine_stderr": err.Error()}, time.Since(start))
			metrics.RecordJobOutcome(queue.QueueImage, jobs.StatusFailed, time.Since(start)) // [VARYING: label — job.Engine is byte-identical, per ARCHITECTURE.md §A]
			if ferr == nil {
				metrics.RecordClientJobOutcome(job.ClientID.String(), queue.QueueImage, jobs.StatusFailed)
			}
			if ferr == nil && job.CallbackURL != "" {
				_ = h.enqueuer.EnqueueWebhookDeliver(ctx, jobID)
			}
			return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
		}
		return err // transient: no metric, let asynq retry
	}
	metrics.RecordJobOutcome(queue.QueueImage, jobs.StatusDone, time.Since(start))
	metrics.RecordClientJobOutcome(job.ClientID.String(), queue.QueueImage, jobs.StatusDone)
	if job.CallbackURL != "" {
		_ = h.enqueuer.EnqueueWebhookDeliver(ctx, jobID)
	}
	return nil
}
```

**Opts-guard variant to fold in as `EngineDescriptor.OptsFromMap`** (verbatim from `HandleDocumentConvert`, worker.go:811-843 — six of seven engines have this, image does not):
```go
if _, err := convert.DocOptsFromMap(job.Opts); err != nil {
	ferr := h.repo.MarkFailed(ctx, jobID, "invalid_options", "stored conversion options are invalid", map[string]any{"opts_error": err.Error()}, 0)
	if ferr == nil && job.CallbackURL != "" {
		_ = h.enqueuer.EnqueueWebhookDeliver(ctx, jobID)
	}
	return fmt.Errorf("%w: opts: %v", asynq.SkipRetry, err)
}
```
Note this guard runs BEFORE `MarkActive`, with `wall_seconds=0` in `MarkFailed` — preserve this exact ordering/argument in the runner (worker.go:824-833's comment documents why: the job never left `queued`).

**FailureCode multi-way mapper pattern** — the runner's `EngineDescriptor.FailureCode func(error) (code, message string)` field should copy these three existing pure functions UNCHANGED (only relocated), in order of complexity:
- Two-way inline (audio, worker.go:1101-1106):
```go
if errors.Is(err, convert.ErrAudioDurationExceeded) {
	ferr = h.repo.MarkFailed(ctx, jobID, "duration_exceeded", "declared audio duration exceeds the configured maximum", ...)
} else {
	ferr = h.repo.MarkFailed(ctx, jobID, "engine_error", "unsupported or corrupted input format", ...)
}
```
- Four-way named function (`avFailureCode`, worker.go:1163-1176):
```go
func avFailureCode(err error) (code, message string) {
	switch {
	case errors.Is(err, convert.ErrAVTimecodeOutOfRange):
		return "timecode_out_of_range", "requested thumbnail timecode exceeds source duration"
	case errors.Is(err, convert.ErrAudioDurationExceeded):
		return "duration_exceeded", "declared video duration exceeds the configured maximum"
	case errors.Is(err, convert.ErrAVResolutionExceeded):
		return "resolution_exceeded", "declared video resolution exceeds the configured maximum"
	case errors.Is(err, convert.ErrAVNoVideoStream):
		return "no_video_stream", "no video stream found in source"
	default:
		return "engine_error", "unsupported or corrupted input format"
	}
}
```
- Six-way with substring fallback (`ocrFailureCode`, worker.go:482-501) — mix of `errors.Is` sentinel checks and `strings.Contains` on the lowercased error string; same pattern, copy verbatim.

**PreConvertGuard splice** (audio-only today, worker.go:1915-1922, spliced inside `process()` — NOT inside `Handle*Convert`) — the `EngineDescriptor.PreConvertGuard` field wraps this exact call shape:
```go
if err := enforceAudioGuardBeforeConvert(attemptCtx, job.Engine, inPath, h.audioMaxDuration, func() error {
	if err := runConvertWithOptionalProgress(attemptCtx, conv, inPath, outPath, job.Opts, h.repo, job.ID, h.progressMinInterval, h.progressMinDeltaPct, job.ProgressWebhook, job.CallbackURL, h.progressWebhookDelivr, h.progressWebhookSecret, h.progressWebhookTimeout); err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	return nil
}); err != nil {
	return err
}
```
`process()` itself (worker.go:1847-1942) is NOT touched by this phase except to make this one splice point descriptor-driven (nil for six engines, the audio closure for one) — everything else in `process()` (attempt-context timeout, cancel registration, download/upload/AddOutput/MarkDone) stays exactly as-is per PROJECT.md's invariant.

**Post-commit-hook single-call-site pattern (the SC4 "structurally impossible to fire before commit" requirement)** — every existing handler already has this shape at both its terminal-failure and success tails (worker.go:739-765, :774-781); the runner's `Handle` method must preserve it as ONE guarded block, not seven copies:
```go
ferr := h.repo.MarkFailed(ctx, jobID, code, message, detail, time.Since(start))
metrics.RecordJobOutcome(job.Engine, jobs.StatusFailed, time.Since(start))
if ferr == nil {
	metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
}
if ferr == nil && job.CallbackURL != "" {
	_ = h.enqueuer.EnqueueWebhookDeliver(ctx, jobID)
}
```

**Error handling pattern:** `fmt.Errorf("%w: %v", asynq.SkipRetry, err)` for every terminal/unrecoverable branch (unparseable payload, already-active, canceled, terminal engine error); bare `return err` (unwrapped) for transient failures so asynq's own retry/backoff applies. Never `panic`; never leak raw `err.Error()` to the client — it goes only into `job_events.detail` (`map[string]any{"engine_stderr": err.Error()}`), the sanitized fixed string goes into `error_message`.

---

### `internal/errcode/errcode.go` (new package — model/constants, N/A data flow)

**Analog:** `internal/jobs/jobs.go:12-20` (untyped string-const enum idiom used for `StatusX`) for the const-block shape; `internal/api/handlers.go:997-1010` (`writeQuotaError`) for the JSON envelope the constants feed into; `internal/ratelimit/ratelimit.go:117` for the second existing `{error,code}` precedent (`"code": "rate_limited"`).

**Package doc comment convention** (mirrors `internal/jobs/jobs.go:1-3`, `internal/worker/worker.go:1`):
```go
// Package errcode defines the sync (HTTP 4xx/429) machine-readable error-code
// vocabulary this service returns in every {"error": msg, "code": ...} body.
// Async job-failure codes (jobs.error_code) are a SEPARATE, already-structured
// vocabulary that lives beside EngineDescriptor in internal/worker — this
// package intentionally does not re-declare them (D-04/D-06, 56-CONTEXT.md).
package errcode
```

**Const-block shape** (mirrors `internal/jobs/jobs.go:13-20`'s comment-tied-to-DB-constraint convention, applied to D-02's ~10-14 code list):
```go
// Sync error codes, one per class of 4xx/429 rejection (D-02). Granularity is
// by failure CLASS, not by call site — do not add a new constant per
// writeError message string; only per distinguishable client-actionable class.
const (
	InvalidRequest        = "invalid_request"
	UnsupportedConversion  = "unsupported_conversion"
	InvalidOptions         = "invalid_options" // same code name as the async MarkFailed("invalid_options", ...) — D-03, one code space
	OptsNotApplicable      = "opts_not_applicable"
	MissingFile            = "missing_file"
	UnknownPreset          = "unknown_preset"
	PayloadTooLarge        = "payload_too_large"
	NotFound               = "not_found"
	InvalidCursor          = "invalid_cursor"
	QuotaExceeded          = "quota_exceeded" // already exists as a literal in writeQuotaError; migrate to this constant, do not rename the string value
	RateLimited            = "rate_limited"   // already exists as a literal in internal/ratelimit; migrate to this constant, do not rename the string value
	InternalError          = "internal_error"
)
```

**Envelope pattern to generalize** (verbatim from `writeQuotaError`, handlers.go:1007-1010 — the exact shape `writeError` must produce once it gains a code parameter):
```go
func writeQuotaError(w http.ResponseWriter, msg string, retryAfterSeconds int) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": msg, "code": "quota_exceeded"})
}
```
D-01 requires the flat `{"error": msg, "code": "..."}` shape (never nested `{error:{code,...}}`) because `frontend/src/api.ts:54` reads `body.error` as a string.

---

### `internal/api/handlers.go` (controller, request-response — ~60 `writeError` call sites)

**Analog:** the file's own existing `writeError`/`writeQuotaError` (handlers.go:1050-1051, 997-1010).

**Current signature to widen:**
```go
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```
**Target shape** (add a `code string` parameter, keep `writeJSON` untouched — mirrors `writeQuotaError`'s existing use of `writeJSON` underneath):
```go
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
```
Every one of the ~60 call sites (handlers.go:122,125,140,146,152,158,169,195,198,215,234,242,306,311,316,380,388,399,416,457,465,477,519,530,539,544,552,565,570,589,596,601,609,621,626,634,647,652,660,667,672,680,685,697,713,716,720,762,790,794,815,821,825,835,860,866,910,916,920,928,953) needs its call updated to pass the appropriate `errcode` constant as the new second-to-last argument, per D-02's taxonomy (e.g. "missing target format" → `errcode.InvalidRequest`; "missing file" → `errcode.MissingFile`; "file exceeds size limit" → `errcode.PayloadTooLarge`; "invalid preset name"/unknown-preset sites → `errcode.UnknownPreset`; "opts not applicable to this conversion" → `errcode.OptsNotApplicable`; "invalid opts" → `errcode.InvalidOptions`; "job not found" → `errcode.NotFound`; every `http.StatusInternalServerError` site → `errcode.InternalError`).

**writeJSON stays untouched** (handlers.go:968-975):
```go
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // presigned URLs contain & that would become &amp;
	_ = enc.Encode(v)
}
```

**writeQuotaError migrates to reuse the constant, not the literal** (handlers.go:1007-1010) — after this phase, `"quota_exceeded"` should be `errcode.QuotaExceeded`, keeping the exact same wire value.

---

### `internal/api/jobs_list_handler.go` (controller, request-response — 4 call sites)

**Analog:** same file, self-consistent already; also `handlers.go`'s widened `writeError`.

**Call sites to update** (verbatim locations):
```go
// :38 — invalid status filter
writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid status filter")
// :46 — invalid engine filter
writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid engine filter")
// :68 — invalid cursor
writeError(w, http.StatusBadRequest, errcode.InvalidCursor, "invalid cursor")
// :76 — failed to list jobs
writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to list jobs")
```
Fail-closed-before-DB-work ordering (status/engine/cursor validated before any `s.repo.List` call) is an existing, load-bearing pattern (jobs_list_handler.go:24-28's own doc comment) — do not reorder while adding codes.

---

### `internal/reconciler/reconciler.go` (service/sweeper, event-driven — RUNR-02 fix)

**Analog:** the file's own adjacent, already-correct pattern three lines below the bug.

**Current (buggy) code** (reconciler.go:311-312):
```go
ferr := s.store.MarkFailed(ctx, j.ID, "reconciler_exhausted", "recovery attempts exhausted", map[string]any{"action": "reconciler_exhausted"}, 0)
metrics.RecordReconcilerAction("exhausted")  // unconditional — not guarded on ferr
if job != nil && ferr == nil {
	metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
}
```

**Correct pattern to copy** (the `RecordClientJobOutcome` call two lines below, already correctly guarded — reconciler.go:313-325's own comment explains why):
```go
if job != nil && ferr == nil {
	metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
}
```

**Fix — move `RecordReconcilerAction("exhausted")` inside the same `ferr == nil` guard:**
```go
ferr := s.store.MarkFailed(ctx, j.ID, "reconciler_exhausted", "recovery attempts exhausted", map[string]any{"action": "reconciler_exhausted"}, 0)
if ferr == nil {
	metrics.RecordReconcilerAction("exhausted")
}
if job != nil && ferr == nil {
	metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
}
```
This is the exact "same defect class, same fix shape" instance ARCHITECTURE.md §A and PITFALLS.md Pitfall 4's sibling call out — one guarded `if`, no duplicated logic. The existing fail-closed `default:` branch at reconciler.go:357-368 (`metrics.RecordReconcilerAction("unroutable_engine")`, unguarded because there is no `MarkFailed` call on that path at all) is NOT a bug and must not be touched — only the `:311-312` pair is guarded incorrectly.

---

### `cmd/*-worker/main.go` (7 files — entry point/wiring, event-driven)

**Analog:** `cmd/worker/main.go` (simplest — no descriptor-specific config) and `cmd/av-worker/main.go` (has post-construction setters, the established "additive wiring, don't widen `NewHandler`" precedent per ARCHITECTURE.md §A).

**Current registration to replace** (`cmd/worker/main.go`):
```go
mux := asynq.NewServeMux()
mux.HandleFunc(queue.TypeImageConvert, h.HandleImageConvert)
```

**Target shape per D-09** (package-level `var` descriptors in `internal/worker`, one import per binary — NOT a registry/map):
```go
mux := asynq.NewServeMux()
mux.HandleFunc(queue.TypeImageConvert, worker.NewRunner(h, worker.ImageDescriptor).Handle)
```
Every other worker main (`document-worker`, `chromium-worker` [HTML], `audio-worker`, `av-worker`, `ocr-worker`, `pandoc-worker`) makes the identical one-line substitution, swapping only the descriptor variable and `queue.TypeXConvert` constant — `NewHandler(...)`'s ~10-parameter constructor call above it, and any post-construction setters (`SetProgressThrottle`/`SetProgressWebhook` in av/ocr/pandoc-worker) are UNCHANGED by this phase; only the `mux.HandleFunc` line moves.

---

## Shared Patterns

### Guarded metrics/webhook post-commit hook (the SC4 invariant)
**Source:** `internal/worker/worker.go` every `Handle*Convert`'s terminal-failure branch (e.g. lines 739-765) and success tail (774-781); the already-correct sibling in `internal/reconciler/reconciler.go:313-329`.
**Apply to:** `internal/worker/runner.go` (single call site instead of 7×2) and the `internal/reconciler/reconciler.go:311-312` fix.
```go
ferr := repo.MarkFailed(ctx, jobID, code, message, detail, elapsed)
if ferr == nil {
	metrics.RecordJobOutcome(job.Engine, jobs.StatusFailed, elapsed) // or RecordReconcilerAction, same guard
	metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, jobs.StatusFailed)
	if job.CallbackURL != "" {
		_ = enqueuer.EnqueueWebhookDeliver(ctx, jobID)
	}
}
```
Note: today's `RecordJobOutcome` call is NOT itself guarded on `ferr` (it fires unconditionally right after `MarkFailed`, only `RecordClientJobOutcome`/webhook are guarded) — worker.go:740-753's comment explains this is intentional (job-outcome duration histogram vs. client-attributed counter have different tolerance for the "MarkFailed didn't commit" edge case). Preserve this exact asymmetry in the runner; do not "fix" it as part of this phase — it is a deliberate existing choice, not the RUNR-02 bug.

### Sentinel-error classification via `errors.Is`
**Source:** `internal/jobs/repo.go:15-16` (`var ErrNotFound = errors.New(...)`, checked via `errors.Is`), `internal/worker/worker.go:1163-1176` (`avFailureCode`).
**Apply to:** `EngineDescriptor.FailureCode` implementations relocated into `runner.go`/`worker.go` — always check specific sentinels before falling through to the generic `"engine_error"` default; never string-match on `err.Error()` except where an existing function already does so as a last resort (`ocrFailureCode`'s `strings.Contains` fallback, worker.go:493-499 — preserve, do not "upgrade" to a new sentinel type in this phase, per D-04/D-05's no-rename freeze).

### `{"error": msg, "code": "..."}` flat envelope (D-01)
**Source:** `internal/api/handlers.go:1007-1010` (`writeQuotaError`), `internal/ratelimit/ratelimit.go:117`.
**Apply to:** every `writeError` call site in `internal/api/handlers.go` and `internal/api/jobs_list_handler.go`.
```go
writeJSON(w, status, map[string]string{"error": msg, "code": code})
```
Never nest as `{"error": {"code": ..., "message": ...}}` — `frontend/src/api.ts:54` reads `body.error` as a plain string (D-01, verified in CONTEXT.md).

### Package-level singleton/var wiring (not a registry)
**Source:** `internal/convert/converters.go`'s `init()`-populated `convert.Default` registry is the ONE existing global-state precedent in this codebase (CLAUDE.md Architectural Constraints), but D-09 explicitly rejects that shape for `EngineDescriptor` — use plain `var ImageDescriptor = EngineDescriptor{...}` package-level values in `internal/worker` instead, one per engine, imported directly by each `cmd/*-worker/main.go` (mirrors the existing "one Handler per binary" deployment model, not a `map[string]EngineDescriptor` indirection).

## No Analog Found

None — every file in scope has at least a role-match analog already present in the codebase (this phase is explicitly a consolidation of existing, well-established patterns, not new architecture).

## Metadata

**Analog search scope:** `internal/worker/`, `internal/api/`, `internal/reconciler/`, `internal/jobs/`, `internal/convert/`, `internal/ratelimit/`, `internal/metrics/`, `cmd/*/`
**Files scanned:** `internal/worker/worker.go` (full structure grep + 4 targeted reads: imports 1-32, HandleImageConvert 704-782, HandleDocumentConvert 784-903 partial, HandleAudioConvert 1033-1142, avFailureCode 1163-1196, ocrFailureCode 482-538, process() 1847-1942), `internal/api/handlers.go` (imports 1-45, writeJSON/writeQuotaError/quotaRejection/writeError 960-1052, full `writeError` call-site grep), `internal/api/jobs_list_handler.go` (full, 106 lines), `internal/reconciler/reconciler.go` (280-380), `internal/metrics/metrics.go` (grep for Record* functions + pinned label declarations), `internal/metrics/metrics_test.go` (TestJobDurationLabelsPinned/TestQueueDepthLabelsPinned), `internal/convert/convert.go` (60-176), `cmd/worker/main.go` (full), `cmd/av-worker/main.go` (1-180), `internal/jobs/jobs.go` (1-22), `internal/jobs/repo.go` (grep for ErrNotFound), `internal/ratelimit/ratelimit.go` (grep for rate_limited/code)
**Pattern extraction date:** 2026-08-08
```
