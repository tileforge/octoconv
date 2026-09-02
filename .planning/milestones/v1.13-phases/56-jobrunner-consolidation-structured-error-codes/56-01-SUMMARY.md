---
phase: 56-jobrunner-consolidation-structured-error-codes
plan: 01
subsystem: testing
tags: [go, asynq, prometheus, worker, pinning-tests, tdd]

# Dependency graph
requires: []
provides:
  - "convertRepo / webhookEnqueuer / processFn seams on internal/worker.Handler, unlocking handler-level unit tests without live Postgres/Redis/S3"
  - "internal/worker/pinning_test.go: a frozen, handler-level pinning suite over all seven Handle*Convert wrappers (terminal/transient/canceled/success/unparseable-payload outcomes, failure-code mapping, emission label sets, emission ordering, MarkFailed-error guard asymmetry, opts-guard-before-MarkActive, and the audio/av stage-polarity contrast)"
affects: [56-02, 56-03, 56-04, 56-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Interface-segregated test seams (convertRepo/webhookEnqueuer) mirroring internal/api/api.go's Repo/Storage/Enqueuer precedent, applied to internal/worker.Handler"
    - "processFn method-value indirection (assigned in NewHandler, overridable per-test) to lift a Handler's live-DB testing restriction without touching process() itself"
    - "Pinning suite: dispatch via Go method expressions ((*Handler).HandleXConvert) rather than bound method calls, so wrapper-level behavior is data-driven across a seven-row engine table"
    - "Shared ordered call-log fakes (pinRepo + pinEnqueuer writing into one *[]string) for positional (not count-only) ordering assertions"

key-files:
  created:
    - internal/worker/pinning_test.go
  modified:
    - internal/worker/worker.go
    - internal/worker/pandoc_integration_test.go

key-decisions:
  - "convertRepo is a strict superset of the pre-existing progressRepo interface (UpdateProgress/SetSourceDuration) so process()'s interface-to-interface assignment to runConvertWithOptionalProgress keeps compiling unchanged"
  - "NewHandler keeps its concrete *jobs.Repo/*queue.Client parameter types; only the Handler struct fields they populate are narrowed to convertRepo/webhookEnqueuer, so no production wiring changes (T-56-01)"
  - "Pinning suite reuses only error strings/inputs already present in worker_test.go's existing passing tables (TestIsTerminalVipsSignatures, TestIsAudioTerminal, TestAVFailureCode, TestOCRFailureCode, TestPandocFailureCode, etc.) -- never an invented error string"
  - "TestPinnedOptsGuardPrecedesMarkActive deliberately asserts nothing about octoconv_job_outcomes_total/octoconv_client_job_outcomes_total on the opts-guard branch -- that gap is ERRC-02, closed by plan 56-04, not this pinning suite"
  - "TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook pins the pre-existing asymmetry (octoconv_job_outcomes_total unguarded on MarkFailed's own error; octoconv_client_job_outcomes_total and the webhook enqueue ARE guarded) as intentional, not the RUNR-02 defect"
  - "Success-path processFn stubs call repo.MarkDone directly (pinSuccessProcessFn), matching process()'s own contract, after an initial version that returned nil silently made the emission-ordering assertion vacuously pass"

patterns-established:
  - "Pinning-before-refactor protocol (56-CONTEXT.md D-10 step 1): write/expand tests against TODAY's implementation until they pass unmodified, before any behavior-changing refactor lands"
  - "Method-expression dispatch table testing pattern for verifying N structurally-similar wrapper functions without repeating call-site boilerplate"

requirements-completed: [RUNR-03]

# Metrics
duration: ~20min
completed: 2026-08-08
---

# Phase 56 Plan 01: Pinning Suite for the Seven-Handler Network Summary

**Introduced behavior-preserving test seams (convertRepo/webhookEnqueuer/processFn) on internal/worker.Handler and wrote a 1007-line handler-level pinning suite covering all seven Handle*Convert wrappers' terminal/transient/canceled/success outcomes, failure-code mappings, emission ordering, MarkFailed-error guard asymmetry, opts-guard-before-MarkActive, and the audio/av stage-polarity contrast — all without a live Postgres, Redis, S3, or engine binary.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-08T02:55:00Z (approx.)
- **Completed:** 2026-08-08T03:12:16Z
- **Tasks:** 3
- **Files modified:** 3 (1 created, 2 modified)

## Accomplishments

- Lifted the pre-existing restriction that a full `Handler` could not be constructed in a pure unit test (documented in `pandoc_integration_test.go`), via three type-level, behavior-preserving seams: `convertRepo`, `webhookEnqueuer`, `processFn`.
- Wrote a handler-level pinning suite (`internal/worker/pinning_test.go`, 1007 lines) that drives all seven `Handle*Convert` methods directly through fakes, covering:
  - Terminal outcome (default `engine_error` code) for all seven engines
  - Every non-default failure-code branch for av/ocr/pandoc/audio (19 sub-cases, reusing `TestAVFailureCode`/`TestOCRFailureCode`/`TestPandocFailureCode`/`TestIsAudioTerminal`'s exact inputs)
  - Transient, canceled, success, and unparseable-payload outcomes for all seven engines
  - Metric emission label-name sets (`{engine,status}` for `octoconv_job_outcomes_total`/`octoconv_job_duration_seconds`, `{client_id,engine,status}` for `octoconv_client_job_outcomes_total`)
  - Positional emission-ordering (webhook enqueue never precedes a `MarkFailed`/`MarkDone` commit token)
  - `MarkFailed`-error suppression of the client counter and webhook, with the pre-existing `RecordJobOutcome` asymmetry pinned as-is
  - The strict-opts-reparse guard preceding `MarkActive` for document/html/audio/av/ocr/pandoc, and image's deliberate absence of that guard
  - The audio/av stage-polarity contrast (`isAudioTerminal`'s ffmpeg-stage-terminal/whisper-stage-transient vs. `isAVTerminal`'s transcode-stage-transient/audio-extract-stage-terminal)
- Verified a zero-line diff on `internal/metrics` and that `internal/worker/worker.go`'s diff against the plan's base commit is exactly the seam introduction (65 insertions / 11 deletions), no emission call site added/removed/reordered.
- Whole-repo `go test ./... -count=1` green.

## Task Commits

Each task was committed atomically:

1. **Task 1: Introduce behavior-preserving test seams in Handler** - `9218b97` (refactor)
2. **Task 2: Pin every engine class's terminal, transient, canceled and success outcome** - `b67eb41` (test)
3. **Task 3: Pin emission ordering, guard semantics and the two stage asymmetries** - `abeb5f7` (test)

**Plan metadata:** (this commit, following)

_Note: Tasks 2 and 3 are `tdd="true"` in the plan, but this is a pinning suite over PRE-EXISTING, already-correct behavior (56-CONTEXT.md D-10 step 1) rather than new-feature development — the tests are written to pass immediately against unmodified code, which is the intended proof (see "TDD Gate Compliance" below)._

## Files Created/Modified

- `internal/worker/pinning_test.go` - New handler-level pinning suite (harness: `pinRepo`/`pinEnqueuer`/`newPinHandler`/`readCounter`/`assertLabelNames`/`newTask`; 7-row `pinEngines` table; 12 top-level test functions)
- `internal/worker/worker.go` - Added `convertRepo`/`webhookEnqueuer` interfaces, widened `Handler.repo`/`Handler.enqueuer` field types, added `Handler.processFn`, substituted `h.process(ctx, job)` → `h.processFn(ctx, job)` at all seven call sites
- `internal/worker/pandoc_integration_test.go` - Updated the stale doc comment that claimed a full `Handler` could never be constructed in a pure unit test

## Decisions Made

- `convertRepo`'s method set is a strict superset of `progressRepo`'s two methods, preserving the existing interface-to-interface assignment inside `process()` without any change to `process()` itself.
- `NewHandler` keeps concrete `*jobs.Repo`/`*queue.Client` parameters (only the struct fields are narrowed), so zero production wiring changed (`T-56-01` in the plan's threat register).
- Every pinning-test error input is copied verbatim from an existing passing `worker_test.go` table — no invented error string, per the plan's explicit instruction.
- The opts-guard pinning test intentionally does not assert on either job-outcome counter family, since today's absence of an increment on that branch is `ERRC-02`, a requirement plan 56-04 is expected to close, not a defect this frozen suite should encode as correct-forever.
- The `MarkFailed`-error-suppression test documents the `RecordJobOutcome`-unguarded / `RecordClientJobOutcome`-and-webhook-guarded asymmetry as deliberate and pre-existing (distinct from `RUNR-02`, the reconciler's unguarded `RecordReconcilerAction("exhausted")`, which is a separate defect fixed elsewhere in this phase).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Success-path processFn stub silently dropped the MarkDone commit token from the ordering pin**
- **Found during:** Task 3 (`TestPinnedEmissionNeverPrecedesCommit`)
- **Issue:** The success-outcome `processFn` stub used in Tasks 2 and 3 (`func(context.Context, *jobs.Job) error { return nil }`) does not call `repo.MarkDone` — but in production, `process()` itself calls `h.repo.MarkDone` before returning nil (worker.go's documented "Postgres-first: MarkDone already committed inside process()" contract). With the bare-`nil` stub, the success sub-case's shared call log never contained a `MarkDone` token, so `TestPinnedEmissionNeverPrecedesCommit`'s positional assertion failed for all seven engines' success sub-cases (correctly — it caught a harness bug, not a real defect in worker.go).
- **Fix:** Introduced `pinSuccessProcessFn(repo *pinRepo) func(context.Context, *jobs.Job) error`, which calls `repo.MarkDone(ctx, job.ID, 0)` before returning nil, exactly mirroring `process()`'s own contract. Replaced all four bare-`nil` success stubs (`TestPinnedSuccessOutcomePerEngine`, `TestPinnedEmissionLabelSets`, `TestPinnedEmissionNeverPrecedesCommit`, `TestPinnedImageHasNoOptsGuard`) with this helper.
- **Files modified:** `internal/worker/pinning_test.go`
- **Verification:** `go test ./internal/worker/... -run TestPinned -count=1 -v` — all 100+ sub-tests pass, including the emission-ordering assertions that previously failed.
- **Committed in:** `abeb5f7` (Task 3 commit — the fix landed within the same task before that task's commit, since it was discovered while completing Task 3's own verification step)

---

**Total deviations:** 1 auto-fixed (Rule 1 - Bug)
**Impact on plan:** The fix is a test-harness correction, not a change to any production code path — `internal/worker/worker.go` and `internal/metrics` retain their zero-line/seam-only diffs. No scope creep.

## Issues Encountered

- `.planning/phases/56-jobrunner-consolidation-structured-error-codes/56-PATTERNS.md`, referenced by the plan's `<context>` block, does not exist in this phase directory (only `56-CONTEXT.md` and `56-DISCUSSION-LOG.md` are present). Proceeded using `56-CONTEXT.md`, `.planning/research/ARCHITECTURE.md`, and `.planning/research/PITFALLS.md` instead, which cover the same D-10/Pitfall-2/Pitfall-4/Pitfall-14 material the plan's task instructions cite directly. No plan content was blocked by this absence — the plan's task `<action>` blocks are self-contained and reference `worker.go`/`worker_test.go` line ranges directly, which is what was actually used to write the tests.

## TDD Gate Compliance

Tasks 2 and 3 are marked `tdd="true"`, but this plan is explicitly a **pinning suite over the pre-existing, already-correct behavior** of the seven handlers (56-CONTEXT.md D-10 step 1 of a 3-step zero-behavior-change protocol) — not new-feature development. The plan's own `<action>` and `<verify>` blocks expect the suite to pass **immediately** against unmodified `worker.go` (Task 1's seams aside), since the entire purpose is to freeze today's behavior before plan 56-04's refactor. A literal RED phase (a test that must fail before any code exists) does not apply here — there is no new implementation to write; the tests assert behavior that `worker.go` already exhibits. Both `test(56-01): ...` commits (`b67eb41`, `abeb5f7`) followed the "write test → run → verify it passes for the right reason" discipline instead, and the one genuine bug found (the `MarkDone`-dropping success stub, documented above) was caught by an assertion that DID fail first, was root-caused to the test harness (not `worker.go`), and was fixed before commit — consistent with the plan's fail-fast intent even though it does not map cleanly onto the standard RED→GREEN→REFACTOR commit sequence.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `internal/worker/pinning_test.go` is now the frozen reference suite plan 56-04's `Runner`/`EngineDescriptor` consolidation must make pass **unchanged** (per the file-level doc comment and 56-CONTEXT.md D-10 step 2).
- `convertRepo`/`webhookEnqueuer`/`processFn` seams are available for any further worker-level unit testing needed by 56-02/56-03 (structured error-code work) without requiring live infrastructure.
- No blockers for 56-02 through 56-05.

## Self-Check: PASSED

- FOUND: internal/worker/pinning_test.go
- FOUND: internal/worker/worker.go (convertRepo/webhookEnqueuer/processFn present)
- FOUND: internal/worker/pandoc_integration_test.go (doc comment updated)
- FOUND commit 9218b97
- FOUND commit b67eb41
- FOUND commit abeb5f7

---
*Phase: 56-jobrunner-consolidation-structured-error-codes*
*Completed: 2026-08-08*
