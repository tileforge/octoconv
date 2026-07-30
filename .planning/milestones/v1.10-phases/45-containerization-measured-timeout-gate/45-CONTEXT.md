# Phase 45: Containerization + Measured Timeout Gate - Context

**Gathered:** 2026-07-26
**Status:** Ready for planning

<domain>
## Phase Boundary

A running `ocr-worker` container in docker-compose passes a full live E2E (upload image + multi-page scanned PDF → poll → presigned txt/hocr download + signed webhook), with `OCR_ENGINE_TIMEOUT` / `OCR_MAX_PAGES` sized from a MEASURED seconds-per-page gate (`scripts/ocr-timing-measure.sh`), never a copied/guessed constant. Mirrors the audio Phase 32 / av Phase 36 containerization + RTF-gate. Covers OCRE-04. Also CLOSES the deferred Phase-44 SC4 live progress-ramp E2E (the compose stack now has a real ocr-worker to run it against).

**In scope:** `Dockerfile.ocr-worker` (source-built Tesseract + poppler + SHA-pinned tessdata_fast eng/rus); the compose `ocr-worker` service; `scripts/ocr-timing-measure.sh` (supervised sec/page gate) → derived `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES`; IN-02 env propagation to every queue-client service; CI bake-matrix entry; the recorded Key Decision (numbers + measurement conditions); the live E2E.
**Out of scope:** KEDA/Helm ScaledObject (Phase 46); any converter/worker-logic change (43/44 done); OSD/deskew/passthrough/searchable-PDF (v2).
</domain>

<decisions>
## Implementation Decisions

### Tesseract build (SC1)
- **D-01:** **Source-built Tesseract 5.5.x, tag/commit-pinned** (+ its Leptonica dependency), in a throwaway build stage the runtime stage COPYs — mirroring `Dockerfile.av-worker`'s ffmpeg-from-source and `Dockerfile.audio-worker`'s whisper.cpp discipline (pin by tag/commit, not a mutable apt version). Reproducibility + the newer 5.5 engine over Debian bookworm's apt 5.3.0, consistent with the project's build-from-pinned-source pattern for every non-trivial engine binary. Rejected: apt tesseract 5.3.0 (simpler/faster build but older + version floats with the Debian point-release; the project's other engines are all source-pinned for exactly this reason). poppler-utils (pdftoppm/pdfinfo/pdftotext) come from apt (a stable, widely-packaged dep — same posture as av's apt-installed codec libs).

### Measurement corpus + safety (OCRE-04, SC2/SC3)
- **D-02:** `scripts/ocr-timing-measure.sh` measures **p95 seconds-per-page** (NOT an RTF ratio — OCR cost is per-page, not per-input-duration) across a **3-document representative corpus: a clean single-page scan, a dense multi-column page, and a noisy/skewed scan**, in BOTH `eng` and `rus`, rasterized at the fixed ~300 DPI, under production-equivalent CPU/memory limits (the compose ocr-worker's `cpus`/`memory`). The go/no-go derivation: `OCR_ENGINE_TIMEOUT = ceil(OCR_MAX_PAGES × p95_sec_per_page × 2.0)` (safety ×2.0, mirroring av's ×2.0), which MUST stay under the global reconciler CAP (900s/15m, `RECONCILER_ACTIVE_STALE_AFTER`). If the derived timeout would breach the CAP, `OCR_MAX_PAGES` is lowered until it fits (the Path-B lever, av Phase-36 precedent) — NEVER copy a neighbor's timeout. The measurement is SUPERVISED (orchestrator/operator-run, like av-rtf-measure.sh); no timeout value is committed before a real run prints the p95.

### OCR_MAX_PAGES + concurrency (SC2/SC4)
- **D-03:** `OCR_MAX_PAGES ≈ 200` as the starting ceiling, FINALIZED from the measurement so `OCR_MAX_PAGES × p95 × 2.0 < CAP` (e.g. at ~2 s/page: 200 × 2 × 2.0 = 800s < 900s). `OCR_WORKER_CONCURRENCY = 1` (mirrors av/audio: one job at a time avoids CPU oversubscription/CFS-thrashing on the `cpus:2` worker; a per-job OCR already streams pages sequentially). Tesseract's own thread use is pinned to the cgroup CPU quota via `OMP_THREAD_LIMIT` (mirroring av's `avThreadCount()` → `-threads` cgroup discipline), so it honors the container limit rather than the host core count. A concurrency > 1 is a measured-evidence future tweak, not v1.

### Claude's Discretion
- The exact Tesseract/Leptonica pinned versions/commits; the corpus fixture sourcing (synthesize scans via the existing image tooling vs commit small real fixtures under testdata); the `OMP_THREAD_LIMIT`/cgroup detection detail (reuse `CgroupCPULimit`); the compose `ocr-worker` resource limits (mirror av's cpus:2/1g unless measurement says otherwise); the `stop_grace_period` (OCR_ENGINE_TIMEOUT + margin, like av's 773s); the env var names — all executor/planner's call, provided: the timeout is DERIVED from the measured p95 (never copied), stays under the CAP, is recorded as a Key Decision with its measurement conditions, and IN-02 propagation is identical across all queue-client services.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The av/audio containerization + RTF-gate to CLONE
- `Dockerfile.av-worker` — the source-build-stage + runtime-slim shape (ffmpeg pinned by commit, runtime apt-installs shared libs, `USER nobody`); `Dockerfile.audio-worker` (whisper.cpp commit-pinned source build + SHA-pinned model bake — the tessdata_fast SHA-pin precedent). Clone as `Dockerfile.ocr-worker` (tesseract+leptonica source, poppler apt, tessdata_fast SHA-pinned eng/rus).
- `scripts/av-rtf-measure.sh` (Phase 36 AVE-04) — THE supervised measurement-gate to clone as `scripts/ocr-timing-measure.sh`: per-cell p95 print, go/no-go, the compose-equivalent resource limits, no-value-before-real-run discipline. `scripts/audio-rtf-measure.sh` also.
- `docker-compose.yml` av-worker service (`AV_ENGINE_TIMEOUT=753s`, `AV_WORKER_CONCURRENCY=1`, `stop_grace_period`, resource limits, `RECONCILER_ACTIVE_STALE_AFTER=15m` CAP, IN-02 propagation of AV_* across all queue-client services) — the ocr-worker service template.

### Engine timeout wiring (OCRE-04)
- `internal/convert/av.go` (`avMaxSourceDuration`/`SetAVDiskSafetyFactor`/`effectiveAVTimeout`-style resolver + the plain-param→env-resolved pattern) — how OCR's `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` env values resolve into the converter's guard params (the page-cap `enforceMaxPages` from Phase 43 gets its value here).
- `cmd/ocr-worker/main.go` (Phase 44) — reads the OCR env this phase adds (OCR_ENGINE_TIMEOUT/OCR_MAX_PAGES/OCR_WORKER_CONCURRENCY/OMP_THREAD_LIMIT); `internal/convert/cgroup.go` (`CgroupCPULimit`) for the thread-limit.
- `internal/queue/queue.go` `OCRUniqueTTL` derivation (Phase 44) — now fed by the measured OCR_ENGINE_TIMEOUT; IN-02: every service constructing a queue.Client must set OCR_* identically.

### Requirements + research + prior
- `.planning/REQUIREMENTS.md` — OCRE-04.
- `.planning/research/STACK.md` (tesseract source-vs-apt note, tessdata_fast SHA-pin) + `PITFALLS.md` (Pitfall 4 language-pack bloat, Pitfall 7 RTF-must-be-measured, Pitfall 8 security).
- `.planning/phases/44-async-pipeline-wiring/44-04-SUMMARY.md` (the deferred live SC4 E2E this phase closes) + `43-CONTEXT.md`/`44-CONTEXT.md`.
- `.github/workflows/*` CI bake matrix (add ocr-worker).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `Dockerfile.av-worker` / `Dockerfile.audio-worker` — near-verbatim clone targets (source-build stage + SHA-pinned data bake + slim runtime + USER nobody).
- `scripts/av-rtf-measure.sh` — the supervised go/no-go gate to clone (p95, resource-limited, no-value-before-run).
- `CgroupCPULimit` (cgroup.go) — cgroup-quota detection for OMP_THREAD_LIMIT (mirrors av's -threads).
- The reconciler CAP (900s/15m) + the Path-B lever (lower MAX_PAGES until timeout fits) — the av Phase-36 derivation pattern.
- The compose IN-02 propagation pattern (AV_* across all queue-client services) — apply to OCR_*.

### Established Patterns
- Build engine binary from pinned source in a throwaway stage; SHA-pin baked data (whisper model → tessdata_fast); slim runtime, USER nobody.
- Measure-don't-assume timeout: derive from p95 × safety, under CAP, recorded as a Key Decision, Path-B lever if over CAP.
- Per-worker resource limits (cpus/memory) matching the measurement container exactly.

### Integration Points
- `Dockerfile.ocr-worker` + compose `ocr-worker` service + `scripts/ocr-timing-measure.sh` + `cmd/ocr-worker` env reads + `internal/convert` OCR guard params (env-resolved) + CI bake matrix. The measurement + live E2E run against the live docker-compose stack (operator/self-run, like av).
</code_context>

<specifics>
## Specific Ideas

- Supervised measurement flow (like av): build `octoconv-ocr-worker:timing-measure`, run `ocr-timing-measure.sh` over the 3-doc × 2-lang corpus at the compose resource limits, read the per-cell p95 sec/page, derive OCR_ENGINE_TIMEOUT = ceil(MAX_PAGES × p95 × 2.0), confirm < CAP (else lower MAX_PAGES), record the Key Decision with arch/cpu/mem/N/p95. I (orchestrator) run this myself and bring the numbers.
- SC4 close: the live E2E here also proves the progress ramp over GET /v1/jobs/{id} (and/or SSE) end-to-end against the real ocr-worker container — the piece Phase 44 proved only at the converter/handler integration level.
- tessdata_fast: bake ONLY eng + rus `.traineddata` (SHA-pinned), matching the closed OCROpts allowlist — never the full upstream set (Pitfall 4 bloat / KEDA cold-start economics).
- Tesseract-from-source build is slow; ensure the CI bake + local build are cached (leptonica + tesseract stages).
</specifics>

<deferred>
## Deferred Ideas

- KEDA/Helm ScaledObject (scale-from-zero + downscale-survival proof) → Phase 46.
- Concurrency > 1 → measured-evidence future tweak.
- tessdata_best (higher accuracy, larger) → evidence-gated if measured accuracy demands.
- Deskew/OSD/passthrough/searchable-PDF → v2.
</deferred>

---

*Phase: 45-containerization-measured-timeout-gate*
*Context gathered: 2026-07-26*
