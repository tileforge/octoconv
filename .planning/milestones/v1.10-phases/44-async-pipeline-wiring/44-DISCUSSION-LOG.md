# Phase 44 Discussion Log

**Date:** 2026-07-26
**Mode:** discuss (default)

## Framing

Async Pipeline Wiring — mechanical clone of audio-31/av-35. Carried forward: HandleOCRConvert (clone HandleAVConvert + progressSink + cancel), own `ocr` queue, register in converters.go init, migration 0009 (`ocr` enum), matchPDF in sniff, hardened runCommand ×2, OCRUniqueTTL from own budget; progress/SSE/webhook UNMODIFIED.

## Key finding (de-risks routing)

`EngineFor(from, to)` already takes source AND target (`convert.go:97`), `handleCreateJob` calls `EngineFor(detected, target)` (`handlers.go:356`) → routing is already (source+target). Registering OCR pairs + adding matchPDF is all that's needed; jpg→png=image, jpg→txt=ocr disambiguate automatically. No new routing code.

## Gray areas presented (all 3 selected)

**A. Routing image→OCR** — existing EngineFor vs explicit engine-hint.
- **Decision (D-01):** existing `EngineFor(from,to)` + register OCR pairs + matchPDF; no client engine-hint (breaks uniformity, duplicates registry).

**B. operation + retry classification.**
- **Decision (D-02):** `jobs.operation='convert'` (consistent with audio transcript; no operation migration).
- **Decision (D-04, locked by recommendation, unopposed):** `isOCRTerminal` — input/client faults (born-digital, too-many-pages, page-box/image-too-large, unknown-language, unsupported-target) = TERMINAL; timeout = TRANSIENT; other engine crashes = TERMINAL (deterministic input). OCRUniqueTTL from OCR's own budget (IN-02).

**C. Upload limit.**
- **Decision (D-03):** 100 MiB (same as image), env-tunable; work separately bounded by the page-count cap.

## Deferred

- Container/timeout → Phase 45; KEDA → Phase 46; registry SEED-010 dormant; passthrough/searchable-PDF/deskew/OSD → v2.
