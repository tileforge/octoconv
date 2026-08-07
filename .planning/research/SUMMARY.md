# Project Research Summary

**Project:** OctoConv — v1.13 "Inspect & Operations"
**Domain:** Document/file-processing API (inspection, CDR/sanitization, operation modeling, structured errors)
**Researched:** 2026-08-07
**Confidence:** HIGH for codebase-grounded claims (direct file reads), MEDIUM for external library/industry findings

## Executive Summary

v1.13 turns OctoConv from a format converter into a small processing API: an `operation` dimension on
jobs, a synchronous `POST /v1/inspect`, `sanitize` as the first async non-convert operation, born-digital
PDF routing, and a JobRunner consolidation with structured error codes underneath it all.

The four researchers converge on one central, non-obvious point that should shape the whole roadmap:
**inspect is cheap in dependencies but expensive in blast radius, while sanitize is expensive in tooling
but its real risk is a false-clean guarantee.** Most of what inspect must report already runs in the API
process today (`Sniff`, `SniffContainer().HasMacro`, `ClassifyCFB`, `Dimensions`) — it is an
aggregation/exposure layer, not green-field parsing. But it is the first request path that parses
adversarial bytes *inline*, in the same process that serves `/healthz`, in a service whose entire safety
model assumes worker-side isolation with measured timeouts. Sanitize is the inverse: nothing in the
codebase parses or rewrites PDF or OOXML content today, so the tooling is genuinely new — and the
security literature is unanimous that a sanitizer verified by the same parser that did the removal is
structurally blind to its own bypasses.

The research also **corrected the milestone's own framing in three places** (see Corrections below), the
largest being that SEED-020's JobRunner scope is real but smaller than assumed. It still earns its place:
it is where the eighth engine class gets built the right way instead of hand-copied an eighth time, and
it is where a live, currently-uncaught instance of the PR #4 defect class gets fixed structurally.

**The single biggest open decision blocking implementation:** `operation='sanitize'` does not exist in
the schema (`0001_init.sql`'s CHECK allows `convert|extract|archive|inspect|render`) and the project's
own planning artifacts model it inconsistently — SEED-013 framed it as `engine=probe` with
`operation=inspect|convert`, the milestone framing treats `sanitize` as a literal new operation value.
This must be a written decision before any migration lands.

## Key Findings

### Recommended Stack

The stack answer is unusually cheap. Most inspect signals need **zero new dependency** — the detection
already runs on the upload path and only needs to be surfaced. The one real addition is a Go library, not
a container binary: **`github.com/pdfcpu/pdfcpu` v0.14.0** (Apache-2.0, pure Go, `CGO_ENABLED=0`-compatible,
released 2026-08-03). It is the only clean way to get PDF page count, encryption status, `/EmbeddedFile`
listing and `/JS`/`/OpenAction`/`/AA`/remote-reference detection without either adding subprocess binaries
to the API container or hand-rolling a compressed-object-stream PDF parser — a genuinely risky DIY, unlike
the fixed-header formats this project already hand-rolls. And it does double duty: the same parser that
detects PDF risks for inspect can re-scan sanitize output to prove the risks are gone.

**Core technologies:**
- `pdfcpu` v0.14.0 (Apache-2.0, pure Go) — PDF structure/security signals for inspect AND sanitize-and-verify. The largest new Go dependency since the MCP `go-sdk` (Phase 21); justify it on the JS/OpenAction/EmbeddedFile need specifically.
- Go stdlib `archive/zip` + `encoding/xml` — OOXML macro/embedded-object stripping, extending the exact pattern `docsniff.go` already uses to *detect* `vbaProject.bin`. Verification is free: re-run the existing `SniffContainer(output).HasMacro`.
- Existing `vips copy in out[strip]` — image EXIF/GPS/IPTC/XMP scrub. A one-argument change to the already-hardened `LibvipsConverter.Convert` path. Zero new dependency, zero new binary.
- Existing `go-chi/httprate` (already a direct dependency) — a *second* limiter instance scoped to `/v1/inspect`, plus `context.WithTimeout` for the work ceiling.
- Structured error codes: **no framework**. Go 1.13+ `errors.Is`/`As` with a typed `Code` field — the project already does this partially (`quota_exceeded`).

**What NOT to add (explicit):** `poppler-utils`/`qpdf`/`mutool`/`ffprobe`/`exiftool` in `Dockerfile.api`
(the API process has never run a subprocess — do not open that door for a synchronous path with no queue
to fall back on); `exiftool` anywhere (Perl runtime + CVE-2021-22204 RCE history on untrusted-input
parsing, for a capability `vips[strip]` and `pdfcpu` already cover); `pikepdf` (Python runtime wrapping
qpdf's engine for zero extra capability); Ghostscript (AGPL question, lossy rewrite, not a designed
sanitizer); `golang.org/x/image` for EXIF presence checks (same zero-dependency bias that already
rejected it for the decompression-bomb guards); byte-scanning a PDF for `/JS` without an object-graph
parse (false negatives against compressed object streams — a silent security bypass).

### Expected Features

**Must have (table stakes):**
- Inspect reports the **detected** format via magic bytes, never the declared/extension one.
- Format-appropriate characteristics (per-format-shaped response, not one flat schema with nulls everywhere).
- A **typed findings list** — `code` + `severity` + `location`, closed enum, not free text.
- An explicit **tri-state per check**: checked-and-clean vs not-applicable vs could-not-determine. This is the single most commonly *missing* property in home-grown inspection APIs, and no OctoConv code models it today (current code is binary reject/pass).
- **Honest sampling/partial-analysis disclosure** when a bound is hit (`sampled: true`, `pages_checked: N of M`) — already promised in the milestone framing, not optional polish.
- Sanitize removal set — PDF: JavaScript, embedded files, launch actions, external URI references. OOXML: VBA macros **including p-code**, not just source (VBA-stomping defense is baseline, not advanced), and embedded OLE objects. Images: EXIF/GPS/IPTC/XMP.
- A sanitize **report** as a second output with `role=report` — CDR products that silently mutate a file with no accounting are exactly what security teams distrust.
- `GET /v1/jobs/{id}` returns all outputs with roles when there are several.
- Sanitize is a separate explicit `operation`, never an implicit side effect of convert.
- Machine-readable error `code` on every terminal failure and every 4xx — the service already ships `quota_exceeded`/`rate_limited`, so having it on 2 of N paths is a half-finished contract both MCP servers already work around by string-matching.
- Born-digital PDF stops being an unconditional dead end.

**Should have (differentiators):**
- Inspect reports available `(operation, target)` pairs **with a duration estimate and a quota check for this specific file** — Tika/ffprobe/exiftool cannot do this; it is differentiated precisely because v1.12 built the accounting.
- The sanitize report as a first-class retrievable audit artifact (S3 object, TTL, presigned GET) rather than a UI-only document.
- Confidence/sampling exposed to the client rather than used only internally.

**Defer (v2+):**
- SEED-021 **Opportunity 2** — per-page Mixed-PDF classification/routing. The seed itself flags an unresolved merging contract (page order, non-duplication, Markdown-vs-hOCR asymmetry). Spike, not a phase.
- Dangerzone-style rasterize-and-rebuild sanitize — strictly higher assurance but lossy by construction (text becomes an OCR approximation, forms become raster). Legitimate future mode, but must ship under a distinct name, never silently substituted for targeted-removal sanitize.
- Migrating the existing 429 bodies into the new error envelope; richer client-tunable inspect sampling.

**Anti-features explicitly called out:** claiming sanitize output is "guaranteed malware-free"; semantically
*repairing* removed form JavaScript instead of removing and reporting it; a bare `{"error","code"}` shape
with no envelope (locks out a future RFC 9457 migration as anything but a breaking change); a dynamic,
runtime-discoverable finding/error code namespace (the same argument that rejected SEED-010 three times);
treating an earlier `inspect` result as authority to skip the fail-closed checks on the subsequent job
creation (TOCTOU — inspect is stateless and pins nothing).

### Architecture Approach

Every claim below was verified against the code, and several corrected the seeds that asserted them.
`operation` turns out to be **already fully plumbed at the persistence layer** — only a single hardcoded
`operationConv = "convert"` in `internal/api/handlers.go` blocks client-supplied operations. `Registry`
can become operation-aware via an `OperationalConverter` optional interface (mirroring the existing
`ProgressConverter` precedent) with zero changes to any of the 30+ existing `Lookup`/`Supports`/`EngineFor`
call sites. The v1.12 admission gate and `client_usage_daily` are already engine-only and
operation-agnostic, so a `sanitize` job on a new `probe` engine gets correct isolated quota accounting for
free — with one disclosed limitation: the ledger's grain is `(client_id, engine, day, status)` with no
operation column, so a *second* probe-engine operation later would be indistinguishable from the first.

**Major components:**
1. `internal/worker/runner.go` (new) — `EngineDescriptor` + `Runner` replacing seven `Handle*Convert` methods. Note `process()` (worker.go:1847-1942) is *already* the shared orchestrator; the ×7 tax is in the wrapper boilerplate around it.
2. `internal/inspect/` (new package) + `internal/api/inspect_handlers.go` — synchronous inspection, composing existing `internal/convert` sniffers.
3. `internal/convert/convert.go` (modified) — `OperationalConverter` optional interface, `Pair` gains `Operation`, `LookupOp`/`SupportsOp`/`EngineForOp` added with existing signatures kept as convert-defaulting wrappers.
4. `cmd/probe-worker/` (new binary) + `QueueProbe`/`TypeProbeConvert` + `scaledobject-probe.yaml` — the eighth engine class, built via the Phase-56 descriptor pattern instead of hand-copied.
5. Migrations — widen `jobs_operation_check`; add `job_outputs.role DEFAULT 'primary'`. `jobs_engine_check` needs no change (`probe` reserved since `0001`).

### Critical Pitfalls

1. **The sync `/v1/inspect` handler shares a process with `/healthz`.** A pathological inspect burst can fail the k8s liveness probe and cascade a fleet-wide restart — not merely slow one client. Needs a live-gated process-isolation proof as a named success criterion, not just a request timeout.
2. **The JobRunner refactor can collapse per-engine timeout classification onto one axis.** `isAudioTerminal` and `isAVTerminal` encode a *stage*-level asymmetry with **opposite polarity** from each other, deliberately. The descriptor must model `(stage, isTimeout)`, not `(engine, isTimeout)`, or the refactor silently regresses the two costliest engine classes.
3. **`handleGetJob` takes `outs[0]` unconditionally** (`internal/api/handlers.go:857-870`) — verified live. The moment sanitize ships a second `role=report` output, the report can be served as the primary result or the primary result can be hidden. Must be fixed in the operation/roles phase, **before** sanitize ships.
4. **KEDA metric-label drift.** The refactor touches all 14 metric call sites in the same milestone that adds an operation dimension — the natural "let's add an `operation` label while we're here" impulse breaks scale-from-zero for all seven engine classes. `TestJobDurationLabelsPinned`/`TestQueueDepthLabelsPinned` must stay green; verify by diff, mirroring FAIR-02's v1.12 method.
5. **Sanitize's false-clean guarantee.** Verified against real sources: PDF incremental updates leave "removed" objects byte-recoverable in an earlier revision; naive OOXML zip-entry deletion leaves dangling `[Content_Types].xml`/`.rels` references or leaves payload bytes physically present; self-verification with the removing parser is blind to polyglot bypasses (CVE-2015-5092 precedent). The project already has the right precedent — veraPDF verifying LibreOffice's PDF/A output with an *independent* parser. Apply it here: forced full-rewrite saves, and verification by a second implementation.

**Live defects the research found in existing code** (actionable regardless of this milestone):

| Location | Defect |
|---|---|
| `internal/reconciler/reconciler.go:311-312` | `RecordReconcilerAction("exhausted")` fires unconditionally, not guarded on `MarkFailed`'s result — **the exact PR #4 defect class, uncaught** |
| `internal/api/handlers.go:857-870` | `handleGetJob` presigns `outs[0]` with no role awareness |
| `internal/jobs/retention.go:126` | `PurgeUsage` derives its cutoff from `time.Now().UTC()` while every other v1.12-era time-sensitive read uses Postgres `now()` |
| `internal/convert/ocrguards.go:227-228` | Comment claims the rejection steers the client "to the document engine instead" — the document engine does not accept pdf as a source at all |

## Implications for Roadmap

Suggested phase structure, numbering continuing from 56.

### Phase 56: JobRunner consolidation + structured error codes (async half)
**Rationale:** Foundation. Everything downstream benefits from landing on a descriptor-driven runner
before an eighth handler is added, and this is where retry classification and terminal error handling get
centralized — their natural home for error codes. No hard dependency *from* 56, but building sanitize
against seven hand-copied handlers would compound the exact problem the milestone exists to remove.
**Delivers:** `EngineDescriptor` + `Runner`; `FailureCode` folded into the descriptor; the
`reconciler.go:311-312` unguarded-metric fix; the `invalid_options` metrics gap closed.
**Avoids:** Pitfalls 2 (stage-asymmetry collapse), 4 (KEDA label drift), 14 (tests that prove the runner's
shape rather than the seven engines' behavior).

### Phase 57: `operation` dimension + output roles
**Rationale:** Both are additive schema/registry work with no client-visible behavior yet, and both gate
sanitize. Bundled because they share one migration and one review.
**Delivers:** migration widening `jobs_operation_check` + `job_outputs.role DEFAULT 'primary'`;
`OperationalConverter`; `handleCreateJob` accepting `operation` with `convert` default;
`GET /v1/formats` gaining an additive `operations` key with `engines` byte-identical.
**Must resolve here:** the `sanitize` vocabulary decision, and `handleGetJob`'s role-blindness — **before**
sanitize ships, not concurrently.
**Avoids:** Pitfalls 3, 9, 10, 13.

### Phase 58: Synchronous `POST /v1/inspect`
**Rationale:** Depends on 57 for "available `(operation, target)` pairs" to be meaningful. Does not depend
on roles.
**Delivers:** `internal/inspect/` (pure-Go sniffers + `pdfcpu`), the report envelope with typed findings,
tri-state check status and sampling disclosure; work ceiling, body cap, dedicated rate budget.
**Uses:** `pdfcpu` v0.14.0, existing `httprate`, `context.WithTimeout`.
**Avoids:** Pitfalls 1 (needs a live process-isolation gate as a success criterion) and 8 (unmetered abuse
budget on a path that bypasses the v1.12 gate by design).

### Phase 59: Born-digital PDF routing (SEED-021 Opportunity 1 — routing only)
**Rationale:** Hard dependency on 58. Deliberately excludes adopting any specific extraction tool.
**Delivers:** `text_layer_present` in the inspect report, pair-recommendation suppression, and the
`ocrguards.go:227-228` comment fix regardless of tool choice.

### Phase 60: `sanitize` — the first async non-convert operation
**Rationale:** Sequenced last. Highest security bar, and the first consumer of every piece built in 56/57.
**Delivers:** `SanitizeConverter` (engine `probe`), `QueueProbe`/`TypeProbeConvert`, `cmd/probe-worker`,
an eighth `ScaledObject` (additive, zero diff to the existing seven), a second `AddOutput` with
`role='report'`, and independent-parser verification of the sanitized output before upload.
**Avoids:** Pitfalls 5, 6, 7, 15.

### Independent hygiene tail (any slot)
52 WR-01 (stuck in-flight job burns the daily budget forever, no operator relief but raw SQL), 52 WR-09
(self-contradictory README quota section), and `PurgeUsage`'s Go-clock cutoff. **Zero file overlap** with
56–60, per the Phase-54 precedent of shipping hygiene debt as an explicitly separate, non-noisy phase.

### Phase Ordering Rationale

- **Attribution of dependencies is asymmetric:** 57 gates 58 and 60; 58 gates 59; 56 gates nothing hard but pays for itself in 60.
- **Sanitize last, not first,** despite being the strongest product story — it needs the descriptor pattern, the operation dimension and the role column to all exist, and it carries the milestone's highest security bar.
- **Adding an eighth queue is NOT high blast radius.** Verified: each `scaledobject-*.yaml`'s PromQL is self-contained to its own queue-name literal, and `AllConvertQueues()` exists precisely so the collector needs no second edit. Proven twice already (v1.10 OCR, v1.11 pandoc). *Renaming* a queue is the expensive operation — nothing in v1.13 requires it. Do not over-fear this and under-fear metric-label drift, which is the real KEDA risk here.

### Research Flags

Needs a design spike during planning:
- **Phase 58** — does any Tier-2 signal justify putting `pdfinfo`/`pdftotext`/`ffprobe` in `Dockerfile.api`? (Research recommends no; the trade-off is disclosed, not resolved.) Exact `pdfcpu` Go-API calls for encryption status and JS/OpenAction walking need a short implementation spike — the capability is confirmed in the object model, but there is no single convenience function.
- **Phase 60** — the sanitize vocabulary decision; blacklist-removal vs reconstruct-from-trusted-content; the independent-verification parser choice.

Standard patterns, skip a dedicated research phase: **56**, **57**, **59**.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH for existing-code findings (source read directly); MEDIUM for `pdfcpu` API specifics | Exact Go calls for JS/OpenAction detection need an implementation spike |
| Features | MEDIUM-HIGH | Official docs (Tika, ffprobe, RFC 9457, Stripe) + vendor primary sources (OPSWAT, Glasswall) + open-source primary (Dangerzone) |
| Architecture | HIGH | Every claim tied to a file/line read this session; all SEED-020/SEED-021 assertions independently re-verified |
| Pitfalls | HIGH for codebase claims and PDF/OOXML security claims; MEDIUM for the k8s liveness-cascade inference | PDF incremental-update and OOXML zip-deletion behavior verified against spec/SANS ISC/a confirmed Open-XML-SDK issue; polyglot claim against CVE-2015-5092 |

**Overall confidence:** HIGH-MEDIUM

### Corrections to the project's own artifacts (made by this research)

1. **SEED-020 overstated its own cost.** Handler bodies measure 79–110 lines (86–127 with doc comments), not 94–163. `process()` at worker.go:1847-1942 is already the shared orchestration function, so the ×7 tax lives entirely in wrapper boilerplate. The "disk guards (av) / page caps (ocr)" the milestone framed as handler-level differences are converter-internal. Its "14 symmetric call sites" claim, however, is **exactly correct** (verified by grep).
2. **SEED-021's code claims are fully verified** — `pdf` is a source format in exactly one place (`ocr.go:34`), the misleading comment is present verbatim, and the document engine genuinely never accepts pdf as source.
3. **The queue blast-radius fear is misplaced.** Adding a queue is cheap and precedented; renaming one is not. The real KEDA risk in this milestone is metric-label drift.

### Gaps to Address

- **`sanitize` operation vocabulary** — undecided, and the sources disagree (`0001_init.sql` CHECK vs SEED-013's `probe`-as-engine framing vs the milestone's literal-new-value framing). Must be a written decision in Phase 57, before any migration lands.
- **Blacklist-removal vs reconstruct-from-trusted-content** for sanitize — a real design decision this research could not resolve; depends on available offline-CLI tooling compatible with the `runCommand` convention.
- **Independent-verification parser choice** for sanitize output.
- **Whether Tier-2 inspect signals justify binaries in `Dockerfile.api`** — disclosed trade-off, not resolved.
- **Born-digital extraction tool choice** — explicitly not settled by this research or by the milestone scope.
- **Whether inspect's rate budget needs multi-replica correctness** (QUOT-05-style) — depends on the deployment target for this milestone.
- **Whether `GET /v1/jobs/{id}` must stay byte-identical for existing convert jobs** — needs an explicit testable requirement, not an assumption; both MCP servers depend on it.
- **LOW-confidence external claims:** exact JSON field names for Tika `/meta`, CloudConvert `get-metadata`, and pdf-inspector's `--json`; pdf-inspector's own unmeasured binary-size and 10-50ms timing claims.

## Sources

### Primary (HIGH confidence)
- Direct reads of `internal/worker/worker.go` (full, 1990 lines), `internal/jobs/{repo,admission,usage,retention}.go`, `internal/convert/{convert,converters,ocrguards,ocr,sniff,docsniff,dimensions,olecfb,av,avsniff,audiosniff,libvips,exec}.go`, `internal/api/{handlers,api,formats_handlers}.go`, `internal/queue/queue.go`, `internal/metrics/metrics.go`, `internal/reconciler/reconciler.go`, migrations `0001`–`0014`, `deploy/chart/octoconv/templates/scaledobject-*.yaml`, `Dockerfile.api`, `Dockerfile.ocr-worker`
- [RFC 9457: Problem Details for HTTP APIs](https://www.rfc-editor.org/rfc/rfc9457.html)
- [ffprobe documentation](https://ffmpeg.org/ffprobe.html) — cross-verified against this project's own `avduration.go`
- [Stripe API error codes](https://docs.stripe.com/error-codes)
- [Dangerzone (freedomofpress)](https://github.com/freedomofpress/dangerzone)
- Context7 `/websites/pdfcpu_io`, `/websites/pkg_go_dev_github_com_pdfcpu_pdfcpu`

### Secondary (MEDIUM confidence)
- [OPSWAT Deep CDR](https://www.opswat.com/technologies/deep-cdr) and its VBA-stomping page; [Glasswall CDR docs](https://docs.glasswall.com/docs/about-glasswall-cdr)
- [Apache Tika Server docs](https://tika.apache.org/docs/4.0.0-SNAPSHOT/using-tika/server/index.html); [CloudConvert API v2](https://cloudconvert.com/api/v2); [Transloadit Robots](https://transloadit.com/docs/robots/)
- [qpdf](https://qpdf.readthedocs.io/en/stable/license.html), [mutool clean](https://mupdf.readthedocs.io/en/latest/tools/mutool-clean.html), Debian bookworm package tracker, [Ghostscript licensing](https://ghostscript.com/licensing/), [pikepdf](https://github.com/pikepdf/pikepdf)
- [pdfcpu releases](https://github.com/pdfcpu/pdfcpu/releases) and [issue #653](https://github.com/pdfcpu/pdfcpu/issues/653) (JS removal is an open request, not shipped)

### Tertiary (LOW confidence — needs validation)
- [firecrawl/pdf-inspector](https://github.com/firecrawl/pdf-inspector) — MIT, 0.1.7 pre-1.0, no prebuilt dual-arch binaries found; network/timing/size claims are the project's own README, unmeasured
- VirusTotal / Cloudmersive response schemas — search-summarized, not fetched live

---
*Research completed: 2026-08-07*
*Ready for roadmap: yes*
