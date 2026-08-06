---
phase: 55-live-gate-multi-replica-quota-correctness
plan: 02
subsystem: infra
tags: [kubernetes, bash, quota, live-gate, multi-replica]

# Dependency graph
requires:
  - phase: 55-live-gate-multi-replica-quota-correctness (plan 01)
    provides: "scripts/quota-multireplica-liveproof.sh Part A: header, preflight, two-phase install, replica/endpoint/pod-set assertions, ClusterIP-direct calibration, DB ground truth, client provisioning, drain helpers (fire_burst/assess_round/run_round call psql_q, inflight_count, job_status, cancel_job, wait_terminal, drain_round exactly as documented in 55-01-SUMMARY.md's interface contract)"
provides:
  - "scripts/quota-multireplica-liveproof.sh Part B: fire_burst() FIFO-barrier burst generator, assess_round() classification+attribution+six-assertion set, run_round() per-round sequencing, STEP 6 ten-round main loop, STEP 7 end-of-run global assertions, STEP 8 evidence summary -- the full gate is now complete end to end"
affects: ["55-03 (was folded into this plan's own 10-round loop -- see Deviations)", "55-05 (operator-gated live run consumes this script as-is)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "FIFO named-pipe barrier (mkfifo + blocking `read -u 3` + single `printf 'go' > barrier`) to release N backgrounded curl subshells together -- first use of mkfifo in this repo's scripts/"
    - "One curl PROCESS per burst request (never a reused connection) so kube-proxy's per-NEW-connection Service DNAT genuinely distributes across pods"
    - "Classification by response body `code` field (quota_exceeded vs rate_limited), never by bare HTTP status -- both are 429"
    - "Replica attribution by client-supplied X-Request-Id + one kubectl logs dump per pod per round, grepped BURST_N times rather than dumped per request"
    - "Every per-round assertion is exact-equality (== K, == N-K) rather than inequality, so a round cannot pass vacuously by under-admitting"

key-files:
  created: []
  modified:
    - scripts/quota-multireplica-liveproof.sh

key-decisions:
  - "Admits classify on HTTP 202 (internal/api/handlers.go:805's writeJSON(w, http.StatusAccepted, resp)), not 201 -- RESEARCH.md/PATTERNS.md's 201 examples were flagged wrong in the plan's own facts_verified_during_planning and confirmed again by direct source read during this plan"
  - "The D-05 cross-pod assertion requires a SPECIFIC admitted request and a SPECIFIC rejected request on DIFFERENT pods within the SAME round, not just 'both pods received traffic' -- the weaker form was rejected in CONTEXT.md D-05 and is repeated here as a comment at the assertion site"
  - "Admitted job ids are handed from assess_round to run_round via a WORKDIR file ($WORKDIR/r<round>-admitted.txt), not a function return value, because assess_round's own stdout is full of PASS/FAIL/WARN narration and using stdout as a data channel would have been fragile"
  - "Each pod's kubectl logs are dumped exactly once per round (not once per request) and grepped BURST_N times against the same dump -- avoids 2*BURST_N kubectl calls per round for no benefit"
  - "The non-fatal log-status-vs-client-status cross-check WARNs rather than FAILs on mismatch, because chi's access log line is written from a defer at request completion and a client-side --max-time timeout can observe a different (or no) status than what the server eventually recorded"
  - "RUN_ID doubles as the evidence summary's 'run start' timestamp (its default is already `date -u +%Y%m%dT%H%M%SZ`) rather than introducing a second timestamp variable -- keeps Part A's config-block ownership untouched except for the two new vars this plan's tasks explicitly call for (BURST_SEND_SPREAD_MS, EVIDENCE_TSV)"

patterns-established:
  - "Round-scoped assertion order: attribution completeness is asserted BEFORE any conclusion (cap exactness, cross-pod split, concurrency-of-flight) is drawn from the per-request pod attribution -- mirrors the plan's own explicit ordering requirement"
  - "End-of-run assertions are deliberately redundant with per-round hard-FAILs where the redundant form is re-derivable from the committed TSV alone by a reviewer who never re-runs the gate (e.g. the whole-run zero-rate_limited check, cross-pod coverage)"

requirements-completed: [QUOT-05]

# Metrics
duration: ~25min
completed: 2026-08-06
---

# Phase 55 Plan 02: Round Engine (FIFO Burst, Classification, Attribution, Assertions) Summary

**Completed `scripts/quota-multireplica-liveproof.sh` Part B: a FIFO-barrier burst generator (one curl process per request, released together), a per-round classifier that attributes each request to its serving pod via `X-Request-Id` + `kubectl logs` and applies six independently-failing assertions (attribution completeness, admit count == K exactly, N-K quota_exceeded rejections, cross-pod admit/reject split, send-spread, in-flight-at-max-send), a ten-round main loop, and end-of-run global assertions plus a committed evidence TSV — the gate script is now complete end to end (935 lines total, Part A + Part B).**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-08-06T17:45:12Z
- **Tasks:** 3 (all `type="auto"`)
- **Files modified:** 1 (`scripts/quota-multireplica-liveproof.sh`, appended to Plan 55-01's Part A)

## Accomplishments

- **Task 1 — `fire_burst()` + timestamp preflight:** added `BURST_SEND_SPREAD_MS` and `EVIDENCE_TSV` to the config block; added a `TS_TOOL` preflight assertion to STEP 1 (python3 preferred, perl's `Time::HiRes` fallback, since macOS `date` has no `%3N`); wrote `epoch_ms()` and `fire_burst(ROUND)` — a per-round `mkfifo` barrier releasing `BURST_N` backgrounded subshells, each running exactly one `curl` process (load-bearing: kube-proxy's Service DNAT choice is made once per NEW TCP connection, so a reused connection would silently collapse cross-pod distribution even with correct ClusterIP reachability). Initialized the evidence TSV with its header before round 1 can fire.
- **Task 2 — `assess_round()` + `run_round()`:** classification switches on `"$status:$code"` (202 admits — NOT 201, confirmed against `internal/api/handlers.go:805`; `429:quota_exceeded` rejects; `429:rate_limited` is an immediate hard FAIL naming the request id); attribution greps one `kubectl logs` dump per pod per round for each request's unique `X-Request-Id`, hard-failing on zero-matched-in-both (empty join) or matched-in-both (ambiguous join) BEFORE any conclusion is drawn from it; then asserts exact admit/reject counts (`==`, never `<=`), the D-05 cross-pod admit+reject-on-different-pods split, the burst send-spread, and the in-flight-at-max-send count. `run_round()` sequences pre-round zero-inflight → `fire_burst` → `assess_round` → `drain_round`, closing Pitfall 4 (slot-drain-between-rounds).
- **Task 3 — main loop + end-of-run assertions + evidence summary:** STEP 6 runs all `ROUNDS` rounds (the `ROUNDS_PASSED` counter is provably accurate because `set -e` means only a genuinely-passing round reaches the increment); STEP 7 asserts the api pod set is unchanged, zero `rate_limited` responses across the whole run, three aggregate totals (rows/202s/quota_exceeded 429s against `rounds*N`/`rounds*K`/`rounds*(N-K)`), and cross-pod coverage (both pods served both a 202 and a 429 somewhere in the run — strictly stronger than the per-round D-05 check); STEP 8 appends a `#`-commented evidence summary block to the TSV and prints the same to stdout, then the final `GATE_OK=1` banner reports rounds passed, assertions passed, and the evidence file's absolute path with an explicit `git add -f` instruction.

## Task Commits

1. **Task 1: FIFO-barrier burst generator with per-request timestamps** - `3a17650` (feat)
2. **Task 2: Per-round classification, replica attribution join, and the six failing assertions** - `5bdd9e6` (feat)
3. **Task 3: Ten-round main loop, end-of-run global assertions, and the evidence summary** - `ad78dea` (feat)
4. **Cleanup: drop unused idx field from assess_round's .rec read** - `70c42d9` (style, shellcheck SC2034 warning, not an acceptance-criteria requirement)

_No TDD tasks in this plan (bash infra, not Go source with `tdd="true"`)._

## Files Modified

- `scripts/quota-multireplica-liveproof.sh` — grew from 534 lines (Plan 55-01's Part A) to 935 lines; every new line is Part B (`epoch_ms`, `fire_burst`, `assess_round`, `run_round`, STEP 6/7/8, plus the `BURST_SEND_SPREAD_MS`/`EVIDENCE_TSV` config additions and the `TS_TOOL` preflight assertion).

## Decisions Made

See `key-decisions` in frontmatter above. All choices were already locked or directly implied by CONTEXT.md/RESEARCH.md/PATTERNS.md and this plan's own `facts_verified_during_planning`; the only genuinely new implementation choice this plan made was the admitted-ids handoff mechanism (WORKDIR file, not a function return value or stdout capture) — a mechanical detail left to Claude's discretion by the plan's action text.

## Deviations from Plan

**1. [Rule 1 — style, not a bug] Dropped an unused positional field from `assess_round`'s `.rec` read.**
- **Found during:** Task 2 verification (`shellcheck` full run, not the `-S error` gate — see below).
- **Issue:** `IFS=$'\t' read -r idx request_id send_ts recv_ts status <"$rec"` read the `.rec` line's leading index field into `idx`, which was never referenced afterward (the loop's own `$i` is what `assess_round` iterates on).
- **Fix:** Read that field into `_` instead of `idx`.
- **Files modified:** `scripts/quota-multireplica-liveproof.sh`.
- **Commit:** `70c42d9`.
- **Note:** This was a `shellcheck` SC2034 *warning*, not an `-S error` finding — the plan's stated verification gate (`shellcheck -S error`) already passed cleanly before this cleanup. Fixed anyway for code quality; not required by any acceptance criterion.

**No other deviations.** All three tasks' acceptance-criteria greps were run verbatim and passed; no Rule 2/3/4 situations arose during implementation.

## Task 2 Acceptance Criterion: Mutation-Test Reasoning Check (static, no live run)

The plan's Task 2 acceptance criteria required a documented reasoning check: mutate `QUOTA_K`'s default to `3` in a scratch copy and confirm (a) `bash -n` still passes and (b) the exact-equality admit assertion now compares against `3`. Performed exactly as specified:

```
$ sed -i '' 's/QUOTA_K="${QUOTA_K:-2}"/QUOTA_K="${QUOTA_K:-3}"/' <scratch copy>
$ bash -n <scratch copy>   # exit 0
```
`assert_eq "$QUOTA_K" "$admit_count" "round ${round}: admitted count"` (line 763 in the mutated copy) now compares the live-observed `admit_count` against `3` instead of `2` — confirming the assertion is not a dead/hardcoded check but genuinely reads its expected value from the same `QUOTA_K` variable a reader would change.

**Honest caveat, not glossed over:** `QUOTA_K` parameterizes *both* the assertion's expected value *and* the real enforced limit (Plan 55-01's STEP 5 calls `manage-clients set-limit "$CLIENT_ID" "$ENGINE" "$QUOTA_K"`), so this specific mutation does not decouple "what the harness expects" from "what the server actually enforces" — a live run against the mutated script would still pass, because the server would genuinely admit 3. That is by design, not a gap: the assertion's real fault-detection target is a live admission-mechanism bug that lets `admit_count` diverge from *whatever K was actually configured* (e.g., an atomicity break under concurrent replicas letting `K+1` through) — exactly the QUOT-05 claim this gate exists to test — not a harness-internal off-by-one. The static check above proves the assertion is genuinely parameterized (not tautological); proving it can *fail against a real bug* requires the live run, which is explicitly out of scope for this plan (`no live run required at this stage`, per the plan's own acceptance criterion wording) and is Plan 55-05's job.

## `.gitignore` / Evidence-Committability: Documented Plan/Reality Mismatch (not auto-fixed)

Task 3's acceptance criteria included: `git check-ignore -q .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/probe.tsv; test $? -ne 0` (i.e., the evidence path must NOT be gitignored). Run verbatim against this repo's actual `.gitignore`:

```
$ git check-ignore -q .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/probe.tsv
$ echo $?
0   # IS ignored -- the criterion as literally written does not hold
```

**Root cause:** `.gitignore:7` is a blanket `/.planning/` exclusion, added in commit `11bc7c3` ("chore: untrack .planning/ from git (internal GSD planning artifacts, not part of the product)") — a deliberate, already-merged project decision, not an oversight. Git's directory-level exclusion means no child-path negation pattern (`!/.planning/phases/*/evidence/`) can re-include files under it (verified empirically in a scratch repo during this plan: even a 4-level negation stack stayed ignored, matching git's documented "cannot re-include a file if a parent directory is excluded" limitation).

**Why this is NOT a Rule 3 auto-fix:** unwinding `/.planning/`'s blanket exclusion (or any negation-based workaround) would reverse a deliberate, already-merged repo-wide policy decision — squarely a Rule 4 (architectural/policy) territory, not a task-scoped blocking bug. It is also unnecessary: `git ls-files | grep evidence/` shows every prior live-gate's evidence artifact in this repo (`46-`, `50-keda-helm-chart-integration/evidence/*`) is already tracked despite this same `/.planning/` pattern — proving the established, working precedent is `git add -f` (force-add), which bypasses `.gitignore` entirely without touching it. The script's own STEP 8 banner already instructs exactly this: `git add -f ${EVIDENCE_TSV_ABS}`.

**Resolution:** left `.gitignore` untouched (matches project precedent and avoids an out-of-scope policy reversal); documented the force-add requirement in the script's final banner (Task 3) and here. Plan 55-05 (the operator-gated live run) should force-add the resulting evidence TSV, exactly as every prior live-gate plan in this repo already does.

## Known Stubs

None. No hardcoded empty values, placeholder text, or unwired data sources were introduced — the entire script is executable bash logic, verified statically (`shellcheck -S error`, `bash -n`, and every task's scoped acceptance-criteria greps).

## Threat Flags

None. This plan added no new production HTTP surface, no new network endpoints, no new auth paths, and no schema changes — it only extended an operator-run bash script that reaches the already-existing `/v1/jobs` endpoint with an already-provisioned throwaway client's own API key. All five threats registered in the plan's own `<threat_model>` (T-55-06 through T-55-10) are mitigated by code already reviewed above (evidence TSV shape, `RUN_ID`-scoped request ids, `429:rate_limited` hard-fail + zero-count end-of-run assertion, client-id-only summary/no raw key or DB password ever written to evidence, fresh-client-per-run + pre/post-round zero-inflight assertions).

## Static Verification Performed

- `shellcheck -S error scripts/quota-multireplica-liveproof.sh` — PASSED (exit 0) after every task.
- `bash -n scripts/quota-multireplica-liveproof.sh` — PASSED (exit 0) after every task.
- `shellcheck` (full, no `-S` filter) — PASSED (exit 0) after the Task 2 cleanup commit (one SC2034 warning fixed, not required by the acceptance gate).
- `scripts/quota-multireplica-liveproof.sh --nonsense` — exits 1 with a usage line (Task 3 acceptance criterion).
- Zero `port-forward.*svc/api` matches anywhere in the finished script (`grep -v '^[[:space:]]*#' ... | grep -c 'svc/api'` → `0`).
- At least one `spec.clusterIP` reference present (Plan 55-01's STEP 4 discovery call).
- All nine required functions present (`fire_burst`, `assess_round`, `run_round`, `drain_round`, `inflight_count`, `psql_q`, `job_status`, `cancel_job`, `wait_terminal`).
- Every task's own scoped acceptance-criteria `grep` commands (mkfifo count, `X-Request-Id: burst-` interpolation, `429:rate_limited`/`201:` literals, `assert_le`/`assert_ge`/`assert_eq "$QUOTA_K"` presence, `api-pods-start.txt` occurrence count) — all PASSED.
- **NOT performed (explicitly out of scope per this plan and the executor's own constraints):** any live cluster run. `helm`/`kubectl` against a real OrbStack cluster is Plan 55-05's job, operator-gated.

## Next Phase Readiness

- `scripts/quota-multireplica-liveproof.sh` is now complete end to end (935 lines) — Plan 55-05 (the operator-gated live run) can invoke it directly with no further script changes expected.
- **Operator invocation (for Plan 55-05):**
  ```
  bash scripts/quota-multireplica-liveproof.sh
  ```
  (or `--calibrate-only` for just the Plan 55-01 calibration step). All parameters are env-overridable with the documented defaults: `QUOTA_K=2`, `BURST_N=10`, `ROUNDS=10`, `BURST_SEND_SPREAD_MS=1000`, `ENGINE=image`, `TARGET_FORMAT=jpg`, `NAMESPACE=octoconv`, `EVIDENCE_DIR=.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence`. Preconditions per the header: compose stack DOWN, `:dev`-tagged images pre-built sequentially, OrbStack Kubernetes up. Expected runtime: dominated by 10 rounds × (a `wait_terminal` drain budget of up to 180s per admitted job, typically far faster on the 86-byte `sample.png` fixture) — budget on the order of 10-20 minutes total, generously.
- **Evidence file layout:** `.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/quota-multireplica-<RUN_ID>.tsv`, RUN_ID formatted `%Y%m%dT%H%M%SZ`. Columns: `round, index, request_id, send_ts_ms, recv_ts_ms, http_status, body_code, serving_pod` (one row per burst request, `10*10=100` data rows for the defaults), followed by a `#`-commented summary block (run id/end timestamp, values files, live-observed `api.replicas`, both pod names, client id, engine, K/N/rounds, rounds passed, three aggregate totals, four cross-pod coverage counts).
- **Committing the evidence (operator action, Plan 55-05):** the TSV lands under `.planning/`, which is blanket-gitignored (see the dedicated section above) — commit with `git add -f <path printed in the final GATE PASS banner>`, exactly as every prior live-gate evidence artifact in this repo was committed.
- Nothing in this plan blocks 55-05 beyond the live cluster itself being available.

## Self-Check: PASSED

- FOUND: `scripts/quota-multireplica-liveproof.sh` (executable, 935 lines)
- FOUND commit `3a17650` (Task 1)
- FOUND commit `5bdd9e6` (Task 2)
- FOUND commit `ad78dea` (Task 3)
- FOUND commit `70c42d9` (cleanup)
- `shellcheck -S error` / `bash -n`: PASSED
- All Task 1/2/3 acceptance-criteria greps: PASSED
- `git check-ignore` evidence-committability check: documented as a known, deliberate plan/reality mismatch (see dedicated section above), not silently glossed over

---
*Phase: 55-live-gate-multi-replica-quota-correctness*
*Completed: 2026-08-06*
