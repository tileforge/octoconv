---
phase: 47-pandoc-engine-foundation
plan: 03
subsystem: convert
tags: [pandoc, go, security, sandbox, markup-conversion, standalone-converter]

# Dependency graph
requires:
  - phase: 47-pandoc-engine-foundation (47-01)
    provides: PandocOpts closed typed allowlist (pandocopts.go), pandocArgs discrete-argv builder, EnginePandoc constant
  - phase: 47-pandoc-engine-foundation (47-02)
    provides: markupsniff.go (LooksLikeLaTeX/LooksLikeText/enforcePandocInputSize) and epub SniffContainer support (docsniff.go)
provides:
  - PandocConverter -- a standalone, UNREGISTERED Converter implementation for the markup family (md/rst/org/latex/html/docx/epub)
  - A curated closed Pairs() allowlist proven against the real pandoc 3.10.1 binary
  - A hardened, unit-tested pandocConvertArgs argv builder (per-source raw-passthrough table + --sandbox + --standalone-for-structural)
  - A live-verified SC3 security canary proving shell-escape/\input/network-fetch cannot fire through this converter
affects: [48-pandoc-queue-worker-api, 49-pandoc-container-timeout]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Standalone-converter-first (unregistered) validated directly against the real external binary before any queue/worker/API plumbing -- mirrors Audio/AV/OCR phase scope fences"
    - "Per-source reader-extension table (map/switch keyed on normalized source format) instead of a blind extension-suffix concat, when a CLI tool's flag grammar varies per input format"
    - "Format-name translation layer (pandocFormatName) at the argv boundary, decoupling this project's own normalized format tokens from an external tool's own format identifiers"
    - "Empty-but-valid output completes as done -- sniffers that are themselves fail-closed on size==0 must be skipped, not called, for a genuinely empty result"

key-files:
  created:
    - internal/convert/pandoc.go
    - internal/convert/pandoc_test.go
  modified: []

key-decisions:
  - "Curated closed Pairs() table (map[string][]string) across md/rst/org/latex/html/docx/epub, excluding identity pairs and epub->latex/org, finalized directly against pandoc 3.10.1's real reader/writer capabilities"
  - "Raw HTML/TeX passthrough disabled via a PER-SOURCE reader-extension table (md/html/epub -> -raw_html-raw_tex; latex -> -raw_tex only; rst/org/docx -> bare -f) -- a blanket suffix errors for rst/org/docx and over-claims for latex"
  - "--sandbox (pandoc 2.19+ IO-restricting flag) used for hardening, NOT --no-shell-escape (a TeX-engine flag pandoc's CLI rejects outright)"
  - "pandocFormatName translates this project's 'md' alias to pandoc's own 'markdown' format identifier at the argv boundary -- pandoc has no format literally named 'md'"
  - "Output validated with the same sniffer family that guards uploads (LooksLikeHTML/LooksLikeLaTeX/SniffContainer/LooksLikeText); a zero-size output skips the sniffer entirely and returns done, since every sniffer is itself fail-closed on empty input"
  - "PandocConverter remains UNREGISTERED (converters.go init() untouched) -- registration is Phase 48's job"

patterns-established:
  - "Argv builders isolated as pure functions (pandocConvertArgs, pandocReaderSpec, pandocFormatName) for unit-testable flag pinning without ever invoking the real subprocess"
  - "SC3-style security canary: assert a concrete OBSERVABLE NON-EFFECT (absent sentinel file, absent leaked secret substring, absent resolved remote content) against the real external binary, never merely 'no error'"

requirements-completed: [PDOC-01, PDOC-02, PDCE-02]

# Metrics
duration: 25min
completed: 2026-07-30
---

# Phase 47 Plan 03: PandocConverter Core Summary

**Standalone `PandocConverter` (unregistered) converting across a curated closed markup allowlist via hardened `pandoc` 3.10.1 invocations (`--sandbox` + per-source raw-extension table, no network), with output validated by the Phase-47 sniffers and a live SC3 security canary proving shell-escape/`\input`/network-fetch cannot fire.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-07-30
- **Tasks:** 3/3
- **Files modified:** 2 (both new: `pandoc.go`, `pandoc_test.go`)

## Accomplishments
- `PandocConverter` with a closed curated `Pairs()` allowlist (md/rst/org/latex/html/docx/epub, excluding identity pairs and epub->latex/org) and a fast-reject helper (`pandocPairSupported`) that Convert calls BEFORE any subprocess
- A hardened, pure `pandocConvertArgs` argv builder: per-source reader-extension table (`pandocReaderSpec`) disabling raw HTML/TeX passthrough correctly for every source class, `--sandbox`, `--standalone` for structural targets (deduped against opts), never a `+raw_*`-enabling or network-enabling token
- `Convert()` running the real pandoc via the existing hardened `runCommand` (byte-for-byte unchanged), with an input-size fail-closed guard and target-appropriate output content validation (`LooksLikeHTML`/`LooksLikeLaTeX`/`SniffContainer`/`LooksLikeText`) -- a zero-size output completes as `done`, never `failed`
- A real-pandoc matrix test proving exactly the pairs a naive blanket raw-suffix approach breaks: `rst->html`, `docx->md`, `latex->md`, plus `md->html`, `md->docx`, `html->md`, `md->epub`
- A three-part SC3 security canary (shell-escape/`\input`/network-fetch), all green against the real pandoc 3.10.1 binary, each asserting a concrete observable non-effect rather than merely "no error"

## Task Commits

Each task was committed atomically:

1. **Task 1: PandocConverter Pairs() closed allowlist + hardened argv builder** - `8b8c504` (feat)
2. **Task 2: Convert() end-to-end via runCommand + input-size guard + output validation** - `dd20b1a` (feat)
3. **Task 3: SC3 security canary -- shell-escape / \input / network-fetch** - `a4d6ea4` (test)

## Files Created/Modified
- `internal/convert/pandoc.go` - `PandocConverter` (Pairs/Engine/Convert), `pandocPairTable`/`pandocStructuralTargets` closed tables, `pandocReaderSpec`/`pandocFormatName`/`pandocConvertArgs` hardened pure argv builders, `pandocValidateOutput` (output-sniffer dispatch + empty-is-done)
- `internal/convert/pandoc_test.go` - Pairs/closed-set tests, argv-pinning tests (per-source suffix + sandbox + standalone), unsupported-pair fast-reject test, real-pandoc matrix test, oversize/empty-source tests, SC3 security canary (3 sub-cases)

## Decisions Made
- Curated `Pairs()` implemented as a `map[string][]string` table (not a generated cartesian), matching the plan's discretion-finalized directional set
- `pandocFormatName` added as a small translation layer (Rule 1 fix, see Deviations) rather than renaming the project's own `NormalizeFormat` alias table, keeping the "md" alias scoped to this project's own domain and the pandoc-specific translation isolated to the argv boundary
- Standalone-flag deduplication (`pandocArgsContains`) chosen over modifying `pandocArgs` (Plan 01, reused verbatim) so a structural target's forced `--standalone` never collides with an opts-requested one

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] pandoc has no format literally named "md" -- md-source/md-target conversions failed against the real binary**
- **Found during:** Task 2 (real-pandoc matrix test)
- **Issue:** `pandocConvertArgs` passed this project's own normalized format token ("md") straight through to `-f`/`-t`. Live-verified against pandoc 3.10.1's `--list-input-formats`/`--list-output-formats`: pandoc has no format literally named `md`, only `markdown` (and other markdown flavors this project doesn't expose). Every md-source and md-target conversion failed with `Unknown input format 'md'` / `Unknown output format 'md'` -- `md->html`, `md->docx`, `md->epub`, `latex->md`, `html->md` all failed in the first matrix run.
- **Fix:** Added `pandocFormatName(f string) string` translating "md" -> "markdown" (all other curated formats pass through unchanged, already valid pandoc identifiers), applied at both the `-f` reader-spec construction (`pandocReaderSpec`) and the `-t` writer-format element in `pandocConvertArgs`.
- **Files modified:** `internal/convert/pandoc.go`, `internal/convert/pandoc_test.go` (updated the Task 1 argv-pinning expectation for the md case + added a dedicated regression test, `TestPandocFormatNameTranslatesMdToMarkdown`)
- **Verification:** Full real-pandoc matrix (`md->html`, `md->docx`, `rst->html`, `latex->md`, `docx->md`, `html->md`, `md->epub`) green after the fix; `go test ./internal/convert/...` fully green.
- **Committed in:** `dd20b1a` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug fix, Rule 1)
**Impact on plan:** Necessary correctness fix caught by the plan's own mandated real-pandoc matrix test -- exactly the kind of discovery that testing against the real binary (rather than trusting an assumed 1:1 format-name mapping) was designed to surface. No scope creep; no hardening flag was weakened to make anything pass.

## Issues Encountered
None beyond the Rule 1 fix above.

## User Setup Required
None - no external service configuration required. pandoc 3.10.1 was already present on `PATH` locally (Homebrew); all real-binary tests skip hermetically via `exec.LookPath` when pandoc is absent (e.g. CI without the binary installed).

## Next Phase Readiness
- `PandocConverter` is fully built and unit/integration-tested against the real pandoc 3.10.1 binary, but remains deliberately UNREGISTERED: `converters.go` `init()`, `internal/api/*`, `internal/queue/*`, `internal/worker/*`, `internal/convert/exec.go`, and `go.mod`/`go.sum` are all untouched (verified via `git diff --stat` across all three task commits).
- Phase 48 can register `PandocConverter{}` in `converters.go` and wire API/queue/worker dispatch directly against the `Convert(ctx, inPath, outPath, opts)` contract proven here.
- FOLLOW-UP for Phase 49 (carried from `47-CONTEXT.md`/47-03-PLAN.md): re-verify `--sandbox` + docx round-trip on the PINNED static-release pandoc binary (D-03) once it is baked into `Dockerfile.pandoc-worker` -- `--sandbox` can block data files a reader/writer needs unless the binary embeds them (`man pandoc`'s own `embed_data_files` note); local Homebrew 3.10.1 works today, but the pinned binary is a DIFFERENT build and must be re-canaried before Phase 48/49 registration is trusted in production.

---
*Phase: 47-pandoc-engine-foundation*
*Completed: 2026-07-30*

## Self-Check: PASSED

- FOUND: internal/convert/pandoc.go
- FOUND: internal/convert/pandoc_test.go
- FOUND: .planning/phases/47-pandoc-engine-foundation/47-03-SUMMARY.md
- FOUND commit: 8b8c504
- FOUND commit: dd20b1a
- FOUND commit: a4d6ea4
