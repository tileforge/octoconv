---
id: SEED-021
status: dormant
planted: 2026-08-03
planted_during: post-Phase-52 (PR #5 open, reviewing external tooling)
trigger_when: when text extraction from born-digital PDFs is asked for, or when OCR routing quality on mixed-content PDFs becomes a complaint — whichever comes first
scope: Medium
---

# SEED-021: Adopt pdf-inspector for born-digital PDF extraction and per-page OCR routing

[`firecrawl/pdf-inspector`](https://github.com/firecrawl/pdf-inspector) — Rust, MIT,
`lopdf`-based, no ML models and no external services. Ships two CLI binaries
(`detect-pdf`, `pdf2md`) alongside its crate / Python / Node / WASM bindings.

Two separable opportunities of different size. The first is a capability hole,
the second is a quality upgrade. **The first stands on its own and does not
depend on the second.**

## Opportunity 1 — `pdf2md` closes a path that is currently unreachable

Verified against the code, not assumed:

`pdf` appears as a **source** format in exactly one place —
`ocrSourceFormats` (`internal/convert/ocr.go:34`). `LibreOfficeConverter` only
produces `documentFormats → pdf`, so pdf is a *target* there, never a source.
`PandocConverter`'s sources are markup formats. `ChromiumConverter` is
`{html → pdf}`.

And the OCR engine **rejects** text-based PDFs: `detectBornDigitalPDF`
(`internal/convert/ocrguards.go:230`) returns `ErrOCRBornDigital` when
`pdftotext` yields more than `ocrBornDigitalCharThreshold` non-whitespace
characters.

So `born-digital PDF → txt` has no path through this service at all: OCR
refuses it, and no other engine accepts pdf as input.

**Bonus finding worth fixing regardless of whether this seed is ever
harvested:** the comment at `ocrguards.go:227-228` says the rejection is
"steering the client to the document engine instead" — but the document engine
does not accept pdf as a source. That advice points at a dead end. Correct the
comment even if pdf-inspector is never adopted.

`pdf2md` fills exactly this: text-based PDF → clean Markdown, without OCR.
Cheap and fast precisely where the current alternative is either a hard refusal
or (if it somehow passed the guard) a 300-DPI per-page rasterize.

## Opportunity 2 — `detect-pdf` upgrades a binary heuristic to per-page routing

Today's classifier is `pdftotext` plus a non-whitespace character count against
a threshold: binary, and reject-only. `detect-pdf` returns four classes
(TextBased / Scanned / ImageBased / Mixed) with a confidence score and
**per-page OCR routing recommendations**.

Where the current heuristic is weakest is the Mixed document: a 200-page PDF
with 190 text pages and 10 scans clears the character threshold and is rejected
wholesale — even though those 10 pages are exactly what OCR is for.

Per-page routing also fits the existing architecture unusually well.
`ocrpdf.go`'s pipeline is already strictly page-at-a-time (pdfinfo page cap →
born-digital check → per page: page-box bound → pdftoppm render → tesseract →
delete before the next iteration). A per-page routing verdict drops into a loop
that already exists.

## Architectural fit: the binary, never the crate

This is the load-bearing constraint and it decides the shape of any adoption.

This project's engine convention is **external CLI invoked through hardened
`runCommand`** (`internal/convert/exec.go` — `Setpgid`, process-group SIGKILL on
timeout). `detect-pdf` and `pdf2md` are exactly that shape: drop-in.

The Rust **crate** does not fit. Linking it means CGO, and both `Dockerfile.api`
and `Dockerfile.worker` pin `CGO_ENABLED=0` for static binaries. Do not
re-litigate this by trying the library form first.

License is MIT — no conflict.

## Costs and risks, stated honestly

- **New toolchain in the OCR worker image.** It currently carries Debian
  packages (tesseract, poppler). A Rust binary means either a cargo build stage
  or pulling a release artifact. Bounded, not free.
- **`detect-pdf` is an *additional* dependency unless it fully replaces the
  `pdftotext` call.** Running both is the worst outcome — decide which.
- **`detectBornDigitalPDF` is a load-bearing guard**, and it must keep running
  **before any render**. That ordering is Phase 43's memory-bomb safety
  criterion (43-CONTEXT.md SC2), not an implementation detail. Any swap
  preserves it or is wrong.

### The unresolved tension: per-page routing vs. a whole-document reject

Flagged during review of this seed and worth stating rather than leaving for
whoever harvests it to trip over.

Opportunity 2 above wants Mixed PDFs routed **per page**. But
`detectBornDigitalPDF` rejects **the whole document** once its text threshold
is crossed, and the constraint immediately above says that guard must keep
running before any render. Those two cannot both hold unchanged: leave the
guard as-is and mixed PDFs stay rejected, so per-page routing is unreachable;
remove it and the memory-safety invariant it exists to enforce goes with it.

The resolution is that the guard's **ordering** is what is load-bearing, not
its current whole-document verdict. A replacement sequence that preserves the
invariant would be:

0. **The `pdfinfo` page-count cap runs first — before `detect-pdf`, not after.**
   An earlier draft of this seed put classification first and said the existing
   guards "stay, unmoved". That was wrong, and the correction matters: the
   invariant is not only "nothing rasterized before the decision" but also
   "nothing *unbounded* parsed before the cap". `detect-pdf` parses the whole
   document to classify every page and emit its per-page routing verdict, so a
   10,000-page PDF would consume that work before any cap fired. The cheap
   `pdfinfo` count is the only guard that bounds the expensive step, so it must
   precede it.
1. **Bounded pre-render classification** — `detect-pdf` then runs, in place of
   the `pdftotext` threshold call, on a document already known to be within the
   page cap. It is a parse, not a render, so "nothing rasterized before the
   decision" still holds.
2. **Per-page routing from that verdict** — pages classified text-based are
   extracted; pages classified scanned enter the existing OCR loop. A document
   with zero scanned pages is still rejected by the OCR engine, but now for a
   stated reason and with `pdf2md` as the path that actually serves it.
3. **The per-page page-box physical-size bound stays, unmoved** — still
   evaluated per page, still before that page's render. Nothing about the
   one-page-in-flight memory ceiling changes; only the *set* of pages entering
   the loop does.

### The merging contract, which this seed does not yet define

A mixed PDF produces two streams — `pdf2md` output for text pages, OCR output
for scanned pages — and the seed above stops before saying how they become one
artifact. That is an open design question, not an oversight to be improvised at
implementation time. It must specify:

- **Complete, non-duplicated coverage** — every page of the source appears
  exactly once in the output, whichever stream produced it.
- **Source order** — pages merge in document order, not stream order (all text
  pages then all scanned pages would be wrong).
- **Whether `pdf2md` can operate per page at all.** Unverified. If it only
  emits whole-document Markdown, a mixed document needs either a page-range
  flag, an upstream split (see SEED-017), or post-hoc slicing of its output —
  and that answer changes the shape of the whole feature.
- **Target-format asymmetry.** `pdf2md` emits Markdown; the OCR engine emits
  `txt`/`hocr`/`alto`. Merging Markdown with hOCR is not obviously meaningful,
  so the supported mixed-output targets need stating rather than assuming.

Precedent worth reusing: `ocrPageMerger` (`internal/convert/ocr.go:261-306`)
already solves the structural half of this for hOCR/ALTO — it assembles
per-page fragments into one valid XML document via `encoding/xml`, deliberately
never by string concatenation. A mixed merger should follow that discipline.

Any harvest of Opportunity 2 needs a proof test on a genuinely mixed fixture
asserting complete, non-duplicated, source-ordered page coverage.

## Assumptions a harvester must confirm

- **`detect-pdf` never rasterizes internally.** If it does, the approach
  collapses back into the guard it was meant to replace, and Opportunity 1
  (`pdf2md`) should be harvested alone.
- **`detect-pdf`'s own cost is bounded by page count**, so the `pdfinfo` cap in
  step 0 is a sufficient bound on it. If its cost scales with embedded-image
  size or some other dimension the page count does not capture, step 0 needs a
  second bound.
- **`pdf2md` supports per-page operation**, per the merging contract above.
- **Young, small project.** For a pure parser with no ML, the realistic failure
  mode is misclassification rather than a crash — so keep the existing guards
  (page-count cap, page-box physical-size bound) in place rather than trusting
  a third-party verdict to bound the work.
- **Unverified claims.** "10-50ms classification" and the quality of the emitted
  Markdown are the project's own README claims. Nobody has run this against
  real OctoConv inputs.

## Harvest as a spike first, not a phase

The open question is empirical and cheap to answer: on real PDFs, does
`detect-pdf`'s classification agree with the current threshold, where do they
diverge, and is `pdf2md`'s Markdown actually usable as a conversion output? A
spike answers that; committing a phase before it would be guessing.

`pdf2md` is worth harvesting even if `detect-pdf` is rejected.

## Relation to other work

- **SEED-017** (PDF split/merge, multi-input/multi-output) — the strongest
  adjacency. Per-page classification is precisely the input a split operation
  wants, and SEED-017's multi-output path is what would let a Mixed PDF be
  handled per-page rather than refused whole.
- **SEED-009** (OCR engine class) — already shipped as `EngineOCR`; this seed
  refines its routing rather than replacing it.
- **SEED-018** (job pipelines) — `detect-pdf → route → extract-or-OCR` is a
  natural pipeline, and would be cleaner expressed as one than as branching
  inside a single converter.
- **SEED-010** (dynamic capability registry) — a new `pdf → md/txt` pair is
  another entry arguing for the catalog.

## Breadcrumbs

- `internal/convert/ocr.go:34` — `ocrSourceFormats`, the only place `pdf` is a
  source format.
- `internal/convert/ocrguards.go:224-239` — `detectBornDigitalPDF`, the
  threshold heuristic `detect-pdf` would replace, plus the misleading
  "document engine" comment.
- `internal/convert/ocrpdf.go` — the per-page streaming loop that per-page
  routing would slot into.
- `internal/convert/exec.go` — `runCommand`, the hardened subprocess seam any
  new binary must go through.
- `internal/convert/libreoffice.go:14` — `documentFormats`, confirming pdf is
  target-only there.
- `Dockerfile.worker` — `CGO_ENABLED=0`, the constraint that rules out the crate.
- `internal/convert/ocrguards_test.go:227-246` —
  `TestDetectBornDigitalPDF_RealBinary` asserts `ErrOCRBornDigital` for
  `testdata/ocr/born_digital.pdf` today. Opportunity 1 makes that PDF
  *acceptable* (routed to `pdf2md`), so this test's assertion inverts under
  the harvest. A harvester must add the accepted-text-path test alongside it
  rather than delete the coverage — the fixture already exists and pins the
  exact case this seed changes.
