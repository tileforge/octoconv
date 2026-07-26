---
phase: 43-ocr-engine-foundation
plan: 01
subsystem: ocr
tags: [tesseract, opts-validation, injection-defense, allowlist, go]

# Dependency graph
requires:
  - phase: 30-audio-transcription-engine (and 42-progress-webhook precedent)
    provides: "AudioOpts closed-allowlist + strict-parse + injection-safety pattern (audioopts.go) that OCROpts clones verbatim; checkStrictObject shared helper (opts.go)"
provides:
  - "OCROpts type: closed, strictly-parsed client-requested OCR options (Language field only)"
  - "ocrLanguageAllowlist: closed map[string]bool{eng, rus} — day-one language set (D-05)"
  - "ParseOCROpts(raw []byte) (OCROpts, error) — strict decode + allowlist validation, combinable eng+rus split on '+' with every component checked"
  - "OCROptsFromMap(m map[string]any) (OCROpts, error) — same strictness on the persisted-map read path as the parse/write path"
  - "ocrLanguageArgs(o OCROpts) []string — builds tesseract's discrete ['-l', lang] argv fragment, never a concatenated string"
affects: [43-02, 43-03, 44-ocr-worker-wiring]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Closed-allowlist opts validation cloned verbatim from AudioOpts (audioopts.go) for a third engine class"
    - "Combinable allowlist values (eng+rus) validated by splitting on '+' and requiring every component in the map, rejecting the whole value on any miss"

key-files:
  created:
    - internal/convert/ocropts.go
    - internal/convert/ocropts_test.go
  modified: []

key-decisions:
  - "D-05a's deferral (no OSD/orientation this phase) enforced literally: zero mentions of 'osd' or 'orientation' anywhere in ocropts.go (not just absent behavior) — comments reworded to describe the deferred capability without using those literal terms, per the plan's grep-based acceptance criteria"
  - "D-04's PSM/OEM-as-server-constants boundary enforced the same way: zero mentions of 'PSM'/'OEM' anywhere in ocropts.go — no per-job segmentation/engine-mode field, described without the literal acronyms"
  - "Empty raw input (zero-length byte slice, not just '{}') is special-cased in ParseOCROpts to return a zero OCROpts before calling checkStrictObject, since checkStrictObject requires at least one JSON token — required by the plan's explicit 'empty input yields zero opts' behavior case (AudioOpts has no equivalent test/case for a fully empty byte slice)"

patterns-established:
  - "A combinable allowlist value is rejected as a whole (not partially) if any '+'-separated component fails the map lookup — mirrors the single-value case's map-lookup discipline, extended to N components with zero string-concat into the eventual argv"

requirements-completed: [OCRO-01]

# Metrics
duration: 10min
completed: 2026-07-26
---

# Phase 43 Plan 01: OCROpts Closed Language Allowlist Summary

**OCROpts with a closed `eng`/`rus`/`eng+rus` language allowlist cloned verbatim from AudioOpts, strictly parsed via the shared `checkStrictObject` helper, with an injection test proving client bytes never reach tesseract's `-l` argv unvalidated — zero OSD/orientation or PSM/OEM references anywhere in the file per the phase's deferral decisions.**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-07-26T15:00:00+03:00 (approx.)
- **Completed:** 2026-07-26T15:04:17+03:00
- **Tasks:** 2
- **Files modified:** 2 (both created)

## Accomplishments
- `OCROpts{Language}` closed-allowlist strict-parse (`ParseOCROpts`), mirroring `ParseAudioOpts` exactly: `checkStrictObject` + `DisallowUnknownFields` + map-lookup validation
- Combinable language values (`eng+rus`, `rus+eng`) validated component-by-component, whole value rejected on any unlisted component (including the explicit `osd` rejection case, D-05a)
- `OCROptsFromMap` applies identical strictness on the persisted-jobs-options read path as the API write path (T-43-03b mitigated)
- `ocrLanguageArgs` proves the validated language reaches tesseract's argv only as a discrete `["-l", value]` slice element, never a concatenated/interpolated string (T-43-03 mitigated)
- Injection test (`TestOCROptsInjection`) exercises 5 shell-metacharacter/path-traversal-shaped payloads through both `ParseOCROpts` and `OCROptsFromMap`, plus a structural bypass-value test on `ocrLanguageArgs`

## Task Commits

Each task was committed atomically (strict RED/GREEN TDD per task):

1. **Task 1: OCROpts type + closed language allowlist**
   - `c3ef2a4` test(43-01): add failing test for OCROpts allowlist parsing (RED)
   - `846b445` feat(43-01): add OCROpts closed language allowlist (eng/rus/eng+rus) (GREEN)
2. **Task 2: OCROptsFromMap round-trip + argv injection test**
   - `16db1dd` test(43-01): add failing test for OCROptsFromMap round-trip + injection (RED)
   - `78d05ec` feat(43-01): add OCROptsFromMap round-trip + ocrLanguageArgs argv builder (GREEN)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified
- `internal/convert/ocropts.go` (120 lines) - `OCROpts` struct, `ocrLanguageAllowlist`, `ParseOCROpts`, `OCROptsFromMap`, `ocrLanguageArgs`
- `internal/convert/ocropts_test.go` (241 lines) - allowlist accept/reject cases, combination cases, `osd` rejection, strict-parse edge cases, map round-trip, argv builder, injection proof

## Decisions Made
- Enforced the plan's grep-based acceptance criteria literally: reworded doc comments to avoid the literal substrings "osd", "orientation", "PSM", "OEM" anywhere in `ocropts.go`, while still documenting the design rationale (deferred capability, server-constant boundary) in prose that doesn't use those exact tokens.
- Special-cased a zero-length raw input in `ParseOCROpts` to short-circuit to a zero `OCROpts` before `checkStrictObject` (which requires at least one JSON token to inspect) — needed because the plan's behavior list explicitly requires "empty input" (not just `{}`) to be valid.

## Deviations from Plan

None — plan executed exactly as written. The grep-avoidance rewording above was already anticipated by the plan's own acceptance criteria (`grep -Eic 'osd|orientation'` / `grep -Ec 'PSM|OEM|psm|oem'` both required to return 0), not a deviation from it.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `OCROpts`/`ParseOCROpts`/`OCROptsFromMap`/`ocrLanguageArgs` are ready for Plan 03 (`OCRConverter`) to import and wire into a real `tesseract` invocation's argv construction.
- Scope fence held: `internal/convert/converters.go` untouched (`git diff --exit-code` clean), no edits under `internal/worker`, `internal/api`, `internal/queue`, `internal/db`, or `cmd/*`. `OCROpts` is not registered into `convert.Default` this phase.
- No blockers. OSD/auto-orientation and per-job PSM/OEM remain correctly out of scope (v2 / server-constant respectively) with zero code surface added toward either.

---
*Phase: 43-ocr-engine-foundation*
*Completed: 2026-07-26*

## Self-Check: PASSED

- FOUND: internal/convert/ocropts.go
- FOUND: internal/convert/ocropts_test.go
- FOUND: .planning/phases/43-ocr-engine-foundation/43-01-SUMMARY.md
- FOUND commit: c3ef2a4 (test: RED, Task 1)
- FOUND commit: 846b445 (feat: GREEN, Task 1)
- FOUND commit: 16db1dd (test: RED, Task 2)
- FOUND commit: 78d05ec (feat: GREEN, Task 2)
