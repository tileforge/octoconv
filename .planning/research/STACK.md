# Stack Research

**Domain:** Classic CPU-OCR engine class (6th engine, OctoConv v1.10)
**Researched:** 2026-07-26
**Confidence:** HIGH (versions/licenses verified via GitHub API + live package-manager probe; accuracy/speed comparisons MEDIUM — vendor/blog benchmarks, not independently reproduced, but multiple sources agree on direction and it matches known engine architecture)

This is a **replacement** stack note for OctoConv's v1.10 milestone (adding OCR as a 6th engine class). It supersedes the previous milestone's STACK.md content (v1.9 — job progress/SSE/cancel, already shipped and merged, not revisited here). This note covers only what the new OCR engine class needs: the OCR engine itself, the pdf→image rasterizer pre-stage, language-data packaging, and preprocessing.

## Bottom Line

**Tesseract 5.5.3, invoked as a CLI subprocess exactly like every other engine, is the recommendation** — it wins the locked eval-gate (offline, CPU-only, clean scans, deterministic, needs hOCR/ALTO) decisively on three axes: it's faster on CPU for clean documents, it has native mature hOCR/ALTO output (PaddleOCR has none), and it fits the existing `runCommand` hardened-exec architecture with **zero new Go dependencies** (no cgo, no Python runtime — a plain CLI binary, same shape as `vips`/`ffmpeg`/`soffice`/`whisper-cli`). Pair it with Poppler's `pdftoppm` for the pdf→image pre-stage (lightweight, CLI-only, GPLv2/v3 — not AGPL). No image-preprocessing library is needed on day one; Tesseract's bundled Leptonica already does adaptive binarization internally.

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| **Tesseract OCR** | **5.5.3** (tag published 2026-07-24, verified via GitHub Releases API) | OCR engine — LSTM-based text recognition, CLI invocation | Wins the eval-gate for THIS project's locked scope (offline, CPU-only, clean scans, deterministic, needs hOCR/ALTO). Faster on CPU than PaddleOCR for clean documents (multiple sources: ~1-3s/page vs ~5-10s/page; one micro-benchmark reported 453ms vs 2143ms per image), has **native, mature hOCR (since 3.x) and ALTO (since 4.1.0) output** with zero extra code, and is a pure CLI binary — fits the existing hardened `runCommand` pattern byte-for-byte (same shape as `vips`/`ffmpeg`/`soffice`/`whisper-cli` invocations already in the codebase) |
| **Poppler `pdftoppm`** (poppler-utils) | 22.12.0 (Debian bookworm, apt) — a newer poppler release is fine too if built from source | pdf → image rasterize pre-stage for scanned/image-only PDF input | Lightweight (~1.1MB package + shared libs), fast, purpose-built exactly for "render PDF pages to raster images at N DPI" — the narrow job needed here (not full PDF editing/printing). CLI-invoked only (matches hardened-exec pattern, no linking) |
| **tessdata_fast** language models | pinned by commit SHA (Apache-2.0, `tesseract-ocr/tessdata_fast`) | Language/script trained data for OCR recognition | LSTM-only "fast" variant: `eng.traineddata` ≈4.1MB, `deu.traineddata` ≈1.5MB, `rus.traineddata` ≈3.9MB — small enough to bake several languages into the image cheaply (mirrors the whisper.cpp SHA-256 model bake-in precedent from v1.7/v1.8). `tessdata_best` (higher accuracy, much larger, slower) exists but is unnecessary for the locked "clean scans" scope — defer, don't default to it |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Leptonica | bundled with Tesseract (apt: transitively pulled; upstream 1.87.0 if building from source) | Image I/O + adaptive Otsu binarization, deskew primitives, used internally by Tesseract's own pipeline | Always present as a Tesseract dependency — **do not add a separate preprocessing stage for it**; Tesseract already runs Otsu-style binarization/normalization on input before OCR |
| `pdfinfo` (poppler-utils, same package as `pdftoppm`) | 22.12.0 | Read PDF page count before the per-page rasterize loop | Needed to compute `total_pages` for the page-granular progress ramp (SEED-009's progress design) — no separate library needed, it ships in the same apt package as `pdftoppm` |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `tesseract --list-langs` / `-l <lang>` | Language selection at invocation time | The `-l` argument resolves to a `<lang>.traineddata` filename under `TESSDATA_PREFIX` — **treat this exactly like the `AudioOpts` closed-allowlist pattern**: map the validated `opts.language` enum value to a fixed, baked-in language code server-side; never pass client-supplied opts bytes straight into the `-l` argv value beyond the allowlist match (same injection-test discipline as `internal/convert/audioopts.go`) |
| `tesseract img out txt hocr alto` | Single invocation, multiple output configfiles | One CLI call can emit `.txt`, `.hocr`, and `.xml` (ALTO) simultaneously by passing multiple output-format configfile names as trailing args — avoids running the engine 3x per page |

## Installation

```dockerfile
# Dockerfile.ocr-worker (sketch — mirrors Dockerfile.audio-worker's multi-stage shape)

# Stage 1: build Tesseract 5.5.3 from source, pinned by tag (mirrors ffmpeg
# n8.1.2-from-source / whisper.cpp commit-pin precedent — reproducible, avoids
# Debian bookworm's stale packaged 5.3.0)
FROM debian:bookworm-slim AS tess-build
RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential autoconf automake libtool pkg-config \
      libleptonica-dev libpng-dev libjpeg62-turbo-dev libtiff-dev zlib1g-dev \
      git ca-certificates
RUN git clone --branch 5.5.3 --depth 1 https://github.com/tesseract-ocr/tesseract.git \
    && cd tesseract && ./autogen.sh && ./configure && make -j"$(nproc)" && make install \
    && ldconfig

# Stage 2: runtime
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates poppler-utils libleptonica6 libpng16-16 libjpeg62-turbo libtiff6 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=tess-build /usr/local/bin/tesseract /usr/local/bin/tesseract
COPY --from=tess-build /usr/local/share/tessdata /usr/local/share/tessdata
# Bake pinned tessdata_fast models with a SHA-256 fail-closed check (mirror v1.7's
# model bake-in): eng + a small fixed set matching the opts allowlist.
COPY tessdata/*.traineddata /usr/local/share/tessdata/
USER nobody
```

```bash
# Simpler fallback path (if source-build reproducibility isn't required):
apt-get install -y --no-install-recommends tesseract-ocr poppler-utils
# Debian bookworm ships tesseract-ocr 5.3.0 — older but stable; verified
# installed footprint with eng+deu+rus+osd tessdata ~= 20MB total.
```

No new Go module dependencies. OCR is invoked exactly like every other engine: a CLI subprocess through the existing hardened `runCommand` (`internal/convert/exec.go`) — `Setpgid` + timeout + SIGKILL-on-timeout, unchanged.

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|--------------------------|
| Tesseract (classic CPU-OCR) | **PaddleOCR (PP-OCRv5)** | If input quality shifts to messy/curved/handwritten/photographed text and a GPU becomes available — PaddleOCR's neural detector+recognizer clearly wins there (reported ~88.7% vs ~52.1% on curved text, ~72.8% vs ~45.2% on handwriting in one comparison) and on GPU throughput (~120 pages/min on RTX 3090 vs Tesseract's CPU-bound ~25 pages/min). This is explicitly the "(b) self-hosted VLM/transformer" path SEED-009 flags as a *different, heavier* engine class — not a drop-in swap. It also has **no native hOCR/ALTO exporter** (JSON bbox+text output only), so hitting the txt+hOCR/ALTO requirement means building and maintaining a custom coordinate-serialization layer that Tesseract gives for free |
| `pdftoppm` (Poppler) | Ghostscript | If a scanned PDF has complex color management, unusual color spaces, or is otherwise malformed enough that Poppler's parser chokes — Ghostscript's PDF interpreter is more tolerant/complete, at the cost of being a much heavier, slower binary and AGPLv3-licensed (see below). Keep as a manual escape hatch, not the default rasterizer |
| `pdftoppm` (Poppler) | `mutool` (MuPDF) | If raw rasterize speed on very large PDFs is the bottleneck — `mutool` is reportedly faster — but MuPDF's licensing moved to **AGPLv3**, a materially stronger copyleft than Poppler's GPLv2/v3; no need to take on that risk for a straightforward "PDF page to PNG at 300dpi" job Poppler already handles well |
| Baked `tessdata_fast` | `tessdata_best` | If accuracy testing on real scanned documents shows the fast/LSTM-only models under-perform on the target document types — `tessdata_best` trades size/speed for accuracy. Don't default to it without measured evidence (same "measure, don't assume" discipline as the RTF timeout gates) |
| No dedicated preprocessing stage | `unpaper` (deskew/border-removal, GPL) or a `leptonica`-based deskew pass | If live testing on real scans shows meaningful accuracy loss from skew/noise that Tesseract's built-in Otsu binarization doesn't handle — add as a gated, evidence-driven follow-up (mirrors how RTF gates and preset validation were only added once measured), not a day-one dependency |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **PaddleOCR / PaddlePaddle** as the primary engine | Pulls in a full Python + ML-framework stack (PaddlePaddle CPU wheel alone ~111MB, growing across 3.x releases, plus opencv/numpy/etc. dependencies) — a categorically heavier and slower-to-cold-start container than every existing engine-worker image (audio-worker is 682MB *with* a bundled Whisper model; a Python+Paddle+models stack would likely exceed that for a CPU-only path that's slower on the clean-scan case this project targets). It also has no native hOCR/ALTO output (custom code needed) and no natural fit with the Go-CLI-exec architecture | Tesseract (this document's recommendation) |
| **`gosseract`** or any cgo-based Tesseract binding | The project compiles all Go binaries with `CGO_ENABLED=0` for static binaries (`Dockerfile.api`, `Dockerfile.worker`, and by convention every subsequent worker) — a cgo binding to `libtesseract` is flatly incompatible with that constraint | Plain CLI invocation of the `tesseract` binary through `runCommand`, same as every other engine |
| **GPU-accelerated OCR of any kind** (PaddleOCR-GPU, Surya, GOT-OCR2.0, Qwen2-VL, MiniCPM-V) | Explicitly out of scope per the locked v1.10 decision (CPU-only, no GPU appetite) — these are the SEED-009 "(b) self-hosted VLM" path, a different and heavier engine class, not this one | Classic CPU-OCR (Tesseract) |
| **Cloud OCR/VLM APIs** (Claude/GPT-4o/Gemini vision, AWS Textract, Google Vision) | Violates the hard offline-engines constraint (same reason cloud OCR/audio APIs were pre-rejected for every prior engine class); reintroduces hallucination/omission risk this project's fail-closed philosophy explicitly rejects | Classic CPU-OCR (Tesseract), fully offline |
| **Ghostscript as the default rasterizer** | AGPLv3-licensed (network-use copyleft trigger, stronger than anything else in the current stack) and a much heavier/slower binary than Poppler for the narrow "rasterize PDF page to image" job | `pdftoppm` (Poppler) as default; Ghostscript only as a documented manual fallback for pathological PDFs, if ever needed |
| A new preprocessing microservice/library (ImageMagick, custom deskew pipeline) as a **mandatory** day-one stage | Adds a new process-exec surface and dependency for a problem that may not exist on the locked "clean scans" input assumption — conflicts with the project's demonstrated zero-new-dependency bias (e.g., the decompression-bomb protection was built as zero-dependency Go parsers rather than pulling in `golang.org/x/image` or shelling out) | Rely on Tesseract's built-in Leptonica-based Otsu binarization first; add `unpaper`/deskew only if measured accuracy on real inputs demands it |

## Stack Patterns by Variant

**If input is a single image (jpg/png/tiff/webp):**
- Skip the rasterize pre-stage entirely — feed the image straight to `tesseract` (it reads jpg/png/tiff natively via Leptonica; webp needs a one-shot decode-to-png first, since Leptonica's webp support is inconsistent across Debian's bundled build — verify at implementation time, or normalize webp→png before OCR, reusing the existing `libvips`/image engine's decode path if convenient)
- Progress = single-page: `null` -> `100` (atomic), per SEED-009's progress design — no ramp

**If input is a scanned/image-only PDF (multi-page):**
- Pre-stage: `pdfinfo file.pdf` -> page count -> loop `pdftoppm -r 300 -f N -l N -png file.pdf page-N` per page (300 DPI is the commonly recommended resolution for Tesseract's LSTM accuracy)
- OCR each rasterized page individually inside the worker's own loop (this is *why* progress here is page-granular, not a smooth ffmpeg-style stream — Tesseract's CLI emits no continuous progress signal)
- Progress = `pages_done / total_pages * 100`, driven by the worker's loop, exactly as SEED-009 specifies — reuses `Repo.UpdateProgress` unchanged

**If a language other than the default is requested:**
- Validate `opts.language` against a closed allowlist (mirror `AudioOpts`) whose values map 1:1 to baked `<lang>.traineddata` files — reject anything else with the same fail-closed 422 pattern used elsewhere, and injection-test the `-l` argv construction the same way `AudioOpts` is tested

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|------------------|-------|
| Tesseract 5.5.3 | tessdata_fast (any recent commit; format unchanged since 4.1.0's LSTM `.traineddata` format) | Traineddata format has been stable across 5.x; no version-pairing gotcha found |
| Tesseract 5.x | Debian bookworm's packaged Leptonica (transitively via apt) or upstream Leptonica >=1.74 if building from source | Debian bookworm ships Tesseract 5.3.0 + compatible Leptonica automatically via apt dependency resolution — only a concern if building Tesseract from source against a newer/older Leptonica than the runtime image provides |
| `pdftoppm` output (PNG/PPM) | Tesseract's Leptonica image loader | Straightforward — PNG is a first-class Leptonica format, no conversion layer needed between the rasterize stage and the OCR stage |

## Sources

- `https://api.github.com/repos/tesseract-ocr/tesseract/releases` — verified latest tag `5.5.3`, published 2026-07-24 (HIGH confidence, direct GitHub API)
- `https://api.github.com/repos/tesseract-ocr/tesseract/license` — Apache-2.0 (HIGH confidence)
- `https://api.github.com/repos/tesseract-ocr/tessdata_fast/license` and `tessdata_best/license` — both Apache-2.0 (HIGH confidence)
- `https://api.github.com/repos/PaddlePaddle/PaddleOCR/releases` — verified latest tag `v3.7.0`, published 2026-06-11; license Apache-2.0 (HIGH confidence, direct GitHub API)
- `https://api.github.com/repos/DanBloomberg/leptonica/releases` — latest `1.87.0`, 2025-12-24 (HIGH confidence)
- Live `debian:bookworm-slim` container probe (`apt-get install tesseract-ocr poppler-utils`) — confirmed Debian bookworm packages `tesseract-ocr 5.3.0-2`, `poppler-utils 22.12.0-2+deb12u2`; installed footprint `tesseract-ocr` ~=2.6MB binary package + `/usr/share/tesseract-ocr` ~=20MB with eng+deu+rus+osd tessdata; `poppler-utils` ~=1.1MB (HIGH confidence, directly measured)
- `tesseract-ocr.github.io/tessdoc/ReleaseNotes.html` and GitHub wiki — hOCR since 3.x, ALTO since 4.1.0, TSV/PAGE also supported (MEDIUM confidence, official docs but not independently re-verified against a live 5.5.3 invocation)
- WebSearch aggregate (imagetotable.ai, tildalice.io, mljourney.com, koncile.ai, gigagpu.com, codesota.com) — CPU speed/accuracy comparisons (Tesseract faster on CPU/clean docs, PaddleOCR wins on GPU/messy input) (MEDIUM confidence — vendor/SEO-adjacent blogs, but the direction agrees across independent sources and matches Tesseract's known architecture: no neural detector stage vs PaddleOCR's full DB+CRNN pipeline)
- `huggingface.co/blog/baidu/ppocrv5`, PaddleOCR docs (`paddleocr.ai`) — PP-OCRv5 languages (~106-109), 5.2x CPU inference speedup vs prior PaddleOCR versions (not vs Tesseract) (MEDIUM confidence, official vendor blog)
- WebSearch on Poppler/MuPDF/Ghostscript licensing — Poppler GPLv2/v3, MuPDF moved to AGPLv3, Ghostscript AGPL/commercial dual-license (MEDIUM confidence, aggregated from multiple sources including Wikipedia and vendor comparison pages; license facts are stable/well-known, low risk of being wrong)
- `ocrmypdf.readthedocs.io` — confirms `pdftoppm`/Ghostscript/pypdfium2 are the three rasterizers in active real-world OCR pipelines, validating the "pick one of these three" framing used here (MEDIUM confidence, official third-party project docs)
- Project source: `Dockerfile.worker`, `Dockerfile.audio-worker` (referenced via CLAUDE.md architecture notes) — confirms `CGO_ENABLED=0` static-binary constraint and the `runCommand` hardened-exec pattern that Tesseract/pdftoppm CLI invocation must fit (HIGH confidence, first-party codebase fact)

---
*Stack research for: Classic CPU-OCR engine class (OctoConv v1.10)*
*Researched: 2026-07-26*
