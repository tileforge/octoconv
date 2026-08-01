---
phase: 50-keda-helm-chart-integration
plan: 02
subsystem: infra
tags: [helm, kubernetes, keda, prometheus, pandoc, autoscaling, loadproof]

# Dependency graph
requires:
  - phase: 50-01
    provides: "pandoc-worker Deployment + ScaledObject templates, keda.pandoc + pandocWorker values blocks, PANDOC_* ConfigMap env, pandocWorker.extraEnv override hook"
provides:
  - "values-loadproof.yaml keda.pandoc gate override (scaleDownStabilizationSeconds: 0, pollingInterval: 3) exercising the falsy-0 guard"
  - "values-driven prometheus.scrapeInterval (production default 15s unchanged; gate overlay sets 5s) via templates/_helpers.tpl octoconv.prometheusScrapeConfig"
  - "values-loadproof.yaml pandocWorker.extraEnv PANDOC_WORKER_CONCURRENCY override hook (one job per pod, for the future SC4 gate)"
  - "scripts/keda-pandoc-loadproof.sh — self-contained SC3 scale-from-zero (0->1->0) live-cluster gate for the pandoc engine class, statically validated"
affects: ["50-03/50-04 (downscale-survival gate, live evidence runs)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "In-container fixture synthesis for gate scripts with no committed testdata (mirrors scripts/pandoc-timing-measure.sh's corpus-generation shape and scripts/keda-av-loadproof.sh's/keda-ocr-downscale-survival.sh's FIXTURE_CONTAINER EXIT-trap cleanup discipline)"
    - "Values-driven Prometheus scrape_interval, gated production-safe via | default \"15s\" in the named template that the checksum/config annotation also hashes"

key-files:
  created:
    - scripts/keda-pandoc-loadproof.sh
  modified:
    - deploy/chart/octoconv/values-loadproof.yaml
    - deploy/chart/octoconv/templates/_helpers.tpl
    - deploy/chart/octoconv/values.yaml

key-decisions:
  - "keda.pandoc gate override sets scaleDownStabilizationSeconds: 0 (a literal 0, not null) — the one place pandoc diverges from ocr/av/audio's 15s gate overrides, because production is already null (D-01) and pandoc's own ~32s worst-case job is shorter than the sibling gates' 15s stabilization plus metric-propagation lag; 0 exercises the falsy-0 guard (HARD-03/D-06) live"
  - "prometheus.scrapeInterval made values-driven (production default 15s unchanged, gate overlay 5s) — planner/checker-flagged decision from 50-CONTEXT.md, accepted and recorded here per that note"
  - "SC3 harness installs with values-local.yaml ONLY, never the gate overlay — SC3 must prove production KEDA tuning (cooldownPeriod 90, null stabilization, concurrency 2), unlike the future SC4 downscale-survival gate which needs the overlay"
  - "SC3 fixture synthesized in-container (mirrors av/ocr's FIXTURE_CONTAINER pattern) per the plan's explicit action text, rather than the simpler inline-write option 50-PATTERNS.md flagged as also acceptable"

patterns-established:
  - "pandocWorker.extraEnv PANDOC_WORKER_CONCURRENCY=1 override in values-loadproof.yaml, ahead of need, for the SC4 downscale-survival gate's deterministic victim-pod selection (mirrors documentWorker.extraEnv precedent)"

requirements-completed: [PDCP-02]

# Metrics
duration: ~40min
completed: 2026-08-01
---

# Phase 50 Plan 02: Pandoc Gate Overlay + SC3 Scale-From-Zero Harness Summary

**values-loadproof.yaml pandoc overlay exercising the falsy-0 guard (scaleDownStabilizationSeconds: 0) plus a values-driven Prometheus scrape interval, and a self-contained scripts/keda-pandoc-loadproof.sh SC3 scale-from-zero gate with run-time in-container markdown fixture synthesis (no committed testdata).**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-08-01T20:05:00Z (approx.)
- **Completed:** 2026-08-01T20:45:00Z (approx.)
- **Tasks:** 2
- **Files modified:** 4 (1 created, 3 modified)

## Accomplishments
- `values.yaml` `prometheus:` block gains `scrapeInterval: "15s"` and `templates/_helpers.tpl`'s `octoconv.prometheusScrapeConfig` reads it via `| default "15s"` — production render unchanged, gate render controllable
- `values-loadproof.yaml` gains a `keda.pandoc` override (`scaleDownStabilizationSeconds: 0`, `pollingInterval: 3`), a top-level `prometheus.scrapeInterval: "5s"`, and a `pandocWorker.extraEnv.PANDOC_WORKER_CONCURRENCY: "1"` block — all render-verified against both production-only and dual-overlay `helm template` output
- `scripts/keda-pandoc-loadproof.sh` (new, 714 lines): a structural clone of `scripts/keda-ocr-loadproof.sh` re-timed for pandoc's own ~65s-scale budget (180s replica-floor wait, 120s job-completion bound, 300s drain bound — none copied from ocr's 744s-scaled constants), synthesizing its own ~4KB markdown trigger fixture in a throwaway `octoconv-pandoc-worker:dev` container since pandoc has no committed testdata
- Full render assertion suite passes: production scrape_interval 15s + no pandoc stabilization block; dual-overlay stabilizationWindowSeconds 0 + pollingInterval 3 + scrape_interval 5s + grace 85s (unchanged) + explicit `PANDOC_WORKER_CONCURRENCY` env entry with value "1"; `helm lint` green with values-local alone and with both overlays layered
- `bash -n` and `shellcheck -S error` both clean on the new script; no pre-existing gate script touched (`git status` confirms only the new file)

## Task Commits

Each task was committed atomically:

1. **Task 1: values-loadproof.yaml pandoc gate overlay + values-driven prometheus scrape interval** - `36215bc` (feat)
2. **Task 2: author scripts/keda-pandoc-loadproof.sh (SC3 scale-from-zero harness)** - `a237283` (test)

_Note: no plan-metadata commit yet — this SUMMARY.md commit itself serves as the plan-completion record (worktree mode; orchestrator handles STATE.md/ROADMAP.md centrally after merge)._

## Files Created/Modified
- `deploy/chart/octoconv/templates/_helpers.tpl` - `octoconv.prometheusScrapeConfig` reads `.Values.prometheus.scrapeInterval | default "15s"` instead of a hardcoded `15s`
- `deploy/chart/octoconv/values.yaml` - added `prometheus.scrapeInterval: "15s"` with a comment tying it to the gate-only override and the checksum/config annotation self-reference note
- `deploy/chart/octoconv/values-loadproof.yaml` - added `keda.pandoc` override block, top-level `prometheus.scrapeInterval: "5s"`, `pandocWorker.extraEnv.PANDOC_WORKER_CONCURRENCY: "1"`, and updated the file's header comment to reflect the growing knob count across phases
- `scripts/keda-pandoc-loadproof.sh` (new) - SC3 live scale-from-zero (0->1->0) proof harness: preflight (compose-down guard, image-present check, KEDA v2.20.1 resolvable re-verify), idempotent KEDA install, `values-local.yaml`-only octoconv install, asynq queue-registry seeding (8 queues incl. pandoc), port-forward + client mint, in-container markdown fixture synthesis + `docker cp`, `postJobPath` submission, watch-based pod event-timeline capture (Scheduled->Pulling->Pulled->Created->Started), job-completion and drain-to-0 assertions, EXIT-trap teardown

## Decisions Made
- Followed 50-CONTEXT.md D-01/D-02/D-04b exactly for the overlay tuning (0, not 15, for pandoc's stabilization override — see key-decisions above for the full derivation)
- Chose the in-container fixture-synthesis path for the SC3 script (per the plan's explicit `<action>` text) over the simpler inline-write alternative 50-PATTERNS.md separately flagged as acceptable — this keeps the script's fixture-handling shape structurally identical to av/ocr's FIXTURE_CONTAINER discipline, which the plan's `<read_first>` and threat register (T-50-06) both anchor on

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Removed literal "values-loadproof" string from the new script's comments**
- **Found during:** Task 2 verification (running the plan's exact `<verify>` command)
- **Issue:** The plan's automated verification includes `! grep -q 'values-loadproof' scripts/keda-pandoc-loadproof.sh` — my first draft's header/STEP-4 comments explained the script "NEVER layers `values-loadproof.yaml`" using that literal string, which is functionally correct (the script genuinely never references the file) but fails the verify command's literal string-absence check. No sibling gate script (ocr/av/audio) mentions the string "values-loadproof" anywhere, even in comments, confirming this was a genuine authoring miss, not an ambiguous requirement.
- **Fix:** Reworded the two comments to describe the same constraint ("never layers any gate-only overlay" / "NOT the loadproof overlay") without the literal hyphenated filename string.
- **Files modified:** scripts/keda-pandoc-loadproof.sh
- **Verification:** `grep -q 'values-loadproof' scripts/keda-pandoc-loadproof.sh` now exits 1 (no match); full verify command re-run clean.
- **Committed in:** a237283 (Task 2 commit — fixed before commit, not a follow-up)

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** Editorial fix caught by re-running the plan's own verification before committing; no functional or scope change.

## Issues Encountered
None beyond the deviation above.

## User Setup Required
None - no external service configuration required. The live SC3 gate run itself (against a real OrbStack cluster) is deferred to a later plan per this phase's operator-gated live-checkpoint convention (50-CONTEXT.md D-06).

## Next Phase Readiness
- The gate overlay and SC3 harness are both statically complete and verified (helm lint/template, bash -n, shellcheck). No live cluster was touched by this plan (NO live cluster in this plan, per the plan's own `<objective>`).
- `scripts/keda-pandoc-loadproof.sh` is ready to run against a real OrbStack session; ports 18097/api and 15441/db are reserved and confirmed non-colliding with every sibling gate.
- The `values-loadproof.yaml` `pandocWorker.extraEnv` hook is in place for the future SC4 downscale-survival script's deterministic victim-pod selection need.
- No blockers.

---
*Phase: 50-keda-helm-chart-integration*
*Completed: 2026-08-01*

## Self-Check: PASSED

- FOUND: deploy/chart/octoconv/templates/_helpers.tpl
- FOUND: deploy/chart/octoconv/values-loadproof.yaml
- FOUND: deploy/chart/octoconv/values.yaml
- FOUND: scripts/keda-pandoc-loadproof.sh
- FOUND: .planning/phases/50-keda-helm-chart-integration/50-02-SUMMARY.md
- FOUND commit: 36215bc (Task 1)
- FOUND commit: a237283 (Task 2)
