---
id: SEED-015
status: dormant
planted: 2026-07-26
planted_during: post-v1.9 (worker-ideas brainstorm)
trigger_when: scoping a milestone that adds a new engine class beyond image/document/html/audio/av/ocr
scope: Large
---

# SEED-015: Tabular / data-format engine (csv ↔ xlsx ↔ json ↔ parquet)

An engine that interconverts **structured data formats**: csv ↔ xlsx ↔ json ↔ parquet ↔ ndjson (via duckdb or pandas). Distinct from the document engine's "xlsx → pdf" (that's a rendered document); this is *data* interconvert that preserves rows/columns/types for downstream data pipelines.

## Why This Matters

Internal data/ETL services routinely need "give me this xlsx as parquet" or "this csv as json." The document engine (LibreOffice) can render a spreadsheet to PDF but does NOT do faithful data interconvert (types, large row counts, parquet). A data engine fills that gap and is a natural fit for an internal service that already does async file conversion.

## When to Surface

**Trigger:** scoping a new engine class. Pattern: own queue `data`/`tabular` + `cmd/data-worker` + container (duckdb is a single lightweight binary; or python+pandas — heavier), fail-closed validation, stage-aware retry, RTF-measured timeout for large files.

## Open Questions

1. **Engine choice** — duckdb (single fast C++ binary, SQL-native, reads/writes csv/parquet/json/xlsx via extensions) vs python+pandas (heavier, more format edge-cases). duckdb is the lean fit; eval-gate like OCR/CAD.
2. **Type fidelity** — csv is untyped; xlsx/parquet are typed. Decide the type-inference/coercion contract and how to report ambiguity (fail-closed on unparseable?).
3. **Size bounds** — parquet/xlsx can be huge; needs a row/byte ceiling (decompression-bomb analog) + streaming, not full in-memory.
4. **Schema/opts** — per-job opts for delimiter, header, sheet selection, type overrides (validated closed allowlist).

## Scope Estimate

**Large** — a full engine-class milestone; the type-fidelity + size-bound contracts add real design surface beyond a plain "shell out to a binary" engine.

## Breadcrumbs

- Engine-class precedents: `internal/convert/` (Pairs/Converter/sniffers), `cmd/*-worker`, `internal/queue/queue.go`, KEDA/Helm chart.
- Decompression-bomb precedent to mirror for size bounds: the image dimension limit (Phase 7) + av resolution/duration guards.
- Boundary vs document engine: LibreOffice owns "spreadsheet → rendered PDF"; this owns "data → data."
