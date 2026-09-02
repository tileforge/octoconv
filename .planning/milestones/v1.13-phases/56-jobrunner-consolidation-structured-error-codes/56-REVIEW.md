---
phase: 56-jobrunner-consolidation-structured-error-codes
reviewed: 2026-08-08T04:35:39Z
depth: standard
files_reviewed: 41
files_reviewed_list:
  - cmd/audio-worker/main.go
  - cmd/av-worker/main.go
  - cmd/chromium-worker/main.go
  - cmd/document-worker/main.go
  - cmd/ocr-worker/main.go
  - cmd/pandoc-worker/main.go
  - cmd/worker/main.go
  - docs/ERROR-CODES.md
  - docs/README.md
  - frontend/src/api.ts
  - frontend/src/App.tsx
  - frontend/src/errorcodes.test.ts
  - frontend/src/no-text-matching.test.ts
  - internal/api/errcode_envelope_test.go
  - internal/api/handlers_test.go
  - internal/api/handlers.go
  - internal/api/jobs_list_handler.go
  - internal/api/presets_handlers.go
  - internal/api/sse.go
  - internal/api/system_presets_handlers_test.go
  - internal/api/system_presets_handlers.go
  - internal/api/system_usage_handlers_test.go
  - internal/api/system_usage_handlers.go
  - internal/auth/middleware_test.go
  - internal/auth/middleware.go
  - internal/errcode/doc_sync_test.go
  - internal/errcode/errcode_test.go
  - internal/errcode/errcode.go
  - internal/mcpserver/client.go
  - internal/mcpserver/errcode_consumer_test.go
  - internal/mcpserver/no_text_matching_test.go
  - internal/mcpserver/tools.go
  - internal/ratelimit/ratelimit_test.go
  - internal/ratelimit/ratelimit.go
  - internal/reconciler/reconciler_test.go
  - internal/reconciler/reconciler.go
  - internal/worker/descriptors.go
  - internal/worker/pandoc_integration_test.go
  - internal/worker/pinning_test.go
  - internal/worker/runner_test.go
  - internal/worker/runner.go
  - internal/worker/worker.go
findings:
  critical: 0
  warning: 1
  info: 3
  total: 4
status: issues_found
---

# Phase 56: Code Review Report

**Reviewed:** 2026-08-08T04:35:39Z
**Depth:** standard
**Files Reviewed:** 41
**Status:** issues_found

## Summary

This phase consolidates the seven per-engine `Handle*Convert` wrappers into a single
descriptor-driven `Runner`/`EngineDescriptor` (`internal/worker/runner.go`,
`internal/worker/descriptors.go`) and freezes a structured error-code vocabulary shared
between the sync HTTP surface (`internal/errcode`) and the async job-failure surface
(`internal/worker` `AllFailureCodes()`), documented and mechanically pinned by
`internal/errcode/doc_sync_test.go` against `docs/ERROR-CODES.md`. The refactor itself is
disciplined: `Runner.finish` is a genuine single terminal-outcome commit point (proven by
`TestSingleTerminalEmissionCallSite`), the `ferr`-guarded metric/webhook asymmetry is
intentional and well-tested, and the seven `cmd/*-worker/main.go` entry points are
consistent, well-commented clones of one another with only the expected per-engine
deltas.

The one substantive defect found is a resource-guard regression in `cmd/ocr-worker/main.go`:
unlike the sibling `cmd/pandoc-worker/main.go` (written in the same phase, guarding the
structurally identical `int64`→cast pattern), the OCR worker's `envInt64` helper has no
negative-value guard before it is cast to `uint64` for `OCR_MAX_IMAGE_PIXELS`, so a
negative env value silently disables the decompression-bomb guard instead of falling back
to a safe default. The remaining findings are minor code-quality observations (dead-code
constructor side effects, a duplicated string literal) that do not affect current
production behavior.

No hardcoded secrets, SQL/command injection, XSS, or `eval`-class issues were found in the
reviewed files. Error handling, ownership checks (404-not-403), and the closed error-code
vocabulary are all consistently applied and cross-checked by dedicated tests
(`errcode_envelope_test.go`, `no_text_matching_test.go`, `doc_sync_test.go`).

## Warnings

### WR-01: `OCR_MAX_IMAGE_PIXELS` negative value silently disables the decompression-bomb guard

**File:** `cmd/ocr-worker/main.go:89, 217-229`
**Issue:** `envInt64` (lines 222-229) has no negative-value check:

```go
func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(firstField(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}
```

It is used at line 89 as:

```go
ocrMaxPixels := uint64(envInt64("OCR_MAX_IMAGE_PIXELS", 100_000_000))
```

If an operator sets `OCR_MAX_IMAGE_PIXELS` to a negative value (e.g. `-1`), `envInt64`
parses it successfully and returns `-1` unchanged (no negative check, unlike this same
file's `resolveOCRThreadLimit`/`envDurationSeconds` or `av-worker`'s `envFloat`, all of
which explicitly reject non-positive values). `uint64(-1)` then wraps to
`18446744073709551615` — the pixel-bomb guard `OCRConverter.MaxImagePixels` is effectively
disabled rather than falling back to the safe 100-megapixel default.

This is the exact bug class `cmd/pandoc-worker/main.go`'s own `envInt64` (added in this
same phase, lines 233-256) explicitly guards against for `PANDOC_MAX_INPUT_BYTES`, with a
comment stating: *"A negative value is worse -- enforcePandocInputSize only applies the cap
when maxBytes > 0, so "-1" disables the guard entirely. Both now warn loudly and fall back."*
`cmd/ocr-worker/main.go`'s `envInt64` for the structurally identical
`int64`→`uint64`-cast-into-a-security-ceiling pattern was not given the same treatment.

**Fix:** Mirror `cmd/pandoc-worker/main.go`'s `envInt64` negative-value handling:

```go
func envInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f := firstField(v)
	n, err := strconv.ParseInt(f, 10, 64)
	if err != nil {
		log.Printf("⚠️ %s=%q is not an integer; using default %d", key, f, def)
		return def
	}
	if n < 0 {
		log.Printf("⚠️ %s=%d is negative (a negative cap would DISABLE the guard); using default %d", key, n, def)
		return def
	}
	return n
}
```

## Info

### IN-01: `HandleXConvert` thin delegators re-mutate shared `Handler` state on every call

**File:** `internal/worker/worker.go:779-781, 819-821` (and the other five `Handle*Convert` delegators), `internal/worker/runner.go:117-128`
**Issue:** `HandleImageConvert`/`HandleAudioConvert`/etc. are documented as existing only
for `pinning_test.go` compatibility and are never wired into a production `asynq.ServeMux`
(every `cmd/*-worker/main.go` registers `worker.NewRunner(h, XDescriptor).Handle` directly,
constructed once at startup). However, each delegator method calls `NewRunner(h, XDescriptor)`
on every invocation, and for the audio engine `NewRunner` mutates the shared
`h.preConvertGuard` field as a side effect on every call:

```go
func (h *Handler) HandleAudioConvert(ctx context.Context, t *asynq.Task) error {
	return NewRunner(h, AudioDescriptor).Handle(ctx, t)
}
```

Today this is inert because these delegators are unused in the real dispatch path. But a
constructor (`NewRunner`) that mutates the object passed into it as a side effect is a
surprising API shape; if a future change ever wires one of these delegator methods into a
real `mux.HandleFunc` registration (an easy mistake given the method exists and type-checks
identically to `Runner.Handle`), it would silently reintroduce a per-task-invocation mutation
of shared state — the exact per-call overhead/mutation this refactor was designed to
eliminate — with no compiler or test signal until a data race or behavioral drift surfaces.

**Fix:** Consider making the delegator methods construct the `Runner` once (e.g. cache it on
`Handler` or a package-level table) rather than reconstructing it per call, or add a comment
on `NewRunner` itself (not just at each call site) warning that it must be called exactly
once per `*Handler` per process. At minimum, an assertion/guard against being called twice
with a non-nil `preConvertGuard` already installed would catch an accidental second wiring
early.

### IN-02: `reconciler.go` hardcodes the `"reconciler_exhausted"` literal instead of referencing `worker.CodeReconcilerExhausted`

**File:** `internal/reconciler/reconciler.go:315`
**Issue:**

```go
ferr := s.store.MarkFailed(ctx, j.ID, "reconciler_exhausted", "recovery attempts exhausted", map[string]any{"action": "reconciler_exhausted"}, 0)
```

`internal/worker/descriptors.go` explicitly documents `CodeReconcilerExhausted` as "the one
code emitted OUTSIDE this package" and notes updating `internal/reconciler` to reference it
was deferred to a later sub-plan within this same phase. The two strings are currently kept
in sync only by a cross-package test (`TestReconcilerExhaustedCodeMatchesFrozenRegistry`,
`internal/reconciler/reconciler_test.go`), not by a shared constant. This is a reasonable
tradeoff to avoid `internal/reconciler` importing `internal/worker` in production code, and
is test-guarded, but it is still a hardcoded string duplicate of a "frozen" vocabulary value
— a future edit to either the literal or the test (without noticing the coupling) would
silently drift the two apart with no compiler signal, only a test failure if the test is run
and not skipped/deleted.

**Fix:** No urgent action needed given the existing test coverage; consider promoting
`CodeReconcilerExhausted`'s string value to a small shared package (or moving it into
`internal/jobs`, which both `internal/worker` and `internal/reconciler` already depend on)
in a later phase, per the plan 56-05 deferral already documented in `descriptors.go`.

### IN-03: `job` with zero `Inputs` rows is classified transient, not terminal, by `isTerminal`

**File:** `internal/worker/worker.go:1331-1340` (`inputKey`), `internal/worker/worker.go:148-194` (`isTerminal`)
**Issue:** `process()`'s `inputKey` returns `fmt.Errorf("job %s has no inputs", jobID)` when
a job has zero associated `job_inputs` rows. This error text matches none of `isTerminal`'s
signature lists (`minio.NoSuchKey`, `"no converter for"`, the vips/LibreOffice/Chromium/
veraPDF signature lists), so every engine's descriptor classifies it as transient and it is
retried up to the per-engine retry budget before the reconciler eventually exhausts it. In
practice `handleCreateJob` (`internal/api/handlers.go`) always writes exactly one input row
inside the same transaction as job creation, so this path should be unreachable in normal
operation; it is a defensive check against a structurally impossible state, not a
client-triggerable condition. Low priority, but worth a deliberate classification (terminal,
via a dedicated substring check or sentinel error) rather than falling through to the
broad-retry default, since a genuinely input-less job can never be fixed by retrying it.
**Fix:** Consider wrapping this condition in a sentinel error (e.g. `ErrNoInputs`) and adding
it to each engine's terminal classifier's deterministic-rejection list, consistent with how
other structurally-impossible states in this codebase are treated as terminal.

---

_Reviewed: 2026-08-08T04:35:39Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
