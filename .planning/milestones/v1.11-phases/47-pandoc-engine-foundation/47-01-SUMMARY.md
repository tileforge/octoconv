---
phase: 47-pandoc-engine-foundation
plan: 01
subsystem: convert
tags: [pandoc, opts-allowlist, injection-defense, json-strict-parse, argv-hardening]

# Dependency graph
requires:
  - phase: 43-ocr-engine-foundation
    provides: "OCROpts/ocrLanguageArgs closed-allowlist + strict-parse + discrete-argv clone pattern"
  - phase: 30 (audio foundation, v1.8)
    provides: "AudioOpts closed-allowlist pattern + checkStrictObject shared helper (opts.go)"
provides:
  - "PandocOpts closed 5-field typed allowlist (Standalone/Toc/NumberSections/Wrap/SectionDivs)"
  - "ParsePandocOpts strict-parse (checkStrictObject + DisallowUnknownFields + wrap enum-gate)"
  - "PandocOptsFromMap persisted-map round-trip through the same strict allowlist"
  - "pandocArgs discrete-argv builder (each opt one argv element; --wrap=<enum> the only value-bearing flag)"
  - "ValidatePandocApplicability engine-scoped applicability check"
  - "EnginePandoc engine-class constant (unregistered, standalone)"
affects: [47-02-content-validation, 47-03-pandoc-converter-core, 48-async-pipeline-wiring]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Closed typed-opts allowlist strict-parsed via shared checkStrictObject, cloned verbatim from OCROpts/AudioOpts"
    - "Discrete-argv builder discipline: every validated opt reaches the external CLI as its own argv slice element, never a concatenated/interpolated string"

key-files:
  created:
    - internal/convert/pandocopts.go
    - internal/convert/pandocopts_test.go
  modified:
    - internal/convert/convert.go

key-decisions:
  - "PandocOpts field set fixed at the D-04 core five (standalone/toc/number_sections/wrap/section_divs) with wrap enum-gated via a closed map lookup (pandocWrapAllowlist), mirroring audioLanguageAllowlist/ocrLanguageAllowlist's discipline exactly"
  - "EnginePandoc added to convert.go's single engine-class const source of truth but the converter is left completely unregistered (no converters.go/init() touch) -- Phase 47 Wave 1 scope fence honored"

patterns-established:
  - "PandocOpts/ParsePandocOpts/PandocOptsFromMap/ValidatePandocApplicability/pandocArgs: the fourth clone of the OCROpts/AudioOpts/DocOpts opts-allowlist shape, now covering pandoc's five D-04 fields"

requirements-completed: [PDCO-01]

# Metrics
duration: 18min
completed: 2026-07-30
---

# Phase 47 Plan 01: PandocOpts Closed Allowlist + Injection Defense Summary

**PandocOpts closed 5-field typed allowlist with strict-parse (checkStrictObject + DisallowUnknownFields), wrap enum-gated via map lookup, and a discrete-argv `pandocArgs` builder proven by an injection test that hostile crafted wrap strings are rejected at parse time, before ever reaching pandoc's argv.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-07-30T12:06:00Z (approx)
- **Completed:** 2026-07-30T12:24:00Z
- **Tasks:** 2 (both TDD: RED -> GREEN)
- **Files modified:** 3 (1 new source, 1 new test, 1 const addition)

## Accomplishments
- `PandocOpts` closed struct (`Standalone`, `Toc`, `NumberSections`, `Wrap`, `SectionDivs`) with `ParsePandocOpts` strict-decoding via the shared `checkStrictObject` helper + `json.Decoder.DisallowUnknownFields` -- unknown keys, duplicate keys, trailing bytes, and top-level `null` all rejected before any work begins
- `Wrap` validated against a closed `pandocWrapAllowlist` map (`auto`/`none`/`preserve`) -- never a raw client string reaching pandoc's argv
- `PandocOptsFromMap` round-trips a persisted `map[string]any` through the identical strict allowlist used on the write path
- `pandocArgs` discrete-argv builder: each bool becomes its own slice element (`--standalone`, `--toc`, `--number-sections`, `--section-divs`); `Wrap` becomes the single value-bearing `--wrap=<enum>` element -- no other concatenated/interpolated string form exists
- `ValidatePandocApplicability` engine-scopes non-empty `PandocOpts` to `EnginePandoc` only
- Injection test (`TestPandocOptsInjection`) proves 5 hostile crafted `wrap` payloads (shell-metacharacter-shaped, command-substitution-shaped, newline-shaped, flag-injection-shaped) are all rejected at `ParsePandocOpts` -- the PDCO-01 trust-boundary proof
- `EnginePandoc = "pandoc"` added to `convert.go`'s single engine-class const source of truth, left completely unregistered (no `converters.go`, no API/worker/exec.go touch)

## Task Commits

Each task was committed atomically (RED -> GREEN per TDD task; no REFACTOR needed, code was clean on first pass):

1. **Task 1: EnginePandoc constant + PandocOpts strict-parse + wrap enum allowlist**
   - `3d4c2e6` (test) - failing test for ParsePandocOpts/PandocOptsFromMap (compile-fails: types don't exist yet)
   - `df4ffc1` (feat) - PandocOpts + ParsePandocOpts + PandocOptsFromMap + EnginePandoc const; tests green
2. **Task 2: pandocArgs discrete-argv builder + ValidatePandocApplicability + injection test**
   - `fa7450d` (test) - failing test for pandocArgs/ValidatePandocApplicability/injection (compile-fails: functions don't exist yet)
   - `8c1ac2d` (feat) - pandocArgs + ValidatePandocApplicability + isZeroPandocOpts; tests green

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified
- `internal/convert/pandocopts.go` - PandocOpts type, ParsePandocOpts, PandocOptsFromMap, ValidatePandocApplicability, isZeroPandocOpts, pandocArgs, pandocWrapAllowlist (145 lines)
- `internal/convert/pandocopts_test.go` - TestParsePandocOpts, TestPandocOptsFromMap, TestPandocArgs, TestValidatePandocApplicability, TestPandocOptsInjection (230 lines)
- `internal/convert/convert.go` - added `EnginePandoc = "pandoc"` to the engine-class const block (1 line)

## Decisions Made
- Followed the plan's D-04 field set exactly: no additional opts, no scope creep beyond the five documented fields.
- Kept `pandocArgs`/`ValidatePandocApplicability`/`isZeroPandocOpts` in the same file as the parse/allowlist logic (mirrors `ocropts.go`'s single-file shape) rather than splitting into a separate file -- consistent with the plan's `files_modified` list (only `pandocopts.go`/`pandocopts_test.go`/`convert.go`).

## Deviations from Plan

**1. [Cosmetic - grep whitespace] Acceptance-criteria grep for `EnginePandoc` needed relaxed spacing**
- **Found during:** Task 1 verification
- **Issue:** The plan's literal acceptance grep `grep -q 'EnginePandoc = "pandoc"'` (single space around `=`) does not match after `gofmt` re-aligns the const block (which inserts multiple spaces to align `=` across all four/five entries: `EnginePandoc   = "pandoc"`).
- **Fix:** No code fix needed -- `gofmt -l` reports the file clean (correctly formatted); this is expected `gofmt` alignment behavior, not a bug. Verified the constant exists and is correct via `grep -n 'EnginePandoc' internal/convert/convert.go`.
- **Files modified:** None (verification-only note).
- **Impact:** None on functionality; the constant is present, correctly named, and correctly valued. Documented here since the plan's literal grep command would report a false negative if run verbatim.

---

**Total deviations:** 1 (cosmetic, no code change, verification-note only)
**Impact on plan:** None -- all functional acceptance criteria met; no scope creep.

## Issues Encountered
None.

## TDD Gate Compliance

Both tasks followed the mandatory RED -> GREEN sequence:
- Task 1: `test(47-01)` commit `3d4c2e6` (RED, compile-fails) -> `feat(47-01)` commit `df4ffc1` (GREEN, tests pass)
- Task 2: `test(47-01)` commit `fa7450d` (RED, compile-fails) -> `feat(47-01)` commit `8c1ac2d` (GREEN, tests pass)

No REFACTOR commits were needed -- both GREEN implementations were clean on first pass (`gofmt -l` reports no files, `go vet ./internal/convert/...` clean).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `PandocOpts`/`ParsePandocOpts`/`PandocOptsFromMap`/`ValidatePandocApplicability`/`pandocArgs` are ready for Plan 03's `PandocConverter.Convert` to consume directly.
- `EnginePandoc` constant exists for Plan 03's `Converter.Engine()` implementation and for `ValidatePandocApplicability`'s engine-scoping.
- Scope fence respected: no converter registration, no `converters.go`/`init()` change, no API/worker/exec.go/go.mod change -- verified via `git diff --stat` against those paths (empty).
- `go build ./...`, `go vet ./...`, and `go test ./internal/convert/... -count=1` all pass cleanly across the whole package (not just the new tests).
- No blockers for Plan 02 (content-validation probes) or Plan 03 (converter core + `--sandbox` hardening + SC3 canary), both of which are independent of this plan's output at the Go-type level (Plan 03 imports `pandocopts.go`'s exports; Plan 02 is orthogonal, working in `sniff.go`/`docsniff.go`).

## Self-Check: PASSED

All created files and referenced commit hashes verified present.

---
*Phase: 47-pandoc-engine-foundation*
*Completed: 2026-07-30*
