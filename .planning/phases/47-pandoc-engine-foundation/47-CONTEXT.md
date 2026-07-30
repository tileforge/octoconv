# Phase 47: Pandoc Engine Foundation - Context

**Gathered:** 2026-07-30
**Status:** Ready for planning

<domain>
## Phase Boundary

A standalone `PandocConverter` (in `internal/convert/`) converts between markup-family formats through a CLOSED `(from→to)` allowlist, security-hardened from the first line, built and tested directly against the real pandoc binary — BEFORE any queue/worker/API plumbing (Phase 48). Mirrors the Audio Phase 30 / AV Phase 34 / OCR Phase 43 "validate the novel external-process surface standalone first" scope fence. Covers PDOC-01/02, PDCO-01, PDCE-01/02/03.

**In scope:** the `PandocConverter` (Pairs/Engine/Convert), its closed curated format allowlist, `--standalone` output where required, the hardened pandoc invocation (no raw HTML/TeX passthrough, `--no-shell-escape`, no network, via existing `runCommand`), `PandocOpts` closed typed allowlist (strict-parse + injection test), and the content-validation gate for the markup family (structural probes + input-size/bomb guard) — all tested against the real pandoc binary.
**Out of scope:** queue/worker/migration/API routing (Phase 48); container/Dockerfile/measured timeout (Phase 49); KEDA/Helm (Phase 50); →pdf output (v2); formats beyond the markup family (v2); client `--lua-filter`/templates (v2).
</domain>

<decisions>
## Implementation Decisions

### Content validation for the markup family (PDCE-01)
- **D-01:** Fail-closed via **structural probes + trust-declared-for-interchangeable-plaintext**, extending the EXISTING html precedent (handlers.go: `detected==""` → `LooksLikeHTML` → set detected, else fall through to reject; D-07/HTML-02):
  - **docx, epub** → `convert.SniffContainer` (existing zip pattern: PK magic + OOXML/ODF root-part inspection + zip-bomb guard; epub's `mimetype` first-entry `application/epub+zip` is a strong signal). Strong content validation.
  - **html** → the existing `convert.LooksLikeHTML` (UTF-8 + no NUL + doctype/`<html>` marker). Reuse verbatim.
  - **latex** → a new `LooksLikeLaTeX` fail-closed probe (UTF-8 + no NUL + a LaTeX marker such as `\documentclass`/`\begin{`/leading `\`-command). Feasible because latex has distinctive markers.
  - **md, rst, org** → a shared `LooksLikeText` fail-closed probe (valid UTF-8 + no NUL/binary control bytes + under the input-size cap) THEN **trust the declared source format** among the text-family. These are mutually-interchangeable plaintext (a plain sentence is valid md AND rst AND org) — content cannot reliably distinguish them, so the gate degrades honestly to "is this plausibly a text document?", not "is it specifically rst-vs-md". This accepted-limitation is DOCUMENTED explicitly (same discipline as OCR's "empty-but-valid output is `done`" and audio's "hallucination-on-silence" documented gaps) — never silently.
  - Anything that fails its probe leaves `detected==""` and falls through to the existing unrecognized-content 422.

### Format (from→to) matrix (PDOC-01)
- **D-02:** A **curated directional closed allowlist** — an explicit `Pairs()` set of MEANINGFUL markup interconversions across {md, rst, org, latex, html, docx, epub}, NOT the full 7×7 cartesian and NOT "anything pandoc accepts". Exclude identity pairs and nonsensical/low-value directions (e.g. epub→latex/org). The exact pair list is Claude's discretion — finalized during planning/execution against pandoc's real reader/writer capabilities — but the SET IS CLOSED and explicit (mirrors every existing engine's `Pairs()` discipline; `NormalizeFormat` handles aliases like `markdown`→`md`, `htm`→`html`).

### Pandoc binary pinning (build posture — informs Phase 49, decided now)
- **D-03:** **Pinned official static-release binary + SHA-256**, NOT a GHC source-build (impractical — pandoc is a large Haskell binary; source-building it pulls an enormous toolchain, unlike ffmpeg/tesseract). Download a specific pinned pandoc release (static linux amd64 tarball), SHA-256-verify, `COPY` into the runtime image — mirroring the audio-worker's SHA-pinned model-bake discipline, NOT apt (which floats with the Debian point-release and ships an older pandoc). The exact pinned version + SHA is Claude's discretion (resolve live), pinned in the future `Dockerfile.pandoc-worker` (Phase 49); Phase 47 tests against whatever pandoc is available locally and does not hardcode a version in Go.

### PandocOpts allowlist + bomb/size guard (PDCO-01, PDCE-03)
- **D-04:** A **small closed typed `PandocOpts` allowlist** (e.g. `standalone` bool, `toc` bool, `number_sections` bool, `wrap` enum {auto|none|preserve}, `section_divs` bool — strict-parse via the shared `checkStrictObject` helper, mirroring `OCROpts`/`AudioOpts` verbatim; unknown/unlisted opt → 422 before any work; every value reaches pandoc ONLY as a discrete argv element, never a raw client string — injection test required). Bomb/DoS guard: an **input-size cap** (`PANDOC_MAX_INPUT_BYTES`, config field defaulting to a sane value) rejected fail-closed BEFORE conversion, PLUS reliance on the engine timeout (Phase 49) for output-expansion runaways — pandoc reads input linearly, so the classic "pre-parse for a bomb shape" is not needed the way it is for zip/pdf; the size cap + timeout bound it. Empty-but-valid output completes `done` (not `failed`) — documented, no `if output=="" {error}` branch.

### Claude's Discretion
- The exact curated `Pairs()` list; the pinned pandoc version + SHA-256; the precise `LooksLikeLaTeX`/`LooksLikeText` marker/threshold details; the `PandocOpts` field set beyond the D-04 core; the `PANDOC_MAX_INPUT_BYTES` default; the raw-passthrough-disable flag set (`--no-shell-escape`; NOT enabling `+raw_html`/`+raw_tex` extensions; possibly `-f <fmt>-raw_html-raw_tex`) — all executor/planner's call, provided: the allowlist is closed, the invocation is hardened (no raw passthrough / no shell-escape / no network / via `runCommand`), content validation is fail-closed with the documented text-family limitation, and everything is proven against the real pandoc binary.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The engine-class analogs to CLONE (internal/convert/)
- `internal/convert/ocropts.go` + `audioopts.go` — the closed typed-opts allowlist + `checkStrictObject` strict-parse + injection-safe argv discipline to clone as `pandocopts.go` (`PandocOpts`, `ParsePandocOpts`, `ValidatePandocApplicability`, `pandocArgs`).
- `internal/convert/convert.go` (Converter interface, Registry, Pairs, `NormalizeFormat`) + `converters.go` (`init()` registration) — the `Converter` contract + how `Pairs()` defines the closed allowlist. (Phase 47 builds the converter standalone; registration wiring is Phase 48 — but follow the interface.)
- `internal/convert/exec.go` (`runCommand`: Setpgid + SIGKILL-on-timeout, no shell) — the MANDATORY hardened invocation path (byte-for-byte unchanged); pandoc argv goes through it from line one.
- `internal/convert/ocr.go` / `libvips.go` / audio converter — concrete `Converter` implementations to mirror the shape of `Convert(ctx, inPath, outPath, opts)`.

### Content validation (PDCE-01) — the existing precedents to EXTEND
- `internal/api/handlers.go` (~lines 201-280) — the content-gate flow: `convert.Sniff` → `detected==""` → `SniffContainer` (zip) → `LooksLikeHTML` (the magic-less-text precedent to mirror for latex/md/rst/org) → `IsOLECFB` reject → final `detected==""` 422.
- `internal/convert/sniff.go` (`Sniff`, the matchers table, `NormalizeFormat`, `MIMEType`, `LooksLikeHTML`) — where `LooksLikeLaTeX`/`LooksLikeText` land + the MIME/format registry to extend for markup formats.
- `internal/convert/docsniff.go` (`SniffContainer`, OOXML root parts, zip-bomb guard) — the docx/epub zip-container validation to reuse.

### Requirements + prior
- `.planning/REQUIREMENTS.md` — PDOC-01/02, PDCO-01, PDCE-01/02/03.
- `.planning/ROADMAP.md` §"Phase 47" — the 5 Success Criteria (the contract).
- `.planning/seeds/SEED-012.md` — the pandoc engine seed (open questions now resolved here).
- Audio Phase 30 / OCR Phase 43 foundation phases (in `.planning/milestones/v1.10-phases/43-*` for OCR) — the standalone-converter scope-fence precedent + the audio SHA-pinned-bake discipline (D-03).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `checkStrictObject` (opts.go) + `OCROpts`/`AudioOpts` — near-verbatim clone target for `PandocOpts`.
- `runCommand` (exec.go) — the hardened exec path; pandoc uses it unchanged.
- `LooksLikeHTML` (sniff.go) + `SniffContainer` (docsniff.go) — the magic-less-text and zip-container validation patterns to extend/reuse.
- `Converter`/`Registry`/`Pairs`/`NormalizeFormat` (convert.go) — the interface + closed-allowlist mechanism.

### Established Patterns
- Closed typed-opts allowlist, strict-parsed, client bytes never reach argv unvalidated (injection test) — every engine.
- Fail-closed content validation: magic bytes for binary formats; structural `LooksLike*` probe for magic-less text (html precedent); reject on any probe failure.
- Hardened `runCommand` from line one for every untrusted external process (never a bare `exec.Command` shortcut).
- Standalone converter first (unregistered), tested against the real binary, before any queue/worker plumbing.

### Integration Points
- `internal/convert/` (new `pandoc.go`/`pandocopts.go`/`pandocsniff.go` or additions to `sniff.go`); `internal/api/handlers.go` content-gate extension for the markup family; `internal/convert/exec.go` (used, not changed).
</code_context>

<specifics>
## Specific Ideas
- Clone the OCR/audio foundation shape: `PandocConverter` with `Pairs()` (curated closed set) + `Convert()` shelling to pinned pandoc via `runCommand` with `--standalone` (where the target needs structure), raw-passthrough disabled (`-raw_html-raw_tex` extensions off), `--no-shell-escape`, no network.
- The security test (SC3) crafts input attempting shell-escape / `\input{/etc/passwd}` / a network fetch and proves none fire — mirroring the chromium html engine's offline-canary discipline.
- Document the md/rst/org content-interchangeability limitation as an accepted, recorded gap (Key Decision), not a silent behavior.
</specifics>

<deferred>
## Deferred Ideas
- →pdf output (weasyprint/tectonic) — v2 (PDOC-V2-01); keeps the image TeX-free.
- Broad format allowlist (textile/mediawiki/asciidoc/ipynb/typst) — v2 (PDOC-V2-02).
- Client `--lua-filter`/templates/reference-doc — v2 (PDOC-V2-03), needs a sandbox/upload contract.
- Queue/worker/migration/API routing → Phase 48; container + measured timeout → Phase 49; KEDA/Helm → Phase 50.

</deferred>

---

*Phase: 47-pandoc-engine-foundation*
*Context gathered: 2026-07-30*
