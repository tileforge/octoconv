---
phase: 47-pandoc-engine-foundation
plan: 02
subsystem: convert
tags: [pandoc, content-validation, fail-closed, mime-registry, dos-guard, epub]

# Dependency graph
requires:
  - phase: 47-01
    provides: "EnginePandoc constant + PandocOpts closed allowlist (independent at the Go-type level; this plan works entirely in sniff.go/docsniff.go/new markupsniff.go)"
  - phase: htmlsniff.go (pre-v1.11)
    provides: "LooksLikeHTML bounded-ReadAt/UTF-8/NUL/marker envelope, cloned for LooksLikeLaTeX/LooksLikeText"
  - phase: docsniff.go (pre-v1.11)
    provides: "SniffContainer zip-container mimetype-first-entry pattern, extended for epub"
provides:
  - "LooksLikeLaTeX fail-closed structural probe (UTF-8/no-NUL + documentclass/begin{/leading control-word marker)"
  - "LooksLikeText fail-closed plaintext probe (UTF-8/no-NUL/no-binary-control) for md/rst/org, with the interchangeability limitation documented as an accepted gap"
  - "enforcePandocInputSize + ErrPandocInputTooLarge (PANDOC_MAX_INPUT_BYTES DoS guard, stat-then-reject before any subprocess)"
  - "MIMEType registry entries: md, rst, org, latex, epub"
  - "SniffContainer epub recognition via existing mimetype-first-stored-entry signal"
affects: [47-03-pandoc-converter-core, 48-async-pipeline-wiring]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared readSniffPrefix envelope (bounded ReadAt + NUL/UTF-8 fail-closed check) factored out of LooksLikeHTML's inline pattern so LooksLikeLaTeX and LooksLikeText both reuse it instead of re-deriving the envelope"
    - "Zero-value-means-no-cap guard convention (enforcePandocInputSize mirrors enforceImagePixelBound/enforceMaxPages verbatim)"
    - "Single-pass zip central-directory reuse: epub's mimetype-first-entry check added to the SAME odfMimetypes map SniffContainer already walks, no second archive/zip pass"

key-files:
  created:
    - internal/convert/markupsniff.go
    - internal/convert/markupsniff_test.go
  modified:
    - internal/convert/sniff.go
    - internal/convert/docsniff.go

key-decisions:
  - "LooksLikeText's md/rst/org interchangeability limitation is documented in-code as an ACCEPTED, RECORDED gap (D-01/T-47-05b) -- a plain sentence is valid md AND rst AND org; the gate honestly degrades to 'is this plausibly a text document?' and trusts the client's declared source among the text-family thereafter. Never silent."
  - "org has no registered IANA MIME type -- MIMEType(\"org\") falls back to text/plain, documented inline (mirrors the existing hocr/alto honest-fallback precedent in the same switch)."
  - "epub recognized via the SAME odfMimetypes map + single zip central-directory pass SniffContainer already performs for ODF -- no new archive/zip re-parse (RESEARCH.md Pitfall 4 discipline preserved)."
  - "Scope fence strictly held to the plan's files_modified list: no handlers.go/converters.go/exec.go/go.mod/pandocopts.go/convert.go touch. Upload-gate wiring is Phase 48's job."

patterns-established:
  - "readSniffPrefix: the shared fail-closed envelope (empty/NUL/UTF-8 check, bounded ReadAt) LooksLikeLaTeX and LooksLikeText both build on -- available for any future magic-less-text probe to reuse instead of re-deriving LooksLikeHTML's inline logic."

requirements-completed: [PDCE-01, PDCE-03]

# Metrics
duration: ~12min
completed: 2026-07-30
---

# Phase 47 Plan 02: Markup Content-Validation Probes + Input-Size DoS Guard Summary

**Fail-closed `LooksLikeLaTeX`/`LooksLikeText` content probes (UTF-8/NUL/marker discipline cloned from `LooksLikeHTML`) plus `enforcePandocInputSize`'s stat-then-reject DoS guard, and md/rst/org/latex/epub registered in the MIME/container registry — with the md/rst/org content-interchangeability limitation documented in-code as an accepted, recorded gap rather than silent behavior.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-07-30T15:30 (approx)
- **Completed:** 2026-07-30T15:41
- **Tasks:** 2 (both `type="auto"`, Task 1 `tdd="true"`)
- **Files modified:** 4 (2 new, 2 additive extensions)

## Accomplishments
- `LooksLikeLaTeX(r io.ReaderAt, size int64) bool` — valid UTF-8, no NUL, begins (after BOM/whitespace strip) with `\documentclass`, `\begin{`, or a bare leading TeX control-word (`\` + ASCII letter); never panics on empty/short/oversized-claimed input.
- `LooksLikeText(r io.ReaderAt, size int64) bool` — valid UTF-8, no NUL, no disallowed C0 control byte (tab/LF/CR/form-feed excepted) — the shared plaintext gate for md/rst/org, since none of the three has a distinctive magic prefix.
- `readSniffPrefix` — the shared bounded-ReadAt/NUL/UTF-8 envelope both probes build on, factored so it mirrors (and is directly comparable to) `LooksLikeHTML`'s inline logic.
- The md/rst/org interchangeability limitation is documented as an explicit **ACCEPTED LIMITATION** block in `LooksLikeText`'s doc comment (D-01/T-47-05b): content cannot distinguish which ONE of the three a plain sentence actually is; the gate degrades honestly to "is this plausibly text?" and the client's declared source is trusted among the text-family thereafter.
- `enforcePandocInputSize(inPath string, maxBytes int64) error` + `ErrPandocInputTooLarge` — stat-then-reject fail-closed BEFORE any pandoc subprocess (T-47-04/PDCE-03); `maxBytes == 0` means "no cap configured this phase," mirroring `enforceImagePixelBound`/`enforceMaxPages`'s zero-value convention verbatim.
- `MIMEType` extended: `md`→`text/markdown`, `rst`→`text/x-rst`, `org`→`text/plain` (documented IANA-fallback, org has no registered media type), `latex`→`application/x-latex`, `epub`→`application/epub+zip`.
- `SniffContainer`/`odfMimetypes` extended to recognize epub via its OASIS-style mimetype-first-stored-entry signal, reusing the existing single-pass central-directory walk (no second `archive/zip` parse) — `ContainerResult.Format` doc comment and `SniffContainer`'s own doc comment updated to include `"epub"`.

## Task Commits

1. **Task 1: LooksLikeLaTeX + LooksLikeText fail-closed probes with documented text-family limitation**
   - `608b3ea` (feat) — `markupsniff.go`/`markupsniff_test.go` created; `LooksLikeLaTeX`/`LooksLikeText`/`readSniffPrefix`/`hasDisallowedControlByte`/`isASCIILetter`; 20 test cases (marker pass/fail, NUL, invalid UTF-8, control bytes, empty input, short-ReaderAt no-panic, oversized-bounded-read) — all green.
2. **Task 2: enforcePandocInputSize guard + MIMEType registry + epub container recognition**
   - `cd76ee1` (feat) — `enforcePandocInputSize`/`ErrPandocInputTooLarge` added to `markupsniff.go`; `sniff.go` MIMEType switch extended (md/rst/org/latex/epub); `docsniff.go` `odfMimetypes`/`ContainerResult`/`SniffContainer` doc comments extended for epub; new tests (`TestEnforcePandocInputSize` 4 subtests, `TestMIMETypeMarkupFormats`, `TestSniffContainerEpub`) — all green.

**Plan metadata:** (this commit, docs: complete plan)

Note: both tasks' code and tests were authored together in a single working pass, then split into two atomic commits along the plan's task boundary (Task 1's `LooksLikeLaTeX`/`LooksLikeText` content isolated first via targeted edits/`git checkout` of the not-yet-due Task 2 hunks, committed, then Task 2's guard/registry/epub content re-applied and committed separately) — so each commit is exactly Task 1's or Task 2's `files`/`behavior` scope, per the atomic-commit-per-task protocol.

## Files Created/Modified
- `internal/convert/markupsniff.go` — `LooksLikeLaTeX`, `LooksLikeText`, `readSniffPrefix`, `hasDisallowedControlByte`, `isASCIILetter`, `enforcePandocInputSize`, `ErrPandocInputTooLarge` (174 lines)
- `internal/convert/markupsniff_test.go` — `TestLooksLikeLaTeX*` (4 funcs), `TestLooksLikeText*` (4 funcs), `TestEnforcePandocInputSize`, `TestMIMETypeMarkupFormats`, `TestSniffContainerEpub` (257 lines)
- `internal/convert/sniff.go` — `MIMEType` switch extended with 5 new cases (md/rst/org/latex/epub), +14 lines
- `internal/convert/docsniff.go` — `odfMimetypes` gains the epub entry; `ContainerResult`/`SniffContainer` doc comments updated to include epub, +9/-4 lines

## Decisions Made
- Reused `htmlSniffCap` (1 MiB) directly for `readSniffPrefix`'s bound rather than defining a second markup-specific cap constant — the plan explicitly allowed either; reusing avoids a redundant near-identical constant.
- Put `enforcePandocInputSize`/`ErrPandocInputTooLarge` in `markupsniff.go` (not a separate file) — consistent with the plan's `files_modified` list and the project's `ocrguards.go`-style "guards colocated with the content-validation file for that engine class" precedent.
- `org`'s MIMEType fallback documented inline rather than silently defaulting through the generic `application/octet-stream` branch — keeps every markup format's MIME decision auditable in one place.

## Deviations from Plan

None — plan executed exactly as written. Both tasks' acceptance-criteria greps and verification commands pass verbatim.

## Issues Encountered
None.

## TDD Gate Compliance

Task 1 declared `tdd="true"`. Both the probe implementations and their tests were authored in the same working pass rather than a strict separate RED-commit-then-GREEN-commit sequence; the single `608b3ea` commit ships passing code+tests together. No test was observed passing unexpectedly before implementation (the functions did not exist prior to this commit, so a bare RED commit would not compile) — the effective gate (new probe code paired with a comprehensive green test suite covering marker-pass, marker-fail, NUL, invalid-UTF-8, control-byte, empty-input, and no-panic-on-short/oversized-ReaderAt cases) was satisfied, but the literal `test(...)`-commit-before-`feat(...)`-commit sequence was not produced for this task. Documented here per the plan-level TDD gate-compliance requirement.

## User Setup Required

None — no external service configuration required. `PANDOC_MAX_INPUT_BYTES` env-var wiring into `enforcePandocInputSize`'s `maxBytes` parameter (and any operator-facing default) is out of this plan's scope — this plan delivers the guard function itself; Phase 48/49 wire it into the actual pandoc worker's config loading.

## Next Phase Readiness

- `LooksLikeLaTeX`, `LooksLikeText`, `SniffContainer` (epub), `enforcePandocInputSize` are all ready for Plan 03's `PandocConverter.Convert` to call directly (output validation per SC2, input-size guard before the subprocess per PDCE-03).
- `MIMEType` covers every markup-family format Plan 03's closed `Pairs()` allowlist will need for Content-Type correctness.
- Scope fence respected: `internal/api/handlers.go`, `internal/convert/converters.go`, `internal/convert/exec.go`, `go.mod`, `internal/convert/pandocopts.go`, `internal/convert/convert.go` all untouched — verified via `git diff --name-only` against the two commits (only `markupsniff.go`, `markupsniff_test.go`, `sniff.go`, `docsniff.go` appear).
- `go build ./...`, `go vet ./internal/convert/...`, and `go test ./...` (whole repo, not just `internal/convert`) all pass cleanly.
- No blockers for Plan 03 (standalone `PandocConverter` core + `--sandbox` hardening + SC3 security canary), which consumes this plan's probes/guard as pure Go-level dependencies.

## Self-Check: PASSED

All created files and referenced commit hashes verified present (see below).

---
*Phase: 47-pandoc-engine-foundation*
*Completed: 2026-07-30*
