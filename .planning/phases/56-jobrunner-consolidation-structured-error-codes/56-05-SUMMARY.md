---
phase: 56-jobrunner-consolidation-structured-error-codes
plan: 05
subsystem: infra
tags: [go, prometheus, reconciler, worker, metrics, testing]

# Dependency graph
requires:
  - phase: 56-jobrunner-consolidation-structured-error-codes
    provides: "plan 56-04's single guarded terminal-outcome commit point (internal/worker/runner.go's finish()) and frozen async job-failure registry (internal/worker/descriptors.go, worker.CodeReconcilerExhausted, worker.AllFailureCodes())"
provides:
  - "internal/reconciler/reconciler.go: RUNR-02 closed -- RecordReconcilerAction(\"exhausted\") and the exhaustion webhook enqueue are both guarded on MarkFailed's ferr == nil, matching the already-correct RecordClientJobOutcome guard three lines below"
  - "internal/reconciler/reconciler_test.go: TestSweepExhaustedActionGuardedOnMarkFailed, TestSweepUnroutableEngineActionStillUnguarded, TestReconcilerExhaustedCodeMatchesFrozenRegistry"
  - "56-05-PROOF.md: committed inversion-run evidence for ROADMAP SC4 (emission-before-commit is structurally impossible) and a metric-label zero-diff evidence trail for ROADMAP SC3 (KEDA-pinned label sets unchanged for the entire phase)"
affects: [56-06, 56-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Guard-the-sibling-call pattern applied a second time in the same file: RecordReconcilerAction(\"exhausted\") now follows the same `if ferr == nil` discipline RecordClientJobOutcome already used three lines below it -- the exact 'same defect class, same fix shape' instance the phase's own ARCHITECTURE.md/PITFALLS.md call out"
    - "Inversion-run proof method (D-10 step 3): a claim that a test suite catches a defect class is only demonstrated by temporarily reintroducing the defect and observing the suite fail with the expected assertion messages, then restoring via `git checkout --` and re-verifying green -- not by reasoning about the suite's shape"
    - "Diff-based zero-cardinality-drift proof (PITFALLS.md Pitfall 4, mirroring v1.12 FAIR-02): metric-label invariance across a whole phase is proven with `git diff <phase-base>..HEAD -- internal/metrics` plus a `[]string{` grep across that diff, not by running the pinned-label tests once at the end"

key-files:
  created:
    - .planning/phases/56-jobrunner-consolidation-structured-error-codes/56-05-PROOF.md
  modified:
    - internal/reconciler/reconciler.go
    - internal/reconciler/reconciler_test.go

key-decisions:
  - "Guarded the exhaustion-path webhook enqueue on ferr == nil in addition to the RecordReconcilerAction counter, even though the plan's 'Fix' paragraph only described moving the counter. The plan's own <behavior> block and Task 2's action-text description of TestSweepExhaustedActionGuardedOnMarkFailed both explicitly require 'zero webhook enqueues' when MarkFailed errors -- firing a job-failed webhook off the back of a MarkFailed call that never actually committed is the identical correctness defect as the unguarded counter (a webhook telling a client the job failed when the row is, in Postgres, still stranded and unmodified). Applied as Rule 1 (auto-fix bug): the current code's webhook enqueue had no ferr guard at all, which is inconsistent with the reconciler's own Postgres-first design principle documented two lines above it (\"the failed status is already committed by MarkFailed above, so a failed enqueue must not undo it\") -- that comment already presupposed ferr == nil without the code enforcing it."
  - "Task 2's inversion run edited internal/worker/runner.go's finish() in place, ran the pinning suite, captured the verbatim FAIL output, then restored via `git checkout --` and confirmed a byte-for-byte diff against the pre-inversion file -- never committed the inverted code, per the plan's explicit instruction and threat T-56-17."
  - "56-05-PROOF.md was split into two atomic commits (Task 2's inversion evidence, then Task 3's metric-label evidence appended) rather than written once, matching the plan's own task boundaries even though both tasks declare the same single output file."

patterns-established:
  - "RUNR-02's fix in reconciler.go is now the second worked example (after runner.go's finish()) of the project's 'guard the terminal-outcome side effect on ferr == nil, textually after the MarkFailed/MarkDone call' pattern -- a future engine class or future reconciler action must follow the same shape."

requirements-completed: [RUNR-02, RUNR-03]

# Metrics
duration: ~50min
completed: 2026-08-08
---

# Phase 56 Plan 05: Reconciler RUNR-02 Fix + Inversion-Run Proof Summary

**Closed the live RUNR-02 defect (an unguarded `RecordReconcilerAction("exhausted")` metric plus an unguarded exhaustion webhook enqueue in `internal/reconciler/reconciler.go`) and produced two committed proof artifacts for ROADMAP claims that cannot be established by code alone: an inversion run showing plan 56-01's pinning suite genuinely catches emission-before-commit, and a diff-based zero-cardinality-drift trail proving the phase never touched a KEDA-pinned metric label.**

## Performance

- **Duration:** ~50 min
- **Completed:** 2026-08-08T04:06:45Z (approx.)
- **Tasks:** 3
- **Files modified:** 3 (1 created, 2 modified)

## Accomplishments

- `internal/reconciler/reconciler.go`: `RecordReconcilerAction("exhausted")` moved inside its own `if ferr == nil` guard (kept separate from the `RecordClientJobOutcome` guard since the action call does not read `job`), and the exhaustion-path `EnqueueWebhookDeliver` call also gained a `ferr == nil` condition — both were previously unconditional, meaning a `MarkFailed` call that did NOT commit still inflated the exhaustion counter every sweep tick AND could fire a "job failed" webhook for a job whose status transition never actually landed in Postgres. The fail-closed `default:` branch's `RecordReconcilerAction("unroutable_engine")` (no `MarkFailed` on that path at all) was deliberately left untouched.
- `internal/reconciler/reconciler_test.go`: three new tests — `TestSweepExhaustedActionGuardedOnMarkFailed` (both directions: zero delta on the action counter/client counter/webhook calls when `MarkFailed` errors, delta of exactly 1 on the action counter when it succeeds, read via before/after Prometheus deltas), `TestSweepUnroutableEngineActionStillUnguarded` (proves the fix did not over-apply to the fail-closed branch), and `TestReconcilerExhaustedCodeMatchesFrozenRegistry` (ties the `"reconciler_exhausted"` literal to plan 56-04's frozen `worker.CodeReconcilerExhausted` constant and `worker.AllFailureCodes()`, confirmed cycle-free via `go list -deps`).
- Reverting only the guard (temporarily, then re-applying it) reproduces the exact failure the plan's acceptance criteria demanded: `TestSweepExhaustedActionGuardedOnMarkFailed` fails with `action=exhausted delta = 1, want 0 when MarkFailed did not commit`.
- `56-05-PROOF.md`: Task 2's inversion run temporarily moved `internal/worker/runner.go`'s `finish()` emission calls ahead of the guarded `MarkFailed` commit and dropped the `ferr == nil` guards, reproducing the exact defect class PR #4 found in seven places; the pinning suite genuinely failed, with `TestPinnedEmissionNeverPrecedesCommit` and `TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook` both catching it (every other `TestPinned*` test stayed green, confirming the inversion is narrowly targeted). The tree was restored via `git checkout --` to byte-identical content.
- `56-05-PROOF.md` Task 3 section: verbatim output of the five PITFALLS.md-Pitfall-4-prescribed commands proving `git diff c6ccfd4..HEAD -- internal/metrics` is empty (both `--stat` and the full file diff), zero `[]string{` label-declaration lines appear anywhere in the phase's `internal/metrics` diff, and both `TestJobDurationLabelsPinned`/`TestQueueDepthLabelsPinned` pass.
- `go build ./... && go vet ./... && go test ./... -count=1` all green throughout, including a final whole-repo run after every task.

## Task Commits

Each task was committed atomically:

1. **Task 1: Guard RecordReconcilerAction(exhausted) on the MarkFailed result** - `0a31579` (fix)
2. **Task 2: Inversion run — prove the pinning suite catches emission-before-commit** - `3842067` (test)
3. **Task 3: Prove the metric-label zero-diff across the whole phase** - `3197eb4` (docs)

**Plan metadata:** (this commit, following)

## Files Created/Modified

- `internal/reconciler/reconciler.go` - Guarded `RecordReconcilerAction("exhausted")` and the exhaustion webhook enqueue on `ferr == nil`; extended the existing comment to name RUNR-02
- `internal/reconciler/reconciler_test.go` - Added `markFailedErr` error-injection field to `fakeStore`, plus three new tests and a local `readCounter` Prometheus-delta helper (mirrors `internal/worker/pinning_test.go`'s idiom)
- `.planning/phases/56-jobrunner-consolidation-structured-error-codes/56-05-PROOF.md` - Committed inversion-run transcript (Task 2) and metric-label zero-diff evidence (Task 3)

## Decisions Made

See `key-decisions` in frontmatter for full rationale on: (1) extending the RUNR-02 fix to also guard the exhaustion webhook enqueue on `ferr == nil` (not explicitly named in the plan's "Fix" paragraph, but required by the plan's own `<behavior>` block and Task 2's test description — Rule 1 bug fix), (2) the inversion-run mechanics (edit in place, capture, restore via `git checkout --`, never commit the inverted code), and (3) splitting `56-05-PROOF.md` into two atomic commits along the plan's task boundaries.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The exhaustion-path webhook enqueue had no `ferr == nil` guard, contradicting the plan's own `<behavior>` spec**
- **Found during:** Task 1, while writing `TestSweepExhaustedActionGuardedOnMarkFailed` per the action text's own description ("...assert a zero delta on `octoconv_reconciler_actions_total{action="exhausted"}`, a zero delta on `octoconv_client_job_outcomes_total`, **and zero webhook enqueues**")
- **Issue:** The plan's `<behavior>` block states: "MarkFailed returns an error: that counter does not move, octoconv_client_job_outcomes_total does not move, **no webhook is enqueued**." The current code's `if job != nil && job.CallbackURL != "" { EnqueueWebhookDeliver }` had no `ferr` condition at all — a `MarkFailed` failure would still fire a webhook telling the client the job failed, even though the transition never actually committed and the job row is unmodified. This is the same correctness defect as the RUNR-02 counter bug, just on a different side effect of the identical branch, and the plan's own comment two lines above it ("the failed status is already committed by MarkFailed above, so a failed enqueue must not undo it") already assumed `ferr == nil` without the code enforcing it.
- **Fix:** Added `ferr == nil` to the webhook enqueue condition (`if job != nil && ferr == nil && job.CallbackURL != ""`), extended the surrounding comment to explain why, and wrote `TestSweepExhaustedActionGuardedOnMarkFailed` to assert zero webhook calls when `MarkFailed` errors — exactly as Task 2's own test description specified.
- **Files modified:** `internal/reconciler/reconciler.go`
- **Verification:** `go test ./internal/reconciler/... -count=1 -v -run 'TestSweep|TestReconcilerExhausted'` — all pass, including the new webhook-zero-delta assertion; whole-repo `go test ./... -count=1` green.
- **Committed in:** `0a31579` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - Bug)
**Impact on plan:** The deviation closes a second, adjacent instance of the same RUNR-02 defect class within the identical branch the plan already targeted — no scope creep beyond `internal/reconciler/reconciler.go`, and it is required to satisfy the plan's own `<behavior>` block and Task 2's literal test description, not an independent addition.

## Issues Encountered

None beyond the deviation documented above, discovered and resolved via the plan's own verify step (writing the test the plan's action text specified, then observing it required a second guard to pass).

## Inversion-Run and Zero-Diff Evidence

See `.planning/phases/56-jobrunner-consolidation-structured-error-codes/56-05-PROOF.md` for the full committed transcript. Key excerpts:

- Reverting Task 1's guard temporarily reproduces the exact acceptance-criteria failure: `action=exhausted delta = 1, want 0 when MarkFailed did not commit` (`TestSweepExhaustedActionGuardedOnMarkFailed`, `reconciler_test.go:982`).
- The Task 2 inversion run's key failure lines: `EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]` (`TestPinnedEmissionNeverPrecedesCommit`, all seven engines' `/terminal` sub-tests) and `octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)` (`TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook`, all seven engines).
- `git diff c6ccfd4..HEAD -- internal/metrics` is byte-empty (0 bytes) at the end of this plan; `git diff c6ccfd4..HEAD --unified=0 -- internal/metrics | grep -E '\[\]string\{'` finds nothing.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- RUNR-02 and RUNR-03 are closed. `internal/reconciler/reconciler.go`'s exhaustion path now follows the identical `ferr == nil`-guard shape as `internal/worker/runner.go`'s `finish()` (plan 56-04) for every terminal-outcome side effect (metric counter AND webhook enqueue).
- ROADMAP SC4 (emission-before-commit structurally impossible) and SC3 (KEDA-pinned label invariance) are now demonstrated with committed evidence, not merely asserted — future phases/reviewers can point to `56-05-PROOF.md` rather than re-deriving the proof.
- No blockers for 56-06 (Go-to-doc sync test, consumes `worker.AllFailureCodes()`) or 56-07.

## Self-Check: PASSED

- FOUND: internal/reconciler/reconciler.go (modified, contains `if ferr == nil` guard around `RecordReconcilerAction("exhausted")`)
- FOUND: internal/reconciler/reconciler_test.go (modified, contains the three new tests)
- FOUND: .planning/phases/56-jobrunner-consolidation-structured-error-codes/56-05-PROOF.md
- FOUND commit 0a31579
- FOUND commit 3842067
- FOUND commit 3197eb4

---
*Phase: 56-jobrunner-consolidation-structured-error-codes*
*Completed: 2026-08-08*
