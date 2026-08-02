---
id: SEED-017
status: dormant
planted: 2026-08-02
planted_during: post-v1.11 (asked while v1.12 "Consumer Accounting & Fairness" was being planned)
trigger_when: when a consumer needs a multi-page PDF assembled from parts, or a multi-page PDF broken into per-page files — OR when any other capability needs a job to produce more than one output
scope: Medium (split) / Medium (PDF merge) / Large (heterogeneous merge — architectural)
---

# SEED-017: PDF split and merge (and the multi-input/multi-output path they open)

Two requests that sound like one feature: break a multi-page PDF into
per-page files, and assemble several files into one multi-page PDF.

They are not one feature. They are three, of very different size, and only the
third changes the architecture.

## Why This Matters

Every job in OctoConv today is exactly one input file, one engine invocation,
one output file. That assumption is not written down as a constraint — it is
simply the only path anyone has built. These two requests are the first real
demand for anything else, which makes them the moment that assumption gets
tested.

## The foresight that already exists

Worth knowing before estimating: **the schema was designed for this from day
one.** `job_inputs` and `job_outputs` both carry an `ordinal int NOT NULL
DEFAULT 0` column (`internal/db/migrations/0001_init.sql:79,92`), and
`internal/jobs/repo.go` already has `ListInputs`/`ListOutputs` reading them
`ORDER BY ordinal`. The storage key layout is already
`uploads/{job_id}/{ordinal}-{filename}` / `results/{job_id}/{ordinal}-{filename}`.

The repository layer supports N. The limitation lives above it:

- `internal/api/handlers.go` reads a single multipart field `file` (`formFieldFile`)
  and returns a single `download_url`.
- `internal/worker/worker.go` reads input ordinal 0 and writes output ordinal 0.

So this is not a schema migration — it is an API contract and a worker read/write
path.

## The engines are already installed

`poppler-utils` is already in `Dockerfile.ocr-worker` (for `pdftoppm`/`pdfinfo`/
`pdftotext`). The same package ships **`pdfseparate`** (split) and **`pdfunite`**
(merge). No new heavy dependency, no new source build — unlike ffmpeg or
Tesseract, this capability arrives with a binary that is already in the image.

## The three requests, smallest first

### 1. Split: one PDF → N page files (Medium)

One input, N outputs. The upload contract is **unchanged**; only the result side
moves.

- Worker writes N rows to `job_outputs` with real ordinals.
- `GET /v1/jobs/{id}` must return a list of results rather than one
  `download_url` — this is the API's first breaking-shaped change, so it needs an
  additive design (keep `download_url` for single-output jobs, add an `outputs`
  array) rather than a silent replacement that breaks every existing consumer.
- Direct precedent exists: the OCR worker already walks a PDF page by page via
  `pdftoppm -f N -l N`, so per-page PDF handling is already proven in this
  codebase.

**This is the one to build first** — it unlocks the multi-output path without
touching upload, auth, size limits, or quota accounting.

### 2. Merge: N PDFs → one PDF (Medium)

N inputs, one output. Now the upload contract itself changes, and it drags in
everything attached to it:

- multipart with repeated file fields; content sniffing must run **per file**,
  fail-closed, before any S3 write (the existing "validate before storage write"
  ordering must hold for the whole set, not per item).
- `MAX_UPLOAD_BYTES` and the per-engine ceilings become set-wide, not per-file.
- v1.12's quota check (QUOT-02) must bound the aggregate.
- Ordering matters semantically here — page order is the user's intent, so
  ordinal is data, not a detail.

### 3. Heterogeneous merge: docx + jpg + pdf → one PDF (Large — architectural)

Each input must first be converted to PDF by its own engine (LibreOffice,
chromium, libvips), and only then merged. That is **not one engine invocation —
it is a pipeline**, and it breaks the assumption the whole system rests on:

- `EngineFor(source, target)` routes one pair to one converter. A pipeline has no
  single pair.
- Retry classification (`isXTerminal`) answers "is this failure retryable" for one
  invocation. What does it mean when step 3 of 5 fails — retry the step, or the job?
- Progress (v1.9) reports one engine's progress. A pipeline needs stage-aware progress.
- The reconciler re-enqueues by `jobs.engine`. A pipeline job has no single engine.

**Do not smuggle this in as an extension of #2.** If it is ever wanted, it is its
own architectural decision about whether OctoConv runs job DAGs — with its own
milestone.

## Nuances / invariants to respect

- **The API response change is the risky part of #1,** not the conversion. Every
  existing consumer reads `download_url`. Additive shape or the integrations break.
- **Ordinal is meaningful data for merge**, incidental for split. Page order is the
  user's intent and must survive a reconciler re-enqueue.
- **Byte accounting** (v1.12 ACCT-02) sums input and output bytes. With N outputs,
  reading ordinal 0 alone under-reports — see the cross-reference below.
- Splitting a 1000-page PDF produces 1000 objects with the storage TTL and the
  result-listing implications that follow. A page-count bound belongs in the design,
  mirroring OCR's own `OCR_MAX_PAGES` lever.

## Cross-reference to v1.12 (acted on at planting time)

This seed's existence changed a live milestone. v1.12 Phase 51 is about to write
per-client accounting, and the natural way to write it — read input ordinal 0,
read output ordinal 0 — would bake the 1:1 assumption into the accounting layer,
where retrofitting it later is expensive and easy to get subtly wrong.

A constraint was therefore added to Phase 51 and to ACCT-02 at planting time:
accounting must sum over `ListInputs`/`ListOutputs` rather than hardcode a single
ordinal. It costs nothing now; it is exactly the class of drift that bit this
project twice during v1.11.

## Breadcrumbs

- Ordinal columns: `internal/db/migrations/0001_init.sql:76-95`.
- Repository already handles N: `internal/jobs/repo.go` `ListInputs`/`ListOutputs`.
- The single-file/single-URL contract: `internal/api/handlers.go` (`formFieldFile`,
  the `download_url` response field).
- Engines already present: `Dockerfile.ocr-worker` poppler-utils layer.
- Per-page PDF precedent: the OCR worker's `pdftoppm -f N -l N` streaming.
