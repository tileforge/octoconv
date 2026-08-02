# Phase 49: Containerization + Measured Timeout Gate - Discussion Log

> **Audit trail only.** Decisions are captured in CONTEXT.md.

**Date:** 2026-07-31
**Phase:** 49-containerization-measured-timeout-gate
**Areas discussed:** Measurement metric + corpus + Path-B, Pandoc pin/source, Live E2E pairs

---

## Measurement metric + derivation (PDCE-04)

| Option | Description | Selected |
|--------|-------------|----------|
| p95 wall-sec/conv, worst×2.0, Path-B=MAX_INPUT_BYTES | Corpus formats×sizes×targets at compose limits; p95 wall-sec/conversion; timeout=ceil(worst p95×2.0)<900s; Path-B lowers PANDOC_MAX_INPUT_BYTES | ✓ |
| Discuss metric | Other safety/corpus/lever | |

**User's choice:** p95 wall-sec/conv, worst×2.0, Path-B=MAX_INPUT_BYTES
**Notes:** pandoc cost = f(input size × (from,to) pair), not per-page/RTF. Path-B lever = the Phase-47 input-size cap (analog of OCR_MAX_PAGES). Supervised run; no value committed before a real p95.

---

## Pandoc binary pinning

| Option | Description | Selected |
|--------|-------------|----------|
| Pinned static-release + SHA-256 | pandoc-X.Y.Z-linux-amd64.tar.gz from GitHub releases, sha256 fail-closed, COPY into debian-slim, no TeX, latest stable 3.x resolved live | ✓ |
| Discuss | Other version/source | |

**User's choice:** Pinned static-release + SHA-256
**Notes:** pandoc ships a ready static binary → no source-build stage (simpler than OCR's tesseract). No apt, no TeX toolchain (lean image; →pdf is v2).

---

## Live E2E pairs

| Option | Description | Selected |
|--------|-------------|----------|
| md→html + md→docx + webhook | text-out + zip-out output paths + signed webhook + presigned download | ✓ |
| + latex→md (reader-heavy) | add a LaTeX-parsing reader path | |

**User's choice:** md→html + md→docx + webhook
**Notes:** covers both output shapes (cheap text-out + heavier zip-writing). Operator-gated, mirrors OCR-45/av-36 live gates.

## Claude's Discretion
- Exact pinned pandoc version+SHA; corpus fixture sourcing + size buckets/N; compose resource limits (pandoc is light — could be < cpus:2/1g); stop_grace_period; optional latex→md corpus cell.

## Deferred Ideas
- KEDA/Helm → Phase 50; →pdf backend / broad formats / lua-filters → v2.
