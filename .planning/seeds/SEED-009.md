---
id: SEED-009
status: dormant
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
- own container/binary (**tesseract** OCR or **PaddleOCR** — an evaluation gate, like the CAD SDK question in SEED-005)
- own KEDA scale-from-zero autoscaler
- fail-closed magic-byte content validation (mirror the existing sniffers)
- stage-aware retry classification (mirror `isAVTerminal`)
- RTF-measured engine timeout (mirror the audio/av RTF gates)

Distinct from the reserved `probe` enum value (metadata probing) and from **SEED-007** file-preview/thumbnail (visual preview, not text).

## Scope Estimate

**Large** — a full milestone (foundation → queue/worker/routing → containerization + RTF gate → KEDA/Helm), same shape as v1.7 (audio) and v1.8 (av).

## Open Questions (resolve before scoping)

1. **OCR engine choice** — tesseract (mature, many languages, Apache-2) vs PaddleOCR (better accuracy on some scripts, heavier) vs a cloud API (**rejected** by the offline-engines constraint). An evaluation gate, mirroring SEED-005's CAD-SDK question.
2. **Language/script allowlist** + per-job language opt via validated `opts` (mirror the `AudioOpts` closed allowlist + injection test).
3. **Input scope** — images only first, or also scanned/image-only PDFs (the latter needs a `pdf→image` rasterize pre-stage).
4. **Output formats** — plain `txt`, hOCR/ALTO with bounding boxes, and/or a searchable-PDF overlay.

## Breadcrumbs

- Engine-class pattern precedents: `internal/convert/` (converters, sniffers), `cmd/av-worker`/`cmd/audio-worker`, `internal/queue/queue.go` (per-engine queues), the KEDA/Helm chart, `scripts/*-rtf-measure.sh`.
- Related seeds: SEED-005 (CAD, shares the "OSS binary vs commercial SDK" evaluation gate), SEED-006 (archive), SEED-007 (file-preview — visual, distinct from OCR's text output).
- `AudioOpts` (`internal/convert/audioopts.go`) — the closed-allowlist opts pattern to mirror for the language option.

## Notes

Numbering: SEED-001 resolved (transcription, v1.7/v1.8); SEED-002/003/004 delivered; SEED-005 CAD / SEED-006 archive / SEED-007 file-preview planted; SEED-008 progress delivered in v1.9 — this is SEED-009.
