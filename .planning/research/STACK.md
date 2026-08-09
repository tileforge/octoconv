# Stack Research — v1.13 "Inspect & Operations"

**Domain:** Synchronous file inspection + async sanitization added to an existing 7-engine Go
conversion service
**Researched:** 2026-08-07
**Confidence:** HIGH for pure-Go/existing-code findings (verified by reading the source);
MEDIUM for library capabilities verified via Context7/official docs; LOW flagged explicitly where
only WebSearch was available (mostly SEED-021's `pdf-inspector`, sourced from its own README/blog).

## Executive Verdict (one line each)

| Workstream | Verdict | Stack element |
|---|---|---|
| A. Format/size/macro/encryption/bomb signals | **Already built, zero new dependency** | `internal/convert/sniff.go`, `docsniff.go`, `dimensions.go`, `olecfb.go` — extend, don't replace |
| A. PDF page count / encryption / embedded files / JS / OpenAction | **Adopt `pdfcpu` v0.14.0 as a Go library (not a binary)** | New *direct* Go dependency, imported into `cmd/api` — the one real addition this milestone needs |
| A. AV duration/resolution/declared-codec in the sync path | **Write new pure-Go bounded container parsers** (extends `avsniff.go`/`audiosniff.go` family) — no ffprobe in API container | Zero new dependency, real new code |
| B. SEED-021 `pdf-inspector` | **Confirmed MIT/offline/no-ML; still "spike first, not a phase"** — its 0.1.x version and unmeasured binary size reinforce, not weaken, that conclusion | No stack change this milestone |
| C. PDF/OOXML sanitize | **`pdfcpu` (already adopted for A) for PDF; Go stdlib `archive/zip`+`encoding/xml` for OOXML; existing `vips copy ...[strip]` for images** | One dependency doing double duty (detect *and* verify); everything else is stdlib/already-present |
| D. Structured error codes | **No framework, no library — Go 1.13+ error-wrapping idiom the project already partially uses** | Zero new dependency |
| E. Bounded sync path | **`context.WithTimeout` + the pdfcpu pre-check-before-full-walk pattern + the already-present `go-chi/httprate` for a second rate budget** | Zero new dependency |

---

## A. Synchronous `POST /v1/inspect` — signal-by-signal

The API container today (`Dockerfile.api`) is `debian:bookworm-slim` + `ca-certificates` only,
`USER nobody`, `CGO_ENABLED=0` static binary, zero subprocess invocations. Every existing engine
shells out through the hardened `internal/convert/exec.go` `runCommand`; the API process itself
has never called `runCommand` — it only *validates* (magic bytes, zip central directory,
declared-dimension bounds) before enqueueing. Inspect must preserve that: it is a new *read path*
in the API process, not a new subprocess surface.

### Already answerable, pure Go, zero new dependency (verified by reading the code)

| Signal | Source | Confidence |
|---|---|---|
| True format (magic bytes) | `internal/convert/sniff.go` `Sniff()` — 9 fixed-offset signatures incl. `%PDF-` | HIGH |
| OOXML/ODF/epub format + total uncompressed size (bomb signal) | `internal/convert/docsniff.go` `SniffContainer()` — single-pass ZIP central-directory read | HIGH |
| **OOXML macros (`vbaProject.bin`, ODF `Basic/`)** | `docsniff.go` `SniffContainer().HasMacro` — **already computed today**, just not surfaced to a client-facing report | HIGH |
| Legacy binary / password-protected OOXML (OLE-CFB) | `internal/convert/olecfb.go` | HIGH |
| Image declared width/height (decompression-bomb-safe, pre-decode) | `internal/convert/dimensions.go` — png/jpg/webp/heic/tiff | HIGH |
| Size | `Content-Length` / bytes read | HIGH |

**Cost to surface these in `/v1/inspect`: none.** The detection already runs on the upload path;
inspect just needs to call the same functions and shape the response — this is the single biggest
finding of this research and should shrink the estimated Phase-56 scope accordingly.

### Requires a new dependency — the one recommended addition

**PDF page count, encryption status, `/EmbeddedFile` attachments, `/JS` `/JavaScript`
`/OpenAction` `/AA`, `/URI` `/GoToR` remote references, and a page-box bomb bound — all need a
real PDF object-graph parse**, not a byte scan. This is the one place in the whole milestone where
hand-rolling pure-Go code is the *wrong* call, for a reason specific to PDF: modern producers
routinely put these very keys inside **compressed object streams** (`/ObjStm`) and **cross-reference
streams** (PDF 1.5+), so a naive `bytes.Contains(b, []byte("/JavaScript"))` scan has a real false-negative
rate — exactly the "silent bypass" failure mode this project has been careful to avoid elsewhere (OLE-CFB
fail-closed rigor, saturating-add zip-bomb guard). Writing a correct compressed-xref/object-stream
PDF parser from scratch is not in the same risk class as the fixed 8-byte PNG/TIFF headers this
project already hand-rolls in `dimensions.go` — it is a miniature PDF parser, and getting it wrong
is a security bug, not a cosmetic one.

**Recommendation: `github.com/pdfcpu/pdfcpu` v0.14.0 (released 2026-08-03), Apache-2.0, pure Go,
zero CGO, zero new container binary.**

| Need | pdfcpu Go API (verified via Context7/pkg.go.dev) | Notes |
|---|---|---|
| Page count | `api.PageCount(rs io.ReadSeeker, conf *model.Configuration) (int, error)` | Direct, one call |
| Encryption status | `model.XRefTable.Encrypt != nil` (low-level) or CLI-equivalent `pdfcpu info` | Confirmed present, exact Go call needs a short spike but the capability is real |
| Embedded files | `pkg/cli` `ListAttachments`/`ListAttachmentsFile` (`cli.Command`) | Returns attachment list directly |
| JS / OpenAction / AA / remote-reference detection | **Not a single built-in call** — requires walking the parsed `XRefTable`/Catalog/Names-tree/page-annotation objects that pdfcpu already resolves (including inside object streams) | Real custom code, but on top of a maintained parser instead of a hand-rolled one — this is the trade this recommendation is making |
| Page-box bomb bound | `MediaBox` is already on the parsed page object pdfcpu exposes | Replaces the worker-only `pdfinfo`-based check for the sync path without adding `pdfinfo` to the API image |

Why this is a *Go dependency* recommendation, not a *container binary* recommendation: pdfcpu is a
library, imported with `go get`, compiled into the static `api` binary like every other Go module
this project already has. It adds zero apt packages, zero subprocess surface, and stays inside
`CGO_ENABLED=0`. It does **not** touch `internal/convert/exec.go` — it simply never uses it, the
same way the decompression-bomb guards never do.

Cost to state honestly: this is the largest single new Go dependency the project has taken since
the MCP `go-sdk` (v1.6.1, Phase 21) — a real content-parsing library, not a thin client wrapper
like `minio-go`/`pgx`/`asynq`. Justify it on the JS/OpenAction/EmbeddedFile need specifically; do
not use it as an excuse to also reimplement page-count-for-office-docs or anything already covered
above.

### Genuinely NOT worth a container binary

Do **not** add `poppler-utils`, `qpdf`, `mutool`, `exiftool`, or `ffprobe`/`ffmpeg` to
`Dockerfile.api`. Every one of them would either (a) duplicate what `pdfcpu`-as-a-library already
gives for PDF, or (b) be needed only for a signal (AV codec/resolution precision) this research
recommends reporting as best-effort/approximate rather than buying a whole binary + subprocess
surface for. Adding any of these to the API image also reopens a question this project has
deliberately kept closed: *the API process has never run `runCommand`.* Don't open that door for
a synchronous, request-scoped code path with its own rate budget and no queue to fall back on if a
subprocess hangs.

### Media duration / resolution / declared codec — the sharp case

**Verified:** today, duration/resolution/codec detection for audio/video exists **only** in
`internal/convert/av.go`, and **only** via `ffprobe` through `runCommand`
(`ffprobeAudioCodecArgs`, `probeAudioCodec`, `avProbeTimeout`) — that binary lives in the
**av-worker** image (built from ffmpeg n8.1.2 source), never in the API container. This is
exactly the case the question anticipates: **there is no zero-cost pure-Go path to ffprobe-grade
codec/profile detail**, and adding ffprobe to the API image means either (a) a second from-source
ffmpeg build stage duplicating the av-worker's, or (b) cross-Dockerfile artifact sharing that this
project's build system doesn't currently support (each `Dockerfile.*` is self-contained).

**Recommendation: write a new bounded pure-Go container-header parser** (a `probe.go` sibling to
`avsniff.go`/`audiosniff.go`/`dimensions.go`, same fail-closed discipline: declared value or
explicit "unknown", never a decode, never unbounded) covering:
- MP4/MOV: `moov > mvhd` (duration+timescale), `moov > trak > tkhd` (declared width/height),
  `stsd` sample-entry fourCC (declared codec identifier, e.g. `avc1`/`hvc1`/`mp4a`) — same box-walk
  discipline `avsniff.go` already uses for `ftyp`.
- RIFF (AVI/WAV): `avih`/`strh` chunks for AVI duration+resolution, `fmt `/`fact`/`data` chunks for
  WAV duration/sample-rate/channels — same discipline `audiosniff.go`'s `matchWAV`/`matchAVI`
  already establishes for the container shape.
- EBML (MKV/WEBM): extend the existing bounded EBML vint walker (`avsniff.go`) to also read
  `Segment > Info > Duration` and the video/audio `TrackEntry`'s `CodecID` string — the walker
  infrastructure (vint length, bounded `avPeekLen`) already exists; this is additive, not new
  machinery.
- MP3/OGG: **explicitly best-effort.** MP3 duration without a Xing/VBRI header requires either a
  full frame count (expensive) or a CBR-bitrate estimate (approximate); OGG duration requires
  reading the last page's granule position. Report these as `"approximate": true` in the inspect
  response rather than skip them — this reuses the same "honest partiality" discipline the
  milestone description already requires for page sampling.

State the boundary explicitly in the requirements that come out of this research: **exact
codec/profile-level detail (H.264 profile/level, precise AAC sample format, etc.) is out of scope
for `/v1/inspect` and stays with the async `convert` path**, where the appropriate worker already
has `ffprobe`. Inspect reports a *declared* codec tag, not an *audited* one — consistent with how
`dimensions.go` already reports *declared* pixel dimensions, not decoded ones.

---

## B. SEED-021 — `firecrawl/pdf-inspector` re-evaluated

Confirmed against current sources, extending (not repeating) SEED-021's own analysis:

| Fact | Value | Source/confidence |
|---|---|---|
| License | MIT | HIGH — GitHub repo header |
| Language | Rust, `lopdf`-based, no ML | HIGH |
| Binaries | `detect-pdf` (classify: TextBased/Scanned/ImageBased/Mixed + confidence + per-page routing), `pdf2md` (PDF→Markdown, has `--pages`/`--select-pages` flags suggesting **per-page operation is supported** — resolves one of SEED-021's open "assumptions a harvester must confirm") | MEDIUM — official README via WebFetch |
| Current version | 0.1.7 on crates.io (8 published versions total) | MEDIUM — crates.io via WebSearch |
| Runtime network | None — "pure Rust", "no external services" | MEDIUM — project's own claim, not independently verified against a live process trace |
| Binary size | **Not published anywhere found** — SEED-021's own "10-50ms classification" claim is likewise the project's own README, not measured against real OctoConv inputs | Confirmed still unverified — LOW |
| Risk-signal detection (macros/JS/encryption/embedded files) | **None.** `detect-pdf` only classifies text-vs-scan content via `Tj`/`TJ`/`Do` operator and font-encoding inspection — it is not a security tool and does not overlap with Section A/C at all | HIGH — scope confirmed via README |
| Build story for dual-arch static binaries | **No official prebuilt release binaries found for amd64+arm64.** Adoption means a `cargo build --release` (or `cross`) stage in `Dockerfile.ocr-worker`, the same *shape* of cost as the from-source Leptonica/Tesseract/ffmpeg build stages this project has already paid three times (D-01 precedent) — bounded, precedented, but a **new toolchain (Rust) in one Dockerfile**, not free | MEDIUM |

**Verdict: unchanged from SEED-021 — spike first, not a phase.** Two independent reasons now
reinforce this, beyond SEED-021's own "empirical question, cheap to answer" framing:
1. **0.1.x is pre-1.0** — no API-stability guarantee; committing a phase around it before a spike
   risks building against an interface that moves.
2. **It answers zero questions in Sections A or C.** Do not let its adoption for OCR-routing (a
   real, separable win per SEED-021 Opportunity 1) get conflated with the PDF risk-detection need
   this milestone actually has — those are answered by `pdfcpu`, not `pdf-inspector`.

Also worth restating plainly for the roadmap: **`pdfcpu`'s "content" extraction is *not* a
`pdftotext`/`pdf2md` substitute** — it extracts raw PDF content-stream operators, not decoded
plain text. If SEED-021 is harvested later, `pdf2md`/`pdftotext` remain the live candidates for
*text extraction*; `pdfcpu` stays scoped to *structure/security* (page count, encryption,
attachments, action dictionaries). Don't let one library's adoption for A/C be read as solving B.

---

## C. PDF and Office sanitization

| Tool | Version | License | Offline | Debian bookworm (amd64+arm64) | Can it *verify* its own output? |
|---|---|---|---|---|---|
| **pdfcpu** (recommended) | v0.14.0 | Apache-2.0 | Yes | N/A — pure Go, no apt package needed | **Yes, uniquely** — the same library that detects `/JS`/`/OpenAction`/`/EmbeddedFile` for inspect (Section A) can immediately re-walk the *sanitized output* before it's written to S3 and fail closed if any residual risk key remains. No other candidate in this table has that property in-process. |
| qpdf | 12.3.2 upstream / **11.3.0-1+deb12u1** Debian bookworm | Apache-2.0 (since v7, 2017) | Yes | Yes, standard multi-arch apt package | No built-in "remove JS/OpenAction/EmbeddedFile" flag found (searched issue tracker — users ask for this, e.g. #1312, no shipped answer). Strong at repair/linearize/decrypt/encrypt, weak as a sanitizer per se. |
| mutool (MuPDF) | 1.27.2 upstream / **1.21.1+ds2-1+b4** Debian bookworm (`mupdf-tools`) | **AGPLv3** or commercial (Artifex) | Yes | Yes, standard multi-arch apt package | `mutool clean` strips document metadata **by default** (only `-m` *preserves* it) and `-s` sanitizes content-stream *graphics* operators — but has **no flag for `/JS`/`/OpenAction`/`/EmbeddedFile` removal** (checked the full flag list). Same structural gap as qpdf. |
| exiftool | 13.53 | Artistic-1.0-Perl OR GPL-1.0+ | Yes | Needs `perl` added to a container (not in bookworm-slim's minimal set) | Good for EXIF/IPTC/XMP/PDF-Info stripping (`-all=`) but carries a documented RCE history (e.g. CVE-2021-22204, arbitrary code execution via a crafted DjVu thumbnail) — a real concern for a service whose entire threat model is untrusted uploads. Runtime cost: interpreter + hundreds of format-specific Perl modules. |
| Ghostscript | 10.07.1 | **AGPLv3** or commercial | Yes | Yes, apt | Rewriting through `-sDEVICE=pdfwrite` drops non-visual actions as a *side effect* of re-rendering, not a designed sanitize guarantee, and is lossy (can alter visual output). AGPL raises a licensing question worth flagging to whoever owns legal review — not resolved here, out of scope for engineering. |
| pikepdf | 10.10.0 / 10.11.0 | MPL-2.0 | Yes | Wheels for linux x86-64 + arm64 | Python wrapper **around qpdf's own C++ engine** — adds a Python runtime for no capability qpdf/pdfcpu don't already give directly. Redundant given the two Go/CLI options above. |
| Hand-rolled pure-Go PDF editor | — | — | — | — | **Rejected.** This is precisely the complexity class (compressed xref/object streams) the project's existing zero-dependency wins (PNG/TIFF headers, ZIP central directory, RIFF chunks) do *not* resemble — see Section A's reasoning. |

### Recommended sanitize pipeline

- **PDF (macros N/A, JS/OpenAction/AA/EmbeddedFile/external refs):** `pdfcpu` Go API — delete
  `/OpenAction` from the Catalog, remove the `/Names /JavaScript` tree, strip `/AA` from the
  Catalog and every page/annotation, remove `/URI` and `/GoToR` actions, call attachments-remove
  for `/EmbeddedFile`s, then **re-run the exact same detection walk built for Section A against
  the output** before upload — fail closed if anything remains. Report `role=report` output lists
  what was found and what was removed (first real consumer of the `role` model per PROJECT.md).
- **PDF document-info / XMP:** same `pdfcpu` dependency — clear `/Info` dictionary entries and
  drop the `/Metadata` XMP stream reference from the Catalog. No second tool needed.
- **OOXML macros:** **Go stdlib `archive/zip` + `encoding/xml` — zero new dependency**, extending
  the exact pattern `internal/convert/docsniff.go` already uses to *detect*
  `word|xl|ppt/vbaProject.bin` and ODF `Basic/`-prefixed parts. Sanitize = copy every zip entry
  except the macro-storage parts, plus removing the corresponding `<Override>` in
  `[Content_Types].xml` and the relationship entries pointing at them (mirrors
  `ocrPageMerger`'s discipline of building output via a real encoder, never string
  concatenation, per PROJECT.md/SEED-021 breadcrumb). **Verification is free**: re-run
  `SniffContainer(output).HasMacro` — the literal existing function — against the sanitized
  bytes. This is the cheapest and most rigorous "prove it's gone" story in the whole milestone.
- **Image EXIF/GPS/IPTC/XMP:** `vips copy inPath outPath[strip]` — a one-argument change to the
  **already-existing** `LibvipsConverter.Convert` call in `internal/convert/libvips.go`, through
  the **already-hardened** `runCommand`. Zero new dependency, zero new binary, zero new container.
  Verification: add a minimal pure-Go **presence-only** marker check (JPEG `APP1`/`Exif`,
  `APP1`/`http://ns.adobe.com/xap` XMP, Photoshop IRB IPTC segment; PNG `eXIf`/`tEXt`/`iTXt`
  chunks) in the same family as `dimensions.go` — detecting *presence*, not decoding field values,
  is exactly the same bounded-header-read complexity class as the code already there, so it's a
  legitimate zero-dependency addition (unlike the PDF case above).

### Explicit what-NOT-to-adopt for Section C

- **Ghostscript** — AGPL licensing question unresolved, lossy rewrite, not a designed sanitizer.
- **exiftool** — new interpreted-language runtime + a real historical RCE surface on
  untrusted-input parsing, for a capability `vips[strip]` and a small pure-Go presence-check
  already cover for images, and `pdfcpu` covers for PDF `/Info`/XMP.
- **pikepdf** — new Python runtime for zero capability beyond what `pdfcpu` or direct `qpdf`
  already provide.
- **qpdf/mutool as the primary sanitize tool** — both lack a built-in JS/OpenAction/EmbeddedFile
  strip (verified against their own docs/issue trackers), so adopting either still means writing
  the same custom object-graph-editing code `pdfcpu` requires, without `pdfcpu`'s advantage of
  being a Go library that fits `CGO_ENABLED=0` and can double as the Section A detector. Keep
  `qpdf` in mind only as a fallback if implementation reveals a `pdfcpu` capability gap — not as
  a parallel primary dependency (running both is explicitly the worst outcome, per SEED-021's own
  framing of the analogous `pdftotext`-vs-`detect-pdf` question).

---

## D. Structured error codes (short, per the question's instruction)

No framework recommendation — none is warranted. The Go community convention for stable,
machine-readable API error codes is:

1. A small exported `type Error struct { Code string; Msg string; Err error }` implementing
   `error` and `Unwrap() error` (stdlib `errors` package, Go 1.13+ wrapping).
2. Package-level sentinel `var Err<Reason> = &Error{Code: "quota_exceeded", ...}` (or a plain
   `errors.New` wrapped at the boundary) — checked with `errors.Is`/`errors.As`, exactly as
   `internal/jobs/repo.go`'s `ErrNotFound` and `internal/api/handlers.go`'s `http.MaxBytesError`
   handling already do today.
3. The HTTP (and MCP) boundary layer does one `errors.As` translation to `{status, error_code,
   message}` — this project's own `quota_exceeded` (v1.12) is already exactly this pattern, just
   not yet generalized past ad hoc string literals per handler (the `invalid_options`
   metrics-gap SEED-020 names is a direct symptom of that lack of generalization).

This requires **zero new dependencies.** The relevant prior art worth naming for the roadmap
(not adopting, just naming as convention validation): `k8s.io/apimachinery`'s `apierrors` package
and Stripe's Go client both use this exact sentinel-plus-code-field shape without any external
error-handling library. SEED-020's JobRunner is the right place to centralize the mapping — that
is a Go-internal refactor, not a stack decision.

---

## E. Bounded synchronous path — stack implications (mechanism only, not policy)

Two concrete stack elements answer "what does the sync path need that it doesn't have," both
already present in the module graph:

1. **`context.WithTimeout`** (stdlib) — the same `ctx`-first convention every handler already
   follows. The work ceiling for inspect is a deadline on the request context, propagated into
   the `pdfcpu` parse calls exactly as `avProbeTimeout` already bounds `ffprobe` in the worker.
2. **`github.com/go-chi/httprate` v0.15.0 — already a direct dependency** (`go.mod`, already used
   for existing rate limiting). `/v1/inspect` needs its **own** limiter instance/key, separate from
   the existing per-client limiter and entirely separate from the v1.12 Postgres admission gate it
   bypasses by design — this is a second instantiation of a library already in the tree, not a new
   one.

One additional stdlib primitive worth naming as defense-in-depth (not a substitute for #1/#2):
`runtime/debug.SetMemoryLimit` (Go 1.19+, stdlib) — a coarse, process-wide soft memory cap. It
protects the API process as a whole from a single pathological inspect request, but it is
process-scoped, not request-scoped, so it complements rather than replaces bounding the *input*
before the expensive parse (the same "bound before you touch it" discipline `enforceMaxPages`/
`enforcePageBoxBound`/`enforceImagePixelBound` already establish in `ocrguards.go`): read
`pdfcpu`'s cheap page-count/MediaBox info **before** any full-document object-graph walk, exactly
mirroring the existing `pdfinfo`-cap-runs-first pattern from `ocrguards.go`.

No new dependency is implied by this section beyond the `pdfcpu` adoption already justified in
Section A.

---

## Recommended Stack — summary tables

### Core additions

| Technology | Version | Purpose | Why Recommended |
|---|---|---|---|
| `github.com/pdfcpu/pdfcpu` | v0.14.0 (2026-08-03) | PDF page count, encryption status, embedded-file listing, and (with custom code on top of its parsed object model) JS/OpenAction/AA/remote-reference detection *and* sanitize-and-verify | Only correct-and-cheap way to answer PDF risk signals without either shelling out from the API container or hand-rolling a compressed-object-stream PDF parser; pure Go, Apache-2.0, `CGO_ENABLED=0`-compatible, doubles as the sanitize verifier |

### Supporting code (not libraries — new pure-Go files following existing conventions)

| File (new) | Purpose | Pattern mirrored |
|---|---|---|
| `internal/convert/avprobe.go` (name TBD) | Bounded MP4/MOV/RIFF/EBML duration+resolution+declared-codec parse for the sync inspect path | `avsniff.go`'s box/vint walkers, `dimensions.go`'s declared-value discipline |
| `internal/convert/imgmeta.go` (name TBD) | Presence-only EXIF/XMP/IPTC marker check (pre- and post-sanitize) | `dimensions.go`'s fixed-offset, bounded-peek discipline |
| OOXML macro-stripping helper alongside `docsniff.go` | Zip-copy-minus-macro-parts + Content_Types/rels edit | `SniffContainer`'s single-pass, stdlib-only `archive/zip` discipline; `ocrPageMerger`'s real-encoder-not-string-concat discipline |

### Installation

```bash
# Core new Go dependency
go get github.com/pdfcpu/pdfcpu@v0.14.0

# No new apt packages in Dockerfile.api
# No new apt packages needed in any worker for this milestone's committed scope
```

## Alternatives Considered

| Recommended | Alternative | When the alternative would actually be better |
|---|---|---|
| `pdfcpu` (Go library) | `qpdf` CLI (Apache-2.0, in Debian bookworm) | If a `pdfcpu` Go-API gap is discovered during implementation (e.g. some encryption edge case), `qpdf` is a reasonable *fallback subprocess* in the async sanitize worker (which already runs `runCommand`) — never in the API container, and never running alongside `pdfcpu` for the same signal |
| Pure-Go container-header parsers for AV | `ffprobe` in the API container | If the product later needs exact codec/profile-level detail in the *synchronous* path (not just approximate declared values) — at that point this becomes a real architecture question (own build stage vs. cross-image artifact sharing), not a quick addition |
| `pdf-inspector`'s `pdf2md`/`detect-pdf` deferred (spike only) | Committing SEED-021 now | If the spike (cheap, per SEED-021's own framing) shows `detect-pdf`'s per-page classification reliably beats the current `pdftotext`-threshold heuristic on real mixed PDFs, *and* v0.1.x has reached a more stable release by then |

## What NOT to Use

| Avoid | Why | Use Instead |
|---|---|---|
| `poppler-utils`, `qpdf`, `mutool`, `ffprobe` in `Dockerfile.api` | API container has never run a subprocess; every one of these signals is answered by `pdfcpu` (Go library) or a bounded pure-Go parser instead | `pdfcpu` import + new pure-Go probe files |
| `exiftool` | Perl runtime + real RCE CVE history on untrusted-format parsing (CVE-2021-22204) for a capability already covered by `vips[strip]` (images) and `pdfcpu` (PDF Info/XMP) | `vips copy ...[strip]`, `pdfcpu`-based Info/XMP clearing |
| `pikepdf` | Python runtime wrapping the same `qpdf` engine already available directly; no unique capability | `pdfcpu` or direct `qpdf` |
| Ghostscript | AGPL licensing question, lossy rewrite, not a designed sanitizer | `pdfcpu` |
| Byte-scanning a PDF for `/JS`/`/OpenAction` without an object-graph parse | False-negative risk against compressed object streams — a silent security bypass, not a cosmetic gap | `pdfcpu`'s parsed `XRefTable` |
| `golang.org/x/image` or any general image-metadata library for the EXIF presence check | Same zero-dependency bias that already rejected `x/image` for decompression-bomb guards (PROJECT.md Key Decisions) — presence-only detection is the same complexity class as the existing hand-rolled dimension parsers | New pure-Go presence-only marker scan, `dimensions.go`-style |
| Committing SEED-021 (`pdf-inspector`) as a phase this milestone | 0.1.x pre-1.0 crate, unmeasured binary size, and zero overlap with this milestone's actual risk-signal need | Leave dormant; spike separately per SEED-021's own conclusion |

## Version Compatibility

| Package | Compatible With | Notes |
|---|---|---|
| `pdfcpu` v0.14.0 | Go 1.26.4 (project's pinned toolchain) | Pure Go, no CGO — no toolchain interaction risk beyond normal `go.sum` pinning |
| `pdfcpu` v0.14.0 | `CGO_ENABLED=0` static build (`Dockerfile.api`) | Confirmed compatible — pure Go module, same class as every other existing dependency |

## Sources

- `internal/convert/sniff.go`, `docsniff.go`, `dimensions.go`, `olecfb.go`, `ocrguards.go`,
  `av.go`, `avsniff.go`, `audiosniff.go`, `libvips.go`, `exec.go` — read directly, HIGH confidence
- `Dockerfile.api`, `Dockerfile.ocr-worker` — read directly, HIGH confidence
- Context7 `/websites/pdfcpu_io`, `/websites/pkg_go_dev_github_com_pdfcpu_pdfcpu` — command set,
  `api.PageCount`, `cli.ListAttachments`, `Info` signature — MEDIUM/HIGH
- [pdfcpu GitHub releases](https://github.com/pdfcpu/pdfcpu/releases) / pkg.go.dev versions page —
  v0.14.0, 2026-08-03 — MEDIUM
- [pdfcpu issue #653](https://github.com/pdfcpu/pdfcpu/issues/653) — confirms JS-removal is an
  open, unresolved feature request, not shipped — MEDIUM
- [qpdf GitHub](https://github.com/qpdf/qpdf), [qpdf license docs](https://qpdf.readthedocs.io/en/stable/license.html) — 12.3.2 upstream, Apache-2.0 since v7 — MEDIUM/HIGH
- [Debian bookworm qpdf package](https://packages.debian.org/bookworm/qpdf) — 11.3.0-1+deb12u1, multi-arch — MEDIUM
- [MuPDF license docs](https://mupdf.readthedocs.io/en/1.27.2/license.html), [mutool clean docs](https://mupdf.readthedocs.io/en/latest/tools/mutool-clean.html) — 1.27.2 upstream AGPLv3, full flag list checked — MEDIUM/HIGH
- Debian bookworm `mupdf-tools` (1.21.1+ds2-1+b4), `poppler-utils` (22.12.0-2+deb12u1) — MEDIUM
- [ExifTool version/license](https://en.wikipedia.org/wiki/ExifTool) — 13.53, Artistic-1.0-Perl OR GPL-1.0+ — MEDIUM
- [Ghostscript releases/licensing](https://ghostscript.com/licensing/) — 10.07.1, AGPLv3/commercial — MEDIUM
- [pikepdf GitHub/PyPI](https://github.com/pikepdf/pikepdf) — 10.10.0/10.11.0, MPL-2.0 — MEDIUM
- [firecrawl/pdf-inspector GitHub](https://github.com/firecrawl/pdf-inspector), crates.io — MIT, 0.1.7, no explicit dual-arch prebuilt binaries found — LOW/MEDIUM (project's own claims for network/size/timing, not independently measured)
- `.planning/seeds/SEED-021.md`, `.planning/seeds/SEED-013.md`, `.planning/PROJECT.md` — read directly, HIGH confidence

---
*Stack research for: OctoConv v1.13 Inspect & Operations*
*Researched: 2026-08-07*
