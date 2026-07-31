# Phase 49: Containerization + Measured Timeout Gate - Context

**Gathered:** 2026-07-31
**Status:** Ready for planning

<domain>
## Phase Boundary

A running `pandoc-worker` container in docker-compose passes a full live E2E (upload markup → poll → presigned download + signed webhook), with `PANDOC_ENGINE_TIMEOUT` sized from a MEASURED conversion-cost gate (`scripts/pandoc-timing-measure.sh`) — never copied/guessed — in a LEAN TeX-FREE image. Near-verbatim clone of OCR Phase 45 (audio 32 / av 36). Covers PDCE-04.

**In scope:** `Dockerfile.pandoc-worker` (pinned static-release pandoc, NO TeX, debian-slim, USER nobody); the compose `pandoc-worker` service; `scripts/pandoc-timing-measure.sh` (supervised wall-sec/conversion gate) → derived `PANDOC_ENGINE_TIMEOUT` (+ Path-B `PANDOC_MAX_INPUT_BYTES` lever); IN-02 `PANDOC_*` env propagation across every queue-client service; CI bake-matrix entry; the recorded Key Decision (numbers + conditions); the live E2E.
**Out of scope:** KEDA/Helm ScaledObject (Phase 50); any converter/worker/queue-logic change (47/48 done); →pdf / TeX / broad formats (v2). The `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_INPUT_BYTES` Go plumbing already exists (Phase 48 placeholder) — this phase MEASURES + sets the real value; `exec.go`/`progress.go`/`sse.go` stay byte-for-byte unchanged.
</domain>

<decisions>
## Implementation Decisions

### Measurement metric + corpus + derivation (PDCE-04, SC2/SC3)
- **D-01:** `scripts/pandoc-timing-measure.sh` measures **p95 WALL-CLOCK SECONDS PER CONVERSION** (NOT per-page like OCR, NOT an RTF ratio like audio/av — pandoc cost scales with input SIZE × the (from,to) pair, not with a page count or media duration). Corpus (D-02 of OCR-45 shape): a representative matrix of **source formats × input sizes × representative targets** — e.g. readers {md, latex, html, docx} × sizes {small ~KB, medium ~100KB, large @≈PANDOC_MAX_INPUT_BYTES} × targets {html (cheap text-out), docx/epub (heavier zip-writing output)} — run at the compose `pandoc-worker` CPU/memory limits, N runs per cell, nearest-rank p95. Derivation: `PANDOC_ENGINE_TIMEOUT = ceil(worst-cell p95_sec × 2.0 safety)` (the ×2.0 mirrors audio/av/OCR), which MUST stay under the global reconciler CAP (900s/15m, `RECONCILER_ACTIVE_STALE_AFTER`). **Path-B lever:** if the derived timeout breaches the CAP, lower `PANDOC_MAX_INPUT_BYTES` (the input-size cap, Phase 47's guard) until it fits — the analog of OCR's `OCR_MAX_PAGES` lever — NEVER copy a neighbor's timeout. Measurement is SUPERVISED (orchestrator-run, like the OCR/av RTF runs); no timeout value is committed before a real run prints the p95. (Expectation: pandoc is fast — likely a small timeout well under CAP, Path-B unlikely to fire — but the lever + measurement discipline are mandatory regardless.)

### Pandoc binary pinning (SC1)
- **D-02:** `Dockerfile.pandoc-worker` installs a **PINNED official static-release pandoc binary + SHA-256** (confirming Phase-47 D-03), NOT apt, NOT a GHC source-build. Download the pinned `pandoc-X.Y.Z-linux-amd64.tar.gz` from pandoc's GitHub releases (the statically-linked binary), `sha256sum -c` fail-closed BEFORE use, `COPY` the extracted `pandoc` binary into a `debian:bookworm-slim` runtime, `USER nobody`. **NO TeX toolchain** (the whole point of the lean image — markup↔markup only, →pdf deferred to v2). Version = latest stable pandoc 3.x, resolved live by the executor (local dev is 3.10.1; pin the current stable release + its published SHA-256). Mirrors the audio-worker's SHA-pinned-bake / OCR-worker's tessdata-pin discipline, but pandoc ships a ready static binary so there is no source-build stage.

### Live E2E scope (SC1)
- **D-03:** the live E2E (against the real compose `pandoc-worker`) proves **md→html** (cheap text-output path) **AND md→docx** (the heavier zip-writing output path) end-to-end (upload → poll → presigned download of a genuine pandoc-rendered result) **+ a signed webhook HMAC-verified** against `WEBHOOK_SIGNING_SECRET`. Covering both a text-out and a zip-out target proves the two distinct output shapes. Operator-gated (orchestrator runs it, brings PASS for approval), mirroring the OCR Phase-45 / av Phase-36 live E2E gates.

### Claude's Discretion
- The exact pinned pandoc version + SHA-256 (resolve live); the corpus fixture sourcing (synthesize markup fixtures in-container vs commit small ones under testdata) and exact size buckets/N; the compose `pandoc-worker` resource limits (mirror the OCR/av cpus:2/1g unless the measurement says otherwise — pandoc is light, could be less); the `stop_grace_period` (measured timeout + margin, like OCR's 764s); whether to add a small latex→md fixture to the corpus for reader-heavy coverage — all executor/planner's call, provided: the timeout is DERIVED from the measured p95 (never copied), stays under the CAP, is recorded as a Key Decision with conditions, IN-02 parity is identical across all queue-client services, and the image is TeX-free.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The OCR Phase 45 containerization + timing-gate to CLONE
- `Dockerfile.ocr-worker` — the debian-slim runtime + SHA-pinned-data + USER nobody shape to clone as `Dockerfile.pandoc-worker` (but SIMPLER: pandoc is a single pinned static binary COPY, no source-build stage, no data-bake, no poppler/tesseract).
- `scripts/ocr-timing-measure.sh` — THE supervised measurement-gate to clone as `scripts/pandoc-timing-measure.sh`: the OrbStack/k8s-down guard, docker build, --cpus/--memory long-lived container, in-container corpus synthesis, timed per-cell p95 nearest-rank, the closing DERIVATION HELP block that PRINTS (never commits) the formula, integrity-only exit. Adapt the metric from seconds-per-page to WALL-SEC-PER-CONVERSION and the lever from OCR_MAX_PAGES to PANDOC_MAX_INPUT_BYTES.
- `docker-compose.yml` ocr-worker service (OCR_ENGINE_TIMEOUT/stop_grace_period/resource limits + the IN-02 propagation of OCR_* across all queue-client services + RECONCILER_ACTIVE_STALE_AFTER=15m CAP) — the pandoc-worker service template.
- `.planning/milestones/v1.10-phases/45-containerization-measured-timeout-gate/` (45-01..04 PLAN/SUMMARY) — the 4-plan shape: Dockerfile+env → script+compose+IN-02+CI → SUPERVISED measurement checkpoint (derive+record Key Decision) → live E2E checkpoint. Plans 03 (measurement) and 04 (live E2E) are OPERATOR gates (autonomous:false).

### Measured-timeout wiring (PDCE-04)
- `internal/queue/queue.go`/`client.go` (Phase 48: PandocUniqueTTL, the PANDOC_ENGINE_TIMEOUT/PANDOC_MAX_RETRY reads — now fed by the measured value; IN-02: every queue.Client service sets PANDOC_* identically).
- `cmd/pandoc-worker/main.go` (Phase 48 — reads PANDOC_ENGINE_TIMEOUT/PANDOC_MAX_RETRY/PANDOC_WORKER_CONCURRENCY); `internal/convert/markupsniff.go` `enforcePandocInputSize`/PANDOC_MAX_INPUT_BYTES (Phase 47 — the Path-B lever's guard).

### Requirements + prior
- `.planning/REQUIREMENTS.md` — PDCE-04. `.planning/ROADMAP.md` §"Phase 49" — the 4 Success Criteria. The OCR 45-03-SUMMARY (measured-timeout derivation precedent) + the PROJECT.md Key Decisions table (where the measured number is recorded). `.github/workflows/ci.yml` (the bake matrix — add pandoc-worker).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `Dockerfile.ocr-worker` / `scripts/ocr-timing-measure.sh` / the compose ocr-worker service — near-verbatim clone targets (simplified: pandoc = one pinned static binary, no source-build/data-bake).
- The reconciler CAP (900s/15m) + the Path-B lever (lower the input cap until the timeout fits) — the OCR/av derivation pattern.
- The compose IN-02 propagation pattern (OCR_* across all queue-client services) — apply to PANDOC_*.
- Phase 48's `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY`/`PANDOC_MAX_INPUT_BYTES` env plumbing (the placeholder now gets its real value).

### Established Patterns
- Pinned engine binary in the runtime image (SHA-pinned); lean slim runtime, USER nobody.
- Measure-don't-assume timeout: derive from p95 × safety, under CAP, recorded as a Key Decision, Path-B lever if over CAP.
- Per-worker resource limits matching the measurement container exactly.

### Integration Points
- `Dockerfile.pandoc-worker` + compose `pandoc-worker` service + `scripts/pandoc-timing-measure.sh` + the PANDOC_* env reads (Phase 48) + CI bake matrix. Measurement + live E2E run against the live docker-compose stack (operator/self-run, like OCR/av).
</code_context>

<specifics>
## Specific Ideas
- Supervised measurement flow (like OCR): build `pandoc-worker`, run `pandoc-timing-measure.sh` over the formats×sizes×targets corpus at compose limits, read the per-cell p95 wall-sec, derive PANDOC_ENGINE_TIMEOUT = ceil(worst-p95 × 2.0), confirm < CAP (else lower PANDOC_MAX_INPUT_BYTES), record the Key Decision with arch/cpu/mem/N/p95. I (orchestrator) run this and bring the numbers for approval.
- Live E2E: md→html + md→docx + signed webhook against the real pandoc-worker container — I run it and bring PASS for approval.
- Lean image: pandoc static binary only — NO texlive/latex/wkhtmltopdf/weasyprint (→pdf is v2).
</specifics>

<deferred>
## Deferred Ideas
- KEDA/Helm ScaledObject (scale-from-zero + downscale-survival proof) → Phase 50.
- →pdf backend (weasyprint/tectonic) → v2 (would break the TeX-free lean-image goal).
- Broad format allowlist / lua-filters → v2.

</deferred>

---

*Phase: 49-containerization-measured-timeout-gate*
*Context gathered: 2026-07-31*
