---
phase: 50-keda-helm-chart-integration
plan: 03
subsystem: infra
tags: [keda, kubernetes, pandoc, autoscaling, downscale-survival, loadproof]

# Dependency graph
requires:
  - phase: 50-01
    provides: "pandoc-worker Deployment + ScaledObject templates, keda.pandoc + pandocWorker values blocks, PANDOC_* ConfigMap env, pandocWorker.extraEnv override hook"
  - phase: 50-02
    provides: "values-loadproof.yaml keda.pandoc gate override (scaleDownStabilizationSeconds, pollingInterval), values-driven prometheus.scrapeInterval, pandocWorker.extraEnv PANDOC_WORKER_CONCURRENCY=1 override hook, scripts/keda-pandoc-loadproof.sh SC3 harness + fixture-synthesis helper shape"
provides:
  - "scripts/keda-pandoc-downscale-survival.sh -- self-contained SC4 downscale-survival proof harness (D-05 mechanics) with a depth-arithmetic drive shape, /proc-based victim-job attribution, and a --calibrate-only offline mode, statically validated"
  - "committed offline calibration evidence (fixture_bytes=4094606, wall_seconds=18.276) proving the ~4 MiB html->md job window is [15,50]s at production cpu=2/memory=1g limits, safely under PANDOC_ENGINE_TIMEOUT=65s"
affects: ["50-04 (the live cluster SC4 gate run using this script)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Depth-arithmetic KEDA downscale drive shape: N identical LONG jobs (PANDOC_WORKER_CONCURRENCY=1, threshold=2) instead of ocr's LONG-then-SHORT victim-selection trick -- needed because pandoc's worst-case job (~32s) is too short for a scale-from-zero cold start to outlive, unlike ocr's 90-120s job"
    - "/proc/[0-9]*/cmdline-based deterministic job<->pod attribution (reads the worker's own os.MkdirTemp workdir naming) replacing submission-order-based attribution when N identical jobs are submitted in one batch"

key-files:
  created:
    - scripts/keda-pandoc-downscale-survival.sh
  modified: []

key-decisions:
  - "Depth-arithmetic drive shape (N=4 default, PANDOC_DOWNSCALE_JOBS-overridable): submits N identical ~4 MiB html->md jobs so depth falls N -> ... -> 2 (both pods freshly re-busy on their final job, nothing pending) at exactly the moment KEDA's desired-replica formula flips from 2 to 1 -- the genuine 2->1 downscale lands mid-conversion without any manual pod-deletion or forced-replica-count command anywhere in the script"
  - "/proc-based attribution poller (2s cadence) resolves INFLIGHT_JOB_ID from the victim pod's own cmdline argv rather than trusting submission order (which carries no signal when all N jobs are identical) -- an all-empty sample set at-or-before the kubelet Killing event is a hard FAIL with a re-run diagnostic, never a guessed job id"
  - "Fixture-size seed raised from the initially-planned ~3.2 MB Markdown target to ~4.0 MB (closer to the ceiling per the plan's own 'aim just under 4 MiB' instruction) after the smaller seed converged to an unrepresentatively small render (3.67 MB) yielding a barely-in-band 15.358s conversion time -- too little margin above the metric-propagation + HPA stabilization delay for a reliable live SC4 gate"
  - "Proportional shrink-on-overflow formula given a 100000-byte safety margin below the 4194304 ceiling, since pandoc's --standalone HTML wrapper adds a roughly fixed (non-proportional) markup overhead that made a naive proportional shrink converge asymptotically toward the ceiling from above, exhausting all 3 attempts on a 19-byte overshoot in testing"

requirements-completed: [PDCE-04]

# Metrics
duration: ~35min
completed: 2026-08-01
---

# Phase 50 Plan 03: Pandoc SC4 Downscale-Survival Harness + Offline Calibration Summary

**scripts/keda-pandoc-downscale-survival.sh (a depth-arithmetic drive shape + /proc-based attribution SC4 harness, structurally distinct from the ocr clone source by design) plus committed offline calibration evidence (fixture_bytes=4094606, wall_seconds=18.276, safely inside [15,50]) proving the SC4 job window before any cluster time is spent.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-08-01T23:08:00Z (approx.)
- **Completed:** 2026-08-01T23:20:00Z (approx.)
- **Tasks:** 2
- **Files modified:** 3 (1 script created, 2 evidence files committed)

## Accomplishments
- Authored `scripts/keda-pandoc-downscale-survival.sh` (1000+ lines): a structural clone of `scripts/keda-ocr-downscale-survival.sh`'s preflight/teardown/watcher/SC4-triple-check discipline, but with a genuinely different drive shape and attribution mechanism because pandoc's ~32s worst-case job cannot outlive a scale-from-zero cold start the way ocr's 90-120s job does
- Depth-arithmetic drive shape: N=4 (env-overridable via `PANDOC_DOWNSCALE_JOBS`) identical ~4 MiB html->md jobs submitted in one batch; with `PANDOC_WORKER_CONCURRENCY=1` (values-loadproof overlay) and `threshold=2`, depth falls from N to 2 exactly when both pods are freshly re-busy on their final job -- the moment KEDA's desired-replica formula flips 2->1, landing a genuine HPA downscale mid-conversion with zero manual pod-deletion/forced-scale commands anywhere in the script
- Deterministic `/proc/[0-9]*/cmdline` attribution poller (2s cadence) against the annotated victim pod, resolving the in-flight job id from the worker's own `os.MkdirTemp("", "octoconv-"+job.ID.String()+"-")` workdir naming -- hard-fails with a re-run diagnostic (never guesses) if the victim was idle at SIGTERM time
- SC4 triple-check mirrors `keda-ocr-downscale-survival.sh` exactly (grace==85 production value, exactly one queued->active transition, Killing-before-finished_at, exit 0/Completed never 137), extended with a batch-completeness check (every other job in the batch also reaches `done`)
- `--calibrate-only` offline mode: fixture synthesis + one timed html->md conversion at production `cpu=2/memory=1g` limits, zero kubectl/helm dependency, writes committed calibration evidence
- Task 2's live dry-run of `--calibrate-only` surfaced and fixed two real bugs before any cluster time was spent (see Deviations below) and one fixture-sizing design gap, converging on `fixture_bytes=4094606` (comfortably in `[3000000, 4194304]`) and `wall_seconds=18.276` (comfortably in `[15, 50]`)

## Task Commits

Each task was committed atomically:

1. **Task 1: author scripts/keda-pandoc-downscale-survival.sh (SC4 harness with a fast-engine drive shape)** - `a02ba72` (test)
2. **Task 2: offline fixture + timing calibration (container-only, proves the SC4 job window before any cluster run)** - `74f0402` (docs)

_Note: no plan-metadata commit yet -- this SUMMARY.md commit itself serves as the plan-completion record (worktree mode; orchestrator handles STATE.md/ROADMAP.md centrally after merge)._

## Files Created/Modified
- `scripts/keda-pandoc-downscale-survival.sh` (new, executable) - SC4 downscale-survival proof harness: dual-overlay `helm install` (values-local + values-loadproof), depth-arithmetic drive shape (N identical LONG jobs), pod-deletion-cost=-1000 victim annotation, `/proc`-based attribution poller, SC4 triple-check + batch-completeness assertions, `--calibrate-only` offline fixture/timing mode
- `.planning/phases/50-keda-helm-chart-integration/evidence/calibration-pandoc-downscale-fixture-20260801T201900Z.txt` (new, force-added) - offline calibration evidence: `fixture_bytes=4094606`, `wall_seconds=18.276`, `container_cpus=2`, `container_memory=1g`, `host_arch=arm64`, comparison against the Phase 49 p95 (32.318s)
- `.planning/phases/50-keda-helm-chart-integration/evidence/gate-transcript-downscale-20260801T201900Z.log` (new, force-added) - the successful calibration run's full teed transcript

## Decisions Made
- Depth-arithmetic drive shape and `/proc`-based attribution (see `key-decisions` in frontmatter) -- both required by the plan's explicit divergence-from-ocr instructions, implemented as specified
- Fixture-size seed raised from ~3.2 MB to ~4.0 MB and the shrink-on-overflow formula given a safety margin (see `key-decisions`) -- discovered necessary only by actually running `--calibrate-only` per Task 2, not assumed in advance

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `wc -c < path` redirect evaluated on the host instead of inside the container**
- **Found during:** Task 2's first `--calibrate-only` dry-run
- **Issue:** `docker exec "$FIXTURE_CONTAINER" wc -c < /tmp/work/long.html` -- the `< /tmp/work/long.html` redirect is parsed and opened by the HOST's own shell (bash), not passed through to the container, so it failed with `No such file or directory` since that path only exists inside the container.
- **Fix:** Wrapped the redirect inside the container's own shell: `docker exec "$FIXTURE_CONTAINER" sh -c 'wc -c < /tmp/work/long.html'`, matching the pattern already used elsewhere in the same function for the paragraph-generation block.
- **Files modified:** scripts/keda-pandoc-downscale-survival.sh
- **Verification:** Re-ran `--calibrate-only`; the fixture-size measurement succeeded on the next attempt.
- **Committed in:** 74f0402 (Task 2 commit)

**2. [Rule 1 - Bug] Fixture-size shrink-on-overflow formula converged asymptotically, exhausting all 3 attempts on a 19-byte overshoot**
- **Found during:** Task 2's second `--calibrate-only` dry-run (after fix #1)
- **Issue:** The proportional shrink `md_target = md_target * PANDOC_MAX_INPUT_BYTES / html_bytes` assumes HTML render size scales purely proportionally with Markdown input size, but pandoc's `--standalone` wrapper adds a roughly FIXED (not proportional) markup overhead (doctype/head/meta). This made the shrink converge toward the ceiling from above asymptotically: attempt 1=4584504B, attempt 2=4194489B, attempt 3=4194323B -- still 19 bytes over the 4194304 ceiling after exhausting all 3 attempts, hard-failing.
- **Fix:** Added a 100000-byte safety margin to the shrink target (`(PANDOC_MAX_INPUT_BYTES - 100000)` instead of the literal ceiling), so the formula converges reliably under the ceiling within 2 attempts instead of asymptotically approaching it from above.
- **Files modified:** scripts/keda-pandoc-downscale-survival.sh
- **Verification:** Re-ran `--calibrate-only`; converged to `html_bytes=4094606` on attempt 2, safely inside `[3000000, 4194304]`.
- **Committed in:** 74f0402 (Task 2 commit)

**3. [Rule 1 - Bug/design gap] Fixture-size seed was too conservative, producing an unrepresentatively small, marginal timing result**
- **Found during:** Task 2's first successful `--calibrate-only` run (after fix #1, before fix #2)
- **Issue:** The initial Markdown target seed (3.2 MB) converged to a comfortably-under-ceiling but notably SMALL HTML render (3.67 MB, ~87.5% of the 4194304 ceiling), which measured `wall_seconds=15.358` -- technically inside the required `[15, 50]` band but only 0.358s above the 15s floor, i.e. too little margin above the ~15-25s metric-propagation + HPA-stabilization delay for the live SC4 gate to reliably catch both victim pods still mid-conversion at downscale time. This did not match the plan's own action text ("aim the HTML at just under the 4 MiB ceiling"), which the conservative 3.2 MB seed did not actually do.
- **Fix:** Raised the initial seed to 4.0 MB (right at the ceiling itself, ignoring markup inflation), relying on the shrink-on-overflow branch (fix #2, above) to converge it back under the ceiling with margin. Re-measured: `fixture_bytes=4094606`, `wall_seconds=18.276` -- both comfortably inside their required bands, and materially closer to the Phase 49 measurement's own ~4 MiB input size.
- **Files modified:** scripts/keda-pandoc-downscale-survival.sh
- **Verification:** Final `--calibrate-only` run passed cleanly with both checks in-band and committed evidence.
- **Committed in:** 74f0402 (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (3 bugs, Rule 1)
**Impact on plan:** All three fixes were discovered only by actually exercising `--calibrate-only` per Task 2's own purpose ("proves the SC4 job window is measured, not assumed, before any cluster time is spent") -- no scope change, the fixes make the harness's own core measurement mechanism correct rather than superficially passing.

## Issues Encountered
None beyond the three deviations above, all resolved before commit.

## Measured vs. Phase 49 comparison (Task 2 acceptance criterion)

- **Measured this run:** `fixture_bytes=4094606`, `wall_seconds=18.276` (arm64 host, cpus=2.0/memory=1g)
- **Phase 49 p95 (49-03-SUMMARY.md):** `html_large->md` worst cell, `p95=32.318s` at ~the same ~4 MiB input cap
- **Comparison:** This run's measurement (18.276s) is materially lower than the Phase 49 p95 (32.318s) -- roughly 57% of it. This is attributed to measurement-host architecture/speed differences, the same documented caveat present at every prior engine-class timing gate (ocr/av/audio) -- NOT a fixture-size defect (fixture_bytes is within 2.4% of the 4194304 ceiling that the 49-03 measurement itself used). The raw `pandoc` CLI conversion time measured here also excludes the live worker pipeline's S3 download/upload + DB transaction overhead, which will add wall time on top of this number during the live cluster run, giving additional (unmeasured, but expected positive) margin.
- **Recommended `PANDOC_DOWNSCALE_JOBS` for Plan 04's operator:** the default `4` remains reasonable as a starting point. The script's own built-in safety net (a hard FAIL with an explicit re-run diagnostic if the attribution poller ever finds the victim pod idle at SIGTERM time) is the correct fallback if the live cluster's job duration turns out to be shorter than this offline measurement suggests -- increasing `PANDOC_DOWNSCALE_JOBS` does not by itself widen the margin for the LAST job in the batch (the one in flight during the downscale), since that job's duration is governed by fixture size, not batch size; if the live run's poller reports an idle victim, the correct operator response is more likely to be a larger `PANDOC_DOWNSCALE_JOBS` value that increases the total number of "rounds" and observation opportunities, per the script's own diagnostic message.

## User Setup Required
None - no external service configuration required. The `--calibrate-only` run in this plan is fully container-local (docker build/run with `--cpus`/`--memory` limits) and does not touch any k8s cluster. The live SC4 gate run itself (against a real OrbStack cluster) is deferred to Plan 04 per this phase's operator-gated live-checkpoint convention.

## Next Phase Readiness
- `scripts/keda-pandoc-downscale-survival.sh` is statically complete (bash -n, shellcheck -S error clean) and structurally incapable of faking the downscale (no `kubectl scale` anywhere, verified by grep).
- The offline calibration is committed, proving the SC4 job window is measured (not assumed) before any cluster time is spent in Plan 04.
- Ports 18098 (api) / 15442 (db) are reserved and confirmed non-colliding with every sibling gate script.
- No blockers. Plan 04 can proceed directly to the live cluster SC3 + SC4 run.

---
*Phase: 50-keda-helm-chart-integration*
*Completed: 2026-08-01*

## Self-Check: PASSED

- FOUND: scripts/keda-pandoc-downscale-survival.sh
- FOUND: .planning/phases/50-keda-helm-chart-integration/evidence/calibration-pandoc-downscale-fixture-20260801T201900Z.txt
- FOUND commit: a02ba72 (Task 1)
- FOUND commit: 74f0402 (Task 2)
