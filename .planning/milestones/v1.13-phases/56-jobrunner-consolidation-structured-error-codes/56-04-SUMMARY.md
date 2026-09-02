---
phase: 56-jobrunner-consolidation-structured-error-codes
plan: 04
subsystem: infra
tags: [go, asynq, prometheus, worker, runner, refactor]

# Dependency graph
requires:
  - phase: 56-jobrunner-consolidation-structured-error-codes
    provides: "plan 56-01's frozen pinning suite (internal/worker/pinning_test.go) over the seven Handle*Convert wrappers, and the convertRepo/webhookEnqueuer/processFn test seams on Handler"
provides:
  - "internal/worker/runner.go: EngineDescriptor type + Runner + NewRunner + Handle, with one guarded finish() method as the sole terminal-outcome commit point (metrics + webhook enqueue) for all seven engine classes"
  - "internal/worker/descriptors.go: the frozen 14-code async job-failure vocabulary as exported Go constants (+ AllFailureCodes()), and the seven package-level EngineDescriptor vars (ImageDescriptor..PandocDescriptor)"
  - "ERRC-02 closed: terminal invalid_options failures on all six opts-guarded engines now increment both octoconv_job_outcomes_total and octoconv_client_job_outcomes_total"
  - "All seven worker binaries (cmd/worker, cmd/document-worker, cmd/chromium-worker, cmd/audio-worker, cmd/av-worker, cmd/ocr-worker, cmd/pandoc-worker) register their asynq mux through worker.NewRunner(h, worker.XDescriptor).Handle"
affects: [56-05, 56-06, 56-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Descriptor-driven single Runner: seven near-identical wrapper functions collapsed into one Runner.Handle over an EngineDescriptor struct carrying each engine's differences (OptsFromMap/IsTerminal/FailureCode/PreConvertGuard) as DATA, not branches"
    - "Single guarded terminal-outcome commit point (Runner.finish): the ONLY call site in internal/worker for metrics.RecordJobOutcome/RecordClientJobOutcome/EnqueueWebhookDeliver, structurally unable to fire before MarkFailed/MarkDone's guarded transition commits"
    - "Thin delegating wrapper methods retained for frozen-test compatibility: the seven Handle*Convert methods on *Handler still exist as one-line delegators to NewRunner(h, XDescriptor).Handle, solely so pinning_test.go's Go method-expression dispatch table ((*Handler).HandleXConvert) keeps compiling and keeps exercising the real Runner path unchanged; production wiring (cmd/*-worker/main.go) calls NewRunner(...).Handle directly and never references these methods"
    - "Frozen async error-code registry as exported Go constants (internal/worker/descriptors.go), replacing scattered string literals across avFailureCode/ocrFailureCode/pandocFailureCode/runner.go"

key-files:
  created:
    - internal/worker/runner.go
    - internal/worker/descriptors.go
    - internal/worker/runner_test.go
  modified:
    - internal/worker/worker.go
    - cmd/worker/main.go
    - cmd/document-worker/main.go
    - cmd/chromium-worker/main.go
    - cmd/audio-worker/main.go
    - cmd/av-worker/main.go
    - cmd/ocr-worker/main.go
    - cmd/pandoc-worker/main.go

key-decisions:
  - "The seven Handle*Convert methods on *Handler are kept as one-line delegators to NewRunner(h, XDescriptor).Handle rather than deleted outright: internal/worker/pinning_test.go's frozen pinEngines table dispatches via Go method expressions ((*Handler).HandleImageConvert etc.), which requires the methods to exist as compilable symbols. Deleting them would break the pinning suite's compilation, directly violating D-10 step 2's 'the refactor must make this suite pass UNCHANGED' contract and threat T-56-13. This makes one of the plan's own acceptance-criteria greps ('func (h *Handler) Handle.*Convert' returns 1) fail as literally written (it returns 7); the actual `go test` proof (all TestPinned* tests pass unchanged, zero-line diff on pinning_test.go) is the load-bearing verification, not that specific grep heuristic."
  - "Runner.finish() labels metrics with r.d.Engine (the EngineDescriptor's own Engine field) instead of job.Engine as the plan's action text literally specified. The two are byte-identical in production (a job's persisted engine column always matches the descriptor its own asynq task type routed to), but pinning_test.go's newPinJob helper never populates Job.Engine on its test fixtures -- using job.Engine caused every counter-delta assertion in TestPinnedSuccessOutcomePerEngine, TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook, TestPinnedAudioStageAsymmetry and TestPinnedAVStageAsymmetry to read the wrong Prometheus series (engine label empty string, not e.g. \"image\") and fail. r.d.Engine is not a queue.QueueX literal (satisfies the plan's Pitfall-4 constraint) and is structurally guaranteed correct since NewRunner binds Runner to exactly one descriptor at construction."
  - "AudioDescriptor.PreConvertGuard is deliberately left nil in descriptors.go; NewRunner special-cases convert.EngineAudio and installs a closure built by the new audioPreConvertGuard(h.audioMaxDuration) helper instead. EngineDescriptor is a package-level var built once at package-init time, before any *Handler (and therefore before h.audioMaxDuration, itself env-configured per binary) exists, so a closure assigned at var-init time could never read a specific Handler's duration ceiling. This was an explicitly plan-sanctioned choice ('Choose one, state the choice in a comment') between this approach and adding a dedicated EngineDescriptor.AudioMaxDuration field, which would have violated the plan's fixed six-field EngineDescriptor list."
  - "process()'s enforceAudioGuardBeforeConvert splice (worker.go) was generalized to call h.preConvertGuard when non-nil, convertFn() directly otherwise -- functionally identical to the pre-refactor unconditional call to enforceAudioGuardBeforeConvert, since that function's own engine-gate (if engine == convert.EngineAudio) was already a pass-through for every non-audio engine. No test exercises process() with a real (non-stubbed) audio job, so this equivalence was verified by direct code inspection plus the two enforceAudioGuardBeforeConvert unit tests, which call the unmodified function directly and are unaffected by the splice's own shape."

patterns-established:
  - "EngineDescriptor as the single per-engine difference table: any future engine class change (a new failure code branch, a new terminal signature) is now a descriptors.go data edit, not a new copy of a 79-163-line wrapper function"

requirements-completed: [RUNR-01, RUNR-03, ERRC-02]

# Metrics
duration: ~35min
completed: 2026-08-08
---

# Phase 56 Plan 04: JobRunner Consolidation Summary

**Collapsed seven near-identical Handle*Convert wrappers (94-163 lines each) into one descriptor-driven Runner with a single guarded terminal-outcome commit point, closing the invalid_options metrics gap (ERRC-02) as a direct consequence, while keeping plan 56-01's frozen pinning suite passing with a byte-for-byte zero-line diff.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-08-08T06:10:00Z (approx.)
- **Completed:** 2026-08-08T06:45:30Z
- **Tasks:** 3
- **Files modified:** 11 (3 created, 8 modified)

## Accomplishments

- `internal/worker/runner.go`: `EngineDescriptor` struct (Engine/Type/OptsFromMap/IsTerminal/FailureCode/PreConvertGuard), `Runner`/`NewRunner`/`Handle` reproducing the shared skeleton exactly once, and `finish()` — the single call site in the entire package for `metrics.RecordJobOutcome`, `metrics.RecordClientJobOutcome` and `EnqueueWebhookDeliver`, structurally unable to fire before a guarded `MarkFailed`/`MarkDone` transition commits.
- `internal/worker/descriptors.go`: all fourteen async job-failure error codes as exported Go constants (byte-identical string values, D-04/D-05 frozen) plus `AllFailureCodes()`, and the seven package-level `EngineDescriptor` vars (`ImageDescriptor`..`PandocDescriptor`) — never a `map[string]EngineDescriptor` registry (D-09).
- All seven `Handle*Convert` methods on `*Handler` reduced from 79-163 lines each to a single delegating line; the seven `avFailureCode`/`ocrFailureCode`/`pandocFailureCode`/opts-guard literal strings replaced with the new frozen constants; `audioFailureCode` lifted from `HandleAudioConvert`'s own inline two-way branch into a named, independently unit-testable function.
- All seven worker binaries (`cmd/worker`, `cmd/document-worker`, `cmd/chromium-worker`, `cmd/audio-worker`, `cmd/av-worker`, `cmd/ocr-worker`, `cmd/pandoc-worker`) rewired to register their asynq mux through `worker.NewRunner(h, worker.XDescriptor).Handle`.
- ERRC-02 closed: `internal/worker/runner_test.go`'s `TestInvalidOptionsRecordsBothOutcomeCounters` proves, for all six opts-guarded engines, that a terminal `invalid_options` failure now increments both outcome counters — a gap plan 56-01's pinning suite deliberately left un-pinned for exactly this reason.
- `TestSingleTerminalEmissionCallSite` is a structural, file-scanning proof (not just a code-review claim) that `metrics.RecordJobOutcome`/`metrics.RecordClientJobOutcome` each occur exactly once, and `EnqueueWebhookDeliver` has exactly one genuine call site, across all non-test `internal/worker` source.
- `internal/worker/pinning_test.go`, `internal/worker/worker_test.go`, `internal/metrics` and `cmd/webhook-worker/HandleWebhookDeliver` are all byte-identical to their state before this plan (verified by `git diff --name-only` and a direct before/after body diff of `HandleWebhookDeliver`). `go build ./... && go vet ./... && go test ./... -count=1` green throughout.

## Task Commits

Each task was committed atomically:

1. **Task 1: Build EngineDescriptor and Runner with one guarded terminal-outcome block** - `91ebab0` (feat)
2. **Task 2: Declare the seven descriptors, delete the seven handlers, rewire the seven binaries** - `8ab3b46` (feat)
3. **Task 3: Close ERRC-02 and assert the single-call-site structure** - `925089a` (test)

**Plan metadata:** (this commit, following)

## Files Created/Modified

- `internal/worker/runner.go` - `EngineDescriptor`, `Runner`, `NewRunner`, `Handle`, `finish`, `audioPreConvertGuard`
- `internal/worker/descriptors.go` - Frozen 14-code constant vocabulary, `AllFailureCodes()`, `audioFailureCode`, the seven `*Descriptor` vars
- `internal/worker/runner_test.go` - `TestInvalidOptionsRecordsBothOutcomeCounters`, `TestInvalidOptionsMarkFailedErrorSuppressesClientCounter`, `TestSingleTerminalEmissionCallSite`
- `internal/worker/worker.go` - Added `Handler.preConvertGuard` field, generalized `process()`'s splice, reduced the seven `Handle*Convert` methods to one-line delegators, replaced failure-code literals with the new constants
- `cmd/worker/main.go`, `cmd/document-worker/main.go`, `cmd/chromium-worker/main.go`, `cmd/audio-worker/main.go`, `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go` - Mux registration rewired to `worker.NewRunner(h, worker.XDescriptor).Handle`

## Decisions Made

See `key-decisions` in frontmatter for full rationale on: (1) keeping the seven `Handle*Convert` methods as thin delegators rather than deleting them outright, (2) using `r.d.Engine` instead of the plan's literally-specified `job.Engine` for metric labels, (3) installing the audio pre-convert guard via `NewRunner`'s engine-specific branch rather than a per-descriptor closure, and (4) generalizing `process()`'s splice as a behavior-preserving change.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Metric labels used job.Engine per the plan's literal instruction, but the frozen pinning fixture never populates Job.Engine**
- **Found during:** Task 2, running `go test ./internal/worker/... -run TestPinned` after gutting the seven handlers
- **Issue:** The plan's Task 1 action text states "Use job.Engine for every metric label. Do not reference queue.QueueImage or any sibling literal anywhere in runner.go." Implementing this literally caused `metrics.RecordJobOutcome`/`RecordClientJobOutcome` to be called with an empty-string engine label on every pinning-test job, because `pinning_test.go`'s `newPinJob` helper (frozen, read-only) never sets `Job.Engine`. Four test functions failed with `octoconv_job_outcomes_total delta = 0, want 1` (reading the wrong Prometheus series).
- **Fix:** Changed `runner.go`'s `finish()` to label both metrics calls with `r.d.Engine` (the `EngineDescriptor`'s own `Engine` field, which `Runner` is structurally bound to at construction) instead of `job.Engine`. The two values are byte-identical in every real deployment (a job's persisted `engine` column always matches the descriptor its own asynq task type routed to); this is not a `queue.QueueX` literal, so it does not reintroduce the Pitfall-4 pattern the plan's instruction was actually guarding against.
- **Files modified:** `internal/worker/runner.go`
- **Verification:** `go test ./internal/worker/... -run TestPinned -count=1` — all sub-tests pass; whole-repo `go test ./... -count=1` green.
- **Committed in:** `91ebab0` (Task 1 commit — the fix landed before Task 1's own commit, while completing Task 1's verification step; Task 2 continued to build on the corrected version)

**2. [Rule 3 - Blocking, documented deviation] Kept the seven `Handle*Convert` methods as thin delegators instead of deleting them, and left one of the plan's own acceptance-criteria greps unsatisfiable as literally written**
- **Found during:** Task 2, while implementing "delete the seven `Handle*Convert` methods from `worker.go`"
- **Issue:** `internal/worker/pinning_test.go`'s frozen `pinEngines` table dispatches every wrapper via a Go method expression (e.g. `wrapper: (*Handler).HandleImageConvert`). Literally deleting the seven `Handle*Convert` methods, as Task 2's action text instructs, makes `pinning_test.go` fail to compile — directly contradicting the plan's own repeated, threat-registered (T-56-13) requirement that `pinning_test.go` have a zero-line diff and pass unmodified in every task's verify step, and contradicting Task 2's own verify command (`go test ./... -count=1` must be green).
- **Fix:** Reduced each of the seven `Handle*Convert` methods to a single delegating line (`return NewRunner(h, XDescriptor).Handle(ctx, t)`) rather than deleting them — satisfying the "no per-engine handler LOGIC" spirit of D-09's must-have truth (every byte of terminal-outcome logic now lives solely in `Runner`/`descriptors.go`) while keeping the frozen dispatch table compiling and exercising the real `Runner` path unchanged. Every worker binary's own mux registration was rewired to call `worker.NewRunner(h, worker.XDescriptor).Handle` directly (never `h.HandleXConvert`), satisfying every other Task-2 acceptance-criteria grep. The one criterion that cannot be satisfied as literally written — `grep -c 'func (h \*Handler) Handle.*Convert' internal/worker/worker.go` returning `1` — returns `7` instead; the load-bearing proof (whole-suite `go test` green, zero-diff `pinning_test.go`) is documented as taking priority.
- **Files modified:** `internal/worker/worker.go` (the seven method bodies), all seven `cmd/*-worker/main.go` files
- **Verification:** `go test ./... -count=1` green; `git diff --name-only -- internal/worker/pinning_test.go internal/worker/worker_test.go internal/metrics cmd/webhook-worker` prints nothing; `grep -c 'worker.NewRunner(h, worker' <each of the seven main.go>` returns 1 each; `grep -c 'h.Handle.*Convert' <each of the seven main.go>` returns 0 each.
- **Committed in:** `8ab3b46` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (1 Rule 1 - Bug, 1 Rule 3 - Blocking/documented)
**Impact on plan:** Both deviations were required to keep the frozen pinning suite (plan 56-01's own gate, threat T-56-13) green while still delivering every must-have truth and success criterion this plan defines. No scope creep — no file outside the plan's declared `files_modified` list was touched, and `internal/metrics`/`internal/reconciler`/`cmd/webhook-worker` remain untouched as instructed.

## Issues Encountered

None beyond the two deviations documented above, both discovered and resolved via the normal task verification loop (running the plan's own `<verify>` commands after each task's implementation).

## TDD Gate Compliance

Task 3 is marked `tdd="true"`, but — mirroring plan 56-01's own precedent, documented in its SUMMARY — the behavior `TestInvalidOptionsRecordsBothOutcomeCounters` and `TestSingleTerminalEmissionCallSite` assert was already implemented by Tasks 1 and 2 within this same plan (routing the opts-guard branch through `Runner.finish` was Task 1's design, not deferred to Task 3). Task 3's job was purely to WRITE the tests proving that already-implemented behavior and the structural single-call-site invariant; running them for the first time passed immediately, which is the intended proof, not a RED-phase violation — there was no new implementation left to write in Task 3 (its only file is `internal/worker/runner_test.go`). A single `test(56-04): ...` commit was made; no separate `feat`/`refactor` commit was needed or created for this task.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Plan 56-05 (reconciler RUNR-02 fix) can now reference `internal/worker/descriptors.go`'s `CodeReconcilerExhausted` constant when it updates `internal/reconciler/reconciler.go`'s own `"reconciler_exhausted"` literal — this plan deliberately left `internal/reconciler` untouched per its own scope boundary.
- Plan 56-06 (Go-to-doc sync test) can consume `worker.AllFailureCodes()` directly.
- `internal/worker/runner.go`/`descriptors.go` are the new single source of truth for per-engine behavior; any future engine-class addition is a `descriptors.go` data edit, not a new 79-163-line wrapper function.
- No blockers for 56-05 through 56-07.

## Self-Check: PASSED

- FOUND: internal/worker/runner.go
- FOUND: internal/worker/descriptors.go
- FOUND: internal/worker/runner_test.go
- FOUND commit 91ebab0
- FOUND commit 8ab3b46
- FOUND commit 925089a

---
*Phase: 56-jobrunner-consolidation-structured-error-codes*
*Completed: 2026-08-08*
