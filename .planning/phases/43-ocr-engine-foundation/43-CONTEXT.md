# Phase 43: OCR Engine Foundation - Context

**Gathered:** 2026-07-26
**Status:** Ready for planning

<domain>
## Phase Boundary

A standalone `OCRConverter` extracts plain text (txt) and word-coordinate output (hOCR/ALTO) from images and scanned/image-only PDFs via **Tesseract**, built and testable directly against the binary — memory-bomb-safe (streaming page rasterize + page-count cap) and born-digital-PDF-safe from the first line — BEFORE any queue/worker plumbing. Covers OCR-01/02/03/04, OCRO-01, OCRE-02, OCRE-03. Mirrors the Audio Phase 30 / AV Phase 34 scope fence: the converter is NOT registered into `convert.Default` and NOT wired to a worker this phase (that is Phase 44).

**In scope:** `OCRConverter` (Pairs/Engine/Convert + the v1.9 `ProgressConverter` seam), image→txt, streaming PDF→txt (`pdftoppm` per page), hOCR/ALTO via a real XML encoder, `OCROpts` (language allowlist), the page-count cap + born-digital detection guards, all tested directly against the real Tesseract/poppler binaries.
**Out of scope:** worker/queue/container/KEDA/migration (Phase 44-46), registration into `convert.Default`, deskew/denoise (v2), searchable-PDF (v2), VLM path (v2), the dynamic registry (SEED-010 dormant).
</domain>

<decisions>
## Implementation Decisions

### Output model (OCR-01/03)
- **D-01:** **Single-target, one output per job** — the client picks `target ∈ {txt, hocr, alto}`, mirroring `AudioConverter`'s `{txt,srt,vtt,json}` Pairs shape. This keeps the worker's hardcoded single `AddOutput` call (shared by all 5 engines) UNCHANGED. **ALTO ships in v1** as a distinct `Pairs()` target (near-free once hOCR exists — same Tesseract config, different configfile). Rejected: multi-output (one job emitting txt+hOCR together) — would force extending `process()`'s single-output contract that every engine shares. Note: ROADMAP SC3's "the same input also produces hOCR" is resolved to mean "the SAME image path, run with a different target, produces hOCR" — NOT one job emitting both.

### Engine eval-gate depth (OCR-01, PSM/OEM)
- **D-02:** **Lock Tesseract** — the research eval already decided it (native hOCR/ALTO, ~1-3s/page CPU, zero new Go deps / CGO_ENABLED=0 fit, ~20MB image; PaddleOCR pulls a Python/ML stack that contradicts the CPU-only + zero-dependency contour and only wins on GPU/messy input which is the out-of-scope VLM path). The Phase-43 "eval-gate" is therefore NOT a tesseract-vs-Paddle bake-off; it is spent on **pinning PSM/OEM against real scanned-document fixtures** (SC1) — resolving the actual accuracy levers, not re-litigating the engine.

### Born-digital PDF detection + policy (OCRE-03)
- **D-03:** Detect via **`pdftotext`** (poppler, already required for the rasterize path — one tool, no new dep): extract text and if the non-whitespace character count exceeds a small threshold, the PDF is treated as born-digital and **rejected fail-closed (422)**. Policy is **conservative — reject on ANY substantial extractable text** (not "only if every page has text"): the v1 scope is *image-only* PDF; a mixed/partial-text PDF is ambiguous and is rejected fail-closed, with the client steered to the document engine. Rejected: `pdffonts`/font-presence (false-positives on a PDF carrying one decorative font but no real text); the lenient "reject only if ALL pages have text" (complicates the contract + implies a mixed-output OCR path that's out of scope).

### PSM / OEM (OCR-01)
- **D-04:** **Fixed server defaults: PSM 3 (fully-automatic page segmentation, the standard for full-page documents) + OEM 1 (LSTM).** PSM is **NOT a per-job opt in v1** — exposing it is a footgun (wrong PSM silently wrecks results). The exact PSM/OEM pair is confirmed against the scan fixtures in the eval-gate (D-02) and may be adjusted there, but stays a server-side constant, not client-controlled. (A per-job PSM opt is a possible v2 addition behind a closed mode allowlist.)

### Language options (OCRO-01)
- **D-05 (locked by recommendation, unopposed):** `OCROpts{Language}` closed allowlist day-one = **`eng`, `rus`** (Russian-first company, mirrors `AudioOpts`'s allowlist discipline), combinable as `eng+rus`; extended per real client demand. Validated via the shared strict-parse helper; an unlisted code → 422 before any work; an injection test proves client bytes never reach Tesseract's `-l` argv unvalidated. Only `eng` + `rus` `.traineddata` are baked in day-one (STACK: `tessdata_fast`, SHA-pinned) — Phase 45 sizes the container.
- **D-05a (AMENDED 2026-07-26 after plan-check):** **OSD / auto-orientation is DEFERRED to v2 (OCR-V2-05), NOT built in Phase 43.** Rationale: the locked PSM 3 (D-04) is by definition "no OSD"; auto-orientation is NOT free — it requires a distinct per-page `--psm 0` orientation pre-pass + rotation logic (an extra subprocess per page). That places it in the SAME "preprocessing-for-accuracy, add only on measured evidence" bucket as deskew/denoise (already v2, OCR-V2-03) — deferring it keeps D-05 consistent and v1 lean. (Supersedes the original D-05 "OSD enabled" clause.)

### Claude's Discretion
- Exact non-whitespace char threshold for born-digital detection; the exact DPI computation from the PDF page box (~300 target); the hOCR-vs-ALTO Tesseract configfile mechanics; the XML encoder choice (stdlib `encoding/xml`); the per-page temp-file lifecycle (create→OCR→delete before next page); the `ProgressConverter` page-loop wiring detail — all executor/planner's call, provided: streaming one-page-at-a-time (never whole-PDF render), a real XML encoder (never string concat), the fail-closed guards fire BEFORE any render, and the opts allowlist + injection discipline hold.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The engine-class shape to mirror (Audio Phase 30 / AV Phase 34 fence)
- `internal/convert/whisper.go` — `AudioConverter` (Pairs/Engine/Convert + the `{txt,srt,vtt,json}` single-target output shape D-01 mirrors; the "standalone, unregistered this phase" fence).
- `internal/convert/audioopts.go` — the `AudioOpts` closed-allowlist + strict-parse + injection-safety pattern `OCROpts{Language}` (D-05) copies.
- `internal/convert/av.go` — `AVConverter` (self-contained guard stage BEFORE the expensive subprocess; the `ConvertWithProgress` implementation D-01/OCR-04 mirrors); `avProbeSource`-style "probe once, decide once".

### Progress reuse (OCR-04, zero-touch)
- `internal/convert/progress.go` — `ProgressConverter`/`ProgressSink` optional-interface seam (:39-48); OCR implements `ConvertWithProgress` and calls `sink.Update(pages_done/total)` from its page loop; it must NOT touch the ffmpeg-specific `progressBlockParser`/`etaEstimator` — a page-based ETA estimator is new here.

### The guards to mirror (OCRE-02)
- `internal/convert/dimensions.go` — the closed `dimensionParsers` dispatch + declared-dimension bound (the `MAX_IMAGE_PIXELS`/decompression-bomb precedent the PDF page-count cap + image-dimension cap mirror, enforced pre-render).
- `internal/convert/exec.go` — `runCommand` (Setpgid+SIGKILL+timeout) — both new untrusted processes (`pdftoppm`, `tesseract`) run through it (OCRE-05, Phase 44 wires it; foundation uses it directly for standalone tests).

### Sniffing (OCRE-01, Phase 44 but shaped here)
- `internal/convert/sniff.go` — the closed `signatures` table (:49); a new `matchPDF` (`%PDF-`) entry slots in with zero new branching (existing image sniffers already cover jpg/png/tiff/webp inputs).

### Requirements + research
- `.planning/REQUIREMENTS.md` — OCR-01..04, OCRO-01, OCRE-02, OCRE-03 (this phase's IDs).
- `.planning/research/SUMMARY.md` + `STACK.md` (Tesseract 5.5.3 + poppler `pdftoppm`/`pdfinfo`/`pdftotext`, tessdata_fast) + `ARCHITECTURE.md` (integration + build order) + `PITFALLS.md` (memory-bomb, preprocessing, PSM, born-digital, empty-output-is-done).
- `.planning/seeds/SEED-009.md` — the OCR seed (progress-reuse section, open questions) and SEED-010 (registry, DORMANT — do not build).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `AudioConverter`/`AVConverter` — the exact standalone-converter shape (Pairs/Engine/Convert + guard-before-subprocess + single-target output) to clone.
- `progress.go` `ProgressConverter`/`ProgressSink` — engine-agnostic since v1.9; OCR is the second implementor (av was first). Page-based ETA estimator is the only new progress code.
- `AudioOpts` strict-parse + closed allowlist + injection test — `OCROpts` copies it verbatim (Language instead of language+the whisper set).
- `runCommand` (exec.go) — hardened exec for pdftoppm/pdfinfo/pdftotext/tesseract.
- `dimensions.go` — the pre-render bound precedent for the page-count + image-dimension caps.

### Established Patterns
- Guard-before-expensive-subprocess (duration/resolution/disk guards in av.go; dimension guard) → the page-count cap + born-digital detect run BEFORE any pdftoppm render.
- Closed detection/allowlist tables (sniff signatures, dimensionParsers, audio allowlist) → language allowlist + (Phase 44) matchPDF.
- `Set*`-at-startup process-global config → any OCR env knob (Phase 45), not this phase.

### Integration Points
- This phase is self-contained in `internal/convert/` (new `ocr.go`/`ocropts.go` + tests). No `internal/worker`/`internal/api`/`internal/queue`/`internal/db` touch (Phase 44). No `convert.Default` registration (Phase 44). Tests invoke the real Tesseract + poppler binaries directly (skip-if-absent, like the av real-ffmpeg tests).
</code_context>

<specifics>
## Specific Ideas

- Streaming discipline (SC2): for a PDF, loop `pdfinfo` (page count → cap check) → for each page `pdftoppm -f N -l N -r <dpi>` to a temp PNG → tesseract that page → **delete the page image before the next iteration**. Never render all pages up front (the memory bomb).
- hOCR/ALTO built via `encoding/xml` (or Tesseract's own hocr/alto configfiles which already emit valid XML) — the round-trip test (SC3) feeds OCR'd text containing `&`/`<`/`>` and asserts the output still parses as valid XML.
- Empty/no-text input completes as `done` with empty txt (NOT `failed`) — mirrors the accepted whisper hallucination-on-silence stance (OCRE-05, contract set here, worker-tested Phase 44).
- Real-binary tests gated behind a `testing.Short()`/binary-presence skip so `go test -short` stays hermetic (like the av real-ffmpeg tests).
</specifics>

<deferred>
## Deferred Ideas

- Detect-and-passthrough for born-digital PDF (extract the existing text layer instead of 422) → v2 (OCR-V2-01).
- Searchable-PDF overlay output → v2 (OCR-V2-02).
- Deskew/denoise preprocessing (unpaper/leptonica) → v2, evidence-gated on measured accuracy (OCR-V2-03).
- Per-job PSM opt (closed mode allowlist) → possible v2.
- Self-hosted VLM path (GPU) → v2 (OCR-V2-04).
- Dynamic capability registry (SEED-010) → dormant; OCR uses compile-time `Registry.Register` in Phase 44.
</deferred>

---

*Phase: 43-ocr-engine-foundation*
*Context gathered: 2026-07-26*
