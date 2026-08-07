---
phase: 55-live-gate-multi-replica-quota-correctness
plan: 07
subsystem: infra
tags: [kubernetes, bash, postgres, quota, live-gate, multi-replica, gap-closure]

# Dependency graph
requires:
  - phase: 55-live-gate-multi-replica-quota-correctness (plan 06)
    provides: "scripts/quota-burst-driver.sh (the in-pod burst/calibration driver) and the rewired scripts/quota-multireplica-liveproof.sh (burst-pod lifecycle, asset transfer, in-cluster calibration) this plan builds directly on top of"
provides:
  - "scripts/fixtures/gen_heavy_png.py + scripts/fixtures/quota-burst-heavy.png: a deterministic stdlib-only generator and its committed 9000x9000 (81 MP), 259,682-byte output that converts png->webp in seconds, not milliseconds, in the real octoconv-worker:dev image"
  - "scripts/quota-burst-driver.sh hardened: EPOCHREALTIME-based fork-free ms_now helper, api key read once before the fork loop, a deterministic ready-file readiness handshake replacing the blind pre-release sleep -- closing the round-4 straggler at its source"
  - "scripts/quota-multireplica-liveproof.sh hardened: THE INVARIANT (BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR <= MIN_ENGINE_WALL_MS) preflight, app-image freshness preflight (presence + build-time-after-newest-migration + api-binary-embeds-migration-name + worker-has-vips) before any cluster mutation, a Postgres clock-skew check, and the terminal_before/min_job_wall_ms/final_statuses discriminating assertions run BEFORE the exact-K cap assertion every round, plus a second committed timeline TSV"
affects: ["55-05 (operator-gated live re-run; runs against the same invocation with new defaults -- TARGET_FORMAT=webp, FIXTURE=the heavy PNG, BURST_SEND_SPREAD_MS=250 -- and a materially longer expected wall-clock runtime)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PNG raster designed for asymmetric cost: identical Up-filtered scanlines compress to near-nothing under DEFLATE while the decoded raster the converter must handle stays the full width*height*3 bytes -- 'small on the wire, expensive to convert' as a deliberate fixture property, not an accident of a large file"
    - "Discriminating assertion run BEFORE the assertion it could explain away: terminal_before (a Postgres read) executes strictly before assert_eq QUOTA_K admit_count, so an over-admitting round is diagnosed (legitimate slot reuse vs. real cap violation) rather than merely reported"
    - "Fork-free per-request timestamps via bash's EPOCHREALTIME, read into a local variable exactly once and split by parameter expansion -- replacing a `date` fork on the post-barrier critical path"
    - "Readiness handshake via marker files + parent-side polling with a hard timeout, replacing a blind fixed sleep before a FIFO barrier release -- provable rather than merely probable"
    - "Preflight-time app-image freshness check derived from embedded migration filenames (Go's `embed` package puts migration filenames literally in the api binary) -- a semantically exact probe for a stale-image defect that otherwise surfaces five minutes into a live run as an unrelated-looking SQL error"

key-files:
  created:
    - scripts/fixtures/gen_heavy_png.py
    - scripts/fixtures/quota-burst-heavy.png
  modified:
    - scripts/quota-burst-driver.sh
    - scripts/quota-multireplica-liveproof.sh

key-decisions:
  - "MIN_ENGINE_WALL_MS set to 5933 (this host's own measured minimum png->webp `vips copy` wall time), not the plan's planning-time estimate of 5194 -- Critical Constraint #4 requires using and reporting the executor's own measurement when it disagrees materially with the plan's numbers"
  - "Two of this task's own newly-written diagnostic messages were reworded (not the plan's pre-existing text) to remove literal mentions of 'sample.png' and the phrase 'admitted count', because those mentions were new code this task introduced that happened to collide with its own file-scoped acceptance-criteria greps -- rewording new code to satisfy a grep is not the same as degrading pre-existing documentation, so no plan-defect was recorded for these two"
  - "Verified the invariant negative test's arithmetic in isolation rather than by invoking the real gate script: this execution environment's kubectl can technically reach an OrbStack k8s node, but Critical Constraint #3 explicitly forbids pointing kubectl at any cluster for this plan (the live run is operator-gated in 55-05) -- the plan's own text anticipated exactly this case ('if your environment cannot reach a cluster at all... run the invariant arithmetic in isolation and say so plainly')"

patterns-established:
  - "Cluster-free negative testing of new SQL against a throwaway, already-local postgres:18 container -- created minimal jobs/job_events tables, inserted before/after fixture rows, ran the exact production SQL text, removed the container immediately after"

requirements-completed: [QUOT-05]

# Metrics
duration: ~50min
completed: 2026-08-07
---

# Phase 55 Plan 07: Make the Exact-K Assertion Sound Summary

**Committed a 9000x9000/259,682-byte PNG fixture (measured 5933-6549ms png->webp in the real worker image) plus a deterministic generator, hardened the burst driver's post-barrier path to remove every fork but curl itself, and added an image-freshness preflight, a spread/engine-time invariant, and per-round Postgres discriminating assertions to the live-gate harness — making the `admitted == K` assertion's premise checked from Postgres every round instead of assumed, closing the exact gap the 2026-08-07 round-4 failure exposed.**

## Performance

- **Duration:** ~50 min (estimated from commit timestamps: first task commit 18:00:49 +03:00, last 18:23:03 +03:00, plus setup/reading time before the first commit; exact start epoch was not captured)
- **Completed:** 2026-08-07T15:23:12Z
- **Tasks:** 3 (all `type="auto"`)
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- **Task 1 — heavy burst fixture:** `scripts/fixtures/gen_heavy_png.py`, a stdlib-only (argparse/struct/zlib/os/sys) deterministic PNG generator, and its committed output `scripts/fixtures/quota-burst-heavy.png` (9000x9000, 81,000,000 px, **259,682 bytes** — matches the plan's own measured byte count exactly). Regenerating the fixture with the same arguments produces a byte-identical file (`cmp` confirms). Measured on this host, inside the real `octoconv-worker:dev` image at the chart's own `--cpus=2 --memory=1g`: three serial `vips copy` png->webp runs at **6549ms / 5933ms / 6001ms** (all comfortably above the plan's 3000ms floor), two parallel conversions completed in **9528ms** wall with both outputs non-zero (478824 bytes each — no OOM signature, which the plan documented at 384MiB). `vips --version` reports `vips-8.14.1`.
- **Task 2 — burst driver hardening:** removed every avoidable fork from the post-barrier critical path in `scripts/quota-burst-driver.sh`. Added `export LC_ALL=C`, an `EPOCHREALTIME` preflight, and a fork-free `ms_now` helper (single `EPOCHREALTIME` read, split by parameter expansion) used for both `send_ts` and `recv_ts` — no `date` fork remains inside the per-request subshell. The api key is now read once before the fork loop into a shell variable, removing a `cat` fork per request. The blind `sleep 0.3` before the barrier release is replaced with a deterministic readiness handshake: each subshell marks a `$SCRATCH/ready-$i` file (an empty-redirection builtin, no fork) as its last action before blocking on the FIFO; the parent polls (10s hard timeout) until all `COUNT` ready-files exist before opening the barrier for writing.
- **Task 3 — harness hardening:** rewrote the `BURST_SEND_SPREAD_MS` config comment to state the invariant plainly (250, ~20x under the measured minimum engine wall time); added `SPREAD_SAFETY_FACTOR`, `MIN_ENGINE_WALL_MS`, `JOB_WALL_FLOOR_MS`, `APP_IMAGE_TAG`; STEP 1 now hard-fails unless `BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR <= MIN_ENGINE_WALL_MS` and unless the fixture is >=100000 bytes, and runs an app-image freshness preflight (presence, build-time-after-newest-migration via a reusable `image_build_epoch` helper, api-binary-embeds-newest-migration-name, worker-has-vips) before any cluster mutation. STEP 4's burst pod CPU raised from `100m`/`500m` to `500m`/`"2"`. STEP 5 adds a Postgres clock-skew check and three new SQL helpers (`terminal_before`, `min_job_wall_ms`, `final_statuses`) plus a second committed timeline TSV. `assess_round` now calls `terminal_before` **before** the exact-K cap assertion (line 1285 vs. line 1302 — confirmed by `grep -n`), failing loudly with a per-job timeline dump if any admitted job went terminal early. `run_round` now asserts, after drain, that the minimum admitted-job wall time is both `>= JOB_WALL_FLOOR_MS` and `>= SPREAD_SAFETY_FACTOR * that round's spread`, and that every admitted job's final status is exactly `done`. `internal/jobs/admission.go` is untouched (`git diff` empty).

## Task Commits

1. **Task 1: Heavy burst fixture — deterministic stdlib-only generator plus the committed 9000x9000 PNG** - `0e15efb` (feat)
2. **Task 2: Burst driver — deterministic readiness handshake and fork-free send timestamps** - `9db21a2` (feat)
3. **Task 3: Harness — image-freshness preflight, spread/engine-time invariant, per-round Postgres discriminating assertions** - `5473983` (feat)

_No TDD tasks in this plan (bash/Python infra, not Go source with `tdd="true"`)._

## Files Created/Modified

- `scripts/fixtures/gen_heavy_png.py` (created) — deterministic stdlib-only PNG generator; docstring records the calibration target, the measured numbers, the `MAX_IMAGE_PIXELS` ceiling math, and the D3 divergence from `gen_heavy_docx.py`'s committed-vs-generated policy
- `scripts/fixtures/quota-burst-heavy.png` (created, 259,682 bytes) — the committed 9000x9000 burst fixture
- `scripts/quota-burst-driver.sh` (modified, 251 -> 314 lines) — `LC_ALL=C`, `EPOCHREALTIME` preflight + `ms_now`, api-key-read-once, readiness handshake, header addition
- `scripts/quota-multireplica-liveproof.sh` (modified, 1153 -> 1556 lines) — config changes, THE INVARIANT, app-image freshness preflight, burst pod CPU raise, clock-skew check, three SQL helpers, discriminating assertion in `assess_round`, post-drain wall/status assertions and timeline append in `run_round`, STEP 8 additions, header section

## Decisions Made

See `key-decisions` in frontmatter above. Summarized:

1. **`MIN_ENGINE_WALL_MS=5933`, this executor's own measured minimum, not the plan's 5194.** The plan's own facts block measured 5194–5561ms on the planning host; this host measured 6549/5933/6001ms — materially slower but still well above the 3000ms floor the plan requires before proceeding. Per Critical Constraint #4, the disagreement is recorded here rather than silently adopting the plan's numbers, and the constant used is the genuinely-measured value from this execution.
2. **Two of this task's own new diagnostic strings were reworded to avoid literal-string collisions with their own acceptance-criteria greps** (see Deviations below) — this is distinct from the plan's Critical Constraint #5 (which forbids rewording *pre-existing* documentation/comments to satisfy a grep): the two strings in question were written by this task itself, so adjusting their wording is normal editing, not degrading anything that existed before this task ran.
3. **The invariant negative test was verified as isolated arithmetic, not via the live script.** This execution environment's `kubectl` can technically reach an OrbStack Kubernetes node (`orbstack` context), but Critical Constraint #3 is explicit and absolute: no live cluster is available to this executor, and pointing `kubectl` at any cluster is out of scope — Plan 55-05 is the operator-gated live re-run. The plan's own acceptance-criteria text anticipated this exact situation and named the fallback used here.

## Deviations from Plan

### Auto-fixed Issues

None — no bugs, missing critical functionality, or blocking issues were found in the existing code. Every change in this plan is new functionality specified by the plan itself.

### Plan-defect findings (documented, not worked around, per Critical Constraint #5)

**A. Task 1's stdlib-import-check one-liner throws `AttributeError` on any Python file with a plain `import x` statement, independent of file content.** The plan's exact command:
```
python3 -c "import ast,sys; t=ast.parse(open('scripts/fixtures/gen_heavy_png.py').read()); mods={(n.module or n.names[0].name).split('.')[0] for n in ast.walk(t) if isinstance(n,(ast.Import,ast.ImportFrom)) for _ in [0]}; print(sorted(mods)); assert mods <= {'argparse','struct','zlib','os','sys'}"
```
fails with `AttributeError: 'Import' object has no attribute 'module'` — `ast.Import` nodes never carry a `.module` attribute (only `ast.ImportFrom` does); this reproduces on a trivial one-line `import os` file, confirming it is a defect in the plan's own check, not in the generator. A corrected equivalent (branching on node type before reading `.module`) was run instead and confirms the generator imports only `argparse`, `os`, `struct`, `sys`, `zlib`:
```python
mods = set()
for n in ast.walk(t):
    if isinstance(n, ast.Import):
        for alias in n.names: mods.add(alias.name.split('.')[0])
    elif isinstance(n, ast.ImportFrom):
        if n.module: mods.add(n.module.split('.')[0])
```
Output: `['argparse', 'os', 'struct', 'sys', 'zlib']` — PASS.

**B. Task 3's grep-based acceptance checks were run through this shell session's `grep` function wrapper, which is `ugrep 7.5.0` in `-G` (basic-regex) mode via `ARGV0=ugrep`, not standard GNU/BSD grep.** `ugrep -G` has a regex quirk where a mid-pattern `${` sequence (e.g. `curl "${CURL_ARGS[@]}"`) fails to match literally even though the identical pattern matches under `command grep` (the real system grep). Confirmed directly: `grep -n 'a${b' <file-containing-literal-a${b>` returns no match while `command grep -n 'a${b' <same-file>` returns a match. **This is an environment/tooling quirk of this session's shell wrapper, not a defect in the script.** All Task 2/3 acceptance-criteria greps were re-run through `command grep` (bypassing the wrapper) to get correct results; the numbers reported throughout this SUMMARY are from `command grep`.

**C. Task 2's `grep -c 'date +%s%3N'` prints `2`, not the plan's implied `1`.** The one surviving real invocation is the preflight probe (`CLOCK_PROBE="$(date +%s%3N)"`); the second occurrence is inside a pre-existing FAIL message (unchanged by this task, present in `HEAD` before any edit) that legitimately quotes the literal string `date +%s%3N` in its own diagnostic text (`echo "FAIL: ... got '$CLOCK_PROBE' from 'date +%s%3N', expected exactly 13 digits) ..."`). Confirmed via `git show HEAD:scripts/quota-burst-driver.sh | grep -n 'date +%s%3N'` that this message predates this task. Left unedited: rewording a pre-existing, still-accurate diagnostic message purely to satisfy a grep count would be exactly the kind of documentation-degradation Critical Constraint #5 warns against.

### New-code wording adjustments (not plan defects — see Decisions #2 above)

Two of *this task's own newly-written* strings were reworded during Task 3, because they were new prose that happened to collide with their own file's acceptance-criteria greps:
- The FAIL/PASS messages for the new fixture-byte-size check originally named `internal/e2e/testdata/sample.png` literally, colliding with the "sample.png no longer referenced by any executable line" criterion (which is a real requirement — the file genuinely should no longer be named in this script's runtime output). Reworded to "the previously-used 86-byte one" / "a too-small fixture" without losing the diagnostic reasoning.
- The new SC1 completion banner originally repeated the exact phrase "admitted count", colliding with the "existing per-round assertion label counts exactly once" criterion (whose true intent is the `assess_round` label, not incidental prose in an unrelated summary banner). Reworded to "exactly K jobs admitted" — same meaning, no collision.

---

**Total deviations:** 0 auto-fixed bugs/missing-functionality. 3 plan-defect findings recorded (A, B, C above) plus 2 new-code wording adjustments made during the same task that introduced the colliding text. **Impact on plan:** None of the findings required contorting the plan's actual implementation; all are either genuine tooling/plan-text mismatches (recorded per Critical Constraint #5) or ordinary self-consistency edits to brand-new prose.

## Issues Encountered

None beyond the plan-defect findings and wording adjustments documented above. No authentication gates. No architectural (Rule 4) decisions were required — every change stayed within the plan's own explicit design decisions (D1-D8).

## Known Stubs

None. `scripts/fixtures/gen_heavy_png.py` is a complete, working generator (verified deterministic and stdlib-only). Both shell scripts are executable logic verified with `bash -n`, `shellcheck -S error`, and the plan's own required local/cluster-free behavioral tests (docker-run driver trials, throwaway-postgres SQL negative tests, image-freshness helper pass/forced-fail).

## Threat Flags

None beyond what the plan's own `<threat_model>` already registers (T-55-31 through T-55-37, T-55-SC — all mitigated exactly as specified: the fixture stays 19% under `MAX_IMAGE_PIXELS`, the memory floor is well under the chart's 1Gi limit, `SPREAD_SAFETY_FACTOR`/`MIN_ENGINE_WALL_MS` are asserted rather than assumed, the timeline artifact carries only job UUIDs/timestamps/status strings, both evidence filenames derive from the same `RUN_ID`, the image-freshness preflight makes third-party-image acquisition auditable, and the burst pod's CPU raise has no privilege-boundary implication). No new network endpoints, auth paths, or schema changes were introduced by this plan. `internal/jobs/admission.go` is untouched.

## NOT PERFORMED (explicitly out of scope per this plan and its critical constraints)

**No live-cluster action of any kind was performed.** No `helm install`, no `kubectl` command against a real cluster, no pod creation, no ten-round burst run. This execution environment's `kubectl` happens to reach a real OrbStack Kubernetes node (`kubectl get nodes` succeeds), but Critical Constraint #3 explicitly forbids using it for this plan regardless — the live re-run is Plan 55-05, operator-gated. Every verification in this SUMMARY is one of: `bash -n`, `shellcheck -S error`, file-scoped acceptance-criteria greps (via `command grep` — see Deviations B), `python3` (stdlib only), `docker run` against images already present locally (`octoconv-worker:dev`, `buildpack-deps:bookworm-curl`, `postgres:18`), and isolated arithmetic for the invariant negative test. **QUOT-05 remains Pending** until Plan 55-05 runs the live gate.

## Interface Note for Plan 55-05 (the operator running the live re-run)

- **Invocation is unchanged:** `bash scripts/quota-multireplica-liveproof.sh` (or `--calibrate-only`). New environment-overridable defaults an operator should know about:
  - `FIXTURE` default changed from `internal/e2e/testdata/sample.png` to `scripts/fixtures/quota-burst-heavy.png` (already committed by this plan — no action needed).
  - `TARGET_FORMAT` default changed from `jpg` to `webp`.
  - `BURST_SEND_SPREAD_MS` default changed from `1000` to `250`.
  - New knobs, all overridable but should not be loosened without re-measuring: `SPREAD_SAFETY_FACTOR` (20), `MIN_ENGINE_WALL_MS` (5933 — **this executor's own measurement**; re-measure on the operator's host if materially different hardware/load is expected, since the STEP 1 invariant will hard-fail if `BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR > MIN_ENGINE_WALL_MS`), `JOB_WALL_FLOOR_MS` (2000), `APP_IMAGE_TAG` (dev, matches the chart's `global.imageTag`).
- **New preflight failure modes, all firing in the first seconds of STEP 1 before any cluster mutation:**
  1. THE INVARIANT: fails if `BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR > MIN_ENGINE_WALL_MS`, naming all three values.
  2. Fixture-too-small: fails if the fixture is under 100000 bytes.
  3. App-image freshness: fails (naming the image and the exact `docker build` rebuild command) if `octoconv-api:${APP_IMAGE_TAG}` or `octoconv-worker:${APP_IMAGE_TAG}` is missing, was built before the newest migration file's mtime, the api binary does not embed the newest migration's filename, or the worker image has no `vips` on PATH. This directly replaces the operator's prior 3-lost-install-cycle experience (a stale image previously surfaced as `relation "client_limits" does not exist` at check 19, five minutes into a run, or as an unnamed 300s `kubectl wait` timeout).
  4. Clock skew (STEP 5, after the burst pod exists): fails if the burst pod's `date +%s%3N` and Postgres's `clock_timestamp()` disagree by more than 1000ms.
- **Second evidence artifact:** `${EVIDENCE_DIR}/quota-multireplica-${RUN_ID}-timeline.tsv`, columns `round, job_id, created_at, first_terminal_at, terminal_status, wall_ms, round_last_recv_utc` — one row per admitted job per round, appended after each round's drain completes. The final banner's `git add -f` reminder now names both evidence files.
- **Revised expected wall-clock runtime:** with the heavy fixture converting in ~6s (vs. the old fixture's ~0.1-0.2s), and `drain_round` polling every 2s until every admitted job (K=2 per round) reaches terminal, expect roughly **8-12s of drain time per round** on top of the burst itself, across 10 rounds — call it **90-150s of round-loop time**, materially longer than the pre-55-07 run's round loop (which took well under a minute for all 4 attempted rounds) but still well inside the two-phase install's `kubectl wait --timeout=300s` budgets used elsewhere in the script. The install/teardown overhead is unchanged by this plan.
- **New per-round assertions that will FAIL LOUDLY if the mechanism, not the gate, is broken:** the discriminating assertion (`terminal_before`), the post-drain wall-time floor/ratio assertions, and the `final_statuses == 'done'` assertion. A genuine failure of any of these after this plan's changes is a signal worth escalating, not something to work around by loosening a threshold.

## Next Phase Readiness

- `scripts/fixtures/gen_heavy_png.py` + `scripts/fixtures/quota-burst-heavy.png`, the hardened `scripts/quota-burst-driver.sh`, and the hardened `scripts/quota-multireplica-liveproof.sh` are ready for Plan 55-05's operator-gated live run.
- QUOT-05 remains **Pending** — this plan makes the gate SOUND; Plan 55-05 runs it.
- Three plan-defect findings (A, B, C above) are documented rather than worked around; a future plan-checker pass could correct the literal wording of those acceptance criteria, but the underlying functionality they were meant to verify is genuinely correct and independently confirmed by other means in every case.
- `MIN_ENGINE_WALL_MS=5933` is this executor's own measurement, not copied from the plan; if Plan 55-05 runs on materially different hardware, re-measuring `vips copy` wall time (the same `docker run` command used in this plan's Task 1) before the live run is recommended.

## Self-Check: PASSED

- FOUND: `scripts/fixtures/gen_heavy_png.py`
- FOUND: `scripts/fixtures/quota-burst-heavy.png` (259,682 bytes)
- FOUND: `scripts/quota-burst-driver.sh` (executable, hardened)
- FOUND: `scripts/quota-multireplica-liveproof.sh` (executable, hardened)
- FOUND commit `0e15efb` (Task 1)
- FOUND commit `9db21a2` (Task 2)
- FOUND commit `5473983` (Task 3)
- `bash -n` / `shellcheck -S error`: PASSED on both scripts after every edit
- Fixture determinism: `cmp` confirms byte-identical regeneration
- Fixture measurements: three serial `vips copy` runs all >=3000ms (6549/5933/6001ms); two-parallel run both outputs non-zero bytes
- Driver before/after spread comparison: new driver worst 1ms vs. pre-change driver's worst 64ms across 3 official trials each at `--cpus=0.5` — not worse
- Discriminating SQL negative test: `1` (terminal-before-cutoff case), `0` (terminal-after-cutoff case), against a throwaway `postgres:18` container, removed afterward
- Image-freshness helper: matches `docker image inspect`'s `.Created` (verified epoch 1786111537 round-trips to `2026-08-07T14:05:37Z` UTC, matching `2026-08-07T17:05:37.641828125+03:00`); forced-fail with a future reference epoch produces the expected FAIL message
- Ordering proof: `terminal_before` call site at line 1285 precedes `assert_eq "$QUOTA_K" "$admit_count"` at line 1302
- `git diff --stat` for Task 3 shows exactly one file changed; `git diff -- internal/jobs/admission.go` is empty
- No live-cluster action of any kind was performed by this plan (Critical Constraint #3)

---
*Phase: 55-live-gate-multi-replica-quota-correctness*
*Completed: 2026-08-07*
