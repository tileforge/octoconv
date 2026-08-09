---
phase: 52-quota-enforcement-intra-class-fairness
verified: 2026-08-03T14:00:00Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 8/9
  gaps_closed:
    - "Daily byte/job quotas enforce a bounded, disclosed overshoot in EVERY valid operator configuration, not only when a concurrency cap is also set (D-15, ROADMAP SC2b's accepted-imprecision clause)"
  gaps_remaining: []
  regressions: []
deferred: []
---

# Phase 52: Quota Enforcement & Intra-Class Fairness Verification Report

**Phase Goal:** An operator can cap what a client may consume (concurrent jobs, daily volume, allowed engines), enforcement is fail-closed on every job-creation path and rolled out safely to already-integrated consumers, and the same admission mechanism gives a structural guarantee that one client cannot starve others within an engine class — all without touching queue topology or the KEDA contract.
**Verified:** 2026-08-03
**Status:** passed
**Re-verification:** Yes — after gap closure (plans 52-06..52-09)

## Independent Reproduction of the Fix (not taken on trust from SUMMARY.md or test names)

The prior verification's own live reproduction was: `max_daily_jobs = 2`, `max_concurrent = nil`,
50 sequential `Repo.Create` calls → 50/50 admitted (the daily cap enforced nothing because none
of the 50 jobs ever reached a terminal state).

I wrote and ran my own throwaway test (`internal/jobs/zz_verify_repro_test.go`, deleted
immediately after this run, never committed) reproducing the identical scenario against the
live `octoconv-db` Postgres instance (`localhost:5434`, `DATABASE_URL` sourced from `.env`
inline per command):

```go
func TestZZVerifyReproDailyOnlyNoConcurrencyCap(t *testing.T) {
	r := newTestRepo(t)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(2))
	// ... 50 sequential createFixture calls, counting admitted/rejected
}
```

First run (enforcement mode left at its DB default, `observe`) reproduced the OLD 50/50-admitted
result — but that is `observe` mode admitting-and-reporting by design (D-13), not evidence the
gap is still open. Once `setEnforcementMode(..., enforcementModeEnforce)` was added — matching
what the permanent regression test `TestDailyJobsLimitWithoutConcurrencyCap` already does — the
result flipped to **admitted=2, rejected=48**, exactly the closed-gap outcome:

```
zz_verify_repro_test.go:28: admitted=2 rejected=48 (want admitted=2, rejected=48)
--- PASS: TestZZVerifyReproDailyOnlyNoConcurrencyCap (0.14s)
```

The throwaway file was deleted after this run (`rm internal/jobs/zz_verify_repro_test.go`);
`git status --short internal/jobs/` confirms no residue. **Verdict: the gap is closed, verified
independently, not merely by re-running the permanent test suite plans 52-06..09 shipped.**

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1 — Operator can set per-client, per-engine limits; `NULL`/absent row both mean unlimited, never a sentinel; no existing client's behavior changes until an operator opts in | ✓ VERIFIED (re-checked, no regression) | `TestAdmitUnlimitedWhenNoLimitsRow`, `TestAdmitUnlimitedWhenAllColumnsNull` re-run live: PASS. Schema/CLI unchanged by gap closure except `set-limit`'s new engine guard (see truth for QUOT-01 below), confirmed by reading `internal/clients/limits.go` (untouched by 52-06..09's file lists). |
| 2 | SC2(a) — Every job-creation entry point (REST, MCP) enforces an identical, per-(client,engine) SERIALIZED check-and-admit, proven by a real cross-entry-point integration test | ✓ VERIFIED (re-checked, no regression) | `internal/mcpserver/quota_admission_test.go`'s `TestQuotaAdmission` re-run live: 5/5 subtests PASS (`RESTRejectsOverCap`, `MCPToolRejectsOverSameCap`, `ObserveModeAdmitsBothPaths`, `FairnessGuaranteeIsPerClientNotGlobal`, `ConcurrentBurstBoundedUploadWaste` — burst-waste log: "observed wasted uploads = 0 (bound N - K = 7)"). `internal/mcpserver/client.go` is untouched by the gap-closure plans (not in any of 52-06/07/08/09's modified-file lists) — confirmed no regression surface exists here. |
| 3 | SC2(b)-i — Authoritative admission is a reservation made before the S3 upload, with a release path if a later step fails | ⚠️ DEVIATION (disclosed & tested, carried forward — NOT re-litigated per instructions) | Unchanged: `admitLocked` still runs inside `Repo.Create`, after `s.storage.Upload`. `52-CONTEXT.md` D-07/D-08 unedited; the only edit near this area was a doc-comment rewording in `PreAdmit`'s comment (removing a stray `MAX_INPUT_BYTES` string per 52-06's own acceptance criteria) with no behavioral change. Same disclosed, measured residual as the prior verification (`ConcurrentBurstBoundedUploadWaste`). |
| 4 | SC2b — Admission reservations and completed usage are distinguishable; a job counted at admission and later completed is counted exactly once | ✓ VERIFIED (re-checked, extended — Cancel now also participates) | No reservation row is ever written; `client_usage_daily` is written exactly once per job, now at THREE terminal transitions (`done`/`failed`/`canceled` — migration 0014 + `Repo.Cancel`'s new `recordUsage` call). `TestCancelDoesNotDoubleCountAcrossInFlightAndRollup`, `TestInFlightAndTerminalUsageCountedExactlyOnce`, `TestCancelRecordsUsageExactlyOnce` all re-run live: PASS. |
| 5 | Daily byte/job quotas enforce a bounded, disclosed overshoot in EVERY valid operator configuration, not only when a concurrency cap is also set (D-15) | ✓ VERIFIED — **GAP CLOSED** | `evaluate()` (`internal/jobs/admission.go:205-285`) now sums the terminal `client_usage_daily` rollup PLUS in-flight (`queued`+`active`) admitted work, inside `admitLocked`'s existing `pg_advisory_xact_lock`-guarded transaction. Independently reproduced (see section above): `max_daily_jobs=2`, `max_concurrent=nil`, 50 sequential creates → 2 admitted / 48 rejected. Permanent tests `TestDailyJobsLimitWithoutConcurrencyCap`, `TestDailyBytesLimitWithoutConcurrencyCap`, `TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher` re-run live: PASS. The false "bounded by the concurrency cap: at most K × MAX_INPUT_BYTES" comment is gone (`grep -c MAX_INPUT_BYTES internal/jobs/admission.go` → 0, confirmed). `52-CONTEXT.md` D-15 carries a dated amendment (2026-08-03) superseding the false bound in place, original text left intact. |
| 6 | SC3 — A limit-exceeding request gets `429` + `Retry-After` + `code=quota_exceeded`, distinguishable from `rate_limited`, never `403`; an in-flight slot is released on EVERY terminal state | ✓ VERIFIED (re-checked — `canceled` now also writes the ledger; confirmed this does not break slot-release or introduce a double-count) | `TestPerClient_OverLimitReturns429WithRetryAfter` (rate_limited body) re-run: PASS. `TestTerminalStatusFreesSlotWithNoReleaseCode` re-run live with all three subtests (`MarkDone`/`MarkFailed`/`Cancel`): PASS — occupancy is still a live `COUNT` over `jobs.status IN ('queued','active')`, so `Cancel`'s new `recordUsage` call (which only touches `client_usage_daily`, never `jobs.status`) cannot affect slot-release; the status UPDATE that frees the slot is unchanged. |
| 7 | SC4 — Observe-only mode computes and exposes what would have been rejected without actually rejecting | ✓ VERIFIED (re-checked, no regression) | `TestObserveModeNeverRejects` and `TestQuotaAdmission/ObserveModeAdmitsBothPaths` re-run live: PASS. `enforcementModeObserve` remains the column default; `admitLocked`/`PreAdmit`'s gating logic around it is untouched by the gap-closure plans (only the in-flight term inside `evaluate` changed; the observe/enforce branch in `admitLocked`/`PreAdmit` is identical). The "at least one full quota window" rollout clause remains an operational/process claim outside codebase-only verification, same treatment as the prior verification. |
| 8 | SC5 — Per-client, per-engine concurrency cap gives a structural K-of-M guarantee, ZERO diff to any `scaledobject-*.yaml`, ZERO signature change to the reconciler's `enqueuer` interface | ✓ VERIFIED (independently re-reproduced from `d7ac603`, not trusted from 52-09's recorded output) | `git diff --name-only d7ac603..HEAD -- 'deploy/chart/octoconv/templates/scaledobject-*.yaml'` → 0; `git diff --name-only d7ac603..HEAD -- internal/queue/` → 0; `git diff --name-only d7ac603..HEAD -- internal/worker/` → 0; `git diff d7ac603..HEAD -- deploy/chart/octoconv/templates/ \| grep -c octoconv_queue_depth` → 0; `git diff d7ac603..HEAD -- internal/api/api.go \| grep -cE '^[+-].*Enqueue(Image\|Document\|HTML\|Audio\|AV\|OCR\|Pandoc)Convert'` → 0; `git diff --name-only d7ac603..HEAD -- internal/reconciler/reconciler.go` → 0 (whole file untouched). All 7 `scaledobject-*.yaml` files present. `TestQuotaAdmission/FairnessGuaranteeIsPerClientNotGlobal` re-run: PASS. |
| 9 | D-12 — MCP proxy carries `code` and `Retry-After` into the agent-visible tool error, API key redaction preserved | ✓ VERIFIED (re-checked, no regression) | `internal/mcpserver/client.go` untouched by 52-06..09 (confirmed via each plan's `key-files: modified` list and the whole-phase file inventory in 52-09's SUMMARY). `TestQuotaAdmission/MCPToolRejectsOverSameCap` re-run live: PASS. |

**Score:** 9/9 truths verified (truth #3 remains a disclosed, accepted deviation carried forward unchanged from the prior verification — not re-litigated per instructions; truth #5, the sole prior FAILED item, is now closed and independently re-proven).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/jobs/admission.go` | `evaluate()` charges in-flight + terminal usage; false bound comment removed | ✓ VERIFIED | Read in full; in-flight subqueries present (lines 236-246), literal `'queued'/'active'` status strings for index matching, no `created_at` predicate (documented, deliberate). `grep -c MAX_INPUT_BYTES` → 0. |
| `internal/jobs/admission_test.go` | New tests pinning the daily-only configuration | ✓ VERIFIED | `TestDailyJobsLimitWithoutConcurrencyCap`, `TestDailyBytesLimitWithoutConcurrencyCap`, `TestInFlightAndTerminalUsageCountedExactlyOnce`, `TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher`, `TestCanceledJobStillCountsAgainstDailyQuota`, `TestCancelDoesNotDoubleCountAcrossInFlightAndRollup`, `TestAdmissionHoldsUnderRepeatableReadDefault` all present and green live. |
| `internal/db/migrations/0014_client_usage_canceled.sql` | Widen `client_usage_daily.status` CHECK to include `canceled` | ✓ VERIFIED | Applied live (`schema_migrations` row confirmed, 14 rows total); `\d client_usage_daily` shows the widened constraint exactly as the migration states, no other schema change. |
| `internal/jobs/repo.go` | `Cancel` calls `recordUsage`; `Create`/`transition` pin `pgx.ReadCommitted` | ✓ VERIFIED | `Cancel` (line 267) calls `r.recordUsage(ctx, tx, id, StatusCanceled, 0)` inside the guarded transition; `Create` (line 137) and `transition` use `pgx.BeginTxFunc(..., pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, ...)`. |
| `internal/convert/convert.go` | `AllEngines()`/`IsKnownEngine()` as engine-class source of truth | ✓ VERIFIED | Both functions present, exact/case-sensitive, no `NormalizeFormat` call (deliberate, documented). `TestAllEnginesCoversEveryEngineClass` re-run: not directly re-run by name here but covered by full `internal/convert` suite pass (28.7s, all green) in the whole-tree run. |
| `cmd/manage-clients/main.go` | `set-limit` rejects unknown engine before any DB write; `clear-limit`/`show-limits` deliberately unvalidated | ✓ VERIFIED | `validateEngine` wired into `set-limit` (line 123) before any `parseOptional*`/`repo.SetLimit` call. Live repro: `set-limit ... images ...` → `invalid engine: "images" is not a known engine class (must be one of: image, document, html, audio, av, ocr, pandoc)`, exit 1, zero DB writes (independently re-run by me, matching 52-07's recorded output). |
| `.planning/REQUIREMENTS.md` | QUOT-01/QUOT-04 reconciled from Pending to Complete | ✓ VERIFIED | Lines 20-23, 87-90 all show `[x]`/`Complete` for QUOT-01..04, FAIR-01, FAIR-02. |
| `README.md` | Daily-cap and cancellation semantics corrected | ⚠️ PARTIAL | The overshoot bullet and cancel-charges-the-ledger bullet ARE corrected (matches shipped code). However, the section's OPENING sentence ("sit in front of `POST /v1/jobs` — enforced fail-closed before the upload touches storage") and the "seven known engine classes" hardcode were NOT corrected — these are exactly 52-REVIEW.md's WR-09 items 1 and 2, which 52-09's scope did not target (52-09 targeted the overshoot bullet and engine-validation bullets only). The README still self-contradicts: it claims fail-closed enforcement happens "before the upload touches storage" at the top of the section, then states "because the pre-upload gate is unlocked by design" further down. See Anti-Patterns. Not a code defect — a documentation-accuracy gap, non-blocking. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `internal/jobs/repo.go Create` | `internal/jobs/admission.go admitLocked` | first statement inside `pgx.BeginTxFunc` (now pinned `ReadCommitted`) | ✓ WIRED (re-checked) | Confirmed at `repo.go:137-147`. |
| `internal/jobs/repo.go Cancel` | `internal/jobs/usage.go recordUsage` | call inside `Cancel`'s guarded `transition` closure, after the status UPDATE | ✓ WIRED (new link, verified) | `repo.go:261-268`; live tests confirm exactly-once write and no interference with slot-release. |
| `cmd/manage-clients/main.go set-limit` | `internal/convert/convert.go IsKnownEngine` | `validateEngine` called immediately after reading `os.Args[3]`, before any DB call | ✓ WIRED (new link, verified) | Live repro above; zero DB writes on rejection. |
| `internal/jobs/admission.go evaluate` | `jobs`/`job_inputs` in-flight subqueries | two `COALESCE(...)` terms added to the existing daily-usage query, inside the advisory-locked transaction | ✓ WIRED (new link, verified) | Live reproduction (2/48 split) and permanent tests confirm this is read on every admission attempt, serialized under the same lock as before. |
| Reconciler advisory lock vs. quota advisory lock | key-space separation | one-bigint vs. two-int32 forms | ✓ WIRED (unchanged, re-confirmed) | `internal/reconciler/reconciler.go` has zero diff since `d7ac603` — untouched by gap closure. |

### Behavioral Spot-Checks (all re-run live by me during this verification)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build clean | `go build ./...` | no output | ✓ PASS |
| Vet clean | `go vet ./...` | no output | ✓ PASS |
| Independent gap repro (throwaway, deleted after run) | ad-hoc `TestZZVerifyReproDailyOnlyNoConcurrencyCap`, `max_daily_jobs=2`, `max_concurrent=nil`, enforce mode, 50 sequential creates | admitted=2, rejected=48 | ✓ PASS — gap closed |
| Permanent daily-only regression test | `go test ./internal/jobs/... -run TestDailyJobsLimitWithoutConcurrencyCap -v` | PASS | ✓ PASS |
| Cancel evasion path closed | `go test ./internal/jobs/... -run TestCanceledJobStillCountsAgainstDailyQuota\|TestCancelDoesNotDoubleCountAcrossInFlightAndRollup\|TestCancelRecordsUsageExactlyOnce -v` | PASS | ✓ PASS |
| READ COMMITTED pin holds under hostile default | `go test ./internal/jobs/... -run TestAdmissionHoldsUnderRepeatableReadDefault -v` | PASS | ✓ PASS |
| Slot-release unaffected by Cancel's new ledger write | `go test ./internal/jobs/... -run TestTerminalStatusFreesSlotWithNoReleaseCode -v` | PASS (MarkDone/MarkFailed/Cancel subtests) | ✓ PASS |
| Cross-entry-point REST+MCP proof | `go test ./internal/mcpserver/... -run TestQuotaAdmission -v` | PASS (5/5 subtests) | ✓ PASS |
| `rate_limited` 429 body shape | `go test ./internal/ratelimit/... -v` | PASS (3/3) | ✓ PASS |
| Metrics label-pinning (FAIR-02 guard) | `go test ./internal/metrics/... -run TestJobDurationLabelsPinned\|TestQueueDepthLabelsPinned -v` | PASS | ✓ PASS |
| Engine validation fail-closed | `go run ./cmd/manage-clients set-limit <id> images 5 - -` | rejected, exit 1, zero rows written | ✓ PASS |
| Migration 0014 applied, correct constraint | `psql \d client_usage_daily` | `CHECK (status = ANY (ARRAY['done','failed','canceled']))` | ✓ PASS |
| FAIR-02 zero-diff, independently re-run | `git diff --name-only d7ac603..HEAD -- 'deploy/chart/octoconv/templates/scaledobject-*.yaml'` etc. | 0 in every check | ✓ PASS |
| Whole-tree regression | `go test ./... -count=1` (DATABASE_URL set) | all packages `ok`, no failures | ✓ PASS |
| Race detector | `go test -race ./internal/jobs/ ./internal/mcpserver/ -count=1` | `ok`, no race | ✓ PASS |
| No debt markers in touched files | `grep -nE "TBD\|FIXME\|XXX"` across all 11 gap-closure files + README + REQUIREMENTS.md | 0 matches | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|--------------|--------|----------|
| QUOT-01 | 52-01, 52-03, 52-07 | Operator can set limits; `NULL`/absent = unlimited; engine validated fail-closed | ✓ SATISFIED | Schema + CLI + `validateEngine` guard, all re-verified live. `.planning/REQUIREMENTS.md` now correctly shows `[x]`/Complete (52-09 closed the documentation-sync gap). |
| QUOT-02 | 52-01, 52-03, 52-04, 52-05, 52-06, 52-08 | Fail-closed check on every job-creation path, in EVERY operator-reachable configuration | ✓ SATISFIED — **gap closed** | Daily-only configuration (`max_concurrent = nil`) now genuinely fail-closed, independently reproduced. Cross-entry-point serialization re-confirmed unaffected. |
| QUOT-03 | 52-02, 52-04, 52-05 | `429` + `Retry-After`, `quota_exceeded` distinguishable from `rate_limited`, never `403` | ✓ SATISFIED | Re-verified, no regression. |
| QUOT-04 | 52-01, 52-02, 52-03, 52-04, 52-06 | Observe-only mode computes/logs/metrics without rejecting | ✓ SATISFIED | Re-verified, no regression. `.planning/REQUIREMENTS.md` now `[x]`/Complete. |
| FAIR-01 | 52-03, 52-05 | Structural K-of-M concurrency ceiling per client per engine class | ✓ SATISFIED | Re-verified, no regression. |
| FAIR-02 | 52-02, 52-05, 52-09 | `octoconv_queue_depth`/PromQL/all 7 `ScaledObject`s unchanged | ✓ SATISFIED | Independently re-measured from `d7ac603` (not trusted from 52-09's recorded output) — zero diff confirmed across templates, `internal/queue/`, `internal/worker/`, both `Enqueuer`/`enqueuer` interfaces. |

**Orphaned requirements check:** `.planning/REQUIREMENTS.md`'s traceability table maps exactly QUOT-01..04, FAIR-01, FAIR-02 to Phase 52. Requirement IDs declared across all nine plans' frontmatter (`52-01`: QUOT-01,04; `52-02`: QUOT-03,04; `52-03`: QUOT-01,02,04,FAIR-01; `52-04`: QUOT-02,03,04; `52-05`: QUOT-02,03,FAIR-01,02; `52-06`: QUOT-02,04; `52-07`: QUOT-01; `52-08`: QUOT-02; `52-09`: QUOT-01,03,04,FAIR-01,02) cover exactly this set. No orphans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/jobs/admission.go` | 241-246, 258-271 | In-flight daily charge carries no time bound: a job stuck in `queued`/`active` (e.g. a lost enqueue with no healthy reconciler) charges `max_daily_jobs`/`max_daily_bytes` on EVERY subsequent UTC day, indefinitely, until it reaches a terminal state. If N stuck jobs equal a daily-only `max_daily_jobs = N`, the client is admitted zero further jobs on that engine for every day the jobs remain stuck, with no operator-side CLI remedy (only raw SQL; only the OWNING CLIENT can call `DELETE /v1/jobs/{id}`, confirmed via the ownership guard at `handlers.go:924-928`). This is 52-REVIEW.md's WR-01, and it is a genuine NEW behavior introduced by the very fix that closes truth #5 — before 52-06, a stuck job never charged daily budget at all. | ⚠️ Warning (not a blocker — see independent assessment below) | Availability/operability concern: quota enforcement now structurally depends on `cmd/webhook-worker`'s reconciler being deployed and healthy to self-heal a stuck client, and this new coupling is not documented in README/.env.example. |
| `README.md` | 241-242, 257 | Self-contradictory pre-upload gate claim (52-REVIEW.md WR-09, items 1 and 2): the section's opening line still claims limits are "enforced fail-closed before the upload touches storage," directly contradicted 16 lines later by "because the pre-upload gate is unlocked by design." The "seven known engine classes" wording is also still hardcoded rather than deriving from `convert.AllEngines()`. Neither was in 52-09's scope (which fixed only the overshoot/cancel/engine-validation bullets). | ⚠️ Warning | Documentation-accuracy gap; a reader sizing a quota off the opening line would wrongly conclude uploads can never outrun the cap. Does not affect shipped behavior, only its description. |
| `internal/jobs/admission.go` | 213-220, 242, 246 | No test/assertion pins that the in-flight predicate (`queued`,`active`) stays the exact complement of the statuses `recordUsage` writes; `jobs.StatusAwaitingUpload` is in the DB's status CHECK domain but in neither set (currently unreachable — no writer — so latent, not live). (52-REVIEW.md WR-02, unresolved.) | ⚠️ Warning (latent, non-blocking) | If a future two-phase-upload flow starts writing `awaiting_upload`, those jobs become invisible to quota accounting with nothing failing to signal the drift. |
| `internal/db/migrations/0014_client_usage_canceled.sql` | 44 | `DROP CONSTRAINT IF EXISTS` breaks this repo's own convention (every prior constraint swap uses a bare `DROP CONSTRAINT`); no test asserts the resulting CHECK exists exactly once. (52-REVIEW.md WR-03, unresolved.) | ⚠️ Warning | If the constraint's actual name ever differs from what was confirmed live, the migration silently no-ops and `DELETE /v1/jobs/{id}` starts 500ing with no signal. Verified live today that the name matches and the migration is correctly applied — this is a latent risk, not a current defect. |
| `internal/jobs/repo.go` | 267 vs `0014_client_usage_canceled.sql` | Accounting (`recordUsage`) can now abort `Cancel` via the `status` CHECK constraint, which two prior migrations (0011, 0013) explicitly argued against doing for `engine` on the same table ("accounting must never be able to break conversion"). (52-REVIEW.md WR-04, unresolved.) | ⚠️ Warning | Cancel is the only lever a client has to release quota it's being charged for (see WR-01 above); coupling it to a CHECK constraint that could abort is a sharper version of a risk this codebase has previously decided to avoid elsewhere. |
| `internal/presets/repo.go`, `internal/clients/repo.go` | various | Same READ COMMITTED dependency the admission fix pinned in `internal/jobs` was NOT applied to these two packages' identical read-modify-write shape. (52-REVIEW.md WR-05, unresolved, out of this phase's stated scope.) | ⚠️ Warning (out of scope) | Inconsistency, not a Phase 52 regression — these packages predate this phase and are not part of its admission-correctness surface. |

(WR-06/WR-07/WR-08(README part)/remaining minor test-hygiene findings from 52-REVIEW.md were read and are consistent with the code; they are test-quality/hardening concerns, not goal-blocking, and are not duplicated here in full.)

## Independent Assessment of 52-REVIEW.md WR-01 (per explicit instruction: reach my own verdict, don't inherit the reviewer's framing)

**Is this (a) a genuine new defect, (b) a pre-existing property made visible, or (c) acceptable fail-closed behavior?**

My verdict: **(a) and (c) together, not (b).** This is genuinely NEW behavior — before plan 52-06, an unfinished job consumed daily budget only if a concurrency cap happened to also be set (and even then, indirectly, via the concurrency check, not the daily one). After 52-06, ANY in-flight job counts against the daily cap regardless of configuration. There was no way to close the CR-01 gap (truth #5) without introducing SOME version of this: as long as an admitted-but-unfinished job's cost counts toward "how much has this client consumed today," a job that never finishes will consume that budget for as long as it remains unfinished. This is the direct, unavoidable, and disclosed price of making the daily cap real.

Whether it is *acceptable*: I judge yes, at the phase-goal level, for three reasons. First, it strengthens fail-closed enforcement rather than weakening it — the system never under-charges, which is the correct failure direction for a quota mechanism whose whole purpose is bounding consumption. Second, it self-heals in a correctly operated deployment: `RequeueStale`'s reconciler eventually exhausts recovery attempts and calls `MarkFailed` (a terminal transition that already existed before this phase), which both frees the in-flight slot and correctly charges the terminal ledger exactly once — the same accounting discipline every other terminal transition already follows. Third, the residual risk (indefinite lockout when `cmd/webhook-worker` is NOT deployed or unhealthy) is an operational dependency, not a security or correctness defect — it is analogous to any other reconciler-dependent self-healing property this codebase already has (stale-job recovery itself has always depended on the reconciler running).

What is a real, standing gap — and where I agree with the review, independently — is the ABSENCE of an operator-facing remedy (a `manage-clients`-level cancel/unstick command) and the absence of README/`.env.example` disclosure of this new coupling between quota availability and `cmd/webhook-worker`'s health. That is exactly what 52-REVIEW.md's WR-01 fix option (B) calls for and it remains undone. I classify this as a **non-blocking WARNING**, consistent with the review's own severity (0 blockers, 9 warnings) — it does not falsify any of the 9 must-have truths above, all of which are independently re-verified as holding. It IS worth carrying forward as recommended follow-up work, likely fitting naturally into Phase 53 (operator usage visibility), which this phase's own `affects:` metadata already points to.

### Human Verification Required

None. Every must-have truth is either directly demonstrated by an automated test re-run live
against the actual `octoconv-db` Postgres instance during this verification pass, an
independent throwaway reproduction I wrote and executed myself (not inherited from any prior
report), or a static repository-diff check (FAIR-02), all independently reproducible. SC4's
"runs for at least one full quota window" clause remains an operational/rollout claim outside
what codebase verification can assert, carried forward unchanged from the prior verification's
treatment.

### Gaps Summary

**No blocking gaps remain.** The single prior BLOCKER (truth #5 — daily quotas unenforceable
in the daily-only, no-concurrency-cap configuration) is closed and independently re-proven
live, not merely re-run from the permanent suite the gap-closure plans shipped. All eight
previously-verified truths were re-checked for regression (not assumed still true, per the
re-verification instructions) against the actual code paths the gap closure touched
(`admission.go`, `repo.go`, `usage.go`, the new migration) and found unaffected.

Five non-blocking WARNING-level anti-patterns remain open from the fresh code review
(`52-REVIEW.md`, 0 blockers / 9 warnings), the most consequential being WR-01 (indefinite
daily-quota lockout for a stuck job, with no operator CLI remedy) and a README section that
still self-contradicts its own pre-upload-gate claim (WR-09, items 1-2). None of these falsify
a must-have truth; they are recorded here as recommended follow-up work, most naturally fitting
Phase 53 (operator usage visibility), which this phase's `affects:` metadata already names.

**Phase goal achieved.** An operator can cap concurrent jobs, daily volume, and allowed engines
per client per engine class; enforcement is fail-closed on every job-creation path in every
operator-reachable configuration (including the one that was previously broken); rollout is
safe via observe-mode and a service-wide kill-switch; and the same admission mechanism gives
the structural K-of-M fairness guarantee — all with zero diff to queue topology or the KEDA
contract, independently re-measured from the same baseline the original verification used.

---

_Verified: 2026-08-03T14:00:00Z_
_Verifier: Claude (gsd-verifier)_
