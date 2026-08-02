---
id: SEED-009
status: implemented
implemented_in: "v1.10 Phases 43-46 (OCR-01..04, OCRO-01, OCRE-01..05, OCRP-01/02) — standalone OCRConverter on Tesseract 5.5.3, page-streaming scan-PDF, txt/hOCR/ALTO, measured OCR_ENGINE_TIMEOUT=744s, KEDA scale-from-zero live-proven; verified 2026-07-27"
planted: 2026-07-25
planted_during: v1.9 Job Progress & Control (raised in Phase 41 discussion)
trigger_when: scoping a milestone that adds a new engine class beyond image/document/html/audio/av
scope: Large
---

# SEED-009: OCR / text-extraction engine class

A new engine class that extracts machine-readable **text** from documents and images — scanned PDFs, images, and photos of text. Raised during the Phase 41 (v1.9) discussion as a natural next engine class.

## Why This Matters

OctoConv already converts *between* formats; OCR adds a genuinely new capability — turning image-only/scanned content into searchable, machine-readable text. It's the highest-demand engine class not yet covered, complements the existing document engine (which handles born-digital docs, not scans), and rounds out the "any input → useful output" story. It follows the proven engine-class pattern, so incremental cost is well-understood.

## When to Surface

**Trigger:** when scoping a milestone that adds a new engine class beyond image/document/html/audio/av.

Follows the proven engine-class pattern established across v1.2–v1.8:
- own asynq queue + dedicated worker binary (`cmd/ocr-worker`)
- own container/binary — the engine itself is a three-way evaluation gate (classic CPU-OCR vs self-hosted GPU VLM vs cloud VLM), see Open Question 1; only the CPU-OCR path (a) fits the current worker infra unchanged
- own KEDA scale-from-zero autoscaler
- fail-closed magic-byte content validation (mirror the existing sniffers)
- stage-aware retry classification (mirror `isAVTerminal`)
- RTF-measured engine timeout (mirror the audio/av RTF gates)

Distinct from the reserved `probe` enum value (metadata probing) and from **SEED-007** file-preview/thumbnail (visual preview, not text).

## Scope Estimate

**Large** — a full milestone (foundation → queue/worker/routing → containerization + RTF gate → KEDA/Helm), same shape as v1.7 (audio) and v1.8 (av).

## Open Questions (resolve before scoping)

1. **Engine class — the decisive evaluation gate (revised 2026-07-26).** NOT simply "tesseract vs PaddleOCR" — it's a three-way choice driven by input quality, the offline constraint, and GPU appetite. "An LLM would obviously be better" is true ONLY for a cloud VLM on hard documents, which the offline-engines constraint currently forbids (same reason a cloud OCR API was pre-rejected). The real options:
   - **(a) Classic CPU-OCR — tesseract / PaddleOCR.** Best fit for *clean scans → faithful text/coords*: offline, deterministic (no hallucination — matches the project's fail-closed philosophy), cheap at scale, gives precise bounding boxes (hOCR/ALTO/searchable-PDF), and slots into the existing CPU-worker pattern (`cpus:2/1g`, KEDA scale-from-zero) with ZERO infra change. **Recommended default** unless the use case is "understand a hard document."
   - **(b) Self-hosted OCR-transformer / VLM — Surya, GOT-OCR2.0, docTR, Qwen2-VL, MiniCPM-V.** Wins on messy/handwritten/photographed input, complex layout→markdown, and semantic extraction; open-weights so it stays offline. BUT it's a heavier engine: **GPU nodes, multi-GB weights, VRAM** — breaks the current CPU-only, cheap, scale-from-zero worker assumption and changes the infra story materially.
   - **(c) Cloud VLM — Claude / GPT-4o / Gemini.** Strongest on the hardest inputs and semantic tasks, but **violates the offline-engines constraint** — only on the table if that constraint is explicitly relaxed. Also reintroduces hallucination/omission risk for a service that promises faithful conversion.

   **Decision drivers to answer first:** (i) input quality — clean scans vs trash/handwriting/photos? (ii) is the offline constraint hard? (iii) is there GPU appetite, or must it stay CPU-only? Clean + offline + CPU-only → (a). Hard/semantic + offline + GPU-ok → (b). Hard/semantic + offline relaxed → (c). The smell test: "faithfully digitize documents" is an OCR *engine* (→ a); "understand a complex document" is a vision *feature* (→ b/c), arguably a different capability than this engine class.
2. **Language/script allowlist** + per-job language opt via validated `opts` (mirror the `AudioOpts` closed allowlist + injection test). Less relevant for a VLM (which is language-agnostic) — another signal that (a) vs (b) changes the whole design.
3. **Input scope** — images only first, or also scanned/image-only PDFs (the latter needs a `pdf→image` rasterize pre-stage).
4. **Output formats** — plain `txt`, hOCR/ALTO with bounding boxes, and/or a searchable-PDF overlay. Bounding-box/coordinate output strongly favors (a); layout→markdown favors (b).

## Progress reporting — reuses the v1.9 infra (page-granular)

OCR is a relatively long process, so online progress (like the av worker's) is wanted — and most of it comes for free. Plan for it up front rather than bolting it on.

**Reused as-is (v1.9 is deliberately engine-agnostic):**
- `Repo.UpdateProgress`/`SetSourceDuration` (non-locking, throttled, freeze-on-abort) — not ffmpeg-specific; just writes `progress_pct`/`eta_seconds`.
- SSE `GET /v1/jobs/{id}/events` (Phase 42) + the opt-in progress webhook — both read `progress_pct` from the job row and DO NOT care what produced it. Zero changes needed to serve OCR progress.
- The `ProgressConverter`/`ProgressSink` seam (Phase 41 D-01) — the OCR converter implements it and calls `sink.Update(...)`.

**New, OCR-specific (the progress *source*):**
- **Unit of progress = PAGES, not time.** OCR has no continuous engine stream (tesseract's CLI emits none). The worker rasterizes the input to N pages and loops page-by-page; progress = `pages_done / total_pages * 100`, driven by the worker's own loop — it does NOT reuse the ffmpeg `-progress`/FIFO parser (that's ffmpeg-only).
- **Granularity is stepwise** (0, 1/N, 2/N, …), not the smooth 0→100 of video. Coarse for a 2–3 page doc — acceptable and honest (better than a fabricated smooth ramp; matches Phase 41's "never fabricate" rule).
- **Single image → `null`→100** (atomic, no sub-progress). Multi-page scan/PDF → a real ramp. Progress is opt-in per engine (Phase 41 D-02).
- **ETA** from `avg_seconds_per_page × pages_remaining` (same EMA/warm-up idea as av, denominator is sec/page instead of ffmpeg `speed=`).

**Registry tie-in (SEED-010):** a capability could declare "emits progress" so the API/SSE knows to expect a ramp without a hardcode. This OCR case is exactly the "generalized progress bus" Phase 41's CONTEXT flagged as out-of-scope-at-the-time — OCR is the natural second progress source that generalizes it.

## Breadcrumbs

- Engine-class pattern precedents: `internal/convert/` (converters, sniffers), `cmd/av-worker`/`cmd/audio-worker`, `internal/queue/queue.go` (per-engine queues), the KEDA/Helm chart, `scripts/*-rtf-measure.sh`.
- Progress infra to reuse: `internal/jobs/repo.go` (`UpdateProgress`/`SetSourceDuration`), `internal/api/sse.go` (`handleJobEvents`), `internal/worker/worker.go` (`progressSink`/`fireDetached`), `internal/convert/progress.go` (`ProgressConverter`/`ProgressSink`/`etaEstimator`) — all engine-agnostic since v1.9.
- Related seeds: SEED-005 (CAD, shares the "OSS binary vs commercial SDK" evaluation gate), SEED-006 (archive), SEED-007 (file-preview — visual, distinct from OCR's text output).
- `AudioOpts` (`internal/convert/audioopts.go`) — the closed-allowlist opts pattern to mirror for the language option.

## Notes

Numbering: SEED-001 resolved (transcription, v1.7/v1.8); SEED-002/003/004 delivered; SEED-005 CAD / SEED-006 archive / SEED-007 file-preview planted; SEED-008 progress delivered in v1.9 — this is SEED-009.
