---
id: SEED-013
status: dormant
planted: 2026-07-26
planted_during: post-v1.9 (worker-ideas brainstorm)
trigger_when: scoping a milestone for a metadata / privacy / file-processing engine (uses the reserved `probe` engine enum value)
scope: Medium
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: dormant
---

# SEED-013: Metadata extraction & scrub engine (EXIF/IPTC/XMP)

A file-processing engine (via **exiftool**) that (a) READS structured metadata from uploads (EXIF/IPTC/XMP/PDF-info) and (b) STRIPS it — the privacy operation "remove GPS/camera/author metadata from this file before it's shared." Maps naturally onto the reserved `probe` engine enum value (metadata-probing).

## Why This Matters

This is a real, high-value privacy/security capability for an internal conversion service: outbound images/PDFs often carry GPS coordinates, device serial numbers, author names, edit history — a leak vector. A "scrub metadata" operation is a concrete compliance/privacy win, distinct from format conversion. The `probe` enum value was reserved for exactly this class (metadata-probing, NOT visual preview — that's SEED-007).

## When to Surface

**Trigger:** a metadata/privacy/file-processing milestone. Note: this is NOT a format conversion — it's read/strip, so the `operation` might be `inspect` (read) and `convert`/`extract` (strip → cleaned copy). Follows the engine-class pattern (own queue, worker, container, fail-closed validation) but the "conversion" is metadata mutation, not format change.

## Open Questions

1. **Read vs strip vs both** — v1 could be strip-only (the privacy win), read-only (an `inspect` operation returning JSON), or both.
2. **Output shape** — strip → a cleaned copy of the SAME file (bytes minus metadata); read → a JSON metadata document. Different result contracts.
3. **Format coverage** — exiftool covers hundreds of types; pick a validated allowlist (images + PDF + office to start).
4. **`operation` modeling** — this stretches the current convert-centric model (`operation IN convert/extract/archive/inspect/render` already exists in `0001_init.sql`, so `inspect` is available). Decide how read-metadata (returns data, no S3 output) fits the job/result contract.

## Scope Estimate

**Medium** — one binary (exiftool), no heavy compute (fast, no RTF gate needed), but the read-vs-strip result contract + the `inspect` operation modeling add design surface.

## Breadcrumbs

- `internal/db/migrations/0001_init.sql` — `operation IN (...)` includes `inspect`; `engine IN (... 'probe' ...)` reserves the enum value.
- Engine-class precedents in `internal/convert/`, `cmd/*-worker`, `internal/queue/queue.go`.
- Distinct from SEED-007 (file-preview/thumbnail = visual) and SEED-009 (OCR = text from pixels).
