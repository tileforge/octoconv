# Pitfalls Research

**Domain:** Adding a classic CPU-OCR engine class (tesseract/PaddleOCR) to an existing hardened, offline, engine-class-patterned Go async conversion service (OctoConv v1.10)
**Researched:** 2026-07-26
**Confidence:** HIGH (tesseract/leptonica behavior, DPI/PSM/OEM guidance, hOCR spec, born-digital detection — corroborated by official tessdoc + multiple independent sources) / MEDIUM (exact RTF numbers, PaddleOCR specifics — not measurable without a live gate, same caveat OctoConv already applies to audio/av)

## Critical Pitfalls

### Pitfall 1: Multi-page memory bomb — rasterizing an entire PDF before OCR starts

**What goes wrong:**
A scanned PDF is rasterized fully into memory (or into N full-resolution temp images) before any page is OCR'd. A 500-page PDF at 300 DPI (letter size ≈ 2550×3300px, ~25MB uncompressed per page as RGB) held all-at-once is ~12GB+ — an instant OOM kill of the OCR worker container. This is the direct structural analog of the image decompression-bomb (VALID-03, `MAX_IMAGE_PIXELS`) and the av duration/resolution guard (AVE-05) — but OCR adds a *third* axis (page count × per-page pixels) that neither existing guard covers, because neither image nor av jobs have a "how many pages" dimension.

**Why it happens:**
The naive implementation calls a PDF library's "render all pages" convenience function (e.g., `pdftoppm` without `-f`/`-l` page range, or PyMuPDF/pdfium "get all page images" in one call) because it's the simplest code path and works fine in local testing with 2-3 page sample PDFs. The bomb only appears with adversarial or just-large real-world input (500-page scanned book), which nobody tests against by default.

**How to avoid:**
- Rasterize **one page at a time** in the worker's page loop (`pdftoppm -f N -l N` / pdfium `render_page(n)` per iteration), never "render all." This also composes naturally with the page-granular progress design already planned in SEED-009 (`pages_done/total_pages`).
- Add a **page-count cap** (`OCR_MAX_PAGES`, analogous to `MAX_IMAGE_PIXELS` and the av `AVE-05` duration/resolution ceiling) enforced *before* rasterization starts, by reading the PDF's declared page count cheaply (most PDF libraries expose page count without rendering) — reject with 422 fail-closed if it exceeds the cap, exactly like the image-dimension pre-check happens before the pixel buffer is allocated.
- Bound the **per-page raster buffer** too: cap effective DPI (see Pitfall 2) so a single page can't be requested at absurd DPI (e.g., 2400 DPI on a large page) — this is the "decompression bomb" analog at the per-page level, distinct from the page-count cap.
- Discard/free each page's raster buffer after that page's OCR pass completes — don't accumulate all N page images before OCR-ing any of them.

**Warning signs:**
- Worker RSS climbs linearly with total page count during the "rasterize" phase and only starts dropping once OCR begins — the classic signature of "load all, then process."
- No `OCR_MAX_PAGES` env var / no pre-render page-count check exists in code review.
- Load-test with a large (200+ page) synthetic scanned PDF is missing from the test plan (mirrors the existing habit of not testing decompression-bomb cases until VALID-03 forced it).

**Phase to address:**
Foundation/converter phase (the `OCRConverter`'s page-loop design) — this must be an architectural decision from the first line of the pdf→image prestage, not a later hardening patch, because retrofitting streaming onto a "render-all" implementation means rewriting the converter's core loop.

---

### Pitfall 2: Accuracy silently depends on preprocessing that nobody scoped

**What goes wrong:**
tesseract (and to a lesser extent PaddleOCR) is trained/tuned assuming roughly 300 DPI, upright (deskewed), and reasonably binarized/contrast-normalized input. Feed it a raw phone-photo-quality scan, a skewed fax, or a PDF rasterized at the wrong DPI, and OCR accuracy can collapse (garbled text, dropped words) with zero error signal — the job still returns `done` with a low-quality `txt`. Because OctoConv's `Converter` interface returns success/failure, not a quality score, a silent accuracy failure looks identical to a healthy job from the API/webhook/DB's point of view. This is qualitatively different from every existing engine class: libvips/LibreOffice/ffmpeg either transcode correctly or fail loudly — OCR can "succeed" while being useless.

**Why it happens:**
The engine-class pattern (magic-byte validation → convert → upload) implicitly assumes conversion is either "works" or "fails." OCR breaks that assumption: the *quality* of a successful conversion is a spectrum, and tesseract does zero preprocessing on its own beyond minimal internal normalization delegated to leptonica — it does not aggressively deskew or denoise; external preprocessing is expected as ecosystem convention, not baked into the tool. Teams that treat OCR like "just another `Converter`" skip the preprocessing stage because the `Converter` interface doesn't have a hook for it.

**How to avoid:**
- Rasterize the pdf→image prestage at a **fixed target DPI (300)** rather than passing through whatever DPI the source PDF happens to declare (many scanned PDFs embed images at odd DPIs — 150, 96, or unspecified). Compute the raster scale factor from the PDF's page box size, not from an assumption the source is already 300 DPI.
- Run a lightweight **deskew + binarization pass** (leptonica exposes both `pixDeskew` and Otsu/adaptive thresholding — tesseract links against leptonica already, so no new dependency is required, only a preprocessing invocation before the OCR call) on each rasterized page before handing it to tesseract. For plain image inputs (not PDF-rasterized), apply the same preprocessing rather than special-casing "images already came from a scanner and are fine" (they usually aren't).
- Do **not** treat this as optional polish deferred to a later milestone — this is the single biggest accuracy lever available and costs nothing at runtime relative to the OCR pass itself.
- Consider exposing tesseract's own confidence score (`tesseract --print-confidence` / TSV output includes per-word confidence) as an internal signal even if not surfaced to the client yet — this gives an observability hook to detect "OCR ran but confidence was terrible" without adding client-facing complexity now.

**Warning signs:**
- Rasterization prestage hardcodes a `-r 150` (or omits `-r` and takes the library default) instead of computing 300 DPI from page dimensions.
- No leptonica preprocessing call appears between "rasterize" and "tesseract invoke" in the converter code.
- Manual QA only tests clean, already-upright, high-contrast sample scans — never a skewed/low-contrast one.

**Phase to address:**
Foundation/converter phase — preprocessing must be inside the `OCRConverter.Convert()` pipeline itself (rasterize → preprocess → OCR per page), not bolted on later; it directly shapes what the converter's internal stages look like.

---

### Pitfall 3: Wrong PSM/OEM defaults wreck otherwise-good input

**What goes wrong:**
tesseract's default Page Segmentation Mode (PSM 3, "fully automatic page segmentation, no OSD") assumes a full page of body text with a fairly standard single/multi-column layout. If OctoConv's OCR jobs are actually single-line receipts, ID cards, sparse form fields, or single words (all plausible internal-service inputs — e.g., extracting text from a screenshot or a cropped photo), PSM 3 badly under-performs or produces garbage/empty output, while a targeted mode (PSM 6 "uniform block of text", PSM 7 "single line", PSM 8 "single word", PSM 11 "sparse text") would work well. Similarly OEM (engine mode: 0 legacy, 1 LSTM-only, 2 both, 3 default-based-on-availability) affects which trained-data variant is actually used — mismatching OEM against the installed tessdata variant either silently falls back to a slower/less-accurate path or errors.

**Why it happens:**
PSM/OEM are invisible unless explicitly set — tesseract runs fine with defaults on *representative-looking* test documents, so the mismatch only surfaces once real client traffic sends something structurally different (a photographed business card, a scanned form) than what was manually tested during development.

**How to avoid:**
- Pick and hardcode a **sensible default PSM per document type**, informed by the locked scope (images + scanned/image-only PDF, i.e., full-page-like documents in the common case): PSM 3 (auto) or PSM 6 (uniform block) as the baseline, verified empirically against a small representative sample set during the RTF-measurement/accuracy-validation phase rather than assumed from documentation alone.
- If per-job layout hints are ever exposed through `opts` (not required by locked scope, but keep the seam open — mirrors how `AudioOpts` added a closed-allowlist knob), keep the allowlist narrow (e.g., `page` vs `sparse`) rather than exposing raw PSM integers to clients — mirrors the closed-allowlist opts pattern (`AudioOpts` closed allowlist + injection test) rather than free-form pass-through.
- Pin OEM explicitly (don't rely on "3 = default, pick automatically") and verify it matches the actually-installed tessdata variant (LSTM-only tessdata_fast/tessdata_best vs legacy) — an implicit default that silently degrades to legacy-engine behavior because the LSTM data isn't present is a straightforward, easy-to-miss self-inflicted regression.

**Warning signs:**
- No PSM/OEM flags appear explicitly in the tesseract invocation (relying entirely on binary defaults).
- Accuracy validation only used one type of test document (e.g., only full-page prose) — no test with sparse/tabular/single-line input despite that being plausible internal-service traffic.

**Phase to address:**
Foundation/converter phase, validated during the RTF-measurement/accuracy-gate phase (same phase that would run representative-sample accuracy checks before committing to the timeout number).

---

### Pitfall 4: Language-pack bloat balloons the worker image

**What goes wrong:**
tessdata language files range from a few MB (fast/integer models) to 10-20+ MB each (best/float models) per language; baking in "all supported languages" (100+ tessdata files) can add hundreds of MB to multiple GB to the OCR-worker image. This directly fights the project's established scale-from-zero KEDA pattern, where image pull time is part of the cold-start latency budget (explicitly measured/discussed for the audio-worker model bake-in, Phase 33) — a bloated OCR-worker image makes `0→1` scale-from-zero slower and defeats the point of scale-to-zero economics.

**Why it happens:**
"Just copy the whole tessdata repo in for language flexibility" is the path of least resistance and looks harmless in a quick local test — the cost only shows up in image size / registry pull time / KEDA cold-start latency, none of which are visible from a `docker build` succeeding.

**How to avoid:**
- Pin only the languages actually in scope per the locked-scope language/script allowlist (mirrors `AudioOpts`'s closed allowlist: `auto/en/ru/es/fr/de`-style short list, not "every language tesseract supports").
- Prefer `tessdata_fast` (integer models, smaller + faster) over `tessdata_best` (float models, larger + slower) unless a measured accuracy gate shows `fast` is insufficient for the target document types — same "measure, don't assume" discipline OctoConv already applies to RTF timeouts and PDF/A validation depth.
- Bake language files into the image the same way the audio-worker bakes its whisper model (SHA-256-pinned, reversible via build-arg) rather than fetching them at container startup (keeps the offline constraint intact and avoids a startup network dependency).

**Warning signs:**
- Dockerfile.ocr-worker copies an entire `tessdata`/`tessdata_best` directory instead of a short explicit language list.
- Image size review is skipped (the project already tracks this number explicitly for other workers — e.g., "образ 682MB arm64" for audio — so silence on OCR-worker image size in planning is itself a red flag).

**Phase to address:**
Containerization phase (Dockerfile.ocr-worker) — same phase that would run the RTF-measurement gate, since language-pack choice (`fast` vs `best`) directly affects both image size and measured pages/sec.

---

### Pitfall 5: hOCR/ALTO XML escaping and coordinate correctness

**What goes wrong:**
hOCR is HTML with structured `title` attributes carrying bounding-box data (`bbox x0 y0 x1 y1`) and OCR'd text as element content; ALTO is a separate XML schema with its own coordinate model. Two concrete failure modes: (1) OCR'd text containing `&`, `<`, `>`, or raw control characters gets embedded unescaped into the HTML/XML output, producing a document that's invalid XML/HTML and breaks downstream XML parsers — this is a real risk because OCR text is *by definition* untrusted/unpredictable content (unlike hand-authored HTML) and can contain anything the source document contained, including characters resembling markup; (2) bounding-box coordinates are reported relative to the *rasterized page image* the engine actually saw, not the original PDF's point-space or DPI — if the coordinate system isn't clearly pinned to "pixels at the DPI we rasterized at" and documented/tested, downstream consumers mis-map boxes onto the wrong regions (especially across pages rasterized at different effective DPIs, or if the rasterization DPI ever changes between requests).

**Why it happens:**
tesseract's own hOCR writer already escapes correctly, so the pitfall isn't "tesseract emits broken XML" — it's introduced when OctoConv's worker code post-processes the raw tesseract hOCR/ALTO output (e.g., to inject job metadata, merge multi-page output into one document, or re-map coordinates) using naive string concatenation instead of a proper XML encoder, exactly the class of bug the project explicitly disallows for JSON (`internal/api/handlers.go` deliberately disables JSON HTML-escaping with care) — the equivalent discipline doesn't automatically transfer to hand-rolled XML/HTML assembly.

**How to avoid:**
- Never hand-concatenate strings to merge per-page hOCR/ALTO fragments into a multi-page document — use a real XML/HTML encoder (Go's `encoding/xml` or `html/template` with auto-escaping) for any post-processing step that touches OCR'd text content, even if tesseract's own per-page output is already correctly escaped.
- Pin and test the coordinate contract explicitly: bbox values are in pixels of the rasterized page at the DPI used for that page's OCR pass; if DPI is ever page-dependent (shouldn't be per Pitfall 2's fixed-300-DPI recommendation, but verify), embed the DPI/dimensions in the output document's page-level metadata so consumers can convert back to PDF point-space themselves.
- Add a round-trip test: OCR a document containing literal `<`, `&`, `"` characters (e.g., a scanned page of source code or an HTML snippet) and assert the hOCR/ALTO output parses as valid XML.

**Warning signs:**
- Multi-page hOCR assembly code uses `fmt.Sprintf`/string `+` instead of an XML/HTML builder.
- No test exercises OCR'ing text containing `&`/`<`/`>` characters.
- No documented/tested statement of what coordinate space bbox values are in.

**Phase to address:**
Output-format phase (hOCR/ALTO writer implementation) — should be validated with a dedicated escaping/round-trip test in the same phase, mirroring how other output-correctness properties (e.g., PDF/A OutputIntent) get a dedicated verification step in this project's pattern.

---

### Pitfall 6: OCR-ing a born-digital PDF wastes compute and can be worse than extracting the existing text layer

**What goes wrong:**
Not every PDF with a `.pdf` magic byte is a scan. A born-digital PDF (produced by LibreOffice, a web page print-to-PDF, an existing OctoConv document-engine job, etc.) already has a proper embedded text layer with exact character positions. Running the pdf→image rasterize + OCR prestage on such a PDF anyway (a) burns CPU/time doing something that could be a near-instant text extraction, and (b) frequently produces *lower-quality* output than the existing text layer, because rasterizing at typical screen/print resolutions and re-OCR-ing reintroduces recognition errors on already-perfect text (wrong DPI, font hinting artifacts, etc.) — the OCR path can literally make already-correct data worse. Given OctoConv already has a document engine class that produces/consumes PDFs (LibreOffice, PDF/A export), it's entirely plausible a client accidentally routes a born-digital PDF into the new OCR queue.

**Why it happens:**
The locked scope says "image-only PDF" but the API/queue-routing layer has no way to *enforce* that a given PDF actually is image-only before the job is accepted — nothing currently in OctoConv's magic-byte sniffing distinguishes "PDF with a text layer" from "PDF that's just a scanned image wrapped in a PDF container" (both share the same `%PDF-` magic bytes and pass the existing PDF sniffer). Without an explicit check, the OCR converter will happily rasterize-and-OCR a born-digital PDF because nothing tells it not to.

**How to avoid:**
- Add a **pre-flight text-layer detection** step: before rasterizing, check whether the PDF's pages already contain extractable text (most PDF libraries expose a fast "does this page have any text runs" check without full parsing/rendering — e.g., pdfium/poppler's text-extraction API returning non-empty for a sampled page). This is the same "detect the real thing before doing expensive/wrong work" principle as content-sniffing by magic bytes rather than trusting client-declared extension.
- Decide and document the **product behavior** for a detected-as-already-has-text PDF: either (a) fail closed with a clear 422 ("this PDF already has extractable text; use the document engine, not OCR") mirroring the project's fail-closed philosophy for OLE-CFB/encrypted inputs, or (b) skip rasterization for pages that already have text and only OCR pages that don't (mirrors `ocrmypdf`'s `--skip-text` behavior) — pick one explicitly rather than leaving it as an accidental byproduct of "OCR ran on everything."
- Whichever behavior is chosen, treat mixed born-digital+scanned PDFs (some pages have text, some don't — plausible for merged documents) as a first-class case to test, not an edge case to ignore.

**Warning signs:**
- No text-layer pre-check exists anywhere between "PDF accepted by API" and "PDF handed to rasterize+OCR."
- No test PDF in the test suite has an existing text layer (only synthetic scan-like fixtures are used).
- No explicit decision recorded (Key Decision entry) on what should happen for a born-digital PDF submitted to the OCR queue.

**Phase to address:**
Foundation/converter phase (the earliest point the pdf→image prestage is designed) — this is a design decision, not a later hardening patch, because it determines whether the OCR converter needs a "per-page has-text" branch in its core loop.

---

### Pitfall 7: RTF (pages/sec) assumed instead of measured

**What goes wrong:**
OCR throughput (pages/sec, i.e., an OCR-domain equivalent of the audio/av "real-time factor") varies wildly by DPI, page complexity, language (CJK/complex scripts are markedly slower to segment and recognize than Latin scripts), PSM mode, and whether preprocessing (deskew/binarize) is applied per page. If `OCR_ENGINE_TIMEOUT` is copied from a neighboring engine class's constant (or picked from documentation "typical" numbers) instead of measured against this specific container's CPU allocation with this specific tessdata variant, it will either be too tight (causing spurious `timeout`-classified terminal/transient failures on legitimate large documents) or dangerously loose (letting a pathological/adversarial document tie up a worker slot for an unbounded time, undermining the whole point of a bounded engine timeout). The project's own Key Decisions log explicitly calls out this exact mistake for audio ("AUDIO_ENGINE_TIMEOUT измеряется RTF-гейтом, не копируется от соседних классов" — a copied constant for the most expensive class is "a direct path to a T-03-10-style race or a dead timeout").

**Why it happens:**
Measuring requires building the actual container, running representative documents through it under the real resource limits (`cpus`/`memory` constraints), and computing p95 — this is real work with an unglamorous payoff, so it's tempting to shortcut with "OCR is probably similar to audio, let's just use a similar number" especially under roadmap time pressure.

**How to avoid:**
- Build a dedicated `scripts/ocr-rtf-measure.sh` (mirroring `scripts/audio-rtf-measure.sh`) that runs N representative documents (varying page count, language, and quality) through the actual containerized worker under production-equivalent `cpus`/`memory` limits, and computes p95 pages/sec (or seconds/page) exactly like the audio p95-RTF gate did (`p95 RTF=0.206`, N=10, documented cpu/mem/arch caveats).
- Treat the measured number as a GO/NO-GO gate exactly like AUD-07's NO-GO lever (which cut `AUDIO_MAX_DURATION_SECONDS` rather than inflating the timeout) — if OCR pages/sec is too slow for the desired `OCR_MAX_PAGES` cap under the target worker CPU budget, the lever to pull is lowering `OCR_MAX_PAGES` (or requiring a beefier worker CPU allocation), not silently inflating the timeout past what's operationally safe.
- Measure separately per language if the language allowlist includes both fast (Latin) and slow (CJK, complex-script) languages — a single blended number risks being wrong in both directions across the allowlist's language spread.
- Record the measurement's caveats explicitly (arch, cpu/mem limits, tessdata variant used) in a Key Decision entry, matching how the audio/av RTF gates documented their own measurement conditions.

**Warning signs:**
- `OCR_ENGINE_TIMEOUT` appears in code/config before any `ocr-rtf-measure.sh`-equivalent script exists or has been run.
- The chosen timeout number matches (or is a round multiple of) an existing `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT` value without independent justification.
- No language-by-language throughput breakdown exists despite a multi-language allowlist being in scope.

**Phase to address:**
Containerization + RTF-gate phase — must be its own explicit phase/plan (mirrors Phase 32 for audio, "Dockerfile.audio-worker... measured RTF-гейтом"), not folded silently into general containerization work.

---

### Pitfall 8: Untrusted images/PDFs into tesseract/rasterizer without hardened execution

**What goes wrong:**
tesseract's core recognition path and, especially, its image-decoding dependency leptonica have a real history of memory-safety issues on malformed/crafted input (documented CVEs: DoS via crafted JPEG causing an arithmetic exception, heap buffer over-reads in TIFF/color-quantization/border-tracing code, crashes via malformed pixel-format conversions) — a tesseract-ocr maintainer issue is literally titled "Leptonica has no regard for security," underscoring that this isn't a one-off patched bug but a structural weak point in the dependency chain OCR necessarily runs. A PDF rasterizer (poppler/pdfium/mupdf) is a second, equally untrusted parser sitting in front of tesseract, with its own history of malformed-file crashes/resource exhaustion. Both external processes are being fed **client-supplied, potentially adversarial bytes** — exactly the threat model OctoConv already designed `runCommand`'s hardened process-group execution for (libvips/LibreOffice/chromium/ffmpeg/whisper all already run through it), but OCR adds *two* new external processes (rasterizer + tesseract) to that same trust boundary, not one.

**Why it happens:**
It's easy to treat "run tesseract" as a lower-risk operation than "run chromium-headless" because OCR feels like a narrower, more mechanical tool — but the CVE history says otherwise, and the locked scope explicitly includes accepting arbitrary client-uploaded images and PDFs, i.e., adversarial input by the project's own threat model (the same one that already justified `USER nobody`, resource limits, and process-group SIGKILL for every other engine).

**How to avoid:**
- Run **both** the rasterizer and tesseract invocations through the existing hardened `runCommand` (process-group `Setpgid` + timeout + full-group SIGKILL) — do not special-case OCR as "just a CLI tool, no need for the harness," since the exact same "orphaned child process" and "runaway resource use" risks apply.
- Keep the OCR-worker container's resource limits (`cpus`/`memory`) and `USER nobody` posture consistent with the existing worker containers (image/document/av/audio) — this is the primary practical mitigation against the leptonica/rasterizer memory-safety CVE class, since a crash/DoS is contained to a resource-capped, unprivileged, killable process rather than affecting the host or other jobs.
- Continue the fail-closed magic-byte sniffing discipline already established (VALID-03/CFB precedent) for whatever new input types OCR accepts beyond the existing 5 image formats + PDF (the PDF sniffer/validator already exists from the document engine class — reuse it rather than writing a second one).
- Pin tesseract/leptonica/rasterizer library versions explicitly (mirrors ffmpeg-from-source pin, whisper.cpp commit-hash pin) rather than "whatever apt/latest gives you," so a known-CVE version isn't silently baked into the image, and version bumps are a deliberate, reviewable action.

**Warning signs:**
- OCR converter shells out via plain `os/exec.Command(...).Run()` instead of the shared hardened `runCommand` helper.
- Dockerfile installs tesseract/leptonica/poppler via unpinned `apt-get install` with no version pin or changelog check.
- No fuzz/malformed-input test exists for the OCR converter (the project already has precedent for this discipline — CFB directory parser fuzzed 3.5M iterations, 0 crashes, Phase 22).

**Phase to address:**
Async pipeline/worker-integration phase (where `cmd/ocr-worker` and its exec calls are wired) — the hardening must be present from the first working version, not retrofitted, since it's a one-line choice (call `runCommand` vs. raw `exec.Command`) that's cheap now and expensive to retrofit once call sites multiply across rasterize+OCR.

---

### Pitfall 9: Empty/garbage OCR output treated as a job failure (or the reverse — silently accepted as fine)

**What goes wrong:**
Two symmetric mistakes are both plausible: (a) a blank page, a photo with no text, or a page that legitimately contains zero recognizable text produces empty `txt` output, and the worker code mistakenly treats "empty string" as an error condition and marks the job `failed` — this is wrong: an empty-but-valid OCR result (nothing to transcribe) is a *successful* conversion with an empty result, exactly analogous to how a silent stretch of audio isn't a whisper-transcription failure. (b) The opposite mistake: tesseract can produce confident-looking but essentially garbage output on pathological input (solid noise, non-text imagery, wrong-language text) with no exception thrown, and if nothing checks output sanity, a job that produced meaningless gibberish is reported `done` with no signal to the caller that the result may be untrustworthy — mirroring OctoConv's already-accepted "hallucination-on-silence" residual risk for whisper, which needed an explicit Key Decision to document rather than silently ship.

**Why it happens:**
The existing `Converter` interface's error contract (`Convert(ctx, inPath, outPath, opts) error`) has no room for "succeeded, but the result quality is questionable" — a binary success/error return naturally collapses both "legitimately nothing to OCR" and "OCR ran but the output is junk" into "no error occurred," so neither gets deliberate handling unless someone explicitly designs for it.

**How to avoid:**
- Explicitly decide and document (as a Key Decision, mirroring the whisper hallucination-on-silence precedent) that **empty `txt` output is a valid, successful result**, not a failure — a job that OCR'd a blank/text-free page correctly reports `done` with empty content, not `failed`.
- Where cheap to obtain (tesseract TSV/hOCR output already carries per-word confidence), consider recording an aggregate confidence/quality signal on the job (even if not exposed as an API field yet) purely for internal observability — this gives future milestones a foothold to add a "low confidence" surface without OctoConv currently promising accuracy guarantees it can't back up.
- Do not attempt to reject "garbage-looking" output algorithmically (e.g., heuristically flagging low-entropy or repetitive output as an error) — that reintroduces exactly the kind of nondeterministic quality judgment the project has consciously avoided elsewhere (fail-closed structural checks, not fuzzy heuristics) and risks false-positive terminal failures on legitimate low-text documents.
- Verify page-granular progress correctly reaches 100% and `done` status even when every single page's OCR result is empty text — an implicit "no output produced" branch is exactly the kind of edge case a naive progress/completion check could mishandle.

**Warning signs:**
- Worker code has an `if output == "" { return error }` check anywhere in the OCR conversion path.
- No test exercises "OCR a blank/no-text image" and asserts the job completes as `done` with empty result content.
- No Key Decision entry records the empty-output-is-valid choice (leaving it as an implicit, undiscussed byproduct of whatever the code happens to do).

**Phase to address:**
Foundation/converter phase (define the contract) with explicit test coverage added in the async-pipeline/worker-integration phase (verify the whole `queued→active→done` path handles the empty-result case correctly end-to-end, the same way `isAudioTerminal`/`isAVTerminal` classification got dedicated pinning tests).

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|-----------------|------------------|
| Render entire PDF to images before OCR-ing any page | Simpler converter code, fewer intermediate states | OOM on large/adversarial PDFs (Pitfall 1) | Never — same severity class as the decompression-bomb gap VALID-03 had to close |
| Skip explicit PSM/OEM flags, rely on tesseract defaults | Fewer knobs to design/test up front | Silent accuracy collapse on non-prose layouts (Pitfall 3) | Only for a throwaway spike/prototype, never for the shipped converter |
| Bake `tessdata_best` + all languages into the image | No language-availability surprises later | Multi-GB image, slower KEDA cold-start (Pitfall 4) | Never in production; acceptable briefly during local accuracy experimentation only |
| Hand-concatenate per-page hOCR fragments into one document | Fast to write | Invalid XML on adversarial OCR'd text, silent downstream parser breakage (Pitfall 5) | Never |
| Copy `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT` as a placeholder for `OCR_ENGINE_TIMEOUT` | Unblocks early integration work before the RTF script exists | Wrong-by-construction timeout for a differently-shaped workload (Pitfall 7) | Acceptable only as a temporary placeholder explicitly flagged TODO before the RTF-gate phase runs; must never reach the milestone's shipped config |
| Run tesseract/rasterizer via plain `exec.Command` instead of `runCommand` | Slightly less boilerplate in a first draft | Orphaned processes / unbounded resource use on malformed input (Pitfall 8) | Never past a local spike |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| PDF rasterizer (poppler/pdfium/mupdf) | Rendering all pages in one call, or trusting the PDF's declared page count without a pre-render cap | Render page-by-page in the worker loop; cap `OCR_MAX_PAGES` from a cheap page-count read before any rendering starts (Pitfall 1) |
| tesseract CLI/library | Treating it as a black-box "just works" conversion, no PSM/OEM tuning, no preprocessing | Fix DPI at rasterize time, apply leptonica deskew/binarization, pin PSM/OEM explicitly and validate against representative samples (Pitfalls 2, 3) |
| tessdata language files | Bundling the full upstream tessdata/tessdata_best repo | Pin a short, locked-scope language allowlist; prefer `tessdata_fast` unless measured accuracy requires `tessdata_best` (Pitfall 4) |
| hOCR/ALTO output consumers | Assuming tesseract's raw per-page output can be string-concatenated into a multi-page document safely | Use a real XML/HTML encoder for any post-processing merge step; pin and document the coordinate space (Pitfall 5) |
| Existing document-engine PDFs routed into OCR | No check for an existing text layer before rasterize+OCR | Pre-flight text-layer detection; explicit fail-closed-or-skip decision, tested with mixed born-digital+scanned fixtures (Pitfall 6) |
| v1.9 progress infra (`UpdateProgress`, SSE, webhook) | Assuming it needs OCR-specific changes | Reuse as-is (per SEED-009) — only the progress *source* (page-loop driven `pages_done/total_pages`) is new; the sink/transport is untouched |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| Full-PDF rasterization before OCR starts | Worker RSS spikes linearly with page count before any OCR work begins | Page-by-page streaming render + `OCR_MAX_PAGES` cap (Pitfall 1) | Any PDF beyond a handful of pages at 300 DPI; a 100+ page scan is enough to OOM a 1GB-limited worker |
| `tessdata_best` on every request regardless of language mix | Pages/sec far lower than audio/av's per-unit-of-work throughput, container CPU pegged | Default to `tessdata_fast`; escalate to `tessdata_best` only where a measured accuracy gate demands it (Pitfall 4, 7) | Becomes visible the moment the RTF-measurement script runs against realistic documents, not in isolated single-page smoke tests |
| Blended (unweighted) RTF measurement across all allowlisted languages | Timeout tuned to the "average" language, too tight for CJK/complex scripts, too loose for Latin | Measure pages/sec per language in the allowlist and take the worst case (or per-language timeout logic) (Pitfall 7) | Shows up first in production once traffic includes the slower language(s), not in a demo that only tests English |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Feeding client-supplied PDFs/images directly to the rasterizer and tesseract without the hardened `runCommand` wrapper | Malformed/crafted input triggers a documented class of leptonica/rasterizer memory-safety bugs (DoS, crashes, resource exhaustion); orphaned child processes on timeout | Route both the rasterizer and tesseract invocations through the existing hardened process-group exec + resource-capped, unprivileged (`nobody`) container, same as every other engine (Pitfall 8) |
| Unpinned tesseract/leptonica/poppler versions from `apt-get install` latest | Silently bakes a known-CVE version into the image with no visibility at review time | Pin explicit versions (mirrors ffmpeg-from-source / whisper.cpp commit-hash pins already used for other engines) |
| Skipping a text-layer/born-digital check on submitted PDFs | Not itself a security hole, but wastes compute budget and can be abused to burn worker-time cheaply | Pre-flight text-layer detection before rasterize+OCR (Pitfall 6) |
| No page-count/DPI cap on the rasterize prestage | A crafted PDF (declared huge page count, or huge page dimensions) becomes a resource-exhaustion vector, same shape as the image decompression-bomb class this project already had to close once | `OCR_MAX_PAGES` + fixed rasterize DPI + per-page dimension bound, enforced before any rendering (Pitfall 1) |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-------------------|
| Job marked `failed` for a legitimately blank/text-free page | Caller sees spurious failures for perfectly normal input (e.g., a blank cover page in a multi-page scan) | Treat empty OCR output as a valid `done` result with empty content (Pitfall 9) |
| No signal that OCR output may be low-confidence/garbage | Caller trusts a `done` job's text blindly, discovers quality issues only downstream | Record an internal confidence signal even if not yet API-exposed; document as an accepted residual risk rather than silence (Pitfall 9, mirrors the whisper hallucination-on-silence precedent) |
| Submitting a born-digital PDF to OCR gets silently reprocessed and returns worse text than the PDF's own text layer | Caller gets a degraded result with no explanation of why | Explicit fail-closed 422 (or explicit skip-already-has-text behavior), not silent re-OCR (Pitfall 6) |
| Multi-page progress ramp looks identical for a healthy job and one whose per-page OCR is failing silently on every page | No visibility into partial failure within a multi-page job | Consider per-page event/error visibility (at minimum in logs/metrics) distinct from the coarse job-level `pages_done/total_pages` ramp |

## "Looks Done But Isn't" Checklist

- [ ] **pdf→image prestage:** Often missing an `OCR_MAX_PAGES` cap and true page-by-page streaming — verify with a 200+ page synthetic scanned PDF under the worker's real memory limit, not a 3-page sample.
- [ ] **Preprocessing pipeline:** Often missing deskew/binarization entirely (tesseract "just runs" on raw rasterized pixels) — verify with a deliberately skewed/low-contrast test scan and compare accuracy against a clean control.
- [ ] **PSM/OEM selection:** Often left at library defaults with no documented rationale — verify tesseract invocation explicitly sets both flags and that a non-full-page-prose sample (sparse/single-line) was tested.
- [ ] **Language pack footprint:** Often ships the full upstream tessdata set "just in case" — verify the Dockerfile lists an explicit, short language allowlist and check final image size against the audio-worker's documented baseline (~682MB) for a sanity comparison.
- [ ] **hOCR/ALTO escaping:** Often only tested against clean sample text — verify with a document containing `&`/`<`/`>` characters and confirm the output parses as valid XML.
- [ ] **Born-digital PDF detection:** Often entirely absent — verify a PDF with a genuine existing text layer submitted to the OCR queue is handled by an explicit, tested code path (reject or skip), not silently re-rasterized.
- [ ] **RTF/timeout:** Often copied from a neighboring engine class as a placeholder that never gets replaced — verify `OCR_ENGINE_TIMEOUT` traces back to an `ocr-rtf-measure.sh`-style script's output with documented p95/N/cpu-mem conditions.
- [ ] **Hardened exec:** Often bypassed for "just a CLI wrapper" — verify both the rasterizer and tesseract calls go through the shared `runCommand` helper, not a bare `exec.Command`.
- [ ] **Empty-output handling:** Often untested — verify a blank/no-text input completes as `done` with empty result content, not `failed`.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|----------------|-----------------|
| Multi-page memory bomb (Pitfall 1) | MEDIUM | Refactor the converter's rasterize step from "render all" to a page-loop; add `OCR_MAX_PAGES` pre-check; requires a live large-PDF regression test to prove the fix, similar cost to how VALID-03 was retrofitted |
| Missing preprocessing (Pitfall 2) | LOW | Insert a leptonica deskew/binarize call between rasterize and tesseract invoke; no schema/API change, purely internal to the converter |
| Wrong PSM/OEM (Pitfall 3) | LOW | Change CLI flags/config; re-run accuracy validation against the representative sample set |
| Language-pack bloat (Pitfall 4) | LOW-MEDIUM | Trim Dockerfile's tessdata COPY to the allowlist; rebuild and re-measure image size and cold-start time |
| hOCR/ALTO escaping bugs (Pitfall 5) | MEDIUM | Swap hand-rolled string assembly for an XML/HTML encoder; requires re-validating every existing consumer of the output format hasn't silently depended on the old (broken) encoding |
| Born-digital PDF mishandling (Pitfall 6) | MEDIUM | Add the pre-flight text-layer check retroactively; decide and backfill the fail-closed-or-skip behavior; low risk of breaking existing jobs since this only changes behavior for a previously-mishandled input class |
| RTF assumed not measured (Pitfall 7) | LOW-MEDIUM | Build and run the RTF-measurement script post-hoc; adjust `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` per the NO-GO lever pattern; no code architecture change, just a config correction |
| Unhardened exec (Pitfall 8) | LOW | Swap `exec.Command` call sites for the shared `runCommand` helper — mechanical, low-risk change given the helper already exists and is proven across 5 other engines |
| Empty/garbage output mishandled (Pitfall 9) | LOW | Remove any `output == ""` → error branch; add the missing empty-output test; document the Key Decision retroactively |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|-------------------|---------------|
| Multi-page memory bomb (streaming + page cap) | Foundation/converter phase | Live test: 200+ page synthetic scanned PDF processed under the worker's real memory limit without RSS blowing past the cap; `OCR_MAX_PAGES` rejects an over-limit PDF with 422 before any rendering |
| Preprocessing (DPI/deskew/binarize) | Foundation/converter phase | Accuracy comparison test: skewed/low-contrast sample vs. clean control, both processed through the same pipeline, documented delta |
| PSM/OEM tuning | Foundation/converter phase, validated at RTF/accuracy-gate phase | Representative sample set (full-page prose + sparse/single-line) both produce acceptable output with the chosen fixed PSM/OEM |
| Language-pack scoping | Containerization phase | Dockerfile diff review + final image size compared against the audio-worker baseline; language allowlist matches the locked-scope opts allowlist exactly |
| hOCR/ALTO escaping + coordinates | Output-format implementation (part of foundation/converter phase) | Round-trip test: OCR text containing `&`/`<`/`>` produces valid, parseable XML; bbox coordinate space documented and pinned in a test |
| Born-digital PDF detection | Foundation/converter phase (prestage design) | Test with a mixed born-digital+scanned fixture and a pure born-digital fixture; explicit fail-closed-or-skip behavior asserted, Key Decision recorded |
| RTF measured not assumed | Containerization + RTF-gate phase (dedicated plan, mirrors Phase 32) | `scripts/ocr-rtf-measure.sh` output (p95 pages/sec, N, cpu/mem/arch conditions) documented in a Key Decision before `OCR_ENGINE_TIMEOUT` is finalized |
| Security (hardened exec, pinned deps, resource caps) | Async pipeline/worker-integration phase | Code review confirms `runCommand` used for both rasterizer and tesseract calls; Dockerfile pins explicit versions; fuzz/malformed-input test exists (mirrors CFB parser's 3.5M-iteration fuzz precedent) |
| Empty/garbage output handling | Foundation/converter phase (contract), verified in async-pipeline/worker-integration phase | Test: blank/no-text input completes as `done` with empty result content; Key Decision documents empty-output-is-valid and (if added) the confidence-signal approach |

## Sources

- [Tesseract Page Segmentation Modes (PSMs) Explained — PyImageSearch](https://pyimagesearch.com/2021/11/15/tesseract-page-segmentation-modes-psms-explained-how-to-improve-your-ocr-accuracy/) — MEDIUM confidence, corroborated by official tessdoc PSM/OEM descriptions
- [Page Segmentation Modes — tesseract-ocr/tessdoc (DeepWiki mirror)](https://deepwiki.com/tesseract-ocr/tessdoc/6.2-page-segmentation-modes) — MEDIUM confidence (official docs, mirrored)
- [Improving the quality of the output — Tesseract documentation](https://tesseract-ocr.github.io/tessdoc/ImproveQuality.html) — HIGH confidence, official docs
- [Language Data Files — tesseract-ocr/tessdoc (DeepWiki mirror)](https://deepwiki.com/tesseract-ocr/tessdoc/4-language-data-files) — MEDIUM confidence
- [tesseract-ocr/tessdata — GitHub (fast vs best model variants)](https://github.com/tesseract-ocr/tessdata) — HIGH confidence, official repo
- [Traineddata Files for Version 4.00+ — Tesseract documentation](https://tesseract-ocr.github.io/tessdoc/Data-Files.html) — HIGH confidence, official docs
- [hOCR spec 1.2 — kba/hocr-spec](http://kba.github.io/hocr-spec/1.2/) — HIGH confidence, the format specification itself
- [Advanced features / --skip-text — OCRmyPDF documentation](https://ocrmypdf.readthedocs.io/en/latest/advanced.html) — HIGH confidence, official docs of the most widely-used OCR+PDF pipeline tool, directly informs the born-digital-detection pitfall
- [ERROR - page already has text! — OCRmyPDF errors doc](https://github.com/jbarlow83/OCRmyPDF/blob/v15.4.1/docs/errors.rst) — HIGH confidence
- ["Leptonica has no regard for security" — tesseract-ocr/tesseract issue #1456](https://github.com/tesseract-ocr/tesseract/issues/1456) — HIGH confidence, maintainer-acknowledged structural security concern in the exact dependency this milestone will use
- [Leptonica CVEs — OpenCVE](https://app.opencve.io/cve/?product=leptonica&vendor=leptonica) — HIGH confidence, aggregated CVE database (arithmetic-exception DoS via crafted JPEG, heap over-reads in TIFF/color-quant/border-tracing code)
- [Tesseract CVEs — OpenCVE](https://app.opencve.io/cve/?product=tesseract&vendor=tesseract_project) — HIGH confidence
- Internal: `.planning/PROJECT.md` — OctoConv's own precedents (VALID-03 decompression-bomb guard, AVE-05 duration/resolution guard, `runCommand` hardened exec, RTF-measured `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT` gates, `isAudioTerminal`/`isAVTerminal` stage-aware retry, `AudioOpts` closed allowlist, whisper hallucination-on-silence accepted-risk precedent, CFB parser fuzz precedent) — HIGH confidence, primary source
- Internal: `.planning/seeds/SEED-009.md` — locked scope, progress-reuse design, engine-class choice rationale — HIGH confidence, primary source

---
*Pitfalls research for: adding a classic CPU-OCR (tesseract/PaddleOCR) engine class to OctoConv's existing async file-conversion service*
*Researched: 2026-07-26*
