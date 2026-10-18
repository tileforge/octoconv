---
id: SEED-014
status: dormant
planted: 2026-07-26
planted_during: post-v1.9 (worker-ideas brainstorm)
trigger_when: scoping a milestone for file optimization / size-reduction (an operation that mutates, not converts, format)
scope: Medium
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: dormant
---

# SEED-014: File optimization / size-reduction engine

## Operator request (2026-10-18)

The operator asked whether OctoConv can compress files and asked to add it to the plan. As of v1.16 there is no compression operation (operations are `convert`, `render`, `sanitize`), and no archive output (zip is only recognized on input; packaging is SEED-006). Size can only change as a side effect:

- av transcode: `resolution_height` and `codec: hevc` (`internal/convert/avopts.go`);
- image: a format change such as png → webp/jpg, but `vips copy` takes no quality option (`internal/convert/libvips.go:82`);
- PDF, documents, audio: no size option.

Candidate scope for the milestone that takes this seed: a closed `quality` option for jpg/webp and a compression level for png in the image engine, CRF or bitrate for av and audio, PDF optimization (ghostscript), and a format-preserving "same format, fewer bytes" operation.

An engine that SHRINKS files without changing their format: **PDF optimization** (ghostscript `-dPDFSETTINGS`), **image optimization** (mozjpeg, pngquant, cwebp), maybe **video re-compress** (already partly the av engine). The operation is "make this file smaller" (same format in → same format out, fewer bytes).

## Why This Matters

Concrete ops/cost value: outbound PDFs and images are frequently bloated (uncompressed scans, full-res photos). A "optimize this PDF/image" operation cuts storage + bandwidth for downstream company services. It's a different operation shape from conversion — format-preserving mutation — but fits the async engine-class contour cleanly.

## When to Surface

**Trigger:** a file-optimization/size-reduction milestone. Follows the engine-class pattern (own queue, worker, container with ghostscript/mozjpeg/pngquant/cwebp, fail-closed validation, stage-aware retry). Compute is bounded but non-trivial (ghostscript re-renders PDFs) → likely wants an RTF-ish timeout gate.

## Open Questions

1. **Quality knobs** — optimization is lossy (image quality, PDF DPI downsampling). Expose a validated `opts` quality/target level (closed allowlist, mirror AudioOpts) — never a raw client-controlled ghostscript flag.
2. **Format-preserving contract** — same format in → out; decide behavior when optimization would INCREASE size (return original? fail? flag?).
3. **Scope** — PDF-only first (highest value), or images too from day one.
4. **Overlap with av** — video re-compress overlaps the av engine's transcode; keep this to PDF/image, leave video to av.

## Scope Estimate

**Medium** — 1-3 binaries, no new conversion semantics (format-preserving), but the lossy-quality `opts` contract + the "optimization made it bigger" edge case add design surface.

## Breadcrumbs

- Engine-class precedents: `internal/convert/`, `cmd/*-worker`, `internal/queue/queue.go`, the KEDA/Helm chart + `scripts/*-rtf-measure.sh` (if a timeout gate is wanted).
- `opts` validation precedent: `internal/convert/audioopts.go` / `avopts.go` (closed allowlist + injection test).
