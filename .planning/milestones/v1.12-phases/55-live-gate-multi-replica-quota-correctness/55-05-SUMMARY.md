---
phase: 55-live-gate-multi-replica-quota-correctness
plan: 05
subsystem: infra
tags: [kubernetes, postgres, quota, live-gate, multi-replica, admission]

# Dependency graph
requires:
  - phase: 55-live-gate-multi-replica-quota-correctness (plan 07)
    provides: "the hardened scripts/quota-multireplica-liveproof.sh (heavy fixture, THE INVARIANT preflight, image-freshness preflight, per-round Postgres discriminating assertions, second timeline TSV) this plan actually ran"
provides:
  - "Committed live-gate evidence at api.replicas: 2 (per-request TSV, per-admitted-job timeline TSV, gate transcript, calibration log) proving ROADMAP Phase 55 SC1 by direct observation"
  - "internal/jobs/admission.go doc-comment counter-statement: the quota admission mechanism carries NO api.replicas: 1-only caveat, unlike internal/ratelimit, closing ROADMAP Phase 55 SC2"
  - "QUOT-05 marked Complete in REQUIREMENTS.md's traceability table"
affects: ["future milestone audits of v1.12 (Consumer Accounting & Fairness) -- QUOT-05 was the last Pending v1 requirement in that milestone"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Comment-only correctness record: a package doc comment names a re-runnable script and a committed evidence file by path plus the observed numbers, so a 'no caveat needed' claim stays falsifiable rather than asserted (mirrors internal/ratelimit's existing caveat register, inverted)"

key-files:
  created:
    - .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/quota-multireplica-20260807T153358Z.tsv
    - .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/quota-multireplica-20260807T153358Z-timeline.tsv
    - .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/gate-run5-PASS-20260807T153358Z.log
  modified:
    - internal/jobs/admission.go
    - .planning/REQUIREMENTS.md

key-decisions:
  - "Evidence artifacts for run 20260807T153358Z were already committed by the orchestrator (commit d23e0a1, message `test(55): QUOT-05 live gate PASS -- 10/10 rounds, 167 assertions`) before this agent resumed at Task 2 -- confirmed via `git log` and `git status` (clean) rather than re-committed, per explicit resume instructions."
  - "The filenames actually written by the run (`gate-run5-PASS-20260807T153358Z.log`, no separate `calibration-<RUN_ID>.log`) differ from the plan's suggested naming (`gate-transcript-<RUN_ID>.log` / `calibration-<RUN_ID>.log`); the calibration phase's PASS lines are embedded in the same gate transcript rather than a separate log for this run. Recorded as a plan-text/execution mismatch, not worked around: the underlying requirement (a committed, RUN_ID-paired transcript covering both calibration and the ten rounds) is satisfied by the single committed log."

requirements-completed: [QUOT-05]

# Metrics
duration: ~15min (Task 2 only; Task 1 was an operator-run checkpoint spanning a full install/calibrate/run/teardown cycle, its own duration not tracked by this agent)
completed: 2026-08-07
---

# Phase 55 Plan 05: Live Multi-Replica Quota Gate Summary

**Live 10-round, 167-assertion multi-replica quota run at `api.replicas: 2` (20 admits / 80 `quota_exceeded` rejects / 0 `rate_limited`, both pods serving both outcomes) committed as evidence, with `internal/jobs/admission.go` now recording — citing that run by path and number — that the quota admission mechanism, unlike `internal/ratelimit`, carries no `api.replicas: 1`-only caveat.**

## Performance

- **Task 1 (operator-gated live run):** run at 2026-08-07T15:33:58Z (`run_id 20260807T153358Z`); operator-run, duration not tracked by this agent.
- **Task 2 (this agent, comment + SUMMARY):** ~15 min.
- **Tasks:** 2 (1 `checkpoint:human-verify`, 1 `auto`)
- **Files modified (Task 2):** 2 (`internal/jobs/admission.go`, `.planning/REQUIREMENTS.md`), comment-only in the former

## Live gate evidence (operator's verbatim numbers, re-derived independently)

Run: `scripts/quota-multireplica-liveproof.sh` against a real OrbStack Kubernetes cluster, `api.replicas: 2`, engine `image`, `QUOTA_K=2`, `BURST_N=10`, `ROUNDS=10`, fixture `scripts/fixtures/quota-burst-heavy.png` (259,682 bytes, 9000x9000), target format `webp`, values files `values-local.yaml` + `values-multireplica.yaml`, run id `20260807T153358Z`, 2026-08-07T15:33:58Z.

**Headline result (from the script's own final banner):**
```
=== GATE PASS: 10/10 rounds passed, 167 assertions passed, evidence at .../quota-multireplica-20260807T153358Z.tsv ===
SC1 (QUOT-05): exactly K (2) jobs admitted in every round; every round's cap decision crossed the
api-6769bc54f7-frgdx/api-6769bc54f7-t8j4m replica boundary; aggregate cross-pod coverage confirmed
both pods served both an admit and a quota_exceeded reject over the whole run. The no-premature-
terminal precondition for the exact-K assertion was proven from Postgres every round (Plan 55-07).
PASS -- Phase 55 (QUOT-05) quota multi-replica live gate complete (167 checks).
exit=0
```

**Four independent `awk` re-derivations against the committed TSV (orchestrator-derived, not copied from the script's own output), each matching the acceptance criteria exactly:**
- `NR>1 && $6==202` → **20** (10 rounds x K=2 admits)
- `NR>1 && $6==429 && $7=="quota_exceeded"` → **80** (10 rounds x 8 rejects)
- `NR>1 && $7=="rate_limited"` → **0**
- `NR>1 {print $8}` unique → **2** distinct serving pods

**Cross-pod attribution:**
- **`api-6769bc54f7-frgdx`**: 52 requests total — 10 admits (202), 42 `quota_exceeded` rejects (429)
- **`api-6769bc54f7-t8j4m`**: 48 requests total — 10 admits (202), 38 `quota_exceeded` rejects (429)
- Both pods served at least one admit and at least one reject; the api pod set was unchanged from start to end of the run.

**Calibration (Step 2, in-cluster, since Plan 55-06):** `api Deployment spec.replicas == 2` PASS, `api Service ready endpoint count == 2` PASS, both api pod names printed, and a final calibration PASS naming a non-zero probe count for each pod — this is the check that makes the whole run non-vacuous (a single-pod-only distribution would have failed calibration before any burst budget was spent).

**Per-round discriminating precondition (Plan 55-07's D6, the load-bearing fix that makes the exact-K assertion sound):** every one of the 10 rounds' transcripts carries a PASS line reading "no-premature-terminal precondition for the exact-K assertion holds (0 of this round's admitted jobs went terminal at or before max_recv=...)" — printed **before** that round's `admitted count == 2` line. This is a proven premise, not an assumption: admitted-job wall times across the run ranged 5983–11001 ms, and burst send spreads ranged 1–11 ms against a 250 ms bound, so no admitted job could plausibly have gone terminal inside a burst — and the gate proves it from Postgres every round rather than trusting the arithmetic.

**Timeline artifact** (`quota-multireplica-20260807T153358Z-timeline.tsv`, 20 rows + header, one per admitted job): columns `round, job_id, created_at, first_terminal_at, terminal_status, wall_ms, round_last_recv_utc`. Every row's `terminal_status` is `done` and every `wall_ms` comfortably exceeds its round's send spread (e.g. round 1's two admitted jobs: 10206ms and 10207ms wall time vs. a burst that landed within milliseconds).

**Teardown confirmed:** namespace `octoconv` absent, no helm releases, no octoconv/burst containers running after the run.

**Both ROADMAP Phase 55 success criteria are now met:**
- **SC1** — "N concurrent job-creation requests ... result in at most K admitted jobs ... observed directly on a live run" — met: exactly K=2 admitted, exactly 8 rejected, in all 10 rounds, at `api.replicas: 2`, with every round's decision crossing the replica boundary.
- **SC2** — "evidence ... is captured and committed ... and the phase records explicitly that the quota mechanism ... does not carry a replicas-count caveat" — met: evidence committed (see Files below) and `internal/jobs/admission.go` now carries the explicit counter-statement (see Task 2 below).

## Full live-gate history (why this run is trustworthy)

This phase's live gate reached a passing run only on its third distinct attempt, after two prior failures that are all committed as permanent evidence rather than discarded:

1. **First calibration attempt, 2026-08-06 (`calibration-FAILED-20260806T214551Z.log`) — host→ClusterIP unreachable, 0/20.** RESEARCH.md's assumption A1 (OrbStack routes host traffic through the real Service ClusterIP DNAT path) was **false** in this environment: 0 of 20 probes from the macOS host to the api Service's ClusterIP returned 200, independently confirmed unreachable against the unrelated `kubernetes` service in `default` too — an environment fact, not a chart defect. Everything before that step passed (preflight, two-phase install, replica count, endpoint count, both pod names discovered). Remedy: Plan 55-06 relocated the FIFO-barrier burst driver into a throwaway in-namespace pod (RESEARCH.md's pre-documented Pattern 1 fallback), with no assertion weakened.

2. **Three preflight-caused aborts during the 2026-08-07 session, before any full run completed** — a missing `octoconv-worker:dev` image surfaced only as an unnamed 300s `kubectl wait` timeout, and a stale `octoconv-api:dev` image (built 2026-08-03, predating migration `0013_client_limits.sql`) surfaced only as `relation "client_limits" does not exist` five minutes into a run. Plan 55-07 closed both: STEP 1 now asserts image presence, build-time-after-newest-migration, api-binary-embeds-newest-migration-name, and worker-has-vips, before any cluster mutation — so an equivalent fault now fails in the first seconds by name rather than after a multi-minute install.

3. **First full ten-round run, 2026-08-07 (run id `20260807T141252Z`, `gate-run4-FAILED-round4-20260807T141252Z.log` + `quota-multireplica-20260807T141252Z.tsv`) — round 4 over-admitted (expected 2, got 3), after 56 passing checks.** Analysis (committed in `evidence/README.md`) showed the 10 send offsets were `+0 +0 +1 +1 +1 +1 +2 +3 +4 +159` ms: the nine genuinely-simultaneous requests produced exactly 2 admits, and the tenth — sent 155ms later, answered 310ms after the first admission — was admitted as a third. With an 86-byte fixture converting in well under 310ms, a concurrency slot most likely freed **legitimately** in between (the cap bounds jobs concurrently in flight, not admissions per burst), but the run tore the namespace down on exit before that could be proven from a job-status timeline. This was **not treated as evidence of a broken cap** and **not fixed by relaxing `==` to `<=`** (which would have let a round admitting 0 pass silently) — it was fixed by making the premise true: Plan 55-07 replaced the fixture with the heavy 259,682-byte PNG (5.9–6.5s per conversion, measured in the real worker image), tightened the send-spread bound from 1000ms to 250ms behind a self-checking invariant, removed the forks and blind sleep that produced the +159ms straggler, and added the per-round Postgres discriminating check described above.

4. **Second full ten-round run, 2026-08-07 (run id `20260807T153358Z`) — PASS, 10/10 rounds, 167 assertions.** This is the run whose numbers are reported above. Every round's discriminating precondition held (0 admitted jobs went terminal early, every round), so this run's exact-K assertion rests on a proven premise rather than an assumption — closing the exact gap round 4 exposed.

This history is why the gate is trustworthy: it did not pass on the first attempt, its one apparent over-admission was diagnosed rather than argued away, and the fix that made the second full run pass is structural (a discriminating Postgres check run before the assertion it could otherwise explain away) rather than a loosened threshold.

## Task Commits

1. **Task 1: Operator runs the multi-replica quota live gate** — `checkpoint:human-verify`, operator-run and PASSED. Evidence already committed by the orchestrator at `d23e0a1` (`test(55): QUOT-05 live gate PASS — 10/10 rounds, 167 assertions`) before this agent resumed. No new commit made by this agent for Task 1's evidence — it was already in place and verified clean via `git status`.
2. **Task 2: Record the SC2 no-caveat counter-statement** — `internal/jobs/admission.go` doc-comment addition (comment-only diff, verified) plus `.planning/REQUIREMENTS.md` QUOT-05 → Complete. Commit hash recorded below after this SUMMARY is committed.

**Plan metadata:** commit hash recorded in the final commit step below.

## Files Created/Modified

- `internal/jobs/admission.go` — appended a paragraph to the existing package-file doc comment (lines 1-23) stating that, unlike `internal/ratelimit`, this mechanism carries no `api.replicas: 1`-only caveat; cites `scripts/quota-multireplica-liveproof.sh` and both evidence TSV filenames by path, states the observed numbers (10/10 rounds, K=2/8 rejects per round, both pod names, aggregate cross-pod coverage), and names what the gate does NOT cover (daily-jobs/daily-bytes caps, per CONTEXT.md D-03, still covered only by `admission_test.go`'s in-process burst tests). No logic line touched — verified with `git diff -U0 -- internal/jobs/admission.go | grep -E '^[+-][^+-]' | grep -vc '^[+-]//'` → `0`.
- `.planning/REQUIREMENTS.md` — QUOT-05 checkbox and traceability row flipped from `Pending` to `Complete`.
- (Already committed by the orchestrator, not re-committed here) `.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/quota-multireplica-20260807T153358Z.tsv`, `.../quota-multireplica-20260807T153358Z-timeline.tsv`, `.../gate-run5-PASS-20260807T153358Z.log`.

## Decisions Made

See `key-decisions` in frontmatter above. Summarized:
1. Evidence for run `20260807T153358Z` was already committed by the orchestrator before this agent resumed — confirmed via `git log`/`git status` rather than re-committed, per explicit resume instructions.
2. The actual filenames written by the run differ slightly from the plan's suggested naming scheme (one combined gate-transcript log covering calibration + all 10 rounds, rather than two separate `gate-transcript-<RUN_ID>.log` / `calibration-<RUN_ID>.log` files) — recorded as a plan-text/execution mismatch; the underlying requirement (a single RUN_ID-paired, committed transcript) is satisfied.

## Deviations from Plan

None requiring a fix. The one plan-text/execution mismatch (evidence filenames not matching the plan's suggested pattern) is recorded under Decisions above rather than as a deviation needing correction, since the underlying acceptance criterion (evidence tracked by git, RUN_ID-paired, non-trivial) is satisfied by the files as actually written.

## Issues Encountered

None. No authentication gates. No Rule 4 (architectural) decisions were required.

## Known Stubs

None.

## Threat Flags

None beyond the plan's own `<threat_model>` (T-55-19 through T-55-23, T-55-SC — all mitigated as specified: the doc comment cites the script and evidence file by path with observed numbers rather than asserting; the run's transcript and TSV were already grepped by the committing agent for the `API_KEY_SALT` literal and 40+ character opaque tokens before commit, per the plan's Task 2 action text; this task's own diff to `internal/jobs/admission.go` is comment-only, so the record and the code cannot drift within this plan). No new network endpoints, auth paths, or schema changes.

## Next Phase Readiness

- QUOT-05 moves to Complete in `.planning/REQUIREMENTS.md` — this was the last Pending v1 requirement in milestone v1.12 (Consumer Accounting & Fairness). A milestone audit of v1.12 can now proceed.
- `internal/jobs/admission.go`'s doc comment is the permanent, falsifiable record of the no-caveat claim; a future maintainer scaling the API further does not need to revisit quota admission's replica-safety.
- The live-gate harness (`scripts/quota-multireplica-liveproof.sh`, `scripts/quota-burst-driver.sh`, `scripts/fixtures/quota-burst-heavy.png`) is re-runnable if the mechanism is ever changed and the claim needs re-verifying.

## Self-Check: PASSED

- FOUND: `.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/quota-multireplica-20260807T153358Z.tsv`
- FOUND: `.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/quota-multireplica-20260807T153358Z-timeline.tsv`
- FOUND: `.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/gate-run5-PASS-20260807T153358Z.log`
- FOUND commit `d23e0a1` (evidence, pre-existing, confirmed via `git log --oneline`)
- `go build ./...`: PASSED
- `go vet ./...`: PASSED
- `gofmt -l internal/jobs`: empty (PASSED)
- `go test ./internal/jobs/ -run TestConcurrentBurstAdmitsExactlyK -count=1`: PASSED
- `git diff -U0 -- internal/jobs/admission.go | grep -E '^[+-][^+-]' | grep -vc '^[+-]//'` → `0` (comment-only diff, PASSED)
- `internal/jobs/admission.go` contains the literals `internal/ratelimit`, `scripts/quota-multireplica-liveproof.sh`, and the evidence filename `quota-multireplica-20260807T153358Z.tsv` — confirmed via grep

---
*Phase: 55-live-gate-multi-replica-quota-correctness*
*Completed: 2026-08-07*
