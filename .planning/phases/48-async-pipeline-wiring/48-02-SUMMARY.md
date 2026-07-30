---
phase: 48-async-pipeline-wiring
plan: 02
subsystem: api
tags: [pandoc, convert-registry, content-detection, opts-validation, postgres, jobs-engine]

# Dependency graph
requires:
  - phase: 48-01
    provides: "TypePandocConvert/QueuePandoc/NewPandocConvertTask/PandocUniqueTTL/EnqueuePandocConvert on *queue.Client (reused verbatim, not re-derived)"
  - phase: 47-pandoc-engine-foundation
    provides: "PandocConverter (Pairs/Engine/Convert), PandocOpts/ParsePandocOpts/ValidatePandocApplicability, LooksLikeLaTeX/LooksLikeText/enforcePandocInputSize"
provides:
  - "0010_pandoc_engine.sql: additive ALTER admitting 'pandoc' into jobs.engine CHECK enum"
  - "PandocConverter{} registered in convert.Default (converters.go init()), disjointness proven by TestPandocPairsDisjointFromOtherEngines"
  - "EngineFor routes (md,html)/(docx,md)/(html,md)->pandoc, (docx,pdf)->document, (html,pdf)->html(chromium) with zero new dispatch code"
  - "EnqueuePandocConvert on the Enqueuer interface + fakeQueue test double"
  - "EnginePandoc 100 MiB upload ceiling in cfg.MaxEngineBytes"
  - "latex/md/rst/org markup-family content-gate branches in the detected=='' chain (LooksLikeLaTeX/LooksLikeText)"
  - "EnginePandoc opts-dispatch case (ParsePandocOpts + ValidatePandocApplicability) and enqueue-switch case in handleCreateJob"
affects: [48-03-worker-wiring, 49-pandoc-containerization]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Registration-only routing: registering a Converter in converters.go's init() is sufficient for EngineFor to disambiguate by (source,target) -- zero new dispatch code needed in convert.go"
    - "Registry.Register's silent last-write-wins is guarded exclusively by a per-engine TestXPairsDisjointFromOtherEngines test, never a runtime check"
    - "Content-family text detection (md/rst/org) trusts the client's declared source once a fail-closed plaintext gate (UTF-8, no NUL, no binary-control bytes) passes -- an accepted, documented degradation from format-specific to family-level detection"

key-files:
  created:
    - internal/db/migrations/0010_pandoc_engine.sql
  modified:
    - internal/convert/converters.go
    - internal/convert/pairs_disjoint_test.go
    - internal/convert/convert_test.go
    - internal/jobs/repo_test.go
    - internal/api/api.go
    - internal/api/handlers.go
    - internal/api/handlers_test.go

key-decisions:
  - "Mirrored 0009_ocr_engine.sql's comment style verbatim for 0010, including the sentence documenting that the operation CHECK is untouched -- the literal plan acceptance-criterion grep expecting zero occurrences of the substring \"operation\" conflicts with matching the precedent's own documented style (0009 itself contains the same substring twice); the SQL statements make no functional change to the operation column/constraint, which is what actually matters"
  - "enforcePandocInputSize is NOT invoked directly from the API -- it already runs inside PandocConverter.Convert (Phase 47) at worker/converter time; the API-level EnginePandoc MaxEngineBytes ceiling is a separate, complementary upload-time gate, documented as such in the ceiling's comment"
  - "Latex test fixture uses filename extension .latex (not .tex) since NormalizeFormat has no tex->latex alias -- source must literally equal \"latex\" for the LooksLikeLaTeX branch to trigger"

patterns-established:
  - "Markup-family content-gate: latex source gated on LooksLikeLaTeX; md/rst/org sources share LooksLikeText's weaker family-level plaintext gate and trust the declared source thereafter"

requirements-completed: []  # PDCP-01 spans all 3 plans of Phase 48 -- NOT marked complete here; 48-03 closes it

# Metrics
duration: 9min
completed: 2026-07-30
---

# Phase 48 Plan 02: API Routing + Content-Gate Wiring for Pandoc Summary

**Registered PandocConverter into convert.Default (zero new routing code), added migration 0010 admitting engine='pandoc', and wired EnginePandoc through the API's opts-dispatch/enqueue switches plus the latex/md/rst/org content-detection chain.**

## Performance

- **Duration:** 9 min
- **Started:** 2026-07-30T19:24:31Z
- **Completed:** 2026-07-30T19:33:12Z
- **Tasks:** 2
- **Files modified:** 8 (1 created, 7 modified)

## Accomplishments
- `0010_pandoc_engine.sql` admits `'pandoc'` into the `jobs.engine` CHECK enum, additive only, verified against a live populated `octoconv-db` on `:5434` (Postgres-gated round-trip test passes)
- `PandocConverter{}` registered last in `converters.go init()`; `TestPandocPairsDisjointFromOtherEngines` proves zero collision against every other converter (Libvips/LibreOffice/Chromium/Audio/AV/OCR) -- the sole guard against `Registry.Register`'s silent last-write-wins
- `EngineFor` now routes `(md,html)`/`(docx,md)`/`(html,md)` to `pandoc` while `(docx,pdf)` stays `document` and `(html,pdf)` stays `html` (chromium) -- proven by `TestRegistryEngineForPandocRouting`, zero new dispatch code
- API wiring complete: `EnqueuePandocConvert` on the `Enqueuer` interface, `EnginePandoc` 100 MiB upload ceiling, `latex`/`md`/`rst`/`org` content-gate branches, `EnginePandoc` opts-dispatch case (`ParsePandocOpts` + `ValidatePandocApplicability`), and enqueue-switch case -- a markdown upload now returns 202 and is routed to the pandoc queue end-to-end

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0010 + register PandocConverter + disjointness/routing tests** - `775acc0` (feat)
2. **Task 2: API wiring — Enqueuer interface + maxEngineBytes + EnginePandoc opts/enqueue cases + markup content-gate** - `d47a04b` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified
- `internal/db/migrations/0010_pandoc_engine.sql` - additive ALTER admitting `'pandoc'` into `jobs.engine` CHECK
- `internal/convert/converters.go` - registers `PandocConverter{}` last in `init()` with a disjointness rationale comment
- `internal/convert/pairs_disjoint_test.go` - adds `TestPandocPairsDisjointFromOtherEngines`
- `internal/convert/convert_test.go` - adds `TestRegistryEngineForPandocRouting`
- `internal/jobs/repo_test.go` - adds `TestPandocEngineRoundTrip` (Postgres-gated)
- `internal/api/api.go` - adds `EnqueuePandocConvert` to `Enqueuer`; adds `convert.EnginePandoc: 100 << 20` to the `MaxEngineBytes` default map
- `internal/api/handlers.go` - adds latex/md/rst/org content-gate branches to the `detected==""` chain; adds `case convert.EnginePandoc` to the opts-dispatch switch and the enqueue-switch
- `internal/api/handlers_test.go` - adds `EnqueuePandocConvert` to `fakeQueue`; adds `TestCreateJob_PandocMarkdownEnqueuesToPandocQueue`, `TestCreateJob_PandocInvalidOptsRejectedBeforeUpload`, `TestCreateJob_PandocBinaryContentDeclaredAsMarkdownRejected`

## Decisions Made
- Mirrored 0009's migration comment style verbatim (including the "operation CHECK is untouched" sentence) over satisfying a literal `grep -c "operation" == 0` acceptance-criterion check that would have required diverging from the explicitly-mandated precedent style; the functional intent (no ALTER on the operation constraint) holds regardless
- `enforcePandocInputSize` stays inside `PandocConverter.Convert` (already wired, Phase 47) rather than being re-invoked at the API layer; the API's `EnginePandoc` `MaxEngineBytes` ceiling is a separate, complementary upload-time bound, documented in-line as such
- Latex content-gate test fixture uses a `.latex` filename extension (not `.tex`) because `NormalizeFormat` has no `tex`->`latex` alias

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Test fixture extension mismatch caught before commit**
- **Found during:** Task 2 (writing `TestCreateJob_PandocInvalidOptsRejectedBeforeUpload`)
- **Issue:** Initial test used filename `in.tex`; `NormalizeFormat` has no `tex`->`latex` alias, so `source` resolved to `"tex"` rather than `"latex"`, causing the new `LooksLikeLaTeX` branch to never fire and the request to fail with `unrecognized_content` instead of exercising the intended `invalid_opts` path
- **Fix:** Renamed the fixture filename to `in.latex` so `source == "latex"` matches the content-gate branch condition
- **Files modified:** internal/api/handlers_test.go
- **Verification:** `go test ./internal/api/... -run TestCreateJob_PandocInvalidOptsRejectedBeforeUpload -v` now logs `reason=invalid_opts` and passes
- **Committed in:** d47a04b (Task 2 commit, fixed before commit)

---

**Total deviations:** 1 auto-fixed (test-fixture bug caught during development, fixed before commit)
**Impact on plan:** No scope creep; the fix stayed entirely inside the new test's own fixture.

## Documented Acceptance-Criterion Discrepancy (non-blocking)

Plan Task 1's acceptance criteria list `grep -c "operation" internal/db/migrations/0010_pandoc_engine.sql == 0`, but the same task's action text requires mirroring `0009_ocr_engine.sql`'s comment style, which itself documents (in prose) that the `operation` CHECK is untouched -- and contains the substring `operation` twice. `0010_pandoc_engine.sql` mirrors that style and therefore also contains the substring once. This is a literal grep-count mismatch against the acceptance criterion, not a functional defect: no SQL statement in the migration touches the `operation` column or its CHECK constraint. Verified with `psql`/`go test` against the live `octoconv-db` (`:5434`) that the migration applies cleanly on a populated DB and the `operation` constraint is unaffected.

## Issues Encountered
None beyond the auto-fixed test-fixture issue above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `engine='pandoc'` is now a first-class value at the DB + API boundary: a client can POST a markdown/latex/docx/epub upload and receive a 202 routed to the `pandoc` asynq queue
- Plan 03 (worker wiring) can now consume `EnqueuePandocConvert`-routed jobs: it needs `HandlePandocConvert`/`isPandocTerminal`/`pandocFailureCode`, `cmd/pandoc-worker`, and the reconciler `engine='pandoc'` recovery route
- **PDCP-01 requirement is NOT marked complete** -- it spans all 3 plans of Phase 48; Plan 03 closes it
- No blockers

---
*Phase: 48-async-pipeline-wiring*
*Completed: 2026-07-30*

## Self-Check: PASSED
