---
phase: 56-jobrunner-consolidation-structured-error-codes
plan: 06
subsystem: docs
tags: [docs, error-codes, testing, go]

# Dependency graph
requires:
  - phase: 56-03
    provides: "internal/errcode's closed 14-code sync vocabulary, All()/IsKnown(), fully swept across internal/api"
  - phase: 56-04
    provides: "internal/worker/descriptors.go's frozen 14-code async vocabulary and AllFailureCodes()"
provides:
  - "docs/ERROR-CODES.md: the frozen, versioned error-code contract (both halves) for the frontend and both MCP servers"
  - "internal/errcode/doc_sync_test.go: bidirectional Go-to-doc consistency proof, fails CI on any drift in either direction"
affects: [56-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Column-anchored markdown table parsing (regexp anchored to line start + first backtick cell) instead of whole-file backtick scanning, so prose mentions of a code name in a detail column cannot satisfy the sync-test's set-equality check"
    - "docs/ERROR-CODES.md as the freeze artifact required by PITFALLS.md Pitfall 11, generated/maintained from Go constants (D-06) and enforced by a same-package-family test, not CI-external tooling"

key-files:
  created:
    - docs/ERROR-CODES.md
    - internal/errcode/doc_sync_test.go
  modified:
    - docs/README.md

key-decisions:
  - "Table-row parsing anchors on '^| `code`' (start-of-line, first cell only) rather than a whole-file backtick-token scan, per the plan's own guidance -- the document's prose legitimately uses backticks around non-code identifiers (`opts`, `message`, `callback_url`, `GET /v1/formats`) and even mentions the sync `invalid_options` code by name inside the async table's own detail column; a whole-file scan would have misreported those as extra/unknown codes."
  - "Checked sync and async halves against errcode.All()/worker.AllFailureCodes() INDEPENDENTLY (not via a single merged-union comparison) so a code accidentally placed in the wrong table (e.g. a sync code appearing only in the async section) would be caught -- a union-only comparison would have missed that class of misplacement."
  - "invalid_options requires no special-case exception in the test: since both Go vocabularies independently declare an invalid_options constant (errcode.InvalidOptions, worker.CodeInvalidOptions) and both document tables independently list it, per-half set-equality checks pass naturally without any exclusion list. TestSyncAndAsyncShareInvalidOptions separately pins that the two constants are byte-identical (D-03)."

requirements-completed: [ERRC-01]

# Metrics
duration: ~20min
completed: 2026-08-08
---

# Phase 56 Plan 06: docs/ERROR-CODES.md freeze point + Go-to-doc sync test Summary

**Published `docs/ERROR-CODES.md` as the single frozen, versioned list of all 28 error codes (14 sync + 14 async) the service can emit, and tied it to the Go source with a bidirectional set-equality test that fails on drift in either direction.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-08 (worktree reset to base f919ea5, no drift encountered)
- **Completed:** 2026-08-08
- **Tasks:** 2 completed
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments

- `docs/ERROR-CODES.md` (new): states the vocabulary is FROZEN as of milestone v1.13 Phase 56, documents the flat `{"error","code"}` sync envelope (D-01) and the `error_code`/`error_message` async surface, tables all 14 sync codes (`internal/errcode/errcode.go`) and all 14 async codes (`internal/worker/descriptors.go`), states the D-03 one-code-space rule for `invalid_options`, states the Pitfall-12 unknown-code consumer contract as a MUST requirement with an explicit anti-closed-enum warning, and states the ERRC-V2-01/ERRC-V2-02 deferrals (D-07).
- `docs/README.md` updated to index the new document, following the file's existing entry style.
- `internal/errcode/doc_sync_test.go` (new, external package `errcode_test`): `TestErrorCodesDocMatchesGoConstants` parses only the code column of each markdown table (anchored regexp, not a whole-file backtick scan) and asserts set equality, independently per half, against `errcode.All()` and `worker.AllFailureCodes()`; `TestSyncAndAsyncShareInvalidOptions` pins `errcode.InvalidOptions == worker.CodeInvalidOptions`; `TestDocDeclaresUnknownCodeFallback` asserts the document contains the exact Pitfall-12 fallback phrase.
- Both failure directions manually verified per the plan's acceptance criteria (see "Manual Verification Record" below) before restoring the document to its committed state.
- `go build ./... && go vet ./... && gofmt -l internal/errcode && go test ./... -count=1` green (28 tested/no-test packages).
- `go list -deps ./internal/worker | grep -c 'octoconv/internal/errcode'` returns `0` — no production dependency introduced between the two packages.

## Task Commits

1. **Task 1: Write docs/ERROR-CODES.md as the frozen contract**
   - `0895e8f` (docs) - `docs/ERROR-CODES.md` created, `docs/README.md` indexed

2. **Task 2: Tie the document to the Go source with a bidirectional sync test**
   - `7639368` (test) - `internal/errcode/doc_sync_test.go` created with the three required tests

**Plan metadata:** this commit (docs: complete plan) — created after this SUMMARY.

## Files Created/Modified

- `docs/ERROR-CODES.md` - the frozen error-code contract: envelope shapes, sync table (14 rows), async table (14 rows), one-code-space note, consumer contract, not-in-scope note, and the "Keeping this document honest" closer naming the sync test
- `docs/README.md` - one paragraph added to the docs index describing `ERROR-CODES.md` and noting `openapi.yaml` enum publication (ERRC-V2-02) is a separate, deferred step
- `internal/errcode/doc_sync_test.go` (new) - `TestErrorCodesDocMatchesGoConstants`, `TestSyncAndAsyncShareInvalidOptions`, `TestDocDeclaresUnknownCodeFallback`, plus the `extractTableCodes`/`diffSets`/`readDoc` helpers and the `codeRowPattern` regexp

## Decisions Made

See `key-decisions` in frontmatter for the parsing-anchor choice (column-anchored regexp vs whole-file backtick scan) and the independent-per-half comparison choice (vs a single merged-union comparison). Both were made to satisfy the plan's own stated risk ("a code merely mentioned in prose cannot satisfy the check") and its "behavior" bullet list (each Go vocabulary's codes must appear specifically in its OWN section of the document, not merely anywhere in the file).

## Deviations from Plan

None - plan executed exactly as written. Both tasks' acceptance criteria were met without needing any Rule 1-4 deviation.

## Manual Verification Record (Task 2 acceptance criteria)

Per the plan's instruction to prove `TestErrorCodesDocMatchesGoConstants` actually fails on both classes of real drift:

**1. Row deletion (missing code):** temporarily deleted the `born_digital` table row from `docs/ERROR-CODES.md` and reran the test:

```
=== RUN   TestErrorCodesDocMatchesGoConstants
    doc_sync_test.go:155: async table drift between worker.AllFailureCodes() and docs/ERROR-CODES.md -- missing from the document: [born_digital]; present in the document but not declared by worker.AllFailureCodes(): []
--- FAIL: TestErrorCodesDocMatchesGoConstants (0.00s)
```

**2. Bogus row injection (extra code):** temporarily added a `| \`bogus_made_up_code\` | 999 | ... |` row to the synchronous table and reran the test:

```
=== RUN   TestErrorCodesDocMatchesGoConstants
    doc_sync_test.go:152: sync table drift between errcode.All() and docs/ERROR-CODES.md -- missing from the document: []; present in the document but not declared by errcode.All(): [bogus_made_up_code]
--- FAIL: TestErrorCodesDocMatchesGoConstants (0.00s)
```

Both failure messages name the exact offending code, as required. The document was restored to its committed state after each check (`git status --short` confirmed no residual diff before the Task 2 commit); `go test ./internal/errcode/... -count=1 -v` was rerun green after restoration and before committing.

## TDD Gate Compliance

Task 2 is marked `tdd="true"`, but — mirroring plan 56-04's own precedent for the same situation (documented in its SUMMARY) — the behavior these three tests assert was already fully implemented before this test file was written: Task 1 (this same plan) created `docs/ERROR-CODES.md`, and the Go constants (`errcode.All()`, `worker.AllFailureCodes()`, `errcode.InvalidOptions`, `worker.CodeInvalidOptions`) were established in plans 56-03/56-04, both of which landed before this plan started. Running the three new tests for the first time passed immediately — that is the intended proof of consistency, not a RED-phase violation, since there was no new production behavior left to implement in Task 2 (its only file is the test itself). The RED-equivalent proof for this task is the manual verification above: deliberately breaking each direction of the invariant and observing the named failure, which is the acceptance criteria's own prescribed substitute for a true RED phase on a pure verification test. A single `test(56-06): ...` commit was made; no separate `feat`/`refactor` commit was needed.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `docs/ERROR-CODES.md` is now the fixed migration target plan 56-07 (frontend/MCP consumer migration) needs per PITFALLS.md Pitfall 11 — the vocabulary is frozen and any future addition (Phases 58/60's `inspect`/`sanitize`) is proven additive-only by `TestErrorCodesDocMatchesGoConstants` running in CI going forward.
- `docs/openapi.yaml` remains untouched; ERRC-V2-02 remains explicitly deferred, as stated in the document itself.
- No blockers for plan 56-07.

---
*Phase: 56-jobrunner-consolidation-structured-error-codes*
*Completed: 2026-08-08*

## Self-Check: PASSED

- FOUND: docs/ERROR-CODES.md
- FOUND: internal/errcode/doc_sync_test.go
- FOUND: .planning/phases/56-jobrunner-consolidation-structured-error-codes/56-06-SUMMARY.md
- FOUND commit: 0895e8f (docs: publish docs/ERROR-CODES.md as the frozen error-code contract)
- FOUND commit: 7639368 (test: tie docs/ERROR-CODES.md to the Go source with a bidirectional sync test)
