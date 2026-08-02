# Feature Research

**Domain:** OCR / text-extraction engine class (classic CPU-OCR — tesseract/PaddleOCR) for OctoConv v1.10
**Researched:** 2026-07-26
**Confidence:** HIGH (mechanics of tesseract/hOCR/ALTO/PDF text-layer detection — official docs + multiple corroborating sources) / MEDIUM (accuracy-uplift numbers from preprocessing — vendor blog consensus, not a controlled study)

**Note:** this replaces the FEATURES.md previously written for the v1.9 (Job Progress & Control) milestone research — that content is preserved in git history (`git log -p -- .planning/research/FEATURES.md`) but no longer reflects the current milestone's scope.

## How Classic CPU-OCR Works (context for the table below)

A classic OCR engine (tesseract, PaddleOCR CPU mode) is a **pixel-in, text-out** pipeline with no document understanding:

1. **Page segmentation (PSM)** — the engine decides where blocks/lines/words are on the image. Tesseract defaults to "assume a full page of text" (PSM 3); a single cropped word/line/uniform block needs a different PSM (6/7/8) or output degrades badly. [Tesseract Command-Line docs](https://tesseract-ocr.github.io/tessdoc/Command-Line-Usage.html)
2. **Recognition** — per-language trained models (`tessdata`) turn glyphs into characters/words. Language is a build/runtime asset, not an algorithm choice — each language needs its own `.traineddata` file.
3. **Output emission** — tesseract can emit `txt`, `hocr`, `alto`, `pdf` (invisible-text-over-image), `tsv` in one invocation (`tesseract image.png demo alto hocr pdf txt`), each carrying different fidelity: `txt` = words only; `hocr`/`alto`/`tsv` = word/line bounding boxes + per-word confidence. [Tesseract Command-Line Usage](https://tesseract-ocr.github.io/tessdoc/Command-Line-Usage.html), [tesseract.1.asc](https://github.com/tesseract-ocr/tesseract/blob/main/doc/tesseract.1.asc)
4. **No layout/semantic reasoning** — tesseract does not know "this is a table" or "this is a heading"; it only knows glyph→word→line→block geometry. Anything resembling document *understanding* (markdown reconstruction, reading order across multi-column layouts, table structure) is a differently-shaped problem, solved today by vision-language models (VLMs), not classic OCR. This is the crux of the SEED-009 "(a) vs (b)/(c)" fork and why layout→markdown is an anti-feature here.
5. **Preprocessing sensitivity** — classic OCR is a pattern-matcher highly sensitive to skew, low contrast, noise, and off-spec DPI. Preprocessing (deskew, binarize, DPI-normalize to ~300 DPI) is reported to lift accuracy 15–30% on marginal scans; sub-200 DPI or skewed input is a primary real-world failure mode, not an edge case. [Docparser preprocessing guide](https://docparser.com/blog/improve-ocr-accuracy/), [Markaicode production tesseract guide](https://markaicode.com/tutorial/tesseract-tutorial-production-setup-guide/)

## Feature Landscape

### Table Stakes (Users Expect These)

Features an internal client integrating "yet another OctoConv engine class" will assume exist, because every other engine class already has the equivalent.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Single image → txt | Baseline "OCR" contract; every competing OCR tool does this | LOW | Straight tesseract/PaddleOCR CLI invocation via existing `runCommand` hardened-exec pattern |
| Scanned/image-only multi-page PDF → txt (per-page, concatenated) | Locked scope; scanned PDFs are the dominant real-world OCR input (invoices, contracts, forms) | MEDIUM | Needs a `pdf→image` rasterize pre-stage (new dependency: a PDF rasterizer CLI, e.g. `pdftoppm`/`pdftocairo` from poppler-utils, or vips-based) before the per-page OCR loop |
| Language/script selection via validated `opts` | Every text-producing engine class (audio: `AudioOpts` closed allowlist) exposes this; OCR is *more* language-sensitive than whisper because tesseract requires the matching `.traineddata` to be baked into the image | LOW–MEDIUM | Mirror the `AudioOpts` pattern exactly: closed allowlist, injection-tested. Complexity driver is deciding which languages to bake into the container (each is a multi-MB asset) |
| hOCR output (word/line bounding boxes) | Locked scope; this is the differentiator over plain-text-only OCR tools and what makes OctoConv's OCR "structured," not just "dump some text" | LOW | Native tesseract config (`hocr`); effectively free once txt output exists — same invocation, extra output file |
| Page-granular progress + ETA (reusing v1.9 infra) | Every long-running engine class since v1.7 (audio RTF, av ffmpeg `-progress`) reports progress; a silent multi-minute multi-page OCR job would be a regression vs. the rest of the product | MEDIUM | v1.9 progress plumbing (`Repo.UpdateProgress`, SSE, webhook) is engine-agnostic and reused as-is; the *new* work is purely the OCR-side progress **source**: pages_done/total_pages loop-driven updates, not a continuous stream (tesseract's CLI has no `-progress`-equivalent) |
| Fail-closed magic-byte validation of OCR input | Every existing engine class validates content by magic bytes before touching S3/Postgres (image/document/audio/av all do this) | LOW–MEDIUM | Mirror existing sniffers for image formats (already exist); PDF sniffing exists too (document engine) but OCR needs an *additional* PDF-specific check — see "born-digital PDF" decision below |
| Full engine-class control plane parity (own queue, `cmd/ocr-worker`, stage-aware retry, RTF-measured timeout, KEDA scale-from-zero) | This is the established pattern for every engine class since v1.2; deviating would be inconsistent and would surprise operators | HIGH | Mechanical but non-trivial — same shape as v1.7/v1.8, this is the bulk of the milestone's actual engineering cost, not the OCR algorithm itself |
| Image preprocessing (deskew, binarize, DPI-normalize) before recognition | Without it, real-world scans (photocopies, faxed docs, phone photos routed through the image engine) will silently produce garbage text with no error — this is an accuracy *floor*, not a nice-to-have. Classic OCR's reputation for being "bad" is overwhelmingly a preprocessing gap, not an engine limitation | MEDIUM | Treat as table stakes, not a differentiator: an OCR engine that skips this will produce embarrassing results on exactly the inputs (scanned/photographed documents) this milestone targets. Can be delegated to the rasterize pre-stage (render PDF pages at a fixed target DPI) plus a lightweight binarize/deskew step (many implementations use OpenCV or ImageMagick; tesseract itself does *some* internal Otsu binarization but benefits measurably from a cleaner input) |

### Differentiators (Competitive Advantage)

Features that go beyond "does OCR" and are where OctoConv's OCR engine can be genuinely more useful than a naive tesseract wrapper — while staying inside the classic-CPU-OCR fence.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| ALTO output (in addition to hOCR) | ALTO is the de facto standard in library/archival/GLAM digitization pipelines; some downstream consumers (search indexers, archival systems) expect ALTO specifically rather than hOCR | LOW | Same tesseract invocation family as hOCR (`tesseract img out alto`) — nearly free once the hOCR path exists; mainly a "which config files to pass" decision |
| Per-word confidence scores surfaced in the API response (not just embedded in hOCR/ALTO XML) | Lets a calling service programmatically flag low-confidence pages/words for human review, without parsing XML client-side — a real differentiator vs. "here's a text blob" competitors | MEDIUM | Tesseract exposes per-word confidence via TSV output; requires parsing TSV (or hOCR's `x_wconf` attribute) worker-side and either aggregating (page-level mean confidence) or attaching structured word-confidence data to the job result |
| Automatic orientation/script detection (OSD) before recognition | Real-world scans/photos are frequently rotated 90/180/270°; auto-correcting orientation before OCR meaningfully improves accuracy on the "photo of a document" input class without any client-side hinting | MEDIUM | Tesseract ships OSD as a separate mode; adds one extra pass per page (cost vs. accuracy tradeoff — relevant to the RTF timeout gate) |
| Mixed-language per-job selection (e.g., `eng+rus` in one document) | Real internal documents are frequently bilingual (e.g., localized contracts); restricting to a single language per job would be a real limitation the audio engine doesn't have to the same degree | LOW–MEDIUM | Tesseract natively supports `-l eng+rus`-style combined language packs; the opts-validation work is deciding which *combinations* are in the closed allowlist (combinatorial growth vs. audio's flat list) |
| Detect-and-passthrough for born-digital PDFs with an existing text layer (see decision below) | Faster and strictly more faithful than pixel-OCR-ing text that's already machine-readable; avoids introducing OCR errors into already-correct text | MEDIUM–HIGH | This is the flagged decision — see "Born-Digital PDF Text Layer" section below. Elevated to differentiator-or-anti-feature pending an explicit scope decision, not silently assumed |

### Anti-Features (Explicitly Out of Scope for This Milestone)

Features that look like natural OCR extensions but are excluded by the locked "classic CPU-OCR, offline, CPU-only" scope. Listed explicitly so they don't creep back in during requirement definition.

| Feature | Why It Looks Tempting | Why It's Out of Scope Here | Alternative |
|---------|------------------------|------------------------------|-------------|
| Semantic/layout understanding + layout→markdown reconstruction (tables, headings, reading order across multi-column layouts) | "Just OCR it and give me clean markdown" is the single most common ask once any OCR feature ships | This is fundamentally a vision-language-model (VLM) capability, not a classic-OCR capability — tesseract/PaddleOCR CPU output is glyph/word/line geometry with zero document-structure reasoning. Bolting markdown reconstruction on top of hOCR geometry (columns, tables) is a large, failure-prone heuristic layer, effectively re-implementing part of a VLM badly | Explicitly out of scope per SEED-009 option (b)/(c); if genuinely needed later, it is a *different engine class* (GPU VLM, offline-model-permitting) discussed and rejected for this milestone, not a mode of the CPU-OCR engine |
| Handwriting recognition (HTR) | "OCR" colloquially implies "read any text in an image, including handwriting" | Tesseract is well-documented as weak on handwriting (its LSTM models are trained overwhelmingly on printed text); shipping a "handwriting OCR" feature on tesseract would produce silently poor, unreliable output — a fail-*open* failure mode this project's philosophy (fail-closed, deterministic, no hallucination) explicitly rejects | Not addressed by this engine class at all; a future handwriting-specific engine (HTR model, GPU-class) would be a separate SEED, matching the same (a)/(b)/(c) fork as SEED-009 itself |
| Cloud OCR/VLM API fallback for "hard" documents the CPU engine can't handle | Tempting escape hatch when a client complains about a low-quality scan | Directly violates the hard offline-engines constraint (same reason cloud OCR APIs were pre-rejected in SEED-009); also reintroduces hallucination/omission risk this project explicitly avoids everywhere else (webhooks, retries, fail-closed validation) | None within this milestone — document the limitation; a relaxed-offline-constraint cloud path is a distinct, not-yet-approved future decision |
| Searchable-PDF overlay output (invisible OCR text layer burned back into a PDF) | Natural-sounding "give me back a searchable PDF," and tesseract can emit this format almost for free (`tesseract img out pdf`) | Locked output scope for this milestone is txt + hOCR/ALTO only — adding a third output format is scope growth beyond what was locked, and searchable-PDF reconstruction has its own correctness surface (page sizing/DPI matching, embedding fonts) not yet designed | Defer to a follow-up if a concrete client need materializes; hOCR/ALTO already carry the coordinate data needed to build this later without re-architecting |
| Blind pixel-OCR of every PDF page regardless of whether it already has an extractable text layer | Simplest implementation — treat every input PDF as "rasterize + OCR," no branching logic | Wastes compute on already-digital documents and, more importantly, can produce OCR text that is *worse* than the existing embedded text layer (introduces character-recognition errors into content that was already 100% faithful) — see decision below | Detect and reject-or-passthrough (decision required — see next section) |

## Born-Digital PDF Text Layer — Flagged Decision

**The concrete question:** the locked input scope is "images + scanned/image-only PDF." A born-digital PDF (produced by Word/LaTeX/a typesetting tool, or already OCR'd previously) is not "image-only" — it already has a real, extractable text layer. What should the OCR engine do if a client submits one?

**Detection is cheap and well-understood.** Standard signal: check whether the PDF's pages contain extractable text objects (not just images) — e.g. `pdftotext`/`pdffonts`-style inspection, or checking whether a page's content stream has zero/near-zero extractable characters while containing one full-page image XObject ("only_images" heuristic). This is a solved, well-documented pattern in the OCR-pipeline ecosystem — OCRmyPDF is the reference implementation of exactly this skip/force/redo logic. [OCRmyPDF](https://github.com/jbarlow83/OCRmyPDF)

**Three real options, all technically feasible:**

1. **Fail-closed reject (422)** — detect a meaningful existing text layer at content-validation time (mirrors the project's existing fail-closed philosophy: OLE-CFB rejection, decompression-bomb rejection) and return a clear error directing the client elsewhere (the document engine, or `pdftotext` client-side). Lowest engineering cost, most consistent with "input = image-only PDF" as literally locked, and consistent with this project's general bias toward explicit rejection over silent best-effort behavior.
2. **Detect-and-passthrough** — if a text layer is detected, extract it directly (bypass pixel-OCR entirely) and return it as the OCR result. Strictly more faithful and faster than OCR-ing an already-digital page, but quietly expands this engine class's actual capability beyond "OCR" into "PDF text extraction," and needs its own confidence/output-format story (a directly-extracted text layer has no OCR confidence scores or hOCR word geometry — what goes in those fields?).
3. **Blind OCR regardless (do nothing)** — simplest to implement, but as noted in the anti-features table, produces strictly worse results than option 2 and wastes a full OCR pass compared to option 1 — dominated by both other options and not recommended.

**Recommendation for requirement definition:** option 1 (fail-closed reject) is the lower-risk, scope-preserving default — it matches the locked "scanned/image-only PDF" framing exactly, avoids inventing a second output contract (extracted-text vs. OCR'd-text) for what is supposed to be one engine class, and is consistent with the project's established pattern of explicit, loud rejection over silent scope-widening (OLE-CFB precedent). Option 2 is a legitimate differentiator worth a deliberate future decision, but should not be assumed as in-scope by default — it changes what "OCR job" means and needs its own opts/output-format design. This should be explicitly decided (not left implicit) during requirement definition, since a naive implementation left undecided defaults to option 3, which is the worst of the three.

## Feature Dependencies

```
Language/script opts (validated allowlist)
    └──requires──> tessdata language assets baked into ocr-worker image
                       └──constrains──> which languages are offerable (image size vs. language coverage tradeoff)

Scanned/image-only PDF input
    └──requires──> pdf→image rasterize pre-stage
                       └──requires──> per-page OCR loop
                                          └──enables──> page-granular progress (pages_done/total_pages)

hOCR/ALTO output
    └──requires──> per-word bounding-box + confidence data from the engine (native tesseract capability, not an add-on)

Per-word confidence surfaced in API response
    └──requires──> hOCR/ALTO output (or raw TSV) as an intermediate parse target

Image preprocessing (deskew/binarize/DPI-normalize)
    └──enhances──> single-image OCR accuracy
    └──enhances──> scanned-PDF-page OCR accuracy (post-rasterize)

Born-digital PDF detection
    └──gates──> whether "scanned/image-only PDF" input validation is fail-closed or passthrough
    └──conflicts with──> "blind OCR every PDF page" (mutually exclusive design choices)

Layout/semantic understanding, layout→markdown ──conflicts──> classic CPU-OCR engine class
    (different capability class entirely — VLM, not this milestone)

Handwriting recognition ──conflicts──> tesseract/PaddleOCR CPU engine
    (known weak point of the chosen engine family, not addressable within this scope)
```

### Dependency Notes

- **Scanned/image-only PDF requires a rasterize pre-stage:** this is a new pipeline stage not present in any existing engine class (image/document/html/audio/av all operate on their native format directly) — it's the single largest new piece of plumbing in this milestone besides the OCR call itself, and it's what makes page-granular progress possible (pages_done/total_pages only exists once rasterization produces a page count up front).
- **hOCR/ALTO output is nearly free once txt output exists:** tesseract emits all four formats from one invocation with different config-file arguments — this is a low-complexity feature despite being locked-scope, and should not be treated as separately risky from plain-text output.
- **Born-digital PDF detection gates the input-validation design, not the OCR algorithm:** this decision belongs at the fail-closed content-validation layer (same place as existing magic-byte sniffers), before the job even reaches the rasterize/OCR pipeline — get it wrong and every born-digital PDF either silently wastes a full OCR pass or silently gets rejected without a clear reason.
- **Layout/markdown and handwriting both conflict with the chosen engine family**, not just "not implemented yet" — this is why they're anti-features rather than deferred differentiators: shipping them on tesseract/PaddleOCR would require either accepting poor quality or effectively building a different engine class in disguise.

## MVP Definition

### Launch With (v1 of this milestone)

- [ ] Single image (jpg/png/tiff/webp) → txt — core contract, locked scope
- [ ] Scanned/image-only PDF → per-page txt (concatenated) — locked scope, needs rasterize pre-stage
- [ ] hOCR output alongside txt — locked scope, low incremental cost once txt path exists
- [ ] Language/script selection via validated `opts` (closed allowlist, mirrors `AudioOpts`) — locked scope
- [ ] Page-granular progress + ETA reusing v1.9 infra — locked scope, differentiator vs. a silent long-running job
- [ ] Deskew/binarize/DPI-normalize preprocessing before recognition — treated as table stakes here, not deferred, because without it real-world scanned input will silently produce poor results
- [ ] Born-digital PDF detection at content-validation time, with an explicit (not implicit) decision on reject-vs-passthrough — must be decided before requirement definition locks the input-validation behavior

### Add After Validation (v1.x)

- [ ] ALTO output (in addition to hOCR) — low cost, add once hOCR path is proven
- [ ] Per-word confidence surfaced in the structured API response (beyond what's embedded in hOCR/ALTO XML)
- [ ] Automatic orientation/script detection (OSD) pre-pass for rotated scans/photos
- [ ] Mixed-language per-job selection (`eng+rus`-style combined packs)

### Future Consideration (v2+, likely a different engine class entirely)

- [ ] Layout/semantic understanding + layout→markdown — requires a VLM-class engine (SEED-009 option b/c), not this milestone's classic-CPU-OCR engine
- [ ] Handwriting recognition — requires a different model family than tesseract/PaddleOCR
- [ ] Searchable-PDF overlay output — a third output format beyond locked scope; revisit if a concrete client need emerges
- [ ] Born-digital PDF detect-and-passthrough (option 2 above) as an active feature (vs. the v1 fail-closed reject) — legitimate differentiator, deliberately deferred pending its own opts/output-format design

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|----------------------|----------|
| Single image → txt | HIGH | LOW | P1 |
| Scanned/image-only PDF → per-page txt | HIGH | MEDIUM | P1 |
| hOCR output | HIGH | LOW | P1 |
| Language/script opts | HIGH | LOW–MEDIUM | P1 |
| Page-granular progress + ETA | MEDIUM | MEDIUM | P1 |
| Preprocessing (deskew/binarize/DPI) | HIGH | MEDIUM | P1 |
| Born-digital PDF detection + explicit reject/passthrough decision | MEDIUM | MEDIUM | P1 (the *decision* is P1; if passthrough is chosen, that specific mode can slip to P2) |
| ALTO output | MEDIUM | LOW | P2 |
| Per-word confidence in API response | MEDIUM | MEDIUM | P2 |
| Orientation/script auto-detection (OSD) | MEDIUM | MEDIUM | P2 |
| Mixed-language per-job selection | LOW–MEDIUM | LOW–MEDIUM | P3 |
| Layout→markdown / semantic understanding | N/A (anti-feature) | HIGH | Excluded |
| Handwriting recognition | N/A (anti-feature) | HIGH | Excluded |
| Searchable-PDF overlay output | LOW (for this milestone) | MEDIUM | Excluded (v2+) |

## Sources

- [Tesseract Command-Line Usage (tessdoc)](https://tesseract-ocr.github.io/tessdoc/Command-Line-Usage.html) — HIGH confidence, official docs: output formats (txt/hocr/alto/pdf/tsv), PSM modes, per-word confidence via TSV
- [tesseract.1.asc (GitHub, tesseract-ocr/tesseract)](https://github.com/tesseract-ocr/tesseract/blob/main/doc/tesseract.1.asc) — HIGH confidence, official man page
- [OCRmyPDF (GitHub, jbarlow83/OCRmyPDF)](https://github.com/jbarlow83/OCRmyPDF) — MEDIUM-HIGH confidence, reference implementation of the "detect existing text layer, skip/force/redo" decision this research flags
- [Docparser — Improve OCR Accuracy with Preprocessing Tips](https://docparser.com/blog/improve-ocr-accuracy/) — MEDIUM confidence (vendor blog, but consensus-aligned with multiple independent sources), preprocessing accuracy uplift figures
- [Markaicode — Tesseract Production Setup Guide](https://markaicode.com/tutorial/tesseract-tutorial-production-setup-guide/) — MEDIUM confidence, production preprocessing pipeline pattern
- Project context: `.planning/PROJECT.md` (locked scope, existing engine-class pattern), `.planning/seeds/SEED-009.md` (open questions, progress-reuse design, engine-class evaluation gate)

---
*Feature research for: OctoConv v1.10 OCR Engine (classic CPU-OCR, offline, CPU-only)*
*Researched: 2026-07-26*
