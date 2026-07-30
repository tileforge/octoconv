# Phase 44: Async Pipeline Wiring - Context

**Gathered:** 2026-07-26
**Status:** Ready for planning

<domain>
## Phase Boundary

OCR jobs (image/PDF → txt/hOCR/alto) flow END-TO-END through the async pipeline — API create → `ocr` asynq queue → `cmd/ocr-worker` → S3 result → done — with correct retry/dedup/engine-routing, over the COMPLETELY UNMODIFIED v1.9 progress/SSE/webhook machinery. This registers the Phase-43 standalone `OCRConverter` into the live lifecycle. A mechanical repeat of the audio (Phase 31) / av (Phase 35) wiring. Covers OCRE-01, OCRE-05, OCRP-01.

**In scope:** migration `0009_ocr_engine.sql` (adds `ocr` to the `jobs.engine` CHECK enum); `matchPDF` in the sniff table; register `OCRConverter` in `converters.go`; the `ocr` queue + `NewOCRConvertTask`/`OCRUniqueTTL`; `cmd/ocr-worker`; `HandleOCRConvert` (clone of `HandleAVConvert` incl. the progressSink + cancel wiring); `isOCRTerminal`; reconciler + API routing for `engine=ocr`; per-engine OCR upload limit; single-output.
**Out of scope:** the container/Dockerfile + measured timeout (Phase 45), KEDA/Helm (Phase 46), any converter-logic change (Phase 43 is done), the dynamic registry (SEED-010 dormant), progress.go edits (reuse only).
</domain>

<decisions>
## Implementation Decisions

### Engine routing (OCRP-01, SC1)
- **D-01:** NO new routing logic. The existing `convert.Default.EngineFor(from, to)` already takes BOTH source AND target (`Registry.Lookup(from,to) → Engine()`, `convert.go:97`), and `handleCreateJob` already calls `EngineFor(detected, target)` (`handlers.go:356`). So once `OCRConverter.Pairs()` registers its `(image→txt/hocr/alto)` and `(pdf→txt/hocr/alto)` pairs, routing disambiguates automatically: `jpg→png` → image engine, `jpg→txt` → ocr engine. The only new pieces: (a) `matchPDF` (`%PDF-`) in the closed `sniff.go` signatures table so PDF inputs `detected` as `pdf`; (b) register `OCRConverter{}` in `converters.go` init. Rejected: an explicit client `engine=ocr` hint (breaks uniformity — the other 5 engines have none — and duplicates the registry's job).

### operation value (OCRP-01)
- **D-02:** `jobs.operation = 'convert'` for OCR jobs (source image/pdf → target txt/hocr/alto). Consistent with the audio engine (mp3→txt transcript is also `'convert'`, not `'extract'`) and every other engine. The `'extract'` enum value stays reserved/unused-for-OCR. No operation-enum migration needed (`convert` already valid since `0001_init.sql`).

### Retry classification (OCRP-01, SC5)
- **D-04 (locked by recommendation, unopposed):** `isOCRTerminal` (mirrors `isAVTerminal`, `worker.go:355`): **input/client faults are TERMINAL** (SkipRetry, no MarkFailed-then-retry) — `ErrOCRBornDigital`, `ErrOCRTooManyPages`, `ErrOCRPageBoxTooLarge`, `ErrOCRImageTooLarge`, unknown-language, unsupported-target (all deterministic on the input — a retry can never help). A **TIMEOUT** is TRANSIENT (retry may succeed when worker load drops, mirroring av transcode-timeout). Any other genuine engine crash (tesseract/pdftoppm non-timeout failure) is TERMINAL (OCR is deterministic on its input, so it will recur). `OCRUniqueTTL` is derived from the OCR engine's OWN timeout/retry budget (Phase 45's measured `OCR_ENGINE_TIMEOUT` × retry), NEVER copied from another engine (IN-02 discipline).

### Upload limit (OCRE-01)
- **D-03:** Per-engine OCR upload ceiling = **100 MiB** (same as image; `api.go:226`-style per-engine map), env-tunable. Reasonable for images and scanned PDFs; the actual work is separately bounded by the page-count cap (OCRE-02, Phase 43). Rejected: 200 MiB (unneeded S3/memory headroom) and 50 MiB (could cut legitimate large multi-page scans).

### Claude's Discretion
- The `matchPDF` byte-signature detail (`%PDF-` at offset 0, allowing a small leading-whitespace tolerance per spec); the exact `NewOCRConvertTask`/queue-name wiring (clone `NewAVConvertTask`); the single-output storage key/extension per target (txt/hocr.html/alto.xml — mirror how audio names {txt,srt,vtt,json} outputs); the OCR upload-limit env var name — all executor/planner's call, provided: the progress/SSE/webhook machinery and `progress.go` stay byte-for-byte unchanged, both external processes go through `runCommand`, and `isOCRTerminal`/`OCRUniqueTTL` follow the av precedent.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The av/audio wiring to CLONE (the mechanical template)
- `internal/worker/worker.go` — `HandleAVConvert` (:938, the handler to clone as `HandleOCRConvert` incl. progressSink + `cancel.Default.Register` + terminal-100/freeze); `isAVTerminal` (:355) / `isAudioTerminal` (:293) — the `isOCRTerminal` template; the metrics.RecordJobOutcome calls.
- `internal/queue/queue.go` — `NewAVConvertTask`/`TypeAVConvert`/`QueueAV`/`AVUniqueTTL` — clone as the `ocr` task/queue/`OCRUniqueTTL`.
- `internal/convert/converters.go` — the `init()` `Default.Register(...)` list (add `OCRConverter{}`).
- `cmd/av-worker/main.go` — the worker main to clone as `cmd/ocr-worker/main.go` (asynq server bound to the `ocr` queue, cancel subscriber, progress throttle wiring — minus the av-specific env; Phase 45 adds OCR env).

### API + routing + sniff (OCRE-01, D-01)
- `internal/convert/convert.go` — `EngineFor(from,to)` (:97), `Lookup`/`Supports` — already (source+target)-based; OCR pairs plug in.
- `internal/api/handlers.go` — `handleCreateJob` (`EngineFor(detected, target)` :356, the 422-fail-closed sniff/pair path); `internal/api/api.go:226` per-engine upload map (add `EngineOCR`).
- `internal/convert/sniff.go` — the closed `signatures` table (add `matchPDF`); existing image sniffers already cover jpg/png/tiff/webp.
- `internal/db/migrations/0001_init.sql:48` — the `engine IN (...)` CHECK the new `0009_ocr_engine.sql` extends with `'ocr'`.
- `internal/reconciler/*` — the stale-sweep that routes `jobs.engine` to its queue (add `ocr`).

### Phase 43 converter + guards (consumed unchanged)
- `internal/convert/ocr.go`, `ocropts.go`, `ocrguards.go`, `ocrpdf.go`, `ocrprogress.go` — the standalone converter this phase registers + wires; `ErrOCR*` sentinels feed `isOCRTerminal`.

### Requirements + progress-reuse invariant
- `.planning/REQUIREMENTS.md` — OCRE-01, OCRE-05, OCRP-01.
- `internal/convert/progress.go` + `internal/api/sse.go` + the worker progressSink/webhook — MUST stay byte-for-byte unchanged (reuse only; SC4 live-verifies the ramp).
- `.planning/phases/43-ocr-engine-foundation/43-CONTEXT.md` (D-01..D-05a).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `HandleAVConvert` + `isAVTerminal` + `NewAVConvertTask`/`AVUniqueTTL` + `cmd/av-worker/main.go` — the near-verbatim clone targets (av is the closest analog: same guard-before-subprocess + progressSink + cancel shape).
- `EngineFor(from,to)` — already (source+target)-routing; zero new routing code (D-01).
- per-engine upload map (`api.go`) + the closed sniff signatures table — add one entry each.
- The v1.9 progressSink/SSE/webhook — engine-agnostic; OCR's `ConvertWithProgress` (Phase 43) plugs in with zero machinery change.

### Established Patterns
- Per-engine queue + `cmd/<x>-worker` + register-in-init + isXTerminal + XUniqueTTL-from-own-budget (IN-02) — the whole engine-class wiring recipe, applied 5× already.
- Additive migration (0007/0008 precedent) → `0009_ocr_engine.sql` (ALTER the CHECK enum; must apply clean fresh + populated).
- Guarded-transition is the only status path; single `AddOutput` per job (single-target, D-01 Phase 43).

### Integration Points
- `internal/api` (sniff+route+limit) ← `internal/convert` (register + EngineFor) → `internal/queue` (ocr task/queue) → `internal/worker` (HandleOCRConvert) → `internal/db` (0009 migration) → `internal/reconciler` (ocr routing). Postgres needed for the migration + DB round-trip tests (octoconv-db :5434).
</code_context>

<specifics>
## Specific Ideas

- `matchPDF`: `%PDF-` signature; PDF is the ONLY genuinely-new input content type (images already sniffed) — it slots into the closed table with no new branching in `handleCreateJob` (per Phase 43 research ARCHITECTURE finding).
- SC4 live E2E: a single image job's progress goes null→100; a multi-page scanned PDF ramps page-by-page over GET/SSE — proving the reused progress machinery serves OCR unchanged. This is a live-stack check (compose), a natural operator/self gate like the av/SSE proofs (Phase 45/46 have the container; a live E2E here may run against a locally-built ocr-worker or defer the live ramp proof to Phase 45's container E2E — planner's call).
- Migration 0009 applies cleanly fresh + populated (additive ALTER of the engine CHECK) — an explicit acceptance criterion.
</specifics>

<deferred>
## Deferred Ideas

- Container/Dockerfile + measured `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` env wiring → Phase 45.
- KEDA/Helm scale-from-zero → Phase 46.
- Dynamic capability registry (SEED-010) → dormant; compile-time `Register` here.
- born-digital passthrough / searchable-PDF / deskew / OSD → v2.
</deferred>

---

*Phase: 44-async-pipeline-wiring*
*Context gathered: 2026-07-26*
