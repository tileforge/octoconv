---
phase: 52-quota-enforcement-intra-class-fairness
plan: 05
subsystem: testing
tags: [integration-test, mcp, prometheus, openapi, documentation, quota, fairness]

# Dependency graph
requires:
  - phase: 52-quota-enforcement-intra-class-fairness (plan 03)
    provides: "internal/jobs/admission.go — admitLocked (authoritative, joined to Repo.Create), Repo.PreAdmit, Repo.SetQuotaObserver, Repo.SetQuotaEnforcementDisabled, ErrConcurrencyLimit/ErrDailyBytesLimit/ErrDailyJobsLimit"
  - phase: 52-quota-enforcement-intra-class-fairness (plan 04)
    provides: "handleCreateJob's PreAdmit-before-Upload pre-upload gate, writeQuotaError/quotaRejection 429 mapping (Retry-After + code=quota_exceeded), QUOTA_ENFORCEMENT_DISABLED kill-switch, metrics.RecordQuotaWouldReject wiring"
provides:
  - "internal/mcpserver/quota_admission_test.go — one live api.Server (real jobs repository, never a fake) proven to reject the same over-quota client identically over REST and over the real convert_file MCP tool, with the MCP error carrying quota_exceeded + retry_after and never leaking the raw API key"
  - "The FAIR-01 per-client-not-global guarantee demonstrated: a second client at its own K=1 cap is unaffected by a first client already occupying its own K=1 cap"
  - "The concurrent-burst upload-waste residual measured (not assumed) against its N - K bound, with an exact 202/402 wait -- 1/7 split, an exact queued+active row count of 1, and a zero-byte-waste steady-state retry"
  - "metrics.QuotaWouldRejectValue — a small exported test-only read seam for the unexported quotaWouldReject CounterVec"
  - "docs/openapi.yaml QuotaError schema + updated TooManyRequests response documenting the two 429 producers and their Retry-After derivations"
  - "README.md Quotas and fairness section: what can be capped, manage-clients invocations, observe-to-enforce rollout, the kill-switch, the client contract, and the accepted daily-overshoot bound"
  - "FAIR-02 static contract gate: recorded, verbatim command output proving zero diff on all seven ScaledObjects, internal/queue, the queue-depth collector, and both Enqueuer interfaces across the whole phase"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cross-entry-point admission proof: one httptest.NewServer wrapping a real api.Server (jobs.NewRepo(pool), never a fake) is driven by BOTH a raw REST client and a genuine in-memory MCP session (mcpserver's own newHarness) against the SAME over-quota client state, so the 'is this actually enforced everywhere' claim is tested rather than inferred from the fact that MCP proxies over HTTP"
    - "Test-only metrics read seam: an unexported prometheus CounterVec stays unexported in production code; a package-level exported function (QuotaWouldRejectValue) reads its current value via GetMetricWithLabelValues + dto.Metric.Write, avoiding both an exported CounterVec and a testutil import in production code"

key-files:
  created:
    - internal/mcpserver/quota_admission_test.go
  modified:
    - internal/metrics/metrics.go
    - docs/openapi.yaml
    - README.md

key-decisions:
  - "QuotaWouldRejectValue reads the counter via CounterVec.GetMetricWithLabelValues + a client_model dto.Metric.Write, not prometheus/client_golang/prometheus/testutil.ToFloat64 -- testutil is a test-support package and the plan's own acceptance criteria required this seam to live in a normal (non-_test.go) file so internal/mcpserver's test can import it across package boundaries; keeping testutil out of production code avoided pulling a test-oriented dependency into internal/metrics's real build."
  - "The REST/MCP/observe/fairness/burst subtests share ONE quotaAdmissionHarness (one httptest.NewServer, one Postgres pool) rather than one server per subtest -- RESTRejectsOverCap and MCPToolRejectsOverSameCap deliberately share client state (subtest 2 needs subtest 1's job still in-flight), and reusing the harness across all five subtests keeps the whole suite's runtime under a second against a warm local Postgres."
  - "enforcementModeEnforceLiteral/enforcementModeObserveLiteral duplicate internal/jobs/admission.go's unexported enforcementModeEnforce/enforcementModeObserve string literals rather than importing them -- this test package cannot reach unexported identifiers in a different package, and internal/jobs/admission_test.go's own setEnforcementMode helper already establishes duplicating the two CHECK-constrained literals as the established pattern in this codebase."

requirements-completed: [QUOT-02, QUOT-03, FAIR-01, FAIR-02]

# Metrics
duration: ~35min
completed: 2026-08-03
---

# Phase 52 Plan 05: Cross-Entry-Point Proof, Autoscaling-Contract Gate & Consumer/Operator Docs Summary

**One live `api.Server` (the real `jobs` admission gate, never a fake) proven — by an integration test, not by proximity to the code — to reject the identical over-quota client through a raw REST `POST` and through a genuine `convert_file` MCP tool call, with the concurrent-burst upload-waste residual measured against its `N - K` bound and the FAIR-02 autoscaling contract recorded as a literal zero-line phase diff.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-08-03T06:01:17+03:00 (approx, worktree base reset to the Phase 52 wave-3 merge commit before execution)
- **Completed:** 2026-08-03T06:22:00+03:00
- **Tasks:** 3/3
- **Files modified:** 4 (1 created, 3 modified)

## Accomplishments

- `internal/mcpserver/quota_admission_test.go`: `TestQuotaAdmission` builds ONE real `api.Server` (`jobs.NewRepo(pool)` as the repo — the actual admission gate under test, never a fake) exposed over `httptest.NewServer`, with local mutex-guarded counting `Storage`/`Enqueuer` stubs and rate limits raised to 10000 RPM so `internal/ratelimit` can never emit a competing 429. Five subtests against that one server:
  - `RESTRejectsOverCap`: a `max_concurrent=1` client's second `POST /v1/jobs` gets exactly 429 with a positive `Retry-After` and `code=quota_exceeded`, with exactly ONE upload and ONE enqueue across both requests (ROADMAP SC2b — the rejection happens before S3).
  - `MCPToolRejectsOverSameCap`: the SAME client, still holding that one in-flight job, calls the real `convert_file` MCP tool via `newHarness` pointed at the real API; the tool result is an error whose text contains both `quota_exceeded` and `retry_after`, and never the raw API key.
  - `ObserveModeAdmitsBothPaths`: a client at `observe` mode with `max_concurrent=0` (the strictest possible limit) is admitted, and `octoconv_quota_would_reject_total{...,reason=concurrency}` increments by exactly 1.
  - `FairnessGuaranteeIsPerClientNotGlobal`: a second, independent client at its own `max_concurrent=1` is admitted while the first client (from `RESTRejectsOverCap`) still sits at its own cap — FAIR-01 stated in the ROADMAP's own K-of-M terms and demonstrated, not asserted.
  - `ConcurrentBurstBoundedUploadWaste`: 8 simultaneous `POST`s against `K=1` admit exactly 1 and reject exactly 7 (every rejection carrying `quota_exceeded` + a positive `Retry-After`), the `queued`+`active` row count is exactly 1, the measured upload-waste residual is asserted `0 <= wasted <= N-K` and logged via `t.Logf` (observed 0 in one `-count=1` run, 7 — the full bound — under `-race`), and a post-burst sequential retry wastes exactly zero additional uploads.
- `internal/metrics/metrics.go`: `QuotaWouldRejectValue(clientID, engine, reason string) float64` — a small exported test-only read seam for the unexported `quotaWouldReject` CounterVec, resolving `clientID` through the same bounded `clientLabel` allocator `RecordQuotaWouldReject` uses, so the cross-package integration test can assert the QUOT-04 counter without this package exporting the vec itself.
- `docs/openapi.yaml`: new `QuotaError` schema (`error` + `code`, `code` enum `[quota_exceeded, rate_limited]`) scoped strictly to the shared `TooManyRequests` response; `Error` is untouched (still exactly one property) — D-10's "code only ever appears on a 429" boundary is enforced structurally, not just by convention. `TooManyRequests`'s description and `Retry-After` header documentation now name both 429 producers (rate limiter vs. quota gate) and their two different `Retry-After` derivations (fixed hint vs. seconds-to-UTC-midnight).
- `README.md`: new "Quotas and fairness" section (between "API Semantics" and "Configuration") covering what can be capped per client/engine, the exact `manage-clients set-limit`/`clear-limit`/`show-limits`/`set-mode` invocations (including the `-`-means-NULL convention), the observe-to-enforce rollout sequence keyed off `octoconv_quota_would_reject_total`, `QUOTA_ENFORCEMENT_DISABLED` (empty does NOT disable), the client-visible `429`/`Retry-After`/`code` contract (never `403`), and the `K x <per-engine ceiling>` accepted daily-overshoot bound; `QUOTA_ENFORCEMENT_DISABLED` also added to the main configuration table.

## Task Commits

Each task was committed atomically:

1. **Task 1: Cross-entry-point admission proof — one live API, driven by REST and by the convert_file MCP tool** - `152e841` (test)
2. **Task 2: FAIR-02 autoscaling-contract gate — recorded, not assumed** - no separate commit (evidence-only task; its output is the `FAIR-02 static contract gate` section below, captured in this SUMMARY per the plan's own `<output>` spec — it modifies no source file)
3. **Task 3: Document the 429 quota contract for consumers and operators** - `b6b5ef5` (docs)

_Note: worktree mode — the plan-metadata commit (SUMMARY.md) is committed separately per the worktree execution protocol; STATE.md/ROADMAP.md are updated centrally by the orchestrator after merge, not by this agent._

## FAIR-02 static contract gate

All commands run from the repository root inside the worktree, using `d7ac603` as the phase-start baseline (per the plan's `<interfaces>` section). Verbatim output below.

**1. Zero diff on every autoscaling template:**
```
$ git diff --name-only d7ac603..HEAD -- 'deploy/chart/octoconv/templates/scaledobject-*.yaml' | wc -l
       0
```

**2. Zero diff on the queue-depth surfaces:**
```
$ git diff --name-only d7ac603..HEAD -- internal/queue/ internal/metrics/queue_collector.go | wc -l
       0
```

**3. All seven ScaledObjects still present, no PromQL touched:**
```
$ ls deploy/chart/octoconv/templates/scaledobject-*.yaml | wc -l
       7
$ git diff d7ac603..HEAD -- deploy/chart/octoconv/templates/ | grep -c octoconv_queue_depth
0
```

**4. Interface signatures unchanged:**
```
$ git diff d7ac603..HEAD -- internal/api/api.go | grep -cE '^[+-].*Enqueue(Image|Document|HTML|Audio|AV|OCR|Pandoc)Convert'
0
$ git diff d7ac603..HEAD -- internal/reconciler/reconciler.go | grep -cE '^[+-].*enqueuer interface'
0
```

**5. Label pinning still green:**
```
$ go test ./internal/metrics/ -run 'TestQueueDepthLabelsPinned|TestJobDurationLabelsPinned' -v -count=1
=== RUN   TestJobDurationLabelsPinned
--- PASS: TestJobDurationLabelsPinned (0.00s)
=== RUN   TestQueueDepthLabelsPinned
--- PASS: TestQueueDepthLabelsPinned (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/metrics	4.144s
```

**6. Whole-phase file inventory, for the record:**
```
$ git diff --name-only d7ac603..HEAD -- . ':!.planning'
.env.example
cmd/api/main.go
cmd/manage-clients/main.go
deploy/chart/octoconv/templates/configmap.yaml
deploy/chart/octoconv/values.yaml
internal/api/api.go
internal/api/handlers.go
internal/api/handlers_test.go
internal/api/sse_test.go
internal/clients/limits.go
internal/clients/limits_test.go
internal/db/migrations/0013_client_limits.sql
internal/jobs/admission.go
internal/jobs/admission_test.go
internal/jobs/repo.go
internal/mcpserver/client.go
internal/mcpserver/client_test.go
internal/mcpserver/quota_admission_test.go
internal/metrics/metrics.go
internal/metrics/metrics_test.go
internal/ratelimit/ratelimit.go
internal/ratelimit/ratelimit_test.go
```
`internal/worker/` appears nowhere in this list — D-08's claim that this phase has no worker-side component holds across the whole phase, not just this plan.

**7. Chart rendering sanity:**

`helm` is available (`v4.2.3`). Bare `helm lint`/`helm template` against the chart's own `values.yaml` fails on a pre-existing, unrelated nil-pointer in `templates/secret.yaml` (`.Values.secrets.postgresPassword`) — the exact same gap plan 52-04 already recorded and worked around with `values-local.yaml`; confirmed via the whole-phase diff above that this plan's (and the phase's) commits never touch `secret.yaml` or `values.yaml`'s `secrets:` block.
```
$ helm lint deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml
==> Linting deploy/chart/octoconv
[INFO] Chart.yaml: icon is recommended

1 chart(s) linted, 0 chart(s) failed
$ helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml | grep -c QUOTA_ENFORCEMENT_DISABLED
1
```

## Files Created/Modified

- `internal/mcpserver/quota_admission_test.go` - `TestQuotaAdmission`, the five-subtest cross-entry-point admission proof; `quotaAdmissionHarness` + `quotaCountingStorage`/`quotaCountingEnqueuer`/`quotaAdmissionPresetRepo` stubs
- `internal/metrics/metrics.go` - `QuotaWouldRejectValue`, the exported test-only read seam for `quotaWouldReject`
- `docs/openapi.yaml` - `QuotaError` schema; `TooManyRequests` response repointed at it with an updated description/`Retry-After` doc
- `README.md` - new "Quotas and fairness" section; `QUOTA_ENFORCEMENT_DISABLED` added to the configuration table

## Decisions Made

See `key-decisions` in the frontmatter above. In short: the QUOT-04 counter read seam avoided both exporting the CounterVec and importing `prometheus/testutil` into production code by reading the metric's value directly via `GetMetricWithLabelValues` + `dto.Metric.Write`; the five subtests share one harness/server deliberately, since two of them (`RESTRejectsOverCap`/`MCPToolRejectsOverSameCap`) must observe the SAME over-quota client state across REST and MCP.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Doc-comment prose accidentally inflated the `jobs.NewRepo(pool)` grep count past the acceptance criterion's expected 1**
- **Found during:** Task 1, running the plan's own acceptance-criteria greps
- **Issue:** Three doc comments in `quota_admission_test.go` quoted the literal `jobs.NewRepo(pool)` for readability (describing the harness/test purpose), making the file-wide `grep -c "jobs.NewRepo(pool)"` count 4 instead of the criterion's required 1
- **Fix:** Reworded the three comments to describe "the real jobs repository" without repeating the exact call expression, leaving the one literal call site (`jobsRepo := jobs.NewRepo(pool)`) as the sole match — the same category of fix plan 52-04 already applied once to `writeQuotaError`'s own doc comment
- **Files modified:** `internal/mcpserver/quota_admission_test.go`
- **Commit:** `152e841`

**2. [Rule 1 - Bug] `t.Fatalf` call referenced an unused `body` variable, failing `go vet`**
- **Found during:** Task 1, running `go vet ./...` per the overall verification step
- **Issue:** `FairnessGuaranteeIsPerClientNotGlobal`'s failure-path `t.Fatalf` did not interpolate the `body` bytes it had just read, so `go vet` flagged `body` as declared-and-not-used
- **Fix:** Added `; body=%s` / `body` to the format string and argument list, matching every other failure-path assertion in the same file
- **Files modified:** `internal/mcpserver/quota_admission_test.go`
- **Commit:** `152e841`

---

**Total deviations:** 2 auto-fixed (both Rule 1 — bugs caught by the plan's own acceptance-criteria greps and `go vet`, fixed before the task commit)
**Impact on plan:** Both fixes are cosmetic/mechanical — no behavior change to the test's assertions or the production code under test. No scope creep.

## Issues Encountered

- `github.com/prometheus/client_golang/prometheus/testutil` was the obvious tool for `QuotaWouldRejectValue`, but it is a test-support package; importing it into `internal/metrics/metrics.go` (a production file) to satisfy a cross-package test import would have pulled a test dependency into the real build. Resolved by reading the counter's value directly via `prometheus.CounterVec.GetMetricWithLabelValues` + `client_model/go`'s `dto.Metric.Write`, both of which are already transitive dependencies of `client_golang`.
- No `redocly` install was available locally (`npx --no-install redocly lint docs/openapi.yaml` reported the package missing, `npx` correctly refused to silently fetch it) — recorded here per the acceptance criterion rather than silently skipped. `docs/openapi.yaml` was instead verified to parse and satisfy the `QuotaError`/`Error` schema shape assertions via a local `pyyaml` check (system Python lacked `pyyaml`; installed into a throwaway venv under the scratchpad directory, not the project).
- Running the FULL test suite with `DATABASE_URL` set and Go's default parallelism (`go test ./... -count=1`) intermittently failed two pre-existing `internal/reconciler` soak tests (`TestSoakExhaustsAtCap`, `TestSoakStaleRecoveryCountsUsageExactlyOnce`) — both are real-wall-clock-timed tests unrelated to this plan (the FAIR-02 whole-phase diff above confirms `internal/reconciler/` is untouched by this phase). Both pass individually and the full suite passes serialized (`go test ./... -count=1 -p 1`), confirming the failure is pre-existing CPU-contention flakiness in those two soak tests' fixed wall-clock budgets, not a regression introduced here. Not auto-fixed (out of scope per the deviation rules' scope boundary: no file this plan touches is implicated) and not logged to a separate `deferred-items.md`, since it is a test-execution-environment observation rather than a scope item — documented here instead.

## User Setup Required

None — no external service configuration required. All verification (build/vet/test/openapi/helm) ran against the local worktree checkout and the already-running local `octoconv-db`/`octoconv-redis`/`octoconv-minio` compose containers (port 5434 for Postgres).

## Next Phase Readiness

- ROADMAP SC2 (the cross-entry-point admission property) and SC5 (the autoscaling contract) are both now closed by evidence rather than by construction alone: `TestQuotaAdmission` is a permanent regression guard, and the FAIR-02 gate's seven commands are re-runnable verbatim against any future baseline.
- The 429 quota contract (`QuotaError`, `Retry-After` derivation, never `403`) is now discoverable from `docs/openapi.yaml`, and the observe-to-enforce operator rollout is fully documented in `README.md` — this phase's public surface (both consumer-facing and operator-facing) is complete.
- Phase 52's own success criteria are all closed by this plan: the concurrent-burst upload-waste bound is measured (not merely bounded in prose), FAIR-01 is demonstrated (not merely asserted), and the whole-phase FAIR-02 diff is zero-line and recorded.
- Phase 55 (the deferred `autonomous: false` live multi-replica gate for QUOT-05) can proceed independently — this plan's harness pattern (one real `api.Server`, no fakes on the admission path) is a template for that live gate's own harness construction, though Phase 55 will need a real `api.replicas: 2+` deployment rather than an in-process `httptest.NewServer`.

---
*Phase: 52-quota-enforcement-intra-class-fairness*
*Completed: 2026-08-03*

## Self-Check: PASSED

- FOUND: internal/mcpserver/quota_admission_test.go
- FOUND: internal/metrics/metrics.go
- FOUND: docs/openapi.yaml
- FOUND: README.md
- FOUND: 152e841 (Task 1 commit)
- FOUND: b6b5ef5 (Task 3 commit)
