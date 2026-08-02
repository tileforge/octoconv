---
phase: 49-containerization-measured-timeout-gate
plan: 03
subsystem: infra
tags: [docker-compose, pandoc, timing-gate, IN-02, measured-timeout, PDCE-04]

# Dependency graph
requires:
  - phase: 49-02
    provides: scripts/pandoc-timing-measure.sh (measurement gate) + pandoc-worker compose service with PLACEHOLDER PANDOC_ENGINE_TIMEOUT=90s + IN-02 parity scaffold
provides:
  - Measured PANDOC_ENGINE_TIMEOUT=65s (ceil(worst-cell p95=32.318s x 2.0)), byte-identical across all 10 queue.NewClient() services
  - PANDOC_MAX_INPUT_BYTES lowered 50 MiB (50<<20) -> 4 MiB (4194304), Path-B lever fired BY MEMORY (zip-out OOM at 8 MiB on 1g)
  - pandoc-worker stop_grace_period 110s -> 85s (65s + 10s ShutdownTimeout margin + 10s Docker SIGKILL margin)
  - Key Decision recorded in PROJECT.md + STATE.md with full measurement conditions
  - PDCE-04 marked complete in REQUIREMENTS.md
affects: [49-04-live-e2e-checkpoint, "Phase 50 (KEDA terminationGracePeriodSeconds/scaleDownStabilizationSeconds consume this measured timeout)"]

# Tech tracking
tech-stack:
  added: []
  patterns: ["measured-not-copied engine timeout derivation (ceil(worst_p95 x safety_factor), CAP-checked, Path-B lever) now closed for all 4 heavy engine classes (audio/av/ocr/pandoc)"]

key-files:
  created: []
  modified: [docker-compose.yml, .env.example, .planning/PROJECT.md, .planning/STATE.md, .planning/REQUIREMENTS.md]

key-decisions:
  - "PANDOC_ENGINE_TIMEOUT = ceil(worst-cell p95 x 2.0) = ceil(32.318 x 2.0) = 65s, 92.8% margin under the 900s/15m reconciler CAP -- distinct from OCR(744s)/AV(753s)/AUDIO(742s)/the 90s placeholder (PITFALLS.md Pitfall 7)."
  - "Worst cell = html_large->md (reader-heavy HTML parsing), p95=32.318s -- NOT a zip-out target as might be assumed; parsing cost dominates serialization cost for this corpus."
  - "Path-B lever (PANDOC_MAX_INPUT_BYTES) FIRED, but BY MEMORY not by the 900s CAP -- the honest finding of this gate. The zip-out targets (docx/epub) OOM-killed on the compose 1g pandoc-worker memory limit at an 8 MiB input (0-byte output, 'Killed'); cliff measured at 4 MiB=~10.2s clean -> 6.5 MiB=~40s thrash -> 8 MiB=OOM. Lowered 50 MiB (50<<20) -> 4 MiB (4194304) for memory headroom; the worst-cell p95 above was measured AT this 4 MiB cap."
  - "No MAX_PAGES/duration-style multiplier in the formula (unlike OCR/AV/AUDIO) -- pandoc's own p95 wall time already IS the full per-conversion cost, per Plan 02's established metric design."

patterns-established: []

requirements-completed: [PDCE-04]

# Metrics
duration: 15min
completed: 2026-08-01
---

# Phase 49 Plan 03: Measured Timeout Gate Summary

**PANDOC_ENGINE_TIMEOUT finalized at 65s from a supervised p95 wall-sec/conversion measurement (worst cell html_large->md, p95=32.318s), propagated byte-identically across all 10 queue-client services, with a Path-B PANDOC_MAX_INPUT_BYTES lever fired for a memory-OOM finding distinct from the CAP.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-08-01T00:00:00Z (approx)
- **Completed:** 2026-08-01
- **Tasks:** 2/2 (Task 1: supervised operator measurement checkpoint, approved prior session; Task 2: derive/plug-in/record, this session)
- **Files modified:** 5

## Accomplishments
- Consumed the operator-approved supervised `scripts/pandoc-timing-measure.sh` run (arch=arm64, cpus=2.0, memory=1g, N=10/cell, nearest-rank p95, at the 4 MiB `PANDOC_MAX_INPUT_BYTES` cap): worst cell html_large->md p95=32.318s (other large cells: md->html 25.37s, md->docx 23.34s, html->docx 15.99s, latex->html 6.06s, latex->docx 3.90s; all small/medium cells < 0.4s).
- Derived `PANDOC_ENGINE_TIMEOUT = ceil(32.318 x 2.0) = 65s` -- 92.8% margin under the 900s/15m reconciler CAP; distinct from OCR(744s)/AV(753s)/AUDIO(742s) and the prior 90s placeholder.
- Propagated `PANDOC_ENGINE_TIMEOUT: "65s"` byte-identically across all 10 queue.NewClient()-constructing services (api, worker, webhook-worker-1/-2, document-worker, chromium-worker, audio-worker, av-worker, ocr-worker, pandoc-worker) -- verified via `docker compose config` (single distinct value).
- Recomputed `pandoc-worker`'s `stop_grace_period` 110s -> 85s (65s measured timeout + 10s ShutdownTimeout margin + 10s Docker SIGKILL margin, mirroring ocr-worker's 744s+20s=764s derivation).
- Fired the Path-B lever on `PANDOC_MAX_INPUT_BYTES` (50 MiB -> 4 MiB) for the measurement's real finding: the zip-out targets (docx/epub) OOM-killed on the 1g pandoc-worker memory limit at an 8 MiB input, well before the timeout/CAP was ever at risk. Documented this honestly as a memory-driven firing, not a CAP-driven one.
- Replaced every placeholder derivation comment in `docker-compose.yml` and `.env.example` with the real measured derivation.
- Recorded the full measurement (numbers + conditions + Path-B finding) as a Key Decision in `PROJECT.md` and as a Decisions entry in `STATE.md`.
- Marked `PDCE-04` complete in `REQUIREMENTS.md` (the measured-timeout gate this requirement specifically calls for is now satisfied; the live E2E proof remains Plan 04's separate close, mirroring OCR's 45-03/45-04 split).

## Task Commits

Each task was committed atomically:

1. **Task 1: Supervised measurement checkpoint** - operator-run, approved prior session (no code commit; numbers carried into this session as pinned input)
2. **Task 2: Derive/CAP-check/plug-in/recompute/record** - `b5b64f6` (feat: compose+env) + `5a73e65` (docs: PROJECT.md/STATE.md/REQUIREMENTS.md)

**Plan metadata:** (this commit)

## Files Created/Modified
- `docker-compose.yml` - `PANDOC_ENGINE_TIMEOUT` "90s"->"65s" on all 10 services (byte-identical); `PANDOC_MAX_INPUT_BYTES` "52428800"->"4194304" on pandoc-worker only; `stop_grace_period` "110s"->"85s" on pandoc-worker; placeholder derivation comments replaced with the real measured derivation including the Path-B-by-memory finding; deploy `memory: 1g` comment updated to flag the OOM cliff dependency.
- `.env.example` - `PANDOC_ENGINE_TIMEOUT=90s`->`65s`, `PANDOC_MAX_INPUT_BYTES=52428800`->`4194304`, full derivation comment replacing the placeholder note.
- `.planning/PROJECT.md` - new Key Decisions table row recording the measured timeout, derivation, CAP margin, Path-B-by-memory finding, and measurement conditions.
- `.planning/STATE.md` - two new Decisions entries mirroring the PROJECT.md Key Decision for session continuity.
- `.planning/REQUIREMENTS.md` - `PDCE-04` checkbox flipped to complete; traceability table row updated to "Complete".

## Decisions Made
- Documented the Path-B firing honestly as memory-driven (zip-out OOM at 8 MiB on 1g), not CAP-driven -- the 900s CAP was never close to being breached (65s clears it with 92.8% margin). This is the key finding the plan's `<CRITICAL_do_not_rederive>` block called out to record honestly rather than silently folding into a CAP narrative.
- Kept the pandoc-worker `cpus:2.0`/`memory:1g` fleet baseline unchanged (matches the measurement container exactly) and updated its comment to note the memory limit is now load-bearing for the `PANDOC_MAX_INPUT_BYTES` cap, not just a generic resource ceiling.
- PDCE-04 marked complete at this plan (measured-timeout gate is literally what PDCE-04's text requires), mirroring the OCR-45 precedent where OCRE-04 was closed at 45-03 while the live E2E proof closed separately at 45-04.

## Deviations from Plan

None - plan executed exactly as written using the operator-approved, pinned measurement numbers. No re-measurement was performed; the numbers in the `<CRITICAL_do_not_rederive>` block were used verbatim.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required. Task 1's supervised measurement run was already completed and approved in a prior session.

## Next Phase Readiness
- Plan 04 (live E2E checkpoint) can now run `TestPandocConversionE2E` against the real compose `pandoc-worker` with the finalized 65s timeout / 85s stop_grace_period / 4 MiB input cap in place.
- Phase 50 (KEDA/Helm) can derive `terminationGracePeriodSeconds`/`scaleDownStabilizationSeconds` from this measured `PANDOC_ENGINE_TIMEOUT=65s` exactly as OCR/AV/AUDIO's own measured timeouts were consumed by their respective KEDA phases.
- No blockers.

---
*Phase: 49-containerization-measured-timeout-gate*
*Completed: 2026-08-01*
