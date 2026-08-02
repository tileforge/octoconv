---
phase: 49-containerization-measured-timeout-gate
plan: 02
subsystem: infra
tags: [docker-compose, ci, pandoc, asynq, timing-gate, IN-02]

# Dependency graph
requires:
  - phase: 49-01
    provides: Dockerfile.pandoc-worker (SHA-256-pinned static pandoc 3.10.1, TeX-free, dual-arch)
provides:
  - scripts/pandoc-timing-measure.sh (p95 wall-sec/conversion measurement gate, never commits a timeout)
  - docker-compose.yml pandoc-worker service with PLACEHOLDER PANDOC_ENGINE_TIMEOUT=90s
  - IN-02 PANDOC_ENGINE_TIMEOUT + PANDOC_MAX_RETRY parity across all 10 queue.NewClient() services
  - .env.example PANDOC_* documentation
  - CI docker-build + e2e bake matrix pandoc-worker target (9 -> 10)
affects: [49-03-measurement-checkpoint, 49-04-live-e2e-checkpoint]

# Tech tracking
tech-stack:
  added: []
  patterns: ["supervised measure-don't-assume timeout gate (clone of scripts/ocr-timing-measure.sh)", "IN-02 byte-identical engine-class env parity across every queue.NewClient() service"]

key-files:
  created: [scripts/pandoc-timing-measure.sh]
  modified: [docker-compose.yml, .env.example, .github/workflows/ci.yml]

key-decisions:
  - "Metric is p95 WALL-CLOCK SECONDS PER CONVERSION with no per-page/RTF divisor -- pandoc's own p95 wall time already IS the per-conversion cost, unlike OCR_MAX_PAGES/AUDIO_MAX_DURATION_SECONDS/AV_MAX_DURATION_SECONDS multipliers."
  - "Corpus matrix: sources {md, latex, html, docx} x sizes {small~2KB, medium~100KB, large~8MiB (env-overridable toward the 50MiB cap)} x one cheap text-out + one heavier zip-out target per source, restricted exclusively to valid pandocPairTable pairs (md->{html,docx}, latex->{html,docx}, html->{md,docx}, docx->{html,epub})."
  - "PANDOC_ENGINE_TIMEOUT=90s and stop_grace_period=110s in docker-compose.yml are CLEARLY-MARKED PLACEHOLDERS -- Plan 03's supervised scripts/pandoc-timing-measure.sh run supplies the real measured value; this plan commits no measured number."
  - "PANDOC_MAX_INPUT_BYTES=52428800 (50 MiB) is the Path-B lever: if Plan 03's derivation breaches the 900s reconciler CAP, lower this value first, never inflate the timeout or the global CAP."

patterns-established:
  - "Pattern: measurement-gate script structure (OrbStack/k8s guard -> docker build -> long-lived --cpus/--memory container -> in-container corpus synthesis -> timed per-cell nearest-rank p95 -> DERIVATION HELP block that prints-never-commits -> integrity-only exit code) is now cloned 4x (audio/av/ocr/pandoc)."

requirements-completed: [PDCE-04]

# Metrics
duration: 35min
completed: 2026-07-31
---

# Phase 49 Plan 02: Pandoc Timing Gate + Compose/CI Scaffold Summary

**Authored scripts/pandoc-timing-measure.sh (p95 wall-sec/conversion gate over a 24-cell md/latex/html/docx x size x target corpus), added the pandoc-worker compose service with a placeholder 90s timeout, and propagated IN-02 PANDOC_ENGINE_TIMEOUT/PANDOC_MAX_RETRY parity across all 10 queue.NewClient()-constructing services.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-31T00:00:00Z (approx)
- **Completed:** 2026-07-31
- **Tasks:** 3/3
- **Files modified:** 4 (1 created, 3 modified)

## Accomplishments
- `scripts/pandoc-timing-measure.sh`: a runnable, `bash -n`/`shellcheck -S warning`-clean measurement script cloning `scripts/ocr-timing-measure.sh`'s section structure, adapted to pandoc's cost model (no page/RTF divisor), synthesizing its own in-container markup corpus (never a committed binary fixture) and timing the exact `pandocReaderSpec`/`pandocConvertArgs` production argv.
- `docker-compose.yml` `pandoc-worker` service: build/resource-limits/concurrency/`PANDOC_MAX_INPUT_BYTES` (the Path-B lever) wired, with `PANDOC_ENGINE_TIMEOUT=90s` and `stop_grace_period=110s` explicitly commented as PLACEHOLDERS pending Plan 03.
- IN-02: `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY` now byte-identical (verified via `docker compose config`) across all 10 queue-client services -- api, worker, webhook-worker-1/-2, document-worker, chromium-worker, audio-worker, av-worker, ocr-worker, pandoc-worker.
- `.env.example` documents the four PANDOC_* vars in the established OCR_*-derivation-comment style.
- CI bake matrix: `pandoc-worker` added to both the `docker-build` tier (cache-to + cache-from) and the `e2e` tier (cache-from), same `gha` scope so the e2e job reuses the warmed layer cache; the tier-3 comment now reads "10 compose bake targets".

## Task Commits

Each task was committed atomically:

1. **Task 1: scripts/pandoc-timing-measure.sh** - `7843fba` (feat)
2. **Task 2: docker-compose.yml pandoc-worker service + IN-02 propagation + .env.example** - `5b4ca24` (feat)
3. **Task 3: CI bake matrix** - `0e2b58b` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified
- `scripts/pandoc-timing-measure.sh` - new supervised p95 wall-sec/conversion measurement gate; builds Dockerfile.pandoc-worker, runs a 24-cell (4 sources x 3 sizes x 2 targets) matrix at `--cpus=2.0 --memory=1g`, prints per-cell p95 + a derivation-help block; commits no timeout value; exit reflects measurement integrity only.
- `docker-compose.yml` - added `pandoc-worker` service (build, `stop_grace_period: 110s` placeholder, resource limits, env block including `PANDOC_ENGINE_TIMEOUT: "90s"` placeholder + `PANDOC_MAX_INPUT_BYTES`/`PANDOC_WORKER_CONCURRENCY` pandoc-worker-only vars + `WEBHOOK_SIGNING_SECRET`); added `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY` to the 9 pre-existing queue-client services (api, worker, webhook-worker-1/-2, document-worker, chromium-worker, audio-worker, av-worker, ocr-worker).
- `.env.example` - new `# Pandoc worker` section documenting `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY`/`PANDOC_WORKER_CONCURRENCY`/`PANDOC_MAX_INPUT_BYTES`.
- `.github/workflows/ci.yml` - added `pandoc-worker` to the `docker-build` tier's bake `set:` (cache-to + cache-from) and the `e2e` tier's bake `set:` (cache-from); updated the "9 -> 10 compose bake targets" comment.

## Decisions Made
- Chose the html-source "text-out" analog target as `md` (rather than `rst`/`latex`) since html cannot target itself in `pandocPairTable` and `md` is the cheapest plausible text output pandoc can emit for an html source — this correctly times a cell WITHOUT `--standalone` (md is not a structural target), exercising both branches of `pandocConvertArgs`' conditional flag, not just the always-standalone case.
- Chose `docx`'s "zip-out" analog target as `epub` (rather than attempting docx->docx, which is not a valid pair — identity pairs are excluded from `pandocPairTable`).
- Bounded the "large" corpus size bucket to a default of 8 MiB (`PANDOC_TIMING_LARGE_BYTES`, env-overridable toward the real 50 MiB `PANDOC_MAX_INPUT_BYTES` cap) so a full 24-cell x N=10-run measurement completes in a reasonable wall-clock window when Plan 03 runs it; an operator can raise it for a more exhaustive supervised run.
- Followed the plan's instruction literally: the compose `pandoc-worker` resource limits mirror the ocr-worker fleet baseline (cpus:2.0/memory:1g) rather than a smaller footprint, since Plan 03's measurement has not yet run to justify lowering it.

## Deviations from Plan

None - plan executed exactly as written. Scope fence held: no Go code changed, no Dockerfile changed (Dockerfile.pandoc-worker is 49-01's artifact, untouched here), and no measured `PANDOC_ENGINE_TIMEOUT` value was committed — only the clearly-marked `90s` PLACEHOLDER, per the plan's explicit constraint.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required. The measurement RUN itself is Plan 03's supervised operator checkpoint; this plan only authored the script and scaffold.

## Next Phase Readiness
- Plan 03 can now run `scripts/pandoc-timing-measure.sh` against the real `Dockerfile.pandoc-worker` image at the compose `pandoc-worker`'s exact `--cpus=2.0 --memory=1g` limits, read the printed per-cell p95 RESULT lines and the DERIVATION HELP block, and supervised-decide the real `PANDOC_ENGINE_TIMEOUT` (checking it clears the 900s reconciler CAP, applying the `PANDOC_MAX_INPUT_BYTES` Path-B lever if not).
- Once Plan 03 lands the measured value, `docker-compose.yml`'s `PANDOC_ENGINE_TIMEOUT`/`stop_grace_period` PLACEHOLDER comments and the IN-02 propagation across all 10 services will need updating to the finalized number (mirrors `OCR_ENGINE_TIMEOUT`'s 45-02 -> 45-03 placeholder-to-measured transition).
- No blockers.

---
*Phase: 49-containerization-measured-timeout-gate*
*Completed: 2026-07-31*

## Self-Check: PASSED
- FOUND: scripts/pandoc-timing-measure.sh
- FOUND: .planning/phases/49-containerization-measured-timeout-gate/49-02-SUMMARY.md
- FOUND commit: 7843fba (Task 1)
- FOUND commit: 5b4ca24 (Task 2)
- FOUND commit: 0e2b58b (Task 3)
