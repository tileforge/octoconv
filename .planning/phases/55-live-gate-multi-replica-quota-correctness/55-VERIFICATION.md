---
phase: 55-live-gate-multi-replica-quota-correctness
verified: 2026-08-07T00:00:00Z
status: passed
score: 10/10 must-haves verified
overrides_applied: 0
---

# Phase 55: Live Gate — Multi-Replica Quota Correctness Verification Report

**Phase Goal:** Prove, on a live multi-replica setup, that the quota admission counter built in
Phase 52 is correct when 2+ API replicas concurrently serve the same client — the one claim in this
milestone that cannot be proven statically, matching this project's own precedent
(`internal/ratelimit`'s documented `api.replicas: 1`-only caveat).

**Verified:** 2026-08-07
**Status:** passed
**Re-verification:** No — initial verification

## Method

This is an operator-gated live-proof phase. Verification was done entirely from committed
artifacts and static code inspection (no cluster was brought up, per instructions): reading all
7 PLAN/SUMMARY pairs, re-deriving the headline claim directly from the committed TSV/timeline
files with independent `awk`/`grep` commands (never trusting the script's own banner or the
SUMMARY's prose), diffing `internal/jobs/admission.go` commit-by-commit across the phase, running
`bash -n` / `shellcheck -S error` on both scripts, running `go build/vet/test`, `gofmt -l`, and
`helm lint`/`helm template` on the overlay, and reading the two committed failure transcripts
(`calibration-FAILED-...`, `gate-run4-FAILED-round4-...`) to confirm the gate's history of real,
unweakened failures.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1: at `api.replicas=2`, 10 rounds of N=10 concurrent requests each admit exactly K=2, reject exactly 8 with `quota_exceeded`, on a live run | ✓ VERIFIED | Independently re-derived from `evidence/quota-multireplica-20260807T153358Z.tsv` (100 data rows, filtered `grep -v '^#'`): `$6==202` → 20 rows; `$6==429 && $7=="quota_exceeded"` → 80; `$7=="rate_limited"` → 0; distinct `$8` (serving_pod) → 2; per-round `$6==202` count is exactly 2 in every one of the 10 rounds. Matches the exact expected shape named in the task brief. |
| 2 | SC1 corollary: each round's cap decision provably crosses the replica boundary (an admit and a reject on *different* pods, not just "both pods saw traffic") | ✓ VERIFIED (with a documented evidence-granularity gap, see Gap Note A) | `scripts/quota-multireplica-liveproof.sh` line ~1312 hard-fails unless `(pod1_admit≥1 ∧ pod2_reject≥1) ∨ (pod2_admit≥1 ∧ pod1_reject≥1)` — a condition that would fail on a single-pod-only distribution, so it is not vacuous. Transcript shows this PASS in all 10 rounds with real per-pod admit/reject counts (e.g. round 1: `frgdx admit=1 reject=2, t8j4m admit=1 reject=6`). However, the transcript does **not** print a line naming a *specific* admitted request-id on one pod and a *specific* rejected request-id on a different pod, as the 55-05-PLAN.md checkpoint's own acceptance criteria required — only aggregate per-pod counts are ever computed/printed. The underlying claim is still soundly proven by the aggregate check; only the plan's own stronger evidentiary-format bar was not literally met, and this was not disclosed as a deviation in 55-05-SUMMARY.md. |
| 3 | The discriminating precondition: `admitted==K` is only asserted after proving, from Postgres, that no admitted job in that round reached a terminal status at or before the round's last response | ✓ VERIFIED | Source: `terminal_before` call site at line 1285 precedes `assert_eq "$QUOTA_K" "$admit_count"` at line 1302 (confirmed by direct `grep -n`, matching 55-07-SUMMARY's own claim). Transcript: all 10 "no-premature-terminal precondition ... holds" PASS lines appear immediately before each round's "admitted count == 2" line. Independently confirmed from the *data*, not narration: `quota-multireplica-20260807T153358Z-timeline.tsv` shows every admitted job's `wall_ms` (5983–11024 ms) is enormously larger than its round's `round_last_recv_utc`-to-send-time window (single-digit ms), i.e. no admitted job could plausibly have gone terminal inside the burst — the premise is true, not merely asserted true. |
| 4 | A round that over-admits is diagnosable (legitimate slot reuse vs. real cap violation), not a riddle, per the 2026-08-07 round-4 incident | ✓ VERIFIED | `evidence/gate-run4-FAILED-round4-20260807T141252Z.log` + `evidence/README.md` document a genuine, undoctored gate failure (`admitted count -- expected [2], got [3]` after 56 real passing checks) that could not be fully diagnosed at the time (no timeline artifact existed). Plan 55-07 closed exactly that gap: heavier fixture (measured 5933–6549 ms conversions, not estimated), tightened+self-checking spread bound, jitter removal, and the `terminal_before` discriminator + committed timeline TSV. The fix is structural (a checked precondition), not a threshold loosening — confirmed by reading the diff; `==` was never relaxed to `<=`. |
| 5 | SC2: the live run's evidence — timestamps, replica identifiers, admitted/rejected counts — is captured and committed | ✓ VERIFIED | `git ls-files` confirms `quota-multireplica-20260807T153358Z.tsv`, `-timeline.tsv`, `gate-run5-PASS-...log`, `calibration-FAILED-...log`, `gate-run4-FAILED-...log`, and `quota-multireplica-20260807T141252Z.tsv` are all tracked despite the blanket `/.planning/` `.gitignore` exclusion (force-added, matching project precedent). TSV carries per-request `send_ts_ms`/`recv_ts_ms`, `serving_pod`, status, body code; timeline TSV carries per-admitted-job `created_at`/`first_terminal_at`/`wall_ms`. No secrets found: grepped both files and all logs for `API_KEY_SALT` and any 40+ char opaque token — only sha256 digests, UUIDs and paths matched. |
| 6 | SC2: the phase records explicitly, in source, that the quota mechanism carries no `api.replicas: 1`-only caveat, structural argument first, live confirmation second | ✓ VERIFIED (with a minor completeness note, see Gap Note B) | `internal/jobs/admission.go`'s package doc comment (lines 24–63) states the structural argument first ("The reason is structural, not empirical...") and *then* the live confirmation ("That structural argument is now also confirmed by direct live observation...", citing the script and both evidence filenames by path with the exact observed numbers). It also states what is NOT covered ("only the concurrency limit ... was exercised live; the daily-jobs and daily-bytes caps were deliberately out of scope"). It does not separately spell out that other K values, other engines, or >2 replicas were not exercised — a minor completeness gap, not a soundness gap, since the structural argument it leads with is generic to K/engine/replica-count by construction. |
| 7 | The mechanism under test (`internal/jobs/admission.go`'s advisory-lock + same-transaction concurrency check) was not behaviorally changed by this phase | ✓ VERIFIED | Only two commits touch this file across the whole phase: `fde52bc` (55-03, folded todo) and `9fecffa` (55-05). Read both diffs in full: `fde52bc` only touches the *daily*-quota branch (adds `DailyQuotaError`, extends the daily `QueryRow` with a third `now()` column) plus a comment above the untouched `if count >= *st.maxConcurrent { return ErrConcurrencyLimit, nil }` line; `9fecffa` is a pure doc-comment append (`git diff -U0 ... | grep -E '^[+-][^+-]' | grep -vc '^[+-]//'` → 0, independently re-checked). `admitLocked`, `quotaLockKeys`, and the advisory-lock/COUNT structure are untouched. |
| 8 | Anti-vacuity: the gate's assertions are genuinely capable of failing, not decorative | ✓ VERIFIED | Direct evidence, not inference: two real, committed failure transcripts exist (`calibration-FAILED-...`, `gate-run4-FAILED-round4-...`), both diagnosed and closed by structural fixes rather than threshold loosening (confirmed: no `<=` relaxations, spread bound tightened not loosened, discriminator added not removed). Cross-pod, exact-equality, stream-truncation (5-fold), and image-freshness assertions were each read directly and confirmed to encode a real failure condition (e.g., a single-pod-only distribution, an under/over-admitting round, a truncated driver stream, a stale image) rather than a tautology. |
| 9 | Requirement coverage: QUOT-05 traced correctly; folded-todo plans (55-03/55-04) traced coherently | ✓ VERIFIED | `.planning/REQUIREMENTS.md:91`: `QUOT-05 \| Phase 55 \| Complete`. No orphaned Phase-55 rows. 55-03/55-04 declare `requirements: [TODO-2026-08-04-...]` / `[TODO-2026-08-05-...]` in PLAN frontmatter rather than a ROADMAP requirement ID — coherent with `55-CONTEXT.md` D-07's explicit "folded todos, independent of the live gate" decision, and both corresponding files are moved to `.planning/todos/completed/`. |
| 10 | Code quality / no debt markers, static checks clean | ✓ VERIFIED | `bash -n` and `shellcheck -S error` clean on both scripts (re-run directly, not copied from SUMMARY). `go build ./...`, `go vet ./...`, `gofmt -l internal/jobs internal/api` all clean. `helm lint`/`helm template` on the overlay: 0 ScaledObjects, api Deployment replicas=2. No `TBD`/`FIXME`/`XXX` in any file this phase touched. |

**Score:** 10/10 truths verified (2 carry a documented, non-blocking gap/completeness note — see below).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `deploy/chart/octoconv/values-multireplica.yaml` | Gate-only overlay: `api.replicas:2`, relaxed RATE_LIMIT, `keda.enabled:false` | ✓ VERIFIED | Present; `helm lint`/`helm template` confirm rendered shape; unaffected default render still shows 7 ScaledObjects. |
| `scripts/quota-multireplica-liveproof.sh` | Host-side gate: install, calibration, round engine, evidence emission | ✓ VERIFIED (1556 lines) | `bash -n`/`shellcheck -S error` clean; zero non-comment `curl`/`svc/api` port-forward references; every per-round assertion label present exactly once. |
| `scripts/quota-burst-driver.sh` | In-pod burst/calibration driver | ✓ VERIFIED (314 lines) | `bash -n`/`shellcheck -S error` clean; sentinel framing, fork-free `ms_now`, readiness handshake all present. |
| `scripts/fixtures/quota-burst-heavy.png` + `gen_heavy_png.py` | Heavy, small-on-wire fixture + deterministic generator | ✓ VERIFIED | 259,682 bytes committed, `git ls-files` tracked, matches TSV's `fixture_bytes=259682`; generator is stdlib-only (confirmed via AST import check). |
| `.planning/phases/55-.../evidence/*` | Committed TSVs + transcripts | ✓ VERIFIED | 7 files present and git-tracked (force-added past the `/.planning/` blanket ignore). |
| `internal/jobs/admission.go` doc comment | No-caveat counter-statement | ✓ VERIFIED | Present, comment-only diff, cites script + evidence filenames + observed numbers. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| Burst request `X-Request-Id` | Serving api pod | `kubectl logs <pod>` grep, joined per-request | ✓ WIRED | Attribution-completeness assertion (`attributed_total == BURST_N`) confirmed present and would fail on an empty/duplicate join. |
| 429 response body | admit/reject classification | body `code` field, not bare status | ✓ WIRED | `429:rate_limited` is a hard FAIL branch distinct from `429:quota_exceeded`; confirmed 0 `rate_limited` rows in the committed TSV. |
| `terminal_before` (Postgres) | `assert_eq admitted==K` | executed strictly before, same round | ✓ WIRED | Line-number ordering (1285 < 1302) independently confirmed. |
| Evidence TSV aggregate claims | The TSV file itself | `awk` re-derivation, no script re-run needed | ✓ WIRED | Independently reproduced all four headline counts (20/80/0/2) directly from the file in this verification pass. |

### Data-Flow Trace (Level 4)

Not applicable in the conventional sense (this is a bash live-gate harness, not a UI/data-fetching
component) — the equivalent check here is "does the committed evidence reflect real observed data
rather than a static/hardcoded shape," which was performed directly: the TSV's per-request rows
carry distinct millisecond timestamps and two distinct real pod names (`api-6769bc54f7-frgdx`,
`api-6769bc54f7-t8j4m`), and the timeline TSV's `wall_ms` values are all different real numbers
(5983–11024 ms) rather than a repeated constant. This rules out a static/fabricated evidence file.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|---|---|---|---|---|
| QUOT-05 | 55-01, 55-02, 55-05, 55-06, 55-07 | Quota counter correctness under N API replicas | ✓ SATISFIED | Live gate PASS, evidence committed, doc-comment counter-statement recorded, REQUIREMENTS.md flipped to Complete. |
| TODO-2026-08-04-RETRY-AFTER-MIDNIGHT | 55-03 | Retry-After midnight race fix | ✓ SATISFIED | `DailyQuotaError`/`EvaluatedAt` added, straddle regression test passes, todo moved to `completed/`. |
| TODO-2026-08-05-PER-ROW-ENFORCED-FLAG, TODO-2026-08-05-USAGE-ROW-EMPTY-ENFORCEMENT-MODE | 55-04 | `/v1/system/usage` per-row `enforced` + enum-safety | ✓ SATISFIED | Both fixes present, tests added, OpenAPI updated, todos moved to `completed/`. |

No orphaned requirements: `.planning/REQUIREMENTS.md`'s "Phase 55" row set contains exactly QUOT-05,
which matches the single requirement PLAN 55-01/02/05/06/07 declare.

### Anti-Patterns Found

None. No `TBD`/`FIXME`/`XXX` in any file touched by this phase. No stub returns, no hardcoded empty
data flowing to output, no placeholder text.

### Human Verification Required

None outstanding. The one `checkpoint:human-verify` task in this phase (55-05 Task 1) was already
executed by the operator and its evidence (transcript, TSVs) is committed and was independently
re-derived in this verification pass rather than taken on faith.

### Gap Notes (non-blocking)

**Gap Note A — D-05 evidentiary granularity.** `55-05-PLAN.md`'s Task 1 acceptance criteria required:
"The transcript contains, for at least one round, an explicit line naming a specific admitted
request id on one pod and a specific rejected request id on a different pod." The actual
implementation (unchanged since 55-02) only tracks and prints **aggregate per-pod admit/reject
counts** (`pod1_admit`, `pod2_reject`, etc.), never a specific named ID pair. The underlying claim
(SC1's "cap decision crossed the replica boundary") is still soundly proven — the check would fail
on a single-pod-only distribution — but the literal evidentiary bar the plan itself set was not met,
and this discrepancy was not surfaced as a deviation in 55-05-SUMMARY.md's Self-Check.

**This looks like an unflagged deviation, not an intentional one.** No override is suggested because
the underlying truth is not in doubt (independently confirmed above); this is recorded so a future
reader does not go looking in the transcript for a line that isn't there.

**Gap Note B — counter-statement scope completeness.** `internal/jobs/admission.go`'s no-caveat
doc comment states the structural argument first and the live confirmation second (as required),
and does name what daily-limit types were out of scope, but does not explicitly enumerate that only
K=2, only the `image` engine, and only exactly 2 replicas were exercised live. This is a minor
completeness gap in an otherwise honest, falsifiable record — the structural argument it leads with
is generic to K/engine/replica-count by construction, so the omission does not undermine the claim's
soundness.

Neither gap note changes the phase's overall status: both concern documentation/evidentiary
completeness on top of independently-verified, sound underlying truths, not a missing or
non-functional artifact.

### Gaps Summary

No blocking gaps. ROADMAP Phase 55 SC1 and SC2 are both independently verified from the committed
evidence (not from SUMMARY prose): the headline claim (100 requests, 20×202/80×429-quota_exceeded/
0×rate_limited, exactly 2 distinct pods, exactly 2 admits in every one of 10 rounds) was re-derived
directly from `quota-multireplica-20260807T153358Z.tsv` with independent `awk` commands and matches
exactly. The discriminating precondition that makes `admitted==K` sound (rather than a timing
coincidence) is present in the source, ordered correctly, and independently confirmed true from the
timeline TSV's own wall-time data. The mechanism under test was not behaviorally altered by this
phase (verified by diffing every commit that touched `internal/jobs/admission.go`). The gate has a
real, documented history of catching genuine failures (a false reachability assumption, a
legitimate-slot-reuse ambiguity) and fixing them structurally rather than by weakening assertions.
Two minor, non-blocking documentation/evidentiary-completeness notes are recorded above for future
attention but do not affect the phase's goal achievement.

---

*Verified: 2026-08-07*
*Verifier: Claude (gsd-verifier)*
