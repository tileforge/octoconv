---
phase: 68-four-straightforward-engine-classes
plan: 03
subsystem: security
tags: [landlock, sandbox, ocr, tesseract, pdfinfo, pdftotext, pdftoppm, engineExec, preflight, worker]

requires:
  - phase: 68-four-straightforward-engine-classes
    provides: "68-01: engineExec/forJob/run/sandboxFault, runCommandAs, ErrEngineSandboxUnavailable, the preflight toolkit -- this plan's shared mechanism"
  - phase: 68-four-straightforward-engine-classes
    provides: "68-02: ocr's re-measured conf05:ocr:readonly/engines blocks in SANDBOX-PATHS.md, scripts/conf05-crosscheck.sh -- this plan's proven set"
provides:
  - "OCRConverter.Sandbox, OCRConverter.exec/WithSandbox: all six OCR engine call sites (tesseract image + per-page PDF, pdftoppm, pdfinfo page-count + per-page-size, pdftotext) share ONE engineExec per job"
  - "ocrPageWorkDir: the per-page PDF render directory moves INSIDE the job directory, replacing a system-temp-dir MkdirTemp"
  - "OCRPreflight/ocrPreflightPDF: a content-judged preflight proving all three OCR routes (text-layer, render, image) plus a denial, using a single-page PDF with a non-embedded Helvetica font -- catches the font-substitution silent-empty-text trap plan 02 found"
  - "cmd/ocr-worker: refuses to start unconfined, preflights before db.Connect; deployment-ocr-worker.yaml gains ocrWorker.extraEnv"
affects: [68-04-av-sandbox, 68-05-audio-sandbox, 68-06]

actuals:
  tokens: 11700
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "One engineExec per job shared across several call-site FILES: ocr.go's image route, ocrpdf.go's PDF route (guard stage + per-page loop), and ocrguards.go's page-count/text-layer helpers all take an explicit `x engineExec` parameter built once by (c OCRConverter) exec(outPath) -- OCR is the first class in this phase with call sites split across multiple files sharing one ruleset, not just one converter method"
    - "ocrPageWorkDir(outPath): a per-job scratch directory created INSIDE filepath.Dir(outPath) instead of the shared system temp dir -- the only writable path a confined job's ruleset grants; inverted-and-restored to prove TestOCRRenderDirectoryIsInsideTheJobDirectory actually depends on it"
    - "ocrPreflightPDF: a hand-assembled, xref-correct PDF with a non-embedded font, generated in production code (not a test fixture) so OCRPreflight can prove the render route survives font substitution on every worker startup, not just once at measurement time"

key-files:
  created:
    - internal/convert/ocrsandbox.go
    - internal/convert/ocrsandbox_test.go
  modified:
    - internal/convert/ocr.go
    - internal/convert/ocrpdf.go
    - internal/convert/ocrguards.go
    - internal/convert/ocrguards_test.go
    - internal/convert/pre59_ocr_pinning_test.go
    - cmd/ocr-worker/main.go
    - deploy/chart/octoconv/templates/deployment-ocr-worker.yaml

key-decisions:
  - "ocrPDFPageCount/ocrPDFPageSize/extractPDFTextLayer/ocrPDFPageSizeForPage all take `x engineExec` as their second parameter (right after ctx) rather than the converter's Sandbox field directly -- keeps them free functions callable from tests with engineExec{} (unconfined, byte-identical to the old runCommand call), while ocrPDFGuardStage/ocrPDFPage/convertPDF (methods on OCRConverter) build the shared engineExec once via c.exec(outPath) and thread it down"
  - "The preflight's render/image routes are judged against ocrPreflightWord (the phrase's first word, \"OCTOCONV\"), not the full ocrPreflightText -- tesseract's OCR of a rendered image is not guaranteed to reproduce every character of a 26-character phrase, but its first, most legible word is; the text-layer route (pdftotext, no OCR involved) is judged against the full phrase since pdftotext returns the exact source bytes"
  - "cmd/ocr-worker's OMP_THREAD_LIMIT resolution/Setenv now runs BEFORE the preflight, not after: the preflight execs a real tesseract through this same process's environment, so it must see the same thread limit every real job does, not the host's raw core count"

patterns-established:
  - "Pattern: a class whose call sites span multiple files (not just one converter's methods) threads one job's engineExec down as an explicit parameter through every function in the chain -- av's five call sites across av.go/avduration.go (plan 04) plug into this shape directly"

requirements-completed: []  # CONF-01/CONF-02/CONF-04/CONF-05 are declared by every sibling plan in this phase (01, 04-06 remaining); requirements.ready-ids reports not-ready until every declaring plan has a SUMMARY (shared-ID gate, #2388) -- not an oversight

coverage:
  - id: D1
    description: "All six OCR engine call sites (tesseract image + per-page PDF, pdftoppm, pdfinfo page-count + per-page-size, pdftotext) run through the job's engineExec under the re-measured set, proven by content in both directions across all three routes (text-layer, render, image) plus a negative run"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/ocrsandbox_test.go#TestOCREveryEngineRunsThroughTheHelper"
        status: pass
      - kind: unit
        ref: "internal/convert/ocrsandbox_test.go#TestOCRRenderDirectoryIsInsideTheJobDirectory"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh ocr (confined-startup check: '🔒 ocr sandbox proven: the engine converted inside the job directory and was denied a file outside it')"
        status: pass
    human_judgment: false
  - id: D2
    description: "cmd/ocr-worker refuses to start when confinement is wanted and cannot be established, naming OCR_SANDBOX_DISABLED as the remedy; the escape hatch is reachable from the Helm chart and defaults to confined (absent from values.yaml)"
    requirement: CONF-02
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh ocr (refuses-without-landlock and escape-hatch checks)"
        status: pass
      - kind: integration
        ref: "helm template ... --set ocrWorker.extraEnv.OCR_SANDBOX_DISABLED=true renders the value; default render carries no env block for ocr-worker; scripts/ci-helm-checks.sh exits 0"
        status: pass
    human_judgment: false
  - id: D3
    description: "OCRPreflight judges all three OCR routes and the negative run by content (never exit status alone): the render route's fixture uses a non-embedded Helvetica font, so an empty hOCR output would mean the page rendered without glyphs -- the exact silent-failure shape plan 02 found live (pdftoppm+tesseract both exit 0). The live gate also proves the worker refuses to start when only landlock_restrict_self is denied -- the preflight, not ResolveHelper, catches that failure"
    requirement: CONF-04
    verification:
      - kind: unit
        ref: "internal/convert/ocrsandbox_test.go#TestOCRPreflightPDFIsBornDigitalWithANonEmbeddedFont"
        status: pass
      - kind: unit
        ref: "internal/convert/ocrsandbox_test.go#TestOCRPreflightUnconfinedProvesNothing"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh ocr (preflight-catches-restrict-self check)"
        status: pass
    human_judgment: false
  - id: D4
    description: "ocr's granted set (ocrReadOnly, seven paths; ocrEngines, four binaries) is pinned by a test that fails when the set drifts, and cross-checked against SANDBOX-PATHS.md's conf05:ocr blocks"
    requirement: CONF-05
    verification:
      - kind: unit
        ref: "internal/convert/ocrsandbox_test.go#TestOCRReadOnlySetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/ocrsandbox_test.go#TestOCREnginesAreTheMeasuredOnes"
        status: pass
      - kind: other
        ref: "scripts/conf05-crosscheck.sh ocr -> 'MATCH: ocr readonly (7) engines (4)'"
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-09-24
status: complete
plan_head_before: 8d582a83a721761f52d08dd321b283c10c18794f
commits: 3
---

# Phase 68 Plan 03: OCR Confined End-to-End Summary

**Every OCR engine call (tesseract, pdftoppm, pdfinfo, pdftotext, across six call sites in three files) now runs through one shared per-job Landlock ruleset, the per-page render directory moved inside the job directory, and a content-judged preflight proves all three OCR routes -- including the font-substitution trap that fails silently at exit 0 -- before the worker accepts a single job.**

## Performance

- **Duration:** 25 min
- **Started:** 2026-09-24T00:35:00Z (approx.)
- **Completed:** 2026-09-24T01:00:00Z
- **Tasks:** 3
- **Files modified:** 9 (2 created, 7 modified)

## Accomplishments

- `internal/convert/ocrsandbox.go` (new): `ocrReadOnly` (7 measured paths) and `ocrEngines` (`tesseract`, `pdftoppm`, `pdfinfo`, `pdftotext`) pinned against SANDBOX-PATHS.md's re-measured `conf05:ocr` blocks; `(OCRConverter).exec(outPath)` builds one shared `engineExec` per job; `ocrPageWorkDir(outPath)` creates the per-page PDF render directory inside `filepath.Dir(outPath)` instead of the shared system temp dir.
- `ocr.go`/`ocrpdf.go`/`ocrguards.go`: every direct `runCommand(ctx, "tesseract"|"pdftoppm"|"pdfinfo"|"pdftotext", ...)` call replaced with a call through the job's `engineExec` -- `ocrPDFPageCount`, `ocrPDFPageSize`, `extractPDFTextLayer`, `ocrPDFPageSizeForPage` take `x engineExec` as their second parameter; `ocrPDFGuardStage`/`ocrPDFPage`/`convertPDF` build and thread it.
- `OCRPreflight`/`ocrPreflightPDF` (`ocrsandbox.go`): a hand-assembled, xref-correct, single-page PDF with a non-embedded Helvetica font drives all three OCR routes (text-layer extraction, per-page render, bare image) plus a negative run outside the job directory, every judgment made by output CONTENT.
- `ocrsandbox_test.go` (new): pins the measured set, proves every one of OCR's four engines routes through the helper across three source/route combinations (scanned-pdf, born-digital-pdf, png) with `rw:<job dir>` as the only writable grant, and proves-by-inversion that the render directory lives inside the job directory.
- `cmd/ocr-worker/main.go`: `sandbox.RunExecModeIfRequested` is now the first statement of `main`; the sandbox is resolved, `OCR_MAX_PAGES`/`OCR_MAX_IMAGE_PIXELS`/`OMP_THREAD_LIMIT` are read, the sandbox-wired converter is registered (exactly one `Registry.Replace`), and `OCRPreflight` runs under a 60s context -- all before `db.Connect`.
- `deployment-ocr-worker.yaml` gains an `ocrWorker.extraEnv` block (mirrors `pandocWorker`'s) so `OCR_SANDBOX_DISABLED=true` is reachable from the chart; absent from `values.yaml`, so the default render carries no env block.
- `scripts/sandbox-liveproof.sh ocr` (built image): `ALL PASS (4 checks)`. `scripts/conf05-crosscheck.sh ocr`: `MATCH: ocr readonly (7) engines (4)`. `scripts/ci-helm-checks.sh`: 0 FAIL lines.

## Task Commits

Each task was committed atomically:

1. **Task 1: Route every OCR engine call through the job's engineExec and render pages inside the job directory** - `a4fae3b` (feat)
2. **Task 2: OCRPreflight over all three routes plus a denial, and tests that pin the set and the routing** - `81ed323` (test)
3. **Task 3: ocr-worker refuses to start unconfined, preflights before Postgres, chart extraEnv** - `2baa705` (feat)

**Plan metadata:** committed separately after this SUMMARY (docs).

## Files Created/Modified

- `internal/convert/ocrsandbox.go` - `ocrReadOnly`, `ocrEngines`, `(OCRConverter).exec`, `ocrPageWorkDir`, `(OCRConverter).WithSandbox`, `OCRPreflight`, `ocrPreflightPDF`, `ocrPreflightText`, `ocrPreflightWord` (new)
- `internal/convert/ocrsandbox_test.go` - pinned sets, every-engine-through-the-helper proof, render-directory-inside-job-dir proof (with recorded inversion), preflight fixture tests (new)
- `internal/convert/ocr.go` - `Sandbox *engineSandbox` field; image route's tesseract call routed through `c.exec(outPath).run`
- `internal/convert/ocrpdf.go` - `ocrPDFPageSizeForPage`/`ocrPDFGuardStage`/`ocrPDFPage`/`convertPDF` take/build `x engineExec`; per-page render directory now `ocrPageWorkDir(outPath)`
- `internal/convert/ocrguards.go` - `ocrPDFPageCount`/`ocrPDFPageSize`/`extractPDFTextLayer` take `x engineExec`, run via `x.run`
- `internal/convert/ocrguards_test.go`, `internal/convert/pre59_ocr_pinning_test.go` - call sites updated to pass `engineExec{}` (unconfined)
- `cmd/ocr-worker/main.go` - `ocrSandboxDisabledEnv`; startup block reordered: helper mode first, `ResolveHelper`, `OCR_MAX_PAGES`/`OCR_MAX_IMAGE_PIXELS`/`OMP_THREAD_LIMIT`, `Replace`, `OCRPreflight`, all before `db.Connect`
- `deploy/chart/octoconv/templates/deployment-ocr-worker.yaml` - `ocrWorker.extraEnv` block added

## Decisions Made

See `key-decisions` in frontmatter. Most consequential for plan 04 (av): a class whose engine call sites span multiple files (not one converter's methods alone) threads the job's `engineExec` down as an explicit parameter through every function in the chain, rather than reaching back into the converter's `Sandbox` field from each file -- keeps every free function in `ocrguards.go`/`ocrpdf.go` testable with a bare `engineExec{}` (unconfined), with zero behavioral change from the pre-Phase-68 `runCommand` calls.

## Deviations from Plan

None - plan executed exactly as written. The plan's own TDD instruction to verify the render-directory inversion (revert `ocrPageWorkDir` to the system temp dir, confirm `TestOCRRenderDirectoryIsInsideTheJobDirectory` fails, then restore) was carried out manually during Task 2 and is recorded above, not encoded as a permanent test-file artifact.

## Issues Encountered

One test-authoring correction, not a deviation from the plan: `TestOCRRenderDirectoryIsInsideTheJobDirectory`'s first draft asserted the render directory equals the job directory exactly; since `ocrPageWorkDir` creates a *subdirectory* of the job directory (by design -- a fresh scratch dir per PDF job, not the job directory itself), the assertion was corrected to "is the job directory or a path under it" before the inversion check was run. Caught immediately by the test's own first run, fixed before any commit.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `engineExec`/`ocrPageWorkDir`/`OCRPreflight` and the multi-file-threading pattern are ready for plan 04 (av: five call sites across `av.go`/`avduration.go`) and plan 05 (audio, including the cross-package ffprobe-guard decision 68-RESEARCH.md Pitfall 2 flags).
- CONF-01/02/04/05 stay "In Progress" in REQUIREMENTS.md until every plan in this phase that declares them (01, 04-06) has its own SUMMARY -- the shared-ID gate (#2388) working as intended.
- No blockers for 68-04 through 68-06.

---
*Phase: 68-four-straightforward-engine-classes*
*Completed: 2026-09-24*

## Self-Check: PASSED

All nine key files verified present on disk with `[ -f ]`. All three task commits
(`a4fae3b`, `81ed323`, `2baa705`) verified present via `git log --oneline --all`.
`go build ./...`, `go vet ./...`, `go test -count=1 ./internal/convert/
./internal/worker/ ./cmd/...` all exit 0. `scripts/conf05-crosscheck.sh ocr` prints
`MATCH: ocr readonly (7) engines (4)`. `scripts/sandbox-liveproof.sh ocr` (run
earlier this session against a freshly built image) ended `ALL PASS (4 checks)`.
Every plan-level `<verification>` command re-run and passing.
