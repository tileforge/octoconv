---
phase: 44-async-pipeline-wiring
plan: 01
subsystem: api
tags: [postgres, migration, ocr, tesseract, sniff, engine-routing, upload-limits]

# Dependency graph
requires:
  - phase: 43-ocr-engine-foundation
    provides: OCRConverter (Pairs/Engine/Convert), ocrguards.go/ocrpdf.go, ErrOCR* sentinels — built and unit-tested but deliberately NOT registered into convert.Default
provides:
  - "0009_ocr_engine.sql: additive migration admitting engine='ocr' into the jobs.engine CHECK constraint"
  - "OCRConverter{} registered into convert.Default (converters.go init()), making engine='ocr' reachable via the existing Registry"
  - "matchPDF signature (%PDF- magic) in the closed sniff.go signatures table — pdf is now a detectable content type"
  - "hocr->text/html and alto->application/xml MIMEType cases"
  - "EngineOCR:100<<20 in the default per-engine upload ceiling map (api.go)"
affects: [44-02-queue-wiring, 44-03-worker-wiring, 44-04-e2e-verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Target-based EngineFor disambiguation: registering a converter whose SOURCE format set is disjoint from every other registered converter's source set makes routing correct with zero new dispatch code, even when TARGET format strings (e.g. txt) are shared across engines"
    - "Cross-converter pair-disjointness unit test (mirrors TestAVAudioPairDisjointness) as the sole runtime guard against Registry.Register's silent last-write-wins semantics"

key-files:
  created:
    - internal/db/migrations/0009_ocr_engine.sql
  modified:
    - internal/jobs/repo_test.go
    - internal/convert/sniff.go
    - internal/convert/sniff_test.go
    - internal/convert/converters.go
    - internal/convert/convert_test.go
    - internal/convert/pairs_disjoint_test.go
    - internal/api/api.go
    - internal/api/handlers_test.go

key-decisions:
  - "No new routing logic added anywhere (D-01): the existing Registry.EngineFor(from,to) already disambiguates by (source,target) pair; registering OCRConverter{} was the only wiring needed for jpg/png/tiff/webp/pdf -> txt/hocr/alto to route to engine='ocr'"
  - "matchPDF tolerates up to 3 leading whitespace bytes before %PDF- (not the full spec-permitted 1024-byte scan window) — a deliberately small tolerance given sniffLen is only 12 bytes"
  - "hOCR stored/served as text/html, ALTO as application/xml — no new hocr/alto-specific MIME types invented"
  - "EngineOCR upload ceiling set to 100 MiB, matching image (D-03) — env-tunable via the existing Config.MaxEngineBytes override, no new env parse site added"
  - "handleCreateJob's enqueue-routing switch (handlers.go:641) was deliberately left untouched — an OCR job created via the live API today will fail at the enqueue step (falls into the existing fail-closed default branch, 500) until 44-02 adds EnqueueOCRConvert; this is the expected, scoped interim state, not a bug in this plan"

patterns-established:
  - "Cross-converter pair-disjointness test per new engine registration (TestOCRPairsDisjointFromOtherEngines added alongside the pre-existing TestAVAudioPairDisjointness)"

requirements-completed: [OCRP-01, OCRE-01]

# Metrics
duration: 35min
completed: 2026-07-26
---

# Phase 44 Plan 01: OCR Routing + Migration Foundation Summary

**Migration 0009 admits engine='ocr'; OCRConverter registered into convert.Default with matchPDF sniffing and a 100 MiB EngineOCR upload ceiling — zero new routing code, target-based EngineFor disambiguation alone routes jpg/pdf->txt to ocr vs jpg->png to image.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-26T17:55:00+03:00 (approx)
- **Completed:** 2026-07-26T18:14:00+03:00
- **Tasks:** 3 completed
- **Files modified:** 9 (1 created, 8 modified)

## Accomplishments
- `0009_ocr_engine.sql` additively extends `jobs_engine_check` to admit `'ocr'`, mirroring `0006_audio_engine.sql`'s exact DROP+ADD CONSTRAINT shape; verified live against `octoconv-db` (:5434) via `go run ./cmd/migrate` and a gated round-trip test (`TestOCREngineRoundTrip`)
- `OCRConverter{}` registered in `converters.go`'s `init()`, after `AVConverter` — its source set (`jpg/png/tiff/webp/pdf`) is disjoint from every other registered converter's source set, so `Registry.EngineFor` now routes `(jpg,txt)`/`(pdf,txt)`/etc. to `"ocr"` with zero new dispatch code, while `(jpg,png)` still correctly resolves to `"image"`
- `matchPDF` added to the closed `sniff.go` signatures table (`%PDF-` magic, small leading-whitespace tolerance) — PDF is now a first-class detectable content type
- `hocr`/`alto` MIMEType cases added (`text/html` / `application/xml`)
- `EngineOCR: 100 << 20` added to the default per-engine upload ceiling map in `api.go` (D-03)
- New tests prove the declared/detected PDF-vs-image mismatch is rejected 422 (OCRE-01, fail-closed before any `storage.Upload` call) via the pre-existing mismatch path, and an over-limit OCR-routed upload is rejected 413 before `Upload`

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0009_ocr_engine.sql** — `8969544` (feat) — additive ALTER + gated round-trip test, verified live
2. **Task 2: Register OCRConverter + matchPDF sniffer + hocr/alto MIME + EngineFor routing test** — `8751363` (feat)
3. **Task 3: EngineOCR upload ceiling + OCRE-01 422 fail-closed test** — `fc343ec` (feat)

_Note: no separate plan-metadata commit yet — this SUMMARY commit serves that role per the state_updates step._

## Files Created/Modified
- `internal/db/migrations/0009_ocr_engine.sql` - Additive `jobs_engine_check` extension admitting `'ocr'`
- `internal/jobs/repo_test.go` - `TestOCREngineRoundTrip` (Create+Get with engine='ocr')
- `internal/convert/sniff.go` - `matchPDF` + signatures table entry + hocr/alto MIMEType cases
- `internal/convert/sniff_test.go` - matchPDF true/false/full-stream-preservation cases, hocr/alto MIMEType cases
- `internal/convert/converters.go` - `Default.Register(OCRConverter{})` in `init()`
- `internal/convert/convert_test.go` - `TestRegistryEngineForOCRRouting` (jpg/txt=ocr, pdf/txt=ocr, jpg/png=image, full pairs matrix)
- `internal/convert/pairs_disjoint_test.go` - `TestOCRPairsDisjointFromOtherEngines` (new, mirrors the existing AV/Audio disjointness test)
- `internal/api/api.go` - `EngineOCR: 100 << 20` in the default `MaxEngineBytes` map + doc-comment updates
- `internal/api/handlers_test.go` - two mismatch-fail-closed tests + one over-limit-413 test + `TestNewServer_MaxEngineBytesDefaults` extended to cover `ocr`

## Decisions Made
- Registered `OCRConverter{}` as a bare zero-value (mirrors `AVConverter{}`'s own zero-value contract) — a future `cmd/ocr-worker/main.go` (Plan 02/03) may re-register a configured instance with `MaxImagePixels`/`MaxPages` before consuming tasks, using the same documented last-write-wins semantics
- Added `TestOCRPairsDisjointFromOtherEngines` (not explicitly in `files_modified`, but the same file the plan's own converters.go comment cross-references) as a Rule 2 correctness safeguard — mirrors the pre-existing `TestAVAudioPairDisjointness` and is the sole runtime guard against `Registry.Register`'s silent last-write-wins semantics for this new registration
- Left `handleCreateJob`'s enqueue-routing switch untouched per the scope fence — an OCR job created through the live API today uploads to storage, creates a `queued` DB row, then fails at enqueue (existing fail-closed default branch, 500) until 44-02 wires `EnqueueOCRConvert`. This is the documented, intentional interim state for wave-1-only completion, not a defect in this plan.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added cross-converter pair-disjointness test for OCRConverter's registration**
- **Found during:** Task 2 (Register OCRConverter + matchPDF sniffer)
- **Issue:** The plan's own `converters.go` action step calls for a comment "mirror[ing] the AVConverter registration comment on disjointness," but the existing disjointness guard (`TestAVAudioPairDisjointness`) only covers AV vs Audio — it does not verify OCRConverter's new pairs against any other converter. Without a matching test, a future edit that widens OCRConverter's target set (or another converter's source set) could silently collide with zero error, exactly the hazard the plan itself calls out.
- **Fix:** Added `TestOCRPairsDisjointFromOtherEngines` to `pairs_disjoint_test.go`, checking OCRConverter's normalized pairs against LibvipsConverter, AudioConverter, and AVConverter (both an identity-collision loop and a union-length check, mirroring the existing test's two-assertion shape).
- **Files modified:** internal/convert/pairs_disjoint_test.go
- **Verification:** `go test ./internal/convert/... -run Disjoint -v` — both tests pass.
- **Committed in:** 8751363 (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 missing critical / Rule 2)
**Impact on plan:** Pure test-coverage addition mirroring an existing pattern; no production code path affected, no scope creep beyond the plan's own stated intent for this registration.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required. The 0009 migration was already applied live against `octoconv-db` (:5434) as part of task verification.

## Next Phase Readiness
- `jobs.engine='ocr'` is now a valid, tested DB value; `convert.Default.EngineFor` routes every OCR (source,target) pair correctly; upload-side detection (matchPDF) and size ceiling (EngineOCR) are in place.
- **Known gap for 44-02/44-03 to close:** `handleCreateJob`'s enqueue switch has no `case convert.EngineOCR`, and the `Enqueuer` interface has no `EnqueueOCRConvert` method — an OCR job POSTed today will upload+create-row successfully but then 500 at enqueue. This is expected per the scope fence and is exactly what wave 2 (queue/worker wiring) closes.
- No worker/queue/progress.go/sse.go/exec.go files were touched — scope fence verified clean (`git diff --exit-code` empty on all three).

---
*Phase: 44-async-pipeline-wiring*
*Completed: 2026-07-26*

## Self-Check: PASSED

All 9 created/modified source files plus this SUMMARY.md verified present on disk; all 3 task commits (`8969544`, `8751363`, `fc343ec`) verified present in `git log`.
