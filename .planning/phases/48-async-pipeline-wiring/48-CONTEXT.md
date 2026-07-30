# Phase 48: Async Pipeline Wiring - Context

**Gathered:** 2026-07-30
**Status:** Ready for planning

<domain>
## Phase Boundary

Pandoc jobs (markup → markup) flow end-to-end through the async pipeline — migration adds `pandoc` to the `jobs.engine` enum, a dedicated `pandoc` asynq queue + `cmd/pandoc-worker` consume jobs (queued→active→done), with correct retry/dedup semantics and engine-routing, over the UNMODIFIED v1.9 progress/SSE/webhook machinery. A near-verbatim clone of OCR Phase 44 (audio Phase 31 / av Phase 35). Registers the Phase-47 `PandocConverter` into `convert.Default` and wires the Phase-47 content-validation probes into the upload gate. Covers PDCP-01.

**In scope:** migration `0010_pandoc_engine.sql` (additive `pandoc` in the engine CHECK enum); `internal/queue` (TypePandocConvert, QueuePandoc, NewPandocConvertTask, PandocUniqueTTL, EnqueuePandocConvert, register QueuePandoc in AllConvertQueues); `cmd/pandoc-worker`; `internal/worker` (HandlePandocConvert + isPandocTerminal + pandocFailureCode); reconciler routing for `engine='pandoc'`; API routing (register PandocConverter in converters.go init(); the `EnginePandoc` opts-dispatch case + enqueue-switch case in handlers.go; wire the Phase-47 markup content-probes into the detected=="" branch + enforcePandocInputSize at upload); single-input/single-target.
**Out of scope:** container/Dockerfile/measured timeout (Phase 49 — Phase 48 uses a clearly-marked placeholder PANDOC_ENGINE_TIMEOUT); KEDA/Helm (Phase 50); any change to v1.9 progress/SSE/webhook code; →pdf / broad formats / lua-filters (v2). The `PandocConverter` core, `PandocOpts`, and the sniff probes are DONE (Phase 47) — this phase WIRES them, it does not rebuild them; `exec.go`/`progress.go`/`sse.go` stay byte-for-byte unchanged.
</domain>

<decisions>
## Implementation Decisions

### Stage-aware retry classification (SC3, PDCP-01)
- **D-01:** `isPandocTerminal` classifies **timeout AND pandoc engine/parse errors as TERMINAL** (SkipRetry, MarkFailed) — mirroring the DOCUMENT engine's `isDocumentTerminal` timeout-is-terminal divergence (DOC-08), NOT the OCR/image transient-timeout behavior. Rationale: pandoc conversion is deterministic and CPU-bound with no external services/network — a timeout means pathological/oversize input that a retry cannot fix, and a pandoc parse/engine error means bad input. Only GENUINE infra transients (worker crash mid-attempt, DB blip, asynq redelivery) remain retryable via the default path. Checked BEFORE the shared-isTerminal fallthrough, engine-scoped like `isDocumentTerminal`/`isOCRTerminal` (never reword another engine's signature). Empty-but-valid output completes `done`, never `failed` (SC4) — no `if output==""{error}` branch; the converter already handles this (Phase 47).

### Content-sniff wiring + opts-dispatch (SC1/SC2)
- **D-02:** MIRROR the existing html/OCR upload-gate precedent VERBATIM (handlers.go). In the `detected==""` branch, add a markup-family dispatch: `latex`→`LooksLikeLaTeX`; `md`/`rst`/`org`→`LooksLikeText` (trust the declared source among the text-family per Phase-47 D-01); `docx`/`epub` already resolve via the existing `SniffContainer` (Phase 47 added epub recognition). Add a `case convert.EnginePandoc` to BOTH the opts-dispatch switch (`ParsePandocOpts` + `ValidatePandocApplicability`, normalize+persist) AND the enqueue-switch (`EnqueuePandocConvert`) — exactly the two sites the OCR wiring uses (handlers.go ~547 opts, ~677 enqueue). Enforce `enforcePandocInputSize` (Phase 47) at the pandoc-engine upload path. Register `PandocConverter{}` in `converters.go init()`.
  - **Registry disjointness (factual constraint, not a choice):** `Registry.Register` panics on a duplicate `(source,target)` pair, so `PandocConverter.Pairs()` MUST be disjoint from every existing engine's pairs. It already is — Phase 47's curated set excluded all `→pdf` pairs (document owns docx→pdf, chromium owns html→pdf) and targets the markup family only. Add a `TestPandocPairsDisjointFromOtherEngines` (mirror `TestOCRPairsDisjointFromOtherEngines`) as the guard. EngineFor's (source,target) routing then disambiguates automatically (e.g. `(html,pdf)`→chromium, `(html,md)`→pandoc; `(docx,pdf)`→document, `(docx,md)`→pandoc) with ZERO new dispatch code.

### PandocUniqueTTL + retry schedule (SC3)
- **D-03:** `PandocUniqueTTL` is derived FRESH from pandoc's OWN budget — `f(PANDOC_ENGINE_TIMEOUT, PANDOC_MAX_RETRY)` — NEVER copied from a neighboring engine (the recurring project anti-pattern). Because the real timeout is measured only in Phase 49, Phase 48 uses a CLEARLY-MARKED PLACEHOLDER `PANDOC_ENGINE_TIMEOUT` (a small value — pandoc is fast — e.g. ~60–120s, comment: superseded by Phase 49's measured value, exactly the OCR Phase-44 300s→Phase-45 744s precedent). A SHORT 2-entry retry schedule (pandoc is fast; and most engine errors are terminal so retry rarely fires) — mirror `ocrRetrySchedule`'s 2-entry shape, values pandoc's own (not copied). `PANDOC_MAX_RETRY` defaults like the other fast engines (e.g. 2).

### Claude's Discretion
- The exact placeholder `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY` defaults + retry-schedule values (pandoc's own, marked placeholder); the `pandocFailureCode` mapping; the migration ordinal (`0010`); the precise `cmd/pandoc-worker` env-resolver shape (mirror `cmd/ocr-worker`, minus the OCR-specific cgroup/thread bits pandoc doesn't need) — all executor/planner's call, provided: timeout+engine errors are terminal, TTL is from pandoc's own budget, the wiring mirrors the html/OCR precedent, v1.9 infra stays unchanged, and the placeholder timeout is clearly flagged for Phase 49.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The OCR Phase 44 wiring to CLONE (the closest, most recent analog)
- `.planning/milestones/v1.10-phases/44-async-pipeline-wiring/` (44-01..04 SUMMARY/PLAN) — the exact 4-plan wiring shape (migration+registry/routing → queue/task/client → HandleXConvert/isXTerminal/cmd/x-worker/reconciler → live proof). Note: Phase-44's live queued→active→done proof was its Plan 04; here the live E2E is Phase 49 (container) — Phase 48 proves wiring via integration tests, not a live container run.
- `internal/db/migrations/0009_ocr_engine.sql` — the additive engine-enum migration to clone as `0010_pandoc_engine.sql`.
- `internal/queue/queue.go` (TypeOCRConvert, QueueOCR, NewOCRConvertTask, OCRUniqueTTL, AllConvertQueues incl QueueOCR) + `client.go` (EnqueueOCRConvert, NewClient reads OCR_MAX_RETRY+OCR_ENGINE_TIMEOUT) — the queue/producer contract to clone.
- `internal/worker/worker.go` (HandleOCRConvert, isOCRTerminal, ocrFailureCode, the isTerminal/isDocumentTerminal engine-scoped-classifier pattern — esp. `isDocumentTerminal`'s timeout-is-terminal for D-01) + `cmd/ocr-worker/main.go` (the worker entrypoint + env resolver to clone, minus OCR-specific cgroup/OMP bits).
- `internal/api/handlers.go` — the opts-dispatch switch (~490-560, the EngineOCR case) + the enqueue-switch (~660-680, `case convert.EngineOCR: EnqueueOCRConvert`) + the detected=="" content-gate branch (~201-280, LooksLikeHTML precedent to extend for the markup family).
- `internal/reconciler/reconciler.go` — the per-`jobs.engine` recovery routing (add `pandoc` → EnqueuePandocConvert).
- `internal/api/api.go` (Enqueuer interface — add EnqueuePandocConvert).

### The Phase-47 assets being WIRED (built, tested, DONE — reuse, do not rebuild)
- `internal/convert/pandoc.go` (PandocConverter Pairs/Engine/Convert), `pandocopts.go` (PandocOpts, ParsePandocOpts, ValidatePandocApplicability), `markupsniff.go` (LooksLikeLaTeX, LooksLikeText, enforcePandocInputSize, ErrPandocInputTooLarge), `convert.go` (EnginePandoc const), `docsniff.go` (epub via SniffContainer), `sniff.go` (MIMEType md/rst/org/latex/epub).

### Requirements + prior
- `.planning/REQUIREMENTS.md` — PDCP-01. `.planning/ROADMAP.md` §"Phase 48" — the 4 Success Criteria. `.planning/phases/47-pandoc-engine-foundation/47-CONTEXT.md` + 47-VERIFICATION.md (what the foundation delivered).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The entire OCR Phase-44 wiring is the near-verbatim clone target (queue/client, worker handler + classifier, cmd/worker, migration, reconciler route, handlers enqueue+opts+content-gate).
- The Phase-47 PandocConverter/PandocOpts/sniff-probes — registered + wired, not rebuilt.
- `isDocumentTerminal` (worker.go) — the timeout-is-terminal precedent for D-01.

### Established Patterns
- Engine-class routing: own queue + own worker binary + payload carries only job_id (all detail re-read from Postgres); EngineFor by (source,target).
- Fresh per-engine UniqueTTL from the engine's OWN retry/timeout budget (never copied) — the project's flagged anti-pattern.
- Postgres-first double write; guarded status transitions; reconciler engine-routed recovery.
- Placeholder timeout in the wiring phase → measured in the containerization phase (OCR 44→45 precedent).

### Integration Points
- `internal/db/migrations/0010_pandoc_engine.sql` (new); `internal/queue/*` (+pandoc); `cmd/pandoc-worker/` (new); `internal/worker/worker.go` (+HandlePandocConvert/isPandocTerminal); `internal/reconciler/reconciler.go` (+pandoc route); `internal/api/{api.go,handlers.go}` (+EnqueuePandocConvert interface + enqueue/opts/content-gate cases); `internal/convert/converters.go` (register PandocConverter). exec.go/progress.go/sse.go UNCHANGED.
</code_context>

<specifics>
## Specific Ideas
- Clone OCR Phase 44 plan-by-plan: (1) migration 0010 + register PandocConverter + API routing (enqueue+opts+content-gate wiring) + Pairs-disjointness test; (2) queue/task/client (TypePandocConvert/QueuePandoc/PandocUniqueTTL/EnqueuePandocConvert + AllConvertQueues); (3) HandlePandocConvert + isPandocTerminal + pandocFailureCode + cmd/pandoc-worker + reconciler route; integration tests prove queued→active→done + terminal-classification + empty→done.
- PandocConverter Pairs disjointness test is the guard against a Register panic; run it in this phase.
</specifics>

<deferred>
## Deferred Ideas
- Container + measured PANDOC_ENGINE_TIMEOUT → Phase 49 (this phase's placeholder is superseded there).
- KEDA/Helm → Phase 50.
- →pdf / broad formats / lua-filters → v2.

</deferred>

---

*Phase: 48-async-pipeline-wiring*
*Context gathered: 2026-07-30*
