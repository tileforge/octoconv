---
gsd_state_version: 1.0
milestone: v1.10
milestone_name: OCR Engine
status: executing
stopped_at: Completed 43-02-PLAN.md
last_updated: "2026-07-26T12:20:01.373Z"
last_activity: 2026-07-26
progress:
  total_phases: 4
  completed_phases: 0
  total_plans: 4
  completed_plans: 2
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-19 after v1.8 milestone start)

**Core value:** Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла (изображения, офисные документы, HTML, аудио, видео) и получить результат — без риска для стабильности или безопасности продакшена.
**Current focus:** v1.10 OCR Engine — Phase 43: OCR Engine Foundation

## Current Position

Phase: 43 (OCR Engine Foundation) — EXECUTING
Plan: 3 of 4
Status: Ready to execute
Last activity: 2026-07-26

## Performance Metrics

**Velocity:**

- Total plans completed: 107 (all v1.0–v1.8)
- Average duration: - min
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 4 | - | - |
| 02 | 3 | - | - |
| 03 | 3 | - | - |
| 04 | 5 | - | - |
| 05 | 1 | - | - |
| 06 | 4 | - | - |
| 07 | 2 | - | - |
| 08 | 2 | - | - |
| 09 | 2 | - | - |
| 10 | 4 | - | - |
| 11 | 4 | - | - |
| 12 | 1 | - | - |
| 13 | 3 | - | - |
| 14 | 3 | - | - |
| 15 | 5 | - | - |
| 16 | 5 | - | - |
| 17 | 2 | - | - |
| 18 | 4 | - | - |
| 19 | 2 | - | - |
| 20 | 2 | - | - |
| 21 | 3 | - | - |
| 22 | 2 | - | - |
| 23 | 3 | - | - |
| 26 | 2 | - | - |
| 27 | 3 | - | - |
| 28 | 3 | - | - |
| 29 | 3 | - | - |
| 30 | 3 | - | - |
| 31 | 4 | - | - |
| 32 | 5 | - | - |
| 33 | 3 | - | - |
| 34 | 3 | - | - |
| 35 | 7 | - | - |
| 36 | 5 | - | - |
| 37 | 3 | - | - |
| 38 | 1 | - | - |
| 39 | 4 | - | - |
| 40 | TBD | - | - |
| 41 | TBD | - | - |
| 42 | TBD | - | - |
| 43 | TBD | - | - |
| 44 | TBD | - | - |
| 45 | TBD | - | - |
| 46 | TBD | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
| Phase 36 P01 | 15min | 3 tasks | 6 files |
| Phase 36 P02 | 30min | 2 tasks | 2 files |
| Phase 36 P03 | 20min | 3 tasks | 3 files |
| Phase 36 P04 | 25min | 1 tasks | 4 files |
| Phase 36 P05 | 20min | 2 tasks | 9 files |
| Phase 37 P01 | 25min | 3 tasks | 4 files |
| Phase 37 P02 | 30min | 2 tasks | 3 files |
| Phase 37 P03 | 40min | 2 tasks | 6 files |
| Phase 38 P01 | 20min | 2 tasks | 1 files |
| Phase 39 P01 | 25min | 2 tasks | 6 files |
| Phase 39 P02 | 20min | 2 tasks | 3 files |
| Phase 39 P03 | 15min | 3 tasks | 8 files |
| Phase 40 P01 | 20min | 2 tasks | 5 files |
| Phase 40 P02 | 25min | 2 tasks | 6 files |
| Phase 41 P01 | 20min | 2 tasks | 3 files |
| Phase 41 P02 | 55min | 2 tasks | 4 files |
| Phase 41 P03 | 15min | 3 tasks | 9 files |
| Phase 42 P02 | 45min | 2 tasks | 6 files |
| Phase 42 P03 | 40min | 3 tasks | 4 files |
| Phase 43 P01 | 10min | 2 tasks | 2 files |
| Phase 43 P02 | 20min | 3 tasks | 5 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. v1.10-specific decisions surfaced by research, to be recorded as Key Decisions before/at implementation:

- **Reconciled preprocessing floor (binding for Phase 43):** ship fixed ~300 DPI rasterization (computed from the PDF page box, never a source PDF's declared/odd DPI) + Tesseract's own internal Leptonica-based binarization as the day-one preprocessing floor. Deskew/denoise (`unpaper`, an explicit `pixDeskew` pass) are NOT added blindly — evidence-gated only if measured accuracy on real representative scans shows a meaningful loss. Do not silently add or silently skip this follow-up.
- **Streaming page-by-page rasterize is architectural, not a later hardening patch (Phase 43):** `pdfinfo` reads the declared page count and rejects over `OCR_MAX_PAGES` BEFORE any `pdftoppm` invocation; the loop renders/OCRs/deletes exactly one page at a time. Never `pdftoppm in.pdf outbase` with no `-f`/`-l` bounds — that is the OCR-specific decompression-bomb class (distinct axis from `MAX_IMAGE_PIXELS`/AVE-05: page-count × per-page pixels).
- **Empty-but-valid OCR output is a successful `done` result, never a failure (Phase 43/44):** a blank/text-free page or image produces empty txt and must complete `done`, not `failed` — mirrors the whisper hallucination-on-silence precedent of documenting an accepted OCR-quality-spectrum gap rather than silently mishandling it. Do NOT add an `if output == "" { return error }` branch anywhere in the OCR path.
- **Born-digital PDF: fail-closed reject (option 1), not detect-and-skip (option 2) in v1 (Phase 43):** a PDF with an existing extractable text layer gets a 422, not silent re-OCR or a per-page skip-text branch. Detect-and-passthrough is explicitly OCR-V2-01, deferred behind its own opts/output-format design.
- **hOCR/ALTO assembly must use a real XML/HTML encoder, never string concatenation (Phase 43):** OCR'd text is untrusted/unpredictable content; a round-trip test (text containing `&`/`<`/`>` producing valid parseable XML) is required before this phase can be considered done.
- **OCR is wired as a 6th hardcoded engine class exactly like image/document/html/audio/av — SEED-010's dynamic registry stays dormant (all phases):** compile-time `Registry.Register` in `converters.go`, hardcoded `EngineOCR` constant, one CHECK-constraint migration (`0009_ocr_engine.sql`). Do not use this milestone as an excuse to generalize `converters.go`'s `init()`.
- **`progress.go`'s shared types (`ProgressUpdate`/`ProgressSink`/`ProgressConverter`) get zero edits (Phase 43/44):** OCR's page-granular progress is driven by its own small `ocrprogress.go` page-EMA estimator calling the same `sink.Update(...)` contract every other engine uses — never a branch inside the ffmpeg-specific `progressBlockParser`/`buildProgressTick`.
- **Both new untrusted processes (rasterizer + tesseract) go through the existing hardened `runCommand` from line one (Phase 44):** tesseract/leptonica have a documented CVE history (maintainer issue "Leptonica has no regard for security") — never a bare `exec.Command` "just a CLI wrapper" shortcut, even temporarily.
- **`OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` must be measured by a dedicated `scripts/ocr-timing-measure.sh` (seconds-per-page, not an RTF ratio) — never copied from `AUDIO_ENGINE_TIMEOUT`/`AV_ENGINE_TIMEOUT` (Phase 45):** the project's own Key Decisions precedent calls this exact shortcut out as a recurring risk for new engine classes; a copied placeholder must never reach shipped config.
- **Language-pack scope stays a short SHA-pinned `tessdata_fast` allowlist matching `OCROpts`, never the full upstream tessdata/tessdata_best set (Phase 45):** bloat directly fights KEDA scale-from-zero cold-start economics (mirrors the audio-worker's baked-model image-size discipline).
- **Zero new Go dependencies for v1.10:** Tesseract/poppler-utils are CLI subprocesses through the existing hardened `runCommand` harness — no cgo, no Python runtime, no new `go.mod` entries.
- **Open gaps to resolve during Phase 43 planning/implementation, not left silently unresolved:** (1) the born-digital-PDF detection mechanism (which library call / threshold counts as "meaningful existing text layer" — the fail-closed-reject *decision* is settled, the *implementation* is not); (2) webp→PNG normalization need (Leptonica's webp support is inconsistent across Debian's bundled build per STACK.md); (3) exact Tesseract PSM/OEM/hOCR-ALTO config-file names — verify against the chosen tool's live 5.5.3 CLI output, not training-data assumption.
- [Phase 43]: ocrBornDigitalCharThreshold=16 non-whitespace chars (D-03 conservative reject-on-any-substantial-text) — verified 0 chars on image-only fixture / 82 on born-digital fixture
- [Phase 43]: OCR guard caps use zero-value-means-uncapped-this-phase convention — mirrors av.go's plain-param-this-phase pattern; OCR_MAX_PAGES/OCR_MAX_PIXELS env wiring deferred to Phase 45

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260712-cqg | Fix Phase 16 verification gaps CR-01/WR-01: advisory-lock connection lifecycle (Release pool slot on TryAcquire error; add PGAdvisoryLock.Close and call at webhook-worker shutdown) | 2026-07-12 | 1f8b22b | [260712-cqg-fix-phase-16-verification-gaps-cr-01-wr-](./quick/260712-cqg-fix-phase-16-verification-gaps-cr-01-wr-/) |
| 260719-2f5 | OpenAPI 3.1 спека API (docs/openapi.yaml, 14 операций, ApiKey auth, multipart POST /v1/jobs с аудио-примерами) для импорта в Yaak + docs/README.md; redocly lint 0 errors | 2026-07-19 | 43799af | [260719-2f5-openapi-3-1-api-octoconv-docs-yaak](./quick/260719-2f5-openapi-3-1-api-octoconv-docs-yaak/) |
| 260724-26g | Актуализация README под shipped v1.0–v1.8 (5 движков image/document/html/audio/video, presets + /v1/formats, PDF/A+veraPDF, CFB, k8s/Helm/KEDA, MCP; cmd/ дерево 12 бинарников, config-таблица, Roadmap, Contributing) | 2026-07-23 | 50bade0 | [260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8](./quick/260724-26g-actualize-readme-md-to-reflect-v1-2-v1-8/) |

### Pending Todos

- Plan Phase 43 (OCR Engine Foundation): standalone `OCRConverter` (Tesseract eval-gate, page-count guard + streaming rasterize, `OCROpts` closed language allowlist, hOCR/ALTO XML encoder). Highest-research-value phase per PITFALLS.md — every architectural decision (memory-bomb guard, born-digital detect, PSM/OEM pin, XML escaping) must be baked in from line one, not retrofitted.

### Blockers/Concerns

None currently blocking the overall roadmap. Sequencing carried into the roadmap:

- Hard-ordered spine: 43 (foundation, standalone/unregistered — nothing to register without a working converter) → 44 (async wiring — can't measure a real timeout without the full pipeline producing real outcomes to time) → 45 (containerization + measured timeout gate — KEDA's shutdown timing derives from the measured timeout) → 46 (KEDA/Helm, depends on Phase 45's measured numbers). Identical 4-phase shape to v1.7 (audio, 30-33) and v1.8 (av, 34-37) — proven twice already.
- Phase 43 is the highest-risk phase: PITFALLS.md flags 5 of 9 critical pitfalls (memory bomb, silent accuracy collapse, wrong PSM/OEM, hOCR/ALTO XML escaping, born-digital PDF mishandling) as "must be architectural from line one" — all must land in this phase's converter core loop, not be spread across later phases.
- The engine-choice eval-gate (Tesseract vs PaddleOCR) is NOT yet run — Tesseract is the converged strong default across all research tracks, but per project process it must be validated against real scanned-document fixtures during Phase 43 before being treated as final.
- Born-digital PDF detection MECHANISM is an open gap (the fail-closed-reject DECISION is settled) — resolve the concrete implementation (pdfinfo/pdffonts-style check, threshold) during Phase 43 planning, not left implicit.
- Phase 45's seconds-per-page measurement methodology is a new metric shape (not a straight RTF-ratio clone from audio/av) — the corpus composition (page counts, languages, quality tiers) needs its own definition during Phase 45 planning.
- Zero new Go dependencies for this milestone — Tesseract/poppler-utils are CLI subprocesses through the existing hardened `runCommand` harness, same shape as every prior engine.
- Operational discipline (OrbStack): pre-build all images sequentially with non-`latest` tags; never run compose and k8s stacks hot simultaneously (four confirmed daemon wedges on record, per v1.6/v1.7/v1.8 history).

## Deferred Items

Acknowledged at v1.8 close (2026-07-23):

| Category | Item | Status |
|----------|------|--------|
| seed | SEED-001: Lesson-recording analysis (tutors/language schools) | RESOLVED 2026-07-23 — the actual need (audio/video → transcript) shipped in v1.7 + v1.8; the vertical analysis-product (top-mistakes + spaced-repetition deck) was deliberately NOT pursued. Do not resurface as a backlog candidate. |
| seed | SEED-004: Local k8s + KEDA full-stack validation | Deferred — superseded in practice by per-phase live gates (keda-gate 21/21, load-proofs Phases 28/33); revisit as CI k8s validation (K8SV2-01) |
| tech_debt | WR-05 keda-load-proof.sh BUSY_POD jsonpath defect (kubectl v1.36.2, `deletionTimestamp==""` vs absent key) | Accepted residual (29-REVIEW), empirically confirmed live in Phase 33; forward-fix of the frozen script recommended in a future phase |
| tech_debt | Registry cold-pull time for 682MB audio image | Unmeasurable on OrbStack shared store (Pulling→Pulled ≈0 recorded); measure in a real-registry environment before production KEDA tuning is trusted; same open question now applies to the av-worker image (AVX-02, v2 requirement) |
| tooling_noise | audit-open flags 29-HUMAN-UAT.md (status resolved) and quick task 260712-cqg (complete, SUMMARY present) | Both artifacts closed; audit parser counts them regardless |
| v2_scope | AVX-01: Trim/crop as validated closed-opts (start/end timecodes) | Deferred to v2 — only on confirmed demand | v1.8 requirements definition (2026-07-19) |
| v2_scope | AVX-02: Registry cold-pull measurement for heavy images (av-worker) | Deferred to v2 — carries forward the same unmeasured-on-OrbStack gap as the audio image | v1.8 requirements definition (2026-07-19) |
| v2_scope | PROG-V2-01: Stage-based sub-progress in the reference frontend UI | Deferred to v2 | v1.9 requirements definition (2026-07-23) |
| v2_scope | PROG-V2-02: Coarse elapsed-vs-timeout progress heuristic for non-ffmpeg engines | Rejected for v1.9 — `null` is the honest answer; revisit only on confirmed demand | v1.9 requirements definition (2026-07-23) |
| v2_scope | CNCL-V2-01: Batch cancel (cancel many jobs in one call) | Deferred to v2 | v1.9 requirements definition (2026-07-23) |
| v2_scope | PUSH-V2-01: `event`-discriminated payloads for ALL webhook types | Deferred to v2 — broader webhook contract-versioning conversation, out of scope for the single new `progress` event | v1.9 requirements definition (2026-07-23) |

Items acknowledged and carried forward at milestone closes (see `.planning/milestones/*-MILESTONE-AUDIT.md` for full detail):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| tech_debt | docker-compose.yml audit for other stale gaps vs .env.example | Closed as DEBT-05, Phase 12 | v1.0 close (2026-07-08) |
| tech_debt | WR-02: docker-compose.e2e.yml lacks `extra_hosts` on `api` — E2E webhook pair fails on plain-Linux docker | Closed as DEBT-01, Phase 12 | v1.2 close (2026-07-10) |
| tech_debt | WR-03: engine-class string literals duplicated in 4 places — extract exported constants | Closed as DEBT-02, Phase 12 | v1.2 close (2026-07-10) |
| tech_debt | WR-04: E2E HTTP clients lack per-request timeouts | Closed as DEBT-03, Phase 12 | v1.2 close (2026-07-10) |
| tech_debt | gofmt nit in internal/queue/queue_test.go (pre-existing since Phase 9/10) | Closed as DEBT-04, Phase 12 | v1.2 close (2026-07-10) |
| v2_scope | Full ISO 19005 (veraPDF) validation of PDF/A outputs | Closed as PDFA-01/02, Phase 23 | v1.3 requirements definition (2026-07-10) |
| v2_scope | Legacy vs encrypted CFB distinction (directory-stream parsing) | Closed as CFB-01/02, Phase 22 | v1.3 requirements definition (2026-07-10) |
| v2_scope | Custom fonts / extended CJK-RTL coverage for HTML→PDF | Deferred to v2 (DOCV3-03, carried) | v1.3 requirements definition (2026-07-10) |
| accepted_risk | Active anti-DoS by document complexity (sheets/cells/unzipped size) | Accepted residual risk (DOC-V2-05, carried) | v1.2 requirements definition (2026-07-09) |
| accepted_risk | `file://` passive subresource read inside chromium-worker (shared UID nobody) | Accepted residual risk (v1.3 Phase 15) | v1.3 close (2026-07-12) |
| accepted_risk | Hallucination on silence/music (whisper exits 0 with structurally-valid garbage) | Accepted residual risk (Phase 30, carried); applies equally to video→transcript pairs added in v1.8 | v1.7 close (2026-07-18) |
| seed | SEED-001: Lesson-recording analysis for tutors and language schools | Foundation in v1.7 (transcription + JSON timestamp contract); mistake-analysis/deck remains a future milestone, not v1.8 | v1.2 close (2026-07-10) |
| seed | SEED-003: MCP-сервер для OctoConv | ✓ Implemented (v1.5 Phase 21, MCP-01..05) | v1.4 planning (2026-07-12) |
| seed | SEED-004: OctoConv on Kubernetes + KEDA autoscaling | ✓ Delivered (v1.6 Phases 24-28) | v1.6 requirements definition (2026-07-14) |
| infra | k8s-валидация в CI (kind/k3d) | Deferred to v2 (K8SV2-01) | v1.6 requirements definition (2026-07-14) |
| infra | `is_operator` column vs env-allowlist for operators | Deferred to v2 (K8SV2-03) | v1.6 requirements definition (2026-07-14) |
| tech_debt | CACHED-hit log confirmation for CI docker-build (needs gh auth) | Operator-accepted residual | v1.4 close (2026-07-13) |
| ops | Branch-protection required-checks (gate/race/docker-build) — manual GitHub UI step | Open operational follow-up | v1.4 close (2026-07-13) |
| tech_debt | presets D-04 single-active-version: application-transactional only, no DB backstop | Accepted residual | v1.4 close (2026-07-13) |
| seed | SEED-002: Decouple webhook delivery from any specific engine worker binary | ✓ Implemented (v1.3 Phase 16, WEBH-01) | v1.2 close (2026-07-10) |

## Session Continuity

Last session: 2026-07-26T12:20:01.362Z
Stopped at: Completed 43-02-PLAN.md
Resume file: None

## Operator Next Steps

- Review the v1.10 roadmap draft (.planning/ROADMAP.md, Phase Details section) and approve or request revisions.
- Once approved, run `/gsd:plan-phase 43` to plan the OCR Engine Foundation phase.
