# Phase 47: Pandoc Engine Foundation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-30
**Phase:** 47-pandoc-engine-foundation
**Areas discussed:** Content-validation of text formats, Format (from→to) matrix, Pandoc pinning/build, PandocOpts allowlist + bomb/size guard

---

## Content validation for magic-less text formats (PDCE-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Probes + trust-declared for plaintext | docx/epub→SniffContainer; html→existing LooksLikeHTML; latex→LooksLikeLaTeX marker; md/rst/org→shared LooksLikeText (UTF-8+no-NUL+size) then trust declared among text-family, limitation documented | ✓ |
| Strict markers for all | Require a distinguishing marker even for md/rst/org — rejects valid plain-prose md, too strict | |
| UTF-8+size for all text | No per-format probes even for html/latex — lighter but weaker, ignores existing LooksLikeHTML | |

**User's choice:** Probes + trust-declared for plaintext
**Notes:** Extends the existing html precedent (handlers.go detected=="" → LooksLikeHTML). md/rst/org are mutually interchangeable plaintext — content can't distinguish them; gate degrades honestly to "is it text?", documented as an accepted gap (like OCR empty-output / audio silence).

---

## Format (from→to) matrix (PDOC-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Curated directional | Explicit Pairs() of meaningful markup interconversions; exclude identity + nonsensical (epub→latex); closed set | ✓ |
| Full cartesian | All 7×7 ordered pairs; simpler but many low-value pairs + bigger test surface | |

**User's choice:** Curated directional
**Notes:** Exact pair list is Claude's discretion (finalized against pandoc's real reader/writer capabilities), but the set is CLOSED and explicit.

---

## Pandoc binary pinning

| Option | Description | Selected |
|--------|-------------|----------|
| Pinned static-release + SHA-256 | Download pinned official static linux binary, SHA-256-pin, COPY into image (like audio model bake); no GHC | ✓ |
| apt-pinned | Simpler Dockerfile but version floats with Debian point-release, older pandoc, less reproducible | |

**User's choice:** Pinned static-release + SHA-256
**Notes:** GHC source-build rejected (impractical for a large Haskell binary). Exact version+SHA resolved live in Phase 49's Dockerfile.

---

## PandocOpts allowlist + bomb/size guard (PDCO-01, PDCE-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Small allowlist + size-cap + timeout | opts: standalone/toc/number-sections/wrap/section-divs (typed strict-parse); input-size cap + rely on ENGINE_TIMEOUT for output-explosion | ✓ |
| standalone only (minimal) | Just standalone in v1, size cap; less surface but less client flexibility | |

**User's choice:** Small allowlist + size-cap + timeout
**Notes:** pandoc reads input linearly — no exotic pre-parse bomb check needed; PANDOC_MAX_INPUT_BYTES + timeout bound it. Injection test required (client bytes never reach argv unvalidated).

## Claude's Discretion
- Exact curated Pairs() list; pinned pandoc version+SHA; LooksLikeLaTeX/LooksLikeText marker details; PandocOpts field set beyond the core; PANDOC_MAX_INPUT_BYTES default; raw-passthrough-disable flag set.

## Deferred Ideas
- →pdf (weasyprint/tectonic) → v2; broad format allowlist → v2; client lua-filters/templates → v2.
