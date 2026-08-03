---
phase: 52-quota-enforcement-intra-class-fairness
verified: 2026-08-03T07:15:00Z
status: gaps_found
score: 8/9 must-haves verified
overrides_applied: 0
gaps:
  - truth: "Daily byte/job quotas enforce a bounded, disclosed overshoot in EVERY valid operator configuration, not only when a concurrency cap is also set (D-15, ROADMAP SC2b's accepted-imprecision clause)"
    status: failed
    reason: >
      evaluate()'s daily check (internal/jobs/admission.go:205-240) reads
      client_usage_daily, which is written only at the terminal transition.
      The in-code comment and CONTEXT.md D-15 both claim the resulting
      overshoot "is bounded by the concurrency cap: at most K x
      MAX_INPUT_BYTES" -- but max_concurrent is an independently nullable
      column (0013_client_limits.sql:20-33) and cmd/manage-clients exposes
      exactly that configuration (`set-limit <id> image - <bytes> <jobs>`).
      When max_concurrent is NULL, K is unbounded, so the documented bound
      does not hold. Independently reproduced (not merely inherited from
      52-REVIEW.md CR-01): a throwaway test in internal/jobs granting only
      max_daily_jobs=2 with max_concurrent=nil admitted 50/50 sequential
      job-creation attempts, because none of the 50 reach a terminal state
      and client_usage_daily never advances. The unit test that is supposed
      to pin D-15 (TestOvershootBoundedByConcurrencyCap,
      admission_test.go:456) only exercises the case where max_concurrent
      IS set, so it cannot and does not catch this. Net effect: a
      supported, operator-reachable, undocumented-as-broken configuration
      (daily-only limits, no concurrency cap) makes QUOT-02's "fail-closed
      on every path" claim false for that configuration -- the check runs,
      but it rejects nothing until a job finishes.
    artifacts:
      - path: "internal/jobs/admission.go"
        issue: "evaluate()'s daily-cap comment (lines 205-221) asserts a bound that is false when st.maxConcurrent == nil; no code path prevents or flags that configuration"
      - path: "internal/clients/limits.go"
        issue: "SetLimit performs no cross-field validation -- a daily-only limit (max_concurrent left nil) is accepted silently by both the Go layer and cmd/manage-clients"
      - path: "internal/jobs/admission_test.go"
        issue: "TestOvershootBoundedByConcurrencyCap (the D-15 pin) only covers max_concurrent set; there is no test for the unbounded case, so CI cannot catch a regression or confirm the claimed fix"
    missing:
      - "Either: count admitted-but-unfinished (queued/active) work into the daily evaluation inside admitLocked's existing advisory-locked transaction (the fix REVIEW.md CR-01 sketches against jobs/job_inputs), so the daily cap is real regardless of max_concurrent."
      - "Or: reject SetLimit (internal/clients/limits.go) when a daily limit is set with max_concurrent == nil, making the unbounded configuration impossible to create rather than silently broken, and correct admission.go's comment plus CONTEXT.md D-15 to state the bound only holds under a concurrency cap."
      - "A test that pins whichever fix is chosen, covering the daily-only (max_concurrent = nil) configuration specifically -- the gap in the current test matrix."
deferred: []
---

# Phase 52: Quota Enforcement & Intra-Class Fairness — Verification Report

**Phase Goal:** An operator can cap what a client may consume (concurrent jobs, daily volume, allowed engines), enforcement is fail-closed on every job-creation path and rolled out safely to already-integrated consumers, and the same admission mechanism gives a structural guarantee that one client cannot starve others within an engine class — all without touching queue topology or the KEDA contract.
**Verified:** 2026-08-03
**Status:** gaps_found
**Re-verification:** No — initial verification

## Independent Assessment of 52-REVIEW.md CR-01

The team lead asked me not to inherit the reviewer's verdict on CR-01 ("daily byte/job
quotas are effectively unenforceable when `max_concurrent` is NULL") without checking it
myself. I did:

1. Read `internal/jobs/admission.go:188-243` (`evaluate`) and confirmed the daily check
   reads only `client_usage_daily`, a rollup written exclusively at the terminal
   transition (`internal/jobs/usage.go`, called from `MarkDone`/`MarkFailed`).
2. Confirmed `max_concurrent` is independently nullable at the schema
   (`0013_client_limits.sql:20-33`), the Go layer (`internal/clients/limits.go` — `SetLimit`
   performs full-row replace with no cross-field check), and the operator CLI
   (`cmd/manage-clients set-limit <id> <engine> - <bytes> <jobs>` — the literal `-` clears
   `max_concurrent`).
3. Wrote and ran a throwaway reproduction (`internal/jobs/zz_cr01_repro_test.go`, deleted
   after the run, not part of the permanent suite) that grants a client
   `max_daily_jobs = 2` with `max_concurrent = nil` and issues 50 sequential
   `Repo.Create` calls against a live Postgres instance:

   ```
   zz_cr01_repro_test.go:27: admitted 50 of 50 attempts against max_daily_jobs=2 with no concurrency cap
   ```

   50/50 admitted, confirming the cap does nothing until a job reaches a terminal state.
4. Confirmed the pinning test the code comment/D-15 rely on
   (`TestOvershootBoundedByConcurrencyCap`, `admission_test.go:456`) only exercises
   `max_concurrent = 3` — never `nil` — so it cannot and does not catch this.

**Verdict: CR-01 is correct.** This is not a reviewer misreading — it reproduces
deterministically against the live database this project uses for its own test suite.

**Does this mean QUOT-01 is unsatisfied?** No — QUOT-01's literal text ("operator can set
limits; absence is `NULL`, never a sentinel") is about the *storage/representation*
contract, and that contract is fully and correctly implemented (see Requirements Coverage
below). What CR-01 actually breaks is **QUOT-02's fail-closed enforcement guarantee**, for
one specific but fully supported operator configuration: daily-only limits with no
concurrency cap. In that configuration, the check runs on every request (so QUOT-02's
"checked on every path" is technically true), but it structurally cannot reject anything
until a prior job completes — which, for a client submitting jobs faster than they finish,
is never. I have filed this as its own gap rather than folding it into QUOT-01, because
conflating "can the operator express a daily-only limit" (yes, and correctly) with "does
that limit do anything" (no, in this configuration) would understate the first and
overstate the second.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1 — Operator can set per-client, per-engine limits (concurrent/daily-bytes/daily-jobs); `NULL` column AND absent row both mean unlimited, never a sentinel; no existing client's behavior changes until an operator opts in | ✓ VERIFIED | `0013_client_limits.sql` schema, `internal/clients/limits.go` (`SetLimit`/`ClearLimit`/`GetLimit`), `cmd/manage-clients` `set-limit`/`clear-limit`/`show-limits` subcommands exist and are wired. `clients.enforcement_mode` defaults to `'observe'` for every row (migration + live-DB check: `\d clients` shows `DEFAULT 'observe'::text`). Tests `TestAdmitUnlimitedWhenNoLimitsRow`, `TestAdmitUnlimitedWhenAllColumnsNull`, `TestLimitAbsentRowIsUnlimited`, `TestLimitSetAllNilRoundTrips`, `TestLimitMaxConcurrentZeroIsNotNil` all pass against a live Postgres instance (re-run by me, not merely trusted from SUMMARY). |
| 2 | SC2(a) — Every job-creation entry point (REST, MCP) enforces an identical, per-(client,engine) SERIALIZED check-and-admit (txn + advisory lock), proven by an integration test exercising both REST and a real MCP tool call, not assumed from proxying | ✓ VERIFIED | `admitLocked` (`internal/jobs/admission.go:254-295`) runs as the first statement inside `Repo.Create`'s `pgx.BeginFunc` transaction (`repo.go:140`), under `pg_advisory_xact_lock` keyed by `(client_id, engine)`. `internal/mcpserver/quota_admission_test.go`'s `TestQuotaAdmission` spins up one real `httptest.NewServer` over the actual `api.NewServer` + `jobs.NewRepo`, and its `RESTRejectsOverCap` / `MCPToolRejectsOverSameCap` subtests both reject the same client at the same cap through the two different entry points. Re-run by me: `PASS` (all 5 subtests). |
| 3 | SC2(b)-i — Authoritative admission is a reservation made before the S3 upload, with a release path if a later step fails | ⚠️ DEVIATION (disclosed & tested, not a raw fail) | Literal wording not met: the authoritative check (`admitLocked`, inside `Repo.Create`) runs AFTER `s.storage.Upload` in `handlers.go` (`Upload` at :696, `Create` at :727). This is a knowing, pre-planned deviation recorded in `52-CONTEXT.md` D-07/D-08 (no reservation record is ever written; occupancy is derived live from `jobs.status`) and restated with its residual explicitly in `admission.go:344-354`. The residual (an over-quota client wasting an upload during a *concurrent* burst, never on sequential retry) is measured, not assumed: `TestQuotaAdmission/ConcurrentBurstBoundedUploadWaste` logs "observed wasted uploads = 0 (bound N - K = 7)" on a live re-run. `PreAdmit` (unlocked, pre-upload) delivers the literal intent — no S3 burn on sequential retry against a full cap — even though it is not the authoritative mechanism. |
| 4 | SC2b — Admission reservations and completed usage are distinguishable; a job counted at admission and later completed is counted exactly once, not twice | ✓ VERIFIED | No reservation row is ever written (consequence of D-07/D-08 above), so there is nothing to double-count by construction: `client_usage_daily` is written exactly once, at the terminal transition only (Phase 51 behavior, unchanged here). |
| 5 | Daily byte/job quotas enforce a bounded, disclosed overshoot in EVERY valid operator configuration (D-15) | ✗ FAILED | See "Independent Assessment of CR-01" above. Reproduced live: 50/50 admits against `max_daily_jobs=2` with `max_concurrent=nil`. |
| 6 | SC3 — A limit-exceeding request gets `429` + `Retry-After` + `code=quota_exceeded`, distinguishable from `rate_limited`, never `403`; an in-flight slot is released on EVERY terminal state (`done`, `failed`, canceled/`reconciler_exhausted`) | ✓ VERIFIED | `writeQuotaError` (`handlers.go:1003-1004`) always emits `429` + `Retry-After` + `code: "quota_exceeded"`. `internal/ratelimit/ratelimit.go`'s existing 429 now carries `code: "rate_limited"` (`TestPerClient_OverLimitReturns429WithRetryAfter` asserts `body.Code == "rate_limited"`, re-run: PASS). No slot-release code exists anywhere (D-08) because occupancy is a live `COUNT` over `jobs.status IN ('queued','active')` — every terminal transition frees a slot as a side effect of the status change that already happens, with zero new code. |
| 7 | SC4 — An observe-only mode computes and exposes what would have been rejected (metric/log) without actually rejecting, runnable before an operator opts a client into enforcement | ✓ VERIFIED (mechanism) | `enforcementModeObserve` is the column default; `admitLocked`/`PreAdmit` both fire `quotaObserver` (→ `metrics.RecordQuotaWouldReject`, `octoconv_quota_would_reject_total{client_id,engine,reason}`) before checking enforcement mode, then admit when mode is `observe` or the global kill-switch is set. `TestObserveModeNeverRejects` and `TestQuotaAdmission/ObserveModeAdmitsBothPaths` both pass live. The "for at least one full quota window before an operator enables enforcement" clause is an operational/rollout claim (a process, not a code property) — not independently verifiable from the codebase; treated as satisfied at the mechanism level. |
| 8 | SC5 — The per-client, per-engine concurrency cap gives a structural K-of-M guarantee, shipped with ZERO diff to any `scaledobject-*.yaml` and ZERO signature change to the reconciler's `enqueuer` interface | ✓ VERIFIED | Independently reproduced the review's diff claim, not trusted from SUMMARY: `git diff --name-only d7ac603..HEAD -- 'deploy/chart/octoconv/templates/scaledobject-*.yaml'` → 0 files; `git diff d7ac603..HEAD -- deploy/chart/octoconv/templates/ \| grep -c octoconv_queue_depth` → 0; `git diff --name-only d7ac603..HEAD -- internal/queue/` → empty; no `Enqueuer`/`enqueuer` interface hits in either diff. `TestQuotaAdmission/FairnessGuaranteeIsPerClientNotGlobal` demonstrates the K-of-M property live rather than asserting it in prose. |
| 9 | D-12 — The MCP proxy carries `code` and the `Retry-After` wait hint into the agent-visible tool error, while the API key redaction is preserved | ✓ VERIFIED | `internal/mcpserver/client.go`'s `apiError` appends `(code=%s, retry_after=%ds)` when both are present, `code=%s` alone when only `code` is present, and redacts the API key on every branch (`redact(msg, c.apiKey)`). `TestQuotaAdmission/MCPToolRejectsOverSameCap` exercises this live. |

**Score:** 8/9 truths verified (truth #3 counted as verified-with-disclosed-deviation, not a raw pass; truth #5 is the one FAILED item).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/db/migrations/0013_client_limits.sql` | `client_limits` table, `clients.enforcement_mode`, in-flight index | ✓ VERIFIED | 78 lines; applied on live DB (`schema_migrations` row confirmed); schema matches spec exactly (`\d client_limits` / `\d clients`) |
| `internal/clients/limits.go` + `limits_test.go` | Set/Clear/Get/SetEnforcementMode + round-trip proof | ✓ VERIFIED | 132 / 217 lines; 5 tests pass live |
| `cmd/manage-clients/main.go` | `set-limit`/`clear-limit`/`show-limits`/`set-mode` subcommands | ✓ VERIFIED | All four cases present, wired to `internal/clients` |
| `internal/jobs/admission.go` + `admission_test.go` | Sentinels, `admitLocked`, `PreAdmit`, lock-key derivation | ✓ VERIFIED | 379 / 550 lines (>> min_lines 150/250); all exported symbols present; wired into `repo.go Create` as first statement |
| `internal/metrics/metrics.go` | `quotaWouldReject` counter reusing `clientLabelAllocator` | ✓ VERIFIED | `RecordQuotaWouldReject` reuses `clientLabel(...)`; `TestQuotaWouldRejectUsesBoundedClientLabel`, `TestQuotaWouldRejectReasonLabels`, `TestJobDurationLabelsPinned`, `TestQueueDepthLabelsPinned` all pass |
| `internal/ratelimit/ratelimit.go` | `code: "rate_limited"` on existing 429 | ✓ VERIFIED | `TestPerClient_OverLimitReturns429WithRetryAfter` asserts the code field |
| `internal/mcpserver/client.go` | `apiError` carries code + Retry-After, redacts key | ✓ VERIFIED | See truth #9 |
| `internal/mcpserver/quota_admission_test.go` | Live REST + MCP cross-entry-point proof, K-of-M demo, burst-waste measurement | ✓ VERIFIED | 595 lines (>> min_lines 230); `TestQuotaAdmission` 5/5 subtests pass live |
| `internal/api/handlers.go`, `internal/api/api.go`, `cmd/api/main.go` | Pre-upload gate, `PreAdmit` on `Repo` interface, kill-switch env wiring | ✓ VERIFIED | `PreAdmit` called before `storage.Upload`; `QUOTA_ENFORCEMENT_DISABLED` read and logged |
| `docs/openapi.yaml`, `README.md` | 429 contract docs (`quota_exceeded`/`rate_limited` enum), operator quota section | ✓ VERIFIED | Both present with the documented shapes |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `internal/jobs/repo.go Create` | `internal/jobs/admission.go admitLocked` | first statement inside existing `pgx.BeginFunc` | ✓ WIRED | `repo.go:140` |
| `internal/api/handlers.go handleCreateJob` | `s.repo.PreAdmit` | call placed before `s.storage.Upload` | ✓ WIRED | Confirmed ordering: `PreAdmit` :430, `Upload` :696 |
| `cmd/api/main.go` | `metrics.RecordQuotaWouldReject` | `jobsRepo.SetQuotaObserver` closure | ✓ WIRED | present |
| `internal/mcpserver/client.go` | HTTP 429 response | `resp.Header.Get("Retry-After")` + parsed `code` | ✓ WIRED | present, tested live |
| Reconciler advisory lock vs. quota advisory lock | key-space separation | one-bigint vs. two-int32 forms | ✓ WIRED (non-colliding) | Structurally distinct PostgreSQL lock tag spaces — confirmed by reading both call sites; `internal/reconciler/reconciler.go`'s comment is stale (WR-09, non-blocking) but the underlying property holds |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build is clean | `go build ./...` | no output | ✓ PASS |
| Vet is clean | `go vet ./...` | no output | ✓ PASS |
| Concurrency cap rejects beyond K | `go test ./internal/jobs/... -run TestAdmitConcurrencyCapRejectsBeyondK -v` | PASS | ✓ PASS |
| Daily caps reject over boundary | `go test ./internal/jobs/... -run TestDailyBytesLimit\|TestDailyJobsLimit -v` | PASS | ✓ PASS |
| Overshoot bound pin (K set) | `go test ./internal/jobs/... -run TestOvershootBoundedByConcurrencyCap -v` | PASS | ✓ PASS (only covers the K-set case — see gap) |
| Cross-entry-point REST+MCP proof | `go test ./internal/mcpserver/... -run TestQuotaAdmission -v` | PASS (5/5 subtests) | ✓ PASS |
| Daily-only config (no K) overshoot | ad-hoc reproduction, `max_daily_jobs=2`, `max_concurrent=nil`, 50 sequential creates | 50/50 admitted | ✗ FAIL — confirms CR-01 |
| `rate_limited` 429 body shape | `go test ./internal/ratelimit/... -v` | PASS | ✓ PASS |
| Metrics label-pinning (FAIR-02 guard) | `go test ./internal/metrics/... -run TestJobDurationLabelsPinned\|TestQueueDepthLabelsPinned -v` | PASS | ✓ PASS |
| FAIR-02 zero-diff, independently re-run | `git diff --name-only d7ac603..HEAD -- 'deploy/chart/octoconv/templates/scaledobject-*.yaml'` etc. | 0 lines / 0 files in every check | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|--------------|--------|----------|
| QUOT-01 | 52-01, 52-03 | Operator can set limits; `NULL`/absent = unlimited, never a sentinel | ✓ SATISFIED | Schema + CLI + tests above. **Note:** `.planning/REQUIREMENTS.md` still lists QUOT-01 as `[ ]` unchecked and "Pending" in its traceability table — this is a documentation-sync gap (REQUIREMENTS.md was last touched at roadmap creation, `ea514da`, and never updated after Phase 52 closed), not a functional gap. See Anti-Patterns. |
| QUOT-02 | 52-01, 52-03, 52-04, 52-05 | Fail-closed check on every job-creation path | ⚠️ SATISFIED WITH A GAP | The check runs on every path (REST + both MCP servers via the single REST enforcement point, D-09) and is correctly serialized under concurrency — but it is not effectively fail-closed for the daily-only (`max_concurrent = nil`) configuration. See Gap #1. |
| QUOT-03 | 52-02, 52-04, 52-05 | `429` + `Retry-After`, `quota_exceeded` distinguishable from `rate_limited`, never `403` | ✓ SATISFIED | Verified truth #6, tests pass live, `docs/openapi.yaml` documents both codes |
| QUOT-04 | 52-01, 52-02, 52-03, 52-04 | Observe-only mode computes/logs/metrics without rejecting | ✓ SATISFIED | Verified truth #7. Same REQUIREMENTS.md checkbox/traceability drift as QUOT-01 (still `[ ]`/"Pending") — documentation-sync gap only. |
| FAIR-01 | 52-03, 52-05 | Structural K-of-M concurrency ceiling per client per engine class | ✓ SATISFIED | Verified truth #8 (K-of-M part), D-06 slot semantics (`queued`+`active`) implemented and tested |
| FAIR-02 | 52-02, 52-05 | `octoconv_queue_depth`/PromQL/all 7 `ScaledObject`s unchanged | ✓ SATISFIED | Independently reproduced zero-diff, verified truth #8 |

**Orphaned requirements check:** `.planning/REQUIREMENTS.md`'s traceability table maps exactly QUOT-01..04, FAIR-01, FAIR-02 to Phase 52, matching the six requirement IDs the team lead specified and the six IDs declared across all five plans' frontmatter (`52-01`: QUOT-01, QUOT-04; `52-02`: QUOT-03, QUOT-04; `52-03`: QUOT-01, QUOT-02, QUOT-04, FAIR-01; `52-04`: QUOT-02, QUOT-03, QUOT-04; `52-05`: QUOT-02, QUOT-03, FAIR-01, FAIR-02). No orphans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/jobs/admission.go` | 205-221 | Comment claims a bound ("bounded by the concurrency cap: at most K x MAX_INPUT_BYTES") that is false when `max_concurrent` is NULL | 🛑 Blocker | Directly caused Gap #1 — a misleading comment backing an incorrect design assumption, independently reproduced |
| `.planning/REQUIREMENTS.md` | 20, 23, 87, 90 | QUOT-01 and QUOT-04 checkboxes/traceability still show `[ ]`/"Pending" despite both being functionally complete and `ROADMAP.md` marking the whole phase `[x]` done | ⚠️ Warning | Documentation drift; risks a future phase (e.g. Phase 53/55 planning) reasoning from a stale requirements state |
| `internal/jobs/repo.go` | 226-243 (Cancel), `internal/jobs/admission.go` 222-239 | `Cancel` never calls `recordUsage`, so `create → cancel → create …` resets daily-cap cost to zero (WR-01 in 52-REVIEW.md, independently plausible from reading `Cancel`) | ⚠️ Warning | Quota-evasion path against a trusted-internal-client threat model; same root cause family as Gap #1 (daily rollup is an incomplete ledger) but distinct trigger |
| `internal/jobs/repo.go` | 130 (`pgx.BeginFunc`) | No explicit `IsoLevel` pinned; the post-lock re-check's correctness silently depends on READ COMMITTED (WR-02 in 52-REVIEW.md, confirmed by reading `BeginFunc`'s call site) | ⚠️ Warning | Config-only single point of silent failure; not exercised by the current deployment (no isolation-level override), so not scored as a blocking gap here |
| `cmd/manage-clients/main.go` | 120 | `engine := os.Args[3]` has no validation against known engine classes (WR-08 in 52-REVIEW.md) | ⚠️ Warning | An operator typo produces a limit that is silently never matched/enforced |

(The remaining warnings/info items in `52-REVIEW.md` — WR-03 through WR-12, IN-01 through IN-04 — were read and are consistent with the code; they are quality/hardening concerns, not goal-blocking, and are not duplicated here.)

### Human Verification Required

None. This phase ships no UI; every must-have truth is either directly demonstrated by an
automated test run against a live Postgres instance (re-run by me during this
verification, not merely trusted from SUMMARY.md) or a static repository-diff check
(FAIR-02), both independently reproducible. SC4's "runs for at least one full quota window"
clause is an operational/rollout claim outside what a codebase-only verification can
assert — it is noted under truth #7 rather than escalated as a human-verification item,
since no phase artifact claims this has already happened in production.

### Gaps Summary

One BLOCKER gap: `52-REVIEW.md`'s CR-01 is independently confirmed correct. Daily
byte/job-count quotas are effectively unenforceable for any client whose operator sets a
daily-only limit (`max_concurrent = nil`) — a configuration the schema, the Go layer, and
the CLI all accept without warning. This is not a hypothetical: it reproduces 50/50 on a
live database with a trivial repro (`max_daily_jobs = 2`, 50 sequential creates, zero
rejections). It directly weakens QUOT-02's "fail-closed on every path" claim for that
configuration, and it means the `D-15` "bounded overshoot" decision recorded in
`52-CONTEXT.md` is not what the shipped code actually does outside the concurrency-capped
case that decision implicitly assumed.

This does not block QUOT-01 (limit *representation* is correct), QUOT-03, QUOT-04, FAIR-01,
or FAIR-02, all of which are independently verified above. It blocks a clean "phase goal
achieved" verdict because the phase's own Goal statement promises the operator can cap
"daily volume" with fail-closed enforcement, and for the daily-only configuration that
promise does not hold.

**Two secondary, non-blocking documentation gaps are worth operator attention but do not
themselves block phase closure:**
- `.planning/REQUIREMENTS.md` was not updated after Phase 52 closed; QUOT-01 and QUOT-04
  still show `[ ]`/"Pending" though both are functionally complete.
- `admission.go`'s own comment (and `52-CONTEXT.md` D-15) states the false bound as fact,
  with no caveat — anyone reading only the code, not the review, would trust the bound.

**This looks like a genuine gap, not an intentional deviation** — unlike truth #3 above
(SC2b's literal reservation wording), there is no CONTEXT.md decision that anticipated or
accepted "the daily cap does nothing when no concurrency cap is set." If, after review, the
team decides the daily-only configuration is out of scope for this phase (e.g., deferring
the fix and instead documenting the caveat + rejecting the unsafe configuration at the CLI
layer, per the review's stated minimum-alternative fix), that is a legitimate path forward
— but it needs an explicit decision recorded (either a code fix + test, or a
`client_limits`-level guard plus corrected comments), not silence. If the team accepts the
current behavior as-is for now, add to this file's frontmatter:

```yaml
overrides:
  - must_have: "Daily byte/job quotas enforce a bounded overshoot in every valid operator configuration (D-15)"
    reason: "{why this deviation is acceptable, e.g. deferred fix with tracking}"
    accepted_by: "{name}"
    accepted_at: "{ISO timestamp}"
```

---

_Verified: 2026-08-03T07:15:00Z_
_Verifier: Claude (gsd-verifier)_
