---
phase: quick-260802-9zc
plan: 01
subsystem: convert
tags: [go, utf8, sniff, html, security, fail-closed]

# Dependency graph

requires:
  - phase: quick-260802-9de
    provides: "readSniffPrefix's truncation-scoped trimPartialTrailingRune fix (PR#2 finding-2), already applied to LooksLikeLaTeX/LooksLikeText"
provides:
  - "LooksLikeHTML delegates its bounded prefix read + NUL/UTF-8 fail-closed check to the shared readSniffPrefix helper instead of a duplicated copy"
  - "Three new htmlsniff_test.go regression tests: straddling-rune acceptance, invalid-UTF-8-at-cap-boundary rejection, dangling-lead-byte-at-EOF rejection"
  - "markupsniff.go doc prose corrected to name LooksLikeHTML as a readSniffPrefix caller"

affects: [convert, api-upload-gate, pandoc-output-validation]

# Tech tracking

tech-stack:
  added: []
  patterns:
    - "Single shared sniff-prefix envelope (readSniffPrefix) now has exactly one implementation and three callers (LooksLikeHTML, LooksLikeLaTeX, LooksLikeText) -- duplication that let a bugfix miss one caller is now structurally impossible"

key-files:
  created: []
  modified:
    - internal/convert/htmlsniff.go
    - internal/convert/htmlsniff_test.go
    - internal/convert/markupsniff.go

key-decisions:
  - "Single atomic commit per task (fix(...) for Task 1, docs(...) for Task 2) rather than separate test/feat commits, per the plan's explicit <output> instruction -- the plan's type is execute (task-level tdd=\"true\"), not a whole-plan type: tdd gate, so the generic RED/GREEN/REFACTOR multi-commit flow does not apply here."

patterns-established: []

requirements-completed: [PR2-F2-HTML]

# Metrics

duration: ~15min
completed: 2026-08-02
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: unknown
---

# Quick Task 260802-9zc: Fix LooksLikeHTML's mid-rune UTF-8 truncation defect Summary

**LooksLikeHTML now delegates to the shared readSniffPrefix helper (instead of a hand-rolled duplicate prologue), closing the last surviving instance of the PR#2 finding-2 mid-rune truncation bug -- a valid HTML file over 1 MiB whose cap-severed cut lands inside a multi-byte rune is no longer wrongly rejected.**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-08-02
- **Tasks:** 2 completed
- **Files modified:** 3

## Accomplishments

- `LooksLikeHTML` (internal/convert/htmlsniff.go) no longer carries its own `make([]byte, ...)` / `ReadAt` / `utf8.Valid` prologue -- it calls `readSniffPrefix(r, size, htmlSniffCap)` and keeps only the BOM/whitespace trim and the marker scan below, byte-for-byte unchanged
- `unicode/utf8` import dropped from htmlsniff.go (now unused); `bytes` and `io` retained
- Three new regression tests added to `internal/convert/htmlsniff_test.go`, mirroring the LaTeX/Text precedent in `markupsniff_test.go`
- `readSniffPrefix`'s doc comment in `internal/convert/markupsniff.go` corrected to name `LooksLikeHTML` as a caller alongside `LooksLikeLaTeX`/`LooksLikeText`, and the package doc comment now notes htmlsniff.go's dependency on this file rather than merely preceding it
- Both production callers (`internal/api/handlers.go:240`, `internal/convert/pandoc.go:253`) proven byte-for-byte untouched by `git diff --stat`

## RED/GREEN Observation (Task 1, tdd="true")

Ran the three new tests against the **unmodified** htmlsniff.go first:

```
=== RUN   TestLooksLikeHTMLRuneStraddlingSniffCap
    htmlsniff_test.go:152: LooksLikeHTML on a rune straddling the sniff cap = false, want true
--- FAIL: TestLooksLikeHTMLRuneStraddlingSniffCap (0.00s)
=== RUN   TestLooksLikeHTMLInvalidUTF8AtSniffCapStillRejected
--- PASS: TestLooksLikeHTMLInvalidUTF8AtSniffCapStillRejected (0.00s)
=== RUN   TestLooksLikeHTMLIncompleteRuneAtEOFStillRejected
--- PASS: TestLooksLikeHTMLIncompleteRuneAtEOFStillRejected (0.00s)
FAIL
```

RED confirmed: Test 1 genuinely failed against the unmodified code (proof the bug exists), while Tests 2 and 3 already passed (proof the fix must not widen acceptance). After applying the substitution in htmlsniff.go, all three pass:

```
--- PASS: TestLooksLikeHTMLRuneStraddlingSniffCap (0.00s)
--- PASS: TestLooksLikeHTMLInvalidUTF8AtSniffCapStillRejected (0.00s)
--- PASS: TestLooksLikeHTMLIncompleteRuneAtEOFStillRejected (0.00s)
PASS
```

## Task Commits

Each task was committed atomically (single commit per task, per the plan's `<output>` instruction):

1. **Task 1: Reuse readSniffPrefix in LooksLikeHTML, with the straddling-rune regression guard** - `a94acdc` (fix)
2. **Task 2: Correct the now-stale shared-helper prose and prove both callers unaffected** - `520cb7a` (docs)

## Files Created/Modified

- `internal/convert/htmlsniff.go` - `LooksLikeHTML` reduced to a single `readSniffPrefix` call + BOM/whitespace trim + marker scan; `unicode/utf8` import dropped; doc comment updated to explain the WHY (duplication caused the original miss)
- `internal/convert/htmlsniff_test.go` - added `TestLooksLikeHTMLRuneStraddlingSniffCap`, `TestLooksLikeHTMLInvalidUTF8AtSniffCapStillRejected`, `TestLooksLikeHTMLIncompleteRuneAtEOFStillRejected`
- `internal/convert/markupsniff.go` - `readSniffPrefix`'s doc comment now names `LooksLikeHTML` as a caller; package doc comment clarifies the current dependency direction; function body, trim condition, and `leadByteRuneLen` untouched

## Decisions Made

- Followed the plan's explicit `<output>` instruction for one commit per task (`fix(...)` for Task 1, `docs(...)` for Task 2) rather than the generic multi-commit RED/GREEN/REFACTOR TDD flow, since this plan's frontmatter `type: execute` with task-level `tdd="true"` is not a whole-plan `type: tdd` gate. The RED-before/GREEN-after observation above satisfies the plan's TDD verification requirement without a separate `test(...)` commit.

## Deviations from Plan

None - plan executed exactly as written. No Rule 1/2/3/4 auto-fixes were needed; the fix was a pure substitution as scoped.

## Notes carried into SUMMARY (per plan's `<output>` instructions)

- `pandocValidateOutput`'s existing doc comment (internal/convert/pandoc.go, around line 229) already asserted "every sniffer below (LooksLikeHTML/LooksLikeLaTeX/LooksLikeText) is ITSELF fail-closed on a zero-size input (readSniffPrefix, markupsniff.go)" -- a claim that was imprecise for `LooksLikeHTML` before this change (it had its own separate zero-size guard, not `readSniffPrefix`'s) and becomes literally true as a side effect of this fix. No edit was made to pandoc.go; this is a comment the fix repaired, not one that was broken.
- All pre-existing `TestLooksLikeHTML*` assertions in `internal/convert/htmlsniff_test.go` (the full table, `TestLooksLikeHTMLEmptyInput`, `TestLooksLikeHTMLNeverPanicsShortReaderAt`, `TestLooksLikeHTMLOversizedInputBoundedRead`) were **not edited** -- only appended to. Confirmed by `go test ./internal/convert/ -run 'TestLooksLikeHTML' -count=1 -v` passing in full.

## Issues Encountered

None.

## Verification Performed

- `go test ./internal/convert/ -run 'TestLooksLikeHTML(RuneStraddlingSniffCap|InvalidUTF8AtSniffCapStillRejected|IncompleteRuneAtEOFStillRejected)' -count=1 -v` -- RED before fix, GREEN after
- `go build ./...` -- clean (proves `unicode/utf8` import was dropped without breaking compilation)
- `go vet ./internal/convert/` -- clean
- `go test ./internal/convert/ -run 'TestLooksLikeHTML' -count=1 -v` -- full pre-existing table passes unmodified
- `grep -v '^//' internal/convert/htmlsniff.go | grep -c 'readSniffPrefix(r, size, htmlSniffCap)'` -- reports exactly 1
- `gofmt -l .` -- empty (whole repo)
- `go build ./... && go vet ./...` -- whole-repo clean
- `go test ./internal/convert/ ./internal/api/ -count=1` -- both green
- `go test ./internal/... ./cmd/... -count=1` -- all 18 testable packages green (10 packages report `[no test files]`, expected)
- `git diff --stat -- internal/api/handlers.go internal/convert/pandoc.go` -- empty, confirming both production callers untouched
- `git diff --name-only cedaee68188c0237e6203f15e74d1977eb4bde5c HEAD` -- exactly `internal/convert/htmlsniff.go`, `internal/convert/htmlsniff_test.go`, `internal/convert/markupsniff.go`

## Next Phase Readiness

This closes the PR2-F2-HTML requirement -- the mid-rune UTF-8 truncation defect no longer has any surviving instance in the sniff family (LooksLikeHTML, LooksLikeLaTeX, LooksLikeText all now share the single `readSniffPrefix` envelope). No blockers. No further action needed on this quick task; the wider sniff-family unification (docsniff.go/SniffContainer) remains explicitly out of scope per this plan's non_goals.

---
*Phase: quick-260802-9zc*
*Completed: 2026-08-02*

## Self-Check: PASSED

- FOUND: internal/convert/htmlsniff.go
- FOUND: internal/convert/htmlsniff_test.go
- FOUND: internal/convert/markupsniff.go
- FOUND: SUMMARY.md
- FOUND commit: a94acdc
- FOUND commit: 520cb7a
