# Project Research Summary

**Project:** OctoConv — v1.10 OCR Engine (6th engine class)
**Domain:** Classic CPU-OCR (offline, CPU-only text extraction) integrated into an existing hardened Go async file-conversion service
**Researched:** 2026-07-26
**Confidence:** HIGH

## Executive Summary

v1.10 adds OCR as OctoConv's 6th engine class — classic CPU-OCR (not a VLM/GPU path), offline, converting images and scanned/image-only PDFs into `txt` + `hOCR`/`ALTO`, with page-granular progress reusing the v1.9 progress/SSE/webhook infrastructure unchanged. Every research track converges on the same shape: **Tesseract 5.5.3** invoked as a plain CLI subprocess through the existing hardened `runCommand` harness, **Poppler's `pdftoppm`/`pdfinfo`** for a streaming, one-page-at-a-time PDF rasterize pre-stage, and a mechanically-repeated engine-class wiring pattern (own queue, own worker binary, own KEDA ScaledObject, own migration) identical to the five engine classes already shipped (image, document, html, audio, av). This is a **zero-new-Go-dependency** milestone — OCR is "just another CLI engine" from the Go codebase's point of view, no cgo, no Python runtime.

The recommended approach is deliberately conservative on two fronts that research flagged as tempting over-builds: (1) preprocessing — rely on Tesseract's own bundled Leptonica (which already does Otsu-style binarization) plus a fixed ~300 DPI raster on day one, and treat deskew/denoise as a measured, evidence-gated follow-up rather than a blind day-one addition (see Reconciled Position below); (2) scope discipline — layout/markdown reconstruction, handwriting recognition, and searchable-PDF overlay output are explicit anti-features for this engine family, and a born-digital PDF (one that already has an extractable text layer) is fail-closed rejected (422) in v1 rather than silently OCR'd or passed through.

The dominant risk is not "will Tesseract work" (it will, on the locked clean-scan scope) but the *class* of pitfalls that only show up under adversarial or large-scale real input and are invisible in a demo: unbounded multi-page rasterization (a memory bomb distinct in shape from every prior engine's guards), a copied-not-measured `OCR_ENGINE_TIMEOUT`, and treating "OCR completed" as equivalent to "OCR succeeded correctly" when the `Converter` interface's binary success/error contract has no room for a quality spectrum. All three are addressed structurally in the suggested phase order below, matching the same measure-don't-assume discipline OctoConv already applied to `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT`.

## Key Findings

### Recommended Stack

Tesseract 5.5.3 wins the locked eval-gate (offline, CPU-only, clean scans, deterministic, needs hOCR/ALTO) decisively over PaddleOCR: faster on CPU for clean documents, native mature hOCR (since 3.x)/ALTO (since 4.1.0) output with zero extra code, and a pure CLI shape that fits `runCommand` byte-for-byte like every other engine. PaddleOCR wins only on messy/curved/handwritten input with a GPU available — explicitly out of scope (SEED-009's "(b) self-hosted VLM" path, a different engine class). Tesseract still gets a formal Phase 43 eval-gate against real scanned fixtures before being locked in, per the project's own precedent (Audio/AV both validated their engine choice against real samples before committing).

**Core technologies:**
- **Tesseract 5.5.3** (built from source, tag-pinned, mirrors the ffmpeg/whisper.cpp source-pin precedent): OCR engine, CLI-invoked — zero new Go dependency, fits `runCommand` exactly
- **Poppler `pdftoppm` + `pdfinfo`** (poppler-utils, apt-installable, GPLv2/v3 not AGPL): PDF→image rasterize pre-stage + cheap page-count probe; the narrow "render one page at N DPI" job, not full PDF editing — MuPDF (AGPLv3) and Ghostscript (AGPL/heavier) are documented fallbacks only
- **`tessdata_fast` language models**, SHA-pinned, small closed allowlist (mirrors `AudioOpts`/whisper model bake-in precedent): LSTM-only "fast" variant is the day-one default; `tessdata_best` is an evidence-gated upgrade, not a default

### Expected Features

OctoConv's OCR engine is a pixel-in/text-out pipeline with no document-structure reasoning — this shapes what's in vs. out of scope sharply.

**Must have (table stakes):**
- Single image (jpg/png/tiff/webp) → txt, and scanned/image-only PDF → per-page txt (concatenated), via a new rasterize pre-stage
- hOCR output (word/line bounding boxes) — native, effectively free once txt output exists
- Language/script selection via a validated closed-allowlist `opts` (mirrors `AudioOpts` exactly, including injection-testing the `-l` argv construction)
- Page-granular progress + ETA, reusing v1.9's `Repo.UpdateProgress`/SSE/webhook infra unchanged — the only new work is the progress *source* (a page loop), not the sink
- Fail-closed magic-byte validation, extended with a PDF-specific born-digital-text-layer check
- Fixed ~300 DPI rasterization + Tesseract's built-in binarization (see Reconciled Position)

**Should have (competitive, add after validation):**
- ALTO output alongside hOCR — nearly free, same invocation family
- Per-word confidence surfaced in the structured API response (parsed from TSV/hOCR `x_wconf`)
- Automatic orientation/script detection (OSD) pre-pass for rotated scans/photos
- Mixed-language per-job selection (`eng+rus`-style)

**Defer / anti-features (v2+ or explicitly excluded):**
- Layout/semantic understanding + layout→markdown reconstruction — a VLM-class capability, not classic OCR; would require a different, heavier engine class (SEED-009 option b/c)
- Handwriting recognition — Tesseract's LSTM models are weak here; a fail-open quality risk this project's fail-closed philosophy rejects
- Cloud OCR/VLM API fallback — violates the hard offline-engines constraint
- Searchable-PDF overlay output — a third output format beyond locked scope
- Born-digital PDF detect-and-passthrough (option 2) — legitimate future differentiator, deliberately deferred behind its own opts/output-format design; v1 ships fail-closed reject only

### Architecture Approach

OCR slots into the existing 5-engine-class pattern with **zero changes** to the v1.9 progress/SSE/webhook machinery, the `Converter`/`Registry` interfaces, or `worker.process()`'s single-output model — it is wired exactly like Audio/AV: new `EngineOCR` constant, new `ocr` queue + task type + retry schedule, new `cmd/ocr-worker` binary with its own KEDA ScaledObject, and one schema migration (`0009_ocr_engine.sql`) adding `'ocr'` to the `jobs.engine` CHECK constraint. The one genuinely new architectural piece is the page-loop: `pdfinfo` reads a page count and enforces `OCR_MAX_PAGES` *before* any rendering, then the worker loops one page at a time (`pdftoppm -f N -l N` → `tesseract` → delete the page image → `sink.Update`), giving both a bounded memory footprint and the page-granular progress signal for free. `txt` and `hocr`/`alto` are modeled as **separate target formats** (like Audio's `{txt,srt,vtt,json}`), not a multi-output-per-job change — a client wanting both submits two jobs, keeping `process()`'s single `AddOutput(ordinal=0, ...)` call untouched.

**Major components:**
1. `internal/convert/ocr.go` (`OCRConverter`, implements `Converter` + `ProgressConverter`) — self-contained page-count guard, mirrors `AVConverter`'s shape (no splice into `worker.process()`)
2. `internal/convert/ocrpages.go` / `ocrprogress.go` / `ocropts.go` — bounded page-count probe, page-EMA/ETA estimator (own file, does not touch `progress.go`'s ffmpeg-specific shared types), closed-allowlist `OCROpts{Language}`
3. `cmd/ocr-worker` (new binary) + `Dockerfile.ocr-worker` (tesseract + poppler-utils, apt-installable, no from-source ffmpeg-style build needed) — own queue, own KEDA ScaledObject, own `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES`/`OCR_WORKER_CONCURRENCY` env
4. `internal/api/handlers.go`, `internal/queue/queue.go`/`client.go`, `internal/reconciler/reconciler.go` — mechanical per-engine-class switch additions (opts validation, enqueue, retry routing, KEDA queue-depth visibility via `AllConvertQueues()`) — same shape as every prior engine, no new abstraction

### Critical Pitfalls

1. **Multi-page memory bomb** — rasterizing an entire PDF before OCR-ing any page (e.g., `pdftoppm` with no `-f`/`-l` bound) turns an attacker-controlled page count directly into unbounded disk/memory pressure; a 500-page PDF at 300 DPI is ~12GB+ held all-at-once. Avoid by reading the page count via `pdfinfo` and rejecting over `OCR_MAX_PAGES` *before* any rendering, then streaming one page in flight at a time.
2. **Silent accuracy collapse with no error signal** — unlike every other engine (transcode either works or fails loudly), OCR can return `done` with garbled/low-quality text and the `Converter` interface's binary success/error contract has no room for a quality spectrum. Avoid via fixed ~300 DPI rasterization + Tesseract's own internal binarization on day one (see Reconciled Position), explicit PSM/OEM pinning validated against a representative sample set, and treating empty-but-valid OCR output as a successful `done` result (never an error).
3. **RTF assumed instead of measured** — copying `OCR_ENGINE_TIMEOUT` from `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT` is explicitly called out by the project's own Key Decisions precedent as a direct path to a dead timeout or spurious failures; OCR pages/sec varies wildly by DPI, language, and page complexity. Avoid via a dedicated `scripts/ocr-timing-measure.sh` (seconds-per-page, not an RTF ratio) measured against representative documents under production-equivalent CPU/memory limits, with `OCR_MAX_PAGES` as the NO-GO lever (not an inflated timeout).
4. **Untrusted input into two new unhardened processes** — both the rasterizer and Tesseract/Leptonica have a documented CVE history on malformed input (a maintainer issue literally titled "Leptonica has no regard for security"), and OCR adds a second untrusted parser (rasterizer) in front of a first (Tesseract) at the same trust boundary. Avoid by routing both through the existing hardened `runCommand` (process-group kill + timeout), unchanged `USER nobody` + resource-capped container posture, and explicit version pinning.
5. **Born-digital PDF mishandling and hOCR/ALTO XML escaping bugs** — a PDF with an existing text layer wastefully (and sometimes worse) gets re-OCR'd if no pre-flight text-layer check exists; and hand-concatenating per-page hOCR fragments (rather than a real XML/HTML encoder) breaks on OCR'd text containing `&`/`<`/`>`. Avoid via a pre-flight text-extractability check gating a fail-closed 422 in v1, and `encoding/xml`/`html/template`-based merging with a round-trip escaping test.

## Reconciled Position: Preprocessing Scope

**Tension:** FEATURES.md calls deskew/binarize/DPI-normalize "table stakes"; STACK.md notes Tesseract's bundled Leptonica already runs Otsu-style binarization internally, so no separate library is needed day one (deskew/`unpaper` explicitly evidence-gated); PITFALLS.md warns Tesseract has **no** built-in deskew and raw scans degrade silently with zero error signal.

**Resolved position (binding for roadmap/requirements):** Ship **fixed ~300 DPI rasterization** (computed from the PDF page box size, never trusting a source PDF's declared/odd DPI) **+ Tesseract's own internal Leptonica-based binarization** as the day-one preprocessing floor — this is non-negotiable table stakes, costs zero new dependencies, and is the single biggest accuracy lever available at effectively no runtime cost. **Deskew and denoise (`unpaper`, an explicit `pixDeskew` pass, or any additional preprocessing library) are NOT added blindly on day one** — they are an evidence-gated follow-up, added only if measured accuracy on real representative scans (during the same phase that validates PSM/OEM defaults and the RTF/timeout gate) shows a meaningful accuracy loss that fixed-DPI-rasterize + internal binarization doesn't cover. This matches the project's established zero-new-dependency and measure-don't-assume bias (identical discipline to how `AUDIO_ENGINE_TIMEOUT` and PDF/A validation depth were gated). Requirements definition should lock "fixed-DPI + internal binarization" as in-scope P1, and record deskew/denoise explicitly as a flagged, evidence-gated P2 decision — not silently deferred and not silently assumed-in.

## Implications for Roadmap

Suggested phase structure (continuing numbering from Phase 42, starting at Phase 43 — this is the fully-converged build order from ARCHITECTURE.md, cross-validated against PITFALLS.md's phase mapping):

### Phase 43: OCR Engine Foundation (standalone, unregistered)
**Rationale:** Mirrors the Audio/AV precedent (Phase 30, Phase 34) of building the converter as a fully-tested standalone unit before wiring it into the async pipeline — nothing to register without a working converter.
**Delivers:** Engine-choice eval-gate (Tesseract vs. PaddleOCR, resolved against real scanned-document fixtures, not assumed); `OCRConverter` implementing `Converter` + `ProgressConverter`; bounded PDF page-count guard + streaming per-page rasterize (`pdftoppm`/`pdfinfo`); `OCROpts` closed language allowlist; PDF magic-byte sniffer addition; born-digital PDF pre-flight text-layer detection with explicit fail-closed (422) behavior; fixed ~300 DPI rasterize + Tesseract's internal binarization; PSM/OEM explicitly pinned and validated; page-granular EMA/ETA estimator; hOCR/ALTO output via a real XML encoder (not string concatenation), with an escaping round-trip test.
**Addresses:** Single image/PDF → txt+hOCR (FEATURES table stakes), page-granular progress source, born-digital PDF decision.
**Avoids:** Pitfalls 1 (memory bomb), 2 (silent accuracy collapse), 3 (wrong PSM/OEM), 5 (XML escaping), 6 (born-digital PDF), 9 (empty-output-as-failure) — all flagged as "must be architectural from line one, not retrofitted."

### Phase 44: Async Pipeline Wiring
**Rationale:** Can't measure a real timeout (Phase 45) without the full pipeline producing real job outcomes to time.
**Delivers:** `0009_ocr_engine.sql` migration; `OCRConverter` registered in `converters.go`; `ocr` queue + `TypeOCRConvert` + retry schedule + `OCRUniqueTTL`; `cmd/ocr-worker` binary; `HandleOCRConvert` + `isOCRTerminal`; reconciler routing; API opts-validation/enqueue switch additions; `MIMEType` additions for hOCR/ALTO; live E2E proof (single image null→100, multi-page PDF real page-granular ramp via SSE) over the unmodified v1.9 machinery.
**Uses:** Tesseract 5.5.3, `pdftoppm`/`pdfinfo`, existing `runCommand` hardened-exec harness (Pitfall 8's mitigation).
**Implements:** Engine-class control-plane parity (own queue/worker/retry/reconciler routing).

### Phase 45: Containerization + Measured Timeout Gate
**Rationale:** 45 must precede 46 — KEDA's `scaleDownStabilizationSeconds`/`terminationGracePeriodSeconds` derive from the measured `OCR_ENGINE_TIMEOUT`, same dependency chain AV's Phase 36→38 followed.
**Delivers:** `Dockerfile.ocr-worker` (Tesseract + poppler-utils + a short, SHA-pinned `tessdata_fast` language allowlist — explicitly NOT the full upstream tessdata set); `scripts/ocr-timing-measure.sh` measuring real seconds-per-page (not an RTF ratio) across representative documents (clean scan, dense multi-column, noisy/skewed) under production-equivalent CPU/memory limits, per-language if the allowlist spans fast/slow scripts; `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` derived as measured values with documented p95/N/cpu-mem conditions (own Key Decision entry, never copied from Audio/AV); docker-compose wiring.
**Addresses:** RTF-measured timeout requirement (page-based analog).
**Avoids:** Pitfall 4 (language-pack bloat/image-size), Pitfall 7 (assumed-not-measured timeout).

### Phase 46: KEDA / Helm
**Rationale:** Final phase — depends on Phase 45's measured timeout values.
**Delivers:** `deployment-ocr-worker.yaml` + `scaledobject-ocr.yaml` + `values.yaml`/`configmap.yaml` wiring; live scale-from-zero proof (mirrors Phase 33's audio KEDA proof).
**Addresses:** Full engine-class contour requirement (KEDA scale-from-zero), completing parity with all 5 prior engine classes.

### Phase Ordering Rationale

- **43→44→45→46 is a strict dependency chain**, not an arbitrary grouping: nothing to register (44) without a working converter (43); can't measure a real timeout (45) without the full pipeline producing real outcomes to time (44); KEDA's shutdown timing (46) derives from the measured timeout (45).
- This is the identical 4-phase shape as v1.7 (audio, phases 30-33) and v1.8 (av, phases 34-38) — a pattern proven twice already; no reason to deviate structurally for OCR.
- Grouping preprocessing/PSM/OEM/born-digital-detection/hOCR-escaping all into Phase 43 (rather than spreading them across later phases) is deliberate: PITFALLS.md flags every one of these as "must be architectural from the first line, not retrofitted" — the converter's core loop shape depends on all of them being decided up front (e.g., a "per-page has-text" branch, a streaming-not-batch rasterize loop).
- SEED-010's dynamic engine registry is explicitly **out of scope** for this milestone (dormant trigger condition not met) — OCR is wired exactly like the prior 5 engine classes, hardcoded registration, no generalization.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 43:** Exact Tesseract/PaddleOCR CLI flag specifics (PSM/OEM combinations, hOCR/ALTO config-file names) are flagged MEDIUM confidence in ARCHITECTURE.md — verify directly against the chosen tool's live 5.5.3 CLI output during planning, not from training-data assumption. Also: the born-digital-PDF detection heuristic (pdfinfo/pdffonts-style "only_images" check) needs a concrete implementation decision — the *decision* (fail-closed reject) is settled, but the *detection mechanism* is not yet code.
- **Phase 45:** The seconds-per-page measurement methodology (adapted from Audio/AV's real-time-factor ratio metric to a per-unit-of-work metric) is a new metric shape — needs its own methodology, not a blind script clone.

Phases with standard patterns (skip research-phase):
- **Phase 44:** Purely mechanical engine-class wiring (queue/worker/reconciler/migration) — an exact structural repeat of Audio (Phase 30-33) and AV (Phase 34-38), five prior implementations already prove the pattern.
- **Phase 46:** KEDA/Helm wiring is a direct template copy of `deployment-av-worker.yaml`/`scaledobject-av.yaml`.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Versions/licenses verified via GitHub Releases API + a live `debian:bookworm-slim` container probe; accuracy/speed comparisons are MEDIUM (vendor/blog benchmarks, not independently reproduced) but multiple sources agree on direction and it matches known engine architecture |
| Features | HIGH (mechanics) / MEDIUM (accuracy-uplift numbers) | Tesseract/hOCR/ALTO/PDF-text-layer-detection mechanics are grounded in official docs + OCRmyPDF's reference implementation; preprocessing accuracy-lift percentages (15-30%) are vendor-blog consensus, not a controlled study |
| Architecture | HIGH | Every claim is grounded directly in the current codebase (`internal/convert/`, `internal/worker/worker.go`, `internal/queue/queue.go`, migrations, chart templates) — not inferred; MEDIUM only on exact CLI invocation specifics, which is an implementation detail for Phase 43, not an architectural risk |
| Pitfalls | HIGH (Tesseract/Leptonica behavior, DPI/PSM/OEM, hOCR spec, born-digital detection, CVE history) / MEDIUM (exact RTF numbers, PaddleOCR specifics) | Corroborated by official tessdoc + multiple independent sources + a maintainer-acknowledged security issue (Leptonica); exact throughput/timeout numbers are unmeasurable without a live Phase 45 gate, same caveat already accepted for Audio/AV |

**Overall confidence:** HIGH

### Gaps to Address

- **Engine-choice eval-gate is not yet run** — Tesseract is the strong, converged default across all four research tracks, but the locked project process still requires Phase 43 to validate it against real scanned-document fixtures before treating it as final (not a stack-research gap, a process gate).
- **Born-digital PDF detection mechanism** — the *decision* (fail-closed reject, option 1) is resolved across FEATURES/ARCHITECTURE/PITFALLS, but the concrete detection implementation (which library call, what threshold counts as "meaningful existing text layer") is unspecified — resolve during Phase 43 planning.
- **webp→PNG normalization** — STACK.md flags Leptonica's webp support as inconsistent across Debian's bundled build; verify at Phase 43 implementation time whether a decode-to-PNG pre-step (reusing the image engine's existing decode path) is actually needed.
- **Deskew/denoise evidence gate** — per the Reconciled Position above, this is deliberately left open pending Phase 43's measured accuracy comparison; do not let it silently become either a day-one dependency or a permanently-ignored TODO.
- **Seconds-per-page measurement methodology** — Phase 45's metric is a new shape (not a straight RTF-ratio clone); the exact corpus composition (page counts, languages, quality tiers) needs to be defined during Phase 45 planning, not assumed from the audio/av precedent.

## Sources

### Primary (HIGH confidence)
- `https://api.github.com/repos/tesseract-ocr/tesseract/releases` — Tesseract 5.5.3, verified via GitHub API
- `https://api.github.com/repos/tesseract-ocr/tesseract/license`, `tessdata_fast`/`tessdata_best` license checks — Apache-2.0
- Live `debian:bookworm-slim` container probe — confirmed `tesseract-ocr 5.3.0-2`, `poppler-utils 22.12.0-2+deb12u2` package footprints
- `tesseract-ocr.github.io/tessdoc/*` (Command-Line Usage, ImproveQuality, Data-Files, ReleaseNotes) — official Tesseract docs
- `hocr-spec 1.2` (kba.github.io) — hOCR format specification
- `github.com/tesseract-ocr/tesseract/issues/1456` ("Leptonica has no regard for security") — maintainer-acknowledged structural security concern
- OpenCVE Leptonica/Tesseract CVE listings
- `github.com/jbarlow83/OCRmyPDF` + its errors doc — reference implementation of born-digital text-layer detection and skip/force/redo logic
- Project source: `internal/convert/*`, `internal/worker/worker.go`, `internal/queue/*`, `internal/api/handlers.go`, `internal/reconciler/reconciler.go`, `internal/db/migrations/0006_audio_engine.sql`/`0007_job_progress.sql`, `cmd/av-worker/main.go`, `deploy/chart/octoconv/*`, `docker-compose.yml` — read directly
- `.planning/PROJECT.md`, `.planning/seeds/SEED-009.md`, `.planning/seeds/SEED-010.md` — project-internal planning documents (locked scope, prior Key Decisions precedent)

### Secondary (MEDIUM confidence)
- WebSearch aggregate (imagetotable.ai, tildalice.io, mljourney.com, koncile.ai, gigagpu.com, codesota.com) — Tesseract-vs-PaddleOCR CPU/GPU speed-accuracy comparisons, direction-consistent across sources
- Docparser and Markaicode preprocessing-accuracy blog posts — 15-30% accuracy-lift figures from deskew/binarize/DPI-normalize, vendor-blog consensus
- PyImageSearch / DeepWiki PSM-OEM explainer articles — corroborated by official tessdoc but not independently re-verified against a live invocation
- HuggingFace/PaddleOCR vendor blog — PP-OCRv5 language coverage and CPU speedup claims (vendor-reported, not independently benchmarked)

### Tertiary (LOW confidence)
- None flagged — all findings above have at least MEDIUM-confidence corroboration from an official or first-party source

---
*Research completed: 2026-07-26*
*Ready for roadmap: yes*
