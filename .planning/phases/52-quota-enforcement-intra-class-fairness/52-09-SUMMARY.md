---
phase: 52-quota-enforcement-intra-class-fairness
plan: 09
subsystem: docs
tags: [documentation, requirements-reconciliation, regression-proof, fair-02]

# Dependency graph
requires:
  - phase: 52-quota-enforcement-intra-class-fairness (plans 01-08)
    provides: "shipped quota enforcement/fairness mechanism (admitLocked in-flight charge, Cancel usage ledger write, pinned READ COMMITTED, set-limit engine validation) that this plan documents and re-proves, changing zero source behavior"
provides:
  - ".planning/REQUIREMENTS.md's QUOT-01/QUOT-04 checkboxes and traceability rows reconciled from Pending to Complete, matching shipped reality and ROADMAP.md"
  - "README.md's Quotas and fairness section corrected: committed-plus-in-flight daily accounting (no more false K x per-engine-ceiling bound), cancel-charges-the-ledger rule, set-limit engine validation"
  - "a single recorded evidence block (this SUMMARY) covering the full regression surface (go build/vet/gofmt, whole-tree go test with and without DATABASE_URL, -race on internal/jobs+internal/mcpserver) plus the FAIR-02 zero-diff contract measured from d7ac603 across the gap-closure plans"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns: []

key-files:
  created: []
  modified:
    - .planning/REQUIREMENTS.md
    - README.md

key-decisions:
  - "No Go source file touched -- this plan is documentation reconciliation plus verification only, exactly as scoped; every claim in README's rewritten bullets was checked against the shipped internal/jobs/admission.go comment, internal/jobs/repo.go Cancel, and cmd/manage-clients/main.go validateEngine before being written"
  - "REQUIREMENTS.md's Coverage block (mapping counts, not completion counts) was inspected and confirmed unaffected by this plan's edit -- it counts 15 v1 requirements mapped to phases, which does not change when a requirement's completion status flips within an already-mapped phase"

requirements-completed: [QUOT-01, QUOT-04]

# Metrics
duration: ~15min (task commits span 13:16:22 -> 13:17:03 UTC+3, 2026-08-03, plus the regression run itself)
completed: 2026-08-03
---

# Phase 52 Plan 09: Documentation reconciliation and full regression re-proof (final gap-closure plan) Summary

**`.planning/REQUIREMENTS.md` and `README.md` now tell the same story the codebase does after plans 52-06/07/08 shipped, and the whole regression surface plus the FAIR-02 zero-diff autoscaling contract are re-proven with real, pasted command output measured from the same `d7ac603` baseline the original verification used.**

## Performance

- **Duration:** ~15 min for the two documentation task commits; the full evidence-gathering run (Task 3) took longer in wall-clock terms (whole-tree `go test` twice, `-race`, and 15+ verification commands) but produced no additional commit — Task 3's only output is this SUMMARY.
- **Tasks:** 3 completed
- **Files modified:** 2 (`.planning/REQUIREMENTS.md`, `README.md`)

## Accomplishments

- `.planning/REQUIREMENTS.md`: QUOT-01 and QUOT-04 flipped from `- [ ]` to `- [x]` in the checkbox list, and their traceability-table rows flipped from `Pending` to `Complete`. Exactly 4 lines changed (`git diff --stat` confirms `4 insertions(+), 4 deletions(-)`); QUOT-05 (Phase 55), OPSV-01/02 (Phase 53), and CAPA-01 (Phase 54) are untouched; the requirement wording itself is byte-identical (only the checkbox/status-cell marker changed).
- The Coverage block at the end of `.planning/REQUIREMENTS.md` was read, not assumed: it counts "v1 requirements: 15 total" / "Mapped to phases: 15" / "Unmapped: 0" — mapping counts, not completion counts — so this edit correctly does not move any of those three numbers.
- `README.md`'s "Quotas and fairness" section rewritten in place: the false "Accepted daily overshoot... `K × <per-engine upload ceiling>`" bullet (a bound plan 52-06 deleted from the code) is replaced with the actual committed-plus-in-flight daily accounting rule, the cancel-charges-the-ledger rule (plan 52-08), and the disclosed-and-measured pre-upload burst residual. Two new sentences next to the `manage-clients` examples document `set-limit`'s fail-closed engine validation and `clear-limit`/`show-limits`'s deliberate non-validation (plan 52-07). No other section of README.md was touched; both diff hunks fall entirely within lines 239-293 (the "Quotas and fairness" section, which spans lines 239-293).
- Full regression suite re-run against a live Postgres (`postgres://octo:octo-pass@localhost:5434/octo_db`, reachable via the pre-existing `gsh-service-postgres-1` container on the standard project port) and recorded verbatim below.
- FAIR-02's zero-diff contract re-measured from `d7ac603` — the same baseline plan 52-05 and `52-VERIFICATION.md` used — across all of plans 52-06 through 52-09: zero diff on all seven `ScaledObject` templates, `internal/queue/`, the queue-depth collector, `internal/worker/`, and both `Enqueuer` interface signatures.

## Task Commits

Each task was committed atomically:

1. **Task 1: Reconcile REQUIREMENTS.md with shipped reality** - `83aeb95` (docs)
2. **Task 2: Correct the operator documentation for what the daily cap now does** - `edd6d7a` (docs)
3. **Task 3: Full regression run and FAIR-02 zero-diff re-proof, recorded verbatim** - no separate commit (evidence recorded in this SUMMARY only, per the plan's own `files_modified` scoping this task's output to the SUMMARY)

_No separate plan-metadata commit was made for STATE.md/ROADMAP.md — this is a worktree-isolated executor; the orchestrator owns those writes after the wave completes._

## Files Created/Modified

- `.planning/REQUIREMENTS.md` - QUOT-01/QUOT-04 checkbox + traceability-table status reconciled from Pending to Complete; nothing else changed
- `README.md` - "Quotas and fairness" section: overshoot bullet replaced with committed-plus-in-flight accounting + cancel-charge rule; two new sentences on `set-limit` engine validation

## Decisions Made

- The rewritten README overshoot bullet was written from the shipped `internal/jobs/admission.go` comment (quoted side-by-side below), not paraphrased from this plan's own prose, per the plan's explicit instruction.
- Task 3 produces no source change and no separate commit — its entire contribution is the verbatim evidence recorded in this SUMMARY, matching the plan's own `files_modified: [.planning/phases/.../52-09-SUMMARY.md]` for that task.

## README bullet vs. shipped code, quoted side by side

**README (this plan, "Quotas and fairness" section):**
> the daily byte and job-count caps compare the request against today's committed usage **plus** all of the client's currently in-flight (`queued`+`active`) jobs on that engine, so the cap holds whether or not a concurrency cap is also set — there is no `K × <per-engine ceiling>` tolerance to size around, because that tolerance no longer exists. A canceled job is charged to the daily ledger for the input bytes and job count it consumed — canceling does not refund the day's budget

**`internal/jobs/admission.go`'s `evaluate` comment (shipped, plan 52-06/52-08):**
> this check now compares today's committed `client_usage_daily` rollup PLUS all currently in-flight (queued+active) admitted work for this (client, engine) against the limit, including the incoming request's own contribution.
>
> Why there is no double count: a job has exactly one status at any instant; client_usage_daily rows exist only for the terminal statuses recordUsage writes (usage.go); queued/active jobs are never in the rollup.

Both describe the same rule: committed rollup + in-flight (queued+active), evaluated against the same limit, exactly once. The README's "cancel does not refund the day's budget" claim is backed by `internal/jobs/repo.go`'s `Cancel` (line 267): `return r.recordUsage(ctx, tx, id, StatusCanceled, 0)`, inside the same guarded transition that flips the job to `canceled`.

## Deviations from Plan

None — plan executed exactly as written. Both documentation edits are confined to their named files/sections; no Go source file was touched; no package was installed.

## Issues Encountered

- No `.env` file exists in this worktree (it is git-ignored and not copied into worktrees), but a live Postgres instance was already reachable at `postgres://octo:octo-pass@localhost:5434/octo_db` via the pre-existing `gsh-service-postgres-1` docker container on the project's standard non-conflicting port. All `DATABASE_URL`-scoped commands below were run with that DSN set inline per command, matching `.env.example`'s documented default.

---

## Regression suite

**1. `go build ./...`, `go vet ./...`, `gofmt -l .` — all three, exit 0, no output:**

```
$ go build ./...
$ echo BUILD_EXIT=$?
BUILD_EXIT=0

$ go vet ./...
$ echo VET_EXIT=$?
VET_EXIT=0

$ gofmt -l .
$ echo GOFMT_EXIT=$?
GOFMT_EXIT=0
```

**2. `DATABASE_URL=... go test ./internal/jobs/... ./internal/clients/... ./internal/mcpserver/... ./internal/metrics/... ./internal/ratelimit/... -count=1`:**

```
ok  	github.com/apaderin/octoconv/internal/jobs	4.590s
ok  	github.com/apaderin/octoconv/internal/clients	1.655s
ok  	github.com/apaderin/octoconv/internal/mcpserver	1.283s
ok  	github.com/apaderin/octoconv/internal/metrics	1.576s
ok  	github.com/apaderin/octoconv/internal/ratelimit	1.035s
```
Exit 0.

**3. `DATABASE_URL=... go test ./... -count=1` (whole tree):**

```
?   	github.com/apaderin/octoconv/cmd/api	[no test files]
ok  	github.com/apaderin/octoconv/cmd/audio-worker	0.936s
?   	github.com/apaderin/octoconv/cmd/av-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/chromium-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/document-worker	[no test files]
ok  	github.com/apaderin/octoconv/cmd/manage-clients	1.213s
?   	github.com/apaderin/octoconv/cmd/manage-presets	[no test files]
ok  	github.com/apaderin/octoconv/cmd/mcp-http	1.681s
?   	github.com/apaderin/octoconv/cmd/mcp-server	[no test files]
?   	github.com/apaderin/octoconv/cmd/migrate	[no test files]
ok  	github.com/apaderin/octoconv/cmd/ocr-worker	1.903s
?   	github.com/apaderin/octoconv/cmd/pandoc-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/webhook-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/worker	[no test files]
ok  	github.com/apaderin/octoconv/internal/api	2.232s
ok  	github.com/apaderin/octoconv/internal/auth	2.361s
ok  	github.com/apaderin/octoconv/internal/cancel	2.635s
ok  	github.com/apaderin/octoconv/internal/clients	3.005s
ok  	github.com/apaderin/octoconv/internal/convert	25.507s
ok  	github.com/apaderin/octoconv/internal/db	2.962s
ok  	github.com/apaderin/octoconv/internal/e2e	3.198s
ok  	github.com/apaderin/octoconv/internal/jobs	6.646s
ok  	github.com/apaderin/octoconv/internal/mcpserver	4.132s
ok  	github.com/apaderin/octoconv/internal/metrics	4.086s
ok  	github.com/apaderin/octoconv/internal/presets	4.588s
ok  	github.com/apaderin/octoconv/internal/queue	4.371s
ok  	github.com/apaderin/octoconv/internal/ratelimit	4.371s
ok  	github.com/apaderin/octoconv/internal/reconciler	11.489s
ok  	github.com/apaderin/octoconv/internal/storage	4.401s
ok  	github.com/apaderin/octoconv/internal/webhook	4.422s
ok  	github.com/apaderin/octoconv/internal/worker	7.118s
```
Exit 0.

**4. `DATABASE_URL=... go test -race ./internal/jobs/ ./internal/mcpserver/ -count=1`:**

```
ok  	github.com/apaderin/octoconv/internal/jobs	6.125s
ok  	github.com/apaderin/octoconv/internal/mcpserver	2.622s
```
Exit 0.

**5. `go test ./... -count=1` with `DATABASE_URL` unset:**

```
?   	github.com/apaderin/octoconv/cmd/api	[no test files]
ok  	github.com/apaderin/octoconv/cmd/audio-worker	0.518s
?   	github.com/apaderin/octoconv/cmd/av-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/chromium-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/document-worker	[no test files]
ok  	github.com/apaderin/octoconv/cmd/manage-clients	0.710s
?   	github.com/apaderin/octoconv/cmd/manage-presets	[no test files]
ok  	github.com/apaderin/octoconv/cmd/mcp-http	1.069s
?   	github.com/apaderin/octoconv/cmd/mcp-server	[no test files]
?   	github.com/apaderin/octoconv/cmd/migrate	[no test files]
ok  	github.com/apaderin/octoconv/cmd/ocr-worker	0.929s
?   	github.com/apaderin/octoconv/cmd/pandoc-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/webhook-worker	[no test files]
?   	github.com/apaderin/octoconv/cmd/worker	[no test files]
ok  	github.com/apaderin/octoconv/internal/api	1.667s
ok  	github.com/apaderin/octoconv/internal/auth	1.753s
ok  	github.com/apaderin/octoconv/internal/cancel	1.168s
ok  	github.com/apaderin/octoconv/internal/clients	2.105s
ok  	github.com/apaderin/octoconv/internal/convert	24.483s
ok  	github.com/apaderin/octoconv/internal/db	2.693s
ok  	github.com/apaderin/octoconv/internal/e2e	2.966s
ok  	github.com/apaderin/octoconv/internal/jobs	3.220s
ok  	github.com/apaderin/octoconv/internal/mcpserver	3.690s
ok  	github.com/apaderin/octoconv/internal/metrics	3.635s
ok  	github.com/apaderin/octoconv/internal/presets	3.575s
ok  	github.com/apaderin/octoconv/internal/queue	3.711s
ok  	github.com/apaderin/octoconv/internal/ratelimit	3.465s
ok  	github.com/apaderin/octoconv/internal/reconciler	3.917s
ok  	github.com/apaderin/octoconv/internal/storage	3.737s
ok  	github.com/apaderin/octoconv/internal/webhook	3.671s
ok  	github.com/apaderin/octoconv/internal/worker	7.631s
```
Exit 0 — every DB-gated suite skips/passes cleanly with no hard dependency on `DATABASE_URL` being set, confirming no new test introduced a hard DB requirement.

## Gap repro, inverted

**6. `DATABASE_URL=... go test ./internal/jobs/ -run TestDailyJobsLimitWithoutConcurrencyCap -v -count=1`:**

```
=== RUN   TestDailyJobsLimitWithoutConcurrencyCap
--- PASS: TestDailyJobsLimitWithoutConcurrencyCap (0.12s)
PASS
ok  	github.com/apaderin/octoconv/internal/jobs	0.540s
```

**Before/after mapping:** with `max_daily_jobs = 2` and `max_concurrent = nil`, 50 sequential `Repo.Create` calls: **before** plan 52-06 (the verifier's live reproduction, `52-VERIFICATION.md` truth #5), all 50 of 50 were admitted (the daily cap enforced nothing with no concurrency cap set). **Now**, this same test asserts exactly 2 admitted and 48 rejected — the gap is closed.

**7. `DATABASE_URL=... go test ./internal/jobs/ -run 'TestInFlightAndTerminalUsageCountedExactlyOnce|TestCancelDoesNotDoubleCountAcrossInFlightAndRollup' -v -count=1`:**

```
=== RUN   TestInFlightAndTerminalUsageCountedExactlyOnce
--- PASS: TestInFlightAndTerminalUsageCountedExactlyOnce (0.07s)
=== RUN   TestCancelDoesNotDoubleCountAcrossInFlightAndRollup
--- PASS: TestCancelDoesNotDoubleCountAcrossInFlightAndRollup (0.05s)
PASS
ok  	github.com/apaderin/octoconv/internal/jobs	0.546s
```

**8. `go test ./cmd/manage-clients/ -run TestValidateEngine -v -count=1`** (no database required):

```
=== RUN   TestValidateEngine
--- PASS: TestValidateEngine (0.00s)
=== RUN   TestValidateEngineAcceptsExactlyKnownSet
--- PASS: TestValidateEngineAcceptsExactlyKnownSet (0.00s)
PASS
ok  	github.com/apaderin/octoconv/cmd/manage-clients	0.359s
```

## FAIR-02 static contract gate

Baseline: `d7ac603` — the same commit `52-05` and `52-VERIFICATION.md` used, so this measures the whole phase including the gap-closure plans (52-06 through this plan, 52-09).

**9. `git diff --name-only d7ac603..HEAD -- 'deploy/chart/octoconv/templates/scaledobject-*.yaml' | wc -l`:**
```
0
```

**10. `git diff --name-only d7ac603..HEAD -- internal/queue/ internal/metrics/queue_collector.go | wc -l`:**
```
0
```

**11. `ls deploy/chart/octoconv/templates/scaledobject-*.yaml | wc -l`:**
```
7
```
`git diff d7ac603..HEAD -- deploy/chart/octoconv/templates/ | grep -c octoconv_queue_depth`:
```
0
```

**12.** `git diff d7ac603..HEAD -- internal/api/api.go | grep -cE '^[+-].*Enqueue(Image|Document|HTML|Audio|AV|OCR|Pandoc)Convert'`:
```
0
```
`git diff d7ac603..HEAD -- internal/reconciler/reconciler.go | grep -cE '^[+-].*enqueuer interface'`:
```
0
```

**13. `git diff --name-only d7ac603..HEAD -- internal/worker/ | wc -l`:**
```
0
```
D-08's claim that this phase has no worker-side component still holds after the gap-closure plans.

**14. `go test ./internal/metrics/ -run 'TestQueueDepthLabelsPinned|TestJobDurationLabelsPinned' -v -count=1`:**
```
=== RUN   TestJobDurationLabelsPinned
--- PASS: TestJobDurationLabelsPinned (0.00s)
=== RUN   TestQueueDepthLabelsPinned
--- PASS: TestQueueDepthLabelsPinned (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/metrics	0.635s
```

**15. `git diff --name-only d7ac603..HEAD -- . ':!.planning'`** — the whole-phase (52-01 through 52-09) file inventory outside `.planning/`:
```
.env.example
README.md
cmd/api/main.go
cmd/manage-clients/main.go
cmd/manage-clients/main_test.go
deploy/chart/octoconv/templates/configmap.yaml
deploy/chart/octoconv/values.yaml
docs/openapi.yaml
internal/api/api.go
internal/api/handlers.go
internal/api/handlers_test.go
internal/api/sse_test.go
internal/clients/limits.go
internal/clients/limits_test.go
internal/convert/convert.go
internal/convert/convert_test.go
internal/db/migrations/0013_client_limits.sql
internal/db/migrations/0014_client_usage_canceled.sql
internal/jobs/admission.go
internal/jobs/admission_test.go
internal/jobs/repo.go
internal/jobs/repo_test.go
internal/jobs/usage.go
internal/mcpserver/client.go
internal/mcpserver/client_test.go
internal/mcpserver/quota_admission_test.go
internal/metrics/metrics.go
internal/metrics/metrics_test.go
internal/ratelimit/ratelimit.go
internal/ratelimit/ratelimit_test.go
```
None of `internal/queue/`, `internal/worker/`, or `deploy/chart/octoconv/templates/scaledobject-*.yaml` appear — consistent with checks 9/10/13 above. `internal/api/api.go` and `internal/reconciler/reconciler.go` (the latter not even listed here, meaning zero diff at the whole-file level too) changed only outside the `Enqueuer`/`enqueuer` interface declarations, per checks 12a/12b.

## Migration state

**16. `psql "$DATABASE_URL" -c "SELECT version FROM schema_migrations ORDER BY version"`:**

```
                   version
----------------------------------------------
 0001_init.sql
 0002_client_api_keys.sql
 0003_webhook_dead_letter.sql
 0004_webhook_deliveries_job_idx.sql
 0005_html_engine.sql
 0006_audio_engine.sql
 0007_job_progress.sql
 0008_progress_webhook.sql
 0009_ocr_engine.sql
 0010_pandoc_engine.sql
 0011_client_usage_daily.sql
 0012_output_identity_and_retention_index.sql
 0013_client_limits.sql
 0014_client_usage_canceled.sql
(14 rows)
```

0014 is applied and no earlier version is missing.

## Known Stubs

None — this plan touched only markdown documentation; no code paths, no hardcoded empty values, no placeholder UI.

## Threat Flags

None — no new network endpoint, auth path, file access pattern, or schema change was introduced by this plan; it edits two documentation files and runs read-only verification commands.

## Next Phase Readiness

- The written record (`.planning/REQUIREMENTS.md`, `README.md`) and the shipped code now agree. A future phase planning from `REQUIREMENTS.md` will read QUOT-01/QUOT-04 as `Complete`, matching `ROADMAP.md` and `52-VERIFICATION.md`.
- Every gate `52-VERIFICATION.md` (or a re-verification pass) would re-run is recorded here with real, pasted output, including the FAIR-02 zero-diff contract measured across all of Phase 52's gap-closure work from the same `d7ac603` baseline the original verification used.
- No blocker for Phase 53 (operator usage visibility) or Phase 55 (multi-replica live proof) — neither this plan's changes (documentation only) touch either phase's structural surface.

## Self-Check: PASSED

- FOUND: `.planning/phases/52-quota-enforcement-intra-class-fairness/52-09-SUMMARY.md`
- FOUND: `83aeb95` (Task 1 commit)
- FOUND: `edd6d7a` (Task 2 commit)

---
*Phase: 52-quota-enforcement-intra-class-fairness*
*Completed: 2026-08-03*
