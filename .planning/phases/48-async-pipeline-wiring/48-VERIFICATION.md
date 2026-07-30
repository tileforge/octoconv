---
phase: 48-async-pipeline-wiring
verified: 2026-07-30T20:08:53Z
status: passed
score: 4/4 success criteria verified (10/10 derived must-have truths verified)
overrides_applied: 0
---

# Phase 48: Async Pipeline Wiring Verification Report

**Phase Goal:** Pandoc jobs (markup→markup) flow end-to-end through the async pipeline with correct retry/dedup semantics and engine-routing, over the UNMODIFIED v1.9 progress/webhook machinery. Requirement: PDCP-01.
**Verified:** 2026-07-30T20:08:53Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (mapped to ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 (SC1) | Migration adds `pandoc` to `jobs.engine` enum | VERIFIED | `internal/db/migrations/0010_pandoc_engine.sql` — additive `DROP/ADD CONSTRAINT jobs_engine_check ... IN (..., 'pandoc')`, mirrors 0009 verbatim; applied cleanly via `cmd/migrate` against live `octoconv-db:5434` (populated DB, no error). `TestPandocEngineRoundTrip` (internal/jobs/repo_test.go) passes against live Postgres. |
| 2 (SC1) | Markup upload targeting a supported pair is routed to a dedicated `pandoc` asynq queue by content-detection and consumed end-to-end by `cmd/pandoc-worker` (queued→active→done) | VERIFIED (live E2E) | Built `cmd/pandoc-worker` + `cmd/api` from source, ran them against the real Postgres(:5434)/Redis(:6379)/MinIO(:9100)/pandoc-3.10.1 stack. POSTed a real `.md` file to `POST /v1/jobs` (source=md,target=html) → `202 {"status":"queued"}`. Polled `GET /v1/jobs/{id}` → reached `"status":"done"` with a working presigned `download_url`; downloaded content is genuine pandoc-3.10.1-generated HTML containing the exact submitted text. `job_events` for that job_id shows `(none→queued), (queued→active), (active→done)`; `jobs.engine='pandoc'`. See Live E2E Proof below. |
| 3 (SC2) | Reconciler routes stranded `jobs.engine='pandoc'` jobs to the pandoc queue | VERIFIED | `internal/reconciler/reconciler.go:67` interface adds `EnqueuePandocConvert`; `:300-301` `case convert.EnginePandoc: enqueueErr = s.enq.EnqueuePandocConvert(...)`. `TestSweepRoutesPandocJobsToPandocQueue` and the per-engine matrix `TestSweepRoutesEveryEngineConstant` (adds `EnginePandoc`/`pandocCallIDs` row) pass. |
| 4 (SC2) | API routing dispatches single-input/single-target pandoc jobs WITHOUT any change to v1.9 progress/SSE/webhook infra | VERIFIED | `internal/api/handlers.go` opts-dispatch `case convert.EnginePandoc` (ParsePandocOpts+ValidatePandocApplicability) and enqueue-switch `case convert.EnginePandoc: EnqueuePandocConvert`; `EnginePandoc` upload ceiling in `api.go`'s `MaxEngineBytes` map. **Fence held:** `git diff --exit-code 51ab6dd..HEAD -- internal/convert/exec.go internal/convert/progress.go internal/api/sse.go` exits 0 (byte-for-byte unchanged across the whole phase, independently verified, not just SUMMARY's claim). `go.mod`/`go.sum` unchanged across the phase (zero new deps). |
| 5 (SC3) | Stage-aware terminal/transient classifier: `isPandocTerminal` treats timeout + pandoc engine/parse errors as TERMINAL (mirrors `isDocumentTerminal`, not `isOCRTerminal`) | VERIFIED | `internal/worker/worker.go:539-561` — `errors.Is(err, context.DeadlineExceeded)` → true, `errors.Is(err, convert.ErrPandocInputTooLarge)` → true, `strings.Contains(..., "pandoc:")` → true, else delegates to shared `isTerminal`. `TestIsPandocTerminal` + `TestIsPandocTerminalDivergesFromOCR` pass; live integration test `TestPandocConvertIntegrationTerminalOnError` drives a real corrupt-docx pandoc failure through the real binary and asserts `isPandocTerminal(err)==true`. `isOCRTerminal`/`isDocumentTerminal`/`isAudioTerminal`/`isAVTerminal` bytes unchanged (no other classifier touched — confirmed by grep, only `isPandocTerminal`/`pandocFailureCode`/`HandlePandocConvert` are new additions to worker.go). |
| 6 (SC3) | `PandocUniqueTTL` derived FRESH from pandoc's own budget, never copied | VERIFIED | `internal/queue/queue.go:797-798` `PandocUniqueTTL(maxRetry, engineTimeout) = (maxRetry+1)*engineTimeout + pandocBackoffSum(maxRetry) + uniqueTTLSafetyMargin` — own formula, reuses only the shared `uniqueTTLSafetyMargin` const (same pattern every other engine uses). `TestPandocUniqueTTL` (scales with engineTimeout) and `TestClientPandocUniqueTTLDiffersFromOCRAndAV` (proves inequality vs OCR/AV/Audio under identical inputs) both pass. `pandocRetrySchedule` is its own 2-entry, no-jitter schedule (5s, 20s) distinct from OCR's (15s, 1m) — `TestPandocRetrySchedule` asserts `len==2`. |
| 7 (SC3) | `PANDOC_ENGINE_TIMEOUT` is a clearly-flagged PLACEHOLDER (Phase 49 measures it) | VERIFIED | `internal/queue/client.go:108-123` and `cmd/pandoc-worker/main.go:89-100` both explicitly comment the 90s default as a placeholder "superseded by Phase 49's measured/finalized value," and the two independent readers of the same env var are documented as required to stay in sync. |
| 8 (SC4) | Converting to an empty-but-valid output completes the job as `done`, not `failed` | VERIFIED (unit + live E2E) | `internal/worker/worker.go` `process()` (:1757-1842) has no `if output==""` branch anywhere — success path always calls `h.repo.MarkDone` unconditionally after upload+AddOutput succeed. `internal/convert/pandoc_test.go:TestPandocConvertEmptySourceIsDone` (real pandoc, empty html→md) and `internal/worker/worker_test.go:TestPandocEmptyOutputSucceeds_SC4` (fake blank converter through the exact `runConvertWithOptionalProgress` seam) both pass. **Live proof:** POSTed an empty-body `<html><body></body></html>` (source=html,target=md) through the real API+worker stack → job reached `"status":"done"`, `error_code` empty, downloaded output was a 1-byte (effectively empty) file. |
| 9 (Registry disjointness) | `PandocConverter.Pairs()` disjoint from every other engine; guarded by a dedicated test | VERIFIED | `internal/convert/pairs_disjoint_test.go:141 TestPandocPairsDisjointFromOtherEngines` passes. `pandocPairTable` (pandoc.go:45-53) excludes all `→pdf` and `→odt` targets by construction (document/chromium retain those). `convert.go` has zero new dispatch code (only the pre-existing `EnginePandoc` const, added in Phase 47) — `EngineFor` disambiguates purely by registered Pairs. `TestRegistryEngineForPandocRouting` asserts `(md,html)/(docx,md)/(html,md)→pandoc`, `(docx,pdf)→document`, `(html,pdf)→html(chromium)` in one passing run. |
| 10 (PDCP-01) | Requirement PDCP-01 fully satisfied and closed | VERIFIED | `.planning/REQUIREMENTS.md:28` checkbox `[x]` + traceability table row `PDCP-01 | Phase 48 | Complete`. `.planning/ROADMAP.md:169` Phase 48 row `[x]`, all 3 sub-plans `[x]`. |

**Score:** 10/10 derived must-have truths verified (mapping onto all 4 ROADMAP Success Criteria)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/db/migrations/0010_pandoc_engine.sql` | Additive ALTER admitting `'pandoc'` | VERIFIED | Confirmed additive-only; applied cleanly on live populated DB |
| `internal/queue/queue.go` | `TypePandocConvert`, `QueuePandoc`, `NewPandocConvertTask`, `pandocRetrySchedule`, `PandocRetryDelay`, `pandocBackoffSum`, `PandocUniqueTTL`, `AllConvertQueues` incl. `QueuePandoc` | VERIFIED | All present, wired into `RetryDelayFunc`'s switch and `AllConvertQueues()`'s return slice |
| `internal/queue/client.go` | `pandocMaxRetry`/`pandocUniqueTTL` fields, `NewClient` wiring, `EnqueuePandocConvert` | VERIFIED | Present, placeholder-timeout comment present |
| `internal/convert/converters.go` | `PandocConverter{}` registered in `init()` | VERIFIED | Registered last, with disjointness rationale comment |
| `internal/api/api.go` | `EnqueuePandocConvert` on `Enqueuer` interface + `EnginePandoc` upload ceiling | VERIFIED | Both present |
| `internal/api/handlers.go` | `EnginePandoc` opts-dispatch + enqueue-switch cases + latex/md/rst/org content-gate | VERIFIED | All 5 wiring points present (2 content-gate branches, 1 opts case, 1 enqueue case, plus the pre-existing SniffContainer path for docx/epub) |
| `internal/worker/worker.go` | `HandlePandocConvert` + `isPandocTerminal` + `pandocFailureCode` | VERIFIED | All 3 present; `HandlePandocConvert` mirrors `HandleOCRConvert`'s lifecycle exactly, metrics tagged `queue.QueuePandoc` |
| `cmd/pandoc-worker/main.go` | asynq server on `QueuePandoc`, cancel subscriber, configured `PandocConverter` re-register, placeholder timeout, NO OCR OMP/cgroup bits | VERIFIED | Built successfully; zero occurrences of `OMP_THREAD_LIMIT`/`resolveOCRThreadLimit`/`OCR_MAX_PAGES`/`buildOCRConverter`; cancel subscriber present; live-ran it successfully against real Redis/Postgres/MinIO |
| `internal/reconciler/reconciler.go` | `EnginePandoc` routing case + interface method | VERIFIED | Present at both the interface and switch-case sites |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `internal/queue/client.go NewClient` | `PandocUniqueTTL(pandocMaxRetry, pandocEngineTimeout)` | fresh derivation | WIRED | Confirmed by reading source + `TestClientPandocUniqueTTLDiffersFromOCRAndAV` |
| `internal/queue/queue.go RetryDelayFunc` | `PandocRetryDelay` | `case TypePandocConvert` | WIRED | Confirmed by source read + `TestRetryDelayFuncRoutesPandocConvert` |
| `internal/api/handlers.go handleCreateJob enqueue-switch` | `s.queue.EnqueuePandocConvert` | `case convert.EnginePandoc` | WIRED | Confirmed live: real md→html POST returned 202 and reached `done` |
| `internal/api/handlers.go detected==''` branch | `convert.LooksLikeLaTeX` / `convert.LooksLikeText` | markup-family content-gate dispatch | WIRED | Source confirmed; `TestCreateJob_PandocBinaryContentDeclaredAsMarkdownRejected` proves fail-closed 422 |
| `internal/convert/converters.go init()` | `convert.Default.EngineFor(detected,target)` | `PandocConverter` registration (disjoint pairs) | WIRED | `TestRegistryEngineForPandocRouting` proves zero new dispatch code needed |
| `cmd/pandoc-worker/main.go mux` | `worker.HandlePandocConvert` | `mux.HandleFunc(queue.TypePandocConvert, ...)` | WIRED | Confirmed by source read + live run: a real enqueued job was consumed and completed |
| `internal/reconciler/reconciler.go recover switch` | `EnqueuePandocConvert` | `case convert.EnginePandoc` | WIRED | `TestSweepRoutesPandocJobsToPandocQueue` passes |
| `internal/worker/worker.go HandlePandocConvert` | `isPandocTerminal` | timeout+engine terminal, D-01 | WIRED | Source confirmed; live corrupt-docx integration test proves classification |

### Data-Flow Trace (Level 4)

Not applicable in the UI-rendering sense (this is a backend Go service, no frontend component rendering state). The equivalent trace here — API upload → S3 write → Redis enqueue → worker consume → S3 download → pandoc subprocess → S3 upload → Postgres status write → API read — was independently traced end-to-end via a live run (see below), not merely inferred from code reading.

### Live E2E Proof (beyond SUMMARY claims)

Built `cmd/pandoc-worker`, `cmd/api`, `cmd/migrate`, `cmd/manage-clients` from the current worktree and ran them against the project's real dev infra (Postgres `:5434`, Redis `:6379`, MinIO `:9100`, pandoc 3.10.1 on PATH):

1. Applied all migrations including `0010_pandoc_engine.sql` cleanly (`migrations applied`).
2. Created a test API client, started `cmd/api` on `127.0.0.1:18099` and `cmd/pandoc-worker` (both healthy: `/healthz` → `{"postgres":"ok","redis":"ok","s3":"ok","status":"ok"}`, worker log shows `🐙 pandoc-worker starting (queue=pandoc)`).
3. **Happy path:** POSTed a real markdown file (source=md, target=html) → `202 {"status":"queued"}`. Polled and observed `"status":"done"` with a working presigned MinIO `download_url`; the downloaded file was genuine pandoc-3.10.1-rendered HTML containing the exact submitted text. `job_events` showed `queued → active → done`; `jobs.engine='pandoc'`.
4. **SC4 empty-output path:** POSTed an empty-body `<html><body></body></html>` (source=html, target=md) → reached `"status":"done"` (not `failed`), `error_code` empty, downloaded output was a 1-byte effectively-empty file — proves the empty-but-valid-output-is-done contract live, not just at the unit-test level.
5. Cleaned up: killed both ad hoc processes, removed the temporary binaries/log files. No repository files were modified; `git status --short` after cleanup shows only the pre-existing unrelated untracked files (`octoconv.code-workspace`, `scripts/fixtures/__pycache__/`).

### Automated Checks

| Check | Result |
|-------|--------|
| `go build ./...` | Clean, no errors |
| `go vet ./...` | Clean, no errors |
| `go build ./cmd/pandoc-worker/...` | Clean |
| `go test ./... -count=1` (DATABASE_URL set) | All packages `ok`, zero failures |
| `go test ./internal/queue/... -run Pandoc -v` (REDIS_ADDR set) | 6/6 pass, **zero skips** (`TestEnqueuePandocConvert` runs and passes with `REDIS_ADDR` set; skips only when the env var is absent, matching the OCR precedent) |
| `go test ./internal/worker/... -run Pandoc -v` | 6/6 pass (incl. 2 real-pandoc-binary integration tests), zero skips (pandoc present on PATH) |
| `go test ./internal/convert/... -run Pandoc -v` | All pandoc-related subtests pass, incl. real-binary matrix (`TestPandocConvertRealMatrix`), zero skips |
| `go test ./internal/reconciler/... -run Pandoc` | 1/1 pass |
| `go test ./internal/jobs/... -run Pandoc` (DATABASE_URL set) | 1/1 pass (live Postgres round-trip) |
| `go test ./internal/api/... -run Pandoc` | 3/3 pass |
| Fence: `git diff --exit-code 51ab6dd..HEAD -- internal/convert/exec.go internal/convert/progress.go internal/api/sse.go` | Exit 0 — byte-for-byte unchanged, independently confirmed |
| `git diff --stat 51ab6dd..HEAD -- go.mod go.sum` | Empty — zero new dependencies |
| Anti-pattern scan (`TBD\|FIXME\|XXX\|TODO\|HACK`) across all 17 phase-modified files | Zero matches |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|--------------|-------------|--------------|--------|----------|
| PDCP-01 | 48-01, 48-02, 48-03 | Pandoc wired into full async pipeline end-to-end (migration, queue, worker, retry classification, reconciler, API routing) over unchanged v1.9 infra | SATISFIED | All 10 derived truths above VERIFIED; REQUIREMENTS.md marks it complete; live E2E run confirms the claim independent of SUMMARY narrative |

No orphaned requirements found for Phase 48 in REQUIREMENTS.md.

### Anti-Patterns Found

None. No `TBD`/`FIXME`/`XXX`/`TODO`/`HACK` markers, no placeholder-return stubs, no hardcoded-empty data flowing to output, in any of the 17 files this phase modified.

### Human Verification Required

None. This is a backend Go service with no UI; all success criteria are mechanically verifiable via code inspection, automated tests, and a live end-to-end run against the real dev stack (Postgres/Redis/MinIO/pandoc), all of which were performed above.

### Gaps Summary

No gaps. All 4 ROADMAP success criteria for Phase 48 are verified against the actual codebase (not just SUMMARY claims), reinforced by a live end-to-end run through the real `cmd/api` + `cmd/pandoc-worker` binaries that independently reproduced both the happy path (queued→active→done, real pandoc-rendered output) and the SC4 empty-output-completes-done contract. The exec.go/progress.go/sse.go invariant fence and the zero-new-dependency constraint both hold, independently confirmed via `git diff`. PDCP-01 is genuinely satisfied; Phase 48 is ready to close and Phase 49 (containerization + measured timeout) may proceed.

---

*Verified: 2026-07-30T20:08:53Z*
*Verifier: Claude (gsd-verifier)*
