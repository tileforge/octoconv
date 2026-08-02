---
phase: 49-containerization-measured-timeout-gate
plan: 04
subsystem: testing
tags: [pandoc, e2e, docx, html, webhook, docker]

requires:
  - phase: 49-containerization-measured-timeout-gate
    provides: Dockerfile.pandoc-worker (Plan 01), compose pandoc-worker service + measured PANDOC_ENGINE_TIMEOUT=65s + PANDOC_MAX_INPUT_BYTES=4MiB (Plan 03)
  - phase: 48-async-pipeline-wiring
    provides: the pandoc queue/worker contract
provides:
  - TestPandocConversionE2E — live md->html (text-out) + md->docx (zip-out) E2E, signed-webhook assertion
  - live proof of the containerized pandoc-worker with the measured timeout (SC1)
affects: [phase-50-keda-helm, pandoc]

tech-stack:
  added: []
  patterns:
    - "Pandoc E2E has no progress-ramp assertion (PandocConverter is not a ProgressConverter) — simpler than the OCR E2E: upload -> poll -> download -> webhook"
    - "Covering a text-out target (html) AND a zip-out target (docx) proves both distinct pandoc output shapes"

key-files:
  created:
    - .planning/phases/49-containerization-measured-timeout-gate/49-04-SUMMARY.md
  modified:
    - internal/e2e/e2e_test.go
    - internal/e2e/testdata/sample.md

key-decisions:
  - "Live E2E covers md->html (text-out) + md->docx (zip-out) + a signed webhook — the two output shapes D-03 calls out; no progress ramp (pandoc isn't a ProgressConverter)"

requirements-completed: [PDCE-04]

duration: ~30min
completed: 2026-08-01
---

# Phase 49 Plan 04: Live Pandoc E2E Summary

**TestPandocConversionE2E proves md->html (text-out) + md->docx (zip-out) end-to-end against the real pandoc-worker container (presigned download + HMAC-signed webhook) with the measured PANDOC_ENGINE_TIMEOUT=65s — closing Phase 49's SC1.**

## Performance
- **Duration:** ~30 min (Task 1 test authoring — autonomous; Task 2 live run — operator-approved)
- **Completed:** 2026-08-01
- **Tasks:** 2/2
- **Files modified:** 2

## Accomplishments
- `TestPandocConversionE2E` authored (Task 1, commit `9d1a406`): two subtests (md->html, md->docx), self-skips offline, signed-webhook HMAC assertion, no progress-ramp (pandoc isn't a ProgressConverter).
- Live run GREEN against the real compose `pandoc-worker` (pandoc 3.10.1 pinned static, `PANDOC_ENGINE_TIMEOUT=65s`, `PANDOC_MAX_INPUT_BYTES=4194304`): `PASS TestPandocConversionE2E (4.13s)` — MD_ToHTML 2.05s, MD_ToDOCX 2.03s.
- **SC1 proven**: the lean TeX-free pandoc-worker container converts an uploaded markup document end-to-end (upload -> poll -> presigned txt/docx download) with a signed webhook confirmed. DB job_events show md->docx / md->html / html->md all `done`, worker with no retry-exhaust.

## Task Commits
1. **Task 1: Author TestPandocConversionE2E** — `9d1a406` (test)
2. **Task 2: Live E2E run** — operator-run (evidence committed with this summary; docs commit)

## Files Created/Modified
- `internal/e2e/e2e_test.go` — TestPandocConversionE2E (MD_ToHTML + MD_ToDOCX subtests, reusing e2eSetup/postJob/pollUntilDone/assertDownloadContainsText/assertDownloadIsFormat/assertSignedWebhook)
- `internal/e2e/testdata/sample.md` — markdown fixture (heading + bold/emphasis + bullet list)

## Decisions Made
- md->html asserts the rendered HTML contains the fixture heading text (proving a real `--standalone` document, not a bare fragment); md->docx asserts a valid zip/OOXML container. No opts needed.

## Deviations from Plan
None — plan executed exactly as written.

## Issues Encountered
- Built api + pandoc-worker :dev images (compose build produces :latest → tagged :dev to match the chart/compose imageTag convention); brought up only the pandoc-relevant services (postgres/redis/minio/api/pandoc-worker/webhook-worker×2) rather than the full stack. Stack torn down clean after the run.

## User Setup Required
None.

## Next Phase Readiness
- PDCE-04 fully closed: measured PANDOC_ENGINE_TIMEOUT=65s live-validated end-to-end against the real container.
- Ready for Phase 50 (KEDA/Helm): the pandoc-worker image, measured `PANDOC_ENGINE_TIMEOUT=65s`, and `stop_grace_period=85s` are the inputs the ScaledObject/`terminationGracePeriodSeconds` derive from.

---
*Phase: 49-containerization-measured-timeout-gate*
*Completed: 2026-08-01*
