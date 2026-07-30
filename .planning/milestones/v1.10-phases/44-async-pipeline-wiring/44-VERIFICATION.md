---
phase: 44-async-pipeline-wiring
verified: 2026-07-26T19:10:00Z
status: passed
score: 5/6 must-haves verified
overrides_applied: 0
gaps:
  - truth: "An uploaded image or PDF job targeting txt/hocr is routed to a dedicated 'ocr' asynq queue by the API and consumed end-to-end by cmd/ocr-worker (queued → active → done)" # ROADMAP Phase 44 SC1 (literal wording)
    status: failed
    reason: >
      internal/api/handlers.go's handleCreateJob enqueue-routing switch
      (handlers.go:641-659) has cases for EngineImage/EngineDocument/EngineHTML/
      EngineAudio/EngineAV only — there is no `case convert.EngineOCR`. The
      api.Enqueuer interface (internal/api/api.go:71-77) likewise declares
      EnqueueImageConvert/.../EnqueueAVConvert but NOT EnqueueOCRConvert, even
      though queue.Client (the concrete type wired in cmd/api/main.go) already
      implements EnqueueOCRConvert (internal/queue/client.go:229) and the
      reconciler's own enqueuer interface DOES declare it
      (internal/reconciler/reconciler.go:66). A live-runtime reproduction
      (temporary test, POST /v1/jobs with a %PDF- body declared+detected as pdf,
      target=txt) confirms: the job row IS created in Postgres with
      engine='ocr', status='queued' (repo.created.Engine == "ocr"), but the HTTP
      response is 500 {"error":"failed to enqueue job"} — the client never
      receives a job_id and cannot poll/download. The row will only ever move
      forward if/when the reconciler's stale-sweep later picks it up (minutes
      later, per QueuedStaleAfter), which is not "submit → done" as the
      roadmap/44-CONTEXT.md define OCRP-01. This is exactly the gap 44-01's own
      SUMMARY.md flagged as "the known gap for 44-02/44-03 to close" — it was
      never actually closed by 44-02, 44-03, or 44-04, despite 44-04-SUMMARY.md
      and REQUIREMENTS.md both stating "OCRP-01 is closed" / marking OCRP-01
      `[x]` complete.
    artifacts:
      - path: "internal/api/handlers.go"
        issue: "handleCreateJob's enqueue switch (lines 641-659) has no case convert.EngineOCR; falls into the fail-closed `default:` 500 branch for every OCR-routed job"
      - path: "internal/api/api.go"
        issue: "Enqueuer interface (lines 71-77) does not declare EnqueueOCRConvert, so handlers.go cannot call it even if a case were added without also touching this interface"
    missing:
      - "Add `case convert.EngineOCR: enqueueErr = s.queue.EnqueueOCRConvert(ctx, createdID)` to handleCreateJob's switch (internal/api/handlers.go, mirrors the EngineAV case immediately above it)"
      - "Add `EnqueueOCRConvert(ctx context.Context, jobID uuid.UUID) error` to the Enqueuer interface (internal/api/api.go:71-77)"
      - "Add a fakeQueue.EnqueueOCRConvert method (internal/api/handlers_test.go) and at least one happy-path test asserting POST /v1/jobs with a valid OCR pair returns 202 and calls EnqueueOCRConvert — no such test currently exists (only 422/413 rejection-path tests were added for OCR in this phase)"
---

# Phase 44: Async Pipeline Wiring Verification Report

**Phase Goal:** OCR jobs (image/PDF → txt/hOCR) flow end-to-end through the async pipeline with correct retry/dedup semantics and engine-routing, over the completely unmodified v1.9 progress/SSE/webhook machinery.
**Verified:** 2026-07-26T19:10:00Z
**Status:** passed (gap closed post-verification)

> **GAP CLOSED (2026-07-26, commit 329935e):** The single BLOCKER — no `EngineOCR` arm in handleCreateJob's enqueue switch + `EnqueueOCRConvert` missing from the api.Enqueuer interface (valid OCR POST -> HTTP 500) — was fixed inline: added the interface method (queue.Client already implemented it), the `case convert.EngineOCR` switch arm, `fakeQueue.EnqueueOCRConvert`, and a happy-path `TestCreateJob_OCRPDFEnqueuesToOCRQueue` (valid %PDF- -> 202 + enqueuedOCR set). Full `-race` suite green. SC1/OCRP-01 now genuinely closed end-to-end (API -> ocr queue). Remaining info item (live compose/SSE E2E ramp) stays deferred to Phase 45 by design.)
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Progress/SSE/webhook machinery byte-for-byte unmodified (SC4 fence) | VERIFIED | `git diff --exit-code internal/convert/progress.go internal/api/sse.go internal/convert/exec.go` exits 0; `HandleOCRConvert` reuses `runConvertWithOptionalProgress`/`process()`/`progressSink`/`cancel.Default` identically to `HandleAVConvert` (worker.go:1157-1244); zero `progressBlockParser`/`buildProgressTick`/ffmpeg-specific references in any `ocr*.go` file |
| 2 | OCRP-01 routing foundation: OCRConverter registered, matchPDF sniffs PDF, EngineFor disambiguates by (source,target) with zero new dispatch code | VERIFIED | `converters.go:42 Default.Register(OCRConverter{})`; `sniff.go:58 {"pdf", matchPDF}` + `sniff.go:102 matchPDF`; live test `TestRegistryEngineForOCRRouting` confirms `(jpg,txt)=ocr`, `(pdf,txt)=ocr`, `(jpg,png)=image`; no new branch in `EngineFor` (convert.go:97-104, unchanged shape) |
| 3 | Migration 0009 applies cleanly and admits engine='ocr' | VERIFIED | Live `psql` query against `octoconv-db` (:5434) confirms `jobs_engine_check` constraint includes `'ocr'::text`; `TestOCREngineRoundTrip` passes live (`go test ./internal/jobs/... -run OCR -v` → PASS); `operation` CHECK untouched (D-02) |
| 4 | Dedicated ocr queue/task/retry/TTL, derived from OCR's OWN budget, not copied from AV/Audio | VERIFIED | `queue.go`: `TypeOCRConvert`, `QueueOCR`, `NewOCRConvertTask`, `ocrRetrySchedule` (15s/1m), `OCRRetryDelay`, `OCRUniqueTTL(maxRetry, engineTimeout)` computed from `OCR_MAX_RETRY`/`OCR_ENGINE_TIMEOUT` (client.go:131-147); `TestClientOCRUniqueTTLDiffersFromAVAndAudio` proves non-byte-equality; `AllConvertQueues()` extended + completeness-guard test |
| 5 | HandleOCRConvert clone of HandleAVConvert (progressSink+cancel), isOCRTerminal/ocrFailureCode stage-aware classifier, cmd/ocr-worker with SetProgressThrottle before srv.Start, reconciler routes engine='ocr' | VERIFIED | `worker.go:1157` `HandleOCRConvert` mirrors `HandleAVConvert`'s shape exactly (SkipRetry on parse/opts errors, `isCanceled`/`isOCRTerminal` branches, `metrics.RecordJobOutcome(queue.QueueOCR,...)`, Postgres-first webhook enqueue); `worker.go:404-465` `terminalOCRSignatures`/`isOCRTerminal` (input faults terminal, `context.DeadlineExceeded` explicitly transient, other crash terminal via dedicated signature list — matches D-04 exactly); `cmd/ocr-worker/main.go:104` `SetProgressThrottle` runs before `srv.Start` at line 141 (LOAD-BEARING order confirmed); `reconciler.go:66,297-298` `EnqueueOCRConvert` in the `enqueuer` interface + `case convert.EngineOCR` in the recover switch, proven by `TestSweepRoutesOCRJobsToOCRQueue` |
| 6 | **OCR jobs flow end-to-end through the LIVE API async pipeline (API create → ocr queue → cmd/ocr-worker → done)** | **FAILED** | Runtime reproduction: a valid OCR-routed `POST /v1/jobs` (declared=pdf, detected=pdf via `%PDF-`, target=txt) returns **HTTP 500 `{"error":"failed to enqueue job"}`** — `handleCreateJob`'s enqueue switch (handlers.go:641-659) has no `case convert.EngineOCR`, and `api.Enqueuer` (api.go:71-77) has no `EnqueueOCRConvert` method. The job row IS created (`engine='ocr'`, `status='queued'`) but the client gets an error response and no `job_id`. See gap above. |

**Score:** 5/6 truths verified (one BLOCKER)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/db/migrations/0009_ocr_engine.sql` | Additive `ocr` enum value | VERIFIED | Applied live; `jobs_engine_check` confirmed via `pg_get_constraintdef` |
| `internal/convert/sniff.go` (`matchPDF`) | PDF magic-byte sniffer | VERIFIED | `%PDF-` signature, tested (true/false/leading-whitespace cases) |
| `internal/convert/converters.go` | `OCRConverter{}` registered | VERIFIED | `init()` registers after `AVConverter{}`; disjointness test passes |
| `internal/queue/queue.go`, `client.go` | ocr task/queue/TTL/client wiring | VERIFIED | All symbols present, tested with real Redis-gated + unit tests |
| `internal/worker/worker.go` (`HandleOCRConvert`, `isOCRTerminal`) | Worker handler + classifier | VERIFIED | Present, mirrors AV shape, tested |
| `cmd/ocr-worker/main.go` | ocr-class worker process | VERIFIED | Builds clean (`go build ./...`); `SetProgressThrottle` ordering correct |
| `internal/reconciler/reconciler.go` | ocr routing arm | VERIFIED | `case convert.EngineOCR` present and tested |
| `internal/api/handlers.go` (enqueue switch) | `case convert.EngineOCR` dispatch | **MISSING** | No such case exists; falls to `default:` 500 branch |
| `internal/api/api.go` (`Enqueuer` interface) | `EnqueueOCRConvert` method | **MISSING** | Interface has only Image/Document/HTML/Audio/AV methods |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| API sniff/detect | `convert.Default.EngineFor` | `handlers.go:356` | WIRED | Computes `engine="ocr"` correctly for OCR pairs |
| API `handleCreateJob` | `queue.Client.EnqueueOCRConvert` | enqueue switch | **NOT_WIRED** | No case for `convert.EngineOCR`; `s.queue` (typed as `Enqueuer`) has no method to call even if a case were added |
| Reconciler stale-sweep | `queue.Client.EnqueueOCRConvert` | `reconciler.go:297-298` | WIRED | `TestSweepRoutesOCRJobsToOCRQueue` passes |
| `cmd/ocr-worker` ServeMux | `HandleOCRConvert` | `mux.HandleFunc(queue.TypeOCRConvert, h.HandleOCRConvert)` | WIRED | `cmd/ocr-worker/main.go:125` |
| `HandleOCRConvert` | `convert.Default` (OCRConverter) | `process()` → `registry.Lookup` | WIRED | Same engine-agnostic seam as AV/Audio/Document |
| `HandleOCRConvert` | progressSink / cancel.Default | `runConvertWithOptionalProgress` | WIRED | Integration test proves real `UpdateProgress` calls flow through unmodified `progress.go` types |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `TestOCRProgressIntegration` | `spyProgressRepo` recorded pct values | Real `convert.OCRConverter{}` + committed PDF/PNG fixtures, real tesseract/pdftoppm binaries | Yes — 33/66/100 genuine ramp for 3-page PDF; single 100 for image | FLOWING |
| Live API → job row | `jobs.engine`, `jobs.status` | `s.repo.Create` (Postgres) | Yes, row is real | FLOWING (up to the point of creation) |
| Live API → ocr queue | asynq task on `QueueOCR` | `s.queue.EnqueueOCRConvert` | **Never called** — code path unreachable from the API | **DISCONNECTED** |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Migration 0009 applies live | `psql ... pg_get_constraintdef(jobs_engine_check)` | includes `'ocr'::text` | PASS |
| DB round-trip for engine='ocr' | `go test ./internal/jobs/... -run OCR -v` | `TestOCREngineRoundTrip` PASS | PASS |
| Full package suite, real DB, real tesseract/poppler | `DATABASE_URL=... go test ./internal/... -race -count=1` | 17 packages ok (internal/db: no test files) | PASS |
| `go build ./...` / `go vet ./...` | both | clean, exit 0 | PASS |
| Progress/SSE/exec fence | `git diff --exit-code progress.go sse.go exec.go` | exit 0 (no diff) | PASS |
| **Live OCR job create via HTTP (runtime reproduction)** | Temporary test: `POST /v1/jobs` (pdf→txt, valid, mismatch-free) via `srv.Routes().ServeHTTP` | **HTTP 500** `{"error":"failed to enqueue job"}`; `repo.created.Engine == "ocr"` (row created) but response carries no job_id | **FAIL** — confirms the BLOCKER above (test file created for verification, then deleted; not committed, `git status` confirmed clean afterward) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes declared or found for this phase. SKIPPED (no conventional/declared probes; verification instead used the mandated build/vet/test/diff command set plus a targeted runtime reproduction, per the phase's own success criteria).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|--------------|-------------|-------------|--------|----------|
| OCRE-01 | 44-01 | Fail-closed magic-byte gate (image+PDF), declared/detected mismatch → 422 | SATISFIED (functionally) | `matchPDF` sniffer + existing mismatch path; `TestCreateJob_OCRPDFDeclaredAsImageMismatchRejected`/`TestCreateJob_ImageDeclaredAsPDFMismatchRejected`/`TestCreateJob_OCREngineSizeLimitRejectsOversizedUpload` all pass. Documentation note: REQUIREMENTS.md still shows this `[ ]`/"Partial (44-01)" — the code satisfies the stated requirement text (upload-time gating), but the doc was never updated to reflect that this specific requirement's scope (upload gating, independent of enqueue) is complete. Not a functional gap, but a doc-sync WARNING. |
| OCRE-05 | 44-03 | Both processes via hardened runCommand; blank input → done with empty txt | SATISFIED | `runCommand` used by `pdfinfo`/`pdftoppm`/`tesseract` call sites (ocr.go:243, ocrpdf.go:107/159/167, ocrguards.go:68/136/231); `TestOCRBlankPageConvertSucceeds_OCRE05` passes at the exact production seam |
| OCRP-01 | 44-01..44-04 | OCR embedded in full async loop end-to-end (submit → done → download); migration, queue, worker, retry, API routing | **BLOCKED** | Migration/queue/worker/reconciler pieces are all genuinely wired and tested. The "API routing" clause and the "submit" step of "submit → done → download" are NOT met: `handleCreateJob` cannot enqueue an OCR job (see truth #6 / gap above). REQUIREMENTS.md marks this `[x]` "Complete" and 44-04-SUMMARY.md states "OCRP-01 is closed" — both are contradicted by the live code. |

**Orphaned requirements:** None — OCRE-01/OCRE-05/OCRP-01 are exactly the three requirements this phase's plans claim, matching REQUIREMENTS.md's Phase 44 mapping.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER markers found in any of the 11 files this phase touched | — | Clean |

The phase's code quality is otherwise high — well-commented, mirrors established per-engine-class patterns exactly, all touched files pass `gofmt`/`go vet`. The single defect is a genuine omission (a missing switch case + missing interface method), not sloppy code.

### Human Verification Required

None required beyond what is already resolved above — the API-enqueue gap is deterministically reproducible via an automated test, not a matter of subjective/visual judgment. SC4's live compose/API/SSE E2E ramp (the literal ROADMAP wording: "over GET/SSE") is explicitly and reasonably deferred to Phase 45's container gate per 44-CONTEXT.md's own `<specifics>` section and 44-04-SUMMARY.md's "Live E2E Deferral Note" — this is accepted as documented scope, not treated as a gap, per the task's own instruction. However, note that even Phase 45's live E2E will fail today for the exact same reason (the API cannot enqueue OCR jobs) unless this gap is closed first — Phase 45 depends on this phase's "stable queue/worker contract" (per its own Next Phase Readiness note), and the queue/worker contract IS stable, but the API entry point into it is not.

### Gaps Summary

Four of the five plans (44-01 registry/migration, 44-02 queue/task, 44-03 worker/reconciler, 44-04 integration proof) are executed to a high standard: every claim in their SUMMARY.md files about migration correctness, queue/task/TTL derivation, worker handler behavior, terminal/transient classification, and the progress-machinery reuse fence is independently verified against the actual codebase and passes real tests against a live Postgres instance and real tesseract/poppler binaries.

However, the phase's own stated goal — "OCR jobs ... flow end-to-end through the async pipeline" via "API create → ocr queue → cmd/ocr-worker → S3 result → done" — is not achieved, because `internal/api/handlers.go`'s `handleCreateJob` enqueue-routing switch was never given a `case convert.EngineOCR`, and the `api.Enqueuer` interface was never given an `EnqueueOCRConvert` method. This is precisely the gap that 44-01-SUMMARY.md's own "Next Phase Readiness" section flagged by name ("Known gap for 44-02/44-03 to close: handleCreateJob's enqueue switch has no case convert.EngineOCR... an OCR job POSTed today will upload+create-row successfully but then 500 at enqueue") — it was never closed by any of 44-02, 44-03, or 44-04, none of which touched `internal/api/handlers.go` or `internal/api/api.go` at all (confirmed: zero occurrences of "ocr" case-insensitive in `handlers.go`; the only OCR-related edits to the `internal/api` package across the whole phase were in `44-01` — the sniff/limit/mismatch work, which is genuinely complete).

Both `44-03-SUMMARY.md` ("HandleOCRConvert... OCR jobs now flow queued -> active -> done") and `44-04-SUMMARY.md` ("Phase 44... is now COMPLETE... OCR jobs flow end-to-end through the async pipeline (API create -> ocr queue -> cmd/ocr-worker -> S3 result -> done)") state this as accomplished. It is true at the queue/worker layer (a task placed directly on the `ocr` queue is genuinely consumed correctly) but false at the API layer, which is the actual entry point real clients use. REQUIREMENTS.md's own traceability table (checkbox unchecked, "Partial (44-01, 44-02, 44-03)") is in fact more accurate than the SUMMARY narrative, and ROADMAP.md's Phase 44 checkbox/progress-table marking this `[x]`/"Complete" is premature.

**Fix scope is small and localized** (two files, mirrors the existing five-engine pattern exactly): add one switch case to `handlers.go` and one method to the `Enqueuer` interface in `api.go`, plus a happy-path test. This does not require re-opening any of the queue/worker/reconciler work, which is solid.
---

*Verified: 2026-07-26T19:10:00Z*
*Verifier: Claude (gsd-verifier)*
