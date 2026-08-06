---
phase: 55-live-gate-multi-replica-quota-correctness
plan: 01
subsystem: infra
tags: [kubernetes, helm, orbstack, quota, live-gate, bash, keda]

# Dependency graph
requires:
  - phase: 52-quota-enforcement-intra-class-fairness
    provides: "internal/jobs/admission.go's advisory-lock + same-transaction COUNT admission mechanism (the thing this gate proves, never edits)"
  - phase: 54-per-class-capacity-floor-seed-011
    provides: "keda.<class>.minReplicaCount pattern this overlay's keda.enabled:false key sits alongside in values-*.yaml"
provides:
  - "deploy/chart/octoconv/values-multireplica.yaml: gate-only Helm overlay (api.replicas:2, relaxed RATE_LIMIT_*, keda.enabled:false)"
  - "scripts/quota-multireplica-liveproof.sh Part A: header, preflight, two-phase install, replica/endpoint/pod-set assertions, ClusterIP-direct calibration (--calibrate-only runnable end to end), DB ground truth, client provisioning, drain helpers"
affects: ["55-02 (round engine, appends to the same script)", "55-03 (10-round burst execution)", "55-05 (operator-gated live run)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "ClusterIP-direct dial instead of kubectl port-forward for burst traffic, so kube-proxy's real per-connection DNAT distributes across both api pods (every prior live gate in this repo used port-forward, which pins to one pod and was safe only because api.replicas was always 1)"
    - "One curl PROCESS per request (never a reused connection) so each request gets a fresh TCP connection and a fresh kube-proxy DNAT decision"
    - "Two-phase helm install (--set api.replicas=1 first, then upgrade into the overlay's replicas:2) to avoid two API pods racing the unlocked db.Migrate on a fresh database"
    - "DB ground truth via psql_q/inflight_count mirroring internal/jobs/admission.go's exact concurrency predicate, rather than asking the API (the component under test) whether its own cap is free"

key-files:
  created:
    - deploy/chart/octoconv/values-multireplica.yaml
    - scripts/quota-multireplica-liveproof.sh
  modified: []

key-decisions:
  - "Reach the api Service by ClusterIP (kubectl get svc api -o jsonpath='{.spec.clusterIP}'), never by kubectl port-forward, because port-forward pins to a single backend pod and would make the cross-pod admit/reject assertion in 55-02/55-03 permanently unwinnable"
  - "Two-phase helm install: --set api.replicas=1 first (single pod runs the unlocked db.Migrate), then helm upgrade into values-multireplica.yaml's real replicas:2"
  - "keda.enabled:false in the overlay removes all 7 ScaledObjects and any scale-from-zero cold-start variance from the gate's 10-round timing budget -- autoscaling is irrelevant to proving quota correctness"
  - "Calibration (20 one-process-per-request curl probes to /healthz, tagged X-Request-Id: calib-<run>-<i>, cross-checked against both pods' kubectl logs) runs BEFORE any of the 10-round burst budget is spent, per RESEARCH.md's P0 assumption A1"
  - "psql_q/inflight_count query 'status IN ('\\''queued'\\'','\\''active'\\'')' directly against Postgres, mirroring admission.go's own predicate, because the API itself is the component under test and cannot be its own ground truth"

patterns-established:
  - "Gate-only Helm overlay register: layered on values-local.yaml (never standalone), documents the exact invocation and the migration-race constraint in its own header (mirrors values-e2e.yaml/values-loadproof.yaml, diverges deliberately from values-capacity-floor.yaml's bare-defaults layering)"
  - "Live-gate script split across two plans appending to the SAME file: 55-01 owns setup/calibration/provisioning/drain helpers, 55-02 appends the round engine that calls them"

requirements-completed: [QUOT-05]

# Metrics
duration: ~35min
completed: 2026-08-06
---

# Phase 55 Plan 01: Multi-Replica Gate Overlay + Harness Part A Summary

**Gate-only Helm overlay (api.replicas:2, relaxed rate limits, KEDA off) plus a bash live-gate harness that reaches the api Service by ClusterIP-direct dial (never kubectl port-forward) and proves via a 20-probe calibration step that host->ClusterIP routing genuinely distributes across both api pods before any of the 10-round quota burst budget is spent.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-08-06T17:19:24Z
- **Tasks:** 3 (all `type="auto"`)
- **Files modified:** 2 (both newly created)

## Accomplishments

- Created `deploy/chart/octoconv/values-multireplica.yaml`: a three-key gate-only overlay (`api.replicas: 2`, `api.extraEnv.RATE_LIMIT_*`, `keda.enabled: false`) that renders zero ScaledObjects, a static single-replica worker, and an api Deployment at 2 replicas with relaxed rate limits — verified via `helm lint`/`helm template` against both the overlay and (unaffected) the default local render.
- Created `scripts/quota-multireplica-liveproof.sh` (534 lines), the operator-run live-gate harness for QUOT-05, covering everything through a fully runnable `--calibrate-only` mode:
  - Header documents what SC1/SC2 prove, explicitly names `TestConcurrentBurstAdmitsExactlyK` as the in-process analogue this gate does NOT duplicate, states the out-of-scope boundary (daily caps, CI, admission.go edits), and warns future editors about the ClusterIP-vs-port-forward divergence and the migration-race constraint.
  - Preflight: cluster reachability, required tools (`helm`/`kubectl`/`curl`/`awk`/`mkfifo` — deliberately NOT `jq`), fixture presence, and the compose-must-be-down guard.
  - Two-phase install: `helm upgrade --install ... --set api.replicas=1` first (avoids two API pods racing the unlocked `db.Migrate` on a fresh database), waits for all 5 Deployments Available, then a second `helm upgrade` with no `--set` picks up the overlay's real `api.replicas: 2`.
  - Asserts `spec.replicas: 2`, polls to 2 ready Service endpoints (with an EndpointSlice fallback), and records the starting 2-pod set to a workdir file for Plan 55-02's end-of-run pod-set-unchanged check.
  - Calibration: discovers the api Service ClusterIP, fires 20 backgrounded one-process-per-request `curl` probes at `/healthz` tagged with unique `X-Request-Id` values, and cross-checks both api pods' `kubectl logs` output to prove the burst genuinely lands on both pods before any quota burst budget is spent — this closes RESEARCH.md's single highest-risk open item (assumption A1).
  - `--calibrate-only` exits 0 here (teardown still runs via the trap); everything below runs only in the full-gate path.
  - DB ground truth (`psql_q`, `inflight_count`) mirrors `internal/jobs/admission.go`'s exact `status IN ('queued','active')` predicate — asking Postgres directly rather than the API under test.
  - Mints a fresh enforce-mode client per run, sets `max_concurrent=$QUOTA_K` with unlimited daily bytes/jobs, and reads back all four anti-vacuity facts (max_concurrent, both daily-limit NULLs, `enforcement_mode`, and the `QUOTA_ENFORCEMENT_DISABLED` kill-switch read live from an api pod) before trusting the client is actually enforcing.
  - Drain helpers (`job_status`, `cancel_job`, `wait_terminal`, `drain_round`) give Plan 55-02 a verified-zero-inflight drain between rounds, closing the "later rounds silently pass vacuously" pitfall named in CONTEXT.md D-06.

## Task Commits

1. **Task 1: Gate-only Helm overlay values-multireplica.yaml** - `765ab8b` (feat)
2. **Task 2: Gate script Part A — header, preflight, two-phase install, ClusterIP calibration** - `8ddbcef` (feat)
3. **Task 3: Gate script Part A — DB ground-truth access, throwaway client provisioning, drain helpers** - `a682e1a` (feat)

_No TDD tasks in this plan (bash/YAML infra, not Go source with `tdd="true"`)._

## Files Created/Modified

- `deploy/chart/octoconv/values-multireplica.yaml` - Gate-only overlay: `api.replicas: 2`, `api.extraEnv` RATE_LIMIT relaxation, `keda.enabled: false`
- `scripts/quota-multireplica-liveproof.sh` - Live-gate harness Part A (setup/calibration/provisioning/drain helpers; Part B/round-engine is Plan 55-02)

## Decisions Made

See `key-decisions` in frontmatter above. No decisions departed from CONTEXT.md/RESEARCH.md/PATTERNS.md — every choice (ClusterIP-direct reachability, two-phase install, `keda.enabled: false`, DB-ground-truth predicate, fresh-client-per-run) was already locked or recommended by planning documents and confirmed against source during this plan's own `facts_verified_during_planning`.

## Deviations from Plan

None - plan executed exactly as written. All acceptance-criteria greps, `helm lint`/`helm template` checks, `shellcheck -S error`, and `bash -n` were run and passed for every task; no auto-fixes were needed.

## Issues Encountered

None. The worktree's git branch had drifted from the expected merge-base at agent startup (a stale prior-session commit was present); this was corrected via the mandatory `<worktree_branch_check>` `git reset --hard` to the orchestrator-supplied base commit before any file was touched — not a deviation from the plan's content, just the required pre-flight safety step.

## Script Interface for Plan 55-02 (continuing the SAME script file)

Plan 55-02 is a **different agent, appending to `scripts/quota-multireplica-liveproof.sh`** after this plan's Task 3 content (the file currently ends with the `drain_round()` function, line 534). The exact interface it inherits:

**Globals already set by the time Part B's code runs** (i.e. after the `if [ -n "$CALIBRATE_ONLY" ]; then ... fi` gate at line 373-379, which only Part A's calibration path can trigger an early exit through):
- `NAMESPACE`, `CHART_DIR`, `VALUES_LOCAL`, `VALUES_MULTIREPLICA`, `ENGINE` (`image`), `TARGET_FORMAT` (`jpg`), `FIXTURE` (`internal/e2e/testdata/sample.png`), `FIXTURE_CONTENT_TYPE` (`image/png`), `QUOTA_K` (`2`), `BURST_N` (`10`), `ROUNDS` (`10`), `DB_LOCAL_PORT`, `API_KEY_SALT`, `EVIDENCE_DIR`, `RUN_ID` — all declared in the config block (lines 74-88), all env-overridable.
- `WORKDIR` — the `mktemp -d` scratch dir, removed unconditionally by the `teardown` trap; per-request status/body files should go here (e.g. `$WORKDIR/round-${ROUND}-req-${i}.json` / `.status`), matching the pattern already used for calibration (`$WORKDIR/calib-${i}.status`).
- `CLUSTER_IP` — the api Service's discovered ClusterIP; Part B's burst curls MUST target `http://${CLUSTER_IP}:8090/v1/jobs`, never a port-forward.
- `API_POD_1` / `API_POD_2` — the two api pod names, with the starting sorted pair already written to `$WORKDIR/api-pods-start.txt` for Part B's end-of-run pod-set-unchanged assertion.
- `CLIENT_ID` / `CLIENT_KEY` — the minted, provisioned (`max_concurrent=$QUOTA_K`, `enforcement_mode=enforce`), anti-vacuity-verified throwaway client. `CLIENT_KEY` must only ever be printed through `assert_nonempty_redacted`, never raw.
- `DB_PF_PID` — the postgres port-forward PID, already killed by `teardown` on exit; Part B does not need to manage it.
- `PASS_COUNT` / `GATE_OK` — Part B must keep incrementing `PASS_COUNT` via the shared assertion helpers and set `GATE_OK=1` only after every required (non-optional) assertion across all `$ROUNDS` rounds has passed, mirroring this plan's `--calibrate-only` `GATE_OK=1` pattern (lines 373-378).

**Functions available to call:**
- `log "STEP N: ..."` — section banner (blank line + `--- ... ---`).
- `assert_eq expected actual label`, `assert_nonempty value label`, `assert_nonempty_redacted value label`, `assert_empty value label`, `assert_ge a b label`, `assert_le a b label` — every one increments `PASS_COUNT` on success and `exit 1`s with a `FAIL:` line on failure. `assert_le`/`assert_ge` are the ones Part B needs for "admitted count == K exactly" (`assert_eq`) and any range-style checks.
- `psql_q "<SQL>"` — runs a scalar query against the postgres pod via `kubectl exec`, trims all whitespace, returns the value on stdout.
- `inflight_count "$CLIENT_ID"` — returns the live `queued+active` count for `($CLIENT_ID, $ENGINE)`, mirroring `admission.go`'s own predicate.
- `job_status "$job_id"` — GETs `/v1/jobs/{id}` via `$CLUSTER_IP` with `$CLIENT_KEY`, returns the `status` field.
- `cancel_job "$job_id"` — POSTs `/v1/jobs/{id}/cancel`; tolerates non-2xx, never aborts.
- `wait_terminal "$job_id" timeout_s` — polls every 2s until `done`/`failed`/`canceled`; returns 0/1, echoes last status either way.
- `drain_round "$job_ids"` (space-separated list) — waits/cancels every job to terminal, then **asserts** `inflight_count` is `0` before returning; this is the exact call Part B should make between rounds (per CONTEXT.md D-06's slot-drain requirement — do not reimplement this loop).

**Exit conventions:** `set -euo pipefail` is active for the whole file; any assertion failure exits 1 via the shared helpers. The `teardown()` trap (already installed at file top) runs unconditionally on any exit and uninstalls the helm release, deletes the namespace, and removes `$WORKDIR` — it does **not** touch `$EVIDENCE_DIR`. Part B must write any committed evidence (per-round CSV/JSON, timestamps, admitted/rejected counts, replica identifiers) into `$EVIDENCE_DIR`, not `$WORKDIR`, or it will be deleted on exit.

**Evidence file layout (Plan 55-02's responsibility to populate, not yet created by this plan):** `$EVIDENCE_DIR` is declared (`.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence`) but no file inside it exists yet — this plan's Task set never had 55-02's round-generation content. Plan 55-02 should create it (e.g. `mkdir -p "$EVIDENCE_DIR"`) before writing evidence, and name files by `$RUN_ID` to avoid clobbering a prior run's committed evidence.

**One thing Part B must NOT do:** re-derive the client, the ClusterIP, or the pod set — all three are already verified global state by the time Part B's code executes; redoing that work would silently duplicate the anti-vacuity readbacks this plan already performed.

## Self-Check: PASSED

- FOUND: `deploy/chart/octoconv/values-multireplica.yaml`
- FOUND: `scripts/quota-multireplica-liveproof.sh` (executable, 534 lines)
- FOUND commit `765ab8b` (Task 1)
- FOUND commit `8ddbcef` (Task 2)
- FOUND commit `a682e1a` (Task 3)
- `helm lint` / `helm template` (overlay + default-render regression check): PASSED
- `shellcheck -S error` / `bash -n`: PASSED
- All Task 1/2/3 acceptance-criteria greps: PASSED

## Next Phase Readiness

- Plan 55-02 can append the round engine directly to `scripts/quota-multireplica-liveproof.sh` after line 534, using the interface documented above.
- Plan 55-03 (10-round burst execution) and 55-05 (operator-gated live run) depend on 55-02's round engine existing; nothing in this plan blocks them beyond that dependency.
- No live cluster run was performed in this plan per its own scope (`<verification>`: "the live behavioral proof of this plan is the `--calibrate-only` run, executed by the operator in Plan 55-05 Task 1 Step 2") — this plan only performed static verification (`helm lint`/`template`, `shellcheck`, `bash -n`, scoped greps), exactly as specified.

---
*Phase: 55-live-gate-multi-replica-quota-correctness*
*Completed: 2026-08-06*
