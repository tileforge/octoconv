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
- ✅ **v1.10 OCR Engine (text extraction)** — Phases 43-46 (shipped 2026-07-27) — see `.planning/milestones/v1.10-ROADMAP.md`
- 🚧 **v1.11 Pandoc Engine** — Phases 47-50 (in progress, started 2026-07-30)

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

<details>
<summary>✅ v1.10 OCR Engine (text extraction) (Phases 43-46) — SHIPPED 2026-07-27</summary>

**Milestone Goal:** Клиент может извлечь машиночитаемый текст из изображений и сканированных PDF через тот же async-контур, что и остальные движки — offline, детерминированно, с онлайн-прогрессом. Шестой engine-класс.

- [x] Phase 43: OCR Engine Foundation - Standalone `OCRConverter` extracts txt/hOCR from images and scanned PDFs, streaming page-by-page — memory-bomb-safe and born-digital-PDF-safe from the first line, before any queue/worker plumbing (completed 2026-07-26)
- [x] Phase 44: Async Pipeline Wiring - OCR jobs flow end-to-end through the async pipeline (queue, `cmd/ocr-worker`, retry, reconciler, API routing) over the completely unmodified v1.9 progress/SSE/webhook machinery (completed 2026-07-26)
- [x] Phase 45: Containerization + Measured Timeout Gate - A running `ocr-worker` container passes a live E2E, with `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` sized from a measured seconds-per-page gate rather than a copied constant (completed 2026-07-27)
- [x] Phase 46: KEDA/Helm Chart Integration - The OCR class autoscales in the cluster with production parity to the other five engine classes, scale-from-zero live-proven (completed 2026-07-27)

Full details: `.planning/milestones/v1.10-ROADMAP.md`

</details>

### 🚧 v1.11 Pandoc Engine (In Progress)

**Milestone Goal:** Седьмой engine-класс — pandoc markup/document interconvert (md ↔ rst ↔ org ↔ latex ↔ epub ↔ html ↔ docx), только markup↔markup (без →pdf), через тот же async-контур, что и остальные шесть движков.

- [x] **Phase 47: Pandoc Engine Foundation** - Standalone `PandocConverter` (closed markup-family allowlist, `--standalone`, hardened `runCommand` with raw HTML/TeX passthrough disabled + --sandbox + no-network) built and tested directly against the real pandoc binary, before any queue/worker plumbing (completed 2026-07-30)
- [x] **Phase 48: Async Pipeline Wiring** - Pandoc jobs flow end-to-end through the async pipeline (queue `pandoc`, `cmd/pandoc-worker`, stage-aware retry, reconciler-routing, API-routing) over the unmodified v1.9 progress/webhook infra (completed 2026-07-30)
- [x] **Phase 49: Containerization + Measured Timeout Gate** - A running `pandoc-worker` container (pandoc pinned, NO TeX) passes a live E2E, with `PANDOC_ENGINE_TIMEOUT` sized from a measured gate rather than a copied constant
- [ ] **Phase 50: KEDA/Helm Chart Integration** - The pandoc class autoscales in the cluster with production parity to the other six engine classes, scale-from-zero live-proven

## Phase Details

### Phase 47: Pandoc Engine Foundation
**Goal**: A standalone `PandocConverter` converts between markup-family formats (md/rst/org/latex/epub/html/docx) through a closed `(from→to)` allowlist, security-hardened from the first line (no raw HTML/TeX passthrough, no shell-escape, no network), built and tested directly against the real pandoc binary — before any queue/worker plumbing.
**Depends on**: Nothing (first phase of v1.11 — validate the novel pandoc external-process surface standalone first, mirroring Audio's Phase 30 / AV's Phase 34 / OCR's Phase 43 scope fence)
**Requirements**: PDOC-01, PDOC-02, PDCO-01, PDCE-01, PDCE-02, PDCE-03
**Success Criteria** (what must be TRUE):
  1. `PandocConverter` converts between any two markup formats in a closed `(from→to)` allowlist covering the markup family (md/rst/org/latex/epub/html/docx), verified by direct invocation against the real pandoc binary; an unlisted pair is rejected before any subprocess runs (mirrors existing `Pairs()` conventions).
  2. Output that requires document structure (docx/epub/html) is produced with `--standalone` — a complete, valid document rather than a bare fragment — and the result is validated by the same content-sniffer that guards uploads.
  3. Every pandoc invocation runs with raw HTML/TeX passthrough disabled, `--sandbox`, no network access, and through the existing hardened `runCommand` (Setpgid + SIGKILL-on-timeout, `USER nobody` posture) — proven by a test showing crafted input cannot trigger shell-escape, `\input`, or a network fetch.
  4. A closed `PandocOpts` allowlist (e.g. `standalone`/`toc`/`wrap`/`section-divs`, typed strict-parse mirroring `OCROpts`/`AudioOpts`) rejects any unlisted opt with 422 before any work begins, with an injection test proving client bytes never reach pandoc's argv unvalidated.
  5. Uploads are gated fail-closed by magic-byte/content validation across the markup family (text-based formats sniffed correctly, not only binary containers like docx/epub), and an oversize or pathological input (size cap; pandoc-specific expansion/bomb shape) is rejected before conversion runs.
**Plans**: 3 plans

Plans:
- [x] 47-01-PLAN.md — PandocOpts closed allowlist + strict-parse + discrete-argv injection defense (PDCO-01)
- [x] 47-02-PLAN.md — Markup content-validation probes (LooksLikeLaTeX/LooksLikeText) + input-size DoS guard + epub/MIME registry (PDCE-01, PDCE-03)
- [x] 47-03-PLAN.md — PandocConverter core: curated closed Pairs, hardened runCommand invocation, --standalone output validation, SC3 security canary (PDOC-01, PDOC-02, PDCE-02)

### Phase 48: Async Pipeline Wiring
**Goal**: Pandoc jobs (markup → markup) flow end-to-end through the async pipeline with correct retry/dedup semantics and engine-routing, over the unmodified v1.9 progress/webhook machinery.
**Depends on**: Phase 47 (needs the working converter to wire into the async lifecycle)
**Requirements**: PDCP-01
**Success Criteria** (what must be TRUE):
  1. Migration adds `pandoc` to the `jobs.engine` enum; an uploaded markup document targeting a supported pair is routed to a dedicated `pandoc` asynq queue by content-detection and consumed end-to-end by `cmd/pandoc-worker` (queued → active → done).
  2. The reconciler routes stranded `jobs.engine='pandoc'` jobs to the pandoc queue, and API routing dispatches single-input/single-target-format pandoc jobs without any changes to v1.9's progress/SSE/webhook infra.
  3. A stage-aware terminal/transient classifier and a fresh `PandocUniqueTTL` (derived from pandoc's own retry/timeout budget, never copied from a neighboring engine) prevent duplicate processing.
  4. Converting to an empty-but-valid output (e.g. an empty source document) completes the job as `done`, not `failed`.
**Plans**: 3 plans
Plans:
- [x] 48-01-PLAN.md — pandoc asynq plumbing: TypePandocConvert/QueuePandoc/NewPandocConvertTask, fresh PandocUniqueTTL (own placeholder budget), 2-entry retry schedule, EnqueuePandocConvert
- [x] 48-02-PLAN.md — migration 0010 (+pandoc enum), register PandocConverter, EngineFor routing + disjointness test, API opts/enqueue/content-gate wiring + EnginePandoc upload ceiling
- [x] 48-03-PLAN.md — HandlePandocConvert + isPandocTerminal (timeout+engine terminal, D-01) + pandocFailureCode, cmd/pandoc-worker, reconciler pandoc route, empty→done + exec/progress/sse fence

### Phase 49: Containerization + Measured Timeout Gate
**Goal**: A running `pandoc-worker` container in docker-compose passes a full live E2E, with `PANDOC_ENGINE_TIMEOUT` sized from a measured gate rather than a copied or guessed constant, in a lean TeX-free image.
**Depends on**: Phase 48 (needs the queue/worker contract stable before containerizing; can't measure a real timeout without the full pipeline producing real job outcomes to time)
**Requirements**: PDCE-04
**Success Criteria** (what must be TRUE):
  1. `Dockerfile.pandoc-worker` installs a pinned pandoc binary with **no TeX toolchain** (lean image), and a compose `pandoc-worker` service converts an uploaded markup document end-to-end (upload → poll → presigned download) through the live compose stack with a signed webhook confirmed.
  2. `scripts/pandoc-timing-measure.sh` measures real conversion cost across a representative markup corpus (varying source formats/sizes) under production-equivalent CPU/memory limits and drives a documented go/no-go decision (mirroring the audio/AV/OCR gate precedent).
  3. The measured `PANDOC_ENGINE_TIMEOUT` is **derived from this measurement, not copied** from `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT`/`OCR_ENGINE_TIMEOUT`, stays under the global 900s reconciler CAP, and is recorded as a Key Decision with its measurement conditions (arch/cpu/mem/N/p95).
  4. IN-02 env parity: all queue-client-constructing services propagate `PANDOC_*` env identically, and the `pandoc-worker` image is added to the CI bake matrix.
**Plans**: 4 plans
Plans:
- [x] 49-01-PLAN.md — Dockerfile.pandoc-worker: SHA-256-pinned static pandoc 3.x binary, TeX-free debian-slim, USER nobody, dual-arch
- [x] 49-02-PLAN.md — scripts/pandoc-timing-measure.sh (wall-sec/conversion gate) + pandoc-worker compose service (placeholder timeout) + IN-02 PANDOC_* across all 10 services + CI bake
- [x] 49-03-PLAN.md — SUPERVISED measurement checkpoint: derive PANDOC_ENGINE_TIMEOUT = ceil(worst-p95 x 2.0) < 900s CAP (Path-B PANDOC_MAX_INPUT_BYTES lever), plug in + record Key Decision
- [x] 49-04-PLAN.md — live E2E checkpoint: TestPandocConversionE2E (md->html + md->docx + signed webhook) against the real compose pandoc-worker — GREEN (PASS 4.13s, both output shapes + HMAC webhook)

### Phase 50: KEDA/Helm Chart Integration
**Goal**: The pandoc class autoscales in the cluster with production parity to the other six engine classes, and scale-from-zero is live-proven.
**Depends on**: Phase 49 (KEDA cooldown/stabilization and grace-period tuning consume Phase 49's measured `PANDOC_ENGINE_TIMEOUT`)
**Requirements**: PDCP-02
**Success Criteria** (what must be TRUE):
  1. A `pandoc-worker` Deployment plus a KEDA `ScaledObject` ship in the chart with `minReplicaCount:0`, `terminationGracePeriodSeconds`/`scaleDownStabilizationSeconds` derived from the measured `PANDOC_ENGINE_TIMEOUT`, and the WR-01 fail-safe triad (`ignoreNullValues:"false"`, fallback replicas, retry-inclusive PromQL) applied verbatim.
  2. `QueuePandoc` is registered in the always-on api queue-depth collector so KEDA resolves the pandoc backlog at genuinely zero replicas.
  3. Scale-from-zero is live-proven for the pandoc class, capturing timestamped evidence (0→1 on real queue depth, scale back to 0 after drain).
  4. `PANDOC_*` env parity (IN-02 pattern) is confirmed across all queue-client-constructing services, and a pandoc job survives a genuine N→N-1 HPA downscale without a premature SIGTERM.
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
| 44. Async Pipeline Wiring | v1.10 | 4/4 | Complete   | 2026-07-26 |
| 45. Containerization + Measured Timeout Gate | v1.10 | 4/4 | Complete | 2026-07-27 |
| 46. KEDA/Helm Chart Integration | v1.10 | 3/3 | Complete | 2026-07-27 |
| 47. Pandoc Engine Foundation | v1.11 | 3/3 | Complete   | 2026-07-30 |
| 48. Async Pipeline Wiring | v1.11 | 3/3 | Complete   | 2026-07-30 |
| 49. Containerization + Measured Timeout Gate | v1.11 | 3/4 | In Progress|  |
| 50. KEDA/Helm Chart Integration | v1.11 | 0/TBD | Not started | - |

---

*v1.10 (OCR Engine) shipped 2026-07-27, Phases 43-46 — 12/12 requirements, 2 live gates. v1.11 (Pandoc Engine) roadmapped 2026-07-30, Phases 47-50 — 9/9 requirements mapped. Next: `/gsd:plan-phase 47`.*
</content>
