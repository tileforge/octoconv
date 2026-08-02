# Phase 43 Discussion Log

**Date:** 2026-07-26
**Mode:** discuss (default)

## Framing

OCR Engine Foundation — standalone `OCRConverter` (Tesseract), unregistered fence like Audio-30/AV-34. Carried forward without re-asking (research + ROADMAP SC + code scout): Tesseract as strong default; streaming `pdftoppm` @~300 DPI with delete-between-pages (memory-safe); progress via the v1.9 `ProgressConverter` seam (zero touch); hOCR via a real XML encoder (not string concat); page-count cap like `MAX_IMAGE_PIXELS`; `OCROpts` allowlist + injection test like `AudioOpts`; `matchPDF` slots into the closed sniff table; converter not registered this phase.

## Gray areas presented (all 4 selected)

**A. Output model** — single-target vs multi-output; ALTO v1 vs v2.
- **Decision (D-01):** single-target `{txt,hocr,alto}` (one output/job, mirrors AudioConverter, worker single-AddOutput unchanged); ALTO ships v1 (near-free after hOCR). SC3 resolved as "same image, different target → hOCR", not one-job-both.

**B. Eval-gate depth** — real tesseract-vs-Paddle bake-off vs lock-Tesseract.
- **Decision (D-02):** lock Tesseract (research already decided; Paddle's Python/ML stack contradicts CPU-only/zero-dep and only wins on the out-of-scope GPU/VLM path); the eval-gate is spent pinning PSM/OEM on real scan fixtures.

**C. Born-digital detection + policy** — tool + mixed-PDF edge.
- **Decision (D-03):** `pdftotext` → non-whitespace char count > threshold → 422; conservative reject on ANY substantial text (scope = image-only PDF; mixed → document engine). Rejected pdffonts (false-positive) and the lenient all-pages rule.

**D. PSM/OEM + languages** — fixed vs per-job; allowlist.
- **Decision (D-04):** fixed server default PSM 3 + OEM 1, NOT a per-job opt in v1 (footgun); confirmed on fixtures in the eval-gate but stays a server constant.
- **Decision (D-05, locked by recommendation, unopposed):** language allowlist day-one = `eng`, `rus` (combinable `eng+rus`), extensible; OSD/auto-orientation on; only eng/rus/osd `.traineddata` baked day-one.

## Deferred

- Born-digital passthrough, searchable-PDF, deskew, per-job PSM, VLM path → v2.
- Dynamic registry (SEED-010) → dormant; compile-time Register in Phase 44.
