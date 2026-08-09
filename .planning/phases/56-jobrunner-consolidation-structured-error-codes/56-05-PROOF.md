# Phase 56 Plan 05 — Proof Document

This document is the committed evidence for two ROADMAP claims that cannot be
established by writing code alone (56-CONTEXT.md D-10 step 3, PITFALLS.md
Pitfall 14):

1. **SC4** — "firing the terminal outcome before the guarded transition
   commits is structurally impossible" is proven by an inversion run: the
   emission is temporarily moved ahead of the commit and plan 56-01's frozen
   pinning suite is shown to FAIL. A suite that stays green under inversion
   proves nothing.
2. **SC3** — the pinned metric label sets (`octoconv_job_duration_seconds`,
   `octoconv_queue_depth`) are unchanged across the entire phase, proven by
   diff against the phase base commit rather than by running the pinning
   tests once at the end (PITFALLS.md Pitfall 4's method, mirroring v1.12's
   FAIR-02).

Phase base commit for every diff in this document: `c6ccfd4`.

---

## Task 2: Inversion run — emission-before-commit is caught by the pinning suite

### Step 1 — Baseline

Commands run (from the worktree root):

```
git status --porcelain
git rev-parse HEAD
go test ./internal/worker/... -count=1 -run TestPinned
```

Baseline HEAD (after this plan's Task 1 commit landed):

```
0a315797eefef2ed5ad74864fa0d36584ae90b1d
```

`git status --porcelain` was empty before the run started. Baseline test
output:

```
ok  	github.com/apaderin/octoconv/internal/worker	0.562s
```

### Step 2 — Invert `internal/worker/runner.go` only

`finish()` was temporarily edited so `metrics.RecordJobOutcome`,
`metrics.RecordClientJobOutcome` and `EnqueueWebhookDeliver` execute BEFORE
the `MarkFailed` call whose result guards them, with the `ferr == nil`
conditions dropped — reproducing the exact defect class PR #4 found in seven
places. Exact diff (`git diff -- internal/worker/runner.go` while inverted):

```diff
diff --git a/internal/worker/runner.go b/internal/worker/runner.go
index 6e1bb98..a7c9865 100644
--- a/internal/worker/runner.go
+++ b/internal/worker/runner.go
@@ -238,55 +238,18 @@ func (r *Runner) Handle(ctx context.Context, t *asynq.Task) error {
 // that single-site structure is what makes "fire before commit" impossible
 // (T-56-12), and plan 56-05's inversion run is the proof.
 func (r *Runner) finish(ctx context.Context, job *jobs.Job, status, code, message string, detail map[string]any, elapsed time.Duration) error {
-	var ferr error
-	if status == jobs.StatusFailed {
-		ferr = r.h.repo.MarkFailed(ctx, job.ID, code, message, detail, elapsed)
-	}
-	// jobs.StatusDone: ferr stays nil -- process() (or a test's processFn
-	// stub standing in for it) already committed MarkDone inside its own
-	// transaction (worker.go's process(), the final line) before Handle
-	// ever calls finish. Guarding the done branch on this same nil is what
-	// keeps every terminal outcome (failed or done) flowing through the
-	// identical ferr==nil gate below.
-
-	// Deliberately NOT guarded on ferr, preserving the pre-existing
-	// asymmetry documented at every terminal branch this Runner replaces
-	// and pinned by TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook:
-	// a lost Prometheus increment is tolerable and never retried. This is
-	// NOT the same durability contract as MarkFailed's client_usage_daily
-	// rollup (D-05), which is transactional and exactly-once -- the two
-	// calls sit on adjacent lines here but must not be read as equivalent.
-	// This is NOT the RUNR-02 defect (internal/reconciler/reconciler.go's
-	// entirely unguarded RecordReconcilerAction("exhausted")), which plan
-	// 56-05 fixes separately.
-	//
-	// r.d.Engine, not job.Engine: both are byte-identical in production
-	// (a job's persisted engine column always matches the descriptor its
-	// own asynq task type routed to), but r.d.Engine is the value this
-	// Runner is STRUCTURALLY bound to at construction (NewRunner(h, d)) --
-	// it needs no defensive nil/empty check and, unlike job.Engine, does
-	// not depend on every test fixture populating Job.Engine (frozen
-	// pinning_test.go's newPinJob deliberately does not, D-10 step 2).
+	// INVERTED FOR 56-05 TASK 2 PROOF ONLY -- emission moved ahead of the
+	// guarded commit and ferr==nil guards dropped, reproducing the exact
+	// defect class PR #4 found in seven places. Not for commit.
 	metrics.RecordJobOutcome(r.d.Engine, status, elapsed)
-
-	// Guarded on ferr: if MarkFailed did not commit (Postgres blip, or a
-	// concurrent transition won the guarded allow-list), the job is NOT
-	// actually in `status` yet. A retry or the reconciler will reach a
-	// terminal state later and increment then -- counting here too would
-	// double-count this client, or attribute an outcome that never
-	// happened.
-	if ferr == nil {
-		metrics.RecordClientJobOutcome(job.ClientID.String(), r.d.Engine, status)
+	metrics.RecordClientJobOutcome(job.ClientID.String(), r.d.Engine, status)
+	if job.CallbackURL != "" {
+		_ = r.h.enqueuer.EnqueueWebhookDeliver(ctx, job.ID)
 	}
 
-	// Postgres-first: enqueue the webhook ONLY once the transition is
-	// actually committed. If MarkFailed itself failed (Postgres blip), the
-	// job is still active -- a webhook now would re-read the row and
-	// deliver a status the contract never defines (WR-04); the reconciler
-	// will requeue the still-active job instead. A failed enqueue after a
-	// successful commit must not fail the conversion -- best-effort only.
-	if ferr == nil && job.CallbackURL != "" {
-		_ = r.h.enqueuer.EnqueueWebhookDeliver(ctx, job.ID)
+	var ferr error
+	if status == jobs.StatusFailed {
+		ferr = r.h.repo.MarkFailed(ctx, job.ID, code, message, detail, elapsed)
 	}
 	return ferr
 }
```

### Step 3 — Run the pinning suite under inversion (verbatim FAIL output)

Command run:

```
go test ./internal/worker/... -count=1 -run TestPinned -v
```

The run FAILED, exit code 1. Both `TestPinnedEmissionNeverPrecedesCommit` and
`TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook` are among the
failures, as required. Verbatim captured output (only the relevant PASS/FAIL
lines and failure messages are elided-free — every failing sub-test and its
assertion message is included verbatim below):

```
=== RUN   TestPinnedEmissionNeverPrecedesCommit
=== RUN   TestPinnedEmissionNeverPrecedesCommit/image/terminal
    pinning_test.go:730: EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]
=== RUN   TestPinnedEmissionNeverPrecedesCommit/image/success
=== RUN   TestPinnedEmissionNeverPrecedesCommit/document/terminal
    pinning_test.go:730: EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]
=== RUN   TestPinnedEmissionNeverPrecedesCommit/document/success
=== RUN   TestPinnedEmissionNeverPrecedesCommit/html/terminal
    pinning_test.go:730: EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]
=== RUN   TestPinnedEmissionNeverPrecedesCommit/html/success
=== RUN   TestPinnedEmissionNeverPrecedesCommit/audio/terminal
    pinning_test.go:730: EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]
=== RUN   TestPinnedEmissionNeverPrecedesCommit/audio/success
=== RUN   TestPinnedEmissionNeverPrecedesCommit/av/terminal
    pinning_test.go:730: EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]
=== RUN   TestPinnedEmissionNeverPrecedesCommit/av/success
=== RUN   TestPinnedEmissionNeverPrecedesCommit/ocr/terminal
    pinning_test.go:730: EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]
=== RUN   TestPinnedEmissionNeverPrecedesCommit/ocr/success
=== RUN   TestPinnedEmissionNeverPrecedesCommit/pandoc/terminal
    pinning_test.go:730: EnqueueWebhookDeliver appears before any MarkFailed/MarkDone commit token in [Get MarkActive EnqueueWebhookDeliver MarkFailed]
=== RUN   TestPinnedEmissionNeverPrecedesCommit/pandoc/success
--- FAIL: TestPinnedEmissionNeverPrecedesCommit (0.00s)
    --- FAIL: TestPinnedEmissionNeverPrecedesCommit/image/terminal (0.00s)
    --- PASS: TestPinnedEmissionNeverPrecedesCommit/image/success (0.00s)
    --- FAIL: TestPinnedEmissionNeverPrecedesCommit/document/terminal (0.00s)
    --- PASS: TestPinnedEmissionNeverPrecedesCommit/document/success (0.00s)
    --- FAIL: TestPinnedEmissionNeverPrecedesCommit/html/terminal (0.00s)
    --- PASS: TestPinnedEmissionNeverPrecedesCommit/html/success (0.00s)
    --- FAIL: TestPinnedEmissionNeverPrecedesCommit/audio/terminal (0.00s)
    --- PASS: TestPinnedEmissionNeverPrecedesCommit/audio/success (0.00s)
    --- FAIL: TestPinnedEmissionNeverPrecedesCommit/av/terminal (0.00s)
    --- PASS: TestPinnedEmissionNeverPrecedesCommit/av/success (0.00s)
    --- FAIL: TestPinnedEmissionNeverPrecedesCommit/ocr/terminal (0.00s)
    --- PASS: TestPinnedEmissionNeverPrecedesCommit/ocr/success (0.00s)
    --- FAIL: TestPinnedEmissionNeverPrecedesCommit/pandoc/terminal (0.00s)
    --- PASS: TestPinnedEmissionNeverPrecedesCommit/pandoc/success (0.00s)
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/image
    pinning_test.go:789: image: octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/document
    pinning_test.go:789: document: octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/html
    pinning_test.go:789: html: octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/audio
    pinning_test.go:789: audio: octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/av
    pinning_test.go:789: av: octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/ocr
    pinning_test.go:789: ocr: octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)
=== RUN   TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/pandoc
    pinning_test.go:789: pandoc: octoconv_client_job_outcomes_total delta = 1, want 0 (guarded on ferr==nil)
--- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook (0.01s)
    --- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/image (0.00s)
    --- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/document (0.00s)
    --- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/html (0.00s)
    --- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/audio (0.00s)
    --- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/av (0.00s)
    --- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/ocr (0.00s)
    --- FAIL: TestPinnedMarkFailedErrorSuppressesClientCounterAndWebhook/pandoc (0.00s)
FAIL
FAIL	github.com/apaderin/octoconv/internal/worker	1.873s
FAIL
```

Every other TestPinned* test (TestPinnedTerminalOutcomePerEngine,
TestPinnedFailureCodePerEngine, TestPinnedTransientOutcomePerEngine,
TestPinnedCanceledOutcomePerEngine, TestPinnedSuccessOutcomePerEngine,
TestPinnedUnparseablePayload, TestPinnedEmissionLabelSets,
TestPinnedOptsGuardPrecedesMarkActive, TestPinnedImageHasNoOptsGuard,
TestPinnedAudioStageAsymmetry, TestPinnedAVStageAsymmetry) still passed under
inversion — only the two tests whose entire purpose is to pin the
commit-before-emission ordering and the ferr-guard asymmetry caught the
defect, exactly as expected: the inversion is narrowly targeted at `finish()`
and does not touch opts-guard, stage-asymmetry, or failure-code-mapping
behavior.

### Step 4 — Restore

Commands run:

```
git checkout -- internal/worker/runner.go
git status --porcelain -- internal/worker
go test ./internal/worker/... -count=1 -run TestPinned
```

`git status --porcelain -- internal/worker` printed nothing after the
restore (confirmed by a byte-for-byte `diff` against the pre-inversion file
content as well, not just `git status`). Post-revert clean run output:

```
ok  	github.com/apaderin/octoconv/internal/worker	0.532s
```

The full worker package suite (`go test ./internal/worker/... -count=1`, no
`-run` filter) was also re-run on the restored tree and passed:

```
ok  	github.com/apaderin/octoconv/internal/worker	5.607s
```

### Reproducibility — exact commands, in order

```
git status --porcelain
git rev-parse HEAD
go test ./internal/worker/... -count=1 -run TestPinned
# (edit internal/worker/runner.go's finish() per the diff above)
go test ./internal/worker/... -count=1 -run TestPinned -v
git checkout -- internal/worker/runner.go
git status --porcelain -- internal/worker
go test ./internal/worker/... -count=1 -run TestPinned
go test ./internal/worker/... -count=1
```

### Conclusion — SC4

ROADMAP SC4 ("firing the terminal outcome before the guarded transition
commits is structurally impossible") is now **demonstrated, not asserted**.
The inversion run proves the pinning suite from plan 56-01
(`internal/worker/pinning_test.go`) is not merely present but actively
detects the exact defect class this phase exists to close — moving emission
ahead of the guarded commit and dropping the `ferr == nil` guard causes two
specific, correctly-targeted tests to fail with assertion messages that name
the precise violation (`EnqueueWebhookDeliver appears before any
MarkFailed/MarkDone commit token`, `octoconv_client_job_outcomes_total delta
= 1, want 0`). The working tree was restored to byte-identical content and
the full test suite is green.

---

## Task 3: Metric-label zero-diff evidence

This section proves ROADMAP SC3 (label sets unchanged) by diff against the
phase base commit `c6ccfd4`, per PITFALLS.md Pitfall 4's prescribed method
(mirroring v1.12's FAIR-02) — not by running the pinning tests once at the
end.

### Command 1 — `git diff c6ccfd4..HEAD --stat -- internal/metrics`

```
$ git diff c6ccfd4..HEAD --stat -- internal/metrics
```

Output: (empty — no files changed)

### Command 2 — `git diff c6ccfd4..HEAD -- internal/metrics/metrics.go`

```
$ git diff c6ccfd4..HEAD -- internal/metrics/metrics.go
```

Output: (empty)

### Command 3 — label-declaration-line grep across the phase's `internal/metrics` diff

```
$ git diff c6ccfd4..HEAD --unified=0 -- internal/metrics | grep -E '\[\]string\{' || echo "NO LABEL DECLARATION LINES IN DIFF"
```

Output:

```
NO LABEL DECLARATION LINES IN DIFF
```

This is the FAIR-02 method quoted in Pitfall 4: no `[]string{...}` label
declaration line appears anywhere in the phase's diff of `internal/metrics`,
which is both necessary and sufficient — an empty file-level diff (Command 1)
already implies this, and this command is the belt-and-suspenders check
Pitfall 4 prescribes running independently rather than inferring it from the
first two.

### Command 4 — pinned-label tests

```
$ go test ./internal/metrics/... -count=1 -run 'TestJobDurationLabelsPinned|TestQueueDepthLabelsPinned' -v
```

Output:

```
=== RUN   TestJobDurationLabelsPinned
--- PASS: TestJobDurationLabelsPinned (0.00s)
=== RUN   TestQueueDepthLabelsPinned
--- PASS: TestQueueDepthLabelsPinned (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/metrics	0.316s
```

### Command 5 — the phase's actual footprint (recorded for reviewer context)

```
$ git diff c6ccfd4..HEAD --stat -- internal/worker cmd internal/reconciler internal/api internal/errcode
```

Output:

```
 cmd/audio-worker/main.go                     |    2 +-
 cmd/av-worker/main.go                        |    2 +-
 cmd/chromium-worker/main.go                  |    2 +-
 cmd/document-worker/main.go                  |    2 +-
 cmd/ocr-worker/main.go                       |    2 +-
 cmd/pandoc-worker/main.go                    |    2 +-
 cmd/worker/main.go                           |    2 +-
 internal/api/errcode_envelope_test.go        |  325 +++++++++++++++++++++++++++
 internal/api/handlers.go                     |  139 ++++++------
 internal/api/handlers_test.go                |   36 +--
 internal/api/jobs_list_handler.go            |    9 +-
 internal/api/presets_handlers.go             |   37 ++--
 internal/api/sse.go                          |    9 +-
 internal/api/system_presets_handlers.go      |   39 ++--
 internal/api/system_presets_handlers_test.go |    5 +-
 internal/api/system_usage_handlers.go        |   11 +-
 internal/api/system_usage_handlers_test.go   |   15 +-
 internal/errcode/errcode.go                  |   71 ++++++
 internal/errcode/errcode_test.go             |   71 ++++++
 internal/reconciler/reconciler.go            |   24 +-
 internal/reconciler/reconciler_test.go       |  161 +++++++++++++-
 internal/worker/descriptors.go               |  182 +++++++++++++++
 internal/worker/pandoc_integration_test.go   |   14 +-
 internal/worker/pinning_test.go              | 1007 +++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++
 internal/worker/runner.go                    |  292 ++++++++++++++++++++++++
 internal/worker/runner_test.go               |  233 +++++++++++++++++++
 internal/worker/worker.go                    |  997 ++++++++++++++++------------------------------------------------------------------
 27 files changed, 2731 insertions(+), 960 deletions(-)
```

(This command's output is recorded verbatim above for reviewer context, per
the plan's action text; exact insertion/deletion counts are illustrative of
the phase's real footprint at the time this document was written and are not
themselves a pass/fail criterion — the `internal/metrics` zero-diff above is
the load-bearing assertion. `internal/api`/`internal/errcode` changes are
plans 56-01 through 56-03's work, already committed before this plan started
— the structured 4xx error-code contract this milestone's roadmap describes;
this plan (56-05) touches only `internal/reconciler` within that footprint,
plus the now-restored, untouched-on-disk `internal/worker/runner.go`.)

### Statement — ROADMAP SC3 is satisfied

`octoconv_job_duration_seconds` still carries exactly `{engine, status}` and
`octoconv_queue_depth` exactly `{queue, state}` (Commands 1-3 above: a
byte-for-byte empty diff against the phase base commit `c6ccfd4` for the
entire `internal/metrics` package, and specifically for `metrics.go` where
both label declarations live). The seven existing ScaledObjects' PromQL
triggers, which read `octoconv_queue_depth{queue="...",state="..."}`, are
therefore untouched by this phase. **No `operation` label was added to
either pinned metric** — this milestone (v1.13) introduces an `operation`
dimension elsewhere (the `jobs.operation` column and API surface, out of
this plan's scope), and the natural "let's add an `operation` label while
we're here" impulse that PROJECT.md's roadmap decisions specifically flag as
this phase's highest-blast-radius risk did not occur: Command 3's grep found
zero label-declaration lines in the entire phase's `internal/metrics` diff.

Both pinned-label tests (`TestJobDurationLabelsPinned`,
`TestQueueDepthLabelsPinned`) pass (Command 4), and the whole-repo suite is
green:

```
$ go test ./... -count=1
```

(See the plan SUMMARY for the full package-by-package output; every package
reports `ok`.)
