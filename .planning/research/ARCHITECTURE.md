# Architecture Research: OCR Engine Class Integration (v1.10)

**Domain:** Adding a 6th engine class (classic CPU-OCR: tesseract/PaddleOCR) to an existing
Go async file-conversion service, reusing the v1.9 progress/SSE/webhook infrastructure.
**Researched:** 2026-07-26
**Confidence:** HIGH (every claim below is grounded in the actual current codebase —
`internal/convert/`, `internal/worker/worker.go`, `internal/queue/queue.go`,
`internal/api/handlers.go`, `internal/db/migrations/`, `deploy/chart/octoconv/` — not
training-data guesses about OctoConv specifically). MEDIUM on the exact tesseract/PaddleOCR
CLI invocation details and PDF-rasterize tool choice, which are implementation specifics for
the build phase, not architecture.

## System Overview

```
POST /v1/jobs (file=scan.pdf, target=txt)
        │
        ▼
┌───────────────────────────────────────────────────────────────────────────┐
│ internal/api  (handleCreateJob)                                          │
│  Sniff() -- NEW: matchPDF ("%PDF-") added to sniff.go's signatures table  │
│  EngineFor(detected="pdf", target="txt") -> engine="ocr" (NEW constant)   │
│  ParseOCROpts/ValidateOCRApplicability (NEW, mirrors Audio/AVOpts)        │
│  Postgres-first INSERT (jobs.engine='ocr') -> EnqueueOCRConvert (NEW)     │
└───────────────────────────────────────────────────────────────────────────┘
        │ asynq task {job_id} on NEW "ocr" queue
        ▼
┌───────────────────────────────────────────────────────────────────────────┐
│ cmd/ocr-worker (NEW binary, own container, own KEDA ScaledObject)         │
│  worker.HandleOCRConvert (NEW, mirrors HandleAVConvert's "simple" shape)  │
│    -> process() (UNCHANGED) -> registry.Lookup(pdf,txt) -> OCRConverter   │
│                                                                            │
│  OCRConverter.ConvertWithProgress (NEW, implements ProgressConverter):    │
│   ┌─────────────────────────────────────────────────────────────────┐    │
│   │ 1. if source==pdf: pdfinfo -> page count -> reject if >MAX_PAGES │    │
│   │    (fail-closed BEFORE any rasterize -- mirrors dimensions.go)   │    │
│   │ 2. for page in 1..N (STREAMING, one page in flight at a time):  │    │
│   │      pdftoppm -f page -l page ...  -> page.png (if pdf)         │    │
│   │      tesseract page.png ... -> page.txt / page.hocr             │    │
│   │      append to accumulated output; os.Remove(page.png)          │    │
│   │      sink.Update(pct=page/N*100, eta=pagesRemaining/pagesPerSec) │    │
│   │    (single image, N==1: NO intermediate Update call -- process()│    │
│   │    already writes the unconditional terminal 100 on success,    │    │
│   │    giving the required null->100 for free, zero extra code)     │    │
│   └─────────────────────────────────────────────────────────────────┘    │
└───────────────────────────────────────────────────────────────────────────┘
        │ sink.Update -> progressSink (worker.go, UNCHANGED)
        ▼
┌───────────────────────────────────────────────────────────────────────────┐
│ v1.9 progress infra -- ZERO changes, engine-agnostic by construction      │
│  progressSink.Update -> repo.UpdateProgress (throttled, jobs.progress_pct)│
│  SSE GET /v1/jobs/{id}/events  -- DB-poll, reads progress_pct verbatim    │
│  opt-in progress webhook -- fireDetached, reads progress_pct verbatim    │
└───────────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities (new vs modified)

| Component | Status | Responsibility |
|-----------|--------|-----------------|
| `internal/convert/ocr.go` | **NEW** | `OCRConverter` — implements `Converter` + `ProgressConverter`. Pairs = `{jpg,png,webp,tiff,pdf} × {txt,hocr}` (mirrors `AudioConverter`'s disjoint source×target cross-product, `whisper.go:183-191`). Self-contains the page-count guard (mirrors `AVConverter`'s self-contained duration/resolution guard, `av.go:516-560` — **no** splice into `worker.process()` needed, unlike audio's `enforceAudioGuardBeforeConvert`). |
| `internal/convert/ocrpages.go` | **NEW** | Bounded PDF page-count probe (`pdfinfo` wrapper, or a zero-dependency structural parser mirroring `dimensions.go`'s fail-closed philosophy) + `MAX_OCR_PAGES` enforcement, run BEFORE any rasterize. |
| `internal/convert/ocrprogress.go` | **NEW** | Page-granular EMA/ETA estimator (own small type, e.g. `pageEMAEstimator`, mirroring `progress.go`'s `etaEstimator` shape but keyed on seconds-per-page instead of ffmpeg `speed=`). Does **not** modify `progress.go`'s shared `ProgressUpdate`/`ProgressSink`/`ProgressConverter` interfaces — those already are engine-agnostic (SEED-009's own premise) and stay untouched. |
| `internal/convert/ocropts.go` | **NEW** | `OCROpts{Language string}` — closed allowlist of tesseract 3-letter language codes (`eng`, `rus`, ...), strict-parsed via the shared `checkStrictObject` helper (`opts.go:68`), mirrors `AudioOpts` (`audioopts.go`) byte-for-byte in shape: `ParseOCROpts`, `OCROptsFromMap`, `ValidateOCRApplicability`. |
| `internal/convert/sniff.go` | **MODIFIED** | Add `{"pdf", matchPDF}` to the `signatures` table (`matchPDF` checks the fixed `%PDF-` prefix at offset 0, reusing the existing `pdfMagic` bytes already declared in `libreoffice.go:172` for OUTPUT validation — same magic, new INPUT-detection use). This is a **pure addition to the existing `Sniff()` call** already at the top of `handleCreateJob` — no new branch needed in `handlers.go`'s detection chain (unlike video/audio, which needed dedicated `SniffVideo`/`SniffAudio` calls because their signatures don't fit the fixed 12-byte window). Also add `MIMEType()` cases for `"txt"` (already exists), `"hocr"` (→ `text/html; charset=utf-8`, since hOCR is a valid XHTML profile), `"alto"` (→ `application/xml`, if ALTO is built as a distinct target format). |
| `internal/convert/converters.go` | **MODIFIED** | Register a zero-value `OCRConverter{}` placeholder in `init()` (mirrors `AVConverter{}`'s placeholder registration, `converters.go:27` — `cmd/ocr-worker` re-registers a CONFIGURED instance before serving, same last-write-wins discipline as AV's `D-09`). |
| `internal/convert/convert.go` | **MODIFIED** | Add `EngineOCR = "ocr"` to the engine-class constant block (`convert.go:19-25`) — the single source of truth every routing switch below reads from. |
| `internal/queue/queue.go` | **MODIFIED** | `TypeOCRConvert`, `QueueOCR = convert.EngineOCR`, `NewOCRConvertTask`, `ocrRetrySchedule`/`OCRRetryDelay`, `ocrBackoffSum`/`OCRUniqueTTL` (all mirror the Document/HTML/Audio precedent exactly — no jitter, clamp-index shape), add to `RetryDelayFunc`'s switch and `AllConvertQueues()` (the latter feeds the API's KEDA queue-depth Prometheus collector — **must** be added or the ocr queue is invisible to KEDA, `queue.go:606-608`). |
| `internal/queue/client.go` | **MODIFIED** | `EnqueueOCRConvert` (mirrors `EnqueueAudioConvert`/`EnqueueAVConvert`, `client.go:184-197`). |
| `internal/worker/worker.go` | **MODIFIED** | `HandleOCRConvert` (mirrors `HandleAVConvert`'s "simple" shape — no guard splice, since the page-count guard is self-contained in `OCRConverter.Convert`) + `isOCRTerminal` classifier. Everything else in this file (`process()`, `progressSink`, `runConvertWithOptionalProgress`, SSE/webhook plumbing) is **UNCHANGED** — this is the whole point of the v1.9 engine-agnostic seam. |
| `internal/reconciler/reconciler.go` | **MODIFIED** | `EnqueueOCRConvert` added to the `Enqueuer` interface + a `case convert.EngineOCR` in the recovery-routing switch (`reconciler.go:285-295`, mirrors Audio/AV exactly). |
| `internal/api/handlers.go` | **MODIFIED** | Two switch additions (opts-validation block ~line 469, enqueue block ~line 641): `case convert.EngineOCR` calling `ParseOCROpts`/`ValidateOCRApplicability` and `s.queue.EnqueueOCRConvert`. No change to the Sniff chain itself (see sniff.go above). |
| `internal/db/migrations/0009_ocr_engine.sql` | **NEW** | `ALTER TABLE jobs ... CHECK (engine IN (..., 'ocr'))` — mirrors `0006_audio_engine.sql` exactly. **Note:** the current CHECK (`0006_audio_engine.sql:11`) already lists `'cad'`, `'archive'`, `'probe'` as *reserved-but-unused* placeholders from the original spec — `'ocr'` is **not** among them and must be added fresh. This is the one unavoidable schema migration for this milestone. |
| `cmd/ocr-worker/main.go` | **NEW** | Own binary, mirrors `cmd/av-worker/main.go`'s shape: connect Postgres/Redis/S3, register a configured `OCRConverter{MaxPages: ...}`, resolve `OCR_ENGINE_TIMEOUT`/`OCR_WORKER_CONCURRENCY`/`OCR_MAX_PAGES` env, call `h.SetProgressThrottle(...)` (reusing `PROGRESS_MIN_INTERVAL_SECONDS`/`PROGRESS_MIN_DELTA_PCT` — same env names, same v1.9 infra, no new progress config), optionally `h.SetProgressWebhook(...)` if `WEBHOOK_SIGNING_SECRET` is set, register `queue.TypeOCRConvert -> h.HandleOCRConvert` on its own `asynq.ServeMux`. |
| `Dockerfile.ocr-worker` | **NEW** | Own container (mirrors `Dockerfile.chromium-worker`'s `tini`-as-PID-1 precedent): `debian:bookworm-slim` + `tesseract-ocr` + language packs (`tesseract-ocr-rus` etc.) + `poppler-utils` (`pdftoppm`, `pdfinfo`) — all apt-installable, no compile-from-source needed (unlike av-worker's ffmpeg-from-source). |
| `docker-compose.yml` | **MODIFIED** | New `ocr-worker` service block (mirrors `av-worker`'s block: build/dockerfile, container_name, resource limits, `OCR_*` env, `depends_on`). |
| `.env.example` | **MODIFIED** | New `OCR_ENGINE_TIMEOUT`, `OCR_WORKER_CONCURRENCY`, `OCR_MAX_RETRY`, `OCR_MAX_PAGES` keys. |
| `deploy/chart/octoconv/templates/deployment-ocr-worker.yaml`, `scaledobject-ocr.yaml` | **NEW** | Mirror `deployment-av-worker.yaml`/`scaledobject-av.yaml` byte-for-shape (per-class KEDA autoscaler, `terminationGracePeriodSeconds` > `ShutdownTimeout` > `OCR_ENGINE_TIMEOUT`). |
| `deploy/chart/octoconv/values.yaml`, `configmap.yaml` | **MODIFIED** | New `ocr:` block (image repo, resources, KEDA thresholds) + `OCR_*` configmap keys. |
| `scripts/ocr-timing-measure.sh` | **NEW** | Measured-not-assumed timeout gate (mirrors `scripts/audio-rtf-measure.sh`/`scripts/av-rtf-measure.sh`'s discipline) — but the metric is **seconds-per-page**, not a real-time-factor ratio; naming/methodology needs its own phase, not a blind RTF-script clone. |

## Data Flow

### Request → completion (multi-page scanned PDF)

1. Client `POST /v1/jobs` (`file=scan.pdf`, `target=txt`, optional `opts={"language":"rus"}`).
2. `Sniff()` matches `%PDF-` → `detected="pdf"`. `EngineFor("pdf","txt")` → `"ocr"`.
3. `ParseOCROpts`/`ValidateOCRApplicability` gate the language allowlist (fail-closed 422 on an unlisted code) — same shape as `ValidateAudioApplicability`.
4. Postgres-first: `jobs` row inserted (`engine='ocr'`, `status='queued'`) inside a transaction, **then** `EnqueueOCRConvert` puts `{job_id}` on the `ocr` queue.
5. `cmd/ocr-worker` (scaled from zero by KEDA on queue depth > 0) picks up the task, `MarkActive`, `process()` downloads the input, looks up `OCRConverter` via the unchanged `registry.Lookup`.
6. `OCRConverter.ConvertWithProgress`:
   a. `pdfinfo` (or the bounded structural parser) reads the declared page count — **before touching pdftoppm** — and rejects (terminal) if it exceeds `OCR_MAX_PAGES`.
   b. Loop `page = 1..N`: `pdftoppm -f page -l page -r <dpi> -png in.pdf pagebase` renders **exactly one page** to disk; `tesseract pagebase-N.png outbase txt|hocr` OCRs it; the page image is deleted immediately after OCR completes, before the next iteration starts — bounded working set of one rasterized page at a time, never N pages simultaneously (the "streaming, not all-at-once" requirement).
   c. After each page: `sink.Update(ProgressUpdate{Pct: page*100/N, ETA: pageEMA.etaSeconds(...)})`.
   d. Fragments are concatenated (txt) or wrapped in a single hOCR/ALTO document (structural merge, not a blind concat) into the final `outPath`.
7. On success, `process()`'s existing unconditional `repo.UpdateProgress(ctx, jobID, 100, nil)` fires (unchanged) — this is what gives a single-image job its required `null→100` jump for free, since a 1-page job's loop body never calls `sink.Update` itself.
8. `AddOutput(ordinal=0, ...)` — **unchanged single-output call** (see Output Handling below) — `MarkDone`.
9. SSE subscribers and the opt-in progress webhook have been receiving `progress_pct` ticks throughout step 6c via the completely unmodified v1.9 machinery; the terminal `completed` webhook fires from the existing `HandleWebhookDeliver` path.

### Single image (no PDF stage)

Same flow, but `N=1`, no `pdfinfo`/`pdftoppm` invocation (the guard/rasterize stage is entered only `if source == "pdf"` — an image input goes straight to the tesseract step on the downloaded file itself). No `sink.Update` calls fire; the client sees `progress_pct: null` for the job's duration then `100` on completion — exactly SEED-009's specified behavior.

## Architectural Patterns

### Pattern 1: Self-contained engine guard (mirrors `AVConverter`, not `AudioConverter`)

**What:** The page-count ceiling is enforced entirely inside `OCRConverter.Convert`/`ConvertWithProgress`, not spliced into the shared `worker.process()` the way `enforceAudioGuardBeforeConvert` splices audio's duration guard.
**When to use:** Whenever the guard's precondition (page count) is itself something only the engine can cheaply determine from the downloaded input, and no other engine needs to share the check.
**Why:** `HandleAVConvert`'s doc comment is explicit that this is the simpler, preferred shape when a guard doesn't need to be shared — it keeps `HandleOCRConvert` a byte-for-byte structural clone of `HandleAVConvert` (mark active → `process()` → `isOCRTerminal` branch → done), with zero new parameters threaded through `NewHandler`/`process()`.

### Pattern 2: Streaming page-by-page rasterize (bounded working set)

**What:** Render exactly one PDF page to disk, OCR it, delete the rendered page, then move to the next page — never `pdftoppm in.pdf outbase` with no `-f`/`-l` bounds (which renders every page in one shot into the container's temp filesystem).
**When to use:** Any PDF-derived multi-page pipeline where page count is attacker-influenced (a malicious/absurd multi-thousand-page PDF is exactly the "decompression bomb" class this project already treats as a first-class threat, `dimensions.go`).
**Trade-off:** N subprocess invocations instead of 1 (mirrors `whisper.go`'s two-stage-per-attempt pattern, and `AudioConverter`'s own precedent of running a distinct `runCommand` per logical stage) — acceptable, since each invocation is cheap relative to the OCR step itself, and it is what makes the page-granular progress signal (`sink.Update` after each page) possible in the first place.

### Pattern 3: Zero-touch progress-consumer reuse

**What:** `progressSink`, `runConvertWithOptionalProgress`, the SSE handler, and the progress webhook are ALL already engine-agnostic (they operate purely on `ProgressUpdate{Pct, ETA}` and `jobs.progress_pct`/`eta_seconds` columns) — OCR needs to implement the `ProgressConverter` interface and call `sink.Update`, nothing more.
**When to use:** Every future engine class that wants progress reporting should follow this exact seam (`ProgressConverter`, `NewHandler`+`SetProgressThrottle`) rather than inventing a parallel mechanism.
**Anti-pattern avoided:** Do NOT reuse `progress.go`'s `progressBlockParser`/`buildProgressTick`/`etaEstimator` — those are ffmpeg `-progress`-stream-specific (they parse `out_time_us`/`speed=` key-value blocks). OCR has no continuous engine stream to parse; it drives `sink.Update` directly from its own page loop. A small OCR-specific EMA (own file, `ocrprogress.go`) is the correct amount of new code — not a refactor of `progress.go`'s shared interfaces.

### Pattern 4: Closed-allowlist opts (mirrors `AudioOpts`)

**What:** `OCROpts{Language string}` validated against a hardcoded map of accepted tesseract language codes, strict-parsed via the shared `checkStrictObject`, and passed to `tesseract`'s `-l` argv element only as a discrete slice entry (never a shell string — `runCommand` never invokes a shell, `exec.go`).
**When to use:** Any per-job engine option that must reach a subprocess argv.
**Why not open-ended:** Same rationale as `AudioOpts`'s doc comment — an unvalidated string reaching a future path-shaped opt (e.g. a model/traineddata selector) is a path-traversal risk; closing the allowlist now avoids that class of bug entirely.

## Anti-Patterns

### Anti-Pattern 1: Rasterizing all pages before OCR starts

**What people might do:** `pdftoppm in.pdf outbase` (no `-f`/`-l`), producing N page images up front, then loop over the files.
**Why it's wrong:** Turns an attacker-controlled page count directly into unbounded disk/memory pressure inside the worker container — exactly the decompression-bomb class this project already treats as a first-class threat elsewhere (`dimensions.go`, `MAX_IMAGE_PIXELS`).
**Do this instead:** Bound the page count via `pdfinfo` FIRST (reject before rendering anything), then render/OCR/delete one page at a time.

### Anti-Pattern 2: Touching `progress.go`'s shared types to "generalize" them for OCR

**What people might do:** Add an `OCRProgress`-flavored branch inside `progressBlockParser`/`buildProgressTick`, or change `ProgressUpdate`/`ProgressSink`'s shape to accommodate a "page" concept.
**Why it's wrong:** Those types are already engine-agnostic (`Pct`, `ETA` — nothing ffmpeg-specific leaks through the interface boundary); the ffmpeg-specific parsing machinery in `progress.go` is a private implementation detail of `AVConverter`, not part of the public seam. Touching it risks the exact kind of drift SEED-010's dynamic-registry discussion explicitly wants to avoid at this stage.
**Do this instead:** A small, OCR-scoped file (`ocrprogress.go`) with its own page-EMA type, calling the SAME `ProgressUpdate`/`sink.Update(...)` contract every other engine uses.

### Anti-Pattern 3: Building the SEED-010 dynamic registry as part of this milestone

**What people might do:** Since OCR is "yet another engine class," use it as the excuse to finally generalize `converters.go`'s `init()` into a data-driven catalog.
**Why it's wrong:** SEED-010 is explicitly dormant/deferred — its own trigger condition ("3+ new engine classes queued" or "adding a worker must require zero API changes") is not met by this milestone alone, and doing both at once conflates a well-understood, mechanically-repeatable pattern (5 prior engine classes shipped this exact way) with a genuinely large platform change.
**Do this instead:** Wire OCR exactly like image/document/html/audio/av — compile-time `Registry.Register` in `converters.go`, hardcoded `EngineOCR` constant, a fresh CHECK-constraint migration. Leave SEED-010 dormant.

### Anti-Pattern 4: Multi-output-per-job (txt AND hOCR from one job)

**What people might do:** Interpret "output txt + hOCR/ALTO" as one OCR job producing both files simultaneously, requiring `process()`'s hardcoded single `AddOutput(ordinal=0, ...)` call to become a loop over multiple ordinals.
**Why it's wrong:** Every existing engine (including audio's 4-target `Pairs()`) is a strict one-job-one-target model; extending `process()`'s output-recording step to multi-output would be a structural change touching the shared code path for all 5 existing engines, for a capability nothing else needs yet.
**Do this instead (recommended, see Output Handling below):** Model txt and hocr/alto as **separate target formats** in `OCRConverter.Pairs()` (`{pdf,jpg,png,...} × {txt,hocr}`), exactly like audio's `{txt,srt,vtt,json}` targets — the client picks one per job via the existing `target` field. `process()` needs zero changes. A true multi-output-per-job capability is explicitly deferred, not built here.

## Output Handling

**Recommended:** `OCRConverter.Pairs()` advertises `{jpg, png, webp, tiff, pdf} × {txt, hocr}` (and `alto` if built as a third target), mirroring `AudioConverter.Pairs()`'s disjoint source×target cross-product exactly (`whisper.go:183-191`). A client wanting both a plain-text transcript and word-coordinate hOCR submits **two jobs** against the same input (or, later, uses presets to make that a one-line client-side convenience — no server change needed for that). `process()`'s existing single `AddOutput(ordinal=0, ObjectKey: storage.OutputKey(job.ID, 0, outName), ...)` call is reused byte-for-byte unchanged; `storage.OutputKey`'s `results/{job_id}/{ordinal}-{filename}` layout already supports the (currently unused) `ordinal` dimension for a future multi-output extension, so nothing about the storage-key scheme needs to change even if multi-output is added later.

`MIMEType()` (`sniff.go`) needs two new cases: `"hocr" → "text/html; charset=utf-8"` (hOCR is a valid XHTML profile per the hOCR 1.2 spec) and, if ALTO is built as its own target rather than folded into hocr, `"alto" → "application/xml"`.

## Integration Points Summary

### External Tools

| Tool | Integration Pattern | Notes |
|------|---------------------|-------|
| `tesseract` (CLI) | `runCommand(ctx, "tesseract", pageImg, outBase, "-l", lang, targetFlag...)` — hardened exec, process-group kill on timeout, identical to every other engine's shell-out (`exec.go`, unchanged). | Apt-installable (`tesseract-ocr` + `tesseract-ocr-<lang>` packages) on `debian:bookworm-slim`; no from-source build needed (unlike av-worker's ffmpeg). PaddleOCR, if chosen instead during the phase-level eval-gate, would need a Python runtime in the container — a materially heavier `Dockerfile.ocr-worker` than the tesseract path; this asymmetry itself is a strong argument for tesseract as the default absent a demonstrated quality gap. |
| `pdftoppm` / `pdfinfo` (poppler-utils) | Same `runCommand` hardening; `pdfinfo` read-only probes the declared page count, `pdftoppm -f N -l N` renders exactly one page per invocation. | Apt-installable (`poppler-utils`). Verify at build time whether libvips' own PDF loader (already a dependency via `libvips-tools` in the image worker, though NOT currently in the ocr-worker's own container) could substitute for `pdftoppm` — this is a real open question for the build phase, not resolved here; poppler-utils is the safer default since it doesn't depend on unverified libvips build flags. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| API ↔ `convert.Registry` | `EngineFor(detected, target)` / `Lookup` (unchanged) | OCR slots in as a 6th registered engine; no interface change. |
| Worker ↔ `convert.ProgressConverter` | Type-assertion seam in `runConvertWithOptionalProgress` (unchanged) | OCR implements the interface; the seam itself needs zero edits. |
| Worker ↔ Postgres progress columns | `progressRepo.UpdateProgress`/`SetSourceDuration` (unchanged) | `SetSourceDuration` is a poor semantic fit for OCR (no "duration" concept) — **do not call it**; leave `source_duration_seconds` NULL for OCR jobs, relying purely on `progress_pct`/`eta_seconds`. Confirm this is acceptable to the job-listing/SSE consumers (it already is — neither reads `source_duration_seconds` directly, GET /v1/jobs/{id} exposes only `progress_pct`/`eta_seconds`). |
| Reconciler ↔ engine routing | `switch j.Engine` (mirrors Audio/AV) | A stuck/hung `queued`/`active` OCR job recovers through the exact same sweep, no new mechanism. |
| KEDA ↔ queue depth | `AllConvertQueues()` (mirrors Audio/AV) | Forgetting to add `QueueOCR` here silently breaks scale-from-zero with no compile error — the existing `TestAllConvertQueuesCoversEveryEngine` completeness guard (`queue_test.go`) should be extended to cover OCR, not just left passing by accident. |

## Suggested Build Order (continuing from Phase 43)

**Phase 43 — OCR engine foundation (standalone, unregistered).**
Mirrors the Audio/AV precedent of building the converter as a fully-tested, standalone unit
before wiring it into the async pipeline (`AudioConverter` in Phase 30, `AVConverter` in
Phase 34). Scope: engine-choice eval-gate (tesseract vs PaddleOCR, resolved against real
scanned-document test fixtures, not assumed); `OCRConverter` implementing `Converter` +
`ProgressConverter`; bounded PDF page-count guard + streaming per-page rasterize
(`pdftoppm`/`pdfinfo`); `OCROpts` closed language allowlist; PDF magic-byte sniffer addition
to `sniff.go`; page-granular EMA/ETA estimator. NOT yet registered into `convert.Default`.
*Addresses:* the "classic CPU-OCR" and "page-granular progress" requirements.
*Avoids:* the rasterize-all-pages-at-once memory-bomb pitfall (built in from day one, not
retrofitted).

**Phase 44 — Async pipeline wiring.**
`0009_ocr_engine.sql` migration; register `OCRConverter` in `converters.go`; `ocr` queue +
`TypeOCRConvert` + retry schedule + `OCRUniqueTTL`; `cmd/ocr-worker` binary;
`HandleOCRConvert` + `isOCRTerminal` in `worker.go`; reconciler routing; API engine-routing
switch (opts validation + enqueue); `queue.Client.EnqueueOCRConvert`; `MIMEType` additions.
Live E2E: single image → `null`→100, multi-page scanned PDF → real page-granular ramp via
SSE, both over the existing (unmodified) v1.9 machinery.
*Addresses:* full async-contour requirement, page-granular progress reuse.
*Avoids:* the "silently break KEDA" pitfall (add OCR to `AllConvertQueues()` in the SAME
change that adds the queue, per the existing completeness-test discipline).

**Phase 45 — Containerization + measured timeout gate.**
`Dockerfile.ocr-worker` (tesseract + poppler-utils + language packs, tini as PID 1);
`scripts/ocr-timing-measure.sh` measuring real seconds-per-page across a representative
corpus (clean scan vs dense multi-column text vs handwriting-adjacent noisy scan) to derive
`OCR_ENGINE_TIMEOUT` and the `OCR_MAX_PAGES` ceiling as measured, not assumed values (mirrors
the audio/AV RTF-gate discipline, adapted from a time-ratio metric to a per-unit-of-work
metric); docker-compose wiring.
*Addresses:* "RTF-measured timeout" requirement (page-based analog).
*Avoids:* the "copied timeout constant" pitfall the project's own Key Decisions table
explicitly calls out as a recurring risk for new engine classes.

**Phase 46 — KEDA / Helm.**
`deployment-ocr-worker.yaml` + `scaledobject-ocr.yaml` + `values.yaml`/`configmap.yaml`
wiring; live scale-from-zero proof (mirrors Phase 33's audio KEDA proof: 0→1 on queue depth,
scale back to 0 after drain).
*Addresses:* full engine-class contour requirement (KEDA scale-from-zero).

**Dependency ordering rationale:** 43 must precede 44 (nothing to register without a working
converter); 44 must precede 45 (can't measure a real timeout without the full pipeline
producing real job outcomes to time); 45 must precede 46 (KEDA's `scaleDownStabilizationSeconds`
and pod `terminationGracePeriodSeconds` are derived from the measured `OCR_ENGINE_TIMEOUT`,
same dependency chain AV's Phase 36→38 followed). This is the same 4-phase shape as v1.7
(audio, phases 30/31/32/33) and v1.8 (av, phases 34/35/36/37-38) — the pattern is proven
twice already; no reason to deviate structurally for OCR.

## Sources

- `internal/convert/convert.go`, `internal/convert/progress.go`, `internal/convert/whisper.go`,
  `internal/convert/av.go`, `internal/convert/avdiskguard.go`, `internal/convert/audioopts.go`,
  `internal/convert/opts.go`, `internal/convert/sniff.go`, `internal/convert/avsniff.go`,
  `internal/convert/dimensions.go`, `internal/convert/converters.go`, `internal/convert/libreoffice.go`
  (`pdfMagic`), `internal/convert/libvips.go` — current engine/converter/sniff/progress
  implementations (HIGH confidence, read directly).
- `internal/worker/worker.go` — full `Handle*Convert` family, `process()`, `progressSink`,
  `runConvertWithOptionalProgress`, terminal classifiers (HIGH confidence, read directly).
- `internal/queue/queue.go`, `internal/queue/client.go` — per-engine queue/task/retry/TTL
  wiring (HIGH confidence, read directly).
- `internal/api/handlers.go` — sniff chain, `EngineFor` routing, opts-validation switch,
  enqueue switch (HIGH confidence, read directly).
- `internal/reconciler/reconciler.go` — recovery-routing switch (HIGH confidence).
- `internal/storage/keys.go` — `InputKey`/`OutputKey` ordinal-aware object-key layout (HIGH
  confidence).
- `internal/db/migrations/0006_audio_engine.sql`, `0007_job_progress.sql` — CHECK-constraint
  migration pattern and the already-additive progress schema (HIGH confidence).
- `cmd/av-worker/main.go` — worker-binary wiring pattern, env-config discipline,
  `SetProgressThrottle`/`SetProgressWebhook` post-construction setters (HIGH confidence).
- `deploy/chart/octoconv/templates/`, `deploy/chart/octoconv/values.yaml`, `docker-compose.yml`
  — per-engine-class container/KEDA/compose wiring precedent (HIGH confidence, listed
  directly).
- `.planning/seeds/SEED-009.md` (OCR seed, progress-reuse breadcrumbs), `.planning/seeds/SEED-010.md`
  (dynamic registry — confirmed deferred, informs Anti-Pattern 3) — project-internal planning
  documents (HIGH confidence, authoritative for this project's own decisions).
- `.planning/PROJECT.md` — milestone goal/locked scope, Key Decisions precedent for
  measured-not-assumed timeouts (HIGH confidence).
- Tesseract/PaddleOCR CLI specifics (exact flags, language-pack package names, hOCR/ALTO
  output flag support) are **not** verified against upstream docs in this research pass —
  MEDIUM confidence, flagged for the Phase 43 eval-gate to confirm directly against the
  chosen tool's actual CLI (training-data knowledge of tesseract's `-l`/`hocr`/`txt` config
  files and PaddleOCR's Python API is plausible but unverified here).

---
*Architecture research for: OctoConv v1.10 OCR engine-class integration*
*Researched: 2026-07-26*
