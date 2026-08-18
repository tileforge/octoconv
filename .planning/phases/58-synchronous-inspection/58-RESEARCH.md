# Phase 58: Synchronous Inspection - Research

**Researched:** 2026-08-10
**Domain:** Bounded pure-Go binary format parsing (PDF/OOXML/ODF/ISOBMFF/EBML/RIFF) inline in a synchronous, untrusted, request-scoped HTTP handler that shares a process with `/healthz`
**Confidence:** MEDIUM — stack/architecture claims are HIGH (verified against this repo's own code); several format-parsing byte-budget claims are MEDIUM (cross-verified against qpdf/ISO documentation via web search, not Context7 — this repo has no PDF/ISOBMFF dependency to consult); the k8s liveness-probe-under-load interaction is MEDIUM (derived from chart values + Go runtime behavior, not yet live-tested)

## Summary

INSP-01..06 asks for a synchronous, in-process, untrusted-byte parser that must never be allowed to threaten `/healthz` — while D-01/D-02 forbid both new binaries and new Go dependencies. The honest technical picture: **encryption detection, macro detection (already built), zip-bomb sizing (already built), and OLE-CFB classification (already built) are cheap, bounded, and reliable in pure Go.** PDF page count and the four action-surface findings (`/JavaScript`, `/OpenAction`/`/AA`, `/EmbeddedFile`, `/Launch`) are only *reliably* extractable when the PDF was NOT written with compressed cross-reference/object streams (PDF 1.5+, `/Type/XRef`, `/ObjStm`) — which a large and growing fraction of real-world PDFs are. The correct, honest design is not "parse harder" but "detect the compression signal itself and downgrade the whole PDF-findings block to `unknown` when it's present" — this is cheap (a single token scan) and turns INSP-03's `unknown` status into the actual right answer for a large class of real files, rather than a false `clean`.

MP4/MOV duration requires walking to the `moov` box, which lives at the END of the file for the vast majority of real-world un-optimized MP4s (only "fast-start" files place it first) — this is NOT a "peek the first N KiB" problem the way image dimensions are; it needs a `ReadAt` at the tail, which is available because uploads are backed by `multipart.File` (an `io.ReaderAt`), not a pure stream. Matroska/WebM duration is much cheaper: the `Info`/`Duration` element is written early by essentially all real encoders and was already reachable by this repo's existing `avPeekLen=4KiB` EBML walker pattern before format detection stopped early.

The single highest-value non-obvious finding from this research: **`/healthz`'s own handler pings Postgres** (`handlers.go:94`) through the **same connection pool** `PreAdmit` (INSP-04's quota check) would use. Moving `/healthz` to its own `http.Server`/listener (D-06 mechanism 3) does NOT isolate it from a pool-exhaustion attack via a burst of `inspect` requests each calling `PreAdmit` — the four D-06 mechanisms protect CPU/goroutine/byte exposure but say nothing about the shared `*pgxpool.Pool`. This must be closed explicitly (own bounded pool, or route `PreAdmit` calls through the same semaphore that gates inspect concurrency) or D-06's "isolates `/healthz` completely" claim is false by construction. Separately: **the `api` Deployment carries no `resources.limits` at all**, in Helm or in compose — unlike every worker, which is CPU/memory-capped. The in-process byte+time ceiling is therefore not a second line of defense against a runaway inspect parser, it is the ONLY line of defense; there is no cgroup backstop.

**Primary recommendation:** Build a closed, versioned findings vocabulary (mirroring `internal/errcode`'s doc-sync-test pattern) with three severity tiers; write PDF/OOXML/MP4/EBML parsers as pure byte-scanners with hard iteration/byte caps (mirroring `cfbWalkDirectory`'s visited-set-plus-cap discipline, not `context`-only bounding, since Go's tight-loop async preemption prevents starvation but not slowness); gate `/healthz` isolation on BOTH a second listener AND a bounded/separate Postgres access path for `PreAdmit`; treat "PDF uses compressed xref/object streams" as the primary trigger for `unknown` rather than trying to parse through it.

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01: Pure Go only, zero new binaries in `Dockerfile.api`.** Cheap/bounded parses run inline in Go; everything else honestly returns `unknown` from INSP-03's three-value scale. Direct consequence of the milestone non-goal (poppler/qpdf/mutool/ffprobe/exiftool forbidden in `Dockerfile.api`) and the fact that the API container today carries only `ca-certificates`. Only existing pure-Go extractor: `Dimensions` (`internal/convert/dimensions.go:50`, 64 KiB peek), already inline in the API path (`handlers.go:509`). Everything else — PDF pages (`ocrguards.go:67` via `pdfinfo`), audio duration (`audioduration.go:80` via `ffprobe`), video resolution/codec (`avduration.go:83` via `ffprobe`) — are worker-only shell-outs. No office page/sheet counter exists anywhere.
- **D-02: Hand-written parsers on stdlib; no third-party libraries.** Continues the project's existing manual/bounded sniffing line (`SniffContainer` `docsniff.go:69`, `ClassifyCFB` `cfb.go:64`, `matchEBML` `avsniff.go:141`, `readSniffPrefix` `markupsniff.go:47`). Reason stronger than usual: the parser eats untrusted bytes inline in the SAME process serving `/healthz`, so byte/allocation control matters more than format coverage. `unknown` is a correct, expected answer for an unwritten parser, not a failure.

  **⚠️ Contradicts STATE.md.** `.planning/STATE.md` (Operator Next Steps, written before this phase's `/gsd:discuss-phase`) records: *"One new Go dependency planned: `github.com/pdfcpu/pdfcpu` v0.14.0 ... justified specifically on `/JS`/`/OpenAction`/`/EmbeddedFile` detection inside compressed object streams... It also doubles as the sanitize verifier."* CONTEXT.md D-02 (dated 2026-08-10, this phase's locked decision) directly reverses that plan. **This research treats D-02 as authoritative** per this agent's brief (report the conflict, do not plan around a decision believed wrong). The planner and Phase 60 (sanitize) both need to know: the pdfcpu-based "parse through object streams" path this earlier plan assumed does not exist for Phase 58, and Phase 60's "independent verification parser" open design question (ROADMAP) may need to be revisited without pdfcpu as a candidate too, or D-02 needs to be scoped as "Phase 58 only." Flagging, not resolving — this is a discuss-phase-level conflict, not a research judgment call.

- **D-03/D-04: One closed findings vocabulary, two consumers (report-only in `inspect`, reject-subset in `handleCreateJob`); vocabulary declared now, reject-path migration deferred.** Mirrors `internal/errcode`'s `doc_sync_test.go`/`openapi_sync_test.go` pattern exactly (`TestErrorCodesDocMatchesGoConstants`).
- **D-05: No typed findings exist anywhere today.** Everything collapses to bool/error/422. Closest precedent: `CFBClass` (`cfb.go:22`), a 3-value enum consumed by a `switch`→422, not a report. New code entirely: PDF JavaScript, action dictionaries (`/OpenAction`, `/AA`), embedded files, external references for ANY format. Reused as-is: OOXML/ODF macros (`ContainerResult.HasMacro`), zip-bomb (`ContainerResult.TotalUncompressed`), CFB legacy/encrypted (`ClassifyCFB`).
- **D-06: Defense in depth — all four mechanisms together, not a choice of one.** (1) byte+time work ceiling per inspect (exhaustion → INSP-03 `unknown`/partial-coverage, NOT a reject); (2) semaphore on concurrent inspect parses, over-limit → immediate 429 machine-readable code; (3) `/healthz` on its own listener (today it shares `chi.Router`/`http.Server` with `/v1/*`, `routes.go:34`, `cmd/api/main.go:252`; precedent: `metricsSrv` already on its own port, `cmd/api/main.go:272`); (4) inspect's own rate budget, separate from the general per-client limit.
- **D-07: The rate budget inherits `internal/ratelimit`'s documented `replicas: 1` caveat** (`ratelimit.go:8-13`) — in-process/in-memory, upgrade path is `WithLimitCounter` over Redis. Must be recorded as an explicit caveat (mirroring `admission.go`'s own multi-replica disclosure), not silently inherited.
- **D-08: The new `/healthz` port is a deploy change** — must land in the Helm chart (liveness/readiness probes), `docker-compose.yml`, and `.env.example` in this phase, not later.
- **D-09: TOCTOU — inspect pins nothing.** No inspect result may let a later job-creation call skip a fail-closed check. Proven by a test that inspects one set of bytes and creates a job with different bytes.
- **D-10/D-11: Duration estimate is an upper bound from the per-engine timeout config, not a real estimate; the estimate MUST carry provenance (measured vs. unmeasured fallback).** No historical-duration data exists to build a real estimate from (`octoconv_job_duration_seconds` is write-only, never read; `client_usage_daily` has no duration column). Timeouts: audio 742s / av 753s / ocr 744s are RTF-measured; pandoc 90s(→65s per `.env.example`, see below) is an explicit unmeasured fallback. Client must be able to tell these apart.
- **D-12: The quota check (INSP-04) is `Repo.PreAdmit`** (`internal/jobs/admission.go:596`) — same `evaluate()` as the authoritative `admitLocked`, on the pool, no advisory lock, no transaction, no write. Two caveats the planner must carry: (a) not linearizable under concurrent burst — irrelevant for a single synchronous inspect, but must never be claimed linearizable; (b) it fires the quota-observer metric on rejection even here (`admission.go:616-618`) — not silent.
- **D-13/D-14: The live gate runs in k8s (OrbStack), not compose; the phase does not close without it.** `autonomous: false`.
- **D-15/D-16/D-17: Evidence discipline carried from Phase 57.** Pin pre-phase bodies of any endpoint this phase touches, first commit. Every new defense is proven by inversion (break it, watch the test fail, revert). Verification must read the phase GOAL, not just requirement text, and enumerate every path untrusted bytes take into the API process.

### Claude's Discretion

- Which formats get a real Go parser vs. always `unknown` — decided by "cheap and bounded," not user preference (this research answers it, see Q1 table below).
- Numeric values for the work ceiling, semaphore size, and rate budget — calibrated by the live gate (SC6), not assigned here.
- JSON response shape (flat vs nested), field names, `/healthz` port number.
- Exact PDF page-counting and `/JavaScript`/`/OpenAction`/`/AA`/`/EmbeddedFile` detection algorithm under the byte cap.

### Deferred Ideas (OUT OF SCOPE)

- Migrating existing `handleCreateJob` 422 checks onto the new findings vocabulary (D-04) — candidate for Phase 61 or a follow-up task.
- Real historical-duration estimation (percentile rollup by engine/size) — D-10, deliberately out of this phase's size.
- Redis-backed rate limiter removing `internal/ratelimit`'s `replicas:1` caveat service-wide.
- BDPD-01 (PDF text-layer detection) — belongs to Phase 59, though it is technically one more field on the same report.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INSP-01 | Magic-byte format detection + per-format characteristics (size always; pages for PDF/office; duration/resolution/codec for media; pixels for images) | Q1 table below gives exact byte budgets/offsets/APIs per format; office page/sheet count is a new `unknown`-by-default gap (no cheap bounded algorithm found — see Q1 "not worth attempting") |
| INSP-02 | Typed closed findings vocabulary: code, severity, location | Q3 gives a concrete vocabulary + 3-tier severity scale cross-checked against `docs/ERROR-CODES.md`'s conventions (Go-source-of-truth + doc-sync test + open-string consumer contract) |
| INSP-03 | Three-state per-check status (clean/not_applicable/unknown) + explicit partial-coverage flag under the work ceiling | Q2 gives SARIF's `kind` enum as directly-reusable prior art; Q1 gives the concrete trigger (`/ObjStm` token present → PDF findings block reports `unknown`, never `clean`) |
| INSP-04 | Available `(operation, target)` + engine + duration estimate (with provenance) + live quota check | `Repo.PreAdmit` (`admission.go:596`) is the quota check; duration estimate = per-engine timeout config value + a measured/unmeasured flag (`.env.example` comments already carry this data) |
| INSP-05 | Independent bounds: work ceiling, body ceiling, own rate budget, machine-readable reject code; TOCTOU-safe | Q4 gives Go-specific bounding mechanisms (iteration caps > context-alone; buffered-channel semaphore, no new dependency) |
| INSP-06 | Live-gate-proven process isolation | Q5 gives the harness shape to reuse (`quota-multireplica-liveproof.sh` precedent) and the concrete liveness-probe numbers the gate must respect (`periodSeconds:10`, `failureThreshold:6` → 60s of continuous failure trips a restart) |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Magic-byte format detection | API / Backend | — | Already lives inline in `internal/api/handlers.go` via `internal/convert` sniffers; `inspect` reuses the exact same code path, no new tier |
| Bounded characteristic extraction (pages/duration/dims) | API / Backend | — | Must run inline (D-01) in the same process as `handleCreateJob`'s existing `Dimensions` call — no worker hop allowed by design |
| Findings evaluation (macros/JS/action-dicts/embedded/external-refs/encryption/zip-bomb/CFB) | API / Backend | — | Same constraint — synchronous, in-process, no queue |
| `(operation, target)` enumeration | API / Backend | — | Reads the existing `OperationalConverter` registry (Phase 57) in-process, no I/O |
| Duration estimate | API / Backend | — | Static read of per-engine timeout config (env-sourced at startup), no I/O |
| Quota check | API / Backend | Database / Storage | `Repo.PreAdmit` reads Postgres directly (read-only, no reservation) — this is the one capability in this phase with a real external dependency, and the one that risks contending with `/healthz`'s own Postgres ping (see Common Pitfalls) |
| `/healthz` liveness | API / Backend | — | Currently coupled to Postgres/Redis/S3 pings; D-06 mechanism 3 moves its HTTP exposure to a second listener but does NOT by itself decouple its dependency pings from load the inspect path puts on those same dependencies |
| Rate/concurrency bounding | API / Backend | — | In-process semaphore + `internal/ratelimit`-style limiter; explicitly NOT Redis-backed this phase (D-07 caveat) |

## Q1 — What a bounded pure-Go parser can actually extract, per format

### Summary table

| Format | Characteristic/Finding | Cheap & bounded? | Byte budget / offset | Confidence |
|---|---|---|---|---|
| PDF | Page count (fast path: `/Count` in the `/Type/Pages` root dict) | Yes, IF the Pages dict is not itself inside an `/ObjStm` and the file uses a classic plaintext xref | Scan for `trailer`→`/Root`, or fall back to scanning the whole bounded window for `/Type/Pages` ... `/Count <n>` — a few hundred bytes to low KiB in practice | MEDIUM |
| PDF | Page count (fallback: count `/Type/Page` occurrences, NOT `/Type/Pages`) | Approximate only — overcounts on incremental updates (stale objects from earlier revisions still match), undercounts when compressed | Whole bounded window (see cap below) | MEDIUM — degrade to `unknown` when `/ObjStm` token is present (see below) |
| PDF | `/JavaScript`, `/JS`, `/OpenAction`, `/AA`, `/EmbeddedFile`, `/Launch`, `/URI` | Yes for the literal token scan itself (cheap); **NOT reliable as a `clean` verdict** — PDF 1.5+ object streams (`/ObjStm`) can hide ANY of these from a byte scan (stream objects and the encryption dictionary are the ONLY things ISO 32000 forbids inside an `/ObjStm` — everything else, including the whole action/JS-carrying dictionaries, is legal there) | 1 MiB is a reasonable cap for a first pass; token presence anywhere flips the whole PDF-findings block to `unknown` rather than `clean` | MEDIUM (cross-verified via qpdf docs + ISO 32000 restriction list, not Context7 — no PDF library in this repo to consult) |
| PDF | `/Encrypt` (encryption) | Yes, reliably — encryption dictionaries are explicitly forbidden inside `/ObjStm` by spec (7.5.7), and the trailer/XRef-stream dict that carries `/Encrypt` is never itself a compressed object (a stream object cannot live inside another object stream) | Trailer is conventionally at file tail (classic) or the XRef stream dict is a normal top-level object (PDF 1.5+); a tail `ReadAt` of the last few KiB usually suffices, cross-checked against a scan for `/Encrypt` anywhere in the bounded window | MEDIUM-HIGH |
| PDF | `/ObjStm` / `/Type/XRef` presence (compression-signal detector) | Yes — trivial literal token scan, the cheapest and most useful single check in this whole phase | Same bounded window as the findings scan | HIGH (structural fact, not content-dependent) |
| OOXML/ODF | Macros (`vbaProject.bin`, `Basic/` prefix) | Yes — already built (`SniffContainer`/`docsniff.go:69`), zero new code | Central-directory-only, no decompression | HIGH (existing, tested code) |
| OOXML/ODF | Zip-bomb shape (declared uncompressed total) | Yes — already built (`ContainerResult.TotalUncompressed`) | Central-directory-only | HIGH |
| OOXML/ODF | Page/sheet count | **No.** Requires parsing `word/document.xml`'s section breaks, `xl/workbook.xml`'s sheet list (cheap-ish, just XML parsing of one small part) vs. LibreOffice's actual rendered page count (which depends on page size, margins, and content reflow — NOT derivable from XML alone). Sheet COUNT for xlsx is genuinely cheap (parse `xl/workbook.xml`'s `<sheets>` list — small, uncompressed-relative-to-content, single zip entry read); page count for docx/pptx is not | Sheet count: one small zip-entry read. Page count: not worth attempting | MEDIUM (sheet count), report `unknown` for true page count |
| OOXML/ODF | External references (OOXML `externalReference`/`oleObject` relationship types in `.rels` parts; ODF `xlink:href` with a URL scheme) | Partially — the `.rels` XML parts are small and already inside the zip central directory this phase already opens; a literal substring scan for `TargetMode="External"` in each `.rels` part is cheap once those parts are identified | One more small-entry read per `.rels` part found; entries are typically <10 KiB | MEDIUM — new code, not yet written anywhere |
| MP4/MOV | Duration (`mvhd` box: version/flags, creation/mod time, timescale, duration) | Yes, but requires reading the TAIL of the file, not the head — `moov` (which contains `mvhd`) is written at the END of the file by the overwhelming majority of real-world non-"fast-start"-optimized MP4s (ffmpeg's default `-movflags` does NOT relocate `moov` to the front unless `+faststart` is explicitly passed) | Read the last ~2 MiB via `io.ReaderAt` (the upload is `multipart.File`, an `io.ReaderAt`, not a pure stream — tail seeking is available) and walk boxes backward-compatible-forward from wherever `moov` is found; `mvhd` itself is a small fixed-layout box once `moov` is located | MEDIUM (box layout HIGH per ISO/IEC 14496-12 cross-verification; tail-vs-head placement MEDIUM, general knowledge not spec-verified this session) |
| Matroska/WebM | Duration (`Segment` → `Info` → `Duration` float element) | Yes — real encoders write `Info` (and `Duration` inside it) early in the `Segment`, well within a bounded head-peek; this repo's existing `avPeekLen=4KiB` EBML walker (`avsniff.go:62`) already proves the shape is walkable, it just stops at `DocType` today and would need to continue into `Segment`/`Info` | 4-16 KiB peek is very likely sufficient based on real-file observation (mkv/webm headers before `DocType` measured under 60 bytes per this repo's own comment, `avsniff.go:56-58`); Info placement not independently spec-verified this session | LOW-MEDIUM — not independently verified against a spec citation for placement guarantee; recommend fail-closed to `unknown` if `Duration` element isn't found within the cap rather than growing the buffer |
| MP3/WAV/OGG | Duration | **No — genuinely not worth attempting in pure Go without a real dependency.** MP3 (VBR especially) has no reliable declared-duration field without either a Xing/VBRI header (not always present) or summing frame sizes (requires walking the whole file); WAV's `data` chunk size ÷ byte rate IS cheap and bounded (RIFF header, fixed offset) but WAV is the exception, not the rule for this engine class; OGG duration requires reading the LAST page's granule position, which — like MP4 — needs a tail read, and Vorbis-vs-Opus differ in sample-rate interpretation | WAV: cheap, RIFF header only. MP3/OGG: report `unknown` | MEDIUM (WAV), HIGH-confidence "not worth attempting" for MP3 VBR duration |
| Images (png/jpg/webp/heic/tiff) | Pixel dimensions | Already fully built and inline (`Dimensions`, `dimensions.go:50`) | 64 KiB peek, existing code | HIGH (existing, tested) |
| Images | EXIF/GPS/IPTC/XMP presence (as an INSP-02 finding, not full parse) | Partially — presence of an EXIF/XMP segment is a cheap marker scan (JPEG APP1 `Exif\0\0` / `http://ns.adobe.com/xap/1.0/`; PNG `eXIf`/`iTXt` chunks) reusing the exact marker-walk this repo's `jpegDimensions`/`pngDimensions` already do | Same 64 KiB window `Dimensions` already reads | MEDIUM — new code, small addition to existing walkers |

### What is genuinely NOT worth attempting in pure Go (report `unknown` always)

- **MP3 exact duration without Xing/VBRI header** — would require a full-file frame walk; no bounded shortcut exists for genuine VBR files.
- **OOXML/ODF true rendered page count** — depends on layout engine output (fonts, margins, reflow), not extractable from XML structure alone; this is exactly why `pdfinfo`/LibreOffice exist as separate binaries today and why D-01 forbids adding them to the API container.
- **PDF page count/findings when `/ObjStm` is present and the parser hasn't decompressed it** — decompressing FlateDecode object streams is itself not hard (stdlib `compress/zlib` handles the codec), but CORRECTLY locating and validating the object-stream's own N/First header fields, resolving object offsets, and doing this recursively (an ObjStm can itself reference objects needed to find other ObjStms) is a meaningfully larger, more failure-prone parser than this phase's byte budget justifies — this is precisely the gap the now-reversed pdfcpu dependency was meant to close (see D-02 contradiction above). Recommend: detect and report `unknown`, do not attempt.
- **Codec profile/level detail (H.264 profile/level, AAC sample format)** — already correctly out of scope per REQUIREMENTS.md INSP-V2-02; report only the ffprobe-free "declared codec fourCC/name" the container itself states (e.g., MP4 `stsd` sample entry type, EBML `CodecID` element) — this IS cheap (single small element read adjacent to the boxes already being walked for duration).
- **Video resolution from MP4** — cheap actually: `tkhd` (track header) box carries declared width/height directly, same tail-of-file `moov` walk as duration; no separate mechanism needed once `moov` is located.

## Q2 — Prior art for the three-state check model (INSP-03)

**SARIF (OASIS Static Analysis Results Interchange Format) `result.kind` is directly reusable prior art.** SARIF 2.1.0's `kind` property is a closed enum: `notApplicable`, `pass`, `fail`, `review`, `open`, `informational` [CITED: docs.oasis-open.org/sarif/sarif/v2.1.0]. This maps cleanly onto INSP-03's three states (`clean`≈`pass`, `not_applicable`≈`notApplicable`, `unknown`≈ no direct SARIF equivalent, closest is an absent result plus a tool-execution notification).

**Partial/truncated-scan signaling** is NOT part of SARIF's per-result `kind` — it lives at the `invocation` level (`executionSuccessful`, `toolExecutionNotifications`) or the `run.properties` bag, i.e., SARIF's own design separates "was this individual check's answer confident" from "was the whole scan complete." This is the right shape for INSP-03's two distinct claims:

1. **Per-check status** (one of exactly 3 values, applies to EACH named finding-code): `clean` | `not_applicable` | `unknown`.
2. **Report-level coverage flag** (applies ONCE, to the whole response): `complete` | `partial` (with a reason: `work_ceiling_bytes`, `work_ceiling_time`, `unsupported_format`).

A report cannot honestly claim `complete` coverage while any individual check is `unknown` for a reason OTHER than "format-inapplicable" — e.g., if the PDF findings block hit the `/ObjStm`-present downgrade, that individual check is `unknown` for a structural/format reason (not a resource-ceiling reason), so the report-level flag can still legitimately be `complete` (the checker did everything it could, bounded or not, and honestly reports what it couldn't determine) while `work_ceiling_bytes` is reserved specifically for "we stopped early because of OUR OWN resource limit," which is the actual DoS-relevant distinction Phase 58 exists to make honest. Keep these two concepts separate in the JSON shape — do not fold "format doesn't apply," "we can't parse this," and "we ran out of budget" into one enum value; a planner designing the response schema should give at minimum a `status` (3-value) plus a `reason` (open string / small enum) per finding, and a top-level `coverage: {complete: bool, truncated_checks: [...]}`.

**A second precedent worth naming**: security scanners like Trivy/Grype use an `UNKNOWN` severity bucket precisely to avoid a truncated scan looking like a clean one — same instinct, less formally specified than SARIF. Recommend SARIF's `kind` naming (or a close synonym) over ad hoc naming, since it is the more citable, versioned standard and this project already favors "align with an established closed-vocabulary convention over inventing one" (`docs/ERROR-CODES.md`'s own framing).

## Q3 — Findings vocabulary shape (INSP-02)

### Recommended vocabulary (closed, Go-source-of-truth, doc-sync-tested — mirrors `internal/errcode`)

| Code | Applies to | What it means | Location field |
|---|---|---|---|
| `ooxml_macro` | docx/xlsx/pptx/odt/ods/odp | VBA/Basic macro-carrying part present (`ContainerResult.HasMacro`, reused as-is) | zip entry path |
| `zip_bomb_shape` | any zip container | Declared uncompressed total exceeds a findings-level threshold (distinct from `handleCreateJob`'s hard reject threshold — inspect reports even a milder ratio as a finding) | — |
| `ole_cfb_legacy` | doc/xls/ppt (legacy binary) | `ClassifyCFB` returned `CFBLegacy` | — |
| `ole_cfb_encrypted` | any OLE-CFB | `ClassifyCFB` returned `CFBEncrypted` | — |
| `pdf_javascript` | PDF | `/JavaScript` or `/JS` token found | byte offset (best-effort) |
| `pdf_action_dict` | PDF | `/OpenAction` or `/AA` token found | byte offset |
| `pdf_launch_action` | PDF | `/Launch` token found | byte offset |
| `pdf_embedded_file` | PDF | `/EmbeddedFile` token found | byte offset |
| `pdf_external_reference` | PDF, OOXML, ODF | `/URI` (PDF) or `TargetMode="External"` relationship (OOXML/ODF) found | byte offset / rels part path |
| `pdf_encrypted` | PDF | `/Encrypt` present in trailer/XRef-stream dict | — |
| `metadata_exif_present` | JPEG/TIFF/PNG/PDF | EXIF/XMP/`/Info`/GPS marker segment present | — |

### Severity: 3 tiers, not more

Rationale: this project's own `docs/ERROR-CODES.md` deliberately keeps error codes coarse ("granularity is by failure CLASS, not by call site") — the same discipline should apply to severity. A 5-level scale (info/low/medium/high/critical) invites bikeshedding about the boundary between adjacent levels with no operational difference in client behavior; 3 tiers map directly to the three DIFFERENT actions a client can honestly take:

| Severity | Meaning | Client action | Example codes |
|---|---|---|---|
| `info` | Structural fact, not itself a risk | None required | `ole_cfb_legacy`, `metadata_exif_present`, `pdf_encrypted` (encryption without other findings is a privacy signal, not code-execution) |
| `warning` | Passive risk — content that could be exfiltration/tracking vector but requires the recipient to act | Consider `sanitize` before further processing | `pdf_external_reference`, `pdf_embedded_file`, `ooxml_macro` (a macro without evidence of auto-run) |
| `critical` | Active/auto-triggered risk — code that can execute without further recipient action | Strongly recommend `sanitize` or reject | `pdf_javascript`, `pdf_action_dict`, `pdf_launch_action`, `zip_bomb_shape` (resource-exhaustion, not content risk, but "auto-triggered" in the same sense — decompressing it is enough to cause harm) |

This directly informs Phase 60 (sanitize): the SANI-01 removal list (JS, action dicts, launch actions, embedded files, external URIs) is exactly the `warning`+`critical` tiers above — the vocabulary declared in Phase 58 should already be shaped so Phase 60 consumes it without renaming (D-04's whole point).

### Cross-check against `docs/ERROR-CODES.md` conventions

Adopt identically: (1) Go constants are the single source of truth; (2) a mechanical doc-sync test (`TestFindingsCodesDocMatchesGoConstants`, mirroring `TestErrorCodesDocMatchesGoConstants`) proves the doc and the Go `All()`-style enumerator never drift; (3) the **open-string consumer contract** — `docs/ERROR-CODES.md`'s Pitfall 12 explicitly forbids a closed TypeScript union or closed Go enum on the CONSUMER side, because future phases add codes non-breakingly. The same must apply to findings codes: MCP servers, the frontend, and Phase 60's sanitize report all consume this vocabulary and must each carry a default/unknown-tolerant branch from day one.

## Q4 — Bounding an inline untrusted-byte parser in Go

### Byte ceiling

`io.LimitReader`/`io.LimitedReader` on the initial multipart body read is necessary but not sufficient by itself for a `ReaderAt`-based parser that jumps around (e.g., MP4 tail read) — those need an explicit `offset+length <= fileSize` bounds check before every `ReadAt`, exactly the discipline `cfbSectorInBounds` (`cfb.go:245`) and `walkBoxes` (`dimensions.go:295`, `i+size > len(buf)` check) already apply. **Recommendation: reuse `readSniffPrefix`'s envelope pattern (bounded `ReadAt`, never grows, never seeks past the declared cap) for every new parser rather than inventing a new bounding primitive.**

### Time ceiling — where `context` fails

`context.WithTimeout` cancels I/O-bound waits (network calls, `select` on a channel) but does **NOT** interrupt a CPU-bound loop that never returns to the scheduler or checks `ctx.Err()`. This project's own existing parsers (`cfbWalkDirectory`, `walkBoxes`, `matchEBML`) do not use `context` at all for their internal loops — they bound work via **hard iteration/sector caps plus a visited-set for cycle detection** (`cfbMaxDirSectors = 4096`, `visited[sector]` check). This is the correct, already-proven pattern for the new PDF/MP4/EBML-continuation parsers too: a fixed maximum object/box/element count per parse, not a context deadline as the primary bound. Use `context` as a courtesy outer bound (so the semaphore slot is guaranteed to release even if a bug slips through), not as the load-bearing mechanism.

**Go 1.14+ nuance worth knowing (not a substitute for the above):** since Go 1.14, the runtime performs asynchronous, signal-based goroutine preemption, so a genuinely infinite tight loop with no function calls will still be time-sliced with other goroutines (including the `/healthz` handler's own goroutine) rather than starving the process outright [MEDIUM confidence, general Go runtime knowledge, not independently re-verified against release notes this session]. This means the risk from an unbounded parser is **CPU contention/latency**, not indefinite starvation of `/healthz` — but with `GOMAXPROCS` effectively unbounded (see below) and no per-request CPU accounting, contention under a semaphore-full burst is still a real, measurable liveness-probe-latency risk, which is exactly what the byte/iteration caps exist to bound.

### Memory

The failure pattern to avoid: allocating a buffer sized by an **attacker-declared length field** before validating it against bytes actually available. Every existing parser in this repo already avoids this (`walkBoxes` checks `size64 > uint64(len(buf))` before trusting a box's declared size; `readSizeVint`/`readIDVint` in `avsniff.go` fail closed the instant a declared length would exceed the already-peeked buffer). New parsers (PDF stream `/Length`, MP4 box sizes at the tail, EBML `Segment`/`Info` continuation) must follow the identical rule: **never `make([]byte, declaredSize)`; always operate within an already-bounded, already-read buffer and reject when a declared offset/length would exceed it.**

### The second `/healthz` listener

Needs: (1) its own `http.Server` with its own `ReadHeaderTimeout` (mirrors `metricsSrv`'s existing pattern, `cmd/api/main.go:272-283`, `ReadHeaderTimeout: 10*time.Second`); (2) graceful shutdown ordering — shut down the `/v1` listener first (stop accepting new inspect load) then metrics, then healthz last, so a rolling deploy's liveness probe stays answering as long as possible during drain (mirrors the existing `httpSrv`→`metricsSrv` sequential shutdown at `cmd/api/main.go:290-295`, just extended to three servers); (3) **it must NOT eliminate `handleHealth`'s existing Postgres/Redis/S3 pings** (`handlers.go:94-113`) — those stay meaningful liveness signals — but see the Postgres-pool-contention pitfall below, which the listener split alone does not solve.

## Q5 — Live gate design (INSP-06/SC6)

### Reusable harness shape

`scripts/quota-multireplica-liveproof.sh` (Phase 55) is the closest and most recent precedent, and its documented history is directly instructive:

- **Evidence shape that worked**: one row-per-request TSV (`quota-multireplica-<RUN_ID>.tsv`) plus a derived per-job timeline TSV, both committed under `.planning/phases/<N>/evidence/`, named by a UTC timestamp `RUN_ID` so re-runs never clobber. Every assertion helper prints the observed value on success AND on failure, so the transcript is self-documenting (no "trust me" lines).
- **What made it credible**: real k8s (not compose — the whole point of D-13 here too), pre-built non-`latest`-tagged images built sequentially (OrbStack daemon-wedge history, `.claude` MEMORY.md `orbstack-k8s-live-proof`), teardown via `trap` on exit so the cluster is never left hot.
- **The recorded harness defect to NOT repeat**: round 4 of the 2026-08-07 run asserted `admitted == K` (an exact count) without first proving no admitted job could have gone terminal mid-burst — a legitimate slot-reuse race was indistinguishable from a real cap violation until the harness was fixed to (a) use a slow-enough fixture and (b) independently check `created_at`→terminal timestamps from Postgres as ground truth BEFORE trusting the exact-count assertion. **Direct analog for Phase 58's gate**: do not assert "`/healthz` returned 200 for every probe during the burst" as the sole pass condition — a single missed probe under `periodSeconds:10` is not a failure by itself (see below). Assert against the ACTUAL liveness contract (consecutive-failure threshold), and independently confirm via `kubectl get pods -o jsonpath='{.status.containerStatuses[0].restartCount}'` that no restart occurred — that is the ground truth a flaky single-probe read cannot substitute for, exactly as the 55-gate learned to use Postgres timestamps as ground truth instead of trusting the burst driver's own timing alone.
- **OrbStack-specific routing gotcha already discovered** (MEMORY.md `orbstack-clusterip-unreachable-from-host`): the ClusterIP is unreachable from the macOS host, and `kubectl port-forward` pins to a single backend pod — the burst driver must run from an **in-cluster throwaway pod**, not the host, exactly as Phase 55's script does (`BURST_IMAGE`, in-namespace curl pod). This applies identically here since the inspect burst must also hit the real Service, not a pinned single connection.

### k8s liveness probe numbers this gate must respect

From `deploy/chart/octoconv/templates/deployment-api.yaml:59-68` (verified live in this repo, not assumed):

| Probe | `periodSeconds` | `failureThreshold` | `timeoutSeconds` | Effective grace period |
|---|---|---|---|---|
| Readiness | 5 | 2 | unset → k8s default `1s` | ~10s of failed readiness pulls the pod from Service rotation |
| Liveness | 10 | 6 | unset → k8s default `1s` | ~60s of continuous liveness failure before a restart |

**The gate's real bar, precisely stated**: a pathological inspect burst must never cause `/healthz` to fail to respond within 1s, 6 times in a row (i.e., across a continuous 60-second window). A single slow probe recovering on the next tick is NOT a failure of D-06 — asserting zero-failures-ever would be a stricter bar than the actual production liveness contract and could produce a false "gate failed" on noise. Design the gate's pass condition around `restartCount` staying flat plus a bound on MAX CONSECUTIVE observed probe latency, not a zero-tolerance single-probe assertion.

## Q6 — Pitfalls specific to this phase (ranked by likelihood × cost)

### Pitfall 1: `/healthz`'s own Postgres ping shares the pool `PreAdmit` uses — the second listener does not fix this

**What goes wrong:** `handleHealth` (`handlers.go:94`) calls `s.health.Postgres.Ping(ctx)` under a 3s timeout. `PreAdmit` (INSP-04's quota check) reads from the SAME `*pgxpool.Pool`. D-06 mechanism 3 (separate `http.Server`/listener for `/healthz`) isolates the HTTP-accept path but does nothing about pool-connection contention — a burst of concurrent inspect requests each calling `PreAdmit` can exhaust the pool's connections/wait queue, delaying `Ping` past its 3s timeout and failing liveness even though every byte-parsing defense (D-06 mechanisms 1/2/4) worked perfectly.
**Why it happens:** "Isolate the listener" and "isolate the dependency" are two different claims, easy to conflate. No explicit pool sizing or dedicated-connection design exists for `/healthz`'s pings today (pgxpool default max-conns is unset in `internal/db/db.go`, defaulting to `max(4, runtime.NumCPU())` — and with no CPU limit on the api Deployment, `NumCPU()` reflects the node's full core count, not a deliberately-sized pool).
**How to avoid:** Either (a) route `PreAdmit` calls through the same semaphore gating inspect concurrency so pool pressure is bounded by the same knob, or (b) give `handleHealth`'s pings a small dedicated connection/pool exempt from inspect contention, or (c) explicitly size `pgxpool.Config.MaxConns` with enough headroom above the inspect semaphore's max concurrency and document the arithmetic. Do not ship D-06 mechanism 3 as if it were a complete answer to the Postgres path.
**Warning signs:** Live gate shows `/healthz` latency correlating with inspect burst concurrency even after the listener split; `pgxpool` wait-queue metrics (if added) spike during the burst.

### Pitfall 2: The `api` Deployment has NO `resources.limits` at all — the code-level ceiling is the only backstop

**What goes wrong:** Unlike every worker (`deployment-worker.yaml:63-66`, `.limits.cpu`/`.limits.memory` from `values.yaml`), the `api` Deployment template has no `resources:` block whatsoever, and `docker-compose.yml`'s `api` service similarly has no `cpus`/`mem_limit`. If the in-process byte/iteration ceiling has a bug or an edge case, there is no cgroup-level memory or CPU cap to fall back on — the process can grow until the NODE (not just the pod) is under pressure, which is a materially worse failure mode than a single OOM-killed worker pod.
**Why it happens:** The api process has never before run untrusted-byte parsing inline; its resource profile was "small, I/O-bound, cheap" until this phase.
**How to avoid:** This phase's D-08 ("new port is a deploy change") should be read to include evaluating whether the `api` Deployment now also needs a `resources.limits` block — at minimum this is a decision the planner must make explicitly (add limits, or explicitly accept the gap and say why), not an omission carried forward silently.
**Warning signs:** Live gate run with `kubectl top pod` unmonitored; no memory ceiling observed even under the intended pathological burst.

### Pitfall 3: PDF page-count "fallback" heuristic silently overcounts on incrementally-updated files

**What goes wrong:** A naive `/Type/Page` token count across the whole file counts every REVISION of every page object across every incremental update, not just the current, live set — a PDF edited/re-saved a few times by Acrobat can report page counts 2-5x too high.
**Why it happens:** Incremental updates append new object bodies and a new xref+trailer without removing the old bytes; a byte scan has no notion of "which xref section is authoritative."
**How to avoid:** Prefer the `/Count`-in-Pages-root fast path (walks from the LAST trailer backward, which incremental-update-aware readers already do); only fall back to the raw token count when the fast path fails, and when falling back, mark the resulting page-count characteristic itself with lower confidence (or, more honestly, report it as `unknown` rather than a specific wrong number) rather than presenting a plausible-looking but inflated integer as fact.
**Warning signs:** Test fixtures built as "save, edit, save again" in a real PDF editor produce a page count mismatch against the true count.

### Pitfall 4: Treating an `unknown`-status finding as equivalent to `not_applicable` in the JSON shape

**What goes wrong:** If the response schema doesn't clearly separate "this check does not apply to this format" from "we could not determine this," a client (or Phase 60's sanitize report) can silently treat a truncated/blind PDF scan as equivalent to "verified nothing here" — exactly the failure INSP-03 exists to prevent, reintroduced at the schema-design layer even after the parser itself is honest.
**Why it happens:** Three-value enums tempt a lazy client-side `if status != "clean"` check that conflates the other two.
**How to avoid:** Q2's SARIF-derived shape (separate per-check `status`+`reason` from report-level `coverage`); require the doc-sync/consumer-contract tests (Q3) to also assert that `unknown` and `not_applicable` are never collapsed in any consumer.
**Warning signs:** Frontend/MCP code review shows a `switch` with only two branches for a three-value field.

### Pitfall 5: `internal/ratelimit`'s `replicas:1` caveat quietly becomes a false sense of security for `api.replicas: 1` today, then a real gap the moment `api.replicas` is raised

**What goes wrong:** The chart's `api.replicas` is currently `1` (`values.yaml:33`), so the in-process rate budget genuinely works today. If a future phase raises `api.replicas` (plausible — Phase 55 already proved multi-replica quota correctness, so this project is heading toward `api.replicas: 2+`), the inspect rate budget silently stops being enforced fleet-wide with no compile-time or deploy-time signal.
**Why it happens:** Exactly the risk D-07 requires be recorded, not silently inherited.
**How to avoid:** Record the caveat next to the mechanism (mirroring `admission.go`'s pattern), and consider whether the live gate (which must run against a real cluster anyway) should ALSO assert single-replica behavior explicitly so a future `api.replicas` change is forced to re-examine this.
**Warning signs:** None until `api.replicas` changes — this is a documentation/process pitfall, not a code one.

## Package Legitimacy Audit

**Not applicable — this phase introduces zero new external packages by locked decision (D-02).** No `go.mod` changes are in scope. If a planner or future editor is tempted to add `github.com/pdfcpu/pdfcpu` (per the now-superseded STATE.md note) or any other PDF/media-parsing library to close a gap this research reports as `unknown`, that is a DIRECT contradiction of D-02 and must go back through `/gsd:discuss-phase`, not be added silently mid-plan.

## Common Pitfalls

See Q6 above — ranked by likelihood × cost per the research brief's request; not duplicated here.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | MP4 `moov` box is at the file tail for the "overwhelming majority" of real-world non-faststart uploads | Q1 | If wrong (e.g., internal service clients already produce faststart MP4s), the tail-read design is unnecessarily complex — a head-peek would have sufficed for THIS deployment's actual traffic. Low cost either way since the tail-read design also correctly handles head-placed `moov` (it can locate `moov` wherever it is, once given full-file `ReaderAt` access) |
| A2 | Matroska/WebM `Segment`/`Info`/`Duration` is reachable within a 4-16 KiB head peek for real-world files | Q1 | If wrong for some encoder, duration reports `unknown` for that file — safe failure mode (INSP-03 exists precisely for this), not a security risk, just reduced coverage |
| A3 | Go's async goroutine preemption (1.14+) prevents indefinite `/healthz` starvation from a CPU-bound inspect parser bug, reducing (not eliminating) the severity of a missing iteration cap | Q4 | If wrong / if this repo somehow builds with `GOEXPERIMENT` or a Go version predating this behavior, an unbounded loop could starve `/healthz` outright rather than merely add latency — verify the pinned Go toolchain (1.26.4 per CLAUDE.md) postdates 1.14 by a wide margin, so this is very low risk, but the iteration-cap discipline should be treated as load-bearing regardless, not as a nice-to-have this assumption would excuse skipping |
| A4 | `pgxpool` defaults to `max(4, runtime.NumCPU())` connections when `MaxConns` is unset, and `runtime.NumCPU()` reflects the node's core count absent a CPU limit on the api container | Pitfall 1 | If pgx's actual default differs by version (v5.10.0 pinned per go.mod), the pool-contention risk sizing in Pitfall 1 could be off in either direction — the qualitative risk (shared pool between `/healthz` ping and `PreAdmit`) holds regardless of the exact default number |
| A5 | PDF object streams (`/ObjStm`) cannot contain stream objects or the encryption dictionary, per ISO 32000 §7.5.7 restrictions, cross-verified via qpdf's own documentation rather than a direct ISO 32000 fetch | Q1 (encryption detection reliability) | If this restriction is misremembered/misquoted, encryption detection could have the same `/ObjStm` blind spot as the other findings — would mean `pdf_encrypted` needs the same `unknown`-on-`/ObjStm` downgrade as the other PDF findings, a straightforward fix if discovered wrong, not a security regression since the downgrade is conservative |

## Open Questions

1. **Does D-02's "no third-party libraries" scope apply ONLY to Phase 58, or does it retroactively invalidate the pdfcpu plan recorded in STATE.md for the whole milestone (including Phase 60's sanitize verifier)?**
   - What we know: CONTEXT.md D-02 is this phase's locked decision, dated 2026-08-10, and is unambiguous about Phase 58. STATE.md's pdfcpu note predates it and was written as a milestone-wide plan, explicitly citing "it also doubles as the sanitize verifier" (Phase 60).
   - What's unclear: whether the user intends D-02 to also foreclose pdfcpu for Phase 60, or whether Phase 58 specifically needed zero dependencies (synchronous/inline risk surface) while Phase 60 (worker-side, per `probe` engine class, not inline in the API process) has a different risk profile that could still justify it.
   - Recommendation: Surface this explicitly at the next `/gsd:discuss-phase` (this phase's own, since it's the one that reverses the plan) rather than silently letting Phase 60's planning inherit an assumption either way.

2. **Should `unknown` PDF findings (triggered by `/ObjStm` presence) still attempt the token scan for reporting purposes, or skip it entirely once the trigger is detected?**
   - What we know: Skipping saves CPU/time budget on files that will report `unknown` regardless.
   - What's unclear: whether a partial/best-effort scan result is still useful context to include (e.g., in a `details` field) even when the formal `status` is `unknown` — could help operators without violating INSP-03's "never overclaim completeness" rule, since the status field alone carries the honesty contract.
   - Recommendation: Planner's discretion — low-stakes UX decision, not a correctness question.

## Environment Availability

Not applicable — this phase adds zero new external tool/binary/service dependencies (D-01/D-02 lock pure Go stdlib only). The only "environment" consideration is the Go toolchain itself (1.26.4, already pinned and in use project-wide).

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | Yes (inherited) | `POST /v1/inspect` sits inside `/v1`, behind `auth.Middleware` — same as every other route (`routes.go:37`); no new auth mechanism |
| V3 Session Management | No | Stateless API-key auth, no sessions anywhere in this project |
| V4 Access Control | Yes (inherited) | Client-scoped resources only; inspect introduces no new resource ownership model (it operates on the uploaded bytes only, not a stored resource) |
| V5 Input Validation | Yes — this IS the phase | Every new parser must fail closed on malformed/truncated/oversized input, mirroring the existing `readSniffPrefix`/`walkBoxes`/`cfbWalkDirectory` discipline; no parser may `panic` (project-wide constraint, CLAUDE.md: "Never use panic for control flow anywhere in internal/") |
| V6 Cryptography | No | No new crypto in this phase (encryption DETECTION, not decryption) |
| V12 Files and Resources (ASVS 4.x numbering; "Resource Management" in 5.0) | Yes — this IS the phase | Byte/time/concurrency ceilings are the core deliverable (D-06) |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Decompression/parse bomb via a declared-but-unvalidated length field (zip, PDF stream `/Length`, MP4/EBML box size) | Denial of Service | Bound every allocation and every seek to an already-validated, already-bounded window before trusting a declared length — the pattern every existing sniffer in this repo already follows |
| CPU-bound tight loop with no iteration cap, only a `context` deadline | Denial of Service | Hard iteration/object caps (mirrors `cfbMaxDirSectors`), not `context` alone (Q4) |
| Liveness-probe starvation cascading into a fleet-wide restart storm | Denial of Service | D-06's four mechanisms + Pitfall 1's Postgres-pool fix + Pitfall 2's resource-limits gap closure |
| False-negative security finding from a byte scan blind to compressed containers (PDF `/ObjStm`) | Tampering / Spoofing (a "clean" report that isn't) | Detect the compression signal itself and downgrade to `unknown` rather than reporting a false `clean` (Q1/Q2) |
| TOCTOU — inspect result used to skip a later fail-closed check | Tampering | D-09; proven by a test that inspects bytes A and creates a job with bytes B |

## Sources

### Primary (HIGH confidence — this repo's own code, read directly)
- `internal/convert/dimensions.go`, `docsniff.go`, `cfb.go`, `avsniff.go`, `markupsniff.go`, `ocrguards.go`, `audioduration.go`, `avduration.go` — existing bounded-parser patterns
- `internal/api/routes.go`, `handlers.go`, `cmd/api/main.go` — routing, `/healthz`, listener/shutdown structure
- `internal/ratelimit/ratelimit.go`, `internal/jobs/admission.go` (`PreAdmit`, `admission.go:596`) — existing rate/quota mechanisms and their documented caveats
- `docs/ERROR-CODES.md` — closed-vocabulary, doc-sync-test, open-consumer-contract conventions to mirror
- `deploy/chart/octoconv/templates/deployment-api.yaml`, `deployment-worker.yaml`, `values.yaml` — liveness/readiness probe numbers, resource-limits gap
- `scripts/quota-multireplica-liveproof.sh`, `.planning/milestones/v1.12-phases/51-attribution-accounting-retention/51-05-PLAN.md` — live-gate harness precedent and documented harness defects
- `.env.example` — per-engine measured/unmeasured timeout provenance
- `.planning/phases/58-synchronous-inspection/58-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, `.planning/STATE.md`

### Secondary (MEDIUM confidence — web search, cross-verified against a named authoritative-adjacent source)
- [Object streams — qpdf documentation](https://qpdf.readthedocs.io/en/stable/object-streams.html) — ISO 32000-1 §7.5.7 object-stream restrictions (no stream objects, no encryption dictionary inside `/ObjStm`)
- [SARIF v2.1.0 (OASIS)](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html) — `result.kind` enum precedent for three/six-state check reporting
- MP4 `moov`/`mvhd` box structure and fast-start placement — cross-verified across multiple MP4-format explainer sources (see WebSearch results this session); no single authoritative spec fetch performed

### Tertiary (LOW confidence — general/training knowledge, flagged for validation)
- Matroska/WebM `Segment`/`Info`/`Duration` element typical placement near file head — not independently spec-verified this session (RFC 8794 defines EBML structure but not encoder placement conventions)
- Go 1.14+ asynchronous goroutine preemption behavior — general runtime knowledge, not re-verified against release notes this session
- MP3 VBR duration extraction difficulty — general audio-format knowledge

## Metadata

**Confidence breakdown:**
- Standard stack / architecture: HIGH — every claim about this repo's existing code was read directly, not assumed
- PDF/MP4/EBML byte-budget specifics: MEDIUM — cross-verified via web search against qpdf/ISO-adjacent and MP4-format sources, not Context7 (no relevant library exists in this repo to query) and not a direct ISO 32000/14496-12 spec fetch this session
- Live-gate design: HIGH for the harness SHAPE (reused directly from this repo's own Phase 55 script and its documented defect history) and the liveness-probe NUMBERS (read directly from the chart); MEDIUM for how the gate should assert against those numbers (not yet built/tested)
- Pitfalls: HIGH — Pitfall 1 (Postgres pool sharing) and Pitfall 2 (no resources.limits on api) are both derived from direct code/chart inspection, not speculation

**Research date:** 2026-08-10
**Valid until:** 30 days for the architecture/stack findings (stable); the PDF/MP4/EBML format-spec findings are effectively permanent (stable file-format specs) but should be treated as MEDIUM until a fixture corpus proves them against real files, which Wave 0 test-gap items above exist to do
