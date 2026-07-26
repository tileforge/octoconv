# Roadmap: OctoConv

## Milestones

- ✅ **v1.0 Hardening MVP** — Phases 1-4 (shipped 2026-07-08) — see `.planning/milestones/v1.0-ROADMAP.md`
- ✅ **v1.1 Tech Debt Cleanup** — Phases 5-7 (shipped 2026-07-08) — see `.planning/milestones/v1.1-ROADMAP.md`
- ✅ **v1.2 Document Engine Class** — Phases 8-11 (shipped 2026-07-10) — see `.planning/milestones/v1.2-ROADMAP.md`
- ✅ **v1.3 Document Class v2** — Phases 12-16 (shipped 2026-07-12) — see `.planning/milestones/v1.3-ROADMAP.md`
- ✅ **v1.4 CI, Presets & Debt Cleanup** — Phases 17-19 (shipped 2026-07-13) — see `.planning/milestones/v1.4-ROADMAP.md`
- ✅ **v1.5 MCP Access & Document Fidelity** — Phases 20-23 (shipped 2026-07-13) — see `.planning/milestones/v1.5-ROADMAP.md`
- ✅ **v1.6 Kubernetes & KEDA** — Phases 24-28 (shipped 2026-07-17) — see `.planning/milestones/v1.6-ROADMAP.md`
- ✅ **v1.7 Audio Engine & Hardening** — Phases 29-33 (shipped 2026-07-18) — see `.planning/milestones/v1.7-ROADMAP.md`
- ✅ **v1.8 AV Engine (video/ffmpeg)** — Phases 34-37 (shipped 2026-07-23) — see `.planning/milestones/v1.8-ROADMAP.md`
- ✅ **v1.9 Job Progress & Control** — Phases 38-42 (shipped 2026-07-25) — see `.planning/milestones/v1.9-ROADMAP.md`
- 🚧 **v1.10 OCR Engine (text extraction)** — Phases 43-46 (in progress, started 2026-07-26)

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

<details>
<summary>✅ v1.0 Hardening MVP (Phases 1-4) — SHIPPED 2026-07-08</summary>

- [x] Phase 1: Merge, Auth & Rate Limiting (4/4 plans) — completed 2026-07-04
- [x] Phase 2: Webhook Delivery (3/3 plans) — completed 2026-07-04
- [x] Phase 3: Retry-Safety & Reconciler (3/3 plans) — completed 2026-07-06
- [x] Phase 4: Content Validation, Storage Lifecycle & Observability (5/5 plans) — completed 2026-07-07

Full details: `.planning/milestones/v1.0-ROADMAP.md`

</details>

<details>
<summary>✅ v1.1 Tech Debt Cleanup (Phases 5-7) — SHIPPED 2026-07-08</summary>

- [x] Phase 5: Webhook SSRF Private-IP Opt-Out (1/1 plans) — completed 2026-07-08
- [x] Phase 6: Reconciler Webhook-Gap Sweep & Staleness Soak Test (4/4 plans) — completed 2026-07-08
- [x] Phase 7: Image Dimension Limit (Decompression-Bomb Protection) (2/2 plans) — completed 2026-07-08

Full details: `.planning/milestones/v1.1-ROADMAP.md`

</details>

<details>
<summary>✅ v1.2 Document Engine Class (Phases 8-11) — SHIPPED 2026-07-10</summary>

- [x] Phase 8: Document Content Safety & Format Detection (2/2 plans) — completed 2026-07-09
- [x] Phase 9: LibreOffice Converter Engine (2/2 plans) — completed 2026-07-09
- [x] Phase 10: Document Worker & Reconciler Integration (4/4 plans) — completed 2026-07-09
- [x] Phase 11: API Routing & End-to-End Document Conversion (4/4 plans, incl. gap closure 11-04) — completed 2026-07-10

Full details: `.planning/milestones/v1.2-ROADMAP.md`

</details>

<details>
<summary>✅ v1.3 Document Class v2 (Phases 12-16) — SHIPPED 2026-07-12</summary>

- [x] Phase 12: Tech Debt Cleanup (1/1 plans) — completed 2026-07-10
- [x] Phase 13: Cross-Format Conversion & Input Safety (3/3 plans) — completed 2026-07-10
- [x] Phase 14: Validated Conversion Options & PDF/A Export (3/3 plans) — completed 2026-07-10
- [x] Phase 15: HTML→PDF Chromium Engine (5/5 plans) — completed 2026-07-11
- [x] Phase 16: Webhook Delivery Decoupling (5/5 plans, incl. gap closure 16-05) — completed 2026-07-12

Full details: `.planning/milestones/v1.3-ROADMAP.md`

</details>

<details>
<summary>✅ v1.4 CI, Presets & Debt Cleanup (Phases 17-19) — SHIPPED 2026-07-13</summary>

- [x] Phase 17: Tech Debt Cleanup (2/2 plans) — completed 2026-07-12
- [x] Phase 18: Presets (4/4 plans) — completed 2026-07-12
- [x] Phase 19: CI Pipeline (2/2 plans) — completed 2026-07-13

Full details: `.planning/milestones/v1.4-ROADMAP.md`

</details>

<details>
<summary>✅ v1.5 MCP Access & Document Fidelity (Phases 20-23) — SHIPPED 2026-07-13</summary>

- [x] Phase 20: Presets REST CRUD & Format Discovery (2/2 plans) — completed 2026-07-13
- [x] Phase 21: MCP Server (3/3 plans) — completed 2026-07-13
- [x] Phase 22: CFB Encrypted-vs-Legacy Classification (2/2 plans) — completed 2026-07-13
- [x] Phase 23: veraPDF ISO 19005 Validation (3/3 plans) — completed 2026-07-13

Full details: `.planning/milestones/v1.5-ROADMAP.md`

</details>

<details>
<summary>✅ v1.6 Kubernetes & KEDA (Phases 24-28) — SHIPPED 2026-07-17</summary>

- [x] Phase 24: Helm Chart Core & Landmine Closure (3/3 plans) — completed 2026-07-14
- [x] Phase 25: MCP Streamable HTTP (3/3 plans) — completed 2026-07-14
- [x] Phase 26: Operator System-Presets REST (2/2 plans) — completed 2026-07-14
- [x] Phase 27: KEDA Autoscaling (3/3 plans) — completed 2026-07-16
- [x] Phase 28: Autoscale Load-Proof (3/3 plans) — completed 2026-07-17

Full details: `.planning/milestones/v1.6-ROADMAP.md`

</details>

<details>
<summary>✅ v1.7 Audio Engine & Hardening (Phases 29-33) — SHIPPED 2026-07-18</summary>

- [x] Phase 29: v1.6 Hardening Tail (3/3 plans) — completed 2026-07-17
- [x] Phase 30: Audio Engine Foundation (3/3 plans) — completed 2026-07-18
- [x] Phase 31: Queue, Worker & Routing Integration (4/4 plans) — completed 2026-07-18
- [x] Phase 32: Containerization & Local E2E + RTF Gate (5/5 plans) — completed 2026-07-18
- [x] Phase 33: KEDA/Helm Chart Integration (3/3 plans) — completed 2026-07-18

Full details: `.planning/milestones/v1.7-ROADMAP.md`

</details>

<details>
<summary>✅ v1.8 AV Engine (video/ffmpeg) (Phases 34-37) — SHIPPED 2026-07-23</summary>

- [x] Phase 34: AV Engine Foundation (3/3 plans) — completed 2026-07-20
- [x] Phase 35: Queue, Worker & Routing Integration (7/7 plans) — completed 2026-07-22
- [x] Phase 36: Containerization & RTF-Measured Timeout (5/5 plans) — completed 2026-07-23
- [x] Phase 37: KEDA/Helm Chart Integration (3/3 plans) — completed 2026-07-23

Full details: `.planning/milestones/v1.8-ROADMAP.md`

</details>

<details>
<summary>✅ v1.9 Job Progress & Control (Phases 38-42) — SHIPPED 2026-07-25</summary>

**Milestone Goal:** Сделать длинные конвертации прозрачными и управляемыми — клиент видит прогресс/ETA в реальном времени, может листать свои джобы и отменять их.

- [x] Phase 38: Schema Foundation (1/1 plans) — completed 2026-07-24
- [x] Phase 39: Cancellation (end-to-end) (4/4 plans) — completed 2026-07-24
- [x] Phase 40: Job Listing (2/2 plans) — completed 2026-07-25
- [x] Phase 41: Progress Core (4/4 plans) — completed 2026-07-25
- [x] Phase 42: Live Push (4/4 plans) — completed 2026-07-25

Full details: `.planning/milestones/v1.9-ROADMAP.md`

</details>

### 🚧 v1.10 OCR Engine (In Progress)

**Milestone Goal:** Клиент может извлечь машиночитаемый текст из изображений и сканированных PDF через тот же async-контур, что и остальные движки — offline, детерминированно, с онлайн-прогрессом. Шестой engine-класс.

- [x] **Phase 43: OCR Engine Foundation** - Standalone `OCRConverter` extracts txt/hOCR from images and scanned PDFs, streaming page-by-page — memory-bomb-safe and born-digital-PDF-safe from the first line, before any queue/worker plumbing (completed 2026-07-26)
- [ ] **Phase 44: Async Pipeline Wiring** - OCR jobs flow end-to-end through the async pipeline (queue, `cmd/ocr-worker`, retry, reconciler, API routing) over the completely unmodified v1.9 progress/SSE/webhook machinery
- [ ] **Phase 45: Containerization + Measured Timeout Gate** - A running `ocr-worker` container passes a live E2E, with `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` sized from a measured seconds-per-page gate rather than a copied constant
- [ ] **Phase 46: KEDA/Helm Chart Integration** - The OCR class autoscales in the cluster with production parity to the other five engine classes, scale-from-zero live-proven

## Phase Details

### Phase 43: OCR Engine Foundation
**Goal**: A standalone `OCRConverter` extracts plain text (txt) and word-coordinate output (hOCR/ALTO) from images and scanned/image-only PDFs via Tesseract, built and testable directly against the binary — memory-bomb-safe and born-digital-PDF-safe from the first line — before any queue/worker plumbing.
**Depends on**: Nothing (first phase of v1.10 — validate the novel OCR/rasterize external-process surface standalone first, mirroring Audio's Phase 30 / AV's Phase 34 scope fence)
**Requirements**: OCR-01, OCR-02, OCR-03, OCR-04, OCRO-01, OCRE-02, OCRE-03
**Success Criteria** (what must be TRUE):
  1. `OCRConverter` extracts plain txt from a jpg/png/tiff/webp image via Tesseract with explicitly pinned PSM/OEM flags (resolved against real scanned-document fixtures during the engine eval-gate — Tesseract vs. PaddleOCR — not assumed defaults), verified by direct invocation against the real binary.
  2. `OCRConverter` extracts txt from a scanned/image-only PDF by streaming exactly one page at a time (`pdftoppm -f N -l N` at a fixed ~300 DPI computed from the page box size, then Tesseract, then delete the page image before the next iteration) — never rasterizing the whole document up front — and drives a page-granular progress estimator (`pages_done/total_pages`, ETA from seconds/page); a single-image job never calls it and instead reports `null`→100.
  3. The same input also produces hOCR (and ALTO, if built as a distinct target) output with word/line bounding boxes, assembled via a real XML/HTML encoder — never string concatenation — and a round-trip test proves OCR'd text containing `&`/`<`/`>` still parses as valid XML.
  4. A PDF whose declared page count (via `pdfinfo`) exceeds `OCR_MAX_PAGES` is rejected fail-closed before any page is rendered; a PDF that already has an extractable text layer (born-digital) is rejected fail-closed (422) rather than silently re-OCR'd.
  5. `OCROpts{Language}` is validated against a closed allowlist (e.g. `eng`, `rus`, combinable as `eng+rus`) via the shared strict-parse helper (mirrors `AudioOpts`) — an unlisted language code is rejected before any work begins, and an injection test proves client bytes never reach Tesseract's `-l` argv unvalidated.
**Plans**: 4 plans (3 waves)
Plans:
- [x] 43-01-PLAN.md — OCROpts closed language allowlist (eng/rus/eng+rus) strict-parse + injection defense
- [x] 43-02-PLAN.md — Guard layer: PDF page-count cap + per-page page-box physical-size cap + image pixel cap + born-digital pdftotext detection (fail-closed pre-render)
- [x] 43-03-PLAN.md — OCRConverter core: Pairs/Engine/Convert image→txt at PSM 3/OEM 1 + hOCR/ALTO via real XML encoder
- [x] 43-04-PLAN.md — Streaming per-page PDF path (pdftoppm→tesseract→delete) + page-granular ConvertWithProgress

### Phase 44: Async Pipeline Wiring
**Goal**: OCR jobs (image/PDF → txt/hOCR) flow end-to-end through the async pipeline with correct retry/dedup semantics and engine-routing, over the completely unmodified v1.9 progress/SSE/webhook machinery.
**Depends on**: Phase 43 (needs the working converter to wire into the async lifecycle)
**Requirements**: OCRE-01, OCRE-05, OCRP-01
**Success Criteria** (what must be TRUE):
  1. An uploaded image or PDF job targeting txt/hocr is routed to a dedicated `ocr` asynq queue by the API's `EngineFor` content-detection path (new PDF magic-byte sniffer added alongside the existing image sniffers) and consumed end-to-end by `cmd/ocr-worker` (queued → active → done), with the reconciler routing stranded `jobs.engine='ocr'` jobs to the same queue.
  2. A declared/detected format mismatch on an image or PDF upload is rejected fail-closed with 422 before any OCR work or S3 write — exactly like every other engine class.
  3. Both new untrusted external processes (`pdftoppm` rasterizer and `tesseract`) run through the existing hardened `runCommand` (process-group `Setpgid` + SIGKILL-on-timeout, `nobody` posture), and a blank/text-free page completes as `done` with empty txt content rather than `failed`.
  4. A live end-to-end test shows a single image job's `progress_pct` going `null`→100 and a multi-page scanned PDF's `progress_pct` genuinely ramping page-by-page over `GET /v1/jobs/{id}` (and/or SSE), with zero changes to `internal/convert/progress.go`'s shared types.
  5. A stage-aware `isOCRTerminal` classifier and a fresh `OCRUniqueTTL` (derived from the OCR engine's own timeout/retry budget, never copied) prevent duplicate processing and correctly distinguish transient vs. terminal OCR failures.
**Plans**: 4 plans (3 waves)
Plans:
- [x] 44-01-PLAN.md — Migration 0009 + matchPDF sniff + register OCRConverter + EngineFor routing + EngineOCR upload limit (OCRP-01/OCRE-01)
- [x] 44-02-PLAN.md — OCR queue/task + fresh-derived OCRUniqueTTL + EnqueueOCRConvert (OCRP-01)
- [ ] 44-03-PLAN.md — HandleOCRConvert + isOCRTerminal + cmd/ocr-worker + reconciler ocr routing (OCRP-01/OCRE-05)
- [ ] 44-04-PLAN.md — Integration proof: image null→100 + multi-page PDF page-by-page ramp + progress.go/sse.go unchanged fence (OCRP-01)

### Phase 45: Containerization + Measured Timeout Gate
**Goal**: A running `ocr-worker` container in docker-compose passes a full live E2E, with `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` sized from a measured seconds-per-page gate rather than a copied or guessed constant.
**Depends on**: Phase 44 (needs the queue/worker contract stable before containerizing; can't measure a real timeout without the full pipeline producing real job outcomes to time)
**Requirements**: OCRE-04
**Success Criteria** (what must be TRUE):
  1. `Dockerfile.ocr-worker` installs Tesseract (source-built, tag-pinned) + poppler-utils, bakes only a short SHA-pinned `tessdata_fast` language allowlist (matching the closed `OCROpts` allowlist, never the full upstream tessdata set), and an `ocr-worker` compose service OCRs an uploaded image and a multi-page scanned PDF end-to-end through the live compose stack (upload → poll → presigned txt/hocr download) with a signed webhook confirmed.
  2. `scripts/ocr-timing-measure.sh` measures real seconds-per-page (not an RTF ratio) across a representative corpus (clean scan, dense multi-column, noisy/skewed) under production-equivalent CPU/memory limits, and drives a documented go/no-go decision (mirroring the audio/AV RTF-gate precedent) that derives `OCR_ENGINE_TIMEOUT` and `OCR_MAX_PAGES`.
  3. The measured `OCR_ENGINE_TIMEOUT` stays under the global reconciler-CAP, and the derived numbers — plus their measurement conditions (arch/cpu/mem/N/p95) — are recorded as a Key Decision, never silently copied from `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT`.
  4. The `ocr-worker` image is added to the CI bake matrix with its `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES`/`OCR_WORKER_CONCURRENCY`/ShutdownTimeout env wired, and all queue-client-constructing services propagate the new env identically (IN-02 pattern).
**Plans**: TBD

### Phase 46: KEDA/Helm Chart Integration
**Goal**: The OCR class autoscales in the cluster with production parity to the other five engine classes, and scale-from-zero is live-proven.
**Depends on**: Phase 45 (KEDA cooldown/stabilization and grace-period tuning consume Phase 45's measured `OCR_ENGINE_TIMEOUT`)
**Requirements**: OCRP-02
**Success Criteria** (what must be TRUE):
  1. An `ocr-worker` Deployment plus a KEDA `ScaledObject` ship in the chart with `terminationGracePeriodSeconds`/`scaleDownStabilizationSeconds` derived from the measured `OCR_ENGINE_TIMEOUT`, and the WR-01 fail-safe triad (`ignoreNullValues:"false"`, fallback replicas, retry-inclusive PromQL) applied verbatim from the first commit.
  2. `QueueOCR` is registered in the always-on api queue-depth collector so KEDA resolves the OCR backlog at genuinely zero replicas.
  3. Scale-from-zero is live-proven for the OCR class, capturing timestamped Phase-33/37-style evidence (0→1 on real queue depth, scale back to 0 after drain).
  4. env-parity (IN-02 pattern) is confirmed across all queue-client-constructing services for the new `OCR_*` env vars, and a long multi-page OCR job survives a genuine N→N-1 HPA downscale without a premature SIGTERM.
**Plans**: TBD

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|-----------------|--------|-----------|
| 1. Merge, Auth & Rate Limiting | v1.0 | 4/4 | Complete | 2026-07-04 |
| 2. Webhook Delivery | v1.0 | 3/3 | Complete | 2026-07-04 |
| 3. Retry-Safety & Reconciler | v1.0 | 3/3 | Complete | 2026-07-06 |
| 4. Content Validation, Storage Lifecycle & Observability | v1.0 | 5/5 | Complete | 2026-07-07 |
| 5. Webhook SSRF Private-IP Opt-Out | v1.1 | 1/1 | Complete | 2026-07-08 |
| 6. Reconciler Webhook-Gap Sweep & Staleness Soak Test | v1.1 | 4/4 | Complete | 2026-07-08 |
| 7. Image Dimension Limit (Decompression-Bomb Protection) | v1.1 | 2/2 | Complete | 2026-07-08 |
| 8. Document Content Safety & Format Detection | v1.2 | 2/2 | Complete | 2026-07-09 |
| 9. LibreOffice Converter Engine | v1.2 | 2/2 | Complete | 2026-07-09 |
| 10. Document Worker & Reconciler Integration | v1.2 | 4/4 | Complete | 2026-07-09 |
| 11. API Routing & End-to-End Document Conversion | v1.2 | 4/4 | Complete | 2026-07-10 |
| 12. Tech Debt Cleanup | v1.3 | 1/1 | Complete    | 2026-07-10 |
| 13. Cross-Format Conversion & Input Safety | v1.3 | 3/3 | Complete    | 2026-07-10 |
| 14. Validated Conversion Options & PDF/A Export | v1.3 | 3/3 | Complete    | 2026-07-10 |
| 15. HTML→PDF Chromium Engine | v1.3 | 5/5 | Complete    | 2026-07-11 |
| 16. Webhook Delivery Decoupling | v1.3 | 5/5 | Complete | 2026-07-12 |
| 17. Tech Debt Cleanup | v1.4 | 2/2 | Complete | 2026-07-12 |
| 18. Presets | v1.4 | 4/4 | Complete | 2026-07-12 |
| 19. CI Pipeline | v1.4 | 2/2 | Complete | 2026-07-13 |
| 20. Presets REST CRUD & Format Discovery | v1.5 | 2/2 | Complete | 2026-07-13 |
| 21. MCP Server | v1.5 | 3/3 | Complete | 2026-07-13 |
| 22. CFB Classification | v1.5 | 2/2 | Complete | 2026-07-13 |
| 23. veraPDF ISO 19005 Validation | v1.5 | 3/3 | Complete | 2026-07-13 |
| 24. Helm Chart Core & Landmine Closure | v1.6 | 3/3 | Complete | 2026-07-14 |
| 25. MCP Streamable HTTP | v1.6 | 3/3 | Complete | 2026-07-14 |
| 26. Operator System-Presets REST | v1.6 | 2/2 | Complete    | 2026-07-14 |
| 27. KEDA Autoscaling | v1.6 | 3/3 | Complete    | 2026-07-16 |
| 28. Autoscale Load-Proof | v1.6 | 3/3 | Complete    | 2026-07-17 |
| 29. v1.6 Hardening Tail | v1.7 | 3/3 | Complete    | 2026-07-17 |
| 30. Audio Engine Foundation | v1.7 | 3/3 | Complete    | 2026-07-18 |
| 31. Queue, Worker & Routing Integration | v1.7 | 4/4 | Complete    | 2026-07-18 |
| 32. Containerization & Local E2E + RTF Gate | v1.7 | 5/5 | Complete    | 2026-07-18 |
| 33. KEDA/Helm Chart Integration | v1.7 | 3/3 | Complete    | 2026-07-18 |
| 34. AV Engine Foundation | v1.8 | 3/3 | Complete    | 2026-07-20 |
| 35. Queue, Worker & Routing Integration | v1.8 | 7/7 | Complete    | 2026-07-22 |
| 36. Containerization & RTF-Measured Timeout | v1.8 | 5/5 | Complete    | 2026-07-23 |
| 37. KEDA/Helm Chart Integration | v1.8 | 3/3 | Complete    | 2026-07-23 |
| 38. Schema Foundation | v1.9 | 1/1 | Complete    | 2026-07-24 |
| 39. Cancellation (end-to-end) | v1.9 | 4/4 | Complete    | 2026-07-24 |
| 40. Job Listing | v1.9 | 2/2 | Complete   | 2026-07-25 |
| 41. Progress Core | v1.9 | 4/4 | Complete | 2026-07-25 |
| 42. Live Push | v1.9 | 4/4 | Complete   | 2026-07-25 |
| 43. OCR Engine Foundation | v1.10 | 4/4 | Complete   | 2026-07-26 |
| 44. Async Pipeline Wiring | v1.10 | 2/4 | In Progress|  |
| 45. Containerization + Measured Timeout Gate | v1.10 | 0/TBD | Not started | - |
| 46. KEDA/Helm Chart Integration | v1.10 | 0/TBD | Not started | - |

---

*v1.9 (Job Progress & Control) shipped 2026-07-25, Phases 38-42 — 13/13 requirements, 3 live gates. v1.10 (OCR Engine) roadmapped 2026-07-26, Phases 43-46 — 12/12 requirements mapped. Next: `/gsd:plan-phase 43`.*
