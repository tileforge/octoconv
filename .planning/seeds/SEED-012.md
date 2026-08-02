---
id: SEED-012
status: implemented
implemented_in: "v1.11 Phases 47-50 (PDOC-01/02, PDCO-01, PDCE-01..04, PDCP-01/02) — PandocConverter over a closed markup allowlist (no ->pdf, deliberately: LibreOffice/chromium own that), TeX-free pinned image, measured PANDOC_ENGINE_TIMEOUT=65s, KEDA scale-from-zero + downscale-survival live-proven; verified 2026-08-02"
planted: 2026-07-26
planted_during: post-v1.9 (worker-ideas brainstorm)
trigger_when: scoping a milestone that adds a new engine class beyond image/document/html/audio/av/ocr
scope: Large
---

# SEED-012: Pandoc engine class (markup interconvert)

A markup-conversion engine built on **pandoc**: markdown ↔ html ↔ docx ↔ pdf ↔ rst/org/latex/epub. The Swiss-army knife of document conversion — more general and more useful than a niche one-off like `tex2pdf` (which it subsumes).

## Why This Matters

Internal services routinely need "turn this markdown/rst into a PDF/docx" or "convert these docs between markup formats." The existing document engine (LibreOffice) handles office binaries and the html engine (Chromium) does HTML→PDF, but neither covers the markdown/rst/org/latex/epub markup family. Pandoc fills exactly that gap with one binary. Strong fit for the engine-class pattern.

## When to Surface

**Trigger:** scoping a new engine class. Follows the proven pattern: own queue `pandoc`/`markup` + `cmd/pandoc-worker` + container (pandoc + a PDF engine like tectonic/wkhtmltopdf/weasyprint for the →pdf path) + KEDA, fail-closed content validation, stage-aware retry, RTF-measured timeout.

## Open Questions

1. **PDF backend** — pandoc needs an external LaTeX/HTML engine for →pdf (tectonic vs weasyprint vs wkhtmltopdf). Same "which binary" eval-gate as OCR/CAD. tectonic is self-contained but pulls a TeX distribution; weasyprint is HTML/CSS-based (lighter, no TeX).
2. **Format pair matrix** — pandoc supports dozens of formats; pick a closed, validated `(from→to)` allowlist (mirror the existing Pairs() sets), not "anything pandoc accepts."
3. **Security** — pandoc's raw-HTML/raw-TeX passthrough + any LaTeX backend reintroduce shell-escape/`\input` risks; disable raw passthrough + `-no-shell-escape`, hardened `runCommand`, no network.
4. **Overlap** — decide the boundary vs the existing document/html engines (don't duplicate docx→pdf that LibreOffice already does; pandoc owns the markup-family pairs).

## Scope Estimate

**Large** — a full engine-class milestone (foundation → queue/worker/routing → container + RTF gate → KEDA/Helm), same shape as audio/av/OCR.

## Breadcrumbs

- Engine-class precedents: `internal/convert/` (Pairs/Converter/sniffers), `cmd/*-worker`, `internal/queue/queue.go`, the KEDA/Helm chart.
- Subsumes the `tex2pdf` illustration from SEED-010 (LaTeX→PDF becomes one pandoc pair, not a dedicated worker).
